import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

// noise.json holds one full Noise_KK handshake (fixed static and ephemeral
// keys, both messages, four transport messages) and six end-to-end prologues,
// each built to catch one way E2EPrologue could be read too loosely (shared/
// noisep/noisep.go, shared/noisep/noisep_test.go). Both engines have to reach
// the same bytes Go reached, or a browser and an API node would agree on
// nothing.
const v = vectors("noise.json");

// Does the Web Crypto API of this engine know X25519? Chrome does; the WebKit
// that Playwright brings answers NotSupportedError to importKey, exactly as
// it does for Ed25519 (sign.spec.js) - but the two curves are not the same
// capability, so this is measured on its own instead of assumed from that
// other gap. Measured directly: pkcs8 import of a static key is the first
// X25519 call HandshakeKK.forTest makes, so it stands for the whole
// handshake - an engine that refuses it can run no message of the test below.
async function hasX25519(shell) {
  return shell.evaluate(async () => {
    const pkcs8Prefix = new Uint8Array([0x30, 0x2e, 0x02, 0x01, 0x00, 0x30, 0x05, 0x06, 0x03, 0x2b,
      0x65, 0x6e, 0x04, 0x22, 0x04, 0x20]);
    const raw = new Uint8Array(32).fill(1);
    const pkcs8 = new Uint8Array(pkcs8Prefix.length + raw.length);
    pkcs8.set(pkcs8Prefix, 0);
    pkcs8.set(raw, pkcs8Prefix.length);
    try {
      await crypto.subtle.importKey("pkcs8", pkcs8, { name: "X25519" }, false, ["deriveBits"]);
      return true;
    } catch {
      return false;
    }
  });
}

test("the end-to-end prologue binds channel, params and task", async ({ shell }) => {
  const got = await shell.evaluate(async (d) => {
    const n = await import("/assets/js/contracts/noise.js");
    const { b64decode, b64encode } = await import("/assets/js/contracts/b64.js");
    const out = [];
    for (const c of d.prologues) {
      const open = JSON.parse(new TextDecoder().decode(b64decode(c.open)));
      out.push(b64encode(await n.e2ePrologue(
        c.controllerId, open.app, open.service, open.action,
        c.params ? b64decode(c.params) : new Uint8Array(0),
        c.task ? b64decode(c.task) : new Uint8Array(0))));
    }
    return out;
  }, v);
  expect(got).toEqual(v.prologues.map((c) => c.prologue));
});

test("the handshake and the transport match the Go vectors", async ({ shell }) => {
  test.skip(!(await hasX25519(shell)),
    "no X25519 in this engine's Web Crypto: it cannot run a Noise_KK handshake at all");
  const got = await shell.evaluate(async (d) => {
    const n = await import("/assets/js/contracts/noise.js");
    const { b64decode, b64encode } = await import("/assets/js/contracts/b64.js");
    const prologue = b64decode(d.prologue);
    const init = await n.HandshakeKK.forTest(true, prologue,
      b64decode(d.initStatic.private), b64decode(d.respStatic.public), b64decode(d.initEphemeral.private));
    const resp = await n.HandshakeKK.forTest(false, prologue,
      b64decode(d.respStatic.private), b64decode(d.initStatic.public), b64decode(d.respEphemeral.private));
    const msg1 = await init.writeMessage1();
    await resp.readMessage1(msg1);
    const msg2 = await resp.writeMessage2();
    await init.readMessage2(msg2);
    const a = await init.split(), b = await resp.split();
    const seen = [];
    for (const t of d.transport) {
      const from = t.from === "initiator" ? a : b;
      const to = t.from === "initiator" ? b : a;
      const cipher = await from.seal(b64decode(t.plaintext));
      const plain = await to.open(b64decode(t.ciphertext));
      seen.push({ cipher: b64encode(cipher), plain: b64encode(plain) });
    }
    return { msg1: b64encode(msg1), msg2: b64encode(msg2), seen };
  }, v);
  expect(got.msg1).toBe(v.msg1);
  expect(got.msg2).toBe(v.msg2);
  expect(got.seen.map((s) => s.cipher)).toEqual(v.transport.map((t) => t.ciphertext));
  expect(got.seen.map((s) => s.plain)).toEqual(v.transport.map((t) => t.plaintext));
});

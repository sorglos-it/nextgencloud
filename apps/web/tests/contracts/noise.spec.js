import { test, expect, skipWithoutFixedKeys, skipWithoutHandshake } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

// noise.json holds one full Noise_KK handshake (fixed static and ephemeral
// keys, both messages, four transport messages) and six end-to-end prologues,
// each built to catch one way E2EPrologue could be read too loosely (shared/
// noisep/noisep.go, shared/noisep/noisep_test.go). Both engines have to reach
// the same bytes Go reached, or a browser and an API node would agree on
// nothing.
const v = vectors("noise.json");

// TWO guards below, and which one a case gets depends on the keys IT uses, not
// on this file being about Noise. fixtures/page.js holds both.
//
//   skipWithoutFixedKeys  for the cases that rebuild noise.json through
//                         HandshakeKK.forTest. forTest reads the public half
//                         out of a PKCS#8 document (publicOf), and Firefox
//                         cannot do that.
//   skipWithoutHandshake  for the one case that makes its own keys with
//                         newKeypair() and of(). It never touches publicOf, so
//                         X25519 alone is what it needs - and Firefox has it,
//                         so it RUNS there.
//
// That difference is not cosmetic: this file first had one guard for all six
// cases, which switched the fresh-key case off on an engine that can run it,
// under a reason that named fixed keys it does not use. Ed25519 is asked about
// in neither - no message of a handshake carries a signature.

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
  await skipWithoutFixedKeys(shell);
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

// split() derives k1/k2 from ck alone and never changes ck, so a second call
// would hand out a second Transport that reuses every nonce the first one
// will ever use - the same key and IV encrypting two different messages,
// silently. It must refuse. page.evaluate runs neither as a module nor in
// strict mode (Task 4), so "it threw" is captured into an explicit return
// value here and checked by the outer expect() - not inferred from the
// absence of a crash.
test("split() refuses a second call on the same handshake", async ({ shell }) => {
  await skipWithoutFixedKeys(shell);
  const got = await shell.evaluate(async (d) => {
    const n = await import("/assets/js/contracts/noise.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const prologue = b64decode(d.prologue);
    const init = await n.HandshakeKK.forTest(true, prologue,
      b64decode(d.initStatic.private), b64decode(d.respStatic.public), b64decode(d.initEphemeral.private));
    const resp = await n.HandshakeKK.forTest(false, prologue,
      b64decode(d.respStatic.private), b64decode(d.initStatic.public), b64decode(d.respEphemeral.private));
    const msg1 = await init.writeMessage1();
    await resp.readMessage1(msg1);
    const msg2 = await resp.writeMessage2();
    await init.readMessage2(msg2);
    await init.split();
    try {
      await init.split();
      return { threw: false };
    } catch (e) {
      return { threw: true, message: e.message };
    }
  }, v);
  expect(got.threw).toBe(true);
  expect(got.message).toBe("noise: split already called");
});

// The only path ever exercised elsewhere in this file is forTest(), which
// ships in the production module but exists for noise.json alone. Nothing
// proved that newKeypair(), of() and a real (non-fixed) ephemeral - the path
// NGC actually takes at runtime - agree with each other, end to end.
test("newKeypair(), of() and a fresh ephemeral complete a real handshake", async ({ shell }) => {
  await skipWithoutHandshake(shell);
  const got = await shell.evaluate(async () => {
    const n = await import("/assets/js/contracts/noise.js");
    const initStatic = await n.newKeypair();
    const respStatic = await n.newKeypair();
    const prologue = new TextEncoder().encode(n.PROLOGUE_SESSION);
    const init = await n.HandshakeKK.of(true, prologue, initStatic.privateKey, initStatic.publicRaw, respStatic.publicRaw);
    const resp = await n.HandshakeKK.of(false, prologue, respStatic.privateKey, respStatic.publicRaw, initStatic.publicRaw);
    const msg1 = await init.writeMessage1();
    await resp.readMessage1(msg1);
    const msg2 = await resp.writeMessage2();
    await init.readMessage2(msg2);
    const a = await init.split(), b = await resp.split();
    const plain = new TextEncoder().encode("hello from a fresh handshake");
    const sealed = await a.seal(plain);
    const opened = await b.open(sealed);
    return Array.from(opened);
  });
  expect(new TextDecoder().decode(new Uint8Array(got))).toBe("hello from a fresh handshake");
});

// n is a plain Number here, not Go's uint64: past 2**53 it can no longer
// represent every integer, so n++ can silently stop moving and two messages
// would share one key/IV pair. 2**53 real seal() calls to reach that state
// is not something a test can run, so this pokes sendState/recvState - plain
// public fields on the Transport a real split() returns, not a test seam -
// straight to the boundary instead.
test("seal() and open() refuse once the nonce counter is no longer exact", async ({ shell }) => {
  await skipWithoutFixedKeys(shell);
  const got = await shell.evaluate(async (d) => {
    const n = await import("/assets/js/contracts/noise.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const prologue = b64decode(d.prologue);
    const init = await n.HandshakeKK.forTest(true, prologue,
      b64decode(d.initStatic.private), b64decode(d.respStatic.public), b64decode(d.initEphemeral.private));
    const resp = await n.HandshakeKK.forTest(false, prologue,
      b64decode(d.respStatic.private), b64decode(d.initStatic.public), b64decode(d.respEphemeral.private));
    const msg1 = await init.writeMessage1();
    await resp.readMessage1(msg1);
    const msg2 = await resp.writeMessage2();
    await init.readMessage2(msg2);
    const a = await init.split();
    const msgOf = async (fn) => {
      try {
        await fn();
        return null;
      } catch (e) {
        return e.message;
      }
    };
    a.sendState.n = Number.MAX_SAFE_INTEGER;
    const sealMsg = await msgOf(() => a.seal(new Uint8Array([1, 2, 3])));
    a.recvState.n = Number.MAX_SAFE_INTEGER;
    const openMsg = await msgOf(() => a.open(new Uint8Array(20)));
    return { sealMsg, openMsg };
  }, v);
  expect(got.sealMsg).toBe("noise: nonce counter exhausted");
  expect(got.openMsg).toBe("noise: nonce counter exhausted");
});

// b64.js promises everywhere that what a caller hands over in the wrong
// shape is refused, never guessed (b64.js:6-7). seal()/open() did not: an
// ArrayBuffer has no .length, so "undefined > MAX_PLAINTEXT" is false and
// the guard was skipped - crypto.subtle happily takes a raw ArrayBuffer too,
// so the call succeeded anyway, over the wire limit. The same case also
// pins K7: a properly-typed Uint8Array one byte over MAX_PLAINTEXT must
// still be refused (shared/noisep/noisep_test.go:80).
test("seal() and open() reject anything that is not Uint8Array, and refuse a plaintext over MAX_PLAINTEXT", async ({ shell }) => {
  await skipWithoutFixedKeys(shell);
  const got = await shell.evaluate(async (d) => {
    const n = await import("/assets/js/contracts/noise.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const prologue = b64decode(d.prologue);
    const init = await n.HandshakeKK.forTest(true, prologue,
      b64decode(d.initStatic.private), b64decode(d.respStatic.public), b64decode(d.initEphemeral.private));
    const resp = await n.HandshakeKK.forTest(false, prologue,
      b64decode(d.respStatic.private), b64decode(d.initStatic.public), b64decode(d.respEphemeral.private));
    const msg1 = await init.writeMessage1();
    await resp.readMessage1(msg1);
    const msg2 = await resp.writeMessage2();
    await init.readMessage2(msg2);
    const a = await init.split();
    const msgOf = async (fn) => {
      try {
        await fn();
        return null;
      } catch (e) {
        return e.message;
      }
    };
    return {
      sealArrayBuffer: await msgOf(() => a.seal(new ArrayBuffer(16))),
      sealOverLimit: await msgOf(() => a.seal(new Uint8Array(n.MAX_PLAINTEXT + 1))),
      openArrayBuffer: await msgOf(() => a.open(new ArrayBuffer(16))),
    };
  }, v);
  expect(got.sealArrayBuffer).toBe("noise: not bytes");
  expect(got.sealOverLimit).toBe("noise: message too large");
  expect(got.openArrayBuffer).toBe("noise: not bytes");
});

// importPublic()'s catch used to swallow any failure and always retry as
// spki - even a real, unrelated failure, which the retry can then hide by
// succeeding for a different reason. crypto.subtle.importKey is patched to
// inject a failure no browser actually raises here (QuotaExceededError),
// deterministic unlike guessing which DOMException a given engine throws for
// which malformed input - and scoped to the exact call importPublic makes
// (format "raw", algorithm X25519) precisely because "raw" alone is too
// broad: hmac() (used by hkdf(), on every mixKey()) also imports "raw" key
// material, for HMAC, and a first attempt at this test that matched on
// format alone was intercepting that unrelated call instead and always
// passed, for the wrong reason (measured, not assumed - see the report). The
// real importKey still runs for every other call, so the rest of the
// handshake is genuine.
test("importPublic() only falls back for NotSupportedError and DataError, not any failure", async ({ shell }) => {
  await skipWithoutFixedKeys(shell);
  const got = await shell.evaluate(async (d) => {
    const realImportKey = crypto.subtle.importKey.bind(crypto.subtle);
    crypto.subtle.importKey = (format, ...rest) => {
      if (format === "raw" && rest[1] && rest[1].name === "X25519") {
        return Promise.reject(new DOMException("simulated failure", "QuotaExceededError"));
      }
      return realImportKey(format, ...rest);
    };
    try {
      const n = await import("/assets/js/contracts/noise.js");
      const { b64decode } = await import("/assets/js/contracts/b64.js");
      const prologue = b64decode(d.prologue);
      const init = await n.HandshakeKK.forTest(true, prologue,
        b64decode(d.initStatic.private), b64decode(d.respStatic.public), b64decode(d.initEphemeral.private));
      await init.writeMessage1();
      return { threw: false, name: null };
    } catch (e) {
      return { threw: true, name: e.name };
    } finally {
      crypto.subtle.importKey = realImportKey;
    }
  }, v);
  expect(got.threw).toBe(true);
  expect(got.name).toBe("QuotaExceededError");
});

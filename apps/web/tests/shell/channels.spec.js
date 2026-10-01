import { test, expect, skipWithoutLogin } from "../fixtures/page.js";

// The mock API runs beside the web server (playwright.config.js) and answers
// with the same modules the shell uses.
//
// Every case here needs a Noise session, and a Noise session needs X25519 and
// Ed25519. Playwright's WebKit has neither, so the whole file skips there -
// MEASURED by doing the work a login does, never decided by the engine's name.
// Every case here needs the desktop behind the login, so it is the login that
// is asked about (fixtures/page.js, skipWithoutLogin).
test.beforeEach(async ({ shell }) => {
  await skipWithoutLogin(shell);
});

test("a call channel returns its result", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const { connectForTest } = await import("/assets/js/net/testlink.js");
    const link = await connectForTest();
    const me = await link.call("ngc.desktop", "api", "me", null);
    link.close();
    return me;
  });
  expect(out.name).toBe("anna");
});

test("the shell never opens more channels than MaxSessionChannels", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const { connectForTest } = await import("/assets/js/net/testlink.js");
    const w = await import("/assets/js/contracts/wire.js");
    const link = await connectForTest();
    const open = [];
    for (let i = 0; i < w.MAX_SESSION_CHANNELS; i++) open.push(await link.openRaw("ngc.desktop", "api", "apps"));
    let refused = false;
    try { await link.openRaw("ngc.desktop", "api", "apps"); } catch { refused = true; }
    const count = link.channelCount();
    for (const c of open) c.close("ok");
    link.close();
    return { refused, count };
  });
  expect(out).toEqual({ refused: true, count: 64 });
});

test("a result over MaxCallResult closes the channel with E_TOOLARGE", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const { connectForTest } = await import("/assets/js/net/testlink.js");
    const link = await connectForTest();
    try {
      // The mock API sends 9 MiB for this action on purpose.
      await link.call("ngc.users", "api", "log", { limit: 500, beforeTime: 1 });
      return "none";
    } catch (e) {
      return e.code;
    } finally {
      link.close();
    }
  });
  expect(out).toBe("E_TOOLARGE");
});

test("an end-to-end channel carries data the API cannot read", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const { connectForTest } = await import("/assets/js/net/testlink.js");
    const link = await connectForTest();
    const ch = await link.openE2E("org.sorglos.terminal", "terminal", "shell", { cols: 80, rows: 24 });
    const seen = [];
    ch.onData((d) => seen.push(new TextDecoder().decode(d)));
    await ch.write(new TextEncoder().encode("cp -r\n"));
    await ch.end();
    const status = await ch.done();
    link.close();
    return { seen: seen.join(""), status: status.status, plaintextSeenByAPI: await link.mockSawPlaintext() };
  });
  expect(out.seen).toContain("cp -r");
  expect(out.status).toBe("ok");
  expect(out.plaintextSeenByAPI).toBe(false);
});

test("a result counts only when the channel ends with ok", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const { connectForTest } = await import("/assets/js/net/testlink.js");
    const link = await connectForTest();
    try {
      // The mock API sends data and then closes with E_EXIT.
      await link.call("ngc.store", "api", "sources", null, { failAfterData: true });
      return "none";
    } catch (e) {
      return e.code;
    } finally {
      link.close();
    }
  });
  expect(out).toBe("E_EXIT");
});

// THE TWO CASES BELOW ARE THE NONCE. An end-to-end channel seals inside
// itself, and until this was fixed nothing serialised that: shell/appframe.js
// doSend is a synchronous postMessage handler that calls
// Promise.resolve(s.write(b)) with no queue behind it, so two "send" messages
// of one app - or a "send" and an "end" - put two seals into ONE cipher state
// at once. CipherState.seal read this.n for the IV BEFORE its await and
// raised it AFTER, so both calls took the same number: one key, one IV, two
// plaintexts, which is the single failure AES-GCM does not survive. net/link.js
// says all of this in its own head and solves it for the transport of the
// LINK; the transport INSIDE a channel was not covered.
test("two overlapping writes on an end-to-end channel never take one nonce twice", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const { connectForTest } = await import("/assets/js/net/testlink.js");
    const link = await connectForTest();
    const ch = await link.openE2E("org.sorglos.terminal", "terminal", "shell", { cols: 80, rows: 24 });
    // The nonce every seal is ENTERED with, written down at the door. A
    // number that shows up twice here IS the reuse, not a symptom of it.
    const inner = ch.transport;
    const realSeal = inner.seal.bind(inner);
    const used = [];
    inner.seal = (plain) => {
      used.push(inner.sendState.n);
      return realSeal(plain);
    };
    const enc = new TextEncoder();
    // No awaiting in between: this is the shape appframe's doSend produces.
    await Promise.all([ch.write(enc.encode("AAAA")), ch.write(enc.encode("BBBB"))]);
    link.close();
    return { count: used.length, distinct: new Set(used).size };
  });
  expect(out.count).toBe(2);
  expect(out.distinct).toBe(2);
});

test("two overlapping writes arrive unfalsified and in order", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const { connectForTest } = await import("/assets/js/net/testlink.js");
    const link = await connectForTest();
    const ch = await link.openE2E("org.sorglos.terminal", "terminal", "shell", { cols: 80, rows: 24 });
    const seen = [];
    ch.onData((d) => seen.push(new TextDecoder().decode(d)));
    const enc = new TextEncoder();
    let status = "none", failed = null;
    try {
      await Promise.all([ch.write(enc.encode("AAAA")), ch.write(enc.encode("BBBB"))]);
      await ch.end();
      status = (await ch.done()).status;
    } catch (e) {
      // A reused nonce does not decrypt on the other side: the service ends
      // the channel instead of echoing, and that arrives here as a throw.
      failed = e && e.code ? e.code : String(e);
    }
    link.close();
    return { seen: seen.join(""), status, failed };
  });
  expect(out.failed).toBe(null);
  // The service is an echo, so both lines come back - and a Noise transport
  // reads in nonce order, so they come back in the order they were written.
  expect(out.seen).toBe("AAAABBBB");
  expect(out.status).toBe("ok");
});

// W2. Channel.take() put every piece into this.chunks for a result() that
// never comes on a stream, and nothing took them out again: a terminal that
// prints for an hour kept every byte of it in the tab. There is no number to
// invent here - a stream is unbounded by nature, and a total bound would cut
// off exactly the session the stream is for. The fault is the collecting, so
// the rule is that whoever reads does not have to collect. MAX_CALL_RESULT
// stays where it is, on result().
test("a channel with a listener does not keep what it has handed over", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const { connectForTest } = await import("/assets/js/net/testlink.js");
    const link = await connectForTest();
    const ch = await link.openE2E("org.sorglos.terminal", "terminal", "shell", { cols: 80, rows: 24 });
    const seen = [];
    ch.onData((d) => seen.push(new TextDecoder().decode(d)));
    const enc = new TextEncoder();
    // Five lines, the same five the task report measured.
    for (const line of ["one", "two", "three", "four", "five"]) {
      await ch.write(enc.encode(line));
    }
    for (let i = 0; i < 300 && seen.length < 5; i++) await new Promise((r) => setTimeout(r, 10));
    const held = ch.chunks.length, total = ch.total, limit = ch.limit;
    link.close();
    return { got: seen.join(""), seen: seen.length, held, total, limit: String(limit) };
  });
  // All five arrived, so all five were handed over.
  expect(out.seen).toBe(5);
  expect(out.got).toBe("onetwothreefourfive");
  // And none of them is still being held for a caller who never asks.
  expect(out.held).toBe(0);
  // The accounting is untouched: total still counts every byte, and a stream
  // still has no limit of its own - that is result()'s job, not take()'s.
  expect(out.total).toBe(19);
  expect(out.limit).toBe("Infinity");
});

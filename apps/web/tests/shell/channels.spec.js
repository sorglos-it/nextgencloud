import { test, expect } from "../fixtures/page.js";
import { hasSessionKeys } from "../fixtures/page.js";

// The mock API runs beside the web server (playwright.config.js) and answers
// with the same modules the shell uses.
//
// Every case here needs a Noise session, and a Noise session needs X25519 and
// Ed25519. Playwright's WebKit has neither, so the whole file skips there -
// MEASURED with the same two generateKey calls the shell itself makes
// (hasSessionKeys), never decided by the engine's name.
test.beforeEach(async ({ shell }) => {
  test.skip(!(await hasSessionKeys(shell)),
    "this engine has no Ed25519/X25519: no session keys, no session link");
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

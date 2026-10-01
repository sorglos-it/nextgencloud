import { test, expect, hasSessionKeys, signIn } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

// net/world.js: the seam between the surfaces, which speak in
// (app, service, name), and net/channel.js, which speaks in actions.
//
// WHAT IS MEASURED HERE AND WHAT IS NOT, said before the first case so nobody
// reads more into a green run than is in it:
//
//  - The TARGET of every action the shell may open is measured against the
//    stand-in it replaces, row by row, over a link that writes down what it
//    was asked to open. That is what "arrives at the same place" can mean
//    while the test API answers five of those actions and the stand-in two
//    dozen.
//  - The WAY is measured against the flow column of core.ACTIONS itself and
//    never against a list written here: a row added to the contract is walked
//    by these cases the day it is added.
//  - Over the REAL session to the mock API only the five actions it answers
//    can come back, and those are measured as bytes the contract takes.
//  - The control messages are fed into the real Link's own control path
//    (onControlFrame): parseControl, controlAllowed and core.parseNotice all
//    run. What is NOT measured is the decryption in front of it;
//    shell/channels.spec.js measures that for the data frames of the same
//    link.
const cases = vectors("trust.json");

function caseOf(name) {
  const c = cases.find((x) => x.name === name);
  if (c === undefined) throw new Error(`world.spec: trust.json has no case "${name}"`);
  return c;
}

// A valid chain of four versions from an anchor at 1, and the installation in
// it: the fixture shell/trustlist.spec.js is measured against.
const GOOD = "admin adds a user, user adds an own key, passkey admin re-signs";
const INSTALLATION = "EREREREREREREREREREREQ";
const ALARM = "Die Vertrauensliste dieser Anlage stimmt nicht. "
  + "NGC hat Sie sicherheitshalber abgemeldet.";

// plant puts the recording link into the page. It is a function handed to
// page.evaluate, which playwright injects as its own source - never a string
// this file would have to eval, which the shell may not do and a case of the
// shell may not teach.
//
// The recorder answers everything the helpers of net/channel.js ask of a
// channel and nothing else, and it writes down what it was asked to OPEN and
// what was written on the channel afterwards. So "what did the adapter send"
// is a measurement, and the adapter needs no hook of its own for it.
async function plant(shell) {
  await shell.evaluate(() => {
    window.__ngcRecorder = function recorder() {
      const opens = [];
      const link = {
        opens,
        notices: [],
        hints: [],
        onNotice(fn) { link.notices.push(fn); },
        onTrustHint(fn) { link.hints.push(fn); },
        async open(o) {
          const text = (b) => (b === null || b === undefined ? "" : new TextDecoder().decode(b));
          const rec = {
            app: o.app, service: o.service, action: o.action,
            params: text(o.params), task: text(o.taskBytes), confirm: text(o.confirmBytes),
            e2e: o.e2e === null || o.e2e === undefined ? "" : "set",
            data: [], ended: false, closed: "", took: 0,
          };
          opens.push(rec);
          return {
            firstGrant: async () => ({ credit: 1 << 20, handshake: null }),
            end: async () => { rec.ended = true; },
            write: async (b) => { rec.data.push(new TextDecoder().decode(b)); },
            close: async (code) => { rec.closed = code; },
            result: async (max) => { rec.took = max; return new TextEncoder().encode("{}"); },
          };
        },
      };
      return link;
    };
  });
}

// serve answers GET /api/v1/trust?from=N out of the fixture chain, the way
// shared/README.md ("Vor dem WebSocket") says the API does. visible is how
// many versions this API owns up to, so a case can let a newer one appear and
// then send the trust hint that is about it.
async function serve(shell, c, { visible = c.chain.length } = {}) {
  const state = { visible, from: [] };
  await shell.route(/\/api\/v1\/trust\?/, async (route) => {
    const from = Number(new URL(route.request().url()).searchParams.get("from"));
    state.from.push(from);
    const at = from - c.anchor.seq;
    if (at < 0 || at > state.visible) {
      // The CLOSE of wire.errorClose("E_NOTFOUND"), written out rather than
      // imported: playwright compiles a spec file as CommonJS and the
      // contracts under public/assets/js are modules, which is why
      // shell/trustlist.spec.js writes it out in the same place.
      await route.fulfill({
        status: 404, contentType: "application/json; charset=utf-8",
        body: JSON.stringify({ status: "error", code: "E_NOTFOUND", message: "Nicht gefunden." }),
      });
      return;
    }
    if (at === state.visible) { await route.fulfill({ status: 204 }); return; }
    const versions = c.chain.slice(at, state.visible);
    await route.fulfill({
      status: 200, contentType: "application/json; charset=utf-8",
      body: JSON.stringify({ versions, more: false }),
    });
  });
  await shell.route(/\/api\/v1\/logout$/, (route) => route.fulfill({ status: 204 }));
  await shell.exposeFunction("__ngcVisible", (n) => { state.visible = n; });
  await shell.exposeFunction("__ngcAsked", () => state.from.slice());
  return state;
}

test.describe("the adapter", () => {
  test("every action the shell may open goes to the same place as through the stand-in",
    async ({ shell }) => {
      test.skip(!(await hasSessionKeys(shell)),
        "no Ed25519 in this engine's Web Crypto: the stand-in cannot even build its world");
      await plant(shell);
      const got = await shell.evaluate(async () => {
        const core = await import("/assets/js/contracts/core.js");
        const { adapterFor } = await import("/assets/js/net/world.js");
        const { fakeWorld } = await import("/assets/js/shell/fake-link.js");
        const utf8 = (s) => new TextEncoder().encode(s);
        // THE ROWS COME OUT OF THE TABLE and never out of a list here, which
        // is the whole point of the flow column: core.shellOpens is the
        // contract's own answer to "may the shell open this one".
        const rows = core.ACTIONS.filter((a) => core.shellOpens(a));
        const calls = rows.filter((a) => a.flow !== "out");
        const outs = rows.filter((a) => a.flow === "out");
        const fake = (await fakeWorld()).link;
        const rec = window.__ngcRecorder();
        const mine = adapterFor(rec);
        const params = utf8('{"x":1}');
        const body = () => new Blob([utf8("abc")]).stream();
        for (const a of calls) {
          // What either side ANSWERS is not the question here - the stand-in
          // answers eight of these and the recorder none of them. Both write
          // the target down before they answer, which is what is compared.
          try { await fake.call(a.app, a.service, a.name, params, { stream: body() }); } catch { /* see above */ }
          try { await mine.call(a.app, a.service, a.name, params, { stream: body() }); } catch { /* see above */ }
        }
        // The one FlowOut row goes through open() on both sides. The stand-in
        // keeps no record of an open(), so what is measured there is that it
        // takes this row and refuses every other.
        const fakeOut = [];
        for (const a of outs) {
          try { await fake.open(a.app, a.service, a.name); fakeOut.push(`${a.app}|${a.service}|${a.name}`); } catch { /* refused */ }
          await mine.open(a.app, a.service, a.name, params);
        }
        let fakeTookACall = true;
        try { await fake.open(core.APP_STORE, core.SERVICE_API, core.ACT_INSTALLED); } catch { fakeTookACall = false; }
        return {
          rows: rows.length,
          outs: outs.length,
          theirs: window.__ngcSent.map((s) => `${s.app}|${s.service}|${s.name}`),
          ours: rec.opens.map((o) => `${o.app}|${o.service}|${o.action}`),
          wantedCalls: calls.map((a) => `${a.app}|${a.service}|${a.name}`),
          // The calls first and the FlowOut row after them, because that is
          // the order they were driven in - not the order of the table.
          wanted: [...calls, ...outs].map((a) => `${a.app}|${a.service}|${a.name}`),
          fakeOut,
          fakeTookACall,
        };
      });
      // The table is walked, not a handful of rows.
      expect(got.rows).toBeGreaterThan(20);
      expect(got.outs).toBe(1);
      // Row for row, in the order of the table: the stand-in and the adapter
      // are asked the same thing and aim at the same place.
      expect(got.theirs).toEqual(got.wantedCalls);
      expect(got.ours).toEqual(got.wanted);
      expect(got.fakeOut).toEqual(["ngc.desktop|api|ui"]);
      expect(got.fakeTookACall).toBe(false);
    });

  test("the flow column decides the way, and a row the shell never opens is refused",
    async ({ shell }) => {
      await plant(shell);
      const got = await shell.evaluate(async () => {
        const core = await import("/assets/js/contracts/core.js");
        const wire = await import("/assets/js/contracts/wire.js");
        const uitar = await import("/assets/js/contracts/uitar.js");
        const { adapterFor } = await import("/assets/js/net/world.js");
        const utf8 = (s) => new TextEncoder().encode(s);
        const rec = window.__ngcRecorder();
        const link = adapterFor(rec);
        const refused = async (fn) => {
          try { await fn(); return false; } catch { return true; }
        };
        const params = utf8('{"app":"de.example.fotos"}');
        // FlowCall: nothing is written on the channel, EOF goes out at once,
        // and the result is bounded by wire.MAX_CALL_RESULT.
        await link.call(core.APP_STORE, core.SERVICE_API, core.ACT_INSTALLED, params, null);
        const call = rec.opens[rec.opens.length - 1];
        // THREE ARGUMENTS AND NO MORE, which is how shell/desktop.js asks for
        // the app list: no params, no option bag. Neither may become a reason
        // to refuse.
        await link.call(core.APP_DESKTOP, core.SERVICE_API, core.ACT_APPS);
        const bare = rec.opens[rec.opens.length - 1];
        // FlowIn: the body travels as DATA and not in the OPEN.
        await link.call(core.APP_STORE, core.SERVICE_API, core.ACT_UPLOAD, params,
          { stream: new Blob([utf8("ein paket")]).stream() });
        const flowIn = rec.opens[rec.opens.length - 1];
        // A task displaces the params of the OPEN: only the signed ones count.
        await link.call(core.APP_STORE, core.SERVICE_CONTROLLER, core.ACT_UNINSTALL, params,
          { taskBytes: utf8('{"t":1}'), confirmBytes: utf8('{"c":1}') });
        const task = rec.opens[rec.opens.length - 1];
        // FlowOut through open(): the listeners are hung up after the call
        // came back, exactly as shell/appframe.js hangs them up, and the
        // answer may not have been handed over before that.
        const out = await link.open(core.APP_DESKTOP, core.SERVICE_API, core.ACT_UI, params);
        const ui = rec.opens[rec.opens.length - 1];
        const end = await new Promise((ok) => {
          const seen = [];
          out.onData = (b) => seen.push(new TextDecoder().decode(b));
          out.onClose = (status) => ok({ status, seen });
        });
        return {
          call: { data: call.data, ended: call.ended, took: call.took, params: call.params },
          bare: { app: bare.app, service: bare.service, action: bare.action,
            params: bare.params, task: bare.task, confirm: bare.confirm, ended: bare.ended },
          flowIn: { data: flowIn.data, params: flowIn.params, ended: flowIn.ended },
          task: { params: task.params, task: task.task, confirm: task.confirm },
          ui: { action: ui.action, took: ui.took, status: end.status, seen: end.seen },
          maxCallResult: wire.MAX_CALL_RESULT,
          maxArchive: uitar.MAX_UI_TOTAL + uitar.MAX_ENTRIES * uitar.BLOCK,
          // The ways that are not open: a FlowOut row as a call, a FlowCall
          // row as an open, a row the shell never opens at all
          // (ngc.users/controller/trust), and an action that stands in no row
          // - an app's own service, which has to be an end-to-end channel to
          // its controller and is none of this adapter's business.
          outAsCall: await refused(() =>
            link.call(core.APP_DESKTOP, core.SERVICE_API, core.ACT_UI, params, null)),
          callAsOpen: await refused(() =>
            link.open(core.APP_STORE, core.SERVICE_API, core.ACT_INSTALLED, params)),
          neverOpened: await refused(() =>
            link.call(core.APP_USERS, core.SERVICE_CONTROLLER, core.ACT_TRUST, params,
              { stream: new Blob([utf8("x")]).stream() })),
          noRow: await refused(() => link.call("org.sorglos.terminal", "terminal", "shell", null, null)),
          // None of the four refusals opened a channel: five went out, and
          // five is what the five ways above asked for.
          opened: rec.opens.length,
        };
      });
      expect(got.call).toEqual({
        data: [], ended: true, took: got.maxCallResult, params: '{"app":"de.example.fotos"}',
      });
      expect(got.bare).toEqual({
        app: "ngc.desktop", service: "api", action: "apps",
        params: "", task: "", confirm: "", ended: true,
      });
      expect(got.flowIn).toEqual({
        data: ["ein paket"], params: '{"app":"de.example.fotos"}', ended: true,
      });
      expect(got.task).toEqual({ params: "", task: '{"t":1}', confirm: '{"c":1}' });
      expect(got.ui).toEqual({
        action: "ui", took: got.maxArchive, status: "ok", seen: ["{}"],
      });
      expect(got.outAsCall).toBe(true);
      expect(got.callAsOpen).toBe(true);
      expect(got.neverOpened).toBe(true);
      expect(got.noRow).toBe(true);
      expect(got.opened).toBe(5);
    });
});

test.describe("the real session", () => {
  test.beforeEach(async ({ shell }) => {
    test.skip(!(await hasSessionKeys(shell)),
      "no session keys in this engine's Web Crypto: there is no session to adapt");
  });

  test("the actions the test API answers come back as bytes the contract takes",
    async ({ shell }) => {
      const got = await shell.evaluate(async () => {
        const core = await import("/assets/js/contracts/core.js");
        const { adapterFor } = await import("/assets/js/net/world.js");
        const { connectForTest } = await import("/assets/js/net/testlink.js");
        const t = await connectForTest();
        const link = adapterFor(t.link);
        // The five the mock answers (tests/mock/world.js, results()). Every
        // one of them is read by the contract that owns the answer, so a byte
        // that did not arrive shows up here and not three layers away.
        const want = [
          [core.APP_DESKTOP, core.ACT_ME],
          [core.APP_DESKTOP, core.ACT_APPS],
          [core.APP_STORE, core.ACT_SOURCES],
          [core.APP_USERS, core.ACT_LIST],
          [core.APP_USERS, core.ACT_NODES],
        ];
        const out = [];
        for (const [app, name] of want) {
          const a = core.lookupByKey(app, name);
          const bytes = await link.call(a.app, a.service, a.name, core.encodeParams(a, null), null);
          // Caught rather than thrown, so an answer that is no longer bytes
          // fails the assertion below instead of the page.evaluate: a case
          // that dies on the way says less than one that says what it saw.
          let read = false;
          try { read = core.parseResult(a, bytes) !== null; } catch { read = false; }
          out.push({ key: `${a.app}/${a.name}`, bytes: bytes instanceof Uint8Array, read });
        }
        t.close();
        return out;
      });
      expect(got).toEqual([
        { key: "ngc.desktop/me", bytes: true, read: true },
        { key: "ngc.desktop/apps", bytes: true, read: true },
        { key: "ngc.store/sources", bytes: true, read: true },
        { key: "ngc.users/list", bytes: true, read: true },
        { key: "ngc.users/nodes", bytes: true, read: true },
      ]);
    });

  test("a notice of the real link reaches every listener as the bytes it is", async ({ shell }) => {
    const got = await shell.evaluate(async () => {
      const wire = await import("/assets/js/contracts/wire.js");
      const core = await import("/assets/js/contracts/core.js");
      const { adapterFor } = await import("/assets/js/net/world.js");
      const { connectForTest } = await import("/assets/js/net/testlink.js");
      const t = await connectForTest();
      const link = adapterFor(t.link);
      const seen = [];
      // TWO listeners, because the real Link holds exactly one: an adapter
      // that handed its own one straight on would quietly drop the first.
      link.onNotice((b) => seen.push(b));
      link.onNotice((b) => seen.push(b));
      const notice = new TextEncoder()
        .encode(JSON.stringify({ level: "warn", text: "Platte fast voll", app: "" }));
      await t.link.onControlFrame({
        type: wire.TYPE_DATA, channel: wire.CONTROL_CHANNEL,
        payload: wire.encodeControl({ t: wire.CTL_NOTIFY, b: notice }),
      });
      const read = seen.map((b) => core.parseNotice(b));
      t.close();
      return {
        listeners: seen.length,
        bytes: seen.every((b) => b instanceof Uint8Array),
        text: read.map((n) => n.text),
        level: read.map((n) => n.level),
      };
    });
    expect(got).toEqual({
      listeners: 2, bytes: true,
      text: ["Platte fast voll", "Platte fast voll"],
      level: ["warn", "warn"],
    });
  });
});

test.describe("the trust list of the world", () => {
  test("a trust hint moves the list, and refresh answers with a number", async ({ shell }) => {
    test.skip(!(await hasSessionKeys(shell)),
      "no Ed25519 in this engine's Web Crypto: no version of this chain can be verified");
    const c = caseOf(GOOD);
    const state = await serve(shell, c, { visible: 1 });
    await plant(shell);
    const got = await shell.evaluate(async ([raw, installation]) => {
      const { b64decode } = await import("/assets/js/contracts/b64.js");
      const { realWorld } = await import("/assets/js/net/world.js");
      const rec = window.__ngcRecorder();
      const world = await realWorld({
        config: null, anchor: { seq: raw.seq, hash: b64decode(raw.hash) },
        session: { inst: installation }, link: rec,
      });
      // startTrust hung the hint up on the link the world was handed: one
      // listener, and it is that link's.
      const hints = rec.hints.length;
      const first = world.trust.current().list.seq;
      // A hint that is not higher asks nobody and answers the NUMBER in
      // force - not a version, not an object.
      const before = (await window.__ngcAsked()).length;
      const same = await world.trust.refresh(first);
      const asked = (await window.__ngcAsked()).length;
      // Now the API owns four versions, and the hint about it carries the
      // number. It is answered without anybody awaiting it, so the
      // measurement waits for the version it is about.
      await window.__ngcVisible(4);
      for (const fn of rec.hints) fn({ seq: 4 });
      for (let i = 0; i < 300 && world.trust.current().list.seq < 4; i++) {
        await new Promise((ok) => setTimeout(ok, 10));
      }
      return {
        hints, first, same, kindOfSame: typeof same, askedNothing: asked === before,
        after: world.trust.current().list.seq,
        installation: world.trust.current().list.installation,
        // The session travels through untouched, the release key is the one
        // the initial setup will pin, and the link is the adapter and not the
        // raw one.
        session: world.session.inst,
        releaseKey: world.releaseKey,
        adapter: world.link !== rec && typeof world.link.call === "function"
          && typeof world.link.open === "function" && typeof world.link.onNotice === "function",
      };
    }, [{ seq: c.anchor.seq, hash: c.anchor.hash }, INSTALLATION]);
    expect(got).toEqual({
      hints: 1, first: 1, same: 1, kindOfSame: "number", askedNothing: true,
      after: 4, installation: INSTALLATION, session: INSTALLATION,
      releaseKey: null, adapter: true,
    });
    // Twice and no more: once for the anchor, and once from the version after
    // the one in force - after the hint and never before it. This API serves
    // the rest in one page, so there is no third request;
    // shell/trustlist.spec.js measures the paging itself.
    expect(state.from).toEqual([1, 2]);
  });

  test("a chain that does not fit the anchor takes the session with it", async ({ shell }) => {
    test.skip(!(await hasSessionKeys(shell)),
      "no Ed25519 in this engine's Web Crypto: no version of this chain can be verified");
    const c = caseOf(GOOD);
    await serve(shell, c);
    await plant(shell);
    const got = await shell.evaluate(async ([raw, installation]) => {
      const { b64decode } = await import("/assets/js/contracts/b64.js");
      const { realWorld } = await import("/assets/js/net/world.js");
      const rec = window.__ngcRecorder();
      let closed = 0;
      rec.close = () => { closed++; };
      // The anchor of this chain with one byte turned round: everything else
      // about the chain is sound, so what refuses it is the anchor and
      // nothing else.
      const hash = b64decode(raw.hash);
      hash[0] ^= 1;
      let threw = false;
      try {
        await realWorld({
          config: null, anchor: { seq: raw.seq, hash },
          session: { inst: installation }, link: rec,
        });
      } catch {
        threw = true;
      }
      return { threw, closed, alarm: document.getElementById("trustalarm") !== null };
    }, [{ seq: c.anchor.seq, hash: c.anchor.hash }, INSTALLATION]);
    // Nothing is handed back, the alarm stands, and the socket does not stay
    // open for a session that has just been logged out.
    expect(got).toEqual({ threw: true, closed: 1, alarm: true });
  });
});

test.describe("the boot", () => {
  test("without the stand-in the shell opens the trust list on the session it just got",
    async ({ shell }) => {
      test.skip(!(await hasSessionKeys(shell)),
        "no session keys in this engine's Web Crypto: the boot stops before the login");
      // The stand-in is what every other case of this suite still eats
      // (tests/fixtures/page.js, signIn). Without it the shell takes the real
      // way: the session it just opened stays open and the trust list is
      // opened against the anchor this test server serves - which cannot fit
      // the chain the mock builds with keys of its own
      // (shell/trustlist.spec.js says why), so the alarm is what has to stand
      // here. THAT is the measurement: with the stand-in there is no alarm at
      // all, because nothing opens a trust list on the real session.
      await signIn(shell, { standIn: false });
      await expect(shell.locator("#trustalarm")).toBeVisible();
      await expect(shell.locator("#trustalarmtext")).toHaveText(ALARM);
      await expect(shell.locator("#taskbar")).toHaveCount(0);
    });
});

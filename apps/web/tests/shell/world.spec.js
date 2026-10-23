import { test, expect, signIn, skipWithoutLogin, skipWithoutSessionKeys, skipWithoutSignatures }
  from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";
import { PASSWORD, USER_NAME } from "../fixtures/logins.js";

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

// THE FOUR SENTENCES a refusal of net/world.js puts in front of a person, each
// written out HERE and not imported: net/world.js keeps them to itself, and a
// case that read the constant it is checking would pass for every wording there
// could be - including the same wording twice. They are the whole point of the
// four words, so they are pinned character for character, and `distinct` below
// pins that they really are four and not two.
//
// NO_CONTROLLER_KEY is shell/trustlist.js's NOT_TRUSTED, on purpose: it is the
// same fact, and one fact gets one wording. shell/trustlist.spec.js pins it
// there as well, which is what makes a change to it fall in two places.
const SAID = {
  node: "Diese App sagt nicht, auf welchem Rechner ihr Dienst läuft.",
  "session-key": "Diese Anmeldung kann den eigenen Dienst einer App nicht erreichen. "
    + "Bitte die Seite neu laden.",
  "trust-list": "NGC hat die Vertrauensliste dieser Anlage nicht geöffnet. "
    + "Bitte die Seite neu laden.",
  "controller-key": "Dieser Server steht nicht in der Vertrauensliste.",
};

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
            // The ORDER of what happened on this channel, so "did it report a
            // channel before there was one" is a measurement: the open, the
            // first grant and the EOF all have to have happened before the
            // caller is handed anything.
            order: ["open"],
            data: [], ended: false, closed: "", took: 0,
          };
          opens.push(rec);
          return {
            firstGrant: async () => { rec.order.push("firstGrant"); return { credit: 1 << 20, handshake: null }; },
            end: async () => { rec.order.push("end"); rec.ended = true; },
            write: async (b) => { rec.order.push("write"); rec.data.push(new TextDecoder().decode(b)); },
            // The code is written down, not swallowed: a close() that takes a
            // code and drops it is the defect this records.
            close: async (code) => { rec.order.push("close"); rec.closed = code === undefined ? "(none)" : code; },
            result: async (max) => { rec.order.push("result"); rec.took = max; return new TextEncoder().encode("{}"); },
          };
        },
      };
      return link;
    };
  });
}

// plantService puts ONE helper into the page: a real session, and on top of it
// the adapter an app's own service goes through. Four cases need it, and a
// login written out four times is a login that drifts four ways.
//
// WHAT IS REAL IN IT and what stands in, so nobody reads more into these cases
// than is in them:
//   - REAL: the login (certificate, proof, cookie, API certificate checked),
//     the Noise session, the X25519 pair auth/login.js built, the controller's
//     Noise key out of the list this world serves, and the whole end-to-end
//     handshake against tests/mock/api.js playing that controller.
//   - A STAND-IN: the one method of shell/trustlist.js's TrustStore the adapter
//     asks for. The chain this world serves is signed by a key that stands in no
//     version of it, so it cannot be opened against an anchor at all - the boot
//     case at the end of this file is about exactly that - and controllerKey's
//     own rules are measured where they live (shell/trustlist.spec.js).
async function plantService(shell, [user, password]) {
  await shell.evaluate(async ([who, pw]) => {
    window.__ngcAppLink = async () => {
      const trust = await import("/assets/js/contracts/trust.js");
      const http = await import("/assets/js/net/http.js");
      const { adapterFor } = await import("/assets/js/net/world.js");
      const { login } = await import("/assets/js/auth/login.js");
      const h = await login({ name: who, password: pw });
      const chain = await http.trustChain(1);
      const list = await trust.parseList(chain.versions[0].payload);
      const ctl = list.nodes.find((n) => n.kind === trust.NODE_CONTROLLER);
      return {
        h,
        node: ctl.id,
        link: adapterFor(h.link, {
          trust: { controllerKey: () => ctl.noise },
          sessionDH: h.sessionDH,
        }),
        // THE SAME ADAPTER WITH THE SESSION IN IT, which is what realWorld
        // hands over (net/world.js). It is a second one and not a replacement
        // of the first, because "this adapter cannot sign a task" is a case of
        // its own: the two differ in exactly one field.
        signing: adapterFor(h.link, {
          trust: { controllerKey: () => ctl.noise, current: () => ({ list }) },
          sessionDH: h.sessionDH,
          session: h.session,
        }),
      };
    };
    // What the app channels of this session carried: the three numbers the
    // mock counts (refused for want of a task, opened with one, with a
    // confirmation). Asked of the mock and not of the page, so no case has to
    // believe a flag the page set about its own work.
    window.__ngcMockState = async () => {
      const res = await fetch("/api/v1/mock?ask=task", { cache: "no-store", credentials: "same-origin" });
      const [taskAsked, taskSeen, confirmSeen] = (await res.text()).split(" ").map(Number);
      return { taskAsked, taskSeen, confirmSeen };
    };
    // What the API saw. It is asked and not read: the mock answers one
    // character, so there is nothing to parse.
    window.__ngcSawPlaintext = async () => {
      const res = await fetch("/api/v1/mock", { cache: "no-store", credentials: "same-origin" });
      return (await res.text()) === "1";
    };
    // collect hangs the two listeners up the way shell/appframe.js does: on the
    // stream open() handed back, in the turn right after it came back, and never
    // before. Everything the service says lands in `pieces` IN ORDER, and the
    // one close it gets lands in `closed` - so "did the app hear it, and when"
    // is a measurement and not a guess.
    window.__ngcCollect = {
      collect(s) {
        const c = {
          pieces: [],
          closed: null,
          // until waits for what the other side still has to send, with a bound:
          // three seconds is far more than a round trip to a mock in the same
          // process, and a case that waits forever says nothing at all.
          async until(ready) {
            for (let i = 0; i < 300 && !ready(); i++) {
              await new Promise((ok) => setTimeout(ok, 10));
            }
          },
        };
        s.onData = (b) => c.pieces.push(new TextDecoder().decode(b));
        s.onClose = (status, message) => { c.closed = { status, message }; };
        return c;
      },
    };
  }, [user, password]);
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
      await skipWithoutSignatures(shell);
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
        // THE PARAMS NAME AN APP THAT HAS A USER INTERFACE, and that is not
        // decoration. Since Task 25 the stand-in ships the archives of SEVERAL
        // apps and reads `app` out of the params to know which one
        // ngc.desktop/ui is about - so made-up params make the STAND-IN refuse
        // the one FlowOut row, and this comparison would then be measuring the
        // stand-in's own refusal instead of the two ways. The way itself did not
        // change with 25: ngc.desktop/api/ui is FlowOut and goes through open()
        // on both sides, as it did before.
        //
        // Which app is asked is taken from the stand-in's OWN app list, the way
        // the frame takes it - an app with an entry page is an app that has a
        // user interface - so no id is written down here to rot.
        const appsAction = core.lookupByKey(core.APP_DESKTOP, core.ACT_APPS);
        const installed = core.parseResult(appsAction,
          await fake.call(appsAction.app, appsAction.service, appsAction.name, null, null));
        const withUI = installed.find((x) => x.entry !== "");
        if (withUI === undefined) throw new Error("world.spec: the stand-in ships no app with a user interface");
        const params = core.encodeParams(core.lookupByKey(core.APP_DESKTOP, core.ACT_UI), { app: withUI.id });
        // What the preparation above already wrote into the stand-in's record;
        // the comparison starts after it.
        const base = window.__ngcSent.length;
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
        // FOUR ARGUMENTS ON BOTH SIDES, because that is what shell/appframe.js
        // hands over for an archive (uiBytes); the fifth, the node, belongs to
        // an app's own service and is measured in its own case.
        const fakeOut = [];
        for (const a of outs) {
          try { await fake.open(a.app, a.service, a.name, params); fakeOut.push(`${a.app}|${a.service}|${a.name}`); } catch { /* refused */ }
          await mine.open(a.app, a.service, a.name, params);
        }
        let fakeTookACall = true;
        try {
          await fake.open(core.APP_STORE, core.SERVICE_API, core.ACT_INSTALLED, params);
        } catch {
          fakeTookACall = false;
        }
        return {
          rows: rows.length,
          outs: outs.length,
          theirs: window.__ngcSent.slice(base).map((s) => `${s.app}|${s.service}|${s.name}`),
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

  test("the node of an app's own service arrives, and the refusal names the piece that is missing",
    async ({ shell }) => {
      await plant(shell);
      const got = await shell.evaluate(async () => {
        const { adapterFor } = await import("/assets/js/net/world.js");
        const utf8 = (s) => new TextEncoder().encode(s);
        const rec = window.__ngcRecorder();
        // ONE ARGUMENT, so this adapter has no world: no session key pair and
        // no trust list. That is not a hypothetical shape - every other case of
        // this file builds the adapter that way, and the shell builds it with
        // both (net/world.js, realWorld). What is measured here is therefore
        // the half of the refusals that needs no list to decide.
        const link = adapterFor(rec);
        const params = utf8('{"cols":80}');
        // What shell/appframe.js hands over for an app's own service, in both
        // doors: the app id, the service and the action the manifest declared,
        // the params, and the NODE in the option bag. Task 25 built that fifth
        // argument; before this round it fell on the floor.
        const APP = "org.sorglos.terminal", SERVICE = "terminal", ACTION = "shell";
        // `said` is the SENTENCE and not "there was one". A refusal nobody can
        // read is a refusal that helps nobody, and two refusals with one
        // sentence are one refusal.
        const refusal = async (fn) => {
          try {
            await fn();
            return "took it";
          } catch (e) {
            return { name: e?.name ?? "", missing: e?.missing ?? "", said: e?.message ?? "" };
          }
        };
        return {
          // WITH the node: what this adapter is missing is the session's own
          // DH key pair, and the refusal has to say exactly that.
          callWithNode: await refusal(() => link.call(APP, SERVICE, ACTION, params, { node: "pi-1" })),
          openWithNode: await refusal(() => link.open(APP, SERVICE, ACTION, params, { node: "pi-1" })),
          // WITHOUT it: a different refusal, so a bridge that stopped sending
          // the node cannot hide behind the one about the key.
          callNoNode: await refusal(() => link.call(APP, SERVICE, ACTION, params, null)),
          openNoNode: await refusal(() => link.open(APP, SERVICE, ACTION, params)),
          // A node that is no node is as good as none.
          callBadNode: await refusal(() => link.call(APP, SERVICE, ACTION, params, { node: "" })),
          // Nothing of this opened a channel: an end-to-end channel is the only
          // right one here, and an ordinary one would let the API read along.
          opened: rec.opens.length,
        };
      });
      const key = { name: "WorldError", missing: "session-key", said: SAID["session-key"] };
      const noNode = { name: "WorldError", missing: "node", said: SAID.node };
      expect(got.callWithNode).toEqual(key);
      expect(got.openWithNode).toEqual(key);
      expect(got.callNoNode).toEqual(noNode);
      expect(got.openNoNode).toEqual(noNode);
      expect(got.callBadNode).toEqual(noNode);
      expect(got.opened).toBe(0);
    });

  test("with both pieces in hand the door opens, and a node the verified list does not know is refused on its own word",
    async ({ shell }) => {
      await skipWithoutSessionKeys(shell);
      await plant(shell);
      const got = await shell.evaluate(async () => {
        const noise = await import("/assets/js/contracts/noise.js");
        const { NotTrusted } = await import("/assets/js/shell/trustlist.js");
        const { adapterFor } = await import("/assets/js/net/world.js");
        const utf8 = (s) => new TextEncoder().encode(s);
        const APP = "org.sorglos.terminal", SERVICE = "terminal", ACTION = "shell";
        const params = utf8('{"cols":80}');
        // The two pieces Task 28 laid: the session's own DH pair, as
        // auth/keys.js makes it, and the one method of the trust list the
        // adapter asks for. controllerKey's OWN rules - a controller, out of
        // the verified list, with a 32-byte Noise key - are measured where they
        // live (shell/trustlist.spec.js); what stands in for it here throws the
        // real NotTrusted, so this case measures what the adapter does with
        // its two answers and nothing else.
        const sessionDH = await noise.newKeypair();
        const controller = await noise.newKeypair();
        const trust = {
          controllerKey: (id) => {
            if (id !== "ctl-1") throw new NotTrusted();
            return controller.publicRaw;
          },
        };
        const rec = window.__ngcRecorder();
        const link = adapterFor(rec, { trust, sessionDH });
        // The same adapter MINUS the list. A list that is not there is not the
        // same fact as a node a list does not carry, so it may not get the same
        // word and may not get the same sentence.
        const listless = adapterFor(window.__ngcRecorder(), { sessionDH });
        // And one whose list answers with something that is NOT NotTrusted: a
        // fault of this shell, which may not come out as a sentence about the
        // server. It has to travel on as what it is.
        const broken = adapterFor(window.__ngcRecorder(), {
          sessionDH,
          trust: { controllerKey: () => { throw new RangeError("world.spec: a fault of the shell"); } },
        });
        const refusal = async (fn) => {
          try {
            await fn();
            return "took it";
          } catch (e) {
            return { name: e?.name ?? "", missing: e?.missing ?? "", said: e?.message ?? "" };
          }
        };
        // A NODE THE LIST DOES NOT CARRY. It is its own case and it gets its own
        // word: without one it would hide behind the refusal about the key, and
        // a shell that says "no session key" where the truth is "this server is
        // not in your trust list" sends somebody looking in the wrong place.
        const unknown = await refusal(() => link.call(APP, SERVICE, ACTION, params, { node: "pi-99" }));
        const unknownOpen = await refusal(() => link.open(APP, SERVICE, ACTION, params, { node: "pi-99" }));
        // Still first, and still its own word: a node that is no node is
        // refused before anybody is asked about it.
        const noNode = await refusal(() => link.call(APP, SERVICE, ACTION, params, null));
        const noList = await refusal(() => listless.call(APP, SERVICE, ACTION, params, { node: "ctl-1" }));
        const noListOpen = await refusal(() => listless.open(APP, SERVICE, ACTION, params, { node: "ctl-1" }));
        const fault = await refusal(() => broken.call(APP, SERVICE, ACTION, params, { node: "ctl-1" }));
        const beforeTheDoor = rec.opens.length;
        // A NODE IT DOES CARRY: the door opens. The recorder's first grant
        // carries no Noise message 2, so channel.openE2E closes the channel
        // with E_PROTOCOL right after - which is how far "it got that far" can
        // be measured without a controller, and the OPEN it had already written
        // down says WHAT went out.
        const wentOut = await refusal(() => link.call(APP, SERVICE, ACTION, params, { node: "ctl-1" }));
        const sent = rec.opens[rec.opens.length - 1];
        return {
          unknown, unknownOpen, noNode, noList, noListOpen, fault, beforeTheDoor,
          // No WorldError any more: what comes back is the CHANNEL's error,
          // which is what shell/appframe.js turns into E_UNREACHABLE for the
          // app. A refusal of this adapter would carry a `missing`; this does
          // not, and that is the difference between "NGC cannot" and "the
          // channel broke".
          wentOutName: wentOut.name, wentOutMissing: wentOut.missing,
          sent: {
            app: sent.app, service: sent.service, action: sent.action,
            params: sent.params, task: sent.task, confirm: sent.confirm, e2e: sent.e2e,
          },
          // One channel for the one call that got past the door, and not one
          // for any of the three refusals.
          opens: rec.opens.length,
        };
      });
      const notTrusted = {
        name: "WorldError", missing: "controller-key", said: SAID["controller-key"],
      };
      const noList = { name: "WorldError", missing: "trust-list", said: SAID["trust-list"] };
      expect(got.unknown).toEqual(notTrusted);
      expect(got.unknownOpen).toEqual(notTrusted);
      expect(got.noNode).toEqual({ name: "WorldError", missing: "node", said: SAID.node });
      expect(got.noList).toEqual(noList);
      expect(got.noListOpen).toEqual(noList);
      // A fault of the shell is NOT dressed up as a statement about the server:
      // it keeps its own name, it carries no `missing`, and its text is its own.
      expect(got.fault).toEqual({
        name: "RangeError", missing: "", said: "world.spec: a fault of the shell",
      });
      // FOUR WORDS, FOUR SENTENCES. Two refusals sharing one sentence would be
      // one refusal as far as anybody reading the screen is concerned, and this
      // is the line that would fall for it.
      expect(new Set(Object.values(SAID)).size).toBe(Object.keys(SAID).length);
      expect(got.beforeTheDoor).toBe(0);
      expect(got.wentOutName).toBe("ChannelError");
      expect(got.wentOutMissing).toBe("");
      expect(got.sent).toEqual({
        app: "org.sorglos.terminal", service: "terminal", action: "shell",
        params: '{"cols":80}', task: "", confirm: "", e2e: "set",
      });
      expect(got.opens).toBe(1);
    });

  test("open hands nothing over before the channel stands, and close really closes it with its code",
    async ({ shell }) => {
      await plant(shell);
      const got = await shell.evaluate(async () => {
        const core = await import("/assets/js/contracts/core.js");
        const wire = await import("/assets/js/contracts/wire.js");
        const { adapterFor } = await import("/assets/js/net/world.js");
        const utf8 = (s) => new TextEncoder().encode(s);
        const rec = window.__ngcRecorder();
        const link = adapterFor(rec);
        const params = utf8('{"app":"org.sorglos.terminal"}');
        const out = await link.open(core.APP_DESKTOP, core.SERVICE_API, core.ACT_UI, params);
        const ch = rec.opens[rec.opens.length - 1];
        // By the time open() came back the channel was opened, its first grant
        // was there, the EOF had gone out AND the bound was set - Channel.result
        // is what sets it, and a bound set one turn later would let the first
        // chunks past it. ngc.js hands an app its stream before any of that has
        // happened, because a postMessage bridge has nothing to wait for; here
        // there is, so we wait.
        const whenOpenReturned = ch.order.slice();
        // close(code) is what shell/appframe.js calls (closeQuietly), and the
        // code is NOT decoration: the real channel builds its CLOSE with
        // wire.errorClose(code), and without one wire refuses the frame.
        await out.close(wire.CODES.PROTOCOL);
        return {
          whenOpenReturned,
          closed: ch.closed,
          order: ch.order.slice(),
          protocol: wire.CODES.PROTOCOL,
        };
      });
      expect(got.whenOpenReturned).toEqual(["open", "firstGrant", "end", "result"]);
      expect(got.closed).toBe(got.protocol);
      expect(got.order).toContain("close");
    });
});

test.describe("the real session", () => {
  test.beforeEach(async ({ shell }) => {
    await skipWithoutLogin(shell);
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

  test("an app's own service runs end to end on the key pair the login carried through",
    async ({ shell }) => {
      await plantService(shell, [USER_NAME, PASSWORD]);
      const got = await shell.evaluate(async () => {
        const utf8 = (s) => new TextEncoder().encode(s);
        // THE REAL LOGIN, and not a key pair this case made: the pair it is
        // about is the one auth/login.js built for the Noise session, and its
        // public half stands in the client certificate the API bound this
        // session to. tests/mock/api.js plays the controller of the end-to-end
        // handshake with THAT half (onOpen, state.clientDH), so a channel that
        // stands here is a channel that stood on the login's own key pair and
        // on no other. A login that stopped carrying it through cannot make
        // this case green by accident.
        const { h, link, node } = await window.__ngcAppLink();
        const APP = "org.sorglos.terminal", SERVICE = "terminal", ACTION = "shell";
        // open() and not call(), because that is what an app's own service is:
        // shell/appframe.js gives the terminal a channel that stays open and
        // writes every line into it. The "shell" service of this world echoes
        // what it is given (tests/mock/api.js, innerData).
        const s = await link.open(APP, SERVICE, ACTION, utf8('{"cols":80}'), { node });
        const line = "cp -r\n";
        const ended = await new Promise((ok) => {
          const seen = [];
          // The listeners are hung up AFTER open() came back, exactly as
          // shell/appframe.js hangs them up - and the echo may not have been
          // handed over before that.
          s.onData = (b) => {
            seen.push(new TextDecoder().decode(b));
            Promise.resolve(s.end()).catch(() => {});
          };
          s.onClose = (status, message) => ok({ seen, status, message });
          Promise.resolve(s.write(utf8(line))).catch(() => ok({ seen, status: "write failed", message: "" }));
        });
        // The one question the API itself has to answer. The mock watches every
        // frame for the bytes the service was given: if the cleartext ever
        // stood in one, end to end bought nothing.
        const sawPlaintext = await window.__ngcSawPlaintext();
        h.link.close();
        return {
          // What the login carried: an X25519 pair whose private half cannot be
          // read out again, not even by the shell.
          keyKind: h.sessionDH?.privateKey?.algorithm?.name ?? "",
          extractable: h.sessionDH?.privateKey?.extractable ?? true,
          publicLength: h.sessionDH?.publicRaw?.length ?? 0,
          node,
          seen: ended.seen, status: ended.status, message: ended.message,
          sawPlaintext,
        };
      });
      expect(got).toEqual({
        keyKind: "X25519", extractable: false, publicLength: 32,
        node: "ctl-1", seen: ["cp -r\n"], status: "ok", message: "",
        sawPlaintext: false,
      });
    });

  test("a service that speaks first is heard, before the app has written anything",
    async ({ shell }) => {
      await plantService(shell, [USER_NAME, PASSWORD]);
      const got = await shell.evaluate(async () => {
        const utf8 = (s) => new TextEncoder().encode(s);
        const { h, link, node } = await window.__ngcAppLink();
        const { collect } = window.__ngcCollect;
        // The "greet" service of this world speaks the moment it has started -
        // which by the contract is the moment it has decrypted the shell's first
        // inner message, the one channel.openE2E sends by itself. So the app
        // writes NOTHING and still has to hear something, which is what a
        // terminal's prompt is. net/world.js's appStream is the only thing
        // between that frame and the app; a piece it dropped would be gone with
        // nothing to see anywhere.
        const s = await link.open("org.sorglos.terminal", "terminal", "greet",
          utf8('{"cols":80}'), { node });
        const c = collect(s);
        await c.until(() => c.pieces.length >= 1);
        const greeting = c.pieces.slice();
        // And now a line of the app's own: its echo has to arrive AFTER the
        // greeting, so the order a service speaks in is the order the app hears.
        await s.write(utf8("ls\n"));
        await c.until(() => c.pieces.length >= 2);
        await s.end();
        await c.until(() => c.closed !== null);
        const sawPlaintext = await window.__ngcSawPlaintext();
        h.link.close();
        return { greeting, pieces: c.pieces.slice(), closed: c.closed, sawPlaintext };
      });
      expect(got.greeting).toEqual(["willkommen$ "]);
      expect(got.pieces).toEqual(["willkommen$ ", "ls\n"]);
      expect(got.closed).toEqual({ status: "ok", message: "" });
      expect(got.sawPlaintext).toBe(false);
    });

  test("a service that is over before it starts tells the app instead of leaving it waiting",
    async ({ shell }) => {
      await plantService(shell, [USER_NAME, PASSWORD]);
      const got = await shell.evaluate(async () => {
        const utf8 = (s) => new TextEncoder().encode(s);
        const { h, link, node } = await window.__ngcAppLink();
        const { collect } = window.__ngcCollect;
        // The "over" service ends the channel the moment it has started and says
        // nothing at all. THE CHANNEL IS STILL HANDED OVER - the ending is the
        // answer to the shell's own first inner message, so it is a later frame -
        // and what matters is that the app is TOLD: shell/appframe.js forgets the
        // number on that close, and an app that never got one would wait on a
        // number that is gone for as long as its window is open.
        const s = await link.open("org.sorglos.terminal", "terminal", "over",
          utf8('{"cols":80}'), { node });
        const c = collect(s);
        await c.until(() => c.closed !== null);
        const count = h.link.channelCount();
        h.link.close();
        return { stream: typeof s === "object" && typeof s.write === "function",
          pieces: c.pieces.slice(), closed: c.closed, count };
      });
      // open() handed a stream over, the app heard nothing, and it was told the
      // channel is over - with the status the service itself ended on.
      expect(got.stream).toBe(true);
      expect(got.pieces).toEqual([]);
      expect(got.closed).toEqual({ status: "ok", message: "" });
      expect(got.count).toBe(0);
    });

  test("an app's own service answers one question through call(), and the number is free again",
    async ({ shell }) => {
      await plantService(shell, [USER_NAME, PASSWORD]);
      const got = await shell.evaluate(async () => {
        const utf8 = (s) => new TextEncoder().encode(s);
        const { h, link, node } = await window.__ngcAppLink();
        // THE WAY shell/appframe.js's doCall GOES, and until this round no case
        // went down it: the question stands in the params of the OPEN, the inner
        // end and the EOF go out at once, and what comes back are the bytes of
        // the service. The echo service of this world was given nothing to echo,
        // so the answer is empty - and empty is an ANSWER: it only exists
        // because the channel ended with ok, which is what Channel.result
        // demands before it hands anything over at all.
        const bytes = await link.call("org.sorglos.terminal", "terminal", "shell",
          utf8('{"cols":80}'), { node });
        // The number has to come free again. A call that left the channel open
        // would spend one of the session's channels per question the app asks.
        for (let i = 0; i < 300 && h.link.channelCount() > 0; i++) {
          await new Promise((ok) => setTimeout(ok, 10));
        }
        const count = h.link.channelCount();
        const sawPlaintext = await window.__ngcSawPlaintext();
        h.link.close();
        return { bytes: bytes instanceof Uint8Array, length: bytes.length, count, sawPlaintext };
      });
      expect(got).toEqual({ bytes: true, length: 0, count: 0, sawPlaintext: false });
    });

  test("close(code) really ends an app's channel, and nothing reaches the caller after it",
    async ({ shell }) => {
      await plantService(shell, [USER_NAME, PASSWORD]);
      const got = await shell.evaluate(async () => {
        const wire = await import("/assets/js/contracts/wire.js");
        const utf8 = (s) => new TextEncoder().encode(s);
        const { h, link, node } = await window.__ngcAppLink();
        const { collect } = window.__ngcCollect;
        const open = () => link.open("org.sorglos.terminal", "terminal", "shell",
          utf8('{"cols":80}'), { node });
        const free = async () => {
          for (let i = 0; i < 300 && h.link.channelCount() > 0; i++) {
            await new Promise((ok) => setTimeout(ok, 10));
          }
          return h.link.channelCount();
        };
        // WITH A CODE, which is what shell/appframe.js passes (closeQuietly).
        const s = await open();
        const c = collect(s);
        await s.write(utf8("hallo\n"));
        await c.until(() => c.pieces.length >= 1);
        await s.close(wire.CODES.PROTOCOL);
        const afterCode = await free();
        // The channel is over, so the link has told everyone waiting on it -
        // and the caller, who asked for this close, hears NOTHING. It has
        // already been told by the one who called close() (appframe's
        // doCloseStream answers the app itself).
        await new Promise((ok) => setTimeout(ok, 200));
        const quiet = { pieces: c.pieces.length, closed: c.closed };
        // WITHOUT one: "ok" is the word for "I am done and this is no error",
        // and a close() that passed nothing on would be refused by wire.
        const s2 = await open();
        const c2 = collect(s2);
        await s2.close();
        const afterNoCode = await free();
        h.link.close();
        return { afterCode, quiet, afterNoCode, closed2: c2.closed };
      });
      // Both numbers came free: the CLOSE really went out and was answered. A
      // close() that only set a flag would leave them taken for the session.
      expect(got.afterCode).toBe(0);
      expect(got.afterNoCode).toBe(0);
      // One piece - the echo - and no close for the caller that closed it.
      expect(got.quiet).toEqual({ pieces: 1, closed: null });
      expect(got.closed2).toBe(null);
    });

  test("a step the controller wants confirmed, with no session that could sign it",
    async ({ shell }) => {
      await plantService(shell, [USER_NAME, PASSWORD]);
      const got = await shell.evaluate(async () => {
        const wire = await import("/assets/js/contracts/wire.js");
        const utf8 = (s) => new TextEncoder().encode(s);
        const { h, link, node } = await window.__ngcAppLink();
        // WHAT THE CONTROLLER REALLY ANSWERS for a step that wants confirming:
        // E_CONFIRM, as the answer to the OPEN, and no service is started
        // (measured on the controller side, plan 3 task 11: access.Check ->
        // task.Check refuses without a signed task once the app needs root or
        // trust.List.NeedsConfirm says yes). What this case pins is that the
        // code REACHES the caller: until 23.10.2026 it did not - a CLOSE before
        // the controller's own inner end was always E_PROTOCOL, however the
        // CLOSE itself read (net/channel.js, Channel.truncated) - and the one
        // code the shell has to act on was the one that could not get through.
        //
        // The action "needsconfirm" of this world is refused whatever comes
        // with it, so what is measured here is the code and nothing else; the
        // case below is the one where a signed task gets through.
        const caught = async (fn) => {
          try {
            await fn();
            return "took it";
          } catch (e) {
            return { name: e?.name ?? "", code: e?.code ?? "", said: e?.message ?? "" };
          }
        };
        const ACT = "needsconfirm";
        const onCall = await caught(() => link.call("org.sorglos.terminal", "terminal", ACT,
          utf8('{"cols":80}'), { node }));
        const onOpen = await caught(() => link.open("org.sorglos.terminal", "terminal", ACT,
          utf8('{"cols":80}'), { node }));
        for (let i = 0; i < 300 && h.link.channelCount() > 0; i++) {
          await new Promise((ok) => setTimeout(ok, 10));
        }
        const count = h.link.channelCount();
        h.link.close();
        return { onCall, onOpen, count, confirm: wire.CODES.CONFIRM, said: wire.message(wire.CODES.CONFIRM) };
      });
      // WHAT ARRIVES, and it is two findings in one line. The refusal was
      // RECOGNISED - before 23.10.2026 a CLOSE before the controller's own
      // inner end was always E_PROTOCOL, however the CLOSE read, so E_CONFIRM
      // could not be told from a broken channel and this is what says it now
      // can. And the shell then tried to sign a task and had nothing to sign
      // with, because THIS adapter was built without a session: what the
      // person gets is that sentence and not a code, which is the whole point
      // of the sentences in net/world.js.
      expect(got.onCall).toEqual({
        name: "WorldError", code: "",
        said: "Diese Anmeldung kann den Auftrag für diese App nicht unterschreiben. Bitte neu anmelden.",
      });
      expect(got.onOpen).toEqual(got.onCall);
      // Whatever the code, the channel is not left behind.
      expect(got.count).toBe(0);
      // And the contract does have the code and a sentence for it; the case
      // below is where both get through and a task goes out.
      expect(got.confirm).toBe("E_CONFIRM");
      expect(got.said).toBe("Bitte erneut bestätigen.");
    });

  test("an app that needs a signed task is opened with one, and the person is asked",
    async ({ shell }) => {
      await plantService(shell, [USER_NAME, PASSWORD]);
      // THE OPEN IS STARTED AND NOT AWAITED HERE, because the person is asked
      // in the middle of it: the question is a modal dialog in the same page,
      // so an evaluate that waited for the open would never come back and the
      // dialog would never be answered. The promise is parked on window and
      // taken up again below - which is also the order it happens in for a
      // person: the window opens, the question comes, the service starts.
      await shell.evaluate(async () => {
        const utf8 = (t) => new TextEncoder().encode(t);
        const { h, signing: link, node } = await window.__ngcAppLink();
        window.__ngcHeld = h;
        // "needstask" is the action of this world that is refused WITHOUT a
        // task and runs WITH one - the real case of an app whose manifest says
        // needs:["root"] (tests/mock/api.js, ACT_TASK).
        window.__ngcOpening = link.open("org.sorglos.terminal", "terminal", "needstask",
          utf8('{"cols":80}'), { node });
        // A rejection nobody is waiting for yet is an unhandled rejection, and
        // the page fixture fails a case over one. It is taken up below.
        window.__ngcOpening.catch(() => {});
      });
      // The question, answered the way every other case of this suite answers
      // it: the password of this world, out of the world and not written here
      // a second time.
      const box = shell.getByRole("dialog", { name: "Bitte bestätigen" });
      await expect(box).toBeVisible();
      await box.locator("input[type=password]").fill(PASSWORD);
      await box.getByRole("button", { name: "Bestätigen", exact: true }).click();
      await expect(box).toHaveCount(0);
      const got = await shell.evaluate(async () => {
        const c = window.__ngcCollect.collect(await window.__ngcOpening);
        await c.until(() => c.pieces.length > 0);
        const asked = await window.__ngcMockState();
        window.__ngcHeld.link.close();
        return { pieces: c.pieces, asked };
      });
      // The service really started, so the SECOND open came off: nothing of
      // this world answers before a handshake that stands, and the handshake
      // of the second open is made over a prologue that hashes the task.
      expect(got.pieces).toEqual(["willkommen$ "]);
      // One refused open, then one with a task. The first is the price this
      // way pays and it is measured, not estimated.
      expect(got.asked.taskAsked).toBe(1);
      expect(got.asked.taskSeen).toBe(1);
      // And the question was put to the person: this world's trust list wants
      // the heaviest kind of step confirmed, and the proof travelled with the
      // task.
      expect(got.asked.confirmSeen).toBe(1);
    });

  test("an app channel is opened with no task at all where the server asks for none",
    async ({ shell }) => {
      await plantService(shell, [USER_NAME, PASSWORD]);
      const got = await shell.evaluate(async () => {
        const utf8 = (s) => new TextEncoder().encode(s);
        const { h, signing: link, node } = await window.__ngcAppLink();
        const c = window.__ngcCollect.collect(
          await link.open("org.sorglos.terminal", "terminal", "greet", utf8('{"cols":80}'), { node }),
        );
        await c.until(() => c.pieces.length > 0);
        const asked = await window.__ngcMockState();
        h.link.close();
        return { pieces: c.pieces, asked };
      });
      // The ordinary way is untouched: one open, no task, no question, and the
      // service speaks. A shell that signed a task for every app channel would
      // fail this case.
      expect(got.pieces).toEqual(["willkommen$ "]);
      expect(got.asked.taskAsked ?? 0).toBe(0);
      expect(got.asked.taskSeen ?? 0).toBe(0);
      expect(got.asked.confirmSeen ?? 0).toBe(0);
    });

  test("params beside a task are refused before anything leaves", async ({ shell }) => {
    await plant(shell);
    const got = await shell.evaluate(async () => {
      const { openE2E } = await import("/assets/js/net/channel.js");
      const utf8 = (t) => new TextEncoder().encode(t);
      // A link that would record an OPEN if one ever reached it. Nothing may:
      // the refusal stands before the handshake, so no key and no server are
      // needed for this case - and that is the point of where it stands.
      const opens = [];
      const link = { open: async (o) => { opens.push(o); throw new Error("never"); } };
      const caught = async (fn) => {
        // The MESSAGE and not the name: b64.BadInput carries no name of its
        // own (class BadInput extends Error {}), so every one of them reads
        // "Error" - measured here, which is why this case says what it says.
        try { await fn(); return "took it"; } catch (e) { return e?.message ?? "?"; }
      };
      const both = await caught(() => openE2E(link, {
        app: "org.sorglos.terminal", service: "terminal", action: "shell",
        params: utf8('{"cols":80}'), taskBytes: utf8("{}"), node: "ctl-1",
        controllerKey: new Uint8Array(32), sessionDH: null,
      }));
      return { both, opens: opens.length };
    });
    // The two answers this door must not pick between are params it sends
    // against the contract and params it hashes and drops (net/channel.js).
    expect(got.both).toBe("channel: params beside a task");
    // And nothing went out: the refusal is before the first frame.
    expect(got.opens).toBe(0);
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
    await skipWithoutSignatures(shell);
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
    await skipWithoutSignatures(shell);
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
      await skipWithoutLogin(shell);
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

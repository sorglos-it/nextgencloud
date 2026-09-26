import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

// The trust list in the shell: the anchor it hangs from, the chain it
// follows, and the alarm when the two do not fit together.
//
// WHERE THE CHAINS COME FROM. Not written here and not from the mock API:
// shared/testdata/trust.json is the common truth of Go and JavaScript, and it
// already holds a valid four-version chain and every way a chain can break.
// The cases below serve THOSE bytes as the answer of GET /api/v1/trust and
// measure what the shell makes of them - the same documents the Go side is
// measured against, over the shell's own fetch, its own parser and its own
// verification.
//
// WHY page.route AND NOT THE MOCK API. The mock builds its world with keys it
// makes at start-up (tests/mock/world.js), so its version 1 differs with every
// run and no fixed anchor can ever match it - and its signer stands in no
// list at all, which trust.open refuses at version 1 on purpose. A route hands
// the shell the fixture's own bytes instead, and it counts every request while
// it is at it: "was this fetched twice" is then a measurement and not a hook.
const cases = vectors("trust.json");

function caseOf(name) {
  const c = cases.find((x) => x.name === name);
  if (c === undefined) throw new Error(`trustlist.spec: trust.json has no case "${name}"`);
  return c;
}

// A valid chain of four versions from an anchor at 1, and the installation
// and the controller that stand in it.
const GOOD = "admin adds a user, user adds an own key, passkey admin re-signs";
const INSTALLATION = "EREREREREREREREREREREQ";
const CONTROLLER = "ctl-1";
const ALARM = "Die Vertrauensliste dieser Anlage stimmt nicht. "
  + "NGC hat Sie sicherheitshalber abgemeldet.";

// Does this engine's Web Crypto know Ed25519? Every key in trust.json is
// Ed25519, so an engine without it can check no signature in any chain here
// and every case below would measure the engine and not the shell. Probed
// with a real key out of the fixture, the way trust.spec.js does it, never
// decided by the engine's name.
async function hasEd25519(shell) {
  return shell.evaluate(async (c) => {
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const payload = JSON.parse(new TextDecoder().decode(b64decode(c.chain[0].payload)));
    let pub = null;
    for (const u of payload.users) {
      for (const k of u.keys) {
        if (k.kind === "password") pub = k.pub;
      }
    }
    try {
      await crypto.subtle.importKey("raw", b64decode(pub), { name: "Ed25519" }, false, ["verify"]);
      return true;
    } catch {
      return false;
    }
  }, caseOf(GOOD));
}

// One box per page, so a case may set the world up twice (once before and
// once after a reload) without hanging a second route or exposing a function
// that is already there.
const wired = new WeakMap();

// serve answers GET /api/v1/trust?from=N out of a fixture chain, exactly as
// shared/README.md ("Vor dem WebSocket") says the API does: the versions from
// N on, at most `size` of them, "more" while any are left, 204 where N is one
// past the newest and 404 E_NOTFOUND past that.
//
// visible is how many versions this API owns up to. A case raises it with
// __ngcVisible() to make a new version appear, which is what a trust hint is
// about; __ngcDead() breaks the connection instead.
async function serve(shell, c, { size = 64, visible = c.chain.length } = {}) {
  const state = {
    chain: c.chain, first: c.anchor.seq, size, visible, all: c.chain.length,
    dead: false, from: [], logouts: 0,
  };
  const box = wired.get(shell);
  if (box !== undefined) {
    box.state = state;
    return state;
  }
  const held = { state };
  wired.set(shell, held);
  await shell.route(/\/api\/v1\/trust\?/, async (route) => {
    const s = held.state;
    const from = Number(new URL(route.request().url()).searchParams.get("from"));
    s.from.push(from);
    if (s.dead) { await route.abort(); return; }
    const at = from - s.first;
    if (at < 0 || at > s.visible) {
      // The CLOSE of wire.errorClose("E_NOTFOUND"), written out rather than
      // imported: playwright compiles a spec file as CommonJS, and the
      // contracts under public/assets/js carry no package.json that would
      // make them modules for it (measured: "Cannot use import statement
      // outside a module" on wire.js). The mock API, which node starts on its
      // own, imports them and is the place that holds them to their contract.
      await route.fulfill({
        status: 404, contentType: "application/json; charset=utf-8",
        body: JSON.stringify({ status: "error", code: "E_NOTFOUND", message: "Nicht gefunden." }),
      });
      return;
    }
    if (at === s.visible) { await route.fulfill({ status: 204 }); return; }
    const versions = s.chain.slice(at, Math.min(at + s.size, s.visible));
    await route.fulfill({
      status: 200, contentType: "application/json; charset=utf-8",
      body: JSON.stringify({ versions, more: at + versions.length < s.visible }),
    });
  });
  // The alarm ends the session itself, so a case can see it happen.
  await shell.route(/\/api\/v1\/logout$/, async (route) => {
    held.state.logouts++;
    await route.fulfill({ status: 204 });
  });
  await shell.exposeFunction("__ngcVisible", () => { held.state.visible = held.state.all; });
  await shell.exposeFunction("__ngcDead", () => { held.state.dead = true; });
  await shell.exposeFunction("__ngcAsked", () => held.state.from.slice());
  await shell.exposeFunction("__ngcForget", () => { held.state.from = []; });
  return state;
}

// The anchor travels into the page as the text it is in the fixture; the
// shell is handed bytes, the way net/config.js hands it bytes.
const anchorOf = (c) => ({ seq: c.anchor.seq, hash: c.anchor.hash });

test.describe("the chain", () => {
  test.beforeEach(async ({ shell }) => {
    test.skip(!(await hasEd25519(shell)),
      "no Ed25519 in this engine's Web Crypto: it can verify no version of any chain here");
  });

  test("the chain is followed from the anchor, page by page, to the newest version", async ({ shell }) => {
    const c = caseOf(GOOD);
    const state = await serve(shell, c, { size: 1 });
    const got = await shell.evaluate(async (raw) => {
      const { b64decode } = await import("/assets/js/contracts/b64.js");
      const { TrustStore } = await import("/assets/js/shell/trustlist.js");
      const s = new TrustStore();
      const seq = await s.bootstrap({ seq: raw.seq, hash: b64decode(raw.hash) });
      const now = s.current();
      return { seq, inForce: now.list.seq, installation: now.list.installation };
    }, anchorOf(c));
    expect(got).toEqual({ seq: 4, inForce: 4, installation: INSTALLATION });
    // One page per version, and every page asked for the one after the last
    // it was given: nothing fetched twice, nothing skipped.
    expect(state.from).toEqual([1, 2, 3, 4]);
  });

  test("a version whose bytes were changed raises the alarm, logs out and leaves no list", async ({ shell }) => {
    const c = caseOf("tampered");
    const state = await serve(shell, c, { visible: 1 });
    const got = await shell.evaluate(async (raw) => {
      const { b64decode } = await import("/assets/js/contracts/b64.js");
      const { TrustStore } = await import("/assets/js/shell/trustlist.js");
      const s = new TrustStore();
      const alarms = [];
      s.onAlarm((text) => alarms.push(text));
      const before = await s.bootstrap({ seq: raw.seq, hash: b64decode(raw.hash) });
      // The API now owns up to a second version - and it is one nobody signed.
      await window.__ngcVisible();
      await s.refresh(2).catch(() => {});
      return { before, alarms, after: s.current() };
    }, anchorOf(c));
    expect(got.before).toBe(1);
    expect(got.alarms).toEqual([ALARM]);
    // Nothing is in force any more: an app that asks for the list gets none.
    expect(got.after).toBeNull();
    expect(state.logouts).toBe(1);
  });

  test("a hint that names a version the shell already has asks nothing", async ({ shell }) => {
    const c = caseOf(GOOD);
    const state = await serve(shell, c);
    const got = await shell.evaluate(async (raw) => {
      const { b64decode } = await import("/assets/js/contracts/b64.js");
      const { TrustStore } = await import("/assets/js/shell/trustlist.js");
      const s = new TrustStore();
      await s.bootstrap({ seq: raw.seq, hash: b64decode(raw.hash) });
      const asked = (await window.__ngcAsked()).length;
      // Both name versions that are already in force.
      const same = await s.refresh(4);
      const older = await s.refresh(1);
      return { asked, same, older, after: (await window.__ngcAsked()).length };
    }, anchorOf(c));
    expect(got.same).toBe(4);
    expect(got.older).toBe(4);
    // Not one request more than the boot itself made.
    expect(got.after).toBe(got.asked);
    expect(state.from).toEqual([1]);
  });

  test("two hints at once follow the chain once and raise no alarm", async ({ shell }) => {
    const c = caseOf(GOOD);
    const state = await serve(shell, c, { size: 1, visible: 1 });
    const got = await shell.evaluate(async (raw) => {
      const { b64decode } = await import("/assets/js/contracts/b64.js");
      const { TrustStore } = await import("/assets/js/shell/trustlist.js");
      const s = new TrustStore();
      const alarms = [];
      s.onAlarm((t) => alarms.push(t));
      await s.bootstrap({ seq: raw.seq, hash: b64decode(raw.hash) });
      await window.__ngcVisible();
      // Two hints in one turn of the event loop. Whoever reads the number of
      // the version in force BEFORE the first await and works with it after
      // fetches the same versions twice - and a version the shell already has
      // looks from the inside exactly like a chain that broke.
      // Caught rather than let out: without the queue this rejects, and a
      // case that dies at the call measures nothing. This way the two
      // assertions below say WHAT went wrong instead of the run saying that
      // something did.
      const both = await Promise.all([s.refresh(4), s.refresh(4)]).catch((e) => [String(e), String(e)]);
      return { both, alarms, seq: s.current()?.list?.seq ?? 0 };
    }, anchorOf(c));
    expect(got.alarms).toEqual([]);
    expect(got.both).toEqual([4, 4]);
    expect(got.seq).toBe(4);
    // 1 for the boot, then 2, 3 and 4 once each: the chain is walked once.
    expect(state.from).toEqual([1, 2, 3, 4]);
  });

  test("a new version tells the shell, with the number that is now in force", async ({ shell }) => {
    const c = caseOf(GOOD);
    await serve(shell, c, { visible: 1 });
    const got = await shell.evaluate(async (raw) => {
      const { b64decode } = await import("/assets/js/contracts/b64.js");
      const { TrustStore } = await import("/assets/js/shell/trustlist.js");
      const s = new TrustStore();
      const seen = [];
      s.onChange((v) => seen.push(v.list.seq));
      await s.bootstrap({ seq: raw.seq, hash: b64decode(raw.hash) });
      await window.__ngcVisible();
      await s.refresh(4);
      // A hint about nothing new must tell nobody.
      await s.refresh(4);
      return seen;
    }, anchorOf(c));
    // Once for the version the boot put in force, once for the one that came
    // after it - and not a third time for the hint that brought nothing.
    expect(got).toEqual([1, 4]);
  });

  for (const name of ["wrong anchor", "missing version", "old version again", "unknown signer",
    "wrong installation", "user makes herself admin"]) {
    test(`a chain that breaks ("${name}") raises the alarm`, async ({ shell }) => {
      const c = caseOf(name);
      const state = await serve(shell, c);
      const got = await shell.evaluate(async (raw) => {
        const { b64decode } = await import("/assets/js/contracts/b64.js");
        const { TrustStore } = await import("/assets/js/shell/trustlist.js");
        const s = new TrustStore();
        const alarms = [];
        s.onAlarm((t) => alarms.push(t));
        const seq = await s.bootstrap({ seq: raw.seq, hash: b64decode(raw.hash) }).catch(() => -1);
        return { seq, alarms, list: s.current() };
      }, anchorOf(c));
      expect(got.seq).toBe(-1);
      expect(got.alarms).toEqual([ALARM]);
      expect(got.list).toBeNull();
      expect(state.logouts).toBe(1);
    });
  }

  test("a connection that breaks is no alarm and changes nothing", async ({ shell }) => {
    const c = caseOf(GOOD);
    const state = await serve(shell, c, { visible: 1 });
    const got = await shell.evaluate(async (raw) => {
      const { b64decode } = await import("/assets/js/contracts/b64.js");
      const { TrustStore } = await import("/assets/js/shell/trustlist.js");
      const s = new TrustStore();
      const alarms = [];
      s.onAlarm((t) => alarms.push(t));
      await s.bootstrap({ seq: raw.seq, hash: b64decode(raw.hash) });
      await window.__ngcDead();
      const failed = await s.refresh(4).then(() => false, () => true);
      return { failed, alarms, seq: s.current()?.list?.seq ?? 0 };
    }, anchorOf(c));
    // A trust list nobody could fetch is a trust list that did not change.
    // Logging a person out over a dropped connection would make every hiccup
    // look like an attack, and the list that stays in force was verified.
    expect(got.failed).toBe(true);
    expect(got.alarms).toEqual([]);
    expect(got.seq).toBe(1);
    expect(state.logouts).toBe(0);
  });

  test("the verified version survives a reload and only what is missing is fetched", async ({ shell }) => {
    const c = caseOf(GOOD);
    const first = await serve(shell, c);
    await shell.evaluate(async (raw) => {
      const { b64decode } = await import("/assets/js/contracts/b64.js");
      const { TrustStore } = await import("/assets/js/shell/trustlist.js");
      await new TrustStore().bootstrap({ seq: raw.seq, hash: b64decode(raw.hash) });
    }, anchorOf(c));
    expect(first.from).toEqual([1]);

    await shell.reload();
    const again = await serve(shell, c);
    const got = await shell.evaluate(async ({ raw, inst }) => {
      const { b64decode } = await import("/assets/js/contracts/b64.js");
      const { TrustStore } = await import("/assets/js/shell/trustlist.js");
      const s = new TrustStore();
      const kept = await s.load(inst);
      const seq = await s.bootstrap({ seq: raw.seq, hash: b64decode(raw.hash) });
      return { keptSeq: kept === null ? 0 : kept.list.seq, seq };
    }, { raw: anchorOf(c), inst: INSTALLATION });
    expect(got).toEqual({ keptSeq: 4, seq: 4 });
    // Only what was missing: one question about the version after the newest,
    // answered with "there is none".
    expect(again.from).toEqual([5]);
  });

  test("a remembered version from another anchor is not used", async ({ shell }) => {
    const c = caseOf(GOOD);
    await serve(shell, c);
    const got = await shell.evaluate(async ({ raw, inst }) => {
      const { b64decode } = await import("/assets/js/contracts/b64.js");
      const { TrustStore } = await import("/assets/js/shell/trustlist.js");
      const anchor = { seq: raw.seq, hash: b64decode(raw.hash) };
      await new TrustStore().bootstrap(anchor);
      // The web controller was paired anew: the same installation, another
      // anchor. What was remembered under the old one says nothing about it.
      const other = { seq: anchor.seq, hash: b64decode(raw.hash) };
      other.hash[3] ^= 1;
      const s = new TrustStore();
      const kept = await s.load(inst);
      await window.__ngcForget();
      const broke = await s.bootstrap(other).then(() => false, () => true);
      return { keptSeq: kept === null ? 0 : kept.list.seq, broke, from: await window.__ngcAsked() };
    }, { raw: anchorOf(c), inst: INSTALLATION });
    expect(got.keptSeq).toBe(4);
    // The chain is walked again from the new anchor - not carried on from a
    // version that was verified against a different one - and it does not fit.
    expect(got.from).toEqual([1]);
    expect(got.broke).toBe(true);
  });

  test("a controller the list does not carry gets no key", async ({ shell }) => {
    const c = caseOf(GOOD);
    await serve(shell, c);
    const got = await shell.evaluate(async ({ raw, node }) => {
      const { b64decode } = await import("/assets/js/contracts/b64.js");
      const { TrustStore } = await import("/assets/js/shell/trustlist.js");
      const s = new TrustStore();
      await s.bootstrap({ seq: raw.seq, hash: b64decode(raw.hash) });
      const known = s.controllerKey(node);
      let refused = "";
      try {
        s.controllerKey("pi-99");
      } catch (e) {
        refused = e.message;
      }
      return { known: known.length, refused, store: s.storeOf("global")?.id ?? "", none: s.storeOf("nope") };
    }, { raw: anchorOf(c), node: CONTROLLER });
    expect(got.known).toBe(32);
    expect(got.refused).toBe("Dieser Server steht nicht in der Vertrauensliste.");
    expect(got.store).toBe("global");
    expect(got.none).toBeNull();
  });
});

test("a new version empties the user interfaces the shell had already proven", async ({ shell }) => {
  const cleared = await shell.evaluate(async () => {
    const { startDesktop } = await import("/assets/js/shell/desktop.js");
    let tell = null;
    const trust = { current: () => null, onChange: (fn) => { tell = fn; } };
    const link = { call: async () => { throw new Error("no link in this case"); }, onNotice() {} };
    const d = startDesktop(document.getElementById("root"), { link, trust });
    d.uiCache.put("org.sorglos.terminal", "1.0.0", [{ path: "ui/index.html", data: new Uint8Array(3) }]);
    const before = d.uiCache.get("org.sorglos.terminal", "1.0.0") !== null;
    if (tell === null) return { before, wired: false, after: true };
    tell({ list: { seq: 2 } });
    return { before, wired: true, after: d.uiCache.get("org.sorglos.terminal", "1.0.0") !== null };
  });
  // A user interface was proven against the list that was in force then. A
  // new version may take a store's key away, so nothing proven under the old
  // one counts any more.
  expect(cleared).toEqual({ before: true, wired: true, after: false });
});

test("the alarm says what is the matter and cannot be clicked away", async ({ shell }) => {
  await shell.evaluate(async () => {
    const { showTrustAlarm } = await import("/assets/js/shell/trustlist.js");
    showTrustAlarm();
  });
  const box = shell.getByRole("alertdialog");
  await expect(box).toContainText(
    "Die Vertrauensliste dieser Anlage stimmt nicht. NGC hat Sie sicherheitshalber abgemeldet.");
  // It says what to do, and without one word of jargon.
  await expect(box).toContainText("Bitte geben Sie hier kein Passwort mehr ein");
  // There is no way out of it: nothing to press, and Escape does nothing. An
  // alarm that can be clicked away is an alarm that gets clicked away.
  await expect(box.locator("button")).toHaveCount(0);
  await shell.keyboard.press("Escape");
  await shell.locator("#trustalarm").click({ force: true, position: { x: 4, y: 4 } });
  await expect(box).toBeVisible();
});

test("without an anchor the shell says the installation is not finished", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const { anchorFor, rememberAnchor, NOT_SET_UP } = await import("/assets/js/shell/trustlist.js");
    const { b64decode, b64encode } = await import("/assets/js/contracts/b64.js");
    const hash = b64decode("zS9XXS6Q0JSYOx3mn2pxZV3w9H6M_PdzXHZtxaXKN9s");
    // No anchor from the server and nothing remembered: there is nothing to
    // hang a trust list from, and the person is told what is missing.
    const nothing = await anchorFor({ anchor: null });
    // What the initial setup left behind stands in for it.
    await rememberAnchor({ seq: 1, hash });
    const kept = await anchorFor({ anchor: null });
    // The server's own anchor beats it: once the web controller is paired,
    // the file on the server is the one that counts.
    const fromServer = await anchorFor({ anchor: { seq: 7, hash } });
    return {
      nothing, text: NOT_SET_UP,
      keptSeq: kept?.seq ?? 0, keptHash: kept === null ? "" : b64encode(kept.hash),
      serverSeq: fromServer?.seq ?? 0,
    };
  });
  expect(got.nothing).toBeNull();
  expect(got.text).toBe(
    "Diese Anlage ist noch nicht fertig eingerichtet. Bitte den Controller des Webservers koppeln.");
  expect(got.keptSeq).toBe(1);
  expect(got.keptHash).toBe("zS9XXS6Q0JSYOx3mn2pxZV3w9H6M_PdzXHZtxaXKN9s");
  expect(got.serverSeq).toBe(7);
});

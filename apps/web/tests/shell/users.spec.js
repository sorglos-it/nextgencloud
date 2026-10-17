import { test, expect } from "@playwright/test";
import { signIn, skipWithoutLogin } from "../fixtures/page.js";

// "Benutzer und Rechte": the surface where an admin hands out rights and lets
// a new server into the installation. It is the most dangerous window NGC
// has, and these cases are written for that: what is on screen, what goes on
// the wire, and what the person is asked before either happens.
//
// WHERE THE DATA COMES FROM. Every answer below is BYTES, and every one of
// them goes through core.parseResult inside the app - the same reading the
// real API's answer will get. The stand-in in shell/fake-link.js is replaced
// here by a world of this file's own (serveWorld), because the stand-in
// answers ngc.desktop/apps and nothing of ngc.users. The shape is the
// stand-in's own - call() and open() hand back bytes, onNotice() takes a
// listener, current() answers with the version in force - so nothing here
// invents a second interface for Task 17 to find.
//
// WHY MOST CASES MOUNT THE APP THEMSELVES instead of opening it from the
// start menu: the WebKit that Playwright brings has neither Ed25519 nor
// X25519, so shell/main.js stops before the desktop is ever built (measured,
// fixtures/page.js says the same). Mounted into a container of its own the
// app runs there too, in the real page, under the real content security
// policy - which is where the markup, the escaping and the keyboard belong.
// The last two cases of this file are the ones that go through the desktop -
// the start menu, the window, the wiring in desktop.js - and they skip where
// there is no desktop.

// ------------------------------------------------------------------ the world
//
// The module the page gets in place of shell/fake-link.js. It is written out
// as source because the page imports a module. fakeWorld() is what
// shell/main.js takes; world(cfg) is the same thing with the answers steered,
// for a mounted app.
const WORLD = `const enc = new TextEncoder();

// Two Noise keys, 32 bytes each, as they arrive from the API: base64url.
const KEY_A = "wcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcE";
const KEY_B = "wsLCwsLCwsLCwsLCwsLCwsLCwsLCwsLCwsLCwsLCwsI";

export const DEFAULTS = {
  "ngc.users/list": [
    { id: "ICAgICAgICAgICAgICAgIA", name: "admin2", roles: ["admin"], totp: true, locked: false, lastLogin: 1790000000 },
    { id: "ISEhISEhISEhISEhISEhIQ", name: "admin", roles: ["admin"], totp: false, locked: false, lastLogin: 0 },
    { id: "IiIiIiIiIiIiIiIiIiIiIg", name: "anna", roles: ["technik"], totp: false, locked: true, lastLogin: 1790003600 },
  ],
  "ngc.users/rights": [
    { role: "technik", rights: ["de.example.fotos/use"] },
    { role: "user", rights: [] },
  ],
  "ngc.users/pending": [
    { name: "dienste", key: KEY_A, since: 1790000000 },
    // A name that tries to be markup. It is a legal name by the contract, so
    // nothing refuses it and the surface has to put it on screen as text.
    { name: "<img src=x onerror=alert(1)>B\\u00f6se", key: KEY_B, since: 1790003600 },
  ],
  "ngc.users/pending-api": [
    {
      node: "api-2",
      noise: "PFxs4t2Z4Q0sPeBddzqhXj5tlx7U5BOJyTtLvdoXchI",
      sign: "Zej5sLxurhJBafBXb5c2LSlajPX3cLReFDV85kfTPuw",
      tls: "0yeBC6i9XzXXJCoODo_Q7qxV74LKjC1p0rR1cXxkldo",
      url: "wss://api-2.test:7443/tunnel",
      since: 1790000000,
    },
  ],
  "ngc.users/nodes": [
    { node: "ctl-1", online: true, roles: ["services"], version: "1.0.0", arch: "amd64" },
  ],
  // A FULL page (the app asks for 50) and then one more, so that "Mehr laden"
  // has something to fetch and the end of the log is reached on the second
  // page. A short first page would mean the button is never drawn at all.
  "ngc.users/log": logPageOne(),
  "ngc.users/log/2": [
    { id: "aaaaaaaabbbbbbbbccccccccdddddddd", time: 1789990000, user: "", kind: "login",
      app: "", action: "", node: "", result: "ok", detail: "" },
  ],
  "ngc.users/log/3": [],
  "ngc.users/pair-code": null, // filled in by world(), it needs the version
  "ngc.users/join-code": null, // the same, under the other prefix
  "ngc.users/set-rights": {},
  "ngc.users/trust": { seq: 2 },
};

function logPageOne() {
  const rows = [{ id: "00000000000000000000000000000000", time: 1790003600,
    user: "ISEhISEhISEhISEhISEhIQ", kind: "task", app: "ngc.store", action: "activate",
    node: "ctl-1", result: "ok", detail: "" }];
  for (let i = 1; i < 50; i++) {
    rows.push({ id: String(i).padStart(32, "0"), time: 1790003600 - i,
      user: "IiIiIiIiIiIiIiIiIiIiIg", kind: "login", app: "", action: "", node: "",
      result: "ok", detail: "" });
  }
  return rows;
}

// The apps of the desktop. ngc.users is in it so the start menu has the
// entry; the rest is what the rights of a role can be about.
export const APPS = [
  { id: "ngc.users", name: "Benutzer und Rechte", version: "1.0.0", icon: "", entry: "",
    width: 860, height: 600, actions: [], state: "ok", core: true, node: "" },
  { id: "de.example.fotos", name: "Fotos", version: "2.1.0", icon: "", entry: "",
    width: 640, height: 480, actions: ["teilen"], state: "ok", core: false, node: "pi-1" },
  { id: "de.example.boese", name: "<img src=x onerror=alert(1)>B\\u00f6se", version: "0.1.0",
    icon: "", entry: "", width: 400, height: 300, actions: [], state: "ok", core: false, node: "" },
];

// The trust list of this installation: the genesis of shared/testdata, field
// for field. It is decoded here rather than parsed with trust.parseList,
// because parseList imports every passkey and no engine without Ed25519 can
// do that - and a list is needed on WebKit too. What it is used for below
// needs no signature: tunnel.checkCode compares numbers and bytes.
export async function genesis() {
  const { b64decode } = await import("/assets/js/contracts/b64.js");
  const trust = await import("/assets/js/contracts/trust.js");
  const payload = b64decode(GENESIS_PAYLOAD);
  return { list: await decodeList(payload), hash: await trust.hash({ payload }), payload };
}

// decodeList reads a payload through the contract's own decoder and its own
// shape, but WITHOUT the key import trust.parseList does on top. The genesis
// of shared/testdata carries a PASSKEY, and an engine without Ed25519 can
// import none - parseList would refuse a perfectly good list there and every
// case built on it would be measuring the engine. What the shell really does
// with a chain is the TrustStore's business, and that is measured with the
// real one.
export async function decodeList(payload) {
  const { decode, Budget } = await import("/assets/js/contracts/json.js");
  const trust = await import("/assets/js/contracts/trust.js");
  const budget = new Budget({ size: 4 << 20, items: {
    "rp/origins": 8, users: 10000, "users/*/roles": 256, "users/*/keys": 16,
    roles: 256, "roles/*/black": 256, "roles/*/white": 256,
    nodes: 256, "nodes/*/rootUsers": 256, "nodes/*/rootRoles": 256, stores: 64, auto: 64,
  } });
  return decode(payload, budget, trust.listShape);
}

const GENESIS_PAYLOAD = ${JSON.stringify(null)}; // replaced by serveWorld

// A pairing code for one version: what tunnel.ParsePairCode reads.
export async function pairCode(version, prefix = "ngc-pair:") {
  const { b64encode } = await import("/assets/js/contracts/b64.js");
  const api = version.list.nodes.find((n) => n.kind === "api");
  const doc = {
    url: api.url, key: b64encode(api.noise), tls: b64encode(api.tls),
    secret: b64encode(new Uint8Array(16).fill(95)),
    anchor: { seq: version.list.seq, hash: b64encode(version.hash) },
  };
  return prefix + b64encode(enc.encode(JSON.stringify(doc)));
}

// world builds the link, the version in force and the session the app is
// given. cfg replaces single answers; everything it does not name stays as
// DEFAULTS has it.
//
// THE SPLIT THIS TURNS ON, and Fixrunde 2 is about exactly it: the API KEEPS
// the chain, and this browser has a version IN FORCE. Two different things.
// The version in force does NOT move because something was sent - it moves
// when the chain is followed, and that is what refresh does. The stand-in
// this file had before held one list and moved it itself, which hid the fork
// K2 is about.
//
// refresh here answers with a NUMBER and follows the kept chain, by the rules
// the real TrustStore keeps. Those rules are not taken on trust: the case
// "refresh answers with a number, and only a higher hint asks anybody"
// measures them against the REAL TrustStore, over a real chain out of
// shared/testdata and a real fetch. What this one does NOT do is check a
// signature while following - trust.advance does that in the real store, and
// a case that needed it here would be measuring the store and not this app.
export async function world(cfg = {}) {
  const answers = { __proto__: null, ...DEFAULTS, ...cfg.answers };
  const notices = new Set();
  const version = cfg.noVersion === true ? null
    : cfg.signable === true ? await signableVersion() : await genesis();

  // The chain the API keeps. Index 0 is seq 1.
  const kept = version === null ? [] : [signedOf(version.payload)];
  let inForce = version;
  if (cfg.apiAhead === true && version !== null) {
    kept.push(signedOf((await nextVersion(version)).payload));
  }
  if (answers["ngc.users/pair-code"] === null && version !== null) {
    answers["ngc.users/pair-code"] = {
      code: await pairCode(cfg.codeFor === "next" ? await nextVersion(version) : version),
      expires: 1790086400,
    };
  }
  // The join code of the same installation, and cfg.codeFor reaches it too:
  // the two codes differ in their prefix and in nothing else, so one world
  // measures both ways through showCode.
  if (answers["ngc.users/join-code"] === null && version !== null) {
    answers["ngc.users/join-code"] = {
      code: await pairCode(cfg.codeFor === "next" ? await nextVersion(version) : version,
        "ngc-join:"),
      expires: 1790086400,
    };
  }
  // ngc.users/api/trust is flow "in": the body travels as opts.stream, and
  // the API answers with the number of the version that now stands. It KEEPS
  // it; nothing here moves what this browser has in force.
  answers["ngc.users/trust"] = async (body) => {
    const t = await import("/assets/js/contracts/trust.js");
    const sg = t.parseSigned(body);
    const list = await decodeList(sg.payload);
    kept.push({ payload: sg.payload, proof: sg.proof });
    return { seq: list.seq };
  };

  // What the remote received. The cases read these instead of guessing from
  // the screen: the bytes are the contract, the screen is only its face.
  window.__ngcCalls = [];
  window.__ngcSent = [];
  window.__ngcHints = [];

  let logPage = 0;
  const link = {
    // The shortcut the desktop's link carries, member for member as
    // apps/store.js hands it over: params as bytes, and one option bag whose
    // stream is the body of a flow-"in" action.
    async call(app, service, name, params, opts) {
      const body = await collect(opts?.stream);
      window.__ngcCalls.push({
        app, service, name, params: params ? new Uint8Array(params) : null, body,
      });
      if (body.length > 0) window.__ngcSent.push({ app, service, name, bytes: body });
      if (app === "ngc.desktop" && name === "apps") return enc.encode(JSON.stringify(APPS));
      let key = app + "/" + name;
      if (key === "ngc.users/log") {
        logPage++;
        if (logPage > 1) key = "ngc.users/log/" + logPage;
      }
      const a = answers[key];
      if (a === undefined) throw new Error("world: nothing for " + key);
      if (a instanceof Error) throw a;
      const v = typeof a === "function" ? await a(body) : a;
      return enc.encode(typeof v === "string" ? v : JSON.stringify(v));
    },
    onNotice(fn) { if (typeof fn === "function") notices.add(fn); },
  };

  const trust = {
    current: () => inForce,
    // A NUMBER in, a NUMBER out - and a hint that is not higher than the
    // version in force asks nobody.
    async refresh(hintSeq) {
      window.__ngcHints.push(hintSeq);
      if (inForce === null) throw new Error("world: no list in force");
      const want = Number.isSafeInteger(hintSeq) && hintSeq >= 1 ? hintSeq : 0;
      if (want <= inForce.list.seq) return inForce.list.seq;
      // stuck is the browser that did not get there: the write went through,
      // the following did not.
      if (cfg.stuck === true) return inForce.list.seq;
      const t = await import("/assets/js/contracts/trust.js");
      for (let i = inForce.list.seq; i < kept.length; i++) {
        const list = await decodeList(kept[i].payload);
        inForce = {
          list, hash: await t.hash({ payload: kept[i].payload }),
          signed: kept[i], payload: kept[i].payload,
        };
      }
      return inForce.list.seq;
    },
  };
  if (cfg.noRefresh === true) delete trust.refresh;

  // The session says WHOSE it is, and nothing else this app needs. It does
  // NOT sign a trust list: a session key stands in no list, so such a
  // signature would never verify. The person's own key is asked for by
  // confirm.js.
  const session = cfg.noSession === true ? null : { user: ADMIN_ID };
  return { link, trust, session, apps: APPS };
}

// collect reads a body stream into one array, the way the link does.
async function collect(stream) {
  if (!stream) return new Uint8Array(0);
  const reader = stream.getReader();
  const parts = [];
  let n = 0;
  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    const v = new Uint8Array(value);
    parts.push(v);
    n += v.length;
  }
  const out = new Uint8Array(n);
  let at = 0;
  for (const p of parts) { out.set(p, at); at += p.length; }
  return out;
}

// One version of the kept chain. The proof of version 1 is never looked at -
// trust.advance checks the versions AFTER the one in force, never that one.
const signedOf = (payload) => ({
  payload,
  proof: {
    kind: "password", kid: "AAAAAAAAAAAAAAAAAAAAAA", sig: new Uint8Array(64).fill(8),
    authData: new Uint8Array(0), clientData: new Uint8Array(0),
  },
});

export const ADMIN_ID = "ISEhISEhISEhISEhISEhIQ";
export const PASSWORD = "Heimnetz-2026";
// The cheapest parameters proof.validateKDF allows: this key is derived again
// on every signature, and the case is about WHO signs, not about how long
// Argon2 takes.
const KDF = { alg: "argon2id", salt: new Uint8Array(16).fill(5), m: 19456, t: 1, p: 1 };

// adminKey is the password key of the admin, derived from PASSWORD with the
// KDF the list carries - so confirm.js, which derives it again out of what
// the person types, really produces a signature this list verifies.
let derived = null;
export async function adminKey() {
  if (derived) return derived;
  const sign = await import("/assets/js/contracts/sign.js");
  const proof = await import("/assets/js/contracts/proof.js");
  const seed = await proof.deriveSeed(PASSWORD, KDF);
  const pub = await proof.publicFromSeed(seed);
  seed.fill(0);
  derived = {
    id: await sign.keyIDAsync(pub), kind: "password", alg: "EdDSA", pub,
    credId: new Uint8Array(0), kdf: KDF,
  };
  return derived;
}

// signableVersion is the same installation with one admin, whose key is the
// one PASSWORD derives. Only then can the shell's own trust.advance check
// what the person just signed, which is what buildNextVersion does before
// anything goes out.
async function signableVersion() {
  const trust = await import("/assets/js/contracts/trust.js");
  const g = await genesis();
  const list = structuredClone(g.list);
  list.users = [{ id: ADMIN_ID, name: "admin", roles: ["admin"], keys: [await adminKey()] }];
  // No passkey in this list, so it names no party to ask one of - and
  // confirm.js therefore asks for the password.
  list.rp = { id: "", origins: [] };
  const payload = trust.encodeList(list);
  return { list, hash: await trust.hash({ payload }), payload };
}

// nextVersion is version 2 of the same installation: the one a pairing code
// can name that the shell has not followed to yet.
async function nextVersion(version) {
  const trust = await import("/assets/js/contracts/trust.js");
  const next = structuredClone(version.list);
  next.seq = version.list.seq + 1;
  next.prev = version.hash;
  next.issued = 1790050000;
  const payload = trust.encodeList(next);
  return { list: next, hash: await trust.hash({ payload }), payload };
}

// fakeWorld is what shell/main.js imports. Same shape as the stand-in it
// stands in for, plus the session, which main.js passes straight on.
export async function fakeWorld() {
  const w = await world({});
  window.__ngcWorld = w;
  return w;
}
`;

// The genesis payload of shared/testdata/trust.json, read out of the
// repository so the version the cases work with is the one Go signs.
import { vectors } from "../fixtures/vectors.js";
const GENESIS = vectors("trust.json")[0];

// The four-version chain of shared/testdata: what the real TrustStore is
// measured over below.
const GENESIS_CHAIN = vectors("trust.json")
  .find((c) => c.valid && c.anchor.seq === 1 && c.chain.length === 4);

// Does this engine's Web Crypto know Ed25519? Every key in trust.json is
// Ed25519, so an engine without it can verify no version of any chain there.
// Probed with a real key out of the fixture, the way trustlist.spec.js does
// it, never decided by the engine's name.
async function hasEd25519(page, c) {
  return page.evaluate(async (one) => {
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const payload = JSON.parse(new TextDecoder().decode(b64decode(one.chain[0].payload)));
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
  }, c);
}

async function serveWorld(page) {
  const src = WORLD.replace("const GENESIS_PAYLOAD = null;",
    "const GENESIS_PAYLOAD = " + JSON.stringify(GENESIS.chain[0].payload) + ";");
  await page.route("**/assets/js/shell/fake-link.js", (route) => route.fulfill({
    status: 200,
    headers: { "content-type": "text/javascript; charset=utf-8", "cache-control": "no-store" },
    body: src,
  }));
}

// A DIALOG IS A FAILURE, the rule desktop-hostile.spec.js already keeps: an
// alert is what a name that got executed would reach for.
test.beforeEach(async ({ page }) => {
  page.on("dialog", (d) => {
    throw new Error("something in the page opened a " + d.type() + ": " + d.message());
  });
  await serveWorld(page);
  await page.goto("/");
});

// mount puts the app in a container of its own, on top of whatever the page
// built, and hands it the world. It answers once the tab strip stands - what
// each panel then fetches is what the case after it waits for.
async function mount(page, cfg = {}) {
  // THE SHELL'S OWN BOOT HAS TO BE OVER FIRST, and that is MEASURED, not
  // tidiness. There is no session in this fixture, so the boot always ends at
  // the login mask - and that mask focuses its name field. Mounting while the
  // boot is still running leaves a window in which the mask appears AFTER the
  // app and takes the focus out of it.
  //
  // A probe caught it in the act under four workers: `activeElement` was
  // "BUTTON/tab/ngc-users-tab-users" right after focus() and "INPUT" one
  // ArrowRight later, with form.loginbox going from 0 to 1 in between - and
  // in one run of eight the input already held the focus before the first
  // key. The rate, chromium, --repeat-each=30 --workers=4: 10 of 30 red
  // without this line, 0 of 30 with it.
  //
  // It had been a window all along and the language files only widened it: a
  // browser offering de-DE fetches de.json AND en.json before shell/main.js
  // draws anything (shell/lang.js, load), where en-US fetches one. Measured
  // on the branch before them: 0 of 30. So this is the window closed, not the
  // fetches hidden - every case here mounts through this function.
  //
  // WHAT IS WAITED FOR IS ALL THREE WAYS THE BOOT CAN END, and the first
  // version of this line waited for the login mask alone. MEASURED what that
  // cost: 16 cases of this file went red in webkit, every one of them with
  // "locator('form.loginbox') ... element(s) not found", because that engine
  // never reaches a login mask - boot.spec.js's own case says the shell
  // either brings the desktop up or SAYS WHY IT CANNOT, and in webkit it says
  // why (.msg). A marker that only one engine reaches is a marker that turns
  // the other engines red for a reason that has nothing to do with the case.
  await page.waitForFunction(() => document.querySelector("form.loginbox") !== null
    || document.querySelector("#taskbar") !== null
    || document.querySelector(".msg") !== null);
  await page.evaluate(async (cfg) => {
    const { usersApp } = await import("/assets/js/apps/users.js");
    const { world } = await import("/assets/js/shell/fake-link.js");
    const ctx = await world(cfg);
    const host = document.createElement("div");
    host.id = "mount";
    Object.assign(host.style, { position: "fixed", inset: "0", zIndex: "99999", overflow: "auto" });
    host.className = "body";
    document.body.append(host);
    window.__ngcCtx = ctx;
    window.__ngcClose = usersApp({ body: host }, ctx);
  }, cfg);
  await expect(page.getByRole("tab", { name: "Benutzer", exact: true })).toBeVisible();
}

const app = (page) => page.locator("#mount");

async function openTab(page, name) {
  await app(page).getByRole("tab", { name, exact: true }).click();
}

// ------------------------------------------------------------------ 1 pending
test("a waiting controller shows the fingerprint the shell computed itself", async ({ page }) => {
  await mount(page);
  await openTab(page, "Server");
  const row = app(page).locator(".pending .row", { hasText: "dienste" });
  await expect(row).toBeVisible();
  // The answer carries NO fingerprint - core.PendingNode has name, key and
  // since and nothing else - so the one on screen can only have been
  // computed. What this case pins is that it was computed the one right way:
  // tunnel.fingerprint over exactly the key of that row. A surface that
  // showed the key itself, its first characters, or a hash of another length
  // is red here.
  const mine = await page.evaluate(async () => {
    const { fingerprint } = await import("/assets/js/contracts/tunnel.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    return fingerprint(b64decode("wcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcE"));
  });
  expect(mine).toHaveLength(22);
  await expect(row.locator(".fp")).toHaveText(mine);
  await expect(app(page).locator(".pending .hint")).toHaveText(
    "Vergleichen Sie diesen Fingerabdruck mit dem, den das Skript auf dem Server anzeigt.");
});

test("a controller name that tries to be markup stays letters", async ({ page }) => {
  await mount(page);
  await openTab(page, "Server");
  const row = app(page).locator(".pending .row", { hasText: "Böse" });
  await expect(row.locator(".nm")).toHaveText("<img src=x onerror=alert(1)>Böse");
  expect(await app(page).locator("img, script, svg, iframe").count()).toBe(0);
});

// -------------------------------------------------------------- 2 pending API
test("a waiting API server shows its join fingerprint and its address character by character",
  async ({ page }) => {
    await mount(page);
    await openTab(page, "Server");
    const row = app(page).locator(".pendapi .row", { hasText: "api-2" });
    await expect(row).toBeVisible();
    const mine = await page.evaluate(async () => {
      const { joinFingerprint } = await import("/assets/js/contracts/tunnel.js");
      const { b64decode } = await import("/assets/js/contracts/b64.js");
      // Exactly node, noise, sign, tls and url, in that order: any other
      // reading is another fingerprint, and the admin compares it by eye.
      return joinFingerprint("api-2",
        b64decode("PFxs4t2Z4Q0sPeBddzqhXj5tlx7U5BOJyTtLvdoXchI"),
        b64decode("Zej5sLxurhJBafBXb5c2LSlajPX3cLReFDV85kfTPuw"),
        b64decode("0yeBC6i9XzXXJCoODo_Q7qxV74LKjC1p0rR1cXxkldo"),
        "wss://api-2.test:7443/tunnel");
    });
    await expect(row.locator(".fp")).toHaveText(mine);
    // The address stands character by character, so a host that only looks
    // like the right one cannot hide in the kerning.
    const url = "wss://api-2.test:7443/tunnel";
    const chars = row.locator(".url .ch");
    await expect(chars).toHaveCount([...url].length);
    await expect(row.locator(".url")).toHaveText(url);
    await expect(row.getByRole("button", { name: "Server api-2 aufnehmen" })).toBeVisible();
  });

test("an address that is no tunnel address is never offered", async ({ page }) => {
  // TWO MEASUREMENTS, because one of them alone would be green for the wrong
  // reason. core.parseResult already refuses the whole answer when one entry
  // carries an address the grammar does not know (checkPendingAPI runs
  // trust.validTunnelURL), so "nothing is offered" says nothing about the
  // app's own rule. joinable is that rule, and it is asked directly.
  await mount(page, { answers: { "ngc.users/pending-api": [{
    node: "api-3", noise: "PFxs4t2Z4Q0sPeBddzqhXj5tlx7U5BOJyTtLvdoXchI",
    sign: "Zej5sLxurhJBafBXb5c2LSlajPX3cLReFDV85kfTPuw",
    tls: "0yeBC6i9XzXXJCoODo_Q7qxV74LKjC1p0rR1cXxkldo",
    url: "https://api-3.test:7443/tunnel", since: 1790000000,
  }] } });
  await openTab(page, "Server");
  await expect(app(page).locator(".pendapi .note")).toHaveText(
    "Die Antwort der Anlage war nicht in Ordnung. Es wird nichts angeboten.");
  expect(await app(page).locator(".pendapi .row").count()).toBe(0);

  const asked = await page.evaluate(async () => {
    const { joinable } = await import("/assets/js/apps/users.js");
    const ok = { node: "api-2", url: "wss://api-2.test:7443/tunnel" };
    return {
      good: joinable(ok),
      scheme: joinable({ ...ok, url: "https://api-2.test:7443/tunnel" }),
      path: joinable({ ...ok, url: "wss://api-2.test:7443/anders" }),
      nothing: joinable({ ...ok, url: "" }),
      broken: joinable(null),
    };
  });
  expect(asked).toEqual({ good: true, scheme: false, path: false, nothing: false, broken: false });
});

// ---------------------------------------------------------------- 3 new version
//
// sign answers the question confirm.js asks for the person's own key. A
// version of the trust list is verified against a key that stands in the
// version BEFORE it, so the session cannot sign one - the person does.
async function sign(page, password = "Heimnetz-2026") {
  const q = page.locator(".confirmbox");
  await expect(q).toBeVisible();
  await q.locator("#confirmpw").fill(password);
  await q.getByRole("button", { name: "Bestätigen", exact: true }).click();
  await expect(q).toHaveCount(0);
}

test("joining a controller builds version N+1, checks it and sends exactly those bytes",
  async ({ page }) => {
    await skipWithoutLogin(page);
    const out = await page.evaluate(async () => {
      const users = await import("/assets/js/apps/users.js");
      const trust = await import("/assets/js/contracts/trust.js");
      const { world, adminKey, PASSWORD } = await import("/assets/js/shell/fake-link.js");
      const sign = await import("/assets/js/contracts/sign.js");
      const proof = await import("/assets/js/contracts/proof.js");
      const { b64decode, b64encode, equal } = await import("/assets/js/contracts/b64.js");

      // The version in force of an installation whose one admin is the key
      // PASSWORD derives - so the shell's own advance() can check what was
      // just signed with it.
      const w = await world({ signable: true });
      const version = w.trust.current();
      const key = await adminKey();
      const seed = await proof.deriveSeed(PASSWORD, key.kdf);
      const priv = await proof.privateFromSeed(seed);
      const signer = async (b) => ({
        kind: "password", kid: key.id,
        sig: new Uint8Array(await crypto.subtle.sign({ name: "Ed25519" }, priv,
          sign.message(sign.PURPOSE_TRUST_LIST, b))),
        authData: new Uint8Array(0), clientData: new Uint8Array(0),
      });

      const built = await users.buildNextVersion(version, (next) => {
        next.nodes.push({
          id: "dienste", kind: "controller", name: "dienste",
          noise: b64decode("wcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcE"),
          sign: new Uint8Array(0), url: "", tls: new Uint8Array(0),
          rootUsers: [], rootRoles: [],
        });
      }, { now: 1790100000, signer });

      const got = await trust.parseList(built.payload);
      const before = structuredClone(version.list);
      before.seq = 0; before.prev = new Uint8Array(0); before.issued = 0; before.nodes = [];
      const after = structuredClone(got);
      after.seq = 0; after.prev = new Uint8Array(0); after.issued = 0; after.nodes = [];
      return {
        seq: got.seq, prevIsHash: equal(got.prev, version.hash), issued: got.issued,
        // Sorted by id: "api-1" < "ctl-1" < "dienste".
        nodes: got.nodes.map((n) => n.id),
        // Everything but seq, prev, issued and the nodes is the version before.
        restUntouched: b64encode(trust.encodeList(before)) === b64encode(trust.encodeList(after)),
        // The same check the API and every controller will run.
        advances: (await trust.advance(version, [built])).list.seq,
        purpose: sign.PURPOSE_TRUST_LIST,
      };
    });
    expect(out.error).toBe(undefined);
    expect(out.seq).toBe(2);
    expect(out.prevIsHash).toBe(true);
    expect(out.issued).toBe(1790100000);
    expect(out.nodes).toEqual(["api-1", "ctl-1", "dienste"]);
    expect(out.restUntouched).toBe(true);
    expect(out.advances).toBe(2);
    expect(out.purpose).toBe("trustlist/v1");
  });

test("a change the trust list would refuse is never signed and never sent", async ({ page }) => {
  const out = await page.evaluate(async () => {
    const users = await import("/assets/js/apps/users.js");
    const { world } = await import("/assets/js/shell/fake-link.js");
    const w = await world({});
    let signed = 0;
    const version = w.trust.current();
    try {
      await users.buildNextVersion(version, (next) => {
        // Two nodes with the same id: trust.List.Validate refuses it.
        next.nodes.push({ ...next.nodes[0] });
      }, { now: 1790100000, signer: async () => { signed++; return null; } });
      return { threw: "", signed };
    } catch (e) {
      return { threw: e.message, signed };
    }
  });
  expect(out.threw).toBe("Diese Änderung ergibt keine gültige Vertrauensliste.");
  // The signature is the LAST thing to happen, so a change that does not hold
  // up never gives one away.
  expect(out.signed).toBe(0);
});

test("the trust list goes over ngc.users/api/trust as one signed version", async ({ page }) => {
  await skipWithoutLogin(page);
  await mount(page, { signable: true });
  await openTab(page, "Server");
  await app(page).getByRole("button", { name: "Server dienste aufnehmen" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "Server aufnehmen" }).click();
  await sign(page);
  await expect(app(page).locator(".pending .note")).toHaveText("Der Server »dienste« ist aufgenommen.");

  const sent = await page.evaluate(async () => {
    const trust = await import("/assets/js/contracts/trust.js");
    const one = window.__ngcSent[0];
    const s = trust.parseSigned(one.bytes);
    const l = await trust.parseList(s.payload);
    return {
      where: one.app + "/" + one.service + "/" + one.name,
      count: window.__ngcSent.length,
      seq: l.seq,
      nodes: l.nodes.map((n) => n.id),
      kind: s.proof.kind,
      hints: window.__ngcHints,
    };
  });
  expect(sent.count).toBe(1);
  expect(sent.where).toBe("ngc.users/api/trust");
  expect(sent.seq).toBe(2);
  expect(sent.nodes).toEqual(["api-1", "ctl-1", "dienste"]);
  expect(sent.kind).toBe("password");
  // The answer was NOT a receipt to throw away: its number went to refresh.
  expect(sent.hints).toEqual([2]);
});

test("two servers taken in one after the other give n+1 and n+2, not two at one number",
  async ({ page }) => {
    // K2 OF FIXRUNDE 2, AND THE PROOF IT OWES. The version in force here does
    // not move because something was sent: the API keeps what it was sent,
    // this browser follows the chain. A surface that threw the answer away
    // would build the second version on the SAME prev, and two versions at
    // one seq are a FORK of the root everything else hangs from.
    await skipWithoutLogin(page);
    await mount(page, { signable: true });
    const start = await page.evaluate(() => {
      const c = window.__ngcStart = window.__ngcCtx.trust.current();
      return c.list.seq;
    });
    expect(start).toBe(1);
    await openTab(page, "Server");

    await app(page).getByRole("button", { name: "Server dienste aufnehmen" }).click();
    await page.getByRole("dialog").getByRole("button", { name: "Server aufnehmen" }).click();
    await sign(page);
    await expect(app(page).locator(".pending .note"))
      .toHaveText("Der Server »dienste« ist aufgenommen.");

    await app(page).getByRole("button", { name: "Server api-2 aufnehmen" }).click();
    await page.getByRole("dialog").getByRole("button", { name: "Server aufnehmen" }).click();
    await sign(page);
    await expect(app(page).locator(".pendapi .note"))
      .toHaveText("Der Server »api-2« ist aufgenommen.");

    const out = await page.evaluate(async () => {
      const trust = await import("/assets/js/contracts/trust.js");
      const chain = [];
      for (const one of window.__ngcSent) chain.push(trust.parseSigned(one.bytes));
      const seqs = [];
      for (const v of chain) seqs.push((await trust.parseList(v.payload)).seq);
      let together = "";
      try {
        // BOTH TOGETHER, from the version the shell started on. A fork does
        // not pass this: the second version would not chain to the first.
        together = String((await trust.advance(window.__ngcStart, chain)).list.seq);
      } catch (e) {
        together = "refused: " + e.message;
      }
      return { sent: chain.length, seqs, together, hints: window.__ngcHints,
        inForce: window.__ngcCtx.trust.current().list.seq };
    });
    expect(out.sent).toBe(2);
    expect(out.seqs).toEqual([2, 3]);
    expect(out.together).toBe("3");
    // Each answer's number went to refresh, and the version in force followed.
    expect(out.hints).toEqual([2, 3]);
    expect(out.inForce).toBe(3);
  });

test("a version this browser did not follow to stops the surface instead of forking it",
  async ({ page }) => {
    await skipWithoutLogin(page);
    // The write goes through, the following does not. Nothing more may be
    // written from here - a second version on the old prev is the fork.
    await mount(page, { signable: true, stuck: true });
    await openTab(page, "Server");
    await app(page).getByRole("button", { name: "Server dienste aufnehmen" }).click();
    await page.getByRole("dialog").getByRole("button", { name: "Server aufnehmen" }).click();
    await sign(page);
    await expect(app(page).locator(".pending .note")).toHaveText(
      "Der Server ist eingetragen. Bitte die Seite neu laden, damit NGC hier auf dem "
      + "neuen Stand ist.");
    const out = await page.evaluate(() => ({
      sent: window.__ngcSent.length, inForce: window.__ngcCtx.trust.current().list.seq,
    }));
    expect(out.sent).toBe(1);
    expect(out.inForce).toBe(1);
  });

test("Abbrechen at the question for the key sends nothing", async ({ page }) => {
  await skipWithoutLogin(page);
  await mount(page, { signable: true });
  await openTab(page, "Server");
  await app(page).getByRole("button", { name: "Server dienste aufnehmen" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Server aufnehmen" }).click();
  const q = page.locator(".confirmbox");
  await expect(q).toBeVisible();
  await q.getByRole("button", { name: "Abbrechen" }).click();
  await expect(app(page).locator(".pending .note")).toHaveText("Nichts geändert.");
  expect(await page.evaluate(() => window.__ngcSent.length)).toBe(0);
});

// ------------------------------------------------------------------ 4 pair code
//
// WHAT refresh REALLY IS, measured here against the REAL TrustStore and not
// taken from anybody's word for it - the whole of K1 rests on these three
// numbers. The chain is the four-version one out of shared/testdata, served
// over the shell's own fetch; how often the API is asked is counted from the
// requests themselves.
test("refresh answers with a number, and only a higher hint asks anybody", async ({ page }) => {
  const c = GENESIS_CHAIN;
  test.skip(!(await hasEd25519(page, c)),
    "no Ed25519 in this engine's Web Crypto: it can verify no version of this chain");
  let visible = 1;
  const asked = [];
  await page.route(/\/api\/v1\/trust\?/, async (route) => {
    const from = Number(new URL(route.request().url()).searchParams.get("from"));
    asked.push(from);
    const at = from - c.anchor.seq;
    if (at < 0 || at > visible) {
      await route.fulfill({ status: 404, contentType: "application/json; charset=utf-8",
        body: JSON.stringify({ status: "error", code: "E_NOTFOUND", message: "Nicht gefunden." }) });
      return;
    }
    if (at === visible) { await route.fulfill({ status: 204 }); return; }
    const versions = c.chain.slice(at, visible);
    await route.fulfill({ status: 200, contentType: "application/json; charset=utf-8",
      body: JSON.stringify({ versions, more: false }) });
  });
  // The API owns up to more versions. A trust hint is exactly this: something
  // appeared that this browser has not followed to yet.
  await page.exposeFunction("__more", () => { visible = c.chain.length; });
  // Read from inside the page, BETWEEN the calls: an array read afterwards
  // says only what the whole run came to, and "asked nobody" is a statement
  // about one call.
  await page.exposeFunction("__asked", () => asked.slice());
  await page.goto("/");

  const out = await page.evaluate(async (raw) => {
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const { TrustStore } = await import("/assets/js/shell/trustlist.js");
    const s = new TrustStore();
    const at1 = await s.bootstrap({ seq: raw.seq, hash: b64decode(raw.hash) });
    const r = {};
    r.at1 = at1;
    // No number at all: the hint counts as 0, which is never higher than the
    // version in force. THIS is the call users.js used to make.
    r.none = await s.refresh();
    r.typeOfNone = typeof r.none;
    r.same = await s.refresh(1);
    r.lower = await s.refresh(0);
    r.askedSoFar = await window.__asked();
    await window.__more();
    r.higher = await s.refresh(4);
    r.askedAfter = await window.__asked();
    r.typeOfHigher = typeof r.higher;
    r.inForce = s.current().list.seq;
    // What a caller who took the answer for a version would have seen.
    r.listOfAnswer = typeof r.higher === "object" ? "an object" : String(r.higher.list);
    return r;
  }, { seq: c.anchor.seq, hash: c.anchor.hash });

  expect(out.at1).toBe(1);
  // A NUMBER, not a version - and the three hints that are not higher than
  // the version in force answer that number and ask nobody.
  expect(out.typeOfNone).toBe("number");
  expect(out.typeOfHigher).toBe("number");
  expect([out.none, out.same, out.lower]).toEqual([1, 1, 1]);
  // One request, and it was the bootstrap's: the three hints asked nobody.
  expect(out.askedSoFar).toEqual([1]);
  // A higher one follows the chain and answers with the number then in force.
  expect(out.higher).toBe(4);
  expect(out.inForce).toBe(4);
  expect(out.askedAfter).toEqual([1, 2]);
  // And the answer has no .list on it: a caller that read one got undefined.
  expect(out.listOfAnswer).toBe("undefined");
});

test("a pairing code is shown only when it fits the version the shell verified",
  async ({ page }) => {
    await mount(page);
    await openTab(page, "Server");
    await app(page).getByRole("button", { name: "Kopplungscode zeigen" }).click();
    const code = app(page).locator(".paircode .code");
    await expect(code).toContainText("ngc-pair:");
    // No round trip to the trust list was needed: the code names the version
    // the shell already holds.
    expect(await page.evaluate(() => window.__ngcHints)).toEqual([]);
  });

test("a pairing code that names a newer version makes the shell fetch the chain first",
  async ({ page }) => {
    await mount(page, { codeFor: "next", apiAhead: true });
    await openTab(page, "Server");
    await app(page).getByRole("button", { name: "Kopplungscode zeigen" }).click();
    await expect(app(page).locator(".paircode .code")).toContainText("ngc-pair:");
    // The number asked for is the one the CODE names, not nothing: a refresh
    // without it counts as 0 and asks nobody.
    expect(await page.evaluate(() => window.__ngcHints)).toEqual([2]);
    expect(await page.evaluate(() => window.__ngcCtx.trust.current().list.seq)).toBe(2);
  });

test("a pairing code that fits no version of this installation is not shown", async ({ page }) => {
  // The code names a version this installation never had: the chain is
  // fetched once, it still does not fit, and the code stays off the screen.
  await mount(page, { codeFor: "next" });
  await openTab(page, "Server");
  await app(page).getByRole("button", { name: "Kopplungscode zeigen" }).click();
  await expect(app(page).locator(".paircode .note")).toHaveText(
    "Dieser Code passt nicht zu dieser Anlage.");
  expect(await app(page).locator(".paircode .code").count()).toBe(0);
  expect(await page.evaluate(() => window.__ngcHints)).toEqual([2]);
});

// MEASURED on 17.10.2026 against a running installation: a second machine
// that asks for the name of a server ALREADY in the trust list is accepted by
// the API, and its request overwrites the waiting one under that name
// (join.keep stores by node id). The row then offered "Aufnehmen", and
// pressing it read "Die Anlage antwortet gerade nicht." - while the error
// really thrown was "Diese Änderung ergibt keine gültige Vertrauensliste."
// (probe in the deployed copy). Nothing was unreachable and nothing was
// signed; the trust list stayed at its version.
test("a waiting API server whose name is already listed is not offered, and the row says why",
  async ({ page }) => {
    await mount(page, { answers: { "ngc.users/pending-api": [{
      node: "api-1", // the API node the genesis of this installation carries
      noise: "PFxs4t2Z4Q0sPeBddzqhXj5tlx7U5BOJyTtLvdoXchI",
      sign: "Zej5sLxurhJBafBXb5c2LSlajPX3cLReFDV85kfTPuw",
      tls: "0yeBC6i9XzXXJCoODo_Q7qxV74LKjC1p0rR1cXxkldo",
      url: "wss://api-1.test:7443/tunnel", since: 1790000000,
    }] } });
    await openTab(page, "Server");
    const row = app(page).locator(".pendapi .row", { hasText: "api-1" });
    await expect(row).toBeVisible();
    await expect(row.locator(".note")).toHaveText(
      "Dieser Name ist in dieser Anlage schon vergeben. "
      + "Der Server muss unter einem anderen Namen beitreten.");
    // No button at all: a button that cannot do its job is a promise the
    // surface does not keep, and this one would end in a dialog that names a
    // fingerprint and then fails for a reason it does not name.
    expect(await row.locator("button").count()).toBe(0);
  });

test("a waiting API server with a free name is still offered", async ({ page }) => {
  // The control beside the case above: the default answer names api-2, which
  // stands in no version, and that row keeps its button.
  await mount(page);
  await openTab(page, "Server");
  const row = app(page).locator(".pendapi .row", { hasText: "api-2" });
  await expect(row.getByRole("button", { name: "Server api-2 aufnehmen" })).toBeVisible();
  expect(await row.locator(".note").count()).toBe(0);
});

// A join code is what ngc-api.sh asks for in its answer join_code, and until
// this there was no way in the interface to get one: ngc.users/join-code
// stood in contracts/core.js and nothing called it, so a second API server
// could not be set up at all. The cases below are the three of the pairing
// code, under the other prefix.
test("a join code is shown only when it fits the version the shell verified",
  async ({ page }) => {
    await mount(page);
    await openTab(page, "Server");
    await app(page).getByRole("button", { name: "Beitrittscode zeigen" }).click();
    const code = app(page).locator(".joincode .code");
    await expect(code).toContainText("ngc-join:");
    // The action really asked for is join-code and not pair-code: a button
    // wired to the neighbouring action would show a code that works and
    // spend the wrong one-time value on the API.
    expect(await page.evaluate(() => window.__ngcCalls.map((c) => c.name)))
      .toContain("join-code");
    expect(await page.evaluate(() => window.__ngcHints)).toEqual([]);
  });

test("a join code that names a newer version makes the shell fetch the chain first",
  async ({ page }) => {
    await mount(page, { codeFor: "next", apiAhead: true });
    await openTab(page, "Server");
    await app(page).getByRole("button", { name: "Beitrittscode zeigen" }).click();
    await expect(app(page).locator(".joincode .code")).toContainText("ngc-join:");
    expect(await page.evaluate(() => window.__ngcHints)).toEqual([2]);
  });

test("a join code that fits no version of this installation is not shown", async ({ page }) => {
  await mount(page, { codeFor: "next" });
  await openTab(page, "Server");
  await app(page).getByRole("button", { name: "Beitrittscode zeigen" }).click();
  await expect(app(page).locator(".joincode .note")).toHaveText(
    "Dieser Code passt nicht zu dieser Anlage.");
  expect(await app(page).locator(".joincode .code").count()).toBe(0);
});

test("the pairing button refuses a join code and the join button a pairing code",
  async ({ page }) => {
    // The one reading each button makes is the prefix of its own action. A
    // crossed wiring is therefore not a cosmetic mistake: it would put a code
    // on screen that the other script cannot use, after the API has already
    // spent the one-time value behind it.
    await mount(page, { answers: {
      "ngc.users/pair-code": { code: "ngc-join:AAAA", expires: 1790086400 },
      "ngc.users/join-code": { code: "ngc-pair:AAAA", expires: 1790086400 },
    } });
    await openTab(page, "Server");
    await app(page).getByRole("button", { name: "Kopplungscode zeigen" }).click();
    await expect(app(page).locator(".paircode .note")).toHaveText(
      "Dieser Code passt nicht zu dieser Anlage.");
    await app(page).getByRole("button", { name: "Beitrittscode zeigen" }).click();
    await expect(app(page).locator(".joincode .note")).toHaveText(
      "Dieser Code passt nicht zu dieser Anlage.");
    expect(await app(page).locator(".paircode .code, .joincode .code").count()).toBe(0);
  });

// --------------------------------------------------------------------- 5 rights
test("a right that is handed out is asked for first, and the question says what happens",
  async ({ page }) => {
    await mount(page);
    await openTab(page, "Rollen und Rechte");
    await app(page).getByRole("checkbox", { name: "Rolle technik: Fotos · teilen" }).check();
    await app(page).getByRole("button", { name: "Rechte der Rolle technik speichern" }).click();

    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    // NOT "Sind Sie sicher?": the question names the role, what is added,
    // what is taken away and whom it hits.
    await expect(dialog.getByRole("heading")).toHaveText("Rechte der Rolle »technik« ändern");
    await expect(dialog.locator(".adds li")).toHaveText(["Fotos · teilen"]);
    await expect(dialog.locator(".gone")).toHaveCount(0);
    await expect(dialog.locator(".who")).toHaveText(
      "Das gilt sofort für 1 Person mit dieser Rolle: anna.");
    // Nothing has gone out while the question stands.
    expect(await page.evaluate(() => window.__ngcCalls
      .filter((c) => c.name === "set-rights").length)).toBe(0);

    await dialog.getByRole("button", { name: "Rechte vergeben" }).click();
    const sent = await page.evaluate(() => {
      const c = window.__ngcCalls.filter((x) => x.name === "set-rights");
      return c.map((x) => new TextDecoder().decode(x.params));
    });
    expect(sent).toEqual(['{"role":"technik","rights":["de.example.fotos/teilen","de.example.fotos/use"]}']);
  });

test("Abbrechen sends nothing and leaves the boxes as they were", async ({ page }) => {
  await mount(page);
  await openTab(page, "Rollen und Rechte");
  const box = app(page).getByRole("checkbox", { name: "Rolle technik: Fotos · teilen" });
  await box.check();
  await app(page).getByRole("button", { name: "Rechte der Rolle technik speichern" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Abbrechen" }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  expect(await page.evaluate(() => window.__ngcCalls
    .filter((c) => c.name === "set-rights").length)).toBe(0);
  await expect(box).toBeChecked();
});

test("a role left without any right sends no rights at all", async ({ page }) => {
  await mount(page);
  await openTab(page, "Rollen und Rechte");
  await app(page).getByRole("checkbox", { name: "Rolle technik: Fotos · benutzen" }).uncheck();
  await app(page).getByRole("button", { name: "Rechte der Rolle technik speichern" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.locator(".gone li")).toHaveText(["Fotos · benutzen"]);
  await dialog.getByRole("button", { name: "Rechte vergeben" }).click();
  const sent = await page.evaluate(() => new TextDecoder()
    .decode(window.__ngcCalls.filter((x) => x.name === "set-rights")[0].params));
  // Go writes RoleRights.Rights with omitempty, and so does core.encodeParams.
  expect(sent).toBe('{"role":"technik"}');
});

test("no action of ngc.users is critical by the contract, so the surface asks on its own",
  async ({ page }) => {
    // THE MEASUREMENT BEHIND THE RULE. core.critical names only actions of
    // the app store; for every action of ngc.users it answers false. A
    // surface that asked core.critical and nothing else would hand out admin
    // rights on one click, without a word. The day the contract changes, this
    // case goes red and the rule above it is looked at again.
    const out = await page.evaluate(async () => {
      const core = await import("/assets/js/contracts/core.js");
      return core.ACTIONS.filter((a) => a.app === core.APP_USERS)
        .map((a) => a.name + "=" + core.critical(a, {}));
    });
    expect(out.every((s) => s.endsWith("=false"))).toBe(true);
    expect(out.length).toBeGreaterThan(5);
  });

// ------------------------------------------------------------------ 6 the log
test("the log pages with the time and the id of its last row", async ({ page }) => {
  await mount(page);
  await openTab(page, "Protokoll");
  const rows = app(page).locator(".log .row");
  await expect(rows).toHaveCount(50);
  // Time, user, kind, app, action, node and result, all as text.
  const first = rows.first();
  await expect(first.locator(".user")).toHaveText("admin");
  await expect(first.locator(".kind")).toHaveText("Aufgabe");
  await expect(first.locator(".app")).toHaveText("ngc.store");
  await expect(first.locator(".action")).toHaveText("activate");
  await expect(first.locator(".node")).toHaveText("ctl-1");
  await expect(first.locator(".result")).toHaveText("ok");
  await expect(first.locator(".time")).not.toHaveText("");

  await app(page).getByRole("button", { name: "Mehr laden" }).click();
  await expect(rows).toHaveCount(51);
  const asked = await page.evaluate(() => window.__ngcCalls
    .filter((c) => c.name === "log").map((c) => new TextDecoder().decode(c.params)));
  expect(asked[0]).toBe('{"limit":50}');
  // The second page starts below the LAST row of the first, by time AND by
  // id: two entries can share a second, and a page that asked by time alone
  // would lose or repeat them.
  expect(asked[1]).toBe(
    '{"beforeTime":1790003551,"beforeId":"00000000000000000000000000000049","limit":50}');

  // The second page was shorter than the limit, so that was the end of it.
  await expect(app(page).locator(".log .note")).toHaveText("Das ist der Anfang des Protokolls.");
  expect(await app(page).getByRole("button", { name: "Mehr laden" }).count()).toBe(0);
});

// ------------------------------------------------------------ users and tabs
test("the users tab shows every user as text", async ({ page }) => {
  await mount(page);
  const rows = app(page).locator(".users .row");
  await expect(rows).toHaveCount(3);
  await expect(rows.nth(2).locator(".nm")).toHaveText("anna");
  await expect(rows.nth(2).locator(".roles")).toHaveText("technik");
  await expect(rows.nth(2).locator(".locked")).toHaveText("gesperrt");
  await expect(rows.nth(1).locator(".last")).toHaveText("noch nie");
});

test("the four tabs can be walked with the keyboard alone", async ({ page }) => {
  await mount(page);
  const tabs = ["Benutzer", "Rollen und Rechte", "Server", "Protokoll"];
  await app(page).getByRole("tab", { name: "Benutzer", exact: true }).focus();
  for (let i = 1; i < tabs.length; i++) {
    await page.keyboard.press("ArrowRight");
    await expect(app(page).getByRole("tab", { name: tabs[i], exact: true }))
      .toHaveAttribute("aria-selected", "true");
  }
  // It comes round, and Home goes back to the first.
  await page.keyboard.press("ArrowRight");
  await expect(app(page).getByRole("tab", { name: "Benutzer", exact: true }))
    .toHaveAttribute("aria-selected", "true");
  await page.keyboard.press("End");
  await expect(app(page).getByRole("tab", { name: "Protokoll", exact: true }))
    .toHaveAttribute("aria-selected", "true");
  await page.keyboard.press("Home");
  await expect(app(page).getByRole("tab", { name: "Benutzer", exact: true }))
    .toHaveAttribute("aria-selected", "true");
  // Exactly one panel is shown, and it is the one the selected tab names.
  await expect(app(page).getByRole("tabpanel")).toHaveCount(1);
});

test("the question can be worked and left with the keyboard alone", async ({ page }) => {
  await mount(page);
  await openTab(page, "Rollen und Rechte");
  await app(page).getByRole("checkbox", { name: "Rolle technik: Fotos · teilen" }).check();
  await app(page).getByRole("button", { name: "Rechte der Rolle technik speichern" }).click();
  const dialog = page.getByRole("dialog");
  // The focus stands on Abbrechen, not on the button that hands out the
  // right: a stray Enter must not give anything away.
  await expect(dialog.getByRole("button", { name: "Abbrechen" })).toBeFocused();
  await expect(dialog).toHaveAttribute("aria-modal", "true");
  // And aria-modal="true" is not a word only: while the question stands, the
  // rest of the window is really out of reach, for a reader's own cursor too.
  await expect(app(page).locator(".panels")).toHaveAttribute("inert", "");
  await expect(app(page).locator(".tabs")).toHaveAttribute("inert", "");
  // Tab stays inside the question.
  await page.keyboard.press("Tab");
  await expect(dialog.getByRole("button", { name: "Rechte vergeben" })).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(dialog.getByRole("button", { name: "Abbrechen" })).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog")).toHaveCount(0);
  expect(await app(page).locator(".panels[inert], .tabs[inert]").count()).toBe(0);
  expect(await page.evaluate(() => window.__ngcCalls
    .filter((c) => c.name === "set-rights").length)).toBe(0);
  // And the focus is back where it came from.
  await expect(app(page).getByRole("button", { name: "Rechte der Rolle technik speichern" }))
    .toBeFocused();
});

test("every button of the surface has a name a screen reader can read", async ({ page }) => {
  await mount(page);
  for (const tab of ["Benutzer", "Rollen und Rechte", "Server", "Protokoll"]) {
    await openTab(page, tab);
    const nameless = await app(page).locator("button").evaluateAll((bs) => bs
      .filter((b) => !(b.getAttribute("aria-label") || b.textContent || "").trim()).length);
    expect(nameless, "nameless buttons on " + tab).toBe(0);
  }
});

test("an answer the contract refuses leaves the surface standing and says so", async ({ page }) => {
  await mount(page, { answers: { "ngc.users/list": [{ id: "zu-kurz", name: "admin",
    roles: ["admin"], totp: false, locked: false, lastLogin: 0 }] } });
  await expect(app(page).locator(".users .note")).toHaveText(
    "Die Antwort der Anlage war nicht in Ordnung. Es wird nichts angezeigt.");
  expect(await app(page).locator(".users .row").count()).toBe(0);
  // Still a surface: the other tabs work.
  await openTab(page, "Protokoll");
  await expect(app(page).locator(".log .row")).toHaveCount(50);
});

// ------------------------------------------------------------------- desktop
test("the app opens from the start menu", async ({ page }) => {
  await skipWithoutLogin(page);
  await signIn(page);
  await expect(page.locator("#taskbar")).toBeVisible();
  await page.getByRole("button", { name: "Start" }).click();
  await page.getByRole("menuitem", { name: "Benutzer und Rechte", exact: true }).click();
  const win = page.locator(".win", { hasText: "Benutzer und Rechte" });
  await expect(win.getByRole("tab", { name: "Benutzer", exact: true })).toBeVisible();
  await expect(win.locator(".users .row")).toHaveCount(3);
});

test("a question belongs to its window and goes with it", async ({ page }) => {
  await skipWithoutLogin(page);
  await signIn(page);
  await expect(page.locator("#taskbar")).toBeVisible();
  await page.getByRole("button", { name: "Start" }).click();
  await page.getByRole("menuitem", { name: "Benutzer und Rechte", exact: true }).click();
  const win = page.locator(".win", { hasText: "Benutzer und Rechte" });
  await win.getByRole("tab", { name: "Rollen und Rechte", exact: true }).click();
  await win.getByRole("checkbox", { name: "Rolle technik: Fotos · teilen" }).check();
  await win.getByRole("button", { name: "Rechte der Rolle technik speichern" }).click();
  // The question stands INSIDE the window, not over the desktop: a question
  // that outlived its window would be one nobody could tell apart from the
  // next window's.
  await expect(win.getByRole("dialog")).toBeVisible();
  await win.getByRole("button", { name: "Schließen" }).click();
  await expect(win).toHaveCount(0);
  expect(await page.getByRole("dialog").count()).toBe(0);
  // And nothing went out while the question was still open.
  expect(await page.evaluate(() => window.__ngcCalls
    .filter((c) => c.name === "set-rights").length)).toBe(0);
});

// THE ADMIN WHO LISTS A SERVER MAY RUN ROOT TASKS ON IT, and until 17.10.2026
// nobody could: both root lists of a newly listed controller were empty, and no
// surface of this interface ever set them. trust.MayRoot then answers false for
// every user, so activate, install, uninstall, delete-data and the terminal all
// come back E_FORBIDDEN -- the server stands in the list and can do nothing.
// Measured on a running installation that day: the package was uploaded, the
// node was offered as "ngc-2", and activating it answered "Keine Berechtigung."
//
// It reads the VERSION THAT WAS SENT, not the screen: who may do what is the
// list's business, and a sentence in the interface proves nothing about it.
test("the listed server grants root tasks to the admin who listed it, and to nobody else", async ({ page }) => {
  await skipWithoutLogin(page);
  await mount(page, { signable: true });
  await openTab(page, "Server");
  await app(page).getByRole("button", { name: "Server dienste aufnehmen" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible();
  // The person is told what they are being given: a right nobody was told
  // about is a right nobody can weigh.
  await expect(dialog).toContainText("Systemrechten");
  await dialog.getByRole("button", { name: "Server aufnehmen" }).click();
  await sign(page);
  await expect(app(page).locator(".pending .note")).toHaveText("Der Server »dienste« ist aufgenommen.");

  const sent = await page.evaluate(async () => {
    const trust = await import("/assets/js/contracts/trust.js");
    const s = trust.parseSigned(window.__ngcSent[0].bytes);
    const l = await trust.parseList(s.payload);
    const node = l.nodes.find((n) => n.id === "dienste");
    return {
      // WHOSE SESSION THIS IS, asked of the page and not written down
      // here: the assurance is "the admin who listed it", so the value
      // has to come from the same place the app read it from.
      me: window.__ngcCtx.session.user,
      rootUsers: node.rootUsers ?? [],
      rootRoles: node.rootRoles ?? [],
      // Every other controller is left exactly as it WAS -- not empty,
      // unchanged: ctl-1 of this list already carries root roles, and
      // taking one server on says nothing about another. So the two are
      // held against the version in force, never against a value written
      // down here.
      others: l.nodes.filter((n) => n.kind === "controller" && n.id !== "dienste")
        .map((n) => ({ id: n.id, users: n.rootUsers ?? [], roles: n.rootRoles ?? [] })),
      before: window.__ngcCtx.trust.current().list.nodes
        .filter((n) => n.kind === "controller")
        .map((n) => ({ id: n.id, users: n.rootUsers ?? [], roles: n.rootRoles ?? [] })),
    };
  });
  expect(typeof sent.me, "the session names its user").toBe("string");
  expect(sent.me.length, "and it is not empty").toBeGreaterThan(0);
  expect(sent.rootUsers).toEqual([sent.me]);
  // A ROLE WOULD BE THE WIDER WAY and is not what this does: it would hand
  // every present and future member of that role root on this machine.
  expect(sent.rootRoles).toEqual([]);
  expect(sent.before.length, "the list in force has controllers to compare against")
    .toBeGreaterThan(0);
  for (const o of sent.others) {
    const was = sent.before.find((b) => b.id === o.id);
    expect(was, `${o.id} stood in the version before`).toBeTruthy();
    expect(o.users, `${o.id} was left alone`).toEqual(was.users);
    expect(o.roles, `${o.id} was left alone`).toEqual(was.roles);
  }
});

// The other half, and it must NOT be made to match: an API node may carry no
// root lists at all (Go: ValidAPINode asks for len(RootUsers)+len(RootRoles)
// == 0), and trust.MayRoot only ever looks at a node of kind controller. A
// version that named anybody there would be refused as invalid.
test("a listed API server carries no root lists at all", async ({ page }) => {
  await skipWithoutLogin(page);
  await mount(page, { signable: true });
  await openTab(page, "Server");
  await app(page).getByRole("button", { name: "Server api-2 aufnehmen" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Server aufnehmen" }).click();
  await sign(page);
  await expect(app(page).locator(".pendapi .note"))
    .toHaveText("Der Server »api-2« ist aufgenommen.");
  const sent = await page.evaluate(async () => {
    const trust = await import("/assets/js/contracts/trust.js");
    const one = window.__ngcSent[window.__ngcSent.length - 1];
    const s = trust.parseSigned(one.bytes);
    const l = await trust.parseList(s.payload);
    const node = l.nodes.find((n) => n.id === "api-2");
    return { users: node.rootUsers ?? [], roles: node.rootRoles ?? [] };
  });
  expect(sent.users).toEqual([]);
  expect(sent.roles).toEqual([]);
});

import { test, expect } from "@playwright/test";
import { hasSessionKeys } from "../fixtures/page.js";

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
  const { decode, Budget } = await import("/assets/js/contracts/json.js");
  const trust = await import("/assets/js/contracts/trust.js");
  const payload = b64decode(GENESIS_PAYLOAD);
  const budget = new Budget({ size: 4 << 20, items: {
    "rp/origins": 8, users: 10000, "users/*/roles": 256, "users/*/keys": 16,
    roles: 256, "roles/*/black": 256, "roles/*/white": 256,
    nodes: 256, "nodes/*/rootUsers": 256, "nodes/*/rootRoles": 256, stores: 64, auto: 64,
  } });
  const list = decode(payload, budget, trust.listShape);
  return { list, hash: await trust.hash({ payload }), payload };
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

// world builds the link, the verified version and the session the app is
// given. cfg replaces single answers; everything it does not name stays as
// DEFAULTS has it.
export async function world(cfg = {}) {
  const answers = { __proto__: null, ...DEFAULTS, ...cfg.answers };
  const notices = new Set();
  const version = cfg.noVersion === true ? null
    : cfg.signable === true ? await signableVersion() : await genesis();
  if (answers["ngc.users/pair-code"] === null && version !== null) {
    answers["ngc.users/pair-code"] = {
      code: await pairCode(cfg.codeFor === "next" ? await nextVersion(version) : version),
      expires: 1790086400,
    };
  }
  // What the remote received. The cases read these instead of guessing from
  // the screen: the bytes are the contract, the screen is only its face.
  window.__ngcCalls = [];
  window.__ngcSent = [];
  window.__ngcRefreshed = 0;

  let logPage = 0;
  const link = {
    async call(app, service, name, params) {
      window.__ngcCalls.push({ app, service, name, params: params ? new Uint8Array(params) : null });
      if (app === "ngc.desktop" && name === "apps") return enc.encode(JSON.stringify(APPS));
      let key = app + "/" + name;
      if (key === "ngc.users/log") {
        logPage++;
        if (logPage > 1) key = "ngc.users/log/" + logPage;
      }
      const a = answers[key];
      if (a === undefined) throw new Error("world: nothing for " + key);
      if (a instanceof Error) throw a;
      return enc.encode(typeof a === "string" ? a : JSON.stringify(a));
    },
    // The one channel that stays open and that the app writes on:
    // ngc.users/api/trust, flow "in". It collects what was sent and answers
    // with the result of the action.
    async open(app, service, name) {
      const parts = [];
      const s = {
        onData: null, onClose: null,
        send(bytes) { parts.push(new Uint8Array(bytes)); },
        end() {
          let n = 0;
          for (const p of parts) n += p.length;
          const all = new Uint8Array(n);
          let at = 0;
          for (const p of parts) { all.set(p, at); at += p.length; }
          window.__ngcSent.push({ app, service, name, bytes: all });
          const a = answers[app + "/" + name];
          setTimeout(() => {
            s.onData?.(enc.encode(JSON.stringify(a === undefined ? {} : a)));
            s.onClose?.(window.__ngcCloseWith ?? "ok", "");
          }, 0);
        },
        close() {},
      };
      return s;
    },
    onNotice(fn) { if (typeof fn === "function") notices.add(fn); },
  };

  let current = version;
  const trust = {
    current: () => current,
    async refresh() {
      window.__ngcRefreshed++;
      if (cfg.refreshTo === "next") current = await nextVersion(version);
      return current;
    },
  };
  if (cfg.noRefresh === true) delete trust.refresh;

  const session = cfg.noSession === true ? null : {
    now: () => 1790100000,
    async prove(purpose, payload) {
      const s = await signer();
      return s(purpose, payload);
    },
  };
  return { link, trust, session, apps: APPS };
}

// signableVersion is the same installation with one admin: the key this file
// made. Only then can the shell's own trust.advance check what it just
// signed, which is what buildNextVersion does before anything goes out.
async function signableVersion() {
  const trust = await import("/assets/js/contracts/trust.js");
  const g = await genesis();
  const s = await signer();
  const list = structuredClone(g.list);
  list.users = [{ id: "ISEhISEhISEhISEhISEhIQ", name: "admin", roles: ["admin"], keys: [{
    id: s.kid, kind: "password", alg: "EdDSA", pub: s.pub, credId: new Uint8Array(0),
    kdf: { alg: "argon2id", salt: new Uint8Array(16).fill(1), m: 65536, t: 3, p: 1 },
  }] }];
  // No passkey in this list, so it names no party to ask one of.
  list.rp = { id: "", origins: [] };
  const payload = trust.encodeList(list);
  return { list, hash: await trust.hash({ payload }), payload };
}

// nextVersion is version 2 of the same installation, built the way the shell
// builds one: it is only ever used to hand the app a pairing code that names
// a number the shell does not have yet.
async function nextVersion(version) {
  const trust = await import("/assets/js/contracts/trust.js");
  const next = structuredClone(version.list);
  next.seq = version.list.seq + 1;
  next.prev = version.hash;
  next.issued = 1790050000;
  const payload = trust.encodeList(next);
  return { list: next, hash: await trust.hash({ payload }), payload };
}

// signer makes an Ed25519 password key and signs with it. Only the cases
// that really sign a version reach it, and those skip where Ed25519 is not
// there.
let made = null;
export async function signer() {
  if (made) return made;
  const sign = await import("/assets/js/contracts/sign.js");
  const proof = await import("/assets/js/contracts/proof.js");
  const seed = crypto.getRandomValues(new Uint8Array(32));
  const priv = await proof.privateFromSeed(seed);
  const pub = await proof.publicFromSeed(seed);
  const kid = await sign.keyIDAsync(pub);
  made = async (purpose, payload) => ({
    kind: "password", kid,
    sig: new Uint8Array(await crypto.subtle.sign({ name: "Ed25519" }, priv,
      sign.message(purpose, payload))),
    authData: new Uint8Array(0), clientData: new Uint8Array(0),
  });
  made.pub = pub;
  made.kid = kid;
  return made;
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
  await page.evaluate(async (cfg) => {
    const { usersApp } = await import("/assets/js/apps/users.js");
    const { world } = await import("/assets/js/shell/fake-link.js");
    const ctx = await world(cfg);
    const host = document.createElement("div");
    host.id = "mount";
    Object.assign(host.style, { position: "fixed", inset: "0", zIndex: "99999", overflow: "auto" });
    host.className = "body";
    document.body.append(host);
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
test("joining a controller builds version N+1, checks it and sends exactly those bytes",
  async ({ page }) => {
    test.skip(!(await hasSessionKeys(page)),
      "no Ed25519 in this engine's Web Crypto: nothing here can sign a version");
    const out = await page.evaluate(async () => {
      const users = await import("/assets/js/apps/users.js");
      const trust = await import("/assets/js/contracts/trust.js");
      const { world, signer } = await import("/assets/js/shell/fake-link.js");
      const sign = await import("/assets/js/contracts/sign.js");
      const { b64decode, b64encode, equal } = await import("/assets/js/contracts/b64.js");

      // A version of this installation whose admin is the key this test
      // holds, so the shell's own advance() can check what it just signed.
      const w = await world({});
      const s = await signer();
      const base = structuredClone(w.trust.current().list);
      base.users = [{ id: "ISEhISEhISEhISEhISEhIQ", name: "admin", roles: ["admin"], keys: [{
        id: s.kid, kind: "password", alg: "EdDSA", pub: s.pub, credId: new Uint8Array(0),
        kdf: { alg: "argon2id", salt: new Uint8Array(16).fill(1), m: 65536, t: 3, p: 1 },
      }] }];
      base.rp = { id: "", origins: [] }; // no passkey in this list, so no party
      if (!trust.validateList(base)) return { error: "the base version is not a valid list" };
      const payload = trust.encodeList(base);
      const version = { list: base, hash: await trust.hash({ payload }) };

      const built = await users.buildNextVersion(version, (next) => {
        next.nodes.push({
          id: "dienste", kind: "controller", name: "dienste",
          noise: b64decode("wcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcE"),
          sign: new Uint8Array(0), url: "", tls: new Uint8Array(0),
          rootUsers: [], rootRoles: [],
        });
      }, { now: 1790100000, signer: (b) => s(sign.PURPOSE_TRUST_LIST, b) });

      const got = await trust.parseList(built.payload);
      const before = structuredClone(base);
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
  test.skip(!(await hasSessionKeys(page)),
    "no Ed25519 in this engine's Web Crypto: nothing here can sign a version");
  const out = await page.evaluate(async () => {
    const users = await import("/assets/js/apps/users.js");
    const trust = await import("/assets/js/contracts/trust.js");
    const { world, signer } = await import("/assets/js/shell/fake-link.js");
    const w = await world({});
    const s = await signer();
    let signed = 0;
    const version = { list: w.trust.current().list, hash: w.trust.current().hash };
    try {
      await users.buildNextVersion(version, (next) => {
        // Two nodes with the same id: trust.List.Validate refuses it.
        next.nodes.push({ ...next.nodes[0] });
      }, { now: 1790100000, signer: async (b) => { signed++; return s("trustlist/v1", b); } });
      return { threw: "", signed };
    } catch (e) {
      return { threw: e.message, signed };
    }
  });
  expect(out.threw).toBe("Diese Änderung ergibt keine gültige Vertrauensliste.");
  expect(out.signed).toBe(0);
});

test("the trust list goes over ngc.users/api/trust as one signed version", async ({ page }) => {
  test.skip(!(await hasSessionKeys(page)),
    "no Ed25519 in this engine's Web Crypto: nothing here can sign a version");
  await mount(page, { signable: true });
  await openTab(page, "Server");
  await app(page).getByRole("button", { name: "Server dienste aufnehmen" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "Server aufnehmen" }).click();
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
    };
  });
  expect(sent.count).toBe(1);
  expect(sent.where).toBe("ngc.users/api/trust");
  expect(sent.seq).toBe(2);
  expect(sent.nodes).toEqual(["api-1", "ctl-1", "dienste"]);
  expect(sent.kind).toBe("password");
});

// ------------------------------------------------------------------ 4 pair code
test("a pairing code is shown only when it fits the version the shell verified",
  async ({ page }) => {
    await mount(page);
    await openTab(page, "Server");
    await app(page).getByRole("button", { name: "Kopplungscode zeigen" }).click();
    const code = app(page).locator(".paircode .code");
    await expect(code).toContainText("ngc-pair:");
    // No round trip to the trust list was needed: the code names the version
    // the shell already holds.
    expect(await page.evaluate(() => window.__ngcRefreshed)).toBe(0);
  });

test("a pairing code that names a newer version makes the shell fetch the chain first",
  async ({ page }) => {
    await mount(page, { codeFor: "next", refreshTo: "next" });
    await openTab(page, "Server");
    await app(page).getByRole("button", { name: "Kopplungscode zeigen" }).click();
    await expect(app(page).locator(".paircode .code")).toContainText("ngc-pair:");
    expect(await page.evaluate(() => window.__ngcRefreshed)).toBe(1);
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
  test.skip(!(await hasSessionKeys(page)),
    "no Ed25519 and no X25519 in this engine's Web Crypto: the shell stops before the desktop");
  await expect(page.locator("#taskbar")).toBeVisible();
  await page.getByRole("button", { name: "Start" }).click();
  await page.getByRole("menuitem", { name: "Benutzer und Rechte", exact: true }).click();
  const win = page.locator(".win", { hasText: "Benutzer und Rechte" });
  await expect(win.getByRole("tab", { name: "Benutzer", exact: true })).toBeVisible();
  await expect(win.locator(".users .row")).toHaveCount(3);
});

test("a question belongs to its window and goes with it", async ({ page }) => {
  test.skip(!(await hasSessionKeys(page)),
    "no Ed25519 and no X25519 in this engine's Web Crypto: the shell stops before the desktop");
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

// A STAND-IN. THIS FILE IS A FAKE AND IS MEANT TO BE DELETED.
//
// It answers with fixed bytes so the desktop has apps to show before anything
// can be asked of a server. It talks to no API, and it must never carry a
// single line that the real thing would need.
//
// WHEN IT GOES: with Task 17, which builds shell/channel.js - the session to
// the API, the Noise tunnel and the channels - and with Task 20, which opens
// the trust list. Then main.js hands startDesktop a channel and the opened
// list instead of this, DELETES this file, and neither the desktop nor the
// app frame is touched at all: that is what the shapes below are for.
//
// WHAT THE REAL ONE HAS TO DO, and this one does not:
//   - call() must open a channel, send the OPEN, collect the result up to
//     wire.MAX_CALL_RESULT bytes and only hand it back when the channel closed
//     with "ok"; anything else is thrown away.
//   - open() must be a channel that stays open, with credit, EOF and CLOSE.
//   - notices must come from the "notify" control message of that session.
//   - the trust list must be the one Task 20 opened against the anchor and
//     followed to its newest version, not the one built here.
//   - it must live at shell/channel.js, and this file must be gone.
//
// WHAT IS NOT FAKE HERE, and the reason this file grew: the app frame runs an
// app's user interface only against a SIGNED checksum, so a stand-in that
// handed it a made-up proof would test nothing at all. So the store key below
// is a real Ed25519 key this page makes, the catalog carries a real signature
// over the real SHA-256 of the real ui-tar, and the shell checks all of it
// with core.parseUIProof and core.verifyUIProof. The one thing standing in is
// WHERE THE STORE KEY COMES FROM: a trust list is opened against the anchor
// and followed version by version (Task 20), and until that exists this hands
// over a list holding that one store and nothing else.
//
// WHAT STAYS TRUE EITHER WAY: it hands back BYTES. Everything that arrives is
// read by the contracts in contracts/ - core.parseResult, core.parseNotice,
// uitar.walkUI - inside the desktop and the app frame, never here.
//
// It is not in contracts/ on purpose: nothing in there is a fake.
import { b64encode } from "../contracts/b64.js";
import { UIWriter } from "../contracts/uitar.js";
import { keyIDAsync, message as signedMessage, PURPOSE_CATALOG } from "../contracts/sign.js";
import { validStore } from "../contracts/trust.js";
import { sha256 } from "../../vendor/noble-hashes/sha2.js";

const enc = new TextEncoder();

// The app whose user interface really runs in the frame. Its id is NOT
// "ngc.terminal": core.AppParams refuses a core app ("ngc.") for both
// ngc.desktop/ui and ngc.desktop/ui-proof, so an app of NGC's own can never
// be asked for a user interface at all. shared/testdata calls it
// org.sorglos.terminal, and so does this.
const TERMINAL = "org.sorglos.terminal";
const STORE = "global";
const VERSION = "1.0.0";

// The apps the stand-in reports for "ngc.desktop/apps". Every field of
// core.DesktopApp stands here, because these bytes go through
// core.parseResult like the real answer and a missing field is refused.
const APPS = [
  {
    id: "ngc.store", name: "App-Store", version: "1.0.0", icon: "", entry: "",
    width: 760, height: 520, actions: [], state: "ok", core: true, node: "",
  },
  {
    id: "ngc.users", name: "Benutzer und Rechte", version: "1.0.0", icon: "", entry: "",
    width: 760, height: 520, actions: [], state: "ok", core: true, node: "",
  },
  {
    id: "ngc.calc", name: "Rechner", version: "1.0.0", icon: "", entry: "",
    width: 300, height: 420, actions: [], state: "ok", core: false, node: "",
  },
  {
    id: TERMINAL, name: "Terminal", version: VERSION, icon: "", entry: "index.html",
    width: 600, height: 360, actions: ["shell"], state: "ok", core: false, node: "pi-1",
  },
  // An app that is installed but whose controller does not answer: the desktop
  // says so and opens no window.
  {
    id: "de.example.fotos", name: "Fotos", version: "2.1.0", icon: "", entry: "",
    width: 640, height: 480, actions: [], state: "unreachable", core: false, node: "pi-2",
  },
  // An app whose licence has run out.
  {
    id: "de.example.buchhaltung", name: "Buchhaltung", version: "3.0.1", icon: "", entry: "",
    width: 640, height: 480, actions: [], state: "unlicensed", core: false, node: "pi-1",
  },
  // A name that tries to be markup. It is a legal name by the contract - the
  // characters are all printable - so nothing refuses it, and the desktop has
  // to put it on screen as the text it is.
  {
    id: "de.example.boese", name: "<img src=x onerror=alert(1)>Böse", version: "0.1.0",
    icon: "", entry: "", width: 400, height: 300, actions: [], state: "ok", core: false, node: "",
  },
  // "Geheim" is deliberately absent: the API only lists what this user may
  // use, and leaving it out is how that is done.
];

// The user interface of the terminal, the files as they would lie in a
// package. It is deliberately ordinary: a page, a stylesheet with a picture
// in it, and a classic script that asks its own service through ngc.js.
const PAGE = '<!DOCTYPE html><html lang="de"><head><meta charset="utf-8"><title>Terminal</title>'
  + '<link rel="stylesheet" href="app.css"></head><body><h1>Terminal</h1>'
  + '<p id="antwort">…</p><script src="app.js"></scr' + "ipt></body></html>";
const STYLE = "body{font-family:system-ui,sans-serif;margin:0;padding:16px;"
  + "background:url(punkt.gif) repeat}h1{font-size:18px;margin:0 0 8px}";
const SCRIPT = 'ngc.call("terminal", "shell", { text: "hallo" }).then(function (bytes) {'
  + 'document.getElementById("antwort").textContent = new TextDecoder().decode(bytes);'
  + "});";
// A 1x1 GIF, so the url(...) in the stylesheet points at something real.
const DOT = Uint8Array.from(
  atob("R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7"), (c) => c.charCodeAt(0));

// A ui-tar as store.UIWriter writes one. The entry page is named by the
// caller so that the test hook can leave the manifest's "index.html" out of
// the archive, and the script can be made a module.
function uiTar({ entry = "index.html", module: asModule = false } = {}) {
  const w = new UIWriter();
  w.dir("ui");
  w.file("ui/" + entry, enc.encode(asModule ? PAGE.replace("<script ", '<script type="module" ') : PAGE));
  w.file("ui/app.css", enc.encode(STYLE));
  w.file("ui/app.js", enc.encode(SCRIPT));
  w.file("ui/punkt.gif", DOT);
  return w.close();
}

// The store's key, its trust list and the signed catalog over the checksum of
// one ui-tar. Real cryptography, made in this page: see the head of the file
// for what that does and does not stand for.
async function world() {
  const pair = await crypto.subtle.generateKey({ name: "Ed25519" }, true, ["sign", "verify"]);
  const pub = new Uint8Array(await crypto.subtle.exportKey("raw", pair.publicKey));
  const kid = await keyIDAsync(pub);
  const store = { id: STORE, name: "NGC Store", url: "https://store.ngc.test", key: pub };
  // The contract's own rule on a store entry, asked here so that a stand-in
  // cannot quietly hand the shell something no trust list would hold.
  if (!validStore(store)) throw new Error("fake-link: this store would not stand in a trust list");

  const proofFor = async (uiHash) => {
    const catalog = enc.encode(JSON.stringify({
      store: STORE, seq: 1, issued: 1790000000,
      apps: [{
        id: TERMINAL, name: "Terminal", publisher: "Sorglos IT",
        versions: [{
          version: VERSION, url: TERMINAL + "-" + VERSION + ".ngc", size: 5000000,
          sha256: b64encode(new Uint8Array(32).fill(2)), ui: b64encode(uiHash),
        }],
      }],
    }));
    const sig = new Uint8Array(await crypto.subtle.sign(
      { name: "Ed25519" }, pair.privateKey, signedMessage(PURPOSE_CATALOG, catalog)));
    return enc.encode(JSON.stringify({
      app: TERMINAL, version: VERSION, store: STORE,
      catalog: { payload: b64encode(catalog), kid, sig: b64encode(sig) },
    }));
  };
  return { store, proofFor };
}

// A TEST HOOK, and it leaves with this file. It steers what the stand-in
// serves, so that the frame's refusals can be measured against a stand-in
// that is otherwise entirely in order.
//
//   swapUI - serve a DIFFERENT archive than the one the proof vouches for.
//   entry  - put the entry page in the archive under this name, so the
//            manifest's "index.html" is not in it.
//   module - ship the entry page with a module script.
//
// The proof is always signed over the archive the hooks produced, except for
// swapUI: otherwise a case meant for the entry rule would die at the checksum
// and prove nothing about the rule it is named after.
function hooks() {
  const t = window.__ngcTest ?? {};
  return { swap: t.swapUI === true, entry: typeof t.entry === "string" && t.entry !== "" ? t.entry : "index.html", module: t.module === true };
}

// fakeWorld builds the stand-in: the link the desktop asks, and the trust
// list the app frame checks a proof against. The shapes are the ones
// shell/channel.js and shell/trust.js will have - call() and open() hand back
// bytes, onNotice() takes a listener that is handed bytes, current() answers
// with the list in force - so neither the desktop nor the frame changes when
// the real ones land.
//
// It is a promise because the store key is a real key and making one is
// asynchronous. Task 20 opens the trust list before the desktop starts for
// the same reason: nothing may be shown before it is known what may be shown.
export async function fakeWorld() {
  const notices = new Set();
  const { store, proofFor } = await world();

  window.__ngcTest = { swapUI: false, entry: "", module: false };
  // A TEST HOOK as well: how many channels the shell has opened. The real
  // session counts its own channels and needs no such number.
  window.__ngcChannelsOpened = 0;

  const results = {
    __proto__: null,
    "ngc.desktop/apps": async () => enc.encode(JSON.stringify(APPS)),
    "ngc.desktop/ui-proof": async () => {
      const h = hooks();
      return await proofFor(sha256(uiTar({ entry: h.entry, module: h.module })));
    },
    // The terminal's own service. What it answers means whatever the app and
    // its service agreed; the shell passes the bytes through untouched.
    [TERMINAL + "/shell"]: async () => enc.encode("hallo aus dem Dienst"),
  };

  const link = {
    async call(app, service, name) {
      window.__ngcChannelsOpened++;
      const make = results[`${app}/${name}`];
      if (make === undefined) throw new Error(`fake-link: nothing for ${app}/${service}/${name}`);
      return await make();
    },
    // A stand-in for a channel that stays open: it hands over the whole
    // answer in one piece and closes. The real one arrives in frames.
    async open(app, service, name) {
      window.__ngcChannelsOpened++;
      if (`${app}/${name}` !== "ngc.desktop/ui") {
        throw new Error(`fake-link: no stream for ${app}/${service}/${name}`);
      }
      const h = hooks();
      const bytes = h.swap
        ? uiTar({ entry: "andere.html" })
        : uiTar({ entry: h.entry, module: h.module });
      const s = { onData: null, onClose: null, close() {} };
      // After the caller has hung its two listeners up, never before.
      setTimeout(() => {
        s.onData?.(bytes);
        s.onClose?.("ok", "");
      }, 0);
      return s;
    },
    onNotice(fn) {
      if (typeof fn === "function") notices.add(fn);
    },
  };

  // A TEST HOOK, and it leaves with this file. The desktop's test sends a
  // notice with it; the real session gets them over the wire and needs no
  // hook. It is bytes, so the desktop reads them with core.parseNotice
  // exactly as it will read the real ones.
  window.__ngcSendNotice = (level, text) => {
    const body = enc.encode(JSON.stringify({ level, text, app: "" }));
    for (const fn of notices) fn(body);
  };

  // The stand-in for shell/trust.js (Task 20): the verified trust list the
  // frame checks a UI proof against.
  //
  // IT IS NOT A TRUST LIST. It holds the one store whose key signed the
  // catalog above, because that is all core.verifyUIProof reads for an app of
  // a store. A real list is opened against the anchor, carries users, roles
  // and nodes and passes trust.validateList - none of which exists yet.
  const trust = { current: () => ({ list: { stores: [store] } }) };

  return { link, trust };
}

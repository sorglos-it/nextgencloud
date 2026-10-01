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
// uitar.walkUI, catalog.verifyCatalog - inside the desktop, the app frame and
// the store, never here.
//
// SINCE TASK 23 it also answers the store's actions, and for that it carries
// three more stand-ins: a trust list the contract accepts (so the store can
// write the next version of it and read its own work back), a session with a
// real signing key (so a task the store signs is really signed), and the
// release key of the initial setup. All three go the way this file goes; what
// the store asks of them - current(), inst, certHash, signKey - is what Task
// 17, 19 and 20 will hand it.
//
// It is not in contracts/ on purpose: nothing in there is a fake.
import { b64encode } from "../contracts/b64.js";
import { UIWriter } from "../contracts/uitar.js";
import { keyIDAsync, message as signedMessage, PURPOSE_CATALOG } from "../contracts/sign.js";
import { encodeList, hash as listHash, parseList, parseSigned, validStore } from "../contracts/trust.js";
import { deriveSeed, publicFromSeed } from "../contracts/proof.js";
import { packageUI } from "../contracts/pack.js";
import { sha256 } from "../../vendor/noble-hashes/sha2.js";

const enc = new TextEncoder();

// The two apps that ship with NGC and really run in the frame. Their ids are
// NOT "ngc.terminal" and "ngc.calc": core.AppParams refuses a core app ("ngc.")
// for both ngc.desktop/ui and ngc.desktop/ui-proof, so an app of NGC's own can
// never be asked for a user interface at all. shared/testdata calls them
// org.sorglos.terminal and org.sorglos.calculator, and so does this.
//
// WHAT STANDS IN HERE SINCE TASK 25: both are apps a person ACTIVATES (design
// 4.2 and 4.4 - "Zum Aktivieren: Rechner und Terminal"), so in operation their
// user interfaces arrive as the ui/ of an app package, over ngc.desktop/ui,
// against a checksum a store's catalog signed. The source of those files is the
// web interface's own (public/assets/apps/<app>/, design 22.09.2026: "ihre
// Oberflächen baut das Webinterface"), and nothing in this repository packs them
// into a release yet. So this stand-in reads those very files and packs them,
// and the shell checks the result exactly as it checks an app out of a store.
const TERMINAL = "org.sorglos.terminal";
const CALCULATOR = "org.sorglos.calculator";
// Which folder under public/assets/apps/ holds which app's ui/ files.
const SHIPPED = new Map([[TERMINAL, "terminal"], [CALCULATOR, "calculator"]]);
// The three files each of them ships. The entry page first, because that is
// the one the manifest names.
const SHIPPED_FILES = ["index.html", "app.css", "app.js"];
const STORE = "global";
const VERSION = "1.0.0";

// The password of the one admin of this world, where a case asks for a key
// that really derives from one (knob passwordKey). It is written here so the
// case and the world cannot drift apart over it.
export const PASSWORD = "geheim";

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
  // The calculator has no service and no node: it reckons in the browser, and
  // the one right there is to use it at all (shared/testdata:
  // "org.sorglos.calculator/use").
  {
    id: CALCULATOR, name: "Rechner", version: VERSION, icon: "", entry: "index.html",
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

// readShipped fetches the ui/ files of both shipped apps off the shell's own
// server, once, before the desktop starts. They are the SAME bytes a release
// would pack, so a checksum over them is a checksum over what runs.
//
// It is the one thing this file fetches, and it is a stand-in for a release
// step and not for a server: in operation these files arrive inside an app
// package, and whoever builds that package reads them from the same place.
async function readShipped() {
  const out = new Map();
  for (const [id, dir] of SHIPPED) {
    const files = new Map();
    for (const name of SHIPPED_FILES) {
      const res = await fetch(`/assets/apps/${dir}/${name}`, { cache: "no-store" });
      if (!res.ok) throw new Error(`fake-link: no ${dir}/${name}`);
      files.set(name, new Uint8Array(await res.arrayBuffer()));
    }
    out.set(id, files);
  }
  return out;
}

// A ui-tar as store.UIWriter writes one, out of one app's files. The entry page
// is named by the caller so that the test hook can leave the manifest's
// "index.html" out of the archive, and the script can be made a module.
function uiTar(files, { entry = "index.html", module: asModule = false } = {}) {
  const w = new UIWriter();
  w.dir("ui");
  const page = files.get("index.html");
  w.file("ui/" + entry, asModule
    // The hook makes the page's one script a module, which is the one thing the
    // frame refuses outright. The replacement is done on the text, which is all
    // right for a hook that leaves with this file - the page it is done to is
    // ours, and what it has to produce is a page the frame turns down.
    ? enc.encode(new TextDecoder().decode(page).replace("<script ", '<script type="module" '))
    : page);
  for (const name of SHIPPED_FILES) {
    if (name !== "index.html") w.file("ui/" + name, files.get(name));
  }
  return w.close();
}

// WHAT THE TERMINAL'S SERVICE ANSWERS, AND WHAT IT REALLY IS. The service does
// not exist here and cannot: the controller builds it (apps/server-controller/,
// "virtuelles Terminal"), it runs the command inside the app's own container,
// and its answer is whatever the program there wrote to stdout. So this is the
// thinnest stand-in that lets the app's side be measured at all - the error a
// real `cp -r` gives, and the line a shell gives a word it does not know.
//
// WHAT IS THEREFORE NOT MEASURED ANYWHERE: that a real service's output arrives
// at all, how it is split over the wire, and what `cp -r` really says on the
// machine. All three need the controller's service.
const SHELL_ANSWERS = new Map([
  ["cp -r", "cp: missing destination file operand after '-r'\n"],
]);

function shellAnswer(line) {
  const cmd = line.replace(/\n+$/, "");
  if (cmd === "") return "";
  return SHELL_ANSWERS.get(cmd) ?? `sh: ${cmd}: command not found\n`;
}

// terminalStream stands in for the channel of that service: it answers every
// line that is written to it and stays open until the app ends it or the window
// takes it away. That is the one thing about it that is not a fake - a terminal
// is a channel that stays open, and the shell has to carry one.
function terminalStream(h) {
  const dec = new TextDecoder();
  const s = {
    onData: null,
    onClose: null,
    write(bytes) {
      // The hook wins over the table: a case about what the shell and the app do
      // with the bytes of a service has to be able to choose those bytes.
      const answer = h.output !== "" ? h.output : shellAnswer(dec.decode(bytes));
      if (answer !== "") s.onData?.(enc.encode(answer));
    },
    end() {
      s.onClose?.("ok", "");
    },
    close() {
      // A TEST HOOK, and it leaves with this file: how many of these channels
      // the shell has given up. A real channel is counted by the session.
      window.__ngcStreamsClosed++;
    },
  };
  // A controller that fell over: the channel ends without an "ok". The text is
  // the one wire.message gives for E_UNREACHABLE; the real close carries the
  // code as well, and nothing on the app's side reads either.
  if (h.cut) setTimeout(() => s.onClose?.("error", "Dienst nicht erreichbar."), 0);
  return s;
}

// The apps the catalog of the store lists. The terminal and the calculator have
// to stand here at the version and with the ui hash the UI proof names, because
// the app frame looks their hash up in exactly this document (core.catalogUI).
// The other two are there so the store has something to show: a free app and a
// paid one, which is the one difference the Apps tab has to make.
//
// Sorted by id and versions ascending, because store.VerifyCatalog refuses a
// catalog that is not - a stand-in that broke its own contract would test the
// shell against a document no store could ever send.
function catalogDoc(uiHashes, evilName) {
  const pkg = (version, ui, encrypted) => ({
    version, url: "pakete/" + version + ".ngc", size: 5000000,
    sha256: b64encode(new Uint8Array(32).fill(2)), ui: b64encode(ui),
    encrypted, needs: [], minNgc: "",
  });
  return {
    store: STORE, seq: 1, issued: 1790000000,
    apps: [
      {
        // A name that tries to be markup, on a knob: the catalog is outside,
        // so its names are text and nothing else.
        id: "de.example.notizen",
        name: evilName ? "<img src=x onerror=alert(1)>Böse" : "Notizen",
        publisher: "Beispiel GmbH",
        description: "Notizen schreiben und wiederfinden.",
        versions: [pkg("1.0.0", new Uint8Array(32).fill(3), false)],
      },
      {
        id: CALCULATOR, name: "Rechner", publisher: "Sorglos IT",
        description: "Rechnen mit den vier Grundrechenarten.",
        versions: [pkg(VERSION, uiHashes.get(CALCULATOR), false)],
      },
      {
        id: TERMINAL, name: "Terminal", publisher: "Sorglos IT",
        description: "Eine Kommandozeile im Browser.",
        versions: [pkg(VERSION, uiHashes.get(TERMINAL), false)],
      },
      {
        id: "org.sorglos.tresor", name: "Tresor", publisher: "Sorglos IT",
        description: "Passwörter sicher aufbewahren.",
        price: {
          model: "buy", per: "site", amount: 4900, currency: "EUR", period: "",
          shop: "https://store.ngc.test/tresor",
        },
        versions: [pkg("2.0.0", new Uint8Array(32).fill(4), true)],
      },
    ],
  };
}

// The store's key, its trust list and the signed catalog over the checksum of
// one ui-tar. Real cryptography, made in this page: see the head of the file
// for what that does and does not stand for.
async function world(knobs) {
  const pair = await crypto.subtle.generateKey({ name: "Ed25519" }, true, ["sign", "verify"]);
  const pub = new Uint8Array(await crypto.subtle.exportKey("raw", pair.publicKey));
  const kid = await keyIDAsync(pub);
  const store = { id: STORE, name: "NGC Store", url: "https://store.ngc.test", key: pub };
  // The contract's own rule on a store entry, asked here so that a stand-in
  // cannot quietly hand the shell something no trust list would hold.
  if (!validStore(store)) throw new Error("fake-link: this store would not stand in a trust list");

  // A SECOND key, and the whole point of it: with the knob on, the catalog
  // the Apps tab fetches is signed by a key that is in no trust list. The UI
  // proof keeps the honest key either way, so a case about the catalog cannot
  // take the app frame down with it.
  const other = await crypto.subtle.generateKey({ name: "Ed25519" }, true, ["sign", "verify"]);
  const otherPub = new Uint8Array(await crypto.subtle.exportKey("raw", other.publicKey));
  const otherKid = await keyIDAsync(otherPub);

  const envelope = async (doc, key, id) => {
    const payload = enc.encode(JSON.stringify(doc));
    const sig = new Uint8Array(await crypto.subtle.sign(
      { name: "Ed25519" }, key.privateKey, signedMessage(PURPOSE_CATALOG, payload)));
    return { payload: b64encode(payload), kid: id, sig: b64encode(sig) };
  };

  // The proof names the app that was asked for. ONE catalog for both of them,
  // because a store has one: two documents at one sequence number would be a
  // fork of the catalog, and a stand-in that handed the shell one would measure
  // it against something no store could send.
  const proofFor = async (app, uiHashes) => enc.encode(JSON.stringify({
    app, version: VERSION, store: STORE,
    catalog: await envelope(catalogDoc(uiHashes, knobs.evilName === true), pair, kid),
  }));

  const catalogFor = async (uiHashes) => enc.encode(JSON.stringify({
    catalog: knobs.foreignCatalog === true
      ? await envelope(catalogDoc(uiHashes, knobs.evilName === true), other, otherKid)
      : await envelope(catalogDoc(uiHashes, knobs.evilName === true), pair, kid),
  }));

  return { store, pair, pub, kid, proofFor, catalogFor };
}

// A TEST HOOK, and it leaves with this file. It steers what the stand-in
// serves, so that the frame's refusals can be measured against a stand-in
// that is otherwise entirely in order.
//
//   swapUI - serve a DIFFERENT archive than the one the proof vouches for.
//   entry  - put the entry page in the archive under this name, so the
//            manifest's "index.html" is not in it.
//   module - ship the entry page with a module script.
//   terminalOutput - what the terminal's service answers, whatever was sent.
//   terminalCut    - the terminal's channel ends without an "ok", the way a
//            controller that fell over ends one.
//
// The proof is always signed over the archive the hooks produced, except for
// swapUI: otherwise a case meant for the entry rule would die at the checksum
// and prove nothing about the rule it is named after.
function hooks() {
  const t = window.__ngcTest ?? {};
  return {
    swap: t.swapUI === true,
    entry: typeof t.entry === "string" && t.entry !== "" ? t.entry : "index.html",
    module: t.module === true,
    output: typeof t.terminalOutput === "string" ? t.terminalOutput : "",
    cut: t.terminalCut === true,
  };
}

// A TEST HOOK, and it leaves with this file: what the store's cases want
// this world to look like. It is read once, when the world is built, so a
// case sets it with addInitScript before the page is loaded.
function knobsOf() {
  const k = window.__ngcStore;
  return k !== null && typeof k === "object" ? k : {};
}

// The trust list of this installation. IT IS BUILT HERE AND OPENED BY
// NOBODY - Task 20 opens the real one against the anchor and follows it
// version by version - but it is a list the contract accepts: it passes
// trust.validateList and trust.parseList, because the store re-encodes it
// when a standing approval is given and reads its own work back. A list that
// only half held up would have the store measured against a document no
// installation could ever carry.
function listOf(store, admin, level) {
  return {
    seq: 1,
    prev: new Uint8Array(0),
    issued: 1790000000,
    installation: b64encode(new Uint8Array(16).fill(9)),
    rp: { id: "ngc.example.org", origins: ["https://ngc.example.org"] },
    users: [admin],
    roles: [{ name: "admin", level, black: [], white: [], passkeyOnly: false, passwordToo: false }],
    nodes: [
      { id: "pi-1", kind: "controller", name: "Dienste 1", noise: new Uint8Array(32).fill(11),
        sign: new Uint8Array(0), url: "", tls: new Uint8Array(0), rootUsers: [admin.id], rootRoles: [] },
      { id: "pi-2", kind: "controller", name: "Dienste 2", noise: new Uint8Array(32).fill(12),
        sign: new Uint8Array(0), url: "", tls: new Uint8Array(0), rootUsers: [admin.id], rootRoles: [] },
    ],
    stores: [store],
    auto: [],
  };
}

// The rows the store shows where a case names none of its own. Byte fields
// stand here as base64url text, because that is what they are in the
// document, and a case that overrides a row writes it the same way.
const SOURCES = [
  { id: STORE, name: "NGC Store", url: "https://store.ngc.test", seq: 1, checked: 1790000000, error: "" },
];
const INSTALLED = [
  { id: TERMINAL, version: VERSION, store: STORE, node: "pi-1", active: true,
    state: "ok", license: "none", keptData: 0 },
  { id: "de.example.fotos", version: "2.1.0", store: "local", node: "pi-2", active: false,
    state: "unreachable", license: "none", keptData: 0 },
];
const NODES = [
  { node: "pi-1", online: true, roles: ["services"], version: "1.0.0", arch: "arm64" },
  { node: "pi-2", online: false, roles: ["services"], version: "1.0.0", arch: "arm64" },
  { node: "api-1", online: true, roles: ["api"], version: "1.0.0", arch: "amd64" },
];
const UPDATES = [
  { target: "ngc-web", node: "web-1", installed: "1.0.0", available: "1.1.0",
    sha256: b64encode(new Uint8Array(32).fill(6)), key: "", mode: "semi", window: "", state: "downloaded" },
  { target: TERMINAL, node: "pi-1", installed: "1.0.0", available: "1.1.0",
    sha256: b64encode(new Uint8Array(32).fill(7)), key: "", mode: "notify", window: "", state: "needsgrants" },
  { target: "system", node: "pi-1", installed: "", available: "", sha256: "", key: "",
    mode: "notify", window: "", state: "" },
];

// fakeWorld builds the stand-in: the link the desktop asks, the trust list
// the app frame checks a proof against and the session the store signs its
// tasks with. The shapes are the ones shell/channel.js and shell/trust.js
// will have - call() and open() hand back bytes, onNotice() takes a listener
// that is handed bytes, current() answers with the list in force - so neither
// the desktop nor the frame nor the store changes when the real ones land.
//
// It is a promise because the store key is a real key and making one is
// asynchronous. Task 20 opens the trust list before the desktop starts for
// the same reason: nothing may be shown before it is known what may be shown.
export async function fakeWorld() {
  const notices = new Set();
  const knobs = knobsOf();
  const { store, proofFor, catalogFor } = await world(knobs);

  window.__ngcTest = { swapUI: false, entry: "", module: false, terminalOutput: "", terminalCut: false };
  // A TEST HOOK as well: how many channels the shell has opened, and how many of
  // the terminal's it has closed again. The real session counts its own channels
  // and needs no such numbers.
  window.__ngcChannelsOpened = 0;
  window.__ngcStreamsClosed = 0;
  // A TEST HOOK as well: what the shell handed this link, in the order it
  // handed it over. Params, task and confirmation are JSON and stand here as
  // the text they are; a body is bytes and stands as base64url.
  window.__ngcSent = [];

  // The admin of this world, with a password key.
  //
  // TWO KINDS OF KEY, AND THE KNOB DECIDES. Without it the public half is a
  // random Ed25519 key of this page: enough for every case in which nobody
  // checks a signature of the user, and it costs nothing.
  //
  // WITH passwordKey it is the key PASSWORD really derives under the KDF
  // that stands in the list below - proof.deriveSeed and proof.publicFromSeed,
  // the same two the shell walks when it asks for the password again. Then a
  // proof the shell built over a new version of the trust list VERIFIES
  // against this list, and trust.advance takes the chain. That is what lets
  // the standing-approval case measure the chain with the contract itself
  // instead of with a promise of this file. It costs one Argon2id at start-up
  // (m=19456, t=1, the cheapest the contract allows), which is why it is a
  // knob and not the default.
  const kdf = { alg: "argon2id", salt: new Uint8Array(16).fill(5), m: 19456, t: 1, p: 1 };
  const userPub = knobs.passwordKey === true
    ? await publicFromSeed(await deriveSeed(PASSWORD, kdf))
    : new Uint8Array(await crypto.subtle.exportKey("raw",
      (await crypto.subtle.generateKey({ name: "Ed25519" }, true, ["sign", "verify"])).publicKey));
  const admin = {
    id: b64encode(new Uint8Array(16).fill(1)),
    name: "anna",
    roles: ["admin"],
    keys: [{
      id: await keyIDAsync(userPub), kind: "password", alg: "EdDSA", pub: userPub,
      credId: new Uint8Array(0), kdf,
    }],
  };
  const level = typeof knobs.level === "string" && knobs.level !== "" ? knobs.level : "off";

  // TWO VERSIONS, NOT ONE, AND THAT IS THE WHOLE POINT OF THIS BLOCK.
  //
  // `kept` is what the API stores; `inForce` is what THIS BROWSER has
  // verified. A real installation has exactly that gap: ngc.users/api/trust
  // takes a version and answers with its number, and the browser's own list
  // moves only when TrustStore.refresh follows the chain over
  // GET /api/v1/trust. Until this round both were one variable here, the
  // stand-in moved itself, and the store could throw the answer away without
  // anything going red - which is what Fixrunde 1 found: the second standing
  // approval would then be built on the version before the first one, and two
  // versions at one number are a FORK of the trust list.
  const first = listOf(store, admin, level);
  let inForce = {
    list: first,
    signed: {
      payload: encodeList(first),
      // Version 1 is signed by nobody here: a real one is opened against the
      // anchor, and this list was opened by nobody. What has to hold is the
      // SHAPE, because the store hashes this payload for the prev of the
      // version it writes.
      proof: { kind: "password", kid: admin.keys[0].id, sig: new Uint8Array(64).fill(8),
        authData: new Uint8Array(0), clientData: new Uint8Array(0) },
    },
  };
  inForce.hash = await listHash(inForce.signed);
  let kept = inForce;
  // A TEST HOOK, and it leaves with this file: version 1 of this world, as it
  // stood before anything was written. A case that wants to check a chain the
  // store built needs the version it started from, and asking fakeWorld()
  // again would build a DIFFERENT world (a new store key, a new version 1)
  // and clear the record of what was sent along the way.
  window.__ngcListFirst = inForce;

  // The release key of the initial setup: what a standing approval for an
  // NGC part is signed with. Task 19 reads it out of the setup offer; here
  // it is a key of this page, exposed so a case can name it.
  const releasePair = await crypto.subtle.generateKey({ name: "Ed25519" }, true, ["sign", "verify"]);
  const releaseKey = new Uint8Array(await crypto.subtle.exportKey("raw", releasePair.publicKey));

  const installKey = { installation: first.installation, key: b64encode(new Uint8Array(32).fill(13)) };
  window.__ngcStoreKey = b64encode(store.key);
  window.__ngcReleaseKey = b64encode(releaseKey);
  window.__ngcLiarKey = b64encode(new Uint8Array(32).fill(0xee));
  window.__ngcInstallKey = installKey;

  const or = (name, fallback) => (Array.isArray(knobs[name]) ? knobs[name] : fallback);

  // The files of the shipped apps, read once. They are read here and not inside
  // the answers below, so the archive the shell is handed and the archive the
  // proof was signed over are built from ONE set of bytes.
  const shipped = await readShipped();

  // The checksum of every shipped app's archive, as the hooks of the moment
  // produce it. It is worked out and not remembered: a case switches a hook on
  // after the world was built, and a proof over yesterday's archive would be a
  // refusal the case did not ask for.
  const uiHashes = () => {
    const h = hooks();
    const out = new Map();
    for (const [id, files] of shipped) out.set(id, sha256(uiTar(files, { entry: h.entry, module: h.module })));
    return out;
  };

  // Which app an answer is about. It stands in the params the shell sent, and
  // reading them with JSON.parse is the one shortcut this file may take: a real
  // API reads them with core.ParseParams, and these are params the shell
  // encoded two function calls ago.
  const appOf = (params) => {
    try {
      return JSON.parse(new TextDecoder().decode(params)).app;
    } catch {
      return "";
    }
  };

  const results = {
    __proto__: null,
    "ngc.desktop/apps": async () => enc.encode(JSON.stringify(APPS)),
    "ngc.desktop/ui-proof": async (_body, params) => await proofFor(appOf(params), uiHashes()),

    "ngc.users/nodes": async () => enc.encode(JSON.stringify(or("nodes", NODES))),
    // A new version of the trust list is READ with the contract and KEPT -
    // and it does not become the version in force here. That is the API's
    // side of the split: it stores the version and answers with its number,
    // and nothing else. Whether this browser follows is the browser's own
    // business, and it is exactly what Fixrunde 1 found missing.
    "ngc.users/trust": async (body) => {
      const s = parseSigned(body);
      const next = await parseList(s.payload);
      kept = { list: next, signed: s, hash: await listHash(s) };
      return enc.encode(JSON.stringify({ seq: next.seq }));
    },
    "ngc.store/sources": async () => enc.encode(JSON.stringify(or("sources", SOURCES))),
    "ngc.store/catalog": async () => await catalogFor(uiHashes()),
    "ngc.store/installed": async () => enc.encode(JSON.stringify(or("installed", INSTALLED))),
    "ngc.store/updates": async () => enc.encode(JSON.stringify(or("updates", UPDATES))),
    "ngc.store/install-key": async () => enc.encode(JSON.stringify(installKey)),
    "ngc.store/license": async () => enc.encode(JSON.stringify({ app: TERMINAL, state: "valid" })),
    "ngc.store/set-update": async () => enc.encode("{}"),
    "ngc.store/activate": async () => enc.encode("{}"),
    "ngc.store/deactivate": async () => enc.encode("{}"),
    "ngc.store/uninstall": async () => enc.encode("{}"),
    "ngc.store/delete-data": async () => enc.encode("{}"),
    "ngc.store/install": async () => enc.encode("{}"),
    // The upload is the one answer this stand-in WORKS OUT instead of making
    // up: it reads the package the shell streamed with the same
    // pack.packageUI the shell used. So an answer that agrees is an answer
    // that really agrees, and the case about a mismatch has to bend it by
    // hand (uploadTwist) instead of getting one for free.
    "ngc.store/upload": async (body) => {
      const u = await packageUI(new Blob([body]).stream());
      const r = {
        id: u.manifest.id, version: u.manifest.version,
        sha256: b64encode(u.packageSha256), ui: b64encode(u.sha256),
      };
      const twist = knobs.uploadTwist;
      return enc.encode(JSON.stringify(twist !== null && typeof twist === "object" ? { ...r, ...twist } : r));
    },
  };

  // collect reads what the shell wants to send. The real link streams it in
  // pieces against a credit; this one takes the whole of it, because what a
  // case has to see is WHAT was sent and not in how many frames.
  const collect = async (stream) => {
    if (!stream || typeof stream.getReader !== "function") return new Uint8Array(0);
    const reader = stream.getReader();
    const parts = [];
    let n = 0;
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      parts.push(value);
      n += value.length;
    }
    const out = new Uint8Array(n);
    let o = 0;
    for (const p of parts) {
      out.set(p, o);
      o += p.length;
    }
    return out;
  };

  const link = {
    async call(app, service, name, params, opts) {
      window.__ngcChannelsOpened++;
      const o = opts ?? {};
      const body = await collect(o.stream);
      o.onProgress?.(body.length);
      window.__ngcSent.push({
        app, service, name,
        params: params ? new TextDecoder().decode(params) : "",
        task: o.taskBytes ? new TextDecoder().decode(o.taskBytes) : "",
        confirm: o.confirmBytes ? new TextDecoder().decode(o.confirmBytes) : "",
        body: b64encode(body),
      });
      const make = results[`${app}/${name}`];
      if (make === undefined) throw new Error(`fake-link: nothing for ${app}/${service}/${name}`);
      return await make(body, params);
    },
    // A stand-in for a channel that stays open. For a user interface it hands
    // the whole archive over in one piece and closes; the real one arrives in
    // frames. For the terminal it stays open and answers every line, which is
    // what a stream is for.
    async open(app, service, name, params) {
      window.__ngcChannelsOpened++;
      const h = hooks();
      if (`${app}/${name}` === "ngc.desktop/ui") {
        const files = shipped.get(appOf(params));
        if (files === undefined) throw new Error(`fake-link: no ui for ${appOf(params)}`);
        const bytes = h.swap
          ? uiTar(files, { entry: "andere.html" })
          : uiTar(files, { entry: h.entry, module: h.module });
        const s = { onData: null, onClose: null, close() {} };
        // After the caller has hung its two listeners up, never before.
        setTimeout(() => {
          s.onData?.(bytes);
          s.onClose?.("ok", "");
        }, 0);
        return s;
      }
      if (`${app}/${name}` === `${TERMINAL}/shell`) return terminalStream(h);
      throw new Error(`fake-link: no stream for ${app}/${service}/${name}`);
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

  // The stand-in for shell/trustlist.js (Task 20): the verified trust list
  // the frame checks a UI proof against and the store writes the next version
  // of.
  //
  // IT WAS OPENED BY NOBODY. A real list is opened against the anchor and
  // followed version by version; this one is built in the page. What it does
  // do is hold up to the contract - validateList and parseList take it - and
  // answer the two calls the real TrustStore answers, with the same rules.
  const trust = {
    // current() has the three members the real one has: the list, the exact
    // bytes it was read from, and the hash of those bytes. The store builds
    // the next version's prev out of that hash and not out of a hash of its
    // own - it is the one the chain was checked with.
    current: () => inForce,
    // refresh(hintSeq) follows TrustStore.refresh, rule for rule, and the
    // rules are measured against the real class in store.spec.js ("what
    // refresh does with the number the answer carries"):
    //   - a hint that is no whole number, or below 1, counts as 0;
    //   - a hint that is not HIGHER than the version in force changes
    //     nothing and answers the number in force, without asking anybody;
    //   - a higher one takes what the API kept and answers the new number.
    // What the real one does on top is the network and the chain check, and
    // neither has a place in a stand-in.
    refresh: async (hintSeq) => {
      const want = Number.isSafeInteger(hintSeq) && hintSeq >= 1 ? hintSeq : 0;
      if (want > inForce.list.seq && kept.list.seq >= want) inForce = kept;
      return inForce.list.seq;
    },
  };

  // The stand-in for the session (Tasks 17 and 19): who is signed in, and
  // the key their tasks are signed with. The key is a real Ed25519 key of
  // this page, so what the store signs is a real signature over the real
  // bytes; whose key it is, is what the login will decide.
  const sessionKey = await crypto.subtle.generateKey({ name: "Ed25519" }, true, ["sign", "verify"]);
  const session = {
    inst: first.installation,
    user: admin.id,
    // The task is bound to the client certificate by its SHA-256, and this
    // is a hash-shaped stand-in for one: 32 bytes, which is all task.parts
    // can ask of it here.
    certHash: new Uint8Array(32).fill(14),
    // The kid is the SESSION key's own id and not the admin's: a verifier
    // takes the signing key out of the client certificate, so a kid from
    // somewhere else would be a name for a key nobody signed with.
    signKey: {
      privateKey: sessionKey.privateKey,
      kid: await keyIDAsync(new Uint8Array(await crypto.subtle.exportKey("raw", sessionKey.publicKey))),
    },
  };

  return { link, trust, session, releaseKey };
}

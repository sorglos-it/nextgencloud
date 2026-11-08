// The trust list of this installation, in the shell: which keys this
// workplace believes. It is the ROOT of everything else - the login, the
// session, the apps, a paid app's licence all hang from it - so it is never
// taken from an answer of the API. The API may hand the versions on; whether
// they count is decided here, against an anchor that came from the page's
// own server (Go: trust.Open, trust.Advance, session.ParseTrustChain).
//
// WHAT THE ANCHOR BINDS, and it is worth knowing before reading on:
// trust.open checks NO SIGNATURE above version 1. The anchor is a hash of the
// exact bytes of one version, and that hash is the whole binding for it; from
// there on every later version is held against the keys of the version
// before it. The Go side does it the same way and says so. So an anchor that
// is wrong by one byte is a trust list that does not open - which is exactly
// the alarm this file exists for.
//
// THE ALARM IS THE POINT OF THIS FILE. A chain that does not fit its anchor
// is not a fault to click away: it can mean that somebody stands between
// this browser and the installation. So the shell stops, ends the session
// itself and says so in words a person who has never heard of a signature can
// act on. There is no button on that message, and there is no code path that
// carries on past it.
//
// WHAT IS NOT KEPT HERE: nothing about passkeys. kid and credId come out of
// the login challenge (session.PasskeyRef), so a browser that has never seen
// this installation logs in with a passkey on the first try and needs no list
// for it.
import { b64encode, bytesOf, equal, utf8 } from "../contracts/b64.js";
import * as trust from "../contracts/trust.js";
import * as http from "../net/http.js";
import { el } from "./dom.js";

// The one sentence the person gets when the chain does not hold. It is the
// same sentence for every way it can break: which of them it was is nothing
// anybody at this browser can act on, and naming it would tell whoever bent
// the chain how close they got.
export const ALARM = "Die Vertrauensliste dieser Anlage stimmt nicht. "
  + "NGC hat Sie sicherheitshalber abgemeldet.";

// No anchor from the server and none remembered from the initial setup: this
// installation was never finished, and the person is told what is missing
// rather than left with a page that does nothing.
export const NOT_SET_UP = "Diese Anlage ist noch nicht fertig eingerichtet. "
  + "Bitte den Controller des Webservers koppeln.";

// A node the verified list does not carry gets no end-to-end channel. The
// sentence says what is the matter and blames nobody.
export const NOT_TRUSTED = "Dieser Server steht nicht in der Vertrauensliste.";

// sha256.Size, and the length of a Noise public key. Two names for one number
// would be one name too few: HASH_SIZE is what an anchor is, KEY_SIZE what a
// controller's key is.
const HASH_SIZE = 32, KEY_SIZE = 32;

// How many pages of the chain one call will fetch. Every page must carry the
// version after the last one taken, so a chain cannot stand still - but a
// bound belongs here all the same, and 64 pages of up to
// session.MAX_CHAIN_VERSIONS versions are more versions than an installation
// will ever have.
const MAX_PAGES = 64;

const DB_NAME = "ngc", DB_VERSION = 1;
const STORE_TRUST = "trust", STORE_SETUP = "setup";
const SETUP_ANCHOR = "anchor";

// TrustBroken is what a caller gets when the chain did not hold. It carries
// no detail on purpose: the caller has nothing to decide any more, the alarm
// has already gone out and the session is already over.
export class TrustBroken extends Error {
  constructor() {
    super("trustlist: the chain does not hold");
    this.name = "TrustBroken";
  }
}

// NotTrusted is what controllerKey answers for a node the list does not
// carry. Its message is the one the person sees, because that is the only
// thing there is to say about it.
export class NotTrusted extends Error {
  constructor() {
    super(NOT_TRUSTED);
    this.name = "NotTrusted";
  }
}

// ------------------------------------------------------------- the storage
//
// Everything below answers "no" rather than throwing where the browser keeps
// nothing: a private window, a browser told to keep no site data, a quota
// that is full. None of that is a reason to leave a person without a
// workplace - it only means the chain is walked from the anchor again on the
// next start, which is the safer of the two ways round anyway.

function openDB() {
  return new Promise((ok, no) => {
    let req;
    try {
      req = indexedDB.open(DB_NAME, DB_VERSION);
    } catch {
      no(new Error("trustlist: no storage"));
      return;
    }
    req.onupgradeneeded = () => {
      const db = req.result;
      if (!db.objectStoreNames.contains(STORE_TRUST)) db.createObjectStore(STORE_TRUST);
      if (!db.objectStoreNames.contains(STORE_SETUP)) db.createObjectStore(STORE_SETUP);
    };
    req.onsuccess = () => ok(req.result);
    req.onerror = () => no(new Error("trustlist: no storage"));
    req.onblocked = () => no(new Error("trustlist: no storage"));
  });
}

function readAt(store, key) {
  return openDB().then((db) => new Promise((ok, no) => {
    let req;
    try {
      req = db.transaction(store, "readonly").objectStore(store).get(key);
    } catch {
      db.close();
      no(new Error("trustlist: no storage"));
      return;
    }
    req.onsuccess = () => { db.close(); ok(req.result ?? null); };
    req.onerror = () => { db.close(); no(new Error("trustlist: no storage")); };
  }));
}

function writeAt(store, key, value) {
  return openDB().then((db) => new Promise((ok, no) => {
    let tx;
    try {
      tx = db.transaction(store, "readwrite");
      tx.objectStore(store).put(value, key);
    } catch {
      db.close();
      no(new Error("trustlist: no storage"));
      return;
    }
    tx.oncomplete = () => { db.close(); ok(); };
    tx.onerror = () => { db.close(); no(new Error("trustlist: no storage")); };
    tx.onabort = () => { db.close(); no(new Error("trustlist: no storage")); };
  }));
}

// ---------------------------------------------------------------- the anchor

// anchorFor answers with the anchor this shell hangs its trust list from: the
// one the page's own server wrote into /config.json, or - while the web
// controller is not paired and the file therefore has none - the one the
// initial setup left in this browser. null means there is neither, and the
// shell shows NOT_SET_UP.
//
// The server's own anchor wins where there is one: once the web controller is
// paired, the file on the server is the newer truth, and a browser that kept
// preferring what it remembered would pin an installation to the day it was
// set up.
export async function anchorFor(config) {
  const fromServer = held(config === null || config === undefined ? null : config.anchor);
  if (fromServer !== null) return fromServer;
  let kept;
  try {
    kept = await readAt(STORE_SETUP, SETUP_ANCHOR);
  } catch {
    return null;
  }
  return held(kept ?? null);
}

// rememberAnchor keeps the anchor of version 1 that the initial setup built,
// so the shell can open the list before the web controller is paired. It is
// written by the initial setup (Task 19) and read by anchorFor.
export async function rememberAnchor(anchor) {
  const a = held(anchor);
  if (a === null) throw new Error("trustlist: that is no anchor");
  try {
    await writeAt(STORE_SETUP, SETUP_ANCHOR, { seq: a.seq, hash: a.hash });
  } catch {
    // A browser that keeps nothing simply asks the server again next time.
  }
}

// held takes an anchor apart into one of our own. The hash is copied with
// bytesOf: from here on it is compared against a digest this shell computed,
// and a caller that could still reach those bytes could let the comparison
// and the fetch see two different anchors.
function held(a) {
  if (a === null || typeof a !== "object") return null;
  const { seq, hash } = a;
  if (!Number.isSafeInteger(seq) || seq < 1) return null;
  if (!(hash instanceof Uint8Array) || hash.length !== HASH_SIZE) return null;
  let copy;
  try {
    copy = bytesOf(hash, "trustlist: that is no anchor");
  } catch {
    return null;
  }
  return copy.length === HASH_SIZE ? { seq, hash: copy } : null;
}

// ------------------------------------------------------------ one version

// own makes one version of the chain a version of OURS: fresh bytes on fresh
// buffers, which nobody outside this module holds a reference to.
//
// THIS IS THE RULE THE WHOLE FILE TURNS ON - the chain that is checked has to
// be the chain that counts afterwards. trust.open and trust.advance copy what
// they are handed (trust.held) and check THAT copy, but they hand the copy
// back to nobody: they answer with the list and the hash alone. A store that
// then kept the caller's own object would keep an object whose bytes were
// never the ones that were checked. So the copy is made here, the copy is
// what is verified, the copy is what is kept - and #settle below holds the
// two together by their hash, measured rather than argued.
function own(v) {
  if (v === null || typeof v !== "object") throw new TrustBroken();
  const p = v.proof;
  if (p === null || typeof p !== "object") throw new TrustBroken();
  try {
    return {
      payload: bytesOf(v.payload, "trustlist: no version"),
      proof: {
        kind: p.kind, kid: p.kid,
        sig: bytesOf(p.sig, "trustlist: no version"),
        authData: bytesOf(p.authData, "trustlist: no version"),
        clientData: bytesOf(p.clientData, "trustlist: no version"),
      },
    };
  } catch {
    throw new TrustBroken();
  }
}

// signedBytes writes one version as the document it is, so it can be kept and
// read back with the contract's own parser instead of a shape check written
// here a second time. apps/store.js writes the same document for the API;
// this one never leaves the browser.
function signedBytes(v) {
  const p = { kind: v.proof.kind, kid: v.proof.kid, sig: b64encode(v.proof.sig) };
  if (v.proof.authData.length > 0) p.authData = b64encode(v.proof.authData);
  if (v.proof.clientData.length > 0) p.clientData = b64encode(v.proof.clientData);
  const b = utf8(JSON.stringify({ payload: b64encode(v.payload), proof: p }));
  // Read back what was written: a version this shell could not read itself
  // must not be put away as one it can.
  trust.parseSigned(b);
  return b;
}

// --------------------------------------------------------------- the store

export class TrustStore {
  // The version in force: the list, the exact bytes it was read from, and the
  // hash those bytes have. Nothing outside gets to set it.
  #cur = null;
  // The anchor the version in force was verified against. A version that was
  // followed from another anchor says nothing about this one.
  #anchor = null;
  #alarmed = false;
  #changed = new Set();
  #alarms = new Set();
  // ONE CHAIN OF WORK, and the second reason this file is shaped the way it
  // is. Two trust hints can arrive in one turn of the event loop. Whoever
  // reads the number of the version in force BEFORE an await and works with
  // it afterwards asks for the same versions twice - and a version the shell
  // already has is, from the inside, indistinguishable from a chain that
  // broke. That would raise the alarm and log a person out over nothing. So
  // every call below runs after the one before it has finished, and the
  // number is read INSIDE that turn, never before it.
  #work = Promise.resolve();

  #serial(fn) {
    const run = this.#work.then(() => undefined, () => undefined).then(fn);
    this.#work = run.then(() => undefined, () => undefined);
    return run;
  }

  // current answers with the version in force, or null where there is none -
  // before the first boot, and after the alarm. The app frame reads .list out
  // of it and refuses to run anything without one; the store reads .signed to
  // build the version after it.
  current() {
    return this.#cur;
  }

  onChange(fn) {
    if (typeof fn === "function") this.#changed.add(fn);
  }

  onAlarm(fn) {
    if (typeof fn === "function") this.#alarms.add(fn);
  }

  // controllerKey answers with a controller's Noise key OUT OF THE VERIFIED
  // LIST and out of nothing else. A node the list does not carry gets no key,
  // and therefore no end-to-end channel: an answer of the API that named the
  // key itself would let the API choose whom the browser encrypts to, which
  // is the whole thing end-to-end is for.
  controllerKey(nodeID) {
    const list = this.#cur === null ? null : this.#cur.list;
    const n = list === null ? null : trust.node(list, nodeID);
    if (n === null || n.kind !== trust.NODE_CONTROLLER
      || !(n.noise instanceof Uint8Array) || n.noise.length !== KEY_SIZE) {
      throw new NotTrusted();
    }
    return n.noise;
  }

  // storeOf answers with an app store out of the verified list, or null. A
  // store that is not in it has no key here, and a catalog it signed is
  // checked against nothing.
  storeOf(id) {
    const list = this.#cur === null ? null : this.#cur.list;
    if (list === null || !Array.isArray(list.stores)) return null;
    return list.stores.find((s) => s.id === id) ?? null;
  }

  // load takes the newest version this browser has already verified for this
  // installation. It answers null where there is none, where the browser
  // keeps nothing, or where what it kept does not hold up to its own
  // contract - and then the chain is simply walked from the anchor again,
  // which is the safer way round.
  //
  // WHAT A KEPT VERSION RESTS ON, said plainly: above version 1 nothing in a
  // chain carries a proof back to the anchor, so a version at 5 is believed
  // because THIS shell followed it there and wrote it down. What is written
  // down with it is the anchor it was followed from, and bootstrap carries on
  // from it only for that same anchor. A browser whose storage somebody else
  // can write is a browser where that somebody already runs in this origin;
  // this does not pretend to help there.
  load(installation) {
    return this.#serial(() => this.#load(installation));
  }

  async #load(installation) {
    // A version that is already in force is never replaced by a kept one.
    // What is in force was followed from the anchor in THIS session; what is
    // kept was followed in an earlier one, and can only be the same or older.
    // Taking it would be a step backwards, and a step backwards is the one
    // move a trust list may never make.
    if (this.#cur !== null) return { list: this.#cur.list, hash: this.#cur.hash };
    if (typeof installation !== "string" || installation === "") return null;
    let rec;
    try {
      rec = await readAt(STORE_TRUST, installation);
    } catch {
      return null;
    }
    if (rec === null || typeof rec !== "object") return null;
    if (!(rec.signed instanceof Uint8Array) || !(rec.hash instanceof Uint8Array)) return null;
    if (!Number.isSafeInteger(rec.seq) || rec.seq < 1) return null;
    const anchor = held({ seq: rec.anchorSeq, hash: rec.anchorHash });
    if (anchor === null) return null;
    let signed, list, hash;
    try {
      // The kept bytes go through the same parsers an answer of the API goes
      // through. They are this browser's own writing, and they are read as if
      // they were not: a record that does not hold up is a record that is not
      // used.
      signed = trust.parseSigned(bytesOf(rec.signed, "trustlist: no record"));
      list = await trust.parseList(signed.payload);
      hash = await trust.hash(signed);
    } catch {
      return null;
    }
    if (!equal(hash, rec.hash) || list.seq !== rec.seq || list.installation !== installation) {
      return null;
    }
    this.#cur = { list, signed, hash };
    this.#anchor = anchor;
    return { list, hash };
  }

  // bootstrap opens the chain against the anchor and follows it to the newest
  // version, and answers with that version's number. Where load put a version
  // in force that was verified against THIS anchor, it carries on from there
  // and fetches only what is missing.
  bootstrap(anchor) {
    return this.#serial(() => this.#bootstrap(anchor));
  }

  async #bootstrap(anchor) {
    if (this.#alarmed) throw new TrustBroken();
    const a = held(anchor);
    if (a === null) throw new Error("trustlist: that is no anchor");
    const kept = this.#cur !== null && this.#anchor !== null
      && this.#anchor.seq === a.seq && equal(this.#anchor.hash, a.hash);
    if (kept) return this.#follow();
    // Another anchor, or none in force: nothing that was verified before
    // counts for this one.
    this.#cur = null;
    this.#anchor = a;
    const answer = await http.trustChain(a.seq);
    // The installation has no version at the anchor at all. That is not a
    // broken chain, it is an API that cannot tell us what we already pinned -
    // and there is nothing to work with either way.
    if (answer === null) throw new Error("trustlist: no version at the anchor");
    let versions;
    try {
      versions = answer.versions.map(own);
      const next = await trust.open({ seq: a.seq, hash: a.hash }, versions);
      await this.#settle(next, versions[versions.length - 1]);
    } catch {
      return this.#raise();
    }
    if (answer.more) return this.#follow(true);
    await this.#save();
    this.#tell();
    return this.#cur.list.seq;
  }

  // refresh answers a trust hint. The hint is ONLY a hint: a number that is
  // not higher than the one in force says nothing new, and asking for it
  // would be one round trip for every message the other side cares to send.
  refresh(hintSeq) {
    return this.#serial(() => this.#refresh(hintSeq));
  }

  async #refresh(hintSeq) {
    if (this.#alarmed) throw new TrustBroken();
    if (this.#cur === null) throw new Error("trustlist: no list in force");
    const want = Number.isSafeInteger(hintSeq) && hintSeq >= 1 ? hintSeq : 0;
    if (want <= this.#cur.list.seq) return this.#cur.list.seq;
    return this.#follow();
  }

  // follow fetches page by page while "more" is set, and checks every page
  // against the version in force before a byte of it is read as a list.
  //
  // from is read out of the version in force at the top of EVERY turn, after
  // the page before it has been taken - never once at the start. That is the
  // whole of the fix described at #work: a cursor that is read before an
  // await and worked with afterwards is a cursor that has moved on.
  async #follow(changed = false) {
    let moved = changed;
    try {
      for (let page = 0; page < MAX_PAGES; page++) {
        const from = this.#cur.list.seq + 1;
        // Everything that goes wrong on the way - no connection, an answer
        // that is no chain, a status the contract does not give - comes back
        // as an ApiError and travels out of here untouched. It is NOT an
        // alarm: a dropped connection must never log a person out, and the
        // list that stays in force is one that was verified. What that costs
        // is named in the task report: somebody who can block this path keeps
        // the shell on an older list, so a key taken away in a newer one is
        // taken away late.
        const answer = await http.trustChain(from);
        if (answer === null) break;
        try {
          const versions = answer.versions.map(own);
          const next = await trust.advance({ list: this.#cur.list, hash: this.#cur.hash }, versions);
          await this.#settle(next, versions[versions.length - 1]);
        } catch {
          return this.#raise();
        }
        moved = true;
        if (!answer.more) break;
      }
    } finally {
      // A page that never arrived leaves the pages before it in force, and
      // they are worth keeping: the next start then has less to fetch. After
      // the alarm there is nothing in force and nothing to keep.
      if (moved && this.#cur !== null) {
        await this.#save();
        this.#tell();
      }
    }
    return this.#cur.list.seq;
  }

  // settle puts one verified version in force. The hash trust.open or
  // trust.advance computed over the bytes IT read is held against the hash of
  // the version this keeps: so "the chain that was checked is the chain that
  // counts" is a measurement here and not a claim about somebody else's code.
  async #settle(next, kept) {
    if (kept === undefined) throw new TrustBroken();
    const mine = await trust.hash(kept);
    if (!equal(mine, next.hash)) throw new TrustBroken();
    this.#cur = { list: next.list, signed: kept, hash: next.hash };
  }

  async #save() {
    const cur = this.#cur, anchor = this.#anchor;
    if (cur === null || anchor === null) return;
    try {
      await writeAt(STORE_TRUST, cur.list.installation, {
        signed: signedBytes(cur.signed), hash: cur.hash, seq: cur.list.seq,
        anchorSeq: anchor.seq, anchorHash: anchor.hash,
      });
    } catch {
      // Nothing kept, nothing lost: the next start walks the chain from the
      // anchor again.
    }
  }

  // tell says that another version is in force. Once per settled state and
  // not once per version: what the listeners do - throw the proven user
  // interfaces away, ask the API for the apps again - is worth doing for the
  // version that now counts, not three times on the way to it.
  #tell() {
    const now = this.#cur;
    if (now === null) return;
    for (const fn of this.#changed) {
      try {
        fn(now);
      } catch {
        // A listener that threw is the shell's own fault and no reason to
        // leave the rest of them untold.
      }
    }
  }

  // raise is the alarm. It runs once, it takes the list out of force, and it
  // ends the session ITSELF instead of asking a listener to: whoever knows
  // that the chain broke is the one who has to act on it, and a listener that
  // nobody hung up would leave a session running against a list nothing can
  // vouch for.
  //
  // The kept version is deliberately NOT deleted. It is the newest version
  // this shell ever verified, and it is the only thing that lets the next
  // start notice a chain that has been rolled back to an older number.
  #raise() {
    if (!this.#alarmed) {
      this.#alarmed = true;
      this.#cur = null;
      http.logout().catch(() => {
        // The session is over here either way; whether the API heard it is
        // its own business.
      });
      for (const fn of this.#alarms) {
        try {
          fn(ALARM);
        } catch {
          // One listener that throws must not keep the next from being told.
        }
      }
    }
    throw new TrustBroken();
  }
}

// --------------------------------------------------------------- the alarm

// showTrustAlarm puts the alarm on the screen. It hangs on the body and not
// in #root, so it survives whatever the shell draws underneath it - the login
// the person is sent back to, for one.
//
// THERE IS NO BUTTON ON IT, and that is the design and not an omission. The
// person cannot judge whether this is a mistake, and every "OK" would be
// pressed within the second. Keys do not reach past it either, so nothing
// behind it can be opened, typed into or closed by accident. The way out is
// to close the window, which the message says.
export function showTrustAlarm() {
  if (document.getElementById("trustalarm") !== null) return;
  const back = el("div", "alarmback");
  back.id = "trustalarm";
  const box = el("div", "alarmbox");
  box.setAttribute("role", "alertdialog");
  box.setAttribute("aria-modal", "true");
  // Nothing in here can be tabbed to, so the box itself takes the focus:
  // otherwise a screen reader would read on behind the alarm.
  box.tabIndex = -1;

  const head = el("h2", "alarmhead", "Etwas stimmt hier nicht");
  head.id = "trustalarmhead";
  box.setAttribute("aria-labelledby", head.id);
  const main = el("p", "alarmmain", ALARM);
  main.id = "trustalarmtext";
  box.setAttribute("aria-describedby", main.id);

  box.append(head, main,
    el("p", null, "Vielleicht hat sich jemand zwischen dieses Gerät und Ihre Anlage geschaltet."),
    el("p", null, "Bitte geben Sie hier kein Passwort mehr ein. Schließen Sie dieses Fenster und "
      + "fragen Sie die Person, die NGC für Sie eingerichtet hat."),
    el("p", "alarmfoot", "Diese Meldung lässt sich mit Absicht nicht wegklicken."));
  back.append(box);
  document.body.append(back);
  box.focus();

  // The keyboard stops here. Escape closes the start menu and a window, Tab
  // walks into whatever is behind the alarm - both of which would be working
  // on past it.
  document.addEventListener("keydown", (e) => {
    e.preventDefault();
    e.stopPropagation();
  }, true);
}

// startTrust is the whole handover in one call: the list is opened against
// the anchor before anything is shown, it is kept up to date from the
// session's own trust hints, and a chain that breaks raises the alarm.
//
// It is called after the login has the cookie and before the desktop starts -
// nothing may be shown before it is known what may be shown.
export async function startTrust({ anchor, installation, link, onAlarm } = {}) {
  const store = new TrustStore();
  // Hung up BEFORE the first version is fetched: a chain that does not fit
  // its anchor is exactly the case that has to reach the screen.
  store.onAlarm(typeof onAlarm === "function" ? onAlarm : () => showTrustAlarm());
  await store.load(installation);
  await store.bootstrap(anchor);
  if (link !== null && link !== undefined && typeof link.onTrustHint === "function") {
    link.onTrustHint((hint) => {
      store.refresh(hint === null || typeof hint !== "object" ? 0 : hint.seq)
        .catch(() => {
          // A broken chain has already raised the alarm; anything else was a
          // connection, and the next hint asks again.
        });
    });
  }
  return store;
}

// openerFor builds the openTrust auth/login.js needs: a function that opens
// the chain against this anchor and answers with the NEWEST version, not the
// oldest one.
//
// WHY IT LIVES HERE and not in auth/login.js: the anchor is the page's own
// server's word (anchorFor above), and everything that holds a chain against
// an anchor is in this file. login.js is handed the opened list and checks a
// certificate against it; it has no business knowing what an anchor is.
//
// ONE STORE PER MASK, built once and closed over, so the second walk below is
// a step forward from the first and not a fresh start: bootstrap carries on
// from the version in force where the anchor is the same one (see #bootstrap),
// and a trust list may never take a step backwards.
//
// { again: true } is the one retry login.js makes when verifyAPI found no
// issuer for the certificate -- a node that joined the list after the first
// walk. It is the SAME bootstrap, so the versions it pulls forward are held
// against the anchor exactly like the first ones. Nothing here decides to
// believe anything the chain did not prove.
//
// A chain that does not hold raises the alarm inside the store and throws
// TrustBroken. login.js lets that through: it is an ordinary Error, so the
// mask draws its one sentence, and the alarm is already on the screen above
// it with the words that say what happened.
export function openerFor({ anchor, installation, onAlarm } = {}) {
  const store = new TrustStore();
  store.onAlarm(typeof onAlarm === "function" ? onAlarm : () => showTrustAlarm());
  let loaded = false;
  // The number of the version the LAST call handed out. It is a local of this
  // opener and of nothing wider: the bound on the retry belongs to one mask,
  // and a field that outlived the page could be added up across attempts.
  let lastSeq = 0;
  return async function openTrust({ again = false } = {}) {
    if (!loaded) {
      loaded = true;
      await store.load(installation);
    }
    // bootstrap on the second call is not a repeat: where the version in
    // force came from this same anchor it follows the chain on from there and
    // fetches only what is missing, which is exactly "pull the list forward
    // once".
    await store.bootstrap(anchor);
    const cur = store.current();
    // No version in force after a bootstrap that did not throw cannot happen;
    // it is said all the same, because the alternative is handing login.js an
    // undefined to read .list out of.
    if (cur === null) throw new Error("trustlist: no version in force");
    if (again && cur.list.seq === lastSeq) {
      // The walk found nothing newer, so the issuer is in no version at all.
      // Saying so here keeps login.js from reporting a second time what is
      // really the first failure.
      throw new Error("trustlist: nothing newer than the version that failed");
    }
    lastSeq = cur.list.seq;
    return { list: cur.list, signed: cur.signed };
  };
}

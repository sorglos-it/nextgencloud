// The app window: the fenced frame every app's user interface runs in.
//
// The order below IS the security of this file, so it stands here as a list
// and the code follows it line for line:
//
//   1. Fetch the signed UI proof and check it against the trust list. What
//      comes out is the checksum of the app's ui-tar.
//   2. Fetch the ui-tar, bounded.
//   3. Hash what arrived and compare. Not equal: refuse, and READ NOTHING.
//   4. Only then walk the archive.
//   5. The entry page has to be a file of that archive.
//   6. Build the document out of those files and out of nothing else.
//   7. Put it in a frame that has an origin of its own, no storage and no
//      connection, and talk to it through one message channel.
//
// WHAT IS CHECKED IS WHAT IS RUN. This is the mistake that cost this strand
// ten rounds, and here is where it would hurt most: a shell that hashes the
// answer once and reads it a second time would run bytes nobody vouched for.
// So the archive is collected into ONE array that no one else holds a
// reference to, and the hash and the walk happen one after the other with no
// await, no callback and no caller object in between - see uiTar below.
import { el, clear } from "./dom.js";
import * as core from "../contracts/core.js";
import * as uitar from "../contracts/uitar.js";
import { equal, fromUtf8, bytesOf } from "../contracts/b64.js";
import { validName, validPath } from "../contracts/text.js";
import { CODES, MAX_PARAMS, validParams, message as codeMessage } from "../contracts/wire.js";
import { sha256 } from "../../vendor/noble-hashes/sha2.js";
import { currentTheme, onThemeChange } from "./theme.js";

// Where an app window comes from. It is a document of this server and not a
// srcdoc frame, because only a document fetched over http gets a policy of
// its own: see classes/Page.php, FRAME_CSP, for the measurement.
const FRAME_PAGE = "/frame.html";

// Where the library an app talks to the shell with lies. The shell reads it
// and writes it into the app's document; it is never fetched by the app.
const NGC_JS = "/assets/ngc.js";

// The most a ui-tar may weigh on the wire: every file it may hold, plus a
// header block for every entry (Go: store.MaxUITotal, store.MaxEntries).
const MAX_UITAR = uitar.MAX_UI_TOTAL + uitar.MAX_ENTRIES * uitar.BLOCK;

// How many verified user interfaces are kept, and how much they may weigh
// together. An app's UI is read once and then opened again and again, and
// fetching 84 MiB for every window is what this saves. The bound is what
// keeps it from being a leak: a desktop with a dozen apps open would
// otherwise hold every one of them for as long as the page lives.
const CACHE_APPS = 8;
const CACHE_BYTES = uitar.MAX_UI_TOTAL;

// How many channels one app window may hold open at the same time. The session
// has a bound of its own for the whole link (wire.MAX_SESSION_CHANNELS, 64),
// and this is the share a single window may take out of it: one frame that
// opened them all would leave none for the desktop, the store or the next
// window. An app that really needs more than a handful at once - a terminal
// needs one - is not an app this bound is in the way of.
const MAX_FRAME_STREAMS = 8;

// The status word of a CLOSE that ended well. It is the one the channel sends
// when the shell gives a channel up on purpose: the person closed the window,
// or the app asked for it, and neither is a fault of the service.
const CLOSE_OK = "ok";

// What the person is told. Four sentences, because four things can go wrong
// that they can tell apart; which rule inside a proof broke is not one of
// them, and naming it would only describe the inside of the server.
const NOT_GENUINE = "Die Oberfläche dieser App ist nicht echt. NGC führt sie nicht aus.";
const NO_ENTRY = "Die Startseite dieser App fehlt. NGC führt sie nicht aus.";
const NEEDS_CLASSIC = "Diese App braucht klassische Skripte.";
const NOT_LOADED = "Die Oberfläche dieser App konnte nicht geladen werden.";

// What to do about it. One line each, because a message with no way out is
// a dead end for somebody who did not choose any of this.
const HINTS = new Map([
  [NOT_GENUINE, "Bitte den Administrator fragen."],
  [NO_ENTRY, "Bitte den Administrator fragen."],
  [NEEDS_CLASSIC, "Bitte den Administrator fragen."],
  [NOT_LOADED, "Bitte das Fenster schließen und es noch einmal versuchen."],
]);

// Refused carries the sentence the person gets. Everything else that is
// thrown on the way becomes NOT_LOADED, so an error of the engine can never
// turn into a window that shows an app all the same.
class Refused extends Error {
  constructor(msg) {
    super(msg);
    this.shown = msg;
  }
}

// The content types the frame knows, by the ending of a file inside the
// ui-tar. A list and not a guess: a type decides how the browser treats the
// bytes, and "whatever the app called it" is not a decision the shell hands
// over. Anything else goes in as application/octet-stream, which shows
// nothing and runs nothing.
const TYPES = new Map(Object.entries({
  html: "text/html", htm: "text/html", css: "text/css", js: "text/javascript",
  mjs: "text/javascript", json: "application/json", txt: "text/plain",
  svg: "image/svg+xml", png: "image/png", jpg: "image/jpeg", jpeg: "image/jpeg",
  gif: "image/gif", webp: "image/webp", ico: "image/x-icon", avif: "image/avif",
  woff: "font/woff", woff2: "font/woff2", ttf: "font/ttf", otf: "font/otf",
  mp3: "audio/mpeg", wav: "audio/wav", ogg: "audio/ogg", mp4: "video/mp4", webm: "video/webm",
}));

function typeOf(path) {
  const dot = path.lastIndexOf(".");
  const ext = dot < 0 ? "" : path.slice(dot + 1).toLowerCase();
  return TYPES.get(ext) ?? "application/octet-stream";
}

// UICache keeps the user interfaces that have already been proven, by app and
// version. A hit means: this app at this version was checked against the
// trust list that is in force, and these are the files that came out of the
// archive whose checksum matched.
//
// clear() is what ties it to the trust list. A new version of the list may
// take a store's key away or an admin's right to run root tasks, and a UI
// that was proven under the old one is not proven any more - so Task 20 empties
// this at every new version of the list, and nothing here outlives it.
export class UICache {
  #entries = new Map();
  #bytes = 0;

  #key(app, version) {
    // Both parts, and a separator that neither may hold: an app id may carry
    // dots, a version may carry dots and dashes, and neither may carry a
    // space (text.validAppID, text.validVersion). Without one, two different
    // pairs could spell the same key and one app would get the other's UI.
    return `${app} ${version}`;
  }

  get(app, version) {
    if (typeof app !== "string" || typeof version !== "string") return null;
    return this.#entries.get(this.#key(app, version)) ?? null;
  }

  put(app, version, files) {
    if (typeof app !== "string" || typeof version !== "string" || !Array.isArray(files)) return;
    let weight = 0;
    for (const f of files) weight += f.data.length;
    // A single UI heavier than the whole cache is not kept at all: throwing
    // everything else out for it would cost the next window its cache too.
    if (weight > CACHE_BYTES) return;
    const key = this.#key(app, version);
    this.#drop(key);
    // The oldest go first, and a Map hands its keys back in the order they
    // were put in, so the first key is the oldest.
    while (this.#entries.size >= CACHE_APPS || this.#bytes + weight > CACHE_BYTES) {
      const oldest = this.#entries.keys().next();
      if (oldest.done) break;
      this.#drop(oldest.value);
    }
    this.#entries.set(key, files);
    this.#bytes += weight;
  }

  clear() {
    this.#entries.clear();
    this.#bytes = 0;
  }

  #drop(key) {
    const had = this.#entries.get(key);
    if (had === undefined) return;
    for (const f of had) this.#bytes -= f.data.length;
    this.#entries.delete(key);
  }
}

// The library, read once. It is the shell's own file, so it is fetched from
// the shell's origin like every other script of the page; the app's frame
// never fetches anything.
let ngcSource = null;
async function ngcLibrary() {
  if (ngcSource !== null) return ngcSource;
  const res = await fetch(NGC_JS, { cache: "no-store" });
  if (!res.ok) throw new Error("appframe: ngc.js");
  ngcSource = await res.text();
  return ngcSource;
}

// openAppUI fills win with the app's user interface, or with the reason it
// will not run it.
export async function openAppUI(win, { app, link, trust, cache } = {}) {
  // The window may be closed before any of this is done - fetching a UI takes
  // as long as it takes. From here on every step asks whether it still has a
  // window, and the listeners it hangs up come down with it.
  let gone = false;
  const stop = [];
  // The channels this window holds open, by the number the APP gave them. One
  // map per window, built here and held by this call alone: a frame can reach
  // nothing but its own channels, because this is the only place a number of
  // its is looked up, and only this frame's messages get in (onMessage below).
  const streams = new Map();
  win.onClose = () => {
    gone = true;
    for (const f of stop) {
      try {
        f();
      } catch {
        // A listener that will not come off must not stop the others, and it
        // must never leave a window that cannot be closed.
      }
    }
  };
  // A window that is closed gives its channels up. Not left to the garbage
  // collector: a channel nobody reads any more still counts against the
  // session's bound, and on the other side a service goes on running.
  stop.push(() => {
    for (const s of streams.values()) closeQuietly(s);
    streams.clear();
  });

  let files, library;
  try {
    [files, library] = await Promise.all([verifiedUI(app, link, trust, cache), ngcLibrary()]);
  } catch (e) {
    if (!gone) refuse(win, e);
    return;
  }
  if (gone) return;

  // A word for this frame. It says which frame a message belongs to, not who
  // may send one - that is decided by the window the message came from, in
  // onMessage below, and by nothing else. The app can read this word out of
  // its own document, which is why it may never be the thing that decides.
  // crypto.randomUUID is in every browser NGC asks for.
  const token = crypto.randomUUID();
  let html;
  try {
    html = buildDocument(app, files, token, library);
  } catch (e) {
    refuse(win, e);
    return;
  }

  const frame = el("iframe", "appframe");
  // The sandbox stands in the answer's header as well (Page::FRAME_CSP), so
  // the frame has an origin of its own however it is loaded. It is repeated
  // here because this attribute is what a reader of the shell sees, and
  // because a frame that lost its header would otherwise lose its walls
  // silently.
  frame.setAttribute("sandbox", "allow-scripts");
  frame.setAttribute("referrerpolicy", "no-referrer");
  frame.setAttribute("allow", "");
  // A frame is a document, and a document in the accessibility tree needs a
  // name. It is the app's name, and it goes in as an attribute value, never
  // as markup.
  frame.title = app.name;
  frame.src = FRAME_PAGE;

  // toFrame answers this frame and only this frame. "*" as the target, because
  // the frame has an origin of its own and "null" is no origin a postMessage
  // may name; the window it goes to is this frame's, and that window only ever
  // held the document the shell put there.
  const toFrame = (msg) => {
    if (frame.contentWindow) frame.contentWindow.postMessage({ ngc: 1, ...msg }, "*");
  };

  // hangUp ends one channel for a reason of the shell's and tells the app, so
  // nobody is left waiting on a number that is gone. The three other ways out of
  // this map each say so themselves: the channel's own close, the app asking for
  // one, and the window closing.
  const hangUp = (id, code) => {
    const s = streams.get(id);
    streams.delete(id);
    if (s) closeQuietly(s);
    toFrame({ id, op: "closed", status: "error", message: codeMessage(code) });
  };

  // doOpen gives the app a channel that stays open.
  //
  // THE APP ID IS THE SHELL'S, exactly as in doCall: it comes from the window
  // this frame was opened for and never out of the message. The service and the
  // action are the app's to choose, but only from what its own manifest
  // declared - which is what app.actions holds.
  const doOpen = async (m) => {
    let params;
    try {
      params = encodeAppParams(m.params);
    } catch {
      toFrame({ id: m.id, op: "closed", status: "error", message: codeMessage(CODES.PROTOCOL) });
      return;
    }
    if (!validName(m.service) || !validName(m.action)
      || !Array.isArray(app.actions) || !app.actions.includes(m.action)) {
      toFrame({ id: m.id, op: "closed", status: "error", message: codeMessage(CODES.FORBIDDEN) });
      return;
    }
    // THE NUMBER IS TAKEN BEFORE THE AWAIT, and that it was free is decided in
    // the same breath. Two opens in one turn of the event loop would otherwise
    // both get past the bound, and an app that sent one number twice would take
    // the channel of its own earlier open away - with the second answer
    // arriving on the first one's listeners.
    if (streams.has(m.id) || streams.size >= MAX_FRAME_STREAMS) {
      toFrame({ id: m.id, op: "closed", status: "error", message: codeMessage(CODES.LIMIT) });
      return;
    }
    streams.set(m.id, null);
    let stream;
    try {
      stream = await link.open(app.id, m.service, m.action, params, { node: app.node });
    } catch {
      streams.delete(m.id);
      toFrame({ id: m.id, op: "closed", status: "error", message: codeMessage(CODES.UNREACHABLE) });
      return;
    }
    // The window was closed, or the number was given up, while the channel was
    // still on its way. null is "this open of ours is still the one running";
    // anything else means the slot is not ours any more.
    if (gone || streams.get(m.id) !== null) {
      closeQuietly(stream);
      streams.delete(m.id);
      return;
    }
    streams.set(m.id, stream);
    stream.onData = (chunk) => {
      let b;
      try {
        // A copy, taken before anything else looks at it (b64.bytesOf): what
        // goes to the app is what arrived, and not a view somebody else can
        // still change. postMessage copies again on the way out, so the app
        // holds no reference into the shell either.
        b = bytesOf(chunk, "appframe: not bytes");
      } catch {
        // A channel that answers with something that is not bytes is a channel
        // the shell cannot pass on. It is ended rather than handed through: an
        // app that was given a number where bytes belong would read it as bytes.
        hangUp(m.id, CODES.PROTOCOL);
        return;
      }
      toFrame({ id: m.id, op: "data", data: b });
    };
    stream.onClose = (status, message) => {
      streams.delete(m.id);
      // The status and the message are the channel's own and go on untouched:
      // what a service's ending means is the app's contract with it.
      toFrame({ id: m.id, op: "closed", status, message });
    };
  };

  const doSend = (m) => {
    const s = streams.get(m.id);
    // undefined is "no such channel", null is "still opening". Neither can be
    // written to, and neither is worth an answer: the app learns what became of
    // its channel from the close it gets either way.
    if (!s) return;
    let b;
    try {
      b = bytesOf(m.data, "appframe: not bytes");
    } catch {
      hangUp(m.id, CODES.PROTOCOL);
      return;
    }
    // The channel's own bound decides how much may be in flight; what a throw
    // here means is that this channel is over.
    Promise.resolve(s.write(b)).catch(() => hangUp(m.id, CODES.UNREACHABLE));
  };

  const doEnd = (m) => {
    const s = streams.get(m.id);
    if (!s) return;
    Promise.resolve(s.end()).catch(() => hangUp(m.id, CODES.UNREACHABLE));
  };

  // The app gives a channel up itself. It is answered at once rather than left
  // to the other side's CLOSE: the app is waiting to be allowed to forget the
  // number, and a close it asked for is no error.
  const doCloseStream = (m) => {
    const s = streams.get(m.id);
    if (s === undefined) return;
    streams.delete(m.id);
    if (s !== null) closeQuietly(s);
    toFrame({ id: m.id, op: "closed", status: CLOSE_OK, message: "" });
  };

  let handed = false;
  const onMessage = (e) => {
    // FROM THIS FRAME OR FROM NOWHERE. Everything below reads a message as
    // data, never as an instruction: the app id is this window's own, and no
    // field of a message can change which app the shell thinks it is talking
    // to.
    if (e.source !== frame.contentWindow) return;
    const m = e.data;
    if (!m || m.ngc !== 1) return;
    if (m.op === "ready") {
      // The frame says its bootstrap stands. It gets the document once: a
      // second one would be an app swapped under a window the person is
      // already using.
      if (handed) return;
      handed = true;
      frame.contentWindow.postMessage({ ngc: 1, op: "doc", html }, "*");
      return;
    }
    if (m.token !== token || typeof m.id !== "number") return;
    // The number has to be one the app could have drawn. ngc.js counts from 1
    // upwards, and a number that is not a whole one would make two different
    // messages name the same channel in a Map that tells 1 from "1".
    if (!Number.isSafeInteger(m.id) || m.id < 1) return;
    if (m.op === "call") doCall(frame, app, link, m);
    else if (m.op === "open") doOpen(m);
    else if (m.op === "send") doSend(m);
    else if (m.op === "end") doEnd(m);
    else if (m.op === "close") doCloseStream(m);
  };
  window.addEventListener("message", onMessage);
  stop.push(() => window.removeEventListener("message", onMessage));

  // From here the app is told when the shell's look changes; see openFrames
  // below. It is the only thing the shell pushes at an app unasked, and it
  // carries nothing about anybody.
  openFrames.add(frame);
  stop.push(() => openFrames.delete(frame));

  clear(win.body);
  win.body.append(frame);

  // THE KEYBOARD GOES INTO THE APP. A frame is a document of its own, so a key
  // press reaches the app only while the browser has given the frame the focus,
  // and there is nothing the shell could forward instead: forwarding would mean
  // the shell reading every key somebody types into an app, which is the one
  // thing the frame is there to stop. So the frame is sent the focus here, the
  // way an installed desktop puts the cursor into the app it just started, and
  // window.js sends it there again whenever this window comes to the front.
  //
  // Only while nobody else is using the keyboard: a user interface takes as
  // long to arrive as it takes, and in the meantime the person may have opened
  // the start menu or another window. Taking the focus out from under them is
  // worse than a window that has to be clicked once.
  const active = document.activeElement;
  if (win.desktop.focused === win
    && (active === null || active === document.body || win.el.contains(active))) {
    frame.focus();
  }
}

// closeQuietly gives a channel up and swallows whatever that throws. The
// channel is being let go either way, and a channel that will not close is no
// reason to leave the next one open. The code says why; "ok" is the right word
// where nothing went wrong and only this side is done with it.
function closeQuietly(stream, code = CLOSE_OK) {
  if (!stream || typeof stream.close !== "function") return;
  try {
    const done = stream.close(code);
    // The real channel's close is asynchronous; a rejection of it is the link
    // being gone, which is exactly what we were about to accept.
    if (done && typeof done.catch === "function") done.catch(() => {});
  } catch {
    // Already gone.
  }
}

// The frames that are open, and ONE listener for all of them. theme.js keeps
// its listeners in a set it never removes from - it is the page's own look
// and nothing there ever goes away - so a listener per window would be a
// listener per window the page keeps for as long as it lives.
const openFrames = new Set();
onThemeChange((t) => {
  const a = accent();
  for (const f of openFrames) {
    // "*" as the target, because the frame has an origin of its own and
    // "null" is no origin a postMessage may name. The message is addressed
    // to one window - this frame's - and that window only ever holds the
    // document the shell put there.
    if (f.contentWindow) f.contentWindow.postMessage({ ngc: 1, op: "theme", theme: t, accent: a }, "*");
  }
});

// doCall hands one question of the app to its own service.
//
// THE APP ID IS THE SHELL'S. It comes from the window this frame was opened
// for, never out of the message: an app that could name an app id would be an
// app that can act as another one. The service and the action are the app's
// to choose, but only from what its own manifest declared.
async function doCall(frame, app, link, m) {
  const answer = (extra) => {
    if (frame.contentWindow) frame.contentWindow.postMessage({ ngc: 1, id: m.id, op: "result", ...extra }, "*");
  };
  let params;
  try {
    params = encodeAppParams(m.params);
  } catch {
    answer({ error: codeMessage(CODES.PROTOCOL) });
    return;
  }
  if (!validName(m.service) || !validName(m.action)
    || !Array.isArray(app.actions) || !app.actions.includes(m.action)) {
    answer({ error: codeMessage(CODES.FORBIDDEN) });
    return;
  }
  try {
    // The answer is the service's own, and what it means is the app's own
    // contract with it. The shell hands the bytes over as they came: reading
    // them would be the shell deciding what an app's data means, and getting
    // that wrong is a hole in the shell rather than in the app.
    //
    // THE NODE GOES WITH IT, here and in doOpen. A service of an app runs on the
    // controller the app is installed on, and the channel to it is encrypted end
    // to end with that controller's key (design 4.3 and 4.5). Which controller
    // that is stands in the app's own row and nowhere else the link could reach,
    // so the shell is the one who has to say it. The stand-in ignores it; the
    // adapter of Task 17b reads it, and since Task 28 it really opens that
    // channel with it (net/world.js, appChannel).
    //
    // THE ONE SENTENCE FOR EVERY WAY THIS CAN FAIL stays right. The adapter
    // refuses with three different words (no node, no session key, a node the
    // trust list does not carry) and the channel throws its own code - and all
    // of them mean the same thing to the app: its service did not answer. Which
    // of them it was is for the person at the shell, not for the app, and an app
    // that could tell them apart would learn about this installation's trust
    // list from its own error message.
    answer({ value: await link.call(app.id, m.service, m.action, params, { node: app.node }) });
  } catch {
    answer({ error: codeMessage(CODES.UNREACHABLE) });
  }
}

// encodeAppParams turns what the app sent into the parameter bytes of a
// channel: strict JSON of an object, at most wire.MAX_PARAMS bytes. The app's
// value crossed a message boundary, so it is already a copy of its own -
// what is checked here is what goes out.
function encodeAppParams(params) {
  if (params === undefined || params === null) return new Uint8Array(0);
  const b = new TextEncoder().encode(JSON.stringify(params));
  if (b.length > MAX_PARAMS || !validParams(b)) throw new Error("appframe: params");
  return b;
}

// ---------------------------------------------------------------- the proof

// verifiedUI answers with the files of an app's user interface, and only ever
// with files that came out of an archive whose checksum a signature vouches
// for.
async function verifiedUI(app, link, trust, cache) {
  const kept = cache ? cache.get(app.id, app.version) : null;
  // A hit was proven against the trust list that was in force when it was put
  // in. WHAT EMPTIES IT: startDesktop hangs a listener on the trust store's
  // onChange (Task 20), and every new version of the list calls cache.clear()
  // before the apps are asked for again. So a hit can only ever have been
  // proven against the list that is in force now - and this is the line to
  // read first if the two ever come apart.
  if (kept !== null) return kept;
  const list = trustList(trust);
  const want = await uiHash(app, link, list);
  const files = uiTar(await uiBytes(app, link), want);
  if (cache) cache.put(app.id, app.version, files);
  return files;
}

// trustList reads the verified trust list out of what the shell was handed.
// No list, no app: without it there is nothing to check a proof against, and
// running a UI unchecked is the one thing this file exists to prevent.
function trustList(trust) {
  const l = typeof trust?.current === "function" ? trust.current()?.list : null;
  if (l === null || typeof l !== "object") throw new Refused(NOT_GENUINE);
  return l;
}

// uiHash asks the API for the app's UI proof and answers with the checksum
// the proof vouches for.
async function uiHash(app, link, list) {
  const action = core.lookupByKey(core.APP_DESKTOP, core.ACT_UI_PROOF);
  const params = core.encodeParams(action, { app: app.id });
  let hash;
  try {
    // core.parseResult hands a ui-proof to core.parseUIProof, which has its
    // own rules; core.verifyUIProof then checks the store's signature or the
    // admin's activation against the list. Both refuse with one error, and
    // one error is what the person sees.
    const proof = core.parseResult(action, await link.call(action.app, action.service, action.name, params));
    hash = await core.verifyUIProof(proof, app.id, app.version, list);
  } catch {
    throw new Refused(NOT_GENUINE);
  }
  if (!(hash instanceof Uint8Array) || hash.length !== 32) throw new Refused(NOT_GENUINE);
  return hash;
}

// uiBytes reads the whole ui-tar off the channel, bounded. It answers with
// ONE array that nobody outside this call holds: every chunk is copied out of
// the caller's reach as it arrives (b64.bytesOf), and the array below is
// built from those copies.
async function uiBytes(app, link) {
  const action = core.lookupByKey(core.APP_DESKTOP, core.ACT_UI);
  const params = core.encodeParams(action, { app: app.id });
  let stream;
  try {
    stream = await link.open(action.app, action.service, action.name, params);
  } catch {
    throw new Refused(NOT_LOADED);
  }
  return await new Promise((resolve, reject) => {
    const parts = [];
    let n = 0, done = false;
    // give ends this read, once. code says WHY the shell is breaking the channel
    // off and is left out where the channel ended by itself - a CLOSE back at a
    // channel that is already closed says nothing to anybody.
    //
    // THE CODE IS NOT OPTIONAL WHERE IT IS GIVEN, and that was measured: the
    // line used to read stream.close(), and the real channel builds its CLOSE
    // with wire.errorClose(code). Without a code that is encodeClose of a CLOSE
    // with an empty message, which wire REFUSES - inside an async method, so it
    // comes back as a rejected promise that the try below does not catch, and
    // the channel's number stays taken for the life of the session. With the
    // stand-in nothing showed, because its close() does nothing at all.
    const give = (fail, code) => {
      if (done) return;
      done = true;
      if (fail) {
        if (code !== undefined) closeQuietly(stream, code);
        reject(fail);
        return;
      }
      const out = new Uint8Array(n);
      let o = 0;
      for (const p of parts) {
        out.set(p, o);
        o += p.length;
      }
      resolve(out);
    };
    stream.onData = (chunk) => {
      if (done) return;
      let b;
      try {
        b = bytesOf(chunk, "appframe: not bytes");
      } catch {
        give(new Refused(NOT_LOADED), CODES.PROTOCOL);
        return;
      }
      n += b.length;
      // Decided on the copy that was just taken, so a chunk cannot grow
      // between the count and the use. Over the bound the channel is closed
      // rather than drained: the rest of an archive nobody will read costs
      // the person time and the machine memory.
      if (n > MAX_UITAR) {
        give(new Refused(NOT_LOADED), CODES.TOOLARGE);
        return;
      }
      parts.push(b);
    };
    stream.onClose = (status) => give(status === "ok" ? null : new Refused(NOT_LOADED));
  });
}

// uiTar is step 3, 4 and 5 in one, and it is written as one function on
// purpose: BETWEEN THE HASH AND THE WALK THERE IS NOTHING. No await, no
// callback, no property of anybody else's object. The bytes were built by
// uiBytes and are held by this call alone, so the array the checksum was
// taken over is by construction the array the archive is read out of.
function uiTar(bytes, want) {
  if (!equal(sha256(bytes), want)) throw new Refused(NOT_GENUINE);
  let files;
  try {
    files = uitar.walkUI(bytes);
  } catch {
    // The archive is the one the signature vouches for and it is still no
    // ui-tar. That is not a broken download, it is a package that should
    // never have been signed.
    throw new Refused(NOT_GENUINE);
  }
  return files;
}

// ------------------------------------------------------------- the document

// buildDocument makes the app's page out of the files of the archive.
//
// Every address in it is replaced by the bytes behind it, as a data: address:
// the frame may make no connection, so a page that still pointed anywhere
// would simply be a page with holes in it. The replacing happens on the
// parsed tree and never on the text - a search for src="..." in a string
// finds what a browser does not and misses what it does.
function buildDocument(app, files, token, library) {
  // The entry page of the manifest has to be a safe relative path before it
  // is looked up at all - store.validPath, the same rule the archive holds
  // its own names to.
  if (typeof app.entry !== "string" || !validPath(app.entry)) throw new Refused(NO_ENTRY);
  const entry = "ui/" + app.entry;
  const byPath = new Map();
  for (const f of files) byPath.set(f.path, f);
  const page = byPath.get(entry);
  if (page === undefined) throw new Refused(NO_ENTRY);

  let source;
  try {
    source = fromUtf8(page.data);
  } catch {
    // A page that is no UTF-8 is a page whose characters nobody can agree on.
    throw new Refused(NO_ENTRY);
  }
  const doc = new DOMParser().parseFromString(source, "text/html");
  const head = doc.head;
  const dir = entry.slice(0, entry.lastIndexOf("/") + 1);
  const find = (ref) => byPath.get(resolve(dir, ref)) ?? null;
  const address = (ref) => {
    const f = find(ref);
    // A file the archive does not hold becomes an EMPTY address of the type
    // its name promised. An empty script and an empty stylesheet do nothing;
    // leaving the address as it stood would be an app pointing at the network.
    return dataURL(typeOf(String(ref ?? "")), f === null ? new Uint8Array(0) : f.data);
  };

  // A base of the app's own would move every address in the document, and
  // there is nothing left to point at.
  for (const b of doc.querySelectorAll("base")) b.remove();
  // Every http-equiv of the app's goes. Two of them matter: a policy of its
  // own, which is replaced by the document's below, and a refresh, which is
  // a page sending itself somewhere - to an address outside, with whatever
  // it wants to say in the query.
  //
  // WHAT THIS DOES NOT CLOSE, said here rather than left to be found: a
  // script can send its own frame away too (location = ...), and no policy
  // stops it - connect-src does not govern navigation, and the directive
  // that did (navigate-to) is gone from the standard. The frame cannot move
  // the shell (no allow-top-navigation) and cannot open a window (no
  // allow-popups), so what is left is an app sending ITSELF away. The app
  // was signed by a store or approved by an admin, which is what stands
  // between a user and that; it is not the frame.
  for (const m of doc.querySelectorAll("meta[http-equiv]")) m.remove();

  for (const s of doc.querySelectorAll("script")) {
    // A module cannot import anything out of a data: address - the browser
    // has no base to resolve against and refuses every specifier - so a
    // module would run halfway and stop, silently. Better to say it.
    if ((s.getAttribute("type") ?? "").trim().toLowerCase() === "module") throw new Refused(NEEDS_CLASSIC);
    if (s.hasAttribute("src")) {
      s.setAttribute("src", address(s.getAttribute("src")));
      continue;
    }
    // An inline script is part of the page that was proven, so it stays -
    // as an address, because the frame's policy allows scripts from data:
    // and from nowhere else. One rule for every script in the document.
    s.setAttribute("src", dataURL("text/javascript", new TextEncoder().encode(s.textContent ?? "")));
    s.textContent = "";
  }
  for (const l of doc.querySelectorAll("link[href]")) {
    const rel = (l.getAttribute("rel") ?? "").trim().toLowerCase();
    if (rel !== "stylesheet") {
      // Preloads, icons, manifests: nothing the frame may fetch. They go,
      // rather than becoming empty addresses that a browser still reports on.
      l.remove();
      continue;
    }
    const f = find(l.getAttribute("href"));
    l.setAttribute("href", dataURL("text/css", f === null ? new Uint8Array(0) : css(f.data, dir, find)));
  }
  for (const s of doc.querySelectorAll("style")) {
    s.textContent = cssText(s.textContent ?? "", dir, find);
  }
  for (const i of doc.querySelectorAll("img[src], source[src], video[src], audio[src], track[src]")) {
    i.setAttribute("src", address(i.getAttribute("src")));
  }
  // srcset holds a list of addresses with descriptors; the frame has no use
  // for a second picture and no way to fetch one.
  for (const i of doc.querySelectorAll("[srcset]")) i.removeAttribute("srcset");
  for (const e of doc.querySelectorAll("[style]")) {
    e.setAttribute("style", cssText(e.getAttribute("style") ?? "", dir, find));
  }

  // The policy of the document, first in the head so that nothing above it
  // could have loaded. It says the same as the answer's header (FRAME_CSP);
  // both hold, and the tighter one wins, so this one is what stays true if
  // the document is ever built somewhere else.
  const csp = doc.createElement("meta");
  csp.setAttribute("http-equiv", "Content-Security-Policy");
  csp.setAttribute("content", "default-src 'none'; script-src data:; style-src data: 'unsafe-inline'; "
    + "img-src data:; font-src data:; media-src data:; connect-src 'none'; form-action 'none'; "
    + "base-uri 'none'");
  head.prepend(csp);

  // ngc.js as the first script of the page, so the app's own scripts find it
  // however they are ordered.
  const lib = doc.createElement("script");
  lib.setAttribute("src", dataURL("text/javascript", new TextEncoder().encode(library)));
  lib.dataset.token = token;
  csp.after(lib);

  // What the shell looks like, before the first paint rather than after it.
  doc.documentElement.dataset.theme = currentTheme() || "dark";
  doc.documentElement.dataset.accent = accent();

  return "<!DOCTYPE html>\n" + doc.documentElement.outerHTML;
}

// resolve turns an address of the app's page into a path inside the archive,
// or null where it points at anything else. Everything with a scheme, a host
// or a leading slash is "anything else": the archive is the whole world of
// this document.
function resolve(dir, ref) {
  if (typeof ref !== "string") return null;
  const cut = ref.split("#")[0].split("?")[0].trim();
  if (cut === "" || cut.startsWith("/") || /^[a-zA-Z][a-zA-Z0-9+.\-]*:/.test(cut)) return null;
  const parts = (dir + cut).split("/");
  const out = [];
  for (const p of parts) {
    if (p === "" || p === ".") continue;
    if (p === "..") {
      // Out of the archive is out of the world. It is refused and not
      // clamped: a path that climbs is a path that meant something else.
      if (out.length === 0) return null;
      out.pop();
      continue;
    }
    out.push(p);
  }
  const path = out.join("/");
  // The archive's own rule, asked a second time on this side: uitar.walkUI
  // only ever hands out paths that pass it, so a reference that does not is
  // a reference to no file of this archive.
  if (!path.startsWith("ui/") || !validPath(path.slice(3))) return null;
  return path;
}

// css rewrites the url(...) of a stylesheet that came out of the archive.
function css(data, dir, find) {
  let src;
  try {
    src = fromUtf8(data);
  } catch {
    // A stylesheet that is no UTF-8 is a stylesheet no browser reads the same
    // way twice. It becomes an empty one; the app looks wrong and runs.
    return new Uint8Array(0);
  }
  return new TextEncoder().encode(cssText(src, dir, find));
}

// url(...) with or without quotes. CSS has no parser in a browser that could
// be handed a string, so this is the one place a pattern does the work; it is
// text going into text, never text going into a document, and what it misses
// stays an address the policy refuses rather than one the frame follows.
const URL_IN_CSS = /url\(\s*(?:"([^"]*)"|'([^']*)'|([^)"'\s]*))\s*\)/g;

function cssText(src, dir, find) {
  return src.replace(URL_IN_CSS, (whole, dq, sq, bare) => {
    const ref = dq ?? sq ?? bare ?? "";
    const f = find(ref);
    if (f === null) return "url()";
    return 'url("' + dataURL(typeOf(ref), f.data) + '")';
  });
}

// dataURL makes an address out of bytes. base64 and not the percent form,
// because a picture or a font is binary and percent-encoding it costs three
// characters a byte.
function dataURL(type, bytes) {
  return "data:" + type + ";base64," + base64(bytes);
}

// The bytes go in in pieces: String.fromCharCode is handed one argument per
// byte, and a whole 16 MiB file at once is more arguments than a call stack
// holds.
const CHUNK = 0x8000;

function base64(bytes) {
  let s = "";
  for (let i = 0; i < bytes.length; i += CHUNK) {
    s += String.fromCharCode.apply(null, bytes.subarray(i, i + CHUNK));
  }
  return btoa(s);
}

// ------------------------------------------------------------------ the rest

// accent is what the document is painted with. It is read off the document
// and not out of a variable of this file, so it cannot drift from what is on
// screen.
function accent() {
  return getComputedStyle(document.documentElement).getPropertyValue("--accent").trim() || "#2f7de1";
}

// refuse puts the reason in the window: what is the matter, and what to do.
// An empty window would say neither, and a window that closed itself would
// leave the person wondering whether they mis-clicked.
function refuse(win, e) {
  const msg = e instanceof Refused ? e.shown : NOT_LOADED;
  const pad = el("div", "pad app");
  const p = el("p", null, msg);
  // An error the person did not ask for is announced at once; the desktop's
  // own strip is for what merely happened.
  p.setAttribute("role", "alert");
  pad.append(p, el("p", "muted", HINTS.get(msg) ?? ""));
  clear(win.body);
  win.body.append(pad);
}

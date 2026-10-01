// The world the desktop runs in, built out of the real session: the link the
// surfaces speak, the trust list that is kept current from that session, and
// the session itself.
//
// WHY THIS FILE EXISTS. net/channel.js speaks in ACTIONS - call(link, action,
// {params, taskBytes, confirmBytes}), sendIn, receiveOut - and the surfaces
// speak in (app, service, name): shell/desktop.js, shell/appframe.js,
// apps/store.js and apps/users.js all hand over those three texts, the
// parameter BYTES and one option bag. Between the two there was nothing, and
// what stood in the gap was shell/fake-link.js. This is the seam, and it is
// the only place that knows both sides. It also assembles the world the
// desktop is started with, which is why a file under net/ reaches into
// shell/trustlist.js: the trust list hangs from the session, and the brief of
// this task put the assembly here.
//
// THE FLOW COLUMN DECIDES THE WAY, never a list written here. Every action of
// the shell stands in core.ACTIONS with a flow, and the row is looked up for
// every single call: a list in this file would be a second table beside the
// contract, and a second table drifts. What the column means here:
//
//   call, transfer -> channel.callBytes  (params or a signed task, then EOF)
//   in             -> channel.sendInBytes (the body streams as DATA)
//   out            -> channel.openOut, through open() and not through call()
//
// WHAT COMES BACK ARE BYTES, and that is deliberate. Every surface reads its
// own answer with the contract that owns it (core.parseResult,
// core.parseUIProof, uitar.walkUI). An adapter that handed an object over
// would be a second reader of the same document, and the one in the surface
// would then read what this file decided instead of what the API sent.
//
// WHAT IS NOT IN HERE, said plainly rather than left to be found: an app's own
// service. It has to be an END-TO-END channel to the app's controller, and one
// of the two things that needs is still missing - appService() below is the
// place, and it says which half.
import * as core from "../contracts/core.js";
import * as wire from "../contracts/wire.js";
import * as uitar from "../contracts/uitar.js";
import { bytesOf } from "../contracts/b64.js";
import { callBytes, openOut, sendInBytes } from "./channel.js";
import { startTrust } from "../shell/trustlist.js";
import { validAppID, validName } from "../contracts/text.js";

// The flows of core.ACTIONS, as the texts the table holds. core.js keeps them
// to itself on purpose - one name per flow, in one place - and the shell is
// meant to compare against the ROW and not against a flow it made up. A fifth
// flow in the contract reaches the default of the switch below, which refuses:
// an action whose way nobody here knows is an action nobody here may send.
const FLOW_CALL = "call";
const FLOW_IN = "in";
const FLOW_OUT = "out";
const FLOW_TRANSFER = "transfer";

// The largest answer of a FlowOut channel. The one FlowOut action is an app's
// user interface, so the bound is the largest ui-tar there can be: the files
// plus one header block per entry, which is what shell/appframe.js bounds the
// same archive with. Taken from the contract's own numbers, never written out
// as one.
const MAX_OUT = uitar.MAX_UI_TOTAL + uitar.MAX_ENTRIES * uitar.BLOCK;

// The status word of a CLOSE that ended well, as net/channel.js and
// shell/appframe.js both spell it: "I am done with this channel" and no error.
const CLOSE_OK = "ok";

// WorldError is what a caller gets for a way this adapter does not have. The
// text is German, because it can reach a person through a surface that shows
// what it caught; the surfaces show their own sentence for it today.
//
// `missing` names the piece that is not there yet, in one word, for the cases
// and for whoever comes next: "node" or "session-key" at appService() below,
// and "" where nothing is missing and the way is simply wrong.
export class WorldError extends Error {
  constructor(text, missing = "") {
    super(text);
    this.name = "WorldError";
    this.missing = missing;
  }
}

const NO_ACTION = "Diese Anfrage kennt NGC nicht.";
const WRONG_WAY = "Diese Anfrage geht so nicht.";
const NO_NODE = "Diese App sagt nicht, auf welchem Rechner ihr Dienst läuft.";
const NO_SESSION_KEY = "NGC kann den eigenen Dienst einer App noch nicht erreichen.";

// rowFor finds the action of three texts. null means "this is no action of
// NGC's own" - the own service of an app, which goes to appService below.
//
// A row the shell may not open at all (core.shellOpens) is refused:
// ngc.users/controller/trust, ngc.store/controller/stage and
// ngc.store/controller/license, which only the API opens and which the API
// answers a shell's OPEN for with E_FORBIDDEN. Refusing it here costs a
// channel less and tells the truth one round trip earlier.
function rowFor(app, service, name) {
  const a = core.lookup(app, service, name);
  if (a === null) return null;
  if (!core.shellOpens(a)) throw new WorldError(NO_ACTION);
  return a;
}

// appService is the own service of an app - the one thing the surfaces ask for
// that is no action of NGC's own. IT ALWAYS THROWS, and this is the place that
// says why, because this is the place where channel.openE2E would be called.
//
// WHAT IS IN HAND, since Task 25: the app, the service, the action, the params
// and THE NODE. shell/appframe.js reads the node out of the app's own row
// (app.node) and hands it over in the option bag of both doors - it is the one
// thing the link could not know by itself, and before this round the adapter
// took four arguments and the fifth fell on the floor. It does not any more:
// it arrives here, it is held to the contract's own rule for a node id
// (trust.validNode uses text.validName), and a call without one is refused
// with a DIFFERENT word than a call with one, so a bridge that stopped sending
// it cannot hide behind the other refusal.
//
// WHAT IS STILL MISSING, the whole of it: THE SESSION'S OWN DH KEY PAIR.
// channel.openE2E takes {app, service, action, params, taskBytes, node,
// controllerKey, sessionDH}. Six of those eight are here; controllerKey is one
// lookup away - trust.controllerKey(node) answers it out of the VERIFIED list
// and realWorld already holds that store - and sessionDH exists nowhere in
// this browser after the login: auth/login.js builds the key pair for
// Link.connect (keys.dh) and hands on inst, user, certHash and signKey, not
// it. So the next session starts in auth/login.js, by carrying keys.dh into
// the handover, and then here.
//
// NOT ARMED, and that is a decision and not an omission: until the key pair is
// there the answer stays a refusal. An ORDINARY channel for a service of an app
// would let the API read what only the controller may, which is the whole
// reason end to end exists. shell/appframe.js turns this into E_UNREACHABLE
// for the app, which is the truth.
// It takes no params: both doors have them in hand and openE2E would be given
// them here, but nothing about them is decided in this function, and an
// argument that is only read by a comment is an argument that reads like a bug.
function appService(app, service, name, node) {
  if (!validAppID(app) || !validName(service) || !validName(name)) {
    throw new WorldError(NO_ACTION);
  }
  // The node, carried through to the one place that would need it.
  if (!validName(node)) throw new WorldError(NO_NODE, "node");
  throw new WorldError(NO_SESSION_KEY, "session-key");
}

// own copies bytes a caller handed over, ONCE, at the door. Everything below
// this line works with that copy: the bytes travel into wire.encodeOpen,
// which writes them into the OPEN and holds the OPEN against parseOpen, and a
// caller who swapped them in between would have one document checked and
// another sent.
function own(bytes, what) {
  if (bytes === null || bytes === undefined) return null;
  return bytesOf(bytes, `world: ${what} are not bytes`);
}

// adapterFor puts the surfaces' three texts on top of one session link. The
// shape is exactly the one shell/fake-link.js hands over, so the desktop, the
// app frame, the store and the user administration change no line for it:
//
//   call(app, service, name, params, opts) -> Promise<Uint8Array>
//   open(app, service, name, params, opts) -> {onData, onClose, close}
//   onNotice(fn)
//
// BOTH DOORS TAKE FIVE ARGUMENTS, and open() did not until Fixrunde 1: Task 25
// gave shell/appframe.js the node of an app and it hands it over in the option
// bag of both. A door that took four of five arguments promised more than it
// did, and JavaScript says nothing about it.
export function adapterFor(link) {
  // EVERY listener, not the last one. net/link.js holds exactly ONE notice
  // handler, so an adapter that handed each caller's listener straight on
  // would silently drop the one before it. Today the desktop hangs up one;
  // tomorrow is not the day to find that out.
  const notices = new Set();
  if (typeof link?.onNotice === "function") {
    link.onNotice((bytes) => {
      // The bytes are handed on as they are. net/link.js has already held
      // them against core.parseNotice - a notice that breaks its contract
      // ends the link there - and the desktop reads them with the contract
      // itself. Reading them here would be a second reader of one document.
      for (const fn of notices) fn(bytes);
    });
  }

  return {
    // call takes a FlowCall, FlowIn or FlowTransfer action and answers with
    // the result BYTES. opts is net/channel.js's own option bag:
    // {taskBytes, confirmBytes} for a signed root task, {stream, onProgress}
    // for a body that streams.
    async call(app, service, name, params, opts) {
      const a = rowFor(app, service, name);
      const o = opts ?? {};
      const p = own(params, "params");
      if (a === null) return appService(app, service, name, o.node);
      const taskBytes = own(o.taskBytes, "task");
      const confirmBytes = own(o.confirmBytes, "confirmation");
      // o.node is NOT read for an action of NGC's own, and that is right: those
      // channels go to the API, and where a root task names a controller the
      // node travels inside the SIGNED task (task.buildTask), never in the
      // OPEN. An adapter that put it in the OPEN as well would offer the API a
      // second, unsigned answer to the same question.
      switch (a.flow) {
        case FLOW_CALL:
        case FLOW_TRANSFER:
          return callBytes(link, a, { params: p, taskBytes, confirmBytes });
        case FLOW_IN:
          return sendInBytes(link, a, {
            params: p, taskBytes, stream: o.stream, onProgress: o.onProgress,
          });
        // FlowOut is a channel that stays open and belongs in open(); taking
        // it here would mean bounding an answer in the one place that cannot
        // know what the caller will do with it. A flow that is neither of the
        // four is one the contract grew after this file, and an action whose
        // way nobody here knows is an action nobody here sends.
        default:
          throw new WorldError(WRONG_WAY);
      }
    },

    // open takes the FlowOut action and answers with the shape the app frame
    // reads: two listeners that are PROPERTIES - the frame assigns
    // stream.onData and stream.onClose after this call came back - and a
    // close(code).
    //
    // NOTHING IS HANDED OVER BEFORE THE CHANNEL STANDS. open() waits for
    // channel.openOut: the OPEN is out, its first grant is in, the EOF has gone
    // out. A channel that cannot be opened therefore THROWS from here, where
    // the frame catches it, instead of being reported later through onClose on
    // a stream that never was one. (public/assets/ngc.js cannot do that - a
    // postMessage bridge has nothing to wait for and hands the app its stream
    // at once, and onClose is what tells it afterwards. Here there is something
    // to wait for, so we wait.)
    //
    // THE ANSWER IS HANDED OVER IN ONE PIECE, after the caller has had the
    // chance to hang his listeners up, which is why the delivery waits for the
    // next turn of the event loop. The channel collects it either way
    // (Channel.result holds the pieces), and the bound is the channel's: past
    // MAX_OUT it closes with E_TOOLARGE instead of reading on.
    async open(app, service, name, params, opts) {
      const a = rowFor(app, service, name);
      const p = own(params, "params");
      if (a === null) return appService(app, service, name, (opts ?? {}).node);
      if (a.flow !== FLOW_OUT) throw new WorldError(WRONG_WAY);
      const ch = await openOut(link, a, { params: p });
      let gone = false;
      const s = {
        onData: null,
        onClose: null,
        // close(code) REALLY ENDS THIS CHANNEL, and with that code.
        //
        // Fixrunde 1: this used to set a flag and nothing else, so a late answer
        // reached nobody but the channel ran on - and shell/appframe.js passes a
        // code (closeQuietly) because the real channel builds its CLOSE with
        // wire.errorClose(code) and refuses the frame without one. A close()
        // that takes a code and drops it is the same kind of defect as a door
        // that takes five arguments and reads four.
        //
        // Without a code it is "ok": a caller who names none is done with the
        // channel and is reporting no error, which is the word Channel.close
        // and shell/appframe.js (closeQuietly's default) both use for that.
        close(code) {
          gone = true;
          return ch.close(typeof code === "string" && code !== "" ? code : CLOSE_OK);
        },
      };
      ch.result(MAX_OUT).then(
        (bytes) => setTimeout(() => {
          if (gone) return;
          s.onData?.(bytes);
          s.onClose?.("ok", "");
        }, 0),
        (e) => setTimeout(() => {
          if (gone) return;
          // The frame branches on "ok" and on nothing else, and the code is
          // the channel's own where it has one.
          s.onClose?.(typeof e?.code === "string" && e.code !== "" ? e.code : wire.CODES.UNREACHABLE, "");
        }, 0),
      );
      return s;
    },

    onNotice(fn) {
      if (typeof fn === "function") notices.add(fn);
    },
  };
}

// realWorld is the whole handover in one call: the link the surfaces speak,
// the trust list opened against the anchor and kept current from this very
// session, and the session that was just opened.
//
// It answers the shape fakeWorld() answers, so shell/main.js hands the same
// four values on and startDesktop changes no line.
//
// config is taken and not used, and that is on purpose rather than an
// oversight: it is the shape the brief of this task fixed, and the initial
// setup (Task 19) is what will put the release key of this installation in
// reach. Until then releaseKey is null, exactly as the login answers it, and
// apps/store.js says so itself instead of trusting an answer.
export async function realWorld({ config, anchor, session, link } = {}) {
  // THE TRUST LIST BEFORE ANYTHING IS SHOWN. It is opened against the anchor
  // and followed to its newest version, and the session's own trust hints
  // keep it there - startTrust hangs that up on this link. A chain that does
  // not fit raises the alarm and throws, and nothing of the desktop is built:
  // nothing may be shown before it is known what may be shown.
  let trust;
  try {
    trust = await startTrust({ anchor, installation: session?.inst ?? "", link });
  } catch (e) {
    // A chain that does not fit has raised the alarm and logged the session
    // out. The socket is what is left of it, and a socket nobody reads holds
    // a session on the API for nothing - so it goes here, before the error
    // travels on to whoever has to put a message on the screen.
    if (typeof link?.close === "function") link.close();
    throw e;
  }
  return { link: adapterFor(link), trust, session: session ?? null, releaseKey: null };
}

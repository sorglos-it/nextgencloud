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
//   call, transfer -> channel.callBytes   (params or a signed task, then EOF)
//   in            -> channel.sendInBytes (the body streams as DATA)
//   out           -> channel.receiveOut, through open() and not through call()
//
// WHAT COMES BACK ARE BYTES, and that is deliberate. Every surface reads its
// own answer with the contract that owns it (core.parseResult,
// core.parseUIProof, uitar.walkUI). An adapter that handed an object over
// would be a second reader of the same document, and the one in the surface
// would then read what this file decided instead of what the API sent.
//
// WHAT IS NOT IN HERE, said plainly rather than left to be found: an app's own
// service. shell/appframe.js asks link.call(app.id, service, action) for it,
// and that has to be an END-TO-END channel to the app's controller
// (channel.openE2E) - which needs the session's own DH key pair and the node
// the app runs on, and the login hands over neither. The three texts of the
// call do not name a node either. So an action that stands in no row of
// core.ACTIONS is REFUSED here; the frame answers the app with E_UNREACHABLE,
// which is the truth. Opening an ordinary channel for it instead would let
// the API read what only the controller may - the whole reason end to end
// exists - and that is not a trade this file makes.
import * as core from "../contracts/core.js";
import * as wire from "../contracts/wire.js";
import * as uitar from "../contracts/uitar.js";
import { bytesOf } from "../contracts/b64.js";
import { callBytes, receiveOut, sendInBytes } from "./channel.js";
import { startTrust } from "../shell/trustlist.js";

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

// WorldError is what a caller gets for a way this adapter does not have. The
// text is German, because it can reach a person through a surface that shows
// what it caught; the surfaces show their own sentence for it today.
export class WorldError extends Error {
  constructor(text) {
    super(text);
    this.name = "WorldError";
  }
}

const NO_ACTION = "Diese Anfrage kennt NGC nicht.";
const WRONG_WAY = "Diese Anfrage geht so nicht.";

// rowFor finds the action of three texts and refuses everything else: an
// action that stands in no row, and a row the shell may not open at all
// (core.shellOpens) - ngc.users/controller/trust, ngc.store/controller/stage
// and ngc.store/controller/license, which only the API opens and which the API
// answers a shell's OPEN for with E_FORBIDDEN. Refusing it here costs a
// channel less and tells the truth one round trip earlier.
function rowFor(app, service, name) {
  const a = core.lookup(app, service, name);
  if (a === null || !core.shellOpens(a)) throw new WorldError(NO_ACTION);
  return a;
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
//   open(app, service, name, params)       -> {onData, onClose, close}
//   onNotice(fn)
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
      const taskBytes = own(o.taskBytes, "task");
      const confirmBytes = own(o.confirmBytes, "confirmation");
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
    // close().
    //
    // THE ANSWER IS HANDED OVER IN ONE PIECE, after the caller has had the
    // chance to hang his listeners up, which is why the delivery waits for
    // the next turn of the event loop. The channel collects it either way
    // (Channel.result holds the pieces), and the bound is the channel's: past
    // MAX_OUT it closes with E_TOOLARGE instead of reading on.
    //
    // ASYNC although nothing in it awaits, because the stand-in's open() is:
    // a caller who wrote link.open(...).catch(...) instead of await would
    // never see a refusal that was thrown straight out of here.
    async open(app, service, name, params) {
      const a = rowFor(app, service, name);
      if (a.flow !== FLOW_OUT) throw new WorldError(WRONG_WAY);
      const p = own(params, "params");
      let gone = false;
      const s = {
        onData: null,
        onClose: null,
        // close() says "I want nothing more of this". The channel is bounded
        // and ends on its own; what this does is make sure a late answer
        // reaches nobody, which is what the frame closes for.
        close() { gone = true; },
      };
      receiveOut(link, a, { params: p, maxBytes: MAX_OUT }).then(
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

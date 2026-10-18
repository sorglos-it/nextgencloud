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
// AN APP'S OWN SERVICE IS IN HERE TOO, and since Task 28 it is armed. It is
// the one thing the surfaces ask for that stands in no row of core.ACTIONS,
// and it may never be an ordinary channel: an ordinary one would let the API
// read what only the app's controller may. So it is an END-TO-END channel -
// channel.openE2E - and appChannel() below is the one place that puts its
// three remaining pieces together: the node, the session's own DH key pair and
// the controller's Noise key out of the VERIFIED trust list. Each of the three
// has its own word for the refusal, so none of them can hide behind another.
import * as core from "../contracts/core.js";
import * as wire from "../contracts/wire.js";
import * as uitar from "../contracts/uitar.js";
import { bytesOf } from "../contracts/b64.js";
import { callBytes, openE2E, openOut, sendInBytes } from "./channel.js";
import { NOT_TRUSTED, startTrust } from "../shell/trustlist.js";
import { validAppID, validName } from "../contracts/text.js";
import { t } from "../lang.js";

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
// `missing` names the piece that is not there, in one word, for the cases and
// for whoever comes next: "node", "session-key" or "controller-key" at
// appChannel() below, and "" where nothing is missing and the way is simply
// wrong. THREE WORDS AND NOT ONE, because the three say three different things
// to the person in front of the screen - an app that does not name its
// controller, a session without its own key, and a server that does not stand
// in this installation's trust list - and a case that hides behind another is
// the mistake this project has made most often.
export class WorldError extends Error {
  constructor(text, missing = "") {
    super(text);
    this.name = "WorldError";
    this.missing = missing;
  }
}

const NO_ACTION = () => t("world.noaction");
const WRONG_WAY = () => t("world.wrongway");
const NO_NODE = () => t("world.nonode");
// No session key pair in this adapter. It is NOT a missing feature any more -
// since Task 28 the login carries the pair through (auth/login.js, sessionDH)
// and realWorld hands it here - so what is wrong is THIS session, and the
// sentence says that and names what helps.
const NO_SESSION_KEY = () => t("world.nosessionkey");
// No trust list in this adapter. A DIFFERENT fact from the one below: there is
// no list to ask, so nothing is known about this server either way, and
// "this server is not in your trust list" would be a sentence about the server
// that says nothing true about it.
const NO_TRUST_LIST = () => t("world.notrustlist");
// A node the verified list does not carry. The sentence is shell/trustlist.js's
// own, and not a second wording of it: it is the same fact, and NotTrusted is
// what answers it.
const NO_CONTROLLER_KEY = () => NOT_TRUSTED();

// rowFor finds the action of three texts. null means "this is no action of
// NGC's own" - the own service of an app, which goes to appChannel below.
//
// A row the shell may not open at all (core.shellOpens) is refused:
// ngc.users/controller/trust, ngc.store/controller/stage and
// ngc.store/controller/license, which only the API opens and which the API
// answers a shell's OPEN for with E_FORBIDDEN. Refusing it here costs a
// channel less and tells the truth one round trip earlier.
function rowFor(app, service, name) {
  const a = core.lookup(app, service, name);
  if (a === null) return null;
  if (!core.shellOpens(a)) throw new WorldError(NO_ACTION());
  return a;
}

// appChannel opens the end-to-end channel of an app's own service - the one
// thing the surfaces ask for that is no action of NGC's own. Until Task 28 it
// always threw; what it does now is put the eight arguments of
// channel.openE2E together, and refuse while one of them is not there.
//
// WHERE THE EIGHT COME FROM, because that is the whole of this function:
//
//   app, service, action  the surface's three texts, held against the
//                         contract's own rules (text.validAppID, validName)
//   params                the caller's bytes, copied once at the door
//   taskBytes             null - see below
//   node                  the option bag of both doors. shell/appframe.js
//                         reads it out of the app's own row (app.node); it is
//                         the one thing the link could not know by itself
//   sessionDH             the X25519 pair auth/login.js built for this session
//                         and carried into the handover, by way of realWorld
//   controllerKey         shell/trustlist.js's controllerKey(node), OUT OF THE
//                         VERIFIED LIST - never out of an answer of the API,
//                         which is the whole point of end to end
//
// TASKBYTES IS NULL, and that is right for the ordinary step - measured on BOTH
// sides, not assumed:
//
//   - This side: shell/appframe.js puts exactly one thing in the option bag of
//     both doors, `node` (doCall and doOpen), and nothing in the shell builds a
//     signed root task for an app's own service.
//   - The other side: the controller's access.Check hands task.Check the OPEN's
//     task, and WITHOUT one task.Check answers "no task needed" as long as the
//     app does not want root and trust.List.NeedsConfirm says no for this user.
//     Who the user is comes from the end-to-end handshake itself - Noise
//     message 1 travels in the OPEN - and not from a signed task (measured in
//     apps/server-controller, classes/access; plan 3, task 11).
//
// WHAT IS NOT REACHABLE THIS WAY, and it now has a name: an app that wants root,
// or a user whose policy in the trust list wants this step confirmed. The
// controller answers that with E_CONFIRM, and THEN the shell has to ask the
// person and send a signed task (contracts/task.js and shell/confirm.js are
// both there; nothing wires them to an app's own service yet). That is its own
// task, and it needs a surface somebody designed - and the code does not even
// arrive here yet, which the task report hands on as a find of its own.
//
// THE ORDER OF THE FOUR REFUSALS is the order in which each one can be decided
// at all, and each one keeps its own word AND its own sentence:
//   node            the caller has to name it; nothing can be asked without it
//   session-key     this browser's own, and no lookup needed
//   trust-list      THIS ADAPTER was built without a trust store at all, so
//                   there is nobody to ask - a shell that handed one over wrong
//   controller-key  the store's answer ABOUT that node, so it comes last. A
//                   store with no version in force answers it too (NotTrusted),
//                   which is right: it knows nothing about this node either way
// A fifth outcome is no refusal of this adapter at all: past the last check
// what can go wrong is the channel, and a ChannelError carries no `missing`.
//
// AND NOTHING ELSE IS TURNED INTO A SENTENCE. The one `catch` here takes
// NotTrusted and rethrows everything else - the rule contracts/noise.js keeps
// in importPublic, "any other failure is real and must reach the caller, not be
// replaced". A programming fault dressed up as "this server is not in your
// trust list" would send the one person who could fix it looking in the wrong
// place, and it is the same mistake as letting one refusal hide behind another.
async function appChannel(link, world, app, service, name, node, params) {
  if (!validAppID(app) || !validName(service) || !validName(name)) {
    throw new WorldError(NO_ACTION());
  }
  // The node is held to the contract's own rule for a node id (trust.validNode
  // uses text.validName), so a bridge that stopped sending it is refused here
  // and not by the controller three round trips later.
  if (!validName(node)) throw new WorldError(NO_NODE(), "node");
  const sessionDH = world.sessionDH ?? null;
  // The private half is a non-extractable CryptoKey and stays one: it is
  // handed to noise.HandshakeKK and used, never read. What is checked here is
  // only that both halves are THERE - a pair with one half missing would fail
  // somewhere inside the handshake, where nobody can tell a person why.
  if (sessionDH === null || !sessionDH.privateKey
    || !(sessionDH.publicRaw instanceof Uint8Array)) {
    throw new WorldError(NO_SESSION_KEY(), "session-key");
  }
  // Asked BEFORE the lookup, because "nobody handed this adapter a trust store"
  // and "the store does not carry this node" are two different things to say to
  // a person - and the first one is not about the server at all.
  if (typeof world.trust?.controllerKey !== "function") {
    throw new WorldError(NO_TRUST_LIST(), "trust-list");
  }
  let controllerKey;
  try {
    controllerKey = world.trust.controllerKey(node);
  } catch (e) {
    // shell/trustlist.js throws NotTrusted for a node the verified list does
    // not carry - no controller, no key, no list in force. That is the one
    // failure with a sentence; anything else is this shell's own fault and
    // travels on, where a trace names it.
    if (e?.name !== "NotTrusted") throw e;
    throw new WorldError(NO_CONTROLLER_KEY(), "controller-key");
  }
  return openE2E(link, {
    app, service, action: name, params, taskBytes: null, node, controllerKey, sessionDH,
  });
}

// appStream hands the app frame the channel of an app's own service in the
// shape it reads: two listeners that are PROPERTIES, write, end and close.
// It is the shape shell/fake-link.js hands over for the same thing
// (terminalStream), so the frame changes no line for the real way.
//
// DATA GOES STRAIGHT THROUGH, and the first round of this task had a buffer
// here that held it back for one turn of the event loop. It is gone, and the
// reason it is gone is worth more than the code was: the window it covered
// cannot occur.
//
//   - NOTHING OF THIS CHANNEL HAS ARRIVED YET when this function runs. Anything
//     a controller says is the answer to the inner message channel.openE2E sends
//     as its LAST step - that is the contract's rule, "the controller starts the
//     service only once it has decrypted" - so the earliest word of it is a
//     frame of a later task.
//   - A frame is a task; the caller assigns stream.onData and stream.onClose in
//     the microtask right after open() returned. No task runs in between.
//   - MEASURED, both ways through that: a service that SPEAKS FIRST - a shell
//     printing its prompt, with the app having written nothing - reaches the
//     app, and one that is OVER the moment it starts reaches it too, through
//     onClose (world.spec.js, "a service that speaks first", "a service that is
//     over before it starts").
//   - READ in net/channel.js and not measured: if an ending did somehow arrive
//     while openE2E was still in its last write, that write would throw -
//     Channel.sendData refuses a channel that is CLOSED or whose CLOSE is out -
//     so open() would throw and hand no stream over at all.
//
// A buffer for a window nobody can reach is a buffer no case can make fall, and
// this file has two of those behind it already.
//
// THE END STILL WAITS FOR THE NEXT TURN, as the FlowOut door below does. That
// one is belt and braces and is named as such: by the paragraph above, done()
// cannot be settled yet when this function runs, so the wait changes nothing
// today - measured, the cases stay green without it. It stays because this
// function is what OWES the caller the one close it gets, and a promise that
// depends on its caller never putting an await between open() and its listeners
// is a promise that breaks the day somebody does.
//
// THE END COMES FROM done() AND NOT FROM onClose, on purpose. Channel.onClose
// fires only where the channel ENDED - the controller's own end - and says
// nothing where it was cut off or broke a rule (Channel.giveUp). done() answers
// both, so the app is told either way; that is what shell/appframe.js needs to
// stop waiting on a number that is gone.
//
// WHAT THIS CHANNEL KEEPS, named here because this is the door that opens it:
// NOTHING, as long as somebody is listening - and this function is listening
// from the moment it runs. Channel.take puts a piece into Channel.chunks only
// where there is no data handler, because chunks exists for result() and
// result() is the only thing that ever reads it; on a stream it never comes.
//
// IT WAS NOT ALWAYS SO, and the trail belongs in the file that found it. The
// first round of this task measured the opposite on a real end-to-end channel
// to the test controller - after five lines the channel held all five - and
// said that the cure belonged in net/channel.js and not here, because a bound
// invented in this file would be a second opinion about a number the contract
// does not give. That is where it was cured (611ad2f, "Stop keeping what a
// listener has already been handed"), and that session measured it: five pieces
// held before, none after. NO NUMBER WAS INVENTED for it, here or there: a
// stream is unbounded by nature, and a bound over its whole life would cut off
// exactly the session the stream is for.
//
// WHAT IS STILL TRUE OF A STREAM, so nobody reads a bound into this: its limit
// stays Infinity, because only result() ever sets one, and Channel.total still
// counts every byte that came. In flight the credit window is what bounds it.
function appStream(ch) {
  let gone = false, listening = false, over = false;
  let ending = null;
  const s = {
    onData: null,
    onClose: null,
    // The channel's own bound decides how much may be in flight; past its
    // credit write waits for the next GRANT rather than overspending.
    write(bytes) {
      return ch.write(bytes);
    },
    // end is "nothing more from me": the inner end first, then the channel's
    // EOF. What the service still has to say comes after it.
    end() {
      return ch.end();
    },
    // close(code) REALLY ENDS THIS CHANNEL, and with that code - the same
    // promise open() keeps for a FlowOut channel below. From here on the caller
    // hears nothing more: it asked to forget this number, and
    // shell/appframe.js has already told the app itself (doCloseStream).
    close(code) {
      gone = true;
      return ch.close(typeof code === "string" && code !== "" ? code : CLOSE_OK);
    },
  };
  ch.onData((b) => {
    if (gone || over) return;
    s.onData?.(b);
  });
  const finish = () => {
    if (gone || over || !listening || ending === null) return;
    // Once: the frame forgets the number on this call, so a second one would
    // reach nobody anyway.
    over = true;
    s.onClose?.(ending.status, ending.message);
  };
  ch.done().then(
    (st) => {
      // The status and the message are the channel's own and go on untouched:
      // what a service's ending means is the app's contract with it.
      ending = { status: st.status, message: typeof st.message === "string" ? st.message : "" };
      finish();
    },
    (e) => {
      // A channel that broke. The frame branches on "ok" and on nothing else,
      // and the text is the one net/channel.js got from wire for that code.
      ending = { status: "error", message: typeof e?.message === "string" ? e.message : "" };
      finish();
    },
  );
  setTimeout(() => {
    listening = true;
    finish();
  }, 0);
  return s;
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
//     and for an app's own service, write and end as well - the shape
//     shell/fake-link.js's terminalStream has, because that is what a channel
//     somebody writes into needs
//   onNotice(fn)
//
// BOTH DOORS TAKE FIVE ARGUMENTS, and open() did not until Fixrunde 1: Task 25
// gave shell/appframe.js the node of an app and it hands it over in the option
// bag of both. A door that took four of five arguments promised more than it
// did, and JavaScript says nothing about it.
//
// THE SECOND ARGUMENT IS THE WORLD AROUND THE LINK, and only an app's own
// service needs it: {sessionDH, trust}. An adapter built without it still
// answers every action of NGC's own - those go to the API over the session that
// is already open - and refuses an app's own service with the word for the
// piece it has not got. realWorld below is what hands both over.
export function adapterFor(link, world = {}) {
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
      if (a === null) {
        // An app's own service, asked ONE question. The question stands in the
        // params of the OPEN, so nothing is left to send: the inner end and the
        // EOF go out at once and the answer is bounded the way every other
        // answer of a call is (wire.MAX_CALL_RESULT). On an end-to-end channel
        // that bound is the receiver's alone - the API never sees the
        // plaintext - which is why it may not be left to the other side.
        const ch = await appChannel(link, world, app, service, name, o.node, p);
        await ch.end();
        return ch.result(wire.MAX_CALL_RESULT);
      }
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
            params: p, taskBytes, confirmBytes, stream: o.stream,
            onProgress: o.onProgress,
          });
        // FlowOut is a channel that stays open and belongs in open(); taking
        // it here would mean bounding an answer in the one place that cannot
        // know what the caller will do with it. A flow that is neither of the
        // four is one the contract grew after this file, and an action whose
        // way nobody here knows is an action nobody here sends.
        default:
          throw new WorldError(WRONG_WAY());
      }
    },

    // open takes the FlowOut action - or an app's own service - and answers
    // with the shape the app frame reads: two listeners that are PROPERTIES -
    // the frame assigns stream.onData and stream.onClose after this call came
    // back - and a close(code).
    //
    // NOTHING IS HANDED OVER BEFORE THE CHANNEL STANDS, on both ways through
    // here. open() waits for channel.openOut: the OPEN is out, its first grant
    // is in, the EOF has gone out. For an app's own service it waits for
    // channel.openE2E, which is more: the OPEN with Noise message 1 is out,
    // message 2 has come back in the first grant and the transport is split. A
    // channel that cannot be opened therefore THROWS from here, where the frame
    // catches it, instead of being reported later through onClose on a stream
    // that never was one. (public/assets/ngc.js cannot do that - a postMessage
    // bridge has nothing to wait for and hands the app its stream at once, and
    // onClose is what tells it afterwards. Here there is something to wait for,
    // so we wait.)
    //
    // THE ANSWER IS HANDED OVER IN ONE PIECE, after the caller has had the
    // chance to hang his listeners up, which is why the delivery waits for the
    // next turn of the event loop. The channel collects it either way
    // (Channel.result holds the pieces), and the bound is the channel's: past
    // MAX_OUT it closes with E_TOOLARGE instead of reading on.
    //
    // AN APP'S OWN SERVICE IS THE OTHER WAY THROUGH HERE, and its answer is NOT
    // one piece: it is a channel the app writes into and reads from for as long
    // as it is open - a terminal - so appStream above hands each piece on as it
    // arrives, once the caller has had its turn to hang its listeners up. That
    // stream carries write and end as well, which this one does not need.
    async open(app, service, name, params, opts) {
      const a = rowFor(app, service, name);
      const p = own(params, "params");
      if (a === null) {
        return appStream(await appChannel(link, world, app, service, name, (opts ?? {}).node, p));
      }
      if (a.flow !== FLOW_OUT) throw new WorldError(WRONG_WAY());
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
//
// sessionDH IS TAKEN AND HANDED STRAIGHT ON, to the adapter and nowhere else.
// It is the X25519 pair auth/login.js built for this session, whose public half
// stands in the client certificate the API bound the session to, and every
// end-to-end channel to a controller is made with it. It does NOT travel into
// the `session` object that startDesktop gets: the fewer places a key is, the
// fewer places can hand it on, and no surface of the desktop has anything to do
// with it. The private half is non-extractable (auth/keys.js), so what is being
// kept narrow here is the right to USE it.
export async function realWorld({ config, anchor, session, link, sessionDH } = {}) {
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
  return {
    link: adapterFor(link, { trust, sessionDH: sessionDH ?? null }),
    trust, session: session ?? null, releaseKey: null,
  };
}

// One channel of a session link: its number, the credit in both directions,
// its state, and on an end-to-end channel the Noise transport inside it
// (Go: wire, "Channels", and shared/README.md, "Öffnen, Schließen, Freigabe").
//
// Every rule here is a rule the other side enforces too, and breaking one
// ends the channel or the link. The shell therefore keeps them itself rather
// than finding out from a CLOSE: it never writes past its credit, never opens
// past MaxSessionChannels, and never takes a result bigger than
// MaxCallResult.
//
// THE ONE READING RULE. A caller's bytes are read ONCE, at the door, and only
// that reading is used afterwards - bytesOf where the bytes are kept and
// chopped up (write, writeE2E), viewOf where they are only looked at. Without
// it a payload whose length answers 0 for the credit check and 65515 for the
// copy leaves as a frame nobody checked; with 9 MiB of DATA on one channel
// that reading happens once per chunk and every chunk is a chunk of OUR copy.
// What arrives is never the caller's: decodeFrame hands back a view into the
// buffer this module decrypted, and the plaintext of an end-to-end message is
// what the Web Crypto API returned.
import * as wire from "../contracts/wire.js";
import * as core from "../contracts/core.js";
import { bytesOf, concat, viewOf } from "../contracts/b64.js";

// The states a channel walks through. "opening" is between the OPEN and the
// first GRANT, when nothing may be sent yet; "closed" is final.
const OPENING = "opening", OPEN = "open", SENT_EOF = "sentEOF", CLOSED = "closed";

// The status word of a CLOSE that ended well. close("ok") is how a caller
// says "I am done with this channel" without naming an error; every other
// argument is a code of wire.CODES.
const OK = "ok";

// The Noise tag an end-to-end message costs on top of its plaintext. It is
// taken from the contract rather than written as 16 here: wire says
// MAX_E2E_PLAINTEXT is MAX_PAYLOAD minus the tag, and one number in the tree
// is one number that cannot drift.
const TAG_SIZE = wire.MAX_PAYLOAD - wire.MAX_E2E_PLAINTEXT;

// ChannelError carries the code a caller acts on. The text is wire's German
// one, so the shell has a sentence to show and never a second wording of its
// own; the code is what the tests and the callers branch on.
export class ChannelError extends Error {
  constructor(code) {
    super(wire.message(code) || "Ungültige Nachricht.");
    this.name = "ChannelError";
    this.code = code;
  }
}

const fail = (code) => new ChannelError(code);

export class Channel {
  // A channel is built by Link.open and by nobody else: the number and the
  // window come from the link, which is the one place that can hold
  // MaxSessionChannels.
  constructor(link, number, { window, e2e }) {
    this.link = link;
    this.number = number;
    this.state = OPENING;
    // Credit the other side has given us for OUR data. Nothing until the
    // first GRANT - the OPEN's own window is the credit the other side gets
    // from us, never the other way round.
    this.sendLeft = 0;
    // Credit we have given the other side: the window of our OPEN, topped
    // back up to it once half of it is used. recvLeft is what is still open,
    // and it never exceeds recvWindow, which never exceeds MAX_WINDOW.
    this.recvWindow = window;
    this.recvLeft = window;
    // An end-to-end channel carries Noise message 1 in its OPEN; the
    // transport arrives with useTransport once message 2 has been read.
    this.e2e = e2e !== null && e2e !== undefined;
    this.transport = null;
    this.sentEnd = false;     // our own wire.E2E_END went out
    this.gotEnd = false;      // the controller's end arrived
    this.endStatus = null;    // the CLOSE payload of that end
    this.sentEOF = false;
    this.gotEOF = false;
    this.sentClose = false;
    this.gotClose = null;     // the parsed CLOSE of the other side
    this.first = null;        // the first GRANT, {credit, handshake}
    this.firstWaiters = [];
    this.creditWaiters = [];
    this.doneWaiters = [];
    this.chunks = [];         // what result() will hand back
    this.total = 0;
    this.limit = Infinity;    // set by result(); over it the channel dies
    this.overLimit = false;
    this.handlers = { data: null, close: null };
    this.error = null;        // the ChannelError every waiter gets
  }

  // ---- what the caller hangs up -------------------------------------------

  onData(fn) {
    if (typeof fn === "function") this.handlers.data = fn;
  }

  onClose(fn) {
    if (typeof fn === "function") this.handlers.close = fn;
  }

  // useTransport hands the channel the Noise transport of its end-to-end
  // handshake. From here on every DATA is one transport message and the API
  // sees ciphertext only.
  useTransport(t) {
    this.transport = t;
  }

  // ---- waiting -------------------------------------------------------------

  // firstGrant answers with the first GRANT of the side that accepted the
  // channel: the credit, and on an end-to-end channel Noise message 2. A
  // CLOSE with an error instead of it is the other legal answer, and it
  // arrives as a throw.
  firstGrant() {
    if (this.first !== null) return Promise.resolve(this.first);
    if (this.error !== null) return Promise.reject(this.error);
    return new Promise((ok, no) => this.firstWaiters.push({ ok, no }));
  }

  // done answers with the CLOSE that ended the channel - on an end-to-end
  // channel with the controller's own end, which is the status that counts
  // there (shared/README.md, "Ergebnis nur mit Erfolg").
  done() {
    const now = this.finished();
    if (now !== null) return Promise.resolve(now);
    if (this.error !== null) return Promise.reject(this.error);
    return new Promise((ok, no) => this.doneWaiters.push({ ok, no }));
  }

  // finished answers with the status of a channel that has ended, or null
  // while it has not. On an end-to-end channel the controller's end decides
  // and the CLOSE of the channel is only the transport going away.
  finished() {
    if (this.e2e) return this.gotEnd ? this.endStatus : null;
    return this.gotClose;
  }

  // result collects the answer of a call-like channel: every DATA until the
  // other side is done, and only then, and only if it ended with ok
  // (shared/README.md, "Ergebnis nur mit Erfolg"). maxBytes is the caller's
  // bound; past it the channel is closed with E_TOOLARGE, because a receiver
  // that just stopped reading would leave the other side writing.
  async result(maxBytes) {
    this.limit = Number.isSafeInteger(maxBytes) && maxBytes >= 0 ? maxBytes : 0;
    // The bound may already be broken by what arrived before result() was
    // called - the answer can be on its way while the caller is still setting
    // up - so it is checked once here and on every chunk after.
    this.checkLimit();
    const status = await this.done();
    if (status.status !== OK) throw fail(status.code);
    // EOF before the end is data that was cut off. On an end-to-end channel
    // the controller's end IS the end, so there is nothing to ask.
    if (!this.e2e && !this.gotEOF) throw fail(wire.CODES.PROTOCOL);
    return concat(...this.chunks);
  }

  // ---- writing -------------------------------------------------------------

  // write sends bytes as DATA, in pieces of at most one frame, and never more
  // than the credit allows. On an end-to-end channel every piece becomes one
  // inner message of kind E2E_DATA.
  async write(bytes) {
    // ONE reading, and a copy: the bytes are chopped up across awaits below,
    // and a caller who swaps them in between would have us check one document
    // and send another.
    const b = bytesOf(bytes, "channel: not bytes");
    if (this.e2e) {
      // The plaintext of an inner message is the kind byte and the body, and
      // the ciphertext is 16 bytes more; MAX_E2E_PLAINTEXT already counts the
      // kind byte, so the body is one less.
      const max = wire.MAX_E2E_PLAINTEXT - 1;
      for (let i = 0; i < b.length; i += max) {
        await this.writeE2E(wire.E2E_DATA, b.subarray(i, Math.min(i + max, b.length)));
      }
      return;
    }
    for (let i = 0; i < b.length;) {
      const room = await this.credit();
      const n = Math.min(b.length - i, wire.MAX_PAYLOAD, room);
      await this.sendData(b.subarray(i, i + n));
      i += n;
    }
  }

  // writeE2E sends one inner message of an end-to-end channel. The credit
  // counts the ciphertext, which is 16 bytes more than the plaintext
  // (shared/README.md, "Freigabe": "samt 16 Byte Noise-Prüfwert").
  async writeE2E(kind, body) {
    if (this.transport === null) throw fail(wire.CODES.PROTOCOL);
    // After our own end nothing more may go out on an end-to-end channel.
    if (this.sentEnd) throw fail(wire.CODES.PROTOCOL);
    // wire.e2e reads the body itself, through its own bytesOrNull, and
    // decides the kind and the length on that reading.
    const plain = wire.e2e(kind, body ?? new Uint8Array(0));
    const need = plain.length + TAG_SIZE;
    for (;;) {
      const room = await this.credit();
      if (room >= need) break;
      // Not enough credit for one whole message: wait for the next GRANT.
      // A window under MinWindow cannot happen - parseGrant refuses 0 and the
      // OPEN's window is at least MinWindow - so this cannot spin forever.
      await this.waitCredit();
    }
    const sealed = await this.transport.seal(plain);
    if (kind === wire.E2E_END) this.sentEnd = true;
    await this.sendData(sealed);
  }

  // sendData puts one DATA frame on the link and books the credit. It is the
  // ONE place that spends credit, so the number that was checked is the
  // number that is spent - and it is checked again HERE and not only where
  // the piece was cut: two writers on one channel may both have read the
  // same credit before either of them spent it, and the second one has to
  // find it gone. It fails rather than overspending, which is the side that
  // costs a channel instead of the link.
  async sendData(payload) {
    if (this.state === CLOSED || this.sentClose) throw fail(wire.CODES.PROTOCOL);
    const v = viewOf(payload, "channel: not bytes");
    if (v.length > this.sendLeft) throw fail(wire.CODES.PROTOCOL);
    this.sendLeft -= v.length;
    await this.link.send({ type: wire.TYPE_DATA, channel: this.number, payload: v });
  }

  // credit answers with the credit in hand, waiting for the next GRANT while
  // there is none.
  async credit() {
    while (this.sendLeft === 0) await this.waitCredit();
    return this.sendLeft;
  }

  waitCredit() {
    if (this.error !== null) return Promise.reject(this.error);
    if (this.state === CLOSED) return Promise.reject(fail(wire.CODES.UNREACHABLE));
    return new Promise((ok, no) => this.creditWaiters.push({ ok, no }));
  }

  // end says "nothing more from me". On an end-to-end channel that is the
  // inner end first - the transport's EOF only ends the transport - and then
  // the channel's own EOF.
  async end() {
    if (this.e2e && !this.sentEnd) await this.writeE2E(wire.E2E_END, null);
    if (this.sentEOF || this.sentClose || this.state === CLOSED) return;
    this.sentEOF = true;
    this.state = SENT_EOF;
    await this.link.send({ type: wire.TYPE_EOF, channel: this.number, payload: new Uint8Array(0) });
  }

  // close ends the channel from this side. "ok" is a normal end, everything
  // else is a code of wire.CODES. After it nothing more goes out on this
  // number, and the number stays taken until the other side's CLOSE has
  // arrived - that is what channelCount counts.
  async close(code) {
    if (this.sentClose || this.state === CLOSED) return;
    this.sentClose = true;
    const payload = code === OK
      ? wire.encodeClose({ status: OK, code: "", message: "", exitCode: null })
      : wire.encodeClose(wire.errorClose(code));
    this.link.handOver(this);
    try {
      await this.link.send({ type: wire.TYPE_CLOSE, channel: this.number, payload });
    } catch {
      // The link may already be gone. The channel is closed either way, and
      // there is nobody left to tell.
    }
    // After our own CLOSE nothing more arrives for this number (the link
    // drops it), so whoever still waits here waits forever unless he is told
    // now. A caller who closed the channel himself and still wanted its
    // result cut it off, which is a protocol error of his own making.
    if (this.gotClose === null) this.giveUp(fail(code === OK ? wire.CODES.PROTOCOL : code));
  }

  // ---- what arrives --------------------------------------------------------

  // receive takes one frame for this channel. Everything that breaks a rule
  // ends the channel with E_PROTOCOL rather than the link: a channel is the
  // unit the contract closes for a protocol error on a channel.
  async receive(frame) {
    try {
      switch (frame.type) {
        case wire.TYPE_GRANT: return await this.onGrant(frame.payload);
        case wire.TYPE_DATA: return await this.onDataFrame(frame.payload);
        case wire.TYPE_EOF: return this.onEOF();
        case wire.TYPE_CLOSE: return await this.onCloseFrame(frame.payload);
        // An OPEN for a channel the shell opened is the other side opening a
        // number it may not open at all.
        default: return await this.protocol();
      }
    } catch {
      return this.protocol();
    }
  }

  async protocol() {
    const e = fail(wire.CODES.PROTOCOL);
    await this.close(wire.CODES.PROTOCOL);
    this.giveUp(e);
  }

  async onGrant(payload) {
    const g = wire.parseGrant(payload);
    if (this.first === null) {
      // The handshake of an end-to-end channel travels in THIS grant and in
      // no other, so it is kept here and a handshake in any LATER grant is
      // refused below. The other half of that rule - a first grant WITHOUT a
      // handshake on a channel that asked for one - is openE2E's, which is
      // the only place that knows a handshake was asked for.
      this.first = g;
      this.state = OPEN;
      const waiting = this.firstWaiters;
      this.firstWaiters = [];
      this.sendLeft += g.credit;
      for (const w of waiting) w.ok(g);
    } else {
      if (g.handshake !== null) return this.protocol();
      // More credit than MaxWindow may be open at once on no channel.
      if (this.sendLeft + g.credit > wire.MAX_WINDOW) return this.protocol();
      this.sendLeft += g.credit;
    }
    this.wakeCredit();
    return undefined;
  }

  wakeCredit() {
    const waiting = this.creditWaiters;
    this.creditWaiters = [];
    for (const w of waiting) w.ok(this.sendLeft);
  }

  // onDataFrame and not onData: onData is the listener a caller hangs up, and
  // a class cannot have both under one name - the later definition wins in
  // silence. Measured: with both called onData, ch.onData(fn) registered
  // nothing and ran the frame handler over the FUNCTION, which decrypted to
  // nothing and closed the channel with E_PROTOCOL on the next write.
  async onDataFrame(payload) {
    // More DATA than we granted is a protocol error, and the check and the
    // booking read the same one number.
    const n = payload.length;
    if (n > this.recvLeft) return this.protocol();
    this.recvLeft -= n;
    // Top the window back up once half of it is used, never past MaxWindow.
    const used = this.recvWindow - this.recvLeft;
    if (used >= this.recvWindow >> 1) {
      this.recvLeft = this.recvWindow;
      await this.link.send({
        type: wire.TYPE_GRANT, channel: this.number, payload: wire.grant(used, null),
      });
    }
    if (!this.e2e) return this.take(payload);
    if (this.transport === null) return this.protocol();
    // The plaintext is what the Web Crypto API hands back, so it is ours, and
    // parseE2E is the one thing that reads it.
    let inner;
    try {
      inner = wire.parseE2E(await this.transport.open(payload), true);
    } catch {
      return this.protocol();
    }
    if (this.gotEnd) return this.protocol();
    if (inner.kind === wire.E2E_DATA) return this.take(inner.data);
    this.gotEnd = true;
    this.endStatus = inner.status;
    return this.settle();
  }

  // take books one piece of the answer and hands it to whoever listens. The
  // bound is checked on the piece that was just counted, not on the next one,
  // and a piece that broke it reaches nobody: past the bound the channel is
  // over, and handing its data on would be handing on the very bytes the
  // bound refused.
  take(p) {
    this.total += p.length;
    this.chunks.push(p);
    this.checkLimit();
    const fn = this.handlers.data;
    if (fn !== null && !this.overLimit) fn(p);
  }

  checkLimit() {
    if (this.overLimit || this.total <= this.limit) return;
    this.overLimit = true;
    // The receiver enforces the bound: the shell for every answer of the API
    // and on every end-to-end channel, whose plaintext the API never sees.
    const e = fail(wire.CODES.TOOLARGE);
    this.close(wire.CODES.TOOLARGE).catch(() => {});
    this.giveUp(e);
  }

  onEOF() {
    if (this.gotEOF) return this.protocol();
    this.gotEOF = true;
    // On an end-to-end channel EOF only ends the transport. Before the
    // controller's own end it means the data was cut off, and cut-off data is
    // an error, never a short answer (shared/README.md, "Abgeschnitten").
    if (this.e2e && !this.gotEnd) return this.truncated();
    return this.settle();
  }

  // truncated is the end of a channel whose answer stopped in the middle.
  // Nothing of it counts, so every waiter gets the error and nobody gets the
  // bytes that did arrive.
  truncated() {
    const e = fail(wire.CODES.PROTOCOL);
    this.close(wire.CODES.PROTOCOL).catch(() => {});
    this.giveUp(e);
    return undefined;
  }

  async onCloseFrame(payload) {
    const c = wire.parseClose(payload);
    this.gotClose = c;
    // Every CLOSE is answered with a CLOSE, unless one has already gone out.
    if (!this.sentClose) {
      this.sentClose = true;
      this.link.handOver(this);
      try {
        await this.link.send({ type: wire.TYPE_CLOSE, channel: this.number, payload });
      } catch {
        // The link is gone; the channel ends here either way.
      }
    }
    this.link.free(this.number);
    this.state = CLOSED;
    // A CLOSE before the controller's end cuts an end-to-end channel off the
    // same way an early EOF does.
    if (this.e2e && !this.gotEnd) return this.truncated();
    return this.settle();
  }

  // settle wakes everyone the channel owes an answer to, once it is clear
  // what that answer is.
  settle() {
    const status = this.finished();
    if (status === null) return undefined;
    const fn = this.handlers.close;
    if (fn !== null) fn(status);
    const waiting = this.doneWaiters;
    this.doneWaiters = [];
    for (const w of waiting) w.ok(status);
    // A channel that ended before its first grant owes that answer too.
    if (this.first === null) {
      const early = this.firstWaiters;
      this.firstWaiters = [];
      const e = status.status === OK ? fail(wire.CODES.PROTOCOL) : fail(status.code);
      for (const w of early) w.no(e);
      this.error = this.error ?? e;
    }
    this.wakeCredit();
    return undefined;
  }

  // giveUp ends every wait with one error. It is the way out for a protocol
  // error, a result past its bound and a link that went away.
  giveUp(e) {
    if (this.state !== CLOSED) this.state = CLOSED;
    this.error = this.error ?? e;
    for (const list of [this.firstWaiters, this.doneWaiters, this.creditWaiters]) {
      const waiting = list.splice(0, list.length);
      for (const w of waiting) w.no(this.error);
    }
  }
}

// ---------------------------------------------------------------- the helpers

// call opens a channel of flow call, in or transfer, sends its params in the
// OPEN (or in the task), sends EOF at once and gives back the parsed result.
export async function call(link, action, opts = {}) {
  return core.parseResult(action, await callBytes(link, action, opts));
}

// callBytes is the same channel without the last step: it answers with the
// RESULT BYTES and leaves the reading to the caller.
//
// WHY THE TWO EXIST, because one of them doing both would be smaller: the
// surfaces read an answer themselves (apps/store.js, apps/users.js,
// shell/desktop.js and shell/appframe.js all call core.parseResult over what
// they were handed), and a result that arrived already parsed would be read a
// second time or not at all. So net/world.js takes the bytes and the parsing
// stays where the contract for that answer is used. There is ONE
// implementation of the channel either way, which is the point: a second copy
// of "open, wait for the grant, EOF, result" would be a second place for the
// bound and the EOF to drift.
export async function callBytes(link, action, { params, taskBytes, confirmBytes } = {}) {
  const ch = await link.open({
    app: action.app, service: action.service, action: action.name,
    params: taskBytes ? null : params, window: wire.DEFAULT_WINDOW,
    e2e: null, taskBytes, confirmBytes,
  });
  await ch.firstGrant();
  // On call and on a transfer the shell opened (activate) it sends EOF at
  // once and no DATA; the API writes header and file on the tunnel hop.
  await ch.end();
  return ch.result(wire.MAX_CALL_RESULT);
}

// sendIn opens a FlowIn channel and streams at most action.maxIn bytes.
export async function sendIn(link, action, opts = {}) {
  return core.parseResult(action, await sendInBytes(link, action, opts));
}

// sendInBytes is sendIn without the last step, for the same reason callBytes
// exists.
export async function sendInBytes(link, action, { params, taskBytes, stream, onProgress } = {}) {
  const ch = await link.open({
    app: action.app, service: action.service, action: action.name,
    params: taskBytes ? null : params, window: wire.DEFAULT_WINDOW, e2e: null, taskBytes,
  });
  await ch.firstGrant();
  let sent = 0;
  const reader = stream.getReader();
  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    // The reader's chunk is read ONCE here, and that reading is what is
    // counted against maxIn and what is written: a chunk whose length
    // answered small for the count and large for the write would carry the
    // stream past the bound the API closes the channel over.
    const piece = bytesOf(value, "channel: not bytes");
    sent += piece.length;
    // The limit is the contract's, not the server's answer: more would be a
    // channel the API closes with E_TOOLARGE anyway.
    if (sent > action.maxIn) {
      await ch.close(wire.CODES.TOOLARGE);
      throw fail(wire.CODES.TOOLARGE);
    }
    await ch.write(piece);
    onProgress?.(sent);
  }
  await ch.end();
  return ch.result(wire.MAX_CALL_RESULT);
}

// receiveOut opens a FlowOut channel and collects what the API sends; the
// caller says how many bytes it will take at most (for ui: the largest
// ui-tar, store.MaxUITotal plus its headers).
//
// net/world.js does NOT use this one: it takes the channel from openOut below,
// because the stream it hands the app frame has to be able to close that
// channel. This stays for a caller who only wants the bytes.
export async function receiveOut(link, action, { params, maxBytes }) {
  return (await openOut(link, action, { params })).result(maxBytes);
}

// openOut is the first half of receiveOut and answers with the CHANNEL: it is
// open, its first grant is in and the EOF has gone out, so nothing is left to
// send on it.
//
// WHY A CALLER WOULD WANT THE CHANNEL AND NOT ONLY THE BYTES: net/world.js
// hands the app frame a stream whose close(code) has to END this channel, with
// that code - the frame calls it when it gives an archive up, and a close()
// that quietly did nothing would leave the number taken for the life of the
// session. Splitting it here keeps ONE implementation of "open, wait for the
// grant, EOF": a second copy of that would be a second place for the EOF to
// drift.
export async function openOut(link, action, { params } = {}) {
  const ch = await link.open({
    app: action.app, service: action.service, action: action.name,
    params, window: wire.DEFAULT_WINDOW, e2e: null,
  });
  await ch.firstGrant();
  await ch.end();
  return ch;
}

// openE2E opens a channel to an app: Noise message 1 goes into the OPEN, and
// the prologue binds controller, app, service, action and the exact params and
// task bytes of that OPEN.
export async function openE2E(link, { app, service, action, params, taskBytes, node, controllerKey, sessionDH }) {
  // Read once, here: the prologue hashes these bytes and encodeOpen writes
  // them, and a second reading could hash one document and send another -
  // then the controller's prologue and ours differ and nothing decrypts, or
  // worse, they agree on bytes nobody checked.
  const paramsBytes = params === null || params === undefined
    ? new Uint8Array(0) : bytesOf(params, "channel: params are not bytes");
  const taskBytesOrEmpty = taskBytes === null || taskBytes === undefined
    ? new Uint8Array(0) : bytesOf(taskBytes, "channel: task is not bytes");
  const prologue = await wireE2EPrologue(node, app, service, action, paramsBytes, taskBytesOrEmpty);
  const hs = await noiseHandshake(prologue, sessionDH, controllerKey);
  const msg1 = await hs.writeMessage1();
  const ch = await link.open({
    app, service, action, params: taskBytesOrEmpty.length > 0 ? null : paramsBytes,
    window: wire.DEFAULT_WINDOW, e2e: msg1,
    taskBytes: taskBytesOrEmpty.length > 0 ? taskBytesOrEmpty : null,
  });
  // Message 2 travels in the first GRANT of the side that accepted the
  // channel; that GRANT without it, or a handshake in any other GRANT, is a
  // protocol error.
  const first = await ch.firstGrant();
  if (first.handshake === null) {
    await ch.close(wire.CODES.PROTOCOL);
    throw fail(wire.CODES.PROTOCOL);
  }
  await hs.readMessage2(first.handshake);
  ch.useTransport(await hs.split());
  // Right after message 2 the shell sends its first inner message; the
  // controller starts the service only once it has decrypted, so a replayed
  // OPEN does nothing on its own.
  await ch.writeE2E(wire.E2E_DATA, new Uint8Array(0));
  return ch;
}

// The two calls into noise are behind these two lines so that this module
// imports noise lazily: everything above works without it, and a page that
// never opens an end-to-end channel never loads the handshake.
async function wireE2EPrologue(node, app, service, action, params, task) {
  const noise = await import("../contracts/noise.js");
  return noise.e2ePrologue(node, app, service, action, params, task);
}

async function noiseHandshake(prologue, sessionDH, controllerKey) {
  const noise = await import("../contracts/noise.js");
  return noise.HandshakeKK.of(true, prologue, sessionDH.privateKey, sessionDH.publicRaw, controllerKey);
}

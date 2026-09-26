// The session link: one WebSocket to the API, one Noise session inside it,
// and the channels on top (Go: wire, "Links" and "Channels").
//
// THE NONCE IS WHY EVERYTHING HERE IS IN A QUEUE. A Noise cipher state reads
// its counter to build the IV and raises it only after the Web Crypto call
// has come back, so two seals or two opens that overlap use ONE nonce for two
// messages - the same key and IV over two different plaintexts, which is the
// one failure AES-GCM does not survive. Measured against contracts/noise.js:
// CipherState.seal builds nonce(this.n) before its await and does this.n++
// after it, and open does the same. So this file sends through one chain
// (sendQueue) and reads through one loop (pump), and no two calls into a
// transport of this link ever overlap. The task brief's own sketch awaited
// transport.open straight out of ws.onmessage, where two frames arriving in
// one turn of the event loop do exactly that.
//
// WHAT THIS MODULE DOES NOT DO: it does not decide whom it is talking to.
// connect is handed the session's own DH key pair and the API's DH public
// key, and whoever hands them over is the one who checked them - the login
// against session.verifyAPI and the trust list. A link that verified its own
// peer would be a link that decides who is trusted, and that decision is not
// the transport's.
import * as wire from "../contracts/wire.js";
import * as noise from "../contracts/noise.js";
import * as session from "../contracts/session.js";
import * as core from "../contracts/core.js";
import { utf8 } from "../contracts/b64.js";
import { Channel, ChannelError } from "./channel.js";

// The largest channel number. wire holds it as a uint32 in Go; here the
// search for a free number has to know where to wrap around.
const MAX_NUMBER = 0xffffffff;

export class Link {
  constructor() {
    this.ws = null;
    this.transport = null;
    this.channels = new Map();   // number -> Channel
    this.closing = new Set();    // numbers whose CLOSE was sent but not answered
    this.next = 1;
    this.handlers = { trust: () => {}, notice: () => {}, bye: () => {} };
    // One chain for everything that goes out, so no two seals overlap.
    this.sendQueue = Promise.resolve();
    // One loop for everything that comes in, for the same reason.
    this.inbox = [];
    this.pumping = false;
    this.dead = false;
  }

  // channelCount counts every number that is not free again, closing ones
  // included: that is what MaxSessionChannels bounds.
  channelCount() {
    return this.channels.size + this.closing.size;
  }

  onTrustHint(fn) {
    if (typeof fn === "function") this.handlers.trust = fn;
  }

  onNotice(fn) {
    if (typeof fn === "function") this.handlers.notice = fn;
  }

  onBye(fn) {
    if (typeof fn === "function") this.handlers.bye = fn;
  }

  async connect(url, sessionDH, apiDH) {
    const ws = new WebSocket(url, session.SUBPROTOCOL);
    ws.binaryType = "arraybuffer";
    await new Promise((ok, no) => {
      ws.onopen = ok;
      ws.onerror = () => no(new Error("link: no connection"));
      ws.onclose = () => no(new Error("link: no connection"));
    });
    // The subprotocol is the one thing about the socket itself that says the
    // other end speaks NGC at all. A server that answered without it - or
    // with another one - is not one this shell talks to.
    if (ws.protocol !== session.SUBPROTOCOL) {
      ws.close();
      throw new Error("link: no connection");
    }
    // Messages that arrive between here and the assignment of onmessage
    // below would be lost, so the handshake reads them out of one queue that
    // is hung up before message 1 goes out.
    const handshake = [];
    let wake = null;
    ws.onmessage = (e) => {
      handshake.push(e.data);
      if (wake !== null) { const w = wake; wake = null; w(); }
    };
    const nextBinary = async () => {
      for (;;) {
        if (handshake.length > 0) {
          const d = handshake.shift();
          // A text message ends the link, here as later.
          if (typeof d === "string") throw new Error("link: no connection");
          return new Uint8Array(d);
        }
        await new Promise((ok, no) => {
          wake = ok;
          ws.onclose = () => no(new Error("link: no connection"));
          ws.onerror = () => no(new Error("link: no connection"));
        });
      }
    };
    const hs = await noise.HandshakeKK.of(true, utf8(noise.PROLOGUE_SESSION),
      sessionDH.privateKey, sessionDH.publicRaw, apiDH);
    ws.send(await hs.writeMessage1());
    await hs.readMessage2(await nextBinary());
    this.transport = await hs.split();
    this.ws = ws;
    // Whatever arrived while the handshake ran is a transport message and
    // belongs in the pump, in the order it came.
    this.inbox.push(...handshake);
    ws.onmessage = (e) => this.take(e.data);
    ws.onerror = () => {};
    ws.onclose = () => this.fail(wire.CODES.UNREACHABLE);
    this.pump();
    return this;
  }

  // take queues one message. The pump reads them one at a time, because the
  // receiving cipher state may not be entered twice at once.
  take(data) {
    this.inbox.push(data);
    this.pump();
  }

  async pump() {
    if (this.pumping) return;
    this.pumping = true;
    try {
      while (this.inbox.length > 0 && !this.dead) {
        try {
          await this.onMessage(this.inbox.shift());
        } catch {
          // Nothing the API did: onMessage answers everything the other side
          // can get wrong itself. What reaches here came out of the shell -
          // a listener that threw - and the link ends on the shell's own
          // code, not on E_PROTOCOL, which would name the wrong culprit.
          this.fail(wire.CODES.INTERNAL);
        }
      }
    } finally {
      this.pumping = false;
    }
  }

  async onMessage(data) {
    // A text message, a frame that does not decode or a failed decryption
    // ends the link.
    if (typeof data === "string") return this.fail(wire.CODES.PROTOCOL);
    let frame;
    try {
      frame = wire.decodeFrame(await this.transport.open(new Uint8Array(data)));
    } catch {
      return this.fail(wire.CODES.PROTOCOL);
    }
    if (frame.channel === wire.CONTROL_CHANNEL) return this.onControlFrame(frame);
    const ch = this.channels.get(frame.channel);
    if (!ch) {
      // A frame for a number that is neither open nor closing is a protocol
      // error; one for a number whose CLOSE we already sent is dropped, and a
      // CLOSE among them still frees the number.
      if (this.closing.has(frame.channel)) {
        if (frame.type === wire.TYPE_CLOSE) this.closing.delete(frame.channel);
        return undefined;
      }
      return this.fail(wire.CODES.PROTOCOL);
    }
    return ch.receive(frame);
  }

  // onControlFrame reads channel 0. Only DATA travels there, and only the
  // three types the API may send on a session link; everything else ends the
  // link, as the contract says it must.
  async onControlFrame(frame) {
    if (frame.type !== wire.TYPE_DATA) return this.fail(wire.CODES.PROTOCOL);
    let c;
    try {
      c = wire.parseControl(frame.payload);
    } catch {
      return this.fail(wire.CODES.PROTOCOL);
    }
    if (!wire.controlAllowed(wire.LINK_SESSION, true, c.t)) return this.fail(wire.CODES.PROTOCOL);
    // Only the READING is guarded here. A listener the shell hung up is not
    // the API's doing, and a link that ended on E_PROTOCOL because a listener
    // threw would blame the wrong side; that one travels on, out of pump,
    // where it is answered as what it is.
    let value;
    try {
      if (c.t === wire.CTL_TRUST) value = session.parseTrustHint(c.b);
      // A notice is read here so one that breaks its contract ends the link -
      // and handed on as the BYTES it is: the desktop reads every answer with
      // the contracts itself (core.parseNotice in Desktop.noticeBytes), and a
      // link that handed over an object would be a second reader of the same
      // document. Both readings are of c.b, which the decoder built and
      // nothing outside this module holds.
      if (c.t === wire.CTL_NOTIFY) value = (core.parseNotice(c.b), c.b);
    } catch {
      // A hint or a notice that does not hold up to its contract is a
      // message the API should never have sent.
      return this.fail(wire.CODES.PROTOCOL);
    }
    if (c.t === wire.CTL_TRUST) {
      this.handlers.trust(value);
      return undefined;
    }
    if (c.t === wire.CTL_NOTIFY) {
      this.handlers.notice(value);
      return undefined;
    }
    // bye: the sender closes the link after it.
    this.handlers.bye();
    return this.fail(wire.CODES.UNREACHABLE);
  }

  // send puts one frame on the link. Everything goes through this one chain,
  // so the sending cipher state is never entered twice at once.
  send(frame) {
    if (this.dead || this.ws === null) return Promise.reject(new ChannelError(wire.CODES.UNREACHABLE));
    const b = wire.encodeFrame(frame);
    const run = this.sendQueue.then(async () => {
      if (this.dead || this.ws === null) throw new ChannelError(wire.CODES.UNREACHABLE);
      this.ws.send(await this.transport.seal(b));
    });
    // The chain must not break on one failed send, or every later send waits
    // on a rejected promise.
    this.sendQueue = run.catch(() => {});
    return run;
  }

  // openNumber picks a free number and keeps the shell from ever going over
  // the limit: the API would end the link.
  openNumber() {
    if (this.channelCount() >= wire.MAX_SESSION_CHANNELS) throw new ChannelError(wire.CODES.LIMIT);
    while (this.channels.has(this.next) || this.closing.has(this.next)) {
      this.next = this.next >= MAX_NUMBER ? 1 : this.next + 1;
    }
    return this.next;
  }

  // open starts one channel: it takes a free number, writes the OPEN and
  // hands back the channel. The OPEN is built by wire.encodeOpen, which holds
  // it against parseOpen before it leaves, so nothing the API refuses ever
  // goes out.
  async open({ app, service, action, params, window, e2e, taskBytes, confirmBytes }) {
    const number = this.openNumber();
    const w = window ?? wire.DEFAULT_WINDOW;
    const payload = wire.encodeOpen({
      app, service, action, params, window: w, e2e, taskBytes, confirmBytes,
    });
    const ch = new Channel(this, number, { window: w, e2e });
    // The number is taken before the frame goes out: two opens in one turn of
    // the event loop would otherwise both pick it.
    this.channels.set(number, ch);
    try {
      await this.send({ type: wire.TYPE_OPEN, channel: number, payload });
    } catch (e) {
      this.channels.delete(number);
      throw e;
    }
    return ch;
  }

  // handOver moves a channel out of the open ones and into the closing ones:
  // its CLOSE has gone out, the other side's has not arrived, and the number
  // still counts against MaxSessionChannels.
  handOver(ch) {
    if (this.channels.get(ch.number) !== ch) return;
    this.channels.delete(ch.number);
    if (ch.gotClose === null) this.closing.add(ch.number);
  }

  free(number) {
    this.channels.delete(number);
    this.closing.delete(number);
  }

  // close ends the link the friendly way: bye on channel 0, then the socket.
  // It is the only control message the browser may send on a session link
  // (wire.controlAllowed).
  close() {
    if (this.dead) return;
    const ws = this.ws;
    // fail() runs only once the bye is really out: it takes the socket away,
    // and a bye queued behind a socket that is already gone is a bye nobody
    // sends. Whether it got there or not, the link ends right after.
    const after = () => this.fail(wire.CODES.UNREACHABLE);
    if (ws !== null && ws.readyState === WebSocket.OPEN) {
      this.send({
        type: wire.TYPE_DATA, channel: wire.CONTROL_CHANNEL,
        payload: wire.encodeControl({ t: wire.CTL_BYE, b: null }),
      }).then(after, after);
      return;
    }
    after();
  }

  // fail ends every channel with this code and closes the socket. It runs at
  // most once: a second run would tell channels that are already gone.
  fail(code) {
    if (this.dead) return undefined;
    this.dead = true;
    const open = [...this.channels.values()];
    this.channels.clear();
    this.closing.clear();
    for (const ch of open) ch.giveUp(new ChannelError(code));
    const ws = this.ws;
    this.ws = null;
    if (ws !== null) {
      ws.onmessage = null;
      ws.onclose = null;
      ws.onerror = null;
      try {
        ws.close();
      } catch {
        // Already closing or closed; there is nothing left to do.
      }
    }
    return undefined;
  }
}

export { ChannelError };

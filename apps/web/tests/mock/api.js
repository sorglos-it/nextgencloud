// The other end for the browser tests: an API in node that speaks the real
// protocol - the seven HTTP paths before the WebSocket, the Noise session as
// responder, and channels with credit, EOF and CLOSE - using the SAME modules
// the shell uses, imported straight out of public/assets/js. No shell test
// needs a real API, and no answer here is written by hand where a contract
// can write it.
//
// WHY IT SITS IN FRONT OF PHP AND NOT BEHIND IT. The task brief has the PHP
// test server pass /api/ on to this one. That cannot carry the WebSocket:
// PHP's built-in cli-server has no way to hand a socket over, so an Upgrade
// reaching it is answered as an ordinary request - and a WebSocket to this
// process's OWN port is a second origin, which the shell's
// "connect-src 'self'" refuses (measured; see the task report). So it is the
// other way round: the browser talks to this server, /api/ is answered here,
// and everything else is passed through to the PHP server unchanged. The
// shell then sees ONE origin for page, assets, /api/ and the socket, which is
// what operation looks like - and public/index.php needs no test branch at
// all.
import { createServer, request as httpRequest } from "node:http";
import { createHash, randomBytes } from "node:crypto";
import { argv } from "node:process";
import { pathToFileURL } from "node:url";

import { b64encode, equal, utf8 } from "../../public/assets/js/contracts/b64.js";
import * as wire from "../../public/assets/js/contracts/wire.js";
import * as noise from "../../public/assets/js/contracts/noise.js";
import * as session from "../../public/assets/js/contracts/session.js";
import * as core from "../../public/assets/js/contracts/core.js";
import * as proof from "../../public/assets/js/contracts/proof.js";
import * as sign from "../../public/assets/js/contracts/sign.js";
import { Budget, decode, bytes, str, P, S } from "../../public/assets/js/contracts/json.js";
import { buildWorld } from "./world.js";

// The GUID of RFC 6455, section 1.3. It is NOT written from memory: the line
// below is held against the RFC's own example on every start (accept()), and
// it was wrong for the first three hours of this file - "…-95CA-5AB0DC85B11F"
// instead of "…-95CA-C5AB0DC85B11", which is one C moved and one F too many.
// Both engines and node's own WebSocket client refused the handshake with
// "Incorrect 'Sec-WebSocket-Accept' header value", and the server looked
// right at every step: the key on the wire, the digest and the base64 all
// agreed with each other and with sha1sum. A constant nobody checks is a
// constant that is right by luck.
const GUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11";

// accept is the Sec-WebSocket-Accept of a key (RFC 6455, 4.2.2 step 5).
function accept(key) {
  return createHash("sha1").update(key + GUID).digest("base64");
}

// The example of RFC 6455, section 1.3, checked once when this file loads.
if (accept("dGhlIHNhbXBsZSBub25jZQ==") !== "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=") {
  throw new Error("mock: the WebSocket GUID is wrong");
}
const certBudget = new Budget({ size: session.MAX_CERT });

// The body of PathLoginFinish (Go: session.LoginFinish). The shell writes it
// and never reads one, so the web contracts have no parser for it; the shape
// is put together here out of the pieces that ARE frozen - proof.proofShape
// for both proofs - so a member that changes there changes here with it.
const loginFinishShape = S({
  cert: bytes, proof: proof.proofShape, password: P({ ...proof.proofShape.m }), totp: str,
});
const finishBudget = new Budget({ size: session.MAX_REQUEST });
const JSON_TYPE = "application/json; charset=utf-8";
// The Noise tag an inner message costs, taken from the contract and not
// written as 16: wire says MAX_E2E_PLAINTEXT is MAX_PAYLOAD minus the tag.
const TAG_SIZE = wire.MAX_PAYLOAD - wire.MAX_E2E_PLAINTEXT;
// The answer of ngc.users/api/log in this world: over wire.MAX_CALL_RESULT on
// purpose, so the shell has to be the one that stops it.
const HUGE = 9 << 20;

// ------------------------------------------------------------------ helpers

const enc = new TextEncoder();

function contains(haystack, needle) {
  if (needle.length === 0 || needle.length > haystack.length) return false;
  for (let i = 0; i + needle.length <= haystack.length; i++) {
    if (equal(haystack.subarray(i, i + needle.length), needle)) return true;
  }
  return false;
}

function sendJSON(res, status, body) {
  res.writeHead(status, { "Content-Type": JSON_TYPE, "Cache-Control": "no-store" });
  res.end(Buffer.from(body));
}

// Every error answer is the CLOSE JSON of its code with the status the
// contract gives it (shared/README.md, "Vor dem WebSocket").
function sendError(res, code) {
  sendJSON(res, session.httpStatus(code), wire.encodeClose(wire.errorClose(code)));
}

function readBody(req, limit) {
  return new Promise((ok, no) => {
    const parts = [];
    let n = 0;
    req.on("data", (c) => {
      n += c.length;
      if (n > limit) { no(new Error("too large")); req.destroy(); return; }
      parts.push(c);
    });
    req.on("end", () => ok(new Uint8Array(Buffer.concat(parts))));
    req.on("error", no);
  });
}

// ------------------------------------------------------- the WebSocket frames

// A minimal RFC 6455 reader. It is deliberately small: only what a browser
// sends on this link - binary and text data, close, ping - and it refuses
// everything else rather than guessing.
class WSReader {
  constructor(onMessage, onClose) {
    this.buf = Buffer.alloc(0);
    this.onMessage = onMessage;
    this.onClose = onClose;
    this.fragments = [];
    this.fragmentOpcode = 0;
  }

  push(chunk) {
    this.buf = Buffer.concat([this.buf, chunk]);
    for (;;) {
      const frame = this.take();
      if (frame === null) return;
      if (frame.opcode === 0x8) { this.onClose(); return; }
      if (frame.opcode === 0x9 || frame.opcode === 0xa) continue;
      if (frame.opcode === 0x0) {
        this.fragments.push(frame.payload);
      } else {
        this.fragments = [frame.payload];
        this.fragmentOpcode = frame.opcode;
      }
      if (!frame.fin) continue;
      const all = Buffer.concat(this.fragments);
      this.fragments = [];
      this.onMessage(this.fragmentOpcode, all);
    }
  }

  take() {
    const b = this.buf;
    if (b.length < 2) return null;
    const fin = (b[0] & 0x80) !== 0;
    const opcode = b[0] & 0x0f;
    const masked = (b[1] & 0x80) !== 0;
    let len = b[1] & 0x7f;
    let at = 2;
    if (len === 126) {
      if (b.length < at + 2) return null;
      len = b.readUInt16BE(at); at += 2;
    } else if (len === 127) {
      if (b.length < at + 8) return null;
      const big = b.readBigUInt64BE(at); at += 8;
      if (big > BigInt(Number.MAX_SAFE_INTEGER)) throw new Error("mock: frame too large");
      len = Number(big);
    }
    let mask = null;
    if (masked) {
      if (b.length < at + 4) return null;
      mask = b.subarray(at, at + 4); at += 4;
    }
    if (b.length < at + len) return null;
    const payload = Buffer.from(b.subarray(at, at + len));
    if (mask !== null) {
      for (let i = 0; i < payload.length; i++) payload[i] ^= mask[i & 3];
    }
    this.buf = b.subarray(at + len);
    return { fin, opcode, payload };
  }
}

// One binary frame out. The server never masks.
function wsFrame(payload, opcode = 0x2) {
  const n = payload.length;
  let head;
  if (n < 126) {
    head = Buffer.from([0x80 | opcode, n]);
  } else if (n < 65536) {
    head = Buffer.alloc(4);
    head[0] = 0x80 | opcode; head[1] = 126; head.writeUInt16BE(n, 2);
  } else {
    head = Buffer.alloc(10);
    head[0] = 0x80 | opcode; head[1] = 127; head.writeBigUInt64BE(BigInt(n), 2);
  }
  return Buffer.concat([head, Buffer.from(payload)]);
}

// ---------------------------------------------------------------- a channel

class MockChannel {
  constructor(conn, number, open) {
    this.conn = conn;
    this.number = number;
    this.open = open;
    // The client's window is the credit it gives us; ours is what we give it.
    this.sendLeft = open.window;
    this.recvWindow = wire.DEFAULT_WINDOW;
    this.recvLeft = wire.DEFAULT_WINDOW;
    this.creditWaiters = [];
    this.transport = null;   // the controller side of an end-to-end channel
    this.gotEOF = false;
    this.closed = false;
    this.sentClose = false;
    this.action = core.lookup(open.app, open.service, open.action);
    this.behaviour = conn.takeBehaviour();
    // Answers are produced on a chain of their own. They wait for credit,
    // and waiting for credit inside the receive loop would mean waiting for
    // a GRANT that the receive loop is the one to read - measured on the
    // 9 MiB answer: the link stood still after the first window.
    this.work = Promise.resolve();
  }

  kick(fn) {
    this.work = this.work.then(fn).catch(() => {});
    return this.work;
  }

  grantRoom(credit, handshake) {
    return this.conn.send({
      type: wire.TYPE_GRANT, channel: this.number, payload: wire.grant(credit, handshake),
    });
  }

  async data(payload) {
    // Credit goes out before the bytes do, so we never write past what the
    // client allowed.
    for (let i = 0; i < payload.length;) {
      while (this.sendLeft === 0) {
        await new Promise((ok) => this.creditWaiters.push(ok));
        if (this.closed) return;
      }
      const n = Math.min(payload.length - i, wire.MAX_PAYLOAD, this.sendLeft);
      const piece = payload.subarray(i, i + n);
      this.sendLeft -= n;
      this.conn.watch(piece);
      if (this.transport !== null) this.conn.watchE2E(piece);
      await this.conn.send({ type: wire.TYPE_DATA, channel: this.number, payload: piece });
      i += n;
    }
  }

  async eof() {
    await this.conn.send({ type: wire.TYPE_EOF, channel: this.number, payload: new Uint8Array(0) });
  }

  async closeWith(payload) {
    if (this.sentClose) return;
    this.sentClose = true;
    await this.conn.send({ type: wire.TYPE_CLOSE, channel: this.number, payload });
  }

  ok() {
    return this.closeWith(wire.encodeClose({ status: "ok", code: "", message: "", exitCode: null }));
  }

  fail(code) {
    return this.closeWith(wire.encodeClose(wire.errorClose(code)));
  }

  addCredit(credit) {
    this.sendLeft += credit;
    const waiting = this.creditWaiters;
    this.creditWaiters = [];
    for (const w of waiting) w();
  }

  async took(n) {
    if (n > this.recvLeft) { await this.fail(wire.CODES.PROTOCOL); return false; }
    this.recvLeft -= n;
    const used = this.recvWindow - this.recvLeft;
    if (used >= this.recvWindow >> 1) {
      this.recvLeft = this.recvWindow;
      await this.grantRoom(used, null);
    }
    return true;
  }
}

// ------------------------------------------------------------- a session link

class Conn {
  constructor(socket, world, state) {
    this.socket = socket;
    this.world = world;
    this.state = state;          // the logged-in session: client cert and keys
    this.transport = null;
    this.channels = new Map();
    this.queue = Promise.resolve();   // one chain, so no two seals overlap
    this.dead = false;
  }

  // The behaviour a case asked for through the mock's own test door. It is
  // kept on the SESSION, because that door is an HTTP request of its own and
  // knows nothing of this socket, and it is taken away as it is read: it
  // steers one channel and not every channel after it.
  takeBehaviour() {
    const b = this.state.behaviour;
    this.state.behaviour = null;
    return b ?? {};
  }

  // watch is the API's own eye on what it moves. Everything that goes over
  // this link passes here, and if a byte string the controller side has had
  // in the clear ever turns up in it, the API saw plaintext.
  watch(payload) {
    for (const secret of this.world.secrets) {
      if (contains(payload, secret)) this.state.sawPlaintext = true;
    }
  }

  // watchE2E is the second half of that eye, and the half that can fire
  // WITHOUT knowing the secret first. The list above is only ever filled
  // after the controller has decrypted a message, so a shell that sent its
  // inner message in the CLEAR would never put anything in it - the
  // controller could not read that message at all. This one asks the other
  // question: is what the API is carrying readable on its own?
  //
  // Deterministic, no guessing: a sealed inner message is its plaintext plus
  // the 16-byte Noise tag, so anything SHORTER than the shortest sealed
  // message (one kind byte plus the tag) cannot be ciphertext. If such a
  // payload also parses as a wire E2E plaintext, it IS one.
  //
  // Its limit, named: a cleartext message of 16 bytes or more is not caught
  // here, because a ciphertext of that length is possible too and guessing
  // would make this answer flap. Those are caught by the list above, as soon
  // as the controller has seen the same bytes once.
  watchE2E(payload) {
    if (payload.length >= 1 + TAG_SIZE) return;
    try {
      wire.parseE2E(payload, false);
      this.state.sawPlaintext = true;
    } catch {
      // Not readable without the key, which is what it should be.
    }
  }

  send(frame) {
    const b = wire.encodeFrame(frame);
    const run = this.queue.then(async () => {
      if (this.dead) return;
      this.socket.write(wsFrame(await this.transport.seal(b)));
    });
    this.queue = run.catch(() => {});
    return run;
  }

  // one takes exactly one transport message. Every message runs to its END
  // before the next one starts - the chain that holds that is startLink's
  // "pending" - because the receiving cipher state may not be entered twice
  // at once: it builds its IV before the await and raises the counter after.
  async one(payload) {
    if (this.dead) return;
    const frame = wire.decodeFrame(await this.transport.open(payload));
    if (frame.channel === wire.CONTROL_CHANNEL) {
      const c = wire.parseControl(frame.payload);
      if (!wire.controlAllowed(wire.LINK_SESSION, false, c.t)) throw new Error("mock: control");
      this.stop();
      return;
    }
    if (frame.type === wire.TYPE_OPEN) { await this.onOpen(frame); return; }
    const ch = this.channels.get(frame.channel);
    if (ch === undefined) throw new Error("mock: unknown channel");
    if (frame.type === wire.TYPE_GRANT) { ch.addCredit(wire.parseGrant(frame.payload).credit); return; }
    if (frame.type === wire.TYPE_DATA) { await this.onData(ch, frame.payload); return; }
    if (frame.type === wire.TYPE_EOF) { await this.onEOF(ch); return; }
    if (frame.type === wire.TYPE_CLOSE) {
      wire.parseClose(frame.payload);
      if (!ch.sentClose) { ch.sentClose = true; await this.send({ type: wire.TYPE_CLOSE, channel: ch.number, payload: frame.payload }); }
      ch.closed = true;
      ch.addCredit(0);
      this.channels.delete(ch.number);
      return;
    }
    throw new Error("mock: frame");
  }

  async onOpen(frame) {
    if (this.channels.size >= wire.MAX_SESSION_CHANNELS) throw new Error("mock: too many channels");
    const open = wire.parseOpen(frame.payload);
    if (this.channels.has(frame.channel)) throw new Error("mock: channel in use");
    const ch = new MockChannel(this, frame.channel, open);
    this.channels.set(frame.channel, ch);
    if (wire.CORE_APP(open.app)) {
      if (ch.action === null) { await ch.fail(wire.CODES.NOTFOUND); return; }
      await ch.grantRoom(ch.recvWindow, null);
      return;
    }
    // An app: this side plays the controller of the end-to-end handshake,
    // with the Noise key ctl-1 carries in the trust list and the dh key of
    // the client certificate as the peer.
    const prologue = await noise.e2ePrologue(this.world.controller.id, open.app, open.service,
      open.action, open.params ?? new Uint8Array(0), new Uint8Array(0));
    const hs = await noise.HandshakeKK.of(false, prologue, this.world.controller.noise.priv,
      this.world.controller.noise.pub, this.state.clientDH);
    await hs.readMessage1(open.e2e);
    const msg2 = await hs.writeMessage2();
    ch.transport = await hs.split();
    // Message 2 travels in the FIRST grant and in no other.
    await ch.grantRoom(ch.recvWindow, msg2);
  }

  async onData(ch, payload) {
    this.watch(payload);
    if (ch.transport !== null) this.watchE2E(payload);
    if (!(await ch.took(payload.length))) return;
    if (ch.transport === null) return;      // a core channel takes no input here
    const plain = await ch.transport.open(payload);
    const inner = wire.parseE2E(plain, false);
    if (inner.kind === wire.E2E_DATA) {
      // Whatever the service was given in the clear is from now on a byte
      // string the API must never be seen carrying.
      if (inner.data.length > 0) {
        const clear = inner.data.slice();
        this.world.secrets.push(clear);
        // And the frame that just carried it is checked again, now that we
        // know what was in it: if the cleartext stood in the ciphertext, the
        // API saw it.
        this.watch(payload);
        ch.kick(() => this.innerData(ch, clear));
      }
      return;
    }
    // The client is done sending. The service answers, ends, and the channel
    // closes.
    ch.kick(() => this.innerEnd(ch));
  }

  // The one service this world has: it echoes what it is given, which is
  // what the end-to-end test reads back.
  async innerData(ch, data) {
    const answer = wire.e2e(wire.E2E_DATA, data);
    await ch.data(await ch.transport.seal(answer));
  }

  async innerEnd(ch) {
    const body = wire.encodeClose({ status: "ok", code: "", message: "", exitCode: null });
    await ch.data(await ch.transport.seal(wire.e2e(wire.E2E_END, body)));
    await ch.eof();
    await ch.ok();
    this.channels.delete(ch.number);
  }

  // EOF from the client is what starts a call: its input is complete.
  async onEOF(ch) {
    if (ch.gotEOF) throw new Error("mock: second eof");
    ch.gotEOF = true;
    if (ch.transport !== null) return;      // end to end: the inner end decides
    ch.kick(() => this.answer(ch));
  }

  async answer(ch) {
    const key = `${ch.open.app}/${ch.open.service}/${ch.open.action}`;
    // The answer of ngc.users/api/log is over MaxCallResult on purpose: the
    // shell has to be the one that stops it.
    if (key === `${core.APP_USERS}/${core.SERVICE_API}/${core.ACT_LOG}`) {
      await ch.data(new Uint8Array(HUGE));
      await ch.eof();
      await ch.ok();
      return;
    }
    const body = this.world.results.get(key);
    if (body === undefined) { await ch.fail(wire.CODES.NOTFOUND); return; }
    await ch.data(body);
    // failAfterData: data first and then an error, so a receiver that kept
    // what arrived would be seen doing it.
    if (ch.behaviour.failAfterData === true) { await ch.fail(wire.CODES.EXIT); return; }
    await ch.eof();
    await ch.ok();
  }

  stop() {
    if (this.dead) return;
    this.dead = true;
    try { this.socket.end(wsFrame(Buffer.alloc(0), 0x8)); } catch { this.socket.destroy(); }
  }
}

// --------------------------------------------------------------- the server

export async function startMockAPI({ world, port, phpPort }) {
  const w = world ?? await buildWorld();
  w.secrets = w.secrets ?? [];
  const sessions = new Map();     // cookie token -> session state

  const server = createServer((req, res) => {
    route(req, res).catch(() => {
      if (!res.headersSent) sendError(res, wire.CODES.INTERNAL);
      else res.end();
    });
  });

  function cookieToken(req) {
    const raw = req.headers.cookie ?? "";
    for (const part of raw.split(";")) {
      const [k, v] = part.trim().split("=");
      if (k === session.COOKIE_NAME) return v ?? "";
    }
    return "";
  }

  async function route(req, res) {
    const url = new URL(req.url, "http://127.0.0.1");
    const path = url.pathname;
    if (path === "/ready") { res.writeHead(200, { "Content-Type": "text/plain" }); res.end("ok"); return; }
    if (!path.startsWith("/api/")) { proxy(req, res); return; }

    // A test-only door, so a case can say how the next channel should go
    // wrong, and can ask afterwards what this side saw. It is on /api/ so it
    // travels the same origin as everything else, and neither net/link.js nor
    // net/channel.js knows it exists.
    //
    // What it takes is one flag in the query and no body at all: a document
    // needs a parser, and the one contract this door has is "failAfterData is
    // there or it is not".
    if (path === "/api/v1/mock") {
      const state = sessions.get(cookieToken(req));
      if (req.method === "POST") {
        if (state !== undefined) {
          state.behaviour = { failAfterData: url.searchParams.get("failAfterData") === "1" };
        }
        sendJSON(res, 200, utf8("{}"));
        return;
      }
      // One character, so the asking side has nothing to parse.
      res.writeHead(200, { "Content-Type": "text/plain", "Cache-Control": "no-store" });
      res.end(state?.sawPlaintext === true ? "1" : "0");
      return;
    }

    if (path === session.PATH_SETUP) { sendError(res, wire.CODES.NOTFOUND); return; }

    if (path === session.PATH_LOGIN_START && req.method === "POST") {
      const state = {
        nonce: new Uint8Array(randomBytes(session.NONCE_SIZE)),
        now: Math.floor(Date.now() / 1000),
      };
      const body = utf8(JSON.stringify({
        nonce: b64encode(state.nonce), now: state.now, maxLifetime: session.MAX_LIFETIME,
        installation: w.installation, user: w.user.id,
        kdf: { ...w.user.kdf },
        passkeys: [], totp: false, rp: { id: w.list.rp.id, origins: w.list.rp.origins },
      }));
      // Read back with the shell's own parser: an answer the shell refuses is
      // a bug of this file, and it must show here.
      session.parseLoginChallenge(body);
      w.nonce = state.nonce;
      sendJSON(res, 200, body);
      return;
    }

    if (path === session.PATH_LOGIN_FINISH && req.method === "POST") {
      const answer = await finishLogin(await readBody(req, session.MAX_REQUEST));
      if (answer === null) { sendError(res, wire.CODES.LOGIN); return; }
      res.writeHead(200, {
        "Content-Type": JSON_TYPE, "Cache-Control": "no-store",
        "Set-Cookie": `${session.COOKIE_NAME}=${answer.token}; Path=/api/; HttpOnly; SameSite=Strict`,
      });
      res.end(Buffer.from(answer.body));
      return;
    }

    if (path === session.PATH_LOGOUT && req.method === "POST") {
      sessions.delete(cookieToken(req));
      res.writeHead(204, { "Cache-Control": "no-store" });
      res.end();
      return;
    }

    if (path === session.PATH_TRUST && req.method === "GET") {
      if (url.searchParams.get("from") !== "1") { res.writeHead(204, { "Cache-Control": "no-store" }); res.end(); return; }
      sendJSON(res, 200, w.chainBytes);
      return;
    }

    sendError(res, wire.CODES.NOTFOUND);
  }

  // finishLogin checks the client certificate the way the API does: the
  // proof is a real signature by anna's password key over the exact bytes of
  // the certificate, and the API answers with its own signed certificate.
  async function finishLogin(body) {
    // Strict JSON with a budget, and the members come from the contracts
    // themselves (proof.proofShape): the web side has no ParseLoginFinish,
    // because the shell WRITES this document and never reads one, so the
    // shape is assembled here out of the pieces that are frozen elsewhere.
    // Nothing is read with JSON.parse.
    let f;
    try {
      f = decode(body, finishBudget, loginFinishShape);
    } catch {
      return null;
    }
    const cert = f.cert;
    if (!(await proof.verify(f.proof, w.user.key, w.list.rp, sign.PURPOSE_CLIENT_CERT, cert))) return null;
    // The certificate goes through the contract's own shape and check too:
    // it is a document from outside like any other, and
    // session.clientCertShape plus validateClientCert is what the API holds
    // it to.
    const c = decode(cert, certBudget, session.clientCertShape);
    if (!session.validateClientCert(c)) return null;
    if (w.nonce === undefined || !equal(c.nonce, w.nonce)) return null;
    if (c.inst !== w.installation || c.user !== w.user.id) return null;
    const clientDH = c.dh;
    const pair = await crypto.subtle.generateKey({ name: "X25519" }, false, ["deriveBits"]);
    const apiDH = new Uint8Array(await crypto.subtle.exportKey("raw", pair.publicKey));
    const payload = utf8(JSON.stringify({
      inst: w.installation, node: w.api.id, client: b64encode(await session.certHash(cert)),
      dh: b64encode(apiDH), iat: c.iat, exp: c.exp,
    }));
    const sig = new Uint8Array(await crypto.subtle.sign({ name: "Ed25519" }, w.api.sign.priv,
      sign.message(sign.PURPOSE_API_CERT, payload)));
    const out = utf8(JSON.stringify({
      api: { payload: b64encode(payload), kid: await sign.keyID(w.api.sign.pub), sig: b64encode(sig) },
      passwordHint: true,
    }));
    session.parseLoginResult(out);
    const token = b64encode(new Uint8Array(randomBytes(session.TOKEN_SIZE)));
    sessions.set(token, {
      clientDH, apiKeys: { privateKey: pair.privateKey, pub: apiDH },
      sawPlaintext: false, behaviour: null,
    });
    return { token, body: out };
  }

  // Everything that is not /api/ belongs to the PHP server behind this one.
  // It is passed through unchanged, headers and status included, so the page
  // the browser gets is the page PHP built - content security policy and all.
  function proxy(req, res) {
    const out = httpRequest({
      host: "127.0.0.1", port: phpPort, method: req.method, path: req.url,
      headers: { ...req.headers, host: `127.0.0.1:${phpPort}` },
    }, (answer) => {
      res.writeHead(answer.statusCode, answer.headers);
      answer.pipe(res);
    });
    out.on("error", () => { res.writeHead(502, { "Content-Type": "text/plain" }); res.end("no php"); });
    req.pipe(out);
  }

  server.on("upgrade", (req, socket) => {
    const url = new URL(req.url, "http://127.0.0.1");
    const state = sessions.get(cookieToken(req));
    const key = req.headers["sec-websocket-key"] ?? "";
    const wants = (req.headers["sec-websocket-protocol"] ?? "").split(",").map((s) => s.trim());
    // No session, no socket: the cookie of PathLoginFinish is what says who
    // this is, exactly as in operation.
    if (url.pathname !== session.PATH_SOCKET || state === undefined || key === ""
      || !wants.includes(session.SUBPROTOCOL)) {
      socket.end("HTTP/1.1 401 Unauthorized\r\n\r\n");
      return;
    }
    socket.write("HTTP/1.1 101 Switching Protocols\r\n"
      + "Upgrade: websocket\r\nConnection: Upgrade\r\n"
      + `Sec-WebSocket-Accept: ${accept(key)}\r\n`
      + `Sec-WebSocket-Protocol: ${session.SUBPROTOCOL}\r\n\r\n`);
    startLink(socket, state);
  });

  function startLink(socket, state) {
    const conn = new Conn(socket, w, state);
    let handshakeDone = false;
    let pending = Promise.resolve();
    const reader = new WSReader((opcode, payload) => {
      if (opcode !== 0x2) { conn.stop(); return; }
      pending = pending.then(async () => {
        if (conn.dead) return;
        if (!handshakeDone) {
          const hs = await noise.HandshakeKK.of(false, utf8(noise.PROLOGUE_SESSION),
            state.apiKeys.privateKey, state.apiKeys.pub, state.clientDH);
          await hs.readMessage1(new Uint8Array(payload));
          socket.write(wsFrame(await hs.writeMessage2()));
          conn.transport = await hs.split();
          handshakeDone = true;
          return;
        }
        conn.watch(new Uint8Array(payload));
        await conn.one(new Uint8Array(payload));
      }).catch(() => conn.stop());
    }, () => conn.stop());
    socket.on("data", (c) => {
      try { reader.push(c); } catch { conn.stop(); }
    });
    socket.on("error", () => conn.stop());
    socket.on("close", () => { conn.dead = true; });
  }

  await new Promise((ok) => server.listen(port, "127.0.0.1", ok));
  return {
    url: `http://127.0.0.1:${port}`,
    stop: () => new Promise((ok) => server.close(ok)),
  };
}

// Started as a program by playwright.config.js. The two ports come from the
// environment: this server is the one the browser talks to, the PHP one sits
// behind it. The comparison is of URLs and not of file names - a name that
// happens to end the same would run the server inside a test that only meant
// to import startMockAPI.
if (argv[1] !== undefined && import.meta.url === pathToFileURL(argv[1]).href) {
  const port = Number(process.env.NGC_MOCK_PORT) || 8099;
  const phpPort = Number(process.env.NGC_PHP_PORT) || (port + 1);
  await startMockAPI({ port, phpPort });
  process.stdout.write(`mock api on ${port}, php behind on ${phpPort}\n`);
}

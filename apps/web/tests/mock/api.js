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
// and everything else is passed through to the PHP server. The shell then sees
// ONE origin for page, assets, /api/ and the socket, which is what operation
// looks like - and public/index.php needs no test branch at all.
//
// Sitting in front is also what lets this process stand in for the OTHER half
// of operation: Apache, which serves the files below assets/ itself and puts
// their headers on them. Those headers are added on the way through, out of the
// virtual host that ships - see withAssetWalls below. Everything else goes by
// unchanged.
import { createServer, request as httpRequest } from "node:http";
import { createHash, randomBytes } from "node:crypto";
import { readFileSync } from "node:fs";
import { argv } from "node:process";
import { fileURLToPath, pathToFileURL } from "node:url";

import { b64encode, equal, utf8 } from "../../public/assets/js/contracts/b64.js";
import * as wire from "../../public/assets/js/contracts/wire.js";
import * as noise from "../../public/assets/js/contracts/noise.js";
import * as session from "../../public/assets/js/contracts/session.js";
import * as core from "../../public/assets/js/contracts/core.js";
import * as proof from "../../public/assets/js/contracts/proof.js";
import * as sign from "../../public/assets/js/contracts/sign.js";
import * as trust from "../../public/assets/js/contracts/trust.js";
import { Budget, decode, bytes, str, P, S } from "../../public/assets/js/contracts/json.js";
import { buildWorld, RP_ID, RP_ORIGINS, TOTP_CODE, SETUP_CODE } from "./world.js";

// A TEST-ONLY DOOR FOR THE USER INTERFACES OF THE APPS NGC SHIPS, and a
// security rule is the reason it exists.
//
// The calculator and the terminal are apps a person activates: their interface
// is the ui/ of an app package and arrives as a SIGNED ARCHIVE over
// ngc.desktop/ui. Their source is the web interface's own
// (apps/web/assets/apps/<app>/), and it may NOT lie in the web root: an .html
// under public/ is a document of the shell's OWN origin, reachable by its
// address, with the shell's storage, cookies and session in reach - which is
// exactly what the app frame takes away from an app. tests/shell/csp.spec.js
// holds the web root to that ("the web root holds nothing a browser would run
// as a document of this origin"), and it found it when it was not so.
//
// In operation whoever builds the package reads those files; here this door
// hands them to the stand-in in shell/fake-link.js, which packs them and signs
// their checksum. It goes away with that file.
//
// TWO RULES, BOTH ABOUT THIS BEING A SERVER:
//   - The name decides the file, and only out of the table below. A name out
//     of a URL never becomes a path, so there is nothing for a "..", a
//     backslash or a percent to climb with.
//   - Everything goes out as text/plain with nosniff. This door answers on the
//     SAME origin as the shell, so a file served as text/html here would be
//     the very document the rule above forbids - only through the test
//     harness instead of through Apache.
const SHIPPED_DOOR = "/mock/ui/";
const SHIPPED_UI = new Map();
for (const app of ["calculator", "terminal"]) {
  for (const name of ["index.html", "app.css", "app.js"]) {
    SHIPPED_UI.set(`${app}/${name}`, fileURLToPath(new URL(`../../assets/apps/${app}/${name}`, import.meta.url)));
  }
}

function sendShipped(res, name) {
  const file = SHIPPED_UI.get(name);
  if (file === undefined) {
    res.writeHead(404, { "Content-Type": "text/plain; charset=utf-8", "X-Content-Type-Options": "nosniff" });
    res.end("no such file");
    return;
  }
  // Read on every request and not once at the start: these are source files of
  // the repository, and a run that measures them has to measure what is there
  // now. A missing one is a broken checkout, which is worth an error that says
  // so rather than an empty answer that packs an empty archive.
  const body = readFileSync(file);
  res.writeHead(200, {
    "Content-Type": "text/plain; charset=utf-8",
    "X-Content-Type-Options": "nosniff",
    "Cache-Control": "no-store",
  });
  res.end(body);
}

// THE WALLS APACHE PUTS ON THE FILES IT HANDS OUT ITSELF, so that a test can
// measure the real situation and not the test server's.
//
// WHY IT IS HERE AND NOT IN public/index.php. In operation PHP never sees a
// file below assets/: Apache serves it and the headers come out of the virtual
// host (templates/apache-vhost.conf). The test server has no Apache - PHP's
// built-in one answers those paths raw, which is what index.php's "return
// false" is for - so something has to stand where Apache stands. This process
// already does: it sits IN FRONT of PHP and passes on everything that is not
// /api/. Putting the headers on the way out is the whole change, and
// public/index.php keeps its ONE test branch instead of getting a second.
//
// THE VALUES ARE NOT WRITTEN HERE. They are read out of the template that
// ships, so there is no second copy that could drift from it: a header taken
// out of the virtual host is a header missing from these answers, and
// tests/shell/csp.spec.js says so.
const VHOST = fileURLToPath(new URL("../../templates/apache-vhost.conf", import.meta.url));

// assetWalls reads the Header lines of the assets block out of the virtual
// host. A line it does not understand is an ERROR and never something to skip:
// this is read by a machine, the block says so, and a wall quietly dropped
// here would make a green test out of an installation with a hole in it.
function assetWalls(file) {
  const lines = readFileSync(file, "utf8").split(/\r?\n/);
  const open = lines.findIndex((l) => /^\s*<Directory\s+"[^"]*\/public\/assets"\s*>\s*$/.test(l));
  if (open < 0) throw new Error(`mock: ${file} has no <Directory ".../public/assets"> block`);
  const walls = [];
  for (let i = open + 1; i < lines.length; i++) {
    const line = lines[i];
    if (/^\s*<\/Directory>\s*$/.test(line)) {
      if (walls.length === 0) throw new Error(`mock: the assets block of ${file} sets no header`);
      return walls;
    }
    if (/^\s*(#|$)/.test(line)) continue;
    const m = line.match(/^\s*Header always set ([A-Za-z0-9-]+) "([^"]*)"\s*$/);
    if (m === null) {
      throw new Error(`mock: ${file}:${i + 1} is no "Header always set" line: ${line.trim()}`);
    }
    walls.push([m[1], m[2]]);
  }
  throw new Error(`mock: the assets block of ${file} is never closed`);
}

const ASSET_WALLS = assetWalls(VHOST);

// withAssetWalls puts those headers on an answer for a path below /assets/ -
// but only on the answers Apache would have given ITSELF.
//
// HOW THE TWO ARE TOLD APART, without a copy of index.php's rule standing
// here: by what came back. Every answer index.php writes goes through
// Page::headers and so carries a Content-Security-Policy; a raw file out of
// the web root carries none (measured: a length, a type where the extension
// gives one, and nothing of ours). So an answer WITH a policy was written by
// the application and is left exactly as it is - that is the 404 for a file
// that is not there, and it already carries every wall of the shell. An answer
// WITHOUT one is a file, and gets the walls of the virtual host.
//
// It sets and does not add, the way mod_headers' "set" does: a name that is
// already there goes, in whatever case it was spelled, so no answer can end up
// carrying one header twice.
function withAssetWalls(url, headers) {
  if (!url.startsWith("/assets/")) return headers;
  if ("content-security-policy" in headers) return headers;
  const out = { ...headers };
  for (const [name, value] of ASSET_WALLS) {
    const low = name.toLowerCase();
    for (const had of Object.keys(out)) if (had.toLowerCase() === low) delete out[had];
    out[name] = value;
  }
  return out;
}

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
// The body of PathLoginStart (Go: session.LoginStart). One member, and the
// name decides which challenge is written - so it is read strictly too.
const loginStartShape = S({ name: str });
// The body of POST PathSetup (Go: session.Setup). The shell writes it and
// never reads one either, so the shape stands here out of the piece that IS
// frozen - trust.signedShape, the version and its proof.
const setupShape = S({ mac: bytes, trust: trust.signedShape });
// Another one-time code than the installation's own, for the case that plays
// a man in the middle. It is a canonical code (session.validSetupCode) and it
// is NOT the one the browser types, which is the whole point: whoever does
// not hold the code cannot make the MAC over the offer.
const FOREIGN_CODE = "22222-33333-44444-55555";
// The keys such an offer carries instead of the installation's own: 32 bytes
// nobody has, so anything of that offer that reached the screen would be
// plain to see.
const FOREIGN_KEY = new Uint8Array(32).fill(0xee);
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

// ------------------------------------------------- the documents of the setup
//
// Three writers that have to agree with trust.encodeList CHARACTER FOR
// CHARACTER, and the reason is the contract's: session.VerifySetup compares
// the nodes and the store of version 1 with the ones it offered AS JSON
// (Go: sameNodes, sameJSON). trust.js keeps its own encodeNode and
// encodeStore private, so these are written here - in the field order of the
// Go structs, with a field left out wherever the struct tag has omitempty
// and the value is empty. A field more, a field fewer or one in another
// place, and an honest version 1 would be refused.

function nodeDoc(n) {
  const o = { id: n.id, kind: n.kind, name: n.name, noise: b64encode(n.noise) };
  if (n.sign.length > 0) o.sign = b64encode(n.sign);
  if (n.url !== "") o.url = n.url;
  if (n.tls.length > 0) o.tls = b64encode(n.tls);
  if (n.rootUsers.length > 0) o.rootUsers = n.rootUsers;
  if (n.rootRoles.length > 0) o.rootRoles = n.rootRoles;
  return o;
}

// A store has no omitempty on any of its four fields.
const storeDoc = (s) => ({ id: s.id, name: s.name, url: s.url, key: b64encode(s.key) });

// One signed version as the document it is, the way apps/store.js and
// shell/trustlist.js write it: authData and clientData only where a passkey
// put something there.
function signedDoc(v) {
  const p = { kind: v.proof.kind, kid: v.proof.kid, sig: b64encode(v.proof.sig) };
  if (v.proof.authData.length > 0) p.authData = b64encode(v.proof.authData);
  if (v.proof.clientData.length > 0) p.clientData = b64encode(v.proof.clientData);
  return { payload: b64encode(v.payload), proof: p };
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
  // What the last login had to check, so a case can ask instead of believing
  // a flag the browser set about its own work: "", "password", "passkey" or
  // "passkey+password".
  let lastProofs = "";
  // Seconds the NEXT login attempt is locked for, set through the test door.
  // A real API counts failed attempts; a mock is told - and it is told once:
  // the number is taken and cleared in the same step, so a case that locks
  // the login cannot leave the next case locked too. One server answers the
  // whole run, and state that outlives the case that set it is how a suite
  // starts failing three files further on (measured: the E_LOCKED case took
  // the four cases after it down with it).
  let lockFor = 0;
  function takeLock() {
    const s = lockFor;
    lockFor = 0;
    return s;
  }

  // ------------------------------------------------- a fresh installation
  //
  // While `fresh` is set this API has NO ADMIN: GET PathSetup offers version 1
  // and POST PathSetup takes it. Otherwise both are 404 E_NOTFOUND, which is
  // what the contract says "as soon as there is an admin" - and what
  // buildWorld leaves behind, since its world has four of them.
  //
  // A setup that goes through does what a real API does: the version the
  // admin signed BECOMES this world's trust list, the admin of it becomes
  // who may log in, and `fresh` falls away because there is an admin now. So
  // the login right after the setup really runs against the list the shell
  // just built - which is a far stronger measurement than a 204.
  //
  // `saved` is the world buildWorld made. One server answers the whole run,
  // so a case that set the installation up must not leave it set up for the
  // file after it: the test door puts the saved world back, and so does the
  // next case that asks for a fresh one.
  let fresh = null;
  let saved = null;
  // How many times GET PathSetup reached this side since the last fresh
  // installation. It stands OUTSIDE fresh, because a case asks after the
  // setup has gone through and fresh is null by then.
  let setupOffers = 0;

  function restoreWorld() {
    if (saved === null) return;
    w.list = saved.list;
    w.installation = saved.installation;
    w.chainBytes = saved.chainBytes;
    w.logins = saved.logins;
    w.user = saved.user;
  }

  function startFresh(foreign) {
    if (saved === null) {
      saved = { list: w.list, installation: w.installation, chainBytes: w.chainBytes,
        logins: w.logins, user: w.user };
    } else {
      restoreWorld();
    }
    // What the offer holds, taken once out of the pristine world: the API
    // nodes, the global store and the release key of the standing approval
    // genesis already carries. POST PathSetup compares version 1 against
    // THESE, as the API compares against its own configuration - so the two
    // halves of one setup cannot disagree even if the world moved between
    // them.
    const nodes = w.list.nodes.filter((n) => n.kind === trust.NODE_API)
      .map((n) => (foreign ? { ...n, noise: FOREIGN_KEY, sign: FOREIGN_KEY, tls: FOREIGN_KEY } : n))
      .map(nodeDoc);
    const store = w.list.stores.find((s) => s.id === trust.STORE_GLOBAL) ?? null;
    const release = w.list.auto.find((a) => a.target === trust.AUTO_CONTROLLER)?.key ?? FOREIGN_KEY;
    fresh = {
      code: foreign ? FOREIGN_CODE : SETUP_CODE,
      nodes,
      store: store === null ? null : storeDoc(store),
      releaseKey: b64encode(foreign ? FOREIGN_KEY : release),
    };
    setupOffers = 0;
  }

  // offerBytes writes the answer of GET PathSetup: SetupInfo as JSON and
  // OfferMAC over exactly those bytes (Go: session.SetupOffer). `now` is this
  // server's clock, as the API's own is.
  async function offerBytes() {
    const info = utf8(JSON.stringify({
      installation: w.installation,
      nodes: fresh.nodes,
      now: Math.floor(Date.now() / 1000),
      releaseKey: fresh.releaseKey,
      ...(fresh.store === null ? {} : { store: fresh.store }),
    }));
    const offer = utf8(JSON.stringify({
      info: b64encode(info), mac: b64encode(await session.offerMAC(fresh.code, info)),
    }));
    // Read back with the shell's own parser, with the code this offer was
    // made for: an offer the shell cannot read is a bug of this file, and it
    // has to show here rather than three layers away. The foreign offer is
    // read back with the foreign code for the same reason - it is a valid
    // offer, just not one for the code the browser holds.
    await session.parseSetupOffer(offer, fresh.code);
    return offer;
  }

  // takeSetup checks version 1 the way the API does (Go: session.VerifySetup)
  // and, where it holds, installs it. false is E_VERIFY.
  async function takeSetup(body) {
    let s;
    try {
      s = decode(body, finishBudget, setupShape);
    } catch {
      return false;
    }
    // The code and the MAC first, before anything of the payload is opened.
    if (s.mac.length !== 32 || !equal(s.mac, await session.setupMAC(fresh.code, s.trust.payload))) {
      return false;
    }
    let v;
    try {
      v = await trust.open({ seq: 1, hash: await trust.hash(s.trust) }, [s.trust]);
    } catch {
      return false;
    }
    const l = v.list;
    if (l.installation !== w.installation) return false;
    if (JSON.stringify(l.nodes.map(nodeDoc)) !== JSON.stringify(fresh.nodes)) return false;
    // Either no store at all, or exactly the one that was offered.
    if (l.stores.length > 1) return false;
    if (l.stores.length === 1
      && (fresh.store === null || JSON.stringify(storeDoc(l.stores[0])) !== JSON.stringify(fresh.store))) {
      return false;
    }
    // A standing approval names no key but the release key of the offer, and
    // `system` names none at all.
    for (const a of l.auto) {
      if (a.target === trust.AUTO_SYSTEM) {
        if (a.key.length !== 0) return false;
      } else if (a.target === trust.AUTO_WEB || a.target === trust.AUTO_API
        || a.target === trust.AUTO_CONTROLLER) {
        if (b64encode(a.key) !== fresh.releaseKey) return false;
      } else {
        return false;
      }
    }
    installSetup(v, s.trust);
    return true;
  }

  // installSetup makes the version the admin signed this world's own, so the
  // login right after the setup runs against it. The API nodes come out of
  // the offer unchanged, so api-1 still carries the signing key this mock
  // holds and the API certificate still verifies.
  function installSetup(v, signed) {
    w.list = v.list;
    w.installation = v.list.installation;
    w.chainBytes = utf8(JSON.stringify({ versions: [signedDoc(signed)] }));
    // Read back with the shell's own parser, for the reason every other
    // answer of this file is.
    session.parseTrustChain(w.chainBytes);
    w.logins = new Map(v.list.users.map((u) => [u.name, { id: u.id, totp: false }]));
    const first = v.list.users[0];
    const pw = first.keys.find((k) => k.kind === proof.KIND_PASSWORD) ?? null;
    w.user = {
      id: first.id, name: first.name, key: pw ?? w.user.key,
      // challengeFor writes a made-up KDF for a name nobody has out of this
      // one, so it has to be the new admin's: a salt out of the old world
      // would answer with parameters no key of this installation has.
      kdf: pw === null ? w.user.kdf
        : { alg: pw.kdf.alg, salt: b64encode(pw.kdf.salt), m: pw.kdf.m, t: pw.kdf.t, p: pw.kdf.p },
    };
    // There is an admin now, so the setup is over - exactly as it is over for
    // the API.
    fresh = null;
  }

  // THE ONE PLACE WHERE THE MOCK'S RELYING PARTY DIFFERS FROM THE CHALLENGE'S.
  // The challenge carries world.RP_ORIGINS, which is https, because
  // url.validOrigin takes nothing else and session.parseLoginChallenge holds
  // every origin to it. What the browser really reports in clientDataJSON is
  // this test server's own origin, which is http - there is no TLS in front
  // of a php -S and a node http server. The shell never looks at origins (it
  // hands rp.id to the authenticator and nothing else), so the difference
  // reaches no shell code; it lives here, where the assertion is checked.
  const browserRP = {
    id: RP_ID,
    origins: [`http://localhost:${port}`, `http://127.0.0.1:${port}`],
  };

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
    if (path.startsWith(SHIPPED_DOOR)) { sendShipped(res, path.slice(SHIPPED_DOOR.length)); return; }
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
        // The lock needs no session: it is what the NEXT login runs into, and
        // there is no session before a login.
        const lock = url.searchParams.get("lock");
        if (lock !== null) lockFor = /^[0-9]{1,4}$/.test(lock) ? Number(lock) : 0;
        // A fresh installation needs no session either: there is no user yet.
        // "1" takes the admin away, "0" puts the world buildWorld made back.
        const setup = url.searchParams.get("setup");
        if (setup === "1") startFresh(url.searchParams.get("offer") === "foreign");
        if (setup === "0") { fresh = null; restoreWorld(); }
        if (state !== undefined) {
          state.behaviour = { failAfterData: url.searchParams.get("failAfterData") === "1" };
        }
        sendJSON(res, 200, utf8("{}"));
        return;
      }
      res.writeHead(200, { "Content-Type": "text/plain", "Cache-Control": "no-store" });
      // Which proofs the last login carried. A word, so the asking side has
      // nothing to parse.
      if (url.searchParams.get("ask") === "proofs") { res.end(lastProofs); return; }
      // How many times the shell asked for the setup offer. A number, so the
      // case does not have to believe a flag the page set about its own work.
      if (url.searchParams.get("ask") === "setup") { res.end(String(setupOffers)); return; }
      // One character, so the asking side has nothing to parse.
      res.end(state?.sawPlaintext === true ? "1" : "0");
      return;
    }

    // The setup, and only while there is no admin: 404 E_NOTFOUND otherwise,
    // which is what the contract gives this path "as soon as there is an
    // admin" (shared/README.md, "Vor dem WebSocket").
    if (path === session.PATH_SETUP) {
      if (fresh === null) { sendError(res, wire.CODES.NOTFOUND); return; }
      if (req.method === "GET") {
        setupOffers++;
        sendJSON(res, 200, await offerBytes());
        return;
      }
      if (req.method === "POST") {
        if (!(await takeSetup(await readBody(req, session.MAX_REQUEST)))) {
          sendError(res, wire.CODES.VERIFY);
          return;
        }
        res.writeHead(204, { "Cache-Control": "no-store" });
        res.end();
        return;
      }
      sendError(res, wire.CODES.PROTOCOL);
      return;
    }

    if (path === session.PATH_LOGIN_START && req.method === "POST") {
      // A locked login is turned away before any work is done, which is where
      // the contract puts E_LOCKED for this path too (shared/session:
      // "PathLoginStart ... 429 E_LOCKED").
      const locked = takeLock();
      if (locked > 0) {
        res.setHeader("Retry-After", String(locked));
        sendError(res, wire.CODES.LOCKED);
        return;
      }
      const body = await readBody(req, session.MAX_REQUEST);
      let name;
      try {
        name = decode(body, finishBudget, loginStartShape).name;
      } catch {
        sendError(res, wire.CODES.PROTOCOL);
        return;
      }
      const nonce = new Uint8Array(randomBytes(session.NONCE_SIZE));
      const answer = challengeFor(name, nonce);
      // Read back with the shell's own parser: an answer the shell refuses is
      // a bug of this file, and it must show here.
      session.parseLoginChallenge(answer);
      w.nonce = nonce;
      w.expect = name;
      sendJSON(res, 200, answer);
      return;
    }

    if (path === session.PATH_LOGIN_FINISH && req.method === "POST") {
      const locked = takeLock();
      if (locked > 0) {
        res.setHeader("Retry-After", String(locked));
        sendError(res, wire.CODES.LOCKED);
        return;
      }
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

  // challengeFor writes the LoginChallenge for a typed name. A name nobody
  // has gets one too, with the same members and a KDF of the same parameters
  // over a salt derived from the name - that is the contract's own rule
  // (session.ParseLoginChallenge's note), and the whole reason the shell may
  // be neither milder nor stricter about it.
  //
  // WHERE THIS MOCK IS THINNER THAN THE CONTRACT, said rather than hidden: a
  // name nobody has answers passkeys [] and totp false, so it can still be
  // told from clara's answer or bernd's. A real API derives those two from
  // its own secret as well. Nothing of the SHELL depends on it - it says the
  // one sentence either way, and the cases below measure that - but a case
  // that wanted to prove the ANSWERS are alike could not be written here.
  //
  // w.nonce and w.expect are one slot each, so two logins at once would tread
  // on one another. One browser logs in at a time in this suite.
  function challengeFor(name, nonce) {
    const known = w.logins.get(name) ?? null;
    const u = known === null ? null : trust.user(w.list, known.id);
    const keys = u === null ? [] : u.keys;
    const password = keys.find((k) => k.kind === proof.KIND_PASSWORD) ?? null;
    const passkeys = keys.filter((k) => k.kind === proof.KIND_PASSKEY);
    return utf8(JSON.stringify({
      nonce: b64encode(nonce),
      now: Math.floor(Date.now() / 1000),
      maxLifetime: session.MAX_LIFETIME,
      installation: w.installation,
      // A made-up but stable user ID for a name nobody has: the same name
      // always answers the same one, as the API's derived answer does.
      user: known === null ? madeUpID(name) : known.id,
      // A user without a password key still gets a KDF offered, so that the
      // answer for clara (passkey only) cannot be told from the answer for a
      // name that does not exist.
      kdf: password === null
        ? { ...w.user.kdf, salt: b64encode(madeUpSalt(name)) }
        : { alg: password.kdf.alg, salt: b64encode(password.kdf.salt),
          m: password.kdf.m, t: password.kdf.t, p: password.kdf.p },
      passkeys: passkeys.map((k) => ({ credId: b64encode(k.credId), kid: k.id })),
      totp: known?.totp === true,
      passwordToo: known !== null && trust.passwordToo(w.list, known.id),
      rp: { id: RP_ID, origins: [...RP_ORIGINS] },
    }));
  }

  // Two made-up values for a name nobody has. They are a hash of the name, so
  // the same name always answers the same thing - a value that changed per
  // request would say "this one is made up" all by itself. The API derives
  // them from a secret of the installation; a mock has nothing to keep.
  function madeUpID(name) {
    return b64encode(new Uint8Array(createHash("sha256").update("id\u0000" + name).digest()).subarray(0, 16));
  }
  function madeUpSalt(name) {
    return new Uint8Array(createHash("sha256").update("salt\u0000" + name).digest()).subarray(0, proof.SALT_SIZE);
  }

  // finishLogin checks the client certificate the way the API does: the
  // proof is a real signature by a key of that very user over the exact bytes
  // of the certificate, and the API answers with its own signed certificate.
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
    lastProofs = "";
    const cert = f.cert;
    // The certificate goes through the contract's own shape and check too:
    // it is a document from outside like any other, and
    // session.clientCertShape plus validateClientCert is what the API holds
    // it to. It comes FIRST now, because the certificate is what says whose
    // keys the proofs are held against.
    const c = decode(cert, certBudget, session.clientCertShape);
    if (!session.validateClientCert(c)) return null;
    if (w.nonce === undefined || !equal(c.nonce, w.nonce)) return null;
    if (c.inst !== w.installation) return null;
    // The name that was typed and the user the certificate names have to be
    // the same person; a name nobody has reaches no user at all and ends here
    // with the one answer every failed login gets.
    const known = w.logins.get(w.expect ?? "") ?? null;
    if (known === null || c.user !== known.id) return null;
    const u = trust.user(w.list, known.id);
    if (u === null) return null;
    // A second factor the API asked for has to be there. A mock has no clock
    // to check a real code against; what it does check is that the shell
    // ASKED for it and sent it on.
    if (known.totp && f.totp !== TOTP_CODE) return null;
    // The proof, by a key of this very user. The key is found by the key ID
    // the proof names, exactly as session.verifyApproval finds it.
    const key = u.keys.find((k) => k.id === f.proof.kid) ?? null;
    if (key === null) return null;
    if (!(await proof.verify(f.proof, key, browserRP, sign.PURPOSE_CLIENT_CERT, cert))) return null;
    // A role that asks for the password too: the proof must be by a passkey,
    // and a second proof by a password key of the same user must cover the
    // SAME certificate bytes (Go: session.VerifyPassword). Both halves are
    // checked, so a shell that sent the passkey twice fails here.
    const both = trust.passwordToo(w.list, known.id);
    if (both) {
      if (key.kind !== proof.KIND_PASSKEY || f.password === null) return null;
      const pwKey = u.keys.find((k) => k.id === f.password.kid) ?? null;
      if (pwKey === null || pwKey.kind !== proof.KIND_PASSWORD) return null;
      if (!(await proof.verify(f.password, pwKey, browserRP, sign.PURPOSE_CLIENT_CERT, cert))) return null;
    } else if (f.password !== null) {
      // A password proof nobody asked for is a document the API never reads,
      // so a shell that sends one is wrong about the contract.
      return null;
    }
    lastProofs = key.kind + (both ? "+" + proof.KIND_PASSWORD : "");
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
      // Only somebody who got in with a password alone is told to move to
      // passkeys; a passwordToo user already has one.
      passwordHint: lastProofs === proof.KIND_PASSWORD,
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
  //
  // The one thing that is NOT passed through unchanged is a file below
  // /assets/: there PHP stands in for a web server that has no application
  // behind it, so the walls of the virtual host go on here. See withAssetWalls.
  function proxy(req, res) {
    const out = httpRequest({
      host: "127.0.0.1", port: phpPort, method: req.method, path: req.url,
      headers: { ...req.headers, host: `127.0.0.1:${phpPort}` },
    }, (answer) => {
      res.writeHead(answer.statusCode, withAssetWalls(req.url, answer.headers));
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

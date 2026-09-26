// A finished link to the mock API, FOR THE TESTS AND FOR NOTHING ELSE.
//
// It signs the fixed test user in and puts the short forms the cases use on
// top of Link. There is no contract logic of its own in here: every document
// is built or read by the contract that owns it - session for the
// certificate, proof for the password key and the proof, sign for the
// signature and the key id, trust for the list, net/http for the seven paths.
// What it adds is the ORDER of those calls, which is what the login page
// (task 19) will add too.
//
// TWO THINGS IT DOES THAT THE REAL LOGIN WILL NOT, both named rather than
// hidden:
//
//  1. The trust list is taken from the chain with trust.parseList and NOT
//     opened against the anchor of /config.json (trust.open, task 20). So
//     the list is checked as a list - every field, every key id - but not
//     followed from the anchor. It is used for one thing here: the signing
//     key of the API node, so session.verifyAPI can do its real work on the
//     API certificate.
//  2. The socket is ws: where the page is http:. net/http.js's socketURL()
//     always says wss:, which is right in operation and impossible against a
//     plain test server. The address is still this page's own origin, so the
//     shell sees one origin for page, /api/ and socket, as it does in
//     operation.
import * as session from "../contracts/session.js";
import * as proof from "../contracts/proof.js";
import * as sign from "../contracts/sign.js";
import * as trust from "../contracts/trust.js";
import * as core from "../contracts/core.js";
import * as wire from "../contracts/wire.js";
import * as noise from "../contracts/noise.js";
import { b64encode, utf8 } from "../contracts/b64.js";
import * as http from "./http.js";
import { Link } from "./link.js";
import { call, openE2E } from "./channel.js";

// The test user of tests/mock/world.js. They are the same two lines there.
const USER = "anna";
const PASSWORD = "eichhoernchen-blau-42";

function socketURL() {
  const u = new URL(session.PATH_SOCKET, location.href);
  u.protocol = location.protocol === "https:" ? "wss:" : "ws:";
  return u.toString();
}

async function login(sessionDH) {
  const c = await http.loginStart(USER);
  if (c.kdf === null) throw new Error("testlink: this user has no password key");
  // The password key of the user: Argon2id over the typed password with the
  // parameters of the challenge, then the Ed25519 key of that seed.
  const seed = await proof.deriveSeed(PASSWORD, c.kdf);
  const userKey = await proof.privateFromSeed(seed);
  const userPub = await proof.publicFromSeed(seed);
  // The session's own signing key. It is what the certificate's "sig" field
  // is - the key that will sign tasks - and not a signature.
  const pair = await crypto.subtle.generateKey({ name: "Ed25519" }, false, ["sign", "verify"]);
  const sigPub = new Uint8Array(await crypto.subtle.exportKey("raw", pair.publicKey));
  const cert = session.encodeClientCert({
    inst: c.installation, user: c.user, nonce: c.nonce,
    sig: sigPub, dh: sessionDH.publicRaw,
    iat: c.now, exp: c.now + c.maxLifetime,
  });
  const signature = new Uint8Array(await crypto.subtle.sign({ name: "Ed25519" }, userKey,
    sign.message(sign.PURPOSE_CLIENT_CERT, cert)));
  const body = utf8(JSON.stringify({
    cert: b64encode(cert),
    proof: {
      kind: proof.KIND_PASSWORD, kid: await sign.keyID(userPub),
      sig: b64encode(signature), authData: "", clientData: "",
    },
  }));
  const result = await http.loginFinish(body);
  const chain = await http.trustChain(1);
  const list = await trust.parseList(chain.versions[0].payload);
  // The real check of the API's certificate: signed by an API node of the
  // list, bound to this very certificate, and valid now.
  const api = await session.verifyAPI(result.api, list, cert, c.now);
  return { api, list, taskKey: pair.privateKey };
}

class TestLink {
  constructor(link, world) {
    this.link = link;
    this.world = world;
  }

  channelCount() {
    return this.link.channelCount();
  }

  close() {
    this.link.close();
  }

  // call is the short form the desktop uses: app, service, action and the
  // parameters as a plain object. Everything below it is channel.call. The
  // fifth argument is for the cases only: it tells the mock how this one
  // channel should go wrong.
  async call(app, service, action, params, opts) {
    const a = core.lookup(app, service, action);
    if (a === null) throw new Error("testlink: no such action");
    if (opts !== undefined && opts !== null) await this.tellMock(opts);
    return call(this.link, a, { params: core.encodeParams(a, params) });
  }

  // openRaw opens a channel and stops after the first GRANT: no EOF, no
  // result. It is how a case holds several channels open at once.
  async openRaw(app, service, action) {
    const a = core.lookup(app, service, action);
    if (a === null) throw new Error("testlink: no such action");
    const ch = await this.link.open({
      app: a.app, service: a.service, action: a.name,
      params: core.encodeParams(a, null), window: wire.DEFAULT_WINDOW,
      e2e: null, taskBytes: null, confirmBytes: null,
    });
    await ch.firstGrant();
    return ch;
  }

  // openE2E opens a channel to an app. The controller is the node the trust
  // list names, and its Noise key comes from that list - never from the
  // answer of the API.
  async openE2E(app, service, action, params) {
    const node = trust.node(this.world.list, this.world.node);
    if (node === null) throw new Error("testlink: no such controller");
    return openE2E(this.link, {
      app, service, action,
      params: params === null || params === undefined ? null : utf8(JSON.stringify(params)),
      taskBytes: null, node: node.id, controllerKey: node.noise,
      sessionDH: this.world.sessionDH,
    });
  }

  // What the mock saw. It is a question for the OTHER process, so it is
  // asked and not read: a property could only ever carry an answer fetched
  // before the exchange it is about. The answer is one character, so there is
  // nothing to parse - and no JSON.parse on anything that came over a wire.
  async mockSawPlaintext() {
    const res = await fetch("/api/v1/mock", { cache: "no-store", credentials: "same-origin" });
    return (await res.text()) === "1";
  }

  // tellMock steers the next channel of this session: the mock's own test
  // door, so a case can ask for an answer that goes wrong after its data.
  // Nothing of net/link.js or net/channel.js knows it exists.
  async tellMock(opts) {
    const where = "/api/v1/mock?failAfterData=" + (opts.failAfterData === true ? "1" : "0");
    await fetch(where, { method: "POST", cache: "no-store", credentials: "same-origin" });
  }
}

export async function connectForTest() {
  const sessionDH = await noise.newKeypair();
  const { api, list } = await login(sessionDH);
  const link = await new Link().connect(socketURL(), sessionDH, api.dh);
  // The controller of the app the tests talk to: the one controller node of
  // this world.
  const controller = list.nodes.find((n) => n.kind === trust.NODE_CONTROLLER);
  return new TestLink(link, { list, sessionDH, node: controller?.id ?? "" });
}

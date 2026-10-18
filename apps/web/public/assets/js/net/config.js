// The configuration of this installation, which the page's own server writes:
// /config.json, built by public/index.php out of web.json (config.Web) and
// trust-anchor.json (trust.Anchor).
//
// It carries the addresses of the API servers, the pinned anchor of the trust
// list and the relying party for passkeys - where the shell connects, which
// trust list it believes, and which party a passkey is asked for. That is the
// root of all three, so it is read the way everything from outside is read:
// as bytes, through strict JSON with a budget, then field by field against
// its grammar, and only then used. "Our own server wrote it" is no exception;
// the rule in CLAUDE.md has none, and a file on a server is exactly what
// somebody who got that far would edit.
//
// Measured, so nobody looks for them here: the TLS pins are NOT in this
// answer. web.json holds apis[].tls, Config.php checks it and keeps it for
// Apache, which does the pinning; /config.json carries the addresses alone.
// The whole answer of the test installation is 228 bytes.
import { decode, Budget, S, P, A, str, bytes, uint } from "../contracts/json.js";
import { validVersion } from "../contracts/text.js";
import { validWebURL, validOrigin } from "../contracts/url.js";
import { validRPID } from "../contracts/proof.js";
import { MAX_ORIGINS } from "../contracts/trust.js";
import { readBounded } from "./http.js";

export const PATH_CONFIG = "/config.json";

// config: maxList in Go, Config::MAX_APIS in the PHP that writes the file.
export const MAX_APIS = 16;

// The largest configuration this grammar can still take: 16 addresses of up
// to 256 bytes each (4144 with their quotes and commas), 8 origins of up to
// 256 (2072), an rp id of 253, an anchor of about 70, a version of 20 and the
// member names - under 7 KiB all told. The bound is not there for our own
// file, which measured 228 bytes. It is there so an answer that is not our
// file is dropped while it arrives, instead of after it is all in memory.
export const MAX_CONFIG = 8 << 10;

// sha256.Size: the anchor hash is what trust.open compares a hash it computed
// itself against, so it has to be that many bytes and no other number.
const HASH_SIZE = 32;

// P and not S for the anchor: an installation whose web controller is not
// paired has none, and strict JSON has no null to say that with (Go:
// enc.ErrNull, and json.js refuses a null just the same), so the member is
// absent and P reads an absent member as null. index.php writes it that way
// for exactly this reason.
const anchorShape = P({ seq: uint, hash: bytes });
const rpShape = S({ id: str, origins: A(str) });
const configShape = S({ version: str, anchor: anchorShape, rp: rpShape, apis: A(str) });
// The items are the ones config.webBudget counts in Go, name for name. The
// size is not: Go bounds the FILE on disk with config.MaxConfig (1 MiB),
// while this bounds the answer to a browser, which holds far less.
const configBudget = new Budget({
  size: MAX_CONFIG,
  items: { apis: MAX_APIS, "rp/origins": MAX_ORIGINS },
});

// One error with one text for every way the configuration can be wrong. The
// shell shows the person the same sentence a missing installation gets: they
// must not be left guessing, and they must learn nothing about the shape
// either - a message that named the field would tell whoever edited the file
// how close they got.
export class BadConfig extends Error {
  constructor() {
    super("config: malformed");
    this.name = "BadConfig";
  }
}

// parseConfig reads the bytes of /config.json. It takes bytes and not a
// string, so json.decode settles them once (its own viewOf) and the budget
// and the parser both work on that one reading.
export function parseConfig(b) {
  let c;
  try {
    c = decode(b, configBudget, configShape);
  } catch {
    throw new BadConfig();
  }
  // index.php leaves the version empty rather than taking the page down over
  // an unreadable VERSION, so empty is the one other value it may have.
  if (c.version !== "" && !validVersion(c.version)) throw new BadConfig();
  // No anchor until the controller of the web server is paired. One that is
  // there is the root of the whole chain, so it is held to both its numbers.
  if (c.anchor !== null && (c.anchor.seq < 1 || c.anchor.hash.length !== HASH_SIZE)) {
    throw new BadConfig();
  }
  // Both or neither, the rule Config.php keeps on the way out. An id without
  // origins matches no browser, and origins without an id are a typo that has
  // to be as loud as a wrong value: otherwise a mistyped configuration looks
  // exactly like one that has no passkeys yet, and nobody ever finds out why
  // the passkeys are gone.
  const { id, origins } = c.rp;
  if (id === "") {
    if (origins.length !== 0) throw new BadConfig();
  } else {
    // rp.id is handed to navigator.credentials.get, so it is a DNS name by
    // proof.validRPID and never free text.
    if (!validRPID(id) || origins.length < 1 || origins.length > MAX_ORIGINS) throw new BadConfig();
    for (const o of origins) {
      if (!validOrigin(o)) throw new BadConfig();
    }
  }
  if (c.apis.length < 1 || c.apis.length > MAX_APIS) throw new BadConfig();
  const seen = new Set();
  for (const u of c.apis) {
    // trust.ValidWebURL and nothing else: this is the address the shell talks
    // to, and https with a port is the only shape of it. The same address
    // twice would have Apache balance over it twice and pin two keys for one
    // server, which is why Config.php refuses it on the way out and this
    // refuses it on the way in.
    if (!validWebURL(u) || seen.has(u)) throw new BadConfig();
    seen.add(u);
  }
  return c;
}

// loadConfig fetches the configuration and checks it. Everything that can go
// wrong answers the same BadConfig, so the shell has one sentence to show and
// the person cannot tell a missing installation from a damaged one - which is
// the point: the difference is nobody's business but the administrator's, and
// they have the file.
export async function loadConfig() {
  let res;
  try {
    res = await fetch(PATH_CONFIG, {
      cache: "no-store",
      credentials: "same-origin",
      mode: "same-origin",
      redirect: "error",
    });
  } catch {
    throw new BadConfig();
  }
  if (!res.ok) throw new BadConfig();
  // readBounded answers null past the bound and throws on a body that broke
  // off; neither is a configuration.
  const raw = await readBounded(res, MAX_CONFIG).catch(() => null);
  if (raw === null) throw new BadConfig();
  return parseConfig(raw);
}

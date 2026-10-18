// Package catalog: the store catalog, the app manifest and the update rows,
// exactly as Go reads them (store.VerifyCatalog, store.ParseManifest,
// store.ParseUpdateReport, store.ValidTarget, store.Granted, store.Approved in
// shared/store/catalog.go and shared/store/manifest.go; vectors: store.json).
//
// Everything here comes from a store, which is outside: a catalog is fetched
// over the network, a manifest comes out of a package somebody uploaded, an
// update row is sent by a controller. So nothing is read with JSON.parse and
// nothing is judged by its shape alone - every document passes the strict
// decoder with a budget of its own, and then the same checks Go makes.
import { BadInput, bytesOf, equal } from "./b64.js";
import { A, BadJSON, Budget, P, S, bool, bytes, decode, int, str, uint64 } from "./json.js";
import {
  byteLength, coreApp, newer, validAppID, validName, validPath, validText, validVersion,
} from "./text.js";
import { validHost, validHostPort } from "./url.js";
import { MAX_PAYLOAD, PURPOSE_CATALOG, verify } from "./sign.js";
import { AUTO_API, AUTO_CONTROLLER, AUTO_SYSTEM, AUTO_WEB } from "./trust.js";
import { MAX_MESSAGE_TEXT } from "./wire.js";

// Prices, limits and update states (Go: shared/store/catalog.go).
export const PRICE_BUY = "buy";
export const PRICE_SUBSCRIPTION = "subscription";
export const PER_SITE = "site";
export const PER_USER = "user";
export const PERIOD_MONTH = "month";
export const PERIOD_YEAR = "year";
// Go writes this as 4<<30. Here that would be 0: << works on 32 bits, and a
// bound of 0 refuses every package instead of bounding it at four gibibytes.
export const MAX_PACKAGE = 4 * 1024 * 1024 * 1024;
export const MAX_APPS = 10000;
export const MAX_VERSIONS = 100; // per app
// Of all apps together: each version costs up to about 750 bytes to decode,
// its app's share included.
export const MAX_CATALOG_VERSIONS = 30000;
export const MAX_LICENSE = 8192;

// How an installation takes updates (Go: ModeAuto, ModeSemi, ModeNotify).
export const MODE_AUTO = "auto"; // install in the maintenance window; needs a standing approval
export const MODE_SEMI = "semi"; // download and check in advance, install on click
export const MODE_NOTIFY = "notify"; // only tell; download and install on click

export const UPDATE_AVAILABLE = "available";
export const UPDATE_DOWNLOADED = "downloaded";
export const UPDATE_INSTALLING = "installing";
export const UPDATE_INSTALLED = "installed";
export const UPDATE_ROLLEDBACK = "rolledback";
export const UPDATE_FAILED = "failed";
export const UPDATE_REJECTED = "rejected"; // signature, approval or version check failed
// UPDATE_NEEDSGRANTS is a state of its own and not UPDATE_REJECTED: nothing is
// wrong with the update, the app may simply not do what it now needs, so the
// shell offers the activation instead of an error.
export const UPDATE_NEEDSGRANTS = "needsgrants";

// Names and limits of the manifest (Go: shared/store/manifest.go).
export const MAX_MANIFEST = 65536;
export const MAX_ACTIONS = 64; // actions per manifest
export const MAX_SERVICES = 16; // services per manifest
export const RUNTIME_PODMAN = "podman";
export const NEED_ROOT = "root"; // root inside the container
export const NEED_NETWORK = "network"; // access to the network

const MAX_COMMAND = 32; // path and arguments of a service
const MAX_NEEDS = 2; // NEED_ROOT and NEED_NETWORK, each at most once
const MAX_TIMEOUT = 3600; // Go: service.MaxTimeout
const MAX_REPORT = 4096;

// The states an update row may carry. A set, so a state the contract does not
// know is refused by the list itself and not by a chain of comparisons
// somebody can forget to extend.
const UPDATE_STATES = new Set([
  UPDATE_AVAILABLE, UPDATE_DOWNLOADED, UPDATE_INSTALLING, UPDATE_INSTALLED,
  UPDATE_ROLLEDBACK, UPDATE_FAILED, UPDATE_REJECTED, UPDATE_NEEDSGRANTS,
]);

const CURRENCY = /^[A-Z]{3}$/; // ISO 4217

// Everything that is wrong with a catalog, a manifest or an update row is this
// one error. Go has three (ErrCatalog, ErrManifest, ErrUpdate), and they are
// three because they name three documents, not three things a caller does
// differently: each of them means refuse. Which rule the document broke is
// never said, here or there - an error that repeats the input tells whoever
// sent it which of its tries came closest.
export class BadCatalog extends Error {}

// Rollback is the one refusal a caller CAN act on differently, and it is why
// it is a class of its own (Go: ErrRollback). The document is whole and really
// signed by the store; it is only older than the newest one already seen. That
// is not a broken catalog, it is a store replaying an old one, and an admin
// should read a different sentence for it. It extends BadCatalog, so whoever
// only wants "refused" catches one thing.
export class Rollback extends BadCatalog {}

// maxURL bounds an https address of a catalog. It is larger than the 256 of
// url.js, because a store signs download links and puts its token in the
// query.
const MAX_URL = 512;

// The characters the path and the query may not hold, besides everything
// outside 0x21 to 0x7e. Each of them would start a fragment or break out of a
// configuration file, a shell word or an HTML attribute.
const FORBIDDEN = "\"'<>\\#";

// validHTTPSURL reports whether u is an address NGC fetches a package from, or
// shows as a shop link: "https://", a host by validHost with an optional ":"
// and port (validHostPort), then nothing, or "/" and a path and query of
// printable ASCII without " ' < > \ and #; at most MAX_URL bytes
// (Go: store.validHTTPSURL).
//
// It follows the address grammar of url.js and calls it, so a host is judged
// the same everywhere. Unlike validStoreURL it allows a query, since stores
// sign download links, but it allows no fragment: NGC fetches these addresses,
// and a fragment says nothing to a server. User info cannot pass: it is part
// of the authority, and neither host rule allows "@".
function validHTTPSURL(u) {
  // Go counts bytes here. The grammar below admits nothing but ASCII, where a
  // UTF-16 unit is a byte, and every character that would make the two numbers
  // differ is refused a few lines further down - so a document can never reach
  // the length check with more bytes than units and pass.
  if (typeof u !== "string" || u.length > MAX_URL || !u.startsWith("https://")) return false;
  const rest = u.slice("https://".length);
  // A bracketed IPv6 host holds no "/", so the first one starts the path.
  const cut = rest.indexOf("/");
  const authority = cut < 0 ? rest : rest.slice(0, cut);
  if (!validHost(authority) && !validHostPort(authority)) return false;
  if (cut < 0) return true;
  const pathQuery = rest.slice(cut + 1);
  for (let i = 0; i < pathQuery.length; i++) {
    const c = pathQuery.charCodeAt(i);
    // 0x21 to 0x7e leaves out the control characters, the space and DEL; a
    // character outside ASCII has a unit above 0x7e and is refused here too.
    if (c < 0x21 || c > 0x7e || FORBIDDEN.includes(pathQuery[i])) return false;
  }
  return true;
}

// validURL reports whether u is somewhere a catalog may point at: an https
// address (validHTTPSURL) or a path beside the catalog (validPath).
function validURL(u) {
  if (typeof u !== "string") return false;
  if (u.startsWith("https://")) return validHTTPSURL(u);
  return validPath(u);
}

// validNeeds reports whether needs holds nothing but NEED_ROOT and
// NEED_NETWORK, each at most once (Go: store.validNeeds).
function validNeeds(needs) {
  for (let i = 0; i < needs.length; i++) {
    const n = needs[i];
    // indexOf answers with the FIRST place n stands at; below i means it
    // already stood earlier, which is Go's slices.Contains(needs[:i], n).
    if ((n !== NEED_ROOT && n !== NEED_NETWORK) || needs.indexOf(n) < i) return false;
  }
  return true;
}

// ---- the catalog ---------------------------------------------------------

const priceShape = P({ model: str, per: str, amount: int, currency: str, period: str, shop: str });
const packageShape = S({
  version: str, url: str, size: int, sha256: bytes, ui: bytes,
  encrypted: bool, needs: A(str), minNgc: str,
});
const entryShape = S({
  id: str, name: str, publisher: str, description: str, price: priceShape, versions: A(packageShape),
});

// catalogShape is what a catalog decodes into (Go: store.Catalog).
export const catalogShape = S({ store: str, seq: uint64, issued: int, apps: A(entryShape) });

// catalogBudget bounds a catalog while it is decoded. The per-app limits alone
// would allow MAX_APPS*MAX_VERSIONS versions; the total keeps what any catalog
// within it costs to decode below 32 MiB (Go: TestCatalogDecodeCost).
const catalogBudget = new Budget({
  size: MAX_PAYLOAD,
  items: { apps: MAX_APPS, "apps/*/versions": MAX_VERSIONS, "apps/*/versions/*/needs": MAX_NEEDS },
  totals: { "apps/*/versions": MAX_CATALOG_VERSIONS },
});

function validPrice(p) {
  const sub = p.model === PRICE_SUBSCRIPTION;
  return (p.model === PRICE_BUY || sub) && (p.per === PER_SITE || p.per === PER_USER) && p.amount > 0
    && CURRENCY.test(p.currency) && sub === (p.period !== "")
    && (!sub || p.period === PERIOD_MONTH || p.period === PERIOD_YEAR)
    && p.shop.startsWith("https://") && validURL(p.shop);
}

function validEntry(e) {
  // The decoder has already proved the whole document is valid UTF-8, so
  // Go's utf8.ValidString(e.Description) is answered before this runs; what is
  // left is its bound, and that one counts BYTES, not UTF-16 units.
  if (!validAppID(e.id) || coreApp(e.id) || !validText(e.name, 64) || !validText(e.publisher, 64)
    || byteLength(e.description) > 1000 || e.versions.length === 0 || e.versions.length > MAX_VERSIONS
    || (e.price !== null && !validPrice(e.price))) {
    return false;
  }
  for (let i = 0; i < e.versions.length; i++) {
    const p = e.versions[i];
    // Ascending as VERSIONS and not as text: 1.9.0 is older than 1.10.0, and a
    // comparison of strings has it the other way round.
    if (!validVersion(p.version) || (i > 0 && !newer(p.version, e.versions[i - 1].version))
      || !validURL(p.url) || p.size <= 0 || p.size > MAX_PACKAGE
      || p.sha256.length !== 32 || p.ui.length !== 32
      // A package is encrypted exactly where the app costs money, and the two
      // are tied both ways: a free app whose package is encrypted has a
      // content key nobody can ever be given, and a paid one whose package is
      // plain is the paid app handed out for nothing.
      || p.encrypted !== (e.price !== null) || !validNeeds(p.needs)
      || (p.minNgc !== "" && !validVersion(p.minNgc))) {
      return false;
    }
  }
  return true;
}

function validCatalog(c) {
  if (!validName(c.store) || c.seq === 0 || c.issued <= 0 || c.apps.length > MAX_APPS) return false;
  let versions = 0;
  for (let i = 0; i < c.apps.length; i++) {
    const e = c.apps[i];
    versions += e.versions.length;
    // Sorted by id and without a double, so one app cannot stand twice with
    // two prices and leave it to whoever reads the list which one holds.
    if ((i > 0 && c.apps[i - 1].id >= e.id) || !validEntry(e) || versions > MAX_CATALOG_VERSIONS) {
      return false;
    }
  }
  return true;
}

function parseCatalog(b) {
  let c;
  try {
    c = decode(b, catalogBudget, catalogShape);
  } catch (e) {
    if (e instanceof BadJSON || e instanceof BadInput) throw new BadCatalog("store: malformed catalog");
    throw e;
  }
  if (!validCatalog(c)) throw new BadCatalog("store: malformed catalog");
  return c;
}

// verifyCatalog checks the signature of the store st and refuses catalogs
// older than lastSeq, the newest one seen from this store (Go:
// store.VerifyCatalog).
export async function verifyCatalog(env, st, lastSeq) {
  if (env === null || typeof env !== "object" || st === null || typeof st !== "object") {
    throw new BadCatalog("store: malformed catalog");
  }
  // Go is handed a uint64 here. A caller can hand over anything, and anything
  // that is no whole number compares FALSE against every seq: the rollback
  // check at the end would quietly stop checking and every replay come
  // through. A bound that cannot decide refuses instead of waving through.
  if (!Number.isSafeInteger(lastSeq) || lastSeq < 0) throw new BadCatalog("store: no sequence number");
  // Each field of the caller's two objects is read ONCE, here.
  const { payload, kid, sig } = env;
  const { id, key } = st;
  // AND THE PAYLOAD IS COPIED BEFORE ANYTHING IS DECIDED ABOUT IT. This is the
  // whole point of the function: the signature says that these bytes came from
  // the store, and the apps, versions and download addresses that come out of
  // the parser have to be read from the very same bytes. Handing env.payload
  // to the signature check and then reading env.payload again for the parser
  // is two readings of an object the caller owns, and an object may answer the
  // second one with something else - checked would be the store's catalog,
  // installed would be the attacker's. b64.bytesOf copies out of the internal
  // slots, where no getter can be installed, so what is verified below and
  // what is parsed below are the same bytes by construction.
  let load, signature;
  try {
    load = bytesOf(payload, "store: payload is not bytes");
    signature = bytesOf(sig, "store: signature is not bytes");
  } catch (e) {
    if (e instanceof BadInput) throw new BadCatalog("store: malformed catalog");
    throw e;
  }
  // An envelope of our own, built from the copies: sign.verify reads the three
  // fields off the object it is handed, and this one answers every reading
  // with the same bytes. The key goes in as it came - verify copies it itself
  // and decides on that copy - and it is read nowhere else here.
  if (!(await verify({ payload: load, kid, sig: signature }, key, PURPOSE_CATALOG))) {
    throw new BadCatalog("store: bad signature");
  }
  const c = parseCatalog(load);
  // A catalog carries the store it belongs to, and it is checked against the
  // store it was fetched for: without this, a catalog one store signed would
  // be taken as another's the moment its key is in the trust list too.
  if (c.store !== id) throw new BadCatalog("store: catalog of another store");
  // An older version nobody plays back: it would offer the version whose
  // weakness the newer catalog exists to replace.
  if (c.seq < lastSeq) throw new Rollback("store: older than the last one seen");
  return c;
}

// ---- the manifest --------------------------------------------------------

const uiShape = P({ entry: str, width: int, height: int });
const actionShape = S({ name: str, title: str, service: str, stream: bool, timeout: int, critical: bool });
const serviceShape = S({ name: str, command: A(str) });

// manifestShape is what manifest.json decodes into (Go: store.Manifest).
export const manifestShape = S({
  id: str, name: str, version: str, publisher: str, description: str, icon: str, ui: uiShape,
  actions: A(actionShape), services: A(serviceShape), start: str, runtime: str, needs: A(str),
  cpu: int, memory: int, minNgc: str,
});

// manifestBudget bounds a manifest while it is decoded.
const manifestBudget = new Budget({
  size: MAX_MANIFEST,
  items: {
    actions: MAX_ACTIONS, services: MAX_SERVICES, "services/*/command": MAX_COMMAND, needs: MAX_NEEDS,
  },
});

// size reports whether v is a window measurement a manifest may ask for: none
// at all, or 200 to 4000 (Go: store.size).
const size = (v) => v === 0 || (v >= 200 && v <= 4000);

function validManifest(m) {
  if (!validAppID(m.id) || coreApp(m.id) || !validText(m.name, 64) || !validText(m.publisher, 64)
    || !validVersion(m.version) || byteLength(m.description) > 1000
    || (m.icon !== "" && !validPath(m.icon)) || (m.minNgc !== "" && !validVersion(m.minNgc))) {
    return false;
  }
  // An app is a window, a set of services, or both. Neither is nothing an
  // admin could ever start.
  if (m.ui === null && m.services.length === 0) return false;
  if (m.ui !== null && (!validPath(m.ui.entry) || !size(m.ui.width) || !size(m.ui.height))) return false;
  if (m.actions.length > MAX_ACTIONS || m.services.length > MAX_SERVICES) return false;
  const services = new Set();
  for (const s of m.services) {
    // The first word is the program, and it is absolute: a relative one would
    // be looked up in a PATH the package may write.
    if (!validName(s.name) || services.has(s.name) || s.command.length === 0
      || s.command.length > MAX_COMMAND
      || byteLength(s.command[0]) < 2 || !s.command[0].startsWith("/")) {
      return false;
    }
    for (const arg of s.command) {
      // A NUL would end the argument where the kernel reads it and leave the
      // rest of it standing where NGC reads it: two different commands.
      if (arg === "" || byteLength(arg) > 256 || arg.includes("\u0000")) return false;
    }
    services.add(s.name);
  }
  const actions = new Set();
  for (const a of m.actions) {
    if (!validName(a.name) || actions.has(a.name) || !validText(a.title, 100) || !services.has(a.service)
      // A stream stays open, so a time limit on it would be a limit nobody
      // means; the two are never set together.
      || a.timeout < 0 || a.timeout > MAX_TIMEOUT || (a.stream && a.timeout !== 0)) {
      return false;
    }
    actions.add(a.name);
  }
  if (m.start !== "") {
    const a = m.actions.find((x) => x.name === m.start);
    if (a === undefined || a.stream) return false;
  }
  if (m.runtime !== "" && m.runtime !== RUNTIME_PODMAN) return false;
  if (!validNeeds(m.needs)) return false;
  if ((m.cpu !== 0 && (m.cpu < 100 || m.cpu > 64000))
    || (m.memory !== 0 && (m.memory < 16 || m.memory > 65536))) {
    return false;
  }
  return true;
}

// parseManifest reads and checks manifest.json (Go: store.ParseManifest).
export function parseManifest(b) {
  let m;
  try {
    m = decode(b, manifestBudget, manifestShape);
  } catch (e) {
    if (e instanceof BadJSON || e instanceof BadInput) throw new BadCatalog("store: malformed manifest");
    throw e;
  }
  if (!validManifest(m)) throw new BadCatalog("store: malformed manifest");
  return m;
}

// ---- update rows ---------------------------------------------------------

const reportShape = S({ target: str, version: str, state: str, message: str });

// reportBudget bounds an update row while it is decoded: it has no array.
const reportBudget = new Budget({ size: MAX_REPORT });

// validTarget reports whether t names something that can be updated: an NGC
// part, the system packages or an app (Go: store.ValidTarget).
export const validTarget = (t) => t === AUTO_WEB || t === AUTO_API || t === AUTO_CONTROLLER
  || t === AUTO_SYSTEM || (validAppID(t) && !coreApp(t));

// parseUpdateReport reads and checks an update row a controller sent (Go:
// store.ParseUpdateReport).
export function parseUpdateReport(b) {
  let r;
  try {
    r = decode(b, reportBudget, reportShape);
  } catch (e) {
    if (e instanceof BadJSON || e instanceof BadInput) throw new BadCatalog("store: malformed update message");
    throw e;
  }
  if (!UPDATE_STATES.has(r.state) || !validTarget(r.target)
    || (r.version !== "" && !validVersion(r.version))
    || (r.message !== "" && !validText(r.message, MAX_MESSAGE_TEXT))) {
    throw new BadCatalog("store: malformed update message");
  }
  return r;
}

// granted reports whether the grants of the activation cover the needs of a
// new version; an update never gets more rights than the app was given.
export const granted = (needs, grants) => needs.every((n) => grants.includes(n));

// approved reports whether a standing approval in l lets updates for target,
// signed with key, install without a fresh signature of a user (Go:
// store.Approved).
export function approved(l, target, key) {
  // An approval is a right, so everything that is not plainly an approval is
  // none: a list without its auto entries answers no, never yes by accident.
  if (l === null || typeof l !== "object" || !Array.isArray(l.auto)) return false;
  // The key is copied ONCE, before a single entry is looked at, and every
  // comparison below runs against that copy. Without it b64.equal views the
  // caller's object again for every entry of the list, and the length that
  // gates the comparison belongs to a reading of its own: an object that
  // answers each reading differently would have its length checked against one
  // key and its bytes compared against another. What is checked here is what
  // is compared.
  let k = null;
  if (key instanceof Uint8Array) {
    try {
      k = bytesOf(key, "store: key is not bytes");
    } catch {
      return false;
    }
  }
  for (const a of l.auto) {
    if (a === null || typeof a !== "object" || a.target !== target) continue;
    // AUTO_SYSTEM carries no key: those packages come from the distribution,
    // and there is no store signature to compare against.
    if (target === AUTO_SYSTEM) return true;
    // A key of no bytes may never match. Every other target's entry carries 32
    // bytes (trust.validateList), so an empty key would compare against
    // nothing - but the rule stands here as well, because it is what says an
    // approval is never given away by leaving the key out.
    if (k !== null && k.length > 0 && equal(a.key, k)) return true;
  }
  return false;
}

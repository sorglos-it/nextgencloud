// Package core: the actions of the built-in apps - what the shell asks the
// API for, and the root tasks the API forwards to a controller - with their
// parameters, their results and the signed evidence for an app's user
// interface (Go: core).
//
// Everything here that comes from outside is read the way Go reads it
// (enc.DecodeStrict with an enc.Budget): through json.decode, with a budget,
// and then field by field. Nothing from the API is believed because it looks
// right; a result the shell would not have accepted from a stranger it does
// not accept from its own API either, because a compromised API is exactly
// the case these checks exist for.
import { b64encode, bytesOf, utf8, BadInput } from "./b64.js";
import { decode, Budget, BadJSON, S, A, str, bool, int, uint, bytes, P } from "./json.js";
import {
  validAppID, coreApp, validLogID, validLoginName, validName, validPath, validText, validTOTP, validVersion, validID,
} from "./text.js";
import { validStoreURL, validTunnelURL } from "./url.js";
import * as proof from "./proof.js";
import { envelopeShape, validateEnvelope, MAX_PAYLOAD } from "./sign.js";
import * as trust from "./trust.js";
import * as session from "./session.js";
import * as task from "./task.js";
import {
  MAX_ACTIONS, MAX_APPS, MAX_CATALOG_VERSIONS, MODE_AUTO, MODE_NOTIFY, MODE_SEMI,
  NEED_NETWORK, NEED_ROOT, UPDATE_AVAILABLE, UPDATE_DOWNLOADED, UPDATE_FAILED, UPDATE_INSTALLED,
  UPDATE_INSTALLING, UPDATE_NEEDSGRANTS, UPDATE_REJECTED, UPDATE_ROLLEDBACK,
  validTarget, verifyCatalog,
} from "./catalog.js";
import { MAX_CALL_RESULT, MAX_MESSAGE_TEXT, MAX_PARAMS, CODES } from "./wire.js";

// Core apps, services, rights.
export const APP_DESKTOP = "ngc.desktop";
export const APP_USERS = "ngc.users";
export const APP_STORE = "ngc.store";

export const SERVICE_API = "api";
export const SERVICE_CONTROLLER = "controller";

export const RIGHT_USE = "use";
// LOCAL_STORE is the store id of apps an admin uploaded. It is trust's own
// name, not a second copy of the text: two places holding "local" are two
// places that can drift apart.
export const LOCAL_STORE = trust.STORE_LOCAL;

// Action names.
export const ACT_ME = "me";
export const ACT_UI_PROOF = "ui-proof";
export const ACT_APPS = "apps";
export const ACT_UI = "ui";
export const ACT_TOTP_BEGIN = "totp-begin";
export const ACT_TOTP_CONFIRM = "totp-confirm";
export const ACT_TOTP_OFF = "totp-off";

export const ACT_LIST = "list";
export const ACT_RIGHTS = "rights";
export const ACT_SET_RIGHTS = "set-rights";
export const ACT_TRUST = "trust";
export const ACT_PAIR_CODE = "pair-code";
export const ACT_JOIN_CODE = "join-code";
export const ACT_PENDING = "pending";
export const ACT_PENDING_API = "pending-api";
export const ACT_NODES = "nodes";
export const ACT_LOG = "log";

export const ACT_SOURCES = "sources";
export const ACT_CATALOG = "catalog";
export const ACT_INSTALLED = "installed";
export const ACT_UPLOAD = "upload";
export const ACT_LICENSE = "license";
export const ACT_INSTALL_KEY = "install-key";
export const ACT_UPDATES = "updates";
export const ACT_SET_UPDATE = "set-update";

export const ACT_ACTIVATE = "activate";
export const ACT_DEACTIVATE = "deactivate";
export const ACT_UNINSTALL = "uninstall";
export const ACT_DELETE_DATA = "delete-data";
export const ACT_STAGE = "stage";
export const ACT_INSTALL = "install";

// MAX_NOTICE limits the body of a "notify" control message, and
// MAX_TRANSFER_HEADER the JSON header of a file the API sends a controller
// (Go: MaxNotice, MaxTransferHeader).
export const MAX_NOTICE = 1024;
export const MAX_TRANSFER_HEADER = 8388608;

// How data moves on the channel of an action (Go: Flow). The shell compares
// against ACTIONS, never against a text of its own, so these stay inside the
// module: one name for each flow, in one place.
const FLOW_CALL = "call";
const FLOW_IN = "in";
const FLOW_OUT = "out";
const FLOW_TRANSFER = "transfer";

// What the opener of a FlowIn channel may send, and what the API sends a
// controller on a FlowTransfer channel: the length of the header, the header,
// the file (Go: MaxTrustIn, MaxLicenseIn, store.MaxPackage, session.MaxChain,
// maxTransferIn).
//
// MAX_TRANSFER_IN belongs to the action, not to whoever opened the channel.
// The shell sends nothing on a transfer channel even when it opened one
// itself - it sends EOF at once, and the API writes the header and the file
// on the tunnel hop - so 0 would read as the shell's own bound and not as the
// contract's. The task brief had 0 in both transfer rows; shared/core/core.go
// is frozen and has maxTransferIn, and the frozen side is what counts.
const MAX_TRUST_IN = 6 << 20;
const MAX_LICENSE_IN = 16 << 10;
const MAX_PACKAGE = 4 * 1024 * 1024 * 1024;
const MAX_TRANSFER_IN = 4 + MAX_TRANSFER_HEADER + MAX_PACKAGE;

// The actions of the built-in apps: what the shell asks the API for, and the
// root tasks the API forwards to a controller (Go: core).
//
// Frozen, rows and all. Go hands out a copy of its table (core.All); here
// every consumer sees this one array, and whoever else runs on the page could
// otherwise add a row - an action nobody wrote - or change the admin flag of
// one that stands here. Freezing is also what keeps the rows honest while
// they are walked: sort, reverse and push throw instead of rearranging the
// contract.
export const ACTIONS = Object.freeze([
  // app, service, name, admin, root, changes, flow, maxIn
  [APP_DESKTOP, SERVICE_API, ACT_ME, false, false, false, FLOW_CALL, 0],
  [APP_DESKTOP, SERVICE_API, ACT_UI_PROOF, false, false, false, FLOW_CALL, 0],
  [APP_DESKTOP, SERVICE_API, ACT_APPS, false, false, false, FLOW_CALL, 0],
  [APP_DESKTOP, SERVICE_API, ACT_UI, false, false, false, FLOW_OUT, 0],
  [APP_DESKTOP, SERVICE_API, ACT_TOTP_BEGIN, false, false, true, FLOW_CALL, 0],
  [APP_DESKTOP, SERVICE_API, ACT_TOTP_CONFIRM, false, false, true, FLOW_CALL, 0],
  [APP_DESKTOP, SERVICE_API, ACT_TOTP_OFF, false, false, true, FLOW_CALL, 0],
  [APP_USERS, SERVICE_API, ACT_LIST, true, false, false, FLOW_CALL, 0],
  [APP_USERS, SERVICE_API, ACT_RIGHTS, true, false, false, FLOW_CALL, 0],
  [APP_USERS, SERVICE_API, ACT_SET_RIGHTS, true, false, true, FLOW_CALL, 0],
  [APP_USERS, SERVICE_API, ACT_TRUST, true, false, true, FLOW_IN, MAX_TRUST_IN],
  [APP_USERS, SERVICE_API, ACT_PAIR_CODE, true, false, true, FLOW_CALL, 0],
  [APP_USERS, SERVICE_API, ACT_JOIN_CODE, true, false, true, FLOW_CALL, 0],
  [APP_USERS, SERVICE_API, ACT_PENDING, true, false, false, FLOW_CALL, 0],
  [APP_USERS, SERVICE_API, ACT_PENDING_API, true, false, false, FLOW_CALL, 0],
  [APP_USERS, SERVICE_API, ACT_NODES, true, false, false, FLOW_CALL, 0],
  [APP_USERS, SERVICE_API, ACT_LOG, true, false, false, FLOW_CALL, 0],
  [APP_USERS, SERVICE_CONTROLLER, ACT_TRUST, true, false, true, FLOW_IN, session.MAX_CHAIN],
  [APP_STORE, SERVICE_API, ACT_SOURCES, true, false, false, FLOW_CALL, 0],
  [APP_STORE, SERVICE_API, ACT_CATALOG, true, false, false, FLOW_CALL, 0],
  [APP_STORE, SERVICE_API, ACT_INSTALLED, true, false, false, FLOW_CALL, 0],
  [APP_STORE, SERVICE_API, ACT_UPLOAD, true, false, true, FLOW_IN, MAX_PACKAGE],
  [APP_STORE, SERVICE_API, ACT_LICENSE, true, false, true, FLOW_IN, MAX_LICENSE_IN],
  [APP_STORE, SERVICE_API, ACT_INSTALL_KEY, true, false, false, FLOW_CALL, 0],
  [APP_STORE, SERVICE_API, ACT_UPDATES, true, false, false, FLOW_CALL, 0],
  [APP_STORE, SERVICE_API, ACT_SET_UPDATE, true, false, true, FLOW_CALL, 0],
  [APP_STORE, SERVICE_CONTROLLER, ACT_ACTIVATE, true, true, true, FLOW_TRANSFER, MAX_TRANSFER_IN],
  [APP_STORE, SERVICE_CONTROLLER, ACT_DEACTIVATE, true, true, true, FLOW_CALL, 0],
  [APP_STORE, SERVICE_CONTROLLER, ACT_UNINSTALL, true, true, true, FLOW_CALL, 0],
  [APP_STORE, SERVICE_CONTROLLER, ACT_DELETE_DATA, true, true, true, FLOW_CALL, 0],
  [APP_STORE, SERVICE_CONTROLLER, ACT_STAGE, true, false, false, FLOW_TRANSFER, MAX_TRANSFER_IN],
  [APP_STORE, SERVICE_CONTROLLER, ACT_INSTALL, true, true, true, FLOW_CALL, 0],
  [APP_STORE, SERVICE_CONTROLLER, ACT_LICENSE, true, false, true, FLOW_IN, MAX_LICENSE_IN],
].map(([app, service, name, admin, root, changes, flow, maxIn]) =>
  Object.freeze({ app, service, name, admin, root, changes, flow, maxIn })));

// Lookup finds a core action. Go is handed three strings; here a caller may
// hand over anything at all, and an action is only ever found for the three
// texts that name one.
export const lookup = (app, service, name) =>
  ACTIONS.find((a) => a.app === app && a.service === service && a.name === name) || null;

// lookupByKey finds an action by the key core.json uses ("ngc.desktop/apps"):
// among the actions of that app with that name, the one of service api, else
// the one of service controller.
export const lookupByKey = (app, name) =>
  lookup(app, SERVICE_API, name) || lookup(app, SERVICE_CONTROLLER, name);

// The shell opens every action of service api, and the root tasks of service
// controller; stage, license and ngc.users/controller/trust only the API opens,
// without a user, and refuses a shell's OPEN for them with E_FORBIDDEN.
//
// Go takes an Action by value and can be handed nothing else. Here a caller
// may pass null, a half-built object or a row somebody made up, so the two
// fields are read through optional chaining and root has to be the boolean
// the table holds: a truthy value that is no boolean is no root task, and
// answering true for one would open a channel the API refuses anyway.
export const shellOpens = (a) => a?.service === SERVICE_API || a?.root === true;

// critical decides whether an action needs re-confirmation at level
// "critical"; the answer comes from the signed params, never from the OPEN.
//
// ONE ANSWER HERE IS NOT GO'S, DELIBERATELY. Go switches on the TYPE of the
// params, so with no params at all it reaches its default and says false for
// every action but upload and delete-data. This switches on the action - there
// are no types to switch on - and an uninstall without params therefore
// answers TRUE where Go answers false. Everything else matches.
//
// It was kept on purpose and not made to match, and the reason is that Go
// never gets there: RuleFor refuses a params-less activate or uninstall
// outright (core.paramsDecide), so Go's false is the answer to a question its
// own contract does not ask. An uninstall whose keepData nobody named is one
// that deletes the app's data, and that is what "critical" is for. The price
// is one re-confirmation too many on a path that should not exist.
//
// core.spec.js "shellOpens and critical decide for every row as Go decides"
// pins it: it computes Go's rule over Go's own table and names uninstall as
// the single row that answers otherwise, so the difference cannot spread to a
// second row unnoticed.
export function critical(a, params) {
  if (a?.name === ACT_ACTIVATE) return (params?.grants?.length ?? 0) > 0;
  if (a?.name === ACT_UNINSTALL) return !params?.keepData;
  return a?.app === APP_STORE && (a?.name === ACT_UPLOAD || a?.name === ACT_DELETE_DATA);
}

// validRight reports whether r is "<app>/use" or "<app>/<action>" for an app
// from a store (Go: ValidRight).
export function validRight(r) {
  if (typeof r !== "string") return false;
  const cut = r.indexOf("/");
  if (cut < 0) return false;
  const app = r.slice(0, cut), action = r.slice(cut + 1);
  return validApp(app) && (action === RIGHT_USE || validName(action));
}

// validApp is Go's validApp: an app id that is not one of NGC's own.
const validApp = (app) => validAppID(app) && !coreApp(app);

// ---------------------------------------------------------------- parameters

// Limits of the parameters that the checks below hold, besides MAX_PARAMS.
const MAX_RIGHTS = 1000; // RoleRights.rights
const MAX_GRANTS = 2; // ActivateParams.grants: NEED_ROOT and NEED_NETWORK, each at most once
const MIN_LOG_LIMIT = 1, MAX_LOG_LIMIT = 500;

// paramsBudget bounds the params of every action while they are decoded:
// only RoleRights and ActivateParams have an array (Go: core.paramsBudget).
const paramsBudget = new Budget({ size: MAX_PARAMS, items: { rights: MAX_RIGHTS, grants: MAX_GRANTS } });

// The maintenance window of UpdateSetting: local time "HH:MM-HH:MM".
const WINDOW = /^([01][0-9]|2[0-3]):[0-5][0-9]-([01][0-9]|2[0-3]):[0-5][0-9]$/;

// A parameter kind is three things that belong together: the shape a document
// decodes into, one reading of a caller's object (take), the rules that
// decide it (check) and what Go's json.Marshal writes for it (write). take
// comes first everywhere, so what is checked is what is written - a caller's
// bytes are copied there and never looked at again through the caller's
// object.
const KIND_APP = {
  shape: S({ app: str }),
  take: (p) => ({ app: p.app }),
  check: (p) => validApp(p.app),
  write: (p) => ({ app: p.app }),
};

const KIND_CODE = {
  shape: S({ code: str }),
  take: (p) => ({ code: p.code }),
  check: (p) => validTOTP(p.code),
  write: (p) => ({ code: p.code }),
};

const KIND_STORE = {
  shape: S({ store: str }),
  take: (p) => ({ store: p.store }),
  check: (p) => validName(p.store),
  write: (p) => ({ store: p.store }),
};

const KIND_ROLE_RIGHTS = {
  shape: S({ role: str, rights: A(str) }),
  take: (p) => ({ role: p.role, rights: list(p.rights) }),
  check: (p) => validName(p.role) && p.rights !== null && p.rights.length <= MAX_RIGHTS
    && noDuplicate(p.rights) && p.rights.every(validRight),
  write: (p) => {
    const o = { role: p.role };
    if (p.rights.length > 0) o.rights = p.rights;
    return o;
  },
};

const KIND_LOG_QUERY = {
  shape: S({ beforeTime: int, beforeId: str, limit: int, user: str }),
  take: (p) => ({ beforeTime: p.beforeTime ?? 0, beforeId: p.beforeId ?? "", limit: p.limit, user: p.user ?? "" }),
  check: (p) => whole(p.limit) && p.limit >= MIN_LOG_LIMIT && p.limit <= MAX_LOG_LIMIT
    && (p.user === "" || validID(p.user)) && whole(p.beforeTime) && p.beforeTime >= 0
    && (p.beforeId === "" || (validLogID(p.beforeId) && p.beforeTime > 0)),
  write: (p) => {
    const o = {};
    if (p.beforeTime !== 0) o.beforeTime = p.beforeTime;
    if (p.beforeId !== "") o.beforeId = p.beforeId;
    o.limit = p.limit;
    if (p.user !== "") o.user = p.user;
    return o;
  },
};

const KIND_UPDATE_SETTING = {
  shape: S({ target: str, mode: str, window: str }),
  take: (p) => ({ target: p.target, mode: p.mode, window: p.window ?? "" }),
  check: (p) => validTarget(p.target) && MODES.has(p.mode) && (p.window === "" || WINDOW.test(p.window)),
  write: (p) => {
    const o = { target: p.target, mode: p.mode };
    if (p.window !== "") o.window = p.window;
    return o;
  },
};

const KIND_ACTIVATE = {
  shape: S({ app: str, version: str, sha256: bytes, ui: bytes, store: str, grants: A(str) }),
  take: (p) => ({
    app: p.app, version: p.version, sha256: copy(p.sha256), ui: copy(p.ui),
    store: p.store, grants: list(p.grants),
  }),
  check: (p) => validApp(p.app) && validVersion(p.version) && p.sha256 !== null && p.sha256.length === HASH_SIZE
    && p.ui !== null && p.ui.length === HASH_SIZE && validName(p.store)
    && p.grants !== null && p.grants.length <= MAX_GRANTS && noDuplicate(p.grants)
    && p.grants.every((g) => g === NEED_ROOT || g === NEED_NETWORK),
  write: (p) => {
    const o = { app: p.app, version: p.version, sha256: b64encode(p.sha256), ui: b64encode(p.ui), store: p.store };
    if (p.grants.length > 0) o.grants = p.grants;
    return o;
  },
};

const KIND_UNINSTALL = {
  shape: S({ app: str, keepData: bool }),
  take: (p) => ({ app: p.app, keepData: p.keepData === true }),
  check: (p) => validApp(p.app),
  write: (p) => {
    const o = { app: p.app };
    if (p.keepData) o.keepData = true;
    return o;
  },
};

const KIND_STAGE = {
  shape: S({ target: str, version: str }),
  take: (p) => ({ target: p.target, version: p.version }),
  check: (p) => validTarget(p.target) && p.target !== trust.AUTO_SYSTEM && validVersion(p.version),
  write: (p) => ({ target: p.target, version: p.version }),
};

const KIND_INSTALL = {
  shape: S({ target: str, version: str, sha256: bytes }),
  take: (p) => ({ target: p.target, version: p.version ?? "", sha256: copy(p.sha256 ?? EMPTY) }),
  // For the system there is nothing to name: the controller runs apt, and a
  // version or a checksum beside it would be a promise nobody can keep.
  check: (p) => (p.sha256 !== null) && (p.target === trust.AUTO_SYSTEM
    ? p.version === "" && p.sha256.length === 0
    : validTarget(p.target) && validVersion(p.version) && p.sha256.length === HASH_SIZE),
  write: (p) => {
    const o = { target: p.target };
    if (p.version !== "") o.version = p.version;
    if (p.sha256.length > 0) o.sha256 = b64encode(p.sha256);
    return o;
  },
};

// Which action takes which parameters (Go: the Params column of core.actions).
// The key is app/service/name, because ngc.store/api/license takes none and
// ngc.store/controller/license takes an app.
const PARAMS_OF = {
  __proto__: null,
  "ngc.desktop/api/ui-proof": KIND_APP,
  "ngc.desktop/api/ui": KIND_APP,
  "ngc.desktop/api/totp-confirm": KIND_CODE,
  "ngc.desktop/api/totp-off": KIND_CODE,
  "ngc.users/api/set-rights": KIND_ROLE_RIGHTS,
  "ngc.users/api/log": KIND_LOG_QUERY,
  "ngc.store/api/catalog": KIND_STORE,
  "ngc.store/api/set-update": KIND_UPDATE_SETTING,
  "ngc.store/controller/activate": KIND_ACTIVATE,
  "ngc.store/controller/deactivate": KIND_APP,
  "ngc.store/controller/uninstall": KIND_UNINSTALL,
  "ngc.store/controller/delete-data": KIND_APP,
  "ngc.store/controller/stage": KIND_STAGE,
  "ngc.store/controller/install": KIND_INSTALL,
  "ngc.store/controller/license": KIND_APP,
};

// kindOf answers with the parameter kind of a, null for an action that takes
// none and undefined for anything that is no action of the table. A caller
// who hands over a row nobody wrote gets no parameters checked against a rule
// that was never meant for it.
function kindOf(a) {
  const found = lookup(a?.app, a?.service, a?.name);
  if (found === null) return undefined;
  return PARAMS_OF[`${found.app}/${found.service}/${found.name}`] ?? null;
}

// validateParams checks the parameters of an action (Go: the Validate methods
// and ParseParams). It answers true or false and never throws: a caller who
// had to catch an exception around a check is a caller who lets one through
// the moment the object surprises him.
export function validateParams(a, params) {
  const kind = kindOf(a);
  if (kind === undefined) return false;
  try {
    // An action without parameters takes nothing but an empty object, as Go's
    // ParseParams takes an empty value or an empty object and nothing else.
    if (kind === null) {
      return params === null || params === undefined
        || (typeof params === "object" && !Array.isArray(params) && Object.keys(params).length === 0);
    }
    if (params === null || typeof params !== "object") return false;
    return kind.check(kind.take(params));
  } catch {
    return false;
  }
}

// encodeParams writes the parameters of an action as Go's json.Marshal writes
// them - these are the bytes the session key signs inside a task, so they are
// built from ONE reading of the caller's object: take copies every byte field
// out of the caller's reach, check decides on that copy and write puts that
// same copy into the document. A second reading could hand a checksum past
// the check and a different one into the signature.
//
// The document is read back before it is handed over, the way task.buildTask
// reads its own task back: what comes out is what every verifier will decode
// out of exactly these bytes.
export function encodeParams(a, params) {
  const kind = kindOf(a);
  if (kind === undefined) throw new Error("core: invalid parameters");
  let b;
  if (kind === null) {
    if (!validateParams(a, params)) throw new Error("core: invalid parameters");
    b = utf8("{}");
  } else {
    let taken;
    try {
      if (params === null || typeof params !== "object") throw new Error("core: invalid parameters");
      taken = kind.take(params);
    } catch {
      throw new Error("core: invalid parameters");
    }
    if (!kind.check(taken)) throw new Error("core: invalid parameters");
    b = utf8(JSON.stringify(kind.write(taken)));
  }
  if (b.length > MAX_PARAMS) throw new Error("core: invalid parameters");
  parseParams(a, b);
  return b;
}

// parseParams decodes the parameters of an action strictly and checks them
// (Go: ParseParams). Every failure is one error, as it is one error in Go.
// It stays inside the module: the shell builds parameters and never reads
// any, and the one place that reads them from outside is the activate task of
// a UI proof.
function parseParams(a, raw) {
  const kind = kindOf(a);
  if (kind === undefined) throw new Error("core: invalid parameters");
  let v;
  try {
    v = decode(raw, paramsBudget, kind === null ? S({}) : kind.shape);
  } catch (e) {
    if (e instanceof BadJSON || e instanceof BadInput) throw new Error("core: invalid parameters");
    throw e;
  }
  // v is this module's own object now, built out of the document just
  // decoded, so the record taken from it is what the checks decided on.
  if (kind === null) return null;
  const taken = kind.take(v);
  if (!kind.check(taken)) throw new Error("core: invalid parameters");
  return taken;
}

// ------------------------------------------------------------------- results

// States of an app, license states, kinds of log entries and levels of a
// notice (Go: results.go).
const APP_STATES = new Set(["ok", "unreachable", "unlicensed"]);
const LICENSE_STATES = new Set(["none", "valid", "grace", "expired"]);
const LOG_KINDS = new Set(["login", "login-failed", "channel", "task", "trust", "rights", "app",
  "store", "license", "update"]);
const NOTICE_LEVELS = new Set(["info", "warn", "error"]);
const LEVELS = new Set([trust.LEVEL_OFF, trust.LEVEL_CRITICAL, trust.LEVEL_ROOT, trust.LEVEL_HARDCORE]);
const MODES = new Set([MODE_AUTO, MODE_SEMI, MODE_NOTIFY]);
const UPDATE_STATES = new Set([UPDATE_AVAILABLE, UPDATE_DOWNLOADED, UPDATE_INSTALLING, UPDATE_INSTALLED,
  UPDATE_ROLLEDBACK, UPDATE_FAILED, UPDATE_REJECTED, UPDATE_NEEDSGRANTS]);
// The result of a log entry is "ok" or one of wire's codes, and nothing else.
const LOG_RESULTS = new Set(["ok", ...Object.values(CODES)]);
// The roles a server carries and the machines NGC is built for (Go:
// tunnel.ValidRole, tunnel.Hello.Arch).
const NODE_ROLES = new Set(["web", "api", "db", "services"]);
const ARCHS = new Set(["amd64", "arm64"]);

// A SHA-256, and the size of a Noise, signing or TLS key in the answers below.
const HASH_SIZE = 32, KEY_SIZE = 32;
const EMPTY = new Uint8Array(0);

// How long a start menu is, how many roles one row may carry and how long a
// pairing code may be. The other limits of the table below are the contracts'
// own (trust.MAX_USERS, MAX_ROLES, MAX_NODES, MAX_STORES, the limit of a
// LogQuery, catalog's MAX_APPS, MAX_ACTIONS and MAX_CATALOG_VERSIONS) and
// keep the shell standing even when the API sends more than it may.
const MAX_DESKTOP_APPS = 1000;
const MAX_NODE_ROLES = 4; // Go: tunnel.maxRoles
const MAX_CODE = 4096;
const MAX_URI = 512;
// Names and texts the API passes on from a manifest or a catalog, in the
// length Go holds them to (store.Entry.Name, trust.validNode).
const MAX_TEXT = 64;

// Go has *sign.Envelope and *proof.Proof inside a UI proof: a member that is
// absent is nil, and one that stands there empty is not the same thing. P is
// json's pointer struct, whose zero is null. The members come from the shapes
// sign and proof already hold, so a member added there is a member here and
// there is nothing to keep in step by hand.
const pointerOf = (shape) => P(shape.m);
const envelopePointer = pointerOf(envelopeShape);
const proofPointer = pointerOf(proof.proofShape);

const meShape = S({ id: str, name: str, roles: A(str), admin: bool, passwordLogin: bool, totp: bool, level: str });
const desktopAppShape = S({
  id: str, name: str, version: str, icon: str, entry: str, width: int, height: int,
  actions: A(str), state: str, core: bool, node: str,
});
const totpBeginShape = S({ secret: str, uri: str });
const userInfoShape = S({ id: str, name: str, roles: A(str), totp: bool, locked: bool, lastLogin: int });
const roleRightsShape = S({ role: str, rights: A(str) });
const pairCodeShape = S({ code: str, expires: int });
const pendingShape = S({ name: str, key: bytes, since: int });
const pendingAPIShape = S({ node: str, noise: bytes, sign: bytes, tls: bytes, url: str, since: int });
const nodeInfoShape = S({ node: str, online: bool, roles: A(str), version: str, arch: str });
const logEntryShape = S({
  id: str, time: int, user: str, kind: str, app: str, action: str, node: str, result: str, detail: str,
});
const sourceInfoShape = S({ id: str, name: str, url: str, seq: uint, checked: int, error: str });
const catalogResultShape = S({ catalog: envelopeShape });
const installedAppShape = S({
  id: str, version: str, store: str, node: str, active: bool, state: str, license: str, keptData: int,
});
const uploadResultShape = S({ id: str, version: str, sha256: bytes, ui: bytes });
const licenseResultShape = S({ app: str, state: str });
const installKeyShape = S({ installation: str, key: bytes });
const updateInfoShape = S({
  target: str, node: str, installed: str, available: str, sha256: bytes, key: bytes,
  mode: str, window: str, state: str,
});
const trustResultShape = S({ seq: uint });
const noneShape = S({});
const uiProofShape = S({
  app: str, version: str, store: str,
  catalog: envelopePointer, task: envelopePointer, client: bytes, proof: proofPointer,
});

// The results, one entry per action key ("ngc.users/list"): the form a
// document decodes into, the array limits its budget needs and the rules its
// fields are then held to. One table, so a shape and the rules that go with
// it cannot drift apart.
//
// A list is bounded at its root, which json's budget calls "": the elements
// of a root array stand at "*", and a member of one of them at "*/<name>".
const RESULTS = {
  __proto__: null,
  "ngc.desktop/me": { shape: meShape, items: { roles: trust.MAX_ROLES }, check: checkMe },
  "ngc.desktop/ui-proof": { shape: uiProofShape, items: {}, check: checkUIProofResult },
  "ngc.desktop/apps": {
    shape: A(desktopAppShape),
    items: { "": MAX_DESKTOP_APPS, "*/actions": MAX_ACTIONS },
    check: (v) => every(v, checkDesktopApp) && unique(v, (a) => a.id),
  },
  "ngc.desktop/ui": { shape: noneShape, items: {}, check: () => true },
  "ngc.desktop/totp-begin": { shape: totpBeginShape, items: {}, check: checkTOTPBegin },
  "ngc.desktop/totp-confirm": { shape: noneShape, items: {}, check: () => true },
  "ngc.desktop/totp-off": { shape: noneShape, items: {}, check: () => true },

  "ngc.users/list": {
    shape: A(userInfoShape),
    items: { "": trust.MAX_USERS, "*/roles": trust.MAX_ROLES },
    check: (v) => every(v, checkUserInfo) && unique(v, (u) => u.id),
  },
  "ngc.users/rights": {
    shape: A(roleRightsShape),
    items: { "": trust.MAX_ROLES, "*/rights": MAX_RIGHTS },
    check: (v) => every(v, checkRoleRights) && unique(v, (r) => r.role),
  },
  "ngc.users/set-rights": { shape: noneShape, items: {}, check: () => true },
  "ngc.users/trust": { shape: trustResultShape, items: {}, check: () => true },
  "ngc.users/pair-code": { shape: pairCodeShape, items: {}, check: (v) => checkCode(v, "ngc-pair:") },
  "ngc.users/join-code": { shape: pairCodeShape, items: {}, check: (v) => checkCode(v, "ngc-join:") },
  "ngc.users/pending": {
    shape: A(pendingShape), items: { "": trust.MAX_NODES },
    check: (v) => every(v, checkPending) && unique(v, (p) => b64encode(p.key)),
  },
  "ngc.users/pending-api": {
    shape: A(pendingAPIShape), items: { "": trust.MAX_NODES },
    check: (v) => every(v, checkPendingAPI) && unique(v, (p) => p.node),
  },
  "ngc.users/nodes": {
    shape: A(nodeInfoShape), items: { "": trust.MAX_NODES, "*/roles": MAX_NODE_ROLES },
    check: (v) => every(v, checkNodeInfo) && unique(v, (n) => n.node),
  },
  "ngc.users/log": {
    shape: A(logEntryShape), items: { "": MAX_LOG_LIMIT },
    check: (v) => every(v, checkLogEntry) && unique(v, (e) => e.id),
  },

  "ngc.store/sources": {
    shape: A(sourceInfoShape), items: { "": trust.MAX_STORES },
    check: (v) => every(v, checkSourceInfo) && unique(v, (s) => s.id),
  },
  "ngc.store/catalog": {
    shape: catalogResultShape, items: {},
    check: (v) => validateEnvelope(v.catalog, MAX_PAYLOAD),
  },
  "ngc.store/installed": {
    shape: A(installedAppShape), items: { "": MAX_APPS },
    check: (v) => every(v, checkInstalledApp),
  },
  "ngc.store/upload": { shape: uploadResultShape, items: {}, check: checkUploadResult },
  "ngc.store/license": {
    shape: licenseResultShape, items: {},
    check: (v) => validApp(v.app) && LICENSE_STATES.has(v.state),
  },
  "ngc.store/install-key": {
    shape: installKeyShape, items: {},
    check: (v) => validID(v.installation) && v.key.length === KEY_SIZE,
  },
  "ngc.store/updates": {
    shape: A(updateInfoShape), items: { "": MAX_CATALOG_VERSIONS },
    check: (v) => every(v, checkUpdateInfo),
  },
  "ngc.store/set-update": { shape: noneShape, items: {}, check: () => true },
  "ngc.store/activate": { shape: noneShape, items: {}, check: () => true },
  "ngc.store/deactivate": { shape: noneShape, items: {}, check: () => true },
  "ngc.store/uninstall": { shape: noneShape, items: {}, check: () => true },
  "ngc.store/delete-data": { shape: noneShape, items: {}, check: () => true },
  "ngc.store/stage": { shape: noneShape, items: {}, check: () => true },
  "ngc.store/install": { shape: noneShape, items: {}, check: () => true },
};

// RESULT_SHAPES is the form of every action's result, taken out of the one
// table above so that a shape and the rules beside it stay together.
//
// Without a prototype, for the reason wire.CODES has none: its keys are
// action keys, and a lookup of "constructor" or "toString" on an ordinary
// object answers with something that is no shape at all.
export const RESULT_SHAPES = Object.freeze(Object.assign(Object.create(null),
  Object.fromEntries(Object.entries(RESULTS).map(([key, r]) => [key, r.shape]))));

// parseResult reads the result of an action: strict JSON within
// wire.MAX_CALL_RESULT bytes and the list limits of the table, then every
// field. Every failure is one error, as it is one error in Go.
//
// The bytes go into json.decode as they stand, which is where they are viewed
// once (b64.viewOf); nothing here looks at them a second time.
export function parseResult(a, b) {
  const found = lookup(a?.app, a?.service, a?.name);
  const r = found === null ? undefined : RESULTS[`${found.app}/${found.name}`];
  if (r === undefined) throw new Error("core: result the shell refuses");
  // A UI proof is a document of its own with rules of its own; reading it
  // here a second way would be a second answer to one question.
  if (r.shape === uiProofShape) return parseUIProof(b);
  let v;
  try {
    v = decode(b, new Budget({ size: MAX_CALL_RESULT, items: r.items }), r.shape);
  } catch (e) {
    if (e instanceof BadJSON || e instanceof BadInput) throw new Error("core: result the shell refuses");
    throw e;
  }
  // v is this module's own object, built out of the document just decoded, so
  // the checks below read what parseResult hands back and nothing else.
  if (!r.check(v)) throw new Error("core: result the shell refuses");
  return v;
}

function checkMe(v) {
  return validID(v.id) && validLoginName(v.name) && roles(v.roles, trust.MAX_ROLES) && LEVELS.has(v.level);
}

function checkDesktopApp(v) {
  return validAppID(v.id) && validText(v.name, MAX_TEXT) && optVersion(v.version)
    && (v.icon === "" || validPath(v.icon)) && (v.entry === "" || validPath(v.entry))
    && size(v.width) && size(v.height)
    && v.actions.length <= MAX_ACTIONS && noDuplicate(v.actions) && v.actions.every(validName)
    && APP_STATES.has(v.state) && (v.node === "" || validName(v.node));
}

// A RULE OF THE SHELL, NOT OF THE CONTRACT. Go has no ParseTOTPBegin - it
// only writes this answer (EncodeResult) - so the alphabet, the 16 to 64
// characters and the prefix are all chosen here, from what Go's TOTP secret
// and otpauth URI look like today. The secret is what the user types into an
// authenticator app; the URI carries it again for the QR code, and nothing
// but an otpauth URI belongs in one.
//
// The price: an API that one day writes the secret in another alphabet, or
// longer, or the URI with another scheme, sends something legitimate that the
// shell refuses - and then nobody can switch TOTP on at all, because
// ngc.desktop/totp-begin is the only way in. It is a refusal and never a hole,
// so it is the safe side; but if the secret ever changes shape, this line
// changes with it, and the failure will look like a broken server.
function checkTOTPBegin(v) {
  return /^[A-Z2-7]{16,64}$/.test(v.secret)
    && v.uri.startsWith("otpauth://totp/") && validText(v.uri, MAX_URI);
}

function checkUserInfo(v) {
  return validID(v.id) && validLoginName(v.name) && roles(v.roles, trust.MAX_ROLES)
    && whole(v.lastLogin) && v.lastLogin >= 0;
}

function checkRoleRights(v) {
  return validName(v.role) && v.rights.length <= MAX_RIGHTS && noDuplicate(v.rights)
    && v.rights.every(validRight);
}

// A RULE OF THE SHELL, NOT OF THE CONTRACT. Go has one type for both answers
// (PairCodeResult) and asks nothing of the text; this holds each of the two
// actions to its own prefix, because a pairing code and a joining code are
// shown to the admin for different jobs and a swapped pair would have them
// paste the wrong one into the wrong script.
//
// The price: if the API ever answers ngc.users/pair-code with a code of
// another kind - or the prefixes in tunnel change - the shell refuses an
// answer that is perfectly good, and no controller can be paired until this
// line follows. A refusal, never a hole.
function checkCode(v, prefix) {
  return v.code.startsWith(prefix) && validText(v.code, MAX_CODE) && whole(v.expires) && v.expires > 0;
}

function checkPending(v) {
  return validText(v.name, MAX_TEXT) && v.key.length === KEY_SIZE && whole(v.since) && v.since > 0;
}

function checkPendingAPI(v) {
  return validName(v.node) && v.noise.length === KEY_SIZE && v.sign.length === KEY_SIZE
    && v.tls.length === HASH_SIZE && validTunnelURL(v.url) && whole(v.since) && v.since > 0;
}

function checkNodeInfo(v) {
  return validName(v.node) && v.roles.length <= MAX_NODE_ROLES && noDuplicate(v.roles)
    && v.roles.every((r) => NODE_ROLES.has(r))
    && optVersion(v.version) && (v.arch === "" || ARCHS.has(v.arch));
}

function checkLogEntry(v) {
  return validLogID(v.id) && whole(v.time) && v.time > 0 && (v.user === "" || validID(v.user))
    && LOG_KINDS.has(v.kind) && (v.app === "" || validAppID(v.app))
    && (v.action === "" || validName(v.action)) && (v.node === "" || validName(v.node))
    && LOG_RESULTS.has(v.result) && (v.detail === "" || validText(v.detail, MAX_MESSAGE_TEXT));
}

function checkSourceInfo(v) {
  return validName(v.id) && validText(v.name, MAX_TEXT) && validStoreURL(v.url)
    && whole(v.checked) && v.checked >= 0 && (v.error === "" || validText(v.error, MAX_MESSAGE_TEXT));
}

function checkInstalledApp(v) {
  return validApp(v.id) && optVersion(v.version) && validName(v.store) && validName(v.node)
    && APP_STATES.has(v.state) && LICENSE_STATES.has(v.license) && whole(v.keptData) && v.keptData >= 0;
}

function checkUploadResult(v) {
  return validApp(v.id) && validVersion(v.version) && v.sha256.length === HASH_SIZE && v.ui.length === HASH_SIZE;
}

function checkUpdateInfo(v) {
  return validTarget(v.target) && validName(v.node) && optVersion(v.installed) && optVersion(v.available)
    && (v.sha256.length === 0 || v.sha256.length === HASH_SIZE)
    && (v.key.length === 0 || v.key.length === KEY_SIZE)
    && MODES.has(v.mode) && (v.window === "" || WINDOW.test(v.window))
    && (v.state === "" || UPDATE_STATES.has(v.state));
}

// checkUIProofResult never runs: parseResult hands a ui-proof straight to
// parseUIProof, which has the rules. It stands in the table so that every
// entry has the same three members and nobody has to remember the exception.
function checkUIProofResult() {
  return true;
}

// ------------------------------------------------------------------- notices

const noticeShape = S({ level: str, text: str, app: str });
// A notice holds no array, so only its size is counted (Go: noticeBudget).
const noticeBudget = new Budget({ size: MAX_NOTICE });

// parseNotice checks the body of a "notify" control message: at most
// MAX_NOTICE bytes of strict JSON, a level of the three, German text of at
// most wire.MAX_MESSAGE_TEXT bytes and an app that is empty or an app of a
// store (Go: ParseNotice).
export function parseNotice(b) {
  let n;
  try {
    n = decode(b, noticeBudget, noticeShape);
  } catch (e) {
    if (e instanceof BadJSON || e instanceof BadInput) throw new Error("core: malformed notice");
    throw e;
  }
  if (!NOTICE_LEVELS.has(n.level) || !validText(n.text, MAX_MESSAGE_TEXT)
    || (n.app !== "" && !validApp(n.app))) {
    throw new Error("core: malformed notice");
  }
  return n;
}

// ----------------------------------------------------------------- UI proofs

// A UI proof holds no array, and its envelopes and its certificate hold
// bytes; the whole answer is at most wire.MAX_CALL_RESULT bytes (Go:
// uiProofBudget).
const uiProofBudget = new Budget({ size: MAX_CALL_RESULT });

// parseUIProof decodes a UI proof from outside (Go: ParseUIProof): either a
// catalog alone, with a store that is not the local one, or a task, a client
// certificate and the user's proof over it, with the local store. Every
// failure is one error.
export function parseUIProof(b) {
  let p;
  try {
    p = decode(b, uiProofBudget, uiProofShape);
  } catch (e) {
    if (e instanceof BadJSON || e instanceof BadInput) throw new Error("core: UI proof does not hold");
    throw e;
  }
  // p is this module's own object from here on, so every read of it below
  // sees the same value.
  if (!validApp(p.app) || !validVersion(p.version) || !validName(p.store)) {
    throw new Error("core: UI proof does not hold");
  }
  const storeApp = p.catalog !== null && p.task === null && p.client.length === 0 && p.proof === null
    && p.store !== LOCAL_STORE && validateEnvelope(p.catalog, MAX_PAYLOAD);
  const uploaded = p.catalog === null && p.task !== null && p.client.length > 0
    && p.client.length <= session.MAX_CERT && p.proof !== null && p.store === LOCAL_STORE
    && validateEnvelope(p.task, task.MAX_TASK) && proof.validateProof(p.proof);
  if (!storeApp && !uploaded) throw new Error("core: UI proof does not hold");
  return p;
}

// verifyUIProof checks p for app at version against the trust list l and
// answers with the UI hash (Go: VerifyUIProof). A store app: the store p.store
// of l signed p.catalog and its catalog lists app at version, whose ui is the
// hash - any store of l may vouch, whichever store the app came from. An
// uploaded app: the user's proof over the client certificate holds against l,
// the task it carries is ngc.store/controller/activate on a node where that
// user may run root tasks, and its parameters name this app, this version and
// the local store; their ui is the hash.
//
// No time is checked: the approval holds while its key and root permission
// stay in l. Every failure is one error, as it is ErrUIProof in Go - which
// rule a proof broke is nothing the shell tells a user apart.
export async function verifyUIProof(p, app, version, l) {
  try {
    return await checkUIProof(p, app, version, l);
  } catch {
    throw new Error("core: UI proof does not hold");
  }
}

// checkUIProof is verifyUIProof with the reason of a refusal. It checks the
// form again, so a proof that did not come through parseUIProof cannot mix
// the two kinds.
async function checkUIProof(p, app, version, l) {
  if (p === null || typeof p !== "object" || l === null || typeof l !== "object") {
    throw new Error("core: UI proof of another form, app or version");
  }
  // Every field of the caller's proof is read ONCE, here, and nothing below
  // ever asks the caller again. The client certificate above all: session
  // decodes those bytes and task hashes them to check what the task is bound
  // to, and two readings of one object are two certificates - the approval
  // would be checked for one user and the task bound to another. bytesOf
  // copies them out of the caller's reach before anything is decided, so the
  // bytes that are checked are the bytes that are used.
  const { app: named, version: at, store, catalog, task: signedTask, proof: userProof, client: given } = p;
  const client = given === null || given === undefined ? EMPTY : bytesOf(given, "core: certificate is not bytes");
  if (named !== app || at !== version) {
    throw new Error("core: UI proof of another form, app or version");
  }
  if (catalog !== null && catalog !== undefined) {
    if (signedTask !== null && signedTask !== undefined) throw new Error("core: UI proof of another form");
    if (userProof !== null && userProof !== undefined) throw new Error("core: UI proof of another form");
    if (client.length !== 0) throw new Error("core: UI proof of another form");
    return await catalogUI(catalog, store, app, version, l);
  }
  if (signedTask === null || signedTask === undefined || userProof === null || userProof === undefined
    || store !== LOCAL_STORE) {
    throw new Error("core: UI proof of another form");
  }
  const cert = await session.verifyApproval(client, userProof, l);
  const t = await task.verifyApproval(signedTask, cert, client);
  if (t.app !== APP_STORE || t.service !== SERVICE_CONTROLLER || t.action !== ACT_ACTIVATE) {
    throw new Error("core: task is no activate of the uploaded app at that version");
  }
  if (!trust.mayRoot(l, cert.user, t.node)) {
    throw new Error("core: approver may not run root tasks on the task's node");
  }
  const params = parseParams(lookup(APP_STORE, SERVICE_CONTROLLER, ACT_ACTIVATE), t.params);
  if (params.app !== app || params.version !== version || params.store !== LOCAL_STORE) {
    throw new Error("core: task is no activate of the uploaded app at that version");
  }
  return params.ui;
}

// catalogUI returns the ui of app at version out of env, a catalog the store
// id of l signed (Go: catalogUI).
async function catalogUI(env, id, app, version, l) {
  const stores = l.stores;
  if (!Array.isArray(stores)) throw new Error("core: store of the UI proof is not in the trust list");
  // The LAST store of that id, as Go's loop leaves it. A verified list has no
  // two stores under one id (trust.validateList), so this only ever differs
  // for a list somebody built by hand - and there the two languages must
  // still answer alike.
  let st = null;
  for (const s of stores) if (s?.id === id) st = s;
  if (st === null) throw new Error("core: store of the UI proof is not in the trust list");
  const c = await verifyCatalog(env, st, 0);
  for (const e of c.apps) {
    if (e.id !== app) continue;
    for (const v of e.versions) if (v.version === version) return v.ui;
  }
  throw new Error("core: catalog does not list the app at that version");
}

// ------------------------------------------------------------------- helpers

// whole is Number.isSafeInteger under a name that says what it is for: Go
// holds these fields as int64 and a decoder that fills one refuses a string,
// a boolean and a fraction, while every comparison here would take all three.
// json.decode already refuses a number beyond 2^53-1 by its digits; this is
// what holds a value the shell built itself to the same rule.
const whole = (n) => Number.isSafeInteger(n);

// A version that may be left out: the API writes one for every app it knows,
// and an answer without one is a gap in what is shown, never a decision.
const optVersion = (s) => s === "" || validVersion(s);

// The width and height a manifest may ask for: none, or a window between 100
// and 4096 pixels (Go: store.ParseManifest).
const size = (n) => whole(n) && (n === 0 || (n >= 100 && n <= 4096));

// roles are the names of roles of the trust list, where they are never empty
// and never repeat.
const roles = (v, max) => v.length <= max && noDuplicate(v) && v.every(validName);

const every = (v, check) => v.every((e) => check(e));

// unique answers whether no two rows of a list carry the same id.
//
// A RULE OF THE SHELL, NOT OF THE CONTRACT. Go writes these lists
// (EncodeResult) and asks nothing of them. It is here because the shell shows
// a list and then acts on the row the user picked, and every one of these
// actions names its row by ONE field: set-rights names a role, ui and ui-proof
// name an app and no node, and a trust list holds one user, one node and one
// store per id. Two rows under one such id are therefore not two things to
// choose between - the second one is unreachable, and whichever the shell
// picked would decide silently.
//
// The price: an API that legitimately repeats a row has its whole answer
// refused, and the page stays empty instead of showing something. So the field
// is the one the actions name and never the whole row, and a list whose rows
// are legitimately keyed by two things is NOT held to this rule:
// ngc.store/installed carries an app AND a node in Go, the same app may run on
// two controllers, and it is left out here for that reason. ngc.users/pending
// is held to its key and not to its name, because two controllers an admin has
// not added yet may well be called the same and are told apart by the
// fingerprint of that key. A refusal, never a hole.
//
// A Set, not the pairwise walk Go uses for its short lists: these run to 10000
// rows, and refusing a bad answer may not cost the shell a second of work.
function unique(v, idOf) {
  const seen = new Set();
  for (const e of v) {
    const id = idOf(e);
    if (seen.has(id)) return false;
    seen.add(id);
  }
  return true;
}

const noDuplicate = (v) => new Set(v).size === v.length;

// list takes one reading of a caller's array of strings: a real array, copied
// element by element, or null. A Proxy over an array passes Array.isArray and
// may answer two readings differently, so the copy is what is checked and
// what is written.
function list(v) {
  if (v === null || v === undefined) return [];
  if (!Array.isArray(v)) return null;
  return [...v];
}

// copy takes one reading of a caller's bytes, out of the caller's reach
// (b64.bytesOf reads no property of the object at all), or null for anything
// that is no bytes.
function copy(v) {
  try {
    return bytesOf(v, "core: not bytes");
  } catch {
    return null;
  }
}


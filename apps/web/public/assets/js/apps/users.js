// "Benutzer und Rechte": who may do what, and which servers belong to this
// installation. It ships with NGC, so the web interface builds its face
// itself - core.AppParams refuses a core app ("ngc.") for ngc.desktop/ui and
// for ngc.desktop/ui-proof alike, so this window can never be a ui-tar out of
// the sandboxed frame.
//
// THIS IS THE MOST DANGEROUS WINDOW NGC HAS, and it is built for that:
//
//   - Nothing that changes anything happens on one click. Every change is
//     asked for first, and the question SAYS WHAT HAPPENS - which role, which
//     rights, how many people it hits, which server gets in and what it may do
//     afterwards. "Sind Sie sicher?" is a question nobody can answer.
//   - The focus of that question stands on Abbrechen, never on the button that
//     gives something away, and Escape is always the way out.
//   - Everything from outside is text. Names of users, of servers and of apps
//     come through dom.js, which writes text nodes; a name that looks like
//     markup stands on screen as the characters it is.
//   - Every answer is read by core.parseResult before a character of it is
//     shown. One bad field throws the whole answer away, the way the desktop
//     does it: a list that broke its contract is a list somebody else chose.
//   - Taking a server into the trust list is signed by the PERSON and not by
//     the session, and the shell follows the list to what then stands before
//     it writes anything else. Both are below, at joinNode.
//
// WHAT core.critical DOES AND DOES NOT DECIDE HERE, measured and named
// because it surprised the round that built this: core.critical answers TRUE
// only for actions of the app store (upload, delete-data, activate with
// grants, uninstall without keepData). For EVERY action of ngc.users it
// answers false. trust.needsConfirm then asks for a second proof only where a
// role stands at level HARDCORE (level root reads op.critical || op.root, and
// no action of ngc.users is either) or names the action in its black list. A
// surface that asked those two and nothing else would hand out admin rights
// on one click without a word. So the question below is this app's own rule
// and does not wait for the contract's: it is asked before every change, and
// the two contract answers only make it heavier. users.spec.js pins the measurement, so the
// day a row of ngc.users becomes critical, that case goes red and this
// paragraph is read again.
//
// WHO SIGNS A NEW VERSION, and why it is not the session (Fixrunde 2, W1).
// A version of the trust list is verified against a key that stands in the
// version BEFORE it (Go: shared/trust/trust.go, prev.KeyOwner(s.Proof.KeyID)).
// A session key stands in no list, so a signature of it would always be
// ErrSigner - the round that built this invented a ctx.session.prove and it
// could never have worked. It is the PERSON's own key, asked for by
// shell/confirm.js (askSignature, purpose trustlist/v1), which is also the
// safer side: whoever changes the root of trust is asked for it.
//
// WHAT IS NOT HERE, named rather than faked: ngc.users has no root task, so
// the second confirmation over the bytes of a task (purpose confirm/v1) has
// nothing here to cover - confirm.js has it for the app store. Where there is
// no session and no verified list, the surface says the change cannot be made
// and why; it does not draw a button that would do nothing.
import { el, button, text, clear } from "../shell/dom.js";
import * as core from "../contracts/core.js";
import * as trust from "../contracts/trust.js";
import * as tunnel from "../contracts/tunnel.js";
import { askSignature } from "../shell/confirm.js";
import { PURPOSE_TRUST_LIST } from "../contracts/sign.js";
import { b64encode } from "../contracts/b64.js";
import { validTunnelURL } from "../contracts/url.js";
import { coreApp, validAppID, validName } from "../contracts/text.js";
import { APPS, NODES, RIGHTS, USERS } from "../shell/changed.js";
import { freshness } from "../shell/stale.js";

// How many log lines one page holds. The contract allows 1 to 500; fifty is
// a screenful and a half, and paging is one button.
const LOG_LIMIT = 50;

// The same sentence wherever an answer did not hold up. It says nothing about
// what was wrong with it: that would describe the inside of the server, and
// it is nothing the person can act on either.
const BAD_LIST = "Die Antwort der Anlage war nicht in Ordnung. Es wird nichts angezeigt.";
const BAD_OFFER = "Die Antwort der Anlage war nicht in Ordnung. Es wird nichts angeboten.";
const NO_REACH = "Die Anlage antwortet gerade nicht.";
// The question quotes the fingerprint, so it is not asked before the
// fingerprint is there: a question that shows three dots where the
// fingerprint belongs is a question nobody can answer.
const NO_FINGERPRINT = "Der Fingerabdruck wird noch berechnet. Bitte noch einmal versuchen.";
// The version IS stored; only this browser did not get there. Writing a
// second version from here would be the fork, so nothing more is written and
// the sentence says the one thing that fixes it.
const BEHIND = "Der Server ist eingetragen. Bitte die Seite neu laden, damit NGC hier auf dem "
  + "neuen Stand ist.";

// Cancelled is what the person answered when the question for their key was
// closed with Abbrechen. It is an answer and not a failure, and it reads
// differently on screen.
class Cancelled extends Error {}
// Behind is BEHIND as an answer: the write went through, the following did
// not.
class Behind extends Error {}

// What a log entry's kind is called in German. A kind the contract knows but
// this table does not is shown as it stands - it is one of core's own ten
// words and it is text either way.
const KINDS = {
  __proto__: null,
  login: "Anmeldung", "login-failed": "Anmeldung gescheitert", channel: "Kanal", task: "Aufgabe",
  trust: "Vertrauensliste", rights: "Rechte", app: "App", store: "Store", license: "Lizenz",
  update: "Update",
};

// The four tabs, in the order they stand on screen.
const TABS = [
  ["users", "Benutzer"],
  ["rights", "Rollen und Rechte"],
  ["servers", "Server"],
  ["log", "Protokoll"],
];

// ---------------------------------------------------------------- the one door
//
// EVERY byte in and out of this app goes through this one function, and that
// is on purpose: the link is a stand-in today (shell/fake-link.js) and becomes
// net/link.js when the login lands. When it does, exactly this function
// changes and nothing else in this file moves.
//
// FIXRUNDE 2: this used to be TWO functions, and the second one was invented.
// It opened a channel with link.open(app, service, name) and wrote on it with
// send()/end() - an interface nobody else has. What Task 17 really built is
// net/channel.js, where the action's own flow column decides what happens and
// the caller hands over ONE option bag:
//
//   FLOW_CALL      -> channel.call(link, action, {params, taskBytes, confirmBytes})
//   FLOW_IN        -> channel.sendIn(link, action, {params, taskBytes, stream, onProgress})
//
// The shortcut the desktop's link carries takes the same thing as
// (app, service, name, paramBytes, opts) - which is what apps/store.js hands
// it, so this hands it the same. ngc.users/api/trust is the one action of
// this app whose flow is "in", and its body travels as opts.stream.
async function ask(link, action, params, opts) {
  const body = params === null || params === undefined ? null : core.encodeParams(action, params);
  const answer = await link.call(action.app, action.service, action.name, body, opts ?? null);
  return core.parseResult(action, answer);
}

// oneChunk turns bytes into the stream the link sends, the same way
// apps/store.js does it: a Blob is what a file would be, and its stream is
// what channel.sendIn reads.
const oneChunk = (bytes) => new Blob([bytes]).stream();

// ------------------------------------------------------------- a new version

// joinable reports whether a waiting API server may be offered for joining at
// all: a name that is a name and an address that is a tunnel address.
//
// core.parseResult already refuses a whole ngc.users/pending-api answer that
// carries an address the grammar does not know (checkPendingAPI runs the same
// trust.ValidTunnelURL), so through the API this can only ever agree with it.
// It stands because the answer of this function decides whether a button is
// drawn that puts that address into the trust list, and because
// trust.List.Validate would refuse the version afterwards - a refusal after
// the admin said yes is a refusal in the wrong place. It is exported so that
// the rule can be measured on its own rather than through a parser that
// refuses first.
export function joinable(p) {
  try {
    return validName(p.node) && validTunnelURL(p.url);
  } catch {
    // A field behind a getter that throws: no answer is no offer.
    return false;
  }
}

// The one order everything in a trust list stands in (Go: trust.List.Validate
// wants users, roles, nodes and stores ascending by id or name and auto by
// target, and a user's roles ascending too). Every id, name and target here
// is ASCII, where sorting by UTF-16 unit and Go's sorting by byte are the
// same order.
const by = (k) => (a, b) => (a[k] < b[k] ? -1 : a[k] > b[k] ? 1 : 0);
const plain = (a, b) => (a < b ? -1 : a > b ? 1 : 0);

function sortAll(l) {
  l.users.sort(by("id"));
  l.roles.sort(by("name"));
  l.nodes.sort(by("id"));
  l.stores.sort(by("id"));
  l.auto.sort(by("target"));
  for (const u of l.users) u.roles.sort(plain);
}

// Every new version is built from the verified one: the shell changes exactly
// what the admin asked for and nothing else, then checks its own work before
// it signs. A version that trust.List.Validate would refuse is never sent -
// and, just as important, never signed: the signature is what makes a version
// a version, so it is the last thing to happen and the first thing to be
// skipped when the change does not hold up.
export async function buildNextVersion(version, change, { now, signer }) {
  const next = structuredClone(version.list);
  next.seq = version.list.seq + 1;
  // A copy, not the caller's array: what is checked below and what is written
  // into the payload have to be the same bytes, and a caller that writes into
  // its own hash afterwards must not be able to change either.
  next.prev = new Uint8Array(version.hash);
  next.issued = now;
  change(next);
  sortAll(next); // users, roles, nodes, stores and auto by id, name or target
  if (!trust.validateList(next)) throw new Error("Diese Änderung ergibt keine gültige Vertrauensliste.");
  const payload = trust.encodeList(next);
  // The signature is the person's own, asked for by confirm.js, and a person
  // may say no: null is an answer and nothing is built out of it.
  const proof = await signer(payload); // purpose trustlist/v1
  if (proof === null || proof === undefined) throw new Cancelled();
  // The same check the API and every controller will run: the chain from the
  // version in force to this one, signature and all. If the shell cannot
  // convince itself, nobody else is asked to be convinced either.
  await trust.advance(version, [{ payload, proof }]);
  return { payload, proof };
}

// encodeSigned writes one trust.Signed as Go writes it: the payload as
// base64url, the proof beside it, and authData/clientData left out where they
// are empty (Go: proof.Proof, both with omitempty). These are the bytes
// ngc.users/api/trust takes.
function encodeSigned(payload, proof) {
  const o = {
    payload: b64encode(payload),
    proof: { kind: proof.kind, kid: proof.kid, sig: b64encode(proof.sig) },
  };
  if (proof.authData && proof.authData.length > 0) o.proof.authData = b64encode(proof.authData);
  if (proof.clientData && proof.clientData.length > 0) o.proof.clientData = b64encode(proof.clientData);
  return new TextEncoder().encode(JSON.stringify(o));
}

// nodeID makes the name a new controller carries in the trust list out of the
// name it calls itself: lower case, letters, digits and hyphens, nothing else
// (Go: wire.ValidName). The name the person reads stays the one the server
// sent; this is only the handle the list needs, and it is shown in the
// question so that nobody is surprised by it later.
function nodeID(name, l) {
  const base = [...String(name).toLowerCase()]
    .map((c) => ((c >= "a" && c <= "z") || (c >= "0" && c <= "9") ? c : "-"))
    .join("").replace(/-+/g, "-").replace(/^-|-$/g, "").slice(0, 32).replace(/-$/, "");
  const start = validName(base) ? base : "ctl";
  let id = start;
  // Two servers with one id would be a version trust.List.Validate refuses,
  // so the second gets a number. The count is bounded because the truncation
  // above can map two numbers onto one name: an id that is still taken after
  // that is handed on as it is, and buildNextVersion refuses the version with
  // the one sentence it refuses every invalid change with.
  for (let n = 2; n < 1000 && trust.node(l, id) !== null; n++) id = (start + "-" + n).slice(0, 32);
  return id;
}

// --------------------------------------------- who may run root tasks there
//
// A CONTROLLER THAT NAMES NOBODY CAN DO NOTHING, and until this screen there
// was no way to name anybody. Measured on 19.10.2026 at a full installation
// out of a published release: the node "ngc-2" was paired and taken in, the
// app store offered it, and "Aktivieren" answered "Das hat der Rechner nicht
// angenommen." with no sentence of the server behind it - the refusal happens
// in the rights check, which writes no log line on either side. Setting up an
// app is a root task (apps/store.js, rootTask(A.activate, ...)), and
// trust.MayRoot lets one through only where the user stands in the node's
// rootUsers or one of their roles in its rootRoles (Go: shared/trust/trust.go
// :419). Both lists were written EMPTY by the only two places that wrote them
// at all (askJoin, askJoinAPI), and no other place in the whole interface ever
// wrote them: so on every freshly taken-in controller, nobody could ever
// install an app. That is not a broken flow, it is a missing screen.
//
// WHY THERE IS NO DEFAULT HERE. Whom to trust with root on a machine is a
// security decision and it belongs to the person who owns the installation,
// not to this file: a default would hand the right out in the one moment
// nobody is looking, and there is no way back out of a right nobody noticed
// being given. So nothing is ticked, the question says what a tick means, and
// the surface takes a server in even when nothing is ticked - and then says so
// in the sentence it answers with, because a server that can do nothing is
// worth saying out loud.
//
// A USER AND A ROLE ARE NOT THE SAME GIFT, and the question says which is
// which: a user names the one person, a role names every present AND future
// member of it, including ones somebody adds later. Both ways are offered
// because both are in the contract (Node.RootUsers, Node.RootRoles) and a home
// installation with one admin wants the short way; which of them is the better
// habit is the owner's call, which is why the surface asks rather than decides.
//
// ONLY A CONTROLLER CARRIES THEM. An API node may carry no root lists at all
// (Go: ValidAPINode asks for len(RootUsers)+len(RootRoles) == 0) and
// trust.MayRoot only ever looks at a node of kind controller, so a version
// that named anybody on an API node would be refused as invalid. The picker is
// therefore drawn for controllers and nowhere else.

// The one sentence for a controller nobody may run root tasks on. It stands in
// the row of such a server and in the question that changes it, so both say the
// same thing. The answer right after a take-up says it the other way round -
// what nobody can do there yet - because a sentence about a server that was
// just taken in has to say what follows from it.
const NO_ROOT = "Mit Systemrechten: niemand";

// rootPicker draws the choice and reads it back. node is the controller as the
// verified list carries it, or null for one that is not in the list yet: the
// boxes then start on what the list says, or on nothing at all.
//
// The users and roles come out of the VERIFIED list and not out of an action of
// the API, for the same reason fillListed does: what an admin is about to sign
// has to be built from the version the shell checked. It is also what makes the
// result valid without a second check - trust.List.Validate refuses a root list
// naming a user or a role the list does not have.
// mine is the id of the user whose session this is, and it is used in ONE
// case: a controller that is not in the list yet, where that person's box
// starts ticked. Only theirs, only rootUsers, and abwaehlbar - the box is a
// box like every other. The reason is V1.2 point 71: a take-up that ends with
// two empty lists leaves a machine on which nobody can set up an app, and the
// only way out of it used to be a surface that did not exist. Whoever can sign
// a version of the trust list holds the installation anyway, so offering them
// the tick adds no power; it only removes the case where someone takes a
// controller in and finds out at the first app that they may do nothing there.
// A role is NOT pre-ticked: a role hands the right to everyone who carries it
// today or tomorrow, which is a different decision from "me on this machine".
function rootPicker(l, node, mine = "") {
  const allUsers = Array.isArray(l?.users) ? l.users.filter(isObj) : [];
  const allRoles = Array.isArray(l?.roles) ? l.roles.filter(isObj) : [];
  const nowUsers = node !== null && Array.isArray(node.rootUsers)
    ? node.rootUsers
    : (node === null && typeof mine === "string" && mine !== "" ? [mine] : []);
  const nowRoles = node !== null && Array.isArray(node.rootRoles) ? node.rootRoles : [];
  const fs = el("fieldset", "role rootpick");
  fs.append(el("legend", null, "Aufgaben mit Systemrechten"));
  fs.append(el("p", "hint", "Wer hier einen Haken hat, darf auf diesem Server Apps "
    + "einrichten, entfernen, Daten löschen und das Terminal öffnen."));
  const users = new Map();
  const roles = new Map();
  const opts = el("div", "opts");
  for (const u of allUsers) {
    if (typeof u.id !== "string" || typeof u.name !== "string") continue;
    opts.append(pickBox(users, u.id, u.name, nowUsers.includes(u.id),
      "Benutzer " + u.name + " darf Aufgaben mit Systemrechten ausführen"));
  }
  fs.append(el("p", "sub", "Einzelne Personen"), opts);
  const ropts = el("div", "opts");
  for (const r of allRoles) {
    if (typeof r.name !== "string") continue;
    ropts.append(pickBox(roles, r.name, r.name, nowRoles.includes(r.name),
      "Rolle " + r.name + " darf Aufgaben mit Systemrechten ausführen"));
  }
  fs.append(el("p", "sub", "Ganze Rollen"), ropts,
    el("p", "hint", "Eine Rolle gilt auch für alle, die ihr später beitreten."));
  return {
    el: fs,
    // Sorted, so that the same choice always writes the same bytes, and
    // without duplicates by construction - one box per id and per role name.
    chosen() {
      return {
        rootUsers: [...users.entries()].filter(([, cb]) => cb.checked).map(([id]) => id).sort(plain),
        rootRoles: [...roles.entries()].filter(([, cb]) => cb.checked).map(([r]) => r).sort(plain),
      };
    },
  };
}

function pickBox(into, value, label, on, aria) {
  const lab = el("label", "opt");
  const cb = el("input", null);
  cb.type = "checkbox";
  cb.checked = on;
  // The legend of a fieldset is not part of a checkbox's name, so the name
  // carries what the tick means on its own.
  cb.setAttribute("aria-label", aria);
  lab.append(cb, el("span", "t", label));
  into.set(value, cb);
  return lab;
}

const isObj = (x) => x !== null && typeof x === "object";

// same compares two sorted lists of strings, so that a question answered
// without a change writes nothing: a version that changes nothing still costs
// a signature and a new number in the chain.
const same = (a, b) => a.length === b.length && a.every((x, i) => x === b[i]);

// rootWho says in one line who may run root tasks on a controller, with the
// names of the users rather than their ids: an id in a row is a row nobody can
// read.
function rootWho(l, node) {
  const us = (Array.isArray(node.rootUsers) ? node.rootUsers : []).map((id) => userOf(l, id));
  const rs = Array.isArray(node.rootRoles) ? node.rootRoles : [];
  if (us.length + rs.length === 0) return NO_ROOT;
  const parts = [...us];
  for (const r of rs) parts.push("Rolle " + r);
  return "Mit Systemrechten: " + parts.join(", ");
}

// tooManyRoot says why a choice cannot be written, and "" when it can. The
// limit is the contract's (Go: MaxRoot per list, trust.MAX_ROOT here), and it
// is said HERE rather than left to buildNextVersion: a refusal of the whole
// version reads as a sentence about the list, and nobody can act on that.
function tooManyRoot(want) {
  if (want.rootUsers.length > trust.MAX_ROOT || want.rootRoles.length > trust.MAX_ROOT) {
    return "Es sind zu viele Haken gesetzt. Mehr als " + trust.MAX_ROOT
      + " Personen und mehr als " + trust.MAX_ROOT + " Rollen kann ein Server nicht führen.";
  }
  return "";
}

function userOf(l, id) {
  const u = (Array.isArray(l?.users) ? l.users : []).find((x) => isObj(x) && x.id === id);
  return u && typeof u.name === "string" ? u.name : String(id);
}

// ------------------------------------------------------------------- the app

class Users {
  constructor(win, ctx) {
    this.ctx = ctx ?? null;
    // The sentence to carry over the next build of the Server panel.
    this.keep = "";
    this.link = ctx?.link ?? null;
    this.trust = ctx?.trust ?? null;
    this.session = ctx?.session ?? null;
    // The apps of this desktop, so a right can be shown as "Fotos · teilen"
    // instead of "de.example.fotos/teilen", and so buildRights knows which
    // rights there are to hand out at all. A missing list is no error: the
    // rights that a role already holds are shown as they stand.
    //
    // This list is ngc.desktop/apps, which only shows an app the viewer holds
    // <app>/use for -- and it is a complete list here all the same, because
    // only an admin reaches this app (core.Action.Admin) and an admin holds
    // the right of every installed app without a row (rights.Of, adminApps on
    // the API). Before that it was a closed circle: a freshly installed app
    // was in nobody's menu, so its right was in nobody's list either, and the
    // page said "Es gibt noch keine App, für die es Rechte gäbe." while the
    // app stood installed. Whoever narrows the API's list again breaks THIS
    // page, not just the menu.
    this.apps = Array.isArray(ctx?.apps) ? ctx.apps : [];
    // AND READ AGAIN AT EVERY BUILD. ctx.apps answers the desktop's list as it
    // stands now (shell/desktop.js, appCtx), and a panel that is built again
    // because an app came or went must not build itself from the list that was
    // there when this window opened.
    this.appsNow = () => (Array.isArray(this.ctx?.apps) ? this.ctx.apps : this.apps);
    this.users = null; // ngc.users/list, once, shared by the log and the question
    this.log = [];
    this.loaded = new Set();
    this.dead = false;
    // What closes the question for the person's key when the window goes.
    // confirm.js takes it and answers null on abort, so a dialog can never
    // outlive the app that asked - the same signal apps/store.js hands it.
    this.abort = new AbortController();

    this.root = el("div", "usersapp");
    this.tabs = el("div", "tabs");
    this.tabs.setAttribute("role", "tablist");
    this.tabs.setAttribute("aria-label", "Benutzer und Rechte");
    this.tabs.addEventListener("keydown", (e) => this.tabKey(e));
    this.panels = el("div", "panels");
    this.btn = new Map();
    this.panel = new Map();
    for (const [id, label] of TABS) {
      const b = button("tab", label, null);
      b.setAttribute("role", "tab");
      b.id = "ngc-users-tab-" + id;
      b.setAttribute("aria-controls", "ngc-users-panel-" + id);
      b.addEventListener("click", () => this.show(id));
      this.tabs.append(b);
      this.btn.set(id, b);

      const p = el("div", "panel " + id);
      p.id = "ngc-users-panel-" + id;
      p.setAttribute("role", "tabpanel");
      p.setAttribute("aria-labelledby", b.id);
      p.hidden = true;
      this.panels.append(p);
      this.panel.set(id, p);
    }
    // Since when these screens have been standing, and the one button that
    // asks again. Every panel here is built from answers of the API that
    // another admin can change, and no message of the session link says so: a
    // screen that can be old must not look fresh (shell/stale.js).
    this.fresh = freshness(() => this.rebuild(this.current));
    this.root.append(this.tabs, this.fresh.el, this.panels);
    win.body.append(this.root);

    // WHAT THE PERSON WALKED INTO (report of 19.-21.10.2026): a right granted
    // here and a server taken in here showed up only after Ctrl+F5, because
    // every panel was built exactly once (this.loaded) and nothing ever asked
    // it to be built again. Now whoever changed something says which STATE
    // moved, and the panels that show that state are built again.
    //
    // A SERVER NEEDS NO LINE OF ITS OWN: joinNode writes a new version of the
    // trust list and follows it, which is a change of the list, and the
    // desktop turns every settled version into all four topics
    // (shell/desktop.js, startDesktop). So the machinery that was already
    // there carries it; only the ear was missing.
    const ch = ctx?.changed ?? null;
    this.off = [];
    if (ch !== null) {
      this.off.push(ch.on(NODES, () => this.rebuild("servers")));
      this.off.push(ch.on(USERS, () => this.rebuild("users")));
      this.off.push(ch.on(RIGHTS, () => this.rebuild("rights")));
      // An app that came or went changes which rights there are to hand out.
      this.off.push(ch.on(APPS, () => this.rebuild("rights")));
    }

    this.show(TABS[0][0]);
  }

  destroy() {
    this.dead = true;
    this.abort.abort();
    this.fresh?.stop();
    for (const off of this.off ?? []) off();
    this.off = [];
    this.closeAsk();
  }

  // rebuild draws one panel again. A panel that was never opened is left
  // alone: it is built from scratch when it is shown, so there is nothing
  // stale in it to replace, and asking the API for a screen nobody is looking
  // at would be a question for nothing.
  rebuild(id) {
    if (this.dead || !this.loaded.has(id)) return;
    this.build(id);
  }

  // ---- tabs ---------------------------------------------------------------
  show(id) {
    this.current = id;
    for (const [tab] of TABS) {
      const on = tab === id;
      const b = this.btn.get(tab);
      b.setAttribute("aria-selected", on ? "true" : "false");
      // One tab stop for the whole strip, and the arrows walk it: that is
      // what role="tablist" promises, so that is what it does.
      b.tabIndex = on ? 0 : -1;
      b.classList.toggle("on", on);
      this.panel.get(tab).hidden = !on;
    }
    if (!this.loaded.has(id)) {
      this.loaded.add(id);
      this.build(id);
    }
  }

  tabKey(e) {
    const step = { __proto__: null, ArrowRight: 1, ArrowDown: 1, ArrowLeft: -1, ArrowUp: -1 }[e.key];
    const at = TABS.findIndex(([id]) => id === this.current);
    let to = -1;
    if (step !== undefined) to = (at + step + TABS.length) % TABS.length;
    else if (e.key === "Home") to = 0;
    else if (e.key === "End") to = TABS.length - 1;
    else return;
    e.preventDefault();
    this.show(TABS[to][0]);
    this.btn.get(TABS[to][0]).focus();
  }

  build(id) {
    this.fresh?.done();
    const p = this.panel.get(id);
    if (id === "users") this.buildUsers(p);
    if (id === "rights") this.buildRights(p);
    if (id === "servers") this.buildServers(p);
    if (id === "log") this.buildLog(p);
  }

  // say puts one line into a panel's own status region. It is built with the
  // panel and stays, empty or not: a region that appears only when there is
  // something to say is announced by nothing - the same rule, and the same
  // reason, as the desktop's own notice strip.
  //
  // The class is "note" and not "msg": shell.css already owns ".msg" for the
  // box that carries "NGC konnte nicht geladen werden", 32 rem wide and 20 vh
  // down the page. Measured on the first screenshot of this surface - four
  // empty boxes the size of a card, one under every heading.
  static say(box, txt) {
    text(box.querySelector(".note"), txt);
  }

  // ---- Benutzer -----------------------------------------------------------
  async buildUsers(p) {
    clear(p);
    const msg = el("p", "note", "");
    msg.setAttribute("role", "status");
    const tbl = table(["Name", "Rollen", "Anmeldung in zwei Schritten", "Gesperrt", "Zuletzt angemeldet"]);
    p.append(el("h3", null, "Benutzer"), msg, tbl.el);
    let list;
    try {
      list = await this.call(core.ACT_LIST);
    } catch (e) {
      text(msg, reason(e));
      return;
    }
    if (this.dead) return;
    this.users = list;
    for (const u of list) {
      const r = row();
      r.append(
        cell("nm", u.name),
        cell("roles", u.roles.join(", ")),
        cell("totp", u.totp ? "ein" : "aus"),
        cell("locked", u.locked ? "gesperrt" : ""),
        cell("last", u.lastLogin > 0 ? when(u.lastLogin) : "noch nie"),
      );
      tbl.body.append(r);
    }
  }

  // ---- Rollen und Rechte --------------------------------------------------
  async buildRights(p) {
    clear(p);
    const msg = el("p", "note", "");
    msg.setAttribute("role", "status");
    p.append(
      el("h3", null, "Rollen und Rechte"),
      el("p", "hint", "Ein Haken heißt: alle mit dieser Rolle dürfen das."),
      msg,
    );
    let roles;
    try {
      roles = await this.call(core.ACT_RIGHTS);
    } catch (e) {
      text(msg, reason(e));
      return;
    }
    if (this.dead) return;
    // The rights that may be handed out: what a role already holds, and for
    // every app of this desktop that is not one of NGC's own, "use" and each
    // of its actions. core.validRight is asked for each of them, so nothing
    // stands on screen that the contract would refuse on the way out.
    const known = new Set();
    for (const r of roles) for (const right of r.rights) known.add(right);
    this.apps = this.appsNow();
    for (const a of this.apps) {
      if (!validAppID(a.id) || coreApp(a.id)) continue;
      for (const act of [core.RIGHT_USE, ...(Array.isArray(a.actions) ? a.actions : [])]) {
        known.add(a.id + "/" + act);
      }
    }
    const all = [...known].filter(core.validRight).sort(plain);
    for (const r of roles) p.append(this.roleBox(r, all, msg));
  }

  roleBox(r, all, msg) {
    const box = el("fieldset", "role");
    box.append(el("legend", null, r.role));
    const opts = el("div", "opts");
    const boxes = new Map();
    // The role admin holds every app right on the API itself and holds it
    // without a row (rights.Of, adminApps), so there is nothing here to tick
    // and nothing to clear: a haken that cannot change anything would be a
    // lie, and one that looked clearable would promise a removal the API does
    // not carry out. The rights stand, named, with no box and no save button.
    if (r.role === trust.ROLE_ADMIN) {
      box.append(
        el("p", "hint", "Diese Rolle darf jede eingerichtete App benutzen. Das ist fest und lässt sich nicht abwählen."),
      );
      for (const right of all) opts.append(el("p", "opt fixed", rightName(right, this.apps)));
      if (all.length === 0) opts.append(el("p", "muted", "Es ist noch keine App eingerichtet."));
      box.append(opts);
      return box;
    }
    for (const right of all) {
      const lab = el("label", "opt");
      const cb = el("input", null);
      cb.type = "checkbox";
      cb.checked = r.rights.includes(right);
      // The name a screen reader reads carries the role, because the legend
      // of a fieldset is not part of a checkbox's name: without it every role
      // would offer the same handful of names over and over.
      cb.setAttribute("aria-label", "Rolle " + r.role + ": " + rightName(right, this.apps));
      lab.append(cb, el("span", "t", rightName(right, this.apps)));
      opts.append(lab);
      boxes.set(right, cb);
    }
    if (all.length === 0) opts.append(el("p", "muted", "Es gibt noch keine App, für die es Rechte gäbe."));
    const save = button("go", "Rechte speichern", "Rechte der Rolle " + r.role + " speichern");
    save.addEventListener("click", () => this.saveRights(r, boxes, save, msg));
    box.append(opts, save);
    return box;
  }

  saveRights(r, boxes, save, msg) {
    const want = [...boxes.entries()].filter(([, cb]) => cb.checked).map(([right]) => right).sort(plain);
    const adds = want.filter((x) => !r.rights.includes(x));
    const gone = r.rights.filter((x) => !want.includes(x)).sort(plain);
    if (adds.length === 0 && gone.length === 0) {
      text(msg, "An der Rolle »" + r.role + "« hat sich nichts geändert.");
      return;
    }
    const body = [];
    if (adds.length > 0) {
      body.push(el("p", null, "Alle mit der Rolle »" + r.role + "« dürfen danach zusätzlich:"));
      body.push(list("adds", adds.map((x) => rightName(x, this.apps))));
    }
    if (gone.length > 0) {
      body.push(el("p", null, "Weggenommen wird:"));
      body.push(list("gone", gone.map((x) => rightName(x, this.apps))));
    }
    const who = this.whoHas(r.role);
    if (who) body.push(el("p", "who", who));
    this.ask({
      title: "Rechte der Rolle »" + r.role + "« ändern",
      body,
      go: "Rechte vergeben",
      from: save,
      run: async () => {
        await this.call(core.ACT_SET_RIGHTS, { role: r.role, rights: want });
        r.rights = want;
        text(msg, "Die Rechte der Rolle »" + r.role + "« sind gespeichert.");
        // THE START MENU. A right that was just granted is what decides
        // whether an app stands in it (core.ActApps answers what this user may
        // use), and until this line it did not move until the page was
        // reloaded. The rights panel is NOT asked to build itself again from
        // here: the sentence above it is the answer to the click, and the
        // boxes already show what was sent.
        this.ctx?.changed?.tell(RIGHTS);
      },
      failed: "Die Rechte konnten nicht gespeichert werden.",
      msg,
    });
  }

  // whoHas says how many people a change to this role hits, and who they are.
  // Without the list of users it says nothing at all rather than a number
  // nobody checked.
  whoHas(role) {
    if (this.users === null) return "";
    const names = this.users.filter((u) => u.roles.includes(role)).map((u) => u.name);
    if (names.length === 0) return "Zurzeit hat niemand diese Rolle.";
    return "Das gilt sofort für " + names.length + (names.length === 1 ? " Person" : " Personen")
      + " mit dieser Rolle: " + names.join(", ") + ".";
  }

  // ---- Server -------------------------------------------------------------
  buildServers(p) {
    clear(p);
    const top = el("p", "note", "");
    top.setAttribute("role", "status");
    // What was said right before this panel was built again. Taking a server
    // in replaces this whole panel - that is the point - and the sentence that
    // confirmed it would go with it. So it is carried over, once.
    text(top, this.keep ?? "");
    this.keep = "";
    p.append(el("h3", null, "Server"), top);
    const pending = section("pending", "Wartende Server",
      "Vergleichen Sie diesen Fingerabdruck mit dem, den das Skript auf dem Server anzeigt.");
    const pendapi = section("pendapi", "Wartende API-Server",
      "Vergleichen Sie diesen Fingerabdruck mit dem, den das Skript auf dem Server anzeigt.");
    const known = section("known", "Server dieser Anlage", "");
    const listed = section("listed", "Eingetragene Server",
      "Diese Server stehen in der Vertrauensliste dieser Anlage.");
    const code = section("paircode", "Kopplungscode",
      "Der Code wird nur gezeigt, wenn er zu dieser Anlage passt.");
    const join = section("joincode", "Beitrittscode",
      "Der Code wird nur gezeigt, wenn er zu dieser Anlage passt.");
    p.append(pending, pendapi, known, listed, code, join);
    this.fillPending(pending);
    this.fillPendingAPI(pendapi);
    this.fillNodes(known);
    this.fillListed(listed);
    this.fillPairCode(code);
    this.fillJoinCode(join);
  }

  async fillPending(box) {
    let list;
    try {
      list = await this.call(core.ACT_PENDING);
    } catch (e) {
      Users.say(box, reason(e, BAD_OFFER));
      return;
    }
    if (this.dead) return;
    if (list.length === 0) {
      Users.say(box, "Zurzeit wartet kein Server.");
      return;
    }
    const why = this.cannotChange();
    for (const n of list) {
      const r = el("div", "row");
      // The fingerprint is not in the answer and cannot be: core.PendingNode
      // carries a name, a key and a time. The shell computes it out of the
      // very key it would then write into the trust list. It is kept beside
      // the element as well, because the question below quotes it and must
      // never quote the three dots that stand there while it is computed.
      const seen = { fp: "" };
      const fp = el("span", "fp mono", "…");
      tunnel.fingerprint(n.key).then((f) => {
        seen.fp = f;
        if (!this.dead) text(fp, f);
      }, () => text(fp, "—"));
      r.append(el("span", "nm", n.name), fp, el("span", "since", when(n.since)));
      if (why === "") {
        const b = button("go", "Aufnehmen", "Server " + n.name + " aufnehmen");
        b.addEventListener("click", () => this.askJoin(n, seen, b, box));
        r.append(b);
      }
      box.append(r);
    }
    if (why !== "") Users.say(box, why);
  }

  async fillPendingAPI(box) {
    let list;
    try {
      list = await this.call(core.ACT_PENDING_API);
    } catch (e) {
      Users.say(box, reason(e, BAD_OFFER));
      return;
    }
    if (this.dead) return;
    if (list.length === 0) {
      Users.say(box, "Zurzeit wartet kein API-Server.");
      return;
    }
    const why = this.cannotChange();
    for (const n of list) {
      const r = el("div", "row");
      const seen = { fp: "" };
      const fp = el("span", "fp mono", "…");
      // Exactly node, noise, sign, tls and url, in that order: the server's
      // own script prints the same five, and the admin compares by eye.
      tunnel.joinFingerprint(n.node, n.noise, n.sign, n.tls, n.url).then((f) => {
        seen.fp = f;
        if (!this.dead) text(fp, f);
      }, () => text(fp, "—"));
      r.append(el("span", "nm", n.node), fp, chars(n.url), el("span", "since", when(n.since)));
      // A name that already stands in the trust list cannot be taken in: a
      // version with two nodes of one id is not a valid list (Go:
      // trust.List.Validate wants the ids strictly ascending), so
      // buildNextVersion refuses this change before anything is signed.
      //
      // MEASURED on 17.10.2026 against a running installation, and this is
      // what it is for: a second machine that asks for the name of a server
      // already listed is ACCEPTED by the API, and its request OVERWRITES the
      // waiting one of that name (join.keep stores by node id). The admin then
      // saw this row with the other machine's fingerprint, pressed Aufnehmen,
      // and read "Die Anlage antwortet gerade nicht." - the error really
      // thrown was "Diese Änderung ergibt keine gültige Vertrauensliste.", and
      // reason() has no prefix for it, so it fell to the sentence about
      // reachability. Nothing was unreachable; the name was taken.
      const taken = this.nameTaken(n.node);
      if (taken) {
        r.append(el("span", "note", "Dieser Name ist in dieser Anlage schon vergeben. "
          + "Der Server muss unter einem anderen Namen beitreten."));
      } else if (joinable(n) && why === "") {
        const b = button("go", "Aufnehmen", "Server " + n.node + " aufnehmen");
        b.addEventListener("click", () => this.askJoinAPI(n, seen, b, box));
        r.append(b);
      }
      box.append(r);
    }
    if (why !== "") Users.say(box, why);
  }

  // nameTaken says whether a node of this id already stands in the version
  // the shell verified. Without a version it answers false: cannotChange
  // already says that nothing can be taken in then, and a row would otherwise
  // carry two sentences that say different things.
  nameTaken(id) {
    const list = this.trust?.current?.()?.list;
    if (!list || !Array.isArray(list.nodes)) return false;
    return list.nodes.some((n) => n !== null && typeof n === "object" && n.id === id);
  }

  // fillListed is the only screen that shows what the VERIFIED trust list
  // itself carries, and the only one that can change it afterwards. The rows
  // come out of this.trust.current() and not out of an action of the API: what
  // an admin is about to re-sign has to be the version the shell checked, not
  // a list a server described.
  //
  // It is here because of a measured hole. A node's address was written once,
  // when the admin took the server in, and nothing could ever change it again:
  // counted over the committed interface, no screen changed or removed a node
  // and shared/core had ActNodes to read them and no action to set one. What
  // that cost was measured on 18.10.2026 at a two-machine installation whose
  // servers had got new addresses: Apache went on proxying to the old ones,
  // answered the client 500 on a share of the requests (the certificate of the
  // machine now at that address does not match it, AH00898), and nothing in
  // NextGenCloud could put it right. The server itself repairs its own
  // certificate at the next start; the signed list cannot repair itself.
  //
  // The key is NOT touched by an address change, and that is what makes it a
  // small change: a server keeps its keys when it moves, the pin in the list
  // goes on proving them, and the only thing that was wrong was where to find
  // it.
  fillListed(box) {
    const l = this.trust?.current?.()?.list;
    if (!l || !Array.isArray(l.nodes)) {
      Users.say(box, "Die Vertrauensliste liegt noch nicht vor.");
      return;
    }
    const nodes = l.nodes.filter((n) => n !== null && typeof n === "object");
    const apis = nodes.filter((n) => n.kind === trust.NODE_API);
    if (nodes.length === 0) {
      Users.say(box, "In der Vertrauensliste steht noch kein Server.");
      return;
    }
    const why = this.cannotChange();
    for (const n of nodes) {
      const r = el("div", "row");
      const api = n.kind === trust.NODE_API;
      r.append(el("span", "nm", n.id), el("span", "kind", api ? "API-Server" : "Server"));
      if (api) r.append(chars(n.url));
      // Who may run root tasks there is shown in the row and not only in the
      // question: a server that nobody may set up an app on looks exactly like
      // a working one until somebody tries, and then the refusal says nothing
      // about rights (measured 19.10.2026: "Das hat der Rechner nicht
      // angenommen.", no sentence of the server, no log line anywhere).
      const says = api ? null : el("span", "root", rootWho(l, n));
      if (says !== null) r.append(says);
      if (why === "") {
        // Only an API server has an address: the contract keeps a controller's
        // url empty (Go: trust.List.Validate refuses a controller with one),
        // so there is nothing to change for one and no button that pretends
        // otherwise.
        if (api) {
          const b = button("b", "Adresse ändern", "Adresse von " + n.id + " ändern");
          b.addEventListener("click", () => this.askAddress(n, b, box));
          r.append(b);
        } else {
          // THE WAY BACK FROM A SERVER TAKEN IN WRONG. A controller whose root
          // lists name nobody could do nothing a root task needs, for ever:
          // nothing in this interface could write those two lists after the
          // take-up, and re-pairing the server is not a repair anybody should
          // need for a tick that was missing.
          const b = button("b", "Systemrechte ändern", "Systemrechte auf " + n.id + " ändern");
          b.addEventListener("click", () => this.askRoot(n, b, box, says));
          r.append(b);
        }
        // The last API server is not offered: an installation whose list names
        // none cannot be reached at all any more, and the balancer of the web
        // server would have no member, which makes Apache answer every /api/
        // with 503.
        if (!api || apis.length > 1) {
          const b = button("b", "Stilllegen", "Server " + n.id + " stilllegen");
          b.addEventListener("click", () => this.askRetire(n, b, box, apis.length));
          r.append(b);
        }
      }
      box.append(r);
    }
    if (why !== "") Users.say(box, why);
    else if (apis.length === 1) {
      Users.say(box, "Der einzige API-Server dieser Anlage kann nicht stillgelegt werden. "
        + "Nehmen Sie zuerst einen weiteren auf.");
    }
  }

  // askAddress asks for the new address of an API server and writes it into
  // the next version of the trust list.
  //
  // The address is checked HERE and not by letting the signing fall over: a
  // version that trust.validateList refuses throws a sentence about the list,
  // and reason() turns anything it does not know into "Die Anlage antwortet
  // gerade nicht." -- an answer about reachability for a typo. Measured once
  // already, for a name that was taken.
  askAddress(n, from, box) {
    const feld = el("input", "addr mono");
    feld.type = "text";
    feld.value = n.url;
    feld.setAttribute("aria-label", "Neue Adresse von " + n.id);
    this.ask({
      title: "Adresse von »" + n.id + "« ändern",
      body: [
        el("p", null, "Diese Anlage erreicht den Server danach unter der neuen Adresse. "
          + "Die Schlüssel des Servers bleiben dieselben; nur wo er zu finden ist, ändert sich."),
        feld,
        el("p", null, "Die Adresse sieht so aus: wss://name-des-servers:7443/tunnel. "
          + "Statt des Namens darf auch eine IPv4-Adresse stehen."),
        el("p", null, "Tragen Sie sie nur ein, wenn der Server unter genau dieser Adresse "
          + "erreichbar ist."),
        // The other half of the move, and it is not this screen's to do: the
        // server makes its own certificate for the address IT knows (its
        // api.json), and the web server proxies only to a certificate that
        // carries the address it dials. So the list may be right and the
        // connection still refused, until the server itself was told. It is
        // said here rather than left to be found: measured at a running
        // installation, Apache answers the client 500 in exactly that case
        // (AH00898, "Error during SSL Handshake with remote server").
        el("p", null, "Wichtig: Sagen Sie dem Server auch selbst seine neue Adresse "
          + "(auf dem Server mit ulh, Eintrag ngc-api) und starten Sie ihn neu. Sonst "
          + "weist der Webserver ihn ab, weil sein Zertifikat noch die alte Adresse nennt."),
      ],
      go: "Adresse ändern",
      from,
      run: async () => {
        const neu = String(feld.value).trim();
        if (neu === n.url) {
          Users.say(box, "Das ist die Adresse, die schon eingetragen ist. Es wurde nichts geändert.");
          return;
        }
        if (!validTunnelURL(neu)) {
          Users.say(box, "Das ist keine Adresse, unter der ein API-Server erreichbar ist. "
            + "Sie muss so aussehen: wss://name-des-servers:7443/tunnel");
          return;
        }
        await this.joinNode((next) => {
          const node = next.nodes.find((x) => x !== null && typeof x === "object" && x.id === n.id);
          // The version is built from the list the shell verified, so the node
          // is there; a list it is not in is not one this screen was drawn
          // from, and writing a second node of that id would be a fork of the
          // name rather than a change of the address.
          if (!node) throw new Error("Dieser Server steht nicht mehr in der Vertrauensliste.");
          node.url = neu;
        });
        Users.say(box, "Die Adresse von »" + n.id + "« ist geändert.");
        from.disabled = true;
      },
      failed: "Die Adresse konnte nicht geändert werden.",
      msg: box.querySelector(".note"),
    });
  }

  // askRoot changes who may run root tasks on a controller: the other half of
  // the take-up, and the half that was missing. The boxes start on what the
  // verified list says, so the question shows the state it is about, and the
  // change is written into the next version like every other change of the
  // list - signed by the person, checked by the shell before it is sent.
  //
  // THE KEYS ARE NOT TOUCHED. As with an address, this is a small change: the
  // server keeps every key it has, and only who may tell it to do something as
  // root moves. What it is NOT is cosmetic - this is the right to install and
  // remove software on that machine and to open a terminal on it.
  askRoot(n, from, box, says) {
    const l = this.trust?.current?.()?.list;
    if (!l) {
      Users.say(box, "Die Vertrauensliste liegt noch nicht vor.");
      return;
    }
    const pick = rootPicker(l, n);
    const was = {
      rootUsers: [...(Array.isArray(n.rootUsers) ? n.rootUsers : [])].sort(plain),
      rootRoles: [...(Array.isArray(n.rootRoles) ? n.rootRoles : [])].sort(plain),
    };
    this.ask({
      title: "Systemrechte auf »" + n.id + "« ändern",
      body: [
        el("p", null, "Wer hier steht, darf auf diesem Server Apps einrichten und entfernen, "
          + "Daten löschen und das Terminal öffnen."),
        el("p", null, rootWho(l, n)),
        pick.el,
        el("p", null, "Die Schlüssel des Servers bleiben dieselben. Es ändert sich nur, "
          + "wer ihm Aufgaben mit Systemrechten geben darf."),
      ],
      go: "Systemrechte ändern",
      from,
      run: async () => {
        const want = pick.chosen();
        const why = tooManyRoot(want);
        if (why !== "") {
          Users.say(box, why);
          return;
        }
        if (same(want.rootUsers, was.rootUsers) && same(want.rootRoles, was.rootRoles)) {
          Users.say(box, "Daran hat sich nichts geändert. Es wurde nichts geschrieben.");
          return;
        }
        await this.joinNode((next) => {
          const node = next.nodes.find((x) => isObj(x) && x.id === n.id);
          // The version is built from the list the shell verified, so the node
          // is there; a list it is not in is not the one this screen was drawn
          // from.
          if (!node) throw new Error("Dieser Server steht nicht mehr in der Vertrauensliste.");
          // Set, not merged: what the person ticked IS the answer, and the
          // version is built from the list in force - so a neighbour who
          // changed this node in between is overwritten by a choice that was
          // made with a sentence in front of it, not by a stale half of one.
          node.rootUsers = want.rootUsers;
          node.rootRoles = want.rootRoles;
        });
        Users.say(box, "Die Systemrechte auf »" + n.id + "« sind geändert.");
        // The row has to stop saying the old thing: it is the one place a
        // person reads who may do this, and a line that is now wrong is worse
        // than no line. It is read out of the version THAT NOW STANDS, never
        // out of what was just sent - joinNode followed the chain to it.
        const now = this.trust?.current?.()?.list;
        const node = now?.nodes?.find((x) => isObj(x) && x.id === n.id);
        if (says && now && node) text(says, rootWho(now, node));
        from.disabled = true;
      },
      failed: "Die Systemrechte konnten nicht geändert werden.",
      msg: box.querySelector(".note"),
    });
  }

  // askRetire takes a server out of the trust list. It is the way back from
  // every take-up, and the only one: a node cannot be edited into nothing.
  askRetire(n, from, box, apis) {
    const api = n.kind === trust.NODE_API;
    this.ask({
      title: "Server »" + n.id + "« stilllegen",
      body: [
        el("p", null, "Der Server wird aus der Vertrauensliste dieser Anlage genommen. "
          + "Danach glaubt diese Anlage ihm nicht mehr und nimmt keine Verbindung mehr zu ihm auf."),
        ...(api ? [chars(n.url)] : []),
        el("p", null, "Der Server selbst läuft weiter; seine Dienste hören nicht auf. "
          + "Um ihn wieder aufzunehmen, muss er erneut beitreten."),
      ],
      go: "Server stilllegen",
      from,
      run: async () => {
        // The count is taken again from the version in force and not from what
        // the screen was drawn with: between drawing and clicking a neighbour
        // may have taken the other API server out, and then this one is the
        // last.
        const now = this.trust?.current?.()?.list?.nodes ?? [];
        const left = now.filter((x) => x !== null && typeof x === "object"
          && x.kind === trust.NODE_API).length;
        if (api && left <= 1) {
          Users.say(box, "Das ist der einzige API-Server dieser Anlage. Er kann nicht "
            + "stillgelegt werden, weil die Anlage danach nicht mehr erreichbar wäre.");
          return;
        }
        await this.joinNode((next) => {
          next.nodes = next.nodes.filter((x) => !(x !== null && typeof x === "object"
            && x.id === n.id));
        });
        Users.say(box, "Der Server »" + n.id + "« ist stillgelegt.");
        from.disabled = true;
      },
      failed: "Der Server konnte nicht stillgelegt werden.",
      msg: box.querySelector(".note"),
    });
  }

  async fillNodes(box) {
    const tbl = table(["Server", "Erreichbar", "Aufgaben", "Version", "Maschine"]);
    box.append(tbl.el);
    let list;
    try {
      list = await this.call(core.ACT_NODES);
    } catch (e) {
      Users.say(box, reason(e));
      return;
    }
    if (this.dead) return;
    for (const n of list) {
      const r = row();
      r.append(
        cell("nm", n.node),
        cell("online", n.online ? "ja" : "nein"),
        cell("roles", n.roles.join(", ")),
        cell("version", n.version),
        cell("arch", n.arch),
      );
      tbl.body.append(r);
    }
  }

  fillPairCode(box) {
    const b = button("go", "Kopplungscode zeigen", null);
    b.addEventListener("click", () => this.showCode(box, b, core.ACT_PAIR_CODE, tunnel.parsePairCode));
    box.append(b);
  }

  // A join code is pasted into the script of a FURTHER API server
  // (ngc-api.sh, answer join_code), and it names the same three things a
  // pairing code does. So it is shown under the same one rule, and the
  // apps/web contract names the two together: "Kopplungs- und Beitrittscodes
  // (Kern-Aktionen pair-code und join-code) zeigst du erst, wenn ... stimmt"
  // (apps/web/CLAUDE.md). Without this way nothing could answer that
  // question of the script at all.
  fillJoinCode(box) {
    const b = button("go", "Beitrittscode zeigen", null);
    b.addEventListener("click", () => this.showCode(box, b, core.ACT_JOIN_CODE, tunnel.parseJoinCode));
    box.append(b);
  }

  // A pairing code is pasted into the script of a new controller, a join code
  // into that of a new API server, and both name the API to dial and the
  // version to believe. So either is shown only once tunnel.checkCode has
  // agreed with the version THE SHELL verified - never because the API sent
  // it. If the code names a higher number than the shell has, the chain is
  // fetched first and the question asked again; a code that still does not
  // fit is not put on screen at all, because a code on screen is a code
  // somebody will paste.
  //
  // parse is the reader of the one prefix this action answers with, so a
  // join code shown under the pairing button, or the other way round, is
  // refused before anything reaches the screen: the prefix is part of what
  // the reader checks (contracts/tunnel.js, parseCode).
  async showCode(box, b, action, parse) {
    const old = box.querySelector(".code");
    if (old) old.remove();
    Users.say(box, "");
    b.disabled = true;
    try {
      const r = await this.call(action);
      if (this.dead) return;
      const c = parse(r.code);
      let v = this.trust?.current?.() ?? null;
      // FIXRUNDE 2, K1. This used to call refresh() with NO number and then
      // work with what came back. Both halves were wrong, and each on its own
      // was enough: without a number the hint counts as 0, which is never
      // higher than the version in force, so NOBODY WAS ASKED and the branch
      // that is supposed to catch up caught nothing. And refresh answers with
      // a NUMBER, not with a version (measured against the real TrustStore in
      // users.spec.js) - so v then held a number, v.list?.seq was undefined,
      // !v was false for every number but 0, and tunnel.checkCode was handed
      // a number where a version belongs. The number to ask for is the one
      // the code itself names, and the version in force is read again from
      // current() afterwards.
      if (v?.list && c.anchor.seq > v.list.seq && typeof this.trust.refresh === "function") {
        await this.trust.refresh(c.anchor.seq);
        if (this.dead) return;
        v = this.trust.current();
      }
      if (!v?.list || !tunnel.checkCode(c, v)) {
        Users.say(box, "Dieser Code passt nicht zu dieser Anlage.");
        return;
      }
      box.append(el("p", "code mono", r.code));
      Users.say(box, "Gültig bis " + when(r.expires) + ".");
    } catch (e) {
      Users.say(box, reason(e, "Dieser Code passt nicht zu dieser Anlage."));
    } finally {
      if (!this.dead) b.disabled = false;
    }
  }

  // cannotChange says in one sentence why no server can be taken in right
  // now, and "" when one can. Nothing draws a button this sentence answers:
  // a button that cannot do its job is a promise the surface does not keep.
  cannotChange() {
    const cur = this.trust?.current?.();
    if (!cur?.list) return "Die Vertrauensliste liegt noch nicht vor. Es kann gerade kein Server aufgenommen werden.";
    // A version of the trust list is signed by the PERSON, with a key that
    // stands in the list itself; without knowing whose session this is, there
    // is nobody to ask.
    if (typeof this.session?.user !== "string" || this.session.user === "") {
      return "Dafür ist eine Anmeldung nötig. Es kann gerade kein Server aufgenommen werden.";
    }
    return "";
  }

  askJoin(n, seen, from, box) {
    if (seen.fp === "") {
      Users.say(box, NO_FINGERPRINT);
      return;
    }
    const v = this.trust.current();
    const id = nodeID(n.name, v.list);
    // The box of the person doing this starts ticked, and nothing else does.
    // It can be taken off again, and the answer below says either way what
    // follows from it (rootPicker carries the why).
    const pick = rootPicker(v.list, null, this.session?.user ?? "");
    this.ask({
      title: "Server »" + n.name + "« aufnehmen",
      body: [
        el("p", null, "Der Server wird in die Vertrauensliste dieser Anlage eingetragen. "
          + "Danach darf er Aufgaben dieser Anlage ausführen."),
        el("p", null, "In der Liste heißt er: " + id),
        el("p", "fp mono", seen.fp),
        el("p", null, "Nehmen Sie ihn nur auf, wenn genau dieser Fingerabdruck auf dem Server steht."),
        pick.el,
        el("p", null, "Ihr Haken ist gesetzt. Nehmen Sie ihn weg, wenn Sie auf diesem Server "
          + "selbst nichts einrichten sollen — dann kann es dort zunächst niemand. "
          + "Das lässt sich später unter »Eingetragene Server« ändern."),
      ],
      go: "Server aufnehmen",
      from,
      run: async () => {
        const want = pick.chosen();
        const why = tooManyRoot(want);
        if (why !== "") {
          Users.say(box, why);
          return;
        }
        await this.joinNode((next) => {
          next.nodes.push({
            id, kind: trust.NODE_CONTROLLER, name: n.name, noise: n.key,
            sign: new Uint8Array(0), url: "", tls: new Uint8Array(0),
            // WHAT THE PERSON TICKED, read off the boxes. One box starts
            // ticked (rootPicker, mine), and it is theirs alone; everything
            // else in these two lists got there by a hand. They are the whole
            // reason a taken-in controller can do anything at all
            // (trust.MayRoot), so nothing is added here that no box carried.
            rootUsers: want.rootUsers, rootRoles: want.rootRoles,
          });
        });
        this.keep = "Der Server »" + n.name + "« ist aufgenommen. "
          + (want.rootUsers.length + want.rootRoles.length === 0
            ? "Auf diesem Server darf noch niemand Apps einrichten."
            : "Aufgaben mit Systemrechten dürfen dort: "
              + [...want.rootUsers.map((uid) => userOf(v.list, uid)),
                ...want.rootRoles.map((r) => "Rolle " + r)].join(", ") + ".");
        Users.say(box, this.keep);
        from.disabled = true;
      },
      failed: "Der Server konnte nicht aufgenommen werden.",
      msg: box.querySelector(".note"),
    });
  }

  askJoinAPI(n, seen, from, box) {
    if (seen.fp === "") {
      Users.say(box, NO_FINGERPRINT);
      return;
    }
    this.ask({
      title: "API-Server »" + n.node + "« aufnehmen",
      body: [
        el("p", null, "Der Server wird in die Vertrauensliste dieser Anlage eingetragen. "
          + "Danach nimmt dieses Webinterface Verbindungen zu ihm auf und glaubt ihm."),
        el("p", "fp mono", seen.fp),
        chars(n.url),
        el("p", null, "Nehmen Sie ihn nur auf, wenn genau dieser Fingerabdruck und genau diese "
          + "Adresse auf dem Server stehen."),
      ],
      go: "Server aufnehmen",
      from,
      run: async () => {
        await this.joinNode((next) => {
          next.nodes.push({
            id: n.node, kind: trust.NODE_API, name: n.node, noise: n.noise,
            // BOTH STAY EMPTY HERE, and not by oversight: an API node may
            // carry no root lists at all (Go: ValidAPINode asks for
            // len(RootUsers)+len(RootRoles) == 0), and trust.MayRoot only ever
            // looks at a node of kind controller. A version that named anybody
            // here would be refused as invalid - so this must NOT be made to
            // match askJoin, which asks the person whom to trust.
            sign: n.sign, url: n.url, tls: n.tls, rootUsers: [], rootRoles: [],
          });
        });
        this.keep = "Der Server »" + n.node + "« ist aufgenommen.";
        Users.say(box, this.keep);
        from.disabled = true;
      },
      failed: "Der Server konnte nicht aufgenommen werden.",
      msg: box.querySelector(".note"),
    });
  }

  // joinNode writes the next version of the trust list, has the PERSON sign
  // it, sends it and then follows the list to what now stands.
  //
  // FIXRUNDE 2, THE FORK. This used to send the version and throw the answer
  // away. The version in force here does not move by itself - the API keeps
  // what it was sent, this browser follows the chain over GET /api/v1/trust -
  // so the next server taken in would be built on the SAME prev and get the
  // SAME number: two versions at one seq, a fork of the root everything else
  // hangs from. ngc.users/api/trust answers with the number of the version
  // that now stands (core: TrustResult), and that number is what this asks
  // trust.refresh for. Where the browser did not get there, nothing more is
  // written and the sentence says the one thing that fixes it.
  //
  // FIXRUNDE 2, W1. The signature is NOT the session's. A version of the
  // trust list is verified against a key that stands in the version BEFORE it
  // (Go: shared/trust/trust.go, prev.KeyOwner) - a session key stands in no
  // list, so such a signature would always be ErrSigner. It is the person's
  // own key, asked for with confirm.js, which is also the safer side: whoever
  // changes the root of trust is asked.
  async joinNode(change) {
    const cur = this.trust.current();
    // The hash the version in force was CHECKED with, not one taken again
    // from its bytes: current() carries it because trust.open and
    // trust.advance computed it over the very bytes they read.
    const prev = cur.hash instanceof Uint8Array ? cur.hash : await trust.hash(cur.signed);
    const { payload, proof } = await buildNextVersion({ list: cur.list, hash: prev }, change, {
      // The same clock apps/store.js writes into a version it builds. A trust
      // list holds no field the API's own time could be read out of, and two
      // surfaces of one shell must not write `issued` from two clocks.
      now: Math.floor(Date.now() / 1000),
      signer: (b) => askSignature(PURPOSE_TRUST_LIST, b, "list",
        { list: cur.list, user: this.session.user, signal: this.abort.signal }),
    });
    const { seq } = await ask(this.link,
      core.lookup(core.APP_USERS, core.SERVICE_API, core.ACT_TRUST), null,
      { stream: oneChunk(encodeSigned(payload, proof)) });
    let now = 0;
    try {
      now = await this.trust.refresh?.(seq);
    } catch {
      // A broken chain, a dead connection, a list already out of force: all
      // of them mean the same here - this browser is not on that version.
      now = 0;
    }
    if (!Number.isSafeInteger(now) || now < seq) throw new Behind();
  }

  // ---- Protokoll ----------------------------------------------------------
  async buildLog(p) {
    clear(p);
    const msg = el("p", "note", "");
    msg.setAttribute("role", "status");
    // "Näheres" and not "Grund": the contract has ONE detail field, and what
    // it carries depends on the kind. A refused channel puts the reason for
    // the refusal in it, and that is the line this column was asked for - but
    // a task puts the base64url of its signature there and a login the method
    // the person proved themselves with. A heading that said "Grund" would
    // call an 86-character signature a reason, which is worse than no column:
    // it would be read as the answer to "why did this not work".
    const tbl = table(["Zeit", "Benutzer", "Art", "App", "Aktion", "Server", "Ergebnis",
      "Näheres"]);
    const more = button("go", "Mehr laden", null);
    more.addEventListener("click", () => this.loadLog(tbl, msg, more));
    p.append(el("h3", null, "Protokoll"), msg, tbl.el, more);
    this.log = [];
    this.loadLog(tbl, msg, more);
  }

  async loadLog(tbl, msg, more) {
    more.disabled = true;
    // The next page starts below the last line this one holds: the contract
    // takes the time AND the id, because two entries can share a second.
    const last = this.log[this.log.length - 1];
    const params = { limit: LOG_LIMIT };
    if (last !== undefined) {
      params.beforeTime = last.time;
      params.beforeId = last.id;
    }
    let page;
    try {
      page = await this.call(core.ACT_LOG, params);
    } catch (e) {
      text(msg, reason(e));
      more.disabled = false;
      return;
    }
    if (this.dead) return;
    for (const e of page) {
      this.log.push(e);
      const r = row();
      r.append(
        cell("time", when(e.time)),
        cell("user", this.userName(e.user)),
        cell("kind", KINDS[e.kind] ?? e.kind),
        cell("app", e.app),
        cell("action", e.action),
        cell("node", e.node),
        cell("result", e.result),
        // Text, never markup: the detail comes from the API, so it goes in
        // through the one helper that cannot take markup - el fills
        // textContent. A reason out of the API's closed German list reads as
        // itself; an <img onerror=...> from a compromised API reads as its own
        // characters and does nothing.
        cell("detail", e.detail),
      );
      tbl.body.append(r);
    }
    if (page.length < LOG_LIMIT) {
      text(msg, this.log.length === 0 ? "Das Protokoll ist leer." : "Das ist der Anfang des Protokolls.");
      more.remove();
      return;
    }
    more.disabled = false;
  }

  // The log names a user by id. The name beside it comes from the list this
  // app already fetched; an id nobody knows stands as the id it is, never as
  // a guess.
  userName(id) {
    if (id === "") return "";
    const u = this.users?.find((x) => x.id === id);
    return u ? u.name : id;
  }

  // ---- the question -------------------------------------------------------
  //
  // One dialog for the whole app. It is modal in fact and not only in name:
  // the focus goes into it, Tab comes round inside it, Escape cancels, the
  // focus goes back where it came from, and the rest of the window is set
  // inert while it stands. It opens on Abbrechen, never on the button that
  // gives something away.
  //
  // The inert is not decoration beside the Tab trap. aria-modal="true" tells a
  // screen reader that nothing outside this question exists; the trap only
  // keeps the Tab key inside, and a reader's own cursor walks past it. Every
  // browser NGC asks for has inert (Chrome 102, Safari 15.5, Firefox 112 -
  // shell/main.js turns away everything older than Chrome 137, Firefox 130 and
  // Safari 17.4), so the promise the attribute makes is one that holds.
  ask({ title, body, go, from, run, failed, msg }) {
    this.closeAsk();
    const back = el("div", "askback");
    const box = el("div", "ask");
    box.setAttribute("role", "dialog");
    box.setAttribute("aria-modal", "true");
    const h = el("h2", null, title);
    h.id = "ngc-users-ask-title";
    box.setAttribute("aria-labelledby", h.id);
    box.append(h, ...body);
    const btns = el("div", "btns");
    const no = button("no", "Abbrechen", null);
    const yes = button("go danger", go, null);
    btns.append(no, yes);
    box.append(btns);
    back.append(box);

    const stop = () => {
      this.closeAsk();
      if (from && from.isConnected) from.focus();
    };
    no.addEventListener("click", stop);
    yes.addEventListener("click", async () => {
      yes.disabled = true;
      no.disabled = true;
      // THE QUESTION GOES BEFORE THE WORK, and that is not tidiness. Joining
      // a server asks confirm.js for the person's key next, and that is a
      // modal of its own. Two dialogs that both say aria-modal="true" are two
      // promises that nothing else exists, and a screen reader walks straight
      // from one into the other. One at a time: this one has been answered,
      // so it goes. What run() has to say afterwards lands in the panel's own
      // status line, which is still on screen behind it.
      stop();
      try {
        await run();
      } catch (e) {
        // Three different things to say: the person said no, the write went
        // through but this browser is behind it, or it did not go through.
        if (msg) {
          text(msg, e instanceof Cancelled ? "Nichts geändert."
            : e instanceof Behind ? BEHIND : reason(e, failed));
        }
      }
    });
    back.addEventListener("keydown", (e) => {
      if (e.key === "Escape") {
        e.preventDefault();
        stop();
        return;
      }
      if (e.key !== "Tab") return;
      // EVERY STOP OF THE QUESTION, IN THE ORDER IT STANDS, and Tab walks
      // between them. Anything outside the question is behind it and must not
      // be reachable while it stands.
      //
      // This used to be [no, yes] and nothing else, which was true as long as
      // the body of a question was only sentences. It no longer is: the
      // question that gives root on a server is a list of checkboxes, and
      // under the old two stops not one of them could be reached with the
      // keyboard - the trap took Tab away and gave it to a button. A question
      // that can only be answered with a mouse is a question this file's own
      // rules refuse. The address field had the same hole and is the same fix.
      const stops = [...box.querySelectorAll("input,button")].filter((b) => !b.disabled);
      if (stops.length === 0) return;
      const i = stops.indexOf(document.activeElement);
      e.preventDefault();
      const next = e.shiftKey ? i - 1 : i + 1;
      stops[(next + stops.length) % stops.length].focus();
    });
    this.tabs.setAttribute("inert", "");
    this.panels.setAttribute("inert", "");
    this.root.append(back);
    this.askBox = back;
    no.focus();
  }

  closeAsk() {
    this.tabs.removeAttribute("inert");
    this.panels.removeAttribute("inert");
    if (this.askBox) {
      this.askBox.remove();
      this.askBox = null;
    }
  }

  // ---- one call -----------------------------------------------------------
  call(name, params) {
    return ask(this.link, core.lookup(core.APP_USERS, core.SERVICE_API, name), params ?? null);
  }
}

// ------------------------------------------------------------------ helpers

// reason turns whatever went wrong into one sentence for the person. An
// answer the contract refused and a server that did not answer are two
// different things to say, and neither of them says anything about the
// inside of the server.
//
// The three prefixes are the three contracts this app reads through: core for
// every result, tunnel for a pairing code, trust for a version it built
// itself. All three mean the same to the person - what came back does not
// hold up - and everything else means the connection.
const REFUSALS = ["core: ", "tunnel: ", "trust: "];
function reason(e, refused = BAD_LIST) {
  const m = String(e?.message ?? "");
  if (!REFUSALS.some((p) => m.startsWith(p))) return NO_REACH;
  // Where the contract named the field it fell over, the sentence names it
  // too. It is a field name out of the contract's own table, not a word off
  // the wire, and it is the one thing that tells the admin where to look -
  // the sentence below says only that something was wrong
  // (docs/vertraege-v1.2.md, point 71).
  const bad = core.refusedField(e);
  return bad === "" ? refused : "Eine Angabe dieses Servers ist unlesbar (" + bad + ").";
}

// chars puts an address on screen character by character, each in its own
// element. It is the one place in NGC where that is worth the nodes: an
// admin is about to let this host into the installation, and a host that
// merely LOOKS like the right one is what this makes visible.
function chars(s) {
  const box = el("span", "url mono");
  box.setAttribute("aria-label", "Adresse " + s);
  for (const c of String(s)) box.append(el("span", "ch", c));
  return box;
}

function rightName(right, apps) {
  const cut = right.indexOf("/");
  const id = right.slice(0, cut), action = right.slice(cut + 1);
  const a = apps.find((x) => x.id === id);
  return (a ? a.name : id) + " · " + (action === core.RIGHT_USE ? "benutzen" : action);
}

function list(cls, items) {
  const ul = el("ul", cls);
  for (const t of items) ul.append(el("li", null, t));
  return ul;
}

function section(cls, head, hint) {
  const s = el("section", cls);
  s.append(el("h4", null, head));
  if (hint) s.append(el("p", "hint", hint));
  const msg = el("p", "note", "");
  msg.setAttribute("role", "status");
  s.append(msg);
  return s;
}

function table(heads) {
  const t = el("table", null);
  const thead = el("thead", null);
  const tr = el("tr", null);
  for (const h of heads) {
    const th = el("th", null, h);
    th.scope = "col";
    tr.append(th);
  }
  thead.append(tr);
  const body = el("tbody", null);
  t.append(thead, body);
  return { el: t, body };
}

const row = () => el("tr", "row");
const cell = (cls, txt) => el("td", cls, txt);

// when shows a time of the API as the person's own clock writes it. The
// number is seconds, as every time in the contracts is.
function when(seconds) {
  const d = new Date(seconds * 1000);
  return d.toLocaleDateString("de-DE", { day: "2-digit", month: "2-digit", year: "numeric" })
    + " " + d.toLocaleTimeString("de-DE", { hour: "2-digit", minute: "2-digit" });
}

// usersApp builds the surface into the window's body and hands back the
// function that tidies up when the window closes. It is not a promise: the
// desktop takes what this returns as win.onClose, and a promise there would
// be a window that cannot be closed. Everything that has to be fetched is
// fetched behind it, and every panel says for itself how far it has got.
export function usersApp(win, ctx) {
  const a = new Users(win, ctx);
  return () => a.destroy();
}

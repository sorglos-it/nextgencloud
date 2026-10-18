// The app store: where apps come from, what is installed, what is new, and
// how a package of one's own gets onto this installation.
//
// It is the first app anybody opens, so it is the one place where "usable
// without being explained" is not a nicety. Five tabs, one thing on each,
// and every message says what to DO - never what went wrong inside.
//
// THE ONE RULE THIS FILE IS BUILT ON: whatever the shell acts on, the shell
// checked itself.
//
//   - A catalog counts once the key of ITS store, taken out of the verified
//     trust list, has signed it (catalog.verifyCatalog). The API hands the
//     document over and vouches for nothing.
//   - An upload counts once the shell's own reading of the file
//     (pack.packageUI) matches what came back. core.UploadResult is compared,
//     never believed.
//   - A task carries the numbers the shell computed and no others, and it is
//     signed before it leaves (task.buildTask, task.signTask).
//   - A standing approval carries a key out of the trust list or out of the
//     initial setup. core.UpdateInfo.key is shown to nobody and used for
//     nothing - see approvalKey.
//
// Every name, every version and every address here comes from outside and
// goes on screen through dom.js, which writes text nodes and nothing else.
import { el, button, text, clear } from "../shell/dom.js";
import { askConfirm, askSignature, needConfirm } from "../shell/confirm.js";
import * as core from "../contracts/core.js";
import * as catalog from "../contracts/catalog.js";
import * as task from "../contracts/task.js";
import * as trust from "../contracts/trust.js";
import { b64encode, equal, utf8 } from "../contracts/b64.js";
import { Budget, decode } from "../contracts/json.js";
import { packageUI } from "../contracts/pack.js";
import { proofShape, validateProof } from "../contracts/proof.js";
import { PURPOSE_TRUST_LIST, parseEnvelope } from "../contracts/sign.js";
// Only for CODES: one refusal of the controller (FORBIDDEN) is worth its own
// sentence, and the code it is read from belongs to the contract, not to this
// app.
import * as wire from "../contracts/wire.js";

const EMPTY = new Uint8Array(0);

// A store the verified trust list does not carry. It is NOT a bad catalog:
// nothing was checked at all, because there is no key to check against, and
// the person can do something about it that they can do about nothing else -
// put the store in the list.
class Untrusted extends Error {}

// The actions this app uses, looked up ONCE out of the frozen table. A row
// that is no row of the contract answers null here and would fail loudly at
// the first call, rather than opening a channel for an action nobody wrote.
const A = {
  __proto__: null,
  sources: core.lookup(core.APP_STORE, core.SERVICE_API, core.ACT_SOURCES),
  catalog: core.lookup(core.APP_STORE, core.SERVICE_API, core.ACT_CATALOG),
  installed: core.lookup(core.APP_STORE, core.SERVICE_API, core.ACT_INSTALLED),
  upload: core.lookup(core.APP_STORE, core.SERVICE_API, core.ACT_UPLOAD),
  license: core.lookup(core.APP_STORE, core.SERVICE_API, core.ACT_LICENSE),
  installKey: core.lookup(core.APP_STORE, core.SERVICE_API, core.ACT_INSTALL_KEY),
  updates: core.lookup(core.APP_STORE, core.SERVICE_API, core.ACT_UPDATES),
  setUpdate: core.lookup(core.APP_STORE, core.SERVICE_API, core.ACT_SET_UPDATE),
  nodes: core.lookup(core.APP_USERS, core.SERVICE_API, core.ACT_NODES),
  trust: core.lookup(core.APP_USERS, core.SERVICE_API, core.ACT_TRUST),
  activate: core.lookup(core.APP_STORE, core.SERVICE_CONTROLLER, core.ACT_ACTIVATE),
  uninstall: core.lookup(core.APP_STORE, core.SERVICE_CONTROLLER, core.ACT_UNINSTALL),
  install: core.lookup(core.APP_STORE, core.SERVICE_CONTROLLER, core.ACT_INSTALL),
};

// The five tabs, in the order the brief names them. "Apps" is the one the
// window opens on, because that is what somebody came here for; the sources
// are for the administrator and stand first only in reading order.
const TABS = [["quellen", "Quellen"], ["apps", "Apps"], ["installiert", "Installiert"],
  ["hochladen", "Hochladen"], ["updates", "Updates"]];
const TAB_APPS = 1;

// What a right MEANS, in words somebody can judge. "network" and "root" are
// the contract's names and say nothing to the person who has to allow them.
const NEED_TEXT = {
  __proto__: null,
  [catalog.NEED_NETWORK]: "Zugang zum Netz",
  [catalog.NEED_ROOT]: "Volle Rechte in ihrem Container",
};

// The parts of NGC by their name on screen. An app stands here under its id,
// which is all an update row carries.
const PART_TEXT = {
  __proto__: null,
  [trust.AUTO_WEB]: "NGC Webinterface",
  [trust.AUTO_API]: "NGC API",
  [trust.AUTO_CONTROLLER]: "NGC Dienste",
  [trust.AUTO_SYSTEM]: "Betriebssystem",
};

const STATE_TEXT = {
  __proto__: null,
  [catalog.UPDATE_AVAILABLE]: "Neue Fassung da",
  [catalog.UPDATE_DOWNLOADED]: "Bereit zum Einspielen",
  [catalog.UPDATE_INSTALLING]: "Wird eingespielt",
  [catalog.UPDATE_INSTALLED]: "Eingespielt",
  [catalog.UPDATE_ROLLEDBACK]: "Zurückgenommen",
  [catalog.UPDATE_FAILED]: "Hat nicht geklappt",
  [catalog.UPDATE_REJECTED]: "Abgelehnt",
  [catalog.UPDATE_NEEDSGRANTS]: "Braucht neue Rechte",
};

const APP_STATE_TEXT = {
  __proto__: null,
  ok: "Läuft",
  unreachable: "Nicht erreichbar",
  unlicensed: "Keine gültige Lizenz",
};

const MODES = [[catalog.MODE_AUTO, "automatisch"], [catalog.MODE_SEMI, "auf Klick"],
  [catalog.MODE_NOTIFY, "nur melden"]];

// What the controller has to be for an app with services to run on it.
const ROLE_SERVICES = "services";

// storeApp builds the store into the window's body and hands back the
// function that tidies up when the window closes.
export function storeApp(win, ctx) {
  const s = new Store(win, ctx ?? {});
  s.build();
  s.select(TAB_APPS);
  return () => s.stop();
}

class Store {
  constructor(win, ctx) {
    this.win = win;
    this.ctx = ctx;
    // Every question the store asks a person hangs on this: closing the
    // window takes the question with it, so a dialog can never outlive the
    // app that asked it.
    this.abort = new AbortController();
    this.cache = { __proto__: null };
    this.turn = 0;
    this.at = TAB_APPS;
    this.source = "";
  }

  stop() {
    this.abort.abort();
  }

  // ---- the frame ---------------------------------------------------------
  build() {
    const box = el("div", "store app");
    const bar = el("div", "tabs");
    // A tablist is walked with the arrows and is ONE stop on the way through
    // with Tab. The word is written here, so the behaviour is written below
    // (key): a role that promises what it does not do is worse than no role.
    bar.setAttribute("role", "tablist");
    bar.setAttribute("aria-label", "App-Store");
    this.panel = el("div", "storepanel");
    this.panel.id = "storepanel-" + this.win.id;
    this.panel.setAttribute("role", "tabpanel");
    // The panel is a tab stop of its own: what the arrows selected has to be
    // reachable without a mouse, and the panel scrolls.
    this.panel.tabIndex = 0;
    this.tabs = TABS.map(([key, name], i) => {
      const b = button("tab", name, null);
      b.id = "storetab-" + this.win.id + "-" + key;
      b.setAttribute("role", "tab");
      b.setAttribute("aria-controls", this.panel.id);
      b.addEventListener("click", () => this.select(i));
      bar.append(b);
      return b;
    });
    bar.addEventListener("keydown", (e) => this.key(e));
    box.append(bar, this.panel);
    clear(this.win.body);
    this.win.body.append(box);
  }

  select(i) {
    this.at = Math.max(0, Math.min(this.tabs.length - 1, i));
    this.tabs.forEach((b, n) => {
      const on = n === this.at;
      b.setAttribute("aria-selected", on ? "true" : "false");
      b.classList.toggle("on", on);
      // Exactly one tab carries the one tabindex of the list; the others are
      // reached with the arrows, as the role promises.
      b.tabIndex = on ? 0 : -1;
    });
    this.panel.setAttribute("aria-labelledby", this.tabs[this.at].id);
    this.render();
  }

  key(e) {
    const how = { __proto__: null, ArrowRight: 1, ArrowLeft: -1, Home: "first", End: "last" }[e.key];
    if (how === undefined) return;
    e.preventDefault();
    const n = how === "first" ? 0
      : how === "last" ? this.tabs.length - 1
        : (this.at + how + this.tabs.length) % this.tabs.length;
    this.select(n);
    this.tabs[this.at].focus();
  }

  // render fills the panel. The turn number is what keeps two quick clicks
  // from painting each other over: only the newest one may write.
  async render() {
    const n = ++this.turn;
    clear(this.panel);
    this.panel.append(el("p", "muted", "Einen Moment …"));
    const draw = [this.drawSources, this.drawApps, this.drawInstalled, this.drawUpload, this.drawUpdates][this.at];
    let parts;
    try {
      parts = await draw.call(this);
    } catch {
      // Why an answer was no good is nothing the person can act on, and
      // saying more would describe the inside of the server.
      parts = [el("p", "fail", "Das hat nicht geklappt. Bitte das Fenster schließen und noch einmal öffnen.")];
    }
    if (n !== this.turn || this.abort.signal.aborted) return;
    clear(this.panel);
    this.panel.append(...parts);
  }

  say(level, txt) {
    this.ctx.notice?.(level, txt);
  }

  // ---- asking the API -----------------------------------------------------
  //
  // The params are written HERE, by the contract's own writer, and the bytes
  // are what travel: a caller that handed an object over and let something
  // else write it would sign one document and send another.
  //
  // THE ONE PLACE THAT TOUCHES THE LINK, and the one that changes when the
  // login lands and a real net/link.js Link arrives in place of the stand-in.
  // What it becomes is decided by the action's own flow column
  // (core.ACTIONS), not by a name written here:
  //
  //   FLOW_CALL, FLOW_TRANSFER  -> channel.call(link, action,
  //                                  {params, taskBytes, confirmBytes})
  //   FLOW_IN                   -> channel.sendIn(link, action,
  //                                  {params, taskBytes, stream, onProgress})
  //
  // Those are net/channel.js as it stands since Task 17, member for member -
  // the option bag below is its, which is why it is the bag this hands over.
  // net/testlink.js has a shortcut of the same NAME that takes params as an
  // object and drops the bag; it is the tests' convenience and not the
  // interface this will use.
  async ask(action, params, opts) {
    const bytes = params === null || params === undefined ? null : core.encodeParams(action, params);
    const answer = await this.ctx.link.call(action.app, action.service, action.name, bytes, opts ?? null);
    return core.parseResult(action, answer);
  }

  async need(name, action) {
    if (this.cache[name] === undefined) this.cache[name] = await this.ask(action, null);
    return this.cache[name];
  }

  // ---- Quellen ------------------------------------------------------------
  async drawSources() {
    const rows = await this.need("sources", A.sources);
    const out = [head("Quellen"), el("p", "muted", "Von hier holt NGC seine Apps.")];
    const box = el("div", "rows");
    for (const s of rows) {
      const r = el("div", "row srcrow");
      const main = el("div", "rowmain");
      main.append(el("b", null, s.name), el("span", "muted", s.url));
      r.append(main, el("div", "rowside", s.checked > 0 ? "Zuletzt geprüft: " + when(s.checked) : "Noch nicht geprüft"));
      // The error the API sends is its own wording and may name anything at
      // all. What is shown is the one sentence the person can act on.
      if (s.error !== "") r.append(el("p", "warnline", "Diese Quelle antwortet nicht. Bitte die Adresse prüfen."));
      box.append(r);
    }
    if (rows.length === 0) box.append(el("p", "muted", "Noch keine Quelle eingetragen."));
    out.push(box);
    return out;
  }

  // ---- Apps ---------------------------------------------------------------
  async drawApps() {
    const sources = await this.need("sources", A.sources);
    const out = [head("Apps")];
    if (sources.length === 0) {
      out.push(el("p", "fail", "Noch keine Quelle. Bitte im Reiter „Quellen“ eine eintragen."));
      return out;
    }
    if (!sources.some((s) => s.id === this.source)) this.source = sources[0].id;
    if (sources.length > 1) {
      const sel = el("select", "source");
      sel.setAttribute("aria-label", "Quelle");
      for (const s of sources) {
        const o = el("option", null, s.name);
        o.value = s.id;
        if (s.id === this.source) o.selected = true;
        sel.append(o);
      }
      sel.addEventListener("change", () => {
        this.source = sel.value;
        this.render();
      });
      out.push(field("Quelle", sel));
    }
    const src = sources.find((s) => s.id === this.source);
    let apps;
    try {
      apps = await this.catalogOf(src);
    } catch (e) {
      // Three different things go wrong here and each gets its own sentence,
      // because each has a different thing to DO about it: a store nobody
      // has vouched for yet, a store replaying an old catalog, and an answer
      // nobody signed for. One sentence for all three would tell the person
      // nothing they could act on.
      out.push(el("p", "fail", e instanceof Untrusted
        ? "Diesem Store wird noch nicht vertraut. Bitte ihn zuerst eintragen."
        : e instanceof catalog.Rollback
          ? "Dieser Store schickt einen alten Katalog. Bitte es später noch einmal versuchen."
          : "Der Katalog dieses Stores ist nicht echt."));
      return out;
    }
    const box = el("div", "rows");
    for (const e of apps) {
      // The newest version is the last one: a catalog's versions ascend, and
      // verifyCatalog refused it if they did not.
      const v = e.versions[e.versions.length - 1];
      const r = el("div", "row approw");
      const main = el("div", "rowmain");
      main.append(el("b", null, e.name), el("span", "muted", e.publisher + " · " + v.version));
      if (e.description !== "") main.append(el("span", "desc", e.description));
      const side = el("div", "rowside");
      if (e.price === null) {
        const b = button("b", "Installieren", e.name + " installieren");
        b.addEventListener("click", () => this.openInstall(r, {
          app: e.id, name: e.name, version: v.version, sha256: v.sha256, ui: v.ui,
          store: src.id, needs: v.needs,
        }));
        side.append(b);
      } else {
        side.append(el("span", "muted", money(e.price)));
        const b = button("b", "Kaufen", e.name + " kaufen");
        b.addEventListener("click", () => this.openBuy(r, e));
        side.append(b);
      }
      r.append(main, side);
      box.append(r);
    }
    if (apps.length === 0) box.append(el("p", "muted", "Dieser Store hat noch keine App."));
    out.push(box);
    return out;
  }

  // catalogOf fetches the catalog of src and checks it against the key THAT
  // STORE has in the verified trust list. A store the list does not carry is
  // a store nobody vouches for, and its catalog is refused the same way a
  // forged signature is - there is no key to tell the two apart with.
  async catalogOf(src) {
    const { list } = this.ctx.trust?.current?.() ?? {};
    const stores = Array.isArray(list?.stores) ? list.stores : [];
    const st = stores.find((s) => s.id === src.id);
    if (st === undefined) throw new Untrusted("store: not in the trust list");
    const res = await this.ask(A.catalog, { store: src.id });
    // src.seq is the newest version this installation has seen of this
    // store, so an older catalog is a replay and says so (catalog.Rollback).
    const c = await catalog.verifyCatalog(res.catalog, st, src.seq);
    return c.apps;
  }

  // openBuy shows what the shop asks for. NGC opens no shop and takes no
  // money: the person buys where they buy, and brings the licence back.
  async openBuy(row, entry) {
    for (const old of row.parentNode.querySelectorAll(".buy, .installbox")) old.remove();
    const box = el("div", "buy");
    let key;
    try {
      key = await this.ask(A.installKey, null);
    } catch {
      this.say("error", "Der Schlüssel dieser Installation kam nicht an. Bitte es noch einmal versuchen.");
      return;
    }
    if (this.abort.signal.aborted) return;
    const keyText = b64encode(key.key);
    box.append(el("p", null, "Der Shop fragt nach diesen beiden Angaben:"));
    box.append(pair("Installation", key.installation), pair("Schlüssel", keyText));
    const copy = button("b", "Kopieren", "Installation und Schlüssel kopieren");
    copy.addEventListener("click", () => this.copy("Installation: " + key.installation + "\nSchlüssel: " + keyText));
    box.append(copy);
    box.append(el("p", "muted", "Shop: " + entry.price.shop));
    box.append(el("p", null, "Die Lizenzdatei aus dem Shop hier auswählen:"));
    const lab = el("label", "filelab");
    const pick = el("input", null);
    pick.type = "file";
    lab.append(el("span", null, "Lizenz wählen"), pick);
    pick.addEventListener("change", () => this.sendLicense(pick));
    box.append(lab);
    row.after(box);
  }

  async sendLicense(pick) {
    const file = pick.files?.[0];
    if (!file) return;
    if (file.size > catalog.MAX_LICENSE) {
      this.say("warn", "Diese Datei ist zu groß für eine Lizenz. Bitte die Datei aus dem Shop wählen.");
      return;
    }
    try {
      const res = await this.ask(A.license, null, { stream: file.stream() });
      this.say("info", res.state === "valid" ? "Die Lizenz gilt." : "Die Lizenz wurde angenommen.");
    } catch {
      this.say("error", "Diese Lizenz wurde nicht angenommen. Bitte die Datei aus dem Shop wählen.");
    }
  }

  async copy(what) {
    try {
      await navigator.clipboard.writeText(what);
      this.say("info", "Kopiert.");
    } catch {
      this.say("warn", "Kopieren geht hier nicht. Bitte den Text von Hand markieren.");
    }
  }

  // ---- Installiert --------------------------------------------------------
  async drawInstalled() {
    const rows = await this.need("installed", A.installed);
    const out = [head("Installiert")];
    const box = el("div", "rows");
    for (const i of rows) {
      const r = el("div", "row instrow");
      const main = el("div", "rowmain");
      main.append(el("b", null, i.id), el("span", "muted", i.version + " · " + i.node));
      main.append(el("span", null, (i.active ? "" : "Ausgeschaltet · ") + (APP_STATE_TEXT[i.state] ?? "")));
      const side = el("div", "rowside");
      const b = button("b", "Entfernen", i.id + " entfernen");
      b.addEventListener("click", () => this.remove(i));
      side.append(b);
      r.append(main, side);
      box.append(r);
    }
    if (rows.length === 0) box.append(el("p", "muted", "Noch keine App eingerichtet."));
    out.push(box);
    return out;
  }

  async remove(i) {
    // keepData false, so the app's data goes with it: that is what makes
    // this a "critical" task (core.critical), and the policy of the trust
    // list decides whether it has to be confirmed.
    const ok = await this.rootTask(A.uninstall, { app: i.id, keepData: false }, i.node,
      "Die App ist entfernt.");
    if (ok) {
      this.cache.installed = undefined;
      this.render();
    }
  }

  // ---- Hochladen ----------------------------------------------------------
  async drawUpload() {
    const out = [head("Hochladen"), el("p", "muted", "Ein Paket von der Festplatte einrichten.")];
    const lab = el("label", "filelab");
    const pick = el("input", null);
    pick.type = "file";
    lab.append(el("span", null, "Paket wählen"), pick);
    const facts = el("div", "upfacts");
    facts.hidden = true;
    const zone = el("div", "upzone");
    pick.addEventListener("change", () => this.weigh(pick, facts, zone));
    out.push(lab, facts, zone);
    return out;
  }

  // weigh reads the package the person chose, shows what the SHELL made of
  // it, sends it and compares. Nothing can be activated before the two
  // agree.
  async weigh(pick, facts, zone) {
    clear(zone);
    clear(facts);
    facts.hidden = true;
    const file = pick.files?.[0];
    if (!file) return;
    let mine;
    try {
      // Whatever the shell signs it computes itself. The API's answer is only
      // compared, never trusted (shared/README.md, rule "Selbst rechnen").
      mine = await packageUI(file.stream());
    } catch {
      zone.append(el("p", "upfail", "Diese Datei ist kein NGC-Paket. Bitte eine Datei mit der Endung .ngc wählen."));
      return;
    }
    if (this.abort.signal.aborted) return;
    facts.hidden = false;
    facts.append(
      pair("App", mine.manifest.name + " (" + mine.manifest.id + ")"),
      pair("Version", mine.manifest.version),
      pair("Prüfsumme", b64encode(mine.packageSha256)),
      pair("UI-Prüfsumme", b64encode(mine.sha256)),
    );
    const sending = el("p", "muted", "Wird übertragen …");
    zone.append(sending);
    let theirs;
    try {
      theirs = await this.ask(A.upload, null, {
        stream: file.stream(),
        onProgress: (n) => text(sending, "Wird übertragen … " + Math.round(n / 1024) + " KB"),
      });
    } catch {
      clear(zone);
      zone.append(el("p", "upfail", "Das Paket kam nicht an. Bitte es noch einmal wählen."));
      return;
    }
    if (this.abort.signal.aborted) return;
    clear(zone);
    if (!equal(mine.packageSha256, theirs.sha256) || !equal(mine.sha256, theirs.ui)
      || mine.manifest.id !== theirs.id || mine.manifest.version !== theirs.version) {
      zone.append(el("p", "upfail", "Die Anlage hat ein anderes Paket bekommen, als geschickt wurde."));
      return;
    }
    zone.append(await this.installBox({
      app: mine.manifest.id, name: mine.manifest.name, version: mine.manifest.version,
      sha256: mine.packageSha256, ui: mine.sha256, store: core.LOCAL_STORE,
      needs: mine.manifest.needs,
    }));
  }

  // openInstall puts the same box under a row of the Apps tab. The values in
  // it come out of the catalog the shell verified, not out of a fresh answer.
  async openInstall(row, spec) {
    for (const old of row.parentNode.querySelectorAll(".buy, .installbox")) old.remove();
    const box = await this.installBox(spec);
    if (!this.abort.signal.aborted) row.after(box);
  }

  // installBox is the one place an activation is set up, for a package that
  // was uploaded and for one out of a catalog alike: the rights to allow and
  // the machine to run it on.
  async installBox(spec) {
    const box = el("div", "installbox");
    const ticks = [];
    if (spec.needs.length > 0) {
      box.append(el("p", null, "Diese App möchte mehr dürfen als andere. Bitte jedes Recht einzeln erlauben:"));
      for (const n of spec.needs) {
        const lab = el("label", "tick");
        const cb = el("input", null);
        cb.type = "checkbox";
        // The box is inside its label, so the words beside it ARE its name -
        // no id to keep in step, and a screen reader reads the right thing.
        lab.append(cb, el("span", null, NEED_TEXT[n] ?? n));
        box.append(lab);
        ticks.push(cb);
      }
    }
    let nodes = [];
    try {
      nodes = await this.need("nodes", A.nodes);
    } catch {
      box.append(el("p", "fail", "Die Rechner konnten nicht geladen werden. Bitte das Fenster neu öffnen."));
      return box;
    }
    // Only a controller that is up and carries the services role can run an
    // app. Offering any other is offering a machine the task would die on.
    const usable = nodes.filter((n) => n.online && n.roles.includes(ROLE_SERVICES));
    if (usable.length === 0) {
      box.append(el("p", "fail", "Kein Rechner für Dienste da. Bitte zuerst einen Rechner hinzufügen."));
      return box;
    }
    const sel = el("select", "target");
    sel.setAttribute("aria-label", "Rechner");
    for (const n of usable) {
      const o = el("option", null, n.node);
      o.value = n.node;
      sel.append(o);
    }
    box.append(field("Rechner", sel));
    const go = button("b on", "Aktivieren", null);
    go.addEventListener("click", async () => {
      if (ticks.some((c) => !c.checked)) {
        this.say("warn", "Bitte alle Rechte erlauben. Ohne sie kann die App nicht arbeiten.");
        return;
      }
      go.disabled = true;
      // grants IS needs: the app gets what its manifest asks for, whole, or
      // it is not activated at all. There is no half-allowed app.
      const ok = await this.rootTask(A.activate, {
        app: spec.app, version: spec.version, sha256: spec.sha256, ui: spec.ui,
        store: spec.store, grants: spec.needs,
      }, sel.value, "Die App ist eingerichtet.");
      go.disabled = false;
      if (ok) this.cache.installed = undefined;
    });
    box.append(go);
    return box;
  }

  // ---- Updates ------------------------------------------------------------
  async drawUpdates() {
    const rows = await this.need("updates", A.updates);
    const out = [head("Updates"), el("p", "muted", "Was es an neuen Fassungen gibt.")];
    const box = el("div", "rows");
    for (const u of rows) {
      const name = PART_TEXT[u.target] ?? u.target;
      const r = el("div", "row uprow");
      const main = el("div", "rowmain");
      main.append(el("b", null, name));
      main.append(el("span", "muted", u.available !== "" && u.installed !== ""
        ? u.installed + " → " + u.available
        : u.installed || "—"));
      if (u.state !== "") main.append(el("span", null, STATE_TEXT[u.state] ?? ""));
      const side = el("div", "rowside");
      const sel = el("select", "mode");
      sel.setAttribute("aria-label", "Updates für " + name);
      for (const [v, t] of MODES) {
        const o = el("option", null, t);
        o.value = v;
        if (u.mode === v) o.selected = true;
        sel.append(o);
      }
      sel.addEventListener("change", () => this.setMode(u, sel));
      side.append(sel);
      if (u.state === catalog.UPDATE_DOWNLOADED) {
        const b = button("b on", "Jetzt aktualisieren", name + " jetzt aktualisieren");
        b.addEventListener("click", () => this.installUpdate(u));
        side.append(b);
      } else if (u.state === catalog.UPDATE_NEEDSGRANTS) {
        // NOT an update button: nothing is wrong with this version, the app
        // may simply not yet do what it now needs. So the offer is the
        // activation, where those rights are allowed one by one.
        const b = button("b", "Neue Version mit neuen Rechten aktivieren", null);
        b.addEventListener("click", () => this.regrant(u, r));
        side.append(b);
      }
      r.append(main, side);
      box.append(r);
    }
    if (rows.length === 0) box.append(el("p", "muted", "Alles auf dem neuesten Stand."));
    out.push(box);
    return out;
  }

  async installUpdate(u) {
    // For the system there is nothing to name: the controller runs apt, and
    // a version or a checksum beside it would be a promise nobody can keep
    // (core.KIND_INSTALL refuses both). Every other target is named by the
    // row - the controller installs only a file that fits the task AND the
    // signed release or catalog entry, so a wrong value can make an update
    // fail and never make a wrong one succeed.
    const params = u.target === trust.AUTO_SYSTEM
      ? { target: u.target, version: "", sha256: EMPTY }
      : { target: u.target, version: u.available, sha256: u.sha256 };
    const ok = await this.rootTask(A.install, params, u.node,
      "Die neue Fassung wird eingespielt.");
    if (ok) {
      this.cache.updates = undefined;
      this.render();
    }
  }

  // regrant offers the activation of the new version. Its checksums come out
  // of the store's signed catalog and never out of the update row: the row
  // is an answer of the API, the catalog is a document the store signed.
  async regrant(u, row) {
    let sources, installed;
    try {
      sources = await this.need("sources", A.sources);
      installed = await this.need("installed", A.installed);
    } catch {
      this.say("error", "Das hat nicht geklappt. Bitte es noch einmal versuchen.");
      return;
    }
    const app = installed.find((i) => i.id === u.target);
    const src = sources.find((s) => s.id === app?.store);
    if (src === undefined) {
      this.say("warn", "Diese App kam nicht aus einem Store. Bitte die neue Fassung unter „Hochladen“ einrichten.");
      return;
    }
    let apps;
    try {
      apps = await this.catalogOf(src);
    } catch {
      this.say("error", "Der Katalog dieses Stores ist nicht echt.");
      return;
    }
    const e = apps.find((x) => x.id === u.target);
    const v = e?.versions.find((x) => x.version === u.available);
    if (v === undefined) {
      this.say("warn", "Diese Fassung steht nicht im Katalog. Bitte es später noch einmal versuchen.");
      return;
    }
    for (const old of row.parentNode.querySelectorAll(".installbox")) old.remove();
    const box = await this.installBox({
      app: e.id, name: e.name, version: v.version, sha256: v.sha256, ui: v.ui,
      store: src.id, needs: v.needs,
    });
    if (!this.abort.signal.aborted) row.after(box);
  }

  // setMode changes how a target takes its updates. "automatisch" is the one
  // that needs more than a setting: the installation has to say in its trust
  // list whose signature it will then accept unattended.
  async setMode(u, sel) {
    const mode = sel.value;
    if (mode === catalog.MODE_AUTO && !(await this.standingYes(u.target))) {
      // Nothing was approved, so nothing is switched: the box goes back to
      // what is really in force.
      sel.value = u.mode;
      return;
    }
    try {
      await this.ask(A.setUpdate, { target: u.target, mode, window: u.window });
    } catch {
      sel.value = u.mode;
      this.say("error", "Das ließ sich nicht speichern. Bitte es noch einmal versuchen.");
      return;
    }
    u.mode = mode;
    this.say("info", "Gespeichert.");
  }

  // standingYes writes the next version of the trust list with the standing
  // approval in it, and has the admin sign it.
  async standingYes(target) {
    const cur = this.ctx.trust?.current?.();
    const s = this.ctx.session;
    if (!cur?.list || !s) {
      this.say("error", "Dafür ist eine Anmeldung nötig. Bitte neu anmelden.");
      return false;
    }
    let installed;
    try {
      installed = await this.need("installed", A.installed);
    } catch {
      this.say("error", "Das hat nicht geklappt. Bitte es noch einmal versuchen.");
      return false;
    }
    const key = approvalKey(target, cur.list, this.ctx.releaseKey, installed);
    if (key === undefined) {
      this.say("warn", "Für diese Freigabe fehlt der Schlüssel. Bitte den Store dieser App zuerst eintragen.");
      return false;
    }
    // The hash the version in force was CHECKED with, not one taken again
    // from its bytes: current() carries it because trust.open and
    // trust.advance computed it over the very bytes they read. A second
    // hashing here would be a second reading of the same thing, and the chain
    // would be built on the reading nobody verified.
    const prev = cur.hash instanceof Uint8Array ? cur.hash : await trust.hash(cur.signed);
    let payload;
    try {
      payload = trust.encodeList(nextList(cur.list, prev, target, key));
    } catch {
      this.say("error", "Das hat nicht geklappt. Bitte es noch einmal versuchen.");
      return false;
    }
    const p = await askSignature(PURPOSE_TRUST_LIST, payload, "list",
      { list: cur.list, user: s.user, signal: this.abort.signal });
    if (p === null) {
      this.say("info", "Nichts geändert.");
      return false;
    }
    try {
      // The same check the API and every controller will run on this version:
      // the chain from the one in force to this one, signature and all. If
      // the shell cannot convince itself, nobody else is asked to be
      // convinced either - and a version that was signed and refused
      // afterwards has already left the signature behind.
      await trust.advance({ list: cur.list, hash: prev }, [{ payload, proof: p }]);
    } catch {
      this.say("error", "Diese Freigabe ergibt keine gültige Vertrauensliste. Bitte neu anmelden.");
      return false;
    }
    let seq;
    try {
      seq = (await this.ask(A.trust, null, { stream: oneChunk(signedBytes(payload, p)) })).seq;
    } catch {
      this.say("error", "Die Freigabe wurde nicht angenommen. Bitte es noch einmal versuchen.");
      return false;
    }
    // THE ANSWER IS NOT A RECEIPT TO THROW AWAY, and Fixrunde 1 found what it
    // costs to treat it as one. ngc.users/api/trust answers with the number
    // of the version that now stands (core: TrustResult, S({seq: uint})), and
    // the version in force HERE does not move by itself: the API keeps what
    // it was sent, the browser follows the chain over GET /api/v1/trust.
    //
    // The next standing approval is built on current(). A current() still
    // standing at the old version would chain the next one to the same prev
    // and give it the same number - two versions at one seq, which is a FORK
    // of the trust list. That is the root everything else hangs from.
    //
    // What refresh does with this number, measured against the real
    // TrustStore and not taken on trust (store.spec.js, "refresh does with
    // the number what the store depends on"): a number that is not higher
    // than the version in force changes nothing and asks nobody; a higher one
    // follows the chain and answers with the number then in force.
    let now = 0;
    try {
      now = await this.ctx.trust?.refresh?.(seq);
    } catch {
      // A broken chain, a dead connection, a list already out of force: all
      // of them mean the same here - this shell is not on that version.
      now = 0;
    }
    if (!Number.isSafeInteger(now) || now < seq) {
      // The approval itself IS stored; only this browser did not get there.
      // So nothing more is written, and the sentence says the one thing that
      // fixes it. Writing a second version from here would be the fork.
      this.say("warn", "Die Freigabe ist eingetragen. Bitte die Seite neu laden, "
        + "damit NGC hier auf dem neuen Stand ist.");
      return false;
    }
    return true;
  }

  // ---- signing a root task ------------------------------------------------
  //
  // The task carries the parameters the shell computed, and it is signed
  // before anything leaves. Where the trust list asks for it, the user signs
  // the very same bytes a second time with their own key; the API refuses the
  // task without that signature, and the shell must not send one it knows
  // will be refused.
  //
  // A GAP IN THE CONTRACT, NAMED HERE BECAUSE THIS IS WHERE IT SHOWS.
  // wire.encodeOpen (and wire.parseOpen behind it) takes `confirm` only
  // together with `task`: "if (confirm !== null) { if (task === null) return
  // false }". So an action of service `api` - which carries no task - CANNOT
  // BE CONFIRMED AT ALL, however the trust list's policy reads. The store
  // therefore asks only for root tasks, and ngc.store/api/set-update goes out
  // unconfirmed even for a role at level `hardcore`, whose `changes` rule
  // would ask for one. That is the contract as it stands, not an omission
  // here; it is in the coordinator's register.
  async rootTask(action, params, node, done) {
    const s = this.ctx.session;
    if (!s) {
      this.say("error", "Dafür ist eine Anmeldung nötig. Bitte neu anmelden.");
      return false;
    }
    let built, taskBytes;
    try {
      built = task.buildTask({
        inst: s.inst, certHash: s.certHash, node,
        app: action.app, service: action.service, action: action.name,
        params: core.encodeParams(action, params),
        now: Math.floor(Date.now() / 1000),
      });
      taskBytes = envelopeBytes(await task.signTask(s.signKey, built.bytes));
    } catch {
      this.say("error", "Das ließ sich nicht unterschreiben. Bitte neu anmelden.");
      return false;
    }
    let confirmBytes = null;
    const { list } = this.ctx.trust?.current?.() ?? {};
    // No list, no decision: the policy of what has to be confirmed lives in
    // it, and a shell that cannot read the policy must not decide that
    // nothing needs confirming. Nothing is sent - the safer of the two, and
    // the API would refuse it a moment later anyway.
    if (!list) {
      this.say("error", "Die Vertrauensliste fehlt. Bitte neu anmelden.");
      return false;
    }
    if (needConfirm(list, s.user, {
      app: action.app, action: action.name, node,
      root: action.root === true, critical: core.critical(action, params),
      changes: action.changes === true,
    })) {
      // The bytes of the task, not a sentence about it: what is confirmed is
      // what runs.
      const p = await askConfirm(built.bytes, { list, user: s.user, signal: this.abort.signal });
      if (p === null) {
        this.say("info", "Nichts geändert.");
        return false;
      }
      try {
        confirmBytes = proofBytes(p);
      } catch {
        this.say("error", "Die Bestätigung hat nicht gereicht. Bitte es noch einmal versuchen.");
        return false;
      }
    }
    try {
      // With a task the OPEN carries no params of its own: only the signed
      // ones count.
      await this.ask(action, null, { taskBytes, confirmBytes });
    } catch (e) {
      // ONE REFUSAL SAYS SOMETHING THE PERSON CAN ACT ON, and until
      // 18.10.2026 this catch threw it away: the code was not even bound, so
      // every refusal read "Das hat der Rechner nicht angenommen" - the
      // sentence that cost a whole evening on the real machine, because a
      // task without root rights on that server looks exactly like a server
      // that is having a bad day. What is read is OUR OWN code
      // (wire.CODES.FORBIDDEN, set by the controller), never a text from the
      // other side: the sentence below is this app's, and nothing of a
      // foreign answer reaches the screen.
      this.say("error", e?.code === wire.CODES.FORBIDDEN
        ? "Dafür fehlen Ihnen auf diesem Server die Systemrechte. Sie werden in "
          + "»Benutzer und Rechte« unter »Server« bei »Systemrechte« vergeben."
        : "Das hat der Rechner nicht angenommen. Bitte es noch einmal versuchen.");
      return false;
    }
    this.say("info", done);
    return true;
  }
}

// ---- the key of a standing approval ---------------------------------------

// approvalKey answers with the key a standing approval is written down with,
// or undefined where there is none to write.
//
// A key for a standing approval never comes from the API: for an NGC part the
// release key of version 1 or of the approvals already in the list, for an
// app the key of its store out of the verified trust list. core.UpdateInfo
// carries a key as well, and it is never read - an API that could name the
// key of its own updates could hand itself an update nobody signed for.
function approvalKey(target, list, releaseKey, installed) {
  // The system's packages come from the distribution: there is no store
  // signature to compare against, so the entry carries no key at all
  // (trust.validateList wants exactly none here).
  if (target === trust.AUTO_SYSTEM) return EMPTY;
  if (target === trust.AUTO_WEB || target === trust.AUTO_API || target === trust.AUTO_CONTROLLER) {
    const already = list.auto.find((a) => a.key.length === 32);
    if (already !== undefined) return already.key;
    return releaseKey instanceof Uint8Array && releaseKey.length === 32 ? releaseKey : undefined;
  }
  const app = installed.find((i) => i.id === target);
  const st = list.stores.find((s) => s.id === app?.store);
  return st === undefined ? undefined : st.key;
}

// nextList is the version of the list that carries the new approval: one
// higher, chained to the one in force by its hash, and its auto entries
// sorted by target, which is what trust.validateList asks of them.
function nextList(list, prev, target, key) {
  const auto = [...list.auto.filter((a) => a.target !== target), { target, key }]
    .sort((a, b) => (a.target < b.target ? -1 : a.target > b.target ? 1 : 0));
  return { ...list, seq: list.seq + 1, prev, issued: Math.floor(Date.now() / 1000), auto };
}

// ---- documents the shell writes -------------------------------------------
//
// Each of the three is read back with the contract that reads one from
// outside before it is handed over. What leaves the shell is then what every
// verifier will decode out of exactly these bytes - the same reason
// task.buildTask and wire.encodeOpen read their own work back.

function envelopeBytes(env) {
  const b = utf8(JSON.stringify({ payload: b64encode(env.payload), kid: env.kid, sig: b64encode(env.sig) }));
  parseEnvelope(b, task.MAX_TASK);
  return b;
}

// A proof is at most a passkey's: signature, authenticator data and client
// data, each in base64url, plus the member names.
const proofBudget = new Budget({ size: 64 << 10 });

function proofBytes(p) {
  const o = { kind: p.kind, kid: p.kid, sig: b64encode(p.sig) };
  if (p.authData.length > 0) o.authData = b64encode(p.authData);
  if (p.clientData.length > 0) o.clientData = b64encode(p.clientData);
  const b = utf8(JSON.stringify(o));
  if (!validateProof(decode(b, proofBudget, proofShape))) throw new Error("store: this proof does not hold");
  return b;
}

function signedBytes(payload, p) {
  const proof = { kind: p.kind, kid: p.kid, sig: b64encode(p.sig) };
  if (p.authData.length > 0) proof.authData = b64encode(p.authData);
  if (p.clientData.length > 0) proof.clientData = b64encode(p.clientData);
  const b = utf8(JSON.stringify({ payload: b64encode(payload), proof }));
  trust.parseSigned(b);
  return b;
}

// oneChunk turns bytes into the stream the link sends. A Blob is what a file
// is too, so the upload and this take one and the same road out.
const oneChunk = (bytes) => new Blob([bytes]).stream();

// ---- small pieces of surface ----------------------------------------------

const head = (t) => el("h2", "h", t);

// pair is a name and a value side by side: the value is text from outside
// and goes in as text.
function pair(k, v) {
  const row = el("div", "kv");
  row.append(el("span", "k", k), el("span", "v", v));
  return row;
}

// field puts a label in front of a control. The control already carries its
// own name for a screen reader, so the label is there for the eye.
function field(name, control) {
  const row = el("div", "fieldrow");
  row.append(el("span", "k", name), control);
  return row;
}

const when = (unix) => new Date(unix * 1000).toLocaleString("de-DE",
  { day: "2-digit", month: "2-digit", year: "numeric", hour: "2-digit", minute: "2-digit" });

// money writes a price the way the person's browser writes prices. A
// currency the engine does not know still comes out readable, which is why
// the fallback is the plain number and the three letters.
function money(price) {
  const amount = price.amount / 100;
  let s;
  try {
    s = new Intl.NumberFormat("de-DE", { style: "currency", currency: price.currency }).format(amount);
  } catch {
    s = amount.toFixed(2) + " " + price.currency;
  }
  if (price.model === catalog.PRICE_SUBSCRIPTION) {
    s += price.period === catalog.PERIOD_YEAR ? " im Jahr" : " im Monat";
  }
  return s + (price.per === catalog.PER_USER ? " je Benutzer" : "");
}

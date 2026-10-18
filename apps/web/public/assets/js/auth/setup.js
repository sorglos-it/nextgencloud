// The first run: an installation nobody has set up yet, a one-time code from
// the server's screen, and the first admin. It is the first minute anybody
// ever spends with NGC, and the person in front of it has never seen a trust
// list, a fingerprint or a standing approval.
//
// THE ONE PROMISE OF THIS FILE: the one-time code never leaves the browser.
// It is typed, normalized, used as the key of two HMACs and dropped. It is in
// no request body, no address, no log and no store - and a code that does not
// fit gets a sentence that says NOTHING about how close it was, because the
// only thing a wrong code can mean to the person is "look at the screen
// again", while telling them which part was wrong would tell whoever bent the
// answer how far they got. tests/shell/setup.spec.js measures both rather
// than believing this paragraph.
//
// THE ORDER IS shared/README.md's, "Ersteinrichtung", and session.js's own:
//
//   1. the code first, and nothing is asked of the API before it is there:
//      the offer can only be checked WITH the code, and an offer nobody can
//      check is an offer nobody may read.
//   2. GET /api/v1/setup, held against session.parseSetupOffer - first the
//      MAC over the exact bytes of info, then info itself.
//   3. what the offer holds, on screen, character for character, so the
//      person can compare it with the server: the installation, every API
//      server with its address and its fingerprint, and the store, which they
//      may switch off.
//   4. the first admin: a login name, a password twice, and out of it the
//      password key - a fresh salt and the contract's own defaults.
//   5. version 1 of the trust list, built out of the offer and nothing else.
//   6. the version, written ONCE, signed, MAC'd and sent.
//   7. the anchor kept, and straight on into the workplace.
import { el, button, field, note, clear, text } from "../shell/dom.js";
import { b64encode, utf8 } from "../contracts/b64.js";
import { validLoginName } from "../contracts/text.js";
import * as session from "../contracts/session.js";
import * as sign from "../contracts/sign.js";
import * as proof from "../contracts/proof.js";
import * as trust from "../contracts/trust.js";
import * as wire from "../contracts/wire.js";
import * as http from "../net/http.js";
import { rememberAnchor, NOT_SET_UP } from "../shell/trustlist.js";
import { proveWithPassword } from "./keys.js";
import { login, loginView } from "./login.js";
import { t } from "../lang.js";

// THE SENTENCES. Every one of them says what to DO, because the person
// reading them is setting a server up for the first time and has nothing to
// go on but these words.
//
// The two about the code are deliberately the only two: "not complete yet"
// while there is nothing to ask the API with, and "does not fit this
// installation" for every other way a code can be wrong. A third sentence
// that named the place of the mistake would be a measuring tape for anybody
// guessing at the code.
// They are functions and not strings because the sentence is read when it is
// said, not when this file is loaded: lang.js has the five languages by
// then (shell/main.js loads them before the first mask is drawn), and a const
// evaluated at import time would freeze whatever was there first.
const INCOMPLETE = () => t("setup.incomplete");
const NO_MATCH = () => t("setup.nomatch");
// The API said no to the version itself. There is no second try: the code is
// good once, and an installation that refused version 1 has to be set up
// again from the start.
const REFUSED = () => t("setup.refused");
const ALREADY = () => t("setup.already");
const BROKEN = () => t("setup.broken");
const UNREACHABLE = () => t("setup.unreachable");
// THE SENTENCE THAT WAS MISSING, and the measurement that asked for it is in
// the report: the installation answered, it just could not answer this.
//
// A busy database during the first run does NOT arrive here as E_UNREACHABLE.
// apps/server-api/classes/setup/setup.go:94-99 (fault) turns every
// E_UNREACHABLE on this path into E_INTERNAL, and the comment above it says
// so in as many words - "nothing on this path can say 503 any more". So the
// answer is 500 E_INTERNAL with no Retry-After, and until this key existed
// E_INTERNAL fell through sentenceForOffer's last line into NO_MATCH: the
// person was told to CHECK THE ONE-TIME CODE. Measured, rendered in a
// browser: "Der Einmal-Code passt nicht zu dieser Anlage. Bitte den Code
// prüfen und noch einmal versuchen." for a database that was merely busy.
//
// That is point 50's own mistake moved one screen along - a false statement
// about the one thing the person holds, and a worse one than the login's was:
// a code is good ONCE, so somebody who believes this sentence installs NGC
// again for nothing. The code is untouched by a 500, which is why this
// sentence says so.
const SILENT = () => t("setup.silent");
const NO_NAME = () => t("setup.noname");
const NO_PASSWORD = () => t("setup.nopassword");
const DIFFERENT = () => t("setup.different");
// The automatic sign-in right after the setup did not work. The installation
// IS set up by then, so this is not a failure of the setup and must not read
// like one: the mask follows underneath and the person simply signs in.
const SIGN_IN_YOURSELF = () => t("setup.signinyourself");

// trust.ValidID is 16 bytes in base64url, which is what a user ID is.
const ID_SIZE = 16;

// signedDoc writes one signed version the way Go's json.Marshal writes
// trust.Signed: authData and clientData carry omitempty, and a password proof
// has neither. apps/store.js and shell/trustlist.js write the same three
// lines for their own documents; this one goes inside session.Setup, so it
// hands back the object rather than bytes.
function signedDoc(v) {
  const p = { kind: v.proof.kind, kid: v.proof.kid, sig: b64encode(v.proof.sig) };
  if (v.proof.authData.length > 0) p.authData = b64encode(v.proof.authData);
  if (v.proof.clientData.length > 0) p.clientData = b64encode(v.proof.clientData);
  return { payload: b64encode(v.payload), proof: p };
}

// setupOffer fetches the offer and holds it against the contract with the
// canonical code. It is a step of its own, and not part of runSetup, for the
// reason the whole flow turns on: the offer is read ONCE, shown to the person
// and then signed. A runSetup that fetched it again would sign a second
// answer - and the person would have compared the first one with the screen.
// Checked A, used B, which is the one mistake this strand has paid for most.
//
// Whatever it refuses leaves as the contract's own error; the mask turns all
// of them into the same one sentence.
export async function setupOffer(code) {
  return session.parseSetupOffer(await http.getSetupOffer(), code);
}

// runSetup builds version 1 out of the offer, signs it with the new admin's
// password key, sends it and keeps the anchor.
//
// It takes the offer it is to use (info) and the relying party of
// /config.json (rp) on top of the brief's own four values, for the reason
// setupOffer gives above - and because rp belongs to the page's own server,
// not to an answer of the API.
export async function runSetup({ code, info, rp, name, password, store }) {
  // Both are the mask's job and both are checked here again, because this
  // function is exported: a code that is not canonical holds for no MAC, and
  // a name the trust list refuses would build a version 1 the API throws
  // away - and the code would be spent.
  if (!session.validSetupCode(code)) throw new Error("setup: that is no one-time code");
  if (!validLoginName(name)) throw new Error("setup: that is no login name");

  // The admin's password key: a salt drawn fresh for this one account, and
  // Argon2id with the contract's own defaults. The numbers come from
  // proof.js and are not written here a second time.
  const kdf = {
    alg: proof.KDF_ARGON2ID,
    salt: crypto.getRandomValues(new Uint8Array(proof.SALT_SIZE)),
    m: proof.DEFAULT_MEM_KIB, t: proof.DEFAULT_TIME, p: proof.DEFAULT_PAR,
  };
  const seed = await proof.deriveSeed(password, kdf);
  let pub;
  try {
    pub = await proof.publicFromSeed(seed);
  } finally {
    // The seed lives for this one export and is gone whichever way this ends.
    seed.fill(0);
  }

  // Version 1. Everything in it comes out of the offer or out of this
  // function; nothing is invented, and nothing is read from the API twice.
  const list = {
    seq: 1,
    // Version 1 has nothing before it.
    prev: new Uint8Array(0),
    // The API's own clock, out of the offer the MAC covered.
    issued: info.now,
    installation: info.installation,
    // The party a passkey would be asked for. It is empty as long as this
    // installation has no passkeys, which is what /config.json carries then.
    // A copy, because encodeList writes what it is handed and the caller
    // keeps its own array.
    rp: { id: rp.id, origins: [...rp.origins] },
    users: [{
      id: b64encode(crypto.getRandomValues(new Uint8Array(ID_SIZE))),
      name,
      roles: [trust.ROLE_ADMIN],
      keys: [{
        id: await sign.keyID(pub), kind: proof.KIND_PASSWORD, alg: proof.ALG_EDDSA,
        pub, credId: new Uint8Array(0), kdf,
      }],
    }],
    // One role, and it is the one the whole installation hangs from. critical
    // means a change of weight is confirmed with a signature; off would let
    // the first admin do everything unasked.
    roles: [{
      name: trust.ROLE_ADMIN, level: trust.LEVEL_CRITICAL,
      black: [], white: [], passkeyOnly: false, passwordToo: false,
    }],
    // EXACTLY the offered nodes, in the offered order: the API compares its
    // own offer with what the admin signed, as JSON (Go: VerifySetup,
    // sameNodes). encodeSetupInfoNodes is the one place that knows that rule.
    nodes: session.encodeSetupInfoNodes(info),
    // The store only where the offer had one AND the person left the box
    // ticked. Nothing else may stand here: the API takes the offered store or
    // none.
    stores: store === true && info.store !== null ? [info.store] : [],
    // The standing approvals of version 1, sorted by target as a trust list
    // demands: the three parts of NGC itself with the release key the offer
    // named, and `system` with no key at all. Apps come later, with the key
    // of their store.
    auto: [
      { target: trust.AUTO_API, key: info.releaseKey },
      { target: trust.AUTO_CONTROLLER, key: info.releaseKey },
      { target: trust.AUTO_WEB, key: info.releaseKey },
      { target: trust.AUTO_SYSTEM, key: new Uint8Array(0) },
    ],
  };

  // WRITTEN ONCE, and these exact bytes are everything afterwards: what the
  // admin signs, what the MAC covers, what the anchor hashes and what goes on
  // the wire. A second encodeList would be a second document, and the API
  // would hold the signature of the one against the hash of the other.
  const payload = trust.encodeList(list);
  // proveWithPassword derives the seed a SECOND time, and that is on purpose:
  // it is the one tested way the shell makes a password proof and it wipes
  // what it derived. What it costs is one more Argon2id - proof.deriveSeed
  // puts that at about a second in Chromium and two in WebKit at these
  // parameters - once in the life of an installation. A "sign with this seed"
  // of its own would save that second and put a second place in the tree
  // where a password key is handled.
  const p = await proveWithPassword(payload, password, kdf, sign.PURPOSE_TRUST_LIST);
  const signed = { payload, proof: p };
  const anchor = { seq: 1, hash: await trust.hash(signed) };

  // THE SAME CHECK THE API RUNS (Go: VerifySetup calls trust.Open with this
  // very anchor), and it runs BEFORE the code is spent. The brief has it
  // afterwards, "zur Kontrolle"; here it is before, because the setup happens
  // ONCE: a version 1 that cannot be opened would come back as E_VERIFY, and
  // the only way on from there is to install NGC again. Finding that out
  // before anything is sent costs one SHA-256 and one signature check.
  const version = await trust.open(anchor, [signed]);

  await http.postSetup(utf8(JSON.stringify({
    mac: b64encode(await session.setupMAC(code, payload)),
    trust: signedDoc(signed),
  })));
  // Only now, and not a line earlier: an anchor kept for a setup that did not
  // go through would pin this browser to a list that does not exist, and the
  // shell would then refuse to show the login mask it needs.
  await rememberAnchor(anchor);
  return { version, anchor };
}

// ------------------------------------------------------------------ the mask

// line is one read-only value with its caption, as a definition list: the
// caption is tied to the value for a screen reader without anybody handing
// out an id. The value carries .setupvalue, which switches text selection
// back on - the shell turns it off for the whole page, and a fingerprint
// nobody can select is a fingerprint nobody can compare.
function line(caption, value) {
  const box = el("div", "setupline");
  box.append(el("dt", "setupcaption", caption), el("dd", "setupvalue", value));
  return box;
}

// facts puts a group of those lines into the list they belong in.
function facts(lines) {
  const dl = el("dl", "setupfacts");
  dl.append(...lines);
  return dl;
}

// setupView builds the mask into root and calls onDone with the handover of a
// finished sign-in, exactly the shape loginView's onSession gets - so the
// shell behind it cannot tell which of the two ways somebody came in.
//
// AND WITH THE ANCHOR, which loginView has no need to pass because its caller
// already holds one. This way through has none: the shell only ever draws this
// mask because anchorFor answered null, and the anchor that exists afterwards
// is the one runSetup just made. Whoever opens the trust list next needs THAT
// one - handing it over beats reading it back out of the browser's storage,
// which would be the same value fetched a second time.
export function setupView(root, { config, onDone } = {}) {
  // The relying party of this page's own server. config went through its
  // contract in net/config.js, so id and origins are already a DNS name and
  // origins or empty; an installation without passkeys has neither.
  const rp = { id: config?.rp?.id ?? "", origins: [...(config?.rp?.origins ?? [])] };
  askCode();

  // box builds the one card the whole flow lives in, with its heading, its
  // live region and nothing else on the page: the person has one thing to do
  // here, and everything that is not that is in the way.
  //
  // IT DOES NOT TOUCH root, and show() below is what puts a finished step on
  // the page. Clearing here would empty the page at the START of building the
  // next step, so anything that went wrong while building it would leave a
  // page with nothing on it - and the live region the message was going to go
  // into would already be gone with the step before. This way the step that
  // stands stays standing until its successor is whole.
  function box(lead) {
    const form = el("form", "loginbox setupbox");
    // A form, so Enter works in every field - and one that never submits
    // itself, because a shell that navigates away from itself is gone.
    form.setAttribute("novalidate", "novalidate");
    const head = el("h1", "loginhead", t("setup.head"));
    head.id = "setuptitle";
    form.setAttribute("aria-labelledby", head.id);
    form.append(head, el("p", "setuplead", lead));
    // One live region, built with the step and emptied again: a region that
    // appears only when there is news is announced by nothing.
    const said = note("", "error");
    return { form, said };
  }

  // show swaps the finished step in and puts the cursor where the person has
  // to type.
  function show(form, focus) {
    clear(root);
    root.append(form);
    focus.focus();
  }

  // ------------------------------------------------------------- step one

  function askCode() {
    const { form, said } = box(t("setup.askcode"));
    const code = field(t("setup.code"), "text");
    // Nothing of this field is remembered, suggested or corrected: it holds a
    // secret that is good once.
    code.input.autocomplete = "off";
    code.input.spellcheck = false;
    code.input.setAttribute("autocapitalize", "characters");
    // A bound, and a forgiving one: twenty characters plus whatever somebody
    // put between the groups. Twenty-three would fit the code exactly and cut
    // off anybody who types two spaces between two groups - which
    // normalizeSetupCode takes without blinking.
    code.input.maxLength = 40;
    const row = el("div", "loginrow");
    const go = button("b on", t("setup.next"), null);
    go.type = "submit";
    row.append(go);
    form.append(code.box, row, said);
    show(form, code.input);

    // Taken and set in the SAME step, before any await: a second click while
    // the first attempt is running would ask for a second offer.
    let busy = false;
    form.addEventListener("submit", (e) => {
      e.preventDefault();
      void next();
    });

    async function next() {
      if (busy) return;
      busy = true;
      text(said, "");
      go.disabled = true;
      try {
        // The typed text is read ONCE and turned into the canonical code
        // once. The field keeps it, because somebody who mistyped a code has
        // to be able to correct it rather than type all twenty characters
        // again - and from here on nothing else on the page is asked what
        // the code is.
        const canonical = session.normalizeSetupCode(code.input.value);
        if (canonical === null) {
          // Nothing is asked of the API: without the code its MAC cannot be
          // checked, so there would be nothing to do with the answer.
          text(said, INCOMPLETE());
          return;
        }
        // Inside the try, and it may be: showOffer puts its step on the page
        // in its very last line (show), so anything that went wrong while it
        // was being built leaves THIS step standing - with the live region
        // below still on the page to write the message into.
        showOffer(canonical, await setupOffer(canonical));
      } catch (e) {
        text(said, sentenceForOffer(e));
      } finally {
        busy = false;
        // Not where the next step is already on the page: switching a button
        // nobody can see back on is a line that only looks like it does
        // something.
        if (go.isConnected) go.disabled = false;
      }
    }
  }

  // sentenceForOffer turns everything that can go wrong on the way to an
  // offer into one of four sentences.
  //
  // Only three of them are told apart, and each for a reason the person can
  // act on: an installation that has an admin already, a server nobody can
  // reach, and too many tries. EVERYTHING ELSE - a MAC that does not match,
  // an answer the contract refused, a field out of range - is the same
  // sentence, because telling those apart would say how close a guessed code
  // got.
  //
  // E_NOTFOUND IS NOT "already set up" HERE, and that is the one place this
  // file and the brief read differently. GET /api/v1/setup answers 404 as soon
  // as there is an admin - and this mask is only ever on screen because the
  // shell found NO anchor, neither in /config.json nor from a setup in this
  // browser. Both together mean one thing and it is the sentence that was
  // standing in this spot before: the installation runs, but the controller of
  // the web server has not been paired, so this page cannot name the trust
  // list yet. "Already set up" would be true and useless; this says what to
  // do. (The POST keeps the brief's own sentence - see below.)
  function sentenceForOffer(e) {
    if (e?.code === wire.CODES.NOTFOUND) return NOT_SET_UP();
    if (e?.code === wire.CODES.UNREACHABLE) return UNREACHABLE();
    if (e?.code === wire.CODES.LOCKED) return t("wire.E_LOCKED");
    // An installation that answered and could not answer THIS is not a code
    // that does not fit. See SILENT above for the measurement and for the
    // line in setup.go that makes a busy database arrive as this code.
    if (e?.code === wire.CODES.INTERNAL) return SILENT();
    return NO_MATCH();
  }

  // ------------------------------------------------------------- step two

  function showOffer(code, info) {
    const { form, said } = box(t("setup.compare"));

    // A heading per section and a definition list under it. The headings stay
    // OUTSIDE the lists: a <dl> may hold dt, dd and div and nothing else, and
    // a heading inside one is markup no screen reader has to make sense of.
    form.append(el("h2", "setuphead", t("setup.installation")));
    form.append(facts([line(t("setup.id"), info.installation)]));
    form.append(el("h2", "setuphead", t("setup.apiservers")));
    // One list per server, so two servers never read as one. Every value goes
    // in character for character - that is what the person compares with the
    // screen, and a value this file prettied up would be a value that does
    // not match.
    for (const n of info.nodes) {
      form.append(facts([
        line(t("setup.name"), n.name), line(t("setup.address"), n.url),
        line(t("setup.fingerprint"), b64encode(n.tls)),
      ]));
    }

    // The store, with a box to switch it off. It is ticked, because an
    // installation without it has no way to install an app at all - and it is
    // a box and not a question, because the answer is yes for nearly
    // everybody.
    let storeBox = null;
    if (info.store !== null) {
      form.append(el("h2", "setuphead", t("setup.store")));
      const label = el("label", "setupcheck");
      storeBox = el("input", null);
      storeBox.type = "checkbox";
      storeBox.checked = true;
      label.append(storeBox, el("span", null, t("setup.storeyes")));
      form.append(label, el("p", "setupvalue", info.store.url));
    }

    form.append(el("h2", "setuphead", t("setup.account")));
    form.append(el("p", "setuplead", t("setup.accountlead")));
    const name = field(t("setup.username"), "text");
    name.input.autocomplete = "username";
    const pw = field(t("setup.password"), "password");
    pw.input.autocomplete = "new-password";
    const again = field(t("setup.passwordagain"), "password");
    again.input.autocomplete = "new-password";
    const row = el("div", "loginrow");
    const go = button("b on", t("setup.go"), null);
    go.type = "submit";
    row.append(go);
    form.append(name.box, pw.box, again.box, row, said);
    show(form, name.input);

    let busy = false;
    form.addEventListener("submit", (e) => {
      e.preventDefault();
      void finish();
    });

    async function finish() {
      if (busy) return;
      busy = true;
      // Every field is read ONCE, and all three before anything else happens:
      // what is checked below has to be what is used.
      const typed = pw.input.value;
      const repeated = again.input.value;
      const who = name.input.value;
      text(said, "");
      go.disabled = true;
      try {
        // The three things the person can get wrong, each with the sentence
        // that says what to do about it. They are checked BEFORE the fields
        // are emptied - all of it synchronous, so nothing is on the page any
        // longer than before - because a typo in the user name must not cost
        // somebody the password they have just thought up twice.
        if (!validLoginName(who)) { text(said, NO_NAME()); return; }
        if (typed === "") { text(said, NO_PASSWORD()); return; }
        if (typed !== repeated) { text(said, DIFFERENT()); return; }
        // Emptied now, with no await between the last read and this line:
        // from here the two typed passwords are in two variables of this
        // closure and nowhere on the page. The same promise login.js keeps.
        pw.input.value = "";
        again.input.value = "";
        const { anchor } = await runSetup({
          code, info, rp, name: who, password: typed,
          store: storeBox === null ? false : storeBox.checked === true,
        });
        // From here the installation IS set up. Whatever happens now, there
        // is no way back into this mask and no second code - so nothing
        // below may end in a page that does nothing.
        await signIn(who, typed, anchor);
      } catch (e) {
        text(said, sentenceForSetup(e));
      } finally {
        busy = false;
        if (go.isConnected) go.disabled = false;
      }
    }
  }

  // sentenceForSetup names the two answers the brief names and nothing else.
  // E_VERIFY is the end of the road and says so; E_NOTFOUND means somebody
  // else got there first.
  function sentenceForSetup(e) {
    if (e?.code === wire.CODES.VERIFY) return REFUSED();
    if (e?.code === wire.CODES.NOTFOUND) return ALREADY();
    if (e?.code === wire.CODES.UNREACHABLE) return UNREACHABLE();
    if (e?.code === wire.CODES.LOCKED) return t("wire.E_LOCKED");
    // E_INTERNAL keeps BROKEN here and does NOT get SILENT, and the
    // difference is the step and not the code: at this point the version has
    // been signed and sent, and whether the installation took it is exactly
    // what nobody knows any more. "Reload the page and try again" is the only
    // true instruction left - the shell finds out on the reload whether there
    // is an admin now. SILENT's promise, that the code is untouched, cannot
    // be made here.
    return BROKEN();
  }

  // signIn walks the person straight into the workplace with the name and the
  // password they have just chosen. Asking them to type the same password a
  // third time, half a second after they set it, is a step that explains
  // nothing and helps nobody.
  //
  // Where it does NOT work - a login that fails for any reason at all - the
  // mask of task 18 goes up with one sentence above it. The installation is
  // finished by then, the anchor is kept, and a reload lands on that mask
  // anyway; what must not happen is a page with nothing on it.
  async function signIn(who, password, anchor) {
    let world;
    try {
      world = await login({ name: who, password });
    } catch {
      // The mask of task 18 takes over, and the anchor travels with it: the
      // person signs in by hand, and what follows needs the same anchor this
      // setup just made.
      loginView(root, { config, onSession: (w) => onDone?.(w, anchor) });
      root.prepend(note(SIGN_IN_YOURSELF(), "info"));
      return;
    }
    clear(root);
    onDone?.(world, anchor);
  }
}

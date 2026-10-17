// The login: the mask the person sees, and the six steps behind the button.
//
// THE ONE PROMISE OF THIS FILE: the password never leaves the browser. It is
// read out of the field, handed to keys.proveWithPassword and to nothing
// else, and the field is empty from the click on. What goes on the wire is a
// signature over the client certificate - never the password, never a hash of
// it, never the seed. tests/shell/login.spec.js measures that by collecting
// every request body, every URL and every WebSocket frame of a whole login
// and searching them, rather than believing this paragraph.
//
// The order is shared/README.md's, "Vor dem WebSocket":
//
//   1. loginStart(name) - the API answers for a name nobody has too, with a
//      challenge that cannot be told from a real one. Whatever
//      parseLoginChallenge refuses is treated like every other failure.
//   2. the session keys and the client certificate, built ONCE: those exact
//      bytes are what is signed and what CertHash hashes.
//   3. the proof, by the password key or by a passkey; a role with
//      passwordToo gets both, over the SAME bytes.
//   4. loginFinish - the API sets the cookie.
//   5. the API's certificate, checked against the trust list.
//   6. the link, and the desktop.
import { el, button, field, note, clear, text } from "../shell/dom.js";
import { b64encode, utf8 } from "../contracts/b64.js";
import * as session from "../contracts/session.js";
import * as sign from "../contracts/sign.js";
import * as trust from "../contracts/trust.js";
import * as wire from "../contracts/wire.js";
import { validTOTP } from "../contracts/text.js";
import * as http from "../net/http.js";
import { Link } from "../net/link.js";
import { newSessionKeys, buildClientCert, proveWithPassword, proveWithPasskey } from "./keys.js";
import { t } from "../lang.js";

// The one sentence every failed login gets. It is wire's own text for
// E_LOGIN and not a second copy: the table is the contract's, and a shell
// with words of its own is a shell that says something different from the
// API about the same thing.
const FAILED = t("wire.E_LOGIN");

// NeedTOTP is no failure. The challenge said this user has a second factor
// and no code was typed yet, so the mask asks for one and nothing was spent:
// the password has not been derived, and no proof has been made.
export class NeedTOTP extends Error {}

// socketURL is the address of the session socket. net/http.js checks the
// grammar of it and always says wss:, which is right in operation and
// impossible on a page that is itself http: - and a page that is http: only
// ever gets this far on loopback, because main.js refuses anything the
// browser does not call a secure context. So the address comes from there
// and only the scheme follows the page, which is the pair net/testlink.js
// has carried since task 17.
function socketURL() {
  const u = new URL(http.socketURL());
  if (location.protocol !== "https:") u.protocol = "ws:";
  return u.toString();
}

// wireProof writes a proof the way session.LoginFinish carries it. Every byte
// string is base64url, including the two a password proof leaves empty.
function wireProof(p) {
  return {
    kind: p.kind, kid: p.kid, sig: b64encode(p.sig),
    authData: b64encode(p.authData), clientData: b64encode(p.clientData),
  };
}

// openTrustFromChain is A STAND-IN and says so. Task 20 opens the list
// against the anchor of /config.json and follows it version by version; this
// fetches version 1 and holds it against the contract - every field, every
// key id - but follows it from no anchor. What it is used for is the one
// thing this task needs it for: the signing key of the API node, so
// session.verifyAPI can do its real work on the API certificate. When
// shell/trustlist.js lands, main.js hands its opened list in here instead and
// this function goes.
async function openTrustFromChain() {
  const chain = await http.trustChain(1);
  if (chain === null || chain.versions.length === 0) throw new Error("login: no trust list");
  const signed = chain.versions[0];
  return { list: await trust.parseList(signed.payload), signed };
}

// login runs the six steps and hands back what the desktop needs. It throws
// http.ApiError for everything the API said no to, NeedTOTP where a second
// factor is missing, and an ordinary Error for everything else; the mask
// turns all but two of them into the same one sentence.
//
// openTrust is the seam for task 20 and defaults to the stand-in above.
export async function login({ name, password, totp, wantPasskey, openTrust = openTrustFromChain }) {
  const challenge = await http.loginStart(name);
  // Asked BEFORE anything is spent: Argon2id takes about a second and a
  // passkey costs the person a gesture, and neither may be spent on an
  // attempt that was going to stop here anyway.
  if (challenge.totp && !validTOTP(totp)) throw new NeedTOTP();

  const keys = await newSessionKeys();
  const { payload } = buildClientCert({
    installation: challenge.installation, user: challenge.user, nonce: challenge.nonce,
    keys, now: challenge.now, maxLifetime: challenge.maxLifetime,
  });

  // A passkey where the challenge offers one AND one of three things holds:
  // the person pressed the passkey button; the role asks for the password as
  // well, which by the contract means the proof HAS to be a passkey
  // (session.VerifyPassword), so pressing "Anmelden" has to reach the
  // authenticator too or that person could never get in and would never be
  // told why; or nothing was typed in the password field, which for somebody
  // whose role forbids a password key is the only thing they can do.
  //
  // Where the challenge offers no passkey the attempt goes on and fails like
  // every other: an answer that said "this name has no passkey" would say
  // that the name exists.
  const usePasskey = challenge.passkeys.length > 0
    && (wantPasskey === true || challenge.passwordToo || password === "");
  const proofs = { proof: null, password: null };
  if (usePasskey) {
    proofs.proof = await proveWithPasskey(payload, challenge, sign.PURPOSE_CLIENT_CERT);
    // A role that asks for the password as well: the second proof is by the
    // password key and covers the SAME certificate bytes. Two documents would
    // be two approvals, and the API checks one against the other.
    if (challenge.passwordToo) {
      proofs.password = await proveWithPassword(payload, password, challenge.kdf,
        sign.PURPOSE_CLIENT_CERT);
    }
  } else {
    proofs.proof = await proveWithPassword(payload, password, challenge.kdf,
      sign.PURPOSE_CLIENT_CERT);
  }

  // Go's LoginFinish has omitempty on password and totp, and strict JSON has
  // no null to say "not there" with, so a member that is not needed is left
  // out rather than written as null.
  const finish = { cert: b64encode(payload), proof: wireProof(proofs.proof) };
  if (proofs.password !== null) finish.password = wireProof(proofs.password);
  if (challenge.totp) finish.totp = totp;
  const result = await http.loginFinish(utf8(JSON.stringify(finish)));

  const opened = await openTrust();
  // The real check of the API's certificate: signed by an API node of the
  // list, bound to this very certificate, and valid at the API's own clock.
  const api = await session.verifyAPI(result.api, opened.list, payload, challenge.now);
  const link = await new Link().connect(socketURL(), keys.dh, api.dh);
  return {
    link,
    // What the desktop signs its tasks with. The kid is the SESSION key's
    // own and not the user's: a verifier takes the signing key out of the
    // client certificate, so a kid from anywhere else would name a key
    // nobody signed with.
    session: {
      inst: challenge.installation,
      user: challenge.user,
      certHash: await session.certHash(payload),
      signKey: { privateKey: keys.sign.privateKey, kid: keys.sign.kid },
    },
    // THE SESSION'S OWN DH PAIR, and it is handed on for ONE thing: the
    // end-to-end channel from an app to its own service on a controller
    // (net/world.js, appChannel). It is the same pair Link.connect just opened
    // the Noise session with, and its public half is the `dh` of the client
    // certificate above - so the controller that answers the handshake is
    // answering the key the API bound this session to, and no other.
    //
    // It is a member of its own and NOT part of `session`, which travels on to
    // the whole desktop: shell/main.js takes it straight to net/world.js and
    // nothing else ever sees it.
    //
    // WHAT `extractable: false` BUYS, exactly, because it is easy to ask too
    // much of it: the BYTES of the private half cannot be read out - not by the
    // shell, not by anything else on this page. It does NOT stop the key being
    // put somewhere: a non-extractable CryptoKey is structurally cloneable and
    // can be written to IndexedDB, where it would outlive this session. So what
    // is being kept narrow here is the right to USE it, and the only thing that
    // keeps it narrow is that this is the one line that hands it on.
    sessionDH: keys.dh,
    trust: { current: () => ({ list: opened.list, signed: opened.signed }) },
    // The release key of the initial setup is NOT in a login answer and is
    // not invented here: task 19 pins it while the installation is set up.
    // Until then the store says so itself rather than trusting an answer
    // (apps/store.js, approvalKey).
    releaseKey: null,
    passwordHint: result.passwordHint === true,
  };
}

// loginView builds the mask into root and calls onSession once somebody is
// in. Every text is German and goes in as textContent; nothing of an answer
// ever becomes markup.
export function loginView(root, { config, onSession, openTrust } = {}) {
  clear(root);

  const box = el("form", "loginbox");
  // A form, so Enter works in every field the way everyone expects - and a
  // form that never submits itself, because a shell that navigates away from
  // itself is gone.
  box.setAttribute("novalidate", "novalidate");
  const head = el("h1", "loginhead", t("login.head"));
  head.id = "loginhead";
  box.setAttribute("aria-labelledby", head.id);
  box.append(head);

  const name = field(t("login.username"), "text");
  name.input.autocomplete = "username";
  const pw = field(t("login.password"), "password");
  pw.input.autocomplete = "current-password";
  box.append(name.box, pw.box);

  // The second factor is not on the page until the API asks for it: a field
  // nobody has to fill in is a field somebody tries to fill in.
  const totp = field(t("login.totp"), "text");
  totp.input.autocomplete = "one-time-code";
  totp.input.inputMode = "numeric";
  totp.input.maxLength = session.TOTP_DIGITS;

  const row = el("div", "loginrow");
  const go = button("b on", t("login.go"), null);
  go.type = "submit";
  row.append(go);
  // The passkey button only where this installation has a relying party at
  // all; without one navigator.credentials.get has nothing to ask for. This
  // reads the CONFIGURATION, because it has to be decided before anybody has
  // typed a name - the rp the authenticator is then actually asked for comes
  // out of the challenge (keys.proveWithPasskey), which is the one the API
  // and the trust list agree on.
  const hasParty = typeof config?.rp?.id === "string" && config.rp.id !== "";
  const withPasskey = hasParty ? button("b", t("login.withpasskey"), null) : null;
  if (withPasskey !== null) row.append(withPasskey);
  box.append(row);

  // One place for the one sentence, built once and emptied again: a live
  // region that is created only when there is something to say is announced
  // by nothing.
  const said = note("", "error");
  box.append(said);
  root.append(box);
  name.input.focus();

  // busy is taken and set in the SAME step, before any await: a second click
  // while the first attempt is running would start a second login on the
  // nonce the first one is already using, and a nonce is good once.
  let busy = false;
  let lockTimer = 0;
  // Set once the mask has handed over and been taken off the page.
  let gone = false;
  // The password between two clicks: it is read out of the field and the
  // field is emptied in the same step, so the mask never holds it while the
  // API is asked. It is kept only while the attempt needs a second round -
  // the code out of the authenticator app - and dropped as soon as it does
  // not. A string cannot be overwritten (proof.js says the same of the text
  // it is handed), so what can be done is to hold it for as short a time as
  // possible and to name that here.
  let held = "";

  function say(txt) {
    text(said, txt);
  }

  function enable(on) {
    go.disabled = !on;
    if (withPasskey !== null) withPasskey.disabled = !on;
  }

  // lock counts the seconds of an E_LOCKED down where the person can see
  // them. Somebody who is told "later" and nothing else tries again at once.
  function lock(seconds) {
    clearInterval(lockTimer);
    let left = Number.isSafeInteger(seconds) && seconds > 0 ? seconds : 0;
    if (left === 0) {
      say(t("wire.E_LOCKED"));
      return;
    }
    // Belt and braces: attempt() has already switched the buttons off before
    // it asked, so on the one path that reaches this today the line changes
    // nothing - measured, the case stays green without it. It stays because
    // lock() is what OWES the person a disabled button, and a function whose
    // promise depends on its caller having kept it is a promise that breaks
    // the day somebody calls it from anywhere else.
    enable(false);
    const tick = () => {
      if (left <= 0) {
        clearInterval(lockTimer);
        lockTimer = 0;
        enable(true);
        say("");
        return;
      }
      say(t("login.locked.wait", { seconds: left }));
      left -= 1;
    };
    tick();
    lockTimer = setInterval(tick, 1000);
  }

  async function attempt(wantPasskey) {
    if (busy || go.disabled) return;
    busy = true;
    // Read and empty in one step, with no await in between: from here on the
    // typed password is in one variable of this closure and nowhere on the
    // page.
    const typed = pw.input.value;
    pw.input.value = "";
    if (typed !== "") held = typed;
    say("");
    enable(false);
    try {
      const handover = await login({
        name: name.input.value, password: held,
        totp: totp.input.value, wantPasskey, openTrust,
      });
      held = "";
      clearInterval(lockTimer);
      lockTimer = 0;
      gone = true;
      clear(root);
      onSession?.(handover);
      return;
    } catch (e) {
      if (e instanceof NeedTOTP) {
        // The one answer that is not a failure. The password stays in hand
        // for the second round, so nobody types it twice.
        if (!totp.box.isConnected) box.insertBefore(totp.box, row);
        say(t("login.needtotp"));
        totp.input.focus();
        return;
      }
      held = "";
      if (e?.code === wire.CODES.LOCKED) {
        lock(e.retryAfter);
        return;
      }
      // Everything else gets the same sentence, whatever it was: a wrong
      // password, a name nobody has, a challenge the contract refused, a
      // cancelled authenticator. An answer that told them apart would tell
      // whether the name exists.
      //
      // The one exception is a server nobody can reach, which says nothing
      // about any account and everything about what to do next.
      say(e?.code === wire.CODES.UNREACHABLE
        ? t("login.unreachable")
        : FAILED);
    } finally {
      busy = false;
      // Not after a login that worked: the mask is off the page by then, and
      // switching a button that nobody can see back on is a line that only
      // looks like it does something.
      if (!gone && lockTimer === 0) enable(true);
    }
  }

  box.addEventListener("submit", (e) => {
    e.preventDefault();
    void attempt(false);
  });
  withPasskey?.addEventListener("click", () => void attempt(true));
}

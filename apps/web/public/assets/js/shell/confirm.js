// Asking again before something happens: when the trust list wants a task
// confirmed, and how the person confirms it.
//
// WHAT IS SIGNED HERE IS THE TASK ITSELF, byte for byte - the very document
// the session key already signed and the controller will run. Not a summary
// of it, not an id of it, not "yes". A confirmation over anything else would
// say yes to a sentence the shell wrote and leave the sentence the controller
// reads unconfirmed, and swapping the two is the whole attack this exists to
// stop (Go: task.VerifyConfirm, purpose sign.PURPOSE_CONFIRM).
//
// The second key is a second person's worth of evidence, so it is the USER's
// key and never the session's: a passkey where the user has one, otherwise
// the password typed again. The password is used to derive and is gone in the
// same breath - proof.deriveSeed hands the bytes to a worker that wipes them,
// and the seed this file holds for the length of one signature is wiped here.
import { el, button, clear } from "./dom.js";
import * as proof from "../contracts/proof.js";
import { message, PURPOSE_CONFIRM } from "../contracts/sign.js";
import { needsConfirm, user as userOf } from "../contracts/trust.js";
import { t } from "../lang.js";

// The two sentences the question can carry. Which of them is shown says what
// the signature is FOR, and that is the only thing the person can judge: a
// dialog that says "please confirm" and nothing else is a dialog anybody
// clicks away.
const WHY = {
  __proto__: null,
  task: "confirm.why.task",
  list: "confirm.why.list",
};

const EMPTY = new Uint8Array(0);

// needConfirm asks the trust list whether op has to be confirmed by the user
// (Go: trust.NeedsConfirm). op is {app, action, node, root, critical,
// changes}, as core.critical and the action's own row decide it.
//
// It is this one line and no rule of its own, which is the point: the policy
// lives in the list an admin signed, and the shell only reads it. A shell
// that decided for itself would decide differently from every verifier.
export function needConfirm(list, user, op) {
  return needsConfirm(list, user, op);
}

// askConfirm shows the question and answers with the user's proof over
// taskBytes, or null when the person said no. It never throws for a person
// who simply cancelled: a cancel is an answer.
//
// who is {list, user, signal}: the verified trust list, whose user this is,
// and a signal that closes the question unanswered - the store aborts it when
// the window is closed, so a dialog can never outlive the app that asked.
export function askConfirm(taskBytes, who = {}) {
  return askSignature(PURPOSE_CONFIRM, taskBytes, "task", who);
}

// askSignature is askConfirm for a document that is no task: today the one
// other thing a user signs from the shell, the next version of the trust
// list (sign.PURPOSE_TRUST_LIST). It is the same question and the same two
// kinds of evidence; only the purpose and the sentence differ, and the
// purpose has to differ, because a signature made for one purpose verifies
// for no other (sign.message).
export async function askSignature(purpose, payload, why, who = {}) {
  const { list, user, signal } = who;
  const u = userOf(list, user);
  if (!u || !Array.isArray(u.keys)) return null;
  const passkeys = u.keys.filter((k) => k.kind === proof.KIND_PASSKEY);
  const password = u.keys.find((k) => k.kind === proof.KIND_PASSWORD) ?? null;
  // A passkey is the first choice wherever there is one: nothing to type, and
  // nothing that can be typed into the wrong window.
  const usePasskey = passkeys.length > 0 && Boolean(list?.rp?.id);
  if (!usePasskey && password === null) return null;

  const answer = await ask(usePasskey, WHY[why] ?? WHY.task, signal);
  if (answer === null) return null;
  const p = usePasskey
    ? await passkeyProof(purpose, payload, passkeys, list.rp.id)
    : await passwordProof(purpose, payload, password, answer);
  // Read back what was built, with the contract that reads one from outside:
  // a proof the shell cannot vouch for must not leave the shell.
  if (p === null || !proof.validateProof(p)) return null;
  return p;
}

// ---- the question ---------------------------------------------------------

// ask puts one modal question on the screen and answers with the password
// that was typed, "" for a passkey, or null for a cancel. It is the only
// place in this file that touches the document.
function ask(usePasskey, why, signal) {
  return new Promise((done) => {
    const before = document.activeElement;
    const back = el("div", "confirmback");
    const box = el("div", "confirmbox");
    box.setAttribute("role", "dialog");
    box.setAttribute("aria-modal", "true");
    const head = el("h2", "confirmhead", t("confirm.head"));
    // A dialog is named by its heading, and the heading has to be findable
    // for that: the id is unique because only one question stands at a time.
    head.id = "confirmhead";
    box.setAttribute("aria-labelledby", head.id);
    const line = el("p", null, t(why) + " "
      + (usePasskey ? t("confirm.withpasskey") : t("confirm.withpassword")));
    box.append(head, line);

    let field = null;
    if (!usePasskey) {
      const lab = el("label", "confirmlabel", t("confirm.password"));
      field = el("input", null);
      field.type = "password";
      field.autocomplete = "current-password";
      field.id = "confirmpw";
      lab.htmlFor = field.id;
      box.append(lab, field);
    }

    const row = el("div", "confirmrow");
    const no = button("b", t("confirm.cancel"), null);
    const yes = button("b on", usePasskey ? t("confirm.yes.passkey") : t("confirm.yes"), null);
    row.append(no, yes);
    box.append(row);
    back.append(box);

    let over = false;
    const end = (value) => {
      if (over) return;
      over = true;
      document.removeEventListener("keydown", onKey, true);
      signal?.removeEventListener?.("abort", onAbort);
      clear(back);
      back.remove();
      // The focus goes back where it came from. Without it a dialog that
      // closes over the focused element leaves it on <body>, and the next Tab
      // starts again at the top of the page.
      if (before instanceof HTMLElement && before.isConnected) before.focus();
      done(value);
    };
    const onAbort = () => end(null);
    const onKey = (e) => {
      if (e.key === "Escape") {
        e.preventDefault();
        end(null);
        return;
      }
      if (e.key !== "Tab") return;
      // Tab comes round inside the question. It is modal, so there is nothing
      // behind it a keyboard may reach.
      const stops = [...box.querySelectorAll("input, button")];
      const i = stops.indexOf(document.activeElement);
      const next = e.shiftKey ? i - 1 : i + 1;
      if (i !== -1 && next >= 0 && next < stops.length) return;
      e.preventDefault();
      stops[e.shiftKey ? stops.length - 1 : 0].focus();
    };

    no.addEventListener("click", () => end(null));
    yes.addEventListener("click", () => end(field ? field.value : ""));
    if (field) {
      field.addEventListener("keydown", (e) => {
        if (e.key === "Enter") {
          e.preventDefault();
          end(field.value);
        }
      });
    }
    // A click beside the box does NOT answer it. A question that goes away
    // when somebody misses the button is a question that was never asked.
    document.addEventListener("keydown", onKey, true);
    signal?.addEventListener?.("abort", onAbort);
    document.body.append(back);
    (field ?? yes).focus();
    if (signal?.aborted) end(null);
  });
}

// ---- the two kinds of evidence -------------------------------------------

// passkeyProof asks the authenticator to sign the challenge over the task.
// The challenge is proof.challenge, the same SHA-256 every verifier computes,
// so what the authenticator signed is what the task says.
async function passkeyProof(purpose, payload, passkeys, rpID) {
  try {
    const cred = await navigator.credentials.get({
      publicKey: {
        challenge: await proof.challenge(purpose, payload),
        rpId: rpID,
        userVerification: "required",
        allowCredentials: passkeys.map((k) => ({ type: "public-key", id: k.credId })),
      },
    });
    if (!cred || !cred.response) return null;
    const raw = new Uint8Array(cred.rawId);
    // WHICH key signed is the authenticator's answer and not a guess: the
    // credential id it hands back names it. A kid picked by position would
    // name the wrong key the moment the user has two.
    const used = passkeys.find((k) => sameBytes(k.credId, raw));
    if (used === undefined) return null;
    return {
      kind: proof.KIND_PASSKEY,
      kid: used.id,
      sig: new Uint8Array(cred.response.signature),
      authData: new Uint8Array(cred.response.authenticatorData),
      clientData: new Uint8Array(cred.response.clientDataJSON),
    };
  } catch {
    // A cancelled authenticator, a timeout, an engine without WebAuthn: all
    // of them mean the same to the caller - no confirmation, nothing runs.
    return null;
  }
}

// passwordProof derives the user's password key again and signs the task with
// it. The seed lives for one signature and is wiped; the key is imported
// unextractable, so it cannot leave the engine either.
async function passwordProof(purpose, payload, key, passwordText) {
  if (passwordText === "") return null;
  let seed = null;
  try {
    seed = await proof.deriveSeed(passwordText, key.kdf);
    const priv = await proof.privateFromSeed(seed);
    const sig = new Uint8Array(await crypto.subtle.sign({ name: "Ed25519" }, priv,
      message(purpose, payload)));
    return { kind: proof.KIND_PASSWORD, kid: key.id, sig, authData: EMPTY, clientData: EMPTY };
  } catch {
    return null;
  } finally {
    if (seed !== null) seed.fill(0);
  }
}

const sameBytes = (a, b) => {
  if (!(a instanceof Uint8Array) || !(b instanceof Uint8Array) || a.length !== b.length) return false;
  let diff = 0;
  for (let i = 0; i < a.length; i++) diff |= a[i] ^ b[i];
  return diff === 0;
};

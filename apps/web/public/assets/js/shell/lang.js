// The sentences the masks say, in the five languages of the house rule
// (de, en, fr, it, es). One file per language under /assets/lang/, the same
// keys in every one of them, and a new language is one more file and no line
// of code here - lang.js never holds a list of languages for its own sake,
// it holds the names of the files it may ask for.
//
// WHY THE FILES LIE IN public/assets/lang/ AND NOT IN apps/web/lang/, which
// is where the project standard puts translations: the browser has to fetch
// them, and only public/ is reachable from outside (apps/web/CLAUDE.md, and
// the one entry point of public/index.php). A lang/ outside the web root
// would need a second route through PHP, which is the one thing the README
// of this app promises there is not.
//
// WHAT IS HERE AND WHAT IS NOT. The keys are the sentences of the login mask
// and of the first run - every sentence a person reads when something went
// wrong. The REST OF THE SHELL IS STILL GERMAN IN ITS SOURCE, and moving it
// is its own task. How much there is of it, with the command, because a
// number without one is no number:
//
//   grep -rhoE '"[A-ZAOU][^"]{4,}"' auth/ shell/ apps/ | sort -u | wc -l
//
// answers 283 here (294 before the keys below left those files). It is an
// UPPER BOUND and not a count of sentences: it catches "Enter" and "Orange"
// too, which are a key name and a colour and no text for anybody. So: this
// is a mechanism with the error sentences in it, not a translated shell, and
// nothing here pretends otherwise.
//
// Fallback is per KEY and not per file, which is what the house rule asks
// for: a key the chosen language does not have is answered out of English,
// and a key English does not have either is answered out of BUILT_IN below.
// Only a key that is in no table at all comes back as "" - never the naked
// key, which is no sentence for anybody.
//
// WHY BUILT_IN EXISTS, and it is MEASURED, not foreseen. The sentences used
// to be constants in auth/login.js and auth/setup.js, so a mask always had
// them; with the files they are one fetch away, and a browser that cannot
// get the files has no sentence at all. Measured in chromium, rendered: with
// every /assets/lang/*.json refused and a wrong password typed, the alert of
// the login mask held "" and was not even laid out (offsetHeight 0). Before
// the files it read "Anmeldung fehlgeschlagen." A release that ships
// assets/js/ and forgets assets/lang/ is exactly that state - the release
// packer names what goes out file by file (tools/release/extra-web.txt, on
// the unmerged release-packer branch) - and so is a disk that fills up while
// the page is already open.
//
// It is NOT a second table that may drift: the English file and BUILT_IN are
// held key for key and value for value by a case of shell/busy.spec.js, so a
// sentence changed in one of them and not the other is a red case and not a
// surprise at somebody's screen.
import { decode, Budget, M, str } from "../contracts/json.js";
import { readBounded } from "../net/http.js";

// The files that may be asked for. A name from outside never reaches the
// address: what the browser offers is held against this list, so a tag like
// "../../etc/ngc/web.json" cannot become a path. That is the whole reason
// this list exists - not to know which languages there are.
export const LANGS = Object.freeze(["de", "en", "fr", "it", "es"]);

// The language a file is written in when nothing else fits, and the one every
// missing key is answered out of.
export const FALLBACK = "en";

// MEASURED with `wc -c public/assets/lang/*.json`: the five files are 1 278
// to 1 482 bytes, the largest of them French. The bound is here so an answer
// that is NOT a language file is dropped while it arrives rather than after
// it is all in memory - the same reason net/config.js bounds /config.json.
export const MAX_LANG = 8 << 10;

// The English sentences, compiled in, so that a mask is never silent. It is a
// COPY of public/assets/lang/en.json and says so: the file is what somebody
// translates and what the browser reads, this is the floor under it for the
// one case where no file could be read at all. The case that holds the two
// equal is "the built-in sentences are the English file, word for word" in
// shell/busy.spec.js.
// THE BROWSERS THIS SHELL KNOWS CANNOT DO A PASSWORD LOGIN, as data and in one
// place, because two sentences have to agree about them.
//
// MEASURED (18.10.2026, Playwright's Firefox 141 on Windows): exportKey("jwk")
// of an Ed25519 key imported from a PKCS#8 seed answers OperationError, so
// contracts/proof.js publicFromSeed has no way to the public half and no
// password gets in. shell/main.js passwordLoginFits() measures it per engine at
// run time - this list is not a gate and must never become one. It is only
// here so the ADVICE the shell gives cannot recommend one of them: a case holds
// boot.tooold against this list in all five languages
// (apps/web/tests/shell/engines.spec.js).
//
// A name goes OUT of here the day an engine is measured to do it, and then the
// advice may name it again.
export const NO_PASSWORD_LOGIN = Object.freeze(["Firefox"]);

export const BUILT_IN = Object.freeze({
  __proto__: null,
  "boot.tooold": "This browser is too old for NGC. Please use Chrome 137 or Safari 17.4 or newer.",
  "boot.noroute.password": "This browser cannot do NGC's password login: it does not hand out the public half of a password key. Please use Chrome 137 or Safari 17.4 or newer. Firefox cannot do it up to version 141.",
  "boot.noroute.passkeysoff": "No login is possible in this browser: it cannot do the password login, and passkeys are switched off here. Please switch passkeys on in the browser settings, or use Chrome 137 or Safari 17.4 or newer.",
  "err.unreachable": "Service unavailable. Please try again later.",
  "err.busy": "The service is busy right now. Please try again in {seconds} seconds.",
  "login.failed": "Login failed.",
  "login.nopasswordkeys": "This browser cannot do a password login. Please log in with a passkey.",
  "login.locked": "Too many failed attempts. Please try again later.",
  "login.locked.left": "Too many failed attempts. {seconds} seconds left.",
  "login.totp.ask": "Please enter the code from your authenticator app.",
  "setup.code.incomplete": "The one-time code is not complete.",
  "setup.code.nomatch": "The one-time code does not fit this installation. Please check the code and try again.",
  "setup.silent": "The installation is not answering right now. The one-time code stays valid. Please try again in a moment.",
  "setup.refused": "The installation refused the setup. Please install NGC again and use the new code.",
  "setup.already": "This installation is already set up.",
  "setup.broken": "The setup did not work. Please reload the page and try again.",
  "setup.name.bad": "A login name may hold only lower-case letters, digits and . _ - and must begin with a letter or a digit.",
  "setup.password.missing": "Please choose a password.",
  "setup.password.different": "The two passwords are not the same.",
  "setup.signin": "NGC is set up. Please sign in now.",
});

// A map of strings, read through the house decoder with a budget: a language
// file is a file on a server, and a file on a server is exactly what somebody
// who got that far would edit. json.MAX_MEMBERS caps it at 64 keys, which is
// the growth this table is allowed before somebody has to think about it -
// MEASURED through this very shape rather than read off the constant: 64 keys
// decode, 65 leave as BadJSON "too many members".
const langShape = M(str);

let chosen = FALLBACK;
let table = Object.freeze({ __proto__: null });
let fallbackTable = Object.freeze({ __proto__: null });

// pick takes what the browser offers and answers with a name out of LANGS.
// It walks the offers in the browser's own order of preference and compares
// only the primary subtag: "de-CH" and "de" are the same file, and a region
// nobody has a file for is not a reason to fall back to English.
//
// It is exported because the test asks it directly: a function that is only
// ever reached through a fetch is a function measured through two things.
export function pick(offers) {
  const list = Array.isArray(offers) ? offers : [];
  for (const offer of list) {
    if (typeof offer !== "string") continue;
    const primary = offer.toLowerCase().split("-")[0];
    if (LANGS.includes(primary)) return primary;
  }
  return FALLBACK;
}

async function fetchLang(name) {
  let res;
  try {
    res = await fetch(`/assets/lang/${name}.json`, { cache: "no-store" });
  } catch {
    return null;
  }
  if (!res.ok) return null;
  const raw = await readBounded(res, MAX_LANG);
  if (raw === null) return null;
  try {
    // MEASURED, not assumed: decode hands M(str) back as a PLAIN OBJECT and
    // not as a Map. The first version of this read it with
    // "for (const [k, v] of got)", which throws on an object, was caught by
    // the catch below and left every sentence empty - four cases red, and the
    // one that fetched the file itself green beside them, which is what made
    // it look like a loader that worked.
    //
    // It is copied into an object with NO PROTOTYPE, so t() for a key from
    // anywhere answers out of the file and never out of Object.prototype -
    // the same reason contracts/wire.js keeps its own table that way.
    const got = decode(raw, new Budget({ size: MAX_LANG }), langShape);
    const out = { __proto__: null };
    for (const [k, v] of Object.entries(got)) out[k] = v;
    return Object.freeze(out);
  } catch {
    return null;
  }
}

// load reads the language of this browser and English beside it, and says
// which language it settled on. It is called once, before the first mask is
// drawn; a second call simply reads again.
//
// A file that cannot be read is NOT a failure of the page: the shell goes on
// with what it has, and t() answers "" for a key it then does not hold. A
// login mask that refuses to appear because a sentence could not be fetched
// would be worse than a login mask with one empty line in it.
export async function load(offers = navigator.languages ?? [navigator.language]) {
  chosen = pick(offers);
  fallbackTable = (await fetchLang(FALLBACK)) ?? Object.freeze({ __proto__: null });
  table = chosen === FALLBACK ? fallbackTable : (await fetchLang(chosen)) ?? fallbackTable;
  document.documentElement.lang = chosen;
  return chosen;
}

export function language() {
  return chosen;
}

// t answers the sentence for a key, with {name} filled in from vars. The
// values are put in as they are and the result goes to textContent, so there
// is nothing here to escape; a placeholder nobody passed a value for stays
// as it is written, which shows up in a test rather than disappearing.
export function t(key, vars = null) {
  const found = typeof key === "string"
    ? (table[key] ?? fallbackTable[key] ?? BUILT_IN[key] ?? "")
    : "";
  if (vars === null || found === "") return found;
  let out = found;
  for (const [name, value] of Object.entries(vars)) {
    out = out.split(`{${name}}`).join(String(value));
  }
  return out;
}

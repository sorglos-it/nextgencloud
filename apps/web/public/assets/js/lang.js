// Every sentence a person reads comes from here.
//
// WHY THE TABLES ARE IN THE PAGE AND NOT FETCHED. The call sites are deep - a
// message about a package that did not arrive is built four frames inside a
// click handler - so t() has to be a plain synchronous call, which means the
// tables have to be there before any other module's body runs. There were two
// ways to get that, and only one of them works:
//
//   a top-level await in this file. Tried, and MEASURED: the page itself came
//     up right on all three engines, but WebKit then refuses to hand this
//     module to a dynamic import out of an injected evaluation
//     ("ReferenceError: Cannot access uninitialized variable"), and since
//     apps/users.js imports this file, that took 19 cases of
//     tests/shell/users.spec.js with it - cases that were green before. A
//     top-level await anywhere in a module graph poisons the whole graph for
//     that way of loading it.
//   the page carries them. templates/desktop.php writes the chosen language
//     and English into a <script type="application/json"> block, because PHP
//     is the only part that can look into apps/web/lang/ anyway. Nothing here
//     awaits anything, there are two fetches fewer at boot, and this is an
//     ordinary module again.
//
// WHY THE LANGUAGES ARE NOT LISTED ANYWHERE. classes/Lang.php reads the folder
// and picks against Accept-Language; a new language is one more file in
// apps/web/lang/ and nothing else - no code change, no build, no table. The
// folder is served as well (GET /lang/index.json and /lang/<tag>.json), for
// anything that is not this page.

// THE ONE EXCEPTION TO "NO TEXT IN THE CODE", and it is the message that says
// the texts are not there. It cannot come out of the files it is about. It
// carries all five languages so that the greeting of the fault is at least in
// a language the person reads, and it is the only literal sentence in here.
const NOLANG = {
  de: "NGC konnte seine Sprachdateien nicht laden. Bitte die Seite neu laden.",
  en: "NGC could not load its language files. Please reload the page.",
  fr: "NGC n'a pas pu charger ses fichiers de langue. Veuillez recharger la page.",
  it: "NGC non ha potuto caricare i suoi file di lingua. Ricaricare la pagina.",
  es: "NGC no pudo cargar sus archivos de idioma. Vuelva a cargar la página.",
};

// English is the fall-back for a missing key and for a missing language. That
// is the rule, and it is why this one tag stands in the code: it is not a list
// of what there is, it is which one catches.
const FALLBACK = "en";

// A table and nothing else. An array, a number or a member that is no string
// would make t() answer undefined, which is the naked key the rule forbids, so
// it is turned away here where it can still be seen.
function tableOf(v) {
  if (v === null || typeof v !== "object" || Array.isArray(v)) return null;
  for (const k of Object.keys(v)) if (typeof v[k] !== "string") return null;
  return v;
}

function fromPage() {
  const none = { tag: FALLBACK, main: null, base: null };
  if (typeof document === "undefined") return none;
  const block = document.getElementById("ngc-lang");
  if (block === null) return none;
  let v;
  try {
    v = JSON.parse(block.textContent);
  } catch {
    return none;
  }
  if (v === null || typeof v !== "object") return none;
  const tag = typeof v.tag === "string" && /^[a-z]{2}(-[a-z]{2})?$/.test(v.tag) ? v.tag : FALLBACK;
  return { tag, main: tableOf(v.main), base: tableOf(v.base) };
}

const loaded = fromPage();

// tag is the language in force: for anything that formats a number or a date,
// and the one the page's lang attribute already carries.
export const tag = loaded.tag;

// fill puts the values into the text. A placeholder is {name}, and a name the
// caller did not pass stays as it stands rather than becoming "undefined": a
// visible {title} is a bug somebody can report in the words they see,
// "undefined" is one nobody can name.
function fill(s, params) {
  if (!params) return s;
  return s.replace(/\{([A-Za-z0-9_]+)\}/g, (whole, name) => (
    Object.prototype.hasOwnProperty.call(params, name) ? String(params[name]) : whole
  ));
}

// t is the whole of this file's surface: a key in, a sentence out.
//
// The order is the rule: the chosen language, then English, and NEVER the
// naked key and never an empty place. A key that is in no file at all can only
// come from a typo in the code - the test that holds the five files to the same
// keys makes the other way impossible - and the person then gets the one
// sentence that tells them what to do instead of a word out of a programme.
export function t(key, params) {
  const m = loaded.main !== null && typeof loaded.main[key] === "string" ? loaded.main[key] : null;
  if (m !== null) return fill(m, params);
  const b = loaded.base !== null && typeof loaded.base[key] === "string" ? loaded.base[key] : null;
  if (b !== null) return fill(b, params);
  return NOLANG[loaded.tag] ?? NOLANG[FALLBACK];
}

// has says whether a key is really there. Only a test asks: t() can never
// answer "no", which is the point of it, so anything that wants to know needs
// a second question.
export function has(key) {
  return (loaded.main !== null && typeof loaded.main[key] === "string")
    || (loaded.base !== null && typeof loaded.base[key] === "string");
}

// codeText is the sentence for one of wire's fourteen error codes, out of
// lang/ and not out of the table in contracts/wire.js. The two are on purpose
// not the same thing: the German table there is a CONTRACT - its text travels
// in the CLOSE frame and shared/wire/wire.go holds the same fourteen words -
// while this is what a person reads. A code that is none answers "", exactly
// as wire.message does, so a caller can keep its own `|| fallback`.
export const codeText = (code) => (has("wire." + code) ? t("wire." + code) : "");

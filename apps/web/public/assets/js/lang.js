// Every sentence a person reads comes from here.
//
// WHY A MODULE WITH A TOP-LEVEL AWAIT. The call sites are deep: a message
// about a package that did not arrive is built four frames inside a click
// handler, and threading a loaded table down to it would mean an argument on
// every function on the way. A module that awaits its own data at import time
// gives every importer a table that is already full: the module graph of the
// page waits for this file before any other module's body runs, so t() is a
// plain synchronous call everywhere, and there is no order to get wrong and no
// "not initialised yet" state to fall into.
//
// WHY THE LANGUAGES ARE NOT LISTED HERE. A new language is one more file in
// apps/web/lang/ and nothing else - no code change, no build, no entry in a
// table. The server reads the folder and answers /lang/index.json with what is
// in it; this file asks for that list and matches it against what the browser
// says the person reads. A list in the code would be a second place to forget.

// THE ONE EXCEPTION TO "NO TEXT IN THE CODE", and it is the message that says
// the texts could not be fetched. It cannot come out of the files it is about.
// It carries all five languages so that at least the greeting of the fault is
// in a language the person reads, and it is the only literal sentence in this
// folder.
const NOLANG = {
  de: "NGC konnte seine Sprachdateien nicht laden. Bitte die Seite neu laden.",
  en: "NGC could not load its language files. Please reload the page.",
  fr: "NGC n'a pas pu charger ses fichiers de langue. Veuillez recharger la page.",
  it: "NGC non ha potuto caricare i suoi file di lingua. Ricaricare la pagina.",
  es: "NGC no pudo cargar sus archivos de idioma. Vuelva a cargar la página.",
};

// English is the fall-back for a missing key and for a missing language. That
// is the rule, and it is why this name is the only tag that stands in the
// code: it is not a list of what there is, it is which one catches.
const FALLBACK = "en";

async function fetchJSON(url) {
  const res = await fetch(url, { cache: "no-store" });
  if (!res.ok) throw new Error("lang");
  const v = await res.json();
  // A table and nothing else. An array or a number would make every t() below
  // answer with undefined, which is the naked key the rule forbids, so it is
  // refused here where it can still be seen.
  if (v === null || typeof v !== "object" || Array.isArray(v)) throw new Error("lang");
  return v;
}

// pick chooses the language file out of what the server has and what the
// browser says. navigator.languages is in the person's own order of
// preference, so the first hit wins. The exact tag goes first ("pt-BR" before
// "pt"), because a folder that one day holds both must be able to tell them
// apart without a change here.
export function pick(available, wanted) {
  const have = new Set(available);
  for (const tag of wanted) {
    const low = String(tag).toLowerCase();
    if (have.has(low)) return low;
    const base = low.split("-")[0];
    if (have.has(base)) return base;
  }
  return have.has(FALLBACK) ? FALLBACK : null;
}

async function load() {
  let available;
  try {
    const idx = await fetchJSON("/lang/index.json");
    available = Array.isArray(idx.languages) ? idx.languages.map((s) => String(s).toLowerCase()) : [];
  } catch {
    available = [];
  }
  const wanted = Array.isArray(navigator.languages) && navigator.languages.length
    ? navigator.languages
    : [navigator.language || FALLBACK];
  const chosen = pick(available, wanted);
  // English always comes along, even when it is not the chosen language: a key
  // that one translation has not got yet is read out of it. Two fetches of a
  // few kilobytes at boot, once.
  const [base, main] = await Promise.all([
    fetchJSON("/lang/" + FALLBACK + ".json").catch(() => null),
    chosen && chosen !== FALLBACK ? fetchJSON("/lang/" + chosen + ".json").catch(() => null) : Promise.resolve(null),
  ]);
  return { tag: chosen ?? FALLBACK, base, main, wanted };
}

const loaded = await load();

// tag is the language in force, for lang="…" on the document and for anything
// that formats a number or a date.
export const tag = loaded.tag;

// fill puts the values into the text. A placeholder is {name}, and a name the
// caller did not pass stays as it stands rather than becoming "undefined":
// a visible {left} is a bug somebody reports, "undefined" is one nobody can
// name.
function fill(s, params) {
  if (!params) return s;
  return s.replace(/\{([A-Za-z0-9_]+)\}/g, (whole, name) => (
    Object.prototype.hasOwnProperty.call(params, name) ? String(params[name]) : whole
  ));
}

// t is the whole of this file's surface: a key in, a sentence out.
//
// The order is the rule: the chosen language, then English, and NEVER the
// naked key and never an empty place. A key that is in no file at all can
// only come from a typo in the code - the test that holds the five files to
// the same keys makes the other way impossible - and the person gets the one
// sentence that tells them what to do instead of a word out of a programme.
export function t(key, params) {
  const m = loaded.main && typeof loaded.main[key] === "string" ? loaded.main[key] : null;
  if (m !== null) return fill(m, params);
  const b = loaded.base && typeof loaded.base[key] === "string" ? loaded.base[key] : null;
  if (b !== null) return fill(b, params);
  return NOLANG[loaded.tag] ?? NOLANG[FALLBACK];
}

// codeText is the sentence for one of wire's fourteen error codes, out of
// lang/ and not out of the table in contracts/wire.js. The two are on purpose
// not the same thing: the German table there is a CONTRACT - its text travels
// in the CLOSE frame and shared/wire/wire.go holds the same fourteen words -
// while this is what a person reads. A code that is none answers "", exactly
// as wire.message does, so a caller can keep its own `|| fallback`.
export const codeText = (code) => (has("wire." + code) ? t("wire." + code) : "");

// has says whether a key is really there. Only the tests ask: t() can never
// answer "no", which is the point of it, so a test that wants to prove a key
// is missing needs a second question.
export const has = (key) => (
  (loaded.main && typeof loaded.main[key] === "string")
  || (loaded.base && typeof loaded.base[key] === "string")
);

// The document says which language it is in. A screen reader picks its voice
// from this, and a page that says "de" while it shows French is read out
// wrongly - that is the whole reason this line is here and not a nicety.
if (typeof document !== "undefined" && document.documentElement) {
  document.documentElement.lang = loaded.tag;
}

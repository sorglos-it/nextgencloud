// Vorlagen: ein Ordner unter dem Webserver, je Vorlage ein Unterordner, der
// das ganze Aussehen mitbringt. Der Benutzer wählt eine aus, mehr nicht
// (docs/gestaltung.md, Punkt 8).
//
// WARUM DAS KEIN FREMDES CSS IST, und warum die Regel der Seite deshalb zu
// bleibt: hochladen darf nur ein Verwalter, also liegt jede Vorlage unter dem
// EIGENEN Webserver. Das Stylesheet der Oberfläche kommt damit von 'self' -
// genau das, was Page::CSP schon erlaubt. Nichts an style-src wird angefasst,
// hier nicht und in Page.php nicht.
//
// WIE WEIT EINE VORLAGE IN EINE APP DARF, ausgeschrieben und gemessen in
// docs/vorlagen-und-der-rahmen.md: genau ein weiteres Stylesheet, als Text,
// von der OBERFLÄCHE geholt und als data:-Adresse in das Dokument gehängt,
// das sie für die App baut. Der Rahmen behält seinen eigenen Ursprung, holt
// weiter nichts und bekommt nichts zurück.
//
// Die Wahl liegt wie Hell/Dunkel, Farbe und Tapete im localStorage des
// Benutzers: sie entscheidet, wie der Schreibtisch aussieht, nie, was er
// darf. Ein geschlossener Speicher ist deshalb kein Fehler - dann gilt die
// Wahl für diese Seite und ist auf der nächsten vergessen.

const KEY_LOOK = "ngc.look";

// Wo die Vorlagen liegen. Eine Zeile, weil jeder Pfad dieses Moduls von hier
// aus gebaut wird und ein zweiter Ort der Ort wäre, der abdriftet.
const DIR = "/assets/looks/";

// Wie eine Kennung aussehen darf. Sie wird zu einem Pfad, also ist das hier
// die Stelle, an der ein Pfad entsteht oder nicht entsteht: Kleinbuchstaben,
// Ziffern und Striche, sonst nichts. Kein Punkt, also auch kein "..", kein
// Schrägstrich, also kein Ausbruch, und eine Länge, die feststeht.
const ID = /^[a-z0-9][a-z0-9-]{0,31}$/;

// Was ein Name sein darf: ein kurzer Text ohne Steuerzeichen. Er geht als
// Textknoten auf die Seite (dom.el), nie als Markup - die Grenze hier ist
// gegen einen Namen, der die Liste unleserlich macht, nicht gegen Markup.
const MAX_NAME = 40;

// Die Liste, einmal geholt. Sie ändert sich nur, wenn ein Verwalter etwas
// hochgeladen hat, und dann wird die Seite ohnehin neu geladen.
let known = null;

const listeners = new Set();

function read() {
  try {
    return localStorage.getItem(KEY_LOOK);
  } catch {
    return null;
  }
}

function write(v) {
  try {
    if (v === "") localStorage.removeItem(KEY_LOOK);
    else localStorage.setItem(KEY_LOOK, v);
  } catch {
    // Nichts zu tun und nichts zu sagen: die Wahl gilt für diese Seite.
  }
}

export function validLookID(id) {
  return typeof id === "string" && ID.test(id);
}

// looks holt die Liste der Vorlagen. Sie kommt als JSON vom eigenen Server,
// und sie wird geprüft wie alles, was von außen kommt: ein Objekt mit einem
// Feld "looks", darin Einträge mit einer gültigen Kennung und einem kurzen
// Namen. Ein Eintrag, der das nicht ist, fliegt raus; eine Liste, die gar
// nicht geht, ist eine leere Liste. Ohne Vorlagen sieht der Schreibtisch aus,
// wie er aussieht - das ist kein Fehler und sagt deshalb auch keiner.
export async function looks() {
  if (known !== null) return known;
  known = [];
  let data;
  try {
    const r = await fetch(DIR + "looks.json", { cache: "no-store" });
    if (!r.ok) return known;
    data = await r.json();
  } catch {
    return known;
  }
  if (data === null || typeof data !== "object" || !Array.isArray(data.looks)) return known;
  const seen = new Set();
  for (const e of data.looks) {
    if (e === null || typeof e !== "object") continue;
    if (!validLookID(e.id) || seen.has(e.id)) continue;
    if (typeof e.name !== "string") continue;
    const name = e.name.replace(/[\u0000-\u001f\u007f]/g, "").trim();
    if (name === "" || name.length > MAX_NAME) continue;
    seen.add(e.id);
    known.push({ id: e.id, name });
  }
  return known;
}

// currentLook ist, was auf der Seite liegt, nicht, was gespeichert ist: mit
// geschlossenem Speicher gibt es keine Kennung zu nennen, und der Aufrufer
// soll dann "keine Vorlage" sehen und nicht einen Namen, der nicht wirkt.
export function currentLook() {
  const l = document.getElementById("looksheet");
  return l ? (l.dataset.look ?? "") : "";
}

// applyLook hängt das Stylesheet der Vorlage hinter shell.css. Hinter, weil
// eine Vorlage die Marken der Oberfläche überschreiben soll - das ist, wozu
// sie da ist.
//
// "" nimmt die Vorlage wieder weg; das ist das mitgelieferte Aussehen und
// steht in der Auswahl als eigener Eintrag.
export function applyLook(id) {
  const want = id === "" ? "" : (validLookID(id) ? id : null);
  // Eine Kennung, die keine ist, wird nicht zu einem Pfad. Still, weil sie
  // nur aus dem eigenen Speicher kommen kann und der Benutzer dafür nichts
  // kann: er bekommt das mitgelieferte Aussehen.
  if (want === null) return;
  const old = document.getElementById("looksheet");
  if (want === "") {
    if (old) old.remove();
    write("");
  } else {
    const l = old ?? document.createElement("link");
    l.id = "looksheet";
    l.rel = "stylesheet";
    l.dataset.look = want;
    l.href = DIR + want + "/shell.css";
    if (!old) document.head.append(l);
    write(want);
  }
  for (const fn of listeners) {
    try {
      fn(want);
    } catch {
      // Ein Zuhörer, der wirft, hält die anderen nicht auf.
    }
  }
}

export function onLookChange(fn) {
  if (typeof fn === "function") listeners.add(fn);
}

// initLook läuft, bevor der Schreibtisch gebaut wird, damit die Seite nicht
// erst ohne und dann mit Vorlage gemalt wird. Es prüft die Kennung gegen die
// Grammatik, nicht gegen die Liste: die Liste zu holen hieße, auf eine
// Antwort zu warten, bevor überhaupt etwas auf dem Schirm steht. Eine
// Kennung, zu der es keinen Ordner gibt, ist ein Stylesheet, das nicht lädt -
// und das ist genau das mitgelieferte Aussehen.
export function initLook() {
  const id = read();
  if (validLookID(id)) applyLook(id);
}

// --------------------------------------------------- das Aussehen einer App

// Eine App-Kennung wird hier zu einem Dateinamen, also gilt dieselbe Regel
// wie für die Vorlage selbst, plus den Punkt, den eine App-Kennung trägt
// (org.sorglos.terminal). Kein Schrägstrich, kein doppelter Punkt.
const APP_ID = /^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$/;

export function validAppID(id) {
  return typeof id === "string" && id.length > 0 && id.length <= 128
    && !id.includes("..") && APP_ID.test(id);
}

// appCSS holt das Aussehen, das die Vorlage für eine App mitbringt, und gibt
// den geputzten Text zurück. Gibt es keines, kommt "" - und die App behält
// ihr eigenes.
//
// DIE OBERFLÄCHE HOLT DAS, NICHT DER RAHMEN. Der Rahmen hat connect-src
// 'none' und bleibt dabei; hier ist 'self' der eigene Server, was Page::CSP
// seit jeher erlaubt.
export async function appCSS(appID) {
  const look = currentLook();
  if (look === "" || !validAppID(appID)) return "";
  try {
    const r = await fetch(DIR + look + "/apps/" + appID + ".css", { cache: "no-store" });
    if (!r.ok) return "";
    // Nur was der Server auch als Stylesheet ausliefert. Eine Fehlerseite mit
    // text/html wäre sonst "CSS", das der Browser still verwirft - und die
    // Vorlage sähe aus, als hätte sie nichts zu sagen.
    const type = (r.headers.get("content-type") ?? "").split(";")[0].trim().toLowerCase();
    if (type !== "text/css") return "";
    return clean(await r.text());
  } catch {
    return "";
  }
}

// clean nimmt aus dem Stylesheet alles heraus, was den Rahmen nach draußen
// zeigen lassen würde.
//
// ZWEIMAL ZU, UND DAS MIT ABSICHT. Der Rahmen würde nichts davon laden: er
// hat img-src data:, font-src data: und connect-src 'none'. Aber eine Regel,
// auf die man sich verlässt, steht besser zweimal da als einmal - und eine
// Adresse, die hier schon weg ist, wird gar nicht erst zu einem Versuch, den
// der Browser meldet und der nach einer kaputten Vorlage aussieht.
//
// Was bleibt: url(data:...). Ein eingebettetes Bild ist der normale Weg und
// geht nirgendwo hin.
const URL_IN_CSS = /url\(\s*(?:"([^"]*)"|'([^']*)'|([^)"'\s]*))\s*\)/gi;

export function clean(src) {
  if (typeof src !== "string") return "";
  // @import holt eine zweite Datei nach - die Stelle, an der ein Stylesheet
  // aufhört, eine Datei zu sein. Es geht ganz raus, bis zum Semikolon.
  let out = src.replace(/@import[^;]*;?/gi, "");
  out = out.replace(URL_IN_CSS, (whole, dq, sq, bare) => {
    const ref = (dq ?? sq ?? bare ?? "").trim();
    if (/^data:/i.test(ref)) return whole;
    // Eine leere Adresse statt der entfernten: die Angabe bleibt stehen und
    // zeigt auf nichts, statt zu verschwinden und eine Angabe darunter
    // plötzlich gelten zu lassen.
    return 'url("data:,")';
  });
  return out;
}

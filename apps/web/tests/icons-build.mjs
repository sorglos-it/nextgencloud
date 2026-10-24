// Writes docs/icons/*.svg out of the shell's own table (shell/icon.js) and
// builds docs/icons/uebersicht.html from the same source.
//
// The shapes have ONE home: assets/js/shell/icon.js. The .svg files are the
// copy an app builder takes into their own ui/, and the overview page is how
// a person looks at them. Running this is how they are kept the same;
// shell/desktop-icons.spec.js is what notices when somebody forgets to.
//
//   node apps/web/tests/icons-build.mjs
import { writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const out = join(here, "..", "..", "..", "docs", "icons");
const icon = await import("../public/assets/js/shell/icon.js");
const { SHAPES, BOX, LINE, SOLID, TITLES } = icon;
const ORDER = icon.names();

const attrs = (o) => Object.entries(o).map(([k, v]) => `${k}="${v}"`).join(" ");

export function svgText(name) {
  const body = SHAPES[name].map(([tag, a]) => {
    const own = Object.entries(a).filter(([k]) => k !== "solid")
      .map(([k, v]) => `${k}="${v}"`).join(" ");
    const style = a.solid ? attrs(SOLID) : "";
    return `  <${tag} ${own}${style ? " " + style : ""}/>`;
  }).join("\n");
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="${BOX}" width="24" height="24" `
    + `${attrs(LINE)} role="img">\n  <title>${TITLES[name]}</title>\n${body}\n</svg>\n`;
}

if (process.argv[1]?.endsWith("icons-build.mjs")) {
  for (const n of ORDER) writeFileSync(join(out, n + ".svg"), svgText(n));
  writeFileSync(join(out, "uebersicht.html"), page());
  console.log("wrote", ORDER.length, "icons and uebersicht.html to docs/icons/");
}

function inline(name) {
  return svgText(name).trim().replace(' width="24" height="24"', "").replace(/\n/g, "\n        ");
}

function page() {
  const cell = (n) => `<div class="zelle"><div class="reihe">`
    + `<span class="s24">${inline(n)}</span><span class="s32">${inline(n)}</span>`
    + `<span class="s48">${inline(n)}</span></div><div class="n">${n}</div></div>`;
  const entry = (n) => `<span class="eintrag">${inline(n)}<span>${TITLES[n]}</span></span>`;
  const panel = (cls, label) => `<div class="panel ${cls}"><h2>${label}</h2>`
    + `<div class="gitter">${ORDER.map(cell).join("\n")}</div>`
    + `<h2 style="margin-top:28px">Startmenue, 24 px</h2>`
    + `<div class="menue">${ORDER.filter((n) => n !== "rechte" && n !== "einstellungen"
      && n !== "dateien" && n !== "knoten").map(entry).join("\n")}</div></div>`;
  return `<!doctype html>
<html lang="de"><head><meta charset="utf-8">
<title>NGC App-Icons</title>
<style>
:root{color-scheme:light}
body{margin:0;font:14px/1.45 system-ui,sans-serif;background:#f4f5f7;color:#101215}
h1{font-size:20px;margin:0 0 4px}
header,footer{padding:20px 24px}
p{margin:4px 0;max-width:62em}
.pair{display:grid;grid-template-columns:1fr 1fr;gap:0}
.panel{padding:20px 24px 28px}
.panel.hell{background:#f4f5f7;color:#101215}
.panel.dunkel{background:#15181d;color:#e6e9ef}
.panel h2{font-size:13px;letter-spacing:.08em;text-transform:uppercase;opacity:.6;margin:0 0 16px}
.gitter{display:grid;grid-template-columns:repeat(auto-fill,minmax(112px,1fr));gap:18px 12px}
.zelle{text-align:center}
.reihe{display:flex;align-items:flex-end;justify-content:center;gap:10px;min-height:52px}
.n{margin-top:8px;font-size:11px;opacity:.75;word-break:break-word}
.s24 svg{width:24px;height:24px}
.s32 svg{width:32px;height:32px}
.s48 svg{width:48px;height:48px}
.menue{display:flex;flex-wrap:wrap;gap:4px;margin-top:8px}
.eintrag{display:flex;align-items:center;gap:8px;padding:6px 10px;border-radius:6px;font-size:13px}
.panel.hell .eintrag{background:#fff}
.panel.dunkel .eintrag{background:rgba(255,255,255,.08)}
.eintrag svg{width:24px;height:24px;flex:0 0 auto}
code{font-family:ui-monospace,monospace;font-size:12px}
</style></head><body>
<header>
<h1>NGC App-Icons</h1>
<p>Ein Satz. Zeichenflaeche 24&times;24, Rand 2, Strichstaerke 1,75, runde Enden,
<code>stroke="currentColor"</code>. Jedes Icon steht hier in 24, 32 und 48 Pixeln,
links auf hellem, rechts auf dunklem Grund.</p>
<p>Diese Seite und die .svg-Dateien daneben werden erzeugt, nicht von Hand gepflegt:
<code>node apps/web/tests/icons-build.mjs</code>. Die Formen selbst stehen in
<code>apps/web/public/assets/js/shell/icon.js</code>.</p>
</header>
<div class="pair">
${panel("hell", "Hell")}
${panel("dunkel", "Dunkel")}
</div>
<footer>
<p>Die Icons erben die Textfarbe. Wo ein Icon als <code>&lt;img&gt;</code> geladen wird, erbt es
nichts und wird schwarz &mdash; dort gehoert es in die Seite hinein, nicht davor.
Die Anleitung fuer App-Bauer steht in <code>docs/app-weg.md</code>, Kapitel 9.</p>
</footer>
</body></html>
`;
}

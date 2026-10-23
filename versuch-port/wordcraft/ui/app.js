// A classic script, because the shell refuses an ES module in an app frame
// (apps/web/public/assets/js/shell/appframe.js, NEEDS_CLASSIC). No fetch, no
// localStorage, no worker, no eval: everything the app may do goes through
// ngc.call and ngc.open.
"use strict";

var ausgabe = document.getElementById("ausgabe");
var datei = document.getElementById("datei");

// The theme arrives before the first paint and again on every change; this is
// the only event the frame gets unasked for (ngc.js).
function thema(t) {
  document.documentElement.setAttribute("data-theme", t);
}
thema(ngc.theme);
ngc.onTheme(thema);

// ngc.call answers with the bytes the service wrote to stdout. The shell
// decodes nothing, so the app does.
function rufen(aktion) {
  ausgabe.textContent = "…";
  ngc.call("wordcraft", aktion, { file: datei.value }).then(function (antwort) {
    var bytes = antwort instanceof Uint8Array ? antwort : new Uint8Array(antwort || 0);
    ausgabe.textContent = new TextDecoder().decode(bytes) || "(keine Ausgabe)";
  }, function (fehler) {
    // A service that ended badly comes back as a rejection; what it wrote to
    // stderr the app never sees, by design.
    ausgabe.textContent = "Fehlgeschlagen: " + (fehler && fehler.message ? fehler.message : fehler);
  });
}

document.getElementById("holen").addEventListener("click", function () { rufen("text"); });
document.getElementById("angaben").addEventListener("click", function () { rufen("info"); });

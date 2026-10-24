// org.sorglos.uhr - the face of the app, inside the frame every app gets.
//
// A CLASSIC SCRIPT, NOT A MODULE. The frame the shell builds has an origin of
// its own and makes no connection at all, and a module is fetched; so a
// <script type="module"> never runs there and the shell says
// "Diese App braucht klassische Skripte" instead of showing the app
// (shell/appframe.js, NEEDS_CLASSIC).
//
// WHAT ARRIVES IS BYTES. ngc.call resolves with a Uint8Array - the raw stdout
// of the service, handed over as it came (net/world.js: "call(...) ->
// Promise<Uint8Array>", appframe.doCall passes the value through without
// reading it). So the decoding is this app's own business, and so is trusting
// none of it: the line lands on textContent, never innerHTML.
//
// WHAT NEVER ARRIVES IS STDERR. The service writes one line to each side on
// purpose (apps/uhr/classes/uhr.go). Only stdout is in the value below; the
// stderr line reaches the controller's log and nothing else, and only where an
// operator switched the text on. An app that showed its errors on stderr would
// show the person nothing.
(() => {
  "use strict";

  const zeit = document.getElementById("zeit");
  const hinweis = document.getElementById("hinweis");
  const lesen = document.getElementById("lesen");
  const decoder = new TextDecoder();

  // The shell tells the app its accent before the app's own scripts run, so
  // the first paint already has it; onTheme keeps it right afterwards.
  const paint = (accent) => document.documentElement.style.setProperty("--ngc-accent", accent);
  paint(ngc.accent);
  ngc.onTheme((theme, accent) => paint(accent));

  async function read() {
    lesen.disabled = true;
    hinweis.textContent = "Wird gelesen …";
    try {
      // "uhr" is the service and "zeit" the action, both exactly as the
      // manifest names them: the shell refuses an action the app's own
      // manifest does not carry (appframe.doCall, CODES.FORBIDDEN), so a
      // typo here is a refusal and not a call.
      const bytes = await ngc.call("uhr", "zeit");
      zeit.textContent = decoder.decode(bytes).trim();
      hinweis.textContent = "Die Zeit kommt vom Server.";
    } catch (err) {
      // One sentence, and it does not quote err: what went wrong between the
      // window and the service is for the person at the shell, and an app
      // that printed it would be telling itself about this installation.
      zeit.textContent = "—";
      hinweis.textContent = "Der Dienst dieser App hat nicht geantwortet.";
    }
    lesen.disabled = false;
  }

  lesen.addEventListener("click", read);
  read();
})();

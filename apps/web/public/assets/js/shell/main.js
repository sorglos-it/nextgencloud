// Boot of the shell: check the browser, then load the configuration. The
// rest follows in later tasks.
const NEEDED = ["Ed25519", "X25519"];

export async function browserFits() {
  // Both are needed for the session keys; a browser without them cannot open
  // a session at all, so the shell says so instead of failing halfway.
  for (const name of NEEDED) {
    try {
      const k = await crypto.subtle.generateKey({ name }, false, name === "Ed25519" ? ["sign", "verify"] : ["deriveBits"]);
      if (!k) return false;
    } catch {
      return false;
    }
  }
  return true;
}

function message(text) {
  const root = document.getElementById("root");
  root.replaceChildren();
  const p = document.createElement("p");
  p.className = "msg";
  p.textContent = text;
  root.append(p);
}

async function start() {
  if (!(await browserFits())) {
    message("Dieser Browser ist zu alt für NGC. Bitte Chrome 137, Firefox 130 oder Safari 17.4 "
      + "oder neuer benutzen.");
    return;
  }
  const res = await fetch("/config.json", { cache: "no-store" });
  if (!res.ok) {
    message("NGC ist nicht eingerichtet.");
    return;
  }
  window.__ngcConfig = await res.json();
  message("NGC wird geladen …");
}

start();

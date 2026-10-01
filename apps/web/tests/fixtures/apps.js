// A list of apps of our own, for the cases where the honest list of the
// stand-in is not the point.
//
// The page is served its shell/fake-link.js from here instead of from disk.
// Everything else stays as it is: the bytes go to the desktop the same way,
// and core.parseResult reads them the same way, so a test written with this
// measures the real path and not a shortcut around it.
//
// IT MOVES WITH TASK 17. shell/fake-link.js is a stand-in that gets deleted
// when shell/channel.js lands. Then the route below points at nothing and
// these tests fail loudly instead of passing on air - which is what should
// happen, because the answer will then come from a session and has to be
// given to the desktop in that session's own way.

// app fills in every field core.DesktopApp wants, so a case has to name only
// what it is about.
export function app(o) {
  return {
    id: "de.example.x", name: "X", version: "1.0.0", icon: "", entry: "",
    width: 400, height: 300, actions: [], state: "ok", core: false, node: "", ...o,
  };
}

// CALC is the one honest app the hostile cases keep beside the bad ones: it
// is how a case tells "the desktop refused the whole list" from "the desktop
// is broken".
//
// THE ID IS THE REAL ONE SINCE TASK 25 ("ngc.calc" before). It is a row of a
// list and nothing more here - the link above answers nothing but
// ngc.desktop/apps, so a window opened on it shows the frame's refusal and not
// a calculator. That is enough for what these cases measure: names, icons,
// titles and taskbar entries.
export const CALC = app({ id: "org.sorglos.calculator", name: "Rechner", width: 300, height: 420 });

// linkModule builds the module the page gets in place of shell/fake-link.js.
// It is written out as source, not as an object: the page imports a module,
// and the shape is the one of the stand-in it replaces - fakeWorld() hands
// back a link whose call() answers with bytes, and onNotice() takes a
// listener.
//
// THE NAME HAS TO BE THE ONE main.js IMPORTS. It was fakeLink until Task 22
// renamed the stand-in's export to fakeWorld; this file was not carried
// along, and from 2cd8d10 to 8669551 the module served here had no fakeWorld
// in it. A named import that is not there does not fail at the call, it
// fails at LINKING: main.js never ran, no desktop was ever built, and all
// twelve cases of desktop-hostile.spec.js failed at "#taskbar is visible" -
// measured on 8669551, 12 failed out of 12. None of them was measuring the
// desktop's refusals any more.
function linkModule(apps) {
  return `const enc = new TextEncoder();
const APPS = ${JSON.stringify(apps)};
export async function fakeWorld() {
  const notices = new Set();
  window.__ngcSendNotice = (level, text) => {
    const body = enc.encode(JSON.stringify({ level, text, app: "" }));
    for (const fn of notices) fn(body);
  };
  const link = {
    async call(appID, service, name) {
      if (appID !== "ngc.desktop" || name !== "apps") throw new Error("test link: nothing for " + appID + "/" + name);
      return enc.encode(JSON.stringify(APPS));
    },
    onNotice(fn) { if (typeof fn === "function") notices.add(fn); },
  };
  return { link, trust: { current: () => ({ list: { stores: [] } }) }, session: null, releaseKey: null };
}
`;
}

// serveApps must be called before the page is loaded: the module is fetched
// once, at boot.
export async function serveApps(page, apps) {
  await page.route("**/assets/js/shell/fake-link.js", (route) => route.fulfill({
    status: 200,
    headers: { "content-type": "text/javascript; charset=utf-8", "cache-control": "no-store" },
    body: linkModule(apps),
  }));
}

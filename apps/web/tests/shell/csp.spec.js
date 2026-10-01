import { test, expect } from "@playwright/test";
import { readdirSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";

// The headers of the web interface, and whether the browser really does what
// they say.
//
// THE POINT OF THIS FILE is that a policy is not a string, it is a behaviour.
// Every other file of this suite measures what the shell does; this one
// measures what the shell is NOT ALLOWED to do, and it measures it twice: the
// header that was sent, and the engine refusing the thing the header forbids.
// A header with a typo in it reads fine and fences nothing, and that is the
// one mistake a toContain over a long string does not catch - so the set of
// directive NAMES is checked as a set, and five of them are then measured
// against a real attempt in the page.
//
// NOTHING HERE SWITCHES THE POLICY OFF. The whole suite runs with it on
// (fixtures/page.js); there is no bypassCSP anywhere in this tree, measured
// with a grep over tests/ at the time this file was written.
//
// EVERY ATTEMPT HAS A CONTROL. "The foreign script did not run" is also true
// of a page where no script runs at all, of a typo in the test, and of a
// network that was never reachable. So each case that measures a refusal also
// measures the same mechanism succeeding where the policy allows it: the
// same-origin script DOES run, the same-origin fetch DOES answer, the
// same-origin worker DOES start. Without those lines the refusals below would
// be a test that cannot fail for its own sake.

// What classes/Page.php sends for the shell document. Written out here, in
// full, so that a change to the policy has to be made twice - and the second
// time with a test run behind it.
//
// NOTE ON script-src: the task brief for this file asked for
// "script-src 'self' data:". That was written before Task 21 decided WHERE an
// app window comes from. It is /frame.html, a document of this server with a
// policy of ITS OWN (Page::FRAME_CSP), so the shell does not need data: for
// scripts and does not have it. Adding it would widen the document that holds
// the session keys for the benefit of a document that no longer needs it, so
// the test follows the code, and the brief is the stale side. See the comment
// over Page::CSP for the measurement that decided it.
const SHELL_CSP = {
  "default-src": "'none'",
  "script-src": "'self'",
  "style-src": "'self'",
  "img-src": "'self' data:",
  "font-src": "'self' data:",
  "media-src": "data:",
  "connect-src": "'self'",
  "frame-src": "'self'",
  "worker-src": "'self'",
  "manifest-src": "'none'",
  "base-uri": "'none'",
  "form-action": "'none'",
  "frame-ancestors": "'none'",
};

// What it sends for an app window (Page::FRAME_CSP). sandbox is in the HEADER
// and not only on the iframe, which is the line that gives the document an
// opaque origin however it is loaded - measured in its own case below.
const FRAME_CSP = {
  sandbox: "allow-scripts",
  "default-src": "'none'",
  "script-src": "data:",
  "style-src": "data: 'unsafe-inline'",
  "img-src": "data:",
  "font-src": "data:",
  "media-src": "data:",
  "connect-src": "'none'",
  "worker-src": "'none'",
  "manifest-src": "'none'",
  "base-uri": "'none'",
  "form-action": "'none'",
  "frame-ancestors": "'self'",
};

// policy reads a Content-Security-Policy header into {directive: value}. A
// header is one string, and a case that only searched it could pass on
// "script-src 'self'" found inside "script-src-elem 'self' 'unsafe-inline'".
function policy(header) {
  const out = Object.create(null);
  for (const part of String(header ?? "").split(";")) {
    const t = part.trim();
    if (t === "") continue;
    const sp = t.indexOf(" ");
    if (sp < 0) { out[t] = ""; continue; }
    out[t.slice(0, sp)] = t.slice(sp + 1).trim();
  }
  return out;
}

// Where the web root is on disk, for the one case that looks at files rather
// than at answers.
const WEBROOT = fileURLToPath(new URL("../../public", import.meta.url));

function filesUnder(dir) {
  const out = [];
  for (const name of readdirSync(dir)) {
    const full = `${dir}/${name}`;
    if (statSync(full).isDirectory()) out.push(...filesUnder(full));
    else out.push(full.slice(WEBROOT.length + 1).replace(/\\/g, "/"));
  }
  return out;
}

// ------------------------------------------------------------- what was sent

test("the shell page carries the whole policy, directive for directive", async ({ page }) => {
  const res = await page.goto("/");
  expect(res.status()).toBe(200);
  const h = res.headers();
  const csp = policy(h["content-security-policy"]);

  for (const [name, value] of Object.entries(SHELL_CSP)) {
    expect(csp[name], name).toBe(value);
  }
  // As a SET, so a directive that was ADDED is a failure too. A policy may be
  // loosened by adding to it - script-src-elem beside script-src, or a
  // default-src that a later directive overrides - and a per-directive check
  // would never see it.
  expect(Object.keys(csp).sort()).toEqual(Object.keys(SHELL_CSP).sort());

  // None of the four words that would undo the rest of it.
  const whole = String(h["content-security-policy"]);
  for (const bad of ["'unsafe-inline'", "'unsafe-eval'", "'unsafe-hashes'", "'strict-dynamic'"]) {
    expect(whole, bad).not.toContain(bad);
  }
  // No source that is a whole scheme or a wildcard: 'self', 'none' and data:
  // are the only three this policy uses, and data: only where no code runs.
  for (const [name, value] of Object.entries(csp)) {
    for (const src of value.split(/\s+/).filter((s) => s !== "")) {
      expect(["'self'", "'none'", "data:"], `${name} ${src}`).toContain(src);
    }
  }

  expect(h["permissions-policy"]).toContain("publickey-credentials-get=(self)");
  expect(h["permissions-policy"]).toContain("camera=()");
  expect(h["permissions-policy"]).toContain("microphone=()");
  expect(h["permissions-policy"]).toContain("geolocation=()");
  expect(h["cross-origin-opener-policy"]).toBe("same-origin");
  expect(h["cross-origin-resource-policy"]).toBe("same-origin");
  expect(h["referrer-policy"]).toBe("no-referrer");
  expect(h["x-content-type-options"]).toBe("nosniff");
  expect(h["cache-control"]).toBe("no-store");
  expect(h["strict-transport-security"]).toContain("max-age=63072000");
  // The server says nothing about itself. Page::headers removes it; a PHP
  // that was configured to add it back would show up here.
  expect(h["x-powered-by"]).toBeUndefined();
});

test("an app window carries a policy of its own, and it is the tighter one", async ({ page }) => {
  const res = await page.goto("/frame.html");
  expect(res.status()).toBe(200);
  const csp = policy(res.headers()["content-security-policy"]);

  for (const [name, value] of Object.entries(FRAME_CSP)) {
    expect(csp[name], name).toBe(value);
  }
  expect(Object.keys(csp).sort()).toEqual(Object.keys(FRAME_CSP).sort());

  // The three that make it a fence and not a second shell: an origin of its
  // own, scripts only out of the bytes the shell wrote in, and no connection
  // of the app's own.
  expect(csp.sandbox).toBe("allow-scripts");
  expect(csp["script-src"]).not.toContain("'self'");
  expect(csp["connect-src"]).toBe("'none'");
  // 'unsafe-inline' for style and for style ALONE. A stylesheet runs nothing,
  // and an app's markup carries style attributes; a script does, and there it
  // is absent.
  expect(csp["style-src"]).toContain("'unsafe-inline'");
  expect(csp["script-src"]).not.toContain("'unsafe-inline'");
});

test("every answer of the one entry point carries the headers", async ({ request }) => {
  // The five shapes of answer index.php can give: the desktop, the
  // configuration, an app window, a path that is not there, and a method that
  // is not allowed. An error page is the one that gets forgotten, and an
  // error page is a document in NGC's own origin like any other.
  const answers = [
    ["GET", "/", 200],
    ["GET", "/config.json", 200],
    ["GET", "/frame.html", 200],
    ["GET", "/gibtesnicht", 404],
    ["POST", "/", 405],
  ];
  for (const [method, path, status] of answers) {
    const res = await request.fetch(path, { method });
    expect(res.status(), `${method} ${path}`).toBe(status);
    const h = res.headers();
    expect(h["content-security-policy"], `${method} ${path} csp`).toContain("default-src 'none'");
    expect(h["x-content-type-options"], `${method} ${path} nosniff`).toBe("nosniff");
    expect(h["referrer-policy"], `${method} ${path} referrer`).toBe("no-referrer");
    expect(h["cross-origin-opener-policy"], `${method} ${path} coop`).toBe("same-origin");
    expect(h["cache-control"], `${method} ${path} cache`).toBe("no-store");
    expect(h["x-powered-by"], `${method} ${path} powered-by`).toBeUndefined();
  }
});

// ------------------------------------------------- what the engine refuses

test("a script from outside does not run, and one of this server's does", async ({ page }) => {
  // The foreign script is SERVED, by a route of this test, so that the only
  // thing standing between it and window.__ran is the policy - not a name
  // that does not resolve and not a network that is not there. The route
  // counts its calls: a request the policy stopped never reaches it, and that
  // count is part of the measurement.
  let foreignHits = 0;
  await page.route("https://fremd.test/**", async (route) => {
    foreignHits++;
    await route.fulfill({
      status: 200, contentType: "text/javascript", body: "window.__ran = 'fremd';",
    });
  });
  // The control: the same mechanism, one origin over. If this one does not
  // run either, the case below says nothing about the policy.
  await page.route("**/assets/js/pruefung.js", async (route) => {
    await route.fulfill({
      status: 200, contentType: "text/javascript", body: "window.__ran = 'eigen';",
    });
  });

  await page.goto("/");
  const own = await page.evaluate(async () => {
    const s = document.createElement("script");
    s.src = "/assets/js/pruefung.js";
    const done = new Promise((r) => { s.onload = () => r("load"); s.onerror = () => r("error"); });
    document.head.append(s);
    return { event: await done, ran: window.__ran ?? null };
  });
  expect(own, "a script of this server must run, or the case below measures nothing")
    .toEqual({ event: "load", ran: "eigen" });

  const foreign = await page.evaluate(async () => {
    window.__ran = null;
    const s = document.createElement("script");
    s.src = "https://fremd.test/x.js";
    const done = new Promise((r) => { s.onload = () => r("load"); s.onerror = () => r("error"); });
    document.head.append(s);
    // A policy that refuses the load fires error; one that let it through
    // fires load. Either way the answer comes, so there is nothing to wait
    // out here.
    return { event: await done, ran: window.__ran ?? null };
  });
  expect(foreign.ran).toBeNull();
  expect(foreign.event).toBe("error");
  expect(foreignHits, "the request never even went out").toBe(0);
});

test("a script written into the page does not run", async ({ page }) => {
  await page.goto("/");
  const ran = await page.evaluate(async () => {
    window.__inline = null;
    const s = document.createElement("script");
    // textContent, the way the whole shell fills an element: the rule against
    // writing markup into a node holds in the tests too.
    s.textContent = "window.__inline = true;";
    document.head.append(s);
    // An inline script runs synchronously when it is appended, so by the next
    // line it has either run or been refused.
    return window.__inline;
  });
  expect(ran, "script-src has no 'unsafe-inline'").toBeNull();
});

test("a base of somebody else's does not move the page's addresses", async ({ page }) => {
  await page.goto("/");
  const got = await page.evaluate(() => {
    const before = document.baseURI;
    const b = document.createElement("base");
    b.setAttribute("href", "https://fremd.test/weg/");
    document.head.prepend(b);
    return { before, after: document.baseURI, resolved: new URL("x", document.baseURI).href };
  });
  // base-uri 'none': the element is there in the document and the browser
  // ignores it. Without the directive every relative address of the page -
  // the module it imports, the stylesheet it loads - would point at
  // fremd.test from this moment on.
  expect(got.after).toBe(got.before);
  expect(got.resolved.startsWith("http://127.0.0.1")).toBe(true);
});

test("a connection to another origin is refused, and one to this one answers", async ({ page }) => {
  let hits = 0;
  await page.route("https://fremd.test/**", async (route) => {
    hits++;
    await route.fulfill({ status: 200, contentType: "text/plain", body: "da" });
  });
  await page.goto("/");
  const got = await page.evaluate(async () => {
    // The control first: connect-src 'self' lets this through, so a "no" below
    // is the origin and not fetch being broken.
    let own = "no";
    try {
      own = (await fetch("/config.json", { cache: "no-store" })).ok ? "yes" : "no";
    } catch { own = "threw"; }
    let foreign = "answered";
    try {
      await fetch("https://fremd.test/x", { cache: "no-store" });
    } catch { foreign = "refused"; }
    return { own, foreign };
  });
  expect(got.own).toBe("yes");
  expect(got.foreign).toBe("refused");
  expect(hits, "the request never even went out").toBe(0);
});

test("a worker from a data: address does not start", async ({ page }) => {
  await page.goto("/");
  const got = await page.evaluate(async () => {
    // The control: a worker of this server's own. worker-src 'self' allows it,
    // and the shell really uses one (auth/argon2-worker.js), so this is the
    // path that has to keep working.
    let own = "no";
    try {
      const w = new Worker("/assets/js/auth/argon2-worker.js", { type: "module" });
      own = "yes";
      w.terminate();
    } catch { own = "threw"; }

    // The attempt. Some engines throw here, some fire an error event, and one
    // refuses data: workers of its own accord - so what is measured is that
    // NOTHING came back from it, not which way it failed.
    let spoke = false;
    try {
      const w = new Worker("data:text/javascript,self.postMessage(1)");
      w.onmessage = () => { spoke = true; };
      await new Promise((r) => setTimeout(r, 300));
      w.terminate();
    } catch { /* refused outright: also no message */ }
    return { own, spoke };
  });
  expect(got.own).toBe("yes");
  expect(got.spoke).toBe(false);
});

// ------------------------------------------- the one exception, and its fence

test("an app window has an origin of its own however it is loaded", async ({ page }) => {
  // Not in a frame: opened as a page of its own. The sandbox is in the HEADER
  // (Page::FRAME_CSP), so the walls do not depend on the iframe attribute of
  // whoever framed it - which is the whole reason shell/frame-boot.js may
  // build a document at all.
  //
  // WHAT THIS CASE IS AND IS NOT. "The frame could not read the storage" is
  // also true of a storage that was empty, and an empty storage is what a
  // fresh context has - so the shell's origin is given something to hold
  // FIRST, and that it really holds it is measured before the frame is asked.
  // Without those two lines the question below has only one possible answer
  // and the case cannot fail.
  await page.goto("/");
  await page.evaluate(() => {
    localStorage.setItem("ngc.probe", "geheim-aus-der-shell");
    document.cookie = "ngc_probe=geheim-aus-der-shell; path=/";
  });
  const atHome = await page.evaluate(() => ({
    storage: localStorage.getItem("ngc.probe"),
    cookie: document.cookie.includes("ngc_probe=geheim-aus-der-shell"),
  }));
  expect(atHome, "the shell's own origin really holds both")
    .toEqual({ storage: "geheim-aus-der-shell", cookie: true });

  await page.goto("/frame.html");
  const got = await page.evaluate(() => {
    // Three readings, and they are not the same thing: "threw" is an engine
    // that refuses the accessor to an opaque origin, "none" is an accessor
    // that answered with nothing, and the value itself is the failure.
    const read = (fn) => { try { const v = fn(); return v === null || v === "" ? "none" : v; } catch { return "threw"; } };
    return {
      origin: String(window.origin),
      storage: read(() => localStorage.getItem("ngc.probe")),
      cookie: read(() => document.cookie),
    };
  });
  expect(got.origin, "an opaque origin answers null here").toBe("null");
  // The one thing that must not happen: this document reading what the shell's
  // origin holds. The session keys live there.
  expect(got.storage, "the frame cannot read the shell's storage")
    .not.toBe("geheim-aus-der-shell");
  expect(got.cookie, "the frame cannot read the shell's cookies")
    .not.toContain("geheim-aus-der-shell");
});

test("the one place that builds a document builds nothing in NGC's own origin", async ({ page }) => {
  // shell/frame-boot.js holds the ONE call of this interface that turns text
  // into a document - the single approved exception to the rule against it.
  // Its first line refuses to do anything where window.origin is not "null",
  // and this is that line: the same file, loaded into the shell's own
  // document, handed exactly the message it waits for.
  //
  // If the guard ever goes, this case goes red in the loudest way there is -
  // the page it was measuring is gone.
  await page.goto("/");
  const got = await page.evaluate(async () => {
    const mark = document.createElement("div");
    mark.id = "ngc-marke";
    document.body.append(mark);

    const s = document.createElement("script");
    s.src = "/assets/js/shell/frame-boot.js";
    const loaded = await new Promise((r) => {
      s.onload = () => r(true);
      s.onerror = () => r(false);
      document.head.append(s);
    });

    // The bootstrap announces itself with a "ready" to its parent. At the top
    // of a tree the parent is this very window, so a bootstrap that had got
    // past its guard would be heard right here.
    const heard = [];
    window.addEventListener("message", (e) => { heard.push(e.data); });
    window.postMessage({ ngc: 1, op: "doc", html: "<h1>fremd</h1>" }, "*");
    await new Promise((r) => setTimeout(r, 200));

    return {
      loaded,
      ready: heard.some((m) => m && m.ngc === 1 && m.op === "ready"),
      marke: document.getElementById("ngc-marke") !== null,
      body: document.body.textContent.includes("fremd"),
    };
  });
  expect(got.loaded, "the file really ran; otherwise this case measures nothing").toBe(true);
  expect(got.ready, "no bootstrap announced itself in NGC's own origin").toBe(false);
  expect(got.marke, "the document is the one this case started with").toBe(true);
  expect(got.body, "nothing of the message reached the page").toBe(false);
});

test("the web root holds nothing a browser would run as a document of this origin", () => {
  // Apache serves everything below public/assets/ itself, with no policy
  // header of ours on it (measured: an asset answer carries Content-Type and
  // nothing else). That is fine for a stylesheet and for a script, which are
  // fetched BY a document whose policy already holds. It is not fine for a
  // file a person can be sent to directly: an .html or an .svg under this
  // origin is a document of NGC's own origin, with scripts of its own and no
  // policy at all - and an .svg is the quiet one, because it looks like a
  // picture until somebody opens it.
  //
  // So the web root carries exactly one file a web server would do anything
  // but hand over: the single entry point. The way to add a picture is a data:
  // address out of the proven ui-tar, or an inline <svg> built with the DOM,
  // which is what the shell already does.
  const files = filesUnder(WEBROOT);
  expect(files.length, "the web root was really walked").toBeGreaterThan(10);
  expect(files, "the single entry point").toContain("index.php");

  const runnable = files.filter((f) =>
    /\.(php|phtml|php[0-9]|phar|html?|xhtml|xml|svg|shtml)$/i.test(f) || /(^|\/)\.ht/.test(f));
  expect(runnable).toEqual(["index.php"]);
});

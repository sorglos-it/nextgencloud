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

// What this host says about itself. ONE constant, used by the page's case and
// by the files' case below: the two have to say the same thing about the host,
// and two literals would let them drift apart without a case noticing.
const HSTS = "max-age=63072000; includeSubDomains";

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
  expect(h["strict-transport-security"]).toBe(HSTS);
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

// ---------------------------------------- and the files, which are answers too

// What the virtual host sends for a file below assets/
// (templates/apache-vhost.conf). Written out here, in full, for the same
// reason SHELL_CSP is: a change to it has to be made twice, and the second time
// with a test run behind it.
//
// IT IS NOT Page::CSP, and the reason is in the template beside the lines
// themselves. Page::CSP says style-src, img-src, font-src, connect-src,
// frame-src and worker-src 'self' because the SHELL DOCUMENT loads those
// things; a file loads nothing, so on a file they are decoration - and a header
// that feigns a wall is worse than no header.
//
// Two are not decoration, and both are about the one file below assets/ whose
// own answer's policy is live: assets/js/auth/argon2-worker.js, the worker that
// holds the typed password.
//   script-src 'self' - it has a static import, and without this it does not
//     run at all on WebKit.
//   no connect-src AT ALL - so default-src 'none' answers for it and there is
//     no way out of that worker. "connect-src 'self'", which Page::CSP has,
//     would be a way.
// Both measured, in the case further down.
const ASSET_CSP = { "default-src": "'none'", "script-src": "'self'" };

// And the four the virtual host leaves out ON PURPOSE, each with a reason
// written beside the block there. They are listed here because a list of
// reasons is not a fence: without this, somebody writing Referrer-Policy back
// in "to be safe" - the exact reflex that section argues against - would find
// the whole suite still green, and the thesis of this task is that a pointless
// header is worse than none.
const ASSET_NOT = [
  "referrer-policy",
  "cross-origin-opener-policy",
  "permissions-policy",
  "cache-control",
];

test("every file the web server hands out itself carries walls too", async ({ request }) => {
  // THE POINT OF THIS CASE. public/index.php says it: Apache "serves
  // everything below assets/ itself", so none of these answers ever reaches
  // Page::headers. Every one of them is still an answer of NGC's origin.
  //
  // The three shapes a file can have, and each is here for its own reason:
  //   a .js   - the shell's code, and the kind of file a worker is made of
  //   a .css  - a different Content-Type, in case a rule was written per type
  //   LICENSE - NO extension, so the server sends no Content-Type at all. That
  //             is the file nosniff is in that block for, and the loop below
  //             measures that it really has no type.
  const files = [
    "/assets/js/shell/dom.js",
    "/assets/css/shell.css",
    "/assets/vendor/noble-hashes/LICENSE",
  ];
  for (const path of files) {
    const res = await request.fetch(path);
    expect(res.status(), path).toBe(200);
    const h = res.headers();
    const csp = policy(h["content-security-policy"]);
    for (const [name, value] of Object.entries(ASSET_CSP)) {
      expect(csp[name], `${path} ${name}`).toBe(value);
    }
    // As a SET again: a directive that was ADDED here is a failure too, and
    // the one that must never turn up is connect-src.
    expect(Object.keys(csp).sort(), `${path} directives`).toEqual(Object.keys(ASSET_CSP).sort());
    expect(h["x-content-type-options"], `${path} nosniff`).toBe("nosniff");
    expect(h["cross-origin-resource-policy"], `${path} corp`).toBe("same-origin");
    expect(h["strict-transport-security"], `${path} hsts`).toBe(HSTS);
    expect(h["x-powered-by"], `${path} powered-by`).toBeUndefined();
    // And none of the four the block leaves out. This is the other half of
    // "the policy as a set": the directives are held as a set above, the
    // HEADERS are held as one here.
    for (const name of ASSET_NOT) {
      expect(h[name], `${path} must not carry ${name}`).toBeUndefined();
    }
    // And the file with no extension really has no type, otherwise the reason
    // nosniff is in that block is a case that does not exist.
    if (path.endsWith("/LICENSE")) {
      expect(h["content-type"], "a file with no extension gets no type").toBeUndefined();
    }
  }
});

test("a file that is not there below /assets/ is the web server's own answer, with all four walls", async ({ request }) => {
  // THE CASE THIS WHOLE BLOCK EXISTS FOR, and until 03.10.2026 it was the one
  // answer that carried a wall LESS than the others.
  //
  // What it used to be: the virtual host routes every path that is no file to
  // index.php, and that directive is INHERITED by assets/ - so a missing .js
  // there was written by PHP. Page::headers then sent its own
  // Cross-Origin-Resource-Policy while the assets block set a second one, and
  // two of that header are neither same-origin nor same-site, so a browser
  // applies NEITHER. The fix is in the virtual host and not in a patch
  // afterwards: "FallbackResource disabled" in the assets block, so Apache
  // answers that path itself and no header of PHP's is there to collide.
  //
  // So this case is now about WHO ANSWERED, and it is held to the strict
  // version: the asset policy exactly, as a set, and NONE of the headers only
  // Page::headers sends. If the line is ever taken out of the virtual host,
  // apps/web/tests/mock/api.js refuses to start at all - and if the harness
  // were made to answer with PHP's headers instead, the loop at the end of
  // this case falls.
  const res = await request.fetch("/assets/js/gibtesnicht.js");
  expect(res.status()).toBe(404);
  const h = res.headers();
  const csp = policy(h["content-security-policy"]);
  for (const [name, value] of Object.entries(ASSET_CSP)) {
    expect(csp[name], name).toBe(value);
  }
  expect(Object.keys(csp).sort(), "directives").toEqual(Object.keys(ASSET_CSP).sort());
  expect(h["x-content-type-options"]).toBe("nosniff");
  expect(h["cross-origin-resource-policy"]).toBe("same-origin");
  expect(h["strict-transport-security"]).toBe(HSTS);
  expect(h["x-powered-by"]).toBeUndefined();
  // The half that says PHP did NOT write this answer. Page::headers sends
  // Cross-Origin-Opener-Policy, Referrer-Policy and Permissions-Policy on
  // every page it writes; the assets block sends none of them, on purpose and
  // with a reason beside each. So one of these turning up here means the
  // fallback reached index.php after all, and then this answer carries two
  // resource policies and so none.
  for (const name of ASSET_NOT) {
    expect(h[name], `a missing asset must not carry ${name}, that would be PHP's answer`).toBeUndefined();
  }
});

test("the policy on a file does the one thing a policy on a file can do", async ({ page, request }) => {
  // THE CASE THAT DECIDES ASSET_CSP, and the only one in this file about a
  // header on a FILE rather than on a document.
  //
  // A worker is the one file below assets/ whose own answer's policy governs
  // anything: it is the policy of the worker's global scope.
  // assets/js/auth/argon2-worker.js is that file, and it is the one that holds
  // the typed password while it derives the key. So the two directives are
  // measured there, one half each.
  //
  // MEASURED FIRST, on the real file, with no stand-in: it still works. A
  // worker is asked something and not merely constructed - new Worker()
  // resolves before the module graph is loaded, so a worker whose import was
  // refused still looks like a worker until it is spoken to.
  //
  // This half is what script-src 'self' is in the policy FOR. Taking it out of
  // templates/apache-vhost.conf was measured: WebKit then refuses the worker's
  // static import outright (onerror, no answer at all), so no password login
  // works in Safari. Chromium is the quiet one - it lets the static import
  // through and refuses only a dynamic import() - which is why this is the
  // kind of header that has to be measured on more than one engine.
  await page.goto("/");
  const real = await page.evaluate(async () => {
    const w = new Worker("/assets/js/auth/argon2-worker.js", { type: "module" });
    const answer = new Promise((done) => {
      w.onmessage = (e) => done({ seed: Array.from(e.data).length });
      w.onerror = (e) => done({ error: String(e.message ?? "error") });
      // A refused import does not always arrive as an error event; on some
      // engines the worker simply never answers. A wait of its own turns that
      // into a sentence instead of a run that times out.
      setTimeout(() => done({ silent: true }), 10000);
    });
    // The smallest job the contract's shape allows: this measures that the
    // worker computes AT ALL. What it computes is held against the vectors in
    // contracts/proof.spec.js.
    w.postMessage({
      password: new Uint8Array([0x61]), salt: new Uint8Array(16),
      t: 1, m: 256, p: 1, dkLen: 32,
    });
    const out = await answer;
    w.terminate();
    return out;
  });
  expect(real, "the worker answered, so its own policy let the import through")
    .toEqual({ seed: 32 });

  // AND THE OTHER HALF: that there is no way out of such a worker. connect-src
  // is absent from the policy, so default-src 'none' answers for it, and that
  // is the wall that matters here - the file holds the plaintext password.
  //
  // The real worker cannot be asked this, because it does not try: it computes
  // and answers, and that is all it does. So the attempt is made by a worker of
  // this case's own - carrying THE POLICY THE SERVER REALLY SENT for the real
  // one, read off that answer just below, so no value is written twice. What is
  // a stand-in here is who served the file; what is measured is the engine.
  const sent = (await request.fetch("/assets/js/auth/argon2-worker.js")).headers();
  const asSent = sent["content-security-policy"];
  expect(asSent, "the real worker's answer carries the policy this measures").toBeTruthy();

  // A name that is no file of this interface, so nothing but this route can
  // answer it - and under /assets/, because that is where the policy this case
  // is about belongs.
  await page.route("**/assets/js/pruefwerker.js", async (route) => {
    await route.fulfill({
      status: 200,
      headers: { "content-type": "text/javascript", "content-security-policy": asSent },
      body: `
        import { utf8 } from "/assets/js/contracts/b64.js";
        self.onmessage = async () => {
          let net = "answered";
          try { await fetch("/config.json", { cache: "no-store" }); } catch { net = "refused"; }
          self.postMessage({ imported: typeof utf8, net });
        };`,
    });
  });

  const probe = await page.evaluate(async () => {
    const w = new Worker("/assets/js/pruefwerker.js", { type: "module" });
    const answer = new Promise((done) => {
      w.onmessage = (e) => done(e.data);
      w.onerror = (e) => done({ error: String(e.message ?? "error") });
      setTimeout(() => done({ silent: true }), 10000);
    });
    w.postMessage("los");
    const out = await answer;
    w.terminate();
    return out;
  });
  // The control is in the same answer: the import went through, so "refused"
  // below is the policy and not a worker that never ran.
  expect(probe.imported, "the stand-in worker ran and imported").toBe("function");
  expect(probe.net, "no connection out of a worker: connect-src falls back to default-src 'none'")
    .toBe("refused");
});

test("another origin may not pull the shell's files into itself", async ({ page, baseURL }) => {
  // Cross-Origin-Resource-Policy, measured as a behaviour and not as a string.
  //
  // The foreign page AND its own script are served by routes of this test, so
  // the only thing between the page and NGC's file is the header: not a name
  // that does not resolve, and not a network that is not there. The foreign
  // page carries no policy of its own, so nothing on its side forbids the load
  // either.
  //
  // WHAT IT IS WORTH ON WHICH ENGINE, measured by taking the header out of
  // templates/apache-vhost.conf: Chromium then loads the file and this case
  // goes red, so there it measures the header. WebKit refuses the load even
  // without it, for a reason of its own - so there this case passes either way
  // and says nothing about the header. It runs on both anyway: on Chromium it
  // is the measurement, on WebKit it is a second pair of eyes on the same
  // load. Stated here so nobody reads the WebKit line as proof of anything.
  await page.route("https://fremd.test/**", async (route) => {
    if (route.request().url().endsWith("/eigen.js")) {
      await route.fulfill({ status: 200, contentType: "text/javascript", body: "window.__eigen = 1;" });
      return;
    }
    await route.fulfill({
      status: 200, contentType: "text/html", body: "<!DOCTYPE html><title>fremd</title>",
    });
  });
  await page.goto("https://fremd.test/");
  expect(await page.evaluate(() => location.origin), "the page really is somewhere else")
    .toBe("https://fremd.test");

  const got = await page.evaluate(async (ngc) => {
    const load = (src) => new Promise((done) => {
      const s = document.createElement("script");
      s.src = src;
      s.onload = () => done("load");
      s.onerror = () => done("error");
      document.head.append(s);
    });
    // The control first, and it is the SAME mechanism one origin over: a
    // script this page is allowed to have. If it does not load either, the
    // line below says nothing about the resource policy.
    const own = await load("https://fremd.test/eigen.js");
    return { own, ran: window.__eigen ?? null, ngc: await load(`${ngc}/assets/js/shell/dom.js`) };
  }, baseURL);
  expect(got.own).toBe("load");
  expect(got.ran, "a script this page may have really ran").toBe(1);
  expect(got.ngc, "Cross-Origin-Resource-Policy: same-origin").toBe("error");
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
  // Apache serves everything below public/assets/ itself, so Page::headers
  // never sees those answers. Since task 29 they are not bare: the virtual
  // host (templates/apache-vhost.conf) puts nosniff, the resource policy and a
  // policy of their own on them, and the cases above measure it.
  //
  // That policy is written for a FILE, though, and this case is the reason it
  // may be: a file a person can be sent to directly is something else. An
  // .html or an .svg under this origin is a document of NGC's own origin, with
  // scripts of its own - and an .svg is the quiet one, because it looks like a
  // picture until somebody opens it. Such a file would need the walls of a
  // document, which is what the one entry point sends and this block does not.
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

  // AND THE SECOND HALF, which this case did not hold until the walls for the
  // files existed. The virtual host puts them on public/assets/ and on nothing
  // else, because the block cannot be widened to public/ without touching the
  // PAGE's own headers. So a file lying DIRECTLY in public/ - a favicon.ico, a
  // robots.txt, a probe.js - would go out under Apache with none of the four,
  // and no answer could show it: the test server hands only ^/assets/… over
  // raw (index.php, "return false"), so right here it is stricter than
  // operation and a file in public/ gets a walled 404 instead.
  //
  // The cheap bolt, and it holds for every file that will ever be added:
  // directly in public/ there is the entry point and nothing besides. Whatever
  // comes next therefore lands under assets/ and gets the walls.
  const loose = files.filter((f) => !f.includes("/"));
  expect(loose, "directly in the web root there is only the entry point").toEqual(["index.php"]);
});

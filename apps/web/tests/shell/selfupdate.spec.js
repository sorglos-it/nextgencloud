import { test, expect } from "../fixtures/page.js";

// The surfaces pull their own new state: no Ctrl+F5, and no question per
// second either.
//
// WHAT THIS MEASURES AND WHY IT LOOKS LIKE THIS. The three screens a person
// walked into stale on 19.-21.10.2026 - the start menu after an app was set up
// and a right granted, the Rechner list of the store after a server was taken
// in, the Server panel of "Benutzer und Rechte" - are built from answers of the
// API. The session link carries NO message that says any of them moved
// (wire.controlAllowed knows "trust" and "notify" and nothing else), so what
// is measured here is not a push: it is that whoever CHANGED something says so
// on the shell's own bus, and that every surface that shows it asks again.
//
// The cases build the surfaces with a stub link that COUNTS and whose answer
// CHANGES between two calls. A case that only counted would pass against a
// surface that asks again and draws the old answer; a case that only read the
// screen would pass against one that never asked and got lucky. Both halves
// are measured in the same case.
//
// Everything runs in the page, because that is where the modules run: the
// look-again hangs on document and window events, and a surface draws into a
// real DOM.
test("a newly allowed app reaches the start menu without a reload", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const { startDesktop } = await import("/assets/js/shell/desktop.js");
    const { utf8 } = await import("/assets/js/contracts/b64.js");
    const app = (id, name) => ({
      id, name, version: "1.0.0", icon: "icon.svg", entry: "index.html",
      width: 720, height: 480, actions: ["shell"], state: "ok", node: "ctl-1",
    });
    let asked = 0;
    const link = {
      async call() {
        asked++;
        // The SECOND answer carries the second app. A surface that asked
        // again and drew the first answer would be caught by the name below;
        // one that never asked would be caught by the count.
        const list = asked === 1
          ? [app("org.sorglos.terminal", "Terminal")]
          : [app("org.sorglos.terminal", "Terminal"), app("de.example.fotos", "Fotos")];
        return utf8(JSON.stringify(list));
      },
      onNotice() {},
    };
    // A box of this case's OWN and not #root: shell/main.js boots the real
    // desktop into #root while this case runs, and a case whose screen another
    // desktop may clear measures that race and not the bus (measured: the same
    // case passed in 0.5 s and failed in 2.8 s). Every query below is scoped to
    // this box, so the ids of the two desktops cannot be confused either.
    const box = document.createElement("div");
    document.body.append(box);
    const d = startDesktop(box, { link, trust: null });
    const names = () => [...box.querySelectorAll("#startmenu .n")].map((e) => e.textContent);
    const wait = async (n) => {
      for (let i = 0; i < 200 && names().length !== n; i++) await new Promise((r) => setTimeout(r, 10));
      return names();
    };
    const before = await wait(1);
    const askedBefore = asked;
    // The store says "an app was set up"; nothing reloads the page.
    d.changed.tell("apps");
    const after = await wait(2);
    return { before, after, askedBefore, asked };
  });
  expect(got.before).toEqual(["Terminal"]);
  expect(got.askedBefore).toBe(1);
  // Asked again, and what came back is what stands there.
  expect(got.asked).toBe(2);
  expect(got.after).toEqual(["Terminal", "Fotos"]);
});

test("a right that was granted makes the desktop ask for the apps again", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const { startDesktop } = await import("/assets/js/shell/desktop.js");
    const { utf8 } = await import("/assets/js/contracts/b64.js");
    let asked = 0;
    const link = { async call() { asked++; return utf8("[]"); }, onNotice() {} };
    const box = document.createElement("div");
    document.body.append(box);
    const d = startDesktop(box, { link, trust: null });
    const settle = () => new Promise((r) => setTimeout(r, 60));
    await settle();
    const first = asked;
    d.changed.tell("rights");
    await settle();
    const second = asked;
    // A topic no surface of the desktop is built from must NOT cost a
    // question. Without this line the case would pass against a bus that
    // reloads everything on everything, which is the "ask per second" this
    // task was written against.
    d.changed.tell("users");
    await settle();
    return { first, second, third: asked };
  });
  expect(got.first).toBe(1);
  expect(got.second).toBe(2);
  expect(got.third).toBe(2);
});

test("a new version of the trust list reaches every surface, once", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const { startDesktop } = await import("/assets/js/shell/desktop.js");
    const { utf8 } = await import("/assets/js/contracts/b64.js");
    let tell = null;
    const trust = { current: () => null, onChange: (fn) => { tell = fn; } };
    const link = { async call() { return utf8("[]"); }, onNotice() {} };
    const box = document.createElement("div");
    document.body.append(box);
    const d = startDesktop(box, { link, trust });
    const seen = [];
    for (const t of ["apps", "rights", "nodes", "users"]) d.changed.on(t, (x) => seen.push(x));
    if (tell === null) return { wired: false, seen };
    tell({ list: { seq: 2 } });
    return { wired: true, seen };
  });
  // The list carries the nodes, the stores and the root rights; which of them
  // moved cannot be read from the outside, so all four are told - and each of
  // them exactly once per settled version.
  expect(got.wired).toBe(true);
  expect(got.seen.sort()).toEqual(["apps", "nodes", "rights", "users"]);
});

test("the store draws its open tab again from the new answer, and drops what it cached", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const { storeApp } = await import("/assets/js/apps/store.js");
    const { Changed } = await import("/assets/js/shell/changed.js");
    const core = await import("/assets/js/contracts/core.js");
    const { utf8 } = await import("/assets/js/contracts/b64.js");
    const row = (id) => ({
      id, version: "1.0.0", store: "global", node: "ctl-1",
      active: true, state: "ok", license: "valid",
    });
    let asked = 0;
    const body = { __proto__: null };
    body[core.ACT_SOURCES] = () => [];
    body[core.ACT_UPDATES] = () => [];
    // The app that was just set up is in the SECOND answer and in no earlier
    // one: a surface that asked again and drew what it had cached would be
    // caught by the names below, one that never asked by the count.
    body[core.ACT_INSTALLED] = () => (++asked === 1
      ? [row("org.sorglos.terminal")]
      : [row("de.example.fotos"), row("org.sorglos.terminal")]);
    const link = {
      async call(app, service, name) {
        const fn = body[name];
        if (fn === undefined) throw new Error("no answer for " + name);
        return utf8(JSON.stringify(fn()));
      },
    };
    const changed = new Changed();
    const box = document.createElement("div");
    document.body.append(box);
    const win = { id: "w1", body: box };
    const stop = storeApp(win, { link, changed, trust: { current: () => null } });
    const settle = () => new Promise((r) => setTimeout(r, 90));
    await settle();
    // The tab is opened the way a person opens it, so what is measured is the
    // screen and not a method.
    const tabs = [...box.querySelectorAll(".store .tab")];
    tabs.find((b) => b.textContent === "Installiert")?.click();
    await settle();
    const before = [...box.querySelectorAll(".store .rowmain b")].map((e) => e.textContent);
    const first = asked;
    // What taking a server in raises, and what a new version of the trust
    // list raises through the desktop.
    changed.tell("nodes");
    await settle();
    const after = [...box.querySelectorAll(".store .rowmain b")].map((e) => e.textContent);
    stop();
    return { before, after, first, asked };
  });
  expect(got.before).toEqual(["org.sorglos.terminal"]);
  expect(got.first).toBe(1);
  // Asked again, and what came back is what stands there. The same cache
  // holds the Rechner list (core.ActNodes), which is why the next Aktivieren
  // box offers a server that was taken in while this window stood open.
  expect(got.asked).toBe(2);
  expect(got.after).toEqual(["de.example.fotos", "org.sorglos.terminal"]);
});

test("the Server panel of Benutzer und Rechte is built again when the list moved", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const { usersApp } = await import("/assets/js/apps/users.js");
    const { Changed } = await import("/assets/js/shell/changed.js");
    const core = await import("/assets/js/contracts/core.js");
    const { utf8 } = await import("/assets/js/contracts/b64.js");
    let pending = 0;
    const link = {
      async call(app, service, name) {
        if (name === core.ACT_PENDING) pending++;
        // Every panel of this surface says for itself how far it got, so an
        // answer it has no stub for is a sentence on the screen and not a
        // broken case. What is counted is the one action this case is about.
        if (name === core.ACT_PENDING || name === core.ACT_PENDING_API) return utf8("[]");
        throw new Error("no answer for " + name);
      },
    };
    const changed = new Changed();
    const box = document.createElement("div");
    document.body.append(box);
    const win = { id: "w1", body: box };
    const stop = usersApp(win, { link, changed, trust: { current: () => null }, session: null });
    const settle = () => new Promise((r) => setTimeout(r, 80));
    const tabs = [...box.querySelectorAll(".usersapp .tab")];
    tabs.find((b) => b.textContent === "Server")?.click();
    await settle();
    const first = pending;
    changed.tell("nodes");
    await settle();
    const second = pending;
    // A panel that was never opened is not asked for. The Protokoll tab was
    // not clicked, so a bus that rebuilt everything would be seen here as a
    // question nobody needed.
    changed.tell("users");
    await settle();
    stop();
    return { first, second, third: pending };
  });
  expect(got.first).toBe(1);
  expect(got.second).toBe(2);
  expect(got.third).toBe(2);
});

test("a screen that can be old says since when it stands, and asks again on the button", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const { freshness, ago } = await import("/assets/js/shell/stale.js");
    let asked = 0;
    const f = freshness(() => { asked++; });
    document.body.append(f.el);
    const before = f.el.querySelector(".standtxt").textContent;
    f.done();
    const after = f.el.querySelector(".standtxt").textContent;
    f.el.querySelector(".standgo").click();
    const words = [ago(0), ago(44000), ago(46000), ago(3 * 60 * 1000), ago(2 * 3600 * 1000),
      ago(3 * 86400 * 1000)];
    f.stop();
    return { before, after, asked, words };
  });
  // Before the first answer it says it is being fetched - not a time it does
  // not have.
  expect(got.before).toBe("Stand: wird geholt …");
  expect(got.after).toBe("Stand: gerade eben");
  expect(got.asked).toBe(1);
  expect(got.words).toEqual(["gerade eben", "gerade eben", "vor 1 Minute", "vor 3 Minuten",
    "vor 2 Stunden", "vor 3 Tagen"]);
});

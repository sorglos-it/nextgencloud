import { readFileSync } from "node:fs";
import { test, expect } from "@playwright/test";
import { signIn, skipWithoutLogin } from "../fixtures/page.js";
import { buildPackage, manifest } from "../fixtures/pkg.js";
import { vectors } from "../fixtures/vectors.js";

// The app store: the first app anybody opens, and the one that decides what
// runs on this installation afterwards.
//
// What every case here is really about is the same sentence: WHATEVER THE
// SHELL ACTS ON, IT CHECKED ITSELF. A catalog counts once its store's key out
// of the verified trust list has signed it; an upload counts once the shell's
// own arithmetic over the file matches what came back; a task carries the
// numbers the shell computed and no others; a standing approval carries the
// key of the list and never the one the answer suggested.
//
// The bytes come from shell/fake-link.js, the stand-in that goes with Task 17.
// It answers with REAL bytes that go through core.parseResult and
// catalog.verifyCatalog inside the shell, so what is measured here is the
// shell's own path; only where the bytes come from changes later. Its knobs
// live on window.__ngcStore and what the shell sent on window.__ngcSent.
//
// A DIALOG IS A FAILURE: alert, confirm and prompt are what a name out of a
// catalog would reach for if it ever became markup.
async function store(page, world = {}) {
  page.on("dialog", (d) => {
    throw new Error("something in the page opened a " + d.type() + ": " + d.message());
  });
  await page.addInitScript((w) => { window.__ngcStore = w; }, world);
  await page.goto("/");
  await skipWithoutLogin(page);
  await signIn(page);
  await expect(page.locator("#taskbar")).toBeVisible();
  await page.locator("#icons").getByRole("button", { name: "App-Store", exact: true }).click();
  await expect(page.locator(".store")).toBeVisible();
}

const tab = (page, name) => page.getByRole("tab", { name, exact: true });

// sent reads back what the shell handed the link, with the contracts of the
// page and not with a parser of the test's own: a task is read by
// sign.parseEnvelope and task.parseTask, exactly as every verifier reads it.
async function sentTask(page, name) {
  return page.evaluate(async (action) => {
    const b64 = await import("/assets/js/contracts/b64.js");
    const sign = await import("/assets/js/contracts/sign.js");
    const task = await import("/assets/js/contracts/task.js");
    const rec = window.__ngcSent.find((s) => s.name === action);
    if (!rec || !rec.task) return null;
    const env = sign.parseEnvelope(b64.utf8(rec.task), task.MAX_TASK);
    const t = task.parseTask(env.payload);
    return {
      app: t.app, service: t.service, action: t.action, node: t.node,
      // The one JSON.parse in this tree, and it is not input from outside:
      // these bytes came out of the shell's own task, through
      // sign.parseEnvelope and task.parseTask, and nothing is done with them
      // but comparing. core.parseParams would be the contract's own reader,
      // and it is not exported - the shell writes parameters and reads none.
      params: JSON.parse(b64.fromUtf8(t.params)),
      hasSignature: env.sig.length === 64 && env.kid.length === 22,
    };
  }, name);
}

const names = (page) => page.evaluate(() => window.__ngcSent.map((s) => s.app + "/" + s.name));

// answerConfirm types the password into the question and says yes.
//
// The password is READ OUT OF THE WORLD and not written here twice: where the
// world derives the admin's key from it (knob passwordKey), a password that
// had drifted apart from it would make the signature fail and the case would
// blame the shell for the fixture.
async function answerConfirm(page) {
  const pw = await page.evaluate(async () => (await import("/assets/js/shell/fake-link.js")).PASSWORD);
  const box = page.getByRole("dialog", { name: "Bitte bestätigen" });
  await expect(box).toBeVisible();
  await box.locator("input[type=password]").fill(pw);
  await box.getByRole("button", { name: "Bestätigen", exact: true }).click();
  await expect(box).toHaveCount(0);
}

// ---- a real chain, for the one case that measures the real TrustStore -----
//
// The same four-version chain trustlist.spec.js measures the store against:
// shared/testdata/trust.json is the common truth of Go and JavaScript, and it
// is served over a route as the answer of GET /api/v1/trust. The mock API
// cannot stand in for it - it builds its world with keys it makes at start-up,
// so no fixed anchor ever matches it.
const CHAIN = "admin adds a user, user adds an own key, passkey admin re-signs";

function chainCase() {
  const c = vectors("trust.json").find((x) => x.name === CHAIN);
  if (c === undefined) throw new Error(`store.spec: trust.json has no case "${CHAIN}"`);
  return c;
}

// Does this engine's Web Crypto know Ed25519? Every key in the chain is one,
// so an engine without it would measure the engine and not the shell. Probed
// with a real key out of the fixture, never decided by the engine's name.
async function hasEd25519(page) {
  return page.evaluate(async (c) => {
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const payload = JSON.parse(new TextDecoder().decode(b64decode(c.chain[0].payload)));
    let pub = null;
    for (const u of payload.users) {
      for (const k of u.keys) if (k.kind === "password") pub = k.pub;
    }
    try {
      await crypto.subtle.importKey("raw", b64decode(pub), { name: "Ed25519" }, false, ["verify"]);
      return true;
    } catch {
      return false;
    }
  }, chainCase());
}

// serveChain answers GET /api/v1/trust?from=N out of the fixture chain, the
// way shared/README.md ("Vor dem WebSocket") says the API does. `visible` is
// how many versions this API owns up to; __ngcVisible() raises it to all of
// them, which is what a new version looks like from outside.
async function serveChain(page, c, { visible = c.chain.length } = {}) {
  const state = { visible, from: [] };
  await page.route(/\/api\/v1\/trust\?/, async (route) => {
    const from = Number(new URL(route.request().url()).searchParams.get("from"));
    state.from.push(from);
    const at = from - c.anchor.seq;
    if (at < 0 || at > state.visible) {
      await route.fulfill({
        status: 404, contentType: "application/json; charset=utf-8",
        body: JSON.stringify({ status: "error", code: "E_NOTFOUND", message: "Nicht gefunden." }),
      });
      return;
    }
    if (at === state.visible) { await route.fulfill({ status: 204 }); return; }
    const versions = c.chain.slice(at, state.visible);
    await route.fulfill({
      status: 200, contentType: "application/json; charset=utf-8",
      body: JSON.stringify({ versions, more: false }),
    });
  });
  await page.exposeFunction("__ngcVisible", () => { state.visible = c.chain.length; });
  await page.exposeFunction("__ngcAsked", () => state.from.slice());
  await page.exposeFunction("__ngcForget", () => { state.from = []; });
  return state;
}

// ---------------------------------------------------------------- 1. catalog

test("the Apps tab shows an app only out of a catalog its store really signed", async ({ page }) => {
  await store(page);
  await tab(page, "Apps").click();
  // Four since task 25: the Rechner stands in the store's catalog like the
  // Terminal, because both are apps a person activates and both are checked
  // against a checksum that catalog signed.
  await expect(page.locator(".approw")).toHaveCount(4);
  await expect(page.locator(".approw").first()).toContainText("Notizen");
  await expect(page.locator(".storepanel")).not.toContainText("Der Katalog dieses Stores ist nicht echt.");
});

// The two sentences that must not be the same one. Point 65 of V1.2 is the
// measurement behind this pair: on a freshly installed plant the catalog was
// REAL (sign.Verify -> nil, the same key id on both sides) and the store still
// said "nicht echt", because the row the hourly round writes was not there yet
// and the empty case fell into the else branch meant for a forgery. A person
// who reads "nicht echt" has nothing to do; a person who reads "noch nicht
// geholt" has: wait, or look at Quellen.
//
// Both halves are measured HERE, one after the other in one run, because
// either alone proves nothing: a case that only shows the new sentence would
// also pass if the shell said it for a forged catalog too, and a case that
// only shows the old one would also pass if nothing had changed at all.
const NOT_FETCHED = "Der Katalog dieses Stores ist noch nicht geholt.";
const NOT_REAL = "Der Katalog dieses Stores ist nicht echt.";

test("an unfetched catalog says so, and a forged one still says it is not real", async ({ page }) => {
  // 1. no row yet: the API answers E_NOTFOUND (appstore.CodeFor over
  //    dbapi.ErrNotFound) and the store says what there is to do about it.
  await store(page, { noCatalog: true });
  await tab(page, "Apps").click();
  await expect(page.locator(".storepanel")).toContainText(NOT_FETCHED);
  await expect(page.locator(".storepanel")).not.toContainText(NOT_REAL);
  await expect(page.locator(".approw")).toHaveCount(0);

  // 2. the same shell, the same tab, a catalog signed by a key that is not
  //    the store's: the old sentence, unchanged. The else branch keeps what
  //    it names.
  await store(page, { foreignCatalog: true });
  await tab(page, "Apps").click();
  await expect(page.locator(".storepanel")).toContainText(NOT_REAL);
  await expect(page.locator(".storepanel")).not.toContainText(NOT_FETCHED);
  await expect(page.locator(".approw")).toHaveCount(0);
});

test("a catalog under a foreign signature shows the sentence and not one app", async ({ page }) => {
  // The store's entry in the trust list stays as it is; only the key that
  // signed the catalog is another one. That is the whole attack: an answer
  // from the API that looks like a catalog and was not signed by the store.
  await store(page, { foreignCatalog: true });
  await tab(page, "Apps").click();
  await expect(page.locator(".storepanel")).toContainText("Der Katalog dieses Stores ist nicht echt.");
  await expect(page.locator(".approw")).toHaveCount(0);
});

// ------------------------------------------------------------- 2. upload

// A package the shell has to weigh itself: manifest.json and one ui file.
const PKG = buildPackage(manifest({}));

test("the upload shows the checksum, the version and the UI checksum the shell computed", async ({ page }) => {
  await store(page);
  await tab(page, "Hochladen").click();
  await page.locator(".store input[type=file]").setInputFiles({
    name: "notizen.ngc", mimeType: "application/octet-stream", buffer: Buffer.from(PKG),
  });
  await expect(page.locator(".upfacts")).toBeVisible();
  // The three numbers on screen are the ones pack.packageUI computed in this
  // page out of these very bytes - computed a second time here, out of the
  // same file, and compared.
  const mine = await page.evaluate(async (bytes) => {
    const pack = await import("/assets/js/contracts/pack.js");
    const b64 = await import("/assets/js/contracts/b64.js");
    const u = await pack.packageUI(new Blob([new Uint8Array(bytes)]).stream());
    return {
      id: u.manifest.id, version: u.manifest.version,
      sha256: b64.b64encode(u.packageSha256), ui: b64.b64encode(u.sha256),
    };
  }, Array.from(PKG));
  await expect(page.locator(".upfacts")).toContainText(mine.version);
  await expect(page.locator(".upfacts")).toContainText(mine.sha256);
  await expect(page.locator(".upfacts")).toContainText(mine.ui);
  await expect(page.locator(".store").getByRole("button", { name: "Aktivieren" })).toBeVisible();
});

test("an answer that names another package breaks the upload off and activates nothing", async ({ page }) => {
  // The API answers with a checksum of its own. Nothing else is changed, so
  // what refuses here is the comparison and not the reader.
  await store(page, { uploadTwist: { sha256: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAM" } });
  await tab(page, "Hochladen").click();
  await page.locator(".store input[type=file]").setInputFiles({
    name: "notizen.ngc", mimeType: "application/octet-stream", buffer: Buffer.from(PKG),
  });
  await expect(page.locator(".upfail"))
    .toHaveText("Die Anlage hat ein anderes Paket bekommen, als geschickt wurde.");
  await expect(page.locator(".store").getByRole("button", { name: "Aktivieren" })).toHaveCount(0);
  expect(await names(page)).not.toContain("ngc.store/activate");
});

// ------------------------------------------------------- 3. activate signs

test("the activate task carries the shell's own checksums and the manifest's needs", async ({ page }) => {
  await store(page);
  await tab(page, "Hochladen").click();
  const pkg = buildPackage(manifest({ needs: ["network"] }));
  await page.locator(".store input[type=file]").setInputFiles({
    name: "notizen.ngc", mimeType: "application/octet-stream", buffer: Buffer.from(pkg),
  });
  await expect(page.locator(".upfacts")).toBeVisible();
  // Every right the manifest asks for has to be allowed by hand; the box is
  // named after the right, so nobody has to know which box means what.
  await page.locator(".store").getByRole("checkbox", { name: /Netz/ }).check();
  await page.locator(".store").getByRole("button", { name: "Aktivieren" }).click();
  await expect.poll(() => names(page)).toContain("ngc.store/activate");
  const t = await sentTask(page, "activate");
  const mine = await page.evaluate(async (bytes) => {
    const pack = await import("/assets/js/contracts/pack.js");
    const b64 = await import("/assets/js/contracts/b64.js");
    const u = await pack.packageUI(new Blob([new Uint8Array(bytes)]).stream());
    return { sha256: b64.b64encode(u.packageSha256), ui: b64.b64encode(u.sha256) };
  }, Array.from(pkg));
  expect(t.app).toBe("ngc.store");
  expect(t.service).toBe("controller");
  expect(t.action).toBe("activate");
  expect(t.hasSignature).toBe(true);
  expect(t.params).toEqual({
    app: "de.example.notizen", version: "1.2.0",
    sha256: mine.sha256, ui: mine.ui, store: "local", grants: ["network"],
  });
});

test("a right the admin did not allow stops the activation", async ({ page }) => {
  await store(page);
  await tab(page, "Hochladen").click();
  await page.locator(".store input[type=file]").setInputFiles({
    name: "notizen.ngc",
    mimeType: "application/octet-stream",
    buffer: Buffer.from(buildPackage(manifest({ needs: ["network"] }))),
  });
  await expect(page.locator(".upfacts")).toBeVisible();
  // The box stays empty on purpose.
  await page.locator(".store").getByRole("button", { name: "Aktivieren" }).click();
  await expect(page.getByRole("status")).toContainText("Bitte alle Rechte erlauben");
  expect(await names(page)).not.toContain("ngc.store/activate");
});

// ------------------------------------------- 3b what the machine itself said
// A refusal of an activation showed ONE sentence for every reason there is:
// "Das hat der Rechner nicht angenommen. Bitte es noch einmal versuchen."
// The catch did not even give the error a name, so whatever the controller
// said was dropped here.
//
// It matters most where the code says least. A controller that answers
// E_INTERNAL because its data disk cannot encrypt knows what the person has to
// do - use ext4 or f2fs - and "Bitte es noch einmal versuchen." is wrong twice
// over: it asks for a repeat of what cannot work, and it buries the one line
// that says why.
//
// IT IS THE SENTENCE BESIDE THE SHELL'S OWN, NOT INSTEAD OF IT, and marked as
// the server's. An earlier draft of this case showed the server's line ALONE
// and held that "Bitte es noch einmal versuchen." must then be gone. That is
// the other of the two decisions that were open, and it was not taken: the
// shell's own sentence says who refused and what this screen did, the person
// must be able to tell words from the other end of the line from the window's
// own, and dropping a wording on a server's say-so is the more generous of the
// two - the house rule reads the other way. So "Der Rechner sagt:" leads the
// server's line in, and nothing of the shell's is given up for it.
//
// The error here comes from the stand-in, built as the real ChannelError of
// net/channel.js - so the guard that decides WHETHER a sentence may be shown
// really runs. That the sentence survives the frame at all is measured where
// the frame is: shell/closemessage.spec.js.
test("a refusal shows the words of the machine, marked as the machine's",
  async ({ page }) => {
    const said = "Bitte ext4 oder f2fs mit aktivierter Verschlüsselung verwenden.";
    await store(page, { refuseActivate: said });
    await tab(page, "Hochladen").click();
    await page.locator(".store input[type=file]").setInputFiles({
      name: "notizen.ngc", mimeType: "application/octet-stream",
      buffer: Buffer.from(buildPackage(manifest({ needs: ["network"] }))),
    });
    await expect(page.locator(".upfacts")).toBeVisible();
    await page.locator(".store").getByRole("checkbox", { name: /Netz/ }).check();
    await page.locator(".store").getByRole("button", { name: "Aktivieren" }).click();
    // Whole and in this order: the shell's own first, then whose words follow,
    // then the words.
    await expect(page.getByRole("status")).toHaveText(
      "Das hat der Rechner nicht angenommen. Bitte es noch einmal versuchen. "
      + "Der Rechner sagt: " + said,
    );
  });

test("a sentence under a code that says what to do never reaches the screen", async ({ page }) => {
  // THE GUARD, AT THE SCREEN. E_VERIFY says "Unterschrift ungültig.", which
  // names what is wrong, so a server may not put its words beside it - and the
  // screen must not have a second way in. The stand-in hands the same sentence
  // under that code, and nothing of it arrives.
  const said = "Bitte ext4 oder f2fs mit aktivierter Verschlüsselung verwenden.";
  await store(page, { refuseActivate: said, refuseActivateCode: "E_VERIFY" });
  await tab(page, "Hochladen").click();
  await page.locator(".store input[type=file]").setInputFiles({
    name: "notizen.ngc", mimeType: "application/octet-stream",
    buffer: Buffer.from(buildPackage(manifest({ needs: [] }))),
  });
  await expect(page.locator(".upfacts")).toBeVisible();
  await page.locator(".store").getByRole("button", { name: "Aktivieren" }).click();
  await expect(page.getByRole("status"))
    .toHaveText("Das hat der Rechner nicht angenommen. Bitte es noch einmal versuchen.");
  await expect(page.getByRole("status")).not.toContainText("Der Rechner sagt");
  await expect(page.getByRole("status")).not.toContainText("f2fs");
});

// noFscryptText is the controller's own sentence, READ OUT OF THE GO SOURCE
// and not copied here. It is the one occasion this whole change exists for -- a
// controller whose data disk cannot encrypt a directory names the file systems
// that work (apps/server-controller/classes/runtime/data.go, NoFscryptText; the
// controller puts it in its CLOSE under E_INTERNAL in serve/activate.go) -- and
// a copy in this file would go on passing after somebody reworded the German.
// Two lines joined by + in Go, so both halves are picked up.
function noFscryptText() {
  const src = readFileSync(new URL("../../../../apps/server-controller/classes/runtime/data.go", import.meta.url), "utf8");
  const at = src.indexOf("const NoFscryptText =");
  if (at < 0) throw new Error("NoFscryptText is not in runtime/data.go any more");
  const parts = [...src.slice(at, src.indexOf("\n\n", at)).matchAll(/"((?:[^"\\]|\\.)*)"/g)].map((m) => m[1]);
  if (parts.length === 0) throw new Error("NoFscryptText has no text in runtime/data.go");
  return parts.join("");
}

test("the sentence of a disk that cannot encrypt arrives on the screen", async ({ page }) => {
  // THE OCCASION, end to end on this side of the line: the controller's own
  // words, under the code it really sends, in the window the person is looking
  // at. Before this change nothing of this shell read close.message, so this
  // sentence reached nobody and the person was told "Interner Fehler." and to
  // try again - on a kernel where every activation ends the same way.
  const said = noFscryptText();
  expect(said).toContain("ext4");
  await store(page, { refuseActivate: said });
  await tab(page, "Hochladen").click();
  await page.locator(".store input[type=file]").setInputFiles({
    name: "notizen.ngc", mimeType: "application/octet-stream",
    buffer: Buffer.from(buildPackage(manifest({ needs: [] }))),
  });
  await expect(page.locator(".upfacts")).toBeVisible();
  await page.locator(".store").getByRole("button", { name: "Aktivieren" }).click();
  await expect(page.getByRole("status")).toHaveText(
    "Das hat der Rechner nicht angenommen. Bitte es noch einmal versuchen. "
    + "Der Rechner sagt: " + said,
  );
});

test("the words of the machine go on the screen as text, never as markup", async ({ page }) => {
  // These are characters from the other end of the line, in the person's own
  // window. A dialog is already a failure in this file (store(), above); this
  // one holds the other half: the sentence is READ OUT as it came, and no tag
  // of it became an element.
  const said = "<b>ext4</b> & f2fs <img src=x onerror=alert(1)>";
  await store(page, { refuseActivate: said });
  await tab(page, "Hochladen").click();
  await page.locator(".store input[type=file]").setInputFiles({
    name: "notizen.ngc", mimeType: "application/octet-stream",
    buffer: Buffer.from(buildPackage(manifest({ needs: [] }))),
  });
  await expect(page.locator(".upfacts")).toBeVisible();
  await page.locator(".store").getByRole("button", { name: "Aktivieren" }).click();
  await expect(page.getByRole("status")).toContainText("Der Rechner sagt: " + said);
  expect(await page.locator("[role=status] b, [role=status] img").count()).toBe(0);
});

test("a refusal with nothing to say keeps the sentence the shell has", async ({ page }) => {
  // The control beside it: almost every close carries the code's own standard
  // sentence (Go: wire.Error), and net/channel.js hands none of those on. Then
  // there is nothing to show but the shell's own words, and they stay.
  await store(page, { refuseActivate: "" });
  await tab(page, "Hochladen").click();
  await page.locator(".store input[type=file]").setInputFiles({
    name: "notizen.ngc", mimeType: "application/octet-stream",
    buffer: Buffer.from(buildPackage(manifest({ needs: [] }))),
  });
  await expect(page.locator(".upfacts")).toBeVisible();
  await page.locator(".store").getByRole("button", { name: "Aktivieren" }).click();
  // It went through, because an empty knob is no refusal - the control that
  // the knob is what makes the case above fail, and not the upload.
  await expect(page.getByRole("status")).toContainText("Die App ist eingerichtet.");
});

// ---------------------------------------------------------------- 4. target

test("the target list offers only controllers that are online and run services", async ({ page }) => {
  await store(page, {
    nodes: [
      { node: "pi-1", online: true, roles: ["services"], version: "1.0.0", arch: "arm64" },
      { node: "pi-2", online: false, roles: ["services"], version: "1.0.0", arch: "arm64" },
      { node: "api-1", online: true, roles: ["api"], version: "1.0.0", arch: "amd64" },
      { node: "pi-3", online: true, roles: ["services", "db"], version: "1.0.0", arch: "arm64" },
    ],
  });
  await tab(page, "Hochladen").click();
  await page.locator(".store input[type=file]").setInputFiles({
    name: "notizen.ngc",
    mimeType: "application/octet-stream",
    buffer: Buffer.from(buildPackage(manifest({
      services: [{ name: "dienst", command: ["/usr/bin/dienst"] }],
      ui: null,
    }))),
  });
  await expect(page.locator(".upfacts")).toBeVisible();
  const opts = page.locator(".store select.target option");
  expect(await opts.allTextContents()).toEqual(["pi-1", "pi-3"]);
});

test("a controller whose version is unreadable is still offered as a target", async ({ page }) => {
  // The release binaries that went out stamped 0.0.0-dev
  // (docs/vertraege-v1.2.md, point 71). version is shown and nothing else, so
  // it is emptied and the machine stays offerable - a machine that runs
  // services does not stop running them because a word in a column cannot be
  // read.
  await store(page, {
    nodes: [
      { node: "pi-1", online: true, roles: ["services"], version: "0.0.0-dev", arch: "arm64" },
      { node: "pi-2", online: true, roles: ["services"], version: "1.0.0", arch: "arm64" },
    ],
  });
  await tab(page, "Hochladen").click();
  await page.locator(".store input[type=file]").setInputFiles({
    name: "notizen.ngc",
    mimeType: "application/octet-stream",
    buffer: Buffer.from(buildPackage(manifest({
      services: [{ name: "dienst", command: ["/usr/bin/dienst"] }],
      ui: null,
    }))),
  });
  await expect(page.locator(".upfacts")).toBeVisible();
  const opts = page.locator(".store select.target option");
  expect(await opts.allTextContents()).toEqual(["pi-1", "pi-2"]);
  await expect(page.locator(".installbox p.fail")).toHaveCount(0);
});

test("a role the contract does not know throws the list away, and the box names the field",
  async ({ page }) => {
    // roles decide whether an app may run on that machine at all, so an
    // unreadable one is not softened: the whole answer goes. What changes is
    // the sentence - it names the field instead of asking for the window to be
    // opened again, which would have changed nothing.
    await store(page, {
      nodes: [
        { node: "pi-1", online: true, roles: ["dienste"], version: "1.0.0", arch: "arm64" },
      ],
    });
    await tab(page, "Hochladen").click();
    await page.locator(".store input[type=file]").setInputFiles({
      name: "notizen.ngc",
      mimeType: "application/octet-stream",
      buffer: Buffer.from(buildPackage(manifest({
        services: [{ name: "dienst", command: ["/usr/bin/dienst"] }],
        ui: null,
      }))),
    });
    await expect(page.locator(".upfacts")).toBeVisible();
    await expect(page.locator(".installbox p.fail")).toHaveText(
      "Eine Angabe dieses Servers ist unlesbar (roles).");
    expect(await page.locator(".store select.target").count()).toBe(0);
  });

// ---------------------------------------------------------------- 5. updates

test("a downloaded update installs on one click, with a signed root task", async ({ page }) => {
  const sha = "BgYGBgYGBgYGBgYGBgYGBgYGBgYGBgYGBgYGBgYGBgY";
  await store(page, {
    updates: [{
      target: "ngc-web", node: "web-1", installed: "1.0.0", available: "1.1.0",
      sha256: sha, key: "", mode: "semi", window: "", state: "downloaded",
    }],
  });
  await tab(page, "Updates").click();
  await page.locator(".store").getByRole("button", { name: "Jetzt aktualisieren" }).click();
  await expect.poll(() => names(page)).toContain("ngc.store/install");
  const t = await sentTask(page, "install");
  expect(t.action).toBe("install");
  expect(t.node).toBe("web-1");
  expect(t.hasSignature).toBe(true);
  expect(t.params).toEqual({ target: "ngc-web", version: "1.1.0", sha256: sha });
});

test("an update that wants new rights offers the activation and no update button", async ({ page }) => {
  await store(page, {
    updates: [{
      target: "org.sorglos.terminal", node: "pi-1", installed: "1.0.0", available: "1.1.0",
      sha256: "BgYGBgYGBgYGBgYGBgYGBgYGBgYGBgYGBgYGBgYGBgY", key: "", mode: "notify",
      window: "", state: "needsgrants",
    }],
  });
  await tab(page, "Updates").click();
  await expect(page.locator(".store").getByRole("button", { name: "Jetzt aktualisieren" })).toHaveCount(0);
  await expect(page.locator(".store")
    .getByRole("button", { name: "Neue Version mit neuen Rechten aktivieren" })).toHaveCount(1);
});

// ----------------------------------------------------------- 6. standing yes

test("a standing approval takes its key from the trust list, never from the answer", async ({ page }) => {
  // The answer carries a key of its own - 32 bytes of 0xEE - and it is the
  // one key that may never end up in the list.
  const liar = "7u7u7u7u7u7u7u7u7u7u7u7u7u7u7u7u7u7u7u7u7u4";
  await store(page, {
    // A version of the trust list is checked by the shell against the list in
    // force before it is sent (trust.advance in standingYes), so the admin's
    // key has to be one somebody can really sign with.
    passwordKey: true,
    updates: [
      {
        target: "ngc-api", node: "api-1", installed: "1.0.0", available: "1.1.0",
        sha256: "", key: liar, mode: "notify", window: "", state: "available",
      },
      {
        target: "org.sorglos.terminal", node: "pi-1", installed: "1.0.0", available: "1.1.0",
        sha256: "", key: liar, mode: "notify", window: "", state: "available",
      },
    ],
  });
  await tab(page, "Updates").click();
  // Both rows go to "automatisch": an NGC part, which takes the release key,
  // and an app, which takes the key of its store. A standing approval is a
  // new version of the trust list, so the admin signs each one.
  await page.locator(".store select.mode").first().selectOption("auto");
  await answerConfirm(page);
  await expect.poll(() => names(page)).toContain("ngc.users/trust");
  await page.locator(".store select.mode").nth(1).selectOption("auto");
  await answerConfirm(page);
  await expect.poll(async () => (await names(page)).filter((n) => n === "ngc.users/trust").length).toBe(2);

  // The new versions are read back with the contract, out of the bytes the
  // shell really sent.
  const auto = await page.evaluate(async () => {
    const b64 = await import("/assets/js/contracts/b64.js");
    const trust = await import("/assets/js/contracts/trust.js");
    const recs = window.__ngcSent.filter((s) => s.name === "trust");
    const out = [];
    for (const r of recs) {
      const signed = trust.parseSigned(b64.b64decode(r.body));
      const l = await trust.parseList(signed.payload);
      out.push({
        seq: l.seq,
        prev: b64.b64encode(l.prev),
        hash: b64.b64encode(await trust.hash(signed)),
        auto: l.auto.map((a) => ({ target: a.target, key: b64.b64encode(a.key) })),
      });
    }
    return { rows: out, store: window.__ngcStoreKey, release: window.__ngcReleaseKey, liar: window.__ngcLiarKey };
  });
  const last = auto.rows[auto.rows.length - 1].auto;
  const byTarget = Object.fromEntries(last.map((a) => [a.target, a.key]));
  expect(byTarget["ngc-api"]).toBe(auto.release);
  expect(byTarget["org.sorglos.terminal"]).toBe(auto.store);
  for (const a of last) expect(a.key).not.toBe(auto.liar);
  // And the setting itself went to the API as well.
  expect(await names(page)).toContain("ngc.store/set-update");

  // THE CHAIN, AND NOT TWICE THE SAME NUMBER. Fixrunde 1 found what the lines
  // above alone let through: the store threw the answer away, the version in
  // force never moved, and the second approval was built on the same prev as
  // the first - two versions at one seq, a fork of the root everything hangs
  // from. The assertions above did not see it, because the LAST of two forks
  // still carries its own key correctly.
  expect(auto.rows.map((r) => r.seq)).toEqual([2, 3]);
  expect(auto.rows[1].prev).toBe(auto.rows[0].hash);
  // And the second version keeps the first approval instead of losing it,
  // which a version built on the older list would.
  expect(auto.rows[0].auto.map((a) => a.target)).toEqual(["ngc-api"]);
  expect(last.map((a) => a.target)).toEqual(["ngc-api", "org.sorglos.terminal"]);
});

test("two standing approvals in a row are a chain the contract itself accepts", async ({ page }) => {
  // The strongest way to say the same thing, and the one that would hold
  // without the stand-in: hand BOTH versions to trust.advance, the very
  // function the API and every controller run on them. It checks the
  // signature against the keys of the version before, seq = seq + 1 and
  // prev = the hash of that version. A fork fails it on the second version.
  //
  // For that the admin's key has to be a key somebody can really sign with,
  // so this world derives it from the password with proof.deriveSeed, exactly
  // as the shell does when it asks for the password again. One Argon2id at
  // start-up is what the knob costs.
  await store(page, {
    passwordKey: true,
    updates: [
      { target: "ngc-api", node: "api-1", installed: "1.0.0", available: "1.1.0",
        sha256: "", key: "", mode: "notify", window: "", state: "available" },
      { target: "org.sorglos.terminal", node: "pi-1", installed: "1.0.0", available: "1.1.0",
        sha256: "", key: "", mode: "notify", window: "", state: "available" },
    ],
  });
  await tab(page, "Updates").click();
  await page.locator(".store select.mode").first().selectOption("auto");
  await answerConfirm(page);
  await expect.poll(() => names(page)).toContain("ngc.users/trust");
  await page.locator(".store select.mode").nth(1).selectOption("auto");
  await answerConfirm(page);
  await expect.poll(async () => (await names(page)).filter((n) => n === "ngc.users/trust").length).toBe(2);

  const got = await page.evaluate(async () => {
    const b64 = await import("/assets/js/contracts/b64.js");
    const trust = await import("/assets/js/contracts/trust.js");
    // Version 1 of the world THIS shell started from. Asking fakeWorld()
    // again would build another world - another store key, another version 1 -
    // and clear the record of what was sent on the way.
    const start = window.__ngcListFirst;
    const chain = window.__ngcSent.filter((s) => s.name === "trust")
      .map((r) => trust.parseSigned(b64.b64decode(r.body)));
    try {
      const end = await trust.advance({ list: start.list, hash: start.hash }, chain);
      return { ok: true, seq: end.list.seq, auto: end.list.auto.map((a) => a.target) };
    } catch (e) {
      return { ok: false, why: String(e.message) };
    }
  });
  expect(got).toEqual({ ok: true, seq: 3, auto: ["ngc-api", "org.sorglos.terminal"] });
});

test("a version the shell cannot convince itself of is never sent", async ({ page }) => {
  // No knob, so the admin's key in this world is a key nobody can sign with:
  // the proof the shell builds over the new version does not verify against
  // the list in force. trust.advance - the very check the API and every
  // controller run - refuses it, and the shell refuses it first. A version
  // that was signed and turned away afterwards has already left the signature
  // behind.
  await store(page, {
    updates: [{
      target: "ngc-api", node: "api-1", installed: "1.0.0", available: "1.1.0",
      sha256: "", key: "", mode: "notify", window: "", state: "available",
    }],
  });
  await tab(page, "Updates").click();
  await page.locator(".store select.mode").first().selectOption("auto");
  await answerConfirm(page);
  await expect(page.getByRole("status")).toContainText("keine gültige Vertrauensliste");
  expect(await names(page)).not.toContain("ngc.users/trust");
  // And the setting is not switched either: what is on screen is what is in
  // force.
  await expect(page.locator(".store select.mode").first()).toHaveValue("notify");
  expect(await names(page)).not.toContain("ngc.store/set-update");
});

test("refresh does with the number what the store depends on", async ({ page }) => {
  // MEASURED AGAINST THE REAL TrustStore, not taken from a suggestion: the
  // line in standingYes hands the seq of the answer to trust.refresh and then
  // requires the version in force to have reached it. This is what that call
  // really does, over the real class, the real fetch and a real chain out of
  // shared/testdata/trust.json.
  await page.goto("/");
  test.skip(!(await hasEd25519(page)),
    "no Ed25519 in this engine's Web Crypto: no version of any chain here can be verified");
  const c = chainCase();
  const state = await serveChain(page, c, { visible: 1 });
  const got = await page.evaluate(async (raw) => {
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const { TrustStore } = await import("/assets/js/shell/trustlist.js");
    const s = new TrustStore();
    const start = await s.bootstrap({ seq: raw.seq, hash: b64decode(raw.hash) });
    // A hint that is not higher than the version in force: nothing moves and
    // nobody is asked. That is the case the store hits when the API answers
    // with a number this browser already has.
    await window.__ngcForget();
    const same = await s.refresh(start);
    const older = await s.refresh(1);
    const askedAfterSame = (await window.__ngcAsked()).length;
    // Now the API owns up to the versions the store has just written, and the
    // hint names the newest of them: the chain is followed and the answer is
    // the number that is then in force.
    await window.__ngcVisible();
    const moved = await s.refresh(4);
    return { start, same, older, askedAfterSame, moved, inForce: s.current().list.seq };
  }, { seq: c.anchor.seq, hash: c.anchor.hash });
  expect(got.start).toBe(1);
  // Not higher than what is in force: the number in force comes back, and
  // no request was made for either of the two.
  expect(got.same).toBe(1);
  expect(got.older).toBe(1);
  expect(got.askedAfterSame).toBe(0);
  // Higher: the list really moved, and the answer is the new number.
  expect(got.moved).toBe(4);
  expect(got.inForce).toBe(4);
  expect(state.from.length).toBeGreaterThan(0);
});

// ------------------------------------------------------------ 7. paid apps

test("a paid app shows the installation and the key, and takes a licence file", async ({ page }) => {
  await store(page);
  await tab(page, "Apps").click();
  await page.locator(".approw").filter({ hasText: "Tresor" }).getByRole("button", { name: "Kaufen" }).click();
  await expect(page.locator(".buy")).toBeVisible();
  const shown = await page.locator(".buy").textContent();
  const key = await page.evaluate(() => window.__ngcInstallKey);
  expect(shown).toContain(key.installation);
  expect(shown).toContain(key.key);
  await expect(page.locator(".buy").getByRole("button", { name: "Kopieren" })).toBeVisible();
  await page.locator(".buy input[type=file]").setInputFiles({
    name: "lizenz.ngclic", mimeType: "application/octet-stream", buffer: Buffer.from("LIZENZ-BYTES"),
  });
  await expect.poll(() => names(page)).toContain("ngc.store/license");
  const body = await page.evaluate(() => window.__ngcSent.find((s) => s.name === "license").body);
  expect(Buffer.from(body, "base64").toString("utf8")).toBe("LIZENZ-BYTES");
});

// ----------------------------------------------- confirming what was signed

test("a policy that asks for a confirmation stops the task until it is given", async ({ page }) => {
  // The role of this admin is at level "root", so every root task needs the
  // user to sign the task bytes a second time (trust.needsConfirm).
  await store(page, {
    level: "root",
    updates: [{
      target: "ngc-web", node: "web-1", installed: "1.0.0", available: "1.1.0",
      sha256: "BgYGBgYGBgYGBgYGBgYGBgYGBgYGBgYGBgYGBgYGBgY", key: "", mode: "semi",
      window: "", state: "downloaded",
    }],
  });
  await tab(page, "Updates").click();
  await page.locator(".store").getByRole("button", { name: "Jetzt aktualisieren" }).click();
  await expect(page.getByRole("dialog", { name: "Bitte bestätigen" })).toBeVisible();
  // Nothing goes out while the question is open.
  expect(await names(page)).not.toContain("ngc.store/install");
  await page.getByRole("dialog").getByRole("button", { name: "Abbrechen" }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  expect(await names(page)).not.toContain("ngc.store/install");
});

// ------------------------------------------------------- the shell as a whole

test("the store is reachable with the keyboard alone", async ({ page }) => {
  await store(page);
  // The window opens on "Apps", and that is the one tab a keyboard can reach
  // with Tab - the others are behind the arrows, which is what the role
  // promises. So the walk starts where the keyboard really starts.
  await expect(page.getByRole("tab", { name: "Apps", exact: true })).toHaveAttribute("aria-selected", "true");
  await page.getByRole("tab", { name: "Apps", exact: true }).focus();
  await page.keyboard.press("ArrowRight");
  await expect(page.getByRole("tab", { name: "Installiert", exact: true }))
    .toHaveAttribute("aria-selected", "true");
  await page.keyboard.press("End");
  await expect(page.getByRole("tab", { name: "Updates", exact: true })).toHaveAttribute("aria-selected", "true");
  await page.keyboard.press("Home");
  await expect(page.getByRole("tab", { name: "Quellen", exact: true })).toHaveAttribute("aria-selected", "true");
  // Every tab but the selected one is out of the Tab order.
  const stops = await page.locator('.store [role="tab"][tabindex="0"]').count();
  expect(stops).toBe(1);
});

test("a name out of the catalog stays text, and nothing of it runs", async ({ page }) => {
  await store(page, { evilName: true });
  await tab(page, "Apps").click();
  await expect(page.locator(".approw").first()).toContainText("<img src=x onerror=alert(1)>Böse");
  expect(await page.locator(".store img, .store script, .store svg, .store iframe").count()).toBe(0);
});

import { test, expect, hasSessionKeys, signIn } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";
import { buildPackage, manifest } from "../fixtures/pkg.js";

// THE EIGHT ATTACKS of apps/web/CLAUDE.md ("Angriffe") and of the design,
// section 4.6 - and this file is the last one of the plan, so its job is not
// to say them again. It is to close what was left open.
//
// WHERE EACH ONE IS MEASURED. This map is the point of the file; a case stands
// below only where the angle is new, and the map says which angle that is:
//
//  1. A user interface swapped under a valid proof
//     -> shell/appframe.spec.js, "a ui-tar that does not fit the proof is
//        refused". NEW HERE: that the refusal is not remembered. A shell that
//        put the refused archive in the cache of proven ones beside the others
//        would hand it straight back the second time the window was opened,
//        and a hit in that cache means "already checked".
//  2. A man in the middle at the initial setup
//     -> contracts/session.spec.js runs the whole offers table of
//        shared/testdata/session.json, "MAC with another code" and "info
//        changed after the MAC" among them. NEW HERE: that NOTHING OF A
//        REFUSED OFFER CAN BE READ - the second half of the sentence in the
//        design, and the half a parser test does not make. The surface that
//        would show it is Task 19's; what this file can hold to account is the
//        value that surface would be handed, and that is nothing.
//  3. A trust list that was tampered with: alarm and log out
//     -> shell/trustlist.spec.js, "a version whose bytes were changed raises
//        the alarm, logs out and leaves no list", with the emptying of the
//        proven user interfaces in "a new version empties the user interfaces
//        the shell had already proven". Nothing missing; no case here.
//  4. An OLDER version of the trust list
//     -> shell/trustlist.spec.js measures the HINT that names an older number
//        ("asks nothing"). NEW HERE: the version itself, served as an answer.
//        That is a different thing - a hint is a number, an answer is bytes.
//  5. A checksum that does not match on upload
//     -> shell/store.spec.js, "an answer that names another package breaks the
//        upload off and activates nothing", for ONE of the four values the
//        answer carries. NEW HERE: each of the other three on its own. Three
//        fields taken on trust and one compared would pass that case.
//  6. More channels than wire.MaxSessionChannels
//     -> shell/channels.spec.js, "the shell never opens more channels than
//        MaxSessionChannels". Nothing missing; no case here.
//  7. A result over wire.MaxCallResult on an END-TO-END channel
//     -> shell/channels.spec.js measures it for an answer of the API. The
//        end-to-end side is NEW HERE, and it is the side that matters: the API
//        never sees that plaintext, so no other side can bound it.
//  8. The password in a recording
//     -> shell/login.spec.js, "the password never leaves the browser", for the
//        LOGIN. NEW HERE: the SECOND one - the confirmation that asks for the
//        password again (shell/confirm.js, purpose confirm/v1). It is typed
//        there in several cases of shell/store.spec.js (answerConfirm), but
//        nothing has ever searched the wire afterwards, which is the half the
//        design asks for and the half that is here.

// valuesOf walks a setup offer and hands back every string value in it that is
// long enough to be a name, an address, an id or a key - both of the envelope
// {info, mac} and of the info document inside it, which travels as base64.
// Field names are left out on purpose; see the case that uses this.
function valuesOf(offerB64) {
  const out = [];
  const walk = (v) => {
    if (typeof v === "string") { if (v.length >= 8) out.push(v); return; }
    if (Array.isArray(v)) { for (const x of v) walk(x); return; }
    if (v !== null && typeof v === "object") { for (const x of Object.values(v)) walk(x); }
  };
  const text = (b64) => {
    try {
      return Buffer.from(String(b64).replace(/-/g, "+").replace(/_/g, "/"), "base64").toString("utf8");
    } catch {
      return "";
    }
  };
  let envelope = null;
  try {
    envelope = JSON.parse(text(offerB64));
  } catch {
    return out;
  }
  walk(envelope);
  if (typeof envelope?.info === "string") {
    try {
      walk(JSON.parse(text(envelope.info)));
    } catch {
      // An info that is no JSON has no values of its own to hide.
    }
  }
  return out;
}

// --------------------------------------------------- 1. the swapped interface

test.describe("a user interface that does not fit its proof", () => {
  test.beforeEach(async ({ shell }) => {
    test.skip(!(await hasSessionKeys(shell)),
      "no Ed25519 and no X25519 in this engine's Web Crypto: the shell stops before the desktop");
    await signIn(shell);
    await expect(shell.locator("#taskbar")).toBeVisible();
  });

  const openTerminal = async (shell) => {
    await shell.getByRole("button", { name: "Start" }).click();
    await shell.getByRole("menuitem", { name: "Terminal" }).click();
  };

  const REFUSED = "Die Oberfläche dieser App ist nicht echt. NGC führt sie nicht aus.";

  test("is refused twice over, and the honest one still opens", async ({ shell }) => {
    // The stand-in hands out an archive that does not hash to what the signed
    // proof says.
    await shell.evaluate(() => { window.__ngcTest.swapUI = true; });

    await openTerminal(shell);
    await expect(shell.getByRole("alert")).toHaveText(REFUSED);
    await expect(shell.locator(".appframe")).toHaveCount(0);

    // THE NEW HALF, and it is measured on behaviour rather than on the inside
    // of a cache: the SAME app at the SAME version, a second time. The shell
    // asks its cache of proven interfaces first, and a hit there is never
    // checked again - so if the refused archive had gone in, this window would
    // show the app.
    //
    // The window is closed first, because the desktop keeps one window per app
    // and would only bring this one forward again (desktop.openApp).
    await shell.getByRole("button", { name: "Schließen" }).click();
    await expect(shell.getByRole("alert")).toHaveCount(0);
    await openTerminal(shell);
    await expect(shell.getByRole("alert")).toHaveText(REFUSED);
    await expect(shell.locator(".appframe")).toHaveCount(0);

    // And the honest path still works: the refusal was about those bytes, not
    // about that app. Without this line the two above would also be true of a
    // shell that had simply stopped opening windows.
    //
    // WHAT IT LOOKS AT CHANGED WITH TASK 25, the assurance did not. This line
    // read getByText("Terminal"), the heading of the fixture page the stand-in
    // carried while nobody had written the terminal yet. The terminal is a real
    // app now, and its own surface is what says the honest archive ran: the
    // field it asks for commands with is there only if the page, its stylesheet
    // and its script all came out of the archive and ngc.js reached the shell.
    // That is strictly more than a heading, which a page shows even when its
    // script never ran.
    await shell.getByRole("button", { name: "Schließen" }).click();
    await shell.evaluate(() => { window.__ngcTest.swapUI = false; });
    await openTerminal(shell);
    await expect(shell.frameLocator(".appframe").getByRole("textbox", { name: "Eingabe" }))
      .toBeVisible();
  });
});

// ------------------------------------------- 2. the man in the middle at setup

test("nothing of a setup offer that was tampered with can be read", async ({ shell }) => {
  const v = vectors("session.json");
  const bad = v.setup.offers.filter((c) => c.valid !== true);
  // The table IS the measurement, so there has to be a table. An empty filter
  // would make every line below true of nothing at all.
  expect(bad.length, "shared/testdata/session.json holds offers that must be refused")
    .toBeGreaterThan(5);

  const got = await shell.evaluate(async ({ offers, code }) => {
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const s = await import("/assets/js/contracts/session.js");
    const out = [];
    for (const c of offers) {
      try {
        const offer = await s.parseSetupOffer(b64decode(c.offer), code);
        // It was TAKEN. What came out is reported, so the failure names the
        // case and the value instead of only saying "not null".
        out.push({ name: c.name, took: JSON.stringify(offer).slice(0, 200), text: "" });
      } catch (e) {
        out.push({ name: c.name, took: null, text: String(e && e.message ? e.message : e) });
      }
    }
    return out;
  }, { offers: bad, code: v.setup.code });

  expect(got.length).toBe(bad.length);
  for (let i = 0; i < got.length; i++) {
    const r = got[i];
    expect(r.took, `${r.name}: refused`).toBeNull();

    // THE SECOND HALF: nothing out of the offer is in what the shell was
    // handed back instead. A refusal carrying the offer's own node name, its
    // address or a key would be a refusal somebody could still put on a
    // screen - and an address on a screen is how a man in the middle gets
    // confirmed by hand.
    //
    // What is searched for are the VALUES of the offer and not its field
    // names: a field name in a message is the contract talking ("the offer has
    // no release key"), a value is the attacker talking ("wss://fremd.test is
    // no tunnel address"). valuesOf walks both levels of the document for
    // them - the envelope, and the info inside it, which travels base64 in
    // the envelope and is where the names, addresses and keys are.
    const words = valuesOf(bad[i].offer);
    expect(words.length, `${r.name}: the offer really has values in it`).toBeGreaterThan(0);
    for (const w of words) {
      expect(r.text, `${r.name}: the refusal says nothing of "${w}"`).not.toContain(w);
    }
  }
});

// ------------------------------------------------- 4. an older trust list page

test("a version of the trust list with a smaller number changes nothing", async ({ shell }) => {
  const good = vectors("trust.json").find((c) =>
    c.name === "admin adds a user, user adds an own key, passkey admin re-signs");
  expect(good, "trust.json holds the four-version chain").toBeTruthy();

  // Ed25519 or nothing: every signature of this chain is one, and an engine
  // without it would measure itself. Probed with a key out of the fixture,
  // never decided by the engine's name.
  const canSign = await shell.evaluate(async (c) => {
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const payload = JSON.parse(new TextDecoder().decode(b64decode(c.chain[0].payload)));
    const pub = payload.users.flatMap((u) => u.keys).find((k) => k.kind === "password")?.pub;
    try {
      await crypto.subtle.importKey("raw", b64decode(pub), { name: "Ed25519" }, false, ["verify"]);
      return true;
    } catch {
      return false;
    }
  }, good);
  test.skip(!canSign, "this engine's Web Crypto has no Ed25519: no chain can be checked here");

  // The API is honest up to version 4 and then serves an OLDER version again -
  // version 2, with its own real signature, where 5 belongs. That is the
  // rollback: every byte of it was signed once, and only its number is wrong.
  let servedOld = 0;
  await shell.route(/\/api\/v1\/trust\?/, async (route) => {
    const from = Number(new URL(route.request().url()).searchParams.get("from"));
    if (from === 1) {
      await route.fulfill({
        status: 200, contentType: "application/json; charset=utf-8",
        body: JSON.stringify({ versions: good.chain, more: false }),
      });
      return;
    }
    servedOld++;
    await route.fulfill({
      status: 200, contentType: "application/json; charset=utf-8",
      body: JSON.stringify({ versions: [good.chain[1]], more: false }),
    });
  });
  await shell.route(/\/api\/v1\/logout$/, (route) => route.fulfill({ status: 204 }));

  const out = await shell.evaluate(async (raw) => {
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const { TrustStore } = await import("/assets/js/shell/trustlist.js");
    const s = new TrustStore();
    const alarms = [];
    s.onAlarm((t) => alarms.push(t));
    const first = await s.bootstrap({ seq: raw.seq, hash: b64decode(raw.hash) });
    let failed = false;
    // The hint names 5, so the shell really does ask - which is what makes the
    // answer above reach it at all.
    await s.refresh(5).catch(() => { failed = true; });
    return { first, failed, alarms, seq: s.current()?.list?.seq ?? 0 };
  }, { seq: good.anchor.seq, hash: good.anchor.hash });

  expect(out.first, "the honest chain was followed first").toBe(4);
  expect(servedOld, "the rollback really was served").toBe(1);
  expect(out.failed, "the rollback was refused").toBe(true);
  // THE SECURITY PROPERTY, and the one this case pins: the shell never ends up
  // believing the older version. 2 is the number that was served; anything but
  // 2 means it was not taken.
  expect(out.seq, "the older version was never taken").not.toBe(2);

  // WHAT WAS MEASURED BEYOND THAT, and why it is not asserted here: today the
  // answer leaves the shell with NO list (seq 0) and raises the alarm, because
  // shell/trustlist.js treats every failure while following the chain as a
  // break of the chain. The design (4.6, and the brief of this task) asks for
  // the other reading: a rollback is the API sending something that does not
  // continue the chain, which is that API misbehaving - not evidence against
  // the version the shell has already verified - so the shell should keep its
  // 4 and raise NO alarm. Which of the two is right is the coordinator's call
  // and it is written up in the task report; shell/ is not this task's to
  // change. So the number above is pinned and the rest is left open, rather
  // than this case blessing either reading.
  expect([0, 4], "today 0, after the design's reading 4 - see the task report")
    .toContain(out.seq);
});

// ---------------------------------------------- 5. an upload answered falsely

// A package the shell weighs itself: manifest.json and one ui file.
const PKG = buildPackage(manifest({}));

// The four values core.UploadResult carries. Each one is twisted ON ITS OWN,
// so a shell that compared three and took the fourth on trust fails exactly
// one of these cases and says which.
const TWISTS = [
  ["the package checksum", { sha256: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAM" }],
  ["the UI checksum", { ui: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAM" }],
  ["the version", { version: "9.9.9" }],
  ["the app id", { id: "de.example.fremd" }],
];

for (const [what, twist] of TWISTS) {
  test(`an upload answered with another ${what} activates nothing`, async ({ page }) => {
    await page.addInitScript((w) => { window.__ngcStore = w; }, { uploadTwist: twist });
    await page.goto("/");
    test.skip(!(await hasSessionKeys(page)),
      "no Ed25519 and no X25519 in this engine's Web Crypto: the shell stops before the desktop");
    await signIn(page);
    await expect(page.locator("#taskbar")).toBeVisible();
    await page.locator("#icons").getByRole("button", { name: "App-Store", exact: true }).click();
    await expect(page.locator(".store")).toBeVisible();
    await page.getByRole("tab", { name: "Hochladen", exact: true }).click();
    await page.locator(".store input[type=file]").setInputFiles({
      name: "notizen.ngc", mimeType: "application/octet-stream", buffer: Buffer.from(PKG),
    });

    // The sentence does not name the field: which number the API got wrong is
    // the inside of a server, and there is one thing the person can do about
    // any of them.
    await expect(page.locator(".upfail"))
      .toHaveText("Die Anlage hat ein anderes Paket bekommen, als geschickt wurde.");
    await expect(page.locator(".store").getByRole("button", { name: "Aktivieren" })).toHaveCount(0);
    const names = await page.evaluate(() =>
      (window.__ngcSent ?? []).map((s) => s.app + "/" + s.name));
    // The upload itself went out - so this is a case about the ANSWER and not
    // about a shell that never got that far.
    expect(names, "the upload really happened").toContain("ngc.store/upload");
    expect(names).not.toContain("ngc.store/activate");
  });
}

// --------------------------------------- 7. the bound of an end-to-end answer

test("the shell bounds an end-to-end answer, and it is the only side that can", async ({ shell }) => {
  test.skip(!(await hasSessionKeys(shell)),
    "this engine has no Ed25519/X25519: no session keys, no session link");

  // WHAT THIS FOUND, written here because the next author of an app needs it:
  // the bound is in net/channel.js (Channel.result -> checkLimit) and it is
  // the CALLER who names it. channel.call and channel.sendIn pass
  // wire.MAX_CALL_RESULT; channel.openE2E hands the channel back with no bound
  // set at all, because a stream action is meant to be unbounded. So an app
  // action WITHOUT "stream" in its manifest is bounded only where its caller
  // says so, and today there is no such caller in the shell yet. This case is
  // the bound working on an end-to-end channel; who has to pass it is in the
  // task report.
  // TWO MEASUREMENTS OF THE MOCK decide the shape of this case, and both are in
  // the task report rather than fixed in tests/mock/ - that folder belongs to
  // another task that is running right now.
  //
  //  1. ONE END-TO-END CHANNEL PER LINK. When a controller's inner END arrives,
  //     the mock deletes its channel at once (innerEnd), and the CLOSE the
  //     shell sends back then lands on a channel the mock no longer knows -
  //     which it answers by tearing the whole socket down. Measured: a second
  //     openE2E on the same link answers E_UNREACHABLE. shell/channels.spec.js
  //     never sees it because its case closes the link straight after. So the
  //     control and the attempt get a session each.
  //  2. EIGHT BYTES AND NOT MORE. Whatever the controller side has seen in the
  //     clear goes into world.secrets for the rest of the mock's life, and
  //     every frame of every later session is then scanned for it with
  //     contains() - a position-by-position loop whose compare is b64.equal,
  //     which is CONSTANT TIME and therefore reads the whole needle at every
  //     position. A 4 KiB payload here cost the 9 MiB case of
  //     shell/channels.spec.js its 30-second timeout (measured: 947 ms alone,
  //     over 30 s after this file had run). Eight bytes is the same order as
  //     the six that case already leaves behind.
  const out = await shell.evaluate(async () => {
    const { connectForTest } = await import("/assets/js/net/testlink.js");
    const bytes = new Uint8Array(8).fill(65);
    const where = { app: "org.sorglos.terminal", service: "terminal", action: "shell" };
    const params = { cols: 80, rows: 24 };

    // THE CONTROL. The same channel, the same echo, a bound the answer fits
    // under: it comes back whole. Without this the refusal below would also be
    // true of an end-to-end channel that never carried anything.
    const first = await connectForTest();
    const okCh = await first.openE2E(where.app, where.service, where.action, params);
    await okCh.write(bytes);
    await okCh.end();
    const whole = (await okCh.result(1 << 16)).length;
    first.close();


    // THE ATTEMPT. A bound smaller than what comes back. These bytes are
    // inside the Noise session to the controller, so the API cannot even see
    // them - if the shell does not stop here, nothing does.
    const second = await connectForTest();
    const bigCh = await second.openE2E(where.app, where.service, where.action, params);
    await bigCh.write(bytes);
    await bigCh.end();
    let code = "none";
    try {
      // Two bytes, and eight are coming back.
      await bigCh.result(2);
    } catch (e) {
      code = e.code ?? String(e);
    }
    second.close();
    return { whole, code };
  });
  expect(out.whole, "the control really carried the whole answer").toBe(8);
  expect(out.code).toBe("E_TOOLARGE");
});

// ------------------------------------------- 8. the password of a confirmation

test("the password of the second confirmation reaches no wire and no link", async ({ page }) => {
  // Everything that leaves the page, collected before anything is typed: the
  // body and the address of every request, and both directions of every socket
  // frame. The same two listeners as shell/login.spec.js, because the question
  // is the same one - only the password is typed into a different box.
  const seen = [];
  page.on("request", (r) => seen.push((r.postData() ?? "") + " " + r.url()));
  const frames = [];
  page.on("websocket", (ws) => {
    ws.on("framesent", (f) => frames.push(String(f.payload)));
    ws.on("framereceived", (f) => frames.push(String(f.payload)));
  });
  // A dialog of the engine's own would mean something in the page reached for
  // alert or prompt with text it did not build itself.
  page.on("dialog", (d) => { throw new Error("the page opened a " + d.type()); });

  // The store with an admin at level root: every root task has to be signed a
  // second time by the user (trust.needsConfirm), and this user has no passkey
  // in the stand-in's list, so the question asks for the password.
  await page.addInitScript((w) => { window.__ngcStore = w; }, {
    level: "root",
    // passwordKey, so the admin's key in the stand-in's list really is the one
    // this password derives - otherwise the signature would be over a key
    // nobody holds and the case would measure the fixture.
    passwordKey: true,
    updates: [{
      target: "ngc-web", node: "web-1", installed: "1.0.0", available: "1.1.0",
      sha256: "BgYGBgYGBgYGBgYGBgYGBgYGBgYGBgYGBgYGBgYGBgY", key: "", mode: "semi",
      window: "", state: "downloaded",
    }],
  });
  await page.goto("/");
  test.skip(!(await hasSessionKeys(page)),
    "no Ed25519 and no X25519 in this engine's Web Crypto: the shell stops before the desktop");
  await signIn(page);
  await expect(page.locator("#taskbar")).toBeVisible();
  await page.locator("#icons").getByRole("button", { name: "App-Store", exact: true }).click();
  await expect(page.locator(".store")).toBeVisible();
  await page.getByRole("tab", { name: "Updates", exact: true }).click();
  await page.locator(".store").getByRole("button", { name: "Jetzt aktualisieren" }).click();

  // The password is READ OUT OF THE WORLD and not written here a second time:
  // a password that had drifted from the key the stand-in derives would make
  // the signature fail, and the case would blame the shell for the fixture.
  const PASSWORD = await page.evaluate(async () =>
    (await import("/assets/js/shell/fake-link.js")).PASSWORD);
  expect(typeof PASSWORD, "the stand-in names the password it derives from").toBe("string");
  expect(PASSWORD.length, "and it is a password, not an empty string").toBeGreaterThan(4);

  const dialog = page.getByRole("dialog", { name: "Bitte bestätigen" });
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText("Bitte noch einmal das Passwort eingeben.");
  await dialog.locator("input[type=password]").fill(PASSWORD);
  await dialog.getByRole("button", { name: "Bestätigen", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);

  // THE GUARD THAT MAKES THIS A MEASUREMENT. The task has to have gone out,
  // with a confirmation on it. A search through a wire that carried nothing
  // would find no password either.
  // The task leaves AFTER the signature, so it is waited for rather than read
  // off at once - a read that was too early would make the guard below fail
  // for the wrong reason.
  await page.waitForFunction(() =>
    (window.__ngcSent ?? []).some((s) => s.app + "/" + s.name === "ngc.store/install"));
  const sent = await page.evaluate(() => {
    const rec = (window.__ngcSent ?? []).find((s) => s.app + "/" + s.name === "ngc.store/install");
    return rec === undefined ? null : { confirm: rec.confirm ?? "", task: rec.task ?? "" };
  });
  expect(sent, "the install task really went out").not.toBeNull();
  expect(sent.confirm.length, "and it carried a confirmation").toBeGreaterThan(0);

  // Every spelling the password could have on a wire, as login.spec.js lists
  // them: plainly, form encoded, and in both base64 alphabets.
  const b64 = Buffer.from(PASSWORD, "utf8").toString("base64");
  const forbidden = [PASSWORD, encodeURIComponent(PASSWORD), b64,
    b64.replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "")];
  const everywhere = [
    ["the requests", seen.join("\n")],
    ["the socket frames", frames.join("\n")],
    // What the shell handed the link. The confirmation is a signature over the
    // task bytes, and a signature is all it may be.
    ["the confirmation", sent.confirm],
    ["the task", sent.task],
  ];
  for (const needle of forbidden) {
    for (const [where, haystack] of everywhere) {
      expect(haystack, `${where} must not hold the password as "${needle}"`).not.toContain(needle);
    }
  }

  // And nothing of the question is left in the document.
  const left = await page.evaluate(() => {
    const f = document.getElementById("confirmpw");
    return f === null ? "gone" : f.value;
  });
  expect(left).toBe("gone");
});

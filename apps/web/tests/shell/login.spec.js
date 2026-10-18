// The login mask: the password key, TOTP and passkeys.
//
// WHAT THIS FILE MEASURES AND WHAT IT CANNOT. The one promise of task 18 is
// that the password never leaves the browser, and the first case measures
// exactly that - every request body, every URL and every WebSocket frame of a
// whole login is collected and searched. The rest measures what the person in
// front of the screen sees: one sentence for every failed login, a name that
// does not exist answering like one that does, a locked login that counts
// down, TOTP where the challenge asks for it, and a passkey that signs the
// certificate.
//
// THREE PLACES WHERE THIS FILE DIFFERS FROM THE TASK BRIEF, each measured
// rather than argued:
//
//  1. The brief steers the mock with window.__ngcTest = { lock: 30 } and
//     window.__ngcTest = { user: "clara" }. A hook on window that changes how
//     the LOGIN behaves is a hook anything else on the page can set, and the
//     login is the one page where that must not be possible - so the shell
//     reads no such thing. What steers the mock here is the mock's own test
//     door (/api/v1/mock, which net/link.js and the shell know nothing about)
//     and the login NAME, which is what steers a real API too.
//  2. The passkey cases run at http://localhost:<port>/ and not at the
//     suite's 127.0.0.1. Measured in chromium with a virtual authenticator:
//     at an IP origin every rp id - "ngc.example.org", "127.0.0.1" and none
//     at all - answers "SecurityError: This is an invalid domain", because an
//     IP literal has no effective domain for WebAuthn. The same page reached
//     through localhost takes rp id "localhost" and makes a credential. Both
//     names are the same server (the mock listens on 127.0.0.1 and localhost
//     resolves to it here), so nothing but the origin changes.
//  3. The brief's passkey case ends with
//     `expect(both === true || both === false).toBe(true)` over a flag of
//     keys.js, in a case that never logs the passwordToo user in at all. That
//     assertion holds for every boolean and so for every implementation: it
//     cannot fail for the thing it is named after. It is replaced here by a
//     case that logs dora in and asks the MOCK how many proofs it had to
//     check - a number this side cannot produce by itself.
import { test, expect, skipWithoutLogin, skipWithoutSeedKeys, skipWithoutShell, standInOn } from "../fixtures/page.js";
import { PASSWORD, PASSKEY_USERS, TOTP_CODE, USER_NAME } from "../fixtures/logins.js";

// The mock's test door. It is on /api/ so it travels the same origin as
// everything else, and it is the mock's alone: nothing of the shell knows it.
// MEASURED, never read off the engine's name. shell/main.js needs Ed25519 and
// X25519 before it builds anything at all, and the WebKit that Playwright
// brings has neither - so there is no mask to type into there, and the boot
// says so instead (shell/boot.spec.js is the case that pins THAT, and it runs
// everywhere). The day this WebKit gains the two, this answers yes by itself
// and every case below runs there with no edit here.
//
// This asks for the MASK, not for a login that goes through. Most cases here
// measure a login that FAILS on purpose - a wrong password, a lock, an empty
// field afterwards - and those run on any engine that can draw the mask. The
// three that wait for a desktop ask for themselves, with skipWithoutLogin.
// Asking the whole file for a finished login switched three of them off on
// Firefox that run there (task 30, Fixrunde 1).
test.beforeEach(async ({ shell }) => {
  await skipWithoutShell(shell);
  // The five cases below look at the DESKTOP behind the mask, and since task
  // 17b the desktop behind it is the real one: net/world.js opens the trust
  // list on the session this login just made, and the chain of the mock API
  // cannot fit the anchor of this test server. The stand-in is what those
  // cases still eat, and standInOn says so - the cases that go through
  // signIn() get it from there, these type into the mask themselves. It leaves
  // with shell/fake-link.js.
  await standInOn(shell);
});

async function tellMock(shell, query) {
  const ok = await shell.evaluate(async (q) => {
    const res = await fetch("/api/v1/mock?" + q, { method: "POST", cache: "no-store" });
    return res.ok;
  }, query);
  expect(ok, "the mock took the instruction").toBe(true);
}

async function askMock(shell, query) {
  return shell.evaluate(async (q) => {
    const res = await fetch("/api/v1/mock?" + q, { cache: "no-store" });
    return res.text();
  }, query);
}

// login fills the mask and presses the button. It is three lines everywhere
// else in the suite from task 21 on, so it stands here once.
//
// exact: true, and the task brief has it without: playwright matches an
// accessible name as a case-insensitive substring by default, so plain
// "Anmelden" also finds "Mit Passkey anmelden" and every click of the brief's
// own cases fails with a strict-mode violation (measured). The brief's
// E_LOCKED case writes /Anmelden/ instead, which is case-SENSITIVE and does
// hit one button - the two spellings in one file disagree with each other.
async function fillLogin(shell, name, password) {
  await shell.getByLabel("Benutzername").fill(name);
  await shell.getByLabel("Passwort").fill(password);
  await press(shell, shell.getByRole("button", { name: "Anmelden", exact: true }));
}

// press clicks a login button and returns once the attempt it started is over.
//
// WHY A STATE AND NOT A TIMEOUT. An attempt takes as long as its slowest step,
// and the slowest is Argon2id (about a second alone, tens of seconds on a
// machine that is running three other suites): a case that waited for the
// RESULT of an attempt with expect's own 5 s failed once in 796 under load
// (task 33) and never alone. A longer number only moves that line. What the
// shell itself says is better: loginView (auth/login.js attempt) switches the
// buttons off the moment an attempt starts, clears the alert in the same step,
// and either puts the mask away (success) or switches the buttons on again,
// with a sentence, a lock or the TOTP field (failure). So "the attempt is
// over" is a thing the page SAYS, and this waits for exactly that. The only
// bound left is the case's own timeout, which is a budget for the whole case
// and not a guess about one step.
//
// "mask idle" covers success-less endings too (wrong password, lock, TOTP
// asked for): the case's own expectations judge what the ending was.
async function loginState(shell) {
  return shell.evaluate(() => {
    const box = document.querySelector("form.loginbox");
    if (box !== null) {
      const off = [...box.querySelectorAll("button")].every((b) => b.disabled);
      const said = box.querySelector('[role="alert"]');
      return off && (said === null || said.textContent === "") ? "working" : "mask idle";
    }
    if (document.querySelector("#taskbar") !== null) return "desktop up";
    if (document.querySelector("#root .msg") !== null) return "shell message";
    return "working"; // the mask is gone and the desktop is still being built
  });
}

async function attemptOver(shell) {
  await expect.poll(async () => (await loginState(shell)) !== "working",
    { message: "the login attempt is over", timeout: 0 }).toBe(true);
}

async function press(shell, button) {
  await button.click();
  await attemptOver(shell);
}

// atLocalhost moves the page to the same server under its DNS name, which is
// what WebAuthn needs (see the note at the top), and hands back the page.
async function atLocalhost(shell) {
  const here = new URL(shell.url());
  await shell.goto(`http://localhost:${here.port}/`);
  await expect(shell.getByLabel("Benutzername")).toBeVisible();
}

// withAuthenticator puts a virtual authenticator in front of the browser and
// loads one credential of the mock's world into it: the very key pair the
// mock holds the public half of, so the assertion it makes verifies.
async function withAuthenticator(shell, who) {
  const cdp = await shell.context().newCDPSession(shell);
  await cdp.send("WebAuthn.enable");
  const { authenticatorId } = await cdp.send("WebAuthn.addVirtualAuthenticator", {
    options: {
      protocol: "ctap2", transport: "usb", hasUserVerification: true,
      hasResidentKey: true, isUserVerified: true, automaticPresenceSimulation: true,
    },
  });
  const u = PASSKEY_USERS[who];
  await cdp.send("WebAuthn.addCredential", {
    authenticatorId,
    credential: {
      credentialId: u.credIdBase64, rpId: u.rpId, privateKey: u.privateKeyBase64,
      isResidentCredential: true, userHandle: u.userHandleBase64, signCount: 0,
    },
  });
  return { cdp, authenticatorId };
}

test("the password never leaves the browser", async ({ shell }) => {
  await skipWithoutLogin(shell);
  const seen = [];
  shell.on("request", (r) => seen.push((r.postData() ?? "") + " " + r.url()));
  const frames = [];
  shell.on("websocket", (ws) => {
    ws.on("framesent", (f) => frames.push(String(f.payload)));
    ws.on("framereceived", (f) => frames.push(String(f.payload)));
  });

  await fillLogin(shell, "anna", PASSWORD);
  await expect(shell.locator("#taskbar")).toBeVisible();

  // The password as it could possibly be spelled on a wire: plainly, form
  // encoded, and in both base64 alphabets - base64url is the one the
  // contracts use, and plain base64 is the one everything else uses.
  const b64 = btoa(PASSWORD);
  const forbidden = [PASSWORD, encodeURIComponent(PASSWORD), b64,
    b64.replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "")];
  for (const needle of forbidden) {
    expect(seen.join("\n"), needle).not.toContain(needle);
    expect(frames.join("\n"), needle).not.toContain(needle);
  }
  // A search that found nothing because there was nothing to search would
  // pass every line above. There has to have been a login on that wire.
  expect(seen.join("\n"), "the login really went over this wire").toContain("/api/v1/login/finish");
  expect(frames.length, "the session really opened a socket").toBeGreaterThan(0);
});

test("the password field is empty again the moment it has been used", async ({ shell }) => {
  // The password ROUTE of the mask, which since 17.10.2026 is shut where the
  // engine makes no password key out of a seed: the field is disabled and the
  // button with it (auth/login.js, passwordKeys), so there is nothing to type
  // into. Not skipWithoutLogin - these three measure a login that FAILS and
  // need no X25519 and no desktop.
  await skipWithoutSeedKeys(shell);
  // A login that FAILS is what this can be measured on: the mask is still
  // there afterwards, and the field has to be empty all the same. After a
  // login that works the mask is gone and there is nothing left to look at.
  await fillLogin(shell, "anna", "falsch");
  // Waited for by "the attempt is over", not by WHAT it says: the sentence
  // belongs to the case below, and a case that goes red because another
  // case's sentence moved is a case that measures the wrong thing.
  await expect(shell.getByRole("alert")).not.toBeEmpty();
  await expect(shell.getByLabel("Passwort")).toHaveValue("");
});

test("a wrong password says only that the login failed", async ({ shell }) => {
  // The password ROUTE of the mask, which since 17.10.2026 is shut where the
  // engine makes no password key out of a seed: the field is disabled and the
  // button with it (auth/login.js, passwordKeys), so there is nothing to type
  // into. Not skipWithoutLogin - these three measure a login that FAILS and
  // need no X25519 and no desktop.
  await skipWithoutSeedKeys(shell);
  await fillLogin(shell, "anna", "falsch");
  await expect(shell.getByRole("alert")).toHaveText("Anmeldung fehlgeschlagen.");
  // An unknown name must give exactly the same answer.
  await fillLogin(shell, "gibtesnicht", "egal");
  await expect(shell.getByRole("alert")).toHaveText("Anmeldung fehlgeschlagen.");
});

test("E_LOCKED waits the given seconds", async ({ shell }) => {
  // The password ROUTE of the mask, which since 17.10.2026 is shut where the
  // engine makes no password key out of a seed: the field is disabled and the
  // button with it (auth/login.js, passwordKeys), so there is nothing to type
  // into. Not skipWithoutLogin - these three measure a login that FAILS and
  // need no X25519 and no desktop.
  await skipWithoutSeedKeys(shell);
  await tellMock(shell, "lock=30");
  await fillLogin(shell, "anna", PASSWORD);
  await expect(shell.getByRole("alert")).toContainText("Zu viele Fehlversuche");
  await expect(shell.getByRole("alert")).toContainText("30 Sekunden");
  await expect(shell.getByRole("button", { name: "Anmelden", exact: true })).toBeDisabled();
});

test("TOTP is asked for when the challenge says so", async ({ shell }) => {
  await skipWithoutLogin(shell);
  await fillLogin(shell, "bernd", PASSWORD);
  await expect(shell.getByLabel("Code aus der Authenticator-App")).toBeVisible();
  await shell.getByLabel("Code aus der Authenticator-App").fill(TOTP_CODE);
  await press(shell, shell.getByRole("button", { name: "Anmelden", exact: true }));
  await expect(shell.locator("#taskbar")).toBeVisible();
});

test("a passkey signs the certificate, on a browser that has never been here",
  async ({ shell, browserName }) => {
  test.skip(browserName !== "chromium", "virtual authenticators only exist in Chromium");
  await atLocalhost(shell);
  // A browser that has never seen this installation: nothing remembered, no
  // trust list yet. The key ID comes from the challenge alone, so this has to
  // work on the first try.
  await shell.evaluate(() => { localStorage.clear(); sessionStorage.clear(); });
  const { cdp, authenticatorId } = await withAuthenticator(shell, "clara");

  await shell.getByLabel("Benutzername").fill("clara");
  await press(shell, shell.getByRole("button", { name: "Mit Passkey anmelden" }));
  await expect(shell.locator("#taskbar")).toBeVisible();
  expect(await askMock(shell, "ask=proofs"), "one proof, by the passkey").toBe("passkey");

  await cdp.send("WebAuthn.removeVirtualAuthenticator", { authenticatorId });
});

test("a role with passwordToo has the certificate signed twice", async ({ shell, browserName }) => {
  test.skip(browserName !== "chromium", "virtual authenticators only exist in Chromium");
  await atLocalhost(shell);
  const { cdp, authenticatorId } = await withAuthenticator(shell, "dora");

  // dora is in a role with passwordToo: the proof comes from the passkey and
  // the password proof from the password key, both over the same certificate.
  await shell.getByLabel("Benutzername").fill("dora");
  await shell.getByLabel("Passwort").fill(PASSWORD);
  await press(shell, shell.getByRole("button", { name: "Mit Passkey anmelden" }));
  await expect(shell.locator("#taskbar")).toBeVisible();
  // The mock is the one that had to check both, so it is the one asked. A
  // flag this side sets about its own work would say nothing.
  expect(await askMock(shell, "ask=proofs"), "both proofs, over the same bytes")
    .toBe("passkey+password");

  await cdp.send("WebAuthn.removeVirtualAuthenticator", { authenticatorId });
});

test("the passkey button never falls back to the password", async ({ shell, browserName }) => {
  test.skip(browserName !== "chromium", "virtual authenticators only exist in Chromium");
  await atLocalhost(shell);
  // An authenticator that WORKS and holds nothing for this installation, which
  // is the state every account is in today: there is no way to make a passkey
  // (apps/web has no credentials.create, and no action of ngc.users writes a
  // user key), so somebody who presses this button has none.
  const cdp = await shell.context().newCDPSession(shell);
  await cdp.send("WebAuthn.enable");
  const { authenticatorId } = await cdp.send("WebAuthn.addVirtualAuthenticator", {
    options: {
      protocol: "ctap2", transport: "usb", hasUserVerification: true,
      hasResidentKey: true, isUserVerified: true, automaticPresenceSimulation: true,
    },
  });

  // anna has a password key and no passkey. MEASURED at a real installation on
  // 17.10.2026: the desktop opened, the authenticator was never asked, and the
  // person was told nothing - they pressed "log in with a passkey" and were let
  // in by their password. The button has to fail instead, with the one sentence
  // every other failure gets: an answer that said "this name has no passkey"
  // would say that the name exists.
  // Taken BEFORE the attempt and compared after: the count only ever grows, so
  // this case does not care what the case before it left in the mock.
  const before = await askMock(shell, "ask=finishes");
  await shell.getByLabel("Benutzername").fill("anna");
  await shell.getByLabel("Passwort").fill(PASSWORD);
  await press(shell, shell.getByRole("button", { name: "Mit Passkey anmelden" }));

  await expect(shell.locator("#taskbar")).toBeHidden();
  await expect(shell.getByText("Anmeldung fehlgeschlagen.")).toBeVisible();
  // The mock is the one that would have had to check a proof, so it is the one
  // asked. A flag this side set about its own work would say nothing.
  expect(await askMock(shell, "ask=finishes"), "no proof was sent at all").toBe(before);

  await cdp.send("WebAuthn.removeVirtualAuthenticator", { authenticatorId });
});

test("a password login is told to switch to passkeys", async ({ shell }) => {
  await skipWithoutLogin(shell);
  await fillLogin(shell, "anna", PASSWORD);
  await expect(shell.locator("#taskbar")).toBeVisible();
  await expect(shell.getByText("Umstellen auf Passkeys empfohlen")).toBeVisible();
});

// ---- the mask of an engine without password keys -------------------------
//
// bootOver waits for shell/main.js to have FINISHED, and the heading is what
// says so: loginView is the last thing start() does, so once the heading
// stands nothing of the boot draws over #root any more.
//
// MEASURED, and it cost a red case: the three cases below build their own mask
// into #root, and on Firefox the boot was still running - loadLang, loadConfig,
// anchorFor and passwordLoginFits are four awaits - so main.js drew ITS mask
// over the one the case had just made, and the case measured the shell's
// screen instead of its own. Chromium got there first and the same case was
// green. A case whose answer depends on which engine wins a race is a case
// that measures the race.
async function bootOver(shell) {
  await expect(shell.getByRole("heading", { name: "Bei NGC anmelden" })).toBeVisible();
}

//
// THE THREE CASES ABOVE SKIP exactly where this one is interesting, so this
// one does not wait for such an engine: it builds the mask with
// passwordKeys: false itself, which is what shell/main.js hands it there
// (passwordLoginFits, measured per engine). So the promise is pinned on every
// engine that can draw a mask at all, and not only on whichever Firefox
// happens to be installed.
//
// WHAT IT PINS, and all three halves matter:
//
//   the reason is ON THE SCREEN - until this round Firefox 141 took the
//   password, ran Argon2id over it and answered "Anmeldung fehlgeschlagen.",
//   the sentence for a wrong password;
//   the route is SHUT and not merely unlabelled - a field somebody can type
//   into is a field somebody types into;
//   the way that WORKS is offered - a passkey needs no public half of a seed
//   (auth/keys.js proveWithPasskey), so refusing the whole shell would have
//   locked out somebody for whom everything works.
test("a mask without password keys says so and shuts the password route", async ({ shell }) => {
  await bootOver(shell);
  await shell.evaluate(async () => {
    const { loginView } = await import("/assets/js/auth/login.js");
    loginView(document.getElementById("root"), {
      config: { rp: { id: "ngc.example.org", origins: ["https://ngc.example.org"] } },
      passwordKeys: false,
    });
  });
  await expect(shell.locator(".note.warn"))
    .toHaveText("Dieser Browser kann die Passwort-Anmeldung nicht ausführen. "
      + "Bitte mit Passkey anmelden.");
  await expect(shell.getByLabel("Passwort")).toBeDisabled();
  await expect(shell.getByRole("button", { name: "Anmelden", exact: true })).toBeDisabled();
  await expect(shell.getByRole("button", { name: "Mit Passkey anmelden" })).toBeEnabled();
});

// The counterpart, so the case above cannot pass by drawing that sentence
// always: with password keys the mask is the mask it has always been, and the
// sentence is nowhere on it.
test("a mask with password keys carries neither the sentence nor a disabled field", async ({ shell }) => {
  await bootOver(shell);
  await shell.evaluate(async () => {
    const { loginView } = await import("/assets/js/auth/login.js");
    loginView(document.getElementById("root"), {
      config: { rp: { id: "ngc.example.org", origins: ["https://ngc.example.org"] } },
    });
  });
  await expect(shell.locator(".note.warn")).toHaveCount(0);
  await expect(shell.getByLabel("Passwort")).toBeEnabled();
  await expect(shell.getByRole("button", { name: "Anmelden", exact: true })).toBeEnabled();
});

// AND THAT ENTER DOES NOT GET PAST IT. The button is off, but a form submits
// on Enter in any field, and the password route would then be open through a
// door nobody looked at. The mock counts the logins, so this is not a matter
// of what the screen says afterwards: nothing may be asked at all.
test("Enter asks for no login where the password route is shut", async ({ shell }) => {
  await bootOver(shell);
  await shell.evaluate(async () => {
    const { loginView } = await import("/assets/js/auth/login.js");
    loginView(document.getElementById("root"), {
      config: { rp: { id: "ngc.example.org", origins: ["https://ngc.example.org"] } },
      passwordKeys: false,
    });
  });
  let asked = 0;
  shell.on("request", (r) => { if (r.url().includes("/api/v1/login")) asked += 1; });
  await shell.getByLabel("Benutzername").fill(USER_NAME);
  await shell.getByLabel("Benutzername").press("Enter");
  await shell.waitForTimeout(500);
  expect(asked, "Enter started no login").toBe(0);
  await expect(shell.locator(".note.warn")).toBeVisible();
});

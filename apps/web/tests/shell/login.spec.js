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
import { test, expect, hasSessionKeys } from "../fixtures/page.js";
import { PASSWORD, PASSKEY_USERS, TOTP_CODE } from "../fixtures/logins.js";

// The mock's test door. It is on /api/ so it travels the same origin as
// everything else, and it is the mock's alone: nothing of the shell knows it.
// MEASURED, never read off the engine's name. shell/main.js needs Ed25519 and
// X25519 before it builds anything at all, and the WebKit that Playwright
// brings has neither - so there is no mask to type into there, and the boot
// says so instead (shell/boot.spec.js is the case that pins THAT, and it runs
// everywhere). The day this WebKit gains the two, hasSessionKeys answers true
// by itself and every case below runs there with no edit here.
test.beforeEach(async ({ shell }) => {
  test.skip(!(await hasSessionKeys(shell)),
    "no Ed25519 and no X25519 in this engine's Web Crypto: the shell stops before the login");
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
  await shell.getByRole("button", { name: "Anmelden", exact: true }).click();
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
  await fillLogin(shell, "anna", "falsch");
  await expect(shell.getByRole("alert")).toHaveText("Anmeldung fehlgeschlagen.");
  // An unknown name must give exactly the same answer.
  await fillLogin(shell, "gibtesnicht", "egal");
  await expect(shell.getByRole("alert")).toHaveText("Anmeldung fehlgeschlagen.");
});

test("E_LOCKED waits the given seconds", async ({ shell }) => {
  await tellMock(shell, "lock=30");
  await fillLogin(shell, "anna", PASSWORD);
  await expect(shell.getByRole("alert")).toContainText("Zu viele Fehlversuche");
  await expect(shell.getByRole("alert")).toContainText("30 Sekunden");
  await expect(shell.getByRole("button", { name: "Anmelden", exact: true })).toBeDisabled();
});

test("TOTP is asked for when the challenge says so", async ({ shell }) => {
  await fillLogin(shell, "bernd", PASSWORD);
  await expect(shell.getByLabel("Code aus der Authenticator-App")).toBeVisible();
  await shell.getByLabel("Code aus der Authenticator-App").fill(TOTP_CODE);
  await shell.getByRole("button", { name: "Anmelden", exact: true }).click();
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
  await shell.getByRole("button", { name: "Mit Passkey anmelden" }).click();
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
  await shell.getByRole("button", { name: "Mit Passkey anmelden" }).click();
  await expect(shell.locator("#taskbar")).toBeVisible();
  // The mock is the one that had to check both, so it is the one asked. A
  // flag this side sets about its own work would say nothing.
  expect(await askMock(shell, "ask=proofs"), "both proofs, over the same bytes")
    .toBe("passkey+password");

  await cdp.send("WebAuthn.removeVirtualAuthenticator", { authenticatorId });
});

test("a password login is told to switch to passkeys", async ({ shell }) => {
  await fillLogin(shell, "anna", PASSWORD);
  await expect(shell.locator("#taskbar")).toBeVisible();
  await expect(shell.getByText("Umstellen auf Passkeys empfohlen")).toBeVisible();
});

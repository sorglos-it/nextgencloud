import { test, expect, shellGap, seedKeyGap, passkeyAskGap } from "../fixtures/page.js";

// THE THREE ENGINES, AND THE ROUTE EACH OF THEM REALLY HAS.
//
// Why this file is its own: shell/boot.spec.js pins the three endings a boot
// may have, and it pins them against the engine it happens to run in. That
// leaves the one case nobody could reach - an engine with the curves, without
// the password seed step, and WITHOUT ANY WAY TO ASK AN AUTHENTICATOR - and
// that is the case the mask used to lie in. Firefox 141 has the first two
// properties and passkeys switched on, so its boot ends in a mask whose one
// enabled button works; turn the pref off and the same mask offers a button
// that cannot work at all.
//
// MEASURED on 18.10.2026, and the reason this file exists:
//
//   chromium 140  curves yes, seed step yes, navigator.credentials.get yes
//   firefox  141  curves yes, seed step NO (exportKey("jwk") OperationError),
//                 navigator.credentials.get yes - and undefined with the one
//                 pref security.webauth.webauthn = false
//   webkit    26  curves NO (NotSupportedError at generateKey), and
//                 window.PublicKeyCredential is an OBJECT while
//                 navigator.credentials is undefined
//
// The last line is why shell/main.js passkeyFits() asks for
// navigator.credentials.get and not for PublicKeyCredential: on WebKit the
// second question answers yes about an engine that cannot be asked for an
// assertion. It is pinned below rather than left in a comment.

// An engine that cannot be asked for an assertion, made out of whichever
// engine is running: the properties the shell reads are taken off the page
// before any of its code runs. This makes the engine LESS able, which is the
// state under test - it switches no check of the shell off.
async function withoutAuthenticator(page) {
  await page.addInitScript(() => {
    try { delete Navigator.prototype.credentials; } catch { /* own property below */ }
    Object.defineProperty(navigator, "credentials", { value: undefined, configurable: true });
    Object.defineProperty(window, "PublicKeyCredential", { value: undefined, configurable: true });
  });
}

// An engine without the password seed step, out of whichever engine is
// running: exportKey("jwk") of an Ed25519 key refuses, which is byte for byte
// what Firefox 141 does (OperationError, measured). Everything else of the
// engine stays as it is - this is the one call that is missing there.
async function withoutSeedStep(page) {
  await page.addInitScript(() => {
    const real = crypto.subtle.exportKey.bind(crypto.subtle);
    crypto.subtle.exportKey = function (format, key) {
      if (format === "jwk" && key?.algorithm?.name === "Ed25519") {
        return Promise.reject(new DOMException("measurement: no jwk of an Ed25519 key", "OperationError"));
      }
      return real(format, key);
    };
  });
}

// The gate itself, and it is the one that was missing. Both routes shut, and
// the person is told BEFORE a mask takes anything - which is the whole point:
// the sentence has to come before the password, not after Argon2id.
test("an engine with no route at all says so before anything is typed", async ({ shell }) => {
  test.skip((await shellGap(shell)) !== "", "this engine does not get past browserFits at all");
  await withoutSeedStep(shell);
  await withoutAuthenticator(shell);
  await shell.reload();

  await expect(shell.locator(".msg")).toContainText(
    "In diesem Browser ist keine Anmeldung möglich");
  // It names what to do about THIS blocker, which is not the one the other
  // sentence names: passkeys are off here, the browser is not too old.
  await expect(shell.locator(".msg")).toContainText("Passkeys");
  // And nothing that takes a secret is on the page.
  await expect(shell.locator("#taskbar")).toHaveCount(0);
  await expect(shell.getByLabel("Passwort")).toHaveCount(0);
  await expect(shell.getByRole("button", { name: "Mit Passkey anmelden" })).toHaveCount(0);
});

// The other half of the same gate: where the engine CAN be asked, the route
// stands. Without this case the gate above could be satisfied by a shell that
// refuses every engine, which is the mistake a guard makes when it is too
// strict - and a skipped way in looks like a finished one.
test("an engine that can be asked for an assertion keeps the passkey route", async ({ shell }) => {
  test.skip((await shellGap(shell)) !== "", "this engine does not get past browserFits at all");
  test.skip(!(await shell.evaluate(() => typeof navigator?.credentials?.get === "function")),
    "this engine has no way to ask an authenticator, so there is no route to keep");
  await withoutSeedStep(shell);
  await shell.reload();

  await expect(shell.getByRole("heading", { name: "Bei NGC anmelden" })).toBeVisible();
  await expect(shell.getByRole("button", { name: "Mit Passkey anmelden" })).toBeEnabled();
  await expect(shell.getByLabel("Passwort")).toBeDisabled();
});

// Where the password route works, a missing authenticator shuts nothing but
// the dead button. This is the case that keeps the new gate from locking
// anybody out: it is the lesson fixtures/page.js paid for twice.
test("without an authenticator the password route still gets in", async ({ shell }) => {
  await test.step("the engine must be able to do a password login at all", async () => {
    test.skip((await seedKeyGap(shell)) !== "", "this engine has no password seed step");
  });
  await withoutAuthenticator(shell);
  await shell.reload();

  await expect(shell.getByRole("heading", { name: "Bei NGC anmelden" })).toBeVisible();
  await expect(shell.getByLabel("Passwort")).toBeEnabled();
  await expect(shell.getByRole("button", { name: "Anmelden", exact: true })).toBeEnabled();
  // The one thing that goes: an offer nothing can serve.
  await expect(shell.getByRole("button", { name: "Mit Passkey anmelden" })).toHaveCount(0);
  await expect(shell.locator(".note.warn")).toHaveCount(0);
});

// THE QUESTION IS ASKED ABOUT THE CALL THE PATH MAKES.
//
// auth/keys.js proveWithPasskey calls navigator.credentials.get, so that is
// what shell/main.js passkeyFits() holds up. Measured on WebKit 26:
// window.PublicKeyCredential is an object there and navigator.credentials is
// undefined - a gate asking the first question would say yes about an engine
// that cannot be asked for an assertion. The engine below is that engine,
// built out of whichever one is running, and the gate has to see through it.
test("a PublicKeyCredential without navigator.credentials is no route", async ({ shell }) => {
  test.skip((await shellGap(shell)) !== "", "this engine does not get past browserFits at all");
  await shell.addInitScript(() => {
    try { delete Navigator.prototype.credentials; } catch { /* own property below */ }
    Object.defineProperty(navigator, "credentials", { value: undefined, configurable: true });
    // Present, and an object - exactly WebKit 26's shape.
    Object.defineProperty(window, "PublicKeyCredential", { value: {}, configurable: true });
  });
  await withoutSeedStep(shell);
  await shell.reload();

  await expect(shell.locator(".msg")).toContainText(
    "In diesem Browser ist keine Anmeldung möglich");
  await expect(shell.getByRole("button", { name: "Mit Passkey anmelden" })).toHaveCount(0);
});

// THE TWO SENTENCES MUST NOT CONTRADICT EACH OTHER.
//
// MEASURED on 18.10.2026 and the second finding of this round: the too-old
// message read "Bitte Chrome 137, Firefox 130 oder Safari 17.4 oder neuer
// benutzen." - and it is WebKit 26 that reads it, because WebKit has no
// Ed25519 at all. Somebody who followed it landed on Firefox 141, which this
// same shell then tells cannot do a password login. One screen recommended
// what the next one refuses.
//
// The browsers that cannot are DATA and not prose (shell/lang.js
// NO_PASSWORD_LOGIN), because parsing them out of a sentence that also names
// the remedy cannot be done - the first attempt at this case read "Chrome" out
// of the password sentence, where it stands as the way IN. So: each name on
// that list must be named in the sentence that warns, and in none of the five
// languages in the sentence that advises.
test("no language recommends a browser the shell says cannot log in", async ({ shell }) => {
  const seen = await shell.evaluate(async () => {
    const { LANGS, NO_PASSWORD_LOGIN } = await import("/assets/js/shell/lang.js");
    const out = { names: [...NO_PASSWORD_LOGIN], rows: [] };
    for (const tag of LANGS) {
      const file = await (await fetch(`/assets/lang/${tag}.json`)).json();
      out.rows.push({
        tag,
        warn: file["boot.noroute.password"],
        advise: file["boot.tooold"],
      });
    }
    return out;
  });

  // The list is this case's subject; an empty one would make it pass by
  // looking at nothing.
  expect(seen.names.length, "the shell names at least one such browser").toBeGreaterThan(0);
  expect(seen.rows.length, "all five languages were read").toBe(5);
  for (const row of seen.rows) {
    expect(typeof row.warn, `${row.tag} holds the warning sentence`).toBe("string");
    expect(typeof row.advise, `${row.tag} holds the advice`).toBe("string");
    // The advice has to name a browser at all, or there is nothing to check
    // and nothing for the person either.
    expect(row.advise, `${row.tag}: the advice names a browser`).toMatch(/Chrome|Safari/);
    for (const name of seen.names) {
      expect(row.warn, `${row.tag}: the warning names ${name} as unable`).toContain(name);
      expect(row.advise, `${row.tag}: ${name} cannot log in and is offered as the remedy`)
        .not.toContain(name);
    }
  }
});

// An engine without the two CFRG curves, made out of whichever engine is
// running: generateKey and importKey refuse Ed25519 and X25519 with the error
// WebKit 26 answers, measured in shell/krypto-tafel.spec.js. This makes the
// engine LESS able - it switches no check of the shell off - and it is what
// lets an engine that HAS the curves read the sentence the ones without them
// get. Without it this case could only ever run in webkit, and a sentence
// nobody can read in two languages is not pinned.
async function withoutCFRGCurves(page) {
  await page.addInitScript(() => {
    const gen = crypto.subtle.generateKey.bind(crypto.subtle);
    const imp = crypto.subtle.importKey.bind(crypto.subtle);
    const gone = (name) => name === "Ed25519" || name === "X25519";
    const no = () => Promise.reject(
      new DOMException("measurement: no CFRG curves here", "NotSupportedError"));
    crypto.subtle.generateKey = function (algo, ...rest) {
      return gone(algo?.name || algo) ? no() : gen(algo, ...rest);
    };
    crypto.subtle.importKey = function (format, data, algo, ...rest) {
      return gone(algo?.name || algo) ? no() : imp(format, data, algo, ...rest);
    };
  });
}

// WHAT THE PERSON WHO IS TURNED AWAY ACTUALLY READS, off the screen and not
// out of the file.
//
// The case above reads the five sentences with fetch, which pins what is
// WRITTEN. It cannot see whether the shell renders them, in which language,
// or at all - a key missing from a file falls back to English, and a release
// without assets/lang/ falls back to BUILT_IN (shell/lang.js, held key for
// key by shell/busy.spec.js). So this one presses the gate shut and reads the
// screen.
//
// THE SENTENCE MUST NAME THE CAPABILITY AND NOT A VERSION NUMBER. It used to
// read "Bitte Chrome 137 oder Safari 17.4 oder neuer benutzen." and that
// advice is measured to be misleading: Safari 17.4 HAS the CFRG curves, and
// the engine reading the sentence is one that lacks them, so the number names
// a remedy the reader may already have. A version number ages, a capability
// does not - hence the digit check below, which is the whole point of this
// case.
//
// WHICH OF THE TWO ASSERTIONS ACTUALLY BITES, measured by writing "Chrome 137
// oder Safari 17.4" back into de.json and running the case: it went red on
// toHaveText, not on the digit. The exact sentence is the first line of
// defence, and the digit check only fires where somebody changes the sentence
// AND brings the expectation here along with it - which is the realistic path,
// because that is what a person editing a sentence does. So it is two checks
// for two different mistakes, not one check twice, and neither of them is
// dead weight.
// ALL THREE SENTENCES, because it is one defect in three places. Searched by
// the NUMBER and not by the wording: `grep '"[a-z.]*": "[^"]*[0-9]'` over the
// five files and BUILT_IN answered exactly these three keys and nothing else,
// and it answers nothing now. A version number in a text for people is the
// pattern; this wording was only where it was found first.
// WHY THIS IS A WRAPPED fetch AND NOT page.route, measured: with
// page.route(/config\.json/) and route.fetch() the run HUNG - 10 minutes and
// no case after this one. The web server of the suite is PHP's built-in one,
// which serves ONE request at a time: the handler holds the browser's request
// open while asking for the same file again, and the server cannot answer the
// second until the first is done. Deadlock, and it looks like a slow test.
// Wrapping fetch inside the page keeps it to one request after the other.
// An installation whose web controller is not paired: shell/main.js takes the
// anchor from config.anchor and, where that is empty, from what the initial
// setup kept in IndexedDB (shell/trustlist.js anchorFor), so BOTH have to go
// or the kept one answers and the case measures nothing.
//
// MEASURED, AND IT COST A RUN: the first attempt emptied config.rp.id instead,
// to make the installation half of the passkey route say no. The configuration
// contract refuses an empty id, so the screen read "NGC ist nicht
// eingerichtet." - the sentence of a missing installation - and four cases
// failed against the EARLIER WALL. The comment here used to claim that could
// not happen. It did.
async function withoutTrustAnchor(page) {
  await page.addInitScript(() => {
    try { indexedDB.deleteDatabase("ngc"); } catch { /* nothing kept here */ }
    const real = window.fetch.bind(window);
    window.fetch = async function (input, init) {
      const url = typeof input === "string" ? input : input?.url ?? "";
      const res = await real(input, init);
      if (!url.includes("config.json") || !res.ok) return res;
      const config = JSON.parse(await res.text());
      delete config.anchor;
      return new Response(JSON.stringify(config), {
        status: res.status,
        headers: { "content-type": "application/json" },
      });
    };
  });
}

const TURNED_AWAY = [
  {
    key: "boot.tooold",
    // The curves go, which is the gate's own question (generateKey).
    reach: async (page) => { await withoutCFRGCurves(page); },
    everywhere: true,
    de: "Dieser Browser kann die Schlüssel nicht erzeugen, die NGC für eine Anmeldung braucht. Bitte ein aktuelles Chrome oder Safari benutzen.",
    en: "This browser cannot create the keys NGC needs for a login. Please use a current Chrome or Safari.",
  },
  {
    key: "boot.noroute.password",
    // No seed step and no anchor, so the shell takes the branch that sends the
    // person to another browser - the passkey route exists here, it is the
    // password one that is missing, and that is what the sentence is about.
    reach: async (page) => { await withoutSeedStep(page); await withoutTrustAnchor(page); },
    everywhere: false,
    // This sentence lives on the branch where the passkey route EXISTS, so it
    // needs an engine that can be asked for one.
    needsPasskeyRoute: true,
    de: "Dieser Browser kann die Passwort-Anmeldung von NGC nicht ausführen: er gibt den öffentlichen Teil eines Passwort-Schlüssels nicht heraus. Bitte ein aktuelles Chrome oder Safari benutzen. Firefox kann es derzeit nicht.",
    en: "This browser cannot do NGC's password login: it does not hand out the public half of a password key. Please use a current Chrome or Safari. Firefox cannot do it at present.",
  },
  {
    key: "boot.noroute.passkeysoff",
    // No seed step and nothing to ask for an assertion, while the
    // installation does have a party: passkeys are off in THIS browser.
    reach: async (page) => { await withoutSeedStep(page); await withoutAuthenticator(page); },
    everywhere: false,
    de: "In diesem Browser ist keine Anmeldung möglich: die Passwort-Anmeldung kann er nicht, und Passkeys sind hier abgeschaltet. Bitte Passkeys in den Browser-Einstellungen einschalten oder ein aktuelles Chrome oder Safari benutzen.",
    en: "No login is possible in this browser: it cannot do the password login, and passkeys are switched off here. Please switch passkeys on in the browser settings, or use a current Chrome or Safari.",
  },
];

for (const [locale, tag] of [["de-DE", "de"], ["en-US", "en"]]) {
  test.describe(`turned away in ${locale}`, () => {
    test.use({ locale });

    for (const screen of TURNED_AWAY) {
      test(`${screen.key} names the missing capability and no version number`, async ({ shell }) => {
        // THE EARLIER WALL. Only the first sentence can be reached in an
        // engine that fails browserFits; the other two live past that gate, so
        // in webkit this case would read the too-old screen and call it theirs
        // (messen.md: a case turned away before the intended line measures the
        // earlier wall). The skip says which wall, not just "skipped".
        if (!screen.everywhere) {
          const gap = await shellGap(shell);
          test.skip(gap !== "", `this engine stops at browserFits (${gap}), so ${screen.key} is not reachable here`);
        }
        // THE SECOND WALL, and it is the one this case walked into on
        // Playwright's WebKit on Linux (run 42215156995). shell/main.js picks
        // between the two sentences on hasParty && !canAsk: where
        // navigator.credentials.get is missing, canAsk is false and the
        // passkeysoff sentence is the ONLY one reachable, whatever the anchor
        // does. boot.noroute.password read that sentence and called it a wrong
        // text; the text was right and the case was on the wrong screen.
        if (screen.needsPasskeyRoute) {
          const ask = await passkeyAskGap(shell);
          test.skip(ask !== "", `this engine always lands in boot.noroute.passkeysoff - ${ask}`);
        }
        await screen.reach(shell);
        await shell.reload();

        // The whole sentence, not a fragment: a fragment would still pass with
        // a version number appended behind it.
        const msg = shell.locator(".msg");
        await expect(msg).toHaveText(screen[tag]);

        const text = (await msg.textContent()) ?? "";
        expect(text, "the advice carries no version number").not.toMatch(/[0-9]/);
        // And it still says where to go, or it names a gap and no way out.
        expect(text, "the advice still names a browser").toMatch(/Chrome|Safari/);
      });
    }
  });
}

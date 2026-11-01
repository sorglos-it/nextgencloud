// WHAT THIS ENGINE CAN REALLY DO, one cell at a time, and whether a person
// gets in. A measurement, not a check: nothing here fails a run.
//
// WHY IT IS NOT fixtures/page.js sessionKeyGaps. That function is a CHAIN and
// stops at the first step that falls, which is exactly right for a skip - a
// case wants the name of the one thing that is missing. It is wrong for the
// question "what can this engine do", because every cell behind the first "no"
// stays unmeasured and then gets read as a "no" by whoever sees the table. On
// webkit four of its six gaps are hidden that way. So: every cell below is
// measured on its own, with its own fresh key material, and NOTHING stops
// early.
//
// THE PROBE CHECKS ITSELF FIRST. A table of nothing but "no" is what a broken
// probe looks like, and it looks exactly like a finding - so two cells that
// every engine since 2017 has (AES-GCM and SHA-256) are measured as a control.
// If the control falls, the run says so and names NO figures: a probe whose
// own preparation did not work measures something else.
//
// RUN IT, and the exit code is the last thing on the line:
//
//   cd apps/web/tests
//   npx playwright test shell/krypto-tafel.spec.js > ..\..\..\tafel.txt 2>&1
//   echo $LASTEXITCODE
//
// Output goes UNFILTERED into a file. A `| tail` would kill the run with
// SIGPIPE after a handful of lines and leave something that looks like a
// partial result (messen.md).
//
// It is a MEASURING TOOL and not part of the suite's guarantees: it makes no
// assertion about any engine, so it cannot go red on an engine that changed.
// What it prints is the finding. Delete it when the table in
// docs/krypto-grenzen-browser.md is filled and nobody needs to measure again.

import { test, expect, standInOn } from "../fixtures/page.js";
import { USER_NAME, PASSWORD, PASSKEY_NAME } from "../fixtures/logins.js";
import { putSoftwareAuthenticator } from "../fixtures/passkey.js";

// Minutes, not the default: three engines each walk two dozen cells and then
// run two logins, and a slow machine must not turn a measurement into a
// timeout that reads like a missing capability.
test.describe.configure({ timeout: 180_000 });

// ---- part A: the capability table -----------------------------------------

// Every cell is its own function in the page, with its own key material, and
// every one is reached whatever the ones before it answered. The answer is
// either "" (it can) or the error's name - never a bare false, because "no"
// without a reason is what sent this whole investigation down the wrong road.
async function capabilities(shell) {
  return shell.evaluate(async () => {
    // PKCS#8 around a bare 32-byte CFRG seed, and SPKI around a bare public
    // key: byte for byte the prefixes contracts/proof.js and contracts/noise.js
    // build their own documents from, so a cell here measures the very call the
    // production module makes.
    const PKCS8 = {
      Ed25519: [0x30, 0x2e, 0x02, 0x01, 0x00, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x70, 0x04, 0x22, 0x04, 0x20],
      X25519: [0x30, 0x2e, 0x02, 0x01, 0x00, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x6e, 0x04, 0x22, 0x04, 0x20],
    };
    const SPKI = {
      Ed25519: [0x30, 0x2a, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x70, 0x03, 0x21, 0x00],
      X25519: [0x30, 0x2a, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x6e, 0x03, 0x21, 0x00],
    };
    const wrap = (prefix, raw) => {
      const out = new Uint8Array(prefix.length + raw.length);
      out.set(prefix, 0);
      out.set(raw, prefix.length);
      return out;
    };
    const message = new TextEncoder().encode("ngc: capability probe");
    const uses = (curve) => (curve === "Ed25519" ? ["sign", "verify"] : ["deriveBits"]);

    // One cell. It answers "" when the call went through and the name of the
    // error otherwise; a call that returns something unusable answers a
    // sentence of its own, because "it did not throw" is not "it worked".
    const cell = async (step) => {
      try {
        const why = await step();
        return why === true ? "" : (why || "answered no");
      } catch (err) {
        return (err && err.name) || "threw";
      }
    };

    const out = {};

    // THE CONTROL. Two things every engine has had for years. If either of
    // these falls, the probe is broken and the table means nothing.
    out["control AES-GCM generateKey"] = await cell(async () => {
      const k = await crypto.subtle.generateKey({ name: "AES-GCM", length: 256 }, true, ["encrypt"]);
      return !!k;
    });
    out["control SHA-256 digest"] = await cell(async () =>
      (await crypto.subtle.digest("SHA-256", message)).byteLength === 32);
    out["control secure context"] = await cell(async () => window.isSecureContext === true);

    for (const curve of ["Ed25519", "X25519"]) {
      // Fresh key material per curve, made with generateKey - and if that
      // falls, the cells that need a key say so with their own error instead
      // of being skipped. A skipped cell is the thing this file exists to
      // avoid.
      let pair = null;
      out[`${curve} generateKey`] = await cell(async () => {
        pair = await crypto.subtle.generateKey({ name: curve }, true, uses(curve));
        return !!(pair && pair.privateKey && pair.publicKey);
      });

      // A seed is any 32 bytes. This one is the key of nothing: made here,
      // used here, dropped here.
      const seed = crypto.getRandomValues(new Uint8Array(32));
      let fromDoc = null;
      out[`${curve} importKey pkcs8`] = await cell(async () => {
        fromDoc = await crypto.subtle.importKey(
          "pkcs8", wrap(PKCS8[curve], seed), { name: curve }, true,
          curve === "Ed25519" ? ["sign"] : ["deriveBits"]);
        return !!fromDoc;
      });

      // THE FIREFOX CELL. The public half of a key that came in as a
      // document - the one call that is missing there, and the one
      // proof.publicFromSeed and noise.publicOf stand on.
      out[`${curve} exportKey jwk OF AN IMPORTED KEY`] = await cell(async () => {
        const jwk = await crypto.subtle.exportKey("jwk", fromDoc);
        return typeof jwk.x === "string" && jwk.x.length > 0 ? true : "no x in the jwk";
      });
      // The same call on a key the engine made ITSELF. Firefox does this one -
      // that difference is the whole finding, and a table that measured only
      // one of the two would hide it.
      out[`${curve} exportKey jwk OF A GENERATED KEY`] = await cell(async () => {
        const jwk = await crypto.subtle.exportKey("jwk", pair.privateKey);
        return typeof jwk.x === "string" && jwk.x.length > 0 ? true : "no x in the jwk";
      });
      out[`${curve} exportKey pkcs8`] = await cell(async () =>
        (await crypto.subtle.exportKey("pkcs8", fromDoc)).byteLength > 0);
      out[`${curve} exportKey raw of the public half`] = await cell(async () =>
        (new Uint8Array(await crypto.subtle.exportKey("raw", pair.publicKey))).length === 32);
      out[`${curve} exportKey spki`] = await cell(async () =>
        (await crypto.subtle.exportKey("spki", pair.publicKey)).byteLength > 0);

      out[`${curve} importKey raw`] = await cell(async () => {
        const raw = new Uint8Array(await crypto.subtle.exportKey("raw", pair.publicKey));
        return !!(await crypto.subtle.importKey("raw", raw, { name: curve }, false,
          curve === "Ed25519" ? ["verify"] : []));
      });
      out[`${curve} importKey spki`] = await cell(async () => {
        const raw = new Uint8Array(await crypto.subtle.exportKey("raw", pair.publicKey));
        return !!(await crypto.subtle.importKey("spki", wrap(SPKI[curve], raw), { name: curve }, false,
          curve === "Ed25519" ? ["verify"] : []));
      });

      if (curve === "Ed25519") {
        let sig = null;
        out["Ed25519 sign"] = await cell(async () => {
          sig = await crypto.subtle.sign({ name: "Ed25519" }, pair.privateKey, message);
          return sig.byteLength === 64;
        });
        // verify does the WORK: a signature that checks true. An engine whose
        // verify answers true to everything would pass a "does it throw"
        // question, and contracts/sign.js would then believe every envelope.
        out["Ed25519 verify a RIGHT signature"] = await cell(async () =>
          (await crypto.subtle.verify({ name: "Ed25519" }, pair.publicKey, sig, message)) === true);
        // And the other way round, which is the half that matters for
        // security: a wrong signature has to come out FALSE. An engine that
        // says true here can tell no right signature from a wrong one, and
        // every trust chain it checks is worthless.
        out["Ed25519 refuse a WRONG signature"] = await cell(async () => {
          const bent = new Uint8Array(sig);
          bent[0] ^= 1;
          return (await crypto.subtle.verify({ name: "Ed25519" }, pair.publicKey, bent, message)) === false
            ? true : "it accepted a bent signature";
        });
      } else {
        // deriveBits against the key that came BACK from the exported bytes,
        // never against the CryptoKey those bytes came from: that is what a
        // session does, and an exportKey("raw") that lies is invisible the
        // other way (measured, fixtures/page.js:268).
        out["X25519 deriveBits, both sides equal"] = await cell(async () => {
          const peer = await crypto.subtle.generateKey({ name: "X25519" }, true, ["deriveBits"]);
          const mineRaw = new Uint8Array(await crypto.subtle.exportKey("raw", pair.publicKey));
          const peerRaw = new Uint8Array(await crypto.subtle.exportKey("raw", peer.publicKey));
          const mineBack = await crypto.subtle.importKey("raw", mineRaw, { name: "X25519" }, false, []);
          const peerBack = await crypto.subtle.importKey("raw", peerRaw, { name: "X25519" }, false, []);
          const a = new Uint8Array(await crypto.subtle.deriveBits(
            { name: "X25519", public: peerBack }, pair.privateKey, 256));
          const b = new Uint8Array(await crypto.subtle.deriveBits(
            { name: "X25519", public: mineBack }, peer.privateKey, 256));
          return a.length === 32 && a.every((v, i) => v === b[i]) ? true : "the two sides differ";
        });
      }
      seed.fill(0);
    }

    // P-256, because it is the curve the PASSKEY route stands on and the one
    // webkit has. A table without it would say "webkit can no crypto", which
    // is false and is the sentence this probe exists to settle.
    let p256 = null;
    out["P-256 generateKey"] = await cell(async () => {
      p256 = await crypto.subtle.generateKey(
        { name: "ECDSA", namedCurve: "P-256" }, true, ["sign", "verify"]);
      return !!p256;
    });
    out["P-256 exportKey jwk OF A GENERATED KEY"] = await cell(async () => {
      const jwk = await crypto.subtle.exportKey("jwk", p256.privateKey);
      return typeof jwk.x === "string" && typeof jwk.y === "string";
    });
    out["P-256 sign and verify"] = await cell(async () => {
      const sig = await crypto.subtle.sign({ name: "ECDSA", hash: "SHA-256" }, p256.privateKey, message);
      return (await crypto.subtle.verify(
        { name: "ECDSA", hash: "SHA-256" }, p256.publicKey, sig, message)) === true;
    });

    return out;
  });
}

// ---- part B: does a person get in -----------------------------------------

// The three gates of shell/main.js, asked of the PRODUCTION module and not
// rebuilt here. A copy of those three functions in this file would measure the
// copy: the day main.js changes, the copy still answers about yesterday.
async function gates(shell) {
  return shell.evaluate(async () => {
    const out = {};
    try {
      const m = await import("/assets/js/shell/main.js");
      out.browserFits = await m.browserFits();
      out.passwordLoginFits = await m.passwordLoginFits();
      out.passkeyFits = m.passkeyFits();
    } catch (err) {
      out.error = (err && err.message) || "import failed";
    }
    return out;
  });
}

// WHAT THE PERSON READS, not what the server sent. The lesson of messen.md
// "ein verschluckter Fehler wird zur Falschauskunft": the server's sentence
// does not reach the screen if the mask has a table of its own, and the
// screen is where a human lives.
async function screen(shell) {
  return shell.evaluate(() => {
    const box = document.querySelector("form.loginbox");
    const msg = document.querySelector("#root .msg");
    const buttons = box === null ? [] : [...box.querySelectorAll("button")].map(
      (b) => `${b.textContent}${b.disabled ? " [AUS]" : " [AN]"}`);
    const field = box === null ? null : box.querySelector("input[type=password]");
    return {
      where: document.querySelector("#taskbar") !== null ? "desktop"
        : box !== null ? "mask"
          : msg !== null ? "message" : "nothing",
      message: msg === null ? "" : msg.textContent,
      buttons,
      passwordField: field === null ? "not there" : (field.disabled ? "AUS" : "AN"),
      notes: box === null ? [] : [...box.querySelectorAll(".warn, [role=alert]")]
        .map((n) => n.textContent).filter((s) => s !== ""),
      // THE SENTENCE THE PERSON READS when an attempt fails, and it is the
      // whole point of the measurement: runs 3 and 4 both ended in "mask said
      // something" and neither printed WHAT it said, because this was read in
      // a second evaluate a moment later, by which time the mask had cleared
      // it. One evaluate, one state - a value read a tick after the state it
      // belongs to is a different state.
      alert: box === null ? "" : (box.querySelector('[role="alert"]')?.textContent || ""),
    };
  });
}

// THE BOOT IS OVER, and reading before it is why the first run of this file
// measured nothing. screen() answers "nothing" while the shell is still
// starting - and "nothing" in all three engines, chromium included, looked
// exactly like a finding. So: wait for a state, never read straight after a
// navigation.
async function settled(shell) {
  await expect.poll(async () => (await screen(shell)).where,
    { message: "the boot has ended in one of its endings", timeout: 60_000 })
    .not.toBe("nothing");
  return screen(shell);
}

// THE ATTEMPT IS OVER WHEN IT HAS AN ENDING, and the ending is one of three:
// a desktop, a sentence the person can read, or a shell message. Nothing else
// counts, and that is the whole repair.
//
// WHAT IT USED TO DO AND WHY THAT WAS WRONG. It copied the measure of
// shell/login.spec.js loginState(): "working" means every button of the mask
// is disabled and no alert has text. That holds in chromium, where both
// buttons are live and go dead together while the attempt runs - and it is
// FALSE in firefox, where "Anmelden" is disabled for good because the engine
// makes no password key. There `every(b => b.disabled)` is false while the
// passkey button is still live, so the state read "idle" IMMEDIATELY after the
// click and the probe took its figures mid-attempt. Measured: the firefox case
// ran in 1.3 s, and the screen it printed showed the passkey button mid-press,
// which reads as "the login did not go through".
//
// THE CONTROL DID NOT CATCH IT, and that is the lesson worth more than the
// bug: chromium reached a desktop on both routes, so the control was green
// while the wait condition was broken for exactly the variant chromium does
// not have - a permanently disabled button. A control only covers the
// assumptions the control's own variant shares. Where a condition depends on
// the variant, it has to be written so it does not.
// ONE state function, and screen() below reads the same one. The second
// repair of this helper, and the reason is that the first one did not work:
// after it, firefox still finished in 1.3 s with the passkey button caught
// mid-press. The two functions disagreed about PRECEDENCE - this one asked for
// "#root .msg" BEFORE the mask, screen() asked for the mask first - so a .msg
// node standing beside the mask ended the wait at the first tick while
// screen() still reported a mask. Two readers of one DOM with two orders are
// the same trap as two files that almost share a name.
//
// It also RETURNS the ending it saw, and the caller prints it. A wait that
// stops for a reason nobody records is why this took three runs: twice the
// figure was wrong and twice the output said nothing about why the wait ended.
// THE ENDING IS A CHANGE, not a state - and getting that wrong cost three
// runs in a row, each time the same mistake wearing a different coat.
//
// Run 3: "the attempt runs while every button is disabled" - false in firefox,
// where "Anmelden" is disabled for good.
// Run 4: "an alert with text is an ending" - false in firefox, where the note
// "Dieser Browser kann die Passwort-Anmeldung nicht ausführen" carries
// role="alert" and STANDS THERE FROM THE START, before anything is clicked.
//
// Both conditions describe a state that already held BEFORE the event, so
// neither could ever report the event. The repair is to stop asking "what does
// the screen look like" and ask "what has changed since the click": a desktop
// that was not there, a sentence that differs from the one already standing,
// or a shell message. That question does not depend on the engine, which is
// what the previous two did and why the control in chromium never caught them.
function endingOf(before, now) {
  if (now.where === "desktop") return "desktop";
  if (now.where === "message") return `shell: "${now.message}"`;
  if (now.where === "mask" && now.alert !== before.alert) return `mask: "${now.alert}"`;
  return "working";
}

// before: the screen as it stood immediately before the button was pressed.
async function attemptOver(shell, before) {
  await expect.poll(async () => endingOf(before, await screen(shell)),
    { message: "the login attempt ended in something that was not there before", timeout: 60_000 })
    .not.toBe("working");
  return endingOf(before, await screen(shell));
}

// ---- the two cases --------------------------------------------------------

test("die Fähigkeiten dieser Engine, Zelle für Zelle", async ({ shell }, info) => {
  const table = await capabilities(shell);

  // THE PROBE'S OWN PREPARATION, before any figure is read out. A table of
  // "no" from a broken probe is indistinguishable from a finding, so the
  // control decides whether there is a finding at all.
  const broken = Object.entries(table)
    .filter(([name, why]) => name.startsWith("control ") && why !== "");
  if (broken.length > 0) {
    console.log(`TAFEL ${info.project.name}: SONDE KAPUTT, KEINE ZAHLEN - ` +
      broken.map(([n, w]) => `${n}: ${w}`).join("; "));
    return;
  }

  const can = Object.entries(table).filter(([n, w]) => !n.startsWith("control ") && w === "");
  const cannot = Object.entries(table).filter(([n, w]) => !n.startsWith("control ") && w !== "");
  console.log(`TAFEL ${info.project.name}: ${can.length} ja, ${cannot.length} nein`);
  for (const [name, why] of Object.entries(table)) {
    console.log(`TAFEL ${info.project.name} | ${why === "" ? "JA  " : "NEIN"} | ${name}${why === "" ? "" : " | " + why}`);
  }
  console.log(`TAFELJSON ${info.project.name} ${JSON.stringify(table)}`);
});

// THE STAND-IN, for the same reason shell/login.spec.js sets it in a
// beforeEach: since task 17b the shell takes the REAL session, and the mock
// API answers five actions where a desktop needs two dozen. Without it a
// login fails in the ENGINE THAT CAN DO EVERYTHING - measured, first run of
// this file: chromium reached "NGC konnte nicht geladen werden." and that was
// my fixture, not NGC. It is named here and in every line this file prints,
// because a figure from a run with a stand-in is a figure about a stand-in.
test.beforeEach(async ({ shell }) => {
  await standInOn(shell);
});

test("kommt eine Anmeldung durch - Passwort und Passkey getrennt", async ({ shell }, info) => {
  const e = info.project.name;
  const g = await gates(shell);
  console.log(`ANMELDUNG ${e} | Tore | ${JSON.stringify(g)}`);

  // The screen a person meets, read AFTER the boot has ended. The stand-in is
  // set, so this is the mask of a page whose session is faked - which changes
  // nothing about which of the three endings the boot takes, because all three
  // gates are measured before any session exists.
  const start = await settled(shell);
  console.log(`ANMELDUNG ${e} | Schirm beim Start | ${JSON.stringify(start)}`);

  // 1. THE PASSWORD ROUTE. anna has a password key and no passkey.
  //
  // It presses the button only if the button can be pressed: a disabled
  // button is itself the answer, and clicking it would measure playwright's
  // patience instead of the shell.
  let pwAttempted = false, pwEnding = "nicht versucht";
  if (start.where === "mask" && start.passwordField === "AN") {
    await shell.getByLabel("Benutzername").fill(USER_NAME);
    await shell.getByLabel("Passwort").fill(PASSWORD);
    const go = shell.getByRole("button", { name: "Anmelden", exact: true });
    if (!(await go.isDisabled())) {
      pwAttempted = true;
      const before = await screen(shell);
      await go.click();
      pwEnding = await attemptOver(shell, before);
    }
  }
  const afterPw = await screen(shell);
  console.log(`ANMELDUNG ${e} | Passwort | versucht=${pwAttempted} | Ende=${pwEnding} | ${JSON.stringify(afterPw)}`);

  // THE CONTROL, and its absence is what made the first run of this file
  // worthless. An engine whose three gates all answer true can do everything
  // NGC asks of a browser, so it MUST reach a desktop. If it does not, the
  // fixture is broken - and then the other two engines were read against
  // nothing, which is how "firefox cannot log in" would get written down about
  // a mistake of mine. Same discipline as the AES-GCM control in part A, one
  // screen further down, where it was missing the first time.
  // IT NAMES WHAT IT SAW, and not just "broken": the whole screen, the
  // buttons and the notes. A control that only says "broken" sends the next
  // round off with no clue, and a measuring slot costs somebody else their
  // place in the queue.
  const allGatesOpen = g.browserFits === true && g.passwordLoginFits === true && g.passkeyFits === true;
  if (allGatesOpen && afterPw.where !== "desktop") {
    console.log(`ANMELDUNG ${e} | VORRICHTUNG KAPUTT, KEINE AUSSAGE - alle drei Tore offen, ` +
      `aber kein Schreibtisch. Versucht=${pwAttempted}. Schirm beim Start: ` +
      `${JSON.stringify(start)} | Schirm danach: ${JSON.stringify(afterPw)}`);
    return;
  }

  // 2. THE PASSKEY ROUTE, with the software authenticator of
  // fixtures/passkey.js - the one that works on every engine, because it is
  // JavaScript and not the browser's own WebAuthn stack. clara has a passkey
  // and no password.
  //
  // THE ORDER MATTERS AND IT IS WHY THIS MEASURED NOTHING THE FIRST TIME:
  // putSoftwareAuthenticator works through addInitScript, so it takes effect
  // for the NEXT document - it has to be set BEFORE the navigation, and the
  // navigation has to be waited out before the mask is read.
  //
  // At localhost and not 127.0.0.1: WebAuthn wants a name, not an address.
  const here = new URL(shell.url());
  await putSoftwareAuthenticator(shell, PASSKEY_NAME);
  await shell.goto(`http://localhost:${here.port}/`);
  // Cleared AFTER the move, because what is cleared is the store of THIS
  // origin: a browser that has never seen this installation.
  await shell.evaluate(() => { localStorage.clear(); sessionStorage.clear(); });
  await shell.goto(`http://localhost:${here.port}/`);

  const atMask = await settled(shell);
  console.log(`ANMELDUNG ${e} | Schirm vor Passkey | ${JSON.stringify(atMask)}`);
  let pkAttempted = false, pkEnding = "nicht versucht";
  if (atMask.where === "mask") {
    await shell.getByLabel("Benutzername").fill(PASSKEY_NAME);
    const button = shell.getByRole("button", { name: "Mit Passkey anmelden" });
    if ((await button.count()) > 0 && !(await button.isDisabled())) {
      pkAttempted = true;
      const before = await screen(shell);
      await button.click();
      pkEnding = await attemptOver(shell, before);
    }
  }
  const afterPk = await screen(shell);
  console.log(`ANMELDUNG ${e} | Passkey | versucht=${pkAttempted} | Ende=${pkEnding} | ${JSON.stringify(afterPk)}`);

  // The same control for the passkey route: in an engine with all three gates
  // open, clara's passkey has to reach a desktop too. A "no" there is a
  // broken authenticator, not a finding about any engine.
  if (allGatesOpen && afterPk.where !== "desktop") {
    console.log(`ANMELDUNG ${e} | AUTHENTIKATOR KAPUTT, KEINE AUSSAGE ZUM PASSKEY-WEG - ` +
      `${afterPk.where} "${afterPk.message}"`);
  }
});

// ---- part C: does the passkey route carry without the CFRG curves? --------
//
// THE QUESTION, and it is route 1c of docs/krypto-grenzen-browser.md: webkit
// has P-256 in full and lacks Ed25519 and X25519. A passkey login signs its
// assertion with P-256, so the PROOF is within reach there. The question is
// what comes AFTER the proof - whether the session the login opens can be
// opened at all without the two curves.
//
// WHY IT IS MEASURED ON CHROMIUM AND NOT ON WEBKIT. On webkit browserFits()
// refuses before the mask, so nothing downstream can be reached and the
// question cannot be asked there. So the question is turned round: take the
// curves away from an engine that HAS them and keep its P-256, which is
// exactly webkit's shape, and see whether a passkey login still gets in. The
// same trick shell/engines.spec.js uses for the seed step - it makes the
// engine LESS able, which is the state under test and switches no check off.
//
// WHAT THE CODE SAYS, read first so this measurement can contradict it:
// auth/keys.js newSessionKeys() generates an Ed25519 pair AND an X25519 pair
// unconditionally, for every session, passkey or password - "Ed25519 signs
// tasks, X25519 carries the Noise session and every end-to-end channel". If
// that is the whole story, route 1c cannot carry. A desktop appearing anyway
// would mean the reading is wrong, and that would be the better outcome.
async function withoutCFRG(page) {
  await page.addInitScript(() => {
    const gen = crypto.subtle.generateKey.bind(crypto.subtle);
    const imp = crypto.subtle.importKey.bind(crypto.subtle);
    const gone = (name) => name === "Ed25519" || name === "X25519";
    // NotSupportedError and not a plain throw: that is the error webkit 26
    // answers, measured in part A, and an engine that refuses differently
    // might take a different branch.
    const no = () => Promise.reject(new DOMException("measurement: no CFRG curves here", "NotSupportedError"));
    crypto.subtle.generateKey = function (algo, ...rest) {
      return gone(algo?.name || algo) ? no() : gen(algo, ...rest);
    };
    crypto.subtle.importKey = function (format, data, algo, ...rest) {
      return gone(algo?.name || algo) ? no() : imp(format, data, algo, ...rest);
    };
  });
}

test("traegt der Passkey-Weg ohne die CFRG-Kurven (Weg 1c)", async ({ shell }, info) => {
  const e = info.project.name;
  const g0 = await gates(shell);
  // Only an engine that can do EVERYTHING can answer this: the thing being
  // measured is the effect of taking two curves away, and that needs an engine
  // they can be taken away FROM. In the others the answer would be their own
  // gap and not the dependency.
  test.skip(!(g0.browserFits === true && g0.passwordLoginFits === true && g0.passkeyFits === true),
    `diese Engine kann nicht alles, hier ist die Abhaengigkeit nicht messbar - ${JSON.stringify(g0)}`);

  await withoutCFRG(shell);
  await putSoftwareAuthenticator(shell, PASSKEY_NAME);
  const here = new URL(shell.url());
  await shell.goto(`http://localhost:${here.port}/`);
  await shell.evaluate(() => { localStorage.clear(); sessionStorage.clear(); });
  await shell.goto(`http://localhost:${here.port}/`);

  // THE FIXTURE CHECKS THAT IT WORKED, before anything is read off it: the
  // engine must now look like webkit - no CFRG curves, P-256 intact. A run in
  // which the patch did not take would measure an untouched chromium and call
  // it a finding (messen.md: a measuring script checks its own preparation,
  // and it checks the EFFECT, not the preparation).
  const shape = await capabilities(shell);
  const asWebkit = shape["Ed25519 generateKey"] === "NotSupportedError"
    && shape["X25519 generateKey"] === "NotSupportedError"
    && shape["P-256 sign and verify"] === "";
  if (!asWebkit) {
    console.log(`WEG1C ${e}: VORRICHTUNG KAPUTT, KEINE AUSSAGE - die Engine sieht nicht wie webkit aus: ` +
      `Ed25519 generateKey="${shape["Ed25519 generateKey"]}", X25519 generateKey="${shape["X25519 generateKey"]}", ` +
      `P-256="${shape["P-256 sign and verify"]}"`);
    return;
  }
  console.log(`WEG1C ${e} | Engine nachgebaut: keine CFRG-Kurven, P-256 da`);

  const gates1 = await gates(shell);
  console.log(`WEG1C ${e} | Tore jetzt | ${JSON.stringify(gates1)}`);

  const atStart = await settled(shell);
  console.log(`WEG1C ${e} | Schirm | ${JSON.stringify(atStart)}`);

  let attempted = false, end = "nicht versucht";
  if (atStart.where === "mask") {
    await shell.getByLabel("Benutzername").fill(PASSKEY_NAME);
    const button = shell.getByRole("button", { name: "Mit Passkey anmelden" });
    if ((await button.count()) > 0 && !(await button.isDisabled())) {
      attempted = true;
      const before = await screen(shell);
      await button.click();
      end = await attemptOver(shell, before);
    }
  }
  const after = await screen(shell);
  console.log(`WEG1C ${e} | Passkey ohne CFRG | versucht=${attempted} | Ende=${end} | ${JSON.stringify(after)}`);

  // THE STEP WITHOUT WHICH THE WHOLE CASE MEASURES THE WRONG WALL.
  //
  // Run 7 printed "route 1c does not carry" and had measured no such thing:
  // browserFits() refused before the mask, exactly as it does on real webkit,
  // so the run never reached the question. That is messen.md's oldest entry -
  // a case turned away before the line it means measures the EARLIER wall -
  // and it nearly went out as a finding.
  //
  // Route 1c asks what happens AFTER the proof, so the question has to be put
  // to the thing itself: auth/keys.js newSessionKeys(), called directly, with
  // the curves gone. No gate in front of it. If it throws, no session can be
  // opened without the two curves and 1c cannot carry whatever the gate does;
  // if it returns keys, the reading of the code is wrong and 1c is worth
  // building. This is the intermediate check that proves the way to the line
  // is open.
  const session = await shell.evaluate(async () => {
    try {
      const keys = await import("/assets/js/auth/keys.js");
      const k = await keys.newSessionKeys();
      return { ok: true, what: `sign.publicRaw ${k?.sign?.publicRaw?.length} bytes, dh ${k?.dh ? "da" : "fehlt"}` };
    } catch (err) {
      return { ok: false, what: `${(err && err.name) || "threw"}: ${(err && err.message) || ""}` };
    }
  });
  console.log(`WEG1C ${e} | newSessionKeys() ohne CFRG | ${JSON.stringify(session)}`);

  // Two separate findings, and they must not be rolled into one sentence: what
  // the GATE does, and what the SESSION can do. The gate is a decision NGC
  // makes and could change; the session is what the curves are actually needed
  // for, and no gate changes that.
  console.log(`WEG1C ${e} | ERGEBNIS Tor | ${gates1.browserFits === false
    ? "browserFits() weist ab, bevor eine Maske kommt - der Mensch kommt nicht zum Passkey-Knopf"
    : "browserFits() laesst durch"}`);
  console.log(`WEG1C ${e} | ERGEBNIS Sitzung | ${session.ok
    ? "WEG 1C WAERE MOEGLICH: newSessionKeys() geht ohne die zwei Kurven - dann ist nur das Tor im Weg"
    : "WEG 1C TRAEGT NICHT: newSessionKeys() scheitert ohne die zwei Kurven, unabhaengig vom Tor"}`);
});

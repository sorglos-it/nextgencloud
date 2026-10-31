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

import { test, expect } from "../fixtures/page.js";
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
    };
  });
}

// The login attempt is over when the mask stops working - the same measure
// shell/login.spec.js uses, and it waits for a STATE and not for a time.
async function attemptOver(shell) {
  await expect.poll(async () => {
    const s = await shell.evaluate(() => {
      const box = document.querySelector("form.loginbox");
      if (box !== null) {
        const off = [...box.querySelectorAll("button")].every((b) => b.disabled);
        const said = box.querySelector('[role="alert"]');
        return off && (said === null || said.textContent === "") ? "working" : "idle";
      }
      if (document.querySelector("#taskbar") !== null) return "idle";
      if (document.querySelector("#root .msg") !== null) return "idle";
      return "working";
    });
    return s !== "working";
  }, { message: "the login attempt is over", timeout: 60_000 }).toBe(true);
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

test("kommt eine Anmeldung durch - Passwort und Passkey getrennt", async ({ shell }, info) => {
  const e = info.project.name;
  console.log(`ANMELDUNG ${e} | Tore | ${JSON.stringify(await gates(shell))}`);

  // THE SHELL AS IT COMES UP, with no stand-in and nothing helped along. This
  // is the screen a person meets.
  console.log(`ANMELDUNG ${e} | Schirm beim Start | ${JSON.stringify(await screen(shell))}`);

  // 1. THE PASSWORD ROUTE. anna has a password key and no passkey.
  //
  // It presses the button only if the button can be pressed: a disabled
  // button is itself the answer, and clicking it would measure playwright's
  // patience instead of the shell.
  const pw = { attempted: false };
  const before = await screen(shell);
  if (before.where === "mask" && before.passwordField === "AN") {
    await shell.getByLabel("Benutzername").fill(USER_NAME);
    await shell.getByLabel("Passwort").fill(PASSWORD);
    const go = shell.getByRole("button", { name: "Anmelden", exact: true });
    if (!(await go.isDisabled())) {
      pw.attempted = true;
      await go.click();
      await attemptOver(shell);
    }
  }
  console.log(`ANMELDUNG ${e} | Passwort | versucht=${pw.attempted} | ${JSON.stringify(await screen(shell))}`);

  // 2. THE PASSKEY ROUTE, with the software authenticator of
  // fixtures/passkey.js - the one that works on every engine, because it is
  // JavaScript and not the browser's own WebAuthn stack. clara has a passkey
  // and no password.
  //
  // A FRESH PAGE, because the attempt above may have left a desktop standing.
  // At localhost and not 127.0.0.1: WebAuthn wants a name, not an address.
  const here = new URL(shell.url());
  await putSoftwareAuthenticator(shell, PASSKEY_NAME);
  await shell.goto(`http://localhost:${here.port}/`);
  await shell.evaluate(() => { localStorage.clear(); sessionStorage.clear(); });
  await shell.goto(`http://localhost:${here.port}/`);

  const atMask = await screen(shell);
  console.log(`ANMELDUNG ${e} | Schirm vor Passkey | ${JSON.stringify(atMask)}`);
  let pkAttempted = false;
  if (atMask.where === "mask") {
    await shell.getByLabel("Benutzername").fill(PASSKEY_NAME);
    const button = shell.getByRole("button", { name: "Mit Passkey anmelden" });
    if ((await button.count()) > 0 && !(await button.isDisabled())) {
      pkAttempted = true;
      await button.click();
      await attemptOver(shell);
    }
  }
  console.log(`ANMELDUNG ${e} | Passkey | versucht=${pkAttempted} | ${JSON.stringify(await screen(shell))}`);
});

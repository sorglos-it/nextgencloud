// A software authenticator for the login cases: one ES256 passkey, built out
// of the Web Crypto API of whatever engine is running.
//
// WHY IT EXISTS. Playwright's virtual authenticator is the CDP WebAuthn
// domain, and CDP exists in Chromium alone - so every passkey case of
// shell/login.spec.js was switched off on Firefox and on WebKit by the NAME
// of the engine. That is the shape in which the Firefox bug of 17.10.2026
// grew: a route nobody ever drove. And on Firefox the passkey is the ONLY
// route the mask offers, because the password field is disabled there
// (contracts/proof.js publicFromSeed, OperationError on a PKCS#8 seed).
//
// WHAT IT REPLACES AND WHAT IT DOES NOT - and this is the line that decides
// whether the measurement is worth anything (claude-md/messen.md: "a mock
// that switches a bolt off devalues every measurement that bolt touches").
//
//   REPLACED: navigator.credentials.get, and nothing else. One function of
//   the browser, the same one the CDP virtual authenticator replaces deeper
//   down. The shell's own code (auth/keys.js proveWithPasskey) runs
//   unchanged above it.
//
//   NOT REPLACED, and this is the point: nothing on the checking side. The
//   assertion that comes out of here is a REAL ECDSA P-256 signature over
//   authenticatorData || SHA-256(clientDataJSON), by the private half of the
//   very key pair the mock world holds the public half of. mock/api.js runs
//   contracts/proof.js verify over it, in full - rp id hash, UP and UV
//   flags, the webauthn.get type, the origin, the challenge, the strict DER
//   reader, the P-256 check. A wrong byte anywhere in here fails the login,
//   and that is MEASURED and not argued. On 18.10.2026, with `raw[0] ^= 1;`
//   written in behind the sign() call below:
//
//     npx playwright test shell/login.spec.js -g "with a software authenticator" //       --project=chromium --project=firefox
//
//   answered 3 failed, 2 passed, 1 skipped. The three that FAILED are the
//   three that really sign (clara in both engines, dora in chromium); the two
//   that passed are the "never falls back" cases, which sign nothing at all,
//   and the skipped one is dora on Firefox, which needs a password key.
//   Without the bent byte the same command is 5 passed, 1 skipped. So the
//   proof route is NOT weakened by this file; only the hardware in front of
//   it is absent.
//
//   WHAT STAYS UNMEASURED BECAUSE OF IT: that a real authenticator refuses
//   to sign without user verification. This one sets the UV flag itself
//   (FLAGS below), as the CDP one does with isUserVerified. The shell asks
//   for userVerification: "required" and that word reaches no authenticator
//   here. Every number this file is in the path of is a number about the
//   SHELL's and the API's half of a passkey login, never about the
//   authenticator's.
//
// WHY ES256 AND NOT EdDSA. contracts/proof.js takes all three (ALG_ES256,
// ALG_EDDSA, ALG_RS256) and a real passkey signs with ES256 anyway. It is
// also the one algorithm all three engines have: Firefox 141 refuses exactly
// the Ed25519 step a password key stands on, and the WebKit Playwright brings
// has no Ed25519 at all. P-256, measured, is in all three - which is why the
// same fixture key pairs of fixtures/logins.js can be used here and over CDP.
import { PASSKEY_USERS } from "./logins.js";

// putSoftwareAuthenticator puts one passkey of the mock world in front of the
// page as a replacement for navigator.credentials.get, for this document and
// every one that follows it (addInitScript, so it survives the move to the
// DNS name atLocalhost makes).
//
// It returns nothing to clean up: the page is thrown away after the case, and
// nothing outside the page was touched.
export async function putSoftwareAuthenticator(shell, who) {
  const u = PASSKEY_USERS[who];
  await shell.addInitScript(
    ({ pkcs8Base64, credIdBase64, rpId }) => {
      const bytesOfBase64 = (s) => {
        const text = atob(s);
        const out = new Uint8Array(text.length);
        for (let i = 0; i < text.length; i++) out[i] = text.charCodeAt(i);
        return out;
      };
      const b64url = (u8) =>
        btoa(String.fromCharCode(...u8)).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");

      // UP (the user was there) and UV (a PIN or biometrics).
      // contracts/proof.js verifyAssertion refuses an assertion without both,
      // so these two bits are what makes the proof go through - and they are
      // set HERE and not earned, which is the gap named at the top of this
      // file. It stands INSIDE the init script: addInitScript serialises this
      // function and sends it to the page, so it closes over nothing of this
      // module (measured on 18.10.2026: a module-level const here answered
      // "ReferenceError: FLAGS is not defined" in the page, and the login
      // failed with the one sentence every failure gets - which is exactly how
      // a broken fixture hides behind a real-looking result).
      const FLAGS = 0x05;

      const pkcs8 = bytesOfBase64(pkcs8Base64);
      const credId = bytesOfBase64(credIdBase64);

      // rawToDer turns the 64 bytes the Web Crypto API hands back (r || s)
      // into the ASN.1 SEQUENCE a real authenticator returns and
      // contracts/proof.js derToRaw reads. It is an ENCODING and not a
      // loosening: derToRaw is strict about the shortest form, so the leading
      // zero below is written exactly where DER wants one and nowhere else.
      // The signature itself is untouched.
      const rawToDer = (raw) => {
        const int = (v) => {
          let i = 0;
          while (i < v.length - 1 && v[i] === 0) i++;
          const digits = v.subarray(i);
          const pad = (digits[0] & 0x80) !== 0 ? 1 : 0;
          const out = new Uint8Array(2 + pad + digits.length);
          out[0] = 0x02;
          out[1] = pad + digits.length;
          out.set(digits, 2 + pad);
          return out;
        };
        const r = int(raw.subarray(0, 32));
        const s = int(raw.subarray(32, 64));
        const out = new Uint8Array(2 + r.length + s.length);
        out[0] = 0x30;
        out[1] = r.length + s.length;
        out.set(r, 2);
        out.set(s, 2 + r.length);
        return out;
      };

      const same = (a, b) => a.length === b.length && a.every((v, i) => v === b[i]);

      navigator.credentials.get = async (options) => {
        const pk = options && options.publicKey;
        if (!pk) throw new DOMException("no publicKey request", "NotSupportedError");
        // The shell offers the credential IDs it was given. An authenticator
        // that holds none of them answers nothing, and that is the state the
        // "never falls back to the password" case measures - so this one
        // really does look, instead of signing whatever it is asked to.
        const offered = pk.allowCredentials || [];
        const wanted = offered.some((c) => same(new Uint8Array(c.id), credId));
        if (offered.length > 0 && !wanted) {
          throw new DOMException("no credential for this request", "NotAllowedError");
        }

        const key = await crypto.subtle.importKey(
          "pkcs8", pkcs8, { name: "ECDSA", namedCurve: "P-256" }, false, ["sign"]);

        const clientData = new TextEncoder().encode(JSON.stringify({
          type: "webauthn.get",
          challenge: b64url(new Uint8Array(pk.challenge)),
          origin: location.origin,
          crossOrigin: false,
        }));

        // rpIdHash || flags || signCount, the 37 bytes WebAuthn asks for and
        // contracts/proof.js holds against MIN_AUTH_DATA. The rp id hashed
        // here is the one the SHELL handed over, so a shell that sent the
        // wrong rp id produces an assertion the API refuses - which is what a
        // real authenticator does too.
        const rpHash = new Uint8Array(
          await crypto.subtle.digest("SHA-256", new TextEncoder().encode(pk.rpId || rpId)));
        const authData = new Uint8Array(37);
        authData.set(rpHash, 0);
        authData[32] = FLAGS;
        // The signature counter. A fixed 1, as the CDP authenticator's
        // signCount: 0 leaves it - nothing on the checking side reads it
        // (contracts/proof.js looks at the flags and the hash and no further),
        // and a counter this side invented would be a number about nothing.
        authData[36] = 1;

        const cdHash = new Uint8Array(await crypto.subtle.digest("SHA-256", clientData));
        const signed = new Uint8Array(authData.length + cdHash.length);
        signed.set(authData, 0);
        signed.set(cdHash, authData.length);

        const raw = new Uint8Array(
          await crypto.subtle.sign({ name: "ECDSA", hash: "SHA-256" }, key, signed));

        return {
          type: "public-key",
          id: b64url(credId),
          rawId: credId.buffer.slice(credId.byteOffset, credId.byteOffset + credId.length),
          response: {
            clientDataJSON: clientData.buffer,
            authenticatorData: authData.buffer,
            signature: rawToDer(raw).buffer,
            userHandle: null,
          },
          getClientExtensionResults: () => ({}),
        };
      };
    },
    { pkcs8Base64: u.privateKeyBase64, credIdBase64: u.credIdBase64, rpId: u.rpId },
  );
}

// putEmptySoftwareAuthenticator puts an authenticator in front of the page
// that WORKS and holds nothing for this installation - the state every NGC
// account is in today, since apps/web has no credentials.create. It is the
// counterpart of the CDP authenticator with no credential added, and it
// refuses the way a real one does: NotAllowedError, nothing signed.
export async function putEmptySoftwareAuthenticator(shell) {
  await shell.addInitScript(() => {
    navigator.credentials.get = async () => {
      throw new DOMException("no credential for this request", "NotAllowedError");
    };
  });
}

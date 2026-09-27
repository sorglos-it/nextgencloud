// Who may log in to the mock world, as plain values. They are in the
// repository on purpose: this is a mock, it holds no real account, and a test
// whose password came out of a file nobody can read is a test nobody can run.
//
// WHY THEY ARE HERE AND NOT IN mock/world.js, where the rest of the world is:
// a case needs them (the password it types, the code it enters, the private
// key it loads into the virtual authenticator), and importing world.js from a
// case pulls the vendored Argon2 in behind it - which playwright's own
// loader does not get through: "The requested module
// '../../public/assets/vendor/noble-hashes/argon2.js' does not provide an
// export named 'argon2id'", measured. This file imports nothing at all, so
// both sides can read it: the cases here and world.js there.

export const USER_NAME = "anna";
export const PASSWORD = "Heimnetz-2026";

// The three other people the login cases need, each one a different way in:
// bernd has a second factor, clara has nothing but a passkey, and dora is in
// a role that asks for the passkey AND the password. They are steered by the
// NAME that is typed and by nothing else, which is how a real API steers them
// too - a hook on window that changed the way the login behaves would be a
// hook anything else on the page could set.
export const TOTP_NAME = "bernd";
export const PASSKEY_NAME = "clara";
export const BOTH_NAME = "dora";

// What bernd's authenticator app shows. A mock has no clock to check it
// against; what a case measures is that the shell ASKS and sends it on.
export const TOTP_CODE = "123456";

// The relying party the login challenge names. It is "localhost" and not the
// trust list's "ngc.example.org", because that is the one thing about a
// passkey that the BROWSER decides: measured in chromium with a virtual
// authenticator, an origin that is an IP literal (the suite's 127.0.0.1)
// answers "SecurityError: This is an invalid domain" for every rp id there
// is - "ngc.example.org", "127.0.0.1" and none at all - and the same page
// reached as http://localhost:<port> takes rp id "localhost" and makes a
// credential.
//
// The origins below are the https spelling url.validOrigin demands, and the
// browser really reports http. The shell never looks at origins (it hands
// rp.id to the authenticator and nothing else), so the difference reaches no
// shell code; mock/api.js holds the assertion against the http one, in the
// single place where the two differ.
export const RP_ID = "localhost";
export const RP_ORIGINS = ["https://localhost"];

// The two passkeys, as fixed key pairs. A virtual authenticator is loaded
// with the private half over CDP and the mock keeps the public half, so an
// assertion it makes is one proof.verify really checks - no registration
// round trip, and the same bytes on every run.
const bytes = (n, b) => new Uint8Array(n).fill(b);

// The credential IDs and the user handles a real registration would have
// set. They are bytes here and base64 below, because the two sides want them
// differently: the mock puts the bytes into the challenge, CDP takes text.
export const CRED_ID = Object.freeze({ clara: bytes(32, 0x31), dora: bytes(32, 0x32) });
export const USER_HANDLE = Object.freeze({ clara: bytes(16, 0x24), dora: bytes(16, 0x25) });

// The private halves, as PKCS#8. Standard base64 and not base64url: that is
// what the CDP WebAuthn domain takes, and it is the only place in this
// repository that does.
export const PKCS8 = Object.freeze({
  clara: "MIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQgWfH1ZOiu0hCGBm6C0TD8Qfh+19AyGqE"
    + "rT+zfN7Rd+EqhRANCAATmH06E6jA5cpS5eVtgOcDHYDsLt9eehqc29fy3e3OApS8I8aXnWPc6EmVOuDia"
    + "oGVuKOBMORcE5uINu7LXf/O3",
  dora: "MIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQg8QeQRO/myvWeT/jjndWDieWYJMhU0A/"
    + "A/dYiTl786MShRANCAARbL0bD/WEMq5pT888pr3MZljb/Vq4YspW+hJHLnFtbPWlVANGmbG+LYr5BxTVt"
    + "mRUqBEH5V8d31iMkowRWLXw9",
});

// What a case hands to CDP, ready to use.
const base64 = (u8) => Buffer.from(u8).toString("base64");
export const PASSKEY_USERS = Object.freeze({
  clara: Object.freeze({
    rpId: RP_ID, privateKeyBase64: PKCS8.clara,
    credIdBase64: base64(CRED_ID.clara), userHandleBase64: base64(USER_HANDLE.clara),
  }),
  dora: Object.freeze({
    rpId: RP_ID, privateKeyBase64: PKCS8.dora,
    credIdBase64: base64(CRED_ID.dora), userHandleBase64: base64(USER_HANDLE.dora),
  }),
});

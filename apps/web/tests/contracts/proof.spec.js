import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

// proof.json holds 21 proofs with their keys, valid and invalid, and the
// relying party they belong to; proof_kdf.json holds the password key of
// three typed passwords. Both are the same files shared/proof checks Go
// against (TestProofVectors, TestKDFVectors), so the two sides are held
// against one truth and not against each other.
const v = vectors("proof.json");
const kdfCases = vectors("proof_kdf.json");

// Proof.Validate of Go over the vectors: everything there is whole except
// these two. The rules stand in the test below; these are the names they pick
// out, written down here so a reader sees at once what the file holds.
const brokenProofs = ["passkey ES256, authData of 36 bytes", "password key, proof claims kind passkey"];

// Does the Web Crypto API of this engine know Ed25519? Chrome, Safari 17 and
// Firefox 130 do; the WebKit that Playwright brings answers NotSupportedError
// to importKey. An engine without it can check no EdDSA signature and import
// no EdDSA key, so it refuses both - which is right, and which means no case
// that needs a right EdDSA signature to come out true can run there. Those
// cases are left out and said out loud, instead of quietly passing.
async function hasEd25519(shell, pub) {
  return shell.evaluate(async (pubB64) => {
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    try {
      await crypto.subtle.importKey("raw", b64decode(pubB64), { name: "Ed25519" }, false, ["verify"]);
      return true;
    } catch {
      return false;
    }
  }, pub);
}

// The password key of proof.json is a raw Ed25519 key, so it is the one this
// engine is asked about.
const passwordPub = v.proofs.find((c) => c.key.kind === "password").key.pub;

test("every proof of proof.json is decided as Go decides it", async ({ shell }) => {
  const ed = await hasEd25519(shell, passwordPub);
  // Without Ed25519 nothing can come out right that rests on an EdDSA
  // signature or on an EdDSA key. The cases that have to come out false stay
  // in: an engine that cannot check has to refuse them too.
  const left = v.proofs.filter((c) => !ed && c.valid && c.key.alg === "EdDSA").map((c) => c.name);
  if (left.length > 0) {
    const why = `no Ed25519 in this engine's Web Crypto, so ${left.length} of ${v.proofs.length} cases are left out: ${left.join("; ")}`;
    console.log(why);
    test.info().annotations.push({ type: "left out", description: why });
  }
  const got = await shell.evaluate(async (d) => {
    const p = await import("/assets/js/contracts/proof.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const bytes = (s) => (s ? b64decode(s) : new Uint8Array(0));
    const out = [];
    for (const c of d.proofs) {
      const key = { ...c.key, pub: b64decode(c.key.pub), credId: bytes(c.key.credId) };
      if (c.key.kdf) key.kdf = { ...c.key.kdf, salt: b64decode(c.key.kdf.salt) };
      const pr = {
        kind: c.proof.kind, kid: c.proof.kid, sig: b64decode(c.proof.sig),
        authData: bytes(c.proof.authData), clientData: bytes(c.proof.clientData),
      };
      out.push({
        name: c.name,
        verified: await p.verify(pr, key, d.rp, c.purpose, b64decode(c.payload)),
        proofWhole: p.validateProof(pr),
        keyWhole: await p.validateUserKey(key),
      });
    }
    return out;
  }, v);

  // 1. The answer of verify, case by case.
  const wrong = v.proofs
    .map((c, i) => ({ c, g: got[i] }))
    .filter(({ c }) => !left.includes(c.name))
    .filter(({ c, g }) => c.valid !== g.verified)
    .map(({ c, g }) => `${c.name}: answered ${g.verified}, ${c.valid} wanted`);
  expect(wrong).toEqual([]);

  // 2. Proof.Validate is a check of its own and says nothing about a
  // signature: every proof of the file is whole but the two named above.
  expect(got.filter((g) => !g.proofWhole).map((g) => g.name).sort()).toEqual([...brokenProofs].sort());

  // 3. UserKey.Validate likewise. Every key of the file is whole except the
  // RSA key of 1024 bits, which is under the floor of 2048 that Go holds, and
  // - where the engine has no Ed25519 - the two passkey keys it cannot import
  // at all.
  const wantBrokenKeys = v.proofs
    .filter((c) => c.name.includes("1024-bit") || (!ed && c.key.kind === "passkey" && c.key.alg === "EdDSA"))
    .map((c) => c.name);
  expect(got.filter((g) => !g.keyWhole).map((g) => g.name).sort()).toEqual([...new Set(wantBrokenKeys)].sort());
});

// verify does not run validateProof, because Go does not run it either:
// proof.Verify checks what it needs itself and leaves authData and clientData
// of a password proof entirely alone, while Proof.Validate refuses a password
// proof that carries them. The two therefore have to disagree about one and
// the same proof - and no vector says so, because proof.json has no password
// proof with authenticator data that is still valid. This case builds one:
// the vector "password key", whose signature is right, with authenticator
// data and client data hung on it. Put validateProof back at the head of
// verify and this case goes red at once, while every vector stays green.
test("a password proof that carries what only a passkey may carry still verifies", async ({ shell }) => {
  test.skip(!(await hasEd25519(shell, passwordPub)),
    "no Ed25519 in this engine's Web Crypto: no right signature can come out true here");
  const c = v.proofs.find((x) => x.name === "password key");
  expect(c.valid).toBe(true);
  const got = await shell.evaluate(async (d) => {
    const p = await import("/assets/js/contracts/proof.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const key = { ...d.c.key, pub: b64decode(d.c.key.pub), credId: new Uint8Array(0),
      kdf: { ...d.c.key.kdf, salt: b64decode(d.c.key.kdf.salt) } };
    const plain = { kind: d.c.proof.kind, kid: d.c.proof.kid, sig: b64decode(d.c.proof.sig),
      authData: new Uint8Array(0), clientData: new Uint8Array(0) };
    // The same proof with the two fields a password proof never has. The
    // signature covers neither of them, so nothing about it changes.
    const dressed = { ...plain, authData: new Uint8Array(37).fill(9), clientData: new Uint8Array(10).fill(9) };
    const payload = b64decode(d.c.payload);
    return {
      plainVerifies: await p.verify(plain, key, d.rp, d.c.purpose, payload),
      plainWhole: p.validateProof(plain),
      dressedVerifies: await p.verify(dressed, key, d.rp, d.c.purpose, payload),
      dressedWhole: p.validateProof(dressed),
    };
  }, { c, rp: v.rp });
  // The proof as it stands: right by both.
  expect(got.plainVerifies).toBe(true);
  expect(got.plainWhole).toBe(true);
  // And dressed up: Verify of Go says yes, Validate of Go says no. The web
  // side has to answer exactly as the API does, or it would refuse proofs the
  // API takes.
  expect(got.dressedVerifies).toBe(true);
  expect(got.dressedWhole).toBe(false);
});

// The challenge a passkey signs is SHA-256 over the bytes a signature would
// cover. The clientData of the vectors was written by Go, so the challenge in
// it is the one Go computed: this holds the JavaScript against that number
// and not against itself.
test("the challenge is the one Go wrote into the client data", async ({ shell }) => {
  const got = await shell.evaluate(async (d) => {
    const p = await import("/assets/js/contracts/proof.js");
    const { b64encode, b64decode, fromUtf8 } = await import("/assets/js/contracts/b64.js");
    const out = [];
    for (const c of d.proofs) {
      if (!c.proof.clientData) continue;
      const cd = JSON.parse(fromUtf8(b64decode(c.proof.clientData)));
      out.push({
        name: c.name,
        same: b64encode(await p.challenge(c.purpose, b64decode(c.payload))) === cd.challenge,
      });
    }
    return out;
  }, v);
  // Two cases carry a challenge made for something else on purpose; every
  // other one has to agree to the character.
  const others = ["passkey ES256, challenge for another purpose", "passkey ES256, challenge for another payload"];
  const wrong = got.filter((g) => g.same === others.includes(g.name)).map((g) => g.name);
  expect(wrong).toEqual([]);
  expect(got.length).toBe(v.proofs.filter((c) => c.proof.clientData).length);
});

// The seed of a password key is the same 32 bytes in Go and here, and the
// public key that belongs to it has to come out the same as well
// (ed25519.NewKeyFromSeed). This case starts at the seeds of the vectors; the
// case below starts at the typed password and derives them.
test("the public key of a seed is the one Go derives", async ({ shell }) => {
  test.skip(!(await hasEd25519(shell, passwordPub)),
    "no Ed25519 in this engine's Web Crypto: it can import no seed and export no public key");
  const got = await shell.evaluate(async (cases) => {
    const p = await import("/assets/js/contracts/proof.js");
    const { b64encode, b64decode } = await import("/assets/js/contracts/b64.js");
    const out = [];
    for (const c of cases) {
      out.push(b64encode(await p.publicFromSeed(b64decode(c.seed))));
    }
    // A seed of the wrong size is no seed, and a private key made from one
    // never leaves the browser again.
    const refused = [];
    for (const bad of [new Uint8Array(31), new Uint8Array(33), new Uint8Array(0), "seed", undefined, null]) {
      try {
        await p.publicFromSeed(bad);
        refused.push(String(bad));
      } catch (e) {
        if (e.constructor.name !== "BadProof") refused.push(`threw ${e.constructor.name}`);
      }
    }
    const priv = await p.privateFromSeed(b64decode(cases[0].seed));
    return { out, refused, extractable: priv.extractable, usages: priv.usages };
  }, kdfCases);
  expect(got.out).toEqual(kdfCases.map((c) => c.public));
  expect(got.refused).toEqual([]);
  expect(got.extractable).toBe(false);
  expect(got.usages).toEqual(["sign"]);
});

// The Web Crypto API hands out no public key of a private one, so
// publicFromSeed imports the seed as extractable and reads the JWK. That JWK
// carries the seed back out as d, base64url text - a second copy of the
// secret that no code can overwrite, because a string cannot be written to.
// What publicFromSeed can do is let go of it at once, so nothing that outlives
// the call holds it any more.
//
// This case stands crypto.subtle.exportKey in the way, keeps the very object
// the engine handed back, and looks at it after publicFromSeed has returned.
// It says out loud that d was there beforehand, so it cannot go green on an
// engine that leaves d out to begin with.
test("the export that gives the public key does not keep the seed", async ({ shell }) => {
  test.skip(!(await hasEd25519(shell, passwordPub)),
    "no Ed25519 in this engine's Web Crypto: it can import no seed and export no JWK");
  const got = await shell.evaluate(async (c) => {
    const p = await import("/assets/js/contracts/proof.js");
    const { b64encode, b64decode } = await import("/assets/js/contracts/b64.js");
    const seed = b64decode(c.seed);
    const original = crypto.subtle.exportKey;
    const seen = [];
    crypto.subtle.exportKey = async function (...a) {
      const jwk = await original.apply(this, a);
      // Read now, before publicFromSeed gets its hands on the object.
      seen.push({ jwk, dWas: typeof jwk.d === "string" && b64encode(b64decode(jwk.d)) === b64encode(seed) });
      return jwk;
    };
    let pub;
    try {
      pub = b64encode(await p.publicFromSeed(seed));
    } finally {
      delete crypto.subtle.exportKey;
    }
    return {
      pub,
      exports: seen.length,
      dWasTheSeed: seen.map((s) => s.dWas),
      dNow: seen.map((s) => (s.jwk.d === undefined ? "let go" : "still there")),
      xStill: seen.map((s) => typeof s.jwk.x),
      restored: crypto.subtle.exportKey === original,
    };
  }, kdfCases[0]);
  expect(got.pub).toBe(kdfCases[0].public);
  // Exactly one export, and what came back really was the seed.
  expect(got.exports).toBe(1);
  expect(got.dWasTheSeed).toEqual([true]);
  // And it is gone from the object the moment publicFromSeed is done with it.
  expect(got.dNow).toEqual(["let go"]);
  // The public half is untouched - only the secret one is dropped.
  expect(got.xStill).toEqual(["string"]);
  expect(got.restored).toBe(true);
});

// The Web Crypto API imports an Ed25519 seed in one form only: a PKCS#8
// document, sixteen bytes of head and the seed behind them. Building that
// document means copying the seed into a new array, so both privateFromSeed
// and publicFromSeed make a second copy of the secret on the page's heap.
//
// Unlike the JWK's d, that copy is bytes and not text, so it can be written
// to - and it is, as soon as the import has taken it. The import copies what
// it is given before it returns, so wiping afterwards costs the key nothing;
// this case signs with the imported key and verifies the signature against
// the public half to prove exactly that.
//
// It stands crypto.subtle.importKey in the way and keeps the very arrays that
// were handed in. It says out loud that the seed was in them beforehand, so
// it cannot go green on a call that never happened.
test("the PKCS#8 copy of the seed does not stay behind", async ({ shell }) => {
  test.skip(!(await hasEd25519(shell, passwordPub)),
    "no Ed25519 in this engine's Web Crypto: it imports no seed at all");
  const got = await shell.evaluate(async (c) => {
    const p = await import("/assets/js/contracts/proof.js");
    const { b64encode, b64decode } = await import("/assets/js/contracts/b64.js");
    const seed = b64decode(c.seed);
    const original = crypto.subtle.importKey;
    const seen = [];
    crypto.subtle.importKey = function (format, data, ...rest) {
      // Only the PKCS#8 documents of this module; the raw import below is the
      // public key and carries no secret.
      if (format === "pkcs8" && data instanceof Uint8Array) {
        const tail = data.subarray(data.length - seed.length);
        seen.push({ data, len: data.length, was: seed.every((b, i) => tail[i] === b) });
      }
      return original.call(this, format, data, ...rest);
    };
    let pub;
    let signs = false;
    try {
      pub = b64encode(await p.publicFromSeed(seed));
      // The same seed through the other door, and the key it gives has to
      // still work: wiping the document after the import must not have taken
      // anything the key needed.
      const key = await p.privateFromSeed(seed);
      const msg = new TextEncoder().encode("ngc");
      const sig = await crypto.subtle.sign({ name: "Ed25519" }, key, msg);
      const half = await crypto.subtle.importKey("raw", b64decode(c.public), { name: "Ed25519" },
        false, ["verify"]);
      signs = await crypto.subtle.verify({ name: "Ed25519" }, half, sig, msg);
    } finally {
      delete crypto.subtle.importKey;
    }
    return {
      pub,
      signs,
      imports: seen.length,
      lens: seen.map((s) => s.len),
      wasTheSeed: seen.map((s) => s.was),
      left: seen.map((s) => Array.from(s.data).filter((b) => b !== 0).length),
      callersSeed: b64encode(seed),
      restored: crypto.subtle.importKey === original,
    };
  }, kdfCases[0]);
  expect(got.pub).toBe(kdfCases[0].public);
  // Two documents, one per door, and each of them 16 bytes of head and 32 of
  // seed.
  expect(got.imports).toBe(2);
  expect(got.lens).toEqual([48, 48]);
  // They really did carry the seed - otherwise this case would mean nothing.
  expect(got.wasTheSeed).toEqual([true, true]);
  // And not one byte of either is left, head and all.
  expect(got.left).toEqual([0, 0]);
  // The seed the caller handed in is his and stays untouched: only the copy
  // this module made is wiped.
  expect(got.callersSeed).toBe(kdfCases[0].seed);
  // And the key that came out of the wiped document still signs.
  expect(got.signs).toBe(true);
  expect(got.restored).toBe(true);
});

// The whole way from the typed password to the key, against the same three
// vectors Go writes (proof.DeriveKey over argon2.IDKey). The third case is
// the second one typed in NFD: Go has no NFC table and put the case here on
// purpose, because the browser is the side that normalizes. Nothing here is
// held against the library alone - every seed is the one Go computed.
//
// Argon2id is slow because it is meant to be, so this case also writes down
// how long it took, and it watches the window while it runs: a timer on the
// page has to keep firing, or the derivation has taken the one thread the
// user's clicks need.
test("the seed of a typed password is the one Go derives with Argon2id", async ({ shell }) => {
  test.setTimeout(120_000);
  const got = await shell.evaluate(async (cases) => {
    const p = await import("/assets/js/contracts/proof.js");
    const { b64encode } = await import("/assets/js/contracts/b64.js");
    const out = [];
    for (const c of cases) {
      // The parameters are taken from the vector, one by one, never from a
      // default of the library: what is not in the document is not a value.
      const kdf = { alg: c.kdf.alg, salt: p.saltBytes(c.kdf.salt), m: c.kdf.m, t: c.kdf.t, p: c.kdf.p };
      let ticks = 0;
      const timer = setInterval(() => { ticks += 1; }, 20);
      const started = performance.now();
      const seed = await p.deriveSeed(c.typed ?? c.password, kdf);
      const ms = Math.round(performance.now() - started);
      clearInterval(timer);
      out.push({
        seed: b64encode(seed),
        bytes: seed instanceof Uint8Array ? seed.length : `no bytes but ${typeof seed}`,
        ms,
        ticks,
      });
    }
    return out;
  }, kdfCases);
  expect(got.map((g) => g.seed)).toEqual(kdfCases.map((c) => c.seed));
  expect(got.map((g) => g.bytes)).toEqual(kdfCases.map(() => 32));
  const how = `deriveSeed at m=65536 t=3 p=1: ${got.map((g) => `${g.ms} ms`).join(", ")}`;
  console.log(how);
  test.info().annotations.push({ type: "Argon2id", description: how });
  // The timer is asked for a tick every 20 ms. Measured while these cases
  // ran, it really gets about 49 a second in Chromium and about 32 in WebKit,
  // so the floor is one tick per 100 ms of the derivation as it actually
  // took: a fifth of the slower engine's rate. It has to grow with the
  // measured time, because a fixed five would still be met by a regression
  // that took nine tenths of the thread - at m=65536 that is 5 ticks against
  // the 48 to 60 there are.
  const tooQuiet = got
    .filter((g) => g.ticks < Math.floor(g.ms / 100))
    .map((g) => `only ${g.ticks} ticks while the derivation ran ${g.ms} ms`);
  expect(tooQuiet).toEqual([]);
  console.log(`ticks while deriving: ${got.map((g) => `${g.ticks} in ${g.ms} ms`).join(", ")}`);
  // Nothing about the public key here: the seeds just came out as the ones of
  // the vectors, and the case above takes those same seeds to the public keys
  // of the vectors. The chain from the typed password to the key is closed
  // across the two, and this half runs in every engine, Ed25519 or not.
});

// The parameters of Argon2id come from the contract and from nowhere else:
// a parameter that is missing, out of range or not a whole number is an
// error, never a default the library would quietly fill in (Go: DeriveKey
// begins with KDF.Validate and returns ErrKDF). The password is the text the
// user typed; bytes or a number are no password.
test("a password key is derived only with the parameters of the contract", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const p = await import("/assets/js/contracts/proof.js");
    const salt = new Uint8Array(16).fill(7);
    // The cheapest parameters the contract allows, so a case that wrongly
    // derives something still answers in a moment.
    const good = { alg: "argon2id", salt, m: 19456, t: 1, p: 1 };
    const how = async (name, password, kdf) => {
      try {
        const seed = await p.deriveSeed(password, kdf);
        return [name, `derived ${seed.length} bytes`];
      } catch (e) {
        return [name, e.constructor.name];
      }
    };
    return [
      await how("a memory that is missing", "pw", { ...good, m: undefined }),
      await how("a time that is missing", "pw", { ...good, t: undefined }),
      await how("a parallelism that is missing", "pw", { ...good, p: undefined }),
      await how("a memory below the floor", "pw", { ...good, m: 19455 }),
      await how("a memory above the ceiling", "pw", { ...good, m: 1048577 }),
      await how("a memory that is text", "pw", { ...good, m: "65536" }),
      await how("a memory with a fraction", "pw", { ...good, m: 65536.5 }),
      await how("a time of zero", "pw", { ...good, t: 0 }),
      await how("a time above the ceiling", "pw", { ...good, t: 11 }),
      await how("a parallelism of zero", "pw", { ...good, p: 0 }),
      await how("a parallelism above the ceiling", "pw", { ...good, p: 5 }),
      await how("a salt of 15 bytes", "pw", { ...good, salt: new Uint8Array(15) }),
      await how("a salt that is text", "pw", { ...good, salt: "0123456789abcdef" }),
      await how("another algorithm", "pw", { ...good, alg: "argon2i" }),
      await how("no KDF at all", "pw", undefined),
      await how("a KDF that is text", "pw", "argon2id"),
      await how("a KDF whose memory fights back", "pw", { ...good, get m() { throw new TypeError("no"); } }),
      await how("a password that is a number", 42, good),
      await how("a password that is bytes", new Uint8Array(4), good),
      await how("a password that is missing", undefined, good),
    ];
  });
  const wrong = out.filter(([, answer]) => answer !== "BadProof");
  expect(wrong).toEqual([]);
});

// The typed password is meant to live in one place and to be gone right
// after: deriveSeed hands its buffer to the worker instead of copying it, and
// the worker wipes it. Where no worker can be made at all, nobody takes the
// buffer and nobody wipes it, and the bytes of the password would stay on the
// page's heap for as long as the engine kept them.
//
// This case stands TextEncoder in the way to keep the very array the password
// was encoded into, and hands out a Worker that refuses to be built. It says
// out loud that those bytes were the password beforehand, so it cannot go
// green on an encoding that never happened.
test("the typed password is wiped even where no worker can be made", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const p = await import("/assets/js/contracts/proof.js");
    const password = "Heimnetz-2026";
    const want = new TextEncoder().encode(password);
    const encode = TextEncoder.prototype.encode;
    const seen = [];
    TextEncoder.prototype.encode = function (...a) {
      const u = encode.apply(this, a);
      seen.push({ u, isPassword: u.length === want.length && want.every((b, i) => u[i] === b) });
      return u;
    };
    const realWorker = globalThis.Worker;
    globalThis.Worker = class { constructor() { throw new DOMException("no worker here"); } };
    let answer = "no error";
    try {
      const salt = new Uint8Array(16).fill(7);
      await p.deriveSeed(password, { alg: "argon2id", salt, m: 19456, t: 1, p: 1 });
      answer = "derived something";
    } catch (e) {
      answer = e.constructor.name;
    } finally {
      TextEncoder.prototype.encode = encode;
      globalThis.Worker = realWorker;
    }
    const mine = seen.filter((s) => s.isPassword);
    return {
      answer,
      encodings: mine.length,
      left: mine.map((s) => Array.from(s.u).filter((b) => b !== 0).length),
    };
  });
  // No worker is no seed, and it says so instead of hanging or throwing
  // something a caller cannot name.
  expect(got.answer).toBe("BadProof");
  // The password really was encoded once - otherwise there would be nothing
  // to wipe and this case would mean nothing.
  expect(got.encodings).toBe(1);
  // And not one byte of it is left.
  expect(got.left).toEqual([0]);
});

// A worker that is built, takes the password and then never answers would
// leave deriveSeed open for good - no seed, no error, and the password's
// bytes sitting in a worker nobody ends. For someone who has just typed his
// password, a sign-in that neither finishes nor fails is the worst of the
// three endings, so there is a deadline.
//
// The deadline is a number this module works out, so this case reads that
// number instead of waiting for it: it hands out a Worker that swallows
// everything in silence, stands setTimeout in the way, writes down the
// milliseconds that were asked for and then lets the timer run at once.
//
// That holds three things in one go, where waiting held one. The budget grows
// with the work Argon2id was asked for - m times t, and p not at all, because
// lanes split that work without adding to it. It stops at two minutes however
// large the parameters of the document are, and those parameters come from a
// document the server stored, not from this page. And the way out really ends
// the worker and answers BadProof instead of leaving the promise open.
//
// What it does not hold is the wall clock: the timer is real, but it is asked
// for zero. That trade is deliberate and it is the whole point - waiting the
// deadlines out cost forty seconds of every run and held nothing in return
// but "more than 5 s and less than 60 s", which every one of these five
// numbers would have passed.
test("the deadline of a derivation is the work it was asked for, capped at two minutes",
  async ({ shell }) => {
    const got = await shell.evaluate(async () => {
      const p = await import("/assets/js/contracts/proof.js");
      const realWorker = globalThis.Worker;
      const realTimeout = globalThis.setTimeout;
      const salt = new Uint8Array(16).fill(7);
      const runs = [];
      // The cheapest the contract allows, twice the passes, four lanes,
      // today's defaults, and the most the contract allows.
      const cases = [{ m: 19456, t: 1, p: 1 }, { m: 19456, t: 2, p: 1 }, { m: 19456, t: 1, p: 4 },
        { m: 65536, t: 3, p: 1 }, { m: 1048576, t: 10, p: 1 }];
      for (const c of cases) {
        let built = 0;
        let ended = 0;
        const asked = [];
        globalThis.Worker = class {
          constructor() { built += 1; }
          postMessage() {}
          terminate() { ended += 1; }
        };
        globalThis.setTimeout = function (fn, ms, ...rest) {
          asked.push(ms);
          return realTimeout.call(globalThis, fn, 0, ...rest);
        };
        let answer = "no error";
        try {
          await p.deriveSeed("Heimnetz-2026", { alg: "argon2id", salt, m: c.m, t: c.t, p: c.p });
          answer = "derived something";
        } catch (e) {
          answer = e.constructor.name;
        } finally {
          globalThis.setTimeout = realTimeout;
          globalThis.Worker = realWorker;
        }
        runs.push({ asked, answer, built, ended });
      }
      return runs;
    });
    // Ten seconds of start-up and half a millisecond per KiB-pass: 19 728,
    // 29 456, 19 728 again because p does not count, and 108 304. The last
    // one would come to 5 252 880 - twenty-two minutes, which is no deadline
    // at all for the person sitting in front of it - and stops at 120 000.
    expect(got.map((r) => r.asked)).toEqual([[19_728], [29_456], [19_728], [108_304], [120_000]]);
    // Every one of them ends the same way: exactly one worker, ended, so
    // nothing of it and nothing in it is left running, and the caller is told
    // instead of left waiting.
    expect(got.map((r) => r.built)).toEqual([1, 1, 1, 1, 1]);
    expect(got.map((r) => r.ended)).toEqual([1, 1, 1, 1, 1]);
    expect(got.map((r) => r.answer)).toEqual(["BadProof", "BadProof", "BadProof", "BadProof", "BadProof"]);
  });

// The three vectors of proof_kdf.json all say m=65536, t=3, p=1, so nothing
// in this repository holds Argon2id against a parallelism above one - and the
// way the lanes are woven together is exactly where implementations have gone
// apart in the past, while validateKDF allows p up to 4.
//
// Until the shared vectors cover it, this case holds the copied library
// against the published test vector of RFC 9106 itself (section 5.3,
// Argon2id, version 0x13, p=4 lanes, m=32, t=3, with a secret and associated
// data). It goes at the library directly and not through deriveSeed, because
// m=32 is far under the floor the contract sets - the point here is the lane
// arithmetic, not the contract.
test("the copied Argon2id agrees with the test vector of RFC 9106, lanes and all", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const { argon2id } = await import("/assets/vendor/noble-hashes/argon2.js");
    const hex = (u) => Array.from(u).map((b) => b.toString(16).padStart(2, "0")).join("");
    const tag = argon2id(new Uint8Array(32).fill(1), new Uint8Array(16).fill(2), {
      m: 32, t: 3, p: 4, dkLen: 32, version: 0x13,
      key: new Uint8Array(8).fill(3), personalization: new Uint8Array(12).fill(4),
    });
    return hex(tag);
  });
  expect(got).toBe("0d640df58d78766c08c037a34a8b53c9d01ef0452d75b65eb52520e96b01e659");
});

// Proof.Validate of Go, rule by rule. There are no vectors for what has to be
// refused, so every rule of the Go source stands here as its own case.
test("the limits of a proof hold", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const p = await import("/assets/js/contracts/proof.js");
    const kid = "AAAAAAAAAAAAAAAAAAAAAA";
    const base = { kind: "passkey", kid, sig: new Uint8Array(64), authData: new Uint8Array(37), clientData: new Uint8Array(10) };
    const password = { kind: "password", kid, sig: new Uint8Array(64), authData: new Uint8Array(0), clientData: new Uint8Array(0) };
    const cases = [
      ["a whole passkey proof", base, true],
      ["a whole password proof", password, true],
      ["a password proof without the two fields at all", { kind: "password", kid, sig: new Uint8Array(64) }, true],
      ["a key id of 4 characters", { ...base, kid: "AAAA" }, false],
      ["a key id of 21 characters", { ...base, kid: kid.slice(0, 21) }, false],
      ["a key id of 23 characters", { ...base, kid: kid + "A" }, false],
      ["a key id outside the alphabet", { ...base, kid: "+" + kid.slice(1) }, false],
      ["a key id with bits left over", { ...base, kid: kid.slice(0, 21) + "B" }, false],
      ["a key id that is no text", { ...base, kid: 22 }, false],
      ["a key id that is missing", { kind: "passkey", sig: base.sig, authData: base.authData, clientData: base.clientData }, false],
      ["a signature of no bytes", { ...base, sig: new Uint8Array(0) }, false],
      ["a signature of 1 byte", { ...base, sig: new Uint8Array(1) }, true],
      ["a signature of 1024 bytes", { ...base, sig: new Uint8Array(1024) }, true],
      ["a signature of 1025 bytes", { ...base, sig: new Uint8Array(1025) }, false],
      ["a signature that is no bytes", { ...base, sig: "AAAA" }, false],
      ["a signature that is missing", { kind: "passkey", kid, authData: base.authData, clientData: base.clientData }, false],
      ["authenticator data of 36 bytes", { ...base, authData: new Uint8Array(36) }, false],
      ["authenticator data of 4096 bytes", { ...base, authData: new Uint8Array(4096) }, true],
      ["authenticator data of 4097 bytes", { ...base, authData: new Uint8Array(4097) }, false],
      // Text has a length just as bytes do, and 40 characters would walk
      // through every length check here if it were not refused for what it is.
      ["authenticator data that is text of 40 characters", { ...base, authData: "a".repeat(40) }, false],
      ["client data of no bytes", { ...base, clientData: new Uint8Array(0) }, false],
      ["client data of 4096 bytes", { ...base, clientData: new Uint8Array(4096) }, true],
      ["client data of 4097 bytes", { ...base, clientData: new Uint8Array(4097) }, false],
      ["client data that is text", { ...base, clientData: "{}" }, false],
      ["a password proof with authenticator data", { ...base, kind: "password" }, false],
      ["a password proof with client data alone", { ...password, clientData: new Uint8Array(1) }, false],
      ["a passkey proof without authenticator data", { kind: "passkey", kid, sig: base.sig, clientData: base.clientData }, false],
      ["a kind nobody has", { ...base, kind: "token" }, false],
      ["a kind that is missing", { kid, sig: base.sig, authData: base.authData, clientData: base.clientData }, false],
      ["a kind that is no text", { ...base, kind: 1 }, false],
      ["no proof at all", null, false],
      ["a proof that is text", "proof", false],
      ["a proof that is a number", 7, false],
      ["a proof that is missing", undefined, false],
      // Fields come from an object the caller owns, so they may fight back.
      ["a signature behind a getter that throws", { ...base, get sig() { throw new TypeError("no"); } }, false],
      ["a proof behind a proxy that throws", new Proxy({ ...base }, { get: () => { throw new TypeError("no"); } }), false],
    ];
    const wrong = [];
    for (const [name, pr, want] of cases) {
      let got;
      try {
        got = p.validateProof(pr);
      } catch (e) {
        wrong.push(`${name}: threw ${e.constructor.name}: ${e.message}`);
        continue;
      }
      if (got !== want) wrong.push(`${name}: answered ${JSON.stringify(got)}, ${want} wanted`);
    }
    return { wrong, limits: [p.MAX_SIG, p.MAX_AUTH_DATA, p.MAX_CLIENT_DATA, p.MAX_PUBLIC, p.MAX_RSA_BITS, p.MAX_CRED_ID] };
  });
  expect(out.wrong).toEqual([]);
  // The numbers, as the Go source has them.
  expect(out.limits).toEqual([1024, 4096, 4096, 1024, 4096, 1023]);
});

// KDF.Validate of Go. Go reads m, t and p into whole numbers and can be
// handed nothing else; here a caller can hand over text, and "65536" compares
// to true against every bound there is.
test("the Argon2id parameters are decided as Go decides them", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const p = await import("/assets/js/contracts/proof.js");
    const sign = await import("/assets/js/contracts/sign.js");
    const { b64encode } = await import("/assets/js/contracts/b64.js");
    const salt = new Uint8Array(16);
    const good = { alg: "argon2id", salt, m: 65536, t: 3, p: 1 };
    const cases = [
      ["the parameters NGC uses", good, true],
      ["the smallest memory there is", { ...good, m: 19456 }, true],
      ["one KiB under it", { ...good, m: 19455 }, false],
      ["the largest memory there is", { ...good, m: 1048576 }, true],
      ["one KiB over it", { ...good, m: 1048577 }, false],
      ["a memory that is text", { ...good, m: "65536" }, false],
      ["a memory that is no whole number", { ...good, m: 65536.5 }, false],
      ["a memory of NaN", { ...good, m: NaN }, false],
      ["a memory of Infinity", { ...good, m: Infinity }, false],
      ["a memory that is missing", { alg: "argon2id", salt, t: 3, p: 1 }, false],
      ["a memory that is a big integer", { ...good, m: BigInt(65536) }, false],
      ["one pass", { ...good, t: 1 }, true],
      ["no pass", { ...good, t: 0 }, false],
      ["ten passes", { ...good, t: 10 }, true],
      ["eleven passes", { ...good, t: 11 }, false],
      ["a pass count that is text", { ...good, t: "3" }, false],
      ["a pass count that is true", { ...good, t: true }, false],
      ["one lane", { ...good, p: 1 }, true],
      ["no lane", { ...good, p: 0 }, false],
      ["four lanes", { ...good, p: 4 }, true],
      ["five lanes", { ...good, p: 5 }, false],
      ["a salt of 15 bytes", { ...good, salt: new Uint8Array(15) }, false],
      ["a salt of 17 bytes", { ...good, salt: new Uint8Array(17) }, false],
      ["a salt that is text", { ...good, salt: "AQIDBAUGBwgJCgsMDQ4PEA" }, false],
      ["a salt that is missing", { alg: "argon2id", m: 65536, t: 3, p: 1 }, false],
      ["another algorithm", { ...good, alg: "argon2i" }, false],
      ["an algorithm that is missing", { salt, m: 65536, t: 3, p: 1 }, false],
      ["no parameters at all", null, false],
      ["parameters that are missing", undefined, false],
      ["parameters that are text", "argon2id", false],
      ["a memory behind a getter that throws", { ...good, get m() { throw new TypeError("no"); } }, false],
    ];
    const wrong = [];
    for (const [name, k, want] of cases) {
      let got;
      try {
        got = p.validateKDF(k);
      } catch (e) {
        wrong.push(`${name}: threw ${e.constructor.name}: ${e.message}`);
        continue;
      }
      if (got !== want) wrong.push(`${name}: answered ${JSON.stringify(got)}, ${want} wanted`);
    }
    // saltBytes reads the salt as it stands in a document: base64url, as
    // strictly as everywhere else, and no second spelling of it.
    const saltRefused = [];
    for (const bad of ["a b", "AQIDBAUGBwgJCgsMDQ4PEA=", 7, undefined]) {
      try {
        p.saltBytes(bad);
        saltRefused.push(String(bad));
      } catch (e) {
        if (e.constructor.name !== "BadInput") saltRefused.push(`threw ${e.constructor.name}`);
      }
    }
    return {
      wrong, saltRefused,
      numbers: [p.SALT_SIZE, p.MIN_MEM_KIB, p.MAX_MEM_KIB, p.DEFAULT_MEM_KIB, p.DEFAULT_TIME, p.DEFAULT_PAR],
      salt: b64encode(p.saltBytes("AQIDBAUGBwgJCgsMDQ4PEA")).length,
      alg: p.KDF_ARGON2ID,
      kinds: [p.KIND_PASSWORD, p.KIND_PASSKEY, p.ALG_EDDSA, p.ALG_ES256, p.ALG_RS256],
      // MAX_PAYLOAD is the number sign holds, handed on from here - not a
      // second one that could drift away from it.
      onePayload: p.MAX_PAYLOAD === sign.MAX_PAYLOAD,
    };
  });
  expect(out.wrong).toEqual([]);
  expect(out.saltRefused).toEqual([]);
  expect(out.numbers).toEqual([16, 19456, 1048576, 65536, 3, 1]);
  expect(out.salt).toBe(22);
  expect(out.alg).toBe("argon2id");
  // The words, letter for letter as the Go source has them.
  expect(out.kinds).toEqual(["password", "passkey", "EdDSA", "ES256", "RS256"]);
  expect(out.onePayload).toBe(true);
});

// UserKey.Validate of Go: the id is the hash of the public key, a password
// key is an Ed25519 key of 32 bytes with a KDF and no credential id, a
// passkey has a credential id of 1 to 1023 bytes, no KDF and a public key the
// engine can import for the algorithm it names.
test("a user key is decided as Go decides it", async ({ shell }) => {
  const out = await shell.evaluate(async (d) => {
    const p = await import("/assets/js/contracts/proof.js");
    const s = await import("/assets/js/contracts/sign.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const pw = d.proofs.find((c) => c.key.kind === "password").key;
    const pk = d.proofs.find((c) => c.key.kind === "passkey" && c.key.alg === "ES256").key;
    const password = { ...pw, pub: b64decode(pw.pub), kdf: { ...pw.kdf, salt: b64decode(pw.kdf.salt) } };
    const passkey = { ...pk, pub: b64decode(pk.pub), credId: b64decode(pk.credId) };
    const other = b64decode(pw.pub).slice();
    other[0] ^= 1;
    const cases = [
      ["the password key of proof.json", password, true],
      ["the passkey of proof.json", passkey, true],
      ["a password key whose id is not its hash", { ...password, id: await s.keyID(other) }, false],
      ["a password key with no id", { ...password, id: undefined }, false],
      ["a password key with an id that is no text", { ...password, id: 7 }, false],
      ["a password key of another algorithm", { ...password, alg: "ES256" }, false],
      ["a password key without a KDF", { ...password, kdf: undefined }, false],
      ["a password key with a KDF out of range", { ...password, kdf: { ...password.kdf, m: 1 } }, false],
      ["a password key with a credential id", { ...password, credId: new Uint8Array(1) }, false],
      ["a password key whose public key is no bytes", { ...password, pub: pw.pub }, false],
      ["a passkey without a credential id", { ...passkey, credId: new Uint8Array(0) }, false],
      ["a passkey whose credential id is missing", { ...passkey, credId: undefined }, false],
      ["a passkey with a credential id of 1023 bytes", { ...passkey, credId: new Uint8Array(1023) }, true],
      ["a passkey with a credential id of 1024 bytes", { ...passkey, credId: new Uint8Array(1024) }, false],
      ["a passkey with a KDF", { ...passkey, kdf: password.kdf }, false],
      ["a passkey whose algorithm is not its key", { ...passkey, alg: "RS256" }, false],
      ["a passkey of an algorithm nobody has", { ...passkey, alg: "HS256" }, false],
      ["a passkey whose public key is no key", { ...passkey, pub: new Uint8Array(91) }, false],
      ["a kind nobody has", { ...password, kind: "token" }, false],
      ["a kind that is missing", { ...password, kind: undefined }, false],
      ["no key at all", null, false],
      ["a key that is text", "key", false],
      ["a key that is missing", undefined, false],
      ["a public key behind a getter that throws", { ...password, get pub() { throw new TypeError("no"); } }, false],
      ["a key behind a proxy that throws", new Proxy({ ...password }, { get: () => { throw new TypeError("no"); } }), false],
    ];
    const wrong = [];
    for (const [name, k, want] of cases) {
      let got;
      try {
        got = await p.validateUserKey(k);
      } catch (e) {
        wrong.push(`${name}: threw ${e.constructor.name}: ${e.message}`);
        continue;
      }
      if (got !== want) wrong.push(`${name}: answered ${JSON.stringify(got)}, ${want} wanted`);
    }
    return wrong;
  }, v);
  expect(out).toEqual([]);
});

// The DER of an ES256 signature, as strictly as Go reads it: one SEQUENCE
// that fills the signature, two integers in their shortest form and nothing
// behind them. Every other spelling is a second name for the same two
// numbers, and Go's VerifyASN1 refuses it - so this one has to as well, or a
// proof the API refuses would pass here.
test("an ES256 signature is read in one spelling and no other", async ({ shell }) => {
  const out = await shell.evaluate(async (d) => {
    const p = await import("/assets/js/contracts/proof.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const c = d.proofs.find((x) => x.valid && x.key.alg === "ES256");
    const key = { ...c.key, pub: b64decode(c.key.pub), credId: b64decode(c.key.credId) };
    const payload = b64decode(c.payload);
    const proof = {
      kind: c.proof.kind, kid: c.proof.kid, sig: b64decode(c.proof.sig),
      authData: b64decode(c.proof.authData), clientData: b64decode(c.proof.clientData),
    };
    // The signature is split and put back together here, by hand, so the
    // rules below are the test's and not the module's.
    const der = proof.sig;
    const rLen = der[3];
    const r = Array.from(der.subarray(4, 4 + rLen));
    const sLen = der[4 + rLen + 1];
    const s = Array.from(der.subarray(4 + rLen + 2, 4 + rLen + 2 + sLen));
    const build = (rr, ss) => {
      const body = [0x02, rr.length, ...rr, 0x02, ss.length, ...ss];
      return new Uint8Array([0x30, body.length, ...body]);
    };
    const withByte = (u8, i, value) => { const out = Uint8Array.from(u8); out[i] = value; return out; };
    const sigs = [
      ["the signature as Go wrote it", der, true],
      ["the same signature built again from its two numbers", build(r, s), true],
      ["a zero in front of r that nothing needs", build([0, ...r], s), false],
      ["a zero in front of s that nothing needs", build(r, [0, ...s]), false],
      ["one byte behind the sequence", new Uint8Array([...der, 0]), false],
      ["a sequence one byte shorter than it says", withByte(der, 1, der[1] - 1), false],
      ["a sequence one byte longer than it says", withByte(der, 1, der[1] + 1), false],
      ["a tag that is no sequence", withByte(der, 0, 0x31), false],
      ["an r that is no integer", withByte(der, 2, 0x04), false],
      ["r of no bytes", build([], s), false],
      ["r of 33 bytes without the zero that would need it", build(new Array(33).fill(1), s), false],
      ["no signature at all", new Uint8Array(0), false],
      ["a signature of one byte", new Uint8Array([0x30]), false],
      ["a signature that is no bytes", "MEUCIQ", false],
    ];
    const wrong = [];
    for (const [name, sig, want] of sigs) {
      let got;
      try {
        got = await p.verify({ ...proof, sig }, key, d.rp, c.purpose, payload);
      } catch (e) {
        wrong.push(`${name}: threw ${e.constructor.name}: ${e.message}`);
        continue;
      }
      if (got !== want) wrong.push(`${name}: answered ${JSON.stringify(got)}, ${want} wanted`);
    }
    return wrong;
  }, v);
  expect(out).toEqual([]);
});

// verify has to answer false and may never throw. A caller that had to catch
// an exception around a proof is a caller that lets one through as soon as
// the engine, or whoever built the proof, surprises it.
test("verify answers false and never throws for anything but a right proof", async ({ shell }) => {
  const out = await shell.evaluate(async (d) => {
    const p = await import("/assets/js/contracts/proof.js");
    const { b64decode, utf8 } = await import("/assets/js/contracts/b64.js");
    const c = d.proofs.find((x) => x.valid && x.key.alg === "ES256");
    const key = { ...c.key, pub: b64decode(c.key.pub), credId: b64decode(c.key.credId) };
    const payload = b64decode(c.payload);
    const proof = {
      kind: c.proof.kind, kid: c.proof.kid, sig: b64decode(c.proof.sig),
      authData: b64decode(c.proof.authData), clientData: b64decode(c.proof.clientData),
    };
    const boom = () => { throw new TypeError("this field fights back"); };
    const trap = (u8) => new Proxy(u8, { get: (t, k) => (k === "length" ? boom() : Reflect.get(t, k)) });
    const hidden = (u8) => new Proxy(u8, { getPrototypeOf: boom });
    // The client data is the one document read leniently, so it is the one
    // place a value of a shape nobody expected reaches a field access.
    const clientDatas = ["null", "[]", '"webauthn.get"', "0", "true", "", "{", "{}",
      '{"type":"webauthn.get"}', '{"__proto__":{"type":"webauthn.get"}}'];
    const cases = [
      ["no proof", undefined, key, d.rp, c.purpose, payload],
      ["a proof that is null", null, key, d.rp, c.purpose, payload],
      ["a proof that is text", "proof", key, d.rp, c.purpose, payload],
      ["no key", proof, undefined, d.rp, c.purpose, payload],
      ["a key that is null", proof, null, d.rp, c.purpose, payload],
      ["a key that is text", proof, "key", d.rp, c.purpose, payload],
      ["no relying party", proof, key, undefined, c.purpose, payload],
      ["a relying party that is null", proof, key, null, c.purpose, payload],
      ["a relying party without origins", proof, key, { id: d.rp.id }, c.purpose, payload],
      ["origins that are no list", proof, key, { id: d.rp.id, origins: d.rp.origins[0] }, c.purpose, payload],
      ["an id that is no text", proof, key, { id: 7, origins: d.rp.origins }, c.purpose, payload],
      ["an id that is missing", proof, key, { origins: d.rp.origins }, c.purpose, payload],
      ["no purpose", proof, key, d.rp, undefined, payload],
      ["a purpose that is a number", proof, key, d.rp, 7, payload],
      ["a purpose with a toString", proof, key, d.rp, { toString: () => c.purpose }, payload],
      ["no payload", proof, key, d.rp, c.purpose, undefined],
      ["a payload that is text", proof, key, d.rp, c.purpose, c.payload],
      ["a signature that is missing", { ...proof, sig: undefined }, key, d.rp, c.purpose, payload],
      ["a signature of 1025 bytes", { ...proof, sig: new Uint8Array(1025) }, key, d.rp, c.purpose, payload],
      ["a public key that is missing", proof, { ...key, pub: undefined }, d.rp, c.purpose, payload],
      ["authenticator data that is missing", { ...proof, authData: undefined }, key, d.rp, c.purpose, payload],
      ["client data that is missing", { ...proof, clientData: undefined }, key, d.rp, c.purpose, payload],
      ["a signature whose length throws", { ...proof, sig: trap(proof.sig) }, key, d.rp, c.purpose, payload],
      ["a public key no instanceof can look at", proof, { ...key, pub: hidden(key.pub) }, d.rp, c.purpose, payload],
      ["a proof behind a proxy that throws", new Proxy({ ...proof }, { get: boom }), key, d.rp, c.purpose, payload],
      ["a key behind a proxy that throws", proof, new Proxy({ ...key }, { get: boom }), d.rp, c.purpose, payload],
      ["a relying party behind a proxy that throws", proof, key, new Proxy({ ...d.rp }, { get: boom }), c.purpose, payload],
      ["a kind behind a getter that throws", { ...proof, get kind() { return boom(); } }, key, d.rp, c.purpose, payload],
    ];
    for (const text of clientDatas) {
      cases.push([`client data that is ${JSON.stringify(text)}`, { ...proof, clientData: utf8(text) }, key, d.rp, c.purpose, payload]);
    }
    // Client data that is no UTF-8 at all.
    cases.push(["client data that is no UTF-8", { ...proof, clientData: new Uint8Array([0xff, 0xfe]) }, key, d.rp, c.purpose, payload]);
    const wrong = [];
    for (const [name, pr, k, rp, purpose, pay] of cases) {
      try {
        const got = await p.verify(pr, k, rp, purpose, pay);
        if (got !== false) wrong.push(`${name}: answered ${JSON.stringify(got)}`);
      } catch (e) {
        wrong.push(`${name}: threw ${e.constructor.name}: ${e.message}`);
      }
    }
    // And the whole proof still comes out true afterwards, so the cases above
    // prove the answers and not a broken page.
    const afterwards = await p.verify(proof, key, d.rp, c.purpose, payload);
    // A shape of a contract is a constant of that contract: whoever could
    // change one would decide what every parser of it reads. The lock sits
    // where the shape is built (json.js: S, P, A). The code of this call is
    // no module and no strict mode, so a write to a frozen object is dropped
    // without a word - what counts is that the shape is the same afterwards.
    const opened = [];
    for (const [what, shape, change] of [
      ["a member added to proofShape", p.proofShape, (sh) => { sh.m.extra = { k: "str" }; }],
      ["a member replaced in userKeyShape", p.userKeyShape, (sh) => { sh.m.pub = { k: "str" }; }],
      ["a member added to kdfShape", p.kdfShape, (sh) => { sh.m.extra = { k: "str" }; }],
      ["the member of rpShape replaced", p.rpShape, (sh) => { sh.m.origins = { k: "str" }; }],
    ]) {
      const before = JSON.stringify(shape);
      try {
        change(shape);
      } catch {
        // A module is strict and a frozen shape throws there.
      }
      if (JSON.stringify(shape) !== before) opened.push(what);
    }
    return { wrong, afterwards, opened };
  }, v);
  expect(out.wrong).toEqual([]);
  expect(out.afterwards).toBe(true);
  expect(out.opened).toEqual([]);
});

// Every field of a proof, of a key and of the relying party is read once and
// only the copy is used after that. Go is handed structs whose fields are
// bytes and text and answer every read the same way; here a field can be a
// getter, and a getter that answers the length check with 64 bytes and the
// Web Crypto API with a megabyte would carry the limit away between the two
// reads. One read is the only shape of that rule that holds by itself.
test("a proof, a key and the relying party are read once", async ({ shell }) => {
  const out = await shell.evaluate(async (d) => {
    const p = await import("/assets/js/contracts/proof.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const c = d.proofs.find((x) => x.valid && x.key.alg === "ES256");
    const key = { ...c.key, pub: b64decode(c.key.pub), credId: b64decode(c.key.credId) };
    const payload = b64decode(c.payload);
    const proof = {
      kind: c.proof.kind, kid: c.proof.kid, sig: b64decode(c.proof.sig),
      authData: b64decode(c.proof.authData), clientData: b64decode(c.proof.clientData),
    };
    // Every member of the original stands behind a getter that counts, and
    // the value it hands out never changes - so the run below is the one that
    // has to come out true, and the counts are the only thing under test.
    const counted = (o) => {
      const reads = {};
      const out = {};
      for (const name of Object.keys(o)) {
        reads[name] = 0;
        Object.defineProperty(out, name, {
          get() { reads[name]++; return o[name]; }, enumerable: true, configurable: true,
        });
      }
      return [out, reads];
    };
    const [pr, prReads] = counted(proof);
    const [k, keyReads] = counted(key);
    const [rp, rpReads] = counted(d.rp);
    const verified = await p.verify(pr, k, rp, c.purpose, payload);
    const twice = [];
    for (const [what, reads] of [["proof", prReads], ["key", keyReads], ["rp", rpReads]]) {
      for (const [name, n] of Object.entries(reads)) {
        if (n > 1) twice.push(`${what}.${name} was read ${n} times`);
      }
    }
    return { verified, twice, prReads, keyReads, rpReads };
  }, v);
  expect(out.twice).toEqual([]);
  expect(out.verified).toBe(true);
  // The fields a passkey proof really needs were read, so the case above is
  // no proof that nothing at all happened.
  expect(out.prReads.sig).toBe(1);
  expect(out.prReads.clientData).toBe(1);
  expect(out.prReads.authData).toBe(1);
  expect(out.keyReads.pub).toBe(1);
  expect(out.rpReads.origins).toBe(1);
});

// The three tests below close the gap the counting test above leaves open. Its
// name promises the byte object is read once, but it only counts reads of the
// FIELD NAMES on the container: every object it hands over answers each field
// with one unchanging value. The forgery needs no getter on the container at
// all - it is an ordinary Uint8Array whose bytes are swapped in a microtask
// that lands between the id and the signature, out of reach of any read count.
// Each of these was true on the parent of this change (a forgery accepted),
// measured in chromium and node, and is false after it. bytes.spec.js does not
// walk proof.verify, so nothing else held these.

test("proof.verify reads the password key once, so the id and the signature are one key's", async ({ shell }) => {
  test.skip(!(await hasEd25519(shell, passwordPub)),
    "no Ed25519 in this engine's Web Crypto: it answers false for everything, hostile or not");
  const got = await shell.evaluate(async () => {
    const p = await import("/assets/js/contracts/proof.js");
    const s = await import("/assets/js/contracts/sign.js");
    const purpose = "clientcert/v1";
    const payload = new Uint8Array([1, 2, 3, 4, 5]);
    const msg = s.message(purpose, payload);
    const mine = await crypto.subtle.generateKey({ name: "Ed25519" }, true, ["sign", "verify"]);
    const trusted = await crypto.subtle.generateKey({ name: "Ed25519" }, true, ["sign", "verify"]);
    const minePub = new Uint8Array(await crypto.subtle.exportKey("raw", mine.publicKey));
    const trustedPub = new Uint8Array(await crypto.subtle.exportKey("raw", trusted.publicKey));
    const mineSig = new Uint8Array(await crypto.subtle.sign({ name: "Ed25519" }, mine.privateKey, msg));
    const idMine = await s.keyID(minePub);
    const idTrusted = await s.keyID(trustedPub);
    const out = {};
    // The attacker's signature is genuine under his own key and no good under
    // the trusted one, so a false below is a refusal of the swap.
    out.signatureIsGenuine = await p.verify({ kind: "password", kid: idMine, sig: mineSig },
      { id: idMine, kind: "password", alg: "EdDSA", pub: minePub }, null, purpose, payload);
    out.signatureIsNotTheTrustedKeys = await s.verifyRaw(trustedPub, mineSig, msg);
    // The proof names the TRUSTED key and carries the ATTACKER's signature. The
    // key is trusted now, the attacker's one microtask later - between the id
    // and the signature.
    const twoFaced = new Uint8Array(trustedPub.length);
    twoFaced.set(trustedPub);
    out.plainArray = Object.getPrototypeOf(twoFaced) === Uint8Array.prototype;
    queueMicrotask(() => twoFaced.set(minePub));
    out.accepted = await p.verify({ kind: "password", kid: idTrusted, sig: mineSig },
      { id: idTrusted, kind: "password", alg: "EdDSA", pub: twoFaced }, null, purpose, payload);
    out.swapHappened = Array.from(twoFaced).join() === Array.from(minePub).join();
    return out;
  });
  expect(got.signatureIsGenuine).toBe(true);
  expect(got.signatureIsNotTheTrustedKeys).toBe(false);
  expect(got.plainArray).toBe(true);
  expect(got.swapHappened).toBe(true);
  expect(got.accepted).toBe(false);
});

test("proof.verify reads the passkey once, so the id and the assertion are one key's", async ({ shell }) => {
  test.skip(!(await hasEd25519(shell, passwordPub)),
    "no Ed25519 in this engine's Web Crypto: it answers false for everything, hostile or not");
  const got = await shell.evaluate(async () => {
    const p = await import("/assets/js/contracts/proof.js");
    const s = await import("/assets/js/contracts/sign.js");
    const b64 = await import("/assets/js/contracts/b64.js");
    const enc = new TextEncoder();
    const purpose = "clientcert/v1";
    const payload = new Uint8Array([1, 2, 3, 4, 5]);
    const rpID = "ex.test", origin = "https://ex.test";
    const mine = await crypto.subtle.generateKey({ name: "Ed25519" }, true, ["sign", "verify"]);
    const trusted = await crypto.subtle.generateKey({ name: "Ed25519" }, true, ["sign", "verify"]);
    const mineSpki = new Uint8Array(await crypto.subtle.exportKey("spki", mine.publicKey));
    const trustedSpki = new Uint8Array(await crypto.subtle.exportKey("spki", trusted.publicKey));
    const ch = await p.challenge(purpose, payload);
    const client = enc.encode(JSON.stringify({ type: "webauthn.get", challenge: b64.b64encode(ch), origin }));
    const rpHash = new Uint8Array(await crypto.subtle.digest("SHA-256", enc.encode(rpID)));
    const auth = new Uint8Array(37); auth.set(rpHash, 0); auth[32] = 0x05; auth[36] = 1;
    const cdHash = new Uint8Array(await crypto.subtle.digest("SHA-256", client));
    const signed = new Uint8Array(auth.length + cdHash.length); signed.set(auth, 0); signed.set(cdHash, auth.length);
    const mineSig = new Uint8Array(await crypto.subtle.sign({ name: "Ed25519" }, mine.privateKey, signed));
    const rp = { id: rpID, origins: [origin] };
    const credId = new Uint8Array([9, 9, 9, 9]);
    const idMine = await s.keyID(mineSpki);
    const idTrusted = await s.keyID(trustedSpki);
    const out = {};
    // The attacker's assertion is genuine under his own key.
    out.honest = await p.verify({ kind: "passkey", kid: idMine, sig: mineSig, authData: auth, clientData: client },
      { id: idMine, kind: "passkey", alg: "EdDSA", pub: mineSpki, credId }, rp, purpose, payload);
    // The proof names the TRUSTED key; the key imports as the attacker's one
    // microtask later, between the id and the import that checks the assertion.
    const twoFaced = new Uint8Array(trustedSpki.length);
    twoFaced.set(trustedSpki);
    queueMicrotask(() => twoFaced.set(mineSpki));
    out.accepted = await p.verify({ kind: "passkey", kid: idTrusted, sig: mineSig, authData: auth, clientData: client },
      { id: idTrusted, kind: "passkey", alg: "EdDSA", pub: twoFaced, credId }, rp, purpose, payload);
    out.swapHappened = Array.from(twoFaced).join() === Array.from(mineSpki).join();
    return out;
  });
  expect(got.honest).toBe(true);
  expect(got.swapHappened).toBe(true);
  expect(got.accepted).toBe(false);
});

test("validateUserKey reads the key once, so the id and the import are one key's", async ({ shell }) => {
  test.skip(!(await hasEd25519(shell, passwordPub)),
    "no Ed25519 in this engine's Web Crypto: it answers false for everything, hostile or not");
  const got = await shell.evaluate(async () => {
    const p = await import("/assets/js/contracts/proof.js");
    const s = await import("/assets/js/contracts/sign.js");
    const mine = await crypto.subtle.generateKey({ name: "Ed25519" }, true, ["sign", "verify"]);
    const mineSpki = new Uint8Array(await crypto.subtle.exportKey("spki", mine.publicKey));
    const idMine = await s.keyID(mineSpki);
    // A is junk that does NOT import as an EdDSA key; the id certifies A. B is
    // the attacker's real key, which DOES import. A and B share length so set
    // works. Read twice, the id is over A and the import is of B - a key whose
    // id and imported half are two different keys. Read once, A is used for
    // both and importPublic(A) is null.
    const A = new Uint8Array(mineSpki.length).fill(0xAB);
    const idA = await s.keyID(A);
    const out = {};
    out.honest = await p.validateUserKey({ id: idMine, kind: "passkey", alg: "EdDSA", pub: mineSpki, credId: new Uint8Array([1, 2, 3]) });
    const twoFaced = new Uint8Array(A.length);
    twoFaced.set(A);
    queueMicrotask(() => twoFaced.set(mineSpki));
    out.accepted = await p.validateUserKey({ id: idA, kind: "passkey", alg: "EdDSA", pub: twoFaced, credId: new Uint8Array([1, 2, 3]) });
    out.swapHappened = Array.from(twoFaced).join() === Array.from(mineSpki).join();
    return out;
  });
  expect(got.honest).toBe(true);
  expect(got.swapHappened).toBe(true);
  expect(got.accepted).toBe(false);
});

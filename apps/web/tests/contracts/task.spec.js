import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

const v = vectors("task.json");

// Does the Web Crypto API of this engine know Ed25519? The three tests that
// check a signature need it twice over: trust.parseList imports this list's
// passkey as spki, and sign.verify imports the certificate's session key as
// raw. The WebKit that Playwright brings has neither - measured in task 8 for
// this same list - and then no right signature can come out true, which is
// the right answer for an engine that cannot check and no test of this
// contract. The two tests that decide on form alone need no crypto and run on
// every engine.
//
// Probed with a real key out of this vector's own list, the way session.spec.js
// and trust.spec.js probe: an engine that has Ed25519 but rejects a made-up
// 32-byte value would otherwise be skipped for the wrong reason.
async function hasEd25519(shell, signed) {
  return shell.evaluate(async (s) => {
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const list = JSON.parse(new TextDecoder().decode(b64decode(s.payload)));
    let pub = null;
    for (const u of list.users) {
      for (const k of u.keys) {
        if (k.kind === "password") pub = k.pub;
      }
    }
    // Outside the try on purpose: a vector file that loses its password key
    // must fail loudly instead of skipping both tests as "no Ed25519 here".
    if (typeof pub !== "string") throw new Error("task.json's trust list holds no password key to probe with");
    try {
      await crypto.subtle.importKey("raw", b64decode(pub), { name: "Ed25519" }, false, ["verify"]);
      return true;
    } catch {
      return false;
    }
  }, signed);
}

test("a task signs with the session key and binds to the certificate", async ({ shell }) => {
  test.setTimeout(120_000);
  test.skip(!(await hasEd25519(shell, v.trust)),
    "no Ed25519 in this engine's Web Crypto: it reads none of this list's passkeys and checks no signature");
  const out = await shell.evaluate(async (d) => {
    const task = await import("/assets/js/contracts/task.js");
    const session = await import("/assets/js/contracts/session.js");
    const sign = await import("/assets/js/contracts/sign.js");
    const trust = await import("/assets/js/contracts/trust.js");
    const proof = await import("/assets/js/contracts/proof.js");
    const { b64decode, b64encode, equal } = await import("/assets/js/contracts/b64.js");

    const list = await trust.parseList(b64decode(d.trust.payload));
    const client = b64decode(d.client);
    const env = { payload: b64decode(d.task.payload), kid: d.task.kid, sig: b64decode(d.task.sig) };
    const cert = JSON.parse(new TextDecoder().decode(client));
    // The task is signed with the session key of the certificate (its sig
    // member) and bound to the exact certificate bytes.
    const good = await sign.verify(env, b64decode(cert.sig), sign.PURPOSE_TASK);
    const t = task.parseTask(env.payload);
    const bound = equal(t.cert, await session.certHash(client));

    // The shell can rebuild exactly these bytes from the fields.
    const rebuilt = task.encodeTask(t);

    // A re-confirmation is a proof over the task payload, purpose confirm/v1.
    const confirm = {
      kind: d.confirm.kind, kid: d.confirm.kid, sig: b64decode(d.confirm.sig),
      authData: d.confirm.authData ? b64decode(d.confirm.authData) : new Uint8Array(0),
      clientData: d.confirm.clientData ? b64decode(d.confirm.clientData) : new Uint8Array(0),
    };
    return {
      good, bound, rebuilt: equal(rebuilt, env.payload), valid: task.validateTask(t),
      confirmed: await task.verifyConfirm(confirm, env, list, cert.user),
      confirmOther: await task.verifyConfirm(confirm, { ...env, payload: new Uint8Array([123, 125]) },
        list, cert.user),
    };
  }, v);
  expect(out).toEqual({ good: true, bound: true, rebuilt: true, valid: true, confirmed: true, confirmOther: false });
});

// The 27 cases below were not thought out: each want comes from Go itself,
// from task.Task.Validate run over the same field in the same shape. Every
// bound is nailed down from both sides - 8192 bytes of params go through and
// 8193 do not, a lifetime of 300 seconds goes through and 301 does not, 16
// bytes of id and 32 of cert go through and one less or one more does not -
// because a bound that is only refused from above would still hold if it
// stood at zero.
//
// Go writes Params as json.RawMessage, and wire.ValidParams asks three things
// of it that the brief's validateTask asked of nothing: at most
// wire.MaxParams bytes, exactly one JSON object, and valid JSON. Measured in
// Go: "[1,2]" and "\"x\"" and "5" are no params, "  {}" is (its Kind reads
// past the white space), and a repeated member name is not.
const GO_CASES = [
  ["good", {}, true],
  ["no params", { params: null }, true],
  ["empty params", { params: { text: "" } }, true],
  ["params {}", { params: { text: "{}" } }, true],
  ["params array", { params: { text: "[1,2]" } }, false],
  ["params string", { params: { text: '"x"' } }, false],
  ["params number", { params: { text: "5" } }, false],
  ["params leading space", { params: { text: "  {}" } }, true],
  ["params 8192", { params: { obj: 8192 } }, true],
  ["params 8193", { params: { obj: 8193 } }, false],
  ["params broken", { params: { text: '{"a":1' } }, false],
  ["params dup names", { params: { text: '{"a":1,"a":2}' } }, false],
  ["id 15", { id: { n: 15, fill: 97 } }, false],
  ["id 17", { id: { n: 17, fill: 97 } }, false],
  ["cert 31", { cert: { n: 31, fill: 7 } }, false],
  ["cert 33", { cert: { n: 33, fill: 7 } }, false],
  ["node empty", { node: "" }, true],
  ["node upper", { node: "Ctl" }, false],
  ["app bad", { app: "ngcstore" }, false],
  ["service bad", { service: "Controller" }, false],
  ["action bad", { action: "" }, false],
  ["inst bad", { inst: "nope" }, false],
  ["iat 0", { iat: 0 }, false],
  ["iat -1", { iat: -1, exp: 10 }, false],
  ["exp == iat", { exp: 1790000000 }, false],
  ["lifetime 300", { exp: 1790000300 }, true],
  ["lifetime 301", { exp: 1790000301 }, false],
];

// What Go is never handed, because its fields are int64 and []byte and a
// decoder that fills them refuses everything else. JavaScript hands a
// contract whatever a caller has, and a string, a boolean and a fraction all
// compare against a number as if they were one: "1790000000" > 0 is true,
// 2 - true is 1, and 1790000060.5 - 1790000000 is under 300. So each of these
// would have walked through a check written with > and - alone.
//
// The last case is the one place in these two tables where Go can be handed
// the value at all and answers differently: measured, Go's Validate takes
// 2^53 for an Issued. The contract's own decoder refuses every number above
// 2^53-1 (enc.safeNumber, json.js: safeNumber), so a task carrying one
// arrives over the wire in neither language. The limit is the API's, not the
// engine's.
const JS_CASES = [
  ["iat as text", { iat: "1790000000" }, false],
  ["exp as text", { exp: "1790000060" }, false],
  ["iat true", { iat: true, exp: 2 }, false],
  ["exp fraction", { exp: 1790000060.5 }, false],
  ["id as text", { id: "aaaaaaaaaaaaaaaa" }, false],
  ["cert as text", { cert: "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" }, false],
  ["params as text", { params: '{"a":1}' }, false],
  ["params as array", { params: [123, 125] }, false],
  ["iat beyond 2^53-1", { iat: 9007199254740992, exp: 9007199254741000 }, false],
];

test("validateTask decides every field as Go's task.Task.Validate does", async ({ shell }) => {
  test.setTimeout(120_000);
  const out = await shell.evaluate(async (cases) => {
    const task = await import("/assets/js/contracts/task.js");
    const { utf8 } = await import("/assets/js/contracts/b64.js");
    // A value the case table describes: bytes by length and fill, params by
    // their text, an object of exactly n bytes for the two params bounds.
    // Everything else - a plain string, a number, an array - is handed on as
    // it stands, which is what the cases that give a field the wrong type
    // need.
    const value = (x) => {
      if (x === null || typeof x !== "object") return x;
      if (Array.isArray(x)) return x;
      if (typeof x.n === "number") return new Uint8Array(x.n).fill(x.fill);
      if (typeof x.text === "string") return utf8(x.text);
      return utf8('{"a":"' + "x".repeat(x.obj - 8) + '"}');
    };
    const out = {};
    for (const [name, over] of cases) {
      const t = {
        id: new Uint8Array(16).fill(97), inst: "EREREREREREREREREREREQ",
        cert: new Uint8Array(32).fill(7), node: "ctl-1", app: "ngc.store",
        service: "controller", action: "deactivate",
        params: utf8('{"app":"org.sorglos.terminal"}'), iat: 1790000000, exp: 1790000060,
      };
      for (const [k, x] of Object.entries(over)) t[k] = value(x);
      out[name] = task.validateTask(t);
    }
    return out;
  }, [...GO_CASES, ...JS_CASES].map(([name, over]) => [name, over]));
  const want = {};
  for (const [name, , ok] of [...GO_CASES, ...JS_CASES]) want[name] = ok;
  expect(out).toEqual(want);
  // The two params bounds above already pin where the limit stands; this line
  // pins that the module carries it under the name Go gives it, so a reader
  // of task.js finds the number and not only this test.
  expect(await shell.evaluate(async () => (await import("/assets/js/contracts/task.js")).MAX_PARAMS))
    .toBe(8 << 10);
});

test("a field that changes between the check and the writing never reaches the bytes", async ({ shell }) => {
  test.setTimeout(120_000);
  const out = await shell.evaluate(async () => {
    const task = await import("/assets/js/contracts/task.js");
    const { utf8, fromUtf8 } = await import("/assets/js/contracts/b64.js");
    const base = () => ({
      id: new Uint8Array(16).fill(97), inst: "EREREREREREREREREREREQ",
      cert: new Uint8Array(32).fill(7), node: "ctl-1", app: "ngc.store",
      service: "controller", action: "deactivate",
      params: utf8('{"a":1}'), iat: 1790000000, exp: 1790000060,
    });

    // How often does each function read each field of the task it is given?
    // The rule is one read per field per function: a field read twice may
    // answer the check with one value and the writer with another, and then
    // what gets signed is not what was checked.
    const counted = (t) => {
      const n = {};
      const out = {};
      for (const k of Object.keys(t)) {
        const value = t[k];
        n[k] = 0;
        Object.defineProperty(out, k, { enumerable: true, get() { n[k]++; return value; } });
      }
      return { task: out, n };
    };
    const a = counted(base());
    task.validateTask(a.task);
    const b = counted(base());
    task.encodeTask(b.task);
    const most = (n) => Math.max(...Object.values(n));

    // A getter that tells the truth twice and lies from the third read on.
    // The brief's validateTask read exp twice on its own - once against iat,
    // once for the lifetime - and its encodeTask read it once more, so the
    // third read was the one that landed in the signed bytes, with no check
    // ever having seen it. Its node went the same way: twice in the check,
    // once in the writing.
    const drifting = (field, good, evil) => {
      const t = base();
      let reads = 0;
      const value = t[field];
      delete t[field];
      Object.defineProperty(t, field, {
        enumerable: true,
        get() { return ++reads <= 2 ? (good === undefined ? value : good) : evil; },
      });
      const ok = task.validateTask(t);
      let written;
      try {
        written = JSON.parse(fromUtf8(task.encodeTask(t)))[field];
      } catch {
        written = "refused";
      }
      return { ok, written, reads };
    };

    // A Uint8Array may carry an own length property. It answers every read
    // with whatever it likes and stays instanceof Uint8Array, so neither the
    // type check nor the length check alone says anything about the bytes
    // that are really there. Measured here rather than assumed.
    //
    // Every read gets the lie, not only the first: a check and a writer that
    // both went by length would agree with each other on the wrong answer,
    // and only looking at what was written catches that as well.
    const lying = (real, says) => {
      const u = utf8(real);
      Object.defineProperty(u, "length", { get() { return says; } });
      return u;
    };
    const liar = lying('{"a":1}', 0);
    const stillBytes = liar instanceof Uint8Array;
    const t = base();
    t.params = liar;
    const okLiar = task.validateTask(t);
    let writtenLiar;
    try {
      writtenLiar = fromUtf8(task.encodeTask(t));
    } catch (e) {
      writtenLiar = "refused: " + e.message;
    }

    return {
      // At most one read of any field, in either function.
      validateReads: most(a.n), encodeReads: most(b.n),
      exp: drifting("exp", undefined, 1890000060),
      node: drifting("node", undefined, "other"),
      stillBytes, okLiar, writtenLiar,
    };
  });
  expect(out.validateReads).toBe(1);
  expect(out.encodeReads).toBe(1);
  expect(out.stillBytes).toBe(true);
  // Neither the check nor the writer asks the params for a length: both go by
  // the text the bytes really hold, so a length that always answers 0 changes
  // nothing - the params are valid and they stand in the bytes.
  expect(out.okLiar).toBe(true);
  expect(out.writtenLiar).toContain('"params":{"a":1}');
  // Two truthful reads, then a different value. Whatever comes out, the exp
  // and the node in the bytes are ones a check has seen.
  expect(out.exp.written).not.toBe(1890000060);
  expect(out.node.written).not.toBe("other");
});

test("verifyApproval ties the task to the certificate, its installation and one reading of the payload",
  async ({ shell }) => {
    test.setTimeout(120_000);
    test.skip(!(await hasEd25519(shell, v.trust)),
      "no Ed25519 in this engine's Web Crypto: no signature of this vector can be checked");
    const out = await shell.evaluate(async (d) => {
      const task = await import("/assets/js/contracts/task.js");
      const session = await import("/assets/js/contracts/session.js");
      const trust = await import("/assets/js/contracts/trust.js");
      const { b64decode, utf8, fromUtf8, equal } = await import("/assets/js/contracts/b64.js");

      const list = await trust.parseList(b64decode(d.trust.payload));
      const client = b64decode(d.client);
      const payload = b64decode(d.task.payload);
      const env = () => ({ payload, kid: d.task.kid, sig: b64decode(d.task.sig) });
      // The certificate the way Go's task.VerifyApproval says it gets it:
      // what session.verifyApproval returned for these very bytes.
      const cert = await session.verifyApproval(client, {
        kind: d.clientProof.kind, kid: d.clientProof.kid, sig: b64decode(d.clientProof.sig),
      }, list);
      const run = async (e, c, bytes) => {
        try {
          return await task.verifyApproval(e, c, bytes);
        } catch (err) {
          return err.message;
        }
      };

      const good = await run(env(), cert, client);
      // The same certificate with one digit of its iat changed: certHash then
      // gives another hash, and the task names the hash of the bytes it
      // belongs to, so these are not them. verifyApproval never reads these
      // bytes as a certificate - it only hashes them - which is why any
      // change at all is enough here.
      const otherClient = utf8(fromUtf8(client).replace('"iat":1790000000', '"iat":1790000001'));
      // Go compares the task's inst against the certificate's, not against
      // the list's, so a certificate of another installation is refused here
      // even though its signature is the one the envelope carries.
      const otherInst = { ...cert, inst: "ISEhISEhISEhISEhISEhIQ" };

      // An envelope whose payload answers the second read with a different,
      // perfectly well-formed task: same length, node ctl-2 instead of ctl-1.
      // A verifier that checks the signature over the first read and decodes
      // the second hands back a task nobody signed.
      const drifting = { kid: d.task.kid, sig: b64decode(d.task.sig) };
      let reads = 0;
      const evil = utf8(fromUtf8(payload).replace('"node":"ctl-1"', '"node":"ctl-2"'));
      Object.defineProperty(drifting, "payload", { get() { return ++reads === 1 ? payload : evil; } });
      const drifted = await run(drifting, cert, client);

      return {
        node: typeof good === "string" ? good : good.node,
        bound: typeof good === "string" ? false : equal(good.cert, await session.certHash(client)),
        otherClient: await run(env(), cert, otherClient),
        otherInst: await run(env(), otherInst, client),
        tooLarge: await run({ ...env(), payload: new Uint8Array(task.MAX_TASK + 1) }, cert, client),
        notAnEnvelope: await run(null, cert, client),
        driftedNode: typeof drifted === "string" ? drifted : drifted.node,
      };
    }, v);
    expect(out.node).toBe("ctl-1");
    expect(out.bound).toBe(true);
    expect(out.otherClient).toBe("task: malformed task");
    expect(out.otherInst).toBe("task: malformed task");
    expect(out.tooLarge).toBe("task: malformed task");
    expect(out.notAnEnvelope).toBe("task: malformed task");
    // Either the first reading all the way through, or a refusal - never the
    // task the second reading brought.
    expect(out.driftedNode).not.toBe("ctl-2");
  });

test("verifyConfirm answers false where it cannot check, and reads the proof once", async ({ shell }) => {
  test.setTimeout(120_000);
  test.skip(!(await hasEd25519(shell, v.trust)),
    "no Ed25519 in this engine's Web Crypto: no signature of this vector can be checked");
  const out = await shell.evaluate(async (d) => {
    const task = await import("/assets/js/contracts/task.js");
    const trust = await import("/assets/js/contracts/trust.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");

    const list = await trust.parseList(b64decode(d.trust.payload));
    const env = { payload: b64decode(d.task.payload), kid: d.task.kid, sig: b64decode(d.task.sig) };
    const cert = JSON.parse(new TextDecoder().decode(b64decode(d.client)));
    const proof = () => ({ kind: d.confirm.kind, kid: d.confirm.kid, sig: b64decode(d.confirm.sig) });
    // The other user of this list: the key exists, the owner is someone else.
    const otherUser = JSON.parse(new TextDecoder().decode(b64decode(d.trust.payload)))
      .users.find((u) => u.id !== cert.user).id;

    const n = {};
    const watched = {};
    const p = proof();
    for (const k of Object.keys(p)) {
      const value = p[k];
      n[k] = 0;
      Object.defineProperty(watched, k, { enumerable: true, get() { n[k]++; return value; } });
    }
    const watchedEnv = {};
    n.payload = 0;
    Object.defineProperty(watchedEnv, "payload", {
      enumerable: true, get() { n.payload++; return env.payload; },
    });
    const counted = await task.verifyConfirm(watched, watchedEnv, list, cert.user);

    const junk = async (...args) => {
      try {
        return await task.verifyConfirm(...args);
      } catch (e) {
        return "threw: " + e.message;
      }
    };
    return {
      counted,
      reads: Math.max(...Object.values(n)),
      wrongUser: await task.verifyConfirm(proof(), env, list, otherUser),
      noSuchKey: await task.verifyConfirm({ ...proof(), kid: "AAAAAAAAAAAAAAAAAAAAAA" }, env, list, cert.user),
      // None of these can be checked, so each of them answers false: the
      // -> boolean family says no by answering, never by throwing, the way
      // sign.verify and proof.verify do.
      nulls: await junk(null, null, null, null),
      numbers: await junk(7, 7, 7, 7),
      noArgs: await junk(),
      throwingList: await junk(proof(), env, { get users() { throw new Error("no"); } }, cert.user),
      throwingProof: await junk({ get kid() { throw new Error("no"); } }, env, list, cert.user),
    };
  }, v);
  expect(out.counted).toBe(true);
  expect(out.reads).toBe(1);
  expect(out.wrongUser).toBe(false);
  expect(out.noSuchKey).toBe(false);
  expect(out.nulls).toBe(false);
  expect(out.numbers).toBe(false);
  expect(out.noArgs).toBe(false);
  expect(out.throwingList).toBe(false);
  expect(out.throwingProof).toBe(false);
});

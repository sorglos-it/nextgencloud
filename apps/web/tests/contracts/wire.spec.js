import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

const v = vectors("wire.json");

// Nothing in this file needs the Web Crypto API: wire.js decides frames,
// payloads and rules, and the one signature an OPEN carries is only measured,
// never verified here (that is sign.verify, on the controller's side). So no
// test below skips on WebKit, whose Web Crypto has no Ed25519.

test("frames, opens, closes, grants, end-to-end messages and control rules match Go", async ({ shell }) => {
  const got = await shell.evaluate(async (d) => {
    const w = await import("/assets/js/contracts/wire.js");
    const { b64decode, b64encode, equal } = await import("/assets/js/contracts/b64.js");
    const decides = (list, fn) => list.map((c) => { try { fn(c); return true; } catch { return false; } });
    return {
      frames: d.frames.map((c) => {
        const f = { type: c.type, channel: c.channel, payload: b64decode(c.payload) };
        return b64encode(w.encodeFrame(f)) === c.encoded
          && equal(w.decodeFrame(b64decode(c.encoded)).payload, f.payload);
      }),
      badFrames: decides(d.badFrames, (c) => w.decodeFrame(b64decode(c.encoded))),
      grants: decides(d.grants, (c) => w.parseGrant(b64decode(c.payload))),
      opens: decides(d.opens, (c) => w.parseOpen(b64decode(c.payload))),
      closes: decides(d.closes, (c) => w.parseClose(b64decode(c.payload))),
      e2e: decides(d.e2e, (c) => w.parseE2E(b64decode(c.plain), c.fromController)),
      controls: d.controls.map((c) => w.controlAllowed(c.link, c.fromApi, c.t)),
    };
  }, v);

  expect(got.frames.every(Boolean)).toBe(true);
  expect(got.badFrames.every((ok) => !ok)).toBe(true);
  for (const key of ["grants", "opens", "closes", "e2e"]) {
    const wrong = v[key].filter((c, i) => c.valid !== got[key][i]).map((c) => `${key}: ${c.name}`);
    expect(wrong).toEqual([]);
  }
  const wrongControls = v.controls.filter((c, i) => c.allowed !== got.controls[i])
    .map((c) => `${c.link}/${c.fromApi}/${c.t}`);
  expect(wrongControls).toEqual([]);
});

test("every error code carries its German message", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const w = await import("/assets/js/contracts/wire.js");
    return Object.fromEntries(Object.values(w.CODES).map((c) => [c, w.message(c)]));
  });
  expect(got).toEqual({
    E_FORBIDDEN: "Keine Berechtigung.",
    E_UNREACHABLE: "Dienst nicht erreichbar.",
    E_EXIT: "Dienst mit Fehler beendet.",
    E_TIMEOUT: "Zeitlimit überschritten.",
    E_LIMIT: "Zu viele offene Verbindungen.",
    E_PROTOCOL: "Ungültige Nachricht.",
    E_CONFIRM: "Bitte erneut bestätigen.",
    E_LICENSE: "Keine gültige Lizenz.",
    E_NOTFOUND: "Nicht gefunden.",
    E_VERIFY: "Unterschrift ungültig.",
    E_INTERNAL: "Interner Fehler.",
    E_TOOLARGE: "Zu viele Daten.",
    E_LOGIN: "Anmeldung fehlgeschlagen.",
    E_LOCKED: "Zu viele Fehlversuche. Bitte später erneut versuchen.",
  });
});

// The 14 codes are the whole list, and a table of them is an object whoever
// runs on the page can also reach through Object.prototype. Measured against
// shared/wire: Go's map answers "" for toString, constructor, hasOwnProperty,
// __proto__ and valueOf, and ParseClose refuses a CLOSE that names one of
// them. A table built as a plain object would answer Message("constructor")
// with a function and let `"toString" in messages` take that CLOSE.
test("no error code falls out of Object.prototype", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const w = await import("/assets/js/contracts/wire.js");
    const { utf8 } = await import("/assets/js/contracts/b64.js");
    const inherited = ["toString", "constructor", "hasOwnProperty", "__proto__", "valueOf", "isPrototypeOf"];
    return {
      messages: inherited.map((c) => w.message(c)),
      closes: inherited.map((c) => {
        try {
          w.parseClose(utf8(JSON.stringify({ status: "error", code: c })));
          return true;
        } catch {
          return false;
        }
      }),
      unknown: w.message("E_WHAT"),
      missing: w.message(undefined),
    };
  });
  expect(got.messages).toEqual(["", "", "", "", "", ""]);
  expect(got.closes).toEqual([false, false, false, false, false, false]);
  expect(got.unknown).toBe("");
  expect(got.missing).toBe("");
});

// WHICH CODES LEAVE THE PERSON WITHOUT ANYTHING TO DO, and that set decides
// where a sentence of the server may be shown beside the shell's own
// (net/channel.ChannelError.serverText). It is written out here code by code
// rather than derived, because a set that grew by itself would let a server
// put its words in place of the shell's own somewhere nobody decided.
test("only a code that says nothing to do takes a sentence of the server", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const w = await import("/assets/js/contracts/wire.js");
    const all = Object.values(w.CODES);
    return {
      general: all.filter((c) => w.isGeneral(c)).sort(),
      internal: w.isGeneral("E_INTERNAL"),
      unknown: w.isGeneral("E_WHAT"),
      missing: w.isGeneral(undefined),
      inherited: w.isGeneral("toString"),
      // Every code of the contract has a text, and only the one above says
      // nothing about what to do.
      texts: all.every((c) => w.message(c) !== ""),
    };
  });
  // E_INTERNAL alone. "Interner Fehler." is true and useless, and it is the
  // code a server has to use when what went wrong is its own machine.
  expect(got.general).toEqual(["E_INTERNAL"]);
  expect(got.internal).toBe(true);
  expect(got.texts).toBe(true);
  // And nothing that is no code of the contract gets in.
  expect(got.unknown).toBe(false);
  expect(got.missing).toBe(false);
  expect(got.inherited).toBe(false);
});

// WHAT THE PERSON READS WHEN THE SERVER SENT A SENTENCE ALONG. The bytes go
// through parseClose and the contract's validText, and only then into the
// error the shell holds, so this measures the whole way from a CLOSE to what
// is shown -- everything except the socket itself.
//
// Until this was built nothing of this shell read close.message. A controller
// whose file system cannot encrypt a directory sends "Bitte ext4 oder f2fs
// mit aktivierter Verschlüsselung verwenden" (runtime.NoFscryptText), and the
// person read "Interner Fehler." -- the one sentence that said what to DO was
// dropped one line before the screen (measured 18.10.2026).
//
// The code stays the branch: serverText stands BESIDE message and never in
// place of it, so every caller that acts on a code goes on as before.
test("a CLOSE with a sentence becomes an error that carries it beside its own", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const w = await import("/assets/js/contracts/wire.js");
    const { ChannelError } = await import("/assets/js/net/channel.js");
    // The bytes as a server sends them, read back the way the shell reads
    // every CLOSE.
    const close = (code, message) =>
      w.parseClose(w.encodeClose({ status: "error", code, message, exitCode: null }));
    const err = (code, message) => {
      const c = close(code, message);
      const e = new ChannelError(c.code, c.message);
      return { code: e.code, said: e.message, server: e.serverText };
    };
    const disk = "Bitte ext4 oder f2fs mit aktivierter Verschlüsselung verwenden.";
    return {
      disk: err("E_INTERNAL", disk),
      // A code that has a sentence of its own keeps it, and the server's
      // words get no room: otherwise a server could put its wording in place
      // of this shell's anywhere.
      forbidden: err("E_FORBIDDEN", "Nimm doch einfach einen anderen Benutzer."),
      // Nothing is said twice, and a CLOSE without a sentence adds nothing.
      twice: err("E_INTERNAL", "Interner Fehler."),
      none: err("E_INTERNAL", ""),
    };
  });
  expect(got.disk).toEqual({
    code: "E_INTERNAL", said: "Interner Fehler.",
    server: "Bitte ext4 oder f2fs mit aktivierter Verschlüsselung verwenden.",
  });
  expect(got.forbidden).toEqual({
    code: "E_FORBIDDEN", said: "Keine Berechtigung.", server: "",
  });
  expect(got.twice.server).toBe("");
  expect(got.none.server).toBe("");
});

// The numbers are the contract. Every one of them is written out here, not
// derived a second time, so a shift of one in wire.js cannot hide behind the
// same arithmetic in the test.
test("the limits are the numbers Go holds", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const w = await import("/assets/js/contracts/wire.js");
    const names = [
      "TYPE_OPEN", "TYPE_DATA", "TYPE_EOF", "TYPE_CLOSE", "TYPE_GRANT", "HEADER_SIZE", "MAX_FRAME",
      "MAX_PAYLOAD", "CONTROL_CHANNEL", "DEFAULT_WINDOW", "MIN_WINDOW", "MAX_WINDOW",
      "MAX_SESSION_CHANNELS", "MAX_OPEN_PAYLOAD", "MAX_BROWSER_OPEN", "MAX_CLOSE_PAYLOAD", "MAX_PARAMS",
      "MAX_MESSAGE_TEXT", "HANDSHAKE_SIZE", "MAX_E2E_PLAINTEXT", "MAX_CALL_RESULT", "E2E_FIRST_TIMEOUT",
      "E2E_DATA", "E2E_END",
    ];
    return Object.fromEntries(names.map((n) => [n, w[n]]));
  });
  expect(got).toEqual({
    TYPE_OPEN: 1, TYPE_DATA: 2, TYPE_EOF: 3, TYPE_CLOSE: 4, TYPE_GRANT: 5,
    HEADER_SIZE: 5, MAX_FRAME: 65519, MAX_PAYLOAD: 65514, CONTROL_CHANNEL: 0,
    DEFAULT_WINDOW: 262144, MIN_WINDOW: 4096, MAX_WINDOW: 1048576,
    MAX_SESSION_CHANNELS: 64, MAX_OPEN_PAYLOAD: 40960, MAX_BROWSER_OPEN: 16384,
    MAX_CLOSE_PAYLOAD: 4096, MAX_PARAMS: 8192, MAX_MESSAGE_TEXT: 256, HANDSHAKE_SIZE: 48,
    MAX_E2E_PLAINTEXT: 65498, MAX_CALL_RESULT: 8388608, E2E_FIRST_TIMEOUT: 30,
    E2E_DATA: 1, E2E_END: 2,
  });
});

// The sizes at the edge, all of them measured against shared/wire first:
// Decode takes 5 and 65519 bytes and refuses 4 and 65520; Encode takes an
// OPEN payload of 40960 and refuses 40961; a DATA payload of MaxPayload is
// taken and one byte more refused, on channel 0 as well.
test("encodeFrame and decodeFrame keep Go's edges", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const w = await import("/assets/js/contracts/wire.js");
    const tries = (fn) => { try { fn(); return true; } catch { return false; } };
    const enc = (type, channel, n) => tries(() => w.encodeFrame({ type, channel, payload: new Uint8Array(n) }));
    const dec = (n, type) => tries(() => {
      const b = new Uint8Array(n);
      if (n > 0) b[0] = type;
      return w.decodeFrame(b);
    });
    return {
      decodeSizes: [dec(4, w.TYPE_DATA), dec(5, w.TYPE_DATA), dec(65519, w.TYPE_DATA), dec(65520, w.TYPE_DATA)],
      openPayload: [enc(w.TYPE_OPEN, 1, 40960), enc(w.TYPE_OPEN, 1, 40961)],
      dataPayload: [enc(w.TYPE_DATA, 0, 65514), enc(w.TYPE_DATA, 0, 65515)],
      rules: [
        enc(w.TYPE_OPEN, 0, 10), enc(w.TYPE_OPEN, 1, 0),
        enc(w.TYPE_EOF, 1, 0), enc(w.TYPE_EOF, 1, 1), enc(w.TYPE_EOF, 0, 0),
        enc(w.TYPE_CLOSE, 1, 1), enc(w.TYPE_CLOSE, 1, 0), enc(w.TYPE_CLOSE, 0, 1), enc(w.TYPE_CLOSE, 1, 4097),
        enc(w.TYPE_GRANT, 1, 4), enc(w.TYPE_GRANT, 1, 52), enc(w.TYPE_GRANT, 1, 51), enc(w.TYPE_GRANT, 0, 4),
        enc(0, 1, 1), enc(6, 1, 1),
      ],
      // Go is handed a Frame of three typed fields; here they come out of an
      // object the caller owns, so a type or a channel that is no number and
      // a payload that is no bytes have to be refused instead of coerced.
      wrongFields: [
        tries(() => w.encodeFrame({ type: w.TYPE_DATA, channel: -1, payload: new Uint8Array(1) })),
        tries(() => w.encodeFrame({ type: w.TYPE_DATA, channel: 4294967296, payload: new Uint8Array(1) })),
        tries(() => w.encodeFrame({ type: w.TYPE_DATA, channel: 1.5, payload: new Uint8Array(1) })),
        tries(() => w.encodeFrame({ type: w.TYPE_DATA, channel: "1", payload: new Uint8Array(1) })),
        tries(() => w.encodeFrame({ type: "2", channel: 1, payload: new Uint8Array(1) })),
        tries(() => w.encodeFrame({ type: w.TYPE_DATA, channel: 1, payload: [1, 2, 3] })),
        tries(() => w.encodeFrame({ type: w.TYPE_DATA, channel: 1, payload: "abc" })),
        tries(() => w.encodeFrame(null)),
        tries(() => w.decodeFrame([1, 2, 3, 4, 5, 6])),
        tries(() => w.encodeFrame({ type: w.TYPE_DATA, channel: 4294967295, payload: new Uint8Array(1) })),
      ],
      // The channel of the highest number still has to come back unchanged.
      topChannel: w.decodeFrame(w.encodeFrame({ type: w.TYPE_DATA, channel: 4294967295, payload: new Uint8Array(1) })).channel,
    };
  });
  expect(got.decodeSizes).toEqual([false, true, true, false]);
  expect(got.openPayload).toEqual([true, false]);
  expect(got.dataPayload).toEqual([true, false]);
  expect(got.rules).toEqual([
    false, false,
    true, false, false,
    true, false, false, false,
    true, true, false, false,
    false, false,
  ]);
  expect(got.wrongFields).toEqual([false, false, false, false, false, false, false, false, false, true]);
  expect(got.topChannel).toBe(4294967295);
});

// A payload is asked for its size once. A Uint8Array a caller subclasses can
// answer length with one number for the check and another for the copy, and
// then the check decides about a frame nobody sends: with two readings a
// payload that says 0 the first time and 65515 the second passes the check
// and goes out as a frame of 65520 bytes, over MaxFrame, which the other side
// drops and the link ends on.
//
// What this holds is a LENIENCY, not a bound: a length that disagrees with
// the object's own view is ignored here, and the real bytes decide. That is
// deliberate and it is the frozen Go contract's own behaviour - measured, a
// slice whose capacity is larger than its length is taken everywhere in
// shared/ and works on the reported number. Refusing instead was built and
// measured and turned down for that reason. The whole of it, with the
// measurement, stands in bytes.spec.js, "a length that disagrees with the
// view is ignored, not refused", which names this test. Do not flip this
// expectation without reading that one.
test("a payload whose length lies is refused, not half written", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const w = await import("/assets/js/contracts/wire.js");
    const tries = (fn) => { try { return { ok: true, v: fn() }; } catch { return { ok: false, v: null }; } };
    // A payload whose first reading of length is 0 and whose later readings
    // are the truth. The lie is armed only after the bytes are in, so nothing
    // the test itself does uses it up.
    const make = (n, head = []) => {
      let lie = false;
      class Shifty extends Uint8Array {
        get length() {
          if (!lie) return this.byteLength;
          lie = false;
          return 0;
        }
      }
      const a = new Shifty(n);
      for (let i = 0; i < head.length; i++) a[i] = head[i];
      lie = true;
      return a;
    };
    const over = tries(() => w.encodeFrame({ type: w.TYPE_DATA, channel: 1, payload: make(w.MAX_PAYLOAD + 1) }));
    const fits = tries(() => w.encodeFrame({ type: w.TYPE_DATA, channel: 1, payload: make(3, [1, 2, 3]) }));
    // Four bytes of credit 4096, then the 48-byte handshake.
    const grant = tries(() => w.parseGrant(make(4 + w.HANDSHAKE_SIZE, [0, 0, 16, 0])));
    const inner = tries(() => w.parseE2E(make(2, [w.E2E_DATA, 65]), false));
    return {
      overRefused: !over.ok,
      overLength: over.v === null ? 0 : over.v.length,
      fitsLength: fits.ok ? fits.v.length : -1,
      grantOK: grant.ok,
      grantHandshake: grant.ok ? grant.v.handshake.length : -1,
      innerOK: inner.ok,
    };
  });
  // Either refused or written whole - never a frame the check never saw.
  expect(got.overRefused).toBe(true);
  expect(got.overLength).toBe(0);
  // A payload that fits is written whole, with its real three bytes.
  expect(got.fitsLength).toBe(8);
  expect(got.grantOK).toBe(true);
  expect(got.grantHandshake).toBe(48);
  expect(got.innerOK).toBe(true);
});

// The same lie, one layer up: parseOpen, parseClose and parseControl handed
// their argument to decode, which keeps the object and reads length in eight
// more places after the budget has been measured against the first reading.
// So a subclass that answers 0 once walked a document of any size past the
// bound. Each document below is one whose members are all there and whose
// space to the closing brace is white space, which is JSON, so it stays one
// valid document at any size. All six were measured against shared/wire
// first: ParseOpen, ParseControl and ParseClose refuse 22063, 70027 and 5015
// bytes and take the very same document cut to 16384, 65514 and 4096. What
// refuses them is the bound, not the document.
//
// What this holds is a LENIENCY, not a bound: a length that disagrees with
// the object's own view is ignored here, and the real bytes decide. That is
// deliberate and it is the frozen Go contract's own behaviour - measured, a
// slice whose capacity is larger than its length is taken everywhere in
// shared/ and works on the reported number. Refusing instead was built and
// measured and turned down for that reason. The whole of it, with the
// measurement, stands in bytes.spec.js, "a length that disagrees with the
// view is ignored, not refused", which names this test. Do not flip this
// expectation without reading that one.
test("a document whose length lies is refused by every parser", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const w = await import("/assets/js/contracts/wire.js");
    const { utf8 } = await import("/assets/js/contracts/b64.js");
    // The lie is armed only after the bytes are in, so nothing the test
    // itself does uses it up.
    const lying = (b) => {
      let lie = false;
      class Shifty extends Uint8Array {
        get length() {
          if (!lie) return this.byteLength;
          lie = false;
          return 0;
        }
      }
      const a = new Shifty(b.length);
      a.set(b);
      lie = true;
      return a;
    };
    const padded = (n, head) => {
      const b = new Uint8Array(n);
      b.fill(0x20);
      b.set(utf8(head), 0);
      b[n - 1] = 0x7d;
      return b;
    };
    const takes = (fn) => { try { fn(); return true; } catch { return false; } };
    const rows = [
      [22063, w.MAX_BROWSER_OPEN,
        '{"app":"ngc.desktop","service":"api","action":"apps","params":{"a":1},"window":262144',
        (x) => w.parseOpen(x)],
      [70027, w.MAX_PAYLOAD, '{"t":"bye"', (x) => w.parseControl(x)],
      [5015, w.MAX_CLOSE_PAYLOAD, '{"status":"error","code":"E_EXIT"', (x) => w.parseClose(x)],
    ];
    const open = utf8('{"app":"ngc.desktop","service":"api","action":"apps","window":262144,"params":{"a":1}}');
    return {
      over: rows.map(([n, , head, fn]) => takes(() => fn(padded(n, head)))),
      lied: rows.map(([n, , head, fn]) => takes(() => fn(lying(padded(n, head))))),
      bound: rows.map(([, n, head, fn]) => takes(() => fn(padded(n, head)))),
      // The raw members are cut out of the bytes the parser holds, and slice
      // keeps the class it was called on. Without the single reading above,
      // parseOpen(p).params is the caller's subclass - and those are the
      // bytes the end-to-end prologue hashes.
      params: Object.getPrototypeOf(w.parseOpen(lying(open)).params) === Uint8Array.prototype,
      body: Object.getPrototypeOf(w.parseControl(lying(utf8('{"t":"bye","b":{"why":"x"}}'))).b)
        === Uint8Array.prototype,
    };
  });
  // Over the bound: refused whether length tells the truth once or twice.
  expect(got.over).toEqual([false, false, false]);
  expect(got.lied).toEqual([false, false, false]);
  // The same document at the bound is taken, so what refuses it is the size.
  expect(got.bound).toEqual([true, true, true]);
  expect(got.params).toBe(true);
  expect(got.body).toBe(true);
});

// Go's Grant makes a payload for any uint32, and ParseGrant is the one that
// refuses 0 and more than MaxWindow (measured: Grant(0) builds four bytes that
// ParseGrant then turns down). The two stay apart here as well.
test("grant and parseGrant split the same way as Go", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const w = await import("/assets/js/contracts/wire.js");
    const tries = (fn) => { try { return fn(); } catch { return null; } };
    const roundTrip = (credit, handshake) => {
      const p = tries(() => w.grant(credit, handshake));
      if (p === null) return { built: false };
      const g = tries(() => w.parseGrant(p));
      return { built: true, len: p.length, parsed: g !== null, credit: g === null ? -1 : g.credit,
        handshake: g === null || g.handshake === null ? -1 : g.handshake.length };
    };
    return {
      zero: roundTrip(0, null),
      one: roundTrip(1, null),
      maxWindow: roundTrip(w.MAX_WINDOW, null),
      overWindow: roundTrip(w.MAX_WINDOW + 1, null),
      withHandshake: roundTrip(4096, new Uint8Array(w.HANDSHAKE_SIZE)),
      noArgument: roundTrip(4096, undefined),
      badCredit: [
        tries(() => w.grant(-1, null)), tries(() => w.grant(4294967296, null)),
        tries(() => w.grant(1.5, null)), tries(() => w.grant("4096", null)),
        tries(() => w.grant(4096, "handshake")),
      ].map((r) => r !== null),
      topCredit: tries(() => w.grant(4294967295, null)) !== null,
    };
  });
  expect(got.zero).toEqual({ built: true, len: 4, parsed: false, credit: -1, handshake: -1 });
  expect(got.one).toEqual({ built: true, len: 4, parsed: true, credit: 1, handshake: -1 });
  expect(got.maxWindow).toEqual({ built: true, len: 4, parsed: true, credit: 1048576, handshake: -1 });
  expect(got.overWindow).toEqual({ built: true, len: 4, parsed: false, credit: -1, handshake: -1 });
  expect(got.withHandshake).toEqual({ built: true, len: 52, parsed: true, credit: 4096, handshake: 48 });
  expect(got.noArgument).toEqual({ built: true, len: 4, parsed: true, credit: 4096, handshake: -1 });
  expect(got.badCredit).toEqual([false, false, false, false, false]);
  expect(got.topCredit).toBe(true);
});

// The edges of ParseOpen that no vector holds, every one of them measured
// against shared/wire first.
test("parseOpen decides the edges the way Go decides them", async ({ shell }) => {
  const cases = [
    // [name, payload, valid]
    ["empty params object", '{"app":"ngc.desktop","service":"api","action":"apps","params":{},"window":262144}', true],
    ["params with white space", '{"app":"ngc.desktop","service":"api","action":"apps","params":{ },"window":262144}', true],
    ["params as a string", '{"app":"ngc.desktop","service":"api","action":"apps","params":"{}","window":262144}', false],
    ["core app with an empty e2e", '{"app":"ngc.desktop","service":"api","action":"apps","window":262144,"e2e":""}', true],
    ["no window", '{"app":"ngc.desktop","service":"api","action":"apps"}', false],
    ["nested params", '{"app":"ngc.desktop","service":"api","action":"apps","params":{"a":{"b":{"c":1}}},"window":262144}', true],
  ];
  const got = await shell.evaluate(async (list) => {
    const w = await import("/assets/js/contracts/wire.js");
    const { utf8, fromUtf8 } = await import("/assets/js/contracts/b64.js");
    return {
      decides: list.map(([, payload]) => {
        try { w.parseOpen(utf8(payload)); return true; } catch { return false; }
      }),
      // The params come back as the bytes that stood there, without the white
      // space around them: noisep.E2EPrologue hashes exactly these.
      params: fromUtf8(w.parseOpen(utf8(list[1][1])).params),
      noParams: w.parseOpen(utf8(list[3][1])).params,
    };
  }, cases);
  const wrong = cases.filter(([, , valid], i) => valid !== got.decides[i]).map(([name]) => name);
  expect(wrong).toEqual([]);
  expect(got.params).toBe("{ }");
  expect(got.noParams).toBe(null);
});

// validateOpen holds Go's checks on the task member and no more: Go's
// Open.validate asks for a payload of 1 to MaxTask bytes and a 64-byte
// signature and never what the key id looks like. Measured against
// shared/wire: ParseOpen takes a task whose kid is "", "short" or 22
// characters outside the base64url alphabet, and the id is checked where the
// key is looked up instead. sign.validateEnvelope would refuse those here,
// and a shell that refuses its own OPEN cannot send it at all.
test("validateOpen asks of a task what Go asks", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const w = await import("/assets/js/contracts/wire.js");
    const { utf8 } = await import("/assets/js/contracts/b64.js");
    const sig = "fyjm3xqkKKVuMKY61OmcHKNoxt7D_wPCvi1eBwBF49ihsrA9Vn_QB7owHE4zJrQ4i-TMDf9XUqDWln0N44qDAw";
    const open = (kid) => '{"app":"org.sorglos.terminal","service":"terminal","action":"shell","window":262144,'
      + '"e2e":"BwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcH",'
      + '"task":{"payload":"eyJ2IjoxfQ","kid":"' + kid + '","sig":"' + sig + '"}}';
    const takes = (kid) => { try { w.parseOpen(utf8(open(kid))); return true; } catch { return false; } };
    // A throwing getter: validateOpen answers false instead of carrying the
    // exception through a caller who wrote `if (validateOpen(...))`.
    const throwing = { get app() { throw new Error("field"); }, service: "api", action: "apps",
      window: 262144, e2e: new Uint8Array(0) };
    return {
      kids: ["MJfi3uLLSjS1OEDNtwWu1w", "", "short", "MJfi3uLLSjS1OEDNtwWu1wAAAA",
        "!!!!!!!!!!!!!!!!!!!!!!"].map(takes),
      throwing: w.validateOpen(throwing),
      // The object form of the same check, for a caller that builds an OPEN.
      objects: [
        w.validateOpen({ app: "ngc.desktop", service: "api", action: "apps", params: null,
          window: 262144, e2e: new Uint8Array(0), task: null, confirm: null }),
        w.validateOpen({ app: "ngc.desktop", service: "api", action: "apps", params: null,
          window: 262144, e2e: new Uint8Array(0), task: null,
          confirm: { kind: "password", kid: "Q0NDQ0NDQ0NDQ0NDQ0NDQw", sig: new Uint8Array(64),
            authData: new Uint8Array(0), clientData: new Uint8Array(0) } }),
        w.validateOpen({ app: "ngc.desktop", service: "api", action: "apps", window: 262144,
          e2e: new Uint8Array(0), task: { payload: new Uint8Array(1), kid: "x", sig: new Uint8Array(64) } }),
        w.validateOpen({ app: "ngc.desktop", service: "api", action: "apps", window: 262144,
          e2e: new Uint8Array(0), task: { payload: new Uint8Array(0), kid: "x", sig: new Uint8Array(64) } }),
        w.validateOpen({ app: "ngc.desktop", service: "api", action: "apps", window: 262144,
          e2e: new Uint8Array(0), task: { payload: new Uint8Array(1), kid: "x", sig: new Uint8Array(63) } }),
        w.validateOpen({ app: "ngc.desktop", service: "api", action: "apps", window: 262144,
          e2e: new Uint8Array(0), task: { payload: new Uint8Array(16 * 1024 + 1), kid: "x", sig: new Uint8Array(64) } }),
        w.validateOpen(null),
        w.validateOpen({ app: "ngc.desktop", service: "api", action: "apps", window: 262144, e2e: "" }),
      ],
      // Nothing, no bytes, an object, an array, one byte over MaxParams, a
      // string that is no bytes, and a caller who handed over nothing at all.
      // Go's ValidParams(nil) is true, so "no params" is valid on both ends -
      // but params that are something other than bytes are no params.
      params: [w.validParams(null), w.validParams(new Uint8Array(0)), w.validParams(new TextEncoder().encode("{}")),
        w.validParams(new TextEncoder().encode("[1]")), w.validParams(new Uint8Array(8193).fill(0x7b)),
        w.validParams("{}"), w.validParams(undefined)],
    };
  });
  expect(got.kids).toEqual([true, true, true, true, true]);
  expect(got.throwing).toBe(false);
  // A task that is there, one that is not, a confirmation without a task, and
  // the three lengths Go asks about.
  expect(got.objects).toEqual([true, false, true, false, false, false, false, false]);
  expect(got.params).toEqual([true, true, true, false, false, false, true]);
});

// encodeOpen writes the bytes that travel to the controller unchanged, so
// what it hands back has to be an OPEN parseOpen takes - otherwise the shell
// sends what the other end drops.
test("encodeOpen writes an OPEN that parseOpen takes", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const w = await import("/assets/js/contracts/wire.js");
    const { fromUtf8, b64encode } = await import("/assets/js/contracts/b64.js");
    const e2e = new Uint8Array(w.HANDSHAKE_SIZE).fill(7);
    const params = new TextEncoder().encode('{"cols":80,"rows":24}');
    const full = w.encodeOpen({ app: "org.sorglos.terminal", service: "terminal", action: "shell",
      params, window: w.DEFAULT_WINDOW, e2e });
    const core = w.encodeOpen({ app: "ngc.desktop", service: "api", action: "apps", window: w.DEFAULT_WINDOW });
    const back = w.parseOpen(full);
    const tries = (o) => { try { w.encodeOpen(o); return true; } catch { return false; } };
    return {
      full: fromUtf8(full),
      core: fromUtf8(core),
      e2eBack: b64encode(back.e2e),
      paramsBack: fromUtf8(back.params),
      windowBack: back.window,
      refuses: [
        tries({ app: "Terminal", service: "terminal", action: "shell", window: w.DEFAULT_WINDOW, e2e }),
        tries({ app: "org.sorglos.terminal", service: "terminal", action: "shell", window: 4095, e2e }),
        tries({ app: "org.sorglos.terminal", service: "terminal", action: "shell", window: w.DEFAULT_WINDOW }),
        tries({ app: "ngc.desktop", service: "api", action: "apps", window: w.DEFAULT_WINDOW, e2e }),
        tries({ app: "ngc.desktop", service: "api", action: "apps", window: w.DEFAULT_WINDOW,
          params: new TextEncoder().encode("[1]") }),
        tries({ app: "ngc.desktop", service: "api", action: "apps", window: w.DEFAULT_WINDOW,
          params: new TextEncoder().encode('{"a":1,"a":2}') }),
        tries({ app: "ngc.desktop", service: "api", action: "apps", window: 1.5 }),
        tries({ app: undefined, service: "api", action: "apps", window: w.DEFAULT_WINDOW }),
        tries(null),
      ],
    };
  });
  expect(got.full).toBe('{"app":"org.sorglos.terminal","service":"terminal","action":"shell"'
    + ',"params":{"cols":80,"rows":24},"window":262144'
    + ',"e2e":"BwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcH"}');
  expect(got.core).toBe('{"app":"ngc.desktop","service":"api","action":"apps","window":262144}');
  expect(got.e2eBack).toBe("BwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcH");
  expect(got.paramsBack).toBe('{"cols":80,"rows":24}');
  expect(got.windowBack).toBe(262144);
  expect(got.refuses).toEqual([false, false, false, false, false, false, false, false, false]);
});

// errorClose and encodeClose against the bytes Go marshals: measured,
// wire.Error(CodeForbidden) becomes
// {"status":"error","code":"E_FORBIDDEN","message":"Keine Berechtigung."} -
// no exitCode member, because a nil pointer with omitempty writes none.
test("encodeClose writes what Go marshals, and keeps the exit code 0", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const w = await import("/assets/js/contracts/wire.js");
    const { fromUtf8 } = await import("/assets/js/contracts/b64.js");
    const tries = (c) => { try { w.encodeClose(c); return true; } catch { return false; } };
    const zero = w.parseClose(w.encodeClose({ status: "error", code: "E_EXIT", message: "", exitCode: 0 }));
    const none = w.parseClose(w.encodeClose(w.errorClose(w.CODES.EXIT)));
    return {
      forbidden: fromUtf8(w.encodeClose(w.errorClose(w.CODES.FORBIDDEN))),
      ok: fromUtf8(w.encodeClose({ status: "ok", code: "", message: "", exitCode: null })),
      exitZero: fromUtf8(w.encodeClose({ status: "error", code: "E_EXIT", message: "", exitCode: 0 })),
      // 0 and "no exit code at all" are two answers, not one.
      zeroBack: zero.exitCode,
      noneBack: none.exitCode,
      errorClose: w.errorClose(w.CODES.LOCKED),
      refuses: [
        tries({ status: "maybe" }), tries({ status: "error", code: "E_WHAT" }),
        tries({ status: "error", code: "E_EXIT", exitCode: 256 }),
        tries({ status: "error", code: "E_EXIT", exitCode: -1 }),
        tries({ status: "error", code: "E_EXIT", exitCode: 1.5 }),
        tries({ status: "error", code: "E_EXIT", message: "Zeile\u0007" }),
        tries({ status: 5 }), tries(null), tries({}),
        tries({ status: "ok", exitCode: 5 }),
      ],
    };
  });
  expect(got.forbidden).toBe('{"status":"error","code":"E_FORBIDDEN","message":"Keine Berechtigung."}');
  expect(got.ok).toBe('{"status":"ok"}');
  expect(got.exitZero).toBe('{"status":"error","code":"E_EXIT","exitCode":0}');
  expect(got.zeroBack).toBe(0);
  expect(got.noneBack).toBe(null);
  expect(got.errorClose).toEqual({ status: "error", code: "E_LOCKED",
    message: "Zu viele Fehlversuche. Bitte später erneut versuchen.", exitCode: null });
  expect(got.refuses).toEqual([false, false, false, false, false, false, false, false, false, true]);
});

// The inner end-to-end messages: e2e builds what parseE2E reads back, at the
// edges Go draws them (measured: E2E(data, 65497) is taken, 65498 refused).
test("e2e builds what parseE2E reads", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const w = await import("/assets/js/contracts/wire.js");
    const { fromUtf8, utf8 } = await import("/assets/js/contracts/b64.js");
    const tries = (fn) => { try { return fn(); } catch { return null; } };
    const data = utf8("ls -la\n");
    const built = w.e2e(w.E2E_DATA, data);
    const end = w.e2e(w.E2E_END, new Uint8Array(0));
    const status = w.e2e(w.E2E_END, w.encodeClose(w.errorClose(w.CODES.TIMEOUT)));
    return {
      dataKind: built[0], dataBack: fromUtf8(w.parseE2E(built, true).data),
      endKind: end[0], endLen: end.length,
      endFromShell: w.parseE2E(end, false),
      statusBack: w.parseE2E(status, true).status,
      sizes: [
        tries(() => w.e2e(w.E2E_DATA, new Uint8Array(w.MAX_E2E_PLAINTEXT - 1))) !== null,
        tries(() => w.e2e(w.E2E_DATA, new Uint8Array(w.MAX_E2E_PLAINTEXT))) !== null,
      ],
      refuses: [
        tries(() => w.e2e(0, new Uint8Array(1))) !== null,
        tries(() => w.e2e(3, new Uint8Array(1))) !== null,
        tries(() => w.e2e(w.E2E_END, utf8('{"status":"maybe"}'))) !== null,
        tries(() => w.e2e(w.E2E_DATA, "text")) !== null,
        tries(() => w.parseE2E(new Uint8Array(0), true)) !== null,
        tries(() => w.parseE2E(new Uint8Array(w.MAX_E2E_PLAINTEXT + 1), true)) !== null,
      ],
      // fromController decides whether a body is allowed at all, so it has to
      // be the bool Go is handed and nothing that only looks like one.
      side: [
        tries(() => w.parseE2E(status, "yes")) !== null,
        tries(() => w.parseE2E(status, 1)) !== null,
        tries(() => w.parseE2E(status, undefined)) !== null,
      ],
      // An end without a body, which Go's E2E(E2EEnd, nil) also builds.
      nilBody: tries(() => w.e2e(w.E2E_END, null)),
    };
  });
  expect(got.dataKind).toBe(1);
  expect(got.dataBack).toBe("ls -la\n");
  expect(got.endKind).toBe(2);
  expect(got.endLen).toBe(1);
  expect(got.endFromShell).toEqual({ kind: 2, data: null, status: null });
  expect(got.statusBack).toEqual({ status: "error", code: "E_TIMEOUT",
    message: "Zeitlimit überschritten.", exitCode: null });
  expect(got.sizes).toEqual([true, false]);
  expect(got.refuses).toEqual([false, false, false, false, false, false]);
  expect(got.side).toEqual([false, false, false]);
  expect(got.nilBody).toEqual(new Uint8Array([2]));
});

// The control channel, again against measured Go: a body may be any JSON
// value, null is refused everywhere, an unknown member and an unknown type
// are refused, and the member order in the document does not matter.
test("parseControl and encodeControl keep Go's rules", async ({ shell }) => {
  const cases = [
    ['{"t":"hello"}', true],
    ['{"t":"bye","b":{"why":"x"}}', true],
    ['{"t":"hello","b":[1,2,3]}', true],
    ['{"t":"hello","b":"text"}', true],
    ['{"b":{"why":"x"},"t":"hello"}', true],
    ['{"t":"shutdown"}', false],
    ['{"t":"hello","b":null}', false],
    ['{"t":"hello","extra":1}', false],
    ['{"t":""}', false],
    ["{}", false],
    ['{"t":"HELLO"}', false],
  ];
  const got = await shell.evaluate(async (list) => {
    const w = await import("/assets/js/contracts/wire.js");
    const { utf8, fromUtf8 } = await import("/assets/js/contracts/b64.js");
    const tries = (fn) => { try { return fn(); } catch { return null; } };
    return {
      decides: list.map(([p]) => tries(() => w.parseControl(utf8(p))) !== null),
      bye: fromUtf8(w.encodeControl({ t: w.CTL_BYE })),
      withBody: fromUtf8(w.encodeControl({ t: w.CTL_NOTIFY, b: utf8('{"why":"x"}') })),
      bodyBack: fromUtf8(w.parseControl(utf8('{"t":"bye","b":{"why":"x"}}')).b),
      noBody: w.parseControl(utf8('{"t":"bye"}')).b,
      refuses: [
        tries(() => w.encodeControl({ t: "shutdown" })) !== null,
        tries(() => w.encodeControl({ t: 5 })) !== null,
        tries(() => w.encodeControl(null)) !== null,
        tries(() => w.encodeControl({ t: w.CTL_BYE, b: "{}" })) !== null,
        tries(() => w.encodeControl({ t: w.CTL_BYE, b: utf8("{") })) !== null,
      ],
      // Go is handed a bool; a side that is not one is no side.
      allowed: [
        w.controlAllowed(w.LINK_SESSION, true, w.CTL_TRUST),
        w.controlAllowed(w.LINK_SESSION, "yes", w.CTL_TRUST),
        w.controlAllowed(w.LINK_SESSION, 1, w.CTL_TRUST),
        w.controlAllowed(w.LINK_SESSION, false, w.CTL_BYE),
        w.controlAllowed("other", true, w.CTL_BYE),
      ],
      coreApp: [w.CORE_APP("ngc.desktop"), w.CORE_APP("org.sorglos.terminal"), w.CORE_APP(undefined)],
    };
  }, cases);
  const wrong = cases.filter(([, valid], i) => valid !== got.decides[i]).map(([p]) => p);
  expect(wrong).toEqual([]);
  expect(got.bye).toBe('{"t":"bye"}');
  expect(got.withBody).toBe('{"t":"notify","b":{"why":"x"}}');
  expect(got.bodyBack).toBe('{"why":"x"}');
  expect(got.noBody).toBe(null);
  expect(got.refuses).toEqual([false, false, false, false, false]);
  expect(got.allowed).toEqual([true, false, false, true, false]);
  expect(got.coreApp).toEqual([true, false, false]);
});

// validParams is Go's exported ValidParams - the shape and the scan of the
// whole value together - and not its unexported paramsShape, which is what
// parseOpen alone still uses. Every answer in the fourth column was measured
// against shared/wire with wire.ValidParams over exactly these bytes.
//
// The four rows where the two differ are named in the table and counted
// below, so the price cannot quietly grow: this file answers no where Go
// answers yes, and Go's own ParseOpen refuses all four as well (measured), so
// no params an OPEN could have carried are turned down here. No row is the
// other way round.
//
// Both counts below read got.decides, so a change to wire.js moves them, and
// since this round they are split by direction instead of by "differs at all":
// the first names the rows where this file is stricter than Go, the second the
// rows where it is looser. Until this round the first named every row that
// differed at all, so the second was a subset of it and could never be the
// only one red - a row it caught was a row the first caught too. The two sets
// are disjoint now and their union is the old first one, so nothing is given
// up. Measured with validParams put back on the pure shape, each red on its
// own: the first names one row, "white space before", against the four it
// expects - the other four stop being stricter, because the shape takes them;
// and the second names twelve, where it expects none. One and twelve are the
// thirteen the old first assertion named.
test("validParams asks the whole question Go's ValidParams asks", async ({ shell }) => {
  const nest = (n) => '{"a":'.repeat(n) + "1" + "}".repeat(n);
  const cases = [
    // [name, params, here, Go's ValidParams]
    ["nothing", "", true, true],
    ["an object", "{}", true, true],
    ["a member", '{"a":1}', true, true],
    ["white space before", " {}", true, true],
    ["white space after", "{} ", true, true],
    ["__proto__ as a member name", '{"__proto__":1}', true, true],
    ["minus zero", '{"a":-0}', true, true],
    ["nesting at the limit", nest(32), true, true],
    ["a value that breaks off", '{"a":', false, false],
    ["a leading zero", '{"a":01}', false, false],
    ["a repeated name", '{"a":1,"a":2}', false, false],
    ["a repeated name deeper down", '{"a":{"b":1,"b":2}}', false, false],
    ["a brace and no more", "{", false, false],
    ["an array", "[1,2]", false, false],
    ["a string", '"x"', false, false],
    ["null", "null", false, false],
    ["something after the value", '{"a":1}x', false, false],
    ["a comma before the brace", '{"a":1,}', false, false],
    ["a number that starts with a dot", '{"a":.5}', false, false],
    ["NaN", '{"a":NaN}', false, false],
    ["single quotes", "{'a':1}", false, false],
    ["a comment", '{"a":1}/*x*/', false, false],
    ["a lone surrogate escape", '{"a":"\\ud800"}', false, false],
    // The four rows where this file is stricter than Go's ValidParams. Go's
    // ParseOpen refuses every one of them too: enc.DecodeStrict allows no
    // null, no nesting past MaxDepth and no number past 2^53-1, raw values
    // included. The price is a no for params that no OPEN could carry.
    ["a null member", '{"a":null}', false, true],
    ["nesting one past the limit", nest(33), false, true],
    ["a number past 2^53-1", '{"a":9007199254740993}', false, true],
    ["an exponent no double holds", '{"a":1e400}', false, true],
  ];
  const got = await shell.evaluate(async (list) => {
    const w = await import("/assets/js/contracts/wire.js");
    const { utf8 } = await import("/assets/js/contracts/b64.js");
    const filled = (n) => { const b = new Uint8Array(n); b.fill(0x20); b[0] = 0x7b; b[n - 1] = 0x7d; return b; };
    return {
      decides: list.map(([, text]) => w.validParams(utf8(text))),
      // Bytes that are no text at all, and the two sizes at the edge. 8192
      // bytes are taken, 8193 are not - MAX_PARAMS, which the budget holds.
      bytes: [
        w.validParams(new Uint8Array([0x7b, 0x22, 0x61, 0x22, 0x3a, 0x22, 0xff, 0x22, 0x7d])),
        w.validParams(filled(8192)),
        w.validParams(filled(8193)),
      ],
      // validateOpen is Go's Open.Validate, so it asks validParams and not
      // the shape alone: an OPEN a caller built with params that break off is
      // refused here, where until this round it was taken.
      open: [
        w.validateOpen({ app: "ngc.desktop", service: "api", action: "apps", window: 262144,
          e2e: new Uint8Array(0), params: utf8('{"a":1}') }),
        w.validateOpen({ app: "ngc.desktop", service: "api", action: "apps", window: 262144,
          e2e: new Uint8Array(0), params: utf8('{"a":') }),
        w.validateOpen({ app: "ngc.desktop", service: "api", action: "apps", window: 262144,
          e2e: new Uint8Array(0), params: utf8('{"a":1,"a":2}') }),
      ],
      // parseOpen keeps Go's other way through, validate(false): the decoder
      // has read these params already, and the answer is the same.
      parses: (() => {
        try {
          w.parseOpen(utf8('{"app":"ngc.desktop","service":"api","action":"apps","params":{"a":1},"window":262144}'));
          return true;
        } catch { return false; }
      })(),
    };
  }, cases);
  // The price against Go, read out of what the code answered and not out of
  // the table's own third column: with `here` on both sides these two would
  // compare the table with itself, and no change to wire.js could make them
  // red. The Go column is the measurement; got.decides is this run.
  //
  // The rows where this file is stricter: it says no, Go says yes.
  expect(cases.filter(([, , , go], i) => !got.decides[i] && go).map(([name]) => name)).toEqual([
    "a null member", "nesting one past the limit", "a number past 2^53-1",
    "an exponent no double holds",
  ]);
  // And the other direction, which has to stay empty: nothing this file takes
  // is anything Go turns down.
  expect(cases.filter(([, , , go], i) => got.decides[i] && !go).map(([name]) => name)).toEqual([]);
  // And every row still answers what the third column says it answers.
  const wrong = cases.filter(([, , here], i) => here !== got.decides[i]).map(([name]) => name);
  expect(wrong).toEqual([]);
  expect(got.bytes).toEqual([false, true, false]);
  expect(got.open).toEqual([true, false, false]);
  expect(got.parses).toBe(true);
});

// CODES is frozen and has no prototype. A plain object answers CODES.toString
// with a function, so a caller who reads CODES[name] for a name from outside
// would hand errorClose something that is no code at all; and a table anyone
// may write to lets whoever else runs on the page turn E_FORBIDDEN into
// another code between the shell reading it and the CLOSE going out. Neither
// is caught by the message table, which is a Map of its own.
test("the code table carries only its own names and cannot be written over", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const w = await import("/assets/js/contracts/wire.js");
    const inherited = ["toString", "constructor", "hasOwnProperty", "valueOf", "isPrototypeOf", "__proto__"];
    // Strict mode throws over a frozen object, sloppy mode does not; either
    // way what counts is the value afterwards.
    const tried = (fn) => { try { fn(); } catch { /* frozen */ } };
    tried(() => { w.CODES.FORBIDDEN = "E_LOGIN"; });
    tried(() => { w.CODES.MADE_UP = "E_MADEUP"; });
    tried(() => { delete w.CODES.EXIT; });
    return {
      prototype: Object.getPrototypeOf(w.CODES),
      frozen: Object.isFrozen(w.CODES),
      inherited: inherited.map((n) => w.CODES[n] === undefined),
      forbidden: w.CODES.FORBIDDEN,
      added: w.CODES.MADE_UP === undefined,
      exit: w.CODES.EXIT,
      names: Object.keys(w.CODES),
    };
  });
  expect(got.prototype).toBe(null);
  expect(got.frozen).toBe(true);
  expect(got.inherited).toEqual([true, true, true, true, true, true]);
  expect(got.forbidden).toBe("E_FORBIDDEN");
  expect(got.added).toBe(true);
  expect(got.exit).toBe("E_EXIT");
  expect(got.names).toEqual([
    "FORBIDDEN", "UNREACHABLE", "EXIT", "TIMEOUT", "LIMIT", "PROTOCOL", "CONFIRM", "LICENSE",
    "NOTFOUND", "VERIFY", "INTERNAL", "TOOLARGE", "LOGIN", "LOCKED",
  ]);
});

// No error of this module repeats what it was given. A marker is put into
// every field a message could pick it up from - the type of a control
// message, a member name, an app id, a status, a code, the bytes of a frame -
// and every call below has to throw and to answer without the marker. The
// decoder's own errors name rules and no input either, and they are folded
// into BadFrame before they leave, so a text from there cannot carry one out.
test("no error repeats what it was given", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const w = await import("/assets/js/contracts/wire.js");
    const { utf8 } = await import("/assets/js/contracts/b64.js");
    const m = "Zq7marker7qZ";
    const out = [];
    const call = (name, fn) => {
      try { fn(); out.push([name, null]); } catch (e) { out.push([name, String(e && e.message)]); }
    };
    call("control type", () => w.parseControl(utf8('{"t":"' + m + '"}')));
    call("control type written", () => w.encodeControl({ t: m }));
    call("control member", () => w.parseControl(utf8('{"t":"bye","' + m + '":1}')));
    call("close code", () => w.parseClose(utf8('{"status":"error","code":"' + m + '"}')));
    call("close status", () => w.parseClose(utf8('{"status":"' + m + '"}')));
    call("close written", () => w.encodeClose({ status: "error", code: m, message: m }));
    call("open app", () => w.parseOpen(utf8('{"app":"' + m + '","service":"api","action":"apps","window":262144}')));
    call("open member", () => w.parseOpen(utf8('{"' + m + '":1}')));
    call("open app written", () => w.encodeOpen({ app: m, service: "api", action: "apps", window: 262144 }));
    call("open params written", () => w.encodeOpen({ app: "ngc.desktop", service: "api", action: "apps",
      window: 262144, params: utf8('{"' + m + '"') }));
    call("frame bytes", () => w.decodeFrame(utf8(m)));
    call("frame type", () => w.encodeFrame({ type: m, channel: 1, payload: new Uint8Array(0) }));
    call("frame payload", () => w.encodeFrame({ type: w.TYPE_DATA, channel: 1, payload: m }));
    call("grant bytes", () => w.parseGrant(utf8(m)));
    call("grant credit", () => w.grant(m, null));
    call("e2e kind", () => w.e2e(m, null));
    call("e2e end body", () => w.e2e(w.E2E_END, utf8(m)));
    call("e2e side", () => w.parseE2E(utf8(m), m));
    call("e2e plaintext", () => w.parseE2E(utf8(m), true));
    return { out, marker: m };
  });
  // Every call has to have thrown: a test that passes because nothing was
  // refused would say nothing about the messages.
  expect(got.out.filter(([, message]) => message === null).map(([name]) => name)).toEqual([]);
  expect(got.out.filter(([, message]) => message.includes(got.marker)).map(([name]) => name)).toEqual([]);
  expect(got.out.length).toBe(19);
});

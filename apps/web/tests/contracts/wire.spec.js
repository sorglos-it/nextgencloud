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

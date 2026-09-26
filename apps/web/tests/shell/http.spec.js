import { test, expect } from "../fixtures/page.js";

// What the shell speaks before the WebSocket. Every answer the API gives is
// played back with route mocking, so this file needs no API at all - and no
// session keys either, so it runs on every engine, WebKit included: nothing
// here reaches for Ed25519 or X25519.
async function mock(page, path, status, body, headers = {}) {
  await page.route("**" + path, (route) =>
    route.fulfill({ status, contentType: "application/json", headers, body }));
}

// Counts what actually left the browser for path, and answers it so no test
// hangs on a request the shell should never have made. The count is the whole
// point of the three cases that say "never sent": without it they would pass
// for a shell that sends the body and only then throws.
//
// The answer is E_INTERNAL and not a 204, so that the code those cases expect
// cannot also come out of a request that WAS sent. Measured: with a 204 here,
// "a name that is no login name is not sent" stayed green while the name
// grammar was taken out, because the 204 became E_PROTOCOL by another road.
async function counter(page, path) {
  const seen = { n: 0 };
  await page.route("**" + path, (route) => {
    seen.n++;
    return route.fulfill({
      status: 500,
      contentType: "application/json",
      body: JSON.stringify({ status: "error", code: "E_INTERNAL", message: "Interner Fehler." }),
    });
  });
  return seen;
}

test("an error is recognised by the code in the CLOSE JSON, not by the status", async ({ shell }) => {
  await mock(shell, "/api/v1/login/start", 429,
    JSON.stringify({ status: "error", code: "E_LOCKED", message: "Zu viele Fehlversuche. Bitte später erneut versuchen." }),
    { "retry-after": "42" });
  const out = await shell.evaluate(async () => {
    const http = await import("/assets/js/net/http.js");
    try {
      await http.loginStart("anna");
      return { thrown: false };
    } catch (e) {
      return { thrown: true, code: e.code, retryAfter: e.retryAfter, message: e.message };
    }
  });
  expect(out).toEqual({ thrown: true, code: "E_LOCKED", retryAfter: 42,
    message: "Zu viele Fehlversuche. Bitte später erneut versuchen." });
});

// The case above cannot fail for the thing its name claims, measured.
// session.httpStatus pairs E_LOCKED with 429 and E_UNREACHABLE with 503, and
// those are exactly the two answers the brief's cases play back - so a shell
// that read res.status, mapped it back and never opened the body at all
// passed both of them, and every other case in this file with them: nine
// green. The two below are the answers where the status and the body
// disagree, and there the body is the only one that may decide. A proxy that
// rewrites a status while passing the payload through is the ordinary way
// they come apart.
test("a 500 that carries an E_LOCKED CLOSE is E_LOCKED, not E_INTERNAL", async ({ shell }) => {
  await mock(shell, "/api/v1/login/start", 500,
    JSON.stringify({ status: "error", code: "E_LOCKED", message: "Zu viele Fehlversuche. Bitte später erneut versuchen." }));
  const out = await shell.evaluate(async () => {
    const http = await import("/assets/js/net/http.js");
    try { await http.loginStart("anna"); return "none"; } catch (e) { return e.code; }
  });
  expect(out).toBe("E_LOCKED");
});

test("a 429 that carries no CLOSE is E_UNREACHABLE, not E_LOCKED", async ({ shell }) => {
  await shell.route("**/api/v1/login/start", (route) =>
    route.fulfill({ status: 429, contentType: "text/html", body: "<html>Too Many Requests</html>" }));
  const out = await shell.evaluate(async () => {
    const http = await import("/assets/js/net/http.js");
    try { await http.loginStart("anna"); return "none"; } catch (e) { return e.code; }
  });
  expect(out).toBe("E_UNREACHABLE");
});

test("an answer that is no JSON counts as E_UNREACHABLE", async ({ shell }) => {
  await shell.route("**/api/v1/login/start", (route) =>
    route.fulfill({ status: 503, contentType: "text/html", body: "<html>Service Unavailable</html>" }));
  const out = await shell.evaluate(async () => {
    const http = await import("/assets/js/net/http.js");
    try { await http.loginStart("anna"); return "none"; } catch (e) { return e.code; }
  });
  // Apache answers 502 or 503 itself when no API replies or its certificate
  // check fails; those answers carry no CLOSE JSON.
  expect(out).toBe("E_UNREACHABLE");
});

test("GET /api/v1/trust answers 204 when nothing newer is there", async ({ shell }) => {
  await mock(shell, "/api/v1/trust*", 204, "");
  const out = await shell.evaluate(async () => {
    const http = await import("/assets/js/net/http.js");
    return await http.trustChain(5);
  });
  expect(out).toBeNull();
});

test("a body over MaxRequest is never sent", async ({ shell }) => {
  const seen = await counter(shell, "/api/v1/setup");
  const out = await shell.evaluate(async () => {
    const http = await import("/assets/js/net/http.js");
    try {
      await http.postSetup(new Uint8Array(65 * 1024));
      return "sent";
    } catch (e) {
      return e.code;
    }
  });
  expect(out).toBe("E_TOOLARGE");
  expect(seen.n).toBe(0);
});

// The mistake this strand paid ten rounds for, on the one place here that
// decides anything about bytes: the size is read once for the check and the
// object is handed to fetch afterwards, so a caller whose length lies gets
// past the bound and puts the real bytes on the wire. b64.bytesOf settles the
// bytes first, and the number checked is then the number sent.
test("the size is decided on the bytes that go out, not on what the object says", async ({ shell }) => {
  const seen = await counter(shell, "/api/v1/setup");
  const out = await shell.evaluate(async () => {
    const http = await import("/assets/js/net/http.js");
    class Liar extends Uint8Array {
      // Small enough to pass every bound; the array itself holds 66560 bytes.
      get length() { return 1; }
    }
    try {
      await http.postSetup(new Liar(65 * 1024));
      return "sent";
    } catch (e) {
      return e.code;
    }
  });
  expect(out).toBe("E_TOOLARGE");
  expect(seen.n).toBe(0);
});

// Retry-After is a header from outside and is read like every other number of
// the contracts: on its digits. Number("0x2a") is 42, and a shell that asks
// Number would tell the person to wait 42 seconds because the answer wrote
// its number in hexadecimal.
test("Retry-After is read on its digits, never through Number", async ({ shell }) => {
  await mock(shell, "/api/v1/login/start", 429,
    JSON.stringify({ status: "error", code: "E_LOCKED", message: "Zu viele Fehlversuche. Bitte später erneut versuchen." }),
    { "retry-after": "0x2a" });
  const out = await shell.evaluate(async () => {
    const http = await import("/assets/js/net/http.js");
    try { await http.loginStart("anna"); return -1; } catch (e) { return e.retryAfter; }
  });
  expect(out).toBe(0);
});

// The budget of a parser only counts once the whole answer is in memory. The
// bound belongs where the bytes arrive, so an answer that runs past it is
// dropped while it is still arriving and never held in one piece.
test("an answer past its bound is dropped, not buffered", async ({ shell }) => {
  await mock(shell, "/api/v1/login/start", 200, "a".repeat(64 * 1024 + 1));
  const out = await shell.evaluate(async () => {
    const http = await import("/assets/js/net/http.js");
    try { await http.loginStart("anna"); return "taken"; } catch (e) { return e.code; }
  });
  expect(out).toBe("E_TOOLARGE");
});

// The API holds LoginStart.Name to trust.ValidLoginName before it looks
// anything up (session.ParseLoginStart), so the shell holds it to the same
// grammar and saves the round trip. The grammar is public and says nothing
// about who exists, so refusing here tells no one anything the API would not
// have told them with the same E_PROTOCOL.
test("a name that is no login name is not sent", async ({ shell }) => {
  const seen = await counter(shell, "/api/v1/login/start");
  const out = await shell.evaluate(async () => {
    const http = await import("/assets/js/net/http.js");
    try { await http.loginStart("Anna"); return "sent"; } catch (e) { return e.code; }
  });
  expect(out).toBe("E_PROTOCOL");
  expect(seen.n).toBe(0);
});

// shared/README.md, "Vor dem WebSocket", gives PathSetup POST and PathLogout
// a 204 and no other success at all. Taking any 2xx would mean the shell
// calls an installation set up on an answer nobody specified - it would be
// believing a proxy rather than the API.
test("a 200 where the contract says 204 is no success", async ({ shell }) => {
  await mock(shell, "/api/v1/setup", 200, "{}");
  await mock(shell, "/api/v1/logout", 200, "");
  const out = await shell.evaluate(async () => {
    const http = await import("/assets/js/net/http.js");
    const codes = [];
    for (const call of [() => http.postSetup(new Uint8Array(1)), () => http.logout()]) {
      try { await call(); codes.push("taken"); } catch (e) { codes.push(e.code); }
    }
    return codes;
  });
  expect(out).toEqual(["E_PROTOCOL", "E_PROTOCOL"]);
});

test("a 204 where the contract says 204 is success", async ({ shell }) => {
  await mock(shell, "/api/v1/setup", 204, "");
  await mock(shell, "/api/v1/logout", 204, "");
  const out = await shell.evaluate(async () => {
    const http = await import("/assets/js/net/http.js");
    const got = [];
    for (const call of [() => http.postSetup(new Uint8Array(1)), () => http.logout()]) {
      try { got.push(await call() === undefined ? "ok" : "value"); } catch (e) { got.push(e.code); }
    }
    return got;
  });
  expect(out).toEqual(["ok", "ok"]);
});

test("the socket address is this page's own, as wss", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const http = await import("/assets/js/net/http.js");
    return { url: http.socketURL(), here: location.host };
  });
  expect(out.url).toBe(`wss://${out.here}/api/v1/ws`);
});

// "link: no connection" nailed down where net/link.js throws it.
//
// WHY ITS OWN FILE AND NOT shell/channels.spec.js, which is where the Link is
// otherwise driven: every case there skips on an engine without X25519 and
// Ed25519, because connectForTest() does the work a login does. This case does
// NOT - Link.connect fails at the socket, before a key is touched - and a case
// switched off for a thing it never touches is a case nobody measured
// (fixtures/page.js, the two guards of contracts/noise.spec.js). So it runs on
// all three engines and asks for no guard.
//
// WHAT OF CLASS 24 IS REACHED HERE, and what is not. COUNTED, because the
// first version of this comment said "four places" from reading the two the
// sheet names, and the grep says six:
//
//   grep -n "link: no connection" apps/web/public/assets/js/net/link.js
//
//   :72  ws.onerror  while opening - the socket never came up    REACHED
//   :73  ws.onclose  while opening - it closed instead           not reached
//   :80  ws.protocol !== SUBPROTOCOL                             not reached
//   :96  a text message where bytes were due                     not reached
//  :101  ws.onclose  while the handshake is being read           not reached
//  :102  ws.onerror  while the handshake is being read           not reached
//
// ONE of six, and the row above says "not reached" for :73 because it was
// MEASURED, not because it was read. This comment first claimed :73 as
// reached, on the reasoning that two sub-cases must use two handlers. Five
// mutants were run one at a time, chromium, 03.11.2026:
//
//   :72 turned into another sentence  -> this case went RED   (exit 1)
//   :73 turned into another sentence  -> this case stayed GREEN (exit 0)
//
// So BOTH sub-cases below arrive through onerror, and onclose is covered by
// nothing. Had both lines been turned in ONE mutant it would have died, and
// the wrong claim would have stood with a green mutant run behind it.
//
// The other five doors need a WebSocket server that answers the upgrade and
// then misbehaves - without the subprotocol, with a text frame, or by dying
// mid-handshake - or one that accepts and closes at once for :73. The mock is
// a PHP cli-server and has none, so they mean a stand-in socket server in
// apps/web/tests that does not exist today.
//
// docs/login-fehlerklassen.md names :80 and :96 for this class - the two that
// are hardest to reach and neither of the two that are easy. Corrected there
// in the same commit.
import { test, expect } from "../fixtures/page.js";

test("a socket that never comes up gives the shell's own sentence", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const { Link } = await import("/assets/js/net/link.js");
    const run = async (path) => {
      const u = new URL(path, location.href);
      u.protocol = location.protocol === "https:" ? "wss:" : "ws:";
      try {
        // No session keys are handed over on purpose: the attempt has to die
        // at the socket, and anything that got past it would need them.
        await new Link().connect(u.toString(), null, null);
        return "connected";
      } catch (e) {
        return e.message;
      }
    };
    return {
      // The web server answers this path with an ordinary 404, so the upgrade
      // never happens and onerror/onclose is what fires.
      missing: await run("/not-a-socket"),
      // Same origin, a path that exists as a document and is no socket
      // either - a second door to the same two handlers.
      document: await run("/index.php"),
    };
  });
  const one = "link: no connection";
  // Equality and not a match: the sentence is the measurement. A SecurityError
  // from the content security policy would read differently, and that is the
  // second thing this holds - "connect-src 'self'" (apps/web/classes/Page.php)
  // lets a socket to this very origin through, so the refusal measured here is
  // net/link.js's and not the policy's.
  expect(got.missing).toBe(one);
  expect(got.document).toBe(one);
});

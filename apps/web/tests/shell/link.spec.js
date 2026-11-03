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
// WHAT OF CLASS 24 IS REACHED HERE, and what is not. net/link.js throws the
// same sentence at four places:
//
//   :73  ws.onerror   - the socket never came up            REACHED below
//   :74  ws.onclose   - it closed instead of opening        REACHED below
//   :80  ws.protocol !== SUBPROTOCOL                        not reached
//   :96  a text message where bytes were due                not reached
//
// The last two need a WebSocket server that answers the upgrade and then
// misbehaves - without the subprotocol, or with a text frame. The mock is a
// PHP cli-server and has none, so bringing them about means a stand-in socket
// server in apps/web/tests that does not exist today. Written down rather than
// claimed: this file covers the sentence, not yet all four of its doors.
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

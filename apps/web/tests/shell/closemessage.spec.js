import { test, expect } from "@playwright/test";

// WHAT THE OTHER SIDE SAID, and whether it reaches a human at all.
//
// A CLOSE carries a sentence beside its code, the contract bounds and
// validates it (contracts/wire.js, close: validText and MAX_MESSAGE_TEXT), and
// net/channel.js used to throw it away: `throw fail(status.code)` passed the
// code alone, and ChannelError built its text out of this shell's own German
// table. Measured on 18.10.2026 over the committed interface: `message`
// appeared exactly once in net/channel.js - that table lookup - and no screen
// read a close's message either. The stand-in's own comment said as much:
// "the real close carries the code as well, and nothing on the app's side
// reads either".
//
// What that cost: a controller that answers E_INTERNAL because the data disk
// cannot encrypt knows what the person has to do (use ext4 or f2fs), and the
// person read "Interner Fehler."
//
// WHAT THIS FILE MEASURES: the step from a real CLOSE frame to the error a
// caller catches, with the contract's own encoder writing the frame and the
// real Channel reading it. Only the link is a stub, and it is one of three
// methods that do nothing - a link cannot make a sentence appear.
//
// WHAT IT DOES NOT MEASURE: the end-to-end path, where the sentence arrives in
// the controller's inner end instead of the transport's CLOSE. That end is a
// whole wire.Close too (Go: e2e.Session.End takes one) and the shell parses it
// with the same parseClose and keeps it in endStatus, so it reaches the same
// line - reasoned from both sides' code, not measured here, and said so.
//
// The screen is measured where the screen is: shell/store.spec.js.

// chan builds a real Channel with a link that does nothing, and hands it one
// frame. The stub has the three methods onCloseFrame calls and no behaviour of
// its own: everything this case is about happens inside the channel.
async function closeWith(page, { code, message }) {
  return page.evaluate(async ({ code, message }) => {
    const wire = await import("/assets/js/contracts/wire.js");
    const { Channel } = await import("/assets/js/net/channel.js");
    const link = { handOver() {}, free() {}, async send() {} };
    const ch = new Channel(link, 1, { window: wire.MIN_WINDOW, e2e: null });
    // The frame is written by the contract's own encoder, so what the channel
    // reads is what a server would really send - a hand-built object would
    // measure this test's idea of a close.
    const payload = wire.encodeClose({ status: "error", code, message, exitCode: null });
    const waiting = ch.result(wire.MAX_CALL_RESULT).then(
      () => ({ threw: false }),
      (e) => ({
        threw: true, name: e?.name ?? "", code: e?.code ?? "",
        said: e?.message ?? "", closeMessage: e?.closeMessage ?? "",
      }),
    );
    await ch.receive({ type: wire.TYPE_CLOSE, channel: 1, payload });
    return { ...(await waiting), table: wire.message(code) };
  }, { code, message });
}

test("a close that carries its own sentence hands that sentence to the caller", async ({ page }) => {
  await page.goto("/");
  const own = "Bitte ext4 oder f2fs mit aktivierter Verschlüsselung verwenden.";
  const got = await closeWith(page, { code: "E_INTERNAL", message: own });
  expect(got.threw).toBe(true);
  expect(got.name).toBe("ChannelError");
  // The code is unchanged: it is what every caller branches on.
  expect(got.code).toBe("E_INTERNAL");
  // And so is the error's own message - this shell's table text. Callers read
  // it (apps/users.js, reason()), and a message that came from a server would
  // send those branches somewhere else.
  expect(got.said).toBe(got.table);
  expect(got.said).toBe("Interner Fehler.");
  // The server's words arrive beside it, whole.
  expect(got.closeMessage).toBe(own);
});

test("a close that only repeats the code's own sentence adds nothing", async ({ page }) => {
  // THE ORDINARY CASE, and the reason this change is safe: the contract fills
  // a close with the code's own German sentence (Go: wire.Error), so almost
  // every close that ever arrives carries exactly what the shell would say.
  // Such a sentence is not handed on - otherwise every screen would gain a
  // doubled text and nobody could tell a server's words from the shell's.
  await page.goto("/");
  const got = await closeWith(page, { code: "E_INTERNAL", message: "Interner Fehler." });
  expect(got.threw).toBe(true);
  expect(got.code).toBe("E_INTERNAL");
  expect(got.said).toBe("Interner Fehler.");
  expect(got.closeMessage).toBe("");
});

test("a close with no sentence at all leaves the caller with the code alone", async ({ page }) => {
  await page.goto("/");
  const got = await closeWith(page, { code: "E_VERIFY", message: "" });
  expect(got.threw).toBe(true);
  expect(got.code).toBe("E_VERIFY");
  expect(got.said).toBe(got.table);
  expect(got.closeMessage).toBe("");
});

test("the sentence of ANOTHER code is a sentence of its own and arrives", async ({ page }) => {
  // The control for the comparison above: it holds against the sentence of
  // THIS code, not against the table as a whole. A server that answers
  // E_VERIFY with the wording of E_INTERNAL has said something the shell would
  // not have said here, and the person gets to see it.
  await page.goto("/");
  const got = await closeWith(page, { code: "E_VERIFY", message: "Interner Fehler." });
  expect(got.threw).toBe(true);
  expect(got.code).toBe("E_VERIFY");
  expect(got.said).toBe(got.table);
  expect(got.closeMessage).toBe("Interner Fehler.");
});

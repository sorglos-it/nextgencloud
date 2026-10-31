import { readFileSync } from "node:fs";
import { test, expect } from "@playwright/test";

// WHAT THE OTHER SIDE SAID, and whether it reaches a human at all.
//
// A CLOSE carries a sentence beside its code, the contract bounds and
// validates it (contracts/wire.js, close: validText and MAX_MESSAGE_TEXT), and
// net/channel.js used to throw it away: `throw fail(status.code)` passed the
// code alone, and ChannelError built its text out of this shell's own German
// table. Measured on 18.10.2026 over the committed interface: `message`
// appeared exactly once in net/channel.js - that table lookup - and no screen
// read a close's message either.
//
// What that cost: a controller that answers E_INTERNAL because the data disk
// cannot encrypt knows what the person has to do (use ext4 or f2fs), and the
// person read "Interner Fehler."
//
// WHAT IT IS NOW, and the two halves are not the same decision:
//   - WHETHER a sentence is kept is decided in ONE place, ChannelError: only
//     for a general code (wire.isGeneral - today E_INTERNAL alone), only what
//     parseClose let through, and never when it merely repeats the shell's own
//     sentence. A server may not write over a wording that says what to do.
//   - WHETHER IT ARRIVES AT ALL is decided where a close turns into a throw,
//     and there are TWO such places in that file: result(), for a channel the
//     other side granted and then ended with an error, and settle(), for one
//     refused before its first grant. Both are measured below, because one of
//     them was carrying nothing: the ordinary refusal of an action - open,
//     grant, work, close with an error - goes through result(), and that is
//     the path the encryption sentence travels.
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

// afterGrant is the ordinary way a refusal arrives: the channel was accepted
// and the caller is reading its answer when the CLOSE comes in. The frame is
// written by the contract's own encoder, so what the channel reads is what a
// server would really send - a hand-built object would measure this test's
// idea of a close.
async function afterGrant(page, { code, message }) {
  return page.evaluate(async ({ code, message }) => {
    const wire = await import("/assets/js/contracts/wire.js");
    const { Channel } = await import("/assets/js/net/channel.js");
    const link = { handOver() {}, free() {}, async send() {} };
    const ch = new Channel(link, 1, { window: wire.MIN_WINDOW, e2e: null });
    const payload = wire.encodeClose({ status: "error", code, message, exitCode: null });
    const waiting = ch.result(wire.MAX_CALL_RESULT).then(
      () => ({ threw: false }),
      (e) => ({
        threw: true, name: e?.name ?? "", code: e?.code ?? "",
        said: e?.message ?? "", serverText: e?.serverText ?? "",
      }),
    );
    await ch.receive({ type: wire.TYPE_CLOSE, channel: 1, payload });
    return { ...(await waiting), table: wire.message(code) };
  }, { code, message });
}

// beforeGrant is the other door: the other side refused the channel instead of
// granting it, so the error comes out of the wait for the first grant. Same
// frame, same channel, different caller - and it has to say the same thing.
async function beforeGrant(page, { code, message }) {
  return page.evaluate(async ({ code, message }) => {
    const wire = await import("/assets/js/contracts/wire.js");
    const { Channel } = await import("/assets/js/net/channel.js");
    const link = { handOver() {}, free() {}, async send() {} };
    const ch = new Channel(link, 1, { window: wire.MIN_WINDOW, e2e: null });
    const payload = wire.encodeClose({ status: "error", code, message, exitCode: null });
    const waiting = ch.firstGrant().then(
      () => ({ threw: false }),
      (e) => ({
        threw: true, name: e?.name ?? "", code: e?.code ?? "",
        said: e?.message ?? "", serverText: e?.serverText ?? "",
      }),
    );
    await ch.receive({ type: wire.TYPE_CLOSE, channel: 1, payload });
    return { ...(await waiting), table: wire.message(code) };
  }, { code, message });
}

const OWN = "Bitte ext4 oder f2fs mit aktivierter Verschlüsselung verwenden.";

// noFscryptText is the controller's real sentence, READ OUT OF THE GO SOURCE:
// apps/server-controller/classes/runtime/data.go holds it, and
// serve/activate.go puts it into a CLOSE under E_INTERNAL. A copy in this file
// would keep passing after somebody reworded the German, and the length is the
// point here - this sentence has to fit through parseClose, which holds
// message against validText and MAX_MESSAGE_TEXT.
function noFscryptText() {
  const src = readFileSync(new URL("../../../../apps/server-controller/classes/runtime/data.go", import.meta.url), "utf8");
  const at = src.indexOf("const NoFscryptText =");
  if (at < 0) throw new Error("NoFscryptText is not in runtime/data.go any more");
  const parts = [...src.slice(at, src.indexOf("\n\n", at)).matchAll(/"((?:[^"\\]|\\.)*)"/g)].map((m) => m[1]);
  if (parts.length === 0) throw new Error("NoFscryptText has no text in runtime/data.go");
  return parts.join("");
}

test("the controller's real sentence about encryption survives a real frame", async ({ page }) => {
  // THE OCCASION, with the contract's own encoder writing the frame and
  // parseClose reading it: the sentence the controller sends is not too long
  // for a CLOSE and is not refused as text, and it comes out of the channel
  // whole and under the code the controller really uses.
  await page.goto("/");
  const said = noFscryptText();
  expect(said).toContain("ext4");
  const got = await afterGrant(page, { code: "E_INTERNAL", message: said });
  expect(got.threw).toBe(true);
  expect(got.code).toBe("E_INTERNAL");
  expect(got.serverText).toBe(said);
});

test("a close after the grant hands its own sentence to the caller", async ({ page }) => {
  // THE PATH OF THE ORDINARY REFUSAL, and the one that carried nothing: an
  // action is opened, granted, done, and the controller ends it with an error.
  // callBytes waits for the grant and then reads result(), so this is where
  // the activation of an app learns that the disk cannot encrypt.
  await page.goto("/");
  const got = await afterGrant(page, { code: "E_INTERNAL", message: OWN });
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
  expect(got.serverText).toBe(OWN);
});

test("a close before the grant hands its own sentence to the caller", async ({ page }) => {
  await page.goto("/");
  const got = await beforeGrant(page, { code: "E_INTERNAL", message: OWN });
  expect(got.threw).toBe(true);
  expect(got.name).toBe("ChannelError");
  expect(got.code).toBe("E_INTERNAL");
  expect(got.said).toBe("Interner Fehler.");
  expect(got.serverText).toBe(OWN);
});

test("a close that only repeats the code's own sentence adds nothing", async ({ page }) => {
  // THE ORDINARY CASE, and the reason this change is safe: the contract fills
  // a close with the code's own German sentence (Go: wire.Error), so almost
  // every close that ever arrives carries exactly what the shell would say.
  // Such a sentence is not handed on - otherwise every screen would gain a
  // doubled text and nobody could tell a server's words from the shell's.
  await page.goto("/");
  const got = await afterGrant(page, { code: "E_INTERNAL", message: "Interner Fehler." });
  expect(got.threw).toBe(true);
  expect(got.code).toBe("E_INTERNAL");
  expect(got.said).toBe("Interner Fehler.");
  expect(got.serverText).toBe("");
});

test("a close with no sentence at all leaves the caller with the code alone", async ({ page }) => {
  await page.goto("/");
  const got = await afterGrant(page, { code: "E_VERIFY", message: "" });
  expect(got.threw).toBe(true);
  expect(got.code).toBe("E_VERIFY");
  expect(got.said).toBe(got.table);
  expect(got.serverText).toBe("");
});

test("a sentence under a code that says what to do is NOT handed on", async ({ page }) => {
  // THE DECISION ITSELF, and it is the narrower of the two that were on the
  // table. A sentence is kept only where the shell's own says nothing to act
  // on - E_VERIFY says "Unterschrift ungültig.", which names the thing that is
  // wrong, so a server has no business putting its words beside it. Both doors
  // are held to it, because a guard in ChannelError that one path went around
  // would be no guard.
  await page.goto("/");
  const after = await afterGrant(page, { code: "E_VERIFY", message: OWN });
  expect(after.threw).toBe(true);
  expect(after.code).toBe("E_VERIFY");
  expect(after.said).toBe("Unterschrift ungültig.");
  expect(after.serverText).toBe("");
  const before = await beforeGrant(page, { code: "E_VERIFY", message: OWN });
  expect(before.threw).toBe(true);
  expect(before.code).toBe("E_VERIFY");
  expect(before.serverText).toBe("");
});

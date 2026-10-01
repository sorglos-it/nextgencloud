import { test, expect, signIn, skipWithoutLogin } from "../fixtures/page.js";

// The terminal - org.sorglos.terminal - in the fenced frame every app runs in.
//
// WHAT IS REAL HERE AND WHAT IS NOT, said first because it decides what these
// cases are worth. Real: the app's page, stylesheet and script come out of an
// archive whose checksum a store's signature vouches for, the shell opens a
// channel that stays open, the app writes to it and reads from it, and every
// byte that comes back goes on screen as text. Standing in: THE SERVICE. The
// controller builds it (apps/server-controller/, "virtuelles Terminal"); it runs
// the command in the app's own container and answers with what the program
// wrote. It does not exist yet, so shell/fake-link.js answers two canned lines.
//
// SO WHAT IS NOT MEASURED ANYWHERE, and no case below pretends otherwise: that
// a real service answers at all, what `cp -r` says on the machine, how its
// output is split over the wire, and what the service makes of the cols and rows
// the app sends it.
//
// THE BRIEF'S FOUR CASES ARE ALL HERE. Three of them are the brief's own; its
// fourth is the calculator's and lives in calc.spec.js, where the nine other
// cases about the calculator stand. What was added: the skip below, which every
// case that needs a desktop has (the brief had none, and four cases would have
// been red on an engine without Ed25519), and four more at the foot of the file
// - an ending that is NOT an error, which is what lets the cut-off case fail for
// its own reason; the window closing on an open channel; the keyboard; the names
// a screen reader reads.
test.beforeEach(async ({ shell }) => {
  await skipWithoutLogin(shell);
  await signIn(shell);
  await expect(shell.locator("#taskbar")).toBeVisible();
});

// winOf finds a window by the name in its title bar. NOT by any text the window
// holds: the app store's own surface says "Rechner" too - about a machine - and
// since task 25 it lists the Rechner among the apps of the store, so a window
// matched by its text is two windows.
const winOf = (shell, title) =>
  shell.locator(".win").filter({ has: shell.locator(".titlebar .t", { hasText: title }) });

async function terminal(shell) {
  await shell.getByRole("button", { name: "Start" }).click();
  await shell.getByRole("menuitem", { name: "Terminal", exact: true }).click();
  const win = winOf(shell, "Terminal");
  await expect(win).toBeVisible();
  const frame = shell.frameLocator(".appframe");
  return { win, frame, out: frame.locator(".out"), input: frame.getByRole("textbox", { name: "Eingabe" }) };
}

// ready waits for the channel to be open. The field is disabled until it is -
// a command typed into a terminal that has no channel is a command the person
// thinks they sent - so this is the app's own answer to "are you connected".
async function ready(t) {
  await expect(t.input).toBeEnabled();
}

test("the terminal shows the output of the service", async ({ shell }) => {
  const t = await terminal(shell);
  await ready(t);
  await t.input.fill("cp -r");
  await t.input.press("Enter");
  await expect(t.out).toContainText("cp: missing destination");
  // The line the person typed stands in front of the answer: without it the
  // answers would stand there without their questions.
  await expect(t.out).toContainText("$ cp -r");
  // And the field is empty again, ready for the next one.
  await expect(t.input).toHaveValue("");
});

test("output from the service is never HTML", async ({ shell }) => {
  await shell.evaluate(() => { window.__ngcTest.terminalOutput = "<img src=x onerror=alert(1)>"; });
  const t = await terminal(shell);
  await ready(t);
  await t.input.press("Enter");
  await expect(t.out).toContainText("<img src=x onerror=alert(1)>");
  expect(await t.out.locator("img").count()).toBe(0);
  // Nowhere on the page, not only in the output: a service's bytes must not
  // have become an element anywhere. The output holds lines, and a line holds
  // text and nothing else.
  expect(await t.out.evaluate((e) => [...e.querySelectorAll("*")]
    .filter((n) => n.tagName !== "DIV").length)).toBe(0);
  // The two scripts of the page are the shell's own, written in as data:
  // addresses; what an "<img ...>" out of an answer could have become is not
  // there at all.
  expect(await t.frame.locator("img, svg, iframe, object, embed").count()).toBe(0);
});

test("a cut-off stream is an error", async ({ shell }) => {
  await shell.evaluate(() => { window.__ngcTest.terminalCut = true; });
  const t = await terminal(shell);
  await expect(t.frame.getByRole("alert")).toHaveText("Die Verbindung wurde abgeschnitten.");
  // And nothing more can be typed into a channel that is gone.
  await expect(t.input).toBeDisabled();
});

test("a stream that ends with ok is not an error", async ({ shell }) => {
  // The other half of the case above, and the one that makes it able to fail:
  // the app must not cry "cut" at every ending. Strg+D is the ending the app
  // asks for itself, and the stand-in answers it with "ok".
  const t = await terminal(shell);
  await ready(t);
  await t.input.fill("cp -r");
  await t.input.press("Enter");
  await expect(t.out).toContainText("cp: missing destination");
  await t.input.press("Control+d");
  await expect(t.input).toBeDisabled();
  await expect(t.frame.locator(".hint")).toContainText("Die Verbindung ist beendet.");
  await expect(t.frame.getByRole("alert")).toHaveCount(0);
});

test("the window closing takes the channel with it", async ({ shell }) => {
  // A channel nobody reads any more still counts against the session's bound and
  // leaves a service running on the other side. Counted on the stand-in's own
  // number - how many of the terminal's channels the shell has closed - and not
  // on anything about the window: a case that only looked at the window would go
  // green against a shell that never closes a channel at all.
  const t = await terminal(shell);
  await ready(t);
  expect(await shell.evaluate(() => window.__ngcStreamsClosed)).toBe(0);
  const opened = await shell.evaluate(() => window.__ngcChannelsOpened);
  await t.win.getByRole("button", { name: "Schließen" }).click();
  await expect(t.win).toHaveCount(0);
  await expect(shell.locator(".appframe")).toHaveCount(0);
  await expect.poll(() => shell.evaluate(() => window.__ngcStreamsClosed)).toBe(1);
  // And the next window opens a channel of its own instead of finding the old
  // one: the app's numbers start at one again in a document that is new.
  const again = await terminal(shell);
  await ready(again);
  expect(await shell.evaluate(() => window.__ngcChannelsOpened)).toBeGreaterThan(opened);
  expect(await shell.evaluate(() => window.__ngcStreamsClosed)).toBe(1);
});

test("the terminal can be worked with the keyboard alone", async ({ shell }) => {
  // The field has the focus as soon as there is a channel, so somebody who
  // never touches a mouse can type and press Enter and be done.
  const t = await terminal(shell);
  await ready(t);
  await expect(t.input).toBeFocused();
  await shell.keyboard.type("ls");
  await shell.keyboard.press("Enter");
  await expect(t.out).toContainText("$ ls");
  await expect(t.out).toContainText("command not found");
});

test("every control of the terminal has a name a screen reader can read", async ({ shell }) => {
  const t = await terminal(shell);
  await ready(t);
  const nameless = await t.frame.locator("button, input").evaluateAll((cs) => cs
    .filter((c) => !(c.getAttribute("aria-label") || c.textContent || "").trim())
    .map((c) => c.className || c.tagName));
  expect(nameless).toEqual([]);
  // The output is a log: lines are added at the end and stay. A reader has to
  // be told that, and told when something arrives.
  await expect(t.out).toHaveAttribute("role", "log");
  await expect(t.out).toHaveAttribute("aria-live", "polite");
});

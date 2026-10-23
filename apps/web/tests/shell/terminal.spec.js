import { test, expect, signIn, skipWithoutLogin } from "../fixtures/page.js";

// The terminal - org.sorglos.terminal - in the fenced frame every app runs in.
//
// WHAT IS REAL HERE AND WHAT IS NOT, said first because it decides what these
// cases are worth. Real: the app's page, stylesheet and script come out of an
// archive whose checksum a store's signature vouches for, the shell opens a
// channel that stays open, the app writes to it and reads from it, and every
// byte that comes back goes on screen as text. Standing in: THE SERVICE. The
// controller has built it (apps/server-controller/classes/terminal/,
// "virtuelles Terminal"); it runs a shell on a pseudo terminal in the app's own
// container and answers with what the program wrote. It needs Linux, so
// shell/fake-link.js stands in - but since this task it stands in properly: it
// reads the app's frames with its own parser and echoes like a pty.
//
// SO WHAT IS NOT MEASURED ANYWHERE, and no case below pretends otherwise: that
// the real service answers at all, what `cp -r` says on the machine, how its
// output is split over the wire, and whether a real pty echo looks like the
// stand-in's.
//
// THE BRIEF'S FOUR CASES ARE ALL HERE. Three of them are the brief's own; its
// fourth is the calculator's and lives in calc.spec.js, where the nine other
// cases about the calculator stand. What was added: the skip below, which every
// case that needs a desktop has (the brief had none, and four cases would have
// been red on an engine without Ed25519), and four more after the brief's own -
// an ending that is NOT an error, which is what lets the cut-off case fail for
// its own reason; the window closing on an open channel; the keyboard; the names
// a screen reader reads. The four at the FOOT of the file came later and are
// about the format of the channel; they have their own heading there. Eleven in
// all, counted against `--list`.
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
  // The line the person typed stands in front of the answer - and it is there
  // because the SERVICE sent it back, not because the app wrote it. The real
  // service runs a shell on a pseudo terminal (pty_linux.go), and a pty echoes
  // what is typed; an app that echoed as well would put every line on the
  // screen twice - and would put a password on it that the pty deliberately
  // kept quiet.
  await expect(t.out).toContainText("cp -r");
  // ONCE, and that is the fence under the sentence above: only the service
  // echoes. The answer to this line ("cp: missing destination ...") does not
  // hold the line itself, so a second "cp -r" on the screen could only come
  // from the app writing it as well - and that is what would put a password
  // there that the pty kept quiet.
  expect(await t.out.evaluate((e) => (e.textContent.match(/cp -r/g) ?? []).length)).toBe(1);
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

test("a cut-off stream is an error, and it says what the shell said", async ({ shell }) => {
  await shell.evaluate(() => { window.__ngcTest.terminalCut = true; });
  const t = await terminal(shell);
  // TWO SENTENCES: what happened, and the reason the shell sent with it. Until
  // 23.10.2026 the second one was thrown away at three stations in a row, and
  // the person was left with the first alone (app.js, reason).
  await expect(t.frame.getByRole("alert"))
    .toHaveText("Die Verbindung wurde abgeschnitten. Dienst nicht erreichbar.");
  // And nothing more can be typed into a channel that is gone.
  await expect(t.input).toBeDisabled();
});

test("an open the shell could not make says the shell's own sentence", async ({ shell }) => {
  await shell.evaluate(() => {
    window.__ngcTest.openFails = "Diese Anmeldung kann den Auftrag für diese App nicht unterschreiben. Bitte neu anmelden.";
  });
  const t = await terminal(shell);
  // THE WHOLE WAY, in one case: the open throws in the shell
  // (net/world.js), shell/appframe.js carries the sentence to the app instead
  // of replacing every failure with "Dienst nicht erreichbar."
  // (whyOpenFailed), and the app puts it on the screen behind its own
  // sentence. Measured on 23.10.2026, all three stations threw it away and the
  // person was left with the first sentence alone - and no red line anywhere,
  // because nothing had gone wrong uncaught.
  await expect(t.frame.getByRole("alert")).toHaveText(
    "Die Verbindung wurde abgeschnitten. "
    + "Diese Anmeldung kann den Auftrag für diese App nicht unterschreiben. Bitte neu anmelden.",
  );
  await expect(t.input).toBeDisabled();
});

test("a refused open says the sentence of its code, not the words of the error",
  async ({ shell }) => {
    await shell.evaluate(() => { window.__ngcTest.openFailsCode = "E_CONFIRM"; });
    const t = await terminal(shell);
    // The CODE becomes the sentence - the German text of the contract
    // (wire.message) - and the words the error itself carried do not travel:
    // those are for a log, not for a window the person reads.
    await expect(t.frame.getByRole("alert"))
      .toHaveText("Die Verbindung wurde abgeschnitten. Bitte erneut bestätigen.");
    await expect(t.frame.getByRole("alert")).not.toContainText("fake-link");
  });

test("a refused open also says the reason on the desktop's own strip", async ({ shell }) => {
  await shell.evaluate(() => {
    window.__ngcTest.openFails = "Nicht bestätigt. Die App wurde nicht geöffnet.";
  });
  const t = await terminal(shell);
  // THE STRIP BELONGS TO THE SHELL, and that is the point of this case:
  // telling the app is not enough. Measured on 23.10.2026, the terminal that
  // was installed on the Debian machine had been built before there was a
  // reason to pass on, so it showed its own sentence and nothing else - while
  // the shell knew the reason all along.
  await expect(shell.getByRole("status")).toHaveText(
    "Terminal: Nicht bestätigt. Die App wurde nicht geöffnet.",
  );
  // AND IT WAITS FOR THE PERSON. The class is what says so: desktop.notice
  // clears a strip of any other level by itself after NOTICE_MS, and measured
  // on 23.10.2026 it did - the reason was gone before it had been read.
  await expect(shell.getByRole("status")).toHaveClass("status error");
  // The window stands and the app keeps its own sentence as well: the strip is
  // beside it, not instead of it.
  await expect(t.frame.getByRole("alert")).toContainText("Nicht bestätigt.");
});

test("an ending with no sentence says the one thing it knows", async ({ shell }) => {
  await shell.evaluate(() => {
    window.__ngcTest.terminalCut = true;
    window.__ngcTest.terminalSaid = "";
  });
  const t = await terminal(shell);
  // No sentence from the shell, no empty space behind the full stop: the app
  // says what it knows and nothing more.
  await expect(t.frame.getByRole("alert")).toHaveText("Die Verbindung wurde abgeschnitten.");
  await expect(t.input).toBeDisabled();
});

test("the same sentence twice is said once", async ({ shell }) => {
  await shell.evaluate(() => {
    window.__ngcTest.terminalCut = true;
    window.__ngcTest.terminalSaid = "Die Verbindung wurde abgeschnitten.";
  });
  const t = await terminal(shell);
  // The shell may send the very sentence this app would show on its own - a
  // code of the contract whose German text reads the same. Saying it twice is
  // one time too many (app.js, reason).
  await expect(t.frame.getByRole("alert")).toHaveText("Die Verbindung wurde abgeschnitten.");
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
  await expect(t.out).toContainText("ls");
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

// ---------------------------------------------------------------------------
// THE FORMAT OF THE CHANNEL (contract point 29, decided by the controller on
// 12.10.2026 and agreed with this session). Every frame carries its own
// length: one byte kind, two bytes length big endian, then the payload. The
// reason is measured, not chosen - serve.shellInput hands the inner messages
// on as one byte STREAM, so the boundary of a message is gone by the time the
// service sees it, and a keys frame without a length has no end.
//
// WHAT THESE FOUR CASES MEASURE AND WHAT THEY DO NOT. The expected bytes below
// are written out by hand from apps/server-controller/classes/terminal/
// terminal.go on main; the stand-in in shell/fake-link.js reads them with its
// own parser, written from the same file and importing nothing of the app's.
// So what is measured is that two independent readings of the contract meet -
// NOT that the real service answers, and not that a real pty echoes. Both need
// the controller's service on a Linux machine.
const TermKeys = 1;             // terminal.KindKeys
const TermResize = 2;           // terminal.KindResize
const MAX_KEYS = 16 << 10;      // terminal.MaxKeys
const MAX_DIM = 2000;           // terminal.MaxDim

// framesOf and rawOf read the stand-in's two records: the frames it parsed and
// the bytes of every write before any parser touched them.
const framesOf = (shell) => shell.evaluate(() => window.__ngcTermFrames);
const rawOf = (shell) => shell.evaluate(() => window.__ngcTermRaw);

// noBadFrame is the fence under every case here: the stand-in is as strict as
// the controller (pumpIn ends the run on the first frame that is none), so a
// frame the service could not read shows up as this one entry - and the
// channel is gone.
function noBadFrame(frames) {
  expect(frames.filter((f) => f.kind === "bad")).toEqual([]);
}

test("the first thing the terminal sends is the window size, byte for byte", async ({ shell }) => {
  const t = await terminal(shell);
  await ready(t);
  await expect.poll(async () => (await rawOf(shell)).length).toBeGreaterThan(0);
  const raw = await rawOf(shell);
  // Kind 2, length 4, then cols and rows as uint16 big endian. The size has to
  // go first and it has to go at all: terminal.go reads no params, so a size
  // frame is the ONLY way the service learns how wide the window is.
  expect(raw[0].slice(0, 3)).toEqual([TermResize, 0, 4]);
  expect(raw[0].length).toBe(7);
  const cols = (raw[0][3] << 8) | raw[0][4];
  const rows = (raw[0][5] << 8) | raw[0][6];
  expect(cols).toBeGreaterThanOrEqual(1);
  expect(rows).toBeGreaterThanOrEqual(1);
  expect(cols).toBeLessThanOrEqual(MAX_DIM);
  expect(rows).toBeLessThanOrEqual(MAX_DIM);
  // And the other side read those same bytes as one size frame, with the same
  // two numbers. Both halves are needed: the first says what went out, this
  // says that a reader of the contract gets the numbers back out of it.
  const frames = await framesOf(shell);
  noBadFrame(frames);
  expect(frames[0]).toEqual({ kind: "resize", len: 4, cols, rows });
});

test("a typed line goes out as one keys frame, byte for byte", async ({ shell }) => {
  const t = await terminal(shell);
  await ready(t);
  await t.input.fill("ls");
  await t.input.press("Enter");
  await expect(t.out).toContainText("command not found");
  const keys = (await rawOf(shell)).filter((f) => f[0] === TermKeys);
  // 1, length 3, then "ls\n". Written out as the numbers they are, so the
  // header and the payload are both nailed down by one comparison: a length
  // byte pair that counted characters, or counted the header in, is a
  // different array.
  expect(keys).toEqual([[TermKeys, 0, 3, 0x6c, 0x73, 0x0a]]);
  noBadFrame(await framesOf(shell));
});

test("a line longer than one frame is split, and every frame keeps the bound", async ({ shell }) => {
  const t = await terminal(shell);
  await ready(t);
  const long = "x".repeat(20000);
  await t.input.fill(long);
  await t.input.press("Enter");
  // 20001 bytes with the line break: one full frame and the rest. BOTH numbers
  // stand here - a case that only held the upper bound would stay green for a
  // sender that cut every frame to one byte.
  await expect.poll(async () => (await framesOf(shell)).filter((f) => f.kind === "keys").length).toBe(2);
  const frames = await framesOf(shell);
  noBadFrame(frames);
  const keys = frames.filter((f) => f.kind === "keys");
  expect(keys.map((f) => f.len)).toEqual([MAX_KEYS, 20001 - MAX_KEYS]);
  // And an empty frame is not a frame: ParseFrame refuses n == 0 for keys, so a
  // sender that padded or flushed an empty rest would kill the channel.
  expect(keys.filter((f) => f.len === 0)).toEqual([]);
  // Nothing was lost or doubled at the seam between the two frames: the
  // stand-in echoes what it was given, the way a pty does, so the whole line
  // stands back on the screen in ONE unbroken run of 20000 - which is what a
  // cut at the wrong byte would break. (It stands there twice over: the echo,
  // and once more inside the "command not found" line. That is the stand-in's
  // doing, so the run is what is held here, not a count.)
  await expect.poll(() => t.out.evaluate((e, n) => e.textContent.includes("x".repeat(n)), 20000)).toBe(true);
});

test("a window wider than the service allows is reported as the limit", async ({ shell }) => {
  await shell.setViewportSize({ width: 1800, height: 1000 });
  const t = await terminal(shell);
  await ready(t);
  // One character made tiny, so the box really measures more columns than a
  // size frame can carry. The clamp is measured THROUGH the app's own
  // measurement, not past it with a hook - and without it the stand-in refuses
  // the frame (cols > MaxDim ends the run), which is what the real service
  // does.
  await t.out.evaluate((e) => { e.style.fontSize = "1px"; e.style.lineHeight = "1px"; });
  const before = (await framesOf(shell)).length;
  // Maximizing is how a person makes a window wide, and it is what gives the
  // frame inside a new size at all: a resize event reaches the app only when
  // ITS box changes, not when the page around it does.
  await t.win.getByRole("button", { name: "Maximieren" }).click();
  await expect.poll(async () => (await framesOf(shell)).length).toBeGreaterThan(before);
  const frames = await framesOf(shell);
  noBadFrame(frames);
  const last = frames.filter((f) => f.kind === "resize").at(-1);
  expect(last.cols).toBe(MAX_DIM);
  // The rows are NOT clamped here, and that is the other side of the same
  // fence: a sender that answered the limit for everything would pass a case
  // that only looked at the columns.
  expect(last.rows).toBeLessThan(MAX_DIM);
  expect(last.rows).toBeGreaterThanOrEqual(1);
});

// org.sorglos.terminal - the terminal, inside the frame every app gets.
//
// WHAT IS HERE AND WHAT IS NOT. This is the face of the app. Behind it stands
// the service "virtuelles Terminal" of the controller
// (apps/server-controller/), which runs the commands in the app's own
// container; it does not exist yet, and nothing in this file would notice if it
// did - the app asks for a channel to its own action and the shell takes it
// from there.
//
// THE CHANNEL IS A STREAM, NOT A QUESTION. ngc.open stays open: what is typed
// goes out with write, Strg+D says "no more input" with end, and what the
// service writes arrives in onData, in as many pieces as the wire happened to
// make. It counts as finished only when the close says "ok"; anything else is a
// connection that was cut, and the person is told so rather than left with half
// an answer that looks whole.
//
// EVERY BYTE FROM THE SERVICE IS TEXT. The prototype puts the output into the
// page with innerHTML - exactly the mistake the design names (4.6). Here it
// lands on textContent, so a service that answers with "<img src=x
// onerror=...>" has written those characters on the screen and nothing else.
//
// A CLASSIC SCRIPT, NOT A MODULE: see the calculator's app.js for why the
// frame cannot run one.
(() => {
  "use strict";

  const enc = new TextEncoder();

  // What the person is told. One sentence each, and none of them says anything
  // about the inside of a server: which rule of which station broke is nothing
  // anybody here can act on.
  const CONNECTING = "Verbindung wird aufgebaut …";
  const READY = "Befehl eingeben und mit der Eingabetaste abschicken.";
  const ENDED = "Die Verbindung ist beendet. Zum Weiterarbeiten das Fenster schließen und neu öffnen.";
  const CUT = "Die Verbindung wurde abgeschnitten.";
  // After a connection that was cut the alert says what happened, so the line
  // under the field only has to say what to do next - the same thing twice is
  // one thing too many to read.
  const AGAIN = "Zum Weiterarbeiten das Fenster schließen und neu öffnen.";

  // How large the window is in characters. The service lays its answers out in
  // columns, so it is told how many there are. Both are measured and not
  // guessed, and the fallbacks are the size of a terminal nobody resized -
  // reached only if the box has no size yet, and then a wrong number is still
  // better than none.
  const COLS = 80;
  const ROWS = 24;

  function el(tag, cls, txt) {
    const e = document.createElement(tag);
    if (cls) e.className = cls;
    if (txt !== undefined && txt !== null) e.textContent = String(txt);
    return e;
  }

  const box = el("div", "term");
  // role="log" says what this is: lines that are added at the end and stay.
  // aria-live as well, because not every reader maps the role to one.
  const out = el("div", "out");
  out.setAttribute("role", "log");
  out.setAttribute("aria-live", "polite");
  const ask = el("div", "ask");
  const sign = el("span", "sign", "$");
  // The sign is decoration: the field beside it says what it is for, and a
  // reader that announced the dollar would announce a character, not a name.
  sign.setAttribute("aria-hidden", "true");
  const input = el("input", null);
  input.type = "text";
  input.autocomplete = "off";
  input.spellcheck = false;
  input.setAttribute("aria-label", "Eingabe");
  // Nothing may be typed before there is a channel to type into: a command
  // that went nowhere is a command the person thinks they sent.
  input.disabled = true;
  ask.append(sign, input);
  const hint = el("p", "hint", CONNECTING);
  box.append(out, ask, hint);
  document.body.append(box);

  // ---- what is on the screen ----------------------------------------------

  // line is the last line of the output, so a piece of text that arrives
  // without a line break at the end is continued instead of starting a line of
  // its own. The service writes bytes, not lines, and where one frame of the
  // wire ends says nothing about where a line ends.
  let line = null;

  function newline(cls) {
    line = el("div", cls, "");
    out.append(line);
    return line;
  }

  // write puts text on the screen, as text. It is the only way anything gets
  // there, and it is the reason this file holds no innerHTML: a service that
  // answers with markup has written characters, not elements.
  function write(s, cls) {
    const parts = String(s).split("\n");
    for (let i = 0; i < parts.length; i++) {
      // Every break ends the line before it. The last piece has none after it,
      // so the next write carries on in that line - and if the text ended with
      // a break there is nothing left to start, so no empty line is made.
      if (i > 0) line = null;
      if (i === parts.length - 1 && parts[i] === "") break;
      if (line === null) newline(cls);
      // += and not textContent =, so a line that arrives in three pieces is
      // one line in the end.
      line.textContent += parts[i];
    }
    out.scrollTop = out.scrollHeight;
  }

  function say(text) {
    hint.textContent = text;
  }

  // fail is the one thing on this page that interrupts: a connection that was
  // cut while somebody was waiting for an answer has to reach them at once,
  // and it is built when there is something to say rather than kept empty -
  // an alert that stands there empty is announced by nobody.
  let failed = false;
  function fail(text) {
    if (failed) return;
    failed = true;
    const p = el("p", "fail", text);
    p.setAttribute("role", "alert");
    box.append(p);
  }

  // ---- the channel ---------------------------------------------------------

  // The size of the output in characters: how wide one character of the
  // monospace font is, and how high one line. Measured in the box that is
  // already on the screen, so what the service is told is what is there - and
  // measured with visibility:hidden, which keeps the probe out of the
  // accessibility tree: the box it goes into is a live region, and a reader
  // must not be told about ten noughts that nobody can see.
  function size() {
    const probe = el("span", null, "0".repeat(10));
    probe.style.cssText = "position:absolute;visibility:hidden;white-space:pre";
    out.append(probe);
    const w = probe.getBoundingClientRect().width / 10;
    probe.remove();
    const lineHeight = parseFloat(getComputedStyle(out).lineHeight);
    const area = out.getBoundingClientRect();
    const cols = w > 0 ? Math.floor(area.width / w) : 0;
    const rows = lineHeight > 0 ? Math.floor(area.height / lineHeight) : 0;
    return { cols: cols > 0 ? cols : COLS, rows: rows > 0 ? rows : ROWS };
  }

  // One channel for the whole window: a terminal is a conversation, and a
  // second one would be a second conversation in the same box. It is closed by
  // the shell when the window closes, so there is nothing to tidy up here.
  let stream = null;
  let sending = false;

  // ONE decoder for the whole channel, and it stands before the listener that
  // uses it: it is what carries the half character of one frame over into the
  // next, and a decoder per piece would turn those into question marks.
  const decoder = new TextDecoder();

  ngc.open("terminal", "shell", size()).then((s) => {
    stream = s;
    // The listeners stand before anything can arrive: the shell answers an
    // open it will not carry out with a close, and a close that nobody is
    // listening for is a window that waits for ever.
    s.onData = (data) => {
      let text = "";
      try {
        // stream: true, because a character of two or three bytes can be split
        // between two frames of the wire. A decoder of its own per piece would
        // turn those into question marks.
        text = decoder.decode(data, { stream: true });
      } catch {
        // Not bytes at all. That is the shell's business and not the person's;
        // what they see is that nothing arrived.
        return;
      }
      if (text !== "") write(text, null);
    };
    s.onClose = (status) => {
      stream = null;
      input.disabled = true;
      // THE RESULT COUNTS ONLY WITH "ok". Anything else is a conversation that
      // broke off, and what is on the screen may be half an answer.
      if (status === "ok") say(ENDED);
      else {
        say(AGAIN);
        fail(CUT);
      }
    };
    input.disabled = false;
    say(READY);
    input.focus();
  }, () => {
    // ngc.open does not reject today - it hands the stream over at once and
    // the shell's refusal arrives as a close. This is here for the day it
    // does, because a promise with no second hand is a window that stays on
    // "Verbindung wird aufgebaut …" for ever.
    say(AGAIN);
    fail(CUT);
  });

  // ---- what the person does ------------------------------------------------

  function send() {
    if (stream === null || sending) return;
    const cmd = input.value;
    // The empty line is sent too: a service that is waiting for one gets it,
    // and a person pressing the Enter key expects something to happen.
    input.value = "";
    // The line the person typed, on the screen in front of the answer. The
    // service does not echo - it is no terminal line discipline - so without
    // this the answers would stand there without their questions. A line of
    // its own before and after, so the mark that tells question from answer
    // belongs to this one line only.
    line = null;
    write("$ " + cmd, "cmd");
    line = null;
    // sending is set before the write and cleared after it, so a second Enter
    // while the first piece is still on its way cannot send out of order.
    sending = true;
    Promise.resolve(stream.write(enc.encode(cmd + "\n"))).then(() => {
      sending = false;
    }, () => {
      sending = false;
    });
  }

  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter") {
      e.preventDefault();
      send();
      return;
    }
    // Strg+D says "no more input". It is the convention of every terminal, and
    // the service needs it to know that nothing else is coming; closing the
    // window does the same for everybody who does not know it.
    if (e.key === "d" && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      if (stream === null) return;
      input.disabled = true;
      Promise.resolve(stream.end()).catch(() => {
        // The shell says what became of the channel in onClose; a throw here
        // would only be the same news twice.
      });
    }
  });

  const paint = (theme, accent) => {
    document.documentElement.dataset.theme = theme === "light" ? "light" : "dark";
    if (/^#[0-9a-fA-F]{3,8}$/.test(String(accent))) {
      document.documentElement.style.setProperty("--accent", accent);
    }
  };
  paint(ngc.theme, ngc.accent);
  ngc.onTheme(paint);
})();

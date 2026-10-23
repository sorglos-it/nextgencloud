// org.sorglos.terminal - the terminal, inside the frame every app gets.
//
// WHAT IS HERE AND WHAT IS NOT. This is the face of the app. Behind it stands
// the service "virtuelles Terminal" of the controller
// (apps/server-controller/classes/terminal/), which runs a shell on a pseudo
// terminal inside the app's own container. SINCE 12.10.2026 IT EXISTS, and
// this file is written against its format - the sentence that stood here, "it
// does not exist yet and nothing in this file would notice if it did", was
// true until then and is not any more.
//
// WHAT IS STILL NOT MEASURED ANYWHERE: that the real service answers at all,
// that its pseudo terminal really echoes, and how its output is split on the
// way here. All three need a Linux machine with the controller running; what
// the cases in tests/shell/terminal.spec.js hold is that the bytes this file
// sends are the bytes terminal.go reads.
//
// THE SIZE IN THE OPEN IS NOT WHAT THE SERVICE READS. ngc.open carries
// {cols, rows} because the plan of this task says so, and the service reads no
// parameters at all (terminal.go takes none, and the shell's environment is
// built from nothing). The size that counts travels as a frame; see tellSize.
//
// THE CHANNEL IS A STREAM, NOT A QUESTION. ngc.open stays open: what is typed
// goes out with write, Strg+D says "no more input" with end, and what the
// service writes arrives in onData, in as many pieces as the wire happened to
// make. It counts as finished only when the close says "ok"; anything else is a
// connection that was cut, and the person is told so rather than left with half
// an answer that looks whole.
//
// EVERY FRAME TOWARDS THE SERVICE CARRIES ITS OWN LENGTH: one byte kind, two
// bytes length big endian, then the payload. The reason is measured and belongs
// to the other side - serve.shellInput hands the inner messages on as one byte
// STREAM, so the boundary of a message is gone by the time the service reads
// it, and a keys frame without a length has no end. A parser that had to guess
// one could be pushed out of step by whoever sends the bytes, and from then on
// every window size would be read out of the middle of a paste. The numbers
// below are the contract's (see FRAMES).
//
// THE WAY BACK CARRIES NO FRAMES. The output of the shell travels raw, because
// this side has nothing to tell apart (terminal.EncodeOutput says so itself).
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
  // THE REASON IS SHOWN BESIDE IT WHERE THE SHELL SENDS ONE, and since
  // 23.10.2026 it does (shell/appframe.js, whyOpenFailed). Why that line
  // exists at all is worth having written down here, where the sentence is
  // read: measured on the Debian machine that day, this app said CUT and
  // nothing else while the controller's journal, the end-to-end channel and
  // the app frame had each thrown the reason away in turn - and the browser
  // console held nothing, because nothing had gone wrong uncaught. One
  // sentence of ours that says what to do beats three stations that say
  // nothing.
  //
  // WHAT MAY STAND THERE is what the shell sends, and the shell sends its own
  // text: the German sentence of a code of the contract, or one of its own
  // ("Bitte neu anmelden", "Nicht bestätigt"). It lands on textContent like
  // every other byte this app shows (see write), and it is bounded, so a
  // sentence that ran away cannot push the page apart.
  const MAX_SAID = 200;
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

  // FRAMES. Read off the service's own file,
  // apps/server-controller/classes/terminal/terminal.go: kind, length of the
  // payload as uint16 big endian, payload. A keys frame holds 1..MAX_KEYS
  // bytes - zero is no frame and the service ends the run over one - and a
  // size frame holds exactly four bytes, cols and rows as uint16, each
  // 1..MAX_DIM.
  const KIND_KEYS = 1;
  const KIND_RESIZE = 2;
  const MAX_KEYS = 16 << 10;
  const MAX_DIM = 2000;

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

  // One line of the output. It carries no class any more: the only thing that
  // ever had one was the line this app wrote itself, and it does not write one
  // (see send).
  function newline() {
    line = el("div", null, "");
    out.append(line);
    return line;
  }

  // write puts text on the screen, as text. It is the only way anything gets
  // there, and it is the reason this file holds no innerHTML: a service that
  // answers with markup has written characters, not elements.
  function write(s) {
    const parts = String(s).split("\n");
    for (let i = 0; i < parts.length; i++) {
      // Every break ends the line before it. The last piece has none after it,
      // so the next write carries on in that line - and if the text ended with
      // a break there is nothing left to start, so no empty line is made.
      if (i > 0) line = null;
      if (i === parts.length - 1 && parts[i] === "") break;
      if (line === null) newline();
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
    return { cols: dim(cols, COLS), rows: dim(rows, ROWS) };
  }

  // dim brings one measured number into the range a size frame can carry. A
  // box with no size yet gives nothing to work with, and then the size of a
  // terminal nobody resized is better than none; above MAX_DIM the service
  // ENDS THE RUN over the frame, so a window somebody made that wide is told
  // as wide as may be said instead of killing the terminal.
  function dim(n, fallback) {
    if (!(n > 0)) return fallback;
    return n > MAX_DIM ? MAX_DIM : n;
  }

  // frame wraps one payload: kind, length as uint16 big endian, payload. The
  // length is what makes the stream readable at all (see the head of this
  // file).
  function frame(kind, payload) {
    const f = new Uint8Array(3 + payload.length);
    f[0] = kind;
    f[1] = (payload.length >> 8) & 0xff;
    f[2] = payload.length & 0xff;
    f.set(payload, 3);
    return f;
  }

  // sizeFrame is the four bytes of a size: cols, then rows, each uint16 big
  // endian. Both have been through dim, so neither is zero and neither is over
  // MAX_DIM - the two things the service refuses a size frame for.
  function sizeFrame(s) {
    return frame(KIND_RESIZE, new Uint8Array([s.cols >> 8, s.cols & 0xff, s.rows >> 8, s.rows & 0xff]));
  }

  // One channel for the whole window: a terminal is a conversation, and a
  // second one would be a second conversation in the same box. It is closed by
  // the shell when the window closes, so there is nothing to tidy up here.
  let stream = null;
  // The writes of this channel, one after another. A frame that overtook
  // another would put a size into the middle of a paste - the very thing the
  // length in the header is there to prevent - so everything goes out through
  // this one chain, in the order it was made.
  let tail = Promise.resolve();
  // The size the service was last told. A resize that changes no character
  // count sends nothing: the same two numbers twice are of no use to it.
  let told = null;

  function push(bytes) {
    if (stream === null) return;
    tail = tail.then(() => (stream === null ? undefined : stream.write(bytes))).catch(() => {
      // What became of the channel arrives in onClose; a throw here would be
      // the same news a second time.
    });
  }

  // tellSize measures the box and sends a size frame if it has changed. It is
  // the ONLY way the service learns how wide the window is: terminal.go reads
  // no parameters, and its pseudo terminal starts at whatever the kernel gives.
  function tellSize() {
    const now = size();
    if (told !== null && told.cols === now.cols && told.rows === now.rows) return;
    told = now;
    push(sizeFrame(now));
  }

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
      if (text !== "") write(text);
    };
    s.onClose = (status, said) => {
      stream = null;
      input.disabled = true;
      // THE RESULT COUNTS ONLY WITH "ok". Anything else is a conversation that
      // broke off, and what is on the screen may be half an answer.
      if (status === "ok") say(ENDED);
      else {
        say(AGAIN);
        fail(reason(said));
      }
    };
    // The size before anything else: a shell that laid its first line out for
    // eighty columns into a window of two hundred has already written the
    // wrong thing.
    tellSize();
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

  // reason is CUT, with the sentence of the shell behind it where there is
  // one. Two sentences and not one joined text: the first says what happened,
  // the second what to do about it.
  function reason(said) {
    if (typeof said !== "string") return CUT;
    const t = said.trim().slice(0, MAX_SAID);
    // The shell's sentence for a cut-off channel is sometimes the same
    // sentence this app would show on its own; saying it twice is one time too
    // many.
    if (t === "" || t === CUT) return CUT;
    return CUT + " " + t;
  }

  // ---- what the person does ------------------------------------------------

  function send() {
    if (stream === null) return;
    const cmd = input.value;
    // The empty line is sent too: a service that is waiting for one gets it,
    // and a person pressing the Enter key expects something to happen.
    input.value = "";
    // NOTHING IS WRITTEN ON THE SCREEN HERE, and that is a change with a
    // reason. The service runs /bin/sh -i on a pseudo terminal, and a pty
    // echoes what is written into it - so the line comes back by itself, and an
    // echo here would put every line on the screen twice. The half that
    // matters more: a program that asks for a password switches that echo OFF,
    // and an app that wrote the line itself would put the password on the
    // screen regardless.
    const bytes = enc.encode(cmd + "\n");
    // Cut into frames the service accepts. The line break is always there, so
    // there is always at least one byte and never an empty frame - and an
    // empty keys frame is no frame: ParseFrame refuses it and the run ends.
    for (let at = 0; at < bytes.length; at += MAX_KEYS) {
      push(frame(KIND_KEYS, bytes.subarray(at, Math.min(at + MAX_KEYS, bytes.length))));
    }
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

  // A window that changed size tells the service, once the changing has
  // stopped: a drag sends a resize event per animation frame, and the service
  // has no use for forty sizes on the way to one.
  let settle = 0;
  window.addEventListener("resize", () => {
    clearTimeout(settle);
    settle = setTimeout(tellSize, 80);
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

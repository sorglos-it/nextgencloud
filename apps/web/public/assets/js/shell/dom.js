// Building blocks for the shell's own markup.
//
// PULLED FORWARD. The plan creates this file in Task 18; the desktop (Task 21)
// is being built first so there is something to click, and it needs these
// helpers. Nothing was added for the desktop's sake: el, text and clear are
// what Task 18 lists, so that task finds the file ready instead of writing it
// a second time.
//
// Why a helper at all, when document.createElement is three words: because the
// one line it replaces is the line where innerHTML gets written. A name, a
// title, a message - every one of them comes from the API, and every one of
// them goes in as a text node here. There is no argument of any function in
// this file that becomes markup, so there is no way to pass markup in.

// el makes an element, gives it a class and fills it with ONE text node.
//
// The third argument is text, never markup: it lands on textContent, so "<b>"
// shows up as the four characters "<b>" and an <img onerror=…> from a
// compromised API is four dozen characters on screen and nothing else.
export function el(tag, cls, txt) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  // An empty text still means "this element holds text": only undefined and
  // null mean "leave it alone". Otherwise a name that is legitimately "" would
  // leave whatever the element had before.
  if (txt !== undefined && txt !== null) e.textContent = String(txt);
  return e;
}

// button makes a button with a label a screen reader can read. type is set
// because a button inside a form defaults to submit, and a shell that submits
// a form navigates away from itself.
export function button(cls, txt, label) {
  const b = el("button", cls, txt);
  b.type = "button";
  if (label) b.setAttribute("aria-label", label);
  return b;
}

// text replaces what an element holds with one text node. Reaching for
// textContent by hand is where innerHTML gets typed by mistake; this is the
// same line with no second way to spell it.
export function text(e, txt) {
  e.textContent = txt === undefined || txt === null ? "" : String(txt);
}

// clear empties an element. replaceChildren over innerHTML = "": the one is a
// list of nodes, the other a string the browser has to parse.
export function clear(e) {
  e.replaceChildren();
}

// field makes a labelled input: the label WRAPS the input, so the two belong
// together without an id anybody has to keep unique. A shell is one page that
// never reloads, and ids handed out by hand are the thing that collides on
// it - two masks, two "pw", and a screen reader reads the wrong caption.
//
// It hands back the input and the label, because a caller needs both: the
// value comes out of the one and the row is put on the page with the other.
//
// THE TASK BRIEF WRITES field(label, type) AND button(text, onClick). The
// second one cannot be had: button(cls, txt, label) is already here since
// task 21 and the desktop, the app frame and the confirmation dialog all
// call it that way. A second meaning for one name is the drift this file
// warns about at the top, so the name keeps the meaning it has and a caller
// hangs its own listener on what comes back - which is one line and reads
// the same.
export function field(label, type) {
  const box = el("label", "field");
  box.append(el("span", "fieldname", label));
  const input = el("input", null);
  // The type is set before the element is on the page: an input that is
  // briefly a text field is an input that briefly shows a password.
  input.type = type;
  box.append(input);
  return { box, input };
}

// note makes one line of feedback. level decides how loudly it is announced,
// and that is not decoration: a message about a failed login that no screen
// reader reads out is a message a blind person never gets.
//
//   "error" and "warn" - role="alert", read out at once,
//   anything else      - role="status", read out when there is a pause.
export function note(txt, level) {
  const p = el("p", "note " + (level === "error" || level === "warn" ? level : "info"), txt);
  p.setAttribute("role", level === "error" || level === "warn" ? "alert" : "status");
  return p;
}

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

// The calculator, the first app with a face of its own.
//
// It computes with the shunting-yard method of docs/prototype.html. There is
// no eval and there must never be one: the content security policy of the
// page forbids it anyway, and a calculator that evaluates a string is a
// calculator that runs whatever was typed into it.
//
// TO BE MOVED WITH TASK 22. This mounts straight into win.body because
// ngc.js and the sandboxed frame do not exist yet. It belongs in that frame:
// an app that ships with NGC must not behave differently from an app out of
// the store. As long as it sits here it runs in the shell's own origin, with
// the whole DOM and the session in reach - exactly what the frame exists to
// take away - and the frame itself is tested by nothing.
import { el, button, text } from "../shell/dom.js";

// Two digits are dropped from the end before the result is shown: 0.1 + 0.2
// is 0.30000000000000004 in binary floating point, and that is not an answer
// anybody wants from a pocket calculator.
const ROUND = 1e10;

const PRECEDENCE = { __proto__: null, "+": 1, "-": 1, "*": 2, "/": 2 };

// The keys, row by row, as in the prototype: label, class, and the label a
// screen reader gets where the character on the key is no word.
const KEYS = [
  ["C", "op"], ["±", "op", "Vorzeichen"], ["%", "op"], ["÷", "op"],
  ["7", ""], ["8", ""], ["9", ""], ["×", "op"],
  ["4", ""], ["5", ""], ["6", ""], ["−", "op"],
  ["1", ""], ["2", ""], ["3", ""], ["+", "op"],
  ["0", ""], [",", "", "Komma"], ["⌫", "op", "Zurück"], ["=", "eq"],
];

// What the keys with a sign on them mean in the expression.
const SIGNS = { __proto__: null, "÷": "/", "×": "*", "−": "-" };

// compute reads the expression and works it out: numbers to the output,
// operators over a stack, point before line. It never sees anything but the
// characters the keys put there, and it checks that once more before it
// starts - the display is a string, and a string is something to be sure of.
function compute(s) {
  if (!/^[0-9+\-*/.]+$/.test(s)) return null;
  const toks = s.match(/(\d+\.?\d*|[+\-*/])/g) || [];
  const out = [], ops = [];
  for (const t of toks) {
    if (/^\d/.test(t)) out.push(parseFloat(t));
    else {
      while (ops.length && PRECEDENCE[ops[ops.length - 1]] >= PRECEDENCE[t]) out.push(ops.pop());
      ops.push(t);
    }
  }
  while (ops.length) out.push(ops.pop());
  const st = [];
  for (const t of out) {
    if (typeof t === "number") st.push(t);
    else {
      const b = st.pop(), a = st.pop();
      if (a === undefined || b === undefined) return null;
      st.push(t === "+" ? a + b : t === "-" ? a - b : t === "*" ? a * b : a / b);
    }
  }
  const r = st.pop();
  // A division by zero gives Infinity and 0/0 gives NaN. Neither is a number
  // to show somebody; both are the same mistake to them.
  if (r === undefined || !isFinite(r)) return null;
  return String(Math.round(r * ROUND) / ROUND);
}

// calcApp builds the calculator into the window's body and hands back the
// function that tidies up when the window closes.
export function calcApp(win) {
  const box = el("div", "calc");
  const disp = el("div", "disp");
  const eE = el("div", "e", "");
  const vE = el("div", "v", "0");
  // The display is the app's answer, and it changes without anyone touching
  // it: a screen reader has to be told. aria-live alone, not role="status":
  // the desktop already has one status region for its notices, and two would
  // be two places a reader is sent to for two different things.
  vE.setAttribute("aria-live", "polite");
  disp.append(eE, vE);
  const keys = el("div", "keys");
  box.append(disp, keys);

  let expr = "", val = "0";

  const show = () => {
    text(eE, expr);
    text(vE, val);
  };

  const push = (c) => {
    if ("0123456789".includes(c)) val = (val === "0" ? "" : val) + c;
    else if (c === ",") { if (!val.includes(".")) val += "."; }
    else if ("+-*/".includes(c)) { expr += val + " " + c + " "; val = "0"; }
    show();
  };

  const equals = () => {
    const r = compute((expr + val).replace(/\s+/g, ""));
    expr = "";
    val = r === null ? "Fehler" : r;
    show();
  };

  const back = () => {
    val = val.length > 1 ? val.slice(0, -1) : "0";
    show();
  };

  const clearAll = () => {
    expr = "";
    val = "0";
    show();
  };

  for (const [label, cls, aria] of KEYS) {
    const b = button(cls, label, aria);
    b.addEventListener("click", () => {
      if (label === "C") clearAll();
      else if (label === "⌫") back();
      else if (label === "±") { val = val.startsWith("-") ? val.slice(1) : "-" + val; show(); }
      else if (label === "%") { val = String(parseFloat(val) / 100); show(); }
      else if (label === "=") equals();
      else push(SIGNS[label] || label);
    });
    keys.append(b);
  }
  show();

  // The keyboard belongs to whichever window has the focus; a calculator in
  // the background must not eat the digits somebody is typing elsewhere.
  const onKey = (e) => {
    if (win.desktop.focused !== win) return;
    // Somebody typing into a field is typing into that field. Without this the
    // calculator would swallow the digits going into the start menu's search
    // box, because the window behind it still holds the focus of the shell.
    if (e.target instanceof Element && e.target.closest("input, textarea, [contenteditable]")) return;
    const k = e.key;
    if (/^[0-9]$/.test(k)) push(k);
    else if ("+-*/".includes(k)) push(k);
    else if (k === "," || k === ".") push(",");
    else if (k === "Enter" || k === "=") equals();
    else if (k === "Backspace") back();
    else if (k === "Escape") clearAll();
    else return;
    e.preventDefault();
  };
  window.addEventListener("keydown", onKey);

  win.body.append(box);
  return () => window.removeEventListener("keydown", onKey);
}

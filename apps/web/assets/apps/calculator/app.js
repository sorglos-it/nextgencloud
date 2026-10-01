// org.sorglos.calculator - the calculator, inside the frame every app gets.
//
// IT MOVED HERE IN TASK 25, and this is what the move is about: until then the
// calculator was built straight into the window's body by the shell, with the
// whole DOM of the shell, the session and localStorage in reach. Now it is a
// page of its own in a frame with an origin of its own, no storage and no
// connection - the same walls an app out of the store runs behind. The app id
// is the one shared/testdata uses; it is no NGC app ("ngc."), because
// core.AppParams refuses a core app for ngc.desktop/ui and /ui-proof alike, so
// an app of NGC's own could never be asked for a user interface at all.
//
// A CLASSIC SCRIPT, NOT A MODULE - but not old JavaScript either. The frame's
// page carries every file as a data: address, and a module cannot import
// anything out of one: the browser has no base to resolve against and refuses
// every specifier, so a module would run halfway and stop. shell/appframe.js
// refuses one outright rather than let that happen.
//
// There is no eval and there must never be one: the frame's policy forbids it,
// and a calculator that evaluates a string is a calculator that runs whatever
// was typed into it. It computes with the shunting-yard method of
// docs/prototype.html instead.
(() => {
  "use strict";

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

  // el and text, the two lines shell/dom.js is for, once more here: a module of
  // the shell cannot be imported into this document, and these are the lines
  // where innerHTML would otherwise get typed. No argument of either becomes
  // markup, so there is no way to pass markup in.
  function el(tag, cls, txt) {
    const e = document.createElement(tag);
    if (cls) e.className = cls;
    if (txt !== undefined && txt !== null) e.textContent = String(txt);
    return e;
  }

  function text(e, txt) {
    e.textContent = txt === undefined || txt === null ? "" : String(txt);
  }

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

  const box = el("div", "calc");
  const disp = el("div", "disp");
  const eE = el("div", "e", "");
  const vE = el("div", "v", "0");
  // The display is the app's answer, and it changes without anyone touching
  // it: a screen reader has to be told. aria-live alone, not role="status" -
  // one region in this document, one place a reader is sent to.
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
    const b = el("button", cls, label);
    // A button inside a form defaults to submit, and submitting is navigating
    // away. There is no form on this page, and the type is set anyway, for the
    // same reason shell/dom.js sets it: the default is the dangerous one.
    b.type = "button";
    if (aria) b.setAttribute("aria-label", aria);
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

  // THE KEYBOARD, AND WHAT THE MOVE CHANGED ABOUT IT. The calculator used to
  // listen on the shell's own window, and it had to ask the desktop two things
  // before it could act: whether its window had the focus, and whether
  // somebody was typing into a field of the shell - the start menu's search,
  // which the window behind it does not lose the focus to.
  //
  // Both guards are gone and nothing was given up: this document gets a key
  // event only while the browser has given this frame the focus. A window in
  // the background and an open start menu are both "the focus is somewhere
  // else", and the engine decides that - not a check of ours that can be
  // wrong. A comma and a point both give a decimal point, because a German
  // keyboard has the comma on the letters and the point on the number block.
  document.addEventListener("keydown", (e) => {
    const k = e.key;
    if (/^[0-9]$/.test(k)) push(k);
    else if (k.length === 1 && "+-*/".includes(k)) push(k);
    else if (k === "," || k === ".") push(",");
    else if (k === "Enter" || k === "=") equals();
    else if (k === "Backspace") back();
    else if (k === "Escape") clearAll();
    else return;
    e.preventDefault();
  });

  // What the shell looks like. The theme is already on <html> before the first
  // paint, so this page is never painted in the wrong colours and repainted;
  // the accent is a colour, and no stylesheet can read a colour out of an
  // attribute, so it goes in as a custom property. Both are followed
  // afterwards, because the person can change them while the window is open.
  const paint = (theme, accent) => {
    document.documentElement.dataset.theme = theme === "light" ? "light" : "dark";
    // Only a colour that really looks like one. It is written into a style
    // property, and a value nobody checked would be a value out of a message.
    if (/^#[0-9a-fA-F]{3,8}$/.test(String(accent))) {
      document.documentElement.style.setProperty("--accent", accent);
    }
  };
  paint(ngc.theme, ngc.accent);
  ngc.onTheme(paint);

  document.body.append(box);
})();

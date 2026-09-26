// THE ONE PLACE IN NGC THAT TURNS TEXT INTO A DOCUMENT.
//
// Everywhere else the rule is absolute: no innerHTML, no insertAdjacentHTML,
// no document.write, no eval, no new Function. Here document.write is used
// once, on purpose, and this comment is what it has to earn.
//
// WHY THIS IS THE EXCEPTION. An app's user interface is HTML - that is what
// an app IS - and a browser has exactly one way to turn HTML into a running
// document. The question is therefore never "markup or no markup" but "in
// whose origin, and out of which bytes".
//
// WHAT HAS TO BE TRUE BEFORE THIS FILE MAY WRITE ANYTHING:
//
//  1. The bytes are proven. shell/appframe.js has fetched the app's ui-tar,
//     hashed it and compared that hash with the one out of the signed UI
//     proof - the store's catalog signature, or the admin's activation - and
//     has read nothing out of the archive before the hashes matched. What is
//     written here is built from those bytes and from nothing else.
//  2. This document is in an origin of its own. /frame.html is served with
//     "Content-Security-Policy: sandbox allow-scripts", so the browser gives
//     it an opaque origin NO MATTER HOW IT IS LOADED - in NGC's frame, in
//     somebody else's frame, or opened on its own. The check below refuses to
//     write anything if that is not so, which is the line that keeps this
//     file from ever becoming a way to run markup in NGC's own origin.
//  3. The document may do nothing but show itself. Its policy is
//     default-src 'none' with data: for scripts, styles, pictures, fonts and
//     media, and connect-src 'none': the app makes no connection of its own,
//     and every byte it shows came out of the archive that was proven in 1.
//  4. The text comes from the window that framed us and from nowhere else,
//     and it is written exactly once.
//
// WHAT IS NOT CHECKED HERE, and why that is right: whether the HTML is
// well-made, what it draws, what it asks for. That is the app's business.
// The frame does not judge an app; it fences it.
(function () {
  "use strict";

  // An opaque origin answers "null" here. A document of NGC's own origin
  // answers with that origin, and then this file does nothing at all: without
  // the sandbox, writing text from a message would be a way to run somebody
  // else's markup where the session keys live. It fails shut, so a future
  // frame that forgets the sandbox shows an empty page instead of a hole.
  if (String(window.origin) !== "null") return;

  var written = false;

  window.addEventListener("message", function (e) {
    // Only the window that framed us. With no allow-same-origin nobody else
    // holds a handle to this one, and the check costs a line.
    if (e.source !== parent) return;
    var m = e.data;
    if (!m || m.ngc !== 1 || m.op !== "doc" || typeof m.html !== "string") return;
    // Once. A second document would be an app replaced under a window the
    // person already trusts.
    if (written) return;
    written = true;
    document.open();
    document.write(m.html);
    document.close();
  });

  // The frame says it is there. The shell waits for this rather than for the
  // load event, because the listener above has to stand before the document
  // arrives.
  parent.postMessage({ ngc: 1, op: "ready" }, "*");
})();

/*!
 * ngc.js - the library an NGC app talks to the shell with.
 * Copyright (c) 2026 Sorglos Thomas Weirich. MIT licence.
 *
 * An app reaches nothing but its own services, and only through here.
 *
 * It lies beside the shell's own scripts rather than under js/shell/ because
 * it is the one file an app developer downloads and ships: a stable public
 * address, and the only part of the web interface that is not AGPL.
 *
 * The shell does not load this file into the app's frame over the network -
 * the frame may make no connection at all. It reads these very bytes and
 * writes them into the app's document as the first script, with the frame's
 * word in data-token. So what runs in the app is what stands here.
 */
(function () {
  "use strict";

  // The word the shell drew for this frame. It is not a secret from the app -
  // it stands in the app's own document - and it is not what tells the shell
  // who is calling: that is the window the message came from. It tells apart
  // the frame the shell is talking to now from one it has already given up,
  // so a message that survived a reload is not answered as if it were new.
  var script = document.currentScript;
  var token = (script && script.dataset && script.dataset.token) || "";

  var nextID = 1;
  var waiting = new Map();
  var streams = new Map();
  var themeHandlers = [];

  // Everything that arrives is data. It comes from the shell - in a frame
  // with no allow-same-origin nobody else holds a handle to this window - and
  // e.source is checked all the same, because a library that trusts its
  // caller's word for who the caller is has no check at all.
  window.addEventListener("message", function (e) {
    if (e.source !== parent) return;
    var m = e.data;
    if (!m || m.ngc !== 1) return;
    if (m.op === "theme") {
      ngc.theme = m.theme;
      ngc.accent = m.accent;
      themeHandlers.forEach(function (f) {
        try {
          f(m.theme, m.accent);
        } catch (err) {
          // One handler that throws must not stop the others: the app's look
          // is not worth its whole window.
        }
      });
      return;
    }
    var w = waiting.get(m.id);
    var s = streams.get(m.id);
    if (m.op === "result" && w) {
      waiting.delete(m.id);
      if (m.error) w.reject(new Error(m.error));
      else w.resolve(m.value);
    }
    if (m.op === "data" && s && s.onData) s.onData(m.data);
    if (m.op === "closed" && s) {
      streams.delete(m.id);
      if (s.onClose) s.onClose(m.status, m.message);
    }
  });

  function send(op, extra) {
    var id = nextID++;
    var msg = { ngc: 1, token: token, id: id, op: op };
    for (var k in extra) if (Object.prototype.hasOwnProperty.call(extra, k)) msg[k] = extra[k];
    parent.postMessage(msg, "*");
    return id;
  }

  var ngc = {
    version: 1,
    // What the shell looks like right now. The shell writes it into the
    // document before the app's own scripts run, so the first paint is
    // already in the right colours.
    theme: document.documentElement.dataset.theme || "dark",
    accent: document.documentElement.dataset.accent || "#2f7de1",
    onTheme: function (fn) {
      if (typeof fn === "function") themeHandlers.push(fn);
    },
    // call asks one of this app's own services for an answer. The answer
    // comes back as the bytes the service sent: what they mean is the app's
    // own contract with its own service, and neither the shell nor this
    // library reads them.
    call: function (service, action, params) {
      return new Promise(function (resolve, reject) {
        waiting.set(send("call", { service: service, action: action, params: params }),
          { resolve: resolve, reject: reject });
      });
    },
    // open asks for a channel that stays open: write and end send, onData and
    // onClose are called as answers arrive.
    open: function (service, action, params) {
      var id = send("open", { service: service, action: action, params: params });
      var s = {
        write: function (data) {
          parent.postMessage({ ngc: 1, token: token, id: id, op: "send", data: data }, "*");
        },
        end: function () {
          parent.postMessage({ ngc: 1, token: token, id: id, op: "end" }, "*");
        },
        close: function () {
          parent.postMessage({ ngc: 1, token: token, id: id, op: "close" }, "*");
        },
        onData: null,
        onClose: null,
      };
      streams.set(id, s);
      return Promise.resolve(s);
    },
  };
  window.ngc = ngc;
})();

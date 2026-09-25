// A STAND-IN. THIS FILE IS A FAKE AND IS MEANT TO BE DELETED.
//
// It answers with fixed bytes so the desktop has apps to show before anything
// can be asked of a server. It talks to no API, it verifies nothing, and it
// must never carry a single line that the real thing would need.
//
// WHEN IT GOES: with Task 17, which builds shell/channel.js - the session to
// the API, the Noise tunnel and the channels. Then main.js hands startDesktop
// a channel instead of this, DELETES this file, and the desktop is not touched
// at all: that is what the shape below is for.
//
// WHAT THE REAL ONE HAS TO DO, and this one does not:
//   - call() must open a channel, send the OPEN, collect the result up to
//     wire.MAX_CALL_RESULT bytes and only hand it back when the channel closed
//     with "ok"; anything else is thrown away.
//   - notices must come from the "notify" control message of that session.
//   - it must live at shell/channel.js, and this file must be gone.
//
// WHAT STAYS TRUE EITHER WAY: it hands back BYTES. Everything that arrives is
// read by the contracts in contracts/ - core.parseResult, core.parseNotice -
// inside the desktop, never here. So the desktop is already doing the whole
// check today; only where the bytes come from changes.
//
// It is not in contracts/ on purpose: nothing in there is a fake.

const enc = new TextEncoder();

// The apps the stand-in reports for "ngc.desktop/apps". Every field of
// core.DesktopApp stands here, because these bytes go through
// core.parseResult like the real answer and a missing field is refused.
//
// "icon" and "entry" are paths inside the app's verified ui-tar. There is no
// proof to check them against yet (Task 22), so the desktop shows neither -
// see the comment on the placeholder in desktop.js.
const APPS = [
  {
    id: "ngc.store", name: "App-Store", version: "1.0.0", icon: "", entry: "",
    width: 760, height: 520, actions: [], state: "ok", core: true, node: "",
  },
  {
    id: "ngc.users", name: "Benutzer und Rechte", version: "1.0.0", icon: "", entry: "",
    width: 760, height: 520, actions: [], state: "ok", core: true, node: "",
  },
  {
    id: "ngc.calc", name: "Rechner", version: "1.0.0", icon: "", entry: "",
    width: 300, height: 420, actions: [], state: "ok", core: false, node: "",
  },
  {
    id: "ngc.terminal", name: "Terminal", version: "1.0.0", icon: "", entry: "",
    width: 600, height: 360, actions: ["shell"], state: "ok", core: false, node: "pi-1",
  },
  // An app that is installed but whose controller does not answer: the desktop
  // says so and opens no window.
  {
    id: "de.example.fotos", name: "Fotos", version: "2.1.0", icon: "", entry: "",
    width: 640, height: 480, actions: [], state: "unreachable", core: false, node: "pi-2",
  },
  // An app whose licence has run out.
  {
    id: "de.example.buchhaltung", name: "Buchhaltung", version: "3.0.1", icon: "", entry: "",
    width: 640, height: 480, actions: [], state: "unlicensed", core: false, node: "pi-1",
  },
  // A name that tries to be markup. It is a legal name by the contract - the
  // characters are all printable - so nothing refuses it, and the desktop has
  // to put it on screen as the text it is.
  {
    id: "de.example.boese", name: "<img src=x onerror=alert(1)>Böse", version: "0.1.0",
    icon: "", entry: "", width: 400, height: 300, actions: [], state: "ok", core: false, node: "",
  },
  // "Geheim" is deliberately absent: the API only lists what this user may
  // use, and leaving it out is how that is done.
];

// One entry, because the desktop asks for one thing. Everything else the
// shell will need comes with the real channel, and a mock answer nobody reads
// is a mock answer nobody keeps right.
const RESULTS = {
  __proto__: null,
  "ngc.desktop/apps": () => APPS,
};

// fakeLink builds the stand-in. The shape is the one shell/channel.js will
// have: call() hands back bytes, onNotice() takes a listener that is handed
// bytes.
export function fakeLink() {
  const notices = new Set();

  const link = {
    async call(app, service, name) {
      const make = RESULTS[`${app}/${name}`];
      if (make === undefined) throw new Error(`fake-link: nothing for ${app}/${service}/${name}`);
      return enc.encode(JSON.stringify(make()));
    },
    onNotice(fn) {
      if (typeof fn === "function") notices.add(fn);
    },
  };

  // A TEST HOOK, and it leaves with this file. The brief's test sends a notice
  // with it; the real session gets them over the wire and needs no hook. It is
  // bytes, so the desktop reads them with core.parseNotice exactly as it will
  // read the real ones.
  window.__ngcSendNotice = (level, text) => {
    const body = enc.encode(JSON.stringify({ level, text, app: "" }));
    for (const fn of notices) fn(body);
  };

  return link;
}

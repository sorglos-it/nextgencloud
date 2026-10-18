import { test, expect } from "../fixtures/page.js";

// What net/channel.js HANDS A LINK, measured against contracts/core.js's own
// table - not against a text typed in this file.
//
// Nothing here needs the Web Crypto API: the link is a recorder with five
// methods, and nothing is signed or verified. So no case below skips on WebKit
// (no Ed25519) or on Firefox (no public half of a password seed), and that is
// why they stand here and not in shell/channels.spec.js, whose every case needs
// a Noise session and therefore skips on both engines. Measured on 18.10.2026:
// of 416 cases per engine Firefox skipped 179 - the whole shell layer - so a
// case that only runs where a desktop comes up is a case that runs on one
// engine of three.
//
// WHY THIS FILE EXISTS. Measured on 18.10.2026 against `main`: callBytes
// forwarded confirmBytes to Link.open, and sendInBytes did not take the name at
// all - so a confirmation handed to the streaming helper was dropped in
// silence. The row that pays for that is the UPLOAD: core.js lists it as FlowIn
// and core.critical() answers true for it, so the one action of the store that
// MUST carry a confirmation was the one whose helper could not carry it. The API
// answers such an OPEN with E_CONFIRM, and nothing on the way says which side
// forgot the paper.
//
// THE RECORDER KEEPS THE OPEN AS THE OBJECT IT WAS GIVEN, because that object
// is the question: net/link.js destructures confirmBytes BY NAME, so a helper
// that never writes the name hands over undefined and the frame leaves without
// the field. It stands inside each page.evaluate and not in a helper of this
// file, because what crosses into the page is JSON and a function is not.

test("a confirmation handed to the streaming helper reaches the OPEN", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const channel = await import("/assets/js/net/channel.js");
    const core = await import("/assets/js/contracts/core.js");
    const opens = [];
    const link = {
      open: async (o) => {
        opens.push(o);
        return {
          firstGrant: async () => {}, write: async () => {}, end: async () => {},
          close: async () => {}, result: async () => new Uint8Array(0),
        };
      },
    };
    const action = core.lookup(core.APP_STORE, core.SERVICE_API, core.ACT_UPLOAD);
    // The flow is taken from a SECOND row of the same table that is FlowIn
    // (ngc.users/api/trust), so the case carries no copy of the word "in" -
    // core.js keeps FLOW_IN to itself on purpose.
    const alsoIn = core.lookup(core.APP_USERS, core.SERVICE_API, core.ACT_TRUST);
    const task = new TextEncoder().encode("{\"task\":\"upload\"}");
    const ok = new TextEncoder().encode("{\"confirm\":\"anna\"}");
    const stream = new Blob([new Uint8Array([7, 7, 7])]).stream();
    await channel.sendInBytes(link, action, { taskBytes: task, confirmBytes: ok, stream });
    const o = opens[0];
    const read = (b) => (b === undefined || b === null ? null : new TextDecoder().decode(b));
    return {
      opens: opens.length,
      // The intermediate steps, so a green case cannot come from an earlier
      // wall: the OPEN really is the upload's, and the task really went through
      // this same helper.
      app: o?.app ?? null,
      action: o?.action ?? null,
      task: read(o?.taskBytes),
      confirm: read(o?.confirmBytes),
      // And what core.js itself says about this row, so the case is pinned to
      // the contract: it is the streaming flow, and it is critical.
      sameFlowAsTrust: action.flow === alsoIn.flow,
      critical: core.critical(action, {}),
    };
  });
  expect(got).toEqual({
    opens: 1,
    app: "ngc.store",
    action: "upload",
    task: "{\"task\":\"upload\"}",
    confirm: "{\"confirm\":\"anna\"}",
    sameFlowAsTrust: true,
    critical: true,
  });
});

// The sister half, so the case above cannot be read as "channel.js forwards
// everything anyway": callBytes has carried the confirmation since it was
// written, and a change that took it out THERE would otherwise go unmeasured.
test("a confirmation handed to the call helper reaches the OPEN", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const channel = await import("/assets/js/net/channel.js");
    const core = await import("/assets/js/contracts/core.js");
    const opens = [];
    const link = {
      open: async (o) => {
        opens.push(o);
        return {
          firstGrant: async () => {}, write: async () => {}, end: async () => {},
          close: async () => {}, result: async () => new Uint8Array(0),
        };
      },
    };
    const action = core.lookup(core.APP_STORE, core.SERVICE_CONTROLLER, core.ACT_UNINSTALL);
    const ok = new TextEncoder().encode("{\"confirm\":\"anna\"}");
    await channel.callBytes(link, action, { confirmBytes: ok });
    const o = opens[0];
    return {
      action: o?.action ?? null,
      confirm: o?.confirmBytes === undefined || o?.confirmBytes === null
        ? null : new TextDecoder().decode(o.confirmBytes),
    };
  });
  expect(got).toEqual({ action: "uninstall", confirm: "{\"confirm\":\"anna\"}" });
});

// The one place the shell says "this state is not what it was".
//
// WHY THIS IS NOT A PUSH FROM THE SERVER. The session link carries exactly two
// messages from the API to the shell (wire.controlAllowed, LINK_SESSION):
// "trust" - a newer version of the trust list exists - and "notify" - a line of
// German text for the person. There is no message that says "the apps of this
// user changed" or "a node joined", and inventing one is a change to the
// contract in shared/wire, which is frozen. So what the channel carries is
// used where it reaches (shell/trustlist.js hangs the trust hint up, and every
// new version is told here as well), and the rest is told by whoever DID the
// change, in this browser, one turn after the API said yes.
//
// That covers the three ways a person walked into a stale screen (the report of
// 19.-21.10.2026): installing an app, granting the right to use it, and taking
// a server in. All three are the person's OWN click in this very window, so the
// knowledge is here - it was simply never passed on.
//
// WHAT IT DOES NOT COVER is a change somebody ELSE made: a second admin who
// installs an app while this window stands open. No message on the link says
// so. That is why every surface built on it also asks again when the window is
// looked at again (shell/stale.js) and says since when its screen has been
// standing. A screen that can be old must not look fresh.
//
// NO TIMER, by decision. A list of apps changes when an admin changes it, which
// is minutes to months apart; a question per second would be a question per
// second for nothing, on a tunnel that carries every other session too.

// The topics. They are the STATE that changed and not the view that shows it:
// three surfaces draw the apps, and whoever installs one does not know which of
// them is open.
export const APPS = "apps";     // which apps exist, and which this user may use
export const RIGHTS = "rights"; // which role may do what
export const NODES = "nodes";   // the servers of this installation
export const USERS = "users";   // the people and their roles

const TOPICS = new Set([APPS, RIGHTS, NODES, USERS]);

export class Changed {
  // A set per topic, so hanging up twice does not tell twice.
  #at = new Map();

  // on hangs a listener up and hands back the way to take it down again. A
  // surface lives in a window, and a window closes: a listener that outlived
  // its window would draw into a body nobody can see any more, and after a
  // hundred openings there would be a hundred of them.
  on(topic, fn) {
    if (!TOPICS.has(topic) || typeof fn !== "function") return () => {};
    let set = this.#at.get(topic);
    if (set === undefined) {
      set = new Set();
      this.#at.set(topic, set);
    }
    set.add(fn);
    return () => set.delete(fn);
  }

  // tell says one topic moved. A listener that throws is this shell's own
  // fault and no reason to leave the others untold - the same rule, and the
  // same reason, as TrustStore's own #tell.
  tell(topic) {
    const set = this.#at.get(topic);
    if (set === undefined) return;
    for (const fn of [...set]) {
      try {
        fn(topic);
      } catch {
        // nothing: see above
      }
    }
  }

  // tellAll is what a new version of the trust list means. The list carries
  // the nodes, the stores and the root rights, and a surface cannot tell from
  // the outside which of them moved - so all of them are asked again. Once per
  // settled version, because that is how often TrustStore tells it.
  tellAll() {
    for (const t of TOPICS) this.tell(t);
  }
}

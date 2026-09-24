import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

const v = vectors("trust.json");

// Does the Web Crypto API of this engine know Ed25519? Every key in
// trust.json is Ed25519 - trusttest.NewSigner signs password keys with it
// too, not only NewPasskey - so an engine without it can verify no signature
// in this file at all, and every "valid" chain would come back refused
// instead of decided (measured: exactly that, on the WebKit Playwright
// brings). Named and narrow, same probe and same reasoning as sign.spec.js's
// hasEd25519, moved here because trust.spec.js has no vectors.public of its
// own to import as a quick check.
async function hasEd25519(shell) {
  return shell.evaluate(async () => {
    try {
      await crypto.subtle.importKey("raw", new Uint8Array(32), { name: "Ed25519" }, false, ["verify"]);
      return true;
    } catch {
      return false;
    }
  });
}

test("every chain of trust.json is decided as Go decides it", async ({ shell }) => {
  test.setTimeout(120_000);
  test.skip(!(await hasEd25519(shell)),
    "no Ed25519 in this engine's Web Crypto: it cannot verify any signature in trust.json");
  const got = await shell.evaluate(async (cases) => {
    const t = await import("/assets/js/contracts/trust.js");
    const { b64decode } = await import("/assets/js/contracts/b64.js");
    const out = [];
    for (const c of cases) {
      try {
        const chain = c.chain.map((s) => ({
          payload: b64decode(s.payload),
          proof: {
            kind: s.proof.kind, kid: s.proof.kid, sig: b64decode(s.proof.sig),
            authData: s.proof.authData ? b64decode(s.proof.authData) : new Uint8Array(0),
            clientData: s.proof.clientData ? b64decode(s.proof.clientData) : new Uint8Array(0),
          },
        }));
        const anchor = { seq: c.anchor.seq, hash: b64decode(c.anchor.hash) };
        const v = await t.open(anchor, chain);
        out.push({ valid: true, seq: v.list.seq });
      } catch {
        out.push({ valid: false, seq: 0 });
      }
    }
    return out;
  }, v);

  const wrong = v.filter((c, i) => c.valid !== got[i].valid || (c.valid && c.seq !== got[i].seq))
    .map((c) => c.name);
  expect(wrong).toEqual([]);
});

test("the limits without their own vectors hold too", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const t = await import("/assets/js/contracts/trust.js");
    const { utf8 } = await import("/assets/js/contracts/b64.js");
    const base = () => ({
      seq: 1, issued: 1, installation: "EREREREREREREREREREREQ",
      rp: { id: "", origins: [] }, users: [], roles: [], nodes: [], stores: [], auto: [],
    });
    const many = (n, make) => Array.from({ length: n }, (_, i) => make(i));
    // trust.js's own interface list writes "parseList(payload) -> list", but
    // the code block that defines it is "export async function parseList",
    // which it has to be: it awaits proof.validateUserKey for the id ===
    // keyID(pub) check. parseList therefore never throws synchronously - it
    // always returns a settled or pending promise - so a decodeFails that
    // calls it without await never reaches its own catch, whatever trust.js
    // does. Measured: with the literal, non-async version of this helper the
    // three cases below came back { autoOver: false, originsOver: false,
    // rootUsersOver: false } against the working validateList from the test
    // above, i.e. always false - the given helper cannot pass as specified
    // (Step 4: "die drei Grenzen ... greifen"). Fixed here by awaiting the
    // promise it was always going to get.
    const decodeFails = async (obj) => {
      try {
        await t.parseList(utf8(JSON.stringify(obj)));
        return false;
      } catch {
        return true;
      }
    };
    return {
      // trust.MaxAuto, MaxOrigins and MaxRoot have no vectors of their own.
      autoOver: await decodeFails({ ...base(), auto: many(65, (i) => ({ target: "a" + i + ".b", key: "A".repeat(43) })) }),
      originsOver: await decodeFails({ ...base(), rp: { id: "a.b", origins: many(9, () => "https://a.b") } }),
      rootUsersOver: await decodeFails({
        ...base(),
        nodes: [{ id: "ctl-1", kind: "controller", name: "n", noise: "A".repeat(43),
          rootUsers: many(257, () => "EREREREREREREREREREREQ") }],
      }),
    };
  });
  expect(out).toEqual({ autoOver: true, originsOver: true, rootUsersOver: true });
});

// The test above pins MaxAuto, MaxOrigins and MaxRoot on the "over" side
// alone, through the decode budget (which rejects an oversized array before
// validateList ever runs, because the list under test is otherwise invalid -
// its users array is empty). A guard that fired at any size over 0 would
// leave that test green too. This one pins both sides of each limit through
// validateList itself, on a structurally valid list: exactly the maximum
// is accepted, one more is refused. No real cryptography is needed, because
// validateList (via proof.validateUserKeySync) checks a key's shape only,
// never its signature or id === keyID(pub) - that check runs later, in
// parseList's async pass. This closes the open item named in Task 6.
test("MAX_AUTO, MAX_ORIGINS and MAX_ROOT are pinned on both sides, not just over", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const t = await import("/assets/js/contracts/trust.js");
    const id = "EREREREREREREREREREREQ"; // 16 bytes, valid ID shape (also used by the vectors above)
    const keyId = "AAAAAAAAAAAAAAAAAAAAAA"; // 16 zero bytes, valid key-id shape
    const validList = (patch) => ({
      seq: 1, prev: new Uint8Array(0), issued: 1, installation: id,
      rp: { id: "", origins: [] },
      users: [{
        id, name: "admin", roles: ["admin"],
        keys: [{
          id: keyId, kind: "password", alg: "EdDSA", pub: new Uint8Array(32), credId: new Uint8Array(0),
          kdf: { alg: "argon2id", salt: new Uint8Array(16), m: 65536, t: 3, p: 1 },
        }],
      }],
      roles: [{ name: "admin", level: "critical", black: [], white: [], passkeyOnly: false, passwordToo: false }],
      nodes: [], stores: [], auto: [],
      ...patch,
    });
    // Same length throughout (3-digit, zero-padded), so ascending index order
    // is ascending string order too - unpadded "a9" > "a10" would break the
    // sorted-target rule before the count limit is ever reached.
    const pad = (i) => String(i).padStart(3, "0");
    const autoEntries = (n) => Array.from({ length: n },
      (_, i) => ({ target: "app" + pad(i) + ".b", key: new Uint8Array(32) }));
    const originEntries = (n) => Array.from({ length: n }, (_, i) => "https://a" + i + ".example");
    const rootUsers = (n) => Array.from({ length: n }, () => id); // one real user, repeated: no dedup rule to honor
    const controller = (rootUsersList) => ([{
      id: "ctl-1", kind: "controller", name: "n", noise: new Uint8Array(32),
      sign: new Uint8Array(0), url: "", tls: new Uint8Array(0), rootUsers: rootUsersList, rootRoles: [],
    }]);
    return {
      autoAtLimit: t.validateList(validList({ auto: autoEntries(64) })),
      autoOverLimit: t.validateList(validList({ auto: autoEntries(65) })),
      originsAtLimit: t.validateList(validList({ rp: { id: "", origins: originEntries(8) } })),
      originsOverLimit: t.validateList(validList({ rp: { id: "", origins: originEntries(9) } })),
      rootAtLimit: t.validateList(validList({ nodes: controller(rootUsers(256)) })),
      rootOverLimit: t.validateList(validList({ nodes: controller(rootUsers(257)) })),
    };
  });
  expect(out).toEqual({
    autoAtLimit: true, autoOverLimit: false,
    originsAtLimit: true, originsOverLimit: false,
    rootAtLimit: true, rootOverLimit: false,
  });
});

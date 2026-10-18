import { test, expect } from "../fixtures/page.js";
import { vectors } from "../fixtures/vectors.js";

const v = vectors("url.json");

test("hosts and addresses follow the same grammar as in Go", async ({ shell }) => {
  const got = await shell.evaluate(async (data) => {
    const u = await import("/assets/js/contracts/url.js");
    const check = (list, fn) => list.map((c) => fn(c.value));
    return {
      hosts: check(data.hosts, u.validHost),
      hostPort: check(data.hostPort, u.validHostPort),
      tunnel: check(data.tunnel, u.validTunnelURL),
      web: check(data.web, u.validWebURL),
      origins: check(data.origins, u.validOrigin),
      store: check(data.store, u.validStoreURL),
      // For a valid tunnel address, webURL derives the address behind Apache.
      derived: data.tunnel.map((c) => (c.valid ? u.webURL(c.value) : null)),
    };
  }, v);

  for (const key of ["hosts", "hostPort", "tunnel", "web", "origins", "store"]) {
    const wrong = v[key].filter((c, i) => c.valid !== got[key][i]).map((c) => `${key}: ${c.name}`);
    expect(wrong).toEqual([]);
  }
  const badDerived = v.tunnel
    .map((c, i) => (c.valid && got.derived[i] !== c.web ? c.name : null))
    .filter(Boolean);
  expect(badDerived).toEqual([]);
});

// url.json holds two cases for the spelling of an IPv6 address, and that is
// where a second implementation drifts away from netip.Addr.String without
// anyone noticing: a host written two ways matches the trust list once and
// misses it once. Every answer below was read off Go itself (trust.ValidHost
// of the version in shared/), never off the rule.
const spelling = [
  // A run of two zero groups or more is compressed, a single one is not.
  ["[2001:db8:0:1:1:1:1:1]", true], ["[2001:db8::1:1:1:1:1]", false],
  ["[1:0:0:0:0:0:0:8]", false], ["[1::8]", true],
  ["[0:0:0:0:0:0:0:0]", false], ["[::]", true],
  // The longest run wins, and of two equally long runs the first.
  ["[1:0:0:1:0:0:0:1]", false], ["[1:0:0:1::1]", true], ["[1::1:0:0:0:1]", false],
  ["[0:0:1:0:0:1:1:1]", false], ["[::1:0:0:1:1:1]", true], ["[0:0:1::1:1:1]", false],
  // ::ffff:a:b is an IPv4 address in disguise and would give it a second
  // spelling; ::ffff on its own is an ordinary address.
  ["[::ffff:0:0]", false], ["[::ffff:0:1]", false], ["[0:0:0:0:0:ffff:1:2]", false],
  ["[::ffff:192.0.2.1]", false], ["[::ffff]", true],
  // Leading zeros, upper case, a zone, half a bracket: each one host too many.
  ["[2001:0db8::1]", false], ["[00:0:0:0:0:0:0:1]", false], ["[0:0:0:0:0:0:0:01]", false],
  ["[2001:DB8::1]", false], ["[fe80::1%eth0]", false], ["[fe80::1%25eth0]", false],
  ["[:1]", false], ["[1:]", false], ["[]", false], ["[", false], ["[::1", false],
  ["::1]", false], ["[1:2:3:4:5:6:7:8]", true], ["[a:b:c:d:e:f:1:2]", true],
  ["[1::]", true], ["[::0]", false], ["[0::]", false], ["[1:2:3:4:5:6:7::]", false],
  ["[1:2:3:4:5:6:7:8:9]", false], ["[1::2::3]", false], ["[:::]", false],
  ["0.0.0.0", true], ["255.255.255.255", true], ["256.0.0.1", false],
  ["1.2.3.4.", false], ["x.1", false], ["xn--a.test", true],
];

test("an IPv6 address has exactly the spelling netip writes", async ({ shell }) => {
  const got = await shell.evaluate(async (list) => {
    const { validHost } = await import("/assets/js/contracts/url.js");
    return list.map((h) => validHost(h));
  }, spelling.map(([h]) => h));

  const wrong = spelling.filter(([, want], i) => got[i] !== want).map(([h]) => h);
  expect(wrong).toEqual([]);
});

// What a caller leaves out, or hands over in the wrong form, is no address.
// A validator that answered true, or threw, for anything but a string would
// put that value into an Apache configuration or a trust list.
test("only a string can be an address", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const u = await import("/assets/js/contracts/url.js");
    const names = ["validHost", "validHostPort", "validTunnelURL", "validWebURL",
      "validOrigin", "validStoreURL"];
    // An object that looks like an address only once someone asks it for text.
    const sneaky = { toString: () => "api-1.test" };
    const values = [undefined, null, 0, 1, true, false, {}, [], sneaky,
      ["api-1.test"], new String("api-1.test"), Symbol("api-1.test")];
    const bad = [];
    for (const name of names) {
      for (let i = 0; i < values.length; i++) {
        let got;
        try {
          got = u[name](values[i]);
        } catch (e) {
          bad.push(`${name}(#${i}) threw ${e.constructor.name}`);
          continue;
        }
        if (got !== false) bad.push(`${name}(#${i}) answered ${String(got)}`);
      }
    }
    for (let i = 0; i < values.length; i++) {
      let got;
      try {
        got = u.webURL(values[i]);
      } catch (e) {
        bad.push(`webURL(#${i}) threw ${e.constructor.name}`);
        continue;
      }
      if (got !== null) bad.push(`webURL(#${i}) answered ${String(got)}`);
    }
    return bad;
  });
  expect(out).toEqual([]);
});

// Where an address is cut decides which part is checked as what. These
// answers come from Go as well.
test("host, port, origin and path are cut the way Go cuts them", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const u = await import("/assets/js/contracts/url.js");
    return {
      // The port is split off at the last colon, so of the IPv6 addresses
      // only the bracketed form survives.
      hostPortIPv6: u.validHostPort("[::1]:1") && !u.validHostPort("[2001:db8::5]")
        && !u.validHostPort("a:1:2") && !u.validHostPort(":1") && !u.validHostPort("a:"),
      // Only ":443" is missing from an origin; every other port is written out.
      origin443: u.validOrigin("https://a:80") && !u.validOrigin("https://a:443")
        && !u.validOrigin("https://[::1]:443") && u.validOrigin("https://[::1]")
        && u.validOrigin("https://a:4430"),
      // A store path knows only whole segments: ".." alone is refused, a name
      // that merely begins with dots is not.
      storePath: u.validStoreURL("https://a/..a") && u.validStoreURL("https://a/a..")
        && !u.validStoreURL("https://a/a/../b") && !u.validStoreURL("https://a//")
        && !u.validStoreURL("https://a/ngc/") && u.validStoreURL("https://a/a/b/c")
        && u.validStoreURL("https://[2001:db8::1]:8443/ngc"),
      // The tunnel path is cut off once, not repeatedly.
      tunnelPath: u.validTunnelURL("wss://a:1/tunnel")
        && !u.validTunnelURL("wss://a:1/tunnel/tunnel") && !u.validTunnelURL("wss:///tunnel")
        && !u.validTunnelURL("wss://:1/tunnel"),
      // Every address webURL hands out has to pass the check of the value it
      // becomes; otherwise web.json would hold an address nothing accepts.
      derived: u.webURL("wss://[2001:db8::1]:7443/tunnel") === "https://[2001:db8::1]:7443"
        && u.validWebURL(u.webURL("wss://[2001:db8::1]:7443/tunnel"))
        && u.webURL("wss://a:1/tunnel/") === null,
    };
  });
  expect(out).toEqual({
    hostPortIPv6: true, origin443: true, storePath: true, tunnelPath: true, derived: true,
  });
});

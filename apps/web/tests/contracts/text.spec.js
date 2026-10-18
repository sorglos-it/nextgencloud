import { test, expect } from "../fixtures/page.js";

// wire.ValidText counts UTF-8 bytes and asks every code point, not every
// UTF-16 unit, whether it is printable.
test("text for people follows wire.ValidText", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const t = await import("/assets/js/contracts/text.js");
    return {
      empty: t.validText("", 10),
      plain: t.validText("Terminal", 64),
      space: t.validText("App Store", 64),
      umlaut: t.validText("K\u00e4se", 64),
      emoji: t.validText("Ordner \u{1F600}", 64),
      tab: t.validText("a\tb", 64),
      newline: t.validText("a\nb", 64),
      // U+202E, the right-to-left override: written out, because a source
      // file that carries it shows its own text in the wrong order.
      rtlOverride: t.validText("a\u202eb", 64),
      // U+FDD0, a noncharacter no table will ever assign.
      noncharacter: t.validText("\ufdd0", 64),
      // The limit counts bytes: four umlauts are eight bytes.
      bytesOverLimit: t.validText("\u00e4\u00e4\u00e4\u00e4", 7),
      bytesAtLimit: t.validText("\u00e4\u00e4\u00e4\u00e4", 8),
      name: t.validName("ctl-1") && !t.validName("1ctl") && !t.validName("CTL"),
      appID: t.validAppID("org.sorglos.terminal") && !t.validAppID("terminal"),
      core: t.coreApp("ngc.desktop") && !t.coreApp("org.sorglos.terminal"),
      path: t.validPath("index.html") && !t.validPath("../x") && !t.validPath(".hidden"),
      version: t.validVersion("1.0.0") && !t.validVersion("01.0.0") && !t.validVersion("1.0"),
      newer: t.newer("1.1.0", "1.0.9") && !t.newer("1.0.0", "1.0.0"),
      minNGC: t.supportsMinNGC("1.2.0", "1.1.0") && !t.supportsMinNGC("0.0.0-dev", "1.0.0")
        && t.supportsMinNGC("1.0.0", ""),
      id: t.validID("EREREREREREREREREREREQ") && !t.validID("ERERERERERERERERERERE"),
      logID: t.validLogID("0123456789abcdef0123456789abcdef")
        && !t.validLogID("0123456789ABCDEF0123456789abcdef"),
    };
  });
  expect(out).toEqual({
    empty: false, plain: true, space: true, umlaut: true, emoji: true,
    tab: false, newline: false, rtlOverride: false, noncharacter: false,
    bytesOverLimit: false, bytesAtLimit: true,
    name: true, appID: true, core: true, path: true, version: true, newer: true,
    minNGC: true, id: true, logID: true,
  });
});

// The edges of the rule, each answer taken from Go (wire.ValidText of the
// version in shared/): the characters that look like a space but are none,
// the ones that are in no table, and the limit at its exact byte.
const edges = [
  ["a b", 64, true],
  ["a\u00a0b", 64, false], // no-break space: a spacing character, not printable
  ["a\u00adb", 64, false], // soft hyphen
  ["a\u200bb", 64, false], // zero width space
  ["a\u2000b", 64, false], // en quad
  ["a\u3000b", 64, false], // ideographic space
  ["a\u2028b", 64, false], // line separator
  ["a\u0080b", 64, false], // C1 control
  ["a\u007fb", 64, false], // delete
  ["a\ufffdb", 64, true], // the replacement character is a symbol and printable
  ["a\uffffb", 64, false], // noncharacter
  ["a\ufffeb", 64, false], // noncharacter
  ["a\u{1fffe}b", 64, false], // noncharacter of another plane
  ["\u0378", 64, false], // unassigned
  ["\u{e0001}", 64, false], // a tag character, no longer assigned
  ["\ud800", 64, false], // half a surrogate pair, which is no code point
  ["\u00e9", 1, false],
  ["\u00e9", 2, true],
  ["a", 0, false],
  ["a", -1, false],
];

test("the edges of wire.ValidText are the edges Go has", async ({ shell }) => {
  const got = await shell.evaluate(async (list) => {
    const { validText } = await import("/assets/js/contracts/text.js");
    return list.map(([s, max]) => validText(s, max));
  }, edges.map(([s, max]) => [s, max]));

  const wrong = edges.filter(([, , want], i) => got[i] !== want).map(([s, max]) => `${JSON.stringify(s)} max=${max}`);
  expect(wrong).toEqual([]);
});

// Everything else Go decides, held against Go's answers. Names, paths and
// versions end up in file names, in update decisions and on the screen.
test("names, paths, versions and ids are decided as in Go", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const t = await import("/assets/js/contracts/text.js");
    const a = (n) => "a".repeat(n);
    return {
      // A name may end with a hyphen but not begin with one, and is at most
      // 32 characters long.
      name: t.validName("a") && t.validName("a-") && !t.validName("-a")
        && t.validName(a(32)) && !t.validName(a(33)) && !t.validName("ctl-1\n")
        && !t.validName("ctl 1") && !t.validName(""),
      // An app id is a reverse domain of at most 100 characters; only the
      // labels after the first may hold a hyphen.
      appID: t.validAppID("a.b") && t.validAppID("a.b-c") && !t.validAppID("a-b.c")
        && !t.validAppID("A.b") && !t.validAppID("a.") && !t.validAppID(".a")
        && !t.validAppID("a..b") && !t.validAppID("a.b\n")
        && t.validAppID(a(98) + ".b") && !t.validAppID(a(99) + ".b"),
      // Only the prefix decides what belongs to NGC itself.
      core: t.coreApp("ngc.") && !t.coreApp("ngc") && !t.coreApp("xngc.a") && !t.coreApp(""),
      // A path has no hidden file, no "..", no backslash and no leading
      // slash, and is at most 200 bytes long.
      path: t.validPath("a.b/c-d_e") && t.validPath("-a") && t.validPath("_a")
        && t.validPath("a..b") && !t.validPath("a/.b") && !t.validPath("a/")
        && !t.validPath("/a") && !t.validPath("a//b") && !t.validPath("a\\b")
        && !t.validPath("a/..") && !t.validPath("..") && !t.validPath("a/../b")
        && t.validPath(a(200)) && !t.validPath(a(201)),
      version: t.validVersion("0.0.0") && t.validVersion("999999.0.0")
        && !t.validVersion("1000000.0.0") && !t.validVersion("1.0.0-dev")
        && !t.validVersion("1.0.0\n") && !t.validVersion(" 1.0.0"),
      // Version parts are compared as numbers, not as text.
      newer: t.newer("10.0.0", "9.0.0") && t.newer("0.0.1", "0.0.0")
        && !t.newer("x", "1.0.0") && !t.newer("1.0.0", "x"),
      // An empty minNgc suits every build, even one that is no release; a
      // build that is no release suits no minNgc.
      minNGC: t.supportsMinNGC("0.0.0-dev", "") && t.supportsMinNGC("", "")
        && !t.supportsMinNGC("", "1.0.0") && t.supportsMinNGC("1.0.0", "1.0.0")
        && !t.supportsMinNGC("1.0.0", "1.0.1"),
      // An id is 16 bytes in base64url, decoded strictly: the last four bits
      // belong to no byte and have to be zero.
      id: t.validID("AAAAAAAAAAAAAAAAAAAAAA") && !t.validID("AAAAAAAAAAAAAAAAAAAAAB")
        && t.validID("A-_AAAAAAAAAAAAAAAAAAA") && !t.validID("ERERERERERERERERERERER")
        && !t.validID("EREREREREREREREREREREQ\n") && !t.validID("EREREREREREREREREREREQ="),
      logID: !t.validLogID("0123456789abcdef0123456789abcde")
        && !t.validLogID("0123456789abcdef0123456789abcdeff"),
      // A login name begins with a letter or a digit and is at most 64 long.
      login: t.validLoginName("a.b") && t.validLoginName("a_b") && t.validLoginName("a-b")
        && t.validLoginName("0a") && !t.validLoginName(".a") && !t.validLoginName("_a")
        && !t.validLoginName("-a") && !t.validLoginName("A") && !t.validLoginName("\u00e4")
        && !t.validLoginName("") && t.validLoginName(a(64)) && !t.validLoginName(a(65))
        && !t.validLoginName("a b") && !t.validLoginName("a\n"),
      // Six ASCII digits, and no digits of another script.
      totp: t.validTOTP("000000") && t.validTOTP("123456") && !t.validTOTP("12345")
        && !t.validTOTP("1234567") && !t.validTOTP("12345a") && !t.validTOTP("+12345")
        && !t.validTOTP(" 12345") && !t.validTOTP("\u0661\u0662\u0663\u0664\u0665\u0666"),
      // The length that counts is the one in UTF-8 bytes.
      bytes: t.byteLength("") === 0 && t.byteLength("a") === 1
        && t.byteLength("\u00e4\u00e4\u00e4\u00e4") === 8 && t.byteLength("\u20ac") === 3
        && t.byteLength("\u{1F600}") === 4,
    };
  });
  expect(out).toEqual({
    name: true, appID: true, core: true, path: true, version: true, newer: true,
    minNGC: true, id: true, logID: true, login: true, totp: true, bytes: true,
  });
});

// Go has no value that is missing. Here one can be handed in anywhere, and a
// check that answered true for it, or threw, would let a name, a path or a
// version through that nobody ever wrote.
test("what is missing or of the wrong kind is refused", async ({ shell }) => {
  const out = await shell.evaluate(async () => {
    const t = await import("/assets/js/contracts/text.js");
    const values = [undefined, null, 0, 1, true, false, {}, [], ["a"],
      { toString: () => "ctl-1" }, new String("ctl-1"), Symbol("ctl-1")];
    const single = ["validName", "validAppID", "coreApp", "validLoginName", "validID",
      "validLogID", "validPath", "validVersion", "validTOTP"];
    const bad = [];
    const ask = (name, fn, want, label) => {
      let got;
      try {
        got = fn();
      } catch (e) {
        bad.push(`${name}(${label}) threw ${e.constructor.name}`);
        return;
      }
      if (got !== want) bad.push(`${name}(${label}) answered ${String(got)}`);
    };
    for (const name of single) {
      values.forEach((x, i) => ask(name, () => t[name](x), false, `#${i}`));
    }
    values.forEach((x, i) => ask("validText", () => t.validText(x, 64), false, `#${i}`));
    values.forEach((x, i) => ask("newer", () => t.newer(x, "1.0.0"), false, `a#${i}`));
    values.forEach((x, i) => ask("newer", () => t.newer("1.0.0", x), false, `b#${i}`));
    values.forEach((x, i) => ask("supportsMinNGC", () => t.supportsMinNGC(x, "1.0.0"), false, `a#${i}`));
    // A missing limit closes the gate: no text is 1 to nothing bytes long.
    ask("validText", () => t.validText("Terminal"), false, "no max");
    ask("validText", () => t.validText("Terminal", null), false, "null max");
    ask("validText", () => t.validText("Terminal", "64"), false, "text max");
    ask("validText", () => t.validText("Terminal", 64.5), false, "fraction max");
    ask("validText", () => t.validText("Terminal", NaN), false, "NaN max");
    ask("validText", () => t.validText("Terminal", Infinity), false, "Infinity max");
    // A missing minNgc is not an empty minNgc: Go is handed a string there.
    ask("supportsMinNGC", () => t.supportsMinNGC("1.0.0"), false, "no minNgc");
    ask("supportsMinNGC", () => t.supportsMinNGC("1.0.0", null), false, "null minNgc");
    // byteLength has nothing to count without a string, and an answer of 0
    // would be the one that opens every limit. It refuses, like b64.js.
    const { BadInput } = await import("/assets/js/contracts/b64.js");
    values.forEach((x, i) => {
      let got;
      try {
        got = t.byteLength(x);
      } catch (e) {
        if (!(e instanceof BadInput)) bad.push(`byteLength(#${i}) threw ${e.constructor.name}`);
        return;
      }
      bad.push(`byteLength(#${i}) answered ${String(got)}`);
    });
    return bad;
  });
  expect(out).toEqual([]);
});

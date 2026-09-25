import { readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { test, expect } from "../fixtures/page.js";

// The ninth case of one mistake, caught by a rule instead of by hand.
//
// Go is handed a []byte, whose length is the slice's own: two looks give one
// number, and the compiler answers for it. Here every byte string comes out
// of an object the caller owns, and a subclass of Uint8Array may define
// length. Eight times that was found one at a time and fixed one at a time.
// This test is the ninth, and it is not a case: it walks every callable of
// every contract and holds each place that takes a caller's bytes to one
// reading of them. "Every contract" was a list of eleven names by hand until
// this round, and a twelfth file beside them was walked by nobody; it is the
// contents of contracts/ now, subfolders and all.
//
// THE PROPERTY: the number that was checked against is the number that is
// worked with.
//
// The round before this one counted how often the size was asked for and
// held that at one. Counting is a stand-in, and three measurements broke it:
//
//   1. a second reading that only starts at 8 bytes stayed invisible, because
//      the probe was always 2 bytes long;
//   2. an export that is no function was never walked at all - a
//      `export const frameTool = { check(bytes){...} }` read the size three
//      times, wrote 0 bytes where 4 were checked, and the guard stayed green;
//   3. noise.Transport.seal and open asked exactly once, and put 70000 bytes
//      through a bound of 65519 all the same: they ask the object, and then
//      hand that same object to the Web Crypto API, which takes the bytes out
//      of the internal slot. One asking, two numbers.
//
// So this test does not count. It reads WHICH properties were touched, and
// holds that the only reading a place may show is the one b64.viewOf makes:
// buffer, byteOffset and byteLength, once each, and length not at all.
//
//   - buffer, byteOffset and byteLength once each is a view built once. That
//     view is a plain Uint8Array, and everything after it - the bound, the
//     indexes, the copy, the engine - reads that and nothing else. One
//     number, and it is the one that was checked.
//   - twice is two views, and two views of the same object need not be the
//     same bytes.
//   - byteLength without buffer is a measurement with no view behind it: the
//     size was taken off the object and the object itself went on to the
//     engine. That is measurement 3 above, and it is what a counter cannot
//     see.
//   - length is never the view's number. It is a second number beside it, and
//     a bound checked against a second number decides about something else.
//
// What this holds and what it does not: the arguments of a function, and not
// the members of an object it is handed. session.encodeClientCert takes a
// certificate whose nonce, sig and dh are the caller's bytes, and that one is
// held where it stands: encodeClientCert makes one view of each field and
// hands the check and the writing that same view. An independent
// measurement of the stand this round started from counted 40 such fields and
// found 27 of them permeable; those are a task of their own and are named
// there, not here. A probe cannot be handed to seven members at once without
// their askings running into one number, so this test names the property it
// can decide for certain and leaves the other one where its reason is.
//
// And it holds the argument positions a function DECLARES, which is not the
// same as the positions it takes. The walk asks f.length, and f.length counts
// neither a rest parameter nor a parameter with a default: b64.concat(...parts)
// has f.length === 0, so the list below knows b64.concat#0 and nothing about
// the second part a caller hands it, or any after it. Those positions do the
// same work as the first - concat views every part in one map - but that is
// read off the source here and not measured, and nothing stops a later body
// from treating part 2 differently from part 1. Named, not closed: handing the
// probe to several positions at once makes their readings run into one number
// and decides nothing about either.

// The modules this walks, read off the directory and not written down.
//
// It was a list of eleven names by hand until this round, and the review put a
// twelfth contract beside them: contracts/frame.js, a parser with a bound of
// 65519 and no caller anywhere. The whole run stayed green - 116 passed, this
// guard among them - and a probe took 70000 bytes through that bound. A list by
// hand cannot go red for a file nobody wrote into it, and the "unlisted" check
// below did not help: it only ever sees a module that one of the listed eleven
// imports, and a new entry point has no importer yet. The same hole covered a
// contract in a subfolder, because no name on that list could carry a path.
//
// So the list is the directory. Every .js under contracts/ is walked,
// subfolders included, and a module in a subfolder carries its path in its
// name: contracts/sub/frame.js is "sub/frame" and its places read
// "sub/frame.parseFrame#0". Put a contract anywhere in there and it is walked
// from that moment, its callables raise the count against CALLABLES below, and
// every place in it that takes a caller's bytes has to be entered in ENTRIES
// before this is green again.
//
// Measured in chromium, the same contract planted at both places, each as the
// only change in a copy:
//
//   contracts/frame.js         list by hand: 116 passed. Directory:
//                              "frame.parseFrame#0: not in the list,
//                              reads 1/0/0/0"
//   contracts/sub/frame.js     list by hand: 116 passed. Directory:
//                              "sub/frame.parseFrame#0: not in the list,
//                              reads 1/0/0/0"
//
// and in both green runs a probe took 70000 bytes through that contract's
// bound of 65519. A contract that takes no caller bytes at all runs into
// CALLABLES instead and not into the list of places: measured, 141 against
// 140.
//
// What the directory cannot answer for is a contract OUTSIDE contracts/, and
// the "unlisted" check is what covers that. Measured the same way: a contract
// at /assets/js/frame.js with one import of it in wire.js left the list by
// hand at 116 passed, and here it is named - "/assets/js/frame.js". Where that
// ends is worth saying: a file outside contracts/ that NO contract imports is
// reached by neither check, and it is also reached by nothing else in the
// tree. The moment anything in contracts/ imports it, it is named.
const CONTRACTS_DIR = fileURLToPath(new URL("../../public/assets/js/contracts/", import.meta.url));
const MODULES = moduleNames(CONTRACTS_DIR);

function moduleNames(dir, prefix = "") {
  const out = [];
  for (const e of readdirSync(dir, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
    if (e.isDirectory()) out.push(...moduleNames(`${dir}/${e.name}`, `${prefix}${e.name}/`));
    else if (e.name.endsWith(".js")) out.push(prefix + e.name.slice(0, -3));
  }
  return out;
}

// The three the walk itself leans on: it makes a Budget and a shape out of
// json, a handshake out of noise and decodes a key with b64. If one of them is
// renamed or moved into a subfolder the walk would quietly do less, so it says
// so instead.
const NEEDED = ["b64", "json", "noise"];

// The sizes the probe is run at. A probe that knows one size checks one size:
// a reading that only starts above some number stays invisible below it, and
// that is how the round before this one was walked past. Every bound the
// eleven contracts declare for a byte string has a size above it and a size
// below it here, and the small end is dense because that is where the fixed
// sizes sit.
//
//   0,1,2,3,4,7,8              nothing, and the edges of the three-byte step
//                              b64encode walks in
//   15..17, 31..33, 47..49,    around SALT_SIZE and the two KEY_ID_SIZEs (16),
//   63..65                     NONCE/TOKEN/KEY/CERT/HASH_SIZE and ID_SIZE
//                              (32), HANDSHAKE_SIZE (48), SIG_SIZE and
//                              MAX_TRUST_HINT (64)
//   1023..1025                 MAX_CRED_ID (1023), MAX_PUBLIC and MAX_SIG
//   2390, 2391                 the envelope budget of parseEnvelope(b, 1024)
//   4096, 4097                 MAX_CERT, MAX_AUTH_DATA, MAX_CLIENT_DATA
//   8192, 8193                 MAX_PARAMS
//   16384, 16385               MAX_TASK, MAX_BROWSER_OPEN
//   40960, 40961               MAX_OPEN_PAYLOAD
//   65519, 65520               MAX_FRAME and MAX_PLAINTEXT - the bound
//                              70000 bytes walked through
//   65535, 65536, 65537        MAX_MESSAGE, MAX_REQUEST
//   131072, 131073             MAX_SETUP_OFFER
//
// It stops there, and that is a decision with a measurement behind it. The
// four bounds above this line - sign.MAX_PAYLOAD 4 MiB, the signed-list
// budget of 5657942, wire.MAX_CALL_RESULT 8 MiB and session.MAX_CHAIN
// 32 MiB - were run, straddled on both sides: the test took 2.7 minutes
// instead of a few seconds on chromium and came back with the same 63 places
// and the same readings, to the character. The cost is b64encode, concat and
// equal, which have no bound at all and do their whole work at whatever size
// they are handed. Nothing was bought, because everything a size can show
// here is shown before that work starts - the reading of the size is the
// first thing each of these places does. Whoever raises this ceiling now
// knows what it costs and what it found.
//
// AND THESE 39 SIZES ARE A SAMPLE, NOT A SWEEP. The review found what that
// costs and it was measured again here: a second view in json.decode, taken
// after the gate and only for a first view between 5000 and 6000 bytes - a
// band no size below touches - left the whole run green, and a probe put 70000
// bytes through a budget of 6000. Measured in chromium, the mutation as the
// only change in a copy: 116 passed on the stand this round started from and
// 117 here, the probe counted in each.
//
// That second view DOES touch the object, and it would read 0/2/1/1 - the
// reading this guard refuses. It is invisible all the same, because it only
// happens at sizes no probe is ever run at. The 39 cover every bound the
// contracts DECLARE, and a second reading need not sit at a bound at all.
//
// It is not closed here, and the decision is deliberate. More sizes do not
// close it: any list is finite, so a band of one single size dodges whatever
// is added, and adding 5000..6000 would kill that one mutation and not its
// shape. A measure that does not hang on sizes would have to decide from the
// OUTCOME of a call instead of from the readings, and the 63 places share no
// outcome - they answer objects, strings, booleans and exceptions - so there
// is nothing to compare against for all of them. What would close it is
// reading the source for a second view rather than running the code, and that
// is a different kind of test and a round of its own.
//
// So it is named and fenced: what this guard decides, it decides AT THE SIZES
// IT RUNS. A place whose second reading exists only inside a band no probe
// visits is invisible to it, and whoever reads a green run should know that
// much. The sizes are returned and asserted below for exactly that reason.
const SIZES = [
  0, 1, 2, 3, 4, 7, 8,
  15, 16, 17, 31, 32, 33, 47, 48, 49, 63, 64, 65,
  1023, 1024, 1025, 2390, 2391, 4096, 4097, 8192, 8193, 16384, 16385,
  40960, 40961, 65519, 65520, 65535, 65536, 65537, 131072, 131073,
];

// The reading a place may show, written as length/byteLength/buffer/byteOffset.
//
//   view    the one b64.viewOf makes, and nothing besides.
//   list    the probe stood in for an array here, not for bytes: the place
//           reads length because that is the array's own length, and it never
//           looks at the bytes at all. One of them, and it is named.
//   open    a place that reads length BESIDE its view and AFTER it. The order
//           is held as well as the numbers, and this round is why: the same
//           1/1/1/1 is safe in one order and an open door in the other. The
//           reason and the measurement stand next to the names below.
// The view, and the view with the one extra reading of length beside it.
const VIEW = "0/1/1/1";
const VIEW_AND_LENGTH = "1/1/1/1";
// Only length, and no byte property at all: the probe was never read as bytes.
const LIST = "1/0/0/0";
// A place that takes a COPY of the caller's bytes and then asks the caller's
// own two numbers, to refuse an object that will not own up to them. The
// copy reads nothing - b64.bytesOf goes to the internal slots, where no getter
// can be installed - so buffer and byteOffset stay at 0, and that is what says
// no window onto the caller's memory was ever opened.
const COPY_AND_NUMBERS = "1/1/0/0";
const LEGAL = {
  view: [VIEW],
  list: [LIST],
  // An "open" place either refused on its view before it got to the length -
  // that is VIEW - or read both, which is VIEW_AND_LENGTH. LIST was legal here
  // until this round, because the length used to be read first and a place
  // that refused on it never built a view at all. With the view first that
  // reading cannot happen any more, so it is no longer allowed: a length with
  // no view behind it is now a finding for these places too.
  //
  // NOTHING IS "open" ANY MORE, and whoever makes a place open again should
  // read this first. The three verifyRaw positions were the only ones and they
  // are "copy" now. The kind is kept because the shape may come back, and with
  // it the gap it is known to have: the order rule below finds the first B and
  // refuses an L before it, so it holds only while the view's FIRST reading is
  // byteLength. A body that reads its four in the order B,L,F,O reads each
  // exactly once, shows 1/1/1/1, and passes - measured green - and a hostile
  // buffer getter steers the bytes there, because buffer is read after the
  // length that was supposed to come last.
  //
  // That gap was not closed, and the reason is that it is not worth closing:
  // it guards an ORDER, and the round that wrote this comment measured a shape
  // no order can guard - an ordinary array, no getter anywhere, rewritten in a
  // microtask while the engine is awaited (see sign.verifyRaw below). A place
  // that handles a caller's bytes should be "copy" and hand the engine bytes
  // of its own. If you are about to write "open" instead, that is the thing to
  // weigh, and this is the measurement to weigh it against.
  open: [VIEW, VIEW_AND_LENGTH],
  // A "copy" place may show nothing but the two numbers. Any reading of buffer
  // or byteOffset here is a finding and means somebody put a view back - a
  // window onto memory the caller still holds, which is the one thing these
  // places exist not to have. Only one reading is legal, so it is also what
  // proves the two numbers are reached at all.
  copy: [COPY_AND_NUMBERS],
};

// Every place that takes a caller's bytes, as name#argument, with the reading
// it is allowed to show. A new one, or one that disappears, is a thing for
// somebody to look at, so the list and its length are both asserted. Whoever
// adds a line here has said which of the three it is.
//
// The list does not depend on the engine any more, and that was worth doing.
// Until this round six places read the size under node and not in the
// browsers - crypto.subtle.digest asks its argument there and takes the bytes
// out of the internal slot here - so node found 44 where chromium and webkit
// found 38, and a browser that started asking would have turned this red with
// nothing in the tree changed. The six are noise.e2ePrologue#4 and #5,
// session.certHash#0, sign.keyID#0, sign.keyIDAsync#0 and trust.tlsPin#0, and
// each of them handed the caller's object straight to the digest. They now
// hand it the view, so the engine asks the view and never the object.
// Measured after that change: node 24 and chromium read exactly the same for
// every place in this list.
//
// If an engine update turns this red all the same, the failure names the
// place and the reading it showed, and there are only two answers. If the
// reading is 0/1/1/1 and the place is simply new to the list, the engine has
// started asking somewhere the tree hands it a caller's object - find that
// spot and put a viewOf in front of it, the way the six above were done, and
// the place leaves the list again. If the reading is anything else, the
// engine is doing what a lying object could do, and then it is a finding and
// not a chore. What is never the answer is widening LEGAL: that is the one
// edit that makes this file stop holding anything.
const ENTRIES = {
  "b64.b64encode#0": "view",
  "b64.concat#0": "view",
  "b64.equal#0": "view",
  "b64.equal#1": "view",
  "b64.viewOf#0": "view",
  "json.decode#0": "view",
  "noise.HandshakeKK.build#1": "view",
  "noise.HandshakeKK.build#3": "view",
  "noise.HandshakeKK.build#4": "view",
  "noise.HandshakeKK.build#5": "view",
  "noise.HandshakeKK.decryptAndHash#0": "view",
  "noise.HandshakeKK.encryptAndHash#0": "view",
  "noise.HandshakeKK.forTest#1": "view",
  "noise.HandshakeKK.forTest#2": "view",
  "noise.HandshakeKK.forTest#3": "view",
  "noise.HandshakeKK.forTest#4": "view",
  "noise.HandshakeKK.mixHash#0": "view",
  "noise.HandshakeKK.of#1": "view",
  "noise.HandshakeKK.of#3": "view",
  "noise.HandshakeKK.of#4": "view",
  "noise.HandshakeKK.readMessage1#0": "view",
  "noise.HandshakeKK.readMessage2#0": "view",
  "noise.Transport.open#0": "view",
  "noise.Transport.seal#0": "view",
  "noise.e2ePrologue#4": "view",
  "noise.e2ePrologue#5": "view",
  "proof.challenge#1": "view",
  "proof.privateFromSeed#0": "view",
  "proof.publicFromSeed#0": "view",
  "session.certHash#0": "view",
  "session.offerMAC#1": "view",
  "session.parseLoginChallenge#0": "view",
  "session.parseLoginResult#0": "view",
  "session.parseSetupOffer#0": "view",
  "session.parseTrustChain#0": "view",
  "session.parseTrustHint#0": "view",
  "session.setupMAC#1": "view",
  "session.verifyAPI#2": "view",
  "session.verifyApproval#0": "view",
  "sign.equal#0": "view",
  "sign.equal#1": "view",
  "sign.keyID#0": "view",
  "sign.keyIDAsync#0": "view",
  "sign.message#1": "view",
  "sign.parseEnvelope#0": "view",
  // verifyRaw no longer builds a view on the caller's memory at all, and the
  // three positions moved from "open" to "copy" because of it.
  //
  // Five rounds argued here about the ORDER of the readings. The table this
  // comment used to carry is why the argument never ended - two hostile
  // objects with opposite outcomes, so that whichever order refused the one
  // accepted the other:
  //
  //                                            flipping   throwing
  //   length first, then the view (f24686c)    true       false
  //   the view only, no length read            false      true
  //   the view first, then the length          false      false
  //
  // and each round a new shape turned up that the newest order did not cover.
  // The measurement that ended it needs no getter at all, so no order could
  // ever have covered it and neither can this guard, which watches which
  // properties were touched:
  //
  //   const plain = new Uint8Array(64).fill(0x5a);   // an ordinary array
  //   queueMicrotask(() => plain.set(realSignature));
  //   await verifyRaw(pub, plain, msg);              // on 8b6fa87: true
  //
  // Every synchronous check saw rubbish; crypto.subtle.verify got the real
  // signature, because the rewrite lands during the awaits. An ordinary array
  // touches no property out of the ordinary, so a counter of touches sees
  // nothing, and a rule about order has nothing to order.
  //
  // So verifyRaw stopped handing out a window onto the caller's memory:
  // b64.bytesOf copies the bytes into a buffer of ours before anything is
  // decided, every size is taken off that copy, and the engine gets the copy.
  // The caller's own length and byteLength are still read - that is the "1/1"
  // below - but now only to REFUSE an object that will not own up to the bytes
  // it handed over, which keeps the accepted assurance "a check of a signature
  // answers false for an envelope that fights back". They decide nothing about
  // the bytes, because the bytes are already out of reach when they run.
  //
  // What this guard holds here now is the 0/0 half, and it is the stronger
  // half: buffer and byteOffset are NEVER touched, so no view onto the
  // caller's memory is built at any size. sign.spec.js "verifyRaw decides on a
  // copy no getter can reach" holds the rest - eight shapes at three arguments
  // and the plain array besides, each handing over rubbish and revealing the
  // bytes that would verify only through its trick.
  "sign.verifyRaw#0": "copy",
  "sign.verifyRaw#1": "copy",
  "sign.verifyRaw#2": "copy",
  "task.parseTask#0": "view",
  "task.signTask#1": "view",
  // open(anchor, chain) - chain is a list of signed lists, and length is that
  // list's own. The probe is a Uint8Array wherever it is put, so here it
  // stands in for an array; no byte property of it is ever read, and that is
  // what "list" asserts.
  "trust.open#1": "list",
  "trust.parseList#0": "view",
  "trust.parseSigned#0": "view",
  "trust.tlsPin#0": "view",
  "wire.decodeFrame#0": "view",
  "wire.e2e#1": "view",
  "wire.grant#1": "view",
  "wire.parseClose#0": "view",
  "wire.parseControl#0": "view",
  "wire.parseE2E#0": "view",
  "wire.parseGrant#0": "view",
  "wire.parseOpen#0": "view",
  "wire.validParams#0": "view",
};

// The places that only exist where the engine can run a Noise_KK handshake.
// A Transport is handed two cipher states that only a finished handshake
// makes, and the handshake needs X25519: chromium has it, the WebKit
// Playwright brings answers NotSupportedError, which is why every test in
// noise.spec.js skips there. So on such an engine these ten are out of reach
// and this test says so instead of pinning a different list per engine. What
// it does not do is let them quietly disappear: on an engine with X25519 all
// ten must be reached, and on one without it none of them may be.
const NEEDS_X25519 = [
  "noise.HandshakeKK.decryptAndHash#0", "noise.HandshakeKK.encryptAndHash#0",
  "noise.HandshakeKK.forTest#1", "noise.HandshakeKK.forTest#3", "noise.HandshakeKK.forTest#4",
  "noise.HandshakeKK.mixHash#0",
  "noise.HandshakeKK.readMessage1#0", "noise.HandshakeKK.readMessage2#0",
  "noise.Transport.open#0", "noise.Transport.seal#0",
];

// How many callables the eleven modules hold, counted the way the walk below
// counts them: every exported function, every static and every method of an
// exported class, and every function that hangs off an exported object, as
// deep as three levels. The round before this one counted exported functions
// only, and an export that is no function slipped past the count as well as
// past the walk. A callable the probe cannot reach - because no filling gives
// it arguments that get that far - is still counted here, so a new one turns
// this red until somebody has said whether it takes a caller's bytes.
//
// Since the module list comes off the directory, this number is what a NEW
// CONTRACT runs into: a file added anywhere under contracts/ brings its
// callables with it and the count no longer matches. That is the one line the
// twelfth contract of the review would have had to pass, and on a list by hand
// it never reached it.
// 141 since b64.bytesOf joined the byte layer beside viewOf. It shows up in
// this count and in no list of places below, and that is correct and worth
// saying: it reads NO property of its argument at all, so the probe records
// nothing for it and there is nothing for a list of readings to hold. Which is
// also this guard's own blind spot, named where somebody will find it - a
// function that touches nothing is invisible here, and so is the shape that
// made this round necessary (an ordinary array rewritten in a microtask). What
// holds those is sign.spec.js "verifyRaw decides on a copy no getter can
// reach", which measures answers instead of touches.
//
// 147 since contracts/uitar.js: the guard counted it, it was not worked out on
// paper. Six, and the walk names them - BadPackage and walkUI, the UIWriter
// constructor and its dir, file and close. Not one of them stands in ENTRIES,
// and that is the blind spot above at its widest: walkUI takes a copy of the
// whole archive and UIWriter.file a copy of its data, both through bytesOf,
// which reads no property at all, so the probe records nothing for either.
// uitar.spec.js holds them by answers instead, the way sign.spec.js does.
const CALLABLES = 147;

// The classes whose methods the walk reaches. A method needs an instance, and
// an instance cannot be guessed: a Transport is handed two cipher states that
// only a finished handshake makes. The list is pinned and the factory below
// answers for it, so an exported class that grows methods of its own turns
// this red until somebody has written down how one of them is made.
//
// uitar.UIWriter joined it with the ui-tar contract, and it is the first entry
// that needs no crypto at all: new UIWriter() is the whole of it. What it does
// need saying is that it goes dead after its first refusal - a writer that
// refused an entry refuses everything after it, so that half an archive cannot
// go on to look like a whole one - and the walk hands every method the probe
// on ONE instance. So dir is reached and refuses the probe as a path, and file
// and close then answer "closed" without ever looking at bytes. That is why no
// place of uitar stands in ENTRIES: nothing there ever reads a property of a
// caller's object. Both places that take a caller's bytes - walkUI and
// UIWriter.file - take a copy through b64.bytesOf, which reads nothing, and
// this guard's own blind spot (named at CALLABLES) is exactly that. What holds
// them instead is uitar.spec.js "what the reader checked is what it hands out"
// and "UIWriter writes the one form walkUI reads", which rewrite the caller's
// bytes after the call and measure the answer.
const CLASSES = ["noise.HandshakeKK", "noise.Transport", "uitar.UIWriter"];
// The two of them an engine without X25519 cannot make. UIWriter is not among
// them, so it must have an instance on every engine.
const HANDSHAKE_CLASSES = ["noise.HandshakeKK", "noise.Transport"];

test("every place that takes caller bytes decides on the number it works with", async ({ shell }) => {
  test.setTimeout(180000);
  const got = await shell.evaluate(async ({ names, sizes }) => {
    const base = "/assets/js/contracts/";
    // What the page had already fetched before this test imported anything.
    // Everything that turns up under /assets/js/ from here on was pulled in by
    // the contracts, so the shell's own modules need no exception by name -
    // and a list of exceptions by name is the very thing this round took out.
    const already = new Set(
      performance.getEntriesByType("resource").map((r) => new URL(r.name, location.href).pathname),
    );
    const mods = {};
    for (const n of names) mods[n] = await import(base + n + ".js");

    // ---- what the contracts import has to stand in the list ---------------
    //
    // Two ways, and the union is asserted. The engine's own record of what it
    // fetched is exact and cannot be fooled by anything written in a comment,
    // and it sees an import() whose name is put together at run time as well;
    // the text is read on top of it for an import that no run reached.
    //
    // Both were too narrow until this round, and the same two shapes walked
    // past both. The record matched only /assets/js/contracts/<one name>.js,
    // so a module in a subfolder - whose name carries a slash - matched
    // nothing. The text matched only "./<one name>.js", so a "../frame.js" out
    // of contracts/ and a "./sub/frame.js" into one matched nothing either.
    // Measured on the stand this round started from: a contract in a subfolder
    // and a contract beside contracts/ each left the whole run green. The
    // record now reads every path under /assets/js/, and the text every
    // relative specifier, resolved against the module it stands in, so what is
    // reported is a path and not a guessed name.
    //
    // A path the page already had is skipped, unless it is a contract: a
    // contract is looked at whatever fetched it, and for everything else the
    // two checks cover each other - a static import is seen in the text even
    // where the shell had fetched the file before this test ran.
    //
    // The round before this one matched every "./x.js" anywhere in the
    // source, so naming a module in a comment turned the guard red - the kind
    // of red that gets a guard switched off. The keyword still has to stand in
    // front of the name, and the three cases were measured again for the wider
    // pattern, in chromium, each as the only change in a copy. They had to be
    // aimed somewhere else to mean anything: a zzz.js INSIDE contracts/ is on
    // the directory's list now and being imported is no finding at all, so the
    // three below put it at /assets/js/zzz.js and let wire.js reach for it.
    //
    //   a comment naming "../zzz.js"           green - 2 passed
    //   import { ZZZ } from "../zzz.js"        red, names /assets/js/zzz.js
    //   import("../" + "zzz" + ".js")          red, names /assets/js/zzz.js
    //                                          through the engine's record
    //
    // What is left is a comment that quotes a whole import line, from
    // "../x.js" and all - narrow enough to live with, and it names a module
    // that ought to be walked anyway.
    //
    // It reaches as far as /assets/js/. Code a contract took from somewhere
    // else - /assets/vendor/ - is a question for THIRD-PARTY.md and not for
    // this guard, and no contract imports any today.
    const knownPaths = new Set(names.map((n) => base + n + ".js"));
    const unlisted = new Set();
    const wanted = /^\/assets\/js\/.+\.js$/;
    const consider = (path) => {
      if (wanted.test(path) && !knownPaths.has(path)) unlisted.add(path);
    };
    for (const r of performance.getEntriesByType("resource")) {
      const path = new URL(r.name, location.href).pathname;
      if (already.has(path) && !path.startsWith(base)) continue;
      consider(path);
    }
    const written = /(?:\bfrom|\bimport)\s*\(?\s*"(\.{1,2}\/[^"\n]*?\.js)"/g;
    for (const n of names) {
      const from = new URL(base + n + ".js", location.href);
      const src = await (await fetch(from)).text();
      for (const m of src.matchAll(written)) consider(new URL(m[1], from).pathname);
    }

    // ---- the probe ------------------------------------------------------
    //
    // One buffer for every size, so a probe of 131073 bytes costs no more
    // than one of 2. The bytes are "{}" and then spaces, which is a valid
    // JSON document at every size from 2 up, so a parser gets past its budget
    // and into its own work instead of stopping at the first character.
    const POOL = new Uint8Array(Math.max(...sizes));
    POOL.fill(0x20);
    POOL[0] = 0x7b;
    POOL[1] = 0x7d;
    // One class for the whole run and one set of counters beside it, cleared
    // before each call. Making the class inside the probe cost webkit two
    // minutes for the same answer chromium gives in three and a half seconds.
    // Beside the four counters, the ORDER the four were read in, as one letter
    // each. A counter cannot tell a length read before the view from one read
    // after it, and that difference is the whole of the finding this round
    // started from: reading length runs the caller's getter, and a getter that
    // sets a flag changes the byteLength the view is built from one line later.
    // Same counters, opposite answer. So the order is read as well, and it is
    // what holds the places marked "open" below.
    let cL = 0, cB = 0, cF = 0, cO = 0, seq = "";
    class Probe extends Uint8Array {
      get length() { cL++; seq += "L"; return super.length; }
      get byteLength() { cB++; seq += "B"; return super.byteLength; }
      get buffer() { cF++; seq += "F"; return super.buffer; }
      get byteOffset() { cO++; seq += "O"; return super.byteOffset; }
    }
    const probe = (size) => {
      // Built from the pool's buffer, so nothing is copied and nothing is
      // read through the getters while it is being made.
      const a = new Probe(POOL.buffer, 0, size);
      cL = cB = cF = cO = 0;
      seq = "";
      return { a, read: () => `${cL}/${cB}/${cF}/${cO}`, order: () => seq };
    };

    // ---- what the walk finds to call ------------------------------------
    //
    // A class is called with new, and its statics and its methods are walked
    // as well: an export that is no plain function used to be walked as one,
    // f(...) without new, which throws at the first line and shows nothing.
    const isClass = (f) => /^\s*class[\s{]/.test(Function.prototype.toString.call(f));
    const CLASS_NAMES = ["noise.HandshakeKK", "noise.Transport", "uitar.UIWriter"];
    const instances = await makeInstances(mods);
    const callables = [];
    const seen = new Set();
    const add = (name, arity, call) => {
      if (seen.has(name)) throw new Error("two callables under one name: " + name);
      seen.add(name);
      callables.push({ name, arity: Math.max(arity, 1), call });
    };
    const walkObject = (name, o, depth) => {
      if (depth > 3) return;
      for (const [k, v] of Object.entries(o)) {
        if (typeof v === "function") add(`${name}.${k}`, v.length, (...a) => v.apply(o, a));
        else if (v !== null && typeof v === "object") walkObject(`${name}.${k}`, v, depth + 1);
      }
      // AND THE METHODS AN EXPORTED INSTANCE INHERITS. Object.entries gives own
      // enumerable properties only, so until this round a whole contract could
      // stand in contracts/ and be walked by nothing:
      //
      //   class Frame { seal(payload) { ...reads payload.length beside its view... } }
      //   export const frame = new Frame();
      //
      // Measured: the file in contracts/, imported by sign.js, its seal putting
      // 70000 bytes through a bound of 65519 - and this test GREEN, with the
      // server log showing the file fetched. Neither door caught it: the walk
      // saw an object with no own functions, and CALLABLES did not move because
      // nothing exported was a function or a class. That is the exact shape of
      // noise.Transport.seal, which is the one this whole file exists for.
      //
      // Only own data properties that are functions are taken. A getter is left
      // alone on purpose: reading one is running the module's code at walk
      // time, and a guard that changes what it measures by measuring it is
      // worth nothing. Object.prototype is where the chain stops.
      let proto = Object.getPrototypeOf(o);
      while (proto && proto !== Object.prototype) {
        for (const k of Object.getOwnPropertyNames(proto)) {
          if (k === "constructor") continue;
          const d = Object.getOwnPropertyDescriptor(proto, k);
          if (!d || typeof d.value !== "function") continue;
          add(`${name}.${k}`, d.value.length, (...a) => d.value.apply(o, a));
        }
        proto = Object.getPrototypeOf(proto);
      }
    };
    for (const n of names) {
      for (const [name, v] of Object.entries(mods[n])) {
        const full = `${n}.${name}`;
        if (typeof v === "function") {
          if (isClass(v)) {
            add(full, v.length, (...a) => new v(...a));
            for (const s of Object.getOwnPropertyNames(v)) {
              if (s === "length" || s === "name" || s === "prototype") continue;
              const f = v[s];
              if (typeof f === "function") add(`${full}.${s}`, f.length, (...a) => f.apply(v, a));
            }
            const methods = Object.getOwnPropertyNames(v.prototype || {}).filter((p) => p !== "constructor");
            const inst = instances[full];
            for (const p of methods) {
              const f = v.prototype[p];
              // Counted either way, called only where there is an instance: an
              // engine that cannot make one must not quietly lower the count.
              if (typeof f === "function") add(`${full}.${p}`, f.length, inst ? (...a) => f.apply(inst, a) : null);
            }
          } else {
            add(full, v.length, (...a) => v(...a));
          }
        } else if (v !== null && typeof v === "object") {
          walkObject(full, v, 1);
        }
      }
    }
    const classesWithMethods = [];
    for (const n of names) {
      for (const [name, v] of Object.entries(mods[n])) {
        if (typeof v !== "function" || !isClass(v)) continue;
        if (Object.getOwnPropertyNames(v.prototype || {}).some((p) => p !== "constructor")) {
          classesWithMethods.push(`${n}.${name}`);
        }
      }
    }
    classesWithMethods.sort();

    // ---- the fillings for the other arguments ---------------------------
    //
    // A function that stops before it ever looks at the bytes shows nothing,
    // so the bytes have to be reached: two ways, and what either one reaches
    // counts. WIDE puts one value in every other place at once; NARROW is run
    // as every combination and covers the ones that want two kinds at once -
    // json.decode wants a Budget and a shape, and no single value is both.
    // The 32- and 64-byte arrays are in WIDE because a key and a signature
    // have those sizes. Since verifyRaw builds its three views before it
    // decides anything, the probe is viewed there whatever its neighbours
    // are - but the reading that says so, the one with the length in it, only
    // happens for a key of 32 and a signature of 64, and "open" is not
    // satisfied without it.
    const BUDGET = new mods.json.Budget({ size: 1 << 20 });
    const WIDE = [undefined, null, true, 0, 1, 1024, 16384, "", "x", {}, [],
      new Uint8Array(0), new Uint8Array(2), new Uint8Array(32), new Uint8Array(64),
      BUDGET, mods.json.raw];
    const NARROW = [undefined, {}, 1024, new Uint8Array(2), new Uint8Array(32), new Uint8Array(64), BUDGET, mods.json.raw];
    // Every filling and every combination is run at these three sizes, and
    // the sweep over all the others takes the first filling that reached the
    // bytes at each of them. Three because 2 is the smallest document a
    // parser gets into, and 32 and 64 are the sizes a key and a signature
    // have - without them verifyRaw refuses on its views before it ever asks
    // either of them for a length. Taking one filling per base into the sweep
    // instead of all of them is a cost decision and it is measured: webkit
    // spends about 3 ms in each crypto.subtle.digest where chromium spends
    // 0.05, so the full cross product at every size cost webkit two minutes
    // for the same answer it now gives in 49 seconds. On chromium the whole
    // test is 1.6 seconds.
    const BASES = [2, 32, 64];

    const found = new Map();
    let calls = 0;
    const attempt = async (c, pos, args, p) => {
      calls++;
      try {
        const r = c.call(...args);
        // Everything that fails, fails silently: what is measured is the
        // reading, not the answer.
        if (r && typeof r.then === "function") { try { await r; } catch { /* no answer wanted */ } }
      } catch { /* no answer wanted */ }
      const sig = p.read();
      if (sig === "0/0/0/0") return;
      const key = `${c.name}#${pos}`;
      let e = found.get(key);
      if (!e) found.set(key, (e = { sigs: new Set(), orders: new Set() }));
      e.sigs.add(sig);
      e.orders.add(p.order());
    };
    const argsFor = (c, pos, r, a) => {
      if ("wide" in r) {
        const args = new Array(c.arity).fill(r.wide);
        args[pos] = a;
        return args;
      }
      const args = []; let q = r.narrow;
      for (let j = 0; j < c.arity; j++) {
        if (j === pos) { args.push(a); continue; }
        args.push(NARROW[q % NARROW.length]); q = Math.floor(q / NARROW.length);
      }
      return args;
    };

    for (const c of callables) {
      if (!c.call) continue;
      for (let pos = 0; pos < c.arity; pos++) {
        const recipes = [];
        for (const v of WIDE) recipes.push({ wide: v });
        // The combinations grow with the arity, so they stop at four
        // arguments; beyond that WIDE alone decides. The two callables that
        // leaves - noise.e2ePrologue and proof.verify - reach their bytes
        // through it, measured.
        const total = c.arity <= 4 ? Math.pow(NARROW.length, c.arity - 1) : 0;
        for (let k = 0; k < total; k++) recipes.push({ narrow: k });
        // Every filling and every combination, at each of the three bases:
        // this is what finds the places at all, and what the readings below
        // are read from.
        const sweep = [];
        for (const b of BASES) {
          let taken = false;
          for (const r of recipes) {
            const p = probe(b);
            const before = found.get(`${c.name}#${pos}`)?.sigs.size ?? 0;
            await attempt(c, pos, argsFor(c, pos, r, p.a), p);
            const after = found.get(`${c.name}#${pos}`)?.sigs.size ?? 0;
            if (p.read() === "0/0/0/0" && after === before) continue;
            // The first filling of this base that reached the bytes is the
            // one the sweep over the other sizes rides on.
            if (!taken && !sweep.includes(r)) { sweep.push(r); taken = true; }
          }
        }
        // All the other sizes, on those fillings. A reading that only starts
        // above some number shows up here and nowhere else.
        for (const r of sweep) {
          for (const size of sizes) {
            if (BASES.includes(size)) continue;
            const p = probe(size);
            await attempt(c, pos, argsFor(c, pos, r, p.a), p);
          }
        }
      }
    }

    const entries = {};
    for (const [k, v] of [...found].sort((a, b) => a[0].localeCompare(b[0]))) {
      entries[k] = { sigs: [...v.sigs].sort(), orders: [...v.orders].sort() };
    }
    return {
      unlisted: [...unlisted].sort(),
      entries,
      callables: callables.length,
      noInstance: CLASS_NAMES.filter((k) => !instances[k]),
      whyNoInstance: instances.failed ?? "",
      classesWithMethods,
      sizes,
      calls,
    };

    // A real Transport and a real handshake, built the way the shell builds
    // them: two parties, message 1 and 2, then split. Nothing else in the
    // tree can make a Transport, so nothing else can walk seal and open -
    // which is why they stood open with the guard green.
    async function makeInstances(m) {
      // A UIWriter needs nothing but itself, so it is made before the try: an
      // engine that runs no handshake must still walk its three methods.
      const out = { "uitar.UIWriter": new m.uitar.UIWriter() };
      try {
        const n = m.noise;
        const rnd = (k) => crypto.getRandomValues(new Uint8Array(k));
        const iPriv = rnd(32), rPriv = rnd(32);
        const PKCS8 = new Uint8Array([0x30, 0x2e, 0x02, 0x01, 0x00, 0x30, 0x05, 0x06,
          0x03, 0x2b, 0x65, 0x6e, 0x04, 0x22, 0x04, 0x20]);
        const pubOf = async (raw) => {
          const cat = new Uint8Array(PKCS8.length + raw.length);
          cat.set(PKCS8, 0); cat.set(raw, PKCS8.length);
          const k = await crypto.subtle.importKey("pkcs8", cat, { name: "X25519" }, true, ["deriveBits"]);
          return m.b64.b64decode((await crypto.subtle.exportKey("jwk", k)).x);
        };
        const iPub = await pubOf(iPriv), rPub = await pubOf(rPriv);
        const pro = m.b64.utf8(n.PROLOGUE_SESSION);
        const hi = await n.HandshakeKK.forTest(true, pro, iPriv, rPub, rnd(32));
        const hr = await n.HandshakeKK.forTest(false, pro, rPriv, iPub, rnd(32));
        await hr.readMessage1(await hi.writeMessage1());
        await hi.readMessage2(await hr.writeMessage2());
        out["noise.HandshakeKK"] = hi;
        out["noise.Transport"] = await hi.split();
        return out;
      } catch (e) {
        // No handshake, and then the pinned list of classes is what says so.
        out.failed = String(e);
        return out;
      }
    }
  }, { names: MODULES, sizes: SIZES });

  // Nothing under /assets/js/ was pulled in that this test does not walk -
  // neither a contract in a subfolder nor one beside contracts/.
  expect(got.unlisted).toEqual([]);
  // The directory gave a list, and the three modules the walk itself leans on
  // are in it. Without this a rename would make the walk quietly do less
  // instead of saying so.
  expect(MODULES).toEqual(expect.arrayContaining(NEEDED));
  // Every exported class with methods of its own has an instance to walk.
  expect(got.classesWithMethods).toEqual(CLASSES);
  // The sizes the probe was run at, said out loud: a probe that knows one
  // size checks one size, and whoever reads a green run should see how many
  // it knew.
  expect(got.sizes).toEqual(SIZES);
  // Either this engine runs a Noise_KK handshake or it runs none of it: the
  // ten places behind it are all reached or none of them is, and which it was
  // stands in the message of every failure below.
  const noHandshake = got.noInstance.length > 0;
  expect(got.noInstance).toEqual(noHandshake ? HANDSHAKE_CLASSES : []);
  const outOfReach = noHandshake ? new Set(NEEDS_X25519) : new Set();
  const where = noHandshake
    ? `this engine makes no Noise_KK handshake (${got.whyNoInstance}), so ${NEEDS_X25519.length} places are out of reach`
    : "every place is in reach on this engine";

  // The places, and what each of them may show. A place that shows anything
  // else is named with the reading it showed, so a failure reads as the list
  // of places to look at.
  const wrong = [];
  for (const [name, e] of Object.entries(got.entries)) {
    const kind = ENTRIES[name];
    if (!kind) { wrong.push(`${name}: not in the list, reads ${e.sigs.join(" ")}`); continue; }
    if (outOfReach.has(name)) { wrong.push(`${name}: reached although ${where}`); continue; }
    for (const s of e.sigs) if (!LEGAL[kind].includes(s)) wrong.push(`${name} (${kind}): reads ${s}`);
    if (kind === "copy") {
      // No view onto the caller's memory, at any size. buffer and byteOffset
      // are the two properties a view is built from, and a "copy" place must
      // never touch either: what it works on is its own. The reading above
      // already says so; this says it again as an order, so a failure names
      // the property and not just a count.
      for (const o of e.orders) {
        if (o.includes("F") || o.includes("O")) {
          wrong.push(`${name} (copy): builds a view on the caller, order ${o}`);
        }
      }
    }
    if (kind === "open") {
      // The length read has to be reached at all - otherwise "open" would be
      // a line nobody ever walked - and it has to come AFTER the view. The
      // view is finished at the first byteLength, because that is the last of
      // the three arguments viewOf builds it from; a length read before it can
      // still move what the view will be, and that is what this refuses.
      if (!e.sigs.includes(VIEW_AND_LENGTH)) {
        wrong.push(`${name} (open): its length is never read, reads ${e.sigs.join(" ")}`);
      }
      for (const o of e.orders) {
        const l = o.indexOf("L");
        const b = o.indexOf("B");
        if (l !== -1 && (b === -1 || l < b)) {
          wrong.push(`${name} (open): reads length before its view, order ${o}`);
        }
      }
    }
  }
  for (const name of Object.keys(ENTRIES)) {
    if (!got.entries[name] && !outOfReach.has(name)) {
      wrong.push(`${name}: in the list, but the probe never reached it - ${where}`);
    }
  }
  expect(wrong).toEqual([]);
  expect(Object.keys(got.entries).length).toBe(Object.keys(ENTRIES).length - outOfReach.size);
  // And every callable has been walked, reached or not.
  expect(got.callables).toBe(CALLABLES);
});

// The one thing next door does NOT do, on purpose, and why - so that nobody
// has to find this out again.
//
// The guard above holds that a place builds one view and decides on it. It
// does not hold what happens to an object whose own length disagrees with
// that view: such a length is IGNORED, not punished. The view wins and the
// call goes through on the real bytes.
//
// Making viewOf refuse instead - one line, `if (v.length !== u.length) throw`
// - was built and measured, and it is the safer side of the two. It was NOT
// taken, and the reason is the Go contract, which is frozen and decides.
//
// Measured on shared/ (a copy from git archive, nothing there touched). Go
// has no second channel like a length getter, but it has one that says the
// same thing - a slice whose capacity is larger than its length carries real
// bytes it does not report - and the contract takes that everywhere, working
// on the reported number:
//
//   enc.DecodeStrict(b, max=7)     len 7, cap 100   taken, decodes the 7
//   enc.DecodeStrict(b, max=6)     len 7, cap 100   "enc: input too large"
//   noisep Transport.Seal          len 65519, cap 66519  taken, 65535 out
//   noisep Transport.Seal          len 65520        "noisep: message too large"
//   wire.ParseGrant                len 4, cap 116   taken, credit 4096
//   task.Task.Validate             ID len 16, cap 64     taken
//   b[7] on a len-7 slice          panics - there is no second number to read
//
// So the frozen side ignores what an object holds beyond what it reports, and
// refuses on the reported number alone. This side does the same. Refusing
// here would be stricter than the contract on one side only, and two sides
// that refuse different inputs is the thing the contract exists to prevent.
// The gap is therefore fenced, not closed, and this test is the fence: it
// says out loud that the leniency is deliberate and pins it, so a later round
// cannot flip it in either direction without reading this.
//
// Four accepted tests rest on this leniency and say so where they stand:
// wire.spec.js "a payload whose length lies is refused, not half written" and
// "a document whose length lies is refused by every parser", task.spec.js
// "parseTask measures a task by one reading of its length" and "verifyApproval
// decides on one reading of the payload and decodes those very bytes".
//
// What the leniency is NOT: a way past a bound. The view is the single
// number, so a lie can only make a call work on the bytes the object really
// holds - which is what a caller who told the truth would have got. The
// eight cases below are the whole of it.
//
// AND WHAT RED MEANS HERE, because a fence that does not say so is a fence
// somebody deletes. The guard above says it for itself and this one did not.
// There are two answers and no third.
//
//   - A case answers what a REFUSAL looks like - an error text where bytes
//     were expected, a shorter output, a false where true stood. Then somebody
//     has made the byte layer fail-closed, and the question is not this test
//     but the frozen Go side above: the contract works on the reported number
//     and refuses on it, and a side that refuses more than the other is the
//     thing the contract exists to prevent. Raise it with whoever owns
//     shared/, and do not take the case out to get green.
//   - A case answers something LOOSER - more bytes than the view holds, a
//     budget that lets through what it should not, a comparison that says true
//     for bytes that differ. Then it is a finding and not a chore: the single
//     number has stopped being single somewhere, which is the whole mistake
//     this file is about.
//
// What is never the answer is changing an expected value to whatever came
// back. That is the one edit that makes this test stop holding anything, and
// it is the same edit as widening LEGAL above.
test("a length that disagrees with the view is ignored, not refused", async ({ shell }) => {
  const got = await shell.evaluate(async () => {
    const b64 = await import("/assets/js/contracts/b64.js");
    const json = await import("/assets/js/contracts/json.js");
    const out = {};
    // Seven real bytes of 7, and a length that says 32.
    const liar = () => {
      class Liar extends Uint8Array {
        get length() { return 32; }
      }
      const a = new Liar(7);
      new Uint8Array(a.buffer).fill(7);
      return a;
    };
    const truth = new Uint8Array(7).fill(7);

    // 1. viewOf answers with the real bytes and does not throw.
    const v = b64.viewOf(liar(), "x");
    out.viewLength = v.length;
    out.viewIsPlain = Object.getPrototypeOf(v) === Uint8Array.prototype;
    // 2. and the written text is the one the honest array gives.
    out.encoded = b64.b64encode(liar());
    out.encodedHonest = b64.b64encode(truth);
    // 3. a comparison sees the real bytes on either side.
    out.equalToHonest = b64.equal(liar(), truth);
    out.equalToOther = b64.equal(liar(), new Uint8Array(7).fill(8));
    // 4. concat copies the real bytes, not the claimed ones.
    out.concatLength = b64.concat(liar(), liar()).length;
    // 5. a budget is measured against the view: 7 real bytes go through a
    //    bound of 7 and not through one of 6, whatever the length claims.
    const doc = () => {
      class Liar extends Uint8Array {
        get length() { return 3; }
      }
      const a = new Liar(7);
      new Uint8Array(a.buffer).set(new TextEncoder().encode('{"a":1}'));
      return a;
    };
    const shape = json.S({ a: json.int });
    const take = (max) => {
      try {
        return JSON.stringify(json.decode(doc(), new json.Budget({ size: max }), shape));
      } catch (e) {
        return e.message;
      }
    };
    out.budget7 = take(7);
    out.budget6 = take(6);
    // 6. and the mirror of Go's capacity: bytes in the buffer beyond the view
    //    are not there for anybody, here as in Go.
    const big = new Uint8Array(100).fill(0x58);
    big.set(new TextEncoder().encode('{"a":1}'));
    const window = new Uint8Array(big.buffer, 0, 7);
    out.windowBudget7 = (() => {
      try {
        return JSON.stringify(json.decode(window, new json.Budget({ size: 7 }), shape));
      } catch (e) {
        return e.message;
      }
    })();
    return out;
  });
  // The view is the real bytes, and it is a plain array from here on.
  expect(got.viewLength).toBe(7);
  expect(got.viewIsPlain).toBe(true);
  // What is written is what an honest array of those bytes writes.
  expect(got.encoded).toBe(got.encodedHonest);
  // What is compared is the real bytes, both ways round.
  expect(got.equalToHonest).toBe(true);
  expect(got.equalToOther).toBe(false);
  // What is copied is 7 and 7, not 32 and 32.
  expect(got.concatLength).toBe(14);
  // The bound is measured on the view: 7 bytes pass a bound of 7 and fail a
  // bound of 6, exactly as enc.DecodeStrict does with max=7 and max=6.
  expect(got.budget7).toBe('{"a":1}');
  expect(got.budget6).toBe("document too large");
  // And bytes past the view are nobody's, here as in Go's capacity.
  expect(got.windowBudget7).toBe('{"a":1}');
});

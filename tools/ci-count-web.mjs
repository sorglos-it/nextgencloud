// Counts a Playwright run from its JSON report and decides red or green.
//
// Why a counter at all, and not the exit code: playwright exits 0 for a run in
// which every case switched itself off, exactly as `go test` prints `ok` for an
// all-skipped package. The exit code answers "did anything fall", never "did
// anything run". Measured on 3.11.2026 on the web suite: 22 cases on chromium,
// 1 on webkit, and both runs exit 0.
//
// Why per engine: one total hides which engine carried it. The web shell is
// measured on chromium and almost unmeasured on webkit, and a single number
// would read as three browsers when it is one.
//
// The floor is the second half of that: a run far below the expected number of
// cases is a run that did not happen, whatever its exit code says.
import { readFileSync } from "node:fs";

const file = process.argv[2];
if (!file) {
  console.error("ci-count-web: no report named");
  process.exit(2);
}

let report;
try {
  report = JSON.parse(readFileSync(file, "utf8"));
} catch (e) {
  // An unreadable report is red. It means the run died before writing one -
  // a crash, an out-of-memory, a server that never came up - and that is the
  // one case where saying nothing would look like saying "fine".
  console.error(`ci-count-web: report not readable (${e.message})`);
  process.exit(1);
}

// Per engine, and a total that is the sum of them, so the parts can be held
// against the whole.
const per = new Map();
const red = [];

// A case is named by its whole TITLE CHAIN, never by file:line.
//
// Both halves of that are measured (3.11.2026). The chain, because
// shell/engines.spec.js builds its cases out of two nested loops around ONE
// test() - two languages times three keys - so several cases share the leaf
// title AND the source line, and they differ only in the suite above them.
// Printed by leaf alone, seven red cases read as six with one of them twice,
// which is exactly the shape of a double-counting bug and is not one.
//
// And NOT file:line, because playwright's line for a case is not the line
// test() stands on: boot.spec.js has its cases on 30 and 95 and is reported
// as 28 and 105 - 105 in a file of 101 lines, so the number is not merely
// shifted, it is outside the file. engines.spec.js: 60/81/96/119/150/308
// reported as 68/91/110/141/182/353. Not the line endings (an LF copy of the
// same file gives the same wrong numbers) and not the fixtures (plain
// @playwright/test does it too). No mechanism is known, and the numbers
// differ between machines, so nothing is claimed here beyond this: a case
// header's number must not be passed on as a source line. The numbers in the
// STACK of a failure are right - those are the ones to quote.
function count(suite, titles) {
  const chain = suite.title ? [...titles, suite.title] : titles;
  for (const spec of suite.specs ?? []) {
    for (const test of spec.tests ?? []) {
      const engine = test.projectName || "?";
      const row = per.get(engine) ?? { passed: 0, failed: 0, skipped: 0, flaky: 0 };
      // status is playwright's own verdict over all retries of this case.
      if (test.status === "expected") row.passed += 1;
      else if (test.status === "skipped") row.skipped += 1;
      else if (test.status === "flaky") row.flaky += 1;
      else {
        row.failed += 1;
        // The chain already begins with the file, so it is not printed twice.
        const where = chain[0] === spec.file ? chain : [spec.file, ...chain];
        red.push(`${engine}  ${[...where, spec.title].join(" > ")}`);
      }
      per.set(engine, row);
    }
  }
  for (const inner of suite.suites ?? []) count(inner, chain);
}
for (const suite of report.suites ?? []) count(suite, []);

let passed = 0, failed = 0, skipped = 0, flaky = 0;
for (const [engine, row] of [...per].sort()) {
  console.log(
    `${engine}: ${row.passed} bestanden, ${row.failed} rot, ${row.skipped} uebersprungen` +
      (row.flaky ? `, ${row.flaky} flatternd` : ""),
  );
  passed += row.passed; failed += row.failed; skipped += row.skipped; flaky += row.flaky;
}
const total = passed + failed + skipped + flaky;
console.log(`zusammen: ${passed} bestanden, ${failed} rot, ${skipped} uebersprungen, ${flaky} flatternd, ${total} Faelle`);

if (red.length) {
  console.log("\nROT - was der Fall selbst sagt:");
  for (const line of red) console.log(`  ${line}`);
}

// A case that only passed on a retry is not a passed case. It is named, and it
// is red, because a suite that needs retries to be green hides its own fault.
if (flaky) {
  console.error(`\nci-count-web: ${flaky} Faelle wurden erst im zweiten Versuch gruen - das ist rot.`);
}

// The floor counts PASSED cases, not cases. Measured against this script on
// 3.11.2026, because a counter that was never red proves nothing: a report in
// which every case was skipped went GREEN while the floor looked at the total,
// since a skip counts towards a total. A run of 1656 skipped cases cleared a
// floor of 500 and reported nothing - the exact shape this job exists to
// catch. So: skips never carry the floor, and a run with ZERO passed cases is
// red whatever the floor says, because that one needs no number to be wrong.
const floor = Number(process.env.NGC_WEB_FLOOR || 0);
if (passed === 0) {
  console.error(`\nci-count-web: kein einziger Fall ist gelaufen (${skipped} uebersprungen) - der Lauf hat nicht stattgefunden.`);
  process.exit(1);
}
if (passed < floor) {
  console.error(`\nci-count-web: nur ${passed} bestandene Faelle von ${total}, erwartet mindestens ${floor} - der Lauf hat nicht stattgefunden.`);
  process.exit(1);
}
if (failed || flaky) {
  console.error("\nci-count-web: rot");
  process.exit(1);
}
console.log("\nci-count-web: gruen");

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

function count(suite, project) {
  const name = suite.title && suite.suites === undefined ? project : project;
  for (const spec of suite.specs ?? []) {
    for (const test of spec.tests ?? []) {
      const engine = test.projectName || name || "?";
      const row = per.get(engine) ?? { passed: 0, failed: 0, skipped: 0, flaky: 0 };
      // status is playwright's own verdict over all retries of this case.
      if (test.status === "expected") row.passed += 1;
      else if (test.status === "skipped") row.skipped += 1;
      else if (test.status === "flaky") row.flaky += 1;
      else {
        row.failed += 1;
        red.push(`${engine}  ${spec.file}:${spec.line}  ${spec.title}`);
      }
      per.set(engine, row);
    }
  }
  for (const inner of suite.suites ?? []) count(inner, name);
}
for (const suite of report.suites ?? []) count(suite, undefined);

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

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { defineConfig, devices } from "@playwright/test";

// The built-in PHP server stands in for Apache: index.php is the only entry
// point there too, and assets/ is served straight from the file system. What it
// does NOT do is put Apache's headers on those files; the mock in front of it
// does that, out of templates/apache-vhost.conf (mock/api.js, withAssetWalls).
//
// NGC_WEB_TEST_PORT moves it. Two runs of this suite at once - two sessions on
// one machine, or a measurement beside the run it is measured against - both
// bind the same socket, and the second one does not fail cleanly: it finds a
// server already answering on that port and tests the OTHER working tree
// without saying so. A port per run is the cheapest way to make that
// impossible. 8099 stays the default, so nothing that does not set it changes.
const port = Number(process.env.NGC_WEB_TEST_PORT) || 8099;

// The mock API (mock/api.js) is the server the BROWSER talks to, and the PHP
// server sits behind it on the next port: everything that is not /api/ is
// passed through, policy headers included.
//
// The task brief has it the other way round - PHP in front, passing /api/ on
// to the mock. That cannot carry the session WebSocket. PHP's built-in
// cli-server hands no socket over, so an Upgrade reaching it is answered as
// an ordinary request; and a socket to the mock's OWN port is a second
// origin, which the shell's "connect-src 'self'" refuses (both measured; see
// the task report). This way the shell sees ONE origin for page, assets,
// /api/ and the socket - which is what operation looks like - and
// public/index.php keeps its production shape with no test branch in it.
const phpPort = port + 1;

// Where php is, in the same four steps as tools/check.sh: NGC_PHP, then
// tools/local.env (not in git, one NGC_PHP=<path> line), then php from the
// PATH, and otherwise stop with an error that names NGC_PHP. No machine's own
// path stands in a file of the repository, and a test run without a server
// never looks like a test run that passed.
function phpCommand() {
  let named = process.env.NGC_PHP || "";
  let where = "NGC_PHP";
  if (!named) {
    const file = fileURLToPath(new URL("../../../tools/local.env", import.meta.url));
    try {
      const line = /^[ \t]*(?:export[ \t]+)?NGC_PHP[ \t]*=[ \t]*(.*?)[ \t]*$/m.exec(readFileSync(file, "utf8"));
      if (line) {
        named = line[1].replace(/^(["'])(.*)\1$/, "$2");
        where = "tools/local.env";
      }
    } catch {
      // No tools/local.env: the PATH is the next step.
    }
  }
  if (named) {
    if (!existsSync(named)) throw new Error(`${where} names a php that is not there: ${named}`);
    return named;
  }
  try {
    execFileSync("php", ["-v"], { stdio: "ignore" });
  } catch {
    throw new Error(
      "no php for the test server: set NGC_PHP to its path, or write NGC_PHP=<path> into tools/local.env",
    );
  }
  return "php";
}

const php = phpCommand();

export default defineConfig({
  testDir: ".",
  fullyParallel: false,
  workers: 1,
  // ONE RETRY, and it is there to make a jumpy case VISIBLE, not to hide it.
  // Measured on 18.10.2026 over four full runs of this suite (1365 cases each,
  // workers 1, so nothing here runs beside anything else): three runs ended
  // with exactly one red case, and it was a DIFFERENT case every time -
  // appframe.spec.js:250, desktop-keyboard.spec.js:382, appframe.spec.js:168,
  // the last one in firefox. None of them reproduces: run on their own they are
  // green 3 of 3, and the whole desktop-keyboard file is green. The fourth run,
  // with this line, ended `1 flaky, 933 passed, 0 red`.
  //
  // Without it every run is a coin: one slow load among 1365 cases turns the
  // suite red, and a red suite then says nothing about the tree. A LARGER
  // TIMEOUT would be the wrong answer to the same thing - it would make the
  // case pass quietly and take the evidence with it, while `flaky` is a word in
  // the summary that somebody has to read. `trace: retain-on-failure` below
  // keeps the trace of the attempt that failed, so a flaky case stays
  // diagnosable.
  //
  // What it is NOT: a licence for a case that fails twice. Two reds in a row
  // are a finding, and the cause of these three is still not named - only that
  // the retry passes (docs/bis-zur-veroeffentlichung.md, the box under H9).
  retries: 1,
  reporter: [["list"]],
  // THE LANGUAGE OF THE RUN, and it is not a nicety. Since apps/web has a
  // lookup (public/assets/js/lang.js, apps/web/lang/*.json) the shell shows
  // the language the BROWSER asks for, and Playwright's own default is
  // en-US: measured, these browsers came up en-US here and cases that pin a
  // GERMAN sentence went red on a tree whose code was right. A suite whose
  // answers change with the machine it runs on is a suite that measures the
  // machine.
  //
  // German, because German is what the cases of this suite pin and what the
  // shell is still written in outside the language files. A case that wants
  // another language says so itself: tests/lang/lookup.spec.js overrides it
  // per case to measure the other four and the fall-back, and
  // shell/busy.spec.js does the same.
  use: { baseURL: `http://127.0.0.1:${port}`, locale: "de-DE", trace: "retain-on-failure" },
  webServer: [
    {
      command: `"${php}" -d variables_order=EGPCS -S 127.0.0.1:${phpPort} -t ../public ../public/index.php`,
      url: `http://127.0.0.1:${phpPort}/config.json`,
      reuseExistingServer: false,
      env: { NGC_WEB_CONFIG: "../tests/web.json", NGC_WEB_ANCHOR: "../tests/trust-anchor.json" },
    },
    {
      command: "node mock/api.js",
      url: `http://127.0.0.1:${port}/ready`,
      reuseExistingServer: false,
      env: { NGC_MOCK_PORT: String(port), NGC_PHP_PORT: String(phpPort) },
      stdout: "pipe", stderr: "pipe",
    },
  ],
  projects: [
    { name: "chromium", use: { ...devices["Desktop Chrome"] } },
    { name: "firefox", use: { ...devices["Desktop Firefox"] } },
    { name: "webkit", use: { ...devices["Desktop Safari"] } },
  ],
});

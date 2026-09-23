import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { defineConfig, devices } from "@playwright/test";

// The built-in PHP server stands in for Apache: index.php is the only entry
// point there too, and assets/ is served straight from the file system.
const port = 8099;

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
  reporter: [["list"]],
  use: { baseURL: `http://127.0.0.1:${port}`, trace: "retain-on-failure" },
  webServer: {
    command: `"${php}" -d variables_order=EGPCS -S 127.0.0.1:${port} -t ../public ../public/index.php`,
    url: `http://127.0.0.1:${port}/config.json`,
    reuseExistingServer: false,
    env: { NGC_WEB_CONFIG: "../tests/web.json", NGC_WEB_ANCHOR: "../tests/trust-anchor.json" },
  },
  projects: [
    { name: "chromium", use: { ...devices["Desktop Chrome"] } },
    { name: "firefox", use: { ...devices["Desktop Firefox"] } },
    { name: "webkit", use: { ...devices["Desktop Safari"] } },
  ],
});

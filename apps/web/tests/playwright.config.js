import { defineConfig, devices } from "@playwright/test";

// The built-in PHP server stands in for Apache: index.php is the only entry
// point there too, and assets/ is served straight from the file system.
const port = 8099;

export default defineConfig({
  testDir: ".",
  fullyParallel: false,
  workers: 1,
  reporter: [["list"]],
  use: { baseURL: `http://127.0.0.1:${port}`, trace: "retain-on-failure" },
  webServer: {
    command: `php -d variables_order=EGPCS -S 127.0.0.1:${port} -t ../public ../public/index.php`,
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

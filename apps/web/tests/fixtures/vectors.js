import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

// The shared test data is the common truth for Go and JavaScript; the tests
// read it from the repository, never from the web root.
const dir = fileURLToPath(new URL("../../../../shared/testdata/", import.meta.url));

export function vectors(name) {
  return JSON.parse(readFileSync(dir + name, "utf8"));
}

export function raw(name) {
  return readFileSync(dir + name);
}

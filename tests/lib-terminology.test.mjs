import { test, describe } from "node:test";
import assert from "node:assert";
import { spawnSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const AGENT_LAYER_ROOT = path.resolve(__dirname, "..");

describe("terminology consistency", () => {
  test("legacy root names are removed from codebase", () => {
    // Check if rg is available
    const rgCheck = spawnSync("rg", ["--version"], { encoding: "utf8" });
    if (rgCheck.error) {
      // Skip if rg not available - this is a lint check
      console.log("  (skipped: rg not available)");
      return;
    }

    const legacyTerms = [
      "WORKING_ROOT",
      "work-root",
      "work_root",
      "AGENTLAYER_",
      "discover-root.sh",
      "temp-work-root.sh",
      "find_working_root",
    ];

    const result = spawnSync(
      "rg",
      [
        "-n",
        "-g",
        "!**/README.md",
        "-g",
        "!**/plan.md",
        "-g",
        "!**/tests/lib-terminology.test.mjs",
        "-g",
        "!**/tmp/**",
        "-e",
        legacyTerms.join("|"),
        AGENT_LAYER_ROOT,
      ],
      { encoding: "utf8" },
    );

    if (result.status === 0) {
      // Found matches - this is a failure
      assert.fail(
        `Legacy terminology found in codebase:\n${result.stdout}\n` +
          "Please update these references to use current terminology.",
      );
    }

    // Exit code 1 = no matches found (expected)
    // Exit code 2+ = error
    assert.ok(
      result.status === 1,
      `rg exited with unexpected status ${result.status}: ${result.stderr}`,
    );
  });
});

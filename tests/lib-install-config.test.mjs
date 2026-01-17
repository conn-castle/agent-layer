import { test, describe, before, after } from "node:test";
import assert from "node:assert";
import fs from "node:fs";
import path from "node:path";
import os from "node:os";
import {
  runInstallConfig,
  runWizard,
  _testing,
} from "../src/lib/install-config.mjs";

const {
  getCurrentModel,
  getCurrentReasoningEffort,
  setModel,
  setReasoningEffort,
} = _testing;

/**
 * Temporarily override TTY flags for stdin/stdout during a task.
 * @param {boolean} stdinValue
 * @param {boolean} stdoutValue
 * @param {() => Promise<void> | void} task
 * @returns {Promise<void>}
 */
function withTtyOverrides(stdinValue, stdoutValue, task) {
  const stdinHas = Object.prototype.hasOwnProperty.call(process.stdin, "isTTY");
  const stdoutHas = Object.prototype.hasOwnProperty.call(
    process.stdout,
    "isTTY",
  );
  const stdinOriginal = process.stdin.isTTY;
  const stdoutOriginal = process.stdout.isTTY;

  process.stdin.isTTY = stdinValue;
  process.stdout.isTTY = stdoutValue;

  return Promise.resolve()
    .then(task)
    .finally(() => {
      if (stdinHas) {
        process.stdin.isTTY = stdinOriginal;
      } else {
        delete process.stdin.isTTY;
      }
      if (stdoutHas) {
        process.stdout.isTTY = stdoutOriginal;
      } else {
        delete process.stdout.isTTY;
      }
    });
}

describe("src/lib/install-config.mjs", () => {
  let tmpDir;

  before(() => {
    tmpDir = fs.mkdtempSync(
      path.join(os.tmpdir(), "agent-layer-tests-install-config-"),
    );
  });

  after(() => {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  });

  test("runInstallConfig creates config files and enables agents", async () => {
    const parentRoot = path.join(tmpDir, "parent");
    const agentLayerRoot = path.join(parentRoot, ".agent-layer");
    fs.mkdirSync(agentLayerRoot, { recursive: true });

    fs.writeFileSync(path.join(agentLayerRoot, ".env.example"), "EXAMPLE=1\n");

    const templatesDir = path.join(
      agentLayerRoot,
      "config",
      "templates",
      "docs",
    );
    fs.mkdirSync(templatesDir, { recursive: true });
    const memoryFiles = [
      "ISSUES.md",
      "FEATURES.md",
      "ROADMAP.md",
      "DECISIONS.md",
      "COMMANDS.md",
    ];
    for (const name of memoryFiles) {
      fs.writeFileSync(path.join(templatesDir, name), `template-${name}\n`);
    }

    const agentsDir = path.join(agentLayerRoot, "config");
    fs.mkdirSync(agentsDir, { recursive: true });
    fs.writeFileSync(
      path.join(agentsDir, "agents.json"),
      `${JSON.stringify(
        {
          gemini: { enabled: false },
          claude: { enabled: false },
          codex: { enabled: false },
          vscode: { enabled: false },
        },
        null,
        2,
      )}\n`,
    );

    await runInstallConfig(
      { parentRoot, agentLayerRoot },
      { force: true, newInstall: true, nonInteractive: true },
    );

    assert.strictEqual(
      fs.readFileSync(path.join(agentLayerRoot, ".env"), "utf8"),
      "EXAMPLE=1\n",
    );

    for (const name of memoryFiles) {
      const content = fs.readFileSync(
        path.join(parentRoot, "docs", name),
        "utf8",
      );
      assert.strictEqual(content, `template-${name}\n`);
    }

    const launcher = fs.readFileSync(path.join(parentRoot, "al"), "utf8");
    assert.ok(launcher.includes(".agent-layer/agent-layer"));

    const gitignore = fs.readFileSync(
      path.join(parentRoot, ".gitignore"),
      "utf8",
    );
    assert.ok(gitignore.includes("# >>> agent-layer"));

    const agents = JSON.parse(
      fs.readFileSync(path.join(agentsDir, "agents.json"), "utf8"),
    );
    assert.strictEqual(agents.gemini.enabled, true);
    assert.strictEqual(agents.claude.enabled, true);
    assert.strictEqual(agents.codex.enabled, true);
    assert.strictEqual(agents.vscode.enabled, true);
  });

  test("getCurrentModel returns empty for undefined defaultArgs", () => {
    assert.strictEqual(getCurrentModel(undefined), "");
  });

  test("getCurrentModel extracts model from --model flag", () => {
    assert.strictEqual(
      getCurrentModel(["--model", "gpt-5.2-codex"]),
      "gpt-5.2-codex",
    );
  });

  test("getCurrentModel extracts model from --model= format", () => {
    assert.strictEqual(
      getCurrentModel(["--model=claude-sonnet-4-5-20250929"]),
      "claude-sonnet-4-5-20250929",
    );
  });

  test("getCurrentModel returns empty when model not set", () => {
    assert.strictEqual(getCurrentModel(["--other", "value"]), "");
  });

  test("getCurrentReasoningEffort returns empty for undefined defaultArgs", () => {
    assert.strictEqual(getCurrentReasoningEffort(undefined), "");
  });

  test("getCurrentReasoningEffort extracts effort from --config flag", () => {
    assert.strictEqual(
      getCurrentReasoningEffort(["--config", 'model_reasoning_effort="high"']),
      "high",
    );
  });

  test("getCurrentReasoningEffort extracts unquoted effort", () => {
    assert.strictEqual(
      getCurrentReasoningEffort(["--config", "model_reasoning_effort=medium"]),
      "medium",
    );
  });

  test("getCurrentReasoningEffort returns empty for other config values", () => {
    assert.strictEqual(
      getCurrentReasoningEffort(["--config", "other_setting=value"]),
      "",
    );
  });

  test("setModel adds model to empty args", () => {
    const result = setModel(undefined, "gpt-5.2-codex");
    assert.deepStrictEqual(result, ["--model", "gpt-5.2-codex"]);
  });

  test("setModel replaces existing model", () => {
    const result = setModel(
      ["--model", "old-model", "--other", "value"],
      "new-model",
    );
    assert.deepStrictEqual(result, [
      "--other",
      "value",
      "--model",
      "new-model",
    ]);
  });

  test("setModel removes model when empty string provided", () => {
    const result = setModel(
      ["--model", "gpt-5.2-codex", "--other", "value"],
      "",
    );
    assert.deepStrictEqual(result, ["--other", "value"]);
  });

  test("setModel handles --model= format removal", () => {
    const result = setModel(
      ["--model=old-model", "--other", "value"],
      "new-model",
    );
    assert.deepStrictEqual(result, [
      "--other",
      "value",
      "--model",
      "new-model",
    ]);
  });

  test("setReasoningEffort adds effort to empty args", () => {
    const result = setReasoningEffort(undefined, "high");
    assert.deepStrictEqual(result, ["--config", "model_reasoning_effort=high"]);
  });

  test("setReasoningEffort replaces existing effort", () => {
    const result = setReasoningEffort(
      ["--config", 'model_reasoning_effort="low"', "--model", "gpt-5.2-codex"],
      "xhigh",
    );
    assert.deepStrictEqual(result, [
      "--model",
      "gpt-5.2-codex",
      "--config",
      "model_reasoning_effort=xhigh",
    ]);
  });

  test("setReasoningEffort removes effort when empty string provided", () => {
    const result = setReasoningEffort(
      ["--config", 'model_reasoning_effort="high"', "--model", "gpt-5.2-codex"],
      "",
    );
    assert.deepStrictEqual(result, ["--model", "gpt-5.2-codex"]);
  });

  test("setReasoningEffort preserves other config entries", () => {
    const result = setReasoningEffort(
      [
        "--config",
        "other_setting=value",
        "--config",
        'model_reasoning_effort="low"',
      ],
      "high",
    );
    assert.deepStrictEqual(result, [
      "--config",
      "other_setting=value",
      "--config",
      "model_reasoning_effort=high",
    ]);
  });

  test("runWizard requires a TTY on stdout", async () => {
    await withTtyOverrides(true, false, async () => {
      await assert.rejects(
        runWizard({ parentRoot: "/tmp", agentLayerRoot: "/tmp" }),
        /requires an interactive terminal/,
      );
    });
  });
});

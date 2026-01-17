import { test, describe, before, after } from "node:test";
import assert from "node:assert";
import fs from "node:fs";
import path from "node:path";
import os from "node:os";
import {
  loadAgentConfig,
  validateAgentConfig,
} from "../src/lib/agent-config.mjs";

describe("src/lib/agent-config.mjs", () => {
  let tmpDir;
  let configDir;
  let agentLayerRoot;

  before(() => {
    tmpDir = fs.mkdtempSync(
      path.join(os.tmpdir(), "agent-layer-tests-config-"),
    );
    agentLayerRoot = tmpDir;
    configDir = path.join(agentLayerRoot, "config");
    fs.mkdirSync(configDir, { recursive: true });
  });

  after(() => {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  });

  const writeConfig = (obj) => {
    fs.writeFileSync(
      path.join(configDir, "agents.json"),
      JSON.stringify(obj, null, 2),
    );
  };

  test("loadAgentConfig loads valid full config", () => {
    const valid = {
      gemini: { enabled: true, defaultArgs: ["--model", "gemini-pro"] },
      claude: { enabled: false },
      codex: { enabled: true },
      vscode: { enabled: false },
    };
    writeConfig(valid);
    const res = loadAgentConfig(agentLayerRoot);
    assert.deepStrictEqual(res, valid);
  });

  test("loadAgentConfig throws if file missing", () => {
    fs.rmSync(path.join(configDir, "agents.json"), { force: true });
    assert.throws(() => loadAgentConfig(agentLayerRoot), /not found/);
  });

  test("validateAgentConfig throws on non-object", () => {
    assert.throws(
      () => validateAgentConfig("string", "test"),
      /must contain a JSON object/,
    );
  });

  test("validateAgentConfig throws on missing agents", () => {
    const invalid = { gemini: { enabled: true } }; // Missing others
    assert.throws(
      () => validateAgentConfig(invalid, "test"),
      /missing required agents/,
    );
  });

  test("validateAgentConfig throws on unknown agents", () => {
    const invalid = {
      gemini: { enabled: true },
      claude: { enabled: false },
      codex: { enabled: false },
      vscode: { enabled: false },
      unknown: { enabled: true },
    };
    assert.throws(
      () => validateAgentConfig(invalid, "test"),
      /contains unknown agents/,
    );
  });

  test("validateAgentConfig throws if agent entry is not object", () => {
    const invalid = {
      gemini: "true",
      claude: { enabled: false },
      codex: { enabled: false },
      vscode: { enabled: false },
    };
    assert.throws(
      () => validateAgentConfig(invalid, "test"),
      /gemini must be an object/,
    );
  });

  test("validateAgentConfig throws if enabled is not boolean", () => {
    const invalid = {
      gemini: { enabled: "yes" },
      claude: { enabled: false },
      codex: { enabled: false },
      vscode: { enabled: false },
    };
    assert.throws(
      () => validateAgentConfig(invalid, "test"),
      /gemini.enabled must be boolean/,
    );
  });

  test("validateAgentConfig throws if defaultArgs is not array", () => {
    const invalid = {
      gemini: { enabled: true, defaultArgs: "bad" },
      claude: { enabled: false },
      codex: { enabled: false },
      vscode: { enabled: false },
    };
    assert.throws(
      () => validateAgentConfig(invalid, "test"),
      /gemini.defaultArgs must be an array/,
    );
  });

  test("validateAgentConfig throws on invalid defaultArgs content", () => {
    const base = {
      gemini: { enabled: true },
      claude: { enabled: false },
      codex: { enabled: false },
      vscode: { enabled: false },
    };

    // Newlines
    let invalid = JSON.parse(JSON.stringify(base));
    invalid.gemini.defaultArgs = ["--arg\nnewline"];
    assert.throws(
      () => validateAgentConfig(invalid, "test"),
      /must not contain newlines/,
    );

    // "--" literal
    invalid = JSON.parse(JSON.stringify(base));
    invalid.gemini.defaultArgs = ["--"];
    assert.throws(
      () => validateAgentConfig(invalid, "test"),
      /must not be "--"/,
    );

    // Value without flag
    invalid = JSON.parse(JSON.stringify(base));
    invalid.gemini.defaultArgs = ["value-without-flag"];
    assert.throws(
      () => validateAgentConfig(invalid, "test"),
      /must follow a --flag/,
    );

    // Valid cases
    invalid = JSON.parse(JSON.stringify(base));
    invalid.gemini.defaultArgs = ["--flag", "value", "--flag2=value"];
    validateAgentConfig(invalid, "test"); // Should not throw
  });
});

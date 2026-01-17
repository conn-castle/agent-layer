import { test, describe, before, after } from "node:test";
import assert from "node:assert";
import fs from "node:fs";
import path from "node:path";
import os from "node:os";
import { removeGeneratedArtifacts } from "../src/lib/cleanup.mjs";

describe("src/lib/cleanup.mjs", () => {
  let tmpRoot;

  before(() => {
    tmpRoot = fs.mkdtempSync(
      path.join(os.tmpdir(), "agent-layer-tests-cleanup-"),
    );
  });

  after(() => {
    fs.rmSync(tmpRoot, { recursive: true, force: true });
  });

  test("removeGeneratedArtifacts removes known files", () => {
    // Setup: regular generated files are always removed
    const regularFiles = [
      "AGENTS.md",
      "CLAUDE.md",
      ".github/copilot-instructions.md",
    ];

    // MCP config files are only removed if they're empty JSON objects
    const mcpFiles = [".mcp.json", ".vscode/mcp.json"];

    for (const f of regularFiles) {
      const p = path.join(tmpRoot, f);
      fs.mkdirSync(path.dirname(p), { recursive: true });
      fs.writeFileSync(p, "test");
    }

    for (const f of mcpFiles) {
      const p = path.join(tmpRoot, f);
      fs.mkdirSync(path.dirname(p), { recursive: true });
      fs.writeFileSync(p, "{}"); // Empty JSON object
    }

    // Execute
    const res = removeGeneratedArtifacts(tmpRoot);

    // Verify: regular files removed
    for (const f of regularFiles) {
      const p = path.join(tmpRoot, f);
      assert.ok(!fs.existsSync(p), `File ${f} should be removed`);
    }

    // Verify: empty MCP config files removed
    for (const f of mcpFiles) {
      const p = path.join(tmpRoot, f);
      assert.ok(!fs.existsSync(p), `Empty MCP file ${f} should be removed`);
    }

    assert.ok(res.removed.length >= regularFiles.length + mcpFiles.length);
  });

  test("removeGeneratedArtifacts preserves MCP config with custom entries", () => {
    const mcpPath = path.join(tmpRoot, ".mcp-custom.json");
    fs.writeFileSync(mcpPath, '{"mcpServers":{"custom":{}}}');

    // Create and run cleanup with a file that has content
    const testDir = path.join(tmpRoot, "mcp-preserve-test");
    fs.mkdirSync(testDir, { recursive: true });
    const testMcpPath = path.join(testDir, ".mcp.json");
    fs.writeFileSync(testMcpPath, '{"mcpServers":{"my-custom-server":{}}}');

    removeGeneratedArtifacts(testDir);

    // File with custom entries should be preserved
    assert.ok(
      fs.existsSync(testMcpPath),
      "MCP file with custom entries should be preserved",
    );
  });

  test("removeGeneratedArtifacts cleans skills directory", () => {
    const skillDir = path.join(tmpRoot, ".codex", "skills", "test-skill");
    fs.mkdirSync(skillDir, { recursive: true });
    fs.writeFileSync(path.join(skillDir, "SKILL.md"), "content");

    removeGeneratedArtifacts(tmpRoot);

    assert.ok(!fs.existsSync(path.join(skillDir, "SKILL.md")));
    // cleanup removes empty dirs too
    assert.ok(!fs.existsSync(skillDir));
  });

  test("removeGeneratedArtifacts handles prompts", () => {
    const promptDir = path.join(tmpRoot, ".vscode", "prompts");
    fs.mkdirSync(promptDir, { recursive: true });

    // Generated file
    const generated = path.join(promptDir, "gen.prompt.md");
    const genContent =
      "<!-- GENERATED FILE - DO NOT EDIT DIRECTLY -->\n<!-- Regenerate: ./al --sync -->";
    fs.writeFileSync(generated, genContent);

    // User file
    const user = path.join(promptDir, "user.prompt.md");
    fs.writeFileSync(user, "User content");

    removeGeneratedArtifacts(tmpRoot);

    assert.ok(!fs.existsSync(generated), "Generated prompt should be removed");
    assert.ok(fs.existsSync(user), "User prompt should remain");
  });
});

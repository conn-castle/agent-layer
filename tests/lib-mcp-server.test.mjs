import { test, describe, before } from "node:test";
import assert from "node:assert";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const AGENT_LAYER_ROOT = path.resolve(__dirname, "..");
const SERVER_FILE = path.join(
  AGENT_LAYER_ROOT,
  "src",
  "mcp",
  "agent-layer-prompts",
  "server.mjs",
);

describe("MCP prompt server", () => {
  let serverContent;

  before(() => {
    serverContent = fs.readFileSync(SERVER_FILE, "utf8");
  });

  test("server file exists", () => {
    assert.ok(fs.existsSync(SERVER_FILE), "server.mjs should exist");
  });

  test("exposes tools/list handler", () => {
    assert.ok(
      serverContent.includes("ListToolsRequestSchema"),
      "Should import ListToolsRequestSchema",
    );
    assert.ok(
      serverContent.includes("setRequestHandler(ListToolsRequestSchema"),
      "Should register ListToolsRequestSchema handler",
    );
    assert.match(
      serverContent,
      /capabilities:.*tools/s,
      "Should declare tools capability",
    );
  });

  test("fails fast when workflows are missing", () => {
    assert.ok(
      serverContent.includes("missing workflows directory"),
      "Should have error message for missing workflows directory",
    );
    assert.ok(
      serverContent.includes("no workflow files found"),
      "Should have error message for no workflow files",
    );
  });
});

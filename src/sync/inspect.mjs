#!/usr/bin/env node
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";
import {
  collectApprovalDivergences,
  collectMcpDivergences,
} from "./divergence.mjs";
import { loadServerCatalog } from "./mcp.mjs";
import { loadCommandPolicy } from "./policy.mjs";
import { resolveWorkingRoot } from "./paths.mjs";
import { fileExists } from "./utils.mjs";

/**
 * Write JSON output and exit.
 * @param {unknown} payload
 * @param {number} code
 */
function output(payload, code = 0) {
  process.stdout.write(`${JSON.stringify(payload, null, 2)}\n`);
  process.exit(code);
}

/**
 * Write JSON error output and exit.
 * @param {string} message
 * @param {number} code
 */
function outputError(message, code = 2) {
  output({ ok: false, error: message }, code);
}

/**
 * Entrypoint.
 */
function main() {
  const scriptDir = path.dirname(fileURLToPath(import.meta.url));
  const workingRoot = resolveWorkingRoot(process.cwd(), scriptDir);
  if (!workingRoot || !fileExists(path.join(workingRoot, ".agent-layer"))) {
    outputError(
      "agent-layer inspect: could not find working repo root containing .agent-layer/",
    );
  }

  const agentlayerRoot = path.join(workingRoot, ".agent-layer");
  const policy = loadCommandPolicy(agentlayerRoot);
  const catalog = loadServerCatalog(agentlayerRoot);
  const approvals = collectApprovalDivergences(workingRoot, policy);
  const mcp = collectMcpDivergences(workingRoot, catalog);
  const divergences = {
    approvals: approvals.items,
    mcp: mcp.items,
    notes: [...approvals.notes, ...mcp.notes],
  };

  output({
    ok: true,
    generatedAt: new Date().toISOString(),
    workingRoot,
    summary: {
      approvals: divergences.approvals.length,
      mcp: divergences.mcp.length,
      total: divergences.approvals.length + divergences.mcp.length,
    },
    guidance: {
      approvals:
        "Add approvals to .agent-layer/config/policy/commands.json, then run: node .agent-layer/src/sync/sync.mjs",
      mcp: "Add MCP servers to .agent-layer/config/mcp-servers.json, then run: node .agent-layer/src/sync/sync.mjs",
    },
    divergences: {
      approvals: divergences.approvals,
      mcp: divergences.mcp,
    },
    notes: divergences.notes,
  });
}

try {
  main();
} catch (err) {
  outputError(
    `agent-layer inspect: ${err instanceof Error ? err.message : String(err)}`,
    1,
  );
}

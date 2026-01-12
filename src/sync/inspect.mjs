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
import { resolveRootsFromEnvOrScript } from "./paths.mjs";
import { fileExists } from "./utils.mjs";

/**
 * CLI: emit a JSON report of divergence between generated outputs and sources.
 */

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
  // Resolve the parent root and validate the .agent-layer directory.
  const entryPath = process.argv[1] ?? fileURLToPath(import.meta.url);
  const roots = resolveRootsFromEnvOrScript(entryPath);
  if (!roots) {
    outputError(
      "agent-layer inspect: PARENT_ROOT must be set when running outside an installed .agent-layer.",
    );
  }
  const parentRoot = path.resolve(roots.parentRoot);
  const agentLayerRoot = path.resolve(roots.agentLayerRoot);
  if (!fileExists(agentLayerRoot)) {
    outputError(
      "agent-layer inspect: could not find .agent-layer directory for this command.",
    );
  }

  // Load source configs and collect divergence details.
  const policy = loadCommandPolicy(agentLayerRoot);
  const catalog = loadServerCatalog(agentLayerRoot);
  const approvals = collectApprovalDivergences(parentRoot, policy);
  const mcp = collectMcpDivergences(parentRoot, catalog);
  const divergences = {
    approvals: approvals.items,
    mcp: mcp.items,
    notes: [...approvals.notes, ...mcp.notes],
  };

  // Emit a structured JSON report for downstream tooling.
  output({
    ok: true,
    generatedAt: new Date().toISOString(),
    parentRoot,
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

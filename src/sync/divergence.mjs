import {
  collectApprovalDivergences,
  collectMcpDivergences,
} from "./divergence-collectors.mjs";

/**
 * @typedef {import("./divergence-utils.mjs").ApprovalItem} ApprovalItem
 * @typedef {import("./divergence-utils.mjs").McpItem} McpItem
 * @typedef {import("./divergence-utils.mjs").DivergenceResult} DivergenceResult
 */

/**
 * Collect divergent approvals + MCP entries for warning output.
 * @param {string} parentRoot
 * @param {import("./policy.mjs").CommandPolicy} policy
 * @param {{ defaults?: Record<string, unknown>, servers?: unknown[] }} catalog
 * @param {Set<string>=} enabledAgents
 * @param {string} agentLayerRoot
 * @returns {DivergenceResult}
 */
export function collectDivergences(
  parentRoot,
  policy,
  catalog,
  enabledAgents,
  agentLayerRoot,
) {
  const approvals = collectApprovalDivergences(
    parentRoot,
    policy,
    enabledAgents,
  );
  const mcp = collectMcpDivergences(
    parentRoot,
    catalog,
    enabledAgents,
    agentLayerRoot,
  );
  return {
    approvals: approvals.items,
    mcp: mcp.items,
    notes: [...approvals.notes, ...mcp.notes],
  };
}

/**
 * Format a simple warning for divergent configs.
 * @param {DivergenceResult} result
 * @returns {string}
 */
export function formatDivergenceWarning(result) {
  const parts = [];
  if (result.approvals.length)
    parts.push(`approvals: ${result.approvals.length}`);
  if (result.mcp.length) parts.push(`mcp: ${result.mcp.length}`);
  const detail = parts.length ? ` (${parts.join(", ")})` : "";
  const detailLines = [];
  if (result.approvals.length) {
    detailLines.push(`- approvals: ${result.approvals.length}`);
  }
  if (result.mcp.length) {
    detailLines.push(`- mcp: ${result.mcp.length}`);
  }
  return [
    `agent-layer sync: WARNING: client configs diverge from .agent-layer sources${detail}.`,
    "",
    "Details:",
    ...detailLines,
    "",
    "Notes:",
    "- Sync preserves existing client entries by default; it will not overwrite them unless you pass --overwrite or choose overwrite in --interactive.",
    "",
    "Next steps:",
    "- Run: ./al --inspect (JSON report)",
    "- Add them to .agent-layer/config/policy/commands.json or .agent-layer/config/mcp-servers.json, then re-run sync",
    "- Or re-run with: ./al --sync --overwrite (discard client-only entries)",
    "- Or re-run with: ./al --sync --interactive (review and choose)",
  ].join("\n");
}

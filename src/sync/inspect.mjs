import path from "node:path";
import { fileURLToPath } from "node:url";
import {
  collectApprovalDivergences,
  collectMcpDivergences,
} from "./divergence-collectors.mjs";
import {
  SUPPORTED_AGENTS,
  getEnabledAgents,
  loadAgentConfig,
} from "../lib/agent-config.mjs";
import { loadServerCatalog } from "./mcp.mjs";
import { loadCommandPolicy } from "./policy.mjs";
import { fileExists } from "./utils.mjs";

export const INSPECT_USAGE = [
  "Usage:",
  "  ./al --inspect [--parent-root <path>] [--temp-parent-root] [--agent-layer-root <path>]",
].join("\n");

/**
 * Build a JSON report of divergence between generated outputs and sources.
 * @param {string} parentRoot
 * @param {string} agentLayerRoot
 * @returns {Record<string, unknown>}
 */
export function buildInspectReport(parentRoot, agentLayerRoot) {
  if (
    !parentRoot ||
    !agentLayerRoot ||
    typeof parentRoot !== "string" ||
    typeof agentLayerRoot !== "string"
  ) {
    throw new Error(
      "agent-layer inspect: parentRoot and agentLayerRoot are required.",
    );
  }
  const resolvedParent = path.resolve(parentRoot);
  const resolvedAgentLayer = path.resolve(agentLayerRoot);
  if (!fileExists(resolvedParent)) {
    throw new Error(
      `agent-layer inspect: parent root does not exist: ${resolvedParent}`,
    );
  }
  if (!fileExists(resolvedAgentLayer)) {
    throw new Error(
      "agent-layer inspect: could not find .agent-layer directory for this command.",
    );
  }

  // Load source configs and collect divergence details.
  const policy = loadCommandPolicy(resolvedAgentLayer);
  const catalog = loadServerCatalog(resolvedAgentLayer);
  const agentConfig = loadAgentConfig(resolvedAgentLayer);
  const enabledAgents = getEnabledAgents(agentConfig);
  const disabledAgents = SUPPORTED_AGENTS.filter(
    (name) => !enabledAgents.has(name),
  );
  const approvals = collectApprovalDivergences(
    resolvedParent,
    policy,
    enabledAgents,
  );
  const mcp = collectMcpDivergences(
    resolvedParent,
    catalog,
    enabledAgents,
    resolvedAgentLayer,
  );
  const notes = [...approvals.notes, ...mcp.notes];
  if (disabledAgents.length) {
    const enabledList = Array.from(enabledAgents).join(", ") || "none";
    notes.push(
      `inspect filtered to enabled agents (${enabledList}); disabled agents: ${disabledAgents.join(", ")}.`,
    );
  }
  const divergences = {
    approvals: approvals.items,
    mcp: mcp.items,
    notes,
  };

  return {
    ok: true,
    generatedAt: new Date().toISOString(),
    parentRoot: resolvedParent,
    summary: {
      approvals: divergences.approvals.length,
      mcp: divergences.mcp.length,
      total: divergences.approvals.length + divergences.mcp.length,
    },
    guidance: {
      approvals:
        "Add approvals to .agent-layer/config/policy/commands.json, then run: ./al --sync",
      mcp: "Add MCP servers to .agent-layer/config/mcp-servers.json, then run: ./al --sync",
    },
    divergences: {
      approvals: divergences.approvals,
      mcp: divergences.mcp,
    },
    notes: divergences.notes,
  };
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  console.error("agent-layer inspect: use ./al --inspect");
  process.exit(2);
}

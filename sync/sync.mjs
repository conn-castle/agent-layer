#!/usr/bin/env node
/**
 * Agent Layer sync (Node-based generator)
 *
 * Generates per-client shim files from `.agent-layer/instructions/` sources:
 * - AGENTS.md
 * - .codex/AGENTS.md
 * - CLAUDE.md
 * - GEMINI.md
 * - .github/copilot-instructions.md
 *
 * Generates per-client MCP configuration from `.agent-layer/mcp/servers.json`:
 * - .mcp.json              (Claude Code)
 * - .gemini/settings.json  (Gemini CLI)
 * - .vscode/mcp.json       (VS Code / Copilot Chat)
 * - .codex/config.toml     (Codex CLI / VS Code extension)
 *
 * Generates Codex Skills from `.agent-layer/workflows/*.md`:
 * - .codex/skills/<workflow>/SKILL.md
 *
 * Generates per-client command allowlists from `.agent-layer/policy/commands.json`:
 * - .gemini/settings.json
 * - .claude/settings.json
 * - .vscode/settings.json
 * - .codex/rules/agent-layer.rules
 *
 * Usage:
 *   node .agent-layer/sync/sync.mjs
 *   node .agent-layer/sync/sync.mjs --check
 *   node .agent-layer/sync/sync.mjs --verbose
 */

import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";
import { REGEN_COMMAND } from "./constants.mjs";
import { banner, concatInstructions } from "./instructions.mjs";
import {
  buildMcpConfigs,
  loadServerCatalog,
  renderCodexConfig,
  trustedServerNames,
} from "./mcp.mjs";
import { diffOrWrite } from "./outdated.mjs";
import { resolveWorkingRoot } from "./paths.mjs";
import {
  buildClaudeAllow,
  buildGeminiAllowed,
  buildVscodeAutoApprove,
  commandPrefixes,
  loadCommandPolicy,
  mergeClaudeSettings,
  mergeGeminiSettings,
  mergeVscodeSettings,
  renderCodexRules,
} from "./policy.mjs";
import { generateCodexSkills } from "./skills.mjs";
import { fileExists, readJsonRelaxed } from "./utils.mjs";

/**
 * Print usage and exit.
 * @param {number} code
 * @returns {void}
 */
function usageAndExit(code) {
  console.error(`Usage: ${REGEN_COMMAND} [--check] [--verbose]`);
  process.exit(code);
}

/**
 * Parse CLI arguments.
 * @param {string[]} argv
 * @returns {{ check: boolean, verbose: boolean }}
 */
function parseArgs(argv) {
  const args = { check: false, verbose: false };
  for (const a of argv.slice(2)) {
    if (a === "--check") args.check = true;
    else if (a === "--verbose") args.verbose = true;
    else if (a === "-h" || a === "--help") usageAndExit(0);
    else usageAndExit(2);
  }
  return args;
}

/**
 * Entry point.
 * @returns {void}
 */
function main() {
  const args = parseArgs(process.argv);
  const scriptDir = path.dirname(fileURLToPath(import.meta.url));
  const workingRoot = resolveWorkingRoot(process.cwd(), scriptDir);
  if (!workingRoot || !fileExists(path.join(workingRoot, ".agent-layer"))) {
    console.error("agent-layer sync: could not find working repo root containing .agent-layer/");
    process.exit(2);
  }

  const agentlayerRoot = path.join(workingRoot, ".agent-layer");
  const instructionsDir = path.join(agentlayerRoot, "instructions");
  const workflowsDir = path.join(agentlayerRoot, "workflows");

  const policy = loadCommandPolicy(agentlayerRoot);
  const prefixes = commandPrefixes(policy);
  const geminiAllowed = buildGeminiAllowed(prefixes);
  const claudeAllowed = buildClaudeAllow(prefixes);
  const vscodeAutoApprove = buildVscodeAutoApprove(prefixes);

  const unified =
    banner(".agent-layer/instructions/*.md", REGEN_COMMAND) +
    concatInstructions(instructionsDir);

  const outputs = [
    [path.join(workingRoot, "AGENTS.md"), unified],
    [path.join(workingRoot, ".codex", "AGENTS.md"), unified],
    [path.join(workingRoot, "CLAUDE.md"), unified],
    [path.join(workingRoot, "GEMINI.md"), unified],
    [path.join(workingRoot, ".github", "copilot-instructions.md"), unified],
  ];

  const catalog = loadServerCatalog(agentlayerRoot);
  const mcpConfigs = buildMcpConfigs(catalog);
  const trustedServers = trustedServerNames(catalog);
  const claudeMcpAllowed = trustedServers.map((name) => `mcp__${name}__*`);
  const claudeAllowPatterns = [...new Set([...claudeAllowed, ...claudeMcpAllowed])];
  const codexConfig = renderCodexConfig(catalog, REGEN_COMMAND);
  outputs.push(
    [
      path.join(workingRoot, ".vscode", "mcp.json"),
      JSON.stringify(mcpConfigs.vscode, null, 2) + "\n",
    ],
    [path.join(workingRoot, ".mcp.json"), JSON.stringify(mcpConfigs.claude, null, 2) + "\n"],
    [path.join(workingRoot, ".codex", "config.toml"), codexConfig]
  );

  const geminiSettingsPath = path.join(workingRoot, ".gemini", "settings.json");
  const geminiExisting = readJsonRelaxed(geminiSettingsPath, {});
  const geminiMerged = mergeGeminiSettings(
    geminiExisting,
    /** @type {{ mcpServers: Record<string, unknown> }} */ (mcpConfigs.gemini),
    geminiAllowed,
    geminiSettingsPath
  );
  outputs.push([geminiSettingsPath, JSON.stringify(geminiMerged, null, 2) + "\n"]);

  const claudeSettingsPath = path.join(workingRoot, ".claude", "settings.json");
  const claudeExisting = readJsonRelaxed(claudeSettingsPath, {});
  const claudeMerged = mergeClaudeSettings(
    claudeExisting,
    claudeAllowPatterns,
    claudeSettingsPath
  );
  outputs.push([claudeSettingsPath, JSON.stringify(claudeMerged, null, 2) + "\n"]);

  const vscodeSettingsPath = path.join(workingRoot, ".vscode", "settings.json");
  const vscodeExisting = readJsonRelaxed(vscodeSettingsPath, {});
  const vscodeMerged = mergeVscodeSettings(
    vscodeExisting,
    vscodeAutoApprove,
    vscodeSettingsPath
  );
  outputs.push([vscodeSettingsPath, JSON.stringify(vscodeMerged, null, 2) + "\n"]);

  const codexRulesPath = path.join(workingRoot, ".codex", "rules", "agent-layer.rules");
  outputs.push([codexRulesPath, renderCodexRules(policy.allowed)]);

  diffOrWrite(outputs, args, workingRoot);
  generateCodexSkills(workingRoot, workflowsDir, args);

  if (!args.check) {
    console.log("agent-layer sync: updated shims + MCP configs + allowlists + Codex skills");
  }
}

main();

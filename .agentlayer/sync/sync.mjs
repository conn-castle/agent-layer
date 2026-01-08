#!/usr/bin/env node
/**
 * Agentlayer sync (Node-based generator)
 *
 * Generates per-client shim files from `.agentlayer/` sources:
 * - AGENTS.md
 * - CLAUDE.md
 * - GEMINI.md
 * - .github/copilot-instructions.md
 *
 * Generates per-client MCP configuration from `.agentlayer/mcp/servers.json`:
 * - .mcp.json              (Claude Code)
 * - .gemini/settings.json  (Gemini CLI)
 * - .vscode/mcp.json       (VS Code / Copilot Chat)
 *
 * Generates Codex Skills from `.agentlayer/workflows/*.md`:
 * - .codex/skills/<workflow>/SKILL.md
 *
 * Generates per-client command allowlists from `.agentlayer/policy/commands.json`:
 * - .gemini/settings.json
 * - .claude/settings.json
 * - .vscode/settings.json
 * - .codex/rules/agentlayer.rules
 *
 * Usage:
 *   node .agentlayer/sync/sync.mjs
 *   node .agentlayer/sync/sync.mjs --check
 *   node .agentlayer/sync/sync.mjs --verbose
 */

import path from "node:path";
import process from "node:process";
import { REGEN_COMMAND } from "./constants.mjs";
import { banner, concatInstructions } from "./instructions.mjs";
import { buildMcpConfigs, loadServerCatalog } from "./mcp.mjs";
import { diffOrWrite } from "./outdated.mjs";
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
 * Find the repo root containing .agentlayer.
 * @param {string} startDir
 * @returns {string | null}
 */
function findRepoRoot(startDir) {
  let dir = path.resolve(startDir);
  for (let i = 0; i < 50; i++) {
    if (fileExists(path.join(dir, ".agentlayer"))) return dir;
    const parent = path.dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }
  return null;
}

/**
 * Entry point.
 * @returns {void}
 */
function main() {
  const args = parseArgs(process.argv);
  const repoRoot = findRepoRoot(process.cwd());
  if (!repoRoot) {
    console.error("agentlayer sync: could not find repo root containing .agentlayer/");
    process.exit(2);
  }

  const instructionsDir = path.join(repoRoot, ".agentlayer", "instructions");
  const workflowsDir = path.join(repoRoot, ".agentlayer", "workflows");

  const policy = loadCommandPolicy(repoRoot);
  const prefixes = commandPrefixes(policy);
  const geminiAllowed = buildGeminiAllowed(prefixes);
  const claudeAllowed = buildClaudeAllow(prefixes);
  const vscodeAutoApprove = buildVscodeAutoApprove(prefixes);

  const unified =
    banner(".agentlayer/instructions/*.md", REGEN_COMMAND) +
    concatInstructions(instructionsDir);

  const outputs = [
    [path.join(repoRoot, "AGENTS.md"), unified],
    [path.join(repoRoot, "CLAUDE.md"), unified],
    [path.join(repoRoot, "GEMINI.md"), unified],
    [path.join(repoRoot, ".github", "copilot-instructions.md"), unified],
  ];

  const catalog = loadServerCatalog(repoRoot);
  const mcpConfigs = buildMcpConfigs(catalog);
  outputs.push(
    [
      path.join(repoRoot, ".vscode", "mcp.json"),
      JSON.stringify(mcpConfigs.vscode, null, 2) + "\n",
    ],
    [path.join(repoRoot, ".mcp.json"), JSON.stringify(mcpConfigs.claude, null, 2) + "\n"]
  );

  const geminiSettingsPath = path.join(repoRoot, ".gemini", "settings.json");
  const geminiExisting = readJsonRelaxed(geminiSettingsPath, {});
  const geminiMerged = mergeGeminiSettings(
    geminiExisting,
    /** @type {{ mcpServers: Record<string, unknown> }} */ (mcpConfigs.gemini),
    geminiAllowed,
    geminiSettingsPath
  );
  outputs.push([geminiSettingsPath, JSON.stringify(geminiMerged, null, 2) + "\n"]);

  const claudeSettingsPath = path.join(repoRoot, ".claude", "settings.json");
  const claudeExisting = readJsonRelaxed(claudeSettingsPath, {});
  const claudeMerged = mergeClaudeSettings(claudeExisting, claudeAllowed, claudeSettingsPath);
  outputs.push([claudeSettingsPath, JSON.stringify(claudeMerged, null, 2) + "\n"]);

  const vscodeSettingsPath = path.join(repoRoot, ".vscode", "settings.json");
  const vscodeExisting = readJsonRelaxed(vscodeSettingsPath, {});
  const vscodeMerged = mergeVscodeSettings(
    vscodeExisting,
    vscodeAutoApprove,
    vscodeSettingsPath
  );
  outputs.push([vscodeSettingsPath, JSON.stringify(vscodeMerged, null, 2) + "\n"]);

  const codexRulesPath = path.join(repoRoot, ".codex", "rules", "agentlayer.rules");
  outputs.push([codexRulesPath, renderCodexRules(policy.allowed)]);

  diffOrWrite(outputs, args, repoRoot);
  generateCodexSkills(repoRoot, workflowsDir, args);

  if (!args.check) {
    console.log("agentlayer sync: updated shims + MCP configs + allowlists + Codex skills");
  }
}

main();

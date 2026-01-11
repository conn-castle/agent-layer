#!/usr/bin/env node
/**
 * Agent Layer sync (Node-based generator)
 *
 * Generates per-client shim files from `.agent-layer/config/instructions/` sources:
 * - AGENTS.md
 * - .codex/AGENTS.md
 * - CLAUDE.md
 * - GEMINI.md
 * - .github/copilot-instructions.md
 *
 * Generates per-client MCP configuration from `.agent-layer/config/mcp-servers.json`:
 * - .mcp.json              (Claude Code)
 * - .gemini/settings.json  (Gemini CLI)
 * - .vscode/mcp.json       (VS Code / Copilot Chat)
 * - .codex/config.toml     (Codex CLI / VS Code extension)
 *
 * Generates Codex Skills from `.agent-layer/config/workflows/*.md`:
 * - .codex/skills/<workflow>/SKILL.md
 *
 * Generates per-client command allowlists from `.agent-layer/config/policy/commands.json`:
 * - .gemini/settings.json
 * - .claude/settings.json
 * - .vscode/settings.json
 * - .codex/rules/default.rules
 *
 * Usage:
 *   node .agent-layer/src/sync/sync.mjs
 *   node .agent-layer/src/sync/sync.mjs --check
 *   node .agent-layer/src/sync/sync.mjs --verbose
 *   node .agent-layer/src/sync/sync.mjs --overwrite
 *   node .agent-layer/src/sync/sync.mjs --interactive
 *   node .agent-layer/src/sync/sync.mjs --codex
 */

import fs from "node:fs";
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
  mergeCodexRules,
  renderCodexRules,
} from "./policy.mjs";
import { generateCodexSkills } from "./skills.mjs";
import {
  fileExists,
  isPlainObject,
  readJsonRelaxed,
  readUtf8,
} from "./utils.mjs";
import { collectDivergences, formatDivergenceWarning } from "./divergence.mjs";
import { parseCodexConfigSections } from "./codex-config.mjs";
import { promptDivergenceAction } from "./ui.mjs";

/**
 * Print usage and exit.
 * @param {number} code
 * @returns {void}
 */
function usageAndExit(code) {
  console.error(
    `Usage: ${REGEN_COMMAND} [--check] [--verbose] [--overwrite] [--interactive] [--codex]`,
  );
  process.exit(code);
}

/**
 * Parse CLI arguments.
 * @param {string[]} argv
 * @returns {{ check: boolean, verbose: boolean, overwrite: boolean, interactive: boolean, codex: boolean }}
 */
function parseArgs(argv) {
  const args = {
    check: false,
    verbose: false,
    overwrite: false,
    interactive: false,
    codex: false,
  };
  for (const a of argv.slice(2)) {
    if (a === "--check") args.check = true;
    else if (a === "--verbose") args.verbose = true;
    else if (a === "--overwrite") args.overwrite = true;
    else if (a === "--interactive") args.interactive = true;
    else if (a === "--codex") args.codex = true;
    else if (a === "-h" || a === "--help") usageAndExit(0);
    else usageAndExit(2);
  }
  if (args.overwrite && args.interactive) {
    console.error(
      "agent-layer sync: choose only one of --overwrite or --interactive.",
    );
    usageAndExit(2);
  }
  if (args.check && args.interactive) {
    console.error(
      "agent-layer sync: --interactive cannot be used with --check.",
    );
    usageAndExit(2);
  }
  return args;
}

/**
 * Enforce repo-local CODEX_HOME when running Codex.
 * @param {string} workingRoot
 * @param {string|undefined} codexHome
 * @returns {void}
 */
function enforceCodexHome(workingRoot, codexHome) {
  const trimmed = (codexHome ?? "").trim();
  if (!trimmed) return;
  if (!path.isAbsolute(trimmed)) {
    throw new Error(
      "agent-layer sync: CODEX_HOME must be an absolute path when running ./al codex.",
    );
  }

  const expectedPath = path.resolve(workingRoot, ".codex");
  const codexPath = path.resolve(trimmed);
  if (!fileExists(trimmed)) {
    if (codexPath !== expectedPath) {
      throw new Error(
        "agent-layer sync: CODEX_HOME must point to the repo-local .codex directory when running ./al codex.",
      );
    }
    return;
  }

  const stats = fs.statSync(trimmed);
  if (!stats.isDirectory()) {
    throw new Error(
      "agent-layer sync: CODEX_HOME must point to the repo-local .codex directory when running ./al codex.",
    );
  }

  const codexReal = fs.realpathSync(trimmed);
  let expectedReal = expectedPath;
  if (fileExists(expectedPath)) {
    expectedReal = fs.realpathSync(expectedPath);
  }
  if (codexReal !== expectedReal) {
    throw new Error(
      "agent-layer sync: CODEX_HOME must point to the repo-local .codex directory when running ./al codex.",
    );
  }
}

/**
 * Compare JSON-compatible values for deep equality (object key order ignored).
 * @param {unknown} a
 * @param {unknown} b
 * @returns {boolean}
 */
function jsonDeepEqual(a, b) {
  if (a === b) return true;
  if (typeof a !== typeof b) return false;
  if (typeof a === "number") {
    if (Number.isNaN(a) && Number.isNaN(b)) return true;
  }
  if (a === null || b === null) return false;

  if (Array.isArray(a) || Array.isArray(b)) {
    if (!Array.isArray(a) || !Array.isArray(b)) return false;
    if (a.length !== b.length) return false;
    for (let i = 0; i < a.length; i++) {
      if (!jsonDeepEqual(a[i], b[i])) return false;
    }
    return true;
  }

  if (isPlainObject(a) && isPlainObject(b)) {
    const keysA = Object.keys(a);
    const keysB = Object.keys(b);
    if (keysA.length !== keysB.length) return false;
    for (const key of keysA) {
      if (!Object.prototype.hasOwnProperty.call(b, key)) return false;
      if (!jsonDeepEqual(a[key], b[key])) return false;
    }
    return true;
  }

  return false;
}

/**
 * Merge an MCP config object, preserving existing server entries when they differ.
 * @param {unknown} existing
 * @param {Record<string, unknown>} generated
 * @param {string} key
 * @returns {Record<string, unknown>}
 */
function mergeMcpConfig(existing, generated, key, options = {}) {
  const merged = isPlainObject(existing) ? { ...existing } : {};
  const generatedServers = isPlainObject(generated[key]) ? generated[key] : {};

  if (options.overwrite) {
    merged[key] = generatedServers;
    return merged;
  }

  const existingServers = isPlainObject(merged[key]) ? merged[key] : {};
  const mergedServers = {};
  for (const [name, entry] of Object.entries(generatedServers)) {
    const existingEntry = existingServers[name];
    if (
      isPlainObject(existingEntry) &&
      !jsonDeepEqual(existingEntry, entry)
    ) {
      mergedServers[name] = existingEntry;
    } else {
      mergedServers[name] = entry;
    }
  }

  for (const [name, entry] of Object.entries(existingServers)) {
    if (!Object.prototype.hasOwnProperty.call(mergedServers, name)) {
      mergedServers[name] = entry;
    }
  }

  merged[key] = mergedServers;
  return merged;
}

/**
 * Merge Codex config.toml sections, preserving existing sections when they differ.
 * @param {string|null} existingContent
 * @param {string} generatedContent
 * @returns {string}
 */
function mergeCodexConfig(existingContent, generatedContent, options = {}) {
  if (options.overwrite) return generatedContent;
  if (!existingContent) return generatedContent;
  const existing = parseCodexConfigSections(existingContent);
  const generated = parseCodexConfigSections(generatedContent);
  const mergedSections = new Map();

  for (const [name, lines] of generated.sections.entries()) {
    const existingLines = existing.sections.get(name);
    if (existingLines && existingLines.join("\n") !== lines.join("\n")) {
      mergedSections.set(name, existingLines);
    } else {
      mergedSections.set(name, lines);
    }
  }

  for (const [name, lines] of existing.sections.entries()) {
    if (!mergedSections.has(name)) mergedSections.set(name, lines);
  }

  const generatedHeader = generated.header.join("\n");
  const existingHeader = existing.header.join("\n");
  const header =
    generatedHeader && generatedHeader === existingHeader
      ? generated.header
      : existing.header.length
        ? existing.header
        : generated.header;
  const out = header.slice();
  for (const lines of mergedSections.values()) {
    const lastLine = out[out.length - 1];
    const nextHeader = lines.find((line) => line.trim() !== "");
    const skipSpacer =
      typeof lastLine === "string" &&
      lastLine.trim().startsWith("#") &&
      typeof nextHeader === "string" &&
      nextHeader.trim().startsWith("[mcp_servers.");
    if (out.length && lastLine !== "" && !skipSpacer) out.push("");
    out.push(...lines);
  }

  while (out.length && out[out.length - 1] === "") out.pop();
  return out.join("\n") + "\n";
}

/**
 * Entry point.
 * @returns {void}
 */
async function main() {
  let args = parseArgs(process.argv);
  const scriptDir = path.dirname(fileURLToPath(import.meta.url));
  const workingRoot = resolveWorkingRoot(process.cwd(), scriptDir);
  if (!workingRoot || !fileExists(path.join(workingRoot, ".agent-layer"))) {
    console.error(
      "agent-layer sync: could not find working repo root containing .agent-layer/",
    );
    process.exit(2);
  }
  if (args.codex) {
    enforceCodexHome(workingRoot, process.env.CODEX_HOME);
  }

  const agentlayerRoot = path.join(workingRoot, ".agent-layer");
  const instructionsDir = path.join(agentlayerRoot, "config", "instructions");
  const workflowsDir = path.join(agentlayerRoot, "config", "workflows");

  const policy = loadCommandPolicy(agentlayerRoot);
  const prefixes = commandPrefixes(policy);
  const geminiAllowed = buildGeminiAllowed(prefixes);
  const claudeAllowed = buildClaudeAllow(prefixes);
  const vscodeAutoApprove = buildVscodeAutoApprove(prefixes);

  const catalog = loadServerCatalog(agentlayerRoot);
  const divergence = collectDivergences(workingRoot, policy, catalog);
  const hasDivergence = divergence.approvals.length || divergence.mcp.length;
  if (hasDivergence) {
    if (args.interactive) {
      const action = await promptDivergenceAction(divergence, workingRoot);
      if (action === "overwrite") {
        args = { ...args, overwrite: true, interactive: false };
      } else {
        console.error(
          "agent-layer sync: divergence not resolved. Update Agent Layer and re-run sync.",
        );
        process.exit(1);
      }
    } else {
      console.warn(formatDivergenceWarning(divergence));
      if (divergence.notes.length) {
        divergence.notes.forEach((note) => console.warn(`note: ${note}`));
      }
    }
  } else if (divergence.notes.length) {
    divergence.notes.forEach((note) =>
      console.warn(`agent-layer sync: WARNING: ${note}`),
    );
  }
  if (args.overwrite && hasDivergence) {
    console.warn(
      "agent-layer sync: overwriting client configs to match Agent Layer sources.",
    );
  }

  const unified =
    banner(".agent-layer/config/instructions/*.md", REGEN_COMMAND) +
    concatInstructions(instructionsDir);

  const outputs = [
    [path.join(workingRoot, "AGENTS.md"), unified],
    [path.join(workingRoot, ".codex", "AGENTS.md"), unified],
    [path.join(workingRoot, "CLAUDE.md"), unified],
    [path.join(workingRoot, "GEMINI.md"), unified],
    [path.join(workingRoot, ".github", "copilot-instructions.md"), unified],
  ];

  const mcpConfigs = buildMcpConfigs(catalog);
  const trustedServers = trustedServerNames(catalog);
  const claudeMcpAllowed = trustedServers.map((name) => `mcp__${name}__*`);
  const claudeAllowPatterns = [
    ...new Set([...claudeAllowed, ...claudeMcpAllowed]),
  ];
  const codexConfig = renderCodexConfig(catalog, REGEN_COMMAND);
  const vscodeMcpPath = path.join(workingRoot, ".vscode", "mcp.json");
  const vscodeMcpExisting = readJsonRelaxed(vscodeMcpPath, {});
  const vscodeMcpMerged = mergeMcpConfig(
    vscodeMcpExisting,
    mcpConfigs.vscode,
    "servers",
    { overwrite: args.overwrite },
  );

  const claudeMcpPath = path.join(workingRoot, ".mcp.json");
  const claudeMcpExisting = readJsonRelaxed(claudeMcpPath, {});
  const claudeMcpMerged = mergeMcpConfig(
    claudeMcpExisting,
    mcpConfigs.claude,
    "mcpServers",
    { overwrite: args.overwrite },
  );

  const codexConfigPath = path.join(workingRoot, ".codex", "config.toml");
  const codexExisting = fileExists(codexConfigPath)
    ? readUtf8(codexConfigPath)
    : null;
  const codexMerged = mergeCodexConfig(codexExisting, codexConfig, {
    overwrite: args.overwrite,
  });

  outputs.push(
    [vscodeMcpPath, JSON.stringify(vscodeMcpMerged, null, 2) + "\n"],
    [claudeMcpPath, JSON.stringify(claudeMcpMerged, null, 2) + "\n"],
    [codexConfigPath, codexMerged],
  );

  const geminiSettingsPath = path.join(workingRoot, ".gemini", "settings.json");
  const geminiExisting = readJsonRelaxed(geminiSettingsPath, {});
  const geminiMerged = mergeGeminiSettings(
    geminiExisting,
    /** @type {{ mcpServers: Record<string, unknown> }} */ (mcpConfigs.gemini),
    geminiAllowed,
    geminiSettingsPath,
    { overwrite: args.overwrite },
  );
  outputs.push([
    geminiSettingsPath,
    JSON.stringify(geminiMerged, null, 2) + "\n",
  ]);

  const claudeSettingsPath = path.join(workingRoot, ".claude", "settings.json");
  const claudeExisting = readJsonRelaxed(claudeSettingsPath, {});
  const claudeMerged = mergeClaudeSettings(
    claudeExisting,
    claudeAllowPatterns,
    claudeSettingsPath,
    { overwrite: args.overwrite },
  );
  outputs.push([
    claudeSettingsPath,
    JSON.stringify(claudeMerged, null, 2) + "\n",
  ]);

  const vscodeSettingsPath = path.join(workingRoot, ".vscode", "settings.json");
  const vscodeExisting = readJsonRelaxed(vscodeSettingsPath, {});
  const vscodeMerged = mergeVscodeSettings(
    vscodeExisting,
    vscodeAutoApprove,
    vscodeSettingsPath,
    { overwrite: args.overwrite },
  );
  outputs.push([
    vscodeSettingsPath,
    JSON.stringify(vscodeMerged, null, 2) + "\n",
  ]);

  const codexRulesPath = path.join(
    workingRoot,
    ".codex",
    "rules",
    "default.rules",
  );
  const codexRulesGenerated = renderCodexRules(policy.allowed);
  const codexRulesExisting = fileExists(codexRulesPath)
    ? readUtf8(codexRulesPath)
    : null;
  const codexRulesMerged = codexRulesExisting
    ? mergeCodexRules(codexRulesExisting, codexRulesGenerated, {
        overwrite: args.overwrite,
      })
    : codexRulesGenerated;
  outputs.push([codexRulesPath, codexRulesMerged]);

  diffOrWrite(outputs, args, workingRoot);
  generateCodexSkills(workingRoot, workflowsDir, args);

  if (!args.check) {
    console.log(
      "agent-layer sync: updated shims + MCP configs + allowlists + Codex skills",
    );
  }
}

main().catch((err) => {
  console.error(
    `agent-layer sync: ${err instanceof Error ? err.message : String(err)}`,
  );
  process.exit(1);
});

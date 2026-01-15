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
  enabledServers,
  loadServerCatalog,
  renderCodexConfig,
  trustedServerNames,
} from "./mcp.mjs";
import { diffOrWrite } from "./outdated.mjs";
import { resolveRootsFromEnvOrScript } from "./paths.mjs";
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
import { generateVscodePrompts } from "./prompts.mjs";
import {
  fileExists,
  isPlainObject,
  readJsonRelaxed,
  readUtf8,
} from "./utils.mjs";
import { collectDivergences, formatDivergenceWarning } from "./divergence.mjs";
import { parseCodexConfigSections } from "./codex-config.mjs";
import { promptDivergenceAction } from "./ui.mjs";
import { getEnabledAgents, loadAgentConfig } from "../lib/agent-config.mjs";

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
 * Resolve parent and agent-layer roots, honoring explicit env overrides.
 * @param {string} scriptDir
 * @returns {{ parentRoot: string, agentLayerRoot: string }}
 */
function resolveRoots(entryPath) {
  const roots = resolveRootsFromEnvOrScript(entryPath);
  if (!roots) {
    console.error(
      "agent-layer sync: PARENT_ROOT must be set when running outside an installed .agent-layer.",
    );
    console.error(
      "agent-layer sync: run via ./al or set PARENT_ROOT/AGENT_LAYER_ROOT.",
    );
    process.exit(2);
  }
  const parentRoot = path.resolve(roots.parentRoot);
  const agentLayerRoot = path.resolve(roots.agentLayerRoot);
  if (!fileExists(parentRoot)) {
    console.error(
      `agent-layer sync: PARENT_ROOT does not exist: ${parentRoot}`,
    );
    process.exit(2);
  }
  if (!fileExists(agentLayerRoot)) {
    console.error(
      `agent-layer sync: AGENT_LAYER_ROOT does not exist: ${agentLayerRoot}`,
    );
    process.exit(2);
  }
  return { parentRoot, agentLayerRoot };
}

/**
 * Enforce repo-local CODEX_HOME when running Codex.
 * @param {string} parentRoot
 * @param {string|undefined} codexHome
 * @returns {void}
 */
function enforceCodexHome(parentRoot, codexHome) {
  const trimmed = (codexHome ?? "").trim();
  if (!trimmed) return;
  if (!path.isAbsolute(trimmed)) {
    throw new Error(
      "agent-layer sync: CODEX_HOME must be an absolute path when running ./al codex.",
    );
  }

  const expectedPath = path.resolve(parentRoot, ".codex");
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
 * Warn when outputs exist for disabled agents.
 * @param {string} parentRoot
 * @param {Set<string>} enabledAgents
 * @returns {void}
 */
function warnDisabledOutputs(parentRoot, enabledAgents) {
  const toRel = (p) => path.relative(parentRoot, p).split(path.sep).join("/");
  const warnings = [];

  const check = (agent, paths) => {
    if (enabledAgents.has(agent)) return;
    const existing = paths.filter((p) => fileExists(p));
    if (!existing.length) return;
    warnings.push({ agent, existing });
  };

  check("gemini", [
    path.join(parentRoot, "GEMINI.md"),
    path.join(parentRoot, ".gemini", "settings.json"),
  ]);
  check("claude", [
    path.join(parentRoot, "CLAUDE.md"),
    path.join(parentRoot, ".claude", "settings.json"),
    path.join(parentRoot, ".mcp.json"),
  ]);
  check("codex", [
    path.join(parentRoot, ".codex", "AGENTS.md"),
    path.join(parentRoot, ".codex", "config.toml"),
    path.join(parentRoot, ".codex", "rules", "default.rules"),
    path.join(parentRoot, ".codex", "skills"),
  ]);
  check("vscode", [
    path.join(parentRoot, ".github", "copilot-instructions.md"),
    path.join(parentRoot, ".vscode", "mcp.json"),
    path.join(parentRoot, ".vscode", "settings.json"),
    path.join(parentRoot, ".vscode", "prompts"),
  ]);

  if (!warnings.length) return;

  for (const warn of warnings) {
    const rels = warn.existing.map(toRel).join(", ");
    console.warn(
      `agent-layer sync: WARNING: ${warn.agent} is disabled in .agent-layer/config/agents.json, but outputs exist: ${rels}`,
    );
    console.warn(
      "agent-layer sync: To remove them, run ./clean.sh or delete them manually. To re-enable, update config/agents.json and re-run ./al --sync.",
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
 * @param {Set<string>} managedServers
 * @returns {Record<string, unknown>}
 */
function mergeMcpConfig(
  existing,
  generated,
  key,
  options = {},
  managedServers,
) {
  const merged = isPlainObject(existing) ? { ...existing } : {};
  const generatedServers = isPlainObject(generated[key]) ? generated[key] : {};
  const generatedInputs = Array.isArray(generated.inputs)
    ? generated.inputs
    : null;

  /**
   * Merge VS Code MCP inputs by id, preserving existing entries.
   * @param {unknown} existingInputs
   * @param {Record<string, unknown>[]} generatedInputsList
   * @returns {unknown[]}
   */
  function mergeMcpInputs(existingInputs, generatedInputsList) {
    const existingList = Array.isArray(existingInputs) ? existingInputs : [];
    if (!existingList.length) return generatedInputsList;
    if (!generatedInputsList.length) return existingList;
    const merged = [];
    const seenIds = new Set();
    const generatedById = new Map();

    for (const entry of generatedInputsList) {
      if (isPlainObject(entry) && typeof entry.id === "string") {
        const id = entry.id.trim();
        if (id) generatedById.set(id, entry);
      }
    }

    const addEntry = (entry) => {
      if (isPlainObject(entry) && typeof entry.id === "string") {
        const id = entry.id.trim();
        if (id) {
          if (seenIds.has(id)) return;
          seenIds.add(id);
          const generatedEntry = generatedById.get(id);
          if (generatedEntry && jsonDeepEqual(entry, generatedEntry)) {
            merged.push(generatedEntry);
            return;
          }
        }
      }
      merged.push(entry);
    };

    for (const entry of existingList) addEntry(entry);
    for (const entry of generatedInputsList) {
      if (isPlainObject(entry) && typeof entry.id === "string") {
        const id = entry.id.trim();
        if (id) {
          if (seenIds.has(id)) continue;
          seenIds.add(id);
        }
      }
      merged.push(entry);
    }
    return merged;
  }

  if (options.overwrite) {
    merged[key] = generatedServers;
    if (generatedInputs) {
      merged.inputs = generatedInputs;
    } else if (Object.prototype.hasOwnProperty.call(merged, "inputs")) {
      delete merged.inputs;
    }
    return merged;
  }

  const existingServers = isPlainObject(merged[key]) ? merged[key] : {};
  const mergedServers = {};
  const generatedNames = new Set(Object.keys(generatedServers));
  const managed = managedServers ?? new Set();

  for (const [name, entry] of Object.entries(generatedServers)) {
    const existingEntry = existingServers[name];
    if (isPlainObject(existingEntry) && !jsonDeepEqual(existingEntry, entry)) {
      mergedServers[name] = existingEntry;
    } else {
      mergedServers[name] = entry;
    }
  }

  for (const [name, entry] of Object.entries(existingServers)) {
    if (Object.prototype.hasOwnProperty.call(mergedServers, name)) continue;
    if (managed.has(name) && !generatedNames.has(name)) continue;
    mergedServers[name] = entry;
  }

  merged[key] = mergedServers;
  if (generatedInputs) {
    merged.inputs = mergeMcpInputs(merged.inputs, generatedInputs);
  }
  return merged;
}

/**
 * Merge Codex config.toml sections, preserving existing sections when they differ.
 * @param {string|null} existingContent
 * @param {string} generatedContent
 * @param {Set<string>} managedServers
 * @returns {string}
 */
function mergeCodexConfig(
  existingContent,
  generatedContent,
  options = {},
  managedServers,
) {
  if (options.overwrite) return generatedContent;
  if (!existingContent) return generatedContent;
  const existing = parseCodexConfigSections(existingContent);
  const generated = parseCodexConfigSections(generatedContent);
  const mergedSections = new Map();
  const managed = managedServers ?? new Set();
  const generatedNames = new Set(generated.sections.keys());

  for (const [name, lines] of generated.sections.entries()) {
    const existingLines = existing.sections.get(name);
    if (existingLines && existingLines.join("\n") !== lines.join("\n")) {
      mergedSections.set(name, existingLines);
    } else {
      mergedSections.set(name, lines);
    }
  }

  for (const [name, lines] of existing.sections.entries()) {
    if (mergedSections.has(name)) continue;
    if (managed.has(name) && !generatedNames.has(name)) continue;
    mergedSections.set(name, lines);
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
  // Parse arguments and resolve the parent repo root.
  let args = parseArgs(process.argv);
  const entryPath = process.argv[1] ?? fileURLToPath(import.meta.url);
  const { parentRoot, agentLayerRoot } = resolveRoots(entryPath);
  // Enforce CODEX_HOME when sync runs for Codex.
  if (args.codex) {
    enforceCodexHome(parentRoot, process.env.CODEX_HOME);
  }

  // Resolve config source directories relative to the repo root.
  const instructionsDir = path.join(agentLayerRoot, "config", "instructions");
  const workflowsDir = path.join(agentLayerRoot, "config", "workflows");

  // Load agent config to determine enabled outputs.
  const agentConfig = loadAgentConfig(agentLayerRoot);
  const enabledAgents = getEnabledAgents(agentConfig);
  const geminiEnabled = enabledAgents.has("gemini");
  const claudeEnabled = enabledAgents.has("claude");
  const codexEnabled = enabledAgents.has("codex");
  const vscodeEnabled = enabledAgents.has("vscode");

  // Load policy and build per-client allowlists.
  const policy = loadCommandPolicy(agentLayerRoot);
  const prefixes = commandPrefixes(policy);
  const geminiAllowed = buildGeminiAllowed(prefixes);
  const claudeAllowed = buildClaudeAllow(prefixes);
  const vscodeAutoApprove = buildVscodeAutoApprove(prefixes);

  // Load MCP catalog and handle any divergence warnings or prompts.
  const catalog = loadServerCatalog(agentLayerRoot);
  const divergence = collectDivergences(
    parentRoot,
    policy,
    catalog,
    enabledAgents,
  );
  const hasDivergence = divergence.approvals.length || divergence.mcp.length;
  if (hasDivergence) {
    if (args.interactive) {
      const action = await promptDivergenceAction(divergence, parentRoot);
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

  // Build the unified instructions output shared across clients.
  const unified =
    banner(".agent-layer/config/instructions/*.md", REGEN_COMMAND) +
    concatInstructions(instructionsDir);

  // Seed the outputs list with instruction shims.
  const outputs = [[path.join(parentRoot, "AGENTS.md"), unified]];
  if (codexEnabled) {
    outputs.push([path.join(parentRoot, ".codex", "AGENTS.md"), unified]);
  }
  if (claudeEnabled) {
    outputs.push([path.join(parentRoot, "CLAUDE.md"), unified]);
  }
  if (geminiEnabled) {
    outputs.push([path.join(parentRoot, "GEMINI.md"), unified]);
  }
  if (vscodeEnabled) {
    outputs.push([
      path.join(parentRoot, ".github", "copilot-instructions.md"),
      unified,
    ]);
  }

  // Build MCP configs and merge with existing client settings.
  const mcpConfigs = buildMcpConfigs(catalog);
  const managedServerNames = new Set();
  for (const server of enabledServers(catalog.servers ?? [])) {
    if (server && typeof server.name === "string") {
      managedServerNames.add(server.name);
    }
  }
  let claudeAllowPatterns = null;
  if (claudeEnabled) {
    const trustedServers = trustedServerNames(catalog, "claude");
    const claudeMcpAllowed = trustedServers.map((name) => `mcp__${name}__*`);
    claudeAllowPatterns = [...new Set([...claudeAllowed, ...claudeMcpAllowed])];
  }
  const codexConfig = codexEnabled
    ? renderCodexConfig(catalog, REGEN_COMMAND)
    : null;
  if (vscodeEnabled) {
    const vscodeMcpPath = path.join(parentRoot, ".vscode", "mcp.json");
    const vscodeMcpExisting = readJsonRelaxed(vscodeMcpPath, {});
    const vscodeMcpMerged = mergeMcpConfig(
      vscodeMcpExisting,
      mcpConfigs.vscode,
      "servers",
      { overwrite: args.overwrite },
      managedServerNames,
    );
    outputs.push([
      vscodeMcpPath,
      JSON.stringify(vscodeMcpMerged, null, 2) + "\n",
    ]);
  }

  if (claudeEnabled) {
    const claudeMcpPath = path.join(parentRoot, ".mcp.json");
    const claudeMcpExisting = readJsonRelaxed(claudeMcpPath, {});
    const claudeMcpMerged = mergeMcpConfig(
      claudeMcpExisting,
      mcpConfigs.claude,
      "mcpServers",
      { overwrite: args.overwrite },
      managedServerNames,
    );
    outputs.push([
      claudeMcpPath,
      JSON.stringify(claudeMcpMerged, null, 2) + "\n",
    ]);
  }

  if (codexEnabled) {
    const codexConfigPath = path.join(parentRoot, ".codex", "config.toml");
    const codexExisting = fileExists(codexConfigPath)
      ? readUtf8(codexConfigPath)
      : null;
    const codexMerged = mergeCodexConfig(
      codexExisting,
      codexConfig,
      {
        overwrite: args.overwrite,
      },
      managedServerNames,
    );
    outputs.push([codexConfigPath, codexMerged]);
  }

  // Merge Gemini settings, preserving non-managed entries.
  if (geminiEnabled) {
    const geminiSettingsPath = path.join(
      parentRoot,
      ".gemini",
      "settings.json",
    );
    const geminiExisting = readJsonRelaxed(geminiSettingsPath, {});
    const geminiMerged = mergeGeminiSettings(
      geminiExisting,
      /** @type {{ mcpServers: Record<string, unknown> }} */ (
        mcpConfigs.gemini
      ),
      geminiAllowed,
      geminiSettingsPath,
      { overwrite: args.overwrite, managedServers: managedServerNames },
    );
    outputs.push([
      geminiSettingsPath,
      JSON.stringify(geminiMerged, null, 2) + "\n",
    ]);
  }

  // Merge Claude settings, preserving non-managed entries.
  if (claudeEnabled) {
    const claudeSettingsPath = path.join(
      parentRoot,
      ".claude",
      "settings.json",
    );
    const claudeExisting = readJsonRelaxed(claudeSettingsPath, {});
    const claudeMerged = mergeClaudeSettings(
      claudeExisting,
      claudeAllowPatterns ?? [],
      claudeSettingsPath,
      { overwrite: args.overwrite },
    );
    outputs.push([
      claudeSettingsPath,
      JSON.stringify(claudeMerged, null, 2) + "\n",
    ]);
  }

  // Merge VS Code settings, preserving non-managed entries.
  if (vscodeEnabled) {
    const vscodeSettingsPath = path.join(
      parentRoot,
      ".vscode",
      "settings.json",
    );
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
  }

  // Render and merge Codex rules for command policy enforcement.
  if (codexEnabled) {
    const codexRulesPath = path.join(
      parentRoot,
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
  }

  warnDisabledOutputs(parentRoot, enabledAgents);

  // Write or diff outputs and regenerate Codex skills/prompts.
  diffOrWrite(outputs, args, parentRoot);
  if (codexEnabled) {
    generateCodexSkills(parentRoot, workflowsDir, args);
  }
  if (vscodeEnabled) {
    generateVscodePrompts(parentRoot, workflowsDir, args);
  }

  // Emit a success summary unless running in --check mode.
  if (!args.check) {
    console.log(
      "agent-layer sync: updated shims + MCP configs + allowlists + Codex skills + VS Code prompts",
    );
  }
}

main().catch((err) => {
  console.error(
    `agent-layer sync: ${err instanceof Error ? err.message : String(err)}`,
  );
  process.exit(1);
});

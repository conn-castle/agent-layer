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
 * Generates Antigravity workflows from `.agent-layer/config/workflows/*.md`:
 * - .agent/workflows/<workflow>.md
 *
 * Generates per-client command allowlists from `.agent-layer/config/policy/commands.json`:
 * - .gemini/settings.json
 * - .claude/settings.json
 * - .vscode/settings.json
 * - .codex/rules/default.rules
 *
 * Usage:
 *   ./al --sync
 *   ./al --sync --check
 *   ./al --sync --verbose
 *   ./al --sync --overwrite
 *   ./al --sync --interactive
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
import { generateAntigravityWorkflows } from "./antigravity.mjs";
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

export const SYNC_USAGE = [
  "Usage:",
  "  ./al --sync [--check] [--verbose] [--overwrite] [--interactive] [--parent-root <path>] [--temp-parent-root] [--agent-layer-root <path>]",
].join("\n");

/**
 * Parse sync command arguments.
 * @param {string[]} argv
 * @returns {{ check: boolean, verbose: boolean, overwrite: boolean, interactive: boolean }}
 */
export function parseSyncArgs(argv) {
  const args = {
    check: false,
    verbose: false,
    overwrite: false,
    interactive: false,
  };
  for (const a of argv) {
    if (a === "--check") args.check = true;
    else if (a === "--verbose") args.verbose = true;
    else if (a === "--overwrite") args.overwrite = true;
    else if (a === "--interactive") args.interactive = true;
    else if (a === "-h" || a === "--help")
      throw new Error("agent-layer sync: help requested.");
    else throw new Error(`agent-layer sync: unknown argument "${a}".`);
  }
  if (args.overwrite && args.interactive) {
    throw new Error(
      "agent-layer sync: choose only one of --overwrite or --interactive.",
    );
  }
  if (args.check && args.interactive) {
    throw new Error(
      "agent-layer sync: --interactive cannot be used with --check.",
    );
  }
  return args;
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
      "agent-layer sync: To remove them, run ./al --clean or delete them manually. To re-enable, update config/agents.json and re-run ./al --sync.",
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
/**
 * Run the sync generator with explicit roots.
 * @param {string} parentRoot
 * @param {string} agentLayerRoot
 * @param {{ check: boolean, verbose: boolean, overwrite: boolean, interactive: boolean }} args
 * @returns {Promise<void>}
 */
export async function runSync(parentRoot, agentLayerRoot, args) {
  if (
    !parentRoot ||
    !agentLayerRoot ||
    typeof parentRoot !== "string" ||
    typeof agentLayerRoot !== "string"
  ) {
    throw new Error(
      "agent-layer sync: parentRoot and agentLayerRoot are required.",
    );
  }
  const resolvedParent = path.resolve(parentRoot);
  const resolvedAgentLayer = path.resolve(agentLayerRoot);
  if (!fileExists(resolvedParent)) {
    throw new Error(
      `agent-layer sync: parent root does not exist: ${resolvedParent}`,
    );
  }
  if (!fileExists(resolvedAgentLayer)) {
    throw new Error(
      `agent-layer sync: agent-layer root does not exist: ${resolvedAgentLayer}`,
    );
  }
  const options = args ?? {
    check: false,
    verbose: false,
    overwrite: false,
    interactive: false,
  };

  // Resolve config source directories relative to the repo root.
  const instructionsDir = path.join(
    resolvedAgentLayer,
    "config",
    "instructions",
  );
  const workflowsDir = path.join(resolvedAgentLayer, "config", "workflows");

  // Load agent config to determine enabled outputs.
  const agentConfig = loadAgentConfig(resolvedAgentLayer);
  const enabledAgents = getEnabledAgents(agentConfig);
  const geminiEnabled = enabledAgents.has("gemini");
  const claudeEnabled = enabledAgents.has("claude");
  const codexEnabled = enabledAgents.has("codex");
  const vscodeEnabled = enabledAgents.has("vscode");

  // Load policy and build per-client allowlists.
  const policy = loadCommandPolicy(resolvedAgentLayer);
  const prefixes = commandPrefixes(policy);
  const geminiAllowed = buildGeminiAllowed(prefixes);
  const claudeAllowed = buildClaudeAllow(prefixes);
  const vscodeAutoApprove = buildVscodeAutoApprove(prefixes);

  // Load MCP catalog and handle any divergence warnings or prompts.
  const catalog = loadServerCatalog(resolvedAgentLayer);
  const divergence = collectDivergences(
    resolvedParent,
    policy,
    catalog,
    enabledAgents,
    resolvedAgentLayer,
  );
  const hasDivergence = divergence.approvals.length || divergence.mcp.length;
  if (hasDivergence) {
    if (options.interactive) {
      const action = await promptDivergenceAction(divergence, resolvedParent);
      if (action === "overwrite") {
        Object.assign(options, { overwrite: true, interactive: false });
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
  if (options.overwrite && hasDivergence) {
    console.warn(
      "agent-layer sync: overwriting client configs to match Agent Layer sources.",
    );
  }

  // Build the unified instructions output shared across clients.
  const unified =
    banner(".agent-layer/config/instructions/*.md", REGEN_COMMAND) +
    concatInstructions(instructionsDir);

  // Seed the outputs list with instruction shims.
  const outputs = [[path.join(resolvedParent, "AGENTS.md"), unified]];
  if (codexEnabled) {
    outputs.push([path.join(resolvedParent, ".codex", "AGENTS.md"), unified]);
  }
  if (claudeEnabled) {
    outputs.push([path.join(resolvedParent, "CLAUDE.md"), unified]);
  }
  if (geminiEnabled) {
    outputs.push([path.join(resolvedParent, "GEMINI.md"), unified]);
  }
  if (vscodeEnabled) {
    outputs.push([
      path.join(resolvedParent, ".github", "copilot-instructions.md"),
      unified,
    ]);
  }

  // Build MCP configs and merge with existing client settings.
  const mcpConfigs = buildMcpConfigs(
    catalog,
    enabledAgents,
    resolvedAgentLayer,
  );
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
    const vscodeMcpPath = path.join(resolvedParent, ".vscode", "mcp.json");
    const vscodeMcpExisting = readJsonRelaxed(vscodeMcpPath, {});
    const vscodeMcpMerged = mergeMcpConfig(
      vscodeMcpExisting,
      mcpConfigs.vscode,
      "servers",
      { overwrite: options.overwrite },
      managedServerNames,
    );
    outputs.push([
      vscodeMcpPath,
      JSON.stringify(vscodeMcpMerged, null, 2) + "\n",
    ]);
  }

  if (claudeEnabled) {
    const claudeMcpPath = path.join(resolvedParent, ".mcp.json");
    const claudeMcpExisting = readJsonRelaxed(claudeMcpPath, {});
    const claudeMcpMerged = mergeMcpConfig(
      claudeMcpExisting,
      mcpConfigs.claude,
      "mcpServers",
      { overwrite: options.overwrite },
      managedServerNames,
    );
    outputs.push([
      claudeMcpPath,
      JSON.stringify(claudeMcpMerged, null, 2) + "\n",
    ]);
  }

  if (codexEnabled) {
    const codexConfigPath = path.join(resolvedParent, ".codex", "config.toml");
    const codexExisting = fileExists(codexConfigPath)
      ? readUtf8(codexConfigPath)
      : null;
    const codexMerged = mergeCodexConfig(
      codexExisting,
      codexConfig,
      {
        overwrite: options.overwrite,
      },
      managedServerNames,
    );
    outputs.push([codexConfigPath, codexMerged]);
  }

  // Merge Gemini settings, preserving non-managed entries.
  if (geminiEnabled) {
    const geminiSettingsPath = path.join(
      resolvedParent,
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
      { overwrite: options.overwrite, managedServers: managedServerNames },
    );
    outputs.push([
      geminiSettingsPath,
      JSON.stringify(geminiMerged, null, 2) + "\n",
    ]);
  }

  // Merge Claude settings, preserving non-managed entries.
  if (claudeEnabled) {
    const claudeSettingsPath = path.join(
      resolvedParent,
      ".claude",
      "settings.json",
    );
    const claudeExisting = readJsonRelaxed(claudeSettingsPath, {});
    const claudeMerged = mergeClaudeSettings(
      claudeExisting,
      claudeAllowPatterns ?? [],
      claudeSettingsPath,
      { overwrite: options.overwrite },
    );
    outputs.push([
      claudeSettingsPath,
      JSON.stringify(claudeMerged, null, 2) + "\n",
    ]);
  }

  // Merge VS Code settings, preserving non-managed entries.
  if (vscodeEnabled) {
    const vscodeSettingsPath = path.join(
      resolvedParent,
      ".vscode",
      "settings.json",
    );
    const vscodeExisting = readJsonRelaxed(vscodeSettingsPath, {});
    const vscodeMerged = mergeVscodeSettings(
      vscodeExisting,
      vscodeAutoApprove,
      vscodeSettingsPath,
      { overwrite: options.overwrite },
    );
    outputs.push([
      vscodeSettingsPath,
      JSON.stringify(vscodeMerged, null, 2) + "\n",
    ]);
  }

  // Render and merge Codex rules for command policy enforcement.
  if (codexEnabled) {
    const codexRulesPath = path.join(
      resolvedParent,
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
          overwrite: options.overwrite,
        })
      : codexRulesGenerated;
    outputs.push([codexRulesPath, codexRulesMerged]);
  }

  warnDisabledOutputs(resolvedParent, enabledAgents);

  // Write or diff outputs and regenerate Codex skills/prompts.
  diffOrWrite(outputs, options, resolvedParent);
  generateAntigravityWorkflows(resolvedParent, workflowsDir, options);
  if (codexEnabled) {
    generateCodexSkills(resolvedParent, workflowsDir, options);
  }
  if (vscodeEnabled) {
    generateVscodePrompts(resolvedParent, workflowsDir, options);
  }

  // Emit a success summary unless running in --check mode.
  if (!options.check) {
    console.log(
      "agent-layer sync: updated shims + MCP configs + allowlists + Antigravity workflows + Codex skills + VS Code prompts",
    );
  }
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  console.error("agent-layer sync: use ./al --sync");
  process.exit(2);
}

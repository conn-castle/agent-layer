import fs from "node:fs";
import path from "node:path";
import readline from "node:readline";
import { fileExists, readUtf8 } from "../sync/utils.mjs";
import { loadAgentConfig, LAUNCHABLE_AGENTS } from "./agent-config.mjs";

/**
 * @typedef {object} SelectChoice
 * @property {string} label - Display label for the choice
 * @property {string} value - Value to use when selected
 * @property {string=} group - Optional group header (for visual grouping)
 */

/**
 * Model choices for Gemini CLI.
 * @type {SelectChoice[]}
 */
const GEMINI_MODEL_CHOICES = [
  { label: "Use client default (unset)", value: "" },
  { label: "gemini-2.5-pro", value: "gemini-2.5-pro" },
  { label: "gemini-2.5-flash", value: "gemini-2.5-flash" },
  { label: "gemini-2.5-flash-lite", value: "gemini-2.5-flash-lite" },
  { label: "gemini-3-pro-preview", value: "gemini-3-pro-preview" },
  { label: "gemini-3-flash-preview", value: "gemini-3-flash-preview" },
];

/**
 * Model choices for Claude Code CLI.
 * @type {SelectChoice[]}
 */
const CLAUDE_MODEL_CHOICES = [
  { label: "Use client default (unset)", value: "" },
  {
    label: "claude-sonnet-4-5-20250929 (default)",
    value: "claude-sonnet-4-5-20250929",
  },
  { label: "claude-opus-4-5-20251101", value: "claude-opus-4-5-20251101" },
  { label: "claude-haiku-4-5-20251001", value: "claude-haiku-4-5-20251001" },
  { label: "claude-sonnet-4-20250514", value: "claude-sonnet-4-20250514" },
  { label: "claude-3-5-haiku-20241022", value: "claude-3-5-haiku-20241022" },
];

/**
 * Model choices for Codex CLI.
 * @type {SelectChoice[]}
 */
const CODEX_MODEL_CHOICES = [
  { label: "Use client default (unset)", value: "" },
  { label: "gpt-5.2-codex", value: "gpt-5.2-codex", group: "Recommended" },
  { label: "gpt-5.1-codex-mini", value: "gpt-5.1-codex-mini" },
  {
    label: "gpt-5.1-codex-max",
    value: "gpt-5.1-codex-max",
    group: "Alternative",
  },
  { label: "gpt-5.2", value: "gpt-5.2" },
  { label: "gpt-5.1", value: "gpt-5.1" },
  { label: "gpt-5.1-codex", value: "gpt-5.1-codex" },
  { label: "gpt-5-codex", value: "gpt-5-codex" },
  { label: "gpt-5-codex-mini", value: "gpt-5-codex-mini" },
  { label: "gpt-5", value: "gpt-5" },
];

/**
 * Reasoning effort choices for Codex CLI.
 * @type {SelectChoice[]}
 */
const CODEX_REASONING_CHOICES = [
  { label: "Use client default (unset)", value: "" },
  { label: "minimal", value: "minimal" },
  { label: "low", value: "low" },
  { label: "medium", value: "medium" },
  { label: "high", value: "high" },
  { label: "xhigh", value: "xhigh" },
];

/**
 * Extract the current model value from defaultArgs.
 * @param {string[]=} defaultArgs
 * @returns {string} The model value or empty string if unset.
 */
function getCurrentModel(defaultArgs) {
  if (!defaultArgs) return "";
  for (let i = 0; i < defaultArgs.length; i++) {
    const arg = defaultArgs[i];
    if (arg === "--model" && i + 1 < defaultArgs.length) {
      return defaultArgs[i + 1];
    }
    if (arg.startsWith("--model=")) {
      return arg.slice("--model=".length);
    }
  }
  return "";
}

/**
 * Extract the current reasoning effort value from Codex defaultArgs.
 * @param {string[]=} defaultArgs
 * @returns {string} The reasoning effort value or empty string if unset.
 */
function getCurrentReasoningEffort(defaultArgs) {
  if (!defaultArgs) return "";
  for (let i = 0; i < defaultArgs.length; i++) {
    const arg = defaultArgs[i];
    if (arg === "--config" && i + 1 < defaultArgs.length) {
      const configValue = defaultArgs[i + 1];
      const match = configValue.match(
        /^model_reasoning_effort=["']?([^"']+)["']?$/,
      );
      if (match) return match[1];
    }
    if (arg.startsWith("--config=")) {
      const configValue = arg.slice("--config=".length);
      const match = configValue.match(
        /^model_reasoning_effort=["']?([^"']+)["']?$/,
      );
      if (match) return match[1];
    }
  }
  return "";
}

/**
 * Update defaultArgs to set or remove the model.
 * @param {string[]=} defaultArgs
 * @param {string} model - The model value to set, or empty string to remove.
 * @returns {string[]} The updated defaultArgs array.
 */
function setModel(defaultArgs, model) {
  const args = defaultArgs ? [...defaultArgs] : [];

  // Remove existing --model entries
  for (let i = args.length - 1; i >= 0; i--) {
    const arg = args[i];
    if (arg === "--model") {
      // Remove --model and its value
      args.splice(
        i,
        i + 1 < args.length && !args[i + 1].startsWith("--") ? 2 : 1,
      );
    } else if (arg.startsWith("--model=")) {
      args.splice(i, 1);
    }
  }

  // Add new model if specified
  if (model) {
    args.push("--model", model);
  }

  return args;
}

/**
 * Update Codex defaultArgs to set or remove the reasoning effort.
 * @param {string[]=} defaultArgs
 * @param {string} effort - The reasoning effort value to set, or empty string to remove.
 * @returns {string[]} The updated defaultArgs array.
 */
function setReasoningEffort(defaultArgs, effort) {
  const args = defaultArgs ? [...defaultArgs] : [];

  // Remove existing --config model_reasoning_effort entries
  for (let i = args.length - 1; i >= 0; i--) {
    const arg = args[i];
    if (arg === "--config" && i + 1 < args.length) {
      const configValue = args[i + 1];
      if (configValue.startsWith("model_reasoning_effort=")) {
        args.splice(i, 2);
      }
    } else if (arg.startsWith("--config=")) {
      const configValue = arg.slice("--config=".length);
      if (configValue.startsWith("model_reasoning_effort=")) {
        args.splice(i, 1);
      }
    }
  }

  // Add new reasoning effort if specified
  if (effort) {
    args.push("--config", `model_reasoning_effort=${effort}`);
  }

  return args;
}

/**
 * Display a single-select menu and return the selected value.
 * Uses arrow keys for navigation and Enter to select.
 * @param {string} title - The title to display above the menu.
 * @param {SelectChoice[]} choices - The available choices.
 * @param {string} currentValue - The currently selected value (for highlighting).
 * @returns {Promise<string>} The selected value.
 */
async function promptSelect(title, choices, currentValue) {
  return new Promise((resolve) => {
    // Find initial selection index based on current value
    let selectedIndex = choices.findIndex((c) => c.value === currentValue);
    if (selectedIndex === -1) selectedIndex = 0;

    const stdin = process.stdin;
    const stdout = process.stdout;

    // Track if we need raw mode
    const wasRaw = stdin.isRaw;

    /**
     * Render the menu.
     * @returns {void}
     */
    function render() {
      // Clear previous output and move cursor up
      stdout.write("\x1b[2K"); // Clear current line
      const lines = [];
      lines.push(`\n${title}`);

      let currentGroup = "";
      for (let i = 0; i < choices.length; i++) {
        const choice = choices[i];

        // Show group header if it changed
        if (choice.group && choice.group !== currentGroup) {
          currentGroup = choice.group;
          lines.push(`  ${currentGroup}:`);
        }

        const prefix = i === selectedIndex ? "> " : "  ";
        const highlight = i === selectedIndex ? "\x1b[36m" : "";
        const reset = i === selectedIndex ? "\x1b[0m" : "";
        lines.push(`${prefix}${highlight}${choice.label}${reset}`);
      }

      lines.push("\n(up/down to move, Enter to select)");

      // Clear screen area and render
      stdout.write(`\x1b[${lines.length + 1}A`); // Move up
      for (const line of lines) {
        stdout.write(`\x1b[2K${line}\n`); // Clear line and write
      }
    }

    /**
     * Handle keypress events.
     * @param {Buffer} key - The key buffer.
     * @returns {void}
     */
    function onKeypress(key) {
      const keyStr = key.toString();

      // Arrow up
      if (keyStr === "\x1b[A" || keyStr === "k") {
        selectedIndex = Math.max(0, selectedIndex - 1);
        render();
        return;
      }

      // Arrow down
      if (keyStr === "\x1b[B" || keyStr === "j") {
        selectedIndex = Math.min(choices.length - 1, selectedIndex + 1);
        render();
        return;
      }

      // Enter
      if (keyStr === "\r" || keyStr === "\n") {
        cleanup();
        resolve(choices[selectedIndex].value);
        return;
      }

      // Ctrl+C
      if (keyStr === "\x03") {
        cleanup();
        process.exit(130);
      }
    }

    /**
     * Cleanup stdin state.
     * @returns {void}
     */
    function cleanup() {
      stdin.removeListener("data", onKeypress);
      if (stdin.isTTY) {
        stdin.setRawMode(wasRaw ?? false);
      }
      stdin.pause();
    }

    // Enable raw mode for keypress detection
    if (stdin.isTTY) {
      stdin.setRawMode(true);
    }
    stdin.resume();
    stdin.on("data", onKeypress);

    // Initial render - first print empty lines to make room
    const totalLines = choices.length + 4; // title + choices + instructions + spacing
    for (let i = 0; i < totalLines; i++) {
      stdout.write("\n");
    }
    render();
  });
}

export const INSTALL_CONFIG_USAGE = [
  "Usage:",
  "  ./al --install-config [--force] [--new-install] [--non-interactive] [--parent-root <path>] [--temp-parent-root] [--agent-layer-root <path>]",
].join("\n");

const GITIGNORE_BLOCK = [
  "# >>> agent-layer",
  ".agent-layer/",
  "",
  "# Agent Layer launcher",
  "al",
  "",
  "# Agent Layer-generated instruction shims",
  "AGENTS.md",
  "CLAUDE.md",
  "GEMINI.md",
  ".github/copilot-instructions.md",
  "",
  "# Agent Layer-generated client configs + artifacts",
  ".mcp.json",
  ".codex/",
  ".gemini/",
  ".claude/",
  ".vscode/mcp.json",
  ".vscode/prompts/",
  ".agent/workflows/",
  "# <<< agent-layer",
].join("\n");

const MEMORY_FILES = [
  "ISSUES.md",
  "FEATURES.md",
  "ROADMAP.md",
  "DECISIONS.md",
  "COMMANDS.md",
];

/**
 * @typedef {{ force: boolean, newInstall: boolean, nonInteractive: boolean }} InstallConfigArgs
 */

/**
 * Parse arguments for the install-config subcommand.
 * @param {string[]} argv
 * @returns {InstallConfigArgs}
 */
export function parseInstallConfigArgs(argv) {
  const args = {
    force: false,
    newInstall: false,
    nonInteractive: false,
  };
  for (const arg of argv) {
    if (arg === "--force") {
      args.force = true;
      continue;
    }
    if (arg === "--new-install") {
      args.newInstall = true;
      continue;
    }
    if (arg === "--non-interactive") {
      args.nonInteractive = true;
      continue;
    }
    throw new Error(`agent-layer cli: install-config unknown argument: ${arg}`);
  }
  return args;
}

/**
 * Print a message.
 * @param {string} message
 * @returns {void}
 */
function say(message) {
  process.stdout.write(`${message}\n`);
}

/**
 * Ensure the .env exists under the agent-layer root.
 * @param {string} agentLayerRoot
 * @returns {void}
 */
function ensureEnvFile(agentLayerRoot) {
  const envPath = path.join(agentLayerRoot, ".env");
  if (fileExists(envPath)) {
    say("==> .agent-layer/.env already exists; leaving as-is");
    return;
  }
  const examplePath = path.join(agentLayerRoot, ".env.example");
  if (!fileExists(examplePath)) {
    throw new Error("Missing .agent-layer/.env.example; cannot create .env");
  }
  fs.copyFileSync(examplePath, envPath);
  say("==> Created .agent-layer/.env from .env.example");
}

/**
 * Create a readline interface for prompts.
 * @returns {import(\"node:readline\").Interface}
 */
function createPromptInterface() {
  return readline.createInterface({
    input: process.stdin,
    output: process.stdout,
  });
}

/**
 * Prompt for a yes/no response.
 * @param {import(\"node:readline\").Interface} rl
 * @param {string} prompt
 * @param {boolean} defaultYes
 * @returns {Promise<boolean>}
 */
async function promptYesNo(rl, prompt, defaultYes) {
  while (true) {
    const answer = await new Promise((resolve) => {
      rl.question(prompt, resolve);
    });
    const normalized = String(answer ?? "").trim();
    if (!normalized) return defaultYes;
    if (/^(y|yes)$/i.test(normalized)) return true;
    if (/^(n|no)$/i.test(normalized)) return false;
    say("Please answer y or n.");
  }
}

/**
 * Ensure a memory file exists, optionally prompting for replacement.
 * @param {string} parentRoot
 * @param {string} agentLayerRoot
 * @param {string} name
 * @param {boolean} interactive
 * @param {import(\"node:readline\").Interface | null} rl
 * @returns {Promise<void>}
 */
async function ensureMemoryFile(
  parentRoot,
  agentLayerRoot,
  name,
  interactive,
  rl,
) {
  const filePath = path.join(parentRoot, "docs", name);
  const templatePath = path.join(
    agentLayerRoot,
    "config",
    "templates",
    "docs",
    name,
  );
  const relPath = path.relative(parentRoot, filePath);

  if (!fileExists(templatePath)) {
    throw new Error(
      `Missing template: ${path.relative(agentLayerRoot, templatePath)}`,
    );
  }

  if (fileExists(filePath)) {
    if (!interactive || !rl) {
      say(`==> ${relPath} exists; leaving as-is (no TTY to confirm)`);
      return;
    }
    const keep = await promptYesNo(
      rl,
      `${relPath} exists. Keep it? [Y/n] `,
      true,
    );
    if (keep) {
      say(`==> Keeping existing ${relPath}`);
      return;
    }
    fs.mkdirSync(path.dirname(filePath), { recursive: true });
    fs.copyFileSync(templatePath, filePath);
    say(`==> Replaced ${relPath} with template`);
    return;
  }

  fs.mkdirSync(path.dirname(filePath), { recursive: true });
  fs.copyFileSync(templatePath, filePath);
  say(`==> Created ${relPath} from template`);
}

/**
 * Ensure all memory files exist using the templates.
 * @param {string} parentRoot
 * @param {string} agentLayerRoot
 * @param {boolean} interactive
 * @param {import(\"node:readline\").Interface | null} rl
 * @returns {Promise<void>}
 */
async function ensureMemoryFiles(parentRoot, agentLayerRoot, interactive, rl) {
  say("==> Ensuring project memory files exist");
  for (const name of MEMORY_FILES) {
    await ensureMemoryFile(parentRoot, agentLayerRoot, name, interactive, rl);
  }
}

/**
 * Write the repo-local launcher script.
 * @param {string} parentRoot
 * @returns {void}
 */
function writeLauncher(parentRoot) {
  const launcher = [
    "#!/usr/bin/env bash",
    "set -euo pipefail",
    "",
    "# Repo-local launcher.",
    "# This script delegates to the managed Agent Layer entrypoint in .agent-layer/.",
    "# If you prefer, replace this file with a symlink to .agent-layer/agent-layer.",
    'SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"',
    'exec "$SCRIPT_DIR/.agent-layer/agent-layer" "$@"',
    "",
  ].join("\n");
  const launcherPath = path.join(parentRoot, "al");
  fs.writeFileSync(launcherPath, launcher);
  fs.chmodSync(launcherPath, 0o755);
}

/**
 * Create or preserve the repo-local launcher based on force.
 * @param {string} parentRoot
 * @param {boolean} force
 * @returns {void}
 */
function ensureLauncher(parentRoot, force) {
  const launcherPath = path.join(parentRoot, "al");
  if (fileExists(launcherPath)) {
    if (force) {
      say("==> Overwriting ./al");
      writeLauncher(parentRoot);
    } else {
      say("==> NOTE: ./al already exists; not overwriting.");
      say("==> Re-run the installer with --force to replace ./al.");
    }
    return;
  }
  say("==> Creating ./al");
  writeLauncher(parentRoot);
}

/**
 * Update the agent-layer block in .gitignore.
 * @param {string} parentRoot
 * @returns {void}
 */
function updateGitignore(parentRoot) {
  const gitignorePath = path.join(parentRoot, ".gitignore");
  const blockLines = GITIGNORE_BLOCK.split("\n");
  let lines = [];
  if (fileExists(gitignorePath)) {
    const raw = readUtf8(gitignorePath);
    lines = raw === "" ? [] : raw.split(/\r?\n/);
  }

  const output = [];
  let found = false;
  let inBlock = false;
  for (const line of lines) {
    if (line === "# >>> agent-layer") {
      if (!found) {
        output.push(...blockLines);
        found = true;
      }
      inBlock = true;
      continue;
    }
    if (inBlock) {
      if (line === "# <<< agent-layer") {
        inBlock = false;
      }
      continue;
    }
    output.push(line);
  }

  if (!found) {
    if (output.length > 0 && output[output.length - 1] !== "") {
      output.push("");
    }
    output.push(...blockLines);
  }

  const content = `${output.join("\n")}\n`;
  fs.writeFileSync(gitignorePath, content);
}

/**
 * Configure enabled agents during new installs.
 * @param {string} agentLayerRoot
 * @param {boolean} interactive
 * @param {import(\"node:readline\").Interface | null} rl
 * @returns {Promise<void>}
 */
async function configureAgents(agentLayerRoot, interactive, rl) {
  const configPath = path.join(agentLayerRoot, "config", "agents.json");
  if (!fileExists(configPath)) {
    throw new Error(
      "Missing .agent-layer/config/agents.json; cannot configure enabled agents.",
    );
  }

  let enableAll = true;
  let enableGemini = true;
  let enableClaude = true;
  let enableCodex = true;
  let enableVscode = true;

  if (interactive && rl) {
    say("==> Choose which agents to enable (press Enter for yes).");
    enableGemini = await promptYesNo(rl, "Enable Gemini CLI? [Y/n] ", true);
    enableClaude = await promptYesNo(
      rl,
      "Enable Claude Code CLI? [Y/n] ",
      true,
    );
    enableCodex = await promptYesNo(rl, "Enable Codex CLI? [Y/n] ", true);
    enableVscode = await promptYesNo(
      rl,
      "Enable VS Code / Copilot Chat? [Y/n] ",
      true,
    );
    enableAll = false;
  }

  if (!interactive || !rl || enableAll) {
    say("==> Non-interactive install: enabling all agents");
    enableGemini = true;
    enableClaude = true;
    enableCodex = true;
    enableVscode = true;
  }

  const config = loadAgentConfig(agentLayerRoot);
  config.gemini.enabled = enableGemini;
  config.claude.enabled = enableClaude;
  config.codex.enabled = enableCodex;
  config.vscode.enabled = enableVscode;

  fs.writeFileSync(configPath, `${JSON.stringify(config, null, 2)}\n`);
  say("==> Updated .agent-layer/config/agents.json");
}

/**
 * Get the model choices for a given agent.
 * @param {string} agent - The agent name.
 * @returns {SelectChoice[]}
 */
function getModelChoices(agent) {
  switch (agent) {
    case "gemini":
      return GEMINI_MODEL_CHOICES;
    case "claude":
      return CLAUDE_MODEL_CHOICES;
    case "codex":
      return CODEX_MODEL_CHOICES;
    default:
      return [];
  }
}

/**
 * Get the display name for a given agent.
 * @param {string} agent - The agent name.
 * @returns {string}
 */
function getAgentDisplayName(agent) {
  switch (agent) {
    case "gemini":
      return "Gemini CLI";
    case "claude":
      return "Claude Code CLI";
    case "codex":
      return "Codex CLI";
    default:
      return agent;
  }
}

/**
 * Get the default model description for a given agent.
 * @param {string} agent - The agent name.
 * @returns {string}
 */
function getDefaultModelDescription(agent) {
  switch (agent) {
    case "gemini":
      return "Uses Gemini CLI's default behavior (often an Auto selection strategy)";
    case "claude":
      return "claude-sonnet-4-5-20250929 (Sonnet 4.5)";
    case "codex":
      return "gpt-5.2-codex";
    default:
      return "client default";
  }
}

/**
 * Configure agent models during wizard setup.
 * @param {string} agentLayerRoot
 * @param {boolean} interactive
 * @param {import("./agent-config.mjs").AgentConfig} config
 * @returns {Promise<import("./agent-config.mjs").AgentConfig>}
 */
async function configureAgentModels(agentLayerRoot, interactive, config) {
  if (!interactive) {
    say("==> Non-interactive mode: keeping existing model configurations");
    return config;
  }

  say("\n==> Configure agent models");

  for (const agent of LAUNCHABLE_AGENTS) {
    if (!config[agent]?.enabled) {
      continue;
    }

    const displayName = getAgentDisplayName(agent);
    const choices = getModelChoices(agent);
    const currentModel = getCurrentModel(config[agent].defaultArgs);
    const defaultDesc = getDefaultModelDescription(agent);

    say(`\n--- ${displayName} ---`);
    say(`Default when unset: ${defaultDesc}`);
    if (currentModel) {
      say(`Current setting: ${currentModel}`);
    } else {
      say("Current setting: (unset - using client default)");
    }

    const selectedModel = await promptSelect(
      `Select model for ${displayName}:`,
      choices,
      currentModel,
    );

    config[agent].defaultArgs = setModel(
      config[agent].defaultArgs,
      selectedModel,
    );

    // For Codex, also prompt for reasoning effort
    if (agent === "codex") {
      const currentEffort = getCurrentReasoningEffort(
        config[agent].defaultArgs,
      );
      say(
        "\nDefault reasoning effort when unset: medium (some models default higher)",
      );
      if (currentEffort) {
        say(`Current setting: ${currentEffort}`);
      } else {
        say("Current setting: (unset - using client default)");
      }

      const selectedEffort = await promptSelect(
        "Select reasoning effort for Codex:",
        CODEX_REASONING_CHOICES,
        currentEffort,
      );

      config[agent].defaultArgs = setReasoningEffort(
        config[agent].defaultArgs,
        selectedEffort,
      );
    }

    // Clean up empty defaultArgs
    if (config[agent].defaultArgs && config[agent].defaultArgs.length === 0) {
      delete config[agent].defaultArgs;
    }
  }

  return config;
}

/**
 * Run the interactive wizard for agent configuration.
 * @param {{ parentRoot: string, agentLayerRoot: string }} roots
 * @returns {Promise<void>}
 */
export async function runWizard(roots) {
  const agentLayerRoot = roots.agentLayerRoot;
  const interactive = Boolean(process.stdin.isTTY && process.stdout.isTTY);

  if (!interactive) {
    throw new Error(
      "agent-layer wizard: requires an interactive terminal (TTY on stdin/stdout).",
    );
  }

  const configPath = path.join(agentLayerRoot, "config", "agents.json");
  if (!fileExists(configPath)) {
    throw new Error(
      "Missing .agent-layer/config/agents.json; run ./al --sync first.",
    );
  }

  say("=== Agent Layer Configuration Wizard ===\n");

  // Create readline interface for yes/no prompts
  const rl = createPromptInterface();

  try {
    // Step 1: Configure agent enablement
    say("==> Step 1: Choose which agents to enable");
    let config = loadAgentConfig(agentLayerRoot);

    const enableGemini = await promptYesNo(
      rl,
      `Enable Gemini CLI? [${config.gemini.enabled ? "Y/n" : "y/N"}] `,
      config.gemini.enabled,
    );
    const enableClaude = await promptYesNo(
      rl,
      `Enable Claude Code CLI? [${config.claude.enabled ? "Y/n" : "y/N"}] `,
      config.claude.enabled,
    );
    const enableCodex = await promptYesNo(
      rl,
      `Enable Codex CLI? [${config.codex.enabled ? "Y/n" : "y/N"}] `,
      config.codex.enabled,
    );
    const enableVscode = await promptYesNo(
      rl,
      `Enable VS Code / Copilot Chat? [${config.vscode.enabled ? "Y/n" : "y/N"}] `,
      config.vscode.enabled,
    );

    config.gemini.enabled = enableGemini;
    config.claude.enabled = enableClaude;
    config.codex.enabled = enableCodex;
    config.vscode.enabled = enableVscode;

    // Close readline before using raw mode for select menus
    rl.close();

    // Step 2: Configure models for enabled agents
    say("\n==> Step 2: Configure models and reasoning");
    config = await configureAgentModels(agentLayerRoot, interactive, config);

    // Write updated config
    fs.writeFileSync(configPath, `${JSON.stringify(config, null, 2)}\n`);
    say("\n==> Configuration saved to .agent-layer/config/agents.json");

    // Suggest running sync
    say("\nRun ./al --sync to apply these changes to client configurations.");
  } catch (err) {
    rl.close();
    throw err;
  }
}

/**
 * Run installer configuration tasks.
 * @param {{ parentRoot: string, agentLayerRoot: string }} roots
 * @param {InstallConfigArgs} args
 * @returns {Promise<void>}
 */
export async function runInstallConfig(roots, args) {
  const parentRoot = roots.parentRoot;
  const agentLayerRoot = roots.agentLayerRoot;
  const interactive = !args.nonInteractive && Boolean(process.stdin.isTTY);

  let rl = null;
  if (interactive) {
    rl = createPromptInterface();
  }

  try {
    ensureEnvFile(agentLayerRoot);
    await ensureMemoryFiles(parentRoot, agentLayerRoot, interactive, rl);
    ensureLauncher(parentRoot, args.force);
    say("==> Updating .gitignore (agent-layer block)");
    updateGitignore(parentRoot);
    if (args.newInstall) {
      await configureAgents(agentLayerRoot, interactive, rl);
    }
  } finally {
    if (rl) rl.close();
  }
}

// Exported for testing
export const _testing = {
  getCurrentModel,
  getCurrentReasoningEffort,
  setModel,
  setReasoningEffort,
};

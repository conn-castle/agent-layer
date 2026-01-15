import path from "node:path";
import { fileURLToPath } from "node:url";
import {
  fileExists,
  isPlainObject,
  readJsonRelaxed,
  writeUtf8,
} from "../sync/utils.mjs";

/**
 * @typedef {object} AgentEntry
 * @property {boolean} enabled
 * @property {string[]=} defaultArgs
 */

/**
 * @typedef {Record<string, AgentEntry>} AgentConfig
 */

/**
 * Supported agent names for config enforcement.
 * @type {string[]}
 */
export const SUPPORTED_AGENTS = ["gemini", "claude", "codex", "vscode"];

/**
 * Assert a condition and throw a config-scoped error if false.
 * @param {boolean} cond
 * @param {string} msg
 * @returns {void}
 */
function assertConfig(cond, msg) {
  if (!cond) throw new Error(`agent-layer config: ${msg}`);
}

/**
 * Resolve the agent config path under the agent-layer root.
 * @param {string} agentLayerRoot
 * @returns {string}
 */
export function agentConfigPath(agentLayerRoot) {
  return path.join(agentLayerRoot, "config", "agents.json");
}

/**
 * Validate the agent config schema.
 * @param {unknown} parsed
 * @param {string} filePath
 * @returns {void}
 */
export function validateAgentConfig(parsed, filePath) {
  assertConfig(isPlainObject(parsed), `${filePath} must contain a JSON object`);

  const keys = Object.keys(parsed);
  const missing = SUPPORTED_AGENTS.filter((name) => !keys.includes(name));
  assertConfig(
    missing.length === 0,
    `${filePath} missing required agents: ${missing.join(", ")}`,
  );

  const unknown = keys.filter((key) => !SUPPORTED_AGENTS.includes(key));
  assertConfig(
    unknown.length === 0,
    `${filePath} contains unknown agents: ${unknown.join(", ")}`,
  );

  for (const name of SUPPORTED_AGENTS) {
    const entry = parsed[name];
    assertConfig(
      isPlainObject(entry),
      `${filePath}: ${name} must be an object`,
    );
    const allowedKeys = new Set(["enabled", "defaultArgs"]);
    for (const key of Object.keys(entry)) {
      assertConfig(
        allowedKeys.has(key),
        `${filePath}: ${name}.${key} is not supported`,
      );
    }
    assertConfig(
      typeof entry.enabled === "boolean",
      `${filePath}: ${name}.enabled must be boolean`,
    );
    if (entry.defaultArgs !== undefined) {
      assertConfig(
        Array.isArray(entry.defaultArgs),
        `${filePath}: ${name}.defaultArgs must be an array`,
      );
      for (let i = 0; i < entry.defaultArgs.length; i++) {
        const arg = entry.defaultArgs[i];
        assertConfig(
          typeof arg === "string" && arg.trim().length > 0,
          `${filePath}: ${name}.defaultArgs[${i}] must be a non-empty string`,
        );
        assertConfig(
          !/[\r\n]/.test(arg),
          `${filePath}: ${name}.defaultArgs[${i}] must not contain newlines`,
        );
      }
      let expectsValue = false;
      for (let i = 0; i < entry.defaultArgs.length; i++) {
        const arg = entry.defaultArgs[i];
        if (arg === "--") {
          assertConfig(
            false,
            `${filePath}: ${name}.defaultArgs[${i}] must not be "--"`,
          );
        }
        if (arg.startsWith("--")) {
          expectsValue = !arg.includes("=");
          continue;
        }
        assertConfig(
          expectsValue,
          `${filePath}: ${name}.defaultArgs[${i}] must follow a --flag`,
        );
        expectsValue = false;
      }
    }
  }
}

/**
 * Load agent config from disk.
 * @param {string} agentLayerRoot
 * @returns {AgentConfig}
 */
export function loadAgentConfig(agentLayerRoot) {
  const filePath = agentConfigPath(agentLayerRoot);
  assertConfig(fileExists(filePath), `${filePath} not found`);
  let parsed;
  try {
    parsed = readJsonRelaxed(filePath, null);
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    throw new Error(`agent-layer config: ${message}`);
  }
  validateAgentConfig(parsed, filePath);
  return /** @type {AgentConfig} */ (parsed);
}

/**
 * Persist agent config to disk.
 * @param {string} agentLayerRoot
 * @param {AgentConfig} config
 * @returns {void}
 */
export function writeAgentConfig(agentLayerRoot, config) {
  const filePath = agentConfigPath(agentLayerRoot);
  validateAgentConfig(config, filePath);
  writeUtf8(filePath, JSON.stringify(config, null, 2) + "\n");
}

/**
 * Return enabled agents as a Set for fast lookup.
 * @param {AgentConfig} config
 * @returns {Set<string>}
 */
export function getEnabledAgents(config) {
  const enabled = new Set();
  for (const name of SUPPORTED_AGENTS) {
    if (config[name]?.enabled) enabled.add(name);
  }
  return enabled;
}

/**
 * Update enabled flags and write the config back to disk.
 * @param {string} agentLayerRoot
 * @param {Record<string, boolean>} updates
 * @returns {void}
 */
export function updateAgentEnabled(agentLayerRoot, updates) {
  const config = loadAgentConfig(agentLayerRoot);
  for (const [name, enabled] of Object.entries(updates)) {
    assertConfig(
      SUPPORTED_AGENTS.includes(name),
      `${agentConfigPath(agentLayerRoot)}: unknown agent "${name}"`,
    );
    config[name] = { ...(config[name] ?? {}), enabled };
  }
  writeAgentConfig(agentLayerRoot, config);
}

/**
 * Render a shell-friendly view of a single agent entry.
 * @param {AgentConfig} config
 * @param {string} agentName
 * @returns {string}
 */
export function renderAgentShellConfig(config, agentName) {
  const agentLayerRoot = path.resolve(
    path.dirname(fileURLToPath(import.meta.url)),
    "..",
    "..",
  );
  assertConfig(
    SUPPORTED_AGENTS.includes(agentName),
    `${agentConfigPath(agentLayerRoot)}: unknown agent "${agentName}"`,
  );
  const entry = config[agentName];
  assertConfig(entry, `Missing agent entry for ${agentName}`);
  const lines = [`enabled=${entry.enabled ? "true" : "false"}`];
  const args = entry.defaultArgs ?? [];
  for (const arg of args) {
    lines.push(`defaultArg=${arg}`);
  }
  return lines.join("\n");
}

/**
 * Parse a boolean string.
 * @param {string} raw
 * @returns {boolean}
 */
function parseBool(raw) {
  if (raw === "true") return true;
  if (raw === "false") return false;
  throw new Error(`Expected boolean value (true/false), got "${raw}"`);
}

/**
 * CLI entrypoint for installer and launcher helpers.
 * @returns {void}
 */
function main() {
  const args = process.argv.slice(2);
  if (args.length === 0) {
    throw new Error(
      "Usage: agent-config.mjs --print-shell <agent> | --set-enabled <agent>=<true|false>...",
    );
  }

  const root = resolveAgentLayerRoot();
  if (args[0] === "--print-shell") {
    const agent = args[1];
    assertConfig(agent, "Usage: --print-shell <agent>");
    const config = loadAgentConfig(root);
    process.stdout.write(`${renderAgentShellConfig(config, agent)}\n`);
    return;
  }

  if (args[0] === "--set-enabled") {
    const updates = {};
    for (let i = 1; i < args.length; i++) {
      const pair = args[i];
      if (!pair || pair.startsWith("--")) {
        throw new Error("Usage: --set-enabled <agent>=<true|false>...");
      }
      const idx = pair.indexOf("=");
      if (idx <= 0) {
        throw new Error(`Invalid update "${pair}" (expected name=true|false)`);
      }
      const name = pair.slice(0, idx);
      const value = pair.slice(idx + 1);
      updates[name] = parseBool(value);
    }
    updateAgentEnabled(root, updates);
    return;
  }

  throw new Error(
    "Usage: agent-config.mjs --print-shell <agent> | --set-enabled <agent>=<true|false>...",
  );
}

try {
  if (process.argv[1] === fileURLToPath(import.meta.url)) {
    main();
  }
} catch (err) {
  const message = err instanceof Error ? err.message : String(err);
  if (message.startsWith("agent-layer")) {
    console.error(message);
  } else {
    console.error(`agent-layer config: ${message}`);
  }
  process.exit(1);
}

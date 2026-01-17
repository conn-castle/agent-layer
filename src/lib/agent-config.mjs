import path from "node:path";
import { fileExists, isPlainObject, readJsonRelaxed } from "../sync/utils.mjs";

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

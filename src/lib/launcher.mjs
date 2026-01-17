import fs from "node:fs";
import path from "node:path";
import { spawnSync } from "node:child_process";
import {
  getEnabledAgents,
  loadAgentConfig,
  SUPPORTED_AGENTS,
} from "./agent-config.mjs";
import { applyEnv, loadEnvFile } from "./env.mjs";
import { resolveParentRoot } from "./roots.mjs";
import { runSync } from "../sync/sync.mjs";

export const LAUNCHER_USAGE = [
  "Usage:",
  "  ./al [--no-sync] [--parent-root <path>] [--temp-parent-root] <command> [args...]",
].join("\n");

/**
 * @typedef {object} LauncherArgs
 * @property {boolean} noSync
 * @property {string|null} parentRoot
 * @property {string|null} agentLayerRoot
 * @property {boolean} useTempParentRoot
 * @property {string[]} commandArgs
 */

/**
 * Select the agent command name when it matches a supported agent.
 * @param {string[]} commandArgs
 * @returns {string|null}
 */
function resolveAgentCommand(commandArgs) {
  if (commandArgs.length === 0) return null;
  const candidate = path.basename(commandArgs[0]);
  if (SUPPORTED_AGENTS.includes(candidate)) return candidate;
  return null;
}

/**
 * Apply agent default args without overriding existing user flags.
 * @param {string[]} userArgs
 * @param {string[]} defaultArgs
 * @returns {string[]}
 */
function applyDefaultArgs(userArgs, defaultArgs) {
  if (defaultArgs.length === 0) return [...userArgs];
  const finalArgs = [...userArgs];
  const userFlags = new Set();
  for (const arg of userArgs.slice(1)) {
    if (!arg.startsWith("--")) continue;
    const flag = arg.includes("=") ? arg.split("=")[0] : arg;
    userFlags.add(flag);
  }

  const appended = [];
  for (let i = 0; i < defaultArgs.length; i++) {
    const token = defaultArgs[i];
    if (token.startsWith("--")) {
      const flag = token.split("=")[0];
      if (userFlags.has(flag)) {
        if (!token.includes("=")) {
          const next = defaultArgs[i + 1];
          if (next && !next.startsWith("--")) {
            i += 1;
          }
        }
        continue;
      }
      appended.push(token);
      if (!token.includes("=")) {
        const next = defaultArgs[i + 1];
        if (next && !next.startsWith("--")) {
          appended.push(next);
          i += 1;
        }
      }
      continue;
    }
    appended.push(token);
  }

  return [...finalArgs, ...appended];
}

/**
 * Build the runtime environment for a command.
 * @param {string} parentRoot
 * @param {string} agentLayerRoot
 * @param {boolean} ensureCodexHome
 * @returns {Record<string, string>}
 */
function buildRuntimeEnv(parentRoot, agentLayerRoot, ensureCodexHome) {
  let env = { ...process.env };

  const agentEnvPath = path.join(agentLayerRoot, ".env");
  const agentEnv = loadEnvFile(agentEnvPath);
  if (agentEnv.loaded) {
    env = applyEnv(env, agentEnv.env);
  }

  env.PARENT_ROOT = parentRoot;
  env.AGENT_LAYER_ROOT = agentLayerRoot;

  if (ensureCodexHome && !env.CODEX_HOME) {
    env.CODEX_HOME = path.join(parentRoot, ".codex");
  }

  return env;
}

/**
 * Warn when CODEX_HOME points outside the repo-local .codex.
 * @param {string} parentRoot
 * @returns {void}
 */
export function warnOnExternalCodexHome(parentRoot) {
  const raw = (process.env.CODEX_HOME ?? "").trim();
  if (!raw) return;
  const expectedPath = path.resolve(parentRoot, ".codex");

  if (!path.isAbsolute(raw)) {
    console.warn(
      `agent-layer: WARNING: CODEX_HOME is set to ${raw}; expected ${expectedPath}. Leaving it unchanged.`,
    );
    return;
  }

  const resolvedRaw = path.resolve(raw);
  if (resolvedRaw === expectedPath) return;

  let rawReal = null;
  let expectedReal = null;
  if (fs.existsSync(raw)) {
    try {
      rawReal = fs.realpathSync(raw);
    } catch {
      rawReal = null;
    }
  }
  if (fs.existsSync(expectedPath)) {
    try {
      expectedReal = fs.realpathSync(expectedPath);
    } catch {
      expectedReal = null;
    }
  }
  if (rawReal && expectedReal && rawReal === expectedReal) return;

  console.warn(
    `agent-layer: WARNING: CODEX_HOME is set to ${raw}; expected ${expectedPath}. Leaving it unchanged.`,
  );
}

/**
 * Execute a command with inherited stdio.
 * @param {string[]} commandArgs
 * @param {Record<string, string>} env
 * @param {string} argv0
 * @returns {number}
 */
function runCommand(commandArgs, env, argv0) {
  if (commandArgs.length === 0) return 0;
  const [command, ...args] = commandArgs;
  const result = spawnSync(command, args, {
    stdio: "inherit",
    env,
    argv0,
  });
  if (result.error) {
    throw result.error;
  }
  return typeof result.status === "number" ? result.status : 0;
}

/**
 * Run the launcher (sync + env + command execution).
 * @param {LauncherArgs} parsed
 * @returns {Promise<number>}
 */
export async function runLauncher(parsed) {
  const roots = resolveParentRoot({
    parentRoot: parsed.parentRoot,
    useTempParentRoot: parsed.useTempParentRoot,
    agentLayerRoot: parsed.agentLayerRoot,
  });

  if (parsed.commandArgs.length === 0) {
    throw new Error(LAUNCHER_USAGE);
  }

  const agentCmd = resolveAgentCommand(parsed.commandArgs);
  let commandArgs = [...parsed.commandArgs];

  if (agentCmd) {
    const config = loadAgentConfig(roots.agentLayerRoot);
    const enabled = getEnabledAgents(config);
    if (!enabled.has(agentCmd)) {
      throw new Error(
        [
          `ERROR: ${agentCmd} is disabled in .agent-layer/config/agents.json.`,
          "",
          "Enable it, then re-run:",
          "  ./al --sync",
          `Then launch: ./al ${agentCmd}`,
        ].join("\n"),
      );
    }
    const defaults = config[agentCmd]?.defaultArgs ?? [];
    commandArgs = applyDefaultArgs(commandArgs, defaults);
  }

  try {
    if (agentCmd === "codex") {
      warnOnExternalCodexHome(roots.parentRoot);
    }
    const syncArgs = {
      check: false,
      verbose: false,
      overwrite: false,
      interactive: false,
    };
    if (!parsed.noSync) {
      await runSync(roots.parentRoot, roots.agentLayerRoot, syncArgs);
    }

    const env = buildRuntimeEnv(
      roots.parentRoot,
      roots.agentLayerRoot,
      agentCmd === "codex",
    );
    const argv0 = commandArgs.length
      ? `al:${path.basename(commandArgs[0])}`
      : "al";
    return runCommand(commandArgs, env, argv0);
  } finally {
    if (
      roots.tempParentRootCreated &&
      process.env.PARENT_ROOT_KEEP_TEMP !== "1"
    ) {
      roots.cleanupTempParentRoot();
    }
  }
}

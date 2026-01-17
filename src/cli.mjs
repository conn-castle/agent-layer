#!/usr/bin/env node
import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { spawnSync } from "node:child_process";
import {
  LAUNCHER_USAGE,
  runLauncher,
  warnOnExternalCodexHome,
} from "./lib/launcher.mjs";
import { removeGeneratedArtifacts } from "./lib/cleanup.mjs";
import { resolveAgentLayerRoot, resolveParentRoot } from "./lib/roots.mjs";
import { runSetup, SETUP_USAGE } from "./lib/setup.mjs";

const CLI_USAGE = [
  "Usage:",
  "  ./al [--no-sync] [--parent-root <path>] [--temp-parent-root] <command> [args...]",
  "",
  "Admin:",
  "  ./al --sync [--check] [--verbose] [--overwrite] [--interactive] [--parent-root <path>] [--temp-parent-root] [--agent-layer-root <path>]",
  "  ./al --inspect [--parent-root <path>] [--temp-parent-root] [--agent-layer-root <path>]",
  "  ./al --clean [--parent-root <path>] [--temp-parent-root] [--agent-layer-root <path>]",
  "  ./al --setup [--skip-checks] [--parent-root <path>] [--temp-parent-root] [--agent-layer-root <path>]",
  "  ./al --wizard [--parent-root <path>] [--temp-parent-root] [--agent-layer-root <path>]",
  "  ./al --mcp-prompts [--parent-root <path>] [--temp-parent-root] [--agent-layer-root <path>]",
  "  ./al --open-vscode [--parent-root <path>] [--temp-parent-root] [--agent-layer-root <path>]",
  "  ./al --version",
  "  ./al --help",
].join("\n");

const MODE_FLAGS = new Map([
  ["--sync", "sync"],
  ["--inspect", "inspect"],
  ["--clean", "clean"],
  ["--setup", "setup"],
  ["--wizard", "wizard"],
  ["--mcp-prompts", "mcp-prompts"],
  ["--open-vscode", "open-vscode"],
  ["--install-config", "install-config"],
  ["--version", "version"],
]);

/**
 * Parse root flags from argv.
 * @param {string[]} argv
 * @returns {{ parentRoot: string | null, agentLayerRoot: string | null, useTempParentRoot: boolean, rest: string[], doubleDashArgs: string[] | null }}
 */
function parseRootFlags(argv) {
  let parentRoot = null;
  let agentLayerRoot = null;
  let useTempParentRoot = false;
  /** @type {string[]} */
  const rest = [];
  /** @type {string[] | null} */
  let doubleDashArgs = null;

  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === "--") {
      doubleDashArgs = argv.slice(i + 1);
      break;
    }
    if (arg === "--parent-root") {
      const value = argv[i + 1];
      if (!value || value.startsWith("--")) {
        throw new Error("agent-layer cli: --parent-root requires a path.");
      }
      if (parentRoot) {
        throw new Error(
          "agent-layer cli: --parent-root provided more than once.",
        );
      }
      parentRoot = value;
      i += 1;
      continue;
    }
    if (arg.startsWith("--parent-root=")) {
      const value = arg.slice("--parent-root=".length);
      if (!value) {
        throw new Error("agent-layer cli: --parent-root requires a path.");
      }
      if (parentRoot) {
        throw new Error(
          "agent-layer cli: --parent-root provided more than once.",
        );
      }
      parentRoot = value;
      continue;
    }
    if (arg === "--agent-layer-root") {
      const value = argv[i + 1];
      if (!value || value.startsWith("--")) {
        throw new Error("agent-layer cli: --agent-layer-root requires a path.");
      }
      if (agentLayerRoot) {
        throw new Error(
          "agent-layer cli: --agent-layer-root provided more than once.",
        );
      }
      agentLayerRoot = value;
      i += 1;
      continue;
    }
    if (arg.startsWith("--agent-layer-root=")) {
      const value = arg.slice("--agent-layer-root=".length);
      if (!value) {
        throw new Error("agent-layer cli: --agent-layer-root requires a path.");
      }
      if (agentLayerRoot) {
        throw new Error(
          "agent-layer cli: --agent-layer-root provided more than once.",
        );
      }
      agentLayerRoot = value;
      continue;
    }
    if (arg === "--temp-parent-root") {
      useTempParentRoot = true;
      continue;
    }
    rest.push(arg);
  }

  return {
    parentRoot,
    agentLayerRoot,
    useTempParentRoot,
    rest,
    doubleDashArgs,
  };
}

/**
 * Parse control flags for the CLI.
 * @param {string[]} argv
 * @param {string[] | null} doubleDashArgs
 * @returns {{ mode: string, modeArgs: string[], commandArgs: string[], skipChecks: boolean, noSync: boolean }}
 */
function parseControlFlags(argv, doubleDashArgs) {
  let mode = "launch";
  let skipChecks = false;
  let noSync = false;
  /** @type {string[]} */
  const modeArgs = [];
  /** @type {string[]} */
  let commandArgs = [];

  let commandStart = -1;
  for (let i = 0; i < argv.length; i++) {
    if (!argv[i].startsWith("-")) {
      commandStart = i;
      break;
    }
  }

  const head = commandStart === -1 ? argv : argv.slice(0, commandStart);
  commandArgs = commandStart === -1 ? [] : argv.slice(commandStart);

  for (const arg of head) {
    const mapped = MODE_FLAGS.get(arg);
    if (mapped) {
      if (mode !== "launch") {
        throw new Error("agent-layer cli: choose only one mode flag.");
      }
      mode = mapped;
      continue;
    }
    if (arg === "--skip-checks") {
      skipChecks = true;
      continue;
    }
    if (arg === "--no-sync") {
      noSync = true;
      continue;
    }
    modeArgs.push(arg);
  }

  if (doubleDashArgs) {
    if (mode !== "launch") {
      throw new Error(
        "agent-layer cli: -- is only valid when launching a command.",
      );
    }
    if (commandArgs.length === 0) {
      commandArgs = [...doubleDashArgs];
    } else {
      commandArgs = [...commandArgs, ...doubleDashArgs];
    }
  }

  return { mode, modeArgs, commandArgs, skipChecks, noSync };
}

/**
 * Print the requested output and exit.
 * @param {string} text
 * @param {number} code
 * @returns {never}
 */
function exitWith(text, code) {
  process.stderr.write(`${text}\n`);
  process.exit(code);
}

/**
 * Print the installed agent-layer version.
 * @param {string} agentLayerRoot
 * @returns {void}
 */
function printVersion(agentLayerRoot) {
  const gitExists = spawnSync("git", ["--version"], { stdio: "ignore" });
  if (gitExists.error && gitExists.error.code === "ENOENT") {
    console.log("unknown");
    return;
  }
  const result = spawnSync(
    "git",
    ["-C", agentLayerRoot, "describe", "--tags", "--always", "--dirty"],
    { encoding: "utf8" },
  );
  const version = result.status === 0 ? String(result.stdout ?? "").trim() : "";
  console.log(version || "unknown");
}

/**
 * Launch VS Code with repo-local CODEX_HOME when unset.
 * @param {{ parentRoot: string }} roots
 * @returns {void}
 */
function runOpenVscode(roots) {
  const codexHome = path.join(roots.parentRoot, ".codex");
  const env = { ...process.env };
  const rawCodexHome = (env.CODEX_HOME ?? "").trim();
  if (rawCodexHome) {
    warnOnExternalCodexHome(roots.parentRoot);
  } else {
    if (!fs.existsSync(codexHome)) {
      throw new Error(
        `error: CODEX_HOME directory not found at ${codexHome}\nRun: ./al --sync`,
      );
    }
    env.CODEX_HOME = codexHome;
  }

  const codeResult = spawnSync("code", ["--version"], { stdio: "ignore" });
  if (codeResult.error && codeResult.error.code === "ENOENT") {
    throw new Error(
      "error: VS Code 'code' CLI not found in PATH.\nIn VS Code, run: Shell Command: Install 'code' command in PATH",
    );
  }

  const result = spawnSync("code", [roots.parentRoot], {
    stdio: "inherit",
    env,
  });
  if (result.error) throw result.error;
  if (typeof result.status === "number" && result.status !== 0) {
    throw new Error(`error: failed to launch VS Code (exit ${result.status})`);
  }
}

/**
 * Print a cleanup summary.
 * @param {{ removed: string[], missing: string[] }} result
 * @param {string} parentRoot
 * @returns {void}
 */
function printCleanupSummary(result, parentRoot) {
  if (result.removed.length === 0) {
    console.log("No generated files removed.");
  } else {
    console.log("Removed generated files:");
    for (const item of result.removed) {
      const rel = path.relative(parentRoot, item) || item;
      console.log(`  - ${rel}`);
    }
  }

  if (result.missing.length > 0) {
    console.log("");
    console.log("Not found (already clean or never generated):");
    for (const item of result.missing) {
      const rel = path.relative(parentRoot, item) || item;
      console.log(`  - ${rel}`);
    }
  }
}

/**
 * Resolve roots and run a task with temp cleanup.
 * @param {{ parentRoot: string | null, useTempParentRoot: boolean, agentLayerRoot: string | null }} options
 * @param {(roots: import("./lib/roots.mjs").ResolvedRoots) => Promise<void> | void} task
 * @returns {Promise<void>}
 */
async function withResolvedRoots(options, task) {
  const roots = resolveParentRoot({
    parentRoot: options.parentRoot,
    useTempParentRoot: options.useTempParentRoot,
    agentLayerRoot: options.agentLayerRoot,
  });
  try {
    await task(roots);
  } finally {
    if (
      roots.tempParentRootCreated &&
      process.env.PARENT_ROOT_KEEP_TEMP !== "1"
    ) {
      roots.cleanupTempParentRoot();
    }
  }
}

/**
 * Run the CLI entrypoint.
 * @returns {Promise<void>}
 */
async function main() {
  const argv = process.argv.slice(2);
  if (argv.length === 0 || argv[0] === "--help" || argv[0] === "-h") {
    console.log(CLI_USAGE);
    return;
  }

  const {
    parentRoot,
    agentLayerRoot,
    useTempParentRoot,
    rest,
    doubleDashArgs,
  } = parseRootFlags(argv);
  const { mode, modeArgs, commandArgs, skipChecks, noSync } = parseControlFlags(
    rest,
    doubleDashArgs,
  );

  if (mode === "version") {
    if (
      modeArgs.length > 0 ||
      commandArgs.length > 0 ||
      skipChecks ||
      noSync ||
      parentRoot ||
      agentLayerRoot ||
      useTempParentRoot
    ) {
      exitWith(
        "agent-layer cli: --version does not accept other arguments.",
        2,
      );
    }
    printVersion(resolveAgentLayerRoot(null));
    return;
  }

  if (modeArgs.includes("--help") || modeArgs.includes("-h")) {
    if (mode === "launch") {
      console.log(LAUNCHER_USAGE);
      return;
    }
    if (mode === "sync") {
      const { SYNC_USAGE } = await import("./sync/sync.mjs");
      console.log(SYNC_USAGE);
      return;
    }
    if (mode === "inspect") {
      const { INSPECT_USAGE } = await import("./sync/inspect.mjs");
      console.log(INSPECT_USAGE);
      return;
    }
    if (mode === "clean") {
      const { CLEAN_USAGE } = await import("./sync/clean.mjs");
      console.log(CLEAN_USAGE);
      return;
    }
    if (mode === "setup") {
      console.log(SETUP_USAGE);
      return;
    }
    if (mode === "wizard") {
      console.log(
        "Usage: ./al --wizard [--parent-root <path>] [--temp-parent-root] [--agent-layer-root <path>]\n\nInteractively configure agent enablement and model defaults.",
      );
      return;
    }
    if (mode === "mcp-prompts") {
      console.log(
        "Usage: ./al --mcp-prompts [--parent-root <path>] [--temp-parent-root] [--agent-layer-root <path>]",
      );
      return;
    }
    if (mode === "open-vscode") {
      console.log(
        "Usage: ./al --open-vscode [--parent-root <path>] [--temp-parent-root] [--agent-layer-root <path>]",
      );
      return;
    }
    if (mode === "install-config") {
      const { INSTALL_CONFIG_USAGE } = await import("./lib/install-config.mjs");
      console.log(INSTALL_CONFIG_USAGE);
      return;
    }
  }

  if (mode !== "setup" && skipChecks) {
    exitWith("agent-layer cli: --skip-checks is only valid with --setup.", 2);
  }
  if (mode !== "launch" && noSync) {
    exitWith(
      "agent-layer cli: --no-sync is only valid when launching a command.",
      2,
    );
  }

  if (mode === "launch") {
    if (modeArgs.length > 0) {
      exitWith(`agent-layer cli: unknown arguments: ${modeArgs.join(" ")}`, 2);
    }
    if (commandArgs.length === 0) {
      exitWith(LAUNCHER_USAGE, 2);
    }
    const exitCode = await runLauncher({
      noSync,
      parentRoot,
      agentLayerRoot,
      useTempParentRoot,
      commandArgs,
    });
    process.exit(exitCode);
  }

  if (mode === "sync") {
    const { parseSyncArgs, runSync } = await import("./sync/sync.mjs");
    if (modeArgs.length === 0 && commandArgs.length > 0) {
      exitWith("agent-layer cli: sync does not accept extra arguments.", 2);
    }
    const syncArgs = parseSyncArgs(modeArgs);
    await withResolvedRoots(
      { parentRoot, useTempParentRoot, agentLayerRoot },
      async (roots) => {
        await runSync(roots.parentRoot, roots.agentLayerRoot, syncArgs);
      },
    );
    return;
  }

  if (mode === "inspect") {
    if (modeArgs.length > 0 || commandArgs.length > 0) {
      exitWith("agent-layer cli: inspect does not accept extra arguments.", 2);
    }
    await withResolvedRoots(
      { parentRoot, useTempParentRoot, agentLayerRoot },
      async (roots) => {
        try {
          const { buildInspectReport } = await import("./sync/inspect.mjs");
          const report = buildInspectReport(
            roots.parentRoot,
            roots.agentLayerRoot,
          );
          process.stdout.write(`${JSON.stringify(report, null, 2)}\n`);
        } catch (err) {
          const message = err instanceof Error ? err.message : String(err);
          process.stdout.write(
            `${JSON.stringify({ ok: false, error: message })}\n`,
          );
          process.exit(1);
        }
      },
    );
    return;
  }

  if (mode === "clean") {
    if (modeArgs.length > 0 || commandArgs.length > 0) {
      exitWith("agent-layer cli: clean does not accept extra arguments.", 2);
    }
    await withResolvedRoots(
      { parentRoot, useTempParentRoot, agentLayerRoot },
      async (roots) => {
        const { runClean } = await import("./sync/clean.mjs");
        runClean(roots.parentRoot, roots.agentLayerRoot);
        const cleanupResult = removeGeneratedArtifacts(roots.parentRoot);
        printCleanupSummary(cleanupResult, roots.parentRoot);
      },
    );
    return;
  }

  if (mode === "setup") {
    if (modeArgs.length > 0 || commandArgs.length > 0) {
      exitWith(SETUP_USAGE, 2);
    }
    await withResolvedRoots(
      { parentRoot, useTempParentRoot, agentLayerRoot },
      async (roots) => {
        await runSetup(roots, skipChecks);
      },
    );
    return;
  }

  if (mode === "wizard") {
    if (modeArgs.length > 0 || commandArgs.length > 0) {
      exitWith("agent-layer cli: wizard does not accept extra arguments.", 2);
    }
    const { runWizard } = await import("./lib/install-config.mjs");
    await withResolvedRoots(
      { parentRoot, useTempParentRoot, agentLayerRoot },
      async (roots) => {
        await runWizard(roots);
      },
    );
    return;
  }

  if (mode === "install-config") {
    if (commandArgs.length > 0) {
      exitWith(
        "agent-layer cli: install-config does not accept extra arguments.",
        2,
      );
    }
    const { parseInstallConfigArgs, runInstallConfig } =
      await import("./lib/install-config.mjs");
    const configArgs = parseInstallConfigArgs(modeArgs);
    await withResolvedRoots(
      { parentRoot, useTempParentRoot, agentLayerRoot },
      async (roots) => {
        await runInstallConfig(roots, configArgs);
      },
    );
    return;
  }

  if (mode === "mcp-prompts") {
    if (modeArgs.length > 0 || commandArgs.length > 0) {
      exitWith(
        "agent-layer cli: mcp-prompts does not accept extra arguments.",
        2,
      );
    }
    await withResolvedRoots(
      { parentRoot, useTempParentRoot, agentLayerRoot },
      async (roots) => {
        const { runPromptServer } =
          await import("./mcp/agent-layer-prompts/server.mjs");
        await runPromptServer(roots.parentRoot, roots.agentLayerRoot);
      },
    );
    return;
  }

  if (mode === "open-vscode") {
    if (modeArgs.length > 0 || commandArgs.length > 0) {
      exitWith(
        "agent-layer cli: open-vscode does not accept extra arguments.",
        2,
      );
    }
    await withResolvedRoots(
      { parentRoot, useTempParentRoot, agentLayerRoot },
      runOpenVscode,
    );
    return;
  }

  exitWith(CLI_USAGE, 2);
}

main().catch((err) => {
  const message = err instanceof Error ? err.message : String(err);
  console.error(
    message.startsWith("agent-layer") ? message : `agent-layer: ${message}`,
  );
  process.exit(1);
});

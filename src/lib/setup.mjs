import fs from "node:fs";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { runSync } from "../sync/sync.mjs";

export const SETUP_USAGE = [
  "Usage:",
  "  ./al --setup [--skip-checks] [--temp-parent-root] [--parent-root <path>]",
].join("\n");

/**
 * Run a command and return the result.
 * @param {string} command
 * @param {string[]} args
 * @param {string} cwd
 * @returns {import(\"node:child_process\").SpawnSyncReturns<Buffer>}
 */
function runCommand(command, args, cwd) {
  return spawnSync(command, args, { stdio: "inherit", cwd });
}

/**
 * Ensure a command is available.
 * @param {string} name
 * @param {string} errorMessage
 * @returns {void}
 */
function requireCommand(name, errorMessage) {
  const result = spawnSync(name, ["--version"], { stdio: "ignore" });
  if (result.error && result.error.code === "ENOENT") {
    throw new Error(errorMessage);
  }
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
 * Run setup tasks for the resolved roots.
 * @param {{ parentRoot: string, agentLayerRoot: string, isConsumerLayout: boolean, tempParentRootCreated: boolean, cleanupTempParentRoot: () => void }} roots
 * @param {boolean} skipChecks
 * @returns {Promise<void>}
 */
export async function runSetup(roots, skipChecks) {
  const agentLayerRoot = roots.agentLayerRoot;
  const parentRoot = roots.parentRoot;

  say(
    "Note: setup is required after install or config changes. ./al runs sync before each command.",
  );

  if (!fs.existsSync(agentLayerRoot)) {
    throw new Error(`Missing agent-layer root: ${agentLayerRoot}`);
  }
  if (!fs.existsSync(path.join(agentLayerRoot, "src", "cli.mjs"))) {
    throw new Error(`Missing src/cli.mjs under ${agentLayerRoot}.`);
  }

  requireCommand(
    "node",
    "Node.js is required (node not found). Install Node, then re-run.",
  );
  requireCommand(
    "npm",
    "npm is required (npm not found). Install npm/Node, then re-run.",
  );
  requireCommand("git", "git is required (git not found).");

  const gitCheck = spawnSync("git", ["rev-parse", "--is-inside-work-tree"], {
    stdio: "ignore",
    cwd: parentRoot,
  });
  const inGitRepo = gitCheck.status === 0;

  say("==> Running agent-layer sync");
  await runSync(parentRoot, agentLayerRoot, {
    check: false,
    verbose: false,
    overwrite: false,
    interactive: false,
  });

  say("==> Installing MCP prompt server dependencies");
  const promptPkg = path.join(
    agentLayerRoot,
    "src",
    "mcp",
    "agent-layer-prompts",
    "package.json",
  );
  if (!fs.existsSync(promptPkg)) {
    throw new Error(
      `Missing src/mcp/agent-layer-prompts/package.json under ${agentLayerRoot}`,
    );
  }
  const promptDir = path.dirname(promptPkg);
  const npmResult = runCommand("npm", ["install"], promptDir);
  if (npmResult.status !== 0) {
    throw new Error("npm install failed.");
  }

  if (inGitRepo) {
    say(
      "Skipping hook enable/test (dev-only; run .agent-layer/dev/bootstrap.sh).",
    );
  } else {
    say("Skipping hook enable/test (not a git repo).");
  }

  if (skipChecks) {
    say("==> Skipping sync check (--skip-checks)");
  } else {
    say("==> Verifying sync is up-to-date (check mode)");
    await runSync(parentRoot, agentLayerRoot, {
      check: true,
      verbose: false,
      overwrite: false,
      interactive: false,
    });
  }

  say("");
  say("Setup complete (manual steps below are required).");
  say("");
  if (roots.isConsumerLayout) {
    say("Required manual steps (do all of these):");
    say(
      "  1) Create/fill .agent-layer/.env (copy from .env.example; do not commit)",
    );
    say("  2) Review MCP servers: .agent-layer/config/mcp-servers.json");
    say("");
    say("Optional customization:");
    say("  - Edit instructions: .agent-layer/config/instructions/*.md");
    say("  - Edit workflows:    .agent-layer/config/workflows/*.md");
    say(
      "  - Disable MCP servers with enabled: false or limit with clients allowlist",
    );
    say("");
    say("Note: ./al automatically runs sync before each command.");
    say("To regenerate without launching a CLI:");
    say("  ./al --sync");
  } else {
    say(
      `Note: running from the agent-layer repo wrote outputs into: ${parentRoot}`,
    );
    say("Edit sources in config/ and re-run as needed.");
    say("Manual regen:");
    say(`  ./al --sync --parent-root "${parentRoot}"`);
  }
}

import path from "node:path";
import { REGEN_COMMAND } from "./constants.mjs";
import { fileExists, readUtf8, writeUtf8 } from "./utils.mjs";
import { classifyGeneratedOutput } from "../lib/generated-outputs.mjs";

/**
 * @typedef {{ check: boolean, verbose: boolean }} SyncArgs
 * @typedef {[string, string]} OutputEntry
 */

/**
 * Convert an absolute path to a repo-relative path.
 * @param {string} repoRoot
 * @param {string} absPath
 * @returns {string}
 */
function relPath(repoRoot, absPath) {
  const r = path.relative(repoRoot, absPath);
  return r.split(path.sep).join("/");
}

/**
 * Emit an out-of-date error and exit.
 * @param {string} repoRoot
 * @param {string[]} changedAbsPaths
 * @param {string} extraMessage
 * @returns {void}
 */
export function failOutOfDate(repoRoot, changedAbsPaths, extraMessage = "") {
  const rels = changedAbsPaths.map((p) => relPath(repoRoot, p));

  const instructionShims = [];
  const mcpConfigs = [];
  const commandAllowlistConfigs = [];
  const codexSkills = [];
  const vscodePrompts = [];
  const antigravityWorkflows = [];
  const other = [];

  for (const rp of rels) {
    let matched = false;
    const categories = classifyGeneratedOutput(rp);
    if (categories.instructionShims) {
      instructionShims.push(rp);
      matched = true;
    }
    if (categories.mcpConfigs) {
      mcpConfigs.push(rp);
      matched = true;
    }
    if (categories.commandAllowlistConfigs) {
      commandAllowlistConfigs.push(rp);
      matched = true;
    }
    if (categories.codexSkills) {
      codexSkills.push(rp);
      matched = true;
    }
    if (categories.vscodePrompts) {
      vscodePrompts.push(rp);
      matched = true;
    }
    if (categories.antigravityWorkflows) {
      antigravityWorkflows.push(rp);
      matched = true;
    }
    if (!matched) {
      other.push(rp);
    }
  }

  console.error("agent-layer sync: WARNING: generated files are out of date.");
  if (extraMessage) console.error(extraMessage);
  console.error("");
  console.error("What this means:");
  console.error(
    "  - One or more generated files differ from the source-of-truth in .agent-layer.",
  );
  console.error(
    "  - This can happen after edits to .agent-layer sources or when client configs diverge.",
  );
  console.error("");
  console.error("Do NOT edit generated files directly.");
  console.error("");

  if (instructionShims.length) {
    console.error(
      "Instruction shims (edit: .agent-layer/config/instructions/*.md):",
    );
    for (const p of instructionShims) console.error(`  - ${p}`);
    console.error("");
  }

  if (mcpConfigs.length) {
    console.error(
      "MCP config files (edit: .agent-layer/config/mcp-servers.json):",
    );
    for (const p of mcpConfigs) console.error(`  - ${p}`);
    console.error("");
  }

  if (commandAllowlistConfigs.length) {
    console.error(
      "Command allowlist configs (edit: .agent-layer/config/policy/commands.json):",
    );
    for (const p of commandAllowlistConfigs) console.error(`  - ${p}`);
    console.error("");
  }

  if (codexSkills.length) {
    console.error("Codex skills (edit: .agent-layer/config/workflows/*.md):");
    for (const p of codexSkills) console.error(`  - ${p}`);
    console.error("");
  }

  if (vscodePrompts.length) {
    console.error(
      "VS Code prompt files (edit: .agent-layer/config/workflows/*.md):",
    );
    for (const p of vscodePrompts) console.error(`  - ${p}`);
    console.error("");
  }

  if (antigravityWorkflows.length) {
    console.error(
      "Antigravity workflow files (edit: .agent-layer/config/workflows/*.md):",
    );
    for (const p of antigravityWorkflows) console.error(`  - ${p}`);
    console.error("");
  }

  if (other.length) {
    console.error("Other generated files:");
    for (const p of other) console.error(`  - ${p}`);
    console.error("");
  }

  console.error("Next steps:");
  console.error(`  1) Run: ${REGEN_COMMAND}`);
  console.error(`  2) Re-run: ${REGEN_COMMAND} --check`);
  console.error("");
  console.error("If step 2 still fails, check for divergence:");
  console.error("  3) Run: ./al --inspect");
  console.error(
    "  4) Update the .agent-layer sources listed above, then re-run sync",
  );
  console.error(
    "     Or re-run sync with --overwrite to discard client-only entries.",
  );
  console.error("");
  console.error(
    "If you accidentally edited a generated file, revert it (example):",
  );
  console.error("  git checkout -- .mcp.json");
  console.error("");
  console.error("Files that would change:");
  for (const p of rels.sort()) console.error(`  - ${p}`);

  process.exit(1);
}

/**
 * Diff expected outputs against disk, writing when not in check mode.
 * @param {OutputEntry[]} outputs
 * @param {SyncArgs} args
 * @param {string} repoRoot
 * @returns {boolean}
 */
export function diffOrWrite(outputs, args, repoRoot) {
  const changed = [];
  for (const [outPath, content] of outputs) {
    const old = fileExists(outPath) ? readUtf8(outPath) : null;
    if (old !== content) {
      changed.push(outPath);
      if (!args.check) writeUtf8(outPath, content);
    }
    if (args.verbose) {
      console.log(
        `${old === content ? "ok" : args.check ? "needs-update" : "wrote"}: ${outPath}`,
      );
    }
  }

  if (args.check && changed.length) {
    failOutOfDate(repoRoot, changed);
  }
  return changed.length > 0;
}

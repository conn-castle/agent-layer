import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { loadEnvFile } from "./env.mjs";
import { fileExists } from "../sync/utils.mjs";

const DEFAULT_AGENT_LAYER_ROOT = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
  "..",
);

/**
 * @typedef {object} ResolvedRoots
 * @property {string} parentRoot
 * @property {string} agentLayerRoot
 * @property {boolean} tempParentRootCreated
 * @property {boolean} isConsumerLayout
 * @property {() => void} cleanupTempParentRoot
 */

/**
 * Throw a root resolution error.
 * @param {string} message
 * @returns {never}
 */
function rootsFail(message) {
  throw new Error(message);
}

/**
 * Resolve the agent-layer root from an override or default.
 * @param {string|undefined|null} agentLayerRoot
 * @returns {string}
 */
export function resolveAgentLayerRoot(agentLayerRoot) {
  const raw = (agentLayerRoot ?? DEFAULT_AGENT_LAYER_ROOT).trim();
  if (!raw) {
    rootsFail("ERROR: Agent layer root override does not exist: <empty>");
  }
  if (agentLayerRoot && !fileExists(raw)) {
    rootsFail(`ERROR: Agent layer root override does not exist: ${raw}`);
  }
  if (!fileExists(raw)) {
    rootsFail(`ERROR: Agent layer root does not exist: ${raw}`);
  }
  const resolved = fs.realpathSync(raw);
  if (resolved === path.parse(resolved).root) {
    rootsFail(
      [
        "ERROR: Agent layer root is the filesystem root (/).",
        "",
        "This is invalid. The agent layer root must be a directory (e.g., .agent-layer)",
        "inside a parent repo.",
        "",
        "Fix:",
        "  - Reinstall agent-layer in a valid subdirectory",
      ].join("\n"),
    );
  }
  return resolved;
}

/**
 * Resolve a parent root path relative to a base directory.
 * @param {string} base
 * @param {string} target
 * @returns {string}
 */
function resolvePathFromBase(base, target) {
  if (path.isAbsolute(target)) return target;
  return path.resolve(base, target);
}

/**
 * Emit a missing parent root error message.
 * @param {string} pathValue
 * @param {string} source
 * @returns {string}
 */
function messageParentRootMissing(pathValue, source) {
  return [
    `ERROR: Parent root path does not exist: ${pathValue}`,
    "",
    `Source: ${source}`,
    "",
    "Fix:",
    "  - Create the directory, or",
    "  - Use a different path, or",
    "  - Use temp parent root for testing: --temp-parent-root",
  ].join("\n");
}

/**
 * Emit a missing .agent-layer error message.
 * @param {string} pathValue
 * @returns {string}
 */
function messageParentRootMissingAgentLayer(pathValue) {
  return [
    `ERROR: Parent root must contain .agent-layer/ (dir or symlink): ${pathValue}`,
    "",
    "Found directory but no .agent-layer/ inside.",
    "",
    "Fix:",
    "  - Install agent-layer in that directory",
    "  - Use a different path that contains .agent-layer/",
    "  - Use temp parent root for testing: --temp-parent-root",
  ].join("\n");
}

/**
 * Emit a parent root consistency error message.
 * @param {string} agentLayerReal
 * @param {string} parentAgentLayerReal
 * @param {string} parentRoot
 * @returns {string}
 */
function messageParentRootConsistency(
  agentLayerReal,
  parentAgentLayerReal,
  parentRoot,
) {
  return [
    "ERROR: Parent root .agent-layer/ does not match script location.",
    "",
    `Resolved script location: ${agentLayerReal}`,
    `Resolved parent config:   ${parentAgentLayerReal}`,
    "",
    "These must point to the same location. You are running scripts from one",
    "agent-layer installation but trying to configure a different one.",
    "",
    "Fix:",
    `  - Use scripts from ${parentRoot}/.agent-layer/`,
    "  - Or adjust --parent-root to match script location",
  ].join("\n");
}

/**
 * Emit a conflicting flags error message.
 * @returns {string}
 */
function messageConflictingFlags() {
  return [
    "ERROR: Conflicting flags: --parent-root and --temp-parent-root",
    "",
    "You provided both flags but they are mutually exclusive.",
    "Choose one:",
    "  - Use --parent-root <path> for explicit parent root",
    "  - Use --temp-parent-root to create temporary parent root",
  ].join("\n");
}

/**
 * Emit a temp parent root failure message.
 * @param {string} agentLayerRoot
 * @returns {string}
 */
function messageTempParentRootFailed(agentLayerRoot) {
  return [
    "ERROR: Failed to create temporary parent root directory.",
    "",
    "Attempted:",
    `  1. ${process.env.TMPDIR || os.tmpdir()}/agent-layer-temp-parent-root.XXXXXX`,
    `  2. ${agentLayerRoot}/tmp/agent-layer-temp-parent-root.XXXXXX`,
    "  3. Manual creation (if mktemp unavailable)",
    "",
    "Possible causes:",
    "  - Disk full (check: df -h)",
    "  - No write permission to temp directories",
    "  - $TMPDIR points to non-existent location",
    "",
    "Fix:",
    "  - Free disk space",
    "  - Set TMPDIR to writable location: export TMPDIR=/writable/path",
    "  - Use explicit parent root instead: --parent-root <path>",
  ].join("\n");
}

/**
 * Emit a temp parent root symlink failure message.
 * @param {string} tempDir
 * @param {string} agentLayerRoot
 * @returns {string}
 */
function messageTempParentRootSymlinkFailed(tempDir, agentLayerRoot) {
  return [
    "ERROR: Failed to create .agent-layer symlink in temp parent root.",
    "",
    `Path: ${tempDir}/.agent-layer -> ${agentLayerRoot}`,
    "",
    "Possible causes:",
    "  - Filesystem doesn't support symlinks (e.g., FAT32, some network mounts)",
    `  - Path already exists at ${tempDir}/.agent-layer`,
    "  - Permission denied",
    "",
    "Fix:",
    "  - Use filesystem that supports symlinks (ext4, APFS, HFS+)",
    "  - Or use explicit parent root: --parent-root <path>",
  ].join("\n");
}

/**
 * Emit a dev-repo parent root requirement message.
 * @param {string} agentLayerRoot
 * @returns {string}
 */
function messageDevRepoRequiresParentRoot(agentLayerRoot) {
  return [
    "ERROR: Running from agent-layer repo requires explicit parent root configuration.",
    "",
    "Context: Agent-layer development repo",
    "",
    "The agent-layer repo cannot auto-discover a parent root because it doesn't have",
    '".agent-layer" as its directory name. You must explicitly specify how to set up',
    "the test environment.",
    "",
    "Options (choose one):",
    "  1. Use temporary parent root (recommended for testing/CI):",
    "     ./al --setup --temp-parent-root",
    "     ./tests/run.sh --temp-parent-root",
    "",
    "  2. Specify explicit parent root (if you have a test consumer repo):",
    "     # NOTE: The test repo must have a symlink .agent-layer -> <this-repo>",
    "     ./al --setup --parent-root /path/to/test-repo",
    "     ./tests/run.sh --parent-root /path/to/test-repo",
    "",
    "  3. Set PARENT_ROOT in the agent-layer .env file:",
    `     ${path.join(agentLayerRoot, ".env")}`,
  ].join("\n");
}

/**
 * Emit a renamed agent-layer directory message.
 * @param {string} agentLayerRoot
 * @param {string} name
 * @returns {string}
 */
function messageRenamedAgentLayerDir(agentLayerRoot, name) {
  return [
    'ERROR: Cannot discover parent root - agent layer directory name is not ".agent-layer"',
    "",
    `Current name: ${name}`,
    "Expected: .agent-layer",
    "",
    'Discovery is only allowed when the agent layer root is named ".agent-layer".',
    "If you renamed it, discovery will not work.",
    "",
    "Options:",
    "  1. Rename directory to .agent-layer (if this is an installed agent layer)",
    "  2. Use explicit parent root: --parent-root <path>",
    "  3. Use temp parent root: --temp-parent-root",
  ].join("\n");
}

/**
 * Resolve parent root from .env when configured.
 * @param {string} agentLayerRoot
 * @returns {string|null}
 */
function resolveParentRootFromEnv(agentLayerRoot) {
  const envPath = path.join(agentLayerRoot, ".env");
  const loaded = loadEnvFile(envPath);
  if (!loaded.loaded) return null;
  if (!Object.prototype.hasOwnProperty.call(loaded.env, "PARENT_ROOT")) {
    return null;
  }
  const raw = String(loaded.env.PARENT_ROOT ?? "");
  const trimmed = raw.trim();
  if (!trimmed) {
    rootsFail(
      [
        "ERROR: PARENT_ROOT is set in .env but is empty.",
        "",
        `Path: ${envPath}`,
        "",
        "Fix:",
        "  - Set PARENT_ROOT to a valid path, or",
        "  - Remove PARENT_ROOT and use --parent-root or --temp-parent-root",
      ].join("\n"),
    );
  }
  return trimmed;
}

/**
 * Resolve explicit parent root and validate consistency.
 * @param {string} agentLayerRoot
 * @param {string} inputPath
 * @param {string} baseDir
 * @param {string} sourceLabel
 * @returns {{ parentRoot: string, tempParentRootCreated: boolean }}
 */
function resolveExplicitParentRoot(
  agentLayerRoot,
  inputPath,
  baseDir,
  sourceLabel,
) {
  const resolvedPath = resolvePathFromBase(baseDir, inputPath);
  if (!fileExists(resolvedPath) || !fs.statSync(resolvedPath).isDirectory()) {
    rootsFail(messageParentRootMissing(resolvedPath, sourceLabel));
  }
  const parentRootReal = fs.realpathSync(resolvedPath);
  const agentLayerEntry = path.join(parentRootReal, ".agent-layer");
  if (
    !fileExists(agentLayerEntry) ||
    (!fs.statSync(agentLayerEntry).isDirectory() &&
      !fs.lstatSync(agentLayerEntry).isSymbolicLink())
  ) {
    rootsFail(messageParentRootMissingAgentLayer(parentRootReal));
  }

  const agentLayerReal = fs.realpathSync(agentLayerRoot);
  const configuredAgentLayerReal = fs.realpathSync(agentLayerEntry);
  if (configuredAgentLayerReal !== agentLayerReal) {
    rootsFail(
      messageParentRootConsistency(
        agentLayerReal,
        configuredAgentLayerReal,
        parentRootReal,
      ),
    );
  }

  return { parentRoot: parentRootReal, tempParentRootCreated: false };
}

/**
 * Create a temporary parent root with a .agent-layer symlink.
 * @param {string} agentLayerRoot
 * @returns {{ parentRoot: string, cleanup: () => void }}
 */
function createTempParentRoot(agentLayerRoot) {
  const candidates = [];
  const envTmp = (process.env.TMPDIR ?? "").trim();
  if (envTmp) candidates.push(envTmp);
  candidates.push(os.tmpdir());
  candidates.push(path.join(agentLayerRoot, "tmp"));

  let tempDir = null;
  for (const base of candidates) {
    try {
      fs.mkdirSync(base, { recursive: true });
      tempDir = fs.mkdtempSync(
        path.join(base, "agent-layer-temp-parent-root."),
      );
      if (tempDir && fs.statSync(tempDir).isDirectory()) break;
    } catch {
      tempDir = null;
    }
  }

  if (!tempDir) {
    const fallbackRoot = path.join(agentLayerRoot, "tmp");
    try {
      fs.mkdirSync(fallbackRoot, { recursive: true });
      const manual = path.join(
        fallbackRoot,
        `agent-layer-temp-parent-root.${process.pid}`,
      );
      fs.mkdirSync(manual, { recursive: false });
      tempDir = manual;
    } catch {
      tempDir = null;
    }
  }

  if (!tempDir) {
    rootsFail(messageTempParentRootFailed(agentLayerRoot));
  }

  const resolvedTemp = fs.realpathSync(tempDir);
  const linkPath = path.join(resolvedTemp, ".agent-layer");
  try {
    fs.symlinkSync(agentLayerRoot, linkPath);
  } catch {
    try {
      fs.rmSync(resolvedTemp, { recursive: true, force: true });
    } catch {
      // Ignore cleanup failures here.
    }
    rootsFail(messageTempParentRootSymlinkFailed(resolvedTemp, agentLayerRoot));
  }

  return {
    parentRoot: resolvedTemp,
    cleanup: () => {
      try {
        fs.rmSync(resolvedTemp, { recursive: true, force: true });
      } catch {
        // Ignore cleanup failures.
      }
    },
  };
}

/**
 * Resolve parent root according to the Root Selection Specification.
 * @param {{ parentRoot?: string|null, useTempParentRoot?: boolean, agentLayerRoot?: string|null, cwd?: string }} options
 * @returns {ResolvedRoots}
 */
export function resolveParentRoot(options) {
  const parentRootArg = options?.parentRoot ?? null;
  const useTempParentRoot = options?.useTempParentRoot ?? false;
  const cwd = options?.cwd ?? process.cwd();
  const agentLayerRoot = resolveAgentLayerRoot(options?.agentLayerRoot ?? null);

  const basename = path.basename(agentLayerRoot);
  const isConsumerLayout = basename === ".agent-layer";

  if (parentRootArg && useTempParentRoot) {
    rootsFail(messageConflictingFlags());
  }

  let resolvedParentRoot = null;
  let tempParentRootCreated = false;
  let cleanupTempParentRoot = () => {};

  if (parentRootArg) {
    const result = resolveExplicitParentRoot(
      agentLayerRoot,
      parentRootArg,
      path.resolve(cwd),
      "--parent-root flag",
    );
    resolvedParentRoot = result.parentRoot;
    tempParentRootCreated = result.tempParentRootCreated;
  } else if (useTempParentRoot) {
    const temp = createTempParentRoot(agentLayerRoot);
    resolvedParentRoot = temp.parentRoot;
    tempParentRootCreated = true;
    cleanupTempParentRoot = temp.cleanup;
  } else {
    const envParentRoot = resolveParentRootFromEnv(agentLayerRoot);
    if (envParentRoot) {
      const result = resolveExplicitParentRoot(
        agentLayerRoot,
        envParentRoot,
        agentLayerRoot,
        `PARENT_ROOT in ${path.join(agentLayerRoot, ".env")}`,
      );
      resolvedParentRoot = result.parentRoot;
      tempParentRootCreated = result.tempParentRootCreated;
    } else if (isConsumerLayout) {
      const parentRoot = fs.realpathSync(path.join(agentLayerRoot, ".."));
      const configuredAgentLayer = fs.realpathSync(
        path.join(parentRoot, ".agent-layer"),
      );
      if (configuredAgentLayer !== agentLayerRoot) {
        rootsFail(
          messageParentRootConsistency(
            agentLayerRoot,
            configuredAgentLayer,
            parentRoot,
          ),
        );
      }
      resolvedParentRoot = parentRoot;
    } else if (basename === "agent-layer") {
      rootsFail(messageDevRepoRequiresParentRoot(agentLayerRoot));
    } else {
      rootsFail(messageRenamedAgentLayerDir(agentLayerRoot, basename));
    }
  }

  if (!resolvedParentRoot) {
    rootsFail("ERROR: Failed to resolve parent root.");
  }

  return {
    parentRoot: resolvedParentRoot,
    agentLayerRoot,
    tempParentRootCreated,
    isConsumerLayout,
    cleanupTempParentRoot,
  };
}

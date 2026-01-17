import fs from "node:fs";
import path from "node:path";
import { fileExists, readUtf8, readJsonRelaxed } from "../sync/utils.mjs";
import {
  CLEANUP_FILE_PATHS,
  ANTIGRAVITY_WORKFLOWS_DIR,
  CODEX_SKILLS_DIR,
  VSCODE_PROMPTS_DIR,
} from "./generated-outputs.mjs";

/**
 * @typedef {{ removed: string[], missing: string[] }} CleanupResult
 */

/**
 * Remove a file if it exists.
 * @param {string} filePath
 * @param {CleanupResult} result
 * @returns {void}
 */
function removeFile(filePath, result) {
  if (fileExists(filePath)) {
    fs.rmSync(filePath, { force: false });
    result.removed.push(filePath);
  } else {
    result.missing.push(filePath);
  }
}

/**
 * Check if a JSON object is effectively empty (no meaningful keys after clean).
 * @param {unknown} obj
 * @returns {boolean}
 */
function isEffectivelyEmptyJson(obj) {
  if (!obj || typeof obj !== "object" || Array.isArray(obj)) return false;
  const keys = Object.keys(obj);
  return keys.length === 0;
}

/**
 * Remove an MCP config file only if it's empty after cleaning.
 * These files may have custom entries that should be preserved.
 * @param {string} filePath
 * @param {CleanupResult} result
 * @returns {void}
 */
function removeMcpConfigIfEmpty(filePath, result) {
  if (!fileExists(filePath)) {
    result.missing.push(filePath);
    return;
  }

  try {
    const data = readJsonRelaxed(filePath, null);
    if (isEffectivelyEmptyJson(data)) {
      fs.rmSync(filePath, { force: false });
      result.removed.push(filePath);
    }
    // If not empty, file is preserved (has custom entries)
  } catch {
    // If we can't parse it, leave it alone
  }
}

/**
 * Remove a directory if it exists and is empty.
 * @param {string} dirPath
 * @param {CleanupResult} result
 * @returns {void}
 */
function removeEmptyDir(dirPath, result) {
  if (!fileExists(dirPath)) return;
  const entries = fs.readdirSync(dirPath);
  if (entries.length === 0) {
    fs.rmdirSync(dirPath);
    result.removed.push(`${dirPath}/`);
  }
}

/**
 * Remove generated artifacts under a parent root.
 * @param {string} parentRoot
 * @returns {CleanupResult}
 */
export function removeGeneratedArtifacts(parentRoot) {
  const result = { removed: [], missing: [] };
  for (const rel of CLEANUP_FILE_PATHS) {
    removeFile(path.join(parentRoot, rel), result);
  }

  // MCP config files are only removed if empty (custom entries preserved by runClean)
  removeMcpConfigIfEmpty(path.join(parentRoot, ".mcp.json"), result);
  removeMcpConfigIfEmpty(path.join(parentRoot, ".vscode/mcp.json"), result);

  const skillsRoot = path.join(parentRoot, CODEX_SKILLS_DIR);
  if (fileExists(skillsRoot)) {
    const entries = fs.readdirSync(skillsRoot, { withFileTypes: true });
    for (const entry of entries) {
      if (!entry.isDirectory()) continue;
      const skillDir = path.join(skillsRoot, entry.name);
      const skillFile = path.join(skillDir, "SKILL.md");
      removeFile(skillFile, result);
      removeEmptyDir(skillDir, result);
    }
    removeEmptyDir(skillsRoot, result);
  }

  const promptDir = path.join(parentRoot, VSCODE_PROMPTS_DIR);
  if (fileExists(promptDir)) {
    const entries = fs.readdirSync(promptDir, { withFileTypes: true });
    for (const entry of entries) {
      if (!entry.isFile()) continue;
      if (!entry.name.endsWith(".prompt.md")) continue;
      const promptPath = path.join(promptDir, entry.name);
      const content = readUtf8(promptPath);
      if (
        content.includes("GENERATED FILE") &&
        content.includes("Regenerate: ./al --sync")
      ) {
        removeFile(promptPath, result);
      }
    }
    removeEmptyDir(promptDir, result);
  }

  const antigravityDir = path.join(parentRoot, ANTIGRAVITY_WORKFLOWS_DIR);
  if (fileExists(antigravityDir)) {
    const entries = fs.readdirSync(antigravityDir, { withFileTypes: true });
    for (const entry of entries) {
      if (!entry.isFile()) continue;
      if (!entry.name.endsWith(".md")) continue;
      const workflowPath = path.join(antigravityDir, entry.name);
      const content = readUtf8(workflowPath);
      if (
        content.includes("GENERATED FILE") &&
        content.includes("Regenerate: ./al --sync")
      ) {
        removeFile(workflowPath, result);
      }
    }
    removeEmptyDir(antigravityDir, result);
    removeEmptyDir(path.dirname(antigravityDir), result);
  }

  return result;
}

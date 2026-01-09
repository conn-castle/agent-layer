import path from "node:path";
import { fileExists } from "./utils.mjs";

/**
 * Find the working repo root containing .agent-layer/.
 * @param {string} startDir
 * @returns {string | null}
 */
export function findWorkingRoot(startDir) {
  let dir = path.resolve(startDir);
  for (let i = 0; i < 50; i++) {
    if (fileExists(path.join(dir, ".agent-layer"))) return dir;
    const parent = path.dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }
  return null;
}

/**
 * Resolve the working repo root by searching for .agent-layer/.
 * @param {...string} startDirs
 * @returns {string | null}
 */
export function resolveWorkingRoot(...startDirs) {
  for (const start of startDirs) {
    if (!start) continue;
    const root = findWorkingRoot(start);
    if (root) return root;
  }
  return null;
}

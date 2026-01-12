import path from "node:path";

/**
 * Resolve roots from the environment when provided.
 * @returns {{ parentRoot: string, agentLayerRoot: string } | null}
 */
function resolveRootsFromEnv() {
  const parentRaw = String(process.env.PARENT_ROOT ?? "").trim();
  if (!parentRaw) return null;
  const parentRoot = path.resolve(parentRaw);
  const agentRaw = String(process.env.AGENT_LAYER_ROOT ?? "").trim();
  const agentLayerRoot = agentRaw
    ? path.resolve(agentRaw)
    : path.join(parentRoot, ".agent-layer");
  return { parentRoot, agentLayerRoot };
}

/**
 * Resolve roots from the entry script path without ancestor scans.
 * @param {string | undefined} entryPath
 * @returns {{ parentRoot: string, agentLayerRoot: string } | null}
 */
function resolveRootsFromScript(entryPath) {
  if (!entryPath) return null;
  const absolute = path.resolve(process.cwd(), entryPath);
  const parts = absolute.split(path.sep);
  const idx = parts.lastIndexOf(".agent-layer");
  if (idx < 0) return null;
  const agentLayerRoot = parts.slice(0, idx + 1).join(path.sep) || path.sep;
  const parentRoot = path.dirname(agentLayerRoot);
  return { parentRoot, agentLayerRoot };
}

/**
 * Resolve parent + agent-layer roots without filesystem discovery.
 * Uses env when provided, otherwise derives from the entry script path.
 * @param {string | undefined} entryPath
 * @returns {{ parentRoot: string, agentLayerRoot: string } | null}
 */
export function resolveRootsFromEnvOrScript(entryPath) {
  return resolveRootsFromEnv() ?? resolveRootsFromScript(entryPath);
}

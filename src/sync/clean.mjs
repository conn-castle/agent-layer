import path from "node:path";
import { fileURLToPath } from "node:url";
import {
  fileExists,
  isPlainObject,
  readJsonRelaxed,
  writeUtf8,
} from "./utils.mjs";
import { isManagedClaudeAllow, isManagedGeminiAllowed } from "./policy.mjs";
import { enabledServers, loadServerCatalog } from "./mcp.mjs";

/**
 * @typedef {Record<string, unknown>} JsonObject
 */

export const CLEAN_USAGE = [
  "Usage:",
  "  ./al --clean [--parent-root <path>] [--temp-parent-root] [--agent-layer-root <path>]",
].join("\n");

/**
 * Throw a clean-specific error.
 * @param {string} message
 * @returns {never}
 */
function fail(message) {
  throw new Error(`agent-layer clean: ${message}`);
}

/**
 * Load a JSON/JSONC file and assert it is a JSON object.
 * @param {string} filePath
 * @returns {JsonObject}
 */
function loadJsonObject(filePath) {
  let parsed;
  try {
    parsed = readJsonRelaxed(filePath, null);
  } catch (err) {
    const detail = err instanceof Error ? err.message : String(err);
    const cleaned = detail.replace(/^agent-layer sync:\s*/u, "");
    fail(`cannot parse ${filePath}: ${cleaned}`);
  }

  if (!isPlainObject(parsed)) {
    fail(`${filePath} must contain a JSON object`);
  }

  return parsed;
}

/**
 * Load and validate the MCP server catalog, mapping sync errors to clean errors.
 * @param {string} agentlayerRoot
 * @returns {{ defaults: Record<string, unknown>, servers: unknown[] }}
 */
function loadCatalog(agentlayerRoot) {
  try {
    return loadServerCatalog(agentlayerRoot);
  } catch (err) {
    const detail = err instanceof Error ? err.message : String(err);
    const cleaned = detail.replace(/^agent-layer sync:\s*/u, "");
    fail(cleaned);
  }
}

/**
 * Remove agent-layer-managed entries from Gemini settings.
 * @param {JsonObject} existing
 * @param {Set<string>} managedServers
 * @returns {{ updated: JsonObject, changed: boolean, removedAllowed: number, removedMcp: number }}
 */
function cleanGeminiSettings(existing, managedServers) {
  const updated = { ...existing };
  let changed = false;
  let removedAllowed = 0;
  let removedMcp = 0;

  const existingTools = existing.tools;
  if (existingTools !== undefined) {
    if (!isPlainObject(existingTools)) {
      fail(".gemini/settings.json: tools must be an object");
    }

    const tools = { ...existingTools };
    let toolsChanged = false;
    const existingAllowed = existingTools.allowed;
    if (existingAllowed !== undefined) {
      if (!Array.isArray(existingAllowed)) {
        fail(".gemini/settings.json: tools.allowed must be an array");
      }
      const preserved = existingAllowed.filter(
        (entry) => !isManagedGeminiAllowed(entry),
      );
      removedAllowed = existingAllowed.length - preserved.length;
      if (removedAllowed > 0) {
        toolsChanged = true;
        changed = true;
        if (preserved.length > 0) {
          tools.allowed = preserved;
        } else {
          delete tools.allowed;
        }
      }
    }

    if (toolsChanged) {
      if (Object.keys(tools).length === 0) {
        delete updated.tools;
      } else {
        updated.tools = tools;
      }
    }
  }

  const existingMcp = existing.mcpServers;
  if (existingMcp !== undefined) {
    if (!isPlainObject(existingMcp)) {
      fail(".gemini/settings.json: mcpServers must be an object");
    }

    let mcpChanged = false;
    /** @type {JsonObject} */
    const preserved = {};
    for (const [name, value] of Object.entries(existingMcp)) {
      if (managedServers.has(name)) {
        removedMcp += 1;
        mcpChanged = true;
      } else {
        preserved[name] = value;
      }
    }

    if (mcpChanged) {
      changed = true;
      if (Object.keys(preserved).length === 0) {
        delete updated.mcpServers;
      } else {
        updated.mcpServers = preserved;
      }
    }
  }

  return { updated, changed, removedAllowed, removedMcp };
}

/**
 * Remove agent-layer-managed entries from Claude settings.
 * @param {JsonObject} existing
 * @returns {{ updated: JsonObject, changed: boolean, removedAllow: number }}
 */
function cleanClaudeSettings(existing) {
  const updated = { ...existing };
  let changed = false;
  let removedAllow = 0;

  const permissions = existing.permissions;
  if (permissions !== undefined) {
    if (!isPlainObject(permissions)) {
      fail(".claude/settings.json: permissions must be an object");
    }

    const updatedPermissions = { ...permissions };
    let permissionsChanged = false;
    const existingAllow = permissions.allow;
    if (existingAllow !== undefined) {
      if (!Array.isArray(existingAllow)) {
        fail(".claude/settings.json: permissions.allow must be an array");
      }
      const preserved = existingAllow.filter(
        (entry) => !isManagedClaudeAllow(entry),
      );
      removedAllow = existingAllow.length - preserved.length;
      if (removedAllow > 0) {
        permissionsChanged = true;
        changed = true;
        if (preserved.length > 0) {
          updatedPermissions.allow = preserved;
        } else {
          delete updatedPermissions.allow;
        }
      }
    }

    if (permissionsChanged) {
      if (Object.keys(updatedPermissions).length === 0) {
        delete updated.permissions;
      } else {
        updated.permissions = updatedPermissions;
      }
    }
  }

  return { updated, changed, removedAllow };
}

/**
 * Remove agent-layer-managed entries from VS Code settings.
 * @param {JsonObject} existing
 * @returns {{ updated: JsonObject, changed: boolean }}
 */
function cleanVscodeSettings(existing) {
  const updated = { ...existing };
  let changed = false;

  if (
    Object.prototype.hasOwnProperty.call(
      existing,
      "chat.tools.terminal.autoApprove",
    )
  ) {
    delete updated["chat.tools.terminal.autoApprove"];
    changed = true;
  }

  return { updated, changed };
}

/**
 * Remove agent-layer-managed entries from VS Code MCP config.
 * @param {JsonObject} existing
 * @param {Set<string>} managedServers
 * @returns {{ updated: JsonObject, changed: boolean, removedServers: number }}
 */
function cleanVscodeMcpConfig(existing, managedServers) {
  const updated = { ...existing };
  let changed = false;
  let removedServers = 0;

  const servers = existing.servers;
  if (servers !== undefined) {
    if (!isPlainObject(servers)) {
      fail(".vscode/mcp.json: servers must be an object");
    }

    let serversChanged = false;
    /** @type {JsonObject} */
    const preserved = {};
    for (const [name, value] of Object.entries(servers)) {
      if (managedServers.has(name)) {
        removedServers += 1;
        serversChanged = true;
      } else {
        preserved[name] = value;
      }
    }

    if (serversChanged) {
      changed = true;
      if (Object.keys(preserved).length === 0) {
        delete updated.servers;
      } else {
        updated.servers = preserved;
      }
    }
  }

  return { updated, changed, removedServers };
}

/**
 * Remove agent-layer-managed entries from Claude MCP config.
 * @param {JsonObject} existing
 * @param {Set<string>} managedServers
 * @returns {{ updated: JsonObject, changed: boolean, removedServers: number }}
 */
function cleanClaudeMcpConfig(existing, managedServers) {
  const updated = { ...existing };
  let changed = false;
  let removedServers = 0;

  const servers = existing.mcpServers;
  if (servers !== undefined) {
    if (!isPlainObject(servers)) {
      fail(".mcp.json: mcpServers must be an object");
    }

    let serversChanged = false;
    /** @type {JsonObject} */
    const preserved = {};
    for (const [name, value] of Object.entries(servers)) {
      if (managedServers.has(name)) {
        removedServers += 1;
        serversChanged = true;
      } else {
        preserved[name] = value;
      }
    }

    if (serversChanged) {
      changed = true;
      if (Object.keys(preserved).length === 0) {
        delete updated.mcpServers;
      } else {
        updated.mcpServers = preserved;
      }
    }
  }

  return { updated, changed, removedServers };
}

/**
 * Write JSON to disk when changes are present.
 * @param {string} filePath
 * @param {JsonObject} updated
 * @param {boolean} changed
 * @returns {boolean}
 */
function writeIfChanged(filePath, updated, changed) {
  if (!changed) return false;
  writeUtf8(filePath, JSON.stringify(updated, null, 2) + "\n");
  return true;
}

/**
 * Remove agent-layer-managed settings from client config files.
 * @param {string} parentRoot
 * @param {string} agentLayerRoot
 * @returns {void}
 */
export function runClean(parentRoot, agentLayerRoot) {
  if (
    !parentRoot ||
    !agentLayerRoot ||
    typeof parentRoot !== "string" ||
    typeof agentLayerRoot !== "string"
  ) {
    fail("parentRoot and agentLayerRoot are required.");
  }
  const resolvedParent = path.resolve(parentRoot);
  const resolvedAgentLayer = path.resolve(agentLayerRoot);
  if (!fileExists(resolvedParent)) {
    fail(`parent root does not exist: ${resolvedParent}`);
  }
  if (!fileExists(resolvedAgentLayer)) {
    fail("Missing .agent-layer directory for this command.");
  }

  // Build client config paths relative to the parent root.
  const geminiPath = path.join(resolvedParent, ".gemini", "settings.json");
  const claudePath = path.join(resolvedParent, ".claude", "settings.json");
  const vscodePath = path.join(resolvedParent, ".vscode", "settings.json");
  const vscodeMcpPath = path.join(resolvedParent, ".vscode", "mcp.json");
  const claudeMcpPath = path.join(resolvedParent, ".mcp.json");

  const updates = [];
  let catalog = null;
  let managedServers = null;
  // Lazily resolve managed server names only when needed.
  const getManagedServers = () => {
    if (!managedServers) {
      if (!catalog) {
        catalog = loadCatalog(resolvedAgentLayer);
      }
      const names = new Set();
      const servers = enabledServers(catalog.servers ?? []);
      for (const server of servers) {
        if (server && typeof server.name === "string") names.add(server.name);
      }
      managedServers = names;
    }
    return managedServers;
  };

  // Clean each client config file if it exists.
  if (fileExists(geminiPath)) {
    const existing = loadJsonObject(geminiPath);
    const result = cleanGeminiSettings(
      existing,
      existing.mcpServers !== undefined ? getManagedServers() : new Set(),
    );
    if (writeIfChanged(geminiPath, result.updated, result.changed)) {
      updates.push(
        `.gemini/settings.json (removed ${result.removedAllowed} tools.allowed entries, ` +
          `${result.removedMcp} mcpServers)`,
      );
    }
  }

  if (fileExists(claudePath)) {
    const existing = loadJsonObject(claudePath);
    const result = cleanClaudeSettings(existing);
    if (writeIfChanged(claudePath, result.updated, result.changed)) {
      updates.push(
        `.claude/settings.json (removed ${result.removedAllow} allow entries)`,
      );
    }
  }

  if (fileExists(vscodePath)) {
    const existing = loadJsonObject(vscodePath);
    const result = cleanVscodeSettings(existing);
    if (writeIfChanged(vscodePath, result.updated, result.changed)) {
      updates.push(".vscode/settings.json (removed terminal auto-approve)");
    }
  }

  if (fileExists(vscodeMcpPath)) {
    const existing = loadJsonObject(vscodeMcpPath);
    const result = cleanVscodeMcpConfig(
      existing,
      existing.servers !== undefined ? getManagedServers() : new Set(),
    );
    if (writeIfChanged(vscodeMcpPath, result.updated, result.changed)) {
      updates.push(
        `.vscode/mcp.json (removed ${result.removedServers} server entries)`,
      );
    }
  }

  if (fileExists(claudeMcpPath)) {
    const existing = loadJsonObject(claudeMcpPath);
    const result = cleanClaudeMcpConfig(
      existing,
      existing.mcpServers !== undefined ? getManagedServers() : new Set(),
    );
    if (writeIfChanged(claudeMcpPath, result.updated, result.changed)) {
      updates.push(
        `.mcp.json (removed ${result.removedServers} mcpServers entries)`,
      );
    }
  }

  // Emit a summary if any files were updated.
  if (updates.length > 0) {
    console.log("agent-layer clean: updated settings");
    for (const entry of updates) {
      console.log(`  - ${entry}`);
    }
  }
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  console.error("agent-layer clean: use ./al --clean");
  process.exit(2);
}

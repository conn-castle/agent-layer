import path from "node:path";
import { assert, fileExists, readUtf8 } from "./utils.mjs";

/**
 * Validate the MCP server catalog schema.
 * @param {unknown} parsed
 * @param {string} filePath
 * @returns {void}
 */
export function validateServerCatalog(parsed, filePath) {
  assert(
    parsed && typeof parsed === "object",
    `${filePath} must contain a JSON object`,
  );

  if (parsed.defaults !== undefined) {
    assert(
      parsed.defaults &&
        typeof parsed.defaults === "object" &&
        !Array.isArray(parsed.defaults),
      `${filePath}: defaults must be an object`,
    );
    if (parsed.defaults.vscodeEnvFile !== undefined) {
      assert(
        typeof parsed.defaults.vscodeEnvFile === "string",
        `${filePath}: defaults.vscodeEnvFile must be a string`,
      );
    }
    if (parsed.defaults.trust !== undefined) {
      assert(
        typeof parsed.defaults.trust === "boolean",
        `${filePath}: defaults.trust must be boolean`,
      );
    }
    assert(
      !Object.prototype.hasOwnProperty.call(parsed.defaults, "geminiTrust"),
      `${filePath}: defaults.geminiTrust is not supported; use defaults.trust`,
    );
  }

  assert(
    Array.isArray(parsed.servers),
    `${filePath}: servers must be an array`,
  );

  const seen = new Set();
  for (const s of parsed.servers) {
    assert(
      s && typeof s === "object" && !Array.isArray(s),
      `${filePath}: each server must be an object`,
    );
    assert(
      typeof s.name === "string" && s.name.trim(),
      `${filePath}: server.name must be a non-empty string`,
    );
    assert(!seen.has(s.name), `${filePath}: duplicate server name "${s.name}"`);
    seen.add(s.name);

    if (s.enabled !== undefined) {
      assert(
        typeof s.enabled === "boolean",
        `${filePath}: ${s.name}.enabled must be boolean`,
      );
    }
    if (s.trust !== undefined) {
      assert(
        typeof s.trust === "boolean",
        `${filePath}: ${s.name}.trust must be boolean`,
      );
    }
    assert(
      !Object.prototype.hasOwnProperty.call(s, "geminiTrust"),
      `${filePath}: ${s.name}.geminiTrust is not supported; use ${s.name}.trust`,
    );

    if (s.transport !== undefined) {
      assert(
        typeof s.transport === "string",
        `${filePath}: ${s.name}.transport must be a string`,
      );
      assert(
        s.transport === "stdio",
        `${filePath}: ${s.name}.transport must be "stdio" (this generator supports only stdio currently)`,
      );
    }

    assert(
      typeof s.command === "string" && s.command.trim(),
      `${filePath}: ${s.name}.command must be a non-empty string`,
    );

    if (s.args !== undefined) {
      assert(
        Array.isArray(s.args),
        `${filePath}: ${s.name}.args must be an array`,
      );
      assert(
        s.args.every((x) => typeof x === "string"),
        `${filePath}: ${s.name}.args must be string[]`,
      );
    }

    if (s.envVars !== undefined) {
      assert(
        Array.isArray(s.envVars),
        `${filePath}: ${s.name}.envVars must be an array`,
      );
      assert(
        s.envVars.every((x) => typeof x === "string"),
        `${filePath}: ${s.name}.envVars must be string[]`,
      );
    }

    // Optional Gemini allow/deny lists.
    if (s.includeTools !== undefined) {
      assert(
        Array.isArray(s.includeTools),
        `${filePath}: ${s.name}.includeTools must be an array`,
      );
      assert(
        s.includeTools.every((x) => typeof x === "string"),
        `${filePath}: ${s.name}.includeTools must be string[]`,
      );
    }
    if (s.excludeTools !== undefined) {
      assert(
        Array.isArray(s.excludeTools),
        `${filePath}: ${s.name}.excludeTools must be an array`,
      );
      assert(
        s.excludeTools.every((x) => typeof x === "string"),
        `${filePath}: ${s.name}.excludeTools must be string[]`,
      );
    }
    if (s.includeTools !== undefined && s.excludeTools !== undefined) {
      throw new Error(
        `agent-layer sync: ${filePath}: ${s.name} cannot set both includeTools and excludeTools`,
      );
    }
  }
}

/**
 * Load and validate the MCP server catalog.
 * @param {string} agentlayerRoot
 * @returns {{ defaults: Record<string, unknown>, servers: unknown[] }}
 */
export function loadServerCatalog(agentlayerRoot) {
  const filePath = path.join(agentlayerRoot, "config", "mcp-servers.json");
  assert(fileExists(filePath), `${filePath} not found`);
  const parsed = JSON.parse(readUtf8(filePath));
  validateServerCatalog(parsed, filePath);
  const servers = Array.isArray(parsed.servers) ? parsed.servers : [];
  const defaults = parsed.defaults ?? {};
  return { defaults, servers };
}

/**
 * Return enabled servers from a validated catalog.
 * @param {unknown[]} servers
 * @returns {unknown[]}
 */
export function enabledServers(servers) {
  return servers.filter(
    (s) => s && s.name && (s.enabled === undefined || s.enabled === true),
  );
}

/**
 * Resolve default trust from catalog defaults.
 * @param {Record<string, unknown>} defaults
 * @returns {boolean}
 */
function resolveDefaultTrust(defaults) {
  return defaults.trust === undefined ? false : Boolean(defaults.trust);
}

/**
 * Resolve trust for a server with defaults fallback.
 * @param {Record<string, unknown>} defaults
 * @param {Record<string, unknown>} server
 * @returns {boolean}
 */
function resolveServerTrust(defaults, server) {
  if (server.trust === undefined) {
    return resolveDefaultTrust(defaults);
  }
  return Boolean(server.trust);
}

/**
 * List trusted server names using defaults as a fallback.
 * @param {{ defaults?: Record<string, unknown>, servers?: unknown[] }} catalog
 * @returns {string[]}
 */
export function trustedServerNames(catalog) {
  const defaults = catalog.defaults ?? {};
  const servers = enabledServers(catalog.servers ?? []);
  const trusted = [];
  for (const s of servers) {
    if (resolveServerTrust(defaults, s)) trusted.push(s.name);
  }
  return trusted;
}

/**
 * Build MCP config objects for each client from the server catalog.
 * @param {{ defaults?: Record<string, unknown>, servers?: unknown[] }} catalog
 * @returns {{ vscode: Record<string, unknown>, claude: Record<string, unknown>, gemini: Record<string, unknown> }}
 */
export function buildMcpConfigs(catalog) {
  const defaults = catalog.defaults ?? {};
  const servers = enabledServers(catalog.servers ?? []);

  // NOTE: VS Code can load env from an envFile. Default remains project root .env
  // unless you set defaults.vscodeEnvFile to "${workspaceFolder}/.agent-layer/.env".
  const vscodeEnvFile = defaults.vscodeEnvFile ?? "${workspaceFolder}/.env";

  // VS Code
  const vscode = { servers: {} };
  for (const s of servers) {
    vscode.servers[s.name] = {
      type: "stdio",
      command: s.command,
      args: s.args ?? [],
      envFile: vscodeEnvFile,
    };
  }

  // Claude Code
  const claude = { mcpServers: {} };
  for (const s of servers) {
    const env = {};
    for (const v of s.envVars ?? []) env[v] = `\${${v}}`;
    claude.mcpServers[s.name] = {
      command: s.command,
      args: s.args ?? [],
      ...(Object.keys(env).length ? { env } : {}),
    };
  }

  // Gemini CLI
  const gemini = { mcpServers: {} };
  for (const s of servers) {
    const env = {};
    for (const v of s.envVars ?? []) env[v] = `\${${v}}`;

    const trust = resolveServerTrust(defaults, s);

    const entry = {
      command: s.command,
      args: s.args ?? [],
      ...(Object.keys(env).length ? { env } : {}),
      trust,
    };

    if (Array.isArray(s.includeTools)) entry.includeTools = s.includeTools;
    if (Array.isArray(s.excludeTools)) entry.excludeTools = s.excludeTools;

    gemini.mcpServers[s.name] = entry;
  }

  return { vscode, claude, gemini };
}

/**
 * Render a TOML string value.
 * @param {string} value
 * @returns {string}
 */
function tomlString(value) {
  return JSON.stringify(String(value));
}

/**
 * Render a TOML array of string values.
 * @param {string[]} values
 * @returns {string}
 */
function tomlArray(values) {
  return `[${values.map(tomlString).join(", ")}]`;
}

/**
 * Render a TOML key (quoted when needed).
 * @param {string} key
 * @returns {string}
 */
function tomlKey(key) {
  const name = String(key);
  if (/^[A-Za-z0-9_-]+$/.test(name)) return name;
  return JSON.stringify(name);
}

/**
 * Build Codex config.toml content for MCP servers.
 * @param {{ defaults?: Record<string, unknown>, servers?: unknown[] }} catalog
 * @param {string} regenCommand
 * @returns {string}
 */
export function renderCodexConfig(catalog, regenCommand) {
  const servers = enabledServers(catalog.servers ?? []);
  const lines = [
    "# GENERATED FILE - DO NOT EDIT DIRECTLY",
    "# Source: .agent-layer/config/mcp-servers.json",
    `# Regenerate: ${regenCommand}`,
    "",
  ];

  if (servers.length === 0) {
    lines.push("# No MCP servers enabled.");
    lines.push("");
    return lines.join("\n");
  }

  for (const s of servers) {
    if (Array.isArray(s.envVars) && s.envVars.length) {
      lines.push(`# Requires env: ${s.envVars.join(", ")}`);
    }
    lines.push(`[mcp_servers.${tomlKey(s.name)}]`);
    lines.push(`command = ${tomlString(s.command)}`);
    if (Array.isArray(s.args) && s.args.length) {
      lines.push(`args = ${tomlArray(s.args)}`);
    }
    lines.push("");
  }

  return lines.join("\n");
}

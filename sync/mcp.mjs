import path from "node:path";
import { assert, fileExists, readUtf8 } from "./utils.mjs";

/**
 * Validate the MCP server catalog schema.
 * @param {unknown} parsed
 * @param {string} filePath
 * @returns {void}
 */
export function validateServerCatalog(parsed, filePath) {
  assert(parsed && typeof parsed === "object", `${filePath} must contain a JSON object`);

  if (parsed.defaults !== undefined) {
    assert(
      parsed.defaults && typeof parsed.defaults === "object" && !Array.isArray(parsed.defaults),
      `${filePath}: defaults must be an object`
    );
    if (parsed.defaults.vscodeEnvFile !== undefined) {
      assert(
        typeof parsed.defaults.vscodeEnvFile === "string",
        `${filePath}: defaults.vscodeEnvFile must be a string`
      );
    }
    // Back-compat: allow defaults.geminiTrust but prefer defaults.trust.
    if (parsed.defaults.trust !== undefined) {
      assert(
        typeof parsed.defaults.trust === "boolean",
        `${filePath}: defaults.trust must be boolean`
      );
    }
    if (parsed.defaults.geminiTrust !== undefined) {
      assert(
        typeof parsed.defaults.geminiTrust === "boolean",
        `${filePath}: defaults.geminiTrust must be boolean`
      );
    }
  }

  assert(Array.isArray(parsed.servers), `${filePath}: servers must be an array`);

  const seen = new Set();
  for (const s of parsed.servers) {
    assert(s && typeof s === "object" && !Array.isArray(s), `${filePath}: each server must be an object`);
    assert(typeof s.name === "string" && s.name.trim(), `${filePath}: server.name must be a non-empty string`);
    assert(!seen.has(s.name), `${filePath}: duplicate server name "${s.name}"`);
    seen.add(s.name);

    if (s.enabled !== undefined) {
      assert(typeof s.enabled === "boolean", `${filePath}: ${s.name}.enabled must be boolean`);
    }
    if (s.trust !== undefined) {
      assert(typeof s.trust === "boolean", `${filePath}: ${s.name}.trust must be boolean`);
    }
    // Back-compat: per-server geminiTrust (prefer trust)
    if (s.geminiTrust !== undefined) {
      assert(typeof s.geminiTrust === "boolean", `${filePath}: ${s.name}.geminiTrust must be boolean`);
    }

    if (s.transport !== undefined) {
      assert(typeof s.transport === "string", `${filePath}: ${s.name}.transport must be a string`);
      assert(
        s.transport === "stdio",
        `${filePath}: ${s.name}.transport must be "stdio" (this generator supports only stdio currently)`
      );
    }

    assert(typeof s.command === "string" && s.command.trim(), `${filePath}: ${s.name}.command must be a non-empty string`);

    if (s.args !== undefined) {
      assert(Array.isArray(s.args), `${filePath}: ${s.name}.args must be an array`);
      assert(s.args.every((x) => typeof x === "string"), `${filePath}: ${s.name}.args must be string[]`);
    }

    if (s.envVars !== undefined) {
      assert(Array.isArray(s.envVars), `${filePath}: ${s.name}.envVars must be an array`);
      assert(s.envVars.every((x) => typeof x === "string"), `${filePath}: ${s.name}.envVars must be string[]`);
    }

    // Optional Gemini allow/deny lists.
    if (s.includeTools !== undefined) {
      assert(Array.isArray(s.includeTools), `${filePath}: ${s.name}.includeTools must be an array`);
      assert(s.includeTools.every((x) => typeof x === "string"), `${filePath}: ${s.name}.includeTools must be string[]`);
    }
    if (s.excludeTools !== undefined) {
      assert(Array.isArray(s.excludeTools), `${filePath}: ${s.name}.excludeTools must be an array`);
      assert(s.excludeTools.every((x) => typeof x === "string"), `${filePath}: ${s.name}.excludeTools must be string[]`);
    }
    if (s.includeTools !== undefined && s.excludeTools !== undefined) {
      throw new Error(
        `agent-layer sync: ${filePath}: ${s.name} cannot set both includeTools and excludeTools`
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
  const filePath = path.join(agentlayerRoot, "mcp", "servers.json");
  if (!fileExists(filePath)) return { defaults: {}, servers: [] };
  const parsed = JSON.parse(readUtf8(filePath));
  validateServerCatalog(parsed, filePath);
  const servers = Array.isArray(parsed.servers) ? parsed.servers : [];
  const defaults = parsed.defaults ?? {};
  return { defaults, servers };
}

/**
 * Return enabled servers after validation.
 * @param {unknown[]} servers
 * @returns {unknown[]}
 */
export function enabledServers(servers) {
  const enabled = servers.filter(
    (s) => s && s.name && (s.enabled === undefined || s.enabled === true)
  );

  // Validate schema to avoid silently generating broken configs.
  for (const s of enabled) {
    const transport = s.transport ?? "stdio";
    if (transport !== "stdio") {
      throw new Error(
        `agent-layer sync: unsupported transport '${transport}' for server '${s.name}'. ` +
          "This generator currently supports only stdio servers."
      );
    }
    if (!s.command || typeof s.command !== "string") {
      throw new Error(`agent-layer sync: server '${s.name}' missing valid 'command'.`);
    }
    if (s.args !== undefined && !Array.isArray(s.args)) {
      throw new Error(`agent-layer sync: server '${s.name}' has non-array 'args'.`);
    }
    if (s.envVars !== undefined && !Array.isArray(s.envVars)) {
      throw new Error(`agent-layer sync: server '${s.name}' has non-array 'envVars'.`);
    }
  }

  return enabled;
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

  // Single generic trust field (applied to Gemini today; ignored elsewhere).
  // Back-compat: accept defaults.geminiTrust if defaults.trust is not present.
  const defaultTrust =
    defaults.trust === undefined
      ? (defaults.geminiTrust === undefined ? false : Boolean(defaults.geminiTrust))
      : Boolean(defaults.trust);

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

    // Back-compat: per-server geminiTrust if trust is not present.
    const trust =
      s.trust === undefined
        ? (s.geminiTrust === undefined ? defaultTrust : Boolean(s.geminiTrust))
        : Boolean(s.trust);

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

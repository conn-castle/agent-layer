import path from "node:path";
import { assert, fileExists, isPlainObject, readUtf8 } from "./utils.mjs";

const KNOWN_CLIENTS = new Set(["claude", "codex", "gemini", "vscode"]);

/**
 * Read a single environment variable from an .env file.
 * @param {string} filePath
 * @param {string} name
 * @returns {string|null}
 */
function readEnvVarFromFile(filePath, name) {
  if (!fileExists(filePath)) return null;
  const lines = readUtf8(filePath).split(/\r?\n/);
  const keyPattern = new RegExp(`^${name}\\s*=`);
  let found = null;

  for (const rawLine of lines) {
    const trimmed = rawLine.trim();
    if (!trimmed || trimmed.startsWith("#")) continue;
    const line = trimmed.startsWith("export ")
      ? trimmed.slice("export ".length).trim()
      : trimmed;

    if (!line.startsWith(name)) continue;
    if (!keyPattern.test(line)) {
      assert(false, `${filePath}: invalid ${name} entry (use ${name}=<value>)`);
    }
    if (found !== null) {
      assert(
        false,
        `${filePath}: multiple ${name} entries found (keep only one)`,
      );
    }
    const value = line.slice(line.indexOf("=") + 1).trim();
    if (!value) {
      found = "";
      continue;
    }
    const quote = value[0];
    if (
      (quote === "'" || quote === '"') &&
      value.length >= 2 &&
      value[value.length - 1] === quote
    ) {
      found = value.slice(1, -1);
    } else {
      found = value;
    }
  }

  return found;
}

/**
 * Resolve an environment variable from process.env or the local .env.
 * @param {string} name
 * @param {string} agentLayerRoot
 * @returns {string|null}
 */
function getEnvVarValue(name, agentLayerRoot) {
  const direct = process.env[name];
  if (typeof direct === "string" && direct.trim()) return direct.trim();
  const envPath = path.join(agentLayerRoot, ".env");
  const fileValue = readEnvVarFromFile(envPath, name);
  if (fileValue && String(fileValue).trim()) return String(fileValue).trim();
  return null;
}

/**
 * Resolve the effective transport type for a server.
 * @param {Record<string, unknown>} server
 * @returns {"stdio" | "http"}
 */
function resolveTransport(server) {
  return server.transport === "http" ? "http" : "stdio";
}

/**
 * Build headers from a base object with an optional Authorization override.
 * @param {Record<string, string>|undefined} baseHeaders
 * @param {string|undefined} authorization
 * @returns {Record<string, string>|undefined}
 */
function buildHeaders(baseHeaders, authorization) {
  const headers = { ...(baseHeaders ?? {}) };
  if (authorization) headers.Authorization = authorization;
  return Object.keys(headers).length ? headers : undefined;
}

/**
 * Create a stable VS Code input id from a server name.
 * @param {string} name
 * @returns {string}
 */
function buildVscodeInputId(name) {
  const safe = name.toLowerCase().replace(/[^a-z0-9_-]+/g, "-");
  return `${safe}-pat`;
}

/**
 * Resolve a bearer token value for Gemini HTTP configs.
 * @param {string} envVar
 * @param {string} serverName
 * @param {string} agentLayerRoot
 * @returns {string}
 */
function resolveBearerToken(envVar, serverName, agentLayerRoot) {
  const token = getEnvVarValue(envVar, agentLayerRoot);
  assert(
    token,
    `Missing ${envVar} for Gemini HTTP server "${serverName}". Set it in .agent-layer/.env or your shell environment.`,
  );
  return token;
}

/**
 * Check whether a client is enabled.
 * @param {Set<string>|undefined} enabledAgents
 * @param {"claude"|"codex"|"gemini"|"vscode"} client
 * @returns {boolean}
 */
function isClientEnabled(enabledAgents, client) {
  if (!enabledAgents) return true;
  return enabledAgents.has(client);
}

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

    let transport = "stdio";
    if (s.transport !== undefined) {
      assert(
        typeof s.transport === "string",
        `${filePath}: ${s.name}.transport must be a string`,
      );
      assert(
        s.transport === "stdio" || s.transport === "http",
        `${filePath}: ${s.name}.transport must be "stdio" or "http"`,
      );
      transport = s.transport;
    }

    if (transport === "http") {
      assert(
        typeof s.url === "string" && s.url.trim(),
        `${filePath}: ${s.name}.url must be a non-empty string for HTTP servers`,
      );
      assert(
        s.command === undefined,
        `${filePath}: ${s.name}.command is not allowed for HTTP servers`,
      );
      assert(
        s.args === undefined,
        `${filePath}: ${s.name}.args is not allowed for HTTP servers`,
      );
      assert(
        s.envVars === undefined,
        `${filePath}: ${s.name}.envVars is not allowed for HTTP servers`,
      );
      if (s.headers !== undefined) {
        assert(
          isPlainObject(s.headers),
          `${filePath}: ${s.name}.headers must be an object`,
        );
        for (const [key, value] of Object.entries(s.headers)) {
          assert(
            typeof value === "string",
            `${filePath}: ${s.name}.headers.${key} must be a string`,
          );
        }
      }
      if (s.bearerTokenEnvVar !== undefined) {
        assert(
          typeof s.bearerTokenEnvVar === "string" && s.bearerTokenEnvVar.trim(),
          `${filePath}: ${s.name}.bearerTokenEnvVar must be a non-empty string`,
        );
      }
      if (isPlainObject(s.headers) && s.bearerTokenEnvVar) {
        const hasAuth = Object.keys(s.headers).some(
          (key) => key.toLowerCase() === "authorization",
        );
        assert(
          !hasAuth,
          `${filePath}: ${s.name}.headers cannot set Authorization when bearerTokenEnvVar is used`,
        );
      }
    } else {
      assert(
        typeof s.command === "string" && s.command.trim(),
        `${filePath}: ${s.name}.command must be a non-empty string`,
      );
      assert(
        s.url === undefined,
        `${filePath}: ${s.name}.url is not allowed for stdio servers`,
      );
      assert(
        s.headers === undefined,
        `${filePath}: ${s.name}.headers is not allowed for stdio servers`,
      );
      assert(
        s.bearerTokenEnvVar === undefined,
        `${filePath}: ${s.name}.bearerTokenEnvVar is not allowed for stdio servers`,
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
    }

    if (s.clients !== undefined) {
      assert(
        Array.isArray(s.clients),
        `${filePath}: ${s.name}.clients must be an array`,
      );
      assert(
        s.clients.length > 0,
        `${filePath}: ${s.name}.clients must not be empty`,
      );
      const seenClients = new Set();
      for (const client of s.clients) {
        assert(
          typeof client === "string" && client.trim().length > 0,
          `${filePath}: ${s.name}.clients must be string[]`,
        );
        assert(
          KNOWN_CLIENTS.has(client),
          `${filePath}: ${s.name}.clients contains unknown client "${client}"`,
        );
        assert(
          !seenClients.has(client),
          `${filePath}: ${s.name}.clients contains duplicate "${client}"`,
        );
        seenClients.add(client);
      }
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
 * @param {"claude"|"codex"|"gemini"|"vscode"=} client
 * @returns {unknown[]}
 */
export function enabledServers(servers, client) {
  if (client !== undefined) {
    assert(
      typeof client === "string" && KNOWN_CLIENTS.has(client),
      `unknown client "${client}"`,
    );
  }
  return servers.filter((s) => {
    if (!s || !s.name) return false;
    if (!(s.enabled === undefined || s.enabled === true)) return false;
    if (!client || !Array.isArray(s.clients)) return true;
    return s.clients.includes(client);
  });
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
 * @param {"claude"|"codex"|"gemini"|"vscode"=} client
 * @returns {string[]}
 */
export function trustedServerNames(catalog, client) {
  const defaults = catalog.defaults ?? {};
  const servers = enabledServers(catalog.servers ?? [], client);
  const trusted = [];
  for (const s of servers) {
    if (resolveServerTrust(defaults, s)) trusted.push(s.name);
  }
  return trusted;
}

/**
 * Build MCP config objects for each client from the server catalog.
 * @param {{ defaults?: Record<string, unknown>, servers?: unknown[] }} catalog
 * @param {Set<string>=} enabledAgents
 * @param {string} agentLayerRoot
 * @returns {{ vscode: Record<string, unknown>, claude: Record<string, unknown>, gemini: Record<string, unknown> }}
 */
export function buildMcpConfigs(catalog, enabledAgents, agentLayerRoot) {
  assert(
    typeof agentLayerRoot === "string" && agentLayerRoot.trim().length > 0,
    "agent-layer sync: agentLayerRoot is required for MCP config generation.",
  );
  const normalizedRoot = path.resolve(agentLayerRoot);
  const defaults = catalog.defaults ?? {};
  const vscodeEnabled = isClientEnabled(enabledAgents, "vscode");
  const claudeEnabled = isClientEnabled(enabledAgents, "claude");
  const geminiEnabled = isClientEnabled(enabledAgents, "gemini");
  const vscodeServers = vscodeEnabled
    ? enabledServers(catalog.servers ?? [], "vscode")
    : [];
  const claudeServers = claudeEnabled
    ? enabledServers(catalog.servers ?? [], "claude")
    : [];
  const geminiServers = geminiEnabled
    ? enabledServers(catalog.servers ?? [], "gemini")
    : [];

  // NOTE: VS Code can load env from an envFile. Default remains .agent-layer/.env
  // unless you set defaults.vscodeEnvFile to "${workspaceFolder}/.env".
  const vscodeEnvFile =
    defaults.vscodeEnvFile ?? "${workspaceFolder}/.agent-layer/.env";

  // VS Code
  const vscode = { servers: {} };
  const vscodeInputs = [];
  const vscodeInputIds = new Set();
  for (const s of vscodeServers) {
    const transport = resolveTransport(s);
    if (transport === "http") {
      let authorization;
      if (s.bearerTokenEnvVar) {
        const inputId = buildVscodeInputId(s.name);
        authorization = `Bearer \${input:${inputId}}`;
        if (!vscodeInputIds.has(inputId)) {
          vscodeInputIds.add(inputId);
          vscodeInputs.push({
            type: "promptString",
            id: inputId,
            description:
              s.name === "github"
                ? "GitHub Personal Access Token"
                : `${s.name} Personal Access Token`,
            password: true,
          });
        }
      }
      const headers = buildHeaders(
        isPlainObject(s.headers) ? s.headers : undefined,
        authorization,
      );
      vscode.servers[s.name] = {
        type: "http",
        url: s.url,
        ...(headers ? { headers } : {}),
      };
    } else {
      vscode.servers[s.name] = {
        command: s.command,
        args: s.args ?? [],
        envFile: vscodeEnvFile,
      };
    }
  }
  if (vscodeInputs.length) vscode.inputs = vscodeInputs;

  // Claude Code
  const claude = { mcpServers: {} };
  for (const s of claudeServers) {
    const transport = resolveTransport(s);
    if (transport === "http") {
      const authorization = s.bearerTokenEnvVar
        ? `Bearer \${${s.bearerTokenEnvVar}}`
        : undefined;
      const headers = buildHeaders(
        isPlainObject(s.headers) ? s.headers : undefined,
        authorization,
      );
      claude.mcpServers[s.name] = {
        type: "http",
        url: s.url,
        ...(headers ? { headers } : {}),
      };
    } else {
      const env = {};
      for (const v of s.envVars ?? []) env[v] = `\${${v}}`;
      claude.mcpServers[s.name] = {
        command: s.command,
        args: s.args ?? [],
        ...(Object.keys(env).length ? { env } : {}),
      };
    }
  }

  // Gemini CLI
  const gemini = { mcpServers: {} };
  for (const s of geminiServers) {
    const trust = resolveServerTrust(defaults, s);
    const transport = resolveTransport(s);
    let entry;
    if (transport === "http") {
      let authorization;
      if (s.bearerTokenEnvVar) {
        const token = resolveBearerToken(
          s.bearerTokenEnvVar,
          s.name,
          normalizedRoot,
        );
        authorization = `Bearer ${token}`;
      }
      const headers = buildHeaders(
        isPlainObject(s.headers) ? s.headers : undefined,
        authorization,
      );
      entry = {
        httpUrl: s.url,
        ...(headers ? { headers } : {}),
        trust,
      };
    } else {
      const env = {};
      for (const v of s.envVars ?? []) env[v] = `\${${v}}`;
      entry = {
        command: s.command,
        args: s.args ?? [],
        ...(Object.keys(env).length ? { env } : {}),
        trust,
      };
    }

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
  const servers = enabledServers(catalog.servers ?? [], "codex");
  const lines = [
    "# GENERATED FILE",
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
    const transport = resolveTransport(s);
    if (transport === "http") {
      if (isPlainObject(s.headers) && Object.keys(s.headers).length) {
        assert(
          false,
          `codex config does not support HTTP headers for ${s.name} yet`,
        );
      }
      lines.push(`[mcp_servers.${tomlKey(s.name)}]`);
      lines.push(`url = ${tomlString(s.url)}`);
      if (s.bearerTokenEnvVar) {
        lines.push(`bearer_token_env_var = ${tomlString(s.bearerTokenEnvVar)}`);
      }
      lines.push("");
      continue;
    }

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

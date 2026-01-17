import path from "node:path";
import { buildVscodeAutoApprove, commandPrefixes } from "./policy.mjs";
import { buildMcpConfigs, enabledServers } from "./mcp.mjs";
import {
  fileExists,
  isPlainObject,
  listFiles,
  readJsonRelaxed,
  readUtf8,
} from "./utils.mjs";
import {
  parseCodexConfigSections,
  parseCodexServerSection,
} from "./codex-config.mjs";
import {
  parseCommandPrefix,
  unescapeRegexLiteral,
  equalStringArrays,
  shouldCheckAgent,
  relPath,
  parseEnvVars,
  parseHeaders,
  redactServer,
  entriesMatch,
} from "./divergence-utils.mjs";

/**
 * @typedef {import("./divergence-utils.mjs").ApprovalItem} ApprovalItem
 * @typedef {import("./divergence-utils.mjs").McpItem} McpItem
 * @typedef {import("./divergence-utils.mjs").DivergenceResult} DivergenceResult
 */

/**
 * Parse a Gemini tools.allowed entry.
 * @param {unknown} entry
 * @returns {{ prefix: string } | null}
 */
function parseGeminiAllowed(entry) {
  if (typeof entry !== "string") return null;
  const match = entry.match(/^run_shell_command\((.*)\)$/);
  if (!match) return null;
  return { prefix: match[1] };
}

/**
 * Parse a Claude permissions.allow entry.
 * @param {unknown} entry
 * @returns {{ prefix: string } | null}
 */
function parseClaudeAllow(entry) {
  if (typeof entry !== "string") return null;
  const match = entry.match(/^Bash\((.*):\*\)$/);
  if (!match) return null;
  return { prefix: match[1] };
}

/**
 * Parse a VS Code auto-approve key into a prefix string.
 * @param {string} key
 * @returns {{ prefix: string } | { reason: string }}
 */
function parseVscodeAutoApproveKey(key) {
  if (!key.startsWith("/^") || !key.endsWith("$/")) {
    return { reason: "unexpected regex format" };
  }

  const inner = key.slice(2, -2);
  const suffix = `(\\b.*)?`;
  if (!inner.endsWith(suffix)) {
    return { reason: "missing word-boundary suffix" };
  }
  const escaped = inner.slice(0, -suffix.length);
  return { prefix: unescapeRegexLiteral(escaped) };
}

/**
 * Extract a JSON array after pattern=... in a Codex rules line.
 * @param {string} line
 * @returns {{ argv: string[] } | { reason: string }}
 */
function parseCodexRulesLine(line) {
  const idx = line.indexOf("pattern=");
  if (idx === -1) return { reason: "pattern= not found" };
  let start = idx + "pattern=".length;
  while (start < line.length && /\s/.test(line[start])) start++;
  if (line[start] !== "[") return { reason: "pattern is not a JSON array" };

  let depth = 0;
  let inString = false;
  let stringChar = "";
  let end = -1;
  for (let i = start; i < line.length; i++) {
    const ch = line[i];
    if (inString) {
      if (ch === "\\") {
        i++;
        continue;
      }
      if (ch === stringChar) {
        inString = false;
        stringChar = "";
      }
      continue;
    }
    if (ch === '"' || ch === "'") {
      inString = true;
      stringChar = ch;
      continue;
    }
    if (ch === "[") depth++;
    if (ch === "]") {
      depth--;
      if (depth === 0) {
        end = i + 1;
        break;
      }
    }
  }
  if (end === -1) return { reason: "pattern array is unterminated" };
  const raw = line.slice(start, end);
  let parsed;
  try {
    parsed = JSON.parse(raw);
  } catch (err) {
    return { reason: "pattern is not valid JSON" };
  }
  if (!Array.isArray(parsed) || parsed.some((v) => typeof v !== "string")) {
    return { reason: "pattern must be a string array" };
  }
  return { argv: parsed };
}

/**
 * Extract stdio server definitions from a parsed MCP config entry.
 * @param {string} name
 * @param {unknown} entry
 * @param {string} filePath
 * @param {boolean=} trust
 * @returns {{ server: { transport: "stdio", command: string, args: string[], envVars: string[], envVarsKnown: boolean, trust?: boolean }, reason?: string }}
 */
function parseStdioEntry(name, entry, filePath, trust) {
  if (!isPlainObject(entry)) {
    return { reason: `${filePath}: ${name} is not an object` };
  }
  const command = entry.command;
  if (typeof command !== "string" || !command.trim()) {
    return { reason: `${filePath}: ${name}.command must be a string` };
  }
  const args = entry.args;
  if (args !== undefined) {
    if (!Array.isArray(args) || args.some((v) => typeof v !== "string")) {
      return { reason: `${filePath}: ${name}.args must be string[]` };
    }
  }
  const envVarsResult = parseEnvVars(entry.env);
  if ("reason" in envVarsResult) return { reason: envVarsResult.reason };

  return {
    server: {
      transport: "stdio",
      command,
      args: args ?? [],
      envVars: envVarsResult.envVars,
      envVarsKnown: envVarsResult.known,
      ...(trust !== undefined ? { trust } : {}),
    },
  };
}

/**
 * Extract HTTP server definitions from a parsed MCP config entry.
 * @param {string} name
 * @param {unknown} entry
 * @param {string} filePath
 * @param {string} urlKey
 * @param {boolean=} trust
 * @returns {{ server: { transport: "http", url: string, headers: Record<string, string>, trust?: boolean }, reason?: string }}
 */
function parseHttpEntry(name, entry, filePath, urlKey, trust) {
  if (!isPlainObject(entry)) {
    return { reason: `${filePath}: ${name} is not an object` };
  }
  const url = entry[urlKey];
  if (typeof url !== "string" || !url.trim()) {
    return { reason: `${filePath}: ${name}.${urlKey} must be a string` };
  }
  const headersResult = parseHeaders(entry.headers, filePath, name);
  if ("reason" in headersResult) return { reason: headersResult.reason };
  return {
    server: {
      transport: "http",
      url,
      headers: headersResult.headers,
      ...(trust !== undefined ? { trust } : {}),
    },
  };
}

/**
 * Parse a Claude MCP entry.
 * @param {string} name
 * @param {unknown} entry
 * @param {string} filePath
 * @returns {{ server: { transport: "stdio", command: string, args: string[], envVars: string[], envVarsKnown: boolean } | { transport: "http", url: string, headers: Record<string, string> }, reason?: string }}
 */
function parseClaudeEntry(name, entry, filePath) {
  if (!isPlainObject(entry)) {
    return { reason: `${filePath}: ${name} is not an object` };
  }
  if (entry.type === "http") {
    return parseHttpEntry(name, entry, filePath, "url");
  }
  if (entry.command !== undefined) {
    return parseStdioEntry(name, entry, filePath);
  }
  return { reason: `${filePath}: ${name} must set command or type=http` };
}

/**
 * Parse a VS Code MCP entry.
 * @param {string} name
 * @param {unknown} entry
 * @param {string} filePath
 * @returns {{ server: { transport: "stdio", command: string, args: string[], envVars: string[], envVarsKnown: boolean } | { transport: "http", url: string, headers: Record<string, string> }, reason?: string }}
 */
function parseVscodeEntry(name, entry, filePath) {
  if (!isPlainObject(entry)) {
    return { reason: `${filePath}: ${name} is not an object` };
  }
  if (entry.type === "http" || entry.url !== undefined) {
    return parseHttpEntry(name, entry, filePath, "url");
  }
  if (entry.command !== undefined) {
    return parseStdioEntry(name, entry, filePath);
  }
  return { reason: `${filePath}: ${name} must set command or type=http` };
}

/**
 * Parse a Gemini MCP entry.
 * @param {string} name
 * @param {unknown} entry
 * @param {string} filePath
 * @param {boolean=} trust
 * @returns {{ server: { transport: "stdio", command: string, args: string[], envVars: string[], envVarsKnown: boolean, trust?: boolean } | { transport: "http", url: string, headers: Record<string, string>, trust?: boolean }, reason?: string }}
 */
function parseGeminiEntry(name, entry, filePath, trust) {
  if (!isPlainObject(entry)) {
    return { reason: `${filePath}: ${name} is not an object` };
  }
  if (entry.httpUrl !== undefined) {
    return parseHttpEntry(name, entry, filePath, "httpUrl", trust);
  }
  if (entry.command !== undefined) {
    return parseStdioEntry(name, entry, filePath, trust);
  }
  return { reason: `${filePath}: ${name} must set command or httpUrl` };
}

/**
 * Collect approval divergences across managed configs.
 * @param {string} parentRoot
 * @param {import("./policy.mjs").CommandPolicy} policy
 * @param {Set<string>=} enabledAgents
 * @returns {{ items: ApprovalItem[], notes: string[] }}
 */
export function collectApprovalDivergences(parentRoot, policy, enabledAgents) {
  const policySet = new Set(commandPrefixes(policy));
  /** @type {ApprovalItem[]} */
  const items = [];
  const notes = [];

  if (shouldCheckAgent(enabledAgents, "gemini")) {
    const geminiPath = path.join(parentRoot, ".gemini", "settings.json");
    const gemini = readJsonRelaxed(geminiPath, null);
    if (isPlainObject(gemini)) {
      const allowed = gemini.tools?.allowed;
      if (Array.isArray(allowed)) {
        for (const entry of allowed) {
          const parsed = parseGeminiAllowed(entry);
          if (!parsed) continue;
          const prefix = parsed.prefix;
          if (policySet.has(prefix)) continue;
          const argvResult = parseCommandPrefix(prefix);
          if ("argv" in argvResult) {
            items.push({
              kind: "approval",
              source: "gemini",
              filePath: geminiPath,
              raw: String(entry),
              prefix,
              argv: argvResult.argv,
              parseable: true,
            });
          } else {
            items.push({
              kind: "approval",
              source: "gemini",
              filePath: geminiPath,
              raw: String(entry),
              prefix,
              argv: null,
              parseable: false,
              reason: argvResult.reason,
            });
          }
        }
      }
    }
  }

  if (shouldCheckAgent(enabledAgents, "claude")) {
    const claudePath = path.join(parentRoot, ".claude", "settings.json");
    const claude = readJsonRelaxed(claudePath, null);
    if (isPlainObject(claude)) {
      const allow = claude.permissions?.allow;
      if (Array.isArray(allow)) {
        for (const entry of allow) {
          const parsed = parseClaudeAllow(entry);
          if (!parsed) continue;
          const prefix = parsed.prefix;
          if (policySet.has(prefix)) continue;
          const argvResult = parseCommandPrefix(prefix);
          if ("argv" in argvResult) {
            items.push({
              kind: "approval",
              source: "claude",
              filePath: claudePath,
              raw: String(entry),
              prefix,
              argv: argvResult.argv,
              parseable: true,
            });
          } else {
            items.push({
              kind: "approval",
              source: "claude",
              filePath: claudePath,
              raw: String(entry),
              prefix,
              argv: null,
              parseable: false,
              reason: argvResult.reason,
            });
          }
        }
      }
    }
  }

  if (shouldCheckAgent(enabledAgents, "vscode")) {
    const vscodeSettingsPath = path.join(
      parentRoot,
      ".vscode",
      "settings.json",
    );
    const vscodeSettings = readJsonRelaxed(vscodeSettingsPath, null);
    if (isPlainObject(vscodeSettings)) {
      const autoApprove = vscodeSettings["chat.tools.terminal.autoApprove"];
      if (isPlainObject(autoApprove)) {
        const generated = buildVscodeAutoApprove(commandPrefixes(policy));
        const generatedKeys = new Set(Object.keys(generated));
        for (const key of Object.keys(autoApprove)) {
          if (generatedKeys.has(key)) continue;
          const prefixResult = parseVscodeAutoApproveKey(key);
          if ("prefix" in prefixResult) {
            const argvResult = parseCommandPrefix(prefixResult.prefix);
            if ("argv" in argvResult) {
              items.push({
                kind: "approval",
                source: "vscode",
                filePath: vscodeSettingsPath,
                raw: key,
                prefix: prefixResult.prefix,
                argv: argvResult.argv,
                parseable: true,
              });
            } else {
              items.push({
                kind: "approval",
                source: "vscode",
                filePath: vscodeSettingsPath,
                raw: key,
                prefix: prefixResult.prefix,
                argv: null,
                parseable: false,
                reason: argvResult.reason,
              });
            }
          } else {
            items.push({
              kind: "approval",
              source: "vscode",
              filePath: vscodeSettingsPath,
              raw: key,
              prefix: null,
              argv: null,
              parseable: false,
              reason: prefixResult.reason,
            });
          }
        }
      }
    }
  }

  if (shouldCheckAgent(enabledAgents, "codex")) {
    const codexRulesDir = path.join(parentRoot, ".codex", "rules");
    const codexRulesPath = path.join(codexRulesDir, "default.rules");
    if (fileExists(codexRulesPath)) {
      const lines = readUtf8(codexRulesPath).split(/\r?\n/);
      for (const line of lines) {
        if (!line.trim()) continue;
        if (!line.startsWith("prefix_rule(")) continue;
        const parsed = parseCodexRulesLine(line);
        if ("argv" in parsed) {
          const prefix = parsed.argv.join(" ");
          if (policySet.has(prefix)) continue;
          items.push({
            kind: "approval",
            source: "codex-rules",
            filePath: codexRulesPath,
            raw: line,
            prefix,
            argv: parsed.argv,
            parseable: true,
          });
        } else {
          items.push({
            kind: "approval",
            source: "codex-rules",
            filePath: codexRulesPath,
            raw: line,
            prefix: null,
            argv: null,
            parseable: false,
            reason: parsed.reason,
          });
        }
      }
    }

    const extraRules = listFiles(codexRulesDir, ".rules").filter(
      (filePath) => path.basename(filePath) !== "default.rules",
    );
    if (extraRules.length > 0) {
      const relRules = extraRules.map((filePath) =>
        relPath(parentRoot, filePath),
      );
      notes.push(
        "Codex rules: extra rules files detected: " +
          relRules.join(", ") +
          ". Agent Layer only reads .codex/rules/default.rules; " +
          "integrate entries into .agent-layer/config/policy/commands.json and re-run sync, " +
          "or delete the extra rules files to clear this warning.",
      );
    }
  }

  return { items, notes };
}

/**
 * Collect MCP divergences across managed configs.
 * @param {string} parentRoot
 * @param {{ defaults?: Record<string, unknown>, servers?: unknown[] }} catalog
 * @param {Set<string>=} enabledAgents
 * @param {string} agentLayerRoot
 * @returns {{ items: McpItem[], notes: string[] }}
 */
export function collectMcpDivergences(
  parentRoot,
  catalog,
  enabledAgents,
  agentLayerRoot,
) {
  /** @type {McpItem[]} */
  const items = [];
  const notes = [];
  const generated = buildMcpConfigs(catalog, enabledAgents, agentLayerRoot);
  const catalogMap = new Map();
  if (shouldCheckAgent(enabledAgents, "codex")) {
    const catalogServers = enabledServers(catalog.servers ?? [], "codex");
    for (const entry of catalogServers) {
      if (!isPlainObject(entry) || typeof entry.name !== "string") continue;
      if (entry.transport === "http") {
        catalogMap.set(entry.name, {
          transport: "http",
          url: entry.url,
          bearerTokenEnvVar:
            typeof entry.bearerTokenEnvVar === "string"
              ? entry.bearerTokenEnvVar
              : null,
        });
      } else {
        const envVars = Array.isArray(entry.envVars) ? entry.envVars : [];
        catalogMap.set(entry.name, {
          transport: "stdio",
          command: entry.command,
          args: Array.isArray(entry.args) ? entry.args : [],
          envVars,
        });
      }
    }
  }

  if (shouldCheckAgent(enabledAgents, "gemini")) {
    const geminiPath = path.join(parentRoot, ".gemini", "settings.json");
    const gemini = readJsonRelaxed(geminiPath, null);
    if (isPlainObject(gemini) && isPlainObject(gemini.mcpServers)) {
      for (const [name, entry] of Object.entries(gemini.mcpServers)) {
        const expected = generated.gemini.mcpServers?.[name];
        const trust = isPlainObject(entry) ? entry.trust : undefined;
        const parsed = parseGeminiEntry(name, entry, geminiPath, trust);
        if ("server" in parsed) {
          let expectedServer = null;
          if (isPlainObject(expected)) {
            const expectedTrust = expected.trust;
            const expectedParsed = parseGeminiEntry(
              name,
              expected,
              "<generated gemini config>",
              expectedTrust,
            );
            if ("server" in expectedParsed)
              expectedServer = expectedParsed.server;
          }
          const shouldInclude = !expectedServer
            ? true
            : !entriesMatch(parsed.server, expectedServer, true);
          if (shouldInclude) {
            items.push({
              kind: "mcp",
              source: "gemini",
              filePath: geminiPath,
              name,
              server: redactServer(parsed.server),
              parseable: true,
            });
          }
        } else {
          items.push({
            kind: "mcp",
            source: "gemini",
            filePath: geminiPath,
            name,
            server: null,
            parseable: false,
            reason: parsed.reason,
          });
        }
      }
    }
  }

  if (shouldCheckAgent(enabledAgents, "claude")) {
    const claudePath = path.join(parentRoot, ".mcp.json");
    const claude = readJsonRelaxed(claudePath, null);
    if (isPlainObject(claude) && isPlainObject(claude.mcpServers)) {
      for (const [name, entry] of Object.entries(claude.mcpServers)) {
        const expected = generated.claude.mcpServers?.[name];
        const parsed = parseClaudeEntry(name, entry, claudePath);
        if ("server" in parsed) {
          let expectedServer = null;
          if (isPlainObject(expected)) {
            const expectedParsed = parseClaudeEntry(
              name,
              expected,
              "<generated claude config>",
            );
            if ("server" in expectedParsed)
              expectedServer = expectedParsed.server;
          }
          const shouldInclude = !expectedServer
            ? true
            : !entriesMatch(parsed.server, expectedServer, false);
          if (shouldInclude) {
            items.push({
              kind: "mcp",
              source: "claude",
              filePath: claudePath,
              name,
              server: redactServer(parsed.server),
              parseable: true,
            });
          }
        } else {
          items.push({
            kind: "mcp",
            source: "claude",
            filePath: claudePath,
            name,
            server: null,
            parseable: false,
            reason: parsed.reason,
          });
        }
      }
    }
  }

  if (shouldCheckAgent(enabledAgents, "vscode")) {
    const vscodeMcpPath = path.join(parentRoot, ".vscode", "mcp.json");
    const vscodeMcp = readJsonRelaxed(vscodeMcpPath, null);
    if (isPlainObject(vscodeMcp) && isPlainObject(vscodeMcp.servers)) {
      for (const [name, entry] of Object.entries(vscodeMcp.servers)) {
        const expected = generated.vscode.servers?.[name];
        const parsed = parseVscodeEntry(name, entry, vscodeMcpPath);
        if ("server" in parsed) {
          let expectedServer = null;
          if (isPlainObject(expected)) {
            const expectedParsed = parseVscodeEntry(
              name,
              expected,
              "<generated vscode config>",
            );
            if ("server" in expectedParsed)
              expectedServer = expectedParsed.server;
          }
          const shouldInclude = !expectedServer
            ? true
            : !entriesMatch(parsed.server, expectedServer, false);
          if (shouldInclude) {
            items.push({
              kind: "mcp",
              source: "vscode",
              filePath: vscodeMcpPath,
              name,
              server: redactServer(parsed.server),
              parseable: true,
            });
          }
        } else {
          items.push({
            kind: "mcp",
            source: "vscode",
            filePath: vscodeMcpPath,
            name,
            server: null,
            parseable: false,
            reason: parsed.reason,
          });
        }
      }
    }
  }

  if (shouldCheckAgent(enabledAgents, "codex")) {
    const codexConfigPath = path.join(parentRoot, ".codex", "config.toml");
    if (fileExists(codexConfigPath)) {
      const content = readUtf8(codexConfigPath);
      const parsed = parseCodexConfigSections(content);
      for (const [name, lines] of parsed.sections.entries()) {
        const expected = catalogMap.get(name);
        const parsedSection = parseCodexServerSection(lines);
        if ("server" in parsedSection) {
          let shouldInclude = false;
          if (!expected) {
            shouldInclude = true;
          } else if (parsedSection.server.transport !== expected.transport) {
            shouldInclude = true;
          } else if (parsedSection.server.transport === "http") {
            if (parsedSection.server.url !== expected.url) shouldInclude = true;
            if (
              parsedSection.server.bearerTokenEnvVar !==
              expected.bearerTokenEnvVar
            ) {
              shouldInclude = true;
            }
          } else {
            const expectedEnvVars = expected.envVars ?? [];
            const envMatches =
              !parsedSection.server.envVarsKnown ||
              equalStringArrays(
                parsedSection.server.envVars.slice().sort(),
                expectedEnvVars.slice().sort(),
              );
            if (
              parsedSection.server.command !== expected.command ||
              !equalStringArrays(parsedSection.server.args, expected.args) ||
              !envMatches
            ) {
              shouldInclude = true;
            }
          }
          if (shouldInclude) {
            items.push({
              kind: "mcp",
              source: "codex",
              filePath: codexConfigPath,
              name,
              server: redactServer(parsedSection.server),
              parseable: true,
            });
          }
        } else {
          items.push({
            kind: "mcp",
            source: "codex",
            filePath: codexConfigPath,
            name,
            server: null,
            parseable: false,
            reason: parsedSection.reason,
          });
        }
      }
    }
  }

  return { items, notes };
}

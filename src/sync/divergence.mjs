import path from "node:path";
import { buildVscodeAutoApprove, commandPrefixes } from "./policy.mjs";
import { buildMcpConfigs } from "./mcp.mjs";
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
  isEnabledServer,
} from "./codex-config.mjs";

/**
 * @typedef {object} ApprovalItem
 * @property {"approval"} kind
 * @property {string} source
 * @property {string} filePath
 * @property {string} raw
 * @property {string|null} prefix
 * @property {string[]|null} argv
 * @property {boolean} parseable
 * @property {string=} reason
 */

/**
 * @typedef {object} McpItem
 * @property {"mcp"} kind
 * @property {string} source
 * @property {string} filePath
 * @property {string} name
 * @property {object|null} server
 * @property {boolean} parseable
 * @property {string=} reason
 * @property {boolean=} trust
 */

/**
 * @typedef {object} DivergenceResult
 * @property {ApprovalItem[]} approvals
 * @property {McpItem[]} mcp
 * @property {string[]} notes
 */

/**
 * Parse a command prefix into argv tokens, returning null if unsafe.
 * @param {string} prefix
 * @returns {{ argv: string[] } | { reason: string }}
 */
function parseCommandPrefix(prefix) {
  const trimmed = prefix.trim();
  if (!trimmed) return { reason: "empty prefix" };
  if (/["'\\`]/.test(trimmed))
    return { reason: "contains quotes or backslashes" };
  if (/[;&|><]/.test(trimmed)) return { reason: "contains shell operators" };
  if (/\$\(/.test(trimmed)) return { reason: "contains command substitution" };
  if (/[\r\n]/.test(trimmed)) return { reason: "contains newline" };

  const argv = trimmed.split(/\s+/).filter(Boolean);
  if (!argv.length) return { reason: "no argv tokens found" };
  if (argv.some((token) => /["'\\`]/.test(token))) {
    return { reason: "contains quotes or backslashes in token" };
  }
  return { argv };
}

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
 * Unescape a regex literal that was escaped by escapeRegexLiteral.
 * @param {string} escaped
 * @returns {string}
 */
function unescapeRegexLiteral(escaped) {
  return escaped.replace(/\\([.*+?^${}()|[\]\\])/g, "$1");
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
 * Compare arrays of strings for equality.
 * @param {string[]|undefined} a
 * @param {string[]|undefined} b
 * @returns {boolean}
 */
function equalStringArrays(a, b) {
  if (!a && !b) return true;
  if (!a || !b) return false;
  if (a.length !== b.length) return false;
  return a.every((val, idx) => val === b[idx]);
}

/**
 * Compare string arrays as unordered sets.
 * @param {string[]|undefined} a
 * @param {string[]|undefined} b
 * @returns {boolean}
 */
function equalStringSets(a, b) {
  if (!a && !b) return true;
  if (!a || !b) return false;
  if (a.length !== b.length) return false;
  const aSorted = a.slice().sort();
  const bSorted = b.slice().sort();
  return aSorted.every((val, idx) => val === bSorted[idx]);
}

/**
 * Convert an absolute path to a repo-relative path for messaging.
 * @param {string} parentRoot
 * @param {string} absPath
 * @returns {string}
 */
function relPath(parentRoot, absPath) {
  const rel = path.relative(parentRoot, absPath);
  return rel.split(path.sep).join("/");
}

/**
 * Normalize env var names from an env object.
 * @param {unknown} env
 * @returns {{ envVars: string[], known: boolean } | { reason: string }}
 */
function parseEnvVars(env) {
  if (env === undefined) return { envVars: [], known: false };
  if (!isPlainObject(env)) return { reason: "env is not an object" };
  const keys = Object.keys(env);
  return { envVars: keys, known: true };
}

/**
 * Extract server definitions from a parsed MCP config entry.
 * @param {string} name
 * @param {unknown} entry
 * @param {string} filePath
 * @param {boolean=} trust
 * @returns {{ server: { command: string, args: string[], envVars: string[], envVarsKnown: boolean }, reason?: string }}
 */
function parseServerEntry(name, entry, filePath, trust) {
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
      command,
      args: args ?? [],
      envVars: envVarsResult.envVars,
      envVarsKnown: envVarsResult.known,
      ...(trust !== undefined ? { trust } : {}),
    },
  };
}

/**
 * Collect approval divergences across managed configs.
 * @param {string} parentRoot
 * @param {import("./policy.mjs").CommandPolicy} policy
 * @returns {{ items: ApprovalItem[], notes: string[] }}
 */
export function collectApprovalDivergences(parentRoot, policy) {
  const policySet = new Set(commandPrefixes(policy));
  /** @type {ApprovalItem[]} */
  const items = [];
  const notes = [];

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

  const vscodeSettingsPath = path.join(parentRoot, ".vscode", "settings.json");
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

  return { items, notes };
}

/**
 * Collect MCP divergences across managed configs.
 * @param {string} parentRoot
 * @param {{ defaults?: Record<string, unknown>, servers?: unknown[] }} catalog
 * @returns {{ items: McpItem[], notes: string[] }}
 */
export function collectMcpDivergences(parentRoot, catalog) {
  /** @type {McpItem[]} */
  const items = [];
  const notes = [];
  const generated = buildMcpConfigs(catalog);
  const catalogServers = Array.isArray(catalog.servers) ? catalog.servers : [];
  const catalogMap = new Map();
  for (const entry of catalogServers) {
    if (!isPlainObject(entry) || typeof entry.name !== "string") continue;
    if (!isEnabledServer(entry)) continue;
    const envVars = Array.isArray(entry.envVars) ? entry.envVars : [];
    catalogMap.set(entry.name, {
      command: entry.command,
      args: Array.isArray(entry.args) ? entry.args : [],
      envVars,
    });
  }

  const geminiPath = path.join(parentRoot, ".gemini", "settings.json");
  const gemini = readJsonRelaxed(geminiPath, null);
  if (isPlainObject(gemini) && isPlainObject(gemini.mcpServers)) {
    for (const [name, entry] of Object.entries(gemini.mcpServers)) {
      const expected = generated.gemini.mcpServers?.[name];
      const trust = isPlainObject(entry) ? entry.trust : undefined;
      const parsed = parseServerEntry(name, entry, geminiPath, trust);
      if ("server" in parsed) {
        const expectedEntry = isPlainObject(expected) ? expected : null;
        const expectedCommand = expectedEntry?.command;
        const expectedArgs = Array.isArray(expectedEntry?.args)
          ? expectedEntry?.args
          : [];
        const expectedEnv = parseEnvVars(expectedEntry?.env);
        const expectedEnvVars =
          "envVars" in expectedEnv ? expectedEnv.envVars : [];
        const shouldInclude =
          !expectedEntry ||
          parsed.server.command !== expectedCommand ||
          !equalStringArrays(parsed.server.args, expectedArgs) ||
          !equalStringSets(parsed.server.envVars, expectedEnvVars) ||
          trust !== expectedEntry?.trust;
        if (shouldInclude) {
          items.push({
            kind: "mcp",
            source: "gemini",
            filePath: geminiPath,
            name,
            server: parsed.server,
            parseable: true,
            ...(trust !== undefined ? { trust } : {}),
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

  const claudePath = path.join(parentRoot, ".mcp.json");
  const claude = readJsonRelaxed(claudePath, null);
  if (isPlainObject(claude) && isPlainObject(claude.mcpServers)) {
    for (const [name, entry] of Object.entries(claude.mcpServers)) {
      const expected = generated.claude.mcpServers?.[name];
      const parsed = parseServerEntry(name, entry, claudePath);
      if ("server" in parsed) {
        const expectedEntry = isPlainObject(expected) ? expected : null;
        const expectedCommand = expectedEntry?.command;
        const expectedArgs = Array.isArray(expectedEntry?.args)
          ? expectedEntry?.args
          : [];
        const expectedEnv = parseEnvVars(expectedEntry?.env);
        const expectedEnvVars =
          "envVars" in expectedEnv ? expectedEnv.envVars : [];
        const shouldInclude =
          !expectedEntry ||
          parsed.server.command !== expectedCommand ||
          !equalStringArrays(parsed.server.args, expectedArgs) ||
          !equalStringSets(parsed.server.envVars, expectedEnvVars);
        if (shouldInclude) {
          items.push({
            kind: "mcp",
            source: "claude",
            filePath: claudePath,
            name,
            server: parsed.server,
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

  const vscodeMcpPath = path.join(parentRoot, ".vscode", "mcp.json");
  const vscodeMcp = readJsonRelaxed(vscodeMcpPath, null);
  if (isPlainObject(vscodeMcp) && isPlainObject(vscodeMcp.servers)) {
    for (const [name, entry] of Object.entries(vscodeMcp.servers)) {
      const expected = generated.vscode.servers?.[name];
      const parsed = parseServerEntry(name, entry, vscodeMcpPath);
      if ("server" in parsed) {
        const expectedEntry = isPlainObject(expected) ? expected : null;
        const expectedCommand = expectedEntry?.command;
        const expectedArgs = Array.isArray(expectedEntry?.args)
          ? expectedEntry?.args
          : [];
        const shouldInclude =
          !expectedEntry ||
          parsed.server.command !== expectedCommand ||
          !equalStringArrays(parsed.server.args, expectedArgs);
        if (shouldInclude) {
          items.push({
            kind: "mcp",
            source: "vscode",
            filePath: vscodeMcpPath,
            name,
            server: parsed.server,
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

  const codexConfigPath = path.join(parentRoot, ".codex", "config.toml");
  if (fileExists(codexConfigPath)) {
    const content = readUtf8(codexConfigPath);
    const parsed = parseCodexConfigSections(content);
    for (const [name, lines] of parsed.sections.entries()) {
      const expected = catalogMap.get(name);
      const parsedSection = parseCodexServerSection(lines);
      if ("server" in parsedSection) {
        const expectedEnvVars = expected?.envVars ?? [];
        const envMatches =
          !parsedSection.server.envVarsKnown ||
          equalStringArrays(
            parsedSection.server.envVars.slice().sort(),
            expectedEnvVars.slice().sort(),
          );
        const shouldInclude =
          !expected ||
          parsedSection.server.command !== expected.command ||
          !equalStringArrays(parsedSection.server.args, expected.args) ||
          !envMatches;
        if (shouldInclude) {
          items.push({
            kind: "mcp",
            source: "codex",
            filePath: codexConfigPath,
            name,
            server: parsedSection.server,
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

  return { items, notes };
}

/**
 * Collect divergent approvals + MCP entries for warning output.
 * @param {string} parentRoot
 * @param {import("./policy.mjs").CommandPolicy} policy
 * @param {{ defaults?: Record<string, unknown>, servers?: unknown[] }} catalog
 * @returns {DivergenceResult}
 */
export function collectDivergences(parentRoot, policy, catalog) {
  const approvals = collectApprovalDivergences(parentRoot, policy);
  const mcp = collectMcpDivergences(parentRoot, catalog);
  return {
    approvals: approvals.items,
    mcp: mcp.items,
    notes: [...approvals.notes, ...mcp.notes],
  };
}

/**
 * Format a simple warning for divergent configs.
 * @param {DivergenceResult} result
 * @returns {string}
 */
export function formatDivergenceWarning(result) {
  const parts = [];
  if (result.approvals.length)
    parts.push(`approvals: ${result.approvals.length}`);
  if (result.mcp.length) parts.push(`mcp: ${result.mcp.length}`);
  const detail = parts.length ? ` (${parts.join(", ")})` : "";
  const detailLines = [];
  if (result.approvals.length) {
    detailLines.push(`- approvals: ${result.approvals.length}`);
  }
  if (result.mcp.length) {
    detailLines.push(`- mcp: ${result.mcp.length}`);
  }
  return [
    `agent-layer sync: WARNING: client configs diverge from .agent-layer sources${detail}.`,
    "",
    "Details:",
    ...detailLines,
    "",
    "Notes:",
    "- Sync preserves existing client entries by default; it will not overwrite them unless you pass --overwrite or choose overwrite in --interactive.",
    "",
    "Next steps:",
    "- Run: node .agent-layer/src/sync/inspect.mjs (JSON report)",
    "- Add them to .agent-layer/config/policy/commands.json or .agent-layer/config/mcp-servers.json, then re-run sync",
    "- Or re-run with: node .agent-layer/src/sync/sync.mjs --overwrite (discard client-only entries)",
    "- Or re-run with: node .agent-layer/src/sync/sync.mjs --interactive (review and choose)",
  ].join("\n");
}

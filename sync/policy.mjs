import path from "node:path";
import { assert, fileExists, isPlainObject, readJsonRelaxed } from "./utils.mjs";

/**
 * @typedef {{ argv: string[] }} CommandPolicyEntry
 * @typedef {{ version: number, allowed: CommandPolicyEntry[] }} CommandPolicy
 */

/**
 * Validate command policy schema and duplicates.
 *
 * Notes:
 * - This policy is intended to represent *argv prefixes* (tokenized), not full shell strings.
 * - Each argv token must be a non-empty string and must not contain newlines.
 * - Duplicate prefixes are rejected.
 *
 * @param {unknown} policy
 * @param {string} filePath
 * @returns {void}
 */
export function validateCommandPolicy(policy, filePath) {
  assert(isPlainObject(policy), `${filePath} must contain a JSON object`);
  assert(policy.version === 1, `${filePath}: version must be 1`);
  assert(Array.isArray(policy.allowed), `${filePath}: allowed must be an array`);

  const seen = new Set();
  for (let i = 0; i < policy.allowed.length; i++) {
    const entry = policy.allowed[i];
    assert(isPlainObject(entry), `${filePath}: allowed[${i}] must be an object`);
    assert(
      Array.isArray(entry.argv) && entry.argv.length > 0,
      `${filePath}: allowed[${i}].argv must be a non-empty array`
    );

    for (let j = 0; j < entry.argv.length; j++) {
      const arg = entry.argv[j];
      assert(
        typeof arg === "string" && arg.trim().length > 0,
        `${filePath}: allowed[${i}].argv[${j}] must be a non-empty string`
      );
      assert(
        !/[\r\n]/.test(arg),
        `${filePath}: allowed[${i}].argv[${j}] must not contain newlines`
      );
    }

    const prefix = entry.argv.join(" ");
    assert(!seen.has(prefix), `${filePath}: duplicate argv prefix "${prefix}"`);
    seen.add(prefix);
  }
}

/**
 * Load command policy from the .agent-layer root.
 * @param {string} agentlayerRoot
 * @returns {CommandPolicy}
 */
export function loadCommandPolicy(agentlayerRoot) {
  const filePath = path.join(agentlayerRoot, "policy", "commands.json");
  assert(fileExists(filePath), `${filePath} not found`);
  const parsed = readJsonRelaxed(filePath, null);
  validateCommandPolicy(parsed, filePath);
  return /** @type {CommandPolicy} */ (parsed);
}

/**
 * Extract canonical argv prefixes from the command policy.
 * @param {CommandPolicy} policy
 * @returns {string[]}
 */
export function commandPrefixes(policy) {
  return policy.allowed.map((entry) => entry.argv.join(" "));
}

/**
 * Escape regex metacharacters in a literal string.
 * @param {string} literal
 * @returns {string}
 */
export function escapeRegexLiteral(literal) {
  return literal.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/**
 * Build Gemini tools.allowed prefixes from command prefixes.
 *
 * Gemini expects entries in the form: `run_shell_command(<prefix>)`.
 * These are treated as prefixes for bypassing confirmation, and chaining (`&&`, `;`, etc.)
 * is validated per-subcommand by Gemini itself.
 *
 * IMPORTANT:
 * - The closing `)` is required for the allowlist entry to be recognized reliably.
 *
 * @param {string[]} prefixes
 * @returns {string[]}
 */
export function buildGeminiAllowed(prefixes) {
  return prefixes.map((prefix) => `run_shell_command(${prefix})`);
}

/**
 * Build Claude permissions.allow patterns from command prefixes.
 * @param {string[]} prefixes
 * @returns {string[]}
 */
export function buildClaudeAllow(prefixes) {
  return prefixes.map((prefix) => `Bash(${prefix}:*)`);
}

/**
 * Build VS Code auto-approve regex keys from command prefixes.
 * @param {string[]} prefixes
 * @returns {Record<string, boolean>}
 */
export function buildVscodeAutoApprove(prefixes) {
  const autoApprove = {};
  for (const prefix of prefixes) {
    const literal = escapeRegexLiteral(prefix);
    const key = `/^${literal}(\\b.*)?$/`;
    autoApprove[key] = true;
  }
  return autoApprove;
}

/**
 * Merge generated Gemini settings with existing user settings.
 * @param {unknown} existing
 * @param {{ mcpServers: Record<string, unknown> }} generated
 * @param {string[]} allowed
 * @param {string} filePath
 * @returns {Record<string, unknown>}
 */
export function mergeGeminiSettings(existing, generated, allowed, filePath) {
  assert(isPlainObject(existing), `${filePath} must contain a JSON object`);

  const existingMcp = existing.mcpServers;
  if (existingMcp !== undefined) {
    assert(isPlainObject(existingMcp), `${filePath}: mcpServers must be an object`);
  }

  const existingTools = existing.tools;
  if (existingTools !== undefined) {
    assert(isPlainObject(existingTools), `${filePath}: tools must be an object`);
  }

  const existingAllowed = existingTools?.allowed;
  if (existingAllowed !== undefined) {
    assert(Array.isArray(existingAllowed), `${filePath}: tools.allowed must be an array`);
  }

  const mergedAllowed = existingAllowed ? existingAllowed.slice() : [];
  const seen = new Set(mergedAllowed);
  for (const entry of allowed) {
    if (!seen.has(entry)) {
      mergedAllowed.push(entry);
      seen.add(entry);
    }
  }

  return {
    ...existing,
    tools: { ...(existingTools ?? {}), allowed: mergedAllowed },
    // Generated MCP servers should win on name collisions (repo-owned source of truth).
    mcpServers: { ...(existingMcp ?? {}), ...generated.mcpServers },
  };
}

/**
 * Merge generated Claude permissions.allow patterns with existing settings.
 * @param {unknown} existing
 * @param {string[]} allowPatterns
 * @param {string} filePath
 * @returns {Record<string, unknown>}
 */
export function mergeClaudeSettings(existing, allowPatterns, filePath) {
  assert(isPlainObject(existing), `${filePath} must contain a JSON object`);

  const permissions = existing.permissions;
  if (permissions !== undefined) {
    assert(isPlainObject(permissions), `${filePath}: permissions must be an object`);
  }

  const existingAllow = permissions?.allow;
  if (existingAllow !== undefined) {
    assert(Array.isArray(existingAllow), `${filePath}: permissions.allow must be an array`);
  }

  const mergedAllow = existingAllow ? existingAllow.slice() : [];
  const seen = new Set(mergedAllow);
  for (const pattern of allowPatterns) {
    if (!seen.has(pattern)) {
      mergedAllow.push(pattern);
      seen.add(pattern);
    }
  }

  return {
    ...existing,
    permissions: { ...(permissions ?? {}), allow: mergedAllow },
  };
}

/**
 * Merge generated VS Code auto-approve rules with existing settings.
 * @param {unknown} existing
 * @param {Record<string, boolean>} generated
 * @param {string} filePath
 * @returns {Record<string, unknown>}
 */
export function mergeVscodeSettings(existing, generated, filePath) {
  assert(isPlainObject(existing), `${filePath} must contain a JSON object`);

  const existingAutoApprove = existing["chat.tools.terminal.autoApprove"];
  if (existingAutoApprove !== undefined) {
    assert(
      isPlainObject(existingAutoApprove),
      `${filePath}: chat.tools.terminal.autoApprove must be an object`
    );
  }

  const mergedAutoApprove = { ...(existingAutoApprove ?? {}) };
  for (const [key, value] of Object.entries(generated)) {
    if (!Object.prototype.hasOwnProperty.call(mergedAutoApprove, key)) {
      mergedAutoApprove[key] = value;
    }
  }

  return {
    ...existing,
    "chat.tools.terminal.autoApprove": mergedAutoApprove,
  };
}

/**
 * Render Codex rules content for the command allowlist.
 * @param {CommandPolicyEntry[]} entries
 * @returns {string}
 */
export function renderCodexRules(entries) {
  const lines = entries.map(
    (entry) =>
      `prefix_rule(pattern=${JSON.stringify(entry.argv)}, decision="allow", justification="agent-layer allowlist")`
  );
  return lines.join("\n") + "\n";
}

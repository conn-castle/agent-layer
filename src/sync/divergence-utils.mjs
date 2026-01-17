import path from "node:path";
import { isPlainObject } from "./utils.mjs";

/**
 * Parse a command prefix into argv tokens, returning null if unsafe.
 * @param {string} prefix
 * @returns {{ argv: string[] } | { reason: string }}
 */
export function parseCommandPrefix(prefix) {
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
 * Unescape a regex literal that was escaped by escapeRegexLiteral.
 * @param {string} escaped
 * @returns {string}
 */
export function unescapeRegexLiteral(escaped) {
  return escaped.replace(/\\([.*+?^${}()|[\]\\])/g, "$1");
}

/**
 * Compare arrays of strings for equality.
 * @param {string[]|undefined} a
 * @param {string[]|undefined} b
 * @returns {boolean}
 */
export function equalStringArrays(a, b) {
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
export function equalStringSets(a, b) {
  if (!a && !b) return true;
  if (!a || !b) return false;
  if (a.length !== b.length) return false;
  const aSorted = a.slice().sort();
  const bSorted = b.slice().sort();
  return aSorted.every((val, idx) => val === bSorted[idx]);
}

/**
 * Check whether divergence should be collected for an agent.
 * @param {Set<string>|undefined} enabledAgents
 * @param {string} name
 * @returns {boolean}
 */
export function shouldCheckAgent(enabledAgents, name) {
  if (!enabledAgents) return true;
  return enabledAgents.has(name);
}

/**
 * Convert an absolute path to a repo-relative path for messaging.
 * @param {string} parentRoot
 * @param {string} absPath
 * @returns {string}
 */
export function relPath(parentRoot, absPath) {
  const rel = path.relative(parentRoot, absPath);
  return rel.split(path.sep).join("/");
}

/**
 * Normalize env var names from an env object.
 * @param {unknown} env
 * @returns {{ envVars: string[], known: boolean } | { reason: string }}
 */
export function parseEnvVars(env) {
  if (env === undefined) return { envVars: [], known: false };
  if (!isPlainObject(env)) return { reason: "env is not an object" };
  const keys = Object.keys(env);
  return { envVars: keys, known: true };
}

/**
 * Parse headers into a normalized object.
 * @param {unknown} headers
 * @param {string} filePath
 * @param {string} name
 * @returns {{ headers: Record<string, string> } | { reason: string }}
 */
export function parseHeaders(headers, filePath, name) {
  if (headers === undefined) return { headers: {} };
  if (!isPlainObject(headers)) {
    return { reason: `${filePath}: ${name}.headers must be an object` };
  }
  for (const [key, value] of Object.entries(headers)) {
    if (typeof value !== "string") {
      return {
        reason: `${filePath}: ${name}.headers.${key} must be a string`,
      };
    }
  }
  return { headers };
}

/**
 * Redact server details for output (avoid leaking secrets).
 * @param {{ transport: "stdio", command: string, args: string[], envVars: string[], envVarsKnown: boolean, trust?: boolean } | { transport: "http", url: string, headers: Record<string, string>, bearerTokenEnvVar?: string|null, trust?: boolean }}
 * @returns {{ transport: "stdio", command: string, args: string[], envVars: string[], envVarsKnown: boolean, trust?: boolean } | { transport: "http", url: string, headerKeys: string[], bearerTokenEnvVar?: string, trust?: boolean }}
 */
export function redactServer(server) {
  if (server.transport === "http") {
    const bearerTokenEnvVar =
      typeof server.bearerTokenEnvVar === "string"
        ? server.bearerTokenEnvVar
        : undefined;
    return {
      transport: "http",
      url: server.url,
      headerKeys: Object.keys(server.headers ?? {}).sort(),
      ...(bearerTokenEnvVar ? { bearerTokenEnvVar } : {}),
      ...(server.trust !== undefined ? { trust: server.trust } : {}),
    };
  }
  return server;
}

/**
 * Compare normalized MCP server entries.
 * @param {{ transport: "stdio", command: string, args: string[], envVars: string[], envVarsKnown: boolean, trust?: boolean } | { transport: "http", url: string, headers: Record<string, string>, trust?: boolean }}
 * @param {{ transport: "stdio", command: string, args: string[], envVars: string[], envVarsKnown: boolean, trust?: boolean } | { transport: "http", url: string, headers: Record<string, string>, trust?: boolean } | null}
 * @param {boolean}
 * @returns {boolean}
 */
export function entriesMatch(parsed, expected, compareTrust) {
  if (!expected) return false;
  if (parsed.transport !== expected.transport) return false;
  if (parsed.transport === "http") {
    const parsedHeaders = parsed.headers ?? {};
    const expectedHeaders = expected.headers ?? {};
    if (parsed.url !== expected.url) return false;
    if (
      !equalStringSets(Object.keys(parsedHeaders), Object.keys(expectedHeaders))
    )
      return false;
    for (const key of Object.keys(parsedHeaders)) {
      if (parsedHeaders[key] !== expectedHeaders[key]) return false;
    }
    if (compareTrust && parsed.trust !== expected.trust) return false;
    return true;
  }
  if (parsed.command !== expected.command) return false;
  if (!equalStringArrays(parsed.args, expected.args)) return false;
  if (!equalStringSets(parsed.envVars, expected.envVars)) return false;
  if (compareTrust && parsed.trust !== expected.trust) return false;
  return true;
}

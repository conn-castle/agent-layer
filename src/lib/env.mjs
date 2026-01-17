import { fileExists, readUtf8 } from "../sync/utils.mjs";

/**
 * @typedef {{ env: Record<string, string>, loaded: boolean }} EnvLoadResult
 */

/**
 * Parse a single KEY=VALUE line.
 * @param {string} rawLine
 * @param {string} filePath
 * @returns {[string, string]|null}
 */
function parseEnvLine(rawLine, filePath) {
  const trimmed = rawLine.trim();
  if (!trimmed || trimmed.startsWith("#")) return null;

  let line = trimmed;
  if (line.startsWith("export ")) {
    line = line.slice("export ".length).trim();
  }

  const match = /^[A-Za-z_][A-Za-z0-9_]*=/.exec(line);
  if (!match) {
    throw new Error(
      [
        `ERROR: Invalid env entry in ${filePath}`,
        "",
        `Line: ${rawLine}`,
        "",
        "Fix:",
        "  - Use KEY=value (no spaces around '=')",
        "  - Remove unsupported shell syntax",
      ].join("\n"),
    );
  }

  const eqIndex = line.indexOf("=");
  const key = line.slice(0, eqIndex);
  let value = line.slice(eqIndex + 1);

  if (value.length > 0) {
    const first = value[0];
    const last = value[value.length - 1];
    if (first === '"' || first === "'") {
      if (value.length < 2 || last !== first) {
        throw new Error(
          [
            `ERROR: Invalid env entry in ${filePath}`,
            "",
            `Line: ${rawLine}`,
            "",
            "Fix:",
            "  - Remove unmatched quotes",
            "  - Use simple KEY=value pairs",
          ].join("\n"),
        );
      }
      value = value.slice(1, -1);
    }
  }

  return [key, value];
}

/**
 * Load a .env file into a plain object.
 * @param {string} filePath
 * @returns {EnvLoadResult}
 */
export function loadEnvFile(filePath) {
  if (!fileExists(filePath)) return { env: {}, loaded: false };
  const lines = readUtf8(filePath).split(/\r?\n/);
  const env = {};
  for (const rawLine of lines) {
    const parsed = parseEnvLine(rawLine, filePath);
    if (!parsed) continue;
    const [key, value] = parsed;
    if (Object.prototype.hasOwnProperty.call(env, key)) {
      throw new Error(
        [
          `ERROR: Duplicate env entry in ${filePath}`,
          "",
          `Line: ${rawLine}`,
          "",
          "Fix:",
          "  - Keep only one entry per key",
        ].join("\n"),
      );
    }
    env[key] = value;
  }
  return { env, loaded: true };
}

/**
 * Apply env file values to a base env object.
 * @param {Record<string, string>} baseEnv
 * @param {Record<string, string>} additions
 * @returns {Record<string, string>}
 */
export function applyEnv(baseEnv, additions) {
  return { ...baseEnv, ...additions };
}

import fs from "node:fs";
import path from "node:path";

/**
 * Assert a condition and throw a prefixed error if false.
 * @param {boolean} cond
 * @param {string} msg
 * @returns {void}
 */
export function assert(cond, msg) {
  if (!cond) throw new Error(`agent-layer sync: ${msg}`);
}

/**
 * Check whether a value is a plain object.
 * @param {unknown} value
 * @returns {value is Record<string, unknown>}
 */
export function isPlainObject(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

/**
 * Test for file existence.
 * @param {string} p
 * @returns {boolean}
 */
export function fileExists(p) {
  try {
    fs.accessSync(p, fs.constants.F_OK);
    return true;
  } catch {
    return false;
  }
}

/**
 * Create a directory recursively.
 * @param {string} p
 * @returns {void}
 */
export function mkdirp(p) {
  fs.mkdirSync(p, { recursive: true });
}

/**
 * Read a UTF-8 file.
 * @param {string} p
 * @returns {string}
 */
export function readUtf8(p) {
  return fs.readFileSync(p, "utf8");
}

/**
 * Write a UTF-8 file, ensuring the parent directory exists.
 * @param {string} p
 * @param {string} content
 * @returns {void}
 */
export function writeUtf8(p, content) {
  mkdirp(path.dirname(p));
  fs.writeFileSync(p, content, "utf8");
}

/**
 * Remove a file or directory tree.
 * @param {string} p
 * @returns {void}
 */
export function rmrf(p) {
  fs.rmSync(p, { recursive: true, force: true });
}

/**
 * List files in a directory with a given suffix.
 * @param {string} dir
 * @param {string} suffix
 * @returns {string[]}
 */
export function listFiles(dir, suffix) {
  if (!fileExists(dir)) return [];
  return fs
    .readdirSync(dir)
    .filter((f) => f.endsWith(suffix))
    .sort()
    .map((f) => path.join(dir, f));
}

/**
 * Strip line and block comments from JSON/JSONC text while preserving strings.
 * @param {string} text
 * @returns {string}
 */
export function stripJsoncComments(text) {
  let out = "";
  let inString = false;
  let stringChar = "";
  let inLineComment = false;
  let inBlockComment = false;

  for (let i = 0; i < text.length; i++) {
    const ch = text[i];
    const next = i + 1 < text.length ? text[i + 1] : "";

    if (inLineComment) {
      if (ch === "\n") {
        inLineComment = false;
        out += ch;
      }
      continue;
    }

    if (inBlockComment) {
      if (ch === "*" && next === "/") {
        inBlockComment = false;
        i++;
      }
      continue;
    }

    if (inString) {
      out += ch;
      if (ch === "\\") {
        if (i + 1 < text.length) {
          out += text[i + 1];
          i++;
        }
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
      out += ch;
      continue;
    }

    if (ch === "/" && next === "/") {
      inLineComment = true;
      i++;
      continue;
    }

    if (ch === "/" && next === "*") {
      inBlockComment = true;
      i++;
      continue;
    }

    out += ch;
  }

  return out;
}

/**
 * Remove trailing commas before } or ] while preserving strings.
 * @param {string} text
 * @returns {string}
 */
export function removeTrailingCommas(text) {
  let out = "";
  let inString = false;
  let stringChar = "";

  for (let i = 0; i < text.length; i++) {
    const ch = text[i];

    if (inString) {
      out += ch;
      if (ch === "\\") {
        if (i + 1 < text.length) {
          out += text[i + 1];
          i++;
        }
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
      out += ch;
      continue;
    }

    if (ch === ",") {
      let j = i + 1;
      while (j < text.length && /\s/.test(text[j])) j++;
      if (j < text.length && (text[j] === "}" || text[j] === "]")) {
        continue;
      }
    }

    out += ch;
  }

  return out;
}

/**
 * Read JSON or JSONC from disk with comment and trailing comma tolerance.
 * @param {string} filePath
 * @param {unknown} defaultObj
 * @returns {unknown}
 */
export function readJsonRelaxed(filePath, defaultObj) {
  if (!fileExists(filePath)) return defaultObj;
  const raw = readUtf8(filePath);
  try {
    return JSON.parse(raw);
  } catch {
    const stripped = stripJsoncComments(raw);
    const cleaned = removeTrailingCommas(stripped);
    try {
      return JSON.parse(cleaned);
    } catch {
      throw new Error(
        `agent-layer sync: cannot parse ${filePath}. Please make it valid JSON/JSONC.`
      );
    }
  }
}

import path from "node:path";
import { listFiles, readUtf8 } from "./utils.mjs";

/**
 * @typedef {{ meta: Record<string, string>, body: string }} FrontMatterResult
 */

/**
 * Parse strict frontmatter (a small subset of YAML).
 * @param {string} markdown
 * @param {string} sourcePath
 * @returns {FrontMatterResult}
 */
export function parseFrontMatter(markdown, sourcePath = "<workflow>") {
  // Strip UTF-8 BOM if present.
  if (markdown.charCodeAt(0) === 0xfeff) {
    markdown = markdown.slice(1);
  }

  const lines = markdown.split(/\r?\n/);

  if (lines.length === 0 || lines[0] !== "---") {
    return { meta: {}, body: markdown };
  }

  const meta = {};
  let i = 1;
  let closed = false;

  for (; i < lines.length; i++) {
    const line = lines[i];

    if (line === "---") {
      closed = true;
      break;
    }

    if (!line.trim()) continue; // allow blank lines in header
    if (line.trim().startsWith("#")) continue; // allow comment lines

    const idx = line.indexOf(":");
    if (idx === -1) {
      throw new Error(
        `agentlayer sync: invalid workflow frontmatter in ${sourcePath}: expected "key: value" but got: ${line}`
      );
    }

    const k = line.slice(0, idx).trim();
    const v = line.slice(idx + 1).trim();

    if (!k) {
      throw new Error(
        `agentlayer sync: invalid workflow frontmatter in ${sourcePath}: empty key in line: ${line}`
      );
    }

    if (!["name", "description"].includes(k)) {
      throw new Error(
        `agentlayer sync: unsupported workflow frontmatter key "${k}" in ${sourcePath}. Allowed keys: name, description`
      );
    }

    if (Object.prototype.hasOwnProperty.call(meta, k)) {
      throw new Error(
        `agentlayer sync: duplicate workflow frontmatter key "${k}" in ${sourcePath}`
      );
    }

    const vv = v.replace(/^["']|["']$/g, "");
    if (k === "name" && !vv.trim()) {
      throw new Error(
        `agentlayer sync: workflow frontmatter "name" must be non-empty in ${sourcePath}`
      );
    }
    meta[k] = vv;
  }

  if (!closed) {
    throw new Error(
      `agentlayer sync: invalid workflow frontmatter in ${sourcePath}: missing closing "---"`
    );
  }

  const body = lines.slice(i + 1).join("\n").replace(/^\n+/, "");
  return { meta, body };
}

/**
 * Slugify an arbitrary string.
 * @param {string} s
 * @returns {string}
 */
export function slugify(s) {
  return (
    String(s)
      .trim()
      .toLowerCase()
      .replace(/[^a-z0-9._-]+/g, "-")
      .replace(/-+/g, "-")
      .replace(/^-|-$/g, "") || "workflow"
  );
}

/**
 * Build a generated file banner.
 * @param {string} sourceHint
 * @param {string} regenHint
 * @returns {string}
 */
export function banner(sourceHint, regenHint) {
  return (
    `<!--\n` +
    `  GENERATED FILE - DO NOT EDIT DIRECTLY\n` +
    `  Source: ${sourceHint}\n` +
    `  Regenerate: ${regenHint}\n` +
    `-->\n\n`
  );
}

/**
 * Concatenate instruction fragments from a directory.
 * @param {string} instructionsDir
 * @returns {string}
 */
export function concatInstructions(instructionsDir) {
  const files = listFiles(instructionsDir, ".md");
  const chunks = [];
  for (const f of files) {
    const name = path.basename(f);
    chunks.push(
      `<!-- BEGIN: ${name} -->\n` +
        readUtf8(f).trimEnd() +
        `\n<!-- END: ${name} -->\n`
    );
  }
  return chunks.join("\n").trimEnd() + "\n";
}

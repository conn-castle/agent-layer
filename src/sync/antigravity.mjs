import fs from "node:fs";
import path from "node:path";
import { REGEN_COMMAND } from "./constants.mjs";
import { parseFrontMatter, resolveWorkflowName } from "./instructions.mjs";
import { failOutOfDate } from "./outdated.mjs";
import {
  fileExists,
  listFiles,
  mkdirp,
  readUtf8,
  rmrf,
  writeUtf8,
} from "./utils.mjs";

/**
 * @typedef {{ check: boolean, verbose: boolean }} SyncArgs
 */

const MAX_DESCRIPTION_LENGTH = 250;
const MAX_CONTENT_LENGTH = 12000;

/**
 * Render Antigravity workflow frontmatter.
 * @param {string} description
 * @param {string} sourceHint
 * @returns {string}
 */
function renderAntigravityFrontmatter(description, sourceHint) {
  return (
    `---\n` +
    `# GENERATED FILE\n` +
    `# Source: ${sourceHint}\n` +
    `# Regenerate: ${REGEN_COMMAND}\n` +
    `description: ${description}\n` +
    `---\n\n`
  );
}

/**
 * Normalize and validate an Antigravity workflow description.
 * @param {Record<string, string>} meta
 * @param {string} sourcePath
 * @returns {string}
 */
function resolveDescription(meta, sourcePath) {
  const description = meta.description ? String(meta.description).trim() : "";
  if (!description) {
    throw new Error(
      `agent-layer sync: workflow description is required for Antigravity in ${sourcePath}`,
    );
  }
  if (/[\r\n]/.test(description)) {
    throw new Error(
      `agent-layer sync: workflow description must be a single line in ${sourcePath}`,
    );
  }
  if (description.length > MAX_DESCRIPTION_LENGTH) {
    throw new Error(
      `agent-layer sync: workflow description exceeds ${MAX_DESCRIPTION_LENGTH} characters in ${sourcePath}`,
    );
  }
  return description;
}

/**
 * Normalize and validate Antigravity workflow content.
 * @param {string} body
 * @param {string} sourcePath
 * @returns {string}
 */
function resolveBody(body, sourcePath) {
  if (!body.trim().length) {
    throw new Error(
      `agent-layer sync: workflow body is empty for ${sourcePath}`,
    );
  }
  const trimmedBody = body.trimEnd();
  if (trimmedBody.length > MAX_CONTENT_LENGTH) {
    throw new Error(
      `agent-layer sync: workflow body exceeds ${MAX_CONTENT_LENGTH} characters in ${sourcePath}`,
    );
  }
  return trimmedBody;
}

/**
 * Check whether an Antigravity workflow file is generated.
 * @param {string} workflowPath
 * @returns {boolean}
 */
function isGeneratedAntigravityWorkflow(workflowPath) {
  if (!fileExists(workflowPath)) return false;
  const txt = readUtf8(workflowPath);
  return (
    txt.includes("GENERATED FILE") &&
    txt.includes(`Regenerate: ${REGEN_COMMAND}`)
  );
}

/**
 * Generate Antigravity workflow files from workflow definitions.
 * @param {string} repoRoot
 * @param {string} workflowsDir
 * @param {SyncArgs} args
 * @returns {void}
 */
export function generateAntigravityWorkflows(repoRoot, workflowsDir, args) {
  if (!fileExists(workflowsDir)) {
    throw new Error(
      `agent-layer sync: missing workflows directory at ${workflowsDir}. ` +
        "Restore .agent-layer/config/workflows before running sync.",
    );
  }

  const workflowsRoot = path.join(repoRoot, ".agent", "workflows");
  mkdirp(workflowsRoot);

  const workflowFiles = listFiles(workflowsDir, ".md");
  if (workflowFiles.length === 0) {
    throw new Error(
      `agent-layer sync: no workflow files found in ${workflowsDir}. ` +
        "Add at least one .md file to .agent-layer/config/workflows.",
    );
  }

  const expectedFiles = new Set();

  for (const wfPath of workflowFiles) {
    const md = readUtf8(wfPath);
    const { meta, body } = parseFrontMatter(md, wfPath);
    const name = resolveWorkflowName(meta, wfPath);
    const description = resolveDescription(meta, wfPath);
    const resolvedBody = resolveBody(body, wfPath);
    const outputPath = path.join(workflowsRoot, `${name}.md`);
    expectedFiles.add(outputPath);

    const sourceHint = `.agent-layer/config/workflows/${path.basename(wfPath)}`;
    const content =
      renderAntigravityFrontmatter(description, sourceHint) +
      `${resolvedBody}\n`;

    if (args.check) {
      const old = fileExists(outputPath) ? readUtf8(outputPath) : null;
      if (old !== content) {
        failOutOfDate(
          repoRoot,
          [outputPath],
          "Antigravity workflow files are generated from .agent-layer/config/workflows/*.md.",
        );
      }
    } else {
      writeUtf8(outputPath, content);
      if (args.verbose) console.log(`wrote: ${outputPath}`);
    }
  }

  if (fileExists(workflowsRoot)) {
    const entries = fs.readdirSync(workflowsRoot, { withFileTypes: true });
    for (const entry of entries) {
      if (!entry.isFile()) continue;
      if (!entry.name.endsWith(".md")) continue;
      const workflowPath = path.join(workflowsRoot, entry.name);
      if (expectedFiles.has(workflowPath)) continue;
      if (!isGeneratedAntigravityWorkflow(workflowPath)) continue;

      if (args.check) {
        failOutOfDate(
          repoRoot,
          [workflowPath],
          "Stale generated Antigravity workflow file found (no matching workflow).",
        );
      }

      rmrf(workflowPath);
      if (args.verbose) console.log(`removed stale workflow: ${workflowPath}`);
    }
  }
}

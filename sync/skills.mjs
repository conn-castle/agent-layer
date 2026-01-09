import fs from "node:fs";
import path from "node:path";
import { LEGACY_REGEN_COMMANDS, REGEN_COMMAND } from "./constants.mjs";
import { banner, parseFrontMatter, slugify } from "./instructions.mjs";
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

/**
 * Check whether a Codex skill directory is generated.
 * @param {string} skillDir
 * @returns {boolean}
 */
export function isGeneratedCodexSkill(skillDir) {
  const p = path.join(skillDir, "SKILL.md");
  if (!fileExists(p)) return false;
  const txt = readUtf8(p);
  return (
    txt.includes("GENERATED FILE - DO NOT EDIT DIRECTLY") &&
    (txt.includes(`Regenerate: ${REGEN_COMMAND}`) ||
      LEGACY_REGEN_COMMANDS.some((cmd) => txt.includes(`Regenerate: ${cmd}`)))
  );
}

/**
 * Generate Codex skills from workflow definitions.
 * @param {string} repoRoot
 * @param {string} workflowsDir
 * @param {SyncArgs} args
 * @returns {void}
 */
export function generateCodexSkills(repoRoot, workflowsDir, args) {
  const codexSkillsRoot = path.join(repoRoot, ".codex", "skills");
  mkdirp(codexSkillsRoot);

  const workflowFiles = listFiles(workflowsDir, ".md");
  const expectedFolders = new Set();
  const slugToName = new Map();

  for (const wfPath of workflowFiles) {
    const md = readUtf8(wfPath);
    const { meta, body } = parseFrontMatter(md, wfPath);

    const fallbackName = path.basename(wfPath, ".md");
    const name = (meta.name && String(meta.name).trim()) ? String(meta.name).trim() : fallbackName;
    const description = meta.description ? String(meta.description) : "";
    const folder = slugify(name);

    if (slugToName.has(folder)) {
      throw new Error(
        `agent-layer sync: workflow slug collision: "${name}" and "${slugToName.get(folder)}" both map to "${folder}". ` +
          "Rename one workflow name to avoid collisions."
      );
    }
    slugToName.set(folder, name);

    expectedFolders.add(folder);

    const skillDir = path.join(codexSkillsRoot, folder);
    const skillFile = path.join(skillDir, "SKILL.md");

    if (!name.trim().length) {
      throw new Error(`agent-layer sync: workflow name resolved to empty for ${wfPath}`);
    }
    if (!body.trim().length) {
      throw new Error(`agent-layer sync: workflow body is empty for ${wfPath}`);
    }

    // YAML frontmatter must be first line for compatibility.
    const content =
      `---\n` +
      `name: ${name}\n` +
      `description: ${description}\n` +
      `---\n\n` +
      banner(`.agent-layer/workflows/${path.basename(wfPath)}`, REGEN_COMMAND) +
      `# ${name}\n\n` +
      (description ? `${description}\n\n` : "") +
      `${body.trimEnd()}\n`;

    mkdirp(skillDir);

    if (args.check) {
      const old = fileExists(skillFile) ? readUtf8(skillFile) : null;
      if (old !== content) {
        failOutOfDate(
          repoRoot,
          [skillFile],
          "Codex skills are generated from .agent-layer/workflows/*.md."
        );
      }
    } else {
      writeUtf8(skillFile, content);
      if (args.verbose) console.log(`wrote: ${skillFile}`);
    }
  }

  // Remove stale generated skills (no matching workflow).
  if (fileExists(codexSkillsRoot)) {
    const entries = fs.readdirSync(codexSkillsRoot, { withFileTypes: true });
    for (const e of entries) {
      if (!e.isDirectory()) continue;
      const dirName = e.name;
      if (expectedFolders.has(dirName)) continue;

      const skillDir = path.join(codexSkillsRoot, dirName);
      if (!isGeneratedCodexSkill(skillDir)) continue;

      if (args.check) {
        const maybeSkill = path.join(skillDir, "SKILL.md");
        failOutOfDate(
          repoRoot,
          [fileExists(maybeSkill) ? maybeSkill : skillDir],
          "Stale generated Codex skill found (no matching workflow)."
        );
      }

      rmrf(skillDir);
      if (args.verbose) console.log(`removed stale generated Codex skill: ${skillDir}`);
    }
  }
}

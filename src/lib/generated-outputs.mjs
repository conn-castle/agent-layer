/**
 * Catalog of generated output paths and classification helpers.
 */

/**
 * Repo-relative paths for instruction shims.
 * @type {string[]}
 */
export const INSTRUCTION_SHIM_PATHS = [
  "AGENTS.md",
  ".codex/AGENTS.md",
  "CLAUDE.md",
  "GEMINI.md",
  ".github/copilot-instructions.md",
];

/**
 * Repo-relative paths for MCP config files.
 * @type {string[]}
 */
export const MCP_CONFIG_PATHS = [
  ".mcp.json",
  ".codex/config.toml",
  ".gemini/settings.json",
  ".vscode/mcp.json",
];

/**
 * Repo-relative paths for command allowlist configs.
 * @type {string[]}
 */
export const COMMAND_ALLOWLIST_PATHS = [
  ".gemini/settings.json",
  ".claude/settings.json",
  ".vscode/settings.json",
  ".codex/rules/default.rules",
];

/**
 * Repo-relative paths removed directly by cleanup.
 * @type {string[]}
 */
export const CLEANUP_FILE_PATHS = [
  "AGENTS.md",
  "CLAUDE.md",
  "GEMINI.md",
  ".github/copilot-instructions.md",
  ".codex/AGENTS.md",
  ".codex/config.toml",
  ".codex/rules/default.rules",
];

/**
 * Repo-relative path for the Codex skills directory.
 * @type {string}
 */
export const CODEX_SKILLS_DIR = ".codex/skills";

/**
 * Repo-relative path for the VS Code prompts directory.
 * @type {string}
 */
export const VSCODE_PROMPTS_DIR = ".vscode/prompts";

/**
 * Repo-relative path for the Antigravity workflows directory.
 * @type {string}
 */
export const ANTIGRAVITY_WORKFLOWS_DIR = ".agent/workflows";

/**
 * Repo-relative paths for client settings cleaned by `./al --clean`.
 * @type {{ geminiSettings: string, claudeSettings: string, vscodeSettings: string, vscodeMcp: string, claudeMcp: string }}
 */
export const CLIENT_CONFIG_PATHS = {
  geminiSettings: ".gemini/settings.json",
  claudeSettings: ".claude/settings.json",
  vscodeSettings: ".vscode/settings.json",
  vscodeMcp: ".vscode/mcp.json",
  claudeMcp: ".mcp.json",
};

const INSTRUCTION_SHIM_SET = new Set(INSTRUCTION_SHIM_PATHS);
const MCP_CONFIG_SET = new Set(MCP_CONFIG_PATHS);
const COMMAND_ALLOWLIST_SET = new Set(COMMAND_ALLOWLIST_PATHS);

/**
 * Classify a repo-relative path into generated output buckets.
 * @param {string} relPath
 * @returns {{ instructionShims: boolean, mcpConfigs: boolean, commandAllowlistConfigs: boolean, codexSkills: boolean, vscodePrompts: boolean, antigravityWorkflows: boolean }}
 */
export function classifyGeneratedOutput(relPath) {
  return {
    instructionShims: INSTRUCTION_SHIM_SET.has(relPath),
    mcpConfigs: MCP_CONFIG_SET.has(relPath),
    commandAllowlistConfigs: COMMAND_ALLOWLIST_SET.has(relPath),
    codexSkills: relPath.startsWith(`${CODEX_SKILLS_DIR}/`),
    vscodePrompts:
      relPath.startsWith(`${VSCODE_PROMPTS_DIR}/`) &&
      relPath.endsWith(".prompt.md"),
    antigravityWorkflows:
      relPath.startsWith(`${ANTIGRAVITY_WORKFLOWS_DIR}/`) &&
      relPath.endsWith(".md"),
  };
}

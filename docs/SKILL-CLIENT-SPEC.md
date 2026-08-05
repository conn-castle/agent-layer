# Agent Skills Client Support Spec

As of 2026-08-05, Agent Layer projects skills from the canonical user-managed tier `.agent-layer/skills/` and Git-imported tier `.agent-layer/imported-skills/` into client discovery locations that support directory-format Agent Skills. Those two source tiers are the single source of truth; client skill roots are disposable sync outputs.

## Sources

- Agent Skills specification: https://agentskills.io/specification
- Agent Skills implementation guide: https://agentskills.io/client-implementation/adding-skills-support
- Claude Code skills: https://docs.anthropic.com/en/docs/claude-code/skills
- OpenAI Codex skills: https://developers.openai.com/codex/skills
- VS Code Agent Skills: https://code.visualstudio.com/docs/copilot/customization/agent-skills
- VS Code Copilot settings: https://code.visualstudio.com/docs/copilot/reference/copilot-settings
- GitHub Copilot CLI skills: https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/add-skills
- Google Antigravity skills: https://antigravity.google/docs/skills

## Support Matrix

| Client | Documented project skill locations | Agent Layer projection | Notes |
|---|---|---|---|
| Claude Code | `.claude/skills/<name>/SKILL.md` | `.claude/skills/<name>/SKILL.md` | Claude documents project, personal, enterprise, and plugin skill scopes. Agent Layer keeps Claude separate because Claude directly documents `.claude/skills/` and its VS Code extension support depends on Claude project files. |
| OpenAI Codex | `.agents/skills/<name>/SKILL.md` and Codex-specific metadata under `agents/openai.yaml` | `.agents/skills/<name>/SKILL.md` | Codex `agents/openai.yaml` metadata is deferred to BACKLOG.md item `codex-openai-yaml-skill-metadata`; Agent Layer does not generate it in this slice. |
| Antigravity | `.agents/skills/<name>/SKILL.md` | `.agents/skills/<name>/SKILL.md` | Agent Layer launches Antigravity with repo-local config via `agy --gemini_dir=<repo>/.agy`. Agent Layer projects only the shared `.agents/skills/` tier; the per-agy `<gemini_dir>/skills/` tier (visible to `agy` per the probe baseline) is left to user/global ownership. |
| VS Code / GitHub Copilot | `.github/skills/`, `.claude/skills/`, `.agents/skills/`; configurable with `chat.agentSkillsLocations` | `.agents/skills/<name>/SKILL.md` plus managed `chat.agentSkillsLocations` | Agent Layer enables `.agents/skills/`, disables duplicate generated project locations `.github/skills/` and `.claude/skills/`, and preserves personal skill locations. |
| GitHub Copilot CLI | `.github/skills/`, `.claude/skills/`, `.agents/skills/` for project skills | `.agents/skills/<name>/SKILL.md` | Copilot CLI also supports resources in the skill directory, so the shared tree preserves scripts, references, assets, and other support files. |

## Projection Rules

- Write `.agents/skills/` when at least one shared-skill consumer is enabled: Codex, Antigravity, VS Code/GitHub Copilot, or Copilot CLI.
- Write `.claude/skills/` when Claude Code or the Claude VS Code extension is enabled.
- Require an uppercase `SKILL.md` in every source directory. Lowercase `skill.md` is not accepted, and having both spellings is ambiguous.
- Read each source once under the project lock and project its complete tree byte-for-byte, including hidden and nested files and executable bits. Ignore only `.git`, `.DS_Store`, and `Thumbs.db`.
- Accept additional frontmatter fields without interpreting or filtering them. Provider-specific fields therefore reach every enabled client projection unchanged.
- Reject symlinks and every other non-directory, non-regular source node without dereferencing or silently skipping it.
- Replace each enabled client skill root as one complete staged tree. Direct edits and extra files in a client root are discarded on the next sync.
- Remove the entire client skill root when its projection is disabled.

### Ownership of current projection paths

Agent Layer exclusively owns `.agents/skills/` and `.claude/skills/` in an Agent Layer project. Do not install or edit skills directly in either directory. Put user-managed skills in `.agent-layer/skills/`; imported skills live in `.agent-layer/imported-skills/` and are managed through `al skills`. Every other entry in the client roots is removed during sync.

### Ownership of legacy projection paths

If a project uses Agent Layer, it must use Agent Layer to manage skills. `.agent-layer/skills/` and `.agent-layer/imported-skills/` are the two canonical source tiers, and the following client-side directories are claimed exclusively by Agent Layer and removed unconditionally on every `al sync`:

- `.codex/skills/`
- `.agent/skills/` (singular; legacy Antigravity location)
- `.gemini/skills/`
- `.github/skills/`
- `.vscode/prompts/`

Any content placed in those directories — generated or hand-authored — is destroyed during sync. Users who want skills surfaced through Codex, Antigravity, GitHub Copilot, or Copilot CLI must define them in one of the two canonical source tiers. The unconditional removal is intentional: it keeps the projection deterministic and prevents drift between a source and any disposable client folder.

## VS Code Settings Contract

When `[agents.vscode]` is enabled, Agent Layer writes `chat.agentSkillsLocations` inside the existing managed `.vscode/settings.json` block:

```json
{
  ".agents/skills": true,
  ".claude/skills": false,
  ".github/skills": false,
  "~/.agents/skills": true,
  "~/.claude/skills": true,
  "~/.copilot/skills": true
}
```

This makes the shared project tree explicit and prevents VS Code/GitHub Copilot from loading duplicate generated project skills from `.claude/skills/` or legacy `.github/skills/`. Personal locations remain enabled.

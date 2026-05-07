# Agent Skills Client Support Spec

As of 2026-05-07, Agent Layer projects skills from the canonical source directory `.agent-layer/skills/<name>/SKILL.md` into client discovery locations that support directory-format Agent Skills. The source skill directory is the single source of truth; generated client folders are disposable sync outputs.

## Sources

- Agent Skills specification: https://agentskills.io/specification
- Agent Skills implementation guide: https://agentskills.io/client-implementation/adding-skills-support
- Claude Code skills: https://docs.anthropic.com/en/docs/claude-code/skills
- OpenAI Codex skills: https://developers.openai.com/codex/skills
- Gemini CLI Agent Skills: https://geminicli.com/docs/cli/skills/
- VS Code Agent Skills: https://code.visualstudio.com/docs/copilot/customization/agent-skills
- VS Code Copilot settings: https://code.visualstudio.com/docs/copilot/reference/copilot-settings
- GitHub Copilot CLI skills: https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/add-skills
- Google Antigravity skills: https://antigravity.google/docs/skills

## Support Matrix

| Client | Documented project skill locations | Agent Layer projection | Notes |
|---|---|---|---|
| Claude Code | `.claude/skills/<name>/SKILL.md` | `.claude/skills/<name>/SKILL.md` | Claude documents project, personal, enterprise, and plugin skill scopes. Agent Layer keeps Claude separate because Claude directly documents `.claude/skills/` and its VS Code extension support depends on Claude project files. |
| OpenAI Codex | `.agents/skills/<name>/SKILL.md` and Codex-specific metadata under `agents/openai.yaml` | `.agents/skills/<name>/SKILL.md` | Codex `agents/openai.yaml` metadata is deferred to BACKLOG.md item `codex-openai-yaml-skill-metadata`; Agent Layer does not generate it in this slice. |
| Gemini CLI | `.agents/skills/<name>/SKILL.md` and `.gemini/skills/<name>/SKILL.md` | `.agents/skills/<name>/SKILL.md` | Gemini gives `.agents/skills/` precedence over `.gemini/skills/` within the same workspace tier, so Agent Layer uses only the shared path to avoid duplicates. |
| VS Code / GitHub Copilot | `.github/skills/`, `.claude/skills/`, `.agents/skills/`; configurable with `chat.agentSkillsLocations` | `.agents/skills/<name>/SKILL.md` plus managed `chat.agentSkillsLocations` | Agent Layer enables `.agents/skills/`, disables duplicate generated project locations `.github/skills/` and `.claude/skills/`, and preserves personal skill locations. |
| GitHub Copilot CLI | `.github/skills/`, `.claude/skills/`, `.agents/skills/` for project skills | `.agents/skills/<name>/SKILL.md` | Copilot CLI also supports resources in the skill directory, so the shared tree preserves scripts, references, assets, and other support files. |
| Antigravity | `.agents/skills/<name>/SKILL.md`; `.agent/skills/` is legacy compatibility | `.agents/skills/<name>/SKILL.md` | Agent Layer treats singular `.agent/skills/` as a retired generated output and removes it during sync. |

## Projection Rules

- Write `.agents/skills/` when at least one shared-skill consumer is enabled: Codex, Gemini, Antigravity, VS Code/GitHub Copilot, or Copilot CLI.
- Write `.claude/skills/` when Claude Code or the Claude VS Code extension is enabled.
- Rebuild generated top-level `SKILL.md` from parsed source metadata and body.
- Copy all non-hidden, non-symlink support files from the source skill directory into every generated skill output, preserving file modes.
- Skip top-level source `SKILL.md` and `skill.md` during resource copying because the generated `SKILL.md` is rebuilt.

### Ownership of legacy projection paths

If a project uses Agent Layer, it must use Agent Layer to manage skills. `.agent-layer/skills/` is the single source of truth, and the following client-side directories are claimed exclusively by Agent Layer and removed unconditionally on every `al sync`:

- `.codex/skills/`
- `.agent/skills/` (singular; legacy Antigravity location)
- `.gemini/skills/`
- `.github/skills/`
- `.vscode/prompts/`

Any content placed in those directories — generated or hand-authored — is destroyed during sync. Users who want skills surfaced through Codex, Gemini, GitHub Copilot, Antigravity, or Copilot CLI must define them in `.agent-layer/skills/`; Agent Layer projects them into the shared `.agents/skills/` (or `.claude/skills/`) location instead. The unconditional removal is intentional: it keeps the projection deterministic and prevents drift between the source and any disposable client folder.

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

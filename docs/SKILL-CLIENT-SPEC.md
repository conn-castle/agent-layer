# Agent Skills Client Support Spec

As of 2026-05-21, Agent Layer projects skills from the canonical source directory `.agent-layer/skills/<name>/SKILL.md` into client discovery locations that support directory-format Agent Skills. The source skill directory is the single source of truth; generated client folders are disposable sync outputs.

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
- Copy each skill's complete source tree byte-for-byte, including the exact `SKILL.md` bytes and the executable bit. Agent Layer injects nothing into projected content: no generated header, no re-rendered frontmatter. What an agent reads is exactly what the skill author wrote.
- Project the same exhaustive file set for user-managed (`.agent-layer/skills/`) and imported (`.agent-layer/imported-skills/`) sources. Hidden files and dotfiles are part of a skill; only `.git`, `.DS_Store`, and `Thumbs.db` are excluded. Symlinks are skipped and never dereferenced.
- Project a legacy lowercase source `skill.md` under the canonical `SKILL.md` name with its bytes unchanged, because clients only look for `SKILL.md`.
- Do not filter client-specific frontmatter out of the portable `.agents/skills/` output. A field such as Claude Code's `disable-model-invocation` reaches every client, because an exact copy cannot drop one key without re-serializing the document and losing the fidelity these rules exist to guarantee. A client that does not recognize a field ignores it.

### Ownership of projected skill directories

Because projected content carries no Agent Layer marker, ownership is recorded beside the skills rather than inside them. Each projected skills directory holds `.agent-layer-skills.json` listing the skill directories Agent Layer created and their last projected canonical tree hashes.

- A complete tree is staged before it replaces live projected content.
- An existing same-name directory must already be owned; unowned client content is never overwritten.
- An owned directory is refreshed or removed only while its current tree still matches the recorded projection hash.
- A skill directory Agent Layer never created — hand-authored or client-installed — is left untouched, however much it looks like a projected skill.
- An unreadable or unsupported manifest fails the sync with instructions to delete it, rather than silently forgetting what Agent Layer owns.
- A projection directory written before this manifest existed has its previously generated skills adopted once, identified by the retired generated header. New output never carries that header, so the migration cannot misfire.

### Ownership of legacy projection paths

If a project uses Agent Layer, it must use Agent Layer to manage skills. `.agent-layer/skills/` is the single source of truth, and the following client-side directories are claimed exclusively by Agent Layer and removed unconditionally on every `al sync`:

- `.codex/skills/`
- `.agent/skills/` (singular; legacy Antigravity location)
- `.gemini/skills/`
- `.github/skills/`
- `.vscode/prompts/`

Any content placed in those directories — generated or hand-authored — is destroyed during sync. Users who want skills surfaced through Codex, Antigravity, GitHub Copilot, or Copilot CLI must define them in `.agent-layer/skills/`; Agent Layer projects them into the shared `.agents/skills/` (or `.claude/skills/`) location instead. The unconditional removal is intentional: it keeps the projection deterministic and prevents drift between the source and any disposable client folder.

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

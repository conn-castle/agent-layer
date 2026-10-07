# Issues

Note: This is an agent-layer memory file. It is primarily for agent use.

## Purpose
Deferred defects, maintainability refactors, technical debt, risks, and engineering concerns. Add an entry only when you are not fixing it now.

## Format
- Insert new entries immediately below `<!-- ENTRIES START -->` (most recent first).
- Keep each entry **3–5 lines**.
- Line 1 starts with `- Issue YYYY-MM-DD <id>:` and a short title.
- Lines 2–5 are indented by **4 spaces** and use `Key: Value`.
- Keep **exactly one blank line** between entries.
- Prevent duplicates: search the file and merge/rewrite instead of adding near-duplicates.
- When fixed, remove the entry from this file.
- Describe the problem without choosing a solution or listing options.
- Use `Next step` only when the action is useful regardless of the eventual solution. Otherwise, use `Open question: <decision needed>`.

### Entry template
```text
- Issue YYYY-MM-DD short-slug: Short title
    Priority: Critical | High | Medium | Low. Area: <area>
    Description: <observed problem or risk>
    Next step: <smallest concrete next action>
    Notes: <optional dependencies/constraints>
```

## Open issues

<!-- ENTRIES START -->

- Issue 2026-10-06 codex-bundled-skills-cleanup-race: Repo-local Codex bundled-skill installation can race AL legacy cleanup.
    Priority: Medium. Area: Codex launch and sync.
    Description: An additional AL Codex pane failed before native startup because removing `.codex/skills` returned `directory not empty` while the native bundled-skill installer recreated that directory.
    Next step: Reproduce concurrent native cache installation and legacy cleanup, then distinguish Codex-owned cache from retired AL outputs.
    Notes: Verified native-acceptance-v8/same-pane-visible.json under `.agent-layer/tmp/herdr-daemon-fix`; ordinary legacy cleanup is in internal/sync/prompts.go. Recovery fixtures using bundled skills disabled do not prove this launch path.

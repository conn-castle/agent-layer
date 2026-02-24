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

### Entry template
```text
- Issue YYYY-MM-DD abcdef: Short title
    Priority: Critical | High | Medium | Low. Area: <area>
    Description: <observed problem or risk>
    Next step: <smallest concrete next action>
    Notes: <optional dependencies/constraints>
```

## Open issues

<!-- ENTRIES START -->

- Issue 2026-02-22 config-key-style-unify: Config key naming mixes kebab-case and snake_case
    Priority: Medium. Area: config schema / UX.
    Description: The config currently mixes naming styles (for example `agents.claude-vscode` vs `agents.codex.agent_specific` and `reasoning_effort`), which makes the schema feel inconsistent and harder to learn.
    Next step: Define one canonical key style based on TOML/client ecosystem best practice, then migrate all config keys, templates, docs, and tests to that convention.

- Issue 2026-02-16 skill-standard-rename: Rename slash-commands to skills and align with standard
    Priority: High. Area: slash-commands / skills.
    Description: Slash-commands should be renamed to "skills" to align with the established skill standard. This includes supporting supplemental folders within the skill directory and updating `al doctor` to verify compatibility using the standard toolset.
    Next step: Perform a global rename of slash-command terminology and implement structural/validation updates to match the skill standard.

- Issue 2026-02-12 3c5f958f: installer struct accumulating responsibilities
    Priority: Low. Area: install / maintainability.
    Description: The `installer` struct in `internal/install/` has 23 fields and 57+ methods spread across 8+ files. While logically grouped, this concentration increases coupling risk as features continue to be added.
    Next step: Audit current method count (Phase 11 is complete). If it exceeds ~70, extract sub-structs (e.g., `templateManager`, `ownershipClassifier`). Scheduled in Phase 13 (maintenance).

- Issue 2026-02-08 tmpl-mk: Slash-command templates reference non-existent Makefile targets
    Priority: Low. Area: templates / developer experience.
    Description: `finish-task.md`, `fix-issues.md`, and `cleanup-code.md` templates reference `make test-fast` and other repo-specific optional targets. The templates already guard these with conditional language ("preferred when available", "only if already present"), but agents may still attempt commands in repos that do not provide them.
    Next step: Consider whether the conditional language is sufficient, or whether a stronger guard (e.g., checking target existence before invocation) would reduce noise.
    Notes: Reconfirmed by documentation audit on 2026-02-18; keep this as a template-level guardrail issue (not a repo-local Makefile requirement).

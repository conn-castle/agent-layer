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

- Issue 2026-02-25 playwright-headless-parity: Evaluate headless Playwright mode without functional regressions
    Priority: Medium. Area: test automation / Playwright runner UX.
    Description: Playwright running in headed mode is noisy and disruptive during normal development. We should assess whether headless can be the default while preserving behavior.
    Next step: Run representative Playwright flows in headed vs headless mode, document any behavior differences, and only switch defaults if parity is confirmed.
    Notes: Keep an explicit opt-in path for headed runs for local debugging even if headless becomes the default.

- Issue 2026-02-25 claude-auth-ignores-config-dir: Claude Code auth ignores CLAUDE_CONFIG_DIR (upstream)
    Priority: Medium. Area: agents.claude / auth isolation.
    Description: Claude Code stores OAuth credentials in the OS credential store (macOS Keychain under service `"Claude Code-credentials"`, Linux via libsecret/gnome-keyring) using a fixed service name, regardless of `CLAUDE_CONFIG_DIR`. Per-repo login isolation does not work. Reported upstream.
    Next step: Track upstream fix. The fix would need Claude Code to namespace credential-store entries by `CLAUDE_CONFIG_DIR` (e.g., `"Claude Code-credentials-<hash>"`).
    Notes: No clean workaround exists. macOS Keychain is system-wide and keyed by service name, not directory path. `XDG_CONFIG_HOME` does not affect Keychain. Symlinks are not per-repo. A `security` CLI wrapper could intercept calls but is fragile and unsupportable.

- Issue 2026-02-22 config-key-style-unify: Config key naming mixes kebab-case and snake_case
    Priority: Medium. Area: config schema / UX.
    Description: The config currently mixes naming styles (for example `agents.claude-vscode` vs `agents.codex.agent_specific` and `reasoning_effort`), which makes the schema feel inconsistent and harder to learn.
    Next step: Define one canonical key style based on TOML/client ecosystem best practice, then migrate all config keys, templates, docs, and tests to that convention.

- Issue 2026-02-16 skill-standard-rename: Rename slash-commands to skills and align with standard
    Priority: High. Area: slash-commands / skills.
    Description: Slash-commands should be renamed to "skills" to align with the established skill standard. This includes supporting supplemental folders within the skill directory and updating `al doctor` to verify compatibility using the standard toolset.
    Next step: Perform a global rename of slash-command terminology and implement structural/validation updates to match the skill standard.

- Issue 2026-02-08 tmpl-mk: Slash-command templates reference non-existent Makefile targets
    Priority: Low. Area: templates / developer experience.
    Description: `finish-task.md`, `fix-issues.md`, and `cleanup-code.md` templates reference `make test-fast` and other repo-specific optional targets. The templates already guard these with conditional language ("preferred when available", "only if already present"), but agents may still attempt commands in repos that do not provide them.
    Next step: Consider whether the conditional language is sufficient, or whether a stronger guard (e.g., checking target existence before invocation) would reduce noise.
    Notes: Reconfirmed by documentation audit on 2026-02-18; keep this as a template-level guardrail issue (not a repo-local Makefile requirement).

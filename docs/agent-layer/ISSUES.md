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

- Issue 2026-02-08 tmpl-mk: Slash-command templates reference non-existent Makefile targets
    Priority: Low. Area: templates / developer experience.
    Description: `finish-task.md`, `fix-issues.md`, and `cleanup-code.md` templates reference `make test-fast` and `make dead-code` which do not exist in the Makefile. Templates note these as optional/conditional, but they may confuse agents in repos that do not provide them.
    Next step: Either add `test-fast` and `dead-code` Makefile targets, or clarify the template language to make the conditional nature more explicit.

- Issue 2026-02-08 u7v0migr: Ensure v0.7.0 migration row is release-ready
    Priority: Medium. Area: documentation / release process.
    Description: `site/docs/upgrades.mdx` includes a `v0.7.0` migration row that is intentionally placeholder-level before release; this must be validated and populated if any real migration steps are required.
    Next step: During v0.7.0 release prep, verify actual upgrade-impact changes and update the migration row (and release notes) with explicit manual steps or confirm none are required.
    Notes: Keep row explicit even when no additional migration is needed, and align with sequential `N-1 -> N` compatibility policy.

- Issue 2026-01-26 j4k5l6: Managed file diff visibility for overwrite decisions
    Priority: Medium. Area: install / UX.
    GitHub: https://github.com/conn-castle/agent-layer/issues/30
    Description: Users cannot easily determine whether differences in managed files are due to intentional local customizations they want to keep, or due to agent-layer version updates that should be accepted. This makes overwrite decisions difficult and error-prone.
    Next step: Implement a diff or comparison view (e.g., `al diff` or during `al init --overwrite`) that shows what changed between local files and the new template versions, with annotations or categories for change types when possible.

- Issue 2026-01-24 a1b2c3: VS Code slow first launch in agent-layer folder
    Priority: Low. Area: developer experience.
    GitHub: https://github.com/conn-castle/agent-layer/issues/39
    Description: Launching VS Code in the agent-layer folder takes a very long time on first use, likely due to extension initialization, indexing, or MCP server startup.
    Next step: Profile VS Code startup to identify the bottleneck (extensions, language servers, MCP servers, or workspace indexing).

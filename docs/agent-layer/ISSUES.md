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

- Issue 2026-01-27 j7k8l9: Sync engine bypasses System interface for config loading
    Priority: High. Area: architecture / testability.
    Description: `sync.Run` calls `config.LoadProjectConfig`, which uses direct `os` calls (ReadFile, ReadDir), bypassing the `System` interface. This prevents testing the sync engine with mock filesystems.
    Next step: Refactor `internal/config` to accept `fs.FS` or a compatible interface.
    Notes: Found during proactive audit.

- Issue 2026-01-27 d1e2f3: Inconsistent System interface adoption
    Priority: Medium. Area: architecture / technical debt.
    Description: `internal/install` and `internal/dispatch` still rely on direct `os` calls and global patching, ignoring the new `System` interface pattern used in `internal/sync`. This creates competing patterns and hampers testability.
    Next step: Refactor `internal/install` and `internal/dispatch` to accept the `System` interface.
    Notes: Violation of Decision 2026-01-25 (Sync dependency injection).

- Issue 2026-01-24 a1b2c3: VS Code slow first launch in agent-layer folder
    Priority: Low. Area: developer experience.
    Description: Launching VS Code in the agent-layer folder takes a very long time on first use, likely due to extension initialization, indexing, or MCP server startup.
    Next step: Profile VS Code startup to identify the bottleneck (extensions, language servers, MCP servers, or workspace indexing).

- Issue 2026-01-26 j4k5l6: Managed file diff visibility for overwrite decisions
    Priority: Medium. Area: install / UX.
    Description: Users cannot easily determine whether differences in managed files are due to intentional local customizations they want to keep, or due to agent-layer version updates that should be accepted. This makes overwrite decisions difficult and error-prone.
    Next step: Implement a diff or comparison view (e.g., `al diff` or during `al init --overwrite`) that shows what changed between local files and the new template versions, with annotations or categories for change types when possible.
    Notes: Related to Issue g2h3i4 but distinct—that issue is about prompt flow, this is about visibility into what's actually different.

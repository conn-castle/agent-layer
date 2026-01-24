# Issues

Purpose: Deferred defects, maintainability refactors, technical debt, risks, and engineering concerns.

Notes for updates:
- Add an entry only when you are not fixing it now.
- Keep each entry 3 to 5 lines (the first line plus 2 to 4 indented lines).
- Lines 2 to 5 must be indented by 4 spaces so they stay associated with the entry.
- Prevent duplicates by searching and merging.
- Remove entries when fixed.

Entry format:
- Issue YYYY-MM-DD abcdef: Short title
    Priority: Critical, High, Medium, or Low. Area: <area>
    Description: <observed problem or risk>
    Next step: <smallest concrete next action>
    Notes: <optional dependencies or constraints>

## Open issues

<!-- ENTRIES START -->

- Issue 2026-01-18 e4f5g6: Memory file template structure investigation
    Priority: Medium. Area: templates.
    Description: Should templates in .agent-layer only contain headers, and how should generated content be handled when overwriting?
    Next step: Review existing template synchronization logic and define the intended behavior for content preservation.

- Issue 2026-01-18 l8m9n0: Limit exposed commands for GitHub MCP
    Priority: Low. Area: mcp configuration.
    Description: The GitHub MCP server (HTTP transport) exposes many tools. HTTP-based MCP servers do not support client-side tool filtering; the MCP protocol has no standard mechanism for this.
    Next step: Evaluate alternatives: (1) request upstream tool filtering support, (2) implement an MCP proxy that filters tools, or (3) accept current behavior and document the limitation.

- Issue 2026-01-24 a1b2c3: VS Code slow first launch in agent-layer folder
    Priority: Low. Area: developer experience.
    Description: Launching VS Code in the agent-layer folder takes a very long time on first use, likely due to extension initialization, indexing, or MCP server startup.
    Next step: Profile VS Code startup to identify the bottleneck (extensions, language servers, MCP servers, or workspace indexing).

- Issue 2026-01-24 9a8b7c: Refactor global function patching to DI
    Priority: High. Area: architecture.
    Description: Systemic pattern of patching global variables (e.g. `var lookPath = exec.LookPath`) prevents parallel testing and invites race conditions.
    Next step: Refactor `internal/sync` to use interface-based dependency injection and enable `t.Parallel()`.

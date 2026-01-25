# Issues

Note: This is an agent-layer memory file. It is primarily for agent use.

## Open issues

<!-- ENTRIES START -->

- Issue 2026-01-24 a1b2c3: VS Code slow first launch in agent-layer folder
    Priority: Low. Area: developer experience.
    Description: Launching VS Code in the agent-layer folder takes a very long time on first use, likely due to extension initialization, indexing, or MCP server startup.
    Next step: Profile VS Code startup to identify the bottleneck (extensions, language servers, MCP servers, or workspace indexing).

- Issue 2026-01-24 9a8b7c: Refactor global function patching to DI
    Priority: High. Area: architecture.
    Description: Systemic pattern of patching global variables (e.g. `var lookPath = exec.LookPath`) prevents parallel testing and invites race conditions.
    Next step: Refactor `internal/sync` to use interface-based dependency injection and enable `t.Parallel()`.

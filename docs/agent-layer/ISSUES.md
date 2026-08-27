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

- Issue 2026-07-28 dispatch-mcp-start-transport-window: An MCP dispatch_start disconnect can orphan a handle
    Priority: Medium. Area: Agent Dispatch MCP interface
    Description: `dispatch_start` is an RPC acknowledgement rather than a direct write to the caller's terminal. If the transport disconnects after the backend starts but before the client observes the response, the dispatch keeps running durably while the caller never learns its handle. This slice deliberately added no idempotency state and no listing API.
    Open question: Has this been observed in practice, justifying a caller-supplied idempotency key or narrow handle-recovery read?
    Notes: Evidence remains under `.agent-layer/tmp/runs/`; documented in docs/AGENT-DISPATCH.md and the dispatch-mcp-interface decision.

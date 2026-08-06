---
name: dispatch-agent
description: Use Agent Dispatch MCP tools only when the user names an external dispatch target or another skill explicitly requires dispatch. Do not use it for generic subagent, second-agent, or fresh-context requests; use the built-in subagent instead.
compatibility: Requires the built-in `agent-layer` MCP server and a configured provider.
allowed-tools: mcp__agent-layer__dispatch_options mcp__agent-layer__dispatch_start mcp__agent-layer__dispatch_wait mcp__agent-layer__dispatch_continue mcp__agent-layer__dispatch_cancel Bash(cat:*)
---

# Dispatch Agent

If the MCP tools are unavailable, report the missing server; do not substitute
command-line calls.

1. Call `dispatch_options`; map the requested target to `agent`, `model`, and
   `reasoning_effort`. Ask if ambiguous.
2. Call `dispatch_start` once with exactly one prompt source and retain its
   session handle. Do not replace active work.
3. Call `dispatch_wait` with the handle. If it returns `running` or is
   interrupted, call it again with the same handle. On `completed`, read the
   Markdown file at `result_path`; report `failed` or `cancelled`.

For parallel work, call `dispatch_start` once per independent conversation and
retain each handle. Call `dispatch_wait` for those handles in parallel when supported.

## Continuing a conversation

After a terminal result, use `dispatch_continue` only for useful follow-up,
requested information, or corrective action within the current scope. It
preserves the provider conversation context. Pass the same handle and exactly
one prompt source, then call `dispatch_wait` again.

Use `dispatch_start` when fresh context is required, not `dispatch_continue`.

## Cancelling a conversation

`dispatch_cancel` permanently stops active provider work. Call it only when the
user explicitly requests cancellation or an active skill explicitly instructs
you to abandon the dispatch.

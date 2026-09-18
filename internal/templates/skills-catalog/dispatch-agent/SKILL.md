---
name: dispatch-agent
description: Use Agent Dispatch MCP tools only when the user names an external dispatch target or another skill explicitly requires dispatch. Do not use it for generic subagent, second-agent, or fresh-context requests; use the built-in subagent instead.
compatibility: Requires the built-in `agent-layer` MCP server and a configured provider.
allowed-tools: mcp__agent-layer__dispatch_options mcp__agent-layer__dispatch_start mcp__agent-layer__dispatch_wait mcp__agent-layer__dispatch_continue mcp__agent-layer__dispatch_cancel mcp__agent-layer__dispatch_inspect mcp__agent-layer__dispatch_output
---

# Dispatch Agent

If the MCP tools are unavailable, report the missing server; do not substitute
command-line calls.

1. Call `dispatch_options`; map the requested target to `agent`, `model`, and
   `reasoning_effort`. Ask if ambiguous.
2. Call `dispatch_start` once with exactly one prompt source and retain its
   `handle` and `invocation_id`. Do not replace active work.
3. Call `dispatch_wait` with exactly one of `handle` or `invocation_id`. If it
   returns `running` or is interrupted, call it again with the same selector.
   An old `invocation_id` never follows a later continuation. On `completed`,
	call `dispatch_output` with artifact `final_answer` (wait also returns
	`result_path`). On `failed` or `cancelled`, call `dispatch_inspect` and
	`dispatch_output` with artifact `events` for available partial output; cancelled is not
   termination proof.

For parallel work, call `dispatch_start` once per independent conversation and
retain each handle and invocation_id. Call `dispatch_wait` for those selectors
in parallel when supported.

## Continuing a conversation

After a terminal result, use `dispatch_continue` only for useful follow-up,
requested information, or corrective action within the current scope. It
preserves the provider conversation context and creates a new `invocation_id`.
Pass the same handle and exactly one prompt source, then call `dispatch_wait`
again.

Use `dispatch_start` when fresh context is required, not `dispatch_continue`.
Do not continue while `termination_confirmed` is false for the active
invocation.

## Cancelling a conversation

`dispatch_cancel` requests that provider work stop. Call it only when the
user explicitly requests cancellation or an active skill explicitly instructs
you to abandon the dispatch. Inspect `termination_confirmed` before treating
the invocation as unable to execute. Repeated cancel retries an unconfirmed
stop.

---
name: agent-dispatch
description: Use `al dispatch` only when the user names an external dispatch target or another skill explicitly requires dispatch. Do not use it for generic subagent, second-agent, or fresh-context requests; use the built-in subagent instead.
compatibility: Requires the project Agent Layer CLI (`al`) and a configured provider.
allowed-tools: Bash(al:*) Bash(cat:*)
---

# al dispatch

Run `al dispatch --help` for the command list and
`al dispatch <command> --help` for current syntax.

## Workflow

1. Run `al dispatch options`. Resolve the caller's role or shorthand against its
   metadata to one exact available agent and any requested overrides. Accept
   only unambiguous shorthand; otherwise ask.
2. Put a substantial prompt under `.agent-layer/tmp/`, then run
   `al dispatch start --agent <agent> --prompt-file <path>`, adding any resolved
   model, reasoning-effort, or skill flags. For short text, use
   `al dispatch start --agent <agent> --prompt <text>` instead. Parse the JSON
   and retain the handle.
3. Run `al dispatch wait <handle>`. It blocks while the current invocation is
   `running`; do not poll or inspect provider output.
4. On `completed`, read and verify the Markdown file at `result_path`. On
   `failed` or `cancelled`, report that terminal result.

For parallel work, run `al dispatch start` once per independent conversation,
retain every handle, and run `al dispatch wait <handle>` for each.

Run `al dispatch cancel <handle>` only when the user requests cancellation.
Repeating it for an already cancelled invocation is safe; cancelling a
completed or failed invocation is an error.

## Continuing a conversation

After a terminal result, the same conversation may continue for useful
follow-up, requested information, or corrective action within the current
scope and authority. Run `al dispatch continue <handle> --prompt <text>` or
`al dispatch continue <handle> --prompt-file <path>`, then run
`al dispatch wait <handle>` again.

## Guardrails

- The only public states are `running`, `completed`, `failed`, and `cancelled`.
- Never repeat `al dispatch start` or `al dispatch continue` to recover
  uncertain work; run `al dispatch wait <handle>` with the known handle.
- Standard output is one JSON object. Do not parse standard error as state.
- Do not use Agent Dispatch for shell work, tests, web retrieval, browser
  automation, or to bypass caller restrictions.

## Definition of done

The exact conversation reaches a terminal state, every completed `result_path`
is read and verified, created caller artifacts are reported, and failures or
unresolved questions are surfaced.

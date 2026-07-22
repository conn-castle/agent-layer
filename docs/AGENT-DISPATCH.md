# Agent Dispatch

Agent Dispatch runs headless provider conversations. It has one read-only
discovery command plus one opaque conversation handle, four lifecycle commands,
and four states.

## Commands

```text
al dispatch options

al dispatch start --agent <agent> [--model <model>] \
  [--reasoning-effort <effort>] [--skill <skill>] \
  (--prompt <text> | --prompt-file <path>)

al dispatch wait <handle>

al dispatch continue <handle> \
  (--prompt <text> | --prompt-file <path>)

al dispatch cancel <handle>
```

`options` returns the known dispatch agents, their current availability, configured
defaults, and supported model and reasoning-effort overrides.

`start` requires an agent and exactly one prompt source. Model and reasoning
effort are optional overrides. When omitted, Agent Layer uses its configured
value; when that is also empty, it omits the provider flag so the provider uses
its own default. `start` returns immediately after durably creating the
conversation and starting its first invocation.

`continue` uses the conversation's existing agent, model, reasoning effort,
and provider context. It requires exactly one new prompt source and returns
immediately after starting the next invocation.

`--prompt-file` reads the named file as the prompt. It avoids shell escaping
and command-length limits; `--prompt` remains convenient for short prompts.

## States

The current invocation has exactly one public state:

```text
running -> completed | failed | cancelled
```

Terminal states are immutable. Continuing a terminal conversation creates a
new current invocation in `running`; it does not change the previous
invocation.

| Command | `running` | `completed` | `failed` | `cancelled` |
| --- | --- | --- | --- | --- |
| `wait` | Blocks until terminal | Returns `result_path` | Returns the failure | Returns `cancelled` |
| `continue` | Errors | Starts the next invocation | Starts the next invocation | Starts the next invocation |
| `cancel` | Cancels the invocation | Errors: already completed | Errors: already failed | Returns `cancelled` successfully |

`failed` means the invocation could not complete, for example because of a
provider, authentication, network, process, or response error. `cancelled`
means a caller intentionally stopped it. Neither state invalidates the
conversation, so either may be continued.

Only one invocation may run for a conversation at a time. Concurrent
`continue` calls cannot start duplicate work: one may succeed and the others
must fail without contacting the provider.

## Output

Every successful command writes exactly one JSON object to standard output.
Diagnostics go to standard error. Field names and state values are stable API
values.

`options` returns:

```json
{
  "agents": [
    {
      "agent": "codex",
      "available": true,
      "model": {
        "supported": true,
        "configured": "gpt-5.6",
        "suggestions": ["gpt-5.6"],
        "allow_custom": true
      },
      "reasoning_effort": {
        "supported": true,
        "configured": "medium",
        "suggestions": ["low", "medium", "high"],
        "allow_custom": true
      }
    }
  ]
}
```

Known but unavailable agents remain present with `available: false` and an
`unavailable_reason`. Unsupported overrides have `supported: false`; callers
must omit those flags.

`start` and `continue` return:

```json
{
  "handle": "abc123",
  "state": "running"
}
```

`wait` on a completed invocation returns:

```json
{
  "handle": "abc123",
  "state": "completed",
  "result_path": "/absolute/path/to/result.md"
}
```

The Markdown result is written atomically before the invocation becomes
`completed`. Each invocation has its own immutable result file. A completed
invocation without a readable result file is invalid and must be reported as
an error rather than as completed.

`wait` on a failed invocation returns:

```json
{
  "handle": "abc123",
  "state": "failed",
  "error": "Provider authentication failed"
}
```

`wait` on a cancelled invocation, or a repeated successful `cancel`, returns:

```json
{
  "handle": "abc123",
  "state": "cancelled"
}
```

## Waiting and idempotency

`wait` is the agent synchronization operation. Callers never poll: it blocks
until the invocation is terminal. Repeating it after termination immediately returns
the same state and, for a completed invocation, the same `result_path` until a
successful `continue` starts the next invocation.

`cancel` is idempotent only after successful cancellation. Repeating it for a
cancelled invocation succeeds. It cannot change a completed or failed
invocation into `cancelled`.

`start` and `continue` must durably reserve their work before contacting the
provider. A new `start` invocation cannot become eligible to contact the
provider until its complete handle response has been written. If a `continue`
response is interrupted, the caller uses the already-known handle with `wait`
instead of repeating `continue`.

## Public surface

There is no public fanout resource. Parallel work consists of independent
conversations, each with its own handle, state, result, cancellation, and
resumability.

There is no separate `read` command: `wait` returns the terminal state and the
completed result path. There is no agent-facing `status` command: agents wait
for state changes instead of polling.

The public Agent Dispatch lifecycle contains only `start`, `wait`, `continue`,
and `cancel`; `options` is read-only discovery. Inspection, history, listing,
deletion, and internal recovery state are not part of the agent interface.

# Agent Dispatch

Agent Dispatch runs headless provider conversations. It has one read-only
discovery operation plus one opaque conversation handle, four lifecycle
operations, and four states.

It has two surfaces over one backend. The MCP tools are the canonical
agent-facing path; the CLI is the human and scripting path. Both use the same
handles, states, result files, and cancellation semantics.

## MCP tools

Agent Layer projects a built-in MCP server, `agent-layer`, into the generated
configuration of every enabled Codex, Claude, Antigravity, VS Code, Copilot
CLI, and Grok caller. It is derived state, not a `[[mcp.servers]]` entry, and its
reserved ID cannot be taken by a user-defined server. It exposes five tools:

Agent-facing tool and parameter descriptions are maintained in
`internal/agentdispatch/mcp_tool_descriptions.toml` and embedded at build time.

| Tool | Purpose |
| --- | --- |
| `dispatch_options` | List dispatchable providers and their allowed overrides |
| `dispatch_start` | Start a conversation and return its handle immediately |
| `dispatch_wait` | Block for the configured wait, then report state |
| `dispatch_continue` | Start the next invocation in a terminal conversation |
| `dispatch_cancel` | Terminate a running conversation (destructive) |

`dispatch_start` accepts `agent`, optional `model`, `reasoning_effort`, `role`,
and `skill`, and exactly one of `prompt` or `prompt_file`. `dispatch_continue`
accepts `handle` and exactly one prompt source. `dispatch_wait` and
`dispatch_cancel` accept `handle`. Results carry the same `handle`, `state`,
`result_path`, and `error` fields as the CLI. A wait that returns `running`
also includes recorded `last_activity_at` and `last_output_at` timestamps when
available.

Successful results are returned as `structuredContent`; the SDK also emits the
serialized text fallback required for compatibility with older clients. The
tools omit optional output schemas to keep their always-loaded definitions
small; their descriptions state the fields callers need.

### Timeouts

Two optional settings in `.agent-layer/config.toml` control MCP timing:

```toml
[dispatch]
mcp_wait_timeout_minutes = 30
mcp_tool_timeout_minutes = 40
```

`mcp_wait_timeout_minutes` bounds one `dispatch_wait` call: a healthy wait
blocks that long and then returns `running`. `mcp_tool_timeout_minutes` is a
hard server-side bound applied to every Agent Dispatch tool call, so a wedged
handler always releases the caller. Both are optional positive integers; when
omitted they resolve to 30 and 40. The tool timeout must be greater than the
wait timeout, and an invalid relationship fails configuration validation.

Codex and Grok also receive the hard bound natively as `tool_timeout_sec`.
Claude Code documents only a client-wide `MCP_TOOL_TIMEOUT`, which Agent Layer
does not change because that would affect every unrelated MCP server;
Antigravity documents no per-server timeout key. For those clients the
server-side guard is the recovery bound.

### Cancelling a request is not cancelling a dispatch

Abandoning a `dispatch_wait` request — a client-side timeout, a disconnect, or
a cancelled tool call — stops only that wait. Provider work remains active and
the same handle can be waited on again.

Only `dispatch_cancel` (or `al dispatch cancel`) terminates provider work.
`dispatch_start`, `dispatch_continue`, and `dispatch_cancel` are annotated
destructive because dispatched agents can modify their environment; cancellation
is never inferred from elapsed time, silence, or a `running` result.

### Transport risk

An MCP `dispatch_start` or `dispatch_continue` is an RPC acknowledgement rather
than a direct write to the caller's terminal. If the transport disconnects
after the backend has started but before the client observes the response,
provider work remains active while the caller never learns its handle. The
evidence remains under `.agent-layer/tmp/runs/`.

## Commands

```text
al dispatch options

al dispatch start --agent <agent> [--model <model>] \
  [--reasoning-effort <effort>] [--role <role>] [--skill <skill>] \
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
its own default. `--role` is optional caller-defined workflow evidence retained
on the run record; it does not change provider selection or prompt text.
`start` returns immediately after durably creating the
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
| `wait` | Waits for the bounded interval, then returns `running` | Returns `result_path` | Returns the failure | Returns `cancelled` |
| `continue` | Errors | Starts the next invocation | Starts the next invocation | Starts the next invocation |
| `cancel` | Cancels the invocation | Errors: already completed | Errors: already failed | Returns `cancelled` successfully |

`failed` means the invocation could not complete, for example because of a
provider, authentication, network, process, or response error. `cancelled`
means a caller intentionally stopped it. Neither state invalidates the
conversation, so either may be continued.

Only one invocation may run for a conversation at a time. Concurrent
`continue` calls cannot start duplicate work: one may succeed and the others
must fail without contacting the provider.

When `wait` returns `running`, the provider invocation is unchanged. Call
`wait` again with the same handle until it returns a terminal state.

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

`wait` returns a `running` object when its bounded interval expires
before the invocation reaches a terminal state. The CLI waits eight minutes;
`dispatch_wait` waits `dispatch.mcp_wait_timeout_minutes` (30 by default).
When available, `last_activity_at` is the UTC timestamp of provider startup or
the most recent normalized stream event, and `last_output_at` is the UTC
timestamp of the most recent answer event. These are observations, not a health
check: absence of new events does not prove a provider has stopped working.

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

`wait` is the agent synchronization operation. It blocks for its bounded
interval — eight minutes on the CLI, `dispatch.mcp_wait_timeout_minutes` for
`dispatch_wait` — then returns `running` if the invocation remains active.
Repeating it after termination immediately returns the same state and, for a
completed invocation, the same `result_path` until a successful `continue`
starts the next invocation.

Continuation requires the recorded provider conversation ID. If a failed or
cancelled invocation may have reached the provider but no ID was captured,
`continue` fails rather than silently starting a fresh conversation. Inspect
the previous run before explicitly choosing `start`. Only a proven pre-start
failure permits a fresh retry through `continue`. Antigravity's `init` event
persists its conversation ID before the terminal result, preserving identity
when a later handoff fails.

Dispatch requires a structured terminal result, a final answer, a consistent
conversation ID, successful provider exit, and proof that its process group
has stopped. After terminal evidence, stdout closure, or provider exit, shutdown
and output draining have a five-second bound; expiry fails the invocation and
terminates its owned process group. Surviving descendants are also terminated
after normal leader exit. Termination signals a group only while the captured
leader identity still matches or, after that leader is a zombie, descendants
still reserve the ID; a reused leader is never signalled. The worker reaps the
leader with a non-blocking wait rather than a background `cmd.Wait` goroutine,
so unproven termination cannot leak a blocked waiter. Output is drained
independently of process waiting so inherited pipes cannot hide that exit.
Failure to prove termination retains the active claim rather than permitting
overlapping work. The group-termination grace and proof windows are separate
from the process/I/O shutdown deadline.

If the worker and leader have died but descendants survive, automatic recovery
retains the claim and reports the group ID. Inspect the saved run evidence and
the surviving processes to establish ownership before manually stopping any of
them; never signal a group based solely on its numeric ID. Once the owned group
is gone, another `wait` reconciles the abandoned run. A verified different start
identity for a replacement group leader proves ID reuse, allowing recovery
without signalling the unrelated group. This relies on the
[POSIX process-ID reuse guarantee](https://pubs.opengroup.org/onlinepubs/009696699/basedefs/xbd_chap04.html#tag_04_12).

This shutdown bound is not an idle-work timeout. In particular, Antigravity may
save a planner response while withholding its structured terminal result until
managed background tasks end. A saved transcript is not a completed dispatch.
The `ship-pr` skill stops its own watcher through its managed task before handing
control back for authorization or a blocker, and restarts it when monitoring
resumes. Existing invocations retain their already-loaded instructions; updating
the skill does not repair them in place.

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

There is no separate `read` command: `wait` returns the current or terminal
state and the completed result path. There is no agent-facing `status` command:
agents use the bounded `wait` operation for state changes.

The public Agent Dispatch lifecycle contains only `start`, `wait`, `continue`,
and `cancel`; `options` is read-only discovery. Inspection, history, listing,
deletion, and internal recovery state are not part of the agent interface.

The MCP surface exposes `options`, `start`, `wait`, `continue`, and `cancel`.
`al dispatch mcp-server`, which serves those tools over stdio, is a hidden entry
point for generated client configuration, not a public command.

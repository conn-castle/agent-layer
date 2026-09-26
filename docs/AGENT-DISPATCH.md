# Agent Dispatch

Agent Dispatch runs headless provider conversations. Each conversation has an
opaque handle; each invocation has an immutable UUID. Discovery, lifecycle,
inspection, and evidence retrieval use the same backend.

It has two surfaces over one backend. The MCP tools are the canonical
agent-facing path; the CLI is the human and scripting path. Both use the same
handles, states, result files, and cancellation semantics.

## MCP tools

Agent Layer projects a built-in MCP server, `agent-layer`, into the generated
configuration of every enabled Codex, Claude, Antigravity, VS Code, Copilot
CLI, Grok, and Muse caller. It is derived state, not a `[[mcp.servers]]` entry, and its
reserved ID cannot be taken by a user-defined server. It exposes seven tools:

Projection does not prove that a client exposes MCP tools at runtime. In
particular, the current Antigravity probe baseline accepts the generated config
but does not register its servers; use `al probe agy` before treating
Antigravity as a caller. Antigravity remains available as a dispatch target.

Muse reads the shared project `.mcp.json`, with client-specific exclusions.
Its project schema does not support required-server startup. Command grants
use workspace-scoped native rules; MCP grants use the project PermissionRequest
hook, including Agent Dispatch, without saving global MCP permissions.

Codex and Muse filter MCP subprocess environments. Their generated built-in
server definitions explicitly forward dispatch depth (`AL_DISPATCH_ACTIVE`),
parent-run metadata (`AL_RUN_ID`, `AL_RUN_DIR`), and development executable
selection. These are launch-time values, so a later session does not inherit the
session that ran `al sync`. Muse uses depth zero when the marker is unset.

Agent-facing tool and parameter descriptions are maintained in
`internal/agentdispatch/mcp_tool_descriptions.toml` and embedded at build time.

| Tool | Purpose |
| --- | --- |
| `dispatch_options` | List dispatchable providers and their allowed overrides |
| `dispatch_start` | Start a conversation and return its handle and invocation_id |
| `dispatch_wait` | Block for the configured wait, then report state |
| `dispatch_continue` | Start the next invocation in a terminal conversation |
| `dispatch_cancel` | Request that provider work stop (destructive) |
| `dispatch_inspect` | Prompt state and termination-confirmation observation |
| `dispatch_output` | Bounded final-answer or event text retrieval |

`dispatch_start` accepts `agent`, optional `model`, `reasoning_effort`, `role`,
and `skill`, and exactly one of `prompt` or `prompt_file`. `dispatch_continue`
accepts `handle` and exactly one prompt source. `dispatch_wait`,
`dispatch_cancel`, `dispatch_inspect`, and `dispatch_output` accept exactly one
of `handle` or `invocation_id`. An explicit invocation ID never follows a later
continuation. Results carry `handle`, `invocation_id`, `state`, `result_path`,
`error`, and `termination_confirmed`. A wait that returns `running` also
includes recorded `last_activity_at` and `last_output_at` timestamps when
available. `dispatch_inspect` does not return or persist caller prompt text.

Successful results are returned as `structuredContent`; the SDK also emits the
serialized text fallback required for compatibility with older clients. The
tools omit optional output schemas to keep their always-loaded definitions
small; their descriptions state the fields callers need.

### Timeouts and retention

Optional settings in `.agent-layer/config.toml` control MCP timing and how long
inactive conversations are kept:

```toml
[dispatch]
session_retention_days = 30
reservation_expiry_days = 7
mcp_wait_timeout_minutes = 30
mcp_tool_timeout_minutes = 40
```

`mcp_wait_timeout_minutes` bounds one `dispatch_wait` call: a healthy wait
blocks at most that long and then returns the current state and `condition_met`.
`mcp_tool_timeout_minutes` is a
hard server-side bound applied to every Agent Dispatch tool call, so a wedged
handler always releases the caller. Both are optional positive integers; when
omitted they resolve to 30 and 40. The tool timeout must be greater than the
wait timeout, and an invalid relationship fails configuration validation.
`session_retention_days` bounds inactive conversation mappings and confirmed
terminal evidence (default 30). Unconfirmed execution evidence is never expired.
`reservation_expiry_days` bounds how long an unstarted reservation stays
startable (default 7); see [Reservations](#reservations).
Older binaries that strictly
decode run records will fail to read records written by this version.

Codex, Grok, and Muse also receive the hard bound natively as `tool_timeout_sec`.
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
provider work remains active while the caller never learns its handle.
`dispatch_inspect` and `dispatch_output` can read that invocation by ID when
the ID is known; evidence remains under `.agent-layer/tmp/runs/`.

## Commands

```text
al dispatch options

al dispatch reserve

al dispatch start --agent <agent> [--model <model>] \
  [--reasoning-effort <effort>] [--role <role>] [--skill <skill>] \
  [--reservation <handle>] \
  (--prompt <text> | --prompt-file <path>)

al dispatch wait <handle-or-invocation-id> [--condition terminal|termination_confirmed]

al dispatch inspect <handle-or-invocation-id>

al dispatch output <handle-or-invocation-id> --artifact final_answer|events

al dispatch continue <handle> \
  (--prompt <text> | --prompt-file <path>)

al dispatch cancel <handle-or-invocation-id>
```

`options` returns the known dispatch agents, their current availability, configured
defaults, and supported model and reasoning-effort overrides.

Model suggestions are discovered from the installed Claude, Codex, Grok,
Antigravity, Muse, and Copilot CLI harnesses, concurrently and without sync or an
inference prompt.
Discovery uses the same project environment and provider configuration helpers
as launching an agent. Each lookup has a ten-second timeout. Model fields report
`source: "harness"` on success; when discovery fails, they report
`source: "unavailable"`, `discovery_error`, and an empty suggestions list.
Skipped lookups report `source: "not_requested"`. No model catalog or fallback
is shipped. The `catalog` source is reserved for non-model option metadata.
Suggestions are not an exhaustive account-access guarantee: custom model IDs
and aliases remain accepted. Starting or continuing a dispatch does not repeat
model discovery. `AL_NO_NETWORK` disables live model discovery.

Wizard starts concurrent discovery before its first configuration screen for
all harnesses with a model-discovery adapter, and waits for each result only
when its model picker needs it. Results are reused through back navigation.
A discovery error is displayed before the picker, which still allows the client
default or an explicit custom model without supplying model suggestions.
Scripted model answers are explicit inputs and do not trigger discovery. Copilot
CLI uses its headless SDK protocol to list models without creating a session.
Doctor checks only enabled
harnesses with configured model overrides and reports discovery
failures or configured models absent from their lists as warnings. Neither
operation syncs as part of model discovery. Discovery may create the normal
repo-local `.agy` and `.grok-config`
directories when absent; it does not sync
configuration or create dispatch runs.

Each explicit dispatch-options request obtains fresh results; no persistent
model cache is used. Provider-version queries also run concurrently. Launch,
sync, and dispatch start/continue do not run model queries just to pass through
an explicit configuration value or use the harness default.

`start` requires an agent and exactly one prompt source. Model and reasoning
effort are optional overrides. When omitted, Agent Layer uses its configured
value; when that is also empty, it omits the provider flag so the provider uses
its own default. `--role` is optional caller-defined workflow evidence retained
on the run record; it does not change provider selection or prompt text.
`start` returns immediately after durably creating the
conversation and starting its first invocation.

`reserve` and `--reservation` are described in [Reservations](#reservations).
Without `--reservation`, `start` is unchanged.

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

An invocation created by `reserve` starts in `reserved` and moves to
`running` when `start --reservation` launches it, or to `cancelled` when it is
cancelled or expires first. A reservation retired that way never ran, so
`continue` rejects it.

Terminal states are immutable. Continuing a terminal conversation creates a
new current invocation in `running`; it does not change the previous
invocation.

| Command | `running` | `completed` | `failed` | `cancelled` |
| --- | --- | --- | --- | --- |
| `wait` | Waits for the bounded interval, then returns `running` | Returns `result_path` | Returns the failure | Returns `cancelled` |
| `continue` | Errors | Starts the next invocation | Starts the next invocation | Starts the next invocation |
| `cancel` | Requests cancellation | Errors: already completed | Retries stopping unconfirmed execution, preserving `failed` | Retries an unconfirmed stop; confirmed cancellation succeeds immediately |

`failed` means the invocation could not complete, for example because of a
provider, authentication, network, process, or response error. `cancelled`
means a caller requested cancellation. Neither state proves execution has
stopped. Continuation requires termination confirmation and sufficient provider
conversation or pre-start recovery evidence.

Muse dispatch remains available in every approvals mode. Outside `yolo`, it
retains native approvals and disables the approval judge. Sync projects the
selected command and MCP grants; unmatched actions retain native prompting. A read-only
`muse serve` observer checks `approval/listPending`; pending tool approval
fails the invocation and triggers provider termination. Ordinary tool progress
does not imply a blocked run. Muse receives `--user-input-auto-resolve`; native
Muse cancels user-input requests and continues with that tool result. The observer
does not race native auto-resolution.

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
  "invocation_id": "11111111-1111-4111-8111-111111111111",
  "state": "running",
  "termination_confirmed": false
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
`dispatch_wait`. Its default condition is a terminal outcome. Select
`condition="termination_confirmed"` in MCP or `--condition termination_confirmed`
on the CLI to wait for stop confirmation, including after cancellation. A timeout
returns the current state with `condition_met=false`; it never establishes
termination. A handle is resolved once per wait; an invocation UUID always
addresses that invocation, even after a continuation.

`termination_confirmed` becomes true only after submission is fenced (or legacy
submitters are proven gone) and the owned provider process and group have stopped.
The timestamp and proof are persisted. A crash between provider start and identity
publication remains explicitly uncertain. Confirmation says nothing about reverting
edits or finishing external jobs the provider started.

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

`cancel` succeeds immediately for confirmed cancellation. For unconfirmed
cancelled or failed invocations it retries termination when process ownership
can be verified. It preserves an existing failed outcome; detailed proof and
attempt errors remain private run evidence.

`start` and `continue` must durably reserve their work before contacting the
provider. A new `start` invocation cannot become eligible to contact the
provider until its complete handle response has been written. If a `continue`
response is interrupted, the caller uses the already-known handle with `wait`
instead of repeating `continue`.

## Reservations

Reservations let callers allocate a conversation name before launching. A
repeated start is rejected, so an at-least-once workflow step cannot launch
the same retained reservation twice. They are CLI only: no MCP tool exposes
them, and agents using MCP should use `dispatch_start`.

```text
al dispatch reserve
al dispatch start --reservation <handle> --agent <agent> ... (--prompt <text> | --prompt-file <path>)
```

`reserve` durably creates an invocation in the `reserved` state, with an Agent
Layer generated handle and invocation ID, and launches nothing. It returns:

```json
{
  "handle": "small-blue-relay",
  "invocation_id": "11111111-1111-4111-8111-111111111111",
  "state": "reserved",
  "termination_confirmed": false,
  "reservation_expires_at": "2026-10-02T12:00:00Z"
}
```

The caller records the `handle`, then starts it with
`start --reservation <handle>`, passing the normal launch arguments. Callers
never choose identifiers: a reservation selector that Agent Layer did not
return fails without launching. The `invocation_id` remains available for
tracking an exact invocation; it is not accepted by `start --reservation`. A
repeated start recognizes the original reserved invocation even after the
conversation has been continued. After retention removes the reservation, its
three-word handle can be reused. A retry of that old handle could then start a
different reservation, so callers must stop retrying before retention ends.

The first start that reaches the reservation launches it through the normal
intent-before-start protocol and returns the normal start result. Concurrent
starts are serialized by the invocation's record lock, so at most one launches.
Every later start fails with exit 82 and guidance to inspect the invocation
and continue only when available; it launches nothing regardless of the
launch arguments. A start interrupted
mid-launch is resolved by the normal launch recovery (for example `failed`
with unknown provider acceptance) and is never relaunched. This guarantees
at-most-once launch while the reservation is retained, not eventual execution:
a crash after claiming can leave a failed invocation that never contacted the
provider. In that case, inspect the failed invocation before starting a new
conversation; continuation may be unavailable.

`inspect`, `wait`, `output`, and `cancel` accept a reservation's handle or
invocation ID. `inspect` reports `reserved` and `reservation_expires_at`.
`wait` on an unstarted reservation waits for the bounded interval like any
other nonterminal invocation, but its timeout result reports `reserved` rather
than `running`, with `condition_met: false`. `cancel` retires an unstarted reservation as `cancelled` with
confirmed termination, without launching. `continue` rejects a reservation
that never started.

A reservation that is not started within `dispatch.reservation_expiry_days`
(default 7) is retired as `cancelled` with the error `reservation expired
before it started`. Retired reservations never launch and are removed by
normal `session_retention_days` retention, counted from retirement for both the
handle and the invocation. Retention also applies to a started reservation: once
its invocation ended longer ago than the retention window, even while later
continuations keep the conversation, a repeat start fails with exit 80.

`start --reservation` fails with these exit codes. None of them launches
anything:

| Exit code | Meaning |
| --- | --- |
| 80 | Not found: the selector names no reservation, for example a typo, an invented or empty value, an invocation ID, a non-reservation handle, or a reservation already removed by retention. |
| 81 | Expired: the reservation expired before it started. It never launched and never will; reserve again. |
| 82 | Already started: this reservation has claimed its one start. Wait for it to finish, then continue if available. |
| 83 | Cancelled: the reservation was cancelled before it started. It never launched and never will. |

Other failures keep the ordinary dispatch exit codes. A start that fails
before it claims the reservation, for example because the agent is disabled
or unavailable, leaves the reservation startable.

## Public surface

There is no public fanout resource. Parallel work consists of independent
conversations, each with its own handle, state, result, cancellation, and
resumability.

`inspect` returns promptly with state, activity timestamps, and termination
confirmation. Process identities and proof details remain private. `output`
retrieves bounded UTF-8 text for `final_answer` or `events`; events can contain
partial output from failed or cancelled runs. Reads return at most 65,536 bytes
and set `truncated` when more captured text exists. Missing, unreadable, invalid,
or unavailable output fails explicitly.

The MCP surface exposes `options`, `start`, `wait`, `continue`, `cancel`, `inspect`,
and `output` with the `dispatch_` prefix. `reserve` and `start --reservation`
are CLI only.
`al dispatch mcp-server`, which serves those tools over stdio, is a hidden entry
point for generated client configuration, not a public command.

# Agent Dispatch

Agent Dispatch runs one focused, headless provider turn for an Agent
Layer-managed project. It is separate from **version dispatch**, which chooses
the repo-pinned `al` binary.

## Commands

Start a fresh conversation:

```bash
al dispatch --agent codex "Review this plan."
al dispatch --agent claude --skill review-plan "Review this plan."
al dispatch --agent antigravity "Check this design."
```

Continue only an explicitly named conversation:

```bash
al dispatch resume tiny-round-capacitor "Review the revision."
```

Inspect Agent Layer-owned evidence or manage a mapping:

```bash
al dispatch inspect tiny-round-capacitor
al dispatch inspect tiny-round-capacitor --json
al dispatch list
al dispatch delete tiny-round-capacitor
al dispatch options --json
```

Ordinary `al dispatch` calls are always fresh. A target, role, prompt, artifact
path, or previous output never implies conversation reuse. `resume` resolves
the provider exclusively from the named durable mapping.

## Output and completion

After atomically reserving a friendly name, Dispatch writes one compact
identity line to standard error, such as:

```text
[tiny-round-capacitor] codex · fresh
```

Standard output receives only the final assistant answer and only after the
provider has supplied its required semantic completion evidence. Provider tool
calls, raw events, reasoning, token usage, command output, diagnostics, and
partial failed answers are retained privately rather than forwarded.

Claude and Codex require a provider session identity, final answer, and
successful terminal event. Antigravity uses its documented print-process exit
and an isolated `--log-file` extractor. If an Antigravity answer succeeds but
the exact ID is not recognized, the answer remains successful and standard
error reports `not resumable` with the installed `agy` version and diagnostic
path.

Failures are nonzero and publish no partial answer. Dispatch owns the target
process group and forwards `SIGINT` and `SIGTERM` to that group.

## State, isolation, and inspection

Durable mappings use one atomic JSON file per friendly name:

```text
.agent-layer/state/dispatch/tiny-round-capacitor.json
```

The mapping contains only the friendly name, provider, exact provider session
ID, and timestamps. Provider transcripts remain provider-owned. Each attempt
also has a mode-0700 private record under `.agent-layer/tmp/runs/<uuid>/` with
bounded stdout, stderr, structured-event, final-answer, and (when needed)
provider-log capture.

Inactive name mappings are retained for 30 days after `last_used_at` and are
pruned opportunistically by fresh dispatch, resume, and list operations.
Pruning removes only the Agent Layer mapping: provider conversations and run
evidence remain untouched. Active and corrupt mappings are never pruned.
Upgrades preserve `.agent-layer/state/`; retention belongs to Agent Dispatch,
not the installer.

`inspect` is read-only. `running` means only that the owned process is alive;
silence, elapsed time, and missing output are not health evidence. Workflows
should wait for the process’s terminal notification, not poll inspection.

`delete` releases only the Agent Layer mapping after the associated run is no
longer active. It never deletes a provider conversation or transcript.

## Provider compatibility

Initial support is exact and per capability:

| Provider | Supported version | Fresh | Resume |
| --- | --- | --- | --- |
| Claude Code | 2.1.207 | Yes | `--resume <id>` |
| Codex CLI | 0.144.1 | Yes | `codex exec resume --json <id> -` |
| Antigravity | 1.1.1 | Yes | `agy --conversation <id>` when its isolated log yields a validated ID |

An unmatched version fails before provider launch with the supported version
and next action. `al dispatch options --json` reports installed status, exact
detected version, and `fresh`, `resume`, and `inspect` capabilities separately;
it has no `dispatch_capable` or streaming promise.

## Configuration and launch parity

Dispatch uses the same provider configuration construction as ordinary
launchers: Claude’s opt-in repository-local `CLAUDE_CONFIG_DIR`, Codex’s
opt-in `CODEX_HOME`, Antigravity’s repository-local `--gemini_dir`, approvals,
models, effort, skills, and dispatch depth. Projection preparation is serialized
by the project sync lock and finishes before name reservation or provider
launch, so independent provider processes can overlap safely.

Prompts from standard input are capped at 10 MiB and rejected rather than
truncated. Antigravity passes prompts through `agy --print` and therefore has a
lower 100 KiB limit; oversized Antigravity prompts are rejected, never
truncated. The final answer is capped at 16 MiB and all captured provider data
per attempt at 64 MiB; a limit or capture failure terminates the owned process
group and emits no answer.

## Retry and recovery

Dispatch makes at most one automatic retry, and only when command start is
proven not to have succeeded. Once a provider process starts, task acceptance
is ambiguous unless explicit provider evidence says otherwise: Dispatch marks
the attempt interrupted or failed and requires an explicit later `resume` when
a durable ID exists. It never launches an out-of-band replacement.

## Exit status

- `0` — provider completed successfully and the final answer was replayed
- `64` — usage or resolution failure
- `65` — configuration or Agent Layer state failure
- `69` — unavailable binary or unsupported provider version
- `70` — provider, capture, completion, or process-supervision failure
- `75` — nesting blocked by `dispatch.max_depth` or invalid dispatch depth
- `130` / `143` — interrupted by `SIGINT` / `SIGTERM`

## Workflow use

Independent orchestration starts separate fresh commands before waiting. The
review-plan workflow preserves its fixed matrix: three external reviewer
dispatches, each starting and synthesizing three built-in perspectives. A
reviewer may be replaced only after its whole descendant tree is terminal and
the failure is proven safe to retry; an ambiguous lifecycle or missing report
is a blocker, not evidence for a fresh replacement.

# Agent Dispatch

Agent Dispatch runs focused headless provider turns for an Agent Layer-managed
project. It is command-line-interface-only and separate from version dispatch,
which chooses the repository-pinned `al` binary.

## Synchronous commands

Fresh and resumed turns block until terminal publication:

```bash
al dispatch --agent codex "Review this plan."
al dispatch --agent claude --skill review-uncommitted-code "Review the current working tree."
al dispatch resume tiny-round-capacitor "Review the revision."
```

One narrow fanout sends one shared prompt, optionally with one skill, to two or
more targets, runs them concurrently, waits for every child, and prints one JSON
manifest:

```bash
al dispatch fanout \
  --target 'agent=codex,reasoning=high' \
  --target 'agent=claude,model=opus' \
  --target 'agent=antigravity' < shared-prompt.md
```

Each repeated `--target` is self-contained:
`agent=<provider>[,model=<model>][,reasoning=<effort>]`. Unknown or duplicate
keys and unsupported overrides fail before launch. Omitted overrides use the
same configured defaults as ordinary dispatch. Fanout cannot express
per-target prompts or skills; different prompts use independent ordinary
commands.

Recovery and diagnostics:

```bash
al dispatch inspect tiny-round-capacitor --json
al dispatch history tiny-round-capacitor --json
al dispatch cancel <name-or-run-or-fanout-id>
al dispatch list
al dispatch delete tiny-round-capacitor
al dispatch options --json
```

Ordinary calls are always fresh. Resume resolves the provider conversation
exclusively from the named durable mapping.

## Internal coordinator and completion

Public fresh, resume, and fanout commands remain synchronous over an internal
concurrency-safe coordinator. The coordinator reserves immutable run state
before launch, owns provider process groups, and publishes terminal state before
the command returns. It is not a detached public job service; there are no
public `start`, `wait`, `wait-all`, or `result` commands.

A chat host may yield a long-running terminal command and require one host-level
wait. That wake-up behavior is outside the CLI contract. Workflows must not
model-poll `inspect` merely to wait.

After atomically reserving a friendly name, Dispatch writes a compact identity
line to standard error. Successful standard output contains exactly one
terminal answer. Raw provider streams, progress, tool/child activity,
diagnostics, prior answer candidates, and failed partial output remain private.

Provider reduction is explicit:

- Codex retains the latest assistant-message candidate and publishes it only
  after authoritative turn completion.
- Claude records streaming deltas as progress and uses the terminal result as
  the answer.
- Antigravity records successful print output as the candidate and publishes it
  at process completion.

Each fanout child has its own friendly name, immutable run record, and result
path. Early child results are persisted immediately. Failure does not cancel
unrelated children: fanout waits until all are terminal, emits complete
per-child evidence, then exits nonzero. Cancelling a fanout affects only its
active children and preserves completed evidence.

## Canonical state and concurrency

Friendly mappings under `.agent-layer/state/dispatch/` are lookup keys, not
history. Immutable run records under `.agent-layer/tmp/runs/<uuid>/` are the
canonical turn history and link a turn to its name, predecessor, provider
conversation, parent, and fanout group when applicable. `history` derives its
ordered output from these records rather than a second mutable history file.

Run records expose:

- execution state: `pending`, `running`, `completed`, `failed`,
  `cancelled`, or `interrupted`
- recovery state: `retry_safe`, `resume_required`,
  `acceptance_unknown`, or `not_resumable`
- factual last semantic activity time and kind
- process identifier, process group, and operating-system start identity
- exact private result and diagnostic paths

Dispatch never derives a `stalled` state from elapsed inactivity. Inspection
reports facts and reconciles a definitively dead owned wrapper to
`interrupted`; it does not infer provider health or descendant terminality
that the provider cannot prove.

A per-conversation active claim spans the entire provider execution. A second
simultaneous resume fails nonzero with the active run handle. It launches no
provider, queues no prompt, and does not mutate provider conversation state.
Unrelated conversations and fresh calls remain concurrent; no global lock is
held while a provider runs.

`cancel` signals only the exact recorded live process group after verifying
its process-start identity. A fanout cancellation iterates only nonterminal
children.

## Retention

Agent Layer applies a fixed 30-day window to name mappings and eligible terminal
raw-run evidence using canonical record timestamps, not filesystem modification
time. Opportunistic pruning never removes active/nonterminal work, corrupt
evidence whose age or state cannot be established, or the current run
referenced by a retained mapping. When an older predecessor was pruned,
`history` reports a retention boundary instead of claiming complete history.
There is no retention configuration.

Provider-owned conversations and transcripts are never deleted. `delete`
removes only an inactive Agent Layer name mapping.

## Provider compatibility and limits

`al dispatch options --json` is authoritative for installed versions and
fresh/resume/inspect capability. A version older than the tested version, or
one that cannot be read or parsed, fails before launch. A newer version stays
dispatchable and emits one compatibility warning (naming the installed and
tested versions) on stderr and in the options report.

Prompts from standard input are capped at 10 MiB. Antigravity currently has a
100 KiB prompt cap because its print mode accepts only an argument. Terminal
answers are capped at 16 MiB and retained provider data at 64 MiB. Inputs and
outputs are rejected rather than truncated.

Dispatch performs at most one automatic retry and only for a proven pre-start
failure. After a provider might have accepted work it preserves known
conversation identity and reports explicit recovery state; it never starts an
out-of-band replacement.

## Exit codes

Dispatch commands exit with a stable, category-scoped code:

- `0` — success
- `64` — usage or name/run resolution failure
- `65` — configuration or dispatch-state failure
- `69` — target unavailable (disabled, missing binary, or busy conversation)
- `70` — target or adapter failure during execution
- `75` — nested-call limit failure
- `130` — terminated by SIGINT
- `143` — terminated by SIGTERM

## Workflow use

Built-in workflows use external dispatch only for bounded leaf judgment, such
as plan review, implementation, bounded repair, and semantic code review
through the `code_reviewer` role. Root orchestration owns transitions,
verification coordination, shipping, and merge authorization. Dispatch always
runs in the current resolved repository-root checkout; built-in workflows
create a linked Git worktree only when the user explicitly requests worktree
isolation. The global maximum dispatch depth remains three for intentional
custom workflows; built-in workflows are root-to-leaf.

Plan review uses one shared-prompt fanout to three equivalent external
reviewers. Each follows the leaf review asset directly and returns one
complete-plan report without launching another agent or workflow. Only primary
artifacts and mechanically verifiable facts cross independent stages; reviewer
conclusions do not.

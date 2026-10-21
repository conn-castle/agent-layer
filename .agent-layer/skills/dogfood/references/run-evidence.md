# Source campaign and reusable lessons

The source is the long conversation beginning October 5, 2026. Product dogfooding
ran October 9–12 and continued through v1.0.0 publication on October 12. It was
identified by the actual user messages in `.codex/history.jsonl`, then checked
against the conversation and discovery-worker JSONL files and retained reports.
Paths below are relative to this repository unless absolute. Logs and `/var/tmp`
artifacts are local evidence and may later disappear; they are not prerequisites
for using the skill. Do not copy their full contents into tracked documentation.

## Conversation JSONL

Primary log, approximately 135 MB at inspection:

`.codex/sessions/2026/10/05/rollout-2026-10-05T07-09-29-01a10bc1-2401-7a61-8e93-b00eee073d2d.jsonl`

Useful primary-log anchors (one-based lines at inspection):

| Line | Evidence |
| --- | --- |
| 14100 | Scope correction: exercise Agent Layer installations, upgrades, wizard/config, dispatch, and CLI behavior. |
| 19103 | Batch findings; at most three parallel dogfood/implementation runs; decide review per fix; skip routine plan review. |
| 20366 | Seven findings grouped into five coherent fix groups rather than one PR per observation. |
| 41340 | User questions stopping; subsequent concurrency/endurance/recovery batch finds five material bugs. |
| 43391 | User asks for another round because five bugs were found readily. |
| 44360 | Orchestration MCP server was still v0.22.0 while separate dogfood candidates were current. |
| 46348 | Final 21-workflow pass finds only help mismatches; PR #392 fixes them without new tests. |
| 47897 | User requires CI gaps to be fixed rather than relying on manual checks. |
| 49637 | Supported nonpublishing signed rehearsal becomes a repeatable release gate. |

Discovery-worker logs whose first-record `parent_thread_id` matches the primary
session:

| Worker | JSONL under `.codex/sessions/2026/10/11/` |
| --- | --- |
| Install/upgrade | `rollout-2026-10-11T17-28-12-01a12cdd-be3c-7e93-b315-d5b08ea76b20.jsonl` |
| Wizard/instructions | `rollout-2026-10-11T17-28-29-01a12cde-01eb-7930-aeb6-53e3b811be8c.jsonl` |
| Dispatch endurance | `rollout-2026-10-11T21-52-33-01a12dcf-c62c-78d0-9d02-4226c818c0fc.jsonl` |
| Concurrent operations | `rollout-2026-10-11T21-53-20-01a12dd0-7e1c-7141-895e-23bd3d16dff5.jsonl` |
| Upgrade recovery | `rollout-2026-10-11T21-52-56-01a12dd0-201c-7201-9f2d-86e906631d64.jsonl` |

Workers were reused for later rounds; their final messages describe the eventual
cleaner coverage, not necessarily their originally named lane.

## Corroborating reports

The retained `/var/tmp/al-v1-final-batch-opmyi3v_/` was inspected directly:

- `concurrency/report.md`: wizard/import lost update reproduced three times;
  sync wrote a pending projection after SIGINT during lock wait. FIFO/lock gates
  established the overlap; fixture/schema errors were recorded separately.
- `dispatch/report.md`: 17 regular native invocations passed, but late cancellation
  changed a record revision and prevented terminal publication. Successful native
  output did not establish a successful dispatch. Bounded retry did not reproduce
  the race, and the original failure remained recorded.
- `upgrade/report.md`: 12 lifecycle scenarios, including two findings: interactive
  upgrade continued after SIGINT and state-directory symlinks allowed external
  baseline writes/snapshot pruning. Recovery comparisons were corrected to the
  actual snapshot scope. An unexplained timeout was retained as unresolved.
- `integration-report.md` and `state.json`: separate implementation checkouts,
  exact integration provenance, review corrections, candidate-specific rechecks,
  PR #390 merge, and merged-main verification. An early corrected CLI candidate
  explicitly did not certify later edits.

The primary and child logs also name the later `/var/tmp/al-v1-dogfood-round2-5xozzmn2/`
and `/var/tmp/al-v1-final-pass-jmspe1pw/` reports; those directories were absent when
this skill was written. Their results are supported here by the JSONL messages and
the maintained `docs/release-evidence/v1.0.0-agent-dispatch.md`, not by a claimed
fresh read of missing files. That document records six ordinary-disk native calls,
CLI/MCP handoff and restart, durable termination, profile/provider limitations,
and the later zero-generation 21-workflow pass.

## Lessons retained without freezing historical choices

The campaign's Codex Luna/low, Claude Sonnet/low, and later Muse substitution were
explicit user choices. They usefully separated cheap compatibility probes from
deeper reviews; they are not permanent model defaults or certification of the
unavailable providers. The original merge/release approvals likewise do not carry
forward. Current code and `docs/RELEASE.md` supersede old commands and gates.

`SKILL.md` owns the current workflow and stopping criteria; this file records
their provenance.

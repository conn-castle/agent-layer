# Unresolved runtime and scratch incidents

Assessment date: 2026-10-14. Source baseline: `61bd288bbb21a9ab46857ce31711a54f65bcf9ca`. This records the evidence available to the known-issues PR batch. It does not close the three incidents or claim a production fix.

| Tracked incident | Established evidence | Missing evidence / next action |
| --- | --- | --- |
| manual-rollback-timeout-unclassified | A qualified reconstruction restored all 30 scoped entries in 0.598 seconds; the first 40-second timeout was not reproduced | On an actual recurrence capture owned PID/start identities, task/FD/wait state, syscalls and exception streams, plus exact fixture and executable identity, before attributing the stall |
| native-dispatch-shutdown-after-answer | Earlier deterministic persistence timing was fixed separately; later native calls passed. A mocked investigation showed kernel/storage stalls, but its minimal candidate still failed and was reverted | Use current sanitized lifecycle diagnostics from PR #412 on a native recurrence. Distinguish semantic completion, leader exit, output readers, persistence, workflow state and durable termination |
| shared-scratch-deletion-recovery | Earlier memory records describe partial recovery, while original binaries, repositories/Git metadata, strace and unknown local changes remained unrecovered | Locate a surviving backup or original recovery directory. Reconcile exact byte/hash evidence without overwriting live work. Unknown original content cannot be reconstructed from replacement binaries or historical snapshots |

## Rollback evidence reviewed

The existing report at `/var/tmp/al-v1-final-batch-opmyi3v_/rollback-diagnostic/report.md` was readable at assessment time. It explicitly labels the reproduction as qualified: exact pre-upgrade snapshot bytes/modes survived, but the complete original post-failure state, inherited environment and I/O/cache conditions did not. Lock acquisition was immediate and observed fsyncs in that successful run were short. These observations do not prove the original failure was a lock, harness or product fault.

The report records that the diagnostic observer stopped and its exact process identities disappeared. No further rollback was attempted against the original fixture during this assessment. Preserve that fixture and its evidence rather than replaying destructive recovery commands against it.

## Shutdown evidence reviewed

The readable report `/var/tmp/al-v1-final-dogfood-osw1igf8/dispatch-shutdown-test-investigation.md` states that no production fix is recommended on its evidence. Mocked repetitions captured missing-leader process probes and file persistence delayed in kernel I/O wait. The Linux ENOENT shortcut experiment still failed and was reverted. Do not adopt the saved candidate, raise the shutdown grace, weaken descendant containment or call failed invocations completed based on a saved answer.

PR #412 supplied sanitized runtime lifecycle timing and executing-build identity. A fresh failed invocation must be inspected using its exact invocation ID; an old conversation handle can point at a later continuation. Keep original stdout/stderr/events local if they include prompts or tool payloads; publish only the relevant sanitized observations. Later successful native calls and full CI runs do not classify an earlier native failure.

## Scratch recovery assessment

At assessment time, neither `/tmp/alhunt-recovery-d2dp71gh` nor `/tmp/alhunt-incident-recovery-881um2eg` existed. A filename search under `/tmp` and `/var/tmp` found no `RECOVERY.md` copy. This is a limited filesystem observation; it does not establish that no backup exists elsewhere or identify who removed any directory. Prior counts of recovered files are historical claims from the memory entry, not a recovery verification performed by this batch.

The user has been asked for any surviving archive or backup location. Until a source is available, do not mark original `al`, `al-old`, `al-cur`, `r1`–`r3` Git metadata, `strace.txt`, or unknown edits recovered. Canonical skills PR #23 adds recurrence prevention through fresh invocation-owned scratch and cleanup rules; prevention does not recover lost bytes.

## Recurrence capture contract

Create a unique owned diagnostic directory and record its exact path. Preserve a copy of the original failure evidence before retrying. Record source/build/executable identity, invocation or snapshot ID, exact command, timestamps and a safe environment allow-list. Sample only proven owned process identities; retain task wait/FD state and exception streams when present. Trace relevant syscalls without credential/prompt payloads. Document tracing overhead and reconstruction limits. Confirm observers stopped and owned processes terminated before handoff. Keep failure artifacts separate from successful controls; do not delete shared scratch or restore historical copies over active state.

These issues remain open pending recurrence evidence or surviving original data.

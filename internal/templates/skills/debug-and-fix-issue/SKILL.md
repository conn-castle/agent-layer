---
name: debug-and-fix-issue
description: >-
  Reproduce an unexplained bug, prove its root cause, capture a failing test or
  diagnostic blocker, then make a proportional repair and verify it.
---

# debug-and-fix-issue

Turn a testable symptom into a proven cause and verified root-cause repair, or
an explicit diagnostic blocker.

## Inputs and boundaries

Require a testable symptom. Fix mode also requires explicit, self-contained
`implementer` and `fixer` dispatch target specifications; the planned repair
path additionally requires exactly three `plan_reviewers` target specifications
and one `code_reviewer` target specification. Accept reproduction evidence,
suspect paths, a regression range, and diagnosis-only mode.

Before fix-mode side effects, show the user the exact role-to-target mapping and
every plan-reviewer target specification. Ask for any missing target; do not
infer roles or target specifications. Dispatch external roles through
`/agent-dispatch` and validate their evidence against the current tree.

Before editing, record the head and working-tree state. Isolate the repair with
path or hunk boundaries where work overlaps; stop only if it cannot be separated
without overwriting user changes.

Write `.agent-layer/tmp/debug-and-fix-issue.<run-id>.report.md`; a direct repair
may use the same prefix for its repair report. Do not stage, commit, discard, or
destructively rewrite changes.

## Workflow

1. Read COMMANDS.md and relevant context. Reproduce the symptom with the
   smallest credible command, recording expected and observed behavior and the
   material environment. If it does not reproduce, name the missing fact or
   runtime state.
2. Trace the failing path with discriminating experiments. Establish the
   defective condition, causal chain, and excluded alternatives from observed
   behavior, logs, code, or authoritative dependency contracts. Instrument
   unresolved alternatives rather than guessing.
3. Add or refine a behavioral test that fails for the proven defect. When an
   automated test is impossible, preserve equivalent diagnostic evidence and
   explain why. In diagnosis-only mode, update ISSUES.md when appropriate and
   stop here.
4. Use the direct path when the cause, desired behavior, boundary, and checks
   are clear: dispatch `implementer` with the diagnosis and failing test, then
   dispatch `fixer` with `/clean-and-fix-code` over the repair boundary. Use the
   planned path only for substantive architecture, behavior, migration, or risk
   changes: run `/plan-work` with `plan_reviewers`, then
   `/fully-implement-plan` with `implementer`, `code_reviewer`, and `fixer`.
   Unknown causes justify instrumentation, not speculative fixes.
5. Prove red-to-green behavior, run affected checks, and verify the original
   symptom. For material in-scope verification gaps on the direct path,
   redispatch `implementer` with those findings before final verification.

Reject retries, sleeps, inflated timeouts, broad catches, silenced errors,
ignored validation, or weakened assertions unless evidence establishes them as
the intended contract.

## Completion contract

Report `fixed`, `diagnosed-only`, or `blocked`, including the symptom, root
cause or missing fact, reproducer or diagnostic evidence, changes, verification,
work-isolation boundary, report path, and residual risk. State explicitly when
the issue remains unfixed.

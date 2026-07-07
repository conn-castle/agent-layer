---
name: fully-implement-plan
description: >-
  Fully execute an existing implementation plan through code implementation,
  cleanup review/fix rounds, contract verification, and all local checks. Use
  when Codex has plan, task, and context artifact paths and should finish the
  planned work end to end rather than only applying the code changes.
---

# fully-implement-plan

Parent orchestration skill for taking a reviewed plan from implementation to a
checked, verified working tree.

This skill owns the outer loop. Child skill returns are intermediate; continue
after each return until this workflow reaches a human checkpoint or the final
all-checks step passes.

## Required inputs

Fail before side effects unless all are present:

- plan artifact path
- task artifact path
- context artifact path

Do not discover or infer plan artifacts from `.agent-layer/tmp/`.

## Required artifacts

Use one shared `run-id = YYYYMMDD-HHMMSS-<short-rand>`:

- `.agent-layer/tmp/fully-implement-plan.<run-id>.state.md`
- `.agent-layer/tmp/fully-implement-plan.<run-id>.report.md`

Create both before writing. The state file tracks the current phase, artifact
paths, implementation attempts, cleanup rounds, verification attempts, and
check-lane result. The report is the final human-readable ledger.

## Workflow

```text
/fully-implement-plan
  /implement-plan
  while true:
    /clean-and-fix-code -> resolved_findings
    if no resolved_findings:
      break
    if every resolved finding is below High severity:
      break
  /verify-work
  if /verify-work is incomplete:
    go back to /implement-plan
  run-and-fix-checks
```

`run-and-fix-checks` means invoke `/repair-checks` with full/all-checks
verification. Include every documented required check, including tests. Do not
use a fast lane when a fuller lane exists.

## Continuation Rule

Delegation returns are intermediate, not terminal. After every child skill
return, continue to the next numbered phase in the same run. A child skill's
closing summary is not this skill's closeout.

The workflow exits only at:

- a listed human checkpoint
- a child workflow that halts on its own human checkpoint without applying
  changes
- the cleanup loop stop gate
- successful `/verify-work` followed by the final `/repair-checks` result

Do not stop after `/implement-plan`, `/clean-and-fix-code`, `/verify-work`, or
`/repair-checks` merely because that child skill produced a final handoff.
Record the handoff in the state file and keep going.

## Context Discipline

You are the orchestrator. Do not do child workflow work yourself. Preserve your
context to make strategic decisions, enforce gates, reconcile child outputs,
and continue the parent workflow after every return.

- Let `/implement-plan` own implementation against the supplied artifacts.
- Let `/clean-and-fix-code` own each cleanup review/fix pass.
- Let `/verify-work` own contract verification.
- Let `/repair-checks` own the full fail/fix/rerun check lane.
- Read only the artifacts, child reports, touched diffs, and repo files needed
  to make parent-level decisions.
- Do not re-review code manually, redo child synthesis, or independently fix
  child findings unless this workflow has returned to the child skill that owns
  that work.

## Phase 0: Preflight

1. Validate the plan, task, and context paths exist.
2. Run `git status --porcelain` and record the current staged, unstaged, and
   untracked state in the state file.
3. Read `COMMANDS.md` before any check or test command is selected.
4. Read the context artifact, then the plan and task artifacts.
5. Confirm all three artifacts describe the same objective and scope. If they
   conflict in a way that changes behavior or scope, stop and ask.

## Phase 1: Implement

Use `/implement-plan` with the supplied plan, task, and context paths.

For verification-repair attempts, call `/implement-plan` again with the same
artifact paths and the latest `/verify-work` report as evidence of remaining
contract gaps. Do not rewrite the plan unless a child skill reaches a human
checkpoint that requires plan changes.

Record each implementation report path under `## Implementation Attempts`.

## Phase 2: Clean And Fix

Run `/clean-and-fix-code` on the current uncommitted working tree.

For each cleanup round, record:

- cleanup report or handoff paths
- every reviewed finding with title, severity, confidence, location, verdict,
  and why-it-matters summary when the child report exposes that detail
- `resolved_findings` with title, severity, fix description, and files touched
- accepted, rejected, deferred, and already-resolved counts
- rejected and deferred findings with their round number and reason
- whether the round triggered another cleanup round

Repeat `/clean-and-fix-code` only when the previous run resolved at least one
Critical or High finding. Do this because a Critical or High fix can materially
change the reviewed surface area.

Stop the cleanup loop when:

- `resolved_findings` is empty, or
- every resolved finding is below High severity.

The Medium/Low-only stop is intentional. Do not run another cleanup round just
to chase diminishing returns after only lower-severity fixes were resolved.

Do not carry unresolved deferred findings into a clean result. If
`/clean-and-fix-code` reports a deferred finding that blocks planning, fixing,
or verification, stop and surface the checkpoint.

Accepted-finding fixer discipline stays required even though `/clean-and-fix-code`
does the child fixing:

- verify each accepted finding against the current repo state before relying on
  it
- group duplicate or tightly coupled findings into one bounded fix target
- keep unrelated findings separate when they can be fixed independently
- diagnose and fix the root cause
- include directly required test, doc, or memory updates
- run focused verification for the fix when the child workflow can do so
- audit the final diff against the accepted finding before counting it as
  resolved

## Phase 3: Verify Contract

Use `/verify-work` with the plan, task, and context paths. This verifies the
implemented working tree against the planned contract.

Verdict handling:

- `complete`: proceed to the check lane.
- `complete-with-follow-up`: proceed only when every follow-up is clearly
  outside the plan contract; otherwise treat it as `incomplete`.
- `incomplete`: record the findings, then return to Phase 1.

If the same incomplete contract item recurs after two implementation attempts,
stop and ask instead of guessing through another loop.

## Phase 4: Run And Fix Checks

Use `/repair-checks` in full/all-checks mode:

- prefer one documented all-checks command when it exists
- otherwise run every relevant documented format, lint, typecheck, test,
  coverage, build, docs, and pre-commit command needed for the repository's
  full local confidence lane
- include tests; do not silently downgrade to a fast lane
- let `/repair-checks` own its fail/fix/rerun loop

If `/repair-checks` reports failures caused by this work, keep fixing through
that skill until all checks pass or a real blocker remains. If it reports a
clearly unrelated or systemic failure, stop and surface the blocker instead of
widening this workflow silently.

## Human checkpoints

Ask when:

- required artifact paths are missing or conflicting
- the target is empty and no credible implementation or review scope exists
- implementation or verification would require an architectural decision,
  end-user-visible behavior change beyond the plan, destructive action, or
  another user-only decision
- an accepted cleanup finding requires an architectural decision,
  end-user-visible behavior change beyond the plan, or another user-only
  decision
- a finding cannot be verified with available code, tests, or docs
- a deferred finding blocks the cleanup or verification result
- the same unresolved verification item recurs after two implementation
  attempts
- the same unresolved cleanup finding recurs after two fix attempts or the
  cleanup loop is no longer converging
- the full check lane is unclear after reading `COMMANDS.md` and repo tooling
- a check failure is clearly unrelated and fixing it would materially broaden
  scope
- a destructive or irreversible action would be required

Stay autonomous when the next step is a normal child skill call, a scoped fix to
the planned work, or a repeat required by the Critical/High cleanup gate.

## Global constraints

- Treat the working tree as input from any author or process. Do not assume a
  human made the changes.
- Always treat staged, unstaged, and untracked files as one combined
  working-tree target for cleanup and final checks.
- Keep scope tight to the plan plus directly required support edits for
  accepted findings and observed check failures.
- Do not stage, commit, discard, or destructively rewrite changes unless the
  user explicitly asks.
- Fix all accepted cleanup findings regardless of severity inside each
  `/clean-and-fix-code` run.
- Use Critical and High resolved-finding counts as the only cleanup repeat gate.
- Rejected findings never count toward the cleanup repeat gate.
- Do not stop a cleanup round merely because Critical and High findings reach
  zero if that child run still has accepted Medium or Low findings to fix.
- Do not run an automatic confirmation cleanup round after a Medium/Low-only
  cleanup round.
- If a Critical or High fix changes the relevant surface area materially, run
  one more `/clean-and-fix-code` pass instead of assuming the old review still
  applies.
- A broad-but-clear fix is still in scope when it resolves an accepted finding
  against the working-tree target and does not trigger a human checkpoint.
- Do not modify unrelated code just because it is nearby.
- Do not skip, disable, weaken, or lower thresholds for checks or tests.

## Minimal Status Protocol

At each major stage, echo the fully-implement-plan report path and state the
current phase: preflight, implementing attempt N, cleanup round N, verification
attempt N, full checks, or closing.

## Required report structure

Write `.agent-layer/tmp/fully-implement-plan.<run-id>.report.md` with:

1. `# Fully Implement Plan Summary`
2. `## Inputs`
3. `## Implementation Attempts`
4. `## Cleanup Rounds`
5. `## Verification Attempts`
6. `## Check Lane`
7. `## Rejected and Deferred Findings`
8. `## Final Status`
9. `## Residual Risk`

In each cleanup round, use:

```text
### Cleanup Round N
- Report: <path or handoff source>
- Findings:
  - <title> (<Severity>, <Confidence>, <Verdict>) - <location>: <why it matters>
- Resolved findings:
  - <title> (<Severity>) - <fix description>; files: <file(s)>
- Counts: accepted=<n>, rejected=<n>, deferred=<n>, already_resolved=<n>
- Critical/High resolved findings: <count>
- Repeat decision: <repeat|stop> - <reason>
```

## Definition of done

- `/implement-plan` ran with the supplied plan, task, and context artifacts.
- `/clean-and-fix-code` ran at least once after implementation unless there
  were no uncommitted changes to review, in which case the report states why.
- Cleanup repeated only while a round resolved Critical or High findings.
- `/verify-work` reached `complete` or acceptable `complete-with-follow-up`.
- `/repair-checks` ran the full/all-checks lane, including tests, and either
  passed or reported a blocker.
- The final report records every child report path, cleanup finding counts,
  rejected/deferred findings, and the final stop reason.

## Final handoff

Report:

- fully-implement-plan report path
- plan, task, context paths
- implementation attempt count
- cleanup round count and the round that stopped the cleanup loop
- total cleanup findings and accepted/rejected/deferred/already-resolved counts
- rejected and deferred findings, if any
- verification verdict and report path
- full check lane result from `/repair-checks`
- any blocker or residual risk

---
name: loop-clean-and-fix
description: >-
  Run repeated review/fix rounds on the uncommitted working tree until the loop
  reaches diminishing returns. Use to improve any uncommitted code.
---

# loop-clean-and-fix

## Required inputs

Fail before side effects unless all are present:

- `review_agents`: one or more dispatch agent roles for `/clean-and-fix-code`

No file, directory, or diff target is required. The target is always the full
uncommitted working tree.

Dispatch agent roles may be terse. Before dispatching, inspect live
`al dispatch options` output and fail if a requested role or override is
unsupported.

## Required artifact

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>`. Write:

- `.agent-layer/tmp/loop-clean-and-fix.<run-id>.report.md`

## Context Discipline

You are the orchestrator for this skill. Do not do work that belongs to
subagents or delegated skills in the orchestration context. Preserve your
context to make strategic decisions, enforce gates, reconcile returned outputs,
and continue this skill's workflow after every delegation returns.

## Rules

- Treat staged, unstaged, and untracked files as one cleanup target.
- Do not ask the user to confirm files before starting.
- If the working tree is empty, write the report with `resolved_findings: []`
  and stop.
- If `/clean-and-fix-code` fails, stops at its own checkpoint, or returns output
  that cannot support the repeat decision, stop the loop, record the failure as
  the stop reason, and surface it to the user. Do not run another cleanup round.
- If the cleanup output includes findings but omits parseable counts or
  `resolved_findings`, preserve the available findings in the issue ledger and
  mark the run blocked on unparseable delegated output.
- If resolved finding severity cannot be classified, stop instead of inferring
  the repeat gate from prose.
- Do not run a confirmation round after a Medium/Low-only round.
- Rejected, deferred, and already-resolved findings never count toward the
  repeat gate.
- Do not stage, commit, discard, or destructively rewrite changes unless the
  user explicitly asks.
- Do not modify unrelated code just because it is nearby.
- Ask when a deferred finding blocks cleanup, the same unresolved cleanup
  finding recurs after two rounds, or another round would require a user-only
  decision or destructive action.

## Workflow

1. Run `/clean-and-fix-code` on the full uncommitted working tree with
   `review_agents`. Treat its output as one cleanup round: record the cleanup
   report path, accepted/rejected/deferred/already-resolved counts,
   `resolved_findings`, blockers, residual risk, and every reported issue for
   the final report.
2. Read the cleanup output and determine whether the round succeeded, failed, or
   stopped at a checkpoint. If it failed or stopped, write the final report with
   the failure/checkpoint as the stop reason and do not repeat. On success,
   count total resolved findings and count resolved High and Critical findings
   separately.
3. Repeat from step 1 only when the successful round resolved at least one
   finding and resolved at least one High or Critical finding. If the round
   resolved zero findings, or both the resolved High and resolved Critical
   counts are zero, stop the loop.
4. Write the final report and prepare the final message for the user.

## Required report structure

Write `.agent-layer/tmp/loop-clean-and-fix.<run-id>.report.md` with:

1. `# Loop Clean and Fix Summary`
2. `## Inputs`
3. `## Cleanup Rounds`
4. `## Issue Ledger`
5. `## Resolved Findings`
6. `## Stop Reason`
7. `## Residual Risk`

In `## Issue Ledger`, include one Markdown table row for every issue reported by
any `/clean-and-fix-code` round, regardless of classification or outcome.

Required columns:

`| Round | Severity | Classification | Issue | Location | Outcome | Source |`

Use the delegated skill's classification when available, such as accepted,
resolved, rejected, deferred, already-resolved, blocker, or unclassified. The
`Round` column is required. If no issues were reported, include a single
`No issues reported` row.

## Report Contents

Include:

- inputs
- cleanup round count
- cleanup report path for each round
- accepted/rejected/deferred/already-resolved counts
- the full `## Issue Ledger` table
- aggregate `resolved_findings`
- stop reason
- blocker or residual risk

## Definition of done

- `/clean-and-fix-code` ran at least once unless the working tree was empty.
- Cleanup repeated only while the previous successful round resolved at least
  one High or Critical finding.
- The report records the stop reason and aggregate `resolved_findings`.

## Final handoff

Present the results to the user in chat. Include:

1. The loop-clean-and-fix report path.
2. Cleanup round count and stop reason.
3. The full issue ledger table with every issue from every round, including the
   round number and classification. Do not replace this table with only a report
   link.
4. Aggregate `resolved_findings`.
5. Any blocker or residual risk.

---
name: review-plan
description: >-
  Review and repair a plan/task/context artifact set through three equivalent
  independent reviewers, then report implementation readiness.
---

# review-plan

Run one bounded pre-implementation review. The current/root agent owns fanout,
cross-review synthesis, artifact edits, and the final readiness decision.

## Required inputs

- exactly three `plan_reviewers` as self-contained dispatch target
  specifications
- plan artifact path
- task artifact path
- context artifact path
- optional specification artifact path
- optional facts-only workflow manifest path

Fail before dispatch if an artifact is missing, unreadable, internally
inconsistent, or if the reviewer count is not exactly three.

## Output artifacts

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>` and materialize:

- one immutable report per fanout child at
  `.agent-layer/tmp/review-plan.<run-id>.<child-run-id>.report.md`
- one synthesis report at `.agent-layer/tmp/review-plan.<run-id>.report.md`

The fanout manifest and each canonical child result are the source of truth.
Record the source child run, canonical result path, and result content hash in
each materialized report. Never put a report destination in the shared prompt.

## Independence contract

Every external reviewer receives byte-equivalent shared inputs:

- the complete contents of `assets/agent-review-prompt.md`
- the complete plan, task, context, and optional spec contents
- the same facts-only manifest contents when present

Only provider-required mechanical envelope, target configuration, and immutable
run identity may differ. Do not forward another reviewer's output, planner
recommendations, inferred risk/readiness labels, scores, or prior synthesis.
Validate artifact and command hashes before reusing factual evidence.

## Workflow

### 1. Preflight

Read every artifact completely and confirm objective/scope alignment. Validate
the optional machine-readable manifest. Reject opinion-bearing fields such as
recommendation, risk, readiness, confidence, materiality, verdict, or summary.

Construct one shared reviewer prompt with complete artifact contents and
content hashes. Do not assign complementary coverage to external reviewers.

### 2. Fan out once

Invoke one synchronous command with the shared prompt:

```bash
al dispatch fanout \
  --target '<reviewer-1>' \
  --target '<reviewer-2>' \
  --target '<reviewer-3>'
```

Supply the shared prompt through standard input. Wait on this one invocation;
do not poll children or `inspect`. A nonzero aggregate result is a blocker
only after reading its complete per-child terminal evidence.

Each external reviewer returns one compact terminal report conforming to the
asset.

### 3. Materialize and validate reports

For every child, read the canonical terminal result, verify its hash, and write
one immutable report with source metadata. Validate:

- every finding cites evidence, impact, and a correction
- child transcripts, progress, style notes, and speculative suggestions are
  absent from the terminal report
- recommendation is exactly `approve` or `changes-needed`

Do not redispatch or discard a child whose lifecycle failure is ambiguous
merely to obtain a cleaner result; report the ambiguous outcome as evidence.

### 4. Synthesize and revise

Evaluate every reported finding against primary artifacts and repository
evidence. Agreement never substitutes for evidence. Merge duplicates and keep
only material correctness, safety, scope, implementability, verification, or
maintainability gaps.

Apply accepted corrections and update direct dependencies. Ask only about a
genuine user-owned decision. Do not redispatch merely because accepted findings
changed the artifacts; run a new review only when the contract or material
scope changed independently.

### 5. Report readiness

Write the synthesis report with artifact and source hashes, accepted changes,
unresolved user decisions, and exactly one readiness value:

- `implementation-ready`
- `blocked-for-user-decision`

Return all materialized report paths, the fanout manifest path, accepted
changes, any smallest unblocking question, and readiness.

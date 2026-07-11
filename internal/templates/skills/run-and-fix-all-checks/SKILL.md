---
name: run-and-fix-all-checks
description: >-
  Run the repository's full documented check lane, directly repair observed
  failures, and repeat only until the lane passes or a concrete blocker remains.
---

# run-and-fix-all-checks

Own the repository's full check lane from first run through a passing result.
Use observed failure evidence to make direct root-cause repairs; do not create a
planning, implementation, or verification sub-workflow.

## Scope

Read `COMMANDS.md` and select the documented full continuous-integration or
all-checks lane. It must include all required tests and any required format,
lint, typecheck, coverage, build, documentation, or pre-commit checks.

Prefer one canonical command when it covers the lane. Otherwise use the
documented command sequence. If the repository does not define the lane clearly,
inspect its tooling only enough to resolve the ambiguity; stop if no
authoritative lane can be determined.

## Output artifacts

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>` and write:

- `.agent-layer/tmp/run-and-fix-all-checks.<run-id>.report.md`
- failure evidence under the same prefix for each failed round

## Rules

- Preserve the failing command, exit status, relevant output, and covered
  repository state before editing.
- Tie every repair to an observed failure and fix its root cause directly.
- Run mutations sequentially against the latest working tree.
- Never skip, disable, weaken, or lower thresholds for checks or tests.
- Do not silently substitute a faster or narrower lane for the documented full
  lane.
- Do not stage, commit, discard, or destructively rewrite changes without the
  user's explicit request.
- Stop for missing credentials, unavailable external services, destructive
  requirements, schema changes, or user-owned behavior and architecture
  decisions.

## Workflow

### 1. Run the full lane

Run the resolved full check lane and record every command result. If it passes,
finish with `checks-passed`.

### 2. Diagnose and repair directly

For each failure:

1. Save a focused failure artifact.
2. Reproduce or narrow the failure when needed to identify its root cause.
3. Repair the root cause directly, including required tests, documentation, or
   memory updates.
4. Run the narrowest credible check covering the repair.
5. Record the changed files, evidence, and residual risk.

Resolve routine repair details autonomously. If multiple viable fixes would
materially change behavior, architecture, scope, risk, or cost, ask the user
before choosing.

### 3. Rerun on concrete failure evidence

Rerun the full lane after repairs. A failed lane is concrete evidence for
another repair pass; a desire for more confidence is not.

If an evidence-equivalent failure recurs, do not repeat the same repair
strategy. Revisit the causal model and add focused instrumentation or another
discriminating diagnostic when useful. Continue only when new evidence supports
a safe repair; otherwise stop with `repeated-failure`. Stop with `blocked` when
the failure is external or cannot be repaired safely within scope.

### 4. Report

Write:

1. `# Run All Checks Summary`
2. `## Check Lane`
3. `## Check Rounds`
4. `## Direct Repairs`
5. `## Stop Reason`
6. `## Residual Risk`

Record the selected commands, each round's result and failure artifact, each
repair and focused evidence, and one stop reason:

- `checks-passed`
- `blocked`
- `repeated-failure`

## Completion contract

Return the report path, full-lane commands, round count, direct repairs, and
final passing evidence or blocker. The stage is complete only when the full lane
passes after the latest changes or a named failure gate prevents safe progress.

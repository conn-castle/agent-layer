---
name: repair-checks
description: >-
  Run the repository's documented checks in the intended order, fix the
  failures that are in scope, and repeat until the checks pass or a real
  blocker requires human direction.
---

# repair-checks

This is the repo-check recovery loop.
It should:
- discover the real required checks
- run them in the right order
- fix the failing causes
- re-run until the lane is green or a blocker makes further autonomous work unsafe

## Defaults

- Default verification depth is the repo's fast lane when it exists; otherwise use the documented standard lane.
- Default iteration cap is 3 repair loops unless the user says otherwise.
- Default scope is failures caused by the current work, not every unrelated red test in the repository.
- Prefer a single documented all-checks command when the repo has one.

## Inputs

Accept any combination of:
- fast or full verification preference
- explicit checks to include
- whether pre-commit should be included
- an iteration cap
- whether repeatable command discoveries should update `COMMANDS.md`

## Required artifact

Write the report to:
- `.agent-layer/tmp/repair-checks.<run-id>.report.md`

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>`.
Create the file with `touch` before writing.

## Multi-agent pattern

Use subagents liberally when available.

Recommended roles:
1. `Repo scout`: discovers the required check sequence.
2. `Executor`: runs commands and captures failures.
3. `Fixer`: addresses the failing causes.
4. `Verifier`: re-runs the lane until it passes or blocks.

## Global constraints

- Do not skip required checks or weaken gates to get a green result.
- Do not guess missing commands.
- Keep fixes tied to the observed failures.
- Log unrelated but discovered issues instead of widening the repair scope silently.

## Human checkpoints

- Required: ask when the required check lane is unclear or conflicting after reading `COMMANDS.md` and repo tooling.
- Required: ask before installing missing tooling.
- Required: ask when a failure is clearly unrelated/systemic and fixing it would materially broaden scope beyond the current task.
- Optional: ask when multiple documented lanes are valid and the choice materially affects runtime or confidence.
- Stay autonomous through the normal fail-fix-rerun loop.

## Verification workflow

### Phase 0: Preflight (Repo scout)

1. Confirm baseline with:
   - `git status --porcelain`
   - `git diff --stat`
2. Read `COMMANDS.md` first.
3. Extract the intended check order:
   - one all-checks command when defined
   - otherwise ordered lint, format, pre-commit, test, and coverage commands as documented

### Phase 1: Resolve command ambiguity (Repo scout)

If the lane is unclear:
- inspect repo tooling such as `Makefile`, `Taskfile`, `justfile`, package scripts, or CI config
- stop and ask if ambiguity remains

If a reusable command was discovered and repo rules allow it, update `COMMANDS.md`.

### Phase 2: Execute the lane (Executor)

1. Run the documented lane in order.
2. Capture the first meaningful failure set.
3. If everything passes, stop and report success.

### Phase 3: Fix the failing causes (Fixer)

1. Fix the root cause of the observed failures.
2. Keep changes minimal and in scope.
3. If the real fix is broad, systemic, or unrelated to the current task, escalate instead of freelancing.

### Phase 4: Re-run and decide (Verifier)

1. Re-run the same lane.
2. Continue until:
   - the lane passes
   - the iteration cap is hit
   - or a blocker requires human input

## Required report structure

Write `.agent-layer/tmp/repair-checks.<run-id>.report.md` with:

1. `# Check Repair Summary`
2. `## Lane`
3. `## Commands Run`
4. `## Failures Fixed`
5. `## Pass Status`
6. `## Remaining Blockers`

## Guardrails

- Do not treat flaky output as fixed unless a re-run proves it.
- Do not silently downgrade from full to fast checks.
- Do not change test expectations unless the original expectation is actually wrong and the fix remains in scope.
- Do not call the repo green without the observed passing command output.

## Final handoff

After writing the report:
1. Echo the report path.
2. Summarize the lane used, commands run, and failures fixed.
3. State whether all checks passed and call out any blocker that prevented a clean finish.

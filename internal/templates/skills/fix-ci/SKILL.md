---
name: fix-ci
description: >-
  Diagnose and fix CI failures for a PR, iterating through fix cycles until all
  CI checks pass. Each cycle: diagnose failure, fix code, audit uncommitted
  changes, commit, push, and re-check CI.
---

# fix-ci

This is the CI failure repair skill.
It should:
- diagnose why CI is failing
- fix the underlying issue
- audit the fix before committing
- commit and push
- verify CI passes
- repeat if CI still fails

## Defaults

- Default PR is the current branch's open PR.
- Default diagnosis uses CI logs from `gh run view --log-failed`.
- Default fix scope is the minimum change needed to make CI pass.

## Inputs

Accept any combination of:
- a PR number or URL
- a specific CI run ID
- hints about the failure from the caller

## Required behavior

Use subagents liberally when available.

Delegate to:
- `audit-and-fix-uncommitted-changes` before every commit

## Global constraints

- Keep fixes minimal and targeted to the CI failure.
- Do not make unrelated changes just because CI logs reveal other warnings.
- Do not disable, skip, or weaken tests or CI checks to make them pass.
- Do not lower coverage thresholds or remove failing tests.
- Treat each CI fix as a focused patch, not a refactoring opportunity.

## Human checkpoints

- Required: ask when the CI failure appears to be an infrastructure or environment issue rather than a code issue.
- Required: ask when the same CI failure persists after 3 fix attempts.
- Required: ask when fixing the CI failure would require a materially broader scope change.
- Stay autonomous during normal diagnose, fix, audit, commit, push, re-check cycles.

## Fix workflow

### Phase 1: Diagnose the failure (Diagnostician)

1. Get CI status: `gh pr checks <pr-number>` to identify which checks failed.
2. Get failure logs: `gh run view <run-id> --log-failed` for each failed check.
3. Identify the root cause:
   - test failures
   - lint/format errors
   - type errors
   - build failures
   - other CI step failures
4. Read the relevant source files and test files to understand the failure.

### Phase 2: Fix the issue (Fixer)

1. Implement the minimum fix needed to resolve the CI failure.
2. If the fix requires understanding project conventions, read `COMMANDS.md` first.
3. Run local verification when possible (the same commands CI runs) before committing.

### Phase 3: Audit and commit (Auditor + Committer)

1. Use the `audit-and-fix-uncommitted-changes` skill to review and stabilize the fix.
2. Stage all changes: `git add -A`
3. Craft a commit message describing the CI fix.
4. Commit and push.

### Phase 4: Verify CI (Verifier)

1. Wait for CI checks to complete on the new push.
2. If all checks pass, the fix is complete.
3. If any check still fails:
   a. Track which failures are new vs. recurring.
   b. Return to Phase 1 with the new failure information.

## Guardrails

- Do not skip the audit-and-fix step before committing.
- Do not disable or weaken CI checks to make them pass.
- Do not expand scope beyond what is needed to fix the CI failure.
- Do not treat CI warnings as failures unless they are configured to fail the build.
- Track recurring failures and escalate rather than looping indefinitely on the same issue.

## Final handoff

After CI passes:
1. State which CI checks were failing and what was fixed.
2. State how many fix iterations were needed.
3. Confirm all CI checks are now passing.

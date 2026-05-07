---
name: fix-ci
description: >-
  Diagnose and fix failing CI/checks on an open PR, iterating through diagnose,
  patch, audit, commit, push, and re-check until green or blocked. Use when a
  PR's GitHub checks are failing. Use `repair-checks` for local checks and
  `address-pr-comments` for reviewer feedback.
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
- Default diagnosis uses CI logs from `gh run view --log-failed` and any artifacts available from the failed workflow run.
- Default artifact location is `./.agent-layer/tmp/ci-artifacts/<run-id>` so downloaded reports stay in the agent temp area.
- Default fix scope is the minimum change needed to make CI pass.

## Inputs

Accept any combination of:
- a PR number or URL
- a specific CI run ID
- hints about the failure from the caller

## Required behavior

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
- When a checkpoint involves a genuine tradeoff between substantive alternatives, present at least two options with brief pros and cons, state which you recommend and why, and let the human decide.
- Stay autonomous during normal diagnose, fix, audit, commit, push, re-check cycles.

## Fix workflow

### Phase 1: Diagnose the failure (Diagnostician)

1. Get CI status: `gh pr checks <pr-number>` to identify which checks failed.
2. Get failure logs: `gh run view <run-id> --log-failed` for each failed check.
3. Download available artifacts for each failed workflow run before coding if available: `mkdir -p .agent-layer/tmp/ci-artifacts/<run-id>` then `gh run download <run-id> --dir .agent-layer/tmp/ci-artifacts/<run-id>`.
   - Inspect artifact contents alongside logs. Prioritize test reports, coverage reports, screenshots/videos, build output, generated files, and any tool-specific diagnostic bundles.
4. Identify the root cause from logs and artifacts:
   - test failures
   - lint/format errors
   - type errors
   - build failures
   - other CI step failures
5. Read the relevant source files and test files to understand the failure.

### Phase 2: Fix the issue (Fixer)

1. If the fix requires understanding project conventions, read `COMMANDS.md` first.
2. When the CI failure is locally testable, find or create a local reproducer (failing test or command) before fixing. This confirms the diagnosis and prevents false-green commits.
3. Implement the minimum fix needed to resolve the CI failure.
4. Run local verification (the same commands CI runs) to confirm the fix before committing.

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
- Do not patch from CI logs alone when CI artifacts are available; logs and artifacts together are the diagnostic source of truth.
- Do not treat CI warnings as failures unless they are configured to fail the build.
- Track recurring failures and escalate rather than looping indefinitely on the same issue.

## Definition of done

- `gh pr checks <pr-number>` shows every required CI check passing on the latest pushed commit.
- Logs and any available artifacts for each failed run were inspected; missing or unavailable artifacts were called out explicitly.
- Each fix cycle committed through the `audit-and-fix-uncommitted-changes` skill before push; no check was disabled, skipped, weakened, or had its threshold lowered.
- The fix iteration count is recorded and stayed below the 3-attempt escalation threshold for any single recurring failure.
- Scope of the changes is confined to what the CI failures required, with no opportunistic edits.

## Final handoff

After CI passes:
1. State which CI checks were failing and what was fixed.
2. State how many fix iterations were needed.
3. Confirm all CI checks are now passing.

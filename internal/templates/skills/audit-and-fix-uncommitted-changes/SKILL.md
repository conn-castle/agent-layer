---
name: audit-and-fix-uncommitted-changes
description: >-
  Audit and iteratively fix all uncommitted working-tree changes until a
  confirming review finds no remaining actionable issues. Use when the agent
  needs to stabilize in-progress diffs from any author or process, run repeated
  review/fix cycles, and deliver a round-by-round severity report of what was
  found and fixed.
---

# audit-and-fix-uncommitted-changes

This is the working-tree audit and fix orchestrator.
It should run an iterative loop that:
- selects the full working-tree change target
- audits the target and the minimum necessary surrounding context
- verifies and fixes accepted findings
- re-audits after each fix pass
- repeats until a confirming review finds no remaining actionable findings
- reports each round's findings and fixes to the human with severities

Use this skill only for the full audit-and-fix loop over all uncommitted changes. For report-only review or single-report remediation, use the dedicated lower-level skills instead.

## Scope default

Default scope:
- all uncommitted changes in the current working tree
- staged changes
- unstaged changes
- untracked files
- any small adjacent edits directly required to root-cause-fix accepted findings

Do not interpret this skill as permission to review old commits, sweep the whole repository, or fix unrelated known issues unless the user explicitly asks.

## Inputs

- No fixed round cap. Run as many audit/fix rounds as needed to converge. In practice, simple changes should converge in 2-3 rounds (minimum 2: at least one fix round plus one confirmation round).

## Required behavior

Use subagents liberally when available.

At minimum, use:
- parallel audit reviewers with different lenses
- a findings resolver/fixer
- a synthesizer that keeps the round-by-round report current

Prefer the dedicated skills that already exist:
- `review-scope`
- `resolve-findings`
- `simplify-code` when a fix exposes obvious local complexity that remains behavior-preserving and in scope

## Required artifacts

Use one shared `run-id = YYYYMMDD-HHMMSS-<short-rand>`.

Always create:
- `.agent-layer/tmp/audit-and-fix-uncommitted-changes.<run-id>.report.md`

Create the file with `touch` before writing.

Reuse and reference the per-round artifacts created by the delegated skills:
- `.agent-layer/tmp/review-scope.<run-id>.report.md`
- `.agent-layer/tmp/resolve-findings.<run-id>.report.md`
- `.agent-layer/tmp/resolve-findings.<run-id>.plan.md`
- `.agent-layer/tmp/resolve-findings.<run-id>.task.md`

The master report is the human-facing round ledger. It must remain readable without opening the child artifacts.

## Global constraints

- Treat the working tree as input from any author or process. Do not assume a human made the changes.
- Always review one combined target for the whole working tree.
- Include untracked files in the default target.
- Fix all accepted findings regardless of severity.
- Use Critical and High counts for urgency, escalation, and stop-state reporting only.
- Do not stop merely because Critical and High findings reach zero if any actionable Medium or Low findings remain.
- Do not stage, commit, or discard changes unless the user explicitly asks.
- Keep scope tight to the selected target plus directly required supporting edits.
- If a fix changes the relevant surface area materially, start the next audit round instead of assuming the old review still applies.

## Human checkpoints

- Required: ask when the target is empty and no credible review scope exists.
- Required: ask when an accepted finding requires materially broader scope, an architectural decision, or a user-visible behavior change beyond the current target.
- Required: ask when a finding cannot be verified with the available code, tests, or docs.
- Required: ask when a deferred finding blocks convergence.
- Required: ask when the same unresolved finding recurs after two fix attempts or the loop is no longer converging.
- Required: ask before any destructive or irreversible action would be required.
- Stay autonomous during normal audit/fix/re-audit cycles when the target and accepted fixes are clear.

## Orchestration loop

### Phase 0: Preflight (Repo scout)

1. Run `git status --porcelain`, `git diff --stat --cached`, and `git diff --stat` to confirm there is working-tree material to review.
2. Read in this order when they exist:
   - `COMMANDS.md`
   - `README.md`
   - `DECISIONS.md`
   - `ISSUES.md`
3. Record baseline assumptions, including whether untracked files are present.
4. If there are no staged, unstaged, or untracked files, stop and ask instead of reviewing unrelated history.

### Phase 1: Select the working-tree target (Scope scout)

1. Build the target set:
   - staged diff: `git diff --cached`
   - unstaged diff: `git diff`
   - untracked files: `git ls-files --others --exclude-standard`
2. Review staged + unstaged + untracked as one working-tree change set.
3. State the actual target at the top of the master report.

### Phase 2: Gather only the needed context (Lead reviewer)

1. Read the minimum surrounding code and tests needed to understand the target.
2. Check docs and memory files only when they matter to the touched area.
3. Call out any overlapping existing known issues from `ISSUES.md` instead of presenting them as novel findings.
4. Record important assumptions in the master report before starting Round 1.

### Phase 3: Run audit Round N (Audit reviewers)

Use the `review-scope` skill on the current target.

The audit must look for:
- correctness issues
- architecture and ownership problems
- reliability or performance risks
- missing tests or docs
- maintainability problems

For each round, copy the high-signal findings summary into the master report under:
- `## Round N Findings`

For every finding, record:
- title
- severity
- confidence
- location
- short why-it-matters summary

### Phase 4: Verify and fix Round N findings (Fixers)

Use the `resolve-findings` skill on the Round N review report with authority to fix accepted findings.

Rules:
- verify every finding independently before accepting it
- fix every accepted finding regardless of severity
- reject false positives explicitly
- do not treat `defer` as a clean outcome; escalate instead when a valid issue cannot be resolved in scope
- if multiple findings share one root cause, fix once and note every resolved finding

Copy the fix summary into the master report under:
- `## Round N Fixes`

For each fixed finding, record:
- title
- severity
- short description of the fix
- key files touched

Also record under:
- `## Round N Status`

Include:
- accepted findings fixed this round
- rejected findings
- deferred findings
- unresolved Critical count
- unresolved High count

### Phase 5: Confirmation round (Convergence gate)

After Phase 4 of any round that applied fixes, a full confirmation round is required before the run can close.

1. Return to Phase 3 to start Round N+1 as a full audit round on the updated working-tree target.
2. Run Phase 3 (audit) → Phase 4 (verify) as a complete numbered round.
3. If Phase 4 of the confirmation round produces zero accepted findings, the run converges — proceed to Phase 6.
4. If the confirmation round produces accepted findings, fix them, then repeat: the next round becomes the new confirmation candidate.

Convergence rule:
- A run converges only when a full numbered round completes Phase 3 and Phase 4 with zero accepted findings.
- The confirmation round must be a separate round from any round that applied fixes.
- The confirmation round must have its own `## Round N Findings` and `## Round N Status` sections in the master report, explicitly stating zero accepted findings.

Severity rule:
- the final non-clean round may contain only Medium or Low findings, but they still must be fixed before the confirmation round can begin

If a fix exposes obvious local complexity that is behavior-preserving and in scope:
- use the `simplify-code` skill
- then treat the next round as the confirmation candidate

Escalate if the loop is not converging (same findings recurring, fix attempts not resolving issues, or complexity growing instead of shrinking).

### Phase 6: Close the run (Reporter)

When the loop converges:
1. add `## Final Verification` to the master report
2. identify which round was the confirmation round and state its artifact path
3. state how many rounds were required (including the confirmation round)
4. summarize whether any findings were rejected as false positives
5. state explicitly that the confirmation round produced zero accepted findings

## Required master report structure

Write `.agent-layer/tmp/audit-and-fix-uncommitted-changes.<run-id>.report.md` with:

1. `# Audit and Fix Summary`
2. `## Target`
3. `## Assumptions`
4. `## Round 1 Findings`
5. `## Round 1 Fixes`
6. `## Round 1 Status`
7. Repeat the three Round sections for each additional round
8. `## Final Verification`
9. `## Residual Risk`

Use this reporting style:

```text
Round 1 findings:
- Missing nil guard in loader path (High) — pkg/foo/loader.go
- Missing regression test for rejected config branch (Medium) — pkg/foo/loader_test.go

Round 1 fixes:
- Added nil guard and error path coverage (High) — pkg/foo/loader.go, pkg/foo/loader_test.go
- Added regression test for rejected config branch (Medium) — pkg/foo/loader_test.go

Round 2 findings (confirmation round):
- No actionable findings.

Round 2 status (confirmation round):
- Accepted findings: 0
- Confirmation round: clean
```

If a finding reappears in a later round, say so explicitly instead of hiding the recurrence.

## Minimal status protocol

At each major stage, echo the master report path, identify the current target, and state one of:
- preflighting the working tree
- selecting the target
- auditing round N
- fixing round N findings
- running confirmation round N
- closing the run

## Final handoff

After the run, present the results to the user in chat so that every finding and fix is clearly attributed to the round that produced it.

Required chat output structure:

1. Echo the master report path.
2. State total rounds, total findings, and final convergence status.
3. Present a **Key fixes applied** table sorted by Round (primary) then Severity (secondary within a round). Every row must include the Round column so the user can see at a glance which check loop found and fixed each issue.
4. Below the table, list rejected and deferred findings (if any) with their round numbers.
5. State which round was the confirmation round and that it ended with zero accepted findings.

Example chat output format:

```text
Summary:
- 3 rounds to converge (Round 3 confirmation)
- 12 findings from 5 parallel reviewers
- 5 accepted and fixed, 6 rejected, 1 deferred

Key fixes applied (unstaged):

| Round | Severity | Fix                                        | Files                              |
|-------|----------|--------------------------------------------|------------------------------------|
| 1     | High     | Added nil guard in loader error path       | pkg/foo/loader.go, loader_test.go  |
| 1     | Medium   | Added regression test for rejected config  | pkg/foo/loader_test.go             |
| 1     | Low      | Fixed inconsistent log level               | pkg/foo/logger.go                  |
| 2     | Medium   | Added missing context cancellation check   | pkg/bar/handler.go, handler_test.go|
| 2     | Low      | Corrected doc comment on exported function | pkg/bar/handler.go                 |

Rejected (6): [list with round numbers]
Deferred to ISSUES.md (1): [list with round numbers]
```

The Round column is required. Do not omit it or collapse multiple rounds into an unlabeled table.

## Guardrails

- Do not silently split the working tree into separate staged and unstaged reviews.
- Do not silently ignore untracked files in the default mode.
- Do not stop once High/Critical findings are gone if actionable Medium/Low findings remain.
- Do not carry unresolved deferred findings into a clean final report.
- Do not collapse multiple rounds into one summary.
- Do not skip the confirmation round. A round that applied fixes cannot also be the confirmation round.
- Do not modify unrelated code just because it is nearby.
- Keep each round grounded in concrete review and resolution artifacts.

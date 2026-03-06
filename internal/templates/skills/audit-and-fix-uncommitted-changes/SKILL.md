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

- Global round cap: 4 audit/fix rounds by default. If the user explicitly provides a different cap, use it.

## Required behavior

Use subagents liberally when available.

At minimum, use:
- parallel audit reviewers with different lenses
- a findings resolver/fixer
- a synthesizer that keeps the round-by-round report current

Prefer the dedicated skills that already exist:
- `review-scope`
- `resolve-findings`
- `mechanical-cleanup` when a fix exposes obvious local cleanup that remains behavior-preserving and in scope

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
- Required: ask when the same unresolved finding recurs after two fix attempts, the global round cap is reached, or the loop is no longer converging.
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

### Phase 5: Re-audit after Round N (Convergence reviewer)

After the fixes land:
1. rerun the `review-scope` skill on the updated working-tree target
2. if the new review finds actionable findings, start Round `N + 1`
3. if the new review appears clean, still verify through the `resolve-findings` skill that there are zero accepted findings

Convergence rule:
- stop only when the post-fix audit yields zero actionable findings after verification

Severity rule:
- the final non-clean round may contain only Medium or Low findings, but they still must be fixed before the run can finish

If a fix exposes obvious local cleanup that is behavior-preserving and in scope:
- use the `mechanical-cleanup` skill
- then rerun the audit before continuing

Respect the global round cap from `## Inputs`.

Recommended cap: no more than 4 audit/fix rounds for the same working tree before escalating.

### Phase 6: Close the run (Reporter)

When the loop converges:
1. add `## Final Verification` to the master report
2. state the clean review artifact path
3. state how many rounds were required
4. summarize whether any findings were rejected as false positives
5. state explicitly that no actionable findings remained after the confirming review

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
```

If a finding reappears in a later round, say so explicitly instead of hiding the recurrence.

## Minimal status protocol

At each major stage, echo the master report path, identify the current target, and state one of:
- preflighting the working tree
- selecting the target
- auditing round N
- fixing round N findings
- re-auditing after round N
- closing the run

## Final handoff

After the run:
1. Echo the master report path.
2. Summarize what was fixed in each round, keeping severities attached to each item.
3. Call out any rejected findings or deferred items that affected convergence.
4. State whether the confirming review ended with zero actionable findings.

## Guardrails

- Do not silently split the working tree into separate staged and unstaged reviews.
- Do not silently ignore untracked files in the default mode.
- Do not stop once High/Critical findings are gone if actionable Medium/Low findings remain.
- Do not carry unresolved deferred findings into a clean final report.
- Do not collapse multiple rounds into one summary.
- Do not modify unrelated code just because it is nearby.
- Keep each round grounded in concrete review and resolution artifacts.

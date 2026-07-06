---
name: audit-and-fix-uncommitted-changes
description: >-
  Audit and iteratively fix uncommitted working-tree changes until a confirming
  review finds no remaining actionable issues. Use to stabilize in-progress
  diffs and report each review/fix round with severity.
---

# audit-and-fix-uncommitted-changes

This is the working-tree audit and fix orchestrator.
It should run an iterative loop that:
- selects the full working-tree change target
- audits the target and the minimum necessary surrounding context
- verifies and fixes accepted findings
- re-audits after each fix pass
- repeats until a confirming review finds no remaining actionable findings
- reports each round's findings and fixes to the user with severities

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

- No fixed round cap. Run as many audit/fix rounds as needed while Critical or High fixes continue to be applied. After each fix pass, stop when zero applied fixes were Critical or High severity.

## Required behavior

At minimum, use:
- parallel audit review agents with different lenses
- a findings resolver/fixer
- a synthesizer that keeps the round-by-round report current

Prefer the dedicated skills that already exist:
- `/prune-new-tests` (mandatory pre-pass when the diff added test files; delegated to subagent)
- `/simplify-new-code` (mandatory pre-pass when the diff added or modified production code; delegated to subagent)
- `/review-code`
- `/resolve-findings`
- `/simplify-new-code` again only if a fix exposes obvious local complexity within the diff that the initial pre-pass did not cover (delegated to subagent)

## Required artifacts

Use one shared `run-id = YYYYMMDD-HHMMSS-<short-rand>`.

Always create:
- `.agent-layer/tmp/audit-and-fix-uncommitted-changes.<run-id>.report.md`

Create the file with `touch` before writing.

The master report is the human-readable round ledger and the single place to preserve orchestrator state.

Delegated skill outputs are handled one way:
- Use `/review-code` report artifacts as findings input to `/resolve-findings`.
- Copy `/resolve-findings` outcomes from its final handoff into the master report.
- Do not require, open, echo, or cross-reference `/resolve-findings` report
  artifacts or fixer narrative.

## Continuation rule

Sub-skill returns are intermediate, not terminal. After every delegation, continue to the next numbered step in the same turn — the sub-skill's closing summary is not audit-and-fix's closeout. The loop exits only at the end of Phase 5, a listed human checkpoint, or a sub-skill that halts on its own human checkpoint without applying its changes.

## Context Discipline

You are the orchestrator. Do not do the child/subagent work yourself. Your job is to preserve your context to make strategic decisions, ensure each child skill or subagent follows its assigned contract, reconcile their outputs, enforce this workflow's gates, and continue the parent workflow after every child return.

## Global constraints

- Treat the working tree as input from any author or process. Do not assume a human made the changes.
- Always review one combined target for the whole working tree.
- Include untracked files in the default target.
- Fix all accepted findings regardless of severity.
- Use Critical and High applied-fix counts as the repeat gate.
- Rejected findings do not count toward the repeat gate.
- Do not stop a round merely because Critical and High findings reach zero if any accepted Medium or Low findings from that round remain unfixed.
- Do not stage, commit, or discard changes unless the user explicitly asks.
- Keep scope tight to the selected target plus directly required supporting edits.
- If a Critical or High fix changes the relevant surface area materially, start the next audit round instead of assuming the old review still applies.

## Human checkpoints

- Required: ask when the target is empty and no credible review scope exists.
- Required: ask when an accepted finding requires an architectural decision, an end-user-visible behavior change beyond the current target, or another user-only decision.
- Required: ask when a finding cannot be verified with the available code, tests, or docs.
- Required: ask when a deferred finding blocks convergence.
- Required: ask when the same unresolved finding recurs after two fix attempts or the loop is no longer converging.
- Required: ask before any destructive or irreversible action would be required.
- Stay autonomous during normal audit/fix/re-audit cycles when the target and accepted fixes are clear.
- A broad-but-clear fix is still in scope when it resolves an accepted finding against the working-tree target and does not trigger a human checkpoint.

## Orchestration loop

### Phase 0: Preflight and target selection (Repo scout)

1. Run `git status --porcelain`, `git diff --stat --cached`, and `git diff --stat` to confirm there is working-tree material to review.
2. If there are no staged, unstaged, or untracked files, stop and ask instead of reviewing unrelated history.
3. Read in this order when they exist: `COMMANDS.md`, `README.md`, `DECISIONS.md`, `ISSUES.md`.
4. Build the target set:
   - staged diff: `git diff --cached`
   - unstaged diff: `git diff`
   - untracked files: `git ls-files --others --exclude-standard`
5. Review staged + unstaged + untracked as one working-tree change set.
6. State the actual target and baseline assumptions at the top of the master report.

### Phase 0.5: Prune agent-side scope creep before any review round (Pre-pass)

Delegate each pre-pass to a subagent so its iterative loop does not register as audit-and-fix's closeout. Run in order:

1. `/prune-new-tests` subagent — when the diff added test files or test functions. Returns master report path plus deleted-count / surviving-gap count.
2. `/simplify-new-code` subagent — when the diff added or modified production code. Returns master report path plus applied-count / reverted-count.

Record each report path and one-line outcome under `## Pre-pass Cleanup`. If a pre-pass materially changes the working tree, restart Phase 0.

### Phase 1: Gather only the needed context (Lead reviewer)

1. Read the minimum surrounding code and tests needed to understand the target.
2. Check docs and memory files only when they matter to the touched area.
3. Call out any overlapping existing known issues from `ISSUES.md` instead of presenting them as novel findings.
4. Record important assumptions in the master report before starting Round 1.

### Phase 2: Run audit Round N (Audit review agents)

Use the `/review-code` skill on the current target.

For each round, copy the high-signal findings summary into the master report under `## Round N Findings`, recording for each finding: title, severity, confidence, location, and short why-it-matters summary.

### Phase 3: Verify and fix Round N findings (Fixers)

Use the `/resolve-findings` skill on the Round N review report with authority to fix accepted findings. Fix every accepted finding regardless of severity. Do not treat `defer` as a clean outcome; escalate instead when a valid issue cannot be resolved for a human-checkpoint reason. "Broader scope than a point fix" is not a valid deferral reason.

Copy the fix summary into the master report under `## Round N Fixes` (title, severity, fix description, files touched) and `## Round N Status` (accepted/rejected/deferred counts, unresolved Critical and High counts).

### Phase 4: Critical/High fix gate (Convergence gate)

Convergence rule:
- After fixing Round N findings, count applied fixes whose accepted finding severity was Critical or High.
- Rejected findings never count toward this gate.
- If the Critical/High applied-fix count is zero, close the run after recording Round N status.
- If the Critical/High applied-fix count is greater than zero, return to Phase 2 for another full audit round on the updated target.

Severity rule:
- A final round may contain accepted Medium or Low findings, but they still must be fixed before the run closes.

If a fix exposes obvious local complexity that is behavior-preserving and in scope:
- delegate `/simplify-new-code` to a subagent on the affected files
- then apply the same Critical/High applied-fix gate to decide whether another audit round is required

Escalate if the loop is not converging (same findings recurring, fix attempts not resolving issues, or complexity growing instead of shrinking).

### Phase 5: Close the run (Reporter)

When the loop converges:
1. add `## Final Verification` to the master report
2. identify which round stopped the loop
3. state how many rounds were required
4. summarize whether any findings were rejected as false positives
5. state explicitly that the stopping round applied zero Critical or High fixes

## Required master report structure

Write `.agent-layer/tmp/audit-and-fix-uncommitted-changes.<run-id>.report.md` with:

1. `# Audit and Fix Summary`
2. `## Target`
3. `## Assumptions`
4. `## Pre-pass Cleanup`
   - `/prune-new-tests` outcome (report path, deleted-count, surviving-gap count) or `Not applicable — no added tests`
   - `/simplify-new-code` outcome (report path, applied-count, reverted-count, out-of-scope count) or `Not applicable — no production-code changes`
5. `## Round 1 Findings`
6. `## Round 1 Fixes`
7. `## Round 1 Status`
8. Repeat the three Round sections for each additional round
9. `## Final Verification`
10. `## Residual Risk`

Each Round section uses the format: `- <title> (<Severity>) — <file(s)>`.
In each `## Round N Status`, include `Critical/High applied fixes: <count>`. If a finding reappears in a later round, say so explicitly.

Example:
```
## Round 1 Findings
- Missing nil guard in loader path (High) — pkg/foo/loader.go
## Round 1 Status
- Critical/High applied fixes: 1
## Round 2 Status
- Critical/High applied fixes: 0
```

## Minimal status protocol

At each major stage, echo the master report path and state the current phase (preflight, pre-pass cleanup, auditing round N, fixing round N, repeat-gate round N, or closing).

## Guardrails

- Do not carry unresolved deferred findings into a clean final report.
- Do not collapse multiple rounds into one summary.
- Do not run an automatic confirmation round after a round with zero Critical/High applied fixes.
- Do not count rejected findings toward the Critical/High repeat gate.
- Do not modify unrelated code just because it is nearby.
- Keep each round grounded in concrete reviewed diffs, /review-code findings, and observed verification.

## Definition of done

- The master report exists at `.agent-layer/tmp/audit-and-fix-uncommitted-changes.<run-id>.report.md` with `## Pre-pass Cleanup` populated (or marked not applicable for each sub-skill), one labeled `## Round N Findings` / `## Round N Fixes` / `## Round N Status` block per round, plus `## Final Verification` and `## Residual Risk`.
- The `## Pre-pass Cleanup` section names both `/prune-new-tests` and `/simplify-new-code` outcomes; either ran or is explicitly recorded as not applicable.
- The final round's status states `Critical/High applied fixes: 0`.
- No accepted finding from any round remains unresolved or deferred in the final report.
- The working tree was not staged, committed, or discarded by this skill.

## Final handoff

After the run, present the results to the user in chat so that every finding and fix is clearly attributed to the round that produced it.

Required chat output:

1. Echo the master report path.
2. State total rounds, total findings, and final convergence status.
3. Present a **Key fixes applied** table sorted by Round then Severity. The Round and Severity columns are required so every fix stays tied to the iteration that produced it. Example columns: `| Round | Severity | Fix | Files |`. If no fixes were applied, still print the table with a single `No fixes applied` row.
4. List rejected and deferred findings (if any) with their round numbers.
5. State which round stopped the loop and that it applied zero Critical or High fixes.

Example summary:
```
- 2 rounds to converge (Round 2 applied zero Critical/High fixes)
- 12 findings from 5 parallel review agents
- 5 accepted and fixed, 6 rejected, 1 deferred
```

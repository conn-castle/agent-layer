---
name: resolve-findings
description: >-
  Resolve a review findings report: verify and verdict each finding, coordinate
  bounded fixes for accepted findings, and aggregate rejected, deferred,
  already-resolved, and fixed outcomes into one report.
---

# resolve-findings

Treat review reports as inputs, not truth. This is an orchestration skill for
report-shaped inputs: it owns finding verdicts, grouping, fix coordination, and
the final resolution ledger.

## Required inputs

The caller must provide a path to the review findings report.

If the report path is missing, stop and ask for it. Do not discover, infer, or
auto-select reports from `.agent-layer/tmp/`.

## Defaults

- Treat the supplied report as the resolution contract, not as truth.
- If the report path does not exist or cannot be read, stop and ask for a
  corrected path.
- Default scope is the findings in the supplied report and the reviewed targets
  cited by those findings.
- If the user asked only for triage or report review, stop after verdicting and
  ask before editing files.

## Required artifact

Write the resolution report to:

- `.agent-layer/tmp/resolve-findings.<run-id>.report.md`

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>`. Create the file before writing.

## Workflow

### Phase 1: Parse and verify findings

For each finding:

1. Locate the evidence cited by the report.
2. Inspect the current repo state directly.
3. Decide whether the finding is valid now.
4. Assign exactly one verdict:
   - `accept`: valid and in scope to fix now
   - `reject`: not valid, not actionable, or based on incorrect evidence
   - `defer`: valid but blocked by a human checkpoint or out of current scope
   - `already-resolved`: valid originally, but no longer present

Do not accept a finding just because it sounds plausible.

### Phase 2: Group accepted findings

If zero findings are accepted, write `No accepted findings` in the resolution
report and stop without editing files.

For accepted findings:

- group duplicate or tightly coupled findings into one bounded fix target
- keep unrelated findings separate when they can be fixed independently
- apply accepted fixes to the actual reviewed target, whether it is plan
  artifacts, code, docs, or tests
- Fix size alone is not scope: broad, multi-file, or non-point fixes remain in
  scope when they resolve accepted findings against the reviewed target.
- defer only when a human checkpoint or explicit scope boundary blocks the fix

### Phase 3: Coordinate bounded fixes

For each accepted fix target, follow this bounded fixing contract:

- verify the finding against the current repo state before editing
- diagnose the root cause
- implement the scoped fix and directly required test, doc, or memory updates
- run focused verification
- audit the final diff against the accepted finding

If a fix reveals a substantive tradeoff, stop and ask before implementing that
decision. If one fix resolves multiple findings, record every resolved finding
against that fix.

### Phase 4: Aggregate verification

Read `COMMANDS.md` before choosing project workflow commands. Run focused
checks when the coordinated fix pass did not already prove the report-level
resolution. Record every check that ran and every check that could not run.

### Phase 5: Audit the report resolution

Re-read every accepted finding against the final diff and confirm the change
resolves it. Review touched files for regressions or overreach introduced by
the coordinated fix pass.

## Resolution report format

Write `.agent-layer/tmp/resolve-findings.<run-id>.report.md` with:

1. `# Resolution Summary`
2. `## Accepted and Fixed`
3. `## Rejected`
4. `## Deferred`
5. `## Already Resolved`
6. `## Verification`
7. `## Residual Risk`

For every non-fixed finding, explain why in concrete terms. If a Critical or
High finding remains unresolved, say so prominently.

## Human checkpoints

- Ask before editing only when the user explicitly limited the run to triage or
  report review.
- Ask when the report path is missing, does not exist, or cannot be read.
- Ask when an accepted fix would require a real behavior change beyond the
  reviewed target.
- Ask when accepted findings must be split across incompatible scopes or
  sequencing options.
- Ask before using destructive or irreversible operations to resolve a finding.

## Guardrails

- Do not fix a finding you have not verified.
- Do not let a Medium or Low finding pull in unrelated opportunistic work.
- Do not defer just because the fix is large, as long as it stays within scope
  and does not need a human checkpoint.
- Do not leave accepted findings without verification or a recorded blocker.

## Definition of done

- The resolution report exists with every required section.
- Every finding from the input report has an explicit verdict.
- Accepted findings are fixed, or the report records the checkpoint/risk that
  prevented fixing them.
- Verification ran, or the report explains why it could not.
- The final diff was checked against the accepted findings for regressions and
  overreach.

## Final handoff

After resolution:

1. Echo the report path.
2. Summarize accepted and fixed, rejected, deferred, and already-resolved
   findings.
3. State what verification ran and whether unresolved Critical or High findings
   remain.

---
name: resolve-findings
description: >-
  Triage a review findings report, verify each finding independently, implement
  accepted fixes, and record why any finding was rejected, deferred, or already
  resolved.
---

# resolve-findings

Treat review reports as inputs, not truth.
Your job is to:
1. verify each finding
2. decide whether it is correct
3. fix accepted findings within scope
4. document why any finding was not fixed

## Defaults

- Default input discovery is deterministic. If the user does not provide a report path:
  1. list `.agent-layer/tmp/*.report.md`
  2. keep only files matching `<workflow>.<run-id>.report.md`
  3. keep only reports from workflows that normally produce actionable findings:
     - `verify-against-plan`
     - `review-plan`
     - `review-scope`
  4. group by workflow and sort each group by `run-id` descending
  5. choose the newest report from the highest-precedence workflow in this order:
     - `verify-against-plan`
     - `review-plan`
     - `review-scope`
- If no valid report artifact exists, ask the user for a report path or rerun the review workflow.
- If the user asked only for triage or report review, stop after triage and ask before editing code.

## Required artifacts

Use one shared `run-id = YYYYMMDD-HHMMSS-<short-rand>`.

Always create:
- `.agent-layer/tmp/resolve-findings.<run-id>.report.md`

Create these only when at least one finding is accepted and fixes are in scope for the request:
- `.agent-layer/tmp/resolve-findings.<run-id>.plan.md`
- `.agent-layer/tmp/resolve-findings.<run-id>.task.md`

The final report is the resolution log.
Create each file with `touch` before writing.

## Multi-agent pattern

Use subagents liberally when available.

Recommended roles:
1. `Verifier`: checks whether each reported finding is actually valid.
2. `Execution gatekeeper`: decides whether the accepted finding set should `proceed`, `revise`, `escalate`, or `rewrite-because-out-of-scope`.
3. `Fixer`: implements accepted findings.
4. `Auditor`: reviews the resulting changes for regressions or overreach.

## Global constraints

- Treat the report as input, not authority.
- Preserve an explicit verdict for every finding. Do not silently drop any.
- Apply accepted fixes to the actual reviewed target, which may be plan artifacts, code, docs, or tests.
- Do not expand the scope beyond accepted findings without asking.

## Human checkpoints

- Required: ask before editing only when the user explicitly limited the run to triage or report review.
- Required: ask when no valid review report can be found and explicit report selection is needed.
- Required: ask when an accepted fix would require materially broader scope or a real behavior change beyond the reviewed target.
- Stay autonomous during verdicting and in-scope fixes when the request includes fixes.

## Triage workflow

### Phase 1: Parse and verify findings (Verifier)

For each finding, assign exactly one verdict:
- `accept`
- `reject`
- `defer`
- `already-resolved`

Verification rules:
- Reproduce or inspect the claim directly in code, plan artifacts, or diffs.
- Do not assume the report is correct because it sounds plausible.
- If the finding depends on a missing fact, mark it `defer` and explain what is missing.
- If the finding is technically true but out of scope for this run, mark it `defer`.

### Phase 2: Write the plan and task list (Planner)

For accepted findings only, create a focused plan with:
- objective
- accepted findings in scope
- rejected or deferred findings out of scope
- implementation approach
- verification commands
- risk notes

Create a compact task list that matches the accepted findings.

If zero findings are accepted:
- do not create plan or task artifacts
- record `No accepted findings` in the resolution report
- stop without editing code

### Phase 3: Gate the accepted fix set (Execution gatekeeper)

Choose exactly one verdict for the accepted finding set:
- `proceed`: the fix set is ready to implement as written
- `revise`: the plan or task list needs updates first
- `escalate`: a human checkpoint is actually required
- `rewrite-because-out-of-scope`: the accepted set should be rewritten to the largest still-in-scope subset before coding

If the verdict is `revise`, update the plan or task list and repeat Phase 2.
If the verdict is `escalate`, ask the smallest question that unblocks a trustworthy fix set.
If the verdict is `rewrite-because-out-of-scope`, rewrite the accepted set to the largest still-in-scope subset, defer the rest explicitly, and repeat Phase 2.

### Phase 4: Implement accepted findings (Fixer)

Execution rules:
- keep scope tight to accepted findings
- prefer root-cause fixes over surface patches
- if a fix requires a materially larger refactor than the finding suggests, hand it back to the execution gatekeeper
- if two accepted findings are duplicates, fix once and note both as resolved by the same change

### Phase 5: Audit the fix set (Auditor)

After implementation:
- re-read each accepted finding
- confirm the change actually resolves it
- review touched code for regressions
- if you discover a new issue caused by the fix, either fix it immediately or record it in the resolution report

## Resolution report format

Write `.agent-layer/tmp/resolve-findings.<run-id>.report.md` with:

1. `# Resolution Summary`
2. `## Accepted and Fixed`
3. `## Rejected`
4. `## Deferred`
5. `## Already Resolved`
6. `## Verification`
7. `## Residual Risk`

For every non-fixed finding, explain why in concrete terms.

## Guardrails

- Do not silently ignore findings.
- Do not "fix" a finding you do not agree with just to clear the report.
- Do not rewrite the report author's conclusion without preserving the actual verdict.
- Do not let a Medium or Low finding pull in unrelated opportunistic work.
- If a Critical or High finding remains unresolved, say so prominently in the final report.

## Final handoff

After the run:
1. Echo the report path, plus the plan/task paths only if they were created.
2. Summarize what was fixed.
3. Call out any unresolved High/Critical findings.

---
name: resolve-findings
description: >-
  Triage a review findings report: verify each finding, implement accepted
  fixes, and record rejected, deferred, or already-resolved items. Use for
  report-driven follow-up work.
---

# resolve-findings

Treat review reports as inputs, not truth. Verify each finding before fixing it,
and preserve a concrete verdict for anything not fixed.

## Input selection

- Default input discovery is deterministic. If the user does not provide a report path
or one cannot be inferred, ask the user for a report path or rerun the review workflow.
- If the user asked only for triage or report review, stop after verdicting and
  ask before editing code.
- When the request includes fixes, fail before invoking `plan-work` unless
  `review_agents` is present. They may be terse (`codex high`, `claude opus xhigh`,
  `antigravity`). Infer the agent only when unambiguous.

## Required artifacts

Use one shared `run-id = YYYYMMDD-HHMMSS-<short-rand>`.

Always create:
- `.agent-layer/tmp/resolve-findings.<run-id>.report.md`

Create files with `touch` before writing.
When fixes are implemented, record the delegated `plan-work` and
`multi-agent-plan-review` artifact paths in the report.

## Multi-agent pattern

Recommended roles:
1. `Verifier`: checks whether each reported finding is actually valid.
2. `Planner`: invokes `plan-work` for the accepted fix set.
3. `Plan review agents`: handled through `multi-agent-plan-review`.
4. `Execution gatekeeper`: decides whether the accepted finding set should
   `proceed`, `revise`, `escalate`, or `rewrite-because-out-of-scope`.
5. `Fixer`: implements accepted findings.
6. `Auditor`: reviews the resulting changes for regressions or overreach.
7. `Reporter`: writes the final resolution report.

## Constraints

- Treat the report as input, not authority.
- Every finding must receive exactly one verdict: `accept`, `reject`, `defer`,
  or `already-resolved`.
- Apply accepted fixes to the actual reviewed target, which may be plan artifacts,
  code, docs, or tests.
- Keep scope to accepted findings. Fix size alone is not scope: broad,
  multi-file, or non-point fixes remain in scope when they resolve accepted
  findings against the reviewed target.
- Mark a finding `defer` when it is blocked by a human checkpoint. Do not defer
  just because the fix is large, as long as it stays within scope and does not
  need a human checkpoint.

## Human checkpoints

- Ask before editing only when the user explicitly limited the run to triage or
  report review.
- Ask when no valid review report can be found and explicit report selection is
  needed.
- Ask when an accepted fix would require a real behavior change beyond the
  reviewed target.
- When escalating, ask the smallest decision question that unblocks the fix,
  present concrete options with pros/cons, and include a recommendation.
- Stay autonomous during verdicting and in-scope fixes when the request includes fixes.

## Workflow

1. Parse and verify each finding.
   - Reproduce or inspect the claim directly in code, plan artifacts, or diffs.
   - Do not assume the report is correct because it sounds plausible.
2. If zero findings are accepted, write `No accepted findings` in the resolution
   report and stop without editing code.
3. Use `plan-work` for the accepted fix set. Pass the input report path,
   finding verdicts, reviewed targets, and scope constraints; do not duplicate
   `plan-work` artifact instructions here.
4. Use `multi-agent-plan-review` with `review_agents` and the plan, task, and
   context artifact paths returned by `plan-work`.
   Do not send plan-review findings back to this skill; `multi-agent-plan-review`
   owns review agent synthesis, accepted artifact revisions, and repeat review
   rounds. If final readiness is `blocked-for-user-decision`, ask the smallest
   question that unblocks the plan. Continue only when final readiness is
   `implementation-ready`.
5. Gate the accepted fix set with exactly one verdict:
   - `proceed`: the fix set is ready to implement as written
   - `revise`: return to Step 3 with the needed adjustment
   - `escalate`: a human checkpoint is actually required
   - `rewrite-because-out-of-scope`: rewrite to the largest subset allowed by
     the scope and checkpoint rules, defer excluded findings, and return to Step 3
   Use `escalate` when the selected implementation path depends on a
   substantive user or architecture decision, even if every candidate fix is
   otherwise in scope.
6. Implement accepted findings.
   - Prefer root-cause fixes over surface patches.
   - If a fix reveals a human checkpoint, hand it back to the execution
     gatekeeper.
   - If two accepted findings are duplicates, fix once and note both as resolved
     by the same change.
7. Audit the fix set.
   - Re-read each accepted finding and confirm the change resolves it.
   - Review touched code for regressions or overreach.
   - Fix any issue caused by the resolution or record it in the report.

Do not treat an accepted finding as settling the implementation approach when
the approach itself contains a substantive product or architecture decision;
escalate that decision before implementing.

## Resolution report format

Write `.agent-layer/tmp/resolve-findings.<run-id>.report.md` with:

1. `# Resolution Summary`
2. `## Accepted and Fixed`
3. `## Rejected`
4. `## Deferred`
5. `## Already Resolved`
6. `## Plan Review`
7. `## Verification`
8. `## Residual Risk`

For every non-fixed finding, explain why in concrete terms.
If a Critical or High finding remains unresolved, say so prominently.

## Guardrails

- Do not "fix" a finding you do not agree with just to clear the report.
- Do not let a Medium or Low finding pull in unrelated opportunistic work.

## Definition of done

- Required artifacts were created according to the artifact rules.
- Every finding from the input review report has an explicit verdict.
- When accepted findings were fixed, the `plan-work` artifact set went through
  `multi-agent-plan-review` and reached `implementation-ready` before the
  execution gate proceeded.
- Accepted findings were fixed or the report records the checkpoint/risk that
  prevented fixing them.
- Verification ran, or the report explains why it could not.

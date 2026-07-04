---
name: review-plan
description: >-
  Single-agent plan/task/context artifact reviewer. Use only when the user
  explicitly requests review-plan or when another skill explicitly dispatches it.
---

# review-plan

This is the pre-execution plan review skill for a single dispatched
reviewer. Use it only when the user explicitly asks for `review-plan` or when
another skill explicitly dispatches a reviewer to critique a workflow plan,
matching task list, and context file. Use `multi-agent-plan-review` for
top-level plan reviews unless the user specifically requested this skill.

## Defaults

- Default target is the latest valid plan/task/context artifact set under `.agent-layer/tmp/`.
- Produce a report only. Do not edit the plan, task, or context artifacts in this workflow.
- If there is no valid plan/task pair, ask for explicit paths or regenerate the artifacts first.
- If the context file is missing, note its absence as a finding.

## Required artifact

Write the report to:
- `.agent-layer/tmp/review-plan.<run-id>.report.md`

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>`.
Create the file with `touch` before writing.

## Artifact discovery

Use the standard artifact naming rule under `.agent-layer/tmp/`:
- `<workflow>.<run-id>.plan.md`
- `<workflow>.<run-id>.task.md`
- `<workflow>.<run-id>.context.md`

Discovery rules:
1. List `.agent-layer/tmp/*.plan.md`, `.agent-layer/tmp/*.task.md`, and `.agent-layer/tmp/*.context.md`.
2. Keep only files that match the standard naming rule and valid `run-id` shape.
3. Build candidate sets when both `.plan.md` and `.task.md` exist for the exact same `<workflow>` and `<run-id>`. A matching `.context.md` is expected but not required.
4. Select the set with the latest `run-id` in lexicographic order.
5. If the intended set is not the latest valid set, require explicit paths.

Fallback:
- If no valid plan/task pair exists, ask the user for explicit paths or regenerate the plan first.

## Multi-agent pattern

Recommended roles:
1. `Plan reader`: extracts the stated objective, scope, risks, and exit criteria.
2. `Risk reviewer`: looks for missing sequencing, dependencies, and non-goals.
3. `Verification reviewer`: stress-tests the test/doc/memory/update expectations.
4. `Reporter`: writes the findings report and recommendation.

## Global constraints

- Keep the review tied to what the plan actually says, not what you wish it said.
- Produce findings with concrete evidence and exact file references.
- Use an adversarial posture: actively try to falsify the plan, challenge
  assumptions, and look for hidden coupling, edge cases, and failure modes.
  Keep findings evidence-backed; do not invent risks or nitpick wording.
- Do not widen this into a code audit. Use the `review-scope` skill for code, diffs, or repo slices.
- If the plan is ambiguous, say so explicitly instead of guessing intent.

## Human checkpoints

- Required: ask when no valid plan/task pair exists.
- Required: ask when the user intends a non-latest artifact set and explicit paths are needed.
- Optional: ask only when the plan wording is so ambiguous that the review target itself is unclear.
- Stay autonomous for normal critique and report writing.

## Review workflow

### Phase 1: Extract the contract (Plan reader)

From the plan, task, and context artifacts, extract:
- objective
- in-scope items
- explicit non-goals
- sequencing and dependencies
- promised tests or verification
- promised docs or memory updates
- exit criteria
- key files and entry point (from context file)

### Phase 2: Critique the plan structure (Risk reviewer)

Check for:
- missing requirements or non-goals
- hidden large refactors
- dependencies ordered after dependents
- risky assumptions presented as settled
- roadmap, issue, or decision constraints that were missed
- context file gaps: missing key files, stale paths, files listed that do not exist, missing entry point

### Phase 3: Critique the verification and completion bar (Verification reviewer)

Check for:
- weak or missing verification commands
- missing test work for risky changes
- missing docs or memory updates
- exit criteria that are subjective or not actually testable
- task list items that are too large or vague to execute safely

### Phase 4: Record only actionable findings (Reporter)

Each finding must include:
- `Title`
- `Severity`: Critical | High | Medium | Low
- `Location`: exact artifact path and section
- `Why it matters`
- `Evidence`
- `Recommendation`

## Required report structure

The report must contain:

1. `# Plan Review Summary`
   - plan path
   - task path
   - context path (or note if absent)
   - short outcome summary
2. `## Findings`
   - findings first, ordered by severity
3. `## Open Questions`
   - only unresolved items that block confidence
4. `## Strengths`
   - short list of what the plan does well
5. `## Recommendation`
   - `approve`
   - `approve-with-changes`
   - `revise`

## Guardrails

- Do not report vague “needs more detail” complaints without naming what is missing.
- Do not invent implementation problems that are not implied by the plan.
- Do not collapse multiple plan problems into one oversized finding.
- If the task list or context file is missing but the plan exists, call that out explicitly instead of pretending the artifact set is complete.

## Definition of done

- The report exists at `.agent-layer/tmp/review-plan.<run-id>.report.md` with every required section (`Summary`, `Findings`, `Open Questions`, `Strengths`, `Recommendation`).
- Every finding names its artifact path + section, severity, evidence, and specific recommendation — no vague "needs more detail" entries.
- The report ends with exactly one recommendation: `approve`, `approve-with-changes`, or `revise`.
- Plan, task, and context artifacts were not modified by this run.

## Final handoff

After writing the report:
1. Echo the report path.
2. Summarize the top findings in chat.
3. State the recommendation clearly.

---
name: verify-against-plan
description: >-
  Compare the current implementation and working tree against a plan/task-list
  pair, then report completeness gaps, regressions, missing tests or docs, and
  scope drift.
---

# verify-against-plan

This is a completeness review, not just a code audit.
The main question is:

Did the implementation deliver what the plan promised, without missing critical verification, docs, or cleanup?

## Defaults

- Require a plan file and a task file. If they are not supplied, discover the latest matching pair.
- Default implementation target is the current working tree plus the files touched by the task.
- Do not modify code. Produce a report only.

## Artifact discovery

Use the standard artifact naming rule under `.agent-layer/tmp/`:
- `<workflow>.<run-id>.plan.md`
- `<workflow>.<run-id>.task.md`

Discovery rules:
1. List `.agent-layer/tmp/*.plan.md` and `.agent-layer/tmp/*.task.md`.
2. Keep only files matching the standard naming rule with a valid `run-id`.
3. Build candidate pairs only when both files exist for the exact same `<workflow>` and `<run-id>`.
4. Select the pair with the latest `run-id` in lexicographic order.
5. If the intended pair is not the latest valid pair, require explicit paths.

Fallback:
- If no valid plan/task pair exists, ask the user for explicit paths or regenerate the artifacts first.

## Required artifact

Write the report to:
- `.agent-layer/tmp/verify-against-plan.<run-id>.report.md`

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>`.
Create the file with `touch` before writing.

## Multi-agent pattern

Use subagents liberally when available.

Recommended roles:
1. `Plan reader`: extracts promises, scope, and exit criteria.
2. `Implementation reviewer`: compares the code and docs to those promises.
3. `Verifier reviewer`: checks whether the reported validation actually proves completion.

## Global constraints

- Produce a report only. Do not modify the implementation or the plan artifacts.
- Judge completion against what the plan actually promised, not what seems “close enough.”
- Call out missing verification, docs, or memory work explicitly.
- If the plan/task pair is ambiguous, say so rather than guessing.

## Human checkpoints

- Required: ask when no valid plan/task pair exists or the intended pair is not the latest valid pair.
- Required: ask when the implementation target is unclear enough that completeness cannot be judged credibly.
- Optional: ask before broadening the completeness review beyond the planned slice.
- Stay autonomous while comparing the agreed contract to the current implementation.

## Review workflow

### Phase 1: Extract the contract (Plan reader)

From the plan and task artifacts, extract:
- objective
- in-scope items
- out-of-scope items
- promised tests or verification
- promised docs or memory updates
- explicit exit criteria

### Phase 2: Compare contract to implementation (Implementation reviewer)

Check for:
- missing deliverables
- partially completed tasks presented as done
- code that diverges from the stated approach without explanation
- missing or weak tests
- missing docs or memory updates
- scope creep that was not acknowledged

### Phase 3: Review quality of completion (Implementation reviewer + Verifier reviewer)

Even if the plan was followed, check whether the implementation is sound:
- obvious regressions
- broken edge cases
- risky shortcuts
- incorrect or unconvincing verification

### Phase 4: Decide completion status (Synthesizer)

Assign one top-level conclusion:
- `complete`
- `complete-with-follow-up`
- `incomplete`

Use `complete-with-follow-up` only when the planned scope is done and remaining items are clearly outside that scope.

## Required report structure

Write:

1. `# Completion Verdict`
2. `## Inputs`
3. `## Plan Coverage`
   - item-by-item status
4. `## Findings`
   - ordered by severity
5. `## Verification Assessment`
6. `## Docs and Memory Assessment`
7. `## Recommended Next Step`

For every finding, include:
- severity
- location
- why it means the plan is not fully complete or not fully trustworthy
- the smallest corrective action

## Guardrails

- Do not mark work complete just because code exists.
- Do not ignore missing verification.
- Do not confuse scope drift with value-add; drift is still drift.
- If the implementation is better than the plan in a harmless way, note it, but still call out undocumented deviation.

## Final handoff

After writing the report:
1. Echo the report path.
2. State the completion verdict clearly.
3. If incomplete, name the next exact action to take.

---
name: verify-against-plan
description: >-
  Read-only check of current implementation and working tree against a
  plan/task/context set, reporting completeness gaps, regressions, missing
  tests/docs, and scope drift. Use `review-plan` to critique the plan.
---

# verify-against-plan

This is a completeness review, not just a code audit.
The main question is:

Did the implementation deliver what the plan promised, without missing critical verification, docs, or cleanup?

Use `review-plan` instead when the target is the plan itself, not the implementation against it. Use `implement-plan` instead when gaps should be closed rather than just reported.

## Defaults

- Require a plan file and a task file. If they are not supplied, discover the latest matching set. Also load the context file if present.
- Default implementation target is the current working tree plus the files touched by the task.
- Do not modify code. Produce a report only.

## Artifact discovery

Use the standard artifact naming rule under `.agent-layer/tmp/`:
- `<workflow>.<run-id>.plan.md`
- `<workflow>.<run-id>.task.md`
- `<workflow>.<run-id>.context.md`

Discovery rules:
1. List `.agent-layer/tmp/*.plan.md`, `.agent-layer/tmp/*.task.md`, and `.agent-layer/tmp/*.context.md`.
2. Keep only files matching the standard naming rule with a valid `run-id`.
3. Build candidate sets when both `.plan.md` and `.task.md` exist for the exact same `<workflow>` and `<run-id>`. A matching `.context.md` is expected but not required.
4. Select the set with the latest `run-id` in lexicographic order.
5. If the intended set is not the latest valid set, require explicit paths.

Fallback:
- If no valid plan/task pair exists, ask the user for explicit paths or regenerate the artifacts first.

## Required artifact

Write the report to:
- `.agent-layer/tmp/verify-against-plan.<run-id>.report.md`

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>`.
Create the file with `touch` before writing.

## Multi-agent pattern

Required roles:
1. `Plan reader`: extracts promises, scope, and exit criteria from the plan/task/context artifacts as written. Does not consume the implementer's narrative.
2. `Plan-anchored reviewer` (fresh-context subagent): performs the plan-vs-implementation comparison with **no implementer narrative, no prior conversation, and no rationalizations** in scope. See the inputs and prompt contract below.
3. `Verifier reviewer`: checks whether the reported validation actually proves completion.

### Reviewer subagent prompt

Pass the contents of [`reviewer-prompt.md`](reviewer-prompt.md) to the reviewer subagent verbatim — do not paraphrase, summarize, or modify the rubric. Send no implementer narrative, no prior conversation, no chat history. The plan/task/context artifacts ARE in scope — they are what the reviewer compares against. The implementer's narrative ABOUT those artifacts is NOT in scope; including it collapses the comparison into the implementer's own self-grading.

Inputs the reviewer receives alongside the prompt:
- The plan file, the task file, and the context file (if present) — read as artifacts, not as conversation context.
- `git status --porcelain`, the staged/unstaged/untracked file list, and the post-implementation content of the touched files.
- Nothing else. No implementer report (`implement-plan.<run-id>.report.md`), no chat transcript, no deviation justifications.

## Context Discipline

You are the orchestrator. Do not do the child/subagent work yourself. Your job is to preserve your context to make strategic decisions, ensure each child skill or subagent follows its assigned contract, reconcile their outputs, enforce this workflow's gates, and continue the parent workflow after every child return.

## Global constraints

- Produce a report only. Do not modify the implementation or the plan artifacts.
- Judge completion against what the plan actually promised, not what seems “close enough.”
- Call out missing verification, docs, or memory work explicitly.
- If the plan/task pair is ambiguous, say so rather than guessing.

## Human checkpoints

- Required: ask when no valid plan/task pair exists or the intended set is not the latest valid set.
- Required: ask when the implementation target is unclear enough that completeness cannot be judged credibly.
- Optional: ask before broadening the completeness review beyond the planned slice.
- When a checkpoint involves a genuine tradeoff between substantive alternatives, present at least two options with brief pros and cons, state which you recommend and why, and let the human decide.
- Stay autonomous while comparing the agreed contract to the current implementation.

## Review workflow

### Phase 1: Extract the contract (Plan reader)

From the plan, task, and context artifacts, extract:
- objective
- in-scope items
- out-of-scope items
- promised tests or verification
- promised docs or memory updates
- explicit exit criteria
- key files and entry point (from context file, if present)

### Phase 2: Compare contract to implementation (Plan-anchored reviewer)

Invoke the plan-anchored reviewer subagent with the contents of `reviewer-prompt.md` and the inputs listed above. The subagent runs with fresh context — no implementer narrative, no prior conversation. It receives only the plan/task/context artifacts and the post-implementation working-tree state, and returns JSON-line findings.

The reviewer covers:
- missing deliverables
- partially completed tasks presented as done
- code that diverges from the stated approach without explanation
- missing or weak tests
- missing docs or memory updates
- scope creep that was not acknowledged

Collect the reviewer's findings under the report's `## Findings` section. Do **not** filter findings by re-reading the implementer's narrative — that defeats the fresh-context contract.

### Phase 3: Review quality of completion (Verifier reviewer)

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

## Definition of done

- The report exists at `.agent-layer/tmp/verify-against-plan.<run-id>.report.md` with every required section (`Completion Verdict`, `Inputs`, `Plan Coverage`, `Findings`, `Verification Assessment`, `Docs and Memory Assessment`, `Recommended Next Step`).
- `Plan Coverage` lists every in-scope plan item with an item-by-item status; partial completions are not presented as done.
- Phase 2 used the plan-anchored fresh-context reviewer subagent; findings reflect that reviewer's verdicts, not a re-grading by the orchestrator after consulting implementer narrative.
- The report carries exactly one verdict: `complete`, `complete-with-follow-up`, or `incomplete`; `complete-with-follow-up` is used only when remaining items are clearly outside planned scope.
- Implementation, plan, task, and context artifacts were not modified by this run.

## Final handoff

After writing the report:
1. Echo the report path.
2. State the completion verdict clearly.
3. If incomplete, name the next exact action to take.

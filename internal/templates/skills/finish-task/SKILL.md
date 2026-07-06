---
name: finish-task
description: >-
  Close out finished work by checking plan alignment, updating only necessary
  memory/docs, running credible verification, and summarizing outcome. Use
  `/ship-pr` for PR work or `/verify-work` for read-only checks.
---

# finish-task

This is the closeout workflow after implementation.
It is not a broad audit and it is not a second planning pass.

## Defaults

- Default scope is the current working tree and the files touched by the just-completed task.
- Use the current run's plan and task artifacts when they are already known.
- Default verification depth is the fastest credible repo-defined check.
- Update memory only where the completed task actually changed project truth.

## Multi-agent pattern

Recommended roles:
1. `Change reviewer`: compares the delivered work to the intended scope.
2. `Memory curator`: updates only the affected memory files.
3. `Verifier`: runs the best fast checks available.
4. `Reporter`: writes the final closeout summary.

## Global constraints

- Keep scope tight to the completed task and its nearby context.
- Do not start a new broad audit in this workflow.
- Do not invent plan alignment if no plan artifact exists.
- Keep memory updates compact, deduplicated, and truthful.

## Closeout workflow

### Phase 0: Preflight (Change reviewer)

1. Confirm baseline with:
   - `git status --porcelain`
   - `git diff --stat`
2. Read `COMMANDS.md` before choosing verification commands.
3. Determine the review scope:
   - explicit user scope first
   - otherwise current uncommitted task changes

### Phase 1: Check plan alignment (Change reviewer)

If a plan artifact is available:
- compare promised work to actual changes
- record completed items
- record meaningful deviations and omissions

If no plan artifact exists, state that explicitly and continue.

### Phase 2: Curate memory updates (Memory curator)

Use the authoritative files only as needed:
- remove resolved items from `ISSUES.md`
- remove implemented items from `BACKLOG.md`
- update `ROADMAP.md` only when status genuinely changed
- update `DECISIONS.md` only for non-obvious, durable choices

Merge near-duplicates instead of appending noise.
Before adding a DECISIONS.md entry, search existing decisions for the same
subsystem or decision axis. Update or replace an existing entry when possible.
If the new or changed decision supersedes older entries, consolidate them in
the same closeout pass instead of leaving a decision chain behind.

### Phase 3: Verify the delivered slice (Verifier)

1. Run the fastest credible repo-defined verification command.
2. Escalate to broader checks only when the risk of the completed work warrants it.
3. If no credible verification command exists, report that limitation explicitly.

### Phase 4: Summarize the task closeout (Reporter)

Report:
- what changed
- whether the plan was completed
- what memory was updated
- what verification ran
- what remains deferred

## Guardrails

- Do not log speculative future work that was not actually observed.
- Do not add routine implementation details to `DECISIONS.md`.
- Do not append a DECISIONS.md entry while leaving an older superseded decision in place.
- Do not mark roadmap work complete without evidence in code, docs, or tests.
- Do not skip verification silently.

## Definition of done

- A credible repo-defined verification command was run and its result is included in the final handoff; if no such command exists, that limitation is stated explicitly.
- Memory files were touched only where the completed task actually changed project truth; no speculative or duplicate entries were added.
- If DECISIONS.md was updated, superseded or near-duplicate decisions in the
  same subsystem were consolidated or the final handoff explains why none were
  present.
- If a plan artifact existed, its deliverables are compared item-by-item; if not, the final handoff states that explicitly.

## Final handoff

After closeout:
1. Summarize the task outcome and any memory updates made.
2. State the verification run and result, plus any deferred risk or follow-up.

---
name: finish-task
description: >-
  Close out finished work by checking plan alignment, updating only necessary
  memory/docs, reusing or gathering credible verification, and summarizing the
  outcome. Do not use for PR delivery or read-only completion review.
---

# finish-task

Close the completed task once. This is not a new planning, review, or broad
audit stage.

## Inputs and scope

- Use explicit scope first; otherwise use the current task's changed files.
- Reuse known plan, task, context, and verification artifacts when provided.
- Keep review, documentation, and memory updates limited to truths changed by
  this task.

## Workflow

### 1. Confirm completion scope

Record the delivered files and intended outcome. If a plan exists, compare its
deliverables to the final work once and record material completion,
deviations, or omissions. If no plan exists, state that and use the user's task
as the contract.

### 2. Update affected project truth

Update only what the completed work made stale:

- remove resolved ISSUES.md entries
- remove implemented BACKLOG.md entries
- update ROADMAP.md only when status changed
- update or consolidate DECISIONS.md only for a non-obvious durable decision
- correct affected documentation when the delivered behavior changed its truth

Read each memory file's format before editing. Merge duplicates and replace
superseded decisions instead of appending a historical chain. Do not launch a
general memory or documentation audit.

### 3. Establish verification evidence

Reuse existing passing evidence when it is credible and covers the exact
current tree and delivered scope. Otherwise read COMMANDS.md and run one
risk-proportional repository-defined verification lane.

If verification fails, report the concrete failure and return the task as
incomplete. Do not create another implementation or review loop inside this
closeout skill.

### 4. Report and yield

Return:

- delivered outcome and plan alignment
- documentation or memory updates
- reused or newly gathered verification evidence
- material deviations, blockers, or deferred work
- terminal status: `complete` | `incomplete` | `blocked-user-decision`

## Guardrails

- Do not log speculative work or routine implementation details.
- Do not mark roadmap work complete without evidence.
- Do not rerun verification merely for greater confidence when current passing
  evidence already covers the final tree.
- Do not stage, commit, push, or ship.

## Definition of done

- The authoritative task contract was checked once against the delivered work.
- Only documentation and memory made stale by the task were updated.
- Current credible verification evidence is cited, gathered once when needed.
- The skill returns its terminal status and yields.

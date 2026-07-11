# write-plan

Write three implementation-ready artifacts: a narrative plan, a small ordered
task list, and an implementation context file.

Use the smallest coherent reviewable scope. Scale detail to the work's risk and
ambiguity; do not add filler, generic background, or exhaustive file lists. For
roadmap execution without a named phase, use the first incomplete roadmap phase.

## Required artifacts

Use the standard artifact naming rule under `.agent-layer/tmp/`:

- `.agent-layer/tmp/write-plan.<run-id>.plan.md`
- `.agent-layer/tmp/write-plan.<run-id>.task.md`
- `.agent-layer/tmp/write-plan.<run-id>.context.md`

Use one shared `run-id = YYYYMMDD-HHMMSS-<short-rand>`.

## Planning workflow

### Phase 1: Preflight

1. Normalize the source into the planning contract:
   - source type and planning target
   - objective and desired outcome
   - acceptance criteria or observable success signals
   - explicit constraints, non-goals, and user-provided requirements
   - source evidence or references the plan must preserve
   - unknowns that must be resolved before drafting an implementation-ready
     plan
2. Read only the files needed to understand the target area. Avoid broad repo
   scans unless the request truly demands it.
3. For architectural, roadmap, issue, or backlog work, read the relevant memory
   files before committing to a plan.
4. Resolve remaining material facts from repository evidence before drafting.
   If required evidence is unavailable, name the concrete evidence blocker; do
   not turn it into a user decision. Use `escalate` only for a choice that meets
   the human-checkpoint standard.

### Phase 2: Draft the plan

Write the plan file with these sections:

1. `# Objective`
   - what will change
   - what will not change
   - what success looks like for a user or maintainer
2. `## Scope`
   - in-scope work
   - out-of-scope work
   - assumptions
3. `## Context`
   - key files, modules, or docs involved
   - source type and source evidence or references
   - any relevant roadmap or decision constraints
   - user-confirmed decisions that settled substantive choices, if any
4. `## Approach`
   - the intended design or execution path
   - rationale only where the choice is consequential or non-obvious
5. `## Material Risks`
   - behavior regressions
   - migration or compatibility concerns
   - unclear dependencies
6. `## Verification`
   - repository-defined commands or other credible evidence required
   - timing only when it matters
   - what the evidence proves
7. `## Exit Criteria`
   - objective conditions that define completion

Use the human checkpoint standard below while drafting. Ask before committing
to an approach that requires a user decision.

### Phase 3: Draft the task list

The task file should be a compact ordered Markdown checkbox list that mirrors
the implementation work in the plan. Keep items small and verifiable, group by
execution order, and include directly required tests, docs, memory updates, and
implementation-time checks. Keep final verification requirements in the
plan's `## Verification` section for the verification stage; do not duplicate
them as implementation tasks.

### Phase 4: Draft the context file

The context file must include these sections:

1. `# Implementation Context`
   - one-sentence summary of what this plan changes and why
2. `## Key Files`
   - relative file paths with a brief description of each file's role
3. `## Current State`
   - how the relevant code or system behaves before this plan is applied
4. `## Constraints`
   - non-obvious facts, dependencies, or invariants discovered during planning
5. `## Entry Point`
   - where the implementing agent should start reading and why

Use relative paths, mark new files explicitly, keep descriptions brief, and do
not duplicate the plan's narrative or include generic best practices.

### Phase 5: Self-check and handoff

Before presenting the artifacts, check:

- scope, non-goals, constraints, acceptance criteria, and source evidence match
  the request
- dependencies, verification, docs, tests, and memory updates are complete for
  the risk level
- context paths are valid or marked as new, and the entry point is clear
- user-owned decisions are recorded as confirmed decisions or the verdict is
  `escalate`
- no artifact defers material investigation or approach selection; state any
  residual uncertainty precisely instead of hiding it behind vague confidence

Correct any evidence-backed inconsistency in this drafting pass. Then choose
one handoff verdict:

- `proceed`: the artifact set is ready for review
- `revise`: a concrete, evidence-backed material gap remains and can be
  corrected without user input; cite the exact artifact location, evidence,
  and required correction
- `escalate`: a choice that materially affects behavior, architecture, scope,
  risk, or cost cannot be settled by available evidence and therefore requires
  the user

`revise` is an autonomous correction signal, not a human checkpoint. Apply the
cited corrections as one focused revision within the same planning stage and
rerun only this self-check. Resolve any remaining concrete, autonomously
correctable material gap, then proceed. Do not restart research, drafting, or
plan review merely for greater confidence.

## Final handoff

After writing the artifacts:

1. Echo all three artifact paths.
2. Summarize the plan in a few sentences.
3. State the handoff verdict and any material risk or open question.

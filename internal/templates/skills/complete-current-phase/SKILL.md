---
name: complete-current-phase
description: >-
  Complete one roadmap phase through one reviewed plan, bounded implementation
  packages, cleanup, verification, concrete-work review, and closeout.
---

# complete-current-phase

Complete the selected roadmap phase without turning each internal package into
its own planning and review workflow. The phase is the contract; packages are
execution boundaries.

## Inputs and scope

- Use the first incomplete roadmap phase unless the user names a phase.
- Include every unchecked task and exit criterion in that phase, plus only
  prerequisites that directly block completion.
- Require `plan_reviewers` before side effects and pass them unchanged to
  `/plan-work`.
- Use `/plan-work` instead when the request is planning-only.

Do not widen the skill to later phases, the whole backlog, or unrelated issues.
If the phase is large, split it into ordered, independently understandable
packages while preserving phase-level done criteria.

## Required agent boundaries

- Use one fresh built-in scout subagent to map the phase, remaining tasks,
  prerequisites, and package boundaries.
- Use the requested dispatch reviewers through `/plan-work`.
- Use one fresh built-in gatekeeper subagent after planning to decide whether
  the phase plan is ready to execute.
- Use fresh built-in implementation subagents for distinct packages or
  subsystems. A narrow package may run `/implement-plan` in the current context.
- Use one fresh built-in subagent for the phase-level `/verify-work` pass.

Preserve these context boundaries. The orchestrator owns scope, sequencing,
artifacts, decisions, and terminal handoff rather than delegated implementation.

## Decision and repetition rules

- Ask only when evidence leaves multiple viable choices with materially
  different behavior, architecture, phase scope, risk, cost, or sequencing.
- Resolve routine package boundaries, implementation details, and verification
  details autonomously.
- Plan and review the phase once by default. Revise completed artifacts only
  when concrete implementation evidence invalidates scope, dependencies,
  architecture, or acceptance criteria.
- Do not repeat a review or verification merely because another pass could find
  something.

## Workflow

### 1. Map the phase once

Read ROADMAP.md first, then relevant DECISIONS.md, ISSUES.md, BACKLOG.md,
COMMANDS.md, and repository evidence. Have the scout return:

- selected phase and phase-level done criteria
- every remaining in-phase task
- direct prerequisites
- ordered package boundaries and dependencies
- any user-owned ambiguity

If the phase is already complete in the current tree, proceed to closeout with
the evidence. Do not manufacture implementation work.

### 2. Produce one reviewed phase plan

Run `/plan-work` once for completion of the entire selected phase, including all
packages and phase-level verification:

```text
/plan-work
{selected roadmap phase and scout evidence}
plan_reviewers are {agent 1, agent 2, ...}
```

Continue only with `implementation-ready`, or ask the exact decision returned
by planning. Do not create a new plan for each package.

### 3. Gate execution once

Give the fresh gatekeeper the phase contract, reviewed artifacts, package map,
and current repository evidence. It returns exactly one verdict:

- `proceed`: execute the packages
- `revise`: name concrete artifact evidence that must be corrected before work
- `blocked-user-decision`: name the smallest material decision

Apply a concrete `revise` correction directly to the artifacts. Do not run
another reviewer pass unless the correction materially changes the contract.

### 4. Implement every package

Run `/implement-plan` once per package against the shared phase artifacts and
that package's bounded task subset. Use fresh built-in subagents for distinct
packages where useful, but serialize working-tree mutations against the latest
tree.

Each package must return completed scope, deviations, task-local checks, and a
blocker or readiness for phase verification. Update the shared task artifact as
packages complete. Do not stop after one package while in-phase work remains.

If concrete evidence invalidates the remaining plan, return only to the
earliest responsible stage. Package completion by itself is not a reason to
replan or re-review.

### 5. Clean the combined implementation once

After all packages are implemented, run `/clean-and-fix-code` once over the
combined uncommitted work. Directly address its accepted findings under that
skill's contract.

### 6. Verify the phase once

Run `/verify-work` once in a fresh built-in subagent against the original phase
plan, task, and context. Include cleanup `resolved_findings` as supplemental
obligations.

If verification is incomplete, validate and directly address every material
in-scope finding. Use focused evidence for each repair; do not rerun
`/verify-work` or reopen planning unless the finding proves the contract itself
is invalid.

### 7. Review the concrete phase result once

Run `/review-uncommitted-code` once over the delivered phase files, tests, docs,
and relevant surrounding boundaries. Validate and directly fix every
`Recommended Accept` finding. Use focused evidence for the final tree and do not
run another broad review.

### 8. Close the phase

Run `/finish-task` once with the phase contract, artifact paths, package
results, verification report, review report, and final focused evidence. It
owns necessary ROADMAP.md, memory, and documentation updates.

If closeout exposes a concrete incomplete contract item, return to its
responsible stage. Otherwise yield with the terminal result.

## Completion contract

Return:

- selected phase and terminal status: `complete` or `blocked`
- plan, task, context, verification, and review report paths
- packages completed and material deviations
- cleanup, verification, accepted-review repairs, and final evidence
- memory and documentation updates
- the smallest unresolved user decision or concrete blocker, if any

The phase is complete only when every in-phase task and exit criterion is
evidence-backed and closeout succeeds. The workflow plans, verifies, and broadly
reviews once by default, then yields.

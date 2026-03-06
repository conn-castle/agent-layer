---
name: complete-current-phase
description: >-
  Drive the current incomplete roadmap phase through planning, plan review,
  implementation, verification against plan, broader audit, cleanup/fix loops,
  and closeout until the selected roadmap phase is fully complete or a real
  blocker requires human input.
---

# complete-current-phase

This is the orchestrator skill for roadmap execution.
It should run an iterative loop that:
- inventories the whole current roadmap phase
- plans the phase to completion, using internal work packages when needed
- stress-tests the plan
- implements one work package at a time when the roadmap and plan are clear enough
- checks completeness against the plan and the remaining phase tasks
- audits the resulting code
- cleans up local mechanical complexity when that improves the touched code
- fixes accepted findings
- repeats until every task in the selected phase is complete or a real blocker requires human input

Use the current active roadmap phase by default, meaning the first incomplete phase.
Do not jump ahead to a later incomplete phase unless the user explicitly names it.
Use the `plan-work` skill instead when the user wants only the planning step without the full execution loop.

## Scope default

Do not interpret this skill as "implement the whole backlog" or "fix every issue in the repository" unless the user explicitly says so and the scope is realistic.

Default scope:
- the first incomplete roadmap phase
- every unchecked task inside that phase
- plus any small prerequisite issues directly blocking phase completion

If the active phase is too large to complete safely in one implementation pass:
- decompose it into ordered internal work packages
- keep every package inside the current phase rather than jumping ahead
- keep iterating until all in-phase tasks are done
- ask the user only when the phase cannot be decomposed without high risk, hidden dependencies, or guesswork

Roadmap phases should normally be distinct enough that this decomposition is straightforward and rarely needs escalation.

## Inputs

Read in this order when they exist:
1. `ROADMAP.md`
2. `DECISIONS.md`
3. `ISSUES.md`
4. `BACKLOG.md`
5. `COMMANDS.md`
6. `README.md`

If the user specifies a phase number, use that phase instead of the first incomplete one.

## Required behavior

Use subagents liberally when available.

At minimum, use:
- a scout/planner subagent
- parallel review subagents with different lenses
- one or more implementation subagents when the work spans distinct files or subsystems

## Global constraints

- Do not interpret this workflow as blanket approval to implement.
- Keep scope to the selected roadmap phase plus directly blocking prerequisite issues only.
- Internal work packages are execution mechanics, not reduced done criteria.
- Use dedicated skills for each phase when they exist instead of improvising a parallel workflow.
- Escalate whenever ambiguity, broadening scope, or non-converging review loops make the next step non-obvious.
- If new evidence invalidates an earlier assumption, jump back to the earliest affected phase instead of continuing forward on stale assumptions.
- Do not stop after a single package if unchecked tasks still remain in the selected phase.

## Human checkpoints

- Required: ask when the selected phase boundary is ambiguous, the roadmap task is ambiguous, or a required fact is unknown.
- Required: ask only when the current phase cannot be decomposed into safe ordered work packages without high-risk sequencing or guesswork.
- Required: ask when review or audit loops stop converging and escalation is the higher-value move.
- Required: ask only when the roadmap and phase-completion plan are not clear enough to proceed without guessing.
- Stay autonomous within normal plan-review, implementation, and audit loops when the selected phase and current work package are clear.

## Orchestration loop

### Phase 1: Select the phase and map the remaining work (Phase Scout)

1. Identify the current active roadmap phase:
   - use the first incomplete phase by default
   - use a later phase only when the user explicitly names it
2. Inventory every unchecked task and explicit exit criterion inside that phase.
3. Pull in blocking issues only when they are necessary prerequisites.
4. Decide whether the phase should be executed as one pass or as multiple internal work packages.
5. State the selected phase, the remaining tasks, and the proposed package boundaries before proceeding.

If Phase 1 shows that the current phase is not reasonably decomposable:
- do not jump ahead
- ask the user the smallest question needed to split, clarify, or reframe the current phase
- recommend tightening the phase boundary when the roadmap convention is the real problem

### Phase 2: Plan the phase to completion (Planner)

Use the `plan-work` skill, but explicitly instruct it to plan completion of the selected phase rather than only the next work package.
Create:
- a plan artifact
- a task-list artifact

The plan must define:
- all remaining in-phase tasks
- ordered internal work packages when more than one is needed
- objective
- scope and non-goals
- risks
- verification
- exit criteria

The plan must make the phase-level done criteria explicit and identify which work package should execute first.

### Phase 3: Review the plan (Plan reviewers)

Use the `review-plan` skill on the plan and task artifacts.

If findings exist:
- use the `resolve-findings` skill to triage them
- revise the plan or task list as needed

Loop back to plan review when either is true:
- an unresolved Critical or High finding remains
- the plan changed materially

Recommended cap: no more than 3 plan-review loops before escalating to the user with the blocker.

### Phase 4: Readiness checkpoint (Reporter)

Before moving into implementation:
1. summarize the selected phase
2. summarize the remaining phase tasks, the current plan, and the next work package
3. call out unresolved risks and any deferred findings

If the roadmap, plan, and open risks are clear enough, continue.
If they are not clear enough, ask the user the smallest question that unblocks the next step.

### Phase 5: Implement the current work package (Implementers)

Use the `implement-plan` skill with the current plan and task list.

Execution rules:
- stay inside the selected roadmap phase
- complete the current work package end-to-end before moving on
- update tests, docs, and memory as required by the work
- record deviations rather than hiding them
- if the current package reveals additional in-phase tasks or dependency changes, update the plan and task list before continuing

If implementation leaves obvious local mechanical complexity, dead scaffolding, or oversized touched files that can be improved without broadening scope:
- use the `mechanical-cleanup` skill
- then continue to Phase 6

### Phase 6: Review against the plan (Completeness reviewers)

Use the `verify-against-plan` skill.

If the verdict is `incomplete`, return to implementation.
Repeat until:
- the verdict is `complete` or `complete-with-follow-up`
- or a real blocker requires human input

Recommended cap: no more than 3 implement/review loops before escalating.

### Phase 7: Broad audit of the delivered work package (Audit reviewers)

Use the `review-scope` skill on the actual implementation:
- touched files
- relevant surrounding modules
- tests and docs that changed

The audit should look for:
- correctness issues
- architecture problems
- reliability or performance risks
- missing docs/tests
- maintainability problems

### Phase 8: Fix audit findings (Fixers + Auditors)

Use the `resolve-findings` skill.

If accepted Critical or High findings were fixed, run one more `review-scope` pass on the touched scope.
Repeat the audit/fix loop only when the new report still contains unresolved Critical or High findings.

If the fixes introduce or expose local cleanup work that remains behavior-preserving and in-scope:
- use the `mechanical-cleanup` skill
- then return to Phase 6

Count every return to Phase 6 after Phase 7 begins, including cleanup-triggered returns, toward the same loop cap.

Recommended cap: no more than 2 Phase 6-8 review/audit loops for the same work package before escalating.

### Phase 9: Reassess phase status (Reporter)

1. Update roadmap and task status for the work that just landed.
2. Compare the remaining unchecked phase tasks against the phase-completion plan.
3. If unchecked phase tasks remain:
   - refresh the plan and task list when needed
   - select the next internal work package
   - return to Phase 3 or Phase 4, whichever is earliest affected
4. Proceed to closeout only when every task in the selected phase is complete and backed by evidence.

### Phase 10: Close the phase (Memory Curator + Reporter)

When the selected roadmap phase is done:
- update roadmap status if appropriate
- remove resolved issues from `ISSUES.md`
- update `DECISIONS.md` only for non-obvious durable decisions
- update `COMMANDS.md` if new repeatable commands were discovered
- check whether `README.md` or other markdown docs now need updates

Use the `finish-task` skill as the final cleanup pass.
If it reveals incomplete work or stale memory/docs, jump back to the earliest affected phase instead of closing the phase.

## Stop conditions

Pause and ask the human when:
- the roadmap task is ambiguous
- the correct fix requires materially broader scope than the selected phase
- repeated review loops do not converge
- required environment, schema, or product behavior is unknown
- a destructive or irreversible action would be needed

## Minimal status protocol

At each major stage, echo the current artifact path(s), identify the active phase and work package, and state one of:
- mapping the phase
- planning the phase
- fixing plan findings
- implementing the current package
- reviewing the current package against plan
- auditing the current package
- fixing audit findings
- reassessing phase status
- closing the phase

## Guardrails

- Do not silently skip review loops.
- Do not silently downscope from the selected phase to a single slice.
- Do not mark a work package complete without evidence.
- Do not mark the phase complete while unchecked phase tasks remain.
- Do not stop after finishing only the current work package.
- Do not turn the skill into an unbounded autonomous backlog sweeper.
- Do not carry unresolved Critical/High findings forward without calling them out.
- Keep each loop grounded in concrete artifacts and observed verification.

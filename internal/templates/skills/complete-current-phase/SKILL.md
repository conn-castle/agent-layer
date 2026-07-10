---
name: complete-current-phase
description: >-
  Complete the current roadmap phase through planning, review, implementation,
  verification, audit, cleanup, and closeout. Do not use for planning-only
  requests or generic non-roadmap closeout.
---

# complete-current-phase

This is the orchestrator skill for roadmap execution. It iteratively plans, implements, reviews, audits, and fixes one roadmap phase until every task in the selected phase is complete or a real blocker requires user input.

Use the current active roadmap phase (the first incomplete phase) by default.
Do not jump ahead to a later phase unless the user explicitly names it.
Use `/plan-work` instead when the user wants only the planning step.

## Scope default

Do not interpret this skill as "implement the whole backlog" or "fix every issue in the repository" unless the user explicitly says so and the scope is realistic.

Default scope:
- the first incomplete roadmap phase
- every unchecked task inside that phase
- plus any small prerequisite issues directly blocking phase completion

If the user specifies a phase number, use that phase instead of the first
incomplete one.

If the active phase is too large to complete safely in one implementation pass:
- decompose it into ordered internal work packages
- keep every package inside the current phase rather than jumping ahead
- keep iterating until all in-phase tasks are done
- ask the user only when the phase cannot be decomposed without high risk, hidden dependencies, or guesswork

Roadmap phases should normally be distinct enough that this decomposition is straightforward and rarely needs escalation.

## Required behavior

Fail before side effects unless `plan_reviewers` is present. Values may be terse
(`codex high`, `claude opus xhigh`, `antigravity`). Infer the agent only when
unambiguous.

At minimum, use:
- a scout/planner subagent
- plan reviewer dispatch roles through `/plan-work`
- an execution gatekeeper subagent that decides `proceed`, `revise`, `escalate`, or `rewrite-because-out-of-scope`
- one or more implementation subagents when the work spans distinct files or subsystems

## Context preservation

You are the orchestrator for this skill. Do not do work that belongs to
subagents or delegated skills in the orchestration context. Preserve your
context to make strategic decisions, enforce gates, reconcile returned outputs,
and continue this skill's workflow after every delegation returns.

## Compaction guidance

When compaction is needed, retain this entire skill verbatim. Also preserve the
current workflow step or phase, active artifact paths, selected scope, pending
gate verdicts, delegated skills and subagents already run and their outcomes,
unresolved blockers or user checkpoints, and the next exact step.

## Global constraints

- Do not interpret this workflow as blanket approval to implement.
- Keep scope to the selected roadmap phase plus directly blocking prerequisite issues only.
- Internal work packages are execution mechanics, not reduced done criteria.
- Treat execution gating as an internal readiness decision, not as a cue to ask the user unless a human checkpoint is actually triggered.
- Use dedicated skills for each phase when they exist instead of improvising a parallel workflow.
- Escalate whenever ambiguity, broadening scope, or non-converging review loops make the next step non-obvious.
- If new evidence invalidates an earlier assumption, jump back to the earliest affected phase instead of continuing forward on stale assumptions.
- If the gatekeeper returns `rewrite-because-out-of-scope`, rewrite the current package or plan to fit the selected phase instead of stopping.
- Do not stop after a single package if unchecked tasks still remain in the selected phase.

## Human checkpoints

- Ask when roadmap evidence leaves multiple viable phase boundaries or task
  interpretations and choosing among them would change what work is considered
  complete.
- Ask when the selected phase cannot be decomposed into safe ordered work
  packages without high-risk sequencing or guesswork.
- Ask when review or audit loops stop converging and the next step depends on a
  user-level scope, risk, or priority decision.
- Stay autonomous within normal plan-review, implementation, and audit loops
  when the selected phase and current work package are clear.

## Orchestration loop

### Phase 1: Select the phase and map the remaining work (Phase Scout)

Read in this order when they exist:

1. `ROADMAP.md`
2. `DECISIONS.md`
3. `ISSUES.md`
4. `BACKLOG.md`
5. `COMMANDS.md`
6. `README.md`

Then:

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

### Phase 2: Plan And Review The Phase To Completion (Planner)

Plan selected phase completion, not just the next work package. The plan must
define remaining in-phase tasks, ordered internal work packages when needed,
phase-level done criteria, and which work package executes first:

```text
/plan-work
{selected roadmap phase}
plan_reviewers are {agent 1, agent 2, ...}
```

### Phase 3: Confirm Plan Readiness (Plan Reviewers)

Do not send plan-review findings into the audit-fix loop; `/review-plan` owns
plan reviewer synthesis, accepted artifact revisions, and repeat review rounds
inside `/plan-work`.

If final readiness is `blocked-for-user-decision`, ask the smallest question
that unblocks the plan. Continue only when final readiness is
`implementation-ready`.

### Phase 4: Gate the next execution step (Execution gatekeeper + Reporter)

Before moving into implementation or advancing to the next package:
1. summarize the selected phase, remaining tasks, current plan, and next work package
2. call out unresolved risks and any deferred findings
3. choose exactly one verdict:

- `proceed` (ready to execute as written): continue immediately to Phase 5. Do not pause to ask the user for confirmation — this verdict already means readiness is confirmed.
- `revise` (artifacts need updates first): update the plan or task artifacts and return to Phase 3.
- `escalate` (human checkpoint required): ask the user the smallest question that unblocks the next step.
- `rewrite-because-out-of-scope` (package does not fit selected phase): rewrite to stay inside the selected phase, record deferrals, and return to the earliest affected phase.

### Phase 5: Implement the current work package (Implementers)

Run:

```text
/implement-plan
Plan artifacts:
{relative path to current plan artifact}
{relative path to current task artifact}
{relative path to current context artifact}
```

Stay inside the selected roadmap phase and complete the current work package
end-to-end before moving on. If the package reveals additional in-phase tasks or
dependency changes, update the plan and task list before continuing.

If implementation leaves obvious local complexity that can be improved without
broadening scope, run:

```text
/clean-and-fix-code
plan_reviewers are {agent 1, agent 2, ...}
```

### Phase 6: Review against the plan (Completeness Review Agents)

Run a built-in subagent with:

```text
/verify-work
Plan artifacts:
{relative path to current plan artifact}
{relative path to current task artifact}
{relative path to current context artifact}
```

If the verdict is `incomplete`, return to implementation.
Repeat until the verdict is `complete` or `complete-with-follow-up`, or a real blocker requires user input.

### Phase 7: Broad audit of the delivered work package (Audit Review Agents)

Use the `/review-uncommitted-code` skill on the touched files, surrounding modules, and changed tests/docs.

### Phase 8: Fix audit findings (Fixers + Auditors)

Use the Phase 7 review report as input. Fix every `Recommended Accept` finding
regardless of severity after verifying it against the current repo state.

For accepted findings:
- group duplicate or tightly coupled findings into one bounded fix target
- keep unrelated findings separate when they can be fixed independently
- diagnose the root cause
- implement the scoped fix and directly required test, doc, or memory updates
- run focused verification
- audit the final diff against the accepted finding

If accepted Critical or High findings were fixed, run one more `/review-uncommitted-code` pass on the touched scope.
Repeat the audit/fix loop only when the new report still contains unresolved Critical or High findings.

If the fixes introduce or expose local complexity that remains
behavior-preserving and in-scope, run:

```text
/clean-and-fix-code
plan_reviewers are {agent 1, agent 2, ...}
```

Then return to Phase 6.

Count every return to Phase 6 after Phase 7 begins, including cleanup-triggered returns. Escalate if the loop is not converging.

### Phase 9: Reassess phase status and gate the next package (Execution gatekeeper + Reporter)

1. Update roadmap and task status for the work that just landed.
2. Compare the remaining unchecked phase tasks against the phase-completion plan.
3. Choose exactly one verdict:

- `proceed` (current package done, next step clear): if unchecked tasks remain, select the next work package and return to Phase 4 immediately. Do not pause to ask the user — proceed means continue.
- `revise` (plan should be refreshed): update the plan and task list and return to Phase 3.
- `escalate` (human checkpoint required): ask the user the smallest question that unblocks the next step.
- `rewrite-because-out-of-scope` (remaining packages drift from selected phase): rewrite package boundaries, record deferrals, and return to the earliest affected phase.

4. Proceed to closeout only when every task in the selected phase is complete and backed by evidence.

### Phase 10: Close the phase (Memory Curator + Reporter)

Use the `/finish-task` skill as the final cleanup pass, including roadmap status, memory file updates, and doc checks.
If it reveals incomplete work or stale memory/docs, jump back to the earliest affected phase instead of closing the phase.

## Minimal status protocol

At each major stage, echo the current artifact path(s), identify the active phase and work package, and state one of:
- mapping the phase
- planning the phase
- fixing plan findings
- gating the next step
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

## Definition of done

- Every unchecked task in the selected roadmap phase is checked off in `ROADMAP.md`, backed by observed code, test, or doc evidence.
- Each internal work package ran the full /plan-work, /implement-plan,
  /verify-work, /review-uncommitted-code, accepted-finding fix loop, and no unresolved Critical or High
  findings remain at phase close.
- The `/finish-task` skill ran as the closeout pass, and memory/doc updates it produced are present.
- The run ended only when the phase is complete or a triggered human checkpoint blocked progress — no stop after a single work package while unchecked phase tasks remain.

## Final handoff

After the selected phase is complete or blocked:
1. Echo the final plan/task/context/report artifact paths used during the phase.
2. State the selected roadmap phase, whether it is complete, and any remaining blocker.
3. Summarize the work packages completed, verification run, and memory/doc updates applied.
4. List any deferred findings with `ISSUES.md` or `BACKLOG.md` references.

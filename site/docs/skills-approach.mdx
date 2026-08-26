---
title: Skills approach
description: Agent Layer's skill ethos and bundled root skill model.
sidebar_position: 7
---

This page defines Agent Layer's root-skill and workflow architecture.

Each document owns a distinct topic:

- [Skill Design](/skill-design) owns portable skill design guidance.
- [CLI Skill Design](/cli-skill-design) owns CLI-skill design.
- [Instruction Design](/instruction-design) owns the boundary between
  always-loaded instructions and skills.
- This page owns Agent Layer's skill architecture.
- `docs/SKILL-AUDIT.md` owns the repository audit procedure.

## Design ethos

The following principles govern every root skill and workflow skill:

- One purposeful pass per stage. Planning, review, implementation,
  verification, cleanup, and shipping each happen once by default. Repetition
  requires a concrete failure, not a desire for greater confidence.

- Sufficiency over exhaustiveness. Each stage should produce enough confidence
  to proceed safely. The goal is a sound result, not the elimination of every
  conceivable uncertainty.

- Progress through complementary perspectives. Quality comes from different
  stages examining different artifacts—not from repeatedly asking agents to
  reconsider the same artifact.

- Review concrete work whenever possible. Prefer reviewing implemented
  behavior, diffs, test results, and observable evidence over repeatedly
  refining hypothetical implementation details.

- Evidence outranks consensus. Repository behavior, tests, specifications, and
  documented requirements settle questions. Additional agent agreement does not
  make established evidence stronger.

- Reasoning should be proportional to risk. Deep reasoning is reserved for
  genuinely difficult, consequential, or ambiguous decisions. Routine
  coordination and review should favor decisive, efficient judgment.

- Only user-owned decisions interrupt momentum. Escalate choices that
  materially affect behavior, architecture, scope, risk, or cost. Agents should
  resolve routine implementation and verification details themselves.

- Findings must justify their cost. A finding should change correctness,
  safety, scope, or meaningful maintainability. Stylistic refinements and
  speculative edge cases should not block the next stage.

- Every stage has a terminal responsibility. An orchestrated role must deliver
  its artifact, verdict, or blocker and then yield. It should not create another
  layer of orchestration to strengthen its own conclusion.

- Forward motion is the default. Once a stage has produced its required
  result, the workflow advances. “More review might find something” is not
  sufficient reason to remain in place.

**Stable modules, not improvisation.** A skill should offer the same job every
time: accepted inputs, required artifacts, stop conditions, verification
expectations, and final handoff shape. The agent still uses judgment inside the
task, but the module boundary and terminal responsibility should be stable.

**Skills own bounded jobs and compose only when the boundary is useful.** The
bundled `implement` skill chooses a direct or planned path and adds independent
review only when its expected value justifies the cost. `ship-pr` owns the
external pull-request lifecycle. `auto-skill-loop` composes those two workflows
through named Agent Dispatch targets. A stage is re-entered only for a concrete
failure or materially changed artifact.

**Evidence over assertion or consensus.** Skills should push agents toward
observable evidence: read the code, inspect the diff, run documented checks,
use real artifacts, and state what was verified. Additional review does not
override repository behavior, specifications, tests, or documented requirements.

**Verification is intentionally placed.** Implementation uses narrow task-local
checks for feedback and finishes with the smallest credible checks for the final
working tree. A failed check or a subsequent material change is a concrete
reason to repair and re-run the affected evidence; unchanged work is not.

**Use built-in subagents only for a distinct perspective or bounded job.** The
child must be able to follow its prompt without knowing whether a user or parent
workflow launched it. The parent supplies inputs, handles the child result, and
chooses the next phase. Do not add agents merely to repeat a judgment or build
consensus around evidence that already settles the question.

**Parents own boundaries; children own procedures.** A parent workflow states a
child skill's required inputs, accepted result, and response to failure or
required user input. The child owns its internal procedure. Do not repeat child
logic, evidence, or rationale in the parent. Once the child returns its
artifact, verdict, or blocker, its role is complete.

**Ask the user only for a user-owned decision or approval.** Name the exact
behavior, architecture, scope, risk, cost, destructive operation, external
write, or missing requirement that prevents progress. State where execution
resumes after the answer. Agents resolve routine implementation and verification
details and continue automatically when no user-owned decision is required.

## Root Skill Model

Representative stable contracts:

| Root skill | Stable contract |
| --- | --- |
| `implement` | Select a proportional direct or planned path, complete the requested code change, simplify it, and verify the final result. |
| `ship-pr` | Prepare and monitor one pull request, dispatch bounded repairs, and stop at the exact-head merge-authorization gate. |
| `interface-audit` | Audit component boundaries and produce or update a report without implementing its recommendations. |

## Workflow skills

Workflow boundaries include:

- `ship-pr` owns one pull request shipping stage, including audit, commit, push,
  continuous integration monitoring, reviewer feedback, and final status
  checks. External state changes may require targeted repair without restarting
  unrelated stages.
- `implement` owns proportional planning, implementation, review selection,
  cleanup, and local verification for one requested change.
- `interface-audit` covers one declared scope, scores component boundaries, and
  reports recommendations without implementing them.
- `auto-skill-loop` adaptively selects the next coherent work from a named mode,
  delegates each bounded step, centrally batches and ships ready deliveries,
  reconciles authoritative sources, and moves past local blockers until
  substantive autonomous work is exhausted or the user stops it.

## Contract expectations

A root skill should define:

- Inputs it accepts, including file paths, artifacts, issue identifiers, or
  plain-language scope.
- Its distinct stage responsibility and the artifact, verdict, or blocker that
  terminates the stage.
- Output artifact or report shape, so another skill or workflow can consume it.
- Any required shared system it depends on, with a reason the root skill cannot
  do its own job without it.
- Stop conditions for missing required input or incompatible state.
- Verification expectations appropriate to its job. Implementation skills may
  run narrow feedback checks, verification skills own completion evidence, and
  workflows should not duplicate broad checks without a concrete failure,
  materially changed artifact, or explicit release or continuous integration
  boundary.
- Final handoff fields that are stable enough for follow-up automation.

A root skill should avoid:

- Hidden mode switches that change the main output contract.
- Open-ended instructions to iterate until confidence, consensus, perfection,
  or diminishing returns.
- Findings based only on style preference or speculative edge cases that do not
  materially affect correctness, safety, scope, or maintainability.
- Delegating the same judgment to another orchestrated role after its own
  contract is satisfied.

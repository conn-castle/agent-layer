---
name: write-plan
description: >-
  Write a scoped implementation plan, task list, and context file for a
  requested change, roadmap slice, execution strategy, task breakdown, or
  pre-implementation design artifact.
---

# write-plan

Write a plan that is clear enough for a fresh agent or junior developer to
execute without guessing. The output is three artifacts:

- a narrative plan
- a small ordered task list
- an implementation context file

The plan should be specific, testable, and tightly scoped to the user's
request. The context file orients a fresh implementing agent.

Scale artifact detail to the scope, risk, and ambiguity of the work. Simple,
localized changes should produce concise artifacts with brief sections, a short
checklist, and only the context needed to start. Larger, cross-cutting,
ambiguous, or risky work needs more rationale, sequencing, risk analysis, and
verification detail. Do not add filler, generic background, or exhaustive file
lists just to make the artifacts look substantial.

## Defaults

- Default to planning only. Do not edit code unless the user explicitly asks
  for implementation too.
- Default scope is the smallest coherent slice that produces a reviewable
  outcome.
- If the request is to plan roadmap execution and no phase is named, use the
  first incomplete roadmap phase and plan the smallest coherent slice inside
  it.
- If the work is architectural or cross-cutting, read the project roadmap and
  decision context first.
- If the request is only to execute an already-valid artifact set, do not write
  a replacement plan unless the user asks for one.
- If the user does not provide target files or directories, infer the smallest
  necessary scope from the request and say what scope you chose.
- Keep the required artifact structure, but let each section be as short or
  detailed as the scoped work warrants.

## Required artifacts

Use the standard artifact naming rule under `.agent-layer/tmp/`:

- `.agent-layer/tmp/write-plan.<run-id>.plan.md`
- `.agent-layer/tmp/write-plan.<run-id>.task.md`
- `.agent-layer/tmp/write-plan.<run-id>.context.md`

Use one shared `run-id = YYYYMMDD-HHMMSS-<short-rand>`. Create all three files
before writing.

## Planning workflow

### Phase 1: Preflight

1. Restate the objective in one sentence.
2. Identify the exact planning target: feature, bug fix, refactor, roadmap
   slice, issue batch, or execution strategy.
3. Read only the files needed to understand the target area. Avoid broad repo
   scans unless the request truly demands it.
4. For architectural, roadmap, issue, or backlog work, read the relevant memory
   files before committing to a plan.
5. Drive substantive unknowns to ground by reading code/docs, running small
   experiments in `.agent-layer/tmp/`, checking current external docs when
   needed, or asking the user.

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
   - any relevant roadmap or decision constraints
   - user-confirmed decisions that settled substantive choices, if any
4. `## Approach`
   - the intended design or execution path
   - why this path is preferred over obvious alternatives
5. `## Risks`
   - behavior regressions
   - migration or compatibility concerns
   - unclear dependencies
6. `## Verification`
   - exact verification commands or evidence required
   - when each command should run, if timing matters
   - what each command proves
7. `## Exit Criteria`
   - objective conditions that define completion

Use the human checkpoint standard below while drafting. Ask before committing
to an approach that requires a user decision.

### Phase 3: Draft the task list

The task file should be a compact ordered checklist that mirrors the plan.

Task requirements:

- keep items small and verifiable
- include tests, docs, and memory updates when applicable
- include a final verification step
- group by execution order, not by file count

Preferred format:

```md
# Task List

- [ ] Confirm scope and context
- [ ] Implement change set A
- [ ] Add or update tests
- [ ] Update docs/memory if affected
- [ ] Run verification
```

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

Context requirements:

- all file paths must be relative to the repository root
- every file listed must actually exist, or be explicitly marked as new
- keep descriptions brief
- do not duplicate the plan's narrative
- do not include generic best practices

### Phase 5: Self-review and gate

Before presenting the artifacts, check:

- scope matches the user request exactly
- non-goals are explicit
- dependencies are ordered before dependents
- verification is credible for the risk level
- docs, tests, and memory updates are accounted for when needed
- the context file lists every file the plan expects to touch
- all file paths in the context file are valid or marked as new
- choices that require a user decision are either recorded as user-confirmed
  decisions or the gate verdict is `escalate`
- the plan contains no unresolved hedge words such as "likely", "probably", or
  "should work"
- the context file identifies a clear implementation entry point

Choose one handoff verdict:

- `proceed`: the artifact set is ready for execution
- `revise`: the artifacts need another drafting pass
- `escalate`: a human checkpoint is required
- `rewrite-because-out-of-scope`: the request should be rewritten around a
  smaller in-scope slice

## Human checkpoints

Use this standard for user-owned decisions:

- A user decision is required when repo evidence leaves multiple viable
  approaches and choosing one would commit the user to materially different
  behavior, public API, CLI behavior, compatibility, architecture, ownership
  boundaries, sequencing, rollout, scope, risk, cost, data migration, security
  or privacy posture, or destructive or irreversible work.
- A user decision is not required for routine implementation details,
  mechanical choices, verification selection, context gathering, or choices
  already settled by the user request, roadmap, DECISIONS.md, repo conventions,
  or supplied artifacts.

A plan satisfies this standard by recording the user-confirmed decision, citing
the source that already settles it, or narrowing scope so the decision is no
longer needed.

When the plan cannot satisfy this standard without a new user decision, ask the
smallest question that resolves the choice before committing it to the plan.

## Definition of done

- All three artifacts exist under one shared run id.
- Artifact size matches the scope instead of padding simple work
- The plan contains every required section.
- The plan records any user-confirmed decisions that shape the approach.
- The task file has an ordered checklist ending in verification.
- The context file lists key files, current state, constraints, and entry point.
- The self-review gate recorded exactly one verdict.


## Final handoff

After writing the artifacts:

1. Echo all three artifact paths.
2. Summarize the plan in a few sentences.
3. State the handoff verdict and highlight the biggest risk or open question,
   if any.

# write-plan

Write three implementation-ready artifacts for the smallest coherent,
reviewable scope. Scale detail to risk and ambiguity; omit generic background,
filler, and exhaustive file inventories. For unnamed roadmap execution, use the
first incomplete phase.

## Artifacts

Use one `YYYYMMDD-HHMMSS-<short-rand>` run id:

- `.agent-layer/tmp/write-plan.<run-id>.plan.md`
- `.agent-layer/tmp/write-plan.<run-id>.task.md`
- `.agent-layer/tmp/write-plan.<run-id>.context.md`

## Preflight

Normalize the source into an objective, observable success criteria, scope and
non-goals, constraints, user requirements, source evidence, and unresolved
facts. Read only relevant code and documentation, including applicable memory
files for architecture, roadmap, issue, or backlog work. Resolve facts from
repository evidence before drafting. Escalate only a substantive choice that
evidence cannot settle.

## Plan artifact

Include:

- `# Objective`: outcome, non-outcome, and observable success
- `## Scope`: in scope, out of scope, and material assumptions
- `## Context`: relevant components, source evidence, roadmap or decision
  constraints, and settled user decisions
- `## Approach`: intended design or execution path, with rationale for
  consequential choices
- `## Material Risks`: behavior, compatibility, migration, or dependency risks
- `## Verification`: repository-defined commands or other evidence and what it
  proves
- `## Exit Criteria`: objective completion conditions

## Task artifact

Write a compact ordered Markdown checklist that mirrors implementation order.
Include directly required code, tests, docs, and memory updates. Keep final
verification in the plan rather than duplicating it as tasks.

## Context artifact

Include:

- `# Implementation Context`: one-sentence purpose
- `## Key Files`: relative paths and their roles; mark new files
- `## Current State`: relevant behavior before implementation
- `## Constraints`: non-obvious dependencies and invariants
- `## Entry Point`: where implementation should begin and why

Do not repeat the plan narrative or generic practices.

## Self-check and handoff

Confirm that the artifacts match the source, resolve the approach rather than
deferring it, cover risk-appropriate tests/docs/memory work, use valid paths,
and record any user-owned decision. Correct autonomous gaps in place and rerun
only the affected check.

Return the three paths, a short summary, material risks, and one verdict:

- `proceed`: ready for review
- `revise`: name an evidence-backed correction that can be made autonomously,
  then apply it in this stage
- `escalate`: name the unresolved substantive user decision

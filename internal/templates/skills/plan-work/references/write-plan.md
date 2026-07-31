# write-plan

## Artifacts

Use one `YYYYMMDD-HHMMSS-<short-rand>` run id:

- `.agent-layer/tmp/write-plan.<run-id>.plan.md`
- `.agent-layer/tmp/write-plan.<run-id>.task.md`
- `.agent-layer/tmp/write-plan.<run-id>.context.md`

### Plan artifact

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

### Task artifact

Write a compact ordered Markdown checklist that mirrors implementation order.
Include directly required code, tests, docs, and memory updates. Keep final
verification in the plan rather than duplicating it as tasks.

### Context artifact

Include:

- `# Implementation Context`: one-sentence purpose
- `## Key Files`: relative paths and their roles; mark new files
- `## Current State`: relevant behavior before implementation
- `## Constraints`: non-obvious dependencies and invariants
- `## Entry Point`: where implementation should begin and why

Do not repeat the plan narrative or generic practices.

Preserve every valid requirement from the inputs in the derived
artifacts without weakening or omitting them.

## Workflow

1. Write the plan artifacts.
2. Escalate substantive decisions to the user.
3. Iterate and update the plan in-place as needed.
4. Confirm that the artifacts are complete, fully resolve the request and
record any user-made decisions.

Return the three paths, a short summary, material risks, and one verdict:
- `proceed`: ready for review
- `escalate`: name the unresolved substantive user decision

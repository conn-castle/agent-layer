---
name: review-plan
description: Review and critique the active implementation plan artifact with concrete risks, missing tests, and scope gaps before execution.
---

# Review plan

## Intent
Review the current plan artifact and provide actionable feedback before execution.

## Active plan discovery (standard)
Use the standard artifact naming convention under `.agent-layer/tmp/`:
- `<workflow>.<run-id>.plan.md`
- `run-id = YYYYMMDD-HHMMSS-<short-rand>`

Discovery rules:
1. List `.agent-layer/tmp/*.plan.md`.
2. Keep only files that match `<workflow>.<run-id>.plan.md` and valid `run-id` shape.
3. Select the plan with the latest `run-id` (lexicographic order is time order for this format).
4. Optionally load the matching task artifact `<workflow>.<run-id>.task.md` when present.

Fallback:
- If no valid plan artifact exists, ask the user to provide the plan path or regenerate the plan.

## Review checklist
- Requirement coverage: all requested goals are represented.
- Scope control: out-of-scope items are explicit.
- Dependency order: prerequisites appear before dependents.
- Validation depth: tests and verification commands are credible.
- Risk handling: major failure modes and rollback path are explicit.
- Docs/memory updates: roadmap/memory updates are explicit when required.

## Output format
- Findings first, ordered by severity.
- Include exact file and line references for each finding.
- Include open questions/assumptions.
- Provide a concise recommendation summary (approve, approve-with-changes, or revise).

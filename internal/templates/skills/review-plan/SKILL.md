---
name: review-plan
description: >-
  Explicit-only.
  Independently review a plan/task/context artifact set and report material
  findings.
---

# review-plan

## Required inputs

Require:

- plan, task, and context artifact paths
- an optional specification artifact path

## Findings artifact

Write `.agent-layer/tmp/review-plan.<run-id>.findings.md` with run ID
`YYYYMMDD-HHMMSS-<short-rand>`.

## Workflow

Do not edit the input artifacts or implementation code.

1. Use 1–4 fresh built-in subagents in parallel, based on the plan's risk and
   complexity. Each reviews all inputs through a distinct framing; do not split
   coverage. Consequential architecture changes require an architecture
   framing. Do not use Agent Dispatch or another workflow.
2. Assess every subagent finding against the artifacts and repository. Do not
   accept findings by vote or forward raw reports.
3. Report only evidence-backed findings that are material. For each, give its
   location, evidence, impact, and suggested correction.

Include the subagent count, rationale, framings, and any substantive user
decision. Return the findings path and one verdict:

- `approved`: no material findings
- `changes-needed`: material findings remain
- `blocked-for-user-decision`: a substantive choice is unresolved

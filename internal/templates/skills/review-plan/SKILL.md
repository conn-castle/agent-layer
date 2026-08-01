---
name: review-plan
description: >-
  Explicit-only.
  Review a plan/task/context artifact set and report material findings.
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

1. Review all inputs directly through 1–3 distinct framings chosen for the
   plan's risk and complexity. Each framing covers the complete artifact set;
   do not split coverage. Consequential architecture changes require an
   architecture framing.
2. Assess every potential finding against the artifacts and repository. Do not
   treat repetition across framings as evidence.
3. Report only evidence-backed findings that are material. For each, give its
   location, evidence, impact, and suggested correction.

Include the framing count, rationale, framings, and any substantive user
decision. Return the findings path and one verdict:

- `approved`: no material findings
- `changes-needed`: material findings remain
- `blocked-for-user-decision`: a substantive choice is unresolved

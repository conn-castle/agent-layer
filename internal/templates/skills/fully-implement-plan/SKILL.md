---
name: fully-implement-plan
description: >-
  Explicit-only.
  Implement a supplied plan, improve the code, check it against the plan, and
  resolve material findings.
---

# fully-implement-plan

## Inputs

Require exact plan and context artifact paths and dispatch targets for
`implementer`, `fixer`, and `code_reviewer`. Use `/agent-dispatch`.

## Workflow

1. Dispatch `implementer` to implement the supplied plan using its context and
   run relevant checks. If it reports unfinished work, continue implementation
   before proceeding.
2. Dispatch `fixer` using `references/improve-code-prompt.md`.
3. Dispatch `code_reviewer` to check the implementation against the plan for
   correctness and completeness.
4. Resume `fixer` with accepted findings. Require it to resolve them and rerun
   affected checks.

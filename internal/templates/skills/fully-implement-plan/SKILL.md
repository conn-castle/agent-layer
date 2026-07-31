---
name: fully-implement-plan
description: >-
  Explicit-only.
  Implement supplied plan/task/context artifacts, review the changes, verify
  the contract, and repair material findings.
---

# fully-implement-plan

Implement an artifact contract, review and verify the delivery, and repair
material findings. Leave shipping and unrelated full-lane checks to callers.

## Inputs and boundaries

Require exact plan, task, and context artifact paths plus one explicit,
self-contained dispatch target specification for each of `implementer`,
`code_reviewer`, and `fixer`. Treat artifacts as the contract and validate
delegated evidence against the latest tree.

Write `.agent-layer/tmp/fully-implement-plan.<run-id>.report.md` and track its
contract obligations, findings, and evidence. Serialize mutations; do not
stage, commit, weaken checks, or destructively rewrite user changes.

If follow-up dispatches are required, pass artifact and report paths, changed
files, and unresolved finding IDs. Do not inline complete artifacts, reports,
command output, or resolved findings. If delegated work remains incomplete,
either continue the retained conversation or start a fresh one if its
existing context is impeding progress. Use a different prompt and approach,
including the unresolved obligations and failure evidence.

## Workflow

1. Dispatch `implementer` with `/implement-plan` and the supplied artifacts.
   If it reports unfinished contract work, continue implementation before
   starting review. Do not send known unfinished work to reviewers or `fixer`.
2. When implementation is ready for review, run against the latest tree and
   supplied artifacts in parallel:
   - Run `/verify-work` in a fresh subagent
   - Dispatch `code_reviewer` with `/review-uncommitted-code`
3. Send all confirmed findings to `fixer` in one batch with the plan artifacts
   and required tests, documentation, and memory updates.
4. Validate the repairs with targeted checks. Address any findings those checks
   confirm remain unresolved without repeating completed review work.

An unsuccessful delegated attempt or unfinished technical work is not a
workflow blocker.

## Completion contract

Return:

- `complete` when the final tree satisfies the contract and all confirmed
  in-scope findings are resolved or disproven with evidence
- `complete-with-follow-up` when only explicit out-of-contract work remains
- `blocked` for a named unresolved constraint

Return the report path and status.

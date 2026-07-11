---
name: interface-audit
description: >-
  Audit product interfaces as component boundaries, score complexity,
  over-engineering, and debt, maintain .agent-layer/tmp interface-audit reports,
  and finish at the final recommendation gate. Use for fresh interface cleanup
  audits or --update refreshes; not for implementation.
---

# interface-audit

Audit product interfaces as component boundaries and produce one evidence-backed
report. Do not implement the recommendation.

## Inputs and references

- Fresh audit: no option.
- Update audit: `--update`, optionally with an explicit report path.
- No other flags are supported. Ask which mode to use if extra flags make the
  request ambiguous.

Read [`references/report-structure.md`](references/report-structure.md) before
creating or editing a report. With `--update`, also read
[`references/update-workflow.md`](references/update-workflow.md); it owns report
selection, update boundaries, and source evidence.

## Fresh-run isolation

A fresh audit uses only current code, tests, docs, command output, and user
instructions. Do not inspect or reuse prior audits, cleanup analyses, or agent
outputs. If the user wants prior evidence incorporated, use update mode.

## Evidence contract

- Treat each scored row as a concrete component boundary, not a vague area.
- Verify names and numeric claims before recording them. Use `partial` when an
  exact count would cost more than it contributes.
- Tie complexity, over-engineering, debt, and confidence scores to current
  observable evidence.
- Preserve row identifiers during updates; retired identifiers are not reused.
- Prefer current code and tests over stale documentation.
- Protect discovered product requirements unless the user approves a behavior
  change.
- Do not preserve a prior score when current evidence no longer supports it.

## Workflow

### 1. Establish mode and artifact

Read the applicable references and establish the repository baseline. For a
fresh audit, create the required report. For an update, select and update only
the report established by the update workflow.

### 2. Run one interface evidence pass

Discover the interface chain, contracts, ownership, state, tests, failure
modes, and material cleanup opportunities. For a broad or context-heavy scope,
form enough coherent boundary groups to give materially different interfaces
independent attention without overloading an investigator's context. Do not
combine groups at the cost of boundary coverage or split them merely to
increase agent count. Give each group to a fresh built-in investigator, running
substantial independent groups concurrently when the wall-clock benefit
warrants the extra agent cost and otherwise running them sequentially. Each
investigator returns compact row evidence and does not edit or calibrate the
report. A compact scope may be investigated directly. Do not ask multiple
investigators to reconsider the same row.

### 3. Calibrate and synthesize once

The main agent resolves evidence, calibrates scores across neighboring rows,
updates the required report sections, and identifies the highest-value coherent
improvement. Revisit a row only when its cited evidence is missing or
internally inconsistent, not to seek additional confidence.

If no candidate would materially reduce complexity, over-engineering, or debt,
record `no-material-improvement` and yield without manufacturing a proposed
change.

### 4. Final recommendation gate

Skip this gate only when Stage 3 recorded `no-material-improvement`.

Decide whether the highest-value finding requires major architecture: broad
ownership changes, protocol redesign, data-model changes, cross-language
contract replacement, or a substantial user-workflow change.

- If yes, propose the architectural item and explain why a smaller interface
  improvement is insufficient.
- Otherwise propose the smallest coherent improvement that materially reduces
  complexity, over-engineering, or debt.
- State any behavior change and require explicit approval before planning it.
- Stop after asking whether to run `/plan-work` for that item or select a
  different item.

## Guardrails

- Do not edit production code, tests, docs, or memory files.
- Do not widen beyond product interfaces or create parallel reports.
- Do not score from intuition when evidence is available.
- Do not run `/plan-work` from this skill.

## Definition of done

- The required report was created or updated through one evidence and
  calibration pass.
- Every material score and recommendation cites concrete evidence.
- The skill returns the report path and either the final recommendation gate or
  `no-material-improvement`, then yields.

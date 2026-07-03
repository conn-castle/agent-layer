---
name: interface-audit
description: >-
  Audit product interfaces as component boundaries, score complexity,
  over-engineering, and debt, maintain .agent-layer/tmp interface-audit reports,
  and finish at the final recommendation gate. Use for fresh interface cleanup
  audits or --update refreshes; not for implementation.
---

# interface-audit

Audit product interfaces as component boundaries. Produce a report, not code.

## Progressive Disclosure

Read [`references/report-structure.md`](references/report-structure.md) before
creating or editing a report. It owns filename, sections, tables, and fields.

With `--update`, also read
[`references/update-workflow.md`](references/update-workflow.md) before
inspecting code. It owns report selection, local-change review, merged-PR
review, and update limits.

Do not duplicate those reference details in generated notes or side artifacts.

## Inputs

- Fresh audit: no option.
- Update audit: `--update`.

No other options are supported. If extra flags appear, stop and ask which mode
to use. In update mode, a report path may appear in the user's message;
otherwise use `references/update-workflow.md`.

## Fresh-Run Isolation

Fresh audits must be independent. Do not open, list, quote, summarize, compare,
or mine prior audit reports, cleanup analyses, or prior subagent outputs. Do not
give prior-run material to subagents.

Use only current code, tests, docs, command output, and user instructions. If
the user asks to use prior audit material, stop and ask whether to switch to
`--update`.

## Audit Rules

- Describe current code in present tense.
- Verify numeric claims and names before writing them.
- Preserve row numbers once assigned; retired numbers are not reused.
- Mark partial or unverifiable claims explicitly.
- Product requirements discovered during the audit are protected unless the user
  explicitly approves a behavior change.
- Prefer code and tests over docs when they disagree.
- Prefer exact file paths and symbol names over prose descriptions.
- Use structured parsers, language tooling, `rg`, `git`, and package test tools
  before manual counting when available.
- Do not preserve stale scores because prior reports said so.

## Workflow

1. Establish the mode and read the required reference file or files.
2. Confirm the repository baseline with `git status --porcelain`.
3. For a fresh audit, create the new report path defined by
   `references/report-structure.md`, then discover and score interfaces from
   current evidence only.
4. For `--update`, follow `references/update-workflow.md` exactly and edit only
   the selected report.
5. Use focused subagents for broad investigation, row-level verification, and
   adversarial score review when available. If unavailable, work inline and
   record the limitation.
6. Honor user-requested agent or model targets when available. If a requested
   target is unavailable, fail loudly and ask whether to continue with available
   targets or inline review.
7. Keep the main agent responsible for scoring calibration, resolving reviewer
   disagreement, and the final recommendation gate.
8. Complete the final recommendation gate below, then stop.

## Final Recommendation Gate

1. Decide whether any major architectural change is required to address the
   highest-value findings. Major means broad ownership changes, protocol
   redesign, data model changes, cross-language contract replacement, or a
   substantial change to user workflows.
2. If major architecture is required, propose that architecture item and state
   why smaller interface work is insufficient.
3. Otherwise propose the smallest coherent interface improvement that
   meaningfully reduces complexity, over-engineering, or debt.
4. If the proposal changes behavior, say so and ask for explicit approval before
   planning or implementation.
5. Stop after asking whether to run `/plan-work` for the proposed item or search
   for another item.

## Guardrails

- Do not edit production code, tests, docs, or memory files as part of this
  skill.
- Do not silently widen the audit beyond product interfaces.
- Do not create a parallel plan, summary, or scratch report.
- Do not score from vibes. Tie every score to concrete evidence.
- Do not treat behavior changes as cleanup.
- Do not run `/plan-work` from this skill without asking at the final gate.

## Definition of done

- Report created or updated.
- Applicable reference workflow followed.
- Final handoff satisfies the final recommendation gate.

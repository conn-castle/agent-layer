---
name: verify-work
description: >-
  Read-only check of completed work against a plan, task list, context file, or
  explicit request, reporting completion gaps, regressions, missing
  verification, docs/memory gaps, working-code evidence, and scope drift.
---

# verify-work

Verify completed work without fixing it. This is a completion and evidence
review, not a code-editing pass.

The main question is:

Did the implementation deliver the agreed contract, and is there credible
evidence that the touched code works?

## Defaults

- Require either a plan/task artifact pair or an explicit user request/scope to
  verify against.
- If plan/task artifacts are used, require explicit paths to both files. Load
  an explicit context file when provided.
- Default implementation target is the current working tree plus files touched
  for the supplied contract.
- Do not modify code, docs, memory files, or plan artifacts. Produce a report
  only.
- Use the sibling `contract-verification-rubric.md` as this skill's fixed
  internal comparison rubric.
- Own final verification for the supplied contract. Run the smallest credible
  checks, or accept existing command evidence only when it clearly covers the
  final working tree.

## Required inputs

The caller must provide one contract source:

- explicit paths to both a plan file and a task file, plus an optional context
  file
- an explicit user request/scope

If either the plan path or task path is missing, stop and ask for the missing
path. Do not discover, infer, or auto-select artifacts from `.agent-layer/tmp/`.

Do not treat implementation reports, summaries, PR descriptions, issue bodies,
or other free-form documents as contract artifacts. Read them only as evidence
when they are relevant to declared deviations, skipped work, or observed
verification.

## Required artifact

Write the report to:

- `.agent-layer/tmp/verify-work.<run-id>.report.md`

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>`. Create the file before writing.

## Review workflow

### Phase 1: Extract the contract

From the supplied plan/task/context artifacts or explicit user request/scope,
extract:

- objective
- in-scope items
- out-of-scope items
- promised tests or verification
- promised docs or memory updates
- explicit exit criteria
- key files and entry point, if present

If the contract is ambiguous enough that completion cannot be judged credibly,
ask for the smallest clarification that resolves it.

### Phase 2: Inventory the implementation

Inspect:

- `git status --porcelain`
- staged, unstaged, and untracked files relevant to the work
- diffs for touched files
- post-implementation content of files needed to judge behavior
- implementation reports, only for declared deviations or skipped work

Do not let the implementation report override the contract. It can explain a
deviation, but it cannot make missing promised work complete.

### Phase 3: Compare contract to implementation

Check item by item for:

- missing deliverables
- partial implementations presented as done
- behavior that diverges from the agreed approach
- missing or weak tests
- missing docs or memory updates
- scope creep that was not part of the contract
- undocumented deviations

Read the sibling `contract-verification-rubric.md` before comparing the
contract to the implementation. Keep the comparison grounded in the contract
artifacts, working-tree state, touched file content, diffs, and observed
verification evidence. Do not use chat history or implementer rationales as
evidence of completion.

### Phase 4: Verify working-code evidence

Read `COMMANDS.md` before choosing project workflow commands. Run or inspect
the checks that are credible for the touched area and contract:

- format, lint, typecheck, tests, build, docs checks, or targeted reproductions
  when relevant
- plan-promised commands when the plan named them
- focused checks when they prove the changed behavior
- broader project checks only when the contract or risk requires them
- user-supplied command output or logs
- direct inspection of touched files and diffs when command output is not the
  right evidence

Do not repeat a command when current, trustworthy output already covers the
same final working tree. If a needed command cannot run, record why and assess
the residual risk. Do not mark working-code evidence as satisfied without
observed command output, direct inspection, or a documented reason the check is
not applicable.

### Phase 5: Decide completion status

Assign one top-level conclusion:

- `complete`
- `complete-with-follow-up`
- `incomplete`

Use `complete-with-follow-up` only when the agreed contract is complete and the
remaining items are clearly outside that contract.

## Required report structure

Write:

1. `# Completion Verdict`
2. `## Inputs`
3. `## Contract Coverage`
   - item-by-item status
4. `## Findings`
   - ordered by severity
5. `## Working-Code Evidence`
6. `## Docs and Memory Assessment`
7. `## Recommended Next Step`

For every finding, include:

- `Title`
- `Severity`: Critical | High | Medium | Low
- `Confidence`: High | Medium | Low
- `Location`
- `Why it matters`
- `Evidence`
- `Recommendation`

## Guardrails

- Do not mark work complete just because code exists.
- Do not ignore missing verification.
- Do not expand the completion review beyond the supplied contract.
- Do not confuse scope drift with value-add; drift is still drift.
- If the implementation is better than the contract in a harmless way, note it,
  but still call out undocumented deviation.
- Do not fix issues discovered during verification.
- Do not run broad commands just to add evidence when a narrower check answers
  the contract.

## Definition of done

- The report exists at `.agent-layer/tmp/verify-work.<run-id>.report.md` with
  every required section.
- `Contract Coverage` lists every in-scope contract item with an item-by-item
  status; partial completions are not presented as done.
- `Working-Code Evidence` lists observed commands, direct inspections, skipped
  checks, and residual risk.
- The report carries exactly one verdict: `complete`,
  `complete-with-follow-up`, or `incomplete`.
- Implementation, plan, task, and context artifacts were not modified.

## Final handoff

After writing the report:

1. Echo the report path.
2. State the completion verdict clearly.
3. If incomplete, name the next exact action to take.

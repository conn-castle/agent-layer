---
name: simplify-new-code
description: >-
  Scan the current uncommitted diff for agent-added scope creep — speculative
  flexibility, premature abstractions, dead branches, defensive scaffolding,
  half-finished work — and auto-apply simplifications, preserving
  user-requested behavior. Use `simplify-codebase` for full-codebase
  complexity sweeps.
---

# simplify-new-code

This skill reverts **agent-side scope creep** in the current uncommitted diff
while preserving the behavior the user requested. LLM coding agents reliably
add speculative flexibility, premature abstractions, defensive scaffolding for
impossible cases, clever patterns, and half-finished extras beyond what was
asked. This skill identifies that scope creep by pattern and undoes it.
It reviews both individual hunks and the changed structure they create,
asking whether added abstraction, indirection, or complexity is justified
by the current behavior.

Use `simplify-codebase` instead when the goal is a codebase-wide complexity
sweep over committed code. This skill never operates outside the current diff
and never touches pre-existing code adjacent to the diff.

## Defaults

- Default scope is the **current uncommitted diff only** (staged, unstaged,
  and untracked production code; tests are out of scope — `prune-new-tests`
  handles them).
- Default disposition is **preserve requested behavior; remove what the agent
  added beyond it**. Removal is one tool among several — inline, flatten,
  collapse, and rewrite-to-straightforward are equally valid.
- Findings are auto-applied. The user is not asked per finding.

## Inputs

Accept any combination of:
- explicit paths or files within the diff (still must intersect the changed
  set)
- a dry-run flag to produce the findings report without applying simplifications
- a per-file override to skip a specific file

## Required artifact

Write the report to:
- `.agent-layer/tmp/simplify-new-code.<run-id>.report.md`

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>`.
Create the file with `touch` before writing.

## Multi-agent pattern

Required roles:
1. `Diff scout`: enumerates production code added or modified in the current
   uncommitted diff.
2. `Smell-pattern reviewer` (fresh-context subagent): receives **only** the
   changed code (and the minimal surrounding context needed to judge it)
   and the smell list below. It does **not** receive the user's original
   prompt, the plan, the task list, the implementer's narrative, or the
   prior conversation. Scope creep is identified by pattern and changed
   structure, not by comparison to the request — a minimal implementation
   of the visible behavior is the implicit baseline.
3. `Applier`: applies each accepted simplification, runs the project test
   command after each batch, and writes the report.

The reviewer is invoked once per batch of changed files. Large diffs are
split into chunks (one file or a small cluster of related files per chunk);
each chunk is a fresh invocation so the reviewer never accumulates context
across chunks.

### Reviewer subagent prompt

Pass the contents of [`reviewer-prompt.md`](reviewer-prompt.md) to the
reviewer subagent verbatim — do not paraphrase, summarize, or modify the
rubric. Send no prior conversation, no plan, no implementer notes.

Inputs the reviewer receives alongside the prompt:
- The diff hunks for production code in scope (added or modified lines,
  with sufficient surrounding context for understanding).
- The minimal pre-existing code that the changes depend on (function
  signatures, type definitions, imports referenced).
- Nothing else. No plan, no task list, no context file, no user prompt,
  no implementer rationale.

## Global constraints

- Preserve the user-requested behavior. If a proposed simplification
  changes observable behavior, reject it.
- Operate only on the current uncommitted diff. Pre-existing code is
  untouchable even when adjacent.
- Apply findings in batches. After each batch, run the project's
  repo-defined test command (consult `COMMANDS.md`) and observe the output.
- If a simplification invalidates a test, update or remove the test as
  normal refactor hygiene — but only within the diff scope (added tests).
  Pre-existing tests that fail are a signal the simplification changed
  behavior; revert the simplification, do not weaken the test.
- Do not consolidate added items into shared abstractions on the cleanup
  pass. Cleanup that introduces new abstractions is itself scope creep.
- Re-scan after applying a batch. Stop when no further smells are found,
  or after the iteration cap below.

## Human checkpoints

- Required: ask when the diff contains zero production code changes — the
  skill has no work to do; confirm before exiting silently.
- Required: ask when a half-finished implementation smell would require
  user input to complete vs. remove. Default is to remove unless the user
  signals otherwise.
- Required: ask when a proposed simplification would change a public API
  surface or break a caller outside the diff — that is no longer scope-
  contained.
- Required: ask when the iteration loop has not converged after 3
  re-scans; do not loop indefinitely.
- Stay autonomous on all in-scope findings whose fix is local to the diff.

## Workflow

### Phase 1: Enumerate diff scope (Diff scout)

1. Run `git status --porcelain`, `git diff --cached`, `git diff`, and
   `git ls-files --others --exclude-standard` to find changed production
   files (excluding test files — those are `prune-new-tests`'s domain).
2. Read `COMMANDS.md` to identify the test command for verification.
3. Record the changed files and the diff scope in the report under
   `## Scope`.

### Phase 2: Smell scan (Smell-pattern reviewer)

1. Group changed files into review chunks.
2. For each chunk, invoke the reviewer subagent with the contents of
   `reviewer-prompt.md` and the chunk inputs above. The subagent must be
   a fresh invocation with no carryover from this conversation.
3. Require the reviewer to consider the changed diff as a small structure,
   not just isolated lines: added helpers, layers, types, state, branches,
   and control flow must earn their complexity against the current behavior.
4. Collect the JSON-line findings under `## Findings` as a table with
   columns `Location`, `Smell`, `Before`, `After`, `Rationale`.

### Phase 3: Apply simplifications (Applier)

1. Group findings into small, coherent batches (one smell category at a
   time, or one file at a time, whichever is smaller).
2. Apply each finding's `before` → `after` simplification.
3. After each batch, run the project's test command from `COMMANDS.md`.
   Record the command, exit status, and any new failures.
4. If a batch causes tests to fail, revert that batch and mark each
   finding `reverted` in the report with the failure observed. Continue
   with the next batch.
5. Stop the batch loop when no further findings remain or after applying
   all findings the reviewer produced.

### Phase 4: Re-scan and converge (Smell-pattern reviewer + Applier)

1. After all initial findings are applied or reverted, re-invoke the
   reviewer subagent (fresh context) on the updated diff to catch second-
   order smells revealed by the first pass.
2. Apply any new findings using Phase 3's batching rules.
3. Stop when a re-scan returns zero findings, or after 3 re-scans.
   Escalate at the 3-scan limit instead of looping.

## Required report structure

Write `.agent-layer/tmp/simplify-new-code.<run-id>.report.md` with:

1. `# Simplify New Code Summary`
2. `## Scope`
   - changed files and the diff range covered
3. `## Findings`
   - table: `| Location | Smell | Before | After | Rationale |`
4. `## Simplifications Applied`
   - one bullet per applied finding, file:line and one-line description
5. `## Reverted Simplifications`
   - any batch reverted due to test failure, with the observed failure
6. `## Test Runs`
   - per batch: the exact command and observed exit status
7. `## Re-scan Iterations`
   - number of re-scan passes, findings per pass, and convergence note
8. `## Out-of-Scope Observations`
   - smells noticed in pre-existing code adjacent to the diff (not
     applied); recommend `simplify-codebase` if material

## Guardrails

- Do not change user-requested behavior. The skill reverts agent additions
  beyond the request, not the request itself.
- Do not redesign module structure, file layout, public APIs, or naming
  — that is `simplify-codebase`'s domain.
- Do not look for cross-file duplication or codebase-wide consolidation
  — that is `simplify-codebase`'s domain.
- Do not optimize for performance.
- Do not enforce stylistic preferences where the existing code is
  acceptably clear.
- Do not "improve" pre-existing code outside the diff scope, even when
  smells are visible nearby. Note them as out-of-scope observations.
- Do not consolidate two added items into a shared abstraction. Cleanup
  that introduces new abstractions is itself agent scope creep.
- Do not lower coverage thresholds or skip checks to clear failures.

## Definition of done

- The report exists at `.agent-layer/tmp/simplify-new-code.<run-id>.report.md`
  with every required section (`Scope`, `Findings`, `Simplifications
  Applied`, `Reverted Simplifications`, `Test Runs`, `Re-scan Iterations`,
  `Out-of-Scope Observations`).
- Every applied finding is reflected in the working tree at the location
  named in the table; reverted findings are recorded with the failure
  observed.
- The re-scan loop terminated with zero new findings or hit the 3-scan
  cap (and the cap-hit was escalated, not silently ignored).
- The project's test command ran after each applied batch with observed
  output recorded; no reverted batch was re-applied without addressing
  the failure.

## Final handoff

After writing the report:
1. Echo the report path.
2. State total findings, applied count, reverted count, and re-scan
   iterations.
3. If `Out-of-Scope Observations` is non-empty, recommend a follow-up
   `simplify-codebase` run scoped to the named files.

# simplify-uncommitted-code

This asset removes complexity in uncommitted production-code changes when that
complexity is not justified by the user-requested behavior.

## Defaults

- Default scope is the **current uncommitted production-code diff only**:
  staged, unstaged, and untracked files. Tests, committed code, and
  pre-existing adjacent code are out of scope.
- Default disposition is **preserve requested behavior; remove what the current
  change added beyond it**. Auto-apply findings without per-finding approval;
  inline, flatten, collapse, or rewrite to straightforward code as needed.

## Inputs

Accept any combination of:
- explicit paths or files within the diff (still must intersect the changed
  set)
- a dry-run flag to produce findings without applying simplifications
- a per-file override to skip a specific file

## Multi-agent pattern

Required roles:
1. `Diff scout`: enumerates production code in scope.
2. `Smell-pattern reviewer`: reviews chunks against the prompt rubric.
3. `Applier`: applies or reverts findings and prepares the final handoff.

### Reviewer subagent prompt

Pass the contents of
[`simplify-uncommitted-code-reviewer-prompt.md`](simplify-uncommitted-code-reviewer-prompt.md)
to the reviewer subagent verbatim — do not paraphrase, summarize, or modify
the rubric.

Inputs the reviewer receives alongside the prompt:
- The diff hunks for production code in scope (added or modified lines,
  with sufficient surrounding context for understanding).
- The minimal pre-existing code that the changes depend on (function
  signatures, type definitions, imports referenced).
- Nothing else, including prior conversation, plans, task lists, context
  files, user prompts, implementer rationale, or prior reviewer output.

## Global constraints

- Preserve the user-requested behavior. If a proposed simplification
  changes observable behavior, reject it.
- If a simplification invalidates a test, update or remove the test as
  normal refactor hygiene — but only within the uncommitted test-change scope.
  Pre-existing tests that fail are a signal the simplification changed
  behavior; revert the simplification, do not weaken the test.
- Do not consolidate added items into shared abstractions on the cleanup
  pass. Cleanup that introduces new abstractions is itself scope creep.
- Do not lower coverage thresholds or skip checks to clear failures.

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
   files (excluding test files, which are out of scope).
2. Read `COMMANDS.md` to identify the test command for verification.
3. Track the changed files and diff scope.

### Phase 2: Smell scan (Smell-pattern reviewer)

1. Group changed files into review chunks (one file or a small cluster of
   related files per chunk).
2. For each chunk, invoke the reviewer subagent with the contents of
   `simplify-uncommitted-code-reviewer-prompt.md` and the chunk inputs above. The
   built-in subagent must be a fresh invocation with no carryover from this
   conversation.
3. Track each JSON-line finding with `Location`, `Smell`, `Before`, `After`,
   and `Rationale`.

### Phase 3: Apply simplifications (Applier)

1. Group findings into small, coherent batches (one smell category at a
   time, or one file at a time, whichever is smaller).
2. Apply each finding's `before` → `after` simplification.
3. After each batch, run the project's test command from `COMMANDS.md`.
   Record the command, exit status, and any new failures.
4. If a batch causes tests to fail, revert that batch and mark each
   finding `reverted` with the failure observed. Continue with the next batch.
5. Stop the batch loop when no further findings remain or after applying
   all findings the reviewer produced.

### Phase 4: Re-scan and converge (Smell-pattern reviewer + Applier)

1. After all initial findings are applied or reverted, re-invoke the
   reviewer subagent (fresh context) on the updated diff to catch second-
   order smells revealed by the first pass.
2. Apply any new findings using Phase 3's batching rules.
3. Stop when a re-scan returns zero findings, or after 3 re-scans.
   Escalate at the 3-scan limit instead of looping.

## Guardrails

- Do not redesign module structure, file layout, public APIs, or naming as
  a cleanup shortcut; broader redesign is outside this skill's scope.
- Track material adjacent smells as out-of-scope observations; do not apply
  them.
- Do not optimize for performance.
- Do not enforce stylistic preferences where the existing code is
  acceptably clear.

## Final handoff

After completing the workflow:
1. State total findings, applied count, reverted count, and re-scan
   iterations.
2. Summarize simplifications applied, reverted batches, and test
   command/results.
3. If out-of-scope observations exist, recommend a follow-up
   codebase-wide cleanup scoped to the named files.

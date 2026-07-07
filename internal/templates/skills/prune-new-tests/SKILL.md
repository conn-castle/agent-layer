---
name: prune-new-tests
description: >-
  Burden-of-proof review of newly added tests: auto-delete any test that can't
  justify itself with a production-code mutation that would flip its assertion
  given the test's actual input. Do not use for full-suite health or for adding
  tests.
---

# prune-new-tests

This skill prunes low-value tests that the implementing agent added as a side
effect of implementation.

## Defaults

- Default scope is **tests added in the current uncommitted diff only**:
  staged, unstaged, and untracked test files, plus new test cases inside
  pre-existing test files. Modified pre-existing tests and deleted tests are
  out of scope.
- Default disposition is **delete unless justified**. Auto-delete without
  per-test approval; surface surviving coverage gaps without backfilling
  replacement assertions.

## Inputs

Accept any combination of:
- explicit paths or test files (still must intersect the added-test set)
- a per-test override list to keep without review
- a dry-run flag to produce verdicts without deleting

## Multi-agent pattern

Required roles:
1. `Diff scout`: enumerates tests in scope.
2. `Burden-of-proof reviewer`: applies the rubric to each review chunk.
3. `Applier`: applies verdicts and prepares the final handoff.

### Reviewer subagent prompt

Pass the contents of [`reviewer-prompt.md`](reviewer-prompt.md) to the
reviewer subagent verbatim — do not paraphrase, summarize, or modify the
rubric.

Inputs the reviewer receives alongside the prompt:
- The added test code (full text of new test files; for new test cases in
  existing files, the test bodies plus minimal surrounding context).
- The production code each test exercises (named imports/symbols followed to
  their definitions; enough to judge what assertion would flip under what
  mutation).
- Nothing else, including plans, task lists, context files, implementer
  rationale, or prior reviewer output.

## Context Discipline

You are the orchestrator. Delegate only the `Burden-of-proof reviewer` role to
a subagent. Perform `Diff scout` and `Applier` yourself.

## Global constraints

- Treat the reviewer subagent's verdicts as authoritative for `delete`
  decisions. The orchestrator does not second-guess deletions on its own.
- Do not lower coverage thresholds or skip checks to clear failures. If a
  deletion causes a real coverage shortfall, surface it in the final handoff.

## Human checkpoints

- Required: ask when the diff contains zero added tests — the skill has no
  work to do; confirm before exiting silently.
- Required: ask when the project has no discoverable test command in
  `COMMANDS.md` and no obvious convention — applier cannot verify deletions.
- Required: ask when applying deletions would empty a brand-new test file
  whose presence the user clearly intended (file added by user, not agent).
- Stay autonomous on all per-test `delete` verdicts within scope.

## Workflow

### Phase 1: Enumerate added tests (Diff scout)

1. Run `git status --porcelain` and `git diff --cached`, `git diff`, and
   `git ls-files --others --exclude-standard` to find:
   - new test files (untracked or staged)
   - new test cases inside otherwise-pre-existing test files, according to
     the project's discovered test conventions
2. Read `COMMANDS.md` to identify the test command for the verification
   step.
3. Track each added test by file path, test name, and line range.

### Phase 2: Burden-of-proof review (Burden-of-proof reviewer)

1. Group added tests into review chunks (one test file or a small cluster
   of related files per chunk).
2. For each chunk, invoke the reviewer subagent with the contents of
   `reviewer-prompt.md` and the chunk inputs above. The subagent must be a
   fresh invocation with no carryover from this conversation.
3. Track each verdict with `Location`, `Name`, `Verdict`, `Justification`,
   and `Coverage Gap`. `Justification` is the `mutation` for `keep` and the
   `reason` for `delete`; `Coverage Gap` is the reviewer's `coverage_gap` or
   `None`.

### Phase 3: Apply deletions (Applier)

1. Delete each `delete`-verdict test:
   - if a whole test file becomes empty, delete the file
   - if a test case within a larger file is deleted, remove the test case
     and any imports/fixtures that become unused as a result
2. Run the project's test command (from `COMMANDS.md`). Track the actual
   command and observed result.
3. If unrelated tests break, stop and surface the failure — do not
   "fix forward" by re-adding deleted tests or weakening assertions.

### Phase 4: Survival check (Applier)

1. Compute `survival = keep_count / total_added_tests`.
2. If `survival > 0.90`, flag the run as suspect and note that the reviewer
   may be rubber-stamping. Recommend re-running with stricter rubric framing
   or splitting chunks more aggressively.
3. Otherwise, record the survival ratio for the audit trail.

### Phase 5: Surface surviving coverage gaps (Applier)

Track surviving coverage gaps from non-null reviewer `coverage_gap` values.
Record no gap for null values; if null conflicts with the reviewer
`reason`, surface the inconsistency instead of inventing a gap.

## Guardrails

- Do not preserve a test on the strength of the implementer's narrative.
  The reviewer never saw that narrative, and the orchestrator must not
  reintroduce it.
- Do not "improve" surviving tests during this skill's run. Improvements
  belong to a separate pass.

## Final handoff

After completing the workflow:
1. State the total added tests and the kept/deleted counts.
2. Name the survival ratio and whether the suspect flag fired.
3. Summarize deletions applied and the test command/result.
4. If surviving coverage gaps exist, recommend running
   follow-up coverage work against the listed behaviors.

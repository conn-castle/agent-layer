---
name: prune-new-tests
description: >-
  Burden-of-proof review of tests added in the current uncommitted diff:
  auto-delete any new test that can't justify itself with a production-code
  mutation that would flip its assertion given the test's actual input. Use
  `audit-tests` for full-suite health; use `boost-coverage` to add tests.
---

# prune-new-tests

This skill prunes low-value tests that the implementing agent added as a side
effect of implementation. Each newly added test must defend its existence with
a **concrete mutation in production code that would flip the assertion given
the test's actual input**. Tests that cannot are auto-deleted. Tests that
target real behavior but pass for the wrong reason are also deleted in this
prune-only pass and reported as surviving coverage gaps, not hardened in place.

Use `audit-tests` instead when the target is the full existing suite. Use
`boost-coverage` instead when the goal is to **add** tests to raise coverage.

## Defaults

- Default scope is **tests added in the current uncommitted diff only**
  (staged, unstaged, and untracked test files). Pre-existing tests are never
  touched.
- Default disposition is **delete unless justified**. The implementing agent
  did not earn a test's place by writing it; the test must demonstrate it can
  catch a defect.
- Tests are auto-deleted in place — no human approval per test. Surviving
  coverage gaps are reported, never backfilled with replacement assertions.

## Inputs

Accept any combination of:
- explicit paths or test files (still must intersect the added-test set)
- a per-test override list to keep without review
- a dry-run flag to produce the verdict report without deleting

## Required artifact

Write the report to:
- `.agent-layer/tmp/prune-new-tests.<run-id>.report.md`

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>`.
Create the file with `touch` before writing.

## Multi-agent pattern

Required roles:
1. `Diff scout`: enumerates every test added in the current uncommitted diff
   (added test files + new test cases inside existing files).
2. `Burden-of-proof reviewer` (fresh-context subagent): applies the rubric to
   each review chunk without inheriting the implementer's rationalizations.
3. `Applier`: deletes tests marked `delete` and writes the report.

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
- Nothing else. No plan, no task list, no context file, no implementer
  rationale, no prior reviewer output.

## Context Discipline

You are the orchestrator. Delegate only the `Burden-of-proof reviewer` role to
a fresh-context subagent. Perform the `Diff scout` and `Applier` roles yourself:
enumerate added tests, apply authoritative `delete` verdicts, run verification,
and write the report.

## Global constraints

- Operate only on tests **added** in the current uncommitted diff. Modified
  pre-existing tests are out of scope. Deleted tests are out of scope.
- Treat the reviewer subagent's verdicts as authoritative for `delete`
  decisions. The orchestrator does not second-guess deletions on its own.
- After applying deletions, run the project's repo-defined test command
  (consult `COMMANDS.md`) and observe the output. Record what ran and the
  outcome.
- Do not replace deleted tests with "stronger" tests in the same run. Report
  the resulting gap so a separate `boost-coverage` invocation can address it
  deliberately.
- Do not lower coverage thresholds or skip checks to clear failures. If a
  deletion causes a real coverage shortfall, surface it in the report.

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
3. List each added test by file path, test name, and line range in the
   report under `## Added Tests`.

### Phase 2: Burden-of-proof review (Burden-of-proof reviewer)

1. Group added tests into review chunks (one test file or a small cluster
   of related files per chunk).
2. For each chunk, invoke the reviewer subagent with the contents of
   `reviewer-prompt.md` and the chunk inputs above. The subagent must be a
   fresh invocation with no carryover from this conversation.
3. Collect the JSON-line verdicts into the report under `## Verdicts` as a
   table with columns `Location`, `Name`, `Verdict`, `Justification`,
   `Coverage Gap`. The `Justification` column shows the `mutation` for
   `keep` and the `reason` for `delete`. The `Coverage Gap` column shows
   the reviewer's `coverage_gap` for deleted tests that were trying to cover
   real behavior, or `None` when the deleted test targeted no real behavior.

### Phase 3: Apply deletions (Applier)

1. Delete each `delete`-verdict test:
   - if a whole test file becomes empty, delete the file
   - if a test case within a larger file is deleted, remove the test case
     and any imports/fixtures that become unused as a result
2. Run the project's test command (from `COMMANDS.md`). Record the actual
   command and the observed result in the report.
3. If unrelated tests break, stop and surface the failure — do not
   "fix forward" by re-adding deleted tests or weakening assertions.

### Phase 4: Survival check (Applier)

1. Compute `survival = keep_count / total_added_tests`.
2. If `survival > 0.90`, flag the run as suspect under `## Survival Check`
   and note that the reviewer may be rubber-stamping. Recommend re-running
   with stricter rubric framing or splitting chunks more aggressively.
3. Otherwise, record the survival ratio for the audit trail.

### Phase 5: Report surviving coverage gaps (Applier)

For every deleted test with a non-null reviewer `coverage_gap`, list the
intended production behavior and the missing discriminating signal under
`## Surviving Coverage Gaps` so a follow-up `boost-coverage` run can address it
deliberately. For deleted tests with a null `coverage_gap`, record no surviving
gap; the verdict already records that the test targeted no real behavior. If a
null `coverage_gap` conflicts with the reviewer `reason`, surface the
inconsistency instead of inventing a gap. Do **not** fabricate replacement
assertions in this run.

## Required report structure

Write `.agent-layer/tmp/prune-new-tests.<run-id>.report.md` with:

1. `# Prune New Tests Summary`
2. `## Scope`
3. `## Added Tests`
4. `## Verdicts`
   - table: `| Location | Name | Verdict | Justification | Coverage Gap |`
5. `## Deletions Applied`
   - one bullet per file or test case actually removed
6. `## Test Run`
   - the exact command, observed exit status, and any unexpected breakage
7. `## Survival Check`
   - kept / total, ratio, and the suspect flag if `> 0.90`
8. `## Surviving Coverage Gaps`
   - behaviors that lost coverage and warrant a `boost-coverage` follow-up

## Guardrails

- Do not delete pre-existing tests, even when they look low-value. That is
  `audit-tests`'s domain and requires explicit opt-in.
- Do not preserve a test on the strength of the implementer's narrative.
  The reviewer never saw that narrative, and the orchestrator must not
  reintroduce it.
- Do not "improve" surviving tests during this skill's run. Improvements
  belong to a separate pass.
- Do not collapse multiple deleted tests into a single replacement test.
  That re-introduces agent-authored speculative coverage.
- Do not skip Phase 3 verification. A green test command is part of the
  contract.

## Definition of done

- The report exists at `.agent-layer/tmp/prune-new-tests.<run-id>.report.md`
  with every required section (`Scope`, `Added Tests`, `Verdicts`,
  `Deletions Applied`, `Test Run`, `Survival Check`, `Surviving Coverage
  Gaps`).
- Every added test in scope appears in the `Verdicts` table with one
  verdict, required justification field, and coverage-gap field populated.
- Every `delete` verdict is reflected by a matching entry under `Deletions
  Applied` and the corresponding test file or test case is gone from the working
  tree.
- `Test Run` records the exact command and its observed result; deletions
  did not introduce unrelated test failures.
- The survival ratio is recorded; if `> 0.90`, the suspect flag is set and
  the recommended next step is named.

## Final handoff

After writing the report:
1. Echo the report path.
2. State the total added tests and the kept/deleted counts.
3. Name the survival ratio and whether the suspect flag fired.
4. If `Surviving Coverage Gaps` is non-empty, recommend running
   `boost-coverage` against the listed behaviors.

# prune-uncommitted-tests

This asset prunes low-value test changes in the current uncommitted working
tree.

## Defaults

- Default scope is **test changes in the current uncommitted diff**: staged,
  unstaged, and untracked test files; new test cases inside pre-existing test
  files; and modified test cases or assertions inside pre-existing test files.
  Deleted tests and unchanged surrounding tests are out of scope.
- Default disposition is **remove the uncommitted test change unless
  justified**. Auto-apply without per-test approval. Delete newly added tests;
  for modified pre-existing tests, remove or revert only the uncommitted test
  change when the baseline can be reconstructed. Surface surviving coverage
  gaps without backfilling replacement assertions.

## Inputs

Accept any combination of:
- explicit paths or test files (still must intersect the changed-test set)
- a per-test override list to keep without review
- a dry-run flag to produce verdicts without deleting

## Multi-agent pattern

Required roles:
1. `Diff scout`: enumerates changed tests in scope.
2. `Burden-of-proof reviewer`: applies the rubric to each review chunk.
3. `Applier`: applies verdicts and prepares the final handoff.

### Reviewer subagent prompt

Pass the contents of
[`prune-uncommitted-tests-reviewer-prompt.md`](prune-uncommitted-tests-reviewer-prompt.md)
to the built-in reviewer subagent verbatim — do not paraphrase, summarize, or
modify the rubric.

Inputs the reviewer receives alongside the prompt:
- The changed test code (full text of new test files; for new or modified test
  cases in existing files, the changed test bodies plus minimal surrounding
  context and enough before/after diff to apply a delete verdict safely).
- The production code each test exercises (named imports/symbols followed to
  their definitions; enough to judge what assertion would flip under what
  mutation).
- Nothing else, including plans, task lists, context files, implementer
  rationale, or prior reviewer output.

## Context Discipline

You are the orchestrator for this skill. Delegate only the
`Burden-of-proof reviewer` role to a built-in subagent. Perform `Diff scout`
and `Applier` yourself, then reconcile the returned review.

## Global constraints

- Treat the reviewer subagent's verdicts as authoritative for `delete`
  decisions. The orchestrator does not second-guess deletions on its own.
- Do not lower coverage thresholds or skip checks to clear failures. If a
  deletion causes a real coverage shortfall, surface it in the final handoff.

## Human checkpoints

- Required: ask when the diff contains zero in-scope test changes — the skill
  has no work to do; confirm before exiting silently.
- Required: ask when the project has no discoverable test command in
  `COMMANDS.md` and no obvious convention — applier cannot verify deletions.
- Required: ask when applying deletions would empty a brand-new test file whose
  presence the user explicitly requested.
- Stay autonomous on all per-test `delete` verdicts within scope.

## Workflow

### Phase 1: Enumerate uncommitted test changes (Diff scout)

1. Run `git status --porcelain` and `git diff --cached`, `git diff`, and
   `git ls-files --others --exclude-standard` to find:
   - new test files (untracked or staged)
   - new test cases inside otherwise-pre-existing test files, according to
     the project's discovered test conventions
   - modified test cases or assertions inside otherwise-pre-existing test
     files
2. Read `COMMANDS.md` to identify the test command for the verification
   step.
3. Track each changed test by file path, test name, line range, and change kind
   (`new-file`, `new-case`, or `modified-case`).

### Phase 2: Burden-of-proof review (Burden-of-proof reviewer)

1. Group changed tests into review chunks (one test file or a small cluster
   of related files per chunk).
2. Build every chunk's immutable input packet before launching reviewers.
3. Invoke one fresh built-in reviewer subagent per chunk concurrently with the
   contents of `prune-uncommitted-tests-reviewer-prompt.md` and the chunk inputs
   above. Launch all reviewers before waiting, join all results, and only then
   enter the single Applier phase. No reviewer may edit the working tree.
4. Track each verdict with `Location`, `Name`, `Verdict`, `Justification`,
   and `Coverage Gap`. `Justification` is the `mutation` for `keep` and the
   `reason` for `delete`; `Coverage Gap` is the reviewer's `coverage_gap` or
   `None`.

### Phase 3: Apply removals (Applier)

1. Apply each `delete` verdict:
   - if a whole test file becomes empty, delete the file
   - if a test case within a larger file is deleted, remove the test case
     and any imports/fixtures that become unused as a result
   - if a pre-existing test case was only modified, restore the pre-change
     body/assertion for the changed portion without deleting unrelated existing
     coverage
   - if the pre-change content cannot be reconstructed from the diff and
     current repo state, stop and surface the blocker
2. Run the project's test command (from `COMMANDS.md`). Track the actual
   command and observed result.
3. If unrelated tests break, stop and surface the failure — do not
   "fix forward" by re-adding deleted tests or weakening assertions.

### Phase 4: Survival check (Applier)

1. Compute `survival = keep_count / total_changed_tests`.
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
1. State the total changed tests and the kept/deleted counts.
2. Name the survival ratio and whether the suspect flag fired.
3. Summarize removals applied and the test command/result.
4. If surviving coverage gaps exist, recommend running
   follow-up coverage work against the listed behaviors.

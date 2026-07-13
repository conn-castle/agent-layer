# prune-uncommitted-tests

Review uncommitted test changes once and remove changes that do not provide
meaningful behavioral coverage.

## Scope

The default target is staged, unstaged, and untracked test changes:

- new test files
- new test cases in existing files
- modified test bodies or assertions

Deleted tests, unchanged surrounding tests, committed history, and unrelated
tests are out of scope. Explicit paths must intersect this target.

Optional inputs:

- test changes to keep without review
- dry-run mode, which reports verdicts without editing

## Review standard

Keep a test change only when its actual input and assertion would detect a
concrete defect in production behavior. The test must exercise the relevant
branch or value and use an independently meaningful expected result.

Remove or revert changes that merely:

- restate setup, mocks, constants, or the implementation under test
- assert existence, absence, or lack of failure without proving the intended
  condition caused the result
- recheck constraints already enforced statically
- pass arguments through mocks without testing behavior
- use inputs that bypass the claimed scenario
- move with production data or logic and therefore cannot detect divergence

When a weak test was aimed at real behavior, record the missing discriminating
signal as a coverage gap. Do not invent replacement assertions during this
pass.

## Workflow

### 1. Inventory changed tests

Inspect the combined uncommitted diff and identify each changed test by file,
name, and change type. Read the relevant production code and `COMMANDS.md`
before judging behavior or selecting a check.

If there are no in-scope test changes, return `not-applicable`.

### 2. Decide once

For each changed test, record:

- `keep`: the concrete production defect it would detect
- `remove`: why it fails the review standard and any surviving coverage gap

Honor explicit keep overrides. Use repository evidence rather than implementer
rationale or reviewer consensus.

### 3. Apply removals

Unless dry-run mode is active:

- delete a new test that receives `remove`
- restore only the uncommitted portion of a modified existing test
- remove imports or fixtures made unused by those edits
- delete a new test file if no kept content remains, unless the user explicitly
  required that file to exist

If baseline content cannot be reconstructed safely, report the concrete blocker
instead of guessing.

### 4. Check and report

Run the narrowest credible test command covering the affected area. If the
cleanup itself causes a failure, repair the cleanup or restore the responsible
removal; do not weaken other tests or production behavior.

Return:

- `outcome`: `not-applicable`, `completed`, `dry-run`, or `blocked`
- total, kept, and removed counts
- kept and removed test changes with reasons
- surviving coverage gaps
- the focused command and result
- any blocker or residual risk

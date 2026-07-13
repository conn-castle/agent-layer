# prune-uncommitted-tests

Remove uncommitted test changes that do not provide meaningful behavioral
coverage.

## Scope

Review new or modified test files, cases, bodies, and assertions in the staged,
unstaged, and untracked tree. Deleted tests, unchanged surrounding tests,
committed history, and unrelated tests are outside scope. Accept explicit keep
overrides and a dry-run mode.

## Standard

Keep a test only when its inputs and independently meaningful assertions can
detect a concrete production defect. Remove changes that merely restate setup,
mocks, constants, implementation details, or statically enforced constraints;
assert only existence or lack of failure; bypass the claimed scenario; or move
with production logic so they cannot detect divergence.

When a weak test targets real behavior, record the missing signal as a coverage
gap. Do not invent a replacement during this pruning pass.

## Workflow

1. Inventory changed tests and read the relevant production contract. Consult
   COMMANDS.md before choosing checks. Return `not-applicable` when none exist.
2. Decide `keep` or `remove` for each change from repository evidence. Record
   the defect a kept test detects or why a removed test fails the standard.
3. Unless dry-run, remove only the uncommitted test content and imports or
   fixtures it made unused. Delete a new file when no kept content remains. If
   baseline content cannot be reconstructed safely, stop rather than guess.
4. Run the narrowest credible affected test command. Repair or restore any
   removal that causes a failure; do not weaken production behavior or other
   tests.

Return `not-applicable`, `completed`, `dry-run`, or `blocked`, with kept and
removed counts and reasons, remaining coverage gaps, check evidence, and any
blocker.

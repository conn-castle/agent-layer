---
name: boost-coverage
description: >-
  Raise coverage iteratively by discovering the repo's real coverage commands,
  selecting the next eligible low-coverage file, adding tests, and repeating
  until the documented threshold is met or a high-value blocker requires human
  input.
---

# boost-coverage

Increase coverage with small, reviewable iterations.
Default behavior is:
- discover the real coverage contract first
- improve one eligible file per iteration
- stop only when further improvement no longer looks worthwhile or a real blocker prevents trustworthy progress

## Defaults

- Use the repo-defined threshold from CI, coverage config, or `DECISIONS.md`.
- Treat the threshold as the minimum floor, not an automatic stop, when this skill is explicitly invoked.
- If the user names a target file, use it for the next iteration after validating eligibility.
- Otherwise choose the eligible file with the lowest trustworthy line coverage.
- Prefer test-only changes. Allow only small behavior-preserving seams when they are required for testability.

## Inputs

Accept any combination of:
- a target file
- a coverage domain or component
- an iteration cap
- a verification depth preference
- whether approved command discoveries should be persisted to `COMMANDS.md`

## Required artifact

Write the report to:
- `.agent-layer/tmp/boost-coverage.<run-id>.report.md`

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>`.
Create the file with `touch` before writing.

## Multi-agent pattern

Use subagents liberally when available.

Recommended roles:
1. `Coverage scout`: identifies domains, commands, and threshold sources.
2. `Coverage runner`: produces the current per-file coverage view.
3. `Target selector`: chooses the next eligible file.
4. `Test designer`: derives meaningful cases and edge conditions.
5. `Implementer`: adds or updates tests.
6. `Verifier`: re-runs coverage and confirms improvement.

## Global constraints

- Do not guess coverage commands, thresholds, or domain boundaries.
- Do not change production behavior to inflate coverage.
- Keep each iteration focused on one target file unless the repo's test runner forces a broader command.
- Keep tests deterministic and aligned with repo conventions.

## Human checkpoints

- Required: ask when the coverage command set or domain model is ambiguous enough that the results would be untrustworthy.
- Required: ask when no threshold is documented.
- Required: ask before installing missing tooling.
- Stay autonomous for normal test-writing and re-run iterations once the coverage contract is clear.

## Coverage workflow

### Phase 0: Preflight (Coverage scout)

1. Confirm baseline repo state with `git status --porcelain`.
2. Read `COMMANDS.md` before choosing any coverage or verification command.
3. Read `DECISIONS.md` only if the threshold is not already obvious from coverage tooling or CI configuration.

### Phase 1: Discover the coverage contract (Coverage scout)

1. Identify:
   - repo or component coverage commands
   - working directories
   - output format or stable textual source
   - exclusions that affect file eligibility
2. Classify confidence:
   - `high`: commands and scope are explicit
   - `medium`: plausible but incomplete mapping
   - `low`: no trustworthy contract
3. Proceed autonomously only on `high`.

### Phase 2: Resolve blockers before execution (Coverage scout)

Stop and ask when any of these are true:
- confidence is `medium` or `low`
- the threshold is missing
- required tooling is missing

When asking, provide:
- the proposed commands
- the domain scope
- the missing threshold or tooling detail
- the smallest decision the human needs to make

### Phase 3: Build the current coverage table (Coverage runner)

1. Run the documented coverage command.
2. Normalize the result into:
   - domain
   - file
   - line coverage percent
   - optional lines total or missed lines
3. Exclude obvious noise:
   - tests
   - fixtures
   - generated code
   - mocks
   - thin entrypoints with no real logic

### Phase 4: Choose the next target (Target selector)

1. Validate a user-supplied target when present.
2. Otherwise select the eligible file with the lowest trustworthy coverage.
3. Record:
   - why the file is eligible
   - coverage before
   - why it was selected over nearby candidates

### Phase 5: Design and implement tests (Test designer + Implementer)

Cover:
- primary behavior
- guard clauses
- error paths
- branch-heavy logic
- important boundary cases

If better testability needs a seam:
- keep it small
- preserve behavior
- escalate first if it stops being obviously mechanical

### Phase 6: Verify improvement (Verifier)

1. Re-run the smallest credible coverage command that proves improvement.
2. Confirm:
   - the target file improved
   - the repo or domain moved toward the threshold
3. Refresh the coverage table before deciding the next iteration.

### Phase 7: Iterate or stop (Coverage scout)

Continue only when:
- the next target still looks meaningful
- the last iteration improved coverage
- eligible files remain

If the repo is still below threshold, keep going until the threshold is met or blocked.
If the repo is already above threshold, keep going while meaningful improvements remain or until the user-imposed iteration cap is reached.

Otherwise stop and report the real blocker or diminishing-return reason.

## Required report structure

Write `.agent-layer/tmp/boost-coverage.<run-id>.report.md` with:

1. `# Coverage Summary`
2. `## Threshold and Commands`
3. `## Iterations`
4. `## Coverage Before and After`
5. `## Remaining Blockers or Diminishing Returns`

## Guardrails

- Do not pad coverage with trivial tests that add little behavioral confidence.
- Do not keep iterating when coverage is no longer improving.
- Do not silently swap to a weaker coverage command.
- Do not claim threshold success without the observed command output.

## Final handoff

After writing the report:
1. Echo the report path.
2. Summarize the threshold used, commands run, and files improved per iteration.
3. Call out coverage before and after, plus any blocked decision or shortfall that prevented reaching the threshold.

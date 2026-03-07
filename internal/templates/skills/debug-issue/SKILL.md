---
name: debug-issue
description: >-
  Investigate a reported bug or unexpected behavior from symptom to root cause:
  reproduce, narrow down, diagnose, write a failing test, fix, and verify. Use
  when the root cause is unknown and investigation is needed before a fix can be
  planned.
---

# debug-issue

This is the investigation-first debugging workflow.
It should:
- start from a symptom, not an assumed cause
- reproduce the problem with observable evidence before attempting any fix
- narrow the cause systematically instead of guessing
- capture the bug in a failing test before writing the fix
- fix the root cause, not the symptom

## Defaults

- Default mode is full investigate-and-fix. If the user asks for diagnosis only, stop after Phase 4 and log the finding to `ISSUES.md`.
- Default scope is a single bug. If investigation reveals multiple distinct bugs, fix only the original and log the others.
- Default verification depth is the repo's documented check lane.
- Prefer the smallest reproduction that isolates the bug over a broad scenario.

## Inputs

Accept any combination of:
- a symptom description (error message, stack trace, unexpected output, user report)
- steps to reproduce when known
- suspect files or areas
- a git range or commit where the bug was introduced
- whether the run is diagnosis-only or investigate-and-fix

## Required artifacts

Use one shared `run-id = YYYYMMDD-HHMMSS-<short-rand>`.

Create:
- `.agent-layer/tmp/debug-issue.<run-id>.report.md`

Create the file with `touch` before writing.

## Multi-agent pattern

Use subagents liberally when available.

Recommended roles:
1. `Repo scout`: gathers context — related code, recent changes, test history, and memory files.
2. `Reproducer`: builds and runs a minimal reproduction.
3. `Investigator`: traces execution, narrows the cause, tests hypotheses.
4. `Test writer`: writes the failing test that captures the bug.
5. `Fixer`: implements the root-cause fix.
6. `Verifier`: runs the repo check lane and confirms the fix.

## Global constraints

- Do not attempt a fix before the bug is reproduced and the root cause is identified.
- Do not guess the root cause — form hypotheses and test them with observable evidence.
- Do not widen scope to fix unrelated problems discovered during investigation.
- Do not modify test expectations to make a failing test pass unless the expectation is genuinely wrong.
- Keep investigation traces in the report, not scattered across the codebase.
- Log newly discovered unrelated issues to `ISSUES.md` instead of fixing them inline.

## Human checkpoints

- Required: ask when the symptom description is too vague to form any testable hypothesis.
- Required: ask when investigation reveals that the fix requires a breaking change, broad refactor, or architectural decision.
- Required: ask when multiple plausible root causes remain after investigation and the correct one depends on intended behavior that is not documented.
- Stay autonomous through reproduction, hypothesis testing, narrowing, test writing, and fixing when the cause is clear.

## Investigation workflow

### Phase 0: Preflight (Repo scout)

1. Confirm baseline with:
   - `git status --porcelain`
   - `git diff --stat`
2. Read, in order, when they exist:
   - `COMMANDS.md`
   - `ISSUES.md` (check if this bug is already tracked)
   - `DECISIONS.md`
3. Restate the symptom in one sentence.
4. If the symptom matches an existing `ISSUES.md` entry, note it and continue — the investigation may still add value by finding the root cause.

### Phase 1: Reproduce (Reproducer)

1. If the user provided reproduction steps, follow them exactly first.
2. If not, construct a minimal reproduction:
   - prefer a short command, script, or rough test that triggers the symptom
   - the goal is confirming the bug exists, not writing the definitive test (that comes in Phase 4)
3. Confirm the bug is observable:
   - capture the actual output, error, or behavior
   - capture the expected output or behavior
4. If the bug cannot be reproduced:
   - document what was tried
   - escalate to the user with specific questions about environment, data, or steps

Do not proceed past this phase without a confirmed reproduction.

### Phase 2: Narrow the cause (Investigator)

Use the smallest set of techniques needed to isolate the root cause:

- **Recent changes**: check `git log` for recent changes to suspect areas.
- **Bisect**: when a git range is available or the bug is a regression, use `git bisect` or manual binary search to find the introducing commit.
- **Trace**: read the code path from input to failure point. If targeted debug logging is temporarily needed, remove it before Phase 5.
- **Hypothesize and test**: form a specific hypothesis, test it with a targeted change or observation, and record the result before moving on.

Choose techniques based on the available evidence. Not all techniques are needed for every investigation.

Record each hypothesis and its outcome in the report. Do not skip straight to a fix based on a hunch.

### Phase 3: Diagnose (Investigator)

State the root cause with evidence:
- what is wrong (the actual defect)
- where it is (file, function, line)
- why it produces the observed symptom
- when it was introduced if identifiable (commit or change)

If the root cause is ambiguous after investigation, escalate with the competing hypotheses and what evidence would distinguish them.

### Phase 4: Write the failing test (Test writer)

1. If the Phase 1 reproduction is already an automated test that precisely captures the bug, refine it to meet the criteria below instead of writing a second test.
2. Write a test that:
   - fails on the current code (red)
   - captures the exact bug behavior
   - will pass once the fix is applied
   - follows repo test conventions
3. Run the test and confirm it fails for the right reason.
4. If diagnosis-only mode was requested, stop here:
   - log the finding to `ISSUES.md` with the root cause, test location, and next step
   - write the report
   - when no broader orchestrator already owns closeout, use the `finish-task` skill
   - hand off

### Phase 5: Fix the root cause (Fixer)

1. Fix the root cause, not the symptom.
2. Keep the fix minimal and focused on the diagnosed defect.
3. If the correct fix is materially larger than a targeted change, escalate before proceeding.

If the fix touches scope that accumulates obvious local complexity or dead scaffolding:
- use the `simplify-code` skill
- then continue to Phase 6

### Phase 6: Verify (Verifier)

1. Run the previously failing test and confirm it passes (green).
2. Run the repo's documented check lane to confirm no regressions.
3. If checks fail for reasons unrelated to the fix, log them and do not mask them.

When no broader orchestrator already owns closeout, use the `finish-task` skill here.
If it reveals stale memory or incomplete verification, jump back to the earliest affected phase.

## Required report structure

Write `.agent-layer/tmp/debug-issue.<run-id>.report.md` with:

1. `# Debug Summary`
   - symptom
   - outcome (fixed, diagnosed-only, blocked)
2. `## Reproduction`
   - steps or test used to reproduce
   - observed vs expected behavior
3. `## Investigation`
   - hypotheses tested with outcomes
   - narrowing technique used
4. `## Root Cause`
   - defect location and explanation
   - introducing commit if identified
5. `## Fix`
   - what changed and why
   - test added
6. `## Verification`
   - commands run and results
7. `## Follow-up`
   - related issues logged
   - remaining concerns

## Guardrails

- Do not skip reproduction and jump straight to "I think I see the problem."
- Do not claim a root cause without evidence from the investigation.
- Do not write the fix before the failing test exists.
- Do not treat a passing test suite as proof the bug is fixed unless the specific failing test now passes.
- Do not expand investigation into a general code review or cleanup.
- Do not silently drop the investigation if reproduction fails — escalate with what was tried.

## Final handoff

After writing the report:
1. Echo the report path.
2. Summarize: the symptom, root cause, fix applied, and test added.
3. State whether the bug is fixed, diagnosed-only, or blocked and why.

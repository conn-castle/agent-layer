---
name: debug-and-fix-issue
description: >-
  Investigate a reported bug from symptom to root cause: reproduce, narrow,
  diagnose, write a failing test, then delegate a proportional repair and
  verification. Use when the cause is unknown and investigation must precede a
  fix.
---

# debug-and-fix-issue

Reproduce the symptom, prove the root cause, capture the failing test or
blocker, then delegate a direct or planned repair.

## Inputs

Required:

- Before starting: symptom description specific enough for a testable
  hypothesis
- Before planning in full investigate-and-fix mode: `implementer`, `fixer`, and
  `plan_reviewers` for `/plan-work` and `/fully-implement-plan`

Optional:

- reproduction steps
- expected and actual behavior, error message, stack trace, or user report
- suspect paths
- regression range or introducing commit
- diagnosis-only mode

If a required input is missing at its boundary, checkpoint. Do not invent
implementers, fixers, or plan reviewers.

## Artifact

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>`.

Create `.agent-layer/tmp/debug-and-fix-issue.<run-id>.report.md` before writing.
Keep all evidence and handoff details there; `/plan-work` uses it as the task
source.

## Guardrails

- Dispatch external roles through `/agent-dispatch`.
- Do not implement inline. After reproduction, evidenced root cause, and the
  failing test or blocker are recorded, delegate a bounded direct repair when
  it is concrete and local; use `/plan-work` then `/fully-implement-plan` only
  for an exceptionally significant repair.
- Diagnose from observable evidence; record each hypothesis and outcome.
- Do not change test expectations unless the expectation is genuinely wrong.
- Fix standard: repair the evidenced root cause first. Add defensive hardening
  only after that, and only when it preserves failure visibility.
- Treat retries, sleeps, timeout bumps, broad catches, error demotion or
  silencing, ignored validation failures, and weaker assertions as blockers
  unless the diagnosis proves they are the product-required response to a
  documented transient boundary.
- If the root cause remains unknown, say the issue is not fixed and hand off
  only diagnostic instrumentation that captures the missing facts.
- Do not stage, commit, discard, or destructively rewrite changes unless the
  user explicitly asks.

## Checkpoints

Checkpoint with the current step, report path, evidence gathered, blocker, and
smallest needed user input when:

- the symptom cannot support a testable hypothesis
- targeted attempts cannot reproduce the bug
- intended behavior is undocumented and affects diagnosis
- multiple plausible root causes remain
- the fix requires a breaking change, broad refactor, or architecture decision
- `implementer`, `fixer`, or `plan_reviewers` is missing before planning
- a direct implementer/fixer/verifier or `/plan-work` or
  `/fully-implement-plan` checkpoints, fails, omits required artifact paths,
  or omits the needed verdict

## Workflow

### Step 1: Preflight

1. Run `git status --porcelain` and `git diff --stat`.
2. Read `COMMANDS.md`, `ISSUES.md`, and `DECISIONS.md` when they exist.
3. Restate the symptom in one sentence.
4. If the symptom matches an `ISSUES.md` entry, note it and continue.

### Step 2: Reproduce

1. Follow supplied reproduction steps exactly first.
2. Otherwise construct a minimal command, script, or rough test that triggers
   the symptom.
3. Capture observed and expected behavior.
4. Checkpoint if targeted attempts cannot reproduce the bug; do not proceed
   without confirmed reproduction.

### Step 3: Narrow

Use only the techniques the evidence warrants:

- **Recent changes**: inspect `git log` for suspect areas.
- **Bisect**: use `git bisect` or manual binary search when a range exists or
  the bug is a regression.
- **Trace**: follow the code path from input to failure; remove temporary debug
  logging before planning.
- **Hypothesize and test**: record each hypothesis and outcome in the report.

### Step 4: Diagnose

Trace the causal chain to the evidenced code-level defect before naming the
root cause; this usually takes 3-5 why-levels.

State the root cause with:

- the defect
- the file, function, and line when available
- the causal chain from symptom to root cause
- the introducing commit or change, if identifiable
- tempting bandaids that would only mask the symptom, and why they are rejected

If the root cause remains ambiguous, ground it before checkpointing with:

- research online for known root causes in the relevant dependency, runtime,
  framework, error message, or platform behavior
- run throwaway experiments under `.agent-layer/tmp` to drive unknowns to ground
- identify exact diagnostic instrumentation that would separate the remaining
  hypotheses

If the root cause still cannot be proven, update the report with reproduction
evidence, competing hypotheses, missing facts, and diagnostic-only handoff.

### Step 5: Write the Failing Test

Skip only when the root cause remains unknown; record why no root-cause failing
test can be written yet.

1. Refine the reproduction if it is already an automated test; otherwise
   write a focused test.
2. Confirm the test fails on current code for the diagnosed bug.
3. Update the report with the test path, command, and failing output.
4. In diagnosis-only mode, log the finding to `ISSUES.md`, write the report,
   use `/finish-task` when no broader orchestrator owns closeout, and stop.

### Step 6: Choose the Repair Path

Choose based on fix complexity and decision risk, not bug severity alone.

- `direct`: the evidenced root cause and bounded fix are concrete, local,
  behaviorally clear, and safely verified by the failing test plus focused
  affected checks.
- `planned`: use the full planning path only when the repair is exceptionally
  significant: cross-cutting, behavior-changing, architecture-sensitive,
  ambiguous, or unsafe to bound directly.

Record the classification and one-line reason. A human checkpoint still wins
over either path.

### Step 7A: Delegate a Direct Repair

For a direct repair, dispatch the implementer with the debug report,
the original user request as the authoritative contract, the exact bounded fix
target, and the root-cause-first standard from this skill. Require it to make
the failing test pass, run focused affected checks, and write
`.agent-layer/tmp/debug-and-fix-issue.<run-id>.direct-repair.report.md`. Do not
create plan/task/context artifacts solely to repair this local defect.

Then dispatch the fixer for one non-iterative cleanup pass with:

```text
/clean-and-fix-code
plan_reviewers are {agent 1, agent 2, ...}
```

Finally, run one fresh built-in verifier with `/verify-work` against the
original user request/scope. Supply the debug report and implementation report
as evidence, and supply every resolved cleanup finding as supplemental
obligations. Supplemental obligations may add checks but must not weaken or
replace the original request. Record the implementation report, cleanup review
and planned artifact paths, verification report path, and verdict.

### Step 7B: Plan and Fully Implement a Significant Repair

For a planned repair, run:

```text
/plan-work
Task source:
.agent-layer/tmp/debug-and-fix-issue.<run-id>.report.md

Use the report's Reproduction, Investigation, Root Cause, and Failing Test
sections as source evidence.
Fix standard: repair the evidenced root cause first. Add defensive coding only
on top of that fix, and reject retries, sleeps, timeout bumps, broad catches,
error silencing, ignored validation failures, or weaker assertions unless the
report proves they are the product-required root-cause response.
If the report says the root cause is unknown, do not plan a fix. Plan only
targeted observability/logging that captures the missing facts, and make the
user-facing outcome explicit: diagnostic instrumentation added, issue not fixed.
plan_reviewers are {agent 1, agent 2, ...}
```

Record the `/plan-work` report path and the returned plan, task, and context
artifact paths. Then run:

```text
/fully-implement-plan
Plan artifacts:
{relative path to plan artifact}
{relative path to task artifact}
{relative path to context artifact}
implementer is {implementer}
fixer is {fixer}
plan_reviewers are {agent 1, agent 2, ...}
```

Record the `/fully-implement-plan` report path, implementation attempts,
cleanup rounds, verification verdict, stop reason, and residual risk.

### Step 8: Close Out

Write the final report. If no broader orchestrator owns closeout, use
`/finish-task`.

## Report and Handoff

Report headings:

1. `# Debug Summary` — symptom; outcome: fixed, diagnosed-only, blocked, or
   delegated-blocked
2. `## Inputs` — supplied inputs; missing required inputs, if any
3. `## Reproduction` — steps or test used; observed vs expected behavior
4. `## Investigation` — hypotheses tested; narrowing technique used
5. `## Root Cause` — causal chain; defect location; introducing change, or
   explicit unknown-root-cause status with remaining hypotheses and missing
   facts
6. `## Failing Test` — test path; command; failing output summary, or why no
   root-cause failing test can be written yet
7. `## Planning Handoff` — `/plan-work` report path; plan, task, context paths;
   direct-repair classification or blocker, if any
8. `## Implementation and Verification` — direct implementation, cleanup, and
   verification report paths, or the `/fully-implement-plan` report path;
   verdict; stop reason; residual risk; explicitly say "not fixed" when only
   diagnostic instrumentation was added
9. `## Follow-up` — related issues logged; remaining concerns

Final chat handoff: report path; symptom; root cause; failing test;
the direct-repair report paths or `/plan-work` and `/fully-implement-plan`
report paths; outcome and why. If the root cause is unknown, say the issue is
not fixed and summarize the diagnostics added for the next occurrence.

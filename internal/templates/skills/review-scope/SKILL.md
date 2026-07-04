---
name: review-scope
description: >-
  Review explicit files, directories, diffs, uncommitted changes, or proactive
  hotspots for correctness, gaps, risks, architecture, tests, docs, performance,
  reliability, and maintainability. Not used to review plans.
---

# review-scope

Run a targeted review or proactive hotspot audit and write a findings report.
This skill is broader than a code review.
Do not use it for pre-execution plan/task artifact critique. Use
`multi-agent-plan-review` for plan-review paths.
Use it for:
- staged or unstaged diffs
- all uncommitted changes
- specific files or directories
- repo slices that need an audit
- proactive issue-hunting when the user asks for a codebase audit without naming exact files

## Defaults

Target resolution order:
1. Use explicit target if provided.
2. If the user asks for a proactive audit without naming targets, use hotspot mode.
3. Otherwise, default to all uncommitted changes.
4. If none of the above yields a target, stop and ask instead of silently reviewing unrelated history.

Output is report-only. Findings must be evidence-backed, prioritized, and reviewable.

## Required artifact

Write the report to:
- `.agent-layer/tmp/review-scope.<run-id>.report.md`

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>`.
Create the file with `touch` before writing.

## Target selection

Supported targets:
- explicit files or directories from the user
- staged changes: `git diff --cached`
- unstaged changes: `git diff`
- all uncommitted changes: staged + unstaged
- last commit, but only when the user explicitly asks for it: `git show --name-only --pretty="" HEAD`
- a git range
- a proactive hotspot set selected by the reviewer when the user asks for a codebase audit without naming exact targets

State the actual target set at the top of the report.

For proactive hotspot mode, keep the review scope narrow and state why each hotspot was selected. Preferred signals:
- recently changed or unstable core files
- oversized or high-churn modules
- code paths with weak or missing tests
- TODO/FIXME or temporary scaffolding markers
- reliability-sensitive entrypoints or data boundaries
- code that appears to drift from `README.md`, `ROADMAP.md`, or `DECISIONS.md`

## Multi-agent review pattern

Recommended review lenses:
1. `Correctness reviewer`
   - logic bugs
   - broken assumptions
   - bad failure handling
2. `Architecture reviewer`
   - layering problems
   - responsibility drift
   - needless complexity
3. `Quality reviewer`
   - missing tests
   - docs drift
   - maintainability/performance/reliability gaps
4. `Maintainer reviewer`
   - awkward UX
   - future support burden
   - safety or migration risk

Run at least two review agents in parallel for non-trivial scopes, then synthesize their findings.

## Review standards

When relevant, align the review against:
- `README.md`
- `ROADMAP.md`
- `DECISIONS.md`
- `ISSUES.md`
- `COMMANDS.md`
- explicitly supplied plan/task artifacts

If a likely issue is already tracked in `ISSUES.md`, do not present it as a novel finding. Instead, mark it as an existing known issue if it materially affects the target.

## Global constraints

- Do not modify code or docs in this workflow.
- Do not silently widen the review target beyond what the user asked for.
- Keep findings evidence-backed and tied to the actual reviewed scope.

## Human checkpoints

- Required: ask when no credible review target can be established from the explicit scope, proactive-audit request, or documented defaults.
- Required: ask before turning findings into code edits, doc edits, or issue logging.
- Stay autonomous during the review itself.

## Review workflow

### Phase 1: Gather context (Lead reviewer)

1. Determine the review target using the defaults resolution order.
2. Read only the minimum surrounding context needed to understand the target.
3. Apply the target-appropriate review focus:
   - **Proactive hotspot mode**: select a small hotspot set, state the selection signals, keep the audit reviewable rather than repo-wide.
   - **Code or diffs**: correctness first, then architecture and operability, then tests/docs and maintainability.

### Phase 2: Evaluate with multiple lenses (Parallel review agents)

Use an adversarial posture: actively try to falsify the change or target,
challenge assumptions, and look for hidden coupling, edge cases, and failure
modes. Keep only evidence-backed findings; do not report invented issues or
low-signal nits.

Assess at least these categories when relevant:
- correctness and edge cases
- architecture and ownership boundaries
- test coverage and verification gaps
- docs drift and operator guidance gaps
- performance or concurrency risks
- data safety and destructive behavior risks
- maintainability and unnecessary complexity

### Phase 3: Record only high-signal findings (Synthesizer)

Each finding must include:
- `Title`
- `Severity`: Critical | High | Medium | Low
- `Confidence`: High | Medium | Low
- `Location`: exact file/path/scope
- `Why it matters`
- `Evidence`
- `Recommendation`

Do not write low-signal style nits.

## Required report structure

The report must contain:

1. `# Review Summary`
   - target reviewed
   - review mode used
   - short outcome summary
2. `## Findings`
   - findings first, ordered by severity
3. `## Open Questions`
   - only unresolved items that block confidence
4. `## Strengths`
   - short list of notable things done well
5. `## Suggested Next Steps`
   - a small number of coherent follow-up actions
6. `## Self-Check`
   - Re-read each finding against the actual code and report evidence, then answer these four questions in writing:
     - Is this a root-cause recommendation, not a band-aid?
     - Is the evidence concrete and tied to actual code, not inferred behavior?
     - Is the severity calibrated to real impact, not theoretical risk?
     - Am I recommending work outside the reviewed scope as though it were a finding?
   - Demote or drop findings that fail any check before finalizing the report.

## Guardrails

- Do not claim certainty when evidence is weak.
- Do not invent runtime behavior you did not observe.
- Do not collapse multiple unrelated problems into one vague finding.
- Do not recommend large refactors unless the current approach is clearly unsafe or unsound.
- Do not silently expand a no-target review into the last commit.
- Do not silently widen a proactive audit into the whole repository.
- If a finding depends on an assumption, say so explicitly.

## Definition of done

- The report exists at `.agent-layer/tmp/review-scope.<run-id>.report.md` with every required section (`Summary`, `Findings`, `Open Questions`, `Strengths`, `Suggested Next Steps`, `Self-Check`).
- The `Self-Check` section contains written answers to all four questions for every remaining finding; any finding that failed a check was demoted or dropped before finalizing.
- Every finding names location, severity, confidence, evidence, and recommendation tied to the actual reviewed scope.
- The report was not used to edit code, docs, or issue files; the handoff ends with one of `proceed`, `proceed after fixes`, or `revise first`.

## Final handoff

After writing the report:
1. Echo the report path.
2. Summarize the top findings in chat.
3. Say whether you recommend:
   - proceed
   - proceed after fixes
   - revise first

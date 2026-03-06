---
name: review-scope
description: >-
  Review explicit files, directories, diffs, uncommitted changes, or a
  proactive hotspot set and produce a findings report covering correctness,
  gaps, risks, architecture, tests, docs, performance, reliability, and
  maintainability. Use the `review-plan` skill instead when the target is
  specifically a plan/task artifact pair.
---

# review-scope

Run a targeted review or proactive hotspot audit and write a findings report.
This skill is broader than a code review.
It can review plan/task artifacts when explicitly targeted, but `review-plan`
is the dedicated pre-execution plan-review path.
Use it for:
- explicit plan/task artifacts
- staged or unstaged diffs
- all uncommitted changes
- specific files or directories
- repo slices that need an audit
- proactive issue-hunting when the user asks for a codebase audit without naming exact files

## Defaults

- Default mode is explicit-scope review.
- Default target for explicit-scope review is all uncommitted changes.
- If the user explicitly asks for a proactive audit and provides no target, switch to hotspot mode instead of asking for files first.
- If there are no uncommitted changes, no explicit target, and no proactive-audit request, stop and ask for a target instead of silently reviewing unrelated history.
- Default output is report-only. Do not modify code.
- Prefer evidence-backed findings over broad commentary.
- Findings must be prioritized and reviewable.

## Required artifact

Write the report to:
- `.agent-layer/tmp/review-scope.<run-id>.report.md`

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>`.
Create the file with `touch` before writing.

## Target selection

Supported targets:
- explicit plan file and optional task file
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

Use subagents liberally when available.

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

Run at least two reviewers in parallel for non-trivial scopes, then synthesize their findings.

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
- Use the `review-plan` skill instead of this one for pre-execution plan/task critique.

## Human checkpoints

- Required: ask when no credible review target can be established from the explicit scope, proactive-audit request, or documented defaults.
- Required: ask before turning findings into code edits, doc edits, or issue logging.
- Optional: ask when the requested target is nominally a plan/task pair but the desired outcome is ambiguous between `review-plan` and a broader audit.
- Stay autonomous during the review itself.

## Review workflow

### Phase 1: Gather context (Lead reviewer)

1. Determine the review mode:
   - explicit-scope review
   - proactive hotspot audit
2. Determine the review target.
3. Read only the minimum surrounding context needed to understand the target.
4. If the target is explicit plan/task artifacts:
   - check scope coverage
   - dependency ordering
   - risk coverage
   - verification credibility
   - missing docs/tests/memory updates
5. If the target is proactive hotspot mode:
   - select a small hotspot set
   - state the selection signals
   - keep the audit reviewable rather than repo-wide by default
6. If the target is code or diffs:
   - check correctness first
   - then architecture and operability
   - then tests/docs and maintainability

### Phase 2: Evaluate with multiple lenses (Parallel reviewers)

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

## Guardrails

- Do not claim certainty when evidence is weak.
- Do not invent runtime behavior you did not observe.
- Do not collapse multiple unrelated problems into one vague finding.
- Do not recommend large refactors unless the current approach is clearly unsafe or unsound.
- Do not silently expand a no-target review into the last commit.
- Do not silently widen a proactive audit into the whole repository.
- If a finding depends on an assumption, say so explicitly.

## Final handoff

After writing the report:
1. Echo the report path.
2. Summarize the top findings in chat.
3. Say whether you recommend:
   - proceed
   - proceed after fixes
   - revise first

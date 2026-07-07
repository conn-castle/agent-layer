---
name: review-uncommitted-code
description: >-
  Review explicit code targets, including files, directories, diffs,
  uncommitted changes, or proactive hotspots, for correctness, gaps, risks,
  architecture, tests, docs, performance, reliability, and maintainability.
---

# review-uncommitted-code

Run a targeted code review and write a findings report. Do not modify files.

## Defaults

Target resolution order:

1. Use an explicit target if provided.
2. If the user asks for a proactive audit without naming targets, use hotspot
   mode.
3. Otherwise, default to all uncommitted changes.
4. If none of the above yields a target, stop and ask instead of silently
   reviewing unrelated history.

## Required artifact

Write the report to:

- `.agent-layer/tmp/review-uncommitted-code.<run-id>.report.md`

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>`. Create the file before writing.

## Target selection

Supported targets:

- explicit files or directories from the user
- staged changes: `git diff --cached`
- unstaged changes: `git diff`
- all uncommitted changes: staged plus unstaged
- last commit, only when the user explicitly asks for it:
  `git show --name-only --pretty="" HEAD`
- a git range
- a proactive hotspot set selected by the reviewer when the user asks for a
  codebase audit without naming exact targets

State the actual target set at the top of the report.

For proactive hotspot mode, keep the review scope narrow and state why each
hotspot was selected. Preferred signals:

- recently changed or unstable core files
- oversized or high-churn modules
- code paths with weak or missing tests
- TODO/FIXME or temporary scaffolding markers
- reliability-sensitive entrypoints or data boundaries
- code that appears to drift from `README.md`, `ROADMAP.md`, or `DECISIONS.md`

## Multi-agent pattern

Use built-in subagents as independent review lenses when available. Keep target
selection, final judgment, and report synthesis with the current agent.

For non-trivial scopes, run at least two review agents in parallel, then
synthesize their findings.

Review lenses:

1. `Correctness reviewer`: correctness, edge cases, assumptions, failure
   handling.
2. `Architecture reviewer`: architecture, interface/API boundaries, ownership
   boundaries, responsibility drift, unnecessary complexity.
3. `Quality reviewer`: test coverage, verification gaps, docs drift, operator
   guidance, maintainability, performance, concurrency.
4. `Maintainer reviewer`: reliability-sensitive entrypoints, data safety,
   destructive behavior, user experience, supportability, migration risk.

## Review workflow

Use an adversarial posture: try to falsify the reviewed code against its
intended behavior, boundaries, and relevant repo constraints.

Before synthesizing the final report, load and apply
`assets/finding-verdict-classification.md`. The report is a reviewer's
recommendation, not the final resolution authority.

### Phase 1: Establish target and context

1. Resolve and record the target using the defaults resolution order.
2. Read only the minimum surrounding context needed to understand the target.
3. Apply the target-appropriate review focus:
   - proactive hotspot mode: use the selected hotspot signals
   - explicit target mode: correctness first, then architecture and
     operability, then tests, docs, and maintainability

### Phase 2: Evaluate with independent review lenses

Evaluate the target through the review lenses. When relevant, compare it
against `README.md`, `ROADMAP.md`, `DECISIONS.md`, `ISSUES.md`, and
`COMMANDS.md`.

If a likely issue is already tracked in `ISSUES.md`, do not present it as a new
finding. Mark it as an existing known issue when it materially affects the
target.

### Phase 3: Synthesize findings

Treat review findings as candidates until they have been verified against the
current repo state. Classify every candidate finding under exactly one
recommended verdict from `assets/finding-verdict-classification.md`.

Each finding must include:

- `Title`
- `Severity`: Critical | High | Medium | Low
- `Confidence`: High | Medium | Low
- `Location`: exact file/path/scope
- `Recommended verdict`: Accept | Reject | Defer | Already Resolved
- `Why it matters`
- `Evidence`
- `Recommendation`

Findings must be concrete, prioritized, and tied to the reviewed scope. Do not
write style nits.

## Required report structure

The report must contain:

1. `# Review Summary`
   - target reviewed
   - review mode used
   - short outcome summary
2. `## Findings`
   - start with: `Verdicts are reviewer recommendations, not final resolution.`
   - group every candidate finding under:
     - `### Recommended Accept`
     - `### Recommended Reject`
     - `### Recommended Defer`
     - `### Recommended Already Resolved`
   - order findings within each group by severity, then confidence
   - use `None` for empty groups
3. `## Open Questions`
   - only unresolved items that block confidence
4. `## Strengths`
   - short list of notable things done well
5. `## Suggested Next Steps`
   - a small number of coherent follow-up actions
6. `## Self-Check`
   - For every recommended accepted finding, answer:
     - Is this a root-cause recommendation, not a band-aid?
     - Is the evidence concrete and tied to actual code?
     - Is the severity calibrated to real impact?
     - Am I recommending work outside the reviewed scope as though it were a
       finding?

Move findings that fail the self-check to the appropriate non-accepted verdict
group instead of dropping them silently.

## Human checkpoints

- Ask when no credible review target can be established.
- Ask before turning findings into code edits, doc edits, or issue logging.
- Stay autonomous during the review itself.

## Guardrails

- Do not claim certainty when evidence is weak.
- Do not invent runtime behavior you did not observe.
- Do not collapse multiple unrelated problems into one vague finding.
- Do not recommend large refactors unless the current approach is clearly
  unsafe or unsound.
- If a finding depends on an assumption, say so explicitly.

## Definition of done

- The report exists with every required section.
- The `Findings` section includes all four recommended verdict groups and
  states that verdicts are recommendations.
- The `Self-Check` section contains written answers for every recommended
  accepted finding.
- Every finding names location, severity, confidence, evidence, and
  recommendation tied to the reviewed scope.

## Final handoff

After writing the report:

1. Echo the report path.
2. Summarize the top recommended accepted findings in chat.
3. Say whether you recommend `proceed`, `proceed after fixes`, or
   `revise first`.

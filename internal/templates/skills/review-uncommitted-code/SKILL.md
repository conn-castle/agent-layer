---
name: review-uncommitted-code
description: >-
  Targeted, report-only code review of files, directories, diffs, git ranges,
  uncommitted changes, or proactive hotspots for correctness, gaps, risks,
  architecture, tests, docs, performance, reliability, and maintainability.
---

# review-uncommitted-code

Write a findings report only; do not modify reviewed files.

## Target selection

Resolve targets in order:

1. User-specified files, directories, diffs, or git ranges. For staged or
   unstaged-only review, use `git diff --cached` or `git diff`.
2. Proactive hotspots, only when the user asks for a codebase audit without
   exact targets.
3. Otherwise, review all uncommitted changes: staged plus unstaged.
4. If no credible target exists, stop and ask.

Review the last commit only when explicitly requested:
`git show --name-only --pretty="" HEAD`.

For proactive hotspots, keep scope narrow and record each selection signal:

- recently changed or unstable core files
- oversized or high-churn modules
- code paths with weak or missing tests
- TODO/FIXME or temporary scaffolding markers
- reliability-sensitive entrypoints or data boundaries
- code that appears to drift from `README.md`, `ROADMAP.md`, or `DECISIONS.md`

## Required artifact

Create `.agent-layer/tmp/review-uncommitted-code.<run-id>.report.md` before
writing. Use `run-id = YYYYMMDD-HHMMSS-<short-rand>`.

## Required asset

During synthesis, load `assets/finding-verdict-classification.md`. After
writing deduplicated unclassified candidates to the report, run one built-in
subagent as the `Finding verdict classifier`. Pass the report path, this asset,
and the minimum cited evidence needed to classify candidates; the classifier
updates the report in place.

Do not assign recommended verdicts yourself or manually move findings between
verdict groups. If the classifier omits candidates, loses evidence, or produces
invalid structure, rerun it with the concrete mismatch.

## Workflow

### Phase 1: Preflight

1. Resolve and record the target, review mode, report path, and proactive
   hotspot signals when applicable.
2. Read the minimum surrounding code, tests, docs, and memory files needed to
   understand the target.

### Phase 2: Review

Use built-in subagents as independent review lenses when available. Keep target
selection, candidate synthesis, and classifier orchestration with the current
agent. For non-trivial scopes, run at least two review agents in parallel.

Review lenses:

1. `Correctness reviewer`: edge cases, assumptions, failure handling.
2. `Architecture reviewer`: boundaries, ownership, responsibility drift,
   unnecessary complexity.
3. `Quality reviewer`: tests, docs, maintainability, performance, concurrency.
4. `Maintainer reviewer`: reliability-sensitive entrypoints, data safety,
   destructive behavior, user experience, supportability, migration risk.
5. `Adversarial reviewer`: intended behavior, boundary inputs, failure modes,
   implicit contracts, missing evidence.

Check the target against intended behavior, repo constraints, error paths, data
boundaries, verification coverage, and docs or memory drift. If a likely issue
is already tracked in `ISSUES.md`, mark it as an existing known issue instead
of presenting it as new.

### Phase 3: Synthesize findings

Treat reviewer output as candidates until checked against the current repo.
Write deduplicated unclassified candidates with all fields below except
`Recommended verdict`, then run the classifier. Final findings must include:

- `Title`
- `Severity`: Critical | High | Medium | Low
- `Confidence`: High | Medium | Low
- `Location`: exact file/path/scope
- `Recommended verdict`: Accept | Reject | Defer | Already Resolved
- `Why it matters`
- `Evidence`
- `Recommendation`

Style-only, unsupported, out-of-scope, or already-tracked candidates must stay
visible for classifier review. Do not silently drop weak candidates.

## Required report structure

The report must contain:

1. `# Review Summary`: target, mode, short outcome.
2. `## Findings`: start with `Verdicts are reviewer recommendations, not final
   resolution.` Group every candidate under `### Recommended Accept`,
   `### Recommended Reject`, `### Recommended Defer`, or
   `### Recommended Already Resolved`; order by severity, then confidence; use
   `None` for empty groups.
3. `## Open Questions`: only items that block confidence.
4. `## Suggested Next Steps`: a small coherent action list.
5. `## Self-Check`: one line for every accepted finding covering root cause,
   evidence, severity, and reviewed scope.

If an accepted finding fails self-check, rerun the classifier with the concrete
mismatch instead of moving it yourself or dropping it silently.

## Human checkpoints

- Ask when no credible review target can be established.
- Ask before turning findings into code edits, doc edits, or issue logging.

## Guardrails

- Do not claim certainty beyond the evidence.
- Keep unrelated problems in separate findings.
- Recommend large refactors only when the current approach is clearly
  unsafe or unsound.
- If a finding depends on an assumption, say so explicitly.

## Definition of done

Done when the report exists, every candidate is classified into exactly one
verdict group, accepted findings pass self-check, and each finding names
location, severity, confidence, evidence, and recommendation tied to scope.

## Final handoff

After writing the report:

1. Echo the report path.
2. Summarize top recommended accepted findings.
3. Recommend `proceed`, `proceed after fixes`, or `revise first`.

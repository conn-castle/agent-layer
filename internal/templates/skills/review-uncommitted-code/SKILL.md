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
3. Otherwise, review all uncommitted changes: staged, unstaged, and untracked
   files from `git ls-files --others --exclude-standard`.
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

## Conditional classification asset

During synthesis, load `assets/finding-verdict-classification.md` and use its
rubric. The current synthesizer may classify unambiguous, evidence-backed
candidates directly.

After gathering, deduplicating, and validating the full candidate set, identify
every candidate with conflicting reviewer conclusions, ambiguous evidence,
uncertain scope or checkpoint ownership, or Critical/High severity that needs
an independent verdict gate. If that gated set is non-empty, run at most one
fresh built-in subagent as the `Finding verdict classifier` for the entire set
in one batch. Pass the gated candidates, report path, asset, and minimum cited
evidence. Validate its updates; if any are missing or invalid, classify those
candidates with the rubric in the current synthesizer and record the classifier
failure instead of launching it again.

## Workflow

### Phase 1: Preflight

1. Resolve and record the target, review mode, report path, and proactive
   hotspot signals when applicable. When reviewing all uncommitted changes,
   include staged diff, unstaged diff, and untracked file contents as one target.
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
Deduplicate and validate candidates, classify unambiguous evidence-backed
candidates with the asset rubric, and collect every gated candidate for the
single conditional classifier batch before finalizing the report. Final
findings must include:

- `Title`
- `Severity`: Critical | High | Medium | Low
- `Confidence`: High | Medium | Low
- `Location`: exact file/path/scope
- `Recommended verdict`: Accept | Reject | Defer | Already Resolved
- `Why it matters`
- `Evidence`
- `Recommendation`

Style-only, unsupported, out-of-scope, or already-tracked candidates must stay
visible through synthesis and receive a rubric-backed verdict. Do not silently
drop weak candidates.

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

If an accepted finding fails self-check, reassess it with the rubric. Do not
launch another classifier; preserve the candidate and record any unresolved
ambiguity as `Defer`.

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
verdict group, every gated candidate was included in the single classifier batch
when one was needed, accepted findings pass self-check, and each finding names
location, severity, confidence, evidence, and recommendation tied to scope.

## Final handoff

After writing the report:

1. Echo the report path.
2. Summarize top recommended accepted findings.
3. Recommend `proceed`, `proceed after fixes`, or `revise first`.

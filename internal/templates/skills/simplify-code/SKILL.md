---
name: simplify-code
description: >-
  Assess code complexity, remove dead code, simplify overly complex functions,
  and split files that mix unrelated responsibilities. Uses complexity metrics
  as diagnostic signals and applies judgment rather than rigid thresholds.
  Scoped to uncommitted changes when they exist, otherwise the full codebase.
---

# simplify-code

This is a complexity-reduction workflow, not a redesign workflow.
Default behavior is:
- assess complexity using measurable signals, then apply judgment
- keep changes behavior-preserving
- verify with repo-defined checks before stopping

## Philosophy

Complexity metrics (cyclomatic complexity, function length, nesting depth, file
size) are diagnostic signals, not pass/fail gates. A 1000-line file with one
clear responsibility may be the right solution. A 30-line function with deeply
nested conditionals may not be.

The question is always: **would simplification actually improve this code for a
reader and maintainer?** Use the metrics to find candidates, then decide with
judgment informed by:
- the language and its idioms
- the domain and its inherent complexity
- whether the code mixes unrelated responsibilities
- whether a reader can hold the logic in their head
- whether the current structure makes bugs easy to introduce or hide

Do not simplify code that is already clear. Do not add complexity in the name
of reducing a metric.

## Scope default

- If uncommitted changes exist (staged, unstaged, or untracked), scope to the
  changed files only.
- If no uncommitted changes exist, scope to the full codebase or user-supplied
  paths.
- The user can always override with explicit paths or file-type filters.

When scoped to the full codebase, focus effort on the worst offenders rather
than exhaustively rewriting everything. Identify, prioritize, and fix the
highest-value simplifications first.

## Defaults

- Default file-size target for splits is a judgment call, not a fixed number.
  Consider the language, the file's role, and whether the current size hurts
  navigability or understanding.
- Attempt dead-code removal first with trustworthy existing tooling; otherwise
  decide whether a credible tool proposal or agent-led evidence is the better
  fit.
- Prefer no-op on speculative simplification over risky "improvements."

## Inputs

Accept any combination of:
- explicit paths or directories
- file-type filters
- whether to attempt dead-code removal
- a maximum number of file splits
- whether to run in assessment-only mode (report without fixing)

## Required artifact

Write the report to:
- `.agent-layer/tmp/simplify-code.<run-id>.report.md`

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>`.
Create the file with `touch` before writing.

## Multi-agent pattern

Use subagents liberally when available.

Recommended roles:
1. `Repo scout`: finds commands, target files, and verification lanes.
2. `Complexity analyst`: measures and identifies simplification candidates.
3. `Dead-code analyst`: identifies safe removals with evidence.
4. `Simplifier`: performs function-level and local simplifications.
5. `Splitter`: plans and performs safe file splits.
6. `Verifier`: runs the fastest credible checks.

## Global constraints

- Never change behavior, public contracts, or intended control flow.
- Do not install new tooling without a credible proposal and human approval.
- Do not broaden scope with opportunistic refactors.
- Keep file splits logical and substantial rather than exploding files into
  fragments.
- Do not simplify code just because a metric is high; simplify when the
  complexity genuinely hurts readability, maintainability, or correctness risk.

## Human checkpoints

- Required: ask when "dead code" evidence is ambiguous or external references
  cannot be ruled out.
- Required: ask when no credible verification path exists for a non-trivial change.
- Required: ask when a simplification would change the public API surface or
  require callers to adapt.
- Stay autonomous for clear simplifications and straightforward verification.

## Simplification workflow

### Phase 0: Preflight (Repo scout)

1. Confirm baseline with:
   - `git status --porcelain`
   - `git diff --stat`
2. Read `COMMANDS.md` before selecting verification commands.
3. Determine the actual target scope:
   - if uncommitted changes exist, scope to changed files
   - if no uncommitted changes exist, scope to the full codebase or explicit
     paths
4. Identify the language(s) and any available complexity tooling in the project.

### Phase 1: Assess complexity (Complexity analyst)

Gather complexity signals across the target scope:
- cyclomatic complexity per function (when tooling is available or estimable)
- function length and nesting depth
- file size and responsibility count
- number of parameters, return paths, and branch density
- any language-specific complexity indicators

For each signal, identify candidates that stand out relative to their peers in
the codebase — not against an arbitrary threshold.

**Large file review (mandatory):** Identify every file in the target scope that
is notably large relative to its peers or the project norm. For each large
file, make an explicit decision:
- **keep:** the file has one clear responsibility and splitting would fragment
  understanding — state why
- **split:** the file mixes unrelated responsibilities or has clear structural
  boundaries that would improve navigability — plan the split

Record every large-file decision in the report, including files you decide to
keep. The goal is evidence that each large file was reviewed, not that every
large file was split.

Produce a prioritized list of simplification candidates, ordered by the
expected value of simplifying (how much clarity the change would add vs. the
risk and effort involved).

### Phase 2: Remove clearly dead code (Dead-code analyst)

Remove only when at least one of these is true:
- trusted dead-code tooling marks it unreachable
- local repo search shows no live references and the symbol is clearly private
- the code is obviously unused scaffolding in the current scope

If evidence is mixed, escalate instead of guessing.

When tooling is not available or not appropriate, use the strongest manual
evidence available:
- repo-wide reference search
- private symbol boundaries
- registration/reflection checks
- test coverage and entrypoint reachability
- roadmap or issue context showing the code path is obsolete

### Phase 3: Simplify flagged functions and code (Simplifier)

For each candidate from Phase 1, apply the simplification that best fits:
- reduce nesting by inverting conditions or using early returns
- extract a meaningful helper when a block has a clear single responsibility
  that the parent function should not own
- replace complex conditional chains with clearer structure
- flatten obvious local indirection
- clarify misleading names without semantic change
- remove stale comments that no longer describe the code
- tighten boundary validation when it is already implied by existing behavior

Do not:
- extract helpers for one-time operations just to shorten a function
- rename things for style preference alone
- add abstraction layers that increase the total amount of code to understand
- break apart a function that reads clearly as a linear sequence

### Phase 4: Split oversized files (Splitter)

For files marked **split** in Phase 1:
- split by responsibility, not by arbitrary line count
- ensure the result remains easy to navigate and understand
- keep splits logical and substantial — two cohesive files, not ten fragments
- update imports and references in the same pass

Do not split when:
- the file has one clear responsibility regardless of length
- the resulting layout would be harder to understand than the current one
- the split would create circular dependencies or awkward coupling

### Phase 5: Verify and re-assess (Verifier)

1. Run the fastest credible repo-defined checks.
2. Re-assess the target scope to confirm complexity was reduced where intended.
3. Stop and report if verification fails or no credible verification exists.

## Required report structure

Write `.agent-layer/tmp/simplify-code.<run-id>.report.md` with:

1. `# Simplification Summary`
2. `## Scope`
3. `## Complexity Assessment`
   - key metrics gathered
   - candidates identified and their ranking rationale
4. `## Large File Decisions`
   - every large file reviewed, with verdict (keep/split) and reasoning
5. `## Dead Code Removed`
6. `## Simplifications Applied`
7. `## File Splits`
8. `## Verification`
9. `## Deferred Opportunities`

## Guardrails

- Do not use simplification as cover for feature work or architecture changes.
- Do not rewrite stable code just because it looks old or a metric is high.
- Do not split files when the new layout is harder to understand than the old
  one.
- Do not delete code whose liveness depends on reflection, registration, or
  external contracts unless the evidence is explicit.
- Do not simplify code that is already clear just to improve a number.
- Do not add abstraction to reduce line count.

## Final handoff

After writing the report:
1. Echo the report path.
2. Summarize: complexity candidates found, dead code removed, simplifications
   applied, and file splits performed.
3. Call out large-file decisions, verification commands run, and any
   simplification opportunities deliberately deferred.

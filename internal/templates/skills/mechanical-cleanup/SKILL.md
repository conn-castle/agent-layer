---
name: mechanical-cleanup
description: >-
  Run a behavior-preserving cleanup pass: remove clearly dead code when tooling
  exists or can be credibly proposed, simplify small local complexity, and
  split oversized files only when the resulting structure stays reviewable and
  verifiable.
---

# mechanical-cleanup

This is a mechanical cleanup workflow, not a redesign workflow.
Default behavior is:
- stay inside the requested or touched scope
- keep changes behavior-preserving
- verify with repo-defined checks before stopping

## Defaults

- Default scope is touched files unless the user supplies paths or requests a broader pass.
- Default file-size target is 400 lines when a split is warranted.
- Attempt dead-code removal first with trustworthy existing tooling; otherwise decide whether a credible tool proposal or agent-led evidence is the better fit.
- Prefer no-op on speculative cleanup over risky “improvements.”

## Inputs

Accept any combination of:
- explicit paths
- a line-limit target
- whether to attempt dead-code removal
- a maximum number of file splits
- file-type filters

## Required artifact

Write the report to:
- `.agent-layer/tmp/mechanical-cleanup.<run-id>.report.md`

Use `run-id = YYYYMMDD-HHMMSS-<short-rand>`.
Create the file with `touch` before writing.

## Multi-agent pattern

Use subagents liberally when available.

Recommended roles:
1. `Repo scout`: finds commands, target files, and verification lanes.
2. `Dead-code analyst`: identifies safe removals with evidence.
3. `Code janitor`: performs small mechanical cleanups.
4. `Splitter`: plans and performs safe file splits.
5. `Verifier`: runs the fastest credible checks.

## Global constraints

- Never change behavior, public contracts, or intended control flow.
- Do not install new tooling without a credible proposal and human approval.
- Do not broaden scope with opportunistic refactors.
- Keep file splits logical and substantial rather than exploding files into fragments.

## Human checkpoints

- Required: ask when “dead code” evidence is ambiguous or external references cannot be ruled out.
- Required: ask when no credible verification path exists for a non-trivial cleanup.
- Stay autonomous for clear mechanical cleanups and straightforward verification.

## Cleanup workflow

### Phase 0: Preflight (Repo scout)

1. Confirm baseline with:
   - `git status --porcelain`
   - `git diff --stat`
2. Read `COMMANDS.md` before selecting verification commands.
3. Determine the actual target scope and file types.

### Phase 1: Discover the cleanup contract (Repo scout)

Identify:
- the best fast verification command
- whether dead-code tooling already exists
- which files are oversized relative to the chosen limit
- which files are safe candidates for mechanical cleanup

If no dead-code tooling exists:
- decide whether the project is amenable to one
- if yes, prepare the smallest credible tooling proposal and ask before installing
- if no, continue with agent-led dead-code detection

### Phase 2: Remove clearly dead code when evidence is strong (Dead-code analyst)

Remove only when at least one of these is true:
- trusted dead-code tooling marks it unreachable
- local repo search shows no live references and the symbol is clearly private
- the code is obviously unused scaffolding in the current scope

If evidence is mixed, escalate instead of guessing.

When tooling is not available or not appropriate, use the strongest manual evidence available:
- repo-wide reference search
- private symbol boundaries
- registration/reflection checks
- test coverage and entrypoint reachability
- roadmap or issue context showing the code path is obsolete

### Phase 3: Apply mechanical cleanup (Code janitor)

Allowed examples:
- clarify misleading names without semantic change
- remove stale comments
- flatten obvious local indirection
- extract small helpers without changing behavior
- tighten boundary validation when it is already implied by existing behavior

### Phase 4: Plan and perform safe file splits (Splitter)

Split only when:
- the file is materially oversized
- there is a clean structural boundary
- the result remains easy to navigate

Prefer splits by responsibility, not by arbitrary line count.

### Phase 5: Verify and re-scan (Verifier)

1. Run the fastest credible repo-defined checks.
2. Re-check oversized-file candidates in the touched scope.
3. Stop and report if verification fails or no credible verification exists.

## Required report structure

Write `.agent-layer/tmp/mechanical-cleanup.<run-id>.report.md` with:

1. `# Cleanup Summary`
2. `## Scope`
3. `## Cleanup Applied`
4. `## Dead Code Removed`
5. `## File Splits`
6. `## Verification`
7. `## Deferred Opportunities`

## Guardrails

- Do not use cleanup as cover for feature work or architecture changes.
- Do not rewrite stable code just because it looks old.
- Do not split files when the new layout is harder to understand than the old one.
- Do not delete code whose liveness depends on reflection, registration, or external contracts unless the evidence is explicit.

## Final handoff

After writing the report:
1. Echo the report path.
2. Summarize files cleaned, dead code removed, and file splits performed.
3. Call out verification commands run and any cleanup opportunities deliberately deferred.

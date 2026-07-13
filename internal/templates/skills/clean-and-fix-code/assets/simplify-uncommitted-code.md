# simplify-uncommitted-code

Review uncommitted production-code changes once and remove complexity that is
not justified by the behavior being delivered.

## Scope

The default target is staged, unstaged, and untracked production-code changes.
Tests, committed history, and pre-existing adjacent complexity are out of scope.
Explicit paths must intersect the changed production set.

Optional inputs:

- files within the target to skip
- dry-run mode, which reports findings without editing

## Review standard

Preserve requested behavior and prefer the smallest clear implementation that
delivers it. Report a finding only when removing the complexity would
meaningfully improve maintainability without changing observable behavior.

Look for:

- speculative options, flags, branches, compatibility shims, or fallbacks
- premature abstractions and single-caller indirection
- dead paths and guards for cases excluded by trusted internal guarantees
- clever or verbose control flow with a straightforward equivalent
- mixed responsibilities introduced by the current change
- half-finished scaffolding or abandoned TODOs

Do not report naming preferences, harmless local style, small duplication, or
hypothetical future concerns. Do not create a new abstraction to remove an old
one.

## Workflow

### 1. Inventory changed production code

Inspect the combined uncommitted diff and the minimum surrounding code needed to
understand it. Read `COMMANDS.md` before selecting focused checks.

If there are no in-scope production changes, return `not-applicable`.

### 2. Assess once

For each material simplification, record its location, current complexity,
simpler form, evidence that behavior is preserved, and maintainability benefit.
Reject a candidate when evidence does not establish behavioral equivalence.

### 3. Apply accepted simplifications

Unless dry-run mode is active, apply accepted findings sequentially against the
latest working tree. Update uncommitted tests only when directly required to
preserve the same behavior.

Stop for a user decision before changing a public contract, behavior,
architecture, scope, risk, or cost. Leave broader redesign and adjacent
pre-existing complexity untouched.

### 4. Check and report

Run the narrowest credible checks for the affected behavior. If a simplification
causes a failure, restore that simplification unless the failure exposes a
separate concrete defect within scope. Do not weaken tests or thresholds.

Return:

- `outcome`: `not-applicable`, `completed`, `dry-run`, or `blocked`
- accepted, applied, rejected, and restored simplifications
- focused commands and results
- any user-owned decision, blocker, or residual risk

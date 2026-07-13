# simplify-uncommitted-code

Remove complexity introduced by uncommitted production-code changes when the
same behavior has a materially clearer implementation.

## Scope and standard

Review staged, unstaged, and untracked production code. Tests, committed
history, and pre-existing adjacent complexity are outside scope. Accept paths
to skip and a dry-run mode.

Preserve requested behavior. Consider speculative options, compatibility
shims, needless indirection, dead paths, overly clever control flow, mixed
responsibilities, and unfinished scaffolding. Ignore naming preferences,
harmless style, small duplication, and hypothetical future concerns. Do not
replace one abstraction with another unless it clearly reduces the burden.

## Workflow

1. Inspect the changed code and enough surrounding context to understand its
   contract. Consult COMMANDS.md before selecting checks. Return
   `not-applicable` when no production changes exist.
2. Accept a simplification only with evidence of behavioral equivalence and a
   material maintainability benefit.
3. Unless dry-run, apply accepted changes sequentially against the latest tree.
   Update tests only when required to preserve the same behavior. Leave public
   contract changes, broader redesign, and adjacent pre-existing complexity
   untouched.
4. Run credible affected checks. Restore a simplification that causes a failure
   unless the failure proves a separate in-scope defect.

Return `not-applicable`, `completed`, `dry-run`, or `blocked`, with accepted,
applied, rejected, or restored simplifications, check evidence, and residual
risk.

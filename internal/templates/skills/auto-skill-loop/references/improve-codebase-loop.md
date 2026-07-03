# improve-codebase Loop Contract

Use when `worker_skill=improve-codebase`.

Dispatch the implementer with `/improve-codebase`. Give it the ledger's recent
branches, merged PRs, touched paths, deferred blockers, and exhausted lenses.
Instruct it to avoid recently improved areas unless it has concrete evidence.

Prefer high-value lenses: correctness, data loss, security, concurrency,
cancellation, parser/input robustness, test integrity, documentation accuracy,
dead code, dependency health, and meaningful coverage gaps.

Reject cosmetic churn, speculative abstraction, broad rewrites, and changes
whose only value is making the diff larger.

The implementer must return:
- scope and lens used
- concrete findings fixed
- rejected candidates and why
- blocker candidates, if any
- changed files and line stats
- verification run and result
- touched areas and suggested avoid list for the next iteration

Stack safe thin improvements on the same batch branch until the normal PR gate
or an exception is met.

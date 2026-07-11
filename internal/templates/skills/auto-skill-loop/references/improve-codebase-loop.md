# improve-codebase Loop Contract

Use when `worker_skill=improve-codebase`.

Dispatch the implementer with `/improve-codebase`. Give it the ledger's recent
branches, merged pull requests, touched paths, user-owned blockers, exhausted
lenses, and concrete evidence that should prevent repeated work.

Prefer high-value lenses: correctness, data loss, security, concurrency,
cancellation, parser and input robustness, test integrity, documentation
accuracy, dead code, dependency health, and meaningful coverage gaps.

Reject cosmetic churn, speculative abstraction, broad rewrites, and changes
whose only value is making the batch larger. Each worker invocation owns one
bounded sweep; the autonomous loop may select a different scope or lens on the
next invocation.

The implementer must return:

- selected scope and lens with evidence
- concrete findings fixed or deferred
- user-owned blocker candidates
- changed files and meaningful line statistics
- focused verification and result
- touched areas, exhausted lens, and justified next-skill recommendation

Stack safe, non-repeating improvements on the same batch branch until the
normal pull-request gate or an exception is met.

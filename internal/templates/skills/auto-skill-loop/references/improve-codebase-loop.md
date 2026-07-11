# improve-codebase Loop Contract

Use when `worker_skill=improve-codebase`.

Dispatch the implementer with `/improve-codebase`. Give it the ledger's recent
branches, merged pull requests, touched paths, user-owned blockers, completed
scope maps and lenses, and concrete evidence that should prevent repeated work.

Prefer high-value lenses: correctness, data loss, security, concurrency,
cancellation, parser and input robustness, test integrity, documentation
accuracy, dead code, dependency health, and meaningful coverage gaps.

Wide coverage requires enough non-overlapping investigators to give materially
different areas credible independent attention without overloading a context.
Do not minimize agent count at the expense of coverage or distinct
perspectives, and do not add agents merely to increase fan-out. Parallelize
substantial independent groups when their context or wall-clock benefit
justifies the additional agent cost.

Reject cosmetic churn, speculative abstraction, unjustified rewrites, and
changes whose only value is making the batch larger. Each worker invocation
owns one wide evidence pass over its declared scope, including local,
cross-boundary, and architectural findings. Do not invoke another whole-scope
sweep merely for confidence. A later invocation requires changed repository
evidence introduced after the completed sweep by other work, a materially
distinct declared lens not already covered, or accepted work that a concrete
prior blocker prevented from completing. Repairs made by the completed sweep do
not by themselves justify surveying the same scope again.

The implementer must return:

- declared scope, boundary coverage, and lenses with evidence
- local, cross-boundary, and architectural findings with terminal outcomes
- user-owned blocker candidates
- changed files and meaningful line statistics
- focused verification and result
- touched areas and completed scope/lens coverage

Stack safe, non-repeating improvements on the same batch branch until the
normal pull-request gate or an exception is met.

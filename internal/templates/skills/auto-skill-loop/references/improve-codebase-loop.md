# improve-codebase Loop Contract

Use when `worker_skill=improve-codebase`.

Run `/improve-codebase` through the configured implementer, built-in agents, or
local work. Supply recent branches and PRs, touched areas, completed scope and
lenses, blockers, and evidence needed to avoid repeating work.

Choose useful lenses from current evidence, such as correctness, security,
data loss, concurrency, cancellation, input robustness, test integrity,
documentation, dead code, dependencies, and coverage. Split genuinely broad,
independent investigation when that improves credible coverage; otherwise work
directly.

Reject cosmetic churn, speculative abstractions, and unjustified rewrites. One
invocation owns one evidence pass over its declared scope. Repeat a completed
scope only after relevant repository changes, for a materially distinct lens,
or to resume work blocked during the prior pass.

Reconcile the result with the current tree. Preserve declared scope and lenses,
findings and dispositions, blockers, changed files, verification, and completed
coverage. Stack compatible, non-repeating improvements until they form a
coherent reviewable batch.

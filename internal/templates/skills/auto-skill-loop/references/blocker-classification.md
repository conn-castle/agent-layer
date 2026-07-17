# Human Decisions and Recoverable Blockers

Decide one question for each candidate: does it require human input? Treat
source wording such as "human decision," proposed alternatives, and deferred
next steps as claims to verify, not as authoritative classifications.

Before answering yes, refresh the current facts and discard any alternative
that contradicts repository evidence or binding constraints, materially expands
beyond the requested scope, violates repository instructions, is dominated by
a safer option, or addresses only speculative future demand. Apply existing
repository defaults such as the smallest reversible change and avoiding
over-engineering. An ordinary, reversible internal implementation choice with
one safe in-scope answer remains agent-owned.

Human input is required only when two or more alternatives remain genuinely
viable after that check and the choice materially affects functionality, user
experience, product scope or priority, consequential long-term architecture or
cost, platform or continuous-integration policy, security, privacy, safety,
compatibility, migration, or a public contract. It is also required for
destructive or irreversible action, an unauthorized external write, credential,
approval, or authority reserved by the user or repo.

Facts, work ordering, breadth, agent or tool failure, and unmet external
conditions do not require human input. If evidence supports one safe answer,
take it. When an item cannot proceed, preserve useful branch or PR work, record
the unmet condition and when to reconsider it, and continue with independent
candidates. Ask the smallest remaining human questions only after a complete
pass finds no independent work, or when the user asks for status.

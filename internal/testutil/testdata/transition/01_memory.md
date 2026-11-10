# Project Memory

Use these files as needed for durable project context.

## Available files

- `docs/agent-layer/CONTEXT.md` — general-purpose cross-session project-specific context.
- `docs/agent-layer/DECISIONS.md` — otherwise-lost rationale that materially constrains future work.
- `docs/agent-layer/COMMANDS.md` — canonical, repeatable development workflow commands for this repository.
- `docs/agent-layer/ISSUES.md` — verified engineering problems deferred from current work; excludes features and speculative improvements.
- `docs/agent-layer/BACKLOG.md` — unscheduled end-user-visible features and tasks (distinct from issues; not refactors).

## Guidelines

- **Durable information only:** Memory is for information that is not derivable, ephemeral, or generic.
- **Canonical artifacts first:** Put current architecture in repository documentation and enforceable behavior in code, tests, schemas, or configuration. Do not duplicate those sources in memory.
- **Decision-log exception:** Add a decision entry only for future-guiding rationale that cannot be recovered from canonical documentation or the implementation. Importance alone does not justify duplicating a decision.
- **Current tracked work:** ISSUES.md and BACKLOG.md should reflect the current working tree; fixed issues and implemented backlog items no longer belong in them.
- **Concise entries:** Include only the detail needed to make an entry useful.

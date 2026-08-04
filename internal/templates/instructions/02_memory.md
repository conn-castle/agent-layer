# Project Memory

Use these files as needed for durable project context.

## Available files

- `docs/agent-layer/CONTEXT.md` — general-purpose project-specific context (domain concepts, architectural context, naming conventions, external dependencies, team norms).
- `docs/agent-layer/DECISIONS.md` — rolling log of important, non-obvious decisions (brief).
- `docs/agent-layer/COMMANDS.md` — canonical, repeatable development workflow commands for this repository (build, test, lint/format, typecheck, coverage, migrations, scripts).
- `docs/agent-layer/ISSUES.md` — verified engineering problems deferred from current work; excludes features and speculative improvements.
- `docs/agent-layer/BACKLOG.md` — unscheduled end-user-visible features and tasks (distinct from issues; not refactors).

## Guidelines

- **Durable information only:** Memory is for information that is not derivable, ephemeral, or generic.
- **High-signal decisions:** DECISIONS.md is for non-obvious decisions that are not apparent from code or documentation, not routine choices or best-practice adherence.
- **Current tracked work:** ISSUES.md and BACKLOG.md should reflect the current working tree; fixed issues and implemented backlog items no longer belong in them.
- **Concise entries:** Include only the detail needed to make an entry useful.

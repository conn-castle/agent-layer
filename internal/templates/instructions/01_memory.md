# Dedicated Memory Section (paste into system instructions)

## Memory files (authoritative, user-editable, agent-maintained)
- `docs/agent-layer/ISSUES.md` — deferred defects, maintainability refactors, technical debt, risks.
- `docs/agent-layer/BACKLOG.md` — unscheduled user-visible features and tasks (distinct from issues; not refactors).
- `docs/agent-layer/ROADMAP.md` — numbered phases; guides architecture and sequencing.
- `docs/agent-layer/DECISIONS.md` — rolling log of important, non-obvious decisions (brief).
- `docs/agent-layer/COMMANDS.md` — canonical, repeatable development workflow commands for this repository (build, test, lint/format, typecheck, coverage, migrations, scripts).
- `docs/agent-layer/CONTEXT.md` — general-purpose project-specific context (domain concepts, architectural context, naming conventions, external dependencies, team norms).

After this list, refer to memory files by filename only (ISSUES.md, BACKLOG.md, ROADMAP.md, DECISIONS.md, COMMANDS.md, CONTEXT.md).

**Formatting instructions are in each memory file.** Before writing to a memory file, open it and follow its “Purpose”/“Format” section and insertion markers.

## Global memory workflow rules
- **Read before planning:** Before making architectural or cross-cutting decisions, read ROADMAP.md, then scan DECISIONS.md, then check relevant entries in BACKLOG.md and ISSUES.md.
- **Read before running commands:** Before running or recommending project workflow commands (tests, coverage, build, lint, start services), consult COMMANDS.md first.
- **Initialize if missing:** If any memory file does not exist, ask the user before creating it. If approved and templates exist, copy `.agent-layer/templates/docs/<NAME>.md` into `<NAME>.md` and preserve headings and markers.
- **Preserve & deduplicate:** Treat existing entries as canonical; do not overwrite/reset memory files unless the user explicitly asks (warn about data loss). Search the target file before adding; merge or rewrite existing entries instead of adding near-duplicates.
- **Decision hygiene:** Only log non-obvious decisions that are not apparent from code or docs. Do not log routine choices or best-practice adherence; when in doubt, skip logging.
- **Write down deferred work:** If you discover something worth doing and you are not doing it now:
  - add it to ISSUES.md if it is a bug, maintainability refactor, technical debt, reliability/security concern, test coverage gap, performance concern, or other engineering risk;
  - add it to BACKLOG.md only if it is a new user-visible capability.
- **Keep files living:** When an issue is fixed, remove it from ISSUES.md. When a backlog item is implemented or scheduled into the roadmap, move it into ROADMAP.md and remove it from BACKLOG.md. Keep DECISIONS.md and COMMANDS.md current by updating and removing stale items.

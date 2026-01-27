# Dedicated Memory Section (paste into system instructions)

## Memory files (authoritative, user-editable, agent-maintained)
- `docs/agent-layer/ISSUES.md` — deferred defects, maintainability refactors, technical debt, risks.
- `docs/agent-layer/BACKLOG.md` — unscheduled user-visible features and tasks (distinct from issues).
- `docs/agent-layer/ROADMAP.md` — numbered phases; guides architecture and sequencing.
- `docs/agent-layer/DECISIONS.md` — rolling log of important decisions (brief).
- `docs/agent-layer/COMMANDS.md` — canonical, repeatable commands for this repository. Sections are team-defined.

After this list, refer to memory files by filename only (ISSUES.md, BACKLOG.md, ROADMAP.md, DECISIONS.md, COMMANDS.md).
These memory rules are canonical and are not repeated inside memory files or templates; those files should contain only headers, insertion markers, and entries.

## Global workflow rules

- **Read before planning:** Before making architectural or cross-cutting decisions, read `ROADMAP.md`, then scan `DECISIONS.md`, and then check relevant entries in `BACKLOG.md` and `ISSUES.md`.
- **Read before running commands:** Before running or recommending project commands (tests, coverage, build, lint, start services), check `COMMANDS.md` first. `COMMANDS.md` is only for development workflow commands (build, test, lint, format, coverage, migrations, scripts), not application or CLI usage documentation. If it is missing, ask the user before creating it by copying `.agent-layer/templates/docs/COMMANDS.md` into `COMMANDS.md`. If it is incomplete, use auto-discovery, ask the user only when needed, then update `COMMANDS.md` with the definitive approach.
- **Initialize if missing:** If any project memory file does not exist, ask the user before creating it. If approved, copy `.agent-layer/templates/docs/<NAME>.md` into `<NAME>.md` (preserve headings and markers).
  - If `.agent-layer/templates/docs/COMMANDS.md` does not exist, ask the user before creating `COMMANDS.md` with a minimal, readable structure and a single `<!-- ENTRIES START -->` insertion marker.
- **Preserve memory entries:** Treat existing entries as canonical; do not overwrite memory files unless the user explicitly asks to reset them. If asked to overwrite, warn about data loss.
- **Single blank line between entries:** Keep exactly one blank line between entries in all memory files.
- **Write down deferred work:** If you discover something worth doing and you are not doing it now:
  - Add it to `ISSUES.md` if it is a bug, maintainability refactor, technical debt, reliability, security, test coverage gap, performance concern, or other engineering risk.
  - Add it to `BACKLOG.md` only if it is a new user-visible capability.
- **Maintainability refactors are always issues:** Do not put refactors in `BACKLOG.md`.
- **BACKLOG is a backlog, not a schedule:** `BACKLOG.md` holds unscheduled features and tasks. Periodically move selected items into `ROADMAP.md` tasks, then remove them from `BACKLOG.md` to keep the backlog lean.
- **Prevent duplicates:** Search the target file before adding a new entry. Merge or rewrite existing entries instead of adding near-duplicates.
- **Keep files living:** When an issue is fixed, remove it from `ISSUES.md`. When a backlog item is implemented, remove it from `BACKLOG.md`. When a backlog item is scheduled into the roadmap, move it into `ROADMAP.md` and remove it from `BACKLOG.md` at that time.
- **Agent autonomy:** You may propose and apply updates to the roadmap, backlog, issues, decisions, and commands when it improves clarity and delivery, while keeping the documents compact.

## ISSUES.md
- Purpose: Deferred defects, maintainability refactors, technical debt, risks, and engineering concerns.
- Add an entry only when you are not fixing it now.
- Keep each entry 3 to 5 lines. Lines 2 to 5 must be indented by 4 spaces so they stay associated with the entry.

Line layout (3 to 5 lines):
- Line 1: `Issue YYYY-MM-DD abcdef:` plus a short title (use a leading `-` list item).
- Line 2: Priority (Critical, High, Medium, or Low) and area.
- Line 3: Description of the observed problem or risk.
- Line 4: Next step (smallest concrete next action).
- Line 5: Notes (optional dependencies or constraints).
- Keep exactly one blank line between entries.

Entry format:
```text
- Issue YYYY-MM-DD abcdef: Short title
    Priority: Critical, High, Medium, or Low. Area: <area>
    Description: <observed problem or risk>
    Next step: <smallest concrete next action>
    Notes: <optional dependencies or constraints>
```

## BACKLOG.md
- Purpose: Unscheduled user-visible features and tasks (distinct from issues).
- Add an entry only when it is a new user-visible capability or task.
- Keep each entry 3 to 5 lines. Lines 2 to 5 must be indented by 4 spaces so they stay associated with the entry.
- When a backlog item is scheduled into a roadmap phase, move it into `ROADMAP.md` and remove it from this file.
- When a backlog item is implemented, ensure it is removed from this file.

Line layout (3 to 5 lines):
- Line 1: `Backlog YYYY-MM-DD abcdef:` plus a short title (use a leading `-` list item).
- Line 2: Priority (Critical, High, Medium, or Low) and area.
- Line 3: Description of what the user should be able to do.
- Line 4: Acceptance criteria (clear condition to consider it done).
- Line 5: Notes (optional dependencies or constraints).
- Keep exactly one blank line between entries.

Entry format:
```text
- Backlog YYYY-MM-DD abcdef: Short title
    Priority: Critical, High, Medium, or Low. Area: <area>
    Description: <what the user should be able to do>
    Acceptance criteria: <clear condition to consider it done>
    Notes: <optional dependencies or constraints>
```

## ROADMAP.md
- Purpose: Phased plan of work; guides architecture decisions and sequencing.
- The roadmap is a single list of numbered phases.
- Do not renumber completed phases (phases marked with ✅).
- You may renumber incomplete phases when updating the roadmap (for example, to insert a new phase).
- Incomplete phases include Goal, Tasks (checkbox list), and Exit criteria sections.
- When a phase is complete:
  - Add a green check emoji to the phase heading: `## Phase N ✅ — <name>`.
  - Replace the phase content with a single bullet list summarizing what was accomplished (no checkbox list).
- There is no separate "current" or "upcoming" section. The phase list itself shows what is done vs not done.

Phase template (completed):
```markdown
## Phase N ✅ — <phase name>
- <Accomplishment summary bullet>
- <Accomplishment summary bullet>
```

Phase template (incomplete):
```markdown
## Phase N — <phase name>

### Goal
- <What success looks like for this phase, in 1 to 3 bullet points.>

### Tasks
- [ ] <Concrete deliverable-oriented task>
- [ ] <Concrete deliverable-oriented task>

### Exit criteria
- <Objective condition that must be true to call the phase complete.>
- <Prefer testable statements: “X exists”, “Y passes”, “Z is documented”.>
```

## DECISIONS.md
- Purpose: Rolling log of important decisions (brief).
- Add an entry only when a decision is non-obvious and materially affects future work (long-term constraints, deferrals, irreversible tradeoffs).
- Focus on decisions future developers/agents would not learn just by reading the code or config.
- Keep entries brief.
- Do not log choices that are obvious from the repo or standard defaults (for example, the selected database/framework when it is evident in code).
- Do not log decisions that have no future ramifications or simply restate best practices or existing instructions.
- Keep the oldest decisions near the top and add new entries at the bottom.
- Lines below the first line must be indented by 4 spaces so they stay associated with the entry.
- Keep exactly one blank line between entries.

Entry format:
```text
- Decision YYYY-MM-DD abcdef: Short title
    Decision: <what was chosen>
    Reason: <why it was chosen>
    Tradeoffs: <what is gained and what is lost>
```

## COMMANDS.md
- Purpose: Canonical, repeatable commands for this repository (tests, coverage, lint/format, typecheck, build, run, migrations, scripts).
- Keep entries concise and practical.
- Prefer commands that will be used repeatedly.
- Organize commands using headings that fit the repository. Create headings as needed.
- For each command, document purpose, command, where to run it, and prerequisites.
- When commands change, update this file and remove stale entries.
- If the repository is a monorepo, group commands per workspace/package/service and specify the working directory.
- Maintain `COMMANDS.md` without asking for confirmation when it improves future work.
- `COMMANDS.md` is for development workflow commands only; do not document application or CLI usage there.
- Only add commands that are expected to be used repeatedly, such as:
  - setup and installation, development server, build, lint and format, typecheck, unit and integration tests, coverage, database migrations, common scripts.
- Do not add one-off debugging commands (search/grep/find, ad-hoc scripts, temporary environment variables) unless they are a stable part of the workflow.
- Keep `COMMANDS.md` concise and structured, but **do not hard-code sections**. Sections are team-defined.
- When adding a command:
  - place it under the best existing heading, or create a new heading if none fits,
  - include purpose, command, where to run it, and prerequisites,
  - prefer headings named **Test** and **Coverage** when applicable (for discoverability), but do not require them.
- Deduplicate and update entries when commands change.

Entry format:
````text
- <Short purpose>
```bash
<command>
```
Run from: <repo root or path>  
Prerequisites: <only if critical>  
Notes: <optional constraints or tips>
````

# Dedicated Memory Section (paste into system instructions)

## Project memory files (authoritative, user-editable, agent-maintained)
- `docs/ISSUES.md` — deferred defects, maintainability refactors, technical debt, risks.
- `docs/FEATURES.md` — backlog of deferred user feature requests (not yet scheduled into the roadmap).
- `docs/ROADMAP.md` — numbered phases; guides architecture and sequencing.
- `docs/DECISIONS.md` — rolling log of important decisions (brief).
- `docs/COMMANDS.md` — canonical, repeatable commands for this repository. Sections are team-defined.

## Operating rules
1. **Read before planning:** Before making architectural or cross-cutting decisions, read `ROADMAP.md`, then scan `DECISIONS.md`, and then check relevant entries in `FEATURES.md` and `ISSUES.md`.
2. **Read before running commands:** Before running or recommending project commands (tests, coverage, build, lint, start services), check `docs/COMMANDS.md` first. If it is missing, ask the user before creating it by copying `.agent-layer/config/templates/docs/COMMANDS.md` into `docs/COMMANDS.md`. If it is incomplete, use auto-discovery, ask the user only when needed, then update `docs/COMMANDS.md` with the definitive approach.
3. **Initialize if missing:** If any project memory file does not exist, ask the user before creating it. If approved, copy `.agent-layer/config/templates/docs/<NAME>.md` into `docs/<NAME>.md` (preserve headings and markers).  
   - If `.agent-layer/config/templates/docs/COMMANDS.md` does not exist, ask the user before creating `docs/COMMANDS.md` with a minimal, readable structure and a single `<!-- ENTRIES START -->` insertion marker.
4. **Write down deferred work:** If you discover something worth doing and you are not doing it now:
   - Add it to `ISSUES.md` if it is a bug, maintainability refactor, technical debt, reliability, security, test coverage gap, performance concern, or other engineering risk.
   - Add it to `FEATURES.md` only if it is a new user-visible capability.
5. **Maintainability refactors are always issues:** Do not put refactors in `FEATURES.md`.
6. **FEATURES is a backlog, not a schedule:** `FEATURES.md` holds unscheduled feature requests. Periodically move selected features into `ROADMAP.md` tasks, then remove them from `FEATURES.md` to keep the backlog lean.
7. **Keep entries compact and readable:** Each issue and feature entry should be **3 to 5 lines**:
   - Line 1: `Issue YYYY-MM-DD abcdef:` or `Feature YYYY-MM-DD abcdef:` plus a short title (use a leading `-` list item).
   - Lines 2 to 5: Indent by **4 spaces** to associate the lines with the entry.
   - Line 2: Priority (Critical, High, Medium, Low) and area.
   - Line 3: Short description focused on the observed problem or requested capability.
   - Line 4: Next step (for issues) or acceptance criteria (for features).
   - Line 5: Optional dependencies or notes (only if needed).
8. **No abbreviations:** Avoid abbreviations in these files. Prefer full words and short sentences.
9. **Prevent duplicates:** Search the target file before adding a new entry. Merge or rewrite existing entries instead of adding near-duplicates.
10. **Keep files living:** When an issue is fixed, remove it from `ISSUES.md`. When a feature is implemented, remove it from `FEATURES.md`. When a feature is scheduled into the roadmap, move it into `ROADMAP.md` and remove it from `FEATURES.md` at that time.
11. **Roadmap phase behavior:**
    - The roadmap is a single list of **numbered phases**.
    - Do not renumber completed phases (phases marked with ✅).
    - You may renumber incomplete phases when updating the roadmap (for example, to insert a new phase).
    - Incomplete phases have **Goal**, **Tasks** (checkbox list), and **Exit criteria** sections.
    - When a phase is complete, add a green check emoji to the phase heading (✅) and replace the phase content with a **single bullet list** summarizing what was accomplished (no checkbox list).
    - There is no separate "current" or "upcoming" section; done vs not done is indicated by the ✅.
12. **Decision logging:** When making a significant decision (architecture, storage, data model, interface boundaries, dependency choice), add an entry to `DECISIONS.md` using `Decision YYYY-MM-DD abcdef:` with decision, reason, and tradeoffs. Keep it brief and add new entries at the bottom so the oldest decisions remain at the top.
13. **COMMANDS.md maintenance (seamless, selective):**
    - Maintain `docs/COMMANDS.md` without asking for confirmation when it improves future work.
    - Only add commands that are expected to be used repeatedly, such as:
      - setup and installation, development server, build, lint and format, typecheck, unit and integration tests, coverage, database migrations, common scripts.
    - Do not add one-off debugging commands (search/grep/find, ad-hoc scripts, temporary environment variables) unless they are a stable part of the workflow.
    - Keep `docs/COMMANDS.md` concise and structured, but **do not hard-code sections**. Sections are team-defined.
    - When adding a command:
      - place it under the best existing heading, or create a new heading if none fits,
      - include purpose, command, where to run it, and prerequisites,
      - prefer headings named **Test** and **Coverage** when applicable (for discoverability), but do not require them.
    - Deduplicate and update entries when commands change.
14. **Agent autonomy:** You may propose and apply updates to the roadmap, features, issues, decisions, and commands when it improves clarity and delivery, while keeping the documents compact.

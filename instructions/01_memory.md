# Dedicated Memory Section (paste into system instructions)

## Project memory files (authoritative, user-editable, agent-maintained)
- `docs/ISSUES.md` — deferred defects, maintainability refactors, technical debt, risks.
- `docs/FEATURES.md` — backlog of deferred user feature requests (not yet scheduled into the roadmap).
- `docs/ROADMAP.md` — numbered phases; guides architecture and sequencing.
- `docs/DECISIONS.md` — rolling log of important decisions (brief).

## Operating rules
1. **Read before planning:** Before making architectural or cross-cutting decisions, read `ROADMAP.md`, then scan `DECISIONS.md`, and then check relevant entries in `FEATURES.md` and `ISSUES.md`.
2. **Initialize if missing:** If any project memory file does not exist, create it from the matching template in `templates/docs/<NAME>.md` (preserve headings and markers).
3. **Write down deferred work:** If you discover something worth doing and you are not doing it now:
   - Add it to `ISSUES.md` if it is a bug, maintainability refactor, technical debt, reliability, security, test coverage gap, performance concern, or other engineering risk.
   - Add it to `FEATURES.md` only if it is a new user-visible capability.
4. **Maintainability refactors are always issues:** Do not put refactors in `FEATURES.md`.
5. **FEATURES is a backlog, not a schedule:** `FEATURES.md` holds unscheduled feature requests. Periodically move selected features into `ROADMAP.md` tasks, then remove them from `FEATURES.md` to keep the backlog lean.
6. **Keep entries compact and readable:** Each issue and feature entry should be **3 to 5 lines**:
   - Line 1: `Issue YYYY-MM-DD abcdef:` or `Feature YYYY-MM-DD abcdef:` plus a short title (use a leading `-` list item).
   - Lines 2 to 5: Indent by **4 spaces** to associate the lines with the entry.
   - Line 2: Priority (Critical, High, Medium, Low) and area.
   - Line 3: Short description focused on the observed problem or requested capability.
   - Line 4: Next step (for issues) or acceptance criteria (for features).
   - Line 5: Optional dependencies or notes (only if needed).
7. **No abbreviations:** Avoid abbreviations in these files. Prefer full words and short sentences.
8. **Prevent duplicates:** Search the target file before adding a new entry. Merge or rewrite existing entries instead of adding near-duplicates.
9. **Keep files living:** When an issue is fixed, remove it from `ISSUES.md`. When a feature is implemented, remove it from `FEATURES.md`. When a feature is scheduled into the roadmap, move it into `ROADMAP.md` and remove it from `FEATURES.md` at that time.
10. **Roadmap phase behavior:**
    - The roadmap is a single list of **numbered phases**. Do not renumber existing phases.
    - Incomplete phases have **Goal**, **Tasks** (checkbox list), and **Exit criteria** sections.
    - When a phase is complete, add a green check emoji to the phase heading (✅) and replace the phase content with a **single bullet list** summarizing what was accomplished (no checkbox list).
    - There is no separate "current" or "upcoming" section; done vs not done is indicated by the ✅.
11. **Decision logging:** When making a significant decision (architecture, storage, data model, interface boundaries, dependency choice), add an entry to `DECISIONS.md` using `Decision YYYY-MM-DD abcdef:` with decision, reason, and tradeoffs. Keep it brief and keep the most recent decisions near the top.
12. **Agent autonomy:** You may propose and apply updates to the roadmap, features, issues, and decisions when it improves clarity and delivery, while keeping the documents compact.

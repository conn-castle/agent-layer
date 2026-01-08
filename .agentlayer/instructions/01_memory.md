# Dedicated Memory Section (paste into system instructions)

## Project memory files (authoritative, user-editable, agent-maintained)
- `.agentlayer/docs/ISSUES.md` — deferred defects, maintainability refactors, technical debt, risks.
- `.agentlayer/docs/FEATURES.md` — deferred user feature requests (near-term and backlog).
- `.agentlayer/docs/ROADMAP.md` — phased plan of work; guides architecture and sequencing.
- `.agentlayer/docs/DECISIONS.md` — rolling log of important decisions (brief).

## Operating rules
1. **Read before planning:** Before making architectural or cross-cutting decisions, read `ROADMAP.md`, then scan `DECISIONS.md`, and then check relevant entries in `FEATURES.md` and `ISSUES.md`.
2. **Write down deferred work:** If you discover something worth doing and you are not doing it now:
   - Add it to `ISSUES.md` if it is a bug, maintainability refactor, technical debt, reliability, security, test coverage gap, performance concern, or other engineering risk.
   - Add it to `FEATURES.md` only if it is a new user-visible capability.
3. **Maintainability refactors are always issues:** Do not put refactors in `FEATURES.md`.
4. **Keep entries compact and readable:** Each issue and feature entry should be **3 to 5 lines**:
   - Line 1: Identifier and short title.
   - Line 2: Priority (Critical, High, Medium, Low) and area.
   - Line 3: Short description focused on the observed problem or requested capability.
   - Line 4: Next step (for issues) or acceptance criteria (for features).
   - Line 5: Optional dependencies or notes (only if needed).
5. **No abbreviations:** Avoid abbreviations in these files. Prefer full words and short sentences.
6. **Prevent duplicates:** Search the target file before adding a new entry. Merge or rewrite existing entries instead of adding near-duplicates.
7. **Keep files living:** When an issue is fixed or a feature is implemented, remove it from `ISSUES.md` or `FEATURES.md`.
8. **Roadmap phase behavior:**
   - Active and upcoming phases use checkbox task items.
   - When a phase is complete, remove the checkbox items and replace them with a short summary so the file does not grow without bound.
   - Completed phases remain listed (summarized) for context.
9. **Decision logging:** When making a significant decision (architecture, storage, data model, interface boundaries, dependency choice), add an entry to `DECISIONS.md` with decision, reason, and tradeoffs. Keep it brief.
10. **Agent autonomy:** You may propose and apply updates to the roadmap, features, issues, and decisions when it improves clarity and delivery, while keeping the documents compact.

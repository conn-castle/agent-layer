# Issues

Purpose: Deferred defects, maintainability refactors, technical debt, risks, and engineering concerns.

Notes for updates:
- Add an entry only when you are not fixing it now.
- Keep each entry 3 to 5 lines (the first line plus 2 to 4 indented lines).
- Lines 2 to 5 must be indented by 4 spaces so they stay associated with the entry.
- Prevent duplicates by searching and merging.
- Remove entries when fixed.

Entry format:
- Issue YYYY-MM-DD abcdef: Short title
    Priority: Critical, High, Medium, or Low. Area: <area>
    Description: <observed problem or risk>
    Next step: <smallest concrete next action>
    Notes: <optional dependencies or constraints>

## Open issues

<!-- ENTRIES START -->

- Issue 2026-01-18 e5f6g7: Slash commands not output for antigravity
    Priority: Medium. Area: antigravity support.
    Description: Slash commands are not being output when antigravity mode is enabled.
    Next step: Investigate where slash commands are generated and ensure antigravity support is included.

- Issue 2026-01-18 h8i9j0: DECISIONS.md grows too large and consumes excessive tokens
    Priority: Medium. Area: project memory.
    Description: The decisions log grows unbounded as entries accumulate, eventually consuming too many tokens when agents read it for context.
    Next step: Consider archiving old decisions, summarizing completed phases, or splitting into a compact summary plus detailed archive.

- Issue 2026-01-18 b1c2d3: Memory file path convention investigation
    Priority: Low. Area: project memory.
    Description: Should memory files use full relative paths or just filenames in 01_memory.md and slash commands?
    Next step: Audit current usage and establish a single convention for referring to memory files.

- Issue 2026-01-18 e4f5g6: Memory file template structure investigation
    Priority: Medium. Area: templates.
    Description: Should templates in .agent-layer only contain headers, and how should generated content be handled when overwriting?
    Next step: Review existing template synchronization logic and define the intended behavior for content preservation.

- Issue 2026-01-18 a7b8c9: Boost coverage slash command too conservative
    Priority: High. Area: slash commands.
    Description: The boost-coverage command only picks one file at a time and stops too early. It should iterate until coverage targets are met, even if it requires many tests.
    Next step: Refactor the boost-coverage logic to support continuous iteration and multiple file targets in a single run.

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

- Issue 2026-01-18 b2c3d4: Development launch fails when the prompt server calls ./al
    Priority: High. Area: development workflow.
    Description: `go run ./cmd/al gemini` can fail because the internal prompt server spawns `./al`, which does not exist.
    Next step: Resolve the prompt server command when running from source, or provide a fallback path.
    Notes: Target v0.3.0.

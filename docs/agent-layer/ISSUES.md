# Issues

Note: This is an agent-layer memory file. It is primarily for agent use.

## Purpose
Deferred defects, maintainability refactors, technical debt, risks, and engineering concerns. Add an entry only when you are not fixing it now.

## Format
- Insert new entries immediately below `<!-- ENTRIES START -->` (most recent first).
- Keep each entry **3–5 lines**.
- Line 1 starts with `- Issue YYYY-MM-DD <id>:` and a short title.
- Lines 2–5 are indented by **4 spaces** and use `Key: Value`.
- Keep **exactly one blank line** between entries.
- Prevent duplicates: search the file and merge/rewrite instead of adding near-duplicates.
- When fixed, remove the entry from this file.

### Entry template
```text
- Issue YYYY-MM-DD abcdef: Short title
    Priority: Critical | High | Medium | Low. Area: <area>
    Description: <observed problem or risk>
    Next step: <smallest concrete next action>
    Notes: <optional dependencies/constraints>
```

## Open issues

<!-- ENTRIES START -->

- Issue 2026-02-25 playwright-headless-parity: Evaluate headless Playwright mode without functional regressions
    Priority: Medium. Area: test automation / Playwright runner UX.
    Description: Playwright running in headed mode is noisy and disruptive during normal development. We should assess whether headless can be the default while preserving behavior.
    Next step: Run representative Playwright flows in headed vs headless mode, document any behavior differences, and only switch defaults if parity is confirmed.
    Notes: Keep an explicit opt-in path for headed runs for local debugging even if headless becomes the default.

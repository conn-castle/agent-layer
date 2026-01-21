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

- Issue 2026-01-18 h8i9j0: DECISIONS.md grows too large and consumes excessive tokens
    Priority: Medium. Area: project memory.
    Description: The decisions log grows unbounded as entries accumulate, eventually consuming too many tokens when agents read it for context.
    Next step: Consider archiving old decisions, summarizing completed phases, or splitting into a compact summary plus detailed archive.

- Issue 2026-01-18 e4f5g6: Memory file template structure investigation
    Priority: Medium. Area: templates.
    Description: Should templates in .agent-layer only contain headers, and how should generated content be handled when overwriting?
    Next step: Review existing template synchronization logic and define the intended behavior for content preservation.

- Issue 2026-01-18 l8m9n0: Limit exposed commands for GitHub MCP
    Priority: Medium. Area: mcp configuration.
    Description: The GitHub MCP server exposes many tools. We should explicitly list only the necessary commands in the configuration to reduce noise and potential security risks.
    Next step: Research useful GitHub MCP commands and configure `args` or `commands` whitelist in the default config template.

- Issue 2026-01-19 c9d2e1: Wizard UI depends on pre-release Charmbracelet packages
    Priority: Low. Area: dependencies.
    Description: `github.com/charmbracelet/huh` v0.8.0 requires pseudo versions of bubbles and colorprofile, leaving go.mod on pre-release commits.
    Next step: Re-evaluate when upstream tags stable releases or update the wizard UI dependency once stable versions are available.

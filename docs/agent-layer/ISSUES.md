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

- Issue 2026-01-20 d6ea375: Wizard rewrites inline comments during configuration edits
    Priority: Medium. Area: wizard configuration editing.
    Description: When editing TOML values, inline comments are re-emitted as leading comments, changing the original layout users may want to preserve.
    Next step: Decide whether to document this behavior or adopt a formatter that preserves inline comment placement.

- Issue 2026-01-20 e7fb486: Wizard comment parsing does not handle all TOML string forms
    Priority: Low. Area: wizard configuration editing.
    Description: Inline comment extraction treats hashes inside multiline or literal strings as comments, which can mis-handle valid TOML.
    Next step: Remove inline comment extraction or switch to parser-aware comment handling.

- Issue 2026-01-20 f8ac597: Restored Model Context Protocol servers inherit template positions
    Priority: Low. Area: wizard configuration editing.
    Description: Restored server blocks reuse template position metadata, but comment preservation reads the original configuration lines, which can mis-associate or drop comments.
    Next step: Clone restored server nodes with cleared positions or skip comment preservation for newly appended servers.

- Issue 2026-01-20 g9bd6a8: Wizard patch tests do not assert comment placement
    Priority: Low. Area: wizard tests.
    Description: The tests only check that a comment exists, not whether inline comment placement is preserved or intentionally moved.
    Next step: Add assertions for the exact expected comment placement behavior.

- Issue 2026-01-19 ceddb83: `.agent-layer/.env` overrides shell environment variables
    Priority: Medium. Area: environment handling.
    Description: When launching via `./al`, values from `.agent-layer/.env` override existing shell environment variables, and empty template keys can shadow valid tokens.
    Next step: Decide precedence and update environment merge logic or templates to avoid overriding with empty values; document the chosen behavior.

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

- Issue 2026-01-18 l8m9n0: Limit exposed commands for GitHub MCP
    Priority: Medium. Area: mcp configuration.
    Description: The GitHub MCP server exposes many tools. We should explicitly list only the necessary commands in the configuration to reduce noise and potential security risks.
    Next step: Research useful GitHub MCP commands and configure `args` or `commands` whitelist in the default config template.

- Issue 2026-01-19 i5j6k7: Wizard model catalogs require manual updates
    Priority: Low. Area: maintenance.
    Description: The list of supported models in `internal/wizard/catalog.go` is hardcoded. New model releases will require code changes to appear in the wizard.
    Next step: Consider fetching the model list dynamically or adding a "Custom..." option in the wizard.

- Issue 2026-01-19 j6k7l8: Generated .mcp.json does not adhere to Claude MCP server schema
    Priority: High. Area: MCP configuration generation.
    Description: Claude fails to parse the generated `.mcp.json` file, reporting that `mcpServers.github` and `mcpServers.tavily` do not adhere to the MCP server configuration schema.
    Next step: Compare the generated schema against Claude's expected MCP server configuration format and fix the output structure.

- Issue 2026-01-19 k7l8n0: COMMANDS.md purpose unclear in instructions
    Priority: Low. Area: documentation.
    Description: The instructions do not clearly state that COMMANDS.md is only for development workflow commands (build, test, lint), not for documenting all application commands or CLI usage.
    Next step: Update 01_memory.md to explicitly clarify that COMMANDS.md covers development commands only.

- Issue 2026-01-19 c9d2e1: Wizard UI depends on pre-release Charmbracelet packages
    Priority: Low. Area: dependencies.
    Description: `github.com/charmbracelet/huh` v0.8.0 requires pseudo versions of bubbles and colorprofile, leaving go.mod on pre-release commits.
    Next step: Re-evaluate when upstream tags stable releases or update the wizard UI dependency once stable versions are available.

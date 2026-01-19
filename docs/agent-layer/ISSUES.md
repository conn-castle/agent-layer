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

- Issue 2026-01-19 cb3ff7e: Wizard cannot restore default Model Context Protocol servers
    Priority: Medium. Area: wizard Model Context Protocol setup.
    Description: If default Model Context Protocol server entries are missing from the configuration file, the wizard cannot re-add them and only toggles existing entries.
    Next step: Prompt to restore missing default server definitions and append them before selection.

- Issue 2026-01-19 cb3ff7e: Wizard configuration edits remove inline comments
    Priority: Medium. Area: configuration editing.
    Description: The line-based patcher replaces keys without preserving inline comments or original formatting in `.agent-layer/config.toml`.
    Next step: Use a comment-preserving configuration editor or extend the patcher to retain inline comments when updating keys.

- Issue 2026-01-19 cb3ff7e: Wizard secret detection misreads commented keys
    Priority: Medium. Area: environment file handling.
    Description: Secret checks rely on substring matches, so commented lines or similar variable names can be treated as existing values and skip prompts.
    Next step: Parse `.agent-layer/.env` line by line and only treat uncommented exact keys as present.

- Issue 2026-01-19 cb3ff7e: Wizard summary omits disabled servers caused by missing secrets
    Priority: Low. Area: wizard summary.
    Description: When a secret prompt is skipped, the wizard disables the server but does not explain why in the summary, which can confuse users.
    Next step: Track disabled-by-missing-secret servers and include them in the summary output.

- Issue 2026-01-19 cb3ff7e: Wizard prompts omit required explanations
    Priority: Low. Area: wizard user experience.
    Description: The wizard does not provide approval mode meanings, preview model warnings, or full multi-select control hints that the specification requires.
    Next step: Add clear explanatory text for approval modes, preview model options, and selection controls in the wizard prompts.

- Issue 2026-01-19 cb3ff7e: Wizard writes configuration files non-atomically
    Priority: Medium. Area: configuration persistence.
    Description: Configuration and environment file updates write directly to disk without a temporary file, which risks partial writes if the process is interrupted.
    Next step: Write to a temporary file, synchronize file contents to disk, and rename to ensure atomic updates.

- Issue 2026-01-19 cb3ff7e: Wizard environment patching can duplicate keys
    Priority: Low. Area: environment file handling.
    Description: The environment patcher only matches `KEY=` lines, so entries with spacing or export statements can be duplicated instead of updated.
    Next step: Parse `.agent-layer/.env` line by line and update keys regardless of spacing or export prefixes.

- Issue 2026-01-19 cb3ff7e: Wizard command bypasses working directory helper
    Priority: Low. Area: command wiring.
    Description: The wizard command uses `os.Getwd` directly instead of the shared working directory helper used by other commands.
    Next step: Use the shared working directory helper to keep command behavior and tests consistent.

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

- Issue 2026-01-18 f1g2h3: Codex initial reasoning effort should be high
    Priority: Medium. Area: configuration templates.
    Description: The initial example configuration for Codex sets reasoning_effort to "xhigh", which can be unnecessarily expensive or slow for a default. It should be "high".
    Next step: Update `internal/templates/config.toml` and `README.md` to use "high" instead of "xhigh".

- Issue 2026-01-18 i4j5k6: Remove MY_TOKEN from default configuration template
    Priority: Low. Area: configuration templates.
    Description: The `MY_TOKEN` placeholder in the default configuration template is unnecessary and should be removed to keep the default config clean. It can remain in the README as an example of environment variable usage.
    Next step: Remove `MY_TOKEN` from `internal/templates/config.toml`.

- Issue 2026-01-18 k7l8m9: Set MCP servers to enabled by default
    Priority: Medium. Area: configuration templates.
    Description: Current MCP server examples in the default configuration are disabled by default. They should be enabled by default to provide a better out-of-the-box experience when tokens are provided.
    Next step: Update `internal/templates/config.toml` to set `enabled = true` for default MCP servers.

- Issue 2026-01-18 l8m9n0: Limit exposed commands for GitHub MCP
    Priority: Medium. Area: mcp configuration.
    Description: The GitHub MCP server exposes many tools. We should explicitly list only the necessary commands in the configuration to reduce noise and potential security risks.
    Next step: Research useful GitHub MCP commands and configure `args` or `commands` whitelist in the default config template.

- Issue 2026-01-19 f2g3h4: Wizard sets Codex reasoning effort to xhigh by default
    Priority: Medium. Area: wizard defaults.
    Description: The wizard implementation hardcodes the default reasoning effort for Codex to "xhigh", which may be too aggressive/expensive for a default. It should align with the template default (which is also currently xhigh but planned to change to high).
    Next step: Change `internal/wizard/catalog.go` default to "high" once the template decision is finalized.

- Issue 2026-01-19 i5j6k7: Wizard model catalogs require manual updates
    Priority: Low. Area: maintenance.
    Description: The list of supported models in `internal/wizard/catalog.go` is hardcoded. New model releases will require code changes to appear in the wizard.
    Next step: Consider fetching the model list dynamically or adding a "Custom..." option in the wizard.

- Issue 2026-01-19 j6k7l8: Generated .mcp.json does not adhere to Claude MCP server schema
    Priority: High. Area: MCP configuration generation.
    Description: Claude fails to parse the generated `.mcp.json` file, reporting that `mcpServers.github` and `mcpServers.tavily` do not adhere to the MCP server configuration schema.
    Next step: Compare the generated schema against Claude's expected MCP server configuration format and fix the output structure.

- Issue 2026-01-19 k7l8m9: COMMANDS.md purpose unclear in instructions
    Priority: Low. Area: documentation.
    Description: The instructions do not clearly state that COMMANDS.md is only for development workflow commands (build, test, lint), not for documenting all application commands or CLI usage.
    Next step: Update 01_memory.md to explicitly clarify that COMMANDS.md covers development commands only.

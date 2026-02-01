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

- Issue 2026-01-30 argpass: Command-line arguments not passed to underlying agents
    Priority: High. Area: CLI / agent integration
    Description: Arguments passed to `al <agent>` (e.g., `al claude --dangerously-skip-permissions`) are not forwarded to the underlying agent process, causing errors or ignored flags.
    Next step: Update agent command handlers to capture and forward trailing arguments to the underlying executable.

- Issue 2026-01-30 launch01: VS Code .app launcher only written by sync, not init
    Priority: Low. Area: install
    Description: The `open-vscode.app` launcher is written by `al sync` but not by `al init`. Users must run `al sync` (or `al vscode`) after init before the launcher exists.
    Next step: Move launcher creation to init so it's available immediately after setup.

- Issue 2026-01-30 codex01: Document per-repo Codex authentication requirement
    Priority: Low. Area: documentation
    Description: README should document that Codex requires per-repo authentication due to CODEX_HOME isolation. This is expected but may surprise users.
    Next step: Add note to README section on "VS Code + Codex extension (CODEX_HOME)" explaining that users must re-authenticate when opening a new repo with a different CODEX_HOME.
    Notes: This is by design to keep agent credentials isolated per repo; users should be aware of this upfront.

- Issue 2026-01-24 a1b2c3: VS Code slow first launch in agent-layer folder
    Priority: Low. Area: developer experience.
    Description: Launching VS Code in the agent-layer folder takes a very long time on first use, likely due to extension initialization, indexing, or MCP server startup.
    Next step: Profile VS Code startup to identify the bottleneck (extensions, language servers, MCP servers, or workspace indexing).

- Issue 2026-01-26 j4k5l6: Managed file diff visibility for overwrite decisions
    Priority: Medium. Area: install / UX.
    Description: Users cannot easily determine whether differences in managed files are due to intentional local customizations they want to keep, or due to agent-layer version updates that should be accepted. This makes overwrite decisions difficult and error-prone.
    Next step: Implement a diff or comparison view (e.g., `al diff` or during `al init --overwrite`) that shows what changed between local files and the new template versions, with annotations or categories for change types when possible.
    Notes: Related to Issue g2h3i4 but distinct—that issue is about prompt flow, this is about visibility into what's actually different.

- Issue 2026-01-30 wiz001: Codex model/reasoning defaults should be empty in wizard
    Priority: Low. Area: wizard
    Description: Codex defaults for model and reasoning are pre-filled unlike other agents during wizard setup.
    Next step: Update default toml template to use empty strings for codex model/reasoning fields.

- Issue 2026-01-30 wiz002: Final save config question in wizard lacks visible text
    Priority: Medium. Area: wizard
    Description: The final save config prompt in the wizard is confusing because no text is displayed on screen.
    Next step: Add descriptive text to the save config confirmation prompt.

- Issue 2026-01-30 doc001: Add MCP server troubleshooting note to README FAQ
    Priority: Low. Area: documentation
    Description: Users need guidance when MCP servers fail in VSCode due to node installation location.
    Next step: Add FAQ entry explaining that node should be installed via Homebrew, not to a user directory.

- Issue 2026-01-30 qd4k2m: Codex VSCode fetch configuration clarification
    Priority: Medium. Area: Documentation / Tooling
    Description: Fetch via the Codex VSCode extension currently fails unless `uvx` is referenced by an absolute path and both the PATH environment variable and the fetch command are explicitly supplied inside the Codex-specific `config.toml`.
    Next Step: Experiment with the Codex VSCode fetch flow, confirm the absolute `uvx` path requirement, document the explicit PATH and command entries in `config.toml`, and update the extension instructions once the workflow is verified.

- Issue 2026-01-30 4f5a2b1: Improve version outdated message text
    Priority: Medium. Area: CLI / DX
    Description: The message shown to users when the agent-layer version is outdated is not clear enough. It should provide specific instructions on how to update.
    Next step: Update the outdated version message to include instructions to update `al` with Homebrew and then update the repository version (e.g., `al init --version latest`).

# Features

Purpose: Backlog of deferred user-visible feature requests (not yet scheduled into the roadmap).

Notes for updates:
- Add an entry only when it is a new user-visible capability.
- Keep each entry 3 to 5 lines (the first line plus 2 to 4 indented lines).
- Lines 2 to 5 must be indented by 4 spaces so they stay associated with the entry.
- Prevent duplicates by searching and merging.
- When a feature is scheduled into a roadmap phase, move it into `ROADMAP.md` and remove it from this file.
- When a feature is implemented, ensure it is removed from this file.

Entry format:
- Feature YYYY-MM-DD abcdef: Short title
    Priority: Critical, High, Medium, or Low. Area: <area>
    Capability: <what the user should be able to do>
    Acceptance criteria: <clear condition to consider it done>
    Notes: <optional dependencies or constraints>

## Backlog (not scheduled)

<!-- ENTRIES START -->

- Feature 2026-01-18 e5f6g7: Per-file overwrite prompts during install
    Priority: Low. Area: installation experience.
    Capability: When using `al install --overwrite`, the user should be prompted for each file individually, allowing selective overwrites. Adding `--force` skips the prompts and overwrites all files. Also make sure to update notification to tell the user about overwriting with and without force.
    Acceptance criteria: `--overwrite` prompts per file; `--overwrite --force` overwrites all without prompting.

- Feature 2026-01-18 f6g7h8: Centralized user-facing strings file
    Priority: Low. Area: code organization.
    Capability: All user-facing language (messages, prompts, errors, help text) should live in a single file so developers know where to look and can easily update user experience strings.
    Acceptance criteria: A single file contains all user-facing strings; code references this file instead of inline strings.

- Feature 2026-01-18 g7h8i9: Optional gitignore entry for docs/agent-layer
    Priority: Low. Area: installation and configuration.
    Capability: The default gitignore block should include a commented-out line for `docs/agent-layer/` with a note explaining users can uncomment it to exclude the agent-layer memory docs from version control.
    Acceptance criteria: `gitignore.block` includes `# docs/agent-layer/` with an explanatory comment.

- Feature 2026-01-18 m0n1o2: Warning for high token count in system instructions
    Priority: Medium. Area: user experience.
    Capability: The system should estimate the token count of generated system instructions and warn the user if it exceeds a threshold that might impact model performance or costs.
    Acceptance criteria: `al sync` displays a warning if estimated tokens > threshold (e.g. 50k).

- Feature 2026-01-18 p3q4r5: Warning for excessive MCP servers
    Priority: Medium. Area: user experience.
    Capability: The system should warn the user if a large number of MCP servers are enabled, as this can confuse agents or degrade performance.
    Acceptance criteria: `al sync` displays a warning if enabled MCP servers > threshold (e.g. 5).

- Feature 2026-01-19 q4r5s6: Persist conversation history in local model folders
    Priority: Medium. Area: model configuration.
    Capability: Gemini and all other supported models should persist their conversation history inside this repository in their respective local folders (e.g. `.agent-layer/gemini/`, `.agent-layer/openai/`).
    Acceptance criteria: Each model's configuration directs conversation history to a model-specific folder within the repository; history files are created and maintained locally.

- Feature 2026-01-19 r5s6t7: Audit instructions and slash commands for duplicate content
    Priority: Low. Area: code organization and maintainability.
    Capability: Review all instruction templates and slash command definitions to identify and consolidate duplicate instructions (e.g., memory formatting rules that appear multiple times across different files).
    Acceptance criteria: Each instruction appears in exactly one canonical location; other locations reference or include the canonical source rather than duplicating the text.

- Feature 2026-01-19 s6t7u8: Finish task and cleanup commands ensure commit-ready state
    Priority: Medium. Area: slash commands.
    Capability: Running finish-task or cleanup-code commands should ensure the codebase is in a fully passing state (tests pass, linting passes, precommit hooks pass) so users can immediately stage and commit without worrying about failures.
    Acceptance criteria: After running finish-task or cleanup-code, all tests pass, lint checks pass, and precommit hooks would succeed on the changed files.

# Features

Purpose: Backlog of deferred user-visible feature requests (not yet scheduled into the roadmap).

Notes for updates:
- Add an entry only when it is a new user-visible capability.
- Keep each entry 3 to 5 lines (the first line plus 2 to 4 indented lines).
- Lines 2 to 5 must be indented by 4 spaces so they stay associated with the entry.
- Prevent duplicates by searching and merging.
- When a feature is scheduled into a roadmap phase, move it into `docs/agent-layer/ROADMAP.md` and remove it from this file.
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

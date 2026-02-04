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

- Issue 2026-02-04 gitign02: Ignore .agent-layer/templates in .gitignore
    Priority: Low. Area: install / templates
    GitHub: https://github.com/conn-castle/agent-layer/issues/36
    Description: The `templates` folder inside `.agent-layer` is duplicative with documentation and other sources. When users commit their agent-layer configuration, this folder adds unnecessary noise.
    Next step: Add `.agent-layer/templates/` to the `.gitignore` file (or create one inside `.agent-layer`) to prevent it from being tracked.

- Issue 2026-02-04 gitign: Update .gitignore during sync
    Priority: Medium. Area: CLI / sync
    GitHub: https://github.com/conn-castle/agent-layer/issues/35
    Description: Currently, .gitignore is only updated during `init`. It should also be updated during `sync` to ensure new agent-layer files are correctly ignored as they are introduced or updated.
    Next step: Move the .gitignore update logic into the sync workflow.

- Issue 2026-02-04 ver002: Opt-in version update warnings for sync
    Priority: Medium. Area: CLI / sync
    GitHub: https://github.com/conn-castle/agent-layer/issues/37
    Description: Outdated version warnings should be opt-in for sync runs. By default (missing config), only warn during `init`, `doctor`, or `wizard`. If opted-in via config (should be enabled in the default template), warn on every `sync` (agent start) as well.
    Next step: Update the configuration schema to include a version warning toggle and modify the version check logic to respect this flag during sync operations.

- Issue 2026-01-31 wiz003: Wizard scrambles config.toml order
    Priority: Critical. Area: wizard
    GitHub: https://github.com/conn-castle/agent-layer/issues/27
    Description: The wizard screws up the order of the config.toml during updates, making the file almost unusable and difficult to maintain manually.
    Next step: Fix the configuration writing logic in the wizard to preserve key order or follow a canonical schema-based order.

- Issue 2026-01-30 argpass: Command-line arguments not passed to underlying agents
    Priority: High. Area: CLI / agent integration
    GitHub: https://github.com/conn-castle/agent-layer/issues/28
    Description: Arguments passed to `al <agent>` (e.g., `al claude --dangerously-skip-permissions`) are not forwarded to the underlying agent process, causing errors or ignored flags.
    Next step: Update agent command handlers to capture and forward trailing arguments to the underlying executable.

- Issue 2026-01-30 launch01: VS Code .app launcher only written by sync, not init
    Priority: Low. Area: install
    GitHub: https://github.com/conn-castle/agent-layer/issues/29
    Description: The `open-vscode.app` launcher is written by `al sync` but not by `al init`. Users must run `al sync` (or `al vscode`) after init before the launcher exists.
    Next step: Move launcher creation to init so it's available immediately after setup.

- Issue 2026-01-30 wiz002: Final save config question in wizard lacks visible text
    Priority: Medium. Area: wizard
    GitHub: https://github.com/conn-castle/agent-layer/issues/31
    Description: The final save config prompt in the wizard is confusing because no text is displayed on screen.
    Next step: Add descriptive text to the save config confirmation prompt.

- Issue 2026-01-26 j4k5l6: Managed file diff visibility for overwrite decisions
    Priority: Medium. Area: install / UX.
    GitHub: https://github.com/conn-castle/agent-layer/issues/30
    Description: Users cannot easily determine whether differences in managed files are due to intentional local customizations they want to keep, or due to agent-layer version updates that should be accepted. This makes overwrite decisions difficult and error-prone.
    Next step: Implement a diff or comparison view (e.g., `al diff` or during `al init --overwrite`) that shows what changed between local files and the new template versions, with annotations or categories for change types when possible.

- Issue 2026-01-24 a1b2c3: VS Code slow first launch in agent-layer folder
    Priority: Low. Area: developer experience.
    GitHub: https://github.com/conn-castle/agent-layer/issues/39
    Description: Launching VS Code in the agent-layer folder takes a very long time on first use, likely due to extension initialization, indexing, or MCP server startup.
    Next step: Profile VS Code startup to identify the bottleneck (extensions, language servers, MCP servers, or workspace indexing).

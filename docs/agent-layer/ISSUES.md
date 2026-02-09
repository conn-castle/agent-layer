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

- Issue 2026-02-09 web-seo: Update website metadata, SEO, and favicon
    Priority: Medium. Area: website / marketing.
    Description: The website needs professional metadata, SEO optimization, and a proper favicon to improve visibility and professional appearance.
    Next step: Audit `site/` for missing meta tags and favicon, then implement them.

- Issue 2026-02-09 web-init: Clarify "Initialize the repo" language on landing page
    Priority: Medium. Area: website / UX.
    Description: The landing page uses "Initialize the repo" which is ambiguous. It should be "Initializing agent-layer for your project" or similar to avoid confusion with `git init`.
    Next step: Update landing page copy in `site/` to use more precise language about agent-layer initialization.

- Issue 2026-02-08 mcp-glob: Document global MCP server fallback on website
    Priority: Medium. Area: documentation / website / UX.
    Description: Users can avoid `CODEX_HOME` and VS Code setup friction by configuring MCP servers globally in their home directory. Codex will still use local repo instructions and skills.
    Next step: Update website documentation to explain this "sucky but functional" fallback for users who don't want to manage local MCP settings.

- Issue 2026-02-08 upd-msg: Ambiguous update available warning message
    Priority: Medium. Area: CLI / update / UX.
    Description: The warning message "Warning: update available: %s (current %s)" does not specify that the update is for `agent-layer`, which can be confusing to users.
    Next step: Update `internal/messages/cli.go` and `internal/messages/doctor.go` to include "agent-layer" in the message (e.g., "Warning: agent-layer update available: ...").

- Issue 2026-02-08 tmpl-mk: Slash-command templates reference non-existent Makefile targets
    Priority: Low. Area: templates / developer experience.
    Description: `finish-task.md`, `fix-issues.md`, and `cleanup-code.md` templates reference `make test-fast` and `make dead-code` which do not exist in the Makefile. Templates note these as optional/conditional, but they may confuse agents in repos that do not provide them.
    Next step: Either add `test-fast` and `dead-code` Makefile targets, or clarify the template language to make the conditional nature more explicit.

- Issue 2026-01-24 a1b2c3: VS Code slow first launch in agent-layer folder
    Priority: Low. Area: developer experience.
    GitHub: https://github.com/conn-castle/agent-layer/issues/39
    Description: Launching VS Code in the agent-layer folder takes a very long time on first use, likely due to extension initialization, indexing, or MCP server startup.
    Next step: Profile VS Code startup to identify the bottleneck (extensions, language servers, MCP servers, or workspace indexing).

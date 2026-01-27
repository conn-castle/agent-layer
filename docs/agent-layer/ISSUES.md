# Issues

Note: This is an agent-layer memory file. It is primarily for agent use.

## Open issues

<!-- ENTRIES START -->

- Issue 2026-01-24 a1b2c3: VS Code slow first launch in agent-layer folder
    Priority: Low. Area: developer experience.
    Description: Launching VS Code in the agent-layer folder takes a very long time on first use, likely due to extension initialization, indexing, or MCP server startup.
    Next step: Profile VS Code startup to identify the bottleneck (extensions, language servers, MCP servers, or workspace indexing).

- Issue 2026-01-26 g2h3i4: Init overwrite should separate managed files from memory files
    Priority: Medium. Area: install / UX.
    Description: When `al init --overwrite` prompts to overwrite files, it groups managed template files (.agent-layer/) and memory files (docs/agent-layer/) together. Users typically want to overwrite managed files to get template updates but preserve memory files (ISSUES.md, BACKLOG.md, ROADMAP.md, DECISIONS.md, COMMANDS.md) which contain project-specific data.
    Next step: Modify the overwrite prompt flow to ask separately: "Overwrite all managed files?" then "Overwrite memory files?" so users can easily say yes/no to each category.
    Notes: Memory files are in docs/agent-layer/; managed template files are in .agent-layer/.

- Issue 2026-01-26 j4k5l6: Managed file diff visibility for overwrite decisions
    Priority: Medium. Area: install / UX.
    Description: Users cannot easily determine whether differences in managed files are due to intentional local customizations they want to keep, or due to agent-layer version updates that should be accepted. This makes overwrite decisions difficult and error-prone.
    Next step: Implement a diff or comparison view (e.g., `al diff` or during `al init --overwrite`) that shows what changed between local files and the new template versions, with annotations or categories for change types when possible.
    Notes: Related to Issue g2h3i4 but distinct—that issue is about prompt flow, this is about visibility into what's actually different.

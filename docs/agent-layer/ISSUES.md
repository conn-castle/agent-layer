# Issues

Note: This is an agent-layer memory file. It is primarily for agent use.

## Open issues

<!-- ENTRIES START -->

- Issue 2026-01-24 a1b2c3: VS Code slow first launch in agent-layer folder
    Priority: Low. Area: developer experience.
    Description: Launching VS Code in the agent-layer folder takes a very long time on first use, likely due to extension initialization, indexing, or MCP server startup.
    Next step: Profile VS Code startup to identify the bottleneck (extensions, language servers, MCP servers, or workspace indexing).

- Issue 2026-01-25 c4d5e6: Codex ignores "Unexpected repository changes" instruction
    Priority: Medium. Area: agent instructions.
    Description: Despite the explicit rule to ignore non-conflicting working tree changes, Codex still pauses and asks the user for guidance when it detects unexpected changes.
    Next step: Refine or strengthen the instruction wording specifically for Codex, or investigate if the client-side system prompt is overriding this repo-local rule.
    Notes: Rule is defined in .agent-layer/instructions/00_base.md or similar.

- Issue 2026-01-25 d7e8f9: Inconsistent decision consolidation in documentation workflows
    Priority: Medium. Area: workflows / documentation.
    Description: Agents do not consistently apply consolidation/deduplication logic for DECISIONS.md during post-task or audit workflows, leading to potential near-duplicates.
    Next step: Update slash-command templates (finish-task, cleanup-code, etc.) to explicitly reinforce the consolidation requirement during the Memory Curator phase.
    Notes: Affects any workflow that writes to DECISIONS.md.

- Issue 2026-01-25 f1e2d3: Agent documentation and search fallback strategy
    Priority: Medium. Area: agent behavior
    Description: When the agent lacks knowledge about a task or question, it should first attempt to search for documentation via available MCP servers before falling back to online search. The logic must be agnostic of specific servers and resilient to the absence of MCP tools.
    Next step: Incorporate prioritized documentation search and online fallback instructions into the core agent logic or relevant instruction files.

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

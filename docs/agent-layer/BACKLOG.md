# Backlog

Note: This is an agent-layer memory file. It is primarily for agent use.

## Purpose
Unscheduled user-visible features and tasks (distinct from issues; not refactors). Maintainability refactors belong in ISSUES.md.

## Format
- Insert new entries immediately below `<!-- ENTRIES START -->` (most recent first).
- Keep each entry **3–5 lines**.
- Line 1 starts with `- Backlog YYYY-MM-DD <id>:` and a short title.
- Lines 2–5 are indented by **4 spaces** and use `Key: Value`.
- Keep **exactly one blank line** between entries.
- Prevent duplicates: search the file and merge/rewrite instead of adding near-duplicates.
- When scheduled into ROADMAP.md, move the work into ROADMAP.md and remove it from this file.
- When implemented, remove the entry from this file.

### Entry template
```text
- Backlog YYYY-MM-DD abcdef: Short title
    Priority: Critical | High | Medium | Low. Area: <area>
    Description: <what the user should be able to do>
    Acceptance criteria: <clear condition to consider it done>
    Notes: <optional dependencies/constraints>
```

## Features and tasks (not scheduled)

<!-- ENTRIES START -->

- Backlog 2026-02-10 test-agents: Multi-agent test strategy and execution workflows
    Priority: High. Area: workflows/testing
    Description: Multi-agent workflow where one agent "dreams up" and documents test strategies/cases (unit/E2E/integration), while a second agent implements tests, runs them, fixes failures, and opens PRs.
    Acceptance criteria: E2E agents use tools like Playwright to actively "break things," turning failures into new tests; results are delivered as complete PRs with code and tests.
    Notes: Must distinguish between test types; requires full product access for E2E agents to explore and find edge cases.

- Backlog 2026-02-10 analytics: Add tracking and analytics to the website
    Priority: Medium. Area: website
    Description: Integrate tracking and analytics into the project website to monitor visitor counts and usage patterns.
    Acceptance criteria: Website includes an analytics provider (e.g., Google Analytics, Plausible) and visitor data is accessible.
    Notes: Ensure privacy compliance (GDPR/CCPA) and consider privacy-respecting alternatives.

- Backlog 2026-02-03 f1a2b3c: Transform roadmap into public-facing documentation
    Priority: Medium. Area: documentation
    Description: Convert the internal `ROADMAP.md` into actual documentation that clearly communicates the project's direction and upcoming features to users.
    Acceptance criteria: `ROADMAP.md` is formatted and positioned as a user-facing document, providing clarity on what is coming and what is speculative.
    Notes: Ensure it remains easy for agents to update while being readable for humans.

- Backlog 2026-01-30 e5f4d3c: Enable full-auto mode for Claude and Codex
    Priority: Low. Area: agent permissions
    Description: Provide a way to give Claude and Codex full access to the CLI to avoid repetitive permission prompts, specifically for Claude's custom Python execution.
    Acceptance criteria: Claude and Codex can be configured to run in "full-auto" mode, bypassing manual approval for CLI commands and Python scripts.
    Notes: Merges and elevates Backlog 2026-01-25 d0e1f2a; requires strong security warnings.

- Backlog 2026-01-28 7e9f3a1: Add support for Claude extension in VSCode
    Priority: Medium. Area: client integration
    Description: Add support for configuring and launching the Claude extension within VSCode, similar to the existing VSCode agent support.
    Acceptance criteria: Users can enable and configure the Claude extension through `al vscode` or a dedicated command.
    Notes: Currently, `al vscode` handles VSCode configuration; this would extend it to specifically support the Claude extension.

- Backlog 2026-01-25 a1b2c3d: Add interaction monitoring for prompt self-improvement
    Priority: Low. Area: agent intelligence
    Description: Add interaction monitoring to agent system instructions to self-improve all prompts, rules, and workflows based on usage patterns.
    Acceptance criteria: Monitoring captures interaction patterns and produces actionable suggestions for prompt improvements.
    Notes: Requires careful design to avoid privacy concerns and ensure suggestions are high-quality.

- Backlog 2026-01-25 b2c3d4e: Enable safe auto-approval for workflow slash commands
    Priority: Medium. Area: workflow automation
    Description: Enable safe auto-approval for slash-command workflows invoked through the workflow system.
    Acceptance criteria: Workflows can run with minimal human intervention where the operation is deemed safe.
    Notes: Requires clear safety criteria and audit trail for auto-approved actions.

- Backlog 2026-01-25 c3d4e5f: Auto-merge client-side edits back to agent-layer sources
    Priority: Medium. Area: config synchronization
    Description: Auto-merge client-side approvals or MCP server edits back into agent-layer sources.
    Acceptance criteria: Changes made in client configs are detected and merged back to source files.
    Notes: Needs conflict resolution strategy and user confirmation for ambiguous merges.

- Backlog 2026-01-25 d4e5f6a: Add task queueing system for chained operations
    Priority: Low. Area: workflow automation
    Description: Add a queueing system to chain tasks without interrupting the current task.
    Acceptance criteria: Users can queue multiple tasks that execute sequentially without manual intervention between them.
    Notes: Consider how to handle failures mid-queue and queue persistence across sessions.

- Backlog 2026-01-25 e5f6a7b: Add slash-command ordering guide
    Priority: Low. Area: documentation
    Description: Add a simple flowchart or rules-based guide for slash-command ordering.
    Acceptance criteria: Documentation clearly explains recommended slash-command sequences for common workflows.
    Notes: Should cover common scenarios like feature development, bug fixing, and code review.

- Backlog 2026-01-25 f6a7b8c: Build multi-agent chat tool
    Priority: Low. Area: agent collaboration
    Description: Build a Ralph Wiggum-like tool where different agents can chat with each other.
    Acceptance criteria: Agents can exchange messages and collaborate on tasks through a shared interface.
    Notes: Experimental feature; needs clear use cases and safety boundaries.

- Backlog 2026-01-25 a7b8c9d: Build unified documentation repository with MCP access
    Priority: Low. Area: knowledge management
    Description: Build a unified documentation repository with Model Context Protocol tool access for shared notes.
    Acceptance criteria: A central repository exists that agents can read/write through MCP tools.
    Notes: Consider access control, versioning, and conflict resolution.

- Backlog 2026-01-25 b8c9d0e: Add indexed chat history for searchable context
    Priority: Low. Area: knowledge management
    Description: Add indexed chat history in the unified documentation repository for searchable context.
    Acceptance criteria: Past conversations are indexed and searchable to provide relevant context to agents.
    Notes: Depends on unified documentation repository being implemented first.

- Backlog 2026-01-25 c9d0e1f: Persist conversation history in model-specific folders
    Priority: Low. Area: session management
    Description: Persist conversation history in model-specific local folders (e.g., `.agent-layer/gemini/`, `.agent-layer/openai/`).
    Acceptance criteria: Conversation history is saved locally per model and can be restored across sessions.
    Notes: Consider storage format, retention policy, and privacy implications.

- Backlog 2026-01-25 1b2c3d4: Investigate support for Claude Code Desktop (GUI)
    Priority: Low. Area: client integration
    Description: Investigate adding support for launching and configuring Claude Code Desktop (GUI version) if/when available.
    Acceptance criteria: Feasibility study completed; if viable, `al claude-desktop` command or similar is spec'd out.
    Notes: Currently `al claude` supports the CLI; need to check if a GUI variant exists or is planned and how it integrates.

- Backlog 2026-01-25 1a2b3c4: Evaluate unified shell/command MCP for centralized allowlisting
    Priority: Low. Area: MCP / security
    Description: Evaluate using a generic shell/command MCP server to enforce a unified allowlist for shell commands across all agents, rather than relying on agent-specific implementations.
    Acceptance criteria: Feasibility study and prototype of a unified shell MCP with granular allowlisting.
    Notes: Deep backlog item. Consider only after high-priority improvements are complete. Goal is centralized control.

- Backlog 2026-01-27 2b3c4d5: Support Codex-as-MCP for multi-agent use
    Priority: Medium. Area: agent collaboration
    Description: Support running Codex as an MCP server to allow multi-agent collaboration. Investigate similar capabilities for Claude and Gemini.
    Acceptance criteria: Codex can be exposed as an MCP server. Investigation into Claude/Gemini MCP agent support is complete.
    Notes: Enables agents to call other agents as tools.

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

- Backlog 2026-02-19 upg-snapshot-list: Add command to list upgrade snapshots
    Priority: Medium. Area: upgrade / CLI
    Description: Provide a CLI way to list available upgrade snapshots so users can discover rollback targets without manually inspecting directories.
    Acceptance criteria: A user can run `al upgrade rollback --list` (or equivalent) and see snapshot IDs and key metadata needed to choose a rollback target.
    Notes: Output should be human-readable and stable for scripting where practical.

- Backlog 2026-02-19 wiz-default-model-names: Update wizard default Claude/Codex model names
    Priority: Medium. Area: wizard / configuration
    Description: Update `al wizard` so the default Claude and Codex model names match the current intended defaults.
    Acceptance criteria: Running `al wizard` with default selections writes the updated Claude and Codex model names to config and related tests/docs reflect the new defaults.
    Notes: Keep model defaults sourced from a single canonical location to avoid future drift.

- Backlog 2026-02-19 readable-upgrade-diff: Improve upgrade diff readability with color coding
    Priority: Medium. Area: upgrades / UX
    Description: Make upgrade-process diffs more human-readable and friendly so users can quickly understand what changed.
    Acceptance criteria: Upgrade diffs render with clear color coding (at minimum) for additions/removals and remain legible in standard terminal workflows.
    Notes: Prefer a format that also improves scanability beyond color when possible (grouping, labels, or concise summaries).

- Backlog 2026-02-16 skill-install: Install community skills from external sources
    Priority: Low. Area: skills / ecosystem
    Description: Allow users to install agentskills.io-compliant skills from GitHub repos or a registry (e.g., `al skill add <repo>` or `al skill add <name>`).
    Acceptance criteria: Users can install a skill from an external source into `.agent-layer/skills/` and it is picked up by `al sync`.
    Notes: Depends on Phase 15 (skills standard alignment). Consider validation, versioning, and update mechanisms.

- Backlog 2026-02-10 test-agents: Multi-agent test strategy and execution workflows
    Priority: High. Area: workflows/testing
    Description: Multi-agent workflow where one agent "dreams up" and documents test strategies/cases (unit/E2E/integration), while a second agent implements tests, runs them, fixes failures, and opens PRs.
    Acceptance criteria: E2E agents use tools like Playwright to actively "break things," turning failures into new tests; results are delivered as complete PRs with code and tests.
    Notes: Must distinguish between test types; requires full product access for E2E agents to explore and find edge cases.

- Backlog 2026-01-25 a1b2c3d: Add interaction monitoring for prompt self-improvement
    Priority: Low. Area: agent intelligence
    Description: Add interaction monitoring to agent system instructions to self-improve all prompts, rules, and workflows based on usage patterns.
    Acceptance criteria: Monitoring captures interaction patterns and produces actionable suggestions for prompt improvements.
    Notes: Requires careful design to avoid privacy concerns and ensure suggestions are high-quality.

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

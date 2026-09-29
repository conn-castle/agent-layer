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
- Describe the problem without choosing a solution or listing options.
- Use `Next step` only when the action is useful regardless of the eventual solution. Otherwise, use `Open question: <decision needed>`.

### Entry template
```text
- Issue YYYY-MM-DD short-slug: Short title
    Priority: Critical | High | Medium | Low. Area: <area>
    Description: <observed problem or risk>
    Next step: <smallest concrete next action>
    Notes: <optional dependencies/constraints>
```

## Open issues

<!-- ENTRIES START -->

- Issue 2026-09-28 vscode-root-mcp-client-filter: VS Code loads Claude-only servers from root `.mcp.json` when Muse is disabled
    Priority: Low. Area: VS Code integration
    Description: VS Code 1.138 discovers root `.mcp.json` and ignores its `enabled` field. With Claude or Claude VS Code and VS Code enabled, that file holds Claude's projection, but `validateMuseVSCodeSharedMCP` rejects servers whose `clients` exclude `vscode` only when Muse is also enabled, so those servers still load in VS Code.
    Open question: Should that validation apply whenever root `.mcp.json` is generated, rejecting configurations that currently sync successfully?
    Notes: Grok and `al copilot` masks were generalized the same way in the `copilot-root-mcp-client-filter` fix; current behavior is documented in docs/MCP_HEADERS_SUPPORT.md.

- Issue 2026-09-23 go-test-truncated-package-undetected: Test runs pass when a test binary is replaced or exits at the syscall level
    Priority: Low. Area: Test suite / Makefile
    Description: When a test process is replaced (`syscall.Exec`) or exits through `syscall.Exit` before its package finishes, `go test` still reports `ok` and test2json emits no package-level result; `os.Exit(0)` is caught by `-test.paniconexit0`, but these low-level paths are not. `make test` and `make coverage` (which `make ci` uses) rely on that exit status alone, so the `cmd/al` suite ran 1 of 292 tests from 2026-07-02 to 2026-09-23 without any failure signal.
    Open question: Is a post-check in `make test` and `make coverage` that fails when a started package lacks a package-level `pass`/`fail`/`skip` event in `go-test.jsonl` worth adding?
    Notes: The `cmd/al` exec-handoff tests now re-execute the test binary; a full `make test` emits a package-level result for all 49 packages (47 `pass`, 2 `skip` with no test files). `grok` and `copilot_cli` also launch through `clients.ExecHandoff`.

- Issue 2026-07-28 dispatch-mcp-start-transport-window: An MCP dispatch_start disconnect can orphan a handle
    Priority: Medium. Area: Agent Dispatch MCP interface
    Description: `dispatch_start` is an RPC acknowledgement rather than a direct write to the caller's terminal. If the transport disconnects after the backend starts but before the client observes the response, the dispatch keeps running durably while the caller never learns its handle. This slice deliberately added no idempotency state and no listing API.
    Open question: Has this been observed in practice, justifying a caller-supplied idempotency key or narrow handle-recovery read?
    Notes: Evidence remains under `.agent-layer/tmp/runs/`; documented in docs/AGENT-DISPATCH.md and the dispatch-mcp-interface decision.

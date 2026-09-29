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

- Issue 2026-09-29 make-381-ignores-shellflags: macOS system Make hides failing recipes
    Priority: High. Area: Makefile / local verification
    Description: macOS `/usr/bin/make` is GNU Make 3.81, which predates `.SHELLFLAGS` (3.82), so recipes run without `-euo pipefail`. `gotestsum ... | tee ... || status=$?` then captures `tee`'s status, and `make test`, `make coverage`, `make ci`, and the `make test` pre-commit hook exit 0 with failing tests; hosted Linux CI is unaffected.
    Open question: Should the Makefile require GNU Make 3.82+ (macOS contributors install and run `gmake`), or should recipes stop depending on `.SHELLFLAGS`?
    Notes: Reproduced 2026-09-29 with a two-line Makefile (`false | true` succeeds; `$-` is `hBc`). A wrapper passed as `SHELL=` running `bash -euo pipefail "$@"` restores the intended behavior.

- Issue 2026-09-29 cmd-al-macos-tempdir-symlink: Three `cmd/al` tests fail on macOS
    Priority: Medium. Area: Test suite
    Description: `TestRootedMCPWorkingDir`, `TestWizardCommandInteractiveRunsWizard`, and `TestWizardCommandProfileModeNonInteractive` compare an unresolved `t.TempDir()` path with a symlink-resolved root (`/var/...` vs `/private/var/...`), so they fail on macOS regardless of `TMPDIR`.
    Next step: Confirm the symlink-resolved root is intended, then align the assertions with it.
    Notes: Hidden locally by `make-381-ignores-shellflags`; Linux CI has no symlinked temp dir.

- Issue 2026-09-28 vscode-root-mcp-client-filter: VS Code loads Claude-only servers from root `.mcp.json` when Muse is disabled
    Priority: Low. Area: VS Code integration
    Description: VS Code 1.138 discovers root `.mcp.json` and ignores its `enabled` field. With Claude or Claude VS Code and VS Code enabled, that file holds Claude's projection, but `validateMuseVSCodeSharedMCP` rejects servers whose `clients` exclude `vscode` only when Muse is also enabled, so those servers still load in VS Code.
    Open question: Should that validation apply whenever root `.mcp.json` is generated, rejecting configurations that currently sync successfully?
    Notes: Grok and `al copilot` masks were generalized the same way in the `copilot-root-mcp-client-filter` fix; current behavior is documented in docs/MCP_HEADERS_SUPPORT.md.

- Issue 2026-07-28 dispatch-mcp-start-transport-window: An MCP dispatch_start disconnect can orphan a handle
    Priority: Medium. Area: Agent Dispatch MCP interface
    Description: `dispatch_start` is an RPC acknowledgement rather than a direct write to the caller's terminal. If the transport disconnects after the backend starts but before the client observes the response, the dispatch keeps running durably while the caller never learns its handle. This slice deliberately added no idempotency state and no listing API.
    Open question: Has this been observed in practice, justifying a caller-supplied idempotency key or narrow handle-recovery read?
    Notes: Evidence remains under `.agent-layer/tmp/runs/`; documented in docs/AGENT-DISPATCH.md and the dispatch-mcp-interface decision.

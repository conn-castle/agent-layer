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

- Issue 2026-09-23 cmd-al-tests-exec-truncated: Most cmd/al tests silently never run
    Priority: High. Area: Test suite / cmd/al
    Description: `internal/clients.ExecHandoff` uses `syscall.Exec`, so `TestClientArgsPassThrough` (the first `cmd/al` test) replaces the test binary with its stub `claude`, which exits 0. `go test` reports `ok` after one of 292 test functions; test2json attributes the process-exit `pass` (package elapsed time) to that unfinished test instead of emitting a package-level `pass`; `make test`, `make coverage`, and CI are green without exercising `cmd/al`. Skipping it and `TestClientArgsPassThroughWithSeparator` lets the package complete and exposes a masked failure in `TestOrganizeScratchLongHelpStatesSafetyBoundaries`. Present since b32b1e94 (2026-07-02).
    Next step: Make the exec-handoff tests unable to replace the test process, then fix whatever the unmasked `cmd/al` run reports.
    Notes: Reproduce with `go test -count=1 -json ./cmd/al` (one `run` event; the only `pass` carries `"Test":"TestClientArgsPassThrough"`). Coverage totals include the truncated package.

- Issue 2026-09-21 release-dispatch-probe-fragility: Live release compatibility probes are very fragile
    Priority: Medium. Area: Release validation / Agent Dispatch
    Description: Probe outcomes depend on local authentication state and nondeterministic model formatting. Local runs hit a transient Claude OAuth refresh lock; a rerun completed all provider lifecycles but failed Codex's exact-output assertion solely because it added a trailing period. Scratch-project runs also missed repo-local sign-ins.
    Next step: Revisit the probe acceptance criteria and execution procedure against these observed failures before relying on them as a repeatable release gate.
    Notes: Local evidence: `.agent-layer/tmp/local-release-probes/report.md` and `.agent-layer/tmp/local-release-probes/retry-20260922T001725Z/report.md`.

- Issue 2026-09-20 copilot-native-project-mcp-loading: Copilot CLI no longer documents the generated project MCP path
    Priority: Medium. Area: Copilot CLI integration
    Description: Agent Layer generates `.copilot/mcp-config.json`, but installed Copilot CLI 1.0.83 documents workspace `.mcp.json` or `.github/mcp.json`; `al copilot` does not pass the generated file explicitly. Copilot-only entries can therefore be absent from native discovery. This predates the Muse rebuild.
    Next step: Verify the generated-file loading contract against supported native Copilot versions using an isolated selected-server fixture.
    Notes: Installed CLI help and startup-source evidence retained in `.agent-layer/tmp/muse-rebuild/other-mcp-audit` and the rebuild worktree's `.agent-layer/tmp/muse-critical`.

- Issue 2026-07-28 dispatch-mcp-start-transport-window: An MCP dispatch_start disconnect can orphan a handle
    Priority: Medium. Area: Agent Dispatch MCP interface
    Description: `dispatch_start` is an RPC acknowledgement rather than a direct write to the caller's terminal. If the transport disconnects after the backend starts but before the client observes the response, the dispatch keeps running durably while the caller never learns its handle. This slice deliberately added no idempotency state and no listing API.
    Open question: Has this been observed in practice, justifying a caller-supplied idempotency key or narrow handle-recovery read?
    Notes: Evidence remains under `.agent-layer/tmp/runs/`; documented in docs/AGENT-DISPATCH.md and the dispatch-mcp-interface decision.

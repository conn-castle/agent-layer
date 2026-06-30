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
- Issue YYYY-MM-DD short-slug: Short title
    Priority: Critical | High | Medium | Low. Area: <area>
    Description: <observed problem or risk>
    Next step: <smallest concrete next action>
    Notes: <optional dependencies/constraints>
```

## Open issues

<!-- ENTRIES START -->

- Issue 2026-06-29 dispatch-claude-print-bg-ceiling: Claude print-mode background-task ceiling can end long dispatches without findings
    Priority: Medium. Area: internal/agentdispatch/providers/claude (`al dispatch` Claude path)
    Description: Agent Layer's Claude dispatch path uses `claude --print --output-format stream-json --verbose --include-partial-messages` to preserve structured streaming output. In current Claude Code print mode, if the main turn is otherwise done/input is closed while Claude-managed background tasks/subagents are still running, Claude Code can terminate after 600s with stderr `Background tasks still running after 600s; terminating. Set CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS=0 to wait indefinitely.` This is not an Agent Layer subprocess timeout; Agent Layer starts Claude and waits on the child process.
    Next step: Reproduce and triage the Claude-target dispatch failure mode with a long-running/open-ended Claude Opus/xhigh target that launches Claude-managed background research/subagent work; capture stdout/stderr, elapsed time, and whether findings are lost.
    Notes: Reported locally with Claude Code 2.1.195; installed Claude binary contained `CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS`, the stderr text above, and a default 600000 ms ceiling. User-visible failure: `al dispatch` can return after roughly 10 minutes without the expected findings even though the target-managed background work was still active.

- Issue 2026-06-23 mcp-commandcontext-kill-path-untested: No test exercises the exec.CommandContext kill-on-timeout path for stdio MCP discovery
    Priority: Low. Area: internal/warnings/mcp_connector.go (`ConnectAndDiscover` stdio branch), internal/warnings/mcp_test.go
    Description: The stdio MCP discovery now binds the spawned server process to the discovery context via `exec.CommandContext(ctx, ...)` (carrying `mcpDiscoveryTimeout`) so a hung server is SIGKILLed directly on timeout rather than only torn down via session Close(). Existing stdio tests (`TestRealConnector_StdioConnectionError`, `TestRealConnector_StdioWithEnv`) execute the `exec.CommandContext` LINE (so it is covered for the coverage gate) but only on the connect-failure path; none drives a live, deliberately-hanging child process to verify the context cancel/timeout actually kills it without a goroutine leak or double-Wait against the SDK's CommandTransport.Close(). The swap's correctness was verified by code trace (the SDK's Close() owns the only cmd.Wait(); watchCtx drains via that Wait), so this is a coverage gap, not a known defect.
    Next step: Human to decide whether a live-process kill test is worth the flakiness/platform risk (spawning a real hanging command, e.g. `sleep`, with a short ctx and asserting the process is reaped). Deferred because such a test is timing- and platform-sensitive and the behavior is already correct by trace.
    Notes: Flagged by the iteration #8 re-audit (concurrency/cancellation lens). Coverage threshold is unaffected; the line is hit by existing tests.

- Issue 2026-06-23 cli-no-signal-aware-root-context: CLI never installs a cancellable, signal-aware root context, so Ctrl-C cannot abort in-flight network calls early
    Priority: Low. Area: cmd/al/main.go (`runMain`/`execute`), context plumbing across update/init/doctor
    Description: `cmd.Execute()` is used (not `ExecuteContext`) and there is no `signal.NotifyContext` anywhere, so cobra defaults `cmd.Context()` to `context.Background()`. The HTTP request plumbing is already correct (`update/check.go:101` and `cmd/al/init.go:188` use `http.NewRequestWithContext`; all clients carry explicit timeouts), but that context can never be cancelled by Ctrl-C — only by the per-call 10s/30s timeouts. Consequence: Ctrl-C during an `al sync` update check, `al init --version latest` release lookup, or `al doctor` MCP discovery does not abort early; the user waits out the timeout. No unbounded hang (every path is timeout-bounded), so this is a responsiveness/robustness gap, not a correctness defect.
    Next step: Human to decide whether to wrap the root with `ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)` in the entrypoint and call `cmd.ExecuteContext(ctx)`. This is a small contained change that makes the existing per-call context plumbing Ctrl-C-cancellable, but it changes user-facing interrupt behavior (the CLI entrypoint), so it is deferred as a UX/behavior decision rather than applied unilaterally.
    Notes: Flagged by the iteration #8 cancellation/timeout lens. HUMAN DECISION CANDIDATE (behavior/UX change at the CLI entrypoint).

- Issue 2026-06-23 ci-wire-govulncheck: Consider wiring govulncheck into CI for ongoing stdlib/dependency vulnerability coverage
    Priority: Low. Area: .github/workflows/ci.yml, Makefile (no `make vuln` target exists today)
    Description: Iteration #8 ran govulncheck and found 4 reachable Go STANDARD LIBRARY vulnerabilities (GO-2026-5039 net/textproto, GO-2026-5037 crypto/x509, GO-2026-4971 net, GO-2026-4918 net/http) in the `al` binary built with go1.26.2 — all fixed by bumping the build toolchain (now pinned via `toolchain go1.26.4` in go.mod; rebuild + rescan reports "No vulnerabilities found"). No third-party module dependency was vulnerable. There is currently no automated guard against the toolchain/dep floor regressing in future. Note: govulncheck source mode (`./...`) currently PANICS on this repo under Go 1.26 (`ForEachElement called on type containing *types.TypeParam` in golang.org/x/tools@v0.46.0 ssa); only binary mode (`-mode binary` on a built binary) works reliably, so any CI wiring must use binary mode (or module mode) until the x/tools generics bug is fixed upstream.
    Next step: Human to decide whether to add a CI job / Makefile target that builds the release binaries and runs `govulncheck -mode binary` (fails on reachable vulns). Tradeoff: it needs network access for the vuln DB (most CI has this) and adds CI time/maintenance; source mode is currently unusable here. Options: (A) add binary-mode govulncheck to `make ci` as a hard gate; (B) add a non-blocking scheduled (e.g. weekly) workflow that reports but does not fail builds; (C) leave manual. Recommendation: (B) as a low-friction safety net that avoids blocking PRs on a network-dependent external DB and on the source-mode panic.
    Notes: Flagged by the iteration #8 govulncheck lens. HUMAN DECISION CANDIDATE (CI policy + network/time tradeoff). The reachable vulns themselves are already FIXED this iteration via the toolchain pin.

- Issue 2026-06-23 resolve-baseline-version-silent-unknown: resolveBaselineVersion still maps a malformed al.version pin to "unknown" silently
    Priority: Low. Area: internal/install/ownership_manifest.go (`resolveBaselineVersion`, ~line 326-342)
    Description: Iteration #7 made `readCurrentPinVersion` fail loud on a non-empty unparseable `.agent-layer/al.version` (it now returns an error instead of `("", nil)`). Its sibling `resolveBaselineVersion` reads the SAME pin file but, on a read error or `version.Normalize` failure, silently falls through to `baselineVersionUnknown` with no diagnostic. This is the same silent-swallow shape, but on a best-effort DISPLAY-LABEL path: `resolveBaselineVersion` returns a plain `string` (no error channel) used only to populate baseline metadata, so a corrupt pin yields a "unknown" baseline label rather than steering behavior. Not a behavior defect today, but an inconsistency with the fail-loud rule for the identical input.
    Next step: Human to decide whether `resolveBaselineVersion` should surface the malformed-pin condition (requires giving it an error return or a sentinel that callers thread into metadata reasons), or whether "unknown" is the intended graceful label here. Adding an error channel changes the function contract and its one call site (the manifest baseline builder), so it is a deliberate choice, not a mechanical edit.
    Notes: Deferred as a fail-loud consistency tradeoff on a label-only path (no error channel today); flagged by the iteration #7 re-audit.

- Issue 2026-06-23 skill-frontmatter-two-parsers-divergent: SKILL.md front-matter is parsed by two independent hand-written parsers that can drift
    Priority: Low. Area: internal/config/skills.go (`parseSkillFrontMatter`) and internal/skillvalidator/frontmatter.go (`parseFrontMatter`)
    Description: The same user-authored `.agent-layer/skills/<name>/SKILL.md` front-matter is parsed by two separate yaml.Node walkers — skills.go on the sync/load path and skillvalidator on the `al doctor` path. They share structure but differ in detail: duplicate-key handling (skills.go `if key != "" && seen[key]` marks `seen` for empty keys too; skillvalidator `if key == "" { continue }` skips empty keys before dedup), field coverage (skillvalidator only string-validates name/description/compatibility/license/allowed-tools and ensures the metadata shape, while skills.go also extracts metadata values and name-multiline style), and error message text. No current concrete defect or panic — both reject the common malformed cases — but two parsers for one input is a single-source-of-truth risk: they can diverge on edge cases (e.g. duplicate empty keys, unusual node kinds) so `al sync` and `al doctor` could disagree about whether a skill is valid.
    Next step: Human to decide the consolidation: (A) extract one shared front-matter parser (e.g. in a small internal package) that both load and validate call, returning a typed result the validator can inspect; (B) keep two parsers but add a shared conformance test fixture set that both must agree on. Option A removes the drift risk at the cost of reshaping the validator's node-level inspection; either is a deliberate refactor, not a mechanical edit.
    Notes: Deferred as a maintainability/architecture refactor (no live defect surfaced); flagged by the iteration #7 input-robustness lens.

- Issue 2026-06-23 dep-go-udiff-v04-move-diff-regression: go-udiff v0.4.x changes move-only diff rendering and defeats the upgrade-preview noise suppression
    Priority: Low. Area: internal/install/diff_preview.go (`collapseEquivalentDiffRun`/`normalizeUnifiedDiffPreview`); dep github.com/aymanbagabas/go-udiff
    Description: Bumping go-udiff v0.3.1 -> v0.4.1 broke TestRenderTruncatedUnifiedDiff_CollapsesEquivalentMovedLines. v0.4.x emits a move-only reorder (e.g. .gemini/.claude swap) as `+/.claude/`, ` /.gemini/` (context), `-/.claude/` — interleaving a CONTEXT line between the equivalent +/- pair. Our `collapseEquivalentDiffRun` only collapses CONTIGUOUS runs of change lines, so the move no longer collapses to empty and upgrade previews would show phantom diffs for files whose managed content did not actually change. The bump was REVERTED (only safe bump deferred); the other 7 direct-dep bumps were applied and CI stays green.
    Next step: Human to decide whether to make the noise-suppression heuristic pair equivalent +/- lines ACROSS context lines within a hunk (restores collapse, but cross-context pairing could mask a genuine move+edit), or keep go-udiff pinned at v0.3.1. Option A (cross-context pairing) is the path to adopt v0.4.x; it changes a user-facing diff heuristic's semantics, so it is a deliberate call, not a mechanical edit.
    Notes: Deferred because the fix changes user-facing diff-preview behavior (a heuristic tradeoff), not a mechanical bump. All other dep currency work shipped this iteration.

- Issue 2026-06-23 dep-major-bumps-charm-huh-bubbles: Major-version bumps available for charmbracelet/huh and charmbracelet/bubbles (and a blocked golangci-lint bump)
    Priority: Low. Area: go.mod direct deps (charmbracelet/huh, charmbracelet/bubbles, golangci-lint/v2)
    Description: `go list -m -u all` shows charmbracelet/huh v0.8.0 -> v1.0.0 and charmbracelet/bubbles (pinned pseudo-version) -> v1.0.0 — both MAJOR bumps with breaking APIs in the bubbletea/huh v1 line (the v2 charm stack reworks lipgloss/x-ansi). Per CLAUDE.md, major/breaking bumps need confirmation + compatibility work and were NOT applied. Separately, golangci-lint/v2 v2.10.1 -> v2.12.2 was attempted but REVERTED: v2.12.2 pulls charm.land/lipgloss/v2 which forces charmbracelet/x/ansi v0.11.7 module-wide, and x/ansi v0.11.7 breaks the pinned x/cellbuf v0.0.13 used by our huh/bubbletea (the `ansi.Style` Italic/Underline/SlowBlink API changed). The golangci-lint bump is therefore coupled to the charm v1/v2 migration and cannot land independently.
    Next step: Human to schedule a charmbracelet stack migration (huh/bubbles/bubbletea v1 -> the unified charm v2 line) as one coordinated change; the golangci-lint bump can ride along once x/ansi can move to v0.11.x without breaking cellbuf. Until then keep huh/bubbles/golangci-lint pinned.
    Notes: Deferred per "breaking dep bumps need confirmation". Not a defect; a coordinated upgrade-planning task.

- Issue 2026-06-23 codex-malformed-projects-silent-trust-skip: Malformed `agent_specific.projects` silently suppresses the managed Codex trust block
    Priority: Low. Area: internal/sync/codex.go (`codexAgentSpecificDefinesProject`, line ~184)
    Description: `agent_specific` is `map[string]any` with no value-shape validation (the `projects` key is allowlisted in internal/config/agent_specific.go but its value type is unchecked). When a user writes `projects = "foo"` (scalar) or `projects = ["a"]` (array) under `[agents.codex.agent_specific]`, `codexAgentSpecificDefinesProject` hits `return true` (the non-table branch), so `appendCodexTrustedProject` skips emitting the managed `[projects."<root>"] trust_level = "trusted"` block. The repo silently loses Codex project trust, while the malformed `projects` is still emitted verbatim — a hidden-default/silent-fallback that degrades a security-adjacent control with no error. Reachable, not dead.
    Next step: Human to choose the disposition: (A) fail loud — change `codexAgentSpecificDefinesProject` to return an error so a non-table `projects` aborts sync with an actionable message; (B) write managed trust defensively (`return false`) — but this risks a TOML key conflict (`projects` as both scalar and table) that Codex would reject; (C) leave as-is and document that `agent_specific.projects` must be a table. Option A is the cleanest fail-loud fit but changes the function signature and call site.
    Notes: Deferred because the safest behavior is a genuine tradeoff (fail-loud vs defensive-write vs document) on a trust control, not a mechanical fix.

- Issue 2026-06-23 completion-windows-build-tag-misleading: `//go:build !windows` on cmd/al/completion.go implies Windows support the project cannot provide
    Priority: Low. Area: cmd/al (completion.go build constraint; platform story)
    Description: completion.go carries `//go:build !windows`, but the project as a whole is unbuildable on Windows (internal/dispatch depends on `unix.Flock`/`unix.LOCK_EX` with no Windows fallback), and platform.go calls `newCompletionCmd()` unconditionally with no Windows variant. The tag therefore signals partial Windows support that does not exist and would leave platform.go referencing an undefined symbol on a Windows build. Latent inconsistency, not a runtime defect on supported (Unix) platforms.
    Next step: Human to decide the platform story. If Windows is never intended: remove the misleading `!windows` tag from completion.go. If Windows is intended: the larger correct fix is to add Windows variants for dispatch/platform (the opposite, much bigger change). Either direction is a deliberate decision, not a mechanical edit.
    Notes: Deferred because it is a project-direction (Windows support) judgment call with two opposite valid resolutions.

- Issue 2026-06-23 dispatch-lock-wait-vs-download-timeout: Dispatch lock wait timeout equals download timeout, so a waiter can fail while the holder is still legitimately downloading
    Priority: Low. Area: internal/dispatch (lock.go `lockWaitTimeout`, cache.go `defaultDownloadTimeout`)
    Description: `withFileLock` holds the exclusive lock for the entire binary download+verify closure. A concurrent `al` invocation waits up to `lockWaitTimeout` (30s) and then fails with a lock-timeout error. The holder's own download is bounded by `defaultDownloadTimeout` (also 30s), so under a genuinely slow cold-cache download a second process can spuriously fail with a lock timeout exactly as the holder is still making valid progress. Real cross-process robustness edge, only in the cold-cache concurrent-invocation window; not a correctness bug.
    Next step: Make `lockWaitTimeout` comfortably exceed the worst-case download timeout (e.g. derive it from `downloadTimeoutWithSystem` plus headroom) so a waiter never gives up before the holder's own download deadline resolves the contention.
    Notes: Deferred because the timeout values are a deliberate "waiters fail rather than pile up" tuning choice; bumping the wait timeout is a judgment call on contention behavior, not a clear-cut win.

- Issue 2026-06-22 secret-in-url-precision-recall: Secret-in-URL detection precision vs recall tradeoff (needs human decision)
    Priority: Medium. Area: internal/warnings/policy.go (`looksLikeSecretQueryKey` and helpers)
    Description: Status quo (retained): secret-bearing query-param keys are matched by SUBSTRING — good recall (flags camelCase keys like `accessToken`/`authToken`/`apiToken`/`clientSecret` because e.g. "token" is a substring) but FALSE POSITIVES on benign keys containing a secret word (`?author=`, `?authority=`, `?tokenizer=`, `?passwordless=`), which currently fail `al sync` with a CRITICAL `POLICY_SECRET_IN_URL` finding. The obvious fix (word-segment matching) removes those false positives but REGRESSES recall: glued/camelCase secret keys (`accessToken`, `authtoken`, `clientSecret`) would no longer be flagged — a genuine security precision/recall tradeoff, not a clear win. An /improve-codebase rewrite of this logic was reverted pending this decision.
    Next step: Human to choose: (A) keep substring matching + add an explicit exclusion list of known-benign keys (preserves recall, surgical); (B) word-segment matching PLUS camelCase boundary splitting (fixes most false positives, restores camelCase recall, still misses fully-glued lowercase like `authtoken`); (C) accept the recall loss and ship pure word-segment matching.
    Notes: Recommendation: Option B is strongest if pursued, but this needs the maintainer's call because it changes a security control's detection semantics.

- Issue 2026-06-22 semver-parse-compare-dispatch-divergent-variant: dispatch's bespoke parseSemver diverges from the shared version helper
    Priority: Low. Area: version/dispatch
    Description: The byte-identical `compareSemver`/`parseSemver` copies in install and update were consolidated into `internal/version` (`version.Compare`/`version.Parse`). `internal/dispatch/dispatch.go` still has its own `parseSemver` returning `(int,int,int,bool)` that skips `version.Normalize` (no `v`-prefix strip, no error) — its only caller normalizes first, so not a live bug, just divergence from the now-canonical helper.
    Next step: Route dispatch through `version.Parse`/`version.Compare`. Human decision needed: whether dispatch adopts the error-returning shape, and where the shared error-message constants belong (the consolidated helper still references `messages.UpdateInvalidVersion*`; a neutral `messages.Version*` home is the cleaner end state).
    Notes: Single-source-of-truth drift; not a runtime defect. Deferred pending the shape/constants decision.

- Issue 2026-06-22 fs-os-abstraction-four-patterns: OS/filesystem testability seam implemented four different ways across packages
    Priority: Low. Area: architecture/cross-cutting
    Description: install, sync, dispatch, launchers each define a `System` interface + `RealSystem` os-passthrough (install/sync overlap on 7 of 11 methods, each re-implemented); config uses both `fs.FS` injection AND a `var osReadFileFunc` function-pointer seam in the same package; doctor/wizard/update/clients use ad-hoc function-pointer vars. Same problem (testable OS access) solved four ways.
    Next step: Decide and document one standard seam style, or extract a shared `internal/osfs` (or expand `internal/fsutil`) that install/sync/launchers embed; at minimum have config pick one of its two internal seams.
    Notes: Genuine architectural tradeoff with multiple defensible options; not a defect. Broad change touching deprioritized install/sync — needs a human decision.

- Issue 2026-05-23 dispatch-antigravity-argv-prompt-cap: Antigravity dispatch still caps prompt size because `agy` has no stdin/prompt-file path
    Priority: Low. Area: providers/antigravity
    Description: `internal/agentdispatch/adapters.go` `runAntigravity` must pass the prompt as a single argv element after `--print` (Antigravity exposes no stdin or prompt-file path). The opaque-failure defect is fixed: a pre-flight guard now rejects prompts over `AntigravityPromptMaxBytes` with a typed `ExitUsage` error and an actionable message (tested by `TestRunAntigravityRejectsOversizePrompt`). The residual limitation is the cap itself — Claude/Codex accept unbounded prompts on stdin, Antigravity does not.
    Next step: When upstream `agy --print` gains a stdin or `--prompt-file` path, switch to it and remove the size cap.
    Notes: Upstream `agy --help` as of 2026-05-22 shows no stdin/prompt-file path; deferred pending upstream CLI support. No code change available until then.

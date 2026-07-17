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

- Issue 2026-07-15 dispatch-random-override-filter-policy: Random selection can choose a provider that rejects a requested override
    Priority: Medium. Area: internal/agentdispatch random resolution and fresh preflight
    Description: Random eligibility considers enabled, installed, compatible, and caller facts, while requested model and reasoning support are validated only after a target is chosen; dispatch can therefore fail on the chosen provider even when another eligible pool member supports the override.
    Next step: Decide whether random pools should be filtered by requested override support or preserve provider-only eligibility and fail after selection, then encode the chosen public policy in focused selection tests.
    Notes: Deferred because filtering semantics are a public-policy choice outside PR #151's classified production repairs.

- Issue 2026-07-13 corrupt-run-record-blocks-delete-cancel: A corrupt run record leaves its mapping undeletable and uncancellable
    Priority: Low. Area: internal/agentdispatch/operations.go (Delete, Cancel) + state.go retention
    Description: Delete and Cancel deliberately fail loud on a corrupt referenced run record (tested behavior consistent with the retention stance of never hiding corrupt evidence), so the only recovery is manual removal of the run directory; history already skips-and-warns, but the mapping stays blocked and retention never expires nonterminal corrupt records.
    Next step: Human to decide whether Delete should treat a provably corrupt record as inactive (releasing the mapping while preserving the evidence) or whether manual cleanup stays the contract.
    Notes: Dispatch is unreleased; blast radius today is developer-local state.

- Issue 2026-07-13 dispatch-redesign-performance-observation: Representative post-cutover workflow timing is not yet observed
    Priority: Medium. Area: full-workflow rollout evidence
    Description: The redesign implementation can be verified locally with fixtures, race tests, and repository checks, but this implementation run is explicitly prohibited from shipping itself or running another full workflow; planning, quality-stage, pull-request-open, shipping-overhead, and merge-continuation targets therefore lack three post-cutover representative observations.
    Next step: After user review and rollout, observe at least one tiny/local, one normal, and one concurrency/cross-cutting workflow; record controlled time separately from external wait and compare each phase to its target.
    Notes: This is the only intentionally deferred acceptance measurement; it does not authorize weakening functional gates. Known optimization candidate if targets are missed: the per-event run-record read in runner.go `consume` used for cooperative cancellation detection.

- Issue 2026-07-13 dispatch-host-yield-descendant-proof: Dispatch cannot control chat-host yielding or universally prove provider-native descendant terminality
    Priority: Critical. Area: agent dispatch host integration and provider lineage
    Description: Synchronous dispatch now separates progress/results, excludes concurrent resume, preserves recovery/history, reconciles owned processes, supports cancellation, and exposes factual activity; however a chat host may still yield the terminal command without a completion callback, and providers do not uniformly expose descendant lineage sufficient to prove every native child terminal.
    Next step: Add host-native completion wake-up only when the host offers a supported callback/tool boundary, and extend lineage claims provider-by-provider from authoritative events; do not infer terminality or poll inspect.
    Notes: The Claude background-wait ceiling is already fixed and is explicitly excluded.

- Issue 2026-07-10 antigravity-malformed-native-aborts-all-sync: A malformed native Antigravity settings.json fails the entire multi-client sync
    Priority: Low. Area: internal/sync/antigravity.go (readAntigravitySettings/mergeAntigravitySettings) + sync.go step ordering
    Description: WriteAntigravitySettings fails loud with no data loss on malformed/non-object/shape-conflict native settings.json, but the error bubbles up as a hard sync step, so one odd native value blocks generation for Claude/Codex/VS Code too. Overlay-only merge plus empty-file tolerance shrank the failure surface, but the blast radius is unchanged.
    Next step: Human to decide whether a single client's settings failure should warn-and-skip that client instead of aborting the whole run.
    Notes: Codex second-opinion flagged this as a separate product-level decision; intentionally not changed in the overlay-preserve change (decision antigravity-settings-overlay-preserve).

- Issue 2026-07-10 antigravity-managed-values-not-reclaimed: Agent Layer never reclaims managed Antigravity settings it previously wrote
    Priority: Low. Area: internal/sync/antigravity.go (overlay-only mergeAntigravitySettings)
    Description: settings.json is overlay-patched and never delete-on-absent, so a managed value (model, permissions.allow, agent_specific) removed from config.toml — or left behind when Antigravity is disabled — lingers in the file, indistinguishable from native state, and is never cleaned by Agent Layer. This is the accepted tradeoff of preserving native state.
    Next step: If reclaiming stale Agent Layer-written keys becomes a demonstrated need, add provenance tracking (a managed-key manifest/marker) so removed keys can be pruned without touching native values.
    Notes: Deferred by decision antigravity-settings-overlay-preserve (2026-07-10); provenance was judged too much persistent state for a preservation fix.

- Issue 2026-07-10 antigravity-settings-format-unverified: Antigravity settings.json treated as strict JSON without primary-source confirmation
    Priority: Low. Area: internal/sync/antigravity.go (readAntigravitySettings strict json.Decoder)
    Description: The merge parses native settings.json as strict JSON. Secondary sources (codelabs/tutorials) consistently describe it as a plain JSON file, but the official docs page is a JS app that could not be machine-read, so the format was not confirmed from a primary source. If Antigravity actually permits JSONC (comments/trailing commas), a native comment would fail every sync loud until hand-edited.
    Next step: Confirm the format from a primary source (official spec or an observed real file); if JSONC, add tolerant parsing for this client instead of strict JSON.
    Notes: Verification gap recorded in decision antigravity-settings-overlay-preserve (2026-07-10).

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

- Issue 2026-06-23 completion-windows-build-tag-misleading: `//go:build !windows` on cmd/al/completion.go implies Windows support the project cannot provide
    Priority: Low. Area: cmd/al (completion.go build constraint; platform story)
    Description: completion.go carries `//go:build !windows`, but the project as a whole is unbuildable on Windows (internal/dispatch depends on `unix.Flock`/`unix.LOCK_EX` with no Windows fallback), and platform.go calls `newCompletionCmd()` unconditionally with no Windows variant. The tag therefore signals partial Windows support that does not exist and would leave platform.go referencing an undefined symbol on a Windows build. Latent inconsistency, not a runtime defect on supported (Unix) platforms.
    Next step: Human to decide the platform story. If Windows is never intended: remove the misleading `!windows` tag from completion.go. If Windows is intended: the larger correct fix is to add Windows variants for dispatch/platform (the opposite, much bigger change). Either direction is a deliberate decision, not a mechanical edit.
    Notes: Deferred because it is a project-direction (Windows support) judgment call with two opposite valid resolutions.

- Issue 2026-06-22 secret-in-url-precision-recall: Secret-in-URL detection precision vs recall tradeoff (needs human decision)
    Priority: Medium. Area: internal/warnings/policy.go (`looksLikeSecretQueryKey` and helpers)
    Description: Status quo (retained): secret-bearing query-param keys are matched by SUBSTRING — good recall (flags camelCase keys like `accessToken`/`authToken`/`apiToken`/`clientSecret` because e.g. "token" is a substring) but FALSE POSITIVES on benign keys containing a secret word (`?author=`, `?authority=`, `?tokenizer=`, `?passwordless=`), which currently fail `al sync` with a CRITICAL `POLICY_SECRET_IN_URL` finding. The obvious fix (word-segment matching) removes those false positives but REGRESSES recall: glued/camelCase secret keys (`accessToken`, `authtoken`, `clientSecret`) would no longer be flagged — a genuine security precision/recall tradeoff, not a clear win. An /improve-codebase rewrite of this logic was reverted pending this decision.
    Next step: Human to choose: (A) keep substring matching + add an explicit exclusion list of known-benign keys (preserves recall, surgical); (B) word-segment matching PLUS camelCase boundary splitting (fixes most false positives, restores camelCase recall, still misses fully-glued lowercase like `authtoken`); (C) accept the recall loss and ship pure word-segment matching.
    Notes: Recommendation: Option B is strongest if pursued, but this needs the maintainer's call because it changes a security control's detection semantics.

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

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

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

- Issue 2026-06-22 secret-in-url-precision-recall: Secret-in-URL detection precision vs recall tradeoff (needs human decision)
    Priority: Medium. Area: internal/warnings/policy.go (`looksLikeSecretQueryKey` and helpers)
    Description: Status quo (retained): secret-bearing query-param keys are matched by SUBSTRING — good recall (flags camelCase keys like `accessToken`/`authToken`/`apiToken`/`clientSecret` because e.g. "token" is a substring) but FALSE POSITIVES on benign keys containing a secret word (`?author=`, `?authority=`, `?tokenizer=`, `?passwordless=`), which currently fail `al sync` with a CRITICAL `POLICY_SECRET_IN_URL` finding. The obvious fix (word-segment matching) removes those false positives but REGRESSES recall: glued/camelCase secret keys (`accessToken`, `authtoken`, `clientSecret`) would no longer be flagged — a genuine security precision/recall tradeoff, not a clear win. An /improve-codebase rewrite of this logic was reverted pending this decision.
    Next step: Human to choose: (A) keep substring matching + add an explicit exclusion list of known-benign keys (preserves recall, surgical); (B) word-segment matching PLUS camelCase boundary splitting (fixes most false positives, restores camelCase recall, still misses fully-glued lowercase like `authtoken`); (C) accept the recall loss and ship pure word-segment matching.
    Notes: Recommendation: Option B is strongest if pursued, but this needs the maintainer's call because it changes a security control's detection semantics.

- Issue 2026-06-22 upgrade-path-hardcoded-user-strings: Upgrade/migration code path is the lone island of inline user-facing strings
    Priority: Low. Area: install/cmd-al (consistency)
    Description: 8 of 10 packages route user-facing text through `internal/messages`. The exception is concentrated in the upgrade path: `internal/install/upgrade_migrations_skills.go` (~17 inline ew.println/printf migration-notice lines + 4 inline fmt.Errorf sentences), `upgrade_migrations.go:639,650` (two inline warnWriter warnings), `cmd/al/upgrade.go` (many inline plan-render titles/labels alongside ~60 messages refs), `cmd/al/mcp_prompts.go:12,15` (inline cobra Short + deprecation; no messages import). The config straggler env_subst.go was fixed in this sweep.
    Next step: Move the upgrade/migration notice text and cmd/al/upgrade.go plan-render labels into `messages` constants; make cmd/al/mcp_prompts.go import/use messages.
    Notes: Consistency drift, not a defect. Files are deprioritized (recently churned install/cmd-al); defer to a focused messages-consolidation pass.

- Issue 2026-06-22 semver-parse-compare-duplicated-three-packages: Semver parse/compare duplicated across install, update, dispatch with one divergent variant
    Priority: Low. Area: version/install/update/dispatch
    Description: `internal/version` owns the canonical `Normalize`/`IsDev` + regex but has NO compare. `internal/install/upgrade_migrations.go` (`compareSemver`/`parseSemver`) and `internal/update/check.go` (`compareSemver`/`parseSemver`) are byte-for-byte identical copies (install even reuses update's `messages.UpdateInvalidVersion*` constants). `internal/dispatch/dispatch.go` has a 4th `parseSemver` returning `(int,int,int,bool)` that skips `version.Normalize` (no `v`-prefix strip) — but its only caller normalizes first, so not a live bug, just divergence.
    Next step: Add `version.Compare(a, b string) (int, error)` to `internal/version`, delete the three duplicates, route all callers through it. Human decision needed on where the shared error-message constants live (move from `messages.Update*` to a neutral `messages.Version*`) and whether dispatch adopts the error-returning shape.
    Notes: Single-source-of-truth violation; not a runtime defect. Spans 5 packages incl. deprioritized install/sync; defer to a focused refactor PR.

- Issue 2026-06-22 fs-os-abstraction-four-patterns: OS/filesystem testability seam implemented four different ways across packages
    Priority: Low. Area: architecture/cross-cutting
    Description: install, sync, dispatch, launchers each define a `System` interface + `RealSystem` os-passthrough (install/sync overlap on 7 of 11 methods, each re-implemented); config uses both `fs.FS` injection AND a `var osReadFileFunc` function-pointer seam in the same package; doctor/wizard/update/clients use ad-hoc function-pointer vars. Same problem (testable OS access) solved four ways.
    Next step: Decide and document one standard seam style, or extract a shared `internal/osfs` (or expand `internal/fsutil`) that install/sync/launchers embed; at minimum have config pick one of its two internal seams.
    Notes: Genuine architectural tradeoff with multiple defensible options; not a defect. Broad change touching deprioritized install/sync — needs a human decision.

- Issue 2026-06-22 gentemplatemanifest-managed-set-no-completeness-check: Manifest generator's managed-file set is hardcoded with no completeness guard against the embedded template tree
    Priority: Low. Area: tools/release
    Description: `internal/tools/gentemplatemanifest/main.go` collects managed templates from hardcoded `rootFiles` (~line 129) and `dirs` (~line 149) lists. Adding a new upgrade-managed root template or directory requires editing these by hand; nothing asserts the generated manifest covers exactly the set of upgrade-managed templates. A forgotten entry degrades to the runtime `OwnershipUnknownNoBaseline` fallback (user-facing "unknown" prompt) rather than a clobber — degraded UX, not data loss.
    Next step: Add a test that walks the embedded template tree, applies the known managed/excluded partition, and asserts the generator's collected dest paths match, so a missed wiring fails CI.
    Notes: Not a current defect; safety net is fail-loud-ish (unknown classification). Defer because the test needs a careful managed/excluded partition definition shared with the generator.

- Issue 2026-05-23 dispatch-antigravity-argv-prompt-cap: Antigravity dispatch still caps prompt size because `agy` has no stdin/prompt-file path
    Priority: Low. Area: providers/antigravity
    Description: `internal/agentdispatch/adapters.go` `runAntigravity` must pass the prompt as a single argv element after `--print` (Antigravity exposes no stdin or prompt-file path). The opaque-failure defect is fixed: a pre-flight guard now rejects prompts over `AntigravityPromptMaxBytes` with a typed `ExitUsage` error and an actionable message (tested by `TestRunAntigravityRejectsOversizePrompt`). The residual limitation is the cap itself — Claude/Codex accept unbounded prompts on stdin, Antigravity does not.
    Next step: When upstream `agy --print` gains a stdin or `--prompt-file` path, switch to it and remove the size cap.
    Notes: Upstream `agy --help` as of 2026-05-22 shows no stdin/prompt-file path; deferred pending upstream CLI support. No code change available until then.

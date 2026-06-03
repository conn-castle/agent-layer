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

- Issue 2026-06-03 local-lint-ci-parity-gap: `make lint` does not reproduce CI golangci-lint findings
    Priority: Medium. Area: tooling/lint
    Description: CI `make ci` failed on a `goconst` finding (`internal/install/statusline_sources.go`: a 3x string should be a constant), but local `make lint` reports `0 issues` on the same code — even with the go.mod-pinned golangci-lint v2.10.1 in `.tools/bin` and a cleared `GOLANGCI_LINT_CACHE`. Local gives false confidence and lets lint failures reach CI. Suspected golangci-lint build/toolchain or GOCACHE-analysis difference between local (darwin) and CI (linux).
    Next step: Reproduce by linting with a fully fresh `GOCACHE` (and on linux/container matching CI) to isolate cache-vs-version-vs-OS; pin the lint result so local `make lint` matches CI.
    Notes: Surfaced while fixing the v0.11.0 PR CI; the specific goconst finding was fixed by extracting `claudeStatuslineSourceRelPath`/`codexStatuslineSourceRelPath` constants.

- Issue 2026-06-03 wizard-no-selfheal-missing-statusline-source: Wizard does not reseed a missing status line source on an enabled-but-untouched repo
    Priority: Medium. Area: wizard/statusline
    Description: `selectedStatuslineSourceFiles` (`internal/wizard/apply_statusline.go:36-58`) seeds a source only when the toggle was touched (`ClaudeStatuslineTouched`/`CodexStatuslineTouched`). If config already has `statusline = true` but the source is missing (deleted, or hand-set), running `al wizard` without re-toggling seeds nothing, then sync fails loud (`internal/sync/claude_statusline.go:90-105`). The DECISIONS promise that running `al wizard` repairs the missing-source state is not fully met.
    Next step: Seed when the effective post-wizard config is enabled (key off `ClaudeStatusline && visible`, not `*Touched`), or detect enabled-but-missing-source before apply and surface it.
    Notes: Rarer now that the upgrade default is off; surfaced by the v0.11.0 audit and deferred to keep the release scoped.

- Issue 2026-06-03 statusline-untracked-perf: Claude status line spawns one git process per untracked file
    Priority: Medium. Area: templates/claude-statusline.sh
    Description: The lines-changed computation loops over `git ls-files -o --exclude-standard` and runs `git diff --numstat --no-index` once per untracked file (`internal/templates/claude-statusline.sh:189-198`). The status line renders on every prompt, so a repo with many un-ignored untracked files (generated assets, fresh scaffold) spawns dozens-to-hundreds of git subprocesses per render and can visibly stall the prompt.
    Next step: Bound the untracked loop (cap at N files, mark count approximate) or count untracked lines in a single cheaper pass instead of N `git diff --no-index` invocations.
    Notes: Surfaced by the v0.11.0 audit; deferred as a perf tune of a shell hot path, not a correctness bug.

- Issue 2026-06-03 statusline-legacy-source-orphan: Legacy .agent-layer/statusline.sh is copied but never removed
    Priority: Low. Area: install/sync statusline-sources
    Description: When seeding a missing `claude-statusline.sh`, both `seedMissingStatuslineSource` (`internal/install/statusline_sources.go:103-115`) and `ensureStatuslineSource` (`internal/sync/claude_statusline.go:90-100`) copy legacy `.agent-layer/statusline.sh` content but leave the legacy file on disk. It is not in `knownTemplateFiles`, so the next upgrade flags it as an unknown and prompts to delete it; duplicated content lingers in the interim.
    Next step: Remove the legacy source after a successful migration copy, or add its rel-path to the known/unknown classifier to suppress the spurious prompt. Confirm intended retention semantics first.
    Notes: Legacy `statusline.sh` was never shipped in a release, so real-world impact is near zero; deferred from the v0.11.0 audit.

- Issue 2026-06-03 statusline-source-mapping-duplicated: Status line source mapping duplicated across wizard and install
    Priority: Low. Area: wizard/install statusline-sources
    Description: The relPath -> templatePath -> perm mapping for the status line sources is hand-written in two packages: `selectedStatuslineSourceFiles` (`internal/wizard/apply_statusline.go:36-58`) and `statuslineSourceTemplates` (`internal/install/statusline_sources.go:21-35`). The wizard copy omits the legacy-path migration and the overwrite/diff prompt. Drift is silent — adding a third source or changing a perm in one place can be missed in the other, against the single-source-of-truth rule.
    Next step: Have the wizard derive its seed list from the install package's `statuslineSourceTemplates()` (or a shared helper) so the canonical mapping lives in one place.
    Notes: Surfaced by the v0.11.0 audit; deferred to keep the release scoped.

- Issue 2026-06-03 statusline-source-preview-vs-seed-mismatch: Upgrade preview shows template content but seeds legacy content for the Claude statusline source
    Priority: Low. Area: install/statusline-sources
    Description: When `.agent-layer/claude-statusline.sh` is missing but legacy `.agent-layer/statusline.sh` exists, `planStatuslineSourceChanges` previews the addition using the embedded template (`statuslineSourceUpgradeChange` -> `templatePath`), but `seedMissingStatuslineSource` actually writes the legacy file's content (`internal/install/statusline_sources.go:103-115` vs `188`). The dry-run/preview therefore misrepresents what will be written. No data loss (the user keeps their legacy content); only a preview-accuracy gap, Claude source only (codex has no legacyRelPath).
    Next step: Have the plan render the legacy-source content (not the template) when a legacy source will be the seed source, or note in the preview that legacy content is being migrated.
    Notes: Surfaced by the v0.11.0 audit; deferred to keep the release scoped — a proper fix touches plan/diff rendering.

- Issue 2026-06-03 interactive-wizard-e2e-gap: Wizard e2e scenarios cover profile mode, not the interactive questionnaire
    Priority: Medium. Area: test-e2e/wizard
    Description: Current wizard e2e scenarios run `al wizard --profile ... --yes`, which exercises profile application and sync but not Huh-driven interactive choices such as workflow bundle, MCP catalog, feature toggles, or CLI-skill selection. A PTY probe blocked on the first form, so the interactive path remains uncovered by a stable e2e.
    Next step: Add a supported deterministic test interface for wizard choices or a reliable PTY harness, then cover the workflow-bundle/default interactive path end to end.
    Notes: Keep the current profile-mode lifecycle test; it catches sync/doctor/file regressions but must not be treated as interactive wizard coverage.

- Issue 2026-06-02 codex-buildconfig-test-only-shim: `buildCodexConfig` survives only as a test seam
    Priority: Low. Area: sync/codex
    Description: The statusline work threaded a `System` through Codex config generation, so production now calls `buildCodexConfigWithSystem(sys, ...)` directly (`internal/sync/codex.go`). The old `buildCodexConfig(root, project)` was reduced to a one-line `RealSystem{}` delegate kept alive solely by ~24 test call sites; no production code calls it. This is a minor test-only seam, not a behavior risk.
    Next step: Migrate the ~24 `buildCodexConfig(` call sites in `internal/sync/codex_test.go` to `buildCodexConfigWithSystem(RealSystem{}, ...)` (or a test system) and delete the shim. Mechanical; deferred to keep the statusline PR scoped.
    Notes: Surfaced by the simplify-new-code pre-pass, which cannot touch tests.

- Issue 2026-06-02 unpinned-upgrade-latest-manifest-only: Unpinned upgrades run only the latest migration manifest, forcing carry-forward of legacy ops
    Priority: Medium. Area: install/upgrade-migrations
    Description: When the source version is unknown/unpinned, `inst.planUpgradeMigrations` loads only the latest manifest (`internal/install/upgrade_migrations.go:204-205`), not the full chain. So a legacy cleanup (e.g. Gemini->Antigravity) reaches unpinned users only while it lives in the latest manifest. Adding `0.11.0` forced duplicating the entire 0.10.2 Gemini op set into `0.11.0.json` so unpinned legacy users still get it — a carry-forward burden that violates single-source-of-truth and silently drops legacy migrations for unpinned users if ever forgotten.
    Next step: Change the unpinned+triggered path to run all `source_agnostic` ops across the full manifest chain (non-source-agnostic ops are already skipped under unknown source), then drop the carried-forward Gemini ops from `0.11.0.json`. Update `TestPlanUpgradeMigrations_UnpinnedMCPGeminiClientTriggersLatestManifest` and `...UnpinnedNoLegacyTriggerSkipsManifest`, which encode the current single-manifest behavior.
    Notes: This is "Option 2" deferred from the statusline work; pinned upgrades already chain correctly and are unaffected.

- Issue 2026-05-23 dispatch-antigravity-argv-arg-max: Antigravity dispatch passes prompt on argv; very large prompts hit OS ARG_MAX
    Priority: Medium. Area: providers/antigravity
    Description: `internal/agentdispatch/adapters.go` `runAntigravity` passes the prompt as a single argv element after `--print`. The OS `execve(2)` cap (~128KB Linux, ~256KB macOS) means a multi-hundred-KB prompt fails at exec time with an opaque "argument list too long" error mapped to dispatch exit 70.
    Next step: Revisit when Antigravity's `agy --print` exposes a stdin or prompt-file path; until then, surface an explicit pre-flight length check with a typed `ExitUsage` error explaining the antigravity-specific limit.
    Notes: Claude and Codex are unaffected (both receive the prompt on stdin). Upstream `agy --help` on 2026-05-22 shows no stdin/prompt-file path; deferred pending upstream CLI support.

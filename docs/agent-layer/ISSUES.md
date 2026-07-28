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

- Issue 2026-07-27 benchmark-cancelled-session-cost: Pre-request cancellation invalidates an otherwise-scored treatment run
    Priority: High. Area: benchmarks / treatment cost accounting
    Description: A Koota treatment completed and scored 37/51, but normalization rejected it because one Codex child dispatch was explicitly cancelled before a request and its session had identity but no token-count event.
    Next step: Exclude only sessions proven by dispatch evidence to be caller-cancelled before any token-usage event, while retaining strict failure for ambiguous or incomplete billable sessions.
    Notes: The consolidated executor now preserves every successful cell and continues the remaining queue after failures; only the no-token cancelled-session accounting case remains.

- Issue 2026-07-24 plan-chain-drops-exact-signatures: Skills workflow loses exact API signatures from the task contract
    Priority: Critical. Area: benchmarks / skills workflow fidelity
    Description: The skills arm builds a different public API than the specification declares, so the graded tests cannot resolve the required symbols and the package fails to compile. Observed twice on onedump: first as `NewEncryptWriter(w, e)` instead of the declared method on `*Encryptor` (reproduced locally; adding one delegating line scored 0.976, the best of any arm), then on 2026-07-25 as `undefined: NewEncryptor` / `undefined: DecryptReader`. Bare and instructions-only have produced the declared API in every observation; that run they scored 0.854 and a build failure from unrelated internal code.
    Next step: Persisting the verbatim spec did NOT fix this - confirmed 2026-07-25 with `instructions.md` reaching every dispatched role. The plan artifact still paraphrases the declared API into prose, and the implementer follows the plan. Fix the plan chain itself: transcribe declared API surface and feature matrices verbatim into the plan artifact, add a conformance check against the spec before quality review, forbid narrowing explicit requirements as an autonomous blocker resolution, and add end-to-end skill tests for exact API and feature preservation.
    Notes: Supersedes the separate `plan-source-contract-loss` entry, which described the same defect generally (plans omitting exact public APIs and feature matrices; implementation proceeding with invented APIs or a self-selected subset while narrow self-authored tests pass). Skills wrote 26423 patch bytes vs bare's 13946 on the failing run and also redefined an unrelated test helper. Repro kept at `.agent-layer/tmp/onedump-repro`; run grader prepare with model.patch in `/logs/artifacts`, then `go vet -tags=encryption ./encryption/...`.

- Issue 2026-07-23 benchmark-provider-auth-preflight: Candidate authentication is not validated before a costly benchmark launch
    Priority: High. Area: benchmarks / provider setup
    Description: The twelve-task `opus-high-bare-calibration-20260722` run prepared every task, then all Claude candidates failed before inference because the copied OAuth session had expired; the current no-model helper preflight did not exercise Claude authentication.
    Next step: Add a provider-specific, non-billing credential-validity preflight (or fail the run before task setup when one is unavailable), preserve its evidence in the experiment report, and cover expiry behavior offline.
    Notes: Every rejected invocation recorded zero tokens and a complete provider-reported $0 cost; no task score is valid and no automatic rerun is authorized.

- Issue 2026-07-22 benchmark-treatment-dispatch-fidelity: Benchmark treatment can substitute native subagents for required external dispatches
    Priority: High. Area: benchmarks / treatment workflow
    Description: Claude Opus Low completed the configured plan-review, implementation, code-review, and fix workflow with five native Agent tool calls and zero `al dispatch` calls, despite those skills requiring external dispatch; the harness scores the result without flagging this protocol violation.
    Next step: Define and enforce a treatment-execution contract that records required role dispatches and marks a pair nonconformant when configured external roles are not actually dispatched, while retaining all candidate evidence.
    Notes: Confirmed in `opus-low-jsonpath-20260722`; `al` was installed and the Agent Dispatch skill was available, so this was not a provider failure.

- Issue 2026-07-22 release-dependency-refresh: Re-evaluate and upgrade release dependency pins
    Priority: Medium. Area: release automation and dependencies
    Description: The v0.14.0 release exposed an outdated `golang.org/x/text` vulnerability and GitHub Actions Node 20 deprecation annotations for pinned workflow actions; release-time dependencies need a current compatible-version review.
    Next step: Inventory Go modules and GitHub Actions pins, verify the latest compatible stable versions from their primary sources, update SHA-pinned workflow actions consistently in CI and release workflows, then run the full release gate.
    Notes: Preserve full commit-SHA action pins and record any necessary compatibility changes; do not rely on runner-forced Node 24 compatibility.

- Issue 2026-07-22 macos-homebrew-tap-trust-warning: macOS release runner emits an unrelated Homebrew tap-trust warning
    Priority: Low. Area: release automation/macOS runner
    Description: `brew install ripgrep` reports an untrusted pre-existing `aws/tap` during release setup even though the install and release succeed, creating misleading GitHub Action annotations.
    Next step: Reproduce on a macOS GitHub-hosted runner and remove the annotation by avoiding Homebrew for ripgrep or safely removing the unused tap before installation; retain Homebrew trust checks.
    Notes: Do not set `HOMEBREW_NO_REQUIRE_TAP_TRUST`; the warning is currently non-blocking and does not affect published artifacts.

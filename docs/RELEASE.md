# Release Process

Releases are designed to be fast, predictable, and tamper-resistant: a single `workflow_dispatch` workflow validates, builds, signs, scans, notarizes, tests both macOS and Linux binaries, builds and validates the website, and validates the Homebrew formula **before** creating an immutable annotated `vX.Y.Z` tag and publishing.

## Required version approval

Before changing any release-versioned file, dispatching a release workflow, or publishing a release, obtain the user's explicit approval of the exact `vX.Y.Z` version in the current conversation. A general request such as "release" authorizes assessment of release readiness only. It does not authorize choosing a version, and the operator must not infer major, minor, or patch from the changelog or commit history.

## Preconditions (local repo state)
- On `main` and up to date with `origin/main` before running `make release`.
- Clean working tree (`git status --porcelain` is empty).
- All release changes merged to `main` (including `CHANGELOG.md`, `site/docs/upgrades.mdx`, migration manifest, template ownership manifest, and any required Agent Dispatch compatibility evidence).

## Fast end-to-end release workflow

### 1. Prepare release metadata on a Release PR

1. **Migration manifest** — create `internal/templates/migrations/<version>.json` (version without leading `v`). Set `min_prior_version` to the release line supported by the target row in `site/docs/upgrades.mdx`; from v1.2.0 onward it must be at least `1.0.0` (`skillmigration.MinimumCLI`), the supported installed-project floor. Unknown-source upgrades receive source-agnostic operations only from manifests after `1.0.0`. Add any needed migration operations; use an empty `operations` array if all changes are additive. See existing manifests for the schema.
2. **Template ownership manifest** — generate from the working tree (no git tag required):
   ```bash
   ./scripts/generate-template-manifest.sh --tag "$VERSION"
   ```
3. **Changelog and upgrade docs** — finalize the `## vX.Y.Z - YYYY-MM-DD` section in `CHANGELOG.md` and add the matching migration-rules row in `site/docs/upgrades.mdx`.
4. **Live Agent Dispatch compatibility evidence (when Agent Dispatch changed)** — run the committed parallel probe script (~1 min):
   ```bash
   make release-probe
   ```
   Attach the generated evidence record under `docs/release-evidence/` to the Release PR. See [Agent Dispatch compatibility evidence](#agent-dispatch-compatibility-evidence) below for pass criteria and failure handling.
5. **Fast local preflight (~10–15s)** — validate upgrade docs/manifests, build unsigned four-platform release binaries + native smoke test, and scan all four binaries with `govulncheck` *before* opening or updating the Release PR:
   ```bash
   make release-tools
   make release-preflight RELEASE_TAG="$VERSION"
   ```
   `make release-preflight` fails fast on missing manifests, upgrade doc drift, build errors, or Go vulnerabilities without re-running the full local `make ci` suite (PR CI runs full `make ci` on both Linux and macOS, plus `govulncheck` on Linux, native Muse on both OSes, website build + asset checks, and benchmark companion contracts).
6. **Open the Release PR, address review feedback, and merge cleanly to `main`.**

### 2. Publish from clean, pushed `main` (~1.5–2 minutes)

Once the Release PR is merged to `main`:

```bash
VERSION="vX.Y.Z"

git checkout main
git fetch origin
git pull --ff-only origin main
git status --porcelain

# Single-run release: verifies CI, builds, signs, scans, notarizes, validates
# website and Homebrew formula, then creates the annotated tag and publishes.
make release AL_VERSION="$VERSION"
```

There is **no manual `git tag` or `git push origin "$VERSION"` step** and **no mandatory separate rehearsal wait**.

- **Zero post-merge CI wait on clean merges:** Both `make release` and the `release-readiness` job run `scripts/verify-release-ci.sh`, which accepts either:
  1. A successful `push`-event `ci.yml` run on `main` for the exact release commit, **or**
  2. A clean-merge exact-tree same-repo PR whose `pull_request` `ci.yml` run passed (verified from GitHub + local Git: merged PR targets `main` from `conn-castle/agent-layer`, never a fork; no other PR sharing that head SHA targets a non-`main` branch; `git tree(release_commit) == git tree(pr_head)`; and `release_commit^1` is an ancestor of `pr_head`, proving `main` did not advance past the PR).
  `main`'s `push` CI still runs unattended in the background to refresh default-branch caches and double-check merged commits. If another commit landed on `main` before the Release PR merged (causing merge skew), `scripts/verify-release-ci.sh` automatically waits for/requires `push` CI on `main`.
- **Single-run atomic publication:** `release.yml` runs `build-release` (macOS signing + parallel 4-target build + `govulncheck` + parallel Apple notarization) and `build-website` (cached `npm ci` + `publish-site` + Docusaurus build + local asset validation) **in parallel** (~1 min). Only after both pass does `publish` verify `dist/checksums.txt`, run a Linux native smoke test on `al-linux-amd64`, validate all three GitHub App tokens, and render + syntax/checksum-check `Formula/agent-layer.rb`. Only after **every** validation passes does it create the annotated `vX.Y.Z` tag, upload all 7 assets into a draft release and publish it (locking it under GitHub Immutable Releases), push the pre-built website commit, verify published asset URLs, and open the Homebrew tap PR.

### Optional signed dry-run rehearsal

Manual `workflow_dispatch` runs of `release.yml` default to `rehearsal=true`. Use a nonpublishing rehearsal when testing changes to `.github/workflows/release.yml`, signing/notarization scripts, or release tooling:

```bash
make release-rehearsal AL_VERSION="$VERSION"
# Equivalent compatibility alias:
make release-certify AL_VERSION="$VERSION"
```

Rehearsal executes the exact same `release-readiness`, `build-release` (signing, vulnerability scanning, notarization), `build-website`, Linux smoke test, App-token issuance, and Homebrew formula validation, then stops before creating the tag or performing any external write.

### Security model & recovery

- **No raw tag triggers:** `release.yml` is triggered strictly via `workflow_dispatch` (no `on: push: tags`), preventing accidental raw tag pushes and ensuring App-created tags never re-trigger the workflow.
- **Release Environment & actor authorization:** Every job runs in the GitHub `release` Environment restricted to `main` only (no UI click gate—releases stay 100% CLI-driven) and enforces fail-closed matching of `github.actor` against the Admin-only `RELEASE_ACTORS` Environment variable. Non-admin contributors with `write` access cannot dispatch releases or access `release` Environment secrets.
- **Split tag protection + Immutable Releases:** Existing `v*` tags are protected by an Admin-only ruleset (`update`, `deletion`, `non_fast_forward`) with no App bypass; a separate `v*` `creation` ruleset allows only Repo Admins and the release GitHub App (`conn-castle-release-bot`) to create new `v*` tags. GitHub Immutable Releases prevents replacing published release assets.
- **Never rebuild after a tag exists:** Because Apple Developer ID secure timestamps make macOS binaries non-reproducible across builds, `release-readiness` refuses to start a new build if `refs/tags/vX.Y.Z` already exists. If a transient GitHub outage interrupts `publish` after tag creation, recover by clicking **Re-run failed jobs** on that workflow run (`publish` reuses the already-uploaded signed `dist` artifact and resumes idempotently).

## Agent Dispatch compatibility evidence

For a release that changes Agent Dispatch, attach a short evidence record under `docs/release-evidence/` to the Release PR before merging. Record the exact `claude --version`, `codex --version`, `agy --version`, `grok --version`, and `muse --version` values, plus a fresh `start`/`wait` probe and a `continue`/`wait` probe for each locally enabled dispatch provider. List every declared supported provider in the evidence, marking disabled or unavailable providers as untested with the reason; do not enable them just for the probe.

Run the committed parallel probe script from the existing local release checkout using its `.agent-layer/config.toml`, `.agent-layer/.env`, and normal repo-local provider configuration and sign-ins:

```bash
make release-probe
```

`scripts/probe-release-dispatch.sh` builds a scratch candidate executable (`make al-dev-build AL_DEV_BIN_DIR=<scratch>`), records its SHA-256 and installed provider CLI versions, probes all locally enabled, available providers **in parallel** inside a clean probe environment (preserving configured models, reasoning efforts, and permissions without copying credentials or creating scratch projects), waits for `termination_confirmed` on every invocation, restores local projections via `make al-sync`, removes the scratch binary directory, validates all pass criteria (including Antigravity structured ACP session/usage evidence), and prints a ready-to-attach Markdown table.

Exit statuses:
- `0`: every available provider passed fresh + continuation probes; projections restored and scratch removed.
- `1`: build failed or one or more providers failed after confirmed termination (projections restored and scratch removed after termination confirmation).
- `20`: unsafe stop (CLI call failed, invalid response, no providers available, or termination unconfirmed). Scratch is retained; confirm termination of every started invocation, run `make al-sync`, and remove scratch manually.
- `30`: projection restoration (`make al-sync`) failed after all invocations terminated; fix restoration and rerun `make al-sync` before removing scratch.

Pass criteria (every provider):
- Fresh and continuation invocations both reach `completed` with `termination_confirmed = true`.
- Continuation reports the same `provider_session_id` as the fresh invocation.
- Each final answer contains its requested token (`release-probe-fresh` or `release-probe-continue`). Other text/punctuation/whitespace is not a failure; record verbatim.
- Authentication and quota failures are not passing evidence. Only an explicitly transient provider error (e.g., an OAuth refresh lock) may be retried once after its requested delay on a new handle, recording the failed attempt alongside the retry.
- Antigravity evidence must include structured ACP session/conversation ID and terminal `usage` in `events.jsonl`; plain provider output cannot substitute.
- If a later PR commit changes only documentation/manifests (no changes to `cmd/`, `internal/`, `go.mod`, or `go.sum`), existing probe evidence from the unchanged executable remains valid—record that `git diff --exit-code <probed-commit> HEAD -- cmd internal go.mod go.sum` is empty.

## Homebrew tap PR — automatic after successful validation

> **Do not merge, approve, label, or close the `conn-castle/homebrew-tap` PR manually. Eligible PRs merge automatically after successful validation.**

An open PR titled `agent-layer vX.Y.Z` in `conn-castle/homebrew-tap` immediately after a release is the **normal, healthy, expected** state. `brew test-bot` or the auto-merge workflow may still be running. Releasing is complete once `make release` succeeds; if both tap workflows complete without a merge, inspect the auto-merge run to identify which guard stopped it.

### What happens without you
1. `release.yml` opens the PR as `conn-castle-release-bot`, from branch `bump-agent-layer-vX.Y.Z`, changing only `Formula/agent-layer.rb`.
2. The tap's `brew test-bot` workflow validates the formula on macOS and Linux.
3. On success, the tap's `Auto merge binary formula bumps` workflow **squash-merges the PR and deletes the branch automatically.**

### Never do these
- **Do not merge the PR by hand.** Merging it yourself bypasses the `brew test-bot` gate.
- **Do not add the `pr-pull` label and do not run `brew pr-pull`.** The auto-merge workflow deliberately refuses any binary-formula PR carrying that label. `agent-layer` is a binary formula pointing at already-signed and notarized release assets, so the tap never builds or bottles it.

## Website publish details (agent-layer-web)

The `build-website` job runs in parallel with `build-release`, staging `site/` + `CHANGELOG.md` into `conn-castle/agent-layer-web` via `go run ./cmd/publish-site --tag vX.Y.Z --repo-b-dir agent-layer-web`, running `npm run build` and `python3 scripts/check-website-assets.py agent-layer-web/build`, and bundling the resulting commit for `publish` to push once the GitHub Release succeeds.

Retention policy in `versions.json`:
- keep the newest 4 patch releases from the newest minor line,
- keep the newest patch release for each of the newest 4 minor lines (including the newest minor line),
- keep stable releases only (prereleases are dropped),
- keep the union in newest-first order and prune dropped versions from `versioned_docs/` and `versioned_sidebars/`.

Website releases span two repositories: navbar config, theme components, CSS, branding assets, and global metadata belong to `agent-layer-web`. Routine CLI releases only need the automated Docusaurus build and asset validation (`website-build` in CI and `build-website` in `release.yml`); run manual browser/Playwright inspection only when website pages, layout, navigation, or styles actually changed.

CI runs the same website build check on every PR and push to `main`:

```bash
make website-build-check SITE_BUILD_TAG=v999.0.0 WEBSITE_REPO_DIR=agent-layer-web
```

Required secrets (stored in the `release` GitHub Environment, restricted to `main`):
- `HOMEBREW_TAP_APP_ID`, `HOMEBREW_TAP_PRIVATE_KEY` (`conn-castle-release-bot` — used for both `agent-layer` tag/release creation and `homebrew-tap` PRs; optional dedicated `RELEASE_APP_ID`/`RELEASE_APP_PRIVATE_KEY` override supported)
- `MACOS_CERT_P12_BASE64`, `MACOS_CERT_P12_PASSWORD`
- `MACOS_NOTARY_API_KEY_ID`, `MACOS_NOTARY_API_KEY_ISSUER_ID`, `MACOS_NOTARY_API_KEY_P8_BASE64`
- `AGENT_LAYER_WEB_APP_ID`, `AGENT_LAYER_WEB_APP_PRIVATE_KEY`

Required variable in the `release` GitHub Environment:
- `RELEASE_ACTORS` (comma-separated list of authorized GitHub usernames, Admin-only editable)

## Upgrade contract maintenance
- `site/docs/upgrades.mdx` is the canonical upgrade contract for event categories, compatibility guarantees, migration rules, and OS/shell support.
- For every release, update the migration-rules table in `site/docs/upgrades.mdx` for the target version (`vX.Y.Z`).
- If a release cannot fully satisfy the stated guarantees, document the limitation explicitly in the migration-rules row and in release notes.

## Post-release verification (fresh repo)
```bash
VERSION="vX.Y.Z"
tmp_dir="$(mktemp -d)"
cd "$tmp_dir"
curl -fsSL https://github.com/conn-castle/agent-layer/releases/latest/download/al-install.sh \
  | bash -s -- --version "$VERSION"
~/.local/bin/al --version
```

Expected: `al --version` prints `$VERSION`.

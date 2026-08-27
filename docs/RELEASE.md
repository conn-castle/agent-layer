# Release Process

Releases are designed to be predictable and verifiable: the same tag should always produce the same artifacts, checksums, and docs. This section documents the exact steps so the release pipeline remains auditable and repeatable.

## Required version approval

Before changing any release-versioned file, creating or pushing a release tag, dispatching a release workflow, or publishing a release, obtain the user's explicit approval of the exact `vX.Y.Z` version in the current conversation. A general request such as "release" authorizes assessment of release readiness only. It does not authorize choosing a version, and the operator must not infer major, minor, or patch from the changelog or commit history.

## Preconditions (local repo state)
- On `main` and up to date with `origin/main`.
- Clean working tree (`git status --porcelain` is empty).
- All release changes committed (including `CHANGELOG.md`).

## Release commands
```bash
VERSION="vX.Y.Z"

# Ensure main is current and clean
git checkout main
git fetch origin
git pull --ff-only origin main
git status --porcelain

# Tag and push
git tag -a "$VERSION" -m "$VERSION"
git push origin main
git push origin "$VERSION"

# Release assets are built by the GitHub Actions workflow.
```

Before tagging, prepare and commit both release manifests:

1. **Migration manifest** — create `internal/templates/migrations/<version>.json` (version without leading `v`). Set `min_prior_version` to the release line supported by the target row in `site/docs/upgrades.mdx`; for patch releases, preserve the previous target's supported range when unknown-source upgrades still need source-agnostic operations from that range. Add any needed migration operations; use an empty `operations` array if all changes are additive. See existing manifests for the schema.

2. **Template ownership manifest** — generate via the script below. The script reads templates directly from the working tree (no git tag required). This keeps `al upgrade plan` ownership inference deterministic without runtime network/tag lookups.

```bash
# 1. Create or verify the migration manifest (manual; see existing files for schema)
#    internal/templates/migrations/"${VERSION#v}".json

# 2. Generate the template ownership manifest (reads from working tree, no tag needed)
./scripts/generate-template-manifest.sh --tag "$VERSION"

# 3. Stage both manifests
git add internal/templates/migrations/"${VERSION#v}".json \
       internal/templates/manifests/"${VERSION#v}".json

# 4. Commit the manifests
git commit -m "release: add manifests for $VERSION"

# 5. Run release preflight to validate everything
make release-preflight RELEASE_TAG="$VERSION"
```

CI validates both manifests exist via `make docs-upgrade-check RELEASE_TAG=<tag>`. The release workflow will fail if either manifest is missing. Run `make release-preflight` locally before tagging to run CI, release-script checks, and upgrade-doc validation before publishing.

## Agent Dispatch compatibility evidence

For a release that changes Agent Dispatch, attach a short evidence record under
`docs/release-evidence/` to the release pull request before tagging. Record the
exact `claude --version`, `codex --version`, `agy --version`, and
`grok --version` values, plus a fresh `start`/`wait` probe and a
`continue`/`wait` probe for every declared supported provider. A changed or
missing Antigravity structured terminal result must fail without publishing
plain provider output. The result must carry a conversation ID, final answer,
and usage evidence; any diagnostic-log UUID that is present must match the
structured result. This is release evidence, not a new public probe command.

## GitHub release (automatic)
1. Tag push triggers the release workflow.
2. The workflow validates upgrade-contract docs for the tag (`make docs-upgrade-check RELEASE_TAG=<tag>`), ensuring a matching migration-table row exists, blocking placeholder migration text when changelog notes breaking/manual migration impact, verifying the migration manifest and template ownership manifest exist, and enforcing upgrade CTA syntax drift checks in core docs/message surfaces.
3. The workflow runs `make ci` on macOS before importing signing credentials, then imports the Developer ID certificate, builds release artifacts, signs the darwin binaries, writes checksums after signing, notarizes the darwin binaries, and publishes `al-install.sh`, macOS/Linux platform binaries, `agent-layer-<version>.tar.gz` (source tarball; version without leading `v`), and `checksums.txt`.
4. The workflow opens a PR against `conn-castle/homebrew-tap` to render `Formula/agent-layer.rb` as a binary formula using the published macOS/Linux release assets and their SHA256 values. **Eligible PRs merge automatically after successful validation — see [Homebrew tap PR](#homebrew-tap-pr--automatic-after-successful-validation). Do not merge it manually.**
5. The workflow publishes website content by pushing directly to `conn-castle/agent-layer-web` on `main`. This is mandatory; the release fails if `cmd/publish-site/main.go` or `site/` is missing, or if the published Docusaurus site does not build.
6. Release notes are automatically extracted from `CHANGELOG.md` by the workflow.

Once this workflow succeeds, the release is done. The tap PR normally completes automatically after its own validation; if it remains open after validation and auto-merge have finished, inspect the auto-merge guards. The only remaining routine step is the optional [post-release verification](#post-release-verification-fresh-repo) below.

## Homebrew tap PR — automatic after successful validation

> **Do not merge, approve, label, or close the `conn-castle/homebrew-tap` PR manually. Eligible PRs merge automatically after successful validation.**

An open PR titled `agent-layer vX.Y.Z` in `conn-castle/homebrew-tap` immediately after a release is the **normal, healthy, expected** state. `brew test-bot` or the auto-merge workflow may still be running. Releasing is complete once the release workflow succeeds; if both tap workflows complete without a merge, inspect the auto-merge run to identify which guard stopped it.

Every `agent-layer` tap PR to date has been merged by the `conn-castle-release-bot` app, typically 2–25 minutes after it was opened.

### What happens without you
1. This repo's release workflow opens the PR as `conn-castle-release-bot`, from branch `bump-agent-layer-vX.Y.Z`, changing only `Formula/agent-layer.rb`.
2. The tap's `brew test-bot` workflow validates the formula on macOS and Linux.
3. On success, the tap's `Auto merge binary formula bumps` workflow (`.github/workflows/auto-merge-binary-formula.yml` in the tap repo) **squash-merges the PR and attempts to delete the branch automatically.**

There is no manual approval gate anywhere in that chain.

### Never do these
- **Do not merge the PR by hand.** Merging it yourself bypasses the `brew test-bot` gate.
- **Do not add the `pr-pull` label and do not run `brew pr-pull`.** The auto-merge workflow deliberately *refuses to merge* any binary-formula PR carrying that label, so adding it converts a self-completing release into a stuck one. `pr-pull` is only for formulae that produce Homebrew bottle artifacts; `agent-layer` is a binary formula that points at this repo's already-signed and notarized release assets, so the tap never builds, signs, notarizes, or bottles it.

**If it really is stuck:** when both `brew test-bot` and auto-merge have completed but the PR remains open, read the tap's `Auto merge binary formula bumps` run to see which guard stopped it, and fix that cause rather than merging by hand.

## Website publish details (agent-layer-web)
The `publish-website-and-tap` job publishes website content by running `go run ./cmd/publish-site --tag vX.Y.Z --repo-b-dir agent-layer-web`, then runs `npm run build` in `agent-layer-web`.
Release publishing currently supports stable tags only (`vX.Y.Z`); prerelease tags are intentionally unsupported.
That command:
1. Copies `site/pages/` into `agent-layer-web/src/pages/`, deleting the destination first.
2. Copies `site/docs/` into `agent-layer-web/docs/`, deleting the destination first.
3. Overwrites `agent-layer-web/CHANGELOG.md` with this repo’s `CHANGELOG.md`.
4. Removes any existing versioned docs for this tag, then runs `npx docusaurus docs:version X.Y.Z` to snapshot the docs into `versioned_docs/version-X.Y.Z/` and `versioned_sidebars/version-X.Y.Z-sidebars.json`.
5. Rewrites `versions.json` (dedupe + newest-first sort), then applies retention:
   - keep the newest 4 patch releases from the newest minor line,
   - keep the newest patch release for each of the newest 4 minor lines (including the newest minor line),
   - keep stable releases only (prereleases are dropped),
   - keep the union of those sets in newest-first order.
6. Prunes dropped versions from both `versioned_docs/version-<version>/` and `versioned_sidebars/version-<version>-sidebars.json`.

Historical docs are retained by the policy above. The current tag is always removed/recreated first for idempotency before retention is applied.

After publishing, the workflow runs the Docusaurus production build before committing and pushing the website changes.

CI also runs the same website build shape on pull requests and pushes to `main` with a synthetic docs tag:

```bash
make website-build-check SITE_BUILD_TAG=v0.0.0 WEBSITE_REPO_DIR=agent-layer-web
```

Required secrets for the tap PR:
- `HOMEBREW_TAP_APP_ID`
- `HOMEBREW_TAP_PRIVATE_KEY`

Required secrets for darwin signing and notarization:
- `MACOS_CERT_P12_BASE64`
- `MACOS_CERT_P12_PASSWORD`
- `MACOS_NOTARY_API_KEY_ID`
- `MACOS_NOTARY_API_KEY_ISSUER_ID`
- `MACOS_NOTARY_API_KEY_P8_BASE64`

Required secrets for the website publish:
- `AGENT_LAYER_WEB_APP_ID`
- `AGENT_LAYER_WEB_APP_PRIVATE_KEY`

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

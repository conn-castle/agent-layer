# Release Process

Releases are designed to be predictable and verifiable: the same tag should always produce the same artifacts, checksums, and docs. This section documents the exact steps so the release pipeline remains auditable and repeatable.

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

1. **Migration manifest** — create `internal/templates/migrations/<version>.json` (version without leading `v`). Set `min_prior_version` to the previous release line (N-1 to N per the compatibility guarantee in `site/docs/upgrades.mdx`). Add any needed migration operations; use an empty `operations` array if all changes are additive. See existing manifests for the schema.

2. **Template ownership manifest** — generate via the script below. This keeps `al upgrade plan` ownership inference deterministic without runtime network/tag lookups.

```bash
# 1. Create or verify the migration manifest (manual; see existing files for schema)
#    internal/templates/migrations/"${VERSION#v}".json

# 2. Generate the template ownership manifest
./scripts/generate-template-manifest.sh --tag "$VERSION"

# 3. Stage both manifests
git add internal/templates/migrations/"${VERSION#v}".json \
       internal/templates/manifests/"${VERSION#v}".json
```

CI validates both manifests exist via `make docs-upgrade-check RELEASE_TAG=<tag>`. The release workflow will fail if either manifest is missing.

## GitHub release (automatic)
1. Tag push triggers the release workflow.
2. The workflow validates upgrade-contract docs for the tag (`make docs-upgrade-check RELEASE_TAG=<tag>`), ensuring a matching migration-table row exists, blocking placeholder migration text when changelog notes breaking/manual migration impact, verifying the migration manifest and template ownership manifest exist, and enforcing upgrade CTA syntax drift checks in core docs/message surfaces.
3. The workflow publishes `al-install.sh`, macOS/Linux platform binaries, `agent-layer-<version>.tar.gz` (source tarball; version without leading `v`), and `checksums.txt`.
4. The workflow opens a PR against `conn-castle/homebrew-tap` to update `Formula/agent-layer.rb` with the new tarball URL + SHA256.
5. The workflow publishes website content by pushing directly to `conn-castle/agent-layer-web` on `main`. This is mandatory; the release fails if `cmd/publish-site/main.go` or `site/` is missing.
6. Release notes are automatically extracted from `CHANGELOG.md` by the workflow.

## Website publish details (agent-layer-web)
The `publish-website-and-tap` job publishes website content by running `go run ./cmd/publish-site --tag vX.Y.Z --repo-b-dir agent-layer-web`.
That command:
1. Copies `site/pages/` into `agent-layer-web/src/pages/`, deleting the destination first.
2. Copies `site/docs/` into `agent-layer-web/docs/`, deleting the destination first.
3. Overwrites `agent-layer-web/CHANGELOG.md` with this repo’s `CHANGELOG.md`.
4. Removes any existing versioned docs for this tag, then runs `npx docusaurus docs:version X.Y.Z` to snapshot the docs into `versioned_docs/version-X.Y.Z/` and `versioned_sidebars/version-X.Y.Z-sidebars.json`.
5. Rewrites `versions.json` (dedupe + newest-first sort).

Historical docs are preserved because each release snapshots a new `versioned_docs/version-X.Y.Z/` directory. Only the directory for the current tag is removed/recreated for idempotency.

Required secrets for the tap PR:
- `HOMEBREW_TAP_APP_ID`
- `HOMEBREW_TAP_PRIVATE_KEY`

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

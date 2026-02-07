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

## GitHub release (automatic)
1. Tag push triggers the release workflow.
2. The workflow publishes `al-install.sh`, macOS/Linux platform binaries, `agent-layer-<version>.tar.gz` (source tarball; version without leading `v`), and `checksums.txt`.
3. The workflow opens a PR against `conn-castle/homebrew-tap` to update `Formula/agent-layer.rb` with the new tarball URL + SHA256.
4. The workflow publishes website content by pushing directly to `conn-castle/agent-layer-web` on `main`. This is mandatory; the release fails if `cmd/publish-site/main.go` or `site/` is missing.
5. Release notes are automatically extracted from `CHANGELOG.md` by the workflow.

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

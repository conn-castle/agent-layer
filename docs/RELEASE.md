# Release Process

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
2. The workflow publishes `al-install.sh`, `al-install.ps1`, platform binaries, `agent-layer-<version>.tar.gz` (source tarball; version without leading `v`), and `checksums.txt`.
3. Release notes are automatically extracted from `CHANGELOG.md` by the workflow.

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

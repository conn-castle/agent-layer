# Development

This repo is built around determinism: the same inputs should produce the same client outputs locally and in CI. These steps keep tooling pinned, workflows repeatable, and changes reviewable so you can trust what ships.

## Prerequisites
- Go 1.26.0+
- Git
- Make (via Xcode Command Line Tools on macOS)
- Recommended: `pre-commit` for local hooks

## macOS quick start (fresh machine)
1. Install Xcode Command Line Tools (includes Git):
   ```bash
   xcode-select --install
   ```
2. Install Go 1.26.0+ (from https://go.dev/dl/) and confirm it works:
   ```bash
   go version
   ```
3. Install `pre-commit` (recommended) and confirm it works:
   ```bash
   pre-commit --version
   ```

## One-time setup (per clone)
```bash
./scripts/setup.sh
```

## Daily workflow
- Use the commands in `docs/agent-layer/COMMANDS.md` for format, lint, test, coverage, and release builds.
- Prefer `make` targets (see `docs/agent-layer/COMMANDS.md`) instead of running `goimports` / `golangci-lint` directly; tools are installed repo-locally under `.tools/bin` so you do not need to edit your shell PATH. This avoids “works on my machine” drift and keeps local output aligned with CI.
- Use `make dev` for a quick local pass (format + fmt-check + lint + coverage + release tests). Run `./scripts/setup.sh` or `make tools` first.
- **Template sources live in `internal/templates/`**, not in `.agent-layer/`. The `.agent-layer/` directory is the *install output* created by `al init`/`al upgrade` in target repos. When adding or editing templates (instructions, memory files, skills, config), always edit the source in `internal/templates/`. If you change installer templates, run `al upgrade` in a target repo to apply the updated templates. When testing from this source repo's scratch target (`tmp/dev-repo`), use `go run ../../cmd/al upgrade` (or `go run ../../cmd/al init` for a fresh repo).
- If template-managed file semantics change for release upgrades, regenerate the release manifest: `./scripts/generate-template-manifest.sh --tag vX.Y.Z`.
- If you change upgrade behavior or upgrade-facing guidance, update the canonical upgrade contract page at `site/docs/upgrades.mdx` and keep release notes/docs links aligned.
- If you change VS Code launch behavior, update `docs/architecture/vscode-launch.md` and keep troubleshooting guidance aligned.

## Go Tooling & Environment
Agent Layer uses several light shell wrappers and `make` targets around standard Go commands (`go fmt`, `go test`, etc.). This is intentional to ensure consistent behavior across local development and CI (GitHub Actions). It also keeps tool versions explicit, which makes regressions easier to trace.

### Common Issues
- **`go mod tidy` or `go run` fails with network errors**: If your environment restricts access to `proxy.golang.org`, you can try setting `GOPROXY=direct` or ensure you have a working internet connection.
- **Permission errors on Go cache**: If you see errors related to `GOCACHE` or `GOMODCACHE`, you can override them to a local directory:
  ```bash
  export GOCACHE=$PWD/tmp/gocache
  export GOMODCACHE=$PWD/tmp/gomodcache
  go mod tidy
  ```
- **Tools not found**: If `make lint` fails because `golangci-lint` is missing, run `make tools` to install all pinned dependencies into `.tools/bin`.

## Run the CLI locally (always uses latest changes)
Run from source (`go run`):
```bash
# One-time init for a fresh repo (creates bare .agent-layer/ operational scaffold)
go run ./cmd/al init

# Generate outputs (optional; client commands already sync on run)
go run ./cmd/al sync

# Launch a client (always runs sync first)
go run ./cmd/al agy
```

Optional: build a local dev binary from the current source (no global install):
```bash
go build -o ./tmp/al ./cmd/al
./tmp/al init
./tmp/al agy
```

### Run against a scratch repo (recommended for install/sync testing)
```bash
mkdir -p tmp/dev-repo
cd tmp/dev-repo
go run ../../cmd/al init
go run ../../cmd/al agy
```

Notes:
- `init` is required once per repo to seed the bare `.agent-layer/` operational scaffold.
- `init` prompts to run the setup wizard by default; pass `--no-wizard` to skip (non-interactive shells skip automatically). The wizard can install the workflow bundle, including `docs/agent-layer/` memory files/templates and instruction/skill templates.
- `sync` is optional because `al <client>` always syncs before launch.
- `./scripts/setup.sh` is only for tool + hook setup, not required just to run the CLI.

## Hidden maintenance commands
These are registered but hidden from `al --help`. They are maintenance aids, not
part of the supported CLI surface, and are not documented on the website.

### `al organize-scratch` — sort a scratch directory for review
Agent scratch directories accumulate hundreds of reports, logs, checkouts, and
dependency trees. This command sorts the top level of one into folders that
encode **how much review each entry needs**, so a later cleanup pass can skip
whole folders instead of judging every entry:

| Destination | Meaning |
| --- | --- |
| `reports/<prefix>/` | Matched a recurring report-family naming convention. No review. |
| `reports/adhoc/<topic>/` | Ad-hoc Markdown, grouped by topic (`pr`, `incidents`, `reviews`, `plans-specs`, `misc`). No review. |
| `artifacts/<kind>/` | Routed purely by file extension (`logs`, `screenshots`, `diffs`, `scripts`, `data`, `evidence`). No review. |
| `review/…` | Needs a human decision, subdivided by the reason: `checkouts`, `regenerable`, `unique-assets`, `bulk-samples`, `secrets`, `unknown`. |

```bash
# Dry run: print the plan, move nothing (default)
al organize-scratch --root .agent-layer/tmp

# Perform the moves
al organize-scratch --root .agent-layer/tmp --apply
```

| Flag | Purpose |
| --- | --- |
| `--root <dir>` | Directory to organize. Required — the command never picks one itself. |
| `--apply` | Perform the moves. Without it the run is a dry run. |
| `--keep <a,b>` | Top-level names to leave in place, for tool-managed paths other software resolves on its own. Repeatable, and accepts comma-separated values. |
| `--move-worktrees` | Also relocate registered Git worktrees, repairing their registration afterwards. |
| `--min-group <n>` | How many entries a filename prefix needs before it earns its own `reports/<prefix>` folder (default 5). |

Guarantees worth knowing before running it with `--apply`:
- **Nothing is ever deleted, overwritten, or merged.** Entries are only moved. An
  entry whose destination path is already taken is reported as a collision and
  left where it is. Every removal decision stays with you.
- Any run that has something to organize writes `ORGANIZE-REVIEW.md` into the
  root — including a dry run — listing what needs a decision, what to check
  before removing it, and anything left in place. A run that finds only reserved
  or kept entries leaves an earlier review list untouched rather than replacing
  a still-outstanding decision list with "nothing to do".
- Entries are moved one at a time, and the destination check is not atomic with
  the move. The no-overwrite guarantee holds against existing files and earlier
  runs, not against another process writing into the scratch root concurrently.
- Registered Git worktrees, and any directory containing one, are left in place
  unless `--move-worktrees` is passed, because relocating one rewrites Git's
  worktree registration. With the flag, each real worktree path is repaired
  after the move (repairing only a moved parent would leave nested worktrees
  registered at their old location as prunable).
- Value is judged by **reproducibility** — whether a tree is a copy of the repo,
  a dependency install, or a build cache — not by whether prose sits next to it.
  A directory of authored assets that exist nowhere else lands in
  `review/unique-assets`, never in a "probably disposable" bucket.
- Copy, asset, and worktree detection all depend on Git. When the root is not
  inside a repository, or `git ls-files` reports nothing, the command says so on
  stderr rather than silently classifying with those checks switched off.

## Run checks locally
```bash
# Quick pass (format + fmt-check + lint + coverage + release tests)
make dev

# Targeted checks (optional)
make test
make lint
make coverage
```
Notes:
- `make dev` includes coverage and `test-release`; use `make test` when you want a faster loop without the coverage gate and release checks.
- `make test` uses `gotestsum` for more readable output (installed via `make tools`).
- `make lint` and `make test` fail fast if tools are missing; run `make tools` once per clone.

## Full local verification (closest CI parity)
```bash
make ci
make release-dist AL_VERSION=ci DIST_DIR=dist
```
Note: `make ci` includes `make tidy-check`, which fails if `go.mod` or `go.sum` would change. While you are actively editing dependencies, use `make test`, `make lint`, and `make coverage` instead. `make ci` expects tools to be installed via `make tools`. The `make release-dist ...` command mirrors CI's dry-run release artifact build.

## Managing bundled instructions

The bundled instruction set is `internal/templates/instructions/00_rules.md` (rules, escalation, and communication style) and `01_memory.md` (the project memory files and how to use them). Both are **managed**: the workflow bundle seeds them when missing, and `al upgrade` updates them, prompting before overwriting local edits. Anything else a user drops into `.agent-layer/instructions/` is their own file and is never touched.

Users tailor instructions by editing these files or adding their own; there is no separate user-owned instruction template.

### Changing a bundled instruction
Edit the template under `internal/templates/instructions/`. New installs pick the change up directly, and existing installs receive it through the normal `al upgrade` managed-file flow.

### Appending to a file that upgrade must not overwrite
Use an `append_to_file` migration when content must reach an existing install without clobbering what is already there — for example adding one rule to a file a user may have edited:
```json
{
  "id": "add_<rule_name>",
  "kind": "append_to_file",
  "rationale": "Add <rule description>",
  "source_agnostic": true,
  "path": ".agent-layer/instructions/00_rules.md",
  "value": "\"- **Rule name:** Rule text.\\n\"",
  "from": "**Rule name:**"
}
```
- `path`: the file to append to (relative to repo root).
- `value`: JSON-encoded string content to append.
- `from`: duplicate-detection match string. If this string is already present in the file, the migration is a no-op (no-op migrations are not shown in the upgrade output). When the path has a bundled template and the file is missing, the full template is seeded as the base so the result is never a partial stub.

During `al upgrade` the migration appends the content and reports it in the upgrade output when it actually applies.

### Renaming or removing a bundled instruction
Template renames and removals do not reach existing installs on their own — an orphaned file keeps being loaded as instructions. Pair the template change with `rename_file` / `delete_file` operations in the next version's migration manifest, as `0.16.0` does for the consolidation into `00_rules.md` and `01_memory.md`. Do not delete files that hold user-authored content.

## Troubleshooting
- If you see `golangci-lint: command not found` or `goimports: command not found`, run:
  ```bash
  make tools
  ```

- If `pre-commit install` fails with:
  - `[ERROR] Cowardly refusing to install hooks with core.hooksPath set.`

  Unset it for this repo and retry:
  ```bash
  git config --show-origin --get core.hooksPath
  git config --unset-all core.hooksPath
  # If it was set globally instead:
  # git config --global --unset-all core.hooksPath
  ./scripts/setup.sh
  ```

- If `go mod download` fails due to proxy access, ensure your network can reach `proxy.golang.org` or set `GOPROXY=direct` temporarily.
- If Go reports a cache permission error, the scripts use a repo-local cache by default. To set it manually:
  ```bash
  GOCACHE=.cache/go-build GOMODCACHE=.cache/go-mod go mod tidy
  ```

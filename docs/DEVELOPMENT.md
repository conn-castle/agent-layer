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
- Use `make dev` for the fast local formatting and lint loop. Run `./scripts/setup.sh` or `make tools` first. Use `make test` or `make coverage` for the global test and coverage gates, and `make ci` as the complete local/pre-PR verification command (hosted CI runs the same target).
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
dependency trees. This command sorts only the top level into folders that encode
**how much review each entry needs**. It is the canonical scratch organizer; it
never deletes, overwrites, merges, or rewrites links. Deletion is a later human
decision.

| Destination | Meaning |
| --- | --- |
| `reports/<prefix>/` | Matched a recurring report-family naming convention. No review. |
| `reports/adhoc/<topic>/` | Ad-hoc Markdown, grouped by topic (`pr`, `incidents`, `reviews`, `plans-specs`, `misc`). No review. |
| `artifacts/<kind>/` | Routed purely by file extension (`logs`, `screenshots`, `diffs`, `scripts`, `data`, `evidence`). No review. |
| `review/…` | Needs a human decision: `checkouts`, `regenerable`, `unique-assets`, `bulk-samples`, `oversized`, `symlinks`, `secrets`, or `unknown`. |

```bash
# Strictly read-only preview (default)
al organize-scratch --root .agent-layer/tmp

# Move entries and write the actual outcome document
al organize-scratch --root .agent-layer/tmp --apply
```

| Flag | Purpose |
| --- | --- |
| `--root <dir>` | Directory to organize. Required — the command never picks one itself. |
| `--apply` | Perform moves and write `ORGANIZE-REVIEW.md`. Without it the run writes nothing. |
| `--keep <a,b>` | Top-level names to leave in place, for tool-managed paths other software resolves on its own. Repeatable, accepts comma-separated values, and rejects path separators. |
| `--move-worktrees` | Also relocate registered Git worktrees, repairing their registration afterwards. |
| `--min-group <n>` | How many entries a filename prefix needs before it earns its own `reports/<prefix>` folder (default 5). |

Safety behavior:

- **Nothing is ever deleted, overwritten, or merged.** Entries are only moved. An
  entry whose destination path is already taken is reported as a collision and
  left where it is. Predicted dry-run collisions and actual apply collisions
  both make the run non-zero, so automation must not interpret a collision-bearing
  preview as a successful safety check.
- A dry run is strictly read-only: it creates no destination directories or
  review document, moves and repairs nothing, predicts collisions with `Lstat`
  (including dangling destination links), and prints the complete proposed
  review list to stdout.
- Apply writes `ORGANIZE-REVIEW.md` from actual outcomes. It preserves entries
  still present under `review/`, carries forward safely parseable earlier
  reasons, and explicitly distinguishes moved, collided, failed, and unattempted
  entries after a partial failure. Unexpected move/filesystem failures stop the
  mutation sequence immediately and return non-zero.
- `reports`, `artifacts`, `review`, `ORGANIZE-REVIEW.md`, `.git`, `.gitignore`,
  `README.md`, `.DS_Store`, and names supplied with `--keep` are never candidates.
- Entries are moved one at a time, and the destination check is not atomic with
  the move. The no-overwrite guarantee holds against existing files and earlier
  runs, not against another process writing into the scratch root concurrently.

Classification is deliberately conservative:

- The metadata/hazard walk is complete. It does not descend into `.git`
  metadata, but every nested `.git` directory or file is recorded. Filename and
  extension samples remain bounded only for JS-ratio, repo-copy, and bulk-sample
  heuristics; any sampled verdict says so.
- Credential candidates go to `review/secrets`: PEM private keys within the
  first 1 MiB of a candidate; bounded base64 content that decodes to a PEM
  private-key header (a base64 public certificate alone does not qualify);
  non-empty assignments in `.env`, `.env.*`, and `*.env` files; JSON with a
  top-level cookies array; and browser profiles containing `Cookies` with
  `Login Data` and/or `Local State`. `.env.example` is included, and a non-empty
  placeholder still requires review even when it is not a live credential.
  Candidate paths are matched broadly around key/secret/token/credential/JWT/
  password/private, auth/session, and environment-file names. A cleanly read
  1 MiB prefix with no signal keeps its normal classification and records a
  bounded-inspection disclosure; content after that prefix is not inspected.
  Content under wholly innocuous names is not probed, so a key there and a
  credential signal appearing only after the bounded prefix remain residual
  limitations.
- Authored source assets outrank statistical dependency/repo-copy heuristics,
  except that explicitly named top-level dependency/cache trees remain
  `review/regenerable`. Assets below nested dependency/cache directories do not
  count as authored evidence.
- Every top-level directory over 100 files or 250 MiB apparent size requires
  review. An immediate child over either limit forces its parent into review and
  is named in the reason. Top-level files over 250 MiB also go to
  `review/oversized`. Existing useful review categories are preserved and gain
  the size evidence; incomplete inspection also forces review.
- Every top-level symlink goes to `review/symlinks`, including dangling links.
  Links inside entries are compared against the complete move plan. Relative
  links whose relocation changes an outside resolution, and relative or
  absolute links whose in-tree target moves elsewhere, are reported. Links are
  never repaired; valid relative links that move with their target as one unit
  are not reported. Caller-provided `--keep` entries and stable control files
  are also inspected as stationary link owners, so a kept link into an entry
  that moves is disclosed in dry-run output and the applied review document.
  Existing destination trees (`reports`, `artifacts`, and `review`) and Git
  metadata are not reinterpreted as kept input.

Git and worktree protection:

- Roots outside a Git repository are supported. An untracked, non-ignored
  directory inside a repository is also allowed. Repository roots are always
  refused, including empty or unborn repositories. If Git tracks any content
  at or below another requested root, the command refuses that tracked subtree.
- Git runs with `LC_ALL=C`. Only Git's genuine “not a git repository” result
  disables repository-backed facts; a missing/broken executable, dubious
  ownership, or failures of `ls-files`/`worktree list` are fatal.
- Registered Git main or linked worktrees, and any directory containing one, are left in place
  unless `--move-worktrees` is passed, because relocating one rewrites Git's
  registration. A moved entry that is itself registered and contains nested
  registered worktrees repairs both target sets. Foreign main checkouts are
  detected from their `.git` directories and repair every linked registration
  from the moved main checkout, including registrations outside the scratch.
  Standalone foreign linked worktrees are detected from their `.git` files and
  repaired from the moved linked checkout, not through an unrelated scratch-root
  repository. Every worktree that moved
  is repaired even if a later entry move fails. A failed repair or a moved
  target registration that is missing/prunable after repair makes the run
  non-zero with the exact repair context; unrelated prunable registrations that
  predated the run do not make this operation fail.

## Run checks locally
```bash
# Fast formatting and lint loop
make dev

# Targeted or global tests and coverage
make test
make coverage
```
Notes:
- `make dev` formats Go source and runs golangci-lint. It does not run tests, coverage, or the full CI suite.
- Use `make test` for the global test suite and `make coverage` to enforce the coverage gate.
- `make test` uses `gotestsum` for more readable output (installed via `make tools`).
- `make lint` and `make test` fail fast if tools are missing; run `make tools` once per clone.

## Full local verification (closest CI parity)
```bash
make ci
make release-dist AL_VERSION=ci DIST_DIR=dist
```
Note: `make ci` is the complete local/pre-PR verification gate and the same target hosted CI runs. It includes `make tidy-check`, which fails if `go.mod` or `go.sum` would change. While you are actively editing dependencies, use `make test`, `make lint`, and `make coverage` instead. `make ci` expects tools to be installed via `make tools`. The `make release-dist ...` command mirrors CI's dry-run release artifact build.

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
  "value": "- **Rule name:** Rule text.\n",
  "from": "**Rule name:**"
}
```
- `path`: the file to append to (relative to repo root).
- `value`: the content to append, as an ordinary JSON string. It is decoded once, so `\n` is a line ending — do not double-escape it.
- `from`: duplicate-detection match string. If this string is already present in the file, the migration is a no-op (no-op migrations are not shown in the upgrade output). When the path has a bundled template and the file is missing, the full template is seeded as the base so the result is never a partial stub.

During `al upgrade` the migration appends the content and reports it in the upgrade output when it actually applies.

### Renaming or removing a bundled instruction
Template renames and removals do not reach existing installs on their own, and an orphaned instruction file keeps being loaded every session.

For a **rename**, add a `rename_file` operation to the next version's migration manifest, as `0.16.0` does for `02_memory.md` → `01_memory.md`. The file carries its content forward and stays on the managed overwrite-prompt path, so local edits are preserved or prompted rather than lost.

For a **removal**, do not add a `delete_file` operation. Instruction fragments are documented as user-editable, and `delete_file` runs unconditionally ahead of the overwrite prompts, so it would silently discard project-specific rules a user added to the file. `al upgrade` already reports a file whose template is gone under template removals/orphans; deleting it is the user's call. `0.16.0` takes this route for `01_base.md` and `03_tools.md` after folding their content into `00_rules.md`.

Reserve `delete_file` for artifacts a user cannot meaningfully have edited.

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

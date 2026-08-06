# Commands

Note: This is an agent-layer memory file. It is primarily for agent use.

## Purpose
Canonical, repeatable **development workflow** commands for this repository (setup, build, run, test, coverage, lint/format, typecheck, migrations, scripts). This file is not for application/CLI usage documentation.

## Format
- Prefer commands that are stable and will be used repeatedly. Avoid one-off debugging commands.
- Organize commands using headings that fit the repo. Create headings as needed.
- If the repo is a monorepo, group commands per workspace/package/service and specify the working directory.
- When commands change, update this file and remove stale entries.
- Insert entries (and any needed headings) below `<!-- ENTRIES START -->`.

### Entry template
````text
- <Short purpose>
```bash
<command>
```
Run from: <repo root or path>
Prerequisites: <only if critical>
Notes: <optional constraints or tips>
````

<!-- ENTRIES START -->

### Setup

- Setup a fresh clone (installs pinned tools + pre-commit hooks)
```bash
./scripts/setup.sh
```
Run from: repo root  
Prerequisites: Go 1.26.0+, Make  
Notes: Installs tools into `.tools/bin`. Go package tools are pinned in `go.mod`; golangci-lint is pinned separately in `Makefile` so its dependencies cannot change the application module graph.

- Install pinned Go tooling (goimports, golangci-lint, gotestsum, deadcode) only
```bash
make tools
```
Run from: repo root  
Prerequisites: Go 1.26.0+, Make  
Notes: Uses versions pinned in `go.mod`. Installs tools into `.tools/bin`.

- Install pre-commit hooks
```bash
pre-commit install --install-hooks
```
Run from: repo root  
Prerequisites: `pre-commit` installed

- Run pre-commit on all files
```bash
pre-commit run --all-files
```
Run from: repo root  
Prerequisites: `pre-commit` installed

### Format

- Format Go code (gofmt + goimports)
```bash
make fmt
```
Run from: repo root  
Prerequisites: `make tools` has been run  
Notes: Applies formatting in place.

- Check formatting (CI/local)
```bash
make fmt-check
```
Run from: repo root  
Prerequisites: `make tools` has been run  
Notes: Fails if any files need formatting.

### Lint

- Run golangci-lint
```bash
make lint
```
Run from: repo root  
Prerequisites: `make tools` has been run

- Run complementary Linux-targeted and native-host golangci-lint
```bash
make lint-ci-local
```
Run from: repo root
Prerequisites: `make tools` has been run; network access may be needed for a fresh module download
Notes: Uses disposable `GOCACHE`, `GOMODCACHE`, and `GOLANGCI_LINT_CACHE` for both a `GOOS=linux GOARCH=amd64 CGO_ENABLED=0` pass and a native-host pass. The Linux-targeted pass preserves cross-target coverage; the native pass catches host package-loading differences, including test-file contributions to occurrence-counting linters. Together they improve local detection but do not reproduce the complete Ubuntu runner image, tools, or environment.

- Run dead code analysis across all packages (test-aware default)
```bash
make dead-code
```
Run from: repo root  
Prerequisites: `make tools` has been run
Notes: Runs `deadcode -test ./...` for high-signal results that include package test executables.

- Run entrypoint-focused dead code analysis (higher noise, deeper audit)
```bash
make dead-code-entrypoints
```
Run from: repo root  
Prerequisites: `make tools` has been run
Notes: Runs `deadcode -test` from `./cmd/al` and `./cmd/publish-site` roots; useful when auditing CLI-reachability specifically.

### Test

- Run all tests
```bash
make test
```
Run from: repo root
Prerequisites: `make tools` has been run
Notes: Uses `gotestsum` for nicer output.

- Run e2e harness self-tests (auth, helpers)
```bash
make test-e2e-harness
```
Run from: repo root
Prerequisites: none
Notes: Validates harness infrastructure (token auth, helpers) without running full e2e scenarios.

- Run race detector on concurrency-critical packages
```bash
make test-race
```
Run from: repo root
Prerequisites: Go 1.26.0+
Notes: Covers `internal/agentdispatch`, `internal/sync`, `internal/install`, `internal/warnings`, `internal/projectlock`, and `internal/skillimport` — every package that participates in the shared project lock that serializes projection with skill import mutations.

- Run scenario-based end-to-end tests (offline, hermetic)
```bash
make test-e2e
```
Run from: repo root
Prerequisites: Go 1.26.0+, `sha256sum` or `shasum`
Notes: Builds release artifacts and runs all discovered scenarios with mock agent binaries. Auto-detects latest migration manifest version for upgrade testing. Upgrade scenarios use pre-cached binaries from `~/.cache/al-e2e/bin/` (run `make test-e2e-online` once to populate cache). Override version with `AL_E2E_VERSION=vX.Y.Z`. Filter: `AL_E2E_SCENARIOS="upgrade*" make test-e2e`. `defaults.toml` profile fixture is generated at runtime from `internal/templates/config.toml` to prevent drift.

- Run e2e tests with online upgrade binary downloads
```bash
make test-e2e-online
```
Run from: repo root
Prerequisites: Go 1.26.0+, `curl`, `sha256sum` or `shasum`, network access
Notes: Same as `make test-e2e` but sets `AL_E2E_ONLINE=1` to download release binaries from GitHub. Use before releases or to populate the persistent binary cache. Pin the latest release version with `AL_E2E_LATEST_VERSION=X.Y.Z`.

- Run e2e tests for CI (mandatory upgrade scenarios)
```bash
make test-e2e-ci
```
Run from: repo root
Prerequisites: Go 1.26.0+, `curl`, `sha256sum` or `shasum`, network access
Notes: Same as `make test-e2e-online` but also sets `AL_E2E_REQUIRE_UPGRADE=1` to fail hard if upgrade binaries are missing. Used by `make ci`. Ensures 100% of scenarios execute including upgrade paths.

### Modules

- Run go mod tidy
```bash
make tidy
```
Run from: repo root  
Prerequisites: Go 1.26.0+

- Verify go.mod/go.sum are tidy
```bash
make tidy-check
```
Run from: repo root  
Prerequisites: Go 1.26.0+  
Notes: Fails if `go.mod`/`go.sum` would change.
Pre-existing intended working-tree changes are allowed; the command compares
the module files immediately before and after `go mod tidy`.

### Coverage

- Enforce coverage threshold (>= 90%)
```bash
make coverage
```
Run from: repo root  
Prerequisites: Go 1.26.0+
Notes: Canonical local/CI parity command for coverage. `make dev` and `make ci` both route through this target, and GitHub Actions runs `make ci`.

### Dev

- Primary local test command (format + fmt-check + lint + coverage + release tests)
```bash
make dev
```
Run from: repo root
Prerequisites: Go 1.26.0+, `make tools` has been run

- Run al subcommands against this repo's own .agent-layer using the source tree
```bash
make al-upgrade   # al upgrade
make al-sync      # al sync
make al-doctor    # al doctor
make al-claude    # al claude
make al-codex     # al codex
make al-agy       # al agy
make al-copilot   # al copilot
```
Run from: repo root
Prerequisites: Go 1.26.0+
Notes: Convenience wrappers against this repo's own `.agent-layer/` config. `al-doctor` and the interactive agent launchers build a source snapshot at `.agent-layer/tmp/dev-bin/al` and prepend that directory to `PATH`, so child `al dispatch` calls use the same source snapshot rather than the globally installed binary. The development launch bypasses repo version-pin handoff only for that Make invocation. `al-upgrade`, `al-sync`, and `al-wizard` continue to use `go run ./cmd/al`. Always use these wrappers instead of a globally installed `al` in this repo: this repo's `.agent-layer/config.toml` tracks the unreleased schema, so a released `al` rejects it as unrecognized keys.

- Run the Antigravity capability probe
```bash
go run ./cmd/al probe agy
```
Run from: repo root
Prerequisites: Antigravity (`agy`) installed on PATH
Notes: Prints JSON describing the current `agy` permissions and MCP behavior observed in a repo-local probe workspace. The workspace is seeded with a real stdio MCP server (this binary's hidden `__probe-mcp-fixture` subcommand exposing one `probe_ping` tool), so `capabilities.mcp_runtime_discovery` and `capabilities.mcp_tool_invoked` report `agy` behavior rather than a fixture defect. `timed_out` reports the probe's own 45-second bound separately from a failed run. Do not claim live Antigravity MCP support unless both MCP capability flags are true.

### CI

- Run CI checks locally
```bash
make ci
```
Run from: repo root
Prerequisites: Go 1.26.0+, `make tools` has been run
Notes: Includes `make tidy-check`, `make test-race` (race detector on concurrency-critical packages), `make test-release`, `make test-e2e-ci` (online e2e with required upgrade scenarios), and `make docs-cta-check`; requires network access for upgrade binary downloads. `tidy-check` permits an existing intended diff, reports a validation failure when `go mod tidy` changes the module files, and propagates dependency, toolchain, network, and filesystem errors.
GitHub Actions also runs a separate website build job using `make website-build-check` against `conn-castle/agent-layer-web`.
The release workflow runs this target on macOS before importing signing credentials.

### Release

- Install the pinned release vulnerability scanner
```bash
make release-tools
```
Run from: repo root
Prerequisites: Go 1.26.0+, network access
Notes: Installs the `govulncheck` version pinned in `go.mod` into `.tools/bin`. This tool is release-only and is not installed by `make tools`.

- Scan all four built release executables for known vulnerable symbols
```bash
make release-vuln-check DIST_DIR=dist
```
Run from: repo root
Prerequisites: `make release-tools`, all four release binaries in `DIST_DIR`, network access to the Go vulnerability database
Notes: Uses `govulncheck -mode=binary` and fails on a missing binary, known vulnerable symbol finding, scanner failure, or database failure. The tag-triggered release workflow enforces this after build and before notarization or publication; ordinary CI does not run it.

- Generate an embedded template ownership manifest for a release version
```bash
./scripts/generate-template-manifest.sh --tag vX.Y.Z
```
Run from: repo root
Prerequisites: Go 1.26.0+ (reads from the working tree, no git tag required)
Notes: Writes `internal/templates/manifests/X.Y.Z.json`. Run for each new release version and commit the generated manifest. After a version is tagged, do not regenerate or edit its manifest for later work; create the next version's manifest instead.

- Validate release readiness (run before tagging)
```bash
make release-preflight RELEASE_TAG=vX.Y.Z
```
Run from: repo root
Prerequisites: Go 1.26.0+, `make tools` has been run, `rg` (ripgrep) available on PATH, both manifests committed
Notes: Runs `make ci` and then validates upgrade-contract docs for the tag. Catches issues that would fail the release workflow. Requires a clean working tree and network access for upgrade binary downloads.

- Validate upgrade-contract docs for a target release tag
```bash
make docs-upgrade-check RELEASE_TAG=vX.Y.Z
```
Run from: repo root
Prerequisites: `site/docs/upgrades.mdx` and `CHANGELOG.md` include the target release tag; `rg` (ripgrep) available on PATH
Notes: Also runs upgrade CTA syntax checks across core docs/message surfaces.

- Validate upgrade CTA syntax drift in core docs/messages
```bash
make docs-cta-check
```
Run from: repo root
Prerequisites: `rg` (ripgrep) available on PATH
Notes: Fails on removed/invalid upgrade command surfaces (for example `--force` or `upgrade plan --json`) and on `al upgrade --yes` guidance that omits required apply flags.

- Build the published website in a local `agent-layer-web` checkout
```bash
make website-build-check SITE_BUILD_TAG=vX.Y.Z WEBSITE_REPO_DIR=/path/to/agent-layer-web
```
Run from: repo root
Prerequisites: Go 1.26.0+, Node 22+, npm, and a local `conn-castle/agent-layer-web` git checkout
Notes: Installs website dependencies, publishes this repo's `site/` content into `WEBSITE_REPO_DIR`, snapshots docs for `SITE_BUILD_TAG`, then runs `npm run build`. The checkout is mutated; use a temporary clone for release previews.

- Refresh the versioned website DeepSWE planner snapshot
```bash
make refresh-deepswe-planner-data
```
Run from: repo root
Prerequisites: Node 22+, curl, and network access
Notes: Downloads the official DeepSWE v1.1 trials/tasks JSON files into `.agent-layer/tmp/deepswe-planner-data/`, validates required source fields, excludes unusable trials visibly, and writes the versioned browser snapshot. Review the printed source URL and SHA-256 before accepting a changed snapshot. The generated `site/static/deepswe-planner/app/data.js` intentionally exceeds the general 500 KB pre-commit limit because it is the planner's reviewable, reproducible evidence snapshot.

- Verify the website optimizer against exhaustive allocation
```bash
make test-deepswe-planner
```
Run from: repo root
Prerequisites: Node 22+
Notes: Executes the planner's actual browser calculation code against the pinned published snapshot, exhaustively enumerates a deterministic small task/repetition search space, and verifies the optimal allocation and stable export contract.

- Build release artifacts locally (cross-compile)
```bash
make release-dist AL_VERSION=dev DIST_DIR=dist
```
Run from: repo root
Prerequisites: Go 1.26.0+, git, gzip, tar, `sha256sum` or `shasum`
Notes: Runs `test-release` first to validate release scripts. Local builds stay unsigned unless `AL_CODESIGN_IDENTITY` is set on macOS; `AL_REQUIRE_CODESIGN=1` fails if signing cannot run.

### Agent Layer skill A/B benchmark

- Re-run the established eight-task Luna-low matrix against the latest instructions and skills
```bash
BENCH_RUNNER_DIR="$(mktemp -d .agent-layer/tmp/deepswe-matrix-runner.XXXXXX)"
git cat-file -e '075ca0d^{commit}' 2>/dev/null || git fetch --no-tags origin 075ca0d
git archive 075ca0d | tar -x -C "$BENCH_RUNNER_DIR"
(cd "$BENCH_RUNNER_DIR" && go build -o "$OLDPWD/.agent-layer/tmp/al-benchmark-matrix-075" ./cmd/al)

BENCH_RUN_ID="${BENCH_RUNNER_DIR##*.}"
BENCH_LABEL="$(TZ=America/New_York date '+%Y-%m-%d %I:%M %p ET') — Luna Low after-change repeat 1 — $BENCH_RUN_ID"

.agent-layer/tmp/al-benchmark-matrix-075 benchmark matrix \
  --selection .agent-layer/tmp/deepswe-luna-low-065-selection-replicate-2.json \
  --baseline-execution luna:low \
  --treatment-execution luna:low \
  --treatment-label "$BENCH_LABEL" \
  --dispatch-config .agent-layer/tmp/luna-low-agent-layer-dispatch-v2.toml \
  --task-concurrency 4 \
  --check

.agent-layer/tmp/al-benchmark-matrix-075 benchmark matrix \
  --selection .agent-layer/tmp/deepswe-luna-low-065-selection-replicate-2.json \
  --baseline-execution luna:low \
  --treatment-execution luna:low \
  --treatment-label "$BENCH_LABEL" \
  --dispatch-config .agent-layer/tmp/luna-low-agent-layer-dispatch-v2.toml \
  --task-concurrency 4 \
  --yes
```
Run from: repo root
Prerequisites: Go 1.26.0+, Git, Docker, `uvx`, Codex authentication, the preserved selection and dispatch files under `.agent-layer/tmp`, and the compatible completed matrix state under `.agent-layer/state/benchmarks/deepswe/matrices/c9be59278c2d7b9b1e9b03dd9a68751ccc206df1f9dadab685d67674ded51c8b`
Notes: This is the recurring descriptive eight-task experiment, with one repetition per task. It is distinct from the website-planned campaign below. Run it from a checkout whose `origin` is this repository; the preflight verifies that commit `075ca0d` exists locally and fetches it from `origin` when missing. Commit `075ca0d` is the report-compatible matrix runner: it supports the current `$implement` workflow and dispatch-v2 file while retaining the task-environment and arm identities used by the completed Luna-low baseline. The binary runs from the repository root, so it fingerprints and packages the repository's current `internal/templates/instructions` and `internal/templates/skills`, not the runner snapshot's templates. Preflight should report `8 of 8` baseline cells cached and `0 of 8` treatment cells cached before paid execution. If it does not, stop rather than rerunning the baseline. The matrix report is written under the matrix state's `report/` directory; the latest consolidated scratch reports are `.agent-layer/tmp/deepswe-valid-runs-full-report.html` and `.agent-layer/tmp/all-eight-task-arms-live.{html,json}`.

Use a unique treatment label in the form `YYYY-MM-DD h:mm AM/PM ET — Luna Low <purpose> repeat <n> — <run-id>` (for example, `2026-08-05 8:12 PM ET — Luna Low after-fix repeat 1 — kP3x9Q`). The command derives the collision-resistant run ID from the unique `mktemp` runner directory. Do not reuse lifecycle labels such as `Latest` or `Final`; the date, description, repetition, and run ID keep repeated eight-task runs distinguishable in reports. Stored arm manifests are immutable benchmark evidence, so apply clearer historical names as report-level aliases rather than editing completed manifests.

- Validate or run the bare-model baseline from an exported website plan
```bash
go run ./cmd/al benchmark baseline --check --plan <plan.json> --task-concurrency 4
go run ./cmd/al benchmark baseline --plan <plan.json> --task-concurrency 4 --yes
pbpaste | go run ./cmd/al benchmark baseline --check --plan -
```
Run from: repo root
Prerequisites: Go 1.26.0+, Git, Docker, `uvx`, provider authentication, and a valid `deepswe-benchmark-plan` JSON exported by the website
Notes: The website is the only task/repetition selector. The command validates and executes its exact allocation. `--plan -` reads the exported JSON from standard input. Successful calls are immutable and reused by plan ID; failures are not retried automatically.

- Validate or run one immutable Agent Layer version
```bash
go run ./cmd/al benchmark treatment --check --plan <plan.json> --label "Iteration 1"
go run ./cmd/al benchmark treatment --plan <plan.json> --label "Iteration 1" --task-concurrency 4 --yes
```
Run from: repo root
Prerequisites: A completed baseline plus the baseline prerequisites
Notes: The current Agent Layer instructions and skills are fingerprinted into a version identity. A changed bundle creates a new version and reuses the shared baseline. `--check` validates readiness and reports missing calls without writing state or invoking a model. Treatment cost is not inferred from the baseline, so paid execution reports the number of calls and asks for explicit confirmation.

- Generate the campaign report without model calls
```bash
go run ./cmd/al benchmark report --plan <plan.json>
```
Run from: repo root
Prerequisites: A completed baseline and at least one completed treatment version
Notes: Calculates the observed two-sided threshold from both arms, then writes canonical JSON and offline HTML from immutable evidence. It does not invoke a provider, Pier, Docker, or a network service. Plans exported before cost-axis provenance was added require an explicit canonical `--analysis` artifact.

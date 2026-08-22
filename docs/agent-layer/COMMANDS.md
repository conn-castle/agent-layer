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

- Verify live Codex Agent Dispatch waits do not create polling turns
```bash
make test-codex-dispatch-wait-live
```
Run from: repo root
Prerequisites: Authenticated Codex CLI with access to `gpt-5.6-luna`, network access
Notes: Paid, local-only integration test. It creates a disposable Agent Layer project, runs fresh Luna-low coordinator and child sessions, and inspects the coordinator rollout for one direct `dispatch_wait` with no Agent Layer code-mode wrapper or polling turns. The `live_codex` build tag excludes it from ordinary test and CI targets.

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
Notes: Canonical local/CI parity command for coverage. `make ci` routes through this target, and GitHub Actions runs `make ci`.

### Dev

- Fast local formatting and lint loop
```bash
make dev
```
Run from: repo root
Prerequisites: Go 1.26.0+, `make tools` has been run
Notes: Formats Go source and runs golangci-lint. Does not run tests, coverage, or the full CI suite. Use `make test` or `make coverage` for those gates, and `make ci` as the complete local/pre-PR verification command (GitHub Actions also runs `make ci`).

- Run al subcommands against this repo's own .agent-layer using the source tree
```bash
make al-upgrade   # al upgrade
make al-sync      # al sync
make al-wizard    # al wizard
make al-doctor    # al doctor
make al-claude    # al claude
make al-codex     # al codex
make al-agy       # al agy
make al-copilot   # al copilot
make al-grok      # al grok
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
Notes: Prints JSON describing the current `agy` permissions and MCP behavior observed in a repo-local probe workspace. The contained `agy --print` process receives probe-only `--dangerously-skip-permissions` and `--sandbox`; stdout-derived visibility and tool-invocation results are measured under that diagnostic mode, not default headless permission prompting. The workspace is seeded with a real stdio MCP server (this binary's hidden `__probe-mcp-fixture` subcommand exposing one `probe_ping` tool), so `capabilities.mcp_runtime_discovery` and `capabilities.mcp_tool_invoked` report `agy` behavior rather than a fixture defect. `timed_out` reports the probe's own 45-second bound separately from a failed run. Do not claim live Antigravity MCP support unless both MCP capability flags are true.

- Run the Grok capability probe
```bash
go run ./cmd/al probe grok
```
Run from: repo root
Prerequisites: Grok Build CLI (`grok`) installed on PATH
Notes: Prints JSON describing a contained `grok` run under `.agent-layer/tmp/probe-grok-<ts>-<suffix>/`. The probe sets `GROK_HOME` to a disposable home, copies only an existing `auth.json` into it for the provider process and removes that copy immediately afterward, seeds folder trust, uses the workspace sandbox, and registers the same `__probe-mcp-fixture` server. `capabilities.mcp_tool_invoked` is true only when Grok actually called `probe_ping`. `timed_out` reports the probe's own 45-second bound separately from a failed run.

### CI

- Run CI checks locally (complete pre-PR verification gate)
```bash
make ci
```
Run from: repo root
Prerequisites: Go 1.26.0+, `make tools` has been run
Notes: The complete local/pre-PR verification command; GitHub Actions runs the same target. Includes `make tidy-check`, `make fmt-check`, `make lint`, `make dead-code`, `make coverage`, `make test-deepswe-planner`, `make test-race` (race detector on concurrency-critical packages), `make test-release`, `make test-e2e-harness`, `make test-e2e-ci` (online e2e with required upgrade scenarios), and `make docs-cta-check`; requires network access for upgrade binary downloads. `tidy-check` permits an existing intended diff, reports a validation failure when `go mod tidy` changes the module files, and propagates dependency, toolchain, network, and filesystem errors.
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
Notes: Downloads the official DeepSWE v1.1 trials/tasks JSON files into `.agent-layer/tmp/deepswe-planner-data/`, validates required source fields, excludes unusable trials visibly, precomputes deterministic one-run task-correlation distributions and per-task OLS calibrations, and writes the versioned browser snapshot. Review the printed source URL and SHA-256 before accepting a changed snapshot. The generated `site/static/deepswe-planner/app/data.js` intentionally exceeds the general 500 KB pre-commit limit because it is the planner's reviewable, reproducible evidence snapshot.

- Verify the website task-correlation evidence
```bash
make test-deepswe-planner
```
Run from: repo root
Prerequisites: Node 22+
Notes: Verifies deterministic build-time correlations and OLS calibrations, incomplete-cell exclusion, conservative sorting, inverse residual-variance weights, budget highlighting, deterministic score/price simulation, and comparison-allocation reuse.

- Build release artifacts locally (cross-compile)
```bash
make release-dist AL_VERSION=dev DIST_DIR=dist
```
Run from: repo root
Prerequisites: Go 1.26.0+, git, gzip, tar, `sha256sum` or `shasum`
Notes: Runs `test-release` first to validate release scripts. Local builds stay unsigned unless `AL_CODESIGN_IDENTITY` is set on macOS; `AL_REQUIRE_CODESIGN=1` fails if signing cannot run.

### Agent Layer skill A/B benchmark

- Preflight every task in the pinned DeepSWE catalog without provider calls
```bash
go run ./cmd/al benchmark readiness --task-concurrency 4
```
Run from: repo root
Prerequisites: Go 1.26.0+, Git, Docker, and network access when the pinned DeepSWE checkout or task images are not cached
Notes: The catalog is certified against pinned task images and writes content-addressed receipts under `.agent-layer/state/benchmarks/deepswe/`; it never invokes a provider model.

- Validate or execute one selection-based study
```bash
go run ./cmd/al benchmark run study.toml --dry-run --task-concurrency 4
go run ./cmd/al benchmark run study.toml --task-concurrency 4
```
Run from: repo root
Prerequisites: Go, Git, Docker, `uvx`, provider authentication, a website-exported `deepswe-benchmark-selection` JSON, and readiness receipts for every selected task.
Notes: `study.toml` is the full reproducibility boundary. It names the selection and one or more experiments, each with explicit `model` and `reasoning`. Every referenced path must be relative to, and remain within, the directory containing `study.toml`; place the study at or above all declared inputs. Agent Layer experiments explicitly name `config`, `instructions`, and/or `skills` paths relative to the study; skill experiments also name a nonempty `entry_prompt` containing exactly one `{{task}}` placeholder and an explicit `required_dispatch_roles` list. That list is the source of truth for required external Agent Dispatch roles (`plan-reviewer`, `implementer`, `code-reviewer`); omit the field only when the experiment has no skills. An explicit `required_dispatch_roles = []` is a valid unconstrained skills contract: the invoked skill may take a direct path with no external dispatch, and the experiment keeps the same identity as today's unconstrained cells. A nonempty list is part of experiment and treatment identity, so a constrained study cannot reuse older unconstrained results. A bare experiment declares none of those paths. The runner accepts historical selection schema v1 and treats v2 (including `manualExclusions`) as canonical. It content-addresses effective input bytes and applies the same versioned 4× agent-timeout resource contract to every arm. `--task` is repeatable invocation scoping only; it never changes study membership or identity.

When no completed-report candidate exists, missing-cell runs check each selected provider's repo-local credentials before treatment bundle staging, DeepSWE checkout, and task preparation. Duplicate experiments that share an adapter run one check. Codex is validated with the provider-native non-billing command `codex login status` under `CODEX_HOME=<repo>/.codex`. Claude selections fail closed before task setup: `claude auth status --json` accepts an invalid `CLAUDE_CODE_OAUTH_TOKEN`, and Claude stores credentials in the OS credential store regardless of `CLAUDE_CONFIG_DIR`. Successful Codex checks record `authentication_preflight` on that experiment in canonical `report/report.json` (provider, check, normalized authentication method, UTC timestamp) without tokens, account identifiers, credential bytes, or raw provider output. Authentication is invocation provenance and does not affect study, arm, or treatment identity.

A completed study whose immutable manifest uniquely matches the current selection, experiments, and treatments regenerates `report/report.json` without provider authentication, DeepSWE checkout, or inference. Treatment hashes are verified only after a cheap completed-report match, which may stage the current treatment without authentication. If that verification misses, the missing-cell path authenticates and continues. Expired Codex credentials and Claude's fail-closed limitation therefore cannot block report regeneration. Missing cells never take that path: they still authenticate before costly task and environment setup. More than one completed historical match is an error rather than a guessed study.

`--dry-run` validates the study and computes missing work. If any selected cell is missing, it performs the same readiness/dependency preflight, including provider authentication status checks. It never performs provider inference. A normal `benchmark run` is authorization for all missing paid calls: it does not prompt or require `--yes`. The disclosure distinguishes the bare arm's published-data estimate from actual Agent Layer cost, which cannot be estimated reliably in advance. Immutable evidence permits interruption-safe resume; completed studies regenerate their report without calls. One repetition is descriptive only; at least two repetitions permit the declared cell-variance Welch inference and three are preferable for stability. Comparisons apply to the fixed selection, not the full DeepSWE population; calibration also has a small part-whole correlation limitation.

Example study manifest:

```toml
selection = "selection.json"

[[experiments]]
name = "bare"
model = "luna"
reasoning = "low"

[[experiments]]
name = "agent-layer"
model = "luna"
reasoning = "low"
config = "treatment/config.toml"
instructions = "treatment/instructions"
skills = "treatment/skills"
entry_prompt = "treatment/prompt.md" # exactly one {{task}}
required_dispatch_roles = ["plan-reviewer", "implementer", "code-reviewer"]
```

The canonical `report/report.json` records exactly those declared experiments and every selected task/repetition, including missing cells. It contains the immutable projected bundle manifest and hashes, certified task environments, resource contract, task checksums, observed provider/runtime/worker provenance, normalized authentication preflight evidence, costs, calibrated means and contributions, and the complete Holm family. Provider-client version is evidence provenance, not arm identity, so compatible evidence remains usable after a client upgrade. Authentication preflight is likewise provenance rather than identity. Inputs are staged as a secret-free projection: the pinned `.env` is an empty marker, while only credential names referenced by the effective config are read from the invoking project's `.agent-layer/.env` (or process environment) and injected through Pier's child environment immediately before in-container sync. Literal credentials are never bundled, placed in Pier arguments, or retained in evidence.

Captured Agent Dispatch lifecycle evidence is retained with the candidate score, cost, receipt, and artifacts. A skills run is workflow-noncompliant when the completed fresh-root dispatches do not match the declared role contract one-to-one against the configured targets and each role's workflow skill (`review-plan`, `implement-plan`, `review-uncommitted-code`). Agent Dispatch `RunRecord.skill` is the attribution; records that omit it cannot fill a nonempty role contract. Native-subagent substitution with no dispatch records is noncompliant. Noncompliance does not delete evidence or suppress experiment-level scores. Any statistical comparison involving a noncompliant experiment is unavailable and states why. Conformant and explicitly unconstrained experiments keep the existing comparison behavior. Holm adjustment continues over only the remaining available family.

For an available pair, the report uses the fixed-selection score difference and variance `sum((weight*slope)^2 * (sA²/nA + sB²/nB))`, with Welch-Satterthwaite degrees of freedom. Raw two-sided Student-t p-values are adjusted by Holm step-down: sort the available unique pairs, multiply each raw p-value by the remaining family size, cap at one, and take the cumulative maximum. One repeat is descriptive, two provide within-cell variance, and three or more reduce the fragility of that estimate. Historical selection-based matrix evidence is considered only when its immutable manifest, target, inputs, resources, task/environment receipts, and verifier evidence exactly match; eligible old normalized scores are atomically canonicalized. Private campaign evidence is never scanned or modified.

arXiv paper forthcoming

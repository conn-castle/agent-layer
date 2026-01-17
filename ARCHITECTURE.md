# Architecture

## Guiding Principle

Correctness over convenience. Behavior must be deterministic, fully specified,
and exhaustively tested. Implementation language is secondary to explicit
responsibility boundaries.

## Layers

### Layer 0: User Interface (Shell - Thin Wrappers)

Responsibility: invoke the Node CLI with minimal wrapper logic.

Allowed:
- Execute the handoff to Node with argv passthrough.
- Print wrapper-level usage/help if the wrapper provides it.

Not allowed:
- Root resolution or temp parent root creation (Layer 2).
- Environment parsing or config loading (Layer 2).
- Filesystem mutations beyond launching the Node process.
- Business logic (Layer 2).

Test requirement: use integration tests that cover downstream layers.

### Layer 1: Infrastructure (Shell - OS Boundary Operations)

Responsibility: work that crosses OS/filesystem boundaries where shell is the
natural interface.

Allowed:
- Filesystem boundary: walk trees, check file existence, resolve symlinks,
  create directories/symlinks.
- OS boundary: install/check system packages via brew/apt-get.
- Process boundary: run external tools (shfmt, shellcheck, prettier, bats).
- Git boundary: run git commands, set hooks.
- Env boundary: load/export environment variables.

Not allowed:
- Parse or merge JSON/YAML/TOML (Layer 2).
- Generate complex output files (Layer 2).
- Complex data structures or policy decisions (Layer 2).

Correctness requirement:
1) Root resolution is owned by Node (`src/lib/roots.mjs`); shell scripts must call it instead of reimplementing.
2) Deterministic for identical inputs.
3) Tested for every scenario, error, and edge case.
4) Boundary-justified (must cross an OS boundary).

Size guideline: >300 lines should be reevaluated; >500 lines is a red flag
unless heavily commented.

### Layer 2: Core Logic (Node.js)

Responsibility: business logic, data transformation, async operations.

Allowed:
- Parse/merge JSON/YAML/TOML configs.
- Generate output files (shims, configs, skills).
- Validate user inputs and config data.
- MCP server runtime.
- Root resolution (including temp parent root creation).
- Complex async operations and error handling.

Not allowed:
- System package installation (Layer 1).
- Git operations (Layer 1).
- Installing git hooks (Layer 1).
- npm install (Layer 1).

Correctness requirement: Root resolution and env loading live in Node and must
follow the Root Selection Specification (README.md: Parent Root Resolution).

## Boundary Decision Guide

Heuristic:
1) OS boundary (filesystem, process, git, packages) -> Shell (Layer 1).
2) Data transformation or validation -> Node (Layer 2).
3) Env loading or root resolution -> Node (Layer 2).

Examples:
- Resolve parent root or `.agent-layer` discovery -> Node (`src/lib/roots.mjs`).
- Parse JSON config -> Node.
- Install npm packages -> Shell.
- Generate output file from template -> Node.
- Set git hooks path -> Shell.

## Test Requirements

- Spec-first tests: scenarios and errors must be explicitly named.
- Root resolution: 100% branch coverage for discovery and temp root creation.
- Error messages: exact match tests for every error condition.
- Precedence: all combinations of CLI flags, temp parent roots, .env parent root config, discovery.
- Integration: bootstrap -> setup -> al -> tests, CI simulation, git hooks,
  cleanup on failure/interrupt.
- CI: no flaky tests; failures must block merge.

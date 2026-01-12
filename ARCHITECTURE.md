# Architecture

## Guiding Principle

Correctness over convenience. Behavior must be deterministic, fully specified,
and exhaustively tested. Implementation language is secondary to explicit
responsibility boundaries.

## Layers

### Layer 0: User Interface (Shell - Thin Wrappers)

Responsibility: load environment configuration (selectively), validate
prerequisites, hand off to the next layer.

Allowed:
- Parse CLI flags into environment variables.
- Read PARENT_ROOT from `.env` (parse only; do NOT source).
- Check required system commands (node, git, npm).
- Decide exec vs child process for temp parent roots.
- Execute the handoff to Node or the next script.
- Print high-level status messages.

Not allowed:
- Path discovery logic (Layer 1).
- Creating directories or symlinks (Layer 1).
- Config parsing/merging (Layer 2).
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
1) Specified in the Root Selection Specification (README.md: Parent Root Resolution).
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
- Complex async operations and error handling.

Not allowed:
- Path discovery (Layer 1).
- System package installation (Layer 1).
- Git operations (Layer 1).
- Installing git hooks (Layer 1).
- npm install (Layer 1).

Correctness requirement: Node logic must honor PARENT_ROOT and AGENT_LAYER_ROOT
set by shell.

## Boundary Decision Guide

Heuristic:
1) OS boundary (filesystem, process, git, packages) -> Shell (Layer 1).
2) Data transformation or validation -> Node (Layer 2).
3) Env loading and handoff -> Shell (Layer 0).

Examples:
- Walk directory tree to find `.agent-layer/` -> Shell.
- Parse JSON config -> Node.
- Install npm packages -> Shell.
- Generate output file from template -> Node.
- Set git hooks path -> Shell.

## Test Requirements

- Spec-first tests: scenarios and errors must be explicitly named.
- Root resolution: 100% branch coverage for discovery and temp root creation.
- Error messages: exact match tests for every error condition.
- Precedence: all combinations of CLI flags, `.env`, discovery.
- Integration: bootstrap -> setup -> al -> tests, CI simulation, git hooks,
  cleanup on failure/interrupt.
- CI: no flaky tests; failures must block merge.

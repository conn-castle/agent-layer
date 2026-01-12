# Plan

## Decisions
- Terminology: parent root (repo root containing `.agent-layer/`), agent layer root (`.agent-layer/` itself).
- Env vars: `PARENT_ROOT` and `AGENT_LAYER_ROOT`.
- `.env` location for non-default usage: `AGENT_LAYER_ROOT/.env`.
- CLI flags: `--parent-root <path>`, `--temp-parent-root`, `--no-discover`.
- Discovery: allowed only when the agent layer root is named `.agent-layer`.
- Agent-layer repo (named `agent-layer`): no default parent root; require explicit `--parent-root` or `--temp-parent-root`.
- Precedence: CLI flags > `.env` > discovery.
- Temp parent root lifecycle: create temp dir, symlink `.agent-layer` into it, set `TEMP_PARENT_ROOT_CREATED=1`, default cleanup via caller trap; allow opt-out with `PARENT_ROOT_KEEP_TEMP=1` (or a flag).
- Error messaging: always explain which scenario applies, available options, and how to fix.
- Tests: full coverage for all scenarios and edge cases.
- Architecture: three-layer model (UI shell, Infrastructure shell, Core Node), with correctness and explicit boundaries prioritized over convenience.

## Work To Do
### Architecture
- Document three-layer responsibilities and explicit "allowed/not allowed" behaviors in the plan.
- Define correctness invariants for parent root selection and error messaging; treat as spec.
- Require tests to enforce the spec across shell and Node boundaries (no implicit behavior).

### Implementation
- Rename internal variables: `WORKING_ROOT` -> `PARENT_ROOT`, `AGENTLAYER_ROOT` -> `AGENT_LAYER_ROOT`.
- Rename helper file and functions to align with new terminology (e.g., `discover-root.sh` -> `parent-root.sh` or `work-root.sh`).
- Update CLI flags in all scripts and tests: `--work-root` -> `--parent-root`, `--temp-work-root` -> `--temp-parent-root`.
- Implement `.env` loading from `AGENT_LAYER_ROOT/.env` before root resolution; ensure `AGENT_LAYER_ROOT` is derived from script location.
- Enforce discovery rules: only allowed when basename of agent layer root is `.agent-layer`; otherwise require explicit parent root or temp parent root.
- Add `--no-discover` to force explicit parent root or temp parent root.
- Keep temp parent root creation responsible for `.agent-layer` symlink creation and proper cleanup signaling.
- Update error messages across scripts to reflect new terminology and guidance.

### Tests
- Add/adjust tests for:
  - Default discovery in consumer repo (`.agent-layer` present).
  - Explicit `--parent-root` usage.
  - `--temp-parent-root` behavior (creates `.agent-layer` symlink).
  - Agent-layer repo requires explicit parent root or temp parent root.
  - `--no-discover` forces explicit config.
  - `.env`-based `PARENT_ROOT` override and precedence with flags.
  - Error messaging for missing/invalid parent root.

### Documentation
- Update README for the four scenarios with new terminology and flags.
- Document `.env` requirement for non-default usage and the env var names.
- Update any examples/snippets that mention `work-root` or `AGENTLAYER_ROOT`.
- Add a short "How to tell which scenario you're in" section and fix-it steps.

### Cleanup
- Remove legacy references to `work-root`, `WORKING_ROOT`, and `AGENTLAYER_ROOT`.
- Ensure old helper files are deleted/renamed and imports updated.

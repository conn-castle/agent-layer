#!/usr/bin/env bash
# Agent Dispatch — verifies final-answer-only fresh dispatch, durable resume,
# skill prefixing, and max-depth failure.

install_dispatch_mock_codex() {
  local dir="$1"
  local mock_bin="$dir/mock-bin"
  local log_dir="$dir/mock-logs"
  mkdir -p "$mock_bin" "$log_dir"

  export MOCK_DISPATCH_CODEX_LOG="$log_dir/codex-dispatch.log"
  export MOCK_DISPATCH_CODEX_PROMPT="$log_dir/codex-dispatch.prompt"
  : > "$MOCK_DISPATCH_CODEX_LOG"
  : > "$MOCK_DISPATCH_CODEX_PROMPT"

  cat > "$mock_bin/codex" <<'MOCK_EOF'
#!/usr/bin/env bash
if [[ "${1:-}" == "--version" ]]; then
  printf '0.144.1\n'
  exit 0
fi
{
  echo "ARGS=$*"
  i=0
  for arg in "$@"; do
    echo "ARG_${i}=${arg}"
    i=$((i + 1))
  done
  env | grep -E '^(AL_DISPATCH|CODEX_HOME)' | sort || true
  echo "---END---"
} >> "$MOCK_DISPATCH_CODEX_LOG"
cat > "$MOCK_DISPATCH_CODEX_PROMPT"
printf '{"type":"thread.started","thread_id":"11111111-1111-4111-8111-111111111111"}\n'
printf '{"type":"agent_message","message":"codex-dispatch-ok"}\n'
printf '{"type":"turn.completed"}\n'
MOCK_EOF

  chmod +x "$mock_bin/codex"
  export PATH="$mock_bin:$PATH"
}

run_scenario_agent_dispatch() {
  section "Agent Dispatch"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"

  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard

  # Opt into repo-local Codex home so the dispatch child receives
  # CODEX_HOME=<repo>/.codex (asserted below).
  enable_codex_local_config_dir "$repo_dir"

  install_dispatch_mock_codex "$repo_dir"

  local skill_dir="$repo_dir/.agent-layer/skills/e2e-skill"
  mkdir -p "$skill_dir"
  cat > "$skill_dir/SKILL.md" <<'SKILL'
---
name: e2e-skill
description: Exercise dispatch skill prefixing.
---

Use the e2e dispatch skill.
SKILL

  local stdout_file="$repo_dir/dispatch.stdout"
  local stderr_file="$repo_dir/dispatch.stderr"
  local rc=0
  (cd "$repo_dir" && al dispatch --agent codex "Return ok" >"$stdout_file" 2>"$stderr_file") || rc=$?
  if [[ $rc -eq 0 ]]; then
    pass "al dispatch --agent codex succeeds"
  else
    fail "al dispatch --agent codex succeeds (exit code: $rc)"
  fi
  assert_file_contains "$stdout_file" "codex-dispatch-ok" "dispatch decodes codex final answer"
  assert_file_contains "$MOCK_DISPATCH_CODEX_LOG" "ARG_0=exec" "dispatch invokes codex exec"
  assert_file_contains "$MOCK_DISPATCH_CODEX_LOG" "ARG_1=--json" "dispatch requests codex JSON stream"
  assert_file_contains "$MOCK_DISPATCH_CODEX_LOG" "ARG_2=-" "dispatch passes prompt on stdin"
  assert_file_contains "$MOCK_DISPATCH_CODEX_LOG" "CODEX_HOME=$repo_dir/.codex" "dispatch child receives repo CODEX_HOME"
  assert_file_contains "$MOCK_DISPATCH_CODEX_LOG" "AL_DISPATCH_ACTIVE=1" "dispatch child receives depth marker"
  assert_file_contains "$MOCK_DISPATCH_CODEX_LOG" "AL_DISPATCH_CALLER_AGENT=codex" "dispatch child receives target caller marker"
  assert_file_contains "$MOCK_DISPATCH_CODEX_PROMPT" "Return ok" "dispatch passes positional prompt"
  assert_file_contains "$stderr_file" "] codex · fresh" "dispatch writes fresh identity to stderr"

  local session_name
  session_name="$(sed -nE 's/^\[([^]]+)\] codex · fresh.*/\1/p' "$stderr_file" | head -n 1)"
  if [[ -n "$session_name" ]]; then
    pass "dispatch allocates a durable friendly name"
  else
    fail "dispatch allocates a durable friendly name"
  fi

  : > "$MOCK_DISPATCH_CODEX_LOG"
  rc=0
  (cd "$repo_dir" && al dispatch resume "$session_name" "Continue" >"$stdout_file" 2>"$stderr_file") || rc=$?
  if [[ $rc -eq 0 ]]; then
    pass "al dispatch resume succeeds"
  else
    fail "al dispatch resume succeeds (exit code: $rc)"
  fi
  assert_file_contains "$stdout_file" "codex-dispatch-ok" "resume replays final answer only"
  assert_file_contains "$stderr_file" "[$session_name] codex · resumed · durable" "resume writes durable identity"
  assert_file_contains "$MOCK_DISPATCH_CODEX_LOG" "ARG_0=exec" "resume invokes codex exec"
  assert_file_contains "$MOCK_DISPATCH_CODEX_LOG" "ARG_1=resume" "resume uses explicit Codex resume command"
  assert_file_contains "$MOCK_DISPATCH_CODEX_LOG" "ARG_2=--json" "resume requests Codex JSON stream"
  assert_file_contains "$MOCK_DISPATCH_CODEX_LOG" "ARG_3=11111111-1111-4111-8111-111111111111" "resume uses stored thread ID"
  assert_file_contains "$MOCK_DISPATCH_CODEX_PROMPT" "Continue" "resume passes follow-up prompt on stdin"

  cat >> "$repo_dir/.agent-layer/config.toml" <<'TOML'

[agents.claude.dispatch]
  default_agent = "codex"
TOML
  : > "$MOCK_DISPATCH_CODEX_LOG"
  rc=0
  (cd "$repo_dir" && AL_DISPATCH_CALLER_AGENT=claude al dispatch "Use configured target" >"$stdout_file" 2>"$stderr_file") || rc=$?
  if [[ $rc -eq 0 ]]; then
    pass "al dispatch resolves configured caller default"
  else
    fail "al dispatch resolves configured caller default (exit code: $rc)"
  fi
  assert_file_contains "$stderr_file" "] codex · fresh" \
    "dispatch reports configured target identity"

  : > "$MOCK_DISPATCH_CODEX_PROMPT"
  rc=0
  (cd "$repo_dir" && al dispatch --agent codex --skill e2e-skill "Use skill" >"$stdout_file" 2>"$stderr_file") || rc=$?
  if [[ $rc -eq 0 ]]; then
    pass "al dispatch prefixes codex skill"
  else
    fail "al dispatch prefixes codex skill (exit code: $rc)"
  fi
  assert_file_contains "$MOCK_DISPATCH_CODEX_PROMPT" '$e2e-skill' "dispatch uses codex skill prefix"
  assert_file_contains "$MOCK_DISPATCH_CODEX_PROMPT" "Use skill" "dispatch includes prompt after skill"

  sed -i.bak 's/^max_depth = 3$/max_depth = 1/' "$repo_dir/.agent-layer/config.toml"
  rm -f "$repo_dir/.agent-layer/config.toml.bak"
  : > "$MOCK_DISPATCH_CODEX_LOG"
  rc=0
  (cd "$repo_dir" && AL_DISPATCH_ACTIVE=1 al dispatch --agent codex "nested" >"$stdout_file" 2>"$stderr_file") || rc=$?
  if [[ $rc -eq 75 ]]; then
    pass "nested dispatch exits 75"
  else
    fail "nested dispatch exits 75 (got: $rc)"
  fi
  assert_file_contains "$stderr_file" "dispatch.max_depth = 1" "nested dispatch explains failure"
  assert_mock_agent_not_called "$MOCK_DISPATCH_CODEX_LOG" "nested dispatch did not invoke target"

  cleanup_scenario_dir "$repo_dir"
}

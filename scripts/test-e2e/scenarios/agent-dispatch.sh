#!/usr/bin/env bash
# Agent Dispatch — verifies discovery and the asynchronous conversation API.

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
sleep "${MOCK_DISPATCH_DELAY_SECONDS:-0}"
printf '{"type":"thread.started","thread_id":"11111111-1111-4111-8111-111111111111"}\n'
printf '{"type":"item.completed","item":{"type":"agent_message","text":"codex-dispatch-ok"}}\n'
printf '{"type":"turn.completed"}\n'
MOCK_EOF

  chmod +x "$mock_bin/codex"
  export PATH="$mock_bin:$PATH"
}

dispatch_json_field() {
  local path="$1"
  local field="$2"
  sed -nE "s/.*\"${field}\":\"([^\"]+)\".*/\1/p" "$path" | head -n 1
}

run_scenario_agent_dispatch() {
  section "Agent Dispatch"

  local repo_dir
  repo_dir="$(setup_scenario_dir)"
  assert_exit_zero_in "$repo_dir" "al init --no-wizard" al init --no-wizard
  enable_codex_local_config_dir "$repo_dir"
  install_dispatch_mock_codex "$repo_dir"

  local options_file="$repo_dir/dispatch-options.json"
  assert_exit_zero_in "$repo_dir" "dispatch options returns selection metadata" \
    al dispatch options
  (cd "$repo_dir" && al dispatch options >"$options_file")
  assert_file_contains "$options_file" '"agent":"codex","available":true' \
    "dispatch options reports available codex"
  assert_file_contains "$options_file" '"reasoning_effort":{"supported":true' \
    "dispatch options reports supported overrides"

  local skill_dir="$repo_dir/.agent-layer/skills/e2e-skill"
  mkdir -p "$skill_dir"
  cat > "$skill_dir/SKILL.md" <<'SKILL'
---
name: e2e-skill
description: Exercise dispatch skill prefixing.
---

Use the e2e dispatch skill.
SKILL

  local start_file="$repo_dir/dispatch-start.json"
  local wait_file="$repo_dir/dispatch-wait.json"
  local stderr_file="$repo_dir/dispatch.stderr"
  local rc=0

  (cd "$repo_dir" && al dispatch start --agent codex \
    --prompt "Use defaults" >"$start_file" 2>"$stderr_file") || rc=$?
  local handle
  handle="$(dispatch_json_field "$start_file" handle)"
  (cd "$repo_dir" && al dispatch wait "$handle" >"$wait_file" 2>>"$stderr_file") || rc=$?
  if [[ $rc -eq 0 ]] && grep -q '"state":"completed"' "$wait_file"; then
    pass "dispatch start accepts omitted overrides"
  else
    fail "dispatch start accepts omitted overrides (exit code: $rc)"
  fi
  assert_file_not_contains "$MOCK_DISPATCH_CODEX_LOG" "--model" \
    "start omits an unconfigured model override"
  assert_file_not_contains "$MOCK_DISPATCH_CODEX_LOG" "model_reasoning_effort=" \
    "start omits an unconfigured reasoning override"

  : > "$MOCK_DISPATCH_CODEX_LOG"
  rc=0
  (cd "$repo_dir" && al dispatch start --agent codex --model gpt-test \
    --reasoning-effort high --prompt "Return ok" >"$start_file" 2>"$stderr_file") || rc=$?
  if [[ $rc -eq 0 ]] && grep -q '"state":"running"' "$start_file"; then
    pass "dispatch start returns a running handle"
  else
    fail "dispatch start returns a running handle (exit code: $rc)"
  fi

  handle="$(dispatch_json_field "$start_file" handle)"
  rc=0
  (cd "$repo_dir" && al dispatch wait "$handle" >"$wait_file" 2>>"$stderr_file") || rc=$?
  local result_path
  result_path="$(dispatch_json_field "$wait_file" result_path)"
  if [[ $rc -eq 0 ]] && grep -q '"state":"completed"' "$wait_file" && [[ -f "$result_path" ]]; then
    pass "dispatch wait returns a durable completed result"
  else
    fail "dispatch wait returns a durable completed result (exit code: $rc)"
  fi
  assert_file_contains "$result_path" "codex-dispatch-ok" "completed Markdown contains the answer"
  assert_file_contains "$MOCK_DISPATCH_CODEX_LOG" "ARG_0=exec" "start invokes codex exec"
  assert_file_contains "$MOCK_DISPATCH_CODEX_LOG" "CODEX_HOME=$repo_dir/.codex" "worker receives repo CODEX_HOME"
  assert_file_contains "$MOCK_DISPATCH_CODEX_LOG" "AL_DISPATCH_ACTIVE=1" "worker receives depth marker"
  assert_file_contains "$MOCK_DISPATCH_CODEX_PROMPT" "Return ok" "start passes prompt"

  : > "$MOCK_DISPATCH_CODEX_LOG"
  rc=0
  (cd "$repo_dir" && al dispatch continue "$handle" --prompt "Continue" >"$start_file" 2>"$stderr_file") || rc=$?
  (cd "$repo_dir" && al dispatch wait "$handle" >"$wait_file" 2>>"$stderr_file") || rc=$?
  if [[ $rc -eq 0 ]] && grep -q '"state":"completed"' "$wait_file"; then
    pass "dispatch continue completes the conversation"
  else
    fail "dispatch continue completes the conversation (exit code: $rc)"
  fi
  assert_file_contains "$MOCK_DISPATCH_CODEX_LOG" "ARG_1=resume" "continue resumes the provider conversation"
  assert_file_contains "$MOCK_DISPATCH_CODEX_PROMPT" "Continue" "continue passes its prompt"

  : > "$MOCK_DISPATCH_CODEX_PROMPT"
  rc=0
  (cd "$repo_dir" && al dispatch start --agent codex --model gpt-test \
    --reasoning-effort high --skill e2e-skill --prompt "Use skill" >"$start_file") || rc=$?
  handle="$(dispatch_json_field "$start_file" handle)"
  (cd "$repo_dir" && al dispatch wait "$handle" >"$wait_file") || rc=$?
  if [[ $rc -eq 0 ]] && grep -q '"state":"completed"' "$wait_file"; then
    pass "dispatch start with skill completes"
  else
    fail "dispatch start with skill completes (exit code: $rc)"
  fi
  assert_file_contains "$MOCK_DISPATCH_CODEX_PROMPT" '$e2e-skill' "start invokes the selected skill"

  rc=0
  local handles=()
  local index
  for index in {1..8}; do
    local parallel_start="$repo_dir/parallel-${index}-start.json"
    (cd "$repo_dir" && MOCK_DISPATCH_DELAY_SECONDS=0.3 al dispatch start --agent codex \
      --model gpt-test --reasoning-effort high --prompt "Review ${index}" >"$parallel_start") || rc=$?
    handles+=("$(dispatch_json_field "$parallel_start" handle)")
  done
  local waiter_pids=()
  for index in {1..8}; do
    (cd "$repo_dir" && al dispatch wait "${handles[$((index - 1))]}" >"$repo_dir/parallel-${index}-wait.json") &
    waiter_pids+=("$!")
  done
  for index in "${waiter_pids[@]}"; do
    wait "$index" || rc=$?
  done
  if [[ $rc -eq 0 ]] && [[ "$(grep -l '"state":"completed"' "$repo_dir"/parallel-*-wait.json | wc -l | tr -d ' ')" -eq 8 ]]; then
    pass "eight independent conversations complete in parallel"
  else
    fail "eight independent conversations complete in parallel"
  fi

  rc=0
  (cd "$repo_dir" && MOCK_DISPATCH_DELAY_SECONDS=5 al dispatch start --agent codex \
    --model gpt-test --reasoning-effort high --prompt "Cancel me" >"$start_file") || rc=$?
  handle="$(dispatch_json_field "$start_file" handle)"
  (cd "$repo_dir" && al dispatch cancel "$handle" >"$wait_file") || rc=$?
  if [[ $rc -eq 0 ]] && grep -q '"state":"cancelled"' "$wait_file"; then
    pass "dispatch cancel returns cancelled"
  else
    fail "dispatch cancel returns cancelled (exit code: $rc)"
  fi
  rc=0
  (cd "$repo_dir" && al dispatch cancel "$handle" >"$wait_file") || rc=$?
  if [[ $rc -eq 0 ]] && grep -q '"state":"cancelled"' "$wait_file"; then
    pass "repeated cancel is idempotent"
  else
    fail "repeated cancel is idempotent (exit code: $rc)"
  fi

  sed -i.bak 's/^max_depth = 3$/max_depth = 1/' "$repo_dir/.agent-layer/config.toml"
  rm -f "$repo_dir/.agent-layer/config.toml.bak"
  : > "$MOCK_DISPATCH_CODEX_LOG"
  rc=0
  (cd "$repo_dir" && AL_DISPATCH_ACTIVE=1 al dispatch start --agent codex --model gpt-test \
    --reasoning-effort high --prompt nested >"$start_file" 2>"$stderr_file") || rc=$?
  if [[ $rc -eq 75 ]]; then
    pass "nested dispatch exits 75"
  else
    fail "nested dispatch exits 75 (got: $rc)"
  fi
  assert_mock_agent_not_called "$MOCK_DISPATCH_CODEX_LOG" "nested dispatch did not invoke target"

  cleanup_scenario_dir "$repo_dir"
}

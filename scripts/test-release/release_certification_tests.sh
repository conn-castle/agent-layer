run_release_certification_script_tests() {
  section "Release Dispatch Helper Tests"

  local fixture mock_bin log head_sha scenario
  fixture="$(mktemp -d)"
  mock_bin="$fixture/bin"
  log="$fixture/gh.log"
  head_sha="1111111111111111111111111111111111111111"
  mkdir -p "$fixture/scripts" "$mock_bin"
  cp "$ROOT_DIR/scripts/release.sh" "$fixture/scripts/"
  cp "$ROOT_DIR/scripts/certify-release.sh" "$fixture/scripts/"
  chmod +x "$fixture/scripts/release.sh" "$fixture/scripts/certify-release.sh"

  # Stub verify-release-ci.sh inside the fixture (its own logic is tested exhaustively in workflow_security_tests.sh).
  cat >"$fixture/scripts/verify-release-ci.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'verify-release-ci %s\n' "$*" >>"${TEST_GH_LOG}"
if [[ "${TEST_SCENARIO}" == ci-failed ]]; then
  echo "ERROR: No successful CI run" >&2
  exit 1
fi
echo "Using exact-tree same-repo PR #580 ci.yml: https://example.invalid/runs/901"
EOF
  chmod +x "$fixture/scripts/verify-release-ci.sh"

  cat >"$mock_bin/git" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case "$*" in
  "branch --show-current") echo "${TEST_BRANCH:-main}" ;;
  "status --porcelain")
    if [[ "${TEST_SCENARIO}" == dirty-after-run && -f "${TEST_FIXTURE}/run.watched" ]]; then
      echo ' M release-source'
    elif [[ "${TEST_SCENARIO}" == dirty-tree ]]; then
      echo ' M dirty'
    fi
    ;;
  "rev-parse HEAD") echo "${TEST_HEAD_SHA}" ;;
  "ls-remote --exit-code origin refs/heads/main") printf '%s\trefs/heads/main\n' "${TEST_REMOTE_SHA}" ;;
  "ls-remote --tags origin refs/tags/"*)
    if [[ "${TEST_SCENARIO}" == tag-exists ]]; then
      printf '9999999999999999999999999999999999999999\trefs/tags/v9.8.7\n'
    fi
    ;;
  *) echo "unexpected git invocation: $*" >&2; exit 2 ;;
esac
EOF

  cat >"$mock_bin/sleep" <<'EOF'
#!/usr/bin/env bash
printf 'sleep %s\n' "$*" >>"${TEST_GH_LOG}"
EOF

  cat >"$mock_bin/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"${TEST_GH_LOG}"
case "$*" in
  "repo view --json nameWithOwner --jq .nameWithOwner")
    echo "conn-castle/agent-layer"
    ;;
  "run list "*"--json databaseId,status,conclusion"*)
    workflow="" branch="" commit="" event="" limit=20 fields="" jq_expr=""
    shift 2
    while [[ $# -gt 0 ]]; do
      case "$1" in
        --workflow) workflow="$2" ;;
        --branch) branch="$2" ;;
        --commit) commit="$2" ;;
        --event) event="$2" ;;
        --limit) limit="$2" ;;
        --json) fields="$2" ;;
        --jq) jq_expr="$2" ;;
        *) echo "unexpected gh run list argument: $1" >&2; exit 2 ;;
      esac
      shift 2
    done
    runs='[]'
    if [[ "${TEST_SCENARIO}" == rehearsal-cached ]]; then
      runs='[{"databaseId":70,"status":"completed","conclusion":"success","displayTitle":"Release rehearsal v9.8.7"}]'
    elif [[ -f "${TEST_FIXTURE}/release.dispatched" ]]; then
      title="Release v9.8.7"
      [[ "${TEST_REHEARSAL:-0}" == "1" ]] && title="Release rehearsal v9.8.7"
      runs="[{\"databaseId\":72,\"status\":\"in_progress\",\"conclusion\":\"\",\"displayTitle\":\"${title}\"}]"
    fi
    jq --arg sha "$TEST_HEAD_SHA" --arg branch "$branch" --arg commit "$commit" \
       --arg event "$event" --argjson limit "$limit" --arg fields "$fields" '
      map(.headSha = (.headSha // $sha) | .headBranch = (.headBranch // "main") | .event = (.event // "workflow_dispatch"))
      | map(select(($branch == "" or .headBranch == $branch) and ($commit == "" or .headSha == $commit) and ($event == "" or .event == $event)))[:$limit]
      | ($fields | split(",")) as $keys
      | map(with_entries(select(.key as $key | $keys | index($key))))
    ' <<<"$runs" | jq -r "$jq_expr"
    ;;
  "workflow run release.yml --ref main -f release_tag=v9.8.7 -f rehearsal="*)
    touch "${TEST_FIXTURE}/release.dispatched"
    ;;
  "run watch "*" --exit-status")
    touch "${TEST_FIXTURE}/run.watched"
    ;;
  "run view "*" --json conclusion --jq .conclusion")
    if [[ "${TEST_SCENARIO}" == run-failed ]]; then echo failure; else echo success; fi
    ;;
  "run view "*" --json url --jq .url")
    echo "https://example.invalid/runs/$3"
    ;;
  *) echo "unexpected gh invocation: $*" >&2; exit 2 ;;
esac
EOF
  chmod +x "$mock_bin/git" "$mock_bin/gh" "$mock_bin/sleep"

  run_release_helper() {
    scenario="$1"
    local remote_sha="${2:-$head_sha}"
    local version="${3-v9.8.7}"
    local rehearsal="${4:-0}"
    local script_name="${5:-release.sh}"
    rm -f "$fixture/release.dispatched" "$fixture/run.watched"
    : >"$log"
    PATH="$mock_bin:$PATH" TEST_HEAD_SHA="$head_sha" TEST_REMOTE_SHA="$remote_sha" \
      TEST_GH_LOG="$log" TEST_FIXTURE="$fixture" TEST_SCENARIO="$scenario" \
      TEST_REHEARSAL="$rehearsal" AL_RELEASE_REHEARSAL="$rehearsal" AL_VERSION="$version" \
      /bin/bash "$fixture/scripts/$script_name" >"$fixture/$scenario.out" 2>"$fixture/$scenario.err"
  }

  if run_release_helper publish-ok "$head_sha" "v9.8.7" "0" &&
     grep -q '^workflow run release.yml --ref main -f release_tag=v9.8.7 -f rehearsal=false$' "$log" &&
     grep -q '^run watch 72 --exit-status$' "$log" &&
     grep -q 'Published v9.8.7 at ' "$fixture/publish-ok.out"; then
    pass "release-helper: dispatches single-run release with rehearsal=false and watches to completion"
  else
    fail "release-helper: publish path failed"
    cat "$fixture/publish-ok.out" "$fixture/publish-ok.err" "$log"
  fi

  if run_release_helper rehearsal-ok "$head_sha" "v9.8.7" "1" "certify-release.sh" &&
     grep -q '^workflow run release.yml --ref main -f release_tag=v9.8.7 -f rehearsal=true$' "$log" &&
     grep -q 'Signed release rehearsal passed for v9.8.7' "$fixture/rehearsal-ok.out"; then
    pass "release-helper: certify-release.sh compatibility wrapper dispatches rehearsal=true"
  else
    fail "release-helper: certify-release.sh wrapper failed"
    cat "$fixture/rehearsal-ok.out" "$fixture/rehearsal-ok.err" "$log"
  fi

  if run_release_helper rehearsal-cached "$head_sha" "v9.8.7" "1" &&
     ! grep -q '^workflow run' "$log" &&
     grep -q 'already succeeded' "$fixture/rehearsal-cached.out"; then
    pass "release-helper: reuses already-successful rehearsal for exact SHA and tag"
  else
    fail "release-helper: failed to reuse cached rehearsal"
  fi

  if run_release_helper tag-exists "$head_sha" "v9.8.7" "0" || [[ -s "$log" ]] ||
     ! grep -q 'already exists on origin' "$fixture/tag-exists.err"; then
    fail "release-helper: must reject an already-existing tag before CI/workflow calls"
  else
    pass "release-helper: rejects an already-existing tag before workflow calls"
  fi

  if run_release_helper unpushed "2222222222222222222222222222222222222222" "v9.8.7" "0" || [[ -s "$log" ]] ||
     ! grep -q 'is not the pushed origin/main commit' "$fixture/unpushed.err"; then
    fail "release-helper: must reject an unpushed main commit"
  else
    pass "release-helper: rejects an unpushed main commit"
  fi

  if run_release_helper ci-failed "$head_sha" "v9.8.7" "0" || grep -q '^workflow run' "$log"; then
    fail "release-helper: must stop before dispatching when CI verification fails"
  else
    pass "release-helper: blocks dispatch when CI verification fails"
  fi

  if run_release_helper no-version "$head_sha" "" "0" || [[ -s "$log" ]] ||
     ! grep -q 'AL_VERSION must explicitly name' "$fixture/no-version.err"; then
    fail "release-helper: must reject missing AL_VERSION"
  else
    pass "release-helper: rejects missing AL_VERSION"
  fi

  if run_release_helper run-failed "$head_sha" "v9.8.7" "0" ||
     ! grep -q 'concluded failure' "$fixture/run-failed.err"; then
    fail "release-helper: must fail when dispatched workflow concludes failure"
  else
    pass "release-helper: propagates workflow failure"
  fi

  if run_release_helper dirty-after-run "$head_sha" "v9.8.7" "0" ||
     ! grep -q 'main changed during release workflow execution' "$fixture/dirty-after-run.err"; then
    fail "release-helper: must detect main/tree change during workflow execution"
  else
    pass "release-helper: detects main/tree changes during workflow execution"
  fi

  rm -rf "$fixture"
}

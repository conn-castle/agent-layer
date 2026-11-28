# Security contracts for the declarative release and CI pipelines.

release_workflow_job() {
  local workflow="$1"
  local job="$2"
  awk -v job="$job" '
    $0 == "  " job ":" { capture = 1 }
    capture {
      if ($0 != "  " job ":" && /^  [^[:space:]]/) exit
      print
    }
  ' "$workflow"
}

release_workflow_step() {
  local job="$1"
  local step="$2"
  awk -v step="$step" '
    $0 == "      - name: " step { capture = 1 }
    capture {
      if ($0 != "      - name: " step && /^      - /) exit
      print
    }
  ' <<<"$job"
}

release_step_line() {
  local job="$1"
  local step="$2"
  awk -v step="$step" '$0 == "      - name: " step { print NR; exit }' <<<"$job"
}

release_workflow_run_script() {
  release_workflow_step "$1" "$2" | awk '
    /^        run: \|$/ { capture = 1; next }
    capture && /^          / { print substr($0, 11); next }
    capture && /^[[:space:]]*$/ { print ""; next }
    capture { exit }
  '
}

run_release_actor_guard_tests() {
  local workflow="$1" readiness_job fixture step_script
  readiness_job=$(release_workflow_job "$workflow" "release-readiness")
  fixture=$(mktemp -d "$tmp_dir/actor-guard.XXXXXX")
  step_script="$fixture/actor.sh"
  release_workflow_run_script "$readiness_job" "Verify authorized release actor and branch" >"$step_script"

  if [[ ! -s "$step_script" ]]; then
    fail "release-security: actor guard script is missing"
    rm -rf "$fixture"
    return
  fi

  local rc=0
  if GITHUB_REF="refs/heads/main" GITHUB_ACTOR="nicholasjconn" RELEASE_ACTORS="nicholasjconn, other-admin" \
     /bin/bash "$step_script" >"$fixture/ok.out" 2>&1; then
    pass "release-security: actor guard accepts allowlisted actor on main"
  else
    fail "release-security: actor guard rejected valid allowlisted actor"
    cat "$fixture/ok.out"
  fi

  local case_name ref actor allowed
  for case_name in wrong-branch unlisted-actor substring-actor empty-allowlist empty-actor; do
    ref="refs/heads/main"
    actor="nicholasjconn"
    allowed="nicholasjconn"
    case "$case_name" in
      wrong-branch) ref="refs/heads/feature" ;;
      unlisted-actor) actor="contributor" ;;
      substring-actor) actor="nicholas"; allowed="nicholasjconn" ;;
      empty-allowlist) allowed="" ;;
      empty-actor) actor="" ;;
    esac
    rc=0
    GITHUB_REF="$ref" GITHUB_ACTOR="$actor" RELEASE_ACTORS="$allowed" \
      /bin/bash "$step_script" >"$fixture/$case_name.out" 2>&1 || rc=$?
    if [[ "$rc" -ne 0 ]]; then
      pass "release-security: actor guard rejects $case_name"
    else
      fail "release-security: actor guard must reject $case_name"
    fi
  done

  rm -rf "$fixture"
}

run_verify_release_ci_tests() {
  local script="$ROOT_DIR/scripts/verify-release-ci.sh"
  if [[ ! -x "$script" ]]; then
    fail "verify-release-ci: script missing or not executable"
    return
  fi

  local fixture repo_dir mock_bin
  fixture=$(mktemp -d "$tmp_dir/verify-ci.XXXXXX")
  repo_dir="$fixture/repo"
  mock_bin="$fixture/bin"
  mkdir -p "$repo_dir" "$mock_bin"

  # Build a real local git repository with:
  #   base -> pr_head -> clean_merge (tree == pr_head, first parent == base, ancestor OK)
  #   base -> extra_main -> skewed_merge (tree != pr_head and first parent not ancestor of pr_head)
  #   base -> squash_commit (tree == pr_head, first parent == base, ancestor OK)
  (
    cd "$repo_dir"
    git init -q --initial-branch=main
    git config user.name "Test"
    git config user.email "test@example.invalid"
    printf 'v1\n' > app.txt
    git add app.txt
    git commit -qm "base"
    base_sha="$(git rev-parse HEAD)"

    git checkout -qb release/v9.8.7
    printf 'v2\n' > app.txt
    git add app.txt
    git commit -qm "pr commit"
    pr_head_sha="$(git rev-parse HEAD)"

    git checkout -q main
    git merge -q --no-ff --no-edit release/v9.8.7
    clean_merge_sha="$(git rev-parse HEAD)"

    # Create a squash commit directly on base with the exact same tree as pr_head.
    git checkout -qb squash-main "$base_sha"
    printf 'v2\n' > app.txt
    git add app.txt
    git commit -qm "squash merge"
    squash_sha="$(git rev-parse HEAD)"

    # Create a skewed merge where main advanced before merging release/v9.8.7.
    git checkout -qb skewed-main "$base_sha"
    printf 'other\n' > other.txt
    git add other.txt
    git commit -qm "main advanced"
    git merge -q --no-ff --no-edit release/v9.8.7
    skewed_merge_sha="$(git rev-parse HEAD)"

    # Create a commit whose tree matches pr_head via revert, but main advanced so base is not ancestor.
    git checkout -qb moved-same-tree "$base_sha"
    printf 'temp\n' > temp.txt
    git add temp.txt
    git commit -qm "advance main"
    git rm -q temp.txt
    printf 'v2\n' > app.txt
    git add app.txt
    git commit -qm "same tree after main moved"
    moved_same_tree_sha="$(git rev-parse HEAD)"

    printf '%s\n' "$base_sha" > "$fixture/base_sha"
    printf '%s\n' "$pr_head_sha" > "$fixture/pr_head_sha"
    printf '%s\n' "$clean_merge_sha" > "$fixture/clean_merge_sha"
    printf '%s\n' "$squash_sha" > "$fixture/squash_sha"
    printf '%s\n' "$skewed_merge_sha" > "$fixture/skewed_merge_sha"
    printf '%s\n' "$moved_same_tree_sha" > "$fixture/moved_same_tree_sha"
  )

  local pr_head_sha clean_merge_sha squash_sha skewed_merge_sha moved_same_tree_sha
  pr_head_sha="$(<"$fixture/pr_head_sha")"
  clean_merge_sha="$(<"$fixture/clean_merge_sha")"
  squash_sha="$(<"$fixture/squash_sha")"
  skewed_merge_sha="$(<"$fixture/skewed_merge_sha")"
  moved_same_tree_sha="$(<"$fixture/moved_same_tree_sha")"

  cat >"$mock_bin/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$1 $2" == "run list" ]]; then
  shift 2
  workflow="" branch="" commit="" event="" status="" limit=20 fields="" jq_expr=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --workflow) workflow="$2" ;;
      --branch) branch="$2" ;;
      --commit) commit="$2" ;;
      --event) event="$2" ;;
      --status) status="$2" ;;
      --limit) limit="$2" ;;
      --json) fields="$2" ;;
      --jq) jq_expr="$2" ;;
      *) echo "unexpected gh run list argument: $1" >&2; exit 2 ;;
    esac
    shift 2
  done
  if [[ -n "$status" ]]; then
    echo "ERROR: --status must not be used" >&2
    exit 2
  fi
  jq --arg workflow "$workflow" --arg branch "$branch" --arg commit "$commit" \
     --arg event "$event" --argjson limit "$limit" --arg fields "$fields" '
    map(select(
      ($workflow == "" or .workflow == $workflow) and
      ($branch == "" or .headBranch == $branch) and
      ($commit == "" or .headSha == $commit) and
      ($event == "" or .event == $event)
    ))[:$limit]
    | map(if has("responseHeadSha") then .headSha = .responseHeadSha else . end)
    | map(if has("responseEvent") then .event = .responseEvent else . end)
    | map(if has("responseHeadBranch") then .headBranch = .responseHeadBranch else . end)
    | ($fields | split(",")) as $keys
    | map(with_entries(select(.key as $key | $keys | index($key))))
  ' "$TEST_PUSH_RUNS" | jq -r "$jq_expr"
  exit 0
fi

if [[ "$1" == "api" ]]; then
  endpoint="$2"
  case "$endpoint" in
    *"/commits/"*"/pulls")
      if [[ "$endpoint" == *"/commits/${TEST_PR_HEAD_SHA}/pulls" ]]; then
        cat "$TEST_HEAD_PULLS"
      else
        cat "$TEST_COMMIT_PULLS"
      fi
      ;;
    *"/actions/workflows/ci.yml/runs"*)
      if [[ "$endpoint" == *"status="* ]]; then
        echo "ERROR: status query param must not be used" >&2
        exit 2
      fi
      cat "$TEST_PR_RUNS"
      ;;
    *)
      echo "unexpected gh api endpoint: $endpoint" >&2
      exit 2
      ;;
  esac
  exit 0
fi
echo "unexpected gh invocation: $*" >&2
exit 2
EOF
  chmod +x "$mock_bin/gh"

  run_verify_case() {
    local name="$1" commit="$2" expected="$3" expected_out="$4"
    local rc=0
    (
      cd "$repo_dir"
      PATH="$mock_bin:$PATH" \
        TEST_PUSH_RUNS="$fixture/push_runs.json" \
        TEST_COMMIT_PULLS="$fixture/commit_pulls.json" \
        TEST_HEAD_PULLS="$fixture/head_pulls.json" \
        TEST_PR_RUNS="$fixture/pr_runs.json" \
        TEST_PR_HEAD_SHA="$pr_head_sha" \
        /bin/bash "$script" --commit "$commit" --repo "conn-castle/agent-layer"
    ) >"$fixture/$name.out" 2>"$fixture/$name.err" || rc=$?
    if [[ "$rc" -ne "$expected" ]]; then
      fail "verify-release-ci: $name exited $rc (expected $expected)"
      cat "$fixture/$name.out" "$fixture/$name.err"
    elif [[ -n "$expected_out" ]] && ! grep -Fq "$expected_out" "$fixture/$name.out"; then
      fail "verify-release-ci: $name output missing expected '$expected_out'"
      cat "$fixture/$name.out"
    else
      pass "verify-release-ci: $name"
    fi
  }

  local default_pr_runs default_commit_pulls default_head_pulls
  default_pr_runs=$(jq -cn --arg sha "$pr_head_sha" '{
    workflow_runs: [{
      id: 901, event: "pull_request", status: "completed", conclusion: "success",
      head_sha: $sha, head_branch: "release/v9.8.7",
      head_repository: { full_name: "conn-castle/agent-layer" },
      pull_requests: [], html_url: "https://example.invalid/runs/901"
    }]
  }')
  default_commit_pulls=$(jq -cn --arg merge "$clean_merge_sha" --arg head "$pr_head_sha" '[{
    number: 580, merged_at: "2026-11-28T01:01:48Z", merge_commit_sha: $merge,
    base: { ref: "main", repo: { full_name: "conn-castle/agent-layer" } },
    head: { ref: "release/v9.8.7", sha: $head, repo: { full_name: "conn-castle/agent-layer" } }
  }]')
  default_head_pulls="$default_commit_pulls"

  # 1. Exact push CI passes (including after >20 failed runs).
  jq -cn --arg sha "$clean_merge_sha" '
    [range(25) | {databaseId: 1, workflow: "ci.yml", headBranch: "main", headSha: $sha, event: "push", status: "completed", conclusion: "failure", url: "https://example.invalid/fail"}] +
    [{databaseId: 534, workflow: "ci.yml", headBranch: "main", headSha: $sha, event: "push", status: "completed", conclusion: "success", url: "https://example.invalid/runs/534"}]
  ' >"$fixture/push_runs.json"
  echo '[]' >"$fixture/commit_pulls.json"
  echo '[]' >"$fixture/head_pulls.json"
  echo '{"workflow_runs":[]}' >"$fixture/pr_runs.json"
  run_verify_case "push-success-after-20" "$clean_merge_sha" 0 "Using exact-commit push ci.yml: https://example.invalid/runs/534"

  # 2. Push response mismatch rejected when no PR matches.
  jq -cn --arg sha "$clean_merge_sha" '[{
    databaseId: 534, workflow: "ci.yml", headBranch: "main", headSha: $sha, event: "push",
    responseEvent: "pull_request", status: "completed", conclusion: "success", url: "https://example.invalid/runs/534"
  }]' >"$fixture/push_runs.json"
  run_verify_case "push-response-event-mismatch" "$clean_merge_sha" 1 ""

  # 3. Clean merge exact-tree same-repo PR passes when push CI has not finished yet.
  echo '[]' >"$fixture/push_runs.json"
  printf '%s\n' "$default_commit_pulls" >"$fixture/commit_pulls.json"
  printf '%s\n' "$default_head_pulls" >"$fixture/head_pulls.json"
  printf '%s\n' "$default_pr_runs" >"$fixture/pr_runs.json"
  run_verify_case "clean-merge-pr-success" "$clean_merge_sha" 0 "Using exact-tree same-repo PR #580 ci.yml: https://example.invalid/runs/901"

  # 4. Squash merge with identical tree & ancestor passes.
  jq -cn --arg merge "$squash_sha" --arg head "$pr_head_sha" '[{
    number: 581, merged_at: "2026-11-28T01:01:48Z", merge_commit_sha: $merge,
    base: { ref: "main", repo: { full_name: "conn-castle/agent-layer" } },
    head: { ref: "release/v9.8.7", sha: $head, repo: { full_name: "conn-castle/agent-layer" } }
  }]' >"$fixture/commit_pulls.json"
  run_verify_case "clean-squash-pr-success" "$squash_sha" 0 "Using exact-tree same-repo PR #581 ci.yml: https://example.invalid/runs/901"

  # 5. Fork PR is rejected even with matching tree and green run.
  jq '.[0].head.repo.full_name = "attacker/agent-layer"' <<<"$default_commit_pulls" >"$fixture/commit_pulls.json"
  run_verify_case "reject-fork-pr" "$clean_merge_sha" 1 ""

  # 6. Skewed merge (tree != PR head, main advanced) is rejected.
  jq -cn --arg merge "$skewed_merge_sha" --arg head "$pr_head_sha" '[{
    number: 582, merged_at: "2026-11-28T01:01:48Z", merge_commit_sha: $merge,
    base: { ref: "main", repo: { full_name: "conn-castle/agent-layer" } },
    head: { ref: "release/v9.8.7", sha: $head, repo: { full_name: "conn-castle/agent-layer" } }
  }]' >"$fixture/commit_pulls.json"
  run_verify_case "reject-skewed-merge" "$skewed_merge_sha" 1 ""

  # 7. Same tree after main moved (first parent not ancestor of PR head) is rejected.
  jq -cn --arg merge "$moved_same_tree_sha" --arg head "$pr_head_sha" '[{
    number: 583, merged_at: "2026-11-28T01:01:48Z", merge_commit_sha: $merge,
    base: { ref: "main", repo: { full_name: "conn-castle/agent-layer" } },
    head: { ref: "release/v9.8.7", sha: $head, repo: { full_name: "conn-castle/agent-layer" } }
  }]' >"$fixture/commit_pulls.json"
  run_verify_case "reject-main-advanced-before-merge" "$moved_same_tree_sha" 1 ""

  # 8. Ambiguous PR targeting non-main branch sharing same head_sha is rejected.
  printf '%s\n' "$default_commit_pulls" >"$fixture/commit_pulls.json"
  jq '. + [{number: 599, base: { ref: "other-branch", repo: { full_name: "conn-castle/agent-layer" } }}]' <<<"$default_head_pulls" >"$fixture/head_pulls.json"
  run_verify_case "reject-ambiguous-non-main-pr" "$clean_merge_sha" 1 ""

  # 9. PR CI run from a fork head_repository or failed conclusion is rejected.
  printf '%s\n' "$default_head_pulls" >"$fixture/head_pulls.json"
  jq '.workflow_runs[0].head_repository.full_name = "attacker/agent-layer"' <<<"$default_pr_runs" >"$fixture/pr_runs.json"
  run_verify_case "reject-fork-ci-run" "$clean_merge_sha" 1 ""

  jq '.workflow_runs[0].conclusion = "failure"' <<<"$default_pr_runs" >"$fixture/pr_runs.json"
  run_verify_case "reject-failed-pr-ci" "$clean_merge_sha" 1 ""

  rm -rf "$fixture"
}

run_release_workflow_security_tests() {
  section "Release Workflow Security Tests"

  local workflow="$ROOT_DIR/.github/workflows/release.yml"
  local ci_workflow="$ROOT_DIR/.github/workflows/ci.yml"
  if [[ ! -f "$workflow" ]]; then
    fail "release-security: release workflow is missing"
    return
  fi

  if [[ ! -f "$ROOT_DIR/.github/workflows/release-catalog-certification.yml" ]] &&
     ! grep -Eq 'release-catalog|benchmark readiness|public\.ecr\.aws|docker pull' "$workflow" "$ci_workflow" &&
     grep -q 'run: make benchmark-contract-check' "$ci_workflow"; then
    pass "release-security: core workflows exclude live catalog pulls and retain companion fixture contracts"
  else
    fail "release-security: live catalog access returned or companion contracts are missing"
  fi

  if grep -q 'cancel-in-progress: \${{ github\.event_name == '\''pull_request'\'' }}' "$ci_workflow" &&
     grep -q 'run: make release-vuln-check DIST_DIR=dist' "$ci_workflow"; then
    pass "ci-security: ci.yml cancels superseded PR runs and runs govulncheck on Linux"
  else
    fail "ci-security: ci.yml must cancel superseded PR runs and run govulncheck"
  fi

  if grep -q '^  workflow_dispatch:$' "$workflow" &&
     ! grep -q '^  push:$' "$workflow" &&
     grep -q 'default: true' <<<"$(sed -n '/      rehearsal:/,/^concurrency:/p' "$workflow")"; then
    pass "release-security: workflow_dispatch-only trigger with safe rehearsal default"
  else
    fail "release-security: release.yml must use workflow_dispatch only with rehearsal defaulting to true"
  fi

  local readiness_job build_job website_job publish_job
  readiness_job=$(release_workflow_job "$workflow" "release-readiness")
  build_job=$(release_workflow_job "$workflow" "build-release")
  website_job=$(release_workflow_job "$workflow" "build-website")
  publish_job=$(release_workflow_job "$workflow" "publish")

  if [[ -z "$readiness_job" || -z "$build_job" || -z "$website_job" || -z "$publish_job" ]]; then
    fail "release-security: one or more required jobs (release-readiness, build-release, build-website, publish) are missing"
    return
  fi

  local job_name job_body
  for job_name in release-readiness build-release build-website publish; do
    job_body=$(release_workflow_job "$workflow" "$job_name")
    if grep -q '^    environment: release$' <<<"$job_body" &&
       grep -q '^      - name: Verify authorized release actor and branch$' <<<"$job_body" &&
       ! grep -Eq '^[[:space:]]+contents: write$' <<<"$job_body"; then
      pass "release-security: $job_name uses release environment, actor guard, and least-privilege token"
    else
      fail "release-security: $job_name must use release environment, actor guard, and read-only GITHUB_TOKEN contents"
    fi
  done

  if grep -q '^    needs: release-readiness$' <<<"$build_job" &&
     grep -q '^    needs: release-readiness$' <<<"$website_job" &&
     grep -q '      - release-readiness' <<<"$publish_job" &&
     grep -q '      - build-release' <<<"$publish_job" &&
     grep -q '      - build-website' <<<"$publish_job"; then
    pass "release-security: build-release and build-website run in parallel after readiness and both gate publish"
  else
    fail "release-security: job dependency graph is invalid"
  fi

  if ! grep -q 'run: make ci' <<<"$build_job"; then
    pass "release-security: build-release omits redundant make ci"
  else
    fail "release-security: build-release must not re-run redundant make ci"
  fi

  if grep -q './scripts/verify-release-ci.sh' <<<"$readiness_job" &&
     ! grep -v '^[[:space:]]*#' "$ROOT_DIR/scripts/verify-release-ci.sh" | grep -q -- '--status'; then
    pass "release-security: readiness delegates exact-commit/exact-tree CI check without lagging --status"
  else
    fail "release-security: readiness must use verify-release-ci.sh without --status"
  fi

  local step
  for step in "Create annotated tag and publish release" "Checkout agent-layer-web for publication" \
              "Commit and push website update" "Verify release assets are available" "Create PR in tap repo"; do
    if grep -q "^        if: env.REHEARSAL != 'true'$" <<<"$(release_workflow_step "$publish_job" "$step")"; then
      pass "release-rehearsal: $step is disabled in rehearsal"
    else
      fail "release-rehearsal: $step must be disabled in rehearsal"
    fi
  done

  local certificate_step build_step scan_step
  certificate_step=$(release_workflow_step "$build_job" "Import Developer ID cert")
  build_step=$(release_workflow_step "$build_job" "Build release artifacts")
  scan_step=$(release_workflow_step "$build_job" "Scan release binaries for known vulnerable symbols")

  if grep -Eq '^        uses: Apple-Actions/import-codesign-certs@[0-9a-f]{40}([[:space:]]+#.*)?$' <<<"$certificate_step" &&
     grep -Eq '^          p12-file-base64: \$\{\{ secrets\.MACOS_CERT_P12_BASE64 \}\}$' <<<"$certificate_step" &&
     grep -Eq '^          p12-password: \$\{\{ secrets\.MACOS_CERT_P12_PASSWORD \}\}$' <<<"$certificate_step"; then
    pass "release-security: certificate import is commit-pinned and secret-backed"
  else
    fail "release-security: certificate import must be commit-pinned and secret-backed"
  fi

  if grep -Eq '^          AL_REQUIRE_CODESIGN: "1"$' <<<"$build_step" &&
     grep -Eq '^          AL_CODESIGN_IDENTITY: ".+"$' <<<"$build_step" &&
     grep -Eq '^        run: \./scripts/build-release\.sh$' <<<"$build_step"; then
    pass "release-security: release artifacts require code signing"
  else
    fail "release-security: release artifacts must require a signing identity"
  fi

  local validate_line build_line scan_line notarize_line upload_line
  validate_line=$(release_step_line "$build_job" "Validate stable release tag format")
  build_line=$(release_step_line "$build_job" "Build release artifacts")
  scan_line=$(release_step_line "$build_job" "Scan release binaries for known vulnerable symbols")
  notarize_line=$(release_step_line "$build_job" "Notarize darwin binaries")
  upload_line=$(release_step_line "$build_job" "Upload dist artifacts for downstream jobs")

  if [[ -n "$validate_line" && -n "$build_line" && -n "$scan_line" && -n "$notarize_line" && -n "$upload_line" ]] &&
     (( validate_line < build_line && build_line < scan_line && scan_line < notarize_line && notarize_line < upload_line )) &&
     grep -Eq '^[[:space:]]+run: make release-vuln-check DIST_DIR=dist[[:space:]]*$' <<<"$scan_step" &&
     grep -q 'retention-days: 1' <<<"$(release_workflow_step "$build_job" "Upload dist artifacts for downstream jobs")"; then
    pass "release-security: vulnerability scan gates notarization and 1-day artifact upload"
  else
    fail "release-security: build/scan/notarize/upload ordering or artifact retention is invalid"
  fi

  local verify_line formula_line tag_publish_line
  verify_line=$(release_step_line "$publish_job" "Verify dist integrity and Linux smoke test")
  formula_line=$(release_step_line "$publish_job" "Validate rendered formula syntax and checksums")
  tag_publish_line=$(release_step_line "$publish_job" "Create annotated tag and publish release")

  if [[ -n "$verify_line" && -n "$formula_line" && -n "$tag_publish_line" ]] &&
     (( verify_line < tag_publish_line && formula_line < tag_publish_line )); then
    pass "release-security: Linux smoke test and formula validation precede tag creation and publication"
  else
    fail "release-security: Linux smoke test and formula validation must precede tag creation and publication"
  fi

  run_release_actor_guard_tests "$workflow"
  run_verify_release_ci_tests
}

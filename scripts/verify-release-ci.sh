#!/usr/bin/env bash
# Verify that full CI (ci.yml) passed for a release commit on main.
#
# Accepts either:
#   1. A successful push-event ci.yml run on main for the exact commit SHA, OR
#   2. A clean-merge exact-tree same-repo PR whose pull_request ci.yml run passed:
#      - The commit's merged PR has base=main and same-repo head (never a fork),
#      - Every PR associated with the PR head SHA also targets main,
#      - git tree(commit) == git tree(pr_head_sha) (computed locally from git),
#      - commit^1 is an ancestor of pr_head_sha (main did not advance past PR),
#      - A successful pull_request ci.yml run exists for exact pr_head_sha,
#        pr_head_branch, and same-repo head_repository.
#
# Usage:
#   scripts/verify-release-ci.sh --commit <sha> --repo <owner/name> [--wait-active] [--push-wait-seconds N]
set -euo pipefail

die() {
  echo "ERROR: $*" >&2
  exit 1
}

commit=""
repo=""
wait_active=0
push_wait_seconds=0
push_poll_seconds=5

while [[ $# -gt 0 ]]; do
  case "$1" in
    --commit)
      [[ $# -ge 2 ]] || die "--commit requires a value"
      commit="$2"
      shift 2
      ;;
    --repo)
      [[ $# -ge 2 ]] || die "--repo requires a value"
      repo="$2"
      shift 2
      ;;
    --wait-active)
      wait_active=1
      shift
      ;;
    --push-wait-seconds)
      [[ $# -ge 2 ]] || die "--push-wait-seconds requires a value"
      push_wait_seconds="$2"
      shift 2
      ;;
    --push-poll-seconds)
      [[ $# -ge 2 ]] || die "--push-poll-seconds requires a value"
      push_poll_seconds="$2"
      shift 2
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

[[ -n "${commit}" ]] || die "--commit is required"
[[ -n "${repo}" ]] || die "--repo is required"

command -v git >/dev/null 2>&1 || die "git is required"
command -v gh >/dev/null 2>&1 || die "gh is required"
command -v jq >/dev/null 2>&1 || die "jq is required"

# Query push-event ci.yml runs on main for exact commit without --status
# (avoids GitHub's lagging server-side status index).
# Sets push_state to "success", "active", "completed", or "none",
# push_id to databaseId, and push_url to run URL.
check_push_ci() {
  local raw
  raw="$(
    gh run list \
      --workflow ci.yml \
      --event push \
      --branch main \
      --commit "${commit}" \
      --limit 100 \
      --json databaseId,status,conclusion,event,headBranch,headSha,url \
      --jq "
        map(select(.event == \"push\" and .headBranch == \"main\" and .headSha == \"${commit}\"))
        | ([.[] | select(.conclusion == \"success\")][0]) as \$success
        | ([.[] | select(.status != \"completed\")][0]) as \$active
        | if \$success != null then \"success|\(\$success.databaseId)|\(\$success.url)\"
          elif \$active != null then \"active|\(\$active.databaseId)|\(\$active.url)\"
          elif length > 0 then \"completed||\"
          else \"none||\" end"
  )"
  IFS='|' read -r push_state push_id push_url <<<"${raw}"
}

# Check clean-merge exact-tree same-repo PR CI for commit.
# Sets pr_state to "success", "active", or "none", pr_number, pr_run_id, pr_run_url.
check_clean_merge_pr_ci() {
  pr_state="none"
  pr_number=""
  pr_run_id=""
  pr_run_url=""

  # Commit must have a parent on main (cannot verify first-parent ancestry on root commit).
  local first_parent release_tree
  first_parent="$(git rev-parse --verify "${commit}^1" 2>/dev/null || true)"
  [[ -n "${first_parent}" ]] || return 0
  release_tree="$(git rev-parse --verify "${commit}^{tree}" 2>/dev/null || true)"
  [[ -n "${release_tree}" ]] || return 0

  local pulls_json candidate_lines
  pulls_json="$(gh api "repos/${repo}/commits/${commit}/pulls" 2>/dev/null || echo '[]')"
  candidate_lines="$(
    jq -r --arg commit "${commit}" --arg repo "${repo}" '
      if type == "array" then
        .[]
        | select(
            .merged_at != null
            and .merge_commit_sha == $commit
            and .base.ref == "main"
            and .base.repo.full_name == $repo
            and .head.repo.full_name == $repo
          )
        | "\(.number)\t\(.head.sha)\t\(.head.ref)"
      else empty end
    ' <<<"${pulls_json}"
  )"
  [[ -n "${candidate_lines}" ]] || return 0

  local cand_num cand_sha cand_branch pr_tree head_pulls_json non_main_count runs_json run_match
  local r_state r_id r_url
  while IFS=$'\t' read -r cand_num cand_sha cand_branch; do
    [[ -n "${cand_num}" && -n "${cand_sha}" && -n "${cand_branch}" ]] || continue

    # Ensure PR head commit is locally available so tree and ancestry are computed from git.
    if ! git cat-file -e "${cand_sha}^{commit}" 2>/dev/null; then
      git fetch --no-tags --quiet origin "${cand_sha}" 2>/dev/null || continue
    fi

    pr_tree="$(git rev-parse --verify "${cand_sha}^{tree}" 2>/dev/null || true)"
    [[ -n "${pr_tree}" && "${release_tree}" == "${pr_tree}" ]] || continue

    # Verify main did not advance past the PR head before merge.
    git merge-base --is-ancestor "${first_parent}" "${cand_sha}" 2>/dev/null || continue

    # Verify no PR sharing this exact head SHA targeted a branch other than main.
    head_pulls_json="$(gh api "repos/${repo}/commits/${cand_sha}/pulls" 2>/dev/null || echo '[]')"
    non_main_count="$(
      jq -r --arg repo "${repo}" '
        if type == "array" then
          [.[] | select(.base.repo.full_name == $repo and .base.ref != "main")] | length
        else 1 end
      ' <<<"${head_pulls_json}"
    )"
    [[ "${non_main_count}" == "0" ]] || continue

    # Query pull_request ci.yml runs without server-side status filter.
    runs_json="$(gh api "repos/${repo}/actions/workflows/ci.yml/runs?event=pull_request&head_sha=${cand_sha}&per_page=100")"
    run_match="$(
      jq -r --arg head_sha "${cand_sha}" --arg head_branch "${cand_branch}" \
        --arg repo "${repo}" --argjson pr_num "${cand_num}" '
        (.workflow_runs // [])
        | map(select(
            .event == "pull_request"
            and .head_sha == $head_sha
            and .head_branch == $head_branch
            and .head_repository.full_name == $repo
            and ((.pull_requests | length == 0) or ([.pull_requests[].number] | index($pr_num) != null))
          ))
        | ([.[] | select(.conclusion == "success")][0]) as $success
        | ([.[] | select(.status != "completed")][0]) as $active
        | if $success != null then "success|\($success.id)|\($success.html_url)"
          elif $active != null then "active|\($active.id)|\($active.html_url)"
          else "none||" end
      ' <<<"${runs_json}"
    )"
    IFS='|' read -r r_state r_id r_url <<<"${run_match}"
    if [[ "${r_state}" == "success" ]]; then
      pr_state="success"
      pr_number="${cand_num}"
      pr_run_id="${r_id}"
      pr_run_url="${r_url}"
      return 0
    elif [[ "${r_state}" == "active" && "${pr_state}" == "none" ]]; then
      pr_state="active"
      pr_number="${cand_num}"
      pr_run_id="${r_id}"
      pr_run_url="${r_url}"
    fi
  done <<<"${candidate_lines}"
}

push_state=""
push_id=""
push_url=""
check_push_ci
if [[ "${push_state}" == "success" ]]; then
  echo "Using exact-commit push ci.yml: ${push_url}"
  exit 0
fi

pr_state=""
pr_number=""
pr_run_id=""
pr_run_url=""
check_clean_merge_pr_ci
if [[ "${pr_state}" == "success" ]]; then
  echo "Using exact-tree same-repo PR #${pr_number} ci.yml: ${pr_run_url}"
  exit 0
fi

if [[ "${wait_active}" == "1" ]]; then
  if [[ "${push_state}" == "none" && "${push_wait_seconds}" -gt 0 ]]; then
    echo "Waiting up to ${push_wait_seconds}s for push-triggered ci.yml run for ${commit}."
    waited=0
    while [[ "${waited}" -lt "${push_wait_seconds}" ]]; do
      sleep "${push_poll_seconds}"
      waited=$((waited + push_poll_seconds))
      check_push_ci
      if [[ "${push_state}" == "success" ]]; then
        echo "Using exact-commit push ci.yml: ${push_url}"
        exit 0
      fi
      [[ "${push_state}" == "none" ]] || break
    done
  fi

  if [[ "${push_state}" == "active" ]]; then
    echo "Waiting for full push CI run ${push_id} for ${commit}."
    gh run watch "${push_id}" --exit-status
    conclusion="$(gh run view "${push_id}" --json conclusion --jq '.conclusion')"
    [[ "${conclusion}" == "success" ]] || die "CI verification ${push_id} concluded ${conclusion}"
    echo "Using exact-commit push ci.yml: ${push_url}"
    exit 0
  fi
fi

die "No successful push-event ci.yml run on main (nor clean-merge exact-tree same-repo PR ci.yml run) exists for commit ${commit} (push=${push_state}, pr=${pr_state})."

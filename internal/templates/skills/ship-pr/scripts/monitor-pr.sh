#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
Usage: monitor-pr.sh --pr <number> --state-file <path> [options]

Poll a GitHub PR until an actionable ship-pr state is reached, then print one
JSON object to stdout and exit.

Options:
  --minimum-ready-seconds <seconds>      Minimum age of the current head before
                                         returning ready. Default: 300.
  --interval <seconds>                   Poll interval. Default: 10.
  --timeout-seconds <seconds>            Wall timeout. Default: 1800.

Actions:
  ci_failed          One or more PR checks failed.
  feedback_changed   External PR feedback changed since the last observation.
  merge_conflict     PR has a merge conflict with the base branch.
  pr_not_open        PR is no longer open.
  ready              Checks are terminal and the minimum ready window elapsed.
  timeout            No actionable state before the wall timeout elapsed.
EOF
}

require_command() {
  local name="$1"
  if ! command -v "$name" >/dev/null 2>&1; then
    printf 'monitor-pr: required command not found: %s\n' "$name" >&2
    exit 2
  fi
}

_GH_RETRY_TRANSIENT_PATTERN='timeout|timed out|temporary failure|connection reset|connection refused|EOF|HTTP 5[0-9][0-9]|could not resolve|i/o timeout|TLS handshake|operation timed out|no route to host|network is unreachable'

gh_retry() {
  local out_var="$1"
  shift
  local max_attempts=5 attempt=1 delay=5
  local out="" rc=0
  local stderr_file
  stderr_file="$(mktemp)"

  while true; do
    if out="$("$@" 2>"$stderr_file")"; then
      printf -v "$out_var" '%s' "$out"
      rc=0
      break
    fi

    rc=$?
    if ((attempt >= max_attempts)) || ! grep -qiE "$_GH_RETRY_TRANSIENT_PATTERN" "$stderr_file"; then
      break
    fi

    printf 'monitor-pr: gh call attempt %d/%d failed (transient): retrying in %ds\n' "$attempt" "$max_attempts" "$delay" >&2
    : >"$stderr_file"
    sleep "$delay"
    attempt=$((attempt + 1))
    delay=$((delay * 2))
  done

  cat "$stderr_file" >&2
  rm -f "$stderr_file"
  return "$rc"
}

fetch_checks_json() {
  local out_var="$1"
  local pr="$2"
  local max_attempts=5 attempt=1 delay=5
  local out="" rc=0
  local stderr_file
  stderr_file="$(mktemp)"

  while true; do
    if out="$(gh pr checks "$pr" --json name,bucket,state,workflow,link 2>"$stderr_file")"; then
      printf -v "$out_var" '%s' "$out"
      rm -f "$stderr_file"
      return 0
    fi

    rc=$?
    if [[ "$rc" -eq 8 ]] || grep -qiE 'no checks|checks? pending' "$stderr_file"; then
      printf -v "$out_var" '%s' "${out:-[]}"
      rm -f "$stderr_file"
      return 0
    fi

    if ((attempt >= max_attempts)) || ! grep -qiE "$_GH_RETRY_TRANSIENT_PATTERN" "$stderr_file"; then
      cat "$stderr_file" >&2
      rm -f "$stderr_file"
      return "$rc"
    fi

    printf 'monitor-pr: gh checks attempt %d/%d failed (transient): retrying in %ds\n' "$attempt" "$max_attempts" "$delay" >&2
    : >"$stderr_file"
    sleep "$delay"
    attempt=$((attempt + 1))
    delay=$((delay * 2))
  done
}

pr_number=""
state_file=""
minimum_ready_seconds="300"
interval_seconds="10"
timeout_seconds="1800"

option_value() {
  local option="$1"
  local value="${2:-}"

  if [[ -z "$value" || "$value" == --* ]]; then
    printf 'monitor-pr: %s requires a value\n' "$option" >&2
    usage
    exit 2
  fi
  printf '%s' "$value"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --pr)
      pr_number="$(option_value "$1" "${2:-}")"
      shift 2
      ;;
    --state-file)
      state_file="$(option_value "$1" "${2:-}")"
      shift 2
      ;;
    --minimum-ready-seconds)
      minimum_ready_seconds="$(option_value "$1" "${2:-}")"
      shift 2
      ;;
    --interval)
      interval_seconds="$(option_value "$1" "${2:-}")"
      shift 2
      ;;
    --timeout-seconds)
      timeout_seconds="$(option_value "$1" "${2:-}")"
      shift 2
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      printf 'monitor-pr: unknown argument: %s\n' "$1" >&2
      usage
      exit 2
      ;;
  esac
done

if [[ -z "$pr_number" || -z "$state_file" ]]; then
  usage
  exit 2
fi

if ! [[ "$pr_number" =~ ^[0-9]+$ ]]; then
  printf 'monitor-pr: --pr must be a numeric PR number\n' >&2
  exit 2
fi

if ! [[ "$minimum_ready_seconds" =~ ^[0-9]+$ ]]; then
  printf 'monitor-pr: --minimum-ready-seconds must be a non-negative integer\n' >&2
  exit 2
fi

if ! [[ "$interval_seconds" =~ ^[0-9]+$ ]] || [[ "$interval_seconds" -lt 1 ]]; then
  printf 'monitor-pr: --interval must be a positive integer\n' >&2
  exit 2
fi

if ! [[ "$timeout_seconds" =~ ^[0-9]+$ ]]; then
  printf 'monitor-pr: --timeout-seconds must be a non-negative integer\n' >&2
  exit 2
fi

require_command gh
require_command git
require_command jq

state_dir="$(dirname "$state_file")"
mkdir -p "$state_dir"
state_dir="$(cd "$state_dir" && pwd -P)"
state_file="$state_dir/$(basename "$state_file")"

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

if [[ -f "$state_file" ]] && ! jq -e 'type == "object"' "$state_file" >/dev/null; then
  printf 'monitor-pr: invalid JSON state file: %s\n' "$state_file" >&2
  exit 2
fi

viewer_login=""
pr_json=""
issue_comments_pages_json=""
review_comments_pages_json=""
review_bodies_pages_json=""
checks_json=""

gh_retry viewer_login gh api user --jq .login
script_start_epoch="$(date +%s)"

previous_feedback_fingerprint() {
  if [[ -f "$state_file" ]]; then
    jq -r '.feedback_fingerprint // ""' "$state_file"
  else
    printf ''
  fi
}

write_state() {
  local head_sha="$1"
  local feedback_fingerprint="$2"
  local statuses_json="$3"
  local feedback_count="$4"
  local unresolved_feedback_count="$5"
  local tmp_file

  tmp_file="$(mktemp "${state_file}.tmp.XXXXXX")"
  if ! jq -n \
    --arg head_sha "$head_sha" \
    --arg feedback_fingerprint "$feedback_fingerprint" \
    --arg updated_at "$(date -u +"%Y-%m-%dT%H:%M:%SZ")" \
    --argjson statuses "$statuses_json" \
    --argjson feedback_count "$feedback_count" \
    --argjson unresolved_feedback_count "$unresolved_feedback_count" \
    --argjson minimum_ready_seconds "$minimum_ready_seconds" \
    --argjson current_head_first_seen_epoch "$current_head_first_seen_epoch" \
    --argjson elapsed_minimum_ready_seconds "$elapsed_minimum_ready_seconds" \
    --argjson remaining_minimum_ready_seconds "$remaining_minimum_ready_seconds" \
    '{
      head_sha: $head_sha,
      feedback_fingerprint: $feedback_fingerprint,
      feedback_count: $feedback_count,
      unresolved_feedback_count: $unresolved_feedback_count,
      statuses: $statuses,
      minimum_ready_seconds: $minimum_ready_seconds,
      current_head_first_seen_epoch: $current_head_first_seen_epoch,
      elapsed_minimum_ready_seconds: $elapsed_minimum_ready_seconds,
      remaining_minimum_ready_seconds: $remaining_minimum_ready_seconds,
      updated_at: $updated_at
    }' >"$tmp_file"; then
    rm -f "$tmp_file"
    return 1
  fi
  if ! mv "$tmp_file" "$state_file"; then
    rm -f "$tmp_file"
    return 1
  fi
}

emit_and_exit() {
  local action="$1"
  local reason="$2"

  write_state "$head_sha" "$feedback_fingerprint" "$statuses_json" "$feedback_count" "$unresolved_feedback_count"

  jq -n \
    --arg action "$action" \
    --arg reason "$reason" \
    --arg pr_number "$pr_number" \
    --arg pr_url "$pr_url" \
    --arg head_sha "$head_sha" \
    --argjson statuses "$statuses_json" \
    --argjson failed_statuses "$failed_statuses_json" \
    --argjson pending_statuses "$pending_statuses_json" \
    --argjson feedback_count "$feedback_count" \
    --argjson unresolved_feedback_count "$unresolved_feedback_count" \
    --argjson minimum_ready_seconds "$minimum_ready_seconds" \
    --argjson elapsed_minimum_ready_seconds "$elapsed_minimum_ready_seconds" \
    --argjson remaining_minimum_ready_seconds "$remaining_minimum_ready_seconds" \
    '{
      action: $action,
      reason: $reason,
      pr_number: ($pr_number | tonumber),
      pr_url: $pr_url,
      head_sha: $head_sha,
      statuses: $statuses,
      failed_statuses: $failed_statuses,
      pending_statuses: $pending_statuses,
      feedback_count: $feedback_count,
      unresolved_feedback_count: $unresolved_feedback_count,
      minimum_ready_seconds: $minimum_ready_seconds,
      elapsed_minimum_ready_seconds: $elapsed_minimum_ready_seconds,
      remaining_minimum_ready_seconds: $remaining_minimum_ready_seconds
    }'

  exit 0
}

while true; do
  gh_retry pr_json gh pr view "$pr_number" --json headRefOid,url,mergeable,mergeStateStatus,state,isDraft
  gh_retry issue_comments_pages_json gh api "repos/{owner}/{repo}/issues/$pr_number/comments" --paginate --slurp
  gh_retry review_comments_pages_json gh api "repos/{owner}/{repo}/pulls/$pr_number/comments" --paginate --slurp
  gh_retry review_bodies_pages_json gh api "repos/{owner}/{repo}/pulls/$pr_number/reviews" --paginate --slurp
  fetch_checks_json checks_json "$pr_number"

  head_sha="$(jq -r '.headRefOid' <<<"$pr_json")"
  pr_url="$(jq -r '.url' <<<"$pr_json")"
  pr_state="$(jq -r '.state // ""' <<<"$pr_json")"
  mergeable="$(jq -r '.mergeable // ""' <<<"$pr_json")"
  merge_state_status="$(jq -r '.mergeStateStatus // ""' <<<"$pr_json")"
  is_draft="$(jq -r '.isDraft // false' <<<"$pr_json")"

  now_epoch="$(date +%s)"
  previous_head_sha=""
  previous_head_first_seen_epoch=""
  if [[ -f "$state_file" ]]; then
    previous_head_sha="$(jq -r '.head_sha // ""' "$state_file")"
    previous_head_first_seen_epoch="$(jq -r '(.current_head_first_seen_epoch // "") | tostring' "$state_file")"
  fi

  if [[ "$previous_head_sha" == "$head_sha" && "$previous_head_first_seen_epoch" =~ ^[0-9]+$ && "$previous_head_first_seen_epoch" -gt 0 ]]; then
    current_head_first_seen_epoch="$previous_head_first_seen_epoch"
  else
    current_head_first_seen_epoch="$now_epoch"
  fi

  elapsed_minimum_ready_seconds=$((now_epoch - current_head_first_seen_epoch))
  if [[ "$elapsed_minimum_ready_seconds" -lt 0 ]]; then
    elapsed_minimum_ready_seconds="0"
  fi

  if [[ "$elapsed_minimum_ready_seconds" -ge "$minimum_ready_seconds" ]]; then
    remaining_minimum_ready_seconds="0"
    minimum_ready_elapsed="true"
  else
    remaining_minimum_ready_seconds=$((minimum_ready_seconds - elapsed_minimum_ready_seconds))
    minimum_ready_elapsed="false"
  fi

  feedback_json="$(
    jq -n -c \
      --arg viewer "$viewer_login" \
      --argjson issue_pages "$issue_comments_pages_json" \
      --argjson review_comment_pages "$review_comments_pages_json" \
      --argjson review_body_pages "$review_bodies_pages_json" '
        def flatten_pages:
          if type != "array" then []
          elif length == 0 then []
          elif (.[0] | type) == "array" then add // []
          else .
          end;
        def text_present:
          ((.body // "") | gsub("\\s"; "") | length) > 0;
        def ignored_author($login):
          ($login | test("^(github-actions(\\[bot\\])?|codecov\\[bot\\]|dependabot\\[bot\\]|renovate\\[bot\\])$"; "i"));
        def reviewer_bot_author($login):
          ($login | test("^(coderabbitai|coderabbit-ai|copilot-pull-request-reviewer|github-copilot|gemini-code-assist|chatgpt-codex-connector)(\\[bot\\])?$"; "i"));
        def coderabbit_auto_status:
          ((.body // "") | test("auto-generated comment: (summarize|skip review|review_status) by coderabbit\\.ai|coderabbit-review-completion-marker"; "i"));
        (($issue_pages | flatten_pages) as $issue_comments
        | ($review_comment_pages | flatten_pages) as $review_comments
        | ($review_body_pages | flatten_pages) as $review_bodies
        | ([
            $issue_comments[]?
            | {
                kind: "issue_comment",
                id: (.id | tostring),
                author: (.user.login // ""),
                updated_at: (.updated_at // .created_at // ""),
                body: (.body // "")
              }
            | select(.author != $viewer and (ignored_author(.author) | not) and text_present and (coderabbit_auto_status | not))
          ] + [
            $review_comments[]?
            | {
                kind: "review_comment",
                id: (.id | tostring),
                author: (.user.login // ""),
                in_reply_to_id: (.in_reply_to_id // null),
                updated_at: (.updated_at // .created_at // ""),
                body: (.body // "")
              }
            | select(
                .author != $viewer
                and (ignored_author(.author) | not)
                and text_present
                and (coderabbit_auto_status | not)
                and (((.in_reply_to_id != null) and reviewer_bot_author(.author)) | not)
              )
          ] + [
            $review_bodies[]?
            | {
                kind: "review_body",
                id: ((.id // ((.user.login // "") + ":" + (.submitted_at // "") + ":" + (.state // ""))) | tostring),
                author: (.user.login // ""),
                updated_at: (.submitted_at // ""),
                body: (.body // "")
              }
            | select(.author != $viewer and (ignored_author(.author) | not) and text_present and (coderabbit_auto_status | not))
          ])
          | sort_by(.kind, .id, .updated_at, .author))
      '
  )"
  feedback_count="$(jq 'length' <<<"$feedback_json")"
  feedback_fingerprint="$(jq -c '[.[] | {kind, id, author, updated_at, body}]' <<<"$feedback_json" | git hash-object --stdin)"

  viewer_latest_ts="$(
    jq -n -r \
      --arg viewer "$viewer_login" \
      --argjson issue_pages "$issue_comments_pages_json" \
      --argjson review_comment_pages "$review_comments_pages_json" \
      --argjson review_body_pages "$review_bodies_pages_json" '
        def flatten_pages:
          if type != "array" then []
          elif length == 0 then []
          elif (.[0] | type) == "array" then add // []
          else .
          end;
        (($issue_pages | flatten_pages) as $issue_comments
        | ($review_comment_pages | flatten_pages) as $review_comments
        | ($review_body_pages | flatten_pages) as $review_bodies
        | [
            ($issue_comments[]? | select(.user.login == $viewer) | (.updated_at // .created_at // "")),
            ($review_comments[]? | select(.user.login == $viewer) | (.updated_at // .created_at // "")),
            ($review_bodies[]? | select(.user.login == $viewer) | (.submitted_at // ""))
          ]
          | map(select(. != ""))
          | sort
          | last // "")
      '
  )"
  unresolved_feedback_count="$(jq --arg vts "$viewer_latest_ts" '[.[] | select(.updated_at > $vts)] | length' <<<"$feedback_json")"

  statuses_json="$(
    jq -n -c --argjson checks "$checks_json" '
      [
        $checks[]?
        | (.state // "" | ascii_upcase) as $state
        | (.bucket // "" | ascii_downcase) as $bucket
        | {
            name: (.name // ""),
            result:
              (if $state == "SUCCESS" or $bucket == "pass" then "success"
               elif $bucket == "skipping" then "skipped"
               elif ($state | test("^(FAILURE|FAILED|ERROR|CANCELLED|CANCELED|TIMED_OUT|ACTION_REQUIRED)$")) or ($bucket | test("^(fail|cancel)$")) then "failed"
               else "pending"
               end),
            state: (.state // null),
            bucket: (.bucket // null),
            workflow: (.workflow // null),
            link: (.link // null)
          }
      ]
    '
  )"

  failed_statuses_json="$(jq -c '[.[] | select(.result == "failed")]' <<<"$statuses_json")"
  pending_statuses_json="$(jq -c '[.[] | select(.result == "pending")]' <<<"$statuses_json")"
  failed_count="$(jq 'length' <<<"$failed_statuses_json")"
  pending_count="$(jq 'length' <<<"$pending_statuses_json")"
  prior_feedback_fingerprint="$(previous_feedback_fingerprint)"
  feedback_changed="false"
  if [[ "$feedback_count" -gt 0 && "$feedback_fingerprint" != "$prior_feedback_fingerprint" ]]; then
    feedback_changed="true"
  fi

  if [[ "$pr_state" != "OPEN" ]]; then
    emit_and_exit "pr_not_open" "PR state is $pr_state."
  fi

  if [[ "$mergeable" == "CONFLICTING" ]]; then
    emit_and_exit "merge_conflict" "PR has a merge conflict with the base branch (mergeStateStatus=$merge_state_status)."
  fi

  if [[ "$feedback_changed" == "true" ]]; then
    emit_and_exit "feedback_changed" "External PR feedback changed since the last observation."
  fi

  if [[ "$failed_count" -gt 0 ]]; then
    emit_and_exit "ci_failed" "One or more PR checks failed."
  fi

  if [[ "$pending_count" -eq 0 && "$minimum_ready_elapsed" == "true" && "$is_draft" == "false" ]]; then
    emit_and_exit "ready" "Checks are terminal and the minimum ready window elapsed."
  fi

  write_state "$head_sha" "$feedback_fingerprint" "$statuses_json" "$feedback_count" "$unresolved_feedback_count"

  if [[ "$timeout_seconds" -gt 0 ]]; then
    elapsed_wall_seconds=$((now_epoch - script_start_epoch))
    if [[ "$elapsed_wall_seconds" -ge "$timeout_seconds" ]]; then
      jq -n \
        --arg action "timeout" \
        --arg reason "No actionable PR state before timeout." \
        --arg pr_number "$pr_number" \
        --arg pr_url "$pr_url" \
        --arg head_sha "$head_sha" \
        --argjson statuses "$statuses_json" \
        --argjson failed_statuses "$failed_statuses_json" \
        --argjson pending_statuses "$pending_statuses_json" \
        --argjson feedback_count "$feedback_count" \
        --argjson unresolved_feedback_count "$unresolved_feedback_count" \
        --argjson minimum_ready_seconds "$minimum_ready_seconds" \
        --argjson elapsed_minimum_ready_seconds "$elapsed_minimum_ready_seconds" \
        --argjson remaining_minimum_ready_seconds "$remaining_minimum_ready_seconds" \
        '{
          action: $action,
          reason: $reason,
          pr_number: ($pr_number | tonumber),
          pr_url: $pr_url,
          head_sha: $head_sha,
          statuses: $statuses,
          failed_statuses: $failed_statuses,
          pending_statuses: $pending_statuses,
          feedback_count: $feedback_count,
          unresolved_feedback_count: $unresolved_feedback_count,
          minimum_ready_seconds: $minimum_ready_seconds,
          elapsed_minimum_ready_seconds: $elapsed_minimum_ready_seconds,
          remaining_minimum_ready_seconds: $remaining_minimum_ready_seconds
        }'
      exit 0
    fi
  fi

  printf 'monitor-pr: PR #%s head=%s waiting; statuses=%s feedback=%s unresolved=%s minimum_ready_remaining=%ss\n' \
    "$pr_number" \
    "${head_sha:0:12}" \
    "$(jq -c '[.[] | {name, result}]' <<<"$statuses_json")" \
    "$feedback_count" \
    "$unresolved_feedback_count" \
    "$remaining_minimum_ready_seconds" >&2
  sleep_seconds="$interval_seconds"
  if [[ "$timeout_seconds" -gt 0 ]]; then
    remaining_timeout_seconds=$((timeout_seconds - elapsed_wall_seconds))
    if [[ "$remaining_timeout_seconds" -lt "$sleep_seconds" ]]; then
      sleep_seconds="$remaining_timeout_seconds"
    fi
  fi
  sleep "$sleep_seconds"
done

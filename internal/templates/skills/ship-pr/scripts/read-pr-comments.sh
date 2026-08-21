#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
Usage: read-pr-comments.sh --repo <owner/name> --pr <number>

Fetch every GitHub pull-request conversation comment, review summary, inline
review comment, and inline reply, including review-thread resolved/outdated
state, and print readable Markdown. The command writes no files and stores no
state.
EOF
}

repo=""
pr_number=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo)
      if [[ $# -lt 2 ]]; then
        printf 'read-pr-comments: --repo requires a value\n' >&2
        exit 2
      fi
      repo="${2:-}"
      shift 2
      ;;
    --pr)
      if [[ $# -lt 2 ]]; then
        printf 'read-pr-comments: --pr requires a value\n' >&2
        exit 2
      fi
      pr_number="${2:-}"
      shift 2
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      printf 'read-pr-comments: unknown argument: %s\n' "$1" >&2
      usage
      exit 2
      ;;
  esac
done

if [[ ! "$repo" =~ ^[^/]+/[^/]+$ ]]; then
  printf 'read-pr-comments: --repo must be in owner/name form\n' >&2
  exit 2
fi
if [[ ! "$pr_number" =~ ^[0-9]+$ ]]; then
  printf 'read-pr-comments: --pr must be a numeric PR number\n' >&2
  exit 2
fi
for command in gh jq; do
  if ! command -v "$command" >/dev/null 2>&1; then
    printf 'read-pr-comments: required command not found: %s\n' "$command" >&2
    exit 2
  fi
done

owner="${repo%%/*}"
name="${repo##*/}"

quote_body() {
  local body="$1"
  if [[ -z "$body" ]]; then
    printf '> (empty)\n'
    return
  fi
  printf '%s\n' "$body" | sed 's/^/> /'
}

print_item() {
  local kind="$1"
  local author="$2"
  local id="$3"
  local url="$4"
  local state="$5"
  local reply_to="$6"
  local thread_state="$7"
  local path="$8"
  local line="$9"
  local side="${10}"
  local review_id="${11}"
  local body="${12}"

  printf '## %s\n' "$kind"
  printf -- '- Author: %s\n' "$author"
  printf -- '- ID: %s\n' "$id"
  if [[ -n "$url" ]]; then
    printf -- '- URL: %s\n' "$url"
  fi
  if [[ -n "$state" ]]; then
    printf -- '- Review state: %s\n' "$state"
  fi
  if [[ -n "$reply_to" ]]; then
    printf -- '- Reply to: %s\n' "$reply_to"
  fi
  if [[ -n "$thread_state" ]]; then
    printf -- '- Thread: %s\n' "$thread_state"
  fi
  if [[ -n "$path" ]]; then
    printf -- '- Path: %s\n' "$path"
  fi
  if [[ -n "$line" ]]; then
    printf -- '- Line: %s\n' "$line"
  fi
  if [[ -n "$side" ]]; then
    printf -- '- Side: %s\n' "$side"
  fi
  if [[ -n "$review_id" ]]; then
    printf -- '- Review ID: %s\n' "$review_id"
  fi
  printf '\n'
  quote_body "$body"
  printf '\n'
}

issue_comments="$(
  gh api --paginate "repos/${repo}/issues/${pr_number}/comments?per_page=100" |
    jq -sc 'add // []'
)"
reviews="$(
  gh api --paginate "repos/${repo}/pulls/${pr_number}/reviews?per_page=100" |
    jq -sc 'add // []'
)"
inline_comments="$(
  gh api --paginate "repos/${repo}/pulls/${pr_number}/comments?per_page=100" |
    jq -sc 'add // []'
)"
thread_states="$(
  gh api graphql --paginate \
    -f owner="$owner" \
    -f name="$name" \
    -F number="$pr_number" \
    -f query='
query($owner: String!, $name: String!, $number: Int!, $endCursor: String) {
  repository(owner: $owner, name: $name) {
    pullRequest(number: $number) {
      reviewThreads(first: 100, after: $endCursor) {
        pageInfo { hasNextPage endCursor }
        nodes {
          isResolved
          isOutdated
          comments(first: 1) {
            nodes { databaseId }
          }
        }
      }
    }
  }
}' | jq -sc '
  [.[] | .data.repository.pullRequest.reviewThreads.nodes // []]
  | add // []
  | map(select(.comments.nodes[0].databaseId != null) | {
      key: (.comments.nodes[0].databaseId | tostring),
      value: "resolved=\(.isResolved); outdated=\(.isOutdated)"
    })
  | from_entries
'
)"

printf '# PR comments for %s #%s\n\n' "$repo" "$pr_number"

jq -c '.[]' <<<"$issue_comments" | while IFS= read -r item; do
  print_item \
    "conversation" \
    "$(jq -r '.user.login // "(deleted)"' <<<"$item")" \
    "$(jq -r '.id' <<<"$item")" \
    "$(jq -r '.html_url // empty' <<<"$item")" \
    "" \
    "" \
    "" \
    "" \
    "" \
    "" \
    "" \
    "$(jq -r '.body // empty' <<<"$item")"
done

jq -c '.[]' <<<"$reviews" | while IFS= read -r item; do
  print_item \
    "review" \
    "$(jq -r '.user.login // "(deleted)"' <<<"$item")" \
    "$(jq -r '.id' <<<"$item")" \
    "$(jq -r '.html_url // empty' <<<"$item")" \
    "$(jq -r '.state // empty' <<<"$item")" \
    "" \
    "" \
    "" \
    "" \
    "" \
    "" \
    "$(jq -r '.body // empty' <<<"$item")"
done

jq -c --argjson states "$thread_states" '
  (map(select(.in_reply_to_id != null) | {(.id | tostring): (.in_reply_to_id | tostring)}) | add // {}) as $parents
  | def root($id):
      if ($parents[$id] // null) == null then $id else root($parents[$id]) end;
  .[]
  | . + {
      kind: (if .in_reply_to_id then "inline-reply" else "inline" end),
      reply_to: (if .in_reply_to_id then (.in_reply_to_id | tostring) else "" end),
      thread_state: ($states[root(.id | tostring)] // "")
    }
' <<<"$inline_comments" | while IFS= read -r item; do
  print_item \
    "$(jq -r '.kind' <<<"$item")" \
    "$(jq -r '.user.login // "(deleted)"' <<<"$item")" \
    "$(jq -r '.id' <<<"$item")" \
    "$(jq -r '.html_url // empty' <<<"$item")" \
    "" \
    "$(jq -r '.reply_to // empty' <<<"$item")" \
    "$(jq -r '.thread_state // empty' <<<"$item")" \
    "$(jq -r '.path // empty' <<<"$item")" \
    "$(jq -r '.line // .original_line // empty' <<<"$item")" \
    "$(jq -r '.side // .original_side // empty' <<<"$item")" \
    "$(jq -r '.pull_request_review_id // empty' <<<"$item")" \
    "$(jq -r '.body // empty' <<<"$item")"
done

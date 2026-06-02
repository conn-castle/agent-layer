#!/usr/bin/env bash
# Claude Code status line — agentic coding view
# Receives JSON on stdin; outputs a single colored line.

# ── Color codes ───────────────────────────────────────────────────────────────
RESET='\033[0m'
BOLD='\033[1m'
DIM='\033[2m'
CYAN='\033[36m'
GREEN='\033[32m'
YELLOW='\033[33m'
MAGENTA='\033[35m'
RED='\033[31m'
BLUE='\033[34m'

SEP="${DIM}│${RESET}"

# ── Read stdin once ───────────────────────────────────────────────────────────
input=$(cat)

# Locate jq (common Homebrew path on Apple Silicon, then PATH)
JQ=/opt/homebrew/bin/jq
command -v "$JQ" >/dev/null 2>&1 || JQ=$(command -v jq 2>/dev/null)
if [ -z "$JQ" ]; then
  printf "statusline: jq not found; install jq and ensure it is on PATH\n"
  exit 0
fi

# ── Parse fields ──────────────────────────────────────────────────────────────
model=$("$JQ" -r '.model.display_name // "unknown"' <<<"$input")
cwd=$("$JQ" -r '.workspace.current_dir // .cwd // "?"' <<<"$input")
project_dir=$("$JQ" -r '.workspace.project_dir // ""' <<<"$input")
session_id=$("$JQ" -r '.session_id // ""' <<<"$input")
git_worktree=$("$JQ" -r '.workspace.git_worktree // ""' <<<"$input")
used_pct=$("$JQ" -r '.context_window.used_percentage // empty' <<<"$input")
weekly_pct=$("$JQ" -r '.rate_limits.seven_day.used_percentage // empty' <<<"$input")
total_input=$("$JQ" -r '.context_window.total_input_tokens // 0' <<<"$input")
ctx_size=$("$JQ" -r '.context_window.context_window_size // 0' <<<"$input")
pr_num=$("$JQ" -r '.pr.number // empty' <<<"$input")
pr_state=$("$JQ" -r '.pr.review_state // "open"' <<<"$input")
cost_usd=$("$JQ" -r '.cost.total_cost_usd // empty' <<<"$input")
effort=$("$JQ" -r '.effort.level // ""' <<<"$input")

# ── Model ─────────────────────────────────────────────────────────────────────
model_str="${CYAN}${model}${RESET}"

# ── Session ───────────────────────────────────────────────────────────────────
sess_str=""
if [ -n "$session_id" ]; then
  sess_str="${DIM}#${session_id}${RESET}"
fi

# ── Directory: show path relative to project root, or full cwd ───────────────
if [ -n "$project_dir" ] && [ "$project_dir" != "$cwd" ]; then
  rel="${cwd#"$project_dir"}"
  rel="${rel#/}"
  [ -z "$rel" ] && rel="."
  dir_str="${DIM}${project_dir##*/}${RESET}/${BOLD}${rel}${RESET}"
else
  dir_str="${BOLD}${cwd##*/}${RESET}"
fi

# ── Worktree indicator ────────────────────────────────────────────────────────
worktree_str=""
if [ -n "$git_worktree" ]; then
  # cwd is inside a linked worktree
  worktree_str="${YELLOW}wt:${git_worktree}${RESET}"
fi

# ── Git: branch + dirty indicator (from the filesystem, not JSON) ─────────────
git_str=""
if command -v git >/dev/null 2>&1; then
  branch=$(git -C "$cwd" rev-parse --abbrev-ref HEAD 2>/dev/null)
  if [ -n "$branch" ] && [ "$branch" != "HEAD" ]; then
    # --porcelain is fast and lock-free
    dirty=$(git -C "$cwd" status --porcelain 2>/dev/null)
    if [ -n "$dirty" ]; then
      git_str="${MAGENTA}${branch}${RESET}${RED}*${RESET}"
    else
      git_str="${MAGENTA}${branch}${RESET}${GREEN}✓${RESET}"
    fi
  elif [ -n "$branch" ]; then
    # detached HEAD
    git_str="${MAGENTA}(detached)${RESET}"
  fi
fi

# ── Context window (always shown, even at 0%) ─────────────────────────────────
if [ -n "$used_pct" ]; then
  used_int=$(printf "%.0f" "$used_pct")
  prefix="ctx:"
elif [ "$ctx_size" -gt 0 ] && [ "$total_input" -gt 0 ]; then
  # Fallback: compute from raw tokens
  used_int=$(( total_input * 100 / ctx_size ))
  prefix="ctx:~"
else
  # No usage data yet — show 0% rather than hiding the segment
  used_int=0
  prefix="ctx:"
fi
# Color by pressure: green < 50%, yellow < 80%, red >= 80%
if [ "$used_int" -ge 80 ]; then
  ctx_color="${RED}"
elif [ "$used_int" -ge 50 ]; then
  ctx_color="${YELLOW}"
else
  ctx_color="${GREEN}"
fi
ctx_str="${ctx_color}${prefix}${used_int}%${RESET}"

# ── Weekly usage limit ────────────────────────────────────────────────────────
weekly_str=""
if [ -n "$weekly_pct" ]; then
  weekly_int=$(printf "%.0f" "$weekly_pct")
  if [ "$weekly_int" -ge 80 ]; then
    weekly_color="${RED}"
  elif [ "$weekly_int" -ge 50 ]; then
    weekly_color="${YELLOW}"
  else
    weekly_color="${GREEN}"
  fi
  weekly_str="${weekly_color}7d:${weekly_int}%${RESET}"
fi

# ── Reasoning effort ──────────────────────────────────────────────────────────
effort_str=""
if [ -n "$effort" ]; then
  effort_str="${BLUE}effort:${effort}${RESET}"
fi

# ── Lines changed + session cost ──────────────────────────────────────────────
cost_str=""
lines_added=0
lines_removed=0
if command -v git >/dev/null 2>&1; then
  # Count tracked staged and unstaged changes against HEAD; untracked files are
  # intentionally excluded.
  while IFS=$'\t' read -r added removed _path; do
    case "$added:$removed" in
      *-*) continue ;;
    esac
    lines_added=$(( lines_added + added ))
    lines_removed=$(( lines_removed + removed ))
  done < <(git -C "$cwd" diff --numstat HEAD -- 2>/dev/null)
fi
if [ "${lines_added:-0}" -gt 0 ] 2>/dev/null || [ "${lines_removed:-0}" -gt 0 ] 2>/dev/null; then
  cost_str="${GREEN}+${lines_added}${RESET}${DIM}/${RESET}${RED}-${lines_removed}${RESET}"
fi
if [ -n "$cost_usd" ]; then
  usd_fmt=$(printf "%.2f" "$cost_usd" 2>/dev/null)
  if [ -n "$usd_fmt" ] && [ "$usd_fmt" != "0.00" ]; then
    [ -n "$cost_str" ] && cost_str="${cost_str} ${SEP} "
    cost_str="${cost_str}${DIM}\$${usd_fmt}${RESET}"
  fi
fi

# ── PR indicator ──────────────────────────────────────────────────────────────
pr_str=""
if [ -n "$pr_num" ]; then
  case "$pr_state" in
    approved)          pr_color="${GREEN}" ;;
    changes_requested) pr_color="${RED}" ;;
    draft)             pr_color="${DIM}" ;;
    *)                 pr_color="${BLUE}" ;;
  esac
  pr_str="${pr_color}PR#${pr_num}${RESET}"
fi

# ── Assemble line ─────────────────────────────────────────────────────────────
parts=()
parts+=("${model_str}")
[ -n "$effort_str" ]   && parts+=("${SEP}" "${effort_str}")
parts+=("${SEP}" "${ctx_str}")
[ -n "$weekly_str" ]   && parts+=("${SEP}" "${weekly_str}")
[ -n "$sess_str" ]     && parts+=("${SEP}" "${sess_str}")
parts+=("${SEP}" "${dir_str}")
[ -n "$worktree_str" ] && parts+=("${SEP}" "${worktree_str}")
[ -n "$git_str" ]      && parts+=("${SEP}" "${git_str}")
[ -n "$cost_str" ]     && parts+=("${SEP}" "${cost_str}")
[ -n "$pr_str" ]       && parts+=("${SEP}" "${pr_str}")

printf "%b\n" "${parts[*]}"

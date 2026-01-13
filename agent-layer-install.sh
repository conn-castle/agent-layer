#!/usr/bin/env bash
set -euo pipefail

# Agent Layer installer/upgrader.
# Run this from the parent repo root (the parent of .agent-layer/).

say() { printf "%s\n" "$*"; }
die() {
  printf "ERROR: %s\n" "$*" >&2
  exit 1
}

usage() {
  cat << 'EOF'
Usage: agent-layer-install.sh [--force] [--upgrade] [--version <tag>] [--latest-branch <branch>] [--repo-url <url>]

Installs/updates agent-layer in the current working repo and sets up a local launcher.
Defaults to the latest tagged release for new installs (detached HEAD).

Options:
  --force, -f       Overwrite ./al if it already exists
  --upgrade, -u     Upgrade .agent-layer to the latest tagged release (detached)
  --version <tag>   Install a specific tagged release (detached)
  --latest-branch   Update .agent-layer to the latest commit of a branch (detached; dev)
  --repo-url <url>  Override the agent-layer repo URL
  --help, -h        Show this help
EOF
}

# Default option values and repo URL configuration.
FORCE="0"
UPGRADE="0"
VERSION=""
VERSION_SET="0"
LATEST_BRANCH=""
LATEST_BRANCH_SET="0"
REPO_URL_DEFAULT="https://github.com/nicholasjconn/agent-layer.git"
REPO_URL_OVERRIDE="0"
REPO_URL="${AGENT_LAYER_REPO_URL:-$REPO_URL_DEFAULT}"
if [[ -n "${AGENT_LAYER_REPO_URL:-}" ]]; then
  REPO_URL_OVERRIDE="1"
fi

# Parse CLI flags and reject unknown options.
while [[ $# -gt 0 ]]; do
  case "$1" in
    --force | -f)
      FORCE="1"
      ;;
    --upgrade | -u)
      UPGRADE="1"
      ;;
    --version)
      [[ $# -ge 2 ]] || die "--version requires a value"
      VERSION="$2"
      VERSION_SET="1"
      shift
      ;;
    --version=*)
      VERSION="${1#*=}"
      VERSION_SET="1"
      ;;
    --latest-branch)
      [[ $# -ge 2 ]] || die "--latest-branch requires a value"
      LATEST_BRANCH="$2"
      LATEST_BRANCH_SET="1"
      shift
      ;;
    --latest-branch=*)
      LATEST_BRANCH="${1#*=}"
      LATEST_BRANCH_SET="1"
      ;;
    --repo-url)
      [[ $# -ge 2 ]] || die "--repo-url requires a value"
      REPO_URL="$2"
      REPO_URL_OVERRIDE="1"
      shift
      ;;
    --repo-url=*)
      REPO_URL="${1#*=}"
      REPO_URL_OVERRIDE="1"
      ;;
    --help | -h)
      usage
      exit 0
      ;;
    *)
      die "Unknown argument: $1"
      ;;
  esac
  shift
done

# Validate incompatible or missing flag combinations.
if [[ "$UPGRADE" == "1" && -n "$LATEST_BRANCH" ]]; then
  die "Choose only one: --upgrade or --latest-branch <branch>"
fi
if [[ "$UPGRADE" == "1" && -n "$VERSION" ]]; then
  die "Choose only one: --upgrade or --version <tag>"
fi
if [[ -n "$VERSION" && -n "$LATEST_BRANCH" ]]; then
  die "Choose only one: --version <tag> or --latest-branch <branch>"
fi
if [[ "$LATEST_BRANCH_SET" == "1" && -z "$LATEST_BRANCH" ]]; then
  die "--latest-branch requires a value"
fi
if [[ "$VERSION_SET" == "1" && -z "$VERSION" ]]; then
  die "--version requires a value"
fi

# Require git up front because install/upgrade uses it heavily.
command -v git > /dev/null 2>&1 || die "git is required (not found)."

# Confirm we are running from the repo root (not inside .agent-layer).
PARENT_ROOT="$(pwd -P)"
if [[ "$(basename "$PARENT_ROOT")" == ".agent-layer" ]]; then
  die "Run this from the parent repo root (parent of .agent-layer/), not inside .agent-layer/."
fi

# Enforce or warn about git repo state to support hooks.
if git rev-parse --show-toplevel > /dev/null 2>&1; then
  GIT_ROOT="$(git rev-parse --show-toplevel)"
  if [[ "$GIT_ROOT" != "$PARENT_ROOT" ]]; then
    die "Run this from the repo root: $GIT_ROOT"
  fi
else
  say "WARNING: This directory does not appear to be a git repo."
  say "If you meant a different folder, stop now."
  say "You can continue, but hooks won't be enabled until you init git."
  if [[ -t 0 ]]; then
    read -r -p "Continue anyway? [y/N] " reply
    case "$reply" in
      y | Y | yes | YES) ;;
      *)
        die "Aborted."
        ;;
    esac
  else
    die "Not a git repo and no TTY available to confirm. Re-run from a TTY or after init."
  fi
fi

AGENT_LAYER_DIR="$PARENT_ROOT/.agent-layer"

# Resolve the fetch target for upgrades (explicit URL or origin remote).
resolve_fetch_target() {
  if [[ "$REPO_URL_OVERRIDE" == "1" ]]; then
    printf "%s" "$REPO_URL"
    return 0
  fi
  if git -C "$AGENT_LAYER_DIR" remote get-url origin > /dev/null 2>&1; then
    printf "%s" "origin"
    return 0
  fi
  die "No origin remote found. Use --repo-url <url> or set AGENT_LAYER_REPO_URL."
}

# Ensure a specific tag exists in the remote before cloning.
ensure_version_exists_remote() {
  local version="$1"
  local remote="$2"
  local refs

  say "==> Checking for tag '$version' in $remote"
  if ! refs="$(git ls-remote --tags "$remote" "refs/tags/$version" "refs/tags/$version^{}" 2> /dev/null)"; then
    die "Failed to query tags from $remote; cannot verify '$version'."
  fi
  if [[ -z "$refs" ]]; then
    die "Tag '$version' not found; cannot install requested version."
  fi
}

# Upgrade .agent-layer to the latest local tag.
upgrade_agent_layer() {
  local fetch_target latest_tag current_commit current_tag changes

  if [[ -n "$(git -C "$AGENT_LAYER_DIR" status --porcelain)" ]]; then
    die ".agent-layer has uncommitted changes. Commit or stash before upgrading."
  fi

  fetch_target="$(resolve_fetch_target)"

  say "==> Fetching tags for .agent-layer"
  git -C "$AGENT_LAYER_DIR" fetch --tags "$fetch_target"

  latest_tag="$(git -C "$AGENT_LAYER_DIR" tag --list --sort=-v:refname | head -n 1)"
  [[ -n "$latest_tag" ]] || die "No tags found after fetching; cannot install latest release. Use --latest-branch <branch> for dev builds."

  current_commit="$(git -C "$AGENT_LAYER_DIR" rev-parse --short HEAD)"
  current_tag="$(git -C "$AGENT_LAYER_DIR" describe --tags --exact-match 2> /dev/null || true)"

  say "==> Current version: ${current_tag:-$current_commit}"
  say "==> Latest tag: $latest_tag"

  if [[ "$current_tag" == "$latest_tag" ]]; then
    say "==> .agent-layer is already up to date."
    return 0
  fi

  say "==> Checking out $latest_tag"
  git -C "$AGENT_LAYER_DIR" checkout -q "$latest_tag"

  say "==> Changes since ${current_tag:-$current_commit}:"
  changes="$(git -C "$AGENT_LAYER_DIR" --no-pager log --oneline "$current_commit..$latest_tag" || true)"
  if [[ -n "$changes" ]]; then
    printf "%s\n" "$changes"
  else
    say "  (no commits listed)"
  fi

  say "==> Note: .agent-layer is now on a detached HEAD at $latest_tag."
}

# Update .agent-layer to a specific tag.
install_agent_layer_version() {
  local version="$1"
  local fetch_target current_commit current_tag changes

  if [[ -n "$(git -C "$AGENT_LAYER_DIR" status --porcelain)" ]]; then
    die ".agent-layer has uncommitted changes. Commit or stash before updating."
  fi

  fetch_target="$(resolve_fetch_target)"

  say "==> Fetching tags for .agent-layer"
  git -C "$AGENT_LAYER_DIR" fetch --tags "$fetch_target"

  if ! git -C "$AGENT_LAYER_DIR" rev-parse -q --verify "refs/tags/$version" > /dev/null; then
    die "Tag '$version' not found after fetching; cannot install requested version."
  fi

  current_commit="$(git -C "$AGENT_LAYER_DIR" rev-parse --short HEAD)"
  current_tag="$(git -C "$AGENT_LAYER_DIR" describe --tags --exact-match 2> /dev/null || true)"

  say "==> Current version: ${current_tag:-$current_commit}"
  say "==> Requested tag: $version"

  if [[ "$current_tag" == "$version" ]]; then
    say "==> .agent-layer is already at $version."
    return 0
  fi

  say "==> Checking out $version"
  git -C "$AGENT_LAYER_DIR" checkout -q "$version"

  say "==> Changes since ${current_tag:-$current_commit}:"
  changes="$(git -C "$AGENT_LAYER_DIR" --no-pager log --oneline "$current_commit..$version" || true)"
  if [[ -n "$changes" ]]; then
    printf "%s\n" "$changes"
  else
    say "  (no commits listed)"
  fi

  say "==> Note: .agent-layer is now on a detached HEAD at $version."
}

# Update .agent-layer to the latest commit on a specific branch.
latest_branch_agent_layer() {
  local branch="$1"
  local fetch_target current_commit latest_commit changes

  if [[ -n "$(git -C "$AGENT_LAYER_DIR" status --porcelain)" ]]; then
    die ".agent-layer has uncommitted changes. Commit or stash before updating."
  fi

  fetch_target="$(resolve_fetch_target)"

  say "==> Fetching latest commit for branch '$branch'"
  git -C "$AGENT_LAYER_DIR" fetch "$fetch_target" "$branch"

  latest_commit="$(git -C "$AGENT_LAYER_DIR" rev-parse --short FETCH_HEAD)"
  current_commit="$(git -C "$AGENT_LAYER_DIR" rev-parse --short HEAD)"

  if [[ "$current_commit" == "$latest_commit" ]]; then
    say "==> .agent-layer is already at latest $branch ($latest_commit)."
  else
    say "==> Current commit: $current_commit"
    say "==> Latest $branch commit: $latest_commit"
  fi
  say "==> Checking out latest $branch commit"
  git -C "$AGENT_LAYER_DIR" checkout -q --detach FETCH_HEAD

  if [[ "$current_commit" != "$latest_commit" ]]; then
    changes="$(git -C "$AGENT_LAYER_DIR" --no-pager log --oneline -n 5 FETCH_HEAD || true)"
    if [[ -n "$changes" ]]; then
      say "==> Recent commits:"
      printf "%s\n" "$changes"
    fi
  fi

  say "==> Note: .agent-layer is now on a detached HEAD at $latest_commit."
  say "==> To update again, re-run the installer with: --latest-branch $branch"
}

# Ensure .agent-layer exists, then apply the requested upgrade behavior.
if [[ ! -e "$AGENT_LAYER_DIR" ]]; then
  [[ -n "$REPO_URL" ]] || die "Missing repo URL (set AGENT_LAYER_REPO_URL or use --repo-url)."
  if [[ -n "$VERSION" ]]; then
    ensure_version_exists_remote "$VERSION" "$REPO_URL"
  fi
  say "==> Cloning agent-layer into .agent-layer/"
  git clone "$REPO_URL" "$AGENT_LAYER_DIR"
  if [[ -n "$VERSION" ]]; then
    install_agent_layer_version "$VERSION"
  elif [[ -n "$LATEST_BRANCH" ]]; then
    latest_branch_agent_layer "$LATEST_BRANCH"
  else
    upgrade_agent_layer
  fi
else
  if [[ -d "$AGENT_LAYER_DIR" ]]; then
    if git -C "$AGENT_LAYER_DIR" rev-parse --is-inside-work-tree > /dev/null 2>&1; then
      if [[ -n "$VERSION" ]]; then
        install_agent_layer_version "$VERSION"
      elif [[ "$UPGRADE" == "1" ]]; then
        upgrade_agent_layer
      elif [[ -n "$LATEST_BRANCH" ]]; then
        latest_branch_agent_layer "$LATEST_BRANCH"
      else
        say "==> .agent-layer exists and is a git repo; leaving as-is"
      fi
    else
      die ".agent-layer exists but is not a git repo. Move it aside or remove it, then re-run."
    fi
  else
    die ".agent-layer exists but is not a directory. Move it aside or remove it, then re-run."
  fi
fi

# Ensure .agent-layer/.env exists (copy from .env.example if needed).
if [[ ! -f "$AGENT_LAYER_DIR/.env" ]]; then
  if [[ -f "$AGENT_LAYER_DIR/.env.example" ]]; then
    cp "$AGENT_LAYER_DIR/.env.example" "$AGENT_LAYER_DIR/.env"
    say "==> Created .agent-layer/.env from .env.example"
  else
    die "Missing .agent-layer/.env.example; cannot create .agent-layer/.env"
  fi
else
  say "==> .agent-layer/.env already exists; leaving as-is"
fi

DOCS_DIR="$PARENT_ROOT/docs"
TEMPLATES_DIR="$AGENT_LAYER_DIR/config/templates/docs"

# Create or refresh project memory files using provided templates.
ensure_memory_file() {
  local file_path="$1"
  local template_path="$2"
  local rel_path

  rel_path="${file_path#"$PARENT_ROOT"/}"

  if [[ ! -f "$template_path" ]]; then
    die "Missing template: ${template_path#"$AGENT_LAYER_DIR"/}"
  fi

  if [[ -f "$file_path" ]]; then
    if [[ -t 0 ]]; then
      read -r -p "$rel_path exists. Keep it? [Y/n] " reply
      case "$reply" in
        n | N | no | NO)
          mkdir -p "$(dirname "$file_path")"
          cp "$template_path" "$file_path"
          say "==> Replaced $rel_path with template"
          ;;
        *)
          say "==> Keeping existing $rel_path"
          ;;
      esac
    else
      say "==> $rel_path exists; leaving as-is (no TTY to confirm)"
    fi
  else
    mkdir -p "$(dirname "$file_path")"
    cp "$template_path" "$file_path"
    say "==> Created $rel_path from template"
  fi
}

say "==> Ensuring project memory files exist"
ensure_memory_file "$DOCS_DIR/ISSUES.md" "$TEMPLATES_DIR/ISSUES.md"
ensure_memory_file "$DOCS_DIR/FEATURES.md" "$TEMPLATES_DIR/FEATURES.md"
ensure_memory_file "$DOCS_DIR/ROADMAP.md" "$TEMPLATES_DIR/ROADMAP.md"
ensure_memory_file "$DOCS_DIR/DECISIONS.md" "$TEMPLATES_DIR/DECISIONS.md"

AL_PATH="$PARENT_ROOT/al"

# Write the repo-local launcher script (overwrites if requested).
write_launcher() {
  cat > "$AL_PATH" << 'EOF'
#!/usr/bin/env bash
set -euo pipefail

# Repo-local launcher.
# This script delegates to the managed Agent Layer entrypoint in .agent-layer/.
# If you prefer, replace this file with a symlink to .agent-layer/agent-layer.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec "$SCRIPT_DIR/.agent-layer/agent-layer" "$@"
EOF
  chmod +x "$AL_PATH"
}

# Create or preserve ./al based on --force.
if [[ -e "$AL_PATH" ]]; then
  if [[ "$FORCE" == "1" ]]; then
    say "==> Overwriting ./al"
    write_launcher
  else
    say "==> ./al exists; leaving as-is (use --force to overwrite)"
  fi
else
  say "==> Creating ./al"
  write_launcher
fi

GITIGNORE_PATH="$PARENT_ROOT/.gitignore"
GITIGNORE_BLOCK="$(
  cat << 'EOF'
# >>> agent-layer
.agent-layer/

# Agent Layer launcher
al

# Agent Layer-generated instruction shims
AGENTS.md
CLAUDE.md
GEMINI.md
.github/copilot-instructions.md

# Agent Layer-generated client configs + artifacts
.mcp.json
.codex/
.gemini/
.claude/
.vscode/mcp.json
# <<< agent-layer
EOF
)"

# Ensure the agent-layer gitignore block exists exactly once.
update_gitignore() {
  local tmp last_char last_line found inblock line
  tmp="$(mktemp)" || die "Failed to create temp file."
  found="0"
  inblock="0"
  if [[ -f "$GITIGNORE_PATH" ]]; then
    while IFS= read -r line || [[ -n "$line" ]]; do
      if [[ "$line" == "# >>> agent-layer" ]]; then
        if [[ "$found" == "0" ]]; then
          printf "%s\n" "$GITIGNORE_BLOCK" >> "$tmp"
          found="1"
        fi
        inblock="1"
        continue
      fi
      if [[ "$inblock" == "1" ]]; then
        if [[ "$line" == "# <<< agent-layer" ]]; then
          inblock="0"
        fi
        continue
      fi
      printf "%s\n" "$line" >> "$tmp"
    done < "$GITIGNORE_PATH"
  else
    : > "$tmp"
  fi

  # Append the block if it was not found in the existing file.
  if [[ "$found" == "0" ]]; then
    if [[ -s "$tmp" ]]; then
      last_char="$(tail -c 1 "$tmp" || true)"
      if [[ "$last_char" != $'\n' ]]; then
        printf '\n' >> "$tmp"
      fi
      last_line="$(tail -n 1 "$tmp" || true)"
      if [[ -n "$last_line" ]]; then
        printf '\n' >> "$tmp"
      fi
    fi
    printf "%s\n" "$GITIGNORE_BLOCK" >> "$tmp"
  fi

  mv "$tmp" "$GITIGNORE_PATH"
}

say "==> Updating .gitignore (agent-layer block)"
update_gitignore

# Run setup to generate configs and install MCP prompt server dependencies.
if [[ -f "$AGENT_LAYER_DIR/setup.sh" ]]; then
  say "==> Running setup"
  bash "$AGENT_LAYER_DIR/setup.sh"
else
  die "Missing .agent-layer/setup.sh"
fi

# Print next steps for running the configured tools.
say ""
say "After completing the required manual steps above, run one of:"
say "  ./al gemini"
say "  ./al claude"
say "  ./al codex"

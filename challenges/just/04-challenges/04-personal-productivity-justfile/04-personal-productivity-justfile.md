# 34. Personal Productivity Justfile

<!--
difficulty: advanced
concepts: [global-justfile, fallback-setting, git-workflows, system-maintenance, project-scaffolding, built-in-functions]
tools: [just]
estimated_time: 45m
bloom_level: create
prerequisites: [shebang-recipes, doc-attributes, group-attributes, built-in-functions]
-->

## Prerequisites

- just >= 1.38.0
- git, docker, curl
- A shell (bash/zsh) with standard Unix tools

## Learning Objectives

- **Create** a global justfile that acts as a personal command palette for daily workflows
- **Design** grouped recipe collections spanning git, system maintenance, and project scaffolding
- **Evaluate** when to use just built-in functions (`datetime_utc()`, `sha256_file()`, `uuid()`) versus shell equivalents

## Why a Personal Productivity Justfile

Every developer accumulates shell aliases, functions, and one-liner scripts across dotfiles. They drift out of sync between machines, lack discoverability, and have no documentation. A global justfile consolidates these into a single, self-documenting, version-controlled file. With `set fallback` enabled in project justfiles, your personal recipes are always available as a fallback when no local recipe matches.

## The Challenge

Build a global justfile at `~/.user.justfile` that covers four categories: git workflows (feature branches, PRs, sync, branch cleanup), system maintenance (Docker cleanup, temp files, disk usage), project scaffolding (new Rust project, generic project), and utilities (UUID generation, timestamps, file hashing). Use `[group()]` for organization, `[doc()]` for descriptions, and just built-in functions wherever they replace shell commands.

## Solution

```justfile
# file: ~/.user.justfile
#
# Personal productivity justfile. Use with:
#   just --justfile ~/.user.justfile --list
#
# Or set in project justfiles:
#   set fallback
#
# Or alias:
#   alias j='just --justfile ~/.user.justfile --working-directory .'

set shell := ["bash", "-euo", "pipefail", "-c"]

today := datetime_utc("%Y-%m-%d")
now := datetime_utc("%Y-%m-%dT%H:%M:%SZ")

# ═══════════════════════════════════════════════════════════
# Git Workflows
# ═══════════════════════════════════════════════════════════

[group('git')]
[doc('Create a feature branch from latest main')]
feature name:
    #!/usr/bin/env bash
    set -euo pipefail
    git fetch origin
    git checkout -b "feature/{{ name }}" origin/main
    printf '\033[32mCreated feature/{{ name }} from origin/main\033[0m\n'

[group('git')]
[doc('Create a bugfix branch from latest main')]
bugfix name:
    #!/usr/bin/env bash
    set -euo pipefail
    git fetch origin
    git checkout -b "bugfix/{{ name }}" origin/main
    printf '\033[32mCreated bugfix/{{ name }} from origin/main\033[0m\n'

[group('git')]
[doc('Sync current branch with latest main (rebase)')]
sync:
    #!/usr/bin/env bash
    set -euo pipefail
    branch=$(git branch --show-current)
    if [ "$branch" = "main" ]; then
        git pull --ff-only origin main
    else
        git fetch origin
        git rebase origin/main
    fi
    printf '\033[32m%s synced with origin/main\033[0m\n' "$branch"

[group('git')]
[doc('Push current branch and open a PR draft')]
pr title="":
    #!/usr/bin/env bash
    set -euo pipefail
    branch=$(git branch --show-current)
    git push -u origin "$branch"
    if command -v gh >/dev/null 2>&1; then
        if [ -n "{{ title }}" ]; then
            gh pr create --draft --title "{{ title }}"
        else
            gh pr create --draft --fill
        fi
    else
        printf '\033[33mgh CLI not found. Create PR manually:\033[0m\n'
        remote_url=$(git remote get-url origin | sed 's/\.git$//')
        echo "  ${remote_url}/compare/${branch}?expand=1"
    fi

[group('git')]
[doc('Delete local branches merged into main')]
clean-branches:
    #!/usr/bin/env bash
    set -euo pipefail
    git checkout main 2>/dev/null || git checkout master
    git fetch --prune origin

    merged=$(git branch --merged | grep -vE '^\*|main|master|develop' || true)
    if [ -z "$merged" ]; then
        printf '\033[32mNo merged branches to clean\033[0m\n'
        exit 0
    fi

    echo "Branches merged into $(git branch --show-current):"
    echo "$merged" | sed 's/^/  /'
    echo ""
    read -p "Delete these branches? [y/N] " confirm
    if [[ "$confirm" =~ ^[Yy]$ ]]; then
        echo "$merged" | xargs git branch -d
        printf '\033[32mDone. Removed %d branch(es).\033[0m\n' "$(echo "$merged" | wc -l | tr -d ' ')"
    else
        echo "Cancelled."
    fi

[group('git')]
[doc('Amend the last commit with staged changes (no message edit)')]
amend:
    git commit --amend --no-edit

[group('git')]
[doc('Show compact log of last N commits (default: 15)')]
log n="15":
    git log --oneline --graph --decorate -{{ n }}

[group('git')]
[doc('Stash work in progress with a timestamped message')]
wip:
    git stash push -m "WIP {{ now }}"
    @printf '\033[32mStashed as: WIP {{ now }}\033[0m\n'

# ═══════════════════════════════════════════════════════════
# System Maintenance
# ═══════════════════════════════════════════════════════════

[group('system')]
[doc('Clean Docker: stopped containers, dangling images, build cache')]
docker-clean:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "=== Docker Cleanup ==="
    echo ""

    containers=$(docker ps -aq --filter status=exited 2>/dev/null | wc -l | tr -d ' ')
    images=$(docker images -f dangling=true -q 2>/dev/null | wc -l | tr -d ' ')
    echo "  Stopped containers: $containers"
    echo "  Dangling images:    $images"

    before=$(docker system df --format '{{`{{.Size}}`}}' 2>/dev/null | head -1)
    docker system prune -f 2>/dev/null
    after=$(docker system df --format '{{`{{.Size}}`}}' 2>/dev/null | head -1)

    echo ""
    printf '\033[32mCleaned. Disk: %s -> %s\033[0m\n' "$before" "$after"

[group('system')]
[doc('Remove common temp files and caches')]
tmp-clean:
    #!/usr/bin/env bash
    set -euo pipefail
    freed=0
    clean_dir() {
        local dir="$1"
        local desc="$2"
        if [ -d "$dir" ]; then
            size=$(du -sh "$dir" 2>/dev/null | cut -f1)
            rm -rf "$dir"
            printf '  \033[32mRemoved\033[0m %-30s %s\n' "$desc" "$size"
        fi
    }

    echo "=== Temp File Cleanup ==="
    echo ""
    clean_dir "$HOME/.cache/pip" "pip cache"
    clean_dir "$HOME/.npm/_cacache" "npm cache"
    clean_dir "$HOME/Library/Caches/Homebrew" "Homebrew cache"
    clean_dir "/tmp/rust-analyzer-*" "rust-analyzer tmp" 2>/dev/null || true

    # Clean .DS_Store recursively from home
    count=$(find "$HOME/Documents" -name '.DS_Store' -delete -print 2>/dev/null | wc -l | tr -d ' ')
    if [ "$count" -gt 0 ]; then
        printf '  \033[32mRemoved\033[0m %-30s %s files\n' ".DS_Store files" "$count"
    fi

    echo ""
    printf '\033[32mDone.\033[0m\n'

[group('system')]
[doc('Show disk usage summary for common directories')]
disk:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "=== Disk Usage ==="
    echo ""
    dirs=(
        "$HOME/Documents"
        "$HOME/Downloads"
        "$HOME/.cargo"
        "$HOME/.rustup"
        "$HOME/.docker"
        "$HOME/.cache"
        "$HOME/Library/Caches"
    )
    for d in "${dirs[@]}"; do
        if [ -d "$d" ]; then
            size=$(du -sh "$d" 2>/dev/null | cut -f1)
            printf "  %-35s %s\n" "${d/#$HOME/~}" "$size"
        fi
    done
    echo ""
    df -h / | tail -1 | awk '{printf "  Disk: %s used of %s (%s)\n", $3, $2, $5}'

# ═══════════════════════════════════════════════════════════
# Project Scaffolding
# ═══════════════════════════════════════════════════════════

[group('scaffold')]
[doc('Scaffold a new Rust project with standard structure')]
new-rust name:
    #!/usr/bin/env bash
    set -euo pipefail
    cargo init "{{ name }}"
    cd "{{ name }}"

    # Add standard files
    cat > .gitignore <<'GITEOF'
    /target
    .env
    .DS_Store
    *.swp
    GITEOF

    cat > justfile <<'JUSTEOF'
    set shell := ["bash", "-euo", "pipefail", "-c"]
    set fallback

    default:
        @just --list

    build:
        cargo build

    test:
        cargo test

    run *args:
        cargo run -- {{ '{{args}}' }}

    check: lint test
        @echo "All checks passed."

    lint:
        cargo clippy -- -D warnings

    fmt:
        cargo fmt

    clean:
        cargo clean
    JUSTEOF

    cat > .editorconfig <<'ECEOF'
    root = true

    [*]
    indent_style = space
    indent_size = 4
    end_of_line = lf
    insert_final_newline = true
    trim_trailing_whitespace = true

    [*.{yml,yaml,toml}]
    indent_size = 2
    ECEOF

    git add -A
    git commit -m "Initial scaffold"

    printf '\033[32mCreated Rust project: {{ name }}/\033[0m\n'
    echo "  cd {{ name }} && just --list"

[group('scaffold')]
[doc('Scaffold a generic project directory with common files')]
new-project name:
    #!/usr/bin/env bash
    set -euo pipefail
    mkdir -p "{{ name }}"/{src,tests,docs}
    cd "{{ name }}"
    git init

    cat > .gitignore <<'GITEOF'
    .env
    .DS_Store
    *.swp
    /dist
    /node_modules
    GITEOF

    cat > justfile <<'JUSTEOF'
    set shell := ["bash", "-euo", "pipefail", "-c"]
    set fallback

    default:
        @just --list
    JUSTEOF

    git add -A
    git commit -m "Initial scaffold"

    printf '\033[32mCreated project: {{ name }}/\033[0m\n'
    ls -la

# ═══════════════════════════════════════════════════════════
# Utilities
# ═══════════════════════════════════════════════════════════

[group('util')]
[doc('Generate a random UUID (v4)')]
uuid:
    @echo "{{ uuid() }}"

[group('util')]
[doc('Print current UTC timestamp in ISO 8601')]
timestamp:
    @echo "{{ now }}"

[group('util')]
[doc('Print today'\''s date')]
date:
    @echo "{{ today }}"

[group('util')]
[doc('Compute SHA-256 hash of a file')]
hash file:
    @echo "{{ sha256_file(file) }}"

[group('util')]
[doc('Encode a string as base64')]
b64 text:
    @echo "{{ text }}" | base64

[group('util')]
[doc('Decode a base64 string')]
b64d text:
    @echo "{{ text }}" | base64 --decode && echo ""

[group('util')]
[doc('Generate a random password (default 32 chars)')]
password len="32":
    @openssl rand -base64 48 | tr -dc 'a-zA-Z0-9!@#$%' | head -c {{ len }} && echo ""

[group('util')]
[doc('Quick HTTP GET with response status')]
http url:
    #!/usr/bin/env bash
    set -euo pipefail
    code=$(curl -s -o /dev/null -w "%{http_code}" "{{ url }}")
    printf "  Status: %s\n" "$code"
    curl -s "{{ url }}" | head -20
    echo ""

[group('util')]
[doc('Show the public IP of this machine')]
my-ip:
    @curl -s https://ifconfig.me && echo ""

[group('util')]
[doc('Quick-serve current directory over HTTP on port 8000')]
serve port="8000":
    #!/usr/bin/env bash
    if command -v python3 >/dev/null 2>&1; then
        echo "Serving on http://localhost:{{ port }}"
        python3 -m http.server {{ port }}
    else
        echo "python3 not found"
        exit 1
    fi
```

## Verify What You Learned

```bash
# List all recipes in the global justfile, grouped
just --justfile ~/.user.justfile --list

# Generate a UUID using the built-in function
just --justfile ~/.user.justfile uuid

# Check current timestamp
just --justfile ~/.user.justfile timestamp

# Hash a file
just --justfile ~/.user.justfile hash ~/.user.justfile

# View disk usage summary
just --justfile ~/.user.justfile disk
```

## What's Next

Continue to [Exercise 35: Modular Plugin System](../05-modular-plugin-system/05-modular-plugin-system.md) to build a plugin architecture with `mod` imports and dynamic discovery.

## Summary

- A global justfile at `~/.user.justfile` consolidates personal workflows into one discoverable file
- `set fallback` in project justfiles lets personal recipes serve as a fallback layer
- Built-in functions like `uuid()`, `datetime_utc()`, and `sha256_file()` eliminate shell dependencies for common operations
- `[group()]` categories (git, system, scaffold, util) keep dozens of recipes navigable
- The justfile is version-controllable via dotfiles for consistency across machines

## Reference

- [Fallback setting](https://just.systems/man/en/fallback.html)
- [Built-in functions](https://just.systems/man/en/built-in-functions.html)
- [Groups](https://just.systems/man/en/groups.html)

## Additional Resources

- [Managing dotfiles with just](https://github.com/casey/just#quick-start)
- [just recipes as shell function replacements](https://www.reddit.com/r/commandline/comments/just_command_runner/)

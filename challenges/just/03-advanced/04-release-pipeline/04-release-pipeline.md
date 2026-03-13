# 24. Release Pipeline

<!--
difficulty: advanced
concepts:
  - require() for tool verification
  - color constants for terminal output
  - confirm attribute for destructive actions
  - git tag creation and management
  - pre-release validation chains
  - dry-run mode
  - VERSION file reading
tools: [just, git, cargo]
estimated_time: 45 minutes
bloom_level: evaluate
prerequisites:
  - just intermediate (dependencies, conditional expressions)
  - git tagging and release workflows
  - semantic versioning
-->

## Prerequisites

| Tool | Minimum Version | Check Command |
|------|----------------|---------------|
| just | 1.25+ | `just --version` |
| git | 2.30+ | `git --version` |
| cargo | 1.70+ | `cargo --version` |

## Learning Objectives

- **Evaluate** which safety gates are necessary at each stage of a release pipeline
- **Design** a validation chain that catches errors early and fails fast before irreversible steps
- **Justify** the use of dry-run mode and `[confirm]` to prevent accidental releases

## Why Automated Release Pipelines

Manual releases are error-prone. Forgetting to run tests, tagging an unclean working tree, or publishing a build from the wrong branch causes downstream pain. A release pipeline encoded in a justfile is version-controlled, repeatable, and self-documenting. Every developer runs the same steps in the same order.

The pipeline in this exercise follows a strict validation chain: verify tools exist, check the working tree is clean, confirm no duplicate tag, lint, test, build, and only then tag and publish. Each step is a dependency of the next, so failure at any point aborts the entire release. The `[confirm]` attribute adds a human checkpoint before the point of no return.

Dry-run mode is essential for testing the pipeline itself. You should be able to run the entire release flow without actually pushing tags or publishing artifacts. This exercise implements dry-run as a variable that gates the final irreversible steps while executing everything else normally.

## Step 1 -- Version Management and Color Constants

```just
# justfile

set dotenv-load
set export

# Read version from file — single source of truth
version := trim(read("VERSION"))

# Color constants for formatted output
RED    := '\033[0;31m'
GREEN  := '\033[0;32m'
YELLOW := '\033[1;33m'
BLUE   := '\033[0;34m'
BOLD   := '\033[1m'
NORMAL := '\033[0m'

# Configuration
project   := "myapp"
bin_name  := project
main_branch := "main"
```

Create the VERSION file:

```bash
echo "1.0.0" > VERSION
```

The `read()` function reads a file's contents and `trim()` strips whitespace. This keeps the version in a single file that other tools (CI, Dockerfile, etc.) can also read.

## Step 2 -- Tool Verification with require()

```just
# Verify all required tools are installed
verify-tools:
    @echo "{{ BLUE }}Checking required tools...{{ NORMAL }}"
    {{ require("git") }}
    {{ require("cargo") }}
    {{ require("tar") }}
    @echo "{{ GREEN }}All tools present{{ NORMAL }}"
```

The `require()` function checks that an executable exists on `PATH` and aborts with a clear error if it does not. Use this at the start of any recipe that depends on external tools. It is far better to fail immediately with "cargo not found" than to fail halfway through a build.

## Step 3 -- Pre-Release Validation Chain

Each validation step is a separate recipe so it can be tested independently. The chain enforces ordering through dependencies.

```just
# Verify the working tree is clean (no uncommitted changes)
verify-clean:
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ -n "$(git status --porcelain)" ]]; then
        echo "{{ RED }}Working tree is dirty. Commit or stash changes first.{{ NORMAL }}"
        git status --short
        exit 1
    fi
    echo "{{ GREEN }}Working tree is clean{{ NORMAL }}"

# Verify the git tag does not already exist
verify-no-tag:
    #!/usr/bin/env bash
    set -euo pipefail
    tag="v{{ version }}"
    if git rev-parse "$tag" >/dev/null 2>&1; then
        echo "{{ RED }}Tag $tag already exists.{{ NORMAL }}"
        echo "Bump the version in VERSION and try again."
        exit 1
    fi
    echo "{{ GREEN }}Tag $tag is available{{ NORMAL }}"

# Verify we are on the main branch
verify-branch:
    #!/usr/bin/env bash
    set -euo pipefail
    branch=$(git branch --show-current)
    if [[ "$branch" != "{{ main_branch }}" ]]; then
        echo "{{ RED }}Releases must be made from '{{ main_branch }}', currently on '$branch'{{ NORMAL }}"
        exit 1
    fi
    echo "{{ GREEN }}On branch {{ main_branch }}{{ NORMAL }}"
```

## Step 4 -- Build and Test Recipes

```just
# Lint the codebase
lint:
    @echo "{{ BLUE }}Linting...{{ NORMAL }}"
    cargo fmt --check
    cargo clippy -- -D warnings
    @echo "{{ GREEN }}Lint passed{{ NORMAL }}"

# Run the test suite
test:
    @echo "{{ BLUE }}Running tests...{{ NORMAL }}"
    cargo test --workspace
    @echo "{{ GREEN }}All tests passed{{ NORMAL }}"

# Build release binary
build:
    @echo "{{ BLUE }}Building release binary...{{ NORMAL }}"
    cargo build --release
    @echo "{{ GREEN }}Built target/release/{{ bin_name }}{{ NORMAL }}"

# Package the release artifact
package: build
    #!/usr/bin/env bash
    set -euo pipefail
    archive="{{ project }}-v{{ version }}-{{ os() }}-{{ arch() }}.tar.gz"
    echo "{{ BLUE }}Packaging $archive...{{ NORMAL }}"
    tar czf "$archive" -C target/release "{{ bin_name }}"
    echo "{{ GREEN }}Created $archive ($(du -h "$archive" | cut -f1)){{ NORMAL }}"
```

## Step 5 -- The Release Recipe

This is the main entry point. It chains all validation and build steps, then gates the irreversible actions behind `[confirm]`.

```just
# Full pre-release validation (everything except tagging/publishing)
pre-release: verify-tools verify-branch verify-clean verify-no-tag lint test build package
    @echo ""
    @echo "{{ GREEN }}{{ BOLD }}Pre-release validation complete for v{{ version }}{{ NORMAL }}"
    @echo ""

# Execute the full release pipeline
[confirm("Release v{{ version }}? This will create a git tag and push. (yes/no)")]
release: pre-release _tag _push
    @echo ""
    @echo "{{ GREEN }}{{ BOLD }}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━{{ NORMAL }}"
    @echo "{{ GREEN }}{{ BOLD }}  Released {{ project }} v{{ version }}  {{ NORMAL }}"
    @echo "{{ GREEN }}{{ BOLD }}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━{{ NORMAL }}"

# Create the git tag (private recipe)
_tag:
    @echo "{{ BLUE }}Creating tag v{{ version }}...{{ NORMAL }}"
    git tag -a "v{{ version }}" -m "Release v{{ version }}"
    @echo "{{ GREEN }}Tag v{{ version }} created{{ NORMAL }}"

# Push the tag (private recipe)
_push:
    @echo "{{ BLUE }}Pushing tag v{{ version }}...{{ NORMAL }}"
    git push origin "v{{ version }}"
    @echo "{{ GREEN }}Tag pushed to origin{{ NORMAL }}"
```

Private recipes (prefixed with `_`) do not appear in `just --list` and should not be called directly. They encapsulate irreversible operations.

## Step 6 -- Dry-Run Mode

```just
# Dry-run: execute the full pipeline without tagging or pushing
dry-run: verify-tools verify-branch verify-clean verify-no-tag lint test build package
    @echo ""
    @echo "{{ YELLOW }}{{ BOLD }}DRY RUN complete for v{{ version }}{{ NORMAL }}"
    @echo "{{ YELLOW }}No tag was created. No artifacts were pushed.{{ NORMAL }}"
    @echo "Run {{ BOLD }}just release{{ NORMAL }} to execute for real."
```

Dry-run runs the entire validation and build chain but stops before `_tag` and `_push`. This is the recipe you run first to verify everything works.

## Step 7 -- Version Bump Helper

```just
# Bump the version (patch, minor, or major)
bump part='patch':
    #!/usr/bin/env bash
    set -euo pipefail
    current="{{ version }}"
    IFS='.' read -r major minor patch <<< "$current"

    case "{{ part }}" in
        major) major=$((major + 1)); minor=0; patch=0 ;;
        minor) minor=$((minor + 1)); patch=0 ;;
        patch) patch=$((patch + 1)) ;;
        *) echo "{{ RED }}Invalid part: {{ part }}. Use major, minor, or patch.{{ NORMAL }}"; exit 1 ;;
    esac

    new="$major.$minor.$patch"
    echo "$new" > VERSION
    echo "{{ GREEN }}Bumped $current → $new{{ NORMAL }}"

# Show current version
version:
    @echo "{{ version }}"
```

## Common Mistakes

**Wrong: Putting `[confirm]` on every validation step**
```just
[confirm("Run lint?")]
lint:
    cargo fmt --check
```
What happens: The developer is prompted at every step. This trains them to type "yes" reflexively, defeating the purpose of confirmation. Confirmation fatigue makes the real gate (publish) less effective.
Fix: Use `[confirm]` only on the final irreversible action (release, deploy, destroy). Let validation steps run automatically as dependencies.

**Wrong: Not separating dry-run from the real release**
```just
release dry_run='false':
    # ...build steps...
    {{ if dry_run == "false" { "git tag ..." } else { "echo 'Skipping tag'" } }}
```
What happens: A typo in the flag (`dry_run=flase`) silently runs the real release. Boolean-as-string is fragile.
Fix: Use separate recipes (`dry-run` and `release`) with shared dependencies. The structure makes the behavior explicit and untypeable wrong.

## Verify What You Learned

```bash
# Show current version from VERSION file
just version
# Expected: 1.0.0

# Run the full dry-run pipeline
just dry-run
# Expected: All validations + build run, ends with "DRY RUN complete"

# Bump version
just bump minor
just version
# Expected: 1.1.0

# Verify pre-release catches a dirty tree
echo "dirty" >> somefile.txt
just verify-clean
# Expected: "Working tree is dirty" error

# Verify tool check
just verify-tools
# Expected: "All tools present" (or clear error naming the missing tool)
```

## What's Next

The next exercise ([25. Full-Stack Hot Reload](../05-full-stack-hot-reload/05-full-stack-hot-reload.md)) builds a development environment orchestrating backend, frontend, and database services with hot reload and migration management.

## Summary

- `read("VERSION")` + `trim()` reads version from a single source of truth
- `require()` fails fast with a clear message if a tool is missing
- Validation chain: `verify-branch → verify-clean → verify-no-tag → lint → test → build`
- `[confirm]` gates only the irreversible action, not intermediate steps
- Private recipes (`_tag`, `_push`) encapsulate dangerous operations
- Dry-run is a separate recipe sharing dependencies, not a boolean flag
- Color constants improve terminal readability for pass/fail feedback

## Reference

- [Just require() Function](https://just.systems/man/en/functions.html)
- [Just Confirm Attribute](https://just.systems/man/en/attributes.html)
- [Just read() and trim()](https://just.systems/man/en/functions.html)
- [Just Private Recipes](https://just.systems/man/en/private-recipes.html)

## Additional Resources

- [Semantic Versioning Specification](https://semver.org/)
- [Git Tagging Best Practices](https://git-scm.com/book/en/v2/Git-Basics-Tagging)

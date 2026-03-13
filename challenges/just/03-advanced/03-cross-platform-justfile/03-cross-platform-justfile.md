# 23. Cross-Platform Justfile

<!--
difficulty: advanced
concepts:
  - os() / arch() / os_family() functions
  - platform-specific attributes [linux] [macos] [windows]
  - set windows-shell
  - dynamic artifact naming
  - cross-platform open and path handling
tools: [just]
estimated_time: 40 minutes
bloom_level: analyze
prerequisites:
  - just basics (recipes, variables, conditional expressions)
  - familiarity with at least two OS environments
  - understanding of shell differences (bash vs PowerShell)
-->

## Prerequisites

| Tool | Minimum Version | Check Command |
|------|----------------|---------------|
| just | 1.25+ | `just --version` |

## Learning Objectives

- **Analyze** how `os()`, `arch()`, and `os_family()` functions enable platform-aware recipe logic
- **Differentiate** between platform attributes (`[linux]`, `[macos]`, `[windows]`) and conditional expressions for selecting behavior
- **Design** a justfile that produces correct artifacts and uses correct commands across all major platforms

## Why Cross-Platform Justfiles

Most development teams work across macOS laptops, Linux CI servers, and occasionally Windows machines. Shell scripts break across these boundaries — `sed -i` behaves differently on macOS vs Linux, `open` vs `xdg-open` vs `start` for launching browsers, and path separators vary. Just provides built-in functions and attributes that make platform-aware recipes clean and maintainable.

The platform attributes (`[linux]`, `[macos]`, `[windows]`) let you define multiple versions of the same recipe name, and Just automatically selects the correct one for the current OS. For finer control, `os()`, `arch()`, and `os_family()` return strings you can use in conditional expressions and variable interpolation. Combined with `set windows-shell`, you can even switch to PowerShell on Windows while keeping bash everywhere else.

This matters most for build systems that produce platform-specific artifacts (binaries, packages, installers) and development workflows that must work identically on every developer's machine.

## Step 1 -- Platform Detection Variables

Start with a justfile that detects and displays platform information.

```just
# justfile

set windows-shell := ["powershell.exe", "-NoLogo", "-Command"]

# Platform info
current_os      := os()
current_arch    := arch()
current_family  := os_family()

# Dynamic artifact naming
bin_suffix := if os_family() == "windows" { ".exe" } else { "" }
archive_ext := if os_family() == "windows" { ".zip" } else { ".tar.gz" }
lib_prefix := if os_family() == "windows" { "" } else { "lib" }
lib_ext := if os() == "macos" { ".dylib" } else if os() == "windows" { ".dll" } else { ".so" }

project := "myapp"
version := "1.0.0"
artifact := project + "-" + version + "-" + current_os + "-" + current_arch + archive_ext

# Display detected platform information
info:
    @echo "OS:       {{ current_os }}"
    @echo "Arch:     {{ current_arch }}"
    @echo "Family:   {{ current_family }}"
    @echo "Binary:   {{ project }}{{ bin_suffix }}"
    @echo "Archive:  {{ artifact }}"
    @echo "Library:  {{ lib_prefix }}{{ project }}{{ lib_ext }}"
```

Run `just info` and observe the detected values. The `os()` function returns `linux`, `macos`, or `windows`. The `arch()` function returns `x86_64`, `aarch64`, etc.

## Step 2 -- Platform-Specific Recipes with Attributes

When a recipe needs completely different commands per platform, use attributes. Just selects the matching recipe automatically.

```just
# Open a directory in the system file manager
[macos]
open-dir path='.':
    open "{{ path }}"

[linux]
open-dir path='.':
    xdg-open "{{ path }}"

[windows]
open-dir path='.':
    explorer "{{ path }}"

# Copy text to clipboard
[macos]
clip text:
    @echo "{{ text }}" | pbcopy
    @echo "Copied to clipboard"

[linux]
clip text:
    @echo "{{ text }}" | xclip -selection clipboard
    @echo "Copied to clipboard"

[windows]
clip text:
    Set-Clipboard "{{ text }}"
    Write-Output "Copied to clipboard"
```

Notice: each recipe name appears multiple times, once per platform attribute. Just does not error — it picks the one matching the current OS and ignores the rest.

## Step 3 -- Conditional Expressions for Minor Differences

When the recipe is mostly the same but a single command or flag differs, use inline conditionals instead of full duplicates.

```just
# Install project dependencies
install-deps:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "Installing dependencies for {{ current_os }}..."

    # Package manager differs by platform
    {{ if os() == "macos" { "brew install protobuf cmake" } else if os() == "linux" { "sudo apt-get install -y protobuf-compiler cmake" } else { "echo 'Install protobuf and cmake manually on Windows'" } }}

    echo "Dependencies installed"

# Find and kill a process by port
kill-port port:
    {{ if os_family() == "unix" { "lsof -ti:" + port + " | xargs -r kill -9" } else { "Stop-Process -Id (Get-NetTCPConnection -LocalPort " + port + ").OwningProcess -Force" } }}
    @echo "Killed process on port {{ port }}"
```

The inline `if` expressions are evaluated before the recipe runs, so the non-matching branches are never executed. This is variable interpolation, not shell logic.

## Step 4 -- Platform-Aware Build System

```just
# Build the project for current platform
build profile='release':
    @echo "Building {{ project }} for {{ current_os }}-{{ current_arch }}..."
    cargo build --profile {{ profile }}
    @echo "Output: target/{{ profile }}/{{ project }}{{ bin_suffix }}"

# Build for a specific target triple
build-target target profile='release':
    cargo build --profile {{ profile }} --target {{ target }}

# Package the built artifact into a distributable archive
package: (build "release")
    #!/usr/bin/env bash
    set -euo pipefail
    binary="target/release/{{ project }}{{ bin_suffix }}"
    archive="{{ artifact }}"

    echo "Packaging $binary → $archive"

    if [[ "{{ os_family() }}" == "windows" ]]; then
        # zip on Windows (or use PowerShell via Compress-Archive)
        zip "$archive" "$binary"
    else
        tar czf "$archive" -C target/release "{{ project }}{{ bin_suffix }}"
    fi

    echo "Created $archive ($(du -h "$archive" | cut -f1))"

# Cross-compile for all supported targets
package-all:
    #!/usr/bin/env bash
    set -euo pipefail
    targets=(
        "x86_64-unknown-linux-gnu"
        "aarch64-unknown-linux-gnu"
        "x86_64-apple-darwin"
        "aarch64-apple-darwin"
        "x86_64-pc-windows-msvc"
    )
    for target in "${targets[@]}"; do
        echo "Building for $target..."
        just build-target "$target"
    done
    echo "All targets built"
```

## Step 5 -- Platform-Aware Development Recipes

```just
# Start development server and open browser
dev:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "Starting dev server on http://localhost:3000"
    # Open browser after a short delay
    (sleep 2 && just open-url "http://localhost:3000") &
    cargo watch -x run

# Open a URL in the default browser
[macos]
open-url url:
    open "{{ url }}"

[linux]
open-url url:
    xdg-open "{{ url }}"

[windows]
open-url url:
    Start-Process "{{ url }}"

# Check if required tools are installed
doctor:
    #!/usr/bin/env bash
    set -euo pipefail
    ok=true
    check() {
        if command -v "$1" &>/dev/null; then
            printf "  ✓ %-15s %s\n" "$1" "$(eval "$2")"
        else
            printf "  ✗ %-15s NOT FOUND\n" "$1"
            ok=false
        fi
    }
    echo "Checking tools for {{ current_os }}-{{ current_arch }}..."
    check "just"   "just --version"
    check "cargo"  "cargo --version"
    check "git"    "git --version"

    # Platform-specific tools
    {{ if os() == "macos" { "check \"brew\" \"brew --version | head -1\"" } else if os() == "linux" { "check \"apt\" \"apt --version\"" } else { "" } }}

    $ok && echo "All tools present" || echo "Some tools missing"
```

## Step 6 -- Handling Path Separators

Path separators rarely cause issues in justfiles because Just uses forward slashes internally. However, when passing paths to native Windows tools, you may need conversion.

```just
# Normalize a path for the current OS
_normalize path:
    {{ if os_family() == "windows" { "echo '" + replace(path, "/", "\\") + "'" } else { "echo '" + path + "'" } }}

# Show where build artifacts live
artifact-dir:
    @echo "Artifacts: {{ join("target", "release", project + bin_suffix) }}"
```

The `join()` function handles path construction. The `replace()` function can convert separators when needed for Windows-native tools.

## Common Mistakes

**Wrong: Using shell-level OS detection instead of Just's built-in functions**
```just
build:
    #!/usr/bin/env bash
    if [[ "$(uname)" == "Darwin" ]]; then
        # macOS logic
    fi
```
What happens: This works on Unix but breaks on Windows (no bash by default, no `uname`). The shebang line itself fails on Windows unless you have WSL or Git Bash.
Fix: Use Just's `os()` function and platform attributes. They work before any shell is invoked. For recipes that need PowerShell on Windows, `set windows-shell` ensures the right interpreter is used.

**Wrong: Defining platform attributes on only some platforms**
```just
[macos]
open path:
    open {{ path }}
# Forgot [linux] and [windows] variants
```
What happens: Running `just open .` on Linux or Windows produces an error — no matching recipe found. The error message is confusing because the recipe appears in `just --list`.
Fix: Always define variants for all platforms your team uses, or provide a fallback using `os_family() == "unix"` logic.

## Verify What You Learned

```bash
# Display platform info
just info
# Expected: OS/arch/family details with correctly suffixed artifact name

# Open current directory in file manager
just open-dir
# Expected: Finder (macOS), Nautilus/Dolphin (Linux), or Explorer (Windows) opens

# Show artifact directory with correct binary name
just artifact-dir
# Expected: target/release/myapp (Unix) or target\release\myapp.exe (Windows concept)

# Run the doctor check
just doctor
# Expected: tool presence check with platform-specific entries
```

## What's Next

The next exercise ([24. Release Pipeline](../04-release-pipeline/04-release-pipeline.md)) builds a complete release workflow with version management, pre-release validation chains, and git tag creation.

## Summary

- `os()` returns `linux`, `macos`, or `windows`; `arch()` returns `x86_64`, `aarch64`, etc.
- `os_family()` returns `unix` or `windows` for broader checks
- Platform attributes (`[linux]`, `[macos]`, `[windows]`) select recipe variants automatically
- `set windows-shell` configures PowerShell for Windows recipes
- Inline `if os() == ...` conditionals handle minor platform differences without duplicating recipes
- `join()` and `replace()` help with path construction across platforms
- Always provide variants for all platforms your team targets

## Reference

- [Just Built-in Functions](https://just.systems/man/en/functions.html)
- [Just Platform Attributes](https://just.systems/man/en/attributes.html)
- [Just Settings: windows-shell](https://just.systems/man/en/settings.html)

## Additional Resources

- [Cross-Platform Development Best Practices](https://doc.rust-lang.org/nightly/rustc/platform-support.html)
- [Just GitHub Discussions: Cross-Platform](https://github.com/casey/just/discussions)

# 36. Build Orchestrator with Caching

<!--
difficulty: advanced
concepts: [content-addressable-caching, sha256-change-detection, build-dag, multi-platform-matrix, conditional-rebuilds, artifact-collection]
tools: [just]
estimated_time: 1h
bloom_level: create
prerequisites: [shebang-recipes, built-in-functions, conditional-expressions, recipe-dependencies]
-->

## Prerequisites

- just >= 1.38.0 (for `sha256_file()`, `path_exists()`)
- bash, sha256sum (or shasum on macOS)
- Optional: docker (for multi-platform builds)

## Learning Objectives

- **Architect** a build DAG expressed as just recipe dependencies with caching at each node
- **Create** a content-addressable cache using `sha256_file()` to skip unchanged builds
- **Design** a multi-platform build matrix that parallelizes across OS and architecture combinations

## Why Build Orchestration with Caching

Rebuilding everything on every change is the simplest approach and the slowest. Real build systems like Bazel and Nix use content hashing to skip work when inputs haven't changed. You can apply the same principle in a justfile: hash the source files, compare to a stored hash, and skip the build if nothing changed. This transforms a 5-minute full rebuild into a 10-second incremental one.

## The Challenge

Build a justfile that orchestrates a multi-stage build pipeline with content-addressable caching. Implement a dependency chain (codegen -> compile -> test -> package -> publish), hash-based skip logic at each stage, a multi-platform build matrix (linux+macos times amd64+arm64), and artifact collection into a `dist/` directory. The cache should survive across runs and be invalidatable on demand.

## Solution

```justfile
# file: justfile

set shell := ["bash", "-euo", "pipefail", "-c"]

project := "orchestrator-demo"
version := `git describe --tags --always 2>/dev/null || echo "0.1.0-dev"`
cache_dir := ".build-cache"
dist_dir := "dist"

platforms := "linux-amd64 linux-arm64 darwin-amd64 darwin-arm64"

# ─── Cache Helpers (private) ───────────────────────────────

[private]
_cache-init:
    @mkdir -p {{ cache_dir }} {{ dist_dir }}

[private]
_hash-sources dir pattern:
    #!/usr/bin/env bash
    set -euo pipefail
    # Compute a composite hash of all matching files in a directory
    if [ -d "{{ dir }}" ]; then
        find "{{ dir }}" -name "{{ pattern }}" -type f | sort | while read -r f; do
            if command -v sha256sum >/dev/null 2>&1; then
                sha256sum "$f"
            else
                shasum -a 256 "$f"
            fi
        done | if command -v sha256sum >/dev/null 2>&1; then
            sha256sum | cut -d' ' -f1
        else
            shasum -a 256 | cut -d' ' -f1
        fi
    else
        echo "no-source"
    fi

[private]
_cache-check stage hash:
    #!/usr/bin/env bash
    set -euo pipefail
    cache_file="{{ cache_dir }}/{{ stage }}.hash"
    if [ -f "$cache_file" ] && [ "$(cat "$cache_file")" = "{{ hash }}" ]; then
        printf '\033[33mSKIP:\033[0m  %s (unchanged, hash: %.12s...)\n' "{{ stage }}" "{{ hash }}"
        exit 0
    fi
    # Return non-zero to indicate cache miss
    exit 1

[private]
_cache-store stage hash:
    @echo "{{ hash }}" > {{ cache_dir }}/{{ stage }}.hash

# ─── Build Stages ──────────────────────────────────────────

[group('build')]
[doc('Stage 1: Generate code from schema definitions')]
codegen: _cache-init
    #!/usr/bin/env bash
    set -euo pipefail
    stage="codegen"

    # Simulate source directory
    mkdir -p src/schema
    cat > src/schema/api.yaml <<'SEOF'
    openapi: "3.0.0"
    info:
      title: Demo API
      version: "1.0"
    paths:
      /health:
        get:
          operationId: healthCheck
          responses:
            "200":
              description: OK
    SEOF

    hash=$(just _hash-sources src/schema "*.yaml")
    if just _cache-check "$stage" "$hash" 2>/dev/null; then
        exit 0
    fi

    printf '\033[36mBUILD:\033[0m %s (hash: %.12s...)\n' "$stage" "$hash"

    # Simulated codegen
    mkdir -p src/generated
    cat > src/generated/api.rs <<'GEOF'
    // Auto-generated from schema — do not edit
    pub fn health_check() -> &'static str {
        "OK"
    }
    GEOF

    just _cache-store "$stage" "$hash"
    printf '\033[32m  OK:\033[0m  %s complete\n' "$stage"

[group('build')]
[doc('Stage 2: Compile source code (depends on codegen)')]
compile: codegen
    #!/usr/bin/env bash
    set -euo pipefail
    stage="compile"

    hash=$(just _hash-sources src "*.rs")
    if just _cache-check "$stage" "$hash" 2>/dev/null; then
        exit 0
    fi

    printf '\033[36mBUILD:\033[0m %s (hash: %.12s...)\n' "$stage" "$hash"

    # Simulated compile for current platform
    mkdir -p target/release
    cat > target/release/{{ project }} <<'BEOF'
    #!/bin/sh
    echo "{{ project }} running"
    BEOF
    chmod +x target/release/{{ project }}

    just _cache-store "$stage" "$hash"
    printf '\033[32m  OK:\033[0m  %s complete\n' "$stage"

[group('build')]
[doc('Stage 3: Run tests (depends on compile)')]
test: compile
    #!/usr/bin/env bash
    set -euo pipefail
    stage="test"

    # Test hash = source hash + test file hash
    src_hash=$(just _hash-sources src "*.rs")
    test_hash=$(just _hash-sources tests "*.rs" 2>/dev/null || echo "no-tests")
    combined="${src_hash}-${test_hash}"
    hash=$(echo "$combined" | if command -v sha256sum >/dev/null 2>&1; then sha256sum | cut -d' ' -f1; else shasum -a 256 | cut -d' ' -f1; fi)

    if just _cache-check "$stage" "$hash" 2>/dev/null; then
        exit 0
    fi

    printf '\033[36mBUILD:\033[0m %s (hash: %.12s...)\n' "$stage" "$hash"

    # Simulated test run
    echo "  Running 42 tests..."
    echo "  42 passed, 0 failed"

    just _cache-store "$stage" "$hash"
    printf '\033[32m  OK:\033[0m  %s complete\n' "$stage"

[group('build')]
[doc('Stage 4: Package artifacts for distribution (depends on test)')]
package: test
    #!/usr/bin/env bash
    set -euo pipefail
    stage="package"

    hash=$(just _hash-sources target/release "*" 2>/dev/null || echo "no-binary")
    if just _cache-check "$stage" "$hash" 2>/dev/null; then
        exit 0
    fi

    printf '\033[36mBUILD:\033[0m %s (hash: %.12s...)\n' "$stage" "$hash"

    mkdir -p {{ dist_dir }}
    tar czf {{ dist_dir }}/{{ project }}-{{ version }}.tar.gz \
        -C target/release {{ project }} 2>/dev/null || true

    echo "{{ version }}" > {{ dist_dir }}/VERSION
    echo "{{ project }}-{{ version }}.tar.gz" > {{ dist_dir }}/MANIFEST

    just _cache-store "$stage" "$hash"
    printf '\033[32m  OK:\033[0m  %s complete -> {{ dist_dir }}/\n' "$stage"

# ─── Multi-Platform Build Matrix ───────────────────────────

[group('matrix')]
[doc('Build for all platforms in the matrix')]
build-all: codegen
    #!/usr/bin/env bash
    set -euo pipefail
    echo "=== Multi-Platform Build Matrix ==="
    echo ""

    platforms="{{ platforms }}"
    total=0
    skipped=0
    built=0

    for platform in $platforms; do
        os=$(echo "$platform" | cut -d- -f1)
        arch=$(echo "$platform" | cut -d- -f2)
        total=$((total + 1))

        just build-platform "$os" "$arch"
        result=$?
        if [ $result -eq 0 ]; then
            built=$((built + 1))
        fi
    done

    echo ""
    echo "=== Matrix Summary ==="
    echo "  Total:   $total"
    echo "  Built:   $built"

[group('matrix')]
[doc('Build for a specific os-arch combination')]
build-platform os arch: _cache-init
    #!/usr/bin/env bash
    set -euo pipefail
    platform="{{ os }}-{{ arch }}"
    stage="platform-${platform}"

    src_hash=$(just _hash-sources src "*.rs")
    if just _cache-check "$stage" "$src_hash" 2>/dev/null; then
        exit 0
    fi

    printf '\033[36mBUILD:\033[0m %-20s' "$platform"

    # Simulated cross-compile
    out_dir="{{ dist_dir }}/${platform}"
    mkdir -p "$out_dir"

    cat > "$out_dir/{{ project }}" <<PEOF
    #!/bin/sh
    echo "{{ project }} {{ version }} ({{ os }}/{{ arch }})"
    PEOF
    chmod +x "$out_dir/{{ project }}"

    # Write build metadata
    cat > "$out_dir/BUILD_INFO" <<BEOF
    project={{ project }}
    version={{ version }}
    os={{ os }}
    arch={{ arch }}
    built=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    source_hash=${src_hash}
    BEOF

    just _cache-store "$stage" "$src_hash"
    printf ' \033[32mdone\033[0m\n'

# ─── Artifact Collection ───────────────────────────────────

[group('release')]
[doc('Collect all platform artifacts into a release bundle')]
collect: build-all package
    #!/usr/bin/env bash
    set -euo pipefail
    echo "=== Collecting Artifacts ==="

    release_dir="{{ dist_dir }}/release-{{ version }}"
    mkdir -p "$release_dir"

    # Copy all platform binaries
    for platform in {{ platforms }}; do
        src="{{ dist_dir }}/${platform}/{{ project }}"
        if [ -f "$src" ]; then
            cp "$src" "$release_dir/{{ project }}-${platform}"
            printf '  \033[32m+\033[0m {{ project }}-%s\n' "$platform"
        fi
    done

    # Generate checksums
    cd "$release_dir"
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum {{ project }}-* > SHA256SUMS
    else
        shasum -a 256 {{ project }}-* > SHA256SUMS
    fi

    echo ""
    echo "Release artifacts:"
    ls -la "$release_dir/"
    printf '\n\033[32mRelease {{ version }} ready: %s\n\033[0m' "$release_dir"

# ─── Cache Management ──────────────────────────────────────

[group('cache')]
[doc('Show cache status and hit/miss statistics')]
cache-status:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "=== Build Cache ==="
    echo ""
    if [ -d {{ cache_dir }} ]; then
        count=$(ls {{ cache_dir }}/*.hash 2>/dev/null | wc -l | tr -d ' ')
        echo "  Cached stages: $count"
        echo ""
        for f in {{ cache_dir }}/*.hash; do
            if [ -f "$f" ]; then
                stage=$(basename "$f" .hash)
                hash=$(cat "$f")
                printf '  %-25s %.16s...\n' "$stage" "$hash"
            fi
        done
    else
        echo "  No cache directory found."
    fi

[group('cache')]
[doc('Invalidate cache for a specific stage (or "all")')]
cache-invalidate stage="all":
    #!/usr/bin/env bash
    set -euo pipefail
    if [ "{{ stage }}" = "all" ]; then
        rm -rf {{ cache_dir }}
        printf '\033[32mAll caches invalidated.\033[0m\n'
    else
        rm -f "{{ cache_dir }}/{{ stage }}.hash"
        printf '\033[32mCache invalidated for: {{ stage }}\033[0m\n'
    fi

# ─── Full Pipeline ─────────────────────────────────────────

[group('build')]
[doc('Run the complete build pipeline: codegen -> compile -> test -> package')]
build: package
    @printf '\033[32mFull build pipeline complete.\033[0m\n'

[group('build')]
[doc('Clean all build artifacts, cache, and dist')]
clean:
    rm -rf {{ cache_dir }} {{ dist_dir }} target/ src/generated/ src/schema/
    @printf '\033[32mAll build artifacts cleaned.\033[0m\n'
```

## Verify What You Learned

```bash
# Run the full build pipeline (all stages execute)
just build

# Run again — all stages should be skipped (cached)
just build

# Invalidate a single stage and rebuild
just cache-invalidate compile
just build

# View cache status
just cache-status

# Build for all platforms
just build-all

# Collect release artifacts with checksums
just collect

# Inspect the release bundle
ls -la dist/release-*/
cat dist/release-*/SHA256SUMS
```

## What's Next

Continue to [Exercise 37: Testing Framework with Just](../07-testing-framework-with-just/07-testing-framework-with-just.md) to build a test runner that uses just recipes as test cases.

## Summary

- Content-addressable caching uses `sha256` hashing to detect source changes and skip unchanged stages
- A build DAG is expressed naturally as just recipe dependencies: `package: test: compile: codegen`
- Cache hash files in `.build-cache/` persist across runs and can be selectively invalidated
- Multi-platform builds iterate over an OS-architecture matrix with per-platform caching
- Artifact collection gathers cross-platform binaries and generates verification checksums

## Reference

- [sha256_file() function](https://just.systems/man/en/built-in-functions.html)
- [path_exists() function](https://just.systems/man/en/built-in-functions.html)
- [Recipe dependencies](https://just.systems/man/en/recipe-dependencies.html)

## Additional Resources

- [Content-addressable storage concepts](https://en.wikipedia.org/wiki/Content-addressable_storage)
- [Bazel build caching philosophy](https://bazel.build/remote/caching)

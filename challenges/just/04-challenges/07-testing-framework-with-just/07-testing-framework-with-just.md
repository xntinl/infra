# 37. Testing Framework with Just

<!--
difficulty: advanced
concepts: [test-runner, assertion-helpers, result-aggregation, ansi-output, setup-teardown, exit-code-handling]
tools: [just]
estimated_time: 1h
bloom_level: create
prerequisites: [shebang-recipes, private-recipes, recipe-parameters, error-handling]
-->

## Prerequisites

- just >= 1.38.0
- bash with ANSI color support
- Standard Unix tools (diff, grep, wc)

## Learning Objectives

- **Architect** a test framework where just recipes serve as both test runner and test cases
- **Create** reusable assertion helpers as private recipes with clear pass/fail output
- **Design** result aggregation that collects outcomes across independent test recipes and reports a summary

## Why a Testing Framework with Just

Infrastructure code -- Terraform modules, Kubernetes manifests, config files -- often lacks a proper test harness. Unit test frameworks target application code, not YAML and HCL. A just-based test framework fills that gap: each test is a recipe, assertions are private helper recipes, and the runner discovers and executes all `test_*` recipes automatically. No additional dependencies, no test framework to learn, just recipes.

## The Challenge

Build a justfile that acts as a test framework. Implement: a test runner that discovers all recipes matching `test_*`, assertion helpers (`_assert_eq`, `_assert_contains`, `_assert_file_exists`, `_assert_exit_code`), setup/teardown patterns, ANSI color output for pass/fail, result counting with a final summary, and a non-zero exit code if any test fails. Use this framework to test an infrastructure configuration directory.

## Solution

```justfile
# file: justfile

set shell := ["bash", "-euo", "pipefail", "-c"]

test_dir := ".test-workspace"
results_file := ".test-results"

# ═══════════════════════════════════════════════════════════
# Test Runner
# ═══════════════════════════════════════════════════════════

[group('runner')]
[doc('Run all test_* recipes and report results')]
test: _setup
    #!/usr/bin/env bash
    set -uo pipefail

    results_file="{{ results_file }}"
    > "$results_file"

    # Discover all test recipes
    tests=$(just --summary 2>/dev/null | tr ' ' '\n' | grep '^test_' | sort)

    if [ -z "$tests" ]; then
        printf '\033[33mNo test_* recipes found.\033[0m\n'
        exit 0
    fi

    total=$(echo "$tests" | wc -l | tr -d ' ')
    passed=0
    failed=0
    skipped=0

    printf '\033[1m═══════════════════════════════════════════\033[0m\n'
    printf '\033[1m  Test Suite — %d test(s) discovered\033[0m\n' "$total"
    printf '\033[1m═══════════════════════════════════════════\033[0m\n\n'

    for test_name in $tests; do
        printf '\033[36m  RUN\033[0m  %s\n' "$test_name"

        # Capture output and exit code
        output=$(just "$test_name" 2>&1) && exit_code=0 || exit_code=$?

        if [ $exit_code -eq 0 ]; then
            printf '\033[32m PASS\033[0m  %s\n' "$test_name"
            echo "PASS:$test_name" >> "$results_file"
            passed=$((passed + 1))
        elif [ $exit_code -eq 77 ]; then
            printf '\033[33m SKIP\033[0m  %s\n' "$test_name"
            echo "SKIP:$test_name" >> "$results_file"
            skipped=$((skipped + 1))
        else
            printf '\033[31m FAIL\033[0m  %s (exit %d)\n' "$test_name" "$exit_code"
            # Print failure output indented
            echo "$output" | sed 's/^/         /'
            echo "FAIL:$test_name" >> "$results_file"
            failed=$((failed + 1))
        fi
    done

    # Summary
    echo ""
    printf '\033[1m═══════════════════════════════════════════\033[0m\n'
    if [ $failed -eq 0 ]; then
        printf '\033[32m  RESULT: ALL TESTS PASSED\033[0m\n'
    else
        printf '\033[31m  RESULT: %d TEST(S) FAILED\033[0m\n' "$failed"
    fi
    printf '  Total: %d  Passed: %d  Failed: %d  Skipped: %d\n' \
        "$total" "$passed" "$failed" "$skipped"
    printf '\033[1m═══════════════════════════════════════════\033[0m\n'

    # Teardown
    just _teardown 2>/dev/null || true

    # Exit with failure if any test failed
    [ $failed -eq 0 ]

[group('runner')]
[doc('Run a single test by name')]
test-one name: _setup
    #!/usr/bin/env bash
    set -uo pipefail
    printf '\033[36m  RUN\033[0m  {{ name }}\n'
    just {{ name }} 2>&1 && exit_code=0 || exit_code=$?
    just _teardown 2>/dev/null || true
    if [ $exit_code -eq 0 ]; then
        printf '\033[32m PASS\033[0m  {{ name }}\n'
    else
        printf '\033[31m FAIL\033[0m  {{ name }} (exit %d)\n' $exit_code
        exit $exit_code
    fi

[group('runner')]
[doc('List all discovered test recipes')]
test-list:
    #!/usr/bin/env bash
    tests=$(just --summary 2>/dev/null | tr ' ' '\n' | grep '^test_' | sort)
    count=$(echo "$tests" | wc -l | tr -d ' ')
    echo "Discovered $count test(s):"
    echo "$tests" | sed 's/^/  /'

# ═══════════════════════════════════════════════════════════
# Assertion Helpers
# ═══════════════════════════════════════════════════════════

[private]
_assert_eq actual expected msg="values should be equal":
    #!/usr/bin/env bash
    if [ "{{ actual }}" = "{{ expected }}" ]; then
        printf '    \033[32m✓\033[0m %s\n' "{{ msg }}"
    else
        printf '    \033[31m✗\033[0m %s\n' "{{ msg }}"
        printf '      expected: %s\n' "{{ expected }}"
        printf '      actual:   %s\n' "{{ actual }}"
        exit 1
    fi

[private]
_assert_neq actual expected msg="values should not be equal":
    #!/usr/bin/env bash
    if [ "{{ actual }}" != "{{ expected }}" ]; then
        printf '    \033[32m✓\033[0m %s\n' "{{ msg }}"
    else
        printf '    \033[31m✗\033[0m %s\n' "{{ msg }}"
        printf '      both values: %s\n' "{{ actual }}"
        exit 1
    fi

[private]
_assert_contains haystack needle msg="should contain substring":
    #!/usr/bin/env bash
    if echo "{{ haystack }}" | grep -qF "{{ needle }}"; then
        printf '    \033[32m✓\033[0m %s\n' "{{ msg }}"
    else
        printf '    \033[31m✗\033[0m %s\n' "{{ msg }}"
        printf '      string:   %s\n' "{{ haystack }}"
        printf '      expected:  %s\n' "{{ needle }}"
        exit 1
    fi

[private]
_assert_file_exists path msg="file should exist":
    #!/usr/bin/env bash
    if [ -f "{{ path }}" ]; then
        printf '    \033[32m✓\033[0m %s (%s)\n' "{{ msg }}" "{{ path }}"
    else
        printf '    \033[31m✗\033[0m %s (%s)\n' "{{ msg }}" "{{ path }}"
        exit 1
    fi

[private]
_assert_dir_exists path msg="directory should exist":
    #!/usr/bin/env bash
    if [ -d "{{ path }}" ]; then
        printf '    \033[32m✓\033[0m %s (%s)\n' "{{ msg }}" "{{ path }}"
    else
        printf '    \033[31m✗\033[0m %s (%s)\n' "{{ msg }}" "{{ path }}"
        exit 1
    fi

[private]
_assert_file_contains path pattern msg="file should contain pattern":
    #!/usr/bin/env bash
    if grep -q "{{ pattern }}" "{{ path }}" 2>/dev/null; then
        printf '    \033[32m✓\033[0m %s\n' "{{ msg }}"
    else
        printf '    \033[31m✗\033[0m %s\n' "{{ msg }}"
        printf '      file:    %s\n' "{{ path }}"
        printf '      pattern: %s\n' "{{ pattern }}"
        exit 1
    fi

[private]
_assert_exit_code command expected_code msg="command should exit with expected code":
    #!/usr/bin/env bash
    set +e
    eval "{{ command }}" >/dev/null 2>&1
    actual=$?
    set -e
    if [ "$actual" -eq "{{ expected_code }}" ]; then
        printf '    \033[32m✓\033[0m %s (exit %d)\n' "{{ msg }}" "$actual"
    else
        printf '    \033[31m✗\033[0m %s\n' "{{ msg }}"
        printf '      expected exit: %s\n' "{{ expected_code }}"
        printf '      actual exit:   %d\n' "$actual"
        exit 1
    fi

# ═══════════════════════════════════════════════════════════
# Setup / Teardown
# ═══════════════════════════════════════════════════════════

[private]
_setup:
    #!/usr/bin/env bash
    set -euo pipefail
    mkdir -p {{ test_dir }}
    # Create sample infrastructure config to test against
    mkdir -p {{ test_dir }}/config

    cat > {{ test_dir }}/config/app.yaml <<'CEOF'
    app:
      name: my-service
      version: "1.2.0"
      port: 8080
      replicas: 3
      environment: production
      health_check:
        path: /health
        interval: 30s
    CEOF

    cat > {{ test_dir }}/config/database.yaml <<'DEOF'
    database:
      host: db.internal
      port: 5432
      name: mydb
      pool_size: 20
      ssl: true
    DEOF

    cat > {{ test_dir }}/config/deploy.env <<'EEOF'
    REGION=us-east-1
    CLUSTER=prod-cluster
    NAMESPACE=default
    IMAGE_TAG=1.2.0
    EEOF

[private]
_teardown:
    @rm -rf {{ test_dir }} {{ results_file }}

# ═══════════════════════════════════════════════════════════
# Test Cases — Infrastructure Config Validation
# ═══════════════════════════════════════════════════════════

[private]
test_config_files_exist:
    just _assert_file_exists "{{ test_dir }}/config/app.yaml" "app config exists"
    just _assert_file_exists "{{ test_dir }}/config/database.yaml" "database config exists"
    just _assert_file_exists "{{ test_dir }}/config/deploy.env" "deploy env file exists"

[private]
test_config_directory_structure:
    just _assert_dir_exists "{{ test_dir }}/config" "config directory exists"

[private]
test_app_config_has_required_fields:
    just _assert_file_contains "{{ test_dir }}/config/app.yaml" "name:" "app.yaml has name field"
    just _assert_file_contains "{{ test_dir }}/config/app.yaml" "version:" "app.yaml has version field"
    just _assert_file_contains "{{ test_dir }}/config/app.yaml" "port:" "app.yaml has port field"
    just _assert_file_contains "{{ test_dir }}/config/app.yaml" "health_check:" "app.yaml has health_check"

[private]
test_database_config_security:
    just _assert_file_contains "{{ test_dir }}/config/database.yaml" "ssl: true" "database SSL is enabled"
    just _assert_file_contains "{{ test_dir }}/config/database.yaml" "port: 5432" "database uses standard port"

[private]
test_deploy_env_variables:
    #!/usr/bin/env bash
    set -euo pipefail
    required_vars="REGION CLUSTER NAMESPACE IMAGE_TAG"
    for var in $required_vars; do
        if grep -q "^${var}=" {{ test_dir }}/config/deploy.env; then
            printf '    \033[32m✓\033[0m %s is defined\n' "$var"
        else
            printf '    \033[31m✗\033[0m %s is missing\n' "$var"
            exit 1
        fi
    done

[private]
test_app_version_matches_image_tag:
    #!/usr/bin/env bash
    set -euo pipefail
    app_version=$(grep 'version:' {{ test_dir }}/config/app.yaml | head -1 | tr -d ' "' | cut -d: -f2)
    image_tag=$(grep 'IMAGE_TAG=' {{ test_dir }}/config/deploy.env | cut -d= -f2)
    just _assert_eq "$app_version" "$image_tag" "app version matches image tag"

[private]
test_replica_count_is_valid:
    #!/usr/bin/env bash
    set -euo pipefail
    replicas=$(grep 'replicas:' {{ test_dir }}/config/app.yaml | tr -d ' ' | cut -d: -f2)
    if [ "$replicas" -ge 2 ] && [ "$replicas" -le 10 ]; then
        printf '    \033[32m✓\033[0m replica count %s is within valid range (2-10)\n' "$replicas"
    else
        printf '    \033[31m✗\033[0m replica count %s is outside valid range (2-10)\n' "$replicas"
        exit 1
    fi

[private]
test_port_is_non_privileged:
    #!/usr/bin/env bash
    set -euo pipefail
    port=$(grep 'port: ' {{ test_dir }}/config/app.yaml | head -1 | tr -d ' ' | cut -d: -f2)
    if [ "$port" -gt 1024 ]; then
        printf '    \033[32m✓\033[0m port %s is non-privileged (>1024)\n' "$port"
    else
        printf '    \033[31m✗\033[0m port %s is privileged (<=1024)\n' "$port"
        exit 1
    fi

[private]
test_no_hardcoded_passwords:
    #!/usr/bin/env bash
    set -euo pipefail
    for f in {{ test_dir }}/config/*; do
        if grep -qiE 'password|secret|token' "$f" 2>/dev/null; then
            printf '    \033[31m✗\033[0m %s contains credential-like strings\n' "$(basename "$f")"
            exit 1
        fi
    done
    printf '    \033[32m✓\033[0m no hardcoded passwords or secrets found\n'

# ═══════════════════════════════════════════════════════════
# Cleanup
# ═══════════════════════════════════════════════════════════

[group('runner')]
[doc('Remove test workspace and results')]
clean:
    @just _teardown
    @printf '\033[32mTest workspace cleaned.\033[0m\n'
```

## Verify What You Learned

```bash
# List all discovered tests
just test-list

# Run the full test suite
just test

# Run a single test in isolation
just test-one test_database_config_security

# Run again to see pass/fail output
just test

# Clean up test workspace
just clean
```

## What's Next

Continue to [Exercise 38: Zero-Downtime Deployment](../08-zero-downtime-deployment/08-zero-downtime-deployment.md) to implement blue-green and canary deployment strategies entirely in just.

## Summary

- Test recipes named `test_*` are discovered automatically using `just --summary` and pattern matching
- Private assertion helpers (`_assert_eq`, `_assert_contains`, `_assert_file_exists`) provide reusable validation with clear output
- Exit code 0 means pass, 77 means skip, anything else means fail -- matching standard test conventions
- Setup and teardown recipes create and destroy isolated test workspaces
- The runner aggregates results, prints a colored summary, and exits non-zero if any test fails
- This pattern is ideal for validating infrastructure configuration without external test frameworks

## Reference

- [Private recipes](https://just.systems/man/en/private-recipes.html)
- [Recipe parameters](https://just.systems/man/en/recipe-parameters.html)
- [--summary flag](https://just.systems/man/en/summary.html)

## Additional Resources

- [TAP (Test Anything Protocol)](https://testanything.org/)
- [BATS: Bash Automated Testing System](https://github.com/bats-core/bats-core) -- inspiration for the approach

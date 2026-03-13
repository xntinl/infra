# 44. Self-Testing Justfile

<!--
difficulty: insane
concepts: [self-validation, test-framework, stdout-capture, error-testing, idempotency, test-reporting]
tools: [just, bash, diff, tee, date]
estimated_time: 2h-4h
bloom_level: create
prerequisites: [exercises 1-38]
-->

## Prerequisites

- Completed exercises 1-38 or equivalent experience
- Solid understanding of testing principles: assertions, expected vs actual, test
  isolation
- Familiarity with exit code conventions and stderr vs stdout separation

## Learning Objectives

After completing this challenge, you will be able to:

- **Create** a self-contained test framework where a justfile validates its own recipes
  against expected behavior
- **Evaluate** recipe correctness through automated assertions covering output, exit
  codes, side effects, timing, and idempotency

## The Challenge

Build a justfile that contains its own comprehensive test suite. This is a testing
framework where the system under test and the test harness are the same file. Recipes
test other recipes: they invoke them, capture their output, compare against
expectations, and report pass/fail results. The result is a justfile you can hand to
anyone and say "run `just test` and it will prove to you that every recipe works
correctly."

The fundamental difficulty is self-reference. A justfile testing itself must invoke its
own recipes as subprocesses (`just --justfile {{justfile()}} recipe-name`), capture both
stdout and stderr separately, check exit codes, and compare all of these against
expected values. This requires careful separation between "the recipe being tested" and
"the recipe doing the testing." Test recipes must not pollute the environment of the
recipes they test, and test failures must not prevent other tests from running.

Your justfile should contain a set of "production" recipes that do real work — file
manipulation, text processing, calculations, environment variable handling, conditional
logic, parameterized behavior — and a parallel set of test recipes that validate each
production recipe. The production recipes should be non-trivial: a recipe that sorts
input, a recipe that transforms file formats, a recipe that computes checksums, a
recipe that manages a simple key-value store in a file, and so on. The test recipes
must cover multiple dimensions: correct output for valid input, correct error messages
for invalid input, correct exit codes for both success and failure cases, and correct
side effects (files created, modified, or deleted).

Beyond basic correctness, implement idempotency testing: run a recipe twice in sequence
and verify the output is identical both times. This catches recipes that depend on
hidden state, produce non-deterministic output, or include timestamps in their output
without an option to suppress them. A recipe that passes its correctness test but fails
its idempotency test has a subtle bug.

Implement timing tests as well: verify that certain recipes complete within expected
time bounds. This is useful for catching accidental O(n^2) behavior in recipes that
process lists or files, or for detecting infinite loops in recipes with conditional
logic. A recipe that should process 100 lines in under 2 seconds but takes 30 seconds
has a performance regression.

The crown jewel is the test report. After all tests run, generate an HTML report
showing: total tests, passed, failed, skipped, execution time per test, and for failed
tests, a diff between expected and actual output. The report should be self-contained
(inline CSS, no external dependencies) and openable in any browser.

## Requirements

1. Include at least 10 "production" recipes that perform real operations: file I/O,
   string manipulation, arithmetic, conditional logic, error handling, environment
   variable usage, recipe dependencies, parameterized behavior, and list processing

2. Create a `test` recipe that discovers and runs all test recipes (those prefixed with
   `test-`), tracks pass/fail counts, and exits with code 0 only if all tests pass

3. Implement an assertion helper (recipe or shell function) supporting: `assert_eq`
   (expected output), `assert_contains` (substring match), `assert_exit_code` (expected
   exit code), `assert_file_exists`, and `assert_not_contains`

4. Test stdout capture: invoke production recipes and compare their stdout against
   expected strings, supporting both exact match and pattern match (via grep)

5. Test stderr capture: invoke recipes with invalid input and verify they produce
   expected error messages on stderr — not just "some error" but specific messages

6. Test exit codes: verify that recipes exit with 0 on success and specific non-zero
   codes on expected failure conditions (e.g., 1 for user error, 2 for missing file)

7. Test side effects: verify that recipes that create, modify, or delete files produce
   the expected filesystem changes, cleaning up test artifacts after each test
   regardless of pass/fail

8. Implement idempotency tests: run a recipe twice consecutively and assert that the
   output is identical both times, catching non-deterministic behavior

9. Implement timing tests: assert that specific recipes complete within a configurable
   time threshold (e.g., under 2 seconds), reporting both the threshold and actual
   duration on failure

10. Generate an HTML test report at `test-report.html` with inline CSS, showing:
    total/pass/fail/skip counts, per-test results with duration, and unified diffs for
    failed tests

11. Support running individual test suites: `just test suite=output` runs only
    output-related tests, `just test suite=errors` runs only error-condition tests,
    `just test suite=all` runs everything

12. Implement test isolation: each test runs in a temporary directory that is created
    before and removed after the test, preventing cross-test contamination even when
    tests fail mid-execution

## Hints

- `just --justfile {{justfile()}} recipe-name args 2>stderr.tmp 1>stdout.tmp` followed
  by capturing `$?` gives you all three dimensions (stdout, stderr, exit code) for any
  recipe invocation — run this in a shell block to keep the exit code

- For the HTML report, construct it incrementally: write the header at test-suite start,
  append a `<tr>` per test, and write the footer at the end — this avoids holding the
  entire report in memory and streams results as tests complete

- `diff <(echo "$expected") <(echo "$actual")` produces a clear unified diff for
  assertion failures; capture this diff output for inclusion in the test report's
  failure details section

- Timing can be measured with the `SECONDS` bash variable: record it before and after
  recipe execution, and the difference is the elapsed time in whole seconds — for
  sub-second precision, use `date +%s%N`

- Test isolation via `mktemp -d` for each test, with `trap 'rm -rf $tmpdir' EXIT` in
  the shell block, ensures tests cannot interfere with each other even if they fail
  mid-execution

## Success Criteria

1. `just test` runs all tests and exits with code 0 when all pass, non-zero when any
   fail — with a clear summary line at the end showing pass/fail counts

2. Deliberately breaking a production recipe (e.g., changing its output) causes the
   corresponding test to fail with a clear diff showing expected vs actual

3. Tests for error conditions verify both the error message content on stderr and the
   non-zero exit code — not just one or the other

4. Idempotency tests detect if a recipe produces different output on its second run
   (e.g., due to timestamp inclusion or random values)

5. Timing tests fail if a recipe exceeds its time threshold, reporting both the
   threshold and actual duration in the failure message

6. `test-report.html` is generated after a test run and is viewable in a browser,
   showing pass/fail status, durations, and diffs for failures

7. `just test suite=output` runs only output-assertion tests, skipping all other test
   categories, and reporting the filtered count

8. Each test runs in an isolated temporary directory, and no test artifacts remain
   after the suite completes regardless of pass or fail outcomes

## Research Resources

- [Just Manual - Justfile Function](https://just.systems/man/en/chapter_43.html)
  -- `justfile()` function for self-referential invocation

- [Just Manual - Error Handling](https://just.systems/man/en/chapter_49.html)
  -- controlling what happens when a recipe line fails, essential for testing error paths

- [Bash Process Substitution](https://www.gnu.org/software/bash/manual/bash.html#Process-Substitution)
  -- `<(command)` syntax for comparing command outputs without temp files

- [Just Manual - Shell Recipes](https://just.systems/man/en/chapter_44.html)
  -- multi-line shell blocks for complex test logic with variable persistence

- [TAP (Test Anything Protocol)](https://testanything.org/)
  -- inspiration for test output formatting conventions and tooling integration

## What's Next

Proceed to exercise 45, where you will build a performance profiler that wraps any
existing justfile to measure and optimize recipe execution.

## Summary

- **Self-testing** -- a justfile that validates its own recipes through automated assertions without external test frameworks
- **Multi-dimensional assertions** -- testing stdout, stderr, exit codes, side effects, timing, and idempotency in every test
- **Test reporting** -- generating human-readable HTML reports with diffs for failed assertions

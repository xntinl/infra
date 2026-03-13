# Unit Tests: Prove Your Policies Work

## Prerequisites

- OPA CLI installed (`opa version` to verify)
- Completed exercises in `01-core-opa` and `02-rego-basics` (or equivalent familiarity with Rego syntax, packages, and rules)

## Learning Objectives

After completing this exercise, you will be able to:

- Write unit tests for Rego policies using the `test_` naming convention
- Use the `with` keyword to inject test inputs without modifying the policy
- Run tests with `opa test`, filter by name with `--run`, and read verbose output
- Test both allow and deny paths, including edge cases with missing or unexpected input

## Why Test Policies

A policy controls who can access your system. If it has a bug, a viewer could delete production resources, or an admin could get locked out during a critical deploy. A policy without tests is code in production with zero coverage -- that is why we test.

OPA ships with a built-in testing framework. You do not need to install anything extra or use an external runner. You write your tests in Rego, run them with `opa test`, and you are done.

## How Testing Works in OPA

The convention is straightforward:

- Any rule whose name starts with `test_` is a test.
- If the rule evaluates to `true`, the test passes. If it evaluates to `false` (or `undefined`), the test fails.
- Tests live in separate files (by convention `_test.rego`), but they **use the same package** as the policy they test. This is key: by sharing the package, the test has direct access to all the rules.

The `with` keyword is how you inject test data. It lets you supply values for `input` and `data` without touching the original policy. Think of it as telling OPA: "for this evaluation, pretend that `input` is this object."

```rego
allow with input as {"user": "admin", "action": "delete"}
```

That line says: "evaluate `allow` but replace `input` with that object." It is dependency injection, Rego-style.

## The Policy: Basic RBAC

We will build a role-based access control policy. Three roles, each with different permissions:

| Role | Permissions |
|---|---|
| `admin` | Can do anything (`read`, `write`, `delete`) |
| `editor` | Can `read` and `write`, but not `delete` |
| `viewer` | Can only `read` |

```rego title="policy.rego"
package authz

import rego.v1

default allow := false

allow if {
    input.role == "admin"
}

allow if {
    input.role == "editor"
    input.action in {"read", "write"}
}

allow if {
    input.role == "viewer"
    input.action == "read"
}
```

Nothing unusual: three `allow` rules, each with its own conditions. If none of them match, the default is `false`. That is deny-by-default in action.

## The Tests: Cover Every Path

Now the interesting part. We will write tests for every scenario we care about. Not just the happy paths -- also the edge cases, incomplete inputs, and roles that do not exist.

```rego title="policy_test.rego"
package authz

import rego.v1

# --- Admin: can do everything ---

test_admin_can_read if {
    allow with input as {"role": "admin", "action": "read"}
}

test_admin_can_write if {
    allow with input as {"role": "admin", "action": "write"}
}

test_admin_can_delete if {
    allow with input as {"role": "admin", "action": "delete"}
}

# --- Editor: read and write, but not delete ---

test_editor_can_read if {
    allow with input as {"role": "editor", "action": "read"}
}

test_editor_can_write if {
    allow with input as {"role": "editor", "action": "write"}
}

test_editor_cannot_delete if {
    not allow with input as {"role": "editor", "action": "delete"}
}

# --- Viewer: read only ---

test_viewer_can_read if {
    allow with input as {"role": "viewer", "action": "read"}
}

test_viewer_cannot_write if {
    not allow with input as {"role": "viewer", "action": "write"}
}

test_viewer_cannot_delete if {
    not allow with input as {"role": "viewer", "action": "delete"}
}

# --- Edge cases ---

test_unknown_role_denied if {
    not allow with input as {"role": "intern", "action": "read"}
}

test_missing_role_denied if {
    not allow with input as {"action": "read"}
}

test_empty_input_denied if {
    not allow with input as {}
}
```

That gives us 12 tests. A few things worth noting:

- **`not allow with input as ...`** -- when we want to verify that access is denied, we use `not`. If `allow` is `false` or `undefined`, then `not allow` is `true`, and the test passes.
- **`test_missing_role_denied`** -- the input has no `role` field. Since `input.role` will be `undefined`, no rule matches, and the default `false` kicks in. Exactly what we want.
- **`test_empty_input_denied`** -- empty input. Same reasoning.
- **Descriptive names** -- `test_editor_cannot_delete` tells you exactly what to expect. When a test fails at 3 AM, the name is the first thing you read.

## Run the Tests

The basic command:

```bash
opa test --format pretty .
```

If everything passes, the output is minimal:

```
PASS: 12/12
```

OPA only bothers you when something fails. To see the detail for each test, add the `-v` (verbose) flag:

```bash
opa test --format pretty -v .
```

```
policy_test.rego:
data.authz.test_admin_can_read: PASS (560.708us)
data.authz.test_admin_can_write: PASS (169.583us)
data.authz.test_admin_can_delete: PASS (135.875us)
data.authz.test_editor_can_read: PASS (130.75us)
data.authz.test_editor_can_write: PASS (107.708us)
data.authz.test_editor_cannot_delete: PASS (112.583us)
data.authz.test_viewer_can_read: PASS (131.208us)
data.authz.test_viewer_cannot_write: PASS (117.208us)
data.authz.test_viewer_cannot_delete: PASS (108.75us)
data.authz.test_unknown_role_denied: PASS (124.875us)
data.authz.test_missing_role_denied: PASS (96.375us)
data.authz.test_empty_input_denied: PASS (84.667us)
--------------------------------------------------------------------------------
PASS: 12/12
```

Each test appears with its fully qualified name (`data.authz.test_...`) and the time it took. If you have 200 tests and only want to run the admin ones, use `--run`:

```bash
opa test --format pretty --run "test_admin" -v .
```

```
policy_test.rego:
data.authz.test_admin_can_read: PASS (502.25us)
data.authz.test_admin_can_write: PASS (158.75us)
data.authz.test_admin_can_delete: PASS (121.333us)
--------------------------------------------------------------------------------
PASS: 3/3
```

The value of `--run` is a pattern that matches against test names. Very useful when you are working on a specific part of the policy and do not want to wait for everything to run.

## What a Failing Test Looks Like

This is just as important as knowing when tests pass. Suppose you accidentally write a test that asserts something incorrect -- for example, that a viewer can write:

```rego title="policy_test.rego (wrong test -- do not keep)"
test_viewer_can_write_WRONG if {
    allow with input as {"role": "viewer", "action": "write"}
}
```

If you add that test and run `opa test --format pretty -v .`, you will see:

```
policy_test.rego:
data.authz.test_admin_can_read: PASS (547.5us)
data.authz.test_admin_can_write: PASS (166us)
data.authz.test_admin_can_delete: PASS (128.25us)
data.authz.test_editor_can_read: PASS (143.833us)
data.authz.test_editor_can_write: PASS (103.917us)
data.authz.test_editor_cannot_delete: PASS (115.5us)
data.authz.test_viewer_can_read: PASS (119.208us)
data.authz.test_viewer_cannot_write: PASS (104.417us)
data.authz.test_viewer_cannot_delete: PASS (108.625us)
data.authz.test_unknown_role_denied: PASS (106.292us)
data.authz.test_missing_role_denied: PASS (93.958us)
data.authz.test_empty_input_denied: PASS (95us)
data.authz.test_viewer_can_write_WRONG: FAIL (110.667us)
--------------------------------------------------------------------------------
PASS: 12/13
FAIL: 1/13
```

The test shows up as `FAIL`. OPA does not tell you "expected true, got false" the way other frameworks do -- it simply tells you the rule did not evaluate to `true`. The test name should give you enough context to understand what went wrong.

If you see a FAIL on a test that *should* pass, the problem is in your policy. If you see a FAIL on a test you wrote incorrectly, the problem is in the test. Either way, a descriptive name is your first clue.

**Remove the `test_viewer_can_write_WRONG` test before continuing.** It was only there to demonstrate what failure looks like.

## Verify What You Learned

Make sure you are in the `03-rego-testing/01-unit-tests/` directory and that both files (`policy.rego` and `policy_test.rego`) contain the content from the code blocks above (without the `_WRONG` test).

**Command 1** -- Run all tests:

```bash
opa test --format pretty .
```

Expected output:

```
PASS: 12/12
```

**Command 2** -- Run in verbose mode:

```bash
opa test --format pretty -v .
```

Expected output: 12 lines of PASS with timings, ending with `PASS: 12/12`.

**Command 3** -- Filter only the viewer tests:

```bash
opa test --format pretty --run "test_viewer" -v .
```

Expected output:

```
data.authz.test_viewer_can_read: PASS
data.authz.test_viewer_cannot_write: PASS
data.authz.test_viewer_cannot_delete: PASS
--------------------------------------------------------------------------------
PASS: 3/3
```

**Command 4** -- Filter edge case tests:

```bash
opa test --format pretty --run "test_unknown|test_missing|test_empty" -v .
```

Expected output:

```
data.authz.test_unknown_role_denied: PASS
data.authz.test_missing_role_denied: PASS
data.authz.test_empty_input_denied: PASS
--------------------------------------------------------------------------------
PASS: 3/3
```

## What's Next

You now know how to write and run unit tests for Rego policies. But so far we have only injected `input`. In the next exercise, we tackle fixtures and mocking -- reusable test data, and how to replace built-in functions like `time.now_ns` and `http.send` so your tests stay deterministic regardless of when or where you run them.

## Reference

- [Policy Testing (OPA docs)](https://www.openpolicyagent.org/docs/latest/policy-testing/)
- [`opa test` CLI](https://www.openpolicyagent.org/docs/latest/#7-testing-policies)
- [The `with` keyword](https://www.openpolicyagent.org/docs/latest/policy-language/#with-keyword)

## Additional Resources

- [Rego Playground](https://play.openpolicyagent.org/) -- experiment with policies and tests in the browser
- [Styra Academy -- OPA Policy Testing](https://academy.styra.com/) -- free guided course covering OPA testing patterns
- [OPA GitHub Examples](https://github.com/open-policy-agent/opa/tree/main/test) -- the OPA project's own test suite for reference

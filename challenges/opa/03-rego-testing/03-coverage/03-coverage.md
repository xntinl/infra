# Coverage: Find Untested Code

## Prerequisites

- OPA CLI installed (`opa version` to verify)
- Completed `01-unit-tests` and `02-fixtures-and-mocking` in this section

## Learning Objectives

After completing this exercise, you will be able to:

- Run `opa test --coverage` and interpret the JSON coverage report
- Identify uncovered branches in a policy by reading `not_covered` entries
- Use coverage iteratively: write partial tests, measure, fill gaps, re-measure
- Distinguish between coverage as a diagnostic tool and coverage as a goal

## Why Coverage Matters

Having tests that pass feels good. But how complete are they? If your policy has five branches and you only test two, the other three can have bugs you will never detect. It is the same as having unit tests that cover the happy path but never test the edge cases.

Coverage tells you exactly that: which parts of your policy were evaluated during the tests and which were not. It is not a guarantee of correctness -- you can have 100% coverage and still have a logic bug -- but it is very useful for finding dead code, forgotten paths, and rules that nobody is testing.

## How Coverage Works in OPA

The command is simple:

```bash
opa test --format pretty --coverage .
```

OPA runs your tests and then shows a JSON report with the coverage. For each `.rego` file, it tells you which lines were covered (evaluated at least once) and which were not.

The report has this structure:

```json
{
  "files": {
    "policy.rego": {
      "covered": [{"start": {"row": 5}, "end": {"row": 5}}, ...],
      "not_covered": [{"start": {"row": 12}, "end": {"row": 12}}, ...],
      "coverage": 66.7
    }
  }
}
```

The key field is `not_covered`: that is where your untested lines are. Each uncovered line is a path through your policy that could have a bug and you would never know.

## The Policy: Access with Multiple Branches

We will build a policy with several paths. Five levels of access, each with different rules.

```rego title="policy.rego"
package access

import rego.v1

default decision := "deny"

decision := "full_access" if {
    input.role == "admin"
    input.environment == "production"
}

decision := "full_access" if {
    input.role == "admin"
    input.environment == "staging"
}

decision := "read_write" if {
    input.role == "editor"
    input.action in {"read", "write"}
}

decision := "read_only" if {
    input.role == "viewer"
    input.action == "read"
}

decision := "sandbox_only" if {
    input.role == "intern"
    input.environment == "sandbox"
    input.action == "read"
}
```

There are five paths that can produce a result other than the default:

1. Admin in production -> `full_access`
2. Admin in staging -> `full_access`
3. Editor with read/write -> `read_write`
4. Viewer with read -> `read_only`
5. Intern in sandbox with read -> `sandbox_only`

If none of them match, `decision` is `"deny"`.

## Step 1: Partial Tests (Deliberately Incomplete)

We start with incomplete tests on purpose. We will only cover admin and viewer.

```rego title="policy_test.rego"
package access

import rego.v1

test_admin_production if {
    decision == "full_access" with input as {
        "role": "admin",
        "environment": "production",
    }
}

test_viewer_read if {
    decision == "read_only" with input as {
        "role": "viewer",
        "action": "read",
    }
}

test_unknown_role_denied if {
    decision == "deny" with input as {
        "role": "contractor",
        "action": "read",
    }
}
```

Three tests. They cover admin in production, viewer with read, and the default deny. Let's see what the coverage report says.

## Step 2: See the Partial Coverage

```bash
opa test --format pretty --coverage .
```

The output will look something like this (format is JSON):

```json
{
  "files": {
    "policy.rego": {
      "covered": [
        {"start":{"row":7},"end":{"row":9}},
        {"start":{"row":19},"end":{"row":21}},
        {"start":{"row":5},"end":{"row":5}}
      ],
      "not_covered": [
        {"start":{"row":11},"end":{"row":13}},
        {"start":{"row":15},"end":{"row":17}},
        {"start":{"row":23},"end":{"row":26}}
      ],
      "coverage": 50
    },
    "policy_test.rego": {
      "covered": [
        {"start":{"row":5},"end":{"row":9}},
        {"start":{"row":11},"end":{"row":15}},
        {"start":{"row":17},"end":{"row":21}}
      ],
      "coverage": 100
    }
  }
}
```

Look at `policy.rego`: coverage is around 50%. The lines in `not_covered` correspond to:

- **Lines 11-13**: admin in staging (we never tested staging)
- **Lines 15-17**: editor with read/write (we never tested editor)
- **Lines 23-26**: intern in sandbox (we never tested intern)

Now we know exactly what is missing. No guessing -- the report tells us the line numbers.

## Step 3: Add Tests for the Missing Branches

Add these tests to the **end** of your `policy_test.rego` code block:

```rego title="policy_test.rego (append to existing)"
# --- New tests to cover missing branches ---

test_admin_staging if {
    decision == "full_access" with input as {
        "role": "admin",
        "environment": "staging",
    }
}

test_editor_read if {
    decision == "read_write" with input as {
        "role": "editor",
        "action": "read",
    }
}

test_editor_write if {
    decision == "read_write" with input as {
        "role": "editor",
        "action": "write",
    }
}

test_editor_delete_denied if {
    decision == "deny" with input as {
        "role": "editor",
        "action": "delete",
    }
}

test_intern_sandbox_read if {
    decision == "sandbox_only" with input as {
        "role": "intern",
        "environment": "sandbox",
        "action": "read",
    }
}

test_intern_production_denied if {
    decision == "deny" with input as {
        "role": "intern",
        "environment": "production",
        "action": "read",
    }
}

test_viewer_write_denied if {
    decision == "deny" with input as {
        "role": "viewer",
        "action": "write",
    }
}
```

## Step 4: Verify Full Coverage

```bash
opa test --format pretty --coverage .
```

Now the report should show that `policy.rego` has significantly higher coverage:

```json
{
  "files": {
    "policy.rego": {
      "covered": [
        {"start":{"row":5},"end":{"row":5}},
        {"start":{"row":7},"end":{"row":9}},
        {"start":{"row":11},"end":{"row":13}},
        {"start":{"row":15},"end":{"row":17}},
        {"start":{"row":19},"end":{"row":21}},
        {"start":{"row":23},"end":{"row":26}}
      ],
      "coverage": 100
    },
    "policy_test.rego": {
      "covered": [
        {"start":{"row":5},"end":{"row":9}},
        {"start":{"row":11},"end":{"row":15}},
        {"start":{"row":17},"end":{"row":21}},
        {"start":{"row":25},"end":{"row":29}},
        {"start":{"row":31},"end":{"row":35}},
        {"start":{"row":37},"end":{"row":41}},
        {"start":{"row":43},"end":{"row":47}},
        {"start":{"row":49},"end":{"row":54}},
        {"start":{"row":56},"end":{"row":61}},
        {"start":{"row":63},"end":{"row":67}}
      ],
      "coverage": 100
    }
  }
}
```

`not_covered` has disappeared (or is empty). Every branch of the policy was exercised by at least one test.

## The Iterative Process

What we just did is the real-world workflow:

1. Write the policy.
2. Write the tests that come to mind.
3. Run `--coverage` and see what is missing.
4. Add tests for the uncovered paths.
5. Repeat until you are satisfied.

You do not need to reach 100% every time. Sometimes an uncovered branch is a default that is not worth testing. Sometimes an uncovered branch reveals dead code you can remove. Coverage is an exploration tool, not a number to chase blindly.

But if your policy controls access to production, that 100% becomes a lot more attractive.

## Verify What You Learned

Make sure you are in the `03-rego-testing/03-coverage/` directory with the complete `policy.rego` and `policy_test.rego` (with all tests added).

**Command 1** -- Run the tests to confirm they pass:

```bash
opa test --format pretty -v .
```

Expected output: 10 tests PASS.

```
data.access.test_admin_production: PASS
data.access.test_viewer_read: PASS
data.access.test_unknown_role_denied: PASS
data.access.test_admin_staging: PASS
data.access.test_editor_read: PASS
data.access.test_editor_write: PASS
data.access.test_editor_delete_denied: PASS
data.access.test_intern_sandbox_read: PASS
data.access.test_intern_production_denied: PASS
data.access.test_viewer_write_denied: PASS
--------------------------------------------------------------------------------
PASS: 10/10
```

**Command 2** -- Check full coverage:

```bash
opa test --format pretty --coverage .
```

Expected output: a JSON report where `policy.rego` has `"coverage": 100` (or close to it) and no entries in `not_covered`.

**Command 3** -- Experiment with removing the intern and editor tests, re-running `--coverage`, and observing how the percentage drops. Then add them back. That cycle of "remove, measure, add" is exactly the workflow you will use in practice.

```bash
opa test --format pretty --coverage .
```

Compare the result with and without those tests to see the impact.

## What's Next

You can now measure how thoroughly your tests exercise a policy. In the next exercise, we look at table-driven tests -- a pattern for testing policies with many input combinations without writing a separate function for each case.

## Reference

- [Coverage reporting (OPA docs)](https://www.openpolicyagent.org/docs/latest/policy-testing/#coverage)
- [`opa test --coverage`](https://www.openpolicyagent.org/docs/latest/#7-testing-policies)
- [Policy Testing](https://www.openpolicyagent.org/docs/latest/policy-testing/)

## Additional Resources

- [Rego Playground](https://play.openpolicyagent.org/) -- run tests and check coverage online
- [Conftest](https://www.conftest.dev/) -- a testing tool built on OPA that also supports coverage reports
- [OPA Best Practices (Styra)](https://www.styra.com/blog/opa-best-practices/) -- guidelines on structuring policies and tests for maintainability

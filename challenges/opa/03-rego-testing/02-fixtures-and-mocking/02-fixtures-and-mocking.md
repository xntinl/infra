# Fixtures and Mocking: Test Without External Dependencies

## Prerequisites

- OPA CLI installed (`opa version` to verify)
- Completed `01-unit-tests` in this section (familiarity with `test_` rules, `with input as`, and `opa test`)

## Learning Objectives

After completing this exercise, you will be able to:

- Extract reusable test data into fixture variables to eliminate duplication
- Mock built-in functions like `time.now_ns` and `http.send` using the `with` keyword
- Chain multiple `with` clauses to replace both input and built-in dependencies in a single evaluation
- Write boundary tests that catch off-by-one errors in time-based policies

## Why Mock Built-ins

Some policies do not depend solely on `input`. A policy might check the current time to decide whether a deploy is allowed. Another might call an HTTP endpoint to verify something against an external service. If your tests depend on the real clock or the network, they stop being deterministic -- they pass at 10 AM and fail at 8 PM. Or they pass with Wi-Fi and fail on an airplane.

The solution is mocking: replacing those dependencies with controlled values. And organizing test data into reusable fixtures so you do not repeat yourself.

## What Is a Fixture

A fixture is simply test data you reuse. Instead of copying the same `input` object into 10 tests, you define it once as a variable and reference it wherever you need it. It is the same thing you would do in any language: extract duplication into a named constant.

In Rego, a fixture can be a variable inside the test file:

```rego
employee_input := {"user": "maria", "role": "employee", "department": "engineering"}
```

Then you use it in multiple tests:

```rego
test_employee_allowed if {
    allow with input as employee_input
}
```

When you have many fixtures, you can also put them in separate JSON files and load them as `data`. But for most cases, variables in the test file are more than enough.

## Mocking Built-ins with `with`

You already know `with input as {...}` for injecting input. But `with` can do much more: it can replace **any built-in function** in OPA. This includes:

- **`time.now_ns`** -- the current time in nanoseconds
- **`http.send`** -- HTTP calls
- **`opa.runtime`** -- runtime information
- And any other built-in function

The syntax is the same:

```rego
allow with time.now_ns as 1609459200000000000
```

That says: "evaluate `allow`, but when the policy calls `time.now_ns()`, return this number instead." The policy does not know it is being mocked. It receives the value and operates normally.

## The Policy: Business Hours Window

We will build a policy that only allows actions during business hours: 9:00 to 17:59 UTC. Outside that range, everything is denied. This is a common pattern in environments where off-hours deploys require explicit approval or are outright prohibited.

```rego title="policy.rego"
package timewindow

import rego.v1

default allow := false

allow if {
    is_business_hours
    input.role == "deployer"
    input.action == "deploy"
}

is_business_hours if {
    clock := time.clock(time.now_ns())
    hour := clock[0]
    hour >= 9
    hour < 18
}
```

The rule `is_business_hours` gets the current time via `time.now_ns()`, converts it to `[hour, minute, second]` with `time.clock()`, and checks that the hour is between 9 and 17 inclusive. If you run this at 14:00 UTC, `is_business_hours` is `true`. At 22:00, it is `false`.

The testing problem is obvious: the result depends on *when* you run the test. Monday at 10 AM it passes, Monday at 11 PM it fails. We need to control the clock.

## The Tests with Mocked Time

To mock time, we need to know what value to pass. `time.now_ns()` returns nanoseconds since the Unix epoch (January 1, 1970). Here are the timestamps we will use, all from January 8, 2024:

- **10:00 UTC** -- `1704708000000000000`
- **14:30 UTC** -- `1704724200000000000`
- **03:00 UTC** -- `1704682800000000000`
- **20:00 UTC** -- `1704744000000000000`
- **09:00 UTC (lower boundary)** -- `1704704400000000000`
- **17:59 UTC (upper boundary)** -- `1704736740000000000`
- **18:00 UTC (just outside)** -- `1704736800000000000`

```rego title="policy_test.rego"
package timewindow

import rego.v1

# --- Fixtures: reusable inputs ---

deployer_deploy := {
    "role": "deployer",
    "action": "deploy",
}

deployer_rollback := {
    "role": "deployer",
    "action": "rollback",
}

viewer_deploy := {
    "role": "viewer",
    "action": "deploy",
}

# --- Timestamps for mocking (nanoseconds) ---
# 2024-01-08 10:00:00 UTC (within business hours)
morning_ns := 1704708000000000000

# 2024-01-08 14:30:00 UTC (within business hours)
afternoon_ns := 1704724200000000000

# 2024-01-08 03:00:00 UTC (outside business hours)
early_morning_ns := 1704682800000000000

# 2024-01-08 20:00:00 UTC (outside business hours)
night_ns := 1704744000000000000

# 2024-01-08 09:00:00 UTC (lower boundary, within)
boundary_start_ns := 1704704400000000000

# 2024-01-08 17:59:00 UTC (upper boundary, within)
boundary_end_ns := 1704736740000000000

# 2024-01-08 18:00:00 UTC (just outside)
just_after_ns := 1704736800000000000

# --- Tests: business hours ---

test_deployer_allowed_morning if {
    allow with input as deployer_deploy
        with time.now_ns as morning_ns
}

test_deployer_allowed_afternoon if {
    allow with input as deployer_deploy
        with time.now_ns as afternoon_ns
}

test_deployer_denied_early_morning if {
    not allow with input as deployer_deploy
        with time.now_ns as early_morning_ns
}

test_deployer_denied_night if {
    not allow with input as deployer_deploy
        with time.now_ns as night_ns
}

# --- Tests: exact boundaries ---

test_allowed_at_boundary_start if {
    allow with input as deployer_deploy
        with time.now_ns as boundary_start_ns
}

test_allowed_at_boundary_end if {
    allow with input as deployer_deploy
        with time.now_ns as boundary_end_ns
}

test_denied_just_after_boundary if {
    not allow with input as deployer_deploy
        with time.now_ns as just_after_ns
}

# --- Tests: wrong role and action ---

test_wrong_action_denied if {
    not allow with input as deployer_rollback
        with time.now_ns as morning_ns
}

test_wrong_role_denied if {
    not allow with input as viewer_deploy
        with time.now_ns as morning_ns
}
```

A few things worth noting:

- **Fixtures as variables** -- `deployer_deploy`, `deployer_rollback`, and `viewer_deploy` are defined once and used in multiple tests. If you change the input structure tomorrow, you change it in one place.
- **Named timestamp variables** -- instead of putting `1704708000000000000` directly in the test (which tells you nothing), we give it a name like `morning_ns`. Now the test reads: "deployer allowed morning." Much clearer.
- **Chained `with`** -- you can chain multiple `with` clauses: one for the input, another for the built-in. Each `with` replaces a dependency, and they all apply to the same evaluation.
- **Boundary tests** -- these are the most important. The classic bug is using `<=` instead of `<` or vice versa. If your policy says `hour < 18`, you need to test that 17:59 passes and 18:00 does not.

Let's verify these 9 tests pass before adding more.

```bash
opa test --format pretty -v .
```

```
policy_test.rego:
data.timewindow.test_deployer_allowed_morning: PASS (691.625us)
data.timewindow.test_deployer_allowed_afternoon: PASS (175.75us)
data.timewindow.test_deployer_denied_early_morning: PASS (145.583us)
data.timewindow.test_deployer_denied_night: PASS (108.375us)
data.timewindow.test_allowed_at_boundary_start: PASS (156us)
data.timewindow.test_allowed_at_boundary_end: PASS (115.042us)
data.timewindow.test_denied_just_after_boundary: PASS (122.333us)
data.timewindow.test_wrong_action_denied: PASS (119.875us)
data.timewindow.test_wrong_role_denied: PASS (103.042us)
--------------------------------------------------------------------------------
PASS: 9/9
```

All 9 pass, and they will produce the same result at 3 AM or 3 PM, because the clock is fully controlled.

## Mocking `http.send`

To complete the picture, let's see how to mock HTTP calls. We will add a rule that checks whether a user is on a blocklist by calling an external auth service.

Add these rules to the **end** of your `policy.rego` code block:

```rego title="policy.rego (append to existing)"
user_is_blocked if {
    resp := http.send({
        "method": "GET",
        "url": sprintf("https://auth.internal/blocked/%s", [input.user]),
    })
    resp.status_code == 200
    resp.body.blocked == true
}

allow_unless_blocked if {
    is_business_hours
    input.role == "deployer"
    input.action == "deploy"
    not user_is_blocked
}
```

And add these tests to the **end** of your `policy_test.rego` code block:

```rego title="policy_test.rego (append to existing)"
# --- Tests: http.send mock ---

test_blocked_user_denied if {
    not allow_unless_blocked
        with input as {"role": "deployer", "action": "deploy", "user": "hacker"}
        with time.now_ns as morning_ns
        with http.send as {"status_code": 200, "body": {"blocked": true}}
}

test_non_blocked_user_allowed if {
    allow_unless_blocked
        with input as {"role": "deployer", "action": "deploy", "user": "maria"}
        with time.now_ns as morning_ns
        with http.send as {"status_code": 200, "body": {"blocked": false}}
}

test_auth_service_down_not_blocked if {
    allow_unless_blocked
        with input as {"role": "deployer", "action": "deploy", "user": "maria"}
        with time.now_ns as morning_ns
        with http.send as {"status_code": 500, "body": {}}
}
```

Here we mock `http.send` to return exactly what we want. No network involved, no latency, no service that can go down. The test is instant and 100% deterministic.

Notice the third test: it simulates the auth service returning a 500. Since `resp.status_code == 200` fails, `user_is_blocked` is `false`, and `not user_is_blocked` is `true`. The user is not considered blocked when the service is down. That may or may not be what you want -- but at least now you *know* how your policy behaves in that scenario.

## Run All Tests

```bash
opa test --format pretty -v .
```

```
policy_test.rego:
data.timewindow.test_deployer_allowed_morning: PASS (691.625us)
data.timewindow.test_deployer_allowed_afternoon: PASS (175.75us)
data.timewindow.test_deployer_denied_early_morning: PASS (145.583us)
data.timewindow.test_deployer_denied_night: PASS (108.375us)
data.timewindow.test_allowed_at_boundary_start: PASS (156us)
data.timewindow.test_allowed_at_boundary_end: PASS (115.042us)
data.timewindow.test_denied_just_after_boundary: PASS (122.333us)
data.timewindow.test_wrong_action_denied: PASS (119.875us)
data.timewindow.test_wrong_role_denied: PASS (103.042us)
data.timewindow.test_blocked_user_denied: PASS (219.5us)
data.timewindow.test_non_blocked_user_allowed: PASS (175.042us)
data.timewindow.test_auth_service_down_not_blocked: PASS (124.583us)
--------------------------------------------------------------------------------
PASS: 12/12
```

12 tests, all deterministic, all instant.

## Verify What You Learned

Make sure you are in the `03-rego-testing/02-fixtures-and-mocking/` directory with the complete `policy.rego` and `policy_test.rego` files (including the `http.send` sections).

**Command 1** -- Run all tests:

```bash
opa test --format pretty -v .
```

Expected output: 12 tests PASS.

**Command 2** -- Filter only the boundary tests:

```bash
opa test --format pretty --run "boundary" -v .
```

Expected output:

```
data.timewindow.test_allowed_at_boundary_start: PASS
data.timewindow.test_allowed_at_boundary_end: PASS
data.timewindow.test_denied_just_after_boundary: PASS
--------------------------------------------------------------------------------
PASS: 3/3
```

**Command 3** -- Filter only the HTTP mock tests:

```bash
opa test --format pretty --run "blocked|auth_service" -v .
```

Expected output:

```
data.timewindow.test_blocked_user_denied: PASS
data.timewindow.test_non_blocked_user_allowed: PASS
data.timewindow.test_auth_service_down_not_blocked: PASS
--------------------------------------------------------------------------------
PASS: 3/3
```

## What's Next

You now know how to keep your tests deterministic by mocking built-in functions and how to reduce duplication with fixtures. In the next exercise, we look at coverage -- how to find out which branches of your policy are being exercised by your tests and which are slipping through untested.

## Reference

- [Mocking built-in functions (OPA docs)](https://www.openpolicyagent.org/docs/latest/policy-testing/#mocking-built-in-functions)
- [The `with` keyword](https://www.openpolicyagent.org/docs/latest/policy-language/#with-keyword)
- [`time.now_ns` built-in](https://www.openpolicyagent.org/docs/latest/policy-reference/#time)
- [`http.send` built-in](https://www.openpolicyagent.org/docs/latest/policy-reference/#http)

## Additional Resources

- [Rego Playground](https://play.openpolicyagent.org/) -- try mocking built-ins interactively
- [OPA Built-in Functions Reference](https://www.openpolicyagent.org/docs/latest/policy-reference/#built-in-functions) -- complete list of functions you can mock
- [Styra Academy](https://academy.styra.com/) -- free guided exercises including testing and mocking

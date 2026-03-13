# Table-Driven Tests: Exhaustive Testing

## Prerequisites

- OPA CLI installed (`opa version` to verify)
- Completed `01-unit-tests`, `02-fixtures-and-mocking`, and `03-coverage` in this section

## Learning Objectives

After completing this exercise, you will be able to:

- Structure test cases as data tables using arrays of objects
- Use the `every` keyword to iterate over a table and assert each case
- Decide when table-driven tests are a better fit than one-function-per-case tests
- Debug a failing table-driven test by narrowing down to the offending case

## Why Table-Driven Tests

When a policy has many possible input combinations -- different IPs, ports, protocols, roles -- writing one test per case becomes unsustainable. You end up with 30 functions that are basically identical; only the input and the expected result change. It is repetitive, hard to maintain, and easy to forget a case.

The table-driven pattern solves this. Instead of one function per case, you define a table (an array of objects) where each row is a test case with its description, input, and expected result. Then you iterate over the table with a single function. It is the same pattern used in Go, Rust, and basically any language with a mature testing framework.

The advantage is obvious: adding a new case means adding one row to the table. Not a new function, not a new code block -- one row.

## The Pattern in Rego

The idea is simple. You define an array of objects:

```rego
test_cases := [
    {"msg": "case 1", "inp": {...}, "exp": true},
    {"msg": "case 2", "inp": {...}, "exp": false},
]
```

Then you iterate with `every`:

```rego
test_all_cases if {
    every case in test_cases {
        allow == case.exp with input as case.inp
    }
}
```

`every` verifies that the condition holds for *all* elements. If any one fails, the entire test fails. It is equivalent to a forEach with an assertion on each iteration.

Each object in the table has three fields:
- **`msg`** -- a human-readable description of the case. It is not used in evaluation, but it tells you which case failed when something goes wrong.
- **`inp`** -- the input injected with `with`.
- **`exp`** -- the expected result (`true` or `false`, or a string, depending on the rule being tested).

## The Policy: Firewall Rules

We will build a firewall policy. It decides whether a network packet is allowed based on the source IP, destination port, and protocol.

```rego title="policy.rego"
package firewall

import rego.v1

default allow := false

# Internal network can access anything
allow if {
    net.cidr_contains("10.0.0.0/8", input.source_ip)
}

# Public HTTP/HTTPS allowed
allow if {
    input.protocol == "tcp"
    input.dest_port in {80, 443}
}

# DNS allowed
allow if {
    input.protocol == "udp"
    input.dest_port == 53
}

# SSH only from specific range
allow if {
    input.protocol == "tcp"
    input.dest_port == 22
    net.cidr_contains("192.168.1.0/24", input.source_ip)
}

# Explicitly block dangerous ports
deny if {
    input.dest_port in {23, 3389, 5900}
}

# Final result: allow and not deny
result := "allow" if {
    allow
    not deny
}

result := "deny" if {
    not allow
}

result := "deny" if {
    deny
}
```

This policy has several paths:
- Internal network (`10.x.x.x`) can access everything.
- Public HTTP/HTTPS traffic is allowed.
- DNS (UDP 53) is allowed.
- SSH only from `192.168.1.0/24`.
- Dangerous ports (telnet 23, RDP 3389, VNC 5900) are always blocked.
- `result` combines `allow` and `deny` into a final decision.

## Table-Driven Tests

Now the table. Each row is a scenario with its description, input, and expected result.

```rego title="policy_test.rego"
package firewall

import rego.v1

# --- Test case table for allow ---

allow_cases := [
    {
        "msg": "internal network can access any port",
        "inp": {"source_ip": "10.1.2.3", "protocol": "tcp", "dest_port": 8080},
        "exp": true,
    },
    {
        "msg": "internal network with IP at range boundary",
        "inp": {"source_ip": "10.255.255.255", "protocol": "tcp", "dest_port": 9090},
        "exp": true,
    },
    {
        "msg": "public HTTP allowed",
        "inp": {"source_ip": "203.0.113.1", "protocol": "tcp", "dest_port": 80},
        "exp": true,
    },
    {
        "msg": "public HTTPS allowed",
        "inp": {"source_ip": "203.0.113.1", "protocol": "tcp", "dest_port": 443},
        "exp": true,
    },
    {
        "msg": "DNS UDP allowed",
        "inp": {"source_ip": "203.0.113.1", "protocol": "udp", "dest_port": 53},
        "exp": true,
    },
    {
        "msg": "SSH from authorized range",
        "inp": {"source_ip": "192.168.1.50", "protocol": "tcp", "dest_port": 22},
        "exp": true,
    },
    {
        "msg": "SSH from outside range denied",
        "inp": {"source_ip": "203.0.113.1", "protocol": "tcp", "dest_port": 22},
        "exp": false,
    },
    {
        "msg": "arbitrary port from public IP denied",
        "inp": {"source_ip": "203.0.113.1", "protocol": "tcp", "dest_port": 8080},
        "exp": false,
    },
    {
        "msg": "UDP to non-DNS port denied",
        "inp": {"source_ip": "203.0.113.1", "protocol": "udp", "dest_port": 12345},
        "exp": false,
    },
    {
        "msg": "unknown protocol denied",
        "inp": {"source_ip": "203.0.113.1", "protocol": "icmp", "dest_port": 0},
        "exp": false,
    },
    {
        "msg": "port 0 denied",
        "inp": {"source_ip": "203.0.113.1", "protocol": "tcp", "dest_port": 0},
        "exp": false,
    },
    {
        "msg": "IP just outside internal network",
        "inp": {"source_ip": "11.0.0.1", "protocol": "tcp", "dest_port": 8080},
        "exp": false,
    },
    {
        "msg": "SSH from 192.168.2.x denied (outside /24)",
        "inp": {"source_ip": "192.168.2.1", "protocol": "tcp", "dest_port": 22},
        "exp": false,
    },
    {
        "msg": "DNS over TCP denied (only UDP allowed)",
        "inp": {"source_ip": "203.0.113.1", "protocol": "tcp", "dest_port": 53},
        "exp": false,
    },
    {
        "msg": "HTTPS over UDP denied (only TCP allowed)",
        "inp": {"source_ip": "203.0.113.1", "protocol": "udp", "dest_port": 443},
        "exp": false,
    },
]

test_allow_rules if {
    every case in allow_cases {
        allow == case.exp with input as case.inp
    }
}

# --- Table for dangerous ports (explicit deny) ---

deny_cases := [
    {
        "msg": "telnet always blocked",
        "inp": {"source_ip": "10.1.2.3", "protocol": "tcp", "dest_port": 23},
        "exp": true,
    },
    {
        "msg": "RDP always blocked",
        "inp": {"source_ip": "10.1.2.3", "protocol": "tcp", "dest_port": 3389},
        "exp": true,
    },
    {
        "msg": "VNC always blocked",
        "inp": {"source_ip": "10.1.2.3", "protocol": "tcp", "dest_port": 5900},
        "exp": true,
    },
    {
        "msg": "safe port not blocked",
        "inp": {"source_ip": "10.1.2.3", "protocol": "tcp", "dest_port": 443},
        "exp": false,
    },
]

test_deny_rules if {
    every case in deny_cases {
        deny == case.exp with input as case.inp
    }
}

# --- Table for final result ---

result_cases := [
    {
        "msg": "internal network HTTP -> allow",
        "inp": {"source_ip": "10.1.2.3", "protocol": "tcp", "dest_port": 80},
        "exp": "allow",
    },
    {
        "msg": "public IP HTTPS -> allow",
        "inp": {"source_ip": "203.0.113.1", "protocol": "tcp", "dest_port": 443},
        "exp": "allow",
    },
    {
        "msg": "public IP random port -> deny",
        "inp": {"source_ip": "203.0.113.1", "protocol": "tcp", "dest_port": 9999},
        "exp": "deny",
    },
    {
        "msg": "internal network telnet -> deny (dangerous port wins)",
        "inp": {"source_ip": "10.1.2.3", "protocol": "tcp", "dest_port": 23},
        "exp": "deny",
    },
]

test_result_rules if {
    every case in result_cases {
        result == case.exp with input as case.inp
    }
}
```

That is 24 test cases distributed across three tables, yet there are only three `test_` functions. Compare that to having 24 separate functions -- the table is much easier to read and maintain.

## Anatomy of a Table Row

Let's look at one row:

```rego
{
    "msg": "DNS over TCP denied (only UDP allowed)",
    "inp": {"source_ip": "203.0.113.1", "protocol": "tcp", "dest_port": 53},
    "exp": false,
},
```

- **`msg`** -- "DNS over TCP denied." When this case fails, this description is what will point you to the problem. In OPA's report it does not appear automatically (OPA only says that `test_allow_rules` failed), but if you inspect the table, the `msg` takes you to the exact case.
- **`inp`** -- a DNS packet but over TCP instead of UDP. Our policy only allows DNS over UDP, so this should be `false`.
- **`exp`** -- `false`, as expected.

## What Happens When a Case Fails

If a case in the table fails, OPA tells you that the function `test_allow_rules` failed, but it does not tell you *which* case. This is the main downside of table-driven tests in OPA: you lose granularity in error reporting.

To debug, you can temporarily comment out half the table and see if the test passes -- binary search style. Or you can add `print()` to see which case is not matching:

```rego title="policy_test.rego (debug version -- do not keep)"
test_allow_rules_debug if {
    every case in allow_cases {
        result := (allow == case.exp with input as case.inp)
        print(case.msg, "->", result)
        result == true
    }
}
```

In practice, the loss of granularity is a tradeoff worth making when you have many similar cases. If you have few cases and each one is very different, individual functions might be a better fit.

## Comparison: One Function Per Case vs Table-Driven

To make it clear why the table is better for exhaustive tests, compare these two approaches.

**Without table (one function per case):**

```rego
test_http_allowed if {
    allow with input as {"source_ip": "203.0.113.1", "protocol": "tcp", "dest_port": 80}
}

test_https_allowed if {
    allow with input as {"source_ip": "203.0.113.1", "protocol": "tcp", "dest_port": 443}
}

test_dns_allowed if {
    allow with input as {"source_ip": "203.0.113.1", "protocol": "udp", "dest_port": 53}
}

# ... 12 more functions ...
```

**With table:**

```rego
allow_cases := [
    {"msg": "HTTP allowed",  "inp": {"source_ip": "203.0.113.1", "protocol": "tcp", "dest_port": 80},  "exp": true},
    {"msg": "HTTPS allowed", "inp": {"source_ip": "203.0.113.1", "protocol": "tcp", "dest_port": 443}, "exp": true},
    {"msg": "DNS allowed",   "inp": {"source_ip": "203.0.113.1", "protocol": "udp", "dest_port": 53},  "exp": true},
    # ... 12 more rows ...
]

test_allow_rules if {
    every case in allow_cases { allow == case.exp with input as case.inp }
}
```

Adding a new case means adding one row to the array. No need to invent a function name, no boilerplate. And the table reads like a specification: "for this input, I expect this result."

## When to Use Each Approach

- **Table-driven** -- when you have many cases with the same structure (same type of input, same type of result). Firewall rules, RBAC with many roles, format validation.
- **One function per case** -- when each test has different setup, different mocks, or complex logic. Or when you need OPA's report to tell you exactly which case failed.
- **Mix both** -- nothing stops you from having both in the same file. Use tables for the repetitive cases and individual functions for the ones that need special attention.

## Run the Tests

```bash
opa test --format pretty -v .
```

```
policy_test.rego:
data.firewall.test_allow_rules: PASS (1.247583ms)
data.firewall.test_deny_rules: PASS (289.625us)
data.firewall.test_result_rules: PASS (378.208us)
--------------------------------------------------------------------------------
PASS: 3/3
```

Three test functions, 24 cases verified. Notice the times are slightly higher because each function evaluates multiple cases, but it is still in the millisecond range.

## Verify What You Learned

Make sure you are in the `03-rego-testing/04-table-driven-tests/` directory with the `policy.rego` and `policy_test.rego` files from the code blocks above.

**Command 1** -- Run all tests:

```bash
opa test --format pretty -v .
```

Expected output:

```
data.firewall.test_allow_rules: PASS
data.firewall.test_deny_rules: PASS
data.firewall.test_result_rules: PASS
--------------------------------------------------------------------------------
PASS: 3/3
```

**Command 2** -- Check the coverage:

```bash
opa test --format pretty --coverage .
```

Expected output: a JSON report where `policy.rego` has high coverage (ideally 100%), because the 24 cases cover all branches of the policy.

**Command 3** -- Add a new case to `allow_cases` and run again. For example, add this row at the end of the array (before the `]`):

```rego
    {
        "msg": "internal network with UDP protocol allowed",
        "inp": {"source_ip": "10.0.0.1", "protocol": "udp", "dest_port": 9999},
        "exp": true,
    },
```

Then run:

```bash
opa test --format pretty -v .
```

You should see the same 3 tests pass, but now with 25 cases verified internally. That is the beauty of the pattern: adding a case does not touch the test structure.

## Section Summary

Across these four exercises, you covered the core of Rego testing:

**Key concepts learned:**

- **Unit tests** (exercise 01) -- the `test_` naming convention, the `with` keyword for injecting `input`, `not` for testing denied paths, the `-v` and `--run` flags.
- **Fixtures and mocking** (exercise 02) -- extracting reusable test data into variables, mocking built-in functions like `time.now_ns` and `http.send`, chaining multiple `with` clauses, and testing boundary conditions on time-based policies.
- **Coverage** (exercise 03) -- using `--coverage` to find untested branches, reading the JSON report to identify `not_covered` lines, and the iterative workflow of measuring, filling gaps, and re-measuring.
- **Table-driven tests** (exercise 04) -- structuring test cases as data tables, iterating with `every`, and the tradeoff between table-driven granularity and individual test clarity.

**Important notes:**

- Tests and policies share the same `package`. This is not optional -- it is how tests get access to the rules they are testing.
- `with` replaces values for a single evaluation. It does not mutate state. Each test starts fresh.
- Coverage measures evaluation, not correctness. A line can be covered and still wrong. Coverage finds what you are not testing; it does not prove what you are testing is right.
- Table-driven tests trade individual failure messages for maintainability. Use `print()` or binary search to debug failures. In practice, the tradeoff is almost always worth it when you have more than a handful of similar cases.
- `opa test` discovers test files automatically. Any file in the directory with `test_` rules in the right package will be picked up. No registration or manifest needed.

## What's Next

With testing fundamentals covered, you are ready to apply these patterns to real-world policies -- Kubernetes admission control, CI/CD pipeline gates, API authorization, and infrastructure-as-code validation. The techniques are the same regardless of the domain: write the policy, write the tests, check coverage, iterate.

## Reference

- [Policy Testing (OPA docs)](https://www.openpolicyagent.org/docs/latest/policy-testing/)
- [`every` keyword](https://www.openpolicyagent.org/docs/latest/policy-language/#every-keyword)
- [Table-driven tests in Go (inspiration)](https://go.dev/wiki/TableDrivenTests)

## Additional Resources

- [Rego Playground](https://play.openpolicyagent.org/) -- prototype table-driven tests interactively
- [OPA GitHub Examples](https://github.com/open-policy-agent/opa/tree/main/test) -- real-world test patterns from the OPA project
- [Conftest](https://www.conftest.dev/) -- testing tool built on OPA for configuration files (Terraform, Kubernetes manifests, Dockerfiles)
- [Regal -- Rego Linter](https://github.com/StyraInc/regal) -- catches common mistakes in Rego policies and tests

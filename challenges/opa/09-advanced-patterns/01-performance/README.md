# Exercise 09-01: Optimizing Policy Evaluation Performance

## Prerequisites

- OPA CLI installed (`opa version`)
- Completed section 08 (Policy Distribution)

## Learning Objectives

After completing this exercise, you will be able to:

- Identify patterns that prevent OPA from indexing rules efficiently
- Rewrite slow policies using equality and direct lookups instead of regex and unnecessary comprehensions
- Measure and compare policy performance with `opa bench`

## Why Performance Matters

Your policies work, your tests pass, your CI is green. But when OPA evaluates 10,000 requests per second, every microsecond counts. The difference between a policy that indexes well and one that does not can be 100x in speed. The good news is that OPA gives you tools to measure and optimize.

## How OPA Indexes Rules

OPA has an internal indexer that works like a database index. When you write a rule that uses equality (`==`) or direct unification, OPA can do a hash lookup in O(1). But when you use functions like `regex.match` or `startswith` inside an iteration, OPA must do a linear scan -- evaluating each element one by one.

Rules that index well look like this:

```rego
# GOOD: OPA indexes by equality
allow if {
    input.role == data.roles[_].name   # hash lookup
}
```

Rules that do not index look like this:

```rego
# BAD: regex forces a linear scan
allow if {
    some role in data.roles
    regex.match(role.pattern, input.path)   # evaluates each pattern
}
```

General performance guidelines:

1. **Equality over regex** -- if you can compare with `==`, do not use `regex.match`
2. **Avoid huge comprehensions** -- a `{x | ...}` over 10,000 elements is expensive
3. **Use indexable partial rules** -- `allow if { input.role == "admin" }` indexes; `allow if { startswith(input.role, "admin") }` does not
4. **Put the most restrictive conditions first** -- OPA short-circuits, so a failing check at the top of a rule body saves evaluating everything below it
5. **Minimize `with` keyword usage** -- each `with` creates a copy of the input or data

## Step 1: Set Up Test Data

You need a dataset large enough that the performance difference is visible.

Create `data.json`:

```json
{
    "roles": {
        "admin": {"level": 100, "permissions": ["read", "write", "delete", "admin"]},
        "editor": {"level": 50, "permissions": ["read", "write"]},
        "viewer": {"level": 10, "permissions": ["read"]},
        "auditor": {"level": 30, "permissions": ["read", "audit"]},
        "operator": {"level": 60, "permissions": ["read", "write", "deploy"]},
        "security": {"level": 80, "permissions": ["read", "audit", "security"]},
        "billing": {"level": 40, "permissions": ["read", "billing"]},
        "support": {"level": 20, "permissions": ["read", "support"]}
    },
    "resources": [
        {"path": "/api/users", "required_permission": "read", "min_level": 10},
        {"path": "/api/users/create", "required_permission": "write", "min_level": 50},
        {"path": "/api/admin/settings", "required_permission": "admin", "min_level": 100},
        {"path": "/api/deploy", "required_permission": "deploy", "min_level": 60},
        {"path": "/api/billing/invoices", "required_permission": "billing", "min_level": 40},
        {"path": "/api/audit/logs", "required_permission": "audit", "min_level": 30},
        {"path": "/api/security/alerts", "required_permission": "security", "min_level": 80},
        {"path": "/api/support/tickets", "required_permission": "support", "min_level": 20}
    ]
}
```

Create `input.json`:

```json
{
    "user": {
        "name": "carlos",
        "role": "editor"
    },
    "request": {
        "path": "/api/users/create",
        "method": "POST"
    }
}
```

## Step 2: Write the Slow Version

This version uses regex and unnecessary comprehensions -- patterns that prevent indexing.

Create `slow.rego`:

```rego
package slow

import rego.v1

default allow := false

# SLOW: builds a set with a comprehension, then iterates with regex
allow if {
    role_name := input.user.role
    role_data := data.roles[role_name]

    # Unnecessary comprehension: converts permissions to regex patterns
    permission_patterns := {sprintf(".*%s.*", [p]) | some p in role_data.permissions}

    # For each resource, check with regex if the path matches
    some resource in data.resources
    regex.match(sprintf(".*%s.*", [resource.path]), input.request.path)

    # Then check permissions with regex too
    some pattern in permission_patterns
    regex.match(pattern, resource.required_permission)

    role_data.level >= resource.min_level
}
```

Verify it produces the correct answer before benchmarking:

```bash
opa eval -d slow.rego -d data.json -i input.json "data.slow.allow" --format pretty
```

Expected output:

```
true
```

## Step 3: Write the Fast Version

This version replaces regex with equality and uses direct lookups.

Create `fast.rego`:

```rego
package fast

import rego.v1

default allow := false

allow if {
    # Direct lookup by role name -- O(1)
    role_data := data.roles[input.user.role]

    # Find resource by exact path match -- no regex
    some resource in data.resources
    resource.path == input.request.path

    # Numeric comparison first (cheap)
    role_data.level >= resource.min_level

    # Set membership check -- O(1) with hash
    resource.required_permission in cast_set(role_data.permissions)
}
```

The optimized version does three things better:

1. Uses equality (`==`) instead of `regex.match` for path matching
2. Converts the permissions array to a set with `cast_set` for O(1) membership checks
3. Puts the numeric level check before the membership check (cheaper operation first)

Verify it produces the same answer:

```bash
opa eval -d fast.rego -d data.json -i input.json "data.fast.allow" --format pretty
```

Expected output:

```
true
```

Both versions produce `true`. The difference is in how fast they get there.

## Step 4: Benchmark Both Versions

The `opa bench` command runs a query thousands of times and reports timing and memory statistics:

```bash
opa bench -d slow.rego -d data.json -i input.json "data.slow.allow"
```

Typical output:

```
+-------------------------------------------+------------+
| samples                                   |      48637 |
| ns/op                                     |      24521 |
| B/op                                      |      18440 |
| allocs/op                                 |        285 |
+-------------------------------------------+------------+
```

```bash
opa bench -d fast.rego -d data.json -i input.json "data.fast.allow"
```

Typical output:

```
+-------------------------------------------+------------+
| samples                                   |     389261 |
| ns/op                                     |       3051 |
| B/op                                      |       2816 |
| allocs/op                                 |         53 |
+-------------------------------------------+------------+
```

The optimized version is roughly 8x faster and uses 6x less memory. On a server handling heavy traffic, that difference multiplies across thousands of requests.

The key numbers to watch:

- **ns/op** -- nanoseconds per operation (lower is better)
- **B/op** -- bytes of memory per operation (lower is better)
- **allocs/op** -- memory allocations per operation (lower is better)
- **samples** -- how many times the benchmark ran (more means more reliable)

You can also use `--count` to repeat the entire benchmark for more stable results:

```bash
opa bench -d fast.rego -d data.json -i input.json "data.fast.allow" --count 5
```

## A Common Mistake: Optimizing Before Measuring

It is tempting to rewrite policies based on intuition. Do not. Always benchmark first with `opa bench`. Sometimes a pattern that looks slow (like iterating a small array) is perfectly fine because OPA's indexer handles it. Other times a pattern that looks clean (like a nested comprehension) is secretly expensive. Let the numbers guide you.

## Verify What You Learned

**Command 1** -- Confirm that both versions produce the same result:

```bash
echo "slow:" && opa eval -d slow.rego -d data.json -i input.json "data.slow.allow" --format pretty && echo "fast:" && opa eval -d fast.rego -d data.json -i input.json "data.fast.allow" --format pretty
```

Expected output:

```
slow:
true
fast:
true
```

**Command 2** -- Run the benchmark on the slow version and check that ns/op is reported:

```bash
opa bench -d slow.rego -d data.json -i input.json "data.slow.allow" 2>&1 | grep "ns/op"
```

Expected output (the number varies, but should be significantly higher than the fast version):

```
| ns/op                                     |      24XXX |
```

**Command 3** -- Run the benchmark on the fast version and compare:

```bash
opa bench -d fast.rego -d data.json -i input.json "data.fast.allow" 2>&1 | grep "ns/op"
```

Expected output (should be several times lower than the slow version):

```
| ns/op                                     |       3XXX |
```

You now know how to identify slow policy patterns, rewrite them for OPA's indexer, and verify the improvement with benchmarks. In the next exercise, you will learn how to compose policies from multiple packages into a well-structured codebase.

## What's Next

Exercise 09-02 covers policy composition -- organizing a growing policy codebase into shared libraries, domain-specific packages, and colocated tests, the same way you would structure any serious software project.

## Reference

- [OPA performance best practices](https://www.openpolicyagent.org/docs/latest/policy-performance/)
- [opa bench CLI reference](https://www.openpolicyagent.org/docs/latest/cli/#opa-bench)
- [OPA indexing internals](https://blog.openpolicyagent.org/optimizing-opa-rule-indexing-59f03f17caf3)

## Additional Resources

- [Styra Academy -- OPA fundamentals](https://academy.styra.com/)
- [OPA profiling with `opa eval --profile`](https://www.openpolicyagent.org/docs/latest/cli/#opa-eval)
- [OPA Playground](https://play.openpolicyagent.org/) -- test performance interactively

# 2. Partial Rules and Default: Building Sets and Objects

## Prerequisites

- `opa` CLI installed
- Completed exercise 1 (Input and Data)

## Learning Objectives

After completing this exercise, you will be able to:

- Use partial rules to accumulate results into sets and objects
- Apply `default` to guarantee a value when no conditions match
- Combine partial rules with `default` to build a complete alert system

## Why Partial Rules

Until now, every rule you wrote produces a single value: `true`, `false`, a string, a number. Those are **complete rules** -- one name, one value. But there are situations where you need a rule to produce a **collection** of values: all violations found, all exposed servers, all permissions for a user. That is what **partial rules** are for.

Think of it this way: a complete rule is like a single variable assignment. A partial rule is like repeatedly appending to a list -- each time the conditions match for a different case, a new element gets added to the collection. The final result is the union of all elements that matched.

## Partial Rules: Set Type

A partial rule with `contains` generates a **set** (unique values, no order).

Create `large-numbers.rego`:

```rego
package demo

import rego.v1

large_numbers contains x if {
    some x in [3, 7, 1, 9, 4, 8]
    x > 5
}
```

```bash
opa eval --format pretty -d large-numbers.rego "data.demo.large_numbers"
```

```
[
  7,
  8,
  9
]
```

The rule `large_numbers` is evaluated for **each** value in the array. Each time `x > 5` is satisfied, that `x` gets added to the set. The result is the set of all numbers greater than 5.

The keyword is `contains` -- it tells OPA "this rule holds elements, not a single value."

## Partial Rules: Object Type

To generate an **object** (key-value pairs), use the `[key]` syntax:

Create `ports.rego`:

```rego
package demo

import rego.v1

servers := ["web", "db", "cache"]

ports[name] := port if {
    some i, name in servers
    ports_list := [80, 5432, 6379]
    port := ports_list[i]
}
```

```bash
opa eval --format pretty -d ports.rego "data.demo.ports"
```

```
{
  "cache": 6379,
  "db": 5432,
  "web": 80
}
```

`ports[name]` says: "the name is the key, and what comes after `:=` is the value." Each iteration adds a key-value pair to the object.

### Intermediate Verification: Complete vs Partial at a Glance

```
Complete rule:    allow := true if { ... }        --> a single value
Partial rule set: violations contains v if { ... } --> set of values
Partial rule obj: details[k] := v if { ... }      --> key-value object
```

Use complete rules when you need a decision (yes/no, a value). Use partial rules when you need to accumulate results.

## The Problem of Undefined and `default`

Before building the full exercise, let's talk about `default`. You may have seen it briefly in `01-elements`, but now it is time to understand it in depth.

When a rule does not match any condition, it is `undefined` -- it does not exist. This can be a problem when an API expects a value to always be present. If your service calls OPA via REST and expects an `allow` field in the response, but OPA returns `{}` (empty, because `allow` is undefined), your service has no value to base its decision on.

`default` solves this: it gives the rule a value when no conditions are met.

Create `input-viewer.json`:

```json
{
    "role": "viewer"
}
```

Create `without-default.rego`:

```rego
package demo

import rego.v1

allow if {
    input.role == "admin"
}
```

```bash
opa eval --format pretty -d without-default.rego -i input-viewer.json "data.demo"
```

```
{}
```

Without `default`, `allow` does not appear in the result. Now with `default`:

Create `with-default.rego`:

```rego
package demo

import rego.v1

default allow := false

allow if {
    input.role == "admin"
}
```

```bash
opa eval --format pretty -d with-default.rego -i input-viewer.json "data.demo"
```

```
{
  "allow": false
}
```

Now `allow` always appears: it is `true` if the condition matches, or `false` if no condition matches (the default activates).

Rule of thumb: **use `default` on rules that an API will consume**. If the consumer is a human reading output, `undefined` is fine. If the consumer is code, it needs a predictable value.

## Building an Alert System

Now let's combine everything: partial rules to accumulate violations, an object for details, and `default` for the final decision.

The scenario: you have an array of servers and need to detect security problems -- missing tags, exposed public IPs, encryption disabled.

Create `policy.rego`:

```rego
package alerts

import rego.v1

default allow := false

# allow only if there are no violations
allow if {
    count(violations) == 0
}

# --- Partial rules (set): accumulate violations ---

# Violation: server without tags
violations contains msg if {
    some server in input.servers
    count(server.tags) == 0
    msg := sprintf("server '%s' has no tags", [server.name])
}

# Violation: server with public IP
violations contains msg if {
    some server in input.servers
    server.public_ip != null
    msg := sprintf("server '%s' has exposed public IP: %s", [server.name, server.public_ip])
}

# Violation: server without encryption
violations contains msg if {
    some server in input.servers
    server.encryption == false
    msg := sprintf("server '%s' does not have encryption enabled", [server.name])
}

# --- Partial rule (object): details per server ---

violation_details[server.name] := details if {
    some server in input.servers

    # Collect problems for THIS server
    problems := {msg |
        count(server.tags) == 0
        msg := "no tags"
    } | {msg |
        server.public_ip != null
        msg := "exposed public IP"
    } | {msg |
        server.encryption == false
        msg := "no encryption"
    }

    # Only include servers that have at least one problem
    count(problems) > 0

    details := {
        "problems": problems,
        "problem_count": count(problems),
    }
}
```

Read the policy step by step:

- `allow` is `false` by default. It is only `true` if there are no violations.
- `violations` is a set that accumulates error messages. There are three definitions of the same rule, one for each type of violation. Each definition iterates the servers and adds a message if it finds the problem.
- `violation_details` is an object that maps the name of each problematic server to its details. It uses the union of three set comprehensions (`|`) to collect the problems for each server.

## Scenario 1: Clean Infrastructure

Create an input where everything is correct -- tags present, no public IP, encryption enabled.

Create `input-clean.json`:

```json
{
    "servers": [
        {
            "name": "web-1",
            "tags": ["production", "frontend"],
            "public_ip": null,
            "encryption": true
        },
        {
            "name": "db-1",
            "tags": ["production", "database"],
            "public_ip": null,
            "encryption": true
        }
    ]
}
```

```bash
opa eval --format pretty -d policy.rego -i input-clean.json "data.alerts"
```

```
{
  "allow": true,
  "violations": []
}
```

Zero violations, `allow` is `true`. Note that `violation_details` does not appear because no server has problems (the condition `count(problems) > 0` is not satisfied for any server, so the object is empty and OPA does not show it).

## Scenario 2: Single Violation

Change one thing from the clean scenario: give one server a public IP.

Create `input-single-violation.json`:

```json
{
    "servers": [
        {
            "name": "web-1",
            "tags": ["production"],
            "public_ip": "54.23.10.5",
            "encryption": true
        },
        {
            "name": "db-1",
            "tags": ["production"],
            "public_ip": null,
            "encryption": true
        }
    ]
}
```

```bash
opa eval --format pretty -d policy.rego -i input-single-violation.json "data.alerts"
```

```
{
  "allow": false,
  "violation_details": {
    "web-1": {
      "problem_count": 1,
      "problems": [
        "exposed public IP"
      ]
    }
  },
  "violations": [
    "server 'web-1' has exposed public IP: 54.23.10.5"
  ]
}
```

One violation found. `allow` is `false` (because `count(violations)` is not 0). `violation_details` shows that `web-1` has a problem. `db-1` does not appear in the details because it is clean.

## Scenario 3: Multiple Violations

Now a scenario where several servers have multiple problems.

Create `input-multiple-violations.json`:

```json
{
    "servers": [
        {
            "name": "web-1",
            "tags": [],
            "public_ip": "54.23.10.5",
            "encryption": false
        },
        {
            "name": "db-1",
            "tags": ["production"],
            "public_ip": null,
            "encryption": false
        },
        {
            "name": "cache-1",
            "tags": ["staging"],
            "public_ip": null,
            "encryption": true
        }
    ]
}
```

```bash
opa eval --format pretty -d policy.rego -i input-multiple-violations.json "data.alerts"
```

```
{
  "allow": false,
  "violation_details": {
    "db-1": {
      "problem_count": 1,
      "problems": [
        "no encryption"
      ]
    },
    "web-1": {
      "problem_count": 3,
      "problems": [
        "exposed public IP",
        "no encryption",
        "no tags"
      ]
    }
  },
  "violations": [
    "server 'db-1' does not have encryption enabled",
    "server 'web-1' does not have encryption enabled",
    "server 'web-1' has no tags",
    "server 'web-1' has exposed public IP: 54.23.10.5"
  ]
}
```

`web-1` has all three problems (no tags, public IP, no encryption). `db-1` has only one (no encryption). `cache-1` is clean and does not appear. The partial rules accumulated all violations from all servers.

## Common Mistakes

### Forgetting `default` with Empty Input

What happens when the input has no servers?

Create `input-empty.json`:

```json
{}
```

```bash
opa eval --format pretty -d policy.rego -i input-empty.json "data.alerts"
```

```
{
  "allow": true
}
```

Without servers, there are no violations, and `count(violations) == 0` is satisfied (an empty set has count 0). But notice that `allow` appears with value `true`, not undefined. That is thanks to the `default allow := false` combined with the rule that checks `count(violations) == 0`.

Now see what would happen if you removed the `default`. Create a version without default to compare:

Create `input-viewer-test.json`:

```json
{
    "role": "viewer"
}
```

Create `without-default-test.rego`:

```rego
package test

import rego.v1

allow if {
    input.role == "admin"
}
```

```bash
opa eval --format pretty -d without-default-test.rego -i input-viewer-test.json "data.test.allow"
```

(no output -- undefined)

Without `default`, when no condition matches, the rule simply does not exist. With `default`, there is always a value. In an API, that difference is critical: `{"allow": false}` is a clear response, `{}` leaves the consumer guessing.

## Verify What You Learned

Create `input-verify-clean.json`:

```json
{
    "servers": [
        {"name": "s1", "tags": ["prod"], "public_ip": null, "encryption": true}
    ]
}
```

```bash
opa eval --format pretty -d policy.rego -i input-verify-clean.json "data.alerts.allow"
```

```
true
```

Create `input-verify-all-violations.json`:

```json
{
    "servers": [
        {"name": "s1", "tags": [], "public_ip": "1.2.3.4", "encryption": false}
    ]
}
```

```bash
opa eval --format pretty -d policy.rego -i input-verify-all-violations.json "data.alerts.violations"
```

```
[
  "server 's1' does not have encryption enabled",
  "server 's1' has no tags",
  "server 's1' has exposed public IP: 1.2.3.4"
]
```

```bash
opa eval --format pretty -d policy.rego -i input-verify-all-violations.json "data.alerts.allow"
```

```
false
```

Create `input-verify-mixed.json`:

```json
{
    "servers": [
        {"name": "ok-server", "tags": ["prod"], "public_ip": null, "encryption": true},
        {"name": "bad-server", "tags": [], "public_ip": null, "encryption": false}
    ]
}
```

```bash
opa eval --format pretty -d policy.rego -i input-verify-mixed.json "data.alerts.violation_details"
```

```
{
  "bad-server": {
    "problem_count": 2,
    "problems": [
      "no encryption",
      "no tags"
    ]
  }
}
```

## What's Next

You now know how to accumulate results with partial rules and guarantee values with `default`. In the next exercise, you will learn comprehensions -- inline expressions that transform data without creating separate rules. Comprehensions are the other side of the same coin as partial rules.

## Reference

- [Partial Rules](https://www.openpolicyagent.org/docs/latest/policy-language/#partial-rules)
- [Default Keyword](https://www.openpolicyagent.org/docs/latest/policy-language/#default-keyword)
- [Generating Sets](https://www.openpolicyagent.org/docs/latest/policy-language/#generating-sets)
- [Generating Objects](https://www.openpolicyagent.org/docs/latest/policy-language/#generating-objects)

## Additional Resources

- [OPA Playground](https://play.openpolicyagent.org/) -- try partial rules interactively
- [Styra Academy](https://academy.styra.com/) -- free OPA/Rego courses
- [OPA Policy Cheatsheet](https://docs.styra.com/opa/rego-cheat-sheet) -- quick reference for Rego syntax

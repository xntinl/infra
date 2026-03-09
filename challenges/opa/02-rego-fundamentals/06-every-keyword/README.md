# 6. The `every` Keyword: Validating That ALL Elements Comply

## Prerequisites

- `opa` CLI installed
- Completed exercise 5 (User-Defined Functions)

## Learning Objectives

After completing this exercise, you will be able to:

- Use the `every` keyword to assert that all elements in a collection satisfy a condition
- Understand the equivalence between `every` and `not` + `some`
- Handle vacuous truth (empty collections) correctly
- Combine `every` with detailed violation reporting for actionable output

## Why `every`

There is a question that comes up constantly in security policies: "Do all servers have encryption enabled?", "Are all databases in private subnets?", "Do all users have MFA activated?" Notice the key word is **all** -- not "some," not "most," but all without exception.

Until now we have used `some` to iterate, which works as "there exists at least one that matches." But `some` and `every` ask fundamentally different questions:

- **`some`**: "Is there any server without encryption?" -- If even ONE lacks encryption, the condition is satisfied.
- **`every`**: "Do all servers have encryption?" -- If even ONE lacks it, the condition fails.

`some` looks for at least one match. `every` demands zero failures. In infrastructure security policies, you almost always need `every`.

## `some` vs `every` Side by Side

Let's see the difference with code.

Create `some-demo.rego`:

```rego
package demos.some_vs_every

import rego.v1

# "some" finds AT LEAST ONE element that matches.
# Here: "is there any number greater than 5?"
# Yes -- 6, 7, and 8 all qualify. One is enough.
result if {
    nums := [2, 4, 6, 7, 8]
    some n in nums
    n > 5
}
```

```bash
opa eval --format pretty -d some-demo.rego "data.demos.some_vs_every.result"
```

```
true
```

`some n in nums; n > 5` asks "does any number greater than 5 exist?" Yes, 6, 7, and 8 qualify. As long as one matches, `some` is satisfied.

Now with `every`. Create `every-demo.rego`:

```rego
package demos.every

import rego.v1

# "every" requires ALL elements to match.
# This FAILS: 2 and 4 are not greater than 5.
all_above_five_fail if {
    nums := [2, 4, 6, 7, 8]
    every n in nums {
        n > 5
    }
}

# This PASSES: all numbers are greater than 5.
all_above_five_pass if {
    nums := [6, 7, 8, 9, 10]
    every n in nums {
        n > 5
    }
}

# Equivalence demo: every vs not+some
# Version with every
all_positive_v1 if {
    nums := [1, 2, 3]
    every n in nums {
        n > 0
    }
}

# Version with not+some (equivalent)
_any_negative if {
    nums := [1, 2, 3]
    some n in nums
    n <= 0
}

all_positive_v2 if {
    not _any_negative
}
```

Case where NOT all elements match:

```bash
opa eval --format pretty -d every-demo.rego "data.demos.every.all_above_five_fail"
```

(no output -- undefined)

Result is `undefined` -- because `every n in nums { n > 5 }` asks "are all numbers greater than 5?" and 2 and 4 are not. A single failure and `every` collapses.

Case where all elements match:

```bash
opa eval --format pretty -d every-demo.rego "data.demos.every.all_above_five_pass"
```

```
true
```

Now all numbers are greater than 5, so `every` is satisfied.

## The Equivalence with `not` + `some`

Before `every` existed in Rego, people wrote the same logic using `not` and `some`. The two forms are equivalent:

```
every x in collection { P(x) }
```

is the same as:

```
not any_fails

any_fails if {
    some x in collection
    not P(x)
}
```

Let's verify. The `every-demo.rego` already has both versions:

```bash
opa eval --format pretty -d every-demo.rego "data.demos.every.all_positive_v1"
```

```
true
```

```bash
opa eval --format pretty -d every-demo.rego "data.demos.every.all_positive_v2"
```

```
true
```

Same result. But `every` is much more readable -- it directly expresses the intent. The `not` + `some` version requires thinking about the negation and creating an auxiliary rule. Whenever you can, use `every`.

## Syntax and Variations

The general form is:

```
every <variable> in <collection> {
    <condition_1>
    <condition_2>
    ...
}
```

You can also use `every` with key-value pairs on objects and index-value pairs on arrays.

Create `every-types.rego`:

```rego
package demos.every_types

import rego.v1

# every with key-value on objects
object_result if {
    obj := {"a": 1, "b": 2, "c": 3}
    every k, v in obj {
        count(k) == 1
        v > 0
    }
}

# every with index-value on arrays
array_result if {
    arr := ["a", "b", "c"]
    every i, v in arr {
        i >= 0
        count(v) == 1
    }
}

# every on an empty collection -- vacuous truth
empty_result if {
    every x in [] {
        x > 100
    }
}
```

With objects, `every k, v` iterates over key and value:

```bash
opa eval --format pretty -d every-types.rego "data.demos.every_types.object_result"
```

```
true
```

With arrays, the "key" is the index:

```bash
opa eval --format pretty -d every-types.rego "data.demos.every_types.array_result"
```

```
true
```

### Intermediate Verification: Vacuous Truth

A case worth understanding: what happens when the collection is empty?

```bash
opa eval --format pretty -d every-types.rego "data.demos.every_types.empty_result"
```

```
true
```

With an empty collection, `every` is automatically satisfied. This is formal logic -- "all elements of an empty set satisfy any property" (vacuous truth). It makes sense: there is no element that violates the condition.

Keep this in mind when writing policies: if `input.servers` could be an empty list, `every server in input.servers { ... }` will pass. If an empty list should be treated as a failure, add an explicit check like `count(input.servers) > 0`.

## Building a Strict Infrastructure Validation

Now let's build a real policy where `every` shines. The scenario: you have a set of servers, a list of service passwords, and a port configuration. You need to validate that **all** of them meet their respective requirements. It is not enough for some to be fine -- in security, a single server without tags is a vulnerable server.

Create `policy.rego`:

```rego
package validation

import rego.v1

# ============================================================
# Required tags -- ALL servers must have them
# ============================================================
required_tags := ["env", "team", "owner", "cost-center"]

all_servers_tagged if {
    every server in input.servers {
        every tag in required_tags {
            server.tags[tag]
            count(server.tags[tag]) > 0
        }
    }
}

default all_servers_tagged := false

# Find which servers fail and why
servers_missing_tags contains detail if {
    some server in input.servers
    missing := {tag |
        some tag in required_tags
        not server.tags[tag]
    } | {tag |
        some tag in required_tags
        server.tags[tag]
        count(server.tags[tag]) == 0
    }
    count(missing) > 0
    detail := {
        "server": server.name,
        "missing_tags": missing,
    }
}

# ============================================================
# Passwords -- ALL must meet requirements
# ============================================================
min_password_length := 12

all_passwords_valid if {
    every pw in input.passwords {
        count(pw.value) >= min_password_length
        regex.match(`[A-Z]`, pw.value)
        regex.match(`[a-z]`, pw.value)
        regex.match(`[0-9]`, pw.value)
        regex.match(`[^a-zA-Z0-9]`, pw.value)
    }
}

default all_passwords_valid := false

# Detail of which passwords fail
password_issues contains detail if {
    some pw in input.passwords
    issues := array.concat(
        array.concat(
            array.concat(
                [msg | count(pw.value) < min_password_length; msg := sprintf("too short (%d chars, minimum %d)", [count(pw.value), min_password_length])],
                [msg | not regex.match(`[A-Z]`, pw.value); msg := "missing uppercase"],
            ),
            [msg | not regex.match(`[a-z]`, pw.value); msg := "missing lowercase"],
        ),
        array.concat(
            [msg | not regex.match(`[0-9]`, pw.value); msg := "missing number"],
            [msg | not regex.match(`[^a-zA-Z0-9]`, pw.value); msg := "missing special character"],
        ),
    )
    count(issues) > 0
    detail := {
        "service": pw.service,
        "issues": issues,
    }
}

# ============================================================
# Ports -- ALL must be in allowed ranges
# ============================================================
allowed_port_ranges := [
    {"min": 80, "max": 80},
    {"min": 443, "max": 443},
    {"min": 8000, "max": 9000},
]

port_in_allowed_range(port) if {
    some range in allowed_port_ranges
    port >= range.min
    port <= range.max
}

all_ports_valid if {
    every entry in input.ports {
        port_in_allowed_range(entry.port)
    }
}

default all_ports_valid := false

# Detail of disallowed ports
invalid_ports contains detail if {
    some entry in input.ports
    not port_in_allowed_range(entry.port)
    detail := {
        "service": entry.service,
        "port": entry.port,
        "message": sprintf("Port %d is not in any allowed range", [entry.port]),
    }
}

# ============================================================
# Comparison: the same tag validation with some + not
# (to show that every is more readable)
# ============================================================

# Version with some + not (equivalent to all_servers_tagged)
_some_server_missing_tag if {
    some server in input.servers
    some tag in required_tags
    not server.tags[tag]
}

_some_server_empty_tag if {
    some server in input.servers
    some tag in required_tags
    server.tags[tag]
    count(server.tags[tag]) == 0
}

all_servers_tagged_v2 if {
    not _some_server_missing_tag
    not _some_server_empty_tag
}

default all_servers_tagged_v2 := false

# ============================================================
# General summary
# ============================================================
fully_compliant if {
    all_servers_tagged
    all_passwords_valid
    all_ports_valid
}

default fully_compliant := false

summary := {
    "fully_compliant": fully_compliant,
    "tags_ok": all_servers_tagged,
    "passwords_ok": all_passwords_valid,
    "ports_ok": all_ports_valid,
    "servers_missing_tags": servers_missing_tags,
    "password_issues": password_issues,
    "invalid_ports": invalid_ports,
}
```

Now let's create three input scenarios to see how `every` behaves in different situations.

### Scenario 1: Everything Passes

Create `input-ok.json`:

```json
{
    "servers": [
        {
            "name": "web-1",
            "tags": {
                "env": "prod",
                "team": "platform",
                "owner": "alice",
                "cost-center": "CC-001"
            }
        },
        {
            "name": "web-2",
            "tags": {
                "env": "prod",
                "team": "platform",
                "owner": "bob",
                "cost-center": "CC-001"
            }
        }
    ],
    "passwords": [
        {
            "service": "database",
            "value": "MyS3cur3!Pass"
        },
        {
            "service": "cache",
            "value": "An0th3r$ecure1"
        }
    ],
    "ports": [
        { "service": "frontend", "port": 443 },
        { "service": "api", "port": 8080 },
        { "service": "web", "port": 80 }
    ]
}
```

```bash
opa eval --format pretty -d policy.rego -i input-ok.json "data.validation.summary"
```

```
{
  "fully_compliant": true,
  "invalid_ports": [],
  "password_issues": [],
  "passwords_ok": true,
  "ports_ok": true,
  "servers_missing_tags": [],
  "tags_ok": true
}
```

Everything passes. `every` is satisfied in all three categories.

### Scenario 2: One Server Breaks Everything

Change only one thing from the passing scenario: add a third server with incomplete tags.

Create `input-tags-fail.json`:

```json
{
    "servers": [
        {
            "name": "web-1",
            "tags": {
                "env": "prod",
                "team": "platform",
                "owner": "alice",
                "cost-center": "CC-001"
            }
        },
        {
            "name": "web-2",
            "tags": {
                "env": "prod",
                "team": "platform",
                "owner": "bob",
                "cost-center": "CC-001"
            }
        },
        {
            "name": "web-3",
            "tags": {
                "env": "staging",
                "team": ""
            }
        }
    ],
    "passwords": [
        {
            "service": "database",
            "value": "MyS3cur3!Pass"
        }
    ],
    "ports": [
        { "service": "frontend", "port": 443 }
    ]
}
```

Notice: `web-1` and `web-2` are perfect, but `web-3` is missing `owner` and `cost-center`, and `team` is empty. A single failing server makes `every` return false for the **entire** validation. That is exactly what we want in security.

```bash
opa eval --format pretty -d policy.rego -i input-tags-fail.json "data.validation.summary"
```

```
{
  "fully_compliant": false,
  "invalid_ports": [],
  "password_issues": [],
  "passwords_ok": true,
  "ports_ok": true,
  "servers_missing_tags": [
    {
      "missing_tags": [
        "cost-center",
        "owner",
        "team"
      ],
      "server": "web-3"
    }
  ],
  "tags_ok": false
}
```

A single server with incomplete tags (`web-3`) makes `all_servers_tagged` `false`. The other two servers are perfect, but `every` does not forgive.

### Scenario 3: Multiple Failures Across All Categories

Create `input-multi-fail.json`:

```json
{
    "servers": [
        {
            "name": "legacy-box",
            "tags": {
                "env": "prod"
            }
        }
    ],
    "passwords": [
        {
            "service": "database",
            "value": "MyS3cur3!Pass"
        },
        {
            "service": "admin-panel",
            "value": "admin123"
        },
        {
            "service": "ftp",
            "value": "short"
        }
    ],
    "ports": [
        { "service": "frontend", "port": 443 },
        { "service": "ssh", "port": 22 },
        { "service": "debug", "port": 3000 },
        { "service": "api", "port": 8080 }
    ]
}
```

This scenario is a mess -- the server has missing tags, there are weak passwords, and there are unauthorized ports.

```bash
opa eval --format pretty -d policy.rego -i input-multi-fail.json "data.validation.summary"
```

You should see `fully_compliant: false`, with details about servers missing tags, weak passwords (`admin-panel` is missing a special character, `ftp` is too short and missing several requirements), and unauthorized ports (22 and 3000).

## Common Mistakes

### Assuming `every` Fails on Empty Collections

A frequent mistake is expecting `every` to return `false` when the collection is empty. As discussed in the syntax section, `every x in [] { ... }` returns `true` (vacuous truth). If your policy should reject an empty list of servers, add an explicit guard:

```rego
# WRONG: passes even with zero servers
all_servers_encrypted if {
    every s in input.servers {
        s.encryption == true
    }
}

# RIGHT: requires at least one server AND all must be encrypted
all_servers_encrypted if {
    count(input.servers) > 0
    every s in input.servers {
        s.encryption == true
    }
}
```

### Intermediate Verification: `every` vs `not` + `some` Equivalence

Let's verify that the two approaches produce the same result.

Create `compare.rego`:

```rego
package demos.compare

import rego.v1

import data.validation.all_servers_tagged
import data.validation.all_servers_tagged_v2

# Compare the two approaches: every vs not+some
v1 := all_servers_tagged

v2 := all_servers_tagged_v2

both_match if {
    v1 == v2
}
```

```bash
opa eval --format pretty -d policy.rego -d compare.rego -i input-tags-fail.json "data.demos.compare"
```

```
{
  "both_match": true,
  "v1": false,
  "v2": false
}
```

Both versions produce the same result. But if you read the code again, `every server in input.servers { every tag in required_tags { ... } }` is immediately clear. The version with `_some_server_missing_tag` and `not` requires more mental effort to parse.

## When to Use `some` vs `every`

| Question | Keyword | Example |
|---|---|---|
| Does at least one resource violate X? | `some` | Check if any port is open to 0.0.0.0/0 |
| Do all resources satisfy X? | `every` | Validate that all buckets have encryption |
| Find the resources that violate X | `some` + collect | List which servers lack tags |
| Guarantee total compliance | `every` | Ensure all infrastructure meets the policy |

The rule of thumb: if the question contains the word "all" or "each," use `every`. If it contains "any" or "exists," use `some`.

## Verify What You Learned

```bash
opa eval --format pretty -d policy.rego -i input-ok.json "data.validation.fully_compliant"
```

```
true
```

```bash
opa eval --format pretty -d policy.rego -i input-tags-fail.json "data.validation.tags_ok"
```

```
false
```

```bash
opa eval --format pretty -d policy.rego -i input-tags-fail.json "data.validation.servers_missing_tags"
```

```
[
  {
    "missing_tags": [
      "cost-center",
      "owner",
      "team"
    ],
    "server": "web-3"
  }
]
```

```bash
opa eval --format pretty -d policy.rego -i input-multi-fail.json "data.validation.ports_ok"
```

```
false
```

```bash
opa eval --format pretty -d policy.rego -d compare.rego -i input-tags-fail.json "data.demos.compare.both_match"
```

```
true
```

## Section Summary: Rego Fundamentals

You have completed all six exercises in the Rego Fundamentals section. Here is what you covered:

**Key concepts learned:**

1. **Input vs Data** -- `input` carries per-request dynamic data, `data` carries static configuration. Policies connect the two. Separating them lets each change independently.
2. **Partial Rules and Default** -- Partial rules accumulate results into sets (`contains`) and objects (`[key]`). `default` guarantees a value when no conditions match, which is critical for API consumers.
3. **Comprehensions** -- Inline expressions for transforming data: sets `{x | ...}` for unique values, arrays `[x | ...]` for ordered/duplicated values, objects `{k: v | ...}` for key-value mappings. Use comprehensions for intermediate calculations inside rules, partial rules for package-level results.
4. **Built-in Functions** -- Rego's standard library covers strings, regex, networking (`net.cidr_contains`, `net.cidr_intersects`), time (`time.parse_rfc3339_ns`), aggregates (`count`, `sum`, `min`, `max`), types, and encoding. Always use `--format pretty` during development.
5. **User-Defined Functions** -- Extract repeated logic into parameterized functions. Multiple definitions of the same function enable pattern-matching behavior. Rules depend on `input`; functions receive explicit arguments.
6. **The `every` Keyword** -- Asserts that ALL elements satisfy a condition. One failure and it collapses. Equivalent to `not` + `some` but far more readable. Empty collections satisfy `every` (vacuous truth) -- add explicit guards if that is not desired.

**Important notes to remember:**

- OPA does not throw errors for missing data -- it returns `undefined`. This is safe by default but can mask typos. Always verify intermediate values when debugging.
- `default` should be used on any rule consumed by an API. Humans can interpret missing output; code cannot.
- Separate configuration (thresholds, allowed ranges, required tags) from logic (validation rules). This makes policies reusable across environments.
- Use `sprintf` for descriptive error messages. Boolean-only violations are hard to act on.
- `every` on an empty collection returns `true`. If you need at least one element, check `count(...) > 0` explicitly.
- Always use `--format pretty` with `opa eval` to get clean, readable output instead of the raw evaluation wrapper.

## What's Next

You now have a solid command of the Rego language -- from basic types and operators through comprehensions, built-ins, functions, and universal quantification with `every`. The next section, [03-rego-testing](../../03-rego-testing/01-unit-tests/), teaches you how to write automated tests for your policies so you can refactor and extend them with confidence.

## Reference

- [OPA -- keyword `every`](https://www.openpolicyagent.org/docs/latest/policy-language/#every-keyword)
- [OPA -- keyword `some`](https://www.openpolicyagent.org/docs/latest/policy-language/#some-keyword)
- [Rego v1 and `import rego.v1`](https://www.openpolicyagent.org/docs/latest/policy-language/#future-keywords)
- [OPA Policy Language -- complete reference](https://www.openpolicyagent.org/docs/latest/policy-language/)

## Additional Resources

- [OPA Playground](https://play.openpolicyagent.org/) -- experiment with `every` and `some` interactively
- [Styra Academy](https://academy.styra.com/) -- free OPA/Rego courses
- [OPA Policy Cheatsheet](https://docs.styra.com/opa/rego-cheat-sheet) -- quick reference for Rego syntax
- [Rego Style Guide](https://github.com/StyraInc/rego-style-guide) -- community best practices for writing Rego

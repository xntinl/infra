# 5. User-Defined Functions: Reusing Logic

## Prerequisites

- `opa` CLI installed
- Completed exercise 4 (Built-in Functions)

## Learning Objectives

After completing this exercise, you will be able to:

- Define reusable functions that accept parameters
- Write functions with multiple definitions for pattern-matching behavior
- Apply the DRY principle by extracting repeated validation logic into helpers
- Distinguish between rules (which depend on `input`) and functions (which receive explicit arguments)

## Why User-Defined Functions

When you have the same validation logic repeated in five different rules, every change requires updating all the copies. It is the same problem as in any other language -- and the solution is the same: extract the common logic into a reusable function.

Consider a policy that validates naming conventions for servers, databases, and caches. Without functions, you end up with the same regex in three separate rules:

```rego
# WITHOUT functions - repeated logic everywhere

server_name_violation contains msg if {
    some s in input.servers
    not regex.match(`^[a-z][a-z0-9-]*$`, s.name)
    msg := sprintf("Server '%s' has invalid name", [s.name])
}

database_name_violation contains msg if {
    some db in input.databases
    not regex.match(`^[a-z][a-z0-9-]*$`, db.name)
    msg := sprintf("Database '%s' has invalid name", [db.name])
}

cache_name_violation contains msg if {
    some c in input.caches
    not regex.match(`^[a-z][a-z0-9-]*$`, c.name)
    msg := sprintf("Cache '%s' has invalid name", [c.name])
}
```

Three times the same regex. If you change the naming convention, you have to update three rules. That is a bug waiting to happen. Functions solve this.

## Rules vs Functions

This distinction is fundamental and causes confusion at first.

A **rule** computes a value from `input` or `data`. It does not receive arguments -- its context comes from `input` and `data`.

A **function** receives arguments explicitly. You can call it with different values.

Create `input-name.json`:

```json
{
    "name": "prod-server"
}
```

Create `function-basics.rego`:

```rego
package demos.functions

import rego.v1

# ============================================================
# Rule (depends on input, not a function)
# ============================================================
valid_name if {
    regex.match("^[a-z-]+$", input.name)
}

# ============================================================
# Functions (receive arguments, reusable)
# ============================================================

# Validates a name with regex
is_valid(n) if {
    regex.match("^[a-z-]+$", n)
}

# Checks if a string is all lowercase
is_lowercase(s) if {
    s == lower(s)
}

# Checks if a string is shorter than 5 characters (with explicit false default)
default is_short(_) := false

is_short(s) := true if {
    count(s) < 5
}

# Validates a name: 3-30 chars, starts with letter, lowercase + digits + hyphens
name_ok(n) if {
    count(n) >= 3
    count(n) <= 30
    regex.match("^[a-z][a-z0-9-]*$", n)
}

# ============================================================
# Demo results (pre-computed calls for terminal evaluation)
# ============================================================
is_valid_prod := is_valid("prod-server")

is_valid_upper := is_valid("PROD_SERVER")

is_lowercase_hello := is_lowercase("hello")

is_lowercase_mixed := is_lowercase("Hello")

is_short_hi := is_short("hi")

is_short_long := is_short("superlong")

name_ok_valid := name_ok("prod-api")

name_ok_short := name_ok("ab")
```

First, the rule that depends on `input.name`:

```bash
opa eval --format pretty -d function-basics.rego -i input-name.json "data.demos.functions.valid_name"
```

```
true
```

That is a rule: it depends on `input.name`. If you want to validate a different name, you need to change the input. Now let's see a function:

```bash
opa eval --format pretty -d function-basics.rego "data.demos.functions.is_valid_prod"
```

```
true
```

```bash
opa eval --format pretty -d function-basics.rego "data.demos.functions.is_valid_upper"
```

(no output -- undefined)

The function `is_valid` receives the name as an argument. In the file, `is_valid_prod` calls `is_valid("prod-server")` and `is_valid_upper` calls `is_valid("PROD_SERVER")`. You can validate any string without changing the input.

## Boolean Functions

The simplest functions answer yes or no. They are perfect for validations.

```bash
opa eval --format pretty -d function-basics.rego "data.demos.functions.is_lowercase_hello"
```

```
true
```

```bash
opa eval --format pretty -d function-basics.rego "data.demos.functions.is_lowercase_mixed"
```

(no output -- undefined)

Notice that when a boolean function fails (the condition is not met), it returns `undefined`, not `false`. If you need an explicit `false`, you can use `default`:

```bash
opa eval --format pretty -d function-basics.rego "data.demos.functions.is_short_hi"
```

```
true
```

```bash
opa eval --format pretty -d function-basics.rego "data.demos.functions.is_short_long"
```

```
false
```

Thanks to `default is_short(_) := false`, when the condition is not met it returns `false` instead of `undefined`. The `_` wildcard means "for any argument."

You can also have multiple conditions inside a function -- all must be satisfied (implicit AND, just like in rules):

```bash
opa eval --format pretty -d function-basics.rego "data.demos.functions.name_ok_valid"
```

```
true
```

```bash
opa eval --format pretty -d function-basics.rego "data.demos.functions.name_ok_short"
```

(no output -- undefined)

The result is `undefined` because `"ab"` has fewer than 3 characters. The function simply does not produce a result for that case.

## Functions That Return Values

Functions do not only say yes or no -- they can also compute and return concrete values. This is very useful for transforming data.

Create `function-returns.rego`:

```rego
package demos.returns

import rego.v1

# Normalizes a string: trims whitespace and lowercases
normalize(s) := lower(trim_space(s))

# Builds a resource ID from type and name
resource_id(rtype, name) := sprintf("%s-%s", [rtype, name])

# Returns severity label based on count (multiple definitions)
severity(n) := "critical" if {
    n > 10
}

severity(n) := "warning" if {
    n > 5
    n <= 10
}

severity(n) := "info" if {
    n <= 5
}

# Demo results
normalize_demo := normalize("  Prod-Server  ")

resource_id_demo := resource_id("compute", "api-server")

severity_critical := severity(15)

severity_warning := severity(7)

severity_info := severity(2)
```

```bash
opa eval --format pretty -d function-returns.rego "data.demos.returns.normalize_demo"
```

```
"prod-server"
```

```bash
opa eval --format pretty -d function-returns.rego "data.demos.returns.resource_id_demo"
```

```
"compute-api-server"
```

```bash
opa eval --format pretty -d function-returns.rego "data.demos.returns.severity_critical"
```

```
"critical"
```

```bash
opa eval --format pretty -d function-returns.rego "data.demos.returns.severity_warning"
```

```
"warning"
```

```bash
opa eval --format pretty -d function-returns.rego "data.demos.returns.severity_info"
```

```
"info"
```

The last example shows a function with multiple definitions, each with different conditions. Rego picks the one that matches -- it is equivalent to pattern matching or a declarative `switch/case`.

### Intermediate Verification: Multiple Arguments

Functions can take as many arguments as needed.

Create `function-multi-arg.rego`:

```rego
package demos.multiarg

import rego.v1

# Checks if a value is within a range (inclusive)
in_range(value, minimum, maximum) if {
    value >= minimum
    value <= maximum
}

# Checks if a tag key exists and has a non-empty value
tag_complete(tags, key) if {
    tags[key]
    count(tags[key]) > 0
}

# Demo results
in_range_yes := in_range(5, 1, 10)

in_range_no := in_range(15, 1, 10)

tag_env := tag_complete({"env": "prod", "team": ""}, "env")

tag_team := tag_complete({"env": "prod", "team": ""}, "team")
```

```bash
opa eval --format pretty -d function-multi-arg.rego "data.demos.multiarg.in_range_yes"
```

```
true
```

```bash
opa eval --format pretty -d function-multi-arg.rego "data.demos.multiarg.in_range_no"
```

(no output -- undefined)

```bash
opa eval --format pretty -d function-multi-arg.rego "data.demos.multiarg.tag_env"
```

```
true
```

```bash
opa eval --format pretty -d function-multi-arg.rego "data.demos.multiarg.tag_team"
```

(no output -- undefined)

`tag_complete` returns `undefined` for `team` because the tag has an empty value -- `count("") > 0` fails.

## Building a Validation Helper Library

Now let's build something real. We will create a set of helper functions and use them in clean, readable policy rules. Compare this to the repeated-regex example from the beginning of this exercise -- the difference in maintainability is dramatic.

Create `policy.rego`:

```rego
package helpers

import rego.v1

# ============================================================
# Helper functions: logic lives here, defined once
# ============================================================

# Validates that a name follows the convention: lowercase, numbers, hyphens.
# Minimum 2 characters, must start with a letter.
is_valid_name(name) if {
    count(name) >= 2
    regex.match(`^[a-z][a-z0-9-]*[a-z0-9]$`, name)
}

# Checks if a CIDR is public (not in any private range)
private_ranges := ["10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"]

is_public_cidr(cidr) if {
    not _is_private(cidr)
}

_is_private(cidr) if {
    some range in private_ranges
    net.cidr_contains(range, cidr)
}

# Generates a standardized resource name: "{env}-{type}-{name}"
resource_name(env, rtype, name) := sprintf("%s-%s-%s", [env, rtype, name])

# Determines severity based on the number of violations
severity_level(n) := "critical" if {
    n > 10
}

severity_level(n) := "high" if {
    n > 5
    n <= 10
}

severity_level(n) := "medium" if {
    n > 2
    n <= 5
}

severity_level(n) := "low" if {
    n <= 2
}

# Verifies that a resource has all required tags with non-empty values
has_required_tags(resource, required) if {
    every tag in required {
        resource.tags[tag]
        count(resource.tags[tag]) > 0
    }
}

# ============================================================
# Policies that USE the helpers: clean and readable
# ============================================================

# Naming violations -- a single rule for all resources
naming_violations contains violation if {
    some resource in input.resources
    not is_valid_name(resource.name)
    violation := {
        "resource": resource.name,
        "type": resource.type,
        "message": sprintf("Name '%s' does not meet the convention", [resource.name]),
        "severity": "high",
    }
}

# Network violations
network_violations contains violation if {
    some resource in input.resources
    resource.cidr
    is_public_cidr(resource.cidr)
    violation := {
        "resource": resource.name,
        "type": resource.type,
        "message": sprintf("CIDR %s is public", [resource.cidr]),
        "severity": "critical",
    }
}

# Tag violations
required_tags := ["env", "team", "owner"]

tag_violations contains violation if {
    some resource in input.resources
    not has_required_tags(resource, required_tags)
    missing := {tag |
        some tag in required_tags
        not resource.tags[tag]
    } | {tag |
        some tag in required_tags
        resource.tags[tag]
        count(resource.tags[tag]) == 0
    }
    violation := {
        "resource": resource.name,
        "type": resource.type,
        "message": sprintf("Missing or empty tags: %v", [missing]),
        "severity": "medium",
    }
}

# Summary with severity calculated by the helper function
all_violations := naming_violations | network_violations | tag_violations

report := {
    "total_violations": count(all_violations),
    "severity": severity_level(count(all_violations)),
    "naming_violations": naming_violations,
    "network_violations": network_violations,
    "tag_violations": tag_violations,
    "suggested_names": {resource_name(r.env, r.type, r.name) |
        some r in input.resources
        r.env
    },
}
```

Notice the difference: the violation rules are now clean and declarative. All the complex logic -- the regex, the private CIDR check, the tag verification -- is encapsulated in functions. If tomorrow you change the naming convention, you only touch `is_valid_name`.

Now create `input.json`:

```json
{
    "resources": [
        {
            "name": "prod-api",
            "type": "compute",
            "env": "prod",
            "cidr": "10.0.1.0/24",
            "tags": {
                "env": "production",
                "team": "platform",
                "owner": "alice"
            }
        },
        {
            "name": "Staging_DB",
            "type": "database",
            "env": "staging",
            "cidr": "172.16.5.0/24",
            "tags": {
                "env": "staging",
                "team": "",
                "owner": "bob"
            }
        },
        {
            "name": "dev-cache",
            "type": "cache",
            "env": "dev",
            "cidr": "203.0.113.0/24",
            "tags": {
                "env": "dev",
                "team": "backend",
                "owner": "carol"
            }
        },
        {
            "name": "x",
            "type": "compute",
            "env": "test",
            "cidr": "10.0.2.0/24",
            "tags": {
                "env": "test"
            }
        },
        {
            "name": "prod-monitoring",
            "type": "observability",
            "env": "prod",
            "cidr": "10.10.0.0/16",
            "tags": {
                "env": "production",
                "team": "sre",
                "owner": "dave"
            }
        }
    ]
}
```

Here is what each resource has:

- `prod-api`: everything correct. Valid name, private CIDR, all tags present.
- `Staging_DB`: uppercase letters and underscore in the name (violates naming). Tags incomplete -- `team` is empty.
- `dev-cache`: valid name, but CIDR `203.0.113.0/24` is public (violates network rules).
- `x`: name too short (violates naming). Missing tags `team` and `owner`.
- `prod-monitoring`: everything correct.

## Common Mistakes

### Using `not` with a Function That Returns a Value Instead of Boolean

When a function returns a value (not just `true`/`undefined`), you cannot negate it with `not`. `not` only works with boolean functions or rules. If you need to check that a value-returning function does not produce a certain result, compare the return value instead:

```rego
# WRONG: not severity_level(5) does not make sense
# RIGHT: severity_level(5) != "critical"
```

This is a subtle difference from general-purpose languages where you might negate any expression.

## Verify What You Learned

```bash
opa eval --format pretty -d policy.rego -i input.json "data.helpers.naming_violations"
```

```
[
  {
    "message": "Name 'Staging_DB' does not meet the convention",
    "resource": "Staging_DB",
    "severity": "high",
    "type": "database"
  },
  {
    "message": "Name 'x' does not meet the convention",
    "resource": "x",
    "severity": "high",
    "type": "compute"
  }
]
```

```bash
opa eval --format pretty -d policy.rego -i input.json "data.helpers.report.suggested_names"
```

```
[
  "dev-cache-dev-cache",
  "prod-compute-prod-api",
  "prod-observability-prod-monitoring",
  "staging-database-Staging_DB",
  "test-compute-x"
]
```

Notice the suggested names use the format `{env}-{type}-{name}`. For `Staging_DB` the suggestion still contains the badly formed original name -- the `resource_name` function only applies the format, it does not fix the base name. That would be another function you could add.

```bash
opa eval --format pretty -d policy.rego -i input.json "data.helpers.report.severity"
```

```
"medium"
```

```bash
opa eval --format pretty -d policy.rego -i input.json "data.helpers.report.total_violations"
```

```
5
```

```bash
opa eval --format pretty -d policy.rego -i input.json "data.helpers.network_violations"
```

```
[
  {
    "message": "CIDR 203.0.113.0/24 is public",
    "resource": "dev-cache",
    "severity": "critical",
    "type": "cache"
  }
]
```

## What's Next

You now know how to define your own reusable functions, use multiple definitions for pattern matching, and structure policies with clean helper layers. In the next and final exercise, you will learn the `every` keyword -- the tool for asserting that ALL elements in a collection satisfy a condition, which is exactly what security policies demand.

## Reference

- [OPA -- Custom Functions](https://www.openpolicyagent.org/docs/latest/policy-language/#functions)
- [OPA -- Multiple Function Definitions](https://www.openpolicyagent.org/docs/latest/policy-language/#functions)
- [OPA -- Default Keyword with Functions](https://www.openpolicyagent.org/docs/latest/policy-language/#default-keyword)

## Additional Resources

- [OPA Playground](https://play.openpolicyagent.org/) -- test functions interactively
- [Styra Academy](https://academy.styra.com/) -- free OPA/Rego courses
- [OPA Policy Cheatsheet](https://docs.styra.com/opa/rego-cheat-sheet) -- quick reference for Rego syntax

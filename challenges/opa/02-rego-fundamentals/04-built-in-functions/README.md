# 4. Built-in Functions: Rego's Standard Library

## Prerequisites

- `opa` CLI installed
- Completed exercise 3 (Comprehensions)

## Learning Objectives

After completing this exercise, you will be able to:

- Use string, regex, network, time, and aggregate built-in functions
- Combine multiple built-in functions in a single infrastructure validation policy
- Produce descriptive error messages with `sprintf`

## Why Built-in Functions

Until now we have written rules with simple comparisons, iterations, and comprehensions. But real infrastructure policies need to validate naming conventions with regex, check that CIDRs fall within private ranges, verify that certificates have not expired, and parse encoded data. Writing that logic from scratch would be impractical.

Rego ships with an extensive library of built-in functions that cover all of these cases. In this exercise we will tour the most useful categories through a quick reference, and then combine them in a practical infrastructure validation policy.

## Quick Reference: Key Built-in Functions

Before building the full policy, here is a compact reference of the functions you will use most often. All examples use `--format pretty` so you see clean values.

Create `reference.rego`:

```rego
package reference

import rego.v1

# --- Strings ---
contains_result := contains("prod-api-server", "api")
startswith_result := startswith("prod-api-server", "prod")
endswith_result := endswith("backup-2024.tar.gz", ".tar.gz")
split_result := split("prod-api-server", "-")
concat_result := concat("/", ["org", "project", "resource"])
lower_result := lower("Prod-API-Server")
upper_result := upper("prod")
trim_space_result := trim_space("  hello world  ")
replace_result := replace("prod_api_server", "_", "-")
sprintf_result := sprintf("Resource %s has %d instances", ["api-server", 3])
substring_result := substring("prod-api-server", 5, 3)

# --- Regex ---
regex_valid := regex.match("^[a-z][a-z0-9-]*$", "prod-api-server")
regex_invalid := regex.match("^[a-z][a-z0-9-]*$", "Prod_API")

# --- Aggregates ---
count_result := count([1, 2, 3, 4, 5])
sum_result := sum([10, 20, 30])
min_result := min([5, 2, 8, 1, 9])
max_result := max([5, 2, 8, 1, 9])
sort_result := sort([5, 2, 8, 1, 9])

# --- Network ---
cidr_contains_result := net.cidr_contains("10.0.0.0/8", "10.1.2.3")
cidr_public_check := net.cidr_contains("10.0.0.0/8", "203.0.113.0/24")
cidr_intersects_result := net.cidr_intersects("10.0.0.0/16", "10.0.128.0/17")

# --- Time ---
parsed_ts := time.parse_rfc3339_ns("2025-01-15T10:30:00Z")
ts_comparison := time.parse_rfc3339_ns("2025-06-01T00:00:00Z") > time.parse_rfc3339_ns("2025-01-01T00:00:00Z")

# --- Types ---
type_of_number := type_name(42)
type_of_string := type_name("hello")
check_is_string := is_string("hello")

# --- Encoding ---
b64_encoded := base64.encode("hello world")
b64_decoded := base64.decode("aGVsbG8gd29ybGQ=")
```

Let's verify a few key ones:

```bash
opa eval --format pretty -d reference.rego "data.reference.split_result"
```

```
[
  "prod",
  "api",
  "server"
]
```

```bash
opa eval --format pretty -d reference.rego "data.reference.cidr_contains_result"
```

```
true
```

```bash
opa eval --format pretty -d reference.rego "data.reference.cidr_public_check"
```

```
false
```

`10.0.0.0/8` does not contain `203.0.113.0/24` -- the public CIDR falls outside the private range.

```bash
opa eval --format pretty -d reference.rego "data.reference.ts_comparison"
```

```
true
```

June is after January. Time values are compared as nanosecond integers.

```bash
opa eval --format pretty -d reference.rego "data.reference.type_of_number"
```

```
"number"
```

You can explore any other rule in the reference file the same way. Now let's put these functions to practical use.

## Building an Infrastructure Validation Policy

The real value of built-in functions becomes clear when you combine them. We will build a policy that validates infrastructure resources against four categories of rules: naming conventions, network security, resource limits, and age.

### Why This Structure

Instead of hardcoding thresholds and patterns, the policy separates configuration from logic. The private CIDR ranges, naming patterns, and maximum age are defined as data at the top. The validation rules reference that data. If the naming convention changes, you update one line -- not every rule that checks names.

Create `policy.rego`:

```rego
package infra_validation

import rego.v1

# ============================================================
# Configuration: separated from logic
# ============================================================
private_cidrs := ["10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"]
max_resources_per_type := 10
max_age_days := 365
max_age_ns := max_age_days * 24 * 60 * 60 * 1000000000

# ============================================================
# Helper functions
# ============================================================

# Naming convention: lowercase letters, numbers, and hyphens only.
# Must start with a letter and cannot end with a hyphen.
valid_name(name) if {
    regex.match(`^[a-z][a-z0-9-]*[a-z0-9]$`, name)
}

# Single-character names are also valid if the character is a letter
valid_name(name) if {
    count(name) == 1
    regex.match(`^[a-z]$`, name)
}

# Check if a CIDR falls within any private range (RFC 1918)
is_private_cidr(cidr) if {
    some private_range in private_cidrs
    net.cidr_contains(private_range, cidr)
}

# ============================================================
# Naming violations (uses: regex.match, sprintf)
# ============================================================
naming_violations contains msg if {
    some resource in input.resources
    not valid_name(resource.name)
    msg := sprintf("Resource '%s' (type: %s) does not meet naming convention (lowercase, numbers, hyphens only)",
        [resource.name, resource.type])
}

# ============================================================
# Network violations (uses: net.cidr_contains)
# ============================================================
network_violations contains msg if {
    some resource in input.resources
    resource.cidr
    not is_private_cidr(resource.cidr)
    msg := sprintf("Resource '%s' uses public CIDR: %s", [resource.name, resource.cidr])
}

# ============================================================
# Count violations (uses: count, comprehensions)
# ============================================================
resource_counts[resource_type] := n if {
    some resource_type in {r.type | some r in input.resources}
    n := count([1 | some r in input.resources; r.type == resource_type])
}

count_violations contains msg if {
    some resource_type, quantity in resource_counts
    quantity > max_resources_per_type
    msg := sprintf("Too many resources of type '%s': %d (maximum: %d)",
        [resource_type, quantity, max_resources_per_type])
}

# ============================================================
# Age violations (uses: time.parse_rfc3339_ns, time.now_ns)
# ============================================================
age_violations contains msg if {
    some resource in input.resources
    resource.created_at
    created_ns := time.parse_rfc3339_ns(resource.created_at)
    age_ns := time.now_ns() - created_ns
    age_ns > max_age_ns
    days := age_ns / (24 * 60 * 60 * 1000000000)
    msg := sprintf("Resource '%s' is %d days old (maximum: %d)",
        [resource.name, days, max_age_days])
}

# ============================================================
# Summary
# ============================================================
all_violations := naming_violations | network_violations | count_violations | age_violations

total_violations := count(all_violations)

compliant if {
    total_violations == 0
}

default compliant := false

summary := {
    "compliant": compliant,
    "total_violations": total_violations,
    "naming_violations": naming_violations,
    "network_violations": network_violations,
    "count_violations": count_violations,
    "age_violations": age_violations,
    "resource_counts": resource_counts,
}
```

Now create `input.json`:

```json
{
    "resources": [
        {
            "name": "prod-api-server",
            "type": "compute",
            "cidr": "10.0.1.0/24",
            "created_at": "2025-06-15T10:00:00Z"
        },
        {
            "name": "staging-db",
            "type": "database",
            "cidr": "172.16.5.0/24",
            "created_at": "2025-09-01T08:30:00Z"
        },
        {
            "name": "Prod_Cache_Server",
            "type": "cache",
            "cidr": "10.0.2.0/24",
            "created_at": "2026-01-10T14:00:00Z"
        },
        {
            "name": "dev-frontend",
            "type": "compute",
            "cidr": "203.0.113.0/24",
            "created_at": "2026-02-20T09:00:00Z"
        },
        {
            "name": "prod-vpc",
            "type": "network",
            "cidr": "192.168.0.0/16",
            "created_at": "2024-01-01T00:00:00Z"
        },
        {
            "name": "monitoring-",
            "type": "observability",
            "cidr": "10.10.0.0/16",
            "created_at": "2026-03-01T12:00:00Z"
        }
    ]
}
```

Here is what we have in the input:

- `prod-api-server` and `staging-db`: valid names, private CIDRs. Everything is fine.
- `Prod_Cache_Server`: uppercase letters and underscores in the name. Violates naming convention.
- `dev-frontend`: valid name, but CIDR `203.0.113.0/24` is public. Violates network rules.
- `prod-vpc`: name and CIDR are fine, but it was created on January 1, 2024 -- more than a year old.
- `monitoring-`: ends with a hyphen. Violates naming convention.

### Intermediate Verification

Let's check the naming violations first before looking at the full summary:

```bash
opa eval --format pretty -d policy.rego -i input.json "data.infra_validation.naming_violations"
```

```
[
  "Resource 'Prod_Cache_Server' (type: cache) does not meet naming convention (lowercase, numbers, hyphens only)",
  "Resource 'monitoring-' (type: observability) does not meet naming convention (lowercase, numbers, hyphens only)"
]
```

Two naming violations, as expected. Now the network violations:

```bash
opa eval --format pretty -d policy.rego -i input.json "data.infra_validation.network_violations"
```

```
[
  "Resource 'dev-frontend' uses public CIDR: 203.0.113.0/24"
]
```

One network violation. The public CIDR `203.0.113.0/24` is not contained in any of the three private ranges.

## Common Mistakes

### Forgetting `--format pretty` and Getting Raw JSON

If you run `opa eval` without `--format pretty`, you get the raw evaluation result wrapped in `result`, `expressions`, and `value` objects:

```bash
opa eval -d policy.rego -i input.json "data.infra_validation.compliant"
```

```
{
  "result": [{ "expressions": [{ "value": false, "text": "data.infra_validation.compliant", "location": { "row": 1, "col": 1 } }] }]
}
```

That is hard to read and easy to misinterpret. Always use `--format pretty` during development:

```bash
opa eval --format pretty -d policy.rego -i input.json "data.infra_validation.compliant"
```

```
false
```

Much clearer. The `--format pretty` flag strips away the evaluation metadata and shows only the value.

### Confusing `net.cidr_contains` Argument Order

`net.cidr_contains(outer, inner)` checks if the second argument is contained within the first. A common mistake is reversing the arguments:

```bash
# WRONG: checking if the large range is inside the small one
opa eval --format pretty "net.cidr_contains(\"10.1.2.0/24\", \"10.0.0.0/8\")"
```

```
false
```

```bash
# RIGHT: checking if the small range is inside the large one
opa eval --format pretty "net.cidr_contains(\"10.0.0.0/8\", \"10.1.2.0/24\")"
```

```
true
```

The first argument is always the container, the second is what you are checking.

## Verify What You Learned

```bash
opa eval --format pretty -d policy.rego -i input.json "data.infra_validation.naming_violations"
```

```
[
  "Resource 'Prod_Cache_Server' (type: cache) does not meet naming convention (lowercase, numbers, hyphens only)",
  "Resource 'monitoring-' (type: observability) does not meet naming convention (lowercase, numbers, hyphens only)"
]
```

```bash
opa eval --format pretty -d policy.rego -i input.json "data.infra_validation.network_violations"
```

```
[
  "Resource 'dev-frontend' uses public CIDR: 203.0.113.0/24"
]
```

```bash
opa eval --format pretty -d policy.rego -i input.json "data.infra_validation.resource_counts"
```

```
{
  "cache": 1,
  "compute": 2,
  "database": 1,
  "network": 1,
  "observability": 1
}
```

```bash
opa eval --format pretty -d policy.rego -i input.json "data.infra_validation.compliant"
```

```
false
```

```bash
opa eval --format pretty -d reference.rego "data.reference.b64_decoded"
```

```
"hello world"
```

## What's Next

You now have a solid grasp of Rego's built-in function library and how to combine multiple function categories in a single policy. In the next exercise, you will learn how to define your own reusable functions -- extracting repeated logic into helpers so your policies stay clean and maintainable.

## Reference

- [OPA Built-in Functions -- complete official reference](https://www.openpolicyagent.org/docs/latest/policy-reference/#built-in-functions)
- [Strings built-ins](https://www.openpolicyagent.org/docs/latest/policy-reference/#strings)
- [Regex built-ins](https://www.openpolicyagent.org/docs/latest/policy-reference/#regex)
- [Net built-ins](https://www.openpolicyagent.org/docs/latest/policy-reference/#net)
- [Time built-ins](https://www.openpolicyagent.org/docs/latest/policy-reference/#time)
- [Aggregates built-ins](https://www.openpolicyagent.org/docs/latest/policy-reference/#aggregates)
- [Encoding built-ins](https://www.openpolicyagent.org/docs/latest/policy-reference/#encoding)
- [Types built-ins](https://www.openpolicyagent.org/docs/latest/policy-reference/#types)

## Additional Resources

- [OPA Playground](https://play.openpolicyagent.org/) -- try built-in functions interactively
- [Styra Academy](https://academy.styra.com/) -- free OPA/Rego courses
- [OPA Policy Cheatsheet](https://docs.styra.com/opa/rego-cheat-sheet) -- quick reference for Rego syntax

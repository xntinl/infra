# 12. coalesce() and try() for Fallback Values

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 11 (zipmap() to Create Maps from Lists)

## Learning Objectives

After completing this exercise, you will be able to:

- Use `coalesce()` to select the first non-null, non-empty value from a list of candidates
- Use `try()` to safely navigate object attributes that may not exist, with a fallback value
- Implement multi-level fallback chains that combine user overrides, derived values, and hardcoded defaults

## Why coalesce() and try()

Configuration values often come from multiple sources with different priorities: a user-provided override, a value derived from other configuration, or a hardcoded default. When the higher-priority source is absent (null or empty), the system should fall back gracefully to the next available value instead of failing.

`coalesce()` handles the "first non-null, non-empty" case. It takes multiple arguments and returns the first one that is not null and not an empty string. This makes it ideal for expressing "use the custom domain if provided, otherwise use the default."

`try()` handles the "might not exist" case. It evaluates expressions in order and returns the first one that does not produce an error. This is essential when navigating configuration objects that may or may not have certain keys -- instead of checking for key existence manually, `try()` attempts the access and falls back if it fails.

Together, these functions let you write configuration that is resilient to missing data without defensive `if` blocks or null checks scattered throughout your code.

## Building the Configuration

This configuration demonstrates four fallback patterns: coalesce for strings, coalescelist for lists, try for safe attribute access, and a combined multi-level fallback chain.

Create `main.tf`:

```hcl
variable "custom_domain" {
  type    = string
  default = ""
}

variable "override_name" {
  type    = string
  default = null
}

variable "config" {
  type = any
  default = {
    database = {
      host = "db.internal"
      port = 5432
    }
  }
}

locals {
  # Pattern 1: coalesce() -- first non-null, non-empty value wins
  domain = coalesce(var.custom_domain, "default.example.com")

  # Pattern 2: coalescelist() -- first non-empty list wins
  allowed_cidrs = coalescelist(
    var.custom_domain != "" ? ["${var.custom_domain}/32"] : [],
    ["10.0.0.0/8"]
  )

  # Pattern 3: try() -- safe navigation of optional attributes
  db_host    = try(var.config.database.host, "localhost")
  db_port    = try(var.config.database.port, 5432)
  cache_host = try(var.config.cache.host, "localhost")
  cache_port = try(var.config.cache.port, 6379)

  # Pattern 4: try() with type conversion
  raw_port    = "8080"
  parsed_port = try(tonumber(local.raw_port), 80)

  # Pattern 5: Multi-level fallback chain combining coalesce and try
  service_name = coalesce(
    var.override_name,
    try(var.config.service_name, null),
    "default-service"
  )
}

output "domain"       { value = local.domain }
output "db_host"      { value = local.db_host }
output "cache_host"   { value = local.cache_host }
output "cache_port"   { value = local.cache_port }
output "service_name" { value = local.service_name }
```

## Testing with Default Values

With no overrides provided, all fallbacks activate.

```bash
terraform plan
```

```
Changes to Outputs:
  + cache_host   = "localhost"
  + cache_port   = 6379
  + db_host      = "db.internal"
  + domain       = "default.example.com"
  + service_name = "default-service"
```

The `domain` falls back to `"default.example.com"` because `custom_domain` is empty. The `cache_host` falls back to `"localhost"` because `var.config` has no `cache` key. The `db_host` resolves to `"db.internal"` because the database configuration exists.

## Testing with Overrides

```bash
terraform plan -var='custom_domain=myapp.company.com'
```

```
Changes to Outputs:
  + domain       = "myapp.company.com"
```

```bash
terraform plan -var='override_name=my-custom-service'
```

```
Changes to Outputs:
  + service_name = "my-custom-service"
```

## Exploring in Console

```bash
terraform console
```

```
> coalesce("", "", "fallback")
"fallback"
> coalesce(null, null, "last")
"last"
> coalesce("first", "second")
"first"
> try(tonumber("abc"), 0)
0
> try(tonumber("42"), 0)
42
> try({a = 1}.b, "missing")
"missing"
> exit
```

## coalesce() vs try(): When to Use Each

| Function | Skips on | Use case |
|----------|----------|----------|
| `coalesce()` | null, empty string | Choosing among multiple candidate values |
| `try()` | any error | Accessing attributes that may not exist |
| Combined | both | Multi-source fallback with optional structure navigation |

Use `coalesce()` when all candidates are valid expressions that might be null or empty. Use `try()` when an expression might throw an error (like accessing a missing map key or calling a function that can fail).

## Common Mistakes

### Confusing coalesce() behavior with empty strings vs null

`coalesce()` skips both null and empty strings. If you want to allow empty strings as valid values, use a conditional instead:

```hcl
# coalesce() treats "" as "missing" and skips it
coalesce("", "fallback")  # returns "fallback"

# If empty string is a valid value, use a conditional
var.custom_domain != null ? var.custom_domain : "fallback"
```

### Using try() where coalesce() is sufficient

`try()` is more powerful but less readable for simple null-checking:

```hcl
# Unnecessarily complex -- try() is not needed for null checks
service_name = try(var.override_name, "default")

# Simpler and clearer
service_name = coalesce(var.override_name, "default")
```

Reserve `try()` for expressions that might actually throw errors, not just return null.

## Verify What You Learned

```bash
terraform init
```

```
Terraform has been successfully initialized!
```

```bash
terraform plan
```

```
Changes to Outputs:
  + cache_host   = "localhost"
  + cache_port   = 6379
  + db_host      = "db.internal"
  + domain       = "default.example.com"
  + service_name = "default-service"
```

```bash
terraform console
```

```
> coalesce("", "fallback")
"fallback"
> try(tolist("not-a-list"), ["default"])
[
  "default",
]
> coalesce(null, "winner")
"winner"
> local.cache_host
"localhost"
> exit
```

## Section 02 Summary

You completed 8 exercises covering:

1. **cidrsubnet() for Subnet Calculation** -- Programmatic network partitioning with newbits and netnum parameters
2. **flatten() for Nested Lists** -- Decomposing hierarchical data into flat structures for for_each consumption
3. **templatefile() for User Data** -- Rendering dynamic scripts with variable interpolation and control directives
4. **merge() and lookup() for Map Defaults** -- Layered configuration pattern where environment-specific values override defaults
5. **regex() for Input Validation** -- Pattern matching for format enforcement and structured string decomposition with capture groups
6. **formatlist() and join() for ARN Construction** -- Building lists of formatted strings from dynamic data for IAM policies
7. **zipmap() to Create Maps from Lists** -- Combining parallel lists into key-value maps for for_each and lookup operations
8. **coalesce() and try() for Fallback Values** -- Graceful handling of missing or null values with multi-level fallback chains

Key takeaways:
- Terraform functions compose well: `flatten()` + `for` + `zipmap()` can transform any data structure into a `for_each`-compatible map
- `can()` and `try()` convert errors into safe values, enabling defensive programming without conditional blocks
- The layered configuration pattern (`merge()` + `lookup()`) scales to any number of override layers
- `formatlist()` and `join()` are the standard tools for constructing ARN lists and other formatted string collections

## What's Next

You completed Section 02 (Expressions and Functions). In Section 03 (Dynamic Blocks), you will learn how to generate repeated nested blocks inside resources using `dynamic` blocks, replacing copy-pasted configurations with data-driven iteration.

## Reference

- [coalesce Function](https://developer.hashicorp.com/terraform/language/functions/coalesce)
- [coalescelist Function](https://developer.hashicorp.com/terraform/language/functions/coalescelist)
- [try Function](https://developer.hashicorp.com/terraform/language/functions/try)
- [tonumber Function](https://developer.hashicorp.com/terraform/language/functions/tonumber)

## Additional Resources

- [Perform Dynamic Operations with Functions](https://developer.hashicorp.com/terraform/tutorials/configuration-language/functions) -- Official HashiCorp tutorial covering Terraform functions including try() and type conversion with fallback
- [Terraform Coalesce vs Try](https://devtodevops.com/blog/terraform-coalesce-vs-try/) -- Practical comparison between coalesce() and try() with examples of when to use each for fallback values
- [coalesce Function](https://developer.hashicorp.com/terraform/language/functions/coalesce) -- Official documentation with examples of fallback chains for null and empty string values
- [try Function](https://developer.hashicorp.com/terraform/language/functions/try) -- Official documentation on try() with examples of safe attribute navigation and expression evaluation with fallback

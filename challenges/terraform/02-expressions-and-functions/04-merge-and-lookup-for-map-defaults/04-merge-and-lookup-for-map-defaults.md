# 8. merge() and lookup() for Map Defaults

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 7 (templatefile() for User Data)

## Learning Objectives

After completing this exercise, you will be able to:

- Use `merge()` to layer configuration maps where later values override earlier ones
- Use `lookup()` to safely access map keys with a fallback when the key does not exist
- Implement a multi-layer configuration pattern (defaults, environment overrides, region overrides)

## Why merge() and lookup()

Infrastructure configuration follows a pattern of progressive specialization: most settings have reasonable defaults, but certain environments or regions need specific overrides. Without a layering mechanism, you end up with large conditional blocks or duplicated configuration across environments.

`merge()` solves this by combining multiple maps into one, with later maps taking precedence for duplicate keys. If the defaults map has `instance_type = "t3.micro"` and the prod overrides map has `instance_type = "t3.medium"`, the result is `"t3.medium"`. Keys that are not overridden retain their default values.

`lookup()` complements `merge()` by safely retrieving a value from a map with a fallback. When you look up an environment that has no overrides defined, `lookup()` returns an empty map `{}` instead of crashing. This makes the pattern robust: adding a new environment that uses all defaults requires zero configuration.

## Building the Layered Configuration

This configuration defines three layers: base defaults, environment-specific overrides, and region-specific overrides. The `merge()` function applies them in order.

Create `main.tf`:

```hcl
variable "environment" {
  default = "dev"
}

locals {
  # Layer 1: Base defaults
  defaults = {
    instance_type = "t3.micro"
    min_size      = 1
    max_size      = 2
    monitoring    = false
    backup        = false
  }

  # Layer 2: Environment-specific overrides
  env_overrides = {
    dev = {}
    staging = {
      instance_type = "t3.small"
      min_size      = 2
      max_size      = 4
      monitoring    = true
    }
    prod = {
      instance_type = "t3.medium"
      min_size      = 3
      max_size      = 10
      monitoring    = true
      backup        = true
    }
  }

  # merge() applies overrides on top of defaults
  config = merge(
    local.defaults,
    lookup(local.env_overrides, var.environment, {})
  )

  # Layer 3: Region-specific overrides (applied on top of everything)
  region_overrides = {
    "us-west-2" = { max_size = 20 }
  }

  # Three layers merged in order: defaults < env < region
  full_config = merge(
    local.defaults,
    lookup(local.env_overrides, var.environment, {}),
    lookup(local.region_overrides, "us-east-1", {})
  )
}

output "resolved_config" { value = local.config }
output "full_config"     { value = local.full_config }
```

## Testing with Different Environments

**dev environment (all defaults):**

```bash
terraform console
```

```
> local.config
{
  "backup"        = false
  "instance_type" = "t3.micro"
  "max_size"      = 2
  "min_size"      = 1
  "monitoring"    = false
}
> exit
```

**prod environment (overrides applied):**

```bash
terraform plan -var='environment=prod'
```

```
Changes to Outputs:
  + resolved_config = {
      + backup        = true
      + instance_type = "t3.medium"
      + max_size      = 10
      + min_size      = 3
      + monitoring    = true
    }
```

**unknown environment (lookup falls back to empty map, so defaults apply):**

```bash
terraform plan -var='environment=unknown'
```

```
Changes to Outputs:
  + resolved_config = {
      + backup        = false
      + instance_type = "t3.micro"
      + max_size      = 2
      + min_size      = 1
      + monitoring    = false
    }
```

## Understanding merge() Precedence

`merge()` processes maps left to right. When the same key appears in multiple maps, the rightmost value wins.

```bash
terraform console
```

```
> merge({a = 1, b = 2}, {b = 3, c = 4})
{
  "a" = 1
  "b" = 3
  "c" = 4
}
> lookup({a = 1, b = 2}, "c", "default")
"default"
> lookup({a = 1, b = 2}, "a", "default")
1
> exit
```

## Common Mistakes

### Reversing the merge() order

If you place the overrides before the defaults, the defaults will overwrite the overrides:

```hcl
# Wrong: defaults overwrite prod values
config = merge(
  lookup(local.env_overrides, var.environment, {}),
  local.defaults
)
```

The result will always be the defaults, regardless of the environment. Always put defaults first and overrides last.

### Using direct map access instead of lookup() for optional keys

Direct access crashes when the key does not exist:

```hcl
# Wrong: crashes if environment is not in the map
config = merge(local.defaults, local.env_overrides[var.environment])
```

```
Error: Invalid index: the given key does not identify an element in this collection.
```

Use `lookup(local.env_overrides, var.environment, {})` to return an empty map for missing keys.

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
  + full_config     = {
      + backup        = false
      + instance_type = "t3.micro"
      + max_size      = 2
      + min_size      = 1
      + monitoring    = false
    }
  + resolved_config = {
      + backup        = false
      + instance_type = "t3.micro"
      + max_size      = 2
      + min_size      = 1
      + monitoring    = false
    }
```

```bash
terraform console
```

```
> merge({a = 1}, {a = 2}, {a = 3})
{
  "a" = 3
}
> lookup({x = "hello"}, "y", "fallback")
"fallback"
> exit
```

## What's Next

You implemented a layered configuration pattern using `merge()` and `lookup()`. In the next exercise, you will learn how to use `regex()` for pattern-based input validation and ARN parsing with capture groups.

## Reference

- [merge Function](https://developer.hashicorp.com/terraform/language/functions/merge)
- [lookup Function](https://developer.hashicorp.com/terraform/language/functions/lookup)
- [Local Values](https://developer.hashicorp.com/terraform/language/values/locals)

## Additional Resources

- [Perform Dynamic Operations with Functions](https://developer.hashicorp.com/terraform/tutorials/configuration-language/functions) -- Official HashiCorp tutorial on Terraform functions including merge() and lookup() with practical examples
- [Terraform Merge Function](https://spacelift.io/blog/terraform-merge-function) -- Detailed guide on merge() with layered configuration patterns and map combination
- [Terraform Map Variable Guide](https://www.env0.com/blog/terraform-map-variable-a-complete-guide-with-practical-examples) -- Practical guide on map variables including lookup patterns with default values

# 20. for_each with toset

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 19 (Resources from YAML with yamldecode)

## Learning Objectives

After completing this exercise, you will be able to:

- Convert lists to sets with `toset()` for use with `for_each`
- Derive new sets from transformations on existing collections
- Understand how `toset()` deduplication prevents duplicate resource creation
- Use `split()` to extract components from structured strings

## Why `toset()` Matters

You saw `toset()` briefly in exercise 16, but there is more to it than simple list conversion. In practice, you often need to create resources from values that are derived from other data -- prefixes extracted from paths, regions parsed from ARNs, or categories computed from naming conventions.

When you transform a collection with a `for` expression, the result may contain duplicates. If four log group paths all start with `/app/`, extracting the first path segment yields `["app", "app", "app", "app"]`. Passing this directly to `for_each` would fail (lists are not allowed) and even if it worked, you would create four identical resources.

`toset()` solves both problems: it converts the list to a set (making it compatible with `for_each`) and removes duplicates (ensuring only one resource per unique value). This exercise explores this derived-set pattern in depth.

## Step 1 -- Define the Input Variables

Create `variables.tf`:

```hcl
variable "availability_zones" {
  description = "List of availability zones to use"
  type        = list(string)
  default     = ["us-east-1a", "us-east-1b", "us-east-1c"]
}

variable "log_groups" {
  description = "List of CloudWatch log group paths"
  type        = list(string)
  default     = ["/app/api", "/app/worker", "/app/scheduler", "/app/cron"]
}
```

## Step 2 -- Create Log Groups from a List

Create `main.tf`:

```hcl
resource "aws_cloudwatch_log_group" "this" {
  for_each          = toset(var.log_groups)
  name              = each.key
  retention_in_days = 30
}
```

Each log group path becomes a key in the resource map. Since the paths are already unique, `toset()` simply converts the list to a set without removing anything.

## Step 3 -- Derive Unique Prefixes

Now extract the first path segment from each log group and deduplicate:

```hcl
locals {
  # Extract the second segment (index 1) from paths like "/app/api"
  # split("/", "/app/api") => ["", "app", "api"]
  # Element at index 1 is "app"
  log_prefixes = toset([for lg in var.log_groups : split("/", lg)[1]])
}
```

All four log groups share the prefix `app`, so `log_prefixes` contains just `toset(["app"])` -- a single element.

## Step 4 -- Create Resources from Derived Prefixes

```hcl
resource "aws_ssm_parameter" "prefix_marker" {
  for_each = local.log_prefixes
  name     = "/monitoring/${each.key}/enabled"
  type     = "String"
  value    = "true"
}
```

Only one SSM parameter is created (`/monitoring/app/enabled`) because the set contains only one unique prefix.

## Step 5 -- Add Outputs

```hcl
output "log_group_names" {
  description = "List of created log group names"
  value       = [for k, v in aws_cloudwatch_log_group.this : v.name]
}

output "unique_prefixes" {
  description = "Deduplicated set of log group prefixes"
  value       = local.log_prefixes
}
```

## Step 6 -- Test Deduplication

Add a log group with a different prefix to `variables.tf`:

```hcl
default = ["/app/api", "/app/worker", "/app/scheduler", "/app/cron", "/system/health"]
```

Run `terraform plan`. You should see:

- 5 log groups (one new: `/system/health`)
- 2 SSM parameters (one new: `/monitoring/system/enabled`)

The prefix `app` still produces only one parameter; the new prefix `system` adds exactly one more.

## Step 7 -- Test with Duplicates in the Input

Add a duplicate log group:

```hcl
default = ["/app/api", "/app/worker", "/app/api", "/app/cron"]
```

Run `terraform plan`. Despite `/app/api` appearing twice in the list, `toset()` deduplicates it. Only 3 log groups are created, not 4.

## Step 8 -- Explore in the Console

Open the Terraform console and experiment:

```bash
terraform console
```

```hcl
> toset(["a", "b", "a"])
toset([
  "a",
  "b",
])

> toset([for lg in ["/app/api", "/app/worker", "/system/health"] : split("/", lg)[1]])
toset([
  "app",
  "system",
])
```

## Common Mistakes

### Assuming list order is preserved

```hcl
# Wrong mental model -- sets have no order
resource "aws_cloudwatch_log_group" "this" {
  for_each = toset(var.log_groups)
  # Do not rely on "first" or "last" element -- sets are unordered
}
```

Sets have no guaranteed order. If you need ordered processing, use a map with explicit keys or `count` with indices.

### Using `split()` on unexpected input formats

```hcl
# If a log group path does not start with "/", split produces unexpected results
# split("/", "app/api") => ["app", "api"]  -- index 1 is "api", not "app"
# split("/", "/app/api") => ["", "app", "api"]  -- index 1 is "app"
```

Always verify the structure of your input strings. A leading `/` changes the indices because `split()` produces an empty string as the first element.

## Verify What You Learned

With the original four log groups:

```bash
terraform plan
```

```
Terraform will perform the following actions:

  # aws_cloudwatch_log_group.this["/app/api"] will be created
  + resource "aws_cloudwatch_log_group" "this" {
      + name              = "/app/api"
      + retention_in_days = 30
    }

  # aws_cloudwatch_log_group.this["/app/cron"] will be created
  + resource "aws_cloudwatch_log_group" "this" {
      + name              = "/app/cron"
      + retention_in_days = 30
    }

  # aws_cloudwatch_log_group.this["/app/scheduler"] will be created
  + resource "aws_cloudwatch_log_group" "this" {
      + name              = "/app/scheduler"
      + retention_in_days = 30
    }

  # aws_cloudwatch_log_group.this["/app/worker"] will be created
  + resource "aws_cloudwatch_log_group" "this" {
      + name              = "/app/worker"
      + retention_in_days = 30
    }

  # aws_ssm_parameter.prefix_marker["app"] will be created
  + resource "aws_ssm_parameter" "prefix_marker" {
      + name  = "/monitoring/app/enabled"
      + type  = "String"
      + value = "true"
    }

Plan: 5 to add, 0 to change, 0 to destroy.
```

Verify counts:

```bash
terraform plan | grep "aws_cloudwatch_log_group" | wc -l
```

```
4
```

```bash
terraform plan | grep "aws_ssm_parameter" | wc -l
```

```
1
```

Test deduplication in the console:

```bash
echo 'toset(["a", "b", "a"])' | terraform console
```

```
toset([
  "a",
  "b",
])
```

## Section 04 Summary

You completed 5 exercises covering:

1. **Multiple S3 Buckets from a List** -- used `for_each` with `toset()` to create homogeneous resources from a flat list, with stable string keys instead of numeric indices
2. **IAM Users with Different Policies** -- used `for_each` on a map of objects for per-instance configuration, and `flatten()` to handle one-to-many relationships between users and policies
3. **Conditional Resource Creation with count** -- applied the `count = condition ? 1 : 0` idiom for optional resources, with `one()` and ternary expressions for safe output access
4. **Resources from YAML with yamldecode** -- externalized configuration to YAML files, separating data from infrastructure logic, and filtered collections with `for` + `if`
5. **for_each with toset** -- derived sets from transformations on collections, leveraging automatic deduplication to prevent duplicate resource creation

Key takeaways:

- Use `for_each` with stable string keys for collections of resources; reserve `count` for 0-or-1 conditional creation
- `toset()` converts lists to sets, removing duplicates and making them compatible with `for_each`
- `flatten()` is essential for cross-product patterns where one resource has multiple related sub-resources
- YAML-driven infrastructure separates data from logic, enabling changes without touching HCL code
- Always guard access to conditional resources with `one()`, splat expressions, or ternary checks

## What's Next

In the next section you will learn about Terraform state operations -- how to inspect, move, import, and manipulate resources in the state file when refactoring or recovering from drift.

## Reference

- [toset Function](https://developer.hashicorp.com/terraform/language/functions/toset)
- [The for_each Meta-Argument](https://developer.hashicorp.com/terraform/language/meta-arguments/for_each)
- [split Function](https://developer.hashicorp.com/terraform/language/functions/split)

## Additional Resources

- [HashiCorp: Manage Similar Resources with for_each](https://developer.hashicorp.com/terraform/tutorials/configuration-language/for-each) -- official tutorial covering for_each with sets and maps, including the toset() conversion
- [Spacelift: Terraform for_each Guide](https://spacelift.io/blog/terraform-for-each) -- comprehensive guide with examples of for_each over sets, including deduplication and derived sets
- [HashiCorp: toset Function](https://developer.hashicorp.com/terraform/language/functions/toset) -- official documentation on toset() with examples of list-to-set conversion and duplicate removal

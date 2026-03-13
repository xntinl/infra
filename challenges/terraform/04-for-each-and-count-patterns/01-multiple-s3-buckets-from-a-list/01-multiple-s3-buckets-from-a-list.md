# 16. Multiple S3 Buckets from a List

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 15 (Dynamic Tags from a Map)

## Learning Objectives

After completing this exercise, you will be able to:

- Use `for_each` with `toset()` to create multiple identical resources from a list
- Generate unique resource names with the `random_id` resource
- Build output maps from resource collections using `for` expressions

## Why `for_each` with `toset()`

When you need multiple resources that share the same configuration but differ only by name or purpose, copying and pasting resource blocks is fragile and unscalable. The `for_each` meta-argument creates one resource instance per element in a map or set, each tracked by a stable key in the state.

The catch: `for_each` does not accept lists directly. Lists are ordered and can contain duplicates, which conflicts with the requirement for unique, stable keys. The `toset()` function solves this by converting a list into a set -- removing duplicates and discarding order. Each element in the set becomes both the key (`each.key`) and the value (`each.value`).

This exercise demonstrates the simplest `for_each` pattern: homogeneous resources from a flat list of strings.

## Step 1 -- Define the Variables

Create `variables.tf`:

```hcl
variable "bucket_purposes" {
  description = "List of bucket purposes to create"
  type        = list(string)
  default     = ["logs", "artifacts", "backups", "data"]
}

variable "project" {
  description = "Project name used as a bucket name prefix"
  default     = "exercise"
}
```

## Step 2 -- Create the Buckets and Random Suffix

Create `main.tf`:

```hcl
resource "random_id" "suffix" {
  byte_length = 4
}

resource "aws_s3_bucket" "this" {
  for_each = toset(var.bucket_purposes)
  bucket   = "${var.project}-${each.key}-${random_id.suffix.hex}"
}

output "bucket_names" {
  description = "Map of purpose to bucket name"
  value       = { for k, v in aws_s3_bucket.this : k => v.id }
}

output "bucket_arns" {
  description = "Map of purpose to bucket ARN"
  value       = { for k, v in aws_s3_bucket.this : k => v.arn }
}
```

The `random_id` resource generates a hex suffix (e.g., `a1b2c3d4`) to ensure global uniqueness of S3 bucket names. It is created once, not per bucket.

Run `terraform validate` to confirm:

```bash
terraform validate
```

```
Success! The configuration is valid.
```

## Step 3 -- Plan and Inspect

Run `terraform plan`:

```bash
terraform plan
```

```
Terraform will perform the following actions:

  # random_id.suffix will be created
  + resource "random_id" "suffix" {
      + byte_length = 4
      + hex         = (known after apply)
    }

  # aws_s3_bucket.this["artifacts"] will be created
  + resource "aws_s3_bucket" "this" {
      + bucket = (known after apply)
    }

  # aws_s3_bucket.this["backups"] will be created
  + resource "aws_s3_bucket" "this" {
      + bucket = (known after apply)
    }

  # aws_s3_bucket.this["data"] will be created
  + resource "aws_s3_bucket" "this" {
      + bucket = (known after apply)
    }

  # aws_s3_bucket.this["logs"] will be created
  + resource "aws_s3_bucket" "this" {
      + bucket = (known after apply)
    }

Plan: 5 to add, 0 to change, 0 to destroy.
```

Notice the resource addresses: `aws_s3_bucket.this["logs"]`, `aws_s3_bucket.this["artifacts"]`, etc. Each bucket is keyed by its purpose, not by a numeric index.

## Step 4 -- Add a New Bucket

Add `"configs"` to the `bucket_purposes` list and run `terraform plan`. Only one new bucket should appear:

```bash
terraform plan
```

```
  # aws_s3_bucket.this["configs"] will be created

Plan: 1 to add, 0 to change, 0 to destroy.
```

The existing four buckets are unchanged because their keys are stable.

## Step 5 -- Remove a Bucket

Remove `"data"` from the list and run `terraform plan`:

```bash
terraform plan
```

```
  # aws_s3_bucket.this["data"] will be destroyed

Plan: 0 to add, 0 to change, 1 to destroy.
```

Only the `data` bucket is marked for destruction. Compare this to `count`, where removing an element from the middle of a list would shift indices and cause unnecessary recreations.

## Common Mistakes

### Passing a list directly to `for_each`

```hcl
# Wrong -- for_each does not accept lists
resource "aws_s3_bucket" "this" {
  for_each = var.bucket_purposes  # Error!
}
```

Terraform requires a map or set. Wrap the list with `toset()`:

```hcl
for_each = toset(var.bucket_purposes)
```

### Duplicate values in the list

```hcl
default = ["logs", "data", "logs"]  # "logs" appears twice
```

`toset()` silently removes duplicates, so you get two buckets instead of three. If this is unintentional, ensure your input list has unique values, or use a map instead of a list to make keys explicit.

## Verify What You Learned

Check that four buckets and one random_id are planned:

```bash
terraform plan | grep "will be created" | wc -l
```

```
5
```

Verify resource addresses use string keys:

```bash
terraform plan | grep 'aws_s3_bucket.this\["logs"\]'
```

```
  # aws_s3_bucket.this["logs"] will be created
```

Confirm outputs are maps:

```bash
terraform plan | grep "bucket_names"
```

```
  + bucket_names = (known after apply)
```

## What's Next

In the next exercise you will use `for_each` with a map of objects instead of a flat set. This lets you give each resource instance its own configuration -- different paths, policies, and settings per IAM user.

## Reference

- [The for_each Meta-Argument](https://developer.hashicorp.com/terraform/language/meta-arguments/for_each)
- [toset Function](https://developer.hashicorp.com/terraform/language/functions/toset)
- [for Expressions](https://developer.hashicorp.com/terraform/language/expressions/for)

## Additional Resources

- [How to Create Multiple S3 Buckets Using Terraform](https://cloudkatha.com/how-to-create-multiple-s3-buckets-using-terraform/) -- practical guide showing for_each and toset() for S3 bucket creation
- [Spacelift: Terraform for_each Guide](https://spacelift.io/blog/terraform-for-each) -- comprehensive walkthrough with examples of for_each over lists, maps, and sets
- [HashiCorp: Manage Similar Resources with for_each](https://developer.hashicorp.com/terraform/tutorials/configuration-language/for-each) -- official tutorial covering the difference between count and for_each

# 19. Resources from YAML with yamldecode

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 18 (Conditional Resource Creation with count)

## Learning Objectives

After completing this exercise, you will be able to:

- Externalize resource configuration to YAML files and load them with `yamldecode()` and `file()`
- Filter collections using `for` expressions with an `if` clause
- Understand the YAML-driven infrastructure pattern and when it is appropriate

## Why YAML-Driven Infrastructure

As infrastructure grows, the Terraform code and the data it operates on become entangled. Adding a new S3 bucket means editing HCL variables, understanding Terraform syntax, and risking accidental changes to logic. This creates a bottleneck: only Terraform-literate engineers can make changes.

The YAML-driven pattern separates data from logic. Resource configurations live in plain YAML files that anyone can edit -- no HCL knowledge required. Terraform reads these files with `yamldecode(file(...))` and uses `for_each` to create the corresponding resources. Adding a bucket means adding four lines of YAML; no Terraform code changes at all.

This pattern works best when resources are homogeneous (same type, similar structure) and the set of configurable properties is well-defined. It is the same approach used in this project's own `config/functions.yaml` and `config/routes.yaml`.

## Step 1 -- Create the YAML Configuration File

Create `config/buckets.yaml`:

```yaml
buckets:
  app-logs:
    versioning: true
    lifecycle_days: 90
    public: false
  static-assets:
    versioning: false
    lifecycle_days: 365
    public: true
  data-archive:
    versioning: true
    lifecycle_days: 2555
    public: false
```

Each key under `buckets` is a bucket name. The properties define how Terraform should configure each bucket.

## Step 2 -- Load YAML and Create Resources

Create `main.tf`:

```hcl
locals {
  config  = yamldecode(file("${path.module}/config/buckets.yaml"))
  buckets = local.config.buckets
}

resource "aws_s3_bucket" "this" {
  for_each = local.buckets
  bucket   = "exercise-${each.key}"
}
```

The `file()` function reads the YAML file as a string, and `yamldecode()` parses it into a Terraform map. The `${path.module}` prefix ensures the path is relative to the module, not the working directory.

## Step 3 -- Filter with `for` + `if` for Conditional Sub-Resources

Not every bucket needs versioning. Use a `for` expression with an `if` clause to create versioning only where it is enabled:

```hcl
resource "aws_s3_bucket_versioning" "this" {
  for_each = { for k, v in local.buckets : k => v if v.versioning }
  bucket   = aws_s3_bucket.this[each.key].id
  versioning_configuration {
    status = "Enabled"
  }
}
```

The expression `{ for k, v in local.buckets : k => v if v.versioning }` builds a new map containing only entries where `versioning` is `true`. This gives you `app-logs` and `data-archive` but not `static-assets`.

## Step 4 -- Add Outputs

```hcl
output "bucket_configs" {
  description = "All bucket configurations from YAML"
  value       = local.buckets
}

output "versioned_buckets" {
  description = "Names of buckets with versioning enabled"
  value       = [for k, v in local.buckets : k if v.versioning]
}
```

## Step 5 -- Add a New Bucket via YAML Only

Edit `config/buckets.yaml` and add a new entry:

```yaml
  reports:
    versioning: false
    lifecycle_days: 180
    public: false
```

Run `terraform plan`. A new bucket should appear with no changes to any `.tf` file -- the YAML file is the only thing that changed.

## Step 6 -- Failure Scenario: Missing YAML Field

Try adding a bucket without the `versioning` field:

```yaml
  broken:
    lifecycle_days: 30
    public: false
```

Run `terraform plan`. Terraform will fail when the `for` expression tries to access `v.versioning` on an entry where it does not exist. This highlights the importance of consistent YAML schemas. You can mitigate this with `lookup()` or by defining a variable type that validates the YAML structure after loading.

## Common Mistakes

### Hardcoding the file path without `path.module`

```hcl
# Wrong -- breaks when called as a module from a different directory
config = yamldecode(file("config/buckets.yaml"))
```

Always use `${path.module}/config/buckets.yaml` to make the path relative to the module root.

### Forgetting that YAML keys are case-sensitive

```yaml
buckets:
  App-Logs:     # This is a different key than "app-logs"
    Versioning: true
```

YAML keys are case-sensitive. If your Terraform code references `v.versioning` (lowercase), but the YAML uses `Versioning` (capitalized), the attribute will not be found. Keep YAML keys lowercase and consistent.

## Verify What You Learned

```bash
terraform plan
```

```
Terraform will perform the following actions:

  # aws_s3_bucket.this["app-logs"] will be created
  + resource "aws_s3_bucket" "this" {
      + bucket = "exercise-app-logs"
    }

  # aws_s3_bucket.this["data-archive"] will be created
  + resource "aws_s3_bucket" "this" {
      + bucket = "exercise-data-archive"
    }

  # aws_s3_bucket.this["static-assets"] will be created
  + resource "aws_s3_bucket" "this" {
      + bucket = "exercise-static-assets"
    }

  # aws_s3_bucket_versioning.this["app-logs"] will be created
  + resource "aws_s3_bucket_versioning" "this" {
      + versioning_configuration {
          + status = "Enabled"
        }
    }

  # aws_s3_bucket_versioning.this["data-archive"] will be created
  + resource "aws_s3_bucket_versioning" "this" {
      + versioning_configuration {
          + status = "Enabled"
        }
    }

Plan: 5 to add, 0 to change, 0 to destroy.
```

Check bucket count:

```bash
terraform plan | grep 'aws_s3_bucket.this\[' | wc -l
```

```
3
```

Check versioning count:

```bash
terraform plan | grep 'aws_s3_bucket_versioning' | wc -l
```

```
2
```

Verify the versioned buckets output:

```bash
terraform apply -auto-approve && terraform output versioned_buckets
```

```
[
  "app-logs",
  "data-archive",
]
```

## What's Next

In the next exercise you will revisit `toset()` in more depth, exploring how to derive sets from transformations on other collections and how deduplication works in practice.

## Reference

- [yamldecode Function](https://developer.hashicorp.com/terraform/language/functions/yamldecode)
- [file Function](https://developer.hashicorp.com/terraform/language/functions/file)
- [for Expressions](https://developer.hashicorp.com/terraform/language/expressions/for)

## Additional Resources

- [Spacelift: Terraform YAML Guide](https://spacelift.io/blog/terraform-yaml) -- practical guide to using YAML files as configuration sources in Terraform with yamldecode()
- [Xebia: Terraform with YAML (Part 1)](https://xebia.com/blog/terraform-with-yaml-part-1/) -- detailed walkthrough of the YAML-driven infrastructure pattern with real-world examples
- [HashiCorp: for Expressions with Filtering](https://developer.hashicorp.com/terraform/language/expressions/for) -- official documentation on for expressions with the if clause for filtering collections

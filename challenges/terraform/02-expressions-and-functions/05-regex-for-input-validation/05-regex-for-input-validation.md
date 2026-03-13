# 9. regex() for Input Validation

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 8 (merge() and lookup() for Map Defaults)

## Learning Objectives

After completing this exercise, you will be able to:

- Use `regex()` with `can()` inside validation blocks to enforce input format constraints
- Implement capture groups in `regex()` to extract components from structured strings like ARNs
- Distinguish between `regex()` for extraction and `can(regex())` for validation

## Why regex() for Input Validation

Infrastructure resource names follow strict conventions imposed by cloud providers: S3 bucket names must be lowercase alphanumeric with hyphens, domain names must follow DNS rules, and email addresses must match a specific format. Type constraints alone (`type = string`) cannot enforce these patterns -- they only guarantee the value is a string, not that it is a valid one.

`regex()` applies a regular expression pattern to a string and returns the match or capture groups. When the pattern does not match, `regex()` throws an error. Wrapping it in `can()` converts this error into a `false` result, making it safe for use in validation conditions.

Beyond validation, `regex()` with capture groups can decompose structured strings. An AWS ARN like `arn:aws:s3:::my-bucket/path/to/key` contains the service name, resource identifier, and path as distinct components. A single `regex()` call with groups extracts all three.

## Validation with Pattern Matching

Each variable uses a regular expression tailored to the format rules of its target resource. The `can()` wrapper ensures a non-matching value triggers the custom error message instead of a crash.

Create `main.tf`:

```hcl
variable "domain_name" {
  type = string
  validation {
    condition     = can(regex("^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*\\.[a-z]{2,}$", var.domain_name))
    error_message = "Must be a valid domain name (e.g., app.example.com)."
  }
}

variable "s3_bucket_name" {
  type = string
  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$", var.s3_bucket_name))
    error_message = "S3 bucket name must be 3-63 chars, lowercase letters, numbers, hyphens, and periods."
  }
}

variable "email" {
  type = string
  validation {
    condition     = can(regex("^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$", var.email))
    error_message = "Must be a valid email address."
  }
}

locals {
  sample_arn   = "arn:aws:s3:::my-bucket/path/to/key"
  arn_parts    = regex("^arn:aws:([^:]+):::(.+)/(.+)$", local.sample_arn)
  arn_service  = local.arn_parts[0]
  arn_resource = local.arn_parts[1]
  arn_path     = local.arn_parts[2]
}

output "arn_parts" {
  value = {
    service  = local.arn_service
    resource = local.arn_resource
    path     = local.arn_path
  }
}
```

Create `terraform.tfvars`:

```hcl
domain_name    = "app.example.com"
s3_bucket_name = "my-app-logs-bucket"
email          = "admin@example.com"
```

## Testing Valid Inputs

```bash
terraform plan
```

```
Changes to Outputs:
  + arn_parts = {
      + path     = "path/to/key"
      + resource = "my-bucket"
      + service  = "s3"
    }
```

## Testing Invalid Inputs

**Invalid domain name:**

```bash
terraform plan -var='domain_name=INVALID'
```

```
Error: Invalid value for variable

  on main.tf line 1:
   1: variable "domain_name" {

Must be a valid domain name (e.g., app.example.com).
```

**Invalid S3 bucket name:**

```bash
terraform plan -var='s3_bucket_name=My Bucket!'
```

```
Error: Invalid value for variable

  on main.tf line 9:
   9: variable "s3_bucket_name" {

S3 bucket name must be 3-63 chars, lowercase letters, numbers, hyphens, and
periods.
```

## Understanding Capture Groups

When `regex()` contains parenthesized groups, it returns a list of captured values instead of the full match. This is how you extract components from a structured string.

```bash
terraform console
```

```
> regex("^([a-z]+)-([0-9]+)$", "app-42")
[
  "app",
  "42",
]
> regex("^([a-z]+)-([0-9]+)$", "app-42")[0]
"app"
> can(regex("^[0-9]+$", "abc"))
false
> can(regex("^[0-9]+$", "123"))
true
> exit
```

## Common Mistakes

### Using regex() directly in a condition without can()

`regex()` throws an error on non-matching input, which crashes the validation instead of showing your error message:

```hcl
# Wrong: regex() will throw an error, not return false
validation {
  condition     = regex("^[a-z]+$", var.name) != ""
  error_message = "Must be lowercase."
}
```

Always wrap with `can()`: `condition = can(regex("^[a-z]+$", var.name))`.

### Over-engineering regex patterns

Complex patterns are hard to read and maintain. If a simpler validation strategy exists, prefer it:

```hcl
# Overly complex regex for environment validation
condition = can(regex("^(dev|staging|prod)$", var.environment))

# Simpler alternative using contains()
condition = contains(["dev", "staging", "prod"], var.environment)
```

Use `regex()` when you need pattern matching (format rules). Use `contains()` when you need membership testing (allowed values).

## Verify What You Learned

```bash
terraform plan
```

```
Changes to Outputs:
  + arn_parts = {
      + path     = "path/to/key"
      + resource = "my-bucket"
      + service  = "s3"
    }
```

```bash
terraform console
```

```
> regex("^arn:aws:([^:]+):::(.+)/(.+)$", "arn:aws:s3:::my-bucket/path/to/key")
[
  "s3",
  "my-bucket",
  "path/to/key",
]
> can(regex("^[a-z0-9.-]+$", "valid-name.123"))
true
> can(regex("^[a-z0-9.-]+$", "INVALID NAME!"))
false
> exit
```

## What's Next

You used `regex()` with `can()` for input validation and capture groups for string decomposition. In the next exercise, you will learn how to use `formatlist()` and `join()` to construct lists of ARNs and other formatted strings from dynamic data.

## Reference

- [regex Function](https://developer.hashicorp.com/terraform/language/functions/regex)
- [can Function](https://developer.hashicorp.com/terraform/language/functions/can)
- [Input Variables - Custom Validation](https://developer.hashicorp.com/terraform/language/values/variables#custom-validation-rules)

## Additional Resources

- [Terraform Regex](https://spacelift.io/blog/terraform-regex) -- Comprehensive guide on regex() in Terraform with input validation examples, capture groups, and common patterns
- [Perform Dynamic Operations with Functions](https://developer.hashicorp.com/terraform/tutorials/configuration-language/functions) -- Official tutorial covering Terraform functions including regex() and can() for string manipulation
- [Terraform Variable Validation](https://spacelift.io/blog/terraform-variable-validation) -- Practical guide with regex()-based validation examples for emails, domains, and resource names

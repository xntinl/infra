# 4. Sensitive Variables

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 3 (Variable Validation with Custom Error Messages)

## Learning Objectives

After completing this exercise, you will be able to:

- Use `sensitive = true` to redact variable values from Terraform plan and apply output
- Distinguish between UI-level redaction and actual encryption of sensitive data
- Debug errors caused by exposing sensitive values in outputs without the `sensitive` flag

## Why Sensitive Variables

Terraform plans and outputs are often displayed in CI/CD logs, terminal sessions, and shared dashboards. When you store a database password or API key in a Terraform variable, its value appears in plaintext in `terraform plan` output and in `terraform output` commands. Anyone with access to those logs can read the secret.

The `sensitive = true` flag tells Terraform to replace the actual value with `(sensitive value)` in all CLI output. This is a UI-level protection: it prevents accidental exposure in logs and terminal sessions. However, it is not encryption -- the value still exists in plaintext in the state file and in `.tfvars` files. Understanding this distinction is critical for building a proper secrets management strategy.

## Defining a Sensitive Variable

The `sensitive = true` attribute goes on the variable definition. Terraform will redact this value everywhere it appears in plan and apply output.

Create `main.tf`:

```hcl
variable "db_password" {
  type      = string
  sensitive = true
}

variable "db_username" {
  type    = string
  default = "admin"
}

resource "aws_ssm_parameter" "db_password" {
  name  = "/myapp/db-password"
  type  = "SecureString"
  value = var.db_password
}

output "password_arn" {
  value = aws_ssm_parameter.db_password.arn
}

output "password_value" {
  value     = var.db_password
  sensitive = true
}

# Uncomment to see the error when exposing a sensitive value:
# output "leaked_password" {
#   value = var.db_password
# }
```

Create `terraform.tfvars`:

```hcl
db_password = "sup3r-s3cret-passw0rd"
```

## Observing Redaction in Plan Output

When you run `terraform plan`, the SSM parameter value is redacted:

```bash
terraform plan
```

```
Terraform will perform the following actions:

  # aws_ssm_parameter.db_password will be created
  + resource "aws_ssm_parameter" "db_password" {
      + name  = "/myapp/db-password"
      + type  = "SecureString"
      + value = (sensitive value)
    }

Plan: 1 to add, 0 to change, 0 to destroy.
```

## Observing Redaction in Outputs

The `password_value` output requires `sensitive = true` because it references a sensitive variable. Without it, Terraform refuses to plan.

```bash
terraform output password_value
```

```
(sensitive value)
```

## Triggering the Exposure Error

Uncomment the `leaked_password` output in `main.tf` (the block without `sensitive = true`) and run:

```bash
terraform plan
```

```
Error: Output refers to sensitive values

  on main.tf line 23:
  23: output "leaked_password" {

To reduce the risk of accidentally exporting sensitive data, Terraform
requires that you mark sensitive output values.
```

This error is intentional. Terraform forces you to explicitly acknowledge that an output contains sensitive data.

## Understanding the Limits of Sensitive Redaction

The `sensitive` flag protects CLI output only. The value is still accessible in two places:

**1. JSON output bypasses redaction:**

```bash
terraform output -json password_value
```

```
"sup3r-s3cret-passw0rd"
```

**2. The .tfvars file stores the value in plaintext:**

```bash
cat terraform.tfvars
```

```
db_password = "sup3r-s3cret-passw0rd"
```

For production secrets, use a secrets manager (AWS Secrets Manager, HashiCorp Vault) and reference secrets via data sources instead of storing them in `.tfvars` files.

## Common Mistakes

### Assuming sensitive means encrypted

Marking a variable as `sensitive = true` does not encrypt the value. It only hides it from CLI output. The state file contains the value in plaintext (unless you use an encrypted backend). Always encrypt your state file at rest and restrict access to it.

### Forgetting sensitive = true on outputs that reference sensitive variables

Terraform enforces this at plan time. If any output references a sensitive value (directly or through a resource attribute derived from a sensitive variable), you must add `sensitive = true` to that output block. There is no way to bypass this check.

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
Plan: 1 to add, 0 to change, 0 to destroy.
```

```bash
terraform console
```

```
> var.db_password
(sensitive value)
> var.db_username
"admin"
> exit
```

## Section 01 Summary

You completed 4 exercises covering:

1. **Map of Objects with for_each** -- Modeled multi-environment configuration with typed maps and iterated with key-based for_each
2. **Optional Attributes with Defaults** -- Simplified module interfaces by making object attributes optional with sensible defaults
3. **Variable Validation with Custom Error Messages** -- Caught invalid inputs at plan time using validation blocks with can(), regex(), and contains()
4. **Sensitive Variables** -- Redacted confidential values from CLI output and understood the boundary between UI redaction and actual encryption

Key takeaways:
- `map(object({...}))` with `for_each` is the standard pattern for managing multiple similar resources with stable identifiers
- `optional(type, default)` reduces configuration boilerplate by letting callers specify only what differs from the baseline
- Validation blocks with descriptive error messages shift error detection to the earliest possible moment
- `sensitive = true` is a UI protection, not an encryption mechanism -- always encrypt state files and avoid storing secrets in `.tfvars`

## What's Next

You completed Section 01 (Variables and Types). In Section 02 (Expressions and Functions), you will start with `cidrsubnet()` for programmatic subnet calculation, then progress through `flatten()`, `templatefile()`, `merge()`, `regex()`, and more advanced Terraform functions.

## Reference

- [Input Variables - Suppressing Values](https://developer.hashicorp.com/terraform/language/values/variables#suppressing-values-in-cli-output)
- [Output Values - Sensitive](https://developer.hashicorp.com/terraform/language/values/outputs#sensitive-suppressing-values-in-cli-output)

## Additional Resources

- [Protect Sensitive Input Variables](https://developer.hashicorp.com/terraform/tutorials/configuration-language/sensitive-variables) -- Official HashiCorp tutorial on marking variables as sensitive and understanding redaction in plan/apply
- [How to Protect Sensitive Data in Terraform](https://www.digitalocean.com/community/tutorials/how-to-protect-sensitive-data-in-terraform) -- Comprehensive guide on protecting sensitive data including variables, outputs, and state
- [AWS Systems Manager Parameter Store](https://docs.aws.amazon.com/systems-manager/latest/userguide/systems-manager-parameter-store.html) -- AWS documentation on Parameter Store and the SecureString type for storing secrets

# 3. Variable Validation with Custom Error Messages

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 2 (Optional Attributes with Defaults)

## Learning Objectives

After completing this exercise, you will be able to:

- Implement `validation` blocks with `condition` and `error_message` to catch invalid inputs at plan time
- Use `can()` to wrap expressions that might fail, converting errors into safe boolean results
- Distinguish between `contains()`, `regex()`, and range checks as validation strategies for different input types

## Why Variable Validation

Terraform variables accept any value that matches their type constraint. A variable typed as `string` accepts any string, and a `number` accepts any number. But in practice, your infrastructure has stricter requirements: a project name must follow a naming convention, a port must be in a non-privileged range, a CIDR block must be syntactically valid.

Without validation, these bad values pass through `terraform plan` and only cause errors during `terraform apply` -- or worse, create misconfigured resources silently. Validation blocks shift this feedback to the earliest possible moment. When a condition fails, Terraform stops with your custom error message before generating any plan, saving time and preventing mistakes from reaching cloud APIs.

## Defining Validation Rules

Each variable can have one or more `validation` blocks. The `condition` must be a boolean expression that references only the variable being validated. The `error_message` should describe what is expected, not just what went wrong.

Create `variables.tf`:

```hcl
variable "project_name" {
  type = string
  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{2,19}$", var.project_name))
    error_message = "Project name must be 3-20 chars, lowercase alphanumeric and hyphens, starting with a letter."
  }
}

variable "environment" {
  type = string
  validation {
    condition     = contains(["dev", "staging", "prod"], var.environment)
    error_message = "Environment must be one of: dev, staging, prod."
  }
}

variable "instance_type" {
  type = string
  validation {
    condition     = can(regex("^t3\\.(micro|small|medium|large)$", var.instance_type))
    error_message = "Instance type must be t3.micro, t3.small, t3.medium, or t3.large."
  }
}

variable "port" {
  type = number
  validation {
    condition     = var.port >= 1024 && var.port <= 65535
    error_message = "Port must be between 1024 and 65535 (non-privileged)."
  }
}

variable "cidr_block" {
  type = string
  validation {
    condition     = can(cidrhost(var.cidr_block, 0))
    error_message = "Must be a valid CIDR block (e.g. 10.0.0.0/16)."
  }
}
```

Create `terraform.tfvars`:

```hcl
project_name  = "my-app"
environment   = "dev"
instance_type = "t3.micro"
port          = 8080
cidr_block    = "10.0.0.0/16"
```

## Testing Valid Inputs

With the valid values in `terraform.tfvars`, the plan succeeds without errors.

```bash
terraform plan
```

```
No changes. Your infrastructure matches the configuration.
```

## Testing Invalid Inputs

Each validation rule rejects bad input with a clear message. Test them one at a time to see the exact error.

**Invalid project name (uppercase, spaces):**

```bash
terraform plan -var='project_name=My Project'
```

```
Error: Invalid value for variable

  on variables.tf line 1:
   1: variable "project_name" {

Project name must be 3-20 chars, lowercase alphanumeric and hyphens, starting
with a letter.
```

**Invalid environment:**

```bash
terraform plan -var='environment=qa'
```

```
Error: Invalid value for variable

  on variables.tf line 8:
   8: variable "environment" {

Environment must be one of: dev, staging, prod.
```

**Port out of range:**

```bash
terraform plan -var='port=80'
```

```
Error: Invalid value for variable

  on variables.tf line 22:
  22: variable "port" {

Port must be between 1024 and 65535 (non-privileged).
```

**Invalid CIDR:**

```bash
terraform plan -var='cidr_block=not-a-cidr'
```

```
Error: Invalid value for variable

  on variables.tf line 29:
  29: variable "cidr_block" {

Must be a valid CIDR block (e.g. 10.0.0.0/16).
```

## How can() Works

The `can()` function is the bridge between expressions that throw errors and the boolean that `condition` requires. Without `can()`, a failing `regex()` call would crash Terraform instead of producing a validation error:

```bash
terraform console
```

```
> can(regex("^[0-9]+$", "abc"))
false
> can(regex("^[0-9]+$", "123"))
true
> can(cidrhost("10.0.0.0/16", 0))
true
> can(cidrhost("invalid", 0))
false
> exit
```

## Common Mistakes

### Using regex() without can() in a validation condition

`regex()` throws an error when the pattern does not match. Without `can()`, Terraform crashes instead of showing your custom error message:

```hcl
# Wrong: regex() will error on non-matching input
validation {
  condition     = regex("^[a-z]+$", var.project_name) != ""
  error_message = "Must be lowercase letters."
}
```

Wrap it: `condition = can(regex("^[a-z]+$", var.project_name))`.

### Writing error messages that describe the failure instead of the expectation

A good error message tells the user what to do, not just what went wrong:

```hcl
# Bad: tells the user what happened
error_message = "The value you entered is invalid."

# Good: tells the user what is expected
error_message = "Project name must be 3-20 chars, lowercase alphanumeric and hyphens, starting with a letter."
```

## Verify What You Learned

```bash
terraform plan -var='project_name=my-app' -var='environment=dev' -var='instance_type=t3.micro' -var='port=8080' -var='cidr_block=10.0.0.0/16'
```

```
No changes. Your infrastructure matches the configuration.
```

```bash
terraform plan -var='project_name=INVALID!'
```

```
Error: Invalid value for variable

Project name must be 3-20 chars, lowercase alphanumeric and hyphens, starting
with a letter.
```

```bash
terraform console
```

```
> can(regex("^t3\\.(micro|small|medium|large)$", "t3.xlarge"))
false
> contains(["dev", "staging", "prod"], "staging")
true
> exit
```

## What's Next

You learned how to catch invalid inputs early with validation blocks, `can()`, and descriptive error messages. In the next exercise, you will explore sensitive variables to understand how Terraform redacts confidential values in plan output and what security guarantees it does (and does not) provide.

## Reference

- [Input Variables - Custom Validation Rules](https://developer.hashicorp.com/terraform/language/values/variables#custom-validation-rules)
- [can Function](https://developer.hashicorp.com/terraform/language/functions/can)
- [regex Function](https://developer.hashicorp.com/terraform/language/functions/regex)
- [contains Function](https://developer.hashicorp.com/terraform/language/functions/contains)

## Additional Resources

- [Terraform Variables Tutorial](https://developer.hashicorp.com/terraform/tutorials/configuration-language/variables) -- Official HashiCorp tutorial including the section on custom validation rules with condition and error_message
- [Terraform Variable Validation](https://spacelift.io/blog/terraform-variable-validation) -- Practical guide with multiple validation examples using can(), regex(), contains(), and conditional expressions
- [Terraform Variable Validation with Samples](https://dev.to/drewmullen/terraform-variable-validation-with-samples-1ank) -- Collection of real-world validation block examples for common use cases

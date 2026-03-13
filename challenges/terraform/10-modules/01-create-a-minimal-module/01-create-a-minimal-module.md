# 41. Create a Minimal Module

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 40 (Pass Providers to Child Modules)

## Learning Objectives

After completing this exercise, you will be able to:

- Structure a Terraform module with the standard `variables.tf`, `main.tf`, and `outputs.tf` convention
- Use input validation to catch invalid values before `apply`
- Merge default tags with caller-supplied tags using `merge()`
- Consume module outputs from the root module

## Why Modules

Copy-pasting resource blocks across configurations is a maintenance burden. When a naming convention changes or a new tag becomes mandatory, you have to update every copy. Modules solve this by encapsulating a group of related resources behind a clean interface of inputs and outputs. Change the module once, and every caller picks up the update.

A well-designed module also enforces organizational standards -- naming patterns, required tags, security defaults -- without relying on every team member to remember the rules. This exercise walks you through building your first module and consuming it from a root configuration.

## Step 1 -- Create the Module Directory

Create the directory `modules/tagged-parameter/` with three files.

```hcl
# modules/tagged-parameter/variables.tf

variable "name" {
  type        = string
  description = "Parameter name (without leading slash)"
  validation {
    condition     = !startswith(var.name, "/")
    error_message = "Name should not start with a slash."
  }
}

variable "value" {
  type        = string
  description = "Parameter value"
}

variable "environment" {
  type        = string
  description = "Environment name"
}

variable "extra_tags" {
  type        = map(string)
  default     = {}
  description = "Additional tags to apply"
}
```

```hcl
# modules/tagged-parameter/main.tf

locals {
  tags = merge(
    {
      Name        = var.name
      Environment = var.environment
      ManagedBy   = "terraform"
      Module      = "tagged-parameter"
    },
    var.extra_tags
  )
}

resource "aws_ssm_parameter" "this" {
  name  = "/${var.environment}/${var.name}"
  type  = "String"
  value = var.value
  tags  = local.tags
}
```

```hcl
# modules/tagged-parameter/outputs.tf

output "arn" {
  description = "ARN of the SSM parameter"
  value       = aws_ssm_parameter.this.arn
}

output "name" {
  description = "Full name of the SSM parameter"
  value       = aws_ssm_parameter.this.name
}
```

## Step 2 -- Call the Module from Root

Create `main.tf` in the exercise root directory:

```hcl
# main.tf

module "db_host" {
  source      = "./modules/tagged-parameter"
  name        = "db/host"
  value       = "db.internal.example.com"
  environment = "dev"
  extra_tags  = { Team = "platform" }
}

output "db_host_arn"  { value = module.db_host.arn }
output "db_host_name" { value = module.db_host.name }
```

## Step 3 -- Plan and Validate

```bash
terraform init
terraform plan
```

### Scenario A -- Successful plan (expected)

```
Terraform will perform the following actions:

  # module.db_host.aws_ssm_parameter.this will be created
  + resource "aws_ssm_parameter" "this" {
      + name  = "/dev/db/host"
      + tags  = {
          + "Environment" = "dev"
          + "ManagedBy"   = "terraform"
          + "Module"      = "tagged-parameter"
          + "Name"        = "db/host"
          + "Team"        = "platform"
        }
      + type  = "String"
      + value = "db.internal.example.com"
      ...
    }

Plan: 1 to add, 0 to change, 0 to destroy.
```

### Scenario B -- Validation failure

Change the `name` input to start with a slash:

```hcl
module "db_host" {
  source      = "./modules/tagged-parameter"
  name        = "/db/host"
  ...
}
```

```bash
terraform validate
```

```
Error: Invalid value for variable

  on main.tf line 3, in module "db_host":
   3:   name        = "/db/host"

Name should not start with a slash.
```

The validation rule catches the error before any API call is made.

### Scenario C -- extra_tags override

If `extra_tags` contains a key that conflicts with a default tag, `merge()` gives precedence to the second map. For example, `extra_tags = { ManagedBy = "custom" }` will override the default `ManagedBy = "terraform"`. This is intentional and allows callers to customize when needed.

## Common Mistakes

### Putting the provider block inside the module

Modules should not contain `provider` blocks. The provider is inherited from the caller (or passed explicitly via the `providers` argument). Hardcoding a provider inside a module makes it impossible to reuse across regions or accounts.

### Omitting output descriptions

Outputs without `description` attributes work, but they make the module harder to use. Always add a description so that consumers can run `terraform output` or read the documentation and understand what each value represents.

## Verify What You Learned

```bash
terraform validate
```

Expected: `Success! The configuration is valid.`

```bash
terraform plan
```

Expected: `Plan: 1 to add, 0 to change, 0 to destroy.`

```bash
terraform plan -out=tf.plan && terraform show -json tf.plan | jq '.planned_values.root_module.child_modules[0].resources[0].values.name'
```

Expected: `"/dev/db/host"`

```bash
terraform plan -out=tf.plan && terraform show -json tf.plan | jq '.planned_values.root_module.child_modules[0].resources[0].values.tags'
```

Expected:

```json
{
  "Environment": "dev",
  "ManagedBy": "terraform",
  "Module": "tagged-parameter",
  "Name": "db/host",
  "Team": "platform"
}
```

## What's Next

In exercise 42 you will use `for_each` to create multiple instances of this module from a map, learning how to access individual instances by key and build derived outputs.

## Reference

- [Modules Configuration](https://developer.hashicorp.com/terraform/language/modules/configuration)

## Additional Resources

- [Build and Use a Local Module](https://developer.hashicorp.com/terraform/tutorials/modules/module-create) -- step-by-step tutorial for creating your first reusable module
- [Module Sources](https://developer.hashicorp.com/terraform/language/modules/sources) -- reference on how to reference modules from local paths, Git, Registry, and S3
- [Terraform Modules: Create, Use, and Best Practices](https://spacelift.io/blog/what-are-terraform-modules) -- comprehensive guide on module encapsulation and composition
- [CloudPosse Example Module](https://github.com/cloudposse/terraform-example-module) -- real-world example of a well-structured open-source module

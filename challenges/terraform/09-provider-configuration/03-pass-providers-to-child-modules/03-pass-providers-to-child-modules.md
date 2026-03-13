# 40. Pass Providers to Child Modules with configuration_aliases

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 39 (Assume Role for Cross-Account Access)

## Learning Objectives

After completing this exercise, you will be able to:

- Declare `configuration_aliases` inside a module's `required_providers` block
- Map root-module provider aliases to a child module's expected aliases using the `providers` block
- Explain the provider inheritance rules: default providers are inherited automatically, aliased providers must be passed explicitly

## Why Provider Passthrough to Modules

Modules are designed for reuse, and a truly reusable module should not hardcode the region or account it operates in. When a module needs to create resources in more than one region (or account), it must receive multiple provider configurations from its caller. Terraform solves this with two mechanisms: the module declares which extra providers it expects via `configuration_aliases`, and the caller supplies them via the `providers` block in the module call.

This decoupling is powerful. The same module can deploy a primary resource in `us-east-1` and a replica in `eu-west-1` for one team, then be reused by another team with `ap-southeast-1` and `ap-northeast-1` -- without changing a single line inside the module. Mastering this pattern is essential for building enterprise-grade, multi-region infrastructure.

## Step 1 -- Create the Multi-Region Module

Create the directory `modules/multi-region-param/` and add the following file.

```hcl
# modules/multi-region-param/main.tf

terraform {
  required_providers {
    aws = {
      source                = "hashicorp/aws"
      configuration_aliases = [aws.secondary]
    }
  }
}

variable "name"  { type = string }
variable "value" { type = string }

resource "aws_ssm_parameter" "primary" {
  name  = "/kata/${var.name}"
  type  = "String"
  value = "${var.value}-primary"
}

resource "aws_ssm_parameter" "secondary" {
  provider = aws.secondary
  name     = "/kata/${var.name}"
  type     = "String"
  value    = "${var.value}-secondary"
}

output "primary_arn"   { value = aws_ssm_parameter.primary.arn }
output "secondary_arn" { value = aws_ssm_parameter.secondary.arn }
```

The `configuration_aliases = [aws.secondary]` line tells Terraform: "This module expects to receive a second AWS provider called `aws.secondary` from whoever calls it."

## Step 2 -- Call the Module from Root

Create `main.tf` in the root of the exercise directory:

```hcl
# main.tf

provider "aws" {
  region = "us-east-1"
}

provider "aws" {
  alias  = "west"
  region = "us-west-2"
}

module "config" {
  source = "./modules/multi-region-param"
  name   = "multi-region-config"
  value  = "hello"

  providers = {
    aws           = aws
    aws.secondary = aws.west
  }
}

output "primary_arn"   { value = module.config.primary_arn }
output "secondary_arn" { value = module.config.secondary_arn }
```

The `providers` block maps the root module's `aws` (default, `us-east-1`) to the child's default `aws`, and the root's `aws.west` to the child's `aws.secondary`.

## Step 3 -- Plan and Inspect

```bash
terraform init
terraform plan
```

### Scenario A -- Successful plan (expected)

Terraform shows two SSM parameters, each in a different region:

```
Plan: 2 to add, 0 to change, 0 to destroy.

Changes to Outputs:
  + primary_arn   = (known after apply)
  + secondary_arn = (known after apply)
```

After apply, the primary ARN will contain `us-east-1` and the secondary ARN will contain `us-west-2`.

### Scenario B -- Missing providers block

If you remove the `providers` block from the module call:

```
Error: Module "config" has configuration_aliases for provider
hashicorp/aws with alias "secondary", but that provider configuration
was not passed to the module.
```

Terraform refuses to plan because the module explicitly requires `aws.secondary` and it was not supplied.

### Scenario C -- Swapping regions

Change the mapping to use a different region pair without modifying the module:

```hcl
provider "aws" {
  alias  = "tokyo"
  region = "ap-northeast-1"
}

module "config" {
  source = "./modules/multi-region-param"
  name   = "multi-region-config"
  value  = "hello"

  providers = {
    aws           = aws
    aws.secondary = aws.tokyo
  }
}
```

The module code stays identical -- only the caller decides which regions to target.

## Common Mistakes

### Assuming aliased providers are inherited automatically

Only the default provider (no alias) is inherited by child modules. If a module needs an aliased provider, it must declare it in `configuration_aliases` and the caller must pass it in the `providers` block. Forgetting either side produces an error.

### Confusing the caller's alias with the module's alias

The root module calls its provider `aws.west`, but the child module expects `aws.secondary`. These names do not need to match. The `providers` block in the module call is the mapping layer. Writing `aws.secondary = aws.secondary` when the root has no such alias will fail.

## Verify What You Learned

```bash
terraform validate
```

Expected: `Success! The configuration is valid.`

```bash
terraform plan
```

Expected: `Plan: 2 to add, 0 to change, 0 to destroy.`

```bash
terraform plan -out=tf.plan && terraform show -json tf.plan | jq '[.planned_values.root_module.child_modules[].resources[].values.value]'
```

Expected:

```json
[
  "hello-primary",
  "hello-secondary"
]
```

```bash
terraform plan -out=tf.plan && terraform show -json tf.plan | jq '[.planned_values.root_module.child_modules[].resources[].values.name]'
```

Expected:

```json
[
  "/kata/multi-region-config",
  "/kata/multi-region-config"
]
```

## Section 09 Summary -- Provider Configuration

Across three exercises you explored the full spectrum of provider configuration:

- **Exercise 38** -- Provider aliases let you target multiple regions from a single configuration.
- **Exercise 39** -- The `assume_role` block enables cross-account access via STS without distributing credentials.
- **Exercise 40** -- `configuration_aliases` and the `providers` block let you pass provider configurations into child modules, making them region- and account-agnostic.

Together, these patterns give you the building blocks for multi-region disaster recovery, multi-account landing zones, and reusable infrastructure modules that work across any combination of regions and accounts.

## What's Next

In section 10 (Modules) you will build on these foundations by creating reusable modules from scratch, scaling them with `for_each`, controlling execution order with `depends_on`, and pinning versions from Git repositories and the Terraform Registry.

## Reference

- [Providers Within Modules](https://developer.hashicorp.com/terraform/language/modules/develop/providers)

## Additional Resources

- [Mastering Terraform Alias & Configuration Aliases](https://medium.com/@niraj8241/mastering-terraform-alias-configuration-aliases-simplify-multi-provider-management-c6493364fc32) -- detailed examples of configuration_aliases and the providers block in module calls
- [Spacelift: How to Use Terraform Provider Alias](https://spacelift.io/blog/terraform-provider-alias) -- practical guide covering provider passthrough to modules including multi-region patterns
- [Create Modules for Reuse](https://developer.hashicorp.com/terraform/tutorials/modules/module-create) -- HashiCorp tutorial on building reusable modules including provider configuration

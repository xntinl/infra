# 22. Move a Resource to a Module with state mv

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 21 (Import an Existing S3 Bucket)

## Learning Objectives

After completing this exercise, you will be able to:

- Use `terraform state mv` to relocate resources within the state file without destroying them
- Refactor flat resource definitions into reusable modules while preserving live infrastructure
- Understand Terraform state address format for both root-level and module-scoped resources

## Why Move Resources in State

As Terraform configurations grow, flat lists of resources become difficult to maintain. The natural next step is to group related resources into modules. But here is the problem: if you simply move a resource block into a module directory, Terraform sees the old address disappear and a new address appear. It interprets this as "destroy the old resource, create a new one" -- which is exactly what you do not want for production infrastructure.

`terraform state mv` solves this by updating the state file to reflect the new address without touching the real resource. The resource continues running in the cloud; only its internal address in the state changes from `aws_ssm_parameter.config_a` to `module.config_a.aws_ssm_parameter.this`.

This is a critical skill for anyone maintaining long-lived Terraform projects. Refactoring should never require downtime or resource recreation.

## Step 1 -- Create the Flat Configuration

Start with two SSM Parameters defined at the root level.

```hcl
# main.tf

terraform {
  required_version = ">= 1.7"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = "us-east-1"
}

resource "aws_ssm_parameter" "config_a" {
  name  = "/kata/config-a"
  type  = "String"
  value = "value-a"
}

resource "aws_ssm_parameter" "config_b" {
  name  = "/kata/config-b"
  type  = "String"
  value = "value-b"
}
```

Apply the initial configuration:

```bash
terraform init
terraform apply -auto-approve
```

Confirm both resources exist:

```bash
terraform state list
```

Expected output:

```
aws_ssm_parameter.config_a
aws_ssm_parameter.config_b
```

## Step 2 -- Create the Module

Create a reusable module that encapsulates a single SSM Parameter.

```hcl
# modules/config/main.tf

variable "name"  { type = string }
variable "value" { type = string }

resource "aws_ssm_parameter" "this" {
  name  = "/kata/${var.name}"
  type  = "String"
  value = var.value
}

output "arn" { value = aws_ssm_parameter.this.arn }
```

## Step 3 -- Refactor the Root to Use the Module

Replace the flat resource blocks with module calls. Remove the old `resource` blocks entirely.

```hcl
# main.tf (updated -- replace the two resource blocks)

module "config_a" {
  source = "./modules/config"
  name   = "config-a"
  value  = "value-a"
}

module "config_b" {
  source = "./modules/config"
  name   = "config-b"
  value  = "value-b"
}
```

Re-initialize to register the new module:

```bash
terraform init
```

**Do NOT run `terraform plan` yet.** It would show a destroy + create for each resource. First, move the state.

## Step 4 -- Move Resources in State

```bash
terraform state mv aws_ssm_parameter.config_a module.config_a.aws_ssm_parameter.this
terraform state mv aws_ssm_parameter.config_b module.config_b.aws_ssm_parameter.this
```

Expected output for each command:

```
Move "aws_ssm_parameter.config_a" to "module.config_a.aws_ssm_parameter.this"
Successfully moved 1 object(s).
```

Now verify the plan is clean:

```bash
terraform plan
```

Expected output:

```
No changes. Your infrastructure matches the configuration.
```

## Step 5 -- Clean Up

```bash
terraform destroy -auto-approve
```

## Common Mistakes

### Running plan before state mv

If you refactor the code and run `terraform plan` without moving the state first, Terraform will show a plan to destroy the old resources and create new ones. This is harmless if you catch it before applying, but applying that plan would destroy and recreate real infrastructure.

### Getting the module address wrong

The target address must follow the exact pattern `module.<module_name>.<resource_type>.<resource_name>`. A common error is forgetting the resource name inside the module (e.g., writing `module.config_a.aws_ssm_parameter` instead of `module.config_a.aws_ssm_parameter.this`).

## Verify What You Learned

1. Confirm the resources are now under module addresses:

```bash
terraform state list
```

Expected output:

```
module.config_a.aws_ssm_parameter.this
module.config_b.aws_ssm_parameter.this
```

2. Confirm the plan shows no changes:

```bash
terraform plan
```

Expected output:

```
No changes. Your infrastructure matches the configuration.
```

3. Confirm the real SSM Parameters were never recreated (check the AWS Console or CLI):

```bash
aws ssm get-parameter --name "/kata/config-a" --query "Parameter.Value" --output text
```

Expected output:

```
value-a
```

## What's Next

In the next exercise, you will learn how to remove a resource from the Terraform state without destroying it in the cloud -- useful when you want to hand off management of a resource to another team or tool.

## Reference

- [terraform state mv Command](https://developer.hashicorp.com/terraform/cli/commands/state/mv)
- [Resource Addressing](https://developer.hashicorp.com/terraform/cli/state/resource-addressing)

## Additional Resources

- [HashiCorp: Move Terraform Config Tutorial](https://developer.hashicorp.com/terraform/tutorials/configuration-language/move-config) -- official tutorial for refactoring configuration by moving resources to modules
- [HashiCorp: moved Block](https://developer.hashicorp.com/terraform/language/moved) -- the declarative alternative to `state mv` using `moved` blocks in HCL code
- [Spacelift: Terraform State Management Guide](https://spacelift.io/blog/terraform-state) -- comprehensive guide to state management including move and refactoring operations

# 18. Conditional Resource Creation with count

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 17 (IAM Users with Different Policies)

## Learning Objectives

After completing this exercise, you will be able to:

- Use the `count = condition ? 1 : 0` pattern to create optional resources
- Access conditional resources safely using `one()` and splat expressions
- Write outputs that handle both the present and absent cases of a conditional resource

## Why Conditional Resources with `count`

Not every resource belongs in every environment. A bastion host is needed in production but not in development. A monitoring endpoint exists when monitoring is enabled but not otherwise. A production-specific configuration should only be created when the environment is `prod`.

Terraform has no native `if` statement for resources. Instead, the community settled on a clean idiom: set `count` to `1` to create the resource or `0` to skip it entirely. A ternary expression bridges the gap between a boolean condition and the numeric `count`:

```hcl
count = var.enable_feature ? 1 : 0
```

When `count = 0`, Terraform acts as if the resource block does not exist -- nothing is planned, created, or tracked in state.

## Step 1 -- Define the Condition Variables

Create `variables.tf`:

```hcl
variable "environment" {
  description = "Deployment environment (dev, staging, prod)"
  default     = "prod"
}

variable "enable_monitoring" {
  description = "Whether to create the monitoring configuration"
  type        = bool
  default     = true
}

variable "enable_bastion" {
  description = "Whether to create the bastion configuration"
  type        = bool
  default     = false
}
```

## Step 2 -- Create Conditional Resources

Create `main.tf`:

```hcl
locals {
  is_production = var.environment == "prod"
}

# Only created when environment is "prod"
resource "aws_ssm_parameter" "prod_config" {
  count = local.is_production ? 1 : 0
  name  = "/app/prod-specific-config"
  type  = "String"
  value = "production-settings"
}

# Only created when monitoring is enabled
resource "aws_ssm_parameter" "monitoring" {
  count = var.enable_monitoring ? 1 : 0
  name  = "/app/monitoring-endpoint"
  type  = "String"
  value = "https://monitoring.example.com"
}

# Only created when bastion is enabled
resource "aws_ssm_parameter" "bastion" {
  count = var.enable_bastion ? 1 : 0
  name  = "/app/bastion-ip"
  type  = "String"
  value = "10.0.0.100"
}
```

With the defaults (`environment = "prod"`, `enable_monitoring = true`, `enable_bastion = false`), only `prod_config` and `monitoring` are created. The bastion resource is skipped entirely.

## Step 3 -- Write Safe Outputs

When a resource has `count = 0`, referencing `resource[0].arn` causes an index-out-of-range error. There are two ways to handle this:

Create `outputs.tf`:

```hcl
# Approach 1: Ternary with explicit index
output "monitoring_param" {
  description = "ARN of the monitoring parameter, or a disabled message"
  value       = var.enable_monitoring ? aws_ssm_parameter.monitoring[0].arn : "monitoring disabled"
}

output "bastion_param" {
  description = "ARN of the bastion parameter, or a disabled message"
  value       = var.enable_bastion ? aws_ssm_parameter.bastion[0].arn : "bastion disabled"
}

# Approach 2: one() with splat expression -- returns the value or null
output "prod_config_arn" {
  description = "ARN of the prod config parameter, or null if not prod"
  value       = one(aws_ssm_parameter.prod_config[*].arn)
}
```

The `one()` function takes a list of zero or one elements and returns the element or `null`. Combined with the splat expression `[*]`, it provides a clean way to handle conditional resources without ternary nesting.

## Step 4 -- Test the Production Scenario

Run `terraform plan` with the defaults:

```bash
terraform plan
```

Expect 2 resources to create (`prod_config` and `monitoring`). The `bastion_param` output should show `"bastion disabled"`.

## Step 5 -- Switch to a Non-Production Environment

Override the environment:

```bash
terraform plan -var="environment=dev"
```

Now `prod_config` is also skipped. Only `monitoring` should be planned for creation. The `prod_config_arn` output will be `null`.

## Step 6 -- Enable the Bastion

```bash
terraform plan -var="enable_bastion=true"
```

Now all three resources are planned. The `bastion_param` output will show the ARN instead of the disabled message.

## Common Mistakes

### Referencing `[0]` without a guard

```hcl
# Wrong -- crashes when count = 0
output "bastion_arn" {
  value = aws_ssm_parameter.bastion[0].arn
}
```

When `count = 0`, there is no `[0]` element. Always guard with a ternary or use `one()`:

```hcl
value = one(aws_ssm_parameter.bastion[*].arn)
```

### Using `count` when `for_each` is more appropriate

```hcl
# Fragile -- if you remove item at index 1, items at 2+ shift
resource "aws_s3_bucket" "this" {
  count  = length(var.buckets)
  bucket = var.buckets[count.index]
}
```

`count` is ideal for 0-or-1 conditional creation. For collections of named resources, use `for_each` with stable keys as shown in exercises 16 and 17.

## Verify What You Learned

Default scenario (prod, monitoring on, bastion off):

```bash
terraform plan
```

```
Terraform will perform the following actions:

  # aws_ssm_parameter.monitoring[0] will be created
  + resource "aws_ssm_parameter" "monitoring" {
      + name  = "/app/monitoring-endpoint"
      + type  = "String"
      + value = "https://monitoring.example.com"
    }

  # aws_ssm_parameter.prod_config[0] will be created
  + resource "aws_ssm_parameter" "prod_config" {
      + name  = "/app/prod-specific-config"
      + type  = "String"
      + value = "production-settings"
    }

Plan: 2 to add, 0 to change, 0 to destroy.

Changes to Outputs:
  + bastion_param    = "bastion disabled"
  + monitoring_param = (known after apply)
  + prod_config_arn  = (known after apply)
```

Dev scenario:

```bash
terraform plan -var="environment=dev" | grep "will be created" | wc -l
```

```
1
```

All features enabled:

```bash
terraform plan -var="enable_bastion=true" | grep "will be created" | wc -l
```

```
3
```

## What's Next

In the next exercise you will externalize resource configuration to a YAML file and use `yamldecode()` with `for_each` to drive infrastructure from data files instead of HCL variables.

## Reference

- [The count Meta-Argument](https://developer.hashicorp.com/terraform/language/meta-arguments/count)
- [Conditional Expressions](https://developer.hashicorp.com/terraform/language/expressions/conditionals)
- [one Function](https://developer.hashicorp.com/terraform/language/functions/one)

## Additional Resources

- [Spacelift: Terraform Conditionals](https://spacelift.io/blog/terraform-conditionals) -- comprehensive guide on conditional expressions including the count = condition ? 1 : 0 pattern
- [HashiCorp: The count Meta-Argument Tutorial](https://developer.hashicorp.com/terraform/tutorials/configuration-language/count) -- official tutorial for using count with multiple instances and conditional resources
- [env0: Terraform Conditional Resources](https://www.env0.com/blog/terraform-conditional-resources) -- practical walkthrough of conditional resource creation with real-world AWS examples

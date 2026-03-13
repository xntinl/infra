# 36. Locals for Derived Values

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 35 (Structured Outputs)

## Learning Objectives

After completing this exercise, you will be able to:

- Use `locals` blocks to compute values once and reference them throughout a configuration
- Build naming conventions and common tags from input variables using locals
- Apply conditional logic in locals to adapt configuration by environment

## Why Locals for Derived Values

Input variables provide raw configuration values -- project name, environment, region. But resources need computed values derived from those inputs: a name prefix combining project and environment, tags that include all context, log retention that changes based on whether you are in production. Without locals, you end up repeating the same expressions in every resource block: `"${var.project}-${var.environment}"` appears in every resource name, and the same ternary for log retention appears in every CloudWatch log group.

Locals solve this by computing a value once and giving it a name. Every resource references `local.name_prefix` instead of repeating the interpolation. When the naming convention changes, you update one local instead of searching through every resource block. When business rules change (production log retention goes from 365 to 730 days), you change one line.

This is not just about reducing keystrokes. Locals make the configuration easier to read because resource blocks only reference named values that communicate intent -- `local.is_production` is clearer than `var.environment == "prod"` repeated twenty times across the codebase.

## Step 1 -- Create the Configuration

Create a file named `main.tf`:

```hcl
# main.tf

variable "project"     { default = "kata" }
variable "environment" { default = "dev" }
variable "region"      { default = "us-east-1" }

locals {
  # Naming convention: project-environment
  name_prefix = "${var.project}-${var.environment}"

  # Environment detection
  is_production = var.environment == "prod"
  is_us_region  = startswith(var.region, "us-")

  # Standard tags applied to all resources
  common_tags = {
    Project     = var.project
    Environment = var.environment
    Region      = var.region
    ManagedBy   = "terraform"
  }

  # Conditional values that change by environment
  resource_prefix = local.is_production ? "${local.name_prefix}-prd" : local.name_prefix
  log_retention   = local.is_production ? 365 : 30
  backup_enabled  = local.is_production
}

resource "aws_ssm_parameter" "demo" {
  name  = "/${local.name_prefix}/demo"
  type  = "String"
  value = local.resource_prefix
  tags  = local.common_tags
}

output "name_prefix"     { value = local.name_prefix }
output "resource_prefix" { value = local.resource_prefix }
output "log_retention"   { value = local.log_retention }
output "is_production"   { value = local.is_production }
```

## Step 2 -- Understand Each Local

**`name_prefix`**: Combines project and environment into a standard prefix used for all resource names. This ensures consistent naming across the entire infrastructure.

**`is_production` and `is_us_region`**: Boolean locals that capture environment detection logic once. Using `startswith()` for region detection is cleaner than comparing against multiple region names.

**`common_tags`**: A map of tags applied to every resource. Centralizing tags in a local ensures consistency and makes it easy to add new tags globally.

**`resource_prefix`, `log_retention`, `backup_enabled`**: Conditional values that change behavior by environment. Production gets longer log retention, backups enabled, and an extended resource prefix.

## Step 3 -- Test with Default Values (dev)

```bash
terraform init
terraform apply -auto-approve
terraform output
```

## Step 4 -- Test with Production Values

```bash
terraform plan -var="environment=prod"
```

Observe how the computed values change:
- `name_prefix` becomes `"kata-prod"`
- `resource_prefix` becomes `"kata-prod-prd"`
- `log_retention` becomes `365`
- `is_production` becomes `true`

## Step 5 -- Test the Tag Propagation

```bash
terraform apply -auto-approve
aws ssm get-parameters-by-path --path "/kata-dev/" --query "Parameters[].{Name:Name,Tags:Tags}" --output json
```

Verify that the SSM parameter carries all four tags defined in `common_tags`.

## Common Mistakes

### Putting complex logic directly in resource blocks

Instead of writing:

```hcl
resource "aws_cloudwatch_log_group" "app" {
  retention_in_days = var.environment == "prod" ? 365 : 30
}
```

Use a local:

```hcl
locals {
  log_retention = var.environment == "prod" ? 365 : 30
}

resource "aws_cloudwatch_log_group" "app" {
  retention_in_days = local.log_retention
}
```

The resource block becomes cleaner, and the business rule is defined in one place.

### Overusing locals for trivial values

Not every value needs a local. If a variable is used only once and the reference is already clear, wrapping it in a local adds indirection without benefit. Use locals when a value is computed, reused, or represents a business rule.

## Verify What You Learned

Run the following commands and confirm the output:

```bash
terraform output name_prefix
```

Expected: `"kata-dev"`

```bash
terraform output log_retention
```

Expected: `30`

```bash
terraform output is_production
```

Expected: `false`

```bash
terraform plan -var="environment=prod" -no-color 2>&1 | grep "resource_prefix"
```

Expected: output showing `"kata-prod-prd"`

```bash
terraform plan -var="environment=prod" -no-color 2>&1 | grep "log_retention"
```

Expected: output showing `365`

## What's Next

In the next exercise, you will chain locals together in a multi-step data transformation pipeline -- each local building on the previous one to enrich, group, filter, and aggregate data.

## Reference

- [Terraform Local Values](https://developer.hashicorp.com/terraform/language/values/locals)

## Additional Resources

- [Simplify Terraform Configuration with Locals (HashiCorp Tutorial)](https://developer.hashicorp.com/terraform/tutorials/configuration-language/locals) -- interactive tutorial covering locals for reducing duplication and deriving values
- [Spacelift: Terraform Locals](https://spacelift.io/blog/terraform-locals) -- practical guide with examples of conditional locals, naming conventions, and common tags
- [env0: Terraform Local Variables](https://www.env0.com/blog/terraform-local-variables) -- when to use locals vs variables, with examples of derived and conditional values

# 31. Dynamic Availability Zones with Data Sources

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 30 (Find the Latest Amazon Linux AMI with Data Sources)

## Learning Objectives

After completing this exercise, you will be able to:

- Use the `aws_availability_zones` data source to discover AZs dynamically at plan time
- Combine `slice()`, `min()`, and `length()` to safely limit a list to a maximum number of elements
- Build region-agnostic configurations that work in any AWS region without code changes

## Why Dynamic Availability Zones

Different AWS regions have different numbers of Availability Zones. `us-east-1` has six, `eu-west-1` has three, and `ap-northeast-3` has three. If you hardcode AZ names like `us-east-1a`, `us-east-1b`, and `us-east-1c` into your configuration, the code breaks the moment you deploy to a region where those names do not exist.

The `aws_availability_zones` data source eliminates this problem entirely. It queries the current region at plan time and returns the list of available AZs. You can then use `slice()` and `min()` to cap the list at a desired maximum -- say, three AZs for a typical multi-AZ deployment -- without risking an index-out-of-bounds error in regions with fewer AZs.

This pattern is essential for building reusable modules and multi-region architectures. The same Terraform code produces correct infrastructure in any region, automatically adapting to the AZ count of the target region.

## Step 1 -- Create the Configuration

Create a file named `main.tf`:

```hcl
# main.tf

data "aws_availability_zones" "available" {
  state = "available"

  filter {
    name   = "opt-in-status"
    values = ["opt-in-not-required"]
  }
}

locals {
  # Cap at 3 AZs, but use fewer if the region has fewer
  az_count = min(length(data.aws_availability_zones.available.names), 3)
  azs      = slice(data.aws_availability_zones.available.names, 0, local.az_count)
}

resource "aws_ssm_parameter" "az_config" {
  for_each = toset(local.azs)
  name     = "/kata/az/${each.key}"
  type     = "String"
  value    = each.key
}

output "all_azs"   { value = data.aws_availability_zones.available.names }
output "selected"  { value = local.azs }
output "az_count"  { value = local.az_count }
```

## Step 2 -- Understand the Filter

The `opt-in-not-required` filter excludes Local Zones and Wavelength Zones, which are opt-in AZs that behave differently from standard AZs. Without this filter, you might get zone names that do not support the resources you plan to create, causing deployment failures.

## Step 3 -- Understand the Capping Logic

The combination of `min()`, `length()`, and `slice()` creates a safe cap:

- `length(...)` returns how many AZs the region has (e.g., 6 in `us-east-1`)
- `min(..., 3)` ensures you never try to use more than 3
- `slice(..., 0, local.az_count)` extracts the first N AZs from the list

This works correctly even in regions with fewer than 3 AZs, because `min()` will return the smaller value.

## Step 4 -- Initialize and Apply

```bash
terraform init
terraform apply -auto-approve
```

## Step 5 -- Test in Another Region

To verify region-agnosticism, temporarily change the provider region:

```hcl
provider "aws" {
  region = "eu-west-1"
}
```

Run `terraform plan` and observe that the AZ names change to `eu-west-1a`, `eu-west-1b`, `eu-west-1c` without any code modification.

## Common Mistakes

### Hardcoding AZ names in resources

Writing `availability_zone = "us-east-1a"` directly in a resource block makes the configuration region-specific. Always use a data source to discover AZs and reference them by index or iteration.

### Not filtering out opt-in zones

Without the `opt-in-not-required` filter, Local Zones and Wavelength Zones may appear in the list. These zones have limited service support and can cause unexpected failures when you try to create resources like RDS instances or ALBs in them.

## Verify What You Learned

Run the following commands and confirm the output matches the expected patterns:

```bash
terraform output all_azs
```

Expected (in `us-east-1`): `["us-east-1a", "us-east-1b", "us-east-1c", "us-east-1d", "us-east-1e", "us-east-1f"]`

```bash
terraform output selected
```

Expected: a list of at most 3 AZs, e.g. `["us-east-1a", "us-east-1b", "us-east-1c"]`

```bash
terraform output az_count
```

Expected: `3` (or fewer if the region has fewer than 3 AZs)

```bash
# Verify SSM parameters were created for each selected AZ
aws ssm get-parameters-by-path --path "/kata/az/" --query "Parameters[].Name" --output json
```

Expected: `["/kata/az/us-east-1a", "/kata/az/us-east-1b", "/kata/az/us-east-1c"]`

## What's Next

In the next exercise, you will use `aws_caller_identity`, `aws_region`, and `aws_partition` to build dynamic ARNs that adapt to any AWS account and region automatically.

## Reference

- [Terraform aws_availability_zones Data Source](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/availability_zones)

## Additional Resources

- [Query Data Sources (HashiCorp Tutorial)](https://developer.hashicorp.com/terraform/tutorials/configuration-language/data-sources) -- hands-on tutorial covering data sources for querying dynamic infrastructure information
- [Spacelift: How to Use Terraform Data Sources](https://spacelift.io/blog/terraform-data-sources) -- comprehensive guide to data sources with practical AWS examples
- [Build a Multi-AZ Architecture with Terraform](https://dev.to/aws-builders/how-to-build-a-multi-az-architecture-with-terraform-2m8b) -- walkthrough of building multi-AZ architectures using dynamic data sources

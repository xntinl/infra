# 32. Account-Aware ARNs with Context Data Sources

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 31 (Dynamic Availability Zones with Data Sources)

## Learning Objectives

After completing this exercise, you will be able to:

- Use `aws_caller_identity`, `aws_region`, and `aws_partition` to retrieve runtime context
- Construct dynamic ARNs from context values instead of hardcoded strings
- Write IAM policies and resource references that work across accounts, regions, and partitions

## Why Dynamic ARN Construction

ARNs follow the pattern `arn:PARTITION:SERVICE:REGION:ACCOUNT_ID:RESOURCE`. Every component except the service name and resource identifier varies depending on where you deploy: the partition changes between commercial AWS, GovCloud, and China regions; the region changes with your deployment target; and the account ID is unique to each AWS account.

Hardcoding any of these values creates fragile infrastructure. A policy that references `arn:aws:lambda:us-east-1:123456789012:function:my-function` will not work when you deploy to a different account, region, or partition. Three data sources solve this:

- **`aws_caller_identity`** provides the current account ID and the ARN of the caller
- **`aws_region`** provides the current region name
- **`aws_partition`** provides the partition (`aws`, `aws-cn`, or `aws-us-gov`)

By combining these data sources with string interpolation, you build ARNs that are always correct in any deployment context. This is essential for writing reusable modules, multi-account architectures, and IAM policies that do not break when infrastructure moves.

## Step 1 -- Create the Configuration

Create a file named `main.tf`:

```hcl
# main.tf

data "aws_caller_identity" "current" {}
data "aws_region" "current" {}
data "aws_partition" "current" {}

locals {
  account_id = data.aws_caller_identity.current.account_id
  region     = data.aws_region.current.name
  partition  = data.aws_partition.current.partition

  # Dynamic ARNs for various AWS services
  dynamodb_table_arn = "arn:${local.partition}:dynamodb:${local.region}:${local.account_id}:table/my-table"
  lambda_arn         = "arn:${local.partition}:lambda:${local.region}:${local.account_id}:function:my-function"
  s3_bucket_arn      = "arn:${local.partition}:s3:::my-bucket"

  # IAM root ARN (note: no region for IAM, it is a global service)
  root_arn = "arn:${local.partition}:iam::${local.account_id}:root"
}

output "account_id"         { value = local.account_id }
output "caller_arn"         { value = data.aws_caller_identity.current.arn }
output "region"             { value = local.region }
output "dynamodb_table_arn" { value = local.dynamodb_table_arn }
output "lambda_arn"         { value = local.lambda_arn }
output "root_arn"           { value = local.root_arn }
```

## Step 2 -- Understand Service-Specific ARN Patterns

Different AWS services have different ARN structures:

- **Regional services with account** (DynamoDB, Lambda): `arn:PARTITION:SERVICE:REGION:ACCOUNT:RESOURCE`
- **Global services** (IAM): `arn:PARTITION:iam::ACCOUNT:RESOURCE` -- region is empty
- **S3 buckets**: `arn:PARTITION:s3:::BUCKET` -- both region and account are empty because bucket names are globally unique

Understanding these patterns is critical for constructing correct ARNs programmatically.

## Step 3 -- Initialize and Plan

```bash
terraform init
terraform plan
```

No resources are created in this exercise -- you are only producing outputs from data sources. The `terraform plan` output will show the computed ARNs.

## Step 4 -- Use in a Realistic IAM Policy

Here is how you would use these context data sources in a real IAM policy document:

```hcl
data "aws_iam_policy_document" "lambda_access" {
  statement {
    actions   = ["dynamodb:GetItem", "dynamodb:PutItem"]
    resources = [local.dynamodb_table_arn]
  }

  statement {
    actions   = ["lambda:InvokeFunction"]
    resources = [local.lambda_arn]
  }
}
```

This policy works in any account and region without modification.

## Common Mistakes

### Hardcoding account IDs in ARNs

Writing `arn:aws:lambda:us-east-1:123456789012:function:my-function` directly in your configuration means the ARN is only valid in one account in one region. Use data sources to construct ARNs dynamically.

### Assuming the partition is always `aws`

In GovCloud, the partition is `aws-us-gov`. In China regions, it is `aws-cn`. If you hardcode `arn:aws:`, your configuration breaks in those environments. Always use `data.aws_partition.current.partition`.

## Verify What You Learned

Run the following commands and confirm the output matches the expected patterns:

```bash
terraform output account_id
```

Expected: a 12-digit AWS account ID, e.g. `"123456789012"`

```bash
terraform output caller_arn
```

Expected: the ARN of the IAM identity running Terraform, e.g. `"arn:aws:iam::123456789012:user/my-user"` or an assumed role ARN

```bash
terraform output region
```

Expected: the configured region, e.g. `"us-east-1"`

```bash
terraform output dynamodb_table_arn
```

Expected: `"arn:aws:dynamodb:us-east-1:123456789012:table/my-table"` (with your actual account ID and region)

```bash
terraform output root_arn
```

Expected: `"arn:aws:iam::123456789012:root"` (with your actual account ID)

## What's Next

In the next exercise, you will use `terraform_remote_state` to read outputs from one Terraform project in another, enabling cross-project state sharing and layered infrastructure architectures.

## Reference

- [Terraform aws_caller_identity Data Source](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/caller_identity)

## Additional Resources

- [Query Data Sources (HashiCorp Tutorial)](https://developer.hashicorp.com/terraform/tutorials/configuration-language/data-sources) -- interactive tutorial covering context data sources like `aws_caller_identity` and `aws_region`
- [Spacelift: How to Use Terraform Data Sources](https://spacelift.io/blog/terraform-data-sources) -- practical guide covering the most commonly used AWS data sources including identity and region
- [AWS IAM ARN Reference](https://docs.aws.amazon.com/IAM/latest/UserGuide/reference_identifiers.html) -- official AWS reference on ARN structure and format for every service

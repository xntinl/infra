# 30. Find the Latest Amazon Linux AMI with Data Sources

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed section 06 (Expressions and Functions)

## Learning Objectives

After completing this exercise, you will be able to:

- Use the `data "aws_ami"` data source to query AWS for dynamic resource IDs
- Apply `filter` blocks to narrow AMI results by name, architecture, and state
- Use `most_recent` and `owners` to reliably select trusted, up-to-date images

## Why Data Sources for AMI Lookup

AMI IDs are region-specific and change frequently as AWS and third-party publishers release patches and updates. Hardcoding an AMI ID such as `ami-0abcdef1234567890` into your configuration creates two problems: it breaks when you deploy to a different region, and it silently drifts out of date as newer images are published.

The `aws_ami` data source solves both problems. Instead of pinning to a specific ID, you declare filters that describe the image you want -- name pattern, owner, architecture, state -- and Terraform resolves the matching AMI at plan time. Combined with `most_recent = true`, this guarantees you always get the latest image that matches your criteria, in whatever region the provider is configured for.

This pattern is foundational. Nearly every EC2-based configuration needs an AMI reference, and using a data source instead of a hardcoded ID is the difference between portable infrastructure and a configuration that only works in one account in one region.

## Step 1 -- Create the Configuration

Create a file named `main.tf`:

```hcl
# main.tf

data "aws_ami" "amazon_linux_2023" {
  most_recent = true
  owners      = ["amazon"]

  filter {
    name   = "name"
    values = ["al2023-ami-2023.*-x86_64"]
  }

  filter {
    name   = "state"
    values = ["available"]
  }

  filter {
    name   = "architecture"
    values = ["x86_64"]
  }
}

data "aws_ami" "ubuntu" {
  most_recent = true
  owners      = ["099720109477"]  # Canonical's AWS account ID

  filter {
    name   = "name"
    values = ["ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-*"]
  }

  filter {
    name   = "state"
    values = ["available"]
  }
}

output "al2023_ami_id"   { value = data.aws_ami.amazon_linux_2023.id }
output "al2023_ami_name" { value = data.aws_ami.amazon_linux_2023.name }
output "ubuntu_ami_id"   { value = data.aws_ami.ubuntu.id }
output "ubuntu_ami_name" { value = data.aws_ami.ubuntu.name }
```

## Step 2 -- Understand the Key Arguments

**`owners`** restricts the search to AMIs published by trusted accounts. Use `"amazon"` for official Amazon images, or a numeric account ID like `"099720109477"` for Canonical (Ubuntu). Never omit `owners` -- without it, Terraform searches community AMIs where anyone can publish images, which is a security risk.

**`filter` blocks** narrow the results. Each filter specifies an attribute name and one or more acceptable values. Multiple filters combine with AND logic: only AMIs matching all filters are returned.

**`most_recent`** selects the newest AMI from the filtered results. Without it, Terraform returns an error if more than one AMI matches.

## Step 3 -- Initialize and Apply

```bash
terraform init
terraform apply -auto-approve
```

## Step 4 -- Verify with a Different Region

To confirm the configuration is truly region-agnostic, add a `provider` block temporarily and re-run:

```hcl
provider "aws" {
  region = "eu-west-1"
}
```

Run `terraform apply` again. You should see different AMI IDs but the same name patterns, proving the data source adapts to the region automatically.

## Common Mistakes

### Omitting the `owners` argument

Without `owners`, the data source searches all public AMIs, including community images. This is both a security risk (untrusted publishers) and a performance issue (slow queries across millions of images). Always specify `owners` to scope the search to trusted accounts.

### Forgetting `most_recent = true`

If multiple AMIs match your filters and you do not set `most_recent = true`, Terraform returns an error:

```
Error: Your query returned more than one result.
```

Always include `most_recent = true` unless you intend to get exactly one result.

## Verify What You Learned

Run the following commands and confirm the output matches the expected patterns:

```bash
terraform output al2023_ami_id
```

Expected: an AMI ID starting with `ami-`, e.g. `"ami-0abcdef1234567890"`

```bash
terraform output al2023_ami_name
```

Expected: a name containing `al2023`, e.g. `"al2023-ami-2023.6.20250303.0-kernel-6.1-x86_64"`

```bash
terraform output ubuntu_ami_id
```

Expected: an AMI ID starting with `ami-`

```bash
terraform output ubuntu_ami_name
```

Expected: a name containing `ubuntu-noble`, e.g. `"ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-20250228"`

```bash
# Confirm both data sources resolved successfully
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## What's Next

In the next exercise, you will use the `aws_availability_zones` data source to dynamically discover AZs in the current region and build region-agnostic configurations that adapt to any deployment target.

## Reference

- [Terraform aws_ami Data Source](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/ami)

## Additional Resources

- [Query Data Sources (HashiCorp Tutorial)](https://developer.hashicorp.com/terraform/tutorials/configuration-language/data-sources) -- hands-on tutorial covering data source fundamentals including `aws_ami`
- [Spacelift: How to Use Terraform Data Sources](https://spacelift.io/blog/terraform-data-sources) -- practical guide to the most common data sources with real-world examples
- [How to Find AWS AMI ID for EC2 Instances](https://dev.to/aws-builders/how-to-find-aws-ami-id-for-ec2-instances-2023-update-3lkn) -- comparison of different methods for finding AMI IDs including the AWS Console, CLI, and Terraform

# 24. Declarative Import Block

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 23 (Remove from State Without Destroying)

## Learning Objectives

After completing this exercise, you will be able to:

- Use the `import` block to bring existing resources into Terraform state declaratively
- Combine `import` blocks with `for_each` resources to import multiple resources in a single apply
- Understand the difference between the imperative `terraform import` command and the declarative `import` block

## Why Declarative Import

In exercise 21, you used `terraform import` -- an imperative command that modifies the state immediately, bypasses the plan step, and leaves no trace in your codebase. This works, but it has drawbacks in team environments:

- No one sees the import in a pull request.
- There is no plan preview showing what will happen.
- If something goes wrong, there is no code to revert.

The `import` block (available since Terraform 1.5) solves all of these problems. You write the import intent directly in HCL. When you run `terraform plan`, it shows "will be imported" instead of "will be created." When you run `terraform apply`, the resource is imported into state. The block is single-use: after the import succeeds, you can remove it from your code (or leave it -- Terraform will simply ignore it since the resource is already in state).

This is the recommended approach for all imports in team and CI/CD workflows.

## Step 1 -- Create Resources Manually

Create two SSM Parameters outside of Terraform:

```bash
aws ssm put-parameter --name "/kata/imported-1" --type String --value "hello"
aws ssm put-parameter --name "/kata/imported-2" --type String --value "world"
```

## Step 2 -- Write the Configuration with Import Blocks

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

import {
  to = aws_ssm_parameter.param["imported-1"]
  id = "/kata/imported-1"
}

import {
  to = aws_ssm_parameter.param["imported-2"]
  id = "/kata/imported-2"
}

resource "aws_ssm_parameter" "param" {
  for_each = toset(["imported-1", "imported-2"])
  name     = "/kata/${each.key}"
  type     = "String"
  value    = each.key == "imported-1" ? "hello" : "world"
}
```

## Step 3 -- Preview the Import

```bash
terraform init
terraform plan
```

Expected output includes:

```
  # aws_ssm_parameter.param["imported-1"] will be imported
    resource "aws_ssm_parameter" "param" {
        arn            = "arn:aws:ssm:us-east-1:XXXXXXXXXXXX:parameter/kata/imported-1"
        name           = "/kata/imported-1"
        type           = "String"
        value          = "hello"
        ...
    }

  # aws_ssm_parameter.param["imported-2"] will be imported
    resource "aws_ssm_parameter" "param" {
        ...
    }

Plan: 2 to import, 0 to add, 0 to change, 0 to destroy.
```

Notice it says "will be imported" -- not "will be created." This is the key difference from a normal plan.

## Step 4 -- Apply the Import

```bash
terraform apply -auto-approve
```

Expected output includes:

```
aws_ssm_parameter.param["imported-1"]: Importing...
aws_ssm_parameter.param["imported-1"]: Import complete [id=/kata/imported-1]
aws_ssm_parameter.param["imported-2"]: Importing...
aws_ssm_parameter.param["imported-2"]: Import complete [id=/kata/imported-2]

Apply complete! Resources: 2 imported, 0 added, 0 changed, 0 destroyed.
```

## Step 5 -- Remove the Import Blocks

After a successful import, the `import` blocks are no longer needed. Remove them from `main.tf`, keeping only the `resource` block:

```hcl
# main.tf (after cleanup)

resource "aws_ssm_parameter" "param" {
  for_each = toset(["imported-1", "imported-2"])
  name     = "/kata/${each.key}"
  type     = "String"
  value    = each.key == "imported-1" ? "hello" : "world"
}
```

Run a plan to confirm nothing changes:

```bash
terraform plan
```

Expected output:

```
No changes. Your infrastructure matches the configuration.
```

## Step 6 -- Clean Up

```bash
terraform destroy -auto-approve
```

## Common Mistakes

### Mismatching the resource configuration with the real resource

If your HCL sets `value = "hello"` but the real parameter has `value = "HELLO"`, the import will succeed but the subsequent plan will show an in-place update. Always make sure your HCL matches the actual resource attributes. Run `terraform plan` before applying to catch these mismatches.

### Confusing `id` in the import block with the Terraform resource ID

The `id` field in the `import` block is the cloud provider's identifier for the resource, not the Terraform address. For SSM Parameters, it is the parameter name (e.g., `/kata/imported-1`). For S3 buckets, it is the bucket name. Check the provider documentation for the correct import ID format for each resource type.

## Verify What You Learned

1. Confirm both parameters are in state:

```bash
terraform state list
```

Expected output:

```
aws_ssm_parameter.param["imported-1"]
aws_ssm_parameter.param["imported-2"]
```

2. Confirm no drift after removing import blocks:

```bash
terraform plan
```

Expected output:

```
No changes. Your infrastructure matches the configuration.
```

3. Inspect one of the imported resources:

```bash
terraform state show 'aws_ssm_parameter.param["imported-1"]' | grep value
```

Expected output:

```
    value             = "hello"
```

## What's Next

In the next exercise, you will dive into the raw state file itself. You will use `terraform state pull` to extract the state as JSON, inspect its internal structure (version, serial, lineage), and understand how Terraform tracks concurrency and change history.

## Reference

- [import Block Documentation](https://developer.hashicorp.com/terraform/language/import)
- [terraform import Command](https://developer.hashicorp.com/terraform/cli/commands/import)

## Additional Resources

- [Spacelift: Terraform Import Block Guide](https://spacelift.io/blog/terraform-import-block) -- complete walkthrough of the import block compared to the CLI command with practical examples
- [HashiCorp: Import Tutorial](https://developer.hashicorp.com/terraform/tutorials/state/state-import) -- official tutorial covering both CLI and declarative import step by step
- [env0: Terraform Import Existing Resources](https://www.env0.com/blog/terraform-import) -- practical guide with declarative import examples in real-world workflows

# 23. Remove from State Without Destroying

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 22 (Move a Resource to a Module with state mv)

## Learning Objectives

After completing this exercise, you will be able to:

- Remove a resource from Terraform state using `terraform state rm` while keeping it alive in the cloud
- Use the declarative `removed` block (Terraform 1.7+) as a code-reviewable alternative
- Verify that an unmanaged resource continues to exist in AWS after removal from state

## Why Remove Resources from State Without Destroying

There are situations where you need Terraform to stop managing a resource without deleting it. Common scenarios include:

- **Ownership transfer:** Another team or tool will take over management of the resource.
- **Migration:** You are splitting a monolithic Terraform configuration into smaller ones and need to move a resource's ownership.
- **Protection:** A resource was created by Terraform but must now live independently because deleting it would cause data loss.

The imperative approach (`terraform state rm`) modifies the state immediately. The declarative approach (`removed` block with `lifecycle { destroy = false }`) is preferred in team environments because it is visible in code reviews and version control -- the intent to stop managing a resource is documented right in the HCL.

## Step 1 -- Create the Resource

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

resource "aws_ssm_parameter" "keep_me" {
  name  = "/kata/keep-me"
  type  = "String"
  value = "important-data"
}
```

```bash
terraform init
terraform apply -auto-approve
```

## Step 2a -- Remove via CLI (Imperative Approach)

```bash
terraform state rm aws_ssm_parameter.keep_me
```

Expected output:

```
Removed aws_ssm_parameter.keep_me
Successfully removed 1 resource instance(s).
```

Now check the state:

```bash
terraform state list
```

Expected output (empty -- no resources managed):

```
(no output)
```

Check the plan:

```bash
terraform plan
```

**Scenario A -- Resource block still in code:** Terraform sees the resource block but no corresponding state entry. It will plan to create a new resource. To avoid this, remove the resource block from your HCL or comment it out.

**Scenario B -- Resource block removed from code:** Terraform shows "No changes" because neither state nor configuration references the resource.

Verify the resource still exists in AWS:

```bash
aws ssm get-parameter --name "/kata/keep-me" --query "Parameter.Value" --output text
```

Expected output:

```
important-data
```

## Step 2b -- Remove via `removed` Block (Declarative Approach)

If you followed Step 2a, re-create the resource first by restoring the resource block and running `terraform apply`.

Now replace the resource block with a `removed` block:

```hcl
# main.tf (replace the resource block with this)

removed {
  from = aws_ssm_parameter.keep_me
  lifecycle {
    destroy = false
  }
}
```

```bash
terraform apply -auto-approve
```

Expected output includes:

```
aws_ssm_parameter.keep_me: Removing from state...
aws_ssm_parameter.keep_me: Removal complete

Apply complete! Resources: 0 added, 0 changed, 0 destroyed.
```

Verify the resource still exists in AWS:

```bash
aws ssm get-parameter --name "/kata/keep-me" --query "Parameter.Value" --output text
```

Expected output:

```
important-data
```

## Step 3 -- Manual Cleanup

Since Terraform no longer manages this resource, you must clean it up manually:

```bash
aws ssm delete-parameter --name "/kata/keep-me"
```

## Common Mistakes

### Forgetting to remove the resource block after `state rm`

After running `terraform state rm`, if you leave the resource block in your HCL, Terraform will plan to create a new resource with the same configuration. Either remove the block or use the `removed` block approach instead, which handles both the state removal and the code cleanup in one step.

### Omitting `lifecycle { destroy = false }` in the `removed` block

A `removed` block without `lifecycle { destroy = false }` will destroy the resource in the cloud. This is the opposite of what you want. Always include the lifecycle directive when the goal is to keep the resource alive.

## Verify What You Learned

1. Confirm the resource is no longer in state:

```bash
terraform state list
```

Expected output:

```
(no output)
```

2. Confirm the resource still exists in AWS:

```bash
aws ssm get-parameter --name "/kata/keep-me" --query "Parameter.Value" --output text
```

Expected output:

```
important-data
```

3. Confirm Terraform shows no planned changes:

```bash
terraform plan
```

Expected output:

```
No changes. Your infrastructure matches the configuration.
```

## What's Next

In the next exercise, you will learn about the declarative `import` block -- a code-reviewable, plan-previewed way to bring existing resources into Terraform state, replacing the imperative `terraform import` command.

## Reference

- [terraform state rm Command](https://developer.hashicorp.com/terraform/cli/commands/state/rm)
- [removed Block Documentation](https://developer.hashicorp.com/terraform/language/state/remove)

## Additional Resources

- [HashiCorp: Manage Resources in Terraform State Tutorial](https://developer.hashicorp.com/terraform/tutorials/state/state-cli) -- hands-on tutorial for managing resources in state with CLI commands
- [Spacelift: Terraform State Management](https://spacelift.io/blog/terraform-state) -- walkthrough of state operations including safe resource removal

# 27. prevent_destroy

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 26 (create_before_destroy)

## Learning Objectives

After completing this exercise, you will be able to:

- Protect critical resources from accidental deletion using `prevent_destroy`
- Distinguish between operations that `prevent_destroy` blocks and those it allows
- Follow the two-step process to intentionally destroy a protected resource when needed

## Why prevent_destroy

Terraform makes it easy to destroy infrastructure -- sometimes too easy. A single `terraform destroy` or an accidental removal of a resource block from your code can wipe out a production database, a storage bucket with years of data, or a parameter store entry that dozens of services depend on.

`prevent_destroy` is a guardrail. When set to `true`, Terraform will refuse to execute any plan that includes destroying that resource. It does not prevent updates -- you can still change the resource's value, tags, or other in-place attributes. It only blocks destruction.

This is particularly valuable for:

- **Databases** (RDS, DynamoDB) that contain irreplaceable data
- **S3 buckets** with audit logs or backups
- **Encryption keys** (KMS) that other resources depend on
- **Configuration parameters** that applications read at boot time

The protection is intentionally easy to remove -- you just set `prevent_destroy = false` and apply. This ensures it is a speed bump, not a locked door: it forces a deliberate two-step process instead of allowing accidental one-step deletion.

## Step 1 -- Create a Protected Resource

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

resource "aws_ssm_parameter" "critical" {
  name  = "/kata/critical-config"
  type  = "String"
  value = "do-not-delete"

  lifecycle {
    prevent_destroy = true
  }
}
```

```bash
terraform init
terraform apply -auto-approve
```

## Step 2 -- Attempt Destruction (Blocked)

Try to destroy the resource:

```bash
terraform destroy
```

Expected error:

```
Error: Instance cannot be destroyed

  on main.tf line X:
   X: resource "aws_ssm_parameter" "critical" {

Resource aws_ssm_parameter.critical has lifecycle.prevent_destroy set, but the
plan calls for this resource to be destroyed. To avoid this error and continue
with the plan, either disable lifecycle.prevent_destroy or reduce the scope of
the destroy using the -target flag.
```

The resource is safe. Terraform refused to proceed.

## Step 3 -- Attempt Removal from Code (Also Blocked)

Comment out or delete the entire resource block from `main.tf`, then run:

```bash
terraform plan
```

Expected error:

```
Error: Instance cannot be destroyed

Resource aws_ssm_parameter.critical has lifecycle.prevent_destroy set, but the
plan calls for this resource to be destroyed.
```

Even removing the code does not bypass the protection. Terraform sees the resource in state, sees it is gone from code, plans to destroy it, and then the lifecycle rule blocks the plan.

## Step 4 -- In-Place Updates Still Work

Restore the resource block and change only the value:

```hcl
resource "aws_ssm_parameter" "critical" {
  name  = "/kata/critical-config"
  type  = "String"
  value = "updated-value"

  lifecycle {
    prevent_destroy = true
  }
}
```

```bash
terraform apply -auto-approve
```

Expected output includes:

```
aws_ssm_parameter.critical: Modifying...
aws_ssm_parameter.critical: Modifications complete after 1s

Apply complete! Resources: 0 added, 1 changed, 0 destroyed.
```

The update succeeds because `prevent_destroy` only blocks destruction, not modification.

## Step 5 -- Intentionally Destroy (Two-Step Process)

When you genuinely need to destroy a protected resource, follow these two steps:

**Step 5a** -- Disable the protection:

```hcl
resource "aws_ssm_parameter" "critical" {
  name  = "/kata/critical-config"
  type  = "String"
  value = "updated-value"

  lifecycle {
    prevent_destroy = false
  }
}
```

```bash
terraform apply -auto-approve
```

**Step 5b** -- Destroy the resource:

```bash
terraform destroy -auto-approve
```

Expected output:

```
aws_ssm_parameter.critical: Destroying...
aws_ssm_parameter.critical: Destruction complete after 1s

Destroy complete! Resources: 1 destroyed.
```

## Common Mistakes

### Assuming prevent_destroy protects against `terraform state rm`

`prevent_destroy` only prevents destruction through `terraform destroy` or plan-based deletion. It does not prevent you from running `terraform state rm` to remove the resource from state. After a `state rm`, Terraform forgets about the resource entirely, and the lifecycle rule no longer applies. The resource continues to exist in AWS but is no longer protected by Terraform.

### Thinking prevent_destroy blocks all changes

`prevent_destroy` only blocks operations that would destroy the resource. In-place updates, tag changes, and attribute modifications all work normally. If you need to prevent all changes (including updates), use `ignore_changes = all` instead -- covered in exercise 28.

## Verify What You Learned

1. Confirm `terraform destroy` is blocked:

```bash
terraform destroy 2>&1 | grep "Instance cannot be destroyed"
```

Expected output:

```
Error: Instance cannot be destroyed
```

2. Confirm in-place updates work (change the value and apply):

```bash
terraform apply -auto-approve
```

Expected output includes:

```
Apply complete! Resources: 0 added, 1 changed, 0 destroyed.
```

3. Confirm the two-step destroy process works (set `prevent_destroy = false`, then destroy):

```bash
terraform destroy -auto-approve
```

Expected output:

```
Destroy complete! Resources: 1 destroyed.
```

## What's Next

In the next exercise, you will learn about `ignore_changes` -- a lifecycle rule that tells Terraform to ignore external modifications to specific attributes, allowing controlled drift for resources managed by both Terraform and external processes.

## Reference

- [lifecycle Meta-Argument](https://developer.hashicorp.com/terraform/language/meta-arguments/lifecycle)

## Additional Resources

- [HashiCorp: Resource Lifecycle Tutorial](https://developer.hashicorp.com/terraform/tutorials/state/resource-lifecycle) -- official tutorial showing how to protect critical resources with `prevent_destroy` step by step
- [Spacelift: Terraform Resource Lifecycle Guide](https://spacelift.io/blog/terraform-resource-lifecycle) -- practical guide to `prevent_destroy` with examples of protecting databases and S3 buckets
- [DEV Community: Understanding Terraform Lifecycle Block](https://dev.to/pwd9000/terraform-understanding-the-lifecycle-block-4f6e) -- walkthrough with real-world scenarios of protecting resources against accidental deletion

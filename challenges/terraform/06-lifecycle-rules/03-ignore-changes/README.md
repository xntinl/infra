# 28. ignore_changes

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 27 (prevent_destroy)

## Learning Objectives

After completing this exercise, you will be able to:

- Use `ignore_changes` to prevent Terraform from reverting external modifications to specific attributes
- Distinguish between ignoring specific attributes and ignoring all attributes with `ignore_changes = all`
- Manage controlled drift for resources shared between Terraform and external processes

## Why ignore_changes

In an ideal world, Terraform is the only thing that modifies your infrastructure. In the real world, that is rarely the case. Auto-scaling groups adjust instance counts. CI/CD pipelines update container image tags. Operations teams change configuration values through the console during incidents. External scripts rotate secrets in parameter store.

When Terraform detects these external modifications, it reports drift and plans to revert the resource to the HCL-defined state. This is usually the right behavior -- but not always. If an auto-scaler set the desired count to 5 because of high traffic, you do not want Terraform to revert it to 2 on the next apply.

`ignore_changes` lets you selectively exclude attributes from drift detection. Terraform still manages the resource -- it creates it, it can destroy it, it tracks other attributes -- but it ignores changes to the listed attributes, whether those changes come from external processes or from edits to your own HCL.

The `ignore_changes = all` variant takes this further: Terraform only manages creation and destruction of the resource, ignoring every attribute change in between. This is useful for resources that are fully managed by external processes after initial creation.

## Step 1 -- Create a Resource with ignore_changes

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

resource "aws_ssm_parameter" "externally_modified" {
  name  = "/kata/external-config"
  type  = "String"
  value = "initial-value"

  tags = {
    ManagedBy = "terraform"
  }

  lifecycle {
    ignore_changes = [value, tags]
  }
}
```

```bash
terraform init
terraform apply -auto-approve
```

## Step 2 -- Modify the Resource Externally

Change the parameter value using the AWS CLI:

```bash
aws ssm put-parameter --name "/kata/external-config" --value "modified-externally" --overwrite
```

Verify the change took effect:

```bash
aws ssm get-parameter --name "/kata/external-config" --query "Parameter.Value" --output text
```

Expected output:

```
modified-externally
```

## Step 3 -- Confirm Terraform Ignores the Change

```bash
terraform plan
```

Expected output:

```
No changes. Your infrastructure matches the configuration.
```

Even though the real value is "modified-externally" and the HCL says "initial-value," Terraform reports no drift because `value` is in the `ignore_changes` list.

## Step 4 -- Change the Value in HCL (Also Ignored)

Update the value in `main.tf`:

```hcl
  value = "new-hcl-value"
```

```bash
terraform plan
```

Expected output:

```
No changes. Your infrastructure matches the configuration.
```

Changes to ignored attributes in HCL are also suppressed. Terraform will not try to update the real resource to match the new HCL value.

## Step 5 -- Remove an Attribute from ignore_changes (Drift Appears)

Update the lifecycle block to stop ignoring `value`:

```hcl
  lifecycle {
    ignore_changes = [tags]
  }
```

```bash
terraform plan
```

Now Terraform detects the drift:

```
  # aws_ssm_parameter.externally_modified will be updated in-place
  ~ resource "aws_ssm_parameter" "externally_modified" {
      ~ value = "modified-externally" -> "new-hcl-value"
        ...
    }

Plan: 0 to add, 1 to change, 0 to destroy.
```

Terraform will revert the value to what the HCL specifies because `value` is no longer ignored.

## Step 6 -- Try ignore_changes = all

Replace the lifecycle block:

```hcl
  lifecycle {
    ignore_changes = all
  }
```

```bash
terraform plan
```

Expected output:

```
No changes. Your infrastructure matches the configuration.
```

With `all`, no attribute changes are detected -- neither value, nor tags, nor anything else. Terraform only manages creation and destruction.

## Step 7 -- Clean Up

Restore `ignore_changes = []` or remove the lifecycle block, then destroy:

```bash
terraform destroy -auto-approve
```

## Common Mistakes

### Using ignore_changes as a permanent fix for misconfigured HCL

If your HCL does not match the real resource and you add `ignore_changes` to suppress the drift, you are hiding the problem rather than fixing it. Use `ignore_changes` only when external modifications are intentional and expected. For unintentional drift, fix the HCL to match reality.

### Forgetting that ignore_changes affects HCL changes too

Once an attribute is in `ignore_changes`, Terraform ignores changes from all sources -- including your own code. If you update the value in HCL and expect Terraform to apply it, it will not. You must remove the attribute from the ignore list first, apply the change, and then add it back.

## Verify What You Learned

1. Modify the parameter externally and confirm Terraform ignores it:

```bash
aws ssm put-parameter --name "/kata/external-config" --value "external-change" --overwrite
terraform plan
```

Expected output:

```
No changes. Your infrastructure matches the configuration.
```

2. Remove `value` from `ignore_changes` and confirm drift is detected:

```bash
terraform plan
```

Expected output includes:

```
Plan: 0 to add, 1 to change, 0 to destroy.
```

3. Set `ignore_changes = all` and confirm all drift is suppressed:

```bash
terraform plan
```

Expected output:

```
No changes. Your infrastructure matches the configuration.
```

4. Confirm the resource can still be destroyed (ignore_changes does not prevent destruction):

```bash
terraform destroy -auto-approve
```

Expected output:

```
Destroy complete! Resources: 1 destroyed.
```

## What's Next

In the next exercise, you will learn about `replace_triggered_by` -- a lifecycle rule that forces a resource to be replaced when a different resource or attribute changes, creating explicit replacement dependency chains.

## Reference

- [lifecycle Meta-Argument](https://developer.hashicorp.com/terraform/language/meta-arguments/lifecycle)

## Additional Resources

- [Spacelift: Terraform ignore_changes Guide](https://spacelift.io/blog/terraform-ignore-changes) -- detailed guide to `ignore_changes` with practical examples of managing drift in specific attributes
- [HashiCorp: Resource Lifecycle Tutorial](https://developer.hashicorp.com/terraform/tutorials/state/resource-lifecycle) -- official tutorial with examples of coexistence between Terraform and external modifications
- [DEV Community: Understanding Terraform Lifecycle Block](https://dev.to/pwd9000/terraform-understanding-the-lifecycle-block-4f6e) -- learn when and why to use `ignore_changes` with real-world use cases

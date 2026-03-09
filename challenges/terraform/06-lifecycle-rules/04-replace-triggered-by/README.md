# 29. replace_triggered_by

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 28 (ignore_changes)

## Learning Objectives

After completing this exercise, you will be able to:

- Use `replace_triggered_by` to force resource replacement when a dependency changes
- Reference both specific attributes and entire resources as replacement triggers
- Combine `replace_triggered_by` with `null_resource` to create coordination triggers

## Why replace_triggered_by

Terraform replaces a resource only when one of its own attributes changes in a way that forces recreation. But sometimes you need a resource to be replaced because something else changed -- a configuration parameter it reads at startup, a certificate it depends on, or a build artifact it was created from.

Without `replace_triggered_by`, these cross-resource replacement dependencies are invisible to Terraform. The configuration parameter changes, but the resource that reads it keeps running with the old value because Terraform does not know the two are related.

`replace_triggered_by` makes these dependencies explicit. You list the resources or attributes that should trigger replacement, and when any of them change, Terraform marks the resource for destruction and recreation -- even though none of its own attributes changed.

This is especially useful for:

- **Application servers** that read configuration at boot time and need to be recycled when the config changes
- **Lambda functions** that should be redeployed when a shared layer is updated
- **Containers** that bake in environment variables and need replacement when those variables change

## Step 1 -- Create the Configuration

```hcl
# main.tf

terraform {
  required_version = ">= 1.7"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    null = {
      source  = "hashicorp/null"
      version = "~> 3.0"
    }
  }
}

provider "aws" {
  region = "us-east-1"
}

variable "config_version" {
  default = "v1"
}

# The "source of truth" configuration parameter
resource "aws_ssm_parameter" "config" {
  name  = "/kata/trigger-config"
  type  = "String"
  value = var.config_version
}

# This resource is replaced when the config parameter's value changes
resource "aws_ssm_parameter" "dependent" {
  name  = "/kata/dependent-resource"
  type  = "String"
  value = "I depend on config version"

  lifecycle {
    replace_triggered_by = [
      aws_ssm_parameter.config.value
    ]
  }
}

# A null_resource used as a coordination trigger
resource "null_resource" "timestamp_trigger" {
  triggers = {
    version = var.config_version
  }
}

# This resource is replaced when the entire null_resource changes
resource "aws_ssm_parameter" "triggered_by_null" {
  name  = "/kata/null-triggered"
  type  = "String"
  value = "triggered by null_resource"

  lifecycle {
    replace_triggered_by = [
      null_resource.timestamp_trigger
    ]
  }
}
```

## Step 2 -- Apply the Initial Configuration

```bash
terraform init
terraform apply -auto-approve
```

Confirm all resources exist:

```bash
terraform state list
```

Expected output:

```
aws_ssm_parameter.config
aws_ssm_parameter.dependent
aws_ssm_parameter.triggered_by_null
null_resource.timestamp_trigger
```

## Step 3 -- Trigger Replacement by Changing the Version

```bash
terraform plan -var="config_version=v2"
```

Examine the plan output carefully:

**`config`** -- updated in-place (its own `value` attribute changed):

```
  # aws_ssm_parameter.config will be updated in-place
  ~ resource "aws_ssm_parameter" "config" {
      ~ value = "v1" -> "v2"
        ...
    }
```

**`dependent`** -- replaced (triggered by `config.value` changing):

```
  # aws_ssm_parameter.dependent will be replaced due to changes in replace_triggered_by
-/+ resource "aws_ssm_parameter" "dependent" {
        name  = "/kata/dependent-resource"
        value = "I depend on config version"
        ...
    }
```

**`triggered_by_null`** -- replaced (triggered by `null_resource.timestamp_trigger` being recreated):

```
  # aws_ssm_parameter.triggered_by_null will be replaced due to changes in replace_triggered_by
-/+ resource "aws_ssm_parameter" "triggered_by_null" {
        name  = "/kata/null-triggered"
        value = "triggered by null_resource"
        ...
    }
```

**`timestamp_trigger`** -- replaced (its own `triggers` map changed):

```
  # null_resource.timestamp_trigger must be replaced
-/+ resource "null_resource" "timestamp_trigger" {
      ~ triggers = {
          ~ "version" = "v1" -> "v2"
        }
        ...
    }
```

## Step 4 -- Apply the Change

```bash
terraform apply -var="config_version=v2" -auto-approve
```

## Step 5 -- Verify Without replace_triggered_by (Thought Experiment)

Without `replace_triggered_by`, the `dependent` resource would not be affected by a change to `config.value`. Its own attributes (`name` and `value`) do not change, so Terraform would leave it alone. The trigger creates an explicit replacement dependency that would not otherwise exist.

## Step 6 -- Clean Up

```bash
terraform destroy -var="config_version=v2" -auto-approve
```

## Common Mistakes

### Referencing a computed attribute that changes on every apply

If you reference an attribute that changes on every apply (like a timestamp or a random value), the dependent resource will be replaced on every apply too. This can lead to unnecessary churn. Make sure the trigger attribute only changes when you actually want replacement to happen.

### Confusing replace_triggered_by with depends_on

`depends_on` controls the order of operations -- it ensures one resource is created before another. It does not force replacement. `replace_triggered_by` forces replacement when the referenced resource or attribute changes, regardless of creation order. They solve different problems and can be used together.

## Verify What You Learned

1. Plan with a version change and confirm the dependent resources are marked for replacement:

```bash
terraform plan -var="config_version=v3" 2>&1 | grep "will be replaced"
```

Expected output:

```
  # aws_ssm_parameter.dependent will be replaced due to changes in replace_triggered_by
  # aws_ssm_parameter.triggered_by_null will be replaced due to changes in replace_triggered_by
```

2. Confirm `config` is only updated in-place (not replaced):

```bash
terraform plan -var="config_version=v3" 2>&1 | grep "will be updated"
```

Expected output:

```
  # aws_ssm_parameter.config will be updated in-place
```

3. Apply and confirm all resources reach their final state:

```bash
terraform apply -var="config_version=v3" -auto-approve
aws ssm get-parameter --name "/kata/trigger-config" --query "Parameter.Value" --output text
```

Expected output:

```
v3
```

4. Run a plan with no changes and confirm everything is stable:

```bash
terraform plan -var="config_version=v3"
```

Expected output:

```
No changes. Your infrastructure matches the configuration.
```

## Section 06 Summary -- Lifecycle Rules

Across these four exercises, you have learned to control how Terraform manages the lifecycle of individual resources:

| Exercise | Rule | Purpose |
|----------|------|---------|
| 26 | `create_before_destroy` | Reverse replacement order to minimize downtime |
| 27 | `prevent_destroy` | Block accidental deletion of critical resources |
| 28 | `ignore_changes` | Allow controlled drift on specific attributes |
| 29 | `replace_triggered_by` | Force replacement when a dependency changes |

Key takeaways:

- All four rules live inside the `lifecycle` block, which is available on every resource.
- `create_before_destroy` and `replace_triggered_by` affect replacement behavior. `prevent_destroy` and `ignore_changes` affect what Terraform is allowed to do.
- These rules are about pragmatism. Real infrastructure has constraints that the default Terraform behavior does not account for -- unique naming, external processes, boot-time configuration reads, and data protection requirements. Lifecycle rules let you model those constraints.
- Lifecycle rules are static -- they cannot reference variables or expressions. They must be known at configuration parse time.

## What's Next

In the next section, you will learn about data sources -- `data` blocks that let you query existing infrastructure and use the results in your Terraform configuration without managing those resources.

## Reference

- [lifecycle Meta-Argument](https://developer.hashicorp.com/terraform/language/meta-arguments/lifecycle)
- [null_resource](https://registry.terraform.io/providers/hashicorp/null/latest/docs/resources/resource)

## Additional Resources

- [HashiCorp: Resource Lifecycle Tutorial](https://developer.hashicorp.com/terraform/tutorials/state/resource-lifecycle) -- official tutorial covering all lifecycle rules including replace_triggered_by
- [DEV Community: Understanding Terraform Lifecycle Block](https://dev.to/pwd9000/terraform-understanding-the-lifecycle-block-4f6e) -- practical walkthrough of the lifecycle block including `replace_triggered_by` with real scenarios
- [Spacelift: Terraform Resource Lifecycle Guide](https://spacelift.io/blog/terraform-resource-lifecycle) -- comprehensive guide with examples of `replace_triggered_by` and `null_resource` as a coordination trigger

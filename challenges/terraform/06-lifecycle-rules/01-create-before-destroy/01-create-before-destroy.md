# 26. create_before_destroy

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 25 (State Pull, Inspect, and Push)

## Learning Objectives

After completing this exercise, you will be able to:

- Use `create_before_destroy` to change the order in which Terraform replaces resources
- Compare the default destroy-then-create behavior with create-before-destroy side by side
- Identify scenarios where `create_before_destroy` prevents downtime during resource replacement

## Why create_before_destroy

When Terraform needs to replace a resource -- because a change to an attribute forces recreation rather than an in-place update -- the default behavior is destroy-then-create: Terraform deletes the old resource first, then creates the new one. During the gap between destruction and creation, the resource does not exist. For many resources, this gap means downtime.

`create_before_destroy` inverts the order. Terraform creates the new resource first, verifies it is healthy, and only then destroys the old one. This is the same principle behind blue-green deployments: the new version is fully running before the old version is retired.

This matters most for resources that other systems depend on -- load balancer target groups, DNS records, security groups, or configuration parameters that applications read at startup. If those resources disappear even briefly, dependent systems can fail.

## Step 1 -- Create Two Parameters with Different Lifecycle Strategies

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

variable "config_version" {
  default = "v1"
}

# Default lifecycle: destroy first, then create
resource "aws_ssm_parameter" "default_lifecycle" {
  name  = "/kata/default-${var.config_version}"
  type  = "String"
  value = "config-${var.config_version}"
}

# Safe lifecycle: create first, then destroy
resource "aws_ssm_parameter" "safe_lifecycle" {
  name  = "/kata/safe-${var.config_version}"
  type  = "String"
  value = "config-${var.config_version}"

  lifecycle {
    create_before_destroy = true
  }
}
```

## Step 2 -- Apply the Initial Configuration

```bash
terraform init
terraform apply -auto-approve
```

Confirm both parameters exist:

```bash
aws ssm get-parameter --name "/kata/default-v1" --query "Parameter.Value" --output text
aws ssm get-parameter --name "/kata/safe-v1" --query "Parameter.Value" --output text
```

Expected output for both:

```
config-v1
```

## Step 3 -- Trigger Replacement and Compare

Change the version to force both resources to be replaced (the name attribute forces recreation):

```bash
terraform plan -var="config_version=v2"
```

Examine the plan output carefully. You will see two different replacement strategies:

**Default lifecycle (`default_lifecycle`):**

```
  # aws_ssm_parameter.default_lifecycle must be replaced
-/+ resource "aws_ssm_parameter" "default_lifecycle" {
      ~ name  = "/kata/default-v1" -> "/kata/default-v2" # forces replacement
      ...
    }
```

The `-/+` prefix means destroy then create (default order).

**Safe lifecycle (`safe_lifecycle`):**

```
  # aws_ssm_parameter.safe_lifecycle must be replaced
+/- resource "aws_ssm_parameter" "safe_lifecycle" {
      ~ name  = "/kata/safe-v1" -> "/kata/safe-v2" # forces replacement
      ...
    }
```

The `+/-` prefix means create then destroy (reversed order due to `create_before_destroy`).

## Step 4 -- Apply and Observe

```bash
terraform apply -var="config_version=v2" -auto-approve
```

During the apply, notice the order of operations:

- `default_lifecycle`: Terraform destroys `/kata/default-v1` first, then creates `/kata/default-v2`.
- `safe_lifecycle`: Terraform creates `/kata/safe-v2` first, then destroys `/kata/safe-v1`.

## Step 5 -- Clean Up

```bash
terraform destroy -var="config_version=v2" -auto-approve
```

## Common Mistakes

### Expecting create_before_destroy to work with unique constraints

Some resources have uniqueness constraints (e.g., a security group rule with the same port/protocol/CIDR cannot exist twice). If Terraform tries to create the new resource before destroying the old one and both have the same unique key, the creation will fail. In these cases, you may need to adjust naming or use a different strategy.

### Using create_before_destroy on resources with no replacement scenario

If a resource only ever gets updated in-place (never replaced), `create_before_destroy` has no effect. It only matters when an attribute change forces recreation. Check the provider documentation to know which attributes trigger replacement.

## Verify What You Learned

1. Plan with a version change and look for the replacement order indicators:

```bash
terraform plan -var="config_version=v3" 2>&1 | grep -E '^\s+#.*must be replaced' -A1
```

2. Confirm the plan symbol for `default_lifecycle` is `-/+` (destroy then create):

```bash
terraform plan -var="config_version=v3" 2>&1 | grep -E '(-/\+|[+]/-)'
```

Expected output includes both patterns:

```
-/+ resource "aws_ssm_parameter" "default_lifecycle" {
+/- resource "aws_ssm_parameter" "safe_lifecycle" {
```

3. After applying, confirm only the new versions exist:

```bash
terraform apply -var="config_version=v3" -auto-approve
aws ssm get-parameter --name "/kata/default-v3" --query "Parameter.Value" --output text
aws ssm get-parameter --name "/kata/safe-v3" --query "Parameter.Value" --output text
```

Expected output for both:

```
config-v3
```

## What's Next

In the next exercise, you will learn about `prevent_destroy` -- a lifecycle rule that acts as a safety net against accidental deletion of critical resources like databases or storage buckets.

## Reference

- [lifecycle Meta-Argument](https://developer.hashicorp.com/terraform/language/meta-arguments/lifecycle)
- [Resource Behavior](https://developer.hashicorp.com/terraform/language/resources/behavior)

## Additional Resources

- [HashiCorp: Resource Lifecycle Tutorial](https://developer.hashicorp.com/terraform/tutorials/state/resource-lifecycle) -- official tutorial covering `create_before_destroy` and other lifecycle rules with practical examples
- [Spacelift: Terraform Resource Lifecycle Guide](https://spacelift.io/blog/terraform-resource-lifecycle) -- detailed walkthrough of all lifecycle rules with diagrams of the replacement flow
- [DEV Community: Understanding Terraform Lifecycle Block](https://dev.to/pwd9000/terraform-understanding-the-lifecycle-block-4f6e) -- practical guide with visual examples of how `create_before_destroy` minimizes downtime

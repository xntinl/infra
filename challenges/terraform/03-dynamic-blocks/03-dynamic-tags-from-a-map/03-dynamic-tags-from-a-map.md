# 15. Dynamic Tags from a Map

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 14 (Dynamic IAM Policy Statements)

## Learning Objectives

After completing this exercise, you will be able to:

- Use `merge()` to combine common tags with resource-specific tags
- Apply tags as a flat map on standard resources and as dynamic `tag` blocks on ASGs
- Understand why Auto Scaling Groups require a different tagging format than other AWS resources

## Why Dynamic Tags

Most AWS resources accept tags as a simple `map(string)`:

```hcl
tags = {
  Project     = "myapp"
  Environment = "dev"
}
```

Auto Scaling Groups are an exception. They require each tag to be a separate block with three fields: `key`, `value`, and `propagate_at_launch` (which controls whether launched EC2 instances inherit the tag). If you have ten tags, you need ten blocks.

Writing those blocks by hand defeats the purpose of centralized tag management. A `dynamic "tag"` block solves this by iterating over the same map you use everywhere else. Combined with `merge()`, you get a single tagging strategy that works uniformly across all resource types.

## Step 1 -- Define Tag Variables

Create `variables.tf`:

```hcl
variable "project" {
  description = "Project name used in tags and resource naming"
  default     = "exercise"
}

variable "environment" {
  description = "Deployment environment"
  default     = "dev"
}

variable "extra_tags" {
  description = "Additional tags to apply to all resources"
  type        = map(string)
  default = {
    CostCenter = "CC-1234"
    Team       = "Platform"
  }
}
```

## Step 2 -- Merge Common and Extra Tags

Create `main.tf`:

```hcl
locals {
  # Tags that every resource in this project should carry
  common_tags = {
    Project     = var.project
    Environment = var.environment
    ManagedBy   = "terraform"
  }

  # Merged result: common tags + any extra tags from the variable
  all_tags = merge(local.common_tags, var.extra_tags)
}
```

The `merge()` function combines multiple maps. If a key exists in both, the last map wins. This lets `extra_tags` override a common tag when needed (for example, overriding `Environment` for a specific use case).

## Step 3 -- Apply Tags to a Standard Resource

Add an SSM Parameter that uses the flat map:

```hcl
resource "aws_ssm_parameter" "example" {
  name  = "/exercise/example"
  type  = "String"
  value = "hello"
  tags  = local.all_tags
}
```

This is straightforward -- `tags` accepts a `map(string)` directly.

## Step 4 -- Apply Tags to an Auto Scaling Group with Dynamic Blocks

Add an ASG that uses a dynamic `tag` block:

```hcl
resource "aws_autoscaling_group" "example" {
  min_size         = 0
  max_size         = 0
  desired_capacity = 0

  dynamic "tag" {
    for_each = local.all_tags
    content {
      key                 = tag.key
      value               = tag.value
      propagate_at_launch = true
    }
  }

  launch_template {
    id      = "lt-placeholder"
    version = "$Latest"
  }
}

output "all_tags" {
  description = "Complete set of tags applied to resources"
  value       = local.all_tags
}
```

When iterating over a map, `tag.key` is the map key (e.g., `"Project"`) and `tag.value` is the map value (e.g., `"exercise"`). Each iteration produces one `tag` block.

## Step 5 -- Add a Tag and Verify Both Resources

Add a new entry to `extra_tags`:

```hcl
variable "extra_tags" {
  type    = map(string)
  default = {
    CostCenter = "CC-1234"
    Team       = "Platform"
    Owner      = "ops@example.com"
  }
}
```

Run `terraform plan`. Both the SSM Parameter and the ASG should show the new tag -- a single change in the variable propagates to all resources.

## Step 6 -- Edge Case: Override a Common Tag

Set `extra_tags` to override `ManagedBy`:

```hcl
default = {
  CostCenter = "CC-1234"
  Team       = "Platform"
  ManagedBy  = "pulumi"
}
```

Run `terraform plan`. The merged map should show `ManagedBy = "pulumi"` because `merge()` gives priority to the last map. This is important to understand -- `extra_tags` can silently override common tags. If this is undesirable, validate inputs or reverse the merge order.

## Common Mistakes

### Forgetting `propagate_at_launch`

```hcl
dynamic "tag" {
  for_each = local.all_tags
  content {
    key   = tag.key
    value = tag.value
    # Missing propagate_at_launch -- Terraform will error
  }
}
```

The `propagate_at_launch` attribute is required on ASG tags. Omitting it causes a validation error. Always set it, typically to `true` so instances inherit the ASG's tags.

### Using the flat `tags` argument on an ASG

```hcl
# Wrong -- ASGs do not support the flat tags map in the same way
resource "aws_autoscaling_group" "example" {
  tags = local.all_tags  # This will not work as expected
}
```

While newer versions of the AWS provider support a `tag` attribute on ASGs, the block format with `propagate_at_launch` is the standard pattern. Use `dynamic "tag"` blocks for clarity and control.

## Verify What You Learned

Run the plan with the original five tags (3 common + 2 extra):

```bash
terraform plan
```

```
Terraform will perform the following actions:

  # aws_ssm_parameter.example will be created
  + resource "aws_ssm_parameter" "example" {
      + name  = "/exercise/example"
      + tags  = {
          + "CostCenter"  = "CC-1234"
          + "Environment" = "dev"
          + "ManagedBy"   = "terraform"
          + "Project"     = "exercise"
          + "Team"        = "Platform"
        }
      + type  = "String"
      + value = "hello"
    }

  # aws_autoscaling_group.example will be created
  + resource "aws_autoscaling_group" "example" {
      + tag {
          + key                 = "CostCenter"
          + propagate_at_launch = true
          + value               = "CC-1234"
        }
      + tag {
          + key                 = "Environment"
          + propagate_at_launch = true
          + value               = "dev"
        }
      + tag {
          + key                 = "ManagedBy"
          + propagate_at_launch = true
          + value               = "terraform"
        }
      + tag {
          + key                 = "Project"
          + propagate_at_launch = true
          + value               = "exercise"
        }
      + tag {
          + key                 = "Team"
          + propagate_at_launch = true
          + value               = "Platform"
        }
    }

Plan: 2 to add, 0 to change, 0 to destroy.
```

Verify tag counts:

```bash
terraform plan | grep -c "CostCenter"
```

```
2
```

Both resources carry the same five tags.

## Section 03 Summary

You completed 3 exercises covering:

1. **Dynamic Ingress/Egress Rules** -- generated security group rules from a map of typed objects, eliminating repetitive `ingress` blocks
2. **Dynamic IAM Policy Statements** -- nested dynamic blocks to build IAM policies with optional conditions, using the ternary-list pattern for conditional sub-blocks
3. **Dynamic Tags from a Map** -- unified tagging across standard resources (flat map) and ASGs (dynamic `tag` blocks) using `merge()`

Key takeaways:

- Dynamic blocks replace hand-written repetition inside a resource; they iterate over a collection and generate one sub-block per element
- Use maps (not lists) with dynamic blocks to get stable, named keys
- Nest dynamic blocks with a ternary `for_each` to conditionally generate sub-blocks
- The `merge()` function combines multiple tag maps into a single source of truth
- Not every repeated block needs to be dynamic -- static blocks are fine when the count is fixed and small

## What's Next

In the next section you will move from dynamic blocks (which generate sub-blocks inside a resource) to `for_each` and `count` (which generate entire resources). Section 04 covers patterns for creating multiple resources from lists, maps, YAML files, and conditional flags.

## Reference

- [Dynamic Blocks](https://developer.hashicorp.com/terraform/language/expressions/dynamic-blocks)
- [merge Function](https://developer.hashicorp.com/terraform/language/functions/merge)
- [aws_autoscaling_group Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/autoscaling_group)

## Additional Resources

- [Applying Common Tags to Resources Using Maps](https://tinfoilcipher.co.uk/2021/05/05/terraform-applying-common-tags-to-resources-using-maps/) -- practical tutorial on common tag patterns with merge() and dynamic blocks for ASGs
- [Understanding Terraform Dynamic Blocks](https://dev.to/pwd9000/terraform-understanding-dynamic-blocks-6f9) -- DEV Community guide explaining dynamic blocks with clear examples including the tags use case
- [Terraform Merge Function](https://spacelift.io/blog/terraform-merge-function) -- guide on merge() for combining common tags with resource-specific or environment-specific tags

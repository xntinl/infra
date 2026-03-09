# 13. Dynamic Ingress/Egress Rules

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 12 (previous section)

## Learning Objectives

After completing this exercise, you will be able to:

- Use `dynamic` blocks to generate repeating sub-blocks inside a resource
- Iterate over a `map(object({...}))` to produce typed, named rules
- Access `<label>.key` and `<label>.value` inside a dynamic block's `content`

## Why Dynamic Blocks

Security groups often need dozens of ingress rules. Writing each one by hand creates maintenance headaches: you have to scroll through walls of nearly identical `ingress` blocks, and every new rule means copying, pasting, and tweaking. If the structure of a rule changes (say, adding a `description` field), you have to touch every block.

A `dynamic` block solves this by iterating over a collection and generating one sub-block per element. The rules themselves live in a variable -- a single source of truth -- and the resource definition stays compact regardless of how many rules you add. This is the first and most common use case for dynamic blocks.

## How `dynamic` Blocks Work

A `dynamic` block replaces a repeated nested block inside a resource. It has two parts:

1. **`for_each`** -- the collection to iterate over (a map or set).
2. **`content`** -- the body of each generated block, where you reference the current element via `<label>.value`.

When iterating over a map, `<label>.key` gives you the map key and `<label>.value` gives you the value. This makes maps ideal for named rules.

## Step 1 -- Define the Ingress Rules Variable

Create `variables.tf`:

```hcl
variable "ingress_rules" {
  description = "Map of ingress rules keyed by a human-readable name"
  type = map(object({
    port        = number
    protocol    = string
    cidr_blocks = list(string)
    description = string
  }))
  default = {
    http = {
      port        = 80
      protocol    = "tcp"
      cidr_blocks = ["0.0.0.0/0"]
      description = "Allow HTTP"
    }
    https = {
      port        = 443
      protocol    = "tcp"
      cidr_blocks = ["0.0.0.0/0"]
      description = "Allow HTTPS"
    }
    ssh = {
      port        = 22
      protocol    = "tcp"
      cidr_blocks = ["10.0.0.0/8"]
      description = "Allow SSH from private network"
    }
    app = {
      port        = 8080
      protocol    = "tcp"
      cidr_blocks = ["10.0.0.0/8", "172.16.0.0/12"]
      description = "Allow app traffic"
    }
  }
}
```

Using `map(object({...}))` instead of a plain list gives each rule a stable key (`http`, `ssh`, etc.). If you reorder entries or add new ones, Terraform matches by key rather than by position, so existing rules are never accidentally destroyed and recreated.

## Step 2 -- Create the Security Group with a Dynamic Block

Create `main.tf`:

```hcl
resource "aws_security_group" "this" {
  name        = "dynamic-sg-exercise"
  description = "Security group with dynamic ingress rules"
  vpc_id      = "vpc-placeholder"

  dynamic "ingress" {
    for_each = var.ingress_rules
    content {
      from_port   = ingress.value.port
      to_port     = ingress.value.port
      protocol    = ingress.value.protocol
      cidr_blocks = ingress.value.cidr_blocks
      description = ingress.value.description
    }
  }

  # Static egress rule -- allow all outbound traffic
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Allow all outbound"
  }
}
```

Notice that the egress rule is static. Not everything needs to be dynamic -- use dynamic blocks only when the number of sub-blocks is data-driven.

Run a quick check:

```bash
terraform validate
```

```
Success! The configuration is valid.
```

## Step 3 -- Add a Rule and Verify

Add a new entry to the `ingress_rules` map in `variables.tf`:

```hcl
    custom_api = {
      port        = 9090
      protocol    = "tcp"
      cidr_blocks = ["192.168.1.0/24"]
      description = "Allow custom API from office"
    }
```

Run `terraform plan`. The plan should show exactly one new ingress block being added, with no changes to the existing four.

## Step 4 -- Remove a Rule and Verify

Remove the `ssh` entry from the map and run `terraform plan` again. Only the SSH ingress block should be removed -- the remaining rules stay unchanged.

## Common Mistakes

### Using a list instead of a map

```hcl
# Wrong -- list of objects
variable "ingress_rules" {
  type = list(object({ ... }))
}
```

With a list, Terraform tracks rules by index (0, 1, 2...). If you remove the first element, every rule after it shifts down, causing unnecessary destroy-and-recreate operations. Always use a `map` so each rule has a stable key.

### Forgetting `content` inside `dynamic`

```hcl
dynamic "ingress" {
  for_each = var.ingress_rules
  # Missing content block -- this will not compile
  from_port = ingress.value.port
}
```

The `content` block is mandatory inside every `dynamic` block. Without it, Terraform cannot determine the structure of the generated sub-blocks.

## Verify What You Learned

Run `terraform plan` with the original four rules:

```bash
terraform plan
```

```
Terraform will perform the following actions:

  # aws_security_group.this will be created
  + resource "aws_security_group" "this" {
      + name        = "dynamic-sg-exercise"
      + description = "Security group with dynamic ingress rules"

      + ingress {
          + from_port   = 80
          + to_port     = 80
          + protocol    = "tcp"
          + cidr_blocks = ["0.0.0.0/0"]
          + description = "Allow HTTP"
        }

      + ingress {
          + from_port   = 443
          + to_port     = 443
          + protocol    = "tcp"
          + cidr_blocks = ["0.0.0.0/0"]
          + description = "Allow HTTPS"
        }

      + ingress {
          + from_port   = 22
          + to_port     = 22
          + protocol    = "tcp"
          + cidr_blocks = ["10.0.0.0/8"]
          + description = "Allow SSH from private network"
        }

      + ingress {
          + from_port   = 8080
          + to_port     = 8080
          + protocol    = "tcp"
          + cidr_blocks = ["10.0.0.0/8", "172.16.0.0/12"]
          + description = "Allow app traffic"
        }

      + egress {
          + from_port   = 0
          + to_port     = 0
          + protocol    = "-1"
          + cidr_blocks = ["0.0.0.0/0"]
          + description = "Allow all outbound"
        }
    }

Plan: 1 to add, 0 to change, 0 to destroy.
```

Verify the following:

```bash
terraform plan | grep -c "ingress"
```

```
4
```

```bash
terraform plan | grep -c "egress"
```

```
1
```

## What's Next

In the next exercise you will nest dynamic blocks inside each other to build IAM policy documents with optional condition blocks -- a pattern that combines dynamic iteration with conditional logic.

## Reference

- [Dynamic Blocks](https://developer.hashicorp.com/terraform/language/expressions/dynamic-blocks)
- [Type Constraints](https://developer.hashicorp.com/terraform/language/expressions/type-constraints)

## Additional Resources

- [Terraform Dynamic Blocks 101](https://dev.to/sre_panchanan/terraform-dynamic-blocks-101-295e) -- step-by-step tutorial on DEV Community covering dynamic blocks with security group examples
- [Manage Similar Resources with For Each](https://developer.hashicorp.com/terraform/tutorials/configuration-language/for-each) -- official tutorial on iteration patterns applicable to both resources and dynamic blocks
- [Terraform Security Group](https://spacelift.io/blog/terraform-security-group) -- practical guide to creating AWS security groups with dynamic ingress/egress rules

# 6. flatten() for Nested Lists

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 5 (cidrsubnet() for Subnet Calculation)

## Learning Objectives

After completing this exercise, you will be able to:

- Use `flatten()` to convert nested lists into a single flat list for iteration
- Implement nested `for` expressions to decompose hierarchical data into individual elements
- Convert flat lists into maps with unique keys suitable for `for_each` consumption

## Why flatten()

Real-world infrastructure configuration is naturally hierarchical. A security group contains multiple rules. A VPC spans multiple availability zones, each with its own subnets. A Kubernetes deployment has multiple containers, each with its own ports.

`for_each` only accepts a map or a set of strings -- it cannot iterate over a list of lists. When your data is nested (a map of security groups, each containing a list of rules), you need to flatten it into a single-level structure before you can create one resource per rule.

`flatten()` takes a list that may contain other lists (at any depth) and produces a single flat list. Combined with nested `for` expressions, it lets you decompose any hierarchical structure into individual elements. A second `for` expression then converts this flat list into a map with unique keys, which is what `for_each` needs.

## Defining the Hierarchical Data

This variable defines two security groups, each with its own list of rules. The goal is to create one security group rule resource per rule entry, not per security group.

Create `main.tf`:

```hcl
variable "security_groups" {
  default = {
    web = {
      description = "Web traffic"
      rules = [
        { port = 80,  cidr = "0.0.0.0/0",    description = "HTTP" },
        { port = 443, cidr = "0.0.0.0/0",    description = "HTTPS" },
      ]
    }
    app = {
      description = "App traffic"
      rules = [
        { port = 8080, cidr = "10.0.0.0/16",  description = "App HTTP" },
        { port = 8443, cidr = "10.0.0.0/16",  description = "App HTTPS" },
        { port = 9090, cidr = "10.0.0.0/16",  description = "Metrics" },
      ]
    }
  }
}

locals {
  # Step 1: Flatten the hierarchy into a single list of rule objects
  sg_rules = flatten([
    for sg_name, sg in var.security_groups : [
      for rule in sg.rules : {
        sg_name     = sg_name
        port        = rule.port
        cidr        = rule.cidr
        description = rule.description
      }
    ]
  ])

  # Step 2: Convert the flat list into a map with unique keys
  sg_rules_map = {
    for rule in local.sg_rules :
    "${rule.sg_name}-${rule.port}" => rule
  }
}

output "flat_rules" {
  value = local.sg_rules
}

output "rules_map" {
  value = local.sg_rules_map
}

output "rule_count" {
  value = length(local.sg_rules)
}
```

## How the Flattening Works

The outer `for` iterates over security groups (web, app). The inner `for` iterates over each group's rules. Without `flatten()`, this produces a list of lists:

```
[
  [ {sg_name="web", port=80, ...}, {sg_name="web", port=443, ...} ],
  [ {sg_name="app", port=8080, ...}, {sg_name="app", port=8443, ...}, {sg_name="app", port=9090, ...} ]
]
```

`flatten()` collapses this into a single list of 5 elements. The second `for` expression converts it into a map where each key is `"sg_name-port"`, producing unique identifiers like `"web-80"`, `"app-9090"`, etc.

## Verifying the Results

```bash
terraform console
```

```
> length(local.sg_rules)
5
> local.sg_rules_map["web-80"].cidr
"0.0.0.0/0"
> local.sg_rules_map["app-9090"].description
"Metrics"
> keys(local.sg_rules_map)
[
  "app-8080",
  "app-8443",
  "app-9090",
  "web-443",
  "web-80",
]
> exit
```

## Using the Map with for_each

Once you have the map, creating one resource per rule is straightforward:

```hcl
# Example usage (not part of the exercise files):
resource "aws_security_group_rule" "this" {
  for_each          = local.sg_rules_map
  type              = "ingress"
  from_port         = each.value.port
  to_port           = each.value.port
  protocol          = "tcp"
  cidr_blocks       = [each.value.cidr]
  security_group_id = aws_security_group.this[each.value.sg_name].id
  description       = each.value.description
}
```

## Common Mistakes

### Generating duplicate map keys

If two rules in different security groups use the same port, the key `"${rule.port}"` alone would collide:

```hcl
# Wrong: port 443 might exist in both "web" and "app"
sg_rules_map = {
  for rule in local.sg_rules :
  "${rule.port}" => rule
}
```

```
Error: Two different items produced the key "443".
```

Always include enough context in the key to make it unique. Using `"${rule.sg_name}-${rule.port}"` ensures uniqueness across security groups.

### Forgetting flatten() and trying to iterate over nested lists

Without `flatten()`, the nested `for` expression produces a list of lists. Passing this directly to `for_each` fails because `for_each` expects a flat map or set:

```hcl
# Wrong: nested_rules is a list of lists, not a flat structure
locals {
  nested_rules = [
    for sg_name, sg in var.security_groups : [
      for rule in sg.rules : rule
    ]
  ]
}
```

Always wrap nested `for` expressions in `flatten()` before converting to a map.

## Verify What You Learned

```bash
terraform init
```

```
Terraform has been successfully initialized!
```

```bash
terraform plan
```

```
Changes to Outputs:
  + rule_count = 5
```

```bash
terraform console
```

```
> length(local.sg_rules)
5
> local.sg_rules_map["web-443"].description
"HTTPS"
> local.sg_rules_map["app-8080"].cidr
"10.0.0.0/16"
> exit
```

## What's Next

You used `flatten()` with nested `for` expressions to decompose hierarchical data into flat structures that `for_each` can consume. In the next exercise, you will learn how to use `templatefile()` to generate dynamic scripts and configuration files with variable interpolation and control directives.

## Reference

- [flatten Function](https://developer.hashicorp.com/terraform/language/functions/flatten)
- [For Expressions](https://developer.hashicorp.com/terraform/language/expressions/for)
- [The for_each Meta-Argument](https://developer.hashicorp.com/terraform/language/meta-arguments/for_each)

## Additional Resources

- [Flatten Function](https://developer.hashicorp.com/terraform/language/functions/flatten) -- Official documentation with the recommended pattern of flatten + for for flattening hierarchical structures
- [Terraform Flatten Function](https://spacelift.io/blog/terraform-flatten) -- Step-by-step guide on using flatten() to decompose nested lists into flat structures
- [Manage Similar Resources with For Each](https://developer.hashicorp.com/terraform/tutorials/configuration-language/for-each) -- HashiCorp tutorial on for_each and preparing flat data for resource iteration

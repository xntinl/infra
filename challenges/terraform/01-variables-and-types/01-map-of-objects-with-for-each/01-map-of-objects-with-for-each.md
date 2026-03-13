# 1. Map of Objects with for_each

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account

## Learning Objectives

After completing this exercise, you will be able to:

- Use `map(object({...}))` to model multi-environment configuration as a single typed variable
- Implement `for_each` to create one resource instance per map key without index-based pitfalls
- Construct map outputs from `for_each` resources using `for` expressions

## Why Map of Objects with for_each

When you manage infrastructure across multiple environments (dev, staging, prod), each environment shares the same structure but differs in specific values such as instance size, scaling limits, or feature flags. Without a structured approach, you end up duplicating resource blocks or relying on `count` with index-based lists, which causes Terraform to destroy and recreate resources whenever you reorder or remove an element.

`map(object({...}))` solves the structural problem: it lets you define a single variable where each key is an environment name and each value is an object with typed attributes. `for_each` solves the iteration problem: it creates one resource instance per key, and because instances are identified by their key (not a numeric index), adding or removing an environment only affects that specific instance.

## Defining the Variable

The variable type constrains every entry in the map to have the same shape. Terraform will reject any input that does not match the object schema at plan time.

Create `variables.tf`:

```hcl
variable "environments" {
  type = map(object({
    instance_type = string
    min_size      = number
    max_size      = number
    enable_https  = bool
  }))
}
```

## Providing Values

Each key in the map becomes an identifier that Terraform tracks in state. Choose stable, meaningful keys -- they should not change once resources exist.

Create `terraform.tfvars`:

```hcl
environments = {
  dev = {
    instance_type = "t3.micro"
    min_size      = 1
    max_size      = 2
    enable_https  = false
  }
  staging = {
    instance_type = "t3.small"
    min_size      = 2
    max_size      = 4
    enable_https  = true
  }
  prod = {
    instance_type = "t3.medium"
    min_size      = 3
    max_size      = 10
    enable_https  = true
  }
}
```

## Iterating with for_each

`for_each` accepts a map or set. When you pass it a map, each iteration exposes `each.key` (the map key) and `each.value` (the object). The resource address in state becomes `aws_ssm_parameter.env_config["dev"]`, `aws_ssm_parameter.env_config["staging"]`, etc.

Create `main.tf`:

```hcl
resource "aws_ssm_parameter" "env_config" {
  for_each = var.environments
  name     = "/app/${each.key}/config"
  type     = "String"
  value    = jsonencode(each.value)
}
```

## Building a Map Output

A `for` expression transforms the `for_each` resource collection into a clean map of environment name to ARN.

Create `outputs.tf`:

```hcl
output "config_arns" {
  value = { for k, v in aws_ssm_parameter.env_config : k => v.arn }
}
```

## Verifying the Variable in Console

Before creating any resources, you can inspect the variable interactively.

```bash
terraform console
```

```
> var.environments["prod"].max_size
10
> var.environments["dev"].enable_https
false
> exit
```

## What Happens When You Remove an Environment

If you delete the `staging` key from `terraform.tfvars` and run `terraform plan`, Terraform will only destroy `aws_ssm_parameter.env_config["staging"]`. The `dev` and `prod` resources remain untouched. This is the key advantage over `count`, where removing an element from a list shifts all subsequent indices and triggers unnecessary replacements.

## Common Mistakes

### Using count instead of for_each for named resources

When you use `count` with a list of environment names, resources are addressed by index (`aws_ssm_parameter.env_config[0]`). Removing the first element shifts all others:

```hcl
# Wrong: index-based addressing
variable "env_list" {
  default = ["dev", "staging", "prod"]
}

resource "aws_ssm_parameter" "env_config" {
  count = length(var.env_list)
  name  = "/app/${var.env_list[count.index]}/config"
  type  = "String"
  value = "{}"
}
```

Removing `"dev"` from the list causes `staging` and `prod` to be destroyed and recreated. Use `for_each` with a map instead.

### Forgetting to mark the output as a map

If you use a list-style `for` expression in the output, you lose the association between environment name and ARN:

```hcl
# Wrong: produces a list, not a map
output "config_arns" {
  value = [for k, v in aws_ssm_parameter.env_config : v.arn]
}
```

Use `{ for k, v in ... : k => v.arn }` to produce a map with meaningful keys.

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
Terraform will perform the following actions:

  # aws_ssm_parameter.env_config["dev"] will be created
  # aws_ssm_parameter.env_config["prod"] will be created
  # aws_ssm_parameter.env_config["staging"] will be created

Plan: 3 to add, 0 to change, 0 to destroy.
```

```bash
terraform console
```

```
> var.environments["prod"].max_size
10
> length(var.environments)
3
> keys(var.environments)
[
  "dev",
  "prod",
  "staging",
]
> exit
```

## What's Next

You modeled multi-environment configuration with `map(object({...}))` and iterated over it with `for_each`. In the next exercise, you will learn how to make object attributes optional with default values using the `optional()` modifier, reducing the amount of configuration callers need to provide.

## Reference

- [The for_each Meta-Argument](https://developer.hashicorp.com/terraform/language/meta-arguments/for_each)
- [Type Constraints](https://developer.hashicorp.com/terraform/language/expressions/type-constraints)
- [For Expressions](https://developer.hashicorp.com/terraform/language/expressions/for)

## Additional Resources

- [Manage Similar Resources with For Each](https://developer.hashicorp.com/terraform/tutorials/configuration-language/for-each) -- HashiCorp tutorial on creating multiple similar resources declaratively with for_each
- [Terraform For Each: A Comprehensive Guide](https://spacelift.io/blog/terraform-for-each) -- Practical guide covering for_each with maps, sets, and advanced iteration patterns
- [Terraform Map Variable: A Complete Guide](https://www.env0.com/blog/terraform-map-variable-a-complete-guide-with-practical-examples) -- Examples of map(object) variables and their use with for_each
- [Terraform Variables Tutorial](https://developer.hashicorp.com/terraform/tutorials/configuration-language/variables) -- Official tutorial covering variable definition, typing, and complex types

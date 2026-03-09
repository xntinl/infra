# 11. zipmap() to Create Maps from Lists

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 10 (formatlist() and join() for ARN Construction)

## Learning Objectives

After completing this exercise, you will be able to:

- Use `zipmap()` to combine two parallel lists into a key-value map
- Implement `for` expressions to extract fields from object lists before passing them to `zipmap()`
- Distinguish when `zipmap()` is appropriate versus using a `for` expression to build a map directly

## Why zipmap()

Data sources and external APIs often return related data as separate, parallel lists: one list of names and one list of IDs, or one list of keys and one list of values. These lists have positional correspondence -- element 0 of the first list relates to element 0 of the second -- but a list of names and a list of values are hard to work with in Terraform. You cannot pass two parallel lists to `for_each`.

`zipmap()` formalizes this positional relationship by taking two lists and producing a map where elements from the first list become keys and elements from the second become values. The result is a proper map that `for_each` can consume, that `lookup()` can query, and that produces clear output when displayed.

This is analogous to Python's `dict(zip(keys, values))` or Rust's `keys.into_iter().zip(values).collect::<HashMap<_,_>>()` -- it converts parallel sequences into an associative structure.

## Three Uses of zipmap()

This configuration demonstrates three common patterns: combining simple lists, building tags from key/value lists, and extracting fields from a list of objects.

Create `main.tf`:

```hcl
variable "subnet_names" {
  default = ["public-a", "public-b", "private-a", "private-b"]
}

variable "subnet_cidrs" {
  default = ["10.0.1.0/24", "10.0.2.0/24", "10.0.101.0/24", "10.0.102.0/24"]
}

locals {
  # Pattern 1: Combine two parallel lists into a map
  subnet_map = zipmap(var.subnet_names, var.subnet_cidrs)

  # Pattern 2: Build a tags map from key/value lists
  tag_keys   = ["Environment", "Team", "CostCenter"]
  tag_values = ["production", "platform", "CC-1234"]
  common_tags = zipmap(local.tag_keys, local.tag_values)

  # Pattern 3: Extract fields from objects and zip them
  users = [
    { name = "alice", role = "admin" },
    { name = "bob",   role = "developer" },
    { name = "carol", role = "viewer" },
  ]
  user_roles = zipmap(
    [for u in local.users : u.name],
    [for u in local.users : u.role]
  )
}

output "subnet_map"  { value = local.subnet_map }
output "common_tags" { value = local.common_tags }
output "user_roles"  { value = local.user_roles }
```

## Inspecting the Results

```bash
terraform plan
```

```
Changes to Outputs:
  + common_tags = {
      + CostCenter  = "CC-1234"
      + Environment = "production"
      + Team        = "platform"
    }
  + subnet_map  = {
      + private-a = "10.0.101.0/24"
      + private-b = "10.0.102.0/24"
      + public-a  = "10.0.1.0/24"
      + public-b  = "10.0.2.0/24"
    }
  + user_roles  = {
      + alice = "admin"
      + bob   = "developer"
      + carol = "viewer"
    }
```

## Exploring zipmap() in Console

```bash
terraform console
```

```
> local.subnet_map["public-a"]
"10.0.1.0/24"
> local.user_roles["bob"]
"developer"
> zipmap(["a", "b"], [1, 2])
{
  "a" = 1
  "b" = 2
}
> length(local.common_tags)
3
> exit
```

## When to Use zipmap() vs for Expressions

For pattern 3 (extracting fields from objects), you can achieve the same result with a `for` expression:

```hcl
# Using zipmap()
user_roles = zipmap(
  [for u in local.users : u.name],
  [for u in local.users : u.role]
)

# Equivalent: using a for expression directly
user_roles = { for u in local.users : u.name => u.role }
```

The `for` expression is more concise when you already have a list of objects. `zipmap()` is more appropriate when you have two truly separate lists (e.g., from different data sources) that need to be combined.

## Common Mistakes

### Passing lists of different lengths

`zipmap()` requires both lists to have exactly the same number of elements:

```hcl
# Wrong: 3 keys but only 2 values
zipmap(["a", "b", "c"], [1, 2])
```

```
Error: Invalid function argument: number of keys (3) does not match number
of values (2).
```

Always verify that both lists have the same length, especially when they come from different sources.

### Duplicate keys in the first list

If the keys list contains duplicates, later values silently overwrite earlier ones:

```hcl
# The key "a" appears twice; the result is {a = 3, b = 2}
zipmap(["a", "b", "a"], [1, 2, 3])
```

This can cause subtle bugs. Ensure your keys list contains unique values, or use a `for` expression with an explicit grouping strategy if duplicates are expected.

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
  + common_tags = {
      + CostCenter  = "CC-1234"
      + Environment = "production"
      + Team        = "platform"
    }
  + subnet_map  = {
      + private-a = "10.0.101.0/24"
      + private-b = "10.0.102.0/24"
      + public-a  = "10.0.1.0/24"
      + public-b  = "10.0.2.0/24"
    }
  + user_roles  = {
      + alice = "admin"
      + bob   = "developer"
      + carol = "viewer"
    }
```

```bash
terraform console
```

```
> local.subnet_map["private-a"]
"10.0.101.0/24"
> local.common_tags["Team"]
"platform"
> local.user_roles["carol"]
"viewer"
> exit
```

## What's Next

You used `zipmap()` to combine parallel lists into maps and learned when `for` expressions are a more concise alternative. In the next exercise, you will learn how to use `coalesce()` and `try()` to implement fallback chains for handling missing or null values gracefully.

## Reference

- [zipmap Function](https://developer.hashicorp.com/terraform/language/functions/zipmap)
- [For Expressions](https://developer.hashicorp.com/terraform/language/expressions/for)

## Additional Resources

- [Terraform Zipmap Function](https://spacelift.io/blog/terraform-zipmap-function) -- Practical guide on zipmap() with examples of combining parallel lists into key-value maps
- [Perform Dynamic Operations with Functions](https://developer.hashicorp.com/terraform/tutorials/configuration-language/functions) -- Official HashiCorp tutorial covering collection functions including zipmap(), keys(), and values()
- [Manage Similar Resources with For Each](https://developer.hashicorp.com/terraform/tutorials/configuration-language/for-each) -- Tutorial on preparing data in map format for for_each, complementing zipmap usage

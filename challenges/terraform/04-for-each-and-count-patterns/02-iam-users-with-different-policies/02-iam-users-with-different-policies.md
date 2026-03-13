# 17. IAM Users with Different Policies

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 16 (Multiple S3 Buckets from a List)

## Learning Objectives

After completing this exercise, you will be able to:

- Use `for_each` on a map of objects to create resources with per-instance configuration
- Apply `flatten()` to generate cross-product combinations for one-to-many relationships
- Use `optional()` attributes with default values in variable type definitions
- Derive unique keys for `for_each` using `basename()` and string interpolation

## Why `for_each` on Maps of Objects

The previous exercise used `for_each` with `toset()` -- every resource was identical except for its name. Real infrastructure is rarely that uniform. You often need multiple resources of the same type where each one has different settings: different IAM paths, different policies, different flags.

A map of objects solves this. Each key identifies a resource instance, and each value is an object containing that instance's configuration. Terraform creates one resource per map entry, and `each.value` provides access to the full object.

The harder problem is one-to-many relationships. A single IAM user can have multiple policy attachments. You cannot nest `for_each` inside a resource, so you need to pre-compute a flat list of all user-policy pairs using `flatten()`, then convert it back to a map with unique keys for a second `for_each`.

## Step 1 -- Define the Users Variable

Create `variables.tf`:

```hcl
variable "users" {
  description = "Map of IAM users with their configuration"
  type = map(object({
    path          = optional(string, "/")
    groups        = list(string)
    policy_arns   = list(string)
    force_destroy = optional(bool, false)
  }))
  default = {
    alice = {
      groups      = ["admins"]
      policy_arns = ["arn:aws:iam::aws:policy/AdministratorAccess"]
    }
    bob = {
      path        = "/developers/"
      groups      = ["developers"]
      policy_arns = [
        "arn:aws:iam::aws:policy/PowerUserAccess",
        "arn:aws:iam::aws:policy/IAMReadOnlyAccess",
      ]
      force_destroy = true
    }
    carol = {
      groups      = ["readers"]
      policy_arns = ["arn:aws:iam::aws:policy/ReadOnlyAccess"]
    }
  }
}
```

The `optional()` modifier lets callers omit `path` and `force_destroy`. When omitted, they default to `"/"` and `false` respectively. Alice and Carol do not specify `path`, so they get the root path automatically.

## Step 2 -- Create Users with `for_each`

Create `main.tf`:

```hcl
resource "aws_iam_user" "this" {
  for_each      = var.users
  name          = each.key
  path          = each.value.path
  force_destroy = each.value.force_destroy
}
```

Each map key (`alice`, `bob`, `carol`) becomes the user name. The resource is addressed as `aws_iam_user.this["alice"]`, `aws_iam_user.this["bob"]`, etc.

## Step 3 -- Flatten User-Policy Pairs

Add the cross-product computation:

```hcl
locals {
  # Flatten the one-to-many relationship: each user can have multiple policies
  user_policy_attachments = flatten([
    for user_name, user in var.users : [
      for policy_arn in user.policy_arns : {
        user_name  = user_name
        policy_arn = policy_arn
      }
    ]
  ])
}
```

This produces a flat list of objects:

```
[
  { user_name = "alice", policy_arn = "...AdministratorAccess" },
  { user_name = "bob",   policy_arn = "...PowerUserAccess" },
  { user_name = "bob",   policy_arn = "...IAMReadOnlyAccess" },
  { user_name = "carol", policy_arn = "...ReadOnlyAccess" },
]
```

## Step 4 -- Create Policy Attachments

Convert the flat list to a map with unique keys and use `for_each`:

```hcl
resource "aws_iam_user_policy_attachment" "this" {
  for_each = {
    for upa in local.user_policy_attachments :
    "${upa.user_name}-${basename(upa.policy_arn)}" => upa
  }
  user       = aws_iam_user.this[each.value.user_name].name
  policy_arn = each.value.policy_arn
}

output "user_arns" {
  description = "Map of user name to ARN"
  value       = { for k, v in aws_iam_user.this : k => v.arn }
}
```

The `basename()` function extracts the last segment of the policy ARN (e.g., `AdministratorAccess` from `arn:aws:iam::aws:policy/AdministratorAccess`), producing readable keys like `alice-AdministratorAccess` and `bob-PowerUserAccess`.

## Step 5 -- Plan and Verify Counts

Run `terraform plan`:

```bash
terraform plan
```

Expect 3 users and 4 policy attachments (alice: 1, bob: 2, carol: 1).

## Step 6 -- Add a New User

Add a fourth user to the map:

```hcl
    dave = {
      path        = "/contractors/"
      groups      = ["developers"]
      policy_arns = ["arn:aws:iam::aws:policy/ReadOnlyAccess"]
      force_destroy = true
    }
```

Run `terraform plan`. Terraform should plan to create 1 new user and 1 new policy attachment, with no changes to existing resources.

## Common Mistakes

### Non-unique keys in the attachment map

```hcl
# Wrong -- if two users share the same policy, basename alone is not unique
for upa in local.user_policy_attachments :
basename(upa.policy_arn) => upa
```

If both `carol` and `dave` have `ReadOnlyAccess`, the key `ReadOnlyAccess` collides. Always prefix with the user name: `"${upa.user_name}-${basename(upa.policy_arn)}"`.

### Referencing `each.key` instead of `each.value.user_name`

```hcl
# Wrong -- each.key is the composite key like "bob-PowerUserAccess"
user = aws_iam_user.this[each.key].name
```

In the attachment resource, `each.key` is the derived composite key, not the user name. Use `each.value.user_name` to reference back to the user resource.

## Verify What You Learned

```bash
terraform plan | grep "will be created" | wc -l
```

```
7
```

(3 users + 4 attachments)

```bash
terraform plan | grep 'aws_iam_user.this\["bob"\]'
```

```
  # aws_iam_user.this["bob"] will be created
```

```bash
terraform plan | grep 'aws_iam_user_policy_attachment'
```

```
  # aws_iam_user_policy_attachment.this["alice-AdministratorAccess"] will be created
  # aws_iam_user_policy_attachment.this["bob-IAMReadOnlyAccess"] will be created
  # aws_iam_user_policy_attachment.this["bob-PowerUserAccess"] will be created
  # aws_iam_user_policy_attachment.this["carol-ReadOnlyAccess"] will be created
```

```bash
terraform plan | grep 'path.*"/developers/"'
```

```
      + path          = "/developers/"
```

## What's Next

In the next exercise you will switch from `for_each` to `count` and learn the idiomatic pattern for conditional resource creation: `count = condition ? 1 : 0`.

## Reference

- [The for_each Meta-Argument](https://developer.hashicorp.com/terraform/language/meta-arguments/for_each)
- [flatten Function](https://developer.hashicorp.com/terraform/language/functions/flatten)
- [Type Constraints - optional](https://developer.hashicorp.com/terraform/language/expressions/type-constraints#optional)
- [basename Function](https://developer.hashicorp.com/terraform/language/functions/basename)

## Additional Resources

- [HashiCorp: AWS IAM Policy Tutorial](https://developer.hashicorp.com/terraform/tutorials/aws/aws-iam-policy) -- official step-by-step tutorial for creating and managing IAM users with policies
- [Flattening Nested Structures for for_each](https://developer.hashicorp.com/terraform/language/functions/flatten#flattening-nested-structures-for-for_each) -- official documentation with examples of flatten() for cross-product patterns
- [Spacelift: Terraform for_each Guide](https://spacelift.io/blog/terraform-for-each) -- detailed guide on for_each with maps of objects for per-instance configuration

# 10. formatlist() and join() for ARN Construction

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 9 (regex() for Input Validation)

## Learning Objectives

After completing this exercise, you will be able to:

- Use `formatlist()` to apply a printf-style format string to every element in a list
- Use `join()` to concatenate list elements into a single string with a separator
- Use `concat()` to combine multiple lists into one for IAM policy construction

## Why formatlist() and join()

IAM policies require lists of ARNs specifying which resources the policy grants access to. When you manage multiple S3 buckets, you need ARNs for both the buckets themselves (`arn:aws:s3:::bucket-name`) and their objects (`arn:aws:s3:::bucket-name/*`). Writing these out by hand for each bucket is repetitive and error-prone, especially when the list of buckets is dynamic.

`formatlist()` applies a format string to every element in a list, producing a new list. If you have `["logs", "artifacts", "backups"]` and the format `"arn:aws:s3:::%s"`, you get three complete ARNs in one expression. `concat()` then merges the bucket ARNs and object ARNs into a single list. `join()` can convert any list into a separated string for contexts that require it.

This pattern is the standard way to construct IAM policy resource lists programmatically: define the resource names once, and derive all ARN variations from that single source of truth.

## Building ARN Lists

This configuration constructs S3 bucket ARNs, S3 object ARNs, and DynamoDB table ARNs from input lists.

Create `main.tf`:

```hcl
variable "bucket_names" {
  default = ["logs", "artifacts", "backups"]
}

variable "account_id" {
  default = "123456789012"
}

variable "dynamodb_tables" {
  default = ["users", "orders", "sessions"]
}

locals {
  # ARNs for the buckets themselves (needed for ListBucket)
  bucket_arns = formatlist("arn:aws:s3:::%s", var.bucket_names)

  # ARNs for objects inside the buckets (needed for GetObject, PutObject)
  bucket_object_arns = formatlist("arn:aws:s3:::%s/*", var.bucket_names)

  # Combined: both bucket and object ARNs in a single list
  all_s3_arns = concat(local.bucket_arns, local.bucket_object_arns)

  # DynamoDB table ARNs include account ID and region
  table_arns = formatlist(
    "arn:aws:dynamodb:us-east-1:%s:table/%s",
    var.account_id,
    var.dynamodb_tables
  )

  # Human-readable list for logging or descriptions
  bucket_list_string = join(", ", var.bucket_names)

  # JSON-style resource list for inline policies
  policy_resources = join("\",\"", local.all_s3_arns)
}

output "bucket_arns"        { value = local.bucket_arns }
output "bucket_object_arns" { value = local.bucket_object_arns }
output "all_s3_arns"        { value = local.all_s3_arns }
output "table_arns"         { value = local.table_arns }
output "bucket_list_string" { value = local.bucket_list_string }
```

## Inspecting the Results

```bash
terraform plan
```

```
Changes to Outputs:
  + all_s3_arns        = [
      + "arn:aws:s3:::logs",
      + "arn:aws:s3:::artifacts",
      + "arn:aws:s3:::backups",
      + "arn:aws:s3:::logs/*",
      + "arn:aws:s3:::artifacts/*",
      + "arn:aws:s3:::backups/*",
    ]
  + bucket_arns        = [
      + "arn:aws:s3:::logs",
      + "arn:aws:s3:::artifacts",
      + "arn:aws:s3:::backups",
    ]
  + bucket_list_string = "logs, artifacts, backups"
  + bucket_object_arns = [
      + "arn:aws:s3:::logs/*",
      + "arn:aws:s3:::artifacts/*",
      + "arn:aws:s3:::backups/*",
    ]
  + table_arns         = [
      + "arn:aws:dynamodb:us-east-1:123456789012:table/users",
      + "arn:aws:dynamodb:us-east-1:123456789012:table/orders",
      + "arn:aws:dynamodb:us-east-1:123456789012:table/sessions",
    ]
```

## How formatlist() Handles Multiple Arguments

When `formatlist()` receives a scalar (non-list) value for an argument, it reuses that value for every element. When it receives a list, it zips the elements positionally:

```bash
terraform console
```

```
> formatlist("Hello, %s!", ["Alice", "Bob"])
[
  "Hello, Alice!",
  "Hello, Bob!",
]
> formatlist("%s-%s", ["a", "b"], ["1", "2"])
[
  "a-1",
  "b-2",
]
> join("-", ["a", "b", "c"])
"a-b-c"
> exit
```

In the DynamoDB example, `var.account_id` is a scalar string and `var.dynamodb_tables` is a list. `formatlist()` uses the same account ID for every table name.

## Common Mistakes

### Passing lists of different lengths to formatlist()

When both arguments are lists, they must have the same length:

```hcl
# Wrong: 2 elements vs 3 elements
formatlist("%s:%s", ["a", "b"], ["1", "2", "3"])
```

```
Error: Invalid function argument: argument lengths must match (2 vs 3).
```

If you need to pair a scalar with a list, pass the scalar directly -- `formatlist()` broadcasts it automatically.

### Using format() instead of formatlist() for lists

`format()` operates on single values. If you pass it a list, you get a stringified list representation, not individual formatted strings:

```hcl
# Wrong: produces a single string like "arn:aws:s3:::[\"logs\",\"artifacts\"]"
format("arn:aws:s3:::%s", var.bucket_names)

# Correct: produces one ARN per bucket
formatlist("arn:aws:s3:::%s", var.bucket_names)
```

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
  + bucket_arns        = [
      + "arn:aws:s3:::logs",
      + "arn:aws:s3:::artifacts",
      + "arn:aws:s3:::backups",
    ]
  + bucket_list_string = "logs, artifacts, backups"
```

```bash
terraform console
```

```
> length(local.all_s3_arns)
6
> local.table_arns[0]
"arn:aws:dynamodb:us-east-1:123456789012:table/users"
> join(", ", ["alpha", "beta", "gamma"])
"alpha, beta, gamma"
> exit
```

## What's Next

You used `formatlist()`, `join()`, and `concat()` to construct ARN lists from dynamic data. In the next exercise, you will learn how to use `zipmap()` to create maps from parallel lists, which is useful when working with data sources that return keys and values as separate lists.

## Reference

- [formatlist Function](https://developer.hashicorp.com/terraform/language/functions/formatlist)
- [join Function](https://developer.hashicorp.com/terraform/language/functions/join)
- [concat Function](https://developer.hashicorp.com/terraform/language/functions/concat)
- [AWS ARN Format](https://docs.aws.amazon.com/IAM/latest/UserGuide/reference-arns.html)

## Additional Resources

- [Perform Dynamic Operations with Functions](https://developer.hashicorp.com/terraform/tutorials/configuration-language/functions) -- Official HashiCorp tutorial covering string and collection functions including formatlist() and join()
- [Terraform Split and Join Functions](https://www.env0.com/blog/terraform-split-and-join-functions-examples-and-best-practices) -- Practical guide on join(), split(), and compound string construction patterns like ARNs
- [AWS IAM ARN Reference](https://docs.aws.amazon.com/IAM/latest/UserGuide/reference-arns.html) -- Official AWS reference on ARN format for understanding the structures built programmatically

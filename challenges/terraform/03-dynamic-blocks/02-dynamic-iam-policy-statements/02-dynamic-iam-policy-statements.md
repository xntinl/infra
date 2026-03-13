# 14. Dynamic IAM Policy Statements

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 13 (Dynamic Ingress/Egress Rules)

## Learning Objectives

After completing this exercise, you will be able to:

- Nest `dynamic` blocks to generate complex multi-level structures
- Use a ternary expression in `for_each` to conditionally generate sub-blocks
- Model optional attributes with `optional()` in variable type definitions
- Build IAM policy documents declaratively using `aws_iam_policy_document`

## Why Nested Dynamic Blocks

IAM policies follow a strict JSON schema: a document contains multiple statements, and each statement may or may not include a `condition` block. Writing this by hand in Terraform means duplicating the entire `statement` block for every permission, and adding conditional logic with separate data sources whenever a condition is needed.

Nested dynamic blocks let you express the entire policy as a single variable -- a list of objects where some entries have a condition and others do not. The outer `dynamic "statement"` generates each statement, while an inner `dynamic "condition"` generates the condition block only when one is present. This keeps the code compact and the data easy to review.

The `aws_iam_policy_document` data source is preferred over raw JSON strings because Terraform validates the structure at plan time, supports variable interpolation, and produces correctly formatted output.

## Step 1 -- Define the Policy Statements Variable

Create `variables.tf`:

```hcl
variable "policy_statements" {
  description = "List of IAM policy statements, each with optional condition"
  type = list(object({
    sid       = string
    effect    = string
    actions   = list(string)
    resources = list(string)
    condition = optional(object({
      test     = string
      variable = string
      values   = list(string)
    }))
  }))
  default = [
    {
      sid       = "S3ReadOnly"
      effect    = "Allow"
      actions   = ["s3:GetObject", "s3:ListBucket"]
      resources = ["arn:aws:s3:::my-bucket", "arn:aws:s3:::my-bucket/*"]
      condition = null
    },
    {
      sid       = "DynamoDBAccess"
      effect    = "Allow"
      actions   = ["dynamodb:GetItem", "dynamodb:PutItem", "dynamodb:Query"]
      resources = ["arn:aws:dynamodb:*:*:table/my-table"]
      condition = null
    },
    {
      sid       = "KMSDecryptWithCondition"
      effect    = "Allow"
      actions   = ["kms:Decrypt"]
      resources = ["*"]
      condition = {
        test     = "StringEquals"
        variable = "kms:ViaService"
        values   = ["s3.us-east-1.amazonaws.com"]
      }
    },
  ]
}
```

The `optional()` modifier on `condition` means callers can omit it entirely -- Terraform defaults it to `null`. This is cleaner than requiring every statement to explicitly set `condition = null`.

## Step 2 -- Build the Policy Document with Nested Dynamic Blocks

Create `main.tf`:

```hcl
data "aws_iam_policy_document" "this" {
  dynamic "statement" {
    for_each = var.policy_statements
    content {
      sid       = statement.value.sid
      effect    = statement.value.effect
      actions   = statement.value.actions
      resources = statement.value.resources

      # Inner dynamic block -- only generates when condition is not null
      dynamic "condition" {
        for_each = statement.value.condition != null ? [statement.value.condition] : []
        content {
          test     = condition.value.test
          variable = condition.value.variable
          values   = condition.value.values
        }
      }
    }
  }
}

output "policy_json" {
  description = "The rendered IAM policy JSON"
  value       = data.aws_iam_policy_document.this.json
}
```

The key pattern is the ternary in the inner `for_each`:

```hcl
for_each = statement.value.condition != null ? [statement.value.condition] : []
```

When `condition` is `null`, the list is empty and no `condition` block is generated. When it has a value, the list has exactly one element and one `condition` block is produced.

## Step 3 -- Verify the Generated JSON

Run `terraform plan` to confirm no errors:

```bash
terraform plan
```

Then inspect the output in the Terraform console:

```bash
terraform apply -auto-approve
terraform output policy_json
```

The output should contain three statements. Only the KMS statement includes a `Condition` block.

## Step 4 -- Add a Statement Without a Condition

Append a new entry to the `policy_statements` default:

```hcl
    {
      sid       = "CloudWatchLogs"
      effect    = "Allow"
      actions   = ["logs:CreateLogGroup", "logs:CreateLogStream", "logs:PutLogEvents"]
      resources = ["arn:aws:logs:*:*:*"]
    }
```

Note that `condition` is completely omitted (not set to `null`). Thanks to `optional()`, Terraform defaults it to `null` automatically. Run `terraform plan` and confirm a fourth statement appears.

## Step 5 -- Failure Scenario: Invalid Condition

Try adding a statement with an incomplete condition:

```hcl
    {
      sid       = "BadCondition"
      effect    = "Allow"
      actions   = ["s3:*"]
      resources = ["*"]
      condition = {
        test     = "StringEquals"
        variable = "aws:RequestedRegion"
        # Missing 'values' field
      }
    }
```

Run `terraform validate`. Terraform will report a type error because `values` is required in the condition object -- the type system catches the mistake before any API call.

## Common Mistakes

### Wrapping the condition in a list unnecessarily

```hcl
# Wrong -- condition is already a single object, not a list
dynamic "condition" {
  for_each = [statement.value.condition]  # Fails when condition is null
  ...
}
```

When `condition` is `null`, `[null]` is a list with one element, so the dynamic block tries to generate a condition from `null` and crashes. Always use the ternary pattern: `condition != null ? [condition] : []`.

### Using `jsonencode()` instead of the data source

```hcl
# Wrong -- manual JSON is fragile and unvalidated
resource "aws_iam_policy" "this" {
  policy = jsonencode({
    Statement = [...]
  })
}
```

The `aws_iam_policy_document` data source validates structure, handles formatting, and integrates cleanly with dynamic blocks. Avoid hand-crafting policy JSON.

## Verify What You Learned

Run the plan and check outputs:

```bash
terraform plan
```

```
Terraform will perform the following actions:

  # data.aws_iam_policy_document.this will be read during apply

Plan: 0 to add, 0 to change, 0 to destroy.

Changes to Outputs:
  + policy_json = (known after apply)
```

After applying:

```bash
terraform apply -auto-approve
terraform output -raw policy_json | python3 -m json.tool
```

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Sid": "S3ReadOnly",
            "Effect": "Allow",
            "Action": [
                "s3:ListBucket",
                "s3:GetObject"
            ],
            "Resource": [
                "arn:aws:s3:::my-bucket/*",
                "arn:aws:s3:::my-bucket"
            ]
        },
        {
            "Sid": "DynamoDBAccess",
            "Effect": "Allow",
            "Action": [
                "dynamodb:Query",
                "dynamodb:PutItem",
                "dynamodb:GetItem"
            ],
            "Resource": "arn:aws:dynamodb:*:*:table/my-table"
        },
        {
            "Sid": "KMSDecryptWithCondition",
            "Effect": "Allow",
            "Action": "kms:Decrypt",
            "Resource": "*",
            "Condition": {
                "StringEquals": {
                    "kms:ViaService": "s3.us-east-1.amazonaws.com"
                }
            }
        }
    ]
}
```

Confirm that:

```bash
terraform output -raw policy_json | python3 -c "import sys,json; d=json.load(sys.stdin); print(len(d['Statement']))"
```

```
3
```

```bash
terraform output -raw policy_json | python3 -c "import sys,json; d=json.load(sys.stdin); print('Condition' in d['Statement'][2])"
```

```
True
```

## What's Next

In the next exercise you will use dynamic blocks for a different purpose: generating `tag` blocks on Auto Scaling Groups, where the tag format differs from the standard `tags` map, and you will combine them with `merge()` for a unified tagging strategy.

## Reference

- [Dynamic Blocks](https://developer.hashicorp.com/terraform/language/expressions/dynamic-blocks)
- [aws_iam_policy_document Data Source](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/iam_policy_document)
- [Type Constraints - optional](https://developer.hashicorp.com/terraform/language/expressions/type-constraints#optional-object-type-attributes)

## Additional Resources

- [Using Dynamic Block to Generate IAM Policy Statements](https://dev.to/arpanadhikari/using-dynamic-block-to-generate-iam-policy-statements-in-terraform-52gd) -- DEV Community tutorial on nested dynamic blocks for IAM policies with optional conditions
- [Terraform IAM Policy](https://spacelift.io/blog/terraform-iam-policy) -- practical guide to creating IAM policies with the aws_iam_policy_document data source
- [AWS IAM Policies](https://docs.aws.amazon.com/IAM/latest/UserGuide/access_policies.html) -- official AWS documentation on policy structure to understand the generated JSON

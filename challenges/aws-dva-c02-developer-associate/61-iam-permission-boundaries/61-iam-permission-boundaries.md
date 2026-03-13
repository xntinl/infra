# 61. IAM Permission Boundaries

<!--
difficulty: intermediate
concepts: [permission-boundary, delegated-administration, privilege-escalation, effective-permissions, intersection-logic, iam-role-creation, maximum-permissions]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: apply, analyze
prerequisites: [60-iam-policy-evaluation-logic]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates IAM roles, policies, and a Lambda function. Cost is approximately $0.01/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| jq installed | `jq --version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Explain** how permission boundaries limit the maximum permissions a role can have, independent of identity-based policies
2. **Apply** a permission boundary to a Lambda execution role using Terraform
3. **Analyze** effective permissions as the intersection of the identity-based policy and the permission boundary
4. **Implement** a delegated admin pattern where a developer can create roles but cannot escalate beyond the boundary
5. **Differentiate** between permission boundaries (cap on maximum permissions) and identity-based policies (granted permissions)

## Why Permission Boundaries

In large organizations, platform teams need to let developers create their own IAM roles for Lambda functions without granting them unlimited IAM access. Without guardrails, a developer with `iam:CreateRole` and `iam:AttachRolePolicy` could create a role with `AdministratorAccess` -- escalating their own privileges beyond what was intended.

Permission boundaries solve this. A permission boundary is an IAM policy attached to a role that sets the **maximum permissions** the role can have. The effective permissions are the **intersection** of the identity-based policy and the boundary. If the boundary allows `s3:GetObject` and `s3:PutObject`, attaching a policy with `s3:*` to the role still limits effective permissions to only GetObject and PutObject. The identity-based policy cannot grant more than what the boundary allows.

The DVA-C02 exam tests this intersection logic. The key insight: a permission boundary does not grant any permissions by itself -- it only restricts. An identity-based policy with `s3:GetObject` combined with a boundary that allows `s3:*` gives you `s3:GetObject` (not `s3:*`). The effective permission is always the smaller of the two.

## Building Blocks

Create the following files in your exercise directory. Your job is to fill in each `# TODO` block.

### `providers.tf`

```hcl
terraform {
  required_providers {
    aws     = { source = "hashicorp/aws", version = "~> 5.0" }
    archive = { source = "hashicorp/archive", version = "~> 2.0" }
    null    = { source = "hashicorp/null", version = "~> 3.0" }
  }
}

provider "aws" {
  region = var.region
}
```

### `variables.tf`

```hcl
variable "region" {
  description = "AWS region"
  type        = string
  default     = "us-east-1"
}

variable "project_name" {
  description = "Project name for resource naming and tagging"
  type        = string
  default     = "perm-boundary-demo"
}
```

### `storage.tf`

```hcl
data "aws_caller_identity" "current" {}

# -- S3 Buckets for testing --
resource "aws_s3_bucket" "allowed" {
  bucket        = "${var.project_name}-allowed-${data.aws_caller_identity.current.account_id}"
  force_destroy = true
}

resource "aws_s3_bucket" "restricted" {
  bucket        = "${var.project_name}-restricted-${data.aws_caller_identity.current.account_id}"
  force_destroy = true
}

resource "aws_s3_object" "allowed_file" {
  bucket  = aws_s3_bucket.allowed.id
  key     = "allowed-file.txt"
  content = "This file is in the allowed bucket."
}

resource "aws_s3_object" "restricted_file" {
  bucket  = aws_s3_bucket.restricted.id
  key     = "restricted-file.txt"
  content = "This file is in the restricted bucket."
}
```

### `iam.tf`

```hcl
# =======================================================
# TODO 1 -- Create the permission boundary policy
# =======================================================
# Create a managed IAM policy that serves as the boundary.
# This policy defines the MAXIMUM permissions any role
# with this boundary can have.
#
# Requirements:
#   - Allow s3:GetObject and s3:ListBucket on the "allowed"
#     bucket ONLY (not the restricted bucket)
#   - Allow logs:CreateLogGroup, logs:CreateLogStream,
#     logs:PutLogEvents on all resources
#   - Do NOT allow s3:PutObject, s3:DeleteObject, or any
#     access to the restricted bucket
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_policy


# =======================================================
# TODO 2 -- Create a Lambda role WITH the boundary
# =======================================================
# Create an IAM role for Lambda with:
#   - Standard lambda.amazonaws.com assume role
#   - permissions_boundary set to the policy from TODO 1
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_role#permissions_boundary


# =======================================================
# TODO 3 -- Attach a broad identity-based policy
# =======================================================
# Attach a policy that allows s3:* on ALL S3 resources.
# Even though this policy grants broad S3 access, the
# permission boundary will restrict effective permissions
# to only s3:GetObject and s3:ListBucket on the allowed bucket.
#
# This demonstrates the intersection behavior:
# - Identity policy: s3:* on *
# - Boundary: s3:GetObject, s3:ListBucket on allowed-bucket
# - Effective: s3:GetObject, s3:ListBucket on allowed-bucket
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_role_policy
```

### `build.tf`

```hcl
# -- Lambda function to test effective permissions --
# Build: GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
# main.go: Go Lambda that tests three scenarios:
#   1. s3:GetObject on allowed bucket (both allow -> ALLOWED)
#   2. s3:PutObject on allowed bucket (boundary excludes -> DENIED)
#   3. s3:GetObject on restricted bucket (boundary restricts bucket -> DENIED)
# Env vars: ALLOWED_BUCKET, RESTRICTED_BUCKET

resource "null_resource" "go_build" {
  triggers = { source_hash = filebase64sha256("${path.module}/main.go") }
  provisioner "local-exec" {
    command     = "GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go"
    working_dir = path.module
  }
}

data "archive_file" "function_zip" {
  type        = "zip"
  source_file = "${path.module}/bootstrap"
  output_path = "${path.module}/build/function.zip"
  depends_on  = [null_resource.go_build]
}
```

### `lambda.tf`

```hcl
resource "aws_cloudwatch_log_group" "this" {
  name              = "/aws/lambda/${var.project_name}"
  retention_in_days = 1
}

resource "aws_lambda_function" "this" {
  function_name    = var.project_name
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.bounded.arn
  timeout          = 30

  environment {
    variables = {
      ALLOWED_BUCKET    = aws_s3_bucket.allowed.id
      RESTRICTED_BUCKET = aws_s3_bucket.restricted.id
    }
  }

  depends_on = [aws_cloudwatch_log_group.this]
}
```

### `outputs.tf`

```hcl
output "function_name"     { value = aws_lambda_function.this.function_name }
output "allowed_bucket"    { value = aws_s3_bucket.allowed.id }
output "restricted_bucket" { value = aws_s3_bucket.restricted.id }
```

## Spot the Bug

A developer creates a permission boundary that allows `s3:*` but the identity-based policy only allows `s3:GetObject`. They are confused why `s3:PutObject` does not work:

```hcl
# Permission boundary
resource "aws_iam_policy" "boundary" {
  name = "dev-boundary"
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = "s3:*"
      Resource = "*"
    }]
  })
}

# Identity-based policy
resource "aws_iam_role_policy" "identity" {
  role = aws_iam_role.dev.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = "s3:GetObject"
      Resource = "arn:aws:s3:::my-bucket/*"
    }]
  })
}
```

<details>
<summary>Explain the bug</summary>

This is not actually a bug -- it is **correct behavior** that the developer does not understand. The permission boundary and identity-based policy work as an **intersection**:

- Boundary allows: `s3:*` on `*`
- Identity allows: `s3:GetObject` on `my-bucket/*`
- Effective: `s3:GetObject` on `my-bucket/*`

The permission boundary sets the **maximum** permissions. It does not grant anything by itself. The identity-based policy grants `s3:GetObject`, which is within the boundary, so that works. But `s3:PutObject` is not in the identity-based policy, so it is not effective -- even though the boundary would permit it.

**Key insight:** The boundary is the ceiling, not the floor. To enable `s3:PutObject`, the developer must add it to the **identity-based policy**, not the boundary. The boundary merely allows it to be granted.

```hcl
# Fix: add PutObject to the identity-based policy
resource "aws_iam_role_policy" "identity" {
  role = aws_iam_role.dev.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["s3:GetObject", "s3:PutObject"]
      Resource = "arn:aws:s3:::my-bucket/*"
    }]
  })
}
```

</details>

## Solutions

<details>
<summary>TODO 1 -- Permission boundary policy (`iam.tf`)</summary>

```hcl
data "aws_iam_policy_document" "boundary" {
  statement {
    sid = "AllowS3ReadOnAllowed"
    actions = [
      "s3:GetObject",
      "s3:ListBucket",
    ]
    resources = [
      aws_s3_bucket.allowed.arn,
      "${aws_s3_bucket.allowed.arn}/*",
    ]
  }

  statement {
    sid = "AllowCloudWatchLogs"
    actions = [
      "logs:CreateLogGroup",
      "logs:CreateLogStream",
      "logs:PutLogEvents",
    ]
    resources = ["*"]
  }
}

resource "aws_iam_policy" "boundary" {
  name   = "${var.project_name}-boundary"
  policy = data.aws_iam_policy_document.boundary.json
}
```

</details>

<details>
<summary>TODO 2 -- Lambda role with permission boundary (`iam.tf`)</summary>

```hcl
data "aws_iam_policy_document" "lambda_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals { type = "Service"; identifiers = ["lambda.amazonaws.com"] }
  }
}

resource "aws_iam_role" "bounded" {
  name                 = "${var.project_name}-bounded-role"
  assume_role_policy   = data.aws_iam_policy_document.lambda_assume.json
  permissions_boundary = aws_iam_policy.boundary.arn
}
```

</details>

<details>
<summary>TODO 3 -- Broad identity-based policy (`iam.tf`)</summary>

```hcl
data "aws_iam_policy_document" "broad_s3" {
  statement {
    sid       = "AllowAllS3"
    actions   = ["s3:*"]
    resources = ["*"]
  }
}

resource "aws_iam_role_policy" "broad_s3" {
  name   = "${var.project_name}-broad-s3"
  role   = aws_iam_role.bounded.id
  policy = data.aws_iam_policy_document.broad_s3.json
}
```

Even though this policy allows `s3:*` on all resources, effective permissions are limited to what the boundary allows.

</details>

## Verify What You Learned

### Step 1 -- Deploy and invoke

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init && terraform apply -auto-approve
```

### Step 2 -- Test effective permissions

```bash
aws lambda invoke --function-name $(terraform output -raw function_name) \
  --payload '{}' /dev/stdout 2>/dev/null | jq .
```

Expected results:

| Operation | Bucket | Expected | Reason |
|-----------|--------|----------|--------|
| s3:GetObject | allowed | ALLOWED | Intersection: both allow |
| s3:PutObject | allowed | DENIED | Identity allows, boundary does not |
| s3:GetObject | restricted | DENIED | Identity allows, boundary restricts to allowed bucket only |

### Step 3 -- Verify the boundary is attached

```bash
aws iam get-role --role-name perm-boundary-demo-bounded-role \
  --query "Role.PermissionsBoundary" --output json
```

Expected: `PermissionsBoundaryArn` pointing to the boundary policy.

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

Destroy all resources:

```bash
terraform destroy -auto-approve
```

Verify nothing remains:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

You implemented permission boundaries to limit maximum permissions for a role. In the next exercise, you will explore **VPC endpoints** -- gateway endpoints for S3 and DynamoDB versus interface endpoints for other AWS services, and how Lambda functions in a VPC access AWS services privately.

## Summary

- **Permission boundaries** set the maximum permissions a role can have -- they do not grant permissions themselves
- Effective permissions = **intersection** of identity-based policy AND permission boundary
- If the boundary allows `s3:*` but the identity policy only allows `s3:GetObject`, effective is `s3:GetObject`
- If the identity policy allows `s3:*` but the boundary only allows `s3:GetObject`, effective is `s3:GetObject`
- Use boundaries for **delegated administration**: let developers create roles with `iam:CreateRole` but require them to attach a boundary (preventing privilege escalation)
- Boundaries apply to IAM **roles** and **users**, not groups
- When debugging access issues, check: identity policy AND boundary AND SCPs AND resource-based policies

## Reference

- [IAM Permission Boundaries](https://docs.aws.amazon.com/IAM/latest/UserGuide/access_policies_boundaries.html)
- [Terraform aws_iam_role permissions_boundary](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_role#permissions_boundary)
- [Delegating Role Creation](https://docs.aws.amazon.com/IAM/latest/UserGuide/id_roles_create_for-user.html)

## Additional Resources

- [Permission Boundaries for IAM Entities](https://docs.aws.amazon.com/IAM/latest/UserGuide/access_policies_boundaries.html) -- complete guide to boundary mechanics
- [Delegated Administration Use Case](https://aws.amazon.com/blogs/security/delegate-permission-management-to-developers-using-iam-permissions-boundaries/) -- AWS blog on implementing delegated admin with boundaries
- [Policy Evaluation with Boundaries](https://docs.aws.amazon.com/IAM/latest/UserGuide/reference_policies_evaluation-logic.html) -- how boundaries fit into the evaluation chain
- [Conditions for Enforcing Boundaries](https://docs.aws.amazon.com/IAM/latest/UserGuide/access_policies_boundaries.html#boundary-eval-logic) -- requiring boundaries when creating roles via IAM conditions

# 60. IAM Policy Evaluation Logic

<!--
difficulty: intermediate
concepts: [iam-policy-evaluation, explicit-deny, implicit-deny, identity-based-policy, resource-based-policy, service-control-policy, permission-boundary, policy-evaluation-order]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: analyze, evaluate
prerequisites: [none]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates IAM roles, policies, a Lambda function, and an S3 bucket. Cost is approximately $0.01/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| jq installed | `jq --version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Analyze** the IAM policy evaluation chain: explicit deny beats explicit allow beats implicit deny
2. **Evaluate** outcomes when identity-based policies, resource-based policies, and SCPs interact
3. **Differentiate** between implicit deny (no matching allow statement) and explicit deny (a deny statement matches)
4. **Implement** conflicting IAM policies using Terraform and predict the effective permissions
5. **Justify** why an API call succeeds or fails by tracing through the evaluation logic step by step

## Why IAM Policy Evaluation Logic

Every AWS API call passes through the IAM policy evaluation engine. The engine collects all applicable policies -- identity-based (attached to the principal), resource-based (attached to the resource), SCPs (from Organizations), permission boundaries, and session policies -- and evaluates them using a strict precedence:

1. **Explicit deny in any policy** -- request is denied, full stop. No other policy can override an explicit deny.
2. **SCP allow required** -- if the account is in an Organization, the SCP must allow the action (SCPs are allowlists that cap maximum permissions).
3. **Resource-based policy** -- if the resource policy grants access to the calling principal directly (not via `*`), access is allowed even without an identity-based allow (same-account only).
4. **Permission boundary** -- if present, the action must be allowed by both the boundary and the identity policy (intersection).
5. **Identity-based policy** -- if no resource-based policy grants access, the identity-based policy must explicitly allow the action.
6. **Implicit deny** -- if no policy explicitly allows, the request is denied by default.

The DVA-C02 exam heavily tests policy evaluation. The most common trap: a developer adds an Allow statement to an identity policy but there is an explicit Deny in an SCP or resource policy. The explicit Deny always wins. Understanding this chain is essential for debugging "Access Denied" errors in production and answering exam questions correctly.

## Building Blocks

Create the following files in your exercise directory. Your job is to fill in each `# TODO` block. The exercise creates conflicting policies and a Lambda function that tests which API calls succeed or fail.

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
  default     = "iam-eval-demo"
}
```

### `storage.tf`

```hcl
data "aws_caller_identity" "current" {}

# -- S3 Bucket (target resource for policy testing) --
resource "aws_s3_bucket" "this" {
  bucket        = "${var.project_name}-${data.aws_caller_identity.current.account_id}"
  force_destroy = true
}

resource "aws_s3_object" "test_object" {
  bucket  = aws_s3_bucket.this.id
  key     = "test-file.txt"
  content = "This file tests IAM policy evaluation."
}
```

### `iam.tf`

```hcl
# -- Lambda execution role --
data "aws_iam_policy_document" "lambda_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals { type = "Service"; identifiers = ["lambda.amazonaws.com"] }
  }
}

resource "aws_iam_role" "lambda" {
  name               = "${var.project_name}-lambda-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

resource "aws_iam_role_policy_attachment" "basic" {
  role       = aws_iam_role.lambda.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

# =======================================================
# TODO 1 -- Identity-based policy: ALLOW s3:GetObject
# =======================================================
# Create an identity-based policy that allows s3:GetObject
# on the test bucket. Attach it to the Lambda role.
#
# Requirements:
#   - Allow s3:GetObject on "${aws_s3_bucket.this.arn}/*"
#   - Allow s3:ListBucket on aws_s3_bucket.this.arn
#   - Allow s3:PutObject on "${aws_s3_bucket.this.arn}/*"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_role_policy


# =======================================================
# TODO 2 -- Resource-based policy: DENY s3:PutObject
# =======================================================
# Create an S3 bucket policy that explicitly denies
# s3:PutObject from the Lambda role. This tests the
# rule: explicit deny > explicit allow.
#
# Requirements:
#   - Effect = "Deny"
#   - Principal = Lambda role ARN
#   - Action = "s3:PutObject"
#   - Resource = "${aws_s3_bucket.this.arn}/*"
#
# Even though the identity policy (TODO 1) allows
# s3:PutObject, this explicit Deny in the resource
# policy will override it.
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket_policy


# =======================================================
# TODO 3 -- Second IAM role with NO s3 identity policy
# =======================================================
# Create a second Lambda role that has NO identity-based
# S3 permissions. Then create an S3 bucket policy that
# explicitly ALLOWS s3:GetObject for this second role.
#
# This tests: resource-based policy can grant access
# even without identity-based policy (same-account).
#
# Requirements:
#   - New IAM role with Lambda assume role
#   - NO s3 permissions in identity-based policy
#   - Add an Allow statement to the bucket policy for
#     s3:GetObject from this second role
```

### `build.tf`

```hcl
# -- Lambda function (policy tester) --
# Build: GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
# main.go: Go Lambda that tests s3:GetObject (allowed), s3:PutObject (denied by resource policy),
# and s3:DeleteObject (implicit deny). Returns TestResult[] with operation, result, error.

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
  role             = aws_iam_role.lambda.arn
  timeout          = 30

  environment {
    variables = {
      BUCKET_NAME = aws_s3_bucket.this.id
    }
  }

  depends_on = [aws_iam_role_policy_attachment.basic, aws_cloudwatch_log_group.this]
}
```

### `outputs.tf`

```hcl
output "function_name" { value = aws_lambda_function.this.function_name }
output "bucket_name"   { value = aws_s3_bucket.this.id }
```

## Spot the Bug

A developer has an SCP on the account that allows only specific services. The Lambda function gets `Access Denied` on `s3:GetObject` even though the identity policy has an explicit Allow:

```json
// SCP on the Organization OU
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "lambda:*",
        "dynamodb:*",
        "logs:*",
        "iam:*",
        "sts:*"
      ],
      "Resource": "*"
    }
  ]
}
```

```json
// Identity-based policy on Lambda role
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:GetObject", "s3:PutObject"],
      "Resource": "arn:aws:s3:::my-bucket/*"
    }
  ]
}
```

<details>
<summary>Explain the bug</summary>

The SCP acts as an **allowlist** that caps the maximum permissions for all principals in the account. The SCP allows `lambda:*`, `dynamodb:*`, `logs:*`, `iam:*`, and `sts:*` -- but it does **not** include `s3:*`. Even though the identity-based policy explicitly allows `s3:GetObject`, the SCP does not allow any S3 actions. Since SCPs are evaluated before identity-based policies in the evaluation chain, the S3 actions are effectively denied.

This is not an explicit deny (there is no `Deny` statement) -- it is the SCP's implicit deny from omission. The evaluation logic treats a missing Allow in an SCP the same as a deny: the action is not permitted.

**Fix -- add S3 to the SCP allowlist:**

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "lambda:*",
        "dynamodb:*",
        "logs:*",
        "iam:*",
        "sts:*",
        "s3:*"
      ],
      "Resource": "*"
    }
  ]
}
```

Or use the FullAWSAccess SCP (which allows all actions) at the OU level and restrict specific actions with explicit Deny SCPs instead. This is the AWS-recommended approach.

</details>

## Solutions

<details>
<summary>TODO 1 -- Identity-based policy: ALLOW s3:GetObject (`iam.tf`)</summary>

```hcl
data "aws_iam_policy_document" "lambda_s3" {
  statement {
    sid       = "AllowGetObject"
    actions   = ["s3:GetObject", "s3:PutObject"]
    resources = ["${aws_s3_bucket.this.arn}/*"]
  }
  statement {
    sid       = "AllowListBucket"
    actions   = ["s3:ListBucket"]
    resources = [aws_s3_bucket.this.arn]
  }
}

resource "aws_iam_role_policy" "lambda_s3" {
  name   = "${var.project_name}-lambda-s3"
  role   = aws_iam_role.lambda.id
  policy = data.aws_iam_policy_document.lambda_s3.json
}
```

</details>

<details>
<summary>TODO 2 -- Resource-based policy: DENY s3:PutObject (`iam.tf`)</summary>

```hcl
data "aws_iam_policy_document" "bucket_policy" {
  statement {
    sid       = "DenyPutFromLambda"
    effect    = "Deny"
    actions   = ["s3:PutObject"]
    resources = ["${aws_s3_bucket.this.arn}/*"]

    principals {
      type        = "AWS"
      identifiers = [aws_iam_role.lambda.arn]
    }
  }
}

resource "aws_s3_bucket_policy" "this" {
  bucket = aws_s3_bucket.this.id
  policy = data.aws_iam_policy_document.bucket_policy.json
}
```

</details>

<details>
<summary>TODO 3 -- Second IAM role with resource-based access only (`iam.tf`)</summary>

Create a second role with only `AWSLambdaBasicExecutionRole` (no S3 identity-based policy). Update the bucket policy to add an Allow statement for `s3:GetObject` from this second role. In same-account scenarios, a resource-based policy can grant access even without a matching identity-based policy.

```hcl
resource "aws_iam_role" "lambda_no_s3" {
  name               = "${var.project_name}-lambda-no-s3-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

# Add to bucket policy: Allow s3:GetObject for lambda_no_s3 role
# Combined with the existing Deny for s3:PutObject from lambda role
```

</details>

## Verify What You Learned

### Step 1 -- Deploy and invoke

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init && terraform apply -auto-approve
```

### Step 2 -- Invoke the Lambda and observe results

```bash
aws lambda invoke --function-name $(terraform output -raw function_name) \
  --payload '{}' /dev/stdout 2>/dev/null | jq .
```

Expected results:

| Operation | Expected | Reason |
|-----------|----------|--------|
| s3:GetObject | ALLOWED | Identity policy allows, no deny |
| s3:PutObject | DENIED | Identity allows, but resource policy has explicit deny |
| s3:DeleteObject | DENIED | No allow in any policy (implicit deny) |

### Step 3 -- Verify policy configuration

```bash
# Check identity-based policies
aws iam list-role-policies --role-name iam-eval-demo-lambda-role --output json

# Check bucket policy
aws s3api get-bucket-policy --bucket $(terraform output -raw bucket_name) | jq -r '.Policy' | jq .
```

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

You explored IAM policy evaluation logic and how explicit deny overrides allow in any policy type. In the next exercise, you will implement **IAM permission boundaries** -- a mechanism for delegating role creation while preventing privilege escalation.

## Summary

- IAM policy evaluation follows a strict order: **explicit deny > SCP > resource-based > permission boundary > identity-based > implicit deny**
- An **explicit deny** in any policy always wins -- no other policy can override it
- **SCPs** are allowlists that cap maximum permissions for an entire account or OU
- **Resource-based policies** can grant cross-principal access in the same account without an identity-based allow
- **Implicit deny** is the default: if no policy explicitly allows an action, it is denied
- When debugging "Access Denied," trace through all applicable policies in evaluation order
- The `aws iam simulate-principal-policy` CLI command can test policy evaluation without making real API calls

## Reference

- [IAM Policy Evaluation Logic](https://docs.aws.amazon.com/IAM/latest/UserGuide/reference_policies_evaluation-logic.html)
- [IAM Policy Types](https://docs.aws.amazon.com/IAM/latest/UserGuide/access_policies.html)
- [S3 Bucket Policies](https://docs.aws.amazon.com/AmazonS3/latest/userguide/bucket-policies.html)
- [Terraform aws_iam_role_policy](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_role_policy)

## Additional Resources

- [Policy Evaluation for Same-Account Access](https://docs.aws.amazon.com/IAM/latest/UserGuide/reference_policies_evaluation-logic.html#policy-eval-basics) -- detailed flowchart of same-account evaluation
- [Policy Evaluation for Cross-Account Access](https://docs.aws.amazon.com/IAM/latest/UserGuide/reference_policies_evaluation-logic-cross-account.html) -- how evaluation differs for cross-account requests
- [IAM Policy Simulator](https://docs.aws.amazon.com/IAM/latest/UserGuide/access_policies_testing-policies.html) -- AWS tool for testing policy evaluation
- [Service Control Policies](https://docs.aws.amazon.com/organizations/latest/userguide/orgs_manage_policies_scps.html) -- how SCPs interact with IAM policies

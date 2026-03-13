# 65. STS AssumeRole and Temporary Credentials

<!--
difficulty: intermediate
concepts: [sts-assume-role, temporary-credentials, cross-account-access, session-duration, external-id, get-caller-identity, role-chaining, trust-policy]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: apply, analyze
prerequisites: [60-iam-policy-evaluation-logic]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates IAM roles, a Lambda function, and associated resources. STS API calls are free. Total cost is approximately $0.01/hr for the Lambda function. Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| jq installed | `jq --version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Implement** a Lambda function that assumes an IAM role using `sts:AssumeRole` and uses the temporary credentials to access resources
2. **Configure** trust policies that control which principals can assume a role, including conditions like `sts:ExternalId`
3. **Analyze** the components of temporary credentials: AccessKeyId, SecretAccessKey, SessionToken, and Expiration
4. **Differentiate** between `AssumeRole` (cross-account or role switching), `GetSessionToken` (MFA-protected), and `GetCallerIdentity` (identity inspection)
5. **Apply** session duration limits and understand their constraints when role chaining

## Why STS AssumeRole

STS (Security Token Service) is the mechanism behind temporary credentials in AWS. When a Lambda function needs to access resources in another AWS account, it cannot use its own execution role -- those credentials are scoped to the function's account. Instead, the function calls `sts:AssumeRole` to obtain temporary credentials for a role in the target account.

The AssumeRole flow: (1) the calling principal (Lambda execution role) calls `sts:AssumeRole` with the target role ARN; (2) STS checks the target role's **trust policy** to verify the caller is allowed to assume it; (3) STS returns temporary credentials (access key, secret key, session token) that expire after the specified duration; (4) the caller uses these credentials to make API calls as the assumed role.

The DVA-C02 exam tests trust policy configuration, session duration limits, and external ID usage. Key facts: maximum session duration is 1 hour for role chaining (assuming a role from an already-assumed role), 12 hours for direct role assumption. The `ExternalId` condition prevents confused deputy attacks when third parties assume roles in your account. `GetCallerIdentity` requires no permissions and always works -- use it to verify which identity is making calls.

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
  default     = "sts-assume-role-demo"
}
```

### `storage.tf`

```hcl
data "aws_caller_identity" "current" {}

# -- S3 Bucket (only accessible via assumed role) --
resource "aws_s3_bucket" "target" {
  bucket        = "${var.project_name}-target-${data.aws_caller_identity.current.account_id}"
  force_destroy = true
}

resource "aws_s3_object" "secret_data" {
  bucket  = aws_s3_bucket.target.id
  key     = "secret-data.txt"
  content = "This data is only accessible via the assumed role."
}
```

### `iam.tf`

```hcl
# -- Lambda execution role (source identity) --
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
# TODO 1 -- Grant the Lambda role permission to AssumeRole
# =======================================================
# The Lambda execution role needs sts:AssumeRole permission
# on the target role. Without this, the Lambda function
# cannot call AssumeRole.
#
# Requirements:
#   - Allow sts:AssumeRole on the target role ARN
#
# Docs: https://docs.aws.amazon.com/STS/latest/APIReference/API_AssumeRole.html


# =======================================================
# TODO 2 -- Create the target role with trust policy
# =======================================================
# Create a role that the Lambda function will assume.
# The trust policy must allow the Lambda execution role
# to assume it, with an ExternalId condition.
#
# Requirements:
#   - Trust policy: Allow sts:AssumeRole
#   - Principal: Lambda execution role ARN
#   - Condition: sts:ExternalId = "demo-external-id-12345"
#   - max_session_duration = 3600 (1 hour)
#   - Attach a policy granting s3:GetObject on the target bucket
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_role
```

### `build.tf`

```hcl
# Build: GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
# main.go: Go Lambda that (1) checks original identity via GetCallerIdentity,
# (2) assumes the target role using stscreds.NewAssumeRoleProvider with ExternalID,
# (3) verifies assumed identity, (4) reads S3 object using assumed credentials.
# Env vars: TARGET_ROLE_ARN, EXTERNAL_ID, BUCKET_NAME.

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
      TARGET_ROLE_ARN = aws_iam_role.target.arn
      EXTERNAL_ID     = "demo-external-id-12345"
      BUCKET_NAME     = aws_s3_bucket.target.id
    }
  }

  depends_on = [aws_iam_role_policy_attachment.basic, aws_cloudwatch_log_group.this]
}
```

### `outputs.tf`

```hcl
output "function_name"   { value = aws_lambda_function.this.function_name }
output "target_role_arn" { value = aws_iam_role.target.arn }
output "bucket_name"     { value = aws_s3_bucket.target.id }
```

## Spot the Bug

A developer calls AssumeRole with a session name that exceeds the 64-character limit:

```go
creds, err := stsClient.AssumeRole(ctx, &sts.AssumeRoleInput{
    RoleArn:         aws.String(targetRoleArn),
    RoleSessionName: aws.String("lambda-session-for-cross-account-data-processing-pipeline-order-service-v2"),  // <-- 76 chars
    ExternalID:      aws.String(externalId),
})
```

<details>
<summary>Explain the bug</summary>

The `RoleSessionName` parameter has a **maximum length of 64 characters**. The name `"lambda-session-for-cross-account-data-processing-pipeline-order-service-v2"` is 76 characters, which exceeds the limit. The `AssumeRole` API call fails with `ValidationError: 1 validation error detected: Value at 'roleSessionName' failed to satisfy constraint: Member must have length less than or equal to 64`.

The session name appears in CloudTrail logs and the assumed role ARN (`arn:aws:sts::account:assumed-role/role-name/session-name`), so it should be descriptive but concise.

**Fix -- shorten the session name:**

```go
creds, err := stsClient.AssumeRole(ctx, &sts.AssumeRoleInput{
    RoleArn:         aws.String(targetRoleArn),
    RoleSessionName: aws.String("lambda-order-svc-v2"),  // 19 chars
    ExternalID:      aws.String(externalId),
})
```

Constraints for `RoleSessionName`: minimum 2 characters, maximum 64 characters, allowed characters are `[a-zA-Z0-9_=,.@-]`.

</details>

## Solutions

<details>
<summary>TODO 1 -- Grant Lambda role permission to AssumeRole (`iam.tf`)</summary>

```hcl
data "aws_iam_policy_document" "lambda_assume_target" {
  statement {
    sid       = "AllowAssumeTargetRole"
    actions   = ["sts:AssumeRole"]
    resources = [aws_iam_role.target.arn]
  }
}

resource "aws_iam_role_policy" "lambda_assume_target" {
  name   = "${var.project_name}-assume-target"
  role   = aws_iam_role.lambda.id
  policy = data.aws_iam_policy_document.lambda_assume_target.json
}
```

</details>

<details>
<summary>TODO 2 -- Target role with trust policy and ExternalId (`iam.tf`)</summary>

```hcl
data "aws_iam_policy_document" "target_trust" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "AWS"
      identifiers = [aws_iam_role.lambda.arn]
    }
    condition {
      test     = "StringEquals"
      variable = "sts:ExternalId"
      values   = ["demo-external-id-12345"]
    }
  }
}

resource "aws_iam_role" "target" {
  name                 = "${var.project_name}-target-role"
  assume_role_policy   = data.aws_iam_policy_document.target_trust.json
  max_session_duration = 3600
}

data "aws_iam_policy_document" "target_s3" {
  statement {
    actions   = ["s3:GetObject"]
    resources = ["${aws_s3_bucket.target.arn}/*"]
  }
}

resource "aws_iam_role_policy" "target_s3" {
  name   = "${var.project_name}-target-s3"
  role   = aws_iam_role.target.id
  policy = data.aws_iam_policy_document.target_s3.json
}
```

</details>

## Verify What You Learned

### Step 1 -- Deploy

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init && terraform apply -auto-approve
```

### Step 2 -- Invoke and verify identity switching

```bash
aws lambda invoke --function-name $(terraform output -raw function_name) \
  --payload '{}' /dev/stdout 2>/dev/null | jq .
```

Expected: `original_identity` shows the Lambda execution role ARN, `assumed_identity` shows the target role ARN with the session name, and `s3_content` contains the secret data.

The response demonstrates the complete AssumeRole flow: identity check, role assumption with ExternalId, and S3 access using temporary credentials.

### Step 3 -- Verify trust policy

```bash
aws iam get-role --role-name sts-assume-role-demo-target-role \
  --query "Role.AssumeRolePolicyDocument" --output json | jq .
```

Expected: trust policy with `sts:ExternalId` condition.

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

You implemented STS AssumeRole with temporary credentials and ExternalId protection. In the next exercise, you will explore **WAF rules for API Gateway** -- protecting your API with managed rule sets, custom rate-based rules, and SQL injection detection.

## Summary

- **AssumeRole** returns temporary credentials (AccessKeyId, SecretAccessKey, SessionToken) with a configurable expiration
- The **trust policy** on the target role controls who can assume it -- both the caller and the target must agree
- **ExternalId** prevents confused deputy attacks when third-party services assume roles in your account
- **GetCallerIdentity** requires no permissions and always works -- use it to verify which identity is active
- Maximum session duration: **1 hour** for role chaining, up to **12 hours** for direct assumption
- **RoleSessionName** (2-64 chars) appears in CloudTrail and the assumed role ARN
- The calling principal needs `sts:AssumeRole` permission in its identity-based policy AND the target role's trust policy must allow it
- Temporary credentials cannot be revoked individually -- to invalidate, update the role's trust policy or add a time-based condition
- **GetSessionToken** is for MFA-protected API access, while **AssumeRole** is for switching roles or cross-account access
- Use `aws sts get-caller-identity` to verify which identity is active -- it requires no permissions and never fails

## Reference

- [STS AssumeRole](https://docs.aws.amazon.com/STS/latest/APIReference/API_AssumeRole.html)
- [IAM Role Trust Policies](https://docs.aws.amazon.com/IAM/latest/UserGuide/id_roles_terms-and-concepts.html#term_trust-policy)
- [External ID](https://docs.aws.amazon.com/IAM/latest/UserGuide/id_roles_create_for-user_externalid.html)
- [Terraform aws_iam_role](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_role)

## Additional Resources

- [Cross-Account Access with Roles](https://docs.aws.amazon.com/IAM/latest/UserGuide/tutorial_cross-account-with-roles.html) -- step-by-step tutorial for cross-account role assumption
- [Role Chaining](https://docs.aws.amazon.com/IAM/latest/UserGuide/id_roles_terms-and-concepts.html#iam-term-role-chaining) -- assuming a role from an already-assumed role (1-hour max)
- [Revoking Temporary Credentials](https://docs.aws.amazon.com/IAM/latest/UserGuide/id_roles_use_revoke-sessions.html) -- invalidating sessions from assumed roles
- [STS Regional Endpoints](https://docs.aws.amazon.com/general/latest/gr/sts.html) -- using regional STS endpoints for lower latency

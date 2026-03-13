# 68. KMS Key Policies and Grants

<!--
difficulty: advanced
concepts: [kms-key-policy, kms-grants, key-administrators, key-users, grant-tokens, retiring-grants, resource-based-policy, condition-keys]
tools: [terraform, aws-cli]
estimated_time: 50m
bloom_level: evaluate, create
prerequisites: [58-kms-key-types-symmetric-asymmetric, 59-kms-envelope-encryption-data-keys]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates KMS customer-managed keys ($1.00/month each) and Lambda functions. KMS API calls cost $0.03 per 10,000 requests. Total cost is approximately $0.01/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally
- jq installed

## Learning Objectives

After completing this exercise, you will be able to:

- **Evaluate** the relationship between KMS key policies (resource-based) and IAM policies (identity-based) for controlling key access
- **Design** a key policy that separates key administrators (manage the key) from key users (encrypt/decrypt with the key)
- **Implement** KMS grants for temporary, programmatic access to encryption operations
- **Analyze** why the root account statement in a key policy is essential and what happens without it
- **Differentiate** between key policies (permanent, attached to the key), IAM policies (attached to the principal), and grants (temporary, programmatic)

## Why KMS Key Policies and Grants

KMS keys have a unique access control model compared to other AWS resources. Every KMS key has a **key policy** (resource-based policy) that is the primary access control mechanism. Unlike S3 bucket policies or Lambda resource-based policies, KMS key policies are **mandatory** -- if the key policy does not grant access, IAM policies alone cannot provide it.

The key policy typically contains three types of statements:

1. **Root account statement**: allows the account root principal `kms:*` on the key. This is the "escape hatch" -- without it, if all key administrators lose access, the key becomes **permanently unmanageable**. AWS cannot recover it.

2. **Key administrator statement**: grants `kms:Create*`, `kms:Describe*`, `kms:Enable*`, `kms:Disable*`, `kms:Put*`, `kms:Update*`, `kms:Revoke*`, `kms:Delete*`, `kms:ScheduleKeyDeletion`, `kms:CancelKeyDeletion`. Administrators can manage the key but not use it for cryptographic operations.

3. **Key user statement**: grants `kms:Encrypt`, `kms:Decrypt`, `kms:ReEncrypt*`, `kms:GenerateDataKey*`, `kms:DescribeKey`. Users can perform cryptographic operations but not manage the key.

**Grants** provide temporary, programmatic access. Unlike key policies and IAM policies (which are declarative), grants are created via API calls and can be retired or revoked. Use grants when you need to give a service or process short-lived access to specific KMS operations -- for example, granting a Lambda function encrypt/decrypt access for the duration of a data processing job.

## The Challenge

Build a KMS key with a properly structured key policy that separates administrators from users, then create a Lambda function that manages grants programmatically.

### Requirements

| Requirement | Description |
|---|---|
| Key Policy | Root account statement, key admin role, key user role |
| Admin Role | Can manage the key but NOT encrypt/decrypt |
| User Role | Can encrypt/decrypt but NOT manage the key |
| Lambda Function | Creates and retires grants programmatically |
| Grant | Time-limited encrypt/decrypt access for a specific role |

## Hints

<details>
<summary>Hint 1: Key policy with separated permissions</summary>

Structure the key policy with three distinct statements:

```hcl
resource "aws_kms_key" "this" {
  description             = "Key with separated admin/user access"
  deletion_window_in_days = 7

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid       = "RootAccess"
        Effect    = "Allow"
        Principal = { AWS = "arn:aws:iam::${data.aws_caller_identity.current.account_id}:root" }
        Action    = "kms:*"
        Resource  = "*"
      },
      {
        Sid       = "KeyAdministrators"
        Effect    = "Allow"
        Principal = { AWS = aws_iam_role.admin.arn }
        Action = [
          "kms:Create*", "kms:Describe*", "kms:Enable*", "kms:List*",
          "kms:Put*", "kms:Update*", "kms:Revoke*", "kms:Disable*",
          "kms:Get*", "kms:Delete*", "kms:TagResource", "kms:UntagResource",
          "kms:ScheduleKeyDeletion", "kms:CancelKeyDeletion"
        ]
        Resource = "*"
      },
      {
        Sid       = "KeyUsers"
        Effect    = "Allow"
        Principal = { AWS = aws_iam_role.user.arn }
        Action = [
          "kms:Encrypt", "kms:Decrypt", "kms:ReEncrypt*",
          "kms:GenerateDataKey*", "kms:DescribeKey"
        ]
        Resource = "*"
      }
    ]
  })
}
```

</details>

<details>
<summary>Hint 2: Creating grants programmatically in Go</summary>

Use the KMS SDK to create a grant:

```go
grant, err := kmsClient.CreateGrant(ctx, &kms.CreateGrantInput{
    KeyId:             aws.String(keyID),
    GranteePrincipal:  aws.String(granteeRoleArn),
    Operations:        []types.GrantOperation{types.GrantOperationEncrypt, types.GrantOperationDecrypt},
    RetiringPrincipal: aws.String(retiringRoleArn),
    Name:              aws.String("temp-processing-grant"),
    Constraints: &types.GrantConstraints{
        EncryptionContextSubset: map[string]string{
            "purpose": "data-processing",
        },
    },
})
```

The grant is active immediately (using the grant token) or after a few minutes (eventual consistency). The `RetiringPrincipal` can retire the grant when it is no longer needed.

</details>

<details>
<summary>Hint 3: Retiring a grant</summary>

The grantee or retiring principal can retire a grant:

```go
_, err := kmsClient.RetireGrant(ctx, &kms.RetireGrantInput{
    GrantId: aws.String(grantID),
    KeyId:   aws.String(keyID),
})
```

Alternatively, a key administrator can revoke a grant (which is different from retiring -- revoking can be done by the admin, retiring can be done by the grantee or retiring principal):

```go
_, err := kmsClient.RevokeGrant(ctx, &kms.RevokeGrantInput{
    GrantId: aws.String(grantID),
    KeyId:   aws.String(keyID),
})
```

</details>

<details>
<summary>Hint 4: Grant tokens for immediate use</summary>

When you create a grant, it may take up to 5 minutes to propagate (eventual consistency). If you need to use the grant immediately, include the **grant token** returned from `CreateGrant` in subsequent API calls:

```go
// Create grant
grant, _ := kmsClient.CreateGrant(ctx, &kms.CreateGrantInput{...})

// Use immediately with grant token
_, err := kmsClient.Encrypt(ctx, &kms.EncryptInput{
    KeyId:       aws.String(keyID),
    Plaintext:   []byte("data"),
    GrantTokens: []string{*grant.GrantToken},
})
```

</details>

## Spot the Bug

A developer creates a KMS key policy that omits the root account statement. Later, when the key administrator role is deleted during a cleanup, nobody can manage the key:

```hcl
resource "aws_kms_key" "this" {
  description = "Customer data encryption"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid       = "KeyAdmin"
        Effect    = "Allow"
        Principal = { AWS = "arn:aws:iam::123456789012:role/key-admin" }
        Action    = "kms:*"
        Resource  = "*"
      },
      {
        Sid       = "KeyUsers"
        Effect    = "Allow"
        Principal = { AWS = "arn:aws:iam::123456789012:role/app-role" }
        Action    = ["kms:Encrypt", "kms:Decrypt", "kms:GenerateDataKey*"]
        Resource  = "*"
      }
    ]
  })
}
```

<details>
<summary>Explain the bug</summary>

The key policy is **missing the root account statement**. Without `"Principal": {"AWS": "arn:aws:iam::123456789012:root"}`, the only principal that can manage this key is the `key-admin` role. If that role is accidentally deleted (e.g., during Terraform cleanup, or an IAM team removes it), **no one can manage the key** -- not even the account root user.

KMS key policies are unique: they are the **sole gatekeeper** for key access. Unlike other AWS services where IAM policies can grant access independently, KMS requires the key policy to allow access (either directly or by delegating to IAM via the root account statement). Without the root statement, IAM policies for other principals are ignored for this key.

If the admin role is deleted, the key becomes permanently unmanageable. You cannot update the key policy, schedule it for deletion, or grant new access. AWS Support cannot recover it either.

**Fix -- always include the root account statement:**

```hcl
policy = jsonencode({
  Version = "2012-10-17"
  Statement = [
    {
      Sid       = "EnableRootAccess"
      Effect    = "Allow"
      Principal = { AWS = "arn:aws:iam::123456789012:root" }
      Action    = "kms:*"
      Resource  = "*"
    },
    {
      Sid       = "KeyAdmin"
      Effect    = "Allow"
      Principal = { AWS = "arn:aws:iam::123456789012:role/key-admin" }
      Action    = ["kms:Create*", "kms:Describe*", "kms:Enable*", "kms:List*",
                   "kms:Put*", "kms:Update*", "kms:Revoke*", "kms:Disable*",
                   "kms:Get*", "kms:Delete*", "kms:ScheduleKeyDeletion", "kms:CancelKeyDeletion"]
      Resource  = "*"
    },
    {
      Sid       = "KeyUsers"
      Effect    = "Allow"
      Principal = { AWS = "arn:aws:iam::123456789012:role/app-role" }
      Action    = ["kms:Encrypt", "kms:Decrypt", "kms:GenerateDataKey*", "kms:DescribeKey"]
      Resource  = "*"
    }
  ]
})
```

The root statement does not mean the root user will actually use the key -- it means IAM policies in the account can grant access to the key. This is the recommended pattern from AWS.

</details>

## Verify What You Learned

### Step 1 -- Deploy

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init && terraform apply -auto-approve
```

### Step 2 -- Verify key policy

```bash
KMS_KEY_ID=$(terraform output -raw kms_key_id)
aws kms get-key-policy --key-id "$KMS_KEY_ID" --policy-name default --output text | jq .
```

Expected: three statements (RootAccess, KeyAdministrators, KeyUsers) with separated permissions.

### Step 3 -- Test grant creation

```bash
aws lambda invoke --function-name $(terraform output -raw function_name) \
  --payload '{"action": "create_grant"}' /dev/stdout 2>/dev/null | jq .
```

Expected: response with grant ID and grant token.

### Step 4 -- List active grants

```bash
aws kms list-grants --key-id "$KMS_KEY_ID" \
  --query "Grants[].{Name:Name,Operations:Operations,GranteePrincipal:GranteePrincipal}" \
  --output table
```

Expected: the grant created in Step 3.

### Step 5 -- Test grant retirement

```bash
GRANT_ID=$(aws kms list-grants --key-id "$KMS_KEY_ID" --query "Grants[0].GrantId" --output text)
aws lambda invoke --function-name $(terraform output -raw function_name) \
  --payload "{\"action\": \"retire_grant\", \"grant_id\": \"$GRANT_ID\"}" /dev/stdout 2>/dev/null | jq .
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

Expected: no output (empty state). KMS keys enter a 7-day pending deletion window.

## What's Next

You implemented KMS key policies with separated administrator and user roles, and managed grants programmatically. In the next exercise, you will explore **CloudFormation custom resources** -- using Lambda functions to extend CloudFormation with custom provisioning logic.

## Summary

- KMS key policies are **mandatory** and are the primary access control for KMS keys -- IAM policies alone are not sufficient
- The **root account statement** (`kms:*` for account root) is essential -- without it, losing the admin role means permanently losing key access
- **Key administrators** manage the key lifecycle (create, disable, delete) but cannot perform cryptographic operations
- **Key users** perform encrypt/decrypt/generate operations but cannot manage the key
- **Grants** provide temporary, programmatic access -- created via API, can be retired by the grantee or revoked by an admin
- **Grant tokens** enable immediate use of a newly created grant (bypasses eventual consistency delay)
- Key policy + IAM policy interaction: the key policy must allow IAM evaluation (via root statement) for IAM policies to work
- KMS customer-managed keys cost **$1.00/month** each; grants are free (no additional cost)

## Reference

- [KMS Key Policies](https://docs.aws.amazon.com/kms/latest/developerguide/key-policies.html)
- [KMS Grants](https://docs.aws.amazon.com/kms/latest/developerguide/grants.html)
- [Terraform aws_kms_key](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/kms_key)
- [Terraform aws_kms_grant](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/kms_grant)

## Additional Resources

- [Default Key Policy](https://docs.aws.amazon.com/kms/latest/developerguide/key-policy-default.html) -- AWS-generated default key policy structure
- [Grant Constraints](https://docs.aws.amazon.com/kms/latest/developerguide/grant-manage.html#grant-authorization) -- limiting grants to specific encryption contexts
- [KMS Condition Keys](https://docs.aws.amazon.com/kms/latest/developerguide/policy-conditions.html) -- condition keys for fine-grained policy control
- [Troubleshooting Key Access](https://docs.aws.amazon.com/kms/latest/developerguide/policy-evaluation.html) -- debugging KMS access denied errors

<details>
<summary>Full Solution</summary>

### `lambda/main.go`

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "os"

    "github.com/aws/aws-lambda-go/lambda"
    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/service/kms"
    "github.com/aws/aws-sdk-go-v2/service/kms/types"
)

var kmsClient *kms.Client

func init() {
    cfg, _ := config.LoadDefaultConfig(context.TODO())
    kmsClient = kms.NewFromConfig(cfg)
}

type GrantRequest struct {
    Action  string `json:"action"`
    GrantID string `json:"grant_id,omitempty"`
}

type GrantResponse struct {
    GrantID    string `json:"grant_id,omitempty"`
    GrantToken string `json:"grant_token,omitempty"`
    Message    string `json:"message"`
}

func handler(ctx context.Context, req GrantRequest) (GrantResponse, error) {
    keyID := os.Getenv("KMS_KEY_ID")
    granteeArn := os.Getenv("GRANTEE_ROLE_ARN")
    retiringArn := os.Getenv("RETIRING_ROLE_ARN")

    switch req.Action {
    case "create_grant":
        grant, err := kmsClient.CreateGrant(ctx, &kms.CreateGrantInput{
            KeyId:             aws.String(keyID),
            GranteePrincipal:  aws.String(granteeArn),
            RetiringPrincipal: aws.String(retiringArn),
            Operations:        []types.GrantOperation{types.GrantOperationEncrypt, types.GrantOperationDecrypt},
            Name:              aws.String("temp-processing-grant"),
        })
        if err != nil {
            return GrantResponse{}, fmt.Errorf("create grant failed: %w", err)
        }
        return GrantResponse{
            GrantID:    *grant.GrantId,
            GrantToken: *grant.GrantToken,
            Message:    "Grant created",
        }, nil

    case "retire_grant":
        _, err := kmsClient.RetireGrant(ctx, &kms.RetireGrantInput{
            GrantId: aws.String(req.GrantID),
            KeyId:   aws.String(keyID),
        })
        if err != nil {
            return GrantResponse{}, fmt.Errorf("retire grant failed: %w", err)
        }
        return GrantResponse{Message: "Grant retired"}, nil

    case "list_grants":
        grants, err := kmsClient.ListGrants(ctx, &kms.ListGrantsInput{
            KeyId: aws.String(keyID),
        })
        if err != nil {
            return GrantResponse{}, fmt.Errorf("list grants failed: %w", err)
        }
        data, _ := json.Marshal(grants.Grants)
        return GrantResponse{Message: fmt.Sprintf("Found %d grants: %s", len(grants.Grants), string(data))}, nil

    default:
        return GrantResponse{}, fmt.Errorf("unknown action: %s", req.Action)
    }
}

func main() {
    lambda.Start(handler)
}
```

### `lambda/go.mod`

```
module kms-grants-demo

go 1.21

require (
    github.com/aws/aws-lambda-go v1.47.0
    github.com/aws/aws-sdk-go-v2 v1.24.0
    github.com/aws/aws-sdk-go-v2/config v1.26.0
    github.com/aws/aws-sdk-go-v2/service/kms v1.27.0
)
```

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
  default     = "kms-grants-demo"
}
```

### `kms.tf`

```hcl
data "aws_caller_identity" "current" {}

resource "aws_kms_key" "this" {
  description             = "Key with separated admin/user access and grants"
  deletion_window_in_days = 7

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid       = "RootAccess"
        Effect    = "Allow"
        Principal = { AWS = "arn:aws:iam::${data.aws_caller_identity.current.account_id}:root" }
        Action    = "kms:*"
        Resource  = "*"
      },
      {
        Sid       = "GrantManager"
        Effect    = "Allow"
        Principal = { AWS = aws_iam_role.grant_manager.arn }
        Action = [
          "kms:CreateGrant", "kms:ListGrants", "kms:RevokeGrant",
          "kms:RetireGrant", "kms:DescribeKey"
        ]
        Resource = "*"
      },
      {
        Sid       = "GranteeUser"
        Effect    = "Allow"
        Principal = { AWS = aws_iam_role.grantee.arn }
        Action    = ["kms:DescribeKey"]
        Resource  = "*"
      }
    ]
  })
}

resource "aws_kms_alias" "this" {
  name          = "alias/${var.project_name}"
  target_key_id = aws_kms_key.this.key_id
}
```

### `build.tf`

```hcl
resource "null_resource" "go_build" {
  triggers = { source_hash = filebase64sha256("${path.module}/lambda/main.go") }
  provisioner "local-exec" {
    command     = "GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go"
    working_dir = "${path.module}/lambda"
  }
}

data "archive_file" "function_zip" {
  type        = "zip"
  source_file = "${path.module}/lambda/bootstrap"
  output_path = "${path.module}/build/function.zip"
  depends_on  = [null_resource.go_build]
}
```

### `iam.tf`

```hcl
data "aws_iam_policy_document" "lambda_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "grant_manager" {
  name               = "${var.project_name}-grant-manager"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

resource "aws_iam_role_policy_attachment" "grant_manager_basic" {
  role       = aws_iam_role.grant_manager.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

data "aws_iam_policy_document" "grant_manager_kms" {
  statement {
    actions = [
      "kms:CreateGrant", "kms:ListGrants",
      "kms:RevokeGrant", "kms:RetireGrant", "kms:DescribeKey",
    ]
    resources = [aws_kms_key.this.arn]
  }
}

resource "aws_iam_role_policy" "grant_manager_kms" {
  name   = "kms-grant-management"
  role   = aws_iam_role.grant_manager.id
  policy = data.aws_iam_policy_document.grant_manager_kms.json
}

resource "aws_iam_role" "grantee" {
  name               = "${var.project_name}-grantee"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
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
  role             = aws_iam_role.grant_manager.arn
  timeout          = 30

  environment {
    variables = {
      KMS_KEY_ID        = aws_kms_key.this.key_id
      GRANTEE_ROLE_ARN  = aws_iam_role.grantee.arn
      RETIRING_ROLE_ARN = aws_iam_role.grant_manager.arn
    }
  }

  depends_on = [aws_iam_role_policy_attachment.grant_manager_basic, aws_cloudwatch_log_group.this]
}
```

### `outputs.tf`

```hcl
output "kms_key_id" {
  value = aws_kms_key.this.key_id
}

output "function_name" {
  value = aws_lambda_function.this.function_name
}
```

</details>

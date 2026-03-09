# 65. IAM Policies: Identity-Based, Resource-Based, and SCPs

<!--
difficulty: basic
concepts: [iam-policy, identity-based-policy, resource-based-policy, scp, policy-evaluation, explicit-deny, implicit-deny, principal, effect, action, resource, condition]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: understand
prerequisites: [none]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** IAM policies, roles, and users have no cost. This exercise uses only IAM resources. No charges beyond minimal API calls (~$0.01/hr). Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Basic understanding of JSON structure

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** the three IAM policy types: identity-based, resource-based, and SCPs
- **Describe** the policy evaluation logic: explicit Deny wins, then explicit Allow, then implicit Deny
- **Construct** identity-based policies with least-privilege permissions
- **Distinguish** when resource-based policies enable cross-account access without role assumption
- **Identify** how SCPs restrict maximum available permissions in an account

## Why IAM Policy Types Matter

IAM policy evaluation is the foundation of every AWS architecture. Identity-based policies answer "What can this identity do?" Resource-based policies answer "Who can access this resource?" SCPs answer "What is the maximum permission available?" The critical rule: explicit Deny always wins. For cross-account access, resource-based policies can grant access directly.

## Step 1 -- Identity-Based Policy Examples

Create the following files in your exercise directory:

### `providers.tf`

```hcl
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
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
  default     = "saa-ex65"
}
```

### `iam.tf`

```hcl
# ------------------------------------------------------------------
# Identity-Based Policy: Attached to a role, defines what the
# role can do. This is the most common policy type.
#
# This role has read-only access to S3 and DynamoDB -- typical
# for an application that reads configuration and data.
# ------------------------------------------------------------------
data "aws_iam_policy_document" "app_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "app" {
  name               = "${var.project_name}-app-role"
  assume_role_policy = data.aws_iam_policy_document.app_assume.json
}

# ------------------------------------------------------------------
# Least-privilege identity-based policy: only the specific
# actions, on specific resources, that the application needs.
#
# Anti-pattern: "Action": "*", "Resource": "*" -- never do this.
# ------------------------------------------------------------------
data "aws_iam_policy_document" "app_permissions" {
  # Read configuration from a specific S3 bucket
  statement {
    sid    = "S3ReadConfig"
    effect = "Allow"
    actions = [
      "s3:GetObject",
      "s3:ListBucket"
    ]
    resources = [
      "arn:aws:s3:::my-app-config-bucket",
      "arn:aws:s3:::my-app-config-bucket/*"
    ]
  }

  # Read items from a specific DynamoDB table
  statement {
    sid    = "DynamoDBReadData"
    effect = "Allow"
    actions = [
      "dynamodb:GetItem",
      "dynamodb:Query",
      "dynamodb:Scan"
    ]
    resources = [
      "arn:aws:dynamodb:us-east-1:*:table/my-app-data"
    ]
  }

  # Write CloudWatch Logs (required for Lambda)
  statement {
    sid    = "CloudWatchLogs"
    effect = "Allow"
    actions = [
      "logs:CreateLogGroup",
      "logs:CreateLogStream",
      "logs:PutLogEvents"
    ]
    resources = ["arn:aws:logs:us-east-1:*:*"]
  }
}

resource "aws_iam_role_policy" "app" {
  name   = "${var.project_name}-app-policy"
  role   = aws_iam_role.app.id
  policy = data.aws_iam_policy_document.app_permissions.json
}
```

## Step 2 -- Resource-Based Policy Example

### `events.tf`

```hcl
# ------------------------------------------------------------------
# Resource-Based Policy: Attached to an SQS queue, defines
# WHO can access the queue.
#
# Resource-based policies are unique because they can grant
# cross-account access without the caller needing to assume
# a role. The policy specifies the external account's principal
# directly.
# ------------------------------------------------------------------
resource "aws_sqs_queue" "notifications" {
  name = "${var.project_name}-notifications"
}

data "aws_caller_identity" "current" {}

resource "aws_sqs_queue_policy" "notifications" {
  queue_url = aws_sqs_queue.notifications.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid       = "AllowSNSPublish"
        Effect    = "Allow"
        Principal = {
          Service = "sns.amazonaws.com"
        }
        Action   = "sqs:SendMessage"
        Resource = aws_sqs_queue.notifications.arn
        Condition = {
          ArnEquals = {
            "aws:SourceArn" = "arn:aws:sns:us-east-1:${data.aws_caller_identity.current.account_id}:my-topic"
          }
        }
      },
      {
        Sid       = "CrossAccountRead"
        Effect    = "Allow"
        Principal = {
          AWS = "arn:aws:iam::111222333444:root"
        }
        Action = [
          "sqs:ReceiveMessage",
          "sqs:DeleteMessage",
          "sqs:GetQueueAttributes"
        ]
        Resource = aws_sqs_queue.notifications.arn
      }
    ]
  })
}
```

## Step 3 -- SCP Examples (Reference)

```hcl
# SCPs require AWS Organizations. They define the MAXIMUM
# permissions available -- they never grant permissions.

# Example 1: Deny actions outside approved regions
# resource "aws_organizations_policy" "region_lockdown" {
#   name    = "deny-outside-approved-regions"
#   type    = "SERVICE_CONTROL_POLICY"
#   content = jsonencode({
#     Version = "2012-10-17"
#     Statement = [{
#       Effect    = "Deny"
#       Action    = "*"
#       Resource  = "*"
#       Condition = { StringNotEquals = { "aws:RequestedRegion" = ["us-east-1", "us-west-2"] } }
#     }]
#   })
# }

# Example 2: Deny deletion of CloudTrail logs
# resource "aws_organizations_policy" "protect_cloudtrail" {
#   name    = "protect-cloudtrail"
#   type    = "SERVICE_CONTROL_POLICY"
#   content = jsonencode({
#     Version = "2012-10-17"
#     Statement = [{
#       Effect   = "Deny"
#       Action   = ["cloudtrail:DeleteTrail", "cloudtrail:StopLogging"]
#       Resource = "*"
#     }]
#   })
# }
```

## Step 4 -- Add Outputs

### `outputs.tf`

```hcl
output "app_role_arn" {
  value = aws_iam_role.app.arn
}

output "queue_arn" {
  value = aws_sqs_queue.notifications.arn
}
```

```bash
terraform init
terraform apply -auto-approve
```

## Step 5 -- Verify Policies

```bash
# View identity-based policy
aws iam get-role-policy --role-name saa-ex65-app-role \
  --policy-name saa-ex65-app-policy --query 'PolicyDocument' --output json
```

## Common Mistakes

### 1. Confusing Deny with no explicit Allow

**Wrong understanding:** "If no policy says Deny, the action is allowed."

**Correct understanding:** The default is DENY (implicit deny). An action is only allowed if there is an explicit Allow in at least one policy AND no explicit Deny in any policy. If no policy mentions the action at all, the result is Deny.

```
No policy mentions s3:PutObject --> DENY (implicit deny)
One policy Allows s3:PutObject --> ALLOW
One policy Allows, another Denies --> DENY (explicit deny wins)
```

### 2. Expecting SCP Allow to grant permissions

**Wrong approach:** Creating an SCP that Allows specific actions and expecting users to have those permissions.

```json
{
  "Effect": "Allow",
  "Action": "s3:*",
  "Resource": "*"
}
```

**What happens:** This SCP allows S3 actions to be granted by other policies -- it does not grant S3 access itself. Users still need an identity-based policy that explicitly allows S3 actions. An SCP with `Allow s3:*` combined with no IAM policy = no S3 access.

**Key insight:** SCPs define the ceiling, IAM policies define the floor. The effective permission is the intersection of what the SCP allows and what the IAM policy grants.

### 3. Forgetting resource-based policies enable cross-account access without role assumption

S3, SQS, KMS, and Lambda support resource-based policies that can grant cross-account access directly -- no role assumption needed. Services without resource-based policies (EC2, DynamoDB) always require cross-account role assumption.

## Verify What You Learned

```bash
# Verify role and policy
aws iam get-role --role-name saa-ex65-app-role --query 'Role.RoleName' --output text
aws iam list-role-policies --role-name saa-ex65-app-role
terraform plan
```

Expected: Role exists, `saa-ex65-app-policy` attached, `No changes.`

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

Exercise 66 covers **AWS Organizations and Service Control Policies (SCPs)** in depth. You will create OUs, attach SCPs that restrict regions and enforce encryption, and understand how SCPs interact with IAM policies to define the effective permissions across an organization -- the governance layer that every enterprise architecture requires.

## Summary

- **Identity-based policies** define what actions an identity can perform on which resources
- **Resource-based policies** define which principals can access a resource
- **SCPs** define the maximum permissions available in an account -- never grant permissions
- **Explicit Deny always wins** -- no other policy can override it
- **Cross-account access** can use resource-based policies directly for S3, SQS, KMS, Lambda
- **Least privilege** means granting only the specific actions on specific resources needed
- **Conditions** add context-based restrictions (IP range, MFA status, source VPC, encryption requirements)

## Reference

- [IAM Policy Types](https://docs.aws.amazon.com/IAM/latest/UserGuide/access_policies.html)
- [Policy Evaluation Logic](https://docs.aws.amazon.com/IAM/latest/UserGuide/reference_policies_evaluation-logic.html)
- [Resource-Based Policies](https://docs.aws.amazon.com/IAM/latest/UserGuide/access_policies_identity-vs-resource.html)
- [Service Control Policies](https://docs.aws.amazon.com/organizations/latest/userguide/orgs_manage_policies_scps.html)

## Additional Resources

- [IAM Policy Simulator](https://policysim.aws.amazon.com/) -- test policies before deploying
- [IAM Access Analyzer](https://docs.aws.amazon.com/IAM/latest/UserGuide/what-is-access-analyzer.html) -- identifies resources shared with external principals
- [Example IAM Policies](https://docs.aws.amazon.com/IAM/latest/UserGuide/access_policies_examples.html) -- common policy patterns

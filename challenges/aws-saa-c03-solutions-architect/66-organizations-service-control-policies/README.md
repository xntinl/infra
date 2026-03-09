# 66. AWS Organizations and Service Control Policies

<!--
difficulty: basic
concepts: [aws-organizations, organizational-units, scp, deny-policy, allow-list, deny-list, consolidated-billing, delegated-administrator, management-account, member-account]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: understand
prerequisites: [65-iam-policies-identity-resource-scp]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** AWS Organizations and SCPs have no cost. This exercise creates only IAM and Organizations policy resources. No charges beyond minimal API calls (~$0.01/hr). Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 65 (IAM policy types)
- Understanding of IAM policy JSON structure

## Learning Objectives

After completing this exercise, you will be able to:

- **Describe** AWS Organizations structure: management account, OUs, member accounts
- **Construct** SCPs for security guardrails: region restriction, encryption, root user restrictions
- **Explain** why SCPs restrict but never grant permissions
- **Identify** SCP inheritance: policies cascade from root OU to child OUs to accounts
- **Distinguish** deny-list vs allow-list SCP strategies

## Why AWS Organizations Matter

SCPs are the only mechanism that can restrict the root user and IAM administrators in member accounts. An IAM admin with `AdministratorAccess` can do anything -- unless an SCP restricts them. The management account is exempt from SCPs -- run minimal workloads in it.

## Step 1 -- Organizations Structure and SCP Examples

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
  default     = "saa-ex66"
}
```

### `iam.tf`

```hcl
# ------------------------------------------------------------------
# NOTE: The Organizations resources below require the management
# account. If you are running this in a sandbox account that is NOT
# the management account, use the IAM policy resources only (they
# demonstrate the same policy patterns without needing Organizations).
#
# To enable Organizations SCPs:
# 1. Create an Organization: aws organizations create-organization
# 2. Enable SCP policy type: aws organizations enable-policy-type \
#      --root-id r-xxxx --policy-type SERVICE_CONTROL_POLICY
# ------------------------------------------------------------------

# ============================================================
# Organization Structure (reference -- requires management account)
# ============================================================
#
# Root (r-xxxx)
#   |-- Management Account (no SCPs apply here)
#   |-- Production OU
#   |     |-- prod-app Account
#   |     |-- prod-data Account
#   |-- Development OU
#   |     |-- dev-app Account
#   |     |-- dev-sandbox Account
#   |-- Security OU
#         |-- security-tooling Account
#         |-- log-archive Account
#
# SCPs cascade: Root OU SCP --> Production OU SCP --> Account
# Effective permissions = intersection of ALL SCPs in the path

# ------------------------------------------------------------------
# SCP 1: Deny All Regions Except Approved
# Prevents any identity in the account from using AWS services
# in non-approved regions. Critical for data sovereignty and
# compliance (GDPR, HIPAA).
#
# Exception: Global services (IAM, STS, CloudFront, Route 53)
# must be excluded because they only operate in us-east-1 or
# globally.
# ------------------------------------------------------------------
resource "aws_iam_policy" "deny_unapproved_regions" {
  name        = "${var.project_name}-deny-unapproved-regions"
  description = "SCP example: deny actions outside approved regions"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid      = "DenyNonApprovedRegions"
        Effect   = "Deny"
        Action   = "*"
        Resource = "*"
        Condition = {
          StringNotEquals = {
            "aws:RequestedRegion" = ["us-east-1", "us-west-2"]
          }
          # Exclude global services that don't have a region
          ArnNotLike = {
            "aws:PrincipalArn" = [
              "arn:aws:iam::*:role/OrganizationAccountAccessRole"
            ]
          }
        }
      }
    ]
  })
}

# ------------------------------------------------------------------
# SCP 2: Require Encryption for S3
# Denies any S3 PutObject call that does not include server-side
# encryption. This ensures all data stored in S3 is encrypted
# regardless of what individual bucket policies say.
# ------------------------------------------------------------------
resource "aws_iam_policy" "require_s3_encryption" {
  name        = "${var.project_name}-require-s3-encryption"
  description = "SCP example: deny S3 uploads without encryption"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid      = "DenyUnencryptedS3Uploads"
        Effect   = "Deny"
        Action   = "s3:PutObject"
        Resource = "*"
        Condition = {
          StringNotEquals = {
            "s3:x-amz-server-side-encryption" = ["aws:kms", "AES256"]
          }
        }
      },
      {
        Sid      = "DenyUnencryptedS3UploadsNull"
        Effect   = "Deny"
        Action   = "s3:PutObject"
        Resource = "*"
        Condition = {
          Null = {
            "s3:x-amz-server-side-encryption" = "true"
          }
        }
      }
    ]
  })
}

# ------------------------------------------------------------------
# SCP 3: Deny Root User Access
# Prevents the root user from performing any action. The root
# user should never be used for day-to-day operations. This SCP
# enforces that policy organizationally.
#
# Note: SCPs cannot restrict the management account's root user.
# This SCP only affects member accounts.
# ------------------------------------------------------------------
resource "aws_iam_policy" "deny_root" {
  name        = "${var.project_name}-deny-root-user"
  description = "SCP example: deny all root user actions"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid       = "DenyRootUserAccess"
        Effect    = "Deny"
        Action    = "*"
        Resource  = "*"
        Condition = {
          StringLike = {
            "aws:PrincipalArn" = "arn:aws:iam::*:root"
          }
        }
      }
    ]
  })
}

# ------------------------------------------------------------------
# SCP 4: Deny Leaving the Organization
# Prevents member accounts from removing themselves from the
# organization. Without this, an administrator in a member
# account could detach from the org and bypass all SCPs.
# ------------------------------------------------------------------
resource "aws_iam_policy" "deny_leave_org" {
  name        = "${var.project_name}-deny-leave-org"
  description = "SCP example: prevent accounts from leaving organization"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid      = "DenyLeaveOrganization"
        Effect   = "Deny"
        Action   = "organizations:LeaveOrganization"
        Resource = "*"
      }
    ]
  })
}

# ------------------------------------------------------------------
# SCP 5: Deny Disabling Security Services
# ------------------------------------------------------------------
resource "aws_iam_policy" "protect_security_services" {
  name        = "${var.project_name}-protect-security-services"
  description = "SCP example: prevent disabling security services"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid    = "DenyDisableSecurityServices"
      Effect = "Deny"
      Action = [
        "guardduty:DeleteDetector", "guardduty:DisassociateFromMasterAccount",
        "securityhub:DisableSecurityHub", "config:StopConfigurationRecorder",
        "cloudtrail:DeleteTrail", "cloudtrail:StopLogging"
      ]
      Resource = "*"
    }]
  })
}
```

## Step 2 -- SCP Strategy Comparison

**Deny-list (most common):** Start with FullAWSAccess, attach Deny SCPs for specific restrictions. Everything is allowed except what is denied.

**Allow-list (strict):** Remove FullAWSAccess, attach narrow Allow SCPs. Only explicitly allowed services are available. Warning: easy to accidentally lock out essential services (IAM, STS, CloudWatch).

## Step 3 -- SCP Inheritance

SCPs cascade from Root OU to child OUs to accounts. Effective permissions = intersection of ALL SCPs in the path. A deny at any level cannot be overridden by a lower-level allow.

## Step 4 -- Add Outputs and Verify

### `outputs.tf`

```hcl
output "region_lockdown_policy_arn" {
  value = aws_iam_policy.deny_unapproved_regions.arn
}

output "encryption_policy_arn" {
  value = aws_iam_policy.require_s3_encryption.arn
}

output "deny_root_policy_arn" {
  value = aws_iam_policy.deny_root.arn
}
```

```bash
terraform init
terraform apply -auto-approve
```

## Common Mistakes

### 1. Thinking SCPs grant permissions

SCPs define the ceiling, not the floor. `SCP Allow s3:* + no IAM policy = DENY`. Users still need an IAM policy that explicitly allows actions. The effective permission is the intersection of SCP allows and IAM policy grants.

### 2. Forgetting SCPs do not apply to the management account

The management account is always exempt from SCPs. Run minimal workloads in it and use it only for organizational management.

### 3. Locking out all access with allow-list SCPs

Removing `FullAWSAccess` and adding a narrow allow SCP that forgets IAM, STS, or CloudWatch breaks account management entirely. Always include essential services in allow-list SCPs.

## Verify What You Learned

```bash
# Verify SCP example policies were created
aws iam list-policies --scope Local \
  --query 'Policies[?starts_with(PolicyName, `saa-ex66`)].PolicyName' --output table
terraform plan
```

Expected: Five policies listed. `No changes.`

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

Exercise 67 covers **IAM Identity Center (successor to AWS SSO)** for workforce identity management. You will learn how to configure centralized access to multiple AWS accounts using permission sets and understand when Identity Center (workforce) vs Cognito (customer-facing) is the correct choice -- a common exam distinction.

## Summary

- **AWS Organizations** provides centralized multi-account management with consolidated billing
- **OUs** group accounts for policy application -- Production, Development, Security are common patterns
- **SCPs restrict but never grant** -- they define the maximum permissions in member accounts
- **SCP inheritance** cascades from root OU to child OUs to accounts
- **Management account is exempt** from SCPs -- use it only for organizational management
- **Deny-list strategy** (most common): FullAWSAccess + deny SCPs for specific restrictions
- **Common guardrails:** deny non-approved regions, require encryption, deny root user, protect security services

## Reference

- [AWS Organizations User Guide](https://docs.aws.amazon.com/organizations/latest/userguide/orgs_introduction.html)
- [SCP Syntax and Examples](https://docs.aws.amazon.com/organizations/latest/userguide/orgs_manage_policies_scps_syntax.html)
- [SCP Inheritance](https://docs.aws.amazon.com/organizations/latest/userguide/orgs_manage_policies_inheritance_auth.html)
- [Terraform aws_organizations_policy](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/organizations_policy)

## Additional Resources

- [SCP Examples from AWS](https://docs.aws.amazon.com/organizations/latest/userguide/orgs_manage_policies_scps_examples.html) -- 30+ battle-tested SCP patterns
- [AWS Control Tower](https://docs.aws.amazon.com/controltower/latest/userguide/what-is-control-tower.html) -- automated multi-account setup with guardrails
- [Organization Consolidated Billing](https://docs.aws.amazon.com/awsaccountbilling/latest/aboutv2/consolidated-billing.html) -- volume discounts across accounts

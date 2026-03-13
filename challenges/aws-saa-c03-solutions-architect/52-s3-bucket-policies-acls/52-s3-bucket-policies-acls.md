# 52. S3 Bucket Policies and ACLs

<!--
difficulty: basic
concepts: [bucket-policy, s3-acl, block-public-access, resource-based-policy, cross-account-access, policy-evaluation, canned-acl, principal]
tools: [terraform, aws-cli]
estimated_time: 30m
bloom_level: understand
prerequisites: [none]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** S3 storage for small test objects is negligible (~$0.01/hr). No additional charges for bucket policies or ACLs. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Basic understanding of JSON and IAM policy structure

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** the difference between bucket policies (resource-based JSON policies) and ACLs (legacy object-level permissions)
- **Construct** bucket policies for common scenarios: IP restriction, HTTPS enforcement, cross-account access
- **Describe** the four Block Public Access settings and how they override bucket policies and ACLs
- **Identify** when ACLs are still necessary (ALB access logs, CloudFront logs) vs when bucket policies are preferred
- **Distinguish** between identity-based IAM policies and resource-based bucket policies for access control
- **Diagnose** conflicting bucket policy and ACL configurations that result in unexpected access denials

## Why S3 Access Control Matters

S3 access control appears on nearly every SAA-C03 exam because misconfigured bucket permissions are one of the most common causes of data breaches. AWS provides multiple overlapping mechanisms -- IAM policies, bucket policies, ACLs, and Block Public Access -- and understanding how they interact is critical. The key principle is that an explicit Deny in any policy always wins. If a bucket policy Denies access and an IAM policy Allows it, the result is Deny. If Block Public Access is enabled, it overrides any public-granting bucket policy or ACL.

The architectural decision is primarily between bucket policies and IAM policies. Bucket policies are resource-based (attached to the bucket) and can grant cross-account access without the target account configuring anything. IAM policies are identity-based (attached to users/roles) and only work within the same account. For cross-account S3 access, you typically need both: a bucket policy granting access and an IAM policy in the other account allowing the action. ACLs are considered legacy -- AWS recommends disabling them for new buckets -- but they are still required for a few specific services (ALB access logs, CloudFront distribution logs).

## Step 1 -- Create the Bucket with Block Public Access

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
  default     = "saa-ex52"
}
```

### `storage.tf`

```hcl
resource "random_id" "suffix" {
  byte_length = 4
}

# ------------------------------------------------------------------
# S3 bucket with Block Public Access enabled. This is the default
# for all new buckets and should NEVER be disabled unless you have
# a specific, documented reason (e.g., static website hosting).
# ------------------------------------------------------------------
resource "aws_s3_bucket" "this" {
  bucket        = "${var.project_name}-policies-${random_id.suffix.hex}"
  force_destroy = true

  tags = { Name = "${var.project_name}-bucket-policies" }
}

# ------------------------------------------------------------------
# Block Public Access: four independent settings that act as a
# safety net above bucket policies and ACLs.
#
# block_public_acls:       Blocks PUT calls with public ACLs
# ignore_public_acls:      Ignores existing public ACLs
# block_public_policy:     Blocks bucket policies that grant public access
# restrict_public_buckets: Restricts access to authorized users only
#
# All four should be TRUE for any bucket that does not need to be
# publicly accessible. This is the single most important S3 security
# configuration.
# ------------------------------------------------------------------
resource "aws_s3_bucket_public_access_block" "this" {
  bucket = aws_s3_bucket.this.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# ------------------------------------------------------------------
# Disable ACLs: AWS recommends "BucketOwnerEnforced" which disables
# ACLs entirely. All access control is managed via bucket policies
# and IAM policies.
# ------------------------------------------------------------------
resource "aws_s3_bucket_ownership_controls" "this" {
  bucket = aws_s3_bucket.this.id

  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

# ------------------------------------------------------------------
# Bucket policy: Deny all access except from a specific IP range.
# This is a common pattern for restricting S3 access to your
# corporate network or VPN exit point.
#
# Policy evaluation order:
#   1. Explicit Deny always wins
#   2. Block Public Access overrides public grants
#   3. Explicit Allow (from any policy) grants access
#   4. Default Deny if no policy explicitly allows
# ------------------------------------------------------------------
resource "aws_s3_bucket_policy" "ip_restriction" {
  bucket = aws_s3_bucket.this.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid       = "AllowFromCorporateIP"
        Effect    = "Allow"
        Principal = "*"
        Action    = ["s3:GetObject", "s3:ListBucket"]
        Resource = [
          aws_s3_bucket.this.arn,
          "${aws_s3_bucket.this.arn}/*"
        ]
        Condition = {
          IpAddress = {
            "aws:SourceIp" = "203.0.113.0/24"
          }
        }
      },
      {
        Sid       = "EnforceHTTPS"
        Effect    = "Deny"
        Principal = "*"
        Action    = "s3:*"
        Resource = [
          aws_s3_bucket.this.arn,
          "${aws_s3_bucket.this.arn}/*"
        ]
        Condition = {
          Bool = {
            "aws:SecureTransport" = "false"
          }
        }
      }
    ]
  })

  depends_on = [aws_s3_bucket_public_access_block.this]
}
```

## Step 2 -- Cross-Account Bucket Policy (Reference)

This is a reference pattern -- do not apply it unless you have a second AWS account:

```hcl
# ------------------------------------------------------------------
# Cross-account access pattern (reference only).
# This bucket policy grants a specific IAM role in another account
# read access to the "shared/" prefix.
#
# Both sides are needed:
#   1. Bucket policy (this account): allows the external role
#   2. IAM policy (other account): allows the role to access S3
#
# Without the bucket policy, the IAM policy alone is insufficient
# because S3 is in a different account.
# ------------------------------------------------------------------

# resource "aws_s3_bucket_policy" "cross_account" {
#   bucket = aws_s3_bucket.this.id
#
#   policy = jsonencode({
#     Version = "2012-10-17"
#     Statement = [
#       {
#         Sid       = "CrossAccountReadAccess"
#         Effect    = "Allow"
#         Principal = {
#           AWS = "arn:aws:iam::111222333444:role/data-reader"
#         }
#         Action    = ["s3:GetObject", "s3:ListBucket"]
#         Resource  = [
#           aws_s3_bucket.this.arn,
#           "${aws_s3_bucket.this.arn}/shared/*"
#         ]
#         Condition = {
#           StringLike = {
#             "s3:prefix" = "shared/*"
#           }
#         }
#       }
#     ]
#   })
# }
```

## Step 3 -- Add Outputs and Upload Test Data

### `outputs.tf`

```hcl
output "bucket_name" {
  value = aws_s3_bucket.this.id
}

output "bucket_arn" {
  value = aws_s3_bucket.this.arn
}
```

```bash
terraform init
terraform apply -auto-approve

BUCKET=$(terraform output -raw bucket_name)

# Upload test objects
echo "public data" | aws s3 cp - "s3://$BUCKET/public/readme.txt"
echo "shared data" | aws s3 cp - "s3://$BUCKET/shared/report.csv"
echo "private data" | aws s3 cp - "s3://$BUCKET/private/secrets.txt"
```

## Step 4 -- Verify Policy Effects

```bash
BUCKET=$(terraform output -raw bucket_name)

# View the active bucket policy
aws s3api get-bucket-policy --bucket "$BUCKET" --output text | python3 -m json.tool

# Check Block Public Access settings
aws s3api get-public-access-block --bucket "$BUCKET" \
  --query 'PublicAccessBlockConfiguration' --output json

# Check object ownership setting (ACLs disabled)
aws s3api get-bucket-ownership-controls --bucket "$BUCKET" \
  --query 'OwnershipControls.Rules[0].ObjectOwnership' --output text
```

### Policy Evaluation Flowchart

```
Request arrives at S3
        |
        v
Is Block Public Access enabled?
   Yes --> Does the policy/ACL grant public access?
              Yes --> DENY (Block Public Access overrides)
              No  --> Continue evaluation
   No  --> Continue evaluation
        |
        v
Is there an explicit DENY in any policy?
   Yes --> DENY (explicit deny always wins)
   No  --> Continue
        |
        v
Is there an explicit ALLOW in bucket policy OR IAM policy?
   Yes --> For same-account: ALLOW
           For cross-account: ALLOW only if BOTH policies allow
   No  --> DENY (implicit deny)
```

## Common Mistakes

### 1. Conflicting bucket policy and ACL

**Wrong approach:** Setting a bucket policy that restricts access while an ACL grants public read:

```bash
# Grant public read via ACL (legacy approach)
aws s3api put-object-acl \
  --bucket my-bucket \
  --key "document.txt" \
  --acl public-read
```

**What happens:** If Block Public Access is enabled (as it should be), the ACL is silently ignored by `ignore_public_acls = true`. If Block Public Access is disabled, the ACL grants public read access even if the bucket policy does not mention public access. This creates a security gap where individual objects can be made public without the bucket policy reflecting it.

**Fix:** Use `BucketOwnerEnforced` ownership to disable ACLs entirely. Manage all access through bucket policies and IAM policies:

```hcl
resource "aws_s3_bucket_ownership_controls" "this" {
  bucket = aws_s3_bucket.this.id
  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}
```

### 2. Forgetting the resource format difference for bucket vs object actions

**Wrong approach:** Using only the bucket ARN for object-level actions:

```json
{
  "Effect": "Allow",
  "Action": ["s3:GetObject", "s3:ListBucket"],
  "Resource": "arn:aws:s3:::my-bucket"
}
```

**What happens:** `s3:ListBucket` works (it operates on the bucket resource), but `s3:GetObject` fails with `AccessDenied` because it operates on objects, which require the `/*` suffix.

**Fix:** Always include both the bucket ARN and the objects ARN:

```json
{
  "Effect": "Allow",
  "Action": "s3:ListBucket",
  "Resource": "arn:aws:s3:::my-bucket"
},
{
  "Effect": "Allow",
  "Action": "s3:GetObject",
  "Resource": "arn:aws:s3:::my-bucket/*"
}
```

### 3. Using Principal "*" without conditions

**Wrong approach:** Granting access to everyone without restriction:

```json
{
  "Effect": "Allow",
  "Principal": "*",
  "Action": "s3:GetObject",
  "Resource": "arn:aws:s3:::my-bucket/*"
}
```

**What happens:** This makes the entire bucket publicly readable. With Block Public Access enabled, this policy is blocked from being applied. Without Block Public Access, this is a data breach waiting to happen.

**Fix:** Always combine `Principal: "*"` with restrictive conditions (IP range, VPC endpoint, source account):

```json
{
  "Condition": {
    "StringEquals": {
      "aws:SourceVpce": "vpce-1234567890abcdef0"
    }
  }
}
```

## Verify What You Learned

```bash
BUCKET=$(terraform output -raw bucket_name)

# Verify Block Public Access is fully enabled
aws s3api get-public-access-block --bucket "$BUCKET" \
  --query 'PublicAccessBlockConfiguration.{BlockACLs:BlockPublicAcls,BlockPolicy:BlockPublicPolicy,IgnoreACLs:IgnorePublicAcls,RestrictPublic:RestrictPublicBuckets}' \
  --output table
```

Expected: All four values are `True`.

```bash
# Verify ACLs are disabled
aws s3api get-bucket-ownership-controls --bucket "$BUCKET" \
  --query 'OwnershipControls.Rules[0].ObjectOwnership' --output text
```

Expected: `BucketOwnerEnforced`

```bash
# Verify the HTTPS enforcement policy exists
aws s3api get-bucket-policy --bucket "$BUCKET" --output text | \
  python3 -c "import sys,json; p=json.load(sys.stdin); print([s['Sid'] for s in p['Statement']])"
```

Expected: `['AllowFromCorporateIP', 'EnforceHTTPS']`

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

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

Exercise 53 covers **S3 event notifications to Lambda, SQS, SNS, and EventBridge**. You will configure S3 to trigger Lambda functions on object creation and deletion events, compare direct S3 notifications with EventBridge for more complex routing, and learn about the permission model for cross-service invocation.

## Summary

- **Bucket policies** are resource-based JSON policies attached to the bucket -- the primary access control mechanism for S3
- **ACLs** are legacy and should be disabled (`BucketOwnerEnforced`) for new buckets -- exceptions exist for ALB/CloudFront logs
- **Block Public Access** is a safety net that overrides public grants in bucket policies and ACLs -- always enable all four settings
- **Explicit Deny always wins** in policy evaluation, regardless of any Allow in other policies
- **Cross-account access** requires both a bucket policy (granting access) and an IAM policy in the other account (allowing the action)
- **Resource ARN format** differs: `arn:aws:s3:::bucket` for bucket-level actions, `arn:aws:s3:::bucket/*` for object-level actions
- **Principal "*" without conditions** makes a bucket public -- always pair with restrictive conditions
- **HTTPS enforcement** via `aws:SecureTransport` condition is a security best practice for all buckets
- **20 KB policy size limit** per bucket -- use S3 Access Points when managing many consumers

## Reference

- [S3 Bucket Policies](https://docs.aws.amazon.com/AmazonS3/latest/userguide/bucket-policies.html)
- [S3 Block Public Access](https://docs.aws.amazon.com/AmazonS3/latest/userguide/access-control-block-public-access.html)
- [S3 ACLs](https://docs.aws.amazon.com/AmazonS3/latest/userguide/acl-overview.html)
- [Terraform aws_s3_bucket_policy](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket_policy)

## Additional Resources

- [Bucket Policy Examples](https://docs.aws.amazon.com/AmazonS3/latest/userguide/example-bucket-policies.html) -- 20+ common bucket policy patterns from AWS
- [S3 Policy Evaluation](https://docs.aws.amazon.com/AmazonS3/latest/userguide/how-s3-evaluates-access-control.html) -- detailed flowchart of how S3 evaluates access requests
- [Controlling Object Ownership](https://docs.aws.amazon.com/AmazonS3/latest/userguide/about-object-ownership.html) -- BucketOwnerEnforced vs BucketOwnerPreferred vs ObjectWriter
- [AWS Policy Simulator](https://policysim.aws.amazon.com/) -- test IAM and resource policies before deploying

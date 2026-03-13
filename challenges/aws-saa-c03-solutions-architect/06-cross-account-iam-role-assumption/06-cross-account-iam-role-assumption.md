# Cross-Account IAM Role Assumption

<!--
difficulty: intermediate
concepts: [cross-account-access, iam-trust-policy, external-id, confused-deputy, sts-assume-role, resource-based-vs-identity-based]
tools: [terraform, aws-cli]
estimated_time: 50m
bloom_level: design, justify, implement
prerequisites: [none]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise uses only IAM roles and S3 -- both free-tier eligible. The only charges come from S3 storage for test objects (~$0.01/hr). Destroy resources promptly after completing the exercise.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Single AWS account (exercise simulates cross-account) | `aws sts get-caller-identity --query Account` |
| jq installed for JSON parsing | `jq --version` |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Design** cross-account access patterns using IAM trust policies and external IDs.
2. **Justify** the use of external IDs for confused deputy prevention in third-party access scenarios.
3. **Implement** a complete role assumption workflow with STS temporary credentials.
4. **Compare** resource-based policies (S3 bucket policy) vs identity-based (assume role) for cross-account access.
5. **Evaluate** trust policy conditions (source IP, MFA, source account) and their security implications.

---

## Why This Matters

Cross-account access is one of the most architecturally significant patterns in AWS and a staple of the SAA-C03 exam. In any organization with more than one AWS account -- which is every organization following AWS best practices -- you need a secure mechanism for resources in Account A to access resources in Account B. The exam tests two distinct approaches: identity-based (assume a role in the target account) and resource-based (grant access directly via the resource's policy). Understanding when to use each, and why you might choose one over the other, is essential for both the exam and production architecture.

The confused deputy problem is a subtle but critical security concept that the exam tests directly. Imagine a SaaS provider that assumes a role in your account to manage your infrastructure. If the SaaS provider's own system is compromised, or if another customer tricks the provider into using your role ARN, the attacker gains access to your account. External IDs prevent this by adding a shared secret that only you and the provider know. This exercise walks you through the exact trust policy patterns you will see on the exam, and more importantly, helps you internalize why each condition exists -- not just what it does.

---

## Building Blocks

Create the following files in your exercise directory:

### `providers.tf`

```hcl
terraform {
  required_version = ">= 1.5"
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
  default     = "saa-ex06"
}

variable "external_id" {
  type        = string
  default     = "saa-ex06-xacct-2026"
  description = "External ID for confused deputy prevention"
  sensitive   = true
}
```

### `main.tf`

```hcl
# ---------- Data Sources ----------

data "aws_caller_identity" "current" {}

data "aws_iam_session_context" "current" {
  arn = data.aws_caller_identity.current.arn
}
```

### `storage.tf`

```hcl
resource "random_id" "bucket_suffix" {
  byte_length = 4
}

resource "aws_s3_bucket" "target" {
  bucket        = "${var.project_name}-target-${random_id.bucket_suffix.hex}"
  force_destroy = true
}

resource "aws_s3_bucket_public_access_block" "target" {
  bucket = aws_s3_bucket.target.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_object" "test_data" {
  bucket  = aws_s3_bucket.target.id
  key     = "confidential/report-2026-q1.json"
  content = jsonencode({
    report  = "Q1 Financial Summary"
    status  = "confidential"
    created = "2026-03-08"
  })
}
```

### `iam.tf`

```hcl
# ---------- Source Role (simulates "Account A" identity) ----------

resource "aws_iam_role" "source" {
  name = "${var.project_name}-source-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action    = "sts:AssumeRole"
      Effect    = "Allow"
      Principal = {
        AWS = data.aws_iam_session_context.current.issuer_arn
      }
    }]
  })
}

resource "aws_iam_role_policy" "source_assume_target" {
  name = "allow-assume-target-role"
  role = aws_iam_role.source.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = "sts:AssumeRole"
      Resource = aws_iam_role.target.arn
    }]
  })
}

# ---------- Target Role (simulates "Account B" role) ----------

resource "aws_iam_role" "target" {
  name = "${var.project_name}-target-role"

  # ============================================================
  # TODO 1: Write the Trust Policy for the Target Role
  # ============================================================
  # The trust policy controls WHO can assume this role.
  # Configure it so that ONLY the source role can assume it,
  # AND only when providing the correct external ID.
  #
  # Requirements:
  #   - Principal: { AWS = source role ARN }
  #   - Action: "sts:AssumeRole"
  #   - Effect: "Allow"
  #   - Condition: StringEquals with "sts:ExternalId" = var.external_id
  #
  # In a real cross-account scenario, the Principal would be
  # the ARN of the role/user in the OTHER account.
  #
  # Docs: https://docs.aws.amazon.com/IAM/latest/UserGuide/id_roles_create_for-user_externalid.html
  # ============================================================
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action    = "sts:AssumeRole"
      Effect    = "Allow"
      Principal = {
        AWS = "*"  # REPLACE THIS -- this is intentionally insecure as a starting point
      }
    }]
  })
}

# ============================================================
# TODO 2: Permission Policy for the Target Role
# ============================================================
# Grant the target role read-only access to the S3 bucket.
# This is the permission the "cross-account" caller receives
# after assuming the role.
#
# Requirements:
#   - Resource: aws_iam_role_policy
#   - Allow s3:GetObject, s3:ListBucket
#   - Restrict to the specific target bucket and its objects
#   - Use least privilege: no s3:PutObject, no s3:DeleteObject
#
# Docs: https://docs.aws.amazon.com/IAM/latest/UserGuide/reference_policies_examples_s3_rw-bucket.html
# ============================================================


# ============================================================
# TODO 3: External ID Configuration
# ============================================================
# The external ID is already defined as a variable above.
# This TODO requires you to:
#
#   a) Update the trust policy in TODO 1 to include a Condition
#      block requiring the external ID:
#      "Condition": {
#        "StringEquals": {
#          "sts:ExternalId": var.external_id
#        }
#      }
#
#   b) Document WHY external IDs matter (confused deputy):
#      - Without external ID: Any entity that knows your role ARN
#        can trick a third-party service into assuming it.
#      - With external ID: The third party must present a secret
#        that only your organization provided.
#
# The confused deputy attack flow:
#   1. You give SaaS-Provider your Role ARN to manage your AWS.
#   2. Attacker also uses SaaS-Provider for their account.
#   3. Attacker tells SaaS-Provider: "my role ARN is <YOUR ARN>".
#   4. SaaS-Provider assumes YOUR role on behalf of the attacker.
#   5. Attacker accesses YOUR resources via SaaS-Provider.
#
# External ID prevents step 4 because SaaS-Provider sends
# the attacker's external ID (not yours) with the AssumeRole call.
#
# Docs: https://docs.aws.amazon.com/IAM/latest/UserGuide/confused-deputy.html
# ============================================================


# ============================================================
# TODO 4: Demonstrate AssumeRole Workflow
# ============================================================
# This TODO is CLI-based. After terraform apply, run these
# commands to perform the full assumption workflow.
#
# Requirements:
#   a) Assume the source role first:
#      SOURCE_CREDS=$(aws sts assume-role \
#        --role-arn <SOURCE_ROLE_ARN> \
#        --role-session-name "source-session" \
#        --query 'Credentials')
#
#   b) Export source role credentials:
#      export AWS_ACCESS_KEY_ID=$(echo $SOURCE_CREDS | jq -r .AccessKeyId)
#      export AWS_SECRET_ACCESS_KEY=$(echo $SOURCE_CREDS | jq -r .SecretAccessKey)
#      export AWS_SESSION_TOKEN=$(echo $SOURCE_CREDS | jq -r .SessionToken)
#
#   c) From source role, assume the target role with external ID:
#      TARGET_CREDS=$(aws sts assume-role \
#        --role-arn <TARGET_ROLE_ARN> \
#        --role-session-name "target-session" \
#        --external-id "<EXTERNAL_ID>" \
#        --query 'Credentials')
#
#   d) Export target role credentials and access S3:
#      export AWS_ACCESS_KEY_ID=$(echo $TARGET_CREDS | jq -r .AccessKeyId)
#      export AWS_SECRET_ACCESS_KEY=$(echo $TARGET_CREDS | jq -r .SecretAccessKey)
#      export AWS_SESSION_TOKEN=$(echo $TARGET_CREDS | jq -r .SessionToken)
#      aws s3 ls s3://<BUCKET>/confidential/
#      aws s3 cp s3://<BUCKET>/confidential/report-2026-q1.json -
#
#   e) Verify write access is denied:
#      aws s3 cp /dev/null s3://<BUCKET>/confidential/should-fail.txt
#      # Expected: AccessDenied
#
# Docs: https://docs.aws.amazon.com/cli/latest/reference/sts/assume-role.html
# ============================================================


# ============================================================
# TODO 5: Add Condition Keys to Trust Policy
# ============================================================
# Enhance the trust policy with additional conditions for
# defense in depth.
#
# Requirements:
#   Add these conditions to the trust policy:
#
#   a) Source IP restriction (example CIDR):
#      "IpAddress": {
#        "aws:SourceIp": "203.0.113.0/24"
#      }
#
#   b) Require MFA (for user-based assumption):
#      "Bool": {
#        "aws:MultiFactorAuthPresent": "true"
#      }
#
#   c) Source account restriction:
#      "StringEquals": {
#        "aws:PrincipalOrgID": "o-exampleorgid"
#      }
#
# Note: For this lab, implement (a) using your current IP.
# Options (b) and (c) are for discussion — they require
# MFA devices and AWS Organizations respectively.
#
# IMPORTANT: aws:SourceIp does not work for calls made from
# within AWS (e.g., Lambda assuming a role). Use VPC endpoint
# conditions (aws:SourceVpce) for service-to-service access.
#
# Docs: https://docs.aws.amazon.com/IAM/latest/UserGuide/reference_policies_condition-keys.html
# ============================================================
```

### `outputs.tf`

```hcl
output "source_role_arn" {
  value = aws_iam_role.source.arn
}

output "target_role_arn" {
  value = aws_iam_role.target.arn
}

output "bucket_name" {
  value = aws_s3_bucket.target.id
}

output "external_id" {
  value     = var.external_id
  sensitive = true
}
```

---

## Spot the Bug

The following trust policy has a critical security vulnerability. Identify the flaw before expanding the answer.

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "AWS": "*"
      },
      "Action": "sts:AssumeRole"
    }
  ]
}
```

<details>
<summary>Explain the bug</summary>

This trust policy allows **any AWS principal in any AWS account** to assume the role. The `"Principal": {"AWS": "*"}` with no conditions is the IAM equivalent of leaving your front door open with a sign saying "come in."

Any person or service with an AWS account can call `sts:AssumeRole` with this role's ARN and receive temporary credentials with whatever permissions the role grants. This is not theoretical -- automated scanners regularly probe for roles with open trust policies.

**The severity compounds** based on what the role can do. If this role has `AdministratorAccess`, you have given every AWS user in the world admin access to your account.

**The fix requires two changes:**

1. **Restrict the Principal** to specific ARNs:
   ```json
   "Principal": {
     "AWS": "arn:aws:iam::123456789012:role/specific-role"
   }
   ```

2. **Add conditions** for defense in depth:
   ```json
   "Condition": {
     "StringEquals": {
       "sts:ExternalId": "unique-secret-id"
     }
   }
   ```

Even with a restricted Principal, always add an external ID for roles assumed by third parties. And always use `aws:PrincipalOrgID` when the assuming account is within your AWS Organization to prevent access from accounts that leave the organization.

</details>

---

## Verify What You Learned

1. **Deploy the infrastructure:**
   ```bash
   terraform init && terraform apply -auto-approve
   ```

2. **Verify both roles exist:**
   ```bash
   aws iam get-role --role-name saa-ex06-source-role --query 'Role.Arn'
   aws iam get-role --role-name saa-ex06-target-role --query 'Role.Arn'
   ```

3. **Inspect the trust policy on the target role:**
   ```bash
   aws iam get-role --role-name saa-ex06-target-role \
     --query 'Role.AssumeRolePolicyDocument' | jq .
   ```

4. **Perform the role assumption chain:**
   ```bash
   SOURCE_ARN=$(terraform output -raw source_role_arn)
   TARGET_ARN=$(terraform output -raw target_role_arn)
   EXTERNAL_ID=$(terraform output -raw external_id)
   BUCKET=$(terraform output -raw bucket_name)

   # Step 1: Assume source role
   SOURCE_CREDS=$(aws sts assume-role \
     --role-arn "$SOURCE_ARN" \
     --role-session-name "source-session" \
     --query 'Credentials' --output json)

   export AWS_ACCESS_KEY_ID=$(echo "$SOURCE_CREDS" | jq -r .AccessKeyId)
   export AWS_SECRET_ACCESS_KEY=$(echo "$SOURCE_CREDS" | jq -r .SecretAccessKey)
   export AWS_SESSION_TOKEN=$(echo "$SOURCE_CREDS" | jq -r .SessionToken)

   # Step 2: From source, assume target role with external ID
   TARGET_CREDS=$(aws sts assume-role \
     --role-arn "$TARGET_ARN" \
     --role-session-name "target-session" \
     --external-id "$EXTERNAL_ID" \
     --query 'Credentials' --output json)

   export AWS_ACCESS_KEY_ID=$(echo "$TARGET_CREDS" | jq -r .AccessKeyId)
   export AWS_SECRET_ACCESS_KEY=$(echo "$TARGET_CREDS" | jq -r .SecretAccessKey)
   export AWS_SESSION_TOKEN=$(echo "$TARGET_CREDS" | jq -r .SessionToken)

   # Step 3: Access S3 (should succeed)
   aws s3 cp "s3://$BUCKET/confidential/report-2026-q1.json" -

   # Step 4: Attempt write (should fail)
   echo "test" | aws s3 cp - "s3://$BUCKET/confidential/should-fail.txt" 2>&1 || true
   ```

5. **Test without external ID (should fail):**
   ```bash
   unset AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN

   SOURCE_CREDS=$(aws sts assume-role \
     --role-arn "$SOURCE_ARN" \
     --role-session-name "source-session" \
     --query 'Credentials' --output json)

   export AWS_ACCESS_KEY_ID=$(echo "$SOURCE_CREDS" | jq -r .AccessKeyId)
   export AWS_SECRET_ACCESS_KEY=$(echo "$SOURCE_CREDS" | jq -r .SecretAccessKey)
   export AWS_SESSION_TOKEN=$(echo "$SOURCE_CREDS" | jq -r .SessionToken)

   aws sts assume-role \
     --role-arn "$TARGET_ARN" \
     --role-session-name "no-external-id" 2>&1 || true
   # Expected: AccessDenied

   unset AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN
   ```

6. **Check CloudTrail for AssumeRole events:**
   ```bash
   aws cloudtrail lookup-events \
     --lookup-attributes AttributeKey=EventName,AttributeValue=AssumeRole \
     --max-items 5 \
     --query 'Events[*].{Time:EventTime,User:Username,Resource:Resources[0].ResourceName}'
   ```

---

## Cross-Account Access Comparison

| Approach | Mechanism | Credential Type | Use Case | Limitations |
|---|---|---|---|---|
| **IAM Role Assumption** | Trust policy + AssumeRole | Temporary (STS) | Service-to-service, human access | Requires role per relationship |
| **Resource-Based Policy** | Bucket/queue/key policy | Caller's own credentials | Direct resource sharing | Not all services support it |
| **AWS Organizations** | SCPs + sharing | Varies | Org-wide governance | Requires Organizations setup |
| **AWS RAM** | Resource sharing | Caller's own credentials | VPC subnets, Transit GW | Limited resource types |

**Exam tip:** Resource-based policies do NOT require the caller to give up their own permissions. When you assume a role, you get ONLY that role's permissions and lose your own. With a resource-based policy (e.g., S3 bucket policy granting cross-account access), you keep your identity-based permissions AND gain the resource-based grant. This distinction appears frequently on the SAA-C03.

---

## Solutions

<details>
<summary>iam.tf -- TODO 1: Trust Policy for Target Role</summary>

```hcl
assume_role_policy = jsonencode({
  Version = "2012-10-17"
  Statement = [{
    Action    = "sts:AssumeRole"
    Effect    = "Allow"
    Principal = {
      AWS = aws_iam_role.source.arn
    }
    Condition = {
      StringEquals = {
        "sts:ExternalId" = var.external_id
      }
    }
  }]
})
```

The Principal is restricted to only the source role, and the Condition requires the external ID. Both must be satisfied for assumption to succeed.

</details>

<details>
<summary>iam.tf -- TODO 2: Permission Policy for Target Role</summary>

```hcl
resource "aws_iam_role_policy" "target_s3_read" {
  name = "s3-read-only"
  role = aws_iam_role.target.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "s3:GetObject",
          "s3:GetObjectVersion"
        ]
        Resource = "${aws_s3_bucket.target.arn}/*"
      },
      {
        Effect   = "Allow"
        Action   = "s3:ListBucket"
        Resource = aws_s3_bucket.target.arn
      }
    ]
  })
}
```

Note the separation: `s3:GetObject` requires the `/*` suffix (object-level action), while `s3:ListBucket` requires the bucket ARN without suffix (bucket-level action). This is a common exam trick.

</details>

<details>
<summary>iam.tf -- TODO 3: External ID for Confused Deputy Prevention</summary>

The external ID is already configured in the trust policy from TODO 1. The key implementation detail is in the Condition block:

```json
"Condition": {
  "StringEquals": {
    "sts:ExternalId": "saa-ex06-xacct-2026"
  }
}
```

When calling `sts:AssumeRole`, the caller must include `--external-id saa-ex06-xacct-2026`. Without it, the call is denied even if the Principal matches.

**Confused deputy prevention flow:**
1. You generate a unique external ID (UUID or similar) and share it with the third party.
2. You embed the external ID in the trust policy Condition.
3. The third party stores the external ID and sends it with every AssumeRole call.
4. If an attacker provides your role ARN to the third party, the third party sends the attacker's external ID (not yours), and the assumption fails.

</details>

<details>
<summary>TODO 4 -- AssumeRole Workflow</summary>

```bash
SOURCE_ARN=$(terraform output -raw source_role_arn)
TARGET_ARN=$(terraform output -raw target_role_arn)
EXTERNAL_ID=$(terraform output -raw external_id)
BUCKET=$(terraform output -raw bucket_name)

# Assume source role
SOURCE_CREDS=$(aws sts assume-role \
  --role-arn "$SOURCE_ARN" \
  --role-session-name "source-session" \
  --query 'Credentials' --output json)

export AWS_ACCESS_KEY_ID=$(echo "$SOURCE_CREDS" | jq -r .AccessKeyId)
export AWS_SECRET_ACCESS_KEY=$(echo "$SOURCE_CREDS" | jq -r .SecretAccessKey)
export AWS_SESSION_TOKEN=$(echo "$SOURCE_CREDS" | jq -r .SessionToken)

# Verify identity
aws sts get-caller-identity

# Assume target role with external ID
TARGET_CREDS=$(aws sts assume-role \
  --role-arn "$TARGET_ARN" \
  --role-session-name "target-session" \
  --external-id "$EXTERNAL_ID" \
  --query 'Credentials' --output json)

export AWS_ACCESS_KEY_ID=$(echo "$TARGET_CREDS" | jq -r .AccessKeyId)
export AWS_SECRET_ACCESS_KEY=$(echo "$TARGET_CREDS" | jq -r .SecretAccessKey)
export AWS_SESSION_TOKEN=$(echo "$TARGET_CREDS" | jq -r .SessionToken)

# Verify new identity
aws sts get-caller-identity

# Read from S3 (should succeed)
aws s3 ls "s3://$BUCKET/confidential/"
aws s3 cp "s3://$BUCKET/confidential/report-2026-q1.json" -

# Write to S3 (should fail - read-only policy)
echo "unauthorized" | aws s3 cp - "s3://$BUCKET/confidential/should-fail.txt" 2>&1 || echo "Write denied as expected"

# Clean up environment
unset AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN
```

</details>

<details>
<summary>iam.tf -- TODO 5: Additional Trust Policy Conditions</summary>

Enhanced trust policy with source IP restriction:

```hcl
assume_role_policy = jsonencode({
  Version = "2012-10-17"
  Statement = [{
    Action    = "sts:AssumeRole"
    Effect    = "Allow"
    Principal = {
      AWS = aws_iam_role.source.arn
    }
    Condition = {
      StringEquals = {
        "sts:ExternalId" = var.external_id
      }
      IpAddress = {
        "aws:SourceIp" = "203.0.113.0/24"
      }
    }
  }]
})
```

To use your actual IP, replace the CIDR with your public IP:

```bash
MY_IP=$(curl -s https://checkip.amazonaws.com)
echo "Use CIDR: ${MY_IP}/32"
```

**Important caveats for the exam:**
- `aws:SourceIp` does NOT work for calls made from within AWS services (Lambda, EC2 without public IP). Use `aws:SourceVpce` or `aws:VpcSourceIp` for VPC-based access.
- `aws:MultiFactorAuthPresent` applies only to console and CLI sessions authenticated with MFA, not to role chaining.
- `aws:PrincipalOrgID` is the strongest control for organization-scoped access because it automatically excludes accounts that leave the org.

</details>

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify roles are deleted:

```bash
aws iam get-role --role-name saa-ex06-source-role 2>/dev/null && echo "Role still exists!" || echo "Source role deleted"
aws iam get-role --role-name saa-ex06-target-role 2>/dev/null && echo "Role still exists!" || echo "Target role deleted"
```

---

## What's Next

Exercise 07 introduces SQS decoupling patterns with dead-letter queues, FIFO ordering, and queue-depth-based auto scaling. You will apply the IAM patterns from this exercise when configuring Lambda execution roles and SQS access policies -- cross-service access within a single account follows the same Principal/Action/Resource model.

---

## Summary

You simulated cross-account access within a single AWS account by creating two IAM roles with a trust relationship, implemented external ID protection against the confused deputy problem, demonstrated the full STS AssumeRole workflow with temporary credentials, and compared identity-based vs resource-based cross-account access patterns. The key architectural insight is that cross-account access is not a single mechanism but a design decision: role assumption gives you fine-grained control and audit trails, while resource-based policies are simpler but less flexible. For the SAA-C03, remember that resource-based policies let the caller keep their own permissions, while role assumption replaces them entirely.

---

## Reference

- [IAM Tutorial: Cross-Account Access](https://docs.aws.amazon.com/IAM/latest/UserGuide/tutorial_cross-account-with-roles.html)
- [External ID for Third-Party Access](https://docs.aws.amazon.com/IAM/latest/UserGuide/id_roles_create_for-user_externalid.html)
- [The Confused Deputy Problem](https://docs.aws.amazon.com/IAM/latest/UserGuide/confused-deputy.html)
- [STS AssumeRole API](https://docs.aws.amazon.com/STS/latest/APIReference/API_AssumeRole.html)
- [IAM Policy Condition Keys](https://docs.aws.amazon.com/IAM/latest/UserGuide/reference_policies_condition-keys.html)

## Additional Resources

- [Terraform aws_iam_role](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_role)
- [AWS Cross-Account Access Patterns (Well-Architected)](https://docs.aws.amazon.com/wellarchitected/latest/security-pillar/identity-and-access-management.html)
- [Resource-Based Policies vs Identity-Based Policies](https://docs.aws.amazon.com/IAM/latest/UserGuide/access_policies_identity-vs-resource.html)
- [AWS Organizations SCPs](https://docs.aws.amazon.com/organizations/latest/userguide/orgs_manage_policies_scps.html)

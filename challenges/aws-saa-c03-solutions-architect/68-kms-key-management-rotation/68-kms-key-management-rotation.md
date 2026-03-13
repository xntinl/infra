# 68. KMS Key Management and Rotation

<!--
difficulty: intermediate
concepts: [kms, cmk, customer-managed-key, aws-managed-key, aws-owned-key, key-rotation, backing-key, key-policy, envelope-encryption, data-key, key-deletion, grants]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: apply, analyze
prerequisites: [65-iam-policies-identity-resource-scp]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** KMS customer-managed keys cost $1.00/month each (prorated). API calls cost $0.03/10,000 requests. For this exercise, costs are negligible (~$0.01/hr). Remember to schedule key deletion when finished (minimum 7-day waiting period).

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Completed exercise 65 (IAM policies) | Understanding of IAM policy evaluation |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Implement** a KMS customer-managed key with key policy, alias, and automatic rotation using Terraform.
2. **Distinguish** between the three CMK types: AWS managed (aws/service), customer managed (you control policy and rotation), and AWS owned (invisible to you).
3. **Apply** automatic key rotation and explain how it works: new backing key generated annually, old backing keys retained for decryption.
4. **Evaluate** the consequences of KMS key deletion: minimum 7-day waiting period, all data encrypted with the key becomes permanently inaccessible.
5. **Analyze** envelope encryption: KMS encrypts a data key, the data key encrypts the actual data -- why this pattern is used instead of encrypting data directly with KMS.

---

## Why This Matters

KMS appears on virtually every SAA-C03 exam question involving encryption. The exam tests three key concepts: (1) CMK types -- AWS managed keys are created automatically when you enable encryption on a service (S3, EBS, RDS), customer managed keys give you control over key policy and rotation, AWS owned keys are used internally by services and invisible to you; (2) key rotation -- automatic rotation generates a new backing key annually but keeps old backing keys so existing ciphertext can still be decrypted; (3) key deletion consequences -- deleting a KMS key is irreversible (after the waiting period), and all data encrypted with that key becomes permanently unrecoverable.

The architect decision is usually: AWS managed key vs customer managed key. AWS managed keys are simpler (no policy management, automatic rotation every year) but provide less control (you cannot change the key policy, share the key cross-account, or disable it independently). Customer managed keys are required when you need cross-account access to encrypted resources, custom key policy with specific grant conditions, or control over key enable/disable state.

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
  default     = "saa-ex68"
}
```

### `main.tf`

```hcl
data "aws_caller_identity" "current" {}
data "aws_region" "current" {}

# ============================================================
# TODO 1: Create a Customer-Managed KMS Key
# ============================================================
# Create symmetric key with enable_key_rotation = true.
# Key policy MUST include root account access statement
# (without it, the key becomes permanently unmanageable).
# Create an alias: alias/${var.project_name}-key
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/kms_key
# ============================================================


# ============================================================
# TODO 2: Encrypt and Decrypt Data (CLI)
# ============================================================
# Use aws kms encrypt/decrypt and generate-data-key.
# See Solutions section for full commands.
# Docs: https://docs.aws.amazon.com/kms/latest/developerguide/concepts.html#enveloping
# ============================================================


# ============================================================
# TODO 3: Verify Key Rotation Status (CLI)
# ============================================================
# aws kms get-key-rotation-status --key-id alias/saa-ex68-key
# Rotation generates new backing key annually; old keys
# retained for decrypting existing data. Key ID never changes.
# Docs: https://docs.aws.amazon.com/kms/latest/developerguide/rotate-keys.html
# ============================================================
```

### `outputs.tf`

```hcl
output "key_id" {
  value = "Set after TODO 1 implementation"
}

output "key_arn" {
  value = "Set after TODO 1 implementation"
}

output "key_alias" {
  value = "alias/${var.project_name}-key"
}
```

---

## CMK Type Comparison

| Criterion | AWS Managed | Customer Managed | AWS Owned |
|---|---|---|---|
| **Created by** | AWS (when you enable encryption on a service) | You | AWS |
| **Visible in KMS console** | Yes (aws/service-name) | Yes | No |
| **Key policy control** | No (AWS manages) | Full control | No |
| **Rotation** | Every year (automatic, cannot disable) | Every year (optional, you enable) | Varies |
| **Cross-account sharing** | No | Yes (via key policy) | No |
| **Cost** | No monthly fee ($0.03/10K API calls) | $1/month + $0.03/10K API calls | Free |
| **Disable/enable** | No | Yes | No |
| **Delete** | No (managed by AWS) | Yes (7-30 day waiting period) | No |
| **Use case** | Default encryption, simple compliance | Cross-account, custom policy, audit | Service-internal |

---

## Spot the Bug

A company decides to clean up unused KMS keys to reduce costs. An administrator identifies a key that "hasn't been used recently" and schedules it for immediate deletion:

```bash
# Administrator's cleanup script
KEY_ID="1234abcd-12ab-34cd-56ef-1234567890ab"

# Check last usage
aws kms describe-key --key-id "$KEY_ID" \
  --query 'KeyMetadata.{Created:CreationDate,State:KeyState}'

# Schedule deletion with minimum waiting period
aws kms schedule-key-deletion \
  --key-id "$KEY_ID" \
  --pending-window-in-days 7
```

Two weeks later, the application team reports that hundreds of S3 objects encrypted with this key are permanently unreadable.

<details>
<summary>Explain the bug</summary>

**Deleting a KMS key makes all data encrypted with that key permanently inaccessible.** The administrator checked when the key was "last used" but did not check what data was encrypted with the key. KMS key usage metrics only track API calls (Encrypt, Decrypt, GenerateDataKey), not the existence of data encrypted with the key. If S3 objects were encrypted months ago and no one has accessed them recently, the key appears "unused" but is still critical for decrypting that data.

The 7-day waiting period exists specifically to catch this mistake. During the waiting period, the key is in `PendingDeletion` state and cannot be used for encryption or decryption. This should surface errors in applications that try to decrypt data. However, if no application accesses the encrypted data during that 7-day window, the deletion proceeds silently.

**Fix:** Before deleting any KMS key:

1. **Disable the key first** (reversible) instead of deleting it:
   ```bash
   aws kms disable-key --key-id "$KEY_ID"
   ```
   Wait at least 30 days. If anything breaks, re-enable it:
   ```bash
   aws kms enable-key --key-id "$KEY_ID"
   ```

2. **Search for encrypted resources** that reference this key:
   ```bash
   # S3 buckets using this key
   for bucket in $(aws s3api list-buckets --query 'Buckets[*].Name' --output text); do
     aws s3api get-bucket-encryption --bucket "$bucket" 2>/dev/null | \
       grep -q "$KEY_ID" && echo "Bucket: $bucket"
   done

   # EBS volumes using this key
   aws ec2 describe-volumes \
     --filters Name=encrypted,Values=true \
     --query "Volumes[?KmsKeyId=='arn:aws:kms:us-east-1:ACCOUNT:key/$KEY_ID'].VolumeId"

   # RDS instances using this key
   aws rds describe-db-instances \
     --query "DBInstances[?KmsKeyId!=null].{Id:DBInstanceIdentifier,Key:KmsKeyId}" | \
     grep "$KEY_ID"
   ```

3. **Use the maximum waiting period** (30 days) if you must delete:
   ```bash
   aws kms schedule-key-deletion \
     --key-id "$KEY_ID" \
     --pending-window-in-days 30
   ```

4. **Set up CloudWatch alarm** for decrypt failures during the waiting period to catch dependencies you missed.

</details>

---

## Envelope Encryption Diagram

```
Encrypt:
  Application --> KMS: "Generate data key for key-id X"
  KMS --> Application: {plaintext_data_key, encrypted_data_key}
  Application: encrypts data locally with plaintext_data_key
  Application: discards plaintext_data_key from memory
  Application: stores encrypted_data + encrypted_data_key together

Decrypt:
  Application --> KMS: "Decrypt this encrypted_data_key"
  KMS --> Application: {plaintext_data_key}
  Application: decrypts data locally with plaintext_data_key
  Application: discards plaintext_data_key from memory

Why envelope encryption?
  - KMS has a 4 KB limit on direct encrypt/decrypt
  - Sending gigabytes to KMS over the network is impractical
  - Local encryption with a data key is fast (AES hardware acceleration)
  - Only the small data key needs KMS interaction (milliseconds)
  - Data keys are unique per object — compromising one key does not
    compromise other objects
```

---

## Verify What You Learned

1. **Deploy the key:**
   ```bash
   terraform init && terraform apply -auto-approve
   ```

2. **Verify the key:**
   ```bash
   aws kms describe-key --key-id alias/saa-ex68-key \
     --query 'KeyMetadata.{KeyId:KeyId,State:KeyState,Origin:Origin,Manager:KeyManager,Spec:KeySpec}' \
     --output json
   ```
   Expected: KeyState = `Enabled`, KeyManager = `CUSTOMER`, KeySpec = `SYMMETRIC_DEFAULT`.

3. **Verify rotation is enabled:**
   ```bash
   aws kms get-key-rotation-status --key-id alias/saa-ex68-key
   ```
   Expected: `KeyRotationEnabled = true`.

4. **Test encrypt/decrypt:**
   ```bash
   CIPHERTEXT=$(aws kms encrypt \
     --key-id alias/saa-ex68-key \
     --plaintext "Hello, KMS!" \
     --query CiphertextBlob --output text)

   aws kms decrypt \
     --ciphertext-blob fileb://<(echo "$CIPHERTEXT" | base64 --decode) \
     --query Plaintext --output text | base64 --decode
   ```
   Expected: `Hello, KMS!`

5. **Verify Terraform state:**
   ```bash
   terraform plan
   ```
   Expected: `No changes. Your infrastructure matches the configuration.`

---

## Solutions

<details>
<summary>TODO 1 -- Customer-Managed KMS Key (main.tf)</summary>

```hcl
resource "aws_kms_key" "this" {
  description             = "Customer-managed key for exercise 68"
  key_usage               = "ENCRYPT_DECRYPT"
  enable_key_rotation     = true
  deletion_window_in_days = 7

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid       = "EnableRootAccountAccess"
        Effect    = "Allow"
        Principal = { AWS = "arn:aws:iam::${data.aws_caller_identity.current.account_id}:root" }
        Action    = "kms:*"
        Resource  = "*"
      },
      {
        Sid       = "AllowKeyAdministration"
        Effect    = "Allow"
        Principal = { AWS = "arn:aws:iam::${data.aws_caller_identity.current.account_id}:root" }
        Action = [
          "kms:Create*",
          "kms:Describe*",
          "kms:Enable*",
          "kms:List*",
          "kms:Put*",
          "kms:Update*",
          "kms:Revoke*",
          "kms:Disable*",
          "kms:Get*",
          "kms:Delete*",
          "kms:ScheduleKeyDeletion",
          "kms:CancelKeyDeletion"
        ]
        Resource = "*"
      },
      {
        Sid       = "AllowKeyUsage"
        Effect    = "Allow"
        Principal = { AWS = "arn:aws:iam::${data.aws_caller_identity.current.account_id}:root" }
        Action = [
          "kms:Encrypt",
          "kms:Decrypt",
          "kms:ReEncrypt*",
          "kms:GenerateDataKey*",
          "kms:DescribeKey"
        ]
        Resource = "*"
      }
    ]
  })

  tags = { Name = "${var.project_name}-key" }
}

resource "aws_kms_alias" "this" {
  name          = "alias/${var.project_name}-key"
  target_key_id = aws_kms_key.this.key_id
}
```

Update `outputs.tf`:

```hcl
output "key_id" {
  value = aws_kms_key.this.key_id
}

output "key_arn" {
  value = aws_kms_key.this.arn
}
```

The root account statement is required in every key policy. Without it, the key becomes unmanageable -- no one can modify the key policy, and the key must be deleted via AWS Support.

</details>

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Note: KMS key deletion has a minimum 7-day waiting period. The key will enter `PendingDeletion` state immediately but will not be fully deleted for 7 days.

Verify:

```bash
aws kms describe-key --key-id alias/saa-ex68-key \
  --query 'KeyMetadata.KeyState' --output text 2>&1 || echo "Key alias deleted"
```

Expected: `PendingDeletion` (if checked within 7 days) or error (after deletion).

---

## What's Next

Exercise 69 covers **Secrets Manager vs Parameter Store** -- two services for storing configuration and secrets with different pricing, rotation capabilities, and cross-account sharing models. Understanding when to use each is a common exam question.

---

## Summary

- **Three CMK types:** AWS managed (automatic, per-service), customer managed (full control, $1/month), AWS owned (invisible, free)
- **Key rotation** generates a new backing key annually -- old backing keys are retained so existing ciphertext can still be decrypted
- **Key policy** is mandatory -- the root account statement is required or the key becomes permanently unmanageable
- **Key deletion** has a minimum 7-day waiting period -- all data encrypted with the key becomes permanently inaccessible after deletion
- **Disable before delete** -- disabling a key is reversible, deleting is not; always disable first and wait to catch dependencies
- **Envelope encryption** uses KMS to encrypt a data key, then the data key encrypts the actual data locally -- overcomes the 4 KB direct encryption limit
- **Cross-account access** requires a customer managed key with a key policy granting the external account access
- **AWS managed keys** are sufficient for most use cases -- customer managed keys are needed for cross-account, custom policy, or regulatory requirements
- **$1/month per key** for customer managed keys -- use aliases for easy reference and key rotation without updating application code
- **GenerateDataKey** is the most common KMS API call -- services like S3, EBS, and RDS call it automatically when encrypting data

## Reference

- [KMS Developer Guide](https://docs.aws.amazon.com/kms/latest/developerguide/overview.html)
- [Key Rotation](https://docs.aws.amazon.com/kms/latest/developerguide/rotate-keys.html)
- [Key Policies](https://docs.aws.amazon.com/kms/latest/developerguide/key-policies.html)
- [Terraform aws_kms_key](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/kms_key)

## Additional Resources

- [Envelope Encryption](https://docs.aws.amazon.com/kms/latest/developerguide/concepts.html#enveloping) -- detailed explanation of the data key pattern used by all AWS services
- [KMS Grants](https://docs.aws.amazon.com/kms/latest/developerguide/grants.html) -- temporary, scoped permissions for key access (used by EBS, RDS internally)
- [Multi-Region Keys](https://docs.aws.amazon.com/kms/latest/developerguide/multi-region-keys-overview.html) -- replicate keys across regions for cross-region encryption
- [KMS Best Practices](https://docs.aws.amazon.com/kms/latest/developerguide/best-practices.html) -- key management, monitoring, and security recommendations

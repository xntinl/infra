# 51. S3 Server-Side Encryption Comparison

<!--
difficulty: intermediate
concepts: [sse-s3, sse-kms, sse-c, aws-managed-key, customer-managed-cmk, customer-provided-key, kms-audit-trail, bucket-default-encryption, encryption-headers]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: apply, analyze
prerequisites: [47-s3-storage-classes-lifecycle-policies]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** S3 storage is negligible for test objects. KMS key usage costs $1/month per CMK plus $0.03 per 10,000 API calls. SSE-S3 and SSE-C have no additional encryption charges. Total for this exercise ~$0.01/hr. Remember to run `terraform destroy` when finished (the KMS key will be scheduled for deletion with a 7-day waiting period).

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| OpenSSL installed (for SSE-C key generation) | `openssl version` |
| Understanding of symmetric encryption concepts | N/A |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Apply** all three S3 server-side encryption methods (SSE-S3, SSE-KMS, SSE-C) using Terraform and CLI.
2. **Analyze** the key management responsibilities, audit capabilities, and cost differences between each method.
3. **Evaluate** which encryption method meets specific compliance requirements (audit trail, key rotation, customer-managed keys).
4. **Implement** bucket default encryption policies that enforce a specific encryption method.
5. **Diagnose** common encryption errors including SSE-C without HTTPS and missing KMS permissions.

---

## Why This Matters

S3 encryption is tested on every SAA-C03 exam because encryption-at-rest is a fundamental security requirement, and choosing the wrong method has real consequences for compliance, cost, and operational complexity. SSE-S3 is the simplest -- AWS manages everything, no extra cost, no audit trail of key usage. SSE-KMS adds a customer-managed key with CloudTrail audit logging of every encryption and decryption operation, which is required for compliance standards like PCI-DSS and HIPAA. SSE-C puts the encryption key entirely in the customer's hands -- AWS never stores the key, so losing the key means losing the data permanently.

The exam presents scenarios with specific requirements: "The security team must audit every access to encrypted objects" (SSE-KMS). "The company must retain full control of encryption keys and AWS must never store them" (SSE-C). "Encryption at rest is required with minimal operational overhead" (SSE-S3). Understanding the trade-offs -- cost, audit capability, key management responsibility, and operational complexity -- is what the exam tests, not just the names.

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
  default     = "saa-ex51"
}
```

### `storage.tf`

```hcl
data "aws_caller_identity" "current" {}

resource "random_id" "suffix" {
  byte_length = 4
}

# ---------- S3 Bucket with SSE-S3 Default Encryption ----------

resource "aws_s3_bucket" "this" {
  bucket        = "${var.project_name}-encryption-${random_id.suffix.hex}"
  force_destroy = true
}

resource "aws_s3_bucket_public_access_block" "this" {
  bucket = aws_s3_bucket.this.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# ------------------------------------------------------------------
# Default encryption: SSE-S3 (AES-256). This is the default for all
# new S3 buckets since January 2023. Every object is encrypted at
# rest with an AWS-managed key. No additional cost. No audit trail
# of key usage (the key is fully managed by S3).
# ------------------------------------------------------------------
resource "aws_s3_bucket_server_side_encryption_configuration" "sse_s3" {
  bucket = aws_s3_bucket.this.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
    bucket_key_enabled = false  # Not applicable for SSE-S3
  }
}


# ============================================================
# TODO 2: Upload Objects with Each Encryption Method
# ============================================================
# This TODO is CLI-based. After terraform apply, run these
# commands to upload objects with each encryption type.
#
# Requirements:
#   a) Upload with SSE-S3 (default, but explicit for clarity):
#      aws s3api put-object \
#        --bucket <BUCKET> \
#        --key "sse-s3/test.txt" \
#        --body <(echo "encrypted with SSE-S3") \
#        --server-side-encryption AES256
#
#   b) Upload with SSE-KMS (using your CMK from kms.tf):
#      aws s3api put-object \
#        --bucket <BUCKET> \
#        --key "sse-kms/test.txt" \
#        --body <(echo "encrypted with SSE-KMS") \
#        --server-side-encryption aws:kms \
#        --ssekms-key-id <KMS_KEY_ARN>
#
#   c) Upload with SSE-C (customer-provided key):
#      # Generate a 256-bit key
#      ENCRYPTION_KEY=$(openssl rand -base64 32)
#      ENCRYPTION_KEY_MD5=$(echo -n "$ENCRYPTION_KEY" | base64 -d | openssl dgst -md5 -binary | base64)
#
#      aws s3api put-object \
#        --bucket <BUCKET> \
#        --key "sse-c/test.txt" \
#        --body <(echo "encrypted with SSE-C") \
#        --sse-customer-algorithm AES256 \
#        --sse-customer-key "$ENCRYPTION_KEY" \
#        --sse-customer-key-md5 "$ENCRYPTION_KEY_MD5"
#
#   d) Verify encryption metadata on each object:
#      aws s3api head-object --bucket <BUCKET> --key "sse-s3/test.txt"
#      aws s3api head-object --bucket <BUCKET> --key "sse-kms/test.txt"
#      # SSE-C requires the key to even HEAD the object:
#      aws s3api head-object --bucket <BUCKET> --key "sse-c/test.txt" \
#        --sse-customer-algorithm AES256 \
#        --sse-customer-key "$ENCRYPTION_KEY" \
#        --sse-customer-key-md5 "$ENCRYPTION_KEY_MD5"
#
# Docs: https://docs.aws.amazon.com/AmazonS3/latest/userguide/specifying-s3-encryption.html
# ============================================================


# ============================================================
# TODO 3: Bucket Policy Enforcing SSE-KMS
# ============================================================
# Create a bucket policy that denies PutObject requests unless
# the object uses SSE-KMS encryption with your specific CMK.
#
# Requirements:
#   - Resource: aws_s3_bucket_policy
#   - Deny PutObject where:
#     - s3:x-amz-server-side-encryption != "aws:kms"
#     OR
#     - s3:x-amz-server-side-encryption-aws-kms-key-id != <KMS_KEY_ARN>
#
# This prevents anyone from uploading unencrypted objects or
# objects encrypted with a different key.
#
# Docs: https://docs.aws.amazon.com/AmazonS3/latest/userguide/UsingKMSEncryption.html
# ============================================================
```

### `kms.tf`

```hcl
# ============================================================
# TODO 1: KMS Customer Managed Key for SSE-KMS
# ============================================================
# Create a KMS key that will be used for SSE-KMS encryption.
#
# Requirements:
#   - Resource: aws_kms_key
#   - description = "S3 SSE-KMS key for exercise 51"
#   - deletion_window_in_days = 7 (minimum)
#   - enable_key_rotation = true (automatic annual rotation)
#   - Resource: aws_kms_alias
#   - name = "alias/${var.project_name}-s3-key"
#
# Why a customer managed key instead of the default aws/s3 key?
#   - Custom key policy (restrict who can use the key)
#   - CloudTrail logging of every Encrypt/Decrypt/GenerateDataKey
#   - Cross-account access (share the key with other accounts)
#   - Automatic rotation (CMK rotates annually; aws/s3 does not)
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/kms_key
# ============================================================
```

### `outputs.tf`

```hcl
output "bucket_name" {
  value = aws_s3_bucket.this.id
}

output "bucket_arn" {
  value = aws_s3_bucket.this.arn
}
```

---

## Encryption Method Decision Table

| Criterion | SSE-S3 | SSE-KMS (aws/s3 key) | SSE-KMS (CMK) | SSE-C |
|---|---|---|---|---|
| **Who manages the key** | AWS (fully) | AWS (shared) | Customer (in KMS) | Customer (externally) |
| **Key stored by AWS** | Yes | Yes | Yes (in KMS) | No |
| **CloudTrail audit** | No | Yes | Yes | No |
| **Key rotation** | Automatic | Not configurable | Annual auto-rotation | Customer responsibility |
| **Cross-account access** | N/A | No | Yes (via key policy) | N/A |
| **Cost** | Free | $0.03/10K requests | $1/month/key + $0.03/10K | Free |
| **HTTPS required** | No (always encrypted) | No | No | **Yes (mandatory)** |
| **Key loss risk** | None | None | Low (KMS) | **High (permanent data loss)** |
| **Compliance use case** | Basic encryption-at-rest | Audit trail needed | Full control + audit | Key never leaves customer |
| **Bucket Key support** | N/A | Yes | Yes (reduces KMS calls) | N/A |

---

## Spot the Bug

The following SSE-C upload command has a critical protocol error. Identify the problem before expanding the answer.

```bash
ENCRYPTION_KEY=$(openssl rand -base64 32)
ENCRYPTION_KEY_MD5=$(echo -n "$ENCRYPTION_KEY" | base64 -d | openssl dgst -md5 -binary | base64)

# Upload via HTTP (not HTTPS)
aws s3api put-object \
  --bucket my-bucket \
  --key "sensitive/data.txt" \
  --body sensitive-data.txt \
  --sse-customer-algorithm AES256 \
  --sse-customer-key "$ENCRYPTION_KEY" \
  --sse-customer-key-md5 "$ENCRYPTION_KEY_MD5" \
  --endpoint-url http://s3.amazonaws.com
```

<details>
<summary>Explain the bug</summary>

**SSE-C requires HTTPS. The command uses `--endpoint-url http://s3.amazonaws.com` (HTTP, not HTTPS).**

When you use SSE-C, the encryption key is sent in the HTTP headers (`x-amz-server-side-encryption-customer-key`). If the request is sent over plain HTTP, the encryption key travels in cleartext across the network, completely defeating the purpose of customer-managed encryption.

AWS enforces this at the API level: **S3 will reject any SSE-C request that is not sent over HTTPS** with a `400 Bad Request` error. This is not optional -- the rejection is hard-coded in the S3 service.

**Fix:** Remove the `--endpoint-url` override (the AWS CLI uses HTTPS by default), or explicitly use HTTPS:

```bash
aws s3api put-object \
  --bucket my-bucket \
  --key "sensitive/data.txt" \
  --body sensitive-data.txt \
  --sse-customer-algorithm AES256 \
  --sse-customer-key "$ENCRYPTION_KEY" \
  --sse-customer-key-md5 "$ENCRYPTION_KEY_MD5"
```

Additional SSE-C risks to remember:
- AWS does not store the encryption key. If you lose the key, the data is permanently unrecoverable.
- You must provide the same key for GET, HEAD, and copy operations.
- SSE-C objects cannot be replicated with CRR unless the destination also supports SSE-C or you re-encrypt during replication.

</details>

---

## Verify What You Learned

1. **Deploy the infrastructure:**
   ```bash
   terraform init && terraform apply -auto-approve
   ```

2. **Upload objects with each encryption method:**
   ```bash
   BUCKET=$(terraform output -raw bucket_name)
   KMS_KEY_ARN=$(aws kms describe-key --key-id alias/saa-ex51-s3-key \
     --query 'KeyMetadata.Arn' --output text)

   # SSE-S3
   echo "SSE-S3 data" | aws s3api put-object \
     --bucket "$BUCKET" --key "sse-s3/test.txt" \
     --body /dev/stdin --server-side-encryption AES256

   # SSE-KMS
   echo "SSE-KMS data" | aws s3api put-object \
     --bucket "$BUCKET" --key "sse-kms/test.txt" \
     --body /dev/stdin --server-side-encryption aws:kms \
     --ssekms-key-id "$KMS_KEY_ARN"
   ```

3. **Verify encryption metadata:**
   ```bash
   aws s3api head-object --bucket "$BUCKET" --key "sse-s3/test.txt" \
     --query 'ServerSideEncryption'
   ```
   Expected: `"AES256"`

   ```bash
   aws s3api head-object --bucket "$BUCKET" --key "sse-kms/test.txt" \
     --query '{Encryption:ServerSideEncryption,KeyId:SSEKMSKeyId}'
   ```
   Expected: `Encryption = "aws:kms"` with the CMK ARN.

4. **Upload and retrieve with SSE-C:**
   ```bash
   ENCRYPTION_KEY=$(openssl rand -base64 32)
   ENCRYPTION_KEY_MD5=$(echo -n "$ENCRYPTION_KEY" | base64 -d | openssl dgst -md5 -binary | base64)

   echo "SSE-C data" | aws s3api put-object \
     --bucket "$BUCKET" --key "sse-c/test.txt" \
     --body /dev/stdin --sse-customer-algorithm AES256 \
     --sse-customer-key "$ENCRYPTION_KEY" \
     --sse-customer-key-md5 "$ENCRYPTION_KEY_MD5"

   # Retrieve -- must provide the key
   aws s3api get-object \
     --bucket "$BUCKET" --key "sse-c/test.txt" \
     --sse-customer-algorithm AES256 \
     --sse-customer-key "$ENCRYPTION_KEY" \
     --sse-customer-key-md5 "$ENCRYPTION_KEY_MD5" \
     /dev/stdout
   ```

5. **Verify Terraform state:**
   ```bash
   terraform plan
   ```
   Expected: `No changes. Your infrastructure matches the configuration.`

---

## Solutions

<details>
<summary>TODO 1 -- KMS Customer Managed Key (kms.tf)</summary>

```hcl
resource "aws_kms_key" "s3" {
  description             = "S3 SSE-KMS key for exercise 51"
  deletion_window_in_days = 7
  enable_key_rotation     = true
}

resource "aws_kms_alias" "s3" {
  name          = "alias/${var.project_name}-s3-key"
  target_key_id = aws_kms_key.s3.key_id
}
```

With `enable_key_rotation = true`, AWS automatically rotates the key material annually. Previous key material is retained for decryption of objects encrypted before rotation. This satisfies compliance requirements for key rotation without operational burden.

</details>

<details>
<summary>TODO 2 -- Upload Objects (CLI)</summary>

```bash
BUCKET=$(terraform output -raw bucket_name)
KMS_KEY_ARN=$(aws kms describe-key --key-id alias/saa-ex51-s3-key \
  --query 'KeyMetadata.Arn' --output text)

# SSE-S3
echo "encrypted with SSE-S3" | aws s3api put-object \
  --bucket "$BUCKET" --key "sse-s3/test.txt" \
  --body /dev/stdin --server-side-encryption AES256

# SSE-KMS with CMK
echo "encrypted with SSE-KMS" | aws s3api put-object \
  --bucket "$BUCKET" --key "sse-kms/test.txt" \
  --body /dev/stdin --server-side-encryption aws:kms \
  --ssekms-key-id "$KMS_KEY_ARN"

# SSE-C
ENCRYPTION_KEY=$(openssl rand -base64 32)
ENCRYPTION_KEY_MD5=$(echo -n "$ENCRYPTION_KEY" | base64 -d | openssl dgst -md5 -binary | base64)

echo "encrypted with SSE-C" | aws s3api put-object \
  --bucket "$BUCKET" --key "sse-c/test.txt" \
  --body /dev/stdin --sse-customer-algorithm AES256 \
  --sse-customer-key "$ENCRYPTION_KEY" \
  --sse-customer-key-md5 "$ENCRYPTION_KEY_MD5"

echo "Save this key! Losing it means permanent data loss: $ENCRYPTION_KEY"
```

</details>

<details>
<summary>TODO 3 -- Bucket Policy Enforcing SSE-KMS (storage.tf)</summary>

```hcl
resource "aws_s3_bucket_policy" "enforce_kms" {
  bucket = aws_s3_bucket.this.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid       = "DenyNonKMSEncryption"
        Effect    = "Deny"
        Principal = "*"
        Action    = "s3:PutObject"
        Resource  = "${aws_s3_bucket.this.arn}/*"
        Condition = {
          StringNotEquals = {
            "s3:x-amz-server-side-encryption" = "aws:kms"
          }
        }
      },
      {
        Sid       = "DenyWrongKMSKey"
        Effect    = "Deny"
        Principal = "*"
        Action    = "s3:PutObject"
        Resource  = "${aws_s3_bucket.this.arn}/*"
        Condition = {
          StringNotEquals = {
            "s3:x-amz-server-side-encryption-aws-kms-key-id" = aws_kms_key.s3.arn
          }
        }
      }
    ]
  })
}
```

This policy rejects any upload that does not use SSE-KMS or uses a different KMS key. After applying this, SSE-S3 and SSE-C uploads to this bucket will fail with `AccessDenied`.

</details>

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Note: The KMS key will be scheduled for deletion with a 7-day waiting period. No charges accrue during the pending deletion period.

```bash
terraform state list
```

Expected: no output (empty state).

---

## What's Next

Exercise 52 covers **S3 bucket policies and ACLs**, where you will explore resource-based access control in depth -- writing bucket policies for cross-account access, understanding the interaction between bucket policies, ACLs, and Block Public Access settings, and diagnosing common policy conflicts.

---

## Summary

- **SSE-S3 (AES-256)** is the default encryption for all S3 buckets -- AWS manages the key, no cost, no audit trail
- **SSE-KMS** encrypts with a KMS key, providing CloudTrail audit logging of every encryption/decryption operation
- **SSE-KMS with CMK** adds customer control over the key policy, automatic annual rotation, and cross-account key sharing
- **SSE-C** puts the customer in full control -- AWS never stores the key, losing it means permanent data loss
- **SSE-C requires HTTPS** -- S3 rejects SSE-C requests over HTTP to prevent key exposure in transit
- **Bucket Key** (for SSE-KMS) reduces KMS API calls by generating a bucket-level key, reducing costs by up to 99%
- **Bucket default encryption** ensures all objects are encrypted even if the uploader does not specify encryption
- **Bucket policy enforcement** can deny uploads that do not use a specific encryption method or KMS key
- Since January 2023, **all new S3 objects are encrypted by default** with SSE-S3 (AES-256) -- unencrypted objects no longer exist

## Reference

- [S3 Server-Side Encryption](https://docs.aws.amazon.com/AmazonS3/latest/userguide/serv-side-encryption.html)
- [SSE-KMS](https://docs.aws.amazon.com/AmazonS3/latest/userguide/UsingKMSEncryption.html)
- [SSE-C](https://docs.aws.amazon.com/AmazonS3/latest/userguide/ServerSideEncryptionCustomerKeys.html)
- [S3 Bucket Key](https://docs.aws.amazon.com/AmazonS3/latest/userguide/bucket-key.html)

## Additional Resources

- [KMS Key Rotation](https://docs.aws.amazon.com/kms/latest/developerguide/rotate-keys.html) -- how automatic rotation works and when to use manual rotation
- [Terraform aws_kms_key](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/kms_key) -- KMS key resource with key policy examples
- [S3 Default Encryption](https://docs.aws.amazon.com/AmazonS3/latest/userguide/bucket-encryption.html) -- configuring bucket-level default encryption
- [CloudTrail for KMS](https://docs.aws.amazon.com/kms/latest/developerguide/logging-using-cloudtrail.html) -- auditing KMS key usage for compliance

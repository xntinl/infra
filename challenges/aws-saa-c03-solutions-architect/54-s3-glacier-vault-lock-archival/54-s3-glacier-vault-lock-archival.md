# 54. S3 Glacier Vault Lock and Archival Strategies

<!--
difficulty: intermediate
concepts: [glacier-vault-lock, vault-lock-policy, 24hr-confirmation, lifecycle-archival, retrieval-tiers, expedited-retrieval, standard-retrieval, bulk-retrieval, compliance-archival]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: apply, analyze
prerequisites: [47-s3-storage-classes-lifecycle-policies, 48-s3-versioning-mfa-delete-object-lock]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** S3 Glacier storage is $0.0036/GB/month (Flexible) or $0.00099/GB/month (Deep Archive). Retrieval costs vary by tier. Vault Lock policies are free. Total for this exercise with small test data ~$0.01/hr. Remember to run `terraform destroy` when finished. Important: completed vault locks cannot be removed -- use `abort-vault-lock` during the 24-hour confirmation window if testing.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Completed exercise 47 (S3 Storage Classes) | Lifecycle policy concepts |
| Completed exercise 48 (Object Lock) | WORM storage concepts |
| Understanding of compliance requirements | N/A |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Implement** a Glacier Vault Lock policy with the 24-hour confirmation workflow.
2. **Analyze** the differences between S3 Object Lock (bucket-level WORM) and Glacier Vault Lock (vault-level compliance).
3. **Apply** lifecycle rules that automatically transition S3 objects to Glacier storage classes after specified periods.
4. **Evaluate** the three Glacier retrieval tiers (Expedited, Standard, Bulk) based on time, cost, and use case.
5. **Design** a compliant archival strategy that meets regulatory retention requirements at minimum cost.

---

## Why This Matters

Glacier Vault Lock is the AWS mechanism for regulatory compliance in archival storage, and the SAA-C03 exam tests it with specific scenario questions. Financial services (SEC Rule 17a-4), healthcare (HIPAA), and government agencies require that archived data cannot be deleted or modified for specified retention periods. Vault Lock makes this guarantee immutable -- once the lock is confirmed after the 24-hour window, even the root account cannot remove or modify the policy.

The architectural trade-off is between S3 Object Lock (newer, per-object, integrated with S3) and Glacier Vault Lock (vault-level, all-or-nothing). Object Lock provides finer granularity -- different retention periods per object, governance vs compliance modes. Vault Lock applies a single policy to the entire vault. The exam also heavily tests retrieval tiers: Expedited (1-5 minutes, $0.03/GB), Standard (3-5 hours, $0.01/GB), and Bulk (5-12 hours, $0.0025/GB). Choosing the wrong tier means either unexpected costs or unacceptable wait times. Not specifying a tier defaults to Standard, which catches many architects off guard when they expect instant access.

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
  default     = "saa-ex54"
}
```

### `storage.tf`

```hcl
resource "random_id" "suffix" {
  byte_length = 4
}

# ---------- S3 Bucket for Lifecycle Archival ----------

resource "aws_s3_bucket" "archival" {
  bucket        = "${var.project_name}-archival-${random_id.suffix.hex}"
  force_destroy = true
}

resource "aws_s3_bucket_versioning" "archival" {
  bucket = aws_s3_bucket.archival.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_public_access_block" "archival" {
  bucket = aws_s3_bucket.archival.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# ============================================================
# TODO 1: Lifecycle Rule for Automatic Archival
# ============================================================
# Create a lifecycle configuration that transitions objects
# through storage classes for a 7-year compliance retention.
#
# Requirements:
#   - Resource: aws_s3_bucket_lifecycle_configuration
#   - Rule 1 (compliance-archive):
#     - filter prefix = "compliance/"
#     - Transition to STANDARD_IA after 30 days
#     - Transition to GLACIER after 90 days
#     - Transition to DEEP_ARCHIVE after 365 days
#     - Expire objects after 2555 days (7 years)
#   - Rule 2 (abort-multipart):
#     - Abort incomplete multipart uploads after 7 days
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket_lifecycle_configuration
# ============================================================


# ============================================================
# TODO 2: Glacier Vault for Direct Archival
# ============================================================
# Create a Glacier vault for direct archive uploads (not via
# S3 lifecycle). Some compliance scenarios require direct
# Glacier usage for audit trail purposes.
#
# Requirements:
#   - Resource: aws_glacier_vault
#   - name = "${var.project_name}-compliance-vault"
#   - Notification configuration (optional):
#     - SNS topic for job completion notifications
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/glacier_vault
# ============================================================


# ============================================================
# TODO 3: Vault Lock Policy (Initiate Only)
# ============================================================
# Initiate a vault lock policy that prevents deletion of
# archives younger than 365 days. This is CLI-based.
#
# Requirements:
#   a) Create the vault lock policy JSON:
#      {
#        "Version": "2012-10-17",
#        "Statement": [{
#          "Sid": "DenyDeleteUnder365Days",
#          "Effect": "Deny",
#          "Principal": "*",
#          "Action": "glacier:DeleteArchive",
#          "Resource": "arn:aws:glacier:us-east-1:ACCOUNT:vaults/VAULT_NAME",
#          "Condition": {
#            "NumericLessThan": {
#              "glacier:ArchiveAgeInDays": "365"
#            }
#          }
#        }]
#      }
#
#   b) Initiate the vault lock:
#      aws glacier initiate-vault-lock \
#        --vault-name VAULT_NAME \
#        --policy '...' \
#        --query 'lockId' --output text
#
#   c) The lock enters a 24-hour confirmation window.
#      During this window, you can:
#      - Test the policy effects
#      - abort-vault-lock to cancel
#      - complete-vault-lock to make it permanent
#
#   d) For this exercise, ABORT the lock (do not complete it):
#      aws glacier abort-vault-lock --vault-name VAULT_NAME
#
# WARNING: complete-vault-lock makes the policy PERMANENT.
# It CANNOT be removed, changed, or overridden by anyone.
#
# Docs: https://docs.aws.amazon.com/amazonglacier/latest/dev/vault-lock.html
# ============================================================


# ============================================================
# TODO 4: Glacier Retrieval Demonstration
# ============================================================
# This TODO is CLI-based. After objects have been transitioned
# to Glacier (or uploaded directly), demonstrate retrieval.
#
# For S3 objects in Glacier storage class, use restore-object:
#   aws s3api restore-object \
#     --bucket BUCKET \
#     --key "compliance/old-file.txt" \
#     --restore-request '{
#       "Days": 7,
#       "GlacierJobParameters": {
#         "Tier": "Standard"
#       }
#     }'
#
# Retrieval tiers:
#   Expedited: 1-5 minutes, $0.03/GB + $10/1000 requests
#   Standard:  3-5 hours,   $0.01/GB + $0.05/1000 requests
#   Bulk:      5-12 hours,  $0.0025/GB + $0.025/1000 requests
#
# For Deep Archive:
#   Standard: 12 hours, $0.02/GB
#   Bulk:     48 hours, $0.0025/GB
#   (Expedited is NOT available for Deep Archive)
#
# Docs: https://docs.aws.amazon.com/AmazonS3/latest/userguide/restoring-objects.html
# ============================================================
```

### `outputs.tf`

```hcl
output "archival_bucket" {
  value = aws_s3_bucket.archival.id
}
```

---

## Retrieval Tier Comparison

| Tier | Glacier Flexible | Deep Archive | Cost/GB | Request Cost | Use Case |
|---|---|---|---|---|---|
| **Expedited** | 1-5 minutes | N/A | $0.03 | $10/1K requests | Urgent, small retrievals |
| **Standard** | 3-5 hours | 12 hours | $0.01 | $0.05/1K requests | Default, regular access |
| **Bulk** | 5-12 hours | 48 hours | $0.0025 | $0.025/1K requests | Large batch, cost-optimized |

### Provisioned Capacity for Expedited

Expedited retrievals can fail during peak demand. For guaranteed Expedited access, purchase provisioned capacity units ($100/unit/month). Each unit provides at least 3 Expedited retrievals every 5 minutes and up to 150 MB/s throughput.

---

## Vault Lock vs Object Lock Decision Framework

| Criterion | Glacier Vault Lock | S3 Object Lock |
|---|---|---|
| **Scope** | Entire vault | Per-object |
| **Granularity** | Single policy for all archives | Different retention per object |
| **Modes** | Compliance only (immutable) | Governance + Compliance |
| **Confirmation** | 24-hour window | Immediate |
| **Reversibility** | Never (after confirmation) | Governance: with permission |
| **Storage types** | Glacier vaults only | Any S3 storage class |
| **SEC 17a-4** | Yes | Yes (Compliance mode) |
| **Recommended for** | Pure archival workloads | Mixed access + retention |

---

## Spot the Bug

The following code restores a Glacier object but the user reports an unexpected 3-5 hour wait. Identify the problem before expanding the answer.

```bash
# Restore an object from Glacier Flexible Retrieval
aws s3api restore-object \
  --bucket my-archival-bucket \
  --key "compliance/audit-log-2024.zip" \
  --restore-request '{"Days": 7}'

echo "Object restored! Downloading now..."
aws s3 cp "s3://my-archival-bucket/compliance/audit-log-2024.zip" ./audit-log.zip
```

<details>
<summary>Explain the bug</summary>

**Two bugs:**

**Bug 1: No retrieval tier specified.** When `GlacierJobParameters.Tier` is omitted, S3 defaults to `Standard` tier, which takes 3-5 hours. The developer expected immediate access (perhaps thinking Expedited was the default). This is one of the most common Glacier mistakes.

**Bug 2: The download command runs immediately after initiating the restore.** `restore-object` is asynchronous -- it initiates a retrieval job but does not wait for it to complete. The `aws s3 cp` command will fail with a `403` error or return an empty file because the object is not yet restored.

**Fix:** Specify the tier explicitly and poll for restoration status:

```bash
# Specify Expedited tier for fast retrieval
aws s3api restore-object \
  --bucket my-archival-bucket \
  --key "compliance/audit-log-2024.zip" \
  --restore-request '{"Days": 7, "GlacierJobParameters": {"Tier": "Expedited"}}'

# Poll until restoration is complete
while true; do
  STATUS=$(aws s3api head-object \
    --bucket my-archival-bucket \
    --key "compliance/audit-log-2024.zip" \
    --query 'Restore' --output text)

  echo "Restore status: $STATUS"

  if echo "$STATUS" | grep -q 'ongoing-request="false"'; then
    echo "Object restored! Downloading..."
    aws s3 cp "s3://my-archival-bucket/compliance/audit-log-2024.zip" ./audit-log.zip
    break
  fi

  sleep 60
done
```

For Deep Archive, Expedited is not available. Plan for 12-48 hours retrieval time.

</details>

---

## Verify What You Learned

1. **Deploy the infrastructure:**
   ```bash
   terraform init && terraform apply -auto-approve
   ```

2. **Upload test objects to the compliance prefix:**
   ```bash
   BUCKET=$(terraform output -raw archival_bucket)
   echo "compliance data from 2024" | aws s3 cp - "s3://$BUCKET/compliance/audit-2024.txt"
   echo "compliance data from 2025" | aws s3 cp - "s3://$BUCKET/compliance/audit-2025.txt"
   ```

3. **Verify lifecycle rules are configured:**
   ```bash
   aws s3api get-bucket-lifecycle-configuration \
     --bucket "$BUCKET" \
     --query 'Rules[*].{Id:ID,Status:Status,Transitions:Transitions[*].{Days:Days,Class:StorageClass}}' \
     --output json
   ```
   Expected: Rules showing transitions at 30, 90, 365 days.

4. **Check that objects are currently in STANDARD:**
   ```bash
   aws s3api list-objects-v2 \
     --bucket "$BUCKET" \
     --prefix "compliance/" \
     --query 'Contents[*].{Key:Key,StorageClass:StorageClass}' \
     --output table
   ```
   Expected: `STANDARD` storage class (transitions have not occurred yet).

5. **Verify Glacier vault exists (if implemented):**
   ```bash
   aws glacier describe-vault \
     --vault-name saa-ex54-compliance-vault \
     --query '{Name:VaultName,ARN:VaultARN,Created:CreationDate}' 2>/dev/null || echo "Vault not yet created"
   ```

6. **Verify Terraform state:**
   ```bash
   terraform plan
   ```
   Expected: `No changes. Your infrastructure matches the configuration.`

---

## Solutions

<details>
<summary>TODO 1 -- Lifecycle Rule for Automatic Archival (storage.tf)</summary>

```hcl
resource "aws_s3_bucket_lifecycle_configuration" "archival" {
  bucket = aws_s3_bucket.archival.id

  rule {
    id     = "compliance-archive"
    status = "Enabled"

    filter {
      prefix = "compliance/"
    }

    transition {
      days          = 30
      storage_class = "STANDARD_IA"
    }

    transition {
      days          = 90
      storage_class = "GLACIER"
    }

    transition {
      days          = 365
      storage_class = "DEEP_ARCHIVE"
    }

    expiration {
      days = 2555  # 7 years
    }
  }

  rule {
    id     = "abort-multipart"
    status = "Enabled"

    filter {
      prefix = ""
    }

    abort_incomplete_multipart_upload {
      days_after_initiation = 7
    }
  }
}
```

</details>

<details>
<summary>TODO 2 -- Glacier Vault (storage.tf)</summary>

```hcl
resource "aws_glacier_vault" "compliance" {
  name = "${var.project_name}-compliance-vault"

  tags = {
    Name = "${var.project_name}-compliance-vault"
  }
}
```

</details>

<details>
<summary>TODO 3 -- Vault Lock Policy (CLI, storage.tf)</summary>

```bash
VAULT_NAME="saa-ex54-compliance-vault"
ACCOUNT=$(aws sts get-caller-identity --query Account --output text)

# Initiate vault lock (24-hour confirmation window starts)
LOCK_ID=$(aws glacier initiate-vault-lock \
  --vault-name "$VAULT_NAME" \
  --policy "{
    \"Version\": \"2012-10-17\",
    \"Statement\": [{
      \"Sid\": \"DenyDeleteUnder365Days\",
      \"Effect\": \"Deny\",
      \"Principal\": \"*\",
      \"Action\": \"glacier:DeleteArchive\",
      \"Resource\": \"arn:aws:glacier:us-east-1:${ACCOUNT}:vaults/${VAULT_NAME}\",
      \"Condition\": {
        \"NumericLessThan\": {
          \"glacier:ArchiveAgeInDays\": \"365\"
        }
      }
    }]
  }" \
  --query 'lockId' --output text)

echo "Lock ID: $LOCK_ID"
echo "24-hour confirmation window is now open."

# Verify the lock is in InProgress state
aws glacier get-vault-lock \
  --vault-name "$VAULT_NAME" \
  --query '{State:State,ExpiryDate:ExpirationDate}'

# ABORT the lock (for exercise purposes -- do NOT complete in a lab)
aws glacier abort-vault-lock --vault-name "$VAULT_NAME"
echo "Vault lock aborted (not made permanent)"
```

</details>

<details>
<summary>TODO 4 -- Glacier Retrieval (CLI, storage.tf)</summary>

```bash
BUCKET=$(terraform output -raw archival_bucket)

# Note: this only works after objects have been transitioned to Glacier
# (requires waiting for lifecycle policy to execute, ~24 hours minimum)

# Expedited retrieval (1-5 minutes, highest cost)
aws s3api restore-object \
  --bucket "$BUCKET" \
  --key "compliance/audit-2024.txt" \
  --restore-request '{"Days": 7, "GlacierJobParameters": {"Tier": "Expedited"}}'

# Standard retrieval (3-5 hours, moderate cost)
aws s3api restore-object \
  --bucket "$BUCKET" \
  --key "compliance/audit-2025.txt" \
  --restore-request '{"Days": 7, "GlacierJobParameters": {"Tier": "Standard"}}'

# Check restoration status
aws s3api head-object \
  --bucket "$BUCKET" \
  --key "compliance/audit-2024.txt" \
  --query 'Restore'
```

</details>

---

## Cleanup

```bash
# Abort any pending vault locks first
aws glacier abort-vault-lock \
  --vault-name saa-ex54-compliance-vault 2>/dev/null

terraform destroy -auto-approve
```

Verify:

```bash
terraform state list
```

Expected: no output (empty state).

---

## What's Next

Exercise 55 introduces **DynamoDB capacity modes and throttling**, shifting from S3 storage to NoSQL databases. You will calculate RCU and WCU requirements, compare provisioned vs on-demand capacity, and understand how item size rounding affects capacity planning -- critical knowledge for the SAA-C03 database section.

---

## Summary

- **Glacier Vault Lock** makes a vault policy permanent and immutable after a 24-hour confirmation window -- even root cannot remove it
- **24-hour confirmation window** lets you test the policy before making it irreversible -- always use `abort-vault-lock` in non-production
- **S3 lifecycle rules** automate transitioning objects from Standard through IA, Glacier, and Deep Archive based on age
- **Three Glacier Flexible retrieval tiers**: Expedited (1-5 min, $0.03/GB), Standard (3-5 hr, $0.01/GB), Bulk (5-12 hr, $0.0025/GB)
- **Deep Archive retrieval**: Standard (12 hr), Bulk (48 hr) -- Expedited is not available for Deep Archive
- **restore-object is asynchronous** -- you must poll `head-object` for the `Restore` header to determine when retrieval is complete
- **Default retrieval tier is Standard**, not Expedited -- always specify the tier explicitly to avoid unexpected wait times
- **Provisioned capacity units** ($100/month) guarantee Expedited retrieval availability during demand peaks
- **Vault Lock vs Object Lock**: Vault Lock is vault-level, compliance-only, permanent. Object Lock is per-object, supports governance mode, and works with all S3 storage classes

## Reference

- [S3 Glacier Vault Lock](https://docs.aws.amazon.com/amazonglacier/latest/dev/vault-lock.html)
- [Restoring Archived Objects](https://docs.aws.amazon.com/AmazonS3/latest/userguide/restoring-objects.html)
- [S3 Lifecycle Transitions](https://docs.aws.amazon.com/AmazonS3/latest/userguide/lifecycle-transition-general-considerations.html)
- [Terraform aws_glacier_vault](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/glacier_vault)

## Additional Resources

- [SEC Rule 17a-4 Compliance with Glacier](https://docs.aws.amazon.com/amazonglacier/latest/dev/vault-lock.html#vault-lock-compliance) -- how Vault Lock satisfies financial services regulatory requirements
- [S3 Glacier Pricing](https://aws.amazon.com/s3/glacier/pricing/) -- detailed cost breakdown for storage, retrieval, and requests
- [S3 Glacier Retrieval Policies](https://docs.aws.amazon.com/amazonglacier/latest/dev/data-retrieval-policy.html) -- account-level policies to limit retrieval costs
- [Provisioned Capacity for Expedited Retrieval](https://docs.aws.amazon.com/amazonglacier/latest/dev/downloading-an-archive-two.html#api-downloading-an-archive-two-querying-capacity) -- guaranteeing Expedited availability

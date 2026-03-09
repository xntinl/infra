# 49. S3 Cross-Region Replication

<!--
difficulty: intermediate
concepts: [s3-crr, replication-configuration, replication-iam-role, replication-filter, replication-time-control, versioning-requirement, delete-marker-replication]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: apply, analyze
prerequisites: [47-s3-storage-classes-lifecycle-policies, 48-s3-versioning-mfa-delete-object-lock]
aws_cost: ~$0.02/hr
-->

> **AWS Cost Warning:** Two S3 buckets in different regions with replication. Storage costs are per GB/month ($0.023/GB Standard). Cross-region data transfer is $0.02/GB. For small test objects, total cost ~$0.02/hr. Remember to run `terraform destroy` when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Completed exercise 48 (S3 Versioning) | Versioning concepts |
| Two AWS regions available in your account | `aws ec2 describe-regions --query 'Regions[*].RegionName'` |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Implement** S3 Cross-Region Replication with Terraform including source bucket, destination bucket, and IAM role.
2. **Analyze** why versioning must be enabled on both source and destination buckets for CRR to function.
3. **Apply** replication filter rules to selectively replicate objects matching specific prefixes or tags.
4. **Evaluate** the trade-offs between standard CRR (best-effort, usually under 15 minutes) and Replication Time Control (99.99% within 15 minutes SLA).
5. **Diagnose** common CRR failures including missing IAM permissions and destination bucket versioning.

---

## Why This Matters

Cross-Region Replication is the primary S3 mechanism for disaster recovery across AWS regions. The SAA-C03 exam frequently presents scenarios where a company needs to maintain a copy of critical data in a different geographic region for compliance, latency reduction, or disaster recovery. CRR is the standard answer, but the exam tests nuances: Does it replicate existing objects? (No -- only new objects after enabling the rule, unless you use S3 Batch Replication.) Does it replicate delete markers? (Only if explicitly configured.) Does it require versioning? (Yes, on both buckets.)

The architectural decision is whether to use CRR at all versus alternatives like S3 Multi-Region Access Points or application-level replication. CRR is simple and fully managed, but it is asynchronous with no SLA unless you pay extra for Replication Time Control ($0.015 per GB replicated). For RPO-sensitive workloads, understanding the replication lag characteristics is critical. The exam also tests Same-Region Replication (SRR) for compliance logging -- replicating to a separate bucket in the same region that the source account cannot delete from.

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

# Two providers: source region and destination region
provider "aws" {
  region = "us-east-1"
  alias  = "source"
}

provider "aws" {
  region = "us-west-2"
  alias  = "destination"
}
```

### `variables.tf`

```hcl
variable "project_name" {
  description = "Project name for resource naming and tagging"
  type        = string
  default     = "saa-ex49"
}
```

### `storage.tf`

```hcl
resource "random_id" "suffix" {
  byte_length = 4
}

# ---------- Source Bucket (us-east-1) ----------

resource "aws_s3_bucket" "source" {
  provider      = aws.source
  bucket        = "${var.project_name}-source-${random_id.suffix.hex}"
  force_destroy = true

  tags = { Name = "${var.project_name}-source" }
}

resource "aws_s3_bucket_versioning" "source" {
  provider = aws.source
  bucket   = aws_s3_bucket.source.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_public_access_block" "source" {
  provider = aws.source
  bucket   = aws_s3_bucket.source.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# ---------- Destination Bucket (us-west-2) ----------

resource "aws_s3_bucket" "destination" {
  provider      = aws.destination
  bucket        = "${var.project_name}-dest-${random_id.suffix.hex}"
  force_destroy = true

  tags = { Name = "${var.project_name}-destination" }
}

resource "aws_s3_bucket_versioning" "destination" {
  provider = aws.destination
  bucket   = aws_s3_bucket.destination.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_public_access_block" "destination" {
  provider = aws.destination
  bucket   = aws_s3_bucket.destination.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# ============================================================
# TODO 2: Replication Configuration
# ============================================================
# Create a replication configuration on the source bucket that
# replicates all objects with the prefix "replicated/" to the
# destination bucket.
#
# Requirements:
#   - Resource: aws_s3_bucket_replication_configuration
#   - role = IAM role ARN from TODO 1 (iam.tf)
#   - rule block with:
#     - id = "replicate-data"
#     - status = "Enabled"
#     - filter with prefix = "replicated/"
#     - destination block with:
#       - bucket = destination bucket ARN
#       - storage_class = "STANDARD"
#
# Important: This resource must depend on both bucket versioning
# resources to ensure versioning is enabled before replication.
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket_replication_configuration
# ============================================================


# ============================================================
# TODO 3: Delete Marker Replication
# ============================================================
# By default, CRR does NOT replicate delete markers. This means
# deleting an object in the source bucket does not delete it
# in the destination -- which is often desired for DR.
#
# For this exercise, add delete_marker_replication to the rule:
#   delete_marker_replication {
#     status = "Enabled"
#   }
#
# Decision framework:
#   - DR scenario: DISABLE delete marker replication (protects
#     against accidental mass deletion propagating)
#   - Active-active sync: ENABLE delete marker replication
#     (keeps both buckets consistent)
#
# Add this block inside the rule in TODO 2.
# ============================================================
```

### `iam.tf`

```hcl
# ============================================================
# TODO 1: IAM Role for Replication
# ============================================================
# Create an IAM role that S3 assumes to replicate objects from
# the source bucket to the destination bucket.
#
# Requirements:
#   a) aws_iam_role with assume_role_policy allowing
#      s3.amazonaws.com to assume the role
#   b) aws_iam_role_policy with:
#      - s3:GetReplicationConfiguration and s3:ListBucket on
#        the source bucket
#      - s3:GetObjectVersionForReplication,
#        s3:GetObjectVersionAcl,
#        s3:GetObjectVersionTagging on source bucket objects
#      - s3:ReplicateObject, s3:ReplicateDelete,
#        s3:ReplicateTags on destination bucket objects
#
# Docs: https://docs.aws.amazon.com/AmazonS3/latest/userguide/setting-repl-config-perm-overview.html
# ============================================================
```

### `outputs.tf`

```hcl
output "source_bucket" {
  value = aws_s3_bucket.source.id
}

output "destination_bucket" {
  value = aws_s3_bucket.destination.id
}

output "source_region" {
  value = "us-east-1"
}

output "destination_region" {
  value = "us-west-2"
}
```

---

## Spot the Bug

The following replication configuration has a critical flaw that will cause all replication to silently fail. Identify the problem before expanding the answer.

```hcl
resource "aws_s3_bucket" "source" {
  bucket = "my-source-bucket"
}

resource "aws_s3_bucket_versioning" "source" {
  bucket = aws_s3_bucket.source.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket" "destination" {
  bucket = "my-destination-bucket"
}

# Note: no versioning configuration for destination bucket

resource "aws_s3_bucket_replication_configuration" "replication" {
  role   = aws_iam_role.replication.arn
  bucket = aws_s3_bucket.source.id

  rule {
    id     = "replicate-all"
    status = "Enabled"

    filter {}

    destination {
      bucket        = aws_s3_bucket.destination.arn
      storage_class = "STANDARD"
    }
  }

  depends_on = [aws_s3_bucket_versioning.source]
}
```

<details>
<summary>Explain the bug</summary>

**The destination bucket does not have versioning enabled.** S3 Cross-Region Replication requires versioning on BOTH the source and destination buckets. Without versioning on the destination bucket, the replication configuration may be accepted by the API but replication will fail silently -- objects uploaded to the source will not appear in the destination.

The `depends_on` only references the source bucket versioning, so Terraform might create the replication configuration before (or without) destination versioning.

**Fix:** Enable versioning on the destination bucket and add it to `depends_on`:

```hcl
resource "aws_s3_bucket_versioning" "destination" {
  bucket = aws_s3_bucket.destination.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_replication_configuration" "replication" {
  # ... existing config ...

  depends_on = [
    aws_s3_bucket_versioning.source,
    aws_s3_bucket_versioning.destination
  ]
}
```

This is one of the most common CRR mistakes. The S3 API may accept the replication configuration without validating destination versioning, creating a silent failure that is difficult to diagnose.

</details>

---

## Verify What You Learned

1. **Deploy the infrastructure:**
   ```bash
   terraform init && terraform apply -auto-approve
   ```

2. **Upload an object to the source bucket (matching the replication prefix):**
   ```bash
   SOURCE=$(terraform output -raw source_bucket)
   echo "replicated data" | aws s3 cp - "s3://$SOURCE/replicated/test-file.txt" --region us-east-1
   ```

3. **Wait 30-60 seconds, then check the destination bucket:**
   ```bash
   DEST=$(terraform output -raw destination_bucket)
   aws s3api list-objects-v2 \
     --bucket "$DEST" \
     --prefix "replicated/" \
     --region us-west-2 \
     --query 'Contents[*].{Key:Key,Size:Size}' \
     --output table
   ```

4. **Upload an object that does NOT match the replication filter:**
   ```bash
   echo "not replicated" | aws s3 cp - "s3://$SOURCE/other/test-file.txt" --region us-east-1
   ```

5. **Verify it was not replicated:**
   ```bash
   aws s3api list-objects-v2 \
     --bucket "$DEST" \
     --prefix "other/" \
     --region us-west-2 \
     --query 'Contents[*].Key' --output text
   ```
   Expected: empty (no objects).

6. **Check replication status on the source object:**
   ```bash
   aws s3api head-object \
     --bucket "$SOURCE" \
     --key "replicated/test-file.txt" \
     --region us-east-1 \
     --query 'ReplicationStatus'
   ```
   Expected: `"COMPLETED"` (or `"PENDING"` if checked immediately).

7. **Verify Terraform state:**
   ```bash
   terraform plan
   ```
   Expected: `No changes. Your infrastructure matches the configuration.`

---

## CRR vs SRR Decision Framework

| Criterion | Cross-Region Replication (CRR) | Same-Region Replication (SRR) |
|---|---|---|
| **Purpose** | DR, compliance, latency reduction | Log aggregation, compliance copies |
| **Data transfer cost** | $0.02/GB cross-region | Free (same region) |
| **Latency** | Best effort (~15 min) or RTC SLA | Best effort (~15 min) |
| **Versioning required** | Yes, both buckets | Yes, both buckets |
| **Cross-account support** | Yes | Yes |
| **Typical use case** | Replicate to DR region | Compliance copy in same region |

---

## Solutions

<details>
<summary>TODO 1 -- IAM Role for Replication (iam.tf)</summary>

```hcl
resource "aws_iam_role" "replication" {
  name = "saa-ex49-replication-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action    = "sts:AssumeRole"
      Effect    = "Allow"
      Principal = { Service = "s3.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy" "replication" {
  name = "saa-ex49-replication-policy"
  role = aws_iam_role.replication.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "s3:GetReplicationConfiguration",
          "s3:ListBucket"
        ]
        Resource = aws_s3_bucket.source.arn
      },
      {
        Effect = "Allow"
        Action = [
          "s3:GetObjectVersionForReplication",
          "s3:GetObjectVersionAcl",
          "s3:GetObjectVersionTagging"
        ]
        Resource = "${aws_s3_bucket.source.arn}/*"
      },
      {
        Effect = "Allow"
        Action = [
          "s3:ReplicateObject",
          "s3:ReplicateDelete",
          "s3:ReplicateTags"
        ]
        Resource = "${aws_s3_bucket.destination.arn}/*"
      }
    ]
  })
}
```

</details>

<details>
<summary>TODO 2 -- Replication Configuration (storage.tf)</summary>

```hcl
resource "aws_s3_bucket_replication_configuration" "replication" {
  provider = aws.source
  role     = aws_iam_role.replication.arn
  bucket   = aws_s3_bucket.source.id

  rule {
    id     = "replicate-data"
    status = "Enabled"

    filter {
      prefix = "replicated/"
    }

    destination {
      bucket        = aws_s3_bucket.destination.arn
      storage_class = "STANDARD"
    }
  }

  depends_on = [
    aws_s3_bucket_versioning.source,
    aws_s3_bucket_versioning.destination
  ]
}
```

</details>

<details>
<summary>TODO 3 -- Delete Marker Replication (storage.tf)</summary>

Add inside the `rule` block in TODO 2:

```hcl
    delete_marker_replication {
      status = "Enabled"
    }
```

Complete rule with delete marker replication:

```hcl
  rule {
    id     = "replicate-data"
    status = "Enabled"

    filter {
      prefix = "replicated/"
    }

    delete_marker_replication {
      status = "Enabled"
    }

    destination {
      bucket        = aws_s3_bucket.destination.arn
      storage_class = "STANDARD"
    }
  }
```

With `status = "Enabled"`, deleting an object in the source bucket creates a delete marker that is replicated to the destination, hiding the object there too. With `status = "Disabled"` (default), delete markers are not replicated, so the destination retains the object even after source deletion -- this is preferred for DR scenarios.

</details>

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify buckets are deleted:

```bash
terraform state list
```

Expected: no output (empty state).

---

## What's Next

Exercise 50 covers **S3 Access Points and Object Lambda**, where you will create access points that simplify bucket policies for multi-application access and use Object Lambda to transform objects on retrieval (such as redacting PII from shared datasets).

---

## Summary

- **Cross-Region Replication (CRR)** asynchronously copies objects to a bucket in a different region for DR, compliance, or latency reduction
- **Versioning must be enabled on both source and destination buckets** -- this is the most common CRR configuration mistake
- **Only new objects** are replicated after enabling CRR; use S3 Batch Replication for existing objects
- **Replication filter rules** let you selectively replicate based on prefix, tags, or both
- **Delete marker replication** is disabled by default -- enable for active-active sync, disable for DR protection
- **Replication Time Control (RTC)** provides an SLA of 99.99% of objects replicated within 15 minutes, at additional cost ($0.015/GB)
- **IAM role** must grant S3 permission to read from source and write to destination
- **Cross-account replication** requires a bucket policy on the destination granting the source account's replication role access
- Standard CRR is best-effort with no SLA -- most objects replicate within 15 minutes, but there is no guarantee

## Reference

- [S3 Replication Overview](https://docs.aws.amazon.com/AmazonS3/latest/userguide/replication.html)
- [Setting Up CRR](https://docs.aws.amazon.com/AmazonS3/latest/userguide/replication-walkthrough1.html)
- [Replication Time Control](https://docs.aws.amazon.com/AmazonS3/latest/userguide/replication-time-control.html)
- [Terraform aws_s3_bucket_replication_configuration](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket_replication_configuration)

## Additional Resources

- [S3 Batch Replication](https://docs.aws.amazon.com/AmazonS3/latest/userguide/s3-batch-replication-batch.html) -- replicate existing objects that were uploaded before CRR was enabled
- [Replication Status Monitoring](https://docs.aws.amazon.com/AmazonS3/latest/userguide/replication-metrics.html) -- CloudWatch metrics for tracking replication lag and failure rates
- [Cross-Account Replication](https://docs.aws.amazon.com/AmazonS3/latest/userguide/replication-walkthrough-2.html) -- additional IAM and bucket policy requirements for cross-account CRR
- [S3 Multi-Region Access Points](https://docs.aws.amazon.com/AmazonS3/latest/userguide/MultiRegionAccessPoints.html) -- alternative to CRR for active-active multi-region access patterns

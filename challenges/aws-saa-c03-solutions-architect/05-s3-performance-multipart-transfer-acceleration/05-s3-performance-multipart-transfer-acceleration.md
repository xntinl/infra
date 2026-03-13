# S3 Performance: Multipart Upload and Transfer Acceleration

<!--
difficulty: intermediate
concepts: [s3-multipart-upload, transfer-acceleration, s3-prefix-performance, s3-request-rates, byte-range-fetches]
tools: [terraform, aws-cli]
estimated_time: 50m
bloom_level: design, justify, implement
prerequisites: [none]
aws_cost: ~$0.02/hr
-->

> **AWS Cost Warning:** S3 storage and requests are minimal (~$0.02/hr during active use). Transfer Acceleration adds $0.04/GB over standard transfer pricing. Total cost depends on data volume transferred. Clean up objects and bucket after the exercise.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| `dd` command available (for generating test files) | `dd --version` |
| At least 200MB free disk space | `df -h .` |
| S3 bucket naming: globally unique name available | N/A |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Design** an S3 upload strategy that maximizes throughput for files of varying sizes.
2. **Justify** when to use Transfer Acceleration vs standard uploads based on geography and cost.
3. **Implement** multipart upload lifecycle rules that prevent orphaned parts from accumulating charges.
4. **Compare** upload performance across single PUT, multipart, and Transfer Acceleration methods.
5. **Evaluate** S3 prefix partitioning strategies for high-request-rate workloads.

---

## Why This Matters

S3 is deceptively simple on the surface -- `aws s3 cp file s3://bucket/` works for a 1KB config file and a 5TB database dump alike. But the SAA-C03 exam tests whether you understand the performance characteristics underneath. A single PUT operation caps out at 5GB and provides no parallelism; a multipart upload splits the file into parts that upload concurrently, retries only the failed parts, and can pause and resume. For the exam, you need to know the thresholds: AWS recommends multipart for files over 100MB, and it is required for files over 5GB.

Transfer Acceleration is another exam favorite because it involves a cost-benefit trade-off. It uses CloudFront edge locations to accelerate uploads over long geographic distances, but it adds $0.04-$0.08/GB on top of standard transfer costs. The exam will present scenarios where a company uploads from remote offices worldwide and ask whether Transfer Acceleration is justified. You also need to understand S3 request rate limits -- 3,500 PUT and 5,500 GET requests per second per prefix -- and how prefix partitioning can multiply that throughput. These are not theoretical numbers; they directly affect architecture decisions for data-intensive applications.

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
  default     = "saa-ex05"
}
```

### `storage.tf`

```hcl
resource "random_id" "bucket_suffix" {
  byte_length = 4
}

resource "aws_s3_bucket" "this" {
  bucket        = "${var.project_name}-perf-${random_id.bucket_suffix.hex}"
  force_destroy = true
}

resource "aws_s3_bucket_versioning" "this" {
  bucket = aws_s3_bucket.this.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_public_access_block" "this" {
  bucket = aws_s3_bucket.this.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# ============================================================
# TODO 1: Enable Transfer Acceleration on the Bucket
# ============================================================
# Configure Transfer Acceleration so uploads can use the
# accelerated endpoint: <bucket>.s3-accelerate.amazonaws.com
#
# Requirements:
#   - Resource: aws_s3_bucket_accelerate_configuration
#   - status = "Enabled"
#   - Associate with the bucket above
#   - Note: bucket name cannot contain dots for acceleration
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket_accelerate_configuration
# ============================================================


# ============================================================
# TODO 2: Multipart Upload Configuration
# ============================================================
# This TODO is CLI-based, not Terraform. After terraform apply,
# run these commands to test multipart upload behavior.
#
# Requirements:
#   a) Generate a 100MB test file:
#      dd if=/dev/urandom of=testfile-100mb bs=1M count=100
#
#   b) Upload using standard single-stream:
#      time aws s3 cp testfile-100mb s3://<BUCKET>/single/testfile-100mb
#
#   c) Upload using multipart with explicit part size (10MB):
#      time aws s3 cp testfile-100mb s3://<BUCKET>/multipart/testfile-100mb \
#        --expected-size 104857600
#
#   d) Configure AWS CLI multipart threshold and part size:
#      aws configure set s3.multipart_threshold 10MB
#      aws configure set s3.multipart_chunksize 10MB
#      time aws s3 cp testfile-100mb s3://<BUCKET>/multipart-tuned/testfile-100mb
#
# Compare the three upload times. The multipart uploads should
# be faster due to parallel part uploads.
#
# Docs: https://docs.aws.amazon.com/cli/latest/topic/s3-config.html
# ============================================================


# ============================================================
# TODO 3: Lifecycle Rule for Incomplete Multipart Uploads
# ============================================================
# Create a lifecycle rule that automatically aborts incomplete
# multipart uploads after 7 days. Without this, orphaned parts
# accumulate storage charges indefinitely.
#
# Requirements:
#   - Resource: aws_s3_bucket_lifecycle_configuration
#   - rule with id = "abort-incomplete-multipart"
#   - filter with empty prefix (applies to all objects)
#   - abort_incomplete_multipart_upload block:
#     - days_after_initiation = 7
#   - status = "Enabled"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket_lifecycle_configuration
# ============================================================


# ============================================================
# TODO 4: Prefix Partitioning Strategy Demonstration
# ============================================================
# This TODO is CLI-based. Demonstrate the S3 prefix performance
# model by creating objects across multiple prefixes.
#
# S3 supports:
#   - 3,500 PUT/COPY/POST/DELETE requests per second per prefix
#   - 5,500 GET/HEAD requests per second per prefix
#
# Requirements:
#   a) Create objects distributed across date-based prefixes:
#      for i in $(seq 1 100); do
#        PREFIX="data/2026/03/$(printf '%02d' $((i % 28 + 1)))/$(printf '%02d' $((i % 24)))"
#        aws s3api put-object \
#          --bucket <BUCKET> \
#          --key "$PREFIX/record-$i.json" \
#          --body <(echo '{"id":'$i'}')
#      done
#
#   b) Compare with a flat prefix (all objects under one key):
#      for i in $(seq 1 100); do
#        aws s3api put-object \
#          --bucket <BUCKET> \
#          --key "flat/record-$i.json" \
#          --body <(echo '{"id":'$i'}')
#      done
#
#   c) List objects per prefix to verify distribution:
#      aws s3api list-objects-v2 --bucket <BUCKET> \
#        --prefix "data/2026/03/01/" --query 'Contents[*].Key'
#
# At scale (thousands of requests/sec), the date-partitioned
# approach distributes load across multiple prefixes, each
# getting its own 3,500/5,500 r/s limit.
#
# Docs: https://docs.aws.amazon.com/AmazonS3/latest/userguide/optimizing-performance.html
# ============================================================


# ============================================================
# TODO 5: Byte-Range Fetch Demonstration
# ============================================================
# This TODO is CLI-based. Demonstrate retrieving specific byte
# ranges from a large file without downloading the entire object.
#
# Requirements:
#   a) Upload a large file (use the 100MB file from TODO 2):
#      aws s3 cp testfile-100mb s3://<BUCKET>/range-test/testfile-100mb
#
#   b) Fetch only the first 1MB:
#      aws s3api get-object \
#        --bucket <BUCKET> \
#        --key "range-test/testfile-100mb" \
#        --range "bytes=0-1048575" \
#        first-1mb.bin
#
#   c) Fetch bytes 50MB-51MB (middle of file):
#      aws s3api get-object \
#        --bucket <BUCKET> \
#        --key "range-test/testfile-100mb" \
#        --range "bytes=52428800-53477375" \
#        middle-1mb.bin
#
#   d) Verify file sizes:
#      ls -la first-1mb.bin middle-1mb.bin
#
# Use cases: resuming failed downloads, parallel download of
# large files, reading specific sections of log files or
# columnar data formats (Parquet, ORC).
#
# Docs: https://docs.aws.amazon.com/AmazonS3/latest/userguide/optimizing-performance-guidelines.html
# ============================================================
```

### `iam.tf`

```hcl
data "aws_caller_identity" "current" {}

resource "aws_iam_policy" "s3_lab" {
  name = "${var.project_name}-s3-lab-policy"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "s3:PutObject",
          "s3:GetObject",
          "s3:DeleteObject",
          "s3:ListBucket",
          "s3:ListMultipartUploadParts",
          "s3:ListBucketMultipartUploads",
          "s3:AbortMultipartUpload",
          "s3:GetBucketAccelerateConfiguration",
          "s3:PutBucketAccelerateConfiguration"
        ]
        Resource = [
          aws_s3_bucket.this.arn,
          "${aws_s3_bucket.this.arn}/*"
        ]
      }
    ]
  })
}
```

### `outputs.tf`

```hcl
output "bucket_name" {
  value = aws_s3_bucket.this.id
}

output "bucket_arn" {
  value = aws_s3_bucket.this.arn
}

output "bucket_regional_domain" {
  value = aws_s3_bucket.this.bucket_regional_domain_name
}
```

---

## Spot the Bug

The following workflow initiates a multipart upload but has a critical operational flaw. Identify the problem before expanding the answer.

```bash
# Initiate multipart upload
UPLOAD_ID=$(aws s3api create-multipart-upload \
  --bucket my-bucket \
  --key "large-dataset/export.csv" \
  --query 'UploadId' --output text)

echo "Upload ID: $UPLOAD_ID"

# Upload parts
for PART in $(seq 1 10); do
  aws s3api upload-part \
    --bucket my-bucket \
    --key "large-dataset/export.csv" \
    --upload-id "$UPLOAD_ID" \
    --part-number $PART \
    --body "part-$PART.bin"
done

# Oops -- script exits here due to unrelated error
# The complete-multipart-upload call never executes

echo "Upload complete!"
```

<details>
<summary>Explain the bug</summary>

The multipart upload is initiated and parts are uploaded, but `complete-multipart-upload` is never called because the script exits prematurely. The uploaded parts remain in S3 as orphaned fragments, and **you are charged standard storage rates for every uploaded part indefinitely**.

This is not hypothetical -- it is one of the most common sources of unexpected S3 bills. Organizations have discovered hundreds of gigabytes of orphaned multipart parts sitting in buckets for months or years. The parts do not appear in normal `list-objects` output; you must specifically use `list-multipart-uploads` and `list-parts` to find them.

**Three mitigations (apply all three):**

1. **Lifecycle rule (TODO 3):** Configure `abort_incomplete_multipart_upload` with `days_after_initiation = 7` as a safety net. This is a best practice for every S3 bucket.

2. **Script error handling:** Add `set -e` and a trap to abort the upload on failure:
   ```bash
   trap 'aws s3api abort-multipart-upload --bucket my-bucket \
     --key "large-dataset/export.csv" --upload-id "$UPLOAD_ID"' ERR
   ```

3. **Use high-level commands:** `aws s3 cp` handles multipart automatically and cleans up on failure. Only use low-level `s3api` commands when you need explicit control over part sizes or parallelism.

</details>

---

## Verify What You Learned

1. **Deploy the infrastructure:**
   ```bash
   terraform init && terraform apply -auto-approve
   ```

2. **Confirm Transfer Acceleration is enabled:**
   ```bash
   BUCKET=$(terraform output -raw bucket_name)
   aws s3api get-bucket-accelerate-configuration --bucket "$BUCKET"
   ```

3. **Generate a 100MB test file:**
   ```bash
   dd if=/dev/urandom of=testfile-100mb bs=1M count=100
   ```

4. **Run the upload speed comparison:**
   ```bash
   BUCKET=$(terraform output -raw bucket_name)

   echo "=== Standard Upload ==="
   time aws s3 cp testfile-100mb "s3://$BUCKET/standard/testfile-100mb"

   echo "=== Multipart Upload (10MB parts) ==="
   aws configure set s3.multipart_threshold 10MB
   aws configure set s3.multipart_chunksize 10MB
   time aws s3 cp testfile-100mb "s3://$BUCKET/multipart/testfile-100mb"

   echo "=== Transfer Acceleration ==="
   time aws s3 cp testfile-100mb "s3://$BUCKET/accelerated/testfile-100mb" \
     --endpoint-url "https://$BUCKET.s3-accelerate.amazonaws.com"
   ```

5. **Verify the lifecycle rule catches orphaned uploads:**
   ```bash
   aws s3api list-bucket-lifecycle-configuration --bucket "$BUCKET"
   ```

6. **Test byte-range fetches:**
   ```bash
   aws s3api get-object --bucket "$BUCKET" \
     --key "standard/testfile-100mb" \
     --range "bytes=0-1048575" first-1mb.bin
   ls -la first-1mb.bin
   ```

7. **Check for any incomplete multipart uploads:**
   ```bash
   aws s3api list-multipart-uploads --bucket "$BUCKET"
   ```

---

## Performance Comparison Framework

| Method | Max Object Size | Parallelism | Resume on Failure | Best For |
|---|---|---|---|---|
| **Single PUT** | 5 GB | None | No (restart entirely) | Files < 100 MB |
| **Multipart Upload** | 5 TB | Yes (concurrent parts) | Yes (retry single part) | Files > 100 MB |
| **Transfer Acceleration** | 5 TB (with multipart) | Yes | Yes | Long-distance uploads |
| **S3 Transfer Manager** | 5 TB | Auto-tuned | Yes | SDK-based applications |

### Cost Analysis: Transfer Acceleration

| Transfer Type | Standard Price | Accelerated Price | Premium |
|---|---|---|---|
| Data into S3 (US/EU) | $0.00/GB | $0.04/GB | +$0.04/GB |
| Data into S3 (other regions) | $0.00/GB | $0.08/GB | +$0.08/GB |
| Within same region | $0.00/GB | No benefit | N/A |

**Rule of thumb:** Transfer Acceleration provides 50-500% speed improvement for distances over 1,000 miles. Test with the [S3 Transfer Acceleration Speed Comparison Tool](https://s3-accelerate-speedtest.s3-accelerate.amazonaws.com/en/accelerate-speed-comparsion.html) before committing.

---

## Solutions

<details>
<summary>storage.tf -- TODO 1: Enable Transfer Acceleration</summary>

```hcl
resource "aws_s3_bucket_accelerate_configuration" "this" {
  bucket = aws_s3_bucket.this.id
  status = "Enabled"
}
```

After applying, the bucket is accessible via:
- Standard: `https://<bucket>.s3.amazonaws.com`
- Accelerated: `https://<bucket>.s3-accelerate.amazonaws.com`

</details>

<details>
<summary>TODO 2 -- Multipart Upload Commands</summary>

```bash
BUCKET=$(terraform output -raw bucket_name)

# Generate test file
dd if=/dev/urandom of=testfile-100mb bs=1M count=100

# Standard single-stream upload
time aws s3 cp testfile-100mb "s3://$BUCKET/single/testfile-100mb"

# Multipart with tuned settings
aws configure set s3.multipart_threshold 10MB
aws configure set s3.multipart_chunksize 10MB
time aws s3 cp testfile-100mb "s3://$BUCKET/multipart/testfile-100mb"

# Transfer Acceleration upload
time aws s3 cp testfile-100mb "s3://$BUCKET/accelerated/testfile-100mb" \
  --endpoint-url "https://$BUCKET.s3-accelerate.amazonaws.com"
```

On a typical connection, multipart upload will be 2-4x faster than single PUT for a 100MB file due to concurrent part uploads (default 10 threads).

</details>

<details>
<summary>storage.tf -- TODO 3: Lifecycle Rule for Incomplete Multipart Uploads</summary>

```hcl
resource "aws_s3_bucket_lifecycle_configuration" "this" {
  bucket = aws_s3_bucket.this.id

  rule {
    id     = "abort-incomplete-multipart"
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

This is a best practice for every S3 bucket. AWS recommends adding this rule even if you do not actively use multipart uploads, because SDKs and tools may initiate multipart uploads automatically and fail silently.

</details>

<details>
<summary>TODO 4 -- Prefix Partitioning Strategy</summary>

```bash
BUCKET=$(terraform output -raw bucket_name)

# Date-partitioned prefix strategy
for i in $(seq 1 100); do
  DAY=$(printf '%02d' $((i % 28 + 1)))
  HOUR=$(printf '%02d' $((i % 24)))
  PREFIX="data/2026/03/$DAY/$HOUR"
  aws s3api put-object \
    --bucket "$BUCKET" \
    --key "$PREFIX/record-$i.json" \
    --body <(echo "{\"id\":$i}")
done

# Flat prefix for comparison
for i in $(seq 1 100); do
  aws s3api put-object \
    --bucket "$BUCKET" \
    --key "flat/record-$i.json" \
    --body <(echo "{\"id\":$i}")
done

# Verify distribution
aws s3api list-objects-v2 --bucket "$BUCKET" \
  --prefix "data/2026/03/01/" \
  --query 'Contents[*].Key'
```

At high request rates, the partitioned approach distributes load: each unique prefix gets its own 3,500 PUT/s and 5,500 GET/s limit. With 28 day-prefixes and 24 hour-prefixes, you effectively get 672 independent partitions.

</details>

<details>
<summary>TODO 5 -- Byte-Range Fetch</summary>

```bash
BUCKET=$(terraform output -raw bucket_name)

# Upload the test file
aws s3 cp testfile-100mb "s3://$BUCKET/range-test/testfile-100mb"

# Fetch first 1MB (bytes 0 through 1,048,575)
aws s3api get-object \
  --bucket "$BUCKET" \
  --key "range-test/testfile-100mb" \
  --range "bytes=0-1048575" \
  first-1mb.bin

# Fetch 1MB from the middle (starting at 50MB)
aws s3api get-object \
  --bucket "$BUCKET" \
  --key "range-test/testfile-100mb" \
  --range "bytes=52428800-53477375" \
  middle-1mb.bin

# Fetch last 1MB
aws s3api get-object \
  --bucket "$BUCKET" \
  --key "range-test/testfile-100mb" \
  --range "bytes=-1048576" \
  last-1mb.bin

# Verify sizes
ls -la first-1mb.bin middle-1mb.bin last-1mb.bin
```

Each file should be exactly 1,048,576 bytes (1MB). Byte-range fetches are essential for:
- Parallel downloads (split a 5GB file into 50 concurrent 100MB range requests)
- Columnar data formats where you only need specific columns
- Resuming interrupted downloads

</details>

---

## Cleanup

```bash
# Remove local test files
rm -f testfile-100mb first-1mb.bin middle-1mb.bin last-1mb.bin

# Destroy infrastructure (force_destroy=true handles bucket contents)
terraform destroy -auto-approve
```

Verify the bucket is deleted:

```bash
BUCKET=$(terraform output -raw bucket_name 2>/dev/null)
aws s3api head-bucket --bucket "$BUCKET" 2>/dev/null && echo "Bucket still exists!" || echo "Bucket deleted successfully"
```

---

## What's Next

Exercise 06 covers cross-account IAM role assumption, where you will implement trust policies, external IDs for confused deputy prevention, and compare resource-based vs identity-based cross-account access patterns. The security-first thinking introduced by S3 bucket policies in this exercise extends naturally into IAM trust relationships.

---

## Summary

You configured S3 Transfer Acceleration, measured the performance difference between single PUT and multipart uploads, implemented lifecycle rules to prevent orphaned multipart parts from accumulating charges, demonstrated prefix partitioning for high-throughput workloads, and used byte-range fetches for partial object retrieval. The key architectural insight is that S3 performance optimization is not about a single setting but about matching your upload strategy (single PUT, multipart, acceleration) and key naming strategy (prefix partitioning) to your specific access patterns and geographic distribution.

---

## Reference

- [S3 Multipart Upload Overview](https://docs.aws.amazon.com/AmazonS3/latest/userguide/mpuoverview.html)
- [S3 Transfer Acceleration](https://docs.aws.amazon.com/AmazonS3/latest/userguide/transfer-acceleration.html)
- [Optimizing S3 Performance](https://docs.aws.amazon.com/AmazonS3/latest/userguide/optimizing-performance.html)
- [S3 Request Rate Performance Guidelines](https://docs.aws.amazon.com/AmazonS3/latest/userguide/optimizing-performance-guidelines.html)
- [Byte-Range Fetches](https://docs.aws.amazon.com/whitepapers/latest/s3-optimizing-performance-best-practices/use-byte-range-fetches.html)

## Additional Resources

- [Terraform aws_s3_bucket_accelerate_configuration](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket_accelerate_configuration)
- [Terraform aws_s3_bucket_lifecycle_configuration](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket_lifecycle_configuration)
- [S3 Transfer Acceleration Speed Comparison](https://s3-accelerate-speedtest.s3-accelerate.amazonaws.com/en/accelerate-speed-comparsion.html)
- [AWS CLI S3 Configuration](https://docs.aws.amazon.com/cli/latest/topic/s3-config.html)

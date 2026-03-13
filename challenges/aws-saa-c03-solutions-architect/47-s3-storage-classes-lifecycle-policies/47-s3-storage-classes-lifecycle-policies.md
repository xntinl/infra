# 47. S3 Storage Classes and Lifecycle Policies

<!--
difficulty: basic
concepts: [s3-standard, s3-ia, s3-one-zone-ia, s3-intelligent-tiering, glacier-instant, glacier-flexible, glacier-deep-archive, lifecycle-policy, storage-class-transition]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: understand
prerequisites: [none]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** S3 storage is charged per GB/month and varies by storage class. A few small test objects cost fractions of a cent. Lifecycle transitions incur per-request charges ($0.01 per 1,000 transition requests). Total cost negligible (~$0.01/hr). Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Basic understanding of object storage concepts

## Learning Objectives

After completing this exercise, you will be able to:

- **Identify** all seven S3 storage classes and their cost, availability, and retrieval characteristics
- **Explain** the minimum storage duration charges and how they affect cost for short-lived objects
- **Construct** lifecycle rules that transition objects between storage classes based on age
- **Compare** the cost per GB across storage classes using a concrete pricing table
- **Distinguish** when to use S3 Intelligent-Tiering (unknown access patterns) vs manual lifecycle rules (known patterns)
- **Describe** the retrieval time and per-GB retrieval cost trade-offs for the three Glacier tiers

## Why S3 Storage Classes Matter

S3 storage class selection is one of the most frequently tested topics on the SAA-C03 exam because it combines cost optimization with availability trade-offs -- two pillars the exam emphasizes heavily. AWS offers seven storage classes, each with different pricing per GB, retrieval costs, minimum storage durations, and availability SLAs. Choosing the wrong class means either overpaying for infrequently accessed data or paying retrieval fees that exceed the storage savings.

The key architectural insight is that most data follows a predictable access pattern: frequently accessed when new, declining access over weeks, and rarely accessed after months. A lifecycle policy automates the transition from expensive, low-latency storage to cheaper, higher-latency storage as data ages. The exam presents scenarios with specific access patterns and asks you to design the optimal lifecycle strategy. For example, a healthcare company must retain medical images for 7 years (regulatory), accesses them frequently for 30 days (diagnosis), occasionally for 1 year (follow-up), and almost never after that (compliance archive). Each phase maps to a different storage class, and the lifecycle policy automates the transitions.

## Step 1 -- Create the Project Files

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
  default     = "saa-ex47"
}
```

### `main.tf`

```hcl
resource "random_id" "suffix" {
  byte_length = 4
}

# ------------------------------------------------------------------
# S3 bucket with versioning enabled (required for some lifecycle
# features like noncurrent version transitions).
# ------------------------------------------------------------------
resource "aws_s3_bucket" "this" {
  bucket        = "${var.project_name}-storage-classes-${random_id.suffix.hex}"
  force_destroy = true

  tags = { Name = "${var.project_name}-storage-classes" }
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

# ------------------------------------------------------------------
# Lifecycle policy: transition objects through storage classes as
# they age. This models a typical pattern where data is hot for
# 30 days, warm for 60 days, cool for 90 days, and cold after that.
#
# Transition order constraints (you cannot skip classes):
#   Standard -> Standard-IA (min 30 days)
#   Standard-IA -> Glacier Instant Retrieval (min 90 days from creation)
#   Glacier Instant -> Glacier Flexible Retrieval
#   Glacier Flexible -> Glacier Deep Archive (min 90 days in Glacier Flexible)
#
# Important: each transition must occur at least 30 days after the
# previous transition. S3 enforces this -- Terraform will error if
# your days values are too close together.
# ------------------------------------------------------------------
resource "aws_s3_bucket_lifecycle_configuration" "this" {
  bucket = aws_s3_bucket.this.id

  # Rule 1: Transition current versions through storage tiers
  rule {
    id     = "tiered-transition"
    status = "Enabled"

    filter {
      prefix = "data/"
    }

    transition {
      days          = 30
      storage_class = "STANDARD_IA"
    }

    transition {
      days          = 90
      storage_class = "GLACIER_IR"
    }

    transition {
      days          = 180
      storage_class = "GLACIER"
    }

    transition {
      days          = 365
      storage_class = "DEEP_ARCHIVE"
    }
  }

  # Rule 2: Clean up noncurrent versions after 30 days
  rule {
    id     = "noncurrent-cleanup"
    status = "Enabled"

    filter {
      prefix = ""
    }

    noncurrent_version_expiration {
      noncurrent_days = 30
    }
  }

  # Rule 3: Abort incomplete multipart uploads after 7 days
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

### `outputs.tf`

```hcl
output "bucket_name" {
  value = aws_s3_bucket.this.id
}

output "bucket_arn" {
  value = aws_s3_bucket.this.arn
}
```

## Step 2 -- Deploy and Upload Objects

```bash
terraform init
terraform apply -auto-approve
```

Upload objects to different storage classes to see the pricing differences:

```bash
BUCKET=$(terraform output -raw bucket_name)

# Upload to Standard (default)
echo "frequently accessed data" | aws s3 cp - "s3://$BUCKET/data/hot-file.txt"

# Upload directly to Standard-IA (infrequent access)
echo "infrequently accessed data" | aws s3 cp - "s3://$BUCKET/data/warm-file.txt" \
  --storage-class STANDARD_IA

# Upload directly to Intelligent-Tiering
echo "unknown access pattern" | aws s3 cp - "s3://$BUCKET/data/auto-tiered.txt" \
  --storage-class INTELLIGENT_TIERING

# Upload directly to One Zone-IA (non-critical data)
echo "non-critical, infrequent data" | aws s3 cp - "s3://$BUCKET/data/one-zone.txt" \
  --storage-class ONEZONE_IA

# Upload directly to Glacier Instant Retrieval
echo "rarely accessed, instant retrieval needed" | aws s3 cp - "s3://$BUCKET/data/glacier-instant.txt" \
  --storage-class GLACIER_IR

# Verify storage classes
aws s3api list-objects-v2 \
  --bucket "$BUCKET" \
  --prefix "data/" \
  --query 'Contents[*].{Key:Key,StorageClass:StorageClass,Size:Size}' \
  --output table
```

## Step 3 -- Storage Class Comparison

### Cost Comparison Table (us-east-1 pricing)

| Storage Class | $/GB/month | Min Duration | Min Object Size | Availability | AZs | Retrieval Cost | Use Case |
|---|---|---|---|---|---|---|---|
| **Standard** | $0.023 | None | None | 99.99% | >= 3 | None | Frequently accessed data |
| **Intelligent-Tiering** | $0.023-$0.0036 | None | None | 99.9% | >= 3 | None (monitoring fee) | Unknown access patterns |
| **Standard-IA** | $0.0125 | 30 days | 128 KB | 99.9% | >= 3 | $0.01/GB | Infrequent, but immediate |
| **One Zone-IA** | $0.01 | 30 days | 128 KB | 99.5% | 1 | $0.01/GB | Recreatable, infrequent |
| **Glacier Instant** | $0.004 | 90 days | 128 KB | 99.9% | >= 3 | $0.03/GB | Quarterly access, ms retrieval |
| **Glacier Flexible** | $0.0036 | 90 days | N/A | 99.99% | >= 3 | $0.01-$0.03/GB | Annual access, minutes-hours |
| **Glacier Deep Archive** | $0.00099 | 180 days | N/A | 99.99% | >= 3 | $0.02-$0.05/GB | 7-10 year retention, 12-48 hrs |

### Intelligent-Tiering Access Tiers (automatic)

| Access Tier | Applies After | $/GB/month | Retrieval |
|---|---|---|---|
| Frequent | Default | $0.023 | Milliseconds |
| Infrequent | 30 days no access | $0.0125 | Milliseconds |
| Archive Instant | 90 days no access | $0.004 | Milliseconds |
| Archive | 90 days (opt-in) | $0.0036 | 3-5 hours |
| Deep Archive | 180 days (opt-in) | $0.00099 | 12-48 hours |

Intelligent-Tiering charges a small monitoring fee ($0.0025 per 1,000 objects/month) but has no retrieval fees. It is ideal when you cannot predict access patterns.

## Step 4 -- Examine Lifecycle Policy Status

```bash
BUCKET=$(terraform output -raw bucket_name)

# View lifecycle configuration
aws s3api get-bucket-lifecycle-configuration \
  --bucket "$BUCKET" \
  --output json

# Check storage class of each object
aws s3api list-objects-v2 \
  --bucket "$BUCKET" \
  --prefix "data/" \
  --query 'Contents[*].{Key:Key,StorageClass:StorageClass,LastModified:LastModified}' \
  --output table
```

Note: Lifecycle transitions are processed asynchronously. S3 runs lifecycle evaluations approximately once per day. You will not see immediate transitions -- this is by design. For this exercise, we uploaded objects directly to different storage classes to demonstrate the differences.

## Common Mistakes

### 1. Not accounting for minimum storage duration charges

**Wrong approach:** Moving objects to Glacier Deep Archive for short-term storage:

```bash
# Upload a temporary report to Deep Archive
echo "weekly report" | aws s3 cp - "s3://$BUCKET/reports/weekly.txt" \
  --storage-class DEEP_ARCHIVE

# Delete it 30 days later
aws s3 rm "s3://$BUCKET/reports/weekly.txt"
```

**What happens:** Deep Archive has a 180-day minimum storage duration. If you delete or transition the object before 180 days, you still pay for the full 180 days. A 1 GB file deleted after 30 days costs: 180 days * ($0.00099/30) = $0.00594 instead of the 30 days * ($0.00099/30) = $0.00099 you expected. You pay 6x more than you planned.

**Fix:** Only use Glacier Deep Archive for data you will retain for at least 180 days. For short-lived data, Standard or Standard-IA is cheaper despite the higher per-GB rate.

### 2. Using Standard-IA for small objects

**Wrong approach:** Transitioning small log entries to Standard-IA:

```hcl
transition {
  days          = 7
  storage_class = "STANDARD_IA"
}
```

**What happens:** Standard-IA has a 128 KB minimum object size charge. If your objects are 1 KB, you are charged for 128 KB. A 1 KB object in Standard costs $0.023 * (1/1048576) = $0.000000022/month. The same object in Standard-IA costs $0.0125 * (128/1048576) = $0.0000015/month -- 68x more expensive than expected because of the minimum size billing.

**Fix:** Aggregate small objects into larger archives before transitioning, or use Intelligent-Tiering which has no minimum object size charge.

### 3. Lifecycle transition ordering violations

**Wrong approach:** Trying to transition directly from Standard to Glacier Deep Archive in 30 days:

```hcl
transition {
  days          = 30
  storage_class = "DEEP_ARCHIVE"
}
```

**What happens:** This is actually allowed by AWS -- you can transition directly from Standard to any storage class. However, the minimum 30-day constraint applies to transitions through intermediate classes. The real mistake is the architectural one: if you might need to access the data within 30-90 days, Deep Archive's 12-48 hour retrieval time makes it inaccessible in practice.

**Fix:** Match transition timing to actual access patterns. If data is accessed occasionally for 90 days, go Standard -> Standard-IA at 30 days -> Glacier Instant at 90 days -> Deep Archive at 365 days.

## Verify What You Learned

```bash
BUCKET=$(terraform output -raw bucket_name)

# Verify lifecycle rules exist
aws s3api get-bucket-lifecycle-configuration \
  --bucket "$BUCKET" \
  --query 'Rules[*].{Id:ID,Status:Status}' \
  --output table
```

Expected: Three rules (`tiered-transition`, `noncurrent-cleanup`, `abort-multipart`), all `Enabled`.

```bash
# Verify objects in different storage classes
aws s3api list-objects-v2 \
  --bucket "$BUCKET" \
  --prefix "data/" \
  --query 'Contents[*].{Key:Key,StorageClass:StorageClass}' \
  --output table
```

Expected: Objects with storage classes `STANDARD`, `STANDARD_IA`, `INTELLIGENT_TIERING`, `ONEZONE_IA`, `GLACIER_IR`.

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify the bucket is deleted:

```bash
BUCKET=$(terraform output -raw bucket_name 2>/dev/null)
aws s3api head-bucket --bucket "$BUCKET" 2>/dev/null && echo "Bucket still exists!" || echo "Bucket deleted successfully"
```

## What's Next

Exercise 48 covers **S3 versioning, MFA Delete, and Object Lock** -- the data protection features that prevent accidental or malicious deletion. You will enable versioning to keep all object versions, configure MFA Delete to require multi-factor authentication for permanent deletes, and set up Object Lock in both governance and compliance modes for WORM (Write Once Read Many) storage.

## Summary

- **S3 Standard** is the default for frequently accessed data -- no retrieval fees, no minimum duration, highest availability (99.99%)
- **Standard-IA** saves 46% over Standard but charges retrieval fees and has a 128 KB minimum object size charge and 30-day minimum duration
- **One Zone-IA** saves 20% over Standard-IA but stores data in a single AZ -- only use for recreatable data
- **Intelligent-Tiering** automatically moves objects between tiers based on access patterns -- ideal when you cannot predict access frequency
- **Glacier Instant Retrieval** provides millisecond access at 83% lower cost than Standard -- use for data accessed quarterly
- **Glacier Flexible Retrieval** offers minutes-to-hours retrieval at the lowest cost for archival data accessed once or twice per year
- **Glacier Deep Archive** is the cheapest option ($0.00099/GB/month) with 12-48 hour retrieval -- use for regulatory compliance archives (7+ year retention)
- **Lifecycle policies** automate transitions between classes based on object age -- always add an `abort_incomplete_multipart_upload` rule
- **Minimum storage duration charges** mean that deleting objects before the minimum period results in paying for the full minimum duration

## Reference

- [S3 Storage Classes](https://docs.aws.amazon.com/AmazonS3/latest/userguide/storage-class-intro.html)
- [S3 Lifecycle Configuration](https://docs.aws.amazon.com/AmazonS3/latest/userguide/object-lifecycle-mgmt.html)
- [S3 Pricing](https://aws.amazon.com/s3/pricing/)
- [Terraform aws_s3_bucket_lifecycle_configuration](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket_lifecycle_configuration)

## Additional Resources

- [S3 Intelligent-Tiering](https://docs.aws.amazon.com/AmazonS3/latest/userguide/intelligent-tiering.html) -- how the automatic tiering algorithm works and when to enable archive access tiers
- [Lifecycle Transition Constraints](https://docs.aws.amazon.com/AmazonS3/latest/userguide/lifecycle-transition-general-considerations.html) -- waterfall rules and minimum duration requirements between transitions
- [S3 Storage Class Analysis](https://docs.aws.amazon.com/AmazonS3/latest/userguide/analytics-storage-class.html) -- S3 Analytics to determine the optimal lifecycle policy based on actual access patterns
- [Cost Optimization with S3 Storage Classes](https://aws.amazon.com/blogs/storage/amazon-s3-cost-optimization-for-predictable-and-unpredictable-access-patterns/) -- AWS blog with real-world cost reduction examples

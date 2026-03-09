# 87. Snow Family Data Transfer

<!--
difficulty: intermediate
concepts: [snowcone, snowball-edge-storage-optimized, snowball-edge-compute-optimized, snowmobile, offline-data-transfer, edge-computing, break-even-analysis, data-migration-planning, otp-encryption]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: apply, analyze
prerequisites: [86-aws-backup-centralized-management]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise is primarily a planning and analysis exercise. The only AWS resources created are S3 buckets for demonstrating transfer targets (~$0.01/hr). Snow Family device orders incur significant costs ($300+ per Snowball job) and are NOT created in this exercise. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 86 (AWS Backup) or equivalent knowledge of data protection strategies
- Understanding of S3 storage classes and data transfer concepts
- Basic understanding of network bandwidth calculations (Gbps to TB/day)

## Learning Objectives

After completing this exercise, you will be able to:

1. **Analyze** when offline data transfer via Snow Family devices is more cost-effective and faster than network-based transfer
2. **Apply** break-even calculations to determine the crossover point between Snow Family and Direct Connect for a given data volume
3. **Evaluate** which Snow Family device fits a specific migration scenario based on data volume, time constraints, and edge computing requirements
4. **Design** a hybrid migration strategy that combines Snow Family devices with online transfer for incremental changes
5. **Distinguish** between Snowcone (edge + small transfer), Snowball Edge Storage Optimized (bulk transfer), Snowball Edge Compute Optimized (edge processing), and Snowmobile (exabyte-scale)

## Why This Matters

The SAA-C03 exam tests your ability to select the right data migration approach based on quantitative analysis, not intuition. A common exam scenario gives you a data volume (e.g., 500 TB), a network bandwidth (e.g., 1 Gbps), and a time constraint (e.g., 2 weeks), and asks you to choose between internet transfer, Direct Connect, and Snow Family. The answer depends on math: 500 TB over a 1 Gbps link takes approximately 46 days at full utilization -- far exceeding the 2-week deadline. Only Snow Family can meet this constraint.

Beyond bulk migration, Snow Family devices serve as edge computing platforms. Snowball Edge Compute Optimized runs EC2 instances and Lambda functions at locations with limited or no connectivity -- factory floors, military forward operating bases, maritime vessels, or disaster recovery sites. Snowcone fits in a backpack and can be shipped or carried to remote locations for lightweight data collection and processing. The exam expects you to match device capabilities to specific environmental constraints, not just data volumes.

The architectural decision is not just "big data = Snowball." You must consider the total migration window, the rate of data change during migration, encryption requirements (all Snow devices use 256-bit encryption with KMS-managed keys), and the logistics of physical device shipping. For datasets that continue to change during the Snow device round-trip, you need a hybrid strategy: bulk transfer via Snow, then incremental sync via DataSync or S3 replication.

## Snow Family Device Comparison

| Feature | Snowcone | Snowball Edge Storage Optimized | Snowball Edge Compute Optimized | Snowmobile |
|---|---|---|---|---|
| **Usable storage** | 8 TB HDD / 14 TB SSD | 80 TB | 42 TB | 100 PB |
| **vCPUs** | 2 | 40 | 104 (with optional GPU) |  N/A |
| **Memory** | 4 GB | 80 GB | 416 GB | N/A |
| **GPU** | No | No | Optional NVIDIA V100 | N/A |
| **EC2 compatible** | No (IoT Greengrass) | Yes (sbe1 instances) | Yes (sbe-c/sbe-g instances) | No |
| **Lambda** | No | Yes | Yes | No |
| **Clustering** | No | Yes (5-10 devices, petabyte) | Yes (5-10 devices) | No |
| **Network** | 2x 1G / 1x 10G | 1x 25G / 1x 100G | 2x 25G / 1x 100G | N/A |
| **Weight** | 4.5 lbs (carry) | 49.7 lbs (ship) | 49.7 lbs (ship) | 45-foot container (semi-truck) |
| **Transfer speed** | ~1 TB/day on 10G link | ~8 TB/day on 100G link | ~5 TB/day on 100G link | Up to 1 EB in weeks |
| **Use case** | Edge IoT, tactical, small migration | Bulk data migration | Edge ML inference, video processing | Datacenter-scale migration |
| **Pricing** | ~$60/job + $6/day (on-site) | ~$300/job + shipping | ~$300/job + shipping | Custom (contact AWS) |

## Step 1 -- Create the Transfer Destination

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
  default     = "saa-ex87"
}
```

### `main.tf`

```hcl
resource "random_id" "suffix" { byte_length = 4 }

# S3 bucket to receive data from Snow Family devices.
# Must exist before ordering the device.
resource "aws_s3_bucket" "migration_target" {
  bucket = "${var.project_name}-migration-${random_id.suffix.hex}", force_destroy = true
  tags   = { Name = "${var.project_name}-migration-target" }
}

resource "aws_s3_bucket_versioning" "migration_target" {
  bucket = aws_s3_bucket.migration_target.id
  versioning_configuration { status = "Enabled" }
}

resource "aws_s3_bucket_public_access_block" "migration_target" {
  bucket = aws_s3_bucket.migration_target.id
  block_public_acls = true, block_public_policy = true
  ignore_public_acls = true, restrict_public_buckets = true
}

# SNS topic for Snow job completion notifications
resource "aws_sns_topic" "snow_notifications" {
  name = "${var.project_name}-snow-notifications"
  tags = { Name = "${var.project_name}-snow-notifications" }
}

resource "aws_sns_topic_subscription" "email" {
  topic_arn = aws_sns_topic.snow_notifications.arn
  protocol  = "email"
  endpoint  = "admin@example.com"
}
```

### `outputs.tf`

```hcl
output "migration_bucket" { value = aws_s3_bucket.migration_target.id }
output "sns_topic_arn"     { value = aws_sns_topic.snow_notifications.arn }
```

```bash
terraform init
terraform apply -auto-approve
```

## Step 2 -- Break-Even Analysis: When Snow Beats the Network

The critical architect skill is knowing when to recommend Snow Family over network transfer.

### Network Transfer Time Formula

```
Transfer time (seconds) = Data size (bits) / Bandwidth (bits per second)
```

### TODO 1: Calculate Transfer Times

Complete the break-even analysis by filling in the transfer times:

```bash
# Network transfer calculator
# Assumptions: 80% effective utilization (protocol overhead, congestion)

calculate_transfer_days() {
  local data_tb=$1
  local bandwidth_gbps=$2
  local utilization=0.80

  local data_bits=$(echo "$data_tb * 1024 * 1024 * 1024 * 1024 * 8" | bc)
  local effective_bps=$(echo "$bandwidth_gbps * 1000000000 * $utilization" | bc)
  local seconds=$(echo "scale=0; $data_bits / $effective_bps" | bc)
  local days=$(echo "scale=1; $seconds / 86400" | bc)

  echo "$days days"
}

# TODO: Fill in the expected transfer times in the comments
# Then run to verify your estimates

echo "=== Internet Transfer (1 Gbps) ==="
echo "10 TB:  $(calculate_transfer_days 10 1)"    # Expected: ___ days
echo "50 TB:  $(calculate_transfer_days 50 1)"    # Expected: ___ days
echo "100 TB: $(calculate_transfer_days 100 1)"   # Expected: ___ days
echo "500 TB: $(calculate_transfer_days 500 1)"   # Expected: ___ days

echo ""
echo "=== Direct Connect (10 Gbps) ==="
echo "10 TB:  $(calculate_transfer_days 10 10)"   # Expected: ___ days
echo "50 TB:  $(calculate_transfer_days 50 10)"   # Expected: ___ days
echo "100 TB: $(calculate_transfer_days 100 10)"  # Expected: ___ days
echo "500 TB: $(calculate_transfer_days 500 10)"  # Expected: ___ days
```

### TODO 2: Complete the Decision Table

Fill in the recommended approach for each scenario:

```
# TODO: Replace each "???" with the recommended transfer method
#
# Options:
#   Internet    - Standard internet transfer (S3 multipart, DataSync)
#   DX          - AWS Direct Connect dedicated link
#   Snowcone    - AWS Snowcone (8TB usable)
#   Snowball    - Snowball Edge Storage Optimized (80TB)
#   Snowball-C  - Snowball Edge Compute Optimized (42TB)
#   Multi-Snow  - Multiple Snowball devices in parallel
#   Snowmobile  - AWS Snowmobile (100PB)
#
# +----------+---------------+-----------------+-----------------+
# | Data     | 1 week        | 1 month         | No deadline     |
# +----------+---------------+-----------------+-----------------+
# | 5 TB     | ???           | ???             | ???             |
# | 50 TB    | ???           | ???             | ???             |
# | 500 TB   | ???           | ???             | ???             |
# | 5 PB     | ???           | ???             | ???             |
# | 50 PB    | ???           | ???             | ???             |
# +----------+---------------+-----------------+-----------------+
#
# Consider: Does the customer have Direct Connect already?
#           What is the available internet bandwidth?
#           Is there edge computing needed during transfer?
```

<details>
<summary>Solution: Decision Table</summary>

Assuming 1 Gbps internet and no existing Direct Connect:

| Data Volume | 1 Week Deadline | 1 Month Deadline | No Deadline |
|---|---|---|---|
| **5 TB** | Internet (DataSync) | Internet (DataSync) | Internet (S3 sync) |
| **50 TB** | Snowball (1 device) | Snowball or DX 10G | Internet (slow but works) |
| **500 TB** | Multi-Snow (7 devices) | Multi-Snow (3 devices) | DX 10G (~46 days) |
| **5 PB** | Multi-Snow (63+ devices) or Snowmobile | Multi-Snow (20+ devices) | DX 10G (~1.5 years) or Snow |
| **50 PB** | Snowmobile | Snowmobile | Snowmobile |

Key thresholds:
- **< 10 TB**: Internet transfer is usually fastest (avoid Snow device shipping time of 5-7 days)
- **10-80 TB**: Single Snowball Edge Storage Optimized
- **80 TB - 5 PB**: Multiple Snowball devices in parallel (up to 10 clustered)
- **> 10 PB**: Snowmobile becomes practical (shipping + loading takes weeks regardless)

The break-even point where Snow beats a 1 Gbps internet link is approximately **10 TB** when accounting for Snow device round-trip time (order + ship + load + ship back + import = ~10-14 days). For a 10 Gbps Direct Connect, the break-even rises to approximately **80-100 TB**.

</details>

## Step 3 -- Snow Family Job Workflow

### TODO 3: Order a Snow Device (CLI Reference -- Do NOT Execute)

```bash
# WARNING: Do NOT execute -- ordering incurs real costs ($300+ per job)

# Step 1: Create import job (TODO: identify missing encryption parameter)
aws snowball create-job \
  --job-type IMPORT \
  --resources '{"S3Resources": [{"BucketArn": "arn:aws:s3:::saa-ex87-migration-XXXXX", "KeyRange": {}}]}' \
  --address-id "ADID..." --role-arn "arn:aws:iam::123456789012:role/SnowballImportRole" \
  --snowball-type "EDGE" --shipping-option "SECOND_DAY" \
  --notification '{"SnsTopicARN": "arn:aws:sns:us-east-1:123456789012:snow-notifications", "JobStatesToNotify": ["Complete", "InTransit"]}' \
  --snowball-capacity-preference "T80"
  # TODO: Add --kms-key-arn for envelope encryption

# Step 2: Check status, get credentials, load data, ship back
aws snowball describe-job --job-id "JID..."
aws snowball get-job-unlock-code --job-id "JID..."
aws snowball get-job-manifest --job-id "JID..."
# On device: snowball cp /local/data s3://bucket-on-device/
# Ship back -- AWS imports to S3 and wipes the device
```

<details>
<summary>Solution: Missing Parameters</summary>

The missing parameter is `--kms-key-arn`. All Snow devices use 256-bit encryption. The `--kms-key-arn` specifies which KMS key provides envelope encryption. If omitted, AWS uses a default key. For compliance, always specify a customer-managed KMS key for key rotation and access control.

</details>

## Step 4 -- Migration Planning Scenario

### TODO 4: Design the Migration Strategy

A media company needs to migrate 200 TB of video archives from an on-premises data center to S3 Glacier Deep Archive. They have a 500 Mbps internet connection and need migration completed within 30 days. Their data grows by approximately 2 TB per week.

Complete the migration plan:

```
# Migration Plan Template
# -----------------------
# Total data: 200 TB
# Internet bandwidth: 500 Mbps
# Deadline: 30 days
# Data growth rate: 2 TB/week

# TODO: Answer the following questions

# Q1: How long would internet-only transfer take at 500 Mbps (80% utilization)?
# Answer: ___ days

# Q2: How many Snowball Edge Storage Optimized devices are needed?
# (Each device holds 80 TB)
# Answer: ___ devices

# Q3: What is the Snow device round-trip timeline?
# Order → Ship (5 days) → Load (??? days @ 100G link) → Ship back (5 days)
#                                                      → Import (??? days)
# Answer: ___ total days

# Q4: How much NEW data accumulates during the Snow round-trip?
# Answer: ___ TB

# Q5: What is the recommended hybrid strategy?
# Step 1: ___
# Step 2: ___
# Step 3: ___

# Q6: What S3 storage class should the data land in first, and why?
# Answer: ___
```

<details>
<summary>Solution: Migration Plan</summary>

**Q1:** 500 Mbps at 80% effective = 400 Mbps = 50 MB/s.
200 TB = 200,000 GB. At 50 MB/s = ~4,000,000 seconds = **~46 days**. Exceeds the 30-day deadline.

**Q2:** 200 TB / 80 TB per device = **3 devices** (2.5 rounded up).

**Q3:** Snow device round-trip timeline:
- Order processing: 1-2 days
- Ship to customer: 3-5 days
- Load data: 80 TB at effective 250 MB/s (25G link, loading overhead) = ~3.7 days per device, run in parallel = ~4 days
- Ship back to AWS: 3-5 days
- AWS import to S3: 1-2 days
- **Total: ~14-18 days** -- well within the 30-day deadline

**Q4:** During ~18-day round-trip, new data = 18/7 * 2 TB = **~5.1 TB** of new data.

**Q5:** Hybrid strategy:
1. Order 3 Snowball Edge Storage Optimized devices and load the 200 TB bulk data
2. While waiting for Snow import, set up AWS DataSync agent on-premises
3. After Snow import completes, use DataSync to transfer the ~5 TB of incremental data accumulated during the Snow round-trip (takes ~1 day at 500 Mbps)

**Q6:** Data should land in **S3 Standard** first, then use a lifecycle rule to transition to S3 Glacier Deep Archive. Reason: Snow Family imports to S3 Standard. Direct import to Glacier is not supported. A lifecycle rule with 0-day transition immediately moves objects to Deep Archive after import. This also allows a validation window -- you can verify data integrity before the lifecycle rule transitions objects.

</details>

## Spot the Bug

A solutions architect proposes the following migration plan. Identify the critical flaw:

```
Scenario: A hospital system needs to migrate 500 TB of medical imaging
data (DICOM files) to AWS. They have a 1 Gbps internet connection.
The migration must complete within 2 weeks for a facility consolidation.

Proposed Plan:
1. Set up AWS DataSync agent on-premises
2. Create DataSync task with bandwidth throttling at 800 Mbps
3. Schedule transfer to run 24/7 for 14 days
4. Data lands in S3 Standard-IA

Calculation: 800 Mbps = 100 MB/s = 8.64 TB/day
             500 TB / 8.64 TB/day = 57.8 days

Wait -- that is 58 days, not 14 days. Let us increase bandwidth
to 5 Gbps by upgrading the internet connection.

Revised Plan:
1. Upgrade internet connection to 5 Gbps (takes 4-6 weeks to provision)
2. Use DataSync at 4 Gbps effective
3. 500 TB / 43.2 TB/day = 11.5 days -- fits in 2 weeks!
```

<details>
<summary>Explain the bug</summary>

**Bug: The architect is trying to solve a Snow Family problem with network bandwidth, and the "fix" takes longer than the migration deadline.**

Issues: (1) The internet calculation proves Snow Family is needed (500 TB at 1 Gbps = 58 days), but the architect ignores it. (2) Upgrading to 5 Gbps takes 4-6 weeks to provision -- longer than the 2-week deadline. (3) Sustaining 4 Gbps continuously for 12 days is optimistic.

**Correct approach:** Order 7 Snowball Edge Storage Optimized devices (500/80 = 6.25, round up). Load in parallel over 3-4 days. Total: ~12-14 days.

**Cost comparison:** 7 Snowball jobs ~$2,100 vs internet transfer $45,000 ($0.09/GB x 500 TB) vs 5 Gbps upgrade $5,000-15,000/month. Snow import to S3 has no per-GB charge.

</details>

## Verify What You Learned

```bash
BUCKET=$(terraform output -raw migration_bucket)

# Verify the S3 bucket exists for migration target
aws s3api head-bucket --bucket "$BUCKET" 2>/dev/null && echo "Bucket exists"

# Verify bucket versioning (required for Snow imports to be idempotent)
aws s3api get-bucket-versioning --bucket "$BUCKET" \
  --query 'Status' --output text
```

Expected: `Bucket exists` and `Enabled`

```bash
# Verify SNS topic for notifications
aws sns list-topics --query "Topics[?contains(TopicArn, 'saa-ex87')]" --output table
```

Expected: One topic containing `saa-ex87-snow-notifications`.

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify resources are deleted:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

Exercise 88 covers **AWS DataSync for online data transfer**, the complement to Snow Family's offline approach. You will configure DataSync agents, transfer tasks with scheduling and bandwidth throttling, and compare DataSync with other online transfer options -- building on the understanding of when online vs offline transfer is appropriate from this exercise.

## Summary

- **Snow Family** provides offline data transfer for scenarios where network bandwidth cannot meet migration deadlines
- **Snowcone** (8 TB) is portable and lightweight for edge computing and small data collection at remote sites
- **Snowball Edge Storage Optimized** (80 TB) is the standard choice for bulk data migration to S3
- **Snowball Edge Compute Optimized** (42 TB + optional GPU) runs EC2 instances and Lambda at the edge for processing before transfer
- **Snowmobile** (100 PB) is a shipping container for datacenter-scale migrations exceeding 10 PB
- **Break-even analysis** is essential: Snow beats 1 Gbps internet at ~10 TB, beats 10 Gbps Direct Connect at ~80-100 TB
- All Snow devices use **256-bit encryption** with KMS-managed keys -- data is encrypted at rest on the device
- **Hybrid migration** combines Snow Family for bulk transfer with DataSync for incremental changes during the round-trip
- Snow Family imports data to **S3 Standard** only -- use lifecycle rules to transition to Glacier or other classes
- Device clustering (5-10 Snowball Edge devices) enables petabyte-scale migrations with parallel loading

## Reference

- [AWS Snow Family Overview](https://docs.aws.amazon.com/snowball/latest/developer-guide/whatisedge.html)
- [Snowball Edge Storage Optimized](https://docs.aws.amazon.com/snowball/latest/developer-guide/device-differences.html)
- [Snow Family Pricing](https://aws.amazon.com/snow/#pricing)
- [AWS Snowball create-job CLI](https://docs.aws.amazon.com/cli/latest/reference/snowball/create-job.html)

## Additional Resources

- [When to Use Snow Family vs Direct Connect](https://docs.aws.amazon.com/whitepapers/latest/aws-overview/migration-services.html) -- AWS guidance on selecting the right migration path
- [Snowball Edge Clustering](https://docs.aws.amazon.com/snowball/latest/developer-guide/BestPractices.html) -- how to cluster multiple devices for larger local storage
- [Data Migration Best Practices](https://docs.aws.amazon.com/prescriptive-guidance/latest/large-migration-guide/welcome.html) -- comprehensive guide covering hybrid migration strategies
- [Snow Family Security](https://docs.aws.amazon.com/snowball/latest/developer-guide/security.html) -- encryption, tamper evidence, and chain of custody details

# 90. CloudTrail API Auditing

<!--
difficulty: basic
concepts: [cloudtrail, management-events, data-events, trail, s3-log-delivery, integrity-validation, insights, event-selectors, multi-region-trail, organization-trail]
tools: [terraform, aws-cli]
estimated_time: 30m
bloom_level: understand
prerequisites: [89-cloudwatch-dashboards-custom-metrics]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** CloudTrail management events are free for the first copy (90-day Event History). Creating a trail to deliver logs to S3 incurs S3 storage costs (~$0.023/GB/month). Data events cost $0.10 per 100,000 events. Insights events cost $0.35 per 100,000 events analyzed. For this exercise with minimal API activity, total cost is ~$0.01/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 89 (CloudWatch Dashboards) or equivalent knowledge
- Understanding of S3 buckets and bucket policies
- Familiarity with IAM and API operations

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** how CloudTrail records API calls as events and the difference between management events and data events
- **Describe** the free 90-day Event History versus a custom trail with S3 delivery and longer retention
- **Identify** when CloudTrail Insights should be enabled to detect unusual API activity patterns
- **Construct** a multi-region trail with S3 delivery, log file integrity validation, and CloudWatch Logs integration
- **Distinguish** between management events (control plane: `CreateBucket`, `RunInstances`) and data events (data plane: `GetObject`, `PutObject`, `Invoke`)
- **Compare** single-region versus multi-region trails and single-account versus organization trails

## Why This Matters

CloudTrail is the compliance backbone of every AWS account, and the SAA-C03 exam tests it frequently because it intersects security, governance, and cost optimization. Every AWS API call -- whether from the console, CLI, SDK, or another AWS service -- is recorded as a CloudTrail event. By default, you get a 90-day Event History of management events for free, with no configuration required. But this default has critical limitations: it does not record data events (S3 object operations, Lambda invocations), it does not deliver logs to S3 for long-term retention, and it has no integrity validation to prove logs have not been tampered with.

The exam presents scenarios where the answer depends on understanding these distinctions. "A security team needs to investigate API calls from 6 months ago" -- Event History only retains 90 days, so you need a trail delivering to S3. "Detect when an unusual number of S3 objects are deleted" -- this requires data events (charged) plus CloudTrail Insights (also charged). "Prove to auditors that CloudTrail logs have not been modified" -- you need log file integrity validation enabled on the trail.

The cost model matters for architecture decisions. Management events are free for the first trail copy. Data events cost $0.10 per 100,000 events -- for a busy S3 bucket with millions of GetObject calls per day, this becomes significant. The architect must decide which buckets and functions warrant data event logging and which do not.

## Step 1 -- Create the Trail Infrastructure

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
  default     = "saa-ex90"
}
```

### `storage.tf`

```hcl
data "aws_caller_identity" "current" {}

resource "random_id" "suffix" {
  byte_length = 4
}

# ------------------------------------------------------------------
# S3 bucket for CloudTrail log delivery.
# CloudTrail requires a specific bucket policy that allows the
# CloudTrail service to write log files to the bucket.
# ------------------------------------------------------------------
resource "aws_s3_bucket" "trail_logs" {
  bucket        = "${var.project_name}-trail-logs-${random_id.suffix.hex}"
  force_destroy = true

  tags = { Name = "${var.project_name}-trail-logs" }
}

resource "aws_s3_bucket_versioning" "trail_logs" {
  bucket = aws_s3_bucket.trail_logs.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_public_access_block" "trail_logs" {
  bucket = aws_s3_bucket.trail_logs.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# ------------------------------------------------------------------
# Bucket policy: CloudTrail must be allowed to check the bucket ACL
# and put log files into the bucket. Without this policy, the trail
# creation will fail with an "insufficient S3 bucket policy" error.
# ------------------------------------------------------------------
data "aws_iam_policy_document" "trail_bucket" {
  statement {
    sid    = "AWSCloudTrailAclCheck"
    effect = "Allow"
    principals {
      type        = "Service"
      identifiers = ["cloudtrail.amazonaws.com"]
    }
    actions   = ["s3:GetBucketAcl"]
    resources = [aws_s3_bucket.trail_logs.arn]
  }

  statement {
    sid    = "AWSCloudTrailWrite"
    effect = "Allow"
    principals {
      type        = "Service"
      identifiers = ["cloudtrail.amazonaws.com"]
    }
    actions   = ["s3:PutObject"]
    resources = ["${aws_s3_bucket.trail_logs.arn}/AWSLogs/${data.aws_caller_identity.current.account_id}/*"]
    condition {
      test     = "StringEquals"
      variable = "s3:x-amz-acl"
      values   = ["bucket-owner-full-control"]
    }
  }
}

resource "aws_s3_bucket_policy" "trail_logs" {
  bucket = aws_s3_bucket.trail_logs.id
  policy = data.aws_iam_policy_document.trail_bucket.json
}
```

### `monitoring.tf`

```hcl
# ------------------------------------------------------------------
# CloudTrail trail: multi-region, with integrity validation.
#
# is_multi_region_trail: captures API calls in ALL regions, not just
# the region where the trail is created. Essential for detecting
# activity in regions you do not normally use (attacker lateral movement).
#
# enable_log_file_validation: creates a digest file every hour with
# SHA-256 hashes of all log files. You can use the AWS CLI to verify
# that no log files have been modified or deleted since delivery.
#
# include_global_service_events: captures events from global services
# (IAM, STS, CloudFront) that do not have a regional endpoint.
# ------------------------------------------------------------------
resource "aws_cloudtrail" "this" {
  name           = "${var.project_name}-trail"
  s3_bucket_name = aws_s3_bucket.trail_logs.id

  is_multi_region_trail         = true
  include_global_service_events = true
  enable_log_file_validation    = true
  enable_logging                = true

  # ------------------------------------------------------------------
  # Event selectors control which events the trail records.
  # Management events: recorded by default (CreateBucket, RunInstances, etc.)
  # Data events: must be explicitly configured (GetObject, PutObject, Invoke)
  # ------------------------------------------------------------------
  event_selector {
    read_write_type           = "All"
    include_management_events = true

    # Record S3 data events for all buckets
    # WARNING: This can be expensive for high-traffic buckets
    data_resource {
      type   = "AWS::S3::Object"
      values = ["arn:aws:s3"]  # All S3 buckets
    }
  }

  tags = { Name = "${var.project_name}-trail" }

  depends_on = [aws_s3_bucket_policy.trail_logs]
}
```

### `outputs.tf`

```hcl
output "trail_name" {
  value = aws_cloudtrail.this.name
}

output "trail_bucket" {
  value = aws_s3_bucket.trail_logs.id
}

output "trail_arn" {
  value = aws_cloudtrail.this.arn
}
```

```bash
terraform init
terraform apply -auto-approve
```

## Step 2 -- Explore and Validate

```bash
# Look up recent management events (free Event History, 90-day retention)
aws cloudtrail lookup-events \
  --start-time "$(date -u -v-1H +%Y-%m-%dT%H:%M:%SZ)" \
  --max-results 10 \
  --query 'Events[*].{Time:EventTime,Name:EventName,User:Username}' \
  --output table

# Check trail log delivery to S3 (may take 5-15 minutes)
BUCKET=$(terraform output -raw trail_bucket)
aws s3 ls "s3://$BUCKET/AWSLogs/" --recursive | head -10

# Validate log file integrity (SHA-256 digest verification)
TRAIL_ARN=$(terraform output -raw trail_arn)
aws cloudtrail validate-logs \
  --trail-arn "$TRAIL_ARN" \
  --start-time "$(date -u -v-1H +%Y-%m-%dT%H:%M:%SZ)" \
  --verbose
```

### CloudTrail Event Types Decision Table

| Event Type | Examples | Default Recording | Cost | Retention |
|---|---|---|---|---|
| **Management events** | CreateBucket, RunInstances, CreateUser | Yes (Event History) | Free (first copy) | 90 days (Event History), unlimited in S3 |
| **Data events (S3)** | GetObject, PutObject, DeleteObject | No (must enable) | $0.10 / 100K events | Unlimited in S3 |
| **Data events (Lambda)** | Invoke | No (must enable) | $0.10 / 100K events | Unlimited in S3 |
| **Insights events** | Unusual API call volume, error rates | No (must enable) | $0.35 / 100K events analyzed | Unlimited in S3 |

## Common Mistakes

### 1. Not enabling multi-region trail

**Wrong:** Setting `is_multi_region_trail = false`. An attacker launching resources in an unused region goes undetected. **Fix:** Always set `is_multi_region_trail = true` -- no additional cost, captures events from all regions.

### 2. Confusing Event History with a trail

**Wrong:** Relying on default Event History for compliance. Event History provides only 90 days of management events with no S3 delivery, no integrity validation, and no data events. Auditors requiring 6+ months of tamper-proof records need a trail with S3 delivery and log file integrity validation.

### 3. Enabling data events on all S3 buckets without cost analysis

**Wrong:** Using `values = ["arn:aws:s3"]` (all buckets). A bucket with 10M GetObject/day = $10/day in data events. **Fix:** Enable data events selectively for sensitive-data buckets only.

## Verify What You Learned

```bash
TRAIL_NAME=$(terraform output -raw trail_name)

# Verify trail is configured correctly
aws cloudtrail describe-trails \
  --trail-name-list "$TRAIL_NAME" \
  --query 'trailList[0].{Name:Name,MultiRegion:IsMultiRegionTrail,LogValidation:LogFileValidationEnabled,GlobalEvents:IncludeGlobalServiceEvents,Logging:IsLogging}' \
  --output table
```

Expected: `MultiRegion = True`, `LogValidation = True`, `GlobalEvents = True`.

```bash
# Verify trail is logging
aws cloudtrail get-trail-status --name "$TRAIL_NAME" \
  --query '{IsLogging:IsLogging,LatestDeliveryTime:LatestDeliveryTime}' \
  --output json
```

Expected: `IsLogging = true` with a recent `LatestDeliveryTime`.

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

Exercise 91 covers **AWS Config Rules and Remediation**, where you will configure AWS Config to record resource configurations, evaluate them against managed rules (like `s3-bucket-public-read-prohibited`), and set up automatic remediation using SSM Automation -- building on the audit foundation from CloudTrail to actively enforce compliance rather than just logging events.

## Summary

- **CloudTrail** records every AWS API call as an event, providing a complete audit trail for security and compliance
- **Event History** is free, requires no configuration, and retains 90 days of management events
- **Trails** deliver logs to S3 for unlimited retention, support integrity validation, and enable data event recording
- **Management events** (control plane) are free for the first trail copy -- always enable a multi-region trail
- **Data events** (data plane: S3 GetObject, Lambda Invoke) cost $0.10 per 100,000 events -- enable selectively
- **Insights events** detect unusual API activity patterns (spike in failed calls, unusual volume) at $0.35 per 100,000 events analyzed
- **Log file integrity validation** creates hourly SHA-256 digest files to prove logs have not been tampered with
- **Multi-region trails** capture API calls in all regions -- essential for detecting unauthorized activity in unused regions
- **Organization trails** apply a single trail configuration across all accounts in an AWS Organization
- CloudTrail logs in S3 can be queried with **Athena** for cost-effective historical analysis across millions of events

## Reference

- [AWS CloudTrail User Guide](https://docs.aws.amazon.com/awscloudtrail/latest/userguide/cloudtrail-user-guide.html)
- [CloudTrail Event Reference](https://docs.aws.amazon.com/awscloudtrail/latest/userguide/cloudtrail-event-reference.html)
- [Terraform aws_cloudtrail](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudtrail)
- [CloudTrail Pricing](https://aws.amazon.com/cloudtrail/pricing/)

## Additional Resources

- [Querying CloudTrail Logs with Athena](https://docs.aws.amazon.com/athena/latest/ug/cloudtrail-logs.html) -- SQL queries across millions of API events without any servers
- [CloudTrail Insights](https://docs.aws.amazon.com/awscloudtrail/latest/userguide/logging-insights-events-with-cloudtrail.html) -- automated anomaly detection for unusual API patterns
- [Organization Trails](https://docs.aws.amazon.com/awscloudtrail/latest/userguide/creating-trail-organization.html) -- centralized audit logging across all accounts in AWS Organizations
- [CloudTrail Log File Integrity Validation](https://docs.aws.amazon.com/awscloudtrail/latest/userguide/cloudtrail-log-file-validation-intro.html) -- cryptographic verification that logs have not been altered

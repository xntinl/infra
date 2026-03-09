# 76. Macie S3 Data Classification

<!--
difficulty: intermediate
concepts: [macie, s3-data-classification, pii-detection, sensitive-data-discovery, classification-job, managed-data-identifiers, custom-data-identifiers, macie-findings, regional-service, allow-lists]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: apply, analyze
prerequisites: [74-security-hub-compliance]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** Macie charges per GB scanned ($1/GB for first 50,000 GB/month). For this exercise with small test files, cost is negligible. Macie also charges per S3 bucket inventoried ($0.10/bucket/month after first free bucket). Total ~$0.01/hr. Remember to run `terraform destroy` and disable Macie when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Completed exercise 74 (Security Hub) | Understanding of findings aggregation |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Implement** Amazon Macie for sensitive data discovery in S3 buckets.
2. **Analyze** Macie findings to identify PII, credentials, and financial data in S3 objects.
3. **Apply** classification jobs to scan specific S3 buckets on a schedule.
4. **Evaluate** when Macie is the right tool versus S3 Object Lock, encryption, or access logging.
5. **Design** a data protection strategy that uses Macie for discovery and other services for remediation.

---

## Why This Matters

Amazon Macie answers the exam question "how do you discover sensitive data stored in S3?" The SAA-C03 tests whether you know that Macie uses machine learning and pattern matching to identify PII (names, addresses, social security numbers), financial data (credit card numbers, bank account numbers), and credentials (API keys, private keys) in S3 objects. Macie does not encrypt data, restrict access, or delete files -- it discovers and classifies, then sends findings to Security Hub.

The critical architectural detail that the exam tests: Macie is a regional service. A Macie job in us-east-1 can only scan S3 buckets in us-east-1. If your organization has buckets in multiple regions, you must enable Macie and create classification jobs in each region. The exam presents scenarios where "Macie is enabled but reports no findings for a bucket" and the answer is that the bucket is in a different region.

The cost model is also testable: Macie charges per GB of data scanned. For large data lakes with terabytes of data, running Macie across everything can be expensive. The architect's approach is to scope classification jobs to buckets most likely to contain sensitive data (uploads, user-generated content, data lake landing zones) rather than scanning everything.

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
    random = {
      source  = "hashicorp/random"
      version = "~> 3.0"
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
  default     = "saa-ex76"
}
```

### `main.tf`

```hcl
resource "random_id" "suffix" {
  byte_length = 4
}

data "aws_caller_identity" "current" {}

# ============================================================
# TODO 1: Enable Macie
# ============================================================
# Enable Amazon Macie in the account.
#
# Requirements:
#   - Resource: aws_macie2_account
#   - finding_publishing_frequency = "FIFTEEN_MINUTES"
#     (options: FIFTEEN_MINUTES, ONE_HOUR, SIX_HOURS)
#
# Macie automatically integrates with Security Hub once both
# are enabled in the same account and region.
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/macie2_account
# ============================================================


# ============================================================
# TODO 3: Macie Classification Job
# ============================================================
# Create a Macie classification job to scan the S3 bucket.
#
# Requirements:
#   - Resource: aws_macie2_classification_job
#   - job_type = "ONE_TIME" (alternatives: SCHEDULED)
#   - s3_job_definition with bucket_definitions pointing
#     to the test bucket
#   - depends_on = [aws_macie2_account.this]
#   - name = "${var.project_name}-scan-job"
#
# For ONE_TIME jobs, Macie scans all objects in the bucket
# once. For SCHEDULED jobs, Macie scans new/modified objects
# on a recurring schedule.
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/macie2_classification_job
# ============================================================


# ============================================================
# TODO 4: Review Macie Findings (CLI)
# ============================================================
# After terraform apply, wait 5-10 minutes for the
# classification job to complete, then review findings:
#
#   a) Check job status:
#      aws macie2 list-classification-jobs \
#        --query 'Items[*].{Name:Name,Status:JobStatus,Type:JobType}' \
#        --output table
#
#   b) List findings:
#      aws macie2 list-findings \
#        --query 'FindingIds' \
#        --output json
#
#   c) Get finding details (replace FINDING_ID):
#      aws macie2 get-findings \
#        --finding-ids "FINDING_ID" \
#        --query 'Findings[*].{Type:Type,Severity:Severity.Description,Resource:ResourcesAffected.S3Bucket.Name,Object:ResourcesAffected.S3Object.Key}'
#
#   d) View sensitive data statistics:
#      aws macie2 get-bucket-statistics \
#        --query '{Buckets:BucketsCount,Classifiable:ClassifiableObjectCount,SizeGB:ClassifiableSizeInBytes}'
# ============================================================
```

### `storage.tf`

```hcl
# ============================================================
# TODO 2: S3 Bucket with Test Data
# ============================================================
# Create an S3 bucket and upload test files containing
# simulated sensitive data for Macie to discover.
#
# Requirements:
#   - aws_s3_bucket named "${var.project_name}-data-${random_id.suffix.hex}"
#   - Upload test files using aws_s3_object:
#
#     File 1: "customers/records.csv" containing:
#       Name,Email,SSN,CreditCard
#       John Doe,john@example.com,123-45-6789,4111-1111-1111-1111
#       Jane Smith,jane@example.com,987-65-4321,5500-0000-0000-0004
#
#     File 2: "config/credentials.txt" containing:
#       AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE
#       AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
#
#     File 3: "reports/annual-report.txt" containing:
#       Annual Report 2024 - No sensitive data here.
#       Revenue: $1.2M, Expenses: $800K, Profit: $400K
#
# These files simulate common scenarios: PII in CSV exports,
# leaked credentials in config files, and clean business docs.
# ============================================================
```

### `outputs.tf`

```hcl
output "bucket_name" {
  value = "Set after TODO 2 implementation"
}

output "macie_job_id" {
  value = "Set after TODO 3 implementation"
}
```

---

## Macie Detection Categories

| Category | Examples | Managed Data Identifier |
|---|---|---|
| **PII** | SSN, passport, driver's license, phone, email, name, address | Yes |
| **Financial** | Credit card numbers, bank account numbers, IBAN | Yes |
| **Credentials** | AWS access keys, private keys, API tokens, passwords | Yes |
| **Health** | Health insurance IDs, medical record numbers (HIPAA) | Yes |
| **Custom** | Employee IDs, internal project codes, proprietary formats | Custom data identifiers required |

### Macie Cost Model

| Component | Price (us-east-1) |
|---|---|
| First bucket inventoried | Free |
| Additional buckets (per month) | $0.10/bucket |
| Data scanned (first 50,000 GB) | $1.00/GB |
| Data scanned (next 450,000 GB) | $0.50/GB |
| Data scanned (over 500,000 GB) | $0.25/GB |

For the exam: Macie is expensive for large-scale scanning. Scope jobs to high-risk buckets. Use S3 bucket-level inventory (free first bucket) to identify which buckets to scan deeply.

---

## Spot the Bug

The following configuration enables Macie and creates a classification job, but the job reports zero findings even though the bucket contains CSV files with credit card numbers. Identify why before expanding the answer.

```hcl
resource "aws_macie2_account" "this" {
  finding_publishing_frequency = "ONE_HOUR"
}

# Macie enabled in us-east-1
provider "aws" {
  region = "us-east-1"
}

resource "aws_macie2_classification_job" "scan" {
  depends_on = [aws_macie2_account.this]
  job_type   = "ONE_TIME"
  name       = "scan-customer-data"

  s3_job_definition {
    bucket_definitions {
      account_id = data.aws_caller_identity.current.account_id
      buckets    = ["customer-data-bucket-eu-west-1"]
    }
  }
}
```

The bucket `customer-data-bucket-eu-west-1` is in eu-west-1 and contains customer CSV exports with PII.

<details>
<summary>Explain the bug</summary>

**Macie is a regional service. The Macie account and classification job are in us-east-1, but the S3 bucket is in eu-west-1.** Macie can only scan buckets in the same region where it is enabled. The classification job either fails with an error or completes with zero findings because it cannot access the cross-region bucket.

This is one of the most commonly tested Macie concepts on the SAA-C03 exam. The bucket name contains "eu-west-1" as a hint, but in real scenarios the region mismatch is less obvious.

**Fix:** Enable Macie in eu-west-1 and create the classification job there:

```hcl
provider "aws" {
  alias  = "eu_west_1"
  region = "eu-west-1"
}

resource "aws_macie2_account" "eu" {
  provider                     = aws.eu_west_1
  finding_publishing_frequency = "ONE_HOUR"
}

resource "aws_macie2_classification_job" "scan" {
  provider   = aws.eu_west_1
  depends_on = [aws_macie2_account.eu]
  job_type   = "ONE_TIME"
  name       = "scan-customer-data"

  s3_job_definition {
    bucket_definitions {
      account_id = data.aws_caller_identity.current.account_id
      buckets    = ["customer-data-bucket-eu-west-1"]
    }
  }
}
```

**Key principle:** If your organization has S3 buckets in multiple regions, you must enable Macie in each region and create classification jobs in each region. For multi-account organizations, use the Macie administrator account (via AWS Organizations) to manage Macie across member accounts within each region.

</details>

---

## Verify What You Learned

1. **Deploy the infrastructure:**
   ```bash
   terraform init && terraform apply -auto-approve
   ```
   Wait 5-10 minutes for the classification job to complete.

2. **Verify Macie is enabled:**
   ```bash
   aws macie2 get-macie-session \
     --query '{Status:Status,FindingPublishingFrequency:FindingPublishingFrequency}' \
     --output table
   ```
   Expected: Status `ENABLED`.

3. **Check classification job status:**
   ```bash
   aws macie2 list-classification-jobs \
     --query 'Items[*].{Name:Name,Status:JobStatus,Type:JobType}' \
     --output table
   ```
   Expected: Job with status `COMPLETE` (may take 5-10 minutes).

4. **List findings:**
   ```bash
   FINDING_IDS=$(aws macie2 list-findings --query 'FindingIds' --output json)
   echo "$FINDING_IDS"
   ```
   Expected: Finding IDs for sensitive data detected in the test files.

5. **Get finding details:**
   ```bash
   FIRST_FINDING=$(aws macie2 list-findings --query 'FindingIds[0]' --output text)
   aws macie2 get-findings --finding-ids "$FIRST_FINDING" \
     --query 'Findings[0].{Type:Type,Severity:Severity.Description,Bucket:ResourcesAffected.S3Bucket.Name,Object:ResourcesAffected.S3Object.Key}' \
     --output table
   ```
   Expected: Finding showing sensitive data type (e.g., `SensitiveData:S3Object/Personal`) with the affected object key.

6. **Terraform state consistency:**
   ```bash
   terraform plan
   ```
   Expected: `No changes. Your infrastructure matches the configuration.`

---

## Solutions

<details>
<summary>TODO 1 -- Enable Macie (main.tf)</summary>

```hcl
resource "aws_macie2_account" "this" {
  finding_publishing_frequency = "FIFTEEN_MINUTES"
}
```

Macie is enabled per account per region. The `finding_publishing_frequency` controls how often Macie publishes findings to Security Hub and EventBridge.

</details>

<details>
<summary>TODO 2 -- S3 Bucket with Test Data (storage.tf)</summary>

```hcl
resource "aws_s3_bucket" "data" {
  bucket        = "${var.project_name}-data-${random_id.suffix.hex}"
  force_destroy = true
}

resource "aws_s3_object" "customers" {
  bucket  = aws_s3_bucket.data.id
  key     = "customers/records.csv"
  content = <<-CSV
    Name,Email,SSN,CreditCard
    John Doe,john@example.com,123-45-6789,4111-1111-1111-1111
    Jane Smith,jane@example.com,987-65-4321,5500-0000-0000-0004
  CSV
}

resource "aws_s3_object" "credentials" {
  bucket  = aws_s3_bucket.data.id
  key     = "config/credentials.txt"
  content = <<-TXT
    AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE
    AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
  TXT
}

resource "aws_s3_object" "report" {
  bucket  = aws_s3_bucket.data.id
  key     = "reports/annual-report.txt"
  content = <<-TXT
    Annual Report 2024 - No sensitive data here.
    Revenue: $1.2M, Expenses: $800K, Profit: $400K
  TXT
}

```

Update `outputs.tf`:

```hcl
output "bucket_name" {
  value = aws_s3_bucket.data.id
}
```

Macie should find sensitive data in `customers/records.csv` (SSN, credit card, PII) and `config/credentials.txt` (AWS credentials), but not in `reports/annual-report.txt`.

</details>

<details>
<summary>TODO 3 -- Macie Classification Job (main.tf)</summary>

```hcl
resource "aws_macie2_classification_job" "scan" {
  depends_on = [aws_macie2_account.this]
  job_type   = "ONE_TIME"
  name       = "${var.project_name}-scan-job"

  s3_job_definition {
    bucket_definitions {
      account_id = data.aws_caller_identity.current.account_id
      buckets    = [aws_s3_bucket.data.id]
    }
  }

  tags = { Name = "${var.project_name}-scan-job" }
}

```

Update `outputs.tf`:

```hcl
output "macie_job_id" {
  value = aws_macie2_classification_job.scan.id
}
```

`ONE_TIME` scans all objects once. `SCHEDULED` scans new or modified objects on a recurring basis, which is better for continuous monitoring of landing zones or upload buckets.

</details>

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Disable Macie if not destroyed by Terraform:

```bash
aws macie2 disable-macie
```

Verify:

```bash
aws macie2 get-macie-session 2>&1 | grep -q "Macie is not enabled" && echo "Macie disabled" || echo "Still enabled"
```

---

## What's Next

Exercise 77 begins the **Serverless** section with **Lambda event sources and invocation patterns**. You will implement Lambda functions triggered by synchronous sources (API Gateway, ALB), asynchronous sources (S3, SNS, EventBridge), and polling sources (SQS, Kinesis, DynamoDB Streams) -- understanding the different error handling and retry behaviors for each pattern.

---

## Summary

- **Macie** discovers and classifies sensitive data (PII, credentials, financial) in S3 using machine learning and pattern matching
- **Macie is regional** -- it can only scan S3 buckets in the same region where it is enabled
- **Classification jobs** can be ONE_TIME (scan once) or SCHEDULED (scan new/modified objects on a recurring basis)
- **Managed data identifiers** detect common patterns (SSN, credit cards, AWS keys) without configuration
- **Custom data identifiers** use regex and proximity rules to detect organization-specific patterns (employee IDs, project codes)
- **Findings flow to Security Hub** automatically for aggregation with GuardDuty, Inspector, and Config findings
- **Cost scales with data volume** ($1/GB for first 50,000 GB) -- scope jobs to high-risk buckets rather than scanning everything
- **Bucket inventory** is free for the first bucket and provides metadata (encryption status, public access, object count) without scanning content
- **Allow lists** suppress findings for known-safe patterns (test data, approved credentials)
- **Macie does not remediate** -- it discovers. Use EventBridge + Lambda or S3 policies for remediation actions

## Reference

- [Amazon Macie User Guide](https://docs.aws.amazon.com/macie/latest/user/what-is-macie.html)
- [Macie Managed Data Identifiers](https://docs.aws.amazon.com/macie/latest/user/managed-data-identifiers.html)
- [Terraform aws_macie2_account](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/macie2_account)
- [Macie Pricing](https://aws.amazon.com/macie/pricing/)

## Additional Resources

- [Custom Data Identifiers](https://docs.aws.amazon.com/macie/latest/user/custom-data-identifiers.html) -- regex-based detection for organization-specific sensitive data
- [Macie Allow Lists](https://docs.aws.amazon.com/macie/latest/user/allow-lists.html) -- suppress findings for known-safe data patterns
- [Multi-Account Macie](https://docs.aws.amazon.com/macie/latest/user/macie-accounts.html) -- administrator account managing Macie across an organization
- [Automated Remediation with Macie](https://docs.aws.amazon.com/macie/latest/user/findings-publish-event-schemas.html) -- EventBridge event patterns for Macie findings

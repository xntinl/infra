# 13. Well-Architected Review and Remediation

<!--
difficulty: advanced
concepts: [well-architected-framework, six-pillars, operational-excellence, security, reliability, performance, cost, sustainability, hri, mri]
tools: [terraform, aws-cli]
estimated_time: 75m
bloom_level: evaluate
prerequisites: [none]
aws_cost: ~$0.05/hr
-->

> **AWS Cost Warning:** Deliberately flawed infrastructure with small instances. ~$0.05/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercises 01-12 or equivalent knowledge
- Familiarity with the AWS Well-Architected Framework concepts
- Understanding of encryption, backup, and monitoring fundamentals

## Learning Objectives

After completing this exercise, you will be able to:

- **Evaluate** an existing architecture against the six pillars of the AWS Well-Architected Framework
- **Design** remediation plans that prioritize High Risk Issues (HRIs) over Medium Risk Issues (MRIs)
- **Implement** Terraform changes that fix security, reliability, and cost optimization issues without rebuilding the entire stack
- **Analyze** the trade-offs between immediate remediation cost and long-term risk reduction

## Why This Matters

The Well-Architected Framework is not just an exam topic -- it is the structured methodology AWS Solutions Architects use in every customer engagement. The SAA-C03 exam tests your ability to identify architectural anti-patterns and recommend specific remediations. In practice, most architectures are not built perfectly from day one. Teams inherit flawed infrastructure, accumulate technical debt, and make trade-offs under time pressure. Your job as an architect is to systematically identify the highest-risk issues and fix them in priority order. This exercise gives you a deliberately broken architecture and asks you to find and fix the problems using the same framework AWS uses internally.

## The Challenge

You are brought in to review an existing AWS architecture. The previous team deployed quickly and skipped many best practices. Your job is to conduct a Well-Architected Review, identify all High Risk Issues (HRIs) and Medium Risk Issues (MRIs), and remediate the top 5 issues.

### Requirements

1. Deploy the deliberately flawed architecture via Terraform (provided below)
2. Conduct a review against all 6 pillars, documenting findings as HRI or MRI
3. Prioritize findings by risk and effort
4. Remediate the top 5 issues by modifying the Terraform configuration
5. Verify each remediation with AWS CLI commands

### The Flawed Architecture

The Terraform code below contains at least one serious issue per pillar. Deploy it, then find and fix the problems:

```hcl
# DELIBERATELY FLAWED -- DO NOT USE IN PRODUCTION
# This architecture has intentional anti-patterns for the exercise.

resource "aws_db_instance" "this" {
  identifier        = "wa-review-demo"
  engine            = "mysql"
  engine_version    = "8.0"
  instance_class    = "db.m5.xlarge"     # Issue: oversized for workload
  allocated_storage = 20
  storage_type      = "gp3"

  db_name  = "appdb"
  username = "admin"
  password = "admin123"                   # Issue: weak password

  multi_az            = false             # Issue: single-AZ
  storage_encrypted   = false             # Issue: no encryption at rest

  backup_retention_period = 0             # Issue: no automated backups
  skip_final_snapshot     = true

  publicly_accessible     = true          # Issue: database exposed to internet
  vpc_security_group_ids  = [aws_security_group.rds.id]
  db_subnet_group_name    = aws_db_subnet_group.this.name
}

resource "aws_s3_bucket" "this" {
  bucket = "wa-review-demo-${data.aws_caller_identity.current.account_id}"
}

resource "aws_s3_bucket_public_access_block" "this" {
  bucket = aws_s3_bucket.this.id

  block_public_acls       = false         # Issue: public access not blocked
  block_public_policy     = false
  ignore_public_acls      = false
  restrict_public_buckets = false
}

# No CloudWatch alarms                    # Issue: no monitoring
# No VPC Flow Logs                        # Issue: no network visibility
# No S3 bucket encryption                 # Issue: no encryption at rest
# No S3 bucket versioning                 # Issue: no data protection
```

### Well-Architected Review Findings

| # | Pillar | Severity | Finding | Remediation |
|---|---|---|---|---|
| 1 | Security | **HRI** | RDS not encrypted at rest | Enable `storage_encrypted = true` with KMS |
| 2 | Security | **HRI** | S3 public access block disabled | Enable all four `block_public_access` settings |
| 3 | Security | **HRI** | RDS publicly accessible | Set `publicly_accessible = false` |
| 4 | Security | MRI | Weak database password | Use Secrets Manager for credential rotation |
| 5 | Security | MRI | No VPC Flow Logs | Enable Flow Logs for network audit trail |
| 6 | Reliability | **HRI** | Single-AZ RDS (no Multi-AZ) | Enable `multi_az = true` |
| 7 | Reliability | **HRI** | No automated backups | Set `backup_retention_period = 7` |
| 8 | Reliability | MRI | No S3 versioning | Enable bucket versioning |
| 9 | Operational Excellence | **HRI** | No CloudWatch alarms | Add CPU, storage, and connection alarms |
| 10 | Operational Excellence | MRI | No VPC Flow Logs | Enable for troubleshooting and compliance |
| 11 | Performance | MRI | Oversized instance (m5.xlarge) | Right-size to db.t3.medium based on workload metrics |
| 12 | Cost Optimization | **HRI** | Oversized instance (m5.xlarge for tiny workload) | Right-size -- saves ~$200/month |
| 13 | Cost Optimization | MRI | No S3 lifecycle policy | Add lifecycle rules for old object transition |
| 14 | Sustainability | MRI | Oversized compute wastes energy | Right-sizing reduces carbon footprint |

## Hints

<details>
<summary>Hint 1: Security Pillar -- Encryption and Access Control</summary>

The security pillar has three HRIs and two MRIs. Fix the HRIs first:

**HRI 1 -- RDS Encryption at Rest:**

```hcl
resource "aws_db_instance" "this" {
  # ...
  storage_encrypted = true
  kms_key_id        = aws_kms_key.rds.arn  # Optional: use custom KMS key
}
```

Note: You cannot enable encryption on an existing unencrypted RDS instance. You must create an encrypted snapshot, restore from it, and switch the endpoint. For this exercise, simply set the flag and let Terraform recreate the instance.

**HRI 2 -- S3 Public Access Block:**

```hcl
resource "aws_s3_bucket_public_access_block" "this" {
  bucket = aws_s3_bucket.this.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}
```

AWS recommends enabling all four settings as a default. If specific buckets need public access (rare), create an exception with documentation.

**HRI 3 -- RDS Publicly Accessible:**

```hcl
resource "aws_db_instance" "this" {
  # ...
  publicly_accessible = false
}
```

Databases should never be directly accessible from the internet. Applications connect via private subnets; administrators connect via bastion hosts or SSM Session Manager.

</details>

<details>
<summary>Hint 2: Reliability Pillar -- Multi-AZ and Backups</summary>

**HRI -- Single-AZ RDS:**

```hcl
resource "aws_db_instance" "this" {
  # ...
  multi_az = true
}
```

Multi-AZ creates a synchronous standby in another AZ. Failover is automatic via DNS update (60-120 seconds). This doubles compute cost but eliminates single-AZ failure as a risk.

**HRI -- No Automated Backups:**

```hcl
resource "aws_db_instance" "this" {
  # ...
  backup_retention_period = 7      # Keep 7 days of automated backups
  backup_window           = "03:00-04:00"  # Off-peak hours
}
```

With `backup_retention_period = 0`, you cannot perform point-in-time recovery (PITR). If the database is corrupted or accidentally deleted, data is permanently lost. PITR with 7-day retention costs minimal additional storage and provides recovery to any second within the retention window.

**MRI -- S3 Versioning:**

```hcl
resource "aws_s3_bucket_versioning" "this" {
  bucket = aws_s3_bucket.this.id
  versioning_configuration {
    status = "Enabled"
  }
}
```

Versioning protects against accidental deletes and overwrites. Combined with lifecycle rules to expire old versions after 30 days, the cost impact is minimal.

</details>

<details>
<summary>Hint 3: Operational Excellence Pillar -- Monitoring and Logging</summary>

**HRI -- No CloudWatch Alarms:**

Every production database needs at minimum these alarms:

```hcl
resource "aws_cloudwatch_metric_alarm" "cpu" {
  alarm_name          = "wa-review-rds-high-cpu"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  metric_name         = "CPUUtilization"
  namespace           = "AWS/RDS"
  period              = 300
  statistic           = "Average"
  threshold           = 80
  alarm_description   = "RDS CPU above 80% for 15 minutes"
  dimensions = {
    DBInstanceIdentifier = aws_db_instance.this.identifier
  }
  alarm_actions = [aws_sns_topic.alerts.arn]
}

resource "aws_cloudwatch_metric_alarm" "storage" {
  alarm_name          = "wa-review-rds-low-storage"
  comparison_operator = "LessThanThreshold"
  evaluation_periods  = 1
  metric_name         = "FreeStorageSpace"
  namespace           = "AWS/RDS"
  period              = 300
  statistic           = "Average"
  threshold           = 2000000000  # 2 GB
  alarm_description   = "RDS free storage below 2 GB"
  dimensions = {
    DBInstanceIdentifier = aws_db_instance.this.identifier
  }
  alarm_actions = [aws_sns_topic.alerts.arn]
}

resource "aws_sns_topic" "alerts" {
  name = "wa-review-alerts"
}
```

**MRI -- No VPC Flow Logs:**

```hcl
resource "aws_flow_log" "this" {
  vpc_id          = aws_vpc.this.id
  traffic_type    = "ALL"
  iam_role_arn    = aws_iam_role.flow_logs.arn
  log_destination = aws_cloudwatch_log_group.flow_logs.arn
}

resource "aws_cloudwatch_log_group" "flow_logs" {
  name              = "/aws/vpc/wa-review-demo"
  retention_in_days = 30
}
```

VPC Flow Logs capture IP traffic metadata for every ENI in the VPC. Essential for security auditing, troubleshooting connectivity issues, and compliance requirements.

</details>

<details>
<summary>Hint 4: Cost Optimization and Performance Pillars -- Right-Sizing</summary>

**HRI -- Oversized Instance:**

The flawed architecture uses `db.m5.xlarge` (4 vCPU, 16 GiB RAM, ~$0.342/hr) for a workload that barely uses 5% CPU. This wastes ~$200/month compared to an appropriately sized instance.

Right-sizing process:
1. Check CloudWatch metrics: CPUUtilization, FreeableMemory, DatabaseConnections
2. If CPU is consistently below 20% and memory is >80% free, the instance is oversized
3. Step down one size at a time, monitor for a week, repeat

```hcl
resource "aws_db_instance" "this" {
  # ...
  instance_class = "db.t3.medium"  # 2 vCPU, 4 GiB -- sufficient for this workload
}
```

Cost comparison:

| Instance | vCPU | RAM | On-Demand $/hr | Monthly |
|---|---|---|---|---|
| db.m5.xlarge | 4 | 16 GiB | $0.342 | ~$246 |
| db.m5.large | 2 | 8 GiB | $0.171 | ~$123 |
| db.t3.medium | 2 | 4 GiB | $0.068 | ~$49 |

For a tiny workload, db.t3.medium provides adequate resources at 80% less cost.

**Performance consideration:** Right-sizing is not just about cost. An oversized instance masks performance problems because there is always spare capacity. A right-sized instance forces you to optimize queries and use caching, which improves overall architecture quality.

</details>

<details>
<summary>Hint 5: Sustainability Pillar and Prioritization</summary>

The Sustainability pillar (added as the 6th pillar) focuses on reducing the environmental impact of your cloud workloads. For this exercise, the main sustainability finding is the same as the cost optimization finding: oversized instances waste energy.

**Prioritization framework for Well-Architected remediations:**

| Priority | Criteria | Example from this exercise |
|---|---|---|
| P0 (immediate) | Security HRI with active exposure | S3 public access, RDS publicly accessible |
| P1 (this sprint) | Reliability HRI | Single-AZ RDS, no backups |
| P2 (next sprint) | Operational HRI + Security MRI | No alarms, no VPC Flow Logs, weak password |
| P3 (backlog) | Cost/Performance MRI | Oversized instance, no lifecycle policy |
| P4 (track) | Sustainability MRI | Right-sizing (often covered by P3) |

Fix P0 issues immediately because they represent active security exposure. P1 issues are the next priority because they represent risk of data loss. P2 and below can be scheduled into normal development cycles.

For this exercise, the top 5 remediations in priority order are:
1. Enable S3 public access block (P0 -- data exposure)
2. Set RDS `publicly_accessible = false` (P0 -- attack surface)
3. Enable RDS encryption at rest (P1 -- compliance)
4. Enable Multi-AZ + backups (P1 -- data loss risk)
5. Add CloudWatch alarms (P2 -- operational blindness)

</details>

## Spot the Bug

A team "fixes" the S3 public access concern by adding a bucket ACL while leaving the public access block disabled:

```hcl
resource "aws_s3_bucket" "this" {
  bucket = "wa-review-demo-data"
}

resource "aws_s3_bucket_acl" "this" {
  bucket = aws_s3_bucket.this.id
  acl    = "private"
}

resource "aws_s3_bucket_public_access_block" "this" {
  bucket = aws_s3_bucket.this.id

  block_public_acls       = false
  block_public_policy     = false
  ignore_public_acls      = false
  restrict_public_buckets = false
}
```

<details>
<summary>Explain the bug</summary>

Setting `acl = "private"` only affects the bucket-level ACL at creation time. It does NOT prevent:

1. **Object-level ACLs:** Any `PutObject` call with `--acl public-read` makes that individual object public, even though the bucket ACL is "private."
2. **Bucket policies:** Anyone with `s3:PutBucketPolicy` permission can add a policy granting public access.
3. **Future changes:** The "private" ACL can be changed later by anyone with `s3:PutBucketAcl` permission.

The `aws_s3_bucket_public_access_block` with all settings set to `false` provides zero protection. This is a defense-in-depth failure -- the team relied on a single control (bucket ACL) that can be easily overridden.

The fix is to enable all four public access block settings, which act as a guardrail that cannot be bypassed by ACLs or bucket policies:

```hcl
resource "aws_s3_bucket_public_access_block" "this" {
  bucket = aws_s3_bucket.this.id

  block_public_acls       = true   # Rejects PutObject calls with public ACLs
  block_public_policy     = true   # Rejects bucket policies that grant public access
  ignore_public_acls      = true   # Ignores any existing public ACLs
  restrict_public_buckets = true   # Restricts access to bucket with public policies
}
```

With these settings enabled, even if someone sets `acl = "public-read"` on the bucket or an object, the public access block overrides it and the data remains private. This is the AWS-recommended default for all buckets unless there is a documented business reason for public access.

</details>

## Verify What You Learned

After applying your remediations, verify each fix:

```bash
# 1. Verify RDS encryption is enabled
aws rds describe-db-instances \
  --db-instance-identifier wa-review-demo \
  --query "DBInstances[0].StorageEncrypted" \
  --output text
```

Expected: `True`

```bash
# 2. Verify S3 public access block
aws s3api get-public-access-block \
  --bucket "wa-review-demo-${ACCOUNT_ID}" \
  --query "PublicAccessBlockConfiguration"
```

Expected: all four settings `true`.

```bash
# 3. Verify RDS is not publicly accessible
aws rds describe-db-instances \
  --db-instance-identifier wa-review-demo \
  --query "DBInstances[0].PubliclyAccessible" \
  --output text
```

Expected: `False`

```bash
# 4. Verify Multi-AZ is enabled
aws rds describe-db-instances \
  --db-instance-identifier wa-review-demo \
  --query "DBInstances[0].{MultiAZ:MultiAZ,BackupRetention:BackupRetentionPeriod}" \
  --output table
```

Expected: MultiAZ=True, BackupRetention=7.

```bash
# 5. Verify CloudWatch alarms exist
aws cloudwatch describe-alarms \
  --alarm-name-prefix "wa-review" \
  --query "MetricAlarms[*].{Name:AlarmName,Metric:MetricName,State:StateValue}" \
  --output table
```

Expected: at least 2 alarms (CPU and storage).

```bash
# 6. Verify VPC Flow Logs
aws ec2 describe-flow-logs \
  --filter Name=resource-id,Values="$VPC_ID" \
  --query "FlowLogs[0].{Status:FlowLogStatus,Traffic:TrafficType}" \
  --output table
```

Expected: Status=ACTIVE, Traffic=ALL.

## Cleanup

Destroy all resources:

```bash
terraform destroy -auto-approve
```

Verify cleanup:

```bash
aws rds describe-db-instances --db-instance-identifier wa-review-demo 2>&1 || echo "RDS deleted"
aws s3api head-bucket --bucket "wa-review-demo-${ACCOUNT_ID}" 2>&1 || echo "Bucket deleted"
```

Expected: "not found" errors confirming deletion.

## What's Next

You have conducted a Well-Architected Review and remediated the most critical findings. In the next exercise, you will compare **Global Accelerator vs CloudFront** by deploying both in front of the same multi-region backend and measuring the differences in latency, failover behavior, and protocol support.

## Summary

- The Well-Architected Framework has **6 pillars**: Operational Excellence, Security, Reliability, Performance Efficiency, Cost Optimization, and Sustainability
- Findings are classified as **High Risk Issues (HRI)** or **Medium Risk Issues (MRI)** based on severity and likelihood
- **Security HRIs** (data exposure, unencrypted storage) should be fixed immediately -- they represent active risk
- **Reliability HRIs** (no backups, single-AZ) should be fixed next -- they represent risk of data loss
- **S3 public access block** is the only reliable way to prevent public access -- bucket ACLs alone are insufficient
- **RDS encryption** cannot be enabled on an existing instance -- you must create an encrypted copy and migrate
- **Right-sizing** addresses Cost Optimization, Performance, and Sustainability simultaneously
- **CloudWatch alarms** are not optional -- without them you are operationally blind to issues until users report them
- **VPC Flow Logs** provide the network audit trail needed for security investigation and compliance
- Prioritize remediations by **risk and exposure**, not by ease of implementation

## Reference

- [AWS Well-Architected Framework](https://docs.aws.amazon.com/wellarchitected/latest/framework/welcome.html)
- [Well-Architected Tool](https://docs.aws.amazon.com/wellarchitected/latest/userguide/intro.html)
- [S3 Block Public Access](https://docs.aws.amazon.com/AmazonS3/latest/userguide/access-control-block-public-access.html)
- [RDS Encryption at Rest](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/Overview.Encryption.html)
- [VPC Flow Logs](https://docs.aws.amazon.com/vpc/latest/userguide/flow-logs.html)

## Additional Resources

- [Well-Architected Labs](https://www.wellarchitectedlabs.com/) -- hands-on labs for each pillar
- [AWS Trusted Advisor](https://docs.aws.amazon.com/awssupport/latest/user/trusted-advisor.html) -- automated checks that identify many Well-Architected issues
- [AWS Config Rules](https://docs.aws.amazon.com/config/latest/developerguide/managed-rules-by-aws-config.html) -- continuous compliance monitoring for best practices
- [Cost Explorer Right-Sizing Recommendations](https://docs.aws.amazon.com/cost-management/latest/userguide/ce-rightsizing.html) -- data-driven instance sizing suggestions

<details>
<summary>Full Solution -- Remediated Version</summary>

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
  default     = "wa-review-demo"
}
```

### `vpc.tf`

```hcl
data "aws_availability_zones" "available" { state = "available" }

resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags = { Name = var.project_name }
}

resource "aws_subnet" "private_a" {
  vpc_id            = aws_vpc.this.id
  cidr_block        = "10.0.1.0/24"
  availability_zone = data.aws_availability_zones.available.names[0]
  tags = { Name = "${var.project_name}-private-a" }
}

resource "aws_subnet" "private_b" {
  vpc_id            = aws_vpc.this.id
  cidr_block        = "10.0.2.0/24"
  availability_zone = data.aws_availability_zones.available.names[1]
  tags = { Name = "${var.project_name}-private-b" }
}

resource "aws_flow_log" "this" {
  vpc_id          = aws_vpc.this.id
  traffic_type    = "ALL"
  iam_role_arn    = aws_iam_role.flow_logs.arn
  log_destination = aws_cloudwatch_log_group.flow_logs.arn
}
```

### `security.tf`

```hcl
resource "aws_security_group" "rds" {
  name_prefix = "${var.project_name}-rds-"
  vpc_id      = aws_vpc.this.id
  description = "RDS access from VPC only"

  ingress {
    from_port   = 3306
    to_port     = 3306
    protocol    = "tcp"
    cidr_blocks = [aws_vpc.this.cidr_block]
    description = "MySQL from VPC"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}
```

### `database.tf`

```hcl
resource "aws_kms_key" "rds" {
  description             = "KMS key for RDS encryption"
  deletion_window_in_days = 7
  enable_key_rotation     = true
}

resource "aws_db_subnet_group" "this" {
  name       = var.project_name
  subnet_ids = [aws_subnet.private_a.id, aws_subnet.private_b.id]
}

resource "aws_db_instance" "this" {
  identifier        = var.project_name
  engine            = "mysql"
  engine_version    = "8.0"
  instance_class    = "db.t3.medium"       # FIXED: right-sized from m5.xlarge
  allocated_storage = 20
  storage_type      = "gp3"

  db_name  = "appdb"
  username = "admin"
  password = "Str0ngP@ssw0rd!2024xK9m"     # FIXED: strong password

  multi_az            = true                # FIXED: Multi-AZ enabled
  storage_encrypted   = true                # FIXED: encryption at rest
  kms_key_id          = aws_kms_key.rds.arn

  backup_retention_period = 7               # FIXED: 7-day backup retention
  backup_window           = "03:00-04:00"
  maintenance_window      = "Mon:04:00-Mon:05:00"

  publicly_accessible     = false           # FIXED: not publicly accessible
  vpc_security_group_ids  = [aws_security_group.rds.id]
  db_subnet_group_name    = aws_db_subnet_group.this.name

  skip_final_snapshot = true
  apply_immediately   = true

  tags = { Name = var.project_name }
}
```

### `storage.tf`

```hcl
data "aws_caller_identity" "current" {}

resource "aws_s3_bucket" "this" {
  bucket        = "${var.project_name}-${data.aws_caller_identity.current.account_id}"
  force_destroy = true
  tags          = { Name = var.project_name }
}

resource "aws_s3_bucket_public_access_block" "this" {
  bucket = aws_s3_bucket.this.id

  block_public_acls       = true            # FIXED
  block_public_policy     = true            # FIXED
  ignore_public_acls      = true            # FIXED
  restrict_public_buckets = true            # FIXED
}

resource "aws_s3_bucket_versioning" "this" {  # ADDED: versioning
  bucket = aws_s3_bucket.this.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "this" {  # ADDED: encryption
  bucket = aws_s3_bucket.this.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "aws:kms"
    }
    bucket_key_enabled = true
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "this" {  # ADDED: lifecycle
  bucket = aws_s3_bucket.this.id
  rule {
    id     = "expire-old-versions"
    status = "Enabled"
    noncurrent_version_expiration {
      noncurrent_days = 30
    }
  }
}
```

### `iam.tf`

```hcl
data "aws_iam_policy_document" "flow_logs_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["vpc-flow-logs.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "flow_logs" {
  name               = "${var.project_name}-flow-logs"
  assume_role_policy = data.aws_iam_policy_document.flow_logs_assume.json
}

data "aws_iam_policy_document" "flow_logs" {
  statement {
    actions = [
      "logs:CreateLogGroup",
      "logs:CreateLogStream",
      "logs:PutLogEvents",
      "logs:DescribeLogGroups",
      "logs:DescribeLogStreams",
    ]
    resources = ["*"]
  }
}

resource "aws_iam_role_policy" "flow_logs" {
  name   = "flow-logs"
  role   = aws_iam_role.flow_logs.id
  policy = data.aws_iam_policy_document.flow_logs.json
}
```

### `monitoring.tf`

```hcl
resource "aws_sns_topic" "alerts" {
  name = "${var.project_name}-alerts"
}

resource "aws_cloudwatch_metric_alarm" "cpu" {
  alarm_name          = "${var.project_name}-rds-high-cpu"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  metric_name         = "CPUUtilization"
  namespace           = "AWS/RDS"
  period              = 300
  statistic           = "Average"
  threshold           = 80
  alarm_description   = "RDS CPU above 80% for 15 minutes"

  dimensions = {
    DBInstanceIdentifier = aws_db_instance.this.identifier
  }

  alarm_actions = [aws_sns_topic.alerts.arn]
}

resource "aws_cloudwatch_metric_alarm" "storage" {
  alarm_name          = "${var.project_name}-rds-low-storage"
  comparison_operator = "LessThanThreshold"
  evaluation_periods  = 1
  metric_name         = "FreeStorageSpace"
  namespace           = "AWS/RDS"
  period              = 300
  statistic           = "Average"
  threshold           = 2000000000
  alarm_description   = "RDS free storage below 2 GB"

  dimensions = {
    DBInstanceIdentifier = aws_db_instance.this.identifier
  }

  alarm_actions = [aws_sns_topic.alerts.arn]
}

resource "aws_cloudwatch_log_group" "flow_logs" {
  name              = "/aws/vpc/${var.project_name}"
  retention_in_days = 30
}
```

### `outputs.tf`

```hcl
output "rds_endpoint" {
  value = aws_db_instance.this.endpoint
}

output "s3_bucket" {
  value = aws_s3_bucket.this.id
}

output "sns_topic_arn" {
  value = aws_sns_topic.alerts.arn
}
```

</details>

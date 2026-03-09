# 88. AWS DataSync Online Transfer

<!--
difficulty: intermediate
concepts: [datasync, datasync-agent, transfer-task, location, bandwidth-throttling, scheduling, s3-sync, transfer-family, online-migration, nfs, smb]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: apply, analyze
prerequisites: [87-snow-family-data-transfer]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** DataSync charges $0.0125/GB transferred. This exercise uses minimal test data (~1 MB), so DataSync costs are negligible. S3 bucket storage is ~$0.01/hr. An actual DataSync agent on-premises requires a VMware/Hyper-V/KVM VM -- this exercise uses an AWS-to-AWS transfer that does not require an agent. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 87 (Snow Family) or equivalent knowledge of data migration concepts
- Understanding of S3 bucket operations and IAM roles
- Familiarity with NFS/SMB protocols (conceptual understanding only)

## Learning Objectives

After completing this exercise, you will be able to:

1. **Implement** an AWS DataSync task that transfers data between AWS storage services with scheduling and filtering
2. **Analyze** when DataSync requires an agent (on-premises sources) versus agentless mode (AWS-to-AWS transfers)
3. **Evaluate** DataSync against alternative transfer methods (S3 sync, Transfer Family, Snow Family) for specific scenarios
4. **Apply** bandwidth throttling to prevent DataSync from saturating a production network link
5. **Design** a recurring DataSync schedule for ongoing data synchronization between on-premises and AWS

## Why This Matters

DataSync appears on the SAA-C03 exam as the go-to answer for online data transfer scenarios involving large datasets. The exam tests whether you understand the agent model: on-premises sources (NFS, SMB, HDFS, self-managed object storage) require a DataSync agent deployed as a VM in your data center, while AWS-to-AWS transfers (S3 to S3, S3 to EFS, EFS to FSx) are agentless. This distinction is a common trap -- the exam describes an on-premises NFS server migrating to EFS and asks which components are needed. Without the agent, DataSync cannot read from the on-premises source.

The architectural value of DataSync over raw `aws s3 sync` is significant. DataSync uses a purpose-built protocol with parallel transfers, automatic retries, integrity validation, bandwidth throttling, and scheduling -- all managed by AWS. A 10 TB transfer that would take days with `aws s3 sync` over a single TCP stream completes in hours with DataSync's parallelized approach. DataSync also handles incremental transfers efficiently, transferring only changed files on subsequent runs.

The exam also tests DataSync vs Transfer Family. Transfer Family (SFTP/FTPS/FTP servers backed by S3 or EFS) is for third-party integration where external partners upload files using existing protocols. DataSync is for migration and ongoing replication that you control. These solve different problems, and the exam expects you to distinguish between them.

## Step 1 -- Create Source and Destination Storage

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
  default     = "saa-ex88"
}
```

### `storage.tf`

```hcl
resource "random_id" "suffix" { byte_length = 4 }

# Source bucket (simulates on-premises data staged in S3)
resource "aws_s3_bucket" "source" {
  bucket = "${var.project_name}-source-${random_id.suffix.hex}", force_destroy = true
  tags = { Name = "${var.project_name}-source" }
}

resource "aws_s3_bucket_versioning" "source" {
  bucket = aws_s3_bucket.source.id
  versioning_configuration { status = "Enabled" }
}

resource "aws_s3_bucket_public_access_block" "source" {
  bucket = aws_s3_bucket.source.id
  block_public_acls = true, block_public_policy = true
  ignore_public_acls = true, restrict_public_buckets = true
}

# Destination bucket
resource "aws_s3_bucket" "destination" {
  bucket = "${var.project_name}-destination-${random_id.suffix.hex}", force_destroy = true
  tags = { Name = "${var.project_name}-destination" }
}

resource "aws_s3_bucket_versioning" "destination" {
  bucket = aws_s3_bucket.destination.id
  versioning_configuration { status = "Enabled" }
}

resource "aws_s3_bucket_public_access_block" "destination" {
  bucket = aws_s3_bucket.destination.id
  block_public_acls = true, block_public_policy = true
  ignore_public_acls = true, restrict_public_buckets = true
}
```

## Step 2 -- Create the DataSync IAM Role

### `iam.tf`

```hcl
data "aws_iam_policy_document" "datasync_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals { type = "Service", identifiers = ["datasync.amazonaws.com"] }
  }
}

resource "aws_iam_role" "datasync" {
  name               = "${var.project_name}-datasync-role"
  assume_role_policy = data.aws_iam_policy_document.datasync_assume.json
}

data "aws_iam_policy_document" "datasync_s3" {
  statement {
    actions   = ["s3:GetBucketLocation", "s3:ListBucket", "s3:ListBucketMultipartUploads"]
    resources = [aws_s3_bucket.source.arn, aws_s3_bucket.destination.arn]
  }
  statement {
    actions   = ["s3:AbortMultipartUpload", "s3:DeleteObject", "s3:GetObject", "s3:GetObjectTagging", "s3:GetObjectVersion", "s3:GetObjectVersionTagging", "s3:ListMultipartUploadParts", "s3:PutObject", "s3:PutObjectTagging"]
    resources = ["${aws_s3_bucket.source.arn}/*", "${aws_s3_bucket.destination.arn}/*"]
  }
}

resource "aws_iam_role_policy" "datasync_s3" {
  name = "s3-access", role = aws_iam_role.datasync.id
  policy = data.aws_iam_policy_document.datasync_s3.json
}
```

## Step 3 -- Configure DataSync Locations and Task

### TODO 1: Create DataSync Source Location

### `datasync.tf`

```hcl
# A location defines where DataSync reads from or writes to.
# S3 locations need bucket ARN + IAM role. On-premises NFS/SMB need agent ARN.
resource "aws_datasync_location_s3" "source" {
  # TODO: s3_bucket_arn, subdirectory="/", s3_config { bucket_access_role_arn }
  tags = { Name = "${var.project_name}-source-location" }
}
```

### TODO 2: Create DataSync Destination Location

```hcl
resource "aws_datasync_location_s3" "destination" {
  # TODO: s3_bucket_arn, subdirectory="/migrated", s3_config { bucket_access_role_arn }
  # Optional: s3_storage_class = "STANDARD" (or STANDARD_IA, GLACIER, etc.)
  tags = { Name = "${var.project_name}-destination-location" }
}
```

### TODO 3: Create the DataSync Task

```hcl
resource "aws_datasync_task" "this" {
  # TODO: source_location_arn, destination_location_arn, name="${var.project_name}-transfer-task"
  # options: verify_mode="ONLY_FILES_TRANSFERRED", overwrite_mode="ALWAYS",
  #          transfer_mode="CHANGED", bytes_per_second=10485760 (10 MB/s throttle)
  # schedule: schedule_expression="cron(0 2 * * ? *)" (daily 2 AM UTC)
  tags = { Name = "${var.project_name}-transfer-task" }
}
```

<details>
<summary>datasync.tf -- Solution: Complete DataSync Configuration</summary>

```hcl
resource "aws_datasync_location_s3" "source" {
  s3_bucket_arn = aws_s3_bucket.source.arn
  subdirectory  = "/"

  s3_config {
    bucket_access_role_arn = aws_iam_role.datasync.arn
  }

  tags = { Name = "${var.project_name}-source-location" }
}

resource "aws_datasync_location_s3" "destination" {
  s3_bucket_arn    = aws_s3_bucket.destination.arn
  subdirectory     = "/migrated"
  s3_storage_class = "STANDARD"

  s3_config {
    bucket_access_role_arn = aws_iam_role.datasync.arn
  }

  tags = { Name = "${var.project_name}-destination-location" }
}

resource "aws_datasync_task" "this" {
  source_location_arn      = aws_datasync_location_s3.source.arn
  destination_location_arn = aws_datasync_location_s3.destination.arn
  name                     = "${var.project_name}-transfer-task"

  options {
    verify_mode    = "ONLY_FILES_TRANSFERRED"
    overwrite_mode = "ALWAYS"
    transfer_mode  = "CHANGED"
    bytes_per_second = 10485760  # 10 MB/s throttle
  }

  schedule {
    schedule_expression = "cron(0 2 * * ? *)"
  }

  tags = { Name = "${var.project_name}-transfer-task" }
}
```

</details>

## Step 4 -- Add Outputs and Apply

### `outputs.tf`

```hcl
output "source_bucket" {
  value = aws_s3_bucket.source.id
}

output "destination_bucket" {
  value = aws_s3_bucket.destination.id
}

output "datasync_task_arn" {
  value = aws_datasync_task.this.arn
}
```

```bash
terraform init
terraform apply -auto-approve
```

## Step 5 -- Execute and Monitor the Transfer

```bash
SOURCE=$(terraform output -raw source_bucket)
TASK_ARN=$(terraform output -raw datasync_task_arn)

# Upload test data to the source bucket
for i in $(seq 1 10); do
  echo "Test file $i content - $(date)" | \
    aws s3 cp - "s3://$SOURCE/data/file-$i.txt"
done

# Start a manual task execution (instead of waiting for the schedule)
EXECUTION_ARN=$(aws datasync start-task-execution \
  --task-arn "$TASK_ARN" \
  --query 'TaskExecutionArn' --output text)

echo "Task execution started: $EXECUTION_ARN"

# Monitor the execution
aws datasync describe-task-execution \
  --task-execution-arn "$EXECUTION_ARN" \
  --query '{Status:Status,FilesTransferred:FilesTransferred,BytesTransferred:BytesTransferred}' \
  --output table
```

Wait a moment, then check again:

```bash
# Poll until complete
aws datasync describe-task-execution \
  --task-execution-arn "$EXECUTION_ARN" \
  --query '{Status:Status,FilesTransferred:FilesTransferred,BytesTransferred:BytesTransferred,BytesVerified:Result.VerifyStatus}' \
  --output table
```

```bash
# Verify data arrived at the destination
DEST=$(terraform output -raw destination_bucket)
aws s3 ls "s3://$DEST/migrated/data/" --recursive
```

## DataSync Transfer Method Comparison

### TODO 4: Complete the Comparison Table

```
# TODO: Fill in the missing cells (marked ???)
#
# +-------------------+------------+-----------+------------+------------+
# | Feature           | DataSync   | S3 Sync   | Transfer   | Snow       |
# |                   |            | (CLI)     | Family     | Family     |
# +-------------------+------------+-----------+------------+------------+
# | Transfer type     | Online     | Online    | Online     | ???        |
# | Protocol          | Custom     | HTTPS     | ???        | Physical   |
# | Agent required    | ???        | No        | No         | No         |
# | Bandwidth control | Yes        | No        | ???        | N/A        |
# | Scheduling        | Built-in   | ???       | No         | N/A        |
# | Integrity check   | Automatic  | ???       | No         | Automatic  |
# | Incremental       | Yes        | Yes       | No         | ???        |
# | Max throughput     | 10 Gbps   | ???       | Varies     | Device I/O |
# | Pricing model     | Per GB     | ???       | Per protocol-hr| Per job |
# | Best for          | Migration  | ???       | ???        | > 10 TB    |
# +-------------------+------------+-----------+------------+------------+
```

<details>
<summary>Solution: Comparison Table</summary>

| Feature | DataSync | S3 Sync (CLI) | Transfer Family | Snow Family |
|---|---|---|---|---|
| **Transfer type** | Online | Online | Online | Offline |
| **Protocol** | Custom (optimized) | HTTPS (single stream) | SFTP/FTPS/FTP/AS2 | Physical device |
| **Agent required** | Yes (on-prem sources) | No | No | No |
| **Bandwidth control** | Yes (bytes/sec) | No | No (protocol-limited) | N/A |
| **Scheduling** | Built-in cron | External (cron/EventBridge) | No (push-based) | N/A |
| **Integrity check** | Automatic checksums | No (manual md5) | No | Automatic checksums |
| **Incremental** | Yes (changed files only) | Yes (size/timestamp) | No (file-at-a-time) | No (full device) |
| **Max throughput** | 10 Gbps per task | ~100-300 MB/s (single stream) | Varies by protocol | Device I/O speed |
| **Pricing model** | $0.0125/GB transferred | Free (data transfer charges only) | $0.30/hr per protocol | $300/job + shipping |
| **Best for** | Bulk migration, ongoing sync | Small ad-hoc transfers | Third-party file exchange | Datasets > 10 TB offline |

</details>

## Spot the Bug

A solutions architect designs a DataSync migration from an on-premises NFS file server to Amazon EFS:

```hcl
# DataSync task to migrate from on-premises NFS to EFS
resource "aws_datasync_location_nfs" "on_prem" {
  server_hostname = "10.0.1.50"
  subdirectory    = "/exports/data"

  on_prem_config {
    agent_arns = []  # No agent specified
  }
}

resource "aws_datasync_location_efs" "target" {
  efs_file_system_arn = aws_efs_file_system.this.arn

  ec2_config {
    security_group_arns = [aws_security_group.efs.arn]
    subnet_arn          = data.aws_subnet.private.arn
  }
}

resource "aws_datasync_task" "migrate" {
  source_location_arn      = aws_datasync_location_nfs.on_prem.arn
  destination_location_arn = aws_datasync_location_efs.target.arn
  name                     = "nfs-to-efs-migration"

  options {
    verify_mode = "POINT_IN_TIME_CONSISTENT"
  }
}
```

<details>
<summary>Explain the bug</summary>

**Bug: The NFS location has `agent_arns = []` (empty) -- DataSync requires an agent for on-premises sources.**

DataSync agents are deployed as VMs (VMware/KVM/Hyper-V/EC2) in the on-premises environment, bridging on-premises storage to AWS. Without an agent, DataSync cannot reach `10.0.1.50`.

**Fix:** Deploy an agent and reference its ARN in `on_prem_config { agent_arns = [aws_datasync_agent.on_prem.arn] }`.

**Key rule:** On-premises sources (NFS, SMB, HDFS, self-managed object storage) always require an agent. AWS-to-AWS transfers (S3 to S3, S3 to EFS, EFS to FSx) are agentless. The agent VM needs NFS access (port 2049), HTTPS to AWS (443), and minimum 4 vCPU/32 GB RAM for production.

</details>

## Verify What You Learned

```bash
SOURCE=$(terraform output -raw source_bucket)
DEST=$(terraform output -raw destination_bucket)
TASK_ARN=$(terraform output -raw datasync_task_arn)

# Verify source files exist
aws s3 ls "s3://$SOURCE/data/" --recursive | wc -l
```

Expected: `10` (the test files uploaded in Step 5).

```bash
# Verify DataSync task configuration
aws datasync describe-task --task-arn "$TASK_ARN" \
  --query '{Name:Name,Status:Status,SourceArn:SourceLocationArn,Schedule:Schedule}' \
  --output json
```

Expected: Task with `AVAILABLE` status and a schedule of `cron(0 2 * * ? *)`.

```bash
# Verify destination has the transferred files
aws s3 ls "s3://$DEST/migrated/data/" --recursive | wc -l
```

Expected: `10` (matching the source files after successful transfer).

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

Exercise 89 covers **CloudWatch Dashboards and Custom Metrics**, where you will build operational visibility dashboards with widgets for EC2, RDS, and ALB metrics, and publish custom application metrics using the PutMetricData API -- transitioning from data migration topics to the monitoring and observability domain.

## Summary

- **AWS DataSync** is a managed online data transfer service optimized for large-scale migrations and ongoing replication
- **Agent-based transfers** are required for on-premises sources (NFS, SMB, HDFS) -- the agent VM bridges on-premises storage to AWS
- **Agentless transfers** work for AWS-to-AWS moves (S3 to S3, S3 to EFS, EFS to FSx for Lustre)
- **Bandwidth throttling** (`bytes_per_second`) prevents DataSync from saturating production network links
- **Scheduling** via built-in cron expressions enables recurring synchronization without external orchestration
- **Incremental transfers** (`transfer_mode = "CHANGED"`) only move modified files, reducing time and cost on subsequent runs
- **Integrity verification** (`verify_mode`) ensures data consistency between source and destination automatically
- **DataSync vs S3 sync**: DataSync provides parallelized transfer, scheduling, throttling, and verification -- use `aws s3 sync` only for small ad-hoc transfers
- **DataSync vs Transfer Family**: DataSync is for migration/replication you control; Transfer Family is for external partners uploading via SFTP/FTPS/FTP
- **Pricing**: $0.0125 per GB transferred -- factor this into cost analysis for large migrations

## Reference

- [AWS DataSync Overview](https://docs.aws.amazon.com/datasync/latest/userguide/what-is-datasync.html)
- [DataSync Agent Requirements](https://docs.aws.amazon.com/datasync/latest/userguide/agent-requirements.html)
- [Terraform aws_datasync_task](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/datasync_task)
- [DataSync Transfer Performance](https://docs.aws.amazon.com/datasync/latest/userguide/how-datasync-transfer-works.html)

## Additional Resources

- [DataSync vs Other Transfer Services](https://docs.aws.amazon.com/datasync/latest/userguide/how-datasync-works.html) -- choosing between DataSync, Transfer Family, Snow Family, and S3 Transfer Acceleration
- [DataSync Filtering](https://docs.aws.amazon.com/datasync/latest/userguide/filtering.html) -- include/exclude filters to transfer only specific files or directories
- [DataSync with VPC Endpoints](https://docs.aws.amazon.com/datasync/latest/userguide/datasync-in-vpc.html) -- keeping DataSync traffic within your VPC via PrivateLink
- [Monitoring DataSync with CloudWatch](https://docs.aws.amazon.com/datasync/latest/userguide/monitor-datasync.html) -- metrics for transfer throughput, files transferred, and task duration

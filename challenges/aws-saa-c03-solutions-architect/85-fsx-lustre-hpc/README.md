# 85. FSx for Lustre HPC

<!--
difficulty: intermediate
concepts: [fsx-lustre, hpc, scratch-file-system, persistent-file-system, s3-data-repository, lazy-loading, write-back, data-compression, lustre-client, iops, throughput, deployment-types]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: apply, analyze
prerequisites: [83-efs-vs-fsx-decision-framework]
aws_cost: ~$0.10/hr
-->

> **AWS Cost Warning:** FSx for Lustre Scratch: $0.140/GB-month. Persistent SSD: $0.145/GB-month. Minimum 1.2 TB = ~$0.24/hr for scratch. EC2 for benchmarking (c5.xlarge): ~$0.17/hr. Total ~$0.10-$0.40/hr depending on configuration. Remember to run `terraform destroy` immediately when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Completed exercise 83 (EFS vs FSx) | Understanding of file system types |
| Default VPC with subnets | `aws ec2 describe-subnets --filters Name=default-for-az,Values=true --query 'Subnets[0].SubnetId'` |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Implement** FSx for Lustre file systems with S3 data repository association using Terraform.
2. **Analyze** the trade-offs between scratch (temporary, highest performance) and persistent (durable, replicated) file systems.
3. **Apply** S3 data repository associations for transparent lazy loading and write-back.
4. **Evaluate** deployment types and storage capacity increments based on throughput requirements.
5. **Design** HPC and ML training architectures that combine S3 data lakes with high-performance file access.

---

## Why This Matters

FSx for Lustre is the SAA-C03 answer to "the workload requires high-performance file system access with hundreds of GB/s throughput." The exam tests whether you choose Lustre when the scenario mentions HPC, machine learning training, media processing, financial simulation, or genomics analysis. The key differentiator from EFS and FSx for Windows: Lustre provides sub-millisecond latency with throughput that scales linearly with storage capacity, reaching hundreds of GB/s for large file systems.

The exam's most important Lustre concept is the S3 data repository association. FSx for Lustre can be linked to an S3 bucket, making S3 objects transparently available as files in the Lustre file system. When a compute job first reads a file, Lustre lazily loads it from S3 (the first read is S3 speed, subsequent reads are Lustre speed). When the job writes results, Lustre can write them back to S3. This pattern is critical for ML training: training data lives in S3 (cheap, durable), Lustre provides high-speed access during training, and results are written back to S3 for long-term storage.

The second critical exam concept is scratch versus persistent. Scratch file systems provide the highest burst throughput (200 MB/s per TB), but data is not replicated -- if the underlying storage fails, data is lost. Scratch is for temporary processing where the source data is in S3 and can be reloaded. Persistent file systems replicate data within the same AZ and support automatic backups. The exam tests this: "the data is not stored elsewhere and must not be lost" = persistent; "the data is sourced from S3 and can be regenerated" = scratch.

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
  default     = "saa-ex85"
}
```

### `main.tf`

```hcl
data "aws_vpc" "default" {
  default = true
}

data "aws_subnets" "default" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.default.id]
  }
  filter {
    name   = "default-for-az"
    values = ["true"]
  }
}

resource "random_id" "suffix" {
  byte_length = 4
}


# ============================================================
# TODO 1: S3 Bucket (Data Repository)
# ============================================================
# Create an S3 bucket to serve as the data repository
# for the Lustre file system.
#
# Requirements:
#   - Bucket name: "${var.project_name}-data-${random_id.suffix.hex}"
#   - force_destroy = true
#   - Upload test data files:
#     - "datasets/training/data-001.csv" (sample CSV)
#     - "datasets/training/data-002.csv" (sample CSV)
#     - "datasets/models/" (empty prefix)
#
# The Lustre file system will lazily load these files
# when first accessed. Results can be written back.
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket
# ============================================================


# ============================================================
# TODO 2: FSx for Lustre File System (Scratch)
# ============================================================
# Create a scratch FSx for Lustre file system.
#
# Requirements:
#   - Resource: aws_fsx_lustre_file_system
#   - storage_capacity = 1200 (minimum for SCRATCH_2, in GB)
#   - deployment_type = "SCRATCH_2"
#     (SCRATCH_1, SCRATCH_2, PERSISTENT_1, PERSISTENT_2)
#   - subnet_ids = [single subnet]
#   - security_group_ids = [Lustre security group]
#   - data_compression_type = "LZ4" (optional, saves storage)
#
# Deployment types:
#   - SCRATCH_1: baseline 200 MB/s per TB (legacy)
#   - SCRATCH_2: baseline 200 MB/s per TB, better burst,
#     encryption in transit
#   - PERSISTENT_1: baseline 50/100/200 MB/s per TB,
#     replicated within AZ
#   - PERSISTENT_2: baseline 125/250/500/1000 MB/s per TB,
#     SSD only
#
# Storage capacity increments:
#   - SCRATCH_2: 1.2 TB, then increments of 2.4 TB
#   - PERSISTENT_1 SSD: 1.2 TB, then increments of 2.4 TB
#   - PERSISTENT_1 HDD: 6 TB, then increments of 6 TB
#   - PERSISTENT_2: 1.2 TB, then increments of 2.4 TB
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/fsx_lustre_file_system
# ============================================================


# ============================================================
# TODO 3: S3 Data Repository Association
# ============================================================
# Link the Lustre file system to the S3 bucket for
# transparent data access.
#
# Requirements:
#   - Resource: aws_fsx_data_repository_association
#   - file_system_id = Lustre file system ID
#   - data_repository_path = "s3://${bucket_name}"
#   - file_system_path = "/data"
#   - s3 block:
#     - auto_import_policy:
#       - events = ["NEW", "CHANGED", "DELETED"]
#         (sync S3 changes to Lustre metadata)
#     - auto_export_policy:
#       - events = ["NEW", "CHANGED", "DELETED"]
#         (sync Lustre changes back to S3)
#
# How lazy loading works:
#   1. Lustre sees the S3 object metadata immediately
#   2. When a process reads the file, Lustre loads the data
#      from S3 on first access
#   3. Subsequent reads serve from Lustre (sub-ms latency)
#   4. New/modified files on Lustre are exported to S3
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/fsx_data_repository_association
# ============================================================


# ============================================================
# TODO 4: Benchmark with IOR (CLI)
# ============================================================
# After terraform apply, mount the Lustre file system from
# an EC2 instance and benchmark:
#
#   a) Install Lustre client (Amazon Linux 2023):
#      sudo dnf install -y lustre-client
#
#   b) Mount:
#      sudo mkdir -p /mnt/lustre
#      LUSTRE_DNS=$(terraform output -raw lustre_dns_name)
#      LUSTRE_MOUNT=$(terraform output -raw lustre_mount_name)
#      sudo mount -t lustre $LUSTRE_DNS@tcp:/$LUSTRE_MOUNT /mnt/lustre
#
#   c) Verify S3 data is visible:
#      ls /mnt/lustre/data/datasets/training/
#      # Files appear as metadata-only stubs (0 blocks allocated)
#
#   d) Read file (triggers lazy load from S3):
#      time cat /mnt/lustre/data/datasets/training/data-001.csv
#      # First read: S3 speed. Second read: Lustre speed.
#      time cat /mnt/lustre/data/datasets/training/data-001.csv
#
#   e) Write results back to S3:
#      echo "model output" > /mnt/lustre/data/datasets/models/result.txt
#      # Auto-exported to S3 based on export policy
# ============================================================
```

### `security.tf`

```hcl
resource "aws_security_group" "lustre" {
  name   = "${var.project_name}-lustre-sg"
  vpc_id = data.aws_vpc.default.id

  ingress {
    from_port   = 988
    to_port     = 988
    protocol    = "tcp"
    cidr_blocks = [data.aws_vpc.default.cidr_block]
    description = "Lustre"
  }

  ingress {
    from_port   = 1021
    to_port     = 1023
    protocol    = "tcp"
    cidr_blocks = [data.aws_vpc.default.cidr_block]
    description = "Lustre"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}
```

### `outputs.tf`

```hcl
output "lustre_dns_name" {
  value = "Set after TODO 2 implementation"
}

output "lustre_mount_name" {
  value = "Set after TODO 2 implementation"
}

output "s3_bucket" {
  value = "Set after TODO 1 implementation"
}
```

---

## Scratch vs Persistent

| Feature | Scratch | Persistent |
|---|---|---|
| **Data durability** | Not replicated (data lost on failure) | Replicated within AZ |
| **Backups** | Not supported | Automatic daily backups |
| **Throughput** | 200 MB/s per TB (SCRATCH_2) | 50-1000 MB/s per TB (configurable) |
| **Encryption in transit** | SCRATCH_2 only | Yes |
| **Cost** | $0.140/GB-month | $0.145-$0.600/GB-month |
| **Use case** | Temporary processing, data in S3 | Long-running workloads, primary storage |
| **Data loss risk** | Yes (if hardware fails) | No (replicated) |

### Key Decision

- **Source data in S3?** Use scratch -- data can be reloaded from S3 if lost
- **Source data only on Lustre?** Use persistent -- data must not be lost
- **Highest throughput needed?** SCRATCH_2 at 200 MB/s per TB is baseline
- **Long-running workload (days/weeks)?** Persistent -- scratch failures would require restart

### Throughput Scaling

Lustre throughput scales linearly with storage capacity:

| Storage | SCRATCH_2 (200 MB/s per TB) | PERSISTENT_2 (250 MB/s per TB) |
|---|---|---|
| 1.2 TB | 240 MB/s | 300 MB/s |
| 4.8 TB | 960 MB/s | 1,200 MB/s |
| 12 TB | 2,400 MB/s | 3,000 MB/s |
| 48 TB | 9,600 MB/s | 12,000 MB/s |

---

## Spot the Bug

The following architecture uses FSx for Lustre for a production genomics analysis pipeline. The source data exists only on the Lustre file system (not in S3). Identify the critical flaw.

```hcl
resource "aws_fsx_lustre_file_system" "genomics" {
  storage_capacity    = 4800
  deployment_type     = "SCRATCH_2"
  subnet_ids          = [data.aws_subnets.default.ids[0]]
  security_group_ids  = [aws_security_group.lustre.id]

  tags = { Name = "genomics-pipeline", Environment = "production" }
}
```

Genomics data is uploaded directly to the Lustre file system by sequencing machines. The analysis pipeline reads from Lustre, processes the data, and writes results back to Lustre. Analysis runs take 3-7 days.

<details>
<summary>Explain the bug</summary>

**A scratch file system is used for data that exists only on Lustre (not backed by S3).** Scratch file systems do not replicate data. If the underlying storage hardware fails during the 3-7 day analysis run, all data is lost -- both the raw sequencing data and any partial results. The pipeline must restart from scratch, and the raw data must be re-sequenced (which may take weeks and cost thousands of dollars).

This violates the fundamental rule for scratch vs persistent: scratch is only appropriate when the data can be regenerated from another source (like S3). When Lustre is the primary data store, you must use persistent.

**Fix:**

```hcl
resource "aws_fsx_lustre_file_system" "genomics" {
  storage_capacity           = 4800
  deployment_type            = "PERSISTENT_2"
  per_unit_storage_throughput = 250          # MB/s per TB
  subnet_ids                 = [data.aws_subnets.default.ids[0]]
  security_group_ids         = [aws_security_group.lustre.id]

  automatic_backup_retention_days = 7       # Daily backups retained 7 days

  tags = { Name = "genomics-pipeline", Environment = "production" }
}
```

Changes:
1. `PERSISTENT_2` replicates data within the AZ for hardware failure protection
2. `per_unit_storage_throughput` configures throughput per TB (PERSISTENT_2: 125/250/500/1000)
3. `automatic_backup_retention_days` enables daily backups for disaster recovery
4. For additional protection, add an S3 data repository association to export results

**Better architecture:** Use S3 as the durable data store, with Lustre as the high-speed processing tier:
- Sequencers upload to S3 (durable)
- Lustre loads from S3 (fast processing)
- Results export back to S3 (durable)
- Scratch is now safe because S3 is the source of truth

</details>

---

## Verify What You Learned

1. **Deploy the infrastructure:**
   ```bash
   terraform init && terraform apply -auto-approve
   ```
   Note: Lustre file system creation takes 10-15 minutes.

2. **Verify the Lustre file system:**
   ```bash
   aws fsx describe-file-systems \
     --query 'FileSystems[?Tags[?Key==`Name`&&Value==`saa-ex85-lustre`]].{Id:FileSystemId,Status:Lifecycle,Type:FileSystemType,Storage:StorageCapacity,Deployment:LustreConfiguration.DeploymentType,Compression:LustreConfiguration.DataCompressionType}' \
     --output table
   ```
   Expected: Lifecycle `AVAILABLE`, DeploymentType `SCRATCH_2`.

3. **Verify data repository association:**
   ```bash
   FSX_ID=$(aws fsx describe-file-systems \
     --query 'FileSystems[?Tags[?Key==`Name`&&Value==`saa-ex85-lustre`]].FileSystemId' --output text)
   aws fsx describe-data-repository-associations \
     --query "DataRepositoryAssociations[?FileSystemId=='$FSX_ID'].{S3Path:DataRepositoryPath,LustrePath:FileSystemPath,ImportEvents:S3.AutoImportPolicy.Events,ExportEvents:S3.AutoExportPolicy.Events}" \
     --output table
   ```
   Expected: S3 path linked to Lustre path `/data` with import/export events.

4. **Verify S3 test data exists:**
   ```bash
   BUCKET=$(terraform output -raw s3_bucket)
   aws s3 ls "s3://${BUCKET}/datasets/training/" --recursive
   ```
   Expected: Test CSV files in the datasets/training/ prefix.

5. **Terraform state consistency:**
   ```bash
   terraform plan
   ```
   Expected: `No changes. Your infrastructure matches the configuration.`

---

## Solutions

<details>
<summary>storage.tf -- TODO 1: S3 Data Repository</summary>

```hcl
resource "aws_s3_bucket" "data" {
  bucket        = "${var.project_name}-data-${random_id.suffix.hex}"
  force_destroy = true
  tags          = { Name = "${var.project_name}-data" }
}

resource "aws_s3_object" "training_data_1" {
  bucket  = aws_s3_bucket.data.id
  key     = "datasets/training/data-001.csv"
  content = "id,feature1,feature2,label\n1,0.5,1.2,positive\n2,0.3,0.8,negative\n3,0.9,1.5,positive"
}

resource "aws_s3_object" "training_data_2" {
  bucket  = aws_s3_bucket.data.id
  key     = "datasets/training/data-002.csv"
  content = "id,feature1,feature2,label\n4,0.1,0.4,negative\n5,0.7,1.1,positive\n6,0.2,0.6,negative"
}
```

</details>

<details>
<summary>storage.tf -- TODO 2: FSx for Lustre (Scratch)</summary>

```hcl
resource "aws_fsx_lustre_file_system" "this" {
  storage_capacity      = 1200
  deployment_type       = "SCRATCH_2"
  subnet_ids            = [data.aws_subnets.default.ids[0]]
  security_group_ids    = [aws_security_group.lustre.id]
  data_compression_type = "LZ4"

  tags = { Name = "${var.project_name}-lustre" }
}
```

`data_compression_type = "LZ4"` compresses data automatically, reducing the effective storage capacity needed. LZ4 is fast enough that compression/decompression does not measurably impact throughput.

</details>

<details>
<summary>storage.tf -- TODO 3: S3 Data Repository Association</summary>

```hcl
resource "aws_fsx_data_repository_association" "this" {
  file_system_id       = aws_fsx_lustre_file_system.this.id
  data_repository_path = "s3://${aws_s3_bucket.data.id}"
  file_system_path     = "/data"

  s3 {
    auto_import_policy {
      events = ["NEW", "CHANGED", "DELETED"]
    }

    auto_export_policy {
      events = ["NEW", "CHANGED", "DELETED"]
    }
  }

  tags = { Name = "${var.project_name}-dra" }
}
```

The data repository association creates a bidirectional link:
- **Import (S3 -> Lustre):** S3 objects appear as files in `/data/`. First read triggers lazy loading from S3. Auto-import updates Lustre metadata when S3 objects change.
- **Export (Lustre -> S3):** Files created or modified in `/data/` are exported to S3. Auto-export happens asynchronously.

</details>

<details>
<summary>outputs.tf -- Updated Outputs</summary>

```hcl
output "lustre_dns_name" {
  value = aws_fsx_lustre_file_system.this.dns_name
}

output "lustre_mount_name" {
  value = aws_fsx_lustre_file_system.this.mount_name
}

output "s3_bucket" {
  value = aws_s3_bucket.data.id
}
```

</details>

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Note: Lustre deletion takes 5-10 minutes. Verify:

```bash
aws fsx describe-file-systems \
  --query 'FileSystems[?Tags[?Key==`Name`&&Value==`saa-ex85-lustre`]].Lifecycle'
```

Expected: Empty or `DELETING`.

---

## What's Next

Exercise 86 covers **AWS Backup for centralized backup management**, where you will create backup plans with lifecycle rules, configure backup vault locks for compliance (governance vs compliance mode), and set up cross-region backup copies. AWS Backup provides a unified backup service that works across EFS, FSx, EBS, DynamoDB, RDS, and other AWS services.

---

## Summary

- **FSx for Lustre** provides sub-millisecond latency and hundreds of GB/s throughput for compute-intensive workloads
- **Scratch file systems** offer highest throughput but no data replication -- use only when data can be regenerated from S3
- **Persistent file systems** replicate data within AZ and support automatic backups -- use when Lustre is the primary data store
- **S3 data repository association** enables transparent lazy loading from S3 and write-back to S3
- **Lazy loading**: first read is S3 speed, subsequent reads are Lustre speed (sub-ms latency)
- **Throughput scales linearly** with storage capacity: 200 MB/s per TB for SCRATCH_2
- **Storage increments**: SCRATCH_2 starts at 1.2 TB, then 2.4 TB increments; HDD starts at 6 TB, then 6 TB increments
- **LZ4 compression** reduces effective storage usage with negligible performance impact
- **Lustre client** must be installed on EC2 instances (`lustre-client` package on Amazon Linux)
- **Port 988** is the Lustre network port (unlike NFS port 2049 or SMB port 445)
- **Single-subnet deployment** -- Lustre does not support Multi-AZ (use persistent + backups for durability)

## Reference

- [FSx for Lustre User Guide](https://docs.aws.amazon.com/fsx/latest/LustreGuide/what-is.html)
- [Data Repository Associations](https://docs.aws.amazon.com/fsx/latest/LustreGuide/fsx-data-repositories.html)
- [Terraform aws_fsx_lustre_file_system](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/fsx_lustre_file_system)
- [FSx for Lustre Pricing](https://aws.amazon.com/fsx/lustre/pricing/)

## Additional Resources

- [Lustre Performance](https://docs.aws.amazon.com/fsx/latest/LustreGuide/performance.html) -- throughput, IOPS, and latency by deployment type
- [Mounting Lustre File Systems](https://docs.aws.amazon.com/fsx/latest/LustreGuide/mounting-on-ec2-instances.html) -- client installation and mount commands
- [Lustre with SageMaker](https://docs.aws.amazon.com/sagemaker/latest/dg/model-access-training-data.html) -- using Lustre for ML training data
- [Data Compression](https://docs.aws.amazon.com/fsx/latest/LustreGuide/data-compression.html) -- LZ4 compression configuration and performance impact

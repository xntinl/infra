# 83. EFS vs FSx Decision Framework

<!--
difficulty: basic
concepts: [efs, fsx-windows, fsx-lustre, fsx-ontap, nfs, smb, posix-permissions, active-directory, hpc, s3-data-repository, multi-protocol, performance-modes, throughput-modes, storage-classes]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: understand, apply
prerequisites: [17-vpc-subnets-route-tables-igw]
aws_cost: ~$0.05/hr
-->

> **AWS Cost Warning:** EFS Standard: $0.30/GB-month (~$0.0004/GB-hr). Minimum practical cost ~$0.05/hr with mount targets. EFS Infrequent Access: $0.016/GB-month. Remember to run `terraform destroy` immediately when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Default VPC with subnets in at least 2 AZs
- Basic understanding of file system protocols (NFS, SMB)

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** the four AWS managed file system options: EFS, FSx for Windows, FSx for Lustre, and FSx for NetApp ONTAP
- **Identify** which file system to use based on protocol (NFS vs SMB), OS (Linux vs Windows), and workload (general purpose vs HPC)
- **Construct** an EFS file system with mount targets, security groups, and lifecycle policies using Terraform
- **Compare** EFS performance modes (General Purpose vs Max I/O) and throughput modes (Bursting vs Provisioned vs Elastic)
- **Calculate** EFS cost with storage classes (Standard, Infrequent Access, Archive) and lifecycle transitions
- **Describe** how EFS integrates with EC2, ECS, Lambda, and cross-AZ access patterns

## Why AWS File Systems Matter

File system selection is a heavily tested SAA-C03 topic because the exam presents scenarios with specific protocol, OS, and performance requirements, then asks you to choose the right service. The four options are distinct and non-overlapping in their primary use cases:

**EFS (Elastic File System)** is a managed NFS file system for Linux workloads. It is elastic (grows and shrinks automatically), supports thousands of concurrent NFS clients, and works across multiple AZs. Use EFS when you need a shared POSIX file system for Linux EC2 instances, ECS containers, or Lambda functions.

**FSx for Windows File Server** is a managed Windows file system with SMB protocol and native Active Directory integration. Use when Windows applications need shared file storage with NTFS permissions, DFS namespaces, or SMB-specific features.

**FSx for Lustre** is a high-performance file system for compute-intensive workloads. It provides sub-millisecond latency and hundreds of GB/s throughput. Its killer feature is transparent S3 integration -- it can lazily load data from S3 and write results back. Use for HPC, machine learning training, media processing, and financial modeling.

**FSx for NetApp ONTAP** is a multi-protocol file system (NFS, SMB, and iSCSI) with NetApp features: snapshots, cloning, data tiering, compression, deduplication. Use when you need both NFS and SMB from the same file system or when migrating from on-premises NetApp.

The exam trap: choosing EFS for Windows workloads (EFS is NFS/Linux only) or choosing FSx for Windows when the scenario specifies Linux (SMB is not native to Linux).

## Step 1 -- Decision Table

| Requirement | Best Choice | Why |
|---|---|---|
| Shared file storage for Linux EC2 | **EFS** | NFS, POSIX, multi-AZ |
| Shared file storage for Windows EC2 | **FSx for Windows** | SMB, AD integration, NTFS |
| High-performance computing (HPC) | **FSx for Lustre** | Sub-ms latency, S3 integration |
| Machine learning training data | **FSx for Lustre** | Fast S3 data loading, high throughput |
| Both NFS and SMB from one system | **FSx for ONTAP** | Multi-protocol |
| Migrating from on-prem NetApp | **FSx for ONTAP** | Compatible features |
| Lambda function shared storage | **EFS** | Native Lambda-EFS integration |
| ECS container shared storage | **EFS** | Native ECS-EFS integration |
| Content management (WordPress) | **EFS** | Linux + multi-AZ + elastic |
| SQL Server file shares | **FSx for Windows** | SMB + AD + DFS |

## Step 2 -- Feature Comparison

| Feature | EFS | FSx Windows | FSx Lustre | FSx ONTAP |
|---|---|---|---|---|
| **Protocol** | NFSv4.1 | SMB 2.0-3.1.1 | Lustre | NFS, SMB, iSCSI |
| **OS** | Linux | Windows | Linux | Linux, Windows |
| **Capacity** | Elastic (auto) | Fixed (set at creation) | Fixed | Fixed |
| **Max throughput** | 10+ GB/s (Elastic) | 2 GB/s | 100s GB/s | 4 GB/s |
| **Latency** | Sub-ms (GP mode) | Sub-ms | Sub-ms | Sub-ms |
| **Multi-AZ** | Yes (default) | Optional | No (single subnet) | Yes (Multi-AZ) |
| **S3 integration** | No | No | Yes (data repository) | Yes (FlexCache) |
| **Active Directory** | No | Yes (required) | No | Optional |
| **Encryption at rest** | KMS | KMS | KMS | KMS |
| **Encryption in transit** | TLS (NFSv4.1) | SMB encryption | In-transit encryption | TLS |
| **Pricing model** | Per GB stored | Per GB provisioned | Per GB provisioned | Per GB provisioned |

## Step 3 -- Create EFS File System

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
  default     = "saa-ex83"
}
```

### `security.tf`

```hcl
data "aws_vpc" "default" {
  default = true
}

resource "aws_security_group" "efs" {
  name   = "${var.project_name}-efs-sg"
  vpc_id = data.aws_vpc.default.id

  ingress {
    from_port   = 2049
    to_port     = 2049
    protocol    = "tcp"
    cidr_blocks = [data.aws_vpc.default.cidr_block]
    description = "NFS from VPC"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}
```

### `storage.tf`

```hcl
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

# ---------- EFS File System ----------

resource "aws_efs_file_system" "this" {
  creation_token = "${var.project_name}-efs"

  # Performance mode:
  #   - generalPurpose (default): lowest latency, 7000 IOPS limit
  #   - maxIO: higher IOPS but slightly higher latency
  #     (used for highly parallelized workloads)
  performance_mode = "generalPurpose"

  # Throughput mode:
  #   - bursting (default): throughput scales with storage size
  #     (50 MiB/s per TB stored, burst to 100 MiB/s)
  #   - provisioned: fixed throughput regardless of size
  #     (use when you need more throughput than storage allows)
  #   - elastic: automatic throughput scaling up to 10 GiB/s
  #     (recommended for unpredictable workloads)
  throughput_mode = "elastic"

  # Encryption at rest using KMS
  encrypted = true

  # Lifecycle policy: transition to Infrequent Access after 30 days
  lifecycle_policy {
    transition_to_ia = "AFTER_30_DAYS"
  }

  # Transition back to Standard on access
  lifecycle_policy {
    transition_to_primary_storage_class = "AFTER_1_ACCESS"
  }

  tags = { Name = "${var.project_name}-efs" }
}

# ---------- Mount Targets ----------
# One mount target per AZ provides cross-AZ access.
# EC2 instances mount via the mount target in their AZ.

resource "aws_efs_mount_target" "this" {
  count           = min(length(data.aws_subnets.default.ids), 2)
  file_system_id  = aws_efs_file_system.this.id
  subnet_id       = data.aws_subnets.default.ids[count.index]
  security_groups = [aws_security_group.efs.id]
}

# ---------- EFS Access Point ----------
# Access points provide application-specific entry points
# with enforced user/group and root directory.

resource "aws_efs_access_point" "app" {
  file_system_id = aws_efs_file_system.this.id

  posix_user {
    uid = 1000
    gid = 1000
  }

  root_directory {
    path = "/app-data"
    creation_info {
      owner_uid   = 1000
      owner_gid   = 1000
      permissions = "755"
    }
  }

  tags = { Name = "${var.project_name}-app-access-point" }
}
```

### `outputs.tf`

```hcl
output "efs_id" {
  value = aws_efs_file_system.this.id
}

output "efs_dns_name" {
  value = aws_efs_file_system.this.dns_name
}

output "mount_command" {
  value = "sudo mount -t nfs4 -o nfsvers=4.1,rsize=1048576,wsize=1048576,hard,timeo=600,retrans=2 ${aws_efs_file_system.this.dns_name}:/ /mnt/efs"
}
```

## Step 4 -- EFS Storage Classes and Costs

| Storage Class | Price (us-east-1) | Access Cost | Use Case |
|---|---|---|---|
| **Standard** | $0.30/GB-month | None | Frequently accessed data |
| **Infrequent Access (IA)** | $0.016/GB-month | $0.01/GB read | Data accessed a few times a month |
| **Archive** | $0.008/GB-month | $0.03/GB read | Data accessed a few times a year |

### Cost Example

1 TB of data, 20% accessed frequently, 80% infrequent:

| Approach | Monthly Cost |
|---|---|
| All Standard | 1,000 GB x $0.30 = **$300.00** |
| With lifecycle (20% Standard, 80% IA) | 200 x $0.30 + 800 x $0.016 = **$72.80** |
| All IA | 1,000 x $0.016 = **$16.00** (+ access charges) |

Lifecycle policies can reduce storage costs by 75%+ for data with mixed access patterns.

## Step 5 -- EFS Throughput Modes

| Mode | How Throughput Works | Best For |
|---|---|---|
| **Bursting** | 50 MiB/s per TB + burst credits | Small to medium workloads with burst needs |
| **Provisioned** | You set throughput (1-3,072 MiB/s) | Workloads needing consistent throughput > bursting allows |
| **Elastic** | Auto-scales up to 10 GiB/s | Unpredictable workloads, spiky access patterns |

**Bursting trap**: A 100 GB EFS file system in bursting mode gets only 5 MiB/s baseline (100 GB x 50 MiB/s per TB = 5 MiB/s). It can burst to 100 MiB/s, but only while burst credits last. For sustained throughput on small file systems, use provisioned or elastic.

## Common Mistakes

### 1. Using EFS for Windows workloads

**Wrong approach:** Deploying EFS and attempting to mount it on Windows EC2 instances.

**What happens:** Windows does not natively support NFS without additional configuration (NFS Client feature). Even with the NFS client, you lose NTFS permissions, Active Directory integration, and SMB features that Windows applications expect.

**Fix:** Use FSx for Windows File Server. It provides native SMB, Active Directory integration, DFS namespaces, and NTFS permissions.

### 2. Choosing Max I/O when General Purpose suffices

**Wrong approach:** Setting `performance_mode = "maxIO"` because "more is better."

**What happens:** Max I/O has higher per-operation latency than General Purpose. For most workloads (web serving, content management, development), General Purpose provides sufficient IOPS with lower latency. Max I/O is designed for highly parallelized workloads with thousands of concurrent clients (HPC, media processing).

**Fix:** Start with General Purpose. Monitor CloudWatch metric `PercentIOLimit`. Only switch to Max I/O if you consistently hit the 7,000 IOPS limit. Note: in Elastic throughput mode, General Purpose can exceed 7,000 IOPS.

### 3. Forgetting mount targets in each AZ

**Wrong approach:** Creating one mount target and expecting all AZs to work.

```hcl
resource "aws_efs_mount_target" "this" {
  file_system_id = aws_efs_file_system.this.id
  subnet_id      = data.aws_subnets.default.ids[0]  # Only us-east-1a
}
```

**What happens:** EC2 instances in other AZs can still mount the file system via the single mount target, but traffic crosses AZ boundaries, incurring cross-AZ data transfer charges ($0.01/GB each way) and adding latency.

**Fix:** Create one mount target in each AZ where you have EC2 instances:

```hcl
resource "aws_efs_mount_target" "this" {
  count          = length(data.aws_subnets.default.ids)
  file_system_id = aws_efs_file_system.this.id
  subnet_id      = data.aws_subnets.default.ids[count.index]
}
```

## Verify What You Learned

```bash
terraform init
terraform apply -auto-approve

# Verify EFS file system
aws efs describe-file-systems \
  --creation-token saa-ex83-efs \
  --query 'FileSystems[0].{Id:FileSystemId,State:LifeCycleState,ThroughputMode:ThroughputMode,PerformanceMode:PerformanceMode,Encrypted:Encrypted}' \
  --output table
```

Expected: State `available`, ThroughputMode `elastic`, PerformanceMode `generalPurpose`, Encrypted `True`.

```bash
# Verify mount targets
EFS_ID=$(terraform output -raw efs_id)
aws efs describe-mount-targets \
  --file-system-id "$EFS_ID" \
  --query 'MountTargets[*].{AZ:AvailabilityZoneName,State:LifeCycleState,IP:IpAddress}' \
  --output table
```

Expected: Mount targets in 2 AZs with state `available`.

```bash
# Verify lifecycle policy
aws efs describe-lifecycle-configuration \
  --file-system-id "$EFS_ID" \
  --query 'LifecyclePolicies'
```

Expected: Transition to IA after 30 days, transition to Standard on access.

```bash
# Verify access point
aws efs describe-access-points \
  --file-system-id "$EFS_ID" \
  --query 'AccessPoints[0].{Name:Name,RootDir:RootDirectory.Path,PosixUser:PosixUser}' \
  --output table
```

Expected: Root directory `/app-data`, PosixUser with UID/GID 1000.

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
EFS_ID=$(terraform output -raw efs_id 2>/dev/null)
aws efs describe-file-systems --file-system-id "$EFS_ID" 2>&1 | grep -q "does not exist" && echo "EFS deleted" || echo "Still exists"
```

## What's Next

Exercise 84 covers **FSx for Windows File Server**, where you will deploy a managed Windows file system with Active Directory integration, mount it from a Windows EC2 instance via SMB, and understand Multi-AZ deployment for production resilience.

## Summary

- **EFS**: NFS for Linux, elastic (auto-size), multi-AZ, works with EC2, ECS, Lambda -- use for shared Linux file storage
- **FSx for Windows**: SMB for Windows, Active Directory integration, NTFS, DFS namespaces -- use for Windows file shares
- **FSx for Lustre**: high-performance parallel file system, S3 data repository integration -- use for HPC, ML, media processing
- **FSx for NetApp ONTAP**: multi-protocol (NFS + SMB + iSCSI), snapshots, cloning, dedup, compression -- use for multi-protocol or NetApp migration
- **EFS performance modes**: General Purpose (low latency, 7K IOPS) vs Max I/O (higher IOPS, higher latency)
- **EFS throughput modes**: Bursting (scales with size), Provisioned (fixed), Elastic (auto-scales to 10 GiB/s)
- **EFS lifecycle**: Standard -> IA (30 days) -> Archive (90 days) reduces costs by 75%+ for infrequently accessed data
- **Mount targets**: one per AZ to avoid cross-AZ data transfer charges and latency
- **Access points**: application-specific entry points with enforced POSIX user, group, and root directory
- **EFS is the only file system that integrates with Lambda** for serverless shared storage

## Reference

- [Amazon EFS User Guide](https://docs.aws.amazon.com/efs/latest/ug/whatisefs.html)
- [FSx for Windows](https://docs.aws.amazon.com/fsx/latest/WindowsGuide/what-is.html)
- [FSx for Lustre](https://docs.aws.amazon.com/fsx/latest/LustreGuide/what-is.html)
- [Terraform aws_efs_file_system](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/efs_file_system)

## Additional Resources

- [EFS Performance](https://docs.aws.amazon.com/efs/latest/ug/performance.html) -- detailed throughput and IOPS characteristics per mode
- [EFS with Lambda](https://docs.aws.amazon.com/lambda/latest/dg/configuration-filesystem.html) -- mounting EFS in Lambda for shared file access
- [FSx File System Comparison](https://docs.aws.amazon.com/fsx/latest/WindowsGuide/what-is.html) -- AWS comparison of all FSx options
- [EFS Pricing](https://aws.amazon.com/efs/pricing/) -- storage class pricing and throughput mode costs

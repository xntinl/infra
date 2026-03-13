# 84. FSx for Windows File Server

<!--
difficulty: intermediate
concepts: [fsx-windows, smb-protocol, active-directory, aws-managed-ad, dfs-namespaces, multi-az, single-az, shadow-copies, data-deduplication, storage-capacity, throughput-capacity, ssd-vs-hdd]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: apply, analyze
prerequisites: [83-efs-vs-fsx-decision-framework]
aws_cost: ~$0.10/hr
-->

> **AWS Cost Warning:** FSx for Windows Single-AZ SSD: ~$0.046/GB-month storage + $0.536/MBps-month throughput. Minimum 32 GB + 8 MBps = ~$0.006/hr storage + ~$0.006/hr throughput. AWS Managed AD (if used): $0.088/hr. Total ~$0.10/hr. Remember to run `terraform destroy` immediately when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Completed exercise 83 (EFS vs FSx) | Understanding of file system types |
| Default VPC with subnets in >= 2 AZs | `aws ec2 describe-subnets --filters Name=default-for-az,Values=true --query 'Subnets[*].AvailabilityZone'` |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Implement** FSx for Windows File Server with Active Directory integration using Terraform.
2. **Analyze** the trade-offs between Single-AZ (cost-optimized) and Multi-AZ (production resilience) deployments.
3. **Apply** storage and throughput capacity sizing based on workload requirements.
4. **Evaluate** SSD versus HDD storage types for different access patterns.
5. **Design** file sharing architectures for Windows workloads with DFS namespaces and shadow copies.

---

## Why This Matters

FSx for Windows File Server is the SAA-C03 answer to "the application requires shared Windows file storage with Active Directory integration." The exam tests whether you can distinguish between EFS (NFS/Linux) and FSx for Windows (SMB/Windows) based on the scenario requirements. Key signals that point to FSx for Windows: "SMB protocol," "Active Directory," "Windows Server," "NTFS permissions," "DFS namespaces," "Group Policy," "SQL Server file shares."

The critical architectural decision for the exam is Single-AZ versus Multi-AZ. Single-AZ is cheaper and sufficient for development, testing, and workloads where brief downtime is acceptable. Multi-AZ provides automatic failover to a standby file system in a different AZ, with sub-30-second failover for production workloads. The exam presents scenarios where "the business requires no data loss and minimal downtime for file shares" -- the answer is Multi-AZ.

FSx for Windows also provides features that the exam tests as differentiators: shadow copies (previous versions of files accessible by end users), data deduplication (reduces storage costs for redundant data), and DFS namespaces (organize multiple file shares under a single namespace). These features are unavailable in EFS, making FSx for Windows the only choice when Windows-specific features are required.

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
  default     = "saa-ex84"
}

variable "ad_password" {
  type      = string
  sensitive = true
  default   = "SuperSecretP@ssw0rd!"
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


# ============================================================
# TODO 1: AWS Managed Microsoft AD
# ============================================================
# FSx for Windows requires Active Directory. Create an AWS
# Managed Microsoft AD (or use self-managed AD).
#
# Requirements:
#   - Resource: aws_directory_service_directory
#   - name = "corp.example.com"
#   - type = "MicrosoftAD"
#   - edition = "Standard" (Standard or Enterprise)
#   - password = (use a variable with sensitive = true)
#   - vpc_settings: vpc_id, subnet_ids (2 subnets in different AZs)
#
# Note: AWS Managed AD takes 20-30 minutes to create.
# For the exam, know that FSx can use either:
#   1. AWS Managed Microsoft AD (fully managed)
#   2. Self-managed AD (on-premises or EC2-based)
#   3. AD Connector (proxy to on-premises AD)
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/directory_service_directory
# ============================================================


# ============================================================
# TODO 2: FSx for Windows File System (Single-AZ)
# ============================================================
# Create an FSx for Windows file system in Single-AZ mode.
#
# Requirements:
#   - Resource: aws_fsx_windows_file_system
#   - storage_capacity = 32 (minimum, in GB)
#   - storage_type = "SSD" (SSD or HDD)
#   - throughput_capacity = 8 (minimum, in MBps)
#   - subnet_ids = [single subnet for Single-AZ]
#   - security_group_ids = [SMB security group]
#   - active_directory_id = AWS Managed AD id
#   - deployment_type = "SINGLE_AZ_2"
#     (SINGLE_AZ_1, SINGLE_AZ_2, MULTI_AZ_1)
#
# SSD vs HDD:
#   - SSD: latency-sensitive workloads, databases, home dirs
#     Min: 32 GB, $0.046/GB-month
#   - HDD: large, throughput-heavy workloads (only SINGLE_AZ_2
#     or MULTI_AZ_1), min: 2,000 GB, $0.013/GB-month
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/fsx_windows_file_system
# ============================================================


# ============================================================
# TODO 3: Security Group for SMB
# ============================================================
# Create a security group allowing SMB traffic.
#
# Requirements:
#   - Inbound: TCP 445 (SMB) from VPC CIDR
#   - Inbound: TCP 5985 (WinRM HTTP) from VPC CIDR
#   - Inbound: TCP 53, UDP 53 (DNS) from VPC CIDR
#   - Inbound: TCP 88, UDP 88 (Kerberos) from VPC CIDR
#   - Outbound: all traffic
#
# Note: FSx for Windows uses port 445 for SMB file sharing.
# DNS and Kerberos ports are needed for AD authentication.
# ============================================================


# ============================================================
# TODO 4: Mount from Windows EC2 (CLI)
# ============================================================
# After terraform apply (30+ minutes for AD creation),
# mount the FSx share from a Windows EC2 instance:
#
#   a) Get the file system DNS name:
#      aws fsx describe-file-systems \
#        --query 'FileSystems[?Tags[?Key==`Name`&&Value==`saa-ex84-fsx`]].DNSName' \
#        --output text
#
#   b) RDP to a Windows EC2 instance joined to the AD domain
#
#   c) Open PowerShell and mount:
#      net use Z: \\<fsx-dns-name>\share /user:corp\Admin <password>
#
#   d) Verify:
#      dir Z:\
#
#   e) For domain-joined instances, use UNC path directly:
#      \\<fsx-dns-name>\share
#
# Note: The default share name is "share". You can create
# additional shares using the fsmgmt.msc snap-in.
# ============================================================
```

### `outputs.tf`

```hcl
output "fsx_dns_name" {
  value = "Set after TODO 2 implementation"
}

output "ad_dns_ips" {
  value = "Set after TODO 1 implementation"
}
```

---

## FSx for Windows Architecture

### Single-AZ vs Multi-AZ

| Feature | Single-AZ | Multi-AZ |
|---|---|---|
| **Availability** | Single file server | Active + standby in different AZs |
| **Failover** | None (manual from backup) | Automatic (<30 seconds) |
| **Data durability** | Replicated within AZ | Synchronously replicated across AZs |
| **Cost** | Base price | ~2x base price |
| **Use case** | Dev/test, non-critical | Production, business-critical |
| **Deployment type** | SINGLE_AZ_1, SINGLE_AZ_2 | MULTI_AZ_1 |

### Storage and Throughput Sizing

| Throughput (MBps) | Network MB/s | IOPS (SSD) | Max clients |
|---|---|---|---|
| 8 | 8 | Up to 10,000 | Small workload |
| 32 | 32 | Up to 20,000 | Medium workload |
| 128 | 128 | Up to 80,000 | Large workload |
| 512 | 512 | Up to 200,000 | Enterprise workload |
| 2,048 | 2,048 | Up to 400,000 | Extreme workload |

Throughput capacity can be increased or decreased after creation.

---

## Spot the Bug

The following FSx for Windows configuration is deployed for a production workload that requires high availability and no data loss. Identify the architectural flaw.

```hcl
resource "aws_fsx_windows_file_system" "production" {
  storage_capacity    = 500
  storage_type        = "SSD"
  throughput_capacity = 64
  subnet_ids          = [data.aws_subnets.default.ids[0]]
  deployment_type     = "SINGLE_AZ_2"
  active_directory_id = aws_directory_service_directory.corp.id

  tags = { Name = "production-file-server", Environment = "prod" }
}
```

<details>
<summary>Explain the bug</summary>

**The file system uses `SINGLE_AZ_2` (Single-AZ) deployment for a production workload requiring high availability and no data loss.** With Single-AZ deployment, there is no standby file system. If the AZ experiences an outage or the file server fails, the file system becomes unavailable until the underlying issue is resolved or you restore from backup. Data is only replicated within the single AZ.

For production workloads requiring high availability and no data loss, you must use `MULTI_AZ_1` deployment:

**Fix:**

```hcl
resource "aws_fsx_windows_file_system" "production" {
  storage_capacity    = 500
  storage_type        = "SSD"
  throughput_capacity = 64
  subnet_ids          = slice(data.aws_subnets.default.ids, 0, 2)  # Two subnets in different AZs
  deployment_type     = "MULTI_AZ_1"
  preferred_subnet_id = data.aws_subnets.default.ids[0]           # Primary AZ
  active_directory_id = aws_directory_service_directory.corp.id

  tags = { Name = "production-file-server", Environment = "prod" }
}
```

Multi-AZ deployment:
- Requires two subnet IDs in different AZs
- `preferred_subnet_id` specifies where the active file server runs
- Data is synchronously replicated to the standby AZ
- Automatic failover in <30 seconds if the active AZ fails
- Costs approximately 2x Single-AZ but provides production-grade resilience

The `subnet_ids` argument is the key signal: Single-AZ takes one subnet, Multi-AZ takes two.

</details>

---

## Verify What You Learned

1. **Deploy the infrastructure:**
   ```bash
   terraform init && terraform apply -auto-approve
   ```
   Note: AWS Managed AD takes 20-30 minutes. FSx takes 15-20 minutes.

2. **Verify Active Directory:**
   ```bash
   aws ds describe-directories \
     --query 'DirectoryDescriptions[?Name==`corp.example.com`].{Name:Name,Type:Type,Stage:Stage,DnsIps:DnsIpAddrs}' \
     --output table
   ```
   Expected: Stage `Active`, two DNS IPs.

3. **Verify FSx file system:**
   ```bash
   aws fsx describe-file-systems \
     --query 'FileSystems[?Tags[?Key==`Name`&&Value==`saa-ex84-fsx`]].{Id:FileSystemId,Status:Lifecycle,Type:FileSystemType,Storage:StorageCapacity,Throughput:WindowsConfiguration.ThroughputCapacity,DeploymentType:WindowsConfiguration.DeploymentType,DNS:DNSName}' \
     --output table
   ```
   Expected: Lifecycle `AVAILABLE`, FileSystemType `WINDOWS`, DeploymentType `SINGLE_AZ_2`.

4. **Verify SMB endpoints:**
   ```bash
   FSX_ID=$(aws fsx describe-file-systems \
     --query 'FileSystems[?Tags[?Key==`Name`&&Value==`saa-ex84-fsx`]].FileSystemId' --output text)
   aws fsx describe-file-systems --file-system-ids "$FSX_ID" \
     --query 'FileSystems[0].WindowsConfiguration.{RemoteAdmin:RemoteAdministrationEndpoint,PreferredSubnet:PreferredSubnetId}'
   ```
   Expected: Remote administration endpoint and subnet.

5. **Terraform state consistency:**
   ```bash
   terraform plan
   ```
   Expected: `No changes. Your infrastructure matches the configuration.`

---

## Solutions

<details>
<summary>main.tf -- TODO 1: AWS Managed Microsoft AD</summary>

```hcl
resource "aws_directory_service_directory" "corp" {
  name     = "corp.example.com"
  type     = "MicrosoftAD"
  edition  = "Standard"
  password = var.ad_password

  vpc_settings {
    vpc_id     = data.aws_vpc.default.id
    subnet_ids = slice(data.aws_subnets.default.ids, 0, 2)
  }

  tags = { Name = "${var.project_name}-ad" }
}
```

AWS Managed AD provides two domain controllers in different AZs. Standard edition supports up to 5,000 users (sufficient for most workloads). Enterprise edition supports up to 500,000 users with additional features like LDAP signing.

</details>

<details>
<summary>storage.tf -- TODO 2: FSx for Windows File System</summary>

```hcl
resource "aws_fsx_windows_file_system" "this" {
  storage_capacity    = 32
  storage_type        = "SSD"
  throughput_capacity = 8
  subnet_ids          = [data.aws_subnets.default.ids[0]]
  deployment_type     = "SINGLE_AZ_2"
  active_directory_id = aws_directory_service_directory.corp.id
  security_group_ids  = [aws_security_group.fsx.id]

  tags = { Name = "${var.project_name}-fsx" }
}
```

For this exercise, Single-AZ with minimum capacity is sufficient. In production, use MULTI_AZ_1 with appropriate storage and throughput sizing.

</details>

<details>
<summary>security.tf -- TODO 3: Security Group</summary>

```hcl
resource "aws_security_group" "fsx" {
  name   = "${var.project_name}-fsx-sg"
  vpc_id = data.aws_vpc.default.id

  ingress {
    from_port   = 445
    to_port     = 445
    protocol    = "tcp"
    cidr_blocks = [data.aws_vpc.default.cidr_block]
    description = "SMB"
  }

  ingress {
    from_port   = 5985
    to_port     = 5985
    protocol    = "tcp"
    cidr_blocks = [data.aws_vpc.default.cidr_block]
    description = "WinRM HTTP"
  }

  ingress {
    from_port   = 53
    to_port     = 53
    protocol    = "tcp"
    cidr_blocks = [data.aws_vpc.default.cidr_block]
    description = "DNS TCP"
  }

  ingress {
    from_port   = 53
    to_port     = 53
    protocol    = "udp"
    cidr_blocks = [data.aws_vpc.default.cidr_block]
    description = "DNS UDP"
  }

  ingress {
    from_port   = 88
    to_port     = 88
    protocol    = "tcp"
    cidr_blocks = [data.aws_vpc.default.cidr_block]
    description = "Kerberos TCP"
  }

  ingress {
    from_port   = 88
    to_port     = 88
    protocol    = "udp"
    cidr_blocks = [data.aws_vpc.default.cidr_block]
    description = "Kerberos UDP"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "${var.project_name}-fsx-sg" }
}
```

</details>

<details>
<summary>outputs.tf -- Updated Outputs</summary>

```hcl
output "fsx_dns_name" {
  value = aws_fsx_windows_file_system.this.dns_name
}

output "ad_dns_ips" {
  value = aws_directory_service_directory.corp.dns_ip_addresses
}
```

</details>

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Note: AD and FSx deletion takes 15-30 minutes. Verify:

```bash
aws ds describe-directories \
  --query 'DirectoryDescriptions[?Name==`corp.example.com`].Stage'
aws fsx describe-file-systems \
  --query 'FileSystems[?Tags[?Key==`Name`&&Value==`saa-ex84-fsx`]].Lifecycle'
```

Expected: Empty results or `Deleting` status.

---

## What's Next

Exercise 85 covers **FSx for Lustre** for high-performance computing workloads. You will deploy a Lustre file system with S3 data repository association for transparent data loading and understand the difference between scratch and persistent storage types.

---

## Summary

- **FSx for Windows** provides fully managed SMB file storage with native Active Directory integration
- **Active Directory** is required -- either AWS Managed Microsoft AD, self-managed AD, or AD Connector
- **Single-AZ** is cheaper for dev/test; **Multi-AZ** provides automatic failover (<30s) for production
- **SSD** (min 32 GB, $0.046/GB-month) for latency-sensitive workloads; **HDD** (min 2,000 GB, $0.013/GB-month) for throughput-heavy workloads
- **Throughput capacity** is independent of storage capacity and can be changed after creation
- **Shadow copies** provide user-accessible previous versions of files (Windows "Previous Versions" feature)
- **Data deduplication** reduces storage costs for environments with redundant data (VDI, software builds)
- **DFS namespaces** organize multiple FSx file systems under a single namespace for transparent scaling
- **Windows-specific signals on the exam**: SMB, Active Directory, NTFS, DFS, Group Policy, SQL Server -- always choose FSx for Windows, never EFS
- **Port 445** is the SMB port; DNS (53) and Kerberos (88) are needed for AD authentication

## Reference

- [FSx for Windows User Guide](https://docs.aws.amazon.com/fsx/latest/WindowsGuide/what-is.html)
- [Terraform aws_fsx_windows_file_system](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/fsx_windows_file_system)
- [FSx for Windows Pricing](https://aws.amazon.com/fsx/windows/pricing/)
- [AWS Managed Microsoft AD](https://docs.aws.amazon.com/directoryservice/latest/admin-guide/directory_microsoft_ad.html)

## Additional Resources

- [Multi-AZ Deployments](https://docs.aws.amazon.com/fsx/latest/WindowsGuide/multi-az-deployments.html) -- failover behavior and networking details
- [Shadow Copies](https://docs.aws.amazon.com/fsx/latest/WindowsGuide/shadow-copies-fsxW.html) -- configuring user-accessible file versioning
- [Data Deduplication](https://docs.aws.amazon.com/fsx/latest/WindowsGuide/using-data-dedup.html) -- reducing storage costs for redundant data patterns
- [DFS Namespaces](https://docs.aws.amazon.com/fsx/latest/WindowsGuide/group-file-systems.html) -- organizing multiple file systems under a single namespace

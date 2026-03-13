# 3. EBS Volume Types and Performance Optimization

<!--
difficulty: basic
concepts: [ebs-gp3, ebs-io2, ebs-st1, ebs-sc1, iops, throughput, ebs-modification, cloudwatch-volume-metrics]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: understand
prerequisites: [none]
aws_cost: ~$0.05/hr
-->

> **AWS Cost Warning:** EC2 t3.micro (~$0.0104/hr) + EBS volumes (gp3 8GB + io2 10GB + st1 125GB + sc1 125GB ~$0.03/hr). Total ~$0.05/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Basic understanding of storage I/O concepts (IOPS, throughput, latency)

## Learning Objectives

After completing this exercise, you will be able to:

- **Identify** the four main EBS volume types (gp3, io2, st1, sc1) and their performance characteristics
- **Construct** an EC2 instance with multiple EBS volumes of different types attached using Terraform
- **Verify** volume performance baselines using `fio` benchmarks and CloudWatch EBS metrics
- **Explain** why gp3 is almost always preferred over gp2 (independent IOPS/throughput tuning at the same price)
- **Describe** the relationship between volume size, provisioned IOPS, and throughput for each volume type
- **Compare** the cost-per-IOPS and cost-per-GB trade-offs across volume types
- **Distinguish** random I/O workloads (databases needing IOPS) from sequential I/O workloads (analytics needing throughput)

## Why EBS Volume Types Matter

EBS volume selection directly impacts application performance, cost, and reliability -- and the SAA-C03 exam tests this extensively. The four volume types map to distinct workload patterns: gp3 and io2 are SSD-backed for random I/O (databases, boot volumes, transactional workloads), while st1 and sc1 are HDD-backed for sequential I/O (log processing, data warehousing, cold archives). Choosing the wrong volume type means either paying too much for performance you don't need or bottlenecking your application on storage I/O. A common exam scenario presents a workload with specific IOPS or throughput requirements and asks you to select the most cost-effective volume type.

The most important architectural decision is understanding gp3 versus gp2. AWS introduced gp3 as the successor to gp2, offering the same baseline performance at 20% lower cost, with the critical advantage of independently configurable IOPS and throughput. With gp2, IOPS scales with volume size (3 IOPS per GB, burst to 3,000), so you might over-provision storage just to get more IOPS. With gp3, you get a baseline of 3,000 IOPS and 125 MiB/s regardless of volume size, and you can increase either independently up to 16,000 IOPS and 1,000 MiB/s. For io2, you provision IOPS explicitly and pay per IOPS -- this is the choice when you need guaranteed, sustained high IOPS (up to 64,000 per volume) with 99.999% durability. The exam expects you to calculate costs and match volume types to requirements, not just memorize names.

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
  default     = "ebs-demo"
}
```

### `vpc.tf`

```hcl
data "aws_availability_zones" "available" {
  state = "available"
}

resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags                 = { Name = var.project_name }
}

resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id
  tags   = { Name = var.project_name }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.this.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this.id
  }

  tags = { Name = "${var.project_name}-public" }
}

resource "aws_subnet" "public" {
  vpc_id                  = aws_vpc.this.id
  cidr_block              = "10.0.1.0/24"
  availability_zone       = data.aws_availability_zones.available.names[0]
  map_public_ip_on_launch = true
  tags                    = { Name = "${var.project_name}-public" }
}

resource "aws_route_table_association" "public" {
  subnet_id      = aws_subnet.public.id
  route_table_id = aws_route_table.public.id
}
```

### `security.tf`

```hcl
# ------------------------------------------------------------------
# Security group: allow SSH (for fio testing) and all outbound.
# In production, restrict SSH to your IP or use SSM Session Manager.
# ------------------------------------------------------------------
resource "aws_security_group" "this" {
  name_prefix = "${var.project_name}-"
  vpc_id      = aws_vpc.this.id
  description = "Allow SSH for EBS testing"

  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "SSH from anywhere (demo only)"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "${var.project_name}-sg" }
}
```

### `compute.tf`

```hcl
# ------------------------------------------------------------------
# Latest Amazon Linux 2023 AMI. The root volume uses gp3 by default.
# ------------------------------------------------------------------
data "aws_ami" "al2023" {
  most_recent = true
  owners      = ["amazon"]

  filter {
    name   = "name"
    values = ["al2023-ami-*-x86_64"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}

# ------------------------------------------------------------------
# EC2 instance: t3.micro is EBS-optimized by default (no extra
# charge). Older instance types like t2 require explicit
# ebs_optimized = true and may not support it at all.
#
# user_data installs fio (Flexible I/O Tester) for benchmarking.
# ------------------------------------------------------------------
resource "aws_instance" "this" {
  ami                    = data.aws_ami.al2023.id
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.public.id
  vpc_security_group_ids = [aws_security_group.this.id]

  # Root volume: gp3, 8 GB. This is the boot volume.
  # Only gp2, gp3, io1, and io2 can be used as boot volumes.
  # st1 and sc1 CANNOT be boot volumes.
  root_block_device {
    volume_type           = "gp3"
    volume_size           = 8
    delete_on_termination = true

    tags = { Name = "${var.project_name}-root-gp3" }
  }

  # Install fio for I/O benchmarking
  user_data = <<-USERDATA
    #!/bin/bash
    yum install -y fio
  USERDATA

  tags = { Name = "${var.project_name}-instance" }
}
```

### `storage.tf`

```hcl
# ------------------------------------------------------------------
# gp3 volume: General Purpose SSD (the default and recommended type).
#
# Baseline: 3,000 IOPS and 125 MiB/s included in the price.
# You can provision up to 16,000 IOPS and 1,000 MiB/s independently
# of volume size. This is the key advantage over gp2, where IOPS
# scales only with volume size (3 IOPS/GB).
#
# Cost: $0.08/GB/month (same as gp2, but with better baseline).
# ------------------------------------------------------------------
resource "aws_ebs_volume" "gp3_data" {
  availability_zone = data.aws_availability_zones.available.names[0]
  size              = 20
  type              = "gp3"

  # Baseline included: 3,000 IOPS, 125 MiB/s
  # Increasing IOPS costs $0.005/provisioned IOPS/month (above 3,000)
  # Increasing throughput costs $0.040/provisioned MiB/s/month (above 125)
  iops       = 3000
  throughput = 125

  tags = { Name = "${var.project_name}-gp3-data" }
}

# ------------------------------------------------------------------
# io2 volume: Provisioned IOPS SSD for latency-sensitive workloads.
#
# Use when you need guaranteed, sustained IOPS beyond what gp3
# provides (up to 64,000 IOPS per volume, 256,000 with io2 Block
# Express on Nitro instances). Also provides 99.999% durability
# versus 99.8%-99.9% for other types.
#
# Cost: $0.125/GB/month + $0.065/provisioned IOPS/month.
# At 10,000 IOPS on 10 GB, that is $1.25 + $650 = $651.25/month.
# This is expensive -- only use when gp3's 16,000 IOPS cap or
# 99.9% durability is insufficient.
# ------------------------------------------------------------------
resource "aws_ebs_volume" "io2_high_iops" {
  availability_zone = data.aws_availability_zones.available.names[0]
  size              = 10
  type              = "io2"

  # io2 requires a ratio of max 500 IOPS per GB.
  # 10 GB * 500 = 5,000 max IOPS for this volume size.
  # To provision 10,000 IOPS, you need at least 20 GB.
  iops = 5000

  tags = { Name = "${var.project_name}-io2-high-iops" }
}

# ------------------------------------------------------------------
# st1 volume: Throughput Optimized HDD for sequential workloads.
#
# Designed for frequently accessed, large sequential I/O: log
# processing, data warehousing, streaming, Kafka, MapReduce.
# Baseline: 40 MiB/s per TB, burst up to 250 MiB/s per TB,
# max 500 MiB/s per volume.
#
# CANNOT be a boot volume. Minimum size is 125 GB.
# Cost: $0.045/GB/month (44% cheaper than gp3 per GB).
# ------------------------------------------------------------------
resource "aws_ebs_volume" "st1_throughput" {
  availability_zone = data.aws_availability_zones.available.names[0]
  size              = 125
  type              = "st1"

  tags = { Name = "${var.project_name}-st1-throughput" }
}

# ------------------------------------------------------------------
# sc1 volume: Cold HDD for infrequently accessed data.
#
# The lowest cost EBS option. Designed for data that is rarely
# read but must be stored on block storage (not S3).
# Baseline: 12 MiB/s per TB, burst up to 80 MiB/s per TB,
# max 250 MiB/s per volume.
#
# CANNOT be a boot volume. Minimum size is 125 GB.
# Cost: $0.015/GB/month (81% cheaper than gp3 per GB).
# ------------------------------------------------------------------
resource "aws_ebs_volume" "sc1_cold" {
  availability_zone = data.aws_availability_zones.available.names[0]
  size              = 125
  type              = "sc1"

  tags = { Name = "${var.project_name}-sc1-cold" }
}

# ------------------------------------------------------------------
# Attach all volumes to the EC2 instance. Device names follow the
# Linux convention: /dev/xvdf through /dev/xvdi.
# ------------------------------------------------------------------
resource "aws_volume_attachment" "gp3_data" {
  device_name = "/dev/xvdf"
  volume_id   = aws_ebs_volume.gp3_data.id
  instance_id = aws_instance.this.id
}

resource "aws_volume_attachment" "io2_high_iops" {
  device_name = "/dev/xvdg"
  volume_id   = aws_ebs_volume.io2_high_iops.id
  instance_id = aws_instance.this.id
}

resource "aws_volume_attachment" "st1_throughput" {
  device_name = "/dev/xvdh"
  volume_id   = aws_ebs_volume.st1_throughput.id
  instance_id = aws_instance.this.id
}

resource "aws_volume_attachment" "sc1_cold" {
  device_name = "/dev/xvdi"
  volume_id   = aws_ebs_volume.sc1_cold.id
  instance_id = aws_instance.this.id
}
```

### `outputs.tf`

```hcl
output "instance_id" {
  description = "EC2 instance ID"
  value       = aws_instance.this.id
}

output "instance_public_ip" {
  description = "Public IP for SSH access"
  value       = aws_instance.this.public_ip
}

output "gp3_volume_id" {
  description = "gp3 data volume ID"
  value       = aws_ebs_volume.gp3_data.id
}

output "io2_volume_id" {
  description = "io2 high-IOPS volume ID"
  value       = aws_ebs_volume.io2_high_iops.id
}

output "st1_volume_id" {
  description = "st1 throughput volume ID"
  value       = aws_ebs_volume.st1_throughput.id
}

output "sc1_volume_id" {
  description = "sc1 cold storage volume ID"
  value       = aws_ebs_volume.sc1_cold.id
}
```

## Step 2 -- Initialize and Apply

```bash
terraform init
terraform apply -auto-approve
```

Terraform will create the VPC, subnet, security group, EC2 instance, four EBS volumes, and four volume attachments.

### Intermediate Verification

Confirm all volumes are attached:

```bash
INSTANCE_ID=$(terraform output -raw instance_id)

aws ec2 describe-volumes \
  --filters "Name=attachment.instance-id,Values=$INSTANCE_ID" \
  --query 'Volumes[*].{VolumeId:VolumeId,Type:VolumeType,Size:Size,IOPS:Iops,Throughput:Throughput,Device:Attachments[0].Device,State:Attachments[0].State}' \
  --output table
```

You should see 5 volumes (root gp3 + 4 attached): gp3, gp3, io2, st1, sc1.

## Step 3 -- Examine Volume Performance Characteristics

Compare the provisioned performance of each volume:

```bash
# Show IOPS and throughput for each volume type
aws ec2 describe-volumes \
  --filters "Name=attachment.instance-id,Values=$INSTANCE_ID" \
  --query 'Volumes[*].{Type:VolumeType,SizeGB:Size,IOPS:Iops,ThroughputMBs:Throughput,Device:Attachments[0].Device}' \
  --output table
```

Note that st1 and sc1 do not show IOPS or Throughput in the API response because their performance is baseline/burst-based, not provisioned.

### Cost Comparison Table

| Volume Type | Price/GB/month | IOPS | Throughput | Boot Volume | Use Case |
|-------------|---------------|------|------------|-------------|----------|
| gp3 | $0.08 | 3,000 baseline (up to 16,000) | 125 MiB/s baseline (up to 1,000) | Yes | General purpose, most workloads |
| gp2 | $0.10 | 3 per GB (burst 3,000) | Up to 250 MiB/s | Yes | Legacy (prefer gp3) |
| io2 | $0.125 + $0.065/IOPS | Up to 64,000 | Up to 1,000 MiB/s | Yes | Databases needing sustained high IOPS |
| st1 | $0.045 | N/A (HDD) | 40 MiB/s per TB (burst 250) | No | Sequential reads: logs, Kafka, ETL |
| sc1 | $0.015 | N/A (HDD) | 12 MiB/s per TB (burst 80) | No | Cold data, infrequent access |

## Step 4 -- Modify gp3 Volume Performance (Elastic Volumes)

Demonstrate elastic volumes by increasing IOPS and throughput on the gp3 volume without detaching it:

```bash
GP3_VOL=$(terraform output -raw gp3_volume_id)

# Modify gp3: increase from 3,000 to 6,000 IOPS and 125 to 250 MiB/s
aws ec2 modify-volume \
  --volume-id $GP3_VOL \
  --iops 6000 \
  --throughput 250

# Check modification progress
aws ec2 describe-volumes-modifications \
  --volume-id $GP3_VOL \
  --query 'VolumesModifications[0].{Status:ModificationState,OriginalIOPS:OriginalIops,TargetIOPS:TargetIops,OriginalThroughput:OriginalThroughput,TargetThroughput:TargetThroughput}' \
  --output table
```

The modification applies without downtime -- the volume remains attached and accessible. This is the Elastic Volumes feature. Note that after a modification, you must wait 6 hours before making another modification to the same volume.

## Step 5 -- Check CloudWatch Volume Metrics

CloudWatch collects EBS metrics automatically. View the available metrics for your volumes:

```bash
GP3_VOL=$(terraform output -raw gp3_volume_id)

# Check read/write IOPS over the last 5 minutes
aws cloudwatch get-metric-statistics \
  --namespace AWS/EBS \
  --metric-name VolumeReadOps \
  --dimensions Name=VolumeId,Value=$GP3_VOL \
  --start-time $(date -u -v-5M '+%Y-%m-%dT%H:%M:%S') \
  --end-time $(date -u '+%Y-%m-%dT%H:%M:%S') \
  --period 300 \
  --statistics Sum \
  --output table
```

Key CloudWatch EBS metrics to monitor:

| Metric | What It Tells You |
|--------|-------------------|
| `VolumeReadOps` / `VolumeWriteOps` | Total IOPS consumed (compare against provisioned) |
| `VolumeReadBytes` / `VolumeWriteBytes` | Throughput consumed |
| `VolumeQueueLength` | I/O operations waiting -- high values indicate bottleneck |
| `VolumeThroughputPercentage` | % of provisioned throughput used (io1/io2 only) |
| `VolumeConsumedReadWriteOps` | Provisioned IOPS consumed (io1/io2 only) |
| `BurstBalance` | Burst credit remaining (gp2, st1, sc1) |

## Common Mistakes

### 1. Trying to use st1 or sc1 as a boot volume

**Wrong approach:** Setting the root volume type to st1 for cost savings:

```hcl
root_block_device {
  volume_type = "st1"
  volume_size = 125
}
```

**What happens:** `terraform apply` fails with `InvalidParameterCombination: st1 is not supported for boot volumes`. The same error occurs for sc1. AWS requires SSD-backed volumes (gp2, gp3, io1, io2) for boot volumes because the OS needs random I/O performance during boot.

**Fix:** Use gp3 for the boot volume (best price/performance) and attach st1 or sc1 as additional data volumes:

```hcl
root_block_device {
  volume_type = "gp3"  # SSD for boot
  volume_size = 8
}
```

### 2. Using gp2 instead of gp3 for new deployments

**Wrong approach:** Defaulting to gp2 out of habit or using older Terraform examples:

```hcl
resource "aws_ebs_volume" "data" {
  type = "gp2"
  size = 100
  # gp2: 100 GB * 3 IOPS/GB = 300 IOPS (with burst to 3,000)
  # Cost: 100 * $0.10 = $10/month
}
```

**What happens:** You pay more ($0.10/GB vs $0.08/GB) and get worse baseline performance. A 100 GB gp2 volume has only 300 baseline IOPS (3 per GB), while a 100 GB gp3 volume has 3,000 baseline IOPS regardless of size. The gp2 volume relies on burst credits that deplete under sustained load.

**Fix:** Always use gp3 for new volumes. It is cheaper and has independent IOPS/throughput configuration:

```hcl
resource "aws_ebs_volume" "data" {
  type       = "gp3"
  size       = 100
  iops       = 3000   # Baseline included at no extra cost
  throughput = 125     # Baseline included at no extra cost
  # Cost: 100 * $0.08 = $8/month (20% less than gp2)
}
```

### 3. Not checking if the instance type is EBS-optimized

**Wrong approach:** Assuming all instance types support the full IOPS/throughput of your EBS volumes:

```hcl
resource "aws_instance" "this" {
  instance_type = "t2.micro"  # t2 is NOT EBS-optimized by default
}

resource "aws_ebs_volume" "fast" {
  type = "io2"
  iops = 10000  # Instance may not support this throughput
}
```

**What happens:** Without EBS optimization, the instance shares network bandwidth between EBS I/O and regular network traffic. Your provisioned IOPS are capped by the instance's EBS bandwidth, not the volume's capability. A t2.micro has limited EBS throughput and cannot saturate a high-IOPS io2 volume.

**Fix:** Use t3 or newer instance families that are EBS-optimized by default at no extra charge. For high-IOPS workloads, choose an instance type with sufficient EBS bandwidth:

```hcl
resource "aws_instance" "this" {
  instance_type = "t3.micro"  # EBS-optimized by default
  # t3.micro max EBS bandwidth: 260 MiB/s
  # For higher IOPS, use m5.large (593 MiB/s) or larger
}
```

Check your instance type's EBS performance limits in the [EC2 Instance Types page](https://aws.amazon.com/ec2/instance-types/).

## Verify What You Learned

```bash
INSTANCE_ID=$(terraform output -raw instance_id)

aws ec2 describe-volumes \
  --filters "Name=attachment.instance-id,Values=$INSTANCE_ID" "Name=volume-type,Values=gp3" \
  --query "Volumes[0].VolumeType" \
  --output text
```

Expected: `gp3`

```bash
aws ec2 describe-volumes \
  --filters "Name=attachment.instance-id,Values=$INSTANCE_ID" "Name=volume-type,Values=io2" \
  --query "Volumes[0].Iops" \
  --output text
```

Expected: `5000`

```bash
aws ec2 describe-volumes \
  --filters "Name=attachment.instance-id,Values=$INSTANCE_ID" "Name=volume-type,Values=st1" \
  --query "Volumes[0].Size" \
  --output text
```

Expected: `125`

```bash
aws ec2 describe-volumes \
  --filters "Name=attachment.instance-id,Values=$INSTANCE_ID" "Name=volume-type,Values=sc1" \
  --query "Volumes[0].Size" \
  --output text
```

Expected: `125`

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.` (Note: if you modified the gp3 volume IOPS/throughput via CLI in Step 4, Terraform will show a diff wanting to revert the change. This is expected -- Terraform manages the declared state.)

## Cleanup

Destroy all resources to stop incurring charges:

```bash
terraform destroy -auto-approve
```

Verify nothing remains:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

You explored all four EBS volume types and learned to match workloads to storage characteristics. In the next exercise, you will build a **VPC with public and private subnets, NAT Gateway, and Network ACLs** -- the foundational networking pattern for almost every AWS architecture, and the most tested networking topic on the SAA-C03 exam.

## Summary

- **gp3** is the default choice for most workloads: 3,000 IOPS and 125 MiB/s baseline, independently configurable, 20% cheaper than gp2
- **io2** provides guaranteed high IOPS (up to 64,000) with 99.999% durability -- use for mission-critical databases when gp3 is insufficient
- **st1** is throughput-optimized HDD for large sequential reads (logs, Kafka, ETL) at 44% less cost than gp3 per GB
- **sc1** is the cheapest EBS option for cold data that rarely needs to be read
- **st1 and sc1 cannot be boot volumes** -- only SSD types (gp2, gp3, io1, io2) support booting
- **Elastic Volumes** lets you modify volume type, size, IOPS, and throughput without downtime (6-hour cooldown between modifications)
- **Instance type determines EBS bandwidth ceiling** -- provisioning high IOPS is useless if the instance cannot saturate the volume
- Monitor **VolumeQueueLength** in CloudWatch to detect I/O bottlenecks before they impact application performance

## Reference

- [Amazon EBS Volume Types](https://docs.aws.amazon.com/ebs/latest/userguide/ebs-volume-types.html)
- [Amazon EBS Pricing](https://aws.amazon.com/ebs/pricing/)
- [Terraform aws_ebs_volume Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ebs_volume)
- [Terraform aws_volume_attachment Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/volume_attachment)

## Additional Resources

- [EBS Performance Tips](https://docs.aws.amazon.com/ebs/latest/userguide/EBSPerformance.html) -- pre-warming, RAID configurations, and how to maximize throughput for HDD volumes
- [Elastic Volumes (Modifying EBS Volumes)](https://docs.aws.amazon.com/ebs/latest/userguide/ebs-modify-volume.html) -- how to change volume type, size, and performance without detaching
- [EC2 Instance EBS Bandwidth](https://docs.aws.amazon.com/ec2/latest/instancetypes/gp.html) -- per-instance-type maximum EBS throughput and IOPS limits
- [CloudWatch Metrics for EBS](https://docs.aws.amazon.com/ebs/latest/userguide/using_cloudwatch_ebs.html) -- complete list of EBS metrics and recommended alarm thresholds

# 32. EC2 Placement Groups -- Cluster, Spread, and Partition

<!--
difficulty: basic
concepts: [placement-groups, cluster-placement, spread-placement, partition-placement, low-latency, fault-isolation, hardware-affinity]
tools: [terraform, aws-cli]
estimated_time: 30m
bloom_level: understand
prerequisites: [none]
aws_cost: ~$0.03/hr
-->

> **AWS Cost Warning:** This exercise launches t3.micro instances (~$0.0104/hr each). Running 4 instances for 30 minutes costs ~$0.03. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Default VPC available in your target region
- Completed exercise 31 or understanding of EC2 instance types

## Learning Objectives

After completing this exercise, you will be able to:

- **Identify** the three placement group strategies (Cluster, Spread, Partition) and their constraints
- **Explain** why Cluster placement groups provide lower network latency (same rack, enhanced networking)
- **Describe** the 7-instance-per-AZ limit for Spread placement groups and why it exists (separate hardware)
- **Distinguish** Partition placement groups from Spread groups (logical isolation vs hardware isolation per instance)
- **Construct** each placement group type using Terraform and verify instance placement
- **Select** the correct placement group strategy for a given workload scenario on the SAA-C03 exam

## Why Placement Groups Matter

Placement groups give you control over how EC2 places your instances on underlying physical hardware. This control matters because the physical location of instances determines network latency, fault blast radius, and capacity availability. The SAA-C03 exam tests all three strategies, and the questions follow a predictable pattern: "A company needs low-latency communication between nodes" (Cluster), "A company needs to maximize availability of individual instances" (Spread), or "A company runs a large distributed database like HDFS or Cassandra" (Partition).

Understanding the trade-offs is critical. Cluster placement groups give you the lowest possible network latency -- instances share the same rack and can achieve 10-25 Gbps throughput between them -- but if that rack fails, all instances fail together. Spread placement groups guarantee that each instance is on different physical hardware, so a single hardware failure affects only one instance -- but you are limited to 7 instances per AZ. Partition placement groups divide instances into logical partitions on separate hardware racks, supporting hundreds of instances while ensuring that a rack failure only affects one partition. Each strategy trades one property for another, and the exam expects you to know which trade-off is appropriate for which workload.

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
  default     = "saa-ex32"
}
```

### `main.tf`

```hcl
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

resource "aws_security_group" "instance" {
  name_prefix = "${var.project_name}-"
  vpc_id      = data.aws_vpc.default.id
  description = "Placement group demo instances"

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

# ------------------------------------------------------------------
# CLUSTER Placement Group
#
# Places all instances on the same rack in a single AZ for the
# lowest possible network latency. All instances must be in the
# same AZ (enforced by AWS).
#
# Trade-off: If the rack fails, ALL instances in the cluster fail.
# Use case: HPC, tightly coupled ML training, real-time analytics.
# ------------------------------------------------------------------
resource "aws_placement_group" "cluster" {
  name     = "${var.project_name}-cluster"
  strategy = "cluster"
}

# ------------------------------------------------------------------
# SPREAD Placement Group
#
# Places each instance on distinct physical hardware. Maximum of
# 7 instances per AZ (because there are a finite number of distinct
# racks per AZ that AWS exposes for spread placement).
#
# Trade-off: Limited to 7 per AZ, but a hardware failure can only
# affect one instance at a time.
# Use case: Critical instances that must survive hardware failure
# independently (e.g., primary/standby pairs, small HA clusters).
# ------------------------------------------------------------------
resource "aws_placement_group" "spread" {
  name     = "${var.project_name}-spread"
  strategy = "spread"
}

# ------------------------------------------------------------------
# PARTITION Placement Group
#
# Divides instances into logical partitions, each on separate
# hardware racks. Up to 7 partitions per AZ. Each partition can
# hold many instances, but all instances in a partition share
# the same rack(s).
#
# Trade-off: Less isolation than Spread (instances in a partition
# can co-fail), but supports hundreds of instances and provides
# rack-level fault awareness to the application.
# Use case: Large distributed workloads (HDFS, Cassandra, Kafka)
# that need rack-awareness for data replication.
# ------------------------------------------------------------------
resource "aws_placement_group" "partition" {
  name            = "${var.project_name}-partition"
  strategy        = "partition"
  partition_count = 3
}

# ------------------------------------------------------------------
# Cluster instances: 2 instances in the same AZ, same rack.
# Both must be in the same AZ for cluster placement to work.
# ------------------------------------------------------------------
resource "aws_instance" "cluster" {
  count                  = 2
  ami                    = data.aws_ami.al2023.id
  instance_type          = "t3.micro"
  subnet_id              = data.aws_subnets.default.ids[0]
  placement_group        = aws_placement_group.cluster.id
  vpc_security_group_ids = [aws_security_group.instance.id]

  tags = {
    Name    = "${var.project_name}-cluster-${count.index}"
    Group   = "cluster"
    Purpose = "placement-group-demo"
  }
}

# ------------------------------------------------------------------
# Spread instances: 2 instances guaranteed on different hardware.
# Can be in the same AZ -- AWS still places them on different racks.
# ------------------------------------------------------------------
resource "aws_instance" "spread" {
  count                  = 2
  ami                    = data.aws_ami.al2023.id
  instance_type          = "t3.micro"
  subnet_id              = data.aws_subnets.default.ids[0]
  placement_group        = aws_placement_group.spread.id
  vpc_security_group_ids = [aws_security_group.instance.id]

  tags = {
    Name    = "${var.project_name}-spread-${count.index}"
    Group   = "spread"
    Purpose = "placement-group-demo"
  }
}

# ------------------------------------------------------------------
# Partition instances: 2 instances across different partitions.
# partition_number tells AWS which logical partition to use.
# ------------------------------------------------------------------
resource "aws_instance" "partition" {
  count                  = 2
  ami                    = data.aws_ami.al2023.id
  instance_type          = "t3.micro"
  subnet_id              = data.aws_subnets.default.ids[0]
  placement_group        = aws_placement_group.partition.id
  placement_partition_number = count.index + 1
  vpc_security_group_ids = [aws_security_group.instance.id]

  tags = {
    Name      = "${var.project_name}-partition-${count.index}"
    Group     = "partition"
    Partition = count.index + 1
    Purpose   = "placement-group-demo"
  }
}
```

### `outputs.tf`

```hcl
output "cluster_instances" {
  value = [for i in aws_instance.cluster : {
    id   = i.id
    az   = i.availability_zone
    type = i.instance_type
  }]
}

output "spread_instances" {
  value = [for i in aws_instance.spread : {
    id   = i.id
    az   = i.availability_zone
    type = i.instance_type
  }]
}

output "partition_instances" {
  value = [for i in aws_instance.partition : {
    id        = i.id
    az        = i.availability_zone
    partition = i.placement_partition_number
  }]
}
```

## Step 2 -- Deploy and Verify Placement

```bash
terraform init
terraform apply -auto-approve
```

After deployment, verify placement group membership:

```bash
# Verify all instances and their placement groups
aws ec2 describe-instances \
  --filters "Name=tag:Purpose,Values=placement-group-demo" "Name=instance-state-name,Values=running" \
  --query 'Reservations[*].Instances[*].{Name:Tags[?Key==`Name`]|[0].Value,ID:InstanceId,AZ:Placement.AvailabilityZone,Group:Placement.GroupName,Partition:Placement.PartitionNumber}' \
  --output table
```

Verify the placement groups themselves:

```bash
aws ec2 describe-placement-groups \
  --group-names saa-ex32-cluster saa-ex32-spread saa-ex32-partition \
  --query 'PlacementGroups[*].{Name:GroupName,Strategy:Strategy,Partitions:PartitionCount,State:State}' \
  --output table
```

## Step 3 -- Understand the Placement Group Decision Table

| Feature | Cluster | Spread | Partition |
|---------|---------|--------|-----------|
| **Instances per AZ** | Unlimited | 7 maximum | Unlimited (7 partitions) |
| **Hardware isolation** | None (same rack) | Per instance | Per partition (rack) |
| **Network latency** | Lowest (single rack) | Normal | Normal |
| **AZ constraint** | Single AZ only | Multi-AZ supported | Multi-AZ supported |
| **Failure blast radius** | All instances | 1 instance | 1 partition |
| **Rack awareness** | No (all same rack) | N/A | Yes (partition = rack) |
| **Use case** | HPC, ML training | Critical HA pairs | HDFS, Cassandra, Kafka |
| **SAA-C03 keyword** | "low latency between nodes" | "survive hardware failure" | "distributed workload, rack-aware" |

## Common Mistakes

### 1. Trying to launch Cluster placement group instances across AZs

**Wrong approach:** Creating a cluster placement group and launching instances in different AZs.

**What happens:** The API returns an error. Cluster placement groups require all instances in the same AZ because they must share the same physical rack for low-latency networking.

**Fix:** Ensure all instances in a cluster placement group use the same subnet (and therefore the same AZ). If you need low latency AND cross-AZ redundancy, use two separate cluster placement groups (one per AZ) and replicate at the application layer.

### 2. Exceeding the 7-instance Spread limit per AZ

**Wrong approach:** Launching 10 instances in a Spread placement group in a single AZ.

**What happens:** The 8th instance fails to launch with `InsufficientInstanceCapacity`. AWS can only guarantee 7 distinct physical hosts per AZ for spread placement.

**Fix:** Distribute instances across multiple AZs (7 per AZ), or switch to Partition placement groups if you need more than 7 instances with hardware isolation. Partition supports up to 7 partitions per AZ, and each partition can hold many instances.

### 3. Confusing Partition with Spread

**Wrong approach:** Using Spread placement for a 100-node Cassandra cluster because "each node should be on different hardware."

**What happens:** Spread only supports 7 instances per AZ, so you cannot launch 100 instances. Even across 3 AZs, you are limited to 21 instances.

**Fix:** Use Partition placement. Cassandra already has rack-awareness built in. Map each partition to a Cassandra rack, configure the replication strategy to place replicas across different racks (partitions), and Cassandra handles the rest. Each partition can hold dozens of nodes.

## Verify What You Learned

```bash
# Verify cluster placement group exists with correct strategy
aws ec2 describe-placement-groups \
  --group-names saa-ex32-cluster \
  --query "PlacementGroups[0].Strategy" --output text
```

Expected: `cluster`

```bash
# Verify spread placement group
aws ec2 describe-placement-groups \
  --group-names saa-ex32-spread \
  --query "PlacementGroups[0].Strategy" --output text
```

Expected: `spread`

```bash
# Verify partition placement group with 3 partitions
aws ec2 describe-placement-groups \
  --group-names saa-ex32-partition \
  --query "PlacementGroups[0].{Strategy:Strategy,Partitions:PartitionCount}" --output table
```

Expected: Strategy=partition, Partitions=3

```bash
# Verify instances are in their placement groups
aws ec2 describe-instances \
  --filters "Name=tag:Group,Values=cluster" "Name=instance-state-name,Values=running" \
  --query "Reservations[*].Instances[*].Placement.GroupName" --output text
```

Expected: `saa-ex32-cluster`

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify cleanup:

```bash
aws ec2 describe-placement-groups \
  --filters "Name=group-name,Values=saa-ex32-cluster,saa-ex32-spread,saa-ex32-partition" \
  --query "PlacementGroups[*].GroupName" --output text
```

Expected: no output (placement groups deleted).

## What's Next

You learned how to control physical instance placement for latency, fault isolation, and distributed workloads. In the next exercise, you will explore **EC2 Spot Instances and Fleet strategies** -- achieving up to 90% cost savings by bidding on unused capacity with intelligent fleet diversification.

## Summary

- **Cluster** placement groups place all instances on the same rack for the lowest network latency, but a rack failure takes down everything
- **Spread** placement groups guarantee each instance is on separate physical hardware, limited to **7 instances per AZ**
- **Partition** placement groups divide instances into logical partitions on separate racks, supporting hundreds of instances with rack-level isolation
- Cluster requires a **single AZ**; Spread and Partition support **multi-AZ** deployments
- On the SAA-C03, "low latency between nodes" = Cluster, "survive hardware failure" = Spread, "distributed rack-aware workload" = Partition
- Placement groups are free -- there is no additional charge for using them
- You cannot merge existing instances into a placement group -- instances must be launched into the group

## Reference

- [Amazon EC2 Placement Groups](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/placement-groups.html)
- [Terraform aws_placement_group Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/placement_group)
- [Terraform aws_instance Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/instance)

## Additional Resources

- [EC2 Enhanced Networking](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/enhanced-networking.html) -- required for maximum cluster placement group throughput
- [Elastic Fabric Adapter (EFA)](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/efa.html) -- OS-bypass networking for HPC workloads in cluster placement groups
- [EC2 Placement Groups Best Practices](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/placement-groups.html#placement-groups-best-practices) -- capacity reservation, instance type homogeneity

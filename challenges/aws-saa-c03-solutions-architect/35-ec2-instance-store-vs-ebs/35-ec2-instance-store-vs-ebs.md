# 35. EC2 Instance Store vs EBS

<!--
difficulty: intermediate
concepts: [instance-store, ephemeral-storage, ebs-volumes, ebs-snapshots, ebs-multi-attach, io2-block-express, nvme, fio-benchmarking]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: apply, evaluate
prerequisites: [31-ec2-instance-types-right-sizing]
aws_cost: ~$0.05/hr
-->

> **AWS Cost Warning:** An i3.large instance (instance store) costs ~$0.156/hr. An m5.large with EBS gp3 costs ~$0.096/hr + ~$0.008/hr for 100GB gp3. Total ~$0.26/hr if both are running simultaneously. Terminate early if you only need to observe the concepts. Remember to run `terraform destroy` when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Default VPC available in target region | `aws ec2 describe-vpcs --filters Name=isDefault,Values=true` |
| Understanding of EC2 instance types | Completed exercise 31 |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Distinguish** between instance store (ephemeral, physically attached NVMe) and EBS (persistent, network-attached) storage.
2. **Explain** why instance store data is lost when an instance is stopped, terminated, or experiences hardware failure.
3. **Implement** an EC2 instance with EBS volumes and verify persistence across stop/start cycles.
4. **Compare** IOPS and throughput characteristics of instance store NVMe vs EBS gp3/io2 using fio benchmarks.
5. **Evaluate** when to use instance store (temporary caches, scratch data, buffers) vs EBS (databases, persistent state, boot volumes).

---

## Why This Matters

The SAA-C03 exam tests a critical distinction: instance store provides the highest possible IOPS (millions of random reads on i3 instances) but data is ephemeral -- it survives reboots but is lost on stop, terminate, or hardware failure. EBS volumes persist independently of the instance lifecycle, support snapshots for backup, and can be detached and reattached. Choosing wrong has severe consequences: using instance store for a database means data loss on instance stop; using EBS for a temporary cache means paying for persistence you do not need and getting lower IOPS than instance store would provide.

The exam presents scenarios designed to test this understanding. "An application requires the highest possible IOPS for a temporary processing buffer" -- answer: instance store. "A database must survive instance failures without data loss" -- answer: EBS with snapshots. "An application needs shared block storage between two instances in the same AZ" -- answer: EBS Multi-Attach (io2 only). These trade-offs directly map to real architecture decisions where the wrong choice either costs too much or loses data.

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
  default     = "saa-ex35"
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
```

### `iam.tf`

```hcl
resource "aws_iam_role" "ec2" {
  name = "${var.project_name}-ec2-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action    = "sts:AssumeRole"
      Effect    = "Allow"
      Principal = { Service = "ec2.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "ssm" {
  role       = aws_iam_role.ec2.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_instance_profile" "ec2" {
  name = "${var.project_name}-ec2-profile"
  role = aws_iam_role.ec2.name
}
```

### `security.tf`

```hcl
resource "aws_security_group" "instance" {
  name_prefix = "${var.project_name}-"
  vpc_id      = data.aws_vpc.default.id
  description = "Instance store vs EBS demo"

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
# ============================================================
# TODO 1: EBS-Backed Instance with gp3 Volume
# ============================================================
# Launch an m5.large with an additional EBS gp3 volume for
# benchmarking. Install fio for I/O testing.
#
# Requirements:
#   - Resource: aws_instance
#     - ami = data.aws_ami.al2023.id
#     - instance_type = "m5.large"
#     - subnet_id, iam_instance_profile, vpc_security_group_ids
#     - tags: Name = "${var.project_name}-ebs-instance"
#
#   - Resource: aws_ebs_volume
#     - availability_zone = same as instance
#     - size = 100 (GB)
#     - type = "gp3"
#     - iops = 3000 (gp3 baseline)
#     - throughput = 125 (MB/s, gp3 baseline)
#     - tags: Name = "${var.project_name}-data-volume"
#
#   - Resource: aws_volume_attachment
#     - device_name = "/dev/xvdf"
#     - volume_id = ebs volume id
#     - instance_id = instance id
#
#   - User data script:
#     #!/bin/bash
#     yum install -y fio
#     # Wait for volume attachment
#     while [ ! -b /dev/xvdf ]; do sleep 1; done
#     mkfs -t xfs /dev/xvdf
#     mkdir -p /data
#     mount /dev/xvdf /data
#     echo "EBS volume mounted at /data"
#
# Key concept: EBS volumes persist independently. You can stop
# the instance, and /data remains intact on restart. You can
# detach the volume and attach it to a different instance.
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ebs_volume
# ============================================================


# ============================================================
# TODO 2: Benchmark with fio
# ============================================================
# After deploying, connect via SSM Session Manager and run
# these fio commands to compare EBS performance:
#
# Sequential write (test throughput):
#   fio --name=seq-write --directory=/data --size=1G \
#     --rw=write --bs=1M --direct=1 --numjobs=1 --runtime=30
#
# Random read (test IOPS):
#   fio --name=rand-read --directory=/data --size=1G \
#     --rw=randread --bs=4K --direct=1 --numjobs=4 --runtime=30
#
# Record the results:
#   - gp3 baseline: 3,000 IOPS, 125 MB/s throughput
#   - gp3 maximum:  16,000 IOPS, 1,000 MB/s (costs extra)
#   - io2: up to 64,000 IOPS per volume
#   - i3 instance store: up to 3.3M random reads (NVMe)
#
# Note: Instance store benchmarks require an i3 instance type,
# which has higher hourly cost. Skip if cost is a concern.
# ============================================================


# ============================================================
# TODO 3: Demonstrate EBS Persistence
# ============================================================
# After writing data to the EBS volume, stop and start the
# instance to prove the data persists.
#
# Requirements (CLI commands):
#
# 1. Write a test file via SSM:
#    aws ssm send-command \
#      --instance-ids <INSTANCE_ID> \
#      --document-name "AWS-RunShellScript" \
#      --parameters 'commands=["echo persistence-test > /data/test.txt"]'
#
# 2. Stop the instance:
#    aws ec2 stop-instances --instance-ids <INSTANCE_ID>
#    aws ec2 wait instance-stopped --instance-ids <INSTANCE_ID>
#
# 3. Start the instance:
#    aws ec2 start-instances --instance-ids <INSTANCE_ID>
#    aws ec2 wait instance-running --instance-ids <INSTANCE_ID>
#
# 4. Verify data persists:
#    aws ssm send-command \
#      --instance-ids <INSTANCE_ID> \
#      --document-name "AWS-RunShellScript" \
#      --parameters 'commands=["mount /dev/xvdf /data && cat /data/test.txt"]'
#
# Expected: "persistence-test" is still there after stop/start.
# If this were instance store, the data would be gone.
# ============================================================
```

### `outputs.tf`

```hcl
output "security_group_id" {
  value = aws_security_group.instance.id
}
```

---

## Spot the Bug

A team selects an i3.xlarge instance for their PostgreSQL database because it offers the highest IOPS:

```hcl
resource "aws_instance" "database" {
  ami           = data.aws_ami.al2023.id
  instance_type = "i3.xlarge"  # 1x 950GB NVMe instance store

  user_data = base64encode(<<-EOF
    #!/bin/bash
    # Format instance store volume
    mkfs -t xfs /dev/nvme0n1
    mkdir -p /var/lib/postgresql
    mount /dev/nvme0n1 /var/lib/postgresql

    # Install and start PostgreSQL
    yum install -y postgresql-server
    postgresql-setup --initdb
    systemctl start postgresql
  EOF
  )

  tags = {
    Name = "production-database"
  }
}
```

<details>
<summary>Explain the bug</summary>

**Instance store data is lost when the instance is stopped, terminated, or experiences hardware failure.** This means:

1. If the team stops the instance for maintenance, all PostgreSQL data is permanently lost.
2. If the underlying hardware fails (which happens at scale), the database is gone with no recovery option.
3. There is no snapshot capability for instance store -- you cannot back up the volume to S3 like you can with EBS.
4. Auto Scaling groups that stop/start instances would wipe the database on every scaling event.

**The fix: Use EBS for database storage.**

```hcl
resource "aws_instance" "database" {
  ami           = data.aws_ami.al2023.id
  instance_type = "r5.xlarge"  # Memory-optimized for databases

  tags = {
    Name = "production-database"
  }
}

resource "aws_ebs_volume" "pgdata" {
  availability_zone = aws_instance.database.availability_zone
  size              = 500
  type              = "io2"
  iops              = 10000  # Provisioned IOPS for consistent DB performance
  encrypted         = true

  tags = {
    Name = "production-pgdata"
  }
}

resource "aws_volume_attachment" "pgdata" {
  device_name = "/dev/xvdf"
  volume_id   = aws_ebs_volume.pgdata.id
  instance_id = aws_instance.database.id
}
```

Instance store is appropriate for: temporary buffers, caches, scratch data, data that is replicated elsewhere (like Cassandra/HDFS replicas). It should never be the sole storage for data you cannot afford to lose.

For PostgreSQL specifically, if you need the IOPS of instance store, use it as a write-ahead log (WAL) cache with EBS as the primary data store, or use RDS/Aurora which handles storage durability automatically.

</details>

---

## Instance Store vs EBS Comparison

| Feature | Instance Store | EBS gp3 | EBS io2 |
|---------|---------------|---------|---------|
| **Persistence** | Lost on stop/terminate/failure | Persists independently | Persists independently |
| **Max IOPS** | 3.3M (i3en.24xlarge) | 16,000 per volume | 256,000 (io2 Block Express) |
| **Max throughput** | 14 GB/s (i3en.24xlarge) | 1,000 MB/s | 4,000 MB/s |
| **Snapshots** | Not supported | Yes (to S3) | Yes (to S3) |
| **Encryption** | Hardware-level | KMS-managed | KMS-managed |
| **Multi-Attach** | No | No | Yes (io2 only) |
| **Detach/reattach** | No (fixed to instance) | Yes | Yes |
| **Cost** | Included in instance price | $0.08/GB-month + IOPS | $0.125/GB-month + $0.065/IOPS |
| **Latency** | Lowest (local NVMe) | Sub-millisecond | Sub-millisecond |
| **Use case** | Cache, scratch, buffer | General workloads | Databases, critical I/O |

### EBS Volume Type Decision Table

| Volume Type | IOPS | Throughput | Cost/GB-mo | Best For |
|-------------|------|-----------|------------|----------|
| gp3 | 3,000 baseline (up to 16K) | 125 MB/s (up to 1,000) | $0.08 | Default choice, boot volumes |
| gp2 | 3 IOPS/GB (up to 16K) | Up to 250 MB/s | $0.10 | Legacy (migrate to gp3) |
| io2 | Up to 64K (256K Block Express) | Up to 4,000 MB/s | $0.125 + IOPS | Production databases |
| st1 | 500 baseline | Up to 500 MB/s | $0.045 | Big data, log processing |
| sc1 | 250 baseline | Up to 250 MB/s | $0.015 | Infrequent access, cold data |

---

## Solutions

<details>
<summary>TODO 1 -- EBS-Backed Instance with gp3 Volume -- `storage.tf`</summary>

```hcl
resource "aws_instance" "ebs_demo" {
  ami                    = data.aws_ami.al2023.id
  instance_type          = "m5.large"
  subnet_id              = data.aws_subnets.default.ids[0]
  iam_instance_profile   = aws_iam_instance_profile.ec2.name
  vpc_security_group_ids = [aws_security_group.instance.id]

  user_data = base64encode(<<-EOF
    #!/bin/bash
    yum install -y fio
    # Wait for EBS volume attachment
    while [ ! -b /dev/xvdf ]; do sleep 1; done
    mkfs -t xfs /dev/xvdf
    mkdir -p /data
    mount /dev/xvdf /data
    echo "EBS volume mounted at /data" > /data/mount-confirmation.txt
  EOF
  )

  tags = {
    Name = "${var.project_name}-ebs-instance"
  }
}

resource "aws_ebs_volume" "data" {
  availability_zone = aws_instance.ebs_demo.availability_zone
  size              = 100
  type              = "gp3"
  iops              = 3000
  throughput        = 125
  encrypted         = true

  tags = {
    Name = "${var.project_name}-data-volume"
  }
}

resource "aws_volume_attachment" "data" {
  device_name = "/dev/xvdf"
  volume_id   = aws_ebs_volume.data.id
  instance_id = aws_instance.ebs_demo.id
}

output "ebs_instance_id" {
  value = aws_instance.ebs_demo.id
}

output "ebs_volume_id" {
  value = aws_ebs_volume.data.id
}
```

Key design decisions:
- `encrypted = true`: always encrypt EBS volumes. There is no performance penalty and it satisfies compliance requirements.
- `gp3` with baseline IOPS (3,000): cheapest option for general workloads. Scale IOPS independently of volume size (unlike gp2 where IOPS = 3 * GB).
- Separate `aws_ebs_volume` + `aws_volume_attachment`: allows the volume to be managed independently of the instance lifecycle. If the instance is terminated, the volume persists.

</details>

<details>
<summary>TODO 2 -- Benchmark Commands</summary>

Connect via SSM Session Manager:

```bash
INSTANCE_ID=$(terraform output -raw ebs_instance_id)
aws ssm start-session --target $INSTANCE_ID
```

Inside the session:

```bash
# Sequential write test (throughput)
sudo fio --name=seq-write --directory=/data --size=1G \
  --rw=write --bs=1M --direct=1 --numjobs=1 --runtime=30 \
  --group_reporting

# Expected gp3 result: ~125 MB/s write throughput (baseline)

# Random read test (IOPS)
sudo fio --name=rand-read --directory=/data --size=1G \
  --rw=randread --bs=4K --direct=1 --numjobs=4 --runtime=30 \
  --group_reporting

# Expected gp3 result: ~3,000 IOPS (baseline)

# Random write test (IOPS)
sudo fio --name=rand-write --directory=/data --size=1G \
  --rw=randwrite --bs=4K --direct=1 --numjobs=4 --runtime=30 \
  --group_reporting

# Expected gp3 result: ~3,000 IOPS (baseline)
```

Compare with instance store (requires i3 instance -- higher cost):
- Sequential read: ~1.5 GB/s
- Random read IOPS: ~400,000+
- The difference is 100x+ for random I/O operations

</details>

<details>
<summary>TODO 3 -- EBS Persistence Demonstration</summary>

```bash
INSTANCE_ID=$(terraform output -raw ebs_instance_id)

# 1. Write test data
aws ssm send-command \
  --instance-ids $INSTANCE_ID \
  --document-name "AWS-RunShellScript" \
  --parameters 'commands=["echo data-survives-stop > /data/persistence.txt && cat /data/persistence.txt"]' \
  --query 'Command.CommandId' --output text

# 2. Stop the instance
aws ec2 stop-instances --instance-ids $INSTANCE_ID
aws ec2 wait instance-stopped --instance-ids $INSTANCE_ID
echo "Instance stopped"

# 3. Start the instance
aws ec2 start-instances --instance-ids $INSTANCE_ID
aws ec2 wait instance-running --instance-ids $INSTANCE_ID
echo "Instance running"

# 4. Wait for SSM agent to reconnect (30-60 seconds)
sleep 45

# 5. Remount and verify data
aws ssm send-command \
  --instance-ids $INSTANCE_ID \
  --document-name "AWS-RunShellScript" \
  --parameters 'commands=["mount /dev/xvdf /data 2>/dev/null; cat /data/persistence.txt"]' \
  --query 'Command.CommandId' --output text

# Check command output
COMMAND_ID=$(aws ssm list-commands --instance-id $INSTANCE_ID \
  --query 'Commands[0].CommandId' --output text)
aws ssm get-command-invocation \
  --command-id $COMMAND_ID \
  --instance-id $INSTANCE_ID \
  --query 'StandardOutputContent' --output text
```

Expected: `data-survives-stop` -- the file persists because EBS volumes are independent of the instance lifecycle.

</details>

---

## Verify What You Learned

```bash
# Verify EBS volume exists and is attached
aws ec2 describe-volumes \
  --filters "Name=tag:Name,Values=saa-ex35-data-volume" \
  --query "Volumes[0].{State:State,Type:VolumeType,IOPS:Iops,Size:Size,Encrypted:Encrypted}" \
  --output table
```

Expected: State=in-use, Type=gp3, IOPS=3000, Size=100, Encrypted=True

```bash
# Verify instance is running
aws ec2 describe-instances \
  --filters "Name=tag:Name,Values=saa-ex35-ebs-instance" "Name=instance-state-name,Values=running" \
  --query "Reservations[0].Instances[0].{Type:InstanceType,State:State.Name}" --output table
```

Expected: Type=m5.large, State=running

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify EBS volume is deleted:

```bash
aws ec2 describe-volumes \
  --filters "Name=tag:Name,Values=saa-ex35-data-volume" \
  --query "Volumes[*].VolumeId" --output text
```

Expected: no output.

---

## What's Next

You compared ephemeral instance store with persistent EBS storage and benchmarked their I/O characteristics. In the next exercise, you will explore **EC2 Hibernate** -- a feature that saves the in-memory state (RAM) to the encrypted root EBS volume, enabling instances to resume exactly where they left off without a full boot sequence.

---

## Summary

- **Instance store** is physically attached NVMe storage -- highest IOPS but data is **lost on stop, terminate, or hardware failure**
- **EBS volumes** are network-attached persistent storage -- survive stop/start cycles, support snapshots, and can be detached/reattached
- **Never use instance store as the sole storage for data you cannot afford to lose** (databases, application state)
- Instance store is ideal for **temporary caches, scratch buffers, and replicated data** (e.g., HDFS/Cassandra replicas)
- **gp3** is the default EBS volume type: 3,000 IOPS and 125 MB/s baseline, scalable independently of size
- **io2** provides up to 64,000 IOPS per volume (256,000 with Block Express) for database workloads
- **EBS Multi-Attach** (io2 only) allows a single volume to be attached to multiple instances in the same AZ
- Always **encrypt EBS volumes** -- no performance penalty and satisfies compliance requirements
- EBS snapshots are incremental and stored in S3 -- use them for backup and cross-region disaster recovery

---

## Reference

- [Amazon EC2 Instance Store](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/InstanceStorage.html)
- [Amazon EBS Volume Types](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ebs-volume-types.html)
- [EBS Snapshots](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/EBSSnapshots.html)
- [Terraform aws_ebs_volume](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ebs_volume)

## Additional Resources

- [EBS Performance Optimization](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ebs-io-characteristics.html) -- I/O characteristics, initialization, and pre-warming
- [EBS Multi-Attach](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ebs-volumes-multi.html) -- io2 shared volumes for clustered applications
- [fio Documentation](https://fio.readthedocs.io/en/latest/) -- flexible I/O benchmarking tool for storage testing
- [EBS Pricing](https://aws.amazon.com/ebs/pricing/) -- cost breakdown by volume type, IOPS, and throughput provisioning

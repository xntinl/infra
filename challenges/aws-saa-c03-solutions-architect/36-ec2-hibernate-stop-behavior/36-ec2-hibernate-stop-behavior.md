# 36. EC2 Hibernate and Stop Behavior

<!--
difficulty: intermediate
concepts: [ec2-hibernate, ec2-stop, ec2-terminate, ram-to-ebs, encrypted-root-volume, hibernate-prerequisites, instance-lifecycle]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: apply, analyze
prerequisites: [35-ec2-instance-store-vs-ebs]
aws_cost: ~$0.02/hr
-->

> **AWS Cost Warning:** Hibernation-capable instances (t3.micro with encrypted root volume) cost ~$0.0104/hr while running. While hibernated, you pay only for the EBS volume (~$0.004/hr for 30GB gp3). Total ~$0.02/hr during the exercise. Remember to run `terraform destroy` when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Default VPC available in target region | `aws ec2 describe-vpcs --filters Name=isDefault,Values=true` |
| Understanding of EBS volumes | Completed exercise 35 |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Explain** the difference between EC2 stop (RAM discarded, EBS persists) and hibernate (RAM saved to encrypted root EBS volume).
2. **List** the prerequisites for hibernation: encrypted root volume, supported instance types, sufficient root volume size, hibernation enabled at launch.
3. **Implement** a hibernation-capable EC2 instance using Terraform with the required configuration.
4. **Demonstrate** that hibernation preserves in-memory state by writing data to RAM, hibernating, and resuming.
5. **Analyze** when hibernate is preferable to stop/start (long initialization, cached data, process state preservation).

---

## Why This Matters

The SAA-C03 exam tests the EC2 instance lifecycle in detail, and hibernation is a frequently tested topic because it has specific prerequisites that are easy to get wrong. The exam might describe a scenario where an application takes 10 minutes to initialize (loading ML models into memory, warming caches, establishing database connection pools) and ask how to reduce startup time while controlling costs. Hibernate is the answer -- it freezes the in-memory state to the root EBS volume and restores it on resume, skipping the entire initialization sequence.

The critical prerequisite the exam tests is that the root volume must be encrypted. Without encryption, hibernation fails silently or throws an error, depending on how it is invoked. The root volume must also be large enough to store the full contents of RAM. Other constraints include supported instance families (most general-purpose and memory-optimized types), maximum RAM size (150 GB), and the requirement that hibernation must be enabled at launch time -- you cannot enable it on a running instance.

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
  default     = "saa-ex36"
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
  description = "Hibernate demo instance"

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}
```

### `compute.tf`

```hcl
# ============================================================
# TODO 1: Hibernation-Capable Instance
# ============================================================
# Launch an EC2 instance configured for hibernation.
#
# Prerequisites for hibernation:
#   1. Root EBS volume MUST be encrypted
#   2. Root volume must be large enough to hold RAM contents
#   3. Instance type must support hibernation
#   4. hibernation = true must be set at launch (cannot enable later)
#   5. Instance RAM must be <= 150 GB
#
# Requirements:
#   - Resource: aws_instance
#     - ami = data.aws_ami.al2023.id
#     - instance_type = "t3.micro" (1 GiB RAM, hibernation supported)
#     - subnet_id, iam_instance_profile, vpc_security_group_ids
#
#     - hibernation = true
#
#     - root_block_device block:
#       - volume_size = 30 (must be >= RAM size + OS)
#       - volume_type = "gp3"
#       - encrypted = true  <-- CRITICAL: without this, hibernate fails
#
#     - user_data to simulate a long-initialization application:
#       #!/bin/bash
#       # Simulate loading data into memory (cache warmup)
#       dd if=/dev/urandom of=/dev/shm/cached_data bs=1M count=100
#       echo "Application initialized at $(date)" > /tmp/init_time.txt
#
#     - tags: Name = "${var.project_name}-hibernate-instance"
#
# Docs: https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/Hibernate.html
# ============================================================


# ============================================================
# TODO 2: Hibernate and Resume the Instance
# ============================================================
# After the instance is running, test the hibernate/resume cycle.
#
# CLI commands to run:
#
# 1. Verify hibernation is configured:
#    aws ec2 describe-instances \
#      --instance-ids <ID> \
#      --query 'Reservations[0].Instances[0].HibernationOptions'
#
# 2. Check initial state (note the init time):
#    aws ssm send-command \
#      --instance-ids <ID> \
#      --document-name "AWS-RunShellScript" \
#      --parameters 'commands=["cat /tmp/init_time.txt && ls -la /dev/shm/cached_data"]'
#
# 3. Hibernate the instance:
#    aws ec2 stop-instances --instance-ids <ID> --hibernate
#    aws ec2 wait instance-stopped --instance-ids <ID>
#
# 4. Resume the instance:
#    aws ec2 start-instances --instance-ids <ID>
#    aws ec2 wait instance-running --instance-ids <ID>
#
# 5. Verify RAM state persists:
#    aws ssm send-command \
#      --instance-ids <ID> \
#      --document-name "AWS-RunShellScript" \
#      --parameters 'commands=["cat /tmp/init_time.txt && ls -la /dev/shm/cached_data && echo Resumed at $(date)"]'
#
# Expected: /tmp/init_time.txt shows the ORIGINAL init time
# (not current time), and /dev/shm/cached_data still exists.
# This proves RAM was saved and restored.
#
# Compare with a normal stop/start: /dev/shm contents would be
# gone because RAM is discarded on stop.
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

A team tries to enable hibernation on their production instances but the hibernate call fails:

```hcl
resource "aws_instance" "app_server" {
  ami           = data.aws_ami.al2023.id
  instance_type = "m5.xlarge"
  hibernation   = true

  root_block_device {
    volume_size = 20
    volume_type = "gp3"
    # Note: no 'encrypted' setting
  }

  tags = {
    Name = "production-app"
  }
}
```

<details>
<summary>Explain the bug</summary>

Two problems prevent hibernation from working:

**1. Root volume is not encrypted.** Hibernation writes the contents of RAM to the root EBS volume. Without encryption, AWS refuses to hibernate because the RAM dump would contain sensitive data (credentials, application secrets, decryption keys) stored in plaintext on disk. The `encrypted = true` attribute is mandatory.

**2. Root volume is too small.** The m5.xlarge has 16 GiB of RAM. The root volume must be large enough to hold the OS, applications, AND the full RAM contents. A 20 GB root volume has roughly 12 GB free after the OS -- not enough for a 16 GB RAM dump. The volume should be at least 36 GB (20 GB OS + 16 GB RAM).

**The fix:**

```hcl
resource "aws_instance" "app_server" {
  ami           = data.aws_ami.al2023.id
  instance_type = "m5.xlarge"
  hibernation   = true

  root_block_device {
    volume_size = 40      # 20 GB OS + 16 GB RAM + headroom
    volume_type = "gp3"
    encrypted   = true    # REQUIRED for hibernation
  }

  tags = {
    Name = "production-app"
  }
}
```

**Additional hibernation requirements to remember:**
- Hibernation must be enabled at launch (`hibernation = true`). You cannot enable it on an existing instance.
- Maximum RAM: 150 GB.
- Maximum hibernation duration: 60 days. After that, AWS starts the instance normally (cold boot).
- Supported families: Most general-purpose (M, T), compute (C), memory (R), and some others. Check the documentation for the full list.
- The instance cannot be part of an ASG (ASG will terminate hibernated instances).

</details>

---

## EC2 Instance Lifecycle Comparison

| Action | Running State | RAM | EBS Root | EBS Data Volumes | Instance Store | Billing |
|--------|--------------|-----|----------|-----------------|----------------|---------|
| **Reboot** | Stays running | Preserved | Preserved | Preserved | Preserved | Continues |
| **Stop** | Stopped | Discarded | Preserved | Preserved | Lost | EBS only |
| **Hibernate** | Stopped | Saved to root EBS | Preserved | Preserved | Lost | EBS only |
| **Start (after stop)** | Running | Empty (cold boot) | Preserved | Preserved | Fresh | Resumes |
| **Start (after hibernate)** | Running | Restored from EBS | Preserved | Preserved | Fresh | Resumes |
| **Terminate** | Terminated | Discarded | Deleted (default) | Persists (default) | Lost | Stops |

### When to Use Hibernate vs Stop

| Scenario | Hibernate | Stop | Why |
|----------|-----------|------|-----|
| ML model loaded in RAM (5 min init) | Yes | No | Avoids re-loading model on resume |
| Warm cache (Redis-like in-process cache) | Yes | No | Preserves cached data |
| Dev/test instance overnight shutdown | Either | Simpler | Cold boot is acceptable for dev |
| Long-running batch job (pause/resume) | Yes | No | Continues from where it left off |
| Instance family upgrade | No | No | Must launch new instance type |
| Database server | No | Stop is fine | DB manages its own persistence on EBS |

---

## Solutions

<details>
<summary>TODO 1 -- Hibernation-Capable Instance -- `compute.tf`</summary>

```hcl
resource "aws_instance" "hibernate_demo" {
  ami                    = data.aws_ami.al2023.id
  instance_type          = "t3.micro"
  subnet_id              = data.aws_subnets.default.ids[0]
  iam_instance_profile   = aws_iam_instance_profile.ec2.name
  vpc_security_group_ids = [aws_security_group.instance.id]

  hibernation = true

  root_block_device {
    volume_size = 30
    volume_type = "gp3"
    encrypted   = true
  }

  user_data = base64encode(<<-EOF
    #!/bin/bash
    # Simulate application that takes time to initialize
    # Write 100MB of random data to shared memory (RAM-backed tmpfs)
    dd if=/dev/urandom of=/dev/shm/cached_data bs=1M count=100 2>/dev/null
    echo "Application initialized at $(date)" > /tmp/init_time.txt
    echo "Cache loaded: $(ls -lh /dev/shm/cached_data)" >> /tmp/init_time.txt
  EOF
  )

  tags = {
    Name = "${var.project_name}-hibernate-instance"
  }
}

output "hibernate_instance_id" {
  value = aws_instance.hibernate_demo.id
}
```

Configuration rationale:
- `hibernation = true`: must be set at launch. Cannot be changed after creation.
- `encrypted = true` on root volume: mandatory prerequisite. RAM contents contain sensitive data.
- `volume_size = 30`: t3.micro has 1 GiB RAM. 30 GB is well beyond the minimum but reasonable for an OS + applications + RAM dump.
- `user_data` simulates an application that loads data into shared memory during initialization. After hibernation and resume, this data should still be present in `/dev/shm`.

</details>

<details>
<summary>TODO 2 -- Hibernate and Resume Commands</summary>

```bash
INSTANCE_ID=$(terraform output -raw hibernate_instance_id)

# 1. Verify hibernation is enabled
aws ec2 describe-instances \
  --instance-ids $INSTANCE_ID \
  --query 'Reservations[0].Instances[0].HibernationOptions' \
  --output json
# Expected: {"Configured": true}

# 2. Check initial state
aws ssm send-command \
  --instance-ids $INSTANCE_ID \
  --document-name "AWS-RunShellScript" \
  --parameters 'commands=["cat /tmp/init_time.txt"]' \
  --query 'Command.CommandId' --output text

# Wait for command, then get output
sleep 5
CMD_ID=$(aws ssm list-commands --instance-id $INSTANCE_ID \
  --query 'Commands[0].CommandId' --output text)
aws ssm get-command-invocation \
  --command-id $CMD_ID --instance-id $INSTANCE_ID \
  --query 'StandardOutputContent' --output text
# Note the initialization timestamp

# 3. Hibernate the instance
echo "Hibernating instance..."
aws ec2 stop-instances --instance-ids $INSTANCE_ID --hibernate
aws ec2 wait instance-stopped --instance-ids $INSTANCE_ID
echo "Instance hibernated"

# 4. Verify instance is in 'stopped' state (billed only for EBS)
aws ec2 describe-instances \
  --instance-ids $INSTANCE_ID \
  --query 'Reservations[0].Instances[0].State.Name' --output text
# Expected: stopped

# 5. Resume the instance
echo "Resuming instance..."
aws ec2 start-instances --instance-ids $INSTANCE_ID
aws ec2 wait instance-running --instance-ids $INSTANCE_ID
echo "Instance resumed"

# Wait for SSM agent to reconnect
sleep 30

# 6. Verify RAM state persisted
aws ssm send-command \
  --instance-ids $INSTANCE_ID \
  --document-name "AWS-RunShellScript" \
  --parameters 'commands=["echo --- Original init time: && cat /tmp/init_time.txt && echo --- RAM cached data: && ls -lh /dev/shm/cached_data && echo --- Current time: $(date)"]' \
  --query 'Command.CommandId' --output text

sleep 5
CMD_ID=$(aws ssm list-commands --instance-id $INSTANCE_ID \
  --query 'Commands[0].CommandId' --output text)
aws ssm get-command-invocation \
  --command-id $CMD_ID --instance-id $INSTANCE_ID \
  --query 'StandardOutputContent' --output text
```

Expected output shows:
- The original initialization timestamp (NOT the current time) -- proving the user data script did not re-run
- The `/dev/shm/cached_data` file still exists with its 100MB of data -- proving RAM was saved and restored
- The current time is later than the init time -- proving the instance was indeed stopped and restarted

</details>

---

## Verify What You Learned

```bash
INSTANCE_ID=$(terraform output -raw hibernate_instance_id)

# Verify hibernation is configured
aws ec2 describe-instances \
  --instance-ids $INSTANCE_ID \
  --query "Reservations[0].Instances[0].HibernationOptions.Configured" \
  --output text
```

Expected: `True`

```bash
# Verify root volume is encrypted
aws ec2 describe-instances \
  --instance-ids $INSTANCE_ID \
  --query "Reservations[0].Instances[0].BlockDeviceMappings[0].Ebs.VolumeId" \
  --output text | xargs -I {} aws ec2 describe-volumes --volume-ids {} \
  --query "Volumes[0].Encrypted" --output text
```

Expected: `True`

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify cleanup:

```bash
aws ec2 describe-instances \
  --filters "Name=tag:Name,Values=saa-ex36-hibernate-instance" "Name=instance-state-name,Values=running,stopped" \
  --query "Reservations[*].Instances[*].InstanceId" --output text
```

Expected: no output.

---

## What's Next

You explored how hibernation preserves in-memory state across stop/start cycles. In the next exercise, you will work with **EC2 User Data and Launch Templates** -- automating instance configuration at boot time and managing template versions for consistent, repeatable deployments.

---

## Summary

- **EC2 Stop** discards RAM and preserves EBS volumes -- the instance cold boots on start
- **EC2 Hibernate** saves RAM contents to the encrypted root EBS volume and restores them on start -- the instance resumes exactly where it left off
- Hibernation requires **encrypted root volume**, sufficient root volume size, a supported instance type, and must be **enabled at launch**
- Maximum RAM for hibernation is **150 GB**, and maximum hibernation duration is **60 days**
- While hibernated, you pay only for **EBS storage** (no compute charges)
- Instance store data is **always lost** on both stop and hibernate
- Hibernation is ideal for applications with **long initialization times** (ML model loading, cache warming, complex startup sequences)
- User data scripts do **not re-run** after hibernate/resume -- the instance restores from the frozen state
- Hibernation is **not compatible with Auto Scaling Groups** (ASG may terminate hibernated instances)

---

## Reference

- [Hibernate Your EC2 Instance](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/Hibernate.html)
- [Hibernation Prerequisites](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/hibernating-prerequisites.html)
- [EC2 Instance Lifecycle](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-instance-lifecycle.html)
- [Terraform aws_instance hibernation](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/instance#hibernation)

## Additional Resources

- [Supported Instance Types for Hibernation](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/hibernating-prerequisites.html#hibernation-prereqs-supported-instance-families) -- full list of hibernation-capable instance families
- [Troubleshooting Hibernation](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/troubleshoot-hibernate.html) -- common failures and their solutions
- [EBS Encryption](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/EBSEncryption.html) -- how KMS encryption works with EBS volumes
- [EC2 Instance Store vs EBS](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/InstanceStorage.html) -- data persistence behavior across lifecycle events

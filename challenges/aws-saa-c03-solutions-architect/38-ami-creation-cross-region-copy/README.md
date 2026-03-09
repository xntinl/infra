# 38. AMI Creation and Cross-Region Copy

<!--
difficulty: intermediate
concepts: [ami-creation, ebs-snapshot, cross-region-ami-copy, ami-encryption, ami-sharing, golden-image, ami-lifecycle]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: apply, evaluate
prerequisites: [37-ec2-user-data-launch-templates]
aws_cost: ~$0.02/hr
-->

> **AWS Cost Warning:** AMI creation is free but storage costs apply: EBS snapshots backing the AMI cost ~$0.05/GB-month. A 30GB AMI snapshot is ~$0.0021/hr. Cross-region copy doubles storage cost. A t3.micro instance costs ~$0.0104/hr. Total ~$0.02/hr. Remember to deregister AMIs and delete snapshots when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Default VPC available in target region | `aws ec2 describe-vpcs --filters Name=isDefault,Values=true` |
| Understanding of user data and launch templates | Completed exercise 37 |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Create** an AMI from a running EC2 instance and explain the snapshot-based backing process.
2. **Copy** an AMI to a different region for cross-region disaster recovery or multi-region deployment.
3. **Explain** why AMIs with encrypted snapshots cannot be shared cross-account without re-encrypting with the target account's KMS key.
4. **Implement** an AMI creation and cross-region copy workflow using Terraform and AWS CLI.
5. **Evaluate** the trade-off between custom AMIs (fast boot, stale packages) and user data (slower boot, fresh packages).

---

## Why This Matters

AMIs are the foundation of repeatable EC2 deployments. The SAA-C03 exam tests AMI concepts in several patterns: cross-region disaster recovery ("How do you launch the same application in another region?"), golden image pipelines ("How do you ensure all instances use the same pre-configured image?"), and encryption constraints ("Can you share an encrypted AMI cross-account?").

The cross-account sharing restriction with encrypted AMIs is a particularly important exam topic. When you create an AMI with an encrypted snapshot using your AWS-managed KMS key (aws/ebs), that key cannot be shared with another account. The target account cannot decrypt the snapshot, so the AMI is unusable. To share, you must re-encrypt the snapshot with a customer-managed KMS key (CMK) and grant the target account access to that key. This is a common exam trap: the question describes cross-account AMI sharing that "doesn't work," and the answer is KMS key permissions.

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
  region = "us-east-1"
  alias  = "source"
}

provider "aws" {
  region = "us-west-2"
  alias  = "target"
}
```

### `variables.tf`

```hcl
variable "project_name" {
  description = "Project name for resource naming and tagging"
  type        = string
  default     = "saa-ex38"
}
```

### `main.tf`

```hcl
data "aws_vpc" "default" {
  provider = aws.source
  default  = true
}

data "aws_subnets" "default" {
  provider = aws.source
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
  provider    = aws.source
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
  provider = aws.source
  name     = "${var.project_name}-ec2-role"

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
  provider   = aws.source
  role       = aws_iam_role.ec2.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_instance_profile" "ec2" {
  provider = aws.source
  name     = "${var.project_name}-ec2-profile"
  role     = aws_iam_role.ec2.name
}
```

### `security.tf`

```hcl
resource "aws_security_group" "instance" {
  provider    = aws.source
  name_prefix = "${var.project_name}-"
  vpc_id      = data.aws_vpc.default.id
  description = "AMI demo instance"

  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

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
# ------------------------------------------------------------------
# Source instance: configure, then create AMI from it.
# ------------------------------------------------------------------
resource "aws_instance" "source" {
  provider               = aws.source
  ami                    = data.aws_ami.al2023.id
  instance_type          = "t3.micro"
  subnet_id              = data.aws_subnets.default.ids[0]
  iam_instance_profile   = aws_iam_instance_profile.ec2.name
  vpc_security_group_ids = [aws_security_group.instance.id]

  user_data = base64encode(<<-EOF
    #!/bin/bash
    set -e
    yum update -y
    yum install -y nginx
    echo "<h1>Golden Image v1.0</h1><p>Built $(date)</p>" > /usr/share/nginx/html/index.html
    systemctl enable nginx
    systemctl start nginx
    echo "CONFIGURED" > /var/log/ami-ready.txt
  EOF
  )

  tags = {
    Name = "${var.project_name}-source-instance"
  }
}

# ============================================================
# TODO 1: Create an AMI from the Source Instance
# ============================================================
# Create an AMI from the running source instance. This captures
# the root volume (and any additional EBS volumes) as snapshots.
#
# Requirements:
#   - Resource: aws_ami_from_instance
#     - provider = aws.source
#     - name = "${var.project_name}-golden-image-v1"
#     - source_instance_id = source instance id
#     - snapshot_without_reboot = true
#       (avoids downtime; false would stop the instance for
#        a consistent snapshot but causes brief outage)
#     - tags: Name, Version = "1.0"
#
# Key concepts:
#   - AMI = metadata + pointers to EBS snapshots
#   - Each EBS volume attached to the instance becomes a snapshot
#   - The AMI is region-specific (us-east-1 in this case)
#   - snapshot_without_reboot: faster but filesystem might be
#     inconsistent if writes are in progress. For production,
#     set to false or quiesce the filesystem first.
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ami_from_instance
# ============================================================


# ============================================================
# TODO 2: Copy AMI to Target Region
# ============================================================
# Copy the AMI from us-east-1 to us-west-2 for cross-region
# disaster recovery.
#
# Requirements:
#   - Resource: aws_ami_copy
#     - provider = aws.target (us-west-2)
#     - name = "${var.project_name}-golden-image-v1-usw2"
#     - source_ami_id = AMI ID from TODO 1
#     - source_ami_region = "us-east-1"
#     - encrypted = true (re-encrypt in target region)
#     - tags: Name, SourceRegion = "us-east-1"
#
# Key concepts:
#   - AMI copy creates new snapshots in the target region
#   - You can encrypt during copy (even if source was unencrypted)
#   - The copy is independent -- deleting the source AMI does
#     not affect the copy
#   - Cross-region copy takes time (proportional to snapshot size)
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ami_copy
# ============================================================


# ============================================================
# TODO 3: Launch Instance from Cross-Region AMI
# ============================================================
# Launch an instance in us-west-2 using the copied AMI.
# This simulates a DR scenario where you deploy to a backup region.
#
# Requirements:
#   - Resource: aws_instance
#     - provider = aws.target
#     - ami = copied AMI ID from TODO 2
#     - instance_type = "t3.micro"
#     - tags: Name = "${var.project_name}-dr-instance"
#
# The instance should boot with nginx already installed and
# configured (from the golden image), without needing user data.
#
# This demonstrates the advantage of AMIs over user data:
#   - AMI: boots in ~30 seconds, already configured
#   - User data: boots in 2-3 minutes (yum update + install)
# ============================================================
```

### `outputs.tf`

```hcl
output "source_instance_id" {
  value = aws_instance.source.id
}

output "source_instance_ip" {
  value = aws_instance.source.public_ip
}
```

---

## Spot the Bug

A team creates an encrypted AMI and tries to share it with a partner AWS account:

```bash
# Create AMI with default encryption (aws/ebs key)
aws ec2 create-image \
  --instance-id i-0abc123def456 \
  --name "shared-app-image" \
  --no-reboot

# Share with partner account 111122223333
aws ec2 modify-image-attribute \
  --image-id ami-0abc123def456 \
  --launch-permission "Add=[{UserId=111122223333}]"
```

The partner account tries to launch from the shared AMI and gets `InvalidAMI.NotFound` or a KMS decryption error.

<details>
<summary>Explain the bug</summary>

**The AMI's EBS snapshot is encrypted with the default AWS-managed KMS key (`aws/ebs`).** AWS-managed keys are per-account and per-region -- they cannot be shared with other accounts. When account 111122223333 tries to launch from the AMI, it cannot decrypt the snapshot because it has no access to the source account's `aws/ebs` key.

**The fix: Re-encrypt with a customer-managed KMS key (CMK) and share both the key and the AMI.**

```bash
# Step 1: Create a customer-managed KMS key
KEY_ID=$(aws kms create-key --description "Shared AMI key" \
  --query 'KeyMetadata.KeyId' --output text)

# Step 2: Grant partner account access to the KMS key
aws kms create-grant \
  --key-id $KEY_ID \
  --grantee-principal arn:aws:iam::111122223333:root \
  --operations Decrypt DescribeKey CreateGrant ReEncryptFrom ReEncryptTo

# Step 3: Copy the AMI and re-encrypt with the CMK
NEW_AMI=$(aws ec2 copy-image \
  --source-image-id ami-0abc123def456 \
  --source-region us-east-1 \
  --name "shared-app-image-cmk" \
  --encrypted \
  --kms-key-id $KEY_ID \
  --query 'ImageId' --output text)

# Step 4: Share the re-encrypted AMI
aws ec2 modify-image-attribute \
  --image-id $NEW_AMI \
  --launch-permission "Add=[{UserId=111122223333}]"
```

**For the SAA-C03 exam, remember this chain:**
1. Encrypted AMI shared cross-account requires a **customer-managed KMS key** (not aws/ebs)
2. The target account needs **KMS key permissions** (Decrypt, DescribeKey)
3. The target account needs **AMI launch permission**
4. Both the KMS key policy and AMI launch permission must be configured

Unencrypted AMIs can be shared cross-account without KMS concerns, but AWS best practices recommend encrypting all EBS volumes.

</details>

---

## AMI Strategy Comparison

| Approach | Boot Time | Package Freshness | Maintenance | Best For |
|----------|-----------|-------------------|-------------|----------|
| **Base AMI + User Data** | 2-5 min | Fresh (installed at boot) | Low (update script) | Dev/test, infrequent changes |
| **Golden AMI** | 30-60 sec | Stale (baked at build) | High (rebuild pipeline) | Production, fast scaling |
| **Hybrid (Golden + User Data)** | 1-2 min | Mostly fresh | Medium | Balance of speed and freshness |
| **Container (ECS/EKS)** | 10-30 sec | Fresh (pull at deploy) | Low (update image tag) | Microservices, CI/CD |

### AMI Lifecycle Decision Framework

```
Do you need instances to boot in < 60 seconds?
+-- Yes -> Golden AMI (pre-bake everything)
|   +-- Use EC2 Image Builder for automated pipeline
+-- No -> Is your configuration complex (10+ packages, custom config)?
    +-- Yes -> Golden AMI + minimal user data for dynamic config
    +-- No -> Base AMI + user data (simpler, fewer AMIs to manage)
```

---

## Solutions

<details>
<summary>TODO 1 -- Create AMI from Source Instance -- `compute.tf`</summary>

```hcl
resource "aws_ami_from_instance" "golden" {
  provider           = aws.source
  name               = "${var.project_name}-golden-image-v1"
  source_instance_id = aws_instance.source.id
  snapshot_without_reboot = true

  tags = {
    Name    = "${var.project_name}-golden-image-v1"
    Version = "1.0"
    Source  = aws_instance.source.id
  }
}

output "ami_id" {
  value = aws_ami_from_instance.golden.id
}
```

Design decisions:
- `snapshot_without_reboot = true`: the instance continues running during AMI creation. The trade-off is a potentially inconsistent filesystem snapshot if writes are happening. For a web server with static content, this is acceptable.
- For databases or applications with active writes, set `snapshot_without_reboot = false` or use application-level quiescence (flush writes, freeze filesystem) before creating the AMI.

</details>

<details>
<summary>TODO 2 -- Copy AMI to Target Region -- `compute.tf`</summary>

```hcl
resource "aws_ami_copy" "cross_region" {
  provider          = aws.target
  name              = "${var.project_name}-golden-image-v1-usw2"
  source_ami_id     = aws_ami_from_instance.golden.id
  source_ami_region = "us-east-1"
  encrypted         = true

  tags = {
    Name         = "${var.project_name}-golden-image-v1-usw2"
    SourceRegion = "us-east-1"
    SourceAMI    = aws_ami_from_instance.golden.id
  }
}

output "cross_region_ami_id" {
  value = aws_ami_copy.cross_region.id
}
```

Key points:
- `encrypted = true` encrypts the snapshot in the target region with the default KMS key. This is recommended even if the source was unencrypted.
- The copy is fully independent. Deleting the source AMI does not affect the copy.
- Copy time depends on snapshot size. A 30 GB snapshot typically takes 5-15 minutes.

</details>

<details>
<summary>TODO 3 -- Launch Instance from Cross-Region AMI -- `compute.tf`</summary>

```hcl
data "aws_vpc" "target_default" {
  provider = aws.target
  default  = true
}

data "aws_subnets" "target_default" {
  provider = aws.target
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.target_default.id]
  }
  filter {
    name   = "default-for-az"
    values = ["true"]
  }
}

resource "aws_security_group" "target_instance" {
  provider    = aws.target
  name_prefix = "${var.project_name}-dr-"
  vpc_id      = data.aws_vpc.target_default.id
  description = "DR instance in target region"

  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_instance" "dr" {
  provider               = aws.target
  ami                    = aws_ami_copy.cross_region.id
  instance_type          = "t3.micro"
  subnet_id              = data.aws_subnets.target_default.ids[0]
  vpc_security_group_ids = [aws_security_group.target_instance.id]

  # No user data needed -- nginx is already installed in the AMI
  tags = {
    Name = "${var.project_name}-dr-instance"
  }
}

output "dr_instance_ip" {
  value = aws_instance.dr.public_ip
}
```

The DR instance boots with nginx already installed and configured because it was baked into the AMI. No user data is needed, and the instance is ready to serve traffic in ~30 seconds instead of 2-3 minutes.

</details>

---

## Verify What You Learned

```bash
# Verify source AMI exists in us-east-1
aws ec2 describe-images --region us-east-1 \
  --filters "Name=name,Values=saa-ex38-golden-image-v1" \
  --query "Images[0].{ID:ImageId,State:State,Name:Name}" --output table
```

Expected: State=available

```bash
# Verify copied AMI exists in us-west-2
aws ec2 describe-images --region us-west-2 \
  --filters "Name=name,Values=saa-ex38-golden-image-v1-usw2" \
  --query "Images[0].{ID:ImageId,State:State,Encrypted:BlockDeviceMappings[0].Ebs.Encrypted}" --output table
```

Expected: State=available, Encrypted=True

```bash
# Verify DR instance is running with the copied AMI
aws ec2 describe-instances --region us-west-2 \
  --filters "Name=tag:Name,Values=saa-ex38-dr-instance" "Name=instance-state-name,Values=running" \
  --query "Reservations[0].Instances[0].{Type:InstanceType,AMI:ImageId}" --output table
```

Expected: AMI matches the copied AMI ID.

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

---

## Cleanup

Destroy all resources including the cross-region AMI and instances:

```bash
terraform destroy -auto-approve
```

Verify AMIs are deregistered and snapshots deleted:

```bash
aws ec2 describe-images --region us-east-1 \
  --filters "Name=name,Values=saa-ex38-*" \
  --query "Images[*].ImageId" --output text
```

Expected: no output.

```bash
aws ec2 describe-images --region us-west-2 \
  --filters "Name=name,Values=saa-ex38-*" \
  --query "Images[*].ImageId" --output text
```

Expected: no output.

---

## What's Next

You created golden AMIs and copied them cross-region for disaster recovery. In the next exercise, you will explore **Dedicated Hosts vs Dedicated Instances** -- understanding the differences in hardware isolation, licensing compliance (BYOL), and cost models for regulatory and compliance workloads.

---

## Summary

- An **AMI** is metadata plus pointers to EBS snapshots -- it is a blueprint for launching identical instances
- **AMIs are region-specific** -- you must copy them to other regions for multi-region deployment or DR
- `snapshot_without_reboot = true` avoids downtime but may capture inconsistent writes; use `false` for databases
- **AMIs with encrypted snapshots** using the default `aws/ebs` key cannot be shared cross-account -- re-encrypt with a customer-managed KMS key
- Cross-account AMI sharing requires both **launch permission** on the AMI and **KMS key access** for encrypted snapshots
- **Golden AMIs** boot in 30-60 seconds (pre-baked) vs user data which takes 2-5 minutes (installed at boot)
- Use **EC2 Image Builder** to automate golden AMI pipelines (build, test, distribute across regions/accounts)
- The AMI copy is independent -- deleting the source AMI does not affect copies in other regions
- Always **deregister AMIs** and **delete backing snapshots** during cleanup to avoid ongoing storage costs

---

## Reference

- [Amazon Machine Images (AMIs)](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/AMIs.html)
- [Copy an AMI](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/CopyingAMIs.html)
- [Share an AMI](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/sharing-amis.html)
- [Terraform aws_ami_from_instance](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ami_from_instance)

## Additional Resources

- [EC2 Image Builder](https://docs.aws.amazon.com/imagebuilder/latest/userguide/what-is-image-builder.html) -- automated AMI pipeline with testing and multi-region distribution
- [AMI Encryption and Sharing](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/sharing-amis.html#sharing-amis-with-encryption) -- detailed KMS key requirements for cross-account sharing
- [AMI Lifecycle Management](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ami-manage.html) -- deprecation, deregistration, and cleanup
- [EBS Snapshot Pricing](https://aws.amazon.com/ebs/pricing/) -- incremental snapshot storage costs

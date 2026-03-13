# 31. EC2 Instance Types and Right-Sizing

<!--
difficulty: basic
concepts: [ec2-instance-families, general-purpose, compute-optimized, memory-optimized, storage-optimized, right-sizing, cloudwatch-metrics, cost-optimization]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: understand
prerequisites: [none]
aws_cost: ~$0.05/hr
-->

> **AWS Cost Warning:** A t3.micro instance costs ~$0.0104/hr and an m5.large costs ~$0.096/hr. Running both for 35 minutes totals ~$0.06. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Default VPC available in your target region
- Basic understanding of CPU and memory concepts

## Learning Objectives

After completing this exercise, you will be able to:

- **Identify** the four main EC2 instance families (General Purpose, Compute Optimized, Memory Optimized, Storage Optimized) and their target workloads
- **Explain** the naming convention for instance types (e.g., `m5.large` = family + generation + size)
- **Compare** the vCPU-to-memory ratios across instance families and match them to workload profiles
- **Construct** a Terraform configuration that launches instances from different families for side-by-side comparison
- **Interpret** CloudWatch CPU and memory metrics to determine whether an instance is right-sized
- **Describe** the cost implications of choosing the wrong instance family for a given workload

## Why EC2 Instance Types Matter

Instance type selection is one of the most impactful cost and performance decisions you make on AWS. The SAA-C03 exam tests whether you can match workloads to the right instance family. A web server running a general-purpose application on a c5.2xlarge (compute-optimized) wastes money on CPU capacity it does not need. A machine learning training job on a t3.large wastes time because it needs the raw compute throughput that a c5 or p3 instance provides.

The four families you must know for the exam cover distinct workload profiles. General Purpose instances (t3, m5) provide a balanced ratio of CPU, memory, and networking -- they are the default choice when you do not have a specific bottleneck. Compute Optimized instances (c5) have a higher vCPU-to-memory ratio for CPU-bound workloads like batch processing, encoding, or scientific modeling. Memory Optimized instances (r5, x1) have a higher memory-to-vCPU ratio for in-memory databases, caching, and real-time analytics. Storage Optimized instances (i3, d2) provide high sequential read/write access to large local datasets via NVMe instance store volumes.

Right-sizing means continuously monitoring your instances and adjusting the type to match actual usage. AWS reports that the average EC2 instance runs at 10-15% CPU utilization -- meaning most customers are over-provisioned by 5-10x. Right-sizing is the single highest-impact cost optimization you can make, typically saving 30-50% of EC2 spend.

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
  default     = "saa-ex31"
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

# ------------------------------------------------------------------
# IAM role for SSM access (allows connecting without SSH keys)
# and CloudWatch agent (publishes memory metrics).
# ------------------------------------------------------------------
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

resource "aws_iam_role_policy_attachment" "cloudwatch" {
  role       = aws_iam_role.ec2.name
  policy_arn = "arn:aws:iam::aws:policy/CloudWatchAgentServerPolicy"
}

resource "aws_iam_instance_profile" "ec2" {
  name = "${var.project_name}-ec2-profile"
  role = aws_iam_role.ec2.name
}

# ------------------------------------------------------------------
# Security group: outbound only (no inbound needed for SSM access).
# ------------------------------------------------------------------
resource "aws_security_group" "instance" {
  name_prefix = "${var.project_name}-"
  vpc_id      = data.aws_vpc.default.id
  description = "EC2 instance - outbound only"

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

# ------------------------------------------------------------------
# General Purpose: t3.micro
#
# The t3 family uses burstable CPU credits. The micro size provides
# 2 vCPUs (burstable) and 1 GiB memory. Good for low-traffic web
# servers, dev environments, and small databases.
#
# vCPU:Memory ratio = 2:1 (burstable)
# On-demand: ~$0.0104/hr ($7.49/mo)
# ------------------------------------------------------------------
resource "aws_instance" "t3_micro" {
  ami                    = data.aws_ami.al2023.id
  instance_type          = "t3.micro"
  subnet_id              = data.aws_subnets.default.ids[0]
  iam_instance_profile   = aws_iam_instance_profile.ec2.name
  vpc_security_group_ids = [aws_security_group.instance.id]
  monitoring             = true # Enables detailed (1-minute) CloudWatch monitoring

  user_data = base64encode(<<-EOF
    #!/bin/bash
    # Install and configure CloudWatch agent for memory metrics
    yum install -y amazon-cloudwatch-agent
    cat > /opt/aws/amazon-cloudwatch-agent/etc/config.json <<'CONFIG'
    {
      "metrics": {
        "metrics_collected": {
          "mem": { "measurement": ["mem_used_percent"] },
          "disk": { "measurement": ["disk_used_percent"], "resources": ["*"] }
        },
        "append_dimensions": { "InstanceId": "$${aws:InstanceId}" }
      }
    }
    CONFIG
    /opt/aws/amazon-cloudwatch-agent/bin/amazon-cloudwatch-agent-ctl \
      -a fetch-config -m ec2 -s \
      -c file:/opt/aws/amazon-cloudwatch-agent/etc/config.json
  EOF
  )

  tags = {
    Name    = "${var.project_name}-t3-micro"
    Family  = "general-purpose"
    Purpose = "right-sizing-demo"
  }
}

# ------------------------------------------------------------------
# General Purpose: m5.large (for comparison)
#
# The m5 family provides fixed-performance vCPUs (not burstable).
# The large size gives 2 vCPUs and 8 GiB memory.
#
# vCPU:Memory ratio = 1:4
# On-demand: ~$0.096/hr ($69.12/mo)
# ------------------------------------------------------------------
resource "aws_instance" "m5_large" {
  ami                    = data.aws_ami.al2023.id
  instance_type          = "m5.large"
  subnet_id              = data.aws_subnets.default.ids[0]
  iam_instance_profile   = aws_iam_instance_profile.ec2.name
  vpc_security_group_ids = [aws_security_group.instance.id]
  monitoring             = true

  user_data = base64encode(<<-EOF
    #!/bin/bash
    yum install -y amazon-cloudwatch-agent
    cat > /opt/aws/amazon-cloudwatch-agent/etc/config.json <<'CONFIG'
    {
      "metrics": {
        "metrics_collected": {
          "mem": { "measurement": ["mem_used_percent"] },
          "disk": { "measurement": ["disk_used_percent"], "resources": ["*"] }
        },
        "append_dimensions": { "InstanceId": "$${aws:InstanceId}" }
      }
    }
    CONFIG
    /opt/aws/amazon-cloudwatch-agent/bin/amazon-cloudwatch-agent-ctl \
      -a fetch-config -m ec2 -s \
      -c file:/opt/aws/amazon-cloudwatch-agent/etc/config.json
  EOF
  )

  tags = {
    Name    = "${var.project_name}-m5-large"
    Family  = "general-purpose"
    Purpose = "right-sizing-demo"
  }
}
```

### `outputs.tf`

```hcl
output "t3_micro_id" {
  description = "t3.micro instance ID"
  value       = aws_instance.t3_micro.id
}

output "m5_large_id" {
  description = "m5.large instance ID"
  value       = aws_instance.m5_large.id
}
```

## Step 2 -- Deploy and Compare Instance Specifications

```bash
terraform init
terraform apply -auto-approve
```

After deployment, compare the two instances:

```bash
# List both instances with their types and state
aws ec2 describe-instances \
  --filters "Name=tag:Purpose,Values=right-sizing-demo" "Name=instance-state-name,Values=running" \
  --query 'Reservations[*].Instances[*].{Name:Tags[?Key==`Name`]|[0].Value,Type:InstanceType,AZ:Placement.AvailabilityZone,State:State.Name}' \
  --output table
```

## Step 3 -- Understand the Instance Family Naming Convention

Every EC2 instance type follows the pattern: **family** + **generation** + **size**.

For `m5.large`:
- **m** = General Purpose (Memory balanced)
- **5** = Fifth generation hardware
- **large** = Size within the family (determines vCPUs and memory)

### Instance Family Reference Table

| Family | Category | vCPU:Memory | Example Types | Use Cases |
|--------|----------|-------------|---------------|-----------|
| **t3** | General Purpose (burstable) | 2:1 to 2:8 | t3.micro, t3.medium | Dev/test, low-traffic web, small DBs |
| **m5** | General Purpose (fixed) | 1:4 | m5.large, m5.xlarge | Web servers, app servers, mid-size DBs |
| **c5** | Compute Optimized | 1:2 | c5.large, c5.2xlarge | Batch processing, encoding, ML inference |
| **r5** | Memory Optimized | 1:8 | r5.large, r5.2xlarge | In-memory DBs, caching, real-time analytics |
| **i3** | Storage Optimized | varies | i3.large, i3.xlarge | NoSQL, data warehousing, high IOPS |
| **p3** | Accelerated Computing | varies | p3.2xlarge | ML training, GPU workloads |

### Size Progression

| Size | vCPUs (m5) | Memory (m5) | Network |
|------|-----------|-------------|---------|
| large | 2 | 8 GiB | Up to 10 Gbps |
| xlarge | 4 | 16 GiB | Up to 10 Gbps |
| 2xlarge | 8 | 32 GiB | Up to 10 Gbps |
| 4xlarge | 16 | 64 GiB | Up to 10 Gbps |
| 8xlarge | 32 | 128 GiB | 10 Gbps |
| 12xlarge | 48 | 192 GiB | 12 Gbps |
| 16xlarge | 64 | 256 GiB | 20 Gbps |
| 24xlarge | 96 | 384 GiB | 25 Gbps |

## Step 4 -- Check CloudWatch CPU Metrics

CloudWatch provides CPU utilization by default. Memory metrics require the CloudWatch agent (installed via user data above):

```bash
T3_ID=$(aws ec2 describe-instances \
  --filters "Name=tag:Name,Values=saa-ex31-t3-micro" "Name=instance-state-name,Values=running" \
  --query 'Reservations[0].Instances[0].InstanceId' --output text)

M5_ID=$(aws ec2 describe-instances \
  --filters "Name=tag:Name,Values=saa-ex31-m5-large" "Name=instance-state-name,Values=running" \
  --query 'Reservations[0].Instances[0].InstanceId' --output text)

# CPU utilization for t3.micro (last 10 minutes)
aws cloudwatch get-metric-statistics \
  --namespace AWS/EC2 \
  --metric-name CPUUtilization \
  --dimensions Name=InstanceId,Value=$T3_ID \
  --start-time $(date -u -v-10M '+%Y-%m-%dT%H:%M:%S') \
  --end-time $(date -u '+%Y-%m-%dT%H:%M:%S') \
  --period 300 --statistics Average \
  --query 'Datapoints[*].{Time:Timestamp,AvgCPU:Average}' \
  --output table

# CPU utilization for m5.large
aws cloudwatch get-metric-statistics \
  --namespace AWS/EC2 \
  --metric-name CPUUtilization \
  --dimensions Name=InstanceId,Value=$M5_ID \
  --start-time $(date -u -v-10M '+%Y-%m-%dT%H:%M:%S') \
  --end-time $(date -u '+%Y-%m-%dT%H:%M:%S') \
  --period 300 --statistics Average \
  --query 'Datapoints[*].{Time:Timestamp,AvgCPU:Average}' \
  --output table
```

Both instances should show very low CPU utilization (under 5%) since they are idle. In a real environment, you would observe usage patterns over days or weeks before making right-sizing decisions.

## Step 5 -- Cost Comparison Across Families

### Monthly Cost Comparison (us-east-1, On-Demand)

| Instance | vCPUs | Memory | Hourly | Monthly | Best For |
|----------|-------|--------|--------|---------|----------|
| t3.micro | 2 (burst) | 1 GiB | $0.0104 | $7.49 | Dev, low-traffic |
| t3.medium | 2 (burst) | 4 GiB | $0.0416 | $29.95 | Light web servers |
| m5.large | 2 | 8 GiB | $0.096 | $69.12 | Balanced workloads |
| c5.large | 2 | 4 GiB | $0.085 | $61.20 | CPU-intensive |
| r5.large | 2 | 16 GiB | $0.126 | $90.72 | Memory-intensive |
| i3.large | 2 | 15.25 GiB | $0.156 | $112.32 | High IOPS storage |

### Right-Sizing Decision Framework

```
Is CPU utilization consistently > 40%?
├── Yes → Is memory utilization < 20%?
│   ├── Yes → Move to Compute Optimized (c5)
│   └── No → Keep General Purpose (m5) or size up
└── No → Is memory utilization consistently > 60%?
    ├── Yes → Move to Memory Optimized (r5)
    └── No → Size down within the current family
```

## Common Mistakes

### 1. Using burstable instances for sustained workloads

**Wrong approach:** Running a production API on t3.large because it is the cheapest 2-vCPU option.

**What happens:** t3 instances use CPU credits. When the credit balance runs out, the instance is throttled to a baseline of 20-30% of one vCPU. Your API latency spikes dramatically and stays high until credits accumulate. The CloudWatch `CPUCreditBalance` metric drops to zero and `CPUSurplusCreditsCharged` starts accruing charges.

**Fix:** Use m5.large for sustained workloads. It costs more per hour ($0.096 vs $0.0832 for t3.large) but provides consistent, non-burstable CPU performance. Alternatively, enable t3 unlimited mode -- but then you pay for surplus credits at $0.05 per vCPU-hour, which can exceed the m5 price.

### 2. Choosing instance family based on name instead of vCPU:memory ratio

**Wrong approach:** Selecting r5.large (16 GiB, $0.126/hr) for a web server because "more memory is always better."

**What happens:** Your web server uses 2 GiB of memory. You are paying for 14 GiB you do not use. Meanwhile, the vCPU count is the same as m5.large, but you are paying 31% more.

**Fix:** Match the vCPU-to-memory ratio to your workload profile. Web servers typically need 2-4 GiB per vCPU (m5 family). Only choose r5 when your application genuinely needs 8+ GiB per vCPU (in-memory caches, analytics).

### 3. Right-sizing based on peak usage instead of sustained patterns

**Wrong approach:** Observing a 95% CPU spike during deployment and upgrading from m5.large to m5.2xlarge.

**What happens:** The spike lasted 5 minutes. The other 99.6% of the time, CPU runs at 15%. You now pay 2x for an instance that is idle most of the day.

**Fix:** Look at the p90 and p99 percentiles over at least one week. Use Auto Scaling for workloads with significant peak/trough patterns rather than sizing a single instance for peak load.

## Verify What You Learned

```bash
# Verify t3.micro is running
aws ec2 describe-instances \
  --filters "Name=tag:Name,Values=saa-ex31-t3-micro" "Name=instance-state-name,Values=running" \
  --query "Reservations[0].Instances[0].InstanceType" --output text
```

Expected: `t3.micro`

```bash
# Verify m5.large is running
aws ec2 describe-instances \
  --filters "Name=tag:Name,Values=saa-ex31-m5-large" "Name=instance-state-name,Values=running" \
  --query "Reservations[0].Instances[0].InstanceType" --output text
```

Expected: `m5.large`

```bash
# Verify detailed monitoring is enabled
aws ec2 describe-instances \
  --filters "Name=tag:Name,Values=saa-ex31-t3-micro" "Name=instance-state-name,Values=running" \
  --query "Reservations[0].Instances[0].Monitoring.State" --output text
```

Expected: `enabled`

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

Destroy all resources to stop incurring charges:

```bash
terraform destroy -auto-approve
```

Verify nothing remains:

```bash
aws ec2 describe-instances \
  --filters "Name=tag:Purpose,Values=right-sizing-demo" "Name=instance-state-name,Values=running" \
  --query "Reservations[*].Instances[*].InstanceId" --output text
```

Expected: no output (no running instances).

## What's Next

You explored how instance families map to workload profiles. In the next exercise, you will deploy **EC2 placement groups** -- Cluster, Spread, and Partition -- and learn how physical hardware placement affects latency, fault tolerance, and capacity for distributed workloads.

## Summary

- EC2 instance types follow the **family + generation + size** naming convention (e.g., m5.large)
- **General Purpose (t3/m5):** balanced CPU:memory ratio, default choice for most workloads
- **Compute Optimized (c5):** higher vCPU-to-memory ratio for CPU-bound tasks (batch, encoding, HPC)
- **Memory Optimized (r5):** higher memory-to-vCPU ratio for in-memory databases and caching
- **Storage Optimized (i3):** high IOPS NVMe instance store for data-intensive workloads
- **t3 burstable instances** use CPU credits -- they throttle under sustained load unless unlimited mode is enabled
- Right-sizing requires monitoring **both CPU and memory** over at least one week (CloudWatch agent needed for memory)
- Over-provisioning is the most common EC2 cost mistake -- average utilization across AWS is only 10-15%
- Use the vCPU:memory ratio to match instance family to workload, not the instance name

## Reference

- [Amazon EC2 Instance Types](https://aws.amazon.com/ec2/instance-types/)
- [Burstable Performance Instances](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/burstable-performance-instances.html)
- [Terraform aws_instance Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/instance)
- [CloudWatch Agent Configuration](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/Install-CloudWatch-Agent.html)

## Additional Resources

- [AWS Compute Optimizer](https://docs.aws.amazon.com/compute-optimizer/latest/ug/what-is-compute-optimizer.html) -- automated right-sizing recommendations based on CloudWatch metrics
- [EC2 Right Sizing Guide](https://docs.aws.amazon.com/cost-management/latest/userguide/ce-rightsizing.html) -- Cost Explorer integration for right-sizing analysis
- [EC2 Pricing](https://aws.amazon.com/ec2/pricing/) -- on-demand, reserved, and spot pricing across all instance types
- [Instance Type Comparison](https://instances.vantage.sh/) -- community-maintained comparison tool for all EC2 instance types

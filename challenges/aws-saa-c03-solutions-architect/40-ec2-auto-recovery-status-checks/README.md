# 40. EC2 Auto Recovery and Status Checks

<!--
difficulty: intermediate
concepts: [ec2-status-checks, system-status-check, instance-status-check, auto-recovery, cloudwatch-alarm, asg-replacement, ebs-backed-recovery]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: apply, evaluate
prerequisites: [31-ec2-instance-types-right-sizing, 35-ec2-instance-store-vs-ebs]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise uses a t3.micro instance (~$0.0104/hr) with a CloudWatch alarm (free tier includes 10 alarms). Total ~$0.01/hr. Remember to run `terraform destroy` when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Default VPC available in target region | `aws ec2 describe-vpcs --filters Name=isDefault,Values=true` |
| Understanding of instance store vs EBS | Completed exercise 35 |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Distinguish** between system status checks (AWS hardware/infrastructure) and instance status checks (OS/software problems).
2. **Implement** a CloudWatch alarm that triggers EC2 auto recovery when a system status check fails.
3. **Explain** the prerequisites for auto recovery: EBS-backed instance, supported instance type, same AZ placement.
4. **Compare** auto recovery (same instance, same IPs, same EBS volumes) with ASG replacement (new instance, potentially different IPs).
5. **Evaluate** when to use auto recovery vs ASG-based self-healing for different architecture patterns.

---

## Why This Matters

EC2 status checks are the foundation of instance health monitoring, and the SAA-C03 exam tests whether you understand the two-level check system and how to respond to failures at each level.

**System status checks** verify the underlying AWS infrastructure: hardware, hypervisor, network connectivity, and power. These are problems you cannot fix from inside the instance. When a system status check fails, the underlying hardware has a problem. Auto recovery migrates the instance to healthy hardware while preserving its instance ID, private IP, Elastic IP, EBS volumes, and instance metadata. This is the correct automated response for standalone instances (not in an ASG).

**Instance status checks** verify that the OS is reachable and responding. Failures indicate OS-level problems: kernel panic, exhausted memory, corrupted filesystem, or misconfigured networking. These require OS-level intervention (reboot, fix configuration). Auto recovery does not help here because the problem is inside the instance, not in the hardware.

The exam often presents a scenario with a standalone EC2 instance (not in an ASG) that needs automatic hardware failure recovery. The answer is a CloudWatch alarm with the `recover` action. If the instance is in an ASG, the answer is different: the ASG detects the unhealthy instance and replaces it with a new one. Understanding which mechanism applies to which architecture is a key exam skill.

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
  default     = "saa-ex40"
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
  description = "Auto recovery demo"

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
# EBS-backed instance for auto recovery.
# Auto recovery requires an EBS-backed instance because the
# recovery process migrates EBS volumes to new hardware.
# Instance store data would be lost.
# ------------------------------------------------------------------
resource "aws_instance" "recoverable" {
  ami                    = data.aws_ami.al2023.id
  instance_type          = "t3.micro"
  subnet_id              = data.aws_subnets.default.ids[0]
  iam_instance_profile   = aws_iam_instance_profile.ec2.name
  vpc_security_group_ids = [aws_security_group.instance.id]

  # Detailed monitoring needed for 1-minute status check resolution
  monitoring = true

  tags = {
    Name = "${var.project_name}-recoverable-instance"
  }
}
```

### `monitoring.tf`

```hcl
# ============================================================
# TODO 1: CloudWatch Alarm for Auto Recovery
# ============================================================
# Create a CloudWatch alarm that triggers EC2 auto recovery
# when the system status check fails.
#
# Requirements:
#   - Resource: aws_cloudwatch_metric_alarm
#     - alarm_name = "${var.project_name}-system-recovery"
#     - alarm_description = "Recover instance on system check failure"
#     - namespace = "AWS/EC2"
#     - metric_name = "StatusCheckFailed_System"
#     - comparison_operator = "GreaterThanThreshold"
#     - threshold = 0
#     - evaluation_periods = 2
#     - period = 60
#     - statistic = "Maximum"
#     - dimensions = { InstanceId = instance ID }
#     - alarm_actions = ["arn:aws:automate:${var.region}:ec2:recover"]
#
# The recover action migrates the instance to new hardware:
#   - Same instance ID
#   - Same private IP address
#   - Same Elastic IP (if assigned)
#   - Same EBS volumes (reattached)
#   - Same instance metadata
#   - Instance store data is LOST
#
# Alternative actions you could add:
#   - arn:aws:automate:${var.region}:ec2:reboot (for instance check failures)
#   - SNS topic ARN (for notifications)
#
# Docs: https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-instance-recover.html
# ============================================================


# ============================================================
# TODO 2: CloudWatch Alarm for Instance Status Check
# ============================================================
# Create a separate alarm for instance-level status check
# failures. For instance check failures, the appropriate
# action is reboot (not recover).
#
# Requirements:
#   - Resource: aws_cloudwatch_metric_alarm
#     - alarm_name = "${var.project_name}-instance-reboot"
#     - metric_name = "StatusCheckFailed_Instance"
#     - comparison_operator = "GreaterThanThreshold"
#     - threshold = 0
#     - evaluation_periods = 3  (wait longer -- some failures are transient)
#     - period = 60
#     - statistic = "Maximum"
#     - dimensions = { InstanceId = instance ID }
#     - alarm_actions = ["arn:aws:automate:${var.region}:ec2:reboot"]
#
# Why reboot and not recover for instance checks?
#   - Instance check failures are OS-level problems (kernel panic,
#     exhausted memory, network misconfiguration)
#   - Moving to new hardware (recover) won't fix an OS problem
#   - Rebooting often resolves transient OS issues
#   - If the reboot doesn't fix it, investigate manually
#
# Docs: https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/monitoring-system-instance-status-check.html
# ============================================================
```

### `outputs.tf`

```hcl
output "instance_id" {
  value = aws_instance.recoverable.id
}

output "private_ip" {
  value = aws_instance.recoverable.private_ip
}
```

---

## Spot the Bug

A team configures auto recovery for their application server, but recovery fails when triggered:

```hcl
resource "aws_instance" "app" {
  ami           = data.aws_ami.al2023.id
  instance_type = "i3.large"  # Instance-store backed

  # Ephemeral storage for application data
  ephemeral_block_device {
    device_name  = "/dev/sdb"
    virtual_name = "ephemeral0"
  }

  tags = {
    Name = "production-app-server"
  }
}

resource "aws_cloudwatch_metric_alarm" "recovery" {
  alarm_name          = "app-server-recovery"
  namespace           = "AWS/EC2"
  metric_name         = "StatusCheckFailed_System"
  comparison_operator = "GreaterThanThreshold"
  threshold           = 0
  evaluation_periods  = 2
  period              = 60
  statistic           = "Maximum"

  dimensions = {
    InstanceId = aws_instance.app.id
  }

  alarm_actions = [
    "arn:aws:automate:us-east-1:ec2:recover"
  ]
}
```

<details>
<summary>Explain the bug</summary>

**Auto recovery is not supported on instance-store backed instances.** The i3.large instance uses NVMe instance store as its primary storage. EC2 auto recovery works by migrating EBS volumes to new hardware, but instance store volumes are physically attached to the original hardware and cannot be moved.

When the system status check fails and the recovery alarm triggers, the recovery action fails with an error because:
1. The instance relies on ephemeral storage that cannot be migrated
2. Even if recovery succeeded, all data on the instance store would be lost
3. The application would come up on new hardware with empty disks -- likely broken

**The fix depends on the architecture:**

**Option 1: Use an EBS-backed instance type with auto recovery.**

```hcl
resource "aws_instance" "app" {
  ami           = data.aws_ami.al2023.id
  instance_type = "m5.large"  # EBS-backed, no instance store

  root_block_device {
    volume_size = 50
    volume_type = "gp3"
  }

  tags = {
    Name = "production-app-server"
  }
}
```

**Option 2: Use an ASG for self-healing (better for stateless apps).**

```hcl
resource "aws_autoscaling_group" "app" {
  name             = "production-app"
  min_size         = 1
  max_size         = 1
  desired_capacity = 1

  launch_template {
    id      = aws_launch_template.app.id
    version = "$Latest"
  }

  health_check_type = "EC2"
}
```

An ASG with min=max=desired=1 provides self-healing: when the instance fails, the ASG launches a replacement. The new instance gets a new ID and potentially a new IP, but for stateless applications this is acceptable and works with any instance type including instance-store backed instances.

**SAA-C03 decision:**
- Standalone instance + needs same IP/ID -> Auto recovery (requires EBS-backed)
- Stateless workload + can tolerate new IP -> ASG with min=1

</details>

---

## Status Check and Recovery Comparison

| Feature | System Status Check | Instance Status Check |
|---------|--------------------|-----------------------|
| **What it checks** | AWS infrastructure (hardware, hypervisor, network, power) | Guest OS (kernel, network config, memory, filesystem) |
| **Who fixes it** | AWS (or auto recovery) | You (reboot, fix OS config) |
| **Auto response** | `ec2:recover` (migrate to new hardware) | `ec2:reboot` (restart OS) |
| **Data preserved** | EBS: yes, Instance store: no | EBS: yes, Instance store: yes |
| **IP preserved** | Private: yes, Elastic IP: yes | Same instance, all IPs preserved |
| **Instance ID** | Preserved | Preserved |
| **Metric** | `StatusCheckFailed_System` | `StatusCheckFailed_Instance` |

### Auto Recovery vs ASG Replacement

| Feature | Auto Recovery | ASG Replacement |
|---------|--------------|-----------------|
| **Instance ID** | Preserved | New ID |
| **Private IP** | Preserved | New IP (unless ENI or target group) |
| **Elastic IP** | Preserved | Must be re-associated |
| **EBS volumes** | Reattached | New volumes (from AMI/template) |
| **Instance store** | Lost | Fresh (from AMI) |
| **Instance type** | Same | Configurable (launch template) |
| **AZ** | Same AZ | Any AZ in ASG config |
| **Speed** | Minutes | Minutes |
| **Use case** | Stateful, standalone instances | Stateless, scalable workloads |
| **Prerequisite** | EBS-backed only | Any instance type |

---

## Solutions

<details>
<summary>TODO 1 -- System Status Check Recovery Alarm -- `monitoring.tf`</summary>

```hcl
resource "aws_cloudwatch_metric_alarm" "system_recovery" {
  alarm_name          = "${var.project_name}-system-recovery"
  alarm_description   = "Trigger EC2 auto recovery on system status check failure. Migrates instance to healthy hardware preserving IPs and EBS volumes."
  namespace           = "AWS/EC2"
  metric_name         = "StatusCheckFailed_System"
  comparison_operator = "GreaterThanThreshold"
  threshold           = 0
  evaluation_periods  = 2
  period              = 60
  statistic           = "Maximum"

  dimensions = {
    InstanceId = aws_instance.recoverable.id
  }

  alarm_actions = [
    "arn:aws:automate:${var.region}:ec2:recover"
  ]
}
```

Key design decisions:
- `evaluation_periods = 2`: wait for 2 consecutive failures (2 minutes) before triggering recovery. This avoids false positives from transient checks.
- `statistic = "Maximum"`: if any check in the period fails, the metric is 1 (failed). Maximum captures this even if the average would be lower.
- The `recover` action is an AWS-managed automation -- no Lambda or custom code needed.

</details>

<details>
<summary>TODO 2 -- Instance Status Check Reboot Alarm -- `monitoring.tf`</summary>

```hcl
resource "aws_cloudwatch_metric_alarm" "instance_reboot" {
  alarm_name          = "${var.project_name}-instance-reboot"
  alarm_description   = "Reboot instance on instance status check failure. OS-level issues often resolve with a reboot."
  namespace           = "AWS/EC2"
  metric_name         = "StatusCheckFailed_Instance"
  comparison_operator = "GreaterThanThreshold"
  threshold           = 0
  evaluation_periods  = 3
  period              = 60
  statistic           = "Maximum"

  dimensions = {
    InstanceId = aws_instance.recoverable.id
  }

  alarm_actions = [
    "arn:aws:automate:${var.region}:ec2:reboot"
  ]
}
```

`evaluation_periods = 3` is intentionally longer than the system check alarm because instance-level failures can be transient (brief network glitch, temporary memory pressure). Three consecutive failures (3 minutes) provides higher confidence that a reboot is needed.

</details>

---

## Verify What You Learned

```bash
INSTANCE_ID=$(terraform output -raw instance_id)

# Verify current status checks are passing
aws ec2 describe-instance-status \
  --instance-ids $INSTANCE_ID \
  --query "InstanceStatuses[0].{System:SystemStatus.Status,Instance:InstanceStatus.Status}" \
  --output table
```

Expected: System=ok, Instance=ok

```bash
# Verify recovery alarm exists
aws cloudwatch describe-alarms \
  --alarm-names saa-ex40-system-recovery \
  --query "MetricAlarms[0].{Name:AlarmName,State:StateValue,Metric:MetricName,Actions:AlarmActions}" \
  --output table
```

Expected: State=OK (or INSUFFICIENT_DATA initially), Metric=StatusCheckFailed_System, Actions contains `ec2:recover`

```bash
# Verify reboot alarm exists
aws cloudwatch describe-alarms \
  --alarm-names saa-ex40-instance-reboot \
  --query "MetricAlarms[0].{Name:AlarmName,State:StateValue,Metric:MetricName,Actions:AlarmActions}" \
  --output table
```

Expected: State=OK, Metric=StatusCheckFailed_Instance, Actions contains `ec2:reboot`

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify alarms are deleted:

```bash
aws cloudwatch describe-alarms \
  --alarm-name-prefix saa-ex40 \
  --query "MetricAlarms[*].AlarmName" --output text
```

Expected: no output.

---

## What's Next

You configured EC2 status checks and auto recovery for standalone instances. In the next exercise, you will deploy a **Gateway Load Balancer** -- a specialized load balancer for third-party network appliances (firewalls, IDS/IPS) that uses GENEVE encapsulation to transparently inspect traffic.

---

## Summary

- EC2 has **two status check levels**: system (AWS infrastructure) and instance (guest OS)
- **System status check failure** = hardware problem -> auto recovery migrates to healthy hardware
- **Instance status check failure** = OS problem -> reboot is the appropriate automated response
- Auto recovery **preserves**: instance ID, private IP, Elastic IP, EBS volumes, metadata
- Auto recovery **requires EBS-backed instances** -- instance store backed instances cannot be recovered
- Auto recovery keeps the instance in the **same AZ** (unlike ASG which can launch in any configured AZ)
- For standalone stateful instances -> use **auto recovery** (CloudWatch alarm + ec2:recover)
- For stateless scalable workloads -> use **ASG replacement** (health check -> terminate -> launch new)
- CloudWatch status check metrics: `StatusCheckFailed_System`, `StatusCheckFailed_Instance`, `StatusCheckFailed` (both combined)
- Enable **detailed monitoring** (`monitoring = true`) for 1-minute metric resolution on status checks

---

## Reference

- [EC2 Status Checks](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/monitoring-system-instance-status-check.html)
- [EC2 Auto Recovery](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-instance-recover.html)
- [Terraform aws_cloudwatch_metric_alarm](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_metric_alarm)
- [CloudWatch EC2 Metrics](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/viewing_metrics_with_cloudwatch.html)

## Additional Resources

- [Troubleshooting Instance Status Checks](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/TroubleshootingInstances.html) -- debugging system and instance check failures
- [Scheduled Events](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/monitoring-instances-status-check_sched.html) -- AWS-initiated hardware maintenance events
- [EC2 Auto Recovery Limitations](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-instance-recover.html#requirements-for-recovery) -- full list of prerequisites and unsupported configurations
- [ASG Health Checks](https://docs.aws.amazon.com/autoscaling/ec2/userguide/ec2-auto-scaling-health-checks.html) -- EC2 vs ELB health check types for auto scaling groups

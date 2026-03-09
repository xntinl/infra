# Auto Scaling Policies Deep Dive

<!--
difficulty: intermediate
concepts: [asg-target-tracking, asg-step-scaling, asg-scheduled-scaling, launch-template, scaling-cooldown, predictive-scaling]
tools: [terraform, aws-cli]
estimated_time: 60m
bloom_level: design, justify, implement
prerequisites: [none]
aws_cost: ~$0.12/hr
-->

> **AWS Cost Warning:** ASG with 2-6 t3.micro instances (~$0.0104/hr each) + ALB (~$0.0225/hr). Total ~$0.12/hr at max capacity. Destroy resources promptly after completing the exercise.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Default VPC available in target region | `aws ec2 describe-vpcs --filters Name=isDefault,Values=true` |
| SSM Session Manager plugin installed | `session-manager-plugin --version` |
| `stress-ng` knowledge (will be installed on EC2) | N/A |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Design** auto scaling strategies that match different traffic patterns (steady-state, spiky, predictable, ML-based).
2. **Justify** the selection of target tracking vs step scaling vs scheduled scaling for a given workload profile.
3. **Implement** multiple scaling policies on a single ASG with proper cooldown configuration.
4. **Compare** the response characteristics of each scaling policy type under simulated load.
5. **Evaluate** cooldown settings and their impact on scaling oscillation and cost.

---

## Why This Matters

Auto Scaling is one of the most heavily tested topics on the SAA-C03 exam because it sits at the intersection of availability, performance, and cost optimization -- three of the four pillars the exam emphasizes. The exam does not simply ask "what is auto scaling?" It presents scenarios with specific traffic patterns and asks you to choose the right policy type. A target tracking policy that works beautifully for steady web traffic will react too slowly for flash-sale spikes, while a step scaling policy configured for spikes will over-provision during normal hours. Understanding these trade-offs is what separates a passing score from a guess.

In production, misconfigured scaling policies are one of the top causes of both outages and runaway AWS bills. A cooldown period that is too short causes flapping -- the ASG scales out, the metric drops, it scales in, the metric spikes again, and the cycle repeats. A cooldown that is too long leaves you under-provisioned during a genuine traffic surge. This exercise forces you to confront these dynamics directly by deploying real policies, generating real CPU load, and observing real scaling events in CloudWatch. You will walk away with an intuition that no multiple-choice practice test can provide.

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
  default     = "saa-ex04"
}
```

### `main.tf`

```hcl
# ---------- Data Sources ----------

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

data "aws_ami" "amazon_linux" {
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
resource "aws_iam_role" "instance" {
  name = "${var.project_name}-instance-role"

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
  role       = aws_iam_role.instance.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_instance_profile" "instance" {
  name = "${var.project_name}-instance-profile"
  role = aws_iam_role.instance.name
}
```

### `security.tf`

```hcl
resource "aws_security_group" "alb" {
  name   = "${var.project_name}-alb-sg"
  vpc_id = data.aws_vpc.default.id

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

resource "aws_security_group" "instance" {
  name   = "${var.project_name}-instance-sg"
  vpc_id = data.aws_vpc.default.id

  ingress {
    from_port       = 80
    to_port         = 80
    protocol        = "tcp"
    security_groups = [aws_security_group.alb.id]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}
```

### `alb.tf`

```hcl
resource "aws_lb" "this" {
  name               = "${var.project_name}-alb"
  internal           = false
  load_balancer_type = "application"
  security_groups    = [aws_security_group.alb.id]
  subnets            = data.aws_subnets.default.ids
}

resource "aws_lb_target_group" "this" {
  name     = "${var.project_name}-tg"
  port     = 80
  protocol = "HTTP"
  vpc_id   = data.aws_vpc.default.id

  health_check {
    path                = "/"
    healthy_threshold   = 2
    unhealthy_threshold = 3
    interval            = 15
    timeout             = 5
  }
}

resource "aws_lb_listener" "http" {
  load_balancer_arn = aws_lb.this.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.this.arn
  }
}
```

### `compute.tf`

```hcl
# ---------- Launch Template ----------

resource "aws_launch_template" "this" {
  name_prefix   = "${var.project_name}-lt-"
  image_id      = data.aws_ami.amazon_linux.id
  instance_type = "t3.micro"

  iam_instance_profile {
    name = aws_iam_instance_profile.instance.name
  }

  vpc_security_group_ids = [aws_security_group.instance.id]

  user_data = base64encode(<<-EOF
    #!/bin/bash
    yum install -y httpd stress-ng
    systemctl enable httpd
    systemctl start httpd
    echo "<h1>Instance $(hostname)</h1>" > /var/www/html/index.html
  EOF
  )

  tag_specifications {
    resource_type = "instance"
    tags = {
      Name = "${var.project_name}-asg-instance"
    }
  }
}

# ---------- Auto Scaling Group ----------

resource "aws_autoscaling_group" "this" {
  name                = "${var.project_name}-asg"
  desired_capacity    = 2
  min_size            = 2
  max_size            = 6
  vpc_zone_identifier = data.aws_subnets.default.ids
  target_group_arns   = [aws_lb_target_group.this.arn]
  health_check_type   = "ELB"

  launch_template {
    id      = aws_launch_template.this.id
    version = "$Latest"
  }

  tag {
    key                 = "Name"
    value               = "${var.project_name}-asg-instance"
    propagate_at_launch = true
  }
}

# ============================================================
# TODO 1: Target Tracking Scaling Policy
# ============================================================
# Create a target tracking scaling policy that maintains average
# CPU utilization at 60% across the ASG.
#
# Requirements:
#   - Resource: aws_autoscaling_policy
#   - policy_type = "TargetTrackingScaling"
#   - target_tracking_configuration block with:
#     - predefined_metric_specification using ASGAverageCPUUtilization
#     - target_value = 60
#   - Associate with the ASG above
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/autoscaling_policy
# ============================================================


# ============================================================
# TODO 2: Step Scaling Policy
# ============================================================
# Create a step scaling policy that adds instances in tiers:
#   - When CPU > 80%: add 2 instances
#   - When CPU > 95%: add 4 instances
#
# This requires BOTH:
#   a) A CloudWatch alarm that triggers on CPUUtilization > 80%
#   b) A step scaling policy with two step_adjustment blocks
#
# Requirements:
#   - Resource: aws_autoscaling_policy (policy_type = "StepScaling")
#   - adjustment_type = "ChangeInCapacity"
#   - step_adjustment blocks:
#     - metric_interval_lower_bound = 0, upper = 15, scaling_adjustment = 2
#     - metric_interval_lower_bound = 15, scaling_adjustment = 4
#   - Resource: aws_cloudwatch_metric_alarm
#     - metric_name = "CPUUtilization"
#     - namespace = "AWS/EC2"
#     - statistic = "Average"
#     - threshold = 80
#     - alarm_actions = [policy ARN]
#     - dimensions = { AutoScalingGroupName = ASG name }
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/autoscaling_policy
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_metric_alarm
# ============================================================


# ============================================================
# TODO 3: Scheduled Scaling Actions
# ============================================================
# Create two scheduled actions:
#   a) Scale to min=4, max=6 at 8:00 AM UTC every weekday
#   b) Scale to min=2, max=4 at 8:00 PM UTC every weekday
#
# Requirements:
#   - Resource: aws_autoscaling_schedule (two of them)
#   - recurrence uses cron format: "0 8 * * MON-FRI"
#   - Set min_size, max_size, desired_capacity for each
#   - Associate with the ASG
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/autoscaling_schedule
# ============================================================


# ============================================================
# TODO 4: Cooldown Configuration
# ============================================================
# Configure scaling cooldowns to prevent flapping:
#   - Default cooldown on the ASG: 300 seconds
#   - Scale-in cooldown on the target tracking policy: 60 seconds
#
# Requirements:
#   - Add default_cooldown = 300 to the aws_autoscaling_group
#   - In the target tracking policy from TODO 1, set:
#     target_tracking_configuration {
#       disable_scale_in = false
#     }
#   - Add a separate scale-in policy or modify the target tracking
#     policy to use estimated_instance_warmup = 120
#
# Note: Target tracking policies manage their own scale-in.
#       The estimated_instance_warmup tells the policy to ignore
#       metrics from instances younger than this value.
#
# Docs: https://docs.aws.amazon.com/autoscaling/ec2/userguide/ec2-auto-scaling-scaling-cooldowns.html
# ============================================================


# ============================================================
# TODO 5: CloudWatch Alarm for GroupInServiceInstances
# ============================================================
# Create a CloudWatch alarm that fires when the number of
# healthy instances drops below the desired minimum.
#
# Requirements:
#   - Resource: aws_cloudwatch_metric_alarm
#   - metric_name = "GroupInServiceInstances"
#   - namespace = "AWS/AutoScaling"
#   - comparison_operator = "LessThanThreshold"
#   - threshold = 2 (our minimum)
#   - evaluation_periods = 2
#   - period = 60
#   - statistic = "Average"
#   - dimensions = { AutoScalingGroupName = ASG name }
#   - alarm_description with meaningful context
#
# Docs: https://docs.aws.amazon.com/autoscaling/ec2/userguide/ec2-auto-scaling-cloudwatch-monitoring.html
# ============================================================
```

### `outputs.tf`

```hcl
output "alb_dns" {
  value = aws_lb.this.dns_name
}

output "asg_name" {
  value = aws_autoscaling_group.this.name
}
```

---

## Spot the Bug

The following step scaling policy has a subtle but dangerous flaw. Read it carefully and identify the problem before expanding the answer.

```hcl
resource "aws_autoscaling_policy" "scale_down" {
  name                   = "scale-down-on-low-cpu"
  autoscaling_group_name = aws_autoscaling_group.this.name
  policy_type            = "StepScaling"
  adjustment_type        = "ExactCapacity"

  step_adjustment {
    metric_interval_upper_bound = 0
    scaling_adjustment          = 2
  }
}

resource "aws_cloudwatch_metric_alarm" "low_cpu" {
  alarm_name          = "low-cpu-alarm"
  comparison_operator = "LessThanThreshold"
  evaluation_periods  = 3
  metric_name         = "CPUUtilization"
  namespace           = "AWS/EC2"
  period              = 60
  statistic           = "Average"
  threshold           = 30
  alarm_actions       = [aws_autoscaling_policy.scale_down.arn]

  dimensions = {
    AutoScalingGroupName = aws_autoscaling_group.this.name
  }
}
```

<details>
<summary>Explain the bug</summary>

The policy uses `adjustment_type = "ExactCapacity"` with `scaling_adjustment = 2`. This means that when CPU drops below 30%, the ASG desired capacity is set to exactly 2 instances -- regardless of current load conditions.

The danger occurs during traffic spike recovery. Imagine this sequence:

1. Traffic spike hits, step scale-up policy adds instances (ASG goes to 6).
2. The scale-up policy handles the load, CPU drops.
3. CPU falls below 30% while requests are still in flight (instances are finishing processing).
4. The scale-down alarm fires and sets desired capacity to exactly 2.
5. AWS terminates 4 instances immediately -- including ones still handling requests.
6. The remaining 2 instances get overwhelmed, CPU spikes again, triggering scale-up.
7. This creates a destructive oscillation loop.

**The fix:** Use `adjustment_type = "ChangeInCapacity"` with `scaling_adjustment = -1` to scale in gradually, or better yet, rely on the target tracking policy's built-in scale-in behavior, which ramps down conservatively. Never use `ExactCapacity` for scale-down unless you have a very specific reason and understand the interaction with other policies.

</details>

---

## Verify What You Learned

1. **Deploy the infrastructure:**
   ```bash
   terraform init && terraform apply -auto-approve
   ```

2. **Confirm the ASG is running with 2 instances:**
   ```bash
   aws autoscaling describe-auto-scaling-groups \
     --auto-scaling-group-names saa-ex04-asg \
     --query 'AutoScalingGroups[0].{Desired:DesiredCapacity,Min:MinSize,Max:MaxSize,Instances:Instances[*].InstanceId}'
   ```

3. **List all scaling policies attached to the ASG:**
   ```bash
   aws autoscaling describe-policies \
     --auto-scaling-group-name saa-ex04-asg \
     --query 'ScalingPolicies[*].{Name:PolicyName,Type:PolicyType,Target:TargetTrackingConfiguration.TargetValue}'
   ```

4. **Generate CPU load via SSM to trigger scaling:**
   ```bash
   INSTANCE_ID=$(aws autoscaling describe-auto-scaling-groups \
     --auto-scaling-group-names saa-ex04-asg \
     --query 'AutoScalingGroups[0].Instances[0].InstanceId' --output text)

   aws ssm send-command \
     --instance-ids "$INSTANCE_ID" \
     --document-name "AWS-RunShellScript" \
     --parameters 'commands=["stress-ng --cpu 4 --timeout 120s"]' \
     --comment "Generate CPU load for scaling test"
   ```

5. **Watch scaling activities in real time:**
   ```bash
   watch -n 10 "aws autoscaling describe-scaling-activities \
     --auto-scaling-group-name saa-ex04-asg \
     --max-items 5 \
     --query 'Activities[*].{Status:StatusCode,Cause:Cause,Time:StartTime}' \
     --output table"
   ```

6. **Verify CloudWatch alarms triggered:**
   ```bash
   aws cloudwatch describe-alarms \
     --alarm-name-prefix saa-ex04 \
     --query 'MetricAlarms[*].{Name:AlarmName,State:StateValue,Metric:MetricName}'
   ```

7. **Confirm instances scaled back after load stops (wait ~5 minutes):**
   ```bash
   aws autoscaling describe-auto-scaling-groups \
     --auto-scaling-group-names saa-ex04-asg \
     --query 'AutoScalingGroups[0].DesiredCapacity'
   ```

---

## Scaling Policy Decision Framework

| Criterion | Target Tracking | Step Scaling | Scheduled Scaling | Predictive Scaling |
|---|---|---|---|---|
| **Best for** | Steady-state workloads | Spiky/bursty traffic | Predictable patterns | Recurring cycles with ML |
| **Reaction speed** | Moderate (waits for metric) | Fast (tiered response) | Proactive (pre-scales) | Proactive (ML forecasts) |
| **Configuration** | Simple (one target value) | Complex (multiple steps) | Simple (cron + capacity) | Moderate (metric + mode) |
| **Scale-in** | Automatic, conservative | Must configure separately | Must schedule separately | Automatic |
| **Overlap risk** | Low | High (conflicts with TT) | Medium | Low |
| **Exam frequency** | Very high | High | Medium | Growing |

---

## Solutions

<details>
<summary>compute.tf -- TODO 1: Target Tracking Policy (CPU at 60%)</summary>

```hcl
resource "aws_autoscaling_policy" "target_tracking_cpu" {
  name                   = "${var.project_name}-target-tracking-cpu"
  autoscaling_group_name = aws_autoscaling_group.this.name
  policy_type            = "TargetTrackingScaling"

  target_tracking_configuration {
    predefined_metric_specification {
      predefined_metric_type = "ASGAverageCPUUtilization"
    }

    target_value     = 60
    disable_scale_in = false
  }
}
```

</details>

<details>
<summary>compute.tf -- TODO 2: Step Scaling Policy (tiered CPU response)</summary>

```hcl
resource "aws_autoscaling_policy" "step_scaling_up" {
  name                   = "${var.project_name}-step-scaling-up"
  autoscaling_group_name = aws_autoscaling_group.this.name
  policy_type            = "StepScaling"
  adjustment_type        = "ChangeInCapacity"

  step_adjustment {
    metric_interval_lower_bound = 0
    metric_interval_upper_bound = 15
    scaling_adjustment          = 2
  }

  step_adjustment {
    metric_interval_lower_bound = 15
    scaling_adjustment          = 4
  }
}

resource "aws_cloudwatch_metric_alarm" "high_cpu" {
  alarm_name          = "${var.project_name}-high-cpu"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 2
  metric_name         = "CPUUtilization"
  namespace           = "AWS/EC2"
  period              = 60
  statistic           = "Average"
  threshold           = 80
  alarm_description   = "Triggers step scaling when CPU exceeds 80%"
  alarm_actions       = [aws_autoscaling_policy.step_scaling_up.arn]

  dimensions = {
    AutoScalingGroupName = aws_autoscaling_group.this.name
  }
}
```

The bounds are relative to the alarm threshold (80%). So `lower=0, upper=15` means 80-95% CPU, and `lower=15` means 95%+ CPU.

</details>

<details>
<summary>compute.tf -- TODO 3: Scheduled Scaling (business hours)</summary>

```hcl
resource "aws_autoscaling_schedule" "scale_up_morning" {
  scheduled_action_name  = "${var.project_name}-scale-up-morning"
  autoscaling_group_name = aws_autoscaling_group.this.name
  min_size               = 4
  max_size               = 6
  desired_capacity       = 4
  recurrence             = "0 8 * * MON-FRI"
}

resource "aws_autoscaling_schedule" "scale_down_evening" {
  scheduled_action_name  = "${var.project_name}-scale-down-evening"
  autoscaling_group_name = aws_autoscaling_group.this.name
  min_size               = 2
  max_size               = 4
  desired_capacity       = 2
  recurrence             = "0 20 * * MON-FRI"
}
```

</details>

<details>
<summary>compute.tf -- TODO 4: Cooldown Configuration</summary>

Add `default_cooldown` to the ASG resource:

```hcl
resource "aws_autoscaling_group" "this" {
  # ... existing configuration ...
  default_cooldown = 300
}
```

Update the target tracking policy from TODO 1 with `estimated_instance_warmup`:

```hcl
resource "aws_autoscaling_policy" "target_tracking_cpu" {
  name                      = "${var.project_name}-target-tracking-cpu"
  autoscaling_group_name    = aws_autoscaling_group.this.name
  policy_type               = "TargetTrackingScaling"
  estimated_instance_warmup = 120

  target_tracking_configuration {
    predefined_metric_specification {
      predefined_metric_type = "ASGAverageCPUUtilization"
    }

    target_value     = 60
    disable_scale_in = false
  }
}
```

Key concepts:
- `default_cooldown` (300s): prevents the ASG from launching or terminating additional instances before previous scaling activity takes effect.
- `estimated_instance_warmup` (120s): the policy ignores CPU metrics from instances younger than 120s, preventing premature scale-in decisions based on idle new instances.

</details>

<details>
<summary>monitoring.tf -- TODO 5: CloudWatch Alarm for GroupInServiceInstances</summary>

```hcl
resource "aws_cloudwatch_metric_alarm" "insufficient_instances" {
  alarm_name          = "${var.project_name}-insufficient-instances"
  comparison_operator = "LessThanThreshold"
  evaluation_periods  = 2
  metric_name         = "GroupInServiceInstances"
  namespace           = "AWS/AutoScaling"
  period              = 60
  statistic           = "Average"
  threshold           = 2
  alarm_description   = "Alarm when healthy instances drop below minimum (2). Investigate launch failures or health check issues."

  dimensions = {
    AutoScalingGroupName = aws_autoscaling_group.this.name
  }
}
```

</details>

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify no instances remain:

```bash
aws autoscaling describe-auto-scaling-groups \
  --auto-scaling-group-names saa-ex04-asg \
  --query 'AutoScalingGroups[0].Instances' 2>/dev/null || echo "ASG deleted successfully"
```

---

## What's Next

Exercise 05 explores S3 performance optimization with multipart uploads and Transfer Acceleration. You will measure the difference between single-PUT and multipart uploads, configure lifecycle rules for orphaned parts, and analyze the cost trade-offs of Transfer Acceleration -- building on the performance-oriented thinking introduced in this scaling exercise.

---

## Summary

You deployed an Auto Scaling Group behind an Application Load Balancer and attached four distinct scaling mechanisms: target tracking for steady-state CPU management, step scaling for tiered burst response, scheduled scaling for predictable business-hours patterns, and cooldown configuration to prevent oscillation. You generated real CPU load via SSM and observed how each policy responds differently to the same stimulus. The decision framework you built -- matching policy type to traffic pattern -- is directly applicable to SAA-C03 scenario questions and to production architecture decisions.

---

## Reference

- [Auto Scaling Policy Types](https://docs.aws.amazon.com/autoscaling/ec2/userguide/as-scale-based-on-demand.html)
- [Target Tracking Scaling](https://docs.aws.amazon.com/autoscaling/ec2/userguide/as-scaling-target-tracking.html)
- [Step and Simple Scaling](https://docs.aws.amazon.com/autoscaling/ec2/userguide/as-scaling-simple-step.html)
- [Scheduled Scaling](https://docs.aws.amazon.com/autoscaling/ec2/userguide/ec2-auto-scaling-scheduled-scaling.html)
- [Predictive Scaling](https://docs.aws.amazon.com/autoscaling/ec2/userguide/ec2-auto-scaling-predictive-scaling.html)
- [Scaling Cooldowns](https://docs.aws.amazon.com/autoscaling/ec2/userguide/ec2-auto-scaling-scaling-cooldowns.html)

## Additional Resources

- [Terraform aws_autoscaling_policy](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/autoscaling_policy)
- [Terraform aws_autoscaling_schedule](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/autoscaling_schedule)
- [AWS Auto Scaling Cheat Sheet (SAA-C03)](https://tutorialsdojo.com/amazon-ec2-auto-scaling/)
- [stress-ng documentation](https://wiki.ubuntu.com/Kernel/Reference/stress-ng)

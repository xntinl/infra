# 43. Cross-Zone Load Balancing

<!--
difficulty: basic
concepts: [cross-zone-load-balancing, alb-cross-zone, nlb-cross-zone, uneven-distribution, az-imbalance, elb-traffic-distribution]
tools: [terraform, aws-cli]
estimated_time: 30m
bloom_level: understand
prerequisites: [none]
aws_cost: ~$0.03/hr
-->

> **AWS Cost Warning:** An ALB costs ~$0.0225/hr. Three t3.micro instances cost ~$0.0312/hr. Total ~$0.05/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Default VPC with subnets in at least 2 AZs
- Basic understanding of load balancer concepts

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** what cross-zone load balancing does: distributes traffic evenly across all registered targets in all AZs, not just within the same AZ
- **Identify** the default cross-zone behavior for each load balancer type: ALB (enabled, free), NLB (disabled, charges apply), CLB (disabled, free)
- **Demonstrate** uneven traffic distribution when cross-zone is disabled with different target counts per AZ
- **Construct** a Terraform configuration with an ALB and instances across 2 AZs to visualize cross-zone behavior
- **Describe** when you might intentionally disable cross-zone load balancing (latency-sensitive, data transfer cost optimization)

## Why Cross-Zone Load Balancing Matters

Cross-zone load balancing determines how a load balancer distributes traffic when targets are unevenly distributed across Availability Zones. Without cross-zone balancing, each AZ's load balancer node distributes traffic only to targets in its own AZ. This creates an imbalance: if AZ-a has 1 target and AZ-b has 4 targets, the single target in AZ-a receives 50% of all traffic while each target in AZ-b receives only 12.5%.

The SAA-C03 exam tests this in scenarios describing uneven traffic distribution. "A company has an NLB with 2 instances in AZ-a and 8 instances in AZ-b, and the AZ-a instances are at 100% CPU while AZ-b instances are at 20%. What is the most likely cause?" The answer is that cross-zone load balancing is disabled (NLB default). Each AZ receives 50% of traffic, but AZ-a has only 2 instances sharing that 50%, while AZ-b has 8 instances sharing the other 50%.

Understanding the defaults is critical:
- **ALB**: cross-zone enabled by default, free of charge
- **NLB**: cross-zone disabled by default, charges for inter-AZ data transfer when enabled
- **CLB**: cross-zone disabled by default, free when enabled

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
  default     = "saa-ex43"
}
```

### `security.tf`

```hcl
resource "aws_security_group" "alb" {
  name_prefix = "${var.project_name}-alb-"
  vpc_id      = data.aws_vpc.default.id
  description = "ALB - public HTTP"

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
  name_prefix = "${var.project_name}-instance-"
  vpc_id      = data.aws_vpc.default.id
  description = "App instances - from ALB"

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

### `compute.tf`

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

data "aws_availability_zones" "available" {
  state = "available"
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
# 3 instances across 2 AZs: 1 in AZ-a, 2 in AZ-b.
# This creates an intentional imbalance to demonstrate
# cross-zone load balancing behavior.
# ------------------------------------------------------------------

# AZ-a: 1 instance
resource "aws_instance" "az_a" {
  ami                    = data.aws_ami.al2023.id
  instance_type          = "t3.micro"
  subnet_id              = data.aws_subnets.default.ids[0]
  vpc_security_group_ids = [aws_security_group.instance.id]

  user_data = base64encode(<<-EOF
    #!/bin/bash
    yum install -y httpd
    TOKEN=$(curl -s -X PUT "http://169.254.169.254/latest/api/token" -H "X-aws-ec2-metadata-token-ttl-seconds: 21600")
    INSTANCE_ID=$(curl -s -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/meta-data/instance-id)
    AZ=$(curl -s -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/meta-data/placement/availability-zone)
    echo "<h1>Instance: $INSTANCE_ID</h1><p>AZ: $AZ</p>" > /var/www/html/index.html
    systemctl start httpd
  EOF
  )

  tags = {
    Name = "${var.project_name}-az-a-1"
    AZ   = "az-a"
  }
}

# AZ-b: 2 instances
resource "aws_instance" "az_b" {
  count                  = 2
  ami                    = data.aws_ami.al2023.id
  instance_type          = "t3.micro"
  subnet_id              = data.aws_subnets.default.ids[1]
  vpc_security_group_ids = [aws_security_group.instance.id]

  user_data = base64encode(<<-EOF
    #!/bin/bash
    yum install -y httpd
    TOKEN=$(curl -s -X PUT "http://169.254.169.254/latest/api/token" -H "X-aws-ec2-metadata-token-ttl-seconds: 21600")
    INSTANCE_ID=$(curl -s -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/meta-data/instance-id)
    AZ=$(curl -s -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/meta-data/placement/availability-zone)
    echo "<h1>Instance: $INSTANCE_ID</h1><p>AZ: $AZ</p>" > /var/www/html/index.html
    systemctl start httpd
  EOF
  )

  tags = {
    Name = "${var.project_name}-az-b-${count.index + 1}"
    AZ   = "az-b"
  }
}
```

### `alb.tf`

```hcl
# ------------------------------------------------------------------
# ALB with cross-zone load balancing (enabled by default on ALB)
# ------------------------------------------------------------------
resource "aws_lb" "this" {
  name               = "${var.project_name}-alb"
  internal           = false
  load_balancer_type = "application"
  security_groups    = [aws_security_group.alb.id]
  subnets            = slice(data.aws_subnets.default.ids, 0, 2)

  # Cross-zone is enabled by default on ALB and cannot be disabled
  # at the LB level (can be disabled per target group in newer API).
  # This means ALB distributes evenly across ALL targets regardless
  # of which AZ they are in.

  tags = { Name = "${var.project_name}-alb" }
}

resource "aws_lb_target_group" "app" {
  name     = "${var.project_name}-app"
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
    target_group_arn = aws_lb_target_group.app.arn
  }
}

# Register all 3 instances in the same target group
resource "aws_lb_target_group_attachment" "az_a" {
  target_group_arn = aws_lb_target_group.app.arn
  target_id        = aws_instance.az_a.id
  port             = 80
}

resource "aws_lb_target_group_attachment" "az_b" {
  count            = 2
  target_group_arn = aws_lb_target_group.app.arn
  target_id        = aws_instance.az_b[count.index].id
  port             = 80
}
```

### `outputs.tf`

```hcl
output "alb_dns" {
  value = aws_lb.this.dns_name
}

output "az_a_instance" {
  value = {
    id = aws_instance.az_a.id
    az = aws_instance.az_a.availability_zone
  }
}

output "az_b_instances" {
  value = [for i in aws_instance.az_b : {
    id = i.id
    az = i.availability_zone
  }]
}
```

## Step 2 -- Deploy and Test Traffic Distribution

```bash
terraform init
terraform apply -auto-approve
```

Wait 2-3 minutes for instances to pass health checks, then test traffic distribution:

```bash
ALB_DNS=$(terraform output -raw alb_dns)

# Send 30 requests and count which instances respond
for i in $(seq 1 30); do
  curl -s "http://$ALB_DNS" | grep "Instance:"
done | sort | uniq -c | sort -rn
```

### Expected Results with Cross-Zone Enabled (ALB default)

With cross-zone enabled, the ALB distributes requests evenly across all 3 targets:

```
~10 requests → AZ-a instance 1  (33%)
~10 requests → AZ-b instance 1  (33%)
~10 requests → AZ-b instance 2  (33%)
```

Each instance gets roughly equal traffic regardless of AZ placement.

### What Would Happen WITHOUT Cross-Zone (NLB default)

If cross-zone were disabled (as it is by default on NLB):

```
~15 requests → AZ-a instance 1  (50% -- all AZ-a traffic goes to 1 instance)
~7  requests → AZ-b instance 1  (25% -- AZ-b traffic split between 2)
~8  requests → AZ-b instance 2  (25%)
```

The AZ-a instance receives 2x the traffic of each AZ-b instance because each AZ node distributes only to its own AZ's targets.

## Step 3 -- Verify Target Health Across AZs

```bash
TARGET_GROUP_ARN=$(aws elbv2 describe-target-groups \
  --names saa-ex43-app \
  --query "TargetGroups[0].TargetGroupArn" --output text)

aws elbv2 describe-target-health \
  --target-group-arn $TARGET_GROUP_ARN \
  --query "TargetHealthDescriptions[*].{Target:Target.Id,AZ:Target.AvailabilityZone,Health:TargetHealth.State}" \
  --output table
```

Expected: 3 healthy targets, 1 in one AZ and 2 in another.

## Step 4 -- Cross-Zone Behavior Reference

### Cross-Zone Load Balancing Defaults

| Load Balancer | Cross-Zone Default | Cost When Enabled | Can Disable? |
|--------------|-------------------|-------------------|-------------|
| **ALB** | Enabled | Free | Per-target-group (newer API) |
| **NLB** | Disabled | Inter-AZ data transfer charges | Yes (per LB or per target group) |
| **CLB** | Disabled | Free | Yes |
| **GWLB** | Enabled | Free | Yes |

### Traffic Distribution Visualization

```
                     Without Cross-Zone              With Cross-Zone
                     (NLB default)                   (ALB default)

                        50%    50%                     33%  33%  33%
                    ┌────┴────┐┌───┴────┐          ┌───┴──┐┌─┴──┐┌─┴──┐
                    │  AZ-a   ││  AZ-b  │          │ AZ-a ││AZ-b││AZ-b│
                    │  node   ││  node  │          │ inst ││inst││inst│
                    └────┬────┘└───┬────┘          └──────┘└────┘└────┘
                         │    ┌────┴────┐
                    ┌────┴──┐ │         │
                    │ inst  │ │  inst   │  inst
                    │ (50%) │ │ (25%)   │  (25%)
                    └───────┘ └─────────┘
```

### When to Disable Cross-Zone

| Scenario | Cross-Zone Behavior | Recommendation |
|----------|-------------------|----------------|
| Standard web app | Enable | Even distribution across all targets |
| Latency-sensitive (same-AZ preferred) | Disable | Avoid cross-AZ latency (~1-2ms overhead) |
| High data transfer cost | Disable (NLB) | Avoid inter-AZ data transfer charges ($0.01/GB) |
| Targets evenly distributed | Does not matter | Same result either way |
| Uneven target count per AZ | Enable | Prevents overloading under-provisioned AZs |

## Common Mistakes

### 1. Not understanding NLB cross-zone default

**Wrong assumption:** "I set up an NLB and my instances are getting uneven traffic. The load balancer must be broken."

**What happens:** NLB has cross-zone disabled by default. If you have 2 targets in AZ-a and 10 targets in AZ-b, each AZ receives 50% of traffic. The 2 targets in AZ-a each handle 25% of total traffic while the 10 targets in AZ-b each handle 5%.

**Fix:** Enable cross-zone load balancing on the NLB:

```hcl
resource "aws_lb" "nlb" {
  name               = "my-nlb"
  load_balancer_type = "network"
  subnets            = data.aws_subnets.default.ids

  enable_cross_zone_load_balancing = true
}
```

Note: this incurs inter-AZ data transfer charges ($0.01/GB).

### 2. Assuming ALB cross-zone can be disabled at the LB level

**Wrong approach:** Setting `enable_cross_zone_load_balancing = false` on an ALB.

**What happens:** ALB always has cross-zone enabled at the load balancer level. The attribute is ignored. Newer AWS API versions allow disabling cross-zone per target group, but the ALB itself always distributes across all AZ nodes.

**Fix:** If you need AZ-affinity, use NLB with cross-zone disabled, or use ALB with target group-level cross-zone settings.

### 3. Ignoring inter-AZ data transfer costs with NLB

**Wrong approach:** Enabling NLB cross-zone without considering the data transfer bill.

**What happens:** With cross-zone enabled, NLB sends traffic from one AZ to targets in another AZ. AWS charges $0.01/GB for inter-AZ data transfer. At 1 TB/day, this adds ~$300/month.

**Fix:** Calculate the cost before enabling. If your target distribution is reasonably even, keep cross-zone disabled. Enable it only when target imbalance causes performance problems that outweigh the data transfer cost.

## Verify What You Learned

```bash
# Verify ALB exists and is active
aws elbv2 describe-load-balancers \
  --names saa-ex43-alb \
  --query "LoadBalancers[0].{Type:Type,State:State.Code}" --output table
```

Expected: Type=application, State=active

```bash
# Verify 3 healthy targets
aws elbv2 describe-target-health \
  --target-group-arn $(aws elbv2 describe-target-groups --names saa-ex43-app --query "TargetGroups[0].TargetGroupArn" --output text) \
  --query "length(TargetHealthDescriptions[?TargetHealth.State=='healthy'])"
```

Expected: `3`

```bash
# Test traffic distribution (run 30 requests)
ALB_DNS=$(terraform output -raw alb_dns)
for i in $(seq 1 30); do curl -s "http://$ALB_DNS" | grep "AZ:"; done | sort | uniq -c
```

Expected: roughly even distribution across both AZs.

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
aws elbv2 describe-load-balancers --names saa-ex43-alb 2>&1 | grep -c "not found" || echo "Check ALB status"
```

Expected: "not found" confirmation.

## What's Next

You observed how cross-zone load balancing affects traffic distribution with uneven target counts. In the next exercise, you will configure **Connection Draining (Deregistration Delay)** -- controlling how in-flight requests are handled when targets are removed from a load balancer.

## Summary

- **Cross-zone load balancing** distributes traffic evenly across ALL targets in ALL AZs, not just within each AZ
- **ALB**: cross-zone enabled by default, **free** -- cannot be disabled at the LB level
- **NLB**: cross-zone disabled by default, **charges inter-AZ data transfer** when enabled
- **CLB**: cross-zone disabled by default, free when enabled
- Without cross-zone, each AZ node sends traffic only to targets in its own AZ -- causing **imbalance** when target counts differ per AZ
- With 1 target in AZ-a and 2 in AZ-b (no cross-zone): AZ-a target gets **50%** of traffic, AZ-b targets get **25% each**
- With cross-zone enabled: all 3 targets get approximately **33% each**
- Disable cross-zone for **latency-sensitive** workloads or to avoid **inter-AZ data transfer costs** on NLB
- The SAA-C03 frequently tests NLB's disabled-by-default behavior in "uneven traffic distribution" scenarios

## Reference

- [Cross-Zone Load Balancing](https://docs.aws.amazon.com/elasticloadbalancing/latest/userguide/how-elastic-load-balancing-works.html#cross-zone-load-balancing)
- [ALB Documentation](https://docs.aws.amazon.com/elasticloadbalancing/latest/application/introduction.html)
- [NLB Cross-Zone](https://docs.aws.amazon.com/elasticloadbalancing/latest/network/network-load-balancers.html#cross-zone-load-balancing)
- [Terraform aws_lb](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb)

## Additional Resources

- [ELB Data Transfer Pricing](https://aws.amazon.com/elasticloadbalancing/pricing/) -- inter-AZ data transfer charges for NLB cross-zone
- [Availability Zone Concepts](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/using-regions-availability-zones.html) -- understanding AZ isolation and cross-AZ traffic
- [Target Group Attributes](https://docs.aws.amazon.com/elasticloadbalancing/latest/application/load-balancer-target-groups.html#target-group-attributes) -- per-target-group cross-zone settings
- [ELB Best Practices](https://docs.aws.amazon.com/elasticloadbalancing/latest/userguide/elb-best-practices.html) -- distributing targets evenly across AZs

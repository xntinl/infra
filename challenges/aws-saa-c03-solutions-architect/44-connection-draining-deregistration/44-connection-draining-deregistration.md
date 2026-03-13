# 44. Connection Draining and Deregistration Delay

<!--
difficulty: intermediate
concepts: [connection-draining, deregistration-delay, in-flight-requests, graceful-shutdown, alb-deregistration, nlb-deregistration, deployment-speed]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: apply, evaluate
prerequisites: [none]
aws_cost: ~$0.03/hr
-->

> **AWS Cost Warning:** An ALB costs ~$0.0225/hr. Two t3.micro instances cost ~$0.0208/hr. Total ~$0.04/hr. Remember to run `terraform destroy` when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Default VPC available in target region | `aws ec2 describe-vpcs --filters Name=isDefault,Values=true` |
| Understanding of ALB target groups | Familiarity with target registration and health checks |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Explain** what deregistration delay (connection draining) does: allows in-flight requests to complete before fully removing a target from the load balancer.
2. **Configure** the `deregistration_delay` attribute on an ALB target group and observe its effect during target deregistration.
3. **Evaluate** the trade-off between delay duration and deployment speed: too long delays slow rolling deployments, too short drops active connections.
4. **Compare** connection draining behavior across ALB, NLB, and CLB.
5. **Select** appropriate deregistration delay values for different workload profiles (short API calls vs long-running uploads).

---

## Why This Matters

Deregistration delay is one of the most overlooked settings that directly impacts both user experience and deployment speed. The SAA-C03 exam tests this in deployment scenarios: "A company performs rolling deployments and notices that deployments take 25 minutes for 5 instances. The deregistration delay is set to 300 seconds. How can they speed up deployments?" The answer is to reduce the deregistration delay to match the actual request completion time.

Here is the problem: when a target is deregistered (during a rolling deployment, scale-in, or manual removal), the load balancer needs to handle two things simultaneously. It must stop sending NEW requests to the deregistering target, and it must allow EXISTING (in-flight) requests to complete. The deregistration delay is how long the load balancer waits for in-flight requests to finish before forcibly closing connections.

The default is 300 seconds (5 minutes). For a REST API where requests complete in under 1 second, this means each instance removal takes 5 minutes of waiting for nothing. A rolling deployment of 5 instances takes 25 minutes instead of 5 minutes. For a file upload service where uploads take 5-10 minutes, 300 seconds might not be enough -- uploads would be interrupted. Right-sizing this value to your actual workload profile is a simple change with massive deployment speed impact.

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
  default     = "saa-ex44"
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

resource "aws_security_group" "app" {
  name_prefix = "${var.project_name}-app-"
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
# Two instances to demonstrate deregistration
# ------------------------------------------------------------------
resource "aws_instance" "app" {
  count                  = 2
  ami                    = data.aws_ami.al2023.id
  instance_type          = "t3.micro"
  subnet_id              = data.aws_subnets.default.ids[count.index % length(data.aws_subnets.default.ids)]
  vpc_security_group_ids = [aws_security_group.app.id]

  user_data = base64encode(<<-EOF
    #!/bin/bash
    yum install -y httpd
    TOKEN=$(curl -s -X PUT "http://169.254.169.254/latest/api/token" -H "X-aws-ec2-metadata-token-ttl-seconds: 21600")
    INSTANCE_ID=$(curl -s -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/meta-data/instance-id)
    echo "<h1>Instance: $INSTANCE_ID</h1><p>Status: Active</p>" > /var/www/html/index.html
    systemctl start httpd
  EOF
  )

  tags = {
    Name = "${var.project_name}-app-${count.index + 1}"
  }
}
```

### `alb.tf`

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

resource "aws_lb" "this" {
  name               = "${var.project_name}-alb"
  internal           = false
  load_balancer_type = "application"
  security_groups    = [aws_security_group.alb.id]
  subnets            = slice(data.aws_subnets.default.ids, 0, 2)

  tags = { Name = "${var.project_name}-alb" }
}

# ============================================================
# TODO 1: Target Group with Deregistration Delay
# ============================================================
# Create a target group with a specific deregistration delay.
#
# Requirements:
#   - Resource: aws_lb_target_group
#     - name = "${var.project_name}-app"
#     - port = 80
#     - protocol = "HTTP"
#     - vpc_id
#     - deregistration_delay = 30 (seconds)
#       Default is 300s (5 min). For a REST API where requests
#       complete in <1s, 30s is more than enough.
#     - health_check block:
#       - path = "/"
#       - healthy_threshold = 2
#       - unhealthy_threshold = 3
#       - interval = 10
#       - timeout = 5
#
# The deregistration_delay controls how long the ALB waits
# for in-flight requests to complete after a target is
# deregistered. During this time:
#   - No NEW requests are sent to the target
#   - EXISTING connections remain open
#   - After the delay expires, remaining connections are
#     forcibly closed
#
# Docs: https://docs.aws.amazon.com/elasticloadbalancing/latest/application/load-balancer-target-groups.html#deregistration-delay
# ============================================================


# ============================================================
# TODO 2: Register Targets and Create Listener
# ============================================================
# Register both instances and create the HTTP listener.
#
# Requirements:
#   - Resource: aws_lb_target_group_attachment (for each instance)
#     - target_group_arn
#     - target_id = instance ID
#     - port = 80
#
#   - Resource: aws_lb_listener
#     - load_balancer_arn
#     - port = 80, protocol = "HTTP"
#     - default_action type = "forward"
#
# After registration, both targets will be in "initial" state
# until they pass health checks (10-30 seconds).
# ============================================================


# ============================================================
# TODO 3: Deregister a Target and Observe Draining
# ============================================================
# After deploying, deregister one target and observe the
# draining behavior using CLI commands.
#
# CLI workflow:
#
# 1. Verify both targets are healthy:
#    aws elbv2 describe-target-health \
#      --target-group-arn <TG_ARN> \
#      --output table
#
# 2. Start a long-running request to the ALB (simulating
#    an in-flight request):
#    curl -v "http://<ALB_DNS>/" &
#
# 3. Deregister one target:
#    aws elbv2 deregister-targets \
#      --target-group-arn <TG_ARN> \
#      --targets Id=<INSTANCE_ID>
#
# 4. Immediately check target health (should show "draining"):
#    aws elbv2 describe-target-health \
#      --target-group-arn <TG_ARN> \
#      --output table
#
# 5. Wait for deregistration_delay (30 seconds), then check again:
#    sleep 35
#    aws elbv2 describe-target-health \
#      --target-group-arn <TG_ARN> \
#      --output table
#
# Expected states:
#   t=0:  healthy, healthy
#   t=1:  healthy, draining
#   t=31: healthy, (gone -- deregistered)
#
# During the "draining" phase, new requests go only to the
# healthy target. Existing connections to the draining target
# are allowed to complete.
# ============================================================
```

### `outputs.tf`

```hcl
output "alb_dns" {
  value = aws_lb.this.dns_name
}

output "instance_ids" {
  value = aws_instance.app[*].id
}
```

---

## Spot the Bug

A team complains that rolling deployments take 30 minutes for a 6-instance ASG. Their application is a stateless REST API where all requests complete in under 200ms:

```hcl
resource "aws_lb_target_group" "api" {
  name     = "production-api"
  port     = 8080
  protocol = "HTTP"
  vpc_id   = aws_vpc.this.id

  # Default deregistration_delay = 300 (5 minutes)
  # Not explicitly set, so it uses the default

  health_check {
    path                = "/health"
    healthy_threshold   = 5    # Bug 2
    unhealthy_threshold = 2
    interval            = 30   # Bug 3
    timeout             = 10
  }
}
```

<details>
<summary>Explain the bug</summary>

Three issues compound to create the 30-minute deployment:

**1. Default `deregistration_delay = 300` (5 minutes).**
For a REST API where requests complete in 200ms, waiting 5 minutes per instance for "in-flight requests" is wildly excessive. Each instance removal takes 5 minutes of waiting for nothing. With 6 instances in a rolling deployment (removing one at a time), that is 6 * 5 = 30 minutes of pure waiting.

**2. `healthy_threshold = 5` with `interval = 30`.**
After launching a replacement instance, the ALB must see 5 consecutive healthy checks before sending traffic. At 30-second intervals, that is 5 * 30 = 150 seconds (2.5 minutes) before the new instance is considered healthy. During this time, the ASG cannot proceed to the next instance.

**3. `interval = 30` seconds is unnecessarily long.**
For a health check endpoint that responds in milliseconds, a 10-second interval is sufficient and dramatically reduces the time to mark new instances as healthy.

**The fix:**

```hcl
resource "aws_lb_target_group" "api" {
  name     = "production-api"
  port     = 8080
  protocol = "HTTP"
  vpc_id   = aws_vpc.this.id

  deregistration_delay = 30  # 30 seconds (more than enough for 200ms requests)

  health_check {
    path                = "/health"
    healthy_threshold   = 2    # 2 checks (not 5)
    unhealthy_threshold = 3
    interval            = 10   # 10 seconds (not 30)
    timeout             = 5
  }
}
```

**Deployment time comparison:**

| Setting | Original | Fixed |
|---------|----------|-------|
| Deregistration delay | 300s | 30s |
| Health check interval | 30s | 10s |
| Healthy threshold | 5 (150s) | 2 (20s) |
| Per-instance time | ~450s | ~50s |
| 6-instance deployment | ~45 min | ~5 min |

The fixed configuration reduces deployment time by **9x** with no impact on service reliability -- the API completes all requests in 200ms, so 30 seconds of draining is more than sufficient.

</details>

---

## Deregistration Delay Decision Framework

| Workload Type | Typical Request Duration | Recommended Delay | Reasoning |
|--------------|------------------------|-------------------|-----------|
| REST API (JSON) | <1 second | 10-30 seconds | Requests are short-lived |
| Web page rendering | 1-5 seconds | 30-60 seconds | Include slow page loads |
| File upload | 1-30 minutes | 300-1800 seconds | Must complete large uploads |
| WebSocket connections | Hours | 3600 seconds (max) | Long-lived connections |
| gRPC streaming | Variable | Match max stream duration | Depends on stream length |
| Background job processing | Minutes | 0 seconds | Use graceful shutdown signal instead |

### Deregistration Delay vs Deployment Speed

```
Delay:  300s (default)     120s            30s            0s
        |                   |               |              |
Speed:  Slowest            Moderate        Fast           Instant
Safety: Safest             Safe            Safe for APIs  May drop connections
        (5 min drain)      (2 min drain)   (30s drain)    (no drain)
```

### Connection Draining Across Load Balancer Types

| Feature | ALB | NLB | CLB |
|---------|-----|-----|-----|
| **Setting name** | Deregistration delay | Deregistration delay | Connection draining |
| **Default** | 300 seconds | 300 seconds | 300 seconds (when enabled) |
| **Range** | 0-3600 seconds | 0-3600 seconds | 1-3600 seconds |
| **Behavior** | No new requests; existing complete | No new connections; existing complete | No new requests; existing complete |
| **Configure on** | Target group attribute | Target group attribute | Load balancer attribute |
| **HTTP/2** | Sends GOAWAY frame | N/A | N/A |

---

## Solutions

<details>
<summary>TODO 1 -- Target Group with Deregistration Delay (alb.tf)</summary>

```hcl
resource "aws_lb_target_group" "app" {
  name     = "${var.project_name}-app"
  port     = 80
  protocol = "HTTP"
  vpc_id   = data.aws_vpc.default.id

  deregistration_delay = 30  # 30 seconds instead of default 300

  health_check {
    path                = "/"
    healthy_threshold   = 2
    unhealthy_threshold = 3
    interval            = 10
    timeout             = 5
  }

  tags = {
    Name = "${var.project_name}-app-tg"
  }
}
```

Setting `deregistration_delay = 30` means:
- When a target is deregistered, the ALB stops sending NEW requests immediately
- Existing connections have 30 seconds to complete
- After 30 seconds, any remaining connections are forcibly closed
- For a web server with sub-second response times, 30 seconds is generous

</details>

<details>
<summary>TODO 2 -- Register Targets and Create Listener (alb.tf)</summary>

```hcl
resource "aws_lb_target_group_attachment" "app" {
  count            = 2
  target_group_arn = aws_lb_target_group.app.arn
  target_id        = aws_instance.app[count.index].id
  port             = 80
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
```

</details>

<details>
<summary>TODO 3 -- Deregistration Observation Commands (CLI)</summary>

```bash
# Get target group ARN
TG_ARN=$(aws elbv2 describe-target-groups \
  --names saa-ex44-app \
  --query "TargetGroups[0].TargetGroupArn" --output text)

ALB_DNS=$(terraform output -raw alb_dns)
INSTANCE_IDS=$(terraform output -json instance_ids | jq -r '.[0]')

# 1. Verify both targets are healthy
echo "=== Before deregistration ==="
aws elbv2 describe-target-health \
  --target-group-arn $TG_ARN \
  --query "TargetHealthDescriptions[*].{ID:Target.Id,State:TargetHealth.State}" \
  --output table

# 2. Deregister the first instance
echo "Deregistering $INSTANCE_IDS..."
aws elbv2 deregister-targets \
  --target-group-arn $TG_ARN \
  --targets Id=$INSTANCE_IDS

# 3. Immediately check -- should show "draining"
echo "=== Immediately after deregistration ==="
aws elbv2 describe-target-health \
  --target-group-arn $TG_ARN \
  --query "TargetHealthDescriptions[*].{ID:Target.Id,State:TargetHealth.State}" \
  --output table

# 4. Verify all new requests go to the remaining target
echo "=== Requests during draining ==="
for i in $(seq 1 5); do
  curl -s "http://$ALB_DNS" | grep "Instance:"
done

# 5. Wait for deregistration delay to expire
echo "Waiting 35 seconds for deregistration delay..."
sleep 35

# 6. Check again -- draining target should be gone
echo "=== After deregistration delay ==="
aws elbv2 describe-target-health \
  --target-group-arn $TG_ARN \
  --query "TargetHealthDescriptions[*].{ID:Target.Id,State:TargetHealth.State}" \
  --output table

# 7. Re-register the instance (for cleanup)
echo "Re-registering $INSTANCE_IDS..."
aws elbv2 register-targets \
  --target-group-arn $TG_ARN \
  --targets Id=$INSTANCE_IDS
```

Expected output shows the state transition:
- `healthy` -> `draining` (immediately after deregister) -> removed (after delay expires)
- During `draining`, all new requests go to the remaining healthy target
- The draining target accepts no new requests but existing connections can complete

</details>

---

## Verify What You Learned

```bash
# Verify target group has custom deregistration delay
aws elbv2 describe-target-group-attributes \
  --target-group-arn $(aws elbv2 describe-target-groups --names saa-ex44-app --query "TargetGroups[0].TargetGroupArn" --output text) \
  --query "Attributes[?Key=='deregistration_delay.timeout_seconds'].Value" \
  --output text
```

Expected: `30`

```bash
# Verify both targets are healthy
aws elbv2 describe-target-health \
  --target-group-arn $(aws elbv2 describe-target-groups --names saa-ex44-app --query "TargetGroups[0].TargetGroupArn" --output text) \
  --query "length(TargetHealthDescriptions[?TargetHealth.State=='healthy'])"
```

Expected: `2`

```bash
# Verify ALB is serving traffic
curl -s "http://$(terraform output -raw alb_dns)" | grep "Instance:"
```

Expected: HTML response with an instance ID.

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
aws elbv2 describe-load-balancers --names saa-ex44-alb 2>&1 | grep -c "not found" || echo "Check ALB deletion"
```

Expected: confirmation that ALB no longer exists.

---

## What's Next

You configured deregistration delay for graceful connection draining during target removal. This concludes the EC2 and ELB exercise series. The next set of exercises covers **VPC networking** -- subnets, route tables, NAT gateways, VPC peering, and Transit Gateway -- building the network foundation that underlies everything you have deployed so far.

---

## Summary

- **Deregistration delay** (connection draining) controls how long the LB waits for in-flight requests to complete after a target is removed
- The default is **300 seconds (5 minutes)** -- too long for most REST APIs, potentially too short for file uploads
- During draining: **no new requests** are sent to the target, but **existing connections remain open**
- After the delay expires, remaining connections are **forcibly closed**
- The default 300s delay can make rolling deployments take **5x longer than necessary** for fast APIs
- Right-size the delay to match your **longest expected request duration** (e.g., 30s for APIs, 300s+ for uploads)
- Health check settings (`interval`, `healthy_threshold`) also impact deployment speed -- use aggressive values for fast endpoints
- **ALB/NLB**: configure deregistration delay per target group; **CLB**: configure per load balancer
- For background job processors, set delay to **0** and implement **SIGTERM handling** in the application for graceful shutdown

---

## Reference

- [ALB Target Group Deregistration Delay](https://docs.aws.amazon.com/elasticloadbalancing/latest/application/load-balancer-target-groups.html#deregistration-delay)
- [NLB Deregistration Delay](https://docs.aws.amazon.com/elasticloadbalancing/latest/network/load-balancer-target-groups.html#deregistration-delay)
- [CLB Connection Draining](https://docs.aws.amazon.com/elasticloadbalancing/latest/classic/config-conn-drain.html)
- [Terraform aws_lb_target_group](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb_target_group)

## Additional Resources

- [ASG Instance Refresh](https://docs.aws.amazon.com/autoscaling/ec2/userguide/asg-instance-refresh.html) -- how deregistration delay interacts with rolling deployments
- [ECS Rolling Updates](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/deployment-type-ecs.html) -- deregistration delay in containerized deployments
- [Blue/Green Deployments](https://docs.aws.amazon.com/elasticloadbalancing/latest/application/load-balancer-target-groups.html#target-group-weights) -- target group weighting as an alternative to connection draining
- [Graceful Shutdown Patterns](https://docs.aws.amazon.com/prescriptive-guidance/latest/patterns/implement-graceful-shutdown-for-amazon-ecs-tasks.html) -- combining deregistration delay with application-level signal handling

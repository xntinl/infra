# 19. ALB: Path Routing, Host Routing, and Weighted Targets

<!--
difficulty: intermediate
concepts: [alb, path-routing, host-routing, weighted-target-groups, listener-rules, canary-deployment]
tools: [terraform, aws-cli]
estimated_time: 50m
bloom_level: design
prerequisites: [01-your-first-vpc, 18-network-load-balancer-patterns]
aws_cost: ~$0.05/hr
-->

> **AWS Cost Warning:** This exercise creates an Application Load Balancer (~$0.0225/hr), t3.micro EC2 instances (~$0.0104/hr each), and generates LCU charges based on traffic. Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Exercise 01 completed | Understand VPC basics |
| Exercise 18 completed | Understand load balancer concepts |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Design** ALB listener rules with path-based and host-based routing
2. **Implement** weighted target groups for canary-style traffic splitting
3. **Configure** fixed-response and redirect actions for operational flexibility
4. **Analyze** listener rule priority ordering and understand how ALB evaluates rules

## Why ALB Routing Rules Matter

A single ALB can replace dozens of separate load balancers by routing requests based on URL path, hostname, HTTP headers, query strings, and source IP. Path-based routing sends `/api/*` to your API fleet and `/static/*` to a different target group. Host-based routing sends `api.example.com` to one service and `admin.example.com` to another. Weighted target groups split traffic between two versions of a service -- send 90% to the stable version and 10% to the canary -- enabling gradual rollouts at the network level without touching application code.

This pattern is the foundation for microservice architectures on AWS. Instead of deploying one load balancer per service (which gets expensive and hard to manage), you deploy one ALB with routing rules that direct traffic to the right target group based on the request.

## Building Blocks

Create the following files in your exercise directory. Your job is to fill in each `# TODO` block.

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
  default     = "alb-routing-lab"
}
```

### `vpc.tf`

```hcl
data "aws_availability_zones" "available" {
  state = "available"
}

locals {
  azs = slice(data.aws_availability_zones.available.names, 0, 2)
}

resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = "${var.project_name}-vpc" }
}

resource "aws_subnet" "public" {
  for_each = toset(local.azs)

  vpc_id                  = aws_vpc.this.id
  cidr_block              = cidrsubnet("10.0.0.0/16", 8, index(local.azs, each.value) + 1)
  availability_zone       = each.value
  map_public_ip_on_launch = true

  tags = { Name = "${var.project_name}-public-${each.value}" }
}

resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id

  tags = { Name = "${var.project_name}-igw" }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.this.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this.id
  }

  tags = { Name = "${var.project_name}-public-rt" }
}

resource "aws_route_table_association" "public" {
  for_each = toset(local.azs)

  subnet_id      = aws_subnet.public[each.value].id
  route_table_id = aws_route_table.public.id
}
```

### `security.tf`

```hcl
# ------------------------------------------------------------------
# ALB security group: accepts HTTP from the internet.
# ------------------------------------------------------------------
resource "aws_security_group" "alb" {
  name        = "${var.project_name}-alb-sg"
  description = "Allow HTTP inbound to ALB"
  vpc_id      = aws_vpc.this.id

  tags = { Name = "${var.project_name}-alb-sg" }
}

resource "aws_vpc_security_group_ingress_rule" "alb_http" {
  security_group_id = aws_security_group.alb.id
  cidr_ipv4         = "0.0.0.0/0"
  from_port         = 80
  to_port           = 80
  ip_protocol       = "tcp"
}

resource "aws_vpc_security_group_egress_rule" "alb_out" {
  security_group_id = aws_security_group.alb.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}

# ------------------------------------------------------------------
# Backend security group: only accepts traffic from the ALB SG.
# ------------------------------------------------------------------
resource "aws_security_group" "backend" {
  name        = "${var.project_name}-backend-sg"
  description = "Allow HTTP from ALB only"
  vpc_id      = aws_vpc.this.id

  tags = { Name = "${var.project_name}-backend-sg" }
}

resource "aws_vpc_security_group_ingress_rule" "backend_from_alb" {
  security_group_id            = aws_security_group.backend.id
  referenced_security_group_id = aws_security_group.alb.id
  from_port                    = 80
  to_port                      = 80
  ip_protocol                  = "tcp"
}

resource "aws_vpc_security_group_egress_rule" "backend_out" {
  security_group_id = aws_security_group.backend.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}
```

> **Best Practice:** ALB weighted target groups enable canary releases at the network level. Send 90% of traffic to the stable version and 10% to the new version. If the canary fails, shift the weight back to 100/0 without redeploying anything.

### `compute.tf`

```hcl
data "aws_ami" "amazon_linux_2023" {
  most_recent = true
  owners      = ["amazon"]

  filter {
    name   = "name"
    values = ["al2023-ami-2023.*-x86_64"]
  }

  filter {
    name   = "state"
    values = ["available"]
  }
}

# ------------------------------------------------------------------
# API instances: serve requests on /api/* paths.
# ------------------------------------------------------------------
resource "aws_instance" "api" {
  for_each = toset(local.azs)

  ami                    = data.aws_ami.amazon_linux_2023.id
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.public[each.value].id
  vpc_security_group_ids = [aws_security_group.backend.id]

  user_data = <<-EOF
    #!/bin/bash
    yum install -y httpd
    mkdir -p /var/www/html/api
    echo '{"service":"api","version":"v1","az":"${each.value}"}' > /var/www/html/api/index.html
    echo '{"status":"ok"}' > /var/www/html/api/health
    systemctl enable httpd
    systemctl start httpd
  EOF

  tags = { Name = "${var.project_name}-api-${each.value}" }
}

# ------------------------------------------------------------------
# Web instances: serve requests on /* (default) paths.
# ------------------------------------------------------------------
resource "aws_instance" "web" {
  for_each = toset(local.azs)

  ami                    = data.aws_ami.amazon_linux_2023.id
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.public[each.value].id
  vpc_security_group_ids = [aws_security_group.backend.id]

  user_data = <<-EOF
    #!/bin/bash
    yum install -y httpd
    echo '<html><body><h1>Web v1 - ${each.value}</h1></body></html>' > /var/www/html/index.html
    systemctl enable httpd
    systemctl start httpd
  EOF

  tags = { Name = "${var.project_name}-web-${each.value}" }
}

# ------------------------------------------------------------------
# Canary instance: serves the new version for weighted routing.
# ------------------------------------------------------------------
resource "aws_instance" "web_canary" {
  ami                    = data.aws_ami.amazon_linux_2023.id
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.public[local.azs[0]].id
  vpc_security_group_ids = [aws_security_group.backend.id]

  user_data = <<-EOF
    #!/bin/bash
    yum install -y httpd
    echo '<html><body><h1>Web v2 (canary)</h1></body></html>' > /var/www/html/index.html
    systemctl enable httpd
    systemctl start httpd
  EOF

  tags = { Name = "${var.project_name}-web-canary" }
}
```

### `alb.tf`

```hcl
# =======================================================
# TODO 1 -- Application Load Balancer
# =======================================================
# Requirements:
#   - Create an aws_lb with load_balancer_type = "application"
#   - Set internal = false
#   - Assign it to all public subnets
#   - Attach the ALB security group
#   - Tag with Name = "${var.project_name}-alb"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb
# Hint: Use subnets = [for s in aws_subnet.public : s.id]


# =======================================================
# TODO 2 -- Target Groups (API, Web stable, Web canary)
# =======================================================
# Requirements:
#   - Create aws_lb_target_group "api" for port 80, HTTP, instance type
#   - Create aws_lb_target_group "web_stable" for port 80, HTTP, instance type
#   - Create aws_lb_target_group "web_canary" for port 80, HTTP, instance type
#   - Each target group needs a health_check block:
#       path     = "/" (or "/api/health" for the API TG)
#       protocol = "HTTP"
#       matcher  = "200"
#       interval = 15
#       healthy_threshold   = 2
#       unhealthy_threshold = 3
#   - Register instances with their target groups using
#     aws_lb_target_group_attachment
#   - Tag each with Name = "${var.project_name}-<type>-tg"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb_target_group


# =======================================================
# TODO 3 -- Default Listener with Weighted Target Groups
# =======================================================
# Requirements:
#   - Create an aws_lb_listener on port 80, HTTP
#   - Default action type = "forward"
#   - Use a forward block with two target_group entries:
#       web_stable with weight = 90
#       web_canary with weight = 10
#   - Tag with Name = "${var.project_name}-listener"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb_listener
# Hint: The forward block with multiple target_group entries enables weighted routing


# =======================================================
# TODO 4 -- Path-Based Routing Rule for /api/*
# =======================================================
# Requirements:
#   - Create an aws_lb_listener_rule with priority = 100
#   - Condition: path_pattern with values = ["/api/*"]
#   - Action: forward to the API target group
#   - Tag with Name = "${var.project_name}-api-rule"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb_listener_rule


# =======================================================
# TODO 5 -- Fixed Response Rule for /health
# =======================================================
# Requirements:
#   - Create an aws_lb_listener_rule with priority = 50
#   - Condition: path_pattern with values = ["/health"]
#   - Action: type = "fixed-response" with:
#       content_type = "application/json"
#       message_body = "{\"status\":\"healthy\"}"
#       status_code  = "200"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb_listener_rule
# Hint: Fixed responses are handled by the ALB itself -- no targets needed
```

### `outputs.tf`

```hcl
output "vpc_id" {
  description = "VPC ID"
  value       = aws_vpc.this.id
}

output "alb_dns_name" {
  description = "ALB DNS name"
  value       = aws_lb.this.dns_name
}

output "api_target_group_arn" {
  description = "API target group ARN"
  value       = aws_lb_target_group.api.arn
}

output "web_stable_target_group_arn" {
  description = "Web stable target group ARN"
  value       = aws_lb_target_group.web_stable.arn
}
```

## Spot the Bug

A colleague set up path-based routing but requests to `/api/users` always hit the default target group instead of the API target group. The ALB health checks show all targets healthy. **What is wrong?**

```hcl
resource "aws_lb_listener_rule" "api" {
  listener_arn = aws_lb_listener.this.arn
  priority     = 200

  condition {
    path_pattern {
      values = ["/api"]  # <-- BUG
    }
  }

  action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.api.arn
  }
}
```

<details>
<summary>Explain the bug</summary>

**The problem:** The path pattern is `/api` (exact match), not `/api/*` (wildcard). Requests to `/api/users`, `/api/health`, or any sub-path do not match the rule. Only a request to exactly `/api` (with no trailing path) would match. Since the rule does not match, the ALB falls through to the default action and sends traffic to the web target group.

**The fix:** Use a wildcard pattern to match all paths under `/api/`:

```hcl
condition {
  path_pattern {
    values = ["/api/*", "/api"]
  }
}
```

Including both `/api/*` and `/api` ensures that requests with and without trailing paths are captured.

</details>

## Solutions

<details>
<summary>TODO 1 -- Application Load Balancer (alb.tf)</summary>

```hcl
resource "aws_lb" "this" {
  name               = "${var.project_name}-alb"
  load_balancer_type = "application"
  internal           = false
  subnets            = [for s in aws_subnet.public : s.id]
  security_groups    = [aws_security_group.alb.id]

  tags = { Name = "${var.project_name}-alb" }
}
```

</details>

<details>
<summary>TODO 2 -- Target Groups and Attachments (alb.tf)</summary>

```hcl
resource "aws_lb_target_group" "api" {
  name        = "${var.project_name}-api-tg"
  port        = 80
  protocol    = "HTTP"
  vpc_id      = aws_vpc.this.id
  target_type = "instance"

  health_check {
    path                = "/api/health"
    protocol            = "HTTP"
    matcher             = "200"
    interval            = 15
    healthy_threshold   = 2
    unhealthy_threshold = 3
  }

  tags = { Name = "${var.project_name}-api-tg" }
}

resource "aws_lb_target_group" "web_stable" {
  name        = "${var.project_name}-web-tg"
  port        = 80
  protocol    = "HTTP"
  vpc_id      = aws_vpc.this.id
  target_type = "instance"

  health_check {
    path                = "/"
    protocol            = "HTTP"
    matcher             = "200"
    interval            = 15
    healthy_threshold   = 2
    unhealthy_threshold = 3
  }

  tags = { Name = "${var.project_name}-web-stable-tg" }
}

resource "aws_lb_target_group" "web_canary" {
  name        = "${var.project_name}-canary-tg"
  port        = 80
  protocol    = "HTTP"
  vpc_id      = aws_vpc.this.id
  target_type = "instance"

  health_check {
    path                = "/"
    protocol            = "HTTP"
    matcher             = "200"
    interval            = 15
    healthy_threshold   = 2
    unhealthy_threshold = 3
  }

  tags = { Name = "${var.project_name}-web-canary-tg" }
}

resource "aws_lb_target_group_attachment" "api" {
  for_each = aws_instance.api

  target_group_arn = aws_lb_target_group.api.arn
  target_id        = each.value.id
  port             = 80
}

resource "aws_lb_target_group_attachment" "web_stable" {
  for_each = aws_instance.web

  target_group_arn = aws_lb_target_group.web_stable.arn
  target_id        = each.value.id
  port             = 80
}

resource "aws_lb_target_group_attachment" "web_canary" {
  target_group_arn = aws_lb_target_group.web_canary.arn
  target_id        = aws_instance.web_canary.id
  port             = 80
}
```

</details>

<details>
<summary>TODO 3 -- Default Listener with Weighted Target Groups (alb.tf)</summary>

```hcl
resource "aws_lb_listener" "this" {
  load_balancer_arn = aws_lb.this.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type = "forward"

    forward {
      target_group {
        arn    = aws_lb_target_group.web_stable.arn
        weight = 90
      }

      target_group {
        arn    = aws_lb_target_group.web_canary.arn
        weight = 10
      }
    }
  }

  tags = { Name = "${var.project_name}-listener" }
}
```

</details>

<details>
<summary>TODO 4 -- Path-Based Routing Rule for /api/* (alb.tf)</summary>

```hcl
resource "aws_lb_listener_rule" "api" {
  listener_arn = aws_lb_listener.this.arn
  priority     = 100

  condition {
    path_pattern {
      values = ["/api/*", "/api"]
    }
  }

  action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.api.arn
  }

  tags = { Name = "${var.project_name}-api-rule" }
}
```

</details>

<details>
<summary>TODO 5 -- Fixed Response Rule for /health (alb.tf)</summary>

```hcl
resource "aws_lb_listener_rule" "health" {
  listener_arn = aws_lb_listener.this.arn
  priority     = 50

  condition {
    path_pattern {
      values = ["/health"]
    }
  }

  action {
    type = "fixed-response"

    fixed_response {
      content_type = "application/json"
      message_body = "{\"status\":\"healthy\"}"
      status_code  = "200"
    }
  }

  tags = { Name = "${var.project_name}-health-rule" }
}
```

</details>

## Verify What You Learned

### Step 1 -- Confirm the ALB is active

```bash
aws elbv2 describe-load-balancers \
  --names "${var.project_name:-alb-routing-lab}-alb" \
  --query "LoadBalancers[0].{Type:Type,State:State.Code,DNS:DNSName}" \
  --output table
```

Expected:

```
--------------------------------------------------------------
|                  DescribeLoadBalancers                      |
+-------------------+--------+-------------------------------+
|       DNS         | State  |           Type                |
+-------------------+--------+-------------------------------+
| alb-routing-la... | active |         application           |
+-------------------+--------+-------------------------------+
```

### Step 2 -- Test path-based routing

```bash
curl http://$(terraform output -raw alb_dns_name)/api/
```

Expected: `{"service":"api","version":"v1","az":"us-east-1a"}` (or the other AZ).

### Step 3 -- Test the fixed response

```bash
curl http://$(terraform output -raw alb_dns_name)/health
```

Expected: `{"status":"healthy"}`

### Step 4 -- Test weighted routing (run multiple times)

```bash
for i in $(seq 1 10); do curl -s http://$(terraform output -raw alb_dns_name)/ ; echo; done
```

Expected: mostly `Web v1` responses with occasional `Web v2 (canary)` responses (~10%).

### Step 5 -- List listener rules

```bash
aws elbv2 describe-rules \
  --listener-arn $(aws elbv2 describe-listeners --load-balancer-arn $(aws elbv2 describe-load-balancers --names "${var.project_name:-alb-routing-lab}-alb" --query "LoadBalancers[0].LoadBalancerArn" --output text) --query "Listeners[0].ListenerArn" --output text) \
  --query "Rules[].{Priority:Priority,Conditions:Conditions[0].Field}" \
  --output table
```

Expected: rules at priority 50 (health), 100 (api), and default.

### Step 6 -- Verify no changes pending

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
terraform state list
```

Expected: no output (empty state).

## What's Next

In **Exercise 20 -- NACLs: Layered Security Patterns**, you will implement stateless network ACLs as an additional security layer on top of security groups. You will learn how rule ordering, ephemeral ports, and the stateless nature of NACLs create both power and pitfalls.

## Summary

- **ALB** operates at Layer 7 (HTTP/HTTPS) and can route based on path, hostname, headers, and query strings
- **Path-based routing** sends different URL paths to different target groups using listener rules
- **Weighted target groups** split traffic by percentage for canary deployments (e.g., 90/10)
- **Fixed-response actions** let the ALB respond directly without sending traffic to targets
- **Rule priority** determines evaluation order -- lower numbers are evaluated first
- ALB security groups control inbound access; backend SGs should reference the ALB SG

## Reference

| Resource | Purpose |
|----------|---------|
| `aws_lb` | Creates the Application Load Balancer |
| `aws_lb_target_group` | Defines health check and routing for targets |
| `aws_lb_listener` | Listens on port 80 with weighted default action |
| `aws_lb_listener_rule` | Path-based and fixed-response routing rules |

## Additional Resources

- [ALB Listener Rules (AWS Docs)](https://docs.aws.amazon.com/elasticloadbalancing/latest/application/listener-update-rules.html) -- how to create and manage listener rules
- [ALB Weighted Target Groups](https://docs.aws.amazon.com/elasticloadbalancing/latest/application/load-balancer-listeners.html#forward-actions) -- weighted routing configuration
- [ALB vs NLB Feature Comparison](https://aws.amazon.com/elasticloadbalancing/features/) -- choosing the right load balancer type
- [Terraform aws_lb_listener_rule Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb_listener_rule) -- Terraform documentation for listener rules
- [ALB Access Logs](https://docs.aws.amazon.com/elasticloadbalancing/latest/application/load-balancer-access-logs.html) -- enable access logging for debugging and auditing

## Apply Your Knowledge

- [ALB Host-Based Routing](https://docs.aws.amazon.com/elasticloadbalancing/latest/application/tutorial-load-balancer-routing.html) -- route traffic based on the Host header for multi-tenant architectures
- [ALB Authentication with Cognito](https://docs.aws.amazon.com/elasticloadbalancing/latest/application/listener-authenticate-users.html) -- add OIDC or Cognito authentication to ALB listener rules
- [Blue/Green Deployments with ALB](https://docs.aws.amazon.com/whitepapers/latest/blue-green-deployments/blue-green-deployments-on-aws.html) -- use weighted target groups for zero-downtime deployments

---

> *"Simplicity is a great virtue but it requires hard work to achieve it and education to appreciate it."*
> -- **Edsger Dijkstra**

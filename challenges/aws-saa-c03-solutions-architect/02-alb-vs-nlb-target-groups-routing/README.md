# 2. ALB vs NLB: Target Groups and Routing Rules

<!--
difficulty: basic
concepts: [alb, nlb, target-groups, path-based-routing, host-based-routing, health-checks, static-ip, source-ip-preservation]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: understand
prerequisites: [none]
aws_cost: ~$0.06/hr
-->

> **AWS Cost Warning:** ALB (~$0.0225/hr) + NLB (~$0.0225/hr) + 2x EC2 t3.micro (~$0.0104/hr each). Total ~$0.06/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Basic understanding of HTTP and TCP protocols

## Learning Objectives

After completing this exercise, you will be able to:

- **Identify** the architectural differences between ALB (Layer 7) and NLB (Layer 4) and when each is appropriate
- **Construct** an ALB with path-based routing rules that direct traffic to different target groups using Terraform
- **Construct** an NLB with a TCP listener that preserves client source IP addresses
- **Verify** that ALB routing rules distribute requests based on URL path and that NLB passes TCP traffic directly
- **Explain** why ALB terminates the HTTP connection (reverse proxy) while NLB passes packets without modification
- **Compare** health check behavior between ALB (HTTP-level) and NLB (TCP or HTTP)
- **Distinguish** use cases requiring ALB features (WAF, Lambda targets, content routing) from those requiring NLB features (static IPs, extreme performance, PrivateLink)

## Why ALB vs NLB

Choosing the right load balancer is one of the most frequently tested topics on the SAA-C03 exam, because the choice affects cost, performance, security, and application architecture. The Application Load Balancer operates at Layer 7 (HTTP/HTTPS) and understands request content -- it can route based on URL path, hostname, HTTP headers, query strings, and even source IP. This makes ALB the default choice for web applications, microservices, and any workload that benefits from content-based routing. ALB also integrates with WAF for web application security, can target Lambda functions directly, and supports WebSocket and HTTP/2. The trade-off is that ALB terminates the client connection and creates a new connection to the target, which means the target sees the ALB's IP as the source (the real client IP is in the `X-Forwarded-For` header).

The Network Load Balancer operates at Layer 4 (TCP/UDP/TLS) and routes packets based on IP protocol data without inspecting content. NLB provides static IP addresses (one per AZ, can attach Elastic IPs), preserves the client source IP by default, and handles millions of requests per second with ultra-low latency. NLB is the right choice when you need static IPs for allowlisting, when your protocol is not HTTP (TCP, UDP, gRPC without HTTP/2), when you need PrivateLink (VPC endpoint services), or when raw performance matters more than routing flexibility. The exam frequently presents scenarios where a specific requirement -- static IP, source IP preservation, non-HTTP protocol, or PrivateLink -- should immediately point you to NLB.

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
  default     = "lb-demo"
}
```

### `vpc.tf`

```hcl
data "aws_availability_zones" "available" {
  state = "available"
}

resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = var.project_name }
}

resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id
  tags   = { Name = var.project_name }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.this.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this.id
  }

  tags = { Name = "${var.project_name}-public" }
}

resource "aws_subnet" "public_a" {
  vpc_id                  = aws_vpc.this.id
  cidr_block              = "10.0.1.0/24"
  availability_zone       = data.aws_availability_zones.available.names[0]
  map_public_ip_on_launch = true
  tags                    = { Name = "${var.project_name}-public-a" }
}

resource "aws_subnet" "public_b" {
  vpc_id                  = aws_vpc.this.id
  cidr_block              = "10.0.2.0/24"
  availability_zone       = data.aws_availability_zones.available.names[1]
  map_public_ip_on_launch = true
  tags                    = { Name = "${var.project_name}-public-b" }
}

resource "aws_route_table_association" "public_a" {
  subnet_id      = aws_subnet.public_a.id
  route_table_id = aws_route_table.public.id
}

resource "aws_route_table_association" "public_b" {
  subnet_id      = aws_subnet.public_b.id
  route_table_id = aws_route_table.public.id
}
```

### `security.tf`

```hcl
# ------------------------------------------------------------------
# ALB security group: accepts HTTP from the internet.
# ALB always has a security group -- this is required.
# ------------------------------------------------------------------
resource "aws_security_group" "alb" {
  name_prefix = "${var.project_name}-alb-"
  vpc_id      = aws_vpc.this.id
  description = "Allow HTTP inbound to ALB"

  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "HTTP from internet"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "${var.project_name}-alb-sg" }
}

# ------------------------------------------------------------------
# EC2 security group: accepts traffic from ALB and from the VPC
# CIDR (NLB preserves source IP, so we allow the VPC range plus
# any external IP). For this demo, we allow port 80 from anywhere.
# ------------------------------------------------------------------
resource "aws_security_group" "ec2" {
  name_prefix = "${var.project_name}-ec2-"
  vpc_id      = aws_vpc.this.id
  description = "Allow HTTP to EC2 instances from LBs"

  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "HTTP from ALB/NLB and direct"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "${var.project_name}-ec2-sg" }
}
```

### `compute.tf`

```hcl
# ------------------------------------------------------------------
# Use the latest Amazon Linux 2023 AMI for the EC2 instances.
# ------------------------------------------------------------------
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
# EC2 instances: each runs a minimal web server that returns the
# instance ID and the request path. This lets us verify that ALB
# routing rules are directing traffic to the correct target group.
# ------------------------------------------------------------------
resource "aws_instance" "app_a" {
  ami                    = data.aws_ami.al2023.id
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.public_a.id
  vpc_security_group_ids = [aws_security_group.ec2.id]

  user_data = <<-USERDATA
    #!/bin/bash
    yum install -y httpd
    INSTANCE_ID=$(ec2-metadata -i | cut -d' ' -f2)

    # Serve /api/ requests
    mkdir -p /var/www/html/api
    echo "{\"instance\": \"$INSTANCE_ID\", \"service\": \"api\", \"path\": \"/api/\"}" > /var/www/html/api/index.html

    # Serve /web/ requests
    mkdir -p /var/www/html/web
    echo "<h1>Web Server: $INSTANCE_ID</h1><p>Service: web</p>" > /var/www/html/web/index.html

    # Root health check
    echo "{\"status\": \"healthy\", \"instance\": \"$INSTANCE_ID\"}" > /var/www/html/index.html

    systemctl enable httpd
    systemctl start httpd
  USERDATA

  tags = { Name = "${var.project_name}-app-a" }
}

resource "aws_instance" "app_b" {
  ami                    = data.aws_ami.al2023.id
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.public_b.id
  vpc_security_group_ids = [aws_security_group.ec2.id]

  user_data = <<-USERDATA
    #!/bin/bash
    yum install -y httpd
    INSTANCE_ID=$(ec2-metadata -i | cut -d' ' -f2)

    mkdir -p /var/www/html/api
    echo "{\"instance\": \"$INSTANCE_ID\", \"service\": \"api\", \"path\": \"/api/\"}" > /var/www/html/api/index.html

    mkdir -p /var/www/html/web
    echo "<h1>Web Server: $INSTANCE_ID</h1><p>Service: web</p>" > /var/www/html/web/index.html

    echo "{\"status\": \"healthy\", \"instance\": \"$INSTANCE_ID\"}" > /var/www/html/index.html

    systemctl enable httpd
    systemctl start httpd
  USERDATA

  tags = { Name = "${var.project_name}-app-b" }
}
```

### `alb.tf`

```hcl
# ------------------------------------------------------------------
# Application Load Balancer (Layer 7).
#
# ALB operates as a reverse proxy: it terminates the client's HTTP
# connection, inspects the request, and opens a new connection to
# the target. This is why:
# - Targets see the ALB's IP as the source (use X-Forwarded-For
#   for the real client IP)
# - ALB can route based on path, host, headers, query strings
# - ALB can integrate with WAF, Cognito, and Lambda targets
# ------------------------------------------------------------------
resource "aws_lb" "alb" {
  name               = "demo-alb"
  internal           = false
  load_balancer_type = "application"
  security_groups    = [aws_security_group.alb.id]
  subnets            = [aws_subnet.public_a.id, aws_subnet.public_b.id]

  tags = { Name = "demo-alb" }
}

# ------------------------------------------------------------------
# Target group for /api/* routes. Health check uses HTTP GET on /api/
# because ALB health checks operate at Layer 7.
# ------------------------------------------------------------------
resource "aws_lb_target_group" "api" {
  name     = "demo-api-tg"
  port     = 80
  protocol = "HTTP"
  vpc_id   = aws_vpc.this.id

  health_check {
    path                = "/api/"
    protocol            = "HTTP"
    healthy_threshold   = 2
    unhealthy_threshold = 3
    interval            = 15
    timeout             = 5
    matcher             = "200"
  }

  tags = { Name = "demo-api-tg" }
}

# ------------------------------------------------------------------
# Target group for /web/* routes.
# ------------------------------------------------------------------
resource "aws_lb_target_group" "web" {
  name     = "demo-web-tg"
  port     = 80
  protocol = "HTTP"
  vpc_id   = aws_vpc.this.id

  health_check {
    path                = "/web/"
    protocol            = "HTTP"
    healthy_threshold   = 2
    unhealthy_threshold = 3
    interval            = 15
    timeout             = 5
    matcher             = "200"
  }

  tags = { Name = "demo-web-tg" }
}

# ------------------------------------------------------------------
# Register instances in both target groups.
# Instance A goes to API target group, Instance B to Web target group.
# This demonstrates routing to different backends based on path.
# ------------------------------------------------------------------
resource "aws_lb_target_group_attachment" "api_a" {
  target_group_arn = aws_lb_target_group.api.arn
  target_id        = aws_instance.app_a.id
  port             = 80
}

resource "aws_lb_target_group_attachment" "web_b" {
  target_group_arn = aws_lb_target_group.web.arn
  target_id        = aws_instance.app_b.id
  port             = 80
}

# ------------------------------------------------------------------
# ALB Listener: the entry point. Default action returns 404 for
# paths that don't match any rule. Path-based rules below direct
# /api/* and /web/* to their respective target groups.
# ------------------------------------------------------------------
resource "aws_lb_listener" "alb_http" {
  load_balancer_arn = aws_lb.alb.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type = "fixed-response"

    fixed_response {
      content_type = "application/json"
      message_body = "{\"error\": \"not found\", \"hint\": \"try /api/ or /web/\"}"
      status_code  = "404"
    }
  }
}

resource "aws_lb_listener_rule" "api_path" {
  listener_arn = aws_lb_listener.alb_http.arn
  priority     = 100

  action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.api.arn
  }

  condition {
    path_pattern {
      values = ["/api/*"]
    }
  }
}

resource "aws_lb_listener_rule" "web_path" {
  listener_arn = aws_lb_listener.alb_http.arn
  priority     = 200

  action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.web.arn
  }

  condition {
    path_pattern {
      values = ["/web/*"]
    }
  }
}

# ------------------------------------------------------------------
# Network Load Balancer (Layer 4).
#
# NLB passes packets directly to targets without inspecting content.
# Key differences from ALB:
# - Preserves client source IP by default (no X-Forwarded-For needed)
# - Provides one static IP per AZ (can attach Elastic IPs)
# - Handles millions of connections per second with sub-millisecond
#   latency
# - Does NOT have security groups by default (you can opt in since
#   August 2023)
# - Required for AWS PrivateLink (VPC endpoint services)
# ------------------------------------------------------------------
resource "aws_lb" "nlb" {
  name               = "demo-nlb"
  internal           = false
  load_balancer_type = "network"
  subnets            = [aws_subnet.public_a.id, aws_subnet.public_b.id]

  # NLB does not require security_groups. Traffic flows directly
  # to targets, which must have their own security group rules.
  # Since Aug 2023, you CAN attach a security group to NLB, but
  # it is optional and disabled by default.

  tags = { Name = "demo-nlb" }
}

# ------------------------------------------------------------------
# NLB target group: TCP protocol. Health check can be TCP (just
# checks port connectivity) or HTTP (sends a GET request).
# TCP health checks are faster but less accurate than HTTP checks.
# ------------------------------------------------------------------
resource "aws_lb_target_group" "nlb_tcp" {
  name     = "demo-nlb-tcp-tg"
  port     = 80
  protocol = "TCP"
  vpc_id   = aws_vpc.this.id

  health_check {
    protocol            = "TCP"
    healthy_threshold   = 2
    unhealthy_threshold = 2
    interval            = 10
  }

  tags = { Name = "demo-nlb-tcp-tg" }
}

resource "aws_lb_target_group_attachment" "nlb_a" {
  target_group_arn = aws_lb_target_group.nlb_tcp.arn
  target_id        = aws_instance.app_a.id
  port             = 80
}

resource "aws_lb_target_group_attachment" "nlb_b" {
  target_group_arn = aws_lb_target_group.nlb_tcp.arn
  target_id        = aws_instance.app_b.id
  port             = 80
}

resource "aws_lb_listener" "nlb_tcp" {
  load_balancer_arn = aws_lb.nlb.arn
  port              = 80
  protocol          = "TCP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.nlb_tcp.arn
  }
}
```

### `outputs.tf`

```hcl
output "alb_dns_name" {
  description = "ALB DNS name (use for HTTP requests)"
  value       = aws_lb.alb.dns_name
}

output "nlb_dns_name" {
  description = "NLB DNS name (use for TCP/HTTP requests)"
  value       = aws_lb.nlb.dns_name
}

output "instance_a_id" {
  description = "Instance ID of app server A (API target)"
  value       = aws_instance.app_a.id
}

output "instance_b_id" {
  description = "Instance ID of app server B (Web target)"
  value       = aws_instance.app_b.id
}
```

## Step 2 -- Initialize and Apply

```bash
terraform init
terraform apply -auto-approve
```

Terraform will create the VPC, subnets, internet gateway, route table, security groups, two EC2 instances, ALB, NLB, target groups, listeners, and routing rules. Allow 2-3 minutes for the load balancers to become active and health checks to pass.

### Intermediate Verification

Wait for targets to become healthy:

```bash
ALB_ARN=$(aws elbv2 describe-load-balancers --names demo-alb --query 'LoadBalancers[0].LoadBalancerArn' --output text)

aws elbv2 describe-target-health --target-group-arn $(aws elbv2 describe-target-groups --load-balancer-arn $ALB_ARN --query 'TargetGroups[0].TargetGroupArn' --output text) --query 'TargetHealthDescriptions[*].{Target:Target.Id,Health:TargetHealth.State}' --output table
```

## Step 3 -- Test ALB Path-Based Routing

Use the ALB DNS name from the Terraform output:

```bash
ALB_DNS=$(terraform output -raw alb_dns_name)

# Request to /api/ -- should route to target group "api" (instance A)
curl -s "http://$ALB_DNS/api/" | jq .

# Request to /web/ -- should route to target group "web" (instance B)
curl -s "http://$ALB_DNS/web/"

# Request to root / -- should return 404 from the default fixed response
curl -s "http://$ALB_DNS/"
```

## Step 4 -- Test NLB TCP Pass-through

```bash
NLB_DNS=$(terraform output -raw nlb_dns_name)

# NLB forwards TCP traffic to both instances (round-robin).
# Run multiple requests to see traffic distributed:
for i in {1..4}; do curl -s "http://$NLB_DNS/" | jq -r '.instance'; done
```

You should see both instance IDs appear, demonstrating NLB load balancing across targets.

### Decision Table: ALB vs NLB

| Feature | ALB (Layer 7) | NLB (Layer 4) |
|---------|---------------|---------------|
| Protocol | HTTP, HTTPS, WebSocket, gRPC | TCP, UDP, TLS |
| Routing | Path, host, header, query string, source IP | Port-based only |
| Client source IP | Via X-Forwarded-For header | Preserved natively |
| Static IP per AZ | No (DNS-based only) | Yes (can attach EIP) |
| Security groups | Required | Optional (since Aug 2023) |
| WAF integration | Yes | No |
| Lambda targets | Yes | No |
| PrivateLink | No | Yes (VPC endpoint service) |
| Cross-zone LB | Enabled by default | Disabled by default |
| Performance | Thousands of RPS | Millions of RPS, sub-ms latency |
| Cost (hourly) | ~$0.0225/hr | ~$0.0225/hr |
| Best for | Web apps, microservices, APIs | TCP/UDP, static IPs, extreme perf |

## Common Mistakes

### 1. Using ALB for non-HTTP workloads

**Wrong approach:** Deploying an ALB in front of a TCP-based database proxy, a custom TCP protocol, or a UDP-based game server.

**What happens:** ALB only supports HTTP, HTTPS, WebSocket, and gRPC (over HTTP/2). Non-HTTP TCP connections are rejected at the listener level. The ALB cannot forward raw TCP packets because it operates as an HTTP reverse proxy that parses request headers.

**Fix:** Use NLB for any workload that is not HTTP-based. NLB handles TCP, UDP, and TLS without inspecting packet content:

```hcl
resource "aws_lb" "this" {
  load_balancer_type = "network"  # Not "application"
}
```

### 2. Forgetting that NLB has no security group by default

**Wrong approach:** Assuming NLB filters traffic like ALB does, then finding that all traffic reaches your instances.

**What happens:** By default, NLB does not have a security group. Traffic flows directly from the client to the target instance. Your instance's security group is the only network filter. Since NLB preserves the source IP, your instance security group rules see the real client IP -- but you must configure those rules to restrict access.

**Fix:** Either rely on the instance security group for access control, or explicitly attach a security group to the NLB (available since August 2023):

```hcl
resource "aws_lb" "nlb" {
  load_balancer_type     = "network"
  security_groups        = [aws_security_group.nlb.id]
  enforce_security_group_inbound_rules_on_private_link_traffic = "on"
}
```

### 3. Not accounting for cross-zone load balancing differences

**Wrong approach:** Assuming NLB distributes traffic evenly across all targets in all AZs, like ALB does.

**What happens:** Cross-zone load balancing is enabled by default on ALB but disabled by default on NLB. With NLB, each AZ node only sends traffic to targets in its own AZ. If you have 3 targets in AZ-A and 1 target in AZ-B, the AZ-B target receives 50% of total traffic (all traffic entering via AZ-B), causing overload.

**Fix:** Enable cross-zone load balancing on the NLB target group:

```hcl
resource "aws_lb_target_group" "nlb_tcp" {
  # ...
  connection_termination = false

  # Enable cross-zone to distribute evenly across all targets
  # regardless of AZ. Note: AWS charges for cross-AZ data transfer
  # when cross-zone is enabled on NLB.
}
```

Or set it at the NLB level:

```hcl
resource "aws_lb" "nlb" {
  enable_cross_zone_load_balancing = true
}
```

## Verify What You Learned

```bash
aws elbv2 describe-load-balancers --names demo-alb --query "LoadBalancers[0].Type" --output text
```

Expected: `application`

```bash
aws elbv2 describe-load-balancers --names demo-nlb --query "LoadBalancers[0].Type" --output text
```

Expected: `network`

```bash
aws elbv2 describe-rules --listener-arn $(aws elbv2 describe-listeners --load-balancer-arn $(aws elbv2 describe-load-balancers --names demo-alb --query 'LoadBalancers[0].LoadBalancerArn' --output text) --query 'Listeners[0].ListenerArn' --output text) --query 'Rules[*].{Priority:Priority,Conditions:Conditions[0].PathPatternConfig.Values[0]}' --output table
```

Expected: rules with priorities 100 (`/api/*`) and 200 (`/web/*`), plus the default rule.

```bash
ALB_DNS=$(terraform output -raw alb_dns_name)
curl -s "http://$ALB_DNS/api/" | jq -r '.service'
```

Expected: `api`

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

You deployed both types of Elastic Load Balancers and saw how Layer 7 routing differs from Layer 4 pass-through. In the next exercise, you will explore **EBS volume types** -- gp3, io2, st1, and sc1 -- and learn how to choose the right storage based on IOPS, throughput, and cost requirements.

## Summary

- **ALB** operates at Layer 7 (HTTP) and supports path-based, host-based, header-based, and query-string-based routing
- **NLB** operates at Layer 4 (TCP/UDP) and forwards packets without inspecting content, preserving client source IP
- ALB **terminates** the HTTP connection (reverse proxy); targets see the ALB's IP, not the client's
- NLB provides **static IP addresses** per AZ and supports Elastic IP attachment for allowlisting
- **Cross-zone load balancing** is enabled by default on ALB but disabled by default on NLB -- this affects traffic distribution
- ALB integrates with **WAF** and can target **Lambda functions**; NLB supports **PrivateLink** (VPC endpoint services)
- NLB has **no security group by default** (opt-in since August 2023); traffic filtering relies on instance security groups
- Choose ALB for web applications needing content routing; choose NLB for static IPs, non-HTTP protocols, or extreme performance

## Reference

- [Application Load Balancer](https://docs.aws.amazon.com/elasticloadbalancing/latest/application/introduction.html)
- [Network Load Balancer](https://docs.aws.amazon.com/elasticloadbalancing/latest/network/introduction.html)
- [Terraform aws_lb Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb)
- [Terraform aws_lb_target_group Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb_target_group)
- [Terraform aws_lb_listener_rule Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb_listener_rule)

## Additional Resources

- [Elastic Load Balancing Features Comparison](https://aws.amazon.com/elasticloadbalancing/features/) -- side-by-side feature matrix for ALB, NLB, GLB, and CLB
- [NLB Security Groups](https://docs.aws.amazon.com/elasticloadbalancing/latest/network/load-balancer-security-groups.html) -- how to opt in to security groups on NLB and the implications for PrivateLink
- [ALB Path-Based Routing](https://docs.aws.amazon.com/elasticloadbalancing/latest/application/load-balancer-listeners.html#rule-condition-types) -- complete list of condition types for listener rules
- [Cross-Zone Load Balancing](https://docs.aws.amazon.com/elasticloadbalancing/latest/userguide/how-elastic-load-balancing-works.html#cross-zone-load-balancing) -- behavior differences between ALB and NLB and cost implications

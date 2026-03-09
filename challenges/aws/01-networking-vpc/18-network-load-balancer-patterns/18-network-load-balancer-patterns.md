# 18. NLB: Static IPs and TCP Passthrough

<!--
difficulty: intermediate
concepts: [nlb, elastic-ip, target-groups, tcp-passthrough, health-checks, cross-zone-load-balancing]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: design
prerequisites: [01-your-first-vpc, 02-public-and-private-subnets]
aws_cost: ~$0.03/hr
-->

> **AWS Cost Warning:** This exercise creates a Network Load Balancer (~$0.0225/hr), Elastic IPs, and t3.micro EC2 instances (~$0.0104/hr each). Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Exercise 01 completed | Understand VPC basics |
| Exercise 02 completed | Understand public/private subnets |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Design** an NLB deployment with static Elastic IPs for IP whitelisting use cases
2. **Implement** TCP target groups with health checks for backend instances
3. **Differentiate** NLB from ALB: when to use each and why NLB preserves client IPs
4. **Configure** cross-zone load balancing and understand its cost implications

## Why NLB Matters

Application Load Balancers (ALBs) operate at Layer 7 (HTTP) and are the default choice for web applications. But some workloads need Layer 4 (TCP/UDP) capabilities that ALBs cannot provide. Network Load Balancers handle millions of requests per second with ultra-low latency, preserve the client source IP address without X-Forwarded-For headers, and -- critically -- support static IP addresses via Elastic IPs.

Static IPs matter when your consumers need to whitelist your endpoint in their firewall rules. ALB DNS names resolve to changing IP addresses, which makes IP whitelisting impossible. An NLB with Elastic IPs gives you a fixed set of IPs (one per AZ) that never change, even if the NLB is destroyed and recreated. NLB is also the only load balancer type that supports PrivateLink (VPC Endpoint Services), making it essential for service mesh architectures.

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
  default     = "nlb-patterns-lab"
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

resource "aws_subnet" "private" {
  for_each = toset(local.azs)

  vpc_id            = aws_vpc.this.id
  cidr_block        = cidrsubnet("10.0.0.0/16", 8, index(local.azs, each.value) + 10)
  availability_zone = each.value

  tags = { Name = "${var.project_name}-private-${each.value}" }
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

> **Best Practice:** NLB is the only AWS load balancer that supports static IP addresses via Elastic IPs. If your consumers need to whitelist a fixed IP in their firewall, NLB is the only option. ALB DNS names resolve to changing IPs and cannot be whitelisted by IP.

### `security.tf`

```hcl
# ------------------------------------------------------------------
# Security group for backend instances: NLB does not have a security
# group itself. Traffic arrives at the instance with the original
# client IP, so the instance SG must allow the client CIDR.
# ------------------------------------------------------------------
resource "aws_security_group" "backend" {
  name        = "${var.project_name}-backend-sg"
  description = "Allow TCP 80 from anywhere and all outbound"
  vpc_id      = aws_vpc.this.id

  tags = { Name = "${var.project_name}-backend-sg" }
}

resource "aws_vpc_security_group_ingress_rule" "http_in" {
  security_group_id = aws_security_group.backend.id
  cidr_ipv4         = "0.0.0.0/0"
  from_port         = 80
  to_port           = 80
  ip_protocol       = "tcp"
}

resource "aws_vpc_security_group_ingress_rule" "health_check" {
  security_group_id = aws_security_group.backend.id
  cidr_ipv4         = aws_vpc.this.cidr_block
  from_port         = 8080
  to_port           = 8080
  ip_protocol       = "tcp"
}

resource "aws_vpc_security_group_egress_rule" "all_out" {
  security_group_id = aws_security_group.backend.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}
```

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
# Backend instances: one per AZ running a simple HTTP server.
# ------------------------------------------------------------------
resource "aws_instance" "backend" {
  for_each = toset(local.azs)

  ami                    = data.aws_ami.amazon_linux_2023.id
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.public[each.value].id
  vpc_security_group_ids = [aws_security_group.backend.id]

  user_data = <<-EOF
    #!/bin/bash
    yum install -y httpd
    echo "Hello from ${each.value}" > /var/www/html/index.html
    systemctl enable httpd
    systemctl start httpd
  EOF

  tags = { Name = "${var.project_name}-backend-${each.value}" }
}
```

### `nlb.tf`

```hcl
# =======================================================
# TODO 1 -- Elastic IPs for the NLB (one per AZ)
# =======================================================
# Requirements:
#   - Create aws_eip resources using for_each over local.azs
#   - Set domain = "vpc"
#   - Tag with Name = "${var.project_name}-nlb-eip-${each.value}"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/eip
# Hint: These EIPs will be assigned to the NLB subnet mappings


# =======================================================
# TODO 2 -- Network Load Balancer with static EIPs
# =======================================================
# Requirements:
#   - Create an aws_lb with load_balancer_type = "network"
#   - Set internal = false for internet-facing
#   - Use dynamic "subnet_mapping" blocks to assign each public
#     subnet with its corresponding EIP
#   - Enable cross_zone_load_balancing = true
#   - Tag with Name = "${var.project_name}-nlb"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb
# Hint: Use for_each inside a dynamic block over local.azs


# =======================================================
# TODO 3 -- TCP Target Group with health checks
# =======================================================
# Requirements:
#   - Create an aws_lb_target_group for TCP port 80
#   - Set target_type = "instance"
#   - Configure health_check with:
#       protocol = "TCP"
#       port = "80"
#       healthy_threshold = 3
#       unhealthy_threshold = 3
#       interval = 10
#   - Tag with Name = "${var.project_name}-tg"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb_target_group


# =======================================================
# TODO 4 -- Target Group Attachments and Listener
# =======================================================
# Requirements:
#   - Create aws_lb_target_group_attachment for each backend instance
#     (use for_each over aws_instance.backend)
#   - Create an aws_lb_listener on port 80, protocol TCP
#   - Default action: forward to the target group
#
# Docs:
#   https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb_target_group_attachment
#   https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb_listener
```

### `outputs.tf`

```hcl
output "vpc_id" {
  description = "VPC ID"
  value       = aws_vpc.this.id
}

output "nlb_dns_name" {
  description = "NLB DNS name"
  value       = aws_lb.this.dns_name
}

output "nlb_eips" {
  description = "Elastic IPs assigned to the NLB"
  value       = { for az, eip in aws_eip.nlb : az => eip.public_ip }
}

output "target_group_arn" {
  description = "Target group ARN"
  value       = aws_lb_target_group.this.arn
}
```

## Spot the Bug

A colleague deployed an NLB with target instances but health checks fail for all targets even though the web servers are running. **What is wrong?**

```hcl
resource "aws_lb_target_group" "this" {
  name        = "my-tg"
  port        = 80
  protocol    = "TCP"
  vpc_id      = aws_vpc.this.id
  target_type = "ip"  # <-- BUG

  health_check {
    protocol            = "HTTP"
    path                = "/health"
    port                = "8080"
    healthy_threshold   = 3
    unhealthy_threshold = 3
    interval            = 10
  }
}

resource "aws_lb_target_group_attachment" "this" {
  for_each         = aws_instance.backend
  target_group_arn = aws_lb_target_group.this.arn
  target_id        = each.value.id
  port             = 80
}
```

<details>
<summary>Explain the bug</summary>

**The problem:** The target type is `"ip"` but the attachment uses `each.value.id` (an instance ID like `i-0abc123`). When `target_type = "ip"`, the `target_id` must be an IP address (e.g., `each.value.private_ip`). Additionally, the health check uses HTTP on port 8080 with a `/health` path, but the instances only serve HTTP on port 80 with no `/health` endpoint. The health check will receive 404 responses and mark all targets as unhealthy.

**The fix:** Use `target_type = "instance"` (matching the instance ID attachment) and configure the health check to match the actual service:

```hcl
resource "aws_lb_target_group" "this" {
  name        = "my-tg"
  port        = 80
  protocol    = "TCP"
  vpc_id      = aws_vpc.this.id
  target_type = "instance"

  health_check {
    protocol            = "TCP"
    port                = "80"
    healthy_threshold   = 3
    unhealthy_threshold = 3
    interval            = 10
  }
}
```

</details>

## Solutions

<details>
<summary>TODO 1 -- Elastic IPs for the NLB (nlb.tf)</summary>

```hcl
resource "aws_eip" "nlb" {
  for_each = toset(local.azs)
  domain   = "vpc"

  tags = { Name = "${var.project_name}-nlb-eip-${each.value}" }
}
```

</details>

<details>
<summary>TODO 2 -- Network Load Balancer with static EIPs (nlb.tf)</summary>

```hcl
resource "aws_lb" "this" {
  name               = "${var.project_name}-nlb"
  load_balancer_type = "network"
  internal           = false

  enable_cross_zone_load_balancing = true

  dynamic "subnet_mapping" {
    for_each = toset(local.azs)
    content {
      subnet_id     = aws_subnet.public[subnet_mapping.value].id
      allocation_id = aws_eip.nlb[subnet_mapping.value].id
    }
  }

  tags = { Name = "${var.project_name}-nlb" }
}
```

</details>

<details>
<summary>TODO 3 -- TCP Target Group with health checks (nlb.tf)</summary>

```hcl
resource "aws_lb_target_group" "this" {
  name        = "${var.project_name}-tg"
  port        = 80
  protocol    = "TCP"
  vpc_id      = aws_vpc.this.id
  target_type = "instance"

  health_check {
    protocol            = "TCP"
    port                = "80"
    healthy_threshold   = 3
    unhealthy_threshold = 3
    interval            = 10
  }

  tags = { Name = "${var.project_name}-tg" }
}
```

</details>

<details>
<summary>TODO 4 -- Target Group Attachments and Listener (nlb.tf)</summary>

```hcl
resource "aws_lb_target_group_attachment" "backend" {
  for_each = aws_instance.backend

  target_group_arn = aws_lb_target_group.this.arn
  target_id        = each.value.id
  port             = 80
}

resource "aws_lb_listener" "tcp" {
  load_balancer_arn = aws_lb.this.arn
  port              = 80
  protocol          = "TCP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.this.arn
  }

  tags = { Name = "${var.project_name}-listener" }
}
```

</details>

## Verify What You Learned

### Step 1 -- Confirm the NLB has static EIPs

```bash
aws ec2 describe-addresses \
  --filters "Name=tag:Name,Values=${var.project_name:-nlb-patterns-lab}-nlb-eip-*" \
  --query "Addresses[].{AZ:Tags[?Key=='Name']|[0].Value,PublicIP:PublicIp,AssociationId:AssociationId}" \
  --output table
```

Expected:

```
---------------------------------------------------------------
|                     DescribeAddresses                       |
+---------------------------------+----------+----------------+
|              AZ                 | AssociationId | PublicIP  |
+---------------------------------+----------+----------------+
|  nlb-patterns-lab-nlb-eip-us..  | eipassoc-... | 54.x.x.x |
|  nlb-patterns-lab-nlb-eip-us..  | eipassoc-... | 52.x.x.x |
+---------------------------------+----------+----------------+
```

### Step 2 -- Verify the NLB is active

```bash
aws elbv2 describe-load-balancers \
  --names "${var.project_name:-nlb-patterns-lab}-nlb" \
  --query "LoadBalancers[0].{Type:Type,State:State.Code,DNS:DNSName}" \
  --output table
```

Expected:

```
--------------------------------------------------------------
|                  DescribeLoadBalancers                      |
+------------+--------+-------------------------------------+
|    DNS     | State  |               Type                  |
+------------+--------+-------------------------------------+
| nlb-pat... | active |              network                |
+------------+--------+-------------------------------------+
```

### Step 3 -- Verify targets are healthy

```bash
aws elbv2 describe-target-health \
  --target-group-arn $(terraform output -raw target_group_arn) \
  --query "TargetHealthDescriptions[].{ID:Target.Id,Port:Target.Port,State:TargetHealth.State}" \
  --output table
```

Expected:

```
-------------------------------------------
|          DescribeTargetHealth           |
+-------------------+------+-------------+
|        ID         | Port |    State    |
+-------------------+------+-------------+
|  i-0abc123...     |  80  |  healthy    |
|  i-0def456...     |  80  |  healthy    |
+-------------------+------+-------------+
```

### Step 4 -- Test NLB connectivity

```bash
curl http://$(terraform output -raw nlb_dns_name)
```

Expected: `Hello from us-east-1a` or `Hello from us-east-1b` (varies with load balancing).

### Step 5 -- Verify no changes pending

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

In **Exercise 19 -- ALB: Path Routing, Host Routing, and Weighted Targets**, you will deploy an Application Load Balancer with advanced Layer 7 routing rules including path-based routing, host-based routing, and weighted target groups for canary deployments.

## Summary

- **NLB** operates at Layer 4 (TCP/UDP) with ultra-low latency and millions of requests per second
- NLB is the **only** AWS load balancer that supports **static IP addresses** via Elastic IPs
- NLB **preserves the client source IP** -- backend security groups must allow the client CIDR, not the NLB CIDR
- **Cross-zone load balancing** distributes traffic evenly across all AZs (enabled by default for ALB, opt-in for NLB)
- NLB **does not have a security group** -- traffic control is at the target instance level
- Use NLB when you need: static IPs, TCP/UDP passthrough, PrivateLink support, or extreme performance

## Reference

| Resource | Purpose |
|----------|---------|
| `aws_lb` | Creates the Network Load Balancer |
| `aws_lb_target_group` | Defines health check and routing for targets |
| `aws_lb_target_group_attachment` | Registers instances with the target group |
| `aws_lb_listener` | Listens on a port/protocol and forwards to targets |
| `aws_eip` | Provides static public IPs for the NLB |

## Additional Resources

- [NLB Documentation (AWS Docs)](https://docs.aws.amazon.com/elasticloadbalancing/latest/network/introduction.html) -- official guide to Network Load Balancers
- [NLB vs ALB Comparison](https://aws.amazon.com/elasticloadbalancing/features/) -- feature comparison between load balancer types
- [NLB Static IPs and PrivateLink](https://docs.aws.amazon.com/elasticloadbalancing/latest/network/create-network-load-balancer.html) -- static IP configuration guide
- [Cross-Zone Load Balancing](https://docs.aws.amazon.com/elasticloadbalancing/latest/network/network-load-balancers.html#cross-zone-load-balancing) -- how cross-zone affects traffic distribution and cost
- [Terraform aws_lb Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb) -- Terraform documentation for load balancers

## Apply Your Knowledge

- [NLB TLS Termination](https://docs.aws.amazon.com/elasticloadbalancing/latest/network/create-tls-listener.html) -- configure TLS listeners on NLB for encrypted TCP passthrough
- [NLB with AWS PrivateLink](https://docs.aws.amazon.com/vpc/latest/privatelink/create-endpoint-service.html) -- expose services privately across VPC boundaries using NLB-backed endpoint services
- [NLB Access Logs](https://docs.aws.amazon.com/elasticloadbalancing/latest/network/load-balancer-access-logs.html) -- enable TLS flow logs for auditing and compliance

---

> *"The web as I envisaged it, we have not seen it yet. The future is still so much bigger than the past."*
> -- **Tim Berners-Lee**

# 41. Gateway Load Balancer for Network Appliances

<!--
difficulty: intermediate
concepts: [gateway-load-balancer, gwlb, geneve-encapsulation, gwlb-endpoint, network-appliances, ids-ips, firewall, transparent-inspection]
tools: [terraform, aws-cli]
estimated_time: 50m
bloom_level: apply, evaluate
prerequisites: [none]
aws_cost: ~$0.05/hr
-->

> **AWS Cost Warning:** A Gateway Load Balancer costs ~$0.0125 per hour plus $0.004 per LCU-hour. EC2 instances simulating appliances cost ~$0.0104/hr each. GWLB endpoints cost ~$0.01/hr each. Total ~$0.05/hr. Remember to run `terraform destroy` when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Understanding of VPC networking | Subnets, route tables, security groups |
| Understanding of load balancer concepts | Completed exercise 02 (ALB vs NLB) |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Explain** how Gateway Load Balancer uses GENEVE encapsulation to transparently route traffic through third-party network appliances.
2. **Distinguish** GWLB from ALB/NLB: GWLB operates at Layer 3 (network), is transparent to the application, and preserves original source/destination IPs.
3. **Implement** a GWLB with a target group of simulated firewall appliances and a GWLB endpoint for traffic routing.
4. **Design** a traffic inspection architecture where all VPC ingress/egress traffic flows through security appliances.
5. **Evaluate** when GWLB is the right choice vs inline NLB or ALB-based inspection.

---

## Why This Matters

Gateway Load Balancer solves a specific problem that ALB and NLB cannot: transparent insertion of third-party network appliances (firewalls, IDS/IPS, DDoS protection, packet inspection) into the traffic path. The SAA-C03 exam tests this in scenarios where "all traffic must be inspected by a security appliance before reaching the application."

Before GWLB, inserting appliances into the traffic path required complex routing with source NAT, destination NAT, or proxy configurations. Appliances had to be aware of the routing topology, and scaling them was manual. GWLB makes appliances transparent: traffic enters the GWLB endpoint, gets encapsulated in GENEVE (a tunnel protocol), forwarded to an appliance for inspection, and returned to the GWLB which de-encapsulates and forwards it to the destination. The application never sees the appliance, and the appliance does not need to know about the application's network topology.

The exam typically presents GWLB in two patterns: centralized inspection VPC (all traffic from spoke VPCs routes through a security VPC with GWLB) and inline inspection (GWLB in the same VPC as the application, intercepting traffic via route table entries). Understanding the GWLB endpoint service model (similar to PrivateLink) is essential for answering these questions correctly.

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
  default     = "saa-ex41"
}
```

### `locals.tf`

```hcl
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

locals {
  azs = slice(data.aws_availability_zones.available.names, 0, 2)
}
```

### `vpc.tf`

```hcl
# ------------------------------------------------------------------
# Security VPC: hosts the GWLB and security appliances.
# In production, this is often a dedicated "inspection VPC."
# ------------------------------------------------------------------
resource "aws_vpc" "security" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = {
    Name = "${var.project_name}-security-vpc"
  }
}

resource "aws_subnet" "appliance" {
  count             = 2
  vpc_id            = aws_vpc.security.id
  cidr_block        = cidrsubnet("10.0.0.0/16", 8, count.index + 1)
  availability_zone = local.azs[count.index]

  tags = {
    Name = "${var.project_name}-appliance-${local.azs[count.index]}"
  }
}

resource "aws_internet_gateway" "security" {
  vpc_id = aws_vpc.security.id
  tags   = { Name = "${var.project_name}-security-igw" }
}

resource "aws_route_table" "appliance" {
  vpc_id = aws_vpc.security.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.security.id
  }

  tags = { Name = "${var.project_name}-appliance-rt" }
}

resource "aws_route_table_association" "appliance" {
  count          = 2
  subnet_id      = aws_subnet.appliance[count.index].id
  route_table_id = aws_route_table.appliance.id
}
```

### `security.tf`

```hcl
resource "aws_security_group" "appliance" {
  name_prefix = "${var.project_name}-appliance-"
  vpc_id      = aws_vpc.security.id
  description = "Security appliance - GENEVE port 6081"

  ingress {
    from_port   = 6081
    to_port     = 6081
    protocol    = "udp"
    cidr_blocks = ["10.0.0.0/16"]
    description = "GENEVE from GWLB"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "${var.project_name}-appliance-sg" }
}
```

### `alb.tf`

```hcl
# ============================================================
# TODO 1: Gateway Load Balancer
# ============================================================
# Create a GWLB that distributes traffic to security appliances.
#
# Requirements:
#   - Resource: aws_lb
#     - name = "${var.project_name}-gwlb"
#     - load_balancer_type = "gateway"
#     - subnets = appliance subnet IDs
#
#   - Resource: aws_lb_target_group
#     - name = "${var.project_name}-appliances"
#     - port = 6081
#     - protocol = "GENEVE"
#     - vpc_id = security VPC
#     - target_type = "instance"
#     - health_check:
#       - protocol = "TCP"
#       - port = 80 (or the appliance's health check port)
#
#   - Resource: aws_lb_listener
#     - load_balancer_arn = GWLB ARN
#     - default_action type = "forward"
#     - default_action target_group_arn = target group ARN
#
# Key concept: GWLB uses GENEVE (Generic Network Virtualization
# Encapsulation) on UDP port 6081. The original packet is
# encapsulated inside a GENEVE header, forwarded to the appliance,
# inspected, and returned via the same GENEVE tunnel.
#
# Docs: https://docs.aws.amazon.com/elasticloadbalancing/latest/gateway/introduction.html
# ============================================================


# ============================================================
# TODO 2: Simulated Security Appliance
# ============================================================
# Launch an EC2 instance that simulates a network security
# appliance. In production, this would be a Palo Alto, Fortinet,
# or Check Point virtual appliance from the AWS Marketplace.
#
# Requirements:
#   - Resource: aws_instance
#     - ami = Amazon Linux 2023
#     - instance_type = "t3.micro"
#     - subnet_id = first appliance subnet
#     - vpc_security_group_ids = appliance SG
#     - source_dest_check = false (CRITICAL for appliances)
#     - user_data to enable IP forwarding:
#       #!/bin/bash
#       sysctl -w net.ipv4.ip_forward=1
#       # Simple health check responder
#       yum install -y httpd
#       systemctl start httpd
#     - tags: Name = "${var.project_name}-appliance-1"
#
#   - Resource: aws_lb_target_group_attachment
#     - target_group_arn = appliance target group
#     - target_id = instance ID
#
# CRITICAL: source_dest_check = false is required for any
# instance that forwards traffic (appliances, NAT instances,
# routers). Without it, AWS drops packets where the instance
# is neither the source nor destination.
#
# Docs: https://docs.aws.amazon.com/vpc/latest/userguide/VPC_NAT_Instance.html#EIP_Disable_SrcDestCheck
# ============================================================


# ============================================================
# TODO 3: GWLB Endpoint Service and Endpoint
# ============================================================
# Create a VPC endpoint service for the GWLB, then create a
# GWLB endpoint in the security VPC (or a consumer VPC).
#
# Requirements:
#   - Resource: aws_vpc_endpoint_service
#     - acceptance_required = false
#     - gateway_load_balancer_arns = [GWLB ARN]
#     - tags: Name = "${var.project_name}-gwlbe-service"
#
#   - Resource: aws_vpc_endpoint
#     - vpc_id = security VPC (or consumer VPC)
#     - service_name = endpoint service service_name
#     - vpc_endpoint_type = "GatewayLoadBalancer"
#     - subnet_ids = [first appliance subnet]
#     - tags: Name = "${var.project_name}-gwlbe"
#
# The GWLB endpoint acts as the entry/exit point for traffic
# inspection. Route table entries point to the GWLB endpoint
# as a next hop, directing traffic through the appliances.
#
# Architecture flow:
#   Client -> IGW -> Route table -> GWLB Endpoint -> GWLB
#   -> GENEVE to appliance -> inspection -> GENEVE return
#   -> GWLB -> GWLB Endpoint -> destination instance
#
# Docs: https://docs.aws.amazon.com/elasticloadbalancing/latest/gateway/getting-started.html
# ============================================================
```

### `outputs.tf`

```hcl
output "security_vpc_id" {
  value = aws_vpc.security.id
}

output "appliance_subnet_ids" {
  value = aws_subnet.appliance[*].id
}
```

---

## Spot the Bug

A team deploys a GWLB with endpoints, but traffic from the consumer VPC is not being inspected:

```hcl
# Consumer VPC route table
resource "aws_route" "to_inspection" {
  route_table_id         = aws_route_table.consumer_public.id
  destination_cidr_block = "0.0.0.0/0"
  gateway_id             = aws_internet_gateway.consumer.id  # Goes directly to IGW
}
```

<details>
<summary>Explain the bug</summary>

**The route table sends traffic directly to the Internet Gateway, bypassing the GWLB endpoint entirely.** For traffic inspection to work, the route must direct traffic to the GWLB endpoint as a next hop.

The architecture requires two route table changes:

1. **Ingress route table** (associated with the IGW): routes incoming traffic to the GWLB endpoint before it reaches the application subnets.
2. **Application subnet route table**: routes outbound traffic to the GWLB endpoint before it reaches the IGW.

**The fix:**

```hcl
# Route table for application subnets: outbound via GWLB endpoint
resource "aws_route" "outbound_via_gwlbe" {
  route_table_id         = aws_route_table.consumer_private.id
  destination_cidr_block = "0.0.0.0/0"
  vpc_endpoint_id        = aws_vpc_endpoint.gwlbe.id  # GWLB endpoint, NOT IGW
}

# Ingress route table for IGW: inbound via GWLB endpoint
resource "aws_route_table" "ingress" {
  vpc_id = aws_vpc.consumer.id

  route {
    destination_cidr_block = "10.1.0.0/24"  # Application subnet CIDR
    vpc_endpoint_id        = aws_vpc_endpoint.gwlbe.id
  }

  tags = { Name = "ingress-via-gwlbe" }
}

resource "aws_route_table_association" "ingress_igw" {
  gateway_id     = aws_internet_gateway.consumer.id
  route_table_id = aws_route_table.ingress.id
}
```

**Key point for the SAA-C03:** GWLB traffic inspection requires route table modifications at both the ingress (IGW) and egress (application subnet) levels. Simply deploying a GWLB does not inspect traffic -- you must route traffic through the GWLB endpoint.

</details>

---

## GWLB vs ALB vs NLB Comparison

| Feature | ALB | NLB | GWLB |
|---------|-----|-----|------|
| **Layer** | 7 (HTTP/HTTPS) | 4 (TCP/UDP/TLS) | 3 (IP, all traffic) |
| **Protocol** | HTTP, HTTPS, gRPC | TCP, UDP, TLS | GENEVE (encapsulation) |
| **Use case** | Web apps, APIs | High-perf TCP, static IPs | Network appliances |
| **Traffic modification** | Terminates connection | Pass-through | Transparent (preserves original) |
| **Source IP preservation** | Via X-Forwarded-For header | Yes (with proxy protocol optional) | Yes (GENEVE preserves original) |
| **Cross-zone** | Enabled by default (free) | Disabled by default (charges) | Enabled by default |
| **Target types** | Instance, IP, Lambda | Instance, IP, ALB | Instance, IP |
| **Health checks** | HTTP, HTTPS | TCP, HTTP, HTTPS | TCP, HTTP, HTTPS |
| **Pricing** | ~$0.0225/hr + LCU | ~$0.0225/hr + NLCU | ~$0.0125/hr + GLCU |

### When to Use GWLB

```
Does traffic need to pass through a third-party appliance?
+-- Yes -> Is the appliance a network-level device (firewall, IDS/IPS)?
|   +-- Yes -> GWLB (transparent, GENEVE encapsulation)
|   +-- No (HTTP-level WAF) -> ALB with AWS WAF
+-- No -> Use ALB (HTTP/HTTPS) or NLB (TCP/UDP)
```

---

## Solutions

<details>
<summary>TODO 1 -- Gateway Load Balancer -- `alb.tf`</summary>

```hcl
resource "aws_lb" "gwlb" {
  name               = "${var.project_name}-gwlb"
  load_balancer_type = "gateway"
  subnets            = aws_subnet.appliance[*].id

  tags = {
    Name = "${var.project_name}-gwlb"
  }
}

resource "aws_lb_target_group" "appliances" {
  name        = "${var.project_name}-appliances"
  port        = 6081
  protocol    = "GENEVE"
  vpc_id      = aws_vpc.security.id
  target_type = "instance"

  health_check {
    protocol = "TCP"
    port     = 80
  }

  tags = {
    Name = "${var.project_name}-appliances-tg"
  }
}

resource "aws_lb_listener" "gwlb" {
  load_balancer_arn = aws_lb.gwlb.arn

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.appliances.arn
  }
}

output "gwlb_arn" {
  value = aws_lb.gwlb.arn
}
```

</details>

<details>
<summary>TODO 2 -- Simulated Security Appliance -- `compute.tf`</summary>

```hcl
resource "aws_instance" "appliance" {
  ami                    = data.aws_ami.al2023.id
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.appliance[0].id
  vpc_security_group_ids = [aws_security_group.appliance.id]
  source_dest_check      = false  # CRITICAL for traffic forwarding

  user_data = base64encode(<<-EOF
    #!/bin/bash
    # Enable IP forwarding (required for traffic passthrough)
    sysctl -w net.ipv4.ip_forward=1
    echo "net.ipv4.ip_forward = 1" >> /etc/sysctl.conf

    # Simple health check responder on port 80
    yum install -y httpd
    echo "HEALTHY" > /var/www/html/index.html
    systemctl start httpd
  EOF
  )

  tags = {
    Name = "${var.project_name}-appliance-1"
  }
}

resource "aws_lb_target_group_attachment" "appliance" {
  target_group_arn = aws_lb_target_group.appliances.arn
  target_id        = aws_instance.appliance.id
}

output "appliance_instance_id" {
  value = aws_instance.appliance.id
}
```

</details>

<details>
<summary>TODO 3 -- GWLB Endpoint Service and Endpoint -- `alb.tf`</summary>

```hcl
resource "aws_vpc_endpoint_service" "gwlb" {
  acceptance_required        = false
  gateway_load_balancer_arns = [aws_lb.gwlb.arn]

  tags = {
    Name = "${var.project_name}-gwlbe-service"
  }
}

resource "aws_vpc_endpoint" "gwlbe" {
  vpc_id            = aws_vpc.security.id
  service_name      = aws_vpc_endpoint_service.gwlb.service_name
  vpc_endpoint_type = "GatewayLoadBalancer"
  subnet_ids        = [aws_subnet.appliance[0].id]

  tags = {
    Name = "${var.project_name}-gwlbe"
  }
}

output "gwlbe_id" {
  value = aws_vpc_endpoint.gwlbe.id
}

output "endpoint_service_name" {
  value = aws_vpc_endpoint_service.gwlb.service_name
}
```

In a cross-VPC architecture, the endpoint would be created in the consumer VPC instead, and the endpoint service would allow the consumer account's VPC to connect. This is the centralized inspection model.

</details>

---

## Verify What You Learned

```bash
# Verify GWLB exists
aws elbv2 describe-load-balancers \
  --names saa-ex41-gwlb \
  --query "LoadBalancers[0].{Type:Type,State:State.Code,Scheme:Scheme}" \
  --output table
```

Expected: Type=gateway, State=active

```bash
# Verify target group uses GENEVE protocol
aws elbv2 describe-target-groups \
  --names saa-ex41-appliances \
  --query "TargetGroups[0].{Protocol:Protocol,Port:Port,TargetType:TargetType}" \
  --output table
```

Expected: Protocol=GENEVE, Port=6081

```bash
# Verify appliance has source_dest_check disabled
aws ec2 describe-instances \
  --filters "Name=tag:Name,Values=saa-ex41-appliance-1" "Name=instance-state-name,Values=running" \
  --query "Reservations[0].Instances[0].SourceDestCheck" --output text
```

Expected: `false`

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify GWLB is deleted:

```bash
aws elbv2 describe-load-balancers \
  --names saa-ex41-gwlb 2>&1 | grep -c "not found" || echo "Check if GWLB is deleted"
```

---

## What's Next

You deployed a Gateway Load Balancer for transparent network appliance insertion. In the next exercise, you will configure **ALB Authentication with Cognito and OIDC** -- adding built-in user authentication to your load balancer without modifying application code.

---

## Summary

- **GWLB** is a Layer 3 load balancer designed for transparent traffic inspection by third-party network appliances
- GWLB uses **GENEVE encapsulation** (UDP port 6081) to forward original packets to appliances without modification
- Appliance instances must have **`source_dest_check = false`** and IP forwarding enabled
- **GWLB endpoints** (similar to PrivateLink) are the entry/exit points -- route table entries point to them as next hops
- The inspection architecture requires **route table changes** at both ingress (IGW) and egress (application subnet) levels
- GWLB is the answer when the exam mentions "inline security appliance," "traffic inspection," "IDS/IPS," or "virtual firewall"
- For HTTP-level inspection (WAF rules), use **ALB + AWS WAF** instead of GWLB
- Centralized inspection VPC pattern: all spoke VPCs route through a shared security VPC with GWLB
- GWLB pricing: ~$0.0125/hr + $0.004/GLCU-hr (cheaper per-hour than ALB/NLB)

---

## Reference

- [Gateway Load Balancer](https://docs.aws.amazon.com/elasticloadbalancing/latest/gateway/introduction.html)
- [GWLB Getting Started](https://docs.aws.amazon.com/elasticloadbalancing/latest/gateway/getting-started.html)
- [Terraform aws_lb (gateway type)](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb)
- [VPC Endpoint Services](https://docs.aws.amazon.com/vpc/latest/privatelink/vpc-endpoint-services-gwlbe.html)

## Additional Resources

- [GENEVE Protocol RFC 8926](https://datatracker.ietf.org/doc/html/rfc8926) -- the tunnel protocol GWLB uses
- [Centralized Inspection Architecture](https://docs.aws.amazon.com/prescriptive-guidance/latest/inline-traffic-inspection-third-party-appliances/architecture.html) -- AWS reference architecture for inspection VPCs
- [GWLB Partners](https://aws.amazon.com/elasticloadbalancing/gateway-load-balancer-partners/) -- supported third-party appliances (Palo Alto, Fortinet, Check Point)
- [GWLB Pricing](https://aws.amazon.com/elasticloadbalancing/pricing/) -- per-hour and per-GLCU cost breakdown

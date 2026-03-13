# 17. VPC with Public/Private Subnets, Route Tables, and Internet Gateway

<!--
difficulty: basic
concepts: [vpc, subnets, cidr-notation, route-tables, internet-gateway, public-subnet, private-subnet, reserved-ips, implicit-router]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: understand
prerequisites: [none]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** A VPC, subnets, route tables, and Internet Gateway have no hourly cost. You pay only for data transfer if you launch instances. Total ~$0.01/hr for this exercise. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Basic understanding of IP addressing (dotted quad notation)

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** CIDR notation and how to calculate the number of usable IPs in a subnet
- **Identify** the 5 IP addresses AWS reserves in every subnet and why each is reserved
- **Construct** a VPC with public and private subnets across multiple Availability Zones using Terraform
- **Distinguish** a public subnet (route to IGW) from a private subnet (no route to IGW) based on route table configuration
- **Associate** route tables with subnets and verify that traffic flows through the correct gateway
- **Describe** the role of the implicit VPC router and how route table entries control packet forwarding

## Why VPC Architecture Matters

A VPC is the foundation of every AWS deployment and one of the most heavily tested topics on the SAA-C03. The exam presents scenarios where workloads need isolation, internet access, or communication between tiers, and the correct answer depends on understanding how subnets, route tables, and gateways interact. A subnet is not inherently "public" or "private" -- it becomes public only when its associated route table has a route to an Internet Gateway. This distinction is critical: placing a database in a subnet with an IGW route exposes it to the internet, even if its security group blocks all inbound traffic (defense in depth requires both network and host-level controls).

CIDR notation determines your address space. A /16 VPC gives you 65,536 addresses. A /24 subnet gives you 256 addresses, but only 251 are usable because AWS reserves 5 in every subnet: the network address (.0), the VPC router (.1), the DNS server (.2), a reserved-for-future-use address (.3), and the broadcast address (.255). If you create a /28 subnet (16 addresses), only 11 are usable -- and that might not be enough for an EKS node group or a Lambda ENI burst. The exam tests whether you can calculate usable IPs and identify when a subnet is too small.

Route tables are the control plane of VPC networking. Every subnet must be associated with exactly one route table (the main route table is used by default). The VPC implicit router evaluates the most specific matching route for each packet. A route to `0.0.0.0/0` via an IGW makes a subnet public. A route to `0.0.0.0/0` via a NAT Gateway makes a subnet private-with-outbound-access. No default route means the subnet is fully isolated. Understanding this routing model is essential for designing multi-tier architectures.

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
  default     = "vpc-demo"
}
```

### `vpc.tf`

```hcl
# ------------------------------------------------------------------
# Data source: fetch AZs so subnets span multiple zones.
# Multi-AZ placement is required for high availability and is
# a prerequisite for many AWS services (ALB, RDS Multi-AZ, etc.).
# ------------------------------------------------------------------
data "aws_availability_zones" "available" {
  state = "available"
}

# ------------------------------------------------------------------
# VPC: 10.0.0.0/16 gives 65,536 IP addresses.
#
# Architect decision: /16 is the largest CIDR AWS allows for a VPC
# and provides room to create many /24 subnets (up to 256).
# Smaller VPCs (/20, /22) are appropriate when you need to conserve
# address space for VPC peering (CIDRs must not overlap).
#
# enable_dns_support and enable_dns_hostnames are required for:
# - VPC endpoints with private DNS
# - Route 53 private hosted zones
# - RDS/ElastiCache DNS resolution
# ------------------------------------------------------------------
resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = {
    Name = var.project_name
  }
}

# ------------------------------------------------------------------
# Internet Gateway: the bridge between your VPC and the internet.
#
# Key facts for the exam:
# - One IGW per VPC (you cannot attach multiple).
# - It is horizontally scaled and HA by default (no single AZ).
# - It performs 1:1 NAT for instances with public IPs.
# - Detaching the IGW instantly cuts all internet access for the VPC.
# ------------------------------------------------------------------
resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id

  tags = {
    Name = "${var.project_name}-igw"
  }
}

# ------------------------------------------------------------------
# Public Subnets: 10.0.1.0/24 and 10.0.2.0/24
#
# Each /24 provides 256 addresses, minus 5 reserved = 251 usable.
# AWS reserved IPs in every subnet:
#   .0   = Network address
#   .1   = VPC router (implicit router for all route table lookups)
#   .2   = DNS server (VPC base + 2, always at x.x.x.2)
#   .3   = Reserved for future use
#   .255 = Broadcast (AWS does not support broadcast, but reserves it)
#
# map_public_ip_on_launch = true means EC2 instances launched here
# automatically get a public IP. This is what makes it convenient
# for public-facing workloads, but the route table is what actually
# makes the subnet "public."
# ------------------------------------------------------------------
resource "aws_subnet" "public_a" {
  vpc_id                  = aws_vpc.this.id
  cidr_block              = "10.0.1.0/24"
  availability_zone       = data.aws_availability_zones.available.names[0]
  map_public_ip_on_launch = true

  tags = {
    Name = "${var.project_name}-public-a"
    Tier = "public"
  }
}

resource "aws_subnet" "public_b" {
  vpc_id                  = aws_vpc.this.id
  cidr_block              = "10.0.2.0/24"
  availability_zone       = data.aws_availability_zones.available.names[1]
  map_public_ip_on_launch = true

  tags = {
    Name = "${var.project_name}-public-b"
    Tier = "public"
  }
}

# ------------------------------------------------------------------
# Private Subnets: 10.0.10.0/24 and 10.0.11.0/24
#
# Note the gap between 10.0.2.0/24 and 10.0.10.0/24. This leaves
# room for additional public subnets (10.0.3.0/24 through 10.0.9.0/24)
# without re-addressing. Planning CIDR ranges upfront prevents
# painful subnet migrations later.
#
# No map_public_ip_on_launch -- instances here have private IPs only.
# ------------------------------------------------------------------
resource "aws_subnet" "private_a" {
  vpc_id            = aws_vpc.this.id
  cidr_block        = "10.0.10.0/24"
  availability_zone = data.aws_availability_zones.available.names[0]

  tags = {
    Name = "${var.project_name}-private-a"
    Tier = "private"
  }
}

resource "aws_subnet" "private_b" {
  vpc_id            = aws_vpc.this.id
  cidr_block        = "10.0.11.0/24"
  availability_zone = data.aws_availability_zones.available.names[1]

  tags = {
    Name = "${var.project_name}-private-b"
    Tier = "private"
  }
}

# ------------------------------------------------------------------
# Public Route Table: routes internet-bound traffic to the IGW.
#
# The local route (10.0.0.0/16 -> local) is implicit and cannot
# be removed. It ensures all intra-VPC traffic stays within the VPC.
# The 0.0.0.0/0 -> IGW route sends everything else to the internet.
# ------------------------------------------------------------------
resource "aws_route_table" "public" {
  vpc_id = aws_vpc.this.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this.id
  }

  tags = {
    Name = "${var.project_name}-public-rt"
  }
}

# ------------------------------------------------------------------
# Associate public subnets with the public route table.
#
# Without explicit association, subnets use the VPC's main route
# table (which has no IGW route by default). This is a common
# mistake: creating the route table but forgetting the association.
# ------------------------------------------------------------------
resource "aws_route_table_association" "public_a" {
  subnet_id      = aws_subnet.public_a.id
  route_table_id = aws_route_table.public.id
}

resource "aws_route_table_association" "public_b" {
  subnet_id      = aws_subnet.public_b.id
  route_table_id = aws_route_table.public.id
}

# ------------------------------------------------------------------
# Private Route Table: no route to IGW = no internet access.
#
# The only route is the implicit local route (10.0.0.0/16 -> local).
# Resources in these subnets can communicate with other VPC
# resources but cannot reach the internet. To add outbound-only
# internet access, you would add a route to a NAT Gateway here
# (covered in exercise 18).
# ------------------------------------------------------------------
resource "aws_route_table" "private" {
  vpc_id = aws_vpc.this.id

  tags = {
    Name = "${var.project_name}-private-rt"
  }
}

resource "aws_route_table_association" "private_a" {
  subnet_id      = aws_subnet.private_a.id
  route_table_id = aws_route_table.private.id
}

resource "aws_route_table_association" "private_b" {
  subnet_id      = aws_subnet.private_b.id
  route_table_id = aws_route_table.private.id
}
```

### `outputs.tf`

```hcl
output "vpc_id" {
  description = "VPC ID"
  value       = aws_vpc.this.id
}

output "vpc_cidr" {
  description = "VPC CIDR block"
  value       = aws_vpc.this.cidr_block
}

output "public_subnet_ids" {
  description = "Public subnet IDs"
  value       = [aws_subnet.public_a.id, aws_subnet.public_b.id]
}

output "private_subnet_ids" {
  description = "Private subnet IDs"
  value       = [aws_subnet.private_a.id, aws_subnet.private_b.id]
}

output "public_route_table_id" {
  description = "Public route table ID"
  value       = aws_route_table.public.id
}

output "private_route_table_id" {
  description = "Private route table ID"
  value       = aws_route_table.private.id
}

output "igw_id" {
  description = "Internet Gateway ID"
  value       = aws_internet_gateway.this.id
}
```

## Step 2 -- Deploy and Verify

```bash
terraform init
terraform apply -auto-approve
```

### CIDR Quick Reference

| CIDR | Addresses | Usable (minus 5) | Common Use |
|------|-----------|-------------------|------------|
| /16 | 65,536 | 65,531 | VPC |
| /20 | 4,096 | 4,091 | Large subnet |
| /24 | 256 | 251 | Standard subnet |
| /26 | 64 | 59 | Small subnet |
| /28 | 16 | 11 | Minimum useful subnet |

### Verify Route Table Differences

```bash
# Public route table -- should show 0.0.0.0/0 -> IGW
aws ec2 describe-route-tables \
  --route-table-ids $(terraform output -raw public_route_table_id) \
  --query "RouteTables[0].Routes[*].{Dest:DestinationCidrBlock,Target:GatewayId}" \
  --output table

# Private route table -- should show only local route
aws ec2 describe-route-tables \
  --route-table-ids $(terraform output -raw private_route_table_id) \
  --query "RouteTables[0].Routes[*].{Dest:DestinationCidrBlock,Target:GatewayId}" \
  --output table
```

## Common Mistakes

### 1. Forgetting route table association

**Wrong approach:** Creating a public route table with an IGW route but not associating it with the subnet:

```hcl
resource "aws_route_table" "public" {
  vpc_id = aws_vpc.this.id
  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this.id
  }
}

# Missing: aws_route_table_association!
```

**What happens:** The subnet uses the VPC main route table, which has no IGW route. Instances in the subnet cannot reach the internet, and you get timeout errors on outbound connections. The Terraform apply succeeds without error -- the misconfiguration is silent.

**Fix:** Always create explicit `aws_route_table_association` resources for every subnet:

```hcl
resource "aws_route_table_association" "public_a" {
  subnet_id      = aws_subnet.public_a.id
  route_table_id = aws_route_table.public.id
}
```

### 2. Overlapping CIDR blocks

**Wrong approach:** Creating subnets with overlapping CIDRs:

```hcl
resource "aws_subnet" "a" {
  cidr_block = "10.0.1.0/24"   # 10.0.1.0 - 10.0.1.255
}
resource "aws_subnet" "b" {
  cidr_block = "10.0.1.0/25"   # 10.0.1.0 - 10.0.1.127 (overlaps!)
}
```

**What happens:** `terraform apply` fails with `InvalidSubnet.Conflict: The CIDR block conflicts with another subnet`. AWS requires that all subnet CIDRs within a VPC be non-overlapping.

**Fix:** Plan your CIDR allocation upfront. Use a consistent scheme: public subnets in 10.0.1.0/24 through 10.0.9.0/24, private subnets in 10.0.10.0/24 through 10.0.19.0/24, database subnets in 10.0.20.0/24 through 10.0.29.0/24.

### 3. Thinking map_public_ip_on_launch makes a subnet public

**Wrong approach:** Setting `map_public_ip_on_launch = true` but not adding a route to the IGW.

**What happens:** Instances get public IPs but cannot communicate with the internet because the route table has no IGW route. Traffic destined for the internet hits the local route and gets dropped. The public IP is allocated but useless.

**Fix:** A subnet is public only when its route table has a `0.0.0.0/0` route to an Internet Gateway. The `map_public_ip_on_launch` flag is a convenience for auto-assigning public IPs, not a networking control.

## Verify What You Learned

```bash
# Verify VPC exists with correct CIDR
aws ec2 describe-vpcs \
  --vpc-ids $(terraform output -raw vpc_id) \
  --query "Vpcs[0].CidrBlock" \
  --output text
```

Expected: `10.0.0.0/16`

```bash
# Verify 4 subnets exist
aws ec2 describe-subnets \
  --filters "Name=vpc-id,Values=$(terraform output -raw vpc_id)" \
  --query "length(Subnets)"
```

Expected: `4`

```bash
# Verify public subnets have map_public_ip_on_launch
aws ec2 describe-subnets \
  --subnet-ids $(terraform output -json public_subnet_ids | jq -r '.[0]') \
  --query "Subnets[0].MapPublicIpOnLaunch" \
  --output text
```

Expected: `True`

```bash
# Verify IGW is attached to VPC
aws ec2 describe-internet-gateways \
  --internet-gateway-ids $(terraform output -raw igw_id) \
  --query "InternetGateways[0].Attachments[0].State" \
  --output text
```

Expected: `available`

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

You built a VPC with public and private subnets, but the private subnets have no internet access at all. In the next exercise, you will deploy **NAT Gateway and NAT Instance** to give private subnets outbound-only internet access, and compare the cost, availability, and performance trade-offs between the managed and self-managed approaches.

## Summary

- A **VPC** defines an isolated network with a CIDR block; /16 is the maximum size (65,536 IPs)
- AWS **reserves 5 IPs** in every subnet: .0 (network), .1 (router), .2 (DNS), .3 (future), .255 (broadcast)
- A subnet becomes **public** when its route table has a `0.0.0.0/0` route to an Internet Gateway
- A subnet is **private** when its route table has no route to an IGW (only the implicit local route)
- **Route table association** is required -- without it, the subnet uses the main route table (which has no IGW route by default)
- **CIDR blocks** within a VPC must not overlap; plan your address space before creating subnets
- `map_public_ip_on_launch` assigns public IPs to instances but does **not** make a subnet public by itself
- The **implicit VPC router** at .1 evaluates the most specific matching route for each packet

## Reference

- [Amazon VPC User Guide](https://docs.aws.amazon.com/vpc/latest/userguide/what-is-amazon-vpc.html)
- [VPC and Subnet Sizing](https://docs.aws.amazon.com/vpc/latest/userguide/vpc-cidr-blocks.html)
- [Route Tables](https://docs.aws.amazon.com/vpc/latest/userguide/VPC_Route_Tables.html)
- [Internet Gateways](https://docs.aws.amazon.com/vpc/latest/userguide/VPC_Internet_Gateway.html)
- [Terraform aws_vpc Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc)

## Additional Resources

- [VPC CIDR Block Association](https://docs.aws.amazon.com/vpc/latest/userguide/vpc-cidr-blocks.html#add-cidr-block-restrictions) -- adding secondary CIDRs when you run out of address space
- [Subnet Sizing Best Practices](https://docs.aws.amazon.com/vpc/latest/userguide/subnet-sizing.html) -- guidelines for choosing subnet sizes based on workload type
- [AWS VPC Quotas](https://docs.aws.amazon.com/vpc/latest/userguide/amazon-vpc-limits.html) -- default limits: 5 VPCs per region, 200 subnets per VPC, 200 route tables per VPC
- [VPC IP Addressing](https://docs.aws.amazon.com/vpc/latest/userguide/vpc-ip-addressing.html) -- IPv4 and IPv6 dual-stack considerations

# 30. IPv6 Dual-Stack VPC

<!--
difficulty: intermediate
concepts: [ipv6, dual-stack, egress-only-igw, ipv6-cidr, ipv6-security-groups, amazon-provided-ipv6]
tools: [terraform, aws-cli]
estimated_time: 50m
bloom_level: design
prerequisites: [01-your-first-vpc, 24-subnet-design-patterns, 29-nat-gateway-patterns]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a VPC with an Egress-Only Internet Gateway (free) and subnets (~$0.00/hr). No NAT Gateways are created for IPv6 egress, saving ~$0.045/hr compared to IPv4 NAT. Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.5 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Exercise 01 completed | Understand VPCs and Internet Gateways |
| Exercise 24 completed | Understand subnet design and AZ placement |
| Exercise 29 completed | Understand NAT Gateway costs |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Create** a dual-stack VPC with both IPv4 and Amazon-provided IPv6 CIDR blocks
2. **Configure** dual-stack subnets that assign both IPv4 and IPv6 addresses
3. **Implement** an Egress-Only Internet Gateway for IPv6 private subnet egress (free, unlike NAT)
4. **Write** security group rules that handle IPv4 and IPv6 as separate rule families
5. **Explain** why `0.0.0.0/0` does NOT cover IPv6 traffic and what `::/0` means

## Why This Matters

IPv6 adoption on AWS is accelerating for three reasons: AWS now charges $0.005/hr per public IPv4 address (effective February 2024), IPv6 addresses are free and plentiful, and modern services like Application Load Balancer and CloudFront natively support dual-stack. More importantly, IPv6 egress through an Egress-Only Internet Gateway is free -- no $0.045/hr NAT Gateway charge, no $0.045/GB data processing fee. For workloads that only need outbound internet access (pulling updates, calling APIs), IPv6 egress can save hundreds of dollars per month.

The critical gotcha is security groups. IPv4 and IPv6 are treated as completely separate rule families. A rule allowing `0.0.0.0/0` on port 443 does NOT allow IPv6 traffic on port 443. You must add a separate rule for `::/0`. Forgetting this means your dual-stack instances silently drop IPv6 traffic even though the subnet has an IPv6 CIDR and a route to the Egress-Only IGW.

AWS provides a /56 IPv6 CIDR block per VPC from Amazon's Global Unicast Address (GUA) range. You subdivide this into /64 subnets (the standard IPv6 subnet size). Unlike IPv4, there is no concept of "private" IPv6 addresses -- all IPv6 addresses are globally routable. The Egress-Only IGW provides the privacy equivalent of NAT: instances can initiate outbound IPv6 connections, but inbound IPv6 connections from the internet are blocked.

## Building Blocks

Create the following files in your exercise directory. Your job is to fill in each `# TODO` block.

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
  default     = "ipv6-dualstack-lab"
}
```

### `locals.tf`

```hcl
data "aws_availability_zones" "available" {
  state = "available"
}

locals {
  vpc_cidr = "10.70.0.0/16"
  azs      = slice(data.aws_availability_zones.available.names, 0, 2)
}
```

### `vpc.tf`

```hcl
# =======================================================
# TODO 1 — Create a dual-stack VPC with IPv4 + IPv6
# =======================================================
# Requirements:
#   - Primary IPv4 CIDR: local.vpc_cidr (10.70.0.0/16)
#   - Request an Amazon-provided IPv6 CIDR block
#     (set assign_generated_ipv6_cidr_block = true)
#   - Enable DNS support and DNS hostnames
#   - Tag with Name = "${var.project_name}-vpc"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc
# Hint: The IPv6 CIDR is auto-assigned by AWS (a /56 block).
#       Access it via aws_vpc.this.ipv6_cidr_block


# =======================================================
# TODO 2 — Create dual-stack public subnets
# =======================================================
# Requirements:
#   - Use for_each over local.azs (create one per AZ)
#   - IPv4 CIDR: cidrsubnet(local.vpc_cidr, 8, i)
#   - IPv6 CIDR: cidrsubnet(aws_vpc.this.ipv6_cidr_block, 8, i)
#     (This carves /64 subnets from the VPC's /56)
#   - Set map_public_ip_on_launch = true (for IPv4)
#   - Set ipv6_native = false (dual-stack, not IPv6-only)
#   - Set assign_ipv6_address_on_creation = true
#   - Tag with Name = "${var.project_name}-public-${each.key}"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/subnet
# Hint: assign_ipv6_address_on_creation gives instances an IPv6 address automatically


# =======================================================
# TODO 3 — Create dual-stack private subnets
# =======================================================
# Requirements:
#   - Use for_each over local.azs (create one per AZ)
#   - IPv4 CIDR: cidrsubnet(local.vpc_cidr, 8, i + 10)
#   - IPv6 CIDR: cidrsubnet(aws_vpc.this.ipv6_cidr_block, 8, i + 10)
#   - Do NOT set map_public_ip_on_launch (private subnet)
#   - Set assign_ipv6_address_on_creation = true
#   - Tag with Name = "${var.project_name}-private-${each.key}"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/subnet
# Hint: Private subnets still get IPv6 addresses but use Egress-Only IGW for outbound
```

### `gateways.tf`

```hcl
# ------------------------------------------------------------------
# Internet Gateway -- for public subnet IPv4 + IPv6 inbound/outbound
# ------------------------------------------------------------------
resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id

  tags = { Name = "${var.project_name}-igw" }
}

# =======================================================
# TODO 4 — Egress-Only Internet Gateway for IPv6
# =======================================================
# Requirements:
#   - Create an aws_egress_only_internet_gateway
#   - Attach it to the VPC
#   - Tag with Name = "${var.project_name}-eigw"
#
# Key insight: Egress-Only IGW is FREE (unlike NAT Gateway at $0.045/hr)
# It allows outbound IPv6 connections but blocks inbound IPv6 from internet.
# This is the IPv6 equivalent of NAT Gateway's outbound-only behavior.
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/egress_only_internet_gateway
# Hint: Only one argument needed: vpc_id
```

> **Best Practice:** The Egress-Only Internet Gateway is free -- it has no hourly charge and no data processing fee. For workloads that only need outbound internet access (pulling container images, calling external APIs), using IPv6 with an Egress-Only IGW instead of IPv4 with a NAT Gateway saves $0.045/hr (~$32/month) per NAT Gateway plus $0.045/GB in data processing. This is one of the most impactful cost optimizations in AWS networking.

### `route-tables.tf`

```hcl
# =======================================================
# TODO 5 — Dual-stack route tables
# =======================================================
# Requirements:
#   PUBLIC route table:
#   - IPv4 default route: 0.0.0.0/0 -> Internet Gateway
#   - IPv6 default route: ::/0 -> Internet Gateway
#   - Associate with all public subnets
#   - Tag: "${var.project_name}-public-rt"
#
#   PRIVATE route table:
#   - NO IPv4 default route (no NAT in this exercise)
#   - IPv6 default route: ::/0 -> Egress-Only Internet Gateway
#   - Associate with all private subnets
#   - Tag: "${var.project_name}-private-rt"
#
# Key insight: Public subnets use the regular IGW for both IPv4 and IPv6.
# Private subnets use Egress-Only IGW for IPv6 outbound only.
# For IPv4 private egress, you would add a NAT Gateway (not in this exercise).
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route_table
# Hint: Use ipv6_cidr_block = "::/0" and
#       egress_only_gateway_id for the Egress-Only IGW route
```

### `security-groups.tf`

```hcl
# =======================================================
# TODO 6 — Dual-stack security groups
# =======================================================
# Requirements:
#   Create a "web" security group with BOTH IPv4 and IPv6 rules:
#
#   Ingress rules (4 separate resources):
#   - HTTP (80) from 0.0.0.0/0 (IPv4)
#   - HTTP (80) from ::/0 (IPv6)
#   - HTTPS (443) from 0.0.0.0/0 (IPv4)
#   - HTTPS (443) from ::/0 (IPv6)
#
#   Egress rules (2 separate resources):
#   - All traffic to 0.0.0.0/0 (IPv4)
#   - All traffic to ::/0 (IPv6)
#
#   Use aws_vpc_security_group_ingress_rule/egress_rule resources.
#   For IPv6 rules, use cidr_ipv6 instead of cidr_ipv4.
#
# CRITICAL: 0.0.0.0/0 does NOT cover IPv6!
# You MUST add separate ::/0 rules for IPv6 traffic.
# Forgetting this is the #1 dual-stack security group mistake.
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_security_group_ingress_rule
# Hint: cidr_ipv6 = "::/0" for the IPv6 rules
```

> **Best Practice:** Security groups treat IPv4 and IPv6 as completely separate rule families. A rule allowing `0.0.0.0/0` does NOT allow `::/0`. Always add parallel rules for both address families. In Terraform, use `cidr_ipv4 = "0.0.0.0/0"` for IPv4 and `cidr_ipv6 = "::/0"` for IPv6. Audit your security groups after enabling dual-stack to ensure no IPv6 rules were forgotten.

### `outputs.tf`

```hcl
# =======================================================
# TODO 7 — Outputs for dual-stack verification
# =======================================================
# Requirements:
#   - Output vpc_id
#   - Output vpc_ipv4_cidr (aws_vpc.this.cidr_block)
#   - Output vpc_ipv6_cidr (aws_vpc.this.ipv6_cidr_block)
#   - Output public_subnet_details: map of AZ -> { ipv4_cidr, ipv6_cidr }
#   - Output private_subnet_details: map of AZ -> { ipv4_cidr, ipv6_cidr }
#   - Output egress_only_igw_id
#   - Output cost_savings message:
#     "Egress-Only IGW: $0/hr. NAT Gateway equivalent: $0.045/hr ($32.40/month). Annual savings per NAT replaced: $388.80"
#
# Hint: Use aws_subnet.public[az].ipv6_cidr_block for IPv6 CIDRs
```

## Spot the Bug

A colleague enabled dual-stack on their VPC but users report that IPv6 HTTPS requests from private instances time out. The instances have IPv6 addresses and the Egress-Only IGW exists. **What did they miss?**

```hcl
resource "aws_security_group" "app" {
  name   = "app-sg"
  vpc_id = aws_vpc.this.id
}

resource "aws_vpc_security_group_egress_rule" "app_out" {
  security_group_id = aws_security_group.app.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}
```

<details>
<summary>Explain the bug</summary>

The security group only has an IPv4 egress rule (`cidr_ipv4 = "0.0.0.0/0"`). There is **no IPv6 egress rule**. In AWS security groups, `0.0.0.0/0` only covers IPv4 traffic. IPv6 traffic requires a separate rule with `cidr_ipv6 = "::/0"`.

Even though the subnet has an IPv6 CIDR, the route table has a `::/0 -> eigw` route, and the instance has an IPv6 address, the security group blocks all outbound IPv6 traffic because there is no egress rule allowing it.

The fix is to add a parallel IPv6 egress rule:

```hcl
resource "aws_vpc_security_group_egress_rule" "app_out_ipv4" {
  security_group_id = aws_security_group.app.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}

resource "aws_vpc_security_group_egress_rule" "app_out_ipv6" {
  security_group_id = aws_security_group.app.id
  cidr_ipv6         = "::/0"
  ip_protocol       = "-1"
}
```

This is the most common dual-stack mistake. The solution is to **always add rules in pairs**: one for IPv4, one for IPv6.

</details>

## Solutions

<details>
<summary>TODO 1 -- Dual-stack VPC</summary>

```hcl
resource "aws_vpc" "this" {
  cidr_block                       = local.vpc_cidr
  assign_generated_ipv6_cidr_block = true
  enable_dns_support               = true
  enable_dns_hostnames             = true

  tags = { Name = "${var.project_name}-vpc" }
}
```

</details>

<details>
<summary>TODO 2 -- Dual-stack public subnets</summary>

```hcl
resource "aws_subnet" "public" {
  for_each = { for i, az in local.azs : az => i }

  vpc_id                          = aws_vpc.this.id
  cidr_block                      = cidrsubnet(local.vpc_cidr, 8, each.value)
  ipv6_cidr_block                 = cidrsubnet(aws_vpc.this.ipv6_cidr_block, 8, each.value)
  availability_zone               = each.key
  map_public_ip_on_launch         = true
  assign_ipv6_address_on_creation = true

  tags = { Name = "${var.project_name}-public-${each.key}" }
}
```

</details>

<details>
<summary>TODO 3 -- Dual-stack private subnets</summary>

```hcl
resource "aws_subnet" "private" {
  for_each = { for i, az in local.azs : az => i }

  vpc_id                          = aws_vpc.this.id
  cidr_block                      = cidrsubnet(local.vpc_cidr, 8, each.value + 10)
  ipv6_cidr_block                 = cidrsubnet(aws_vpc.this.ipv6_cidr_block, 8, each.value + 10)
  availability_zone               = each.key
  assign_ipv6_address_on_creation = true

  tags = { Name = "${var.project_name}-private-${each.key}" }
}
```

</details>

<details>
<summary>TODO 4 -- Egress-Only Internet Gateway</summary>

```hcl
resource "aws_egress_only_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id

  tags = { Name = "${var.project_name}-eigw" }
}
```

</details>

<details>
<summary>TODO 5 -- Dual-stack route tables</summary>

```hcl
# ------------------------------------------------------------------
# Public route table -- IGW for both IPv4 and IPv6
# ------------------------------------------------------------------
resource "aws_route_table" "public" {
  vpc_id = aws_vpc.this.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this.id
  }

  route {
    ipv6_cidr_block = "::/0"
    gateway_id      = aws_internet_gateway.this.id
  }

  tags = { Name = "${var.project_name}-public-rt" }
}

resource "aws_route_table_association" "public" {
  for_each = aws_subnet.public

  subnet_id      = each.value.id
  route_table_id = aws_route_table.public.id
}

# ------------------------------------------------------------------
# Private route table -- Egress-Only IGW for IPv6 only
# No IPv4 default route (would need NAT Gateway, not in this exercise)
# ------------------------------------------------------------------
resource "aws_route_table" "private" {
  vpc_id = aws_vpc.this.id

  route {
    ipv6_cidr_block        = "::/0"
    egress_only_gateway_id = aws_egress_only_internet_gateway.this.id
  }

  tags = { Name = "${var.project_name}-private-rt" }
}

resource "aws_route_table_association" "private" {
  for_each = aws_subnet.private

  subnet_id      = each.value.id
  route_table_id = aws_route_table.private.id
}
```

</details>

<details>
<summary>TODO 6 -- Dual-stack security groups</summary>

```hcl
resource "aws_security_group" "web" {
  name        = "${var.project_name}-web-sg"
  description = "Dual-stack web tier - HTTP/HTTPS for IPv4 and IPv6"
  vpc_id      = aws_vpc.this.id

  tags = { Name = "${var.project_name}-web-sg" }
}

# ------------------------------------------------------------------
# IPv4 ingress rules
# ------------------------------------------------------------------
resource "aws_vpc_security_group_ingress_rule" "web_http_ipv4" {
  security_group_id = aws_security_group.web.id
  cidr_ipv4         = "0.0.0.0/0"
  from_port         = 80
  to_port           = 80
  ip_protocol       = "tcp"
  description       = "HTTP from internet (IPv4)"
}

resource "aws_vpc_security_group_ingress_rule" "web_https_ipv4" {
  security_group_id = aws_security_group.web.id
  cidr_ipv4         = "0.0.0.0/0"
  from_port         = 443
  to_port           = 443
  ip_protocol       = "tcp"
  description       = "HTTPS from internet (IPv4)"
}

# ------------------------------------------------------------------
# IPv6 ingress rules -- MUST be separate from IPv4!
# ------------------------------------------------------------------
resource "aws_vpc_security_group_ingress_rule" "web_http_ipv6" {
  security_group_id = aws_security_group.web.id
  cidr_ipv6         = "::/0"
  from_port         = 80
  to_port           = 80
  ip_protocol       = "tcp"
  description       = "HTTP from internet (IPv6)"
}

resource "aws_vpc_security_group_ingress_rule" "web_https_ipv6" {
  security_group_id = aws_security_group.web.id
  cidr_ipv6         = "::/0"
  from_port         = 443
  to_port           = 443
  ip_protocol       = "tcp"
  description       = "HTTPS from internet (IPv6)"
}

# ------------------------------------------------------------------
# Egress rules -- both IPv4 and IPv6
# ------------------------------------------------------------------
resource "aws_vpc_security_group_egress_rule" "web_all_out_ipv4" {
  security_group_id = aws_security_group.web.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
  description       = "All outbound (IPv4)"
}

resource "aws_vpc_security_group_egress_rule" "web_all_out_ipv6" {
  security_group_id = aws_security_group.web.id
  cidr_ipv6         = "::/0"
  ip_protocol       = "-1"
  description       = "All outbound (IPv6)"
}
```

</details>

<details>
<summary>TODO 7 -- Outputs</summary>

```hcl
output "vpc_id" {
  description = "VPC ID"
  value       = aws_vpc.this.id
}

output "vpc_ipv4_cidr" {
  description = "VPC IPv4 CIDR block"
  value       = aws_vpc.this.cidr_block
}

output "vpc_ipv6_cidr" {
  description = "VPC IPv6 CIDR block (Amazon-provided /56)"
  value       = aws_vpc.this.ipv6_cidr_block
}

output "public_subnet_details" {
  description = "Public subnet CIDRs by AZ"
  value = {
    for k, v in aws_subnet.public : k => {
      ipv4_cidr = v.cidr_block
      ipv6_cidr = v.ipv6_cidr_block
    }
  }
}

output "private_subnet_details" {
  description = "Private subnet CIDRs by AZ"
  value = {
    for k, v in aws_subnet.private : k => {
      ipv4_cidr = v.cidr_block
      ipv6_cidr = v.ipv6_cidr_block
    }
  }
}

output "egress_only_igw_id" {
  description = "Egress-Only Internet Gateway ID"
  value       = aws_egress_only_internet_gateway.this.id
}

output "cost_savings" {
  description = "Cost savings from using Egress-Only IGW instead of NAT"
  value       = "Egress-Only IGW: $0/hr. NAT Gateway equivalent: $0.045/hr ($32.40/month). Annual savings per NAT replaced: $388.80"
}
```

</details>

## Verify What You Learned

### Confirm VPC has both IPv4 and IPv6 CIDRs

```bash
aws ec2 describe-vpcs \
  --filters "Name=tag:Name,Values=ipv6-dualstack-lab-vpc" \
  --query "Vpcs[0].{IPv4:CidrBlock,IPv6:Ipv6CidrBlockAssociationSet[0].Ipv6CidrBlock,State:Ipv6CidrBlockAssociationSet[0].Ipv6CidrBlockState.State}" \
  --output table
```

Expected:

```
---------------------------------------------------------------
|                        DescribeVpcs                         |
+----------------+-------------------------------+------------+
|      IPv4      |             IPv6              |   State    |
+----------------+-------------------------------+------------+
|  10.70.0.0/16  |  2600:1f18:xxxx:xx00::/56     |  associated|
+----------------+-------------------------------+------------+
```

### Verify subnets have dual-stack CIDRs

```bash
aws ec2 describe-subnets \
  --filters "Name=vpc-id,Values=$(terraform output -raw vpc_id)" \
  --query "Subnets[].{AZ:AvailabilityZone,IPv4:CidrBlock,IPv6:Ipv6CidrBlockAssociationSet[0].Ipv6CidrBlock,AutoIPv6:AssignIpv6AddressOnCreation}" \
  --output table
```

Expected:

```
--------------------------------------------------------------------------
|                           DescribeSubnets                              |
+---------------+----------------+-------------------------------+-------+
|      AZ       |      IPv4      |             IPv6              |AutoV6 |
+---------------+----------------+-------------------------------+-------+
|  us-east-1a   |  10.70.0.0/24  |  2600:1f18:xxxx:xx00::/64     | True  |
|  us-east-1b   |  10.70.1.0/24  |  2600:1f18:xxxx:xx01::/64     | True  |
|  us-east-1a   |  10.70.10.0/24 |  2600:1f18:xxxx:xx0a::/64     | True  |
|  us-east-1b   |  10.70.11.0/24 |  2600:1f18:xxxx:xx0b::/64     | True  |
+---------------+----------------+-------------------------------+-------+
```

### Verify Egress-Only IGW exists

```bash
aws ec2 describe-egress-only-internet-gateways \
  --filters "Name=tag:Name,Values=ipv6-dualstack-lab-eigw" \
  --query "EgressOnlyInternetGateways[].{ID:EgressOnlyInternetGatewayId,VPC:Attachments[0].VpcId,State:Attachments[0].State}" \
  --output table
```

Expected:

```
---------------------------------------------------------
|       DescribeEgressOnlyInternetGateways              |
+-------------------+-----------------+-----------------+
|        ID         |       State     |       VPC       |
+-------------------+-----------------+-----------------+
|  eigw-0abc123...  |  attached       |  vpc-0def456... |
+-------------------+-----------------+-----------------+
```

### Verify private route table has Egress-Only IGW route for IPv6

```bash
aws ec2 describe-route-tables \
  --filters "Name=tag:Name,Values=ipv6-dualstack-lab-private-rt" \
  --query "RouteTables[0].Routes[].{Dest4:DestinationCidrBlock,Dest6:DestinationIpv6CidrBlock,Target:EgressOnlyInternetGatewayId||GatewayId||'local'}" \
  --output table
```

Expected:

```
---------------------------------------------------------------
|                    DescribeRouteTables                       |
+-------------------+-------------------+---------------------+
|       Dest4       |       Dest6       |       Target        |
+-------------------+-------------------+---------------------+
|  10.70.0.0/16     |  None             |  local              |
|  None             |  2600:1f18:.../56 |  local              |
|  None             |  ::/0             |  eigw-0abc123...    |
+-------------------+-------------------+---------------------+
```

### Verify security group has both IPv4 and IPv6 rules

```bash
aws ec2 describe-security-group-rules \
  --filters "Name=group-id,Values=$(aws ec2 describe-security-groups --filters 'Name=tag:Name,Values=ipv6-dualstack-lab-web-sg' --query 'SecurityGroups[0].GroupId' --output text)" \
  --query "SecurityGroupRules[?!IsEgress].{Port:FromPort,Protocol:IpProtocol,IPv4:CidrIpv4,IPv6:CidrIpv6}" \
  --output table
```

Expected: 4 ingress rules (HTTP + HTTPS for both IPv4 and IPv6).

### Confirm cost savings output

```bash
terraform output cost_savings
```

Expected: `"Egress-Only IGW: $0/hr. NAT Gateway equivalent: $0.045/hr ($32.40/month). Annual savings per NAT replaced: $388.80"`

### Confirm no drift

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

Destroy all resources:

```bash
terraform destroy -auto-approve
```

Verify nothing remains:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

You built a complete dual-stack VPC with IPv4 and IPv6 support, demonstrating the Egress-Only Internet Gateway as a free replacement for NAT Gateway on IPv6 traffic. This completes the foundational networking exercises. You now have the building blocks -- CIDR planning, subnet tiers, route tables, security groups, multi-VPC isolation, NAT patterns, and IPv6 dual-stack -- to tackle production multi-AZ architectures (Exercise 04), VPC peering (Exercise 05), and Transit Gateway (Exercise 08) with confidence.

## Summary

- **Amazon-provided IPv6** gives each VPC a /56 block subdivided into /64 subnets -- addresses are free and globally routable
- **Egress-Only Internet Gateway** is free ($0/hr, $0/GB) and provides outbound-only IPv6 access -- the IPv6 equivalent of NAT Gateway
- **Security groups treat IPv4 and IPv6 separately**: `0.0.0.0/0` does NOT cover `::/0` -- always add rules in pairs
- **Dual-stack subnets** assign both IPv4 and IPv6 addresses when `assign_ipv6_address_on_creation = true`
- **Cost savings**: replacing a single NAT Gateway with Egress-Only IGW saves ~$389/year; replacing 3 (multi-AZ) saves ~$1,167/year
- IPv6 has **no private address ranges** -- all addresses are globally routable, but Egress-Only IGW blocks inbound initiation

## Reference

| Resource | Purpose |
|----------|---------|
| `aws_vpc` | VPC with `assign_generated_ipv6_cidr_block` |
| `aws_subnet` | Dual-stack subnet with `ipv6_cidr_block` |
| `aws_internet_gateway` | Full bidirectional IPv4/IPv6 internet access |
| `aws_egress_only_internet_gateway` | Outbound-only IPv6 internet access (free) |
| `aws_route_table` | Dual-stack routes with `::/0` entries |
| `aws_vpc_security_group_ingress_rule` | Separate IPv4/IPv6 rules with `cidr_ipv6` |

## Additional Resources

- [IPv6 in Amazon VPC (AWS Docs)](https://docs.aws.amazon.com/vpc/latest/userguide/vpc-ipv6.html) -- official guide to dual-stack VPC configuration
- [Egress-Only Internet Gateway (AWS Docs)](https://docs.aws.amazon.com/vpc/latest/userguide/egress-only-internet-gateway.html) -- how Egress-Only IGW works and when to use it
- [Terraform aws_egress_only_internet_gateway](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/egress_only_internet_gateway) -- Terraform resource reference
- [AWS IPv6 on VPC FAQ](https://aws.amazon.com/vpc/faqs/#IPv6) -- frequently asked questions about IPv6 on AWS
- [Public IPv4 Address Charges (AWS Blog)](https://aws.amazon.com/blogs/aws/new-aws-public-ipv4-address-charge-public-ip-insights/) -- why IPv6 adoption is accelerating

## Apply Your Knowledge

- [Migrate to IPv6 on AWS (AWS Prescriptive Guidance)](https://docs.aws.amazon.com/prescriptive-guidance/latest/ipv6-on-aws/welcome.html) -- step-by-step migration guide for existing workloads
- [Dual-Stack ALB Configuration](https://docs.aws.amazon.com/elasticloadbalancing/latest/application/application-load-balancers.html#ip-address-type) -- how to enable IPv6 on Application Load Balancers
- [AWS re:Invent: IPv6 Best Practices](https://www.youtube.com/watch?v=2t398lBDgrU) -- enterprise IPv6 adoption strategies and lessons learned

---

> *"The internet is the first thing that humanity has built that humanity doesn't understand."*
> — **Eric Schmidt**

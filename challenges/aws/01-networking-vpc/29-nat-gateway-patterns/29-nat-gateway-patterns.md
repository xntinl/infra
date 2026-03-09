# 29. NAT Gateway Patterns

<!--
difficulty: intermediate
concepts: [nat-gateway, eip, single-az-nat, multi-az-nat, cost-modeling, vpc-endpoints]
tools: [terraform, aws-cli]
estimated_time: 55m
bloom_level: analyze
prerequisites: [01-your-first-vpc, 24-subnet-design-patterns, 25-route-table-deep-dive]
aws_cost: ~$0.15/hr
-->

> **AWS Cost Warning:** This exercise creates NAT Gateways (~$0.045/hr each). The multi-AZ pattern creates 3 NAT Gateways (~$0.135/hr total) plus EIPs. Estimated total: ~$0.15/hr. Remember to run `terraform destroy` promptly when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.5 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Exercise 01 completed | Understand VPCs and Internet Gateways |
| Exercise 24 completed | Understand subnet tiers and AZ placement |
| Exercise 25 completed | Understand route tables and route priority |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Implement** a single-NAT pattern for development environments to minimize cost
2. **Implement** a multi-NAT pattern (one per AZ) for production high availability
3. **Analyze** the cost and failure-mode tradeoffs between single-NAT and multi-NAT
4. **Calculate** NAT Gateway data processing costs and identify VPC endpoints as cost optimization

## Why This Matters

Private subnets need outbound internet access for software updates, API calls, and pulling container images. A NAT Gateway provides this by translating private IPs to a public Elastic IP. But NAT Gateways are one of the most expensive networking resources in AWS: $0.045/hr ($32.40/month) per gateway, plus $0.045/GB for data processing.

The architecture decision is straightforward but has significant cost and availability implications. A single NAT Gateway in one AZ costs ~$32/month but creates a single point of failure: if that AZ goes down, all private subnets lose internet access. A NAT Gateway per AZ costs ~$97/month (for 3 AZs) but survives any single AZ failure and eliminates cross-AZ data transfer charges ($0.01/GB).

The hidden cost trap is data processing. Every byte that flows through a NAT Gateway costs $0.045/GB. A workload that pulls 100GB/month from S3 through NAT pays $4.50/month in data processing alone. A VPC Gateway Endpoint for S3 is free and keeps that traffic on the AWS backbone. Production teams routinely save thousands of dollars per month by adding S3 and DynamoDB gateway endpoints.

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
  default     = "nat-patterns-lab"
}

variable "nat_pattern" {
  description = "NAT pattern: 'single' (one NAT, all AZs share) or 'multi' (one NAT per AZ)"
  type        = string
  default     = "multi"

  validation {
    condition     = contains(["single", "multi"], var.nat_pattern)
    error_message = "nat_pattern must be 'single' or 'multi'."
  }
}
```

### `locals.tf`

```hcl
data "aws_availability_zones" "available" {
  state = "available"
}

locals {
  vpc_cidr = "10.60.0.0/16"
  azs      = slice(data.aws_availability_zones.available.names, 0, 3)

  public_subnets = {
    for i, az in local.azs : az => cidrsubnet(local.vpc_cidr, 8, i)
  }

  private_subnets = {
    for i, az in local.azs : az => cidrsubnet(local.vpc_cidr, 8, i + 10)
  }

  # ------------------------------------------------------------------
  # Cost comparison (us-east-1 pricing):
  #
  # Pattern  | NAT GWs | Monthly Cost | AZ Failure Impact
  # ---------|---------|--------------|-------------------
  # single   | 1       | ~$32.40      | All private subnets lose internet
  # multi    | 3       | ~$97.20      | Only affected AZ loses internet
  #
  # Plus data processing: $0.045/GB through NAT
  # Plus cross-AZ transfer: $0.01/GB (single pattern only)
  # ------------------------------------------------------------------

  # For single-NAT, we only create resources in the first AZ
  # For multi-NAT, we create resources in every AZ
  nat_azs = var.nat_pattern == "single" ? { (local.azs[0]) = local.azs[0] } : {
    for az in local.azs : az => az
  }
}
```

### `vpc.tf`

```hcl
resource "aws_vpc" "this" {
  cidr_block           = local.vpc_cidr
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = "${var.project_name}-vpc" }
}

resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id

  tags = { Name = "${var.project_name}-igw" }
}

resource "aws_subnet" "public" {
  for_each = local.public_subnets

  vpc_id                  = aws_vpc.this.id
  cidr_block              = each.value
  availability_zone       = each.key
  map_public_ip_on_launch = true

  tags = { Name = "${var.project_name}-public-${each.key}" }
}

resource "aws_subnet" "private" {
  for_each = local.private_subnets

  vpc_id            = aws_vpc.this.id
  cidr_block        = each.value
  availability_zone = each.key

  tags = { Name = "${var.project_name}-private-${each.key}" }
}

# ------------------------------------------------------------------
# Public route table -- shared by all public subnets
# ------------------------------------------------------------------
resource "aws_route_table" "public" {
  vpc_id = aws_vpc.this.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this.id
  }

  tags = { Name = "${var.project_name}-public-rt" }
}

resource "aws_route_table_association" "public" {
  for_each = aws_subnet.public

  subnet_id      = each.value.id
  route_table_id = aws_route_table.public.id
}
```

### `nat.tf`

```hcl
# =======================================================
# TODO 1 — Create Elastic IPs for NAT Gateways
# =======================================================
# Requirements:
#   - Use for_each over local.nat_azs
#   - Set domain = "vpc"
#   - Tag with Name = "${var.project_name}-nat-eip-${each.key}"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/eip
# Hint: for_each = local.nat_azs (1 EIP for single, 3 for multi)


# =======================================================
# TODO 2 — Create NAT Gateways in public subnets
# =======================================================
# Requirements:
#   - Use for_each over local.nat_azs
#   - Place each NAT Gateway in the PUBLIC subnet of its AZ
#   - Reference the EIP from TODO 1
#   - Add depends_on = [aws_internet_gateway.this]
#   - Tag with Name = "${var.project_name}-nat-${each.key}"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/nat_gateway
# Hint: subnet_id = aws_subnet.public[each.key].id


# =======================================================
# TODO 3 — Create private route tables with NAT routes
# =======================================================
# Requirements:
#   For MULTI-NAT (one RT per AZ):
#   - Create one route table per AZ using for_each over local.azs
#   - Each RT has 0.0.0.0/0 -> its own AZ's NAT Gateway
#   - Associate each private subnet with its own AZ's route table
#
#   For SINGLE-NAT (one shared RT):
#   - Create one route table for all private subnets
#   - The RT has 0.0.0.0/0 -> the single NAT Gateway
#   - Associate all private subnets with this one route table
#
#   Handle both patterns using conditional logic:
#   - Multi: aws_nat_gateway.this[each.key].id
#   - Single: aws_nat_gateway.this[local.azs[0]].id
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route_table
# Hint: Create one RT per AZ regardless of pattern, but point to
#       different NAT GWs. For single: all RTs point to the same NAT.
```

> **Best Practice:** Production workloads need one NAT Gateway per AZ. A single-NAT pattern saves ~$65/month but creates a single point of failure and incurs cross-AZ data transfer charges ($0.01/GB). For development environments where cost matters more than availability, single-NAT is acceptable. Always add VPC Gateway Endpoints for S3 and DynamoDB -- they are free and eliminate NAT data processing charges for those services.

### `endpoints.tf`

```hcl
# =======================================================
# TODO 4 — S3 and DynamoDB Gateway VPC Endpoints
# =======================================================
# Requirements:
#   - Create an S3 Gateway VPC Endpoint
#     Service: "com.amazonaws.${var.region}.s3"
#   - Create a DynamoDB Gateway VPC Endpoint
#     Service: "com.amazonaws.${var.region}.dynamodb"
#   - Associate both with ALL route tables (public + private)
#   - Tag both endpoints with Name
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_endpoint
# Hint: route_table_ids = concat(
#         [aws_route_table.public.id],
#         [for rt in aws_route_table.private : rt.id]
#       )
```

### `outputs.tf`

```hcl
# =======================================================
# TODO 5 — Outputs for verification and cost analysis
# =======================================================
# Requirements:
#   - Output vpc_id
#   - Output nat_gateway_ids (map of AZ -> NAT GW ID)
#   - Output nat_eip_public_ips (map of AZ -> public IP)
#   - Output nat_pattern (single or multi)
#   - Output estimated_monthly_cost with a calculation string:
#     "${length(local.nat_azs)} NAT GW(s) x $32.40 = $${length(local.nat_azs) * 32.40}/month + $0.045/GB data processing"
#   - Output endpoint_ids for S3 and DynamoDB endpoints
#
# Hint: { for k, v in aws_nat_gateway.this : k => v.id }
```

## Spot the Bug

A colleague created per-AZ route tables but pointed all of them to the same NAT Gateway. **Why is this a problem?**

```hcl
resource "aws_route_table" "private" {
  for_each = toset(local.azs)
  vpc_id   = aws_vpc.this.id

  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.this[local.azs[0]].id   # <-- BUG
  }

  tags = { Name = "${var.project_name}-private-rt-${each.key}" }
}
```

<details>
<summary>Explain the bug</summary>

All three private route tables point to `aws_nat_gateway.this[local.azs[0]]` -- the NAT Gateway in the first AZ only. This defeats the purpose of having multiple NAT Gateways:

1. **Single point of failure:** If AZ-a goes down, all three private subnets lose internet access even though AZ-b and AZ-c have healthy NAT Gateways.

2. **Cross-AZ data transfer costs:** Traffic from private subnets in AZ-b and AZ-c must cross AZ boundaries to reach the NAT Gateway in AZ-a. This costs $0.01/GB in each direction ($0.02/GB round-trip), which adds up quickly for high-traffic workloads.

3. **Wasted NAT Gateways:** The NAT Gateways in AZ-b and AZ-c are running (and costing $0.045/hr each) but receiving zero traffic.

The fix is to point each route table to its own AZ's NAT Gateway:

```hcl
resource "aws_route_table" "private" {
  for_each = toset(local.azs)
  vpc_id   = aws_vpc.this.id

  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.this[each.key].id   # Fixed!
  }

  tags = { Name = "${var.project_name}-private-rt-${each.key}" }
}
```

</details>

## Solutions

<details>
<summary>TODO 1 -- Elastic IPs for NAT Gateways</summary>

```hcl
resource "aws_eip" "nat" {
  for_each = local.nat_azs

  domain = "vpc"

  tags = { Name = "${var.project_name}-nat-eip-${each.key}" }
}
```

</details>

<details>
<summary>TODO 2 -- NAT Gateways in public subnets</summary>

```hcl
resource "aws_nat_gateway" "this" {
  for_each = local.nat_azs

  allocation_id = aws_eip.nat[each.key].id
  subnet_id     = aws_subnet.public[each.key].id

  depends_on = [aws_internet_gateway.this]

  tags = { Name = "${var.project_name}-nat-${each.key}" }
}
```

</details>

<details>
<summary>TODO 3 -- Private route tables with NAT routes</summary>

```hcl
# ------------------------------------------------------------------
# One route table per AZ. For multi-NAT, each points to its own
# NAT Gateway. For single-NAT, all point to the same one.
# ------------------------------------------------------------------
resource "aws_route_table" "private" {
  for_each = toset(local.azs)

  vpc_id = aws_vpc.this.id

  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = var.nat_pattern == "multi" ? aws_nat_gateway.this[each.key].id : aws_nat_gateway.this[local.azs[0]].id
  }

  tags = { Name = "${var.project_name}-private-rt-${each.key}" }
}

resource "aws_route_table_association" "private" {
  for_each = local.private_subnets

  subnet_id      = aws_subnet.private[each.key].id
  route_table_id = aws_route_table.private[each.key].id
}
```

</details>

<details>
<summary>TODO 4 -- S3 and DynamoDB Gateway VPC Endpoints</summary>

```hcl
resource "aws_vpc_endpoint" "s3" {
  vpc_id       = aws_vpc.this.id
  service_name = "com.amazonaws.${var.region}.s3"

  vpc_endpoint_type = "Gateway"

  route_table_ids = concat(
    [aws_route_table.public.id],
    [for rt in aws_route_table.private : rt.id]
  )

  tags = { Name = "${var.project_name}-s3-endpoint" }
}

resource "aws_vpc_endpoint" "dynamodb" {
  vpc_id       = aws_vpc.this.id
  service_name = "com.amazonaws.${var.region}.dynamodb"

  vpc_endpoint_type = "Gateway"

  route_table_ids = concat(
    [aws_route_table.public.id],
    [for rt in aws_route_table.private : rt.id]
  )

  tags = { Name = "${var.project_name}-dynamodb-endpoint" }
}
```

</details>

<details>
<summary>TODO 5 -- Outputs for verification and cost analysis</summary>

```hcl
output "vpc_id" {
  description = "VPC ID"
  value       = aws_vpc.this.id
}

output "nat_gateway_ids" {
  description = "NAT Gateway IDs by AZ"
  value       = { for k, v in aws_nat_gateway.this : k => v.id }
}

output "nat_eip_public_ips" {
  description = "NAT Gateway public IPs by AZ"
  value       = { for k, v in aws_eip.nat : k => v.public_ip }
}

output "nat_pattern" {
  description = "NAT pattern used (single or multi)"
  value       = var.nat_pattern
}

output "estimated_monthly_cost" {
  description = "Estimated NAT Gateway monthly cost"
  value       = "${length(local.nat_azs)} NAT GW(s) x $32.40 = $${length(local.nat_azs) * 32.40}/month + $0.045/GB data processing"
}

output "endpoint_ids" {
  description = "VPC Endpoint IDs"
  value = {
    s3       = aws_vpc_endpoint.s3.id
    dynamodb = aws_vpc_endpoint.dynamodb.id
  }
}
```

</details>

## Verify What You Learned

### Confirm NAT Gateways are created in the correct AZs

```bash
aws ec2 describe-nat-gateways \
  --filter "Name=vpc-id,Values=$(terraform output -raw vpc_id)" "Name=state,Values=available" \
  --query "NatGateways[].{AZ:SubnetId,State:State,PublicIP:NatGatewayAddresses[0].PublicIp}" \
  --output table
```

Expected (multi-NAT pattern):

```
--------------------------------------------------------------
|                   DescribeNatGateways                      |
+---------------------+-----------+--------------------------+
|         AZ          |   State   |        PublicIP           |
+---------------------+-----------+--------------------------+
|  subnet-0a...       |  available|  54.210.x.x              |
|  subnet-0b...       |  available|  52.87.x.x               |
|  subnet-0c...       |  available|  3.95.x.x                |
+---------------------+-----------+--------------------------+
```

### Verify private route tables point to NAT Gateways

```bash
aws ec2 describe-route-tables \
  --filters "Name=tag:Name,Values=nat-patterns-lab-private-rt-*" \
  --query "RouteTables[].{Name:Tags[?Key=='Name'].Value|[0],NATRoute:Routes[?DestinationCidrBlock=='0.0.0.0/0'].NatGatewayId|[0]}" \
  --output table
```

Expected (multi-NAT): each route table points to a different NAT Gateway ID.

Expected (single-NAT): all route tables point to the same NAT Gateway ID.

### Verify VPC Endpoints are active

```bash
aws ec2 describe-vpc-endpoints \
  --filters "Name=vpc-id,Values=$(terraform output -raw vpc_id)" \
  --query "VpcEndpoints[].{Service:ServiceName,Type:VpcEndpointType,State:State}" \
  --output table
```

Expected:

```
--------------------------------------------------------------
|                   DescribeVpcEndpoints                     |
+-----------------------------------+----------+-------------+
|             Service               |  State   |    Type     |
+-----------------------------------+----------+-------------+
|  com.amazonaws.us-east-1.s3       |  available|  Gateway   |
|  com.amazonaws.us-east-1.dynamodb |  available|  Gateway   |
+-----------------------------------+----------+-------------+
```

### Check the estimated cost output

```bash
terraform output estimated_monthly_cost
```

Expected (multi): `"3 NAT GW(s) x $32.40 = $97.2/month + $0.045/GB data processing"`

Expected (single): `"1 NAT GW(s) x $32.40 = $32.4/month + $0.045/GB data processing"`

### Switch between patterns and compare

```bash
terraform apply -var="nat_pattern=single" -auto-approve
terraform output nat_gateway_ids
```

Expected: only one NAT Gateway ID.

```bash
terraform apply -var="nat_pattern=multi" -auto-approve
terraform output nat_gateway_ids
```

Expected: three NAT Gateway IDs (one per AZ).

### Confirm no drift

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

Destroy all resources. NAT Gateways cost ~$0.045/hr each -- destroy promptly:

```bash
terraform destroy -auto-approve
```

Verify nothing remains:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

You now understand single-NAT and multi-NAT patterns, their cost and availability tradeoffs, and how VPC endpoints eliminate NAT data processing charges for S3 and DynamoDB. In the next exercise, **30 -- IPv6 Dual-Stack VPC**, you will add IPv6 support to a VPC, use the free Egress-Only Internet Gateway (replacing NAT for IPv6 egress), and configure dual-stack subnets and security groups.

## Summary

- **Single-NAT** (~$32/month) is acceptable for development; **multi-NAT** (~$97/month for 3 AZs) is required for production
- A single-AZ NAT Gateway failure takes down internet access for **all private subnets** that route through it
- **NAT data processing** costs $0.045/GB -- high-traffic workloads can spend thousands per month
- **VPC Gateway Endpoints** for S3 and DynamoDB are free and bypass NAT entirely -- always add them
- **Cross-AZ data transfer** ($0.01/GB) adds hidden costs to single-NAT patterns
- **NAT Gateways must be placed in public subnets** with an IGW route -- they need internet access themselves
- Always add **`depends_on = [aws_internet_gateway.this]`** to NAT Gateway resources

## Reference

| Resource | Purpose |
|----------|---------|
| `aws_nat_gateway` | Provides outbound internet for private subnets |
| `aws_eip` | Static public IP for NAT Gateway |
| `aws_route_table` | Routes private subnet traffic to NAT |
| `aws_vpc_endpoint` | Gateway endpoint for S3/DynamoDB (free) |
| `aws_internet_gateway` | Required for NAT Gateway to function |

## Additional Resources

- [NAT Gateway (AWS Docs)](https://docs.aws.amazon.com/vpc/latest/userguide/vpc-nat-gateway.html) -- official guide to NAT Gateway creation and behavior
- [NAT Gateway Pricing](https://aws.amazon.com/vpc/pricing/) -- per-hour and per-GB pricing details
- [Gateway VPC Endpoints (AWS Docs)](https://docs.aws.amazon.com/vpc/latest/privatelink/vpc-endpoints-s3.html) -- free endpoints for S3 and DynamoDB
- [Terraform aws_nat_gateway](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/nat_gateway) -- Terraform resource reference
- [AWS Cost Optimization: NAT Gateway](https://aws.amazon.com/blogs/networking-and-content-delivery/identify-and-optimize-public-ipv4-address-usage-on-aws/) -- strategies for reducing NAT costs

## Apply Your Knowledge

- [AWS Pricing Calculator](https://calculator.aws/) -- model NAT Gateway costs for your specific workload
- [VPC Endpoint Policies (AWS Docs)](https://docs.aws.amazon.com/vpc/latest/privatelink/vpc-endpoints-access.html) -- restrict endpoint access to specific S3 buckets
- [AWS re:Invent: Cost Optimization for Networking](https://www.youtube.com/watch?v=UNiT36l8rmI) -- deep-dive on NAT, endpoints, and data transfer costs

---

> *"The business of America is business."*
> — **Calvin Coolidge**

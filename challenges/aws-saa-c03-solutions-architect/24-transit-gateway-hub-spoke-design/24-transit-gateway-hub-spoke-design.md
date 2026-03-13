# 24. Transit Gateway Hub-and-Spoke Design

<!--
difficulty: intermediate
concepts: [transit-gateway, hub-spoke, tgw-route-table, tgw-attachment, network-segmentation, tgw-peering, vpc-connectivity-at-scale]
tools: [terraform, aws-cli]
estimated_time: 60m
bloom_level: apply, evaluate
prerequisites: [17-vpc-subnets-route-tables-igw, 21-vpc-peering-dns-resolution]
aws_cost: ~$0.10/hr
-->

> **AWS Cost Warning:** Transit Gateway costs $0.05/hr plus $0.02/GB data processed. Each VPC attachment adds ~$0.05/hr. With 3 VPC attachments, total ~$0.10/hr. Remember to run `terraform destroy` when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| Terraform >= 1.7 installed | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Understanding of VPC peering (exercise 21) | Non-transitive peering limitations |
| Understanding of route tables | Route table associations and propagation |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Design** a hub-and-spoke network topology using Transit Gateway to connect multiple VPCs.
2. **Construct** a Transit Gateway with VPC attachments and configure route table segmentation using Terraform.
3. **Compare** Transit Gateway (O(n) connections) versus VPC peering (O(n^2) connections) for multi-VPC architectures.
4. **Implement** route table segmentation to isolate workloads while allowing shared services access.
5. **Evaluate** the cost trade-off between Transit Gateway ($0.05/hr + $0.02/GB) and VPC peering (free + $0.01/GB cross-AZ).
6. **Analyze** the impact of default route table association on network segmentation.

---

## Why This Matters

Transit Gateway is the networking hub for multi-VPC architectures. Instead of creating N*(N-1)/2 peering connections for N VPCs, you create N attachments to a single Transit Gateway. For 10 VPCs, that is 10 attachments versus 45 peering connections -- a dramatic reduction in management overhead.

The SAA-C03 exam tests three aspects of Transit Gateway. First, when to use it: scenarios with 3+ VPCs needing any-to-any connectivity, hybrid connectivity (VPN/Direct Connect), or centralized network inspection. Second, route table segmentation: by default, all attachments share a single route table (full mesh). To isolate workloads (production vs development), you create separate TGW route tables and associate attachments selectively. Third, the difference between association (which route table is used for routing decisions) and propagation (which attachments advertise their routes to which route tables).

The default behavior is critical to understand: when you create a TGW with `default_route_table_association = "enable"`, all VPC attachments are automatically associated with the default route table and propagate their routes to it. This creates a full mesh -- every VPC can reach every other VPC. For network segmentation, you must disable the default route table and create custom route tables.

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
```

### `vpc.tf`

```hcl
data "aws_availability_zones" "available" {
  state = "available"
}

locals {
  vpcs = {
    shared = { cidr = "10.0.0.0/16", name = "shared-services" }
    prod   = { cidr = "10.1.0.0/16", name = "production" }
    dev    = { cidr = "10.2.0.0/16", name = "development" }
  }
}

# ==================================================================
# Three VPCs: shared-services (hub), production, development
# ==================================================================
resource "aws_vpc" "this" {
  for_each = local.vpcs

  cidr_block           = each.value.cidr
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags                 = { Name = "tgw-demo-${each.value.name}" }
}

resource "aws_subnet" "a" {
  for_each = local.vpcs

  vpc_id            = aws_vpc.this[each.key].id
  cidr_block        = cidrsubnet(each.value.cidr, 8, 1)
  availability_zone = data.aws_availability_zones.available.names[0]
  tags              = { Name = "tgw-demo-${each.value.name}-a" }
}

resource "aws_subnet" "b" {
  for_each = local.vpcs

  vpc_id            = aws_vpc.this[each.key].id
  cidr_block        = cidrsubnet(each.value.cidr, 8, 2)
  availability_zone = data.aws_availability_zones.available.names[1]
  tags              = { Name = "tgw-demo-${each.value.name}-b" }
}

# Route tables for each VPC
resource "aws_route_table" "this" {
  for_each = local.vpcs

  vpc_id = aws_vpc.this[each.key].id
  tags   = { Name = "tgw-demo-${each.value.name}-rt" }
}

resource "aws_route_table_association" "a" {
  for_each = local.vpcs

  subnet_id      = aws_subnet.a[each.key].id
  route_table_id = aws_route_table.this[each.key].id
}

resource "aws_route_table_association" "b" {
  for_each = local.vpcs

  subnet_id      = aws_subnet.b[each.key].id
  route_table_id = aws_route_table.this[each.key].id
}
```

### `tgw.tf`

```hcl
# ============================================================
# TODO 1: Create Transit Gateway  [tgw.tf]
# ============================================================
# Create a Transit Gateway with default route table association
# DISABLED so you can implement custom segmentation.
#
# Requirements:
#   - Resource: aws_ec2_transit_gateway
#   - description = "Hub for multi-VPC connectivity"
#   - default_route_table_association = "disable"
#   - default_route_table_propagation = "disable"
#   - dns_support = "enable"
#   - vpn_ecmp_support = "enable"
#
# CRITICAL: Setting default_route_table_association = "disable"
# prevents the full-mesh default behavior. Without this, all
# VPCs can reach all other VPCs -- no segmentation.
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ec2_transit_gateway
# ============================================================


# ============================================================
# TODO 2: Create VPC Attachments  [tgw.tf]
# ============================================================
# Attach all three VPCs to the Transit Gateway.
#
# Requirements:
#   - Resource: aws_ec2_transit_gateway_vpc_attachment (3 instances)
#   - For each VPC:
#     - transit_gateway_id = TGW ID
#     - vpc_id = VPC ID
#     - subnet_ids = [subnet_a, subnet_b] (one per AZ)
#     - transit_gateway_default_route_table_association = false
#     - transit_gateway_default_route_table_propagation = false
#
# Each attachment costs ~$0.05/hr. Subnets must be in different
# AZs for high availability.
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ec2_transit_gateway_vpc_attachment
# ============================================================


# ============================================================
# TODO 3: Create TGW Route Tables for Segmentation  [tgw.tf]
# ============================================================
# Create two TGW route tables:
# 1. "shared" -- used by shared-services VPC; can route to all VPCs
# 2. "workload" -- used by prod and dev VPCs; can route to shared
#    services but NOT to each other
#
# Requirements:
#   - Resource: aws_ec2_transit_gateway_route_table (2 instances)
#   - Resource: aws_ec2_transit_gateway_route_table_association
#     - shared VPC attachment -> shared route table
#     - prod VPC attachment -> workload route table
#     - dev VPC attachment -> workload route table
#   - Resource: aws_ec2_transit_gateway_route_table_propagation
#     - Propagate all VPCs to the shared route table (shared can reach all)
#     - Propagate ONLY shared VPC to the workload route table
#       (prod/dev can reach shared but NOT each other)
#
# This creates the segmentation:
#   shared <-> prod (via shared route table)
#   shared <-> dev (via shared route table)
#   prod -/-> dev (workload route table has no route to dev)
#
# Docs: https://docs.aws.amazon.com/vpc/latest/tgw/tgw-route-tables.html
# ============================================================


# ============================================================
# TODO 4: Add VPC Routes to Transit Gateway  [tgw.tf]
# ============================================================
# Add routes in each VPC's route table pointing to the TGW
# for cross-VPC traffic.
#
# Requirements:
#   - Resource: aws_route (3 instances, one per VPC)
#   - For shared VPC: route 10.0.0.0/8 -> TGW (reaches all VPCs)
#   - For prod VPC: route 10.0.0.0/16 -> TGW (reaches shared only)
#   - For dev VPC: route 10.0.0.0/16 -> TGW (reaches shared only)
#
# Using 10.0.0.0/8 as a supernet for shared keeps routing
# simple as you add more VPCs. Prod and dev only need routes
# to the shared CIDR since TGW segmentation prevents cross-access.
# ============================================================
```

### `outputs.tf`

```hcl
output "vpc_ids" {
  value = { for k, v in aws_vpc.this : k => v.id }
}
```

---

## Spot the Bug

A network engineer enables Transit Gateway but complains that production and development VPCs can communicate, violating the segmentation policy:

```hcl
resource "aws_ec2_transit_gateway" "this" {
  description                     = "Hub TGW"
  default_route_table_association = "enable"   # <-- Bug
  default_route_table_propagation = "enable"   # <-- Bug
}

resource "aws_ec2_transit_gateway_vpc_attachment" "prod" {
  transit_gateway_id = aws_ec2_transit_gateway.this.id
  vpc_id             = aws_vpc.prod.id
  subnet_ids         = [aws_subnet.prod_a.id, aws_subnet.prod_b.id]
}

resource "aws_ec2_transit_gateway_vpc_attachment" "dev" {
  transit_gateway_id = aws_ec2_transit_gateway.this.id
  vpc_id             = aws_vpc.dev.id
  subnet_ids         = [aws_subnet.dev_a.id, aws_subnet.dev_b.id]
}
```

<details>
<summary>Explain the bug</summary>

With `default_route_table_association = "enable"` and `default_route_table_propagation = "enable"`, **every VPC attachment is automatically associated with the default route table and propagates its routes to it.** This creates a full mesh -- prod can reach dev, dev can reach prod, and both can reach shared services. There is no segmentation.

This is the most common Transit Gateway misconfiguration. The default behavior is designed for simplicity (all VPCs can talk to all VPCs), but most production environments need segmentation.

**The fix:** Disable default association and propagation, then create custom route tables:

```hcl
resource "aws_ec2_transit_gateway" "this" {
  description                     = "Hub TGW"
  default_route_table_association = "disable"
  default_route_table_propagation = "disable"
}
```

Then create separate route tables with selective propagation:
- **Shared route table:** all VPCs propagate routes (shared can reach everything)
- **Workload route table:** only shared VPC propagates (prod/dev can reach shared but not each other)

This is the hub-and-spoke model: shared services is the hub, prod and dev are spokes. Spokes can reach the hub but not each other.

</details>

---

## Solutions

<details>
<summary>tgw.tf -- TODO 1: Transit Gateway</summary>

```hcl
resource "aws_ec2_transit_gateway" "this" {
  description                     = "Hub for multi-VPC connectivity"
  default_route_table_association = "disable"
  default_route_table_propagation = "disable"
  dns_support                     = "enable"
  vpn_ecmp_support                = "enable"

  tags = { Name = "tgw-demo" }
}
```

</details>

<details>
<summary>tgw.tf -- TODO 2: VPC Attachments</summary>

```hcl
resource "aws_ec2_transit_gateway_vpc_attachment" "this" {
  for_each = local.vpcs

  transit_gateway_id = aws_ec2_transit_gateway.this.id
  vpc_id             = aws_vpc.this[each.key].id
  subnet_ids         = [aws_subnet.a[each.key].id, aws_subnet.b[each.key].id]

  transit_gateway_default_route_table_association = false
  transit_gateway_default_route_table_propagation = false

  tags = { Name = "tgw-demo-${each.value.name}" }
}
```

</details>

<details>
<summary>tgw.tf -- TODO 3: Route Table Segmentation</summary>

```hcl
# Shared route table: shared services VPC uses this
resource "aws_ec2_transit_gateway_route_table" "shared" {
  transit_gateway_id = aws_ec2_transit_gateway.this.id
  tags               = { Name = "tgw-demo-shared-rt" }
}

# Workload route table: prod and dev VPCs use this
resource "aws_ec2_transit_gateway_route_table" "workload" {
  transit_gateway_id = aws_ec2_transit_gateway.this.id
  tags               = { Name = "tgw-demo-workload-rt" }
}

# Associations: which route table does each attachment use?
resource "aws_ec2_transit_gateway_route_table_association" "shared" {
  transit_gateway_attachment_id  = aws_ec2_transit_gateway_vpc_attachment.this["shared"].id
  transit_gateway_route_table_id = aws_ec2_transit_gateway_route_table.shared.id
}

resource "aws_ec2_transit_gateway_route_table_association" "prod" {
  transit_gateway_attachment_id  = aws_ec2_transit_gateway_vpc_attachment.this["prod"].id
  transit_gateway_route_table_id = aws_ec2_transit_gateway_route_table.workload.id
}

resource "aws_ec2_transit_gateway_route_table_association" "dev" {
  transit_gateway_attachment_id  = aws_ec2_transit_gateway_vpc_attachment.this["dev"].id
  transit_gateway_route_table_id = aws_ec2_transit_gateway_route_table.workload.id
}

# Propagations: which routes appear in which route table?
# Shared route table: all VPCs propagate (shared can reach everything)
resource "aws_ec2_transit_gateway_route_table_propagation" "shared_from_all" {
  for_each = local.vpcs

  transit_gateway_attachment_id  = aws_ec2_transit_gateway_vpc_attachment.this[each.key].id
  transit_gateway_route_table_id = aws_ec2_transit_gateway_route_table.shared.id
}

# Workload route table: ONLY shared VPC propagates
# (prod/dev can reach shared, but NOT each other)
resource "aws_ec2_transit_gateway_route_table_propagation" "workload_from_shared" {
  transit_gateway_attachment_id  = aws_ec2_transit_gateway_vpc_attachment.this["shared"].id
  transit_gateway_route_table_id = aws_ec2_transit_gateway_route_table.workload.id
}
```

</details>

<details>
<summary>tgw.tf -- TODO 4: VPC Routes to TGW</summary>

```hcl
# Shared VPC: route to all other VPCs via TGW
resource "aws_route" "shared_to_tgw" {
  route_table_id         = aws_route_table.this["shared"].id
  destination_cidr_block = "10.0.0.0/8"
  transit_gateway_id     = aws_ec2_transit_gateway.this.id
}

# Prod VPC: route to shared services via TGW
resource "aws_route" "prod_to_tgw" {
  route_table_id         = aws_route_table.this["prod"].id
  destination_cidr_block = "10.0.0.0/16"
  transit_gateway_id     = aws_ec2_transit_gateway.this.id
}

# Dev VPC: route to shared services via TGW
resource "aws_route" "dev_to_tgw" {
  route_table_id         = aws_route_table.this["dev"].id
  destination_cidr_block = "10.0.0.0/16"
  transit_gateway_id     = aws_ec2_transit_gateway.this.id
}
```

</details>

---

## Scaling Comparison: TGW vs VPC Peering

| VPCs | Peering Connections | Peering Routes | TGW Attachments | TGW Cost/hr |
|------|-------------------|---------------|-----------------|-------------|
| 3 | 3 | 6 | 3 | $0.20 |
| 5 | 10 | 20 | 5 | $0.30 |
| 10 | 45 | 90 | 10 | $0.55 |
| 20 | 190 | 380 | 20 | $1.05 |

---

## Verify What You Learned

```bash
# Verify TGW exists
aws ec2 describe-transit-gateways \
  --filters "Name=tag:Name,Values=tgw-demo" \
  --query "TransitGateways[0].{Id:TransitGatewayId,State:State}" \
  --output table
```

Expected: State=available.

```bash
# Verify 3 VPC attachments
aws ec2 describe-transit-gateway-attachments \
  --filters "Name=transit-gateway-id,Values=$(aws ec2 describe-transit-gateways --filters 'Name=tag:Name,Values=tgw-demo' --query 'TransitGateways[0].TransitGatewayId' --output text)" \
  --query "TransitGatewayAttachments[*].{Name:Tags[?Key=='Name'].Value|[0],State:State}" \
  --output table
```

Expected: 3 attachments, all in state "available".

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Transit Gateway deletion may take several minutes as attachments are removed first.

Verify:

```bash
terraform state list
```

Expected: no output (empty state).

---

## What's Next

Transit Gateway provides hub-and-spoke connectivity within a region. In the next exercise, you will study **AWS Direct Connect** -- the dedicated physical connection between your data center and AWS -- including virtual interface types, LAG groups, and the decision framework for choosing between Direct Connect and VPN.

---

## Summary

- **Transit Gateway** is a regional network hub that connects VPCs, VPNs, and Direct Connect with O(n) complexity
- VPC peering requires **O(n^2)** connections for full mesh; TGW requires only **O(n)** attachments
- **default_route_table_association = "disable"** is essential for network segmentation; the default creates a full mesh
- **Route table association** determines which route table an attachment uses; **propagation** determines which routes are advertised
- Hub-and-spoke: shared services is the hub (sees all routes), workloads are spokes (see only shared routes)
- TGW costs **$0.05/hr** plus **$0.02/GB** data processed; VPC peering is free but does not scale
- Each VPC attachment requires subnets in **multiple AZs** for high availability
- TGW supports **cross-region peering** for multi-region hub-and-spoke architectures

## Reference

- [Transit Gateway](https://docs.aws.amazon.com/vpc/latest/tgw/what-is-transit-gateway.html)
- [TGW Route Tables](https://docs.aws.amazon.com/vpc/latest/tgw/tgw-route-tables.html)
- [TGW Design Best Practices](https://docs.aws.amazon.com/vpc/latest/tgw/transit-gateway-design-best-practices.html)
- [Terraform aws_ec2_transit_gateway](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ec2_transit_gateway)

## Additional Resources

- [Transit Gateway Network Manager](https://docs.aws.amazon.com/network-manager/latest/tgwnm/what-are-global-networks.html) -- centralized visibility into your global network topology
- [TGW Inter-Region Peering](https://docs.aws.amazon.com/vpc/latest/tgw/tgw-peering.html) -- connecting Transit Gateways across regions
- [TGW Flow Logs](https://docs.aws.amazon.com/vpc/latest/tgw/tgw-flow-logs.html) -- capturing traffic metadata at the TGW level
- [Multi-Account TGW with RAM](https://docs.aws.amazon.com/ram/latest/userguide/shareable.html#shareable-tgw) -- sharing Transit Gateway across AWS accounts using Resource Access Manager

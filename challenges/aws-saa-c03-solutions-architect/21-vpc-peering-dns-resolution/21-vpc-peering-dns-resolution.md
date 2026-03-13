# 21. VPC Peering with DNS Resolution

<!--
difficulty: intermediate
concepts: [vpc-peering, bidirectional-routing, non-transitive-peering, route53-private-hosted-zone, dns-resolution, cross-vpc-communication, cidr-non-overlap]
tools: [terraform, aws-cli]
estimated_time: 50m
bloom_level: apply, analyze
prerequisites: [17-vpc-subnets-route-tables-igw]
aws_cost: ~$0.02/hr
-->

> **AWS Cost Warning:** VPC peering connections are free. You pay only for cross-AZ data transfer ($0.01/GB). Route 53 private hosted zones cost $0.50/month each. Total ~$0.02/hr. Remember to run `terraform destroy` when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| Terraform >= 1.7 installed | `terraform version` |
| AWS CLI configured with a sandbox account | `aws sts get-caller-identity` |
| Completed exercise 17 (VPC, subnets, route tables) | Understanding of route table associations |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Construct** a VPC peering connection between two VPCs with non-overlapping CIDR blocks using Terraform.
2. **Configure** bidirectional route table entries so that instances in both VPCs can communicate.
3. **Explain** why VPC peering is non-transitive and what this means for three-VPC architectures.
4. **Integrate** Route 53 private hosted zones with peered VPCs for cross-VPC DNS resolution.
5. **Analyze** routing failures caused by missing routes in one direction of the peering connection.
6. **Evaluate** when VPC peering is appropriate versus Transit Gateway for multi-VPC connectivity.

---

## Why This Matters

VPC peering connects two VPCs so they can communicate using private IP addresses as if they were on the same network. The traffic stays on the AWS backbone -- it does not traverse the internet. Peering is free (no hourly charge), making it the cheapest option for connecting VPCs. However, it has two critical limitations the SAA-C03 tests heavily.

First, peering is non-transitive. If VPC-A peers with VPC-B, and VPC-B peers with VPC-C, VPC-A cannot communicate with VPC-C through VPC-B. Each pair that needs to communicate requires its own peering connection. For N VPCs, you need N*(N-1)/2 connections -- a full mesh. With 5 VPCs, that is 10 peering connections with 20 route table entries to manage. This O(n^2) scaling is why Transit Gateway exists for larger architectures.

Second, CIDRs must not overlap. If VPC-A uses 10.0.0.0/16 and VPC-B also uses 10.0.0.0/16, peering fails because the router cannot distinguish local traffic from peered traffic. This is a common real-world problem when teams create VPCs independently with default CIDRs.

DNS resolution across peered VPCs requires enabling DNS hostnames and DNS support on both VPCs, plus enabling DNS resolution on the peering connection itself. Without this, private DNS names resolve to public IPs instead of private IPs, and traffic routes through the internet gateway instead of the peering connection.

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

# ==================================================================
# VPC A: 10.0.0.0/16 (Production)
# ==================================================================
resource "aws_vpc" "a" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags                 = { Name = "peering-demo-vpc-a" }
}

resource "aws_subnet" "a" {
  vpc_id            = aws_vpc.a.id
  cidr_block        = "10.0.1.0/24"
  availability_zone = data.aws_availability_zones.available.names[0]
  tags              = { Name = "peering-demo-a-subnet" }
}

resource "aws_route_table" "a" {
  vpc_id = aws_vpc.a.id
  tags   = { Name = "peering-demo-a-rt" }
}

resource "aws_route_table_association" "a" {
  subnet_id      = aws_subnet.a.id
  route_table_id = aws_route_table.a.id
}

# ==================================================================
# VPC B: 10.1.0.0/16 (Shared Services)
# Note: CIDR must NOT overlap with VPC A
# ==================================================================
resource "aws_vpc" "b" {
  cidr_block           = "10.1.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags                 = { Name = "peering-demo-vpc-b" }
}

resource "aws_subnet" "b" {
  vpc_id            = aws_vpc.b.id
  cidr_block        = "10.1.1.0/24"
  availability_zone = data.aws_availability_zones.available.names[0]
  tags              = { Name = "peering-demo-b-subnet" }
}

resource "aws_route_table" "b" {
  vpc_id = aws_vpc.b.id
  tags   = { Name = "peering-demo-b-rt" }
}

resource "aws_route_table_association" "b" {
  subnet_id      = aws_subnet.b.id
  route_table_id = aws_route_table.b.id
}

# ============================================================
# TODO 1: Create VPC Peering Connection  [vpc.tf]
# ============================================================
# Create a VPC peering connection between VPC A and VPC B.
#
# Requirements:
#   - Resource: aws_vpc_peering_connection
#   - peer_vpc_id = VPC B's ID
#   - vpc_id = VPC A's ID (requester)
#   - auto_accept = true (same account, same region)
#   - Add a requester block to enable DNS resolution:
#     requester { allow_remote_vpc_dns_resolution = true }
#   - Add an accepter block:
#     accepter { allow_remote_vpc_dns_resolution = true }
#
# Without DNS resolution enabled, private DNS hostnames
# (like ip-10-1-1-5.ec2.internal) resolve to public IPs
# and traffic goes through the internet instead of the
# peering connection.
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_peering_connection
# ============================================================


# ============================================================
# TODO 2: Add Routes in BOTH Directions  [vpc.tf]
# ============================================================
# Add a route in VPC A's route table pointing VPC B's CIDR
# to the peering connection, AND a route in VPC B's route
# table pointing VPC A's CIDR to the peering connection.
#
# Requirements:
#   - Resource: aws_route (2 instances)
#   - Route in VPC A: destination = 10.1.0.0/16,
#     vpc_peering_connection_id = peering connection ID
#   - Route in VPC B: destination = 10.0.0.0/16,
#     vpc_peering_connection_id = peering connection ID
#
# CRITICAL: Routes must exist in BOTH VPCs. A common mistake
# is adding a route only in the requester VPC. Traffic from
# VPC A reaches VPC B, but the response cannot route back.
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route
# ============================================================
```

### `dns.tf`

```hcl
# ============================================================
# TODO 3: Route 53 Private Hosted Zone with VPC Association
#                                                     [dns.tf]
# ============================================================
# Create a Route 53 private hosted zone and associate it
# with both VPCs so DNS names resolve in both.
#
# Requirements:
#   - Resource: aws_route53_zone (private)
#     - name = "shared.internal"
#     - vpc { vpc_id = VPC A's ID }
#   - Resource: aws_route53_zone_association
#     - zone_id = hosted zone ID
#     - vpc_id = VPC B's ID
#   - Resource: aws_route53_record (A record)
#     - name = "service.shared.internal"
#     - type = "A"
#     - ttl = 300
#     - records = ["10.1.1.100"] (example IP in VPC B)
#
# This allows instances in VPC A to resolve
# "service.shared.internal" to a private IP in VPC B,
# and vice versa.
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route53_zone
# ============================================================
```

### `outputs.tf`

```hcl
output "vpc_a_id" {
  value = aws_vpc.a.id
}

output "vpc_b_id" {
  value = aws_vpc.b.id
}
```

---

## Spot the Bug

A team creates a VPC peering connection between three VPCs but complains that VPC-A cannot reach VPC-C. They have VPC-A peered with VPC-B and VPC-B peered with VPC-C:

```hcl
# VPC A <-> VPC B peering
resource "aws_vpc_peering_connection" "a_to_b" {
  vpc_id      = aws_vpc.a.id
  peer_vpc_id = aws_vpc.b.id
  auto_accept = true
}

# VPC B <-> VPC C peering
resource "aws_vpc_peering_connection" "b_to_c" {
  vpc_id      = aws_vpc.b.id
  peer_vpc_id = aws_vpc.c.id
  auto_accept = true
}

# Route in VPC A to VPC C's CIDR via the A-B peering connection
resource "aws_route" "a_to_c" {
  route_table_id            = aws_route_table.a.id
  destination_cidr_block    = "10.2.0.0/16"  # VPC C CIDR
  vpc_peering_connection_id = aws_vpc_peering_connection.a_to_b.id
}
```

<details>
<summary>Explain the bug</summary>

**VPC peering is non-transitive.** Traffic from VPC-A cannot pass through VPC-B to reach VPC-C, even if you add a route. The route `10.2.0.0/16 -> peering(A-B)` is invalid because VPC-B's CIDR is 10.1.0.0/16, not 10.2.0.0/16. AWS will not forward traffic through VPC-B to VPC-C.

This is a fundamental networking property of VPC peering:
- VPC-A can reach VPC-B through peering(A-B)
- VPC-B can reach VPC-C through peering(B-C)
- VPC-A **cannot** reach VPC-C through VPC-B

**The fix** depends on the architecture:

**Option 1: Direct peering (for small number of VPCs)**
```hcl
resource "aws_vpc_peering_connection" "a_to_c" {
  vpc_id      = aws_vpc.a.id
  peer_vpc_id = aws_vpc.c.id
  auto_accept = true
}
```

**Option 2: Transit Gateway (for many VPCs)**

Use AWS Transit Gateway (exercise 24) to create a hub that all VPCs connect to. This scales linearly (N connections for N VPCs) instead of quadratically.

**Scaling comparison:**

| VPCs | Peering Connections | TGW Attachments |
|------|-------------------|-----------------|
| 3 | 3 | 3 |
| 5 | 10 | 5 |
| 10 | 45 | 10 |
| 20 | 190 | 20 |

The exam frequently tests this non-transitive property. If the scenario describes three or more VPCs needing full mesh connectivity, Transit Gateway is almost always the correct answer.

</details>

---

## Solutions

<details>
<summary>vpc.tf -- TODO 1: VPC Peering Connection</summary>

```hcl
resource "aws_vpc_peering_connection" "a_to_b" {
  vpc_id      = aws_vpc.a.id
  peer_vpc_id = aws_vpc.b.id
  auto_accept = true

  requester {
    allow_remote_vpc_dns_resolution = true
  }

  accepter {
    allow_remote_vpc_dns_resolution = true
  }

  tags = { Name = "peering-demo-a-to-b" }
}
```

The `auto_accept = true` flag works only when both VPCs are in the same account and region. For cross-account or cross-region peering, you must create an `aws_vpc_peering_connection_accepter` resource in the accepter account/region.

DNS resolution flags ensure that private DNS hostnames (like `ip-10-1-1-5.ec2.internal`) resolve to private IPs through the peering connection instead of public IPs through the internet.

</details>

<details>
<summary>vpc.tf -- TODO 2: Bidirectional Routes</summary>

```hcl
# VPC A -> VPC B route
resource "aws_route" "a_to_b" {
  route_table_id            = aws_route_table.a.id
  destination_cidr_block    = "10.1.0.0/16"
  vpc_peering_connection_id = aws_vpc_peering_connection.a_to_b.id
}

# VPC B -> VPC A route
resource "aws_route" "b_to_a" {
  route_table_id            = aws_route_table.b.id
  destination_cidr_block    = "10.0.0.0/16"
  vpc_peering_connection_id = aws_vpc_peering_connection.a_to_b.id
}
```

Both routes use the same peering connection ID. The peering connection is bidirectional, but routing is not automatic -- you must explicitly add routes in both directions. Forgetting the return route is the most common VPC peering misconfiguration.

</details>

<details>
<summary>dns.tf -- TODO 3: Route 53 Private Hosted Zone</summary>

```hcl
resource "aws_route53_zone" "shared" {
  name = "shared.internal"

  vpc {
    vpc_id = aws_vpc.a.id
  }

  tags = { Name = "peering-demo-shared-zone" }
}

resource "aws_route53_zone_association" "b" {
  zone_id = aws_route53_zone.shared.id
  vpc_id  = aws_vpc.b.id
}

resource "aws_route53_record" "service" {
  zone_id = aws_route53_zone.shared.id
  name    = "service.shared.internal"
  type    = "A"
  ttl     = 300
  records = ["10.1.1.100"]
}
```

The private hosted zone is initially associated with VPC A (in the `vpc` block). The `aws_route53_zone_association` resource adds VPC B. Now both VPCs can resolve `service.shared.internal` to `10.1.1.100`.

For cross-account zone associations, VPC A's account must create an authorization (`aws_route53_vpc_association_authorization`), and VPC B's account must create the association.

</details>

---

## Verify What You Learned

```bash
# Verify peering connection is active
aws ec2 describe-vpc-peering-connections \
  --filters "Name=tag:Name,Values=peering-demo-a-to-b" \
  --query "VpcPeeringConnections[0].Status.Code" \
  --output text
```

Expected: `active`

```bash
# Verify routes exist in both directions
aws ec2 describe-route-tables \
  --route-table-ids $(terraform output -raw vpc_a_id | xargs -I{} aws ec2 describe-route-tables --filters "Name=vpc-id,Values={}" "Name=tag:Name,Values=peering-demo-a-rt" --query "RouteTables[0].RouteTableId" --output text) \
  --query "RouteTables[0].Routes[?DestinationCidrBlock=='10.1.0.0/16'].VpcPeeringConnectionId" \
  --output text
```

Expected: peering connection ID.

```bash
# Verify Route 53 private hosted zone
aws route53 list-hosted-zones-by-name \
  --dns-name "shared.internal" \
  --query "HostedZones[0].{Name:Name,Private:Config.PrivateZone}" \
  --output table
```

Expected: Name=shared.internal., Private=True.

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify:

```bash
terraform state list
```

Expected: no output (empty state).

---

## What's Next

VPC peering works well for connecting two VPCs, but it becomes unmanageable at scale because it is non-transitive. In the next exercise, you will explore **VPC Endpoints** -- Gateway endpoints for S3 and DynamoDB (free) versus Interface endpoints for other services (ENI-based, costs ~$0.01/hr) -- to access AWS services without traversing the internet.

---

## Summary

- **VPC peering** connects two VPCs using private IPs over the AWS backbone -- no internet traversal, no hourly charge
- Peering is **non-transitive**: A-B and B-C does not mean A-C; each pair needs its own connection
- **CIDR blocks must not overlap** between peered VPCs; plan address space before creating VPCs
- Routes must exist in **both directions** -- forgetting the return route is the most common misconfiguration
- **DNS resolution** requires `allow_remote_vpc_dns_resolution = true` on both sides of the peering connection
- Route 53 **private hosted zones** can be associated with multiple VPCs for cross-VPC DNS name resolution
- For 3+ VPCs needing full connectivity, **Transit Gateway** (O(n)) scales better than peering (O(n^2))
- Cross-account peering requires **separate accept** step; cross-region peering does not support security group references

## Reference

- [VPC Peering](https://docs.aws.amazon.com/vpc/latest/peering/what-is-vpc-peering.html)
- [VPC Peering Limitations](https://docs.aws.amazon.com/vpc/latest/peering/vpc-peering-basics.html#vpc-peering-limitations)
- [Route 53 Private Hosted Zones](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/hosted-zones-private.html)
- [Terraform aws_vpc_peering_connection](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_peering_connection)

## Additional Resources

- [VPC Peering vs Transit Gateway](https://docs.aws.amazon.com/vpc/latest/tgw/how-transit-gateways-work.html) -- when to use each based on scale and routing requirements
- [Cross-Account VPC Peering](https://docs.aws.amazon.com/vpc/latest/peering/create-vpc-peering-connection.html) -- the authorization and acceptance flow for multi-account architectures
- [DNS Resolution Over Peering](https://docs.aws.amazon.com/vpc/latest/peering/modify-peering-connections.html) -- enabling private DNS hostname resolution across peered VPCs
- [Route 53 Private Hosted Zone VPC Associations](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/hosted-zone-private-associate-vpcs.html) -- sharing DNS zones across accounts

# 5. VPC Peering and Cross-VPC DNS Resolution

<!--
difficulty: intermediate
concepts: [vpc-peering, route-tables, dns-resolution, private-hosted-zones]
tools: [terraform, aws-cli]
estimated_time: 50m
bloom_level: explain, configure, enable
prerequisites: [01-04]
aws_cost: ~$0.05/hr
-->

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.5 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Exercise 04 completed | Understand multi-AZ VPC architecture |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Explain** when VPC peering is appropriate versus other connectivity options (Transit Gateway, PrivateLink)
2. **Configure** a VPC peering connection with auto-accept for same-account peering
3. **Enable** bidirectional routing so both VPCs can reach each other
4. **Enable** DNS resolution across the peering connection
5. **Create** a Route 53 private hosted zone shared across both VPCs

## Why This Matters

Microservice architectures often span multiple VPCs. A team might run their API in one VPC and their data processing pipeline in another for blast-radius isolation. These services still need to communicate, but routing traffic over the public internet adds latency, cost, and security risk. VPC peering creates a private, direct network path between two VPCs using AWS's internal backbone -- no internet gateway, no VPN, no NAT required.

Peering alone gives you IP-to-IP connectivity, but hardcoding private IPs is fragile. Route 53 private hosted zones let you assign friendly DNS names (like `api.internal.example.com`) that resolve to private IPs only from within associated VPCs. Combined with DNS resolution enabled on the peering connection, services in VPC-A can resolve names hosted in VPC-B's private zone. This is how production teams make cross-VPC service discovery work without a service mesh.

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
  default     = "peering-lab"
}
```

### `vpc.tf`

```hcl
data "aws_availability_zones" "available" {
  state = "available"
}

data "aws_ami" "amazon_linux" {
  most_recent = true
  owners      = ["amazon"]

  filter {
    name   = "name"
    values = ["al2023-ami-2023.*-x86_64"]
  }
}

locals {
  az = data.aws_availability_zones.available.names[0]
}

# -------------------------------------------------------
# VPC A — "Services" (10.0.0.0/16)
# -------------------------------------------------------
resource "aws_vpc" "a" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = "${var.project_name}-vpc-a" }
}

resource "aws_subnet" "a_public" {
  vpc_id                  = aws_vpc.a.id
  cidr_block              = "10.0.1.0/24"
  availability_zone       = local.az
  map_public_ip_on_launch = true

  tags = { Name = "${var.project_name}-vpc-a-public" }
}

resource "aws_internet_gateway" "a" {
  vpc_id = aws_vpc.a.id
  tags   = { Name = "${var.project_name}-igw-a" }
}

resource "aws_route_table" "a_public" {
  vpc_id = aws_vpc.a.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.a.id
  }

  tags = { Name = "${var.project_name}-vpc-a-rt" }
}

resource "aws_route_table_association" "a_public" {
  subnet_id      = aws_subnet.a_public.id
  route_table_id = aws_route_table.a_public.id
}

resource "aws_security_group" "a" {
  name   = "${var.project_name}-sg-a"
  vpc_id = aws_vpc.a.id

  ingress {
    from_port   = -1
    to_port     = -1
    protocol    = "icmp"
    cidr_blocks = ["10.1.0.0/16"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "${var.project_name}-sg-a" }
}

resource "aws_instance" "a" {
  ami                    = data.aws_ami.amazon_linux.id
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.a_public.id
  vpc_security_group_ids = [aws_security_group.a.id]

  tags = { Name = "${var.project_name}-instance-a" }
}

# -------------------------------------------------------
# VPC B — "Data Processing" (10.1.0.0/16)
# -------------------------------------------------------
resource "aws_vpc" "b" {
  cidr_block           = "10.1.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = "${var.project_name}-vpc-b" }
}

resource "aws_subnet" "b_public" {
  vpc_id                  = aws_vpc.b.id
  cidr_block              = "10.1.1.0/24"
  availability_zone       = local.az
  map_public_ip_on_launch = true

  tags = { Name = "${var.project_name}-vpc-b-public" }
}

resource "aws_internet_gateway" "b" {
  vpc_id = aws_vpc.b.id
  tags   = { Name = "${var.project_name}-igw-b" }
}

resource "aws_route_table" "b_public" {
  vpc_id = aws_vpc.b.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.b.id
  }

  tags = { Name = "${var.project_name}-vpc-b-rt" }
}

resource "aws_route_table_association" "b_public" {
  subnet_id      = aws_subnet.b_public.id
  route_table_id = aws_route_table.b_public.id
}

resource "aws_security_group" "b" {
  name   = "${var.project_name}-sg-b"
  vpc_id = aws_vpc.b.id

  ingress {
    from_port   = -1
    to_port     = -1
    protocol    = "icmp"
    cidr_blocks = ["10.0.0.0/16"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "${var.project_name}-sg-b" }
}

resource "aws_instance" "b" {
  ami                    = data.aws_ami.amazon_linux.id
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.b_public.id
  vpc_security_group_ids = [aws_security_group.b.id]

  tags = { Name = "${var.project_name}-instance-b" }
}
```

### `peering.tf`

```hcl
# =======================================================
# TODO 1 — Create the VPC Peering Connection
# =======================================================
# Requirements:
#   - Create an aws_vpc_peering_connection resource
#   - The requester (peer_vpc_id) is VPC A
#   - The accepter (vpc_id) is VPC B
#     (or vice versa — just be consistent)
#   - Set auto_accept = true (same account, same region)
#   - Tag with Name = "${var.project_name}-peering"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_peering_connection


# =======================================================
# TODO 2 — Accept the Peering Connection
# =======================================================
# Requirements:
#   - Create an aws_vpc_peering_connection_accepter resource
#   - Reference the peering connection ID from TODO 1
#   - Set auto_accept = true
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_peering_connection_accepter
#
# Note: For same-account peering, auto_accept on the
#       aws_vpc_peering_connection resource is often sufficient.
#       The accepter resource gives you a place to set accepter-side
#       options (like DNS resolution). Create it anyway for practice.


# =======================================================
# TODO 3 — Add routes in BOTH VPCs pointing to the peer
# =======================================================
# Requirements:
#   - In VPC A's route table, add a route for 10.1.0.0/16
#     with the peering connection as the target
#   - In VPC B's route table, add a route for 10.0.0.0/16
#     with the peering connection as the target
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route
#
# Hint: Use aws_route (standalone resource) rather than inline route
#       blocks, since the route tables already have inline routes.


# =======================================================
# TODO 4 — Enable DNS resolution on the peering connection
# =======================================================
# Requirements:
#   - Create an aws_vpc_peering_connection_options resource
#   - Enable allow_remote_vpc_dns_resolution on BOTH the
#     requester and accepter sides
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_peering_connection_options
#
# Hint: The resource has requester {} and accepter {} blocks,
#       each with allow_remote_vpc_dns_resolution = true
```

### `dns.tf`

```hcl
# =======================================================
# TODO 5 — Create a Route 53 private hosted zone
# =======================================================
# Requirements:
#   - Create an aws_route53_zone with name = "internal.example.com"
#   - Associate it with VPC A (use the vpc {} block inside the zone)
#   - Then create an aws_route53_zone_association to ALSO
#     associate the zone with VPC B
#
# Docs:
#   https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route53_zone
#   https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route53_zone_association
#
# Hint: The zone's primary VPC goes in the aws_route53_zone vpc {} block.
#       Additional VPCs use separate aws_route53_zone_association resources.


# =======================================================
# TODO 6 — Create a DNS A record for the app in VPC B
# =======================================================
# Requirements:
#   - Create an aws_route53_record in the private hosted zone
#   - Name: "data-processor.internal.example.com"
#   - Type: A
#   - TTL: 300
#   - Value: the private IP of aws_instance.b
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route53_record
```

### `outputs.tf`

```hcl
output "vpc_a_id" {
  value = aws_vpc.a.id
}

output "vpc_b_id" {
  value = aws_vpc.b.id
}

output "instance_a_private_ip" {
  value = aws_instance.a.private_ip
}

output "instance_b_private_ip" {
  value = aws_instance.b.private_ip
}

output "peering_connection_id" {
  value = aws_vpc_peering_connection.this.id
}
```

## Spot the Bug

A teammate added the peering routes but users in VPC-B cannot reach VPC-A. Traffic from VPC-A to VPC-B works fine. **What is wrong?**

```hcl
# Route in VPC A -> VPC B (works)
resource "aws_route" "a_to_b" {
  route_table_id            = aws_route_table.a_public.id
  destination_cidr_block    = "10.1.0.0/16"
  vpc_peering_connection_id = aws_vpc_peering_connection.this.id
}

# Route in VPC B -> VPC A (MISSING!)
# The teammate forgot to add the return route.
```

<details>
<summary>Explain the bug</summary>

VPC peering is **not transitive and not automatically bidirectional**. Creating the peering connection only establishes the link -- you still need to update route tables on **both sides** to tell each VPC how to reach the other's CIDR range.

In this case, VPC A has a route for `10.1.0.0/16` pointing to the peering connection, so traffic from A reaches B. But VPC B has no route for `10.0.0.0/16`, so return packets from B have nowhere to go and are dropped.

The fix is to add the missing route:

```hcl
resource "aws_route" "b_to_a" {
  route_table_id            = aws_route_table.b_public.id
  destination_cidr_block    = "10.0.0.0/16"
  vpc_peering_connection_id = aws_vpc_peering_connection.this.id
}
```

This is one of the most common VPC peering mistakes. Always verify routes exist in **both** directions.

</details>

## Verify What You Learned

### Step 1 -- Plan and apply

Run `terraform init` and then `terraform plan`. You should see approximately **22-25 resources** to be created. Apply when ready.

```
Plan: 24 to add, 0 to change, 0 to destroy.
```

### Step 2 -- Verify peering connection is active

```
aws ec2 describe-vpc-peering-connections \
  --filters "Name=tag:Name,Values=peering-lab-peering" \
  --query "VpcPeeringConnections[].{ID:VpcPeeringConnectionId,Status:Status.Code,Requester:RequesterVpcInfo.CidrBlock,Accepter:AccepterVpcInfo.CidrBlock}" \
  --output table
```

Expected:

```
--------------------------------------------------------------------
|                  DescribeVpcPeeringConnections                   |
+----------------+----------+----------------+---------------------+
|    Accepter    |    ID    |   Requester    |      Status         |
+----------------+----------+----------------+---------------------+
|  10.1.0.0/16   |  pcx-... |  10.0.0.0/16  |  active             |
+----------------+----------+----------------+---------------------+
```

### Step 3 -- Verify routes exist in both VPCs

Check VPC A's route table includes a route to `10.1.0.0/16`:

```
aws ec2 describe-route-tables \
  --route-table-ids $(aws ec2 describe-route-tables \
    --filters "Name=tag:Name,Values=peering-lab-vpc-a-rt" \
    --query "RouteTables[0].RouteTableId" --output text) \
  --query "RouteTables[0].Routes[?DestinationCidrBlock=='10.1.0.0/16'].{Dest:DestinationCidrBlock,Target:VpcPeeringConnectionId}" \
  --output table
```

Repeat for VPC B with destination `10.0.0.0/16`.

### Step 4 -- Verify DNS resolution

```
aws route53 list-hosted-zones-by-name \
  --dns-name "internal.example.com" \
  --query "HostedZones[?Name=='internal.example.com.'].{ID:Id,Name:Name,Private:Config.PrivateZone}" \
  --output table
```

Expected:

```
---------------------------------------------------
|            ListHostedZonesByName                |
+-----------------------+---------+---------------+
|          ID           |  Name   |   Private     |
+-----------------------+---------+---------------+
|  /hostedzone/Z0...   |  internal.example.com. |  True  |
+-----------------------+---------+---------------+
```

Then verify the A record resolves to instance B's private IP:

```
ZONE_ID=$(aws route53 list-hosted-zones-by-name \
  --dns-name "internal.example.com" \
  --query "HostedZones[0].Id" --output text)

aws route53 list-resource-record-sets \
  --hosted-zone-id "$ZONE_ID" \
  --query "ResourceRecordSets[?Name=='data-processor.internal.example.com.']" \
  --output table
```

## Solutions

<details>
<summary>TODO 1 -- VPC Peering Connection (peering.tf)</summary>

```hcl
resource "aws_vpc_peering_connection" "this" {
  vpc_id        = aws_vpc.a.id
  peer_vpc_id   = aws_vpc.b.id
  auto_accept   = true

  tags = {
    Name = "${var.project_name}-peering"
  }
}
```

</details>

<details>
<summary>TODO 2 -- Accept the Peering Connection (peering.tf)</summary>

```hcl
resource "aws_vpc_peering_connection_accepter" "this" {
  vpc_peering_connection_id = aws_vpc_peering_connection.this.id
  auto_accept               = true

  tags = {
    Name = "${var.project_name}-peering-accepter"
  }
}
```

</details>

<details>
<summary>TODO 3 -- Bidirectional routes (peering.tf)</summary>

```hcl
resource "aws_route" "a_to_b" {
  route_table_id            = aws_route_table.a_public.id
  destination_cidr_block    = "10.1.0.0/16"
  vpc_peering_connection_id = aws_vpc_peering_connection.this.id
}

resource "aws_route" "b_to_a" {
  route_table_id            = aws_route_table.b_public.id
  destination_cidr_block    = "10.0.0.0/16"
  vpc_peering_connection_id = aws_vpc_peering_connection.this.id
}
```

</details>

<details>
<summary>TODO 4 -- DNS resolution options (peering.tf)</summary>

```hcl
resource "aws_vpc_peering_connection_options" "this" {
  vpc_peering_connection_id = aws_vpc_peering_connection.this.id

  requester {
    allow_remote_vpc_dns_resolution = true
  }

  accepter {
    allow_remote_vpc_dns_resolution = true
  }
}
```

</details>

<details>
<summary>TODO 5 -- Route 53 private hosted zone (dns.tf)</summary>

```hcl
resource "aws_route53_zone" "internal" {
  name = "internal.example.com"

  vpc {
    vpc_id = aws_vpc.a.id
  }

  tags = {
    Name = "${var.project_name}-internal-zone"
  }
}

resource "aws_route53_zone_association" "b" {
  zone_id = aws_route53_zone.internal.zone_id
  vpc_id  = aws_vpc.b.id
}
```

</details>

<details>
<summary>TODO 6 -- DNS A record (dns.tf)</summary>

```hcl
resource "aws_route53_record" "data_processor" {
  zone_id = aws_route53_zone.internal.zone_id
  name    = "data-processor.internal.example.com"
  type    = "A"
  ttl     = 300
  records = [aws_instance.b.private_ip]
}
```

</details>

## Cleanup

Destroy all resources:

```
terraform destroy
```

Type `yes` when prompted. The Route 53 zone associations must be removed before the zone can be deleted -- Terraform handles this ordering automatically.

## What's Next

In **Exercise 06 -- CloudFront with Custom Origin, OAC, and WAF**, you will place a CDN in front of an ALB, protect it with WAF rate limiting, and restrict the origin to only accept traffic from CloudFront.

## Summary

You built cross-VPC connectivity with:

- **VPC Peering** for private, low-latency communication between two VPCs
- **Bidirectional routes** so traffic flows in both directions
- **DNS resolution** enabled on the peering connection for hostname-based discovery
- **Route 53 private hosted zone** shared across both VPCs for service discovery

This pattern is the foundation for multi-VPC architectures. For more than 2-3 VPCs, consider Transit Gateway (Exercise 08) instead of the N-squared peering connections that full-mesh peering requires.

## Reference

| Resource | Purpose |
|----------|---------|
| `aws_vpc_peering_connection` | Establishes the peering link |
| `aws_vpc_peering_connection_accepter` | Accepts the peering (other side) |
| `aws_vpc_peering_connection_options` | DNS resolution settings |
| `aws_route` | Adds routing entries for cross-VPC traffic |
| `aws_route53_zone` | Private DNS zone |
| `aws_route53_zone_association` | Associates zone with additional VPCs |
| `aws_route53_record` | DNS record within the zone |

## Additional Resources

- [VPC Peering Documentation](https://docs.aws.amazon.com/vpc/latest/peering/what-is-vpc-peering.html)
- [VPC Peering Limitations](https://docs.aws.amazon.com/vpc/latest/peering/vpc-peering-basics.html#vpc-peering-limitations)
- [Route 53 Private Hosted Zones](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/hosted-zones-private.html)
- [DNS Resolution for Peering Connections](https://docs.aws.amazon.com/vpc/latest/peering/modify-peering-connections.html#vpc-peering-dns)

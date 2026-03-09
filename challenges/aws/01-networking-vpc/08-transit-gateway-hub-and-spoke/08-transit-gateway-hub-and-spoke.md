# 8. Multi-VPC Hub-and-Spoke with Transit Gateway

<!--
difficulty: advanced
concepts: [transit-gateway, hub-and-spoke, route-table-segmentation, tgw-attachments, vpc-flow-logs, centralized-nat]
tools: [terraform, aws-cli]
estimated_time: 90m
bloom_level: evaluate
prerequisites: [01-04, 01-05]
aws_cost: ~$0.25/hr
-->

> **AWS Cost Warning:** This exercise creates a Transit Gateway (~$0.05/hr), 4 VPC attachments (~$0.05/hr each), a NAT Gateway (~$0.045/hr), and EC2 instances. Estimated total: ~$0.25/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercises 04 (Production Multi-AZ VPC) and 05 (VPC Peering and DNS)
- Familiarity with VPC route tables and CIDR allocation

## Learning Objectives

After completing this exercise, you will be able to:

- **Design** a hub-and-spoke network topology using AWS Transit Gateway
- **Implement** network segmentation by creating separate TGW route tables per environment
- **Configure** centralized NAT egress through a shared-services VPC
- **Evaluate** route propagation rules to enforce isolation between spoke VPCs
- **Verify** that segmentation policies prevent unauthorized inter-spoke traffic

## Why Transit Gateway

VPC peering works for connecting two or three VPCs, but the number of peering connections grows quadratically -- 10 VPCs need 45 peerings, each with its own route entries. AWS Transit Gateway acts as a regional network hub: every VPC connects once to the TGW, and routing is controlled centrally through TGW route tables. Combined with route table segmentation, you can enforce strict isolation (e.g., production cannot reach development) while still allowing shared services to communicate with everyone. This hub-and-spoke model is the foundation of most enterprise AWS network architectures. See the [AWS Transit Gateway documentation](https://docs.aws.amazon.com/vpc/latest/tgw/what-is-transit-gateway.html) for the full feature set.

## The Challenge

Build a hub-and-spoke network connecting **4 VPCs** through a single AWS Transit Gateway:

| VPC | CIDR | Role |
|-----|------|------|
| shared-services | 10.0.0.0/16 | Hub -- NAT Gateway, central egress |
| dev | 10.1.0.0/16 | Spoke -- development workloads |
| staging | 10.2.0.0/16 | Spoke -- staging workloads |
| prod | 10.3.0.0/16 | Spoke -- production workloads |

### Requirements

1. **Transit Gateway** with `default_route_table_association` and `default_route_table_propagation` both disabled (you will manage route tables manually)
2. **4 TGW VPC attachments** -- one per VPC, attached to private subnets
3. **Network segmentation** -- dev, staging, and prod cannot communicate with each other. Only shared-services can reach all three spokes
4. **Separate TGW route tables** -- one for shared-services (receives propagations from all spokes), one per spoke (receives propagation only from shared-services)
5. **Centralized NAT** -- all internet-bound traffic from spoke VPCs routes through the NAT Gateway in shared-services
6. **VPC Flow Logs** enabled on all 4 VPCs, writing to CloudWatch Logs
7. **EC2 test instances** in each VPC (t3.micro) to validate connectivity

### Architecture

```
                    ┌─────────────────────┐
                    │   Shared Services    │
                    │  10.0.0.0/16        │
                    │  IGW + NAT Gateway  │
                    └─────────┬───────────┘
                              │ TGW Attachment
                    ┌─────────┴───────────┐
                    │   Transit Gateway    │
                    │  (route table        │
                    │   segmentation)      │
                    └──┬──────┼────────┬──┘
                       │      │        │
               ┌───────┘      │        └───────┐
               │              │                │
    ┌──────────┴──┐  ┌───────┴─────┐  ┌───────┴──────┐
    │    Dev       │  │   Staging   │  │    Prod      │
    │ 10.1.0.0/16 │  │ 10.2.0.0/16│  │ 10.3.0.0/16  │
    └─────────────┘  └─────────────┘  └──────────────┘
         ✗ ──────── no connectivity ────────── ✗
```

## Hints

Work through these one at a time. Only open the next hint if you are stuck.

<details>
<summary>Hint 1: Transit Gateway basics</summary>

Create the Transit Gateway with manual route table management:

```
resource "aws_ec2_transit_gateway" -- set these arguments:
  - auto_accept_shared_attachments = "enable"
  - default_route_table_association = "disable"
  - default_route_table_propagation = "disable"
```

Disabling the defaults is critical. If you leave them enabled, AWS automatically associates every attachment with a single default route table and propagates all routes there -- which means every VPC can reach every other VPC with zero segmentation.

</details>

<details>
<summary>Hint 2: VPC attachments</summary>

Create one `aws_ec2_transit_gateway_vpc_attachment` per VPC. Each attachment references:
- The Transit Gateway ID
- The VPC ID
- A list of **private** subnet IDs (one per AZ)

For the shared-services attachment, you will also need to set `appliance_mode_support = "enable"` if you plan to route return traffic symmetrically through the NAT Gateway.

Use `for_each` over a map of VPC configurations to avoid repeating yourself four times.

</details>

<details>
<summary>Hint 3: TGW route tables and associations</summary>

Create **4 TGW route tables** -- one per VPC:
- `tgw-rt-shared-services`
- `tgw-rt-dev`
- `tgw-rt-staging`
- `tgw-rt-prod`

Associate each VPC's attachment with its own TGW route table using `aws_ec2_transit_gateway_route_table_association`.

For **propagation**: the shared-services route table should receive propagations from **all 4 attachments** (so it knows how to route to every VPC). Each spoke route table receives propagation **only from shared-services** (so it can only reach shared-services, not the other spokes).

Use `aws_ec2_transit_gateway_route_table_propagation` for each propagation relationship.

</details>

<details>
<summary>Hint 4: Centralized NAT egress routing</summary>

To route internet traffic from spokes through the shared-services NAT Gateway:

1. In each **spoke TGW route table**, add a static default route (`0.0.0.0/0`) pointing to the **shared-services TGW attachment** using `aws_ec2_transit_gateway_route`
2. In each **spoke VPC route table** (private subnets), add a default route (`0.0.0.0/0`) pointing to the TGW attachment ID
3. In the **shared-services VPC**, ensure the private subnet route table sends `0.0.0.0/0` to the NAT Gateway
4. In the **shared-services VPC**, ensure the NAT Gateway's public subnet route table sends `0.0.0.0/0` to the IGW
5. In the **shared-services VPC**, add routes for each spoke CIDR (`10.1.0.0/16`, `10.2.0.0/16`, `10.3.0.0/16`) pointing to the TGW in the NAT subnet route table so return traffic goes back through the TGW

</details>

<details>
<summary>Hint 5: VPC Flow Logs and testing strategy</summary>

For flow logs on each VPC, you need:
- An `aws_cloudwatch_log_group` per VPC
- An `aws_iam_role` with a trust policy allowing `vpc-flow-logs.amazonaws.com` to assume it
- An `aws_iam_role_policy` granting `logs:CreateLogGroup`, `logs:CreateLogStream`, `logs:PutLogEvents`, `logs:DescribeLogGroups`, `logs:DescribeLogStreams`
- An `aws_flow_log` per VPC referencing the log group and IAM role

For testing connectivity, use SSM Session Manager (add the `AmazonSSMManagedInstanceCore` IAM instance profile) or deploy instances with user-data that installs a simple web server. Test matrix:
- shared-services -> dev: SHOULD WORK
- dev -> shared-services: SHOULD WORK
- dev -> staging: SHOULD FAIL
- dev -> prod: SHOULD FAIL
- staging -> prod: SHOULD FAIL

</details>

## Spot the Bug

A colleague wrote this Transit Gateway configuration but every spoke VPC can ping every other spoke. Why?

```hcl
resource "aws_ec2_transit_gateway" "this" {
  description = "Hub and spoke TGW"

  auto_accept_shared_attachments  = "enable"
  default_route_table_association = "enable"
  default_route_table_propagation = "enable"

  tags = { Name = "hub-spoke-tgw" }
}

resource "aws_ec2_transit_gateway_vpc_attachment" "dev" {
  transit_gateway_id = aws_ec2_transit_gateway.this.id
  vpc_id             = aws_vpc.dev.id
  subnet_ids         = [aws_subnet.dev_private.id]
}

resource "aws_ec2_transit_gateway_vpc_attachment" "staging" {
  transit_gateway_id = aws_ec2_transit_gateway.this.id
  vpc_id             = aws_vpc.staging.id
  subnet_ids         = [aws_subnet.staging_private.id]
}

resource "aws_ec2_transit_gateway_vpc_attachment" "prod" {
  transit_gateway_id = aws_ec2_transit_gateway.this.id
  vpc_id             = aws_vpc.prod.id
  subnet_ids         = [aws_subnet.prod_private.id]
}
```

<details>
<summary>Explain the bug</summary>

**The problem:** `default_route_table_association = "enable"` and `default_route_table_propagation = "enable"` cause AWS to automatically associate **every** attachment with the **same default TGW route table** and propagate **every** VPC's routes into that single table.

The result is a flat, fully-meshed network: dev's route table contains routes to staging and prod (and vice versa), completely defeating hub-and-spoke segmentation.

**The fix:** Set both to `"disable"` and create separate TGW route tables per environment. Associate each attachment with its own route table and configure propagations explicitly -- spoke route tables should only receive propagations from the shared-services attachment.

```hcl
resource "aws_ec2_transit_gateway" "this" {
  description = "Hub and spoke TGW"

  auto_accept_shared_attachments  = "enable"
  default_route_table_association = "disable"
  default_route_table_propagation = "disable"

  tags = { Name = "hub-spoke-tgw" }
}
```

</details>

## Verify What You Learned

### Transit Gateway exists with correct settings

```bash
aws ec2 describe-transit-gateways \
  --filters "Name=tag:Name,Values=<your-tgw-name>" \
  --query "TransitGateways[0].Options.{DefaultAssociation:DefaultRouteTableAssociation,DefaultPropagation:DefaultRouteTablePropagation}" \
  --output table
```

Expected:

```
------------------------------------------
|       DescribeTransitGateways          |
+--------------------+-------------------+
| DefaultAssociation |  DefaultPropagation|
+--------------------+-------------------+
|  disable           |  disable          |
+--------------------+-------------------+
```

### Four TGW route tables exist

```bash
aws ec2 describe-transit-gateway-route-tables \
  --filters "Name=transit-gateway-id,Values=<your-tgw-id>" \
  --query "TransitGatewayRouteTables[].Tags[?Key=='Name'].Value | []" \
  --output table
```

Expected: four route table names (shared-services, dev, staging, prod).

### Spoke isolation -- dev cannot reach staging

```bash
# From the dev EC2 instance (via SSM or SSH):
ping -c 3 <staging-instance-private-ip>
```

Expected:

```
PING 10.2.x.x (10.2.x.x) 56(84) bytes of data.

--- 10.2.x.x ping statistics ---
3 packets transmitted, 0 received, 100% packet loss, time 2003ms
```

### Spoke can reach shared-services

```bash
# From the dev EC2 instance:
ping -c 3 <shared-services-instance-private-ip>
```

Expected:

```
PING 10.0.x.x (10.0.x.x) 56(84) bytes of data.
64 bytes from 10.0.x.x: icmp_seq=1 ttl=254 time=1.23 ms
64 bytes from 10.0.x.x: icmp_seq=2 ttl=254 time=0.98 ms
64 bytes from 10.0.x.x: icmp_seq=3 ttl=254 time=1.05 ms
```

### Centralized NAT egress works from spoke

```bash
# From the dev EC2 instance:
curl -s ifconfig.me
```

Expected: the Elastic IP of the shared-services NAT Gateway (not the dev instance's IP).

### VPC Flow Logs are active

```bash
aws ec2 describe-flow-logs \
  --filter "Name=resource-id,Values=<your-shared-services-vpc-id>" \
  --query "FlowLogs[0].{Status:FlowLogStatus,LogGroup:LogGroupName}" \
  --output table
```

Expected:

```
-------------------------------------
|         DescribeFlowLogs          |
+-----------+-----------------------+
|  Status   |  LogGroup             |
+-----------+-----------------------+
|  ACTIVE   |  /vpc/shared-services |
+-----------+-----------------------+
```

### Terraform state is clean

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

Destroy all resources to stop incurring charges:

```bash
terraform destroy -auto-approve
```

Transit Gateway attachments can take several minutes to delete. If the destroy times out, run it again.

Verify nothing remains:

```bash
terraform state list
```

Expected: no output (empty state).

```bash
aws ec2 describe-transit-gateways \
  --filters "Name=tag:Name,Values=<your-tgw-name>" \
  --query "TransitGateways[0].State" \
  --output text
```

Expected: `deleted` or no output.

## What's Next

You now have a centralized hub-and-spoke network with segmentation. In the next exercise, you will explore **AWS PrivateLink** -- a way to expose services across VPC boundaries without any routing, peering, or Transit Gateway at all.

## Summary

- **Transit Gateway** is a regional hub that simplifies multi-VPC networking from O(n^2) peerings to O(n) attachments
- **Disabling default route table association and propagation** is mandatory for network segmentation -- without it, every VPC can reach every other VPC
- **Separate TGW route tables** per environment enforce isolation: spoke route tables only propagate routes to/from shared-services
- **Centralized NAT** reduces cost and operational overhead by funneling all internet egress through a single NAT Gateway in the hub VPC
- **VPC Flow Logs** provide visibility into accepted and rejected traffic, essential for validating segmentation policies

## Reference

- [AWS Transit Gateway Documentation](https://docs.aws.amazon.com/vpc/latest/tgw/what-is-transit-gateway.html)
- [Terraform aws_ec2_transit_gateway](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ec2_transit_gateway)
- [Terraform aws_ec2_transit_gateway_route_table](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ec2_transit_gateway_route_table)
- [Terraform aws_flow_log](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/flow_log)

## Additional Resources

- [Transit Gateway Design Best Practices (AWS Blog)](https://aws.amazon.com/blogs/networking-and-content-delivery/building-a-scalable-and-secure-multi-vpc-aws-network-infrastructure/) -- reference architecture for hub-and-spoke with segmentation
- [Centralized Egress with Transit Gateway (AWS Whitepaper)](https://docs.aws.amazon.com/whitepapers/latest/building-scalable-secure-multi-vpc-network-infrastructure/centralized-egress-to-internet.html) -- detailed walkthrough of centralized NAT patterns
- [Transit Gateway Route Table Segmentation](https://docs.aws.amazon.com/vpc/latest/tgw/transit-gateway-isolated-shared.html) -- isolated VPCs with shared services pattern
- [VPC Flow Log Record Examples](https://docs.aws.amazon.com/vpc/latest/userguide/flow-log-records.html) -- understanding flow log fields and analyzing traffic patterns

<details>
<summary>Full Solution</summary>

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
  region = "us-east-1"
}
```

### `locals.tf`

```hcl
locals {
  vpcs = {
    shared-services = { cidr = "10.0.0.0/16", is_hub = true }
    dev             = { cidr = "10.1.0.0/16", is_hub = false }
    staging         = { cidr = "10.2.0.0/16", is_hub = false }
    prod            = { cidr = "10.3.0.0/16", is_hub = false }
  }
  spokes = { for k, v in local.vpcs : k => v if !v.is_hub }
  azs    = slice(data.aws_availability_zones.available.names, 0, 2)
}

data "aws_availability_zones" "available" {
  state = "available"
}

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
```

### `vpc.tf`

```hcl
# ---------------------------------------------------------------
# VPCs
# ---------------------------------------------------------------
resource "aws_vpc" "this" {
  for_each             = local.vpcs
  cidr_block           = each.value.cidr
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags                 = { Name = each.key }
}

resource "aws_subnet" "private" {
  for_each          = local.vpcs
  vpc_id            = aws_vpc.this[each.key].id
  cidr_block        = cidrsubnet(each.value.cidr, 8, 1)
  availability_zone = local.azs[0]
  tags              = { Name = "${each.key}-private" }
}

resource "aws_subnet" "public" {
  vpc_id                  = aws_vpc.this["shared-services"].id
  cidr_block              = cidrsubnet(local.vpcs["shared-services"].cidr, 8, 10)
  availability_zone       = local.azs[0]
  map_public_ip_on_launch = true
  tags                    = { Name = "shared-services-public" }
}

# ---------------------------------------------------------------
# Internet Gateway + NAT Gateway (shared-services only)
# ---------------------------------------------------------------
resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this["shared-services"].id
  tags   = { Name = "shared-services-igw" }
}

resource "aws_eip" "nat" {
  domain = "vpc"
  tags   = { Name = "shared-services-nat-eip" }
}

resource "aws_nat_gateway" "this" {
  allocation_id = aws_eip.nat.id
  subnet_id     = aws_subnet.public.id
  tags          = { Name = "shared-services-nat" }
  depends_on    = [aws_internet_gateway.this]
}

# ---------------------------------------------------------------
# Route tables -- shared-services
# ---------------------------------------------------------------
resource "aws_route_table" "shared_public" {
  vpc_id = aws_vpc.this["shared-services"].id
  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this.id
  }
  tags = { Name = "shared-services-public-rt" }
}

resource "aws_route_table_association" "shared_public" {
  subnet_id      = aws_subnet.public.id
  route_table_id = aws_route_table.shared_public.id
}

resource "aws_route_table" "shared_private" {
  vpc_id = aws_vpc.this["shared-services"].id
  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.this.id
  }
  tags = { Name = "shared-services-private-rt" }
}

resource "aws_route_table_association" "shared_private" {
  subnet_id      = aws_subnet.private["shared-services"].id
  route_table_id = aws_route_table.shared_private.id
}

resource "aws_route" "shared_private_to_spokes" {
  for_each               = local.spokes
  route_table_id         = aws_route_table.shared_private.id
  destination_cidr_block = each.value.cidr
  transit_gateway_id     = aws_ec2_transit_gateway.this.id
  depends_on             = [aws_ec2_transit_gateway_vpc_attachment.this]
}

# ---------------------------------------------------------------
# Route tables -- spokes (default route to TGW)
# ---------------------------------------------------------------
resource "aws_route_table" "spoke_private" {
  for_each = local.spokes
  vpc_id   = aws_vpc.this[each.key].id
  route {
    cidr_block         = "0.0.0.0/0"
    transit_gateway_id = aws_ec2_transit_gateway.this.id
  }
  tags       = { Name = "${each.key}-private-rt" }
  depends_on = [aws_ec2_transit_gateway_vpc_attachment.this]
}

resource "aws_route_table_association" "spoke_private" {
  for_each       = local.spokes
  subnet_id      = aws_subnet.private[each.key].id
  route_table_id = aws_route_table.spoke_private[each.key].id
}
```

### `tgw.tf`

```hcl
# ---------------------------------------------------------------
# Transit Gateway
# ---------------------------------------------------------------
resource "aws_ec2_transit_gateway" "this" {
  description                     = "Hub and spoke TGW"
  auto_accept_shared_attachments  = "enable"
  default_route_table_association = "disable"
  default_route_table_propagation = "disable"
  tags                            = { Name = "hub-spoke-tgw" }
}

resource "aws_ec2_transit_gateway_vpc_attachment" "this" {
  for_each           = local.vpcs
  transit_gateway_id = aws_ec2_transit_gateway.this.id
  vpc_id             = aws_vpc.this[each.key].id
  subnet_ids         = [aws_subnet.private[each.key].id]
  tags               = { Name = "${each.key}-tgw-attachment" }
}

# ---------------------------------------------------------------
# TGW route tables
# ---------------------------------------------------------------
resource "aws_ec2_transit_gateway_route_table" "this" {
  for_each           = local.vpcs
  transit_gateway_id = aws_ec2_transit_gateway.this.id
  tags               = { Name = "tgw-rt-${each.key}" }
}

resource "aws_ec2_transit_gateway_route_table_association" "this" {
  for_each                       = local.vpcs
  transit_gateway_attachment_id  = aws_ec2_transit_gateway_vpc_attachment.this[each.key].id
  transit_gateway_route_table_id = aws_ec2_transit_gateway_route_table.this[each.key].id
}

# Shared-services route table: propagations from ALL VPCs
resource "aws_ec2_transit_gateway_route_table_propagation" "hub_from_all" {
  for_each                       = local.vpcs
  transit_gateway_attachment_id  = aws_ec2_transit_gateway_vpc_attachment.this[each.key].id
  transit_gateway_route_table_id = aws_ec2_transit_gateway_route_table.this["shared-services"].id
}

# Spoke route tables: propagation ONLY from shared-services
resource "aws_ec2_transit_gateway_route_table_propagation" "spoke_from_hub" {
  for_each                       = local.spokes
  transit_gateway_attachment_id  = aws_ec2_transit_gateway_vpc_attachment.this["shared-services"].id
  transit_gateway_route_table_id = aws_ec2_transit_gateway_route_table.this[each.key].id
}

# Spoke route tables: default route to shared-services (for NAT egress)
resource "aws_ec2_transit_gateway_route" "spoke_default" {
  for_each                       = local.spokes
  destination_cidr_block         = "0.0.0.0/0"
  transit_gateway_attachment_id  = aws_ec2_transit_gateway_vpc_attachment.this["shared-services"].id
  transit_gateway_route_table_id = aws_ec2_transit_gateway_route_table.this[each.key].id
}
```

### `monitoring.tf`

```hcl
# ---------------------------------------------------------------
# VPC Flow Logs
# ---------------------------------------------------------------
resource "aws_cloudwatch_log_group" "flow_logs" {
  for_each          = local.vpcs
  name              = "/vpc/${each.key}"
  retention_in_days = 7
}

resource "aws_iam_role" "flow_logs" {
  name = "vpc-flow-logs-role"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = { Service = "vpc-flow-logs.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy" "flow_logs" {
  name = "vpc-flow-logs-policy"
  role = aws_iam_role.flow_logs.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = [
        "logs:CreateLogGroup",
        "logs:CreateLogStream",
        "logs:PutLogEvents",
        "logs:DescribeLogGroups",
        "logs:DescribeLogStreams"
      ]
      Effect   = "Allow"
      Resource = "*"
    }]
  })
}

resource "aws_flow_log" "this" {
  for_each             = local.vpcs
  vpc_id               = aws_vpc.this[each.key].id
  traffic_type         = "ALL"
  log_destination_type = "cloud-watch-logs"
  log_destination      = aws_cloudwatch_log_group.flow_logs[each.key].arn
  iam_role_arn         = aws_iam_role.flow_logs.arn
  tags                 = { Name = "${each.key}-flow-log" }
}
```

### `security.tf`

```hcl
# ---------------------------------------------------------------
# Security groups -- allow ICMP and egress for testing
# ---------------------------------------------------------------
resource "aws_security_group" "test" {
  for_each    = local.vpcs
  name        = "${each.key}-test-sg"
  description = "Allow ICMP and all egress for connectivity tests"
  vpc_id      = aws_vpc.this[each.key].id
  tags        = { Name = "${each.key}-test-sg" }
}

resource "aws_vpc_security_group_ingress_rule" "icmp" {
  for_each          = local.vpcs
  security_group_id = aws_security_group.test[each.key].id
  cidr_ipv4         = "10.0.0.0/8"
  from_port         = -1
  to_port           = -1
  ip_protocol       = "icmp"
}

resource "aws_vpc_security_group_egress_rule" "all" {
  for_each          = local.vpcs
  security_group_id = aws_security_group.test[each.key].id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}
```

### `compute.tf`

```hcl
# ---------------------------------------------------------------
# EC2 test instances
# ---------------------------------------------------------------
resource "aws_instance" "test" {
  for_each               = local.vpcs
  ami                    = data.aws_ami.amazon_linux_2023.id
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.private[each.key].id
  vpc_security_group_ids = [aws_security_group.test[each.key].id]
  tags                   = { Name = "${each.key}-test" }
}
```

### `outputs.tf`

```hcl
output "transit_gateway_id" {
  value = aws_ec2_transit_gateway.this.id
}

output "instance_private_ips" {
  value = { for k, v in aws_instance.test : k => v.private_ip }
}

output "nat_gateway_public_ip" {
  value = aws_eip.nat.public_ip
}
```

</details>

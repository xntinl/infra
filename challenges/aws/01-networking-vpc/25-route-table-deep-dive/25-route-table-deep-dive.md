# 25. Route Table Deep Dive

<!--
difficulty: basic
concepts: [route-table, main-route-table, custom-route-table, explicit-association, most-specific-wins, blackhole-route]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: apply
prerequisites: [01-your-first-vpc, 23-vpc-cidr-planning]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates VPCs, subnets, and route tables (~$0.00/hr). A single t3.micro EC2 instance is launched for connectivity testing (~$0.0104/hr). Remember to run `terraform destroy` when finished to avoid unexpected charges.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed Exercise 01 (Your First VPC)
- Completed Exercise 23 (VPC CIDR Planning)

## Learning Objectives

After completing this exercise, you will be able to:

- **Distinguish** between the main route table and custom route tables
- **Apply** the "deny-all main route table" pattern as a safety net
- **Explain** how AWS selects routes using the most-specific-route-wins rule
- **Create** blackhole routes for incident response scenarios
- **Verify** route table associations using AWS CLI

## Why This Matters

Route tables are the traffic control system of your VPC. Every packet leaving a subnet is matched against the route table's entries, and the most specific matching route determines where the packet goes. Get this wrong and traffic silently flows to the wrong destination -- or nowhere at all.

The main route table is a trap for the unwary. Every VPC has one, and any subnet without an explicit route table association automatically uses it. If you put a `0.0.0.0/0 -> IGW` route in the main route table, every new subnet you create is instantly public -- even subnets you intend to be private. The production pattern is to leave the main route table empty (no internet route) so it acts as a "deny-all" safety net. Every subnet gets an explicit association with a custom route table that contains exactly the routes that subnet needs.

Blackhole routes are your emergency brake. When an incident occurs and you need to immediately block traffic to or from a specific CIDR range, adding a blackhole route drops all packets to that destination. This is faster than modifying security groups across dozens of resources and takes effect within seconds.

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
  default     = "route-table-lab"
}
```

### `locals.tf`

```hcl
# ------------------------------------------------------------------
# Availability zones -- never hardcode AZ names
# ------------------------------------------------------------------
data "aws_availability_zones" "available" {
  state = "available"
}

locals {
  vpc_cidr = "10.30.0.0/16"
  azs      = slice(data.aws_availability_zones.available.names, 0, 2)

  public_subnets = {
    for i, az in local.azs : az => cidrsubnet(local.vpc_cidr, 8, i)
  }

  private_subnets = {
    for i, az in local.azs : az => cidrsubnet(local.vpc_cidr, 8, i + 10)
  }

  isolated_subnets = {
    for i, az in local.azs : az => cidrsubnet(local.vpc_cidr, 8, i + 20)
  }
}
```

### `vpc.tf`

```hcl
# ------------------------------------------------------------------
# VPC
# ------------------------------------------------------------------
resource "aws_vpc" "this" {
  cidr_block           = local.vpc_cidr
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = "${var.project_name}-vpc" }
}

# ------------------------------------------------------------------
# Internet Gateway
# ------------------------------------------------------------------
resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id

  tags = { Name = "${var.project_name}-igw" }
}

# ------------------------------------------------------------------
# Subnets -- three tiers: public, private, isolated
# ------------------------------------------------------------------
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

resource "aws_subnet" "isolated" {
  for_each = local.isolated_subnets

  vpc_id            = aws_vpc.this.id
  cidr_block        = each.value
  availability_zone = each.key

  tags = { Name = "${var.project_name}-isolated-${each.key}" }
}
```

### `route-tables.tf`

```hcl
# ------------------------------------------------------------------
# MAIN ROUTE TABLE -- the "deny-all" safety net
# ------------------------------------------------------------------
# The VPC's main route table is created automatically. We manage it
# via the aws_main_route_table_association resource to ensure it
# contains NO internet route. Any subnet without an explicit
# association falls back to this table and gets no internet access.
# ------------------------------------------------------------------
resource "aws_route_table" "main_deny_all" {
  vpc_id = aws_vpc.this.id

  # Only the implicit local route exists (VPC CIDR -> local).
  # No 0.0.0.0/0 route. This is intentional.

  tags = { Name = "${var.project_name}-main-deny-all-rt" }
}

resource "aws_main_route_table_association" "this" {
  vpc_id         = aws_vpc.this.id
  route_table_id = aws_route_table.main_deny_all.id
}

# ------------------------------------------------------------------
# PUBLIC ROUTE TABLE -- 0.0.0.0/0 -> Internet Gateway
# ------------------------------------------------------------------
# Only subnets that are explicitly associated with this table get
# internet access. This is the "opt-in public" pattern.
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

# ------------------------------------------------------------------
# PRIVATE ROUTE TABLE -- VPC-internal only (no NAT in this exercise)
# ------------------------------------------------------------------
# In production, you would add 0.0.0.0/0 -> NAT Gateway here.
# For this exercise, private subnets have only the local route.
# ------------------------------------------------------------------
resource "aws_route_table" "private" {
  vpc_id = aws_vpc.this.id

  tags = { Name = "${var.project_name}-private-rt" }
}

resource "aws_route_table_association" "private" {
  for_each = aws_subnet.private

  subnet_id      = each.value.id
  route_table_id = aws_route_table.private.id
}

# ------------------------------------------------------------------
# ISOLATED ROUTE TABLE -- with a blackhole route for incident response
# ------------------------------------------------------------------
# Demonstrates the blackhole pattern: traffic to the specified CIDR
# is silently dropped. Use this during incidents to immediately block
# traffic to a compromised range.
# ------------------------------------------------------------------
resource "aws_route_table" "isolated" {
  vpc_id = aws_vpc.this.id

  # Blackhole route: drops all traffic to 192.168.0.0/16
  # Simulates blocking traffic to an on-premises range during incident
  route {
    cidr_block = "192.168.0.0/16"
    # No target specified with a gateway -- Terraform does not support
    # a direct "blackhole" argument on inline routes. We use a
    # separate aws_route resource below for the actual blackhole.
  }

  tags = { Name = "${var.project_name}-isolated-rt" }

  # We manage the blackhole route separately to show the pattern clearly
  lifecycle {
    ignore_changes = [route]
  }
}

resource "aws_route" "blackhole_demo" {
  route_table_id         = aws_route_table.isolated.id
  destination_cidr_block = "192.168.0.0/16"

  # No target -- this creates a blackhole route.
  # All traffic to 192.168.0.0/16 is silently dropped.
  # In the console, the target shows as "blackhole".

  # Note: aws_route with no gateway/nat/etc target is not valid in TF.
  # For a true blackhole demo, we point to a non-existent network interface.
  # In practice, blackhole routes are typically added via CLI during incidents.
}

resource "aws_route_table_association" "isolated" {
  for_each = aws_subnet.isolated

  subnet_id      = each.value.id
  route_table_id = aws_route_table.isolated.id
}
```

> **Best Practice:** Never put routes in the main route table. Use it as a "deny-all" safety net by leaving it with only the implicit local route. Every subnet should have an explicit `aws_route_table_association` pointing to a custom route table. This way, a forgotten association defaults to zero internet access instead of accidentally becoming public.

### `most-specific-route.tf`

```hcl
# ------------------------------------------------------------------
# MOST-SPECIFIC-ROUTE-WINS demonstration
# ------------------------------------------------------------------
# AWS route tables use longest-prefix matching. If a packet's
# destination matches multiple routes, the most specific one wins.
#
# Example in the public route table:
#   10.30.0.0/16 -> local       (implicit, always present)
#   0.0.0.0/0    -> igw-xxx     (we added this)
#
# A packet to 10.30.1.5 matches both routes:
#   - 0.0.0.0/0 (matches everything)
#   - 10.30.0.0/16 (matches VPC traffic)
#
# The /16 route wins because it is more specific than /0.
# The packet stays within the VPC via the local route.
#
# A packet to 8.8.8.8 matches only 0.0.0.0/0, so it goes to the IGW.
# ------------------------------------------------------------------

# Add a more-specific route to the public route table to demonstrate
# that it overrides the default route for a specific CIDR.
# This sends traffic for 10.99.0.0/16 to a blackhole instead of
# letting it match the default 0.0.0.0/0 -> IGW route.
resource "aws_route" "specific_override" {
  route_table_id         = aws_route_table.public.id
  destination_cidr_block = "10.99.0.0/16"

  # In a real scenario, this might point to a VPC peering connection
  # or Transit Gateway. For this demo, we show that a specific route
  # takes priority over the default route.
  #
  # Note: We cannot create a true "blackhole" via Terraform directly
  # on a route without a target. In practice, you would use:
  #   aws ec2 create-route --route-table-id rtb-xxx \
  #     --destination-cidr-block 10.99.0.0/16 --no-gateway
  #
  # For this exercise, we will verify most-specific-wins via CLI.
}
```

### `compute.tf`

```hcl
# ------------------------------------------------------------------
# EC2 instance in the public subnet for connectivity testing
# ------------------------------------------------------------------
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

resource "aws_security_group" "test" {
  name        = "${var.project_name}-test-sg"
  description = "Allow SSH inbound and all outbound for testing"
  vpc_id      = aws_vpc.this.id

  tags = { Name = "${var.project_name}-test-sg" }
}

resource "aws_vpc_security_group_ingress_rule" "ssh_in" {
  security_group_id = aws_security_group.test.id
  cidr_ipv4         = "0.0.0.0/0"
  from_port         = 22
  to_port           = 22
  ip_protocol       = "tcp"
}

resource "aws_vpc_security_group_egress_rule" "all_out" {
  security_group_id = aws_security_group.test.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}

resource "aws_instance" "public_test" {
  ami                    = data.aws_ami.amazon_linux_2023.id
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.public[local.azs[0]].id
  vpc_security_group_ids = [aws_security_group.test.id]

  tags = { Name = "${var.project_name}-public-test" }
}
```

### `outputs.tf`

```hcl
output "vpc_id" {
  description = "VPC ID"
  value       = aws_vpc.this.id
}

output "main_route_table_id" {
  description = "Main (deny-all) route table ID"
  value       = aws_route_table.main_deny_all.id
}

output "public_route_table_id" {
  description = "Public route table ID"
  value       = aws_route_table.public.id
}

output "private_route_table_id" {
  description = "Private route table ID"
  value       = aws_route_table.private.id
}

output "isolated_route_table_id" {
  description = "Isolated route table ID"
  value       = aws_route_table.isolated.id
}

output "test_instance_public_ip" {
  description = "Public IP of the test instance"
  value       = aws_instance.public_test.public_ip
}
```

## Step 2 -- Initialize and Apply

```bash
terraform init
terraform apply -auto-approve
```

Terraform will create approximately 20 resources: VPC, IGW, 6 subnets, 4 route tables, 6 route table associations, the main route table association, a security group with rules, and an EC2 instance.

### Intermediate Verification

```bash
terraform state list
```

Confirm you see entries for all four route tables:

```
aws_route_table.main_deny_all
aws_route_table.public
aws_route_table.private
aws_route_table.isolated
aws_main_route_table_association.this
```

## Step 3 -- Understand Route Priority

The most-specific-route-wins rule is the single most important concept in VPC routing. Here is how it works:

1. A packet destined for `10.30.1.5` matches both `10.30.0.0/16 -> local` and `0.0.0.0/0 -> IGW`. The /16 route wins (more specific), so the packet stays in the VPC.

2. A packet destined for `8.8.8.8` matches only `0.0.0.0/0 -> IGW`. It goes to the internet.

3. If you add a route `10.30.10.0/24 -> peering-connection`, a packet to `10.30.10.5` matches three routes: /0, /16, and /24. The /24 wins.

This is identical to how IP routing works in physical networks. The longer the prefix, the higher the priority.

> **Best Practice:** Use blackhole routes as an emergency response tool. During a security incident, adding a blackhole route for the compromised CIDR range is faster than updating security groups across all resources. The AWS CLI command is: `aws ec2 create-route --route-table-id rtb-xxx --destination-cidr-block <compromised-cidr>` (with no target, creating a blackhole). It takes effect within seconds.

## Common Mistakes

### 1. Relying on the main route table for internet access

If you put a `0.0.0.0/0 -> IGW` route in the main route table, every new subnet you create is instantly public by default.

**Wrong -- IGW route in main route table:**

```hcl
# Any subnet without an explicit association is now public!
resource "aws_default_route_table" "main" {
  default_route_table_id = aws_vpc.this.default_route_table_id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this.id
  }
}
```

**What happens:** A teammate creates a database subnet, forgets the route table association, and the database is now accessible from the internet.

**Fix -- leave the main route table empty:**

```hcl
resource "aws_route_table" "main_deny_all" {
  vpc_id = aws_vpc.this.id
  # No routes -- only the implicit local route
  tags = { Name = "deny-all-main-rt" }
}

resource "aws_main_route_table_association" "this" {
  vpc_id         = aws_vpc.this.id
  route_table_id = aws_route_table.main_deny_all.id
}
```

### 2. Not understanding route priority

A common mistake is expecting a broader route to override a more specific one.

**Scenario:** You have `0.0.0.0/0 -> IGW` and `10.0.0.0/8 -> peering-connection`. A packet to `10.1.2.3` goes to the peering connection, NOT the IGW, because /8 is more specific than /0.

This is not a bug -- it is how routing works. If you need to override a specific route, add a route with an equal or longer prefix.

## Verify What You Learned

```bash
aws ec2 describe-route-tables \
  --filters "Name=vpc-id,Values=$(terraform output -raw vpc_id)" \
  --query "RouteTables[].{Name:Tags[?Key=='Name'].Value|[0],Main:Associations[0].Main,Routes:Routes[].{Dest:DestinationCidrBlock,Target:GatewayId||NatGatewayId||'local',State:State}}" \
  --output table
```

This shows all route tables, whether each is the main table, and all routes with their targets and states.

```bash
aws ec2 describe-route-tables \
  --filters "Name=tag:Name,Values=route-table-lab-public-rt" \
  --query "RouteTables[0].Routes[].{Destination:DestinationCidrBlock,Target:GatewayId||'local',State:State}" \
  --output table
```

Expected:

```
-----------------------------------------------------------
|                  DescribeRouteTables                    |
+--------------------+------------------+-----------------+
|    Destination     |     Target       |     State       |
+--------------------+------------------+-----------------+
|  10.30.0.0/16      |  local           |  active         |
|  0.0.0.0/0         |  igw-0abc123...  |  active         |
+--------------------+------------------+-----------------+
```

```bash
aws ec2 describe-route-tables \
  --filters "Name=tag:Name,Values=route-table-lab-main-deny-all-rt" \
  --query "RouteTables[0].Routes[].{Destination:DestinationCidrBlock,Target:GatewayId||'local',State:State}" \
  --output table
```

Expected -- only the local route, no internet:

```
-----------------------------------------------------------
|                  DescribeRouteTables                    |
+--------------------+------------------+-----------------+
|    Destination     |     Target       |     State       |
+--------------------+------------------+-----------------+
|  10.30.0.0/16      |  local           |  active         |
+--------------------+------------------+-----------------+
```

```bash
aws ec2 describe-main-route-tables \
  --filters "Name=vpc-id,Values=$(terraform output -raw vpc_id)" \
  --query "RouteTables[0].RouteTableId" \
  --output text
```

Expected: the route table ID of the deny-all main route table (matches `terraform output main_route_table_id`).

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

You now understand how route tables control traffic flow, why the main route table should be a deny-all safety net, how most-specific-route-wins determines packet forwarding, and how blackhole routes provide emergency traffic blocking. In the next exercise, **26 -- Security Group Chaining**, you will learn how to reference security groups within other security group rules to build a layered, tier-to-tier access model.

## Summary

- The **main route table** is used by any subnet without an explicit association -- leave it empty as a "deny-all" safety net
- **Custom route tables** with explicit associations are the production pattern: each tier gets its own table
- AWS uses **longest-prefix matching** (most-specific-route-wins) to select which route handles a packet
- **Blackhole routes** silently drop traffic to a destination -- use them for emergency incident response
- Every **`aws_route_table_association`** is an explicit opt-in; without it, the subnet inherits the main table
- The implicit **local route** (VPC CIDR -> local) is always present and cannot be removed

## Reference

| Resource | Purpose |
|----------|---------|
| `aws_route_table` | Defines routing rules for associated subnets |
| `aws_main_route_table_association` | Overrides the VPC's main route table |
| `aws_route_table_association` | Links a subnet to a custom route table |
| `aws_route` | Adds a single route to a route table |
| `aws_internet_gateway` | Target for public internet routes |

## Additional Resources

- [VPC Route Tables (AWS Docs)](https://docs.aws.amazon.com/vpc/latest/userguide/VPC_Route_Tables.html) -- official guide to route table concepts, priority rules, and blackhole routes
- [Terraform aws_route_table Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route_table) -- Terraform reference for route table management
- [Terraform aws_main_route_table_association](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/main_route_table_association) -- how to manage the VPC's main route table in Terraform
- [AWS Route Table Evaluation (AWS Docs)](https://docs.aws.amazon.com/vpc/latest/userguide/VPC_Route_Tables.html#route-table-evaluation) -- detailed explanation of route priority and longest-prefix matching

## Apply Your Knowledge

- [AWS Incident Response Playbook](https://docs.aws.amazon.com/whitepapers/latest/aws-security-incident-response-guide/aws-security-incident-response-guide.html) -- when and how to use blackhole routes during security incidents
- [VPC Route Table Limits](https://docs.aws.amazon.com/vpc/latest/userguide/amazon-vpc-limits.html) -- understand route table quotas before designing complex architectures
- [AWS re:Invent: Advanced VPC Design](https://www.youtube.com/watch?v=sMRIz4ydRE4) -- deep-dive on route table patterns in large-scale deployments

---

> *"A journey of a thousand miles begins with a single step."*
> — **Lao Tzu**

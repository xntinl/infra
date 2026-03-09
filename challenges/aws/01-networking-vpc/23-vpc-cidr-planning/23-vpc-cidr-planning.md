# 23. VPC CIDR Planning

<!--
difficulty: basic
concepts: [vpc-cidr, cidrsubnet, secondary-cidr, ipam]
tools: [terraform, aws-cli]
estimated_time: 30m
bloom_level: understand
prerequisites: [01-your-first-vpc]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates VPCs with no compute resources (~$0.00/hr for VPCs alone). The secondary CIDR association is free. Remember to run `terraform destroy` when finished to keep your account tidy.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed Exercise 01 (Your First VPC)

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** how CIDR notation maps to usable IP addresses in a VPC
- **Use** Terraform's `cidrsubnet()` function to carve a VPC into subnets of varying sizes
- **Add** a secondary CIDR block to an existing VPC for address space expansion
- **Identify** the 5 AWS-reserved IPs per subnet and account for them when sizing

## Why This Matters

CIDR planning is the first decision you make when building a cloud network, and it is the hardest to change later. A VPC's primary CIDR block cannot be modified after creation. If you choose a range that overlaps with another VPC, you cannot peer them. If you choose a range that is too small, you run out of IPs and face painful migrations. If you choose a range that is too large, you waste address space that could serve other environments.

Production teams typically allocate a /16 (65,536 addresses) for production VPCs and a /20 (4,096 addresses) for development or sandbox VPCs. They plan all VPC ranges on a spreadsheet before writing a single line of Terraform, treating the address plan like a building blueprint. The `cidrsubnet()` function turns that plan into repeatable, calculable infrastructure.

AWS reserves 5 IP addresses in every subnet: the network address, the VPC router, the DNS server, a future-use address, and the broadcast address. A /24 subnet (256 addresses) therefore provides only 251 usable IPs. A /28 subnet (16 addresses) provides only 11. Forgetting this reservation is one of the most common causes of capacity surprises.

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
  default     = "cidr-planning-lab"
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
  # Primary VPC: /16 gives 65,536 addresses
  primary_cidr = "10.10.0.0/16"

  # Secondary CIDR: /20 gives 4,096 addresses for future growth
  secondary_cidr = "100.64.0.0/20"

  azs = slice(data.aws_availability_zones.available.names, 0, 2)

  # ------------------------------------------------------------------
  # cidrsubnet(prefix, newbits, netnum) explained:
  #   prefix  = the base CIDR to subdivide
  #   newbits = how many additional bits to add to the prefix length
  #   netnum  = which subnet number to select (0-indexed)
  #
  # Example: cidrsubnet("10.10.0.0/16", 8, 0) = "10.10.0.0/24"
  #          cidrsubnet("10.10.0.0/16", 8, 1) = "10.10.1.0/24"
  #          cidrsubnet("10.10.0.0/16", 4, 0) = "10.10.0.0/20"
  # ------------------------------------------------------------------

  # /24 subnets for application tiers (251 usable IPs each)
  app_subnets = {
    for i, az in local.azs : az => cidrsubnet(local.primary_cidr, 8, i)
  }

  # /24 subnets for database tiers (251 usable IPs each)
  db_subnets = {
    for i, az in local.azs : az => cidrsubnet(local.primary_cidr, 8, i + 10)
  }

  # /28 subnets for load balancers (11 usable IPs each)
  lb_subnets = {
    for i, az in local.azs : az => cidrsubnet(local.primary_cidr, 12, i + 256)
  }

  # /24 subnets carved from the secondary CIDR for overflow workloads
  overflow_subnets = {
    for i, az in local.azs : az => cidrsubnet(local.secondary_cidr, 4, i)
  }
}
```

> **Best Practice:** Always plan your CIDR ranges on paper before writing Terraform. Assume every VPC you create will eventually need to peer with others. Overlapping CIDRs between any two VPCs make peering impossible and require a full migration to fix. Use a central spreadsheet or IPAM tool to track allocations across all accounts and regions.

### `vpc.tf`

```hcl
# ------------------------------------------------------------------
# Primary VPC with /16 CIDR
# ------------------------------------------------------------------
resource "aws_vpc" "this" {
  cidr_block           = local.primary_cidr
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = "${var.project_name}-vpc" }
}

# ------------------------------------------------------------------
# Secondary CIDR block -- extends the VPC address space
# Use case: you ran out of IPs in the primary range, or you need
# a non-RFC1918 range for overlapping-CIDR workarounds.
# ------------------------------------------------------------------
resource "aws_vpc_ipv4_cidr_block_association" "secondary" {
  vpc_id     = aws_vpc.this.id
  cidr_block = local.secondary_cidr

  # Note: 100.64.0.0/10 is the Carrier-Grade NAT (CGNAT) range.
  # AWS allows it as a secondary CIDR. It is useful when all
  # RFC1918 space is exhausted or for PrivateLink workarounds.
}

# ------------------------------------------------------------------
# Application-tier subnets (/24 -- 251 usable IPs each)
# ------------------------------------------------------------------
resource "aws_subnet" "app" {
  for_each = local.app_subnets

  vpc_id            = aws_vpc.this.id
  cidr_block        = each.value
  availability_zone = each.key

  tags = { Name = "${var.project_name}-app-${each.key}" }
}

# ------------------------------------------------------------------
# Database-tier subnets (/24 -- 251 usable IPs each)
# ------------------------------------------------------------------
resource "aws_subnet" "db" {
  for_each = local.db_subnets

  vpc_id            = aws_vpc.this.id
  cidr_block        = each.value
  availability_zone = each.key

  tags = { Name = "${var.project_name}-db-${each.key}" }
}

# ------------------------------------------------------------------
# Load-balancer subnets (/28 -- 11 usable IPs each)
# NLBs and ALBs need very few IPs; /28 is the minimum subnet size.
# ------------------------------------------------------------------
resource "aws_subnet" "lb" {
  for_each = local.lb_subnets

  vpc_id            = aws_vpc.this.id
  cidr_block        = each.value
  availability_zone = each.key

  tags = { Name = "${var.project_name}-lb-${each.key}" }
}

# ------------------------------------------------------------------
# Overflow subnets from the secondary CIDR (/24 each)
# These use the 100.64.x.x range for additional capacity.
# ------------------------------------------------------------------
resource "aws_subnet" "overflow" {
  for_each = local.overflow_subnets

  vpc_id            = aws_vpc.this.id
  cidr_block        = each.value
  availability_zone = each.key

  # This subnet must wait for the secondary CIDR association
  depends_on = [aws_vpc_ipv4_cidr_block_association.secondary]

  tags = { Name = "${var.project_name}-overflow-${each.key}" }
}
```

> **Best Practice:** Size subnets based on workload requirements, not one-size-fits-all. Use /24 (251 usable IPs) for application and database tiers. Use /28 (11 usable IPs) for NLB-only subnets. Use /20 or /19 for container-heavy workloads (EKS pods consume one IP per pod with the VPC CNI plugin).

### `outputs.tf`

```hcl
output "vpc_id" {
  description = "VPC ID"
  value       = aws_vpc.this.id
}

output "primary_cidr" {
  description = "Primary VPC CIDR block"
  value       = aws_vpc.this.cidr_block
}

output "secondary_cidr" {
  description = "Secondary CIDR block"
  value       = aws_vpc_ipv4_cidr_block_association.secondary.cidr_block
}

output "app_subnet_cidrs" {
  description = "Application subnet CIDRs by AZ"
  value       = { for k, v in aws_subnet.app : k => v.cidr_block }
}

output "db_subnet_cidrs" {
  description = "Database subnet CIDRs by AZ"
  value       = { for k, v in aws_subnet.db : k => v.cidr_block }
}

output "lb_subnet_cidrs" {
  description = "Load balancer subnet CIDRs by AZ"
  value       = { for k, v in aws_subnet.lb : k => v.cidr_block }
}

output "overflow_subnet_cidrs" {
  description = "Overflow subnet CIDRs from secondary CIDR by AZ"
  value       = { for k, v in aws_subnet.overflow : k => v.cidr_block }
}

# ------------------------------------------------------------------
# Demonstrate cidrsubnet() calculations in outputs
# ------------------------------------------------------------------
output "cidrsubnet_examples" {
  description = "Examples of cidrsubnet() calculations"
  value = {
    "slash_24_from_16_subnet_0"  = cidrsubnet("10.10.0.0/16", 8, 0)
    "slash_24_from_16_subnet_1"  = cidrsubnet("10.10.0.0/16", 8, 1)
    "slash_24_from_16_subnet_10" = cidrsubnet("10.10.0.0/16", 8, 10)
    "slash_20_from_16_subnet_0"  = cidrsubnet("10.10.0.0/16", 4, 0)
    "slash_28_from_16_subnet_0"  = cidrsubnet("10.10.0.0/16", 12, 0)
    "reserved_ips_per_subnet"    = "5 (network, router, DNS, future, broadcast)"
    "usable_in_slash_24"         = "251 (256 - 5)"
    "usable_in_slash_28"         = "11 (16 - 5)"
  }
}
```

## Step 2 -- Initialize and Apply

```bash
terraform init
terraform apply -auto-approve
```

Terraform will create 11 resources: the VPC, the secondary CIDR association, and 8 subnets across 4 tiers (2 AZs each).

### Intermediate Verification

Confirm the resource count:

```bash
terraform state list
```

You should see:

```
aws_subnet.app["us-east-1a"]
aws_subnet.app["us-east-1b"]
aws_subnet.db["us-east-1a"]
aws_subnet.db["us-east-1b"]
aws_subnet.lb["us-east-1a"]
aws_subnet.lb["us-east-1b"]
aws_subnet.overflow["us-east-1a"]
aws_subnet.overflow["us-east-1b"]
aws_vpc.this
aws_vpc_ipv4_cidr_block_association.secondary
data.aws_availability_zones.available
```

## Step 3 -- Explore the cidrsubnet() Outputs

```bash
terraform output cidrsubnet_examples
```

Expected output:

```
{
  "reserved_ips_per_subnet"    = "5 (network, router, DNS, future, broadcast)"
  "slash_20_from_16_subnet_0"  = "10.10.0.0/20"
  "slash_24_from_16_subnet_0"  = "10.10.0.0/24"
  "slash_24_from_16_subnet_1"  = "10.10.1.0/24"
  "slash_24_from_16_subnet_10" = "10.10.10.0/24"
  "slash_28_from_16_subnet_0"  = "10.10.0.0/28"
  "usable_in_slash_24"         = "251 (256 - 5)"
  "usable_in_slash_28"         = "11 (16 - 5)"
}
```

This shows how `cidrsubnet()` carves progressively smaller blocks from a parent CIDR. The `newbits` parameter controls how much smaller: adding 8 bits to a /16 gives a /24; adding 12 bits gives a /28.

## Common Mistakes

### 1. Overlapping CIDRs across VPCs

If you create two VPCs with overlapping CIDR blocks, you cannot peer them -- ever. The peering request will fail with an error.

**Wrong -- two VPCs with the same range:**

```hcl
resource "aws_vpc" "vpc_a" {
  cidr_block = "10.0.0.0/16"
}

resource "aws_vpc" "vpc_b" {
  cidr_block = "10.0.0.0/16"   # Overlaps with vpc_a!
}

resource "aws_vpc_peering_connection" "a_to_b" {
  vpc_id      = aws_vpc.vpc_a.id
  peer_vpc_id = aws_vpc.vpc_b.id
}
```

**What happens:** The peering connection fails:

```
Error: error creating VPC Peering Connection: InvalidParameterValue:
  The CIDRs of the two VPCs overlap
```

**Fix -- use non-overlapping ranges from the start:**

```hcl
resource "aws_vpc" "vpc_a" {
  cidr_block = "10.0.0.0/16"
}

resource "aws_vpc" "vpc_b" {
  cidr_block = "10.1.0.0/16"   # Non-overlapping
}
```

### 2. Forgetting the 5 reserved IPs per subnet

AWS reserves 5 IPs in every subnet. If you create a /28 subnet expecting 16 usable IPs, you actually get 11.

**Wrong -- expecting 16 IPs from a /28 for 14 instances:**

```hcl
resource "aws_subnet" "small" {
  vpc_id     = aws_vpc.this.id
  cidr_block = "10.0.0.0/28"   # 16 IPs total, but only 11 usable
}
```

**What happens:** Your 12th instance fails to launch:

```
Error: InsufficientFreeAddressesInSubnet: Not enough free addresses
  in subnet subnet-0abc123 to satisfy the request
```

**Fix -- account for the 5 reserved IPs when sizing:**

```hcl
# Need 14 IPs? Use /27 (32 total - 5 reserved = 27 usable)
resource "aws_subnet" "small" {
  vpc_id     = aws_vpc.this.id
  cidr_block = "10.0.0.0/27"
}
```

### 3. Using cidrsubnet() with overlapping netnum values

If you reuse the same `netnum` for different tiers, the subnets will overlap.

**Wrong -- both tiers use netnum 0:**

```hcl
locals {
  app_cidr = cidrsubnet("10.0.0.0/16", 8, 0)   # 10.0.0.0/24
  db_cidr  = cidrsubnet("10.0.0.0/16", 8, 0)    # 10.0.0.0/24 -- same!
}
```

**What happens:** Terraform fails when creating the second subnet:

```
Error: error creating subnet: InvalidSubnet.Conflict:
  The CIDR '10.0.0.0/24' conflicts with another subnet
```

**Fix -- use distinct netnum values per tier:**

```hcl
locals {
  app_cidr = cidrsubnet("10.0.0.0/16", 8, 0)    # 10.0.0.0/24
  db_cidr  = cidrsubnet("10.0.0.0/16", 8, 10)   # 10.0.10.0/24
}
```

## Verify What You Learned

```bash
aws ec2 describe-vpcs \
  --filters "Name=tag:Name,Values=cidr-planning-lab-vpc" \
  --query "Vpcs[0].CidrBlockAssociationSet[].CidrBlock" \
  --output table
```

Expected:

```
------------------------------
|       DescribeVpcs         |
+----------------------------+
|  10.10.0.0/16              |
|  100.64.0.0/20             |
+----------------------------+
```

This confirms both the primary and secondary CIDR blocks are attached.

```bash
aws ec2 describe-subnets \
  --filters "Name=vpc-id,Values=$(terraform output -raw vpc_id)" \
  --query "Subnets[].{AZ:AvailabilityZone,CIDR:CidrBlock,IPs:AvailableIpAddressCount}" \
  --output table
```

Expected (AZ names vary by region):

```
-----------------------------------------------------
|                  DescribeSubnets                   |
+---------------+------------------+-----------------+
|      AZ       |      CIDR        |      IPs        |
+---------------+------------------+-----------------+
|  us-east-1a   |  10.10.0.0/24    |  251            |
|  us-east-1b   |  10.10.1.0/24    |  251            |
|  us-east-1a   |  10.10.10.0/24   |  251            |
|  us-east-1b   |  10.10.11.0/24   |  251            |
|  us-east-1a   |  10.10.16.0/28   |  11             |
|  us-east-1b   |  10.10.16.16/28  |  11             |
|  us-east-1a   |  100.64.0.0/24   |  251            |
|  us-east-1b   |  100.64.1.0/24   |  251            |
+---------------+------------------+-----------------+
```

Notice the `IPs` column: /24 subnets show 251, /28 subnets show 11 -- confirming the 5 reserved IPs.

```bash
terraform output app_subnet_cidrs
```

Expected:

```
{
  "us-east-1a" = "10.10.0.0/24"
  "us-east-1b" = "10.10.1.0/24"
}
```

```bash
terraform output lb_subnet_cidrs
```

Expected:

```
{
  "us-east-1a" = "10.10.16.0/28"
  "us-east-1b" = "10.10.16.16/28"
}
```

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

Destroy all resources to keep your account tidy:

```bash
terraform destroy -auto-approve
```

Verify nothing remains:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

You now understand how to plan VPC CIDR blocks, use `cidrsubnet()` to carve subnets of varying sizes, and extend a VPC with secondary CIDR blocks. In the next exercise, **24 -- Subnet Design Patterns**, you will apply these skills to build a 4-tier subnet architecture (public, private-app, private-db, and management) with proper AZ placement and sizing for real-world workloads.

## Summary

- A **VPC CIDR block** defines the total IP address space; it cannot be changed after creation
- **`cidrsubnet(prefix, newbits, netnum)`** deterministically carves subnets from a parent CIDR
- **Secondary CIDR blocks** extend a VPC's address space without replacing the primary range
- AWS **reserves 5 IPs per subnet** (network, router, DNS, future-use, broadcast) -- always account for this when sizing
- Plan all VPC CIDRs **before building** -- overlapping ranges permanently prevent VPC peering
- Use **/16 for production**, **/20 for development**, and **/28 as the minimum** subnet size

## Reference

| Resource | Purpose |
|----------|---------|
| `aws_vpc` | The virtual network with primary CIDR |
| `aws_vpc_ipv4_cidr_block_association` | Adds a secondary CIDR to an existing VPC |
| `aws_subnet` | A segment within the VPC tied to one AZ |
| `cidrsubnet()` | Terraform function to calculate subnet CIDRs |
| `cidrhost()` | Terraform function to calculate a specific host IP |

## Additional Resources

- [VPC CIDR Blocks (AWS Docs)](https://docs.aws.amazon.com/vpc/latest/userguide/vpc-cidr-blocks.html) -- official guide to primary and secondary CIDR allocation rules
- [Terraform cidrsubnet Function](https://developer.hashicorp.com/terraform/language/functions/cidrsubnet) -- reference and examples for the cidrsubnet() function
- [Understanding CIDR Notation (AWS)](https://aws.amazon.com/what-is/cidr/) -- primer on CIDR notation, subnet masks, and address planning
- [AWS IPAM Overview](https://docs.aws.amazon.com/vpc/latest/ipam/what-it-is-ipam.html) -- centralized IP address management for multi-account environments
- [VPC Subnet Sizing (AWS Docs)](https://docs.aws.amazon.com/vpc/latest/userguide/subnet-sizing.html) -- reserved addresses and minimum subnet sizes

## Apply Your Knowledge

- [AWS Well-Architected Framework -- Networking Pillar](https://docs.aws.amazon.com/wellarchitected/latest/reliability-pillar/plan-your-network-topology.html) -- design principles for production network planning
- [CIDR Subnet Calculator](https://www.subnet-calc.com/) -- interactive tool for planning CIDR allocations across multiple VPCs
- [AWS re:Invent: VPC Design and New Capabilities](https://www.youtube.com/watch?v=HBS0kLEfUJM) -- deep-dive session on VPC CIDR planning at scale

---

> *"Plans are worthless, but planning is everything."*
> — **Dwight D. Eisenhower**

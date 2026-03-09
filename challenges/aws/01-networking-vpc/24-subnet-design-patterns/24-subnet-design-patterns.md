# 24. Subnet Design Patterns

<!--
difficulty: basic
concepts: [subnets, four-tier-design, reserved-ips, az-placement, cidrsubnet, map-public-ip]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: apply
prerequisites: [01-your-first-vpc, 23-vpc-cidr-planning]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a VPC with subnets and an Internet Gateway (~$0.00/hr). No compute resources are launched. Remember to run `terraform destroy` when finished to keep your account tidy.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed Exercise 01 (Your First VPC)
- Completed Exercise 23 (VPC CIDR Planning)

## Learning Objectives

After completing this exercise, you will be able to:

- **Build** a 4-tier VPC architecture (public, private-app, private-db, management) across multiple AZs
- **Apply** proper subnet sizing based on workload requirements
- **Configure** `map_public_ip_on_launch` only on public subnets
- **Use** `for_each` with `cidrsubnet()` to create repeatable subnet patterns
- **Explain** why every tier needs subnets in at least 2 AZs

## Why This Matters

Most production architectures follow a tiered subnet model. Public subnets hold load balancers and bastion hosts -- resources that need direct internet access. Private application subnets hold your containers, Lambda functions, and application servers -- they reach the internet through a NAT Gateway but are not directly reachable. Private database subnets hold RDS instances and ElastiCache clusters -- they have no internet route at all, only VPC-internal connectivity. A management tier holds monitoring agents, CI/CD runners, and jump boxes that administrators use to access the other tiers.

This separation gives you defense in depth. Even if an attacker compromises a public-facing load balancer, they cannot directly reach the database subnet because there is no route between them without traversing security groups. Each tier has its own route table, its own security group rules, and its own access patterns.

Sizing subnets correctly matters. Over-allocating wastes address space you may need later for VPC peering or container workloads. Under-allocating causes capacity failures during scale-up events. The key is matching subnet size to the expected workload: /24 for application tiers, /26 for databases (rarely more than 60 instances), and /28 for NLB-only subnets.

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
  default     = "subnet-patterns-lab"
}

variable "az_count" {
  description = "Number of Availability Zones to use (minimum 2)"
  type        = number
  default     = 3

  validation {
    condition     = var.az_count >= 2
    error_message = "Must use at least 2 AZs for high availability."
  }
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
  vpc_cidr = "10.20.0.0/16"
  azs      = slice(data.aws_availability_zones.available.names, 0, var.az_count)

  # ------------------------------------------------------------------
  # 4-tier subnet layout using cidrsubnet():
  #
  # Tier         | Size | Usable IPs | Use Case
  # -------------|------|------------|----------------------------------
  # Public       | /24  | 251        | ALBs, NLBs, NAT Gateways, bastions
  # Private-App  | /22  | 1019       | ECS tasks, EKS pods, EC2 instances
  # Private-DB   | /26  | 59         | RDS, ElastiCache, OpenSearch
  # Management   | /27  | 27         | Monitoring, CI/CD, jump boxes
  #
  # We use different newbits values to create different subnet sizes:
  #   /24 = newbits 8 from /16
  #   /22 = newbits 6 from /16
  #   /26 = newbits 10 from /16
  #   /27 = newbits 11 from /16
  # ------------------------------------------------------------------

  public_subnets = {
    for i, az in local.azs : az => cidrsubnet(local.vpc_cidr, 8, i)
    # AZ-a: 10.20.0.0/24, AZ-b: 10.20.1.0/24, AZ-c: 10.20.2.0/24
  }

  app_subnets = {
    for i, az in local.azs : az => cidrsubnet(local.vpc_cidr, 6, i + 4)
    # AZ-a: 10.20.16.0/22, AZ-b: 10.20.20.0/22, AZ-c: 10.20.24.0/22
  }

  db_subnets = {
    for i, az in local.azs : az => cidrsubnet(local.vpc_cidr, 10, i + 512)
    # AZ-a: 10.20.128.0/26, AZ-b: 10.20.128.64/26, AZ-c: 10.20.128.128/26
  }

  mgmt_subnets = {
    for i, az in local.azs : az => cidrsubnet(local.vpc_cidr, 11, i + 1536)
    # AZ-a: 10.20.192.0/27, AZ-b: 10.20.192.32/27, AZ-c: 10.20.192.64/27
  }
}
```

> **Best Practice:** Always deploy subnets in at least 2 Availability Zones, even for development environments. AWS services like RDS Multi-AZ, ALB, and EKS require subnets in multiple AZs. Building single-AZ now means rearchitecting later. The marginal cost of an extra empty subnet is zero.

### `vpc.tf`

```hcl
# ------------------------------------------------------------------
# VPC -- the foundation for all 4 tiers
# ------------------------------------------------------------------
resource "aws_vpc" "this" {
  cidr_block           = local.vpc_cidr
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = "${var.project_name}-vpc" }
}

# ------------------------------------------------------------------
# Internet Gateway -- required for the public tier
# ------------------------------------------------------------------
resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id

  tags = { Name = "${var.project_name}-igw" }
}

# ------------------------------------------------------------------
# Tier 1: PUBLIC subnets
# These receive a public IP automatically (map_public_ip_on_launch).
# Used for ALBs, NLBs, NAT Gateways, and bastion hosts.
# ------------------------------------------------------------------
resource "aws_subnet" "public" {
  for_each = local.public_subnets

  vpc_id                  = aws_vpc.this.id
  cidr_block              = each.value
  availability_zone       = each.key
  map_public_ip_on_launch = true

  tags = { Name = "${var.project_name}-public-${each.key}" }
}

# ------------------------------------------------------------------
# Tier 2: PRIVATE APPLICATION subnets
# No public IPs. Application servers, ECS tasks, EKS pods.
# Outbound internet via NAT Gateway (not created in this exercise).
# ------------------------------------------------------------------
resource "aws_subnet" "app" {
  for_each = local.app_subnets

  vpc_id            = aws_vpc.this.id
  cidr_block        = each.value
  availability_zone = each.key

  tags = { Name = "${var.project_name}-app-${each.key}" }
}

# ------------------------------------------------------------------
# Tier 3: PRIVATE DATABASE subnets
# No public IPs, no internet route. Only VPC-internal traffic.
# RDS, ElastiCache, OpenSearch.
# ------------------------------------------------------------------
resource "aws_subnet" "db" {
  for_each = local.db_subnets

  vpc_id            = aws_vpc.this.id
  cidr_block        = each.value
  availability_zone = each.key

  tags = { Name = "${var.project_name}-db-${each.key}" }
}

# ------------------------------------------------------------------
# Tier 4: MANAGEMENT subnets
# Small subnets for monitoring, CI/CD runners, jump boxes.
# ------------------------------------------------------------------
resource "aws_subnet" "mgmt" {
  for_each = local.mgmt_subnets

  vpc_id            = aws_vpc.this.id
  cidr_block        = each.value
  availability_zone = each.key

  tags = { Name = "${var.project_name}-mgmt-${each.key}" }
}
```

### `route-tables.tf`

```hcl
# ------------------------------------------------------------------
# Public route table -- sends 0.0.0.0/0 to the IGW
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
# Private route table -- no internet route (NAT not in this exercise)
# The app and mgmt tiers share this for now.
# ------------------------------------------------------------------
resource "aws_route_table" "private" {
  vpc_id = aws_vpc.this.id

  # No default route -- private subnets cannot reach the internet.
  # In production, you would add 0.0.0.0/0 -> NAT Gateway here.

  tags = { Name = "${var.project_name}-private-rt" }
}

resource "aws_route_table_association" "app" {
  for_each = aws_subnet.app

  subnet_id      = each.value.id
  route_table_id = aws_route_table.private.id
}

resource "aws_route_table_association" "mgmt" {
  for_each = aws_subnet.mgmt

  subnet_id      = each.value.id
  route_table_id = aws_route_table.private.id
}

# ------------------------------------------------------------------
# Data route table -- fully isolated, no internet route at all.
# Not even NAT. Databases should never initiate external connections.
# ------------------------------------------------------------------
resource "aws_route_table" "data" {
  vpc_id = aws_vpc.this.id

  tags = { Name = "${var.project_name}-data-rt" }
}

resource "aws_route_table_association" "db" {
  for_each = aws_subnet.db

  subnet_id      = each.value.id
  route_table_id = aws_route_table.data.id
}
```

> **Best Practice:** Give the database tier its own route table with no internet route -- not even through a NAT Gateway. Databases should never initiate outbound connections. If RDS needs to download patches, use VPC endpoints for S3. This zero-egress pattern limits blast radius if database credentials are compromised.

### `outputs.tf`

```hcl
output "vpc_id" {
  description = "VPC ID"
  value       = aws_vpc.this.id
}

output "public_subnet_ids" {
  description = "Public subnet IDs"
  value       = [for s in aws_subnet.public : s.id]
}

output "app_subnet_ids" {
  description = "Application subnet IDs"
  value       = [for s in aws_subnet.app : s.id]
}

output "db_subnet_ids" {
  description = "Database subnet IDs"
  value       = [for s in aws_subnet.db : s.id]
}

output "mgmt_subnet_ids" {
  description = "Management subnet IDs"
  value       = [for s in aws_subnet.mgmt : s.id]
}

output "subnet_layout" {
  description = "Complete subnet layout with tier, AZ, and CIDR"
  value = merge(
    { for k, v in aws_subnet.public : "public-${k}" => v.cidr_block },
    { for k, v in aws_subnet.app : "app-${k}" => v.cidr_block },
    { for k, v in aws_subnet.db : "db-${k}" => v.cidr_block },
    { for k, v in aws_subnet.mgmt : "mgmt-${k}" => v.cidr_block },
  )
}
```

## Step 2 -- Initialize and Apply

```bash
terraform init
terraform apply -auto-approve
```

Terraform will create 18 resources: the VPC, IGW, 12 subnets (4 tiers x 3 AZs), 3 route tables, and 12 route table associations.

### Intermediate Verification

```bash
terraform state list | wc -l
```

Expected: a number around `18` (exact count depends on AZ count).

## Common Mistakes

### 1. Making all subnets the same size

New users often give every subnet a /24 regardless of workload. This wastes address space and can prevent future expansion.

**Wrong -- uniform sizing:**

```hcl
locals {
  public_cidr = cidrsubnet("10.0.0.0/16", 8, 0)   # /24 = 251 IPs
  app_cidr    = cidrsubnet("10.0.0.0/16", 8, 1)    # /24 = 251 IPs
  db_cidr     = cidrsubnet("10.0.0.0/16", 8, 2)    # /24 = 251 IPs
  mgmt_cidr   = cidrsubnet("10.0.0.0/16", 8, 3)    # /24 = 251 IPs
}
```

**Problem:** The app tier (running hundreds of EKS pods) is constrained to 251 IPs, while the DB tier (with 3 RDS instances) wastes 248 unused addresses.

**Fix -- size by workload:**

```hcl
locals {
  public_cidr = cidrsubnet("10.0.0.0/16", 8, 0)    # /24 = 251 IPs (ALBs)
  app_cidr    = cidrsubnet("10.0.0.0/16", 6, 4)     # /22 = 1019 IPs (pods)
  db_cidr     = cidrsubnet("10.0.0.0/16", 10, 512)  # /26 = 59 IPs (RDS)
  mgmt_cidr   = cidrsubnet("10.0.0.0/16", 11, 1536) # /27 = 27 IPs (agents)
}
```

### 2. Forgetting map_public_ip_on_launch on public subnets

Without `map_public_ip_on_launch = true`, instances in "public" subnets do not receive a public IP and cannot be reached from the internet even though the route table points to the IGW.

**Wrong -- public subnet without auto-assign:**

```hcl
resource "aws_subnet" "public" {
  vpc_id     = aws_vpc.this.id
  cidr_block = "10.0.0.0/24"
  # map_public_ip_on_launch is false by default!
}
```

**What happens:** EC2 instances launch with only a private IP. SSH times out, ALB health checks fail.

**Fix -- enable auto-assign for public subnets:**

```hcl
resource "aws_subnet" "public" {
  vpc_id                  = aws_vpc.this.id
  cidr_block              = "10.0.0.0/24"
  map_public_ip_on_launch = true
}
```

### 3. Placing all subnets in a single AZ

Single-AZ deployments are a ticking time bomb. When that AZ experiences an outage, your entire application goes offline.

**Wrong -- hardcoded single AZ:**

```hcl
resource "aws_subnet" "app" {
  vpc_id            = aws_vpc.this.id
  cidr_block        = "10.0.1.0/24"
  availability_zone = "us-east-1a"   # Single point of failure
}
```

**Fix -- use `for_each` across multiple AZs:**

```hcl
resource "aws_subnet" "app" {
  for_each = local.app_subnets

  vpc_id            = aws_vpc.this.id
  cidr_block        = each.value
  availability_zone = each.key   # Spreads across AZs
}
```

## Verify What You Learned

```bash
aws ec2 describe-subnets \
  --filters "Name=vpc-id,Values=$(terraform output -raw vpc_id)" \
  --query "Subnets[].{AZ:AvailabilityZone,CIDR:CidrBlock,Public:MapPublicIpOnLaunch,IPs:AvailableIpAddressCount}" \
  --output table
```

Expected (3-AZ deployment):

```
---------------------------------------------------------------------
|                        DescribeSubnets                            |
+---------------+-------------------+---------+---------------------+
|      AZ       |       CIDR        |   IPs   |      Public         |
+---------------+-------------------+---------+---------------------+
|  us-east-1a   |  10.20.0.0/24     |  251    |  True               |
|  us-east-1b   |  10.20.1.0/24     |  251    |  True               |
|  us-east-1c   |  10.20.2.0/24     |  251    |  True               |
|  us-east-1a   |  10.20.16.0/22    |  1019   |  False              |
|  us-east-1b   |  10.20.20.0/22    |  1019   |  False              |
|  us-east-1c   |  10.20.24.0/22    |  1019   |  False              |
|  us-east-1a   |  10.20.128.0/26   |  59     |  False              |
|  us-east-1b   |  10.20.128.64/26  |  59     |  False              |
|  us-east-1c   |  10.20.128.128/26 |  59     |  False              |
|  us-east-1a   |  10.20.192.0/27   |  27     |  False              |
|  us-east-1b   |  10.20.192.32/27  |  27     |  False              |
|  us-east-1c   |  10.20.192.64/27  |  27     |  False              |
+---------------+-------------------+---------+---------------------+
```

Notice the different sizes per tier: /24 (public), /22 (app), /26 (db), /27 (mgmt).

```bash
aws ec2 describe-route-tables \
  --filters "Name=vpc-id,Values=$(terraform output -raw vpc_id)" "Name=tag:Name,Values=*-rt" \
  --query "RouteTables[].{Name:Tags[?Key=='Name'].Value|[0],Routes:Routes[].{Dest:DestinationCidrBlock,Target:GatewayId||NatGatewayId||'local'}}" \
  --output table
```

This confirms each route table has the correct routes: public has IGW, private has local-only, data has local-only.

```bash
terraform output subnet_layout
```

Expected: a map showing all 12 subnets with their tier, AZ, and CIDR block.

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

You built a 4-tier VPC with properly sized subnets spread across multiple AZs. In the next exercise, **25 -- Route Table Deep Dive**, you will explore main vs custom route tables, explicit associations, route priority (most-specific-route-wins), and blackhole routes for incident response.

## Summary

- A **4-tier subnet design** (public, app, db, mgmt) provides defense in depth by isolating workloads with different access patterns
- **Size subnets by workload**: /22 for container-heavy app tiers, /24 for public ALB subnets, /26 for databases, /27 for management
- **`map_public_ip_on_launch = true`** is required only on public subnets -- never set it on private tiers
- **Always use 2+ AZs** even in development -- many AWS services (ALB, RDS Multi-AZ, EKS) require multi-AZ subnets
- **`for_each` with `cidrsubnet()`** creates repeatable, calculable subnet patterns that scale to any number of AZs
- **Separate route tables per tier** enforce traffic isolation: public has IGW, private has NAT, data has no internet route

## Reference

| Resource | Purpose |
|----------|---------|
| `aws_vpc` | The virtual network |
| `aws_subnet` | A segment within the VPC tied to one AZ |
| `aws_internet_gateway` | Public internet access for the public tier |
| `aws_route_table` | Routing rules per tier |
| `aws_route_table_association` | Links a subnet to a route table |
| `cidrsubnet()` | Terraform function to calculate subnet CIDRs |

## Additional Resources

- [VPC Subnet Basics (AWS Docs)](https://docs.aws.amazon.com/vpc/latest/userguide/configure-subnets.html) -- official guide to subnet creation and configuration
- [Terraform cidrsubnet Function](https://developer.hashicorp.com/terraform/language/functions/cidrsubnet) -- reference and examples for subnet calculation
- [AWS Multi-AZ Architectures](https://docs.aws.amazon.com/whitepapers/latest/real-time-communication-on-aws/high-availability-and-scalability-on-aws.html) -- why multi-AZ is essential for production
- [Subnet Sizing for EKS](https://docs.aws.amazon.com/eks/latest/userguide/network-reqs.html) -- how EKS pod networking consumes subnet IPs
- [VPC Design Best Practices (AWS Blog)](https://aws.amazon.com/blogs/networking-and-content-delivery/vpc-sharing-a-new-approach-to-multiple-accounts-and-vpc-management/) -- patterns for subnet design across accounts

## Apply Your Knowledge

- [AWS EKS VPC and Subnet Requirements](https://docs.aws.amazon.com/eks/latest/userguide/network-reqs.html) -- real-world subnet sizing for Kubernetes workloads
- [RDS Subnet Group Requirements](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/USER_VPC.WorkingWithRDSInstanceinaVPC.html) -- why databases need subnets in multiple AZs
- [AWS Well-Architected Labs -- Networking](https://www.wellarchitectedlabs.com/reliability/) -- hands-on labs for production subnet design

---

> *"A great building must begin with the unmeasurable, must go through measurable means when it is being designed, and in the end must be unmeasurable."*
> — **Louis Kahn**

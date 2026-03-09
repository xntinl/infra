# 28. Multi-VPC Isolated Routing

<!--
difficulty: intermediate
concepts: [multi-vpc, route-table-isolation, non-overlapping-cidr, shared-services-vpc, vpc-design]
tools: [terraform, aws-cli]
estimated_time: 50m
bloom_level: design
prerequisites: [23-vpc-cidr-planning, 25-route-table-deep-dive]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates 3 VPCs with subnets and route tables (~$0.00/hr for VPCs). No NAT Gateways or compute resources are launched. Remember to run `terraform destroy` when finished to keep your account tidy.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.5 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Exercise 23 completed | Understand CIDR planning and non-overlapping ranges |
| Exercise 25 completed | Understand route tables and the deny-all pattern |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Design** a multi-VPC architecture with non-overlapping CIDRs prepared for future peering
2. **Implement** isolated route tables that prevent cross-VPC traffic without explicit connectivity
3. **Create** a shared-services VPC pattern with proper subnet tiers
4. **Validate** that VPCs are truly isolated using AWS CLI route table queries

## Why This Matters

Most organizations outgrow a single VPC quickly. Development, staging, and production environments need network isolation to prevent accidental cross-environment access. A shared-services VPC hosts common infrastructure -- DNS resolvers, CI/CD runners, monitoring agents -- that all environments need to reach.

The critical design decision is CIDR planning. Every VPC must use a non-overlapping CIDR range from day one. If dev uses `10.0.0.0/16` and staging uses `10.0.0.0/16`, you can never connect them via peering or Transit Gateway. The remediation is a painful migration to new CIDR ranges. Teams that plan their address space upfront avoid this entirely.

Each VPC is isolated by default. There is no implicit connectivity between VPCs -- traffic between them must traverse an explicit connection (peering, Transit Gateway, or PrivateLink). This exercise builds the foundation: three properly planned VPCs with isolated routing that you can later connect using the method that best fits your needs.

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
  default     = "multi-vpc-lab"
}
```

### `locals.tf`

```hcl
data "aws_availability_zones" "available" {
  state = "available"
}

locals {
  azs = slice(data.aws_availability_zones.available.names, 0, 2)

  # ------------------------------------------------------------------
  # Non-overlapping CIDR plan for 3 VPCs:
  #
  # VPC              | CIDR          | Role
  # -----------------|---------------|-----------------------------
  # shared-services  | 10.0.0.0/16   | DNS, monitoring, CI/CD
  # dev              | 10.1.0.0/16   | Development workloads
  # staging          | 10.2.0.0/16   | Staging workloads
  #
  # Each VPC gets 65,536 addresses. The ranges are contiguous but
  # non-overlapping, leaving 10.3.0.0/16+ for future VPCs (prod, etc).
  # ------------------------------------------------------------------
  vpcs = {
    shared-services = {
      cidr        = "10.0.0.0/16"
      description = "Shared services - DNS, monitoring, CI/CD"
      has_public  = true
    }
    dev = {
      cidr        = "10.1.0.0/16"
      description = "Development environment"
      has_public  = true
    }
    staging = {
      cidr        = "10.2.0.0/16"
      description = "Staging environment"
      has_public  = false
    }
  }
}
```

### `vpcs.tf`

```hcl
# =======================================================
# TODO 1 — Create 3 VPCs using for_each
# =======================================================
# Requirements:
#   - Use for_each over local.vpcs
#   - Each VPC uses the CIDR from local.vpcs[each.key].cidr
#   - Enable DNS support and DNS hostnames on all VPCs
#   - Tag with Name = "${var.project_name}-${each.key}-vpc"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc
# Hint: for_each = local.vpcs; cidr_block = each.value.cidr


# =======================================================
# TODO 2 — Create public and private subnets for each VPC
# =======================================================
# Requirements:
#   - For each VPC, create 2 private subnets (one per AZ)
#     using cidrsubnet(each.value.cidr, 8, i + 10)
#   - For VPCs with has_public = true, create 2 public subnets
#     using cidrsubnet(each.value.cidr, 8, i)
#   - Public subnets need map_public_ip_on_launch = true
#   - Tag private: "${var.project_name}-${vpc_name}-private-${az}"
#   - Tag public: "${var.project_name}-${vpc_name}-public-${az}"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/subnet
# Hint: Create a flattened local map for subnet iteration, e.g.:
#   locals {
#     private_subnets = merge([for vpc_name, vpc in local.vpcs : {
#       for i, az in local.azs : "${vpc_name}-${az}" => {
#         vpc_name = vpc_name, cidr = cidrsubnet(vpc.cidr, 8, i + 10), az = az
#       }
#     }]...)
#   }
```

> **Best Practice:** Assign non-overlapping CIDRs to every VPC from day one, even if you have no immediate plans to connect them. Overlapping CIDRs permanently prevent VPC peering and Transit Gateway attachment. Use a central IPAM tool or spreadsheet to track allocations across all accounts and regions. Reserve address space for future environments (prod, DR) in the same plan.

### `route-tables.tf`

```hcl
# =======================================================
# TODO 3 — Create Internet Gateways for VPCs with public subnets
# =======================================================
# Requirements:
#   - Create an IGW for each VPC where has_public = true
#   - Use for_each with a filtered map
#   - Tag with Name = "${var.project_name}-${each.key}-igw"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/internet_gateway
# Hint: { for k, v in local.vpcs : k => v if v.has_public }


# =======================================================
# TODO 4 — Create route tables with deny-all main + custom routes
# =======================================================
# Requirements:
#   - For each VPC, override the main route table to deny-all
#     (use aws_route_table + aws_main_route_table_association)
#   - For VPCs with has_public = true:
#     Create a public route table with 0.0.0.0/0 -> IGW
#     Associate public subnets with the public route table
#   - For all VPCs:
#     Create a private route table with no internet route
#     Associate private subnets with the private route table
#   - Tag all route tables with Name
#
# Docs:
#   https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route_table
#   https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/main_route_table_association
# Hint: Use for_each over local.vpcs for the deny-all main RT;
#       use the filtered map for public RTs
```

### `default-sg-lockdown.tf`

```hcl
# =======================================================
# TODO 5 — Lock down the default SG in every VPC
# =======================================================
# Requirements:
#   - Use aws_default_security_group for each VPC
#   - Remove all ingress and egress rules
#   - Tag with Name = "${var.project_name}-${each.key}-default-LOCKED"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/default_security_group
# Hint: for_each = aws_vpc.this; vpc_id = each.value.id
```

### `outputs.tf`

```hcl
# =======================================================
# TODO 6 — Output VPC details for verification
# =======================================================
# Requirements:
#   - Output a map of VPC IDs: { vpc_name => vpc_id }
#   - Output the CIDR blocks: { vpc_name => cidr_block }
#   - Output all private subnet IDs grouped by VPC
#   - Output a "peering_readiness" message confirming no overlaps
#
# Hint: { for k, v in aws_vpc.this : k => v.id }
```

## Spot the Bug

A colleague is preparing to peer the dev and staging VPCs but assigned them the same CIDR. **What fails and why?**

```hcl
locals {
  vpcs = {
    dev = {
      cidr = "10.0.0.0/16"
    }
    staging = {
      cidr = "10.0.0.0/16"   # <-- BUG: same CIDR as dev
    }
  }
}

resource "aws_vpc_peering_connection" "dev_to_staging" {
  vpc_id      = aws_vpc.this["dev"].id
  peer_vpc_id = aws_vpc.this["staging"].id
  auto_accept = true
}
```

<details>
<summary>Explain the bug</summary>

Both VPCs use `10.0.0.0/16`, which means their address spaces completely overlap. The peering request will fail with:

```
Error: error creating VPC Peering Connection: InvalidParameterValue:
  The CIDRs of the two VPCs overlap
```

Even partial overlaps block peering. If dev is `10.0.0.0/16` and staging is `10.0.128.0/17`, the peering still fails because `10.0.128.0/17` is a subset of `10.0.0.0/16`.

The fix is to use non-overlapping ranges planned before VPC creation:

```hcl
locals {
  vpcs = {
    dev = {
      cidr = "10.1.0.0/16"
    }
    staging = {
      cidr = "10.2.0.0/16"   # Non-overlapping
    }
  }
}
```

This is why CIDR planning matters. Once a VPC is deployed with workloads running inside it, changing its CIDR requires creating a new VPC and migrating everything.

</details>

## Solutions

<details>
<summary>TODO 1 -- Create 3 VPCs</summary>

```hcl
resource "aws_vpc" "this" {
  for_each = local.vpcs

  cidr_block           = each.value.cidr
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = "${var.project_name}-${each.key}-vpc" }
}
```

</details>

<details>
<summary>TODO 2 -- Create subnets for each VPC</summary>

```hcl
locals {
  private_subnets = merge([
    for vpc_name, vpc in local.vpcs : {
      for i, az in local.azs : "${vpc_name}-${az}" => {
        vpc_name = vpc_name
        cidr     = cidrsubnet(vpc.cidr, 8, i + 10)
        az       = az
      }
    }
  ]...)

  public_subnet_vpcs = { for k, v in local.vpcs : k => v if v.has_public }

  public_subnets = merge([
    for vpc_name, vpc in local.public_subnet_vpcs : {
      for i, az in local.azs : "${vpc_name}-${az}" => {
        vpc_name = vpc_name
        cidr     = cidrsubnet(vpc.cidr, 8, i)
        az       = az
      }
    }
  ]...)
}

resource "aws_subnet" "private" {
  for_each = local.private_subnets

  vpc_id            = aws_vpc.this[each.value.vpc_name].id
  cidr_block        = each.value.cidr
  availability_zone = each.value.az

  tags = { Name = "${var.project_name}-${each.value.vpc_name}-private-${each.value.az}" }
}

resource "aws_subnet" "public" {
  for_each = local.public_subnets

  vpc_id                  = aws_vpc.this[each.value.vpc_name].id
  cidr_block              = each.value.cidr
  availability_zone       = each.value.az
  map_public_ip_on_launch = true

  tags = { Name = "${var.project_name}-${each.value.vpc_name}-public-${each.value.az}" }
}
```

</details>

<details>
<summary>TODO 3 -- Internet Gateways for public VPCs</summary>

```hcl
resource "aws_internet_gateway" "this" {
  for_each = { for k, v in local.vpcs : k => v if v.has_public }

  vpc_id = aws_vpc.this[each.key].id

  tags = { Name = "${var.project_name}-${each.key}-igw" }
}
```

</details>

<details>
<summary>TODO 4 -- Route tables with deny-all main + custom routes</summary>

```hcl
# ------------------------------------------------------------------
# Deny-all main route table for every VPC
# ------------------------------------------------------------------
resource "aws_route_table" "main_deny_all" {
  for_each = local.vpcs

  vpc_id = aws_vpc.this[each.key].id

  tags = { Name = "${var.project_name}-${each.key}-main-deny-all-rt" }
}

resource "aws_main_route_table_association" "this" {
  for_each = local.vpcs

  vpc_id         = aws_vpc.this[each.key].id
  route_table_id = aws_route_table.main_deny_all[each.key].id
}

# ------------------------------------------------------------------
# Public route tables (only for VPCs with has_public = true)
# ------------------------------------------------------------------
resource "aws_route_table" "public" {
  for_each = { for k, v in local.vpcs : k => v if v.has_public }

  vpc_id = aws_vpc.this[each.key].id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this[each.key].id
  }

  tags = { Name = "${var.project_name}-${each.key}-public-rt" }
}

resource "aws_route_table_association" "public" {
  for_each = local.public_subnets

  subnet_id      = aws_subnet.public[each.key].id
  route_table_id = aws_route_table.public[each.value.vpc_name].id
}

# ------------------------------------------------------------------
# Private route tables (one per VPC)
# ------------------------------------------------------------------
resource "aws_route_table" "private" {
  for_each = local.vpcs

  vpc_id = aws_vpc.this[each.key].id

  # No default route -- fully isolated from internet
  tags = { Name = "${var.project_name}-${each.key}-private-rt" }
}

resource "aws_route_table_association" "private" {
  for_each = local.private_subnets

  subnet_id      = aws_subnet.private[each.key].id
  route_table_id = aws_route_table.private[each.value.vpc_name].id
}
```

</details>

<details>
<summary>TODO 5 -- Lock down default SGs</summary>

```hcl
resource "aws_default_security_group" "lockdown" {
  for_each = aws_vpc.this

  vpc_id = each.value.id

  # No ingress or egress blocks = all rules removed
  tags = { Name = "${var.project_name}-${each.key}-default-LOCKED" }
}
```

</details>

<details>
<summary>TODO 6 -- Output VPC details</summary>

```hcl
output "vpc_ids" {
  description = "VPC IDs by name"
  value       = { for k, v in aws_vpc.this : k => v.id }
}

output "vpc_cidrs" {
  description = "VPC CIDR blocks by name"
  value       = { for k, v in aws_vpc.this : k => v.cidr_block }
}

output "private_subnet_ids" {
  description = "Private subnet IDs grouped by VPC"
  value = {
    for vpc_name in keys(local.vpcs) : vpc_name => [
      for k, v in aws_subnet.private : v.id if v.vpc_id == aws_vpc.this[vpc_name].id
    ]
  }
}

output "peering_readiness" {
  description = "CIDR overlap check"
  value       = "All VPCs use non-overlapping CIDRs. Ready for peering or Transit Gateway."
}
```

</details>

## Verify What You Learned

### Confirm 3 VPCs with non-overlapping CIDRs

```bash
aws ec2 describe-vpcs \
  --filters "Name=tag:Name,Values=multi-vpc-lab-*-vpc" \
  --query "Vpcs[].{Name:Tags[?Key=='Name'].Value|[0],CIDR:CidrBlock,ID:VpcId}" \
  --output table
```

Expected:

```
---------------------------------------------------------------------
|                          DescribeVpcs                             |
+-------------------+----------------------------------------------+
|       CIDR        |              Name              |     ID       |
+-------------------+--------------------------------+--------------+
|  10.0.0.0/16      |  multi-vpc-lab-shared-serv...  |  vpc-0a...   |
|  10.1.0.0/16      |  multi-vpc-lab-dev-vpc         |  vpc-0b...   |
|  10.2.0.0/16      |  multi-vpc-lab-staging-vpc     |  vpc-0c...   |
+-------------------+--------------------------------+--------------+
```

### Verify subnet count per VPC

```bash
for vpc_name in shared-services dev staging; do
  echo "--- $vpc_name ---"
  aws ec2 describe-subnets \
    --filters "Name=tag:Name,Values=multi-vpc-lab-${vpc_name}-*" \
    --query "Subnets[].{AZ:AvailabilityZone,CIDR:CidrBlock,Public:MapPublicIpOnLaunch}" \
    --output table
done
```

Expected: shared-services and dev have 4 subnets each (2 public + 2 private). Staging has 2 subnets (private only).

### Verify route table isolation

```bash
aws ec2 describe-route-tables \
  --filters "Name=tag:Name,Values=multi-vpc-lab-dev-private-rt" \
  --query "RouteTables[0].Routes[].{Dest:DestinationCidrBlock,Target:GatewayId||'local'}" \
  --output table
```

Expected -- only the local route, no cross-VPC routes:

```
-------------------------------------------
|         DescribeRouteTables             |
+-------------------+---------------------+
|       Dest        |       Target        |
+-------------------+---------------------+
|  10.1.0.0/16      |  local              |
+-------------------+---------------------+
```

### Verify no cross-VPC connectivity exists

```bash
aws ec2 describe-vpc-peering-connections \
  --filters "Name=status-code,Values=active" \
  --query "VpcPeeringConnections[?RequesterVpcInfo.VpcId=='$(terraform output -json vpc_ids | jq -r '.dev')'].VpcPeeringConnectionId" \
  --output text
```

Expected: no output (no peering connections exist).

### Confirm default SGs are locked down

```bash
for vpc_name in shared-services dev staging; do
  echo "--- $vpc_name default SG ---"
  aws ec2 describe-security-groups \
    --filters "Name=vpc-id,Values=$(terraform output -json vpc_ids | jq -r ".[\"$vpc_name\"]")" "Name=group-name,Values=default" \
    --query "SecurityGroups[0].{IngressCount:length(IpPermissions),EgressCount:length(IpPermissionsEgress)}" \
    --output table
done
```

Expected: IngressCount and EgressCount both 0 for all three VPCs.

### Confirm no changes

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

You built three isolated VPCs with non-overlapping CIDRs, proper route table isolation, and locked-down default security groups. This is the foundation for multi-environment architectures. In the next exercise, **29 -- NAT Gateway Patterns**, you will add outbound internet access to private subnets using single-NAT and multi-NAT patterns, and analyze the cost and availability tradeoffs of each approach.

## Summary

- **Non-overlapping CIDRs** across all VPCs are mandatory for future peering or Transit Gateway connectivity
- **VPCs are isolated by default** -- there is no implicit cross-VPC routing; connectivity requires explicit resources
- **Deny-all main route tables** prevent accidental internet exposure when new subnets are added
- **Shared-services VPC** pattern centralizes common infrastructure that all environments need
- **Default SG lockdown** in every VPC satisfies CIS Benchmark requirements
- **`for_each` over a VPC map** creates repeatable multi-VPC infrastructure with consistent naming and tagging

## Reference

| Resource | Purpose |
|----------|---------|
| `aws_vpc` | The virtual network with primary CIDR |
| `aws_subnet` | Segments within each VPC |
| `aws_internet_gateway` | Public internet access for selected VPCs |
| `aws_route_table` | Routing rules per tier per VPC |
| `aws_main_route_table_association` | Overrides VPC main route table |
| `aws_default_security_group` | Manages default SG per VPC |

## Additional Resources

- [Multiple VPCs Architecture (AWS Docs)](https://docs.aws.amazon.com/whitepapers/latest/building-scalable-secure-multi-vpc-network-infrastructure/welcome.html) -- official whitepaper on multi-VPC design patterns
- [VPC CIDR Blocks (AWS Docs)](https://docs.aws.amazon.com/vpc/latest/userguide/vpc-cidr-blocks.html) -- rules for primary and secondary CIDR allocation
- [AWS IPAM](https://docs.aws.amazon.com/vpc/latest/ipam/what-it-is-ipam.html) -- centralized IP address management for multi-account environments
- [Terraform for_each with Complex Types](https://developer.hashicorp.com/terraform/language/meta-arguments/for_each) -- using for_each with maps and sets
- [VPC Peering Limitations (AWS Docs)](https://docs.aws.amazon.com/vpc/latest/peering/vpc-peering-basics.html#vpc-peering-limitations) -- why non-overlapping CIDRs matter

## Apply Your Knowledge

- [AWS Landing Zone Multi-VPC Strategy](https://docs.aws.amazon.com/prescriptive-guidance/latest/migration-aws-environment/building-landing-zones.html) -- enterprise patterns for multi-account, multi-VPC deployments
- [Terraform AWS VPC Module](https://registry.terraform.io/modules/terraform-aws-modules/vpc/aws/latest) -- production-grade VPC module with multi-VPC support
- [AWS re:Invent: Networking Foundations](https://www.youtube.com/watch?v=hiKPPy584Mg) -- multi-VPC design patterns at scale

---

> *"Good fences make good neighbors."*
> — **Robert Frost**

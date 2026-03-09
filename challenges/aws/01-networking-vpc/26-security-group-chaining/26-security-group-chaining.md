# 26. Security Group Chaining

<!--
difficulty: basic
concepts: [security-group, sg-to-sg-reference, self-referencing-sg, prefix-list, default-sg-lockdown]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: apply
prerequisites: [01-your-first-vpc, 03-security-groups-and-nacls]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a VPC with security groups but no compute resources (~$0.00/hr for VPC and SGs). Remember to run `terraform destroy` when finished to keep your account tidy.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed Exercise 01 (Your First VPC)
- Completed Exercise 03 (Security Groups and NACLs)

## Learning Objectives

After completing this exercise, you will be able to:

- **Build** a 3-tier security group chain where each tier references the tier above it (web -> app -> db)
- **Create** self-referencing security group rules for clustered services
- **Lock down** the default security group in every VPC
- **Use** managed prefix lists to share common CIDR sets across security groups
- **Explain** why SG-to-SG references are superior to CIDR-based rules between tiers

## Why This Matters

Security groups are stateful firewalls that control traffic at the instance level. The most common mistake is using CIDR ranges (like `10.0.0.0/24`) to allow traffic between tiers. This is brittle: if you add a new subnet or change a CIDR, you must update every security group that references it.

The production pattern is **security group chaining**. Instead of saying "allow port 8080 from 10.0.1.0/24," you say "allow port 8080 from any instance that belongs to the web security group." This creates a logical dependency chain: web-sg -> app-sg -> db-sg. If you add a new instance to the web tier and attach the web security group, it automatically gains access to the app tier without any rule changes.

Self-referencing security groups are equally important. A clustered database (like ElastiCache or Cassandra) needs its nodes to communicate with each other. A self-referencing rule says "allow traffic from any instance in this security group to any other instance in this security group." This is cleaner than listing individual IPs and automatically adapts as nodes are added or removed.

Every VPC comes with a default security group that allows all traffic between members and all outbound traffic. This is a security risk. AWS Security Hub and CIS Benchmarks both flag default security groups with rules as a finding. The fix is to remove all rules from the default security group, making it a useless empty shell.

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
  default     = "sg-chaining-lab"
}
```

### `vpc.tf`

```hcl
# ------------------------------------------------------------------
# VPC and subnets for the 3-tier application
# ------------------------------------------------------------------
data "aws_availability_zones" "available" {
  state = "available"
}

locals {
  vpc_cidr = "10.40.0.0/16"
  azs      = slice(data.aws_availability_zones.available.names, 0, 2)
}

resource "aws_vpc" "this" {
  cidr_block           = local.vpc_cidr
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = "${var.project_name}-vpc" }
}

resource "aws_subnet" "web" {
  for_each = { for i, az in local.azs : az => cidrsubnet(local.vpc_cidr, 8, i) }

  vpc_id                  = aws_vpc.this.id
  cidr_block              = each.value
  availability_zone       = each.key
  map_public_ip_on_launch = true

  tags = { Name = "${var.project_name}-web-${each.key}" }
}

resource "aws_subnet" "app" {
  for_each = { for i, az in local.azs : az => cidrsubnet(local.vpc_cidr, 8, i + 10) }

  vpc_id            = aws_vpc.this.id
  cidr_block        = each.value
  availability_zone = each.key

  tags = { Name = "${var.project_name}-app-${each.key}" }
}

resource "aws_subnet" "db" {
  for_each = { for i, az in local.azs : az => cidrsubnet(local.vpc_cidr, 8, i + 20) }

  vpc_id            = aws_vpc.this.id
  cidr_block        = each.value
  availability_zone = each.key

  tags = { Name = "${var.project_name}-db-${each.key}" }
}
```

### `default-sg-lockdown.tf`

```hcl
# ------------------------------------------------------------------
# LOCK DOWN THE DEFAULT SECURITY GROUP
# ------------------------------------------------------------------
# Every VPC comes with a default SG that allows all inbound traffic
# from other members and all outbound traffic. This is a security
# risk flagged by CIS Benchmarks and AWS Security Hub.
#
# We use aws_default_security_group to take ownership of the default
# SG and remove all its rules, making it an empty, useless shell.
# ------------------------------------------------------------------
resource "aws_default_security_group" "lockdown" {
  vpc_id = aws_vpc.this.id

  # No ingress or egress blocks = all rules removed.
  # The default SG now blocks all traffic.

  tags = { Name = "${var.project_name}-default-sg-LOCKED" }
}
```

> **Best Practice:** Always lock down the default security group in every VPC. Use `aws_default_security_group` with no ingress or egress blocks to strip all rules. This prevents accidental use of the default SG and satisfies CIS Benchmark 5.4 ("Ensure the default security group of every VPC restricts all traffic").

### `security-groups.tf`

```hcl
# ------------------------------------------------------------------
# WEB TIER SECURITY GROUP
# ------------------------------------------------------------------
# Allows HTTP/HTTPS from the internet and all outbound traffic.
# This is the entry point for the 3-tier chain.
# ------------------------------------------------------------------
resource "aws_security_group" "web" {
  name        = "${var.project_name}-web-sg"
  description = "Web tier - allows HTTP/HTTPS from internet"
  vpc_id      = aws_vpc.this.id

  tags = { Name = "${var.project_name}-web-sg" }
}

resource "aws_vpc_security_group_ingress_rule" "web_http" {
  security_group_id = aws_security_group.web.id
  cidr_ipv4         = "0.0.0.0/0"
  from_port         = 80
  to_port           = 80
  ip_protocol       = "tcp"
  description       = "HTTP from internet"
}

resource "aws_vpc_security_group_ingress_rule" "web_https" {
  security_group_id = aws_security_group.web.id
  cidr_ipv4         = "0.0.0.0/0"
  from_port         = 443
  to_port           = 443
  ip_protocol       = "tcp"
  description       = "HTTPS from internet"
}

resource "aws_vpc_security_group_egress_rule" "web_all_out" {
  security_group_id = aws_security_group.web.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
  description       = "All outbound traffic"
}

# ------------------------------------------------------------------
# APP TIER SECURITY GROUP
# ------------------------------------------------------------------
# Allows port 8080 ONLY from instances in the web security group.
# This is the key pattern: SG-to-SG reference instead of CIDR.
# ------------------------------------------------------------------
resource "aws_security_group" "app" {
  name        = "${var.project_name}-app-sg"
  description = "App tier - allows traffic only from web tier SG"
  vpc_id      = aws_vpc.this.id

  tags = { Name = "${var.project_name}-app-sg" }
}

resource "aws_vpc_security_group_ingress_rule" "app_from_web" {
  security_group_id            = aws_security_group.app.id
  referenced_security_group_id = aws_security_group.web.id
  from_port                    = 8080
  to_port                      = 8080
  ip_protocol                  = "tcp"
  description                  = "App port from web tier SG"
}

resource "aws_vpc_security_group_egress_rule" "app_all_out" {
  security_group_id = aws_security_group.app.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
  description       = "All outbound traffic"
}

# ------------------------------------------------------------------
# DB TIER SECURITY GROUP
# ------------------------------------------------------------------
# Allows port 5432 (PostgreSQL) ONLY from instances in the app SG.
# Completes the chain: web-sg -> app-sg -> db-sg.
# ------------------------------------------------------------------
resource "aws_security_group" "db" {
  name        = "${var.project_name}-db-sg"
  description = "DB tier - allows traffic only from app tier SG"
  vpc_id      = aws_vpc.this.id

  tags = { Name = "${var.project_name}-db-sg" }
}

resource "aws_vpc_security_group_ingress_rule" "db_from_app" {
  security_group_id            = aws_security_group.db.id
  referenced_security_group_id = aws_security_group.app.id
  from_port                    = 5432
  to_port                      = 5432
  ip_protocol                  = "tcp"
  description                  = "PostgreSQL from app tier SG"
}

resource "aws_vpc_security_group_egress_rule" "db_all_out" {
  security_group_id = aws_security_group.db.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
  description       = "All outbound traffic"
}

# ------------------------------------------------------------------
# SELF-REFERENCING SECURITY GROUP (for clustered services)
# ------------------------------------------------------------------
# A self-referencing rule allows instances in the same SG to
# communicate with each other. Essential for:
#   - ElastiCache cluster replication
#   - Cassandra/MongoDB gossip protocol
#   - ECS service-to-service within a cluster
# ------------------------------------------------------------------
resource "aws_security_group" "cluster" {
  name        = "${var.project_name}-cluster-sg"
  description = "Cluster SG with self-referencing rule for node-to-node"
  vpc_id      = aws_vpc.this.id

  tags = { Name = "${var.project_name}-cluster-sg" }
}

resource "aws_vpc_security_group_ingress_rule" "cluster_self" {
  security_group_id            = aws_security_group.cluster.id
  referenced_security_group_id = aws_security_group.cluster.id
  ip_protocol                  = "-1"
  description                  = "All traffic from members of this SG"
}

resource "aws_vpc_security_group_egress_rule" "cluster_all_out" {
  security_group_id = aws_security_group.cluster.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
  description       = "All outbound traffic"
}
```

> **Best Practice:** Always reference security groups (using `referenced_security_group_id`) instead of CIDRs between tiers. SG references are dynamic: when you add a new instance to the web tier, it automatically gains access to the app tier without updating any rules. CIDR-based rules require manual updates every time subnets change.

### `prefix-list.tf`

```hcl
# ------------------------------------------------------------------
# MANAGED PREFIX LIST
# ------------------------------------------------------------------
# A prefix list is a set of CIDR blocks that can be referenced in
# multiple security groups and route tables. Instead of duplicating
# the same CIDRs across 10 security groups, reference one prefix
# list. When the CIDRs change, update the prefix list once.
# ------------------------------------------------------------------
resource "aws_ec2_managed_prefix_list" "corporate_vpn" {
  name           = "${var.project_name}-corporate-vpn"
  address_family = "IPv4"
  max_entries    = 10

  entry {
    cidr        = "203.0.113.0/24"
    description = "Corporate office #1"
  }

  entry {
    cidr        = "198.51.100.0/24"
    description = "Corporate office #2"
  }

  tags = { Name = "${var.project_name}-corporate-vpn-pl" }
}

# Allow SSH from the corporate prefix list to the web tier
resource "aws_vpc_security_group_ingress_rule" "web_ssh_from_corp" {
  security_group_id = aws_security_group.web.id
  prefix_list_id    = aws_ec2_managed_prefix_list.corporate_vpn.id
  from_port         = 22
  to_port           = 22
  ip_protocol       = "tcp"
  description       = "SSH from corporate VPN prefix list"
}
```

### `outputs.tf`

```hcl
output "vpc_id" {
  description = "VPC ID"
  value       = aws_vpc.this.id
}

output "web_sg_id" {
  description = "Web tier security group ID"
  value       = aws_security_group.web.id
}

output "app_sg_id" {
  description = "App tier security group ID"
  value       = aws_security_group.app.id
}

output "db_sg_id" {
  description = "DB tier security group ID"
  value       = aws_security_group.db.id
}

output "cluster_sg_id" {
  description = "Cluster (self-referencing) security group ID"
  value       = aws_security_group.cluster.id
}

output "prefix_list_id" {
  description = "Corporate VPN prefix list ID"
  value       = aws_ec2_managed_prefix_list.corporate_vpn.id
}

output "default_sg_id" {
  description = "Default SG ID (locked down)"
  value       = aws_default_security_group.lockdown.id
}
```

## Step 2 -- Initialize and Apply

```bash
terraform init
terraform apply -auto-approve
```

Terraform will create approximately 24 resources: VPC, subnets, 5 security groups (web, app, db, cluster, default lockdown), their ingress/egress rules, and a prefix list.

### Intermediate Verification

```bash
terraform state list
```

Confirm you see all security groups:

```
aws_default_security_group.lockdown
aws_security_group.app
aws_security_group.cluster
aws_security_group.db
aws_security_group.web
```

## Common Mistakes

### 1. Using 0.0.0.0/0 between tiers

The biggest security anti-pattern is allowing all traffic from anywhere between internal tiers.

**Wrong -- CIDR 0.0.0.0/0 on the app tier:**

```hcl
resource "aws_vpc_security_group_ingress_rule" "app_wide_open" {
  security_group_id = aws_security_group.app.id
  cidr_ipv4         = "0.0.0.0/0"
  from_port         = 8080
  to_port           = 8080
  ip_protocol       = "tcp"
}
```

**What happens:** Any instance in any VPC (or the internet, if the subnet has an IGW route) can reach the app tier on port 8080. A compromised web server in a different account could attack your app tier directly.

**Fix -- reference the web security group:**

```hcl
resource "aws_vpc_security_group_ingress_rule" "app_from_web" {
  security_group_id            = aws_security_group.app.id
  referenced_security_group_id = aws_security_group.web.id
  from_port                    = 8080
  to_port                      = 8080
  ip_protocol                  = "tcp"
}
```

### 2. Not locking down the default security group

The default SG allows all inbound traffic from its own members and all outbound traffic. If someone accidentally attaches it to an instance, that instance can communicate with every other instance using the default SG.

**Wrong -- leaving the default SG unmanaged:**

```hcl
# The default SG exists with permissive rules.
# No aws_default_security_group resource in your code.
# CIS Benchmark 5.4 flags this as a FAIL.
```

**Fix -- take ownership and strip all rules:**

```hcl
resource "aws_default_security_group" "lockdown" {
  vpc_id = aws_vpc.this.id
  # No ingress or egress blocks = all rules removed
  tags = { Name = "default-sg-LOCKED" }
}
```

### 3. Forgetting that SG references only work within the same VPC

Security group references (`referenced_security_group_id`) only work within the same VPC. Cross-VPC references require VPC peering with security group referencing enabled, or you must fall back to CIDR-based rules.

**Wrong -- referencing a SG from another VPC without peering:**

```hcl
resource "aws_vpc_security_group_ingress_rule" "cross_vpc" {
  security_group_id            = aws_security_group.app.id
  referenced_security_group_id = "sg-0abc123"   # Different VPC!
  from_port                    = 8080
  to_port                      = 8080
  ip_protocol                  = "tcp"
}
```

**What happens:** The API call fails with `InvalidGroup.NotFound` because the referenced SG is not in the same VPC.

**Fix -- use CIDR or enable peering SG references:**

For cross-VPC access without peering, use CIDR-based rules or prefix lists.

## Verify What You Learned

```bash
aws ec2 describe-security-groups \
  --filters "Name=vpc-id,Values=$(terraform output -raw vpc_id)" \
  --query "SecurityGroups[].{Name:GroupName,ID:GroupId,Description:Description}" \
  --output table
```

Expected:

```
------------------------------------------------------------------------------------
|                           DescribeSecurityGroups                                 |
+---------------------------+-------------+----------------------------------------+
|       Description         |     ID      |            Name                        |
+---------------------------+-------------+----------------------------------------+
|  default VPC security ... |  sg-0a...   |  default                               |
|  Web tier - allows HT... |  sg-0b...   |  sg-chaining-lab-web-sg                |
|  App tier - allows tra...|  sg-0c...   |  sg-chaining-lab-app-sg                |
|  DB tier - allows traf...|  sg-0d...   |  sg-chaining-lab-db-sg                 |
|  Cluster SG with self-...|  sg-0e...   |  sg-chaining-lab-cluster-sg            |
+---------------------------+-------------+----------------------------------------+
```

```bash
aws ec2 describe-security-group-rules \
  --filters "Name=group-id,Values=$(terraform output -raw app_sg_id)" \
  --query "SecurityGroupRules[?IsEgress==\`false\`].{Port:FromPort,Protocol:IpProtocol,SourceSG:ReferencedGroupInfo.GroupId}" \
  --output table
```

Expected -- the app tier references the web SG, not a CIDR:

```
------------------------------------------------------
|            DescribeSecurityGroupRules              |
+--------+------------+-----------------------------+
|  Port  |  Protocol  |          SourceSG           |
+--------+------------+-----------------------------+
|  8080  |  tcp       |  sg-0b... (web SG ID)       |
+--------+------------+-----------------------------+
```

```bash
aws ec2 describe-security-group-rules \
  --filters "Name=group-id,Values=$(terraform output -raw default_sg_id)" \
  --query "SecurityGroupRules" \
  --output table
```

Expected: empty table (no rules -- the default SG is locked down).

```bash
aws ec2 describe-managed-prefix-lists \
  --filters "Name=prefix-list-name,Values=sg-chaining-lab-corporate-vpn" \
  --query "PrefixLists[0].{Name:PrefixListName,MaxEntries:MaxEntries,State:State}" \
  --output table
```

Expected:

```
---------------------------------------------------------
|             DescribeManagedPrefixLists                 |
+----------------------------+-----------+---------------+
|           Name             | MaxEntries|    State      |
+----------------------------+-----------+---------------+
|  sg-chaining-lab-corp...   |  10       |  create-..    |
+----------------------------+-----------+---------------+
```

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

You now understand how to chain security groups across tiers, create self-referencing rules for clusters, lock down default security groups, and use prefix lists for reusable CIDR sets. In the next exercise, **27 -- Security Group Audit**, you will learn how to detect misconfigured security groups at scale -- finding overly permissive rules, unused SGs, and correlating flow log data with SG configurations.

## Summary

- **SG-to-SG references** (`referenced_security_group_id`) create dynamic, logical access chains between tiers -- no CIDR maintenance required
- **Self-referencing SGs** allow instances within the same group to communicate -- essential for clustered services like ElastiCache and Cassandra
- **Lock down the default SG** in every VPC using `aws_default_security_group` with no rules -- this satisfies CIS Benchmark 5.4
- **Managed prefix lists** centralize CIDR sets and can be referenced in SGs and route tables -- update once, applied everywhere
- **SG references only work within the same VPC** (or across peered VPCs with SG referencing enabled)
- Use **separate rule resources** (`aws_vpc_security_group_ingress_rule`/`egress_rule`) instead of inline blocks for clarity and independent lifecycle

## Reference

| Resource | Purpose |
|----------|---------|
| `aws_security_group` | Virtual firewall at the instance level |
| `aws_vpc_security_group_ingress_rule` | Inbound rule as separate resource |
| `aws_vpc_security_group_egress_rule` | Outbound rule as separate resource |
| `aws_default_security_group` | Takes ownership of the VPC default SG |
| `aws_ec2_managed_prefix_list` | Reusable set of CIDR blocks |

## Additional Resources

- [Security Groups (AWS Docs)](https://docs.aws.amazon.com/vpc/latest/userguide/vpc-security-groups.html) -- official guide to security group rules, limits, and behavior
- [Terraform aws_security_group Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/security_group) -- Terraform reference for security group management
- [Managed Prefix Lists (AWS Docs)](https://docs.aws.amazon.com/vpc/latest/userguide/managed-prefix-lists.html) -- how to create and share prefix lists across accounts
- [CIS AWS Foundations Benchmark](https://www.cisecurity.org/benchmark/amazon_web_services) -- security group controls from the CIS Benchmark

## Apply Your Knowledge

- [AWS Security Hub -- Security Group Findings](https://docs.aws.amazon.com/securityhub/latest/userguide/ec2-controls.html) -- automated detection of misconfigured security groups
- [VPC Peering Security Group References](https://docs.aws.amazon.com/vpc/latest/peering/vpc-peering-security-groups.html) -- how to enable cross-VPC security group references
- [AWS re:Invent: VPC Security Best Practices](https://www.youtube.com/watch?v=zKmv99xnDHk) -- advanced security group patterns for enterprise deployments

---

> *"The price of freedom is eternal vigilance."*
> — **Thomas Jefferson**

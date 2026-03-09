# 19. Security Groups vs Network ACLs

<!--
difficulty: basic
concepts: [security-groups, nacls, stateful-vs-stateless, inbound-outbound-rules, rule-evaluation-order, defense-in-depth, ephemeral-ports]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: understand
prerequisites: [17-vpc-subnets-route-tables-igw]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** Security groups and NACLs have no hourly cost. You pay only for the underlying VPC resources. Total ~$0.01/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 17 or equivalent understanding of VPC and subnets

## Learning Objectives

After completing this exercise, you will be able to:

- **Distinguish** security groups (stateful, instance-level, allow-only) from NACLs (stateless, subnet-level, allow and deny)
- **Explain** why NACL rules are evaluated in numbered order and how a lower-numbered deny overrides a higher-numbered allow
- **Construct** security groups with self-referencing rules and NACLs with explicit allow/deny rules using Terraform
- **Identify** the need for ephemeral port ranges in NACL outbound rules when using stateless filtering
- **Design** a defense-in-depth strategy combining security groups and NACLs for multi-tier architectures
- **Describe** the default security group behavior (allow all outbound, deny all inbound) and default NACL behavior (allow all)

## Why Security Groups vs NACLs Matter

The SAA-C03 exam expects you to know when to use security groups, NACLs, or both. Security groups are the primary tool for controlling access to individual resources. They are stateful: if you allow an inbound request on port 443, the response is automatically allowed regardless of outbound rules. You can only write allow rules -- there is no way to explicitly deny a specific IP. Security groups evaluate all rules collectively; there is no ordering.

NACLs operate at the subnet level and are stateless: you must explicitly allow both inbound and outbound traffic, including ephemeral ports for return traffic. NACLs support both allow and deny rules, and rules are evaluated in order by rule number (lowest first). The first matching rule wins. This makes NACLs useful for blocking specific IPs or CIDR ranges -- something security groups cannot do. The classic exam pattern: "Block a specific IP address from accessing your application" requires a NACL deny rule because security groups do not support deny.

Defense in depth means using both: NACLs as a coarse subnet-level filter (block known bad IPs, restrict protocols) and security groups as a fine-grained instance-level filter (allow only specific ports from specific sources).

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
  default     = "sg-nacl-demo"
}
```

### `vpc.tf`

```hcl
data "aws_availability_zones" "available" {
  state = "available"
}

resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags                 = { Name = var.project_name }
}

resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id
  tags   = { Name = "${var.project_name}-igw" }
}

resource "aws_subnet" "web" {
  vpc_id                  = aws_vpc.this.id
  cidr_block              = "10.0.1.0/24"
  availability_zone       = data.aws_availability_zones.available.names[0]
  map_public_ip_on_launch = true
  tags                    = { Name = "${var.project_name}-web" }
}

resource "aws_subnet" "app" {
  vpc_id            = aws_vpc.this.id
  cidr_block        = "10.0.10.0/24"
  availability_zone = data.aws_availability_zones.available.names[0]
  tags              = { Name = "${var.project_name}-app" }
}

resource "aws_subnet" "db" {
  vpc_id            = aws_vpc.this.id
  cidr_block        = "10.0.20.0/24"
  availability_zone = data.aws_availability_zones.available.names[0]
  tags              = { Name = "${var.project_name}-db" }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.this.id
  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this.id
  }
  tags = { Name = "${var.project_name}-public-rt" }
}

resource "aws_route_table_association" "web" {
  subnet_id      = aws_subnet.web.id
  route_table_id = aws_route_table.public.id
}
```

### `security.tf`

```hcl
# ------------------------------------------------------------------
# Web Security Group: allows HTTP/HTTPS from the internet.
#
# Security groups are stateful: allowing inbound port 443
# automatically allows the outbound response. No need to add
# an outbound rule for ephemeral ports.
#
# All rules are evaluated collectively -- there is no ordering.
# You can only write ALLOW rules; there is no DENY.
# ------------------------------------------------------------------
resource "aws_security_group" "web" {
  name_prefix = "${var.project_name}-web-"
  vpc_id      = aws_vpc.this.id
  description = "Web tier: HTTP/HTTPS from internet"

  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "HTTP from internet"
  }

  ingress {
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "HTTPS from internet"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
    description = "All outbound"
  }

  tags = { Name = "${var.project_name}-web" }
}

# ------------------------------------------------------------------
# App Security Group: allows traffic only from the web tier.
#
# This uses a security group reference instead of a CIDR block.
# Any instance attached to the web SG can reach port 8080 on
# instances attached to the app SG. This is more maintainable
# than CIDR-based rules because it adapts as instances are added
# or removed.
# ------------------------------------------------------------------
resource "aws_security_group" "app" {
  name_prefix = "${var.project_name}-app-"
  vpc_id      = aws_vpc.this.id
  description = "App tier: accepts from web tier only"

  ingress {
    from_port       = 8080
    to_port         = 8080
    protocol        = "tcp"
    security_groups = [aws_security_group.web.id]
    description     = "App port from web tier"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
    description = "All outbound"
  }

  tags = { Name = "${var.project_name}-app" }
}

# ------------------------------------------------------------------
# DB Security Group: allows traffic only from the app tier.
# ------------------------------------------------------------------
resource "aws_security_group" "db" {
  name_prefix = "${var.project_name}-db-"
  vpc_id      = aws_vpc.this.id
  description = "DB tier: accepts from app tier only"

  ingress {
    from_port       = 3306
    to_port         = 3306
    protocol        = "tcp"
    security_groups = [aws_security_group.app.id]
    description     = "MySQL from app tier"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
    description = "All outbound"
  }

  tags = { Name = "${var.project_name}-db" }
}

# ------------------------------------------------------------------
# Web Subnet NACL: coarse subnet-level filtering.
#
# NACLs are stateless: you MUST allow return traffic explicitly.
# For HTTP inbound (port 80), the client uses an ephemeral port
# (1024-65535) for the return path. Without an outbound rule for
# ephemeral ports, responses are silently dropped.
#
# Rules are evaluated by number (lowest first). The FIRST matching
# rule is applied. Rule 100 is evaluated before rule 200.
# The default rule (*) denies everything not explicitly allowed.
# ------------------------------------------------------------------
resource "aws_network_acl" "web" {
  vpc_id     = aws_vpc.this.id
  subnet_ids = [aws_subnet.web.id]

  # Rule 100: Allow HTTP inbound
  ingress {
    rule_no    = 100
    protocol   = "tcp"
    action     = "allow"
    cidr_block = "0.0.0.0/0"
    from_port  = 80
    to_port    = 80
  }

  # Rule 110: Allow HTTPS inbound
  ingress {
    rule_no    = 110
    protocol   = "tcp"
    action     = "allow"
    cidr_block = "0.0.0.0/0"
    from_port  = 443
    to_port    = 443
  }

  # Rule 120: Allow return traffic on ephemeral ports
  # (responses to outbound connections initiated from this subnet)
  ingress {
    rule_no    = 120
    protocol   = "tcp"
    action     = "allow"
    cidr_block = "0.0.0.0/0"
    from_port  = 1024
    to_port    = 65535
  }

  # Rule 100: Allow all outbound (for simplicity; restrict in production)
  egress {
    rule_no    = 100
    protocol   = "-1"
    action     = "allow"
    cidr_block = "0.0.0.0/0"
    from_port  = 0
    to_port    = 0
  }

  tags = { Name = "${var.project_name}-web-nacl" }
}

# ------------------------------------------------------------------
# App Subnet NACL: allows only traffic from the web subnet.
#
# This demonstrates using CIDR-based source filtering at the
# subnet level. Only the web subnet (10.0.1.0/24) can reach
# port 8080, and return traffic on ephemeral ports is allowed.
# ------------------------------------------------------------------
resource "aws_network_acl" "app" {
  vpc_id     = aws_vpc.this.id
  subnet_ids = [aws_subnet.app.id]

  # Allow app traffic from web subnet
  ingress {
    rule_no    = 100
    protocol   = "tcp"
    action     = "allow"
    cidr_block = "10.0.1.0/24"
    from_port  = 8080
    to_port    = 8080
  }

  # Allow return traffic on ephemeral ports from anywhere in VPC
  ingress {
    rule_no    = 110
    protocol   = "tcp"
    action     = "allow"
    cidr_block = "10.0.0.0/16"
    from_port  = 1024
    to_port    = 65535
  }

  # Allow all outbound within VPC
  egress {
    rule_no    = 100
    protocol   = "-1"
    action     = "allow"
    cidr_block = "10.0.0.0/16"
    from_port  = 0
    to_port    = 0
  }

  tags = { Name = "${var.project_name}-app-nacl" }
}

# ------------------------------------------------------------------
# DB Subnet NACL with explicit deny example.
#
# Rule 50 DENIES a specific IP range before rule 100 ALLOWS
# the app subnet. Since 50 < 100, the deny is evaluated first.
# This is the pattern for "block a specific IP" -- something
# security groups cannot do.
# ------------------------------------------------------------------
resource "aws_network_acl" "db" {
  vpc_id     = aws_vpc.this.id
  subnet_ids = [aws_subnet.db.id]

  # Rule 50: DENY a specific IP range (example: known bad actor)
  ingress {
    rule_no    = 50
    protocol   = "tcp"
    action     = "deny"
    cidr_block = "10.0.10.100/32"
    from_port  = 3306
    to_port    = 3306
  }

  # Rule 100: Allow MySQL from app subnet
  ingress {
    rule_no    = 100
    protocol   = "tcp"
    action     = "allow"
    cidr_block = "10.0.10.0/24"
    from_port  = 3306
    to_port    = 3306
  }

  # Rule 110: Allow return traffic on ephemeral ports
  ingress {
    rule_no    = 110
    protocol   = "tcp"
    action     = "allow"
    cidr_block = "10.0.0.0/16"
    from_port  = 1024
    to_port    = 65535
  }

  # Allow all outbound within VPC
  egress {
    rule_no    = 100
    protocol   = "-1"
    action     = "allow"
    cidr_block = "10.0.0.0/16"
    from_port  = 0
    to_port    = 0
  }

  tags = { Name = "${var.project_name}-db-nacl" }
}
```

### `outputs.tf`

```hcl
output "vpc_id" {
  value = aws_vpc.this.id
}

output "web_sg_id" {
  value = aws_security_group.web.id
}

output "app_sg_id" {
  value = aws_security_group.app.id
}

output "db_sg_id" {
  value = aws_security_group.db.id
}

output "web_nacl_id" {
  value = aws_network_acl.web.id
}

output "db_nacl_id" {
  value = aws_network_acl.db.id
}
```

## Step 2 -- Deploy and Explore

```bash
terraform init
terraform apply -auto-approve
```

### Comparison Table: Security Groups vs NACLs

| Feature | Security Group | NACL |
|---------|---------------|------|
| **Scope** | Instance/ENI level | Subnet level |
| **State** | Stateful (return traffic auto-allowed) | Stateless (must allow return explicitly) |
| **Rule type** | Allow only | Allow and Deny |
| **Rule evaluation** | All rules evaluated together | Rules evaluated by number (first match) |
| **Default inbound** | Deny all | Allow all (default NACL) |
| **Default outbound** | Allow all | Allow all (default NACL) |
| **Apply to** | Explicitly assigned to resource | All resources in associated subnet |
| **# per VPC** | Up to 2,500 | Up to 200 |
| **Rules per SG/NACL** | 60 inbound + 60 outbound | 20 inbound + 20 outbound |
| **SG references** | Yes (reference other SGs) | No (CIDR only) |

## Common Mistakes

### 1. NACL deny rule with higher number than allow

**Wrong approach:** Placing a deny rule at rule number 200 to block an IP, with an allow at rule 100:

```hcl
# Rule 100: Allow all from app subnet
ingress {
  rule_no    = 100
  action     = "allow"
  cidr_block = "10.0.10.0/24"
  from_port  = 3306
  to_port    = 3306
  protocol   = "tcp"
}

# Rule 200: Deny a specific IP -- THIS NEVER FIRES
ingress {
  rule_no    = 200
  action     = "deny"
  cidr_block = "10.0.10.100/32"
  from_port  = 3306
  to_port    = 3306
  protocol   = "tcp"
}
```

**What happens:** Rule 100 matches first (lower number) and allows traffic from the entire 10.0.10.0/24 subnet, including 10.0.10.100. Rule 200 is never evaluated for that traffic because rule 100 already matched. The IP you intended to block has full access.

**Fix:** Place deny rules with LOWER numbers than allow rules:

```hcl
ingress {
  rule_no    = 50     # Evaluated FIRST
  action     = "deny"
  cidr_block = "10.0.10.100/32"
  # ...
}

ingress {
  rule_no    = 100    # Evaluated SECOND
  action     = "allow"
  cidr_block = "10.0.10.0/24"
  # ...
}
```

### 2. Forgetting ephemeral ports in NACL outbound rules

**Wrong approach:** Allowing only port 80 inbound in the NACL but not allowing ephemeral ports outbound:

```hcl
ingress {
  rule_no    = 100
  action     = "allow"
  cidr_block = "0.0.0.0/0"
  from_port  = 80
  to_port    = 80
  protocol   = "tcp"
}

# No outbound rule for ephemeral ports (1024-65535)
```

**What happens:** NACLs are stateless. The HTTP request arrives on port 80, but the response (from a high ephemeral port) is blocked by the default deny outbound rule. The client sees a connection timeout despite the inbound rule being correct. Security groups do not have this problem because they are stateful.

**Fix:** Add an outbound rule for ephemeral ports (1024-65535):

```hcl
egress {
  rule_no    = 100
  action     = "allow"
  cidr_block = "0.0.0.0/0"
  from_port  = 1024
  to_port    = 65535
  protocol   = "tcp"
}
```

### 3. Trying to block a specific IP with a security group

**Wrong approach:** Attempting to deny a specific IP address using a security group.

**What happens:** Security groups only support allow rules. There is no deny action. You cannot block a single IP while allowing the rest of a CIDR range in a security group.

**Fix:** Use a NACL deny rule to block specific IPs or CIDR ranges. Security groups handle allow logic; NACLs handle deny logic.

## Verify What You Learned

```bash
# Verify security group has only ALLOW rules
aws ec2 describe-security-groups \
  --group-ids $(terraform output -raw web_sg_id) \
  --query "SecurityGroups[0].IpPermissions[*].{Port:FromPort,Source:IpRanges[0].CidrIp}" \
  --output table
```

Expected: Ports 80 and 443 with source 0.0.0.0/0.

```bash
# Verify NACL has both ALLOW and DENY rules
aws ec2 describe-network-acls \
  --network-acl-ids $(terraform output -raw db_nacl_id) \
  --query "NetworkAcls[0].Entries[?Egress==\`false\`].{RuleNum:RuleNumber,Action:RuleAction,CIDR:CidrBlock,Port:PortRange.From}" \
  --output table
```

Expected: Rule 50 with action DENY, rule 100 with action ALLOW.

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

You now understand both layers of VPC access control. In the next exercise, you will enable **VPC Flow Logs** to capture and analyze network traffic metadata, identify blocked traffic patterns, and use Flow Logs to troubleshoot connectivity issues caused by misconfigured security groups and NACLs.

## Summary

- **Security groups** are stateful, instance-level, and support only allow rules -- return traffic is automatically permitted
- **NACLs** are stateless, subnet-level, and support both allow and deny rules -- return traffic must be explicitly allowed
- NACL rules are evaluated by **rule number** (lowest first); the first matching rule wins
- To block a specific IP, use a **NACL deny rule** with a lower number than the corresponding allow rule
- NACLs require **ephemeral port ranges** (1024-65535) for return traffic because they are stateless
- Security groups support **SG references** (allow traffic from another security group); NACLs use only CIDRs
- **Defense in depth**: use NACLs for coarse subnet-level filtering and security groups for fine-grained instance-level control
- The default NACL **allows all** traffic; custom NACLs **deny all** by default (rule * = deny)

## Reference

- [Security Groups](https://docs.aws.amazon.com/vpc/latest/userguide/vpc-security-groups.html)
- [Network ACLs](https://docs.aws.amazon.com/vpc/latest/userguide/vpc-network-acls.html)
- [Comparison of Security Groups and NACLs](https://docs.aws.amazon.com/vpc/latest/userguide/VPC_Security.html)
- [Terraform aws_security_group Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/security_group)
- [Terraform aws_network_acl Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/network_acl)

## Additional Resources

- [Ephemeral Port Ranges by OS](https://docs.aws.amazon.com/vpc/latest/userguide/vpc-network-acls.html#nacl-ephemeral-ports) -- Linux uses 32768-60999, Windows uses 49152-65535
- [Security Group Rules Per ENI Limits](https://docs.aws.amazon.com/vpc/latest/userguide/amazon-vpc-limits.html#vpc-limits-security-groups) -- exceeding limits causes new instance launches to fail
- [VPC Security Best Practices](https://docs.aws.amazon.com/vpc/latest/userguide/vpc-security-best-practices.html) -- AWS recommendations for layered security
- [Managed Prefix Lists](https://docs.aws.amazon.com/vpc/latest/userguide/managed-prefix-lists.html) -- reusable CIDR sets for security groups and route tables

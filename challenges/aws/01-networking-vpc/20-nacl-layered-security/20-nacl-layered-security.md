# 20. NACLs: Layered Security Patterns

<!--
difficulty: intermediate
concepts: [nacl, stateless-filtering, rule-ordering, ephemeral-ports, defense-in-depth, security-groups]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: analyze
prerequisites: [01-your-first-vpc, 03-security-groups-and-nacls]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates t3.micro EC2 instances (~$0.0104/hr each). NACLs themselves are free. Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Exercise 01 completed | Understand VPC basics |
| Exercise 03 completed | Understand SG vs NACL basics |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Analyze** the difference between stateful security groups and stateless NACLs
2. **Implement** NACL rules with correct priority ordering to deny-then-allow
3. **Configure** ephemeral port ranges for return traffic in stateless rules
4. **Design** a layered security architecture combining NACLs and security groups
5. **Debug** connectivity issues caused by NACL rule ordering mistakes

## Why NACLs for Layered Security

Security groups are stateful firewalls at the instance level -- they automatically allow return traffic for any connection they permit. NACLs (Network Access Control Lists) are stateless firewalls at the subnet level -- every packet is evaluated independently against the rule set, including return traffic. This means you must explicitly allow return traffic on ephemeral ports (1024-65535) or connections will silently break.

Why bother with NACLs when security groups already exist? Because security groups can only **allow** traffic -- they have no deny rules. NACLs can explicitly **deny** traffic from specific IP ranges, which is essential for blocking known malicious IPs, complying with IP-based access policies, and adding a defense-in-depth layer that protects against security group misconfigurations. The combination of NACLs (subnet perimeter) and security groups (instance perimeter) creates two independent layers that an attacker must bypass.

> **Best Practice:** NACLs are stateless -- you MUST explicitly allow return traffic on ephemeral ports (1024-65535). Forgetting this is the number one cause of mysterious connectivity failures with NACLs.

## Building Blocks

Create the following files in your exercise directory. Your job is to fill in each `# TODO` block.

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
  default     = "nacl-security-lab"
}

variable "blocked_cidr" {
  description = "CIDR range to explicitly deny (simulates a known-bad IP range)"
  type        = string
  default     = "198.51.100.0/24"
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

  tags = { Name = "${var.project_name}-vpc" }
}

resource "aws_subnet" "public" {
  vpc_id                  = aws_vpc.this.id
  cidr_block              = "10.0.1.0/24"
  availability_zone       = data.aws_availability_zones.available.names[0]
  map_public_ip_on_launch = true

  tags = { Name = "${var.project_name}-public" }
}

resource "aws_subnet" "app" {
  vpc_id            = aws_vpc.this.id
  cidr_block        = "10.0.2.0/24"
  availability_zone = data.aws_availability_zones.available.names[0]

  tags = { Name = "${var.project_name}-app" }
}

resource "aws_subnet" "data" {
  vpc_id            = aws_vpc.this.id
  cidr_block        = "10.0.3.0/24"
  availability_zone = data.aws_availability_zones.available.names[0]

  tags = { Name = "${var.project_name}-data" }
}

resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id

  tags = { Name = "${var.project_name}-igw" }
}

resource "aws_eip" "nat" {
  domain = "vpc"

  tags = { Name = "${var.project_name}-nat-eip" }
}

resource "aws_nat_gateway" "this" {
  allocation_id = aws_eip.nat.id
  subnet_id     = aws_subnet.public.id

  tags = { Name = "${var.project_name}-nat" }

  depends_on = [aws_internet_gateway.this]
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.this.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this.id
  }

  tags = { Name = "${var.project_name}-public-rt" }
}

resource "aws_route_table_association" "public" {
  subnet_id      = aws_subnet.public.id
  route_table_id = aws_route_table.public.id
}

resource "aws_route_table" "private" {
  vpc_id = aws_vpc.this.id

  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.this.id
  }

  tags = { Name = "${var.project_name}-private-rt" }
}

resource "aws_route_table_association" "app" {
  subnet_id      = aws_subnet.app.id
  route_table_id = aws_route_table.private.id
}

resource "aws_route_table_association" "data" {
  subnet_id      = aws_subnet.data.id
  route_table_id = aws_route_table.private.id
}
```

### `security.tf`

```hcl
# ------------------------------------------------------------------
# Security groups: the inner layer of defense (stateful).
# ------------------------------------------------------------------
resource "aws_security_group" "web" {
  name        = "${var.project_name}-web-sg"
  description = "Web tier: HTTP from internet, SSH for management"
  vpc_id      = aws_vpc.this.id

  tags = { Name = "${var.project_name}-web-sg" }
}

resource "aws_vpc_security_group_ingress_rule" "web_http" {
  security_group_id = aws_security_group.web.id
  cidr_ipv4         = "0.0.0.0/0"
  from_port         = 80
  to_port           = 80
  ip_protocol       = "tcp"
}

resource "aws_vpc_security_group_ingress_rule" "web_ssh" {
  security_group_id = aws_security_group.web.id
  cidr_ipv4         = "0.0.0.0/0"
  from_port         = 22
  to_port           = 22
  ip_protocol       = "tcp"
}

resource "aws_vpc_security_group_egress_rule" "web_out" {
  security_group_id = aws_security_group.web.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}

resource "aws_security_group" "app" {
  name        = "${var.project_name}-app-sg"
  description = "App tier: HTTP from web SG only"
  vpc_id      = aws_vpc.this.id

  tags = { Name = "${var.project_name}-app-sg" }
}

resource "aws_vpc_security_group_ingress_rule" "app_from_web" {
  security_group_id            = aws_security_group.app.id
  referenced_security_group_id = aws_security_group.web.id
  from_port                    = 8080
  to_port                      = 8080
  ip_protocol                  = "tcp"
}

resource "aws_vpc_security_group_egress_rule" "app_out" {
  security_group_id = aws_security_group.app.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}

resource "aws_security_group" "db" {
  name        = "${var.project_name}-db-sg"
  description = "Data tier: MySQL from app SG only"
  vpc_id      = aws_vpc.this.id

  tags = { Name = "${var.project_name}-db-sg" }
}

resource "aws_vpc_security_group_ingress_rule" "db_from_app" {
  security_group_id            = aws_security_group.db.id
  referenced_security_group_id = aws_security_group.app.id
  from_port                    = 3306
  to_port                      = 3306
  ip_protocol                  = "tcp"
}

resource "aws_vpc_security_group_egress_rule" "db_out" {
  security_group_id = aws_security_group.db.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}
```

### `nacl.tf`

```hcl
# =======================================================
# TODO 1 -- Public Subnet NACL
# =======================================================
# Requirements:
#   - Create an aws_network_acl for the public subnet
#   - DENY all traffic from var.blocked_cidr at rule 100 (ingress)
#   - ALLOW HTTP (80) from 0.0.0.0/0 at rule 200 (ingress)
#   - ALLOW SSH (22) from 0.0.0.0/0 at rule 300 (ingress)
#   - ALLOW ephemeral ports (1024-65535) from 0.0.0.0/0 at rule 400 (ingress)
#   - ALLOW all outbound at rule 100 (egress)
#   - Tag with Name = "${var.project_name}-public-nacl"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/network_acl
# Hint: DENY rules must have LOWER rule numbers than ALLOW rules to take effect


# =======================================================
# TODO 2 -- App Subnet NACL
# =======================================================
# Requirements:
#   - Create an aws_network_acl for the app subnet
#   - ALLOW TCP 8080 from 10.0.1.0/24 (public subnet) at rule 100 (ingress)
#   - ALLOW ephemeral ports (1024-65535) from 0.0.0.0/0 at rule 200 (ingress)
#   - ALLOW TCP to 0.0.0.0/0 on all ports at rule 100 (egress)
#   - ALLOW ephemeral ports (1024-65535) to 10.0.1.0/24 at rule 200 (egress)
#   - Tag with Name = "${var.project_name}-app-nacl"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/network_acl
# Hint: The app tier needs to accept requests from the web tier AND
#       return responses on ephemeral ports


# =======================================================
# TODO 3 -- Data Subnet NACL
# =======================================================
# Requirements:
#   - Create an aws_network_acl for the data subnet
#   - DENY all traffic from 10.0.1.0/24 (public subnet) at rule 50 (ingress)
#   - ALLOW TCP 3306 from 10.0.2.0/24 (app subnet) at rule 100 (ingress)
#   - ALLOW ephemeral ports to 10.0.2.0/24 at rule 100 (egress)
#   - DENY all other egress at rule 200 (egress)
#   - Tag with Name = "${var.project_name}-data-nacl"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/network_acl
# Hint: The data tier NACL adds an extra layer: even if someone
#       misconfigures the DB security group, the NACL blocks direct
#       access from the public subnet
```

### `outputs.tf`

```hcl
output "vpc_id" {
  description = "VPC ID"
  value       = aws_vpc.this.id
}

output "public_nacl_id" {
  description = "Public subnet NACL ID"
  value       = aws_network_acl.public.id
}

output "app_nacl_id" {
  description = "App subnet NACL ID"
  value       = aws_network_acl.app.id
}

output "data_nacl_id" {
  description = "Data subnet NACL ID"
  value       = aws_network_acl.data.id
}
```

## Spot the Bug

A colleague configured the public subnet NACL but HTTP requests from outside are timing out even though the security group allows port 80. **What is wrong?**

```hcl
resource "aws_network_acl" "public" {
  vpc_id     = aws_vpc.this.id
  subnet_ids = [aws_subnet.public.id]

  ingress {
    rule_no    = 100
    protocol   = "tcp"
    action     = "allow"
    cidr_block = "0.0.0.0/0"
    from_port  = 80
    to_port    = 80
  }

  egress {
    rule_no    = 100
    protocol   = "tcp"
    action     = "allow"
    cidr_block = "0.0.0.0/0"
    from_port  = 80
    to_port    = 80  # <-- BUG
  }

  tags = { Name = "public-nacl" }
}
```

<details>
<summary>Explain the bug</summary>

**The problem:** The egress rule only allows outbound traffic on port 80. When the web server responds to an HTTP request, the response goes back to the **client's ephemeral port** (a random port in the 1024-65535 range), not port 80. Since the NACL is stateless, it evaluates the outbound response packet independently and blocks it because port 1024-65535 is not allowed.

This is the most common NACL mistake: forgetting that response traffic uses ephemeral ports, not the original destination port.

**The fix:** Allow outbound ephemeral ports so responses can reach clients:

```hcl
egress {
  rule_no    = 100
  protocol   = "tcp"
  action     = "allow"
  cidr_block = "0.0.0.0/0"
  from_port  = 1024
  to_port    = 65535
}
```

Or, more commonly, allow all outbound traffic:

```hcl
egress {
  rule_no    = 100
  protocol   = "-1"
  action     = "allow"
  cidr_block = "0.0.0.0/0"
  from_port  = 0
  to_port    = 0
}
```

</details>

## Solutions

<details>
<summary>TODO 1 -- Public Subnet NACL (nacl.tf)</summary>

```hcl
resource "aws_network_acl" "public" {
  vpc_id     = aws_vpc.this.id
  subnet_ids = [aws_subnet.public.id]

  # DENY blocked CIDR first (lowest rule number = evaluated first)
  ingress {
    rule_no    = 100
    protocol   = "-1"
    action     = "deny"
    cidr_block = var.blocked_cidr
    from_port  = 0
    to_port    = 0
  }

  # ALLOW HTTP
  ingress {
    rule_no    = 200
    protocol   = "tcp"
    action     = "allow"
    cidr_block = "0.0.0.0/0"
    from_port  = 80
    to_port    = 80
  }

  # ALLOW SSH
  ingress {
    rule_no    = 300
    protocol   = "tcp"
    action     = "allow"
    cidr_block = "0.0.0.0/0"
    from_port  = 22
    to_port    = 22
  }

  # ALLOW ephemeral ports for return traffic
  ingress {
    rule_no    = 400
    protocol   = "tcp"
    action     = "allow"
    cidr_block = "0.0.0.0/0"
    from_port  = 1024
    to_port    = 65535
  }

  # ALLOW all outbound
  egress {
    rule_no    = 100
    protocol   = "-1"
    action     = "allow"
    cidr_block = "0.0.0.0/0"
    from_port  = 0
    to_port    = 0
  }

  tags = { Name = "${var.project_name}-public-nacl" }
}
```

</details>

<details>
<summary>TODO 2 -- App Subnet NACL (nacl.tf)</summary>

```hcl
resource "aws_network_acl" "app" {
  vpc_id     = aws_vpc.this.id
  subnet_ids = [aws_subnet.app.id]

  # ALLOW port 8080 from public subnet
  ingress {
    rule_no    = 100
    protocol   = "tcp"
    action     = "allow"
    cidr_block = "10.0.1.0/24"
    from_port  = 8080
    to_port    = 8080
  }

  # ALLOW ephemeral ports for return traffic (NAT, etc.)
  ingress {
    rule_no    = 200
    protocol   = "tcp"
    action     = "allow"
    cidr_block = "0.0.0.0/0"
    from_port  = 1024
    to_port    = 65535
  }

  # ALLOW all TCP outbound (for NAT, DB calls, etc.)
  egress {
    rule_no    = 100
    protocol   = "tcp"
    action     = "allow"
    cidr_block = "0.0.0.0/0"
    from_port  = 1
    to_port    = 65535
  }

  # ALLOW ephemeral port responses to public subnet
  egress {
    rule_no    = 200
    protocol   = "tcp"
    action     = "allow"
    cidr_block = "10.0.1.0/24"
    from_port  = 1024
    to_port    = 65535
  }

  tags = { Name = "${var.project_name}-app-nacl" }
}
```

</details>

<details>
<summary>TODO 3 -- Data Subnet NACL (nacl.tf)</summary>

```hcl
resource "aws_network_acl" "data" {
  vpc_id     = aws_vpc.this.id
  subnet_ids = [aws_subnet.data.id]

  # DENY direct access from public subnet (defense in depth)
  ingress {
    rule_no    = 50
    protocol   = "-1"
    action     = "deny"
    cidr_block = "10.0.1.0/24"
    from_port  = 0
    to_port    = 0
  }

  # ALLOW MySQL from app subnet only
  ingress {
    rule_no    = 100
    protocol   = "tcp"
    action     = "allow"
    cidr_block = "10.0.2.0/24"
    from_port  = 3306
    to_port    = 3306
  }

  # ALLOW ephemeral port responses to app subnet
  egress {
    rule_no    = 100
    protocol   = "tcp"
    action     = "allow"
    cidr_block = "10.0.2.0/24"
    from_port  = 1024
    to_port    = 65535
  }

  # DENY all other egress
  egress {
    rule_no    = 200
    protocol   = "-1"
    action     = "deny"
    cidr_block = "0.0.0.0/0"
    from_port  = 0
    to_port    = 0
  }

  tags = { Name = "${var.project_name}-data-nacl" }
}
```

</details>

## Verify What You Learned

### Step 1 -- Confirm NACLs are created

```bash
aws ec2 describe-network-acls \
  --filters "Name=vpc-id,Values=$(terraform output -raw vpc_id)" "Name=default,Values=false" \
  --query "NetworkAcls[].{Name:Tags[?Key=='Name']|[0].Value,ID:NetworkAclId,SubnetCount:length(Associations)}" \
  --output table
```

Expected:

```
--------------------------------------------------------------
|                   DescribeNetworkAcls                      |
+-------------------+-----------------+----------------------+
|        ID         |      Name       |    SubnetCount       |
+-------------------+-----------------+----------------------+
|  acl-0abc...      | nacl-sec..public |        1            |
|  acl-0def...      | nacl-sec..app   |        1            |
|  acl-0ghi...      | nacl-sec..data  |        1            |
+-------------------+-----------------+----------------------+
```

### Step 2 -- Verify the public NACL deny rule exists

```bash
aws ec2 describe-network-acls \
  --filters "Name=tag:Name,Values=${var.project_name:-nacl-security-lab}-public-nacl" \
  --query "NetworkAcls[0].Entries[?RuleAction=='deny' && !Egress].{RuleNo:RuleNumber,CIDR:CidrBlock,Action:RuleAction}" \
  --output table
```

Expected:

```
-------------------------------------------
|          DescribeNetworkAcls            |
+----------+-----------------+------------+
|  Action  |      CIDR       |  RuleNo    |
+----------+-----------------+------------+
|  deny    | 198.51.100.0/24 |  100       |
|  deny    | 0.0.0.0/0       |  32767     |
+----------+-----------------+------------+
```

Rule 100 is our explicit deny; rule 32767 is the implicit deny-all that exists on every NACL.

### Step 3 -- Verify the data NACL blocks the public subnet

```bash
aws ec2 describe-network-acls \
  --filters "Name=tag:Name,Values=${var.project_name:-nacl-security-lab}-data-nacl" \
  --query "NetworkAcls[0].Entries[?RuleAction=='deny' && !Egress].{RuleNo:RuleNumber,CIDR:CidrBlock}" \
  --output table
```

Expected: a deny rule for `10.0.1.0/24` at rule 50.

### Step 4 -- Count total NACL rules

```bash
aws ec2 describe-network-acls \
  --filters "Name=vpc-id,Values=$(terraform output -raw vpc_id)" "Name=default,Values=false" \
  --query "NetworkAcls[].{Name:Tags[?Key=='Name']|[0].Value,IngressRules:length(Entries[?!Egress]),EgressRules:length(Entries[?Egress])}" \
  --output table
```

Expected: rule counts for each NACL (including the implicit deny-all rules that AWS adds automatically).

### Step 5 -- Verify no changes pending

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

Destroy all resources to stop incurring charges:

```bash
terraform destroy -auto-approve
```

Verify nothing remains:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

In **Exercise 21 -- Private DNS with Route 53 Hosted Zones**, you will create private hosted zones that override public DNS within your VPC. This enables split-horizon DNS -- where the same hostname resolves to different IPs depending on whether you are inside or outside the VPC.

## Summary

- **NACLs are stateless** -- every packet (including return traffic) is evaluated independently
- **Rule ordering matters** -- rules are evaluated from lowest number to highest; first match wins
- **DENY rules must come before ALLOW rules** (lower rule numbers) to take effect
- **Ephemeral ports (1024-65535)** must be explicitly allowed for return traffic in both ingress and egress
- **Defense in depth**: NACLs block traffic at the subnet perimeter even if security groups are misconfigured
- **Security groups + NACLs** together create two independent layers of network security

## Reference

| Resource | Purpose |
|----------|---------|
| `aws_network_acl` | Stateless subnet-level firewall |
| `aws_security_group` | Stateful instance-level firewall |
| `aws_vpc_security_group_ingress_rule` | SG inbound rule (separate resource) |
| `aws_vpc_security_group_egress_rule` | SG outbound rule (separate resource) |

## Additional Resources

- [Network ACLs (AWS Docs)](https://docs.aws.amazon.com/vpc/latest/userguide/vpc-network-acls.html) -- official documentation on NACL rules, evaluation order, and ephemeral ports
- [Security Groups vs NACLs Comparison](https://docs.aws.amazon.com/vpc/latest/userguide/VPC_Security.html) -- how the two layers work together
- [Ephemeral Port Ranges by OS](https://docs.aws.amazon.com/vpc/latest/userguide/vpc-network-acls.html#nacl-ephemeral-ports) -- port ranges used by different operating systems
- [Terraform aws_network_acl Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/network_acl) -- Terraform documentation for NACLs
- [Defense in Depth (AWS Whitepaper)](https://docs.aws.amazon.com/whitepapers/latest/aws-security-best-practices/network-security.html) -- layered security patterns for AWS networks

## Apply Your Knowledge

- [NACL Rule Limits and Workarounds](https://docs.aws.amazon.com/vpc/latest/userguide/amazon-vpc-limits.html#vpc-limits-nacls) -- understand the 20-rule-per-direction limit and strategies for working within it
- [AWS WAF vs NACLs for IP Blocking](https://docs.aws.amazon.com/waf/latest/developerguide/waf-rule-statement-type-ipset-match.html) -- when to use WAF IP sets instead of NACLs for large IP block lists
- [VPC Flow Logs for NACL Debugging](https://docs.aws.amazon.com/vpc/latest/userguide/flow-logs.html) -- use flow logs to identify which NACL rules are accepting or rejecting traffic

---

> *"Be conservative in what you send, be liberal in what you accept."*
> -- **Jon Postel**

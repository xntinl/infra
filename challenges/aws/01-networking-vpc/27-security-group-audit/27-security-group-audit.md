# 27. Security Group Audit

<!--
difficulty: intermediate
concepts: [security-group-audit, vpc-flow-logs, overly-permissive-rules, unused-sg, rule-consolidation]
tools: [terraform, aws-cli]
estimated_time: 50m
bloom_level: analyze
prerequisites: [03-security-groups-and-nacls, 26-security-group-chaining]
aws_cost: ~$0.02/hr
-->

> **AWS Cost Warning:** This exercise creates a VPC with flow logs to CloudWatch (~$0.50/GB ingested) and a t3.micro EC2 instance (~$0.0104/hr). Estimated total: ~$0.02/hr for short sessions. Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.5 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Exercise 03 completed | Understand security groups and NACLs |
| Exercise 26 completed | Understand SG chaining and prefix lists |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Detect** security groups with overly permissive rules (0.0.0.0/0 on sensitive ports)
2. **Identify** unused security groups that consume quota without serving any purpose
3. **Analyze** VPC Flow Logs to find rejected traffic patterns that indicate misconfigured rules
4. **Consolidate** redundant security group rules into cleaner, more maintainable configurations

## Why This Matters

Security groups accumulate cruft faster than any other VPC resource. A team adds a temporary rule for debugging, forgets to remove it, and suddenly port 22 is open to the world. Another team creates a security group for a service that was decommissioned months ago, but the SG remains, consuming one of the 2,500-per-region quota slots. A third team has 15 individual CIDR rules that could be a single prefix list reference.

Regular auditing catches these issues before they become vulnerabilities or quota exhaustion. VPC Flow Logs add a second dimension: by filtering for REJECT actions, you can identify which ports are being probed from outside and verify that your security groups are actually blocking that traffic. Combined with AWS CLI queries, you can build an audit script that runs in CI/CD and alerts on drift.

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
  default     = "sg-audit-lab"
}
```

### `vpc.tf`

```hcl
data "aws_availability_zones" "available" {
  state = "available"
}

locals {
  vpc_cidr = "10.50.0.0/16"
  azs      = slice(data.aws_availability_zones.available.names, 0, 2)
}

resource "aws_vpc" "this" {
  cidr_block           = local.vpc_cidr
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = "${var.project_name}-vpc" }
}

resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id

  tags = { Name = "${var.project_name}-igw" }
}

resource "aws_subnet" "public" {
  vpc_id                  = aws_vpc.this.id
  cidr_block              = cidrsubnet(local.vpc_cidr, 8, 0)
  availability_zone       = local.azs[0]
  map_public_ip_on_launch = true

  tags = { Name = "${var.project_name}-public" }
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
```

### `security-groups.tf`

```hcl
# ------------------------------------------------------------------
# INTENTIONALLY MISCONFIGURED SECURITY GROUPS
# These represent common anti-patterns you will audit and fix.
# ------------------------------------------------------------------

# =======================================================
# TODO 1 — Lock down the default security group
# =======================================================
# Requirements:
#   - Use aws_default_security_group to take ownership
#   - Remove all ingress and egress rules (empty body)
#   - Tag with Name = "${var.project_name}-default-sg-LOCKED"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/default_security_group
# Hint: aws_default_security_group with no ingress/egress blocks strips all rules


# ------------------------------------------------------------------
# MISCONFIGURED SG #1: SSH open to the world
# ------------------------------------------------------------------
resource "aws_security_group" "bad_ssh" {
  name        = "${var.project_name}-bad-ssh-sg"
  description = "AUDIT TARGET: SSH open to 0.0.0.0/0"
  vpc_id      = aws_vpc.this.id

  tags = { Name = "${var.project_name}-bad-ssh-sg" }
}

resource "aws_vpc_security_group_ingress_rule" "bad_ssh_in" {
  security_group_id = aws_security_group.bad_ssh.id
  cidr_ipv4         = "0.0.0.0/0"
  from_port         = 22
  to_port           = 22
  ip_protocol       = "tcp"
  description       = "SSH from anywhere - AUDIT FINDING"
}

resource "aws_vpc_security_group_egress_rule" "bad_ssh_out" {
  security_group_id = aws_security_group.bad_ssh.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}

# ------------------------------------------------------------------
# MISCONFIGURED SG #2: All ports open to the world
# ------------------------------------------------------------------
resource "aws_security_group" "bad_all_ports" {
  name        = "${var.project_name}-bad-all-ports-sg"
  description = "AUDIT TARGET: All ports open to 0.0.0.0/0"
  vpc_id      = aws_vpc.this.id

  tags = { Name = "${var.project_name}-bad-all-ports-sg" }
}

resource "aws_vpc_security_group_ingress_rule" "bad_all_ports_in" {
  security_group_id = aws_security_group.bad_all_ports.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
  description       = "All traffic from anywhere - CRITICAL AUDIT FINDING"
}

resource "aws_vpc_security_group_egress_rule" "bad_all_ports_out" {
  security_group_id = aws_security_group.bad_all_ports.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}

# ------------------------------------------------------------------
# MISCONFIGURED SG #3: RDS port open to the world
# ------------------------------------------------------------------
resource "aws_security_group" "bad_rds" {
  name        = "${var.project_name}-bad-rds-sg"
  description = "AUDIT TARGET: RDS port open to 0.0.0.0/0"
  vpc_id      = aws_vpc.this.id

  tags = { Name = "${var.project_name}-bad-rds-sg" }
}

resource "aws_vpc_security_group_ingress_rule" "bad_rds_in" {
  security_group_id = aws_security_group.bad_rds.id
  cidr_ipv4         = "0.0.0.0/0"
  from_port         = 3306
  to_port           = 3306
  ip_protocol       = "tcp"
  description       = "MySQL from anywhere - AUDIT FINDING"
}

resource "aws_vpc_security_group_egress_rule" "bad_rds_out" {
  security_group_id = aws_security_group.bad_rds.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}

# ------------------------------------------------------------------
# UNUSED SG: created for a service that was decommissioned
# ------------------------------------------------------------------
resource "aws_security_group" "unused_legacy" {
  name        = "${var.project_name}-legacy-unused-sg"
  description = "AUDIT TARGET: Unused SG - wasting quota"
  vpc_id      = aws_vpc.this.id

  tags = { Name = "${var.project_name}-legacy-unused-sg" }
}

resource "aws_vpc_security_group_ingress_rule" "unused_legacy_in" {
  security_group_id = aws_security_group.unused_legacy.id
  cidr_ipv4         = "10.50.0.0/16"
  from_port         = 6379
  to_port           = 6379
  ip_protocol       = "tcp"
  description       = "Redis from VPC"
}

resource "aws_vpc_security_group_egress_rule" "unused_legacy_out" {
  security_group_id = aws_security_group.unused_legacy.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}

# =======================================================
# TODO 2 — Create a properly configured web security group
# =======================================================
# Requirements:
#   - Name: "${var.project_name}-web-sg"
#   - Allow HTTP (80) and HTTPS (443) from 0.0.0.0/0 (these are public-facing)
#   - Allow SSH (22) only from 10.50.0.0/16 (VPC internal)
#   - Allow all egress to 0.0.0.0/0
#   - Use separate aws_vpc_security_group_ingress_rule resources
#   - Add description to every rule
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_security_group_ingress_rule
# Hint: Public-facing ports (80, 443) are OK with 0.0.0.0/0; management ports (22) are not


# =======================================================
# TODO 3 — Create a properly configured app security group
# =======================================================
# Requirements:
#   - Name: "${var.project_name}-app-sg"
#   - Allow port 8080 ONLY from the web security group (SG-to-SG reference)
#   - Allow all egress to 0.0.0.0/0
#   - Use referenced_security_group_id, not cidr_ipv4
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_security_group_ingress_rule
# Hint: referenced_security_group_id = aws_security_group.web.id
```

### `flow-logs.tf`

```hcl
# =======================================================
# TODO 4 — VPC Flow Logs to CloudWatch
# =======================================================
# Requirements:
#   - Create an aws_cloudwatch_log_group with 7-day retention
#     Name: "/vpc/${var.project_name}/flow-logs"
#   - Create an IAM role for vpc-flow-logs.amazonaws.com
#     Use data "aws_iam_policy_document" for both trust and permissions
#   - Create an aws_flow_log capturing REJECT traffic only
#     (to identify probed ports)
#
# Docs:
#   https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/flow_log
#   https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/iam_policy_document
# Hint: traffic_type = "REJECT" captures only denied connections
```

### `compute.tf`

```hcl
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

# =======================================================
# TODO 5 — Launch a test instance with the bad-ssh SG
# =======================================================
# Requirements:
#   - t3.micro in the public subnet
#   - Attach the bad_ssh security group
#   - Tag with Name = "${var.project_name}-test-instance"
#   - This instance demonstrates the audit finding:
#     SSH is open to the world
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/instance
# Hint: vpc_security_group_ids = [aws_security_group.bad_ssh.id]
```

### `audit-queries.tf`

```hcl
# =======================================================
# TODO 6 — Output audit queries
# =======================================================
# Requirements:
#   - Output the VPC ID
#   - Output a list of all SG IDs in this VPC
#   - Output an "audit_commands" map with ready-to-run CLI commands:
#     * "find_open_ssh" - AWS CLI to find SGs with 0.0.0.0/0 on port 22
#     * "find_unused_sgs" - AWS CLI to find SGs not attached to any ENI
#     * "find_all_ports_open" - AWS CLI to find SGs allowing all protocols
#
# Hint: Use templatestring or simple string interpolation
#       to include the VPC ID in the CLI commands
```

> **Best Practice:** Run security group audit queries on a schedule -- weekly at minimum, daily for production accounts. Unused SGs consume quota (limit: 2,500 per region by default). Overly permissive rules are the #1 finding in AWS security assessments. Automate these checks in your CI/CD pipeline using AWS Config rules or custom scripts.

## Spot the Bug

A colleague wrote this flow log configuration but no rejected traffic appears in CloudWatch. **What is wrong?**

```hcl
resource "aws_flow_log" "this" {
  vpc_id               = aws_vpc.this.id
  traffic_type         = "ALL"
  log_destination_type = "cloud-watch-logs"
  log_destination      = aws_cloudwatch_log_group.flow_logs.arn
  # iam_role_arn is missing!
}
```

<details>
<summary>Explain the bug</summary>

The `iam_role_arn` argument is **required** when `log_destination_type` is `"cloud-watch-logs"`. Without it, the VPC Flow Logs service cannot write to the CloudWatch log group. The `terraform apply` will actually succeed (the flow log is created), but its status will be `ACTIVE` with no data flowing -- the logs silently fail to deliver because the service has no permission to write.

The fix is to create an IAM role with a trust policy for `vpc-flow-logs.amazonaws.com` and a permissions policy granting CloudWatch Logs write access:

```hcl
resource "aws_flow_log" "this" {
  vpc_id               = aws_vpc.this.id
  traffic_type         = "ALL"
  log_destination_type = "cloud-watch-logs"
  log_destination      = aws_cloudwatch_log_group.flow_logs.arn
  iam_role_arn         = aws_iam_role.flow_logs.arn
}
```

Note: `iam_role_arn` is NOT required when logging to S3 (`log_destination_type = "s3"`), because S3 uses a bucket policy instead. This inconsistency is a common source of confusion.

</details>

## Solutions

<details>
<summary>TODO 1 -- Lock down the default security group</summary>

```hcl
resource "aws_default_security_group" "lockdown" {
  vpc_id = aws_vpc.this.id

  # No ingress or egress blocks = all rules removed
  tags = { Name = "${var.project_name}-default-sg-LOCKED" }
}
```

</details>

<details>
<summary>TODO 2 -- Properly configured web security group</summary>

```hcl
resource "aws_security_group" "web" {
  name        = "${var.project_name}-web-sg"
  description = "Web tier - HTTP/HTTPS public, SSH internal only"
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

resource "aws_vpc_security_group_ingress_rule" "web_ssh_internal" {
  security_group_id = aws_security_group.web.id
  cidr_ipv4         = "10.50.0.0/16"
  from_port         = 22
  to_port           = 22
  ip_protocol       = "tcp"
  description       = "SSH from VPC only"
}

resource "aws_vpc_security_group_egress_rule" "web_all_out" {
  security_group_id = aws_security_group.web.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
  description       = "All outbound traffic"
}
```

</details>

<details>
<summary>TODO 3 -- Properly configured app security group</summary>

```hcl
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
  description                  = "App port from web tier SG only"
}

resource "aws_vpc_security_group_egress_rule" "app_all_out" {
  security_group_id = aws_security_group.app.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
  description       = "All outbound traffic"
}
```

</details>

<details>
<summary>TODO 4 -- VPC Flow Logs with REJECT filter</summary>

```hcl
resource "aws_cloudwatch_log_group" "flow_logs" {
  name              = "/vpc/${var.project_name}/flow-logs"
  retention_in_days = 7

  tags = { Name = "${var.project_name}-flow-logs" }
}

data "aws_iam_policy_document" "flow_logs_trust" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["vpc-flow-logs.amazonaws.com"]
    }
  }
}

data "aws_iam_policy_document" "flow_logs_permissions" {
  statement {
    effect = "Allow"

    actions = [
      "logs:CreateLogGroup",
      "logs:CreateLogStream",
      "logs:PutLogEvents",
      "logs:DescribeLogGroups",
      "logs:DescribeLogStreams",
    ]

    resources = ["*"]
  }
}

resource "aws_iam_role" "flow_logs" {
  name               = "${var.project_name}-flow-logs-role"
  assume_role_policy = data.aws_iam_policy_document.flow_logs_trust.json

  tags = { Name = "${var.project_name}-flow-logs-role" }
}

resource "aws_iam_role_policy" "flow_logs" {
  name   = "${var.project_name}-flow-logs-policy"
  role   = aws_iam_role.flow_logs.id
  policy = data.aws_iam_policy_document.flow_logs_permissions.json
}

resource "aws_flow_log" "this" {
  vpc_id               = aws_vpc.this.id
  traffic_type         = "REJECT"
  log_destination_type = "cloud-watch-logs"
  log_destination      = aws_cloudwatch_log_group.flow_logs.arn
  iam_role_arn         = aws_iam_role.flow_logs.arn

  tags = { Name = "${var.project_name}-flow-log" }
}
```

</details>

<details>
<summary>TODO 5 -- Test instance with bad-ssh SG</summary>

```hcl
resource "aws_instance" "test" {
  ami                    = data.aws_ami.amazon_linux_2023.id
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.public.id
  vpc_security_group_ids = [aws_security_group.bad_ssh.id]

  tags = { Name = "${var.project_name}-test-instance" }
}
```

</details>

<details>
<summary>TODO 6 -- Audit query outputs</summary>

```hcl
output "vpc_id" {
  description = "VPC ID"
  value       = aws_vpc.this.id
}

output "all_sg_ids" {
  description = "All security group IDs in this VPC"
  value = [
    aws_security_group.bad_ssh.id,
    aws_security_group.bad_all_ports.id,
    aws_security_group.bad_rds.id,
    aws_security_group.unused_legacy.id,
    aws_security_group.web.id,
    aws_security_group.app.id,
  ]
}

output "audit_commands" {
  description = "Ready-to-run AWS CLI audit commands"
  value = {
    find_open_ssh = join(" ", [
      "aws ec2 describe-security-group-rules",
      "--filters \"Name=group-id,Values=${aws_security_group.bad_ssh.id}\"",
      "--query \"SecurityGroupRules[?CidrIpv4=='0.0.0.0/0' && FromPort==\\`22\\`]\"",
      "--output table"
    ])
    find_unused_sgs = join(" ", [
      "aws ec2 describe-network-interfaces",
      "--filters \"Name=vpc-id,Values=${aws_vpc.this.id}\"",
      "--query \"NetworkInterfaces[].Groups[].GroupId\"",
      "--output text"
    ])
    find_all_ports_open = join(" ", [
      "aws ec2 describe-security-group-rules",
      "--filters \"Name=group-id,Values=${aws_security_group.bad_all_ports.id}\"",
      "--query \"SecurityGroupRules[?IpProtocol=='-1' && CidrIpv4=='0.0.0.0/0']\"",
      "--output table"
    ])
  }
}
```

</details>

## Verify What You Learned

### Find SGs with SSH open to the world

```bash
aws ec2 describe-security-groups \
  --filters "Name=vpc-id,Values=$(terraform output -raw vpc_id)" \
  --query "SecurityGroups[?IpPermissions[?FromPort==\`22\` && IpRanges[?CidrIp=='0.0.0.0/0']]].{Name:GroupName,ID:GroupId}" \
  --output table
```

Expected:

```
---------------------------------------------------------
|               DescribeSecurityGroups                  |
+-------------------------------+-----------------------+
|             Name              |          ID           |
+-------------------------------+-----------------------+
|  sg-audit-lab-bad-ssh-sg      |  sg-0abc123...        |
+-------------------------------+-----------------------+
```

### Find SGs with all ports open

```bash
aws ec2 describe-security-groups \
  --filters "Name=vpc-id,Values=$(terraform output -raw vpc_id)" \
  --query "SecurityGroups[?IpPermissions[?IpProtocol=='-1' && IpRanges[?CidrIp=='0.0.0.0/0']]].{Name:GroupName,ID:GroupId}" \
  --output table
```

Expected:

```
---------------------------------------------------------
|               DescribeSecurityGroups                  |
+-------------------------------+-----------------------+
|             Name              |          ID           |
+-------------------------------+-----------------------+
|  sg-audit-lab-bad-all-ports-sg|  sg-0def456...        |
+-------------------------------+-----------------------+
```

### Find unused SGs (not attached to any ENI)

```bash
aws ec2 describe-network-interfaces \
  --filters "Name=vpc-id,Values=$(terraform output -raw vpc_id)" \
  --query "NetworkInterfaces[].Groups[].GroupId" \
  --output text | tr '\t' '\n' | sort -u
```

Compare this list with all SGs in the VPC. Any SG not in this list is unused.

```bash
aws ec2 describe-security-groups \
  --filters "Name=vpc-id,Values=$(terraform output -raw vpc_id)" \
  --query "SecurityGroups[].GroupId" \
  --output text | tr '\t' '\n' | sort -u
```

Expected: the unused legacy SG (`sg-audit-lab-legacy-unused-sg`) will not appear in the ENI list.

### Verify flow logs are capturing REJECT traffic

```bash
aws ec2 describe-flow-logs \
  --filter "Name=resource-id,Values=$(terraform output -raw vpc_id)" \
  --query "FlowLogs[].{Status:FlowLogStatus,Traffic:TrafficType,Dest:LogDestinationType}" \
  --output table
```

Expected:

```
-----------------------------------------------------
|                DescribeFlowLogs                   |
+------------------+-----------+--------------------+
|       Dest       |  Status   |     Traffic        |
+------------------+-----------+--------------------+
|  cloud-watch-logs|  ACTIVE   |  REJECT            |
+------------------+-----------+--------------------+
```

### Verify the default SG is locked down

```bash
aws ec2 describe-security-group-rules \
  --filters "Name=group-id,Values=$(aws ec2 describe-security-groups --filters 'Name=vpc-id,Values='$(terraform output -raw vpc_id) 'Name=group-name,Values=default' --query 'SecurityGroups[0].GroupId' --output text)" \
  --query "SecurityGroupRules" \
  --output table
```

Expected: empty table (no rules).

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

You now know how to audit security groups for overly permissive rules, find unused SGs, and use VPC Flow Logs to detect probed ports. In the next exercise, **28 -- Multi-VPC Isolated Routing**, you will create multiple VPCs with non-overlapping CIDRs and isolated route tables -- the foundation for a multi-environment architecture that can later be connected via peering or Transit Gateway.

## Summary

- **Overly permissive SGs** (0.0.0.0/0 on SSH, RDS, or all ports) are the #1 finding in AWS security assessments
- **Unused SGs** waste quota (limit: 2,500 per region) and should be cleaned up regularly
- **VPC Flow Logs with REJECT filter** identify which ports are being probed and confirm SGs are blocking correctly
- **Default SG lockdown** with `aws_default_security_group` satisfies CIS Benchmark 5.4
- **SG-to-SG references** between tiers are more secure and maintainable than CIDR-based rules
- **`data "aws_iam_policy_document"`** is the correct pattern for IAM policies in Terraform (not `jsonencode()`)

## Reference

| Resource | Purpose |
|----------|---------|
| `aws_security_group` | Virtual firewall at the instance level |
| `aws_default_security_group` | Manages the VPC's default SG |
| `aws_vpc_security_group_ingress_rule` | Inbound rule as separate resource |
| `aws_vpc_security_group_egress_rule` | Outbound rule as separate resource |
| `aws_flow_log` | VPC traffic logging |
| `aws_cloudwatch_log_group` | Flow log destination |
| `data "aws_iam_policy_document"` | IAM policy builder |

## Additional Resources

- [Security Group Rules Reference (AWS Docs)](https://docs.aws.amazon.com/vpc/latest/userguide/security-group-rules.html) -- complete reference for security group rule types and limits
- [VPC Flow Logs (AWS Docs)](https://docs.aws.amazon.com/vpc/latest/userguide/flow-logs.html) -- how to create, configure, and query flow logs
- [CIS AWS Foundations Benchmark](https://www.cisecurity.org/benchmark/amazon_web_services) -- security controls including SG requirements
- [Terraform aws_default_security_group](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/default_security_group) -- managing the default SG in Terraform
- [AWS Config Rules for Security Groups](https://docs.aws.amazon.com/config/latest/developerguide/operational-best-practices-for-nist-csf.html) -- automated compliance checks for SG configurations

## Apply Your Knowledge

- [AWS Security Hub Controls](https://docs.aws.amazon.com/securityhub/latest/userguide/ec2-controls.html) -- automated findings for misconfigured security groups
- [CloudWatch Logs Insights Query Syntax](https://docs.aws.amazon.com/AmazonCloudWatch/latest/logs/CWL_QuerySyntax.html) -- querying flow logs for rejected traffic patterns
- [AWS re:Invent: Securing Your VPC](https://www.youtube.com/watch?v=5_7xn5bTd2k) -- advanced security group audit and hardening techniques

---

> *"Trust, but verify."*
> — **Ronald Reagan**

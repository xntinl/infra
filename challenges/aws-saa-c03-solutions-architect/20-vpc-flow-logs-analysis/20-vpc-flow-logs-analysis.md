# 20. VPC Flow Logs and Traffic Analysis

<!--
difficulty: basic
concepts: [vpc-flow-logs, cloudwatch-logs, s3-flow-logs, accept-reject-records, traffic-analysis, iam-flow-log-role, network-troubleshooting]
tools: [terraform, aws-cli]
estimated_time: 30m
bloom_level: understand
prerequisites: [17-vpc-subnets-route-tables-igw, 19-security-groups-vs-nacls]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** VPC Flow Logs themselves are free to enable. You pay for CloudWatch Logs ingestion ($0.50/GB) and S3 storage ($0.023/GB). For this exercise with minimal traffic, costs are negligible (~$0.01/hr). Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Understanding of security groups and NACLs (exercise 19)

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** what VPC Flow Logs capture (metadata about IP traffic, not packet contents) and the three attachment levels (VPC, subnet, ENI)
- **Construct** VPC Flow Logs that publish to both CloudWatch Logs and S3 using Terraform
- **Interpret** Flow Log records to distinguish ACCEPT from REJECT traffic and identify the source of blocked connections
- **Identify** common IAM permission issues that prevent Flow Logs from publishing to CloudWatch Logs
- **Analyze** Flow Log data to troubleshoot network connectivity issues caused by security group or NACL misconfigurations
- **Compare** CloudWatch Logs (real-time queries) vs S3 (long-term storage, Athena analysis) as Flow Log destinations

## Why VPC Flow Logs Matter

VPC Flow Logs capture metadata about IP traffic flowing through your network interfaces. They do not capture packet contents (that requires packet capture tools), but they record source/destination IPs, ports, protocol, number of packets/bytes, and whether the traffic was accepted or rejected. This makes Flow Logs the primary tool for diagnosing network connectivity issues, detecting unusual traffic patterns, and auditing security group and NACL effectiveness.

On the SAA-C03 exam, Flow Logs appear in two contexts. First, troubleshooting: "An EC2 instance cannot reach the internet. What should the architect do to diagnose the issue?" -- enable VPC Flow Logs and look for REJECT records. Second, compliance: "The company must audit all network traffic for PCI-DSS compliance" -- VPC Flow Logs to S3 with Athena queries provide the audit trail. The key distinction from the exam perspective is that Flow Logs show you whether traffic was accepted or rejected, but they do not tell you which specific rule caused the rejection. To determine whether the rejection came from a security group or a NACL, you must analyze the flow log record fields and cross-reference with your rules.

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
  default     = "flow-logs-demo"
}
```

### `vpc.tf`

```hcl
data "aws_caller_identity" "current" {}

resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags                 = { Name = var.project_name }
}

resource "aws_subnet" "public" {
  vpc_id                  = aws_vpc.this.id
  cidr_block              = "10.0.1.0/24"
  map_public_ip_on_launch = true
  tags                    = { Name = "${var.project_name}-public" }
}

resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id
  tags   = { Name = "${var.project_name}-igw" }
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

### `security.tf`

```hcl
# Security group that allows SSH only -- HTTP will be REJECTED
resource "aws_security_group" "demo" {
  name_prefix = "${var.project_name}-"
  vpc_id      = aws_vpc.this.id
  description = "Allow SSH only, reject everything else"

  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "SSH"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "${var.project_name}-sg" }
}
```

### `iam.tf`

```hcl
# ------------------------------------------------------------------
# IAM Role for VPC Flow Logs to publish to CloudWatch Logs.
#
# Flow Logs need permission to create log streams and put log events.
# Without this role, Flow Logs fail silently -- no error in the
# console, no data in CloudWatch. This is a very common mistake.
# ------------------------------------------------------------------
data "aws_iam_policy_document" "flow_log_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["vpc-flow-logs.amazonaws.com"]
    }
  }
}

data "aws_iam_policy_document" "flow_log_publish" {
  statement {
    actions = [
      "logs:CreateLogGroup",
      "logs:CreateLogStream",
      "logs:PutLogEvents",
      "logs:DescribeLogGroups",
      "logs:DescribeLogStreams"
    ]
    resources = ["*"]
  }
}

resource "aws_iam_role" "flow_log" {
  name               = "${var.project_name}-role"
  assume_role_policy = data.aws_iam_policy_document.flow_log_assume.json
}

resource "aws_iam_role_policy" "flow_log" {
  name   = "flow-logs-publish"
  role   = aws_iam_role.flow_log.id
  policy = data.aws_iam_policy_document.flow_log_publish.json
}
```

### `monitoring.tf`

```hcl
# ------------------------------------------------------------------
# CloudWatch Log Group: destination for real-time Flow Log data.
#
# Retention of 7 days keeps costs low for this exercise.
# In production, use 30-90 days for operational visibility and
# archive to S3 for long-term compliance storage.
# ------------------------------------------------------------------
resource "aws_cloudwatch_log_group" "flow_logs" {
  name              = "/vpc/${var.project_name}"
  retention_in_days = 7
  tags              = { Name = var.project_name }
}

# ------------------------------------------------------------------
# VPC Flow Log: captures ALL traffic (ACCEPT + REJECT) at the VPC level.
#
# traffic_type options:
# - "ALL": captures both accepted and rejected traffic
# - "ACCEPT": only traffic allowed by SGs and NACLs
# - "REJECT": only traffic blocked by SGs or NACLs
#
# For troubleshooting, use ALL. For cost optimization with high
# traffic volumes, consider REJECT only (shows problems, not normal flow).
# ------------------------------------------------------------------
resource "aws_flow_log" "cloudwatch" {
  vpc_id                   = aws_vpc.this.id
  traffic_type             = "ALL"
  log_destination_type     = "cloud-watch-logs"
  log_destination          = aws_cloudwatch_log_group.flow_logs.arn
  iam_role_arn             = aws_iam_role.flow_log.arn
  max_aggregation_interval = 60

  tags = { Name = "${var.project_name}-cloudwatch" }
}
```

### `storage.tf`

```hcl
# ------------------------------------------------------------------
# S3 Bucket: long-term storage for Flow Log data.
#
# S3 is cheaper than CloudWatch Logs for storage and supports
# Athena queries for large-scale analysis. Flow Logs publish
# to S3 in a structured path: s3://bucket/AWSLogs/account-id/
# vpcflowlogs/region/year/month/day/
# ------------------------------------------------------------------
resource "aws_s3_bucket" "flow_logs" {
  bucket_prefix = "${var.project_name}-"
  force_destroy = true
  tags          = { Name = "${var.project_name}-s3" }
}

resource "aws_flow_log" "s3" {
  vpc_id                   = aws_vpc.this.id
  traffic_type             = "ALL"
  log_destination_type     = "s3"
  log_destination          = aws_s3_bucket.flow_logs.arn
  max_aggregation_interval = 60

  tags = { Name = "${var.project_name}-s3" }
}
```

### `outputs.tf`

```hcl
output "vpc_id" {
  value = aws_vpc.this.id
}

output "log_group_name" {
  description = "CloudWatch Log Group for Flow Logs"
  value       = aws_cloudwatch_log_group.flow_logs.name
}

output "s3_bucket_name" {
  description = "S3 bucket for Flow Logs"
  value       = aws_s3_bucket.flow_logs.id
}

output "flow_log_cloudwatch_id" {
  value = aws_flow_log.cloudwatch.id
}

output "flow_log_s3_id" {
  value = aws_flow_log.s3.id
}
```

## Step 2 -- Deploy and Analyze

```bash
terraform init
terraform apply -auto-approve
```

### Understanding Flow Log Records

A Flow Log record looks like this:

```
2 123456789012 eni-abc123 10.0.1.52 198.51.100.7 443 49152 6 25 20000 1620140661 1620140721 ACCEPT OK
```

| Field | Value | Meaning |
|-------|-------|---------|
| version | 2 | Flow log version |
| account-id | 123456789012 | AWS account |
| interface-id | eni-abc123 | Network interface |
| srcaddr | 10.0.1.52 | Source IP |
| dstaddr | 198.51.100.7 | Destination IP |
| srcport | 443 | Source port |
| dstport | 49152 | Destination port |
| protocol | 6 | TCP (6), UDP (17), ICMP (1) |
| packets | 25 | Number of packets |
| bytes | 20000 | Number of bytes |
| start | 1620140661 | Start time (Unix epoch) |
| end | 1620140721 | End time (Unix epoch) |
| action | ACCEPT | ACCEPT or REJECT |
| log-status | OK | OK, NODATA, or SKIPDATA |

### Query Flow Logs in CloudWatch

Wait 2-3 minutes for Flow Logs to start publishing, then query:

```bash
# View recent flow log entries
aws logs filter-log-events \
  --log-group-name "/vpc/flow-logs-demo" \
  --start-time $(date -v-5M +%s000) \
  --query "events[*].message" \
  --output text | head -20

# Find REJECTED traffic (blocked by SG or NACL)
aws logs filter-log-events \
  --log-group-name "/vpc/flow-logs-demo" \
  --filter-pattern "REJECT" \
  --query "events[*].message" \
  --output text | head -10
```

### Destination Comparison

| Feature | CloudWatch Logs | S3 |
|---------|----------------|-----|
| **Latency** | Near real-time (~1 min) | 5-10 minutes |
| **Query tool** | CloudWatch Insights | Athena |
| **Cost (storage)** | $0.03/GB/month | $0.023/GB/month |
| **Cost (ingestion)** | $0.50/GB | Free |
| **Best for** | Real-time troubleshooting | Long-term compliance |
| **Retention** | Configurable (1 day-10 years) | Lifecycle rules |

## Common Mistakes

### 1. Flow Log IAM role missing permissions

**Wrong approach:** Creating a Flow Log without the IAM role or with insufficient permissions:

```hcl
resource "aws_flow_log" "this" {
  vpc_id               = aws_vpc.this.id
  traffic_type         = "ALL"
  log_destination_type = "cloud-watch-logs"
  log_destination      = aws_cloudwatch_log_group.flow_logs.arn
  # Missing: iam_role_arn
}
```

**What happens:** For CloudWatch Logs destinations, the `iam_role_arn` is required. Without it, Terraform apply fails. If you provide a role but it lacks `logs:CreateLogStream` or `logs:PutLogEvents`, the Flow Log is created but no data appears in CloudWatch -- the failure is silent.

**Fix:** Always create a dedicated IAM role with the required permissions and verify data appears within 5 minutes of creation.

### 2. Thinking Flow Logs capture packet contents

**Wrong approach:** Enabling Flow Logs expecting to see HTTP headers, request bodies, or DNS query content.

**What happens:** Flow Logs capture only metadata: source/destination IPs, ports, protocol, action (ACCEPT/REJECT), packet count, and byte count. They never capture packet payloads. For packet inspection, you need VPC Traffic Mirroring or a network appliance.

**Fix:** Use Flow Logs for connectivity troubleshooting and traffic analysis. Use VPC Traffic Mirroring or AWS Network Firewall for deep packet inspection.

### 3. Not accounting for aggregation interval

**Wrong approach:** Expecting to see individual packets in Flow Logs.

**What happens:** Flow Logs aggregate traffic over the configured interval (60 seconds or 10 minutes). A record represents all packets matching the same 5-tuple (source IP, destination IP, source port, destination port, protocol) within that interval. High-traffic interfaces may show large packet/byte counts per record.

**Fix:** Use `max_aggregation_interval = 60` for the finest granularity. Remember that even at 60 seconds, you see aggregated records, not individual packets.

## Verify What You Learned

```bash
# Verify flow logs are active
aws ec2 describe-flow-logs \
  --filter "Name=resource-id,Values=$(terraform output -raw vpc_id)" \
  --query "FlowLogs[*].{Id:FlowLogId,Type:LogDestinationType,Status:FlowLogStatus}" \
  --output table
```

Expected: 2 flow logs (cloud-watch-logs and s3), both with Status=ACTIVE.

```bash
# Verify CloudWatch Log Group exists
aws logs describe-log-groups \
  --log-group-name-prefix "/vpc/flow-logs-demo" \
  --query "logGroups[0].logGroupName" \
  --output text
```

Expected: `/vpc/flow-logs-demo`

```bash
# Verify S3 bucket exists
aws s3 ls | grep flow-logs-demo
```

Expected: bucket name starting with `flow-logs-demo-`.

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

You enabled VPC Flow Logs to monitor and troubleshoot network traffic. In the next exercise, you will create **VPC Peering** between two VPCs, configure bidirectional routing, and integrate Route 53 private hosted zones for DNS resolution across peered VPCs -- the foundation for multi-VPC architectures.

## Summary

- **VPC Flow Logs** capture IP traffic metadata (not packet contents) at VPC, subnet, or ENI level
- Records show source/destination IPs, ports, protocol, action (**ACCEPT** or **REJECT**), and byte/packet counts
- **CloudWatch Logs** destination enables real-time queries; **S3** destination enables Athena analysis and long-term storage
- Flow Logs to CloudWatch require an **IAM role** with logs:CreateLogStream and logs:PutLogEvents permissions
- Flow Logs to S3 do **not** require an IAM role (S3 bucket policy is sufficient)
- **traffic_type** can be ALL, ACCEPT, or REJECT -- use REJECT for cost-effective troubleshooting
- Records are **aggregated** over 60 seconds or 10 minutes; they are not per-packet
- Flow Logs cannot determine **which rule** caused a REJECT -- only that the traffic was rejected

## Reference

- [VPC Flow Logs](https://docs.aws.amazon.com/vpc/latest/userguide/flow-logs.html)
- [Flow Log Record Fields](https://docs.aws.amazon.com/vpc/latest/userguide/flow-logs.html#flow-logs-fields)
- [Publishing Flow Logs to CloudWatch Logs](https://docs.aws.amazon.com/vpc/latest/userguide/flow-logs-cwl.html)
- [Publishing Flow Logs to S3](https://docs.aws.amazon.com/vpc/latest/userguide/flow-logs-s3.html)
- [Terraform aws_flow_log Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/flow_log)

## Additional Resources

- [Querying Flow Logs with Athena](https://docs.aws.amazon.com/vpc/latest/userguide/flow-logs-athena.html) -- creating Athena tables and running SQL queries on Flow Log data stored in S3
- [CloudWatch Logs Insights Queries for Flow Logs](https://docs.aws.amazon.com/AmazonCloudWatch/latest/logs/CWL_AnalyzeLogData-discoverable-fields.html) -- sample queries for top talkers, rejected traffic, and protocol distribution
- [VPC Traffic Mirroring](https://docs.aws.amazon.com/vpc/latest/mirroring/what-is-traffic-mirroring.html) -- for full packet capture when Flow Logs metadata is insufficient
- [Flow Log Pricing](https://aws.amazon.com/cloudwatch/pricing/) -- CloudWatch Logs ingestion and storage costs by region

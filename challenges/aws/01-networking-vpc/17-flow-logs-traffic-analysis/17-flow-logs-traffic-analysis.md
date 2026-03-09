# 17. VPC Flow Logs: Traffic Analysis

<!--
difficulty: basic
concepts: [vpc-flow-logs, cloudwatch-logs, s3, iam, traffic-analysis, log-format]
tools: [terraform, aws-cli]
estimated_time: 30m
bloom_level: apply
prerequisites: [01-your-first-vpc, 02-public-and-private-subnets]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a t3.micro EC2 instance (~$0.0104/hr) and CloudWatch Log ingestion (~$0.50/GB). Flow log volume is minimal for a lab. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed Exercise 01 (Your First VPC) and Exercise 02 (Public and Private Subnets)
- Basic understanding of IAM roles and CloudWatch Logs

## Learning Objectives

After completing this exercise, you will be able to:

- **Enable** VPC Flow Logs to capture accepted and rejected traffic
- **Configure** dual destinations: CloudWatch Logs for real-time queries and S3 for long-term retention
- **Interpret** flow log records to identify source IP, destination port, action, and protocol
- **Query** flow logs using CloudWatch Logs Insights to find rejected connections

## Why Flow Logs Matter

When a connection fails in your VPC, the first question is always: "Is it a security group issue, a NACL issue, or a routing issue?" Without Flow Logs, you are guessing. VPC Flow Logs capture metadata about every network flow -- source IP, destination IP, port, protocol, and whether the packet was accepted or rejected. They do not capture packet contents (this is not a packet sniffer), but they tell you exactly which traffic is being allowed and which is being dropped.

Flow logs sent to CloudWatch Logs let you run real-time queries using Logs Insights. Flow logs sent to S3 are cheaper for long-term retention and can be queried with Athena for historical analysis. A production VPC should have both: CloudWatch for operational debugging and S3 for compliance and auditing.

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
  default     = "flow-logs-lab"
}
```

### `vpc.tf`

```hcl
data "aws_availability_zones" "available" {
  state = "available"
}

# ------------------------------------------------------------------
# VPC: the network we will monitor with Flow Logs.
# ------------------------------------------------------------------
resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = "${var.project_name}-vpc" }
}

# ------------------------------------------------------------------
# Public subnet with internet access for generating test traffic.
# ------------------------------------------------------------------
resource "aws_subnet" "public" {
  vpc_id                  = aws_vpc.this.id
  cidr_block              = "10.0.1.0/24"
  availability_zone       = data.aws_availability_zones.available.names[0]
  map_public_ip_on_launch = true

  tags = { Name = "${var.project_name}-public" }
}

resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id

  tags = { Name = "${var.project_name}-igw" }
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
# ------------------------------------------------------------------
# Security group: allows SSH inbound and all outbound. We
# intentionally leave port 80 closed to generate REJECT entries
# in the flow logs for verification.
# ------------------------------------------------------------------
resource "aws_security_group" "instance" {
  name        = "${var.project_name}-instance-sg"
  description = "Allow SSH in and all out"
  vpc_id      = aws_vpc.this.id

  tags = { Name = "${var.project_name}-instance-sg" }
}

resource "aws_vpc_security_group_ingress_rule" "ssh_in" {
  security_group_id = aws_security_group.instance.id
  cidr_ipv4         = "0.0.0.0/0"
  from_port         = 22
  to_port           = 22
  ip_protocol       = "tcp"
}

resource "aws_vpc_security_group_egress_rule" "all_out" {
  security_group_id = aws_security_group.instance.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}
```

### `flow-logs.tf`

```hcl
# ------------------------------------------------------------------
# CloudWatch Log Group: receives flow logs for real-time Insights
# queries. 7-day retention keeps costs low in a lab environment.
# ------------------------------------------------------------------
resource "aws_cloudwatch_log_group" "flow_logs" {
  name              = "/vpc/${var.project_name}/flow-logs"
  retention_in_days = 7

  tags = { Name = "${var.project_name}-flow-log-group" }
}

# ------------------------------------------------------------------
# IAM Role: grants the VPC Flow Logs service permission to write
# to CloudWatch Logs. The trust policy allows vpc-flow-logs to
# assume this role.
# ------------------------------------------------------------------
data "aws_iam_policy_document" "flow_logs_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["vpc-flow-logs.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "flow_logs" {
  name               = "${var.project_name}-flow-logs-role"
  assume_role_policy = data.aws_iam_policy_document.flow_logs_assume.json

  tags = { Name = "${var.project_name}-flow-logs-role" }
}

data "aws_iam_policy_document" "flow_logs_write" {
  statement {
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

resource "aws_iam_role_policy" "flow_logs" {
  name   = "${var.project_name}-flow-logs-policy"
  role   = aws_iam_role.flow_logs.id
  policy = data.aws_iam_policy_document.flow_logs_write.json
}

# ------------------------------------------------------------------
# Flow Log to CloudWatch: captures ALL traffic (accepted + rejected)
# on the entire VPC. This is the primary log for real-time debugging.
# ------------------------------------------------------------------
resource "aws_flow_log" "cloudwatch" {
  vpc_id               = aws_vpc.this.id
  traffic_type         = "ALL"
  log_destination_type = "cloud-watch-logs"
  log_destination      = aws_cloudwatch_log_group.flow_logs.arn
  iam_role_arn         = aws_iam_role.flow_logs.arn

  tags = { Name = "${var.project_name}-cw-flow-log" }
}

# ------------------------------------------------------------------
# S3 Bucket: receives flow logs for long-term retention and Athena
# queries. S3 is cheaper than CloudWatch for archival storage.
# ------------------------------------------------------------------
resource "aws_s3_bucket" "flow_logs" {
  bucket_prefix = "${var.project_name}-flow-logs-"
  force_destroy = true

  tags = { Name = "${var.project_name}-flow-logs-bucket" }
}

resource "aws_s3_bucket_lifecycle_configuration" "flow_logs" {
  bucket = aws_s3_bucket.flow_logs.id

  rule {
    id     = "expire-after-30-days"
    status = "Enabled"

    expiration {
      days = 30
    }
  }
}

# ------------------------------------------------------------------
# Flow Log to S3: same VPC, ALL traffic. S3 flow logs do NOT need
# an IAM role -- the service writes directly using a bucket policy.
# ------------------------------------------------------------------
resource "aws_flow_log" "s3" {
  vpc_id               = aws_vpc.this.id
  traffic_type         = "ALL"
  log_destination_type = "s3"
  log_destination      = aws_s3_bucket.flow_logs.arn

  tags = { Name = "${var.project_name}-s3-flow-log" }
}
```

> **Best Practice:** Send flow logs to both CloudWatch Logs (for real-time Insights queries) and S3 (for long-term retention and Athena analysis). CloudWatch is better for operational debugging; S3 is cheaper for compliance archival.

### `compute.tf`

```hcl
# ------------------------------------------------------------------
# Look up the latest Amazon Linux 2023 AMI.
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

# ------------------------------------------------------------------
# SSH key pair for instance access.
# ------------------------------------------------------------------
resource "tls_private_key" "this" {
  algorithm = "RSA"
  rsa_bits  = 4096
}

resource "aws_key_pair" "this" {
  key_name   = "${var.project_name}-key"
  public_key = tls_private_key.this.public_key_openssh
}

resource "local_file" "private_key" {
  content         = tls_private_key.this.private_key_pem
  filename        = "${path.module}/my-key.pem"
  file_permission = "0400"
}

# ------------------------------------------------------------------
# EC2 instance: generates traffic that appears in the flow logs.
# ------------------------------------------------------------------
resource "aws_instance" "this" {
  ami                    = data.aws_ami.amazon_linux_2023.id
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.public.id
  vpc_security_group_ids = [aws_security_group.instance.id]
  key_name               = aws_key_pair.this.key_name

  tags = { Name = "${var.project_name}-instance" }
}
```

### `outputs.tf`

```hcl
output "vpc_id" {
  description = "VPC ID"
  value       = aws_vpc.this.id
}

output "instance_public_ip" {
  description = "Public IP of the test instance"
  value       = aws_instance.this.public_ip
}

output "ssh_command" {
  description = "SSH command for the instance"
  value       = "ssh -i my-key.pem ec2-user@${aws_instance.this.public_ip}"
}

output "log_group_name" {
  description = "CloudWatch Log Group name for flow logs"
  value       = aws_cloudwatch_log_group.flow_logs.name
}

output "s3_bucket_name" {
  description = "S3 bucket name for flow logs"
  value       = aws_s3_bucket.flow_logs.id
}

output "flow_log_cw_id" {
  description = "CloudWatch flow log ID"
  value       = aws_flow_log.cloudwatch.id
}

output "flow_log_s3_id" {
  description = "S3 flow log ID"
  value       = aws_flow_log.s3.id
}
```

## Step 2 -- Initialize and Apply

```bash
terraform init
terraform apply -auto-approve
```

Terraform will create approximately 18 resources: VPC, subnet, IGW, route table, security group, IAM role, CloudWatch log group, two flow logs, S3 bucket, EC2 instance, and supporting resources.

### Intermediate Verification

```bash
terraform state list
```

You should see entries including:

```
aws_cloudwatch_log_group.flow_logs
aws_flow_log.cloudwatch
aws_flow_log.s3
aws_iam_role.flow_logs
aws_instance.this
aws_s3_bucket.flow_logs
aws_vpc.this
```

## Step 3 -- Generate Traffic for Flow Logs

SSH into the instance and make some network requests:

```bash
ssh -i my-key.pem ec2-user@$(terraform output -raw instance_public_ip)
```

From inside the instance, generate accepted traffic:

```bash
curl -s ifconfig.me
ping -c 3 google.com
```

Exit the instance:

```bash
exit
```

Now generate rejected traffic by trying to connect on port 80 (which our security group blocks):

```bash
curl --connect-timeout 5 http://$(terraform output -raw instance_public_ip)
```

Expected: `Connection timed out` -- this attempt will create a REJECT entry in the flow logs.

Wait 2-3 minutes for flow logs to appear in CloudWatch (there is a delivery delay).

## Step 4 -- Query Flow Logs with CloudWatch Insights

Run a Logs Insights query to find rejected traffic:

```bash
aws logs start-query \
  --log-group-name "$(terraform output -raw log_group_name)" \
  --start-time $(date -v-1H +%s 2>/dev/null || date -d '1 hour ago' +%s) \
  --end-time $(date +%s) \
  --query-string 'fields @timestamp, srcAddr, dstAddr, dstPort, action | filter action = "REJECT" | sort @timestamp desc | limit 10'
```

This returns a query ID. Retrieve results (wait a few seconds for the query to complete):

```bash
aws logs get-query-results --query-id <QUERY_ID>
```

Expected: rows showing rejected connections with source IP, destination IP, destination port, and `REJECT` action.

> **Best Practice:** Flow log records use a standard format: `version account-id interface-id srcaddr dstaddr srcport dstport protocol packets bytes start end action log-status`. The `action` field is either `ACCEPT` or `REJECT`. Rejected traffic is your first clue when debugging connectivity issues.

## Common Mistakes

### 1. Missing IAM role for CloudWatch flow logs

Flow logs writing to CloudWatch need an IAM role with the `vpc-flow-logs.amazonaws.com` trust policy. Flow logs writing to S3 do NOT need an IAM role (the service uses a bucket policy). If you forget the role for CloudWatch, the flow log is created but never delivers any records.

**Wrong -- no IAM role specified for CloudWatch flow log:**

```hcl
resource "aws_flow_log" "cloudwatch" {
  vpc_id               = aws_vpc.this.id
  traffic_type         = "ALL"
  log_destination_type = "cloud-watch-logs"
  log_destination      = aws_cloudwatch_log_group.flow_logs.arn
  # Missing: iam_role_arn
}
```

**What happens:** Terraform may succeed, but the flow log status shows as `ACTIVE` while delivering zero records. The CloudWatch log group stays empty indefinitely.

**Fix -- always provide the IAM role for CloudWatch flow logs:**

```hcl
resource "aws_flow_log" "cloudwatch" {
  vpc_id               = aws_vpc.this.id
  traffic_type         = "ALL"
  log_destination_type = "cloud-watch-logs"
  log_destination      = aws_cloudwatch_log_group.flow_logs.arn
  iam_role_arn         = aws_iam_role.flow_logs.arn
}
```

### 2. Setting traffic_type to ACCEPT only

If you set `traffic_type = "ACCEPT"`, you only see successful connections. You miss all the rejected traffic -- which is exactly the traffic you need to see when debugging connectivity issues.

**Wrong -- only capturing accepted traffic:**

```hcl
resource "aws_flow_log" "cloudwatch" {
  vpc_id               = aws_vpc.this.id
  traffic_type         = "ACCEPT"   # Misses REJECT entries
  log_destination_type = "cloud-watch-logs"
  log_destination      = aws_cloudwatch_log_group.flow_logs.arn
  iam_role_arn         = aws_iam_role.flow_logs.arn
}
```

**What happens:** When a security group blocks traffic, there is no flow log entry. You cannot distinguish "traffic was rejected" from "traffic was never sent."

**Fix -- capture ALL traffic:**

```hcl
resource "aws_flow_log" "cloudwatch" {
  vpc_id       = aws_vpc.this.id
  traffic_type = "ALL"
  # ... rest of config
}
```

## Verify What You Learned

```bash
terraform output flow_log_cw_id
```

Expected: a flow log ID starting with `fl-`, e.g. `"fl-0abc123def456789"`

```bash
aws ec2 describe-flow-logs \
  --filter "Name=resource-id,Values=$(terraform output -raw vpc_id)" \
  --query "FlowLogs[].{ID:FlowLogId,Status:FlowLogStatus,Traffic:TrafficType,Dest:LogDestinationType}" \
  --output table
```

Expected:

```
--------------------------------------------------------------
|                    DescribeFlowLogs                        |
+------------------+--------+-------------+---------+--------+
|        Dest      |   ID   |   Status    | Traffic |
+------------------+--------+-------------+---------+
| cloud-watch-logs | fl-... |   ACTIVE    |   ALL   |
| s3               | fl-... |   ACTIVE    |   ALL   |
+------------------+--------+-------------+---------+
```

```bash
aws logs describe-log-groups \
  --log-group-name-prefix "/vpc/${var.project_name:-flow-logs-lab}" \
  --query "logGroups[0].{Name:logGroupName,RetentionDays:retentionInDays}" \
  --output table
```

Expected: the log group name and 7-day retention.

```bash
aws s3 ls s3://$(terraform output -raw s3_bucket_name)/ --recursive | head -5
```

Expected: flow log files in the S3 bucket (may take a few minutes to appear).

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

You now know how to capture and analyze network traffic with VPC Flow Logs. In **Exercise 18 -- NLB: Static IPs and TCP Passthrough**, you will deploy a Network Load Balancer with Elastic IPs, configure TCP health checks, and understand when NLB is the right choice over ALB.

## Summary

- **VPC Flow Logs** capture metadata (not content) about every network flow: source, destination, port, protocol, and action
- Flow logs have two destinations: **CloudWatch Logs** for real-time queries and **S3** for long-term archival
- CloudWatch flow logs require an **IAM role** with `vpc-flow-logs.amazonaws.com` trust; S3 flow logs do not
- Always capture **ALL** traffic (not just ACCEPT) so rejected connections are visible for debugging
- The **action** field (`ACCEPT` or `REJECT`) is the key indicator when troubleshooting connectivity
- Flow logs have a **delivery delay** of 1-5 minutes -- they are not real-time

## Reference

| Resource | Purpose |
|----------|---------|
| `aws_flow_log` | Captures VPC network traffic metadata |
| `aws_cloudwatch_log_group` | Stores flow logs for Insights queries |
| `aws_s3_bucket` | Stores flow logs for long-term retention |
| `aws_iam_role` | Grants flow logs service permission to write to CloudWatch |

## Additional Resources

- [VPC Flow Logs (AWS Docs)](https://docs.aws.amazon.com/vpc/latest/userguide/flow-logs.html) -- official documentation for enabling and configuring flow logs
- [Flow Log Record Format](https://docs.aws.amazon.com/vpc/latest/userguide/flow-log-records.html) -- field-by-field breakdown of the default and custom log formats
- [Query Flow Logs with CloudWatch Insights](https://docs.aws.amazon.com/AmazonCloudWatch/latest/logs/CWL_AnalyzeLogData_FlowLogs.html) -- sample Insights queries for common flow log analysis tasks
- [Query Flow Logs with Athena](https://docs.aws.amazon.com/athena/latest/ug/vpc-flow-logs.html) -- set up Athena tables for S3-based flow log analysis

## Apply Your Knowledge

- [Custom Flow Log Format](https://docs.aws.amazon.com/vpc/latest/userguide/flow-logs.html#flow-logs-fields) -- add fields like `tcp-flags`, `pkt-srcaddr`, and `flow-direction` for deeper traffic analysis
- [Athena VPC Flow Logs Partitioning](https://docs.aws.amazon.com/athena/latest/ug/vpc-flow-logs.html) -- set up partitioned Athena tables for cost-effective historical queries
- [Flow Logs for Network Troubleshooting (AWS Blog)](https://aws.amazon.com/blogs/networking-and-content-delivery/introducing-vpc-flow-logs-for-aws-transit-gateway/) -- advanced flow log patterns for Transit Gateway and cross-VPC analysis

---

> *"The internet is a reflection of our society and that mirror is going to be reflecting what we see."*
> -- **Vint Cerf**

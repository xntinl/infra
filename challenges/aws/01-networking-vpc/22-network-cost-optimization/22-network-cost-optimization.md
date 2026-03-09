# 22. Network Cost Optimization Patterns

<!--
difficulty: intermediate
concepts: [vpc-endpoints, nat-gateway, data-transfer, cost-optimization]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: analyze
prerequisites: [04-production-multi-az-vpc, 15-vpc-endpoints-s3-dynamodb]
aws_cost: ~$0.05/hr
-->

> **AWS Cost Warning:** This exercise creates a NAT Gateway (~$0.045/hr) and a t3.micro EC2 instance (~$0.0104/hr). Gateway endpoints are free. Remember to run `terraform destroy` when finished to avoid unexpected charges.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.5 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Exercise 04 completed | Understand Multi-AZ VPC with NAT Gateways |
| Exercise 15 completed | Understand VPC Gateway Endpoints for S3/DynamoDB |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Evaluate** the cost impact of NAT Gateway data processing charges versus VPC endpoints
2. **Identify** cross-AZ data transfer costs using CloudWatch metrics and VPC Flow Logs
3. **Implement** Gateway and Interface endpoints to eliminate unnecessary NAT charges
4. **Analyze** NAT Gateway metrics to find top bandwidth consumers

## Why Network Costs Catch Teams Off Guard

AWS network costs are invisible until the bill arrives. A NAT Gateway charges $0.045 per hour ($32.40/month) just to exist, plus $0.045 per GB of data it processes. For a workload pulling 500 GB of S3 data monthly through a NAT Gateway, that is $22.50 in data processing charges -- for traffic that could flow for free through a Gateway endpoint. Inter-AZ transfer adds $0.01/GB in each direction; a chatty microservices architecture spanning three AZs can accumulate hundreds of dollars monthly in cross-AZ charges alone.

The fix is straightforward: Gateway endpoints for S3 and DynamoDB are free and eliminate NAT processing for those services. Interface endpoints cost ~$0.01/hr per AZ but are still cheaper than NAT for high-volume AWS API traffic. CloudWatch metrics on the NAT Gateway (`BytesOutToDestination`) reveal exactly how much data flows through it, and VPC Flow Logs let you identify which instances are responsible.

> **Best Practice:** VPC Gateway endpoints for S3 and DynamoDB are free. Create them in every VPC on day one -- even if you think you do not need them yet. The $0 cost means there is zero downside, and they eliminate NAT data processing charges the moment any resource starts accessing S3 or DynamoDB.

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
  default     = "net-cost-lab"
}
```

### `locals.tf`

```hcl
data "aws_availability_zones" "available" {
  state = "available"
}

locals {
  vpc_cidr = "10.0.0.0/16"
  azs      = slice(data.aws_availability_zones.available.names, 0, 2)

  public_subnets = {
    for i, az in local.azs : az => cidrsubnet(local.vpc_cidr, 8, i)
  }
  private_subnets = {
    for i, az in local.azs : az => cidrsubnet(local.vpc_cidr, 8, i + 10)
  }
}
```

### `vpc.tf`

```hcl
# ------------------------------------------------------------------
# VPC: baseline network for cost optimization analysis.
# ------------------------------------------------------------------
resource "aws_vpc" "this" {
  cidr_block           = local.vpc_cidr
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = "${var.project_name}-vpc" }
}

# =======================================================
# TODO 1 — Subnets (public + private, one per AZ)
# =======================================================
# Requirements:
#   - Create aws_subnet.public using for_each over local.public_subnets
#     with map_public_ip_on_launch = true
#   - Create aws_subnet.private using for_each over local.private_subnets
#     (no public IPs)
#   - Tag each: "${var.project_name}-{tier}-${each.key}"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/subnet
# Hint: each.key is the AZ name, each.value is the CIDR block


# =======================================================
# TODO 2 — Internet Gateway + NAT Gateway + Route tables
# =======================================================
# Requirements:
#   - Create one IGW attached to the VPC
#   - Create one public route table (0.0.0.0/0 → IGW),
#     associate all public subnets
#   - Create one EIP + one NAT Gateway in the FIRST public
#     subnet (local.azs[0]) -- intentionally single-AZ to
#     demonstrate the cross-AZ cost problem
#   - Create one private route table (0.0.0.0/0 → NAT GW),
#     associate BOTH private subnets
#
# Docs:
#   https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/internet_gateway
#   https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/nat_gateway
#
# Hint: depends_on = [aws_internet_gateway.this] on the NAT GW
```

### `endpoints.tf`

```hcl
# =======================================================
# TODO 3 — S3 + DynamoDB Gateway Endpoints (free)
# =======================================================
# Requirements:
#   - Create a Gateway endpoint for "com.amazonaws.${var.region}.s3"
#     associated with ALL route tables (public + private)
#   - Create a Gateway endpoint for "com.amazonaws.${var.region}.dynamodb"
#     associated with the private route table only
#   - Tag each: "${var.project_name}-{service}-endpoint"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_endpoint
# Hint: route_table_ids accepts a list


# =======================================================
# TODO 4 — CloudWatch Logs Interface Endpoint
# =======================================================
# Requirements:
#   - Create an Interface endpoint for "com.amazonaws.${var.region}.logs"
#   - Place in private subnets (one ENI per AZ)
#   - Attach the endpoint security group
#   - Enable private_dns_enabled = true
#   - Tag: "${var.project_name}-logs-endpoint"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_endpoint
# Hint: Interface endpoints need subnet_ids and security_group_ids
```

### `monitoring.tf`

```hcl
# =======================================================
# TODO 5 — NAT Gateway CloudWatch Alarm + VPC Flow Logs
# =======================================================
# Requirements:
#   - CloudWatch alarm on BytesOutToDestination (AWS/NATGateway)
#     Period: 3600, Statistic: Sum, Threshold: 1073741824 (1 GB)
#     comparison_operator: GreaterThanThreshold
#   - CloudWatch log group: "/vpc/${var.project_name}/flow-logs"
#     with 7 days retention
#   - IAM role for vpc-flow-logs.amazonaws.com with
#     data "aws_iam_policy_document" for trust and permissions
#   - VPC Flow Log capturing ALL traffic with custom log_format
#     including az-id and flow-direction fields
#
# Docs:
#   https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_metric_alarm
#   https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/flow_log
#
# Hint: The az-id field lets you identify cross-AZ traffic patterns
```

### `security.tf`

```hcl
# ------------------------------------------------------------------
# Security group for test EC2 instance: all outbound, no inbound.
# ------------------------------------------------------------------
resource "aws_security_group" "instance" {
  name        = "${var.project_name}-instance-sg"
  description = "Allow all outbound for cost analysis testing"
  vpc_id      = aws_vpc.this.id

  tags = { Name = "${var.project_name}-instance-sg" }
}

resource "aws_vpc_security_group_egress_rule" "instance_all_out" {
  security_group_id = aws_security_group.instance.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}

# ------------------------------------------------------------------
# Security group for Interface endpoints: HTTPS from VPC CIDR.
# ------------------------------------------------------------------
resource "aws_security_group" "endpoints" {
  name        = "${var.project_name}-endpoint-sg"
  description = "Allow HTTPS inbound from VPC for Interface endpoints"
  vpc_id      = aws_vpc.this.id

  tags = { Name = "${var.project_name}-endpoint-sg" }
}

resource "aws_vpc_security_group_ingress_rule" "endpoints_https" {
  security_group_id = aws_security_group.endpoints.id
  cidr_ipv4         = local.vpc_cidr
  from_port         = 443
  to_port           = 443
  ip_protocol       = "tcp"
}

resource "aws_vpc_security_group_egress_rule" "endpoints_all_out" {
  security_group_id = aws_security_group.endpoints.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}
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
# TODO 6 — EC2 instance + IAM role for cost testing
# =======================================================
# Requirements:
#   - IAM role with ec2.amazonaws.com trust policy
#   - Policy granting s3:ListAllMyBuckets, s3:GetBucketLocation,
#     dynamodb:ListTables, cloudwatch:GetMetricStatistics
#   - Instance profile attached to the role
#   - t3.micro in the FIRST private subnet with user_data:
#       #!/bin/bash
#       yum install -y aws-cli
#       aws s3 ls --region ${var.region} > /tmp/s3-test.txt 2>&1
#   - Tag: "${var.project_name}-test-instance"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/instance
```

### `outputs.tf`

```hcl
output "vpc_id" {
  description = "VPC ID"
  value       = aws_vpc.this.id
}

output "nat_gateway_id" {
  description = "NAT Gateway ID for CloudWatch metric queries"
  value       = aws_nat_gateway.this.id
}

output "s3_endpoint_id" {
  description = "S3 Gateway endpoint ID"
  value       = aws_vpc_endpoint.s3.id
}

output "dynamodb_endpoint_id" {
  description = "DynamoDB Gateway endpoint ID"
  value       = aws_vpc_endpoint.dynamodb.id
}

output "private_route_table_id" {
  description = "Private route table ID"
  value       = aws_route_table.private.id
}

output "instance_id" {
  description = "EC2 instance ID"
  value       = aws_instance.this.id
}
```

## Spot the Bug

A colleague deployed this VPC and wonders why the second AZ's private subnet has higher data transfer costs than the first. The code passes `terraform validate`. **What is wrong and why does it cost more?**

```hcl
resource "aws_nat_gateway" "this" {
  allocation_id = aws_eip.nat.id
  subnet_id     = aws_subnet.public[local.azs[0]].id   # <-- only AZ-a

  tags       = { Name = "cost-lab-nat" }
  depends_on = [aws_internet_gateway.this]
}

resource "aws_route_table" "private" {
  vpc_id = aws_vpc.this.id

  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.this.id
  }

  tags = { Name = "cost-lab-private-rt" }
}

resource "aws_route_table_association" "private" {
  for_each       = local.private_subnets
  subnet_id      = aws_subnet.private[each.key].id
  route_table_id = aws_route_table.private.id   # <-- same RT for both AZs
}
```

<details>
<summary>Explain the bug</summary>

**The problem:** A single NAT Gateway sits in AZ-a, but both private subnets share one route table pointing to that NAT Gateway. Traffic from AZ-b crosses the AZ boundary to reach the NAT Gateway in AZ-a.

1. **Cross-AZ data transfer charges** -- AWS charges $0.01/GB in each direction, so $0.02/GB round-trip on top of the NAT processing fee.
2. **Single point of failure** -- if AZ-a goes down, AZ-b loses internet access too.

**The fix:** For production, create a NAT Gateway per AZ with separate route tables:

```hcl
resource "aws_nat_gateway" "this" {
  for_each      = local.public_subnets
  allocation_id = aws_eip.nat[each.key].id
  subnet_id     = aws_subnet.public[each.key].id
  tags          = { Name = "cost-lab-nat-${each.key}" }
  depends_on    = [aws_internet_gateway.this]
}

resource "aws_route_table" "private" {
  for_each = local.private_subnets
  vpc_id   = aws_vpc.this.id

  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.this[each.key].id
  }

  tags = { Name = "cost-lab-private-rt-${each.key}" }
}
```

For non-production where cost matters more than HA, a single NAT Gateway is acceptable -- but budget for cross-AZ charges.

</details>

## Verify What You Learned

### Step 1 -- Apply and verify endpoints

```bash
terraform init && terraform apply -auto-approve
```

```bash
aws ec2 describe-vpc-endpoints \
  --filters "Name=vpc-id,Values=$(terraform output -raw vpc_id)" \
  --query "VpcEndpoints[].{Service:ServiceName,Type:VpcEndpointType,State:State}" \
  --output table
```

Expected:

```
----------------------------------------------------------------------
|                      DescribeVpcEndpoints                          |
+-----------+------------------------------------------+-------------+
|   State   |              Service                     |    Type     |
+-----------+------------------------------------------+-------------+
|  available|  com.amazonaws.us-east-1.s3              |  Gateway    |
|  available|  com.amazonaws.us-east-1.dynamodb        |  Gateway    |
|  available|  com.amazonaws.us-east-1.logs            |  Interface  |
+-----------+------------------------------------------+-------------+
```

### Step 2 -- Verify prefix list routes

```bash
aws ec2 describe-route-tables \
  --route-table-ids $(terraform output -raw private_route_table_id) \
  --query "RouteTables[0].Routes[].{Dest:DestinationCidrBlock,PrefixList:DestinationPrefixListId,GW:GatewayId,NAT:NatGatewayId}" \
  --output table
```

Expected: two `pl-` prefix list entries for S3 and DynamoDB alongside the local and NAT routes.

### Step 3 -- Query NAT Gateway metrics

```bash
aws cloudwatch get-metric-statistics \
  --namespace "AWS/NATGateway" \
  --metric-name "BytesOutToDestination" \
  --dimensions Name=NatGatewayId,Value=$(terraform output -raw nat_gateway_id) \
  --start-time $(date -u -v-1H +%Y-%m-%dT%H:%M:%S 2>/dev/null || date -u -d '1 hour ago' +%Y-%m-%dT%H:%M:%S) \
  --end-time $(date -u +%Y-%m-%dT%H:%M:%S) \
  --period 3600 \
  --statistics Sum \
  --output table
```

Expected: a table showing bytes processed. After Gateway endpoints, S3 and DynamoDB traffic no longer appears here.

### Step 4 -- Verify VPC Flow Logs

```bash
aws ec2 describe-flow-logs \
  --filter "Name=resource-id,Values=$(terraform output -raw vpc_id)" \
  --query "FlowLogs[].{Status:FlowLogStatus,Traffic:TrafficType,Dest:LogDestinationType}" \
  --output table
```

Expected:

```
---------------------------------------------
|            DescribeFlowLogs               |
+------------------+----------+-------------+
|      Dest        |  Status  |   Traffic   |
+------------------+----------+-------------+
| cloud-watch-logs |  ACTIVE  |  ALL        |
+------------------+----------+-------------+
```

## Solutions

<details>
<summary>TODO 1 -- Subnets (vpc.tf)</summary>

```hcl
resource "aws_subnet" "public" {
  for_each                = local.public_subnets
  vpc_id                  = aws_vpc.this.id
  cidr_block              = each.value
  availability_zone       = each.key
  map_public_ip_on_launch = true

  tags = { Name = "${var.project_name}-public-${each.key}" }
}

resource "aws_subnet" "private" {
  for_each          = local.private_subnets
  vpc_id            = aws_vpc.this.id
  cidr_block        = each.value
  availability_zone = each.key

  tags = { Name = "${var.project_name}-private-${each.key}" }
}
```

</details>

<details>
<summary>TODO 2 -- IGW + NAT + Route tables (vpc.tf)</summary>

```hcl
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
  for_each       = aws_subnet.public
  subnet_id      = each.value.id
  route_table_id = aws_route_table.public.id
}

resource "aws_eip" "nat" {
  domain = "vpc"
  tags   = { Name = "${var.project_name}-nat-eip" }
}

resource "aws_nat_gateway" "this" {
  allocation_id = aws_eip.nat.id
  subnet_id     = aws_subnet.public[local.azs[0]].id
  tags          = { Name = "${var.project_name}-nat" }
  depends_on    = [aws_internet_gateway.this]
}

resource "aws_route_table" "private" {
  vpc_id = aws_vpc.this.id
  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.this.id
  }
  tags = { Name = "${var.project_name}-private-rt" }
}

resource "aws_route_table_association" "private" {
  for_each       = local.private_subnets
  subnet_id      = aws_subnet.private[each.key].id
  route_table_id = aws_route_table.private.id
}
```

</details>

<details>
<summary>TODO 3 -- S3 + DynamoDB Gateway Endpoints (endpoints.tf)</summary>

```hcl
resource "aws_vpc_endpoint" "s3" {
  vpc_id            = aws_vpc.this.id
  service_name      = "com.amazonaws.${var.region}.s3"
  vpc_endpoint_type = "Gateway"
  route_table_ids   = [aws_route_table.public.id, aws_route_table.private.id]

  tags = { Name = "${var.project_name}-s3-endpoint" }
}

resource "aws_vpc_endpoint" "dynamodb" {
  vpc_id            = aws_vpc.this.id
  service_name      = "com.amazonaws.${var.region}.dynamodb"
  vpc_endpoint_type = "Gateway"
  route_table_ids   = [aws_route_table.private.id]

  tags = { Name = "${var.project_name}-dynamodb-endpoint" }
}
```

</details>

<details>
<summary>TODO 4 -- CloudWatch Logs Interface Endpoint (endpoints.tf)</summary>

```hcl
resource "aws_vpc_endpoint" "logs" {
  vpc_id              = aws_vpc.this.id
  service_name        = "com.amazonaws.${var.region}.logs"
  vpc_endpoint_type   = "Interface"
  subnet_ids          = [for s in aws_subnet.private : s.id]
  security_group_ids  = [aws_security_group.endpoints.id]
  private_dns_enabled = true

  tags = { Name = "${var.project_name}-logs-endpoint" }
}
```

</details>

<details>
<summary>TODO 5 -- CloudWatch Alarm + VPC Flow Logs (monitoring.tf)</summary>

```hcl
resource "aws_cloudwatch_metric_alarm" "nat_bytes" {
  alarm_name          = "${var.project_name}-nat-bytes-alarm"
  alarm_description   = "Alert when NAT processes more than 1 GB/hr"
  namespace           = "AWS/NATGateway"
  metric_name         = "BytesOutToDestination"
  statistic           = "Sum"
  period              = 3600
  evaluation_periods  = 1
  threshold           = 1073741824
  comparison_operator = "GreaterThanThreshold"
  treat_missing_data  = "notBreaching"
  dimensions          = { NatGatewayId = aws_nat_gateway.this.id }

  tags = { Name = "${var.project_name}-nat-bytes-alarm" }
}

resource "aws_cloudwatch_log_group" "flow_logs" {
  name              = "/vpc/${var.project_name}/flow-logs"
  retention_in_days = 7
  tags              = { Name = "${var.project_name}-flow-logs" }
}

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
  tags               = { Name = "${var.project_name}-flow-logs-role" }
}

data "aws_iam_policy_document" "flow_logs_write" {
  statement {
    actions = [
      "logs:CreateLogGroup", "logs:CreateLogStream",
      "logs:PutLogEvents", "logs:DescribeLogGroups",
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

resource "aws_flow_log" "this" {
  vpc_id               = aws_vpc.this.id
  traffic_type         = "ALL"
  log_destination_type = "cloud-watch-logs"
  log_destination      = aws_cloudwatch_log_group.flow_logs.arn
  iam_role_arn         = aws_iam_role.flow_logs.arn

  log_format = "$${version} $${account-id} $${interface-id} $${srcaddr} $${dstaddr} $${srcport} $${dstport} $${protocol} $${packets} $${bytes} $${start} $${end} $${action} $${log-status} $${az-id} $${flow-direction}"

  tags = { Name = "${var.project_name}-flow-log" }
}
```

</details>

<details>
<summary>TODO 6 -- EC2 instance + IAM role (compute.tf)</summary>

```hcl
data "aws_iam_policy_document" "ec2_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ec2.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "this" {
  name               = "${var.project_name}-ec2-role"
  assume_role_policy = data.aws_iam_policy_document.ec2_assume.json
  tags               = { Name = "${var.project_name}-ec2-role" }
}

data "aws_iam_policy_document" "ec2_perms" {
  statement {
    actions   = ["s3:ListAllMyBuckets", "s3:GetBucketLocation"]
    resources = ["*"]
  }
  statement {
    actions   = ["dynamodb:ListTables"]
    resources = ["*"]
  }
  statement {
    actions   = ["cloudwatch:GetMetricStatistics", "cloudwatch:ListMetrics"]
    resources = ["*"]
  }
}

resource "aws_iam_role_policy" "this" {
  name   = "${var.project_name}-ec2-perms"
  role   = aws_iam_role.this.id
  policy = data.aws_iam_policy_document.ec2_perms.json
}

resource "aws_iam_instance_profile" "this" {
  name = "${var.project_name}-profile"
  role = aws_iam_role.this.name
  tags = { Name = "${var.project_name}-profile" }
}

resource "aws_instance" "this" {
  ami                    = data.aws_ami.amazon_linux_2023.id
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.private[local.azs[0]].id
  vpc_security_group_ids = [aws_security_group.instance.id]
  iam_instance_profile   = aws_iam_instance_profile.this.name

  user_data = <<-EOF
    #!/bin/bash
    yum install -y aws-cli
    aws s3 ls --region ${var.region} > /tmp/s3-test.txt 2>&1
  EOF

  tags = { Name = "${var.project_name}-test-instance" }
}
```

</details>

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

You analyzed and optimized network costs by adding Gateway endpoints, Interface endpoints, and monitoring. In **Exercise 23 -- VPC CIDR Planning**, you will learn how to design IP address allocation strategies that avoid conflicts across multiple VPCs and accounts.

## Summary

- **NAT Gateway** charges $0.045/hr plus $0.045/GB of data processed -- these costs compound quickly
- **Gateway endpoints** for S3 and DynamoDB are free and eliminate NAT data processing for those services
- **Interface endpoints** cost ~$0.01/hr per AZ but are cheaper than NAT for high-volume AWS API traffic
- **Cross-AZ data transfer** costs $0.01/GB in each direction -- a single-AZ NAT Gateway forces cross-AZ traffic from other AZs
- **CloudWatch metrics** (`BytesOutToDestination`) on the NAT Gateway reveal how much data is being processed
- **VPC Flow Logs** with the `az-id` field let you identify cross-AZ traffic patterns that incur hidden charges

## Reference

| Resource | Purpose |
|----------|---------|
| `aws_vpc_endpoint` | Gateway (free for S3/DynamoDB) or Interface (ENI-based) endpoint |
| `aws_nat_gateway` | Outbound internet for private subnets (costly) |
| `aws_cloudwatch_metric_alarm` | Alert on NAT Gateway bytes processed |
| `aws_flow_log` | Network traffic logging for cost analysis |

## Additional Resources

- [Reduce NAT Gateway Costs with VPC Endpoints (AWS Blog)](https://aws.amazon.com/blogs/networking-and-content-delivery/reduce-nat-gateway-costs-by-using-vpc-endpoints/) -- strategies to minimize NAT data processing charges
- [AWS Data Transfer Pricing](https://aws.amazon.com/ec2/pricing/on-demand/#Data_Transfer) -- official pricing for inter-AZ, inter-region, and internet data transfer
- [VPC Flow Logs Custom Format](https://docs.aws.amazon.com/vpc/latest/userguide/flow-logs.html#flow-logs-fields) -- all available fields including az-id for cross-AZ analysis
- [Terraform aws_cloudwatch_metric_alarm](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_metric_alarm) -- creating CloudWatch alarms for cost monitoring

## Apply Your Knowledge

- [AWS Cost Optimization Pillar](https://docs.aws.amazon.com/wellarchitected/latest/cost-optimization-pillar/welcome.html) -- Well-Architected Framework guidance on cost optimization
- [VPC Endpoint Policies](https://docs.aws.amazon.com/vpc/latest/privatelink/vpc-endpoints-access.html) -- restrict which resources are accessible through an endpoint for security and cost control
- [NAT Gateway CloudWatch Metrics](https://docs.aws.amazon.com/vpc/latest/userguide/vpc-nat-gateway-cloudwatch.html) -- complete list of NAT Gateway metrics for monitoring and alerting

---

> *"One accurate measurement is worth a thousand expert opinions."*
> -- **Grace Hopper**

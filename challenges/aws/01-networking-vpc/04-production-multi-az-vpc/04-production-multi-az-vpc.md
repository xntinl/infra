# 4. Production Multi-AZ Multi-Tier VPC

<!--
difficulty: intermediate
concepts: [multi-az, three-tier-architecture, nat-gateway-per-az, vpc-endpoints, flow-logs]
tools: [terraform, aws-cli]
estimated_time: 60m
bloom_level: design, justify, implement
prerequisites: [01-02, 01-03]
aws_cost: ~$0.15/hr
-->

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.5 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Exercise 02 completed | Understand public/private subnets |
| Exercise 03 completed | Understand security groups and NACLs |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Design** a three-tier VPC architecture spanning multiple Availability Zones
2. **Justify** why production workloads need a NAT Gateway per AZ instead of a shared one
3. **Implement** subnet layouts using `cidrsubnet()` with `for_each` loops
4. **Configure** VPC Flow Logs for network traffic auditing
5. **Create** a Gateway VPC Endpoint for S3 to keep traffic off the public internet

## Why This Matters

Production workloads demand high availability. A single-AZ deployment means one data center failure takes your entire application offline. Spreading resources across three AZs in the same region gives you fault tolerance -- if one AZ goes down, the other two keep serving traffic. AWS designs AZs with independent power, cooling, and networking, so correlated failures are extremely rare.

The three-tier model (public, private, data) enforces defense in depth. Load balancers live in public subnets, application servers in private subnets behind NAT, and databases in isolated data subnets with no internet route at all. Placing a NAT Gateway in each AZ avoids a cross-AZ single point of failure and eliminates cross-AZ data transfer charges for outbound traffic. VPC Flow Logs give you an audit trail of every accepted and rejected connection, and a Gateway VPC Endpoint for S3 keeps your S3 traffic on the AWS backbone instead of routing it through NAT (saving cost and reducing latency).

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
  default     = "prod-vpc-lab"
}
```

### `locals.tf`

```hcl
data "aws_availability_zones" "available" {
  state = "available"
}

locals {
  vpc_cidr = "10.0.0.0/16"
  azs      = slice(data.aws_availability_zones.available.names, 0, 3)

  # Each tier gets a /20 block, split into three /22 subnets (one per AZ)
  # Tier 0 = public  (10.0.0.0/20  -> 10.0.0.0/22, 10.0.4.0/22, 10.0.8.0/22)
  # Tier 1 = private (10.0.16.0/20 -> 10.0.16.0/22, 10.0.20.0/22, 10.0.24.0/22)
  # Tier 2 = data    (10.0.32.0/20 -> 10.0.32.0/22, 10.0.36.0/22, 10.0.40.0/22)
  public_subnets = {
    for i, az in local.azs : az => cidrsubnet(local.vpc_cidr, 6, i)
  }
  private_subnets = {
    for i, az in local.azs : az => cidrsubnet(local.vpc_cidr, 6, i + 4)
  }
  data_subnets = {
    for i, az in local.azs : az => cidrsubnet(local.vpc_cidr, 6, i + 8)
  }
}
```

### `vpc.tf`

```hcl
resource "aws_vpc" "this" {
  cidr_block           = local.vpc_cidr
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = {
    Name = "${var.project_name}-vpc"
  }
}

# =======================================================
# TODO 1 — Public subnets (one per AZ)
# =======================================================
# Requirements:
#   - Use for_each over local.public_subnets
#   - Set map_public_ip_on_launch = true
#   - Set the availability_zone from each.key
#   - Tag with Name = "${var.project_name}-public-${each.key}"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/subnet
# Hint: each.key is the AZ name, each.value is the CIDR block


# =======================================================
# TODO 2 — Private subnets + Data subnets
# =======================================================
# Requirements:
#   - Create aws_subnet.private using for_each over local.private_subnets
#   - Create aws_subnet.data using for_each over local.data_subnets
#   - Neither tier maps public IPs
#   - Tag each with its tier name and AZ
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/subnet


# =======================================================
# TODO 3 — Internet Gateway + Public route table
# =======================================================
# Requirements:
#   - Create one aws_internet_gateway attached to the VPC
#   - Create one aws_route_table for the public tier
#   - Add a default route (0.0.0.0/0) to the IGW
#   - Associate every public subnet with this route table
#     (use for_each over aws_subnet.public)
#
# Docs:
#   https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/internet_gateway
#   https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route_table
#   https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route_table_association


# =======================================================
# TODO 4 — NAT Gateways (one per AZ) + Private route tables
# =======================================================
# Requirements:
#   - Create 3 EIPs (one per AZ) using for_each over local.azs
#   - Create 3 NAT Gateways, each in the PUBLIC subnet of its AZ
#   - Create 3 route tables (one per AZ) for the private tier
#   - Each private route table has a default route to its own AZ's NAT GW
#   - Associate each private subnet with its AZ's route table
#   - Associate each data subnet with its AZ's private route table
#     (data tier uses NAT only for patching; no IGW route)
#
# Docs:
#   https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/eip
#   https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/nat_gateway
#
# Hint: for_each = local.public_subnets on NAT GW lets you use
#        each.key (AZ) to look up aws_subnet.public[each.key].id
```

### `monitoring.tf`

```hcl
# =======================================================
# TODO 5 — VPC Flow Logs to CloudWatch
# =======================================================
# Requirements:
#   - Create an aws_cloudwatch_log_group with retention of 14 days
#   - Create an IAM role that allows vpc-flow-logs.amazonaws.com to
#     write to CloudWatch Logs (use aws_iam_role + aws_iam_role_policy)
#   - Create an aws_flow_log capturing ALL traffic on the VPC
#
# Docs:
#   https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/flow_log
#   https://docs.aws.amazon.com/vpc/latest/userguide/flow-logs-cwl.html
#
# Hint: the IAM trust policy principal is "vpc-flow-logs.amazonaws.com"


# =======================================================
# TODO 6 — S3 Gateway VPC Endpoint
# =======================================================
# Requirements:
#   - Create an aws_vpc_endpoint of type "Gateway" for the
#     service "com.amazonaws.${var.region}.s3"
#   - Associate it with ALL route tables (public + all 3 private)
#   - Use a full-access policy (or the default)
#
# Docs:
#   https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_endpoint
#
# Hint: route_table_ids accepts a list. Combine the public RT id
#        with the values of the private RT map.
```

### `outputs.tf`

```hcl
output "vpc_id" {
  value = aws_vpc.this.id
}

output "public_subnet_ids" {
  value = [for s in aws_subnet.public : s.id]
}

output "private_subnet_ids" {
  value = [for s in aws_subnet.private : s.id]
}

output "data_subnet_ids" {
  value = [for s in aws_subnet.data : s.id]
}
```

## Spot the Bug

A colleague submitted this code for the private route tables. It passes `terraform validate` but defeats the purpose of per-AZ NAT Gateway redundancy. **What is wrong and why does it matter?**

```hcl
resource "aws_route_table" "private" {
  for_each = local.public_subnets
  vpc_id   = aws_vpc.this.id

  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.this[local.azs[0]].id   # <-- BUG
  }

  tags = {
    Name = "${var.project_name}-private-${each.key}"
  }
}
```

<details>
<summary>Explain the bug</summary>

Every private route table points to the NAT Gateway in `local.azs[0]` (the first AZ) instead of `aws_nat_gateway.this[each.key].id`. This means:

1. **Single point of failure** -- if the first AZ goes down, all three private subnets lose internet access even though their own NAT Gateways are healthy.
2. **Cross-AZ data transfer costs** -- traffic from AZ-b and AZ-c private subnets crosses AZ boundaries to reach the NAT Gateway in AZ-a, incurring $0.01/GB charges in both directions.

The fix is to reference the NAT Gateway that matches the current AZ:

```hcl
nat_gateway_id = aws_nat_gateway.this[each.key].id
```

</details>

## Verify What You Learned

### Step 1 -- Plan resource count

Run `terraform init` and then `terraform plan`. You should see approximately **30+ resources** to be created:

```
Plan: 30 to add, 0 to change, 0 to destroy.
```

The breakdown:
- 1 VPC, 9 subnets, 1 IGW
- 3 EIPs, 3 NAT Gateways
- 4 route tables, 9 route table associations, 4 routes
- 1 log group, 1 IAM role, 1 IAM role policy, 1 flow log
- 1 VPC endpoint

### Step 2 -- Apply and verify subnets span 3 AZs

After `terraform apply`, run:

```
aws ec2 describe-subnets \
  --filters "Name=vpc-id,Values=$(terraform output -raw vpc_id)" \
  --query "Subnets[].{AZ:AvailabilityZone,CIDR:CidrBlock,Public:MapPublicIpOnLaunch}" \
  --output table
```

Expected output (AZ names will vary by region):

```
-------------------------------------------------
|               DescribeSubnets                 |
+---------------+----------------+--------------+
|      AZ       |     CIDR       |   Public     |
+---------------+----------------+--------------+
|  us-east-1a   |  10.0.0.0/22   |  True        |
|  us-east-1b   |  10.0.4.0/22   |  True        |
|  us-east-1c   |  10.0.8.0/22   |  True        |
|  us-east-1a   |  10.0.16.0/22  |  False       |
|  us-east-1b   |  10.0.20.0/22  |  False       |
|  us-east-1c   |  10.0.24.0/22  |  False       |
|  us-east-1a   |  10.0.32.0/22  |  False       |
|  us-east-1b   |  10.0.36.0/22  |  False       |
|  us-east-1c   |  10.0.40.0/22  |  False       |
+---------------+----------------+--------------+
```

### Step 3 -- Verify VPC Endpoint

```
aws ec2 describe-vpc-endpoints \
  --filters "Name=vpc-id,Values=$(terraform output -raw vpc_id)" \
  --query "VpcEndpoints[].{Service:ServiceName,Type:VpcEndpointType,State:State}" \
  --output table
```

Expected:

```
--------------------------------------------------------------
|                   DescribeVpcEndpoints                     |
+-----------+----------------------------------+-------------+
|   State   |            Service               |    Type     |
+-----------+----------------------------------+-------------+
|  available|  com.amazonaws.us-east-1.s3      |  Gateway    |
+-----------+----------------------------------+-------------+
```

### Step 4 -- Verify Flow Logs

```
aws ec2 describe-flow-logs \
  --filter "Name=resource-id,Values=$(terraform output -raw vpc_id)" \
  --query "FlowLogs[].{Status:FlowLogStatus,Traffic:TrafficType,Dest:LogDestinationType}" \
  --output table
```

Expected:

```
---------------------------------------------
|            DescribeFlowLogs               |
+----------+----------------+---------------+
|   Dest   |    Status      |   Traffic     |
+----------+----------------+---------------+
| cloud-watch-logs |  ACTIVE |  ALL         |
+----------+----------------+---------------+
```

## Solutions

<details>
<summary>TODO 1 -- Public subnets (vpc.tf)</summary>

```hcl
resource "aws_subnet" "public" {
  for_each = local.public_subnets

  vpc_id                  = aws_vpc.this.id
  cidr_block              = each.value
  availability_zone       = each.key
  map_public_ip_on_launch = true

  tags = {
    Name = "${var.project_name}-public-${each.key}"
  }
}
```

</details>

<details>
<summary>TODO 2 -- Private subnets + Data subnets (vpc.tf)</summary>

```hcl
resource "aws_subnet" "private" {
  for_each = local.private_subnets

  vpc_id            = aws_vpc.this.id
  cidr_block        = each.value
  availability_zone = each.key

  tags = {
    Name = "${var.project_name}-private-${each.key}"
  }
}

resource "aws_subnet" "data" {
  for_each = local.data_subnets

  vpc_id            = aws_vpc.this.id
  cidr_block        = each.value
  availability_zone = each.key

  tags = {
    Name = "${var.project_name}-data-${each.key}"
  }
}
```

</details>

<details>
<summary>TODO 3 -- Internet Gateway + Public route table (vpc.tf)</summary>

```hcl
resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id

  tags = {
    Name = "${var.project_name}-igw"
  }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.this.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this.id
  }

  tags = {
    Name = "${var.project_name}-public-rt"
  }
}

resource "aws_route_table_association" "public" {
  for_each = aws_subnet.public

  subnet_id      = each.value.id
  route_table_id = aws_route_table.public.id
}
```

</details>

<details>
<summary>TODO 4 -- NAT Gateways + Private route tables (vpc.tf)</summary>

```hcl
resource "aws_eip" "nat" {
  for_each = local.public_subnets
  domain   = "vpc"

  tags = {
    Name = "${var.project_name}-nat-eip-${each.key}"
  }
}

resource "aws_nat_gateway" "this" {
  for_each = local.public_subnets

  allocation_id = aws_eip.nat[each.key].id
  subnet_id     = aws_subnet.public[each.key].id

  tags = {
    Name = "${var.project_name}-nat-${each.key}"
  }

  depends_on = [aws_internet_gateway.this]
}

resource "aws_route_table" "private" {
  for_each = local.public_subnets
  vpc_id   = aws_vpc.this.id

  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.this[each.key].id
  }

  tags = {
    Name = "${var.project_name}-private-rt-${each.key}"
  }
}

resource "aws_route_table_association" "private" {
  for_each = local.private_subnets

  subnet_id      = aws_subnet.private[each.key].id
  route_table_id = aws_route_table.private[each.key].id
}

resource "aws_route_table_association" "data" {
  for_each = local.data_subnets

  subnet_id      = aws_subnet.data[each.key].id
  route_table_id = aws_route_table.private[each.key].id
}
```

</details>

<details>
<summary>TODO 5 -- VPC Flow Logs (monitoring.tf)</summary>

```hcl
resource "aws_cloudwatch_log_group" "flow_logs" {
  name              = "/vpc/${var.project_name}/flow-logs"
  retention_in_days = 14

  tags = {
    Name = "${var.project_name}-flow-logs"
  }
}

resource "aws_iam_role" "flow_logs" {
  name = "${var.project_name}-flow-logs-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = {
        Service = "vpc-flow-logs.amazonaws.com"
      }
    }]
  })
}

resource "aws_iam_role_policy" "flow_logs" {
  name = "${var.project_name}-flow-logs-policy"
  role = aws_iam_role.flow_logs.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = [
        "logs:CreateLogGroup",
        "logs:CreateLogStream",
        "logs:PutLogEvents",
        "logs:DescribeLogGroups",
        "logs:DescribeLogStreams"
      ]
      Effect   = "Allow"
      Resource = "*"
    }]
  })
}

resource "aws_flow_log" "this" {
  vpc_id                = aws_vpc.this.id
  traffic_type          = "ALL"
  log_destination_type  = "cloud-watch-logs"
  log_destination       = aws_cloudwatch_log_group.flow_logs.arn
  iam_role_arn          = aws_iam_role.flow_logs.arn

  tags = {
    Name = "${var.project_name}-flow-log"
  }
}
```

</details>

<details>
<summary>TODO 6 -- S3 Gateway VPC Endpoint (monitoring.tf)</summary>

```hcl
resource "aws_vpc_endpoint" "s3" {
  vpc_id       = aws_vpc.this.id
  service_name = "com.amazonaws.${var.region}.s3"

  vpc_endpoint_type = "Gateway"

  route_table_ids = concat(
    [aws_route_table.public.id],
    [for rt in aws_route_table.private : rt.id]
  )

  tags = {
    Name = "${var.project_name}-s3-endpoint"
  }
}
```

</details>

## Cleanup

Destroy all resources to stop incurring charges (NAT Gateways cost ~$0.045/hr each):

```
terraform destroy
```

Type `yes` when prompted. Verify no resources remain:

```
aws ec2 describe-vpcs \
  --filters "Name=tag:Name,Values=prod-vpc-lab-vpc" \
  --query "Vpcs[].VpcId" --output text
```

This should return empty output.

## What's Next

In **Exercise 05 -- VPC Peering and Cross-VPC DNS Resolution**, you will connect two VPCs using peering, configure bidirectional routing, and set up Route 53 private hosted zones so services can discover each other by DNS name instead of IP address.

## Summary

You built a production-grade VPC with:

- **9 subnets** across 3 AZs in 3 tiers (public, private, data)
- **3 NAT Gateways** for per-AZ redundancy and cost optimization
- **VPC Flow Logs** for traffic auditing
- **S3 Gateway Endpoint** to keep S3 traffic on the AWS backbone

This is the standard foundation for any production AWS deployment. Every subsequent exercise builds on top of this pattern.

## Reference

| Resource | Purpose |
|----------|---------|
| `aws_vpc` | The virtual network |
| `aws_subnet` | Segments within the VPC |
| `aws_internet_gateway` | Public internet access |
| `aws_nat_gateway` | Outbound internet for private subnets |
| `aws_eip` | Static IP for NAT Gateways |
| `aws_route_table` | Routing rules |
| `aws_flow_log` | Network traffic logging |
| `aws_vpc_endpoint` | Private access to AWS services |

## Additional Resources

- [AWS VPC Documentation](https://docs.aws.amazon.com/vpc/latest/userguide/what-is-amazon-vpc.html)
- [Terraform cidrsubnet Function](https://developer.hashicorp.com/terraform/language/functions/cidrsubnet)
- [VPC Flow Logs](https://docs.aws.amazon.com/vpc/latest/userguide/flow-logs.html)
- [Gateway VPC Endpoints](https://docs.aws.amazon.com/vpc/latest/privatelink/vpc-endpoints-s3.html)
- [AWS Multi-AZ Best Practices](https://docs.aws.amazon.com/whitepapers/latest/real-time-communication-on-aws/high-availability-and-scalability-on-aws.html)

# 15. VPC Gateway Endpoints: S3 and DynamoDB

<!--
difficulty: basic
concepts: [vpc-endpoints, gateway-endpoints, s3, dynamodb, route-tables, nat-gateway]
tools: [terraform, aws-cli]
estimated_time: 30m
bloom_level: understand
prerequisites: [01-your-first-vpc, 02-public-and-private-subnets]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a NAT Gateway (~$0.045/hr) and a t3.micro EC2 instance (~$0.0104/hr). Gateway endpoints themselves are **free**. Remember to run `terraform destroy` when finished to avoid unexpected charges.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed Exercise 01 (Your First VPC) and Exercise 02 (Public and Private Subnets)
- Basic understanding of route tables and NAT Gateways

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** the difference between Gateway endpoints and Interface endpoints
- **Create** VPC Gateway endpoints for S3 and DynamoDB using Terraform
- **Identify** how Gateway endpoints inject routes into route tables automatically
- **Verify** that traffic to S3 and DynamoDB bypasses the NAT Gateway when a Gateway endpoint exists

## Why Gateway Endpoints Matter

Every time an EC2 instance in a private subnet calls S3 or DynamoDB, that traffic flows through the NAT Gateway. The NAT Gateway charges $0.045 per GB of data processed -- on top of the hourly cost. For a workload that transfers 1 TB of data to S3 per month, that is $45 in NAT data processing charges alone.

VPC Gateway endpoints eliminate this cost entirely. A Gateway endpoint is a route table entry that tells the VPC to send S3 or DynamoDB traffic directly over the AWS private backbone instead of through the NAT Gateway. There is no hourly charge, no data processing charge, and no bandwidth limit. Gateway endpoints are available only for S3 and DynamoDB -- these two services handle the vast majority of high-volume data transfer in most AWS architectures.

The endpoint works by adding a prefix list route to every route table you associate it with. When the instance resolves `s3.amazonaws.com`, the IP address falls within the prefix list, and the VPC routes that traffic directly to the endpoint instead of following the `0.0.0.0/0` route to the NAT Gateway.

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
  default     = "vpc-endpoints-lab"
}
```

### `vpc.tf`

```hcl
data "aws_availability_zones" "available" {
  state = "available"
}

# ------------------------------------------------------------------
# VPC: the foundation network with DNS support enabled.
# DNS hostnames are required for S3 endpoint resolution.
# ------------------------------------------------------------------
resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = "${var.project_name}-vpc" }
}

# ------------------------------------------------------------------
# Public subnet: hosts the NAT Gateway so private instances can
# reach the internet for packages and updates.
# ------------------------------------------------------------------
resource "aws_subnet" "public" {
  vpc_id                  = aws_vpc.this.id
  cidr_block              = "10.0.1.0/24"
  availability_zone       = data.aws_availability_zones.available.names[0]
  map_public_ip_on_launch = true

  tags = { Name = "${var.project_name}-public" }
}

# ------------------------------------------------------------------
# Private subnet: hosts the EC2 instance that will access S3 and
# DynamoDB through the Gateway endpoints.
# ------------------------------------------------------------------
resource "aws_subnet" "private" {
  vpc_id            = aws_vpc.this.id
  cidr_block        = "10.0.2.0/24"
  availability_zone = data.aws_availability_zones.available.names[0]

  tags = { Name = "${var.project_name}-private" }
}

# ------------------------------------------------------------------
# Internet Gateway: required for the public subnet and NAT Gateway.
# ------------------------------------------------------------------
resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id

  tags = { Name = "${var.project_name}-igw" }
}

# ------------------------------------------------------------------
# NAT Gateway: gives the private subnet outbound internet access.
# Gateway endpoints will bypass this for S3 and DynamoDB traffic.
# ------------------------------------------------------------------
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

# ------------------------------------------------------------------
# Route tables: public routes to IGW, private routes to NAT GW.
# The Gateway endpoints will add prefix list routes to the private
# route table automatically.
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

resource "aws_route_table_association" "private" {
  subnet_id      = aws_subnet.private.id
  route_table_id = aws_route_table.private.id
}
```

### `endpoints.tf`

```hcl
# ------------------------------------------------------------------
# S3 Gateway Endpoint: routes S3 traffic directly over the AWS
# backbone instead of through the NAT Gateway. This is free and
# eliminates NAT data processing charges for all S3 operations.
# ------------------------------------------------------------------
resource "aws_vpc_endpoint" "s3" {
  vpc_id            = aws_vpc.this.id
  service_name      = "com.amazonaws.${var.region}.s3"
  vpc_endpoint_type = "Gateway"

  route_table_ids = [
    aws_route_table.private.id,
    aws_route_table.public.id,
  ]

  tags = { Name = "${var.project_name}-s3-endpoint" }
}

# ------------------------------------------------------------------
# DynamoDB Gateway Endpoint: same concept as S3 -- DynamoDB traffic
# bypasses the NAT Gateway and flows over the AWS backbone for free.
# ------------------------------------------------------------------
resource "aws_vpc_endpoint" "dynamodb" {
  vpc_id            = aws_vpc.this.id
  service_name      = "com.amazonaws.${var.region}.dynamodb"
  vpc_endpoint_type = "Gateway"

  route_table_ids = [
    aws_route_table.private.id,
  ]

  tags = { Name = "${var.project_name}-dynamodb-endpoint" }
}
```

> **Best Practice:** Always associate Gateway endpoints with every route table that has resources accessing S3 or DynamoDB. If you miss a route table, traffic from those subnets will still flow through the NAT Gateway, incurring unnecessary charges.

### `iam.tf`

```hcl
# ------------------------------------------------------------------
# IAM role for the EC2 instance: grants read access to S3 and
# DynamoDB so we can verify endpoint connectivity.
# ------------------------------------------------------------------
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

  tags = { Name = "${var.project_name}-ec2-role" }
}

data "aws_iam_policy_document" "s3_dynamo_read" {
  statement {
    actions = [
      "s3:ListAllMyBuckets",
      "s3:GetBucketLocation",
    ]
    resources = ["*"]
  }

  statement {
    actions = [
      "dynamodb:ListTables",
      "dynamodb:DescribeTable",
    ]
    resources = ["*"]
  }
}

resource "aws_iam_role_policy" "this" {
  name   = "${var.project_name}-s3-dynamo-read"
  role   = aws_iam_role.this.id
  policy = data.aws_iam_policy_document.s3_dynamo_read.json
}

resource "aws_iam_instance_profile" "this" {
  name = "${var.project_name}-profile"
  role = aws_iam_role.this.name

  tags = { Name = "${var.project_name}-profile" }
}
```

### `security.tf`

```hcl
# ------------------------------------------------------------------
# Security group: allows all outbound traffic so the instance can
# reach S3, DynamoDB, and the internet. No inbound rules needed
# because we are not SSHing in -- we verify via AWS CLI outputs.
# ------------------------------------------------------------------
resource "aws_security_group" "instance" {
  name        = "${var.project_name}-instance-sg"
  description = "Allow all outbound for endpoint testing"
  vpc_id      = aws_vpc.this.id

  tags = { Name = "${var.project_name}-instance-sg" }
}

resource "aws_vpc_security_group_egress_rule" "all_out" {
  security_group_id = aws_security_group.instance.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}
```

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
# EC2 instance in the private subnet: used to verify that S3 and
# DynamoDB calls work through the Gateway endpoints.
# ------------------------------------------------------------------
resource "aws_instance" "this" {
  ami                    = data.aws_ami.amazon_linux_2023.id
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.private.id
  vpc_security_group_ids = [aws_security_group.instance.id]
  iam_instance_profile   = aws_iam_instance_profile.this.name

  user_data = <<-EOF
    #!/bin/bash
    yum install -y aws-cli
  EOF

  tags = { Name = "${var.project_name}-instance" }
}
```

### `outputs.tf`

```hcl
output "vpc_id" {
  description = "VPC ID"
  value       = aws_vpc.this.id
}

output "s3_endpoint_id" {
  description = "S3 Gateway endpoint ID"
  value       = aws_vpc_endpoint.s3.id
}

output "dynamodb_endpoint_id" {
  description = "DynamoDB Gateway endpoint ID"
  value       = aws_vpc_endpoint.dynamodb.id
}

output "instance_id" {
  description = "EC2 instance ID for SSM access"
  value       = aws_instance.this.id
}

output "private_route_table_id" {
  description = "Private route table ID (check for prefix list routes)"
  value       = aws_route_table.private.id
}
```

## Step 2 -- Initialize and Apply

```bash
terraform init
terraform apply -auto-approve
```

Terraform will create approximately 18 resources: the VPC, subnets, IGW, NAT Gateway, EIP, route tables, associations, security group, egress rule, IAM role, instance profile, EC2 instance, and the two Gateway endpoints.

### Intermediate Verification

Confirm the expected resource count:

```bash
terraform state list | wc -l
```

Expected: `18` (give or take depending on provider version)

```bash
terraform state list
```

You should see entries including:

```
aws_vpc.this
aws_subnet.public
aws_subnet.private
aws_internet_gateway.this
aws_nat_gateway.this
aws_eip.nat
aws_route_table.public
aws_route_table.private
aws_vpc_endpoint.s3
aws_vpc_endpoint.dynamodb
aws_instance.this
```

## Step 3 -- Verify Gateway Endpoint Routes

Gateway endpoints work by injecting prefix list routes into the associated route tables. Check the private route table to see these entries:

```bash
aws ec2 describe-route-tables \
  --route-table-ids $(terraform output -raw private_route_table_id) \
  --query "RouteTables[0].Routes[].{Dest:DestinationCidrBlock,PrefixList:DestinationPrefixListId,Target:GatewayId,NAT:NatGatewayId}" \
  --output table
```

Expected output (IDs will vary):

```
--------------------------------------------------------------------
|                       DescribeRouteTables                        |
+----------------+------------------+--------------+---------------+
|      Dest      |    NAT           | PrefixList   |    Target     |
+----------------+------------------+--------------+---------------+
|  10.0.0.0/16   |  None            |  None        |  local        |
|  0.0.0.0/0     |  nat-0abc123def  |  None        |  None         |
|  None          |  None            |  pl-63a5400a |  vpce-0abc... |
|  None          |  None            |  pl-02cd2c6b |  vpce-0def... |
+----------------+------------------+--------------+---------------+
```

The two `pl-` entries are prefix lists for S3 and DynamoDB. Traffic matching these prefixes goes directly to the VPC endpoint instead of the NAT Gateway.

> **Best Practice:** Gateway endpoints are free. There is zero reason not to create them in every VPC that has S3 or DynamoDB traffic. Even if the savings seem small today, they compound as your data transfer grows.

## Step 4 -- Verify S3 and DynamoDB Access

Confirm the endpoints are in the `available` state:

```bash
aws ec2 describe-vpc-endpoints \
  --filters "Name=vpc-id,Values=$(terraform output -raw vpc_id)" \
  --query "VpcEndpoints[].{Service:ServiceName,Type:VpcEndpointType,State:State}" \
  --output table
```

Expected:

```
--------------------------------------------------------------
|                   DescribeVpcEndpoints                     |
+-----------+-------------------------------------+----------+
|   State   |              Service                |  Type    |
+-----------+-------------------------------------+----------+
|  available|  com.amazonaws.us-east-1.s3         |  Gateway |
|  available|  com.amazonaws.us-east-1.dynamodb   |  Gateway |
+-----------+-------------------------------------+----------+
```

## Common Mistakes

### 1. Not associating the endpoint with all relevant route tables

If you create a Gateway endpoint but only associate it with one route table, subnets using other route tables still send S3/DynamoDB traffic through the NAT Gateway.

**Wrong -- only the private route table is associated:**

```hcl
resource "aws_vpc_endpoint" "s3" {
  vpc_id            = aws_vpc.this.id
  service_name      = "com.amazonaws.${var.region}.s3"
  vpc_endpoint_type = "Gateway"

  route_table_ids = [
    aws_route_table.private.id,
    # Missing: aws_route_table.public.id
  ]
}
```

**What happens:** Instances in the public subnet still route S3 traffic through the IGW (or NAT), potentially incurring data transfer charges and adding latency.

**Fix -- include all route tables that have resources accessing S3:**

```hcl
resource "aws_vpc_endpoint" "s3" {
  vpc_id            = aws_vpc.this.id
  service_name      = "com.amazonaws.${var.region}.s3"
  vpc_endpoint_type = "Gateway"

  route_table_ids = [
    aws_route_table.private.id,
    aws_route_table.public.id,
  ]
}
```

### 2. Confusing Gateway endpoints with Interface endpoints

Gateway endpoints are free and only work for S3 and DynamoDB. Interface endpoints cost ~$0.01/hr per AZ and work for most other AWS services. If you try to create a Gateway endpoint for a service that does not support it (like ECR or Secrets Manager), Terraform will fail.

**Wrong -- trying to create a Gateway endpoint for ECR:**

```hcl
resource "aws_vpc_endpoint" "ecr" {
  vpc_id            = aws_vpc.this.id
  service_name      = "com.amazonaws.${var.region}.ecr.api"
  vpc_endpoint_type = "Gateway"   # WRONG: ECR only supports Interface
  route_table_ids   = [aws_route_table.private.id]
}
```

**What happens:** Terraform returns an error:

```
Error: creating VPC Endpoint: InvalidParameter:
  The service com.amazonaws.us-east-1.ecr.api does not support the gateway endpoint type.
```

**Fix -- use Interface type for non-S3/DynamoDB services:**

```hcl
resource "aws_vpc_endpoint" "ecr" {
  vpc_id              = aws_vpc.this.id
  service_name        = "com.amazonaws.${var.region}.ecr.api"
  vpc_endpoint_type   = "Interface"
  subnet_ids          = [aws_subnet.private.id]
  security_group_ids  = [aws_security_group.instance.id]
  private_dns_enabled = true
}
```

### 3. Forgetting to enable DNS support on the VPC

Gateway endpoints rely on DNS resolution to map S3/DynamoDB hostnames to IP addresses within the prefix list. If `enable_dns_support` is `false`, the instance cannot resolve `s3.amazonaws.com` and the endpoint is useless.

**Wrong -- DNS support disabled:**

```hcl
resource "aws_vpc" "this" {
  cidr_block         = "10.0.0.0/16"
  enable_dns_support = false   # WRONG

  tags = { Name = "${var.project_name}-vpc" }
}
```

**What happens:** S3 CLI commands fail with DNS resolution errors even though the endpoint is correctly configured.

**Fix -- always enable DNS support:**

```hcl
resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = "${var.project_name}-vpc" }
}
```

## Verify What You Learned

```bash
terraform output s3_endpoint_id
```

Expected: a VPC endpoint ID starting with `vpce-`, e.g. `"vpce-0abc123def456789"`

```bash
terraform output dynamodb_endpoint_id
```

Expected: a second VPC endpoint ID starting with `vpce-`

```bash
aws ec2 describe-vpc-endpoints \
  --filters "Name=vpc-id,Values=$(terraform output -raw vpc_id)" \
  --query "VpcEndpoints[].{Service:ServiceName,State:State,RouteTableCount:length(RouteTableIds)}" \
  --output table
```

Expected:

```
------------------------------------------------------------------
|                    DescribeVpcEndpoints                         |
+------------------+-------------------------------------+-------+
| RouteTableCount  |              Service                | State |
+------------------+-------------------------------------+-------+
|  2               |  com.amazonaws.us-east-1.s3         | available |
|  1               |  com.amazonaws.us-east-1.dynamodb   | available |
+------------------+-------------------------------------+-------+
```

The S3 endpoint is associated with 2 route tables (public + private); the DynamoDB endpoint with 1 (private only).

```bash
aws ec2 describe-prefix-lists \
  --query "PrefixLists[?contains(PrefixListName,'s3') || contains(PrefixListName,'dynamodb')].{Name:PrefixListName,ID:PrefixListId}" \
  --output table
```

Expected: two prefix lists for S3 and DynamoDB with their IDs.

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

You created Gateway endpoints for S3 and DynamoDB -- the two services that support this free, route-based approach. In **Exercise 16 -- Interface Endpoints: Private API Access**, you will create Interface endpoints for services like ECR and Secrets Manager. These use ENIs (Elastic Network Interfaces) placed directly in your subnets and support private DNS, but they come with an hourly cost per AZ.

## Summary

- **Gateway endpoints** are free VPC endpoints available for S3 and DynamoDB only
- They work by adding **prefix list routes** to associated route tables, redirecting traffic to the AWS backbone
- Traffic through Gateway endpoints **bypasses the NAT Gateway**, eliminating data processing charges
- Always associate endpoints with **every route table** that has resources accessing S3 or DynamoDB
- Gateway endpoints require **DNS support** enabled on the VPC to function correctly
- There is **no bandwidth limit** on Gateway endpoints -- they scale automatically

## Reference

| Resource | Purpose |
|----------|---------|
| `aws_vpc_endpoint` | Creates a Gateway or Interface VPC endpoint |
| `aws_vpc` | The virtual network (DNS support must be enabled) |
| `aws_route_table` | Routes that receive prefix list entries from Gateway endpoints |
| `aws_iam_instance_profile` | Grants EC2 instances permission to call S3/DynamoDB |

## Additional Resources

- [Gateway VPC Endpoints (AWS Docs)](https://docs.aws.amazon.com/vpc/latest/privatelink/vpc-endpoints-s3.html) -- official guide to S3 and DynamoDB Gateway endpoints
- [VPC Endpoint Pricing](https://aws.amazon.com/privatelink/pricing/) -- pricing breakdown showing Gateway endpoints are free while Interface endpoints have hourly and data charges
- [Terraform aws_vpc_endpoint Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_endpoint) -- Terraform documentation for creating VPC endpoints
- [Reduce NAT Gateway Costs (AWS Blog)](https://aws.amazon.com/blogs/networking-and-content-delivery/reduce-nat-gateway-costs-by-using-vpc-endpoints/) -- strategies to minimize NAT Gateway data processing charges

## Apply Your Knowledge

- [AWS Well-Architected: Cost Optimization Pillar](https://docs.aws.amazon.com/wellarchitected/latest/cost-optimization-pillar/welcome.html) -- framework for evaluating cost optimization strategies including VPC endpoints
- [VPC Endpoint Policies](https://docs.aws.amazon.com/vpc/latest/privatelink/vpc-endpoints-access.html) -- restrict which S3 buckets or DynamoDB tables are accessible through an endpoint
- [S3 Gateway Endpoint Limitations](https://docs.aws.amazon.com/vpc/latest/privatelink/vpc-endpoints-s3.html#vpc-endpoints-s3-limitations) -- edge cases and limitations to consider in production

---

> *"The most dangerous phrase in the language is, 'We've always done it this way.'"*
> -- **Grace Hopper**

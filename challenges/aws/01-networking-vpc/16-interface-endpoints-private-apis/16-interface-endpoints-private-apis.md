# 16. Interface Endpoints: Private API Access

<!--
difficulty: intermediate
concepts: [interface-endpoints, privatelink, ecr, secrets-manager, private-dns, eni, security-groups]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: analyze
prerequisites: [01-your-first-vpc, 15-vpc-endpoints-s3-dynamodb]
aws_cost: ~$0.02/hr
-->

> **AWS Cost Warning:** This exercise creates Interface VPC endpoints (~$0.01/hr each) and a NAT Gateway (~$0.045/hr). Interface endpoints charge per AZ per hour plus data processing. Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Exercise 01 completed | Understand VPC basics |
| Exercise 15 completed | Understand Gateway endpoints |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Differentiate** between Gateway endpoints (free, route-based) and Interface endpoints (ENI-based, per-hour cost)
2. **Create** Interface endpoints for ECR, Secrets Manager, and CloudWatch Logs
3. **Analyze** how private DNS resolution maps public service hostnames to private endpoint IPs
4. **Configure** security groups on Interface endpoints to control which resources can use them
5. **Verify** that API calls stay within the VPC by inspecting ENI private IP addresses

## Why Interface Endpoints Matter

Gateway endpoints handle S3 and DynamoDB, but what about the other 100+ AWS services? When an EC2 instance in a private subnet calls `ecr.us-east-1.amazonaws.com`, that request travels through the NAT Gateway, out to the public internet, and back into the AWS network. Interface endpoints place an Elastic Network Interface (ENI) directly in your subnet with a private IP address. With private DNS enabled, the public hostname resolves to that private IP, so your application code does not need to change -- it just magically stays on the private network.

This matters for three reasons: security (traffic never leaves the AWS backbone), compliance (no data traverses the public internet), and cost (no NAT data processing charges for these API calls). The trade-off is that Interface endpoints cost ~$0.01/hr per AZ, so you should deploy them for services with high call volume or strict compliance requirements.

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
  default     = "interface-endpoints-lab"
}
```

### `vpc.tf`

```hcl
data "aws_availability_zones" "available" {
  state = "available"
}

locals {
  azs = slice(data.aws_availability_zones.available.names, 0, 2)
}

resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = "${var.project_name}-vpc" }
}

resource "aws_subnet" "private" {
  for_each = toset(local.azs)

  vpc_id            = aws_vpc.this.id
  cidr_block        = cidrsubnet("10.0.0.0/16", 8, index(local.azs, each.value) + 1)
  availability_zone = each.value

  tags = { Name = "${var.project_name}-private-${each.value}" }
}

resource "aws_subnet" "public" {
  vpc_id                  = aws_vpc.this.id
  cidr_block              = "10.0.10.0/24"
  availability_zone       = local.azs[0]
  map_public_ip_on_launch = true

  tags = { Name = "${var.project_name}-public" }
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

resource "aws_route_table_association" "private" {
  for_each = toset(local.azs)

  subnet_id      = aws_subnet.private[each.value].id
  route_table_id = aws_route_table.private.id
}
```

> **Best Practice:** Always enable both `enable_dns_support` and `enable_dns_hostnames` on your VPC when using Interface endpoints with private DNS. Without these, the private DNS override that maps public hostnames to endpoint ENI IPs will not work.

### `security.tf`

```hcl
# ------------------------------------------------------------------
# Security group for the Interface endpoints: controls which
# resources in the VPC can reach the endpoint ENIs.
# ------------------------------------------------------------------
resource "aws_security_group" "endpoints" {
  name        = "${var.project_name}-endpoints-sg"
  description = "Allow HTTPS from VPC CIDR to Interface endpoints"
  vpc_id      = aws_vpc.this.id

  tags = { Name = "${var.project_name}-endpoints-sg" }
}

resource "aws_vpc_security_group_ingress_rule" "endpoints_https" {
  security_group_id = aws_security_group.endpoints.id
  cidr_ipv4         = aws_vpc.this.cidr_block
  from_port         = 443
  to_port           = 443
  ip_protocol       = "tcp"
}

resource "aws_security_group" "instance" {
  name        = "${var.project_name}-instance-sg"
  description = "Allow all outbound for testing"
  vpc_id      = aws_vpc.this.id

  tags = { Name = "${var.project_name}-instance-sg" }
}

resource "aws_vpc_security_group_egress_rule" "instance_all_out" {
  security_group_id = aws_security_group.instance.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}
```

### `endpoints.tf`

```hcl
# =======================================================
# TODO 1 -- ECR API Interface Endpoint
# =======================================================
# Requirements:
#   - Create an aws_vpc_endpoint for "com.amazonaws.${var.region}.ecr.api"
#   - Set vpc_endpoint_type to "Interface"
#   - Place it in the private subnets (use [for s in aws_subnet.private : s.id])
#   - Attach the endpoints security group
#   - Enable private_dns
#   - Tag with Name = "${var.project_name}-ecr-api-endpoint"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_endpoint
# Hint: private_dns_enabled = true makes ecr.us-east-1.amazonaws.com resolve to the ENI IP


# =======================================================
# TODO 2 -- ECR Docker Interface Endpoint
# =======================================================
# Requirements:
#   - Create an aws_vpc_endpoint for "com.amazonaws.${var.region}.ecr.dkr"
#   - Same configuration as TODO 1 (Interface, private subnets, SG, private DNS)
#   - Tag with Name = "${var.project_name}-ecr-dkr-endpoint"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_endpoint
# Hint: ECR requires BOTH ecr.api and ecr.dkr endpoints to pull images privately


# =======================================================
# TODO 3 -- Secrets Manager Interface Endpoint
# =======================================================
# Requirements:
#   - Create an aws_vpc_endpoint for "com.amazonaws.${var.region}.secretsmanager"
#   - Interface type, private subnets, endpoints SG, private DNS enabled
#   - Tag with Name = "${var.project_name}-secrets-endpoint"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_endpoint


# =======================================================
# TODO 4 -- CloudWatch Logs Interface Endpoint
# =======================================================
# Requirements:
#   - Create an aws_vpc_endpoint for "com.amazonaws.${var.region}.logs"
#   - Interface type, private subnets, endpoints SG, private DNS enabled
#   - Tag with Name = "${var.project_name}-logs-endpoint"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_endpoint
# Hint: CloudWatch Logs endpoint lets instances send logs without NAT
```

### `s3-gateway.tf`

```hcl
# ------------------------------------------------------------------
# S3 Gateway Endpoint: ECR stores container images in S3, so pulling
# images also requires S3 access. A Gateway endpoint keeps this
# traffic free and off the NAT Gateway.
# ------------------------------------------------------------------
resource "aws_vpc_endpoint" "s3" {
  vpc_id            = aws_vpc.this.id
  service_name      = "com.amazonaws.${var.region}.s3"
  vpc_endpoint_type = "Gateway"

  route_table_ids = [aws_route_table.private.id]

  tags = { Name = "${var.project_name}-s3-endpoint" }
}
```

### `outputs.tf`

```hcl
output "vpc_id" {
  description = "VPC ID"
  value       = aws_vpc.this.id
}

output "endpoint_sg_id" {
  description = "Security group ID for Interface endpoints"
  value       = aws_security_group.endpoints.id
}

output "private_subnet_ids" {
  description = "Private subnet IDs"
  value       = [for s in aws_subnet.private : s.id]
}
```

## Spot the Bug

A colleague deployed Interface endpoints but applications in the private subnet cannot reach `secretsmanager.us-east-1.amazonaws.com`. The endpoint shows as `available`. **What is wrong?**

```hcl
resource "aws_security_group" "endpoints" {
  name   = "endpoint-sg"
  vpc_id = aws_vpc.this.id
}

resource "aws_vpc_security_group_ingress_rule" "endpoints_https" {
  security_group_id = aws_security_group.endpoints.id
  cidr_ipv4         = "10.0.10.0/24"  # <-- BUG
  from_port         = 443
  to_port           = 443
  ip_protocol       = "tcp"
}

resource "aws_vpc_endpoint" "secrets" {
  vpc_id              = aws_vpc.this.id
  service_name        = "com.amazonaws.us-east-1.secretsmanager"
  vpc_endpoint_type   = "Interface"
  subnet_ids          = [aws_subnet.private.id]
  security_group_ids  = [aws_security_group.endpoints.id]
  private_dns_enabled = true
}
```

<details>
<summary>Explain the bug</summary>

**The problem:** The security group ingress rule allows HTTPS only from `10.0.10.0/24` (the public subnet). The private subnets where applications run use `10.0.1.0/24` and `10.0.2.0/24`. The endpoint ENI rejects HTTPS connections from the private subnet because the security group does not permit them. The endpoint is `available` but unreachable from the application instances.

**The fix:** Allow HTTPS from the entire VPC CIDR so all subnets can reach the endpoint:

```hcl
resource "aws_vpc_security_group_ingress_rule" "endpoints_https" {
  security_group_id = aws_security_group.endpoints.id
  cidr_ipv4         = "10.0.0.0/16"
  from_port         = 443
  to_port           = 443
  ip_protocol       = "tcp"
}
```

</details>

## Solutions

<details>
<summary>TODO 1 -- ECR API Interface Endpoint (endpoints.tf)</summary>

```hcl
resource "aws_vpc_endpoint" "ecr_api" {
  vpc_id              = aws_vpc.this.id
  service_name        = "com.amazonaws.${var.region}.ecr.api"
  vpc_endpoint_type   = "Interface"
  subnet_ids          = [for s in aws_subnet.private : s.id]
  security_group_ids  = [aws_security_group.endpoints.id]
  private_dns_enabled = true

  tags = { Name = "${var.project_name}-ecr-api-endpoint" }
}
```

</details>

<details>
<summary>TODO 2 -- ECR Docker Interface Endpoint (endpoints.tf)</summary>

```hcl
resource "aws_vpc_endpoint" "ecr_dkr" {
  vpc_id              = aws_vpc.this.id
  service_name        = "com.amazonaws.${var.region}.ecr.dkr"
  vpc_endpoint_type   = "Interface"
  subnet_ids          = [for s in aws_subnet.private : s.id]
  security_group_ids  = [aws_security_group.endpoints.id]
  private_dns_enabled = true

  tags = { Name = "${var.project_name}-ecr-dkr-endpoint" }
}
```

</details>

<details>
<summary>TODO 3 -- Secrets Manager Interface Endpoint (endpoints.tf)</summary>

```hcl
resource "aws_vpc_endpoint" "secretsmanager" {
  vpc_id              = aws_vpc.this.id
  service_name        = "com.amazonaws.${var.region}.secretsmanager"
  vpc_endpoint_type   = "Interface"
  subnet_ids          = [for s in aws_subnet.private : s.id]
  security_group_ids  = [aws_security_group.endpoints.id]
  private_dns_enabled = true

  tags = { Name = "${var.project_name}-secrets-endpoint" }
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

## Verify What You Learned

### Step 1 -- Confirm all endpoints are available

```bash
aws ec2 describe-vpc-endpoints \
  --filters "Name=vpc-id,Values=$(terraform output -raw vpc_id)" \
  --query "VpcEndpoints[].{Service:ServiceName,Type:VpcEndpointType,State:State,DNS:PrivateDnsEnabled}" \
  --output table
```

Expected:

```
--------------------------------------------------------------------------------
|                           DescribeVpcEndpoints                               |
+-------+------+-------------------------------------------+------------------+
|  DNS  | State|                 Service                   |      Type        |
+-------+------+-------------------------------------------+------------------+
|  True | available | com.amazonaws.us-east-1.ecr.api      |  Interface       |
|  True | available | com.amazonaws.us-east-1.ecr.dkr      |  Interface       |
|  True | available | com.amazonaws.us-east-1.secretsmanager|  Interface       |
|  True | available | com.amazonaws.us-east-1.logs          |  Interface       |
|  False| available | com.amazonaws.us-east-1.s3            |  Gateway         |
+-------+------+-------------------------------------------+------------------+
```

### Step 2 -- Verify ENIs were created in private subnets

```bash
aws ec2 describe-vpc-endpoints \
  --filters "Name=vpc-id,Values=$(terraform output -raw vpc_id)" "Name=vpc-endpoint-type,Values=Interface" \
  --query "VpcEndpoints[0].NetworkInterfaceIds" \
  --output table
```

Expected: a list of ENI IDs (one per AZ per endpoint).

### Step 3 -- Inspect the endpoint DNS entries

```bash
aws ec2 describe-vpc-endpoints \
  --filters "Name=vpc-id,Values=$(terraform output -raw vpc_id)" "Name=service-name,Values=com.amazonaws.*.secretsmanager" \
  --query "VpcEndpoints[0].DnsEntries[].DnsName" \
  --output table
```

Expected: DNS names including the regional hostname (e.g., `secretsmanager.us-east-1.amazonaws.com`) and AZ-specific hostnames.

### Step 4 -- Verify the endpoint security group allows HTTPS

```bash
aws ec2 describe-security-group-rules \
  --filters "Name=group-id,Values=$(terraform output -raw endpoint_sg_id)" \
  --query "SecurityGroupRules[?IsEgress==\`false\`].{Port:FromPort,CIDR:CidrIpv4,Protocol:IpProtocol}" \
  --output table
```

Expected:

```
-------------------------------------
|  DescribeSecurityGroupRules       |
+--------+---------------+---------+
|  Port  |    CIDR       | Protocol|
+--------+---------------+---------+
|  443   |  10.0.0.0/16  |  tcp    |
+--------+---------------+---------+
```

### Step 5 -- Verify no changes pending

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

Destroy all resources to stop incurring charges (Interface endpoints cost ~$0.01/hr each per AZ):

```bash
terraform destroy -auto-approve
```

Verify nothing remains:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

In **Exercise 17 -- VPC Flow Logs: Traffic Analysis**, you will enable VPC Flow Logs to capture and analyze network traffic. Flow logs reveal which connections are being accepted or rejected, helping you debug security group rules, verify that endpoints are working, and audit network access patterns.

## Summary

- **Interface endpoints** create ENIs in your subnets with private IP addresses for AWS service APIs
- **Private DNS** makes public service hostnames (e.g., `secretsmanager.us-east-1.amazonaws.com`) resolve to the endpoint ENI IP within the VPC
- **Security groups on endpoints** control which resources can access the endpoint -- always allow HTTPS (443) from the VPC CIDR
- **ECR requires three endpoints**: `ecr.api`, `ecr.dkr`, and an S3 Gateway endpoint (images are stored in S3)
- Interface endpoints cost **~$0.01/hr per AZ** plus data processing -- deploy them for high-volume or compliance-sensitive services
- Both `enable_dns_support` and `enable_dns_hostnames` must be `true` on the VPC for private DNS to work

## Reference

| Resource | Purpose |
|----------|---------|
| `aws_vpc_endpoint` | Creates Gateway or Interface endpoints |
| `aws_security_group` | Controls access to Interface endpoint ENIs |
| `aws_vpc_security_group_ingress_rule` | Allows HTTPS to endpoint ENIs |
| `aws_subnet` | Hosts the endpoint ENIs (one per AZ) |

## Additional Resources

- [Interface VPC Endpoints (AWS Docs)](https://docs.aws.amazon.com/vpc/latest/privatelink/create-interface-endpoint.html) -- creating and managing Interface endpoints
- [AWS Services with VPC Endpoint Support](https://docs.aws.amazon.com/vpc/latest/privatelink/aws-services-privatelink-support.html) -- full list of services that support Interface endpoints
- [ECR Private Registry with VPC Endpoints](https://docs.aws.amazon.com/AmazonECR/latest/userguide/vpc-endpoints.html) -- specific guidance for ECR endpoint configuration
- [VPC Endpoint Policies](https://docs.aws.amazon.com/vpc/latest/privatelink/vpc-endpoints-access.html) -- restrict which API actions are allowed through an endpoint
- [Terraform aws_vpc_endpoint Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_endpoint) -- Terraform documentation for VPC endpoints

## Apply Your Knowledge

- [AWS PrivateLink Pricing](https://aws.amazon.com/privatelink/pricing/) -- calculate the cost of Interface endpoints vs NAT Gateway data processing for your workload
- [VPC Endpoint Policy Examples](https://docs.aws.amazon.com/vpc/latest/privatelink/vpc-endpoints-access.html#vpc-endpoint-policy-examples) -- add fine-grained access controls to endpoints
- [Troubleshooting VPC Endpoint Connectivity](https://repost.aws/knowledge-center/connect-s3-vpc-endpoint) -- common issues and debugging steps for endpoint connectivity

---

> *"Information is the resolution of uncertainty."*
> -- **Claude Shannon**

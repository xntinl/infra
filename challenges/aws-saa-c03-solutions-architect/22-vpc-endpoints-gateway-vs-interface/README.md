# 22. VPC Endpoints: Gateway vs Interface

<!--
difficulty: intermediate
concepts: [vpc-endpoint, gateway-endpoint, interface-endpoint, privatelink, s3-endpoint, dynamodb-endpoint, private-dns, eni-endpoint, security-group-endpoint]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: apply, analyze
prerequisites: [17-vpc-subnets-route-tables-igw, 19-security-groups-vs-nacls]
aws_cost: ~$0.02/hr
-->

> **AWS Cost Warning:** Gateway endpoints (S3, DynamoDB) are free. Interface endpoints cost ~$0.01/hr per AZ plus $0.01/GB of data processed. This exercise creates one interface endpoint in one AZ. Total ~$0.02/hr. Remember to run `terraform destroy` when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| Terraform >= 1.7 installed | `terraform version` |
| AWS CLI configured with a sandbox account | `aws sts get-caller-identity` |
| Understanding of VPC, subnets, route tables | Completed exercise 17 |
| Understanding of security groups | Completed exercise 19 |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Distinguish** Gateway endpoints (S3, DynamoDB -- free, route table entry) from Interface endpoints (most services -- ENI-based, ~$0.01/hr, security group).
2. **Construct** both Gateway and Interface VPC endpoints using Terraform and verify connectivity.
3. **Explain** how Gateway endpoints modify route tables while Interface endpoints create ENIs with private IPs.
4. **Configure** private DNS on Interface endpoints so that AWS service calls automatically use the endpoint without code changes.
5. **Evaluate** security implications: Interface endpoints can be restricted by security groups; Gateway endpoints use endpoint policies.
6. **Analyze** the cost impact of replacing NAT Gateway traffic with VPC endpoints for AWS service calls.

---

## Why This Matters

Without VPC endpoints, resources in private subnets must send traffic through a NAT Gateway to reach AWS services like S3, DynamoDB, or SQS. This creates two problems. First, cost: NAT Gateway charges $0.045/GB for data processing. If your Lambda functions pull 100 GB of S3 data per month, you pay $4.50 in NAT processing fees that a Gateway endpoint eliminates entirely. Second, security: traffic through NAT traverses the internet (albeit over TLS), while VPC endpoints keep traffic on the AWS backbone.

The SAA-C03 tests whether you know the two types. Gateway endpoints are available only for S3 and DynamoDB. They add a prefix list to your route table -- no ENI, no hourly cost. Interface endpoints use AWS PrivateLink: they create an ENI in your subnet with a private IP. Traffic to the service resolves to that ENI. Interface endpoints cost ~$0.01/hr per AZ and support security groups for fine-grained access control. The exam asks: "How do you access SQS from a private subnet without internet access?" -- the answer is an Interface endpoint, not a Gateway endpoint (Gateway is only for S3 and DynamoDB).

A critical configuration detail: Interface endpoints support `private_dns_enabled`. When true, the default service DNS name (like `sqs.us-east-1.amazonaws.com`) resolves to the endpoint's private IP. Your application code does not need to change. When false, you must use the endpoint-specific DNS name (like `vpce-abc123.sqs.us-east-1.vpce.amazonaws.com`). For the exam, private DNS is almost always the correct choice.

---

## Building Blocks

Create the following files in your exercise directory:

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
```

### `vpc.tf`

```hcl
data "aws_availability_zones" "available" {
  state = "available"
}

data "aws_region" "current" {}

# ==================================================================
# VPC with private subnets (no NAT Gateway -- endpoints only)
# ==================================================================
resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags                 = { Name = "endpoint-demo" }
}

resource "aws_subnet" "private_a" {
  vpc_id            = aws_vpc.this.id
  cidr_block        = "10.0.1.0/24"
  availability_zone = data.aws_availability_zones.available.names[0]
  tags              = { Name = "endpoint-demo-private-a" }
}

resource "aws_route_table" "private" {
  vpc_id = aws_vpc.this.id
  tags   = { Name = "endpoint-demo-private-rt" }
}

resource "aws_route_table_association" "private_a" {
  subnet_id      = aws_subnet.private_a.id
  route_table_id = aws_route_table.private.id
}

# ============================================================
# TODO 1: Gateway Endpoint for S3  [vpc.tf]
# ============================================================
# Create a Gateway VPC endpoint for S3.
#
# Requirements:
#   - Resource: aws_vpc_endpoint
#   - vpc_id = VPC ID
#   - service_name = "com.amazonaws.${data.aws_region.current.name}.s3"
#   - vpc_endpoint_type = "Gateway"
#   - route_table_ids = [private route table ID]
#
# Gateway endpoints are FREE and modify the route table by
# adding a prefix list entry (pl-xxxxx) that routes S3 traffic
# directly to the S3 service without leaving the VPC.
#
# Optional: Add a policy to restrict which S3 buckets can be
# accessed through this endpoint:
#   policy = jsonencode({
#     Version = "2012-10-17"
#     Statement = [{
#       Effect    = "Allow"
#       Principal = "*"
#       Action    = "s3:*"
#       Resource  = ["arn:aws:s3:::my-bucket", "arn:aws:s3:::my-bucket/*"]
#     }]
#   })
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_endpoint
# ============================================================


# ============================================================
# TODO 2: Gateway Endpoint for DynamoDB  [vpc.tf]
# ============================================================
# Create a Gateway VPC endpoint for DynamoDB.
#
# Requirements:
#   - Resource: aws_vpc_endpoint
#   - service_name = "com.amazonaws.${data.aws_region.current.name}.dynamodb"
#   - vpc_endpoint_type = "Gateway"
#   - route_table_ids = [private route table ID]
#
# DynamoDB is the other service that supports Gateway endpoints.
# Same behavior as S3: free, route table entry, no ENI.
# ============================================================


# ============================================================
# TODO 3: Interface Endpoint for SQS  [vpc.tf]
# ============================================================
# Create an Interface VPC endpoint for SQS.
#
# Requirements:
#   - Resource: aws_vpc_endpoint
#   - service_name = "com.amazonaws.${data.aws_region.current.name}.sqs"
#   - vpc_endpoint_type = "Interface"
#   - subnet_ids = [private subnet ID]
#   - security_group_ids = [endpoint security group ID]
#   - private_dns_enabled = true
#
# Interface endpoints create an ENI in your subnet with a
# private IP. When private_dns_enabled = true, the default
# service DNS (sqs.us-east-1.amazonaws.com) resolves to this
# private IP. Your code needs no changes.
#
# IMPORTANT: private_dns_enabled requires both enable_dns_support
# and enable_dns_hostnames to be true on the VPC.
#
# Docs: https://docs.aws.amazon.com/vpc/latest/privatelink/vpc-endpoints-access.html
# ============================================================
```

### `security.tf`

```hcl
# Security group for Interface endpoints
resource "aws_security_group" "endpoint" {
  name_prefix = "endpoint-demo-"
  vpc_id      = aws_vpc.this.id
  description = "Allow HTTPS to VPC endpoints"

  ingress {
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = [aws_vpc.this.cidr_block]
    description = "HTTPS from VPC"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "endpoint-demo-sg" }
}
```

### `outputs.tf`

```hcl
output "vpc_id" {
  value = aws_vpc.this.id
}

output "private_route_table_id" {
  value = aws_route_table.private.id
}
```

---

## Spot the Bug

A team creates an Interface endpoint for SQS but their Lambda function in a private subnet still cannot reach SQS:

```hcl
resource "aws_vpc_endpoint" "sqs" {
  vpc_id              = aws_vpc.this.id
  service_name        = "com.amazonaws.us-east-1.sqs"
  vpc_endpoint_type   = "Interface"
  subnet_ids          = [aws_subnet.private_a.id]
  security_group_ids  = [aws_security_group.endpoint.id]
  private_dns_enabled = false  # <-- Bug is here
}
```

<details>
<summary>Explain the bug</summary>

With `private_dns_enabled = false`, the default SQS DNS name (`sqs.us-east-1.amazonaws.com`) still resolves to a public IP. The Lambda function uses the AWS SDK, which calls the default DNS name. Since the private subnet has no NAT Gateway, the connection times out.

When `private_dns_enabled = false`, you must use the endpoint-specific DNS name:
```
vpce-abc123-def456.sqs.us-east-1.vpce.amazonaws.com
```

This requires code changes to configure the SDK with a custom endpoint URL:

```go
cfg, _ := config.LoadDefaultConfig(ctx, config.WithEndpointResolver(
    aws.EndpointResolverFunc(func(service, region string) (aws.Endpoint, error) {
        return aws.Endpoint{
            URL: "https://vpce-abc123-def456.sqs.us-east-1.vpce.amazonaws.com",
        }, nil
    }),
))
```

**The fix:** Set `private_dns_enabled = true`:

```hcl
resource "aws_vpc_endpoint" "sqs" {
  vpc_id              = aws_vpc.this.id
  service_name        = "com.amazonaws.us-east-1.sqs"
  vpc_endpoint_type   = "Interface"
  subnet_ids          = [aws_subnet.private_a.id]
  security_group_ids  = [aws_security_group.endpoint.id]
  private_dns_enabled = true  # Default DNS resolves to private IP
}
```

With this setting, `sqs.us-east-1.amazonaws.com` resolves to the ENI's private IP within the VPC. No code changes needed. This requires `enable_dns_support = true` and `enable_dns_hostnames = true` on the VPC.

</details>

---

## Solutions

<details>
<summary>vpc.tf -- TODO 1: Gateway Endpoint for S3</summary>

```hcl
resource "aws_vpc_endpoint" "s3" {
  vpc_id            = aws_vpc.this.id
  service_name      = "com.amazonaws.${data.aws_region.current.name}.s3"
  vpc_endpoint_type = "Gateway"
  route_table_ids   = [aws_route_table.private.id]

  tags = { Name = "endpoint-demo-s3" }
}
```

After applying, check the route table:

```bash
aws ec2 describe-route-tables \
  --route-table-ids $(terraform output -raw private_route_table_id) \
  --query "RouteTables[0].Routes[*].{Dest:DestinationPrefixListId,Target:GatewayId}" \
  --output table
```

You will see a prefix list entry (pl-xxxxx) pointing to the gateway endpoint. The prefix list contains all S3 IP ranges for the region.

</details>

<details>
<summary>vpc.tf -- TODO 2: Gateway Endpoint for DynamoDB</summary>

```hcl
resource "aws_vpc_endpoint" "dynamodb" {
  vpc_id            = aws_vpc.this.id
  service_name      = "com.amazonaws.${data.aws_region.current.name}.dynamodb"
  vpc_endpoint_type = "Gateway"
  route_table_ids   = [aws_route_table.private.id]

  tags = { Name = "endpoint-demo-dynamodb" }
}
```

</details>

<details>
<summary>vpc.tf -- TODO 3: Interface Endpoint for SQS</summary>

```hcl
resource "aws_vpc_endpoint" "sqs" {
  vpc_id              = aws_vpc.this.id
  service_name        = "com.amazonaws.${data.aws_region.current.name}.sqs"
  vpc_endpoint_type   = "Interface"
  subnet_ids          = [aws_subnet.private_a.id]
  security_group_ids  = [aws_security_group.endpoint.id]
  private_dns_enabled = true

  tags = { Name = "endpoint-demo-sqs" }
}
```

Verify the ENI was created:

```bash
aws ec2 describe-vpc-endpoints \
  --filters "Name=tag:Name,Values=endpoint-demo-sqs" \
  --query "VpcEndpoints[0].{Id:VpcEndpointId,Type:VpcEndpointType,DNS:DnsEntries[0].DnsName,PrivateDns:PrivateDnsEnabled}" \
  --output table
```

</details>

---

## Decision Table: Gateway vs Interface Endpoints

| Feature | Gateway Endpoint | Interface Endpoint |
|---------|-----------------|-------------------|
| **Services** | S3 and DynamoDB only | 100+ services (SQS, SNS, KMS, etc.) |
| **Cost** | Free | ~$0.01/hr per AZ + $0.01/GB |
| **Mechanism** | Route table prefix list | ENI with private IP |
| **Security** | Endpoint policy only | Security groups + endpoint policy |
| **DNS** | No change needed | Private DNS or custom endpoint URL |
| **Cross-region** | No | No |
| **VPC requirement** | Route table association | Subnet + security group |
| **Use case** | Always use for S3/DynamoDB | When private subnet needs AWS service access |

### Cost Savings: VPC Endpoint vs NAT Gateway

| Monthly S3 traffic | NAT GW processing fee | Gateway endpoint cost | Savings |
|-------------------|-----------------------|----------------------|---------|
| 10 GB | $0.45 | $0.00 | $0.45 |
| 100 GB | $4.50 | $0.00 | $4.50 |
| 1 TB | $46.08 | $0.00 | $46.08 |
| 10 TB | $460.80 | $0.00 | $460.80 |

---

## Verify What You Learned

```bash
# Verify Gateway endpoints exist
aws ec2 describe-vpc-endpoints \
  --filters "Name=vpc-id,Values=$(terraform output -raw vpc_id)" "Name=vpc-endpoint-type,Values=Gateway" \
  --query "VpcEndpoints[*].{Service:ServiceName,Type:VpcEndpointType,State:State}" \
  --output table
```

Expected: 2 Gateway endpoints (S3 and DynamoDB), both in state "available".

```bash
# Verify Interface endpoint exists with private DNS
aws ec2 describe-vpc-endpoints \
  --filters "Name=vpc-id,Values=$(terraform output -raw vpc_id)" "Name=vpc-endpoint-type,Values=Interface" \
  --query "VpcEndpoints[*].{Service:ServiceName,PrivateDns:PrivateDnsEnabled,State:State}" \
  --output table
```

Expected: 1 Interface endpoint (SQS) with PrivateDns=True, state "available".

```bash
# Verify route table has prefix list entries for Gateway endpoints
aws ec2 describe-route-tables \
  --route-table-ids $(terraform output -raw private_route_table_id) \
  --query "RouteTables[0].Routes[?GatewayId!=null && GatewayId!='local'].{PrefixList:DestinationPrefixListId,Gateway:GatewayId}" \
  --output table
```

Expected: 2 entries with prefix list IDs pointing to vpce-xxx gateway IDs.

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify:

```bash
terraform state list
```

Expected: no output (empty state).

---

## What's Next

You deployed both types of VPC endpoints for accessing AWS services privately. In the next exercise, you will use **AWS PrivateLink** to expose your own services across VPC boundaries -- creating a VPC endpoint service backed by a Network Load Balancer that consumers in other VPCs can connect to without VPC peering.

---

## Summary

- **Gateway endpoints** are free and available only for S3 and DynamoDB -- they add a prefix list to your route table
- **Interface endpoints** cost ~$0.01/hr per AZ, create an ENI with a private IP, and support 100+ AWS services
- `private_dns_enabled = true` makes the default service DNS resolve to the endpoint's private IP -- **no code changes needed**
- Private DNS requires `enable_dns_support` and `enable_dns_hostnames` on the VPC
- Interface endpoints support **security groups** for fine-grained access control; Gateway endpoints use **endpoint policies**
- Gateway endpoints for S3 **eliminate NAT Gateway processing fees** ($0.045/GB) -- always create them for private subnets
- VPC endpoints keep traffic on the **AWS backbone** -- it never traverses the internet
- For the exam: S3 and DynamoDB = Gateway endpoint; everything else = Interface endpoint

## Reference

- [VPC Endpoints](https://docs.aws.amazon.com/vpc/latest/privatelink/vpc-endpoints.html)
- [Gateway Endpoints](https://docs.aws.amazon.com/vpc/latest/privatelink/vpc-endpoints-s3.html)
- [Interface Endpoints](https://docs.aws.amazon.com/vpc/latest/privatelink/vpc-endpoints-access.html)
- [Terraform aws_vpc_endpoint](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_endpoint)

## Additional Resources

- [VPC Endpoint Policies](https://docs.aws.amazon.com/vpc/latest/privatelink/vpc-endpoints-access.html) -- restricting which S3 buckets or DynamoDB tables can be accessed through an endpoint
- [Interface Endpoint Pricing](https://aws.amazon.com/privatelink/pricing/) -- per-AZ hourly cost and data processing fees
- [Available AWS Services for Interface Endpoints](https://docs.aws.amazon.com/vpc/latest/privatelink/aws-services-privatelink-support.html) -- full list of supported services
- [Troubleshooting VPC Endpoints](https://docs.aws.amazon.com/vpc/latest/privatelink/vpc-endpoints-troubleshoot.html) -- DNS resolution issues, security group rules, endpoint policies

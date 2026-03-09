# 62. VPC Endpoints: Gateway vs Interface

<!--
difficulty: intermediate
concepts: [vpc-gateway-endpoint, vpc-interface-endpoint, privatelink, private-dns, route-table, security-groups, lambda-vpc, s3-endpoint, sqs-endpoint]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: apply, differentiate
prerequisites: [21-lambda-vpc-networking-nat-access]
aws_cost: ~$0.02/hr
-->

> **AWS Cost Warning:** This exercise creates a VPC, subnets, an interface endpoint (~$0.01/hr per AZ), a Lambda function, and associated networking resources. Gateway endpoints are free. Total cost is approximately $0.02/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| jq installed | `jq --version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Differentiate** between gateway endpoints (S3, DynamoDB -- free, route table entries) and interface endpoints (most services -- ENI, private DNS, security groups, ~$0.01/hr per AZ)
2. **Implement** a gateway endpoint for S3 with route table association using Terraform
3. **Implement** an interface endpoint for SQS with private DNS enabled using Terraform
4. **Configure** a Lambda function in a VPC that accesses both S3 and SQS through their respective endpoints
5. **Analyze** the network path differences: gateway endpoints modify route tables while interface endpoints create ENIs with private IP addresses

## Why VPC Endpoints

When a Lambda function runs inside a VPC, it has no internet access by default. Without a NAT gateway, VPC-attached Lambda functions cannot reach AWS service endpoints (which are public). VPC endpoints solve this by providing private connectivity to AWS services without leaving the AWS network.

**Gateway endpoints** exist only for S3 and DynamoDB. They are free, require no ENI, and work by adding a route table entry that directs traffic destined for the service to the endpoint. You cannot assign security groups to a gateway endpoint -- access is controlled via endpoint policies and bucket/table policies.

**Interface endpoints** (powered by PrivateLink) work for most other AWS services (SQS, SNS, KMS, Secrets Manager, etc.). They create an ENI in your subnet with a private IP address. You can attach security groups to control traffic. When private DNS is enabled, calls to the standard service endpoint (e.g., `sqs.us-east-1.amazonaws.com`) resolve to the private ENI IP instead of the public IP. Without private DNS, you must use the endpoint-specific DNS name (`vpce-xxx.sqs.us-east-1.vpce.amazonaws.com`).

The DVA-C02 exam tests when to use each type, the cost implications (gateway = free, interface = hourly + data processing), and the private DNS requirement for transparent integration.

## Building Blocks

Create the following files in your exercise directory. Your job is to fill in each `# TODO` block.

### `providers.tf`

```hcl
terraform {
  required_providers {
    aws     = { source = "hashicorp/aws", version = "~> 5.0" }
    archive = { source = "hashicorp/archive", version = "~> 2.0" }
    null    = { source = "hashicorp/null", version = "~> 3.0" }
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
  default     = "vpc-endpoints-demo"
}
```

### `vpc.tf`

```hcl
data "aws_caller_identity" "current" {}
data "aws_region" "current" {}

resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags = { Name = "${var.project_name}-vpc" }
}

resource "aws_subnet" "private_a" {
  vpc_id            = aws_vpc.this.id
  cidr_block        = "10.0.1.0/24"
  availability_zone = "${data.aws_region.current.name}a"
}

resource "aws_subnet" "private_b" {
  vpc_id            = aws_vpc.this.id
  cidr_block        = "10.0.2.0/24"
  availability_zone = "${data.aws_region.current.name}b"
}

resource "aws_route_table" "private" {
  vpc_id = aws_vpc.this.id
}

resource "aws_route_table_association" "private_a" {
  subnet_id      = aws_subnet.private_a.id
  route_table_id = aws_route_table.private.id
}

resource "aws_route_table_association" "private_b" {
  subnet_id      = aws_subnet.private_b.id
  route_table_id = aws_route_table.private.id
}

# =======================================================
# TODO 1 -- Gateway Endpoint for S3
# =======================================================
# Create a gateway endpoint for S3 and associate it
# with the private route table.
#
# Requirements:
#   - service_name = "com.amazonaws.${data.aws_region.current.name}.s3"
#   - vpc_endpoint_type = "Gateway"
#   - route_table_ids = [aws_route_table.private.id]
#
# Gateway endpoints are FREE and add a prefix list route
# to the route table. No ENI is created.
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_endpoint


# =======================================================
# TODO 3 -- Interface Endpoint for SQS
# =======================================================
# Create an interface endpoint for SQS with private DNS.
#
# Requirements:
#   - service_name = "com.amazonaws.${data.aws_region.current.name}.sqs"
#   - vpc_endpoint_type = "Interface"
#   - subnet_ids = [private subnets]
#   - security_group_ids = [endpoint security group]
#   - private_dns_enabled = true
#
# With private DNS, the standard sqs.us-east-1.amazonaws.com
# DNS name resolves to the private ENI IP within the VPC.
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_endpoint
```

### `security.tf`

```hcl
# -- Lambda Security Group --
resource "aws_security_group" "lambda" {
  name   = "${var.project_name}-lambda-sg"
  vpc_id = aws_vpc.this.id

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "${var.project_name}-lambda-sg" }
}

# =======================================================
# TODO 2 -- Security Group for Interface Endpoint
# =======================================================
# Create a security group for the SQS interface endpoint.
# The endpoint ENI needs to accept HTTPS (443) traffic
# from the Lambda function's security group.
#
# Requirements:
#   - Ingress: port 443 from the Lambda security group
#   - Egress: all traffic (or port 443 to 0.0.0.0/0)
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/security_group
```

### `storage.tf`

```hcl
resource "aws_s3_bucket" "this" {
  bucket        = "${var.project_name}-${data.aws_caller_identity.current.account_id}"
  force_destroy = true
}

resource "aws_s3_object" "test" {
  bucket  = aws_s3_bucket.this.id
  key     = "test.txt"
  content = "Accessed via gateway endpoint!"
}
```

### `events.tf`

```hcl
resource "aws_sqs_queue" "this" {
  name = "${var.project_name}-queue"
}
```

### `iam.tf`

```hcl
data "aws_iam_policy_document" "lambda_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals { type = "Service"; identifiers = ["lambda.amazonaws.com"] }
  }
}

resource "aws_iam_role" "lambda" {
  name               = "${var.project_name}-lambda-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

resource "aws_iam_role_policy_attachment" "basic" {
  role       = aws_iam_role.lambda.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_iam_role_policy_attachment" "vpc" {
  role       = aws_iam_role.lambda.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaVPCAccessExecutionRole"
}

data "aws_iam_policy_document" "lambda_permissions" {
  statement {
    actions   = ["s3:GetObject", "s3:ListBucket"]
    resources = [aws_s3_bucket.this.arn, "${aws_s3_bucket.this.arn}/*"]
  }
  statement {
    actions   = ["sqs:SendMessage", "sqs:ReceiveMessage", "sqs:GetQueueUrl"]
    resources = [aws_sqs_queue.this.arn]
  }
}

resource "aws_iam_role_policy" "lambda_permissions" {
  name   = "${var.project_name}-permissions"
  role   = aws_iam_role.lambda.id
  policy = data.aws_iam_policy_document.lambda_permissions.json
}
```

### `build.tf`

```hcl
# Build: GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
# main.go: Go Lambda in VPC that tests S3 GetObject (via gateway endpoint)
# and SQS SendMessage (via interface endpoint). Env vars: BUCKET_NAME, QUEUE_NAME.

resource "null_resource" "go_build" {
  triggers = { source_hash = filebase64sha256("${path.module}/main.go") }
  provisioner "local-exec" {
    command     = "GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go"
    working_dir = path.module
  }
}

data "archive_file" "function_zip" {
  type        = "zip"
  source_file = "${path.module}/bootstrap"
  output_path = "${path.module}/build/function.zip"
  depends_on  = [null_resource.go_build]
}
```

### `lambda.tf`

```hcl
resource "aws_cloudwatch_log_group" "this" {
  name              = "/aws/lambda/${var.project_name}"
  retention_in_days = 1
}

resource "aws_lambda_function" "this" {
  function_name    = var.project_name
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.lambda.arn
  timeout          = 30

  vpc_config {
    subnet_ids         = [aws_subnet.private_a.id, aws_subnet.private_b.id]
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = {
      BUCKET_NAME = aws_s3_bucket.this.id
      QUEUE_NAME  = aws_sqs_queue.this.name
    }
  }

  depends_on = [
    aws_iam_role_policy_attachment.basic,
    aws_iam_role_policy_attachment.vpc,
    aws_cloudwatch_log_group.this,
  ]
}
```

### `outputs.tf`

```hcl
output "function_name" { value = aws_lambda_function.this.function_name }
output "bucket_name"   { value = aws_s3_bucket.this.id }
output "queue_name"    { value = aws_sqs_queue.this.name }
```

## Spot the Bug

A developer creates an interface endpoint for SQS but does not enable private DNS. The Lambda function in the VPC gets connection timeouts when calling `sqs.us-east-1.amazonaws.com`:

```hcl
resource "aws_vpc_endpoint" "sqs" {
  vpc_id              = aws_vpc.this.id
  service_name        = "com.amazonaws.us-east-1.sqs"
  vpc_endpoint_type   = "Interface"
  subnet_ids          = [aws_subnet.private_a.id]
  security_group_ids  = [aws_security_group.endpoint.id]
  private_dns_enabled = false                              # <-- BUG
}
```

<details>
<summary>Explain the bug</summary>

Without `private_dns_enabled = true`, the standard SQS endpoint DNS name (`sqs.us-east-1.amazonaws.com`) still resolves to the public IP address. Since the Lambda function is in a VPC with no internet access (no NAT gateway), the connection times out because it cannot reach the public endpoint.

With private DNS disabled, you must use the endpoint-specific DNS name: `vpce-0abc123def.sqs.us-east-1.vpce.amazonaws.com`. However, the AWS SDK uses the standard DNS name by default, so you would need to configure a custom endpoint URL in the SDK client -- which is error-prone and not the intended usage pattern.

**Fix -- enable private DNS:**

```hcl
resource "aws_vpc_endpoint" "sqs" {
  vpc_id              = aws_vpc.this.id
  service_name        = "com.amazonaws.us-east-1.sqs"
  vpc_endpoint_type   = "Interface"
  subnet_ids          = [aws_subnet.private_a.id]
  security_group_ids  = [aws_security_group.endpoint.id]
  private_dns_enabled = true   # Standard DNS resolves to private ENI IP
}
```

Note: Private DNS requires `enable_dns_support = true` and `enable_dns_hostnames = true` on the VPC.

</details>

## Solutions

<details>
<summary>TODO 1 -- Gateway Endpoint for S3 (`vpc.tf`)</summary>

```hcl
resource "aws_vpc_endpoint" "s3" {
  vpc_id            = aws_vpc.this.id
  service_name      = "com.amazonaws.${data.aws_region.current.name}.s3"
  vpc_endpoint_type = "Gateway"
  route_table_ids   = [aws_route_table.private.id]

  tags = { Name = "${var.project_name}-s3-endpoint" }
}
```

</details>

<details>
<summary>TODO 2 -- Security Group for Interface Endpoint (`security.tf`)</summary>

```hcl
resource "aws_security_group" "endpoint" {
  name   = "${var.project_name}-endpoint-sg"
  vpc_id = aws_vpc.this.id

  ingress {
    from_port       = 443
    to_port         = 443
    protocol        = "tcp"
    security_groups = [aws_security_group.lambda.id]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "${var.project_name}-endpoint-sg" }
}
```

</details>

<details>
<summary>TODO 3 -- Interface Endpoint for SQS (`vpc.tf`)</summary>

```hcl
resource "aws_vpc_endpoint" "sqs" {
  vpc_id              = aws_vpc.this.id
  service_name        = "com.amazonaws.${data.aws_region.current.name}.sqs"
  vpc_endpoint_type   = "Interface"
  subnet_ids          = [aws_subnet.private_a.id, aws_subnet.private_b.id]
  security_group_ids  = [aws_security_group.endpoint.id]
  private_dns_enabled = true

  tags = { Name = "${var.project_name}-sqs-endpoint" }
}
```

</details>

## Verify What You Learned

### Step 1 -- Deploy

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init && terraform apply -auto-approve
```

### Step 2 -- Invoke the Lambda

```bash
aws lambda invoke --function-name $(terraform output -raw function_name) \
  --payload '{}' /dev/stdout 2>/dev/null | jq .
```

Expected: both S3 and SQS operations succeed via their respective endpoints.

### Step 3 -- Verify endpoints

```bash
# List all endpoints in the VPC
VPC_ID=$(aws ec2 describe-vpcs --filters "Name=tag:Name,Values=vpc-endpoints-demo" --query "Vpcs[0].VpcId" --output text)
aws ec2 describe-vpc-endpoints --filters "Name=vpc-id,Values=$VPC_ID" \
  --query "VpcEndpoints[].{Type:VpcEndpointType,Service:ServiceName,State:State}" --output table
```

Expected: one Gateway (S3) and one Interface (SQS) endpoint, both `available`.

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

You configured both gateway and interface VPC endpoints for private connectivity to AWS services. In the next exercise, you will explore **Lambda resource-based policies** -- controlling which AWS services and accounts can invoke your Lambda functions.

## Summary

- **Gateway endpoints** (S3, DynamoDB): free, route table-based, no ENI, no security groups, controlled via endpoint policies
- **Interface endpoints** (most services): ~$0.01/hr per AZ + data processing, ENI-based, security groups required, private DNS recommended
- **Private DNS** is critical for interface endpoints -- without it, standard SDK calls resolve to public IPs and fail in isolated VPCs
- Private DNS requires VPC settings: `enable_dns_support = true` and `enable_dns_hostnames = true`
- Gateway endpoints add a **prefix list route** to the route table; interface endpoints create **ENIs with private IPs** in your subnets
- VPC-attached Lambda functions need endpoints (or NAT gateway) to reach AWS services
- Gateway endpoints are the **preferred** choice for S3 and DynamoDB due to zero cost and no bandwidth limits

## Reference

- [VPC Gateway Endpoints](https://docs.aws.amazon.com/vpc/latest/privatelink/vpc-endpoints-s3.html)
- [VPC Interface Endpoints](https://docs.aws.amazon.com/vpc/latest/privatelink/create-interface-endpoint.html)
- [Terraform aws_vpc_endpoint](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_endpoint)
- [Lambda VPC Access](https://docs.aws.amazon.com/lambda/latest/dg/configuration-vpc.html)

## Additional Resources

- [Gateway vs Interface Endpoints](https://docs.aws.amazon.com/vpc/latest/privatelink/gateway-endpoints.html) -- official comparison
- [VPC Endpoint Policies](https://docs.aws.amazon.com/vpc/latest/privatelink/vpc-endpoints-access.html) -- restricting endpoint access to specific buckets or resources
- [PrivateLink Pricing](https://aws.amazon.com/privatelink/pricing/) -- interface endpoint cost details
- [DNS Resolution for Endpoints](https://docs.aws.amazon.com/vpc/latest/privatelink/manage-dns-names.html) -- how private DNS works

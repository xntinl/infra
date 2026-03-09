# 21. Lambda VPC Networking and NAT Access

<!--
difficulty: intermediate
concepts: [lambda-vpc, nat-gateway, vpc-endpoint, private-subnet, security-groups, eni, hyperplane]
tools: [terraform, aws-cli]
estimated_time: 50m
bloom_level: design, implement, justify
prerequisites: [none]
aws_cost: ~$0.05/hr
-->

> **AWS Cost Warning:** This exercise creates a NAT Gateway ($0.045/hr) and a VPC with associated networking resources. Total cost is approximately $0.05/hr. Remember to run `terraform destroy` when finished to avoid charges.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| jq installed | `jq --version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Design** a VPC network topology that allows a Lambda function in a private subnet to access both the internet (via NAT Gateway) and AWS services (via VPC endpoints)
2. **Implement** Lambda VPC configuration with `vpc_config` specifying `subnet_ids` and `security_group_ids` in Terraform
3. **Justify** why Lambda functions in a VPC require a NAT Gateway for internet access and why they never receive public IP addresses, even in a public subnet
4. **Configure** a VPC Gateway Endpoint for DynamoDB to avoid sending DynamoDB traffic through the NAT Gateway (reducing cost and latency)
5. **Differentiate** between Gateway Endpoints (S3, DynamoDB -- free, route-table based) and Interface Endpoints (other services -- ENI-based, hourly charge)

## Why This Matters

When you attach a Lambda function to a VPC, it creates an Elastic Network Interface (ENI) in the subnets you specify. This ENI is in a private address space, so the function loses internet access unless you route traffic through a NAT Gateway. This is the most commonly misunderstood Lambda networking concept, and it appears on nearly every DVA-C02 exam.

The critical insight: Lambda functions in a VPC **never** receive public IP addresses, even if placed in a public subnet with an Internet Gateway. Unlike EC2 instances, Lambda ENIs cannot be assigned Elastic IPs or auto-assigned public IPs. The only path to the internet is through a NAT Gateway in a public subnet. Placing a Lambda function in a public subnet is a common mistake -- it has no internet access and gains nothing from the IGW.

Since 2019, Lambda uses AWS Hyperplane (the same technology behind NLB and PrivateLink) to create ENIs. This dramatically reduced cold start penalties for VPC-attached functions from 5-10 seconds to approximately 1 second. The ENIs are shared across execution environments for the same function, rather than creating one ENI per concurrent invocation.

## Building Blocks

Create the Lambda function code in a file called `main.go`:

### `main.go`

```go
// main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/aws"
)

func handler(ctx context.Context, event json.RawMessage) (map[string]interface{}, error) {
	result := map[string]interface{}{
		"function_name": os.Getenv("AWS_LAMBDA_FUNCTION_NAME"),
	}

	// Test 1: Internet access via NAT Gateway
	internetResult := testInternet()
	result["internet_access"] = internetResult

	// Test 2: DynamoDB access via VPC Endpoint
	dynamoResult := testDynamoDB(ctx)
	result["dynamodb_access"] = dynamoResult

	return result, nil
}

func testInternet() map[string]interface{} {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("https://httpbin.org/ip")
	if err != nil {
		return map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return map[string]interface{}{
		"success":     true,
		"status_code": resp.StatusCode,
		"response":    string(body),
	}
}

func testDynamoDB(ctx context.Context) map[string]interface{} {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}
	}
	client := dynamodb.NewFromConfig(cfg)

	tableName := os.Getenv("TABLE_NAME")
	_, err = client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(tableName),
		Item: map[string]types.AttributeValue{
			"pk":        &types.AttributeValueMemberS{Value: "test-from-vpc-lambda"},
			"timestamp": &types.AttributeValueMemberS{Value: fmt.Sprintf("%d", time.Now().Unix())},
		},
	})
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}
	}
	return map[string]interface{}{
		"success": true,
		"message": "Successfully wrote to DynamoDB via VPC Endpoint",
	}
}

func main() {
	lambda.Start(handler)
}
```

Create the following Terraform files. Your job is to fill in each `# TODO` block.

### `providers.tf`

```hcl
terraform {
  required_version = ">= 1.5"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    archive = {
      source  = "hashicorp/archive"
      version = "~> 2.0"
    }
    null = {
      source  = "hashicorp/null"
      version = "~> 3.0"
    }
  }
}

provider "aws" { region = var.region }
```

### `variables.tf`

```hcl
variable "region" {
  description = "AWS region for all resources"
  type        = string
  default     = "us-east-1"
}

variable "project_name" {
  description = "Project name used for resource naming"
  type        = string
  default     = "vpc-lambda-demo"
}
```

### `vpc.tf`

```hcl
# -------------------------------------------------------
# VPC and Networking
# -------------------------------------------------------
resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags = { Name = "${var.project_name}" }
}

resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id
  tags   = { Name = "${var.project_name}-igw" }
}

# Public subnet (for NAT Gateway)
resource "aws_subnet" "public" {
  vpc_id                  = aws_vpc.this.id
  cidr_block              = "10.0.1.0/24"
  availability_zone       = "${var.region}a"
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

# Private subnets (for Lambda)
resource "aws_subnet" "private_a" {
  vpc_id            = aws_vpc.this.id
  cidr_block        = "10.0.10.0/24"
  availability_zone = "${var.region}a"
  tags = { Name = "${var.project_name}-private-a" }
}

resource "aws_subnet" "private_b" {
  vpc_id            = aws_vpc.this.id
  cidr_block        = "10.0.11.0/24"
  availability_zone = "${var.region}b"
  tags = { Name = "${var.project_name}-private-b" }
}

# =======================================================
# TODO 1 -- NAT Gateway with Elastic IP
# =======================================================
# Requirements:
#   - Create an aws_eip for the NAT Gateway
#   - Create an aws_nat_gateway in the PUBLIC subnet
#   - The NAT Gateway must be in a public subnet (with IGW
#     route) so it can forward traffic to the internet
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/nat_gateway


# =======================================================
# TODO 2 -- Private Route Table with NAT Gateway Route
# =======================================================
# Requirements:
#   - Create an aws_route_table for the private subnets
#   - Add a default route (0.0.0.0/0) pointing to the
#     NAT Gateway
#   - Associate both private subnets with this route table
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route_table


# =======================================================
# TODO 3 -- Security Group for Lambda
# =======================================================
# Requirements:
#   - Create an aws_security_group in the VPC
#   - Allow ALL outbound traffic (egress 0.0.0.0/0)
#   - No inbound rules needed (Lambda initiates connections)
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/security_group


# =======================================================
# TODO 4 -- VPC Gateway Endpoint for DynamoDB
# =======================================================
# Requirements:
#   - Create an aws_vpc_endpoint for DynamoDB
#   - Set service_name to "com.amazonaws.us-east-1.dynamodb"
#   - Set vpc_endpoint_type = "Gateway"
#   - Associate with the private route table
#   - Gateway endpoints are free and route traffic
#     directly without going through NAT Gateway
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_endpoint
```

### `database.tf`

```hcl
# -------------------------------------------------------
# DynamoDB table for testing
# -------------------------------------------------------
resource "aws_dynamodb_table" "this" {
  name         = var.project_name
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "pk"
  attribute { name = "pk"; type = "S" }
  tags = { Name = var.project_name }
}
```

### `build.tf`

```hcl
# -------------------------------------------------------
# Build and package
# -------------------------------------------------------
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

### `iam.tf`

```hcl
# -------------------------------------------------------
# IAM
# -------------------------------------------------------
data "aws_iam_policy_document" "lambda_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals { type = "Service"; identifiers = ["lambda.amazonaws.com"] }
  }
}

resource "aws_iam_role" "this" {
  name               = "${var.project_name}-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

# VPC-attached Lambda needs AWSLambdaVPCAccessExecutionRole
# (includes ec2:CreateNetworkInterface, ec2:DescribeNetworkInterfaces,
#  ec2:DeleteNetworkInterface, plus basic CloudWatch Logs)
resource "aws_iam_role_policy_attachment" "vpc_access" {
  role       = aws_iam_role.this.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaVPCAccessExecutionRole"
}

data "aws_iam_policy_document" "dynamodb_access" {
  statement {
    actions   = ["dynamodb:PutItem", "dynamodb:GetItem"]
    resources = [aws_dynamodb_table.this.arn]
  }
}

resource "aws_iam_role_policy" "dynamodb" {
  name   = "dynamodb-access"
  role   = aws_iam_role.this.id
  policy = data.aws_iam_policy_document.dynamodb_access.json
}
```

### `lambda.tf`

```hcl
# =======================================================
# TODO 5 -- Lambda Function with VPC Configuration
# =======================================================
# Requirements:
#   - Create the aws_lambda_function with a vpc_config block
#   - Set subnet_ids to both private subnets
#   - Set security_group_ids to the Lambda security group
#   - Set the TABLE_NAME environment variable
#   - Use handler="bootstrap", runtime="provided.al2023"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_function#vpc_config
```

### `outputs.tf`

```hcl
output "function_name" { value = aws_lambda_function.this.function_name }
output "vpc_id"        { value = aws_vpc.this.id }
output "table_name"    { value = aws_dynamodb_table.this.name }
```

## Spot the Bug

A developer places their Lambda function in a **public** subnet thinking it will have internet access through the Internet Gateway:

```hcl
resource "aws_lambda_function" "this" {
  function_name = "my-api-handler"
  # ...

  vpc_config {
    subnet_ids         = [aws_subnet.public.id]       # <-- public subnet
    security_group_ids = [aws_security_group.lambda.id]
  }
}
```

The function fails to reach any external API. The developer verifies the Internet Gateway exists and the public subnet has a route to `0.0.0.0/0` via the IGW. **What is wrong?**

<details>
<summary>Explain the bug</summary>

Lambda functions in a VPC **never receive public IP addresses**. Even though the subnet is public (has a route to an IGW), the Lambda ENI only gets a private IP. The IGW requires a public IP to perform NAT for outbound traffic, so traffic from the Lambda ENI is silently dropped.

This is different from EC2 instances, which can receive auto-assigned public IPs or Elastic IPs in public subnets.

The fix -- place the Lambda function in a **private** subnet with a route to a NAT Gateway in the public subnet:

```hcl
resource "aws_lambda_function" "this" {
  function_name = "my-api-handler"
  # ...

  vpc_config {
    subnet_ids         = [aws_subnet.private.id]      # <-- private subnet
    security_group_ids = [aws_security_group.lambda.id]
  }
}

# NAT Gateway in the public subnet provides outbound internet access
resource "aws_nat_gateway" "this" {
  allocation_id = aws_eip.nat.id
  subnet_id     = aws_subnet.public.id
}

# Private route table routes 0.0.0.0/0 to NAT Gateway
resource "aws_route_table" "private" {
  vpc_id = aws_vpc.this.id
  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.this.id
  }
}
```

This is one of the most frequently tested Lambda networking concepts on the DVA-C02 exam.

</details>

## Verify What You Learned

### Step 1 -- Apply the infrastructure

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init && terraform apply -auto-approve
```

### Step 2 -- Verify Lambda is attached to VPC

```bash
aws lambda get-function-configuration \
  --function-name vpc-lambda-demo \
  --query "VpcConfig.{SubnetIds:SubnetIds,SecurityGroupIds:SecurityGroupIds,VpcId:VpcId}" \
  --output json
```

Expected: JSON showing the VPC ID, two private subnet IDs, and a security group ID.

### Step 3 -- Invoke and verify both internet and DynamoDB access

```bash
aws lambda invoke --function-name vpc-lambda-demo --payload '{}' /dev/stdout 2>/dev/null | jq .
```

Expected: Both `internet_access.success` and `dynamodb_access.success` should be `true`.

### Step 4 -- Verify the VPC endpoint

```bash
aws ec2 describe-vpc-endpoints \
  --filters "Name=vpc-id,Values=$(terraform output -raw vpc_id)" \
  --query "VpcEndpoints[*].{Service:ServiceName,Type:VpcEndpointType,State:State}" \
  --output table
```

Expected: a Gateway endpoint for `com.amazonaws.us-east-1.dynamodb` in state `available`.

## Solutions

<details>
<summary>TODO 1 -- NAT Gateway with Elastic IP (vpc.tf)</summary>

```hcl
resource "aws_eip" "nat" {
  domain = "vpc"
  tags   = { Name = "vpc-lambda-demo-nat-eip" }
}

resource "aws_nat_gateway" "this" {
  allocation_id = aws_eip.nat.id
  subnet_id     = aws_subnet.public.id
  tags          = { Name = "vpc-lambda-demo-nat" }

  depends_on = [aws_internet_gateway.this]
}
```

</details>

<details>
<summary>TODO 2 -- Private Route Table with NAT Gateway Route (vpc.tf)</summary>

```hcl
resource "aws_route_table" "private" {
  vpc_id = aws_vpc.this.id
  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.this.id
  }
  tags = { Name = "vpc-lambda-demo-private-rt" }
}

resource "aws_route_table_association" "private_a" {
  subnet_id      = aws_subnet.private_a.id
  route_table_id = aws_route_table.private.id
}

resource "aws_route_table_association" "private_b" {
  subnet_id      = aws_subnet.private_b.id
  route_table_id = aws_route_table.private.id
}
```

</details>

<details>
<summary>TODO 3 -- Security Group for Lambda (vpc.tf)</summary>

```hcl
resource "aws_security_group" "lambda" {
  name        = "vpc-lambda-demo-sg"
  description = "Security group for VPC Lambda function"
  vpc_id      = aws_vpc.this.id

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Allow all outbound traffic"
  }

  tags = { Name = "vpc-lambda-demo-sg" }
}
```

</details>

<details>
<summary>TODO 4 -- VPC Gateway Endpoint for DynamoDB (vpc.tf)</summary>

```hcl
resource "aws_vpc_endpoint" "dynamodb" {
  vpc_id            = aws_vpc.this.id
  service_name      = "com.amazonaws.us-east-1.dynamodb"
  vpc_endpoint_type = "Gateway"
  route_table_ids   = [aws_route_table.private.id]

  tags = { Name = "vpc-lambda-demo-dynamodb-endpoint" }
}
```

</details>

<details>
<summary>TODO 5 -- Lambda Function with VPC Configuration (lambda.tf)</summary>

```hcl
resource "aws_cloudwatch_log_group" "this" {
  name              = "/aws/lambda/vpc-lambda-demo"
  retention_in_days = 1
}

resource "aws_lambda_function" "this" {
  function_name    = "vpc-lambda-demo"
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.this.arn
  memory_size      = 256
  timeout          = 30

  vpc_config {
    subnet_ids         = [aws_subnet.private_a.id, aws_subnet.private_b.id]
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = {
      TABLE_NAME = aws_dynamodb_table.this.name
    }
  }

  depends_on = [
    aws_iam_role_policy_attachment.vpc_access,
    aws_cloudwatch_log_group.this,
    aws_nat_gateway.this
  ]
}
```

</details>

## Cleanup

Destroy all resources:

```bash
terraform destroy -auto-approve
```

Verify the VPC is deleted:

```bash
aws ec2 describe-vpcs --filters "Name=tag:Name,Values=vpc-lambda-demo" \
  --query "Vpcs[*].VpcId" --output text
```

Expected: empty output.

## What's Next

In **Exercise 22 -- Lambda Extensions and Telemetry API**, you will build a Lambda external extension as a layer that captures telemetry data during the INIT, INVOKE, and SHUTDOWN lifecycle phases.

## Summary

You deployed a Lambda function in a VPC with NAT Gateway for internet access and a VPC endpoint for DynamoDB:

- Lambda in a VPC creates **ENIs in the specified subnets** using AWS Hyperplane (shared across concurrent invocations)
- Lambda **never gets a public IP**, even in a public subnet -- NAT Gateway is required for internet access
- Place Lambda in **private subnets** with a route to NAT Gateway in a public subnet
- **Gateway Endpoints** (S3, DynamoDB) are free, route-table based, and avoid NAT Gateway charges
- **Interface Endpoints** (other services) use ENIs and incur hourly charges
- The IAM policy `AWSLambdaVPCAccessExecutionRole` grants the ENI management permissions Lambda needs
- VPC-attached Lambda cold starts are approximately 1 second (improved from 5-10 seconds pre-2019 via Hyperplane)

Key exam concept: if a Lambda function in a VPC cannot reach the internet, the answer is almost always "add a NAT Gateway in a public subnet and route private subnet traffic through it." If it cannot reach AWS services, add a VPC endpoint.

## Reference

| Resource | Purpose |
|----------|---------|
| `aws_vpc` | Virtual Private Cloud for network isolation |
| `aws_nat_gateway` | NAT for outbound internet from private subnets |
| `aws_vpc_endpoint` | Direct access to AWS services without NAT |
| `vpc_config` on Lambda | Attaches function to VPC subnets and security groups |

## Additional Resources

- [Lambda VPC Networking](https://docs.aws.amazon.com/lambda/latest/dg/configuration-vpc.html)
- [Lambda Hyperplane ENI](https://aws.amazon.com/blogs/compute/announcing-improved-vpc-networking-for-aws-lambda-functions/)
- [VPC Endpoints](https://docs.aws.amazon.com/vpc/latest/privatelink/vpc-endpoints.html)
- [Gateway vs Interface Endpoints](https://docs.aws.amazon.com/vpc/latest/privatelink/concepts.html)
- [NAT Gateway](https://docs.aws.amazon.com/vpc/latest/userguide/vpc-nat-gateway.html)

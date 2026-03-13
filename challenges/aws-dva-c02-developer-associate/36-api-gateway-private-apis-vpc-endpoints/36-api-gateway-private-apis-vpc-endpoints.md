# 36. API Gateway Private APIs with VPC Endpoints

<!--
difficulty: advanced
concepts: [private-api, vpc-endpoint, interface-endpoint, resource-policy, execute-api-endpoint, private-dns, security-group, vpce-policy]
tools: [terraform, aws-cli]
estimated_time: 50m
bloom_level: evaluate, create
prerequisites: [21-lambda-vpc-networking-nat-access, 02-api-gateway-rest-vs-http-validation]
aws_cost: ~$0.03/hr
-->

> **AWS Cost Warning:** This exercise creates a VPC with subnets, a VPC interface endpoint (~$0.01/hr per AZ), and an EC2 instance for testing. Total cost is approximately $0.03/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally
- Basic understanding of VPC networking (subnets, security groups, route tables)

## Learning Objectives

After completing this exercise, you will be able to:

- **Evaluate** when a private API is appropriate versus a public API with IAM authorization or VPN-based access
- **Design** a VPC architecture with interface endpoints that enables private communication with API Gateway without traversing the public internet
- **Implement** a private REST API with a resource policy that restricts access to a specific VPC endpoint
- **Analyze** DNS resolution behavior for private APIs -- how private DNS associates the `execute-api` hostname with the VPC endpoint's private IP addresses
- **Configure** security groups on the VPC endpoint to control which resources within the VPC can reach the API

## Why This Matters

By default, API Gateway REST APIs are publicly accessible from the internet. For internal microservices, compliance-regulated workloads, or backend APIs that should never be exposed publicly, a private API is the correct choice. A private API is only accessible from within a VPC through an interface VPC endpoint for the `execute-api` service. No internet gateway, NAT gateway, or public IP address is involved.

The DVA-C02 exam tests three aspects of private APIs. First, the resource policy: a private API must have a resource policy that explicitly allows access from the VPC endpoint; without it, all requests are denied with 403. Second, DNS resolution: when private DNS is enabled on the VPC endpoint, the standard `execute-api` hostname resolves to the endpoint's private IP addresses, which means you use the same URL format as public APIs. When private DNS is disabled, you must use the VPC endpoint-specific URL format. Third, the relationship between the VPC endpoint security group and the API resource policy -- both must allow the traffic.

## The Challenge

Build a private REST API accessible only from within a VPC. Create an EC2 instance in the VPC to verify that the API is reachable internally and unreachable from the public internet.

### Requirements

| Requirement | Description |
|---|---|
| VPC | VPC with private subnets in 2 AZs |
| VPC Endpoint | Interface endpoint for `execute-api` service with private DNS enabled |
| Security Group | Allows HTTPS (443) inbound from the VPC CIDR |
| Private API | REST API with `endpoint_configuration { types = ["PRIVATE"] }` |
| Resource Policy | Restricts access to the VPC endpoint ID |
| Lambda Backend | Simple Go Lambda behind proxy integration |
| Test Instance | EC2 instance in the VPC to test API access |

### Architecture

```
  +------------------------------------------------------------------+
  |  VPC (10.0.0.0/16)                                               |
  |                                                                    |
  |  +-------------------+    +-------------------+                    |
  |  | Private Subnet A  |    | Private Subnet B  |                    |
  |  |  10.0.1.0/24      |    |  10.0.2.0/24      |                    |
  |  |                   |    |                   |                    |
  |  |  +-------------+  |    |                   |                    |
  |  |  | EC2 (test)  |  |    |                   |                    |
  |  |  +------+------+  |    |                   |                    |
  |  |         |          |    |                   |                    |
  |  +---------+----------+    +--------+----------+                    |
  |            |                        |                               |
  |  +---------+------------------------+----------+                    |
  |  |  VPC Endpoint (execute-api)                 |                    |
  |  |  ENIs in both subnets                       |                    |
  |  |  Private DNS: execute-api.us-east-1 -> ENI  |                    |
  |  |  Security Group: allow 443 from VPC CIDR    |                    |
  |  +---------------------+-----------------------+                    |
  |                        |                                           |
  +------------------------------------------------------------------+
                           |
              +------------+------------+
              |  API Gateway            |
              |  PRIVATE REST API       |
              |  Resource Policy:       |
              |    Allow from vpce-xxx  |
              |                         |
              |  GET /hello -> Lambda   |
              +-------------------------+
```

## Hints

<details>
<summary>Hint 1: VPC endpoint for execute-api</summary>

The interface VPC endpoint creates elastic network interfaces (ENIs) in your subnets. These ENIs have private IP addresses that receive API Gateway traffic. With private DNS enabled, the standard `execute-api` hostname resolves to these private IPs within the VPC.

```hcl
resource "aws_vpc_endpoint" "execute_api" {
  vpc_id              = aws_vpc.this.id
  service_name        = "com.amazonaws.us-east-1.execute-api"
  vpc_endpoint_type   = "Interface"
  private_dns_enabled = true

  subnet_ids = [
    aws_subnet.private_a.id,
    aws_subnet.private_b.id,
  ]

  security_group_ids = [aws_security_group.vpce.id]
}
```

The security group must allow inbound HTTPS (port 443) from the VPC CIDR. Without this, even resources within the VPC cannot reach the endpoint.

</details>

<details>
<summary>Hint 2: Resource policy for private API</summary>

A private API requires a resource policy that explicitly allows access. Without a resource policy, or with a policy that does not match the VPC endpoint, all requests return 403 Forbidden.

```hcl
resource "aws_api_gateway_rest_api" "this" {
  name = "${var.project_name}-private-api"

  endpoint_configuration {
    types            = ["PRIVATE"]
    vpc_endpoint_ids = [aws_vpc_endpoint.execute_api.id]
  }

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect    = "Allow"
        Principal = "*"
        Action    = "execute-api:Invoke"
        Resource  = "execute-api:/*"
        Condition = {
          StringEquals = {
            "aws:sourceVpce" = aws_vpc_endpoint.execute_api.id
          }
        }
      }
    ]
  })
}
```

The `vpc_endpoint_ids` in the endpoint configuration associates the API with specific VPC endpoints. The resource policy's `aws:sourceVpce` condition restricts invocation to that endpoint.

</details>

<details>
<summary>Hint 3: DNS resolution and URL format</summary>

With `private_dns_enabled = true` on the VPC endpoint, you use the standard URL format:

```
https://{rest-api-id}.execute-api.{region}.amazonaws.com/{stage}/hello
```

Without private DNS, you must use the VPC endpoint-specific URL:

```
https://{vpce-id}-{rest-api-id}.execute-api.{region}.vpce.amazonaws.com/{stage}/hello
```

Or use the `Host` header:

```bash
curl https://{vpce-id}.execute-api.{region}.vpce.amazonaws.com/{stage}/hello \
  -H "Host: {rest-api-id}.execute-api.{region}.amazonaws.com"
```

For the exam: private DNS is the simpler approach but requires that no other VPC endpoint in the same VPC uses the same service (only one execute-api endpoint per VPC can have private DNS enabled).

</details>

<details>
<summary>Hint 4: Testing from within the VPC</summary>

You need an EC2 instance (or SSM-connected instance) inside the VPC to test the private API. The instance must be in a subnet that can reach the VPC endpoint ENIs.

```hcl
# SSM endpoint so you can connect without SSH
resource "aws_vpc_endpoint" "ssm" {
  vpc_id              = aws_vpc.this.id
  service_name        = "com.amazonaws.us-east-1.ssm"
  vpc_endpoint_type   = "Interface"
  private_dns_enabled = true
  subnet_ids          = [aws_subnet.private_a.id]
  security_group_ids  = [aws_security_group.vpce.id]
}

resource "aws_vpc_endpoint" "ssm_messages" {
  vpc_id              = aws_vpc.this.id
  service_name        = "com.amazonaws.us-east-1.ssmmessages"
  vpc_endpoint_type   = "Interface"
  private_dns_enabled = true
  subnet_ids          = [aws_subnet.private_a.id]
  security_group_ids  = [aws_security_group.vpce.id]
}

# Test from the instance
# aws ssm start-session --target <instance-id>
# curl https://{api-id}.execute-api.us-east-1.amazonaws.com/dev/hello
```

</details>

<details>
<summary>Hint 5: Common pitfall -- missing DNS support in VPC</summary>

For private DNS to work, the VPC must have both `enable_dns_support` and `enable_dns_hostnames` set to `true`:

```hcl
resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
}
```

Without `enable_dns_hostnames`, the VPC endpoint's private DNS does not activate, and the `execute-api` hostname resolves to the public IP addresses instead of the endpoint's private IPs.

</details>

## Spot the Bug

A developer created a private API and VPC endpoint, but all requests from within the VPC return 403 Forbidden. The VPC endpoint is active, the security group allows HTTPS, and DNS resolves correctly. **What is wrong?**

```hcl
resource "aws_api_gateway_rest_api" "this" {
  name = "private-api"

  endpoint_configuration {
    types = ["PRIVATE"]
    # vpc_endpoint_ids is not set   <-- BUG 1
  }

  # No resource policy defined      <-- BUG 2
}
```

<details>
<summary>Explain the bug</summary>

There are two issues:

**Bug 1: Missing `vpc_endpoint_ids`** in the endpoint configuration. Without this, the private API is not associated with any VPC endpoint, and API Gateway does not know which endpoints should be able to reach it.

**Bug 2: No resource policy.** Private APIs require an explicit resource policy that grants `execute-api:Invoke` permission. Unlike public APIs where requests are allowed by default, private APIs deny all requests unless a resource policy explicitly allows them.

The fix:

```hcl
resource "aws_api_gateway_rest_api" "this" {
  name = "private-api"

  endpoint_configuration {
    types            = ["PRIVATE"]
    vpc_endpoint_ids = [aws_vpc_endpoint.execute_api.id]
  }

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect    = "Allow"
        Principal = "*"
        Action    = "execute-api:Invoke"
        Resource  = "execute-api:/*"
        Condition = {
          StringEquals = {
            "aws:sourceVpce" = aws_vpc_endpoint.execute_api.id
          }
        }
      }
    ]
  })
}
```

On the exam, if a private API returns 403 for all requests, check: (1) resource policy exists and allows the VPC endpoint, (2) `vpc_endpoint_ids` is set, (3) VPC endpoint security group allows inbound 443, and (4) the API has been redeployed after adding the resource policy.

</details>

<details>
<summary>Full Solution</summary>

### `lambda/main.go`

```go
package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

func handler(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"message":   "Hello from private API",
		"source_ip": req.RequestContext.Identity.SourceIP,
		"vpce_id":   req.RequestContext.Identity.VPCEId,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})

	return events.APIGatewayProxyResponse{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(body),
	}, nil
}

func main() {
	lambda.Start(handler)
}
```

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
  description = "Project name for resource naming"
  type        = string
  default     = "private-api-lab"
}
```

### `vpc.tf`

```hcl
resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags                 = { Name = var.project_name }
}

data "aws_availability_zones" "available" { state = "available" }

resource "aws_subnet" "private_a" {
  vpc_id            = aws_vpc.this.id
  cidr_block        = "10.0.1.0/24"
  availability_zone = data.aws_availability_zones.available.names[0]
  tags              = { Name = "${var.project_name}-private-a" }
}

resource "aws_subnet" "private_b" {
  vpc_id            = aws_vpc.this.id
  cidr_block        = "10.0.2.0/24"
  availability_zone = data.aws_availability_zones.available.names[1]
  tags              = { Name = "${var.project_name}-private-b" }
}

resource "aws_vpc_endpoint" "execute_api" {
  vpc_id              = aws_vpc.this.id
  service_name        = "com.amazonaws.${var.region}.execute-api"
  vpc_endpoint_type   = "Interface"
  private_dns_enabled = true

  subnet_ids         = [aws_subnet.private_a.id, aws_subnet.private_b.id]
  security_group_ids = [aws_security_group.vpce.id]
}
```

### `security.tf`

```hcl
resource "aws_security_group" "vpce" {
  name_prefix = "${var.project_name}-vpce-"
  vpc_id      = aws_vpc.this.id

  ingress {
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = [aws_vpc.this.cidr_block]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}
```

### `build.tf`

```hcl
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
data "aws_iam_policy_document" "assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals { type = "Service"; identifiers = ["lambda.amazonaws.com"] }
  }
}

resource "aws_iam_role" "this" {
  name               = "${var.project_name}-role"
  assume_role_policy = data.aws_iam_policy_document.assume.json
}

resource "aws_iam_role_policy_attachment" "basic" {
  role       = aws_iam_role.this.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}
```

### `lambda.tf`

```hcl
resource "aws_lambda_function" "this" {
  function_name    = var.project_name
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.this.arn
  timeout          = 10
  depends_on       = [aws_iam_role_policy_attachment.basic, aws_cloudwatch_log_group.this]
}

resource "aws_lambda_permission" "apigw" {
  statement_id  = "AllowAPIGateway"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.this.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.this.execution_arn}/*/*"
}
```

### `monitoring.tf`

```hcl
resource "aws_cloudwatch_log_group" "this" {
  name              = "/aws/lambda/${var.project_name}"
  retention_in_days = 1
}
```

### `api.tf`

```hcl
resource "aws_api_gateway_rest_api" "this" {
  name = "${var.project_name}-api"

  endpoint_configuration {
    types            = ["PRIVATE"]
    vpc_endpoint_ids = [aws_vpc_endpoint.execute_api.id]
  }

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = "*"
      Action    = "execute-api:Invoke"
      Resource  = "execute-api:/*"
      Condition = {
        StringEquals = { "aws:sourceVpce" = aws_vpc_endpoint.execute_api.id }
      }
    }]
  })
}

resource "aws_api_gateway_resource" "hello" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  parent_id   = aws_api_gateway_rest_api.this.root_resource_id
  path_part   = "hello"
}

resource "aws_api_gateway_method" "get_hello" {
  rest_api_id   = aws_api_gateway_rest_api.this.id
  resource_id   = aws_api_gateway_resource.hello.id
  http_method   = "GET"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "get_hello" {
  rest_api_id             = aws_api_gateway_rest_api.this.id
  resource_id             = aws_api_gateway_resource.hello.id
  http_method             = aws_api_gateway_method.get_hello.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.this.invoke_arn
}

resource "aws_api_gateway_deployment" "this" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  triggers    = { redeployment = sha1(jsonencode([
    aws_api_gateway_resource.hello.id,
    aws_api_gateway_method.get_hello.id,
    aws_api_gateway_integration.get_hello.id,
    aws_api_gateway_rest_api.this.policy,
  ])) }
  lifecycle { create_before_destroy = true }
  depends_on = [aws_api_gateway_integration.get_hello]
}

resource "aws_api_gateway_stage" "dev" {
  deployment_id = aws_api_gateway_deployment.this.id
  rest_api_id   = aws_api_gateway_rest_api.this.id
  stage_name    = "dev"
}
```

### `outputs.tf`

```hcl
output "api_url" { value = "${aws_api_gateway_stage.dev.invoke_url}/hello" }
output "rest_api_id" { value = aws_api_gateway_rest_api.this.id }
output "vpce_id" { value = aws_vpc_endpoint.execute_api.id }
output "vpc_id" { value = aws_vpc.this.id }
```

</details>

## Verify What You Learned

```bash
# Verify the API is PRIVATE
aws apigateway get-rest-api --rest-api-id $(terraform output -raw rest_api_id) \
  --query "endpointConfiguration" --output json

# Verify VPC endpoint is active
aws ec2 describe-vpc-endpoints --vpc-endpoint-ids $(terraform output -raw vpce_id) \
  --query "VpcEndpoints[0].State" --output text

# Verify resource policy
aws apigateway get-rest-api --rest-api-id $(terraform output -raw rest_api_id) \
  --query "policy" --output text

# Attempt to call from outside VPC (should fail)
curl -s "$(terraform output -raw api_url)"

terraform plan
```

Expected: API type is `PRIVATE`, VPC endpoint state is `available`, resource policy includes the VPC endpoint condition, curl from outside returns connection error or 403.

## Cleanup

```bash
terraform destroy -auto-approve
terraform state list
```

Expected: no output (empty state).

## What's Next

You built a private API accessible only through a VPC endpoint. In the next exercise, you will explore **DynamoDB single-table design patterns** -- modeling multiple entity types (Users, Orders, OrderItems) in a single table using partition key and sort key overloading.

## Summary

- **Private APIs** use `endpoint_configuration { types = ["PRIVATE"] }` and are only accessible via VPC interface endpoints for `execute-api`
- A **resource policy** is required on private APIs to allow access from the VPC endpoint; without it, all requests return 403
- **Private DNS** on the VPC endpoint maps the standard `execute-api` hostname to private IPs; requires `enable_dns_support` and `enable_dns_hostnames` on the VPC
- The VPC endpoint **security group** must allow inbound HTTPS (443) from the VPC CIDR
- Private APIs cannot be accessed from the public internet, even with IAM credentials
- `requestContext.identity.vpcId` and `requestContext.identity.vpceId` in the Lambda event identify the source VPC and endpoint

## Reference

- [Private REST APIs](https://docs.aws.amazon.com/apigateway/latest/developerguide/apigateway-private-apis.html)
- [VPC Interface Endpoints](https://docs.aws.amazon.com/vpc/latest/privatelink/vpce-interface.html)
- [API Gateway Resource Policies](https://docs.aws.amazon.com/apigateway/latest/developerguide/apigateway-resource-policies.html)
- [Terraform aws_vpc_endpoint](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_endpoint)

## Additional Resources

- [Private API DNS](https://docs.aws.amazon.com/apigateway/latest/developerguide/apigateway-private-api-test-invoke-url.html) -- URL formats for private APIs with and without private DNS
- [Resource Policy Examples](https://docs.aws.amazon.com/apigateway/latest/developerguide/apigateway-resource-policies-examples.html) -- common resource policy patterns for private APIs
- [VPC Endpoint Policies](https://docs.aws.amazon.com/vpc/latest/privatelink/vpc-endpoints-access.html) -- restricting which APIs can be called through a VPC endpoint
- [Troubleshooting Private APIs](https://docs.aws.amazon.com/apigateway/latest/developerguide/apigateway-private-api-troubleshooting.html) -- diagnosing 403 errors, DNS issues, and connectivity problems

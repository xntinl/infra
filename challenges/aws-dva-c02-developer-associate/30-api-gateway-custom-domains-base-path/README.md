# 30. API Gateway Custom Domains and Base Path Mapping

<!--
difficulty: basic
concepts: [custom-domain-name, acm-certificate, base-path-mapping, route53-alias, edge-optimized, regional, api-versioning]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: understand
prerequisites: [exercise-02]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates API Gateway endpoints, an ACM certificate (free), and optionally a Route 53 hosted zone (if you own a domain). API Gateway pricing is per-request and negligible for testing. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally (for compiling Lambda binaries)
- A registered domain name (optional -- the exercise can be completed with just the API Gateway domain for understanding the concepts)

## Learning Objectives

After completing this exercise, you will be able to:

- **Identify** the two types of custom domain names (edge-optimized and regional) and when to use each
- **Construct** a custom domain name with an ACM certificate and base path mappings that route `/v1` to one API stage and `/v2` to another
- **Explain** how base path mappings enable API versioning under a single domain: `api.example.com/v1` and `api.example.com/v2` can point to different API deployments
- **Verify** the DNS configuration using Route 53 alias records pointing to the API Gateway domain
- **Describe** the certificate requirements: edge-optimized domains require certificates in us-east-1 (CloudFront), regional domains require certificates in the same region as the API

## Why API Gateway Custom Domains and Base Path Mapping

Without custom domains, your API endpoint looks like `https://abc123def.execute-api.us-east-1.amazonaws.com/prod`. This exposes implementation details, is not brandable, and changes if you recreate the API. Custom domain names give you `https://api.example.com`, hiding the infrastructure behind a professional URL.

Base path mappings are the mechanism for API versioning. A single custom domain can route different base paths to different APIs or stages:

- `api.example.com/v1` -> REST API, production stage
- `api.example.com/v2` -> HTTP API, production stage
- `api.example.com/admin` -> Admin API, production stage

The DVA-C02 exam tests the distinction between edge-optimized and regional custom domains:

**Edge-optimized**: Routes through CloudFront. The ACM certificate MUST be in `us-east-1` regardless of the API region. Best for geographically distributed clients. This is the default type.

**Regional**: No CloudFront. The ACM certificate must be in the SAME region as the API. Best for clients in the same region, or when you want to manage your own CloudFront distribution in front of the API.

The exam frequently asks: "A developer created a custom domain name for their API in us-west-2 but gets a certificate error. What is wrong?" Answer: if the custom domain is edge-optimized, the certificate must be in us-east-1, not us-west-2.

## Step 1 -- Create Two Lambda Functions (v1 and v2)

### `lambda/main.go`

```go
package main

import (
	"context"
	"encoding/json"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

func handler(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	version := os.Getenv("API_VERSION")

	body, _ := json.MarshalIndent(map[string]string{
		"version": version,
		"message": "Hello from API " + version,
		"path":    request.Path,
		"method":  request.HTTPMethod,
	}, "", "  ")

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

## Step 2 -- Create the Terraform Project Files

Create the following files in your exercise directory:

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
  default     = "custom-domain-demo"
}

variable "domain_name" {
  description = "Custom domain name (e.g., api.example.com). Leave empty if no domain available."
  type        = string
  default     = ""
}

variable "hosted_zone_id" {
  description = "Route 53 Hosted Zone ID for the domain. Leave empty if no domain available."
  type        = string
  default     = ""
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

resource "aws_iam_role_policy_attachment" "basic" {
  role       = aws_iam_role.this.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}
```

### `lambda.tf`

```hcl
# -- Lambda Function v1 --
resource "aws_lambda_function" "v1" {
  function_name    = "${var.project_name}-v1"
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.this.arn
  memory_size      = 256
  timeout          = 10

  environment {
    variables = { API_VERSION = "v1" }
  }

  depends_on = [aws_iam_role_policy_attachment.basic, aws_cloudwatch_log_group.v1]
}

# -- Lambda Function v2 --
resource "aws_lambda_function" "v2" {
  function_name    = "${var.project_name}-v2"
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.this.arn
  memory_size      = 256
  timeout          = 10

  environment {
    variables = { API_VERSION = "v2" }
  }

  depends_on = [aws_iam_role_policy_attachment.basic, aws_cloudwatch_log_group.v2]
}

resource "aws_lambda_permission" "v1" {
  statement_id  = "AllowAPIGatewayInvokeV1"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.v1.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.v1.execution_arn}/*/*"
}

resource "aws_lambda_permission" "v2" {
  statement_id  = "AllowAPIGatewayInvokeV2"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.v2.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.v2.execution_arn}/*/*"
}
```

### `monitoring.tf`

```hcl
resource "aws_cloudwatch_log_group" "v1" {
  name              = "/aws/lambda/${var.project_name}-v1"
  retention_in_days = 1
}

resource "aws_cloudwatch_log_group" "v2" {
  name              = "/aws/lambda/${var.project_name}-v2"
  retention_in_days = 1
}
```

### `api.tf`

```hcl
# -- REST API for v1 --
resource "aws_api_gateway_rest_api" "v1" {
  name        = "${var.project_name}-v1"
  description = "API v1 for custom domain demo"
}

resource "aws_api_gateway_resource" "v1" {
  rest_api_id = aws_api_gateway_rest_api.v1.id
  parent_id   = aws_api_gateway_rest_api.v1.root_resource_id
  path_part   = "{proxy+}"
}

resource "aws_api_gateway_method" "v1" {
  rest_api_id   = aws_api_gateway_rest_api.v1.id
  resource_id   = aws_api_gateway_resource.v1.id
  http_method   = "ANY"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "v1" {
  rest_api_id             = aws_api_gateway_rest_api.v1.id
  resource_id             = aws_api_gateway_resource.v1.id
  http_method             = aws_api_gateway_method.v1.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.v1.invoke_arn
}

resource "aws_api_gateway_deployment" "v1" {
  rest_api_id = aws_api_gateway_rest_api.v1.id
  depends_on  = [aws_api_gateway_integration.v1]

  triggers = {
    redeployment = sha1(jsonencode([
      aws_api_gateway_resource.v1.id,
      aws_api_gateway_method.v1.id,
      aws_api_gateway_integration.v1.id,
    ]))
  }

  lifecycle { create_before_destroy = true }
}

resource "aws_api_gateway_stage" "v1" {
  rest_api_id   = aws_api_gateway_rest_api.v1.id
  deployment_id = aws_api_gateway_deployment.v1.id
  stage_name    = "live"
}

# -- REST API for v2 --
resource "aws_api_gateway_rest_api" "v2" {
  name        = "${var.project_name}-v2"
  description = "API v2 for custom domain demo"
}

resource "aws_api_gateway_resource" "v2" {
  rest_api_id = aws_api_gateway_rest_api.v2.id
  parent_id   = aws_api_gateway_rest_api.v2.root_resource_id
  path_part   = "{proxy+}"
}

resource "aws_api_gateway_method" "v2" {
  rest_api_id   = aws_api_gateway_rest_api.v2.id
  resource_id   = aws_api_gateway_resource.v2.id
  http_method   = "ANY"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "v2" {
  rest_api_id             = aws_api_gateway_rest_api.v2.id
  resource_id             = aws_api_gateway_resource.v2.id
  http_method             = aws_api_gateway_method.v2.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.v2.invoke_arn
}

resource "aws_api_gateway_deployment" "v2" {
  rest_api_id = aws_api_gateway_rest_api.v2.id
  depends_on  = [aws_api_gateway_integration.v2]

  triggers = {
    redeployment = sha1(jsonencode([
      aws_api_gateway_resource.v2.id,
      aws_api_gateway_method.v2.id,
      aws_api_gateway_integration.v2.id,
    ]))
  }

  lifecycle { create_before_destroy = true }
}

resource "aws_api_gateway_stage" "v2" {
  rest_api_id   = aws_api_gateway_rest_api.v2.id
  deployment_id = aws_api_gateway_deployment.v2.id
  stage_name    = "live"
}
```

### `dns.tf`

```hcl
# -- Custom Domain Name (only created if domain_name is provided) --
# ACM Certificate -- for edge-optimized, must be in us-east-1
resource "aws_acm_certificate" "this" {
  count             = var.domain_name != "" ? 1 : 0
  domain_name       = var.domain_name
  validation_method = "DNS"

  lifecycle { create_before_destroy = true }
}

# DNS validation record
resource "aws_route53_record" "cert_validation" {
  count   = var.domain_name != "" ? 1 : 0
  zone_id = var.hosted_zone_id
  name    = tolist(aws_acm_certificate.this[0].domain_validation_options)[0].resource_record_name
  type    = tolist(aws_acm_certificate.this[0].domain_validation_options)[0].resource_record_type
  records = [tolist(aws_acm_certificate.this[0].domain_validation_options)[0].resource_record_value]
  ttl     = 60
}

resource "aws_acm_certificate_validation" "this" {
  count                   = var.domain_name != "" ? 1 : 0
  certificate_arn         = aws_acm_certificate.this[0].arn
  validation_record_fqdns = [aws_route53_record.cert_validation[0].fqdn]
}

# Custom domain name (REGIONAL type)
resource "aws_api_gateway_domain_name" "this" {
  count                    = var.domain_name != "" ? 1 : 0
  domain_name              = var.domain_name
  regional_certificate_arn = aws_acm_certificate_validation.this[0].certificate_arn

  endpoint_configuration {
    types = ["REGIONAL"]
  }
}

# Base path mapping: /v1 -> REST API v1, live stage
resource "aws_api_gateway_base_path_mapping" "v1" {
  count       = var.domain_name != "" ? 1 : 0
  api_id      = aws_api_gateway_rest_api.v1.id
  stage_name  = aws_api_gateway_stage.v1.stage_name
  domain_name = aws_api_gateway_domain_name.this[0].domain_name
  base_path   = "v1"
}

# Base path mapping: /v2 -> REST API v2, live stage
resource "aws_api_gateway_base_path_mapping" "v2" {
  count       = var.domain_name != "" ? 1 : 0
  api_id      = aws_api_gateway_rest_api.v2.id
  stage_name  = aws_api_gateway_stage.v2.stage_name
  domain_name = aws_api_gateway_domain_name.this[0].domain_name
  base_path   = "v2"
}

# Route 53 alias record
resource "aws_route53_record" "api" {
  count   = var.domain_name != "" ? 1 : 0
  zone_id = var.hosted_zone_id
  name    = var.domain_name
  type    = "A"

  alias {
    name                   = aws_api_gateway_domain_name.this[0].regional_domain_name
    zone_id                = aws_api_gateway_domain_name.this[0].regional_zone_id
    evaluate_target_health = true
  }
}
```

### `outputs.tf`

```hcl
output "v1_url"        { value = aws_api_gateway_stage.v1.invoke_url }
output "v2_url"        { value = aws_api_gateway_stage.v2.invoke_url }
output "custom_domain" { value = var.domain_name != "" ? var.domain_name : "No custom domain configured" }
output "v1_function"   { value = aws_lambda_function.v1.function_name }
output "v2_function"   { value = aws_lambda_function.v2.function_name }
```

## Step 3 -- Build and Apply

Build the Go binary and deploy. If you do not have a custom domain, the exercise works with just the default API Gateway endpoints:

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init

# Without custom domain (concepts only):
terraform apply -auto-approve

# With custom domain:
# terraform apply -var="domain_name=api.example.com" -var="hosted_zone_id=Z0123456789" -auto-approve
```

### Intermediate Verification

```bash
terraform state list | grep api_gateway
```

You should see both REST APIs, their stages, and the Lambda functions.

## Step 4 -- Test the APIs via Default Endpoints

```bash
V1_URL=$(terraform output -raw v1_url)
V2_URL=$(terraform output -raw v2_url)

echo "--- v1 API ---"
curl -s "${V1_URL}/hello" | jq .

echo "--- v2 API ---"
curl -s "${V2_URL}/hello" | jq .
```

Expected: v1 returns `"version": "v1"`, v2 returns `"version": "v2"`.

## Step 5 -- Test via Custom Domain (if configured)

```bash
DOMAIN=$(terraform output -raw custom_domain)

# v1 base path
curl -s "https://${DOMAIN}/v1/hello" | jq .

# v2 base path
curl -s "https://${DOMAIN}/v2/hello" | jq .
```

Expected: both responses include their version, served from the same domain with different base paths.

## Common Mistakes

### 1. Using an edge-optimized domain with a certificate outside us-east-1

Edge-optimized custom domains use CloudFront under the hood. CloudFront requires the ACM certificate to be in us-east-1, regardless of the API's region.

**Wrong -- certificate in us-west-2 for edge-optimized domain:**

```hcl
provider "aws" { region = "us-west-2" }

resource "aws_acm_certificate" "this" {
  domain_name = "api.example.com"  # Certificate created in us-west-2
}

resource "aws_api_gateway_domain_name" "this" {
  domain_name     = "api.example.com"
  certificate_arn = aws_acm_certificate.this.arn  # Wrong region!

  endpoint_configuration {
    types = ["EDGE"]  # Edge-optimized requires us-east-1 cert
  }
}
```

**What happens:** `terraform apply` fails with `BadRequestException: The certificate must be in us-east-1`.

**Fix -- use a regional domain OR create the cert in us-east-1:**

```hcl
# Option 1: Use REGIONAL endpoint (cert in same region as API)
resource "aws_api_gateway_domain_name" "this" {
  domain_name              = "api.example.com"
  regional_certificate_arn = aws_acm_certificate.this.arn

  endpoint_configuration {
    types = ["REGIONAL"]
  }
}

# Option 2: Use EDGE with cert in us-east-1
provider "aws" {
  alias  = "us_east_1"
  region = "us-east-1"
}

resource "aws_acm_certificate" "this" {
  provider    = aws.us_east_1
  domain_name = "api.example.com"
}
```

### 2. Forgetting to create the base path mapping

Creating a custom domain name without base path mappings means the domain resolves but returns 403 Forbidden for all paths.

**Wrong -- domain without mappings:**

```hcl
resource "aws_api_gateway_domain_name" "this" {
  domain_name = "api.example.com"
  # ...
}
# No aws_api_gateway_base_path_mapping resources!
```

**What happens:** `https://api.example.com/anything` returns `{"message":"Forbidden"}`.

**Fix -- add base path mappings:**

```hcl
resource "aws_api_gateway_base_path_mapping" "v1" {
  api_id      = aws_api_gateway_rest_api.v1.id
  stage_name  = "live"
  domain_name = aws_api_gateway_domain_name.this.domain_name
  base_path   = "v1"
}
```

### 3. Using an empty base path when you want versioned paths

An empty `base_path` maps the domain root to the API. If you later try to add a `/v2` mapping, it conflicts with the empty mapping.

**Wrong -- empty base path blocks versioned paths:**

```hcl
resource "aws_api_gateway_base_path_mapping" "root" {
  base_path = ""  # Maps entire domain to one API
}

resource "aws_api_gateway_base_path_mapping" "v2" {
  base_path = "v2"  # Conflicts with empty base path
}
```

**Fix -- always use versioned base paths:**

```hcl
resource "aws_api_gateway_base_path_mapping" "v1" {
  base_path = "v1"
}

resource "aws_api_gateway_base_path_mapping" "v2" {
  base_path = "v2"
}
```

## Verify What You Learned

```bash
# Verify both APIs exist
aws apigateway get-rest-apis --query "items[?contains(name,'custom-domain')].{Name:name,Id:id}" --output table
```

Expected: two APIs (custom-domain-demo-v1, custom-domain-demo-v2).

```bash
# Test v1 returns correct version
curl -s "$(terraform output -raw v1_url)/test" | jq -r '.version'
```

Expected: `v1`

```bash
# Test v2 returns correct version
curl -s "$(terraform output -raw v2_url)/test" | jq -r '.version'
```

Expected: `v2`

```bash
# If custom domain configured, verify the domain name
aws apigateway get-domain-names --query "items[*].{Domain:domainName,Endpoint:endpointConfiguration.types[0]}" --output table
```

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

Destroy all resources:

```bash
terraform destroy -auto-approve
```

Verify:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

You configured API versioning using custom domains and base path mappings. This concludes the API Gateway section of the DVA-C02 exercises. Future exercises will cover additional AWS developer services including DynamoDB advanced patterns, Step Functions, and AppSync.

## Summary

- **Custom domain names** replace default API Gateway URLs (`abc123.execute-api...`) with branded URLs (`api.example.com`)
- **Edge-optimized** domains use CloudFront and require ACM certificates in **us-east-1**
- **Regional** domains do not use CloudFront and require ACM certificates in the **same region** as the API
- **Base path mappings** route different URL prefixes to different APIs/stages: `/v1` -> API v1, `/v2` -> API v2
- **Route 53 alias records** point the custom domain to the API Gateway domain name (no TTL, no extra hop)
- An empty base path maps the domain root to a single API; use versioned paths for multi-API setups
- ACM certificates for custom domains must use **DNS validation** (recommended) or email validation
- Without base path mappings, a custom domain returns **403 Forbidden** for all requests

## Reference

- [API Gateway Custom Domain Names](https://docs.aws.amazon.com/apigateway/latest/developerguide/how-to-custom-domains.html)
- [Base Path Mappings](https://docs.aws.amazon.com/apigateway/latest/developerguide/how-to-custom-domains-prerequisites.html)
- [ACM Certificate Manager](https://docs.aws.amazon.com/acm/latest/userguide/gs.html)
- [Terraform aws_api_gateway_domain_name](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_domain_name)

## Additional Resources

- [Edge-Optimized vs Regional API Endpoints](https://docs.aws.amazon.com/apigateway/latest/developerguide/api-gateway-api-endpoint-types.html) -- comparison of endpoint types and when to use each
- [ACM DNS Validation](https://docs.aws.amazon.com/acm/latest/userguide/dns-validation.html) -- how DNS validation works and how to automate it with Route 53
- [Custom Domain for HTTP APIs](https://docs.aws.amazon.com/apigateway/latest/developerguide/http-api-custom-domain-names.html) -- custom domains work differently for HTTP APIs (apigatewayv2)
- [API Gateway Mutual TLS](https://docs.aws.amazon.com/apigateway/latest/developerguide/rest-api-mutual-tls.html) -- configuring mTLS with custom domains for additional security

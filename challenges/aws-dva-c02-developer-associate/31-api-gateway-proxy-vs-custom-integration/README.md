# 31. API Gateway Proxy vs Custom Integration

<!--
difficulty: intermediate
concepts: [api-gateway-proxy-integration, custom-integration, mapping-templates, integration-request, integration-response, vtl, passthrough-behavior]
tools: [terraform, aws-cli, curl]
estimated_time: 45m
bloom_level: analyze, implement
prerequisites: [02-api-gateway-rest-vs-http-validation]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a REST API with two Lambda integrations. API Gateway and Lambda costs are negligible during testing (~$0.01/hr or less). Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| curl installed | `curl --version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Analyze** the structural differences between Lambda proxy integration (AWS_PROXY) and custom integration (AWS) request/response flows
2. **Implement** a custom integration with VTL mapping templates that transform the request before it reaches Lambda and reshape the response before it returns to the client
3. **Differentiate** between passthrough behaviors (WHEN_NO_MATCH, WHEN_NO_TEMPLATES, NEVER) and their effect on unmapped content types
4. **Configure** Integration Response status code mappings to translate Lambda error patterns into appropriate HTTP status codes
5. **Evaluate** when to choose proxy integration (rapid development, full control in code) versus custom integration (API Gateway handles transformation, decoupled contract)

## Why This Matters

API Gateway offers two fundamentally different ways to connect to Lambda. **Proxy integration** (`AWS_PROXY`) passes the entire HTTP request -- headers, query strings, path parameters, body -- as a single JSON event to your Lambda function. The function must return a response object with `statusCode`, `headers`, and `body`. This is the default for most tutorials and works well when your Lambda owns the entire request/response contract.

**Custom integration** (`AWS`) gives API Gateway control over the transformation layer. You write VTL (Velocity Template Language) mapping templates that reshape the incoming request before Lambda sees it, and reshape the Lambda response before the client receives it. This decouples the API contract from the Lambda implementation: you can change query parameter names, flatten nested objects, inject default values, and map Lambda errors to specific HTTP status codes -- all without touching Lambda code.

The DVA-C02 exam tests this distinction heavily. Questions describe a scenario where the Lambda function returns raw data but the API must return a different JSON structure, or where errors from Lambda should map to 400 vs 500 status codes. The answer almost always involves custom integration with mapping templates. Understanding the `Integration Request` and `Integration Response` pipeline is essential.

## Building Blocks

### Lambda Function Code

Create a file called `main.go`. This single Lambda handles both integration types -- the difference is in how API Gateway wraps and unwraps the request:

### `main.go`

```go
// main.go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

// CustomRequest is the shape that the custom integration mapping template sends.
type CustomRequest struct {
	ProductID string `json:"product_id"`
	Category  string `json:"category"`
	Action    string `json:"action"`
}

// handleProxy is invoked via AWS_PROXY integration.
// API Gateway sends the full HTTP request as APIGatewayProxyRequest.
func handleProxy(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	category := req.QueryStringParameters["category"]
	if category == "" {
		category = "all"
	}

	body, _ := json.Marshal(map[string]interface{}{
		"integration_type": "PROXY",
		"method":           req.HTTPMethod,
		"path":             req.Path,
		"category":         category,
		"timestamp":        time.Now().UTC().Format(time.RFC3339),
		"note":             "Lambda received the full HTTP request and must format the full HTTP response",
	})

	return events.APIGatewayProxyResponse{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(body),
	}, nil
}

// handleCustom is invoked via AWS (custom) integration.
// API Gateway sends only the fields defined in the mapping template.
func handleCustom(ctx context.Context, req CustomRequest) (map[string]interface{}, error) {
	if req.ProductID == "" {
		return nil, errors.New("ValidationError: product_id is required")
	}

	return map[string]interface{}{
		"product_id":  req.ProductID,
		"category":    req.Category,
		"action":      req.Action,
		"processed":   true,
		"processed_at": time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func router(ctx context.Context, raw json.RawMessage) (interface{}, error) {
	// If the payload has httpMethod, it is a proxy integration event
	var probe map[string]interface{}
	json.Unmarshal(raw, &probe)

	if _, ok := probe["httpMethod"]; ok {
		var req events.APIGatewayProxyRequest
		json.Unmarshal(raw, &req)
		return handleProxy(ctx, req)
	}

	var req CustomRequest
	json.Unmarshal(raw, &req)
	resp, err := handleCustom(ctx, req)
	if err != nil {
		// Return the error string so API Gateway can regex-match it
		return nil, fmt.Errorf("%s", err.Error())
	}
	return resp, nil
}

func main() {
	lambda.Start(router)
}
```

Ignore the `strings` import if your editor flags it -- the routing logic uses `json.Unmarshal` probing instead. The key insight is that proxy integration sends the full HTTP envelope while custom integration sends only what the mapping template defines.

### Terraform Skeleton

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
  default     = "proxy-vs-custom"
}
```

### `build.tf`

```hcl
# -------------------------------------------------------
# Build the Go binary
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
# -------------------------------------------------------
# Lambda function
# -------------------------------------------------------
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
  role             = aws_iam_role.this.arn
  timeout          = 10
  depends_on       = [aws_iam_role_policy_attachment.basic, aws_cloudwatch_log_group.this]
}
```

### `api.tf`

```hcl
# -------------------------------------------------------
# REST API
# -------------------------------------------------------
resource "aws_api_gateway_rest_api" "this" {
  name = "${var.project_name}-api"
}

resource "aws_lambda_permission" "apigw" {
  statement_id  = "AllowAPIGateway"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.this.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.this.execution_arn}/*/*"
}

# ============================================================
# PATH 1: /proxy -- Lambda Proxy Integration (AWS_PROXY)
# ============================================================
resource "aws_api_gateway_resource" "proxy" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  parent_id   = aws_api_gateway_rest_api.this.root_resource_id
  path_part   = "proxy"
}

resource "aws_api_gateway_method" "proxy_get" {
  rest_api_id   = aws_api_gateway_rest_api.this.id
  resource_id   = aws_api_gateway_resource.proxy.id
  http_method   = "GET"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "proxy_get" {
  rest_api_id             = aws_api_gateway_rest_api.this.id
  resource_id             = aws_api_gateway_resource.proxy.id
  http_method             = aws_api_gateway_method.proxy_get.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.this.invoke_arn
}

# ============================================================
# PATH 2: /custom -- Custom Integration (AWS) with mapping
# ============================================================
resource "aws_api_gateway_resource" "custom" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  parent_id   = aws_api_gateway_rest_api.this.root_resource_id
  path_part   = "custom"
}

resource "aws_api_gateway_method" "custom_get" {
  rest_api_id   = aws_api_gateway_rest_api.this.id
  resource_id   = aws_api_gateway_resource.custom.id
  http_method   = "GET"
  authorization = "NONE"

  request_parameters = {
    "method.request.querystring.product_id" = true
    "method.request.querystring.category"   = false
  }
}

# =======================================================
# TODO 1 -- Custom Integration Request with Mapping Template
# =======================================================
# Requirements:
#   - Create an aws_api_gateway_integration for GET /custom
#   - Set type = "AWS" (NOT "AWS_PROXY")
#   - Set integration_http_method = "POST"
#   - Set uri to the Lambda invoke ARN
#   - Set passthrough_behavior = "WHEN_NO_TEMPLATES"
#   - Add a request_templates block for "application/json" that
#     maps query string parameters to a JSON body:
#
#     {
#       "product_id": "$input.params('product_id')",
#       "category": "$input.params('category')",
#       "action": "lookup"
#     }
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_integration


# =======================================================
# TODO 2 -- Integration Response (200 OK)
# =======================================================
# Requirements:
#   - Create an aws_api_gateway_method_response for status 200
#     with response_models = { "application/json" = "Empty" }
#   - Create an aws_api_gateway_integration_response for status 200
#   - Set response_templates for "application/json" that wraps the
#     Lambda output in an envelope:
#
#     #set($body = $input.path('$'))
#     {
#       "status": "success",
#       "data": {
#         "product_id": "$body.product_id",
#         "category": "$body.category",
#         "processed": $body.processed,
#         "processed_at": "$body.processed_at"
#       }
#     }
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_integration_response


# =======================================================
# TODO 3 -- Integration Response (400 Error)
# =======================================================
# Requirements:
#   - Create an aws_api_gateway_method_response for status 400
#   - Create an aws_api_gateway_integration_response for status 400
#   - Set selection_pattern = ".*ValidationError.*"
#     (this regex matches the Lambda error message)
#   - Set response_templates for "application/json":
#
#     #set($errorMsg = $input.path('$.errorMessage'))
#     {
#       "status": "error",
#       "message": "$errorMsg"
#     }
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_integration_response
# Hint: selection_pattern matches against the errorMessage field
#       in the Lambda error response JSON


# =======================================================
# TODO 4 -- Deployment and Stage
# =======================================================
# Requirements:
#   - Create aws_api_gateway_deployment with triggers for redeployment
#   - Create aws_api_gateway_stage named "dev"
#   - depends_on must include all integrations and methods
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_deployment
```

### `outputs.tf`

```hcl
output "api_url"         { value = aws_api_gateway_stage.dev.invoke_url }
output "proxy_endpoint"  { value = "${aws_api_gateway_stage.dev.invoke_url}/proxy" }
output "custom_endpoint" { value = "${aws_api_gateway_stage.dev.invoke_url}/custom" }
```

## Spot the Bug

A developer configured a custom integration with a mapping template, but the API always returns a 500 Internal Server Error with `{"message": "Internal server error"}`. The Lambda function works correctly when invoked directly. **What is wrong?**

```hcl
resource "aws_api_gateway_integration" "custom_get" {
  rest_api_id             = aws_api_gateway_rest_api.this.id
  resource_id             = aws_api_gateway_resource.custom.id
  http_method             = aws_api_gateway_method.custom_get.http_method
  integration_http_method = "POST"
  type                    = "AWS"
  uri                     = aws_lambda_function.this.invoke_arn

  request_templates = {
    "application/json" = <<EOF
{
  "product_id": "$input.params('product_id')"
}
EOF
  }
}

resource "aws_api_gateway_method_response" "ok" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  resource_id = aws_api_gateway_resource.custom.id
  http_method = aws_api_gateway_method.custom_get.http_method
  status_code = "200"
}

# NOTE: No aws_api_gateway_integration_response resource exists
```

<details>
<summary>Explain the bug</summary>

The custom integration is missing an `aws_api_gateway_integration_response` resource. With `AWS_PROXY` integration, API Gateway automatically maps the Lambda response to an HTTP response. With `AWS` (custom) integration, you **must** explicitly define Integration Responses that tell API Gateway how to map the Lambda output to an HTTP response.

Without an Integration Response, API Gateway does not know how to translate the Lambda response into an HTTP response, so it returns a 500 Internal Server Error.

The fix -- add the Integration Response:

```hcl
resource "aws_api_gateway_integration_response" "ok" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  resource_id = aws_api_gateway_resource.custom.id
  http_method = aws_api_gateway_method.custom_get.http_method
  status_code = aws_api_gateway_method_response.ok.status_code

  response_templates = {
    "application/json" = "$input.body"
  }

  depends_on = [aws_api_gateway_integration.custom_get]
}
```

This is a critical DVA-C02 concept: proxy integration handles everything automatically, but custom integration requires explicit Method Response + Integration Response configuration for every status code your API can return.

</details>

## Solutions

<details>
<summary>TODO 1 -- Custom Integration Request with Mapping Template (api.tf)</summary>

```hcl
resource "aws_api_gateway_integration" "custom_get" {
  rest_api_id             = aws_api_gateway_rest_api.this.id
  resource_id             = aws_api_gateway_resource.custom.id
  http_method             = aws_api_gateway_method.custom_get.http_method
  integration_http_method = "POST"
  type                    = "AWS"
  uri                     = aws_lambda_function.this.invoke_arn
  passthrough_behavior    = "WHEN_NO_TEMPLATES"

  request_templates = {
    "application/json" = <<EOF
{
  "product_id": "$input.params('product_id')",
  "category": "$input.params('category')",
  "action": "lookup"
}
EOF
  }
}
```

</details>

<details>
<summary>TODO 2 -- Integration Response (200 OK) (api.tf)</summary>

```hcl
resource "aws_api_gateway_method_response" "ok" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  resource_id = aws_api_gateway_resource.custom.id
  http_method = aws_api_gateway_method.custom_get.http_method
  status_code = "200"

  response_models = {
    "application/json" = "Empty"
  }
}

resource "aws_api_gateway_integration_response" "ok" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  resource_id = aws_api_gateway_resource.custom.id
  http_method = aws_api_gateway_method.custom_get.http_method
  status_code = aws_api_gateway_method_response.ok.status_code

  response_templates = {
    "application/json" = <<EOF
#set($body = $input.path('$'))
{
  "status": "success",
  "data": {
    "product_id": "$body.product_id",
    "category": "$body.category",
    "processed": $body.processed,
    "processed_at": "$body.processed_at"
  }
}
EOF
  }

  depends_on = [aws_api_gateway_integration.custom_get]
}
```

</details>

<details>
<summary>TODO 3 -- Integration Response (400 Error) (api.tf)</summary>

```hcl
resource "aws_api_gateway_method_response" "bad_request" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  resource_id = aws_api_gateway_resource.custom.id
  http_method = aws_api_gateway_method.custom_get.http_method
  status_code = "400"

  response_models = {
    "application/json" = "Error"
  }
}

resource "aws_api_gateway_integration_response" "bad_request" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  resource_id = aws_api_gateway_resource.custom.id
  http_method = aws_api_gateway_method.custom_get.http_method
  status_code = aws_api_gateway_method_response.bad_request.status_code

  selection_pattern = ".*ValidationError.*"

  response_templates = {
    "application/json" = <<EOF
#set($errorMsg = $input.path('$.errorMessage'))
{
  "status": "error",
  "message": "$errorMsg"
}
EOF
  }

  depends_on = [aws_api_gateway_integration.custom_get]
}
```

</details>

<details>
<summary>TODO 4 -- Deployment and Stage (api.tf)</summary>

```hcl
resource "aws_api_gateway_deployment" "this" {
  rest_api_id = aws_api_gateway_rest_api.this.id

  triggers = {
    redeployment = sha1(jsonencode([
      aws_api_gateway_resource.proxy.id,
      aws_api_gateway_resource.custom.id,
      aws_api_gateway_method.proxy_get.id,
      aws_api_gateway_method.custom_get.id,
      aws_api_gateway_integration.proxy_get.id,
      aws_api_gateway_integration.custom_get.id,
      aws_api_gateway_integration_response.ok.id,
      aws_api_gateway_integration_response.bad_request.id,
    ]))
  }

  lifecycle { create_before_destroy = true }

  depends_on = [
    aws_api_gateway_integration.proxy_get,
    aws_api_gateway_integration.custom_get,
    aws_api_gateway_integration_response.ok,
    aws_api_gateway_integration_response.bad_request,
  ]
}

resource "aws_api_gateway_stage" "dev" {
  deployment_id = aws_api_gateway_deployment.this.id
  rest_api_id   = aws_api_gateway_rest_api.this.id
  stage_name    = "dev"
}
```

</details>

## Verify What You Learned

### Step 1 -- Apply the infrastructure

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init && terraform apply -auto-approve
```

### Step 2 -- Test proxy integration

```bash
API_URL=$(terraform output -raw api_url)

curl -s "${API_URL}/proxy?category=electronics" | jq .
```

Expected: Lambda receives the full HTTP request and returns `integration_type: "PROXY"` with the category parameter.

### Step 3 -- Test custom integration (success)

```bash
curl -s "${API_URL}/custom?product_id=PROD-001&category=electronics" | jq .
```

Expected: Response wrapped in the `{"status": "success", "data": {...}}` envelope from the response mapping template.

### Step 4 -- Test custom integration (validation error)

```bash
curl -s "${API_URL}/custom?category=electronics" | jq .
```

Expected: 400 response with `{"status": "error", "message": "ValidationError: product_id is required"}`.

### Step 5 -- Verify the mapping template transformed the request

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

You compared proxy and custom integrations and built VTL mapping templates for request/response transformation. In the next exercise, you will dive deeper into **VTL mapping templates** -- extracting query parameters, transforming JSON bodies, and using `#set`, `#if`, and `#foreach` directives to build complex request transformations.

## Summary

- **Proxy integration** (`AWS_PROXY`) passes the full HTTP request to Lambda and requires Lambda to return `statusCode`, `headers`, and `body` -- simple but couples the API contract to the Lambda code
- **Custom integration** (`AWS`) lets API Gateway transform requests and responses via VTL mapping templates -- decouples the API contract from the Lambda implementation
- Custom integration **requires** both `aws_api_gateway_method_response` and `aws_api_gateway_integration_response` for every status code; without them, API Gateway returns 500
- `selection_pattern` on Integration Response uses regex to match Lambda error messages and route them to specific HTTP status codes
- `passthrough_behavior` controls what happens when no mapping template matches the request Content-Type: `WHEN_NO_TEMPLATES` (pass through if no templates defined), `WHEN_NO_MATCH` (pass through if no template matches), `NEVER` (reject with 415)
- VTL variables: `$input.params('name')` reads query/path/header parameters, `$input.body` reads the raw body, `$input.path('$.field')` extracts JSON fields

## Reference

- [API Gateway Integration Types](https://docs.aws.amazon.com/apigateway/latest/developerguide/api-gateway-api-integration-types.html)
- [API Gateway Mapping Templates](https://docs.aws.amazon.com/apigateway/latest/developerguide/api-gateway-mapping-template-reference.html)
- [Terraform aws_api_gateway_integration](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_integration)
- [Terraform aws_api_gateway_integration_response](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_integration_response)

## Additional Resources

- [Lambda Proxy Integration](https://docs.aws.amazon.com/apigateway/latest/developerguide/set-up-lambda-proxy-integrations.html) -- detailed guide on proxy integration event format and response requirements
- [Lambda Custom Integration](https://docs.aws.amazon.com/apigateway/latest/developerguide/set-up-lambda-custom-integrations.html) -- step-by-step guide for custom integration with mapping templates
- [API Gateway Passthrough Behavior](https://docs.aws.amazon.com/apigateway/latest/developerguide/integration-passthrough-behaviors.html) -- how WHEN_NO_MATCH, WHEN_NO_TEMPLATES, and NEVER differ
- [VTL Reference for API Gateway](https://docs.aws.amazon.com/apigateway/latest/developerguide/api-gateway-mapping-template-reference.html) -- complete reference for $input, $context, $stageVariables, and $util

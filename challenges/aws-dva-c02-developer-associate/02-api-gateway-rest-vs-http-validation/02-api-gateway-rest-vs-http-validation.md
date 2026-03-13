# 2. API Gateway REST vs HTTP: Request Validation and Transformations

<!--
difficulty: basic
concepts: [api-gateway-rest, api-gateway-http, request-validation, proxy-integration, payload-format]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: understand
prerequisites: [none]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a REST API and an HTTP API in API Gateway, both backed by a Lambda function. API Gateway charges per million requests and is negligible during testing. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally (for compiling the Lambda binary)
- curl or similar HTTP client for testing endpoints

## Learning Objectives

After completing this exercise, you will be able to:

- **Identify** the structural differences between REST APIs (v1) and HTTP APIs (v2) in API Gateway
- **Construct** a REST API with a JSON request model and request validation using Terraform
- **Verify** that the REST API rejects invalid payloads before they reach Lambda
- **Explain** how payload format 1.0 (REST proxy) and 2.0 (HTTP API) differ in the event object Lambda receives
- **Describe** the deployment model differences: explicit stage deployment for REST vs auto-deploy for HTTP APIs

## Why API Gateway Request Validation and Integration Types

API Gateway is the front door to most serverless applications on AWS, and the DVA-C02 exam expects you to understand both flavors: REST APIs and HTTP APIs. REST APIs (launched in 2015) offer the full feature set -- request validation, usage plans, API keys, WAF integration, and request/response transformations. HTTP APIs (launched in 2019) trade some of those features for lower latency and lower cost (roughly 70% cheaper). Knowing when to use which type is a frequent exam topic.

Request validation is a critical REST API feature that catches malformed requests at the API Gateway layer, before they reach your Lambda function. Without it, every invalid request still triggers a Lambda invocation -- consuming compute time and cost to return an error your API gateway could have caught. You define a JSON Schema model describing the expected request body, then attach a request validator to the method. API Gateway checks incoming payloads against the schema and returns a 400 error immediately for non-conforming requests. HTTP APIs do not support request validation natively, which is one of the key trade-offs the exam tests.

## Step 1 -- Create the Lambda Function Code

### `lambda/main.go`

```go
package main

import (
	"context"
	"encoding/json"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

func handler(ctx context.Context, event json.RawMessage) (events.APIGatewayProxyResponse, error) {
	// Parse the raw event to detect payload format version
	var raw map[string]interface{}
	json.Unmarshal(event, &raw)

	payloadVersion := "1.0"
	if v, ok := raw["version"].(string); ok && v == "2.0" {
		payloadVersion = v
	}

	bodyRaw := "{}"
	if b, ok := raw["body"].(string); ok {
		bodyRaw = b
	}

	var body interface{}
	if err := json.Unmarshal([]byte(bodyRaw), &body); err != nil {
		body = map[string]interface{}{}
	}

	var method, path, sourceAPI string
	if payloadVersion == "2.0" {
		sourceAPI = "HTTP API (v2)"
		if rc, ok := raw["requestContext"].(map[string]interface{}); ok {
			if httpInfo, ok := rc["http"].(map[string]interface{}); ok {
				method, _ = httpInfo["method"].(string)
				path, _ = httpInfo["path"].(string)
			}
		}
	} else {
		sourceAPI = "REST API (v1)"
		method, _ = raw["httpMethod"].(string)
		path, _ = raw["path"].(string)
	}

	responseBody, _ := json.MarshalIndent(map[string]interface{}{
		"source_api":      sourceAPI,
		"payload_version": payloadVersion,
		"method":          method,
		"path":            path,
		"received_body":   body,
	}, "", "  ")

	return events.APIGatewayProxyResponse{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(responseBody),
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
  default     = "apigw-comparison-demo"
}
```

### `build.tf`

```hcl
# -- Build the Go binary for Lambda --
resource "null_resource" "go_build" {
  triggers = {
    source_hash = filebase64sha256("${path.module}/main.go")
  }
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
# -- IAM role for Lambda --
data "aws_iam_policy_document" "lambda_assume_role" {
  statement {
    actions = ["sts:AssumeRole"]
    principals { type = "Service"; identifiers = ["lambda.amazonaws.com"] }
  }
}

resource "aws_iam_role" "this" {
  name               = "${var.project_name}-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json
}

resource "aws_iam_role_policy_attachment" "basic" {
  role       = aws_iam_role.this.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}
```

### `lambda.tf`

```hcl
# -- Lambda function: shared backend for both APIs --
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

### `monitoring.tf`

```hcl
resource "aws_cloudwatch_log_group" "this" {
  name              = "/aws/lambda/${var.project_name}"
  retention_in_days = 1
}
```

### `api.tf`

```hcl
# =================================================================
# REST API (v1): full-featured with request validation
# =================================================================
resource "aws_api_gateway_rest_api" "this" {
  name = "items-rest-api"
  endpoint_configuration { types = ["REGIONAL"] }
}

# /items resource -- every path segment is an explicit resource
resource "aws_api_gateway_resource" "items" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  parent_id   = aws_api_gateway_rest_api.this.root_resource_id
  path_part   = "items"
}

# JSON Schema model -- API Gateway validates the body BEFORE invoking Lambda
resource "aws_api_gateway_model" "item_model" {
  rest_api_id  = aws_api_gateway_rest_api.this.id
  name         = "ItemModel"
  content_type = "application/json"
  schema = jsonencode({
    "$schema" = "http://json-schema.org/draft-04/schema#"
    type = "object", required = ["name", "price"]
    properties = {
      name     = { type = "string", minLength = 1, maxLength = 100 }
      price    = { type = "number", minimum = 0 }
      category = { type = "string" }
    }
  })
}

resource "aws_api_gateway_request_validator" "body" {
  rest_api_id           = aws_api_gateway_rest_api.this.id
  name                  = "validate-body"
  validate_request_body = true
}

# POST /items with validator and model attached
resource "aws_api_gateway_method" "post_items" {
  rest_api_id          = aws_api_gateway_rest_api.this.id
  resource_id          = aws_api_gateway_resource.items.id
  http_method          = "POST"
  authorization        = "NONE"
  request_validator_id = aws_api_gateway_request_validator.body.id
  request_models       = { "application/json" = aws_api_gateway_model.item_model.name }
}

# AWS_PROXY passes the full request as payload format 1.0
resource "aws_api_gateway_integration" "post_lambda" {
  rest_api_id             = aws_api_gateway_rest_api.this.id
  resource_id             = aws_api_gateway_resource.items.id
  http_method             = aws_api_gateway_method.post_items.http_method
  type                    = "AWS_PROXY"
  integration_http_method = "POST"
  uri                     = aws_lambda_function.this.invoke_arn
}

# REST APIs require an explicit deployment + stage to go live
resource "aws_api_gateway_deployment" "this" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  triggers = {
    redeployment = sha1(jsonencode([
      aws_api_gateway_resource.items.id, aws_api_gateway_method.post_items.id,
      aws_api_gateway_integration.post_lambda.id, aws_api_gateway_model.item_model.id,
    ]))
  }
  lifecycle { create_before_destroy = true }
}

resource "aws_api_gateway_stage" "dev" {
  rest_api_id   = aws_api_gateway_rest_api.this.id
  deployment_id = aws_api_gateway_deployment.this.id
  stage_name    = "dev"
}

resource "aws_lambda_permission" "rest_api" {
  statement_id  = "AllowRESTAPIInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.this.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.this.execution_arn}/*/*"
}

# =================================================================
# HTTP API (v2): simpler, cheaper, auto-deploying
# =================================================================
resource "aws_apigatewayv2_api" "this" {
  name          = "items-http-api"
  protocol_type = "HTTP"
}

# Payload format 2.0 -- simplified event with requestContext.http
resource "aws_apigatewayv2_integration" "lambda" {
  api_id                 = aws_apigatewayv2_api.this.id
  integration_type       = "AWS_PROXY"
  integration_uri        = aws_lambda_function.this.invoke_arn
  payload_format_version = "2.0"
}

# Single resource combines method + path (vs separate resource/method in REST)
resource "aws_apigatewayv2_route" "post_items" {
  api_id    = aws_apigatewayv2_api.this.id
  route_key = "POST /items"
  target    = "integrations/${aws_apigatewayv2_integration.lambda.id}"
}

# auto_deploy = true: no explicit deployment resource needed
resource "aws_apigatewayv2_stage" "dev" {
  api_id      = aws_apigatewayv2_api.this.id
  name        = "dev"
  auto_deploy = true
}

resource "aws_lambda_permission" "http_api" {
  statement_id  = "AllowHTTPAPIInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.this.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.this.execution_arn}/*/*"
}
```

### `outputs.tf`

```hcl
output "rest_api_url"  { value = "${aws_api_gateway_stage.dev.invoke_url}/items" }
output "http_api_url"  { value = "${aws_apigatewayv2_stage.dev.invoke_url}/items" }
output "function_name" { value = aws_lambda_function.this.function_name }
```

## Step 3 -- Build and Apply

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init
terraform apply -auto-approve
```

Terraform will create approximately 16 resources: Lambda function, IAM role, CloudWatch log group, REST API with its resource/method/model/validator/integration/deployment/stage, HTTP API with its integration/route/stage, and two Lambda permissions.

## Step 4 -- Test the REST API with Request Validation

Send a valid request:

```bash
REST_URL=$(terraform output -raw rest_api_url)
curl -s -X POST "$REST_URL" \
  -H "Content-Type: application/json" \
  -d '{"name": "Widget", "price": 9.99, "category": "gadgets"}' | jq .
```

Expected: a 200 response showing `source_api: "REST API (v1)"` and `payload_version: "1.0"`.

Send an invalid request (missing required `price` field):

```bash
curl -s -X POST "$REST_URL" \
  -H "Content-Type: application/json" \
  -d '{"name": "Widget"}' | jq .
```

Expected: API Gateway returns a 400 error **without invoking Lambda**:

```json
{ "message": "Invalid request body" }
```

## Step 5 -- Test the HTTP API

```bash
HTTP_URL=$(terraform output -raw http_api_url)
curl -s -X POST "$HTTP_URL" \
  -H "Content-Type: application/json" \
  -d '{"name": "Widget", "price": 9.99}' | jq .
```

Expected: a 200 response showing `source_api: "HTTP API (v2)"` and `payload_version: "2.0"`.

Send the same invalid request:

```bash
curl -s -X POST "$HTTP_URL" \
  -H "Content-Type: application/json" \
  -d '{"name": "Widget"}' | jq .
```

Expected: a 200 response -- the HTTP API **does not validate** the request body. Lambda receives the incomplete payload. This is the key behavioral difference.

## Common Mistakes

### 1. Missing Content-Type header causes validation bypass on REST API

REST API request validation only triggers when the incoming Content-Type matches a model defined in `request_models`.

**Wrong:**

```bash
curl -s -X POST "$REST_URL" -d '{"name": "Widget"}'
```

**What happens:** The request reaches Lambda despite missing the required `price` field. Without `Content-Type: application/json`, the validator has no model to check against.

**Fix -- always include the Content-Type header:**

```bash
curl -s -X POST "$REST_URL" -H "Content-Type: application/json" -d '{"name": "Widget"}'
```

### 2. Forgetting the REST API deployment and stage

REST APIs require an explicit `aws_api_gateway_deployment` and `aws_api_gateway_stage`. Without them, the API exists but has no invoke URL.

**Wrong:**

```hcl
resource "aws_api_gateway_rest_api" "this" { name = "my-api" }
resource "aws_api_gateway_method" "post" { ... }
resource "aws_api_gateway_integration" "lambda" { ... }
# No deployment, no stage
```

**What happens:** `terraform apply` succeeds, but calling the API returns `403 Forbidden`.

**Fix -- add deployment with redeployment triggers and a stage:**

```hcl
resource "aws_api_gateway_deployment" "this" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  triggers    = { redeployment = sha1(jsonencode([...])) }
  lifecycle   { create_before_destroy = true }
}
resource "aws_api_gateway_stage" "dev" {
  rest_api_id   = aws_api_gateway_rest_api.this.id
  deployment_id = aws_api_gateway_deployment.this.id
  stage_name    = "dev"
}
```

### 3. Lambda response format mismatch

REST API proxy integration requires `StatusCode` (integer) and `Body` (string). Returning a plain struct without these fields causes a 502.

**Wrong:**

```go
func handler(ctx context.Context, event events.APIGatewayProxyRequest) (map[string]interface{}, error) {
	return map[string]interface{}{"message": "success", "items": []string{}}, nil
}
```

**What happens:** REST API returns `502 Bad Gateway`. CloudWatch logs show `Malformed Lambda proxy response`.

**Fix:**

```go
func handler(ctx context.Context, event events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	body, _ := json.Marshal(map[string]interface{}{"message": "success", "items": []string{}})
	return events.APIGatewayProxyResponse{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(body),
	}, nil
}
```

HTTP API v2 is more forgiving -- it can automatically wrap a plain return value into a valid response.

## Verify What You Learned

```bash
REST_URL=$(terraform output -raw rest_api_url)
HTTP_URL=$(terraform output -raw http_api_url)

# REST API rejects invalid payloads (returns 400)
curl -s -o /dev/null -w "%{http_code}" -X POST "$REST_URL" \
  -H "Content-Type: application/json" -d '{"name": "test"}'
```

Expected: `400`

```bash
# HTTP API passes all payloads to Lambda (returns 200)
curl -s -o /dev/null -w "%{http_code}" -X POST "$HTTP_URL" \
  -H "Content-Type: application/json" -d '{"name": "test"}'
```

Expected: `200`

```bash
aws apigateway get-request-validators \
  --rest-api-id $(aws apigateway get-rest-apis --query "items[?name=='items-rest-api'].id" --output text) \
  --query "items[0].validateRequestBody"
```

Expected: `true`

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

You compared the two API Gateway types and saw how REST APIs can validate requests before they reach Lambda. In the next exercise, you will work directly with **DynamoDB using the AWS SDK for Go v2**, performing PutItem, GetItem, Query, Scan, and UpdateItem operations -- and learn why expression attributes and consistent reads matter for the developer exam.

## Summary

- **REST APIs** (v1) support request models, validation, usage plans, and API keys but require explicit deployments
- **HTTP APIs** (v2) are simpler, cheaper, and auto-deploy but lack native request validation
- Request validation on REST APIs uses **JSON Schema models** and catches invalid payloads at the gateway layer
- Validation only applies when the **Content-Type header matches** the model's content type mapping
- **Payload format 1.0** (REST proxy) includes `httpMethod` and `path` at the top level
- **Payload format 2.0** (HTTP API) nests HTTP details under `requestContext.http`
- Lambda responses for REST API proxy integration **must** include `StatusCode` (integer) and `Body` (string)

## Reference

- [API Gateway REST API vs HTTP API](https://docs.aws.amazon.com/apigateway/latest/developerguide/http-api-vs-rest.html)
- [Request Validation in REST APIs](https://docs.aws.amazon.com/apigateway/latest/developerguide/api-gateway-method-request-validation.html)
- [Terraform aws_api_gateway_rest_api Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_rest_api)
- [Terraform aws_apigatewayv2_api Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/apigatewayv2_api)

## Additional Resources

- [API Gateway Payload Format Versions](https://docs.aws.amazon.com/apigateway/latest/developerguide/http-api-develop-integrations-lambda.html) -- detailed comparison of 1.0 and 2.0 event formats
- [API Gateway Request Models](https://docs.aws.amazon.com/apigateway/latest/developerguide/models-mappings.html) -- how to define JSON Schema models for validation
- [Lambda Proxy Integration for REST APIs](https://docs.aws.amazon.com/apigateway/latest/developerguide/set-up-lambda-proxy-integrations.html) -- required request/response format for proxy integrations
- [Choosing Between REST and HTTP APIs](https://docs.aws.amazon.com/apigateway/latest/developerguide/http-api-vs-rest.html) -- AWS decision guide for selecting the right API type
- [Building Lambda Functions with Go](https://docs.aws.amazon.com/lambda/latest/dg/lambda-golang.html) -- official guide for deploying Go functions with the provided.al2023 runtime

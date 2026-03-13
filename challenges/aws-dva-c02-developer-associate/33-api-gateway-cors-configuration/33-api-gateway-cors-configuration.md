# 33. API Gateway CORS Configuration

<!--
difficulty: intermediate
concepts: [cors, preflight-request, options-method, mock-integration, access-control-headers, rest-api-cors, http-api-cors, gateway-response]
tools: [terraform, aws-cli, curl]
estimated_time: 35m
bloom_level: analyze, implement
prerequisites: [02-api-gateway-rest-vs-http-validation]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a REST API and an HTTP API, each with a Lambda backend. Costs are negligible during testing (~$0.01/hr or less). Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| curl installed | `curl --version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Analyze** the CORS preflight request flow -- how browsers send an OPTIONS request before the actual request and what headers the server must return
2. **Implement** CORS on a REST API using an OPTIONS method with mock integration and explicit `Access-Control-*` response headers
3. **Configure** CORS on an HTTP API using the built-in `cors_configuration` block in Terraform
4. **Differentiate** between CORS headers returned by the gateway (Method Response headers) and CORS headers returned by the Lambda function (proxy integration response)
5. **Debug** CORS failures caused by missing gateway response configuration for 4XX/5XX errors

## Why This Matters

CORS (Cross-Origin Resource Sharing) is a browser security mechanism that blocks JavaScript from calling APIs on a different domain. When a React app on `app.example.com` calls your API at `api.example.com`, the browser sends a preflight OPTIONS request to check if the API allows cross-origin requests. If the API does not return the correct `Access-Control-Allow-Origin`, `Access-Control-Allow-Methods`, and `Access-Control-Allow-Headers` response headers, the browser blocks the request before it reaches your code.

On the DVA-C02 exam, CORS questions test two things. First, the difference between REST API and HTTP API CORS configuration: REST APIs require you to manually create an OPTIONS method with a mock integration and configure each response header, while HTTP APIs have a built-in `cors` configuration block that handles everything. Second, a common trap: even if your Lambda returns CORS headers in its response, API Gateway error responses (4XX, 5XX) generated before reaching Lambda -- like 403 Forbidden from an authorizer or 429 from throttling -- do not include CORS headers unless you configure Gateway Responses. This causes the browser to show a CORS error instead of the actual error, making debugging difficult.

## Building Blocks

### Lambda Function Code

Create a file called `main.go`. This Lambda returns CORS headers in its response for proxy integration:

### `lambda/main.go`

```go
// main.go
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
		"message":   "Hello from Lambda",
		"method":    req.HTTPMethod,
		"path":      req.Path,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})

	return events.APIGatewayProxyResponse{
		StatusCode: 200,
		Headers: map[string]string{
			"Content-Type":                 "application/json",
			"Access-Control-Allow-Origin":  "*",
			"Access-Control-Allow-Methods": "GET,POST,OPTIONS",
			"Access-Control-Allow-Headers": "Content-Type,Authorization",
		},
		Body: string(body),
	}, nil
}

func main() {
	lambda.Start(handler)
}
```

### `lambda/go.mod`

```text
module lambda

go 1.21

require github.com/aws/aws-lambda-go v1.47.0
```

### Terraform Skeleton

Create the following files in your exercise directory. Fill in the `# TODO` blocks.

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
  default     = "cors-lab"
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

resource "aws_lambda_permission" "rest" {
  statement_id  = "AllowRESTAPI"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.this.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.rest.execution_arn}/*/*"
}
```

### `api.tf`

```hcl
# ============================================================
# PART A: REST API with manual CORS
# ============================================================
resource "aws_api_gateway_rest_api" "rest" {
  name = "${var.project_name}-rest-api"
}

resource "aws_api_gateway_resource" "items" {
  rest_api_id = aws_api_gateway_rest_api.rest.id
  parent_id   = aws_api_gateway_rest_api.rest.root_resource_id
  path_part   = "items"
}

# GET /items -- proxy integration (Lambda returns CORS headers)
resource "aws_api_gateway_method" "get_items" {
  rest_api_id   = aws_api_gateway_rest_api.rest.id
  resource_id   = aws_api_gateway_resource.items.id
  http_method   = "GET"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "get_items" {
  rest_api_id             = aws_api_gateway_rest_api.rest.id
  resource_id             = aws_api_gateway_resource.items.id
  http_method             = aws_api_gateway_method.get_items.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.this.invoke_arn
}

# =======================================================
# TODO 1 -- OPTIONS method with MOCK integration (api.tf)
# =======================================================
# Requirements:
#   - Create an aws_api_gateway_method for OPTIONS on /items
#     with authorization = "NONE"
#   - Create an aws_api_gateway_integration with type = "MOCK"
#     and request_templates = { "application/json" = "{\"statusCode\": 200}" }
#   - Create an aws_api_gateway_method_response for 200 with
#     response_parameters:
#       "method.response.header.Access-Control-Allow-Headers" = true
#       "method.response.header.Access-Control-Allow-Methods" = true
#       "method.response.header.Access-Control-Allow-Origin"  = true
#   - Create an aws_api_gateway_integration_response for 200 with
#     response_parameters that set the actual header values:
#       "method.response.header.Access-Control-Allow-Headers" = "'Content-Type,Authorization'"
#       "method.response.header.Access-Control-Allow-Methods" = "'GET,POST,OPTIONS'"
#       "method.response.header.Access-Control-Allow-Origin"  = "'*'"
#     (note: values are single-quoted strings inside double quotes)
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_integration


# =======================================================
# TODO 2 -- Gateway Response for 4XX errors with CORS (api.tf)
# =======================================================
# Requirements:
#   - Create an aws_api_gateway_gateway_response for
#     response_type = "DEFAULT_4XX"
#   - Set response_parameters to include CORS headers:
#       "gatewayresponse.header.Access-Control-Allow-Origin"  = "'*'"
#       "gatewayresponse.header.Access-Control-Allow-Headers" = "'Content-Type,Authorization'"
#   - Without this, 403 and 429 errors from API Gateway itself
#     (before reaching Lambda) will not include CORS headers,
#     and browsers will show a CORS error instead of the actual error
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_gateway_response


# =======================================================
# TODO 3 -- REST API Deployment and Stage (api.tf)
# =======================================================
# Requirements:
#   - Create aws_api_gateway_deployment with triggers
#   - Create aws_api_gateway_stage "dev"


# ============================================================
# PART B: HTTP API with built-in CORS
# ============================================================

# =======================================================
# TODO 4 -- HTTP API with cors_configuration block (api.tf)
# =======================================================
# Requirements:
#   - Create an aws_apigatewayv2_api with protocol_type = "HTTP"
#   - Add a cors_configuration block:
#       allow_origins = ["*"]
#       allow_methods = ["GET", "POST", "OPTIONS"]
#       allow_headers = ["Content-Type", "Authorization"]
#       max_age       = 3600
#   - Create an aws_apigatewayv2_integration (type "AWS_PROXY",
#     integration_type = "AWS_PROXY", integration_method = "POST",
#     payload_format_version = "2.0")
#   - Create an aws_apigatewayv2_route for "GET /items"
#   - Create an aws_apigatewayv2_stage named "$default" with auto_deploy = true
#   - Create an aws_lambda_permission for the HTTP API
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/apigatewayv2_api
```

### `outputs.tf`

```hcl
output "rest_api_url" { value = aws_api_gateway_stage.dev.invoke_url }
output "http_api_url" { value = aws_apigatewayv2_stage.default.invoke_url }
```

## Spot the Bug

A developer configured CORS on a REST API. The GET request works from the browser, but when the API returns a 403 Forbidden (from a missing API key), the browser shows "CORS error" instead of "403 Forbidden." **What is wrong?**

```hcl
# OPTIONS method with CORS headers -- configured correctly
resource "aws_api_gateway_method" "options" {
  rest_api_id   = aws_api_gateway_rest_api.this.id
  resource_id   = aws_api_gateway_resource.items.id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "options" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  resource_id = aws_api_gateway_resource.items.id
  http_method = aws_api_gateway_method.options.http_method
  type        = "MOCK"
  request_templates = { "application/json" = "{\"statusCode\": 200}" }
}

# Lambda returns CORS headers in its 200 response -- correct

# BUT: No aws_api_gateway_gateway_response for DEFAULT_4XX
```

<details>
<summary>Explain the bug</summary>

The CORS setup only covers two cases: the OPTIONS preflight (mock integration returns CORS headers) and successful Lambda responses (Lambda includes CORS headers in its response body). But when API Gateway itself generates an error response -- such as 403 Forbidden from a missing API key, 429 Too Many Requests from throttling, or 401 Unauthorized from an authorizer -- the response is generated by API Gateway, not Lambda. These gateway-generated responses do not include CORS headers by default.

Without `Access-Control-Allow-Origin` on the 403 response, the browser treats it as a CORS violation and shows "CORS error" instead of the actual 403 error message. This makes debugging extremely difficult because the real error is hidden.

The fix -- add Gateway Responses for both 4XX and 5XX:

```hcl
resource "aws_api_gateway_gateway_response" "cors_4xx" {
  rest_api_id   = aws_api_gateway_rest_api.this.id
  response_type = "DEFAULT_4XX"

  response_parameters = {
    "gatewayresponse.header.Access-Control-Allow-Origin"  = "'*'"
    "gatewayresponse.header.Access-Control-Allow-Headers" = "'Content-Type,Authorization'"
  }
}

resource "aws_api_gateway_gateway_response" "cors_5xx" {
  rest_api_id   = aws_api_gateway_rest_api.this.id
  response_type = "DEFAULT_5XX"

  response_parameters = {
    "gatewayresponse.header.Access-Control-Allow-Origin"  = "'*'"
    "gatewayresponse.header.Access-Control-Allow-Headers" = "'Content-Type,Authorization'"
  }
}
```

This is a common DVA-C02 exam question: "Lambda returns CORS headers but the browser still shows CORS errors for 4XX responses." The answer is always Gateway Responses.

</details>

## Solutions

<details>
<summary>TODO 1 -- OPTIONS method with MOCK integration</summary>

### `api.tf`

```hcl
resource "aws_api_gateway_method" "options_items" {
  rest_api_id   = aws_api_gateway_rest_api.rest.id
  resource_id   = aws_api_gateway_resource.items.id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "options_items" {
  rest_api_id = aws_api_gateway_rest_api.rest.id
  resource_id = aws_api_gateway_resource.items.id
  http_method = aws_api_gateway_method.options_items.http_method
  type        = "MOCK"

  request_templates = {
    "application/json" = "{\"statusCode\": 200}"
  }
}

resource "aws_api_gateway_method_response" "options_200" {
  rest_api_id = aws_api_gateway_rest_api.rest.id
  resource_id = aws_api_gateway_resource.items.id
  http_method = aws_api_gateway_method.options_items.http_method
  status_code = "200"

  response_parameters = {
    "method.response.header.Access-Control-Allow-Headers" = true
    "method.response.header.Access-Control-Allow-Methods" = true
    "method.response.header.Access-Control-Allow-Origin"  = true
  }

  response_models = {
    "application/json" = "Empty"
  }
}

resource "aws_api_gateway_integration_response" "options_200" {
  rest_api_id = aws_api_gateway_rest_api.rest.id
  resource_id = aws_api_gateway_resource.items.id
  http_method = aws_api_gateway_method.options_items.http_method
  status_code = aws_api_gateway_method_response.options_200.status_code

  response_parameters = {
    "method.response.header.Access-Control-Allow-Headers" = "'Content-Type,Authorization'"
    "method.response.header.Access-Control-Allow-Methods" = "'GET,POST,OPTIONS'"
    "method.response.header.Access-Control-Allow-Origin"  = "'*'"
  }

  depends_on = [aws_api_gateway_integration.options_items]
}
```

</details>

<details>
<summary>TODO 2 -- Gateway Response for 4XX errors</summary>

### `api.tf`

```hcl
resource "aws_api_gateway_gateway_response" "cors_4xx" {
  rest_api_id   = aws_api_gateway_rest_api.rest.id
  response_type = "DEFAULT_4XX"

  response_parameters = {
    "gatewayresponse.header.Access-Control-Allow-Origin"  = "'*'"
    "gatewayresponse.header.Access-Control-Allow-Headers" = "'Content-Type,Authorization'"
  }
}

resource "aws_api_gateway_gateway_response" "cors_5xx" {
  rest_api_id   = aws_api_gateway_rest_api.rest.id
  response_type = "DEFAULT_5XX"

  response_parameters = {
    "gatewayresponse.header.Access-Control-Allow-Origin"  = "'*'"
    "gatewayresponse.header.Access-Control-Allow-Headers" = "'Content-Type,Authorization'"
  }
}
```

</details>

<details>
<summary>TODO 3 -- REST API Deployment and Stage</summary>

### `api.tf`

```hcl
resource "aws_api_gateway_deployment" "rest" {
  rest_api_id = aws_api_gateway_rest_api.rest.id

  triggers = {
    redeployment = sha1(jsonencode([
      aws_api_gateway_resource.items.id,
      aws_api_gateway_method.get_items.id,
      aws_api_gateway_method.options_items.id,
      aws_api_gateway_integration.get_items.id,
      aws_api_gateway_integration.options_items.id,
      aws_api_gateway_gateway_response.cors_4xx.id,
    ]))
  }

  lifecycle { create_before_destroy = true }

  depends_on = [
    aws_api_gateway_integration.get_items,
    aws_api_gateway_integration.options_items,
    aws_api_gateway_integration_response.options_200,
  ]
}

resource "aws_api_gateway_stage" "dev" {
  deployment_id = aws_api_gateway_deployment.rest.id
  rest_api_id   = aws_api_gateway_rest_api.rest.id
  stage_name    = "dev"
}
```

</details>

<details>
<summary>TODO 4 -- HTTP API with cors_configuration</summary>

### `api.tf`

```hcl
resource "aws_apigatewayv2_api" "http" {
  name          = "${var.project_name}-http-api"
  protocol_type = "HTTP"

  cors_configuration {
    allow_origins = ["*"]
    allow_methods = ["GET", "POST", "OPTIONS"]
    allow_headers = ["Content-Type", "Authorization"]
    max_age       = 3600
  }
}

resource "aws_apigatewayv2_integration" "http" {
  api_id                 = aws_apigatewayv2_api.http.id
  integration_type       = "AWS_PROXY"
  integration_method     = "POST"
  integration_uri        = aws_lambda_function.this.invoke_arn
  payload_format_version = "2.0"
}

resource "aws_apigatewayv2_route" "get_items" {
  api_id    = aws_apigatewayv2_api.http.id
  route_key = "GET /items"
  target    = "integrations/${aws_apigatewayv2_integration.http.id}"
}

resource "aws_apigatewayv2_stage" "default" {
  api_id      = aws_apigatewayv2_api.http.id
  name        = "$default"
  auto_deploy = true
}

resource "aws_lambda_permission" "http" {
  statement_id  = "AllowHTTPAPI"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.this.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.http.execution_arn}/*/*"
}
```

</details>

## Verify What You Learned

### Step 1 -- Deploy

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init && terraform apply -auto-approve
```

### Step 2 -- Test REST API OPTIONS preflight

```bash
REST_URL=$(terraform output -raw rest_api_url)

curl -s -X OPTIONS "${REST_URL}/items" \
  -H "Origin: https://example.com" \
  -H "Access-Control-Request-Method: GET" \
  -D - -o /dev/null 2>/dev/null | grep -i "access-control"
```

Expected: `Access-Control-Allow-Origin: *`, `Access-Control-Allow-Methods: GET,POST,OPTIONS`, `Access-Control-Allow-Headers: Content-Type,Authorization`.

### Step 3 -- Test HTTP API CORS

```bash
HTTP_URL=$(terraform output -raw http_api_url)

curl -s -X OPTIONS "${HTTP_URL}/items" \
  -H "Origin: https://example.com" \
  -H "Access-Control-Request-Method: GET" \
  -D - -o /dev/null 2>/dev/null | grep -i "access-control"
```

Expected: Same CORS headers, automatically handled by HTTP API.

### Step 4 -- Verify no drift

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

```bash
terraform destroy -auto-approve
terraform state list
```

Expected: no output (empty state).

## What's Next

You configured CORS on both REST and HTTP APIs and learned why Gateway Responses are critical for CORS on error responses. In the next exercise, you will explore **API Gateway stage variables and canary deployments** -- using stage variables to route to different Lambda aliases and gradually shifting traffic with canary settings.

## Summary

- **CORS preflight** is an OPTIONS request sent by browsers before cross-origin requests; the response must include `Access-Control-Allow-Origin`, `Access-Control-Allow-Methods`, and `Access-Control-Allow-Headers`
- **REST API CORS** requires manual setup: OPTIONS method with MOCK integration, Method Response headers, and Integration Response header values
- **HTTP API CORS** uses a built-in `cors_configuration` block that handles preflight automatically
- **Gateway Responses** for `DEFAULT_4XX` and `DEFAULT_5XX` are essential -- without them, API Gateway error responses (403, 429) lack CORS headers and browsers show "CORS error" instead of the actual error
- In proxy integration, Lambda must return CORS headers in its response; in custom integration, set them in the Integration Response
- `max_age` controls how long browsers cache the preflight response (reducing OPTIONS requests)

## Reference

- [API Gateway CORS](https://docs.aws.amazon.com/apigateway/latest/developerguide/how-to-cors.html)
- [API Gateway Gateway Responses](https://docs.aws.amazon.com/apigateway/latest/developerguide/api-gateway-gatewayResponse-definition.html)
- [HTTP API CORS](https://docs.aws.amazon.com/apigateway/latest/developerguide/http-api-cors.html)
- [Terraform aws_api_gateway_gateway_response](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_gateway_response)

## Additional Resources

- [CORS Specification (MDN)](https://developer.mozilla.org/en-US/docs/Web/HTTP/CORS) -- detailed browser-side CORS behavior and preflight conditions
- [Enabling CORS for REST API Resources](https://docs.aws.amazon.com/apigateway/latest/developerguide/how-to-cors-console.html) -- step-by-step AWS guide for REST API CORS
- [Configuring CORS for HTTP APIs](https://docs.aws.amazon.com/apigateway/latest/developerguide/http-api-cors.html) -- HTTP API cors_configuration reference
- [Troubleshooting CORS Issues](https://docs.aws.amazon.com/apigateway/latest/developerguide/apigateway-cors-troubleshooting.html) -- common CORS failure scenarios and solutions

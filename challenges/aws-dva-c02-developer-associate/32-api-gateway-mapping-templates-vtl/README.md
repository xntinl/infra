# 32. API Gateway Mapping Templates with VTL

<!--
difficulty: intermediate
concepts: [vtl-mapping-templates, velocity-template-language, request-transformation, response-transformation, input-variables, context-variables, set-directive, foreach-directive, if-directive]
tools: [terraform, aws-cli, curl]
estimated_time: 50m
bloom_level: analyze, implement
prerequisites: [31-api-gateway-proxy-vs-custom-integration]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a REST API with Lambda custom integrations. API Gateway and Lambda costs are negligible during testing (~$0.01/hr or less). Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| curl installed | `curl --version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Analyze** VTL mapping template syntax including `$input`, `$context`, and `$stageVariables` variable families
2. **Implement** request mapping templates that extract query parameters, path parameters, and headers into a structured JSON payload for Lambda
3. **Construct** response mapping templates that reshape Lambda output, add computed fields, and conditionally include or exclude data using `#if` and `#foreach` directives
4. **Differentiate** between `$input.params()`, `$input.path()`, `$input.body`, and `$input.json()` for accessing different parts of the request
5. **Debug** common VTL syntax errors including incorrect `#set` directive usage and missing `$util.escapeJavaScript()` calls

## Why This Matters

VTL mapping templates are the transformation engine of API Gateway REST APIs. When your API contract does not match your Lambda function's expected input format, mapping templates bridge the gap without requiring code changes. A frontend team expects `snake_case` field names but your Lambda returns `camelCase`? A mapping template handles the conversion. A mobile client sends query parameters but your Lambda expects a JSON body? A mapping template reshapes the request.

The DVA-C02 exam includes questions where you must read a VTL template and predict its output, identify syntax errors, or choose the correct `$input` method for a given scenario. Common traps include confusing `$input.path('$.field')` (extracts a value from JSON) with `$input.params('field')` (reads query/path/header parameters), and forgetting that `#set` requires parentheses around the assignment. Understanding the `$context` variable is also tested -- it provides the request ID, API ID, stage, identity, and authorizer claims.

## Building Blocks

### Lambda Function Code

Create a file called `main.go`. This Lambda returns structured data that the response mapping template will transform:

### `lambda/main.go`

```go
// main.go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
)

type OrderRequest struct {
	CustomerID string `json:"customer_id"`
	OrderDate  string `json:"order_date"`
	Priority   string `json:"priority"`
	RequestID  string `json:"request_id"`
}

type OrderItem struct {
	SKU      string  `json:"sku"`
	Name     string  `json:"name"`
	Price    float64 `json:"price"`
	Quantity int     `json:"quantity"`
}

func handler(ctx context.Context, req OrderRequest) (map[string]interface{}, error) {
	if req.CustomerID == "" {
		return nil, errors.New("ValidationError: customer_id is required")
	}

	items := []OrderItem{
		{SKU: "WIDGET-A", Name: "Blue Widget", Price: 12.99, Quantity: 2},
		{SKU: "GADGET-B", Name: "Red Gadget", Price: 24.50, Quantity: 1},
		{SKU: "THING-C", Name: "Green Thing", Price: 8.75, Quantity: 5},
	}

	return map[string]interface{}{
		"customer_id":  req.CustomerID,
		"order_date":   req.OrderDate,
		"priority":     req.Priority,
		"request_id":   req.RequestID,
		"items":        items,
		"item_count":   len(items),
		"generated_at": time.Now().UTC().Format(time.RFC3339),
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
  default     = "vtl-templates"
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

resource "aws_lambda_permission" "apigw" {
  statement_id  = "AllowAPIGateway"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.this.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.this.execution_arn}/*/*"
}
```

### `api.tf`

```hcl
resource "aws_api_gateway_rest_api" "this" {
  name = "${var.project_name}-api"
}

# /orders/{customer_id}
resource "aws_api_gateway_resource" "orders" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  parent_id   = aws_api_gateway_rest_api.this.root_resource_id
  path_part   = "orders"
}

resource "aws_api_gateway_resource" "orders_by_customer" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  parent_id   = aws_api_gateway_resource.orders.id
  path_part   = "{customer_id}"
}

resource "aws_api_gateway_method" "get_orders" {
  rest_api_id   = aws_api_gateway_rest_api.this.id
  resource_id   = aws_api_gateway_resource.orders_by_customer.id
  http_method   = "GET"
  authorization = "NONE"

  request_parameters = {
    "method.request.path.customer_id"       = true
    "method.request.querystring.order_date"  = false
    "method.request.querystring.priority"    = false
    "method.request.header.X-Request-Id"     = false
  }
}

# =======================================================
# TODO 1 -- Request Mapping Template (api.tf)
# =======================================================
# Requirements:
#   - Create an aws_api_gateway_integration (type "AWS")
#   - Set passthrough_behavior = "WHEN_NO_TEMPLATES"
#   - Write a request_templates for "application/json" that:
#     a) Extracts the path parameter {customer_id} using $input.params('customer_id')
#     b) Extracts the query string parameter "order_date" using $input.params('order_date')
#     c) Extracts the query string parameter "priority", defaulting to "normal"
#        using: #if($input.params('priority') != "")$input.params('priority')#{else}normal#end
#     d) Extracts the X-Request-Id header using $input.params().header.get('X-Request-Id')
#     e) Injects the API Gateway request context ID using $context.requestId
#
# Expected template output:
#   {
#     "customer_id": "<from path>",
#     "order_date": "<from query>",
#     "priority": "<from query or default>",
#     "request_id": "<from header or context>"
#   }


# =======================================================
# TODO 2 -- Response Mapping Template (200 OK) (api.tf)
# =======================================================
# Requirements:
#   - Create aws_api_gateway_method_response for 200
#   - Create aws_api_gateway_integration_response for 200
#   - Write a response_templates for "application/json" that:
#     a) Uses #set to assign the Lambda response body to a variable
#     b) Uses #foreach to iterate over the items array and build a
#        transformed array with only "sku" and "total" (price * quantity)
#     c) Adds a "meta" object with request_id and generated_at
#
# Expected response shape:
#   {
#     "customer": "<customer_id>",
#     "order_count": <item_count>,
#     "items": [
#       { "sku": "WIDGET-A", "total": 25.98 },
#       ...
#     ],
#     "meta": {
#       "request_id": "<from lambda>",
#       "generated_at": "<timestamp>"
#     }
#   }


# =======================================================
# TODO 3 -- Error Response Mapping (400) (api.tf)
# =======================================================
# Requirements:
#   - Create aws_api_gateway_method_response for 400
#   - Create aws_api_gateway_integration_response for 400
#     with selection_pattern = ".*ValidationError.*"
#   - Write a response_templates that extracts the error message
#     using $input.path('$.errorMessage')
#   - Include $context.requestId in the error response


# =======================================================
# TODO 4 -- Deployment and Stage (api.tf)
# =======================================================
# Requirements:
#   - Create aws_api_gateway_deployment with triggers
#   - Create aws_api_gateway_stage "dev"
```

### `outputs.tf`

```hcl
output "api_url" { value = aws_api_gateway_stage.dev.invoke_url }
output "orders_endpoint" {
  value = "${aws_api_gateway_stage.dev.invoke_url}/orders/{customer_id}"
}
```

## Spot the Bug

A developer wrote a VTL mapping template to set a default value for a missing query parameter. The API returns `{"priority": ""}` instead of `{"priority": "normal"}`. **What is wrong?**

```velocity
#set($priority = $input.params('priority'))
#if($priority != "")
  #set($priorityValue = $priority)
#else
  #set($priorityValue = "normal")
#end
{
  "customer_id": "$input.params('customer_id')",
  "priority": "$priorityValue"
}
```

<details>
<summary>Explain the bug</summary>

The `#set` directive in VTL does not create the variable if the right-hand side evaluates to `null`. When the `priority` query parameter is missing, `$input.params('priority')` returns an empty string `""`, not `null`. However, the comparison `$priority != ""` should catch this.

The actual bug is more subtle: the whitespace around the `#set` and `#if` directives is included in the output. VTL treats everything outside directives as literal text, so the template produces extra whitespace and newlines that can break JSON parsing. Additionally, in some VTL implementations, an empty string comparison may behave unexpectedly.

The correct approach uses compact VTL syntax to avoid whitespace issues:

```velocity
#set($priority = $input.params('priority'))
#if($priority == "")#set($priority = "normal")#end
{
  "customer_id": "$input.params('customer_id')",
  "priority": "$priority"
}
```

Key differences:
1. Keep `#if` and `#set` on the same line to avoid injecting whitespace into the JSON output
2. Use `== ""` instead of `!= ""` with the else branch to simplify the logic
3. Reuse the same variable `$priority` instead of creating a new `$priorityValue`

On the exam, VTL whitespace issues and `#set` variable scoping are common traps. The `#set` directive does not create newlines, but the line breaks around it do.

</details>

## Solutions

<details>
<summary>TODO 1 -- Request Mapping Template</summary>

### `api.tf`

```hcl
resource "aws_api_gateway_integration" "get_orders" {
  rest_api_id             = aws_api_gateway_rest_api.this.id
  resource_id             = aws_api_gateway_resource.orders_by_customer.id
  http_method             = aws_api_gateway_method.get_orders.http_method
  integration_http_method = "POST"
  type                    = "AWS"
  uri                     = aws_lambda_function.this.invoke_arn
  passthrough_behavior    = "WHEN_NO_TEMPLATES"

  request_templates = {
    "application/json" = <<EOF
#set($priority = $input.params('priority'))
#if($priority == "")#set($priority = "normal")#end
#set($reqId = $input.params().header.get('X-Request-Id'))
#if($reqId == "")#set($reqId = $context.requestId)#end
{
  "customer_id": "$input.params('customer_id')",
  "order_date": "$input.params('order_date')",
  "priority": "$priority",
  "request_id": "$reqId"
}
EOF
  }
}
```

</details>

<details>
<summary>TODO 2 -- Response Mapping Template (200 OK)</summary>

### `api.tf`

```hcl
resource "aws_api_gateway_method_response" "ok" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  resource_id = aws_api_gateway_resource.orders_by_customer.id
  http_method = aws_api_gateway_method.get_orders.http_method
  status_code = "200"

  response_models = {
    "application/json" = "Empty"
  }
}

resource "aws_api_gateway_integration_response" "ok" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  resource_id = aws_api_gateway_resource.orders_by_customer.id
  http_method = aws_api_gateway_method.get_orders.http_method
  status_code = aws_api_gateway_method_response.ok.status_code

  response_templates = {
    "application/json" = <<EOF
#set($body = $input.path('$'))
{
  "customer": "$body.customer_id",
  "order_count": $body.item_count,
  "items": [
#foreach($item in $body.items)
    {
      "sku": "$item.sku",
      "total": $item.price * $item.quantity
    }#if($foreach.hasNext),#end
#end
  ],
  "meta": {
    "request_id": "$body.request_id",
    "generated_at": "$body.generated_at"
  }
}
EOF
  }

  depends_on = [aws_api_gateway_integration.get_orders]
}
```

</details>

<details>
<summary>TODO 3 -- Error Response Mapping (400)</summary>

### `api.tf`

```hcl
resource "aws_api_gateway_method_response" "bad_request" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  resource_id = aws_api_gateway_resource.orders_by_customer.id
  http_method = aws_api_gateway_method.get_orders.http_method
  status_code = "400"

  response_models = {
    "application/json" = "Error"
  }
}

resource "aws_api_gateway_integration_response" "bad_request" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  resource_id = aws_api_gateway_resource.orders_by_customer.id
  http_method = aws_api_gateway_method.get_orders.http_method
  status_code = aws_api_gateway_method_response.bad_request.status_code

  selection_pattern = ".*ValidationError.*"

  response_templates = {
    "application/json" = <<EOF
#set($errorMsg = $input.path('$.errorMessage'))
{
  "status": "error",
  "message": "$errorMsg",
  "request_id": "$context.requestId"
}
EOF
  }

  depends_on = [aws_api_gateway_integration.get_orders]
}
```

</details>

<details>
<summary>TODO 4 -- Deployment and Stage</summary>

### `api.tf`

```hcl
resource "aws_api_gateway_deployment" "this" {
  rest_api_id = aws_api_gateway_rest_api.this.id

  triggers = {
    redeployment = sha1(jsonencode([
      aws_api_gateway_resource.orders_by_customer.id,
      aws_api_gateway_method.get_orders.id,
      aws_api_gateway_integration.get_orders.id,
      aws_api_gateway_integration_response.ok.id,
      aws_api_gateway_integration_response.bad_request.id,
    ]))
  }

  lifecycle { create_before_destroy = true }

  depends_on = [
    aws_api_gateway_integration.get_orders,
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

### Step 1 -- Deploy

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init && terraform apply -auto-approve
```

### Step 2 -- Test with all parameters

```bash
API_URL=$(terraform output -raw api_url)

curl -s -H "X-Request-Id: my-req-123" \
  "${API_URL}/orders/CUST-001?order_date=2024-01-15&priority=high" | jq .
```

Expected: Response with `customer: "CUST-001"`, `items` array with computed `total` fields, and `meta.request_id: "my-req-123"`.

### Step 3 -- Test default priority

```bash
curl -s "${API_URL}/orders/CUST-002?order_date=2024-01-15" | jq .priority
```

Expected: `null` at top level (priority is inside the Lambda response, not the transformed output). The request mapping template should have set priority to `"normal"` before passing to Lambda.

### Step 4 -- Test validation error

```bash
curl -s -o /dev/null -w "%{http_code}" "${API_URL}/orders/%20"
```

Expected: `400` with a ValidationError message.

### Step 5 -- Verify no drift

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

You mastered VTL mapping templates for request and response transformation. In the next exercise, you will configure **CORS on API Gateway** -- setting up OPTIONS methods with mock integrations on REST APIs and using the built-in CORS configuration block on HTTP APIs.

## Summary

- VTL mapping templates transform requests and responses in API Gateway custom (AWS) integrations without modifying Lambda code
- **`$input.params('name')`** reads query string, path, and header parameters; **`$input.path('$.field')`** extracts fields from the JSON body; **`$input.body`** reads the raw body
- **`$context`** provides request metadata: `$context.requestId`, `$context.apiId`, `$context.stage`, `$context.identity.sourceIp`
- **`#set($var = value)`** assigns variables; **`#if`/`#else`/`#end`** provides conditional logic; **`#foreach($item in $list)`** iterates over arrays
- VTL whitespace is significant -- line breaks around directives appear in the output and can break JSON formatting
- **`$util.escapeJavaScript()`** escapes strings that might contain quotes or special characters to prevent JSON injection
- `#foreach` provides `$foreach.count` (1-based index), `$foreach.index` (0-based), and `$foreach.hasNext` (boolean for comma handling)

## Reference

- [API Gateway Mapping Template Reference](https://docs.aws.amazon.com/apigateway/latest/developerguide/api-gateway-mapping-template-reference.html)
- [API Gateway $input Variable](https://docs.aws.amazon.com/apigateway/latest/developerguide/api-gateway-mapping-template-reference.html#input-variable-reference)
- [API Gateway $context Variable](https://docs.aws.amazon.com/apigateway/latest/developerguide/api-gateway-mapping-template-reference.html#context-variable-reference)
- [Terraform aws_api_gateway_integration](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_integration)

## Additional Resources

- [VTL Reference for API Gateway](https://docs.aws.amazon.com/apigateway/latest/developerguide/api-gateway-mapping-template-reference.html) -- complete $input, $context, $stageVariables, and $util reference
- [API Gateway Models and Mapping Templates](https://docs.aws.amazon.com/apigateway/latest/developerguide/models-mappings.html) -- how models and templates work together
- [Apache Velocity Engine User Guide](https://velocity.apache.org/engine/2.3/user-guide.html) -- the underlying template language specification
- [API Gateway Request/Response Data Mapping](https://docs.aws.amazon.com/apigateway/latest/developerguide/request-response-data-mappings.html) -- how data flows through the integration pipeline

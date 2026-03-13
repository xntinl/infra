# 28. Lambda Function URLs vs API Gateway

<!--
difficulty: intermediate
concepts: [lambda-function-url, auth-type-none, auth-type-iam, api-gateway-comparison, function-url-cors, invoke-mode]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: differentiate, implement, design
prerequisites: [exercise-02]
aws_cost: ~$0.01/hr
-->

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| Exercise 02 completed | Understanding of API Gateway REST vs HTTP APIs |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Differentiate** between Lambda Function URLs and API Gateway based on features, cost, and use cases
2. **Implement** a Lambda Function URL with both `NONE` (public) and `AWS_IAM` authentication types using Terraform
3. **Design** a decision framework for choosing between Function URLs (simple, free, no caching) and API Gateway (features, throttling, caching, custom domains)
4. **Configure** CORS settings on a Function URL and verify cross-origin request handling
5. **Justify** why Function URLs do not replace API Gateway for production APIs that require throttling, API keys, request validation, or caching

## Why This Matters

Lambda Function URLs (launched April 2022) provide a dedicated HTTPS endpoint for a Lambda function without API Gateway. They are free (you only pay for Lambda invocations), support IAM authentication, and require zero additional infrastructure. This makes them ideal for webhooks, internal service-to-service calls, and simple single-function APIs.

However, Function URLs lack every feature that makes API Gateway production-ready: throttling, caching, API keys, usage plans, request/response transformation, request validation, WAF integration, and custom domain names. The DVA-C02 exam presents scenarios where you must choose between them. The decision usually comes down to: "Does the API need any gateway-level feature?" If yes, use API Gateway. If it is a simple, single-function endpoint with no gateway-level requirements, a Function URL is simpler and cheaper.

The exam also tests the two authentication modes: `NONE` (public, any caller can invoke) and `AWS_IAM` (requires Signature Version 4 signed requests). There is no Cognito, API key, or Lambda authorizer option with Function URLs -- if you need those, you need API Gateway.

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
	"os"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

func handler(ctx context.Context, request events.LambdaFunctionURLRequest) (events.LambdaFunctionURLResponse, error) {
	response := map[string]interface{}{
		"message":         "Hello from Lambda Function URL",
		"function_name":   os.Getenv("AWS_LAMBDA_FUNCTION_NAME"),
		"request_method":  request.RequestContext.HTTP.Method,
		"request_path":    request.RawPath,
		"source_ip":       request.RequestContext.HTTP.SourceIP,
		"timestamp":       time.Now().Format(time.RFC3339),
		"query_params":    request.QueryStringParameters,
		"invoke_mode":     "BUFFERED",
	}

	if request.Body != "" {
		response["request_body"] = request.Body
	}

	body, _ := json.MarshalIndent(response, "", "  ")

	return events.LambdaFunctionURLResponse{
		StatusCode: 200,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: string(body),
	}, nil
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
  default     = "function-url-demo"
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

resource "aws_iam_role_policy_attachment" "basic" {
  role       = aws_iam_role.this.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}
```

### `lambda.tf`

```hcl
# -------------------------------------------------------
# Lambda Functions
# -------------------------------------------------------
resource "aws_cloudwatch_log_group" "public_fn" {
  name              = "/aws/lambda/function-url-public"
  retention_in_days = 1
}

resource "aws_lambda_function" "public_fn" {
  function_name    = "function-url-public"
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.this.arn
  memory_size      = 256
  timeout          = 30
  depends_on       = [aws_cloudwatch_log_group.public_fn]
}

resource "aws_cloudwatch_log_group" "iam_fn" {
  name              = "/aws/lambda/function-url-iam"
  retention_in_days = 1
}

resource "aws_lambda_function" "iam_fn" {
  function_name    = "function-url-iam"
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.this.arn
  memory_size      = 256
  timeout          = 30
  depends_on       = [aws_cloudwatch_log_group.iam_fn]
}

# =======================================================
# TODO 1 -- Public Function URL (auth_type = NONE)
# =======================================================
# Requirements:
#   - Create an aws_lambda_function_url for the public function
#   - Set authorization_type = "NONE"
#   - Configure CORS to allow all origins, methods, and headers
#   - This creates a publicly accessible HTTPS endpoint
#   - Anyone with the URL can invoke the function
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_function_url
# Note: You also need an aws_lambda_permission with
#        principal = "*" and function_url_auth_type = "NONE"


# =======================================================
# TODO 2 -- IAM-Authenticated Function URL (auth_type = AWS_IAM)
# =======================================================
# Requirements:
#   - Create an aws_lambda_function_url for the IAM function
#   - Set authorization_type = "AWS_IAM"
#   - Callers must sign requests with SigV4
#   - No CORS needed (service-to-service calls)
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_function_url


# =======================================================
# TODO 3 -- Resource-Based Policy for Public URL
# =======================================================
# Requirements:
#   - Create an aws_lambda_permission that allows
#     public invocation via the Function URL
#   - Set action = "lambda:InvokeFunctionUrl"
#   - Set principal = "*"
#   - Set function_url_auth_type = "NONE"
#   - Without this permission, even NONE auth type
#     returns 403 Forbidden
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_permission
```

### `outputs.tf`

```hcl
output "public_url"      { value = aws_lambda_function_url.public.function_url }
output "iam_url"         { value = aws_lambda_function_url.iam.function_url }
output "public_function" { value = aws_lambda_function.public_fn.function_name }
output "iam_function"    { value = aws_lambda_function.iam_fn.function_name }
```

### Decision Table: Function URL vs API Gateway

| Feature | Lambda Function URL | API Gateway (HTTP API) | API Gateway (REST API) |
|---------|--------------------|-----------------------|----------------------|
| **Cost** | Free (Lambda only) | $1.00/million requests | $3.50/million requests |
| **Custom domain** | No | Yes | Yes |
| **Throttling** | No | Yes (route-level) | Yes (method-level) |
| **API keys** | No | No | Yes |
| **Usage plans** | No | No | Yes |
| **Caching** | No | No | Yes |
| **Request validation** | No | Parameter validation | Model + parameter validation |
| **WAF integration** | No | No | Yes |
| **Authorization** | NONE, IAM | IAM, JWT, Lambda | IAM, Cognito, Lambda, API Key |
| **WebSocket** | No | No | No (separate WebSocket API) |
| **Response streaming** | Yes (RESPONSE_STREAM mode) | No | No |
| **Max payload** | 6 MB | 10 MB | 10 MB |
| **Max timeout** | 15 min | 30s (HTTP), 29s (REST) | 29s |

## Spot the Bug

A developer creates a Function URL with `NONE` auth type but forgets the resource-based policy:

```hcl
resource "aws_lambda_function_url" "public" {
  function_name      = aws_lambda_function.this.function_name
  authorization_type = "NONE"
}

# No aws_lambda_permission resource!
```

They test with `curl` and get `403 Forbidden`.

<details>
<summary>Explain the bug</summary>

Even with `authorization_type = "NONE"`, the Lambda function must have a resource-based policy that grants `lambda:InvokeFunctionUrl` permission to `*` (all principals). Without this policy, the function rejects all Function URL invocations with a 403 error.

This is different from API Gateway, where creating the integration automatically grants invoke permission. Function URLs require an explicit permission.

The fix -- add the resource-based policy:

```hcl
resource "aws_lambda_permission" "function_url_public" {
  statement_id           = "AllowPublicFunctionURL"
  action                 = "lambda:InvokeFunctionUrl"
  function_name          = aws_lambda_function.this.function_name
  principal              = "*"
  function_url_auth_type = "NONE"
}
```

Note: `function_url_auth_type` must match the Function URL's `authorization_type`. If they do not match, the permission is ignored.

</details>

## Verify What You Learned

### Step 1 -- Apply the infrastructure

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init && terraform apply -auto-approve
```

### Step 2 -- Test the public Function URL

```bash
PUBLIC_URL=$(terraform output -raw public_url)
curl -s "$PUBLIC_URL" | jq .
```

Expected: JSON response with function details, no authentication required.

### Step 3 -- Test the public URL with query parameters

```bash
curl -s "${PUBLIC_URL}?name=test&env=dev" | jq '.query_params'
```

Expected: `{"name": "test", "env": "dev"}`

### Step 4 -- Test the IAM-authenticated Function URL

```bash
IAM_URL=$(terraform output -raw iam_url)

# Unsigned request should fail
curl -s -o /dev/null -w "%{http_code}" "$IAM_URL"
```

Expected: `403` (unsigned request rejected)

```bash
# Signed request using AWS CLI
aws lambda invoke-with-response-stream \
  --function-name function-url-iam \
  --qualifier '$LATEST' \
  /dev/stdout 2>/dev/null | jq .
```

Or use `awscurl` for signed HTTP requests to the Function URL.

### Step 5 -- Verify CORS headers on the public URL

```bash
curl -s -I -X OPTIONS "$PUBLIC_URL" \
  -H "Origin: https://example.com" \
  -H "Access-Control-Request-Method: GET" \
  | grep -i "access-control"
```

Expected: CORS headers including `Access-Control-Allow-Origin`, `Access-Control-Allow-Methods`.

### Step 6 -- Verify Function URL configuration

```bash
aws lambda get-function-url-config --function-name function-url-public \
  --query "{AuthType:AuthType,Cors:Cors,URL:FunctionUrl}" --output json

aws lambda get-function-url-config --function-name function-url-iam \
  --query "{AuthType:AuthType,URL:FunctionUrl}" --output json
```

Expected: public URL with `NONE` auth and CORS config; IAM URL with `AWS_IAM` auth.

## Solutions

<details>
<summary>TODO 1 -- Public Function URL (auth_type = NONE) (lambda.tf)</summary>

```hcl
resource "aws_lambda_function_url" "public" {
  function_name      = aws_lambda_function.public_fn.function_name
  authorization_type = "NONE"

  cors {
    allow_origins = ["*"]
    allow_methods = ["GET", "POST", "PUT", "DELETE"]
    allow_headers = ["*"]
    max_age       = 3600
  }
}
```

</details>

<details>
<summary>TODO 2 -- IAM-Authenticated Function URL (auth_type = AWS_IAM) (lambda.tf)</summary>

```hcl
resource "aws_lambda_function_url" "iam" {
  function_name      = aws_lambda_function.iam_fn.function_name
  authorization_type = "AWS_IAM"
}
```

</details>

<details>
<summary>TODO 3 -- Resource-Based Policy for Public URL (lambda.tf)</summary>

```hcl
resource "aws_lambda_permission" "function_url_public" {
  statement_id           = "AllowPublicFunctionURL"
  action                 = "lambda:InvokeFunctionUrl"
  function_name          = aws_lambda_function.public_fn.function_name
  principal              = "*"
  function_url_auth_type = "NONE"
}
```

</details>

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

In **Exercise 29 -- API Gateway WebSocket APIs**, you will build a real-time WebSocket API with $connect, $disconnect, and $default routes, using DynamoDB to manage connection state.

## Summary

You created Lambda Function URLs with both public and IAM-authenticated access:

- **Function URLs** provide a dedicated HTTPS endpoint for a single Lambda function at **no additional cost**
- Two authentication modes: `NONE` (public) and `AWS_IAM` (SigV4 signed requests)
- `NONE` auth still requires a **resource-based policy** granting `lambda:InvokeFunctionUrl` to `*`
- **CORS** is configured directly on the Function URL (not on the function itself)
- Function URLs **do not support**: custom domains, throttling, caching, API keys, request validation, WAF, or WebSocket
- **Response streaming** (`invoke_mode = "RESPONSE_STREAM"`) is unique to Function URLs -- API Gateway does not support it
- Choose Function URLs for **webhooks, internal tools, simple single-function APIs** where gateway features are not needed
- Choose API Gateway for **production APIs** requiring throttling, caching, custom domains, or complex authorization

Key exam pattern: "simplest way to expose a Lambda function over HTTPS without additional infrastructure" = Function URL. "API needs rate limiting and caching" = API Gateway.

## Reference

| Resource | Purpose |
|----------|---------|
| `aws_lambda_function_url` | Creates HTTPS endpoint for Lambda |
| `aws_lambda_permission` | Resource-based policy for public access |
| `authorization_type` | NONE (public) or AWS_IAM (signed) |
| `cors` block | Cross-origin request configuration |

## Additional Resources

- [Lambda Function URLs](https://docs.aws.amazon.com/lambda/latest/dg/lambda-urls.html)
- [Function URL Security](https://docs.aws.amazon.com/lambda/latest/dg/urls-auth.html)
- [Function URL CORS](https://docs.aws.amazon.com/lambda/latest/dg/urls-configuration.html#urls-cors)
- [Response Streaming with Function URLs](https://docs.aws.amazon.com/lambda/latest/dg/configuration-response-streaming.html)
- [Terraform aws_lambda_function_url](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_function_url)

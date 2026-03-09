# 78. API Gateway: REST vs HTTP vs WebSocket

<!--
difficulty: intermediate
concepts: [api-gateway-rest, api-gateway-http, api-gateway-websocket, request-validation, response-caching, usage-plans, jwt-authorizer, iam-authorizer, lambda-authorizer, websocket-routes, connection-management, api-gateway-throttling]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: apply, analyze
prerequisites: [77-lambda-event-sources-patterns]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** API Gateway free tier: 1M REST API calls + 1M HTTP API calls + 1M WebSocket messages per month (12 months). Lambda free tier covers invocations. Total ~$0.01/hr. Remember to run `terraform destroy` when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Completed exercise 77 (Lambda event sources) | Lambda function deployment |
| Go 1.21+ installed | `go version` |
| Lambda function.zip from exercise 77 | `ls function.zip` |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Implement** REST API, HTTP API, and WebSocket API using Terraform.
2. **Analyze** the feature and cost differences between REST and HTTP APIs.
3. **Apply** the correct API type for a given use case (full features vs cost optimization vs real-time).
4. **Evaluate** authorization strategies: IAM, Lambda authorizer, Cognito, JWT.
5. **Design** API architectures that match specific requirements for validation, caching, and throttling.

---

## Why This Matters

API Gateway type selection is a frequent SAA-C03 exam topic because it tests whether you understand the trade-offs between feature richness and cost. The exam presents scenarios and asks "which API Gateway type should you use?" The answer depends on which features the scenario requires.

**REST API** is the full-featured option: request/response validation, API keys with usage plans, response caching, WAF integration, request transformation, and all authorizer types (IAM, Lambda, Cognito). It costs $3.50 per million requests. Use REST API when you need features that HTTP API does not support.

**HTTP API** is simpler and cheaper: $1.00 per million requests (71% cheaper), native JWT authorizer, simpler CORS, but no request validation, no caching, no usage plans, no WAF integration, and no request/response transformation. The exam tests whether you pick HTTP API when the scenario needs "a simple, cost-effective API proxy to Lambda."

**WebSocket API** provides bidirectional communication: the client maintains a persistent connection, and the server can push messages without the client requesting them. Use cases: chat applications, real-time dashboards, gaming, financial tickers. The exam rarely tests WebSocket deeply but expects you to know when it is the right choice (real-time, bidirectional).

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

variable "project_name" {
  description = "Project name for resource naming and tagging"
  type        = string
  default     = "saa-ex78"
}
```

### `iam.tf`

```hcl
resource "aws_iam_role" "lambda" {
  name = "${var.project_name}-lambda-role"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action    = "sts:AssumeRole"
      Effect    = "Allow"
      Principal = { Service = "lambda.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "lambda_basic" {
  role       = aws_iam_role.lambda.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}
```

### `lambda.tf`

```hcl
resource "aws_lambda_function" "this" {
  function_name    = "${var.project_name}-handler"
  role             = aws_iam_role.lambda.arn
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  filename         = "function.zip"
  source_code_hash = filebase64sha256("function.zip")
  timeout          = 30
  memory_size      = 128
}
```

### `api.tf`

```hcl
# ============================================================
# TODO 1: REST API (Full Featured)
# ============================================================
# Create a REST API with request validation and stage config.
#
# Requirements:
#   - Resource: aws_api_gateway_rest_api
#   - name = "${var.project_name}-rest-api"
#   - Create a resource at path /items
#   - Create a GET method on /items
#   - Create a Lambda proxy integration (AWS_PROXY)
#   - Create a deployment and stage named "v1"
#   - Add Lambda permission for the REST API
#
# REST API features not in HTTP API:
#   - Request validation (models, request validators)
#   - Response caching (per-stage, per-method)
#   - Usage plans and API keys
#   - WAF integration
#   - Request/response transformation (mapping templates)
#   - Canary deployments
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_rest_api
# ============================================================


# ============================================================
# TODO 2: HTTP API (Simple and Cheap)
# ============================================================
# Create an HTTP API with JWT authorizer support.
#
# Requirements:
#   - Resource: aws_apigatewayv2_api
#   - name = "${var.project_name}-http-api"
#   - protocol_type = "HTTP"
#   - Create integration with Lambda (AWS_PROXY, version 2.0)
#   - Create route: GET /items
#   - Create $default stage with auto_deploy = true
#   - Add Lambda permission
#
# HTTP API advantages over REST API:
#   - 71% cheaper ($1.00 vs $3.50 per million)
#   - Lower latency (~10ms less)
#   - Native OIDC/JWT authorizer (no Lambda authorizer needed)
#   - Automatic deployments
#   - Simpler configuration
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/apigatewayv2_api
# ============================================================


# ============================================================
# TODO 3: WebSocket API
# ============================================================
# Create a WebSocket API for bidirectional communication.
#
# Requirements:
#   - Resource: aws_apigatewayv2_api
#   - name = "${var.project_name}-ws-api"
#   - protocol_type = "WEBSOCKET"
#   - route_selection_expression = "$request.body.action"
#   - Create integration with Lambda
#   - Create routes: $connect, $disconnect, $default
#   - Create stage named "v1" with auto_deploy = true
#   - Add Lambda permission
#
# WebSocket concepts:
#   - $connect: fired when client connects (auth happens here)
#   - $disconnect: fired when client disconnects
#   - $default: catch-all for unmatched routes
#   - route_selection_expression: which field in the message
#     determines the route (e.g., {"action":"chat"} routes
#     to the "chat" route)
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/apigatewayv2_api
# ============================================================
```

### `outputs.tf`

```hcl
output "rest_api_url" {
  value = "Set after TODO 1 implementation"
}

output "http_api_url" {
  value = "Set after TODO 2 implementation"
}

output "ws_api_url" {
  value = "Set after TODO 3 implementation"
}
```

---

## API Gateway Decision Table

| Feature | REST API | HTTP API | WebSocket API |
|---|---|---|---|
| **Price per million** | $3.50 | $1.00 | $1.00 (messages) + $0.25 (connection min) |
| **Request validation** | Yes | No | No |
| **Response caching** | Yes ($0.020-$3.800/hr) | No | N/A |
| **Usage plans / API keys** | Yes | No | No |
| **WAF integration** | Yes | No | No |
| **Request transformation** | Yes (VTL mapping) | No | No |
| **IAM authorizer** | Yes | Yes | Yes |
| **Lambda authorizer** | Yes (v1 + v2) | Yes (v2 only) | Yes |
| **Cognito authorizer** | Yes (native) | Via JWT | Via Lambda |
| **JWT authorizer** | Via Lambda | Yes (native) | No |
| **Canary deployment** | Yes | No | No |
| **Mutual TLS** | Yes | Yes | No |
| **Private API** | Yes (VPC endpoint) | Yes (VPC link) | No |
| **Latency** | ~20-30ms overhead | ~10-15ms overhead | Persistent connection |
| **Protocol** | HTTP/HTTPS | HTTP/HTTPS | WebSocket (wss://) |

### When to Use Each

| Use Case | Best Choice | Why |
|---|---|---|
| Simple Lambda proxy, cost-sensitive | **HTTP API** | 71% cheaper, lower latency |
| Need request validation or caching | **REST API** | Only REST supports these |
| Need WAF protection on API | **REST API** | HTTP API does not support WAF |
| Need API keys for third-party access | **REST API** | Usage plans only on REST |
| Real-time chat or notifications | **WebSocket API** | Bidirectional, persistent |
| OIDC/OAuth2 JWT auth without Lambda | **HTTP API** | Native JWT authorizer |
| API marketplace with rate limiting | **REST API** | Usage plans + throttling |

---

## Spot the Bug

The following configuration creates an HTTP API with IAM authorization, but authenticated requests always return 403 Forbidden. Identify the problem before expanding the answer.

```hcl
resource "aws_apigatewayv2_api" "http" {
  name          = "my-http-api"
  protocol_type = "HTTP"
}

resource "aws_apigatewayv2_route" "secure" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "GET /secure"
  target             = "integrations/${aws_apigatewayv2_integration.lambda.id}"
  authorization_type = "AWS_IAM"

  # REST API-style authorizer reference (wrong for HTTP API)
  authorizer_id = aws_api_gateway_authorizer.iam.id
}

resource "aws_api_gateway_authorizer" "iam" {
  name        = "iam-authorizer"
  rest_api_id = aws_api_gateway_rest_api.legacy.id
  type        = "AWS_IAM"
}
```

<details>
<summary>Explain the bug</summary>

**HTTP API (apigatewayv2) and REST API (api_gateway) are separate services with incompatible resource types.** The code creates an HTTP API route but references a REST API authorizer (`aws_api_gateway_authorizer`). These resources belong to different API Gateway services and cannot cross-reference.

For HTTP API, IAM authorization is set directly on the route without a separate authorizer resource:

```hcl
resource "aws_apigatewayv2_route" "secure" {
  api_id             = aws_apigatewayv2_api.http.id
  route_key          = "GET /secure"
  target             = "integrations/${aws_apigatewayv2_integration.lambda.id}"
  authorization_type = "AWS_IAM"
  # No authorizer_id needed for IAM auth on HTTP API
}
```

For HTTP API, the supported authorization types are:
- `NONE` -- no authorization
- `AWS_IAM` -- IAM-based (set on route, no authorizer resource)
- `JWT` -- JWT/OIDC (requires `aws_apigatewayv2_authorizer` resource)
- `CUSTOM` -- Lambda authorizer v2 (requires `aws_apigatewayv2_authorizer` resource)

The Terraform resource naming convention is the key signal:
- `aws_api_gateway_*` = REST API (v1)
- `aws_apigatewayv2_*` = HTTP API and WebSocket API (v2)

Mixing resources from v1 and v2 is the bug.

</details>

---

## Verify What You Learned

1. **Deploy the infrastructure:**
   ```bash
   terraform init && terraform apply -auto-approve
   ```

2. **Test REST API:**
   ```bash
   REST_URL=$(terraform output -raw rest_api_url)
   curl -s "$REST_URL" | jq .
   ```
   Expected: Response from Lambda function.

3. **Test HTTP API:**
   ```bash
   HTTP_URL=$(terraform output -raw http_api_url)
   curl -s "$HTTP_URL" | jq .
   ```
   Expected: Same response, but from HTTP API endpoint.

4. **Compare latencies:**
   ```bash
   REST_URL=$(terraform output -raw rest_api_url)
   HTTP_URL=$(terraform output -raw http_api_url)

   echo "REST API:"
   curl -s -o /dev/null -w "Total: %{time_total}s\n" "$REST_URL"

   echo "HTTP API:"
   curl -s -o /dev/null -w "Total: %{time_total}s\n" "$HTTP_URL"
   ```
   Expected: HTTP API is slightly faster (~10ms less overhead).

5. **Verify API types:**
   ```bash
   # REST APIs
   aws apigateway get-rest-apis \
     --query 'items[?name==`saa-ex78-rest-api`].{Name:name,Id:id}' \
     --output table

   # HTTP and WebSocket APIs
   aws apigatewayv2 get-apis \
     --query 'Items[?starts_with(Name,`saa-ex78`)].{Name:Name,Protocol:ProtocolType,Endpoint:ApiEndpoint}' \
     --output table
   ```
   Expected: One REST API, one HTTP API, one WEBSOCKET API.

6. **Terraform state consistency:**
   ```bash
   terraform plan
   ```
   Expected: `No changes. Your infrastructure matches the configuration.`

---

## Solutions

<details>
<summary>TODO 1 -- REST API (api.tf)</summary>

```hcl
resource "aws_api_gateway_rest_api" "this" {
  name = "${var.project_name}-rest-api"
}

resource "aws_api_gateway_resource" "items" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  parent_id   = aws_api_gateway_rest_api.this.root_resource_id
  path_part   = "items"
}

resource "aws_api_gateway_method" "get_items" {
  rest_api_id   = aws_api_gateway_rest_api.this.id
  resource_id   = aws_api_gateway_resource.items.id
  http_method   = "GET"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "lambda" {
  rest_api_id             = aws_api_gateway_rest_api.this.id
  resource_id             = aws_api_gateway_resource.items.id
  http_method             = aws_api_gateway_method.get_items.http_method
  type                    = "AWS_PROXY"
  integration_http_method = "POST"
  uri                     = aws_lambda_function.this.invoke_arn
}

resource "aws_api_gateway_deployment" "this" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  depends_on  = [aws_api_gateway_integration.lambda]

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_api_gateway_stage" "v1" {
  rest_api_id   = aws_api_gateway_rest_api.this.id
  deployment_id = aws_api_gateway_deployment.this.id
  stage_name    = "v1"
}

resource "aws_lambda_permission" "rest_api" {
  statement_id  = "AllowRESTAPI"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.this.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.this.execution_arn}/*/*"
}

```

Update `outputs.tf`:

```hcl
output "rest_api_url" {
  value = "${aws_api_gateway_stage.v1.invoke_url}/items"
}
```

REST API requires explicit resource, method, integration, deployment, and stage resources. This verbosity reflects the full feature set (each layer is configurable).

</details>

<details>
<summary>TODO 2 -- HTTP API (api.tf)</summary>

```hcl
resource "aws_apigatewayv2_api" "http" {
  name          = "${var.project_name}-http-api"
  protocol_type = "HTTP"
}

resource "aws_apigatewayv2_integration" "http_lambda" {
  api_id                 = aws_apigatewayv2_api.http.id
  integration_type       = "AWS_PROXY"
  integration_uri        = aws_lambda_function.this.invoke_arn
  payload_format_version = "2.0"
}

resource "aws_apigatewayv2_route" "http_items" {
  api_id    = aws_apigatewayv2_api.http.id
  route_key = "GET /items"
  target    = "integrations/${aws_apigatewayv2_integration.http_lambda.id}"
}

resource "aws_apigatewayv2_stage" "http_default" {
  api_id      = aws_apigatewayv2_api.http.id
  name        = "$default"
  auto_deploy = true
}

resource "aws_lambda_permission" "http_api" {
  statement_id  = "AllowHTTPAPI"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.this.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.http.execution_arn}/*/*"
}

```

Update `outputs.tf`:

```hcl
output "http_api_url" {
  value = "${aws_apigatewayv2_api.http.api_endpoint}/items"
}
```

HTTP API is significantly simpler: no separate resource/method/deployment objects. `auto_deploy = true` eliminates manual deployment management.

</details>

<details>
<summary>TODO 3 -- WebSocket API (api.tf)</summary>

```hcl
resource "aws_apigatewayv2_api" "ws" {
  name                       = "${var.project_name}-ws-api"
  protocol_type              = "WEBSOCKET"
  route_selection_expression = "$request.body.action"
}

resource "aws_apigatewayv2_integration" "ws_lambda" {
  api_id             = aws_apigatewayv2_api.ws.id
  integration_type   = "AWS_PROXY"
  integration_uri    = aws_lambda_function.this.invoke_arn
  integration_method = "POST"
}

resource "aws_apigatewayv2_route" "ws_connect" {
  api_id    = aws_apigatewayv2_api.ws.id
  route_key = "$connect"
  target    = "integrations/${aws_apigatewayv2_integration.ws_lambda.id}"
}

resource "aws_apigatewayv2_route" "ws_disconnect" {
  api_id    = aws_apigatewayv2_api.ws.id
  route_key = "$disconnect"
  target    = "integrations/${aws_apigatewayv2_integration.ws_lambda.id}"
}

resource "aws_apigatewayv2_route" "ws_default" {
  api_id    = aws_apigatewayv2_api.ws.id
  route_key = "$default"
  target    = "integrations/${aws_apigatewayv2_integration.ws_lambda.id}"
}

resource "aws_apigatewayv2_stage" "ws_v1" {
  api_id      = aws_apigatewayv2_api.ws.id
  name        = "v1"
  auto_deploy = true
}

resource "aws_lambda_permission" "ws_api" {
  statement_id  = "AllowWSAPI"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.this.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.ws.execution_arn}/*/*"
}

```

Update `outputs.tf`:

```hcl
output "ws_api_url" {
  value = aws_apigatewayv2_api.ws.api_endpoint
}
```

WebSocket API requires `route_selection_expression` to determine which route handles each message. The `$connect` and `$disconnect` routes are special -- they fire on connection lifecycle events. The `$default` route catches messages that do not match any custom route.

</details>

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify:

```bash
aws apigateway get-rest-apis \
  --query 'items[?name==`saa-ex78-rest-api`]'
aws apigatewayv2 get-apis \
  --query 'Items[?starts_with(Name,`saa-ex78`)]'
```

Expected: Empty results for both queries.

---

## What's Next

Exercise 79 covers **Step Functions workflow orchestration**, where you will define a state machine for an order processing workflow using Choice, Parallel, and Map states. Step Functions provides the orchestration layer that coordinates multiple Lambda functions, unlike the direct event-driven patterns you built in exercises 77-78.

---

## Summary

- **REST API** ($3.50/M): full features -- request validation, caching, usage plans, WAF, VTL transformation, all authorizer types
- **HTTP API** ($1.00/M): simpler and 71% cheaper -- native JWT authorizer, auto-deploy, lower latency, but no validation/caching/WAF
- **WebSocket API** ($1.00/M messages): bidirectional real-time -- persistent connections, server push, route selection by message content
- **REST API** uses `aws_api_gateway_*` Terraform resources; **HTTP/WebSocket** use `aws_apigatewayv2_*` -- these are different services
- **JWT authorizer** is native to HTTP API (no Lambda needed); REST API requires Lambda authorizer for JWT
- **Cognito authorizer** is native to REST API; HTTP API uses JWT authorizer with Cognito issuer URL
- **WAF integration** is only available on REST API -- if you need WAF on an API, you must use REST or put CloudFront in front of HTTP API
- **Usage plans with API keys** only on REST API -- for API monetization or third-party rate limiting
- **Auto-deploy** on HTTP API eliminates the deployment/stage management overhead of REST API
- Choose **HTTP API by default** unless you need a REST API-specific feature

## Reference

- [API Gateway REST vs HTTP](https://docs.aws.amazon.com/apigateway/latest/developerguide/http-api-vs-rest.html)
- [WebSocket API](https://docs.aws.amazon.com/apigateway/latest/developerguide/apigateway-websocket-api.html)
- [Terraform aws_api_gateway_rest_api](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_rest_api)
- [Terraform aws_apigatewayv2_api](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/apigatewayv2_api)

## Additional Resources

- [HTTP API JWT Authorizer](https://docs.aws.amazon.com/apigateway/latest/developerguide/http-api-jwt-authorizer.html) -- native OIDC/OAuth2 integration without Lambda
- [REST API Request Validation](https://docs.aws.amazon.com/apigateway/latest/developerguide/api-gateway-method-request-validation.html) -- schema-based validation before Lambda invocation
- [API Gateway Caching](https://docs.aws.amazon.com/apigateway/latest/developerguide/api-gateway-caching.html) -- per-stage response caching to reduce Lambda invocations
- [WebSocket Connection Management](https://docs.aws.amazon.com/apigateway/latest/developerguide/apigateway-websocket-api-mapping-template-reference.html) -- managing connection IDs for server-side message pushing

# 9. API Gateway Caching, Throttling, and Usage Plans

<!--
difficulty: intermediate
concepts: [api-gateway-cache, stage-cache, throttling-rate-burst, usage-plans, api-keys, quota, cloudwatch-cache-metrics]
tools: [terraform, aws-cli]
estimated_time: 50m
bloom_level: design, implement
prerequisites: [none]
aws_cost: ~$0.05/hr
-->

> **AWS Cost Warning:** API Gateway cache clusters cost ~$0.02/hr for the smallest size (0.5 GB). Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.5 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| curl installed | `curl --version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Design** an API Gateway caching strategy with stage-level and per-method cache settings
2. **Implement** method-level throttling to protect backend services from traffic spikes
3. **Configure** Usage Plans with quotas and API Keys to manage third-party API consumers
4. **Implement** cache key parameters to ensure different query strings return different cached responses
5. **Differentiate** between stage-level throttling, method-level throttling, and Usage Plan throttling

## Why This Matters

API Gateway sits between your clients and your Lambda functions. Without caching, every request invokes Lambda -- which costs money and adds latency. Enabling the API Gateway cache means repeated requests for the same resource are served from an in-memory cache at the edge, reducing Lambda invocations by 80-90% for read-heavy APIs. The cache is configured per stage with a TTL (time-to-live), and you can override cache settings per method to exclude writes or set different TTLs for different endpoints.

Throttling and Usage Plans are equally important for the DVA-C02 exam. API Gateway applies throttling at multiple levels: account-level (10,000 requests/second by default), stage-level, method-level, and per-client via Usage Plans. A Usage Plan lets you create tiers of access -- a free tier with 1,000 requests/month and 5 requests/second, a paid tier with 100,000 requests/month and 50 requests/second -- each enforced via API Keys. The exam tests your ability to layer these controls: understanding that the most restrictive limit wins, that API Keys are for identification and throttling (not authentication), and that cache invalidation requires the `execute-api:InvalidateCache` IAM action or the `Cache-Control: max-age=0` header.

## Building Blocks

Create the following project files. Your job is to fill in each `# TODO` block.

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
  default     = "apigw-cache-lab"
}
```

### `iam.tf`

```hcl
data "aws_iam_policy_document" "lambda_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "lambda" {
  name               = "${var.project_name}-lambda-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

resource "aws_iam_role_policy_attachment" "lambda_basic" {
  role       = aws_iam_role.lambda.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}
```

### `lambda.tf`

```hcl
# -------------------------------------------------------
# Lambda Backend
# -------------------------------------------------------
# NOTE: Build the Go binary before applying:
#   GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
#   zip backend.zip bootstrap
data "archive_file" "backend" {
  type        = "zip"
  source_file = "${path.module}/bootstrap"
  output_path = "${path.module}/backend.zip"
}

# main.go -- Go Lambda handler for API Gateway proxy:
#
# package main
#
# import (
# 	"context"
# 	"encoding/json"
# 	"fmt"
# 	"time"
#
# 	"github.com/aws/aws-lambda-go/events"
# 	"github.com/aws/aws-lambda-go/lambda"
# 	"github.com/google/uuid"
# )
#
# func handler(ctx context.Context, event events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
# 	category := "all"
# 	if event.QueryStringParameters != nil {
# 		if c, ok := event.QueryStringParameters["category"]; ok {
# 			category = c
# 		}
# 	}
#
# 	body, _ := json.Marshal(map[string]interface{}{
# 		"request_id": uuid.New().String(),
# 		"timestamp":  float64(time.Now().UnixMilli()) / 1000.0,
# 		"category":   category,
# 		"message":    fmt.Sprintf("Products in category: %s", category),
# 	})
#
# 	return events.APIGatewayProxyResponse{
# 		StatusCode: 200,
# 		Headers: map[string]string{
# 			"Content-Type": "application/json",
# 			"X-Request-Id": uuid.New().String(),
# 		},
# 		Body: string(body),
# 	}, nil
# }
#
# func main() {
# 	lambda.Start(handler)
# }

resource "aws_lambda_function" "backend" {
  function_name    = "${var.project_name}-backend"
  role             = aws_iam_role.lambda.arn
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  timeout          = 10
  filename         = data.archive_file.backend.output_path
  source_code_hash = data.archive_file.backend.output_base64sha256

  tags = {
    Name = "${var.project_name}-backend"
  }
}

resource "aws_lambda_permission" "api_gw" {
  statement_id  = "AllowAPIGatewayInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.backend.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.this.execution_arn}/*/*"
}
```

### `api.tf`

```hcl
# -------------------------------------------------------
# REST API + Resources + Methods
# -------------------------------------------------------
resource "aws_api_gateway_rest_api" "this" {
  name        = "${var.project_name}-api"
  description = "API Gateway caching and throttling lab"

  endpoint_configuration {
    types = ["REGIONAL"]
  }
}

resource "aws_api_gateway_resource" "products" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  parent_id   = aws_api_gateway_rest_api.this.root_resource_id
  path_part   = "products"
}

resource "aws_api_gateway_method" "get_products" {
  rest_api_id      = aws_api_gateway_rest_api.this.id
  resource_id      = aws_api_gateway_resource.products.id
  http_method      = "GET"
  authorization    = "NONE"
  api_key_required = false

  request_parameters = {
    "method.request.querystring.category" = false
  }
}

resource "aws_api_gateway_integration" "get_products" {
  rest_api_id             = aws_api_gateway_rest_api.this.id
  resource_id             = aws_api_gateway_resource.products.id
  http_method             = aws_api_gateway_method.get_products.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.backend.invoke_arn
}

resource "aws_api_gateway_resource" "orders" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  parent_id   = aws_api_gateway_rest_api.this.root_resource_id
  path_part   = "orders"
}

resource "aws_api_gateway_method" "post_orders" {
  rest_api_id   = aws_api_gateway_rest_api.this.id
  resource_id   = aws_api_gateway_resource.orders.id
  http_method   = "POST"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "post_orders" {
  rest_api_id             = aws_api_gateway_rest_api.this.id
  resource_id             = aws_api_gateway_resource.orders.id
  http_method             = aws_api_gateway_method.post_orders.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.backend.invoke_arn
}

# =======================================================
# TODO 1 -- Stage with Cache Cluster Enabled
# =======================================================
# Requirements:
#   - Create an aws_api_gateway_deployment
#   - Create an aws_api_gateway_stage named "dev"
#   - Set cache_cluster_enabled = true
#   - Set cache_cluster_size = "0.5" (smallest, cheapest option)
#   - Use a triggers block to force redeployment on changes
#
# Docs:
#   https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_stage
# Note: cache_cluster_size valid values: "0.5", "1.6", "6.1",
#       "13.5", "28.4", "58.2", "118", "237"


# =======================================================
# TODO 2 -- Per-Method Cache Settings
# =======================================================
# Requirements:
#   - Create an aws_api_gateway_method_settings for
#     the GET /products method
#   - Set caching_enabled = true
#   - Set cache_ttl_in_seconds = 300 (5 minutes)
#   - Set cache_data_encrypted = true
#   - Set require_authorization_for_cache_control = true
#   - Important: set cache key parameters so that different
#     "category" query strings produce different cache entries
#     Use: cache_key_parameters = ["method.request.querystring.category"]
#   - Create a SECOND aws_api_gateway_method_settings for
#     POST /orders with caching_enabled = false
#     (writes should never be cached)
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_method_settings
# Hint: method_path format is "resource_path/HTTP_METHOD" e.g. "products/GET"


# =======================================================
# TODO 3 -- Method-Level Throttling
# =======================================================
# Requirements:
#   - On the GET /products method settings (from TODO 2),
#     also configure throttling:
#     - throttling_rate_limit = 10 (requests per second)
#     - throttling_burst_limit = 5 (concurrent requests)
#   - On the POST /orders method settings, configure:
#     - throttling_rate_limit = 5
#     - throttling_burst_limit = 2
#   - These per-method limits override the stage default
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_method_settings


# =======================================================
# TODO 4 -- Usage Plan
# =======================================================
# Requirements:
#   - Create an aws_api_gateway_usage_plan named
#     "${var.project_name}-free-tier"
#   - Set description to "Free tier: 1000 requests/month"
#   - Add a throttle_settings block:
#     - rate_limit = 10
#     - burst_limit = 5
#   - Add a quota_settings block:
#     - limit = 1000
#     - period = "MONTH"
#   - Add an api_stages block referencing the dev stage
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_usage_plan


# =======================================================
# TODO 5 -- API Key
# =======================================================
# Requirements:
#   - Create an aws_api_gateway_api_key named
#     "${var.project_name}-test-key" with enabled = true
#   - Create an aws_api_gateway_usage_plan_key to associate
#     the API key with the usage plan from TODO 4
#     (key_type = "API_KEY")
#
# Docs:
#   https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_api_key
#   https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_usage_plan_key


# =======================================================
# TODO 6 -- Require API Key on GET /products
# =======================================================
# Requirements:
#   - Update the aws_api_gateway_method "get_products" to
#     set api_key_required = true
#   - This forces clients to send the x-api-key header
#   - Without the header, API Gateway returns 403 Forbidden
#   - The API key is matched to a Usage Plan, which enforces
#     the quota and throttle settings
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_method
# Hint: Modify the existing resource above, changing
#       api_key_required from false to true
```

### `outputs.tf`

```hcl
output "api_url" {
  value = aws_api_gateway_stage.dev.invoke_url
}

output "api_key_value" {
  value     = aws_api_gateway_api_key.test.value
  sensitive = true
}

output "rest_api_id" {
  value = aws_api_gateway_rest_api.this.id
}
```

## Spot the Bug

A developer configured Usage Plans with API Keys and quotas, but clients can access the API without any API key and the quota is never enforced. **What is wrong?**

```hcl
resource "aws_api_gateway_method" "get_products" {
  rest_api_id      = aws_api_gateway_rest_api.this.id
  resource_id      = aws_api_gateway_resource.products.id
  http_method      = "GET"
  authorization    = "NONE"
  api_key_required = false   # <-- BUG
}

resource "aws_api_gateway_usage_plan" "free_tier" {
  name = "free-tier"

  throttle_settings {
    rate_limit  = 10
    burst_limit = 5
  }

  quota_settings {
    limit  = 1000
    period = "MONTH"
  }

  api_stages {
    api_id = aws_api_gateway_rest_api.this.id
    stage  = aws_api_gateway_stage.dev.stage_name
  }
}
```

<details>
<summary>Explain the bug</summary>

The method has `api_key_required = false`. Even though a Usage Plan is configured with quotas and throttle settings, API Gateway only enforces them when the method **requires** an API key. Without `api_key_required = true`, the `x-api-key` header is ignored, the request is never matched to a Usage Plan, and the quota/throttle settings have no effect.

The fix:

```hcl
resource "aws_api_gateway_method" "get_products" {
  rest_api_id      = aws_api_gateway_rest_api.this.id
  resource_id      = aws_api_gateway_resource.products.id
  http_method      = "GET"
  authorization    = "NONE"
  api_key_required = true   # <-- Fixed
}
```

Important DVA-C02 distinction: API Keys are for **identification and throttling**, not for **authentication**. An API key proves which Usage Plan a client belongs to, but it does not authenticate the user. Use Cognito or Lambda authorizers for authentication, and API keys for rate limiting and quota enforcement.

</details>

## Verify What You Learned

### Step 1 -- Apply the infrastructure

```
terraform init && terraform apply -auto-approve
```

Note: The cache cluster takes 3-5 minutes to provision.

### Step 2 -- Get the API key value

```
API_KEY=$(terraform output -raw api_key_value)
API_URL=$(terraform output -raw api_url)
echo "API URL: $API_URL"
echo "API Key: ${API_KEY:0:10}..."
```

### Step 3 -- Verify API key is required

```
# Without API key -- should return 403 Forbidden
curl -s "${API_URL}/products" | jq .
```

Expected:

```json
{
    "message": "Forbidden"
}
```

### Step 4 -- Call with API key and verify caching

```
# First call (cache miss -- hits Lambda)
echo "=== First call ==="
curl -s -H "x-api-key: $API_KEY" "${API_URL}/products?category=electronics" | jq .

sleep 2

# Second call (cache hit -- same request_id and timestamp)
echo "=== Second call (should be cached) ==="
curl -s -H "x-api-key: $API_KEY" "${API_URL}/products?category=electronics" | jq .
```

Expected: Both calls return the **same** `request_id` and `timestamp`, proving the second response came from the cache.

### Step 5 -- Verify different cache keys for different categories

```
# Different category = different cache entry
curl -s -H "x-api-key: $API_KEY" "${API_URL}/products?category=books" | jq .
```

Expected: A **different** `request_id` from the electronics response, because `category` is a cache key parameter.

### Step 6 -- Check CloudWatch cache metrics

Wait 5 minutes for metrics to populate, then:

```
REST_API_ID=$(terraform output -raw rest_api_id)
aws cloudwatch get-metric-statistics \
  --namespace "AWS/ApiGateway" \
  --metric-name "CacheHitCount" \
  --dimensions Name=ApiName,Value=apigw-cache-lab-api Name=Stage,Value=dev \
  --start-time $(date -u -v-30M +"%Y-%m-%dT%H:%M:%S") \
  --end-time $(date -u +"%Y-%m-%dT%H:%M:%S") \
  --period 300 \
  --statistics Sum \
  --query "Datapoints[].{Time:Timestamp,Hits:Sum}" \
  --output table
```

Expected: At least 1 cache hit.

### Step 7 -- Verify Usage Plan and quota

```
aws apigateway get-usage-plans \
  --query "items[?name=='apigw-cache-lab-free-tier'].{Name:name,Rate:throttle.rateLimit,Burst:throttle.burstLimit,Quota:quota.limit,Period:quota.period}" \
  --output table
```

Expected:

```
--------------------------------------------------------------
|                       GetUsagePlans                        |
+--------+------+-------------------------------------------+
| Burst  | Name                | Period | Quota | Rate       |
+--------+---------------------+--------+-------+------------+
| 5      | apigw-cache-lab-... | MONTH  | 1000  | 10.0       |
+--------+---------------------+--------+-------+------------+
```

### Step 8 -- Test throttling (optional, sends many requests)

```
echo "Sending 20 rapid requests to test throttling..."
for i in $(seq 1 20); do
  STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
    -H "x-api-key: $API_KEY" "${API_URL}/products")
  echo "Request $i: HTTP $STATUS"
done
```

Expected: The first several requests return 200, then you should see some 429 (Too Many Requests) responses when the throttle kicks in.

## Solutions

<details>
<summary>TODO 1 -- Stage with Cache Cluster Enabled (api.tf)</summary>

```hcl
resource "aws_api_gateway_deployment" "this" {
  rest_api_id = aws_api_gateway_rest_api.this.id

  triggers = {
    redeployment = sha1(jsonencode([
      aws_api_gateway_resource.products.id,
      aws_api_gateway_resource.orders.id,
      aws_api_gateway_method.get_products.id,
      aws_api_gateway_method.post_orders.id,
      aws_api_gateway_integration.get_products.id,
      aws_api_gateway_integration.post_orders.id,
    ]))
  }

  lifecycle {
    create_before_destroy = true
  }

  depends_on = [
    aws_api_gateway_integration.get_products,
    aws_api_gateway_integration.post_orders,
  ]
}

resource "aws_api_gateway_stage" "dev" {
  deployment_id        = aws_api_gateway_deployment.this.id
  rest_api_id          = aws_api_gateway_rest_api.this.id
  stage_name           = "dev"
  cache_cluster_enabled = true
  cache_cluster_size    = "0.5"

  tags = {
    Name = "${var.project_name}-dev"
  }
}
```

</details>

<details>
<summary>TODO 2 + TODO 3 -- Per-Method Cache Settings and Throttling (api.tf)</summary>

```hcl
resource "aws_api_gateway_method_settings" "get_products" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  stage_name  = aws_api_gateway_stage.dev.stage_name
  method_path = "products/GET"

  settings {
    caching_enabled                            = true
    cache_ttl_in_seconds                       = 300
    cache_data_encrypted                       = true
    require_authorization_for_cache_control     = true
    cache_key_parameters                       = ["method.request.querystring.category"]

    throttling_rate_limit  = 10
    throttling_burst_limit = 5
  }
}

resource "aws_api_gateway_method_settings" "post_orders" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  stage_name  = aws_api_gateway_stage.dev.stage_name
  method_path = "orders/POST"

  settings {
    caching_enabled = false

    throttling_rate_limit  = 5
    throttling_burst_limit = 2
  }
}
```

</details>

<details>
<summary>TODO 4 -- Usage Plan (api.tf)</summary>

```hcl
resource "aws_api_gateway_usage_plan" "free_tier" {
  name        = "${var.project_name}-free-tier"
  description = "Free tier: 1000 requests/month"

  throttle_settings {
    rate_limit  = 10
    burst_limit = 5
  }

  quota_settings {
    limit  = 1000
    period = "MONTH"
  }

  api_stages {
    api_id = aws_api_gateway_rest_api.this.id
    stage  = aws_api_gateway_stage.dev.stage_name
  }

  depends_on = [aws_api_gateway_stage.dev]
}
```

</details>

<details>
<summary>TODO 5 -- API Key (api.tf)</summary>

```hcl
resource "aws_api_gateway_api_key" "test" {
  name    = "${var.project_name}-test-key"
  enabled = true
}

resource "aws_api_gateway_usage_plan_key" "test" {
  key_id        = aws_api_gateway_api_key.test.id
  key_type      = "API_KEY"
  usage_plan_id = aws_api_gateway_usage_plan.free_tier.id
}
```

</details>

<details>
<summary>TODO 6 -- Require API Key on GET /products (api.tf)</summary>

Update the existing `aws_api_gateway_method "get_products"` resource:

```hcl
resource "aws_api_gateway_method" "get_products" {
  rest_api_id      = aws_api_gateway_rest_api.this.id
  resource_id      = aws_api_gateway_resource.products.id
  http_method      = "GET"
  authorization    = "NONE"
  api_key_required = true   # Changed from false to true

  request_parameters = {
    "method.request.querystring.category" = false
  }
}
```

</details>

## Cleanup

Destroy all resources immediately after finishing to stop cache cluster charges:

```
terraform destroy -auto-approve
```

Verify the API is deleted:

```
aws apigateway get-rest-apis \
  --query "items[?name=='apigw-cache-lab-api'].id" \
  --output text
```

This should return empty output.

## What's Next

You have completed the intermediate exercises for the DVA-C02 Developer Associate certification preparation. Review the concepts across all exercises: Cognito authorization (Exercise 04), Lambda error handling (Exercise 05), SAM development (Exercise 06), runtime configuration (Exercise 07), SQS concurrency (Exercise 08), and API Gateway caching (Exercise 09). These patterns appear repeatedly on the exam in different combinations.

## Summary

You built an API Gateway with production-grade traffic management:

- **Stage-level cache** -- 0.5 GB cache cluster with encrypted storage
- **Per-method cache** -- GET /products cached with 300s TTL, POST /orders excluded from cache
- **Cache key parameters** -- different `category` values produce separate cache entries
- **Method-level throttling** -- rate and burst limits per HTTP method
- **Usage Plans** -- monthly quota of 1,000 requests with throttle enforcement
- **API Keys** -- client identification for quota tracking (not authentication)

Key exam concepts: Cache is per-stage, configured per-method. API keys enable Usage Plan enforcement but do not provide authentication. The most restrictive throttle limit wins (account > stage > method > Usage Plan). Cache invalidation requires the `Cache-Control: max-age=0` header and the `execute-api:InvalidateCache` permission.

## Reference

| Resource | Purpose |
|----------|---------|
| `aws_api_gateway_stage` | Stage with cache cluster |
| `aws_api_gateway_method_settings` | Per-method cache and throttle config |
| `aws_api_gateway_usage_plan` | Quota and throttle for API consumers |
| `aws_api_gateway_api_key` | Client identification key |
| `aws_api_gateway_usage_plan_key` | Associates key with usage plan |
| `cache_key_parameters` | Query string params that vary cache entries |

## Additional Resources

- [API Gateway Caching](https://docs.aws.amazon.com/apigateway/latest/developerguide/api-gateway-caching.html)
- [API Gateway Throttling](https://docs.aws.amazon.com/apigateway/latest/developerguide/api-gateway-request-throttling.html)
- [Usage Plans and API Keys](https://docs.aws.amazon.com/apigateway/latest/developerguide/api-gateway-api-usage-plans.html)
- [Cache Invalidation](https://docs.aws.amazon.com/apigateway/latest/developerguide/api-gateway-caching.html#invalidate-method-caching)
- [CloudWatch Metrics for API Gateway](https://docs.aws.amazon.com/apigateway/latest/developerguide/api-gateway-metrics-and-dimensions.html)
- [DVA-C02 Exam Guide](https://aws.amazon.com/certification/certified-developer-associate/)

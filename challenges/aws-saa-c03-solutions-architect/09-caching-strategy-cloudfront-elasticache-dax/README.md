# Caching Strategy: CloudFront vs ElastiCache vs DAX

<!--
difficulty: intermediate
concepts: [cloudfront-cache, cloudfront-ttl, cache-keys, elasticache-redis, dax, write-through, lazy-loading, cache-invalidation]
tools: [terraform, aws-cli]
estimated_time: 55m
bloom_level: design, justify, implement
prerequisites: [none]
aws_cost: ~$0.15/hr
-->

> **AWS Cost Warning:** ElastiCache cache.t3.micro (~$0.017/hr) + DAX dax.t3.small (~$0.04/hr) + CloudFront (minimal) + DynamoDB on-demand + Lambda + API Gateway. Total ~$0.15/hr. Destroy resources promptly after completing the exercise.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Default VPC with at least 2 AZs | `aws ec2 describe-subnets --filters Name=vpc-id,Values=$(aws ec2 describe-vpcs --filters Name=isDefault,Values=true --query 'Vpcs[0].VpcId' --output text) --query 'Subnets[*].AvailabilityZone'` |
| curl installed for latency testing | `curl --version` |
| Go runtime knowledge (Lambda) | N/A |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Design** a multi-layer caching architecture that places the right cache at each tier of the application stack.
2. **Justify** the selection of CloudFront vs ElastiCache vs DAX for different data access patterns.
3. **Implement** lazy-loading and write-through cache patterns with ElastiCache Redis.
4. **Compare** latency characteristics across uncached, Redis-cached, DAX-cached, and CloudFront-cached responses.
5. **Evaluate** cache invalidation strategies and their trade-offs between freshness and performance.

---

## Why This Matters

Caching is arguably the most impactful performance optimization in cloud architecture, and the SAA-C03 tests it from multiple angles. The exam does not just ask "what is caching?" -- it presents scenarios with specific latency requirements, data freshness constraints, and geographic distribution needs, then asks you to select the correct caching layer. A common exam pattern is: "An application reads from DynamoDB with single-digit millisecond latency but needs microsecond latency for a hot key set." The answer is DAX, not ElastiCache, because DAX is a write-through cache that sits transparently in front of DynamoDB. But if the question adds "the application also caches results of complex queries across multiple tables," the answer shifts to ElastiCache because DAX only caches individual DynamoDB operations.

The three caching layers in this exercise -- CloudFront, ElastiCache, and DAX -- are not competing alternatives. They operate at different levels of the architecture and solve different problems. CloudFront caches at the edge, reducing latency for geographically distributed users and offloading traffic from your origin. ElastiCache caches at the application level, storing computed results, session data, or aggregated query results. DAX caches at the database level, transparently accelerating DynamoDB reads without changing application code. Understanding where each layer fits, and being able to articulate why you would use one vs another (or all three simultaneously), is what the exam rewards.

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
  default     = "saa-ex09"
}
```

### `main.tf`

```hcl
# ---------- Data Sources ----------

data "aws_vpc" "default" {
  default = true
}

data "aws_subnets" "default" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.default.id]
  }
  filter {
    name   = "default-for-az"
    values = ["true"]
  }
}
```

### `database.tf`

```hcl
# ---------- DynamoDB Table ----------

resource "aws_dynamodb_table" "products" {
  name         = "${var.project_name}-products"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "product_id"

  attribute {
    name = "product_id"
    type = "S"
  }

  tags = {
    Name = "${var.project_name}-products"
  }
}

resource "aws_dynamodb_table_item" "sample_products" {
  for_each = {
    "PROD-001" = { name = "Widget Alpha", price = "29.99", category = "electronics" }
    "PROD-002" = { name = "Widget Beta", price = "49.99", category = "electronics" }
    "PROD-003" = { name = "Gadget Gamma", price = "99.99", category = "gadgets" }
    "PROD-004" = { name = "Tool Delta", price = "14.99", category = "tools" }
    "PROD-005" = { name = "Accessory Epsilon", price = "9.99", category = "accessories" }
  }

  table_name = aws_dynamodb_table.products.name
  hash_key   = aws_dynamodb_table.products.hash_key

  item = jsonencode({
    product_id = { S = each.key }
    name       = { S = each.value.name }
    price      = { N = each.value.price }
    category   = { S = each.value.category }
  })
}

# ============================================================
# TODO 2: ElastiCache Redis Cluster  [database.tf]
# ============================================================
# Deploy a single-node ElastiCache Redis cluster for
# application-level caching.
#
# Requirements:
#   - Resource: aws_elasticache_subnet_group
#     - name = "${var.project_name}-cache-subnet"
#     - subnet_ids = default subnets
#
#   - Resource: aws_elasticache_cluster
#     - cluster_id = "${var.project_name}-redis"
#     - engine = "redis"
#     - engine_version = "7.1"
#     - node_type = "cache.t3.micro"
#     - num_cache_nodes = 1
#     - port = 6379
#     - subnet_group_name = subnet group above
#     - security_group_ids = [cache security group]
#
#   - Update the Lambda environment variable REDIS_HOST with
#     the cache cluster's cache_nodes[0].address
#
#   - Add Lambda to VPC (vpc_config block) so it can reach Redis
#     - subnet_ids = default subnets
#     - security_group_ids = [cache security group]
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/elasticache_cluster
# ============================================================


# ============================================================
# TODO 4: DAX Cluster  [database.tf]
# ============================================================
# Deploy a DAX cluster for transparent DynamoDB caching with
# microsecond read latency.
#
# Requirements:
#   - Resource: aws_iam_role for DAX service
#     - Trust policy: Principal = dax.amazonaws.com
#     - Permission: dynamodb:* on the products table
#
#   - Resource: aws_dax_subnet_group
#     - name = "${var.project_name}-dax-subnet"
#     - subnet_ids = default subnets
#
#   - Resource: aws_dax_cluster
#     - cluster_name = "${var.project_name}-dax"
#     - node_type = "dax.t3.small"
#     - replication_factor = 1 (single node for lab)
#     - iam_role_arn = DAX role ARN
#     - subnet_group_name = DAX subnet group
#     - security_group_ids = [cache security group]
#
#   - Update Lambda environment variable DAX_ENDPOINT with
#     the DAX cluster's cluster_address
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/dax_cluster
# ============================================================
```

### `iam.tf`

```hcl
# ---------- Lambda IAM ----------

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

resource "aws_iam_role_policy" "lambda" {
  name = "lambda-permissions"
  role = aws_iam_role.lambda.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "dynamodb:GetItem",
          "dynamodb:Query",
          "dynamodb:Scan"
        ]
        Resource = aws_dynamodb_table.products.arn
      },
      {
        Effect   = "Allow"
        Action   = "logs:*"
        Resource = "arn:aws:logs:*:*:*"
      },
      {
        Effect = "Allow"
        Action = [
          "ec2:CreateNetworkInterface",
          "ec2:DescribeNetworkInterfaces",
          "ec2:DeleteNetworkInterface"
        ]
        Resource = "*"
      },
      {
        Effect = "Allow"
        Action = [
          "dax:GetItem",
          "dax:Query",
          "dax:Scan"
        ]
        Resource = "*"
      }
    ]
  })
}
```

### `lambda.tf`

```hcl
# ---------- Lambda Function ----------

data "archive_file" "api" {
  type        = "zip"
  output_path = "${path.module}/api.zip"

  source {
    content = <<-GO
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

var dynamoClient *dynamodb.Client
var tableName string

func init() {
	tableName = os.Getenv("TABLE_NAME")
	cfg, _ := config.LoadDefaultConfig(context.Background())
	dynamoClient = dynamodb.NewFromConfig(cfg)
}

// Redis client initialized lazily
// redisClient *redis.Client = nil (see TODO 3)

func handler(ctx context.Context, event events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	start := time.Now()
	productID := "PROD-001"
	if id, ok := event.QueryStringParameters["id"]; ok {
		productID = id
	}
	cacheSource := "none"

	// ===== CACHE LOOKUP LOCATION =====
	// TODO 3 will add Redis lazy-loading cache check here
	// TODO 5 will add DAX client usage here

	// Direct DynamoDB read (no cache)
	result, err := dynamoClient.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &tableName,
		Key: map[string]types.AttributeValue{
			"product_id": &types.AttributeValueMemberS{Value: productID},
		},
	})

	item := make(map[string]interface{})
	if err == nil && result.Item != nil {
		for k, v := range result.Item {
			switch val := v.(type) {
			case *types.AttributeValueMemberS:
				item[k] = val.Value
			case *types.AttributeValueMemberN:
				item[k] = val.Value
			}
		}
	}

	elapsed := float64(time.Since(start).Microseconds()) / 1000.0 // ms

	body, _ := json.Marshal(map[string]interface{}{
		"product":      item,
		"latency_ms":   elapsed,
		"cache_source": cacheSource,
	})

	return events.APIGatewayProxyResponse{
		StatusCode: 200,
		Headers: map[string]string{
			"Content-Type":       "application/json",
			"X-Cache-Source":     cacheSource,
			"X-Response-Time-Ms": fmt.Sprintf("%.2f", elapsed),
		},
		Body: string(body),
	}, nil
}

func main() {
	lambda.Start(handler)
}
    GO
    filename = "main.go"
  }
}

resource "aws_lambda_function" "api" {
  function_name    = "${var.project_name}-api"
  filename         = data.archive_file.api.output_path
  source_code_hash = data.archive_file.api.output_base64sha256
  role             = aws_iam_role.lambda.arn
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  timeout          = 15
  memory_size      = 256

  environment {
    variables = {
      TABLE_NAME = aws_dynamodb_table.products.name
      REDIS_HOST = ""  # Updated in TODO 2
      DAX_ENDPOINT = ""  # Updated in TODO 4
    }
  }
}

# ---------- API Gateway ----------

resource "aws_apigatewayv2_api" "this" {
  name          = "${var.project_name}-api"
  protocol_type = "HTTP"
}

resource "aws_apigatewayv2_integration" "lambda" {
  api_id                 = aws_apigatewayv2_api.this.id
  integration_type       = "AWS_PROXY"
  integration_uri        = aws_lambda_function.api.invoke_arn
  payload_format_version = "2.0"
}

resource "aws_apigatewayv2_route" "get_product" {
  api_id    = aws_apigatewayv2_api.this.id
  route_key = "GET /product"
  target    = "integrations/${aws_apigatewayv2_integration.lambda.id}"
}

resource "aws_apigatewayv2_stage" "default" {
  api_id      = aws_apigatewayv2_api.this.id
  name        = "$default"
  auto_deploy = true
}

resource "aws_lambda_permission" "apigw" {
  statement_id  = "AllowAPIGateway"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.api.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.this.execution_arn}/*/*"
}

# ============================================================
# TODO 3: Lambda Code for Lazy-Loading Cache Pattern  [lambda.tf]
# ============================================================
# Modify the Lambda function code above to implement the
# lazy-loading (cache-aside) pattern with Redis.
#
# The pattern:
#   1. Check Redis for cached product data
#   2. If cache HIT: return cached data, set cache_source = "redis"
#   3. If cache MISS: read from DynamoDB, write to Redis with
#      TTL of 300 seconds, set cache_source = "dynamodb"
#
# Requirements:
#   Add go-redis client initialization and cache logic:
#
#   import "github.com/redis/go-redis/v9"
#
#   var redisClient *redis.Client
#
#   func initRedis() {
#       redisHost := os.Getenv("REDIS_HOST")
#       if redisHost != "" {
#           redisClient = redis.NewClient(&redis.Options{
#               Addr: redisHost + ":6379",
#           })
#       }
#   }
#
#   In the handler, before the DynamoDB read:
#
#   if redisClient != nil {
#       val, err := redisClient.Get(ctx, "product:"+productID).Result()
#       if err == nil {
#           json.Unmarshal([]byte(val), &item)
#           cacheSource = "redis"
#           // return response with cached item
#       }
#   }
#
#   After DynamoDB read (cache MISS path):
#
#   if redisClient != nil && len(item) > 0 {
#       serialized, _ := json.Marshal(item)
#       redisClient.Set(ctx, "product:"+productID, serialized, 5*time.Minute)
#   }
#
# Note: The Lambda function needs the go-redis module.
# Include it in go.mod and compile the binary.
#
# Docs: https://docs.aws.amazon.com/prescriptive-guidance/latest/patterns/implement-caching.html
# ============================================================


# ============================================================
# TODO 5: Lambda Code for DAX Client  [lambda.tf]
# ============================================================
# Modify the Lambda function to use the DAX client instead of
# the standard DynamoDB client for transparent caching.
#
# Requirements:
#   Add DAX client initialization:
#
#   import "github.com/aws/aws-dax-go/dax"
#
#   var daxClient *dax.Dax
#
#   func initDAX() {
#       daxEndpoint := os.Getenv("DAX_ENDPOINT")
#       if daxEndpoint != "" {
#           cfg := dax.DefaultConfig()
#           cfg.HostPorts = []string{daxEndpoint}
#           cfg.Region = os.Getenv("AWS_REGION")
#           var err error
#           daxClient, err = dax.New(cfg)
#           if err != nil {
#               fmt.Printf("DAX connection error: %v\n", err)
#           }
#       }
#   }
#
#   In the handler, replace the DynamoDB read with:
#
#   if daxClient != nil {
#       result, err := daxClient.GetItem(ctx, &dynamodb.GetItemInput{
#           TableName: &tableName,
#           Key: map[string]types.AttributeValue{
#               "product_id": &types.AttributeValueMemberS{Value: productID},
#           },
#       })
#       cacheSource = "dax"
#   } else {
#       result, err := dynamoClient.GetItem(ctx, &dynamodb.GetItemInput{...})
#       cacheSource = "dynamodb"
#   }
#
# DAX is a write-through cache: writes go through DAX to
# DynamoDB, and reads are served from cache if available.
# The API is identical to the DynamoDB SDK -- the only change
# is the client initialization.
#
# Note: Lambda needs the aws-dax-go module compiled into the binary.
#
# Docs: https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/DAX.client.html
# ============================================================
```

### `security.tf`

```hcl
# ---------- Security Groups ----------

resource "aws_security_group" "cache" {
  name   = "${var.project_name}-cache-sg"
  vpc_id = data.aws_vpc.default.id

  ingress {
    from_port   = 6379
    to_port     = 6379
    protocol    = "tcp"
    cidr_blocks = [data.aws_vpc.default.cidr_block]
  }

  ingress {
    from_port   = 8111
    to_port     = 8111
    protocol    = "tcp"
    cidr_blocks = [data.aws_vpc.default.cidr_block]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}
```

### `dns.tf`

```hcl
# ============================================================
# TODO 1: CloudFront Distribution with Cache Key Configuration
#                                                      [dns.tf]
# ============================================================
# Create a CloudFront distribution in front of the API Gateway
# to cache responses at the edge.
#
# Requirements:
#   a) Resource: aws_cloudfront_distribution
#      - origin: domain_name = API GW domain (without https://)
#        origin_id = "api-gateway"
#        custom_origin_config with https_only
#
#   b) default_cache_behavior:
#      - allowed_methods = ["GET", "HEAD"]
#      - cached_methods  = ["GET", "HEAD"]
#      - viewer_protocol_policy = "redirect-to-https"
#      - min_ttl = 0
#      - default_ttl = 60
#      - max_ttl = 300
#
#   c) Cache key configuration (CRITICAL):
#      - Forward the "id" query string parameter (it determines
#        which product is returned)
#      - Do NOT forward the Authorization header
#      - Resource: aws_cloudfront_cache_policy
#        - query_strings_config with whitelist: ["id"]
#        - headers_config with none (do NOT include Authorization)
#
#   d) restrictions: none (no geo restrictions for lab)
#   e) viewer_certificate: cloudfront_default_certificate = true
#   f) enabled = true
#   g) price_class = "PriceClass_100" (cheapest, US/EU only)
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudfront_distribution
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudfront_cache_policy
# ============================================================
```

### `outputs.tf`

```hcl
output "api_endpoint" {
  value = aws_apigatewayv2_api.this.api_endpoint
}

output "dynamodb_table" {
  value = aws_dynamodb_table.products.name
}
```

---

## Spot the Bug

The following CloudFront cache policy and distribution configuration has a flaw that causes a 0% cache hit rate. Identify the problem before expanding the answer.

```hcl
resource "aws_cloudfront_cache_policy" "api_cache" {
  name        = "api-cache-policy"
  min_ttl     = 0
  default_ttl = 300
  max_ttl     = 3600

  parameters_in_cache_key_and_forwarded_to_origin {
    query_strings_config {
      query_string_behavior = "whitelist"
      query_strings {
        items = ["id"]
      }
    }

    headers_config {
      header_behavior = "whitelist"
      headers {
        items = ["Authorization", "Accept", "User-Agent"]
      }
    }

    cookies_config {
      cookie_behavior = "none"
    }
  }
}
```

<details>
<summary>Explain the bug</summary>

The cache policy includes `Authorization`, `Accept`, and `User-Agent` headers in the cache key. This means every unique combination of these headers creates a separate cache entry.

**The impact is devastating:**

- **Authorization header:** Every authenticated user has a unique token. If 1,000 users request the same product, CloudFront creates 1,000 separate cache entries instead of serving from one. Cache hit rate drops to near 0%.

- **User-Agent header:** Every browser version, operating system, and device creates a unique cache key. Chrome 120 on Windows, Chrome 120 on Mac, Chrome 121 on Windows -- all separate entries.

- **Accept header:** Different clients may send `application/json`, `text/html`, `*/*`, etc. Each creates a separate entry.

Combined, these three headers can produce thousands of cache key variations for the same content, making the cache effectively useless.

**The fix:**

```hcl
headers_config {
  header_behavior = "none"
}
```

Only include headers in the cache key when the response genuinely varies based on that header. For an API returning JSON product data, the response is the same regardless of who asks or what browser they use.

**If you need authentication at the edge**, use CloudFront Functions or Lambda@Edge to validate tokens WITHOUT including the Authorization header in the cache key. This way, authenticated users still share cached responses.

**Exam tip:** Any time you see "cache hit rate is low" or "CloudFront is not reducing origin load," check the cache key configuration. The most common cause is forwarding too many headers or query strings, creating excessively specific cache keys.

</details>

---

## Verify What You Learned

1. **Deploy the infrastructure:**
   ```bash
   terraform init && terraform apply -auto-approve
   ```

2. **Baseline: measure uncached API latency:**
   ```bash
   API=$(terraform output -raw api_endpoint)

   echo "=== Uncached Latency (5 requests) ==="
   for i in $(seq 1 5); do
     curl -s -o /dev/null -w "Request $i: %{time_total}s\n" \
       "$API/product?id=PROD-001"
   done
   ```

3. **Test CloudFront caching (after TODO 1):**
   ```bash
   CF_DOMAIN="<cloudfront_domain_name from terraform output>"

   echo "=== First request (cache MISS) ==="
   curl -s -D - "https://$CF_DOMAIN/product?id=PROD-001" | grep -E "X-Cache|X-Response-Time"

   echo "=== Second request (cache HIT) ==="
   curl -s -D - "https://$CF_DOMAIN/product?id=PROD-001" | grep -E "X-Cache|X-Response-Time"
   ```

4. **Test Redis caching (after TODOs 2-3):**
   ```bash
   echo "=== First request (Redis MISS, DynamoDB read + Redis write) ==="
   curl -s "$API/product?id=PROD-002" | jq '{latency_ms, cache_source}'

   echo "=== Second request (Redis HIT) ==="
   curl -s "$API/product?id=PROD-002" | jq '{latency_ms, cache_source}'
   ```

5. **Test DAX caching (after TODOs 4-5):**
   ```bash
   echo "=== First request (DAX MISS, DynamoDB read) ==="
   curl -s "$API/product?id=PROD-003" | jq '{latency_ms, cache_source}'

   echo "=== Second request (DAX HIT, microsecond read) ==="
   curl -s "$API/product?id=PROD-003" | jq '{latency_ms, cache_source}'
   ```

6. **Compare all three layers:**
   ```bash
   echo "=== Latency Comparison ==="
   echo "Direct DynamoDB:"
   curl -s "$API/product?id=PROD-004" | jq '.latency_ms'

   echo "Via Redis:"
   # First call populates cache, second measures hit
   curl -s "$API/product?id=PROD-004" > /dev/null
   curl -s "$API/product?id=PROD-004" | jq '{latency_ms, cache_source}'

   echo "Via CloudFront:"
   curl -s -w "\nTotal: %{time_total}s\n" "https://$CF_DOMAIN/product?id=PROD-004"
   ```

7. **Check CloudWatch metrics for cache performance:**
   ```bash
   aws cloudwatch get-metric-statistics \
     --namespace AWS/ElastiCache \
     --metric-name CacheHitRate \
     --dimensions Name=CacheClusterId,Value=saa-ex09-redis \
     --start-time "$(date -u -v-30M +%Y-%m-%dT%H:%M:%SZ)" \
     --end-time "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
     --period 300 \
     --statistics Average
   ```

---

## Caching Layer Decision Framework

| Criterion | CloudFront | ElastiCache Redis | DAX |
|---|---|---|---|
| **Cache location** | Edge (200+ PoPs globally) | In-region (your VPC) | In-region (your VPC) |
| **Best for** | Static content, API responses | Complex queries, sessions, aggregations | DynamoDB GetItem/Query acceleration |
| **Latency** | <10ms (from nearby edge) | Sub-millisecond (in-VPC) | Microseconds (in-VPC) |
| **Data freshness** | TTL-based (seconds to days) | TTL or explicit invalidation | Write-through (eventual, ~10ms) |
| **Code changes** | None (infrastructure only) | Significant (cache-aside logic) | Minimal (swap SDK client) |
| **Invalidation** | CreateInvalidation API (~$0.005) | DEL command (free) | Automatic on writes |
| **Multi-region** | Built-in (edge network) | Global Datastore (extra cost) | Single-region only |
| **Cost model** | Request + data transfer | Node hours (always on) | Node hours (always on) |
| **Free tier** | 1TB/month, 10M requests | None | None |

### Cache Pattern Comparison

| Pattern | How It Works | Pros | Cons |
|---|---|---|---|
| **Lazy Loading** | Read cache -> miss -> read DB -> write cache | Only caches what is needed; cache failures are not fatal | Cache miss penalty (extra round trip); stale data until TTL |
| **Write-Through** | Write to cache AND DB on every write | Cache always has latest data; no stale reads | Write penalty (double write); caches data that may never be read |
| **Write-Behind** | Write to cache, async write to DB | Fastest writes; batch DB writes | Risk of data loss if cache fails before DB write |
| **TTL-Based** | Expire cache entries after fixed time | Simple; prevents unbounded staleness | Data can be stale for up to TTL duration |

### Cost Model Comparison (Monthly)

```
Scenario: 10M requests/month, 1GB cached data, us-east-1

CloudFront:
  Requests: 10M x $0.0000001 = $1.00
  Data transfer: 1GB x $0.085 = $0.085
  Total: ~$1.09/month

ElastiCache (cache.t3.micro):
  Node: $0.017/hr x 730 hrs = $12.41
  No per-request cost
  Total: ~$12.41/month

DAX (dax.t3.small):
  Node: $0.04/hr x 730 hrs = $29.20
  No per-request cost
  Total: ~$29.20/month

Conclusion: CloudFront is cheapest for read-heavy API caching.
ElastiCache is cheapest for app-level caching with complex logic.
DAX is justified only when you need microsecond DynamoDB reads.
```

---

## Solutions

<details>
<summary>dns.tf -- TODO 1: CloudFront Distribution with Cache Key Settings</summary>

```hcl
resource "aws_cloudfront_cache_policy" "api_cache" {
  name        = "${var.project_name}-api-cache-policy"
  min_ttl     = 0
  default_ttl = 60
  max_ttl     = 300

  parameters_in_cache_key_and_forwarded_to_origin {
    query_strings_config {
      query_string_behavior = "whitelist"
      query_strings {
        items = ["id"]
      }
    }

    headers_config {
      header_behavior = "none"
    }

    cookies_config {
      cookie_behavior = "none"
    }
  }
}

resource "aws_cloudfront_distribution" "this" {
  enabled         = true
  price_class     = "PriceClass_100"
  is_ipv6_enabled = true

  origin {
    domain_name = replace(aws_apigatewayv2_api.this.api_endpoint, "https://", "")
    origin_id   = "api-gateway"

    custom_origin_config {
      http_port              = 80
      https_port             = 443
      origin_protocol_policy = "https-only"
      origin_ssl_protocols   = ["TLSv1.2"]
    }
  }

  default_cache_behavior {
    allowed_methods        = ["GET", "HEAD"]
    cached_methods         = ["GET", "HEAD"]
    target_origin_id       = "api-gateway"
    cache_policy_id        = aws_cloudfront_cache_policy.api_cache.id
    viewer_protocol_policy = "redirect-to-https"
    compress               = true
  }

  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }

  viewer_certificate {
    cloudfront_default_certificate = true
  }

  tags = {
    Name = "${var.project_name}-distribution"
  }
}

output "cloudfront_domain" {
  value = aws_cloudfront_distribution.this.domain_name
}
```

Key decisions:
- Cache key includes ONLY the `id` query string -- each product ID gets its own cache entry.
- NO headers in cache key -- all users share the same cached response for the same product.
- `default_ttl = 60` seconds balances freshness with cache efficiency for a product catalog.
- `PriceClass_100` uses only US/EU edge locations (cheapest for a lab).

</details>

<details>
<summary>database.tf -- TODO 2: ElastiCache Redis Cluster</summary>

```hcl
resource "aws_elasticache_subnet_group" "this" {
  name       = "${var.project_name}-cache-subnet"
  subnet_ids = data.aws_subnets.default.ids
}

resource "aws_elasticache_cluster" "redis" {
  cluster_id           = "${var.project_name}-redis"
  engine               = "redis"
  engine_version       = "7.1"
  node_type            = "cache.t3.micro"
  num_cache_nodes      = 1
  port                 = 6379
  subnet_group_name    = aws_elasticache_subnet_group.this.name
  security_group_ids   = [aws_security_group.cache.id]

  tags = {
    Name = "${var.project_name}-redis"
  }
}

output "redis_endpoint" {
  value = aws_elasticache_cluster.redis.cache_nodes[0].address
}
```

Update the Lambda function to connect to the VPC and set the Redis host:

```hcl
resource "aws_lambda_function" "api" {
  # ... existing config ...

  vpc_config {
    subnet_ids         = data.aws_subnets.default.ids
    security_group_ids = [aws_security_group.cache.id]
  }

  environment {
    variables = {
      TABLE_NAME   = aws_dynamodb_table.products.name
      REDIS_HOST   = aws_elasticache_cluster.redis.cache_nodes[0].address
      DAX_ENDPOINT = ""
    }
  }
}
```

Note: When Lambda runs in a VPC, it loses internet access by default. If it needs to reach DynamoDB, you need either a VPC endpoint for DynamoDB or a NAT Gateway. Add a DynamoDB VPC endpoint:

```hcl
resource "aws_vpc_endpoint" "dynamodb" {
  vpc_id       = data.aws_vpc.default.id
  service_name = "com.amazonaws.${var.region}.dynamodb"
}
```

</details>

<details>
<summary>lambda.tf -- TODO 3: Lazy-Loading Cache Pattern (Lambda Code)</summary>

Updated Lambda function with Redis lazy-loading:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/redis/go-redis/v9"
)

var dynamoClient *dynamodb.Client
var redisClient *redis.Client
var tableName string

func init() {
	tableName = os.Getenv("TABLE_NAME")
	cfg, _ := config.LoadDefaultConfig(context.Background())
	dynamoClient = dynamodb.NewFromConfig(cfg)

	// Initialize Redis client lazily
	redisHost := os.Getenv("REDIS_HOST")
	if redisHost != "" {
		redisClient = redis.NewClient(&redis.Options{
			Addr:        redisHost + ":6379",
			DialTimeout: 2 * time.Second,
		})
	}
}

func handler(ctx context.Context, event events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	start := time.Now()
	productID := "PROD-001"
	if id, ok := event.QueryStringParameters["id"]; ok {
		productID = id
	}
	cacheSource := "dynamodb"

	item := make(map[string]interface{})

	// Step 1: Check Redis cache
	if redisClient != nil {
		val, err := redisClient.Get(ctx, "product:"+productID).Result()
		if err == nil {
			if jsonErr := json.Unmarshal([]byte(val), &item); jsonErr == nil {
				cacheSource = "redis"
				elapsed := float64(time.Since(start).Microseconds()) / 1000.0
				body, _ := json.Marshal(map[string]interface{}{
					"product":      item,
					"latency_ms":   elapsed,
					"cache_source": cacheSource,
				})
				return events.APIGatewayProxyResponse{
					StatusCode: 200,
					Headers: map[string]string{
						"Content-Type":       "application/json",
						"X-Cache-Source":     cacheSource,
						"X-Response-Time-Ms": fmt.Sprintf("%.2f", elapsed),
					},
					Body: string(body),
				}, nil
			}
		} else if err != redis.Nil {
			fmt.Printf("Redis error: %v\n", err)
		}
	}

	// Step 2: Cache MISS -- read from DynamoDB
	result, err := dynamoClient.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &tableName,
		Key: map[string]types.AttributeValue{
			"product_id": &types.AttributeValueMemberS{Value: productID},
		},
	})

	if err == nil && result.Item != nil {
		for k, v := range result.Item {
			switch val := v.(type) {
			case *types.AttributeValueMemberS:
				item[k] = val.Value
			case *types.AttributeValueMemberN:
				item[k] = val.Value
			}
		}
	}

	// Step 3: Write to Redis cache (TTL 300 seconds)
	if redisClient != nil && len(item) > 0 {
		serialized, marshalErr := json.Marshal(item)
		if marshalErr == nil {
			if setErr := redisClient.Set(ctx, "product:"+productID, serialized, 5*time.Minute).Err(); setErr != nil {
				fmt.Printf("Redis write error: %v\n", setErr)
			}
		}
	}

	elapsed := float64(time.Since(start).Microseconds()) / 1000.0

	body, _ := json.Marshal(map[string]interface{}{
		"product":      item,
		"latency_ms":   elapsed,
		"cache_source": cacheSource,
	})

	return events.APIGatewayProxyResponse{
		StatusCode: 200,
		Headers: map[string]string{
			"Content-Type":       "application/json",
			"X-Cache-Source":     cacheSource,
			"X-Response-Time-Ms": fmt.Sprintf("%.2f", elapsed),
		},
		Body: string(body),
	}, nil
}

func main() {
	lambda.Start(handler)
}
```

Key design decisions in lazy loading:
- Redis errors are caught and logged, not propagated -- cache failures should never break the application.
- `DialTimeout: 2 * time.Second` prevents the Lambda from hanging if Redis is unresponsive.
- TTL of 300 seconds means data can be up to 5 minutes stale. Adjust based on freshness requirements.
- DynamoDB `AttributeValue` types are mapped to plain Go types for JSON serialization.

</details>

<details>
<summary>database.tf -- TODO 4: DAX Cluster</summary>

```hcl
resource "aws_iam_role" "dax" {
  name = "${var.project_name}-dax-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action    = "sts:AssumeRole"
      Effect    = "Allow"
      Principal = { Service = "dax.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy" "dax" {
  name = "dax-dynamodb-access"
  role = aws_iam_role.dax.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = [
        "dynamodb:GetItem",
        "dynamodb:PutItem",
        "dynamodb:Query",
        "dynamodb:Scan",
        "dynamodb:BatchGetItem",
        "dynamodb:BatchWriteItem",
        "dynamodb:DescribeTable"
      ]
      Resource = aws_dynamodb_table.products.arn
    }]
  })
}

resource "aws_dax_subnet_group" "this" {
  name       = "${var.project_name}-dax-subnet"
  subnet_ids = data.aws_subnets.default.ids
}

resource "aws_dax_cluster" "this" {
  cluster_name       = "${var.project_name}-dax"
  node_type          = "dax.t3.small"
  replication_factor = 1
  iam_role_arn       = aws_iam_role.dax.arn
  subnet_group_name  = aws_dax_subnet_group.this.name
  security_group_ids = [aws_security_group.cache.id]

  tags = {
    Name = "${var.project_name}-dax"
  }
}

output "dax_endpoint" {
  value = aws_dax_cluster.this.cluster_address
}
```

Update the Lambda environment:

```hcl
environment {
  variables = {
    TABLE_NAME   = aws_dynamodb_table.products.name
    REDIS_HOST   = aws_elasticache_cluster.redis.cache_nodes[0].address
    DAX_ENDPOINT = "${aws_dax_cluster.this.cluster_address}:8111"
  }
}
```

</details>

<details>
<summary>lambda.tf -- TODO 5: Lambda Code for DAX Client</summary>

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-dax-go/dax"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// Standard DynamoDB client
var dynamoClient *dynamodb.Client
var tableName string

// DAX client (initialized lazily)
var daxClient *dax.Dax

func init() {
	tableName = os.Getenv("TABLE_NAME")
	cfg, _ := config.LoadDefaultConfig(context.Background())
	dynamoClient = dynamodb.NewFromConfig(cfg)

	// Initialize DAX client
	daxEndpoint := os.Getenv("DAX_ENDPOINT")
	if daxEndpoint != "" {
		daxCfg := dax.DefaultConfig()
		daxCfg.HostPorts = []string{daxEndpoint}
		daxCfg.Region = os.Getenv("AWS_REGION")
		var err error
		daxClient, err = dax.New(daxCfg)
		if err != nil {
			fmt.Printf("DAX connection error: %v\n", err)
		}
	}
}

func handler(ctx context.Context, event events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	start := time.Now()
	productID := "PROD-001"
	if id, ok := event.QueryStringParameters["id"]; ok {
		productID = id
	}
	cacheSource := "dynamodb"

	input := &dynamodb.GetItemInput{
		TableName: &tableName,
		Key: map[string]types.AttributeValue{
			"product_id": &types.AttributeValueMemberS{Value: productID},
		},
	}

	// Try DAX first (transparent caching)
	var result *dynamodb.GetItemOutput
	var err error
	if daxClient != nil {
		result, err = daxClient.GetItem(ctx, input)
		if err != nil {
			fmt.Printf("DAX error, falling back to DynamoDB: %v\n", err)
			result, err = dynamoClient.GetItem(ctx, input)
		} else {
			cacheSource = "dax"
		}
	} else {
		result, err = dynamoClient.GetItem(ctx, input)
	}

	item := make(map[string]interface{})
	if err == nil && result.Item != nil {
		for k, v := range result.Item {
			switch val := v.(type) {
			case *types.AttributeValueMemberS:
				item[k] = val.Value
			case *types.AttributeValueMemberN:
				item[k] = val.Value
			}
		}
	}

	elapsed := float64(time.Since(start).Microseconds()) / 1000.0

	body, _ := json.Marshal(map[string]interface{}{
		"product":      item,
		"latency_ms":   elapsed,
		"cache_source": cacheSource,
	})

	return events.APIGatewayProxyResponse{
		StatusCode: 200,
		Headers: map[string]string{
			"Content-Type":       "application/json",
			"X-Cache-Source":     cacheSource,
			"X-Response-Time-Ms": fmt.Sprintf("%.2f", elapsed),
		},
		Body: string(body),
	}, nil
}

func main() {
	lambda.Start(handler)
}
```

The DAX client is a drop-in replacement for the DynamoDB client. The same `GetItem`, `Query`, and `Scan` calls work identically. DAX handles caching transparently:
- **Item cache:** Caches individual `GetItem` and `BatchGetItem` results (default 5-minute TTL).
- **Query cache:** Caches `Query` and `Scan` results (default 5-minute TTL).
- Writes (`PutItem`, `UpdateItem`, `DeleteItem`) go through DAX to DynamoDB and update the cache immediately (write-through).

**Build requirement:** The `aws-dax-go` module must be included in `go.mod` and compiled into the Lambda binary. The binary is deployed as `bootstrap` with `runtime = "provided.al2023"`.

</details>

---

## Cleanup

```bash
terraform destroy -auto-approve
```

CloudFront distributions can take 10-15 minutes to disable and delete. If `terraform destroy` times out, wait and retry:

```bash
aws cloudfront list-distributions \
  --query 'DistributionList.Items[?Comment==`saa-ex09`].{Id:Id,Status:Status}'
```

Verify all resources are deleted:

```bash
aws elasticache describe-cache-clusters --query 'CacheClusters[?starts_with(CacheClusterId, `saa-ex09`)]'
aws dax describe-clusters --query 'Clusters[?starts_with(ClusterName, `saa-ex09`)]' 2>/dev/null || echo "No DAX clusters"
aws dynamodb describe-table --table-name saa-ex09-products 2>/dev/null && echo "Table still exists!" || echo "Table deleted"
```

---

## What's Next

Exercise 10 explores hybrid storage with AWS Storage Gateway, bridging on-premises storage with cloud. The caching concepts from this exercise extend directly into Storage Gateway, which uses local caching to provide low-latency access to frequently used data while storing the full dataset in S3. The same cache hit/miss patterns and TTL considerations apply.

---

## Summary

You built a three-layer caching architecture: CloudFront at the edge for geographic distribution, ElastiCache Redis at the application level for computed results with lazy-loading patterns, and DAX at the database level for transparent DynamoDB acceleration. You measured latency at each layer and observed the progression from millisecond DynamoDB reads to sub-millisecond Redis cache hits to microsecond DAX cache hits. The key architectural insight is that these layers are complementary, not competing: CloudFront reduces origin traffic globally, Redis caches application-level computations, and DAX accelerates DynamoDB-specific read patterns with minimal code changes. For the SAA-C03, remember the cache key pitfall (forwarding Authorization headers kills CloudFront hit rate), the lazy-loading vs write-through pattern trade-offs, and that DAX is a DynamoDB-specific solution, not a general-purpose cache.

---

## Reference

- [CloudFront Cache Behavior](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/distribution-web-values-specify.html)
- [CloudFront Cache Keys](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/understanding-the-cache-key.html)
- [ElastiCache for Redis](https://docs.aws.amazon.com/AmazonElastiCache/latest/red-ug/WhatIs.html)
- [DAX Developer Guide](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/DAX.html)
- [Caching Patterns (AWS Prescriptive Guidance)](https://docs.aws.amazon.com/prescriptive-guidance/latest/patterns/implement-caching.html)

## Additional Resources

- [Terraform aws_cloudfront_distribution](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudfront_distribution)
- [Terraform aws_elasticache_cluster](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/elasticache_cluster)
- [Terraform aws_dax_cluster](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/dax_cluster)
- [CloudFront Pricing](https://aws.amazon.com/cloudfront/pricing/)
- [ElastiCache Pricing](https://aws.amazon.com/elasticache/pricing/)
- [DAX Pricing](https://aws.amazon.com/dynamodb/pricing/on-demand/)

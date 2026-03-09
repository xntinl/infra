# 98. ElastiCache Redis for Session Management

<!--
difficulty: intermediate
concepts: [elasticache-redis, session-management, ttl, in-memory-caching, vpc-networking, encryption-in-transit, dynamodb-comparison]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: apply
prerequisites: [none]
aws_cost: ~$0.05/hr
-->

> **AWS Cost Warning:** This exercise creates an ElastiCache Redis cluster (cache.t3.micro), a VPC with subnets, and Lambda functions. ElastiCache cache.t3.micro costs approximately $0.017/hr. Combined with VPC NAT Gateway ($0.045/hr) if needed for Lambda VPC access, total cost is approximately $0.05/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.5 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| jq installed | `jq --version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Configure** an ElastiCache Redis cluster with encryption in transit and a subnet group for VPC placement
2. **Implement** session storage and retrieval in a Lambda function using Redis SET/GET with TTL expiration
3. **Compare** ElastiCache Redis and DynamoDB for session management based on latency, cost, TTL behavior, and operational complexity
4. **Diagnose** the security risk of unencrypted in-transit communication exposing session data on the network
5. **Apply** VPC networking to connect a Lambda function to a Redis cluster in private subnets

## Why ElastiCache Redis for Session Management

Web applications need session state: login tokens, shopping carts, user preferences. Storing sessions in the application server's memory fails when you scale to multiple instances -- each server has different session data. You need a centralized session store.

ElastiCache Redis provides sub-millisecond latency for session reads and writes. A typical session lookup takes 0.2-0.5ms with Redis versus 2-5ms with DynamoDB. For applications serving thousands of requests per second, this latency difference matters. Redis also provides native TTL expiration -- set a session to expire in 30 minutes and Redis automatically evicts it. No background processes, no scan operations.

The DVA-C02 exam compares Redis and DynamoDB for session storage:

| Criteria | ElastiCache Redis | DynamoDB |
|----------|-------------------|----------|
| **Latency** | Sub-millisecond (0.2-0.5ms) | Single-digit milliseconds (2-5ms) |
| **TTL** | Native, automatic eviction | TTL attribute, eventual deletion (up to 48h delay) |
| **Durability** | In-memory (lost on restart unless persistence enabled) | Durable (replicated across AZs) |
| **Cost model** | Per-node hourly (always running) | Per-request (pay per read/write) |
| **Scaling** | Vertical (node size) + horizontal (read replicas) | Automatic (on-demand) |
| **VPC required** | Yes (runs in VPC subnets) | No (accessed via public API endpoint) |
| **Managed failover** | Multi-AZ with automatic failover | Built-in (no configuration needed) |

The exam frequently asks: "Which service provides the lowest latency for session data?" (Redis) and "Which service requires no VPC configuration?" (DynamoDB).

## Building Blocks

### `lambda/main.go`

Your job is to fill in the `// TODO` sections for Redis session operations:

```go
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/redis/go-redis/v9"
	"github.com/google/uuid"
)

var redisClient *redis.Client

func init() {
	redisAddr := os.Getenv("REDIS_ENDPOINT")
	redisClient = redis.NewClient(&redis.Options{
		Addr: redisAddr,
		// TODO 1 -- Enable TLS for encryption in transit
		// Add TLSConfig to encrypt data between Lambda and Redis
		// Hint: TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}
	})
}

type SessionRequest struct {
	Action    string `json:"action"`
	SessionID string `json:"session_id,omitempty"`
	UserID    string `json:"user_id,omitempty"`
	Data      map[string]string `json:"data,omitempty"`
}

type SessionResponse struct {
	SessionID string            `json:"session_id"`
	UserID    string            `json:"user_id,omitempty"`
	Data      map[string]string `json:"data,omitempty"`
	TTL       string            `json:"ttl,omitempty"`
	Message   string            `json:"message"`
}

func handler(ctx context.Context, req SessionRequest) (SessionResponse, error) {
	switch req.Action {
	case "create":
		return createSession(ctx, req)
	case "get":
		return getSession(ctx, req)
	case "delete":
		return deleteSession(ctx, req)
	default:
		return SessionResponse{}, fmt.Errorf("unknown action: %s", req.Action)
	}
}

func createSession(ctx context.Context, req SessionRequest) (SessionResponse, error) {
	sessionID := uuid.New().String()
	sessionKey := fmt.Sprintf("session:%s", sessionID)

	sessionData := map[string]string{
		"user_id":    req.UserID,
		"created_at": time.Now().UTC().Format(time.RFC3339),
	}
	for k, v := range req.Data {
		sessionData[k] = v
	}

	// TODO 2 -- Store session data in Redis with TTL
	// Use redisClient.HSet to store the session as a hash
	// Then use redisClient.Expire to set a 30-minute TTL
	//
	// Steps:
	//   1. Convert sessionData map to []interface{} for HSet
	//   2. Call redisClient.HSet(ctx, sessionKey, fields...)
	//   3. Call redisClient.Expire(ctx, sessionKey, 30*time.Minute)
	//
	// The hash allows storing multiple fields per session
	// (user_id, created_at, custom data) in a single Redis key

	_ = sessionKey
	_ = sessionData

	return SessionResponse{
		SessionID: sessionID,
		UserID:    req.UserID,
		Data:      sessionData,
		TTL:       "30m",
		Message:   "Session created",
	}, nil
}

func getSession(ctx context.Context, req SessionRequest) (SessionResponse, error) {
	sessionKey := fmt.Sprintf("session:%s", req.SessionID)

	// TODO 3 -- Retrieve session data from Redis
	// Use redisClient.HGetAll to get all fields of the session hash
	// Check if the result is empty (session expired or not found)
	// Also get the remaining TTL with redisClient.TTL

	_ = sessionKey

	return SessionResponse{
		SessionID: req.SessionID,
		Message:   "TODO: implement session retrieval",
	}, nil
}

func deleteSession(ctx context.Context, req SessionRequest) (SessionResponse, error) {
	sessionKey := fmt.Sprintf("session:%s", req.SessionID)

	err := redisClient.Del(ctx, sessionKey).Err()
	if err != nil {
		return SessionResponse{}, fmt.Errorf("failed to delete session: %w", err)
	}

	return SessionResponse{
		SessionID: req.SessionID,
		Message:   "Session deleted",
	}, nil
}

func main() {
	lambda.Start(handler)
}
```

### Terraform Skeleton

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
  default     = "elasticache-demo"
}
```

### `vpc.tf`

```hcl
data "aws_availability_zones" "available" {
  state = "available"
}

resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_hostnames = true
  enable_dns_support   = true
  tags = { Name = "${var.project_name}-vpc" }
}

resource "aws_subnet" "private_a" {
  vpc_id            = aws_vpc.this.id
  cidr_block        = "10.0.1.0/24"
  availability_zone = data.aws_availability_zones.available.names[0]
  tags = { Name = "${var.project_name}-private-a" }
}

resource "aws_subnet" "private_b" {
  vpc_id            = aws_vpc.this.id
  cidr_block        = "10.0.2.0/24"
  availability_zone = data.aws_availability_zones.available.names[1]
  tags = { Name = "${var.project_name}-private-b" }
}
```

### `security.tf`

```hcl
resource "aws_security_group" "lambda" {
  name   = "${var.project_name}-lambda-sg"
  vpc_id = aws_vpc.this.id

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "${var.project_name}-lambda-sg" }
}

resource "aws_security_group" "redis" {
  name   = "${var.project_name}-redis-sg"
  vpc_id = aws_vpc.this.id

  ingress {
    from_port       = 6379
    to_port         = 6379
    protocol        = "tcp"
    security_groups = [aws_security_group.lambda.id]
  }

  tags = { Name = "${var.project_name}-redis-sg" }
}
```

### `database.tf`

```hcl
# =======================================================
# TODO 1 -- ElastiCache Subnet Group
# =======================================================
# Requirements:
#   - Create an aws_elasticache_subnet_group named "elasticache-demo"
#   - Include both private subnets
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/elasticache_subnet_group


# =======================================================
# TODO 2 -- ElastiCache Redis Cluster
# =======================================================
# Requirements:
#   - Create an aws_elasticache_cluster named "session-store"
#   - Set engine to "redis"
#   - Set engine_version to "7.0"
#   - Set node_type to "cache.t3.micro"
#   - Set num_cache_nodes to 1
#   - Set subnet_group_name to the subnet group
#   - Set security_group_ids to the Redis security group
#   - Set transit_encryption_enabled to true
#   - Set parameter_group_name to "default.redis7"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/elasticache_cluster
# Note: For transit encryption, use aws_elasticache_replication_group
#       instead of aws_elasticache_cluster if you need TLS support
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

resource "aws_iam_role_policy_attachment" "vpc" {
  role       = aws_iam_role.this.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaVPCAccessExecutionRole"
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
  memory_size      = 256
  timeout          = 30

  vpc_config {
    subnet_ids         = [aws_subnet.private_a.id, aws_subnet.private_b.id]
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = {
      REDIS_ENDPOINT = "${aws_elasticache_cluster.this.cache_nodes[0].address}:${aws_elasticache_cluster.this.cache_nodes[0].port}"
    }
  }

  depends_on = [
    aws_iam_role_policy_attachment.basic,
    aws_iam_role_policy_attachment.vpc,
    aws_cloudwatch_log_group.this,
  ]
}
```

### `outputs.tf`

```hcl
output "function_name" {
  value = aws_lambda_function.this.function_name
}

output "redis_endpoint" {
  value = "${aws_elasticache_cluster.this.cache_nodes[0].address}:${aws_elasticache_cluster.this.cache_nodes[0].port}"
}
```

## Spot the Bug

A developer creates an ElastiCache Redis cluster for session management. Sessions contain authentication tokens and user profile data. The cluster works correctly, but a security audit reveals that session data is transmitted in plaintext between the Lambda function and Redis.

```hcl
resource "aws_elasticache_cluster" "sessions" {
  cluster_id         = "session-store"
  engine             = "redis"
  node_type          = "cache.t3.micro"
  num_cache_nodes    = 1
  subnet_group_name  = aws_elasticache_subnet_group.this.name
  security_group_ids = [aws_security_group.redis.id]
  # No transit_encryption_enabled!
}
```

```go
redisClient = redis.NewClient(&redis.Options{
    Addr: redisAddr,
    // No TLS configuration!
})
```

<details>
<summary>Explain the bug</summary>

The Redis cluster has **no encryption in transit** configured. All data between the Lambda function and Redis -- including session tokens, user IDs, and profile data -- is sent as plaintext TCP traffic. Anyone with network access to the VPC (e.g., a compromised EC2 instance in the same VPC) could sniff this traffic.

Two fixes are required (both must be applied):

**1. Enable transit encryption on the Redis cluster:**

For encryption in transit, use `aws_elasticache_replication_group` instead of `aws_elasticache_cluster`:

```hcl
resource "aws_elasticache_replication_group" "sessions" {
  replication_group_id       = "session-store"
  description                = "Session store with encryption"
  engine                     = "redis"
  engine_version             = "7.0"
  node_type                  = "cache.t3.micro"
  num_cache_clusters         = 1
  subnet_group_name          = aws_elasticache_subnet_group.this.name
  security_group_ids         = [aws_security_group.redis.id]
  transit_encryption_enabled = true
  at_rest_encryption_enabled = true
}
```

**2. Configure TLS in the Redis client:**

```go
import "crypto/tls"

redisClient = redis.NewClient(&redis.Options{
    Addr: redisAddr,
    TLSConfig: &tls.Config{
        MinVersion: tls.VersionTLS12,
    },
})
```

Without both changes, either: (a) the cluster rejects TLS connections because it is not configured, or (b) the client sends plaintext because TLS is not requested.

Note: Enabling transit encryption on an existing cluster requires recreating the cluster -- it cannot be modified in place.

</details>

## Verify What You Learned

### Step 1 -- Apply

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init && terraform apply -auto-approve
```

Note: ElastiCache cluster creation takes 5-10 minutes.

### Step 2 -- Create a session

```bash
FUNC=$(terraform output -raw function_name)

aws lambda invoke --function-name "$FUNC" \
  --payload '{"action": "create", "user_id": "user-123", "data": {"role": "admin", "theme": "dark"}}' \
  /dev/stdout 2>/dev/null | jq .
```

Note the `session_id` from the response.

### Step 3 -- Retrieve the session

```bash
aws lambda invoke --function-name "$FUNC" \
  --payload '{"action": "get", "session_id": "<session_id_from_step_2>"}' \
  /dev/stdout 2>/dev/null | jq .
```

Expected: session data with user_id, role, theme, and remaining TTL.

### Step 4 -- Verify Redis endpoint

```bash
echo "Redis endpoint: $(terraform output -raw redis_endpoint)"
```

### Step 5 -- Verify no drift

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Solutions

<details>
<summary>database.tf -- TODO 1 -- ElastiCache Subnet Group</summary>

```hcl
resource "aws_elasticache_subnet_group" "this" {
  name       = "elasticache-demo"
  subnet_ids = [aws_subnet.private_a.id, aws_subnet.private_b.id]
}
```

</details>

<details>
<summary>database.tf -- TODO 2 -- ElastiCache Redis Cluster</summary>

```hcl
resource "aws_elasticache_cluster" "this" {
  cluster_id           = "session-store"
  engine               = "redis"
  engine_version       = "7.0"
  node_type            = "cache.t3.micro"
  num_cache_nodes      = 1
  subnet_group_name    = aws_elasticache_subnet_group.this.name
  security_group_ids   = [aws_security_group.redis.id]
  parameter_group_name = "default.redis7"
}
```

Note: For transit encryption, replace with `aws_elasticache_replication_group` as shown in the Spot the Bug solution.

</details>

<details>
<summary>lambda/main.go -- Go TODO Solutions -- Redis Session Operations</summary>

**TODO 1 -- TLS Config:**
```go
import "crypto/tls"

redisClient = redis.NewClient(&redis.Options{
    Addr: redisAddr,
    TLSConfig: &tls.Config{
        MinVersion: tls.VersionTLS12,
    },
})
```

**TODO 2 -- Store session with TTL:**
```go
fields := make([]interface{}, 0, len(sessionData)*2)
for k, v := range sessionData {
    fields = append(fields, k, v)
}

if err := redisClient.HSet(ctx, sessionKey, fields...).Err(); err != nil {
    return SessionResponse{}, fmt.Errorf("failed to store session: %w", err)
}

if err := redisClient.Expire(ctx, sessionKey, 30*time.Minute).Err(); err != nil {
    return SessionResponse{}, fmt.Errorf("failed to set TTL: %w", err)
}
```

**TODO 3 -- Retrieve session:**
```go
data, err := redisClient.HGetAll(ctx, sessionKey).Result()
if err != nil {
    return SessionResponse{}, fmt.Errorf("failed to get session: %w", err)
}

if len(data) == 0 {
    return SessionResponse{
        SessionID: req.SessionID,
        Message:   "Session not found or expired",
    }, nil
}

ttl, _ := redisClient.TTL(ctx, sessionKey).Result()

return SessionResponse{
    SessionID: req.SessionID,
    UserID:    data["user_id"],
    Data:      data,
    TTL:       ttl.String(),
    Message:   "Session retrieved",
}, nil
```

</details>

## Cleanup

Destroy all resources to stop incurring charges:

```bash
terraform destroy -auto-approve
```

Note: ElastiCache cluster deletion takes 5-10 minutes.

Verify nothing remains:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

You built session management with ElastiCache Redis and compared it with DynamoDB. In the next exercise, you will configure **S3 event notifications triggering Lambda** for file processing workflows.

## Summary

- **ElastiCache Redis** provides sub-millisecond session reads (0.2-0.5ms) versus DynamoDB's single-digit milliseconds (2-5ms)
- Redis **TTL** evicts expired sessions automatically and immediately; DynamoDB TTL deletions can be delayed up to 48 hours
- Redis runs **inside a VPC** -- Lambda must be in the same VPC with appropriate security groups to connect
- **Encryption in transit** requires both cluster-level configuration (`transit_encryption_enabled`) and client-level TLS config
- DynamoDB sessions require **no VPC** and scale automatically; Redis requires capacity planning and node type selection
- Use Redis when: sub-millisecond latency is critical, exact TTL expiration is needed, or session data is ephemeral
- Use DynamoDB when: durability is required, VPC complexity is undesirable, or traffic is unpredictable (on-demand pricing)
- Lambda in VPC needs `AWSLambdaVPCAccessExecutionRole` for ENI management

## Reference

- [ElastiCache for Redis](https://docs.aws.amazon.com/AmazonElastiCache/latest/red-ug/WhatIs.html)
- [Terraform aws_elasticache_cluster](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/elasticache_cluster)
- [Terraform aws_elasticache_replication_group](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/elasticache_replication_group)
- [Lambda VPC Configuration](https://docs.aws.amazon.com/lambda/latest/dg/configuration-vpc.html)

## Additional Resources

- [ElastiCache Encryption In Transit](https://docs.aws.amazon.com/AmazonElastiCache/latest/red-ug/in-transit-encryption.html) -- TLS configuration for Redis clusters
- [ElastiCache Best Practices](https://docs.aws.amazon.com/AmazonElastiCache/latest/red-ug/BestPractices.html) -- node sizing, connection pooling, and failover
- [Session Management Comparison](https://aws.amazon.com/caching/session-management/) -- AWS guide comparing ElastiCache, DynamoDB, and in-memory sessions
- [ElastiCache Pricing](https://aws.amazon.com/elasticache/pricing/) -- per-node hourly pricing by instance type

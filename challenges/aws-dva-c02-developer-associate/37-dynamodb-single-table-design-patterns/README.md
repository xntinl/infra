# 37. DynamoDB Single-Table Design Patterns

<!--
difficulty: basic
concepts: [single-table-design, partition-key-overloading, sort-key-overloading, composite-keys, access-patterns, entity-types, pk-sk-pattern]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: understand
prerequisites: [03-dynamodb-developer-sdk-operations]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a DynamoDB table in on-demand mode and three Lambda functions. Costs are negligible during testing (~$0.01/hr or less). Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally (for compiling the Lambda binaries)

## Learning Objectives

After completing this exercise, you will be able to:

- **Identify** how partition key and sort key overloading stores multiple entity types (User, Order, OrderItem) in a single DynamoDB table
- **Construct** composite key patterns (PK=`USER#123`, SK=`ORDER#456`) that support multiple access patterns without additional tables
- **Verify** that Query operations on a single partition retrieve only the intended entity type using sort key prefix conditions
- **Explain** why single-table design reduces the number of DynamoDB tables, simplifies IAM policies, and enables transactional operations across entity types
- **Describe** the trade-offs of single-table design: increased query complexity, reduced readability, and the need for careful key schema planning

## Why Single-Table Design

In relational databases, each entity gets its own table: Users, Orders, OrderItems. In DynamoDB, the recommended approach for most applications is to store all entities in a single table using key overloading. This is not because DynamoDB cannot handle multiple tables -- it is because DynamoDB charges per table for provisioned capacity, and more importantly, DynamoDB does not support joins. If you need data from Users and Orders in a single request, they must be in the same table so you can use a single Query operation.

The DVA-C02 exam tests single-table design because it is the foundation of DynamoDB data modeling. Questions describe access patterns and ask you to choose the correct PK/SK schema. For example, "get all orders for a user" requires the user ID as the partition key and order IDs as sort keys. "Get all items in an order" requires the order ID as the partition key and item IDs as sort keys. In single-table design, you overload the same PK/SK attributes to store both patterns: `PK=USER#123, SK=ORDER#456` for an order and `PK=ORDER#456, SK=ITEM#789` for an order item.

## Step 1 -- Understand the Data Model

Three entity types share one table:

| Entity | PK | SK | Attributes |
|--------|-----|-----|------------|
| User | `USER#<user_id>` | `PROFILE` | name, email, tier |
| Order | `USER#<user_id>` | `ORDER#<order_id>` | order_date, status, total |
| OrderItem | `ORDER#<order_id>` | `ITEM#<item_id>` | product_name, price, quantity |

**Access patterns supported:**

1. **Get user profile:** Query PK=`USER#123`, SK=`PROFILE`
2. **Get all orders for a user:** Query PK=`USER#123`, SK begins_with `ORDER#`
3. **Get all items in an order:** Query PK=`ORDER#456`, SK begins_with `ITEM#`
4. **Get user profile + all orders:** Query PK=`USER#123` (returns profile + all orders in one query)

## Step 2 -- Create the Lambda Function Code

### `write_data/main.go`

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

var client *dynamodb.Client
var tableName string

func init() {
	tableName = os.Getenv("TABLE_NAME")
	cfg, _ := config.LoadDefaultConfig(context.Background())
	client = dynamodb.NewFromConfig(cfg)
}

func handler(ctx context.Context, event json.RawMessage) (map[string]interface{}, error) {
	items := []map[string]types.AttributeValue{
		// User profile
		{
			"PK":    &types.AttributeValueMemberS{Value: "USER#u001"},
			"SK":    &types.AttributeValueMemberS{Value: "PROFILE"},
			"name":  &types.AttributeValueMemberS{Value: "Alice Johnson"},
			"email": &types.AttributeValueMemberS{Value: "alice@example.com"},
			"tier":  &types.AttributeValueMemberS{Value: "premium"},
		},
		// Orders for Alice
		{
			"PK":         &types.AttributeValueMemberS{Value: "USER#u001"},
			"SK":         &types.AttributeValueMemberS{Value: "ORDER#ord-001"},
			"order_date": &types.AttributeValueMemberS{Value: "2024-01-15"},
			"status":     &types.AttributeValueMemberS{Value: "shipped"},
			"total":      &types.AttributeValueMemberN{Value: "129.99"},
		},
		{
			"PK":         &types.AttributeValueMemberS{Value: "USER#u001"},
			"SK":         &types.AttributeValueMemberS{Value: "ORDER#ord-002"},
			"order_date": &types.AttributeValueMemberS{Value: "2024-02-20"},
			"status":     &types.AttributeValueMemberS{Value: "pending"},
			"total":      &types.AttributeValueMemberN{Value: "49.50"},
		},
		// Items in order ord-001
		{
			"PK":           &types.AttributeValueMemberS{Value: "ORDER#ord-001"},
			"SK":           &types.AttributeValueMemberS{Value: "ITEM#item-001"},
			"product_name": &types.AttributeValueMemberS{Value: "Keyboard"},
			"price":        &types.AttributeValueMemberN{Value: "79.99"},
			"quantity":     &types.AttributeValueMemberN{Value: "1"},
		},
		{
			"PK":           &types.AttributeValueMemberS{Value: "ORDER#ord-001"},
			"SK":           &types.AttributeValueMemberS{Value: "ITEM#item-002"},
			"product_name": &types.AttributeValueMemberS{Value: "Mouse"},
			"price":        &types.AttributeValueMemberN{Value: "49.99"},
			"quantity":     &types.AttributeValueMemberN{Value: "1"},
		},
		// Items in order ord-002
		{
			"PK":           &types.AttributeValueMemberS{Value: "ORDER#ord-002"},
			"SK":           &types.AttributeValueMemberS{Value: "ITEM#item-003"},
			"product_name": &types.AttributeValueMemberS{Value: "USB Cable"},
			"price":        &types.AttributeValueMemberN{Value: "9.99"},
			"quantity":     &types.AttributeValueMemberN{Value: "5"},
		},
	}

	for _, item := range items {
		_, err := client.PutItem(ctx, &dynamodb.PutItemInput{
			TableName: aws.String(tableName),
			Item:      item,
		})
		if err != nil {
			return nil, fmt.Errorf("PutItem: %w", err)
		}
	}

	body, _ := json.Marshal(map[string]interface{}{
		"message":    "Seed data written",
		"item_count": len(items),
	})
	return map[string]interface{}{"statusCode": 200, "body": string(body)}, nil
}

func main() { lambda.Start(handler) }
```

### `query_user/main.go`

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

var client *dynamodb.Client
var tableName string

func init() {
	tableName = os.Getenv("TABLE_NAME")
	cfg, _ := config.LoadDefaultConfig(context.Background())
	client = dynamodb.NewFromConfig(cfg)
}

type Request struct {
	UserID    string `json:"user_id"`
	OrderOnly bool   `json:"order_only"`
}

func handler(ctx context.Context, event json.RawMessage) (map[string]interface{}, error) {
	var req Request
	json.Unmarshal(event, &req)

	pk := fmt.Sprintf("USER#%s", req.UserID)

	input := &dynamodb.QueryInput{
		TableName:              aws.String(tableName),
		KeyConditionExpression: aws.String("PK = :pk"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk": &types.AttributeValueMemberS{Value: pk},
		},
	}

	// Optionally filter to only orders (SK begins_with "ORDER#")
	if req.OrderOnly {
		input.KeyConditionExpression = aws.String("PK = :pk AND begins_with(SK, :sk_prefix)")
		input.ExpressionAttributeValues[":sk_prefix"] = &types.AttributeValueMemberS{Value: "ORDER#"}
	}

	result, err := client.Query(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("Query: %w", err)
	}

	items := make([]map[string]interface{}, 0, len(result.Items))
	for _, item := range result.Items {
		var raw map[string]interface{}
		attributevalue.UnmarshalMap(item, &raw)
		items = append(items, raw)
	}

	body, _ := json.Marshal(map[string]interface{}{
		"query":         "user data",
		"pk":            pk,
		"count":         result.Count,
		"items":         items,
		"orders_only":   req.OrderOnly,
	})
	return map[string]interface{}{"statusCode": 200, "body": string(body)}, nil
}

func main() { lambda.Start(handler) }
```

### `query_order_items/main.go`

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

var client *dynamodb.Client
var tableName string

func init() {
	tableName = os.Getenv("TABLE_NAME")
	cfg, _ := config.LoadDefaultConfig(context.Background())
	client = dynamodb.NewFromConfig(cfg)
}

type Request struct {
	OrderID string `json:"order_id"`
}

func handler(ctx context.Context, event json.RawMessage) (map[string]interface{}, error) {
	var req Request
	json.Unmarshal(event, &req)

	pk := fmt.Sprintf("ORDER#%s", req.OrderID)

	result, err := client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(tableName),
		KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :sk_prefix)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk":        &types.AttributeValueMemberS{Value: pk},
			":sk_prefix": &types.AttributeValueMemberS{Value: "ITEM#"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("Query: %w", err)
	}

	items := make([]map[string]interface{}, 0, len(result.Items))
	for _, item := range result.Items {
		var raw map[string]interface{}
		attributevalue.UnmarshalMap(item, &raw)
		items = append(items, raw)
	}

	body, _ := json.Marshal(map[string]interface{}{
		"query":    "order items",
		"pk":       pk,
		"count":    result.Count,
		"items":    items,
	})
	return map[string]interface{}{"statusCode": 200, "body": string(body)}, nil
}

func main() { lambda.Start(handler) }
```

## Step 3 -- Create the Terraform Configuration

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
  default     = "single-table"
}
```

### `database.tf`

```hcl
resource "aws_dynamodb_table" "this" {
  name         = "${var.project_name}-data"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "PK"
  range_key    = "SK"

  attribute { name = "PK"; type = "S" }
  attribute { name = "SK"; type = "S" }
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

data "aws_iam_policy_document" "ddb" {
  statement {
    actions   = ["dynamodb:PutItem", "dynamodb:Query", "dynamodb:GetItem"]
    resources = [aws_dynamodb_table.this.arn]
  }
}

resource "aws_iam_policy" "ddb" {
  name   = "${var.project_name}-ddb-access"
  policy = data.aws_iam_policy_document.ddb.json
}

resource "aws_iam_role_policy_attachment" "ddb" {
  role       = aws_iam_role.this.name
  policy_arn = aws_iam_policy.ddb.arn
}
```

### `build.tf`

```hcl
locals {
  functions = toset(["write_data", "query_user", "query_order_items"])
}

resource "null_resource" "go_build" {
  for_each = local.functions
  triggers = { source_hash = filebase64sha256("${path.module}/${each.key}/main.go") }
  provisioner "local-exec" {
    command     = "GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go"
    working_dir = "${path.module}/${each.key}"
  }
}

data "archive_file" "fn" {
  for_each    = local.functions
  type        = "zip"
  source_file = "${path.module}/${each.key}/bootstrap"
  output_path = "${path.module}/build/${each.key}.zip"
  depends_on  = [null_resource.go_build]
}
```

### `lambda.tf`

```hcl
resource "aws_cloudwatch_log_group" "fn" {
  for_each          = local.functions
  name              = "/aws/lambda/${var.project_name}-${each.key}"
  retention_in_days = 1
}

resource "aws_lambda_function" "fn" {
  for_each         = local.functions
  function_name    = "${var.project_name}-${each.key}"
  filename         = data.archive_file.fn[each.key].output_path
  source_code_hash = data.archive_file.fn[each.key].output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.this.arn
  timeout          = 15
  environment { variables = { TABLE_NAME = aws_dynamodb_table.this.name } }
  depends_on = [aws_iam_role_policy_attachment.basic, aws_iam_role_policy_attachment.ddb, aws_cloudwatch_log_group.fn]
}
```

### `outputs.tf`

```hcl
output "table_name" { value = aws_dynamodb_table.this.name }
output "function_names" { value = { for k, v in aws_lambda_function.fn : k => v.function_name } }
```

## Step 4 -- Build and Deploy

```bash
for fn in write_data query_user query_order_items; do
  (cd "$fn" && GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go)
done

terraform init
terraform apply -auto-approve
```

## Step 5 -- Seed Data and Query

```bash
# Seed the table with sample entities
aws lambda invoke --function-name single-table-write_data \
  --payload '{}' /dev/stdout 2>/dev/null | jq -r '.body' | jq .

# Get user profile + all orders in one query
aws lambda invoke --function-name single-table-query_user \
  --payload '{"user_id":"u001"}' /dev/stdout 2>/dev/null | jq -r '.body' | jq .

# Get only orders (no profile)
aws lambda invoke --function-name single-table-query_user \
  --payload '{"user_id":"u001","order_only":true}' /dev/stdout 2>/dev/null | jq -r '.body' | jq .

# Get all items in an order
aws lambda invoke --function-name single-table-query_order_items \
  --payload '{"order_id":"ord-001"}' /dev/stdout 2>/dev/null | jq -r '.body' | jq .
```

## Common Mistakes

### 1. Using different tables for each entity type

**Wrong approach:** Creating `users`, `orders`, and `order_items` tables separately.

**What happens:** You cannot query a user and their orders in a single DynamoDB request. You must make multiple API calls (one per table) and join the results in your application code. This increases latency and costs.

**Fix:** Use single-table design with PK/SK overloading. One Query on `PK=USER#123` returns the profile and all orders.

### 2. Using Scan instead of Query for entity-type filtering

**Wrong:**

```go
// Scanning the entire table and filtering by entity type
result, _ := client.Scan(ctx, &dynamodb.ScanInput{
    TableName:        aws.String(tableName),
    FilterExpression: aws.String("begins_with(PK, :prefix)"),
    ExpressionAttributeValues: map[string]types.AttributeValue{
        ":prefix": &types.AttributeValueMemberS{Value: "USER#"},
    },
})
```

**What happens:** Scans the entire table and charges RCU for every item, then filters. This is O(n) on table size.

**Fix:** Use Query with the partition key. Query targets a single partition and reads only matching items.

### 3. Forgetting the sort key prefix in Query

**Wrong:**

```go
// Gets ALL items with PK=USER#u001, including profile AND orders
result, _ := client.Query(ctx, &dynamodb.QueryInput{
    TableName:              aws.String(tableName),
    KeyConditionExpression: aws.String("PK = :pk"),
    ExpressionAttributeValues: map[string]types.AttributeValue{
        ":pk": &types.AttributeValueMemberS{Value: "USER#u001"},
    },
})
```

**What happens:** Returns both the PROFILE record and all ORDER records. This might be intended (get everything about a user) or a bug (you only wanted orders).

**Fix:** Add `begins_with(SK, :prefix)` to filter by entity type within the partition.

## Verify What You Learned

```bash
aws dynamodb describe-table --table-name single-table-data \
  --query "Table.KeySchema" --output json
```

Expected: `[{"AttributeName":"PK","KeyType":"HASH"},{"AttributeName":"SK","KeyType":"RANGE"}]`

```bash
aws dynamodb scan --table-name single-table-data --select COUNT \
  --query "Count" --output text
```

Expected: `6` (1 user + 2 orders + 3 order items)

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

You modeled three entity types in a single DynamoDB table using PK/SK overloading. In the next exercise, you will add **Global Secondary Indexes** to support alternate access patterns -- querying by email, querying by order status, and understanding projection types.

## Summary

- **Single-table design** stores multiple entity types in one DynamoDB table using partition key and sort key overloading
- Key pattern: `PK=USER#<id>, SK=PROFILE` for user data; `PK=USER#<id>, SK=ORDER#<id>` for orders; `PK=ORDER#<id>, SK=ITEM#<id>` for order items
- **Query with `begins_with(SK, :prefix)`** retrieves only a specific entity type within a partition
- **Query without SK condition** retrieves all entities for a partition key -- useful for fetching a user profile and all their orders in one request
- Single-table design enables **transactional writes** across entity types (e.g., create a user and their first order atomically)
- Trade-offs: increased key schema complexity, reduced readability when scanning the table, and the need to plan all access patterns before creating the table

## Reference

- [DynamoDB Single-Table Design](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/bp-modeling-nosql.html)
- [DynamoDB Key Design](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/bp-partition-key-design.html)
- [Terraform aws_dynamodb_table](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/dynamodb_table)
- [AWS SDK for Go v2 DynamoDB](https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/dynamodb)

## Additional Resources

- [Alex DeBrie: The DynamoDB Book](https://www.dynamodbbook.com/) -- the definitive guide to DynamoDB data modeling and single-table design
- [DynamoDB Design Patterns](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/bp-modeling-nosql-B.html) -- AWS examples of adjacency list and other single-table patterns
- [Rick Houlihan: Advanced Design Patterns (re:Invent)](https://www.youtube.com/watch?v=HaEPXoXVf2k) -- seminal talk on single-table design patterns
- [DynamoDB Best Practices](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/best-practices.html) -- official AWS best practices for table design and query optimization

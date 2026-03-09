# 38. DynamoDB Global Secondary Index Design

<!--
difficulty: basic
concepts: [global-secondary-index, gsi, projection-types, all-keys-only-include, alternate-access-patterns, query-by-email, query-by-status, eventual-consistency-gsi]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: understand
prerequisites: [37-dynamodb-single-table-design-patterns]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a DynamoDB table with two GSIs in on-demand mode and three Lambda functions. Costs are negligible during testing (~$0.01/hr or less). Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally (for compiling the Lambda binaries)

## Learning Objectives

After completing this exercise, you will be able to:

- **Identify** the purpose of a Global Secondary Index: enabling queries on attributes other than the table's primary key
- **Construct** GSIs with different partition keys and sort keys to support alternate access patterns (query by email, query by status+date)
- **Verify** the behavior of the three GSI projection types (ALL, KEYS_ONLY, INCLUDE) and their impact on storage, cost, and query capabilities
- **Explain** why GSI queries are always eventually consistent and cannot use `ConsistentRead=true`
- **Describe** the storage and throughput implications: GSIs maintain their own copy of projected attributes and consume their own read/write capacity

## Why Global Secondary Indexes

A DynamoDB table's primary key determines how data is partitioned and accessed. If your table has PK=`user_id` and SK=`order_id`, you can efficiently query "get all orders for user X." But what if you need to query "get all orders with status shipped" or "find the user with email alice@example.com"? These access patterns require a different partition key, and that is what a GSI provides.

A GSI creates a separate, automatically maintained projection of your table data with a different primary key structure. When you write to the base table, DynamoDB asynchronously replicates the relevant attributes to the GSI. This asynchronous replication is why GSI reads are **always eventually consistent** -- a detail the DVA-C02 exam tests frequently.

The exam also tests projection types. `ALL` projects every attribute (most flexible but uses the most storage). `KEYS_ONLY` projects only the table and index keys (least storage but you must fetch the full item from the base table if you need other attributes). `INCLUDE` lets you specify exactly which additional attributes to project (middle ground). Choosing the wrong projection type means either wasting storage or making extra GetItem calls to fetch missing attributes.

## Step 1 -- Create the Lambda Function Code

### `seed_data/main.go`

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
		{
			"user_id":    &types.AttributeValueMemberS{Value: "u001"},
			"order_id":   &types.AttributeValueMemberS{Value: "ord-001"},
			"email":      &types.AttributeValueMemberS{Value: "alice@example.com"},
			"status":     &types.AttributeValueMemberS{Value: "shipped"},
			"order_date": &types.AttributeValueMemberS{Value: "2024-01-15"},
			"total":      &types.AttributeValueMemberN{Value: "129.99"},
			"product":    &types.AttributeValueMemberS{Value: "Keyboard"},
		},
		{
			"user_id":    &types.AttributeValueMemberS{Value: "u001"},
			"order_id":   &types.AttributeValueMemberS{Value: "ord-002"},
			"email":      &types.AttributeValueMemberS{Value: "alice@example.com"},
			"status":     &types.AttributeValueMemberS{Value: "pending"},
			"order_date": &types.AttributeValueMemberS{Value: "2024-02-20"},
			"total":      &types.AttributeValueMemberN{Value: "49.50"},
			"product":    &types.AttributeValueMemberS{Value: "Mouse"},
		},
		{
			"user_id":    &types.AttributeValueMemberS{Value: "u002"},
			"order_id":   &types.AttributeValueMemberS{Value: "ord-003"},
			"email":      &types.AttributeValueMemberS{Value: "bob@example.com"},
			"status":     &types.AttributeValueMemberS{Value: "shipped"},
			"order_date": &types.AttributeValueMemberS{Value: "2024-01-20"},
			"total":      &types.AttributeValueMemberN{Value: "299.00"},
			"product":    &types.AttributeValueMemberS{Value: "Monitor"},
		},
		{
			"user_id":    &types.AttributeValueMemberS{Value: "u003"},
			"order_id":   &types.AttributeValueMemberS{Value: "ord-004"},
			"email":      &types.AttributeValueMemberS{Value: "carol@example.com"},
			"status":     &types.AttributeValueMemberS{Value: "pending"},
			"order_date": &types.AttributeValueMemberS{Value: "2024-03-01"},
			"total":      &types.AttributeValueMemberN{Value: "15.00"},
			"product":    &types.AttributeValueMemberS{Value: "Cable"},
		},
	}

	for _, item := range items {
		_, err := client.PutItem(ctx, &dynamodb.PutItemInput{
			TableName: aws.String(tableName), Item: item,
		})
		if err != nil {
			return nil, fmt.Errorf("PutItem: %w", err)
		}
	}

	body, _ := json.Marshal(map[string]interface{}{"message": "Seed data written", "count": len(items)})
	return map[string]interface{}{"statusCode": 200, "body": string(body)}, nil
}

func main() { lambda.Start(handler) }
```

### `query_by_email/main.go`

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
	Email string `json:"email"`
}

func handler(ctx context.Context, event json.RawMessage) (map[string]interface{}, error) {
	var req Request
	json.Unmarshal(event, &req)

	result, err := client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(tableName),
		IndexName:              aws.String("email-index"),
		KeyConditionExpression: aws.String("email = :email"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":email": &types.AttributeValueMemberS{Value: req.Email},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("Query GSI: %w", err)
	}

	items := make([]map[string]interface{}, 0, len(result.Items))
	for _, item := range result.Items {
		var raw map[string]interface{}
		attributevalue.UnmarshalMap(item, &raw)
		items = append(items, raw)
	}

	body, _ := json.Marshal(map[string]interface{}{
		"query":      "by email",
		"index":      "email-index",
		"email":      req.Email,
		"count":      result.Count,
		"items":      items,
		"note":       "GSI queries are always eventually consistent",
	})
	return map[string]interface{}{"statusCode": 200, "body": string(body)}, nil
}

func main() { lambda.Start(handler) }
```

### `query_by_status/main.go`

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
	Status    string `json:"status"`
	DateAfter string `json:"date_after"`
}

func handler(ctx context.Context, event json.RawMessage) (map[string]interface{}, error) {
	var req Request
	json.Unmarshal(event, &req)

	input := &dynamodb.QueryInput{
		TableName:              aws.String(tableName),
		IndexName:              aws.String("status-date-index"),
		KeyConditionExpression: aws.String("#s = :status"),
		ExpressionAttributeNames: map[string]string{"#s": "status"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":status": &types.AttributeValueMemberS{Value: req.Status},
		},
	}

	if req.DateAfter != "" {
		input.KeyConditionExpression = aws.String("#s = :status AND order_date > :date")
		input.ExpressionAttributeValues[":date"] = &types.AttributeValueMemberS{Value: req.DateAfter}
	}

	result, err := client.Query(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("Query GSI: %w", err)
	}

	items := make([]map[string]interface{}, 0, len(result.Items))
	for _, item := range result.Items {
		var raw map[string]interface{}
		attributevalue.UnmarshalMap(item, &raw)
		items = append(items, raw)
	}

	body, _ := json.Marshal(map[string]interface{}{
		"query":      "by status and date",
		"index":      "status-date-index",
		"status":     req.Status,
		"date_after": req.DateAfter,
		"count":      result.Count,
		"items":      items,
	})
	return map[string]interface{}{"statusCode": 200, "body": string(body)}, nil
}

func main() { lambda.Start(handler) }
```

## Step 2 -- Create the Terraform Configuration

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
  default     = "gsi-design"
}
```

### `database.tf`

```hcl
resource "aws_dynamodb_table" "this" {
  name         = "${var.project_name}-orders"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "user_id"
  range_key    = "order_id"

  attribute { name = "user_id";    type = "S" }
  attribute { name = "order_id";   type = "S" }
  attribute { name = "email";      type = "S" }
  attribute { name = "status";     type = "S" }
  attribute { name = "order_date"; type = "S" }

  # GSI 1: Query by email -- projection ALL (all attributes available)
  global_secondary_index {
    name            = "email-index"
    hash_key        = "email"
    projection_type = "ALL"
  }

  # GSI 2: Query by status + order_date -- projection INCLUDE (selected attributes)
  global_secondary_index {
    name               = "status-date-index"
    hash_key           = "status"
    range_key          = "order_date"
    projection_type    = "INCLUDE"
    non_key_attributes = ["user_id", "total", "product"]
  }
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

# IAM must include the GSI ARN for Query on indexes
data "aws_iam_policy_document" "ddb" {
  statement {
    actions   = ["dynamodb:PutItem", "dynamodb:Query", "dynamodb:GetItem"]
    resources = [
      aws_dynamodb_table.this.arn,
      "${aws_dynamodb_table.this.arn}/index/*",
    ]
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
  functions = toset(["seed_data", "query_by_email", "query_by_status"])
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

## Step 3 -- Build and Deploy

```bash
for fn in seed_data query_by_email query_by_status; do
  (cd "$fn" && GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go)
done

terraform init
terraform apply -auto-approve
```

## Step 4 -- Seed Data and Query

```bash
# Seed the table
aws lambda invoke --function-name gsi-design-seed_data \
  --payload '{}' /dev/stdout 2>/dev/null | jq -r '.body' | jq .

# Query by email (uses email-index GSI)
aws lambda invoke --function-name gsi-design-query_by_email \
  --payload '{"email":"alice@example.com"}' /dev/stdout 2>/dev/null | jq -r '.body' | jq .

# Query by status (uses status-date-index GSI)
aws lambda invoke --function-name gsi-design-query_by_status \
  --payload '{"status":"shipped"}' /dev/stdout 2>/dev/null | jq -r '.body' | jq .

# Query by status with date filter
aws lambda invoke --function-name gsi-design-query_by_status \
  --payload '{"status":"shipped","date_after":"2024-01-16"}' /dev/stdout 2>/dev/null | jq -r '.body' | jq .
```

## Common Mistakes

### 1. Forgetting the GSI ARN in IAM policy

**Wrong:**

```hcl
statement {
  actions   = ["dynamodb:Query"]
  resources = [aws_dynamodb_table.this.arn]  # Only table ARN
}
```

**What happens:** `AccessDeniedException` when querying the GSI. DynamoDB treats GSI queries as operations on a sub-resource.

**Fix:** Include `{table-arn}/index/*` or `{table-arn}/index/email-index`:

```hcl
resources = [
  aws_dynamodb_table.this.arn,
  "${aws_dynamodb_table.this.arn}/index/*",
]
```

### 2. Using ConsistentRead on a GSI query

**Wrong:**

```go
result, err := client.Query(ctx, &dynamodb.QueryInput{
    TableName:      aws.String(tableName),
    IndexName:      aws.String("email-index"),
    ConsistentRead: aws.Bool(true),  // Not supported on GSI
    // ...
})
```

**What happens:** `ValidationException: Consistent reads are not supported on global secondary indexes`.

**Fix:** Remove `ConsistentRead` from GSI queries. GSIs only support eventually consistent reads.

### 3. Querying for non-projected attributes

**Wrong:** The `status-date-index` uses `INCLUDE` projection with `["user_id", "total", "product"]`. Querying this index and expecting the `email` attribute:

```go
// email is NOT in the INCLUDE projection for status-date-index
// The query succeeds but email is missing from the results
```

**Fix:** Either change the projection to `ALL`, add `email` to `non_key_attributes`, or make a separate GetItem call to the base table for the missing attribute.

## Verify What You Learned

```bash
aws dynamodb describe-table --table-name gsi-design-orders \
  --query "Table.GlobalSecondaryIndexes[*].{Name:IndexName,PK:KeySchema[0].AttributeName,Projection:Projection.ProjectionType}" \
  --output table
```

Expected: Two GSIs -- `email-index` with `ALL` projection and `status-date-index` with `INCLUDE` projection.

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

You created GSIs for alternate access patterns and learned about projection types. In the next exercise, you will compare **Local Secondary Indexes vs Global Secondary Indexes** -- understanding when to use each, their creation constraints, and consistency trade-offs.

## Summary

- A **Global Secondary Index** enables queries on attributes other than the table's primary key by maintaining a separate, asynchronously replicated copy of the data
- GSI queries are **always eventually consistent** -- `ConsistentRead=true` is not supported
- **Projection types**: `ALL` (full item copy, most storage), `KEYS_ONLY` (minimal storage, requires GetItem for other attributes), `INCLUDE` (specified attributes, balanced)
- GSIs consume their own **read and write capacity**, separate from the base table
- IAM policies must include `{table-arn}/index/*` to allow GSI queries
- GSIs can be **added or removed after table creation** (unlike LSIs)
- A table can have up to **20 GSIs** (soft limit, can be increased)

## Reference

- [DynamoDB Global Secondary Indexes](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/GSI.html)
- [GSI Projection](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/GSI.html#GSI.Projections)
- [Terraform aws_dynamodb_table GSI](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/dynamodb_table#global_secondary_index)
- [AWS SDK for Go v2 DynamoDB Query](https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/dynamodb#Client.Query)

## Additional Resources

- [Best Practices for GSIs](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/bp-indexes-general.html) -- guidelines for choosing partition keys, projection types, and managing GSI capacity
- [GSI Write Sharding](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/bp-indexes-general-sparse.html) -- using sparse indexes and write sharding to optimize GSI performance
- [Querying a GSI](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/GSI.html#GSI.Querying) -- differences between base table and GSI query semantics
- [GSI Backfill](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/GSI.OnlineOps.html) -- how DynamoDB populates a new GSI on an existing table

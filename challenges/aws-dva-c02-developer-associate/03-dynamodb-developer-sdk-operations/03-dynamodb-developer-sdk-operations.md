# 3. DynamoDB Developer SDK Operations

<!--
difficulty: basic
concepts: [dynamodb, partition-key, sort-key, putitem, getitem, query, scan, updateitem, expression-attributes, consistent-reads]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: understand
prerequisites: [none]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a DynamoDB table in on-demand mode and five Lambda functions. Costs are negligible during testing. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally (for compiling the Lambda binaries)
- Basic familiarity with JSON and Go

## Learning Objectives

After completing this exercise, you will be able to:

- **Identify** the components of a DynamoDB primary key (partition key and sort key) and their role in data access
- **Construct** Lambda functions that perform PutItem, GetItem, Query, Scan, and UpdateItem operations using the AWS SDK for Go v2
- **Verify** the difference between eventually consistent and strongly consistent reads using `ConsistentRead`
- **Explain** why Scan with FilterExpression still consumes full table read capacity
- **Describe** the purpose of ExpressionAttributeNames and ExpressionAttributeValues when using reserved words

## Why DynamoDB SDK Operations

DynamoDB is the default database for serverless applications on AWS, and the DVA-C02 exam tests your ability to write correct SDK calls -- not just provision tables. Knowing the difference between Query and Scan is not academic: a Query targets a single partition and reads only the items you need, while a Scan reads every item in the table and applies filters after the fact. In a table with millions of items, that distinction is the difference between a 10ms response and a timeout.

Expression attributes are another area the exam targets heavily. DynamoDB reserves over 570 words (including common names like `status`, `name`, `data`, `count`, and `timestamp`), and using them directly in expressions causes `ValidationException` errors at runtime. The solution is `ExpressionAttributeNames` (aliasing `#s` for `status`) and `ExpressionAttributeValues` (prefixing values with `:val`). Understanding consistent reads is equally important: by default, reads are eventually consistent, and setting `ConsistentRead=true` doubles the RCU cost but guarantees the latest data.

## Step 1 -- Create the Lambda Function Code

Create five Go files, one per DynamoDB operation. Each is a standalone `main` package that compiles to its own `bootstrap` binary.

### `put_item/main.go`

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

type Request struct {
	OrderID     string  `json:"order_id"`
	ItemID      string  `json:"item_id"`
	ProductName string  `json:"product_name"`
	Price       float64 `json:"price"`
	Quantity    int     `json:"quantity"`
	Status      string  `json:"status"`
}

func handler(ctx context.Context, event json.RawMessage) (map[string]interface{}, error) {
	var req Request
	if err := json.Unmarshal(event, &req); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	if req.ProductName == "" {
		req.ProductName = "Unknown"
	}
	if req.Quantity == 0 {
		req.Quantity = 1
	}
	if req.Status == "" {
		req.Status = "pending"
	}

	_, err := client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(tableName),
		Item: map[string]types.AttributeValue{
			"order_id":     &types.AttributeValueMemberS{Value: req.OrderID},
			"item_id":      &types.AttributeValueMemberS{Value: req.ItemID},
			"product_name": &types.AttributeValueMemberS{Value: req.ProductName},
			"price":        &types.AttributeValueMemberN{Value: fmt.Sprintf("%.2f", req.Price)},
			"quantity":     &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", req.Quantity)},
			"status":       &types.AttributeValueMemberS{Value: req.Status},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("PutItem: %w", err)
	}

	body, _ := json.Marshal(map[string]interface{}{
		"message": "Item created",
		"item": map[string]string{
			"order_id": req.OrderID, "item_id": req.ItemID,
			"product_name": req.ProductName, "price": fmt.Sprintf("%.2f", req.Price),
			"quantity": fmt.Sprintf("%d", req.Quantity), "status": req.Status,
		},
	})
	return map[string]interface{}{"statusCode": 200, "body": string(body)}, nil
}

func main() { lambda.Start(handler) }
```

### `get_item/main.go`

Retrieves a single item with optional consistent read:

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
	OrderID        string `json:"order_id"`
	ItemID         string `json:"item_id"`
	ConsistentRead bool   `json:"consistent_read"`
}

func handler(ctx context.Context, event json.RawMessage) (map[string]interface{}, error) {
	var req Request
	json.Unmarshal(event, &req)

	result, err := client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(tableName),
		Key: map[string]types.AttributeValue{
			"order_id": &types.AttributeValueMemberS{Value: req.OrderID},
			"item_id":  &types.AttributeValueMemberS{Value: req.ItemID},
		},
		ConsistentRead: aws.Bool(req.ConsistentRead),
	})
	if err != nil {
		return nil, fmt.Errorf("GetItem: %w", err)
	}

	var item map[string]string
	if result.Item != nil {
		var raw map[string]interface{}
		attributevalue.UnmarshalMap(result.Item, &raw)
		item = make(map[string]string)
		for k, v := range raw {
			item[k] = fmt.Sprintf("%v", v)
		}
	}

	rcuNote := "1x RCU for eventually consistent"
	if req.ConsistentRead {
		rcuNote = "2x RCU for consistent"
	}

	body, _ := json.Marshal(map[string]interface{}{
		"consistent_read": req.ConsistentRead,
		"item":            item,
		"rcu_note":        rcuNote,
	})
	return map[string]interface{}{"statusCode": 200, "body": string(body)}, nil
}

func main() { lambda.Start(handler) }
```

### `query_items/main.go`

Queries all items within a single partition:

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

	result, err := client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(tableName),
		KeyConditionExpression: aws.String("order_id = :pk"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk": &types.AttributeValueMemberS{Value: req.OrderID},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("Query: %w", err)
	}

	items := make([]map[string]string, 0, len(result.Items))
	for _, item := range result.Items {
		var raw map[string]interface{}
		attributevalue.UnmarshalMap(item, &raw)
		m := make(map[string]string)
		for k, v := range raw {
			m[k] = fmt.Sprintf("%v", v)
		}
		items = append(items, m)
	}

	body, _ := json.Marshal(map[string]interface{}{
		"operation":     "Query",
		"order_id":      req.OrderID,
		"count":         result.Count,
		"scanned_count": result.ScannedCount,
		"items":         items,
	})
	return map[string]interface{}{"statusCode": 200, "body": string(body)}, nil
}

func main() { lambda.Start(handler) }
```

### `scan_items/main.go`

Scans the table with optional FilterExpression:

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
	FilterStatus string `json:"filter_status"`
}

// FilterExpression is applied AFTER the read. DynamoDB charges RCU for the
// full scan -- the filter only reduces the result set, not the data read.
func handler(ctx context.Context, event json.RawMessage) (map[string]interface{}, error) {
	var req Request
	json.Unmarshal(event, &req)

	input := &dynamodb.ScanInput{
		TableName: aws.String(tableName),
	}
	if req.FilterStatus != "" {
		input.FilterExpression = aws.String("#s = :status")
		input.ExpressionAttributeNames = map[string]string{"#s": "status"}
		input.ExpressionAttributeValues = map[string]types.AttributeValue{
			":status": &types.AttributeValueMemberS{Value: req.FilterStatus},
		}
	}

	result, err := client.Scan(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("Scan: %w", err)
	}

	items := make([]map[string]string, 0, len(result.Items))
	for _, item := range result.Items {
		var raw map[string]interface{}
		attributevalue.UnmarshalMap(item, &raw)
		m := make(map[string]string)
		for k, v := range raw {
			m[k] = fmt.Sprintf("%v", v)
		}
		items = append(items, m)
	}

	body, _ := json.Marshal(map[string]interface{}{
		"operation":     "Scan",
		"filter_status": req.FilterStatus,
		"count":         result.Count,
		"scanned_count": result.ScannedCount,
		"note":          "ScannedCount = items READ (charged). Count = items RETURNED after filter.",
		"items":         items,
	})
	return map[string]interface{}{"statusCode": 200, "body": string(body)}, nil
}

func main() { lambda.Start(handler) }
```

### `update_item/main.go`

Updates an item using expression attributes for reserved words:

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
	OrderID   string `json:"order_id"`
	ItemID    string `json:"item_id"`
	NewStatus string `json:"new_status"`
	Quantity  int    `json:"quantity"`
}

// Uses ExpressionAttributeNames because "status" is a reserved word.
func handler(ctx context.Context, event json.RawMessage) (map[string]interface{}, error) {
	var req Request
	json.Unmarshal(event, &req)

	if req.NewStatus == "" {
		req.NewStatus = "shipped"
	}
	if req.Quantity == 0 {
		req.Quantity = 1
	}

	result, err := client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(tableName),
		Key: map[string]types.AttributeValue{
			"order_id": &types.AttributeValueMemberS{Value: req.OrderID},
			"item_id":  &types.AttributeValueMemberS{Value: req.ItemID},
		},
		UpdateExpression:          aws.String("SET #s = :new_status, quantity = :qty"),
		ExpressionAttributeNames: map[string]string{"#s": "status"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":new_status": &types.AttributeValueMemberS{Value: req.NewStatus},
			":qty":        &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", req.Quantity)},
		},
		ReturnValues: types.ReturnValueAllNew,
	})
	if err != nil {
		return nil, fmt.Errorf("UpdateItem: %w", err)
	}

	var raw map[string]interface{}
	attributevalue.UnmarshalMap(result.Attributes, &raw)
	item := make(map[string]string)
	for k, v := range raw {
		item[k] = fmt.Sprintf("%v", v)
	}

	body, _ := json.Marshal(map[string]interface{}{
		"operation":    "UpdateItem",
		"updated_item": item,
	})
	return map[string]interface{}{"statusCode": 200, "body": string(body)}, nil
}

func main() { lambda.Start(handler) }
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
  default     = "dynamodb-sdk-demo"
}
```

### `database.tf`

```hcl
# -- DynamoDB table: composite key (PK + SK), on-demand billing --
resource "aws_dynamodb_table" "orders" {
  name         = "orders"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "order_id"
  range_key    = "item_id"
  attribute { name = "order_id"; type = "S" }
  attribute { name = "item_id"; type = "S" }
  tags = { Name = "${var.project_name}-orders-table" }
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

# Least-privilege: only the 5 operations needed, scoped to this table
data "aws_iam_policy_document" "ddb" {
  statement {
    actions   = ["dynamodb:PutItem", "dynamodb:GetItem", "dynamodb:Query", "dynamodb:Scan", "dynamodb:UpdateItem"]
    resources = [aws_dynamodb_table.orders.arn]
  }
}

resource "aws_iam_policy" "ddb" {
  name   = "dynamodb-orders-access"
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
  functions = toset(["put_item", "get_item", "query_items", "scan_items", "update_item"])
}

resource "null_resource" "go_build" {
  for_each = local.functions
  triggers = {
    source_hash = filebase64sha256("${path.module}/${each.key}/main.go")
  }
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
resource "aws_lambda_function" "fn" {
  for_each         = local.functions
  function_name    = "ddb-${each.key}"
  filename         = data.archive_file.fn[each.key].output_path
  source_code_hash = data.archive_file.fn[each.key].output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.this.arn
  timeout          = 15
  environment { variables = { TABLE_NAME = aws_dynamodb_table.orders.name } }
  depends_on = [aws_iam_role_policy_attachment.basic, aws_iam_role_policy_attachment.ddb, aws_cloudwatch_log_group.fn]
}
```

### `monitoring.tf`

```hcl
resource "aws_cloudwatch_log_group" "fn" {
  for_each          = local.functions
  name              = "/aws/lambda/ddb-${each.key}"
  retention_in_days = 1
}
```

### `outputs.tf`

```hcl
output "table_name"     { value = aws_dynamodb_table.orders.name }
output "function_names" { value = { for k, v in aws_lambda_function.fn : k => v.function_name } }
```

## Step 3 -- Build and Apply

```bash
# Build all five Go binaries
for fn in put_item get_item query_items scan_items update_item; do
  (cd "$fn" && GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go)
done

terraform init
terraform apply -auto-approve
```

Terraform will create 15 resources: the DynamoDB table, IAM role, two IAM policy attachments, one IAM policy, five CloudWatch log groups, and five Lambda functions.

## Step 4 -- Insert Test Data with PutItem

```bash
aws lambda invoke --function-name ddb-put_item \
  --payload '{"order_id":"ORD-001","item_id":"ITEM-A","product_name":"Keyboard","price":79.99,"quantity":1,"status":"pending"}' \
  /dev/stdout 2>/dev/null | jq -r '.body' | jq .

aws lambda invoke --function-name ddb-put_item \
  --payload '{"order_id":"ORD-001","item_id":"ITEM-B","product_name":"Mouse","price":29.99,"quantity":2,"status":"shipped"}' \
  /dev/stdout 2>/dev/null | jq -r '.body' | jq .

aws lambda invoke --function-name ddb-put_item \
  --payload '{"order_id":"ORD-002","item_id":"ITEM-A","product_name":"Monitor","price":349.99,"quantity":1,"status":"pending"}' \
  /dev/stdout 2>/dev/null | jq -r '.body' | jq .
```

## Step 5 -- GetItem: Eventually Consistent vs Strongly Consistent

```bash
# Eventually consistent (default) -- 1x RCU
aws lambda invoke --function-name ddb-get_item \
  --payload '{"order_id":"ORD-001","item_id":"ITEM-A","consistent_read":false}' \
  /dev/stdout 2>/dev/null | jq -r '.body' | jq .

# Strongly consistent -- 2x RCU, guaranteed latest data
aws lambda invoke --function-name ddb-get_item \
  --payload '{"order_id":"ORD-001","item_id":"ITEM-A","consistent_read":true}' \
  /dev/stdout 2>/dev/null | jq -r '.body' | jq .
```

Both return the same item. The difference is cost and consistency guarantee.

## Step 6 -- Query by Partition Key

```bash
aws lambda invoke --function-name ddb-query_items \
  --payload '{"order_id":"ORD-001"}' \
  /dev/stdout 2>/dev/null | jq -r '.body' | jq .
```

Expected: `count: 2` (ITEM-A and ITEM-B). Query reads only the ORD-001 partition -- it never touches ORD-002.

## Step 7 -- Scan with FilterExpression

```bash
aws lambda invoke --function-name ddb-scan_items \
  --payload '{"filter_status":"pending"}' \
  /dev/stdout 2>/dev/null | jq -r '.body' | jq .
```

Expected: `count: 2` (two pending items), but `scanned_count: 3`. DynamoDB read all three items and charged RCU for all three, then filtered the result.

## Step 8 -- UpdateItem with Expression Attributes

```bash
aws lambda invoke --function-name ddb-update_item \
  --payload '{"order_id":"ORD-001","item_id":"ITEM-A","new_status":"shipped","quantity":3}' \
  /dev/stdout 2>/dev/null | jq -r '.body' | jq .
```

Expected: updated item with `status: "shipped"` and `quantity: 3`.

## Common Mistakes

### 1. Scan with FilterExpression still consumes full table RCU

**Wrong assumption:**

```go
// "This should only read pending items, right?"
result, _ := client.Scan(ctx, &dynamodb.ScanInput{
    TableName:        aws.String(tableName),
    FilterExpression: aws.String("#s = :status"),
    ExpressionAttributeNames:  map[string]string{"#s": "status"},
    ExpressionAttributeValues: map[string]types.AttributeValue{
        ":status": &types.AttributeValueMemberS{Value: "pending"},
    },
})
// ScannedCount: 10000 (ALL items read), Count: 50 (only 50 returned)
```

**What happens:** DynamoDB reads and charges for all 10,000 items. The filter only reduces the response, not the read cost.

**Fix -- use Query with a GSI for frequent filter patterns:**

```hcl
resource "aws_dynamodb_table" "orders" {
  # ... existing config ...
  global_secondary_index {
    name            = "status-index"
    hash_key        = "status"
    projection_type = "ALL"
  }
}
```

### 2. Using reserved words without ExpressionAttributeNames

**Wrong:**

```go
result, err := client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
    TableName: aws.String(tableName),
    Key: map[string]types.AttributeValue{
        "order_id": &types.AttributeValueMemberS{Value: "ORD-001"},
        "item_id":  &types.AttributeValueMemberS{Value: "ITEM-A"},
    },
    UpdateExpression: aws.String("SET status = :val"),
    ExpressionAttributeValues: map[string]types.AttributeValue{
        ":val": &types.AttributeValueMemberS{Value: "shipped"},
    },
})
```

**What happens:** `ValidationException: Attribute name is a reserved keyword; reserved keyword: status`

**Fix -- alias the reserved word:**

```go
result, err := client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
    TableName: aws.String(tableName),
    Key: map[string]types.AttributeValue{
        "order_id": &types.AttributeValueMemberS{Value: "ORD-001"},
        "item_id":  &types.AttributeValueMemberS{Value: "ITEM-A"},
    },
    UpdateExpression:         aws.String("SET #s = :val"),
    ExpressionAttributeNames: map[string]string{"#s": "status"},
    ExpressionAttributeValues: map[string]types.AttributeValue{
        ":val": &types.AttributeValueMemberS{Value: "shipped"},
    },
})
```

### 3. Forgetting billing_mode = "PAY_PER_REQUEST"

**Wrong:**

```hcl
resource "aws_dynamodb_table" "orders" {
  name     = "orders"
  hash_key = "order_id"
  # billing_mode defaults to PROVISIONED with 0 RCU/WCU
}
```

**What happens:** First write fails with `ProvisionedThroughputExceededException`.

**Fix:**

```hcl
resource "aws_dynamodb_table" "orders" {
  name         = "orders"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "order_id"
}
```

## Verify What You Learned

```bash
aws dynamodb describe-table --table-name orders --query "Table.BillingModeSummary.BillingMode" --output text
```

Expected: `PAY_PER_REQUEST`

```bash
aws dynamodb describe-table --table-name orders --query "Table.KeySchema" --output json
```

Expected: `[{"AttributeName":"order_id","KeyType":"HASH"},{"AttributeName":"item_id","KeyType":"RANGE"}]`

```bash
aws dynamodb scan --table-name orders --select COUNT --query "Count" --output text
```

Expected: `3`

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

You performed the five core DynamoDB operations using the AWS SDK for Go v2 and learned why expression attributes and consistent reads matter. In the next exercise, you will explore **SQS and SNS integration patterns** -- building event-driven architectures where Lambda functions consume messages from queues and fan out notifications through topics.

## Summary

- A DynamoDB **composite primary key** consists of a partition key (HASH) and sort key (RANGE) that together uniquely identify an item
- **Query** targets a single partition and reads only matching items -- efficient for known access patterns
- **Scan** reads the entire table; `FilterExpression` reduces the response but not the read cost or RCU consumption
- **ConsistentRead=true** guarantees the latest data but consumes 2x the RCU of an eventually consistent read
- DynamoDB has **570+ reserved words** (including `status`, `name`, `data`) -- use `ExpressionAttributeNames` to alias them
- **PAY_PER_REQUEST** billing mode avoids capacity planning and `ProvisionedThroughputExceededException`

## Reference

- [DynamoDB Developer Guide](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/Introduction.html)
- [DynamoDB Reserved Words](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/ReservedWords.html)
- [Terraform aws_dynamodb_table Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/dynamodb_table)
- [Terraform aws_lambda_function Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_function)

## Additional Resources

- [DynamoDB Query vs Scan](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/bp-query-scan.html) -- best practices for choosing between Query and Scan operations
- [DynamoDB Read Consistency](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/HowItWorks.ReadConsistency.html) -- how eventually consistent and strongly consistent reads work
- [DynamoDB Expressions](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/Expressions.html) -- complete guide to condition, projection, update, and filter expressions
- [AWS SDK for Go v2 DynamoDB](https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/dynamodb) -- Go SDK reference for DynamoDB operations

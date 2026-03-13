# 41. DynamoDB Batch Operations

<!--
difficulty: intermediate
concepts: [batch-write-item, batch-get-item, unprocessed-items, unprocessed-keys, exponential-backoff, 25-item-write-limit, 100-item-read-limit, partial-failure]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: analyze, implement
prerequisites: [03-dynamodb-developer-sdk-operations]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a DynamoDB table and Lambda functions. Costs are negligible during testing (~$0.01/hr or less). Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Analyze** the difference between batch operations and transactions: batches are for throughput (partial success allowed), transactions are for atomicity (all-or-nothing)
2. **Implement** BatchWriteItem (up to 25 items) and BatchGetItem (up to 100 items) with proper UnprocessedItems/UnprocessedKeys handling
3. **Configure** exponential backoff retry logic for unprocessed items returned by DynamoDB when capacity is insufficient
4. **Differentiate** between the 25-item write limit, 100-item read limit, and the 16 MB response size limit for batch operations
5. **Debug** the common mistake of not checking the UnprocessedItems response field, which causes silent data loss

## Why This Matters

Batch operations are the bulk-loading workhorse of DynamoDB. Unlike transactions, batches do not provide atomicity -- individual items within a batch can succeed or fail independently. When DynamoDB cannot process all items in a batch (due to throughput limits or internal errors), it returns the unprocessed items in the response. Your application must check for these and retry them with exponential backoff.

The DVA-C02 exam tests batch operations in two ways. First, the limits: BatchWriteItem accepts up to 25 PutItem or DeleteItem requests (no UpdateItem), each item up to 400 KB, and the total request size must be under 16 MB. BatchGetItem accepts up to 100 items, each up to 400 KB, and the total response size must be under 16 MB. Second, the retry pattern: if you ignore `UnprocessedItems` in the BatchWriteItem response or `UnprocessedKeys` in the BatchGetItem response, items are silently dropped. This is the most common mistake and the most common exam question on batch operations.

## Building Blocks

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
  default     = "batch-ops"
}
```

### `database.tf`

```hcl
resource "aws_dynamodb_table" "this" {
  name         = "${var.project_name}-products"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "product_id"

  attribute { name = "product_id"; type = "S" }
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

# =======================================================
# TODO 1 -- IAM Policy with batch permissions (iam.tf)
# =======================================================
# Requirements:
#   - Grant dynamodb:BatchWriteItem and dynamodb:BatchGetItem
#   - Also grant dynamodb:PutItem and dynamodb:GetItem for
#     comparison operations
#   - Scope to the table ARN


# =======================================================
# TODO 2 -- BatchWriteItem Lambda (lambda.tf)
# =======================================================
# Create a batch_write Lambda that:
#   - Writes 25 products in a single BatchWriteItem call
#   - Checks UnprocessedItems in the response
#   - Retries unprocessed items with exponential backoff
#   - Reports how many items were processed vs unprocessed


# =======================================================
# TODO 3 -- BatchGetItem Lambda (lambda.tf)
# =======================================================
# Create a batch_read Lambda that:
#   - Reads up to 100 items in a single BatchGetItem call
#   - Checks UnprocessedKeys in the response
#   - Retries unprocessed keys with exponential backoff
#   - Reports the results
```

### `outputs.tf`

```hcl
output "table_name" { value = aws_dynamodb_table.this.name }
```

### Lambda Function: BatchWriteItem with retry

### `batch_write/main.go`

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"time"

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
	// Build 25 write requests (maximum per BatchWriteItem call)
	writeRequests := make([]types.WriteRequest, 0, 25)
	for i := 1; i <= 25; i++ {
		writeRequests = append(writeRequests, types.WriteRequest{
			PutRequest: &types.PutRequest{
				Item: map[string]types.AttributeValue{
					"product_id": &types.AttributeValueMemberS{Value: fmt.Sprintf("PROD-%03d", i)},
					"name":       &types.AttributeValueMemberS{Value: fmt.Sprintf("Product %d", i)},
					"price":      &types.AttributeValueMemberN{Value: fmt.Sprintf("%.2f", float64(i)*9.99)},
					"category":   &types.AttributeValueMemberS{Value: "electronics"},
				},
			},
		})
	}

	input := &dynamodb.BatchWriteItemInput{
		RequestItems: map[string][]types.WriteRequest{
			tableName: writeRequests,
		},
	}

	totalProcessed := 0
	retryCount := 0
	maxRetries := 5

	for {
		result, err := client.BatchWriteItem(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("BatchWriteItem: %w", err)
		}

		batchSize := len(input.RequestItems[tableName])
		unprocessedCount := 0
		if result.UnprocessedItems != nil {
			if items, ok := result.UnprocessedItems[tableName]; ok {
				unprocessedCount = len(items)
			}
		}
		totalProcessed += batchSize - unprocessedCount

		// Check for unprocessed items
		if len(result.UnprocessedItems) == 0 {
			break
		}

		retryCount++
		if retryCount > maxRetries {
			return nil, fmt.Errorf("exceeded max retries, %d items still unprocessed", unprocessedCount)
		}

		// Exponential backoff: 100ms, 200ms, 400ms, 800ms, 1600ms
		backoff := time.Duration(math.Pow(2, float64(retryCount-1))) * 100 * time.Millisecond
		fmt.Printf("Retry %d: %d unprocessed items, backing off %v\n", retryCount, unprocessedCount, backoff)
		time.Sleep(backoff)

		// Retry only the unprocessed items
		input = &dynamodb.BatchWriteItemInput{
			RequestItems: result.UnprocessedItems,
		}
	}

	body, _ := json.Marshal(map[string]interface{}{
		"message":         "Batch write complete",
		"total_processed": totalProcessed,
		"retry_count":     retryCount,
	})
	return map[string]interface{}{"statusCode": 200, "body": string(body)}, nil
}

func main() { lambda.Start(handler) }
```

### Lambda Function: BatchGetItem with retry

### `batch_read/main.go`

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"time"

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
	ProductIDs []string `json:"product_ids"`
}

func handler(ctx context.Context, event json.RawMessage) (map[string]interface{}, error) {
	var req Request
	json.Unmarshal(event, &req)

	if len(req.ProductIDs) == 0 {
		// Default: read first 10 products
		for i := 1; i <= 10; i++ {
			req.ProductIDs = append(req.ProductIDs, fmt.Sprintf("PROD-%03d", i))
		}
	}

	keys := make([]map[string]types.AttributeValue, 0, len(req.ProductIDs))
	for _, id := range req.ProductIDs {
		keys = append(keys, map[string]types.AttributeValue{
			"product_id": &types.AttributeValueMemberS{Value: id},
		})
	}

	input := &dynamodb.BatchGetItemInput{
		RequestItems: map[string]types.KeysAndAttributes{
			tableName: {Keys: keys},
		},
	}

	allItems := make([]map[string]interface{}, 0)
	retryCount := 0
	maxRetries := 5

	for {
		result, err := client.BatchGetItem(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("BatchGetItem: %w", err)
		}

		if items, ok := result.Responses[tableName]; ok {
			for _, item := range items {
				var raw map[string]interface{}
				attributevalue.UnmarshalMap(item, &raw)
				allItems = append(allItems, raw)
			}
		}

		// Check for unprocessed keys
		if len(result.UnprocessedKeys) == 0 {
			break
		}

		retryCount++
		if retryCount > maxRetries {
			unprocessedCount := len(result.UnprocessedKeys[tableName].Keys)
			return nil, fmt.Errorf("exceeded max retries, %d keys still unprocessed", unprocessedCount)
		}

		backoff := time.Duration(math.Pow(2, float64(retryCount-1))) * 100 * time.Millisecond
		fmt.Printf("Retry %d: unprocessed keys, backing off %v\n", retryCount, backoff)
		time.Sleep(backoff)

		input = &dynamodb.BatchGetItemInput{
			RequestItems: result.UnprocessedKeys,
		}
	}

	body, _ := json.Marshal(map[string]interface{}{
		"message":     "Batch read complete",
		"item_count":  len(allItems),
		"retry_count": retryCount,
		"items":       allItems,
	})
	return map[string]interface{}{"statusCode": 200, "body": string(body)}, nil
}

func main() { lambda.Start(handler) }
```

## Spot the Bug

A developer uses BatchWriteItem to bulk-load 25 items. The function reports success, but only 20 items appear in the table. **What is wrong?**

```go
result, err := client.BatchWriteItem(ctx, input)
if err != nil {
    return nil, fmt.Errorf("BatchWriteItem: %w", err)
}

// BUG: No check for UnprocessedItems
body, _ := json.Marshal(map[string]interface{}{
    "message": "All items written successfully",
})
return map[string]interface{}{"statusCode": 200, "body": string(body)}, nil
```

<details>
<summary>Explain the bug</summary>

The code does not check `result.UnprocessedItems`. BatchWriteItem can return a successful response (`err == nil`) even when some items were not written. DynamoDB returns the unprocessed items in the `UnprocessedItems` field of the response. If you do not check this field and retry, those items are silently dropped.

This happens when DynamoDB cannot process all items due to insufficient provisioned throughput or internal capacity limits. With on-demand billing, it is less common but still possible during sudden traffic spikes.

The fix:

```go
result, err := client.BatchWriteItem(ctx, input)
if err != nil {
    return nil, fmt.Errorf("BatchWriteItem: %w", err)
}

// MUST check for unprocessed items
for len(result.UnprocessedItems) > 0 {
    time.Sleep(time.Second) // Exponential backoff recommended
    result, err = client.BatchWriteItem(ctx, &dynamodb.BatchWriteItemInput{
        RequestItems: result.UnprocessedItems,
    })
    if err != nil {
        return nil, fmt.Errorf("retry BatchWriteItem: %w", err)
    }
}
```

This is one of the most commonly tested DynamoDB concepts on the DVA-C02 exam: **always check UnprocessedItems (write) and UnprocessedKeys (read) in batch operation responses.**

</details>

## Solutions

<details>
<summary>TODO 1 -- IAM Policy</summary>

### `iam.tf`

```hcl
data "aws_iam_policy_document" "ddb" {
  statement {
    actions = [
      "dynamodb:PutItem",
      "dynamodb:GetItem",
      "dynamodb:BatchWriteItem",
      "dynamodb:BatchGetItem",
    ]
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

</details>

<details>
<summary>TODO 2 + TODO 3 -- Lambda Functions</summary>

### `lambda.tf`

```hcl
locals {
  functions = toset(["batch_write", "batch_read"])
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
  timeout          = 30
  environment { variables = { TABLE_NAME = aws_dynamodb_table.this.name } }
  depends_on = [aws_iam_role_policy_attachment.basic, aws_iam_role_policy_attachment.ddb, aws_cloudwatch_log_group.fn]
}
```

</details>

## Verify What You Learned

```bash
# Write 25 items
aws lambda invoke --function-name batch-ops-batch_write \
  --payload '{}' /dev/stdout 2>/dev/null | jq -r '.body' | jq .

# Read 10 items
aws lambda invoke --function-name batch-ops-batch_read \
  --payload '{}' /dev/stdout 2>/dev/null | jq -r '.body' | jq .

# Verify all 25 items exist
aws dynamodb scan --table-name batch-ops-products --select COUNT \
  --query "Count" --output text

terraform plan
```

Expected: 25 items processed, 10 items read, total count 25.

## Cleanup

```bash
terraform destroy -auto-approve
terraform state list
```

Expected: no output (empty state).

## What's Next

You implemented batch operations with proper retry handling. In the next exercise, you will configure **DynamoDB TTL and item expiration** -- automatically deleting items after a specified time and processing expired items via DynamoDB Streams.

## Summary

- **BatchWriteItem** processes up to **25** PutItem or DeleteItem requests (no UpdateItem) in a single call
- **BatchGetItem** retrieves up to **100** items in a single call
- Batch operations are **not atomic**: individual items can succeed or fail independently
- **Always check `UnprocessedItems`** (write) and **`UnprocessedKeys`** (read) -- ignoring them causes silent data loss
- Use **exponential backoff** when retrying unprocessed items to avoid overwhelming DynamoDB
- Total request size limit: **16 MB** for both BatchWriteItem and BatchGetItem
- Individual item size limit: **400 KB** per item
- Batch operations cost the same WCU/RCU as individual operations (no discount, but fewer API calls)

## Reference

- [DynamoDB BatchWriteItem](https://docs.aws.amazon.com/amazondynamodb/latest/APIReference/API_BatchWriteItem.html)
- [DynamoDB BatchGetItem](https://docs.aws.amazon.com/amazondynamodb/latest/APIReference/API_BatchGetItem.html)
- [Error Handling for Batch Operations](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/Programming.Errors.html#Programming.Errors.BatchOperations)
- [AWS SDK for Go v2 DynamoDB](https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/dynamodb)

## Additional Resources

- [Batch Operations Best Practices](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/bp-batch-operations.html) -- optimizing batch sizes and retry strategies
- [Exponential Backoff and Jitter](https://aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter/) -- why jitter improves retry performance in distributed systems
- [DynamoDB Limits](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/ServiceQuotas.html) -- item size, request size, and throughput limits
- [Parallel Scan](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/Scan.html#Scan.ParallelScan) -- an alternative to BatchGetItem for large-scale data retrieval

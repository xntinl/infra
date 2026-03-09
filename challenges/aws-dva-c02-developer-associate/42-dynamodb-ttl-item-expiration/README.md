# 42. DynamoDB TTL and Item Expiration

<!--
difficulty: intermediate
concepts: [dynamodb-ttl, time-to-live, epoch-seconds, item-expiration, ttl-streams, expired-item-processing, ttl-attribute-type]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: analyze, implement
prerequisites: [03-dynamodb-developer-sdk-operations, 12-dynamodb-streams-lambda-trigger-patterns]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a DynamoDB table with TTL and streams enabled, plus Lambda functions. Costs are negligible during testing (~$0.01/hr or less). Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Analyze** how DynamoDB TTL works: items are deleted asynchronously within 48 hours after the TTL attribute timestamp passes, at no additional cost
2. **Implement** TTL on a DynamoDB table by enabling the `ttl` block in Terraform and writing items with an epoch-second expiration timestamp
3. **Configure** DynamoDB Streams to capture TTL-deleted items and process them in a Lambda function for archival or cleanup tasks
4. **Differentiate** between TTL deletions (free, asynchronous, up to 48 hours delay) and explicit DeleteItem calls (immediate, costs WCU)
5. **Debug** the common mistake of storing the TTL attribute as a String instead of a Number, which causes TTL to silently ignore the item

## Why This Matters

DynamoDB TTL automatically deletes expired items without consuming write capacity units. This is essential for session stores, temporary tokens, audit logs with retention policies, and any data with a natural expiration. The deletion is free but asynchronous -- items may persist for up to 48 hours after expiration.

The DVA-C02 exam tests TTL in three ways. First, the attribute format: the TTL attribute must be a **Number** type containing the expiration time as **Unix epoch seconds** (not milliseconds, not ISO 8601 strings). If the attribute is a String, DynamoDB silently ignores it and the item never expires. Second, the integration with DynamoDB Streams: TTL-deleted items appear in the stream as REMOVE events with `userIdentity.type = "Service"` and `userIdentity.principalId = "dynamodb.amazonaws.com"`, which lets you distinguish TTL deletions from explicit deletes. Third, the deletion timing: items are not deleted immediately at the TTL timestamp -- there can be a delay of up to 48 hours, and expired items may still appear in Query and Scan results until actually deleted.

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
  default     = "ttl-lab"
}
```

### `database.tf`

```hcl
# =======================================================
# TODO 1 -- DynamoDB Table with TTL Enabled (database.tf)
# =======================================================
# Requirements:
#   - Create a table with hash_key = "session_id"
#   - billing_mode = "PAY_PER_REQUEST"
#   - Enable TTL with attribute_name = "expires_at"
#   - Enable streams with stream_view_type = "NEW_AND_OLD_IMAGES"
#     (to capture TTL deletions in the stream)
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/dynamodb_table#ttl


# =======================================================
# TODO 2 -- Lambda to Write Items with TTL (lambda.tf)
# =======================================================
# Create a write_session Lambda that:
#   - Writes a session item with a TTL attribute "expires_at"
#   - The TTL value must be a Number (epoch seconds), NOT a String
#   - Example: expires_at = time.Now().Add(5*time.Minute).Unix()
#   - Also write an item with a deliberately wrong TTL (string type)
#     to demonstrate the "silent ignore" behavior


# =======================================================
# TODO 3 -- Lambda to Process Expired Items (Stream) (lambda.tf)
# =======================================================
# Create a process_expired Lambda triggered by DynamoDB Streams:
#   - Filter for REMOVE events only
#   - Check record.UserIdentity to distinguish TTL deletes from
#     explicit deletes
#   - Log the expired session details for archival
#   - Configure event source mapping with filter_criteria for REMOVE
```

### Lambda: Write sessions with TTL

### `write_session/main.go`

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
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

type Request struct {
	SessionID string `json:"session_id"`
	UserID    string `json:"user_id"`
	TTLMinutes int   `json:"ttl_minutes"`
}

func handler(ctx context.Context, event json.RawMessage) (map[string]interface{}, error) {
	var req Request
	json.Unmarshal(event, &req)
	if req.TTLMinutes == 0 {
		req.TTLMinutes = 5
	}

	expiresAt := time.Now().Add(time.Duration(req.TTLMinutes) * time.Minute).Unix()

	// Correct: TTL as Number (epoch seconds)
	_, err := client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(tableName),
		Item: map[string]types.AttributeValue{
			"session_id": &types.AttributeValueMemberS{Value: req.SessionID},
			"user_id":    &types.AttributeValueMemberS{Value: req.UserID},
			"created_at": &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", time.Now().Unix())},
			"expires_at": &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", expiresAt)},
			"data":       &types.AttributeValueMemberS{Value: "session data here"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("PutItem: %w", err)
	}

	body, _ := json.Marshal(map[string]interface{}{
		"message":    "Session created",
		"session_id": req.SessionID,
		"expires_at": expiresAt,
		"ttl_type":   "Number (correct)",
		"note":       fmt.Sprintf("Item will be deleted ~%d minutes from now (may take up to 48 hours)", req.TTLMinutes),
	})
	return map[string]interface{}{"statusCode": 200, "body": string(body)}, nil
}

func main() { lambda.Start(handler) }
```

### Lambda: Process expired items from stream

### `process_expired/main.go`

```go
package main

import (
	"context"
	"fmt"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

func handler(ctx context.Context, event events.DynamoDBEvent) error {
	for _, record := range event.Records {
		if record.EventName != "REMOVE" {
			continue
		}

		oldImage := record.Change.OldImage
		sessionID := "unknown"
		if v, ok := oldImage["session_id"]; ok {
			sessionID = v.String()
		}
		userID := "unknown"
		if v, ok := oldImage["user_id"]; ok {
			userID = v.String()
		}

		// Check if this was a TTL deletion or explicit delete
		isTTLDelete := false
		if record.UserIdentity != nil {
			isTTLDelete = record.UserIdentity.Type == "Service" &&
				record.UserIdentity.PrincipalID == "dynamodb.amazonaws.com"
		}

		if isTTLDelete {
			fmt.Printf("TTL expired: session=%s user=%s\n", sessionID, userID)
			// Archive to S3, send notification, update analytics, etc.
		} else {
			fmt.Printf("Explicit delete: session=%s user=%s\n", sessionID, userID)
		}
	}
	return nil
}

func main() { lambda.Start(handler) }
```

## Spot the Bug

A developer enabled TTL on a DynamoDB table, but items never expire. The TTL attribute is set and the table configuration shows TTL is enabled. **What is wrong?**

```go
_, err := client.PutItem(ctx, &dynamodb.PutItemInput{
    TableName: aws.String(tableName),
    Item: map[string]types.AttributeValue{
        "session_id": &types.AttributeValueMemberS{Value: "sess-001"},
        "user_id":    &types.AttributeValueMemberS{Value: "user-001"},
        "expires_at": &types.AttributeValueMemberS{Value: "1706745600"},  // <-- BUG
    },
})
```

<details>
<summary>Explain the bug</summary>

The TTL attribute `expires_at` is stored as a **String** (`AttributeValueMemberS`) instead of a **Number** (`AttributeValueMemberN`). DynamoDB TTL requires the attribute to be of type `N` (Number) containing the expiration time as Unix epoch seconds.

When the TTL attribute is a String, DynamoDB silently ignores it -- no error is raised, the item is written successfully, but it will never be automatically deleted. This is one of the most insidious DynamoDB bugs because there is no error message to help you diagnose it.

The fix:

```go
"expires_at": &types.AttributeValueMemberN{Value: "1706745600"},  // Number type
```

Other common TTL mistakes:
- Using **milliseconds** instead of **seconds**: `1706745600000` is interpreted as a date far in the future (year 56000+), so the item never expires
- Using an **ISO 8601 string** like `"2024-01-31T12:00:00Z"`: DynamoDB ignores non-numeric TTL values
- Setting the TTL to a **past date**: the item will be deleted (eventually), but this is sometimes done accidentally

</details>

## Solutions

<details>
<summary>TODO 1 -- DynamoDB Table with TTL</summary>

### `database.tf`

```hcl
resource "aws_dynamodb_table" "this" {
  name         = "${var.project_name}-sessions"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "session_id"

  attribute { name = "session_id"; type = "S" }

  ttl {
    attribute_name = "expires_at"
    enabled        = true
  }

  stream_enabled   = true
  stream_view_type = "NEW_AND_OLD_IMAGES"
}
```

</details>

<details>
<summary>TODO 2 + TODO 3 -- Lambda Functions and Event Source Mapping</summary>

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
    actions   = ["dynamodb:PutItem", "dynamodb:GetItem", "dynamodb:Scan", "dynamodb:DeleteItem"]
    resources = [aws_dynamodb_table.this.arn]
  }
  statement {
    actions = ["dynamodb:DescribeStream", "dynamodb:GetRecords", "dynamodb:GetShardIterator", "dynamodb:ListStreams"]
    resources = [aws_dynamodb_table.this.stream_arn]
  }
}

resource "aws_iam_policy" "ddb" {
  name   = "${var.project_name}-ddb"
  policy = data.aws_iam_policy_document.ddb.json
}

resource "aws_iam_role_policy_attachment" "ddb" {
  role       = aws_iam_role.this.name
  policy_arn = aws_iam_policy.ddb.arn
}
```

### `lambda.tf`

```hcl
locals {
  functions = toset(["write_session", "process_expired"])
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
  timeout          = 15
  environment { variables = { TABLE_NAME = aws_dynamodb_table.this.name } }
  depends_on = [aws_iam_role_policy_attachment.basic, aws_iam_role_policy_attachment.ddb, aws_cloudwatch_log_group.fn]
}

# Event source mapping for stream -> process_expired
resource "aws_lambda_event_source_mapping" "stream" {
  event_source_arn  = aws_dynamodb_table.this.stream_arn
  function_name     = aws_lambda_function.fn["process_expired"].arn
  starting_position = "LATEST"
  batch_size        = 10

  filter_criteria {
    filter {
      pattern = jsonencode({ eventName = ["REMOVE"] })
    }
  }

  depends_on = [aws_iam_role_policy.ddb]
}
```

</details>

## Verify What You Learned

```bash
# Verify TTL is enabled
aws dynamodb describe-time-to-live --table-name ttl-lab-sessions \
  --query "TimeToLiveDescription" --output json

# Write a session with 5-minute TTL
aws lambda invoke --function-name ttl-lab-write_session \
  --payload '{"session_id":"sess-001","user_id":"user-001","ttl_minutes":5}' \
  /dev/stdout 2>/dev/null | jq -r '.body' | jq .

# Verify the item exists and check the TTL attribute type
aws dynamodb get-item --table-name ttl-lab-sessions \
  --key '{"session_id":{"S":"sess-001"}}' \
  --query "Item.expires_at" --output json

terraform plan
```

Expected: TTL enabled with attribute `expires_at`, item has `expires_at` as type `N`.

## Cleanup

```bash
terraform destroy -auto-approve
terraform state list
```

Expected: no output (empty state).

## What's Next

You configured TTL for automatic item expiration and processed expired items via streams. In the next exercise, you will compare **DynamoDB auto scaling vs on-demand capacity** -- configuring target tracking auto scaling and understanding the cost trade-offs.

## Summary

- **DynamoDB TTL** automatically deletes expired items at no additional cost (no WCU consumed)
- The TTL attribute must be a **Number** type containing **Unix epoch seconds**; String values are silently ignored
- Deletion is **asynchronous**: items may persist up to 48 hours after expiration and still appear in Query/Scan results
- TTL deletions appear in DynamoDB Streams as **REMOVE events** with `userIdentity.type = "Service"` and `principalId = "dynamodb.amazonaws.com"`
- Use stream filter criteria `eventName = ["REMOVE"]` to process only deletions
- **Common mistakes**: storing TTL as String instead of Number, using milliseconds instead of seconds, confusing epoch time with ISO 8601
- To filter out expired (but not yet deleted) items in queries, add a `FilterExpression` checking `expires_at > :now`

## Reference

- [DynamoDB Time to Live](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/TTL.html)
- [TTL and DynamoDB Streams](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/time-to-live-ttl-streams.html)
- [Terraform aws_dynamodb_table TTL](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/dynamodb_table#ttl)
- [DynamoDB Streams REMOVE Events](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/Streams.html)

## Additional Resources

- [Enabling TTL](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/time-to-live-ttl-how-to.html) -- step-by-step guide for enabling and verifying TTL
- [TTL Expiration Timing](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/howitworks-ttl.html) -- details on deletion timing and background scanner behavior
- [Session Store Pattern](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/bp-use-cases-session-store.html) -- using TTL for session management
- [Data Archival with TTL and Streams](https://aws.amazon.com/blogs/database/automatically-archive-items-to-s3-using-dynamodb-time-to-live-with-aws-lambda-and-amazon-kinesis-data-firehose/) -- archiving expired items to S3

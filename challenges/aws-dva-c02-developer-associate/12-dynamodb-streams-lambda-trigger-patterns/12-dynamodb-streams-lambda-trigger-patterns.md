# 12. DynamoDB Streams with Lambda Trigger Patterns

<!--
difficulty: advanced
concepts: [dynamodb-streams, stream-view-type, lambda-event-source-mapping, filter-criteria, bisect-batch, idempotent-processing]
tools: [terraform, aws-cli]
estimated_time: 55m
bloom_level: evaluate
prerequisites: [none]
aws_cost: ~$0.02/hr
-->

> **AWS Cost Warning:** This exercise creates two DynamoDB tables (one with streams), two Lambda functions, and an event source mapping. Cost is approximately $0.02/hr. DynamoDB Streams are free for the first 2.5 million read requests per month. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally
- Basic understanding of DynamoDB (from exercise 3)

## Learning Objectives

After completing this exercise, you will be able to:

- **Evaluate** which `StreamViewType` (KEYS_ONLY, NEW_IMAGE, OLD_IMAGE, NEW_AND_OLD_IMAGES) to use based on the processing requirements of downstream consumers
- **Design** an event source mapping with filter criteria to selectively process only specific DynamoDB Stream event types (INSERT, MODIFY, REMOVE)
- **Implement** idempotent stream processing using conditional writes with `ConditionExpression` to prevent duplicate side effects
- **Configure** error handling with `bisect_batch_on_function_error` and `maximum_retry_attempts` to isolate poison messages in a batch
- **Analyze** the differences between DynamoDB Stream records for INSERT, MODIFY, and REMOVE events and how `NewImage` and `OldImage` availability depends on the view type

## Why This Matters

DynamoDB Streams unlock event-driven architectures by turning every table change into a processable event. When a user updates their profile, you can replicate the change to a search index. When an order status changes from "pending" to "shipped," you can trigger a notification. The DVA-C02 exam tests this pattern heavily because it combines three concepts: stream configuration, Lambda event source mappings, and error handling.

The most common exam trap is the `StreamViewType`. If you enable streams with `KEYS_ONLY` but your Lambda expects to read `NewImage.email`, the code crashes because `NewImage` is null in `KEYS_ONLY` mode. The exam also tests filter criteria -- a relatively new feature that lets you discard events at the infrastructure level before Lambda even invokes your function, saving cost and reducing noise. Finally, understanding batch error handling (`bisect_batch_on_function_error`, `maximum_retry_attempts`, `maximum_record_age_in_seconds`) is essential because a single poison record in a batch of 100 can block the entire shard if not handled correctly.

## The Challenge

Build an event sourcing pipeline where changes to a source DynamoDB table flow through a stream to a Lambda function that writes derived events to a target table. Configure filtering, error handling, and idempotent processing.

### Requirements

| Requirement | Description |
|---|---|
| Source Table | DynamoDB table with streams enabled using `NEW_AND_OLD_IMAGES` view type |
| Target Table | Separate DynamoDB table that receives processed events (event log / audit trail) |
| Stream Lambda | Processes stream records, writes to target table with idempotent conditional writes |
| Filter Criteria | Only process `MODIFY` events (skip INSERT and REMOVE) |
| Error Handling | `bisect_batch_on_function_error = true`, `maximum_retry_attempts = 3` |
| Idempotency | Conditional PutItem with `attribute_not_exists(event_id)` to prevent duplicates |
| Batch Size | Process up to 10 records per batch with `maximum_batching_window_in_seconds = 5` |

### Architecture

```
  +------------------+     +------------------+     +------------------+
  |   Source Table    |     |   DynamoDB        |     |  Stream Lambda   |
  |   (orders)       |---->|   Stream          |---->|  (processor)     |
  |                  |     |                   |     |                  |
  |  INSERT ---------+-----+--> filtered out   |     |  Reads:          |
  |  MODIFY ---------+-----+--> passed through-+---->|   NewImage       |
  |  REMOVE ---------+-----+--> filtered out   |     |   OldImage       |
  +------------------+     +------------------+     |   eventName      |
                                                     |                  |
                                                     |  Writes to:      |
                                                     +--------+---------+
                                                              |
                                                              v
                                                     +------------------+
                                                     |  Target Table    |
                                                     |  (event-log)     |
                                                     |                  |
                                                     |  event_id (PK)   |
                                                     |  order_id        |
                                                     |  old_status      |
                                                     |  new_status      |
                                                     |  timestamp       |
                                                     +------------------+

  Error handling:
  +---------------------------------------------------------------------+
  |  Batch [rec1, rec2, rec3_poison, rec4, rec5]                       |
  |       | first attempt fails                                        |
  |  bisect_batch_on_function_error = true                             |
  |       |                                                            |
  |  Batch A [rec1, rec2]  OK succeeds                                 |
  |  Batch B [rec3_poison, rec4, rec5]  X fails                       |
  |       | bisect again                                               |
  |  Batch C [rec3_poison]  X fails -> retries exhausted -> discarded  |
  |  Batch D [rec4, rec5]  OK succeeds                                 |
  +---------------------------------------------------------------------+
```

## Hints

<details>
<summary>Hint 1: Enabling DynamoDB Streams with the correct view type</summary>

The `stream_view_type` determines what data is available in each stream record. Choose based on what your consumer needs:

| View Type | NewImage | OldImage | Use Case |
|---|---|---|---|
| `KEYS_ONLY` | No | No | Trigger processing, fetch full item separately |
| `NEW_IMAGE` | Yes | No | Forward new state to downstream systems |
| `OLD_IMAGE` | No | Yes | Audit what was overwritten or deleted |
| `NEW_AND_OLD_IMAGES` | Yes | Yes | Compare before/after (change detection) |

```hcl
resource "aws_dynamodb_table" "source" {
  name         = "stream-demo-orders"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "order_id"

  attribute {
    name = "order_id"
    type = "S"
  }

  stream_enabled   = true
  stream_view_type = "NEW_AND_OLD_IMAGES"
}
```

The stream ARN is available as `aws_dynamodb_table.source.stream_arn`. This is what you use in the event source mapping, not the table ARN.

</details>

<details>
<summary>Hint 2: Event source mapping with filter criteria</summary>

Filter criteria let you discard stream records before Lambda invocation. The filter uses a JSON pattern matching syntax. For DynamoDB Streams, the event structure wraps records in a `dynamodb` key with `NewImage`, `OldImage`, and `Keys` subfields.

```hcl
resource "aws_lambda_event_source_mapping" "stream" {
  event_source_arn  = aws_dynamodb_table.source.stream_arn
  function_name     = aws_lambda_function.processor.arn
  starting_position = "LATEST"
  batch_size        = 10

  maximum_batching_window_in_seconds = 5

  # Only process MODIFY events (skip INSERT and REMOVE)
  filter_criteria {
    filter {
      pattern = jsonencode({
        eventName = ["MODIFY"]
      })
    }
  }

  # Error handling
  bisect_batch_on_function_error = true
  maximum_retry_attempts         = 3
  maximum_record_age_in_seconds  = 120

  depends_on = [aws_iam_role_policy.processor]
}
```

You can also filter on record content. For example, to only process records where the new status is "shipped":

```hcl
filter_criteria {
  filter {
    pattern = jsonencode({
      eventName = ["MODIFY"]
      dynamodb = {
        NewImage = {
          status = {
            S = ["shipped"]
          }
        }
      }
    })
  }
}
```

Note the DynamoDB JSON format (`{"S": "value"}`) in filter patterns -- this matches the raw stream record format, not the simplified SDK format.

</details>

<details>
<summary>Hint 3: Processing stream records in Go</summary>

DynamoDB Stream records use DynamoDB JSON format (with type descriptors like `{"S": "value"}`). The Go Lambda events library provides `events.DynamoDBEvent` and helper types that give you access to the stream record fields.

```go
import (
    "github.com/aws/aws-lambda-go/events"
)

func handler(ctx context.Context, event events.DynamoDBEvent) error {
    for _, record := range event.Records {
        eventName := record.EventName  // "INSERT", "MODIFY", or "REMOVE"

        // NewImage and OldImage availability depends on StreamViewType
        newImage := record.Change.NewImage  // map[string]events.DynamoDBAttributeValue
        oldImage := record.Change.OldImage

        if eventName == "MODIFY" {
            oldStatus := oldImage["status"].String()
            newStatus := newImage["status"].String()

            if oldStatus != newStatus {
                // Status changed -- process it
            }
        }
    }
    return nil
}
```

The `record.Change.SequenceNumber` is unique per shard and can be used as part of an idempotency key.

</details>

<details>
<summary>Hint 4: Idempotent processing with conditional writes</summary>

DynamoDB Streams guarantees at-least-once delivery, meaning your Lambda may process the same record more than once (during retries or shard rebalancing). Use a conditional write to prevent duplicate side effects:

```go
import (
    "crypto/sha256"
    "fmt"

    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

func writeEvent(ctx context.Context, client *dynamodb.Client, tableName string,
    record events.DynamoDBEventRecord, newItem, oldItem map[string]events.DynamoDBAttributeValue) error {

    // Create a deterministic event ID from the stream record
    sequenceNumber := record.Change.SequenceNumber
    hash := sha256.Sum256([]byte(sequenceNumber))
    eventID := fmt.Sprintf("%x", hash)[:32]

    _, err := client.PutItem(ctx, &dynamodb.PutItemInput{
        TableName: aws.String(tableName),
        Item: map[string]types.AttributeValue{
            "event_id":   &types.AttributeValueMemberS{Value: eventID},
            "order_id":   &types.AttributeValueMemberS{Value: newItem["order_id"].String()},
            "old_status": &types.AttributeValueMemberS{Value: oldItem["status"].String()},
            "new_status": &types.AttributeValueMemberS{Value: newItem["status"].String()},
            "timestamp":  &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", record.Change.ApproximateCreationDateTime.Unix())},
        },
        // Only write if this event_id doesn't already exist
        ConditionExpression: aws.String("attribute_not_exists(event_id)"),
    })

    // Ignore ConditionalCheckFailedException -- means duplicate, already processed
    var ccfe *types.ConditionalCheckFailedException
    if errors.As(err, &ccfe) {
        fmt.Printf("Duplicate event %s, skipping\n", eventID)
        return nil
    }

    return err
}
```

This pattern ensures that even if Lambda processes the same stream record twice, the target table only gets one entry per event.

</details>

<details>
<summary>Hint 5: Understanding bisect_batch_on_function_error</summary>

Without `bisect_batch_on_function_error`, a single poison record in a batch blocks the entire batch. Lambda retries the whole batch until `maximum_retry_attempts` is exhausted, then discards all records in the batch -- including the healthy ones.

With `bisect_batch_on_function_error = true`, Lambda splits the failed batch in half and retries each half independently. This continues recursively until the batch contains only the poison record, which is then discarded after retries are exhausted. All healthy records in the original batch are still processed.

To test this behavior:

```bash
# Insert a normal record (status change triggers processing)
aws dynamodb put-item --table-name stream-demo-orders \
  --item '{"order_id":{"S":"order-1"},"status":{"S":"pending"}}'
aws dynamodb update-item --table-name stream-demo-orders \
  --key '{"order_id":{"S":"order-1"}}' \
  --update-expression "SET #s = :new" \
  --expression-attribute-names '{"#s":"status"}' \
  --expression-attribute-values '{":new":{"S":"shipped"}}'

# Insert a poison record (e.g., with a field that causes the Lambda to crash)
aws dynamodb put-item --table-name stream-demo-orders \
  --item '{"order_id":{"S":"poison"},"status":{"S":"pending"},"crash":{"BOOL":true}}'
aws dynamodb update-item --table-name stream-demo-orders \
  --key '{"order_id":{"S":"poison"}}' \
  --update-expression "SET #s = :new" \
  --expression-attribute-names '{"#s":"status"}' \
  --expression-attribute-values '{":new":{"S":"explode"}}'
```

Check CloudWatch Logs to observe the bisecting behavior: you will see the batch size decrease with each retry.

</details>

## Spot the Bug

A developer enabled DynamoDB Streams and created a Lambda trigger. The Lambda function crashes with a nil pointer dereference on every invocation.

```hcl
resource "aws_dynamodb_table" "orders" {
  name             = "orders"
  billing_mode     = "PAY_PER_REQUEST"
  hash_key         = "order_id"
  stream_enabled   = true
  stream_view_type = "KEYS_ONLY"

  attribute {
    name = "order_id"
    type = "S"
  }
}
```

```go
func handler(ctx context.Context, event events.DynamoDBEvent) error {
    for _, record := range event.Records {
        newImage := record.Change.NewImage
        email := newImage["email"].String()
        fmt.Printf("Processing order for %s\n", email)
    }
    return nil
}
```

<details>
<summary>Explain the bug</summary>

The stream is configured with `stream_view_type = "KEYS_ONLY"`, but the Lambda code accesses `record.Change.NewImage`. With `KEYS_ONLY`, neither `NewImage` nor `OldImage` is included in the stream record -- only the key attributes are present.

`record.Change.NewImage` is an empty/nil map, and accessing `newImage["email"]` returns a zero-value `DynamoDBAttributeValue`. Calling `.String()` on it may return an empty string or panic depending on the access pattern. If the code further dereferences the result, it causes a nil pointer dereference.

The fix depends on the requirement:

**Option A -- change the stream view type** (if you need the full item):

```hcl
stream_view_type = "NEW_AND_OLD_IMAGES"
```

**Option B -- fetch the item separately** (if you want to keep KEYS_ONLY for cost):

```go
func handler(ctx context.Context, event events.DynamoDBEvent) error {
    for _, record := range event.Records {
        keys := record.Change.Keys
        orderID := keys["order_id"].String()

        // Fetch the full item since KEYS_ONLY doesn't include it
        result, err := client.GetItem(ctx, &dynamodb.GetItemInput{
            TableName: aws.String("orders"),
            Key: map[string]types.AttributeValue{
                "order_id": &types.AttributeValueMemberS{Value: orderID},
            },
        })
        if err != nil {
            return err
        }
        // Use result.Item to access the full record
    }
    return nil
}
```

On the exam, this is a common trap: the question describes a stream-triggered Lambda that "doesn't receive the item data" and asks you to identify the root cause. The answer is always the `StreamViewType` mismatch.

</details>

## Verify What You Learned

```bash
# Verify stream is enabled with correct view type
aws dynamodb describe-table --table-name stream-demo-orders \
  --query "Table.StreamSpecification" --output json
```

Expected: `{"StreamEnabled": true, "StreamViewType": "NEW_AND_OLD_IMAGES"}`

```bash
# Verify event source mapping has filter criteria
aws lambda list-event-source-mappings \
  --function-name stream-demo-processor \
  --query "EventSourceMappings[0].FilterCriteria" --output json
```

Expected: JSON containing `{"Filters": [{"Pattern": "{\"eventName\":[\"MODIFY\"]}"}]}`

```bash
# Verify bisect is enabled
aws lambda list-event-source-mappings \
  --function-name stream-demo-processor \
  --query "EventSourceMappings[0].BisectBatchOnFunctionError" --output text
```

Expected: `True`

```bash
# Trigger a MODIFY event and verify it was processed
aws dynamodb put-item --table-name stream-demo-orders \
  --item '{"order_id":{"S":"test-1"},"status":{"S":"pending"}}'
aws dynamodb update-item --table-name stream-demo-orders \
  --key '{"order_id":{"S":"test-1"}}' \
  --update-expression "SET #s = :new" \
  --expression-attribute-names '{"#s":"status"}' \
  --expression-attribute-values '{":new":{"S":"shipped"}}'

# Wait for processing, then check the target table
sleep 10
aws dynamodb scan --table-name stream-demo-event-log \
  --query "Items[*].{OrderId:order_id.S,OldStatus:old_status.S,NewStatus:new_status.S}" \
  --output table
```

Expected: One row showing `test-1` with `pending` to `shipped` transition.

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

You built an event sourcing pipeline with DynamoDB Streams, stream filtering, batch error handling, and idempotent processing. In the next exercise, you will create a **CodePipeline with CodeBuild for Lambda deployment** -- building a complete CI/CD pipeline with buildspec configuration, CodeDeploy traffic shifting, and artifact management.

## Summary

- **StreamViewType** controls what data appears in stream records: `KEYS_ONLY` (just keys), `NEW_IMAGE` (new state), `OLD_IMAGE` (old state), or `NEW_AND_OLD_IMAGES` (both for comparison)
- **Filter criteria** on the event source mapping discard events before Lambda invocation using JSON pattern matching, reducing cost and noise
- Stream records use **DynamoDB JSON format** with type descriptors (`{"S": "value"}`) -- use the `events.DynamoDBAttributeValue` helpers in Go to access values
- **bisect_batch_on_function_error** splits failed batches to isolate poison records instead of blocking the entire batch
- DynamoDB Streams provide **at-least-once delivery** -- use conditional writes (`attribute_not_exists`) for idempotent processing
- The event source mapping uses the **stream ARN** (not the table ARN) and `starting_position` controls whether to read from `LATEST` or `TRIM_HORIZON`
- Stream records include `SequenceNumber` (unique per shard) and `ApproximateCreationDateTime` -- useful for ordering and deduplication

## Reference

- [DynamoDB Streams](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/Streams.html)
- [Lambda Event Source Mapping for DynamoDB](https://docs.aws.amazon.com/lambda/latest/dg/with-ddb.html)
- [Event Filtering for Lambda](https://docs.aws.amazon.com/lambda/latest/dg/invocation-eventfiltering.html)
- [Terraform aws_lambda_event_source_mapping](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_event_source_mapping)
- [Terraform aws_dynamodb_table stream_enabled](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/dynamodb_table#stream_enabled)

## Additional Resources

- [DynamoDB Streams and Time to Live](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/time-to-live-ttl-streams.html) -- TTL deletions also appear in streams as REMOVE events with `userIdentity` set to `dynamodb.amazonaws.com`
- [Lambda Event Source Mapping Error Handling](https://docs.aws.amazon.com/lambda/latest/dg/with-ddb.html#services-dynamodb-errors) -- bisecting, retry limits, and destination configuration for failed records
- [DynamoDB Streams Low-Level API](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/Streams.LowLevel.html) -- how shards, iterators, and read throughput work under the hood
- [Change Data Capture Patterns](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/streamsmain.html) -- architectural patterns for replication, aggregation, and materialized views

<details>
<summary>Full Solution</summary>

### File Structure

```
12-dynamodb-streams-lambda-trigger-patterns/
├── main.tf
└── processor_lambda/
    ├── main.go
    └── go.mod
```

### `providers.tf`

```hcl
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    archive = {
      source  = "hashicorp/archive"
      version = "~> 2.0"
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
  description = "Project name for resource naming"
  type        = string
  default     = "stream-demo"
}
```

### `database.tf`

```hcl
# Source DynamoDB Table (with Streams)
resource "aws_dynamodb_table" "source" {
  name         = "${var.project_name}-orders"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "order_id"

  attribute {
    name = "order_id"
    type = "S"
  }

  stream_enabled   = true
  stream_view_type = "NEW_AND_OLD_IMAGES"
}

# Target DynamoDB Table (event log)
resource "aws_dynamodb_table" "target" {
  name         = "${var.project_name}-event-log"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "event_id"

  attribute {
    name = "event_id"
    type = "S"
  }
}
```

### `iam.tf`

```hcl
data "aws_iam_policy_document" "lambda_assume_role" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "processor" {
  name               = "${var.project_name}-processor-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json
}

resource "aws_iam_role_policy_attachment" "processor_basic" {
  role       = aws_iam_role.processor.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

data "aws_iam_policy_document" "processor_policy" {
  # Read from source stream
  statement {
    actions = [
      "dynamodb:DescribeStream",
      "dynamodb:GetRecords",
      "dynamodb:GetShardIterator",
      "dynamodb:ListStreams"
    ]
    resources = [aws_dynamodb_table.source.stream_arn]
  }

  # Write to target table
  statement {
    actions   = ["dynamodb:PutItem"]
    resources = [aws_dynamodb_table.target.arn]
  }
}

resource "aws_iam_role_policy" "processor" {
  name   = "processor-policy"
  role   = aws_iam_role.processor.id
  policy = data.aws_iam_policy_document.processor_policy.json
}
```

### `lambda.tf`

```hcl
# NOTE: Build the Go binary before applying:
#   cd processor_lambda && GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go && zip ../build/processor.zip bootstrap && cd ..
data "archive_file" "processor" {
  type        = "zip"
  source_file = "${path.module}/processor_lambda/bootstrap"
  output_path = "${path.module}/build/processor.zip"
}

resource "aws_lambda_function" "processor" {
  function_name    = "${var.project_name}-processor"
  filename         = data.archive_file.processor.output_path
  source_code_hash = data.archive_file.processor.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  role             = aws_iam_role.processor.arn
  timeout          = 30
  memory_size      = 256

  environment {
    variables = {
      TARGET_TABLE = aws_dynamodb_table.target.name
    }
  }

  depends_on = [
    aws_iam_role_policy_attachment.processor_basic,
    aws_cloudwatch_log_group.processor
  ]
}

resource "aws_lambda_event_source_mapping" "stream" {
  event_source_arn  = aws_dynamodb_table.source.stream_arn
  function_name     = aws_lambda_function.processor.arn
  starting_position = "LATEST"
  batch_size        = 10

  maximum_batching_window_in_seconds = 5
  bisect_batch_on_function_error     = true
  maximum_retry_attempts             = 3
  maximum_record_age_in_seconds      = 120

  # Only process MODIFY events
  filter_criteria {
    filter {
      pattern = jsonencode({
        eventName = ["MODIFY"]
      })
    }
  }

  depends_on = [aws_iam_role_policy.processor]
}
```

### `monitoring.tf`

```hcl
resource "aws_cloudwatch_log_group" "processor" {
  name              = "/aws/lambda/${var.project_name}-processor"
  retention_in_days = 1
}
```

### `outputs.tf`

```hcl
output "source_table_name" {
  value = aws_dynamodb_table.source.name
}

output "target_table_name" {
  value = aws_dynamodb_table.target.name
}

output "stream_arn" {
  value = aws_dynamodb_table.source.stream_arn
}

output "processor_function_name" {
  value = aws_lambda_function.processor.function_name
}
```

### `processor_lambda/main.go`

```go
package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

var (
	client      *dynamodb.Client
	targetTable string
)

func init() {
	targetTable = os.Getenv("TARGET_TABLE")
	cfg, _ := config.LoadDefaultConfig(context.TODO())
	client = dynamodb.NewFromConfig(cfg)
}

func handler(ctx context.Context, event events.DynamoDBEvent) error {
	fmt.Printf("Received %d records\n", len(event.Records))

	for _, record := range event.Records {
		eventName := record.EventName
		fmt.Printf("Processing %s event\n", eventName)

		// With NEW_AND_OLD_IMAGES, both are available for MODIFY events
		newImage := record.Change.NewImage
		oldImage := record.Change.OldImage

		if len(newImage) == 0 || len(oldImage) == 0 {
			fmt.Println("Skipping record: NewImage or OldImage missing")
			continue
		}

		// Check if this is a poison record (for testing bisect behavior)
		if crashVal, ok := newImage["crash"]; ok && crashVal.Boolean() {
			return fmt.Errorf("poison record detected: %s", newImage["order_id"].String())
		}

		// Detect status change
		oldStatus := "unknown"
		if v, ok := oldImage["status"]; ok {
			oldStatus = v.String()
		}
		newStatus := "unknown"
		if v, ok := newImage["status"]; ok {
			newStatus = v.String()
		}

		if oldStatus == newStatus {
			fmt.Printf("No status change for %s, skipping\n", newImage["order_id"].String())
			continue
		}

		// Create deterministic event ID for idempotency
		sequenceNumber := record.Change.SequenceNumber
		hash := sha256.Sum256([]byte(sequenceNumber))
		eventID := fmt.Sprintf("%x", hash)[:32]

		timestamp := strconv.FormatInt(record.Change.ApproximateCreationDateTime.Unix(), 10)
		processedAt := strconv.FormatInt(time.Now().Unix(), 10)

		_, err := client.PutItem(ctx, &dynamodb.PutItemInput{
			TableName: aws.String(targetTable),
			Item: map[string]types.AttributeValue{
				"event_id":     &types.AttributeValueMemberS{Value: eventID},
				"order_id":     &types.AttributeValueMemberS{Value: newImage["order_id"].String()},
				"old_status":   &types.AttributeValueMemberS{Value: oldStatus},
				"new_status":   &types.AttributeValueMemberS{Value: newStatus},
				"timestamp":    &types.AttributeValueMemberN{Value: timestamp},
				"processed_at": &types.AttributeValueMemberN{Value: processedAt},
			},
			ConditionExpression: aws.String("attribute_not_exists(event_id)"),
		})

		var ccfe *types.ConditionalCheckFailedException
		if errors.As(err, &ccfe) {
			fmt.Printf("Duplicate event %s, skipping\n", eventID)
			continue
		}
		if err != nil {
			return fmt.Errorf("failed to write event %s: %w", eventID, err)
		}

		fmt.Printf("Wrote event %s: %s %s -> %s\n", eventID,
			newImage["order_id"].String(), oldStatus, newStatus)
	}

	return nil
}

func main() {
	lambda.Start(handler)
}
```

### Testing

```bash
# Build the Go binary
cd processor_lambda && GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go && cd ..

# Deploy
terraform init && terraform apply -auto-approve

# Insert a record (INSERT event -- filtered out by filter_criteria)
aws dynamodb put-item --table-name stream-demo-orders \
  --item '{"order_id":{"S":"order-001"},"status":{"S":"pending"},"customer":{"S":"alice"}}'

# Update the record (MODIFY event -- passes filter, processed by Lambda)
aws dynamodb update-item --table-name stream-demo-orders \
  --key '{"order_id":{"S":"order-001"}}' \
  --update-expression "SET #s = :new" \
  --expression-attribute-names '{"#s":"status"}' \
  --expression-attribute-values '{":new":{"S":"shipped"}}'

# Wait for processing
sleep 15

# Check the event log table
aws dynamodb scan --table-name stream-demo-event-log \
  --query "Items[*].{EventId:event_id.S,OrderId:order_id.S,OldStatus:old_status.S,NewStatus:new_status.S}" \
  --output table

# Delete the record (REMOVE event -- filtered out)
aws dynamodb delete-item --table-name stream-demo-orders \
  --key '{"order_id":{"S":"order-001"}}'

# Wait and verify no new event was logged (REMOVE was filtered)
sleep 15
aws dynamodb scan --table-name stream-demo-event-log \
  --query "Count" --output text
# Expected: 1 (only the MODIFY event)
```

</details>

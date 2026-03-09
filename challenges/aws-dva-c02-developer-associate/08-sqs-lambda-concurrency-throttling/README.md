# 8. SQS-Lambda Concurrency, Throttling, and Batch Processing

<!--
difficulty: intermediate
concepts: [sqs-lambda-trigger, event-source-mapping, batch-size, batching-window, reserved-concurrency, partial-batch-response, maximum-concurrency]
tools: [terraform, aws-cli]
estimated_time: 50m
bloom_level: design, justify, implement
prerequisites: [none]
aws_cost: ~$0.02/hr
-->

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.5 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| jq installed | `jq --version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Design** an SQS-to-Lambda pipeline with appropriate batch size, batching window, and concurrency controls
2. **Justify** the relationship between `maximum_concurrency` on the event source mapping versus `reserved_concurrency` on the Lambda function
3. **Implement** partial batch failure reporting using `ReportBatchItemFailures` to avoid reprocessing successful messages
4. **Configure** a DLQ with `maxReceiveCount` to isolate poison messages that repeatedly fail processing
5. **Differentiate** between Lambda throttling (429), batch processing timeouts, and SQS visibility timeout behavior

## Why This Matters

SQS-to-Lambda is the most common event-driven pattern on AWS. The Lambda service polls SQS on your behalf, retrieves batches of messages, and invokes your function. But the default behavior is aggressive: Lambda scales up to 1,000 concurrent executions, each processing a batch of up to 10 messages. If your downstream dependency (a database, an API) cannot handle that throughput, you overwhelm it. The `maximum_concurrency` setting on the event source mapping (introduced in 2023) lets you cap the number of concurrent Lambda invocations per SQS source -- this is the preferred way to protect downstream systems.

The DVA-C02 exam heavily tests batch processing edge cases. If your Lambda function throws an unhandled exception, the entire batch of messages returns to the queue and is retried -- including messages that were already processed successfully. This leads to duplicate processing unless you implement `ReportBatchItemFailures`, which lets your function report exactly which messages failed so only those are retried. Combined with a DLQ (`maxReceiveCount=3`), poison messages that repeatedly fail are moved out of the main queue after a fixed number of attempts. Understanding the interplay between batch size, visibility timeout, Lambda timeout, concurrency limits, and DLQ behavior is critical for both the exam and production workloads.

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
  default     = "sqs-lambda-lab"
}
```

### `database.tf`

```hcl
# -------------------------------------------------------
# DynamoDB Table (downstream data store)
# -------------------------------------------------------
resource "aws_dynamodb_table" "orders" {
  name         = "${var.project_name}-orders"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "order_id"

  attribute {
    name = "order_id"
    type = "S"
  }

  tags = {
    Name = "${var.project_name}-orders"
  }
}
```

### `iam.tf`

```hcl
# -------------------------------------------------------
# IAM Role for Lambda
# -------------------------------------------------------
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

data "aws_iam_policy_document" "lambda_permissions" {
  statement {
    sid = "SQSAccess"
    actions = [
      "sqs:ReceiveMessage",
      "sqs:DeleteMessage",
      "sqs:GetQueueAttributes",
      "sqs:ChangeMessageVisibility",
    ]
    resources = ["*"]
  }

  statement {
    sid = "DynamoDBAccess"
    actions = [
      "dynamodb:PutItem",
      "dynamodb:GetItem",
    ]
    resources = [aws_dynamodb_table.orders.arn]
  }
}

resource "aws_iam_role_policy" "lambda_permissions" {
  name   = "${var.project_name}-lambda-permissions"
  role   = aws_iam_role.lambda.id
  policy = data.aws_iam_policy_document.lambda_permissions.json
}
```

### `lambda.tf`

```hcl
# -------------------------------------------------------
# Lambda function -- order processor
# -------------------------------------------------------
# NOTE: Build the Go binary before applying:
#   GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
#   zip processor.zip bootstrap
data "archive_file" "processor" {
  type        = "zip"
  source_file = "${path.module}/bootstrap"
  output_path = "${path.module}/processor.zip"
}

# main.go -- Go Lambda handler for SQS batch processing:
#
# package main
#
# import (
# 	"context"
# 	"encoding/json"
# 	"fmt"
# 	"os"
# 	"time"
#
# 	"github.com/aws/aws-lambda-go/events"
# 	"github.com/aws/aws-lambda-go/lambda"
# 	"github.com/aws/aws-sdk-go-v2/config"
# 	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
# 	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
# 	"github.com/aws/aws-sdk-go-v2/aws"
# )
#
# var tableName string
# var client *dynamodb.Client
#
# func init() {
# 	tableName = os.Getenv("TABLE_NAME")
# 	cfg, _ := config.LoadDefaultConfig(context.TODO())
# 	client = dynamodb.NewFromConfig(cfg)
# }
#
# type OrderMessage struct {
# 	OrderID    string `json:"order_id"`
# 	Item       string `json:"item"`
# 	ShouldFail bool   `json:"should_fail"`
# }
#
# func handler(ctx context.Context, event events.SQSEvent) (events.SQSEventResponse, error) {
# 	var failures []events.SQSBatchItemFailure
#
# 	for _, record := range event.Records {
# 		if err := processRecord(ctx, record); err != nil {
# 			fmt.Printf("Error processing record %s: %v\n", record.MessageId, err)
# 			failures = append(failures, events.SQSBatchItemFailure{
# 				ItemIdentifier: record.MessageId,
# 			})
# 		}
# 	}
#
# 	fmt.Printf("Processed %d records, %d failures\n", len(event.Records), len(failures))
#
# 	return events.SQSEventResponse{BatchItemFailures: failures}, nil
# }
#
# func processRecord(ctx context.Context, record events.SQSMessage) error {
# 	var order OrderMessage
# 	if err := json.Unmarshal([]byte(record.Body), &order); err != nil {
# 		return fmt.Errorf("failed to unmarshal: %w", err)
# 	}
#
# 	// Simulate processing time
# 	time.Sleep(500 * time.Millisecond)
#
# 	// Simulate failure for specific orders
# 	if order.ShouldFail {
# 		return fmt.Errorf("failed to process order %s", order.OrderID)
# 	}
#
# 	data, _ := json.Marshal(order)
# 	_, err := client.PutItem(ctx, &dynamodb.PutItemInput{
# 		TableName: aws.String(tableName),
# 		Item: map[string]types.AttributeValue{
# 			"order_id": &types.AttributeValueMemberS{Value: order.OrderID},
# 			"status":   &types.AttributeValueMemberS{Value: "processed"},
# 			"data":     &types.AttributeValueMemberS{Value: string(data)},
# 		},
# 	})
# 	if err != nil {
# 		return fmt.Errorf("failed to write to DynamoDB: %w", err)
# 	}
#
# 	fmt.Printf("Successfully processed order %s\n", order.OrderID)
# 	return nil
# }
#
# func main() {
# 	lambda.Start(handler)
# }

resource "aws_lambda_function" "processor" {
  function_name    = "${var.project_name}-processor"
  role             = aws_iam_role.lambda.arn
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  timeout          = 30
  filename         = data.archive_file.processor.output_path
  source_code_hash = data.archive_file.processor.output_base64sha256

  environment {
    variables = {
      TABLE_NAME = aws_dynamodb_table.orders.name
    }
  }

  tags = {
    Name = "${var.project_name}-processor"
  }
}

# =======================================================
# TODO 4 -- Reserved Concurrency on Lambda
# =======================================================
# Requirements:
#   - Create an aws_lambda_function_event_invoke_config or
#     set reserved_concurrent_executions on the Lambda function
#   - Set reserved_concurrent_executions = 10
#   - This reserves 10 execution slots from your account's
#     concurrency pool exclusively for this function
#   - Note: reserved_concurrency ALSO acts as a hard cap --
#     the function cannot exceed 10 concurrent executions
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_function#reserved_concurrent_executions
# Hint: Add this as an attribute on the aws_lambda_function resource above
```

### `events.tf`

```hcl
# -------------------------------------------------------
# SQS Dead Letter Queue
# -------------------------------------------------------
resource "aws_sqs_queue" "dlq" {
  name                      = "${var.project_name}-dlq"
  message_retention_seconds = 1209600  # 14 days

  tags = {
    Name = "${var.project_name}-dlq"
  }
}

# =======================================================
# TODO 1 -- SQS Source Queue with Redrive Policy
# =======================================================
# Requirements:
#   - Create an aws_sqs_queue named "${var.project_name}-orders"
#   - Set visibility_timeout_seconds to 180
#     (must be >= 6x the Lambda timeout of 30s)
#   - Set message_retention_seconds to 345600 (4 days)
#   - Add a redrive_policy with:
#     - deadLetterTargetArn pointing to the DLQ
#     - maxReceiveCount = 3
#   - After 3 failed processing attempts, messages move to DLQ
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sqs_queue
# Hint: redrive_policy is a JSON-encoded string


# =======================================================
# TODO 2 -- Event Source Mapping with Batch Configuration
# =======================================================
# Requirements:
#   - Create an aws_lambda_event_source_mapping connecting
#     the orders queue to the processor Lambda
#   - batch_size = 10
#   - maximum_batching_window_in_seconds = 5
#     (wait up to 5s to collect a full batch before invoking)
#   - function_response_types = ["ReportBatchItemFailures"]
#   - enabled = true
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_event_source_mapping


# =======================================================
# TODO 3 -- Maximum Concurrency on Event Source Mapping
# =======================================================
# Requirements:
#   - Add a scaling_config block to the event source mapping
#     from TODO 2 with maximum_concurrency = 5
#   - This limits the number of concurrent Lambda invocations
#     that the SQS poller will create
#   - Without this, Lambda auto-scales up to 1,000 concurrent
#     executions, potentially overwhelming downstream services
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_event_source_mapping#scaling_config
# Note: maximum_concurrency on event source mapping is different
#       from reserved_concurrency on the Lambda function
```

### `monitoring.tf`

```hcl
# =======================================================
# TODO 5 -- CloudWatch Alarm on Queue Depth
# =======================================================
# Requirements:
#   - Create an aws_cloudwatch_metric_alarm that monitors
#     ApproximateNumberOfMessagesVisible on the orders queue
#   - Set threshold to 1000
#   - Set evaluation_periods to 3 and period to 60
#   - Set comparison_operator to "GreaterThanThreshold"
#   - Set statistic to "Maximum"
#   - Set namespace to "AWS/SQS" and metric_name to
#     "ApproximateNumberOfMessagesVisible"
#   - Add a dimensions block with QueueName = queue name
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_metric_alarm


# =======================================================
# TODO 6 -- CloudWatch Alarm on DLQ Messages
# =======================================================
# Requirements:
#   - Create a second alarm that fires when ANY message
#     lands in the DLQ (threshold = 0, comparison =
#     GreaterThanThreshold)
#   - evaluation_periods = 1, period = 60
#   - This alerts you immediately when poison messages
#     are being quarantined
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_metric_alarm
```

### `outputs.tf`

```hcl
output "queue_url" {
  value = aws_sqs_queue.orders.url
}

output "dlq_url" {
  value = aws_sqs_queue.dlq.url
}

output "function_name" {
  value = aws_lambda_function.processor.function_name
}

output "table_name" {
  value = aws_dynamodb_table.orders.name
}
```

## Spot the Bug

A developer sets `batch_size=10` on the event source mapping but their Lambda function has a `timeout=3` seconds. Each message takes approximately 0.5 seconds to process. The function keeps timing out and messages cycle endlessly between the queue and Lambda. **What is wrong and how do you fix it?**

```hcl
resource "aws_lambda_event_source_mapping" "orders" {
  event_source_arn = aws_sqs_queue.orders.arn
  function_name    = aws_lambda_function.processor.arn
  batch_size       = 10                                  # <-- 10 messages
}

resource "aws_lambda_function" "processor" {
  function_name = "order-processor"
  timeout       = 3                                       # <-- 3 seconds
  # ...
}
```

<details>
<summary>Explain the bug</summary>

With `batch_size=10` and ~0.5 seconds per message, processing a full batch takes approximately 5 seconds. But the Lambda timeout is only 3 seconds, so the function is killed before it finishes the batch. All 10 messages (including the ones already processed) return to the queue because the function did not complete successfully.

This creates an infinite retry loop: messages are received, partially processed, the function times out, messages return to the queue, and the cycle repeats until `maxReceiveCount` is exceeded and everything moves to the DLQ.

Two fixes (use both):

1. **Increase the Lambda timeout** to accommodate the worst-case batch processing time. For 10 messages at 0.5s each, set timeout to at least 10 seconds (with margin: 15-30s).

2. **Use `ReportBatchItemFailures`** so that even if some messages fail, the successfully processed ones are deleted from the queue:

```hcl
resource "aws_lambda_event_source_mapping" "orders" {
  event_source_arn        = aws_sqs_queue.orders.arn
  function_name           = aws_lambda_function.processor.arn
  batch_size              = 10
  function_response_types = ["ReportBatchItemFailures"]
}

resource "aws_lambda_function" "processor" {
  function_name = "order-processor"
  timeout       = 30
  # ...
}
```

Also ensure the SQS `visibility_timeout_seconds` is at least 6x the Lambda timeout per AWS best practices.

</details>

## Verify What You Learned

### Step 1 -- Apply the infrastructure

```
terraform init && terraform apply -auto-approve
```

### Step 2 -- Send a batch of messages (mix of success and failure)

```
QUEUE_URL=$(terraform output -raw queue_url)

for i in $(seq 1 8); do
  aws sqs send-message \
    --queue-url "$QUEUE_URL" \
    --message-body "{\"order_id\": \"order-$i\", \"item\": \"widget-$i\", \"should_fail\": false}"
done

# Send 2 messages that will fail
for i in $(seq 9 10); do
  aws sqs send-message \
    --queue-url "$QUEUE_URL" \
    --message-body "{\"order_id\": \"order-$i\", \"item\": \"widget-$i\", \"should_fail\": true}"
done

echo "Sent 10 messages (8 good, 2 bad)"
```

### Step 3 -- Wait and verify successful orders in DynamoDB

```
sleep 30

aws dynamodb scan \
  --table-name $(terraform output -raw table_name) \
  --query "Items[].{OrderID:order_id.S,Status:status.S}" \
  --output table
```

Expected: 8 orders with status "processed". The 2 failing orders should not appear.

### Step 4 -- Check DLQ after retries exhaust maxReceiveCount

Wait for the failing messages to exhaust their 3 retry attempts:

```
sleep 120

aws sqs get-queue-attributes \
  --queue-url $(terraform output -raw dlq_url) \
  --attribute-names ApproximateNumberOfMessages \
  --query "Attributes.ApproximateNumberOfMessages" \
  --output text
```

Expected: `2` (the two poison messages landed in the DLQ).

### Step 5 -- Verify event source mapping configuration

```
aws lambda list-event-source-mappings \
  --function-name $(terraform output -raw function_name) \
  --query "EventSourceMappings[].{Source:EventSourceArn,BatchSize:BatchSize,MaxConcurrency:ScalingConfig.MaximumConcurrency,ResponseTypes:FunctionResponseTypes}" \
  --output table
```

Expected:

```
-------------------------------------------------------------------
|               ListEventSourceMappings                           |
+-----------+------------------+------------+---------------------+
| BatchSize | MaxConcurrency   | ResponseTypes | Source            |
+-----------+------------------+---------------+------------------+
| 10        | 5                | ReportBatch...| arn:aws:sqs:...  |
+-----------+------------------+---------------+------------------+
```

### Step 6 -- Verify reserved concurrency

```
aws lambda get-function \
  --function-name $(terraform output -raw function_name) \
  --query "Concurrency.ReservedConcurrentExecutions" \
  --output text
```

Expected: `10`

### Step 7 -- Verify CloudWatch alarms

```
aws cloudwatch describe-alarms \
  --alarm-name-prefix "sqs-lambda-lab" \
  --query "MetricAlarms[].{Name:AlarmName,Metric:MetricName,Threshold:Threshold}" \
  --output table
```

Expected:

```
-----------------------------------------------------------------------
|                        DescribeAlarms                               |
+---------------------------------+------------------------------+----+
|             Name                |          Metric              | Threshold |
+---------------------------------+------------------------------+-----------+
| sqs-lambda-lab-queue-depth      | ApproxNumberOfMessagesVisible| 1000.0   |
| sqs-lambda-lab-dlq-messages     | ApproxNumberOfMessagesVisible| 0.0      |
+---------------------------------+------------------------------+-----------+
```

## Solutions

<details>
<summary>TODO 1 -- SQS Source Queue with Redrive Policy (events.tf)</summary>

```hcl
resource "aws_sqs_queue" "orders" {
  name                       = "${var.project_name}-orders"
  visibility_timeout_seconds = 180
  message_retention_seconds  = 345600

  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.dlq.arn
    maxReceiveCount     = 3
  })

  tags = {
    Name = "${var.project_name}-orders"
  }
}
```

</details>

<details>
<summary>TODO 2 + TODO 3 -- Event Source Mapping with Batch Config and Max Concurrency (events.tf)</summary>

```hcl
resource "aws_lambda_event_source_mapping" "orders" {
  event_source_arn                   = aws_sqs_queue.orders.arn
  function_name                      = aws_lambda_function.processor.arn
  batch_size                         = 10
  maximum_batching_window_in_seconds = 5
  enabled                            = true

  function_response_types = ["ReportBatchItemFailures"]

  scaling_config {
    maximum_concurrency = 5
  }
}
```

</details>

<details>
<summary>TODO 4 -- Reserved Concurrency on Lambda (lambda.tf)</summary>

Add `reserved_concurrent_executions` to the existing `aws_lambda_function "processor"` resource:

```hcl
resource "aws_lambda_function" "processor" {
  function_name                  = "${var.project_name}-processor"
  role                           = aws_iam_role.lambda.arn
  handler                        = "bootstrap"
  runtime                        = "provided.al2023"
  timeout                        = 30
  reserved_concurrent_executions = 10
  filename                       = data.archive_file.processor.output_path
  source_code_hash               = data.archive_file.processor.output_base64sha256

  environment {
    variables = {
      TABLE_NAME = aws_dynamodb_table.orders.name
    }
  }

  tags = {
    Name = "${var.project_name}-processor"
  }
}
```

</details>

<details>
<summary>TODO 5 -- CloudWatch Alarm on Queue Depth (monitoring.tf)</summary>

```hcl
resource "aws_cloudwatch_metric_alarm" "queue_depth" {
  alarm_name          = "${var.project_name}-queue-depth"
  alarm_description   = "Alert when order queue depth exceeds 1000"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  metric_name         = "ApproximateNumberOfMessagesVisible"
  namespace           = "AWS/SQS"
  period              = 60
  statistic           = "Maximum"
  threshold           = 1000

  dimensions = {
    QueueName = aws_sqs_queue.orders.name
  }

  tags = {
    Name = "${var.project_name}-queue-depth-alarm"
  }
}
```

</details>

<details>
<summary>TODO 6 -- CloudWatch Alarm on DLQ Messages (monitoring.tf)</summary>

```hcl
resource "aws_cloudwatch_metric_alarm" "dlq_messages" {
  alarm_name          = "${var.project_name}-dlq-messages"
  alarm_description   = "Alert when any message lands in the DLQ"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "ApproximateNumberOfMessagesVisible"
  namespace           = "AWS/SQS"
  period              = 60
  statistic           = "Maximum"
  threshold           = 0

  dimensions = {
    QueueName = aws_sqs_queue.dlq.name
  }

  tags = {
    Name = "${var.project_name}-dlq-alarm"
  }
}
```

</details>

## Cleanup

Destroy all resources:

```
terraform destroy -auto-approve
```

Verify the queues and table are deleted:

```
aws sqs list-queues --queue-name-prefix "sqs-lambda-lab" \
  --query "QueueUrls" --output text
aws dynamodb describe-table --table-name sqs-lambda-lab-orders 2>&1 | head -1
```

Both should return empty output or a "ResourceNotFoundException".

## What's Next

In **Exercise 09 -- API Gateway Caching, Throttling, and Usage Plans**, you will configure API Gateway stage-level caching, per-method throttling, and Usage Plans with API Keys to control client access and protect backend services.

## Summary

You built a production-grade SQS-to-Lambda pipeline with:

- **Batch processing** -- batch_size=10 with a 5-second batching window for throughput optimization
- **Partial batch failure** -- `ReportBatchItemFailures` ensures only failed messages are retried
- **Maximum concurrency** -- `scaling_config.maximum_concurrency=5` on the event source mapping caps SQS-driven invocations
- **Reserved concurrency** -- 10 reserved execution slots for the processor function
- **DLQ with redrive** -- poison messages quarantined after 3 failed attempts
- **CloudWatch alarms** -- queue depth and DLQ monitoring

Key exam concept: `maximum_concurrency` on the event source mapping limits how many concurrent Lambda invocations the SQS poller creates. `reserved_concurrency` on the function limits total concurrent executions across ALL triggers. Use both together to protect downstream services while ensuring the function has dedicated capacity.

## Reference

| Resource | Purpose |
|----------|---------|
| `aws_sqs_queue` | Message queue with redrive policy |
| `aws_lambda_event_source_mapping` | Connects SQS to Lambda with batch settings |
| `scaling_config.maximum_concurrency` | Caps concurrent invocations per source |
| `reserved_concurrent_executions` | Reserves Lambda concurrency from account pool |
| `function_response_types` | Enables partial batch failure reporting |
| `aws_cloudwatch_metric_alarm` | Monitors queue depth and DLQ |

## Additional Resources

- [SQS as Lambda Event Source](https://docs.aws.amazon.com/lambda/latest/dg/with-sqs.html)
- [Partial Batch Response](https://docs.aws.amazon.com/lambda/latest/dg/with-sqs.html#services-sqs-batchfailurereporting)
- [Lambda Concurrency](https://docs.aws.amazon.com/lambda/latest/dg/configuration-concurrency.html)
- [Event Source Mapping Maximum Concurrency](https://docs.aws.amazon.com/lambda/latest/dg/with-sqs.html#events-sqs-max-concurrency)
- [SQS Visibility Timeout](https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/sqs-visibility-timeout.html)
- [DVA-C02 Exam Guide](https://aws.amazon.com/certification/certified-developer-associate/)

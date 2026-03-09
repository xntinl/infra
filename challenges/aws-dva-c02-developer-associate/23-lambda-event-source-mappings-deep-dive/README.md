# 23. Lambda Event Source Mappings Deep Dive

<!--
difficulty: intermediate
concepts: [event-source-mapping, sqs-polling, kinesis-shard-iterator, dynamodb-streams, batch-size, parallelization-factor, bisect-on-error, trim-horizon]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: differentiate, implement, design
prerequisites: [exercise-08, exercise-12]
aws_cost: ~$0.02/hr
-->

> **AWS Cost Warning:** This exercise creates a Kinesis Data Stream (on-demand), an SQS queue, a DynamoDB table with streams, and three Lambda functions. Total cost is approximately $0.02/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| Exercise 08 completed | SQS-Lambda event source basics |
| Exercise 12 completed | DynamoDB Streams trigger patterns |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Differentiate** between event source mapping behaviors for SQS (queue-based, message deletion), Kinesis (stream-based, shard iterator), and DynamoDB Streams (stream-based, trim horizon)
2. **Implement** event source mappings with appropriate batch settings, parallelization factor, and error handling for each source type
3. **Design** error handling strategies using `bisect_batch_on_function_error`, `maximum_retry_attempts`, and `destination_config` for stream-based sources
4. **Configure** parallelization factor on Kinesis to process multiple batches per shard concurrently
5. **Justify** the choice of `starting_position` (TRIM_HORIZON vs LATEST vs AT_TIMESTAMP) based on data processing requirements

## Why This Matters

Event source mappings are how Lambda polls event sources on your behalf. The Lambda service manages the polling, batching, and invocation -- you do not write polling code. But the behavior differs significantly between queue-based sources (SQS) and stream-based sources (Kinesis, DynamoDB Streams), and the DVA-C02 exam tests these differences extensively.

**SQS**: Lambda polls the queue, receives up to `batch_size` messages, invokes your function, and deletes successful messages. If the function fails, messages return to the queue after `visibility_timeout` expires. `ReportBatchItemFailures` enables partial success. `maximum_concurrency` in the scaling config caps concurrent invocations.

**Kinesis**: Lambda reads from each shard using a shard iterator. Each shard has exactly one concurrent invocation by default, but `parallelization_factor` (1-10) allows multiple batches per shard. If your function fails, the entire batch is retried from the same position (the shard iterator does not advance). `bisect_batch_on_function_error` splits a failing batch in half to isolate the poison record. After `maximum_retry_attempts`, failed records go to an on-failure destination.

**DynamoDB Streams**: Nearly identical to Kinesis -- stream-based, shard iterator, same error handling options. The key difference is `starting_position`: TRIM_HORIZON reads from the oldest available record, LATEST reads only new records. There is no AT_TIMESTAMP option for DynamoDB Streams.

## Building Blocks

Create the Go handler in `main.go` that processes events from all three sources:

### `main.go`

```go
// main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

func handler(ctx context.Context, event json.RawMessage) (interface{}, error) {
	sourceType := os.Getenv("SOURCE_TYPE")
	fmt.Printf("[%s] Received event: %s\n", sourceType, string(event))

	switch sourceType {
	case "SQS":
		return handleSQS(ctx, event)
	case "KINESIS":
		return handleKinesis(ctx, event)
	case "DYNAMODB":
		return handleDynamoDB(ctx, event)
	default:
		return map[string]string{"error": "Unknown source type"}, nil
	}
}

func handleSQS(ctx context.Context, raw json.RawMessage) (interface{}, error) {
	var event events.SQSEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		return nil, err
	}

	var failures []events.SQSBatchItemFailure
	for _, record := range event.Records {
		fmt.Printf("[SQS] Processing message %s: %s\n", record.MessageId, record.Body)

		// Simulate failure for messages containing "FAIL"
		if contains(record.Body, "FAIL") {
			failures = append(failures, events.SQSBatchItemFailure{
				ItemIdentifier: record.MessageId,
			})
		}
	}

	return events.SQSEventResponse{BatchItemFailures: failures}, nil
}

func handleKinesis(ctx context.Context, raw json.RawMessage) (interface{}, error) {
	var event events.KinesisEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		return nil, err
	}

	for _, record := range event.Records {
		data := string(record.Kinesis.Data)
		fmt.Printf("[Kinesis] Shard %s, Seq %s: %s\n",
			record.EventSourceArn, record.Kinesis.SequenceNumber, data)

		if contains(data, "POISON") {
			return nil, fmt.Errorf("poison record detected: %s", data)
		}
	}

	return map[string]string{"status": "ok", "records": fmt.Sprintf("%d", len(event.Records))}, nil
}

func handleDynamoDB(ctx context.Context, raw json.RawMessage) (interface{}, error) {
	var event events.DynamoDBEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		return nil, err
	}

	for _, record := range event.Records {
		fmt.Printf("[DynamoDB] %s event on %s\n", record.EventName, record.EventSourceArn)
	}

	return map[string]string{"status": "ok", "records": fmt.Sprintf("%d", len(event.Records))}, nil
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func main() {
	lambda.Start(handler)
}
```

Create the following Terraform files. Your job is to fill in each `# TODO` block.

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

provider "aws" { region = var.region }
```

### `variables.tf`

```hcl
variable "region" {
  description = "AWS region for all resources"
  type        = string
  default     = "us-east-1"
}

variable "project_name" {
  description = "Project name used for resource naming"
  type        = string
  default     = "esm-deep-dive"
}
```

### `events.tf`

```hcl
# -------------------------------------------------------
# Event Sources
# -------------------------------------------------------
resource "aws_sqs_queue" "dlq" {
  name = "${var.project_name}-dlq"
}

resource "aws_sqs_queue" "this" {
  name                       = "${var.project_name}-queue"
  visibility_timeout_seconds = 180
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.dlq.arn
    maxReceiveCount     = 3
  })
}

resource "aws_kinesis_stream" "this" {
  name             = "${var.project_name}-stream"
  shard_count      = 2
  retention_period = 24

  stream_mode_details {
    stream_mode = "PROVISIONED"
  }
}

resource "aws_dynamodb_table" "this" {
  name             = "${var.project_name}-table"
  billing_mode     = "PAY_PER_REQUEST"
  hash_key         = "pk"
  stream_enabled   = true
  stream_view_type = "NEW_AND_OLD_IMAGES"

  attribute { name = "pk"; type = "S" }
}

# SNS topic for failed Kinesis records
resource "aws_sns_topic" "failures" {
  name = "${var.project_name}-failures"
}

# =======================================================
# TODO 1 -- SQS Event Source Mapping
# =======================================================
# Requirements:
#   - batch_size = 10
#   - maximum_batching_window_in_seconds = 5
#   - function_response_types = ["ReportBatchItemFailures"]
#   - scaling_config with maximum_concurrency = 5
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_event_source_mapping


# =======================================================
# TODO 2 -- Kinesis Event Source Mapping
# =======================================================
# Requirements:
#   - starting_position = "TRIM_HORIZON"
#   - batch_size = 100
#   - maximum_batching_window_in_seconds = 10
#   - parallelization_factor = 2
#   - bisect_batch_on_function_error = true
#   - maximum_retry_attempts = 3
#   - maximum_record_age_in_seconds = 3600
#   - destination_config with on_failure destination_arn
#     pointing to the SNS topic
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_event_source_mapping


# =======================================================
# TODO 3 -- DynamoDB Streams Event Source Mapping
# =======================================================
# Requirements:
#   - starting_position = "TRIM_HORIZON"
#   - batch_size = 50
#   - maximum_batching_window_in_seconds = 5
#   - bisect_batch_on_function_error = true
#   - maximum_retry_attempts = 2
#   - maximum_record_age_in_seconds = 1800
#   - parallelization_factor = 1 (default for DynamoDB)
#   - Note: DynamoDB Streams does NOT support AT_TIMESTAMP
#     starting_position
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_event_source_mapping
```

### `build.tf`

```hcl
# -------------------------------------------------------
# Build and package
# -------------------------------------------------------
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
# -------------------------------------------------------
# IAM (shared role for all three functions)
# -------------------------------------------------------
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

data "aws_iam_policy_document" "source_access" {
  statement {
    sid     = "SQS"
    actions = ["sqs:ReceiveMessage", "sqs:DeleteMessage", "sqs:GetQueueAttributes", "sqs:ChangeMessageVisibility"]
    resources = [aws_sqs_queue.this.arn]
  }
  statement {
    sid     = "Kinesis"
    actions = ["kinesis:GetRecords", "kinesis:GetShardIterator", "kinesis:DescribeStream", "kinesis:ListShards", "kinesis:SubscribeToShard"]
    resources = [aws_kinesis_stream.this.arn]
  }
  statement {
    sid     = "DynamoDBStreams"
    actions = ["dynamodb:GetRecords", "dynamodb:GetShardIterator", "dynamodb:DescribeStream", "dynamodb:ListStreams"]
    resources = ["${aws_dynamodb_table.this.arn}/stream/*"]
  }
  statement {
    sid       = "SNSPublish"
    actions   = ["sns:Publish"]
    resources = [aws_sns_topic.failures.arn]
  }
}

resource "aws_iam_role_policy" "source_access" {
  name   = "source-access"
  role   = aws_iam_role.this.id
  policy = data.aws_iam_policy_document.source_access.json
}
```

### `lambda.tf`

```hcl
# -------------------------------------------------------
# Lambda Functions (one per source type)
# -------------------------------------------------------
resource "aws_cloudwatch_log_group" "sqs_processor" {
  name              = "/aws/lambda/${var.project_name}-sqs-processor"
  retention_in_days = 1
}

resource "aws_lambda_function" "sqs_processor" {
  function_name    = "${var.project_name}-sqs-processor"
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.this.arn
  timeout          = 30
  environment { variables = { SOURCE_TYPE = "SQS" } }
  depends_on = [aws_cloudwatch_log_group.sqs_processor]
}

resource "aws_cloudwatch_log_group" "kinesis_processor" {
  name              = "/aws/lambda/${var.project_name}-kinesis-processor"
  retention_in_days = 1
}

resource "aws_lambda_function" "kinesis_processor" {
  function_name    = "${var.project_name}-kinesis-processor"
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.this.arn
  timeout          = 30
  environment { variables = { SOURCE_TYPE = "KINESIS" } }
  depends_on = [aws_cloudwatch_log_group.kinesis_processor]
}

resource "aws_cloudwatch_log_group" "dynamodb_processor" {
  name              = "/aws/lambda/${var.project_name}-dynamodb-processor"
  retention_in_days = 1
}

resource "aws_lambda_function" "dynamodb_processor" {
  function_name    = "${var.project_name}-dynamodb-processor"
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.this.arn
  timeout          = 30
  environment { variables = { SOURCE_TYPE = "DYNAMODB" } }
  depends_on = [aws_cloudwatch_log_group.dynamodb_processor]
}
```

### `outputs.tf`

```hcl
output "queue_url"         { value = aws_sqs_queue.this.url }
output "stream_name"       { value = aws_kinesis_stream.this.name }
output "table_name"        { value = aws_dynamodb_table.this.name }
output "sqs_function"      { value = aws_lambda_function.sqs_processor.function_name }
output "kinesis_function"  { value = aws_lambda_function.kinesis_processor.function_name }
output "dynamodb_function" { value = aws_lambda_function.dynamodb_processor.function_name }
```

## Spot the Bug

A developer sets `parallelization_factor = 10` on an SQS event source mapping:

```hcl
resource "aws_lambda_event_source_mapping" "sqs" {
  event_source_arn = aws_sqs_queue.orders.arn
  function_name    = aws_lambda_function.processor.arn
  batch_size       = 10

  parallelization_factor = 10   # <-- Trying to increase parallelism
}
```

<details>
<summary>Explain the bug</summary>

`parallelization_factor` is only valid for **stream-based** event sources (Kinesis and DynamoDB Streams). It controls how many batches Lambda processes concurrently per shard. SQS does not have shards -- it auto-scales the number of Lambda invocations based on queue depth.

For SQS, the equivalent control is `scaling_config.maximum_concurrency`, which caps the number of concurrent Lambda invocations the SQS poller creates.

The `terraform apply` will fail with `InvalidParameterValueException: parallelization_factor is not supported for this event source type`.

The fix -- use `scaling_config` for SQS:

```hcl
resource "aws_lambda_event_source_mapping" "sqs" {
  event_source_arn = aws_sqs_queue.orders.arn
  function_name    = aws_lambda_function.processor.arn
  batch_size       = 10

  scaling_config {
    maximum_concurrency = 10
  }
}
```

</details>

## Verify What You Learned

### Step 1 -- Apply the infrastructure

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init && terraform apply -auto-approve
```

### Step 2 -- Verify all three event source mappings

```bash
for fn in sqs-processor kinesis-processor dynamodb-processor; do
  echo "--- esm-deep-dive-$fn ---"
  aws lambda list-event-source-mappings \
    --function-name "esm-deep-dive-$fn" \
    --query "EventSourceMappings[0].{Source:EventSourceArn,BatchSize:BatchSize,Parallelization:ParallelizationFactor,BisectOnError:BisectBatchOnFunctionError,MaxRetries:MaximumRetryAttempts}" \
    --output table
done
```

### Step 3 -- Send test messages to SQS

```bash
QUEUE_URL=$(terraform output -raw queue_url)
aws sqs send-message --queue-url "$QUEUE_URL" --message-body '{"order": "test-1"}'
aws sqs send-message --queue-url "$QUEUE_URL" --message-body '{"order": "FAIL-test"}'
sleep 10
aws logs filter-log-events --log-group-name /aws/lambda/esm-deep-dive-sqs-processor \
  --query "events[-3:].message" --output text
```

### Step 4 -- Put records into Kinesis

```bash
STREAM=$(terraform output -raw stream_name)
aws kinesis put-record --stream-name "$STREAM" --partition-key "key1" --data "$(echo -n 'test record 1' | base64)"
aws kinesis put-record --stream-name "$STREAM" --partition-key "key2" --data "$(echo -n 'test record 2' | base64)"
sleep 10
aws logs filter-log-events --log-group-name /aws/lambda/esm-deep-dive-kinesis-processor \
  --query "events[-3:].message" --output text
```

### Step 5 -- Write to DynamoDB (triggers stream)

```bash
TABLE=$(terraform output -raw table_name)
aws dynamodb put-item --table-name "$TABLE" --item '{"pk":{"S":"item-1"},"data":{"S":"hello"}}'
sleep 10
aws logs filter-log-events --log-group-name /aws/lambda/esm-deep-dive-dynamodb-processor \
  --query "events[-3:].message" --output text
```

## Solutions

<details>
<summary>TODO 1 -- SQS Event Source Mapping (events.tf)</summary>

```hcl
resource "aws_lambda_event_source_mapping" "sqs" {
  event_source_arn                   = aws_sqs_queue.this.arn
  function_name                      = aws_lambda_function.sqs_processor.arn
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
<summary>TODO 2 -- Kinesis Event Source Mapping (events.tf)</summary>

```hcl
resource "aws_lambda_event_source_mapping" "kinesis" {
  event_source_arn                   = aws_kinesis_stream.this.arn
  function_name                      = aws_lambda_function.kinesis_processor.arn
  starting_position                  = "TRIM_HORIZON"
  batch_size                         = 100
  maximum_batching_window_in_seconds = 10
  parallelization_factor             = 2
  bisect_batch_on_function_error     = true
  maximum_retry_attempts             = 3
  maximum_record_age_in_seconds      = 3600
  enabled                            = true

  destination_config {
    on_failure {
      destination_arn = aws_sns_topic.failures.arn
    }
  }
}
```

</details>

<details>
<summary>TODO 3 -- DynamoDB Streams Event Source Mapping (events.tf)</summary>

```hcl
resource "aws_lambda_event_source_mapping" "dynamodb" {
  event_source_arn                   = aws_dynamodb_table.this.stream_arn
  function_name                      = aws_lambda_function.dynamodb_processor.arn
  starting_position                  = "TRIM_HORIZON"
  batch_size                         = 50
  maximum_batching_window_in_seconds = 5
  bisect_batch_on_function_error     = true
  maximum_retry_attempts             = 2
  maximum_record_age_in_seconds      = 1800
  parallelization_factor             = 1
  enabled                            = true
}
```

</details>

## Cleanup

Destroy all resources:

```bash
terraform destroy -auto-approve
```

Verify:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

In **Exercise 24 -- Lambda Power Tuning Optimization**, you will deploy the AWS Lambda Power Tuning Step Function to systematically test your function across different memory configurations and find the optimal cost/performance balance.

## Summary

You configured event source mappings for three different source types:

| Feature | SQS | Kinesis | DynamoDB Streams |
|---------|-----|---------|-----------------|
| Polling model | Queue-based | Stream/shard-based | Stream/shard-based |
| starting_position | N/A | TRIM_HORIZON, LATEST, AT_TIMESTAMP | TRIM_HORIZON, LATEST |
| parallelization_factor | N/A (use scaling_config) | 1-10 per shard | 1-10 per shard |
| bisect_batch_on_error | N/A (use ReportBatchItemFailures) | Yes | Yes |
| Partial batch failure | ReportBatchItemFailures | Not supported | Not supported |
| On-failure destination | DLQ via redrive_policy | SNS, SQS, or EventBridge | SNS, SQS, or EventBridge |
| Concurrency control | scaling_config.maximum_concurrency | parallelization_factor * shard count | parallelization_factor * shard count |

Key exam concept: SQS uses `ReportBatchItemFailures` for partial success; streams use `bisect_batch_on_function_error` to isolate poison records. SQS concurrency is controlled via `maximum_concurrency`; stream concurrency is `parallelization_factor * shard_count`.

## Reference

- [Lambda Event Source Mappings](https://docs.aws.amazon.com/lambda/latest/dg/invocation-eventsourcemapping.html)
- [SQS as Lambda Source](https://docs.aws.amazon.com/lambda/latest/dg/with-sqs.html)
- [Kinesis as Lambda Source](https://docs.aws.amazon.com/lambda/latest/dg/with-kinesis.html)
- [DynamoDB Streams as Lambda Source](https://docs.aws.amazon.com/lambda/latest/dg/with-ddb.html)
- [Terraform aws_lambda_event_source_mapping](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_event_source_mapping)

## Additional Resources

- [Parallelization Factor for Kinesis](https://aws.amazon.com/blogs/compute/new-aws-lambda-scaling-controls-for-kinesis-and-dynamodb-event-sources/)
- [Bisect Batch on Error](https://docs.aws.amazon.com/lambda/latest/dg/with-kinesis.html#services-kinesis-errors)
- [Event Source Mapping Error Handling](https://docs.aws.amazon.com/lambda/latest/dg/invocation-eventsourcemapping.html#invocation-eventsourcemapping-errors)
- [SQS Maximum Concurrency](https://docs.aws.amazon.com/lambda/latest/dg/with-sqs.html#events-sqs-max-concurrency)

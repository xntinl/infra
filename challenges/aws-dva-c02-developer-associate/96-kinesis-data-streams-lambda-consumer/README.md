# 96. Kinesis Data Streams with Lambda Consumer

<!--
difficulty: intermediate
concepts: [kinesis-data-stream, lambda-event-source-mapping, shards, batch-size, starting-position, parallelization-factor, iterator-age, trim-horizon, latest]
tools: [terraform, aws-cli]
estimated_time: 50m
bloom_level: apply
prerequisites: [none]
aws_cost: ~$0.02/hr
-->

> **AWS Cost Warning:** This exercise creates a Kinesis Data Stream (2 shards) and Lambda functions. Kinesis on-demand pricing is $0.04/hr per shard ($0.08/hr for 2 shards). With provisioned mode, 2 shards cost $0.015/hr each. Total cost is approximately $0.02/hr with provisioned shards. Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.5 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| jq installed | `jq --version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Configure** a Kinesis Data Stream with multiple shards and a Lambda consumer via event source mapping
2. **Differentiate** between `TRIM_HORIZON` (process from oldest record) and `LATEST` (process only new records) starting positions
3. **Apply** `batch_size`, `maximum_batching_window_in_seconds`, and `parallelization_factor` to optimize Lambda consumption
4. **Analyze** the `IteratorAge` CloudWatch metric to detect when a Lambda consumer falls behind the stream producer
5. **Implement** partial batch failure reporting with `bisect_batch_on_function_error` and `ReportBatchItemFailures`

## Why Kinesis Data Streams with Lambda

Kinesis Data Streams captures real-time data at scale -- clickstreams, IoT telemetry, application logs, financial transactions. Unlike SQS (which deletes messages after processing), Kinesis retains records for 24 hours to 365 days, allowing multiple consumers to read the same data independently.

Lambda consumes Kinesis via an **event source mapping** that polls each shard and invokes your function with batches of records. Key configuration decisions:

**Starting position**: `TRIM_HORIZON` starts from the oldest record in the shard (full replay). `LATEST` starts from new records only (skip history). `AT_TIMESTAMP` starts from a specific time. The exam frequently asks which position to use when deploying a new consumer that needs historical data.

**Parallelization factor**: By default, Lambda processes one batch per shard concurrently. Setting `parallelization_factor` to 10 allows up to 10 concurrent Lambda invocations per shard, increasing throughput tenfold without adding shards. Records with the same partition key are still processed in order within a sub-batch.

**Iterator age**: The `IteratorAge` CloudWatch metric measures the lag between the newest record in the shard and the record currently being processed. A growing iterator age means the consumer cannot keep up with the producer. Solutions: increase `parallelization_factor`, add shards, optimize Lambda processing time, or increase `batch_size`.

The DVA-C02 exam tests iterator age troubleshooting, starting position selection, and the difference between `parallelization_factor` (sub-shard parallelism) and shard count (stream-level parallelism).

## Building Blocks

### `lambda/main.go`

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
)

type Response struct {
	BatchItemFailures []BatchItemFailure `json:"batchItemFailures"`
}

type BatchItemFailure struct {
	ItemIdentifier string `json:"itemIdentifier"`
}

func handler(ctx context.Context, event events.KinesisEvent) (Response, error) {
	var failures []BatchItemFailure
	funcName := os.Getenv("AWS_LAMBDA_FUNCTION_NAME")

	fmt.Printf("[%s] Received %d records\n", funcName, len(event.Records))

	for _, record := range event.Records {
		data := string(record.Kinesis.Data)
		seqNum := record.Kinesis.SequenceNumber
		partKey := record.Kinesis.PartitionKey
		shardID := record.EventID

		fmt.Printf("  Record: shard=%s partition_key=%s seq=%s data=%s\n",
			shardID, partKey, seqNum, data)

		// Parse and process the record
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			fmt.Printf("  ERROR: Failed to parse record %s: %v\n", seqNum, err)
			failures = append(failures, BatchItemFailure{
				ItemIdentifier: seqNum,
			})
			continue
		}

		// Simulate processing
		time.Sleep(10 * time.Millisecond)

		fmt.Printf("  Processed record %s successfully\n", seqNum)
	}

	if len(failures) > 0 {
		fmt.Printf("[%s] %d failures in batch\n", funcName, len(failures))
	}

	return Response{BatchItemFailures: failures}, nil
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
  default     = "kinesis-lambda-demo"
}
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

### `events.tf`

```hcl
# =======================================================
# TODO 1 -- Kinesis Data Stream with 2 Shards
# =======================================================
# Requirements:
#   - Create an aws_kinesis_stream named "kinesis-lambda-demo"
#   - Set shard_count to 2
#   - Set retention_period to 24 (hours)
#   - Set stream_mode_details to { stream_mode = "PROVISIONED" }
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/kinesis_stream
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

data "aws_iam_policy_document" "kinesis_access" {
  statement {
    actions = [
      "kinesis:DescribeStream",
      "kinesis:DescribeStreamSummary",
      "kinesis:GetRecords",
      "kinesis:GetShardIterator",
      "kinesis:ListShards",
      "kinesis:ListStreams",
      "kinesis:SubscribeToShard",
    ]
    resources = [aws_kinesis_stream.this.arn]
  }
}

resource "aws_iam_role_policy" "kinesis" {
  name   = "kinesis-access"
  role   = aws_iam_role.this.id
  policy = data.aws_iam_policy_document.kinesis_access.json
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
  timeout          = 60

  depends_on = [aws_iam_role_policy_attachment.basic, aws_cloudwatch_log_group.this]
}

# =======================================================
# TODO 2 -- Event Source Mapping with Batch Configuration
# =======================================================
# Requirements:
#   - Create an aws_lambda_event_source_mapping
#   - Set event_source_arn to the Kinesis stream ARN
#   - Set function_name to the Lambda function ARN
#   - Set starting_position to "TRIM_HORIZON" (process from oldest)
#   - Set batch_size to 100
#   - Set maximum_batching_window_in_seconds to 5
#   - Set parallelization_factor to 2
#   - Set bisect_batch_on_function_error to true
#   - Set function_response_types to ["ReportBatchItemFailures"]
#   - Set maximum_retry_attempts to 3
#   - Set maximum_record_age_in_seconds to 86400 (24 hours)
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_event_source_mapping
```

### `monitoring.tf`

```hcl
# =======================================================
# TODO 3 -- CloudWatch Alarm on Iterator Age
# =======================================================
# Requirements:
#   - Create an aws_cloudwatch_metric_alarm named "kinesis-lambda-demo-iterator-age"
#   - Monitor "IteratorAge" metric in "AWS/Kinesis" namespace
#   - Set threshold to 60000 (60 seconds in milliseconds)
#   - Set comparison_operator to "GreaterThanThreshold"
#   - Set evaluation_periods to 3, period to 60
#   - Set statistic to "Maximum"
#   - Add dimensions: StreamName = stream name
#   - Set treat_missing_data to "notBreaching"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_metric_alarm
```

### `outputs.tf`

```hcl
output "stream_name" {
  value = aws_kinesis_stream.this.name
}

output "function_name" {
  value = aws_lambda_function.this.function_name
}
```

## Spot the Bug

A developer configures a Kinesis stream with 2 shards and a Lambda consumer. The Lambda function takes 200ms per record and processes batches of 100. After a traffic spike, the `IteratorAge` CloudWatch metric keeps increasing -- the Lambda consumer is falling further and further behind.

```hcl
resource "aws_kinesis_stream" "events" {
  name             = "event-stream"
  shard_count      = 2
  retention_period = 24
}

resource "aws_lambda_event_source_mapping" "kinesis" {
  event_source_arn  = aws_kinesis_stream.events.arn
  function_name     = aws_lambda_function.consumer.arn
  starting_position = "TRIM_HORIZON"
  batch_size        = 100
  # parallelization_factor defaults to 1
  # No parallelization_factor configured!
}

resource "aws_lambda_function" "consumer" {
  function_name = "stream-consumer"
  timeout       = 300
  memory_size   = 128
  # ...
}
```

<details>
<summary>Explain the bug</summary>

With 2 shards and `parallelization_factor=1` (default), Lambda processes at most 2 batches concurrently (one per shard). Each batch of 100 records takes 100 * 200ms = 20 seconds to process. So the consumer processes approximately 100 records every 20 seconds per shard = 5 records/second/shard = 10 records/second total.

If the producer writes more than 10 records per second, the consumer falls behind and `IteratorAge` increases continuously.

**Three fixes (use one or combine):**

1. **Increase `parallelization_factor`** to process more batches per shard concurrently:

```hcl
resource "aws_lambda_event_source_mapping" "kinesis" {
  event_source_arn        = aws_kinesis_stream.events.arn
  function_name           = aws_lambda_function.consumer.arn
  starting_position       = "TRIM_HORIZON"
  batch_size              = 100
  parallelization_factor  = 10   # 10 concurrent invocations per shard
}
```

With `parallelization_factor=10`, the consumer processes up to 20 batches concurrently (10 per shard), increasing throughput 10x.

2. **Add more shards** to the stream (resharding). Each shard adds another concurrent consumer.

3. **Optimize Lambda processing time**: increase memory (more CPU), reduce per-record processing time, or use batch processing instead of per-record loops.

The `parallelization_factor` is the lowest-cost solution because it does not require adding shards (which cost $0.015/hr each). Records with the same partition key are still processed in order within each sub-batch.

</details>

## Verify What You Learned

### Step 1 -- Apply

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init && terraform apply -auto-approve
```

### Step 2 -- Put records into the stream

```bash
STREAM=$(terraform output -raw stream_name)

for i in $(seq 1 20); do
  aws kinesis put-record \
    --stream-name "$STREAM" \
    --partition-key "user-$(( i % 5 ))" \
    --data "$(echo -n "{\"event_id\": \"evt-$i\", \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\", \"user\": \"user-$(( i % 5 ))\"}" | base64)" \
    --query "ShardId" --output text
done

echo "Sent 20 records across 5 partition keys"
```

### Step 3 -- Verify Lambda processed the records

```bash
sleep 30

aws logs filter-log-events \
  --log-group-name "/aws/lambda/$(terraform output -raw function_name)" \
  --filter-pattern "Processed record" \
  --query "events | length(@)"
```

Expected: a number close to 20 (all records processed).

### Step 4 -- Check event source mapping configuration

```bash
aws lambda list-event-source-mappings \
  --function-name $(terraform output -raw function_name) \
  --query "EventSourceMappings[0].{BatchSize:BatchSize,StartPos:StartingPosition,Parallel:ParallelizationFactor,Bisect:BisectBatchOnFunctionError}" \
  --output table
```

Expected: `BatchSize=100`, `StartingPosition=TRIM_HORIZON`, `ParallelizationFactor=2`, `BisectBatchOnFunctionError=true`.

### Step 5 -- Check iterator age

```bash
aws cloudwatch get-metric-statistics \
  --namespace "AWS/Kinesis" \
  --metric-name "GetRecords.IteratorAgeMilliseconds" \
  --dimensions Name=StreamName,Value=$(terraform output -raw stream_name) \
  --start-time $(date -u -v-30M +%Y-%m-%dT%H:%M:%S) \
  --end-time $(date -u +%Y-%m-%dT%H:%M:%S) \
  --period 300 \
  --statistics Maximum \
  --query "Datapoints[0].Maximum"
```

Expected: a low number (consumer is keeping up with the low test volume).

### Step 6 -- Verify no drift

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Solutions

<details>
<summary>events.tf -- TODO 1 -- Kinesis Data Stream</summary>

```hcl
resource "aws_kinesis_stream" "this" {
  name             = "kinesis-lambda-demo"
  shard_count      = 2
  retention_period = 24

  stream_mode_details {
    stream_mode = "PROVISIONED"
  }
}
```

</details>

<details>
<summary>lambda.tf -- TODO 2 -- Event Source Mapping</summary>

```hcl
resource "aws_lambda_event_source_mapping" "this" {
  event_source_arn                   = aws_kinesis_stream.this.arn
  function_name                      = aws_lambda_function.this.arn
  starting_position                  = "TRIM_HORIZON"
  batch_size                         = 100
  maximum_batching_window_in_seconds = 5
  parallelization_factor             = 2
  bisect_batch_on_function_error     = true
  function_response_types            = ["ReportBatchItemFailures"]
  maximum_retry_attempts             = 3
  maximum_record_age_in_seconds      = 86400
}
```

</details>

<details>
<summary>monitoring.tf -- TODO 3 -- Iterator Age Alarm</summary>

```hcl
resource "aws_cloudwatch_metric_alarm" "iterator_age" {
  alarm_name          = "kinesis-lambda-demo-iterator-age"
  alarm_description   = "Alert when Kinesis consumer falls behind"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  metric_name         = "GetRecords.IteratorAgeMilliseconds"
  namespace           = "AWS/Kinesis"
  period              = 60
  statistic           = "Maximum"
  threshold           = 60000
  treat_missing_data  = "notBreaching"

  dimensions = {
    StreamName = aws_kinesis_stream.this.name
  }
}
```

</details>

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

You built a Kinesis stream with a Lambda consumer using batch processing and parallelization. In the next exercise, you will explore **Kinesis enhanced fan-out consumers** for dedicated throughput per consumer.

## Summary

- **Kinesis Data Streams** retain records for 24h-365d, supporting multiple consumers reading the same data
- **Starting position**: `TRIM_HORIZON` reads from oldest, `LATEST` reads only new records, `AT_TIMESTAMP` reads from a specific time
- **Batch size** (1-10,000) controls how many records are sent per Lambda invocation
- **Parallelization factor** (1-10) enables multiple concurrent Lambda invocations per shard, increasing throughput without adding shards
- **Iterator age** (`GetRecords.IteratorAgeMilliseconds`) measures consumer lag -- increasing values indicate the consumer cannot keep up
- **bisect_batch_on_function_error** splits failed batches in half and retries, isolating poison records
- **ReportBatchItemFailures** returns only failed sequence numbers so successful records are not reprocessed
- Each shard provides 1 MB/s input and 2 MB/s output (shared among standard consumers)

## Reference

- [Kinesis Data Streams Developer Guide](https://docs.aws.amazon.com/streams/latest/dev/introduction.html)
- [Lambda with Kinesis](https://docs.aws.amazon.com/lambda/latest/dg/with-kinesis.html)
- [Terraform aws_kinesis_stream](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/kinesis_stream)
- [Terraform aws_lambda_event_source_mapping](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_event_source_mapping)

## Additional Resources

- [Kinesis Parallelization Factor](https://docs.aws.amazon.com/lambda/latest/dg/with-kinesis.html#services-kinesis-configure) -- how sub-shard parallelism works
- [Kinesis Resharding](https://docs.aws.amazon.com/streams/latest/dev/kinesis-using-sdk-java-resharding.html) -- splitting and merging shards
- [Kinesis Pricing](https://aws.amazon.com/kinesis/data-streams/pricing/) -- per-shard hourly and per-PUT payload pricing
- [Monitoring Kinesis with CloudWatch](https://docs.aws.amazon.com/streams/latest/dev/monitoring-with-cloudwatch.html) -- key metrics for production monitoring

# 97. Kinesis Enhanced Fan-Out Consumers

<!--
difficulty: advanced
concepts: [kinesis-enhanced-fan-out, dedicated-throughput, subscribe-to-shard, consumer-registration, shared-vs-dedicated, push-model, consumer-limits]
tools: [terraform, aws-cli]
estimated_time: 55m
bloom_level: evaluate
prerequisites: [none]
aws_cost: ~$0.03/hr
-->

> **AWS Cost Warning:** This exercise creates a Kinesis Data Stream (2 shards), enhanced fan-out consumer registrations, and Lambda functions. Enhanced fan-out costs $0.015/shard-hour/consumer + $0.013/GB data retrieval. Combined with shard costs, total is approximately $0.03/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally
- Completed exercise 96 (Kinesis basics) recommended

## Learning Objectives

After completing this exercise, you will be able to:

- **Evaluate** when enhanced fan-out is needed versus standard shared throughput consumers
- **Explain** the throughput difference: 2 MB/s shared (standard) vs 2 MB/s dedicated per consumer (enhanced fan-out)
- **Implement** enhanced fan-out consumer registration and Lambda event source mapping with consumer ARN
- **Analyze** the cost implications of enhanced fan-out: per-consumer per-shard hourly charges plus data retrieval fees
- **Identify** the hard limit of 20 enhanced fan-out consumers per stream and its impact on architecture design

## Why Enhanced Fan-Out

Standard Kinesis consumers share 2 MB/s of read throughput per shard. When one consumer reads from a shard, it reduces available throughput for other consumers on the same shard. With 5 standard consumers reading from a single shard, each effectively gets 400 KB/s. This shared model works when you have few consumers or low throughput requirements.

Enhanced fan-out changes the model fundamentally. Each registered consumer gets a **dedicated** 2 MB/s of read throughput per shard, delivered via HTTP/2 push (SubscribeToShard). Five enhanced fan-out consumers each get the full 2 MB/s -- no sharing. The data is pushed to consumers within 70ms (versus up to 200ms polling latency with standard consumers).

The DVA-C02 exam tests three scenarios. First, "multiple consumers need to process the same stream data independently without impacting each other" -- the answer is enhanced fan-out. Second, "a consumer needs sub-200ms latency for processing stream records" -- enhanced fan-out's push model delivers records within 70ms. Third, the hard limit: **20 enhanced fan-out consumers per stream**. Exceeding this limit requires architectural changes (splitting into multiple streams or consolidating consumers).

## The Challenge

Build a Kinesis stream with both a standard consumer and an enhanced fan-out consumer. Compare the configuration, throughput behavior, and cost implications.

### Requirements

| Requirement | Description |
|---|---|
| Kinesis Stream | 2 shards, provisioned mode |
| Standard Consumer | Lambda with standard event source mapping (shared throughput) |
| Enhanced Fan-Out Consumer | Registered consumer with dedicated throughput, Lambda event source mapping using consumer ARN |
| Producer | Script to put records into the stream |
| Comparison | Observe throughput and latency differences |

### Architecture

```
  Producer (AWS CLI / Lambda)
       |
       v
  ┌──────────────────────────────┐
  │   Kinesis Data Stream        │
  │   (2 shards)                 │
  │                              │
  │   Shard-1 ──┬── Standard Consumer (Lambda A)
  │             │   [Shared 2 MB/s across all standard consumers]
  │             │
  │             └── Enhanced Fan-Out Consumer (Lambda B)
  │                 [Dedicated 2 MB/s per consumer per shard]
  │                 [Push via SubscribeToShard / HTTP/2]
  │                              │
  │   Shard-2 ──┬── Standard Consumer (Lambda A)
  │             └── Enhanced Fan-Out Consumer (Lambda B)
  └──────────────────────────────┘
```

### Comparison Table

| Feature | Standard (Shared) | Enhanced Fan-Out (Dedicated) |
|---------|-------------------|------------------------------|
| **Throughput per shard** | 2 MB/s shared across ALL consumers | 2 MB/s dedicated PER consumer |
| **Delivery model** | Pull (GetRecords polling) | Push (SubscribeToShard HTTP/2) |
| **Latency** | ~200ms (polling interval) | ~70ms (push) |
| **Max consumers** | Unlimited (but throughput shared) | 20 per stream |
| **Additional cost** | None (included in shard pricing) | $0.015/shard-hour/consumer + $0.013/GB |
| **Use case** | Few consumers, cost-sensitive | Many consumers, low-latency, high-throughput |

## Hints

<details>
<summary>Hint 1: Registering an enhanced fan-out consumer</summary>

Enhanced fan-out consumers must be explicitly registered with the stream before they can subscribe to shards:

```hcl
resource "aws_kinesis_stream_consumer" "enhanced" {
  name       = "enhanced-consumer"
  stream_arn = aws_kinesis_stream.this.arn
}
```

The consumer registration creates a consumer ARN that is used in the Lambda event source mapping instead of the stream ARN.

</details>

<details>
<summary>Hint 2: Lambda event source mapping with enhanced fan-out</summary>

For enhanced fan-out, the event source mapping references the **consumer ARN** (not the stream ARN):

```hcl
# Standard consumer (uses stream ARN)
resource "aws_lambda_event_source_mapping" "standard" {
  event_source_arn  = aws_kinesis_stream.this.arn
  function_name     = aws_lambda_function.standard_consumer.arn
  starting_position = "LATEST"
  batch_size        = 100
}

# Enhanced fan-out consumer (uses consumer ARN)
resource "aws_lambda_event_source_mapping" "enhanced" {
  event_source_arn  = aws_kinesis_stream_consumer.enhanced.arn
  function_name     = aws_lambda_function.enhanced_consumer.arn
  starting_position = "LATEST"
  batch_size        = 100
}
```

When `event_source_arn` points to a `aws_kinesis_stream_consumer`, Lambda automatically uses the SubscribeToShard API for push-based delivery.

</details>

<details>
<summary>Hint 3: IAM permissions for enhanced fan-out</summary>

The Lambda execution role needs additional permissions for enhanced fan-out:

```hcl
data "aws_iam_policy_document" "kinesis_enhanced" {
  statement {
    actions = [
      "kinesis:DescribeStream",
      "kinesis:DescribeStreamSummary",
      "kinesis:GetRecords",
      "kinesis:GetShardIterator",
      "kinesis:ListShards",
      "kinesis:SubscribeToShard",
      "kinesis:DescribeStreamConsumer",
      "kinesis:ListStreamConsumers",
    ]
    resources = [
      aws_kinesis_stream.this.arn,
      "${aws_kinesis_stream.this.arn}/*",
    ]
  }
}
```

The `SubscribeToShard` and `DescribeStreamConsumer` actions are required for enhanced fan-out. The resource must include `/*` to cover consumer sub-resources.

</details>

<details>
<summary>Hint 4: Deregistering consumers on cleanup</summary>

When destroying infrastructure, the enhanced fan-out consumer must be deregistered before the stream can be deleted. Terraform handles this automatically through the dependency graph when using `aws_kinesis_stream_consumer`.

However, if you manually registered consumers via the CLI, you must deregister them:

```bash
aws kinesis deregister-stream-consumer \
  --stream-arn arn:aws:kinesis:us-east-1:123456789012:stream/my-stream \
  --consumer-name my-consumer
```

</details>

## Spot the Bug

A developer tries to register 25 enhanced fan-out consumers on a single Kinesis stream for different microservices. The first 20 succeed, but the 21st fails with `LimitExceededException`.

```bash
# Attempting to register consumer #21
aws kinesis register-stream-consumer \
  --stream-arn arn:aws:kinesis:us-east-1:123456789012:stream/events \
  --consumer-name microservice-21

# Error: LimitExceededException: Maximum number of consumers reached for this stream
```

<details>
<summary>Explain the bug</summary>

Kinesis has a **hard limit of 20 enhanced fan-out consumers per stream**. This is not a soft limit that can be increased via a service quota request -- it is a fixed architectural constraint.

The developer has 25 microservices that need to consume the same stream data independently. Three solutions:

1. **Consolidate consumers**: Instead of one consumer per microservice, create a fan-out Lambda that reads from the stream and dispatches to SNS/SQS for each microservice. This reduces the consumer count to 1 but adds a dispatch layer.

2. **Use standard consumers for low-throughput services**: Not all 25 microservices need dedicated throughput. Reserve enhanced fan-out for latency-sensitive consumers and use standard (shared) consumers for the rest. The standard consumers share 2 MB/s per shard but have no consumer count limit.

3. **Split into multiple streams**: Route records to different streams based on event type. Each stream gets its own set of 20 enhanced fan-out consumers.

```
Option 1: Fan-out dispatcher
Stream --> [Dispatcher Lambda] --> SNS Topic --> 25 SQS queues --> 25 microservices

Option 2: Mixed consumer types
Stream --> 10 Enhanced fan-out consumers (critical, low-latency)
       --> 15 Standard consumers (best-effort, shared throughput)

Option 3: Multiple streams
Stream-A (events-orders)  --> 8 consumers
Stream-B (events-users)   --> 7 consumers
Stream-C (events-metrics) --> 10 consumers
```

The exam frequently tests this limit as a "gotcha" in architecture design questions.

</details>

## Verify What You Learned

After deploying, run these verification commands:

```bash
# Verify the stream exists with 2 shards
aws kinesis describe-stream-summary \
  --stream-name $(terraform output -raw stream_name) \
  --query "StreamDescriptionSummary.{Shards:OpenShardCount,Status:StreamStatus,Consumers:ConsumerCount}" \
  --output table
```

Expected: 2 open shards, ACTIVE status, 1 consumer.

```bash
# Verify the enhanced fan-out consumer is registered
aws kinesis list-stream-consumers \
  --stream-arn $(terraform output -raw stream_arn) \
  --query "Consumers[].{Name:ConsumerName,Status:ConsumerStatus}" \
  --output table
```

Expected: consumer with status `ACTIVE`.

```bash
# Put test records
STREAM=$(terraform output -raw stream_name)
for i in $(seq 1 10); do
  aws kinesis put-record \
    --stream-name "$STREAM" \
    --partition-key "key-$i" \
    --data "$(echo -n "{\"id\": $i, \"source\": \"test\"}" | base64)" \
    --query "ShardId" --output text
done
```

```bash
# Wait for processing then check both consumer logs
sleep 30

echo "=== Standard Consumer ==="
aws logs filter-log-events \
  --log-group-name "/aws/lambda/$(terraform output -raw standard_function_name)" \
  --filter-pattern "Received" \
  --query "events | length(@)"

echo "=== Enhanced Fan-Out Consumer ==="
aws logs filter-log-events \
  --log-group-name "/aws/lambda/$(terraform output -raw enhanced_function_name)" \
  --filter-pattern "Received" \
  --query "events | length(@)"
```

Expected: both consumers processed the same records independently.

```bash
# Check event source mappings
aws lambda list-event-source-mappings \
  --function-name $(terraform output -raw enhanced_function_name) \
  --query "EventSourceMappings[0].EventSourceArn" --output text
```

Expected: a consumer ARN (containing `/consumer/`) rather than a stream ARN.

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

Destroy all resources:

```bash
terraform destroy -auto-approve
```

Verify nothing remains:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

You compared standard and enhanced fan-out consumers on Kinesis. In the next exercise, you will build a **session management system with ElastiCache Redis** and compare it with DynamoDB-based sessions.

## Summary

- **Standard consumers** share 2 MB/s per shard across all consumers; delivered via polling (GetRecords)
- **Enhanced fan-out** provides dedicated 2 MB/s per consumer per shard; delivered via HTTP/2 push (SubscribeToShard) with ~70ms latency
- Enhanced fan-out consumers must be **registered** with `aws_kinesis_stream_consumer` before use
- Lambda event source mapping uses the **consumer ARN** (not stream ARN) for enhanced fan-out
- **Hard limit: 20 enhanced fan-out consumers per stream** -- not adjustable via service quotas
- Enhanced fan-out costs $0.015/shard-hour/consumer + $0.013/GB retrieved -- standard consumers have no additional cost
- Use enhanced fan-out when: multiple consumers need independent throughput, sub-200ms latency required, or standard consumers are throttling each other
- IAM requires `kinesis:SubscribeToShard` and `kinesis:DescribeStreamConsumer` for enhanced fan-out

## Reference

- [Kinesis Enhanced Fan-Out](https://docs.aws.amazon.com/streams/latest/dev/enhanced-consumers.html)
- [Using Enhanced Fan-Out with Lambda](https://docs.aws.amazon.com/lambda/latest/dg/with-kinesis.html#services-kinesis-configure)
- [Terraform aws_kinesis_stream_consumer](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/kinesis_stream_consumer)
- [Terraform aws_lambda_event_source_mapping](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_event_source_mapping)

## Additional Resources

- [SubscribeToShard API](https://docs.aws.amazon.com/kinesis/latest/APIReference/API_SubscribeToShard.html) -- the HTTP/2 push API for enhanced fan-out
- [Kinesis Limits](https://docs.aws.amazon.com/streams/latest/dev/service-sizes-and-limits.html) -- stream, shard, and consumer limits
- [Enhanced Fan-Out Pricing](https://aws.amazon.com/kinesis/data-streams/pricing/) -- detailed cost breakdown per consumer per shard
- [Choosing Between Standard and Enhanced Fan-Out](https://docs.aws.amazon.com/streams/latest/dev/enhanced-consumers.html#enhanced-consumers-considerations) -- decision criteria

<details>
<summary>Full Solution</summary>

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

func handler(ctx context.Context, event events.KinesisEvent) error {
	funcName := os.Getenv("AWS_LAMBDA_FUNCTION_NAME")
	now := time.Now().UTC().Format(time.RFC3339)

	fmt.Printf("[%s] Received %d records at %s\n", funcName, len(event.Records), now)

	for _, record := range event.Records {
		data := string(record.Kinesis.Data)
		var payload map[string]interface{}
		json.Unmarshal([]byte(data), &payload)
		fmt.Printf("  [%s] partition=%s seq=%s data=%s\n",
			funcName, record.Kinesis.PartitionKey, record.Kinesis.SequenceNumber, data)
	}

	return nil
}

func main() {
	lambda.Start(handler)
}
```

### `lambda/go.mod`

```
module kinesis-efo-demo

go 1.21

require github.com/aws/aws-lambda-go v1.47.0
```

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
  default     = "efo-demo"
}
```

### `build.tf`

```hcl
resource "null_resource" "go_build" {
  triggers = { source_hash = filebase64sha256("${path.module}/lambda/main.go") }
  provisioner "local-exec" {
    command     = "GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go"
    working_dir = "${path.module}/lambda"
  }
}

data "archive_file" "function_zip" {
  type        = "zip"
  source_file = "${path.module}/lambda/bootstrap"
  output_path = "${path.module}/build/function.zip"
  depends_on  = [null_resource.go_build]
}
```

### `events.tf`

```hcl
resource "aws_kinesis_stream" "this" {
  name             = "${var.project_name}-stream"
  shard_count      = 2
  retention_period = 24
  stream_mode_details { stream_mode = "PROVISIONED" }
}

resource "aws_kinesis_stream_consumer" "enhanced" {
  name       = "enhanced-consumer"
  stream_arn = aws_kinesis_stream.this.arn
}
```

### `iam.tf`

```hcl
data "aws_iam_policy_document" "lambda_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
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

data "aws_iam_policy_document" "kinesis_policy" {
  statement {
    actions = [
      "kinesis:DescribeStream", "kinesis:DescribeStreamSummary",
      "kinesis:GetRecords", "kinesis:GetShardIterator",
      "kinesis:ListShards", "kinesis:SubscribeToShard",
      "kinesis:DescribeStreamConsumer", "kinesis:ListStreamConsumers",
    ]
    resources = [aws_kinesis_stream.this.arn, "${aws_kinesis_stream.this.arn}/*"]
  }
}

resource "aws_iam_role_policy" "kinesis" {
  name   = "kinesis-access"
  role   = aws_iam_role.this.id
  policy = data.aws_iam_policy_document.kinesis_policy.json
}
```

### `lambda.tf`

```hcl
resource "aws_cloudwatch_log_group" "standard" {
  name              = "/aws/lambda/${var.project_name}-standard"
  retention_in_days = 1
}

resource "aws_cloudwatch_log_group" "enhanced" {
  name              = "/aws/lambda/${var.project_name}-enhanced"
  retention_in_days = 1
}

resource "aws_lambda_function" "standard" {
  function_name    = "${var.project_name}-standard"
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.this.arn
  memory_size      = 128
  timeout          = 30
  depends_on       = [aws_iam_role_policy_attachment.basic, aws_cloudwatch_log_group.standard]
}

resource "aws_lambda_function" "enhanced" {
  function_name    = "${var.project_name}-enhanced"
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.this.arn
  memory_size      = 128
  timeout          = 30
  depends_on       = [aws_iam_role_policy_attachment.basic, aws_cloudwatch_log_group.enhanced]
}

resource "aws_lambda_event_source_mapping" "standard" {
  event_source_arn  = aws_kinesis_stream.this.arn
  function_name     = aws_lambda_function.standard.arn
  starting_position = "LATEST"
  batch_size        = 100
}

resource "aws_lambda_event_source_mapping" "enhanced" {
  event_source_arn  = aws_kinesis_stream_consumer.enhanced.arn
  function_name     = aws_lambda_function.enhanced.arn
  starting_position = "LATEST"
  batch_size        = 100
}
```

### `outputs.tf`

```hcl
output "stream_name" {
  value = aws_kinesis_stream.this.name
}

output "stream_arn" {
  value = aws_kinesis_stream.this.arn
}

output "standard_function_name" {
  value = aws_lambda_function.standard.function_name
}

output "enhanced_function_name" {
  value = aws_lambda_function.enhanced.function_name
}

output "consumer_arn" {
  value = aws_kinesis_stream_consumer.enhanced.arn
}
```

</details>

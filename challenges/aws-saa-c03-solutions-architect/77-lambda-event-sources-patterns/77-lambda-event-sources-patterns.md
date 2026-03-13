# 77. Lambda Event Sources and Invocation Patterns

<!--
difficulty: basic
concepts: [lambda-synchronous, lambda-asynchronous, lambda-polling, event-source-mapping, api-gateway-trigger, alb-trigger, s3-event-notification, sns-trigger, sqs-trigger, kinesis-trigger, dynamodb-streams, dead-letter-queue, retry-behavior, provided-al2023]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: understand, apply
prerequisites: [none]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** Lambda free tier: 1M requests + 400,000 GB-seconds/month. SQS free tier: 1M requests/month. DynamoDB on-demand: pennies for this exercise. Total ~$0.01/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed (`go version`)
- Basic understanding of event-driven architecture

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** the three Lambda invocation models: synchronous, asynchronous, and polling (event source mapping)
- **Identify** which AWS services use which invocation model and how error handling differs for each
- **Describe** retry behavior for each model: synchronous (caller retries), asynchronous (2 built-in retries), polling (retries until expiry or success)
- **Construct** a Lambda function using Go with the `provided.al2023` runtime
- **Compare** error handling strategies across invocation models and when to use dead-letter queues

## Why Lambda Invocation Patterns Matter

Lambda invocation patterns are among the most heavily tested serverless topics on the SAA-C03 exam. The exam does not ask you to write Lambda code, but it tests whether you understand how different AWS services invoke Lambda and what happens when the function fails. The three models have fundamentally different error handling:

**Synchronous** invocation means the caller waits for a response. API Gateway sends a request to Lambda and holds the HTTP connection open until Lambda returns. If Lambda fails, the caller (API Gateway) receives the error and the client gets a 5xx response. There are no built-in retries -- the client must retry. Services: API Gateway, ALB, CloudFront (Lambda@Edge), Cognito, Alexa.

**Asynchronous** invocation means the caller sends the event and moves on. S3 puts an event on Lambda's internal queue and does not wait for processing. Lambda automatically retries failed invocations twice (3 total attempts). After all retries fail, the event goes to a dead-letter queue (SQS) or on-failure destination (SQS, SNS, Lambda, EventBridge). Services: S3, SNS, EventBridge, CloudWatch Events, SES, CloudFormation, CodeCommit, Config.

**Polling** (event source mapping) means Lambda polls a source for records. Lambda reads batches from SQS, Kinesis, or DynamoDB Streams and invokes your function with the batch. If the function fails, the batch is retried until it succeeds, expires, or is sent to a DLQ. For ordered sources (Kinesis, DynamoDB Streams), a failed batch blocks processing of that shard until resolved. Services: SQS, Kinesis, DynamoDB Streams, Amazon MQ, MSK, DocumentDB, Self-managed Kafka.

## Step 1 -- Lambda Function Code (Go)

### `function/main.go`

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

// Handler processes any event by logging its structure.
// In production, you would use typed event structs for each source.
func Handler(ctx context.Context, event json.RawMessage) (interface{}, error) {
	log.Printf("Received event: %s", string(event))

	// Attempt to parse as API Gateway Proxy Request (synchronous)
	var apiEvent events.APIGatewayProxyRequest
	if err := json.Unmarshal(event, &apiEvent); err == nil && apiEvent.HTTPMethod != "" {
		log.Printf("API Gateway event: %s %s", apiEvent.HTTPMethod, apiEvent.Path)
		return events.APIGatewayProxyResponse{
			StatusCode: 200,
			Body:       fmt.Sprintf(`{"message":"Hello from %s %s"}`, apiEvent.HTTPMethod, apiEvent.Path),
			Headers:    map[string]string{"Content-Type": "application/json"},
		}, nil
	}

	// Attempt to parse as SQS Event (polling)
	var sqsEvent events.SQSEvent
	if err := json.Unmarshal(event, &sqsEvent); err == nil && len(sqsEvent.Records) > 0 {
		for _, record := range sqsEvent.Records {
			log.Printf("SQS message: %s", record.Body)
		}
		return fmt.Sprintf("Processed %d SQS messages", len(sqsEvent.Records)), nil
	}

	// Attempt to parse as S3 Event (asynchronous)
	var s3Event events.S3Event
	if err := json.Unmarshal(event, &s3Event); err == nil && len(s3Event.Records) > 0 {
		for _, record := range s3Event.Records {
			log.Printf("S3 event: %s on %s/%s",
				record.EventName, record.S3.Bucket.Name, record.S3.Object.Key)
		}
		return fmt.Sprintf("Processed %d S3 events", len(s3Event.Records)), nil
	}

	// Default: return the event as-is
	return map[string]string{"message": "Event processed", "type": "unknown"}, nil
}

func main() {
	lambda.Start(Handler)
}
```

### `function/go.mod`

```
module function

go 1.21

require github.com/aws/aws-lambda-go v1.47.0
```

Build for Lambda:

```bash
cd function
go mod tidy
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags lambda.norpc \
  -o bootstrap main.go
zip ../function.zip bootstrap
cd ..
```

The `provided.al2023` runtime requires the binary to be named `bootstrap`. The `lambda.norpc` build tag uses the newer, more efficient API mode.

## Step 2 -- Terraform Infrastructure

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
  default     = "saa-ex77"
}
```

### `iam.tf`

```hcl
resource "aws_iam_role" "lambda" {
  name = "${var.project_name}-lambda-role"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action    = "sts:AssumeRole"
      Effect    = "Allow"
      Principal = { Service = "lambda.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "lambda_basic" {
  role       = aws_iam_role.lambda.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_iam_role_policy" "lambda_sqs" {
  name = "${var.project_name}-sqs-policy"
  role = aws_iam_role.lambda.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = [
        "sqs:ReceiveMessage",
        "sqs:DeleteMessage",
        "sqs:GetQueueAttributes"
      ]
      Resource = aws_sqs_queue.this.arn
    }]
  })
}
```

### `lambda.tf`

```hcl
resource "aws_lambda_function" "this" {
  function_name    = "${var.project_name}-handler"
  role             = aws_iam_role.lambda.arn
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  filename         = "function.zip"
  source_code_hash = filebase64sha256("function.zip")
  timeout          = 30
  memory_size      = 128

  tags = { Name = "${var.project_name}-handler" }
}

# ---------- Synchronous: API Gateway ----------
# API Gateway invokes Lambda synchronously.
# The client waits for Lambda to finish and gets the response.
# Error handling: client receives the error and must retry.

resource "aws_lambda_permission" "apigw" {
  statement_id  = "AllowAPIGateway"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.this.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.http.execution_arn}/*/*"
}

# ---------- Asynchronous: S3 Event Notification ----------
# S3 invokes Lambda asynchronously.
# S3 sends the event and does not wait for Lambda to finish.
# Error handling: Lambda retries twice, then DLQ/destination.

resource "aws_lambda_permission" "s3" {
  statement_id  = "AllowS3"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.this.function_name
  principal     = "s3.amazonaws.com"
  source_arn    = aws_s3_bucket.trigger.arn
}

# ---------- Polling: SQS Event Source Mapping ----------
# Lambda polls SQS for messages (event source mapping).
# Lambda pulls batches, processes them, then deletes messages.
# Error handling: failed messages return to queue, eventually DLQ.

resource "aws_lambda_event_source_mapping" "sqs" {
  event_source_arn = aws_sqs_queue.this.arn
  function_name    = aws_lambda_function.this.arn
  batch_size       = 10
  enabled          = true
}
```

### `api.tf`

```hcl
resource "aws_apigatewayv2_api" "http" {
  name          = "${var.project_name}-http-api"
  protocol_type = "HTTP"
}

resource "aws_apigatewayv2_integration" "lambda" {
  api_id                 = aws_apigatewayv2_api.http.id
  integration_type       = "AWS_PROXY"
  integration_uri        = aws_lambda_function.this.invoke_arn
  payload_format_version = "2.0"
}

resource "aws_apigatewayv2_route" "default" {
  api_id    = aws_apigatewayv2_api.http.id
  route_key = "GET /hello"
  target    = "integrations/${aws_apigatewayv2_integration.lambda.id}"
}

resource "aws_apigatewayv2_stage" "default" {
  api_id      = aws_apigatewayv2_api.http.id
  name        = "$default"
  auto_deploy = true
}
```

### `events.tf`

```hcl
resource "aws_sqs_queue" "dlq" {
  name = "${var.project_name}-dlq"
}

resource "aws_sqs_queue" "this" {
  name                       = "${var.project_name}-queue"
  visibility_timeout_seconds = 180  # 6x Lambda timeout
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.dlq.arn
    maxReceiveCount     = 3
  })
}
```

### `storage.tf`

```hcl
resource "random_id" "suffix" {
  byte_length = 4
}

resource "aws_s3_bucket" "trigger" {
  bucket        = "${var.project_name}-trigger-${random_id.suffix.hex}"
  force_destroy = true
}

resource "aws_s3_bucket_notification" "lambda" {
  bucket = aws_s3_bucket.trigger.id

  lambda_function {
    lambda_function_arn = aws_lambda_function.this.arn
    events              = ["s3:ObjectCreated:*"]
    filter_prefix       = "uploads/"
  }

  depends_on = [aws_lambda_permission.s3]
}
```

### `outputs.tf`

```hcl
output "api_url" {
  value = "${aws_apigatewayv2_api.http.api_endpoint}/hello"
}

output "s3_bucket" {
  value = aws_s3_bucket.trigger.id
}

output "sqs_queue_url" {
  value = aws_sqs_queue.this.url
}
```

## Step 3 -- Deploy and Test Each Pattern

```bash
terraform init
terraform apply -auto-approve

# Test synchronous invocation (API Gateway)
API_URL=$(terraform output -raw api_url)
curl -s "$API_URL" | jq .

# Test asynchronous invocation (S3)
BUCKET=$(terraform output -raw s3_bucket)
echo "test data" | aws s3 cp - "s3://${BUCKET}/uploads/test.txt"
sleep 5
aws logs tail "/aws/lambda/saa-ex77-handler" --since 1m --format short

# Test polling invocation (SQS)
QUEUE_URL=$(terraform output -raw sqs_queue_url)
aws sqs send-message --queue-url "$QUEUE_URL" \
  --message-body '{"order_id":"12345","amount":99.99}'
sleep 5
aws logs tail "/aws/lambda/saa-ex77-handler" --since 1m --format short
```

## Invocation Model Reference

| Model | Who Invokes | Caller Waits | Built-in Retries | Error Destination |
|---|---|---|---|---|
| **Synchronous** | API GW, ALB, CloudFront, Cognito | Yes | None (caller retries) | N/A (error returned to caller) |
| **Asynchronous** | S3, SNS, EventBridge, SES, Config | No | 2 retries (3 total) | DLQ (SQS) or on-failure destination |
| **Polling** | Lambda (ESM) from SQS, Kinesis, DDB Streams | N/A | Until success, expiry, or DLQ | Source DLQ (SQS redrive) or bisect batch |

### Retry Behavior Deep Dive

| Source | Retry Mechanism | Ordering | Failure Impact |
|---|---|---|---|
| **SQS** | Message returns to queue after visibility timeout | No ordering guarantee | Other messages continue processing |
| **SQS FIFO** | Retries within message group | Strict per message group | Blocks message group until resolved |
| **Kinesis** | Retries entire batch | Per-shard ordering | Blocks shard until success or expiry |
| **DynamoDB Streams** | Retries entire batch | Per-shard ordering | Blocks shard until success or expiry |

## Common Mistakes

### 1. SQS visibility timeout too short

**Wrong configuration:**

```hcl
resource "aws_sqs_queue" "this" {
  visibility_timeout_seconds = 30  # Same as Lambda timeout
}

resource "aws_lambda_function" "this" {
  timeout = 30
}
```

**What happens:** If Lambda takes 29 seconds to process a message and then fails, the message becomes visible again after 30 seconds. But Lambda might still be processing it (it has 1 second left). Two Lambda invocations process the same message simultaneously.

**Fix:** Set the SQS visibility timeout to at least 6 times the Lambda function timeout:

```hcl
resource "aws_sqs_queue" "this" {
  visibility_timeout_seconds = 180  # 6x Lambda timeout of 30s
}
```

### 2. Confusing synchronous and asynchronous error handling

**Wrong assumption:** "Lambda automatically retries when my API Gateway request fails."

**What actually happens:** API Gateway invokes Lambda synchronously. If Lambda fails, the error is returned to the client as a 5xx response. There are no automatic retries. The client (or a retry layer like API Gateway retry configuration) must retry.

For asynchronous invocations (S3, SNS), Lambda does retry automatically -- twice. This is a common exam trap.

### 3. Missing DLQ for asynchronous invocations

**Wrong approach:** No DLQ configured for asynchronous Lambda.

```hcl
resource "aws_lambda_function" "this" {
  # No dead_letter_config
}
```

**What happens:** After 3 failed attempts (1 original + 2 retries), the event is silently dropped. You have no record of the failure and no way to reprocess.

**Fix:** Always configure a DLQ for asynchronous Lambda functions:

```hcl
resource "aws_lambda_function" "this" {
  dead_letter_config {
    target_arn = aws_sqs_queue.dlq.arn
  }
}
```

Or use Lambda Destinations (newer, preferred) for on-failure routing.

## Verify What You Learned

```bash
# Verify synchronous invocation works
API_URL=$(terraform output -raw api_url)
curl -s "$API_URL"
```

Expected: `{"message":"Hello from GET /hello"}`

```bash
# Verify asynchronous invocation (check CloudWatch logs after S3 upload)
BUCKET=$(terraform output -raw s3_bucket)
echo "verify test" | aws s3 cp - "s3://${BUCKET}/uploads/verify.txt"
sleep 10
aws logs tail "/aws/lambda/saa-ex77-handler" --since 2m --format short | grep "S3 event"
```

Expected: Log line showing `S3 event: ObjectCreated:Put on <bucket>/uploads/verify.txt`

```bash
# Verify polling invocation (SQS)
QUEUE_URL=$(terraform output -raw sqs_queue_url)
aws sqs send-message --queue-url "$QUEUE_URL" --message-body '{"test":"verify"}'
sleep 10
aws logs tail "/aws/lambda/saa-ex77-handler" --since 2m --format short | grep "SQS message"
```

Expected: Log line showing `SQS message: {"test":"verify"}`

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify:

```bash
aws lambda get-function --function-name saa-ex77-handler 2>&1 | grep -q "ResourceNotFoundException" && echo "Lambda deleted" || echo "Still exists"
```

## What's Next

Exercise 78 covers **API Gateway types: REST vs HTTP vs WebSocket**. You will deploy all three API types for the same Lambda backend and understand the decision framework: REST API for full features (request validation, caching, WAF integration), HTTP API for simpler and cheaper deployments, and WebSocket API for bidirectional real-time communication.

## Summary

- **Synchronous** invocation: caller waits for response, no built-in retries, error returned to caller (API Gateway, ALB, CloudFront)
- **Asynchronous** invocation: caller sends event and moves on, 2 automatic retries, then DLQ/destination (S3, SNS, EventBridge)
- **Polling** (event source mapping): Lambda polls source for batches, retries until success or expiry (SQS, Kinesis, DynamoDB Streams)
- **SQS visibility timeout** must be >= 6x the Lambda function timeout to prevent duplicate processing
- **Kinesis and DynamoDB Streams** block per-shard processing on failure -- use bisect batch on error or max retry attempts to prevent stuck shards
- **DLQ for async Lambda** prevents silent event loss after retry exhaustion
- **Lambda Destinations** (on-success, on-failure) provide richer routing than DLQ for asynchronous invocations
- **`provided.al2023`** runtime for Go requires binary named `bootstrap` with `GOOS=linux GOARCH=amd64`
- **Event source mapping** requires IAM permissions to read from the source (SQS: ReceiveMessage, DeleteMessage, GetQueueAttributes)
- **Batch size** controls how many records Lambda processes per invocation -- larger batches reduce invocations but increase per-invocation duration

## Reference

- [Lambda Invocation Types](https://docs.aws.amazon.com/lambda/latest/dg/lambda-invocation.html)
- [Lambda Event Source Mappings](https://docs.aws.amazon.com/lambda/latest/dg/invocation-eventsourcemapping.html)
- [Lambda Dead-Letter Queues](https://docs.aws.amazon.com/lambda/latest/dg/invocation-async.html#invocation-dlq)
- [Terraform aws_lambda_event_source_mapping](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_event_source_mapping)

## Additional Resources

- [Lambda Destinations](https://docs.aws.amazon.com/lambda/latest/dg/invocation-async.html#invocation-async-destinations) -- routing success and failure to SQS, SNS, Lambda, or EventBridge
- [SQS as Event Source](https://docs.aws.amazon.com/lambda/latest/dg/with-sqs.html) -- detailed SQS polling behavior and batch processing
- [Kinesis as Event Source](https://docs.aws.amazon.com/lambda/latest/dg/with-kinesis.html) -- shard-level parallelism and bisect-on-error strategies
- [Go Lambda Runtime](https://docs.aws.amazon.com/lambda/latest/dg/lambda-golang.html) -- building Go functions for provided.al2023

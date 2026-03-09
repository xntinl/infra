# 53. SQS Temporary Queues and the Request-Reply Pattern

<!--
difficulty: intermediate
concepts: [request-reply-pattern, temporary-queue, virtual-queue, reply-to-attribute, message-correlation, sqs-cleanup]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: apply
prerequisites: [08-sqs-lambda-concurrency-throttling]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates SQS queues and Lambda functions. SQS pricing is $0.40 per million requests. Cost is approximately $0.01/hr for testing volumes. Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| jq installed | `jq --version` |

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** the request-reply pattern using SQS queues with `ReplyTo` message attributes and correlation IDs
- **Configure** temporary response queues with short retention periods for transient request-reply exchanges
- **Apply** message attribute-based routing to correlate responses with their originating requests
- **Diagnose** resource leaks caused by reply queues not being deleted after use
- **Differentiate** between the request-reply pattern and simple pub/sub for synchronous-style communication over async messaging

## Why the Request-Reply Pattern

Microservices often need synchronous-style request-response communication over asynchronous messaging. A client sends a request to a service via SQS and needs a response. The client creates a temporary reply queue, includes the reply queue URL as a message attribute, and waits for the response on that queue. The server processes the request, reads the `ReplyTo` attribute, and sends the response to the specified queue.

The DVA-C02 exam tests this pattern because it combines several SQS concepts: message attributes, queue creation, polling, and resource cleanup. The most common production issue is **resource leaks** -- if the client creates a reply queue for each request but does not delete it after receiving the response, the account accumulates thousands of orphaned queues. AWS recommends using the Temporary Queue Client (which creates virtual queues backed by a single physical host queue) or implementing strict cleanup logic. On the exam, if you see a scenario about "growing number of SQS queues" or "SQS queue limit reached," the answer is almost always a missing cleanup step in the request-reply pattern.

## Building Blocks

Create the following files in your exercise directory. Your job is to fill in each `# TODO` block.

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
  default     = "request-reply-demo"
}
```

### `events.tf`

```hcl
# -------------------------------------------------------
# Request Queue (where clients send requests)
# -------------------------------------------------------
resource "aws_sqs_queue" "requests" {
  name                       = "${var.project_name}-requests"
  visibility_timeout_seconds = 30
  message_retention_seconds  = 3600
}

# =======================================================
# TODO 1 -- Create a Server Lambda that processes requests
#           and sends replies
# =======================================================
# Requirements:
#   - Lambda reads from the request queue via event source mapping
#   - For each message, read the "ReplyToQueueUrl" message attribute
#   - Read the "CorrelationId" message attribute
#   - Process the request and send the response to the
#     ReplyTo queue URL with the same CorrelationId
#   - IAM policy must include sqs:SendMessage for reply queues
#     (use wildcard ARN since reply queue names vary)
#
# Handler code outline (implement in handler/main.go):
#   1. Parse SQS event records
#   2. Extract ReplyToQueueUrl from message attributes
#   3. Extract CorrelationId from message attributes
#   4. Process the request (e.g., calculate price)
#   5. Send response to ReplyToQueueUrl with CorrelationId


# =======================================================
# TODO 2 -- Create an Event Source Mapping for the server
# =======================================================
# Requirements:
#   - Connect the request queue to the server Lambda
#   - batch_size = 1 (process one request at a time)
#   - function_response_types = ["ReportBatchItemFailures"]


# =======================================================
# TODO 3 -- Create a Client Go program that:
# =======================================================
# Requirements:
#   - Creates a temporary reply queue with a unique name
#   - Sets message_retention_seconds to 300 (5 minutes)
#   - Sends a request to the request queue with:
#     - MessageBody: the request payload
#     - MessageAttribute "ReplyToQueueUrl": the reply queue URL
#     - MessageAttribute "CorrelationId": a UUID
#   - Polls the reply queue for the response (long polling)
#   - Deletes the reply queue after receiving the response
#
# IMPORTANT: The reply queue MUST be deleted after use.
#   Failure to delete creates resource leaks.
```

### `iam.tf`

```hcl
# -------------------------------------------------------
# IAM Role for Server Lambda
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

resource "aws_iam_role" "server" {
  name               = "${var.project_name}-server-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

resource "aws_iam_role_policy_attachment" "server_basic" {
  role       = aws_iam_role.server.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

data "aws_iam_policy_document" "server_permissions" {
  statement {
    actions   = ["sqs:ReceiveMessage", "sqs:DeleteMessage", "sqs:GetQueueAttributes"]
    resources = [aws_sqs_queue.requests.arn]
  }
  statement {
    actions   = ["sqs:SendMessage"]
    resources = ["arn:aws:sqs:*:*:${var.project_name}-reply-*"]
  }
}

resource "aws_iam_role_policy" "server_permissions" {
  name   = "server-permissions"
  role   = aws_iam_role.server.id
  policy = data.aws_iam_policy_document.server_permissions.json
}
```

### `outputs.tf`

```hcl
output "request_queue_url" {
  value = aws_sqs_queue.requests.url
}

output "function_name" {
  value = aws_lambda_function.server.function_name
}
```

### `handler/main.go`

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

var sqsClient *sqs.Client

func init() {
	cfg, _ := config.LoadDefaultConfig(context.TODO())
	sqsClient = sqs.NewFromConfig(cfg)
}

func handler(ctx context.Context, event events.SQSEvent) (events.SQSEventResponse, error) {
	var failures []events.SQSBatchItemFailure
	for _, record := range event.Records {
		if err := processRequest(ctx, record); err != nil {
			failures = append(failures, events.SQSBatchItemFailure{ItemIdentifier: record.MessageId})
		}
	}
	return events.SQSEventResponse{BatchItemFailures: failures}, nil
}

func processRequest(ctx context.Context, record events.SQSMessage) error {
	// Extract ReplyToQueueUrl and CorrelationId from message attributes
	replyTo, correlationId := "", ""
	for key, attr := range record.MessageAttributes {
		if key == "ReplyToQueueUrl" { replyTo = *attr.StringValue }
		if key == "CorrelationId" { correlationId = *attr.StringValue }
	}
	if replyTo == "" { return fmt.Errorf("missing ReplyToQueueUrl") }

	// Process request and build response
	response, _ := json.Marshal(map[string]interface{}{
		"status": "success", "correlation_id": correlationId,
		"result": fmt.Sprintf("Processed: %s", record.Body),
	})

	// Send response to the ReplyTo queue with CorrelationId
	_, err := sqsClient.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl: aws.String(replyTo), MessageBody: aws.String(string(response)),
		MessageAttributes: map[string]types.MessageAttributeValue{
			"CorrelationId": {DataType: aws.String("String"), StringValue: aws.String(correlationId)},
		},
	})
	return err
}

func main() { lambda.Start(handler) }
```

## Spot the Bug

A developer implements the request-reply pattern. After a week of production use, they hit the SQS queue limit (account maximum of 1,000 queues) and requests start failing.

```go
func sendRequest(ctx context.Context, client *sqs.Client, requestQueueURL string, payload string) (string, error) {
    // Create a reply queue for this request
    correlationId := uuid.New().String()
    replyQueueName := fmt.Sprintf("reply-%s", correlationId)

    createResult, _ := client.CreateQueue(ctx, &sqs.CreateQueueInput{
        QueueName: aws.String(replyQueueName),
        Attributes: map[string]string{
            "MessageRetentionPeriod": "300",
        },
    })
    replyQueueURL := *createResult.QueueUrl

    // Send request with ReplyTo
    client.SendMessage(ctx, &sqs.SendMessageInput{
        QueueUrl:    aws.String(requestQueueURL),
        MessageBody: aws.String(payload),
        MessageAttributes: map[string]types.MessageAttributeValue{
            "ReplyToQueueUrl": {DataType: aws.String("String"), StringValue: aws.String(replyQueueURL)},
            "CorrelationId":   {DataType: aws.String("String"), StringValue: aws.String(correlationId)},
        },
    })

    // Poll for response
    for i := 0; i < 10; i++ {
        result, _ := client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
            QueueUrl:        aws.String(replyQueueURL),
            WaitTimeSeconds: 2,
        })
        if len(result.Messages) > 0 {
            return *result.Messages[0].Body, nil
        }
    }

    return "", fmt.Errorf("timeout waiting for reply")
}
```

<details>
<summary>Explain the bug</summary>

The reply queue is **never deleted** after receiving the response. Each request creates a new queue, but no code path calls `DeleteQueue`. Over time, this creates thousands of orphaned queues, eventually hitting the account limit (1,000 standard queues by default, or 10,000 with a limit increase).

Even setting `MessageRetentionPeriod: 300` does not help -- this controls how long messages stay in the queue, not the queue's lifetime. SQS queues persist indefinitely until explicitly deleted.

**Fix -- add `defer` cleanup immediately after queue creation:**

```go
// CRITICAL: Always delete the reply queue, even on error or timeout
defer func() {
    client.DeleteQueue(ctx, &sqs.DeleteQueueInput{QueueUrl: aws.String(replyQueueURL)})
}()
```

For production systems, use the **Temporary Queue Client** pattern (virtual queues backed by a single host queue) or a shared response queue with CorrelationId-based filtering.

</details>

## Verify What You Learned

```bash
# Verify request queue exists
aws sqs get-queue-url --queue-name request-reply-demo-requests \
  --query "QueueUrl" --output text
```

Expected: the queue URL.

```bash
# Verify Lambda event source mapping
aws lambda list-event-source-mappings \
  --function-name $(terraform output -raw function_name) \
  --query "EventSourceMappings[0].{BatchSize:BatchSize,State:State}" --output table
```

Expected: `BatchSize=1`, `State=Enabled`.

```bash
# Send a test request with ReplyTo
REQUEST_URL=$(terraform output -raw request_queue_url)
REPLY_QUEUE_NAME="request-reply-demo-reply-test-$(date +%s)"

# Create temp reply queue
REPLY_URL=$(aws sqs create-queue --queue-name "$REPLY_QUEUE_NAME" \
  --attributes '{"MessageRetentionPeriod":"300"}' \
  --query "QueueUrl" --output text)

# Send request
aws sqs send-message --queue-url "$REQUEST_URL" \
  --message-body '{"action":"get_price","item":"widget"}' \
  --message-attributes "{
    \"ReplyToQueueUrl\":{\"DataType\":\"String\",\"StringValue\":\"$REPLY_URL\"},
    \"CorrelationId\":{\"DataType\":\"String\",\"StringValue\":\"test-123\"}
  }"

# Wait and receive reply
sleep 10
aws sqs receive-message --queue-url "$REPLY_URL" \
  --max-number-of-messages 1 --wait-time-seconds 10 \
  --query "Messages[0].Body" --output text | jq .

# Clean up reply queue
aws sqs delete-queue --queue-url "$REPLY_URL"
```

Expected: JSON response with `status: success` and `correlation_id: test-123`.

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Solutions

<details>
<summary>TODO 1 -- Server Lambda (`events.tf`)</summary>

Standard Go Lambda build + deploy pattern: `null_resource` for `go build`, `archive_file` for ZIP, `aws_lambda_function` with `runtime = "provided.al2023"`, `handler = "bootstrap"`, `architectures = ["arm64"]`, `timeout = 30`. See exercise 01 for the full template.

</details>

<details>
<summary>TODO 2 -- Event Source Mapping (`events.tf`)</summary>

```hcl
resource "aws_lambda_event_source_mapping" "requests" {
  event_source_arn        = aws_sqs_queue.requests.arn
  function_name           = aws_lambda_function.server.arn
  batch_size              = 1
  function_response_types = ["ReportBatchItemFailures"]
}
```

</details>

<details>
<summary>TODO 3 -- Client Program (`client/main.go`)</summary>

Key steps: (1) `CreateQueue` with `MessageRetentionPeriod: 300` and a unique name; (2) `defer DeleteQueue` immediately after creation; (3) `SendMessage` to the request queue with `ReplyToQueueUrl` and `CorrelationId` as message attributes; (4) long-poll `ReceiveMessage` on the reply queue with a timeout; (5) the `defer` ensures cleanup on all code paths.

```go
// Critical: always delete the reply queue
defer func() {
    client.DeleteQueue(ctx, &sqs.DeleteQueueInput{QueueUrl: aws.String(replyQueueURL)})
}()
```

</details>

## Cleanup

Destroy all resources to stop incurring charges:

```bash
terraform destroy -auto-approve
```

Clean up any orphaned reply queues:

```bash
aws sqs list-queues --queue-name-prefix "request-reply-demo-reply" \
  --query "QueueUrls[]" --output text | tr '\t' '\n' | \
  xargs -I{} aws sqs delete-queue --queue-url {}
```

Verify nothing remains:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

You implemented the request-reply pattern with temporary queues, correlation IDs, and cleanup logic. In the next exercise, you will build **EventBridge Pipes with transformation** -- connecting SQS to Step Functions with filtering and Lambda enrichment stages.

## Summary

- The **request-reply pattern** uses a temporary reply queue with `ReplyTo` and `CorrelationId` message attributes
- Reply queues must be **explicitly deleted** after receiving the response -- SQS queues persist indefinitely
- **Resource leaks** from orphaned reply queues can exhaust the account queue limit (1,000 default, 10,000 max)
- Use `defer` or equivalent cleanup to ensure deletion on all code paths (success, error, timeout)
- `MessageRetentionPeriod` controls **message** lifetime, not **queue** lifetime -- queues outlive their messages
- For high-throughput systems, prefer a **shared response queue** with CorrelationId filtering over per-request queues
- The **Temporary Queue Client** (AWS library) creates virtual queues backed by a single host queue, eliminating physical queue overhead

## Reference

- [SQS Temporary Queue Client](https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/sqs-temporary-queues.html)
- [SQS Message Attributes](https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/sqs-message-metadata.html)
- [Terraform aws_sqs_queue](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sqs_queue)
- [Terraform aws_lambda_event_source_mapping](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_event_source_mapping)

## Additional Resources

- [Request-Reply Pattern](https://www.enterpriseintegrationpatterns.com/patterns/messaging/RequestReply.html) -- the foundational messaging pattern this exercise implements
- [SQS Quotas](https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/quotas-queues.html) -- queue limits per account and region
- [Correlation Identifier Pattern](https://www.enterpriseintegrationpatterns.com/patterns/messaging/CorrelationIdentifier.html) -- matching responses to their originating requests

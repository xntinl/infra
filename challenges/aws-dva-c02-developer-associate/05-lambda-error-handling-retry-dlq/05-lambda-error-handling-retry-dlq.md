# 5. Lambda Error Handling, Retries, and Dead Letter Queues

<!--
difficulty: intermediate
concepts: [lambda-async-invoke, retry-attempts, event-age, dlq, lambda-destinations, sqs-redrive-policy]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: design, justify, implement
prerequisites: [none]
aws_cost: ~$0.01/hr
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

1. **Design** a fault-tolerant event processing pipeline using Lambda retry policies and dead letter queues
2. **Justify** when to use a DLQ versus Lambda Destinations for failure handling
3. **Implement** async invocation configuration with controlled retry attempts and maximum event age
4. **Configure** SQS-based event source mappings with partial batch failure reporting
5. **Differentiate** between synchronous error handling, async retry behavior, and event source mapping retries

## Why This Matters

Lambda functions fail. Network timeouts, downstream service outages, bad input data -- in production you must decide what happens to events that cannot be processed. AWS provides three distinct failure-handling mechanisms and the DVA-C02 exam tests all of them: async invocation retries (built into the Lambda service), DLQs (SQS or SNS targets for failed async events), and Lambda Destinations (richer metadata about failures sent to SQS, SNS, Lambda, or EventBridge). Using the wrong mechanism -- or none at all -- means silent data loss.

The exam specifically tests the difference between DLQs and Destinations. A DLQ receives only the original event payload after all retries are exhausted. A Destination wraps the event in a response object that includes the error message, stack trace, and request context -- giving you enough information to debug and replay failures without guessing. When Lambda processes SQS messages via an event source mapping, the retry behavior is completely different: the SQS visibility timeout controls retries, and you must use `ReportBatchItemFailures` to avoid reprocessing successful messages in a failed batch. Mastering all three patterns is non-negotiable for the exam and for building production serverless systems.

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
  default     = "lambda-errors-lab"
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

data "aws_iam_policy_document" "lambda_sqs" {
  statement {
    actions = [
      "sqs:SendMessage",
      "sqs:ReceiveMessage",
      "sqs:DeleteMessage",
      "sqs:GetQueueAttributes",
    ]
    resources = ["*"]
  }
}

resource "aws_iam_role_policy" "lambda_sqs" {
  name   = "${var.project_name}-sqs-policy"
  role   = aws_iam_role.lambda.id
  policy = data.aws_iam_policy_document.lambda_sqs.json
}
```

### `lambda.tf`

```hcl
# -------------------------------------------------------
# Lambda function that deliberately fails
# -------------------------------------------------------
# NOTE: For Go Lambdas, build the binary externally and reference the zip.
#   GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
#   zip failing.zip bootstrap
data "archive_file" "failing" {
  type        = "zip"
  output_path = "${path.module}/failing.zip"

  source {
    content  = <<-GO
package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-lambda-go/lambda"
)

type Event struct {
	Body       string `json:"body,omitempty"`
	ShouldFail bool   `json:"should_fail,omitempty"`
}

type Response struct {
	StatusCode int    `json:"statusCode"`
	Body       string `json:"body"`
}

func handler(ctx context.Context, raw json.RawMessage) (Response, error) {
	// If the event contains {"should_fail": true}, return an error.
	// Otherwise, return success.
	var event Event
	if err := json.Unmarshal(raw, &event); err != nil {
		return Response{}, fmt.Errorf("failed to unmarshal event: %w", err)
	}

	// Handle API Gateway proxy event with body as string
	if event.Body != "" {
		var body Event
		if err := json.Unmarshal([]byte(event.Body), &body); err == nil {
			event = body
		}
	}

	if event.ShouldFail {
		bodyBytes, _ := json.Marshal(event)
		return Response{}, fmt.Errorf("deliberate failure for testing: %s", string(bodyBytes))
	}

	respBody, _ := json.Marshal(map[string]interface{}{
		"message": "Success",
		"input":   event,
	})

	return Response{
		StatusCode: 200,
		Body:       string(respBody),
	}, nil
}

func main() {
	lambda.Start(handler)
}
GO
    filename = "main.go"
  }
}

resource "aws_lambda_function" "failing" {
  function_name    = "${var.project_name}-failing"
  role             = aws_iam_role.lambda.arn
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  timeout          = 10
  filename         = data.archive_file.failing.output_path
  source_code_hash = data.archive_file.failing.output_base64sha256

  tags = {
    Name = "${var.project_name}-failing"
  }
}

# -------------------------------------------------------
# Lambda function for SQS batch processing
# -------------------------------------------------------
# NOTE: For Go Lambdas, build the binary externally and reference the zip.
#   GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
#   zip batch_processor.zip bootstrap
data "archive_file" "batch_processor" {
  type        = "zip"
  output_path = "${path.module}/batch_processor.zip"

  source {
    content  = <<-GO
package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

type MessageBody struct {
	ID         int  `json:"id,omitempty"`
	ShouldFail bool `json:"should_fail,omitempty"`
}

// Process SQS batch. Reports individual item failures
// using the batchItemFailures response format.
func handler(ctx context.Context, event events.SQSEvent) (events.SQSEventResponse, error) {
	var batchItemFailures []events.SQSBatchItemFailure

	for _, record := range event.Records {
		var body MessageBody
		if err := json.Unmarshal([]byte(record.Body), &body); err != nil {
			fmt.Printf("Error unmarshaling record %s: %v\n", record.MessageId, err)
			batchItemFailures = append(batchItemFailures, events.SQSBatchItemFailure{
				ItemIdentifier: record.MessageId,
			})
			continue
		}

		if body.ShouldFail {
			fmt.Printf("Error processing record %s: failed to process: %+v\n", record.MessageId, body)
			batchItemFailures = append(batchItemFailures, events.SQSBatchItemFailure{
				ItemIdentifier: record.MessageId,
			})
			continue
		}

		fmt.Printf("Successfully processed: %+v\n", body)
	}

	return events.SQSEventResponse{
		BatchItemFailures: batchItemFailures,
	}, nil
}

func main() {
	lambda.Start(handler)
}
GO
    filename = "main.go"
  }
}

resource "aws_lambda_function" "batch_processor" {
  function_name    = "${var.project_name}-batch-processor"
  role             = aws_iam_role.lambda.arn
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  timeout          = 30
  filename         = data.archive_file.batch_processor.output_path
  source_code_hash = data.archive_file.batch_processor.output_base64sha256

  tags = {
    Name = "${var.project_name}-batch-processor"
  }
}

# =======================================================
# TODO 1 -- Async Invocation Configuration
# =======================================================
# Requirements:
#   - Create an aws_lambda_function_event_invoke_config for
#     the "failing" Lambda function
#   - Set maximum_retry_attempts to 1 (default is 2)
#   - Set maximum_event_age_in_seconds to 60 (default is 21600)
#   - This controls what happens when the function is invoked
#     asynchronously (e.g., via S3, SNS, EventBridge triggers)
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_function_event_invoke_config


# =======================================================
# TODO 2 -- Dead Letter Queue on Lambda
# =======================================================
# Requirements:
#   - Update the aws_lambda_function "failing" resource to
#     include a dead_letter_config block
#   - Set target_arn to the DLQ SQS queue ARN
#   - The DLQ receives the ORIGINAL event payload after all
#     async retries are exhausted
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_function#dead_letter_config
# Hint: Add this block inside the aws_lambda_function "failing" resource above


# =======================================================
# TODO 3 -- On-Failure Destination
# =======================================================
# Requirements:
#   - Add a destination_config block to the event_invoke_config
#     from TODO 1
#   - Configure an on_failure destination pointing to the
#     destination_queue SQS queue ARN
#   - Destinations provide richer failure metadata than DLQs:
#     error message, stack trace, request/response context
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_function_event_invoke_config#destination_config
# Note: When both DLQ and on_failure destination are configured,
#       BOTH receive the failure notification
```

### `events.tf`

```hcl
# -------------------------------------------------------
# SQS Queues
# -------------------------------------------------------
resource "aws_sqs_queue" "dlq" {
  name                      = "${var.project_name}-dlq"
  message_retention_seconds = 1209600  # 14 days

  tags = {
    Name = "${var.project_name}-dlq"
  }
}

resource "aws_sqs_queue" "destination_queue" {
  name                      = "${var.project_name}-on-failure"
  message_retention_seconds = 1209600

  tags = {
    Name = "${var.project_name}-on-failure"
  }
}

resource "aws_sqs_queue" "batch_dlq" {
  name                      = "${var.project_name}-batch-dlq"
  message_retention_seconds = 1209600

  tags = {
    Name = "${var.project_name}-batch-dlq"
  }
}

# =======================================================
# TODO 4 -- SQS Source Queue with Redrive Policy
# =======================================================
# Requirements:
#   - Create an aws_sqs_queue named "${var.project_name}-source"
#   - Set visibility_timeout_seconds to 180 (6x Lambda timeout)
#   - Add a redrive_policy pointing to batch_dlq
#   - Set maxReceiveCount to 3 (after 3 failed processing
#     attempts, messages move to the DLQ)
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sqs_queue#redrive_policy
# Hint: redrive_policy is a JSON string with deadLetterTargetArn and maxReceiveCount


# =======================================================
# TODO 5 -- SQS Event Source Mapping with Partial Batch Response
# =======================================================
# Requirements:
#   - Create an aws_lambda_event_source_mapping connecting
#     the source queue (TODO 4) to the batch_processor Lambda
#   - Set batch_size to 5
#   - Set maximum_batching_window_in_seconds to 10
#   - Set function_response_types to ["ReportBatchItemFailures"]
#     This tells Lambda to check the response for individual
#     item failures instead of treating the entire batch as failed
#   - Enabled = true
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_event_source_mapping
```

### `outputs.tf`

```hcl
output "failing_function_name" {
  value = aws_lambda_function.failing.function_name
}

output "batch_processor_function_name" {
  value = aws_lambda_function.batch_processor.function_name
}

output "dlq_url" {
  value = aws_sqs_queue.dlq.url
}

output "destination_queue_url" {
  value = aws_sqs_queue.destination_queue.url
}

output "source_queue_url" {
  value = aws_sqs_queue.source.url
}

output "batch_dlq_url" {
  value = aws_sqs_queue.batch_dlq.url
}
```

## Spot the Bug

A developer configured a DLQ for their Lambda function but no failed events ever appear in the DLQ. They are testing by calling the function with the AWS CLI. **What is wrong?**

```go
// Test invocation:
// aws lambda invoke --function-name my-func \
//   --payload '{"should_fail": true}' response.json
```

```hcl
resource "aws_lambda_function" "my_func" {
  function_name = "my-func"
  # ... other config ...

  dead_letter_config {
    target_arn = aws_sqs_queue.dlq.arn
  }
}
```

<details>
<summary>Explain the bug</summary>

The DLQ only works for **asynchronous** invocations. The `aws lambda invoke` command performs a **synchronous** invocation by default. The error is returned directly to the caller, and the DLQ is never triggered.

To test async invocation, you must add `--invocation-type Event`:

```bash
aws lambda invoke --function-name my-func \
  --invocation-type Event \
  --payload '{"should_fail": true}' response.json
```

With `--invocation-type Event`, Lambda queues the event internally. If the function fails after all retries, the original event is sent to the DLQ. With the default synchronous invocation (`RequestResponse`), you get the error back immediately and the DLQ is bypassed entirely.

This is one of the most common DVA-C02 exam traps: understanding that DLQs and async retry configuration only apply to async invocations (Event type), not synchronous calls.

</details>

## Verify What You Learned

### Step 1 -- Apply the infrastructure

```
terraform init && terraform apply -auto-approve
```

### Step 2 -- Test async invocation with failure

Invoke the function asynchronously with a failing payload:

```
aws lambda invoke \
  --function-name $(terraform output -raw failing_function_name) \
  --invocation-type Event \
  --payload '{"should_fail": true}' \
  /dev/null
```

Expected: HTTP 202 (accepted, not processed yet).

### Step 3 -- Wait for retries, then check the DLQ

Wait approximately 2 minutes for the initial attempt + 1 retry to complete:

```
sleep 120
aws sqs get-queue-attributes \
  --queue-url $(terraform output -raw dlq_url) \
  --attribute-names ApproximateNumberOfMessages \
  --query "Attributes.ApproximateNumberOfMessages" \
  --output text
```

Expected output: `1` (one failed event landed in the DLQ).

### Step 4 -- Check the on-failure destination

```
aws sqs receive-message \
  --queue-url $(terraform output -raw destination_queue_url) \
  --max-number-of-messages 1 \
  --query "Messages[0].Body" \
  --output text | jq .
```

Expected: A JSON object containing `requestContext`, `requestPayload`, `responseContext`, and `responsePayload` with the error details. Note how this is richer than the DLQ message (which only contains the raw event).

### Step 5 -- Test partial batch failure with SQS

Send a mix of good and bad messages to the source queue:

```
SOURCE_URL=$(terraform output -raw source_queue_url)
aws sqs send-message --queue-url "$SOURCE_URL" --message-body '{"id": 1, "should_fail": false}'
aws sqs send-message --queue-url "$SOURCE_URL" --message-body '{"id": 2, "should_fail": true}'
aws sqs send-message --queue-url "$SOURCE_URL" --message-body '{"id": 3, "should_fail": false}'
```

Wait 30 seconds, then check the batch DLQ:

```
sleep 30
aws sqs get-queue-attributes \
  --queue-url $(terraform output -raw batch_dlq_url) \
  --attribute-names ApproximateNumberOfMessages \
  --query "Attributes.ApproximateNumberOfMessages" \
  --output text
```

After the `maxReceiveCount` (3) is exceeded for message 2, it should appear in the batch DLQ. Messages 1 and 3 should be successfully processed and deleted.

### Step 6 -- Verify Lambda event invoke configuration

```
aws lambda get-function-event-invoke-config \
  --function-name $(terraform output -raw failing_function_name) \
  --query "{MaxRetry:MaximumRetryAttempts,MaxAge:MaximumEventAgeInSeconds,OnFailure:DestinationConfig.OnFailure.Destination}" \
  --output table
```

Expected:

```
-----------------------------------------------------------------
|              GetFunctionEventInvokeConfig                     |
+----------+-------------------+--------------------------------+
| MaxAge   |    MaxRetry       |          OnFailure             |
+----------+-------------------+--------------------------------+
|  60      |    1              | arn:aws:sqs:...:on-failure     |
+----------+-------------------+--------------------------------+
```

## Solutions

<details>
<summary>TODO 1 -- Async Invocation Configuration (lambda.tf)</summary>

```hcl
resource "aws_lambda_function_event_invoke_config" "failing" {
  function_name          = aws_lambda_function.failing.function_name
  maximum_retry_attempts = 1
  maximum_event_age_in_seconds = 60
}
```

</details>

<details>
<summary>TODO 2 -- Dead Letter Queue on Lambda (lambda.tf)</summary>

Add this block inside the `aws_lambda_function "failing"` resource:

```hcl
resource "aws_lambda_function" "failing" {
  function_name    = "${var.project_name}-failing"
  role             = aws_iam_role.lambda.arn
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  timeout          = 10
  filename         = data.archive_file.failing.output_path
  source_code_hash = data.archive_file.failing.output_base64sha256

  dead_letter_config {
    target_arn = aws_sqs_queue.dlq.arn
  }

  tags = {
    Name = "${var.project_name}-failing"
  }
}
```

</details>

<details>
<summary>TODO 3 -- On-Failure Destination (lambda.tf)</summary>

Update the event invoke config from TODO 1 to include a destination:

```hcl
resource "aws_lambda_function_event_invoke_config" "failing" {
  function_name                = aws_lambda_function.failing.function_name
  maximum_retry_attempts       = 1
  maximum_event_age_in_seconds = 60

  destination_config {
    on_failure {
      destination = aws_sqs_queue.destination_queue.arn
    }
  }
}
```

</details>

<details>
<summary>TODO 4 -- SQS Source Queue with Redrive Policy (events.tf)</summary>

```hcl
resource "aws_sqs_queue" "source" {
  name                       = "${var.project_name}-source"
  visibility_timeout_seconds = 180

  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.batch_dlq.arn
    maxReceiveCount     = 3
  })

  tags = {
    Name = "${var.project_name}-source"
  }
}
```

</details>

<details>
<summary>TODO 5 -- SQS Event Source Mapping (events.tf)</summary>

```hcl
resource "aws_lambda_event_source_mapping" "sqs_to_batch" {
  event_source_arn                   = aws_sqs_queue.source.arn
  function_name                      = aws_lambda_function.batch_processor.arn
  batch_size                         = 5
  maximum_batching_window_in_seconds = 10
  enabled                            = true

  function_response_types = ["ReportBatchItemFailures"]
}
```

</details>

## Cleanup

Destroy all resources:

```
terraform destroy -auto-approve
```

Verify the queues are deleted:

```
aws sqs list-queues \
  --queue-name-prefix "lambda-errors-lab" \
  --query "QueueUrls" --output text
```

This should return empty output.

## What's Next

In **Exercise 06 -- SAM Local Development, Packaging, and Deployment**, you will use the SAM CLI to build, test locally, and deploy Lambda functions without Terraform, understanding how SAM templates relate to CloudFormation transforms.

## Summary

You built a complete failure-handling pipeline with three distinct mechanisms:

- **Async retry configuration** -- controlled retry attempts (1) and maximum event age (60s) before giving up
- **Dead Letter Queue** -- receives the raw event payload after all retries are exhausted (async invocations only)
- **Lambda Destinations** -- receives enriched failure metadata including error messages and stack traces
- **SQS event source mapping** -- redrive policy with maxReceiveCount for message-level retries, and `ReportBatchItemFailures` for partial batch success

Key exam distinction: DLQs work only for async invocations. Event source mapping retries are controlled by the source service (SQS visibility timeout), not by Lambda's retry configuration.

## Reference

| Resource | Purpose |
|----------|---------|
| `aws_lambda_function_event_invoke_config` | Async retry and destination settings |
| `aws_lambda_function` `dead_letter_config` | DLQ for failed async events |
| `aws_sqs_queue` `redrive_policy` | Move poison messages to DLQ |
| `aws_lambda_event_source_mapping` | Connect SQS to Lambda with batch settings |
| `function_response_types` | Enable partial batch failure reporting |

## Additional Resources

- [Lambda Asynchronous Invocation](https://docs.aws.amazon.com/lambda/latest/dg/invocation-async.html)
- [Lambda Destinations](https://docs.aws.amazon.com/lambda/latest/dg/invocation-async.html#invocation-async-destinations)
- [Dead Letter Queues](https://docs.aws.amazon.com/lambda/latest/dg/invocation-async.html#invocation-dlq)
- [SQS Event Source Mapping](https://docs.aws.amazon.com/lambda/latest/dg/with-sqs.html)
- [Partial Batch Response (ReportBatchItemFailures)](https://docs.aws.amazon.com/lambda/latest/dg/with-sqs.html#services-sqs-batchfailurereporting)
- [DVA-C02 Exam Guide](https://aws.amazon.com/certification/certified-developer-associate/)

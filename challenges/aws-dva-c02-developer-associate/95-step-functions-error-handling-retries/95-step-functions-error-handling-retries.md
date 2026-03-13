# 95. Step Functions Error Handling and Retries

<!--
difficulty: intermediate
concepts: [step-functions-retry, step-functions-catch, error-equals, interval-seconds, max-attempts, backoff-rate, result-path, fallback-state, exponential-backoff]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: apply
prerequisites: [none]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a Step Functions state machine and Lambda functions. Standard workflows cost $0.025 per 1,000 state transitions. Total cost is approximately $0.01/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.5 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| jq installed | `jq --version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Configure** Retry blocks on Task states with ErrorEquals, IntervalSeconds, MaxAttempts, and BackoffRate for exponential backoff
2. **Implement** Catch blocks to route failed tasks to fallback states that preserve error information
3. **Apply** ResultPath in Catch blocks to preserve the original input alongside error details
4. **Differentiate** between built-in error types: `States.ALL`, `States.TaskFailed`, `States.Timeout`, `States.Permissions`
5. **Diagnose** incorrect Retry/Catch ordering in ASL that causes catch to fire before retries are exhausted

## Why Step Functions Error Handling

Lambda functions fail. Networks time out. DynamoDB throttles. In a multi-step workflow, any step can fail, and the failure mode determines the correct response. Some failures are transient (throttling, network timeouts) and should be retried with exponential backoff. Other failures are permanent (validation errors, missing data) and should route to a fallback state.

Step Functions provides declarative error handling directly in the ASL definition. Instead of writing retry loops and try-catch blocks in your Lambda code, you declare `Retry` and `Catch` arrays on each Task state. The state machine runtime handles retry timing, backoff calculations, and error routing automatically.

The DVA-C02 exam tests three concepts heavily. First, the Retry configuration: `IntervalSeconds` (initial delay), `BackoffRate` (multiplier per retry, e.g., 2.0 for exponential), and `MaxAttempts` (total retries before giving up). Second, `ErrorEquals` matching: errors are matched in order, and `States.ALL` acts as a catch-all. Third, the critical ordering rule: **Retry must come before Catch** in the ASL definition. If Catch appears first, it fires immediately on the first error and retries never execute.

## Building Blocks

### `lambda/main.go`

```go
package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
)

type Input struct {
	OrderID    string `json:"order_id"`
	Action     string `json:"action"`
	FailMode   string `json:"fail_mode,omitempty"`
}

type Output struct {
	OrderID   string `json:"order_id"`
	Status    string `json:"status"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
	Attempt   string `json:"attempt"`
}

func handler(ctx context.Context, input Input) (Output, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	funcName := os.Getenv("AWS_LAMBDA_FUNCTION_NAME")

	switch input.Action {
	case "process_payment":
		// Simulate transient failures (retry-able)
		if input.FailMode == "transient" && rand.Float64() < 0.7 {
			return Output{}, fmt.Errorf("TransientError: payment gateway temporarily unavailable")
		}
		// Simulate permanent failure
		if input.FailMode == "permanent" {
			return Output{}, fmt.Errorf("PermanentError: invalid credit card number")
		}
		return Output{
			OrderID:   input.OrderID,
			Status:    "payment_processed",
			Message:   "Payment completed successfully",
			Timestamp: now,
			Attempt:   funcName,
		}, nil

	case "ship_order":
		// Simulate timeout
		if input.FailMode == "timeout" {
			time.Sleep(15 * time.Second)
		}
		return Output{
			OrderID:   input.OrderID,
			Status:    "shipped",
			Message:   "Order shipped to customer",
			Timestamp: now,
			Attempt:   funcName,
		}, nil

	case "send_to_dlq":
		return Output{
			OrderID:   input.OrderID,
			Status:    "dlq",
			Message:   "Order sent to dead letter queue for manual review",
			Timestamp: now,
			Attempt:   funcName,
		}, nil

	default:
		return Output{
			OrderID: input.OrderID,
			Status:  "processed",
			Message: fmt.Sprintf("Action '%s' completed", input.Action),
			Timestamp: now,
			Attempt:   funcName,
		}, nil
	}
}

func main() {
	lambda.Start(handler)
}
```

### State Machine Definition

Create a file called `state_machine.json` with the following skeleton. Your job is to fill in the `TODO` sections:

```json
{
  "Comment": "Error handling with Retry and Catch",
  "StartAt": "ProcessPayment",
  "States": {
    "ProcessPayment": {
      "Type": "Task",
      "Resource": "${process_function_arn}",
      "Parameters": {
        "order_id.$": "$.order_id",
        "action": "process_payment",
        "fail_mode.$": "$.fail_mode"
      },
      "ResultPath": "$.payment",

      "TODO_1": "Add a Retry block here",
      "TODO_1_Requirements": [
        "Retry on 'TransientError' with:",
        "  IntervalSeconds: 2",
        "  MaxAttempts: 3",
        "  BackoffRate: 2.0",
        "This retries at 2s, 4s, 8s intervals",
        "",
        "Retry on 'States.Timeout' with:",
        "  IntervalSeconds: 5",
        "  MaxAttempts: 2",
        "  BackoffRate: 1.0"
      ],

      "TODO_2": "Add a Catch block here (AFTER Retry)",
      "TODO_2_Requirements": [
        "Catch 'PermanentError' and go to 'HandlePermanentFailure'",
        "  ResultPath: '$.error' (preserves original input)",
        "",
        "Catch 'States.ALL' as fallback and go to 'SendToDLQ'",
        "  ResultPath: '$.error'"
      ],

      "Next": "ShipOrder"
    },

    "ShipOrder": {
      "Type": "Task",
      "Resource": "${ship_function_arn}",
      "Parameters": {
        "order_id.$": "$.order_id",
        "action": "ship_order"
      },
      "ResultPath": "$.shipping",
      "TimeoutSeconds": 10,
      "Next": "OrderCompleted"
    },

    "HandlePermanentFailure": {
      "Type": "Pass",
      "Parameters": {
        "order_id.$": "$.order_id",
        "status": "permanently_failed",
        "error_info.$": "$.error"
      },
      "End": true
    },

    "SendToDLQ": {
      "Type": "Task",
      "Resource": "${dlq_function_arn}",
      "Parameters": {
        "order_id.$": "$.order_id",
        "action": "send_to_dlq"
      },
      "ResultPath": "$.dlq",
      "End": true
    },

    "OrderCompleted": {
      "Type": "Succeed"
    }
  }
}
```

### Terraform Skeleton

Create the following files in your exercise directory:

### `providers.tf`

```hcl
terraform {
  required_version = ">= 1.5"
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
  default     = "sfn-errors-demo"
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

### `iam.tf`

```hcl
data "aws_iam_policy_document" "lambda_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals { type = "Service"; identifiers = ["lambda.amazonaws.com"] }
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

data "aws_iam_policy_document" "sfn_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals { type = "Service"; identifiers = ["states.amazonaws.com"] }
  }
}

resource "aws_iam_role" "sfn" {
  name               = "${var.project_name}-sfn-role"
  assume_role_policy = data.aws_iam_policy_document.sfn_assume.json
}

data "aws_iam_policy_document" "sfn_policy" {
  statement {
    actions   = ["lambda:InvokeFunction"]
    resources = [
      aws_lambda_function.process.arn,
      aws_lambda_function.ship.arn,
      aws_lambda_function.dlq.arn,
    ]
  }
}

resource "aws_iam_role_policy" "sfn" {
  name   = "sfn-invoke-lambda"
  role   = aws_iam_role.sfn.id
  policy = data.aws_iam_policy_document.sfn_policy.json
}
```

### `lambda.tf`

```hcl
resource "aws_cloudwatch_log_group" "process" {
  name              = "/aws/lambda/${var.project_name}-process"
  retention_in_days = 1
}

resource "aws_cloudwatch_log_group" "ship" {
  name              = "/aws/lambda/${var.project_name}-ship"
  retention_in_days = 1
}

resource "aws_cloudwatch_log_group" "dlq" {
  name              = "/aws/lambda/${var.project_name}-dlq"
  retention_in_days = 1
}

resource "aws_lambda_function" "process" {
  function_name    = "${var.project_name}-process"
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.lambda.arn
  memory_size      = 128
  timeout          = 10
  depends_on       = [aws_iam_role_policy_attachment.lambda_basic, aws_cloudwatch_log_group.process]
}

resource "aws_lambda_function" "ship" {
  function_name    = "${var.project_name}-ship"
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.lambda.arn
  memory_size      = 128
  timeout          = 10
  depends_on       = [aws_iam_role_policy_attachment.lambda_basic, aws_cloudwatch_log_group.ship]
}

resource "aws_lambda_function" "dlq" {
  function_name    = "${var.project_name}-dlq"
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.lambda.arn
  memory_size      = 128
  timeout          = 10
  depends_on       = [aws_iam_role_policy_attachment.lambda_basic, aws_cloudwatch_log_group.dlq]
}
```

### `workflow.tf`

```hcl
resource "aws_sfn_state_machine" "this" {
  name     = "${var.project_name}-workflow"
  role_arn = aws_iam_role.sfn.arn
  type     = "STANDARD"

  definition = templatefile("${path.module}/state_machine.json", {
    process_function_arn = aws_lambda_function.process.arn
    ship_function_arn    = aws_lambda_function.ship.arn
    dlq_function_arn     = aws_lambda_function.dlq.arn
  })
}
```

### `outputs.tf`

```hcl
output "state_machine_arn" { value = aws_sfn_state_machine.this.arn }
```

## Spot the Bug

A developer writes an ASL state definition with both Retry and Catch. But when a transient error occurs, the Catch block fires immediately and retries never happen.

```json
{
  "ProcessPayment": {
    "Type": "Task",
    "Resource": "arn:aws:lambda:us-east-1:123456789012:function:process",
    "Catch": [
      {
        "ErrorEquals": ["States.ALL"],
        "Next": "HandleFailure",
        "ResultPath": "$.error"
      }
    ],
    "Retry": [
      {
        "ErrorEquals": ["TransientError"],
        "IntervalSeconds": 2,
        "MaxAttempts": 3,
        "BackoffRate": 2.0
      }
    ],
    "Next": "Done"
  }
}
```

<details>
<summary>Explain the bug</summary>

This is actually a common misconception. In ASL, the **order of Retry and Catch in the JSON does not matter** -- Step Functions always processes Retry first, then Catch, regardless of their position in the JSON object. JSON objects are unordered by specification.

The real bug is more subtle: the `Catch` block uses `"ErrorEquals": ["States.ALL"]` which catches ALL errors including `TransientError`. After all retry attempts are exhausted (3 retries), `States.ALL` in the Catch block catches the error and routes to `HandleFailure`. This is actually correct behavior.

However, if the developer intended to retry ALL errors (not just `TransientError`), the bug is in the `Retry` block: it only retries `TransientError`. If a different error occurs (e.g., `TimeoutError`), it is not retried and goes directly to Catch.

**The common exam trap**: Candidates believe JSON key ordering affects Retry/Catch evaluation. It does not. Step Functions always:
1. Matches the error against Retry entries (in array order)
2. If a matching Retry is found and MaxAttempts is not exhausted, retries
3. If no matching Retry or MaxAttempts exhausted, matches against Catch entries (in array order)
4. If no matching Catch, the state machine fails

**Best practice -- retry transient errors and catch-all for fallback:**

```json
{
  "ProcessPayment": {
    "Type": "Task",
    "Resource": "arn:aws:lambda:us-east-1:123456789012:function:process",
    "Retry": [
      {
        "ErrorEquals": ["TransientError", "States.Timeout"],
        "IntervalSeconds": 2,
        "MaxAttempts": 3,
        "BackoffRate": 2.0
      }
    ],
    "Catch": [
      {
        "ErrorEquals": ["PermanentError"],
        "Next": "HandlePermanentFailure",
        "ResultPath": "$.error"
      },
      {
        "ErrorEquals": ["States.ALL"],
        "Next": "HandleFailure",
        "ResultPath": "$.error"
      }
    ],
    "Next": "Done"
  }
}
```

</details>

## Verify What You Learned

### Step 1 -- Apply

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init && terraform apply -auto-approve
```

### Step 2 -- Successful execution (no failures)

```bash
SFN_ARN=$(terraform output -raw state_machine_arn)

aws stepfunctions start-execution \
  --state-machine-arn "$SFN_ARN" \
  --input '{"order_id": "ORD-001", "fail_mode": "none"}'
```

```bash
sleep 15

aws stepfunctions list-executions \
  --state-machine-arn "$SFN_ARN" \
  --status-filter SUCCEEDED \
  --query "executions | length(@)"
```

Expected: `1`

### Step 3 -- Permanent failure (caught by Catch)

```bash
aws stepfunctions start-execution \
  --state-machine-arn "$SFN_ARN" \
  --input '{"order_id": "ORD-002", "fail_mode": "permanent"}'
```

```bash
sleep 10

EXEC_ARN=$(aws stepfunctions list-executions \
  --state-machine-arn "$SFN_ARN" \
  --query "executions[0].executionArn" --output text)

aws stepfunctions get-execution-history \
  --execution-arn "$EXEC_ARN" \
  --query "events[?type=='TaskStateExited' || type=='ExecutionSucceeded' || type=='ExecutionFailed'].{Type:type,Timestamp:timestamp}" \
  --output table
```

Expected: execution routed to `HandlePermanentFailure` state.

### Step 4 -- Transient failure (retried then succeeds or caught)

```bash
aws stepfunctions start-execution \
  --state-machine-arn "$SFN_ARN" \
  --input '{"order_id": "ORD-003", "fail_mode": "transient"}'
```

```bash
sleep 30

aws stepfunctions get-execution-history \
  --execution-arn "$(aws stepfunctions list-executions --state-machine-arn "$SFN_ARN" --query 'executions[0].executionArn' --output text)" \
  --query "events[?type=='TaskStateEntered' || type=='TaskFailed' || type=='TaskSucceeded'].{Type:type,Name:stateEnteredEventDetails.name}" \
  --output table
```

Expected: multiple `TaskFailed` events (retries) followed by either `TaskSucceeded` or routing to the DLQ state.

### Step 5 -- Verify no drift

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Solutions

<details>
<summary>state_machine.json -- TODO 1 + TODO 2 -- Retry and Catch blocks</summary>

Replace the ProcessPayment state in `state_machine.json` with:

```json
"ProcessPayment": {
  "Type": "Task",
  "Resource": "${process_function_arn}",
  "Parameters": {
    "order_id.$": "$.order_id",
    "action": "process_payment",
    "fail_mode.$": "$.fail_mode"
  },
  "ResultPath": "$.payment",
  "Retry": [
    {
      "ErrorEquals": ["TransientError"],
      "IntervalSeconds": 2,
      "MaxAttempts": 3,
      "BackoffRate": 2.0
    },
    {
      "ErrorEquals": ["States.Timeout"],
      "IntervalSeconds": 5,
      "MaxAttempts": 2,
      "BackoffRate": 1.0
    }
  ],
  "Catch": [
    {
      "ErrorEquals": ["PermanentError"],
      "Next": "HandlePermanentFailure",
      "ResultPath": "$.error"
    },
    {
      "ErrorEquals": ["States.ALL"],
      "Next": "SendToDLQ",
      "ResultPath": "$.error"
    }
  ],
  "Next": "ShipOrder"
}
```

Retry details:
- `TransientError`: retries at 2s, 4s, 8s (2 * 2^attempt) -- 3 total retries
- `States.Timeout`: retries at 5s, 5s (no backoff) -- 2 total retries
- Retry entries are evaluated in order; first match wins

Catch details:
- `PermanentError` routes to a Pass state that preserves error info
- `States.ALL` catches everything else and routes to DLQ
- `ResultPath: "$.error"` adds error details under `$.error` without overwriting the original input

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

You built resilient error handling with Retry and Catch. In the next exercise, you will work with **Kinesis Data Streams and Lambda consumers** to process real-time streaming data with event source mappings, batch processing, and parallelization.

## Summary

- **Retry** handles transient failures with configurable `IntervalSeconds`, `MaxAttempts`, and `BackoffRate` (exponential backoff)
- **Catch** routes to fallback states after all retries are exhausted or for non-retryable errors
- `ErrorEquals` matches errors in array order: put specific errors before `States.ALL`
- **ResultPath** in Catch blocks preserves original input: `"ResultPath": "$.error"` adds error info under `$.error`
- Built-in error types: `States.ALL` (catch-all), `States.TaskFailed`, `States.Timeout`, `States.Permissions`, `States.ResultPathMatchFailure`
- JSON object key ordering does **not** affect Retry/Catch evaluation -- Step Functions always processes Retry first
- `BackoffRate: 2.0` with `IntervalSeconds: 2` produces delays of 2s, 4s, 8s (exponential growth)
- Retry with `MaxAttempts: 0` disables retries for that error type

## Reference

- [Step Functions Error Handling](https://docs.aws.amazon.com/step-functions/latest/dg/concepts-error-handling.html)
- [Retry and Catch](https://docs.aws.amazon.com/step-functions/latest/dg/concepts-error-handling.html#error-handling-retrying-after-an-error)
- [Built-in Error Codes](https://docs.aws.amazon.com/step-functions/latest/dg/concepts-error-handling.html#error-handling-error-representation)
- [Terraform aws_sfn_state_machine](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sfn_state_machine)

## Additional Resources

- [Input/Output Processing](https://docs.aws.amazon.com/step-functions/latest/dg/concepts-input-output-filtering.html) -- how ResultPath interacts with Catch to preserve state data
- [Error Handling Best Practices](https://docs.aws.amazon.com/step-functions/latest/dg/bp-error-handling.html) -- AWS recommendations for production error handling
- [Step Functions Error Examples](https://docs.aws.amazon.com/step-functions/latest/dg/concepts-error-handling.html#error-handling-examples) -- ASL examples for common error patterns
- [Custom Error Names](https://docs.aws.amazon.com/step-functions/latest/dg/concepts-error-handling.html#error-handling-error-representation) -- naming conventions for application-specific errors

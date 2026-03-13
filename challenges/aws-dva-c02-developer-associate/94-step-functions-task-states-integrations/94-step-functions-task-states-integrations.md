# 94. Step Functions Task States and Service Integrations

<!--
difficulty: intermediate
concepts: [step-functions-task, request-response, run-a-job-sync, wait-for-callback, task-token, dynamodb-integration, sqs-integration, lambda-integration]
tools: [terraform, aws-cli]
estimated_time: 50m
bloom_level: apply
prerequisites: [none]
aws_cost: ~$0.02/hr
-->

> **AWS Cost Warning:** This exercise creates a Step Functions state machine, Lambda functions, SQS queue, and DynamoDB table. Standard workflows cost $0.025 per 1,000 transitions. SQS and DynamoDB costs are negligible for testing. Total cost is approximately $0.02/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.5 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| jq installed | `jq --version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Differentiate** between the three Task state integration patterns: Request-Response, Run a Job (.sync), and Wait for Callback (.waitForTaskToken)
2. **Implement** a Lambda task using Request-Response (fire-and-wait for Lambda return value)
3. **Configure** an SQS task with `.waitForTaskToken` to pause execution until an external system calls `SendTaskSuccess`
4. **Apply** a DynamoDB direct integration (no Lambda) using the `arn:aws:states:::dynamodb:putItem` resource pattern
5. **Diagnose** the impact of a missing `.sync` suffix causing Step Functions to not wait for job completion

## Why Task State Integration Patterns

Step Functions Task states invoke AWS services, but the way they wait for results differs by integration pattern. Understanding these three patterns is critical for the DVA-C02 exam:

**Request-Response** (default): Step Functions calls the service and immediately moves to the next state. For Lambda, this means Step Functions invokes the function and waits for it to return. For SQS, it sends a message and moves on without waiting for the message to be processed. This is the default behavior when no suffix is added to the resource ARN.

**Run a Job (.sync)**: Step Functions calls the service and pauses until the job completes. Used with services that have long-running operations: ECS RunTask, Batch SubmitJob, Glue StartJobRun. The resource ARN ends with `.sync` (e.g., `arn:aws:states:::ecs:runTask.sync`). Step Functions polls the job status automatically.

**Wait for Callback (.waitForTaskToken)**: Step Functions pauses execution and generates a unique task token. The token is passed to the target service (e.g., SQS message body). Execution resumes only when an external system calls `SendTaskSuccess` or `SendTaskFailure` with that token. Used for human approvals, external system callbacks, and asynchronous processing.

The exam frequently tests which pattern to use: "A Step Functions workflow needs to wait for a human to approve an order before proceeding" -- the answer is `.waitForTaskToken`. "A workflow runs a Batch job and needs the result" -- the answer is `.sync`.

## Building Blocks

### `lambda/main.go`

This Lambda processes orders:

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
	"github.com/aws/aws-sdk-go-v2/service/sfn"
)

var sfnClient *sfn.Client

func init() {
	cfg, _ := config.LoadDefaultConfig(context.TODO())
	sfnClient = sfn.NewFromConfig(cfg)
}

type Input struct {
	OrderID   string `json:"order_id"`
	Amount    float64 `json:"amount"`
	Action    string `json:"action"`
	TaskToken string `json:"task_token,omitempty"`
}

type Output struct {
	OrderID   string `json:"order_id"`
	Status    string `json:"status"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

func handler(ctx context.Context, input Input) (Output, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	switch input.Action {
	case "validate":
		status := "approved"
		if input.Amount > 10000 {
			status = "requires_review"
		}
		return Output{
			OrderID:   input.OrderID,
			Status:    status,
			Message:   fmt.Sprintf("Order %s validated, amount $%.2f", input.OrderID, input.Amount),
			Timestamp: now,
		}, nil

	case "process_callback":
		// Simulate external callback: approve the task token
		if input.TaskToken != "" {
			result, _ := json.Marshal(Output{
				OrderID:   input.OrderID,
				Status:    "callback_approved",
				Message:   "External review completed",
				Timestamp: now,
			})
			_, err := sfnClient.SendTaskSuccess(ctx, &sfn.SendTaskSuccessInput{
				TaskToken: aws.String(input.TaskToken),
				Output:    aws.String(string(result)),
			})
			if err != nil {
				return Output{}, fmt.Errorf("failed to send task success: %w", err)
			}
			return Output{
				OrderID:   input.OrderID,
				Status:    "callback_sent",
				Message:   "Task token callback sent to Step Functions",
				Timestamp: now,
			}, nil
		}
		return Output{}, fmt.Errorf("no task token provided")

	case "finalize":
		return Output{
			OrderID:   input.OrderID,
			Status:    "completed",
			Message:   fmt.Sprintf("Order %s finalized", input.OrderID),
			Timestamp: now,
		}, nil

	default:
		return Output{
			OrderID: input.OrderID,
			Status:  "unknown",
			Message: fmt.Sprintf("Unknown action: %s", input.Action),
		}, nil
	}
}

func main() {
	lambda.Start(handler)
}
```

### State Machine Definition

Create a file called `state_machine.json` with the following skeleton. Your job is to fill in each `TODO` section:

```json
{
  "Comment": "Demonstrates three Task integration patterns",
  "StartAt": "ValidateOrder",
  "States": {
    "ValidateOrder": {
      "Type": "Task",
      "Comment": "Pattern 1: Request-Response (Lambda) -- Step Functions invokes Lambda and waits for the return value",
      "Resource": "${validate_function_arn}",
      "Parameters": {
        "order_id.$": "$.order_id",
        "amount.$": "$.amount",
        "action": "validate"
      },
      "ResultPath": "$.validation",
      "Next": "CheckApproval"
    },

    "CheckApproval": {
      "Type": "Choice",
      "Choices": [
        {
          "Variable": "$.validation.status",
          "StringEquals": "requires_review",
          "Next": "WaitForExternalReview"
        }
      ],
      "Default": "SaveOrder"
    },

    "WaitForExternalReview": {
      "TODO": "Pattern 2: Wait for Callback (.waitForTaskToken)",
      "Comment": "TODO -- Replace this state with a Task state that sends a message to SQS with a task token. The execution pauses until SendTaskSuccess is called with the token.",
      "Type": "Pass",
      "Result": {"status": "placeholder"},
      "ResultPath": "$.review",
      "Next": "SaveOrder"
    },

    "SaveOrder": {
      "TODO": "Pattern 3: DynamoDB Direct Integration (no Lambda)",
      "Comment": "TODO -- Replace this state with a Task state that writes directly to DynamoDB using arn:aws:states:::dynamodb:putItem. No Lambda function needed.",
      "Type": "Pass",
      "Result": {"status": "placeholder"},
      "ResultPath": "$.save",
      "Next": "FinalizeOrder"
    },

    "FinalizeOrder": {
      "Type": "Task",
      "Resource": "${finalize_function_arn}",
      "Parameters": {
        "order_id.$": "$.order_id",
        "amount.$": "$.amount",
        "action": "finalize"
      },
      "ResultPath": "$.finalization",
      "End": true
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
  default     = "sfn-task-demo"
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

### `database.tf`

```hcl
resource "aws_dynamodb_table" "orders" {
  name         = "${var.project_name}-orders"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "order_id"
  attribute {
    name = "order_id"
    type = "S"
  }
}
```

### `events.tf`

```hcl
resource "aws_sqs_queue" "review" {
  name                       = "${var.project_name}-review"
  visibility_timeout_seconds = 300
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

data "aws_iam_policy_document" "lambda_sfn" {
  statement {
    actions   = ["states:SendTaskSuccess", "states:SendTaskFailure"]
    resources = ["*"]
  }
}

resource "aws_iam_role_policy" "lambda_sfn" {
  name   = "sfn-task-callback"
  role   = aws_iam_role.lambda.id
  policy = data.aws_iam_policy_document.lambda_sfn.json
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
      aws_lambda_function.validate.arn,
      aws_lambda_function.finalize.arn,
    ]
  }
  statement {
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.review.arn]
  }
  statement {
    actions   = ["dynamodb:PutItem"]
    resources = [aws_dynamodb_table.orders.arn]
  }
}

resource "aws_iam_role_policy" "sfn" {
  name   = "sfn-task-permissions"
  role   = aws_iam_role.sfn.id
  policy = data.aws_iam_policy_document.sfn_policy.json
}
```

### `lambda.tf`

```hcl
resource "aws_cloudwatch_log_group" "validate" {
  name              = "/aws/lambda/${var.project_name}-validate"
  retention_in_days = 1
}

resource "aws_cloudwatch_log_group" "finalize" {
  name              = "/aws/lambda/${var.project_name}-finalize"
  retention_in_days = 1
}

resource "aws_lambda_function" "validate" {
  function_name    = "${var.project_name}-validate"
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.lambda.arn
  memory_size      = 128
  timeout          = 10
  depends_on       = [aws_iam_role_policy_attachment.lambda_basic, aws_cloudwatch_log_group.validate]
}

resource "aws_lambda_function" "finalize" {
  function_name    = "${var.project_name}-finalize"
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.lambda.arn
  memory_size      = 128
  timeout          = 10
  depends_on       = [aws_iam_role_policy_attachment.lambda_basic, aws_cloudwatch_log_group.finalize]
}
```

### `workflow.tf`

```hcl
resource "aws_sfn_state_machine" "this" {
  name     = "${var.project_name}-workflow"
  role_arn = aws_iam_role.sfn.arn
  type     = "STANDARD"

  definition = templatefile("${path.module}/state_machine.json", {
    validate_function_arn = aws_lambda_function.validate.arn
    finalize_function_arn = aws_lambda_function.finalize.arn
    review_queue_url      = aws_sqs_queue.review.url
    orders_table_name     = aws_dynamodb_table.orders.name
  })
}
```

### `outputs.tf`

```hcl
output "state_machine_arn" { value = aws_sfn_state_machine.this.arn }
output "review_queue_url"  { value = aws_sqs_queue.review.url }
output "table_name"        { value = aws_dynamodb_table.orders.name }
output "function_name"     { value = aws_lambda_function.validate.function_name }
```

## Spot the Bug

A developer creates a Step Functions workflow that submits an ECS task and expects to use the result in the next state. The workflow immediately moves to the next state without waiting for the ECS task to complete.

```json
{
  "RunBatchJob": {
    "Type": "Task",
    "Resource": "arn:aws:states:::ecs:runTask",
    "Parameters": {
      "LaunchType": "FARGATE",
      "Cluster": "arn:aws:ecs:us-east-1:123456789012:cluster/my-cluster",
      "TaskDefinition": "arn:aws:ecs:us-east-1:123456789012:task-definition/my-task:1"
    },
    "Next": "ProcessResult"
  }
}
```

<details>
<summary>Explain the bug</summary>

The resource ARN `arn:aws:states:::ecs:runTask` uses the **Request-Response** pattern (no suffix). Step Functions calls the ECS RunTask API, gets the immediate API response (task ARN, not task output), and immediately moves to the `ProcessResult` state. The ECS task is still running when Step Functions proceeds.

The `.sync` suffix is missing. Without it, Step Functions does not poll for task completion.

**Fix -- add `.sync` to wait for task completion:**

```json
{
  "RunBatchJob": {
    "Type": "Task",
    "Resource": "arn:aws:states:::ecs:runTask.sync",
    "Parameters": {
      "LaunchType": "FARGATE",
      "Cluster": "arn:aws:ecs:us-east-1:123456789012:cluster/my-cluster",
      "TaskDefinition": "arn:aws:ecs:us-east-1:123456789012:task-definition/my-task:1"
    },
    "Next": "ProcessResult"
  }
}
```

With `.sync`, Step Functions:
1. Calls ECS RunTask
2. Polls the task status automatically
3. Pauses execution until the task reaches STOPPED state
4. Passes the task result (including exit code and output) to the next state

Services that support `.sync`: ECS RunTask, Batch SubmitJob, Glue StartJobRun, SageMaker training, CodeBuild. Lambda does NOT use `.sync` because Lambda invocation is synchronous by default.

</details>

## Verify What You Learned

### Step 1 -- Apply and test

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init && terraform apply -auto-approve
```

### Step 2 -- Execute with auto-approved order

```bash
SFN_ARN=$(terraform output -raw state_machine_arn)

aws stepfunctions start-execution \
  --state-machine-arn "$SFN_ARN" \
  --input '{"order_id": "ORD-100", "amount": 500}'
```

```bash
sleep 15

aws stepfunctions list-executions \
  --state-machine-arn "$SFN_ARN" \
  --query "executions[0].{Status:status,Name:name}" --output table
```

Expected: `SUCCEEDED`

### Step 3 -- Check DynamoDB for saved order

```bash
aws dynamodb get-item \
  --table-name $(terraform output -raw table_name) \
  --key '{"order_id": {"S": "ORD-100"}}' \
  --query "Item" --output json
```

Expected: order item with status and timestamp.

### Step 4 -- Verify no drift

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Solutions

<details>
<summary>state_machine.json -- WaitForExternalReview -- Wait for Callback Pattern</summary>

Replace the `WaitForExternalReview` state in `state_machine.json`:

```json
"WaitForExternalReview": {
  "Type": "Task",
  "Resource": "arn:aws:states:::sqs:sendMessage.waitForTaskToken",
  "Parameters": {
    "QueueUrl": "${review_queue_url}",
    "MessageBody": {
      "order_id.$": "$.order_id",
      "amount.$": "$.amount",
      "task_token.$": "$$.Task.Token",
      "message": "Order requires manual review"
    }
  },
  "ResultPath": "$.review",
  "TimeoutSeconds": 3600,
  "Next": "SaveOrder"
}
```

Key points:
- `"Resource": "arn:aws:states:::sqs:sendMessage.waitForTaskToken"` -- the `.waitForTaskToken` suffix pauses execution
- `"task_token.$": "$$.Task.Token"` -- the `$$` context object contains the unique task token
- `"TimeoutSeconds": 3600` -- execution fails if no callback within 1 hour
- An external system reads the SQS message, processes the review, then calls `SendTaskSuccess` with the token

</details>

<details>
<summary>state_machine.json -- SaveOrder -- DynamoDB Direct Integration</summary>

Replace the `SaveOrder` state in `state_machine.json`:

```json
"SaveOrder": {
  "Type": "Task",
  "Resource": "arn:aws:states:::dynamodb:putItem",
  "Parameters": {
    "TableName": "${orders_table_name}",
    "Item": {
      "order_id": { "S.$": "$.order_id" },
      "amount": { "N.$": "States.Format('{}', $.amount)" },
      "status": { "S": "saved" },
      "validated_at": { "S.$": "$.validation.timestamp" }
    }
  },
  "ResultPath": "$.save",
  "Next": "FinalizeOrder"
}
```

Key points:
- `"Resource": "arn:aws:states:::dynamodb:putItem"` -- direct DynamoDB integration, no Lambda needed
- DynamoDB items use typed attribute values (`S` for string, `N` for number)
- `States.Format('{}', $.amount)` converts the numeric amount to a string (DynamoDB N type requires string representation)
- No `.sync` suffix needed -- DynamoDB PutItem is synchronous

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

You implemented three Task state integration patterns. In the next exercise, you will add **Step Functions error handling** with Retry (exponential backoff) and Catch (fallback states) to build resilient workflows.

## Summary

- **Request-Response** (default): Step Functions calls the service and waits for the immediate API response (Lambda return, SQS SendMessage acknowledgement)
- **Run a Job (.sync)**: Step Functions calls the service and polls until the job completes; used for ECS, Batch, Glue, SageMaker
- **Wait for Callback (.waitForTaskToken)**: Step Functions pauses and generates a task token; execution resumes when `SendTaskSuccess`/`SendTaskFailure` is called
- The **task token** is accessed via `$$.Task.Token` in the Parameters block
- **DynamoDB direct integration** (`arn:aws:states:::dynamodb:putItem`) avoids Lambda for simple read/write operations
- Missing `.sync` suffix is a common bug: Step Functions proceeds immediately without waiting for job completion
- Lambda invocations are inherently synchronous -- Lambda tasks do NOT use `.sync`
- `TimeoutSeconds` on callback tasks prevents executions from hanging indefinitely

## Reference

- [Step Functions Service Integrations](https://docs.aws.amazon.com/step-functions/latest/dg/concepts-service-integrations.html)
- [Wait for Callback Pattern](https://docs.aws.amazon.com/step-functions/latest/dg/connect-to-resource.html#connect-wait-token)
- [DynamoDB Integration](https://docs.aws.amazon.com/step-functions/latest/dg/connect-ddb.html)
- [Terraform aws_sfn_state_machine](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sfn_state_machine)

## Additional Resources

- [Supported Service Integrations](https://docs.aws.amazon.com/step-functions/latest/dg/connect-supported-services.html) -- complete list of services supporting .sync and .waitForTaskToken
- [Context Object ($$)](https://docs.aws.amazon.com/step-functions/latest/dg/input-output-contextobject.html) -- accessing task token, execution ARN, and state machine metadata
- [Optimized Integrations](https://docs.aws.amazon.com/step-functions/latest/dg/connect-supported-services.html) -- native integrations vs SDK integrations
- [Step Functions Workshop](https://catalog.workshops.aws/stepfunctions/en-US) -- hands-on workshop covering all integration patterns

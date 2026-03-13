# 93. Step Functions Standard vs Express Workflows

<!--
difficulty: basic
concepts: [step-functions, standard-workflow, express-workflow, state-machine, task-state, choice-state, wait-state, asl-json, exactly-once, at-least-once]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: understand
prerequisites: [none]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a Step Functions state machine and Lambda functions. Standard workflows cost $0.025 per 1,000 state transitions. Express workflows cost based on duration and memory. Total cost is approximately $0.01/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally (for compiling the Lambda binary)

## Learning Objectives

After completing this exercise, you will be able to:

- **Identify** the differences between Standard and Express Step Functions workflows in terms of pricing, execution duration, and delivery guarantees
- **Construct** a state machine definition using ASL (Amazon States Language) with Task, Choice, and Wait states
- **Verify** that a state machine executes the expected state transitions using the AWS CLI
- **Explain** why Standard workflows provide exactly-once execution while Express workflows provide at-least-once
- **Describe** the pricing models: Standard ($0.025/1,000 transitions) vs Express (duration + memory based)

## Why Step Functions Standard vs Express

Step Functions orchestrate multi-step workflows by defining a state machine in Amazon States Language (ASL). Instead of writing complex error handling, retry logic, and state management in your application code, you declare the flow as JSON and let Step Functions manage execution.

AWS offers two workflow types that serve different use cases:

**Standard workflows** run for up to one year, guarantee exactly-once execution, and support all service integrations. Each state transition costs $0.025 per 1,000. Use Standard for long-running processes: order fulfillment, ETL pipelines, human approval workflows. The execution history is stored for 90 days and every execution has a unique ID for deduplication.

**Express workflows** run for up to 5 minutes, provide at-least-once execution, and cost based on duration and memory (not transitions). Use Express for high-volume, short-lived workloads: IoT data processing, streaming transformations, real-time API orchestration. Express workflows can handle over 100,000 invocations per second.

The DVA-C02 exam frequently presents a scenario and asks which workflow type is appropriate. Key decision factors: execution duration (>5 minutes requires Standard), delivery guarantee (exactly-once requires Standard), volume (>100K/sec favors Express), and cost (high transition count favors Express pricing).

## Step 1 -- Create the Lambda Function Code

### `main.go`

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
)

type OrderInput struct {
	OrderID string  `json:"order_id"`
	Amount  float64 `json:"amount"`
	Action  string  `json:"action"`
}

type OrderOutput struct {
	OrderID   string  `json:"order_id"`
	Amount    float64 `json:"amount"`
	Status    string  `json:"status"`
	Message   string  `json:"message"`
	Timestamp string  `json:"timestamp"`
}

func handler(ctx context.Context, input OrderInput) (OrderOutput, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	switch input.Action {
	case "validate":
		if input.Amount <= 0 {
			return OrderOutput{
				OrderID:   input.OrderID,
				Amount:    input.Amount,
				Status:    "invalid",
				Message:   "Order amount must be positive",
				Timestamp: now,
			}, nil
		}
		return OrderOutput{
			OrderID:   input.OrderID,
			Amount:    input.Amount,
			Status:    "validated",
			Message:   "Order validated successfully",
			Timestamp: now,
		}, nil

	case "process":
		// Simulate occasional processing failure
		if rand.Float64() < 0.1 {
			return OrderOutput{}, fmt.Errorf("transient processing error for order %s", input.OrderID)
		}
		return OrderOutput{
			OrderID:   input.OrderID,
			Amount:    input.Amount,
			Status:    "processed",
			Message:   fmt.Sprintf("Payment of $%.2f processed", input.Amount),
			Timestamp: now,
		}, nil

	case "notify":
		return OrderOutput{
			OrderID:   input.OrderID,
			Amount:    input.Amount,
			Status:    "notified",
			Message:   "Customer notification sent",
			Timestamp: now,
		}, nil

	default:
		return OrderOutput{
			OrderID:   input.OrderID,
			Amount:    input.Amount,
			Status:    "unknown",
			Message:   fmt.Sprintf("Unknown action: %s", input.Action),
			Timestamp: now,
		}, nil
	}
}

func main() {
	lambda.Start(handler)
}
```

## Step 2 -- Create the State Machine Definition

Create a file named `state_machine.json`. This ASL definition uses Task, Choice, and Wait states:

```json
{
  "Comment": "Order processing workflow with Task, Choice, and Wait states",
  "StartAt": "ValidateOrder",
  "States": {
    "ValidateOrder": {
      "Type": "Task",
      "Resource": "${validate_function_arn}",
      "Parameters": {
        "order_id.$": "$.order_id",
        "amount.$": "$.amount",
        "action": "validate"
      },
      "ResultPath": "$.validation",
      "Next": "CheckValidation"
    },
    "CheckValidation": {
      "Type": "Choice",
      "Choices": [
        {
          "Variable": "$.validation.status",
          "StringEquals": "validated",
          "Next": "WaitForProcessing"
        },
        {
          "Variable": "$.validation.status",
          "StringEquals": "invalid",
          "Next": "OrderRejected"
        }
      ],
      "Default": "OrderRejected"
    },
    "WaitForProcessing": {
      "Type": "Wait",
      "Seconds": 3,
      "Next": "ProcessPayment"
    },
    "ProcessPayment": {
      "Type": "Task",
      "Resource": "${process_function_arn}",
      "Parameters": {
        "order_id.$": "$.order_id",
        "amount.$": "$.amount",
        "action": "process"
      },
      "ResultPath": "$.payment",
      "Next": "NotifyCustomer"
    },
    "NotifyCustomer": {
      "Type": "Task",
      "Resource": "${notify_function_arn}",
      "Parameters": {
        "order_id.$": "$.order_id",
        "amount.$": "$.amount",
        "action": "notify"
      },
      "ResultPath": "$.notification",
      "Next": "OrderCompleted"
    },
    "OrderCompleted": {
      "Type": "Succeed"
    },
    "OrderRejected": {
      "Type": "Fail",
      "Error": "OrderValidationFailed",
      "Cause": "The order failed validation"
    }
  }
}
```

## Step 3 -- Create the Terraform Configuration

Create the following files in your exercise directory:

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
  default     = "sfn-demo"
}
```

### `build.tf`

```hcl
resource "null_resource" "go_build" {
  triggers = {
    source_hash = filebase64sha256("${path.module}/main.go")
  }
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
# IAM for Lambda
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

# IAM for Step Functions
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
      aws_lambda_function.process.arn,
      aws_lambda_function.notify.arn,
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
# Lambda functions (same binary, different function names)
resource "aws_cloudwatch_log_group" "validate" {
  name              = "/aws/lambda/${var.project_name}-validate"
  retention_in_days = 1
}

resource "aws_cloudwatch_log_group" "process" {
  name              = "/aws/lambda/${var.project_name}-process"
  retention_in_days = 1
}

resource "aws_cloudwatch_log_group" "notify" {
  name              = "/aws/lambda/${var.project_name}-notify"
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

resource "aws_lambda_function" "notify" {
  function_name    = "${var.project_name}-notify"
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.lambda.arn
  memory_size      = 128
  timeout          = 10
  depends_on       = [aws_iam_role_policy_attachment.lambda_basic, aws_cloudwatch_log_group.notify]
}
```

### `workflow.tf`

```hcl
resource "aws_sfn_state_machine" "this" {
  name     = "${var.project_name}-order-workflow"
  role_arn = aws_iam_role.sfn.arn
  type     = "STANDARD"

  definition = templatefile("${path.module}/state_machine.json", {
    validate_function_arn = aws_lambda_function.validate.arn
    process_function_arn  = aws_lambda_function.process.arn
    notify_function_arn   = aws_lambda_function.notify.arn
  })
}
```

### `outputs.tf`

```hcl
output "state_machine_arn" {
  description = "ARN of the Step Functions state machine"
  value       = aws_sfn_state_machine.this.arn
}

output "state_machine_name" {
  description = "Name of the Step Functions state machine"
  value       = aws_sfn_state_machine.this.name
}
```

## Step 4 -- Build and Apply

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init
terraform apply -auto-approve
```

### Intermediate Verification

```bash
terraform state list
```

You should see entries for the three Lambda functions, IAM roles, and the Step Functions state machine.

## Step 5 -- Execute the State Machine

Start a successful execution:

```bash
SFN_ARN=$(terraform output -raw state_machine_arn)

aws stepfunctions start-execution \
  --state-machine-arn "$SFN_ARN" \
  --input '{"order_id": "ORD-001", "amount": 99.99}'
```

Wait for completion and check the result:

```bash
EXEC_ARN=$(aws stepfunctions list-executions \
  --state-machine-arn "$SFN_ARN" \
  --status-filter SUCCEEDED \
  --query "executions[0].executionArn" --output text)

aws stepfunctions describe-execution \
  --execution-arn "$EXEC_ARN" \
  --query "{Status:status,Input:input,Output:output}" --output json
```

Start a failed execution (invalid amount):

```bash
aws stepfunctions start-execution \
  --state-machine-arn "$SFN_ARN" \
  --input '{"order_id": "ORD-002", "amount": -10}'
```

Check the failed execution:

```bash
sleep 5

FAIL_ARN=$(aws stepfunctions list-executions \
  --state-machine-arn "$SFN_ARN" \
  --status-filter FAILED \
  --query "executions[0].executionArn" --output text)

aws stepfunctions describe-execution \
  --execution-arn "$FAIL_ARN" \
  --query "{Status:status,Error:error,Cause:cause}" --output json
```

## Common Mistakes

### 1. Using Express workflow for long-running processes

Express workflows have a 5-minute maximum execution duration. If your workflow includes a Wait state of 10 minutes or waits for a human approval, it will time out.

**Wrong -- Express workflow with long wait:**

```hcl
resource "aws_sfn_state_machine" "this" {
  name = "approval-workflow"
  type = "EXPRESS"   # Max 5 minutes!
  # ...
}
```

With a state machine that includes `"Wait": {"Seconds": 600}`, the Express workflow fails after 5 minutes.

**Fix -- use Standard for long-running workflows:**

```hcl
resource "aws_sfn_state_machine" "this" {
  name = "approval-workflow"
  type = "STANDARD"   # Supports up to 1 year
  # ...
}
```

### 2. Confusing ResultPath with OutputPath

`ResultPath` controls where the task result is placed in the state input. `OutputPath` controls what part of the combined state is passed to the next state.

**Wrong -- ResultPath overwrites entire input:**

```json
{
  "Type": "Task",
  "Resource": "arn:aws:lambda:...",
  "ResultPath": "$"
}
```

This replaces the entire state input with the Lambda output. The original `order_id` and `amount` are lost.

**Fix -- nest the result under a new key:**

```json
{
  "Type": "Task",
  "Resource": "arn:aws:lambda:...",
  "ResultPath": "$.validation"
}
```

Now the Lambda output is added under `$.validation` and the original input fields are preserved.

### 3. Missing Choice state Default

A Choice state without a `Default` field fails with `States.NoChoiceMatched` if none of the conditions match.

**Wrong -- no default:**

```json
{
  "Type": "Choice",
  "Choices": [
    { "Variable": "$.status", "StringEquals": "success", "Next": "Done" }
  ]
}
```

**Fix -- always include Default:**

```json
{
  "Type": "Choice",
  "Choices": [
    { "Variable": "$.status", "StringEquals": "success", "Next": "Done" }
  ],
  "Default": "HandleUnexpected"
}
```

## Verify What You Learned

```bash
aws stepfunctions describe-state-machine \
  --state-machine-arn $(terraform output -raw state_machine_arn) \
  --query "type" --output text
```

Expected: `STANDARD`

```bash
aws stepfunctions list-executions \
  --state-machine-arn $(terraform output -raw state_machine_arn) \
  --query "executions | length(@)"
```

Expected: at least `2` (one succeeded, one failed).

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

You built a Standard workflow with Task, Choice, and Wait states. In the next exercise, you will explore **Step Functions Task state integration patterns** -- Request-Response, Run a Job (.sync), and Wait for Callback (.waitForTaskToken) -- to understand how Step Functions interacts with different AWS services.

## Summary

- **Standard workflows**: up to 1 year, exactly-once execution, $0.025/1,000 transitions, execution history stored 90 days
- **Express workflows**: up to 5 minutes, at-least-once execution, duration-based pricing, 100K+ executions/sec
- **Task state** invokes a Lambda function or AWS service integration and stores the result
- **Choice state** branches execution based on variable comparisons -- always include a `Default` branch
- **Wait state** pauses execution for a fixed duration or until a timestamp
- **ResultPath** controls where task output is placed in the state input -- use `"$.fieldName"` to preserve original input
- **templatefile()** in Terraform renders Lambda ARNs into the ASL JSON definition at plan time
- Step Functions IAM role needs `lambda:InvokeFunction` permission for each Lambda it calls

## Reference

- [AWS Step Functions Developer Guide](https://docs.aws.amazon.com/step-functions/latest/dg/welcome.html)
- [Standard vs Express Workflows](https://docs.aws.amazon.com/step-functions/latest/dg/concepts-standard-vs-express.html)
- [Amazon States Language](https://docs.aws.amazon.com/step-functions/latest/dg/concepts-amazon-states-language.html)
- [Terraform aws_sfn_state_machine](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sfn_state_machine)

## Additional Resources

- [Step Functions Pricing](https://aws.amazon.com/step-functions/pricing/) -- detailed pricing comparison between Standard and Express
- [ASL State Types](https://docs.aws.amazon.com/step-functions/latest/dg/concepts-states.html) -- Task, Pass, Choice, Wait, Succeed, Fail, Parallel, Map
- [Input and Output Processing](https://docs.aws.amazon.com/step-functions/latest/dg/concepts-input-output-filtering.html) -- InputPath, ResultPath, OutputPath, Parameters
- [Step Functions Quotas](https://docs.aws.amazon.com/step-functions/latest/dg/limits-overview.html) -- execution limits, state machine size, and API throttling

# 79. Step Functions Workflow Orchestration

<!--
difficulty: intermediate
concepts: [step-functions, state-machine, asl, task-state, choice-state, parallel-state, map-state, wait-state, catch-retry, execution-history, standard-vs-express, service-integrations]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: apply, analyze
prerequisites: [77-lambda-event-sources-patterns]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** Step Functions Standard: 4,000 free state transitions/month, then $0.025 per 1,000. Express: $1.00 per million requests + duration. Lambda free tier covers invocations. Total ~$0.01/hr. Remember to run `terraform destroy` when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Completed exercise 77 (Lambda event sources) | Lambda function deployment |
| Go 1.21+ installed | `go version` |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Implement** a Step Functions state machine using ASL (Amazon States Language) with Task, Choice, Parallel, and Map states.
2. **Analyze** when to use Step Functions orchestration versus direct Lambda-to-Lambda invocation or EventBridge choreography.
3. **Apply** error handling with Catch and Retry at the state level.
4. **Evaluate** Standard workflows (exactly-once, up to 1 year) versus Express workflows (at-least-once, up to 5 minutes).
5. **Design** workflow patterns for common use cases: sequential processing, branching, fan-out, and iteration.

---

## Why This Matters

Step Functions is the SAA-C03 answer to "how do you coordinate multiple microservices?" While you could chain Lambda functions by having each one invoke the next, this creates tight coupling and makes error handling fragile. Step Functions provides a visual, declarative workflow definition where each step, its inputs, outputs, retries, and error handling are explicitly defined.

The exam tests three key decision points. First, Standard vs Express: Standard workflows guarantee exactly-once execution, can run for up to one year, and cost per state transition -- use for long-running processes like order fulfillment or ETL pipelines. Express workflows run at-least-once, limited to 5 minutes, cost per execution+duration -- use for high-volume, short-duration work like IoT data processing or streaming transformation. Second, orchestration (Step Functions) vs choreography (EventBridge): Step Functions is better when you need a central view of the workflow and explicit error handling; EventBridge is better when services are loosely coupled and can evolve independently. Third, the Parallel state versus Map state: Parallel runs different branches simultaneously (fixed branches), while Map iterates over a collection and runs the same steps for each item (dynamic iteration).

---

## Building Blocks

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
  default     = "saa-ex79"
}
```

### `lambda.tf`

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

resource "aws_lambda_function" "this" {
  function_name    = "${var.project_name}-handler"
  role             = aws_iam_role.lambda.arn
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  filename         = "function.zip"
  source_code_hash = filebase64sha256("function.zip")
  timeout          = 30
  memory_size      = 128
}
```

### `iam.tf`

```hcl
resource "aws_iam_role" "sfn" {
  name = "${var.project_name}-sfn-role"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action    = "sts:AssumeRole"
      Effect    = "Allow"
      Principal = { Service = "states.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy" "sfn_lambda" {
  name = "${var.project_name}-sfn-lambda"
  role = aws_iam_role.sfn.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = "lambda:InvokeFunction"
      Resource = aws_lambda_function.this.arn
    }]
  })
}
```

### `workflow.tf`

```hcl
# ============================================================
# TODO 1: Order Processing State Machine
# ============================================================
# Create a Step Functions state machine for an order processing
# workflow with the following flow:
#
#   ValidateOrder (Task)
#         |
#     [Choice: is order valid?]
#        / \
#   Yes     No
#    |       |
#   ProcessPayment   RejectOrder (Fail)
#    |
#   [Parallel]
#    / \
#  Ship  Notify
#    \ /
#   OrderComplete (Succeed)
#
# Requirements:
#   - Resource: aws_sfn_state_machine
#   - name = "${var.project_name}-order-workflow"
#   - role_arn = Step Functions IAM role
#   - definition: ASL JSON (see TODO 2 for the ASL)
#   - type = "STANDARD"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sfn_state_machine
# ============================================================


# ============================================================
# TODO 2: ASL Definition
# ============================================================
# Define the ASL JSON for the order processing workflow.
# Use templatefile() or jsonencode() to build the definition.
#
# States:
#   1. ValidateOrder (Task)
#      - Resource: Lambda function ARN
#      - Next: CheckValidation
#      - Retry: on Lambda.ServiceException, max 2 attempts
#
#   2. CheckValidation (Choice)
#      - If $.valid == true -> ProcessPayment
#      - Default -> RejectOrder
#
#   3. ProcessPayment (Task)
#      - Resource: Lambda function ARN
#      - Next: FulfillOrder
#
#   4. FulfillOrder (Parallel)
#      - Branch 1: ShipOrder (Task -> Lambda)
#      - Branch 2: NotifyCustomer (Task -> Lambda)
#      - Next: OrderComplete
#      - Catch: on States.ALL -> HandleError
#
#   5. OrderComplete (Succeed)
#
#   6. RejectOrder (Fail)
#      - Error: "OrderInvalid"
#      - Cause: "Order validation failed"
#
#   7. HandleError (Fail)
#      - Error: "FulfillmentFailed"
#      - Cause: "Order fulfillment failed"
#
# ASL Reference: https://states-language.net/spec.html
# ============================================================


# ============================================================
# TODO 3: Execute and Monitor (CLI)
# ============================================================
# After terraform apply, start a state machine execution
# and observe its progress:
#
#   a) Start execution with valid order:
#      aws stepfunctions start-execution \
#        --state-machine-arn $(terraform output -raw state_machine_arn) \
#        --input '{"order_id":"ORD-001","valid":true,"items":["item1","item2"],"amount":99.99}'
#
#   b) Start execution with invalid order:
#      aws stepfunctions start-execution \
#        --state-machine-arn $(terraform output -raw state_machine_arn) \
#        --input '{"order_id":"ORD-002","valid":false}'
#
#   c) Check execution status:
#      aws stepfunctions describe-execution \
#        --execution-arn <EXECUTION_ARN> \
#        --query '{Status:Status,StartDate:StartDate,StopDate:StopDate}'
#
#   d) View execution history:
#      aws stepfunctions get-execution-history \
#        --execution-arn <EXECUTION_ARN> \
#        --query 'Events[*].{Type:Type,Timestamp:Timestamp}' \
#        --output table
# ============================================================
```

### `outputs.tf`

```hcl
output "state_machine_arn" {
  value = "Set after TODO 1 implementation"
}
```

---

## State Types Reference

| State Type | Purpose | Example Use |
|---|---|---|
| **Task** | Execute work (Lambda, service integration, Activity) | Invoke Lambda to validate order |
| **Choice** | Branch based on input conditions | Route valid/invalid orders |
| **Parallel** | Execute multiple branches simultaneously | Ship + notify in parallel |
| **Map** | Iterate over a collection | Process each line item |
| **Wait** | Pause for time or until timestamp | Wait for payment confirmation |
| **Pass** | Pass input to output (optional transformation) | Inject default values |
| **Succeed** | Terminal success state | Order complete |
| **Fail** | Terminal failure state | Order rejected |

### Standard vs Express Workflows

| Feature | Standard | Express |
|---|---|---|
| **Duration** | Up to 1 year | Up to 5 minutes |
| **Execution semantics** | Exactly-once | At-least-once (sync), at-most-once (async) |
| **Execution history** | 90 days in console | CloudWatch Logs only |
| **Pricing** | $0.025 per 1,000 state transitions | $1.00/M requests + $0.00001667/GB-second |
| **Start rate** | 2,000/second | 100,000/second |
| **Use case** | Long-running, audit trail needed | High-volume, short-duration |

---

## Spot the Bug

The following state machine definition has a critical resilience flaw. Identify the problem before expanding the answer.

```json
{
  "StartAt": "ValidateOrder",
  "States": {
    "ValidateOrder": {
      "Type": "Task",
      "Resource": "arn:aws:lambda:us-east-1:123456789:function:validate",
      "Next": "ProcessPayment"
    },
    "ProcessPayment": {
      "Type": "Task",
      "Resource": "arn:aws:lambda:us-east-1:123456789:function:payment",
      "Next": "FulfillOrder"
    },
    "FulfillOrder": {
      "Type": "Parallel",
      "Branches": [
        {
          "StartAt": "ShipOrder",
          "States": {
            "ShipOrder": {
              "Type": "Task",
              "Resource": "arn:aws:lambda:us-east-1:123456789:function:ship",
              "End": true
            }
          }
        },
        {
          "StartAt": "NotifyCustomer",
          "States": {
            "NotifyCustomer": {
              "Type": "Task",
              "Resource": "arn:aws:lambda:us-east-1:123456789:function:notify",
              "End": true
            }
          }
        }
      ],
      "Next": "OrderComplete"
    },
    "OrderComplete": {
      "Type": "Succeed"
    }
  }
}
```

<details>
<summary>Explain the bug</summary>

**The Parallel state has no `Catch` block. If either the ShipOrder or NotifyCustomer branch throws an error, the entire state machine execution fails immediately.** There is no way to handle the error, no fallback state, and no record of what went wrong beyond the execution history.

In production, the notification service might be temporarily unavailable. Without a Catch block, a transient notification failure kills the entire order after payment has already been processed. The customer is charged but receives no shipment confirmation and the order is stuck in a failed state.

**Fix:** Add `Catch` and `Retry` to the Parallel state:

```json
"FulfillOrder": {
  "Type": "Parallel",
  "Branches": [...],
  "Retry": [
    {
      "ErrorEquals": ["Lambda.ServiceException", "Lambda.TooManyRequestsException"],
      "IntervalSeconds": 2,
      "MaxAttempts": 3,
      "BackoffRate": 2.0
    }
  ],
  "Catch": [
    {
      "ErrorEquals": ["States.ALL"],
      "Next": "HandleFulfillmentError",
      "ResultPath": "$.error"
    }
  ],
  "Next": "OrderComplete"
}
```

This retries transient Lambda errors up to 3 times with exponential backoff. If all retries fail, the error is caught and routed to `HandleFulfillmentError` where you can log the error, notify operations, or initiate a refund.

**Key principle:** Every Task and Parallel state in a production workflow should have Catch blocks. Retry handles transient failures automatically. Catch handles permanent failures gracefully.

</details>

---

## Verify What You Learned

1. **Deploy the infrastructure:**
   ```bash
   terraform init && terraform apply -auto-approve
   ```

2. **Start a valid order execution:**
   ```bash
   SFN_ARN=$(terraform output -raw state_machine_arn)
   EXEC_ARN=$(aws stepfunctions start-execution \
     --state-machine-arn "$SFN_ARN" \
     --input '{"order_id":"ORD-001","valid":true,"items":["item1"],"amount":49.99}' \
     --query 'executionArn' --output text)
   echo "Execution: $EXEC_ARN"
   ```

3. **Wait and check status:**
   ```bash
   sleep 10
   aws stepfunctions describe-execution \
     --execution-arn "$EXEC_ARN" \
     --query '{Status:Status,Input:Input,Output:Output}' \
     --output json
   ```
   Expected: Status `SUCCEEDED`.

4. **Start an invalid order execution:**
   ```bash
   SFN_ARN=$(terraform output -raw state_machine_arn)
   EXEC_ARN2=$(aws stepfunctions start-execution \
     --state-machine-arn "$SFN_ARN" \
     --input '{"order_id":"ORD-002","valid":false}' \
     --query 'executionArn' --output text)
   sleep 5
   aws stepfunctions describe-execution \
     --execution-arn "$EXEC_ARN2" \
     --query '{Status:Status}' --output text
   ```
   Expected: Status `FAILED` (rejected by Choice state).

5. **View execution history:**
   ```bash
   aws stepfunctions get-execution-history \
     --execution-arn "$EXEC_ARN" \
     --query 'Events[*].{Id:Id,Type:Type}' \
     --output table
   ```
   Expected: Sequence of state transitions showing the workflow path.

6. **Terraform state consistency:**
   ```bash
   terraform plan
   ```
   Expected: `No changes. Your infrastructure matches the configuration.`

---

## Solutions

<details>
<summary>workflow.tf -- TODO 1 and TODO 2: State Machine with ASL Definition</summary>

```hcl
resource "aws_sfn_state_machine" "this" {
  name     = "${var.project_name}-order-workflow"
  role_arn = aws_iam_role.sfn.arn
  type     = "STANDARD"

  definition = jsonencode({
    Comment = "Order processing workflow"
    StartAt = "ValidateOrder"
    States = {
      ValidateOrder = {
        Type     = "Task"
        Resource = aws_lambda_function.this.arn
        Next     = "CheckValidation"
        Retry = [{
          ErrorEquals     = ["Lambda.ServiceException", "Lambda.TooManyRequestsException"]
          IntervalSeconds = 2
          MaxAttempts     = 2
          BackoffRate     = 2.0
        }]
      }

      CheckValidation = {
        Type = "Choice"
        Choices = [{
          Variable      = "$.valid"
          BooleanEquals = true
          Next          = "ProcessPayment"
        }]
        Default = "RejectOrder"
      }

      ProcessPayment = {
        Type     = "Task"
        Resource = aws_lambda_function.this.arn
        Next     = "FulfillOrder"
        Retry = [{
          ErrorEquals     = ["Lambda.ServiceException"]
          IntervalSeconds = 2
          MaxAttempts     = 3
          BackoffRate     = 2.0
        }]
      }

      FulfillOrder = {
        Type = "Parallel"
        Branches = [
          {
            StartAt = "ShipOrder"
            States = {
              ShipOrder = {
                Type     = "Task"
                Resource = aws_lambda_function.this.arn
                End      = true
              }
            }
          },
          {
            StartAt = "NotifyCustomer"
            States = {
              NotifyCustomer = {
                Type     = "Task"
                Resource = aws_lambda_function.this.arn
                End      = true
              }
            }
          }
        ]
        Next = "OrderComplete"
        Catch = [{
          ErrorEquals = ["States.ALL"]
          Next        = "HandleError"
          ResultPath  = "$.error"
        }]
      }

      OrderComplete = {
        Type = "Succeed"
      }

      RejectOrder = {
        Type  = "Fail"
        Error = "OrderInvalid"
        Cause = "Order validation failed"
      }

      HandleError = {
        Type  = "Fail"
        Error = "FulfillmentFailed"
        Cause = "Order fulfillment failed"
      }
    }
  })

  tags = { Name = "${var.project_name}-order-workflow" }
}
```

</details>

<details>
<summary>outputs.tf -- Updated Output</summary>

```hcl
output "state_machine_arn" {
  value = aws_sfn_state_machine.this.arn
}
```

Using `jsonencode()` in Terraform lets you reference Lambda ARNs directly. Alternatively, you can use `templatefile()` with a separate JSON file and `${lambda_arn}` placeholders.

</details>

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify:

```bash
aws stepfunctions list-state-machines \
  --query 'stateMachines[?name==`saa-ex79-order-workflow`]'
```

Expected: Empty list.

---

## What's Next

Exercise 80 covers **AWS AppSync with GraphQL**, where you will create a managed GraphQL API with DynamoDB resolvers and real-time subscriptions. AppSync provides an alternative to API Gateway+Lambda for data-driven APIs, particularly when clients need flexible queries and real-time updates.

---

## Summary

- **Step Functions** orchestrates multi-step workflows with visual state machine definitions using ASL (Amazon States Language)
- **Task state** executes work: Lambda invocation, AWS SDK call, or Activity worker
- **Choice state** implements conditional branching based on input values
- **Parallel state** runs multiple branches simultaneously -- all branches must succeed (unless Catch is configured)
- **Map state** iterates over a collection and runs the same steps for each item
- **Retry** handles transient errors automatically with configurable interval, max attempts, and exponential backoff
- **Catch** routes unhandled errors to fallback states -- every production Task and Parallel state should have Catch blocks
- **Standard workflows**: exactly-once, up to 1 year, priced per state transition -- for long-running processes
- **Express workflows**: at-least-once, up to 5 minutes, priced per request+duration -- for high-volume, short processes
- **Orchestration (Step Functions) vs choreography (EventBridge)**: Step Functions when you need central control; EventBridge when services evolve independently
- **Service integrations** allow Step Functions to call 200+ AWS services directly without Lambda (e.g., DynamoDB PutItem, SQS SendMessage)

## Reference

- [AWS Step Functions Developer Guide](https://docs.aws.amazon.com/step-functions/latest/dg/welcome.html)
- [Amazon States Language Specification](https://states-language.net/spec.html)
- [Terraform aws_sfn_state_machine](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sfn_state_machine)
- [Step Functions Pricing](https://aws.amazon.com/step-functions/pricing/)

## Additional Resources

- [Step Functions Service Integrations](https://docs.aws.amazon.com/step-functions/latest/dg/concepts-service-integrations.html) -- call AWS services directly without Lambda
- [Error Handling in Step Functions](https://docs.aws.amazon.com/step-functions/latest/dg/concepts-error-handling.html) -- Retry and Catch configuration patterns
- [Standard vs Express Workflows](https://docs.aws.amazon.com/step-functions/latest/dg/concepts-standard-vs-express.html) -- detailed comparison
- [Map State for Dynamic Parallelism](https://docs.aws.amazon.com/step-functions/latest/dg/amazon-states-language-map-state.html) -- processing collections with configurable concurrency

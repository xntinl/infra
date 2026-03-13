# 51. EventBridge Scheduler and Cron Jobs

<!--
difficulty: intermediate
concepts: [eventbridge-scheduler, rate-expression, cron-expression, flexible-time-window, one-time-schedule, recurring-schedule, scheduler-vs-rules]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: apply
prerequisites: [49-eventbridge-rules-event-patterns]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates EventBridge Scheduler schedules and a Lambda function. Scheduler pricing is free for the first 14 million invocations per month. Cost is approximately $0.01/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| jq installed | `jq --version` |

## Learning Objectives

After completing this exercise, you will be able to:

- **Differentiate** between EventBridge Scheduler (standalone service) and EventBridge scheduled rules (legacy approach)
- **Configure** rate-based schedules (`rate(5 minutes)`) and cron-based schedules using 6-field cron expressions
- **Implement** one-time schedules for future execution with automatic deletion after completion
- **Apply** flexible time windows to distribute invocations and prevent thundering herd patterns
- **Explain** why EventBridge Scheduler uses 6-field cron expressions (including year) unlike standard 5-field Unix cron

## Why EventBridge Scheduler

EventBridge Scheduler is a standalone scheduling service (separate from EventBridge rules with schedule expressions). While EventBridge rules support `rate()` and `cron()` expressions, Scheduler adds critical features: one-time schedules, flexible time windows, time zone support, and the ability to target over 270 AWS services directly without writing a Lambda intermediary.

The DVA-C02 exam tests three concepts. First, Scheduler uses **6-field cron expressions** (minute, hour, day-of-month, month, day-of-week, year), not the 5-field format used by standard Unix cron. This is the most common exam trap -- a cron expression with 5 fields is invalid in EventBridge. Second, **flexible time windows** distribute invocations across a specified window (e.g., 15 minutes) instead of triggering all schedules at the exact same second -- essential for preventing thundering herd on shared downstream resources. Third, **one-time schedules** can be configured with `ActionAfterCompletion: DELETE` to automatically clean up after execution.

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
  default     = "scheduler-demo"
}
```

### `iam.tf`

```hcl
# Lambda execution role
data "aws_iam_policy_document" "lambda_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "handler" {
  name               = "${var.project_name}-handler-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

resource "aws_iam_role_policy_attachment" "handler_basic" {
  role       = aws_iam_role.handler.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

# Scheduler execution role (allows Scheduler to invoke Lambda)
data "aws_iam_policy_document" "scheduler_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["scheduler.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "scheduler" {
  name               = "${var.project_name}-scheduler-role"
  assume_role_policy = data.aws_iam_policy_document.scheduler_assume.json
}

data "aws_iam_policy_document" "scheduler_invoke" {
  statement {
    actions   = ["lambda:InvokeFunction"]
    resources = [aws_lambda_function.handler.arn]
  }
}

resource "aws_iam_role_policy" "scheduler_invoke" {
  name   = "invoke-lambda"
  role   = aws_iam_role.scheduler.id
  policy = data.aws_iam_policy_document.scheduler_invoke.json
}
```

### `build.tf`

```hcl
resource "null_resource" "go_build" {
  triggers = {
    source_hash = filebase64sha256("${path.module}/handler/main.go")
  }
  provisioner "local-exec" {
    command     = "GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go"
    working_dir = "${path.module}/handler"
  }
}

data "archive_file" "handler" {
  type        = "zip"
  source_file = "${path.module}/handler/bootstrap"
  output_path = "${path.module}/build/handler.zip"
  depends_on  = [null_resource.go_build]
}
```

### `lambda.tf`

```hcl
resource "aws_cloudwatch_log_group" "handler" {
  name              = "/aws/lambda/${var.project_name}-handler"
  retention_in_days = 1
}

resource "aws_lambda_function" "handler" {
  function_name    = "${var.project_name}-handler"
  filename         = data.archive_file.handler.output_path
  source_code_hash = data.archive_file.handler.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.handler.arn
  timeout          = 10

  depends_on = [aws_iam_role_policy_attachment.handler_basic, aws_cloudwatch_log_group.handler]
}
```

### `events.tf`

```hcl
# =======================================================
# TODO 1 -- Create a Rate-Based Schedule (events.tf)
# =======================================================
# Requirements:
#   - Create an aws_scheduler_schedule named "${var.project_name}-rate"
#   - Use schedule_expression = "rate(5 minutes)"
#   - Set flexible_time_window mode to "OFF"
#   - Configure target to invoke the Lambda function
#   - Set input to JSON: {"schedule_type": "rate", "interval": "5min"}
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/scheduler_schedule
# Note: Scheduler requires its own IAM role (not Lambda permissions)


# =======================================================
# TODO 2 -- Create a Cron-Based Schedule (events.tf)
# =======================================================
# Requirements:
#   - Create an aws_scheduler_schedule named "${var.project_name}-cron"
#   - Use a 6-field cron expression: cron(0 9 * * ? *)
#     (every day at 9:00 AM UTC)
#   - Set schedule_expression_timezone to "UTC"
#   - Set flexible_time_window to 15 minutes (mode = "FLEXIBLE")
#   - Configure target to invoke the Lambda function
#
# IMPORTANT: EventBridge cron uses 6 fields, not 5:
#   cron(minute hour day-of-month month day-of-week year)
#   The 6th field (year) can be * for "every year"
#   Use ? for either day-of-month or day-of-week (not both)


# =======================================================
# TODO 3 -- Create a Scheduler Group (events.tf)
# =======================================================
# Requirements:
#   - Create an aws_scheduler_schedule_group for organizing schedules
#   - Groups help manage related schedules (e.g., delete all at once)
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/scheduler_schedule_group
```

### `outputs.tf`

```hcl
output "function_name" {
  value = aws_lambda_function.handler.function_name
}
```

### `handler/main.go`

```go
// handler/main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
)

type ScheduleEvent struct {
	ScheduleType string `json:"schedule_type"`
	Interval     string `json:"interval,omitempty"`
	Message      string `json:"message,omitempty"`
}

func handler(ctx context.Context, event json.RawMessage) error {
	var schedEvent ScheduleEvent
	if err := json.Unmarshal(event, &schedEvent); err != nil {
		fmt.Printf("Raw event: %s\n", string(event))
		return nil
	}

	fmt.Printf("Schedule triggered at %s\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Printf("  Type: %s\n", schedEvent.ScheduleType)
	if schedEvent.Interval != "" {
		fmt.Printf("  Interval: %s\n", schedEvent.Interval)
	}
	if schedEvent.Message != "" {
		fmt.Printf("  Message: %s\n", schedEvent.Message)
	}

	return nil
}

func main() {
	lambda.Start(handler)
}
```

### `handler/go.mod`

```text
module handler

go 1.21

require github.com/aws/aws-lambda-go v1.47.0
```

## Spot the Bug

A developer creates an EventBridge Scheduler cron schedule to run a cleanup job every Monday at 8:00 AM. The schedule never fires.

```hcl
resource "aws_scheduler_schedule" "weekly_cleanup" {
  name = "weekly-cleanup"

  schedule_expression = "cron(0 8 * * MON)"

  flexible_time_window {
    mode = "OFF"
  }

  target {
    arn      = aws_lambda_function.cleanup.arn
    role_arn = aws_iam_role.scheduler.arn
  }
}
```

<details>
<summary>Explain the bug</summary>

The cron expression `cron(0 8 * * MON)` has only **5 fields** (standard Unix cron format). EventBridge Scheduler requires **6 fields**: `cron(minute hour day-of-month month day-of-week year)`. The missing 6th field (year) causes a validation error or the schedule is never created.

Additionally, EventBridge cron uses `?` (no specific value) for either `day-of-month` or `day-of-week` when the other is specified. You cannot use `*` for both.

**Fix -- use 6-field cron with `?` for day-of-month:**

```hcl
resource "aws_scheduler_schedule" "weekly_cleanup" {
  name = "weekly-cleanup"

  schedule_expression          = "cron(0 8 ? * MON *)"
  schedule_expression_timezone = "UTC"

  flexible_time_window {
    mode = "OFF"
  }

  target {
    arn      = aws_lambda_function.cleanup.arn
    role_arn = aws_iam_role.scheduler.arn
  }
}
```

The corrected expression: `cron(0 8 ? * MON *)` -- minute=0, hour=8, day-of-month=? (any), month=* (every), day-of-week=MON, year=* (every).

On the exam, the most common cron trap is using 5 fields instead of 6. EventBridge (both Scheduler and rules) always uses 6 fields. Standard Unix cron uses 5. AWS-specific cron also requires `?` for either the day-of-month or day-of-week field.

</details>

## Verify What You Learned

```bash
# Verify schedules exist
aws scheduler list-schedules \
  --query "Schedules[?contains(Name, 'scheduler-demo')].{Name:Name,State:State,Expression:ScheduleExpression}" \
  --output table
```

Expected: two schedules (rate and cron) in ENABLED state.

```bash
# Verify scheduler role can invoke Lambda
aws iam simulate-principal-policy \
  --policy-source-arn $(terraform output -raw scheduler_role_arn 2>/dev/null || echo "check-manually") \
  --action-names lambda:InvokeFunction \
  --query "EvaluationResults[0].EvalDecision" --output text
```

Expected: `allowed`

```bash
# Check Lambda logs for scheduled invocations (wait a few minutes for rate schedule)
aws logs filter-log-events \
  --log-group-name "/aws/lambda/scheduler-demo-handler" \
  --filter-pattern "Schedule triggered" \
  --query "events[*].message" --output text
```

Expected: log entries showing schedule triggers.

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Solutions

<details>
<summary>TODO 1 -- Rate-Based Schedule</summary>

### `events.tf`

```hcl
resource "aws_scheduler_schedule" "rate" {
  name = "${var.project_name}-rate"

  schedule_expression = "rate(5 minutes)"

  flexible_time_window {
    mode = "OFF"
  }

  target {
    arn      = aws_lambda_function.handler.arn
    role_arn = aws_iam_role.scheduler.arn

    input = jsonencode({
      schedule_type = "rate"
      interval      = "5min"
    })
  }
}
```

</details>

<details>
<summary>TODO 2 -- Cron-Based Schedule</summary>

### `events.tf`

```hcl
resource "aws_scheduler_schedule" "cron" {
  name = "${var.project_name}-cron"

  schedule_expression          = "cron(0 9 * * ? *)"
  schedule_expression_timezone = "UTC"

  flexible_time_window {
    mode                = "FLEXIBLE"
    maximum_window_in_minutes = 15
  }

  target {
    arn      = aws_lambda_function.handler.arn
    role_arn = aws_iam_role.scheduler.arn

    input = jsonencode({
      schedule_type = "cron"
      message       = "Daily 9 AM UTC job"
    })
  }
}
```

</details>

<details>
<summary>TODO 3 -- Scheduler Group</summary>

### `events.tf`

```hcl
resource "aws_scheduler_schedule_group" "app" {
  name = "${var.project_name}-group"
}
```

To assign schedules to the group, add `group_name = aws_scheduler_schedule_group.app.name` to each schedule resource.

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

You configured EventBridge Scheduler with rate and cron expressions, flexible time windows, and scheduler groups. In the next exercise, you will build **SNS FIFO with SQS FIFO ordering** -- creating strictly ordered pub/sub messaging with message group IDs and deduplication.

## Summary

- **EventBridge Scheduler** is a standalone service, separate from EventBridge scheduled rules
- Scheduler supports **one-time schedules**, **rate expressions**, and **cron expressions**
- Cron expressions use **6 fields** (minute, hour, day-of-month, month, day-of-week, year) -- not 5-field Unix cron
- Use `?` for either day-of-month or day-of-week when the other is specified -- `*` cannot be used for both simultaneously
- **Flexible time windows** distribute invocations over a window (e.g., 15 minutes) to prevent thundering herd
- Scheduler requires its own **IAM execution role** to invoke targets (not Lambda resource-based policies)
- Scheduler supports **270+ AWS service targets** directly, without needing a Lambda intermediary
- **Schedule groups** organize related schedules for management (e.g., bulk delete)
- One-time schedules support `ActionAfterCompletion: DELETE` for automatic cleanup

## Reference

- [EventBridge Scheduler](https://docs.aws.amazon.com/scheduler/latest/UserGuide/what-is-scheduler.html)
- [Schedule Expressions](https://docs.aws.amazon.com/scheduler/latest/UserGuide/schedule-types.html)
- [Terraform aws_scheduler_schedule](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/scheduler_schedule)
- [Cron Expression Reference](https://docs.aws.amazon.com/scheduler/latest/UserGuide/schedule-types.html#cron-based)

## Additional Resources

- [Scheduler vs EventBridge Rules](https://docs.aws.amazon.com/scheduler/latest/UserGuide/getting-started.html) -- when to use Scheduler over scheduled rules
- [Flexible Time Windows](https://docs.aws.amazon.com/scheduler/latest/UserGuide/schedule-types.html#flexible-time-windows) -- distributing invocations to prevent load spikes
- [One-Time Schedules](https://docs.aws.amazon.com/scheduler/latest/UserGuide/schedule-types.html#one-time) -- scheduling future actions with automatic cleanup
- [Universal Targets](https://docs.aws.amazon.com/scheduler/latest/UserGuide/managing-targets-universal.html) -- invoking any AWS API action as a scheduler target

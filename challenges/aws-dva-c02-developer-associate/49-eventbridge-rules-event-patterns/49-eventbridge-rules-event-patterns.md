# 49. EventBridge Rules and Event Patterns

<!--
difficulty: basic
concepts: [eventbridge-rules, event-patterns, custom-events, put-events, lambda-target, detail-type, source-field, event-matching]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: apply
prerequisites: [01-lambda-environment-layers-configuration]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates EventBridge rules and a Lambda function. EventBridge custom events cost $1.00 per million events. Cost is approximately $0.01/hr for testing volumes. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally (for compiling the Lambda binary)

## Learning Objectives

After completing this exercise, you will be able to:

- **Construct** EventBridge rules with event patterns that match on `source`, `detail-type`, and `detail` fields
- **Publish** custom events to the default event bus using the AWS CLI (`put-events`) and the Go SDK
- **Configure** a Lambda function as an EventBridge rule target with the required resource-based permission
- **Explain** how EventBridge event pattern matching uses prefix, numeric, exists, and content-based filtering
- **Verify** that events matching a rule trigger the target Lambda and non-matching events are ignored

## Why EventBridge Rules and Event Patterns

EventBridge is the central event router in AWS. Services publish events to an event bus, and rules evaluate each event against a pattern. When an event matches, EventBridge routes it to one or more targets (Lambda, SQS, Step Functions, etc.). Unlike SNS filtering which matches on message attributes, EventBridge pattern matching works on the event body itself -- specifically the `source`, `detail-type`, and `detail` JSON fields.

The DVA-C02 exam tests three critical concepts. First, event patterns use a JSON structure where each field is an array of allowed values -- `{"source": ["myapp.orders"]}` matches events where the `source` field equals `myapp.orders`. Second, nested field matching in `detail` uses the same array syntax -- `{"detail": {"status": ["shipped"]}}` matches events where `detail.status` equals `shipped`. Third, all specified fields must match (AND logic), while values within a field use OR logic -- `{"source": ["myapp.orders"], "detail-type": ["OrderCreated", "OrderUpdated"]}` matches events from `myapp.orders` with either detail-type.

## Step 1 -- Create the Lambda Target Code

### `handler/main.go`

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-lambda-go/lambda"
)

// EventBridgeEvent represents the structure of an EventBridge event.
type EventBridgeEvent struct {
	Version    string          `json:"version"`
	ID         string          `json:"id"`
	Source     string          `json:"source"`
	DetailType string          `json:"detail-type"`
	Time       string          `json:"time"`
	Region     string          `json:"region"`
	Resources  []string        `json:"resources"`
	Detail     json.RawMessage `json:"detail"`
}

func handler(ctx context.Context, event EventBridgeEvent) error {
	fmt.Printf("EventBridge event received:\n")
	fmt.Printf("  Source:      %s\n", event.Source)
	fmt.Printf("  DetailType:  %s\n", event.DetailType)
	fmt.Printf("  ID:          %s\n", event.ID)
	fmt.Printf("  Time:        %s\n", event.Time)

	var detail map[string]interface{}
	if err := json.Unmarshal(event.Detail, &detail); err == nil {
		prettyDetail, _ := json.MarshalIndent(detail, "  ", "  ")
		fmt.Printf("  Detail:\n  %s\n", string(prettyDetail))
	}

	return nil
}

func main() {
	lambda.Start(handler)
}
```

## Step 2 -- Create the Terraform Configuration

Create the following files in your exercise directory:

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
  default     = "eb-rules-demo"
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

resource "aws_iam_role" "handler" {
  name               = "${var.project_name}-handler-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

resource "aws_iam_role_policy_attachment" "handler_basic" {
  role       = aws_iam_role.handler.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
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

# Permission: allow EventBridge to invoke the Lambda function
resource "aws_lambda_permission" "eventbridge" {
  statement_id  = "AllowEventBridgeInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.handler.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.order_events.arn
}
```

### `events.tf`

```hcl
# EventBridge Rule -- matches order events from myapp.orders
resource "aws_cloudwatch_event_rule" "order_events" {
  name        = "${var.project_name}-order-events"
  description = "Match all order events from the orders service"

  event_pattern = jsonencode({
    source      = ["myapp.orders"]
    detail-type = ["OrderCreated", "OrderShipped", "OrderCancelled"]
  })
}

# Target: send matching events to the Lambda function
resource "aws_cloudwatch_event_target" "lambda" {
  rule = aws_cloudwatch_event_rule.order_events.name
  arn  = aws_lambda_function.handler.arn
}
```

### `outputs.tf`

```hcl
output "function_name" { value = aws_lambda_function.handler.function_name }
output "rule_name"     { value = aws_cloudwatch_event_rule.order_events.name }
```

## Step 4 -- Deploy and Test

```bash
cd handler && GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go && cd ..
terraform init
terraform apply -auto-approve
```

Publish events via CLI:

```bash
# This event matches the rule (source=myapp.orders, detail-type=OrderCreated)
aws events put-events --entries '[{
  "Source": "myapp.orders",
  "DetailType": "OrderCreated",
  "Detail": "{\"order_id\":\"order-100\",\"customer\":\"bob\",\"total\":75.00}"
}]'

# This event does NOT match (source=myapp.auth)
aws events put-events --entries '[{
  "Source": "myapp.auth",
  "DetailType": "UserLoggedIn",
  "Detail": "{\"user_id\":\"user-100\"}"
}]'
```

Verify that only the matching event triggered the Lambda:

```bash
sleep 5
aws logs filter-log-events \
  --log-group-name "/aws/lambda/eb-rules-demo-handler" \
  --filter-pattern "EventBridge event received" \
  --query "events[*].message" --output text
```

Expected: log entries for `OrderCreated` but not `UserLoggedIn`.

## Common Mistakes

### 1. Missing Lambda permission for EventBridge

Without `aws_lambda_permission` with principal `events.amazonaws.com`, EventBridge cannot invoke the Lambda. The rule matches events but delivery fails silently. Fix: add the permission as shown in the Terraform code above.

### 2. Using string values instead of arrays in event patterns

Event pattern fields must be arrays, even for single values. `{"source": "myapp.orders"}` fails validation. Fix: `{"source": ["myapp.orders"]}`.

### 3. Detail must be a JSON string in PutEvents

The `Detail` field in `PutEvents` must be a JSON string, not an object. In Go: `Detail: aws.String(`{"order_id": "123"}`)`, not a map.

## Verify What You Learned

```bash
# Verify rule exists with correct event pattern
aws events describe-rule --name eb-rules-demo-order-events \
  --query "EventPattern" --output text | jq .
```

Expected: `{"source": ["myapp.orders"], "detail-type": ["OrderCreated", "OrderShipped", "OrderCancelled"]}`

```bash
# Verify target is configured
aws events list-targets-by-rule --rule eb-rules-demo-order-events \
  --query "Targets[*].Arn" --output text
```

Expected: Lambda function ARN.

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

You created EventBridge rules with event pattern matching and triggered Lambda functions from custom events. In the next exercise, you will configure **EventBridge custom event buses with cross-account access** -- creating dedicated event buses with resource policies for multi-account event routing.

## Summary

- **EventBridge rules** evaluate events against JSON patterns and route matching events to targets
- Event patterns use **arrays** for field values -- `{"source": ["myapp.orders"]}`, not `{"source": "myapp.orders"}`
- Multiple values in a field use **OR logic** -- `["OrderCreated", "OrderShipped"]` matches either detail-type
- Multiple fields use **AND logic** -- both `source` and `detail-type` must match
- The `detail` field supports **nested matching** -- `{"detail": {"status": ["shipped"]}}` matches the detail JSON
- Custom events are published via `PutEvents` -- the `Detail` field must be a JSON **string**
- Lambda targets require an `aws_lambda_permission` with principal `events.amazonaws.com`
- Pattern operators include prefix, numeric, exists, anything-but, and IP address matching

## Reference

- [EventBridge Event Patterns](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-event-patterns.html)
- [EventBridge PutEvents API](https://docs.aws.amazon.com/eventbridge/latest/APIReference/API_PutEvents.html)
- [Terraform aws_cloudwatch_event_rule](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_event_rule)
- [Terraform aws_cloudwatch_event_target](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_event_target)

## Additional Resources

- [EventBridge Content-Based Filtering](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-event-patterns-content-based-filtering.html) -- prefix, numeric, IP address, and exists operators
- [EventBridge and Lambda](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-run-lambda-schedule.html) -- configuring Lambda as an EventBridge target
- [EventBridge Quotas](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-quota.html) -- limits on rules per bus, targets per rule, and PutEvents batch size
- [Building Event-Driven Architectures on AWS](https://docs.aws.amazon.com/prescriptive-guidance/latest/modernization-integrating-microservices/eventbridge.html) -- architectural patterns with EventBridge

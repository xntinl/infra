# 81. EventBridge Event-Driven Architecture

<!--
difficulty: intermediate
concepts: [eventbridge, custom-event-bus, event-rules, content-based-filtering, input-transformation, event-patterns, target-types, schema-registry, archive-replay, cross-account-events, dead-letter-queue]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: apply, analyze
prerequisites: [77-lambda-event-sources-patterns]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** EventBridge: $1.00 per million custom events. AWS service events are free. Lambda free tier covers invocations. SNS/SQS free tier covers messages. Total ~$0.01/hr. Remember to run `terraform destroy` when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Completed exercise 77 (Lambda event sources) | Understanding of async invocation |
| Go 1.21+ installed | `go version` |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Implement** custom event buses with content-based filtering rules and multiple targets.
2. **Analyze** event pattern matching syntax including prefix, suffix, numeric, and exists conditions.
3. **Apply** input transformations to reshape events before delivering to targets.
4. **Evaluate** when to use EventBridge choreography versus Step Functions orchestration.
5. **Design** event-driven architectures with proper dead-letter queues and cross-account event routing.

---

## Why This Matters

EventBridge is the SAA-C03 answer to "how do you build loosely coupled, event-driven architectures?" While SNS provides simple pub/sub fan-out, EventBridge adds content-based filtering, input transformation, schema discovery, event archiving, and replay. The exam tests the distinction: SNS fans out the same message to all subscribers; EventBridge routes events to specific targets based on event content.

The key architectural pattern is choreography versus orchestration. In orchestration (Step Functions), a central coordinator manages the workflow -- each step knows about the coordinator, and the coordinator knows about every step. In choreography (EventBridge), services emit events and consume events independently -- no service knows about the others. The order service emits "OrderCreated," and the payment, inventory, and notification services each have rules that match this event. Adding a new consumer requires only a new rule, not changing the producer.

The exam tests three specific EventBridge capabilities. First, event patterns with content-based filtering -- rules that match events based on field values, prefixes, numeric ranges, or field existence. Second, input transformation -- reshaping the event before it reaches the target so each consumer gets exactly the data it needs. Third, the default event bus (AWS service events) versus custom event buses (application events) -- custom buses provide isolation and cross-account sharing.

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
  default     = "saa-ex81"
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

### `events.tf`

```hcl
# ============================================================
# TODO 1: Custom Event Bus
# ============================================================
# Create a custom event bus for application events.
#
# Requirements:
#   - Resource: aws_cloudwatch_event_bus
#   - name = "${var.project_name}-orders"
#
# Why custom bus vs default bus?
#   - Default bus receives AWS service events (EC2, RDS, etc.)
#   - Custom bus isolates application events from AWS events
#   - Custom bus can be shared cross-account via resource policy
#   - Separate permission boundaries per bus
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_event_bus
# ============================================================


# ============================================================
# TODO 2: Content-Based Filtering Rules
# ============================================================
# Create rules that route events based on content.
#
# Rule 1: "high-value-orders" (priority orders)
#   - event_bus_name = custom bus
#   - event_pattern matching:
#     - source = ["com.myapp.orders"]
#     - detail-type = ["OrderCreated"]
#     - detail.amount > 100 (numeric filter)
#   - Target: Lambda function
#
# Rule 2: "electronics-orders" (category filter)
#   - event_bus_name = custom bus
#   - event_pattern matching:
#     - source = ["com.myapp.orders"]
#     - detail-type = ["OrderCreated"]
#     - detail.category prefix "electronics" (prefix filter)
#   - Target: SQS queue (for batch processing)
#
# Rule 3: "all-orders" (catch-all for logging)
#   - event_bus_name = custom bus
#   - event_pattern matching:
#     - source = ["com.myapp.orders"]
#   - Target: CloudWatch log group
#
# Docs: https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-event-patterns.html
# ============================================================


# ============================================================
# TODO 3: Input Transformation
# ============================================================
# Configure input transformation on the high-value-orders
# rule so the Lambda receives a simplified event payload.
#
# Original event detail:
#   { "order_id": "ORD-123", "amount": 150.00,
#     "customer": { "id": "C-1", "name": "John" },
#     "items": [...], "metadata": {...} }
#
# Transformed input for Lambda:
#   { "orderId": "<order_id>",
#     "amount": <amount>,
#     "customerName": "<customer.name>",
#     "priority": "high" }
#
# Requirements:
#   - input_transformer block on the event target
#   - input_paths_map: extract fields from the event
#   - input_template: JSON template using extracted fields
#
# Docs: https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-transform-target-input.html
# ============================================================


# ============================================================
# TODO 4: Send Test Events (CLI)
# ============================================================
# After terraform apply, send test events and verify routing:
#
#   a) High-value electronics order (matches rules 1, 2, 3):
#      aws events put-events --entries '[{
#        "Source": "com.myapp.orders",
#        "DetailType": "OrderCreated",
#        "EventBusName": "saa-ex81-orders",
#        "Detail": "{\"order_id\":\"ORD-001\",\"amount\":250.00,\"category\":\"electronics-laptops\",\"customer\":{\"id\":\"C-1\",\"name\":\"John\"}}"
#      }]'
#
#   b) Low-value order (matches only rule 3):
#      aws events put-events --entries '[{
#        "Source": "com.myapp.orders",
#        "DetailType": "OrderCreated",
#        "EventBusName": "saa-ex81-orders",
#        "Detail": "{\"order_id\":\"ORD-002\",\"amount\":25.00,\"category\":\"books\",\"customer\":{\"id\":\"C-2\",\"name\":\"Jane\"}}"
#      }]'
#
#   c) Check Lambda logs (high-value):
#      aws logs tail "/aws/lambda/saa-ex81-handler" --since 5m
#
#   d) Check SQS queue (electronics):
#      aws sqs receive-message --queue-url <QUEUE_URL> --max-number-of-messages 5
# ============================================================


# ---------- Supporting Resources ----------

resource "aws_sqs_queue" "electronics" {
  name = "${var.project_name}-electronics-orders"
}

resource "aws_cloudwatch_log_group" "orders" {
  name              = "/events/${var.project_name}/orders"
  retention_in_days = 1
}
```

### `outputs.tf`

```hcl
output "event_bus_name" {
  value = "Set after TODO 1 implementation"
}

output "sqs_queue_url" {
  value = aws_sqs_queue.electronics.url
}
```

---

## Event Pattern Syntax

| Operator | Syntax | Matches |
|---|---|---|
| **Exact** | `"status": ["active"]` | status == "active" |
| **Prefix** | `"status": [{"prefix": "act"}]` | status starts with "act" |
| **Suffix** | `"filename": [{"suffix": ".png"}]` | filename ends with ".png" |
| **Numeric** | `"amount": [{"numeric": [">", 100]}]` | amount > 100 |
| **Numeric range** | `"amount": [{"numeric": [">=", 10, "<", 100]}]` | 10 <= amount < 100 |
| **Exists** | `"email": [{"exists": true}]` | email field is present |
| **Not exists** | `"email": [{"exists": false}]` | email field is absent |
| **Anything-but** | `"status": [{"anything-but": "deleted"}]` | status != "deleted" |
| **OR** | `"status": ["active", "pending"]` | status in ("active", "pending") |

### EventBridge vs SNS

| Feature | EventBridge | SNS |
|---|---|---|
| **Filtering** | Content-based (field values, ranges, patterns) | Message attributes only (not body) |
| **Transformation** | Input transformer (reshape before delivery) | None |
| **Targets** | 20+ (Lambda, SQS, Step Functions, API dest, etc.) | Lambda, SQS, HTTP, email, SMS |
| **Schema** | Schema registry + discovery | None |
| **Replay** | Archive and replay events | None |
| **Cross-account** | Event bus resource policy | Topic policy |
| **Pricing** | $1/M custom events, free AWS events | $0.50/M publishes + delivery |

---

## Spot the Bug

The following EventBridge rule is supposed to match "OrderCreated" events but never triggers. Identify the problem before expanding the answer.

```hcl
resource "aws_cloudwatch_event_rule" "order_created" {
  name           = "order-created-rule"
  event_bus_name = aws_cloudwatch_event_bus.orders.name

  event_pattern = jsonencode({
    source      = ["com.myapp.orders"]
    detail_type = ["OrderCreated"]
  })
}
```

Events are sent with:

```bash
aws events put-events --entries '[{
  "Source": "com.myapp.orders",
  "DetailType": "OrderCreated",
  "EventBusName": "my-orders-bus",
  "Detail": "{\"order_id\":\"ORD-001\",\"amount\":50.00}"
}]'
```

<details>
<summary>Explain the bug</summary>

**The event pattern uses `detail_type` (with underscore) instead of `detail-type` (with hyphen).** In EventBridge event patterns, the field name is `detail-type` (hyphen), matching the EventBridge event envelope format. Using `detail_type` creates a pattern that looks for a custom field called `detail_type` in the event, which does not exist.

This is a common Terraform-specific bug because Terraform/HCL uses underscores in resource arguments, so developers instinctively use underscores. But the event pattern is a JSON document that must use the exact EventBridge field names.

**Fix:**

```hcl
event_pattern = jsonencode({
  source        = ["com.myapp.orders"]
  "detail-type" = ["OrderCreated"]
})
```

Note the quotes around `"detail-type"` -- required because HCL does not allow hyphens in bare identifiers.

The correct EventBridge event envelope fields are:
- `source` -- who sent the event
- `detail-type` -- what type of event (hyphenated)
- `detail` -- event payload
- `account` -- AWS account ID
- `region` -- AWS region
- `time` -- event timestamp

</details>

---

## Verify What You Learned

1. **Deploy the infrastructure:**
   ```bash
   terraform init && terraform apply -auto-approve
   ```

2. **Send a high-value electronics order:**
   ```bash
   BUS_NAME=$(terraform output -raw event_bus_name)
   aws events put-events --entries "[{
     \"Source\": \"com.myapp.orders\",
     \"DetailType\": \"OrderCreated\",
     \"EventBusName\": \"$BUS_NAME\",
     \"Detail\": \"{\\\"order_id\\\":\\\"ORD-001\\\",\\\"amount\\\":250.00,\\\"category\\\":\\\"electronics-laptops\\\",\\\"customer\\\":{\\\"id\\\":\\\"C-1\\\",\\\"name\\\":\\\"John\\\"}}\"
   }]"
   ```

3. **Verify Lambda received the transformed event:**
   ```bash
   sleep 5
   aws logs tail "/aws/lambda/saa-ex81-handler" --since 2m --format short
   ```
   Expected: Log showing transformed event with `orderId`, `amount`, `customerName`, `priority` fields.

4. **Verify SQS received the electronics event:**
   ```bash
   QUEUE_URL=$(terraform output -raw sqs_queue_url)
   aws sqs receive-message --queue-url "$QUEUE_URL" --max-number-of-messages 5 | jq '.Messages[0].Body'
   ```
   Expected: Event with category matching "electronics-laptops".

5. **Verify CloudWatch log group captured all events:**
   ```bash
   aws logs tail "/events/saa-ex81/orders" --since 5m --format short
   ```
   Expected: All sent events logged regardless of amount or category.

6. **Terraform state consistency:**
   ```bash
   terraform plan
   ```
   Expected: `No changes. Your infrastructure matches the configuration.`

---

## Solutions

<details>
<summary>events.tf -- TODO 1: Custom Event Bus</summary>

```hcl
resource "aws_cloudwatch_event_bus" "orders" {
  name = "${var.project_name}-orders"
  tags = { Name = "${var.project_name}-orders" }
}
```

</details>

<details>
<summary>events.tf -- TODO 2: Content-Based Filtering Rules</summary>

```hcl
# Rule 1: High-value orders (amount > 100)
resource "aws_cloudwatch_event_rule" "high_value" {
  name           = "${var.project_name}-high-value-orders"
  event_bus_name = aws_cloudwatch_event_bus.orders.name

  event_pattern = jsonencode({
    source        = ["com.myapp.orders"]
    "detail-type" = ["OrderCreated"]
    detail = {
      amount = [{ numeric = [">", 100] }]
    }
  })
}

resource "aws_lambda_permission" "eventbridge" {
  statement_id  = "AllowEventBridge"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.this.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.high_value.arn
}

resource "aws_cloudwatch_event_target" "high_value_lambda" {
  rule           = aws_cloudwatch_event_rule.high_value.name
  event_bus_name = aws_cloudwatch_event_bus.orders.name
  arn            = aws_lambda_function.this.arn

  # Input transformation added in TODO 3
}

# Rule 2: Electronics orders (category prefix)
resource "aws_cloudwatch_event_rule" "electronics" {
  name           = "${var.project_name}-electronics-orders"
  event_bus_name = aws_cloudwatch_event_bus.orders.name

  event_pattern = jsonencode({
    source        = ["com.myapp.orders"]
    "detail-type" = ["OrderCreated"]
    detail = {
      category = [{ prefix = "electronics" }]
    }
  })
}

resource "aws_sqs_queue_policy" "electronics" {
  queue_url = aws_sqs_queue.electronics.url
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "events.amazonaws.com" }
      Action    = "sqs:SendMessage"
      Resource  = aws_sqs_queue.electronics.arn
      Condition = {
        ArnEquals = { "aws:SourceArn" = aws_cloudwatch_event_rule.electronics.arn }
      }
    }]
  })
}

resource "aws_cloudwatch_event_target" "electronics_sqs" {
  rule           = aws_cloudwatch_event_rule.electronics.name
  event_bus_name = aws_cloudwatch_event_bus.orders.name
  arn            = aws_sqs_queue.electronics.arn
}

# Rule 3: All orders (catch-all)
resource "aws_cloudwatch_event_rule" "all_orders" {
  name           = "${var.project_name}-all-orders"
  event_bus_name = aws_cloudwatch_event_bus.orders.name

  event_pattern = jsonencode({
    source = ["com.myapp.orders"]
  })
}

data "aws_iam_policy_document" "logs" {
  statement {
    actions   = ["logs:CreateLogStream", "logs:PutLogEvents"]
    resources = ["${aws_cloudwatch_log_group.orders.arn}:*"]
    principals {
      type        = "Service"
      identifiers = ["events.amazonaws.com"]
    }
  }
}

resource "aws_cloudwatch_log_resource_policy" "orders" {
  policy_name     = "${var.project_name}-orders-log-policy"
  policy_document = data.aws_iam_policy_document.logs.json
}

resource "aws_cloudwatch_event_target" "all_orders_logs" {
  rule           = aws_cloudwatch_event_rule.all_orders.name
  event_bus_name = aws_cloudwatch_event_bus.orders.name
  arn            = aws_cloudwatch_log_group.orders.arn
}
```

One event can match multiple rules. A high-value electronics order matches all three rules: the Lambda receives it (via high-value rule), SQS receives it (via electronics rule), and CloudWatch Logs captures it (via catch-all rule).

</details>

<details>
<summary>events.tf -- TODO 3: Input Transformation</summary>

Update the high-value Lambda target with an input transformer:

```hcl
resource "aws_cloudwatch_event_target" "high_value_lambda" {
  rule           = aws_cloudwatch_event_rule.high_value.name
  event_bus_name = aws_cloudwatch_event_bus.orders.name
  arn            = aws_lambda_function.this.arn

  input_transformer {
    input_paths = {
      orderId      = "$.detail.order_id"
      amount       = "$.detail.amount"
      customerName = "$.detail.customer.name"
    }

    input_template = <<-TEMPLATE
      {
        "orderId": <orderId>,
        "amount": <amount>,
        "customerName": <customerName>,
        "priority": "high"
      }
    TEMPLATE
  }
}
```

Input transformation extracts specific fields from the event (`input_paths`) and inserts them into a template (`input_template`). The target receives only the transformed payload, not the full event envelope. This reduces the data transferred and simplifies the Lambda handler.

</details>

<details>
<summary>outputs.tf -- Updated Outputs</summary>

```hcl
output "event_bus_name" {
  value = aws_cloudwatch_event_bus.orders.name
}

output "sqs_queue_url" {
  value = aws_sqs_queue.electronics.url
}
```

</details>

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify:

```bash
aws events list-event-buses \
  --query 'EventBuses[?Name==`saa-ex81-orders`]'
```

Expected: Empty list.

---

## What's Next

Exercise 82 covers **serverless architecture patterns**, where you will design complete architectures for different use cases: synchronous request-response, asynchronous processing, streaming, and orchestration. This capstone exercise combines Lambda, API Gateway, SQS, EventBridge, Step Functions, and DynamoDB into cohesive patterns.

---

## Summary

- **EventBridge** routes events based on content using rules with pattern matching -- not just fan-out like SNS
- **Custom event buses** isolate application events from AWS service events and enable cross-account sharing
- **Event patterns** support exact match, prefix, suffix, numeric comparison, exists, and anything-but operators
- **Input transformation** reshapes events before delivery, so each target receives only the fields it needs
- **One event can match multiple rules** -- EventBridge evaluates all rules independently (unlike Step Functions Choice which takes the first match)
- **`detail-type`** uses a hyphen in event patterns (not underscore) -- a common Terraform bug since HCL uses underscores
- **EventBridge vs SNS**: EventBridge adds content filtering, transformation, schema registry, archive/replay; SNS is simpler for basic fan-out
- **Choreography (EventBridge) vs orchestration (Step Functions)**: use EventBridge when services evolve independently; Step Functions when you need a central workflow view
- **Dead-letter queues** on event targets catch delivery failures (target down, permission error, throttling)
- **Event archive and replay** enables re-processing historical events for debugging or backfilling

## Reference

- [Amazon EventBridge User Guide](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-what-is.html)
- [Event Pattern Syntax](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-event-patterns.html)
- [Terraform aws_cloudwatch_event_rule](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_event_rule)
- [EventBridge Pricing](https://aws.amazon.com/eventbridge/pricing/)

## Additional Resources

- [Input Transformation](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-transform-target-input.html) -- reshaping events for specific targets
- [Event Archive and Replay](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-archive.html) -- storing and replaying historical events
- [Cross-Account Event Delivery](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-cross-account.html) -- sharing events across AWS accounts
- [Schema Registry](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-schema.html) -- auto-discovering event schemas for code generation

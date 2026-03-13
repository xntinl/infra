# 48. SNS Message Filtering Policies

<!--
difficulty: basic
concepts: [sns-filter-policy, message-attributes, subscription-filter, attribute-based-filtering, filter-policy-scope]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: apply
prerequisites: [47-sns-topics-subscriptions-filtering]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates an SNS topic, two SQS queues, and subscriptions with filter policies. Cost is approximately $0.01/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally
- Completed exercise 47 (SNS fundamentals)

## Learning Objectives

After completing this exercise, you will be able to:

- **Construct** SNS subscription filter policies that route messages to specific subscribers based on message attributes
- **Apply** filter policy operators including exact match, prefix match, numeric range, and exists/not-exists conditions
- **Publish** messages with message attributes and verify that only matching subscribers receive them
- **Explain** the difference between `MessageAttributes` filter scope (default) and `MessageBody` filter scope
- **Describe** how unfiltered subscriptions receive all messages while filtered subscriptions receive only matching messages

## Why SNS Message Filtering

Without filter policies, every SNS subscriber receives every message. If you have an order processing system that publishes all order events to a single topic, both the express shipping handler and the standard shipping handler receive every order -- each must inspect the message and discard irrelevant ones. This wastes Lambda invocations and SQS message deliveries.

Filter policies solve this at the SNS level. You attach a JSON filter policy to each subscription that specifies which message attributes must match for delivery. SNS evaluates the filter before sending, so non-matching messages are never delivered to the subscriber. This reduces cost (fewer SQS messages, fewer Lambda invocations) and simplifies subscriber code (no need to filter in application logic).

The DVA-C02 exam tests filter policy syntax, operator types, and edge cases. Key exam traps: (1) filter policies match on `MessageAttributes` by default, not the message body -- use `FilterPolicyScope: MessageBody` to filter on body content; (2) a subscription without a filter policy receives all messages; (3) the `exists` operator can check for the presence or absence of an attribute; (4) string filters are exact match by default -- use `prefix` for partial matching.

## Step 1 -- Create the Terraform Configuration

Create the following files in your exercise directory:

### `providers.tf`

```hcl
terraform {
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 5.0" }
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
  default     = "sns-filter-demo"
}
```

### `events.tf`

```hcl
resource "aws_sns_topic" "orders" {
  name = "${var.project_name}-orders"
}

resource "aws_sqs_queue" "express" {
  name = "${var.project_name}-express"
}

data "aws_iam_policy_document" "express_policy" {
  statement {
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.express.arn]
    principals {
      type        = "Service"
      identifiers = ["sns.amazonaws.com"]
    }
    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = [aws_sns_topic.orders.arn]
    }
  }
}

resource "aws_sqs_queue_policy" "express" {
  queue_url = aws_sqs_queue.express.id
  policy    = data.aws_iam_policy_document.express_policy.json
}

# Subscription with filter policy: only receives express orders
resource "aws_sns_topic_subscription" "express" {
  topic_arn = aws_sns_topic.orders.arn
  protocol  = "sqs"
  endpoint  = aws_sqs_queue.express.arn

  filter_policy = jsonencode({
    order_type = ["express"]
  })
}

resource "aws_sqs_queue" "standard" {
  name = "${var.project_name}-standard"
}

data "aws_iam_policy_document" "standard_policy" {
  statement {
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.standard.arn]
    principals {
      type        = "Service"
      identifiers = ["sns.amazonaws.com"]
    }
    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = [aws_sns_topic.orders.arn]
    }
  }
}

resource "aws_sqs_queue_policy" "standard" {
  queue_url = aws_sqs_queue.standard.id
  policy    = data.aws_iam_policy_document.standard_policy.json
}

# Subscription with filter policy: only receives standard orders
resource "aws_sns_topic_subscription" "standard" {
  topic_arn = aws_sns_topic.orders.arn
  protocol  = "sqs"
  endpoint  = aws_sqs_queue.standard.arn

  filter_policy = jsonencode({
    order_type = ["standard"]
  })
}
```

### `outputs.tf`

```hcl
output "topic_arn"       { value = aws_sns_topic.orders.arn }
output "express_queue_url" { value = aws_sqs_queue.express.url }
output "standard_queue_url" { value = aws_sqs_queue.standard.url }
```

## Step 2 -- Deploy and Publish Filtered Messages

```bash
terraform init
terraform apply -auto-approve
```

Publish an express order:

```bash
TOPIC_ARN=$(terraform output -raw topic_arn)

aws sns publish \
  --topic-arn "$TOPIC_ARN" \
  --message '{"order_id":"order-001","total":99.99}' \
  --message-attributes '{"order_type": {"DataType": "String", "StringValue": "express"}}'
```

Publish a standard order:

```bash
aws sns publish \
  --topic-arn "$TOPIC_ARN" \
  --message '{"order_id":"order-002","total":29.99}' \
  --message-attributes '{"order_type": {"DataType": "String", "StringValue": "standard"}}'
```

## Step 3 -- Verify Filtered Delivery

Check the express queue -- should only contain the express order:

```bash
EXPRESS_URL=$(terraform output -raw express_queue_url)

aws sqs receive-message --queue-url "$EXPRESS_URL" \
  --max-number-of-messages 10 --wait-time-seconds 5 \
  --query "Messages[*].Body" --output text | jq -r '.Message' | jq .
```

Expected: `{"order_id":"order-001","total":99.99}`

Check the standard queue -- should only contain the standard order:

```bash
STANDARD_URL=$(terraform output -raw standard_queue_url)

aws sqs receive-message --queue-url "$STANDARD_URL" \
  --max-number-of-messages 10 --wait-time-seconds 5 \
  --query "Messages[*].Body" --output text | jq -r '.Message' | jq .
```

Expected: `{"order_id":"order-002","total":29.99}`

## Step 4 -- Advanced Filter Policy Operators

SNS filter policies support several operators beyond exact string matching:

```json
{
  "order_type": ["express"],
  "priority": [{"numeric": [">=", 5]}],
  "region": [{"prefix": "us-"}],
  "is_premium": [{"exists": true}]
}
```

| Operator | Example | Matches |
|---|---|---|
| Exact match | `["express"]` | Attribute value equals "express" |
| Multiple values | `["express", "priority"]` | Attribute equals "express" OR "priority" |
| Prefix | `[{"prefix": "us-"}]` | Attribute starts with "us-" |
| Numeric range | `[{"numeric": [">=", 5, "<=", 100]}]` | Numeric value between 5 and 100 |
| Exists | `[{"exists": true}]` | Attribute is present (any value) |
| Not exists | `[{"exists": false}]` | Attribute is absent |
| Anything-but | `[{"anything-but": ["test"]}]` | Attribute is not "test" |

Publish a message that matches none of the filter policies:

```bash
aws sns publish \
  --topic-arn "$TOPIC_ARN" \
  --message '{"order_id":"order-003","total":15.00}' \
  --message-attributes '{"order_type": {"DataType": "String", "StringValue": "bulk"}}'
```

Neither queue receives this message because `order_type=bulk` does not match either filter policy.

## Common Mistakes

### 1. Publishing without message attributes when filter expects them

If a filter policy checks `order_type` but the publisher does not include `order_type` as a message attribute, the message is filtered out (not delivered).

**Wrong -- message body has order_type but message attributes do not:**

```bash
aws sns publish \
  --topic-arn "$TOPIC_ARN" \
  --message '{"order_type":"express","order_id":"order-004"}'
  # No --message-attributes flag
```

**What happens:** No subscriber receives the message because filter policies check `MessageAttributes`, not the message body.

**Fix -- include the attribute in --message-attributes:**

```bash
aws sns publish \
  --topic-arn "$TOPIC_ARN" \
  --message '{"order_id":"order-004"}' \
  --message-attributes '{"order_type": {"DataType": "String", "StringValue": "express"}}'
```

### 2. Confusing filter policy scope

By default, filter policies match on `MessageAttributes`. To filter based on the message body (JSON content), you must set `filter_policy_scope = "MessageBody"`:

```hcl
resource "aws_sns_topic_subscription" "body_filter" {
  topic_arn = aws_sns_topic.orders.arn
  protocol  = "sqs"
  endpoint  = aws_sqs_queue.express.arn

  filter_policy_scope = "MessageBody"
  filter_policy = jsonencode({
    order_type = ["express"]
  })
}
```

### 3. Expecting numeric filtering on String DataType

Numeric operators like `{"numeric": [">=", 5]}` only work when the message attribute `DataType` is `Number`. Using `DataType: String` with a numeric value causes the filter to never match.

## Verify What You Learned

```bash
# Verify filter policies on subscriptions
aws sns get-subscription-attributes \
  --subscription-arn $(aws sns list-subscriptions-by-topic --topic-arn $(terraform output -raw topic_arn) \
    --query "Subscriptions[?Protocol=='sqs'] | [0].SubscriptionArn" --output text) \
  --query "Attributes.FilterPolicy" --output text | jq .
```

Expected: `{"order_type": ["express"]}` or `{"order_type": ["standard"]}` depending on which subscription is returned.

```bash
# Verify express queue is empty (all messages consumed in Step 3)
aws sqs get-queue-attributes --queue-url $(terraform output -raw express_queue_url) \
  --attribute-names ApproximateNumberOfMessages --query "Attributes.ApproximateNumberOfMessages" --output text
```

Expected: `0`

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

You configured SNS filter policies to route messages to specific subscribers based on message attributes. In the next exercise, you will build **EventBridge rules with event patterns** -- creating rules that match custom events on source, detail-type, and detail fields to trigger Lambda functions.

## Summary

- **Filter policies** are JSON documents attached to SNS subscriptions that control which messages are delivered
- By default, filter policies match on **MessageAttributes** (not the message body) -- use `FilterPolicyScope: MessageBody` to filter on body content
- A subscription **without** a filter policy receives **all** messages published to the topic
- Filter operators include exact match, prefix, numeric range, exists/not-exists, and anything-but
- Messages that match **no** subscription filter are effectively dropped -- SNS does not deliver them
- Message attributes must be included in the `--message-attributes` parameter, not embedded in the `--message` body
- Numeric filters require `DataType: Number` on the message attribute -- `DataType: String` with numeric values will not match

## Reference

- [SNS Message Filtering](https://docs.aws.amazon.com/sns/latest/dg/sns-message-filtering.html)
- [SNS Filter Policy Operators](https://docs.aws.amazon.com/sns/latest/dg/sns-subscription-filter-policies.html)
- [Terraform aws_sns_topic_subscription filter_policy](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sns_topic_subscription#filter_policy)
- [SNS Message Attributes](https://docs.aws.amazon.com/sns/latest/dg/sns-message-attributes.html)

## Additional Resources

- [Filter Policy Scope](https://docs.aws.amazon.com/sns/latest/dg/sns-message-filtering.html#filter-policy-scope) -- choosing between MessageAttributes and MessageBody filtering
- [Filter Policy Constraints](https://docs.aws.amazon.com/sns/latest/dg/sns-subscription-filter-policies.html#subscription-filter-policy-constraints) -- limits on policy complexity (5 attributes, 150 values)
- [SNS Filter Policy Best Practices](https://docs.aws.amazon.com/sns/latest/dg/sns-message-filtering.html) -- when to use filtering vs separate topics

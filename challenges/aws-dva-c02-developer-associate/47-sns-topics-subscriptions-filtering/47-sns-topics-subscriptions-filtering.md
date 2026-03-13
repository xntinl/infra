# 47. SNS Topics, Subscriptions, and Message Delivery

<!--
difficulty: basic
concepts: [sns-topic, sns-subscription, sqs-subscription, lambda-subscription, email-subscription, message-publish, delivery-verification]
tools: [terraform, aws-cli]
estimated_time: 30m
bloom_level: apply
prerequisites: [01-lambda-environment-layers-configuration]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates an SNS topic, SQS queues, and a Lambda function. SNS pricing is $0.50 per million requests and negligible for testing. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally (for compiling the Lambda binary)

## Learning Objectives

After completing this exercise, you will be able to:

- **Construct** an SNS topic with SQS, Lambda, and email subscription types using Terraform
- **Publish** messages to an SNS topic using the AWS CLI and verify delivery to all subscribers
- **Explain** the differences between SNS subscription protocols (sqs, lambda, email) and their confirmation requirements
- **Configure** an SQS queue policy that grants SNS permission to send messages to the queue
- **Verify** message delivery by inspecting SQS messages and Lambda CloudWatch logs

## Why SNS Topics and Subscriptions

Amazon SNS implements the publish-subscribe (pub/sub) pattern. A publisher sends a message to a topic, and SNS delivers copies to all subscribers. This decouples producers from consumers -- the publisher does not need to know who is listening or how many subscribers exist. Adding a new consumer is a configuration change, not a code change.

The DVA-C02 exam tests three key concepts. First, different subscription protocols have different confirmation requirements: SQS and Lambda subscriptions are confirmed automatically when created via Terraform or CloudFormation, but email and HTTP subscriptions require the subscriber to click a confirmation link. Second, SQS subscriptions require a queue policy that grants `sqs:SendMessage` permission to the SNS topic -- without this, messages are silently dropped. Third, Lambda subscriptions require a resource-based policy (`aws_lambda_permission`) allowing SNS to invoke the function. Missing either permission is a common exam scenario where "messages are published but subscribers don't receive them."

## Step 1 -- Create the Lambda Subscriber Code

### `subscriber/main.go`

```go
package main

import (
	"context"
	"fmt"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

func handler(ctx context.Context, event events.SNSEvent) error {
	for _, record := range event.Records {
		sns := record.SNS
		fmt.Printf("Received SNS message: MessageId=%s Subject=%s Message=%s\n",
			sns.MessageID, sns.Subject, sns.Message)
		fmt.Printf("TopicArn=%s Timestamp=%s\n", sns.TopicArn, sns.Timestamp.String())

		// Log message attributes
		for key, attr := range sns.MessageAttributes {
			fmt.Printf("Attribute: %s = %s (Type: %s)\n", key, attr.Value, attr.Type)
		}
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
  default     = "sns-demo"
}
```

### `events.tf`

```hcl
# SNS Topic
resource "aws_sns_topic" "orders" {
  name = "${var.project_name}-orders"
}

# SQS Subscriber
resource "aws_sqs_queue" "order_processor" {
  name = "${var.project_name}-order-processor"
}

# Queue policy -- grants SNS permission to send messages
data "aws_iam_policy_document" "sqs_policy" {
  statement {
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.order_processor.arn]
    principals { type = "Service"; identifiers = ["sns.amazonaws.com"] }
    condition { test = "ArnEquals"; variable = "aws:SourceArn"; values = [aws_sns_topic.orders.arn] }
  }
}

resource "aws_sqs_queue_policy" "order_processor" {
  queue_url = aws_sqs_queue.order_processor.id
  policy    = data.aws_iam_policy_document.sqs_policy.json
}

resource "aws_sns_topic_subscription" "sqs" {
  topic_arn = aws_sns_topic.orders.arn
  protocol  = "sqs"
  endpoint  = aws_sqs_queue.order_processor.arn
}

# SNS -> Lambda subscription
resource "aws_sns_topic_subscription" "lambda" {
  topic_arn = aws_sns_topic.orders.arn
  protocol  = "lambda"
  endpoint  = aws_lambda_function.subscriber.arn
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

resource "aws_iam_role" "subscriber" {
  name               = "${var.project_name}-subscriber-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

resource "aws_iam_role_policy_attachment" "subscriber_basic" {
  role       = aws_iam_role.subscriber.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}
```

### `build.tf`

```hcl
resource "null_resource" "go_build" {
  triggers = {
    source_hash = filebase64sha256("${path.module}/subscriber/main.go")
  }
  provisioner "local-exec" {
    command     = "GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go"
    working_dir = "${path.module}/subscriber"
  }
}

data "archive_file" "subscriber" {
  type        = "zip"
  source_file = "${path.module}/subscriber/bootstrap"
  output_path = "${path.module}/build/subscriber.zip"
  depends_on  = [null_resource.go_build]
}
```

### `lambda.tf`

```hcl
resource "aws_cloudwatch_log_group" "subscriber" {
  name              = "/aws/lambda/${var.project_name}-subscriber"
  retention_in_days = 1
}

resource "aws_lambda_function" "subscriber" {
  function_name    = "${var.project_name}-subscriber"
  filename         = data.archive_file.subscriber.output_path
  source_code_hash = data.archive_file.subscriber.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.subscriber.arn
  timeout          = 10

  depends_on = [aws_iam_role_policy_attachment.subscriber_basic, aws_cloudwatch_log_group.subscriber]
}

# Lambda permission -- allows SNS to invoke the function.
# Without this, SNS cannot trigger the Lambda and messages are dropped.
resource "aws_lambda_permission" "sns_invoke" {
  statement_id  = "AllowSNSInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.subscriber.function_name
  principal     = "sns.amazonaws.com"
  source_arn    = aws_sns_topic.orders.arn
}
```

### `outputs.tf`

```hcl
output "topic_arn"      { value = aws_sns_topic.orders.arn }
output "queue_url"      { value = aws_sqs_queue.order_processor.url }
output "function_name"  { value = aws_lambda_function.subscriber.function_name }
```

## Step 3 -- Build and Apply

```bash
cd subscriber && GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go && cd ..
terraform init
terraform apply -auto-approve
```

## Step 4 -- Publish a Message and Verify Delivery

Publish a message with attributes:

```bash
TOPIC_ARN=$(terraform output -raw topic_arn)

aws sns publish \
  --topic-arn "$TOPIC_ARN" \
  --subject "New Order" \
  --message '{"order_id":"order-001","customer":"alice","total":49.99}' \
  --message-attributes '{
    "order_type": {"DataType": "String", "StringValue": "standard"},
    "priority": {"DataType": "Number", "StringValue": "1"}
  }'
```

Verify delivery to SQS:

```bash
QUEUE_URL=$(terraform output -raw queue_url)

aws sqs receive-message \
  --queue-url "$QUEUE_URL" \
  --max-number-of-messages 1 \
  --wait-time-seconds 5 | jq -r '.Messages[0].Body' | jq .
```

The SQS message body is an SNS notification envelope containing `Type`, `MessageId`, `TopicArn`, `Subject`, `Message`, and `MessageAttributes`.

Verify delivery to Lambda by checking CloudWatch Logs:

```bash
FUNCTION_NAME=$(terraform output -raw function_name)

aws logs filter-log-events \
  --log-group-name "/aws/lambda/$FUNCTION_NAME" \
  --filter-pattern "Received SNS message" \
  --query "events[*].message" --output text
```

## Common Mistakes

### 1. Missing SQS queue policy for SNS

Without `aws_sqs_queue_policy` granting `sqs:SendMessage` to `sns.amazonaws.com`, SNS silently drops messages. The subscription appears confirmed, the publish returns a MessageId, but the queue stays empty. Fix: add the queue policy as shown in the Terraform code above.

### 2. Missing Lambda permission for SNS

Without `aws_lambda_permission` allowing `sns.amazonaws.com` to invoke the function, SNS gets AccessDenied. Fix: add `aws_lambda_permission` with `principal = "sns.amazonaws.com"` as shown above.

### 3. Expecting email subscriptions to auto-confirm

SQS and Lambda subscriptions are auto-confirmed via Terraform. Email subscriptions require the recipient to click a confirmation link. Until confirmed, the subscription status is `PendingConfirmation` and no messages are delivered.

## Verify What You Learned

```bash
# Verify topic exists
aws sns list-topics --query "Topics[?contains(TopicArn, 'sns-demo')]" --output text
```

Expected: one topic ARN containing `sns-demo-orders`.

```bash
# Verify subscriptions
aws sns list-subscriptions-by-topic --topic-arn $(terraform output -raw topic_arn) \
  --query "Subscriptions[*].{Protocol:Protocol,Endpoint:Endpoint}" --output table
```

Expected: two subscriptions (sqs and lambda).

```bash
# Publish and verify round-trip
aws sns publish --topic-arn $(terraform output -raw topic_arn) \
  --message "verification-test" --output text

sleep 3

aws sqs receive-message --queue-url $(terraform output -raw queue_url) \
  --max-number-of-messages 1 --query "Messages[0].Body" --output text | grep -q "verification-test" && echo "SQS delivery verified" || echo "SQS delivery FAILED"
```

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

You built a pub/sub system with SNS delivering to SQS and Lambda subscribers. In the next exercise, you will configure **SNS message filtering policies** -- routing messages to different SQS queues based on message attributes so that each subscriber only receives relevant messages.

## Summary

- **SNS topics** implement pub/sub: one publisher, many subscribers, each receiving a copy of every message
- **Subscription protocols** include sqs, lambda, email, http/https, sms, and application (mobile push)
- SQS and Lambda subscriptions are **auto-confirmed** via Terraform; email and HTTP require manual confirmation
- SQS subscriptions require a **queue policy** granting `sqs:SendMessage` to the SNS service principal
- Lambda subscriptions require an **aws_lambda_permission** resource allowing `sns.amazonaws.com` to invoke the function
- Missing permissions cause **silent message drops** -- no error on the publish side, which makes debugging difficult
- SNS messages delivered to SQS are wrapped in an **SNS notification envelope** containing metadata (TopicArn, MessageId, etc.)
- **Message attributes** are key-value metadata attached to messages -- used for filtering in the next exercise

## Reference

- [Amazon SNS Developer Guide](https://docs.aws.amazon.com/sns/latest/dg/welcome.html)
- [SNS Message Delivery](https://docs.aws.amazon.com/sns/latest/dg/sns-message-delivery.html)
- [Terraform aws_sns_topic](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sns_topic)
- [Terraform aws_sns_topic_subscription](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sns_topic_subscription)
- [Terraform aws_sqs_queue_policy](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sqs_queue_policy)

## Additional Resources

- [SNS Subscription Protocols](https://docs.aws.amazon.com/sns/latest/dg/sns-create-subscribe-endpoint-to-topic.html) -- details on each protocol type and confirmation requirements
- [SNS Message Attributes](https://docs.aws.amazon.com/sns/latest/dg/sns-message-attributes.html) -- how to attach metadata for filtering and routing
- [SNS Raw Message Delivery](https://docs.aws.amazon.com/sns/latest/dg/sns-large-payload-raw-message-delivery.html) -- skip the SNS envelope and deliver the raw message body to SQS
- [Lambda Permissions for SNS](https://docs.aws.amazon.com/lambda/latest/dg/with-sns.html) -- configuring resource-based policies for SNS-triggered Lambda functions

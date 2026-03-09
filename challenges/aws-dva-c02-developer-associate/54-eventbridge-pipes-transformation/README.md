# 54. EventBridge Pipes with Filtering and Transformation

<!--
difficulty: advanced
concepts: [eventbridge-pipes, pipe-source, pipe-filter, pipe-enrichment, pipe-target, input-transformation, sqs-to-step-functions, lambda-enrichment]
tools: [terraform, aws-cli]
estimated_time: 55m
bloom_level: evaluate
prerequisites: [49-eventbridge-rules-event-patterns, 08-sqs-lambda-concurrency-throttling]
aws_cost: ~$0.02/hr
-->

> **AWS Cost Warning:** This exercise creates EventBridge Pipes, SQS queues, a Lambda function, and a Step Functions state machine. Cost is approximately $0.02/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| jq installed | `jq --version` |

## Learning Objectives

After completing this exercise, you will be able to:

- **Design** an EventBridge Pipe with all four stages: source, filter, enrichment, and target
- **Configure** SQS as a pipe source with filter patterns that discard irrelevant messages before processing
- **Implement** a Lambda enrichment function that augments event data before it reaches the target
- **Evaluate** input transformation syntax to reshape event payloads between pipe stages
- **Analyze** enrichment Lambda response formats and identify how incorrect formats cause pipe failures

## Why EventBridge Pipes

EventBridge Pipes provide point-to-point integrations between AWS services with optional filtering, enrichment, and transformation -- all without writing glue code. Before Pipes, connecting SQS to Step Functions required an intermediary Lambda function that polled SQS, transformed the message, and started a Step Functions execution. Pipes eliminate that Lambda, reducing cost, latency, and code to maintain.

A pipe has four stages: (1) **Source** -- where events originate (SQS, DynamoDB Streams, Kinesis, etc.); (2) **Filter** -- optional pattern matching to discard events before processing; (3) **Enrichment** -- optional Lambda or API call to augment the event with additional data; (4) **Target** -- where the processed event is delivered (Step Functions, Lambda, SQS, EventBridge, etc.).

The DVA-C02 exam tests pipe configuration, especially the enrichment stage. The most common trap is the enrichment Lambda response format: the Lambda must return the enriched event as a JSON object (or array of objects). Returning a string, null, or an incorrectly structured response causes the pipe to fail silently or drop events. Understanding input transformation syntax (`$.body`, `$.detail`, etc.) is also tested.

## The Challenge

Build an EventBridge Pipe that connects an SQS queue (source) to a Step Functions state machine (target) with a filter stage that only processes high-priority orders and an enrichment Lambda that adds customer details from DynamoDB.

### Requirements

| Requirement | Description |
|---|---|
| Source | SQS queue receiving order events |
| Filter | Only process messages where `priority` is `high` in the message body |
| Enrichment | Lambda function that looks up customer details from DynamoDB and merges them into the event |
| Target | Step Functions state machine that processes the enriched order |
| Input Transform | Transform the SQS message body into the Step Functions input format |

### Architecture

```
  +-----------+     +----------+     +-------------+     +----------------+
  |  SQS      |     |  Filter  |     |  Enrichment |     |  Step Functions |
  |  Source    |---->|  Stage   |---->|  Lambda     |---->|  Target        |
  |           |     |          |     |             |     |                |
  | All orders|     | priority |     | Lookup      |     | Process order  |
  |           |     | = "high" |     | customer    |     | with full      |
  |           |     | only     |     | from DDB    |     | details        |
  +-----------+     +----------+     +-------------+     +----------------+
       |                 |                  |
       | low priority    |                  |
       | orders are      |                  |
       | discarded       |                  |
       +----- X ---------+                  |
                                            v
                                   +----------------+
                                   |  DynamoDB      |
                                   |  Customers     |
                                   |  Table         |
                                   +----------------+
```

## Hints

<details>
<summary>Hint 1: Creating the EventBridge Pipe resource</summary>

The pipe resource connects all four stages. The source and target are required; filter and enrichment are optional.

```hcl
resource "aws_pipes_pipe" "order_processing" {
  name     = "order-processing-pipe"
  role_arn = aws_iam_role.pipe.arn

  source = aws_sqs_queue.orders.arn

  source_parameters {
    sqs_queue_parameters {
      batch_size                         = 1
      maximum_batching_window_in_seconds = 0
    }

    filter_criteria {
      filter {
        pattern = jsonencode({
          body = {
            priority = ["high"]
          }
        })
      }
    }
  }

  enrichment = aws_lambda_function.enrichment.arn

  target = aws_sfn_state_machine.processor.arn

  target_parameters {
    step_function_state_machine_parameters {
      invocation_type = "FIRE_AND_FORGET"
    }
  }
}
```

Note: for SQS sources, the filter pattern uses `body` to match on the parsed message body JSON.

</details>

<details>
<summary>Hint 2: Enrichment Lambda input and output format</summary>

The enrichment Lambda receives an array of events (even with batch_size=1, the input is an array). It must return an array of the same length with enriched objects:

```go
// Input to enrichment Lambda (array of pipe events)
// [{"body": "{\"order_id\":\"o-1\",\"customer_id\":\"c-1\",\"priority\":\"high\"}"}]

// Expected output (array of enriched objects)
// [{"order_id":"o-1","customer_id":"c-1","priority":"high","customer_name":"Alice","customer_tier":"premium"}]
```

If the Lambda returns a single object instead of an array, or returns a string, the pipe fails.

</details>

<details>
<summary>Hint 3: Enrichment Lambda Go implementation</summary>

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"

    "github.com/aws/aws-lambda-go/lambda"
    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

var ddbClient *dynamodb.Client

func init() {
    cfg, _ := config.LoadDefaultConfig(context.TODO())
    ddbClient = dynamodb.NewFromConfig(cfg)
}

type PipeEvent struct {
    Body string `json:"body"`
}

type Order struct {
    OrderID    string  `json:"order_id"`
    CustomerID string  `json:"customer_id"`
    Priority   string  `json:"priority"`
    Amount     float64 `json:"amount"`
}

type EnrichedOrder struct {
    Order
    CustomerName string `json:"customer_name"`
    CustomerTier string `json:"customer_tier"`
}

func handler(ctx context.Context, events []PipeEvent) ([]EnrichedOrder, error) {
    var results []EnrichedOrder

    for _, evt := range events {
        var order Order
        if err := json.Unmarshal([]byte(evt.Body), &order); err != nil {
            return nil, fmt.Errorf("failed to parse order: %w", err)
        }

        // Look up customer in DynamoDB
        result, err := ddbClient.GetItem(ctx, &dynamodb.GetItemInput{
            TableName: aws.String("pipes-demo-customers"),
            Key: map[string]types.AttributeValue{
                "customer_id": &types.AttributeValueMemberS{Value: order.CustomerID},
            },
        })
        if err != nil {
            return nil, fmt.Errorf("failed to get customer: %w", err)
        }

        enriched := EnrichedOrder{Order: order}
        if result.Item != nil {
            if name, ok := result.Item["name"]; ok {
                enriched.CustomerName = name.(*types.AttributeValueMemberS).Value
            }
            if tier, ok := result.Item["tier"]; ok {
                enriched.CustomerTier = tier.(*types.AttributeValueMemberS).Value
            }
        }

        results = append(results, enriched)
    }

    return results, nil
}

func main() {
    lambda.Start(handler)
}
```

</details>

<details>
<summary>Hint 4: IAM role for the pipe</summary>

The pipe needs permissions to read from the source, invoke the enrichment, and start the target:

```hcl
data "aws_iam_policy_document" "pipe_policy" {
  # Read from SQS source
  statement {
    actions = [
      "sqs:ReceiveMessage",
      "sqs:DeleteMessage",
      "sqs:GetQueueAttributes",
    ]
    resources = [aws_sqs_queue.orders.arn]
  }

  # Invoke enrichment Lambda
  statement {
    actions   = ["lambda:InvokeFunction"]
    resources = [aws_lambda_function.enrichment.arn]
  }

  # Start Step Functions execution
  statement {
    actions   = ["states:StartExecution"]
    resources = [aws_sfn_state_machine.processor.arn]
  }
}
```

</details>

<details>
<summary>Hint 5: Step Functions state machine as target</summary>

A simple state machine that logs the enriched order:

```hcl
resource "aws_sfn_state_machine" "processor" {
  name     = "pipes-demo-processor"
  role_arn = aws_iam_role.sfn.arn

  definition = jsonencode({
    StartAt = "ProcessOrder"
    States = {
      ProcessOrder = {
        Type = "Pass"
        Result = {
          status = "processed"
        }
        ResultPath = "$.processing_result"
        End = true
      }
    }
  })
}
```

</details>

## Spot the Bug

A developer creates an EventBridge Pipe with a Lambda enrichment stage. The pipe starts but events are dropped after the enrichment stage. The Lambda executes successfully but the target never receives events.

```go
func handler(ctx context.Context, events []PipeEvent) (string, error) {
    var results []EnrichedOrder

    for _, evt := range events {
        var order Order
        json.Unmarshal([]byte(evt.Body), &order)

        enriched := EnrichedOrder{
            Order:        order,
            CustomerName: "Alice",
            CustomerTier: "premium",
        }
        results = append(results, enriched)
    }

    // Return as JSON string
    data, _ := json.Marshal(results)
    return string(data), nil
}
```

<details>
<summary>Explain the bug</summary>

The enrichment Lambda returns a **string** (`string(data)`) instead of a **JSON array**. EventBridge Pipes expects the enrichment Lambda to return a JSON array of objects matching the input array length. When the Lambda returns a string, Pipes cannot parse it as structured data and drops the event.

The return type `(string, error)` causes the Lambda runtime to serialize the result as a JSON string literal (with escaped quotes), not as a parsed JSON array.

**Fix -- return the array directly, not as a string:**

```go
func handler(ctx context.Context, events []PipeEvent) ([]EnrichedOrder, error) {
    var results []EnrichedOrder

    for _, evt := range events {
        var order Order
        json.Unmarshal([]byte(evt.Body), &order)

        enriched := EnrichedOrder{
            Order:        order,
            CustomerName: "Alice",
            CustomerTier: "premium",
        }
        results = append(results, enriched)
    }

    // Return the array directly -- Pipes expects []object, not string
    return results, nil
}
```

The key difference: the return type is `([]EnrichedOrder, error)` instead of `(string, error)`. The Lambda runtime serializes the Go struct array into a JSON array, which Pipes can parse correctly.

On the exam, when a pipe's enrichment stage "succeeds but events don't reach the target," check the enrichment Lambda's return format. It must be a JSON array.

</details>

## Verify What You Learned

```bash
# Verify pipe exists and is running
aws pipes list-pipes --query "Pipes[?contains(Name, 'pipes-demo')].{Name:Name,State:CurrentState}" --output table
```

Expected: one pipe in `RUNNING` state.

```bash
# Send a high-priority order (should pass filter)
aws sqs send-message --queue-url $(terraform output -raw source_queue_url) \
  --message-body '{"order_id":"o-001","customer_id":"c-001","priority":"high","amount":99.99}'

# Send a low-priority order (should be filtered out)
aws sqs send-message --queue-url $(terraform output -raw source_queue_url) \
  --message-body '{"order_id":"o-002","customer_id":"c-002","priority":"low","amount":9.99}'

sleep 15

# Check Step Functions executions (only high-priority should trigger)
aws stepfunctions list-executions \
  --state-machine-arn $(terraform output -raw state_machine_arn) \
  --query "executions[*].{Name:name,Status:status}" --output table
```

Expected: one execution (for the high-priority order), status SUCCEEDED.

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

You built an EventBridge Pipe with filtering, Lambda enrichment, and Step Functions as a target. In the next exercise, you will implement the **fan-out pattern with SNS and multiple SQS subscribers** -- routing order events to payment, inventory, and notification queues with filter policies.

## Summary

- **EventBridge Pipes** connect sources to targets with optional filter and enrichment stages
- Pipes support sources: SQS, DynamoDB Streams, Kinesis, MSK, self-managed Kafka
- The **filter stage** uses JSON pattern matching on the event body (for SQS: `body.field = ["value"]`)
- The **enrichment Lambda** receives an array of events and must return an **array of objects** -- returning a string or single object causes event loss
- **Input transformations** reshape data between stages using JSONPath syntax (`$.body`, `$.detail`)
- Pipes require an **IAM execution role** with permissions for all stages (source read, enrichment invoke, target write)
- Pipes replace custom glue Lambda functions, reducing cost and operational overhead
- Filter patterns for SQS sources use `body` as the top-level key to access parsed message body JSON

## Reference

- [EventBridge Pipes](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-pipes.html)
- [Pipes Filter Patterns](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-pipes-event-filtering.html)
- [Pipes Enrichment](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-pipes-enrichment.html)
- [Terraform aws_pipes_pipe](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/pipes_pipe)

## Additional Resources

- [Pipes Input Transformation](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-pipes-input-transformation.html) -- reshaping events between stages with JSONPath
- [Pipes Supported Sources and Targets](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-pipes-event-source.html) -- complete list of supported integrations
- [Pipes vs EventBridge Rules](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-pipes.html) -- when to use Pipes (point-to-point) vs Rules (event bus routing)
- [Pipes Logging and Monitoring](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-pipes-monitoring.html) -- CloudWatch metrics and execution logging

<details>
<summary>Full Solution</summary>

### `lambda/main.go`

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"

    "github.com/aws/aws-lambda-go/lambda"
    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

var ddbClient *dynamodb.Client

func init() {
    cfg, _ := config.LoadDefaultConfig(context.TODO())
    ddbClient = dynamodb.NewFromConfig(cfg)
}

type PipeEvent struct {
    Body string `json:"body"`
}

type Order struct {
    OrderID    string  `json:"order_id"`
    CustomerID string  `json:"customer_id"`
    Priority   string  `json:"priority"`
    Amount     float64 `json:"amount"`
}

type EnrichedOrder struct {
    Order
    CustomerName string `json:"customer_name"`
    CustomerTier string `json:"customer_tier"`
}

func handler(ctx context.Context, events []PipeEvent) ([]EnrichedOrder, error) {
    var results []EnrichedOrder

    for _, evt := range events {
        var order Order
        if err := json.Unmarshal([]byte(evt.Body), &order); err != nil {
            return nil, fmt.Errorf("failed to parse order: %w", err)
        }

        result, err := ddbClient.GetItem(ctx, &dynamodb.GetItemInput{
            TableName: aws.String("pipes-demo-customers"),
            Key: map[string]types.AttributeValue{
                "customer_id": &types.AttributeValueMemberS{Value: order.CustomerID},
            },
        })
        if err != nil {
            return nil, fmt.Errorf("failed to get customer: %w", err)
        }

        enriched := EnrichedOrder{Order: order}
        if result.Item != nil {
            if name, ok := result.Item["name"]; ok {
                enriched.CustomerName = name.(*types.AttributeValueMemberS).Value
            }
            if tier, ok := result.Item["tier"]; ok {
                enriched.CustomerTier = tier.(*types.AttributeValueMemberS).Value
            }
        }

        results = append(results, enriched)
    }

    return results, nil
}

func main() {
    lambda.Start(handler)
}
```

### `lambda/go.mod`

```
module pipes-enrichment

go 1.21

require (
    github.com/aws/aws-lambda-go v1.47.0
    github.com/aws/aws-sdk-go-v2 v1.24.0
    github.com/aws/aws-sdk-go-v2/config v1.26.0
    github.com/aws/aws-sdk-go-v2/service/dynamodb v1.26.0
)
```

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
  default     = "pipes-demo"
}
```

### `events.tf`

```hcl
resource "aws_sqs_queue" "orders" {
  name = "${var.project_name}-orders"
}

resource "aws_pipes_pipe" "order_processing" {
  name     = "${var.project_name}-order-processing"
  role_arn = aws_iam_role.pipe.arn

  source = aws_sqs_queue.orders.arn

  source_parameters {
    sqs_queue_parameters {
      batch_size                         = 1
      maximum_batching_window_in_seconds = 0
    }

    filter_criteria {
      filter {
        pattern = jsonencode({
          body = {
            priority = ["high"]
          }
        })
      }
    }
  }

  enrichment = aws_lambda_function.enrichment.arn

  target = aws_sfn_state_machine.processor.arn

  target_parameters {
    step_function_state_machine_parameters {
      invocation_type = "FIRE_AND_FORGET"
    }
  }
}

resource "aws_sfn_state_machine" "processor" {
  name     = "${var.project_name}-processor"
  role_arn = aws_iam_role.sfn.arn

  definition = jsonencode({
    StartAt = "ProcessOrder"
    States = {
      ProcessOrder = {
        Type = "Pass"
        Result = {
          status = "processed"
        }
        ResultPath = "$.processing_result"
        End        = true
      }
    }
  })
}
```

### `database.tf`

```hcl
resource "aws_dynamodb_table" "customers" {
  name         = "${var.project_name}-customers"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "customer_id"

  attribute {
    name = "customer_id"
    type = "S"
  }
}
```

### `build.tf`

```hcl
resource "null_resource" "go_build" {
  triggers = { source_hash = filebase64sha256("${path.module}/lambda/main.go") }
  provisioner "local-exec" {
    command     = "GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go"
    working_dir = "${path.module}/lambda"
  }
}

data "archive_file" "enrichment_zip" {
  type        = "zip"
  source_file = "${path.module}/lambda/bootstrap"
  output_path = "${path.module}/build/enrichment.zip"
  depends_on  = [null_resource.go_build]
}
```

### `lambda.tf`

```hcl
resource "aws_cloudwatch_log_group" "enrichment" {
  name              = "/aws/lambda/${var.project_name}-enrichment"
  retention_in_days = 1
}

resource "aws_lambda_function" "enrichment" {
  function_name    = "${var.project_name}-enrichment"
  filename         = data.archive_file.enrichment_zip.output_path
  source_code_hash = data.archive_file.enrichment_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.enrichment.arn
  timeout          = 30

  environment {
    variables = {
      TABLE_NAME = aws_dynamodb_table.customers.name
    }
  }

  depends_on = [aws_iam_role_policy_attachment.enrichment_basic, aws_cloudwatch_log_group.enrichment]
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

data "aws_iam_policy_document" "pipes_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["pipes.amazonaws.com"]
    }
  }
}

data "aws_iam_policy_document" "sfn_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["states.amazonaws.com"]
    }
  }
}

# Enrichment Lambda role
resource "aws_iam_role" "enrichment" {
  name               = "${var.project_name}-enrichment-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

resource "aws_iam_role_policy_attachment" "enrichment_basic" {
  role       = aws_iam_role.enrichment.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

data "aws_iam_policy_document" "enrichment_ddb" {
  statement {
    actions   = ["dynamodb:GetItem"]
    resources = [aws_dynamodb_table.customers.arn]
  }
}

resource "aws_iam_role_policy" "enrichment_ddb" {
  name   = "dynamodb-read"
  role   = aws_iam_role.enrichment.id
  policy = data.aws_iam_policy_document.enrichment_ddb.json
}

# Pipe role
resource "aws_iam_role" "pipe" {
  name               = "${var.project_name}-pipe-role"
  assume_role_policy = data.aws_iam_policy_document.pipes_assume.json
}

data "aws_iam_policy_document" "pipe_policy" {
  statement {
    actions = [
      "sqs:ReceiveMessage",
      "sqs:DeleteMessage",
      "sqs:GetQueueAttributes",
    ]
    resources = [aws_sqs_queue.orders.arn]
  }

  statement {
    actions   = ["lambda:InvokeFunction"]
    resources = [aws_lambda_function.enrichment.arn]
  }

  statement {
    actions   = ["states:StartExecution"]
    resources = [aws_sfn_state_machine.processor.arn]
  }
}

resource "aws_iam_role_policy" "pipe" {
  name   = "pipe-permissions"
  role   = aws_iam_role.pipe.id
  policy = data.aws_iam_policy_document.pipe_policy.json
}

# Step Functions role
resource "aws_iam_role" "sfn" {
  name               = "${var.project_name}-sfn-role"
  assume_role_policy = data.aws_iam_policy_document.sfn_assume.json
}
```

### `outputs.tf`

```hcl
output "source_queue_url" {
  value = aws_sqs_queue.orders.url
}

output "state_machine_arn" {
  value = aws_sfn_state_machine.processor.arn
}
```

### Testing

```bash
# Seed a customer
aws dynamodb put-item --table-name pipes-demo-customers \
  --item '{"customer_id":{"S":"c-001"},"name":{"S":"Alice"},"tier":{"S":"premium"}}'

terraform init && terraform apply -auto-approve

# Send high-priority order (passes filter)
aws sqs send-message --queue-url $(terraform output -raw source_queue_url) \
  --message-body '{"order_id":"o-001","customer_id":"c-001","priority":"high","amount":99.99}'

# Send low-priority order (filtered out)
aws sqs send-message --queue-url $(terraform output -raw source_queue_url) \
  --message-body '{"order_id":"o-002","customer_id":"c-002","priority":"low","amount":9.99}'

sleep 15

# Verify only one execution
aws stepfunctions list-executions \
  --state-machine-arn $(terraform output -raw state_machine_arn) \
  --query "executions[*].{Name:name,Status:status}" --output table
```

</details>

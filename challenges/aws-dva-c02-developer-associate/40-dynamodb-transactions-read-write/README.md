# 40. DynamoDB Transactions: Read and Write

<!--
difficulty: intermediate
concepts: [transact-write-items, transact-get-items, atomicity, idempotency-token, conditional-checks, transaction-conflicts, 100-item-limit, 4mb-limit]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: analyze, implement
prerequisites: [03-dynamodb-developer-sdk-operations, 37-dynamodb-single-table-design-patterns]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a DynamoDB table and Lambda functions. Costs are negligible during testing (~$0.01/hr or less). Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Analyze** the ACID guarantees of DynamoDB transactions: atomicity (all-or-nothing), consistency, isolation (serializable), and durability
2. **Implement** a TransactWriteItems operation that atomically transfers a balance between two accounts using Put, Update, and ConditionCheck actions
3. **Configure** an idempotency token (`ClientRequestToken`) to prevent duplicate transaction execution during retries
4. **Differentiate** between the 100-item limit per transaction and the 4 MB total request size limit, and understand which limit applies in different scenarios
5. **Debug** transaction cancellation reasons including `ConditionalCheckFailed`, `TransactionConflict`, and `ValidationException`

## Why This Matters

DynamoDB transactions enable all-or-nothing operations across up to 100 items in a single account and region. Before transactions, developers had to implement complex saga patterns with compensating writes to achieve atomicity. Now, `TransactWriteItems` guarantees that either all operations succeed or none do -- with serializable isolation, meaning no other operation can see partial results.

The DVA-C02 exam tests transactions in several ways. First, the limits: up to 100 items per transaction, 4 MB total request size, and 2x the WCU cost of standard writes (transactions consume double the capacity units). Second, idempotency: the `ClientRequestToken` parameter lets you safely retry a failed transaction without risk of double-execution -- DynamoDB returns the same result for duplicate tokens within a 10-minute window. Third, conflict resolution: two transactions that touch the same item will cause one to fail with `TransactionCanceledException` and reason `TransactionConflict`. Understanding when to retry versus when to fail is critical for production applications.

## Building Blocks

### Lambda Function Code

### `transfer/main.go`

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

var client *dynamodb.Client
var tableName string

func init() {
	tableName = os.Getenv("TABLE_NAME")
	cfg, _ := config.LoadDefaultConfig(context.Background())
	client = dynamodb.NewFromConfig(cfg)
}

type TransferRequest struct {
	FromAccount    string  `json:"from_account"`
	ToAccount      string  `json:"to_account"`
	Amount         float64 `json:"amount"`
	IdempotencyKey string  `json:"idempotency_key"`
}

func handler(ctx context.Context, event json.RawMessage) (map[string]interface{}, error) {
	var req TransferRequest
	if err := json.Unmarshal(event, &req); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	amountStr := fmt.Sprintf("%.2f", req.Amount)

	input := &dynamodb.TransactWriteItemsInput{
		TransactItems: []types.TransactWriteItem{
			// Debit from source account (with balance check)
			{
				Update: &types.Update{
					TableName: aws.String(tableName),
					Key: map[string]types.AttributeValue{
						"PK": &types.AttributeValueMemberS{Value: fmt.Sprintf("ACCOUNT#%s", req.FromAccount)},
						"SK": &types.AttributeValueMemberS{Value: "BALANCE"},
					},
					UpdateExpression:    aws.String("SET balance = balance - :amount"),
					ConditionExpression: aws.String("balance >= :amount"),
					ExpressionAttributeValues: map[string]types.AttributeValue{
						":amount": &types.AttributeValueMemberN{Value: amountStr},
					},
				},
			},
			// Credit to destination account
			{
				Update: &types.Update{
					TableName: aws.String(tableName),
					Key: map[string]types.AttributeValue{
						"PK": &types.AttributeValueMemberS{Value: fmt.Sprintf("ACCOUNT#%s", req.ToAccount)},
						"SK": &types.AttributeValueMemberS{Value: "BALANCE"},
					},
					UpdateExpression: aws.String("SET balance = balance + :amount"),
					ExpressionAttributeValues: map[string]types.AttributeValue{
						":amount": &types.AttributeValueMemberN{Value: amountStr},
					},
				},
			},
			// Write transaction record (audit log)
			{
				Put: &types.Put{
					TableName: aws.String(tableName),
					Item: map[string]types.AttributeValue{
						"PK":           &types.AttributeValueMemberS{Value: fmt.Sprintf("TXN#%s", req.IdempotencyKey)},
						"SK":           &types.AttributeValueMemberS{Value: "RECORD"},
						"from_account": &types.AttributeValueMemberS{Value: req.FromAccount},
						"to_account":   &types.AttributeValueMemberS{Value: req.ToAccount},
						"amount":       &types.AttributeValueMemberN{Value: amountStr},
					},
					ConditionExpression: aws.String("attribute_not_exists(PK)"),
				},
			},
		},
	}

	// Idempotency token: safe to retry within 10 minutes
	if req.IdempotencyKey != "" {
		input.ClientRequestToken = aws.String(req.IdempotencyKey)
	}

	_, err := client.TransactWriteItems(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("TransactWriteItems: %w", err)
	}

	body, _ := json.Marshal(map[string]interface{}{
		"message":         "Transfer complete",
		"from":            req.FromAccount,
		"to":              req.ToAccount,
		"amount":          req.Amount,
		"idempotency_key": req.IdempotencyKey,
	})
	return map[string]interface{}{"statusCode": 200, "body": string(body)}, nil
}

func main() { lambda.Start(handler) }
```

### Terraform Skeleton

Create the following files in your exercise directory. Fill in the `# TODO` blocks.

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
  default     = "ddb-transactions"
}
```

### `database.tf`

```hcl
resource "aws_dynamodb_table" "this" {
  name         = "${var.project_name}-data"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "PK"
  range_key    = "SK"

  attribute { name = "PK"; type = "S" }
  attribute { name = "SK"; type = "S" }
}
```

### `iam.tf`

```hcl
data "aws_iam_policy_document" "assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals { type = "Service"; identifiers = ["lambda.amazonaws.com"] }
  }
}

resource "aws_iam_role" "this" {
  name               = "${var.project_name}-role"
  assume_role_policy = data.aws_iam_policy_document.assume.json
}

resource "aws_iam_role_policy_attachment" "basic" {
  role       = aws_iam_role.this.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

# =======================================================
# TODO 1 -- IAM Policy for Transactions (iam.tf)
# =======================================================
# Requirements:
#   - Grant dynamodb:PutItem, dynamodb:UpdateItem, dynamodb:GetItem,
#     dynamodb:Query for standard operations
#   - ALSO grant dynamodb:TransactWriteItems and dynamodb:TransactGetItems
#     (transaction operations are separate IAM actions)
#   - Scope to the table ARN


# =======================================================
# TODO 2 -- Seed Lambda (create initial account balances) (lambda.tf)
# =======================================================
# Requirements:
#   - Create a seed_data Lambda that writes two account records:
#     PK=ACCOUNT#acct-001, SK=BALANCE, balance=1000.00
#     PK=ACCOUNT#acct-002, SK=BALANCE, balance=500.00
#   - Use standard PutItem (not transactional) for seeding


# =======================================================
# TODO 3 -- Transfer Lambda (as shown above) (lambda.tf)
# =======================================================
# Requirements:
#   - Deploy the transfer/main.go Lambda function
#   - Set TABLE_NAME environment variable


# =======================================================
# TODO 4 -- Read Lambda (TransactGetItems) (lambda.tf)
# =======================================================
# Requirements:
#   - Create a read_balances Lambda that uses TransactGetItems
#     to atomically read both account balances in a single call
#   - TransactGetItems guarantees a consistent snapshot across
#     all items at the same point in time
```

### `outputs.tf`

```hcl
output "table_name" { value = aws_dynamodb_table.this.name }
```

## Spot the Bug

A developer implemented a transaction that transfers funds and writes an audit record. The transaction works for the first transfer but fails on subsequent transfers with `TransactionCanceledException`. The error shows `ConditionalCheckFailed` for the third item. **What is wrong?**

```go
input := &dynamodb.TransactWriteItemsInput{
    TransactItems: []types.TransactWriteItem{
        {Update: &types.Update{/* debit from_account */}},
        {Update: &types.Update{/* credit to_account */}},
        {
            Put: &types.Put{
                TableName: aws.String(tableName),
                Item: map[string]types.AttributeValue{
                    "PK":     &types.AttributeValueMemberS{Value: "TXN#transfer"},  // <-- BUG
                    "SK":     &types.AttributeValueMemberS{Value: "RECORD"},
                    "amount": &types.AttributeValueMemberN{Value: "50.00"},
                },
                ConditionExpression: aws.String("attribute_not_exists(PK)"),
            },
        },
    },
}
```

<details>
<summary>Explain the bug</summary>

The transaction record uses a **static key** `PK=TXN#transfer` for every transfer. The `ConditionExpression: attribute_not_exists(PK)` ensures the item does not already exist. The first transfer creates this item successfully. The second transfer fails because the item already exists -- the condition check fails.

The fix -- use a **unique key** for each transaction, such as the idempotency key or a UUID:

```go
{
    Put: &types.Put{
        TableName: aws.String(tableName),
        Item: map[string]types.AttributeValue{
            "PK":     &types.AttributeValueMemberS{Value: fmt.Sprintf("TXN#%s", req.IdempotencyKey)},
            "SK":     &types.AttributeValueMemberS{Value: "RECORD"},
            "amount": &types.AttributeValueMemberN{Value: amountStr},
        },
        ConditionExpression: aws.String("attribute_not_exists(PK)"),
    },
}
```

This pattern gives each transaction its own audit record. The `attribute_not_exists(PK)` condition now serves as an idempotency guard: if you retry the same transaction with the same key, the condition fails (preventing duplicate execution), which is the correct behavior.

Additionally, the total request size limit of 4 MB applies to the entire `TransactWriteItems` call, including all items combined. With large items, you may hit this limit before reaching the 100-item limit.

</details>

## Solutions

<details>
<summary>TODO 1 -- IAM Policy for Transactions</summary>

### `iam.tf`

```hcl
data "aws_iam_policy_document" "ddb" {
  statement {
    actions = [
      "dynamodb:PutItem",
      "dynamodb:UpdateItem",
      "dynamodb:GetItem",
      "dynamodb:Query",
      "dynamodb:TransactWriteItems",
      "dynamodb:TransactGetItems",
    ]
    resources = [aws_dynamodb_table.this.arn]
  }
}

resource "aws_iam_policy" "ddb" {
  name   = "${var.project_name}-ddb-access"
  policy = data.aws_iam_policy_document.ddb.json
}

resource "aws_iam_role_policy_attachment" "ddb" {
  role       = aws_iam_role.this.name
  policy_arn = aws_iam_policy.ddb.arn
}
```

</details>

<details>
<summary>TODO 2 + TODO 3 + TODO 4 -- Lambda Functions</summary>

### `lambda.tf`

```hcl
locals {
  functions = toset(["seed_data", "transfer", "read_balances"])
}

resource "null_resource" "go_build" {
  for_each = local.functions
  triggers = { source_hash = filebase64sha256("${path.module}/${each.key}/main.go") }
  provisioner "local-exec" {
    command     = "GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go"
    working_dir = "${path.module}/${each.key}"
  }
}

data "archive_file" "fn" {
  for_each    = local.functions
  type        = "zip"
  source_file = "${path.module}/${each.key}/bootstrap"
  output_path = "${path.module}/build/${each.key}.zip"
  depends_on  = [null_resource.go_build]
}

resource "aws_cloudwatch_log_group" "fn" {
  for_each          = local.functions
  name              = "/aws/lambda/${var.project_name}-${each.key}"
  retention_in_days = 1
}

resource "aws_lambda_function" "fn" {
  for_each         = local.functions
  function_name    = "${var.project_name}-${each.key}"
  filename         = data.archive_file.fn[each.key].output_path
  source_code_hash = data.archive_file.fn[each.key].output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.this.arn
  timeout          = 15
  environment { variables = { TABLE_NAME = aws_dynamodb_table.this.name } }
  depends_on = [aws_iam_role_policy_attachment.basic, aws_iam_role_policy_attachment.ddb, aws_cloudwatch_log_group.fn]
}
```

</details>

## Verify What You Learned

```bash
# Seed initial balances
aws lambda invoke --function-name ddb-transactions-seed_data \
  --payload '{}' /dev/stdout 2>/dev/null | jq -r '.body' | jq .

# Transfer $200 from acct-001 to acct-002
aws lambda invoke --function-name ddb-transactions-transfer \
  --payload '{"from_account":"acct-001","to_account":"acct-002","amount":200,"idempotency_key":"txn-001"}' \
  /dev/stdout 2>/dev/null | jq -r '.body' | jq .

# Verify balances: acct-001=800, acct-002=700
aws dynamodb get-item --table-name ddb-transactions-data \
  --key '{"PK":{"S":"ACCOUNT#acct-001"},"SK":{"S":"BALANCE"}}' \
  --query "Item.balance.N" --output text

aws dynamodb get-item --table-name ddb-transactions-data \
  --key '{"PK":{"S":"ACCOUNT#acct-002"},"SK":{"S":"BALANCE"}}' \
  --query "Item.balance.N" --output text

# Retry the same transfer (idempotent -- should succeed without double-debit)
aws lambda invoke --function-name ddb-transactions-transfer \
  --payload '{"from_account":"acct-001","to_account":"acct-002","amount":200,"idempotency_key":"txn-001"}' \
  /dev/stdout 2>/dev/null | jq -r '.body' | jq .

terraform plan
```

Expected: acct-001 balance is `800.00`, acct-002 balance is `700.00`. Retry with same idempotency key does not change balances.

## Cleanup

```bash
terraform destroy -auto-approve
terraform state list
```

Expected: no output (empty state).

## What's Next

You implemented atomic multi-item transactions with idempotency. In the next exercise, you will explore **DynamoDB batch operations** -- using BatchWriteItem and BatchGetItem for high-throughput bulk operations with retry handling.

## Summary

- **TransactWriteItems** performs up to 100 Put, Update, Delete, or ConditionCheck actions atomically (all-or-nothing)
- **TransactGetItems** reads up to 100 items as a consistent snapshot at a single point in time
- Transactions consume **2x WCU/RCU** compared to standard operations
- **ClientRequestToken** provides idempotency: retrying with the same token within 10 minutes returns the original result without re-executing
- Transactions fail with `TransactionCanceledException` if any condition check fails; the `CancellationReasons` array tells you which item and why
- **Limits**: 100 items per transaction, 4 MB total request size, all items must be in the same AWS account and region
- Two concurrent transactions on the same item cause `TransactionConflict` -- one succeeds, the other must retry

## Reference

- [DynamoDB Transactions](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/transaction-apis.html)
- [TransactWriteItems API](https://docs.aws.amazon.com/amazondynamodb/latest/APIReference/API_TransactWriteItems.html)
- [TransactGetItems API](https://docs.aws.amazon.com/amazondynamodb/latest/APIReference/API_TransactGetItems.html)
- [Terraform aws_dynamodb_table](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/dynamodb_table)

## Additional Resources

- [Transaction Conflict Handling](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/transaction-apis.html#transaction-conflict-handling) -- how DynamoDB resolves conflicts between concurrent transactions
- [Idempotent Transactions](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/transaction-apis.html#transaction-apis-txwriteitems) -- using ClientRequestToken for safe retries
- [Transaction Best Practices](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/bp-transactions.html) -- minimizing transaction conflicts and cost
- [DynamoDB Pricing for Transactions](https://aws.amazon.com/dynamodb/pricing/) -- 2x WCU/RCU cost details for transactional operations

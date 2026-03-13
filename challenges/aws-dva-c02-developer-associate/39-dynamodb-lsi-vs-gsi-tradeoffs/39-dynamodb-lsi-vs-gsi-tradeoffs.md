# 39. DynamoDB LSI vs GSI Trade-offs

<!--
difficulty: intermediate
concepts: [local-secondary-index, global-secondary-index, lsi-vs-gsi, lsi-creation-constraint, consistent-reads-lsi, eventual-consistency-gsi, 10gb-partition-limit, shared-throughput]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: analyze, implement
prerequisites: [38-dynamodb-global-secondary-index-design]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a DynamoDB table with both LSI and GSI in on-demand mode, plus Lambda functions. Costs are negligible during testing (~$0.01/hr or less). Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Analyze** the fundamental constraint of Local Secondary Indexes: they must be defined at table creation time and cannot be added or removed afterward
2. **Implement** a DynamoDB table with both LSI (same partition key, alternate sort key) and GSI (different partition key) in Terraform
3. **Differentiate** between LSI and GSI across five dimensions: creation timing, partition key flexibility, consistency model, throughput, and item collection size limit
4. **Configure** a GSI added after initial table creation to support a new access pattern without downtime
5. **Evaluate** the 10 GB item collection limit imposed by LSIs and its implications for high-cardinality partition keys

## Why This Matters

The DVA-C02 exam frequently presents scenarios where you must choose between an LSI and a GSI. The decision hinges on five factors:

| Factor | LSI | GSI |
|--------|-----|-----|
| **Partition Key** | Same as table | Any attribute |
| **Creation** | Table creation only | Anytime |
| **Consistency** | Eventual + Strong | Eventual only |
| **Throughput** | Shares table capacity | Own capacity |
| **Size Limit** | 10 GB per partition | No limit |

A common exam trap: a developer wants to add a "query by creation_date" access pattern to an existing table. If the question says "same partition key, different sort key," the answer is LSI -- but only if the table was just created. If the table already exists with data, the answer is GSI, because LSIs cannot be added after table creation.

Another trap: a question asks about strongly consistent reads on an index. Only LSIs support `ConsistentRead=true`. If the scenario requires strong consistency on the index query, GSI is not an option.

## Building Blocks

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
  default     = "lsi-vs-gsi"
}
```

### `database.tf`

```hcl
# =======================================================
# TODO 1 -- DynamoDB Table with LSI (must be at creation) (database.tf)
# =======================================================
# Requirements:
#   - Create a table with hash_key = "user_id" and range_key = "order_id"
#   - Add a local_secondary_index named "date-index":
#     - range_key = "order_date" (same partition key user_id, different sort key)
#     - projection_type = "ALL"
#   - Add attributes: user_id (S), order_id (S), order_date (S), status (S)
#   - Use billing_mode = "PAY_PER_REQUEST"
#
# IMPORTANT: The LSI must be defined HERE at table creation.
#            It CANNOT be added later via a separate terraform apply.
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/dynamodb_table#local_secondary_index


# =======================================================
# TODO 2 -- Add a GSI (can be added anytime) (database.tf)
# =======================================================
# Requirements:
#   - Add a global_secondary_index named "status-index" to the SAME table:
#     - hash_key = "status"
#     - range_key = "order_date"
#     - projection_type = "INCLUDE"
#     - non_key_attributes = ["user_id", "total"]
#   - Unlike the LSI, this GSI could be added after table creation
#     (Terraform handles this via an update, not recreation)
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

data "aws_iam_policy_document" "ddb" {
  statement {
    actions   = ["dynamodb:PutItem", "dynamodb:Query", "dynamodb:GetItem"]
    resources = [
      aws_dynamodb_table.this.arn,
      "${aws_dynamodb_table.this.arn}/index/*",
    ]
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

### `lambda.tf`

```hcl
locals {
  functions = toset(["seed_data", "query_lsi", "query_gsi"])
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

### `outputs.tf`

```hcl
output "table_name" { value = aws_dynamodb_table.this.name }
output "function_names" { value = { for k, v in aws_lambda_function.fn : k => v.function_name } }
```

### Lambda Functions

### `seed_data/main.go`

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

func handler(ctx context.Context, event json.RawMessage) (map[string]interface{}, error) {
	items := []map[string]types.AttributeValue{
		{"user_id": &types.AttributeValueMemberS{Value: "u001"}, "order_id": &types.AttributeValueMemberS{Value: "ord-001"}, "order_date": &types.AttributeValueMemberS{Value: "2024-01-15"}, "status": &types.AttributeValueMemberS{Value: "shipped"}, "total": &types.AttributeValueMemberN{Value: "129.99"}},
		{"user_id": &types.AttributeValueMemberS{Value: "u001"}, "order_id": &types.AttributeValueMemberS{Value: "ord-002"}, "order_date": &types.AttributeValueMemberS{Value: "2024-03-01"}, "status": &types.AttributeValueMemberS{Value: "pending"}, "total": &types.AttributeValueMemberN{Value: "49.50"}},
		{"user_id": &types.AttributeValueMemberS{Value: "u001"}, "order_id": &types.AttributeValueMemberS{Value: "ord-003"}, "order_date": &types.AttributeValueMemberS{Value: "2024-02-10"}, "status": &types.AttributeValueMemberS{Value: "shipped"}, "total": &types.AttributeValueMemberN{Value: "200.00"}},
		{"user_id": &types.AttributeValueMemberS{Value: "u002"}, "order_id": &types.AttributeValueMemberS{Value: "ord-004"}, "order_date": &types.AttributeValueMemberS{Value: "2024-01-20"}, "status": &types.AttributeValueMemberS{Value: "pending"}, "total": &types.AttributeValueMemberN{Value: "75.00"}},
	}
	for _, item := range items {
		client.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String(tableName), Item: item})
	}
	body, _ := json.Marshal(map[string]interface{}{"message": "Seeded", "count": len(items)})
	return map[string]interface{}{"statusCode": 200, "body": string(body)}, nil
}

func main() { lambda.Start(handler) }
```

### `query_lsi/main.go`

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
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
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

type Request struct {
	UserID         string `json:"user_id"`
	DateAfter      string `json:"date_after"`
	ConsistentRead bool   `json:"consistent_read"`
}

func handler(ctx context.Context, event json.RawMessage) (map[string]interface{}, error) {
	var req Request
	json.Unmarshal(event, &req)

	input := &dynamodb.QueryInput{
		TableName:              aws.String(tableName),
		IndexName:              aws.String("date-index"),
		KeyConditionExpression: aws.String("user_id = :uid AND order_date > :date"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":uid":  &types.AttributeValueMemberS{Value: req.UserID},
			":date": &types.AttributeValueMemberS{Value: req.DateAfter},
		},
		ConsistentRead: aws.Bool(req.ConsistentRead),
	}

	result, err := client.Query(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("Query LSI: %w", err)
	}

	items := make([]map[string]interface{}, 0)
	for _, item := range result.Items {
		var raw map[string]interface{}
		attributevalue.UnmarshalMap(item, &raw)
		items = append(items, raw)
	}

	body, _ := json.Marshal(map[string]interface{}{
		"query": "LSI date-index", "consistent_read": req.ConsistentRead,
		"count": result.Count, "items": items,
		"note": "LSI supports ConsistentRead=true, unlike GSI",
	})
	return map[string]interface{}{"statusCode": 200, "body": string(body)}, nil
}

func main() { lambda.Start(handler) }
```

### `query_gsi/main.go`

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
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
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

type Request struct {
	Status string `json:"status"`
}

func handler(ctx context.Context, event json.RawMessage) (map[string]interface{}, error) {
	var req Request
	json.Unmarshal(event, &req)

	result, err := client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(tableName),
		IndexName:              aws.String("status-index"),
		KeyConditionExpression: aws.String("#s = :status"),
		ExpressionAttributeNames: map[string]string{"#s": "status"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":status": &types.AttributeValueMemberS{Value: req.Status},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("Query GSI: %w", err)
	}

	items := make([]map[string]interface{}, 0)
	for _, item := range result.Items {
		var raw map[string]interface{}
		attributevalue.UnmarshalMap(item, &raw)
		items = append(items, raw)
	}

	body, _ := json.Marshal(map[string]interface{}{
		"query": "GSI status-index", "count": result.Count, "items": items,
		"note": "GSI only supports eventually consistent reads",
	})
	return map[string]interface{}{"statusCode": 200, "body": string(body)}, nil
}

func main() { lambda.Start(handler) }
```

## Spot the Bug

A developer has an existing DynamoDB table with data. They want to add a new access pattern: "get orders for a user sorted by date." They add a Local Secondary Index to the Terraform configuration. **What is wrong?**

```hcl
resource "aws_dynamodb_table" "orders" {
  name         = "orders"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "user_id"
  range_key    = "order_id"

  attribute { name = "user_id";    type = "S" }
  attribute { name = "order_id";   type = "S" }
  attribute { name = "order_date"; type = "S" }

  # Added AFTER the table already exists with data
  local_secondary_index {    # <-- BUG
    name            = "date-index"
    range_key       = "order_date"
    projection_type = "ALL"
  }
}
```

<details>
<summary>Explain the bug</summary>

**Local Secondary Indexes can only be defined at table creation time.** If the table already exists, adding an LSI in Terraform forces table recreation -- Terraform will destroy the existing table (and all its data) and create a new one with the LSI. This causes data loss.

The error from DynamoDB (if you tried via API): `ValidationException: One or more parameter values were invalid: Table already exists: orders`

The fix depends on the situation:

**Option A -- use a GSI instead (no data loss, added online):**

```hcl
global_secondary_index {
  name            = "date-index"
  hash_key        = "user_id"
  range_key       = "order_date"
  projection_type = "ALL"
}
```

GSIs can be added to existing tables without downtime. The trade-off is that GSI queries are eventually consistent.

**Option B -- create a new table with LSI and migrate data:**

If you specifically need strongly consistent reads on the date-sorted index, create a new table with the LSI and migrate data from the old table.

This is a critical DVA-C02 exam distinction: LSI = table creation only, GSI = anytime.

</details>

## Solutions

<details>
<summary>TODO 1 + TODO 2 -- Table with LSI and GSI</summary>

### `database.tf`

```hcl
resource "aws_dynamodb_table" "this" {
  name         = "${var.project_name}-orders"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "user_id"
  range_key    = "order_id"

  attribute { name = "user_id";    type = "S" }
  attribute { name = "order_id";   type = "S" }
  attribute { name = "order_date"; type = "S" }
  attribute { name = "status";     type = "S" }

  # LSI: same partition key (user_id), different sort key (order_date)
  # Must be defined here at table creation -- cannot be added later
  local_secondary_index {
    name            = "date-index"
    range_key       = "order_date"
    projection_type = "ALL"
  }

  # GSI: different partition key (status), can be added anytime
  global_secondary_index {
    name               = "status-index"
    hash_key           = "status"
    range_key          = "order_date"
    projection_type    = "INCLUDE"
    non_key_attributes = ["user_id", "total"]
  }
}
```

</details>

## Verify What You Learned

```bash
# Verify LSI exists
aws dynamodb describe-table --table-name lsi-vs-gsi-orders \
  --query "Table.LocalSecondaryIndexes[*].{Name:IndexName,SK:KeySchema[1].AttributeName}" \
  --output table

# Verify GSI exists
aws dynamodb describe-table --table-name lsi-vs-gsi-orders \
  --query "Table.GlobalSecondaryIndexes[*].{Name:IndexName,PK:KeySchema[0].AttributeName,Projection:Projection.ProjectionType}" \
  --output table

# Test LSI with consistent read (should work)
aws lambda invoke --function-name lsi-vs-gsi-query_lsi \
  --payload '{"user_id":"u001","date_after":"2024-01-01","consistent_read":true}' \
  /dev/stdout 2>/dev/null | jq -r '.body' | jq .

terraform plan
```

Expected: LSI `date-index` with sort key `order_date`, GSI `status-index` with hash key `status`, consistent read succeeds on LSI.

## Cleanup

```bash
terraform destroy -auto-approve
terraform state list
```

Expected: no output (empty state).

## What's Next

You compared LSI and GSI across creation timing, consistency, and capacity dimensions. In the next exercise, you will implement **DynamoDB transactions** -- using TransactWriteItems and TransactGetItems for atomic multi-item operations.

## Summary

- **LSI** shares the table's partition key but uses a different sort key; **GSI** can have any attribute as partition and sort key
- LSIs must be defined **at table creation** -- they cannot be added, modified, or removed after the table exists
- GSIs can be **added or removed at any time** without table recreation
- LSIs support **strongly consistent reads** (`ConsistentRead=true`); GSIs support **only eventually consistent** reads
- LSIs share the **base table's throughput**; GSIs have their **own read/write capacity**
- LSIs impose a **10 GB item collection limit** per partition key value -- exceeding this returns `ItemCollectionSizeLimitExceededException`
- Maximum **5 LSIs** and **20 GSIs** per table

## Reference

- [DynamoDB Local Secondary Indexes](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/LSI.html)
- [DynamoDB Global Secondary Indexes](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/GSI.html)
- [Terraform aws_dynamodb_table LSI](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/dynamodb_table#local_secondary_index)
- [Choosing Between GSI and LSI](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/bp-indexes-general.html)

## Additional Resources

- [LSI Item Collection Size Limit](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/LSI.html#LSI.ItemCollections.SizeLimit) -- understanding and monitoring the 10 GB per-partition limit
- [GSI Write Throttling](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/GSI.html#GSI.ThroughputConsiderations) -- how GSI back-pressure affects base table writes
- [Index Design Best Practices](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/bp-indexes.html) -- when to use sparse indexes, overloaded indexes, and composite keys
- [DynamoDB Limits](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/ServiceQuotas.html) -- table limits for indexes, item sizes, and throughput

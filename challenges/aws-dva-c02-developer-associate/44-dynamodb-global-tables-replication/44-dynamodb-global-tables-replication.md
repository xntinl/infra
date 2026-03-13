# 44. DynamoDB Global Tables and Replication

<!--
difficulty: advanced
concepts: [global-tables, multi-region-replication, last-writer-wins, conflict-resolution, replication-latency, replica-configuration, on-demand-global-table, cross-region-consistency]
tools: [terraform, aws-cli]
estimated_time: 55m
bloom_level: evaluate, create
prerequisites: [03-dynamodb-developer-sdk-operations, 37-dynamodb-single-table-design-patterns]
aws_cost: ~$0.05/hr
-->

> **AWS Cost Warning:** This exercise creates DynamoDB tables in two AWS regions. Global tables incur costs in both regions for storage and replicated write capacity. Total cost is approximately $0.05/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally
- Access to at least two AWS regions (us-east-1 and us-west-2 used in this exercise)

## Learning Objectives

After completing this exercise, you will be able to:

- **Evaluate** when global tables are appropriate: active-active multi-region applications, disaster recovery, and low-latency reads for geographically distributed users
- **Design** a multi-region DynamoDB architecture considering conflict resolution (last writer wins), replication latency (typically under 1 second), and cost implications
- **Implement** a global table with replicas in two regions using Terraform's `replica` block
- **Analyze** how conflict resolution works: when the same item is written simultaneously in two regions, the write with the latest timestamp wins (last writer wins)
- **Configure** independent auto scaling, streams, and TTL per replica region

## Why This Matters

DynamoDB global tables enable active-active multi-region architectures where any replica can accept reads and writes. When a user in Europe writes to the eu-west-1 replica, the change is automatically replicated to all other replicas (typically within 1 second). A user in the US reading from us-east-1 sees the change shortly after. This eliminates the need for application-level replication and provides built-in disaster recovery.

The DVA-C02 exam tests global tables in several ways. First, **conflict resolution**: global tables use "last writer wins" based on the item's timestamp, which is a non-configurable behavior. If two users update the same item in different regions at the same time, the write with the latest timestamp prevails. Second, **prerequisites**: all replicas must use on-demand billing or have auto scaling configured, DynamoDB Streams must be enabled on the base table, and the table must have the same name in all regions. Third, **replication scope**: global tables replicate the entire table, not individual items or partitions -- you cannot selectively replicate. Fourth, **cost**: replicated writes consume replicated WCUs (rWCUs) in each replica region, which are more expensive than standard WCUs.

## The Challenge

Build a DynamoDB global table with replicas in two regions. Write data in one region and verify it appears in the other region. Observe conflict resolution behavior with concurrent writes.

### Requirements

| Requirement | Description |
|---|---|
| Base Table | DynamoDB table in us-east-1 with on-demand billing |
| Replica | Replica in us-west-2 |
| Streams | Enabled on base table (required for global tables) |
| Lambda (Region 1) | Go Lambda in us-east-1 that writes and reads items |
| Lambda (Region 2) | Go Lambda in us-west-2 that reads items (verify replication) |
| Conflict Test | Write the same item in both regions, verify last-writer-wins |

### Architecture

```
  us-east-1                              us-west-2
  +-----------------------+              +-----------------------+
  |  DynamoDB Table       |              |  DynamoDB Replica     |
  |  global-demo-data     |  <-------->  |  global-demo-data     |
  |                       |  replication |                       |
  |  Write Lambda ------> |              | <------ Read Lambda   |
  +-----------------------+              +-----------------------+

  Replication:
  - Automatic, bi-directional
  - Typically < 1 second latency
  - Last writer wins for conflicts
  - Replicated WCU (rWCU) cost in each region
```

## Hints

<details>
<summary>Hint 1: Creating a global table with Terraform</summary>

In Terraform, you create a global table by adding `replica` blocks to the `aws_dynamodb_table` resource. The base table must use on-demand billing or have auto scaling configured.

```hcl
resource "aws_dynamodb_table" "this" {
  name         = "global-demo-data"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "PK"
  range_key    = "SK"

  attribute { name = "PK"; type = "S" }
  attribute { name = "SK"; type = "S" }

  # Streams are required for global tables
  stream_enabled   = true
  stream_view_type = "NEW_AND_OLD_IMAGES"

  # Add replica in us-west-2
  replica {
    region_name = "us-west-2"
  }
}
```

Important notes:
- The `replica` block creates the table in the specified region automatically
- The table name must be the same in all regions
- DynamoDB Streams must be enabled on the base table
- Billing mode must be `PAY_PER_REQUEST` or provisioned with auto scaling

</details>

<details>
<summary>Hint 2: Multi-region provider configuration</summary>

To deploy Lambda functions in both regions, configure provider aliases:

```hcl
provider "aws" {
  region = "us-east-1"
  alias  = "east"
}

provider "aws" {
  region = "us-west-2"
  alias  = "west"
}

resource "aws_lambda_function" "writer" {
  provider = aws.east
  # ... Lambda in us-east-1
}

resource "aws_lambda_function" "reader" {
  provider = aws.west
  # ... Lambda in us-west-2
}
```

Each Lambda function uses its local DynamoDB endpoint, which automatically routes to the regional replica.

</details>

<details>
<summary>Hint 3: Verifying replication with AWS CLI</summary>

Write in one region, read in another:

```bash
# Write in us-east-1
aws dynamodb put-item --table-name global-demo-data \
  --item '{"PK":{"S":"USER#001"},"SK":{"S":"PROFILE"},"name":{"S":"Alice"}}' \
  --region us-east-1

# Wait for replication (typically < 1 second)
sleep 2

# Read in us-west-2
aws dynamodb get-item --table-name global-demo-data \
  --key '{"PK":{"S":"USER#001"},"SK":{"S":"PROFILE"}}' \
  --region us-west-2
```

</details>

<details>
<summary>Hint 4: Conflict resolution (last writer wins)</summary>

DynamoDB global tables use **last writer wins** conflict resolution based on timestamps. When the same item is updated in two regions simultaneously, the write with the later timestamp prevails in all replicas.

```bash
# Simulate conflict: write the same item in both regions "simultaneously"
aws dynamodb put-item --table-name global-demo-data \
  --item '{"PK":{"S":"CONFLICT"},"SK":{"S":"TEST"},"value":{"S":"east-wins"}}' \
  --region us-east-1 &

aws dynamodb put-item --table-name global-demo-data \
  --item '{"PK":{"S":"CONFLICT"},"SK":{"S":"TEST"},"value":{"S":"west-wins"}}' \
  --region us-west-2 &

wait
sleep 5

# Check which value won in both regions
aws dynamodb get-item --table-name global-demo-data \
  --key '{"PK":{"S":"CONFLICT"},"SK":{"S":"TEST"}}' \
  --region us-east-1 --query "Item.value.S" --output text

aws dynamodb get-item --table-name global-demo-data \
  --key '{"PK":{"S":"CONFLICT"},"SK":{"S":"TEST"}}' \
  --region us-west-2 --query "Item.value.S" --output text
```

Both regions will show the same value -- whichever write had the later timestamp.

</details>

<details>
<summary>Hint 5: IAM and permissions for global tables</summary>

The IAM role for Lambda in each region only needs standard DynamoDB permissions for the local table. Replication is handled by DynamoDB's internal service, not by your application. However, the IAM role used by Terraform (or the user running `terraform apply`) needs permissions to create tables in all replica regions.

```hcl
data "aws_iam_policy_document" "ddb" {
  statement {
    actions = [
      "dynamodb:PutItem",
      "dynamodb:GetItem",
      "dynamodb:Query",
      "dynamodb:UpdateItem",
    ]
    resources = [aws_dynamodb_table.this.arn]
  }
}
```

The replication permissions (`dynamodb:CreateTableReplica`, `dynamodb:DescribeTable`) are only needed during table creation, not at runtime.

</details>

## Spot the Bug

A developer created a global table with a replica, but Terraform fails with an error about missing stream configuration. **What is wrong?**

```hcl
resource "aws_dynamodb_table" "this" {
  name         = "global-orders"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "order_id"

  attribute { name = "order_id"; type = "S" }

  # No stream_enabled or stream_view_type   <-- BUG

  replica {
    region_name = "us-west-2"
  }
}
```

<details>
<summary>Explain the bug</summary>

Global tables require DynamoDB Streams to be enabled on the base table. Streams are the mechanism that DynamoDB uses internally to replicate changes between regions. Without streams, replication cannot function.

The fix:

```hcl
resource "aws_dynamodb_table" "this" {
  name         = "global-orders"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "order_id"

  attribute { name = "order_id"; type = "S" }

  stream_enabled   = true
  stream_view_type = "NEW_AND_OLD_IMAGES"

  replica {
    region_name = "us-west-2"
  }
}
```

Additional common mistakes with global tables:
- Using `PROVISIONED` billing without auto scaling on replicas
- Trying to replicate to a region where a table with the same name already exists
- Forgetting that deleting the base table does not automatically delete replicas (in some configurations)
- Not accounting for replicated WCU costs in the budget

</details>

<details>
<summary>Full Solution</summary>

### File Structure

```
44-dynamodb-global-tables-replication/
├── main.tf
├── writer_lambda/
│   └── main.go
└── reader_lambda/
    └── main.go
```

### `writer_lambda/main.go`

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

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

type WriteRequest struct {
	UserID string `json:"user_id"`
	Name   string `json:"name"`
	Email  string `json:"email"`
}

func handler(ctx context.Context, event json.RawMessage) (map[string]interface{}, error) {
	var req WriteRequest
	json.Unmarshal(event, &req)

	region := os.Getenv("AWS_REGION")
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(tableName),
		Item: map[string]types.AttributeValue{
			"PK":         &types.AttributeValueMemberS{Value: fmt.Sprintf("USER#%s", req.UserID)},
			"SK":         &types.AttributeValueMemberS{Value: "PROFILE"},
			"name":       &types.AttributeValueMemberS{Value: req.Name},
			"email":      &types.AttributeValueMemberS{Value: req.Email},
			"written_by": &types.AttributeValueMemberS{Value: region},
			"written_at": &types.AttributeValueMemberS{Value: now},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("PutItem: %w", err)
	}

	body, _ := json.Marshal(map[string]interface{}{
		"message":    "Item written",
		"region":     region,
		"written_at": now,
		"user_id":    req.UserID,
	})
	return map[string]interface{}{"statusCode": 200, "body": string(body)}, nil
}

func main() { lambda.Start(handler) }
```

### `reader_lambda/main.go`

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

type ReadRequest struct {
	UserID string `json:"user_id"`
}

func handler(ctx context.Context, event json.RawMessage) (map[string]interface{}, error) {
	var req ReadRequest
	json.Unmarshal(event, &req)

	region := os.Getenv("AWS_REGION")

	result, err := client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(tableName),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: fmt.Sprintf("USER#%s", req.UserID)},
			"SK": &types.AttributeValueMemberS{Value: "PROFILE"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("GetItem: %w", err)
	}

	var item map[string]interface{}
	if result.Item != nil {
		attributevalue.UnmarshalMap(result.Item, &item)
	}

	body, _ := json.Marshal(map[string]interface{}{
		"message":     "Item read",
		"read_region": region,
		"item":        item,
	})
	return map[string]interface{}{"statusCode": 200, "body": string(body)}, nil
}

func main() { lambda.Start(handler) }
```

### `providers.tf`

```hcl
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    archive = { source = "hashicorp/archive", version = "~> 2.0" }
    null    = { source = "hashicorp/null", version = "~> 3.0" }
  }
}

# Default provider for the global table
provider "aws" {
  region = var.region
}

provider "aws" {
  region = var.region
  alias  = "east"
}

provider "aws" {
  region = var.replica_region
  alias  = "west"
}
```

### `variables.tf`

```hcl
variable "region" {
  description = "Primary AWS region"
  type        = string
  default     = "us-east-1"
}

variable "replica_region" {
  description = "Replica AWS region"
  type        = string
  default     = "us-west-2"
}

variable "project_name" {
  description = "Project name for resource naming"
  type        = string
  default     = "global-demo"
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

  stream_enabled   = true
  stream_view_type = "NEW_AND_OLD_IMAGES"

  replica {
    region_name = var.replica_region
  }
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

# -- IAM (us-east-1) --
resource "aws_iam_role" "east" {
  provider           = aws.east
  name               = "${var.project_name}-east-role"
  assume_role_policy = data.aws_iam_policy_document.assume.json
}

resource "aws_iam_role_policy_attachment" "east_basic" {
  provider   = aws.east
  role       = aws_iam_role.east.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

data "aws_iam_policy_document" "ddb_east" {
  statement {
    actions   = ["dynamodb:PutItem", "dynamodb:GetItem", "dynamodb:Query", "dynamodb:UpdateItem"]
    resources = [aws_dynamodb_table.this.arn]
  }
}

resource "aws_iam_policy" "ddb_east" {
  provider = aws.east
  name     = "${var.project_name}-ddb-east"
  policy   = data.aws_iam_policy_document.ddb_east.json
}

resource "aws_iam_role_policy_attachment" "ddb_east" {
  provider   = aws.east
  role       = aws_iam_role.east.name
  policy_arn = aws_iam_policy.ddb_east.arn
}

# -- IAM (us-west-2) --
resource "aws_iam_role" "west" {
  provider           = aws.west
  name               = "${var.project_name}-west-role"
  assume_role_policy = data.aws_iam_policy_document.assume.json
}

resource "aws_iam_role_policy_attachment" "west_basic" {
  provider   = aws.west
  role       = aws_iam_role.west.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

data "aws_iam_policy_document" "ddb_west" {
  statement {
    actions   = ["dynamodb:PutItem", "dynamodb:GetItem", "dynamodb:Query"]
    resources = ["arn:aws:dynamodb:${var.replica_region}:*:table/${var.project_name}-data"]
  }
}

resource "aws_iam_policy" "ddb_west" {
  provider = aws.west
  name     = "${var.project_name}-ddb-west"
  policy   = data.aws_iam_policy_document.ddb_west.json
}

resource "aws_iam_role_policy_attachment" "ddb_west" {
  provider   = aws.west
  role       = aws_iam_role.west.name
  policy_arn = aws_iam_policy.ddb_west.arn
}
```

### `build.tf`

```hcl
resource "null_resource" "build_writer" {
  triggers = { source_hash = filebase64sha256("${path.module}/writer_lambda/main.go") }
  provisioner "local-exec" {
    command     = "GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go"
    working_dir = "${path.module}/writer_lambda"
  }
}

data "archive_file" "writer" {
  type        = "zip"
  source_file = "${path.module}/writer_lambda/bootstrap"
  output_path = "${path.module}/build/writer.zip"
  depends_on  = [null_resource.build_writer]
}

resource "null_resource" "build_reader" {
  triggers = { source_hash = filebase64sha256("${path.module}/reader_lambda/main.go") }
  provisioner "local-exec" {
    command     = "GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go"
    working_dir = "${path.module}/reader_lambda"
  }
}

data "archive_file" "reader" {
  type        = "zip"
  source_file = "${path.module}/reader_lambda/bootstrap"
  output_path = "${path.module}/build/reader.zip"
  depends_on  = [null_resource.build_reader]
}
```

### `lambda.tf`

```hcl
# -- Writer Lambda (us-east-1) --
resource "aws_lambda_function" "writer" {
  provider         = aws.east
  function_name    = "${var.project_name}-writer"
  filename         = data.archive_file.writer.output_path
  source_code_hash = data.archive_file.writer.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.east.arn
  timeout          = 15
  environment { variables = { TABLE_NAME = aws_dynamodb_table.this.name } }
  depends_on = [aws_iam_role_policy_attachment.east_basic, aws_iam_role_policy_attachment.ddb_east, aws_cloudwatch_log_group.writer]
}

# -- Reader Lambda (us-west-2) --
resource "aws_lambda_function" "reader" {
  provider         = aws.west
  function_name    = "${var.project_name}-reader"
  filename         = data.archive_file.reader.output_path
  source_code_hash = data.archive_file.reader.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.west.arn
  timeout          = 15
  environment { variables = { TABLE_NAME = aws_dynamodb_table.this.name } }
  depends_on = [aws_iam_role_policy_attachment.west_basic, aws_iam_role_policy_attachment.ddb_west, aws_cloudwatch_log_group.reader]
}
```

### `monitoring.tf`

```hcl
resource "aws_cloudwatch_log_group" "writer" {
  provider          = aws.east
  name              = "/aws/lambda/${var.project_name}-writer"
  retention_in_days = 1
}

resource "aws_cloudwatch_log_group" "reader" {
  provider          = aws.west
  name              = "/aws/lambda/${var.project_name}-reader"
  retention_in_days = 1
}
```

### `outputs.tf`

```hcl
output "table_name" { value = aws_dynamodb_table.this.name }
output "writer_function" { value = aws_lambda_function.writer.function_name }
output "reader_function" { value = aws_lambda_function.reader.function_name }
```

### Testing

```bash
# Build
cd writer_lambda && GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go && cd ..
cd reader_lambda && GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go && cd ..

# Deploy
terraform init && terraform apply -auto-approve

# Write in us-east-1
aws lambda invoke --function-name global-demo-writer \
  --payload '{"user_id":"u001","name":"Alice","email":"alice@example.com"}' \
  --region us-east-1 /dev/stdout 2>/dev/null | jq -r '.body' | jq .

# Wait for replication
sleep 3

# Read in us-west-2
aws lambda invoke --function-name global-demo-reader \
  --payload '{"user_id":"u001"}' \
  --region us-west-2 /dev/stdout 2>/dev/null | jq -r '.body' | jq .
```

</details>

## Verify What You Learned

```bash
# Verify global table has replica
aws dynamodb describe-table --table-name global-demo-data --region us-east-1 \
  --query "Table.Replicas[*].RegionName" --output json

# Write in us-east-1
aws dynamodb put-item --table-name global-demo-data --region us-east-1 \
  --item '{"PK":{"S":"TEST#001"},"SK":{"S":"DATA"},"value":{"S":"from-east"}}'

# Read in us-west-2 (wait for replication)
sleep 3
aws dynamodb get-item --table-name global-demo-data --region us-west-2 \
  --key '{"PK":{"S":"TEST#001"},"SK":{"S":"DATA"}}' \
  --query "Item.value.S" --output text

# Verify streams are enabled
aws dynamodb describe-table --table-name global-demo-data --region us-east-1 \
  --query "Table.StreamSpecification" --output json

terraform plan
```

Expected: Replicas include `us-west-2`, item value reads `from-east` in us-west-2, streams show `NEW_AND_OLD_IMAGES`.

## Cleanup

```bash
terraform destroy -auto-approve
terraform state list
```

Expected: no output (empty state). Note: destroying global tables may take several minutes as replicas are removed.

## What's Next

You built a multi-region DynamoDB global table with automatic replication and explored conflict resolution. Review the DynamoDB exercises 37-44 for a comprehensive understanding of single-table design, indexes, transactions, batch operations, TTL, capacity modes, and global tables -- all heavily tested on the DVA-C02 exam.

## Summary

- **Global tables** provide active-active multi-region replication with typically sub-second latency
- **Conflict resolution** uses last writer wins based on item timestamps -- this is automatic and non-configurable
- **Prerequisites**: DynamoDB Streams must be enabled, billing must be on-demand or provisioned with auto scaling, table name must be identical in all regions
- Replicated writes consume **replicated WCUs (rWCUs)**, which are more expensive than standard WCUs
- Each replica can have its own auto scaling configuration, CloudWatch alarms, and IAM policies
- **Replication is eventual**: there is no guarantee of immediate consistency across regions; reads in the replica region may return stale data for a brief period
- Global tables replicate the **entire table** -- selective replication of individual items or partitions is not supported
- You can add or remove replicas from an existing global table without downtime

## Reference

- [DynamoDB Global Tables](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/GlobalTables.html)
- [Global Tables v2](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/V2globaltables.html)
- [Terraform aws_dynamodb_table replica](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/dynamodb_table#replica)
- [Global Table Pricing](https://aws.amazon.com/dynamodb/pricing/global-tables/)

## Additional Resources

- [Global Tables Best Practices](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/V2globaltables.BestPractices.html) -- designing applications for multi-region consistency
- [Conflict Resolution in Global Tables](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/V2globaltables.BestPractices.html#V2globaltables.BestPractices.Conflicts) -- understanding last-writer-wins semantics
- [Monitoring Global Tables](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/V2globaltables.Monitoring.html) -- replication latency metrics and CloudWatch alarms
- [Global Tables and DynamoDB Streams](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/V2globaltables.Streams.html) -- how streams interact with replication and regional processing

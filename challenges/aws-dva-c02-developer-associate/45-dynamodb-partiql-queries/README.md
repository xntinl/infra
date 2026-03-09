# 45. DynamoDB PartiQL Queries

<!--
difficulty: advanced
concepts: [partiql, dynamodb-sql-queries, select, insert, update, delete, full-table-scan, partition-key, aws-cli-partiql, go-sdk-partiql]
tools: [terraform, aws-cli, go-sdk]
estimated_time: 45m
bloom_level: evaluate
prerequisites: [03-dynamodb-developer-sdk-operations]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a DynamoDB table in on-demand mode. Cost is approximately $0.01/hr for minimal read/write activity. Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| jq installed | `jq --version` |

## Learning Objectives

After completing this exercise, you will be able to:

- **Evaluate** when PartiQL queries are appropriate versus native DynamoDB API operations based on performance and cost tradeoffs
- **Construct** PartiQL SELECT, INSERT, UPDATE, and DELETE statements that target DynamoDB tables using partition key and sort key predicates
- **Differentiate** between PartiQL queries that perform efficient key lookups and those that trigger expensive full table scans
- **Execute** PartiQL statements through both the AWS CLI (`aws dynamodb execute-statement`) and the Go SDK (`ExecuteStatement` API)
- **Analyze** query execution behavior to identify whether a PartiQL SELECT uses a Query (efficient) or Scan (expensive) operation under the hood

## Why PartiQL for DynamoDB

PartiQL gives DynamoDB a SQL-compatible query language. Instead of constructing `PutItem`, `GetItem`, `Query`, and `Scan` calls with expression attribute names and values, you write familiar SQL syntax: `SELECT * FROM "orders" WHERE order_id = 'abc'`. This reduces the learning curve for developers coming from relational databases and simplifies ad-hoc queries from the CLI.

However, PartiQL on DynamoDB is not a relational query engine. The DVA-C02 exam tests whether you understand what happens under the hood. A `SELECT * FROM "orders" WHERE order_id = 'abc'` with a partition key predicate translates to an efficient `Query` operation. But a `SELECT * FROM "orders" WHERE status = 'shipped'` on a non-key attribute triggers a full table `Scan` -- reading every item in the table and filtering client-side. On a table with millions of items, this is slow and expensive. The exam frequently presents scenarios where a developer writes a PartiQL query that "works in dev but is slow in production" and asks you to identify the root cause: the WHERE clause does not include the partition key.

PartiQL also supports batch operations via `BatchExecuteStatement` and transactions via `ExecuteTransaction`, mirroring `BatchWriteItem` and `TransactWriteItems`. Understanding these mappings is essential because the exam tests whether you can identify the correct API for a given consistency or atomicity requirement.

## The Challenge

Build a Go program that executes PartiQL queries against a DynamoDB table through both the AWS CLI and the Go SDK. Configure the table with a composite primary key (partition key + sort key) and demonstrate all four CRUD operations. Identify which queries perform efficient key lookups versus expensive full scans.

### Requirements

| Requirement | Description |
|---|---|
| Table | DynamoDB table with partition key `customer_id` (S) and sort key `order_date` (S) |
| CLI INSERT | Insert 5 items using `aws dynamodb execute-statement` with PartiQL INSERT |
| CLI SELECT | Query items by partition key, by partition key + sort key, and without key (full scan) |
| CLI UPDATE | Update an item's status field using PartiQL UPDATE |
| CLI DELETE | Remove an item using PartiQL DELETE |
| Go SDK | Replicate all four operations using `ExecuteStatement` in the Go SDK |
| Batch | Use `BatchExecuteStatement` to insert 3 items in a single call |

### Architecture

```
  +-------------------+       +-------------------+
  |  AWS CLI           |       |  Go SDK Program    |
  |                   |       |                   |
  |  execute-statement|       |  ExecuteStatement  |
  |  batch-execute-   |       |  BatchExecute-     |
  |   statement       |       |   Statement        |
  +--------+----------+       +--------+----------+
           |                           |
           v                           v
  +-------------------------------------------+
  |  DynamoDB  --  PartiQL Engine              |
  |                                           |
  |  SELECT with PK  -->  Query (efficient)   |
  |  SELECT without PK --> Scan (expensive)   |
  |  INSERT  -->  PutItem                     |
  |  UPDATE  -->  UpdateItem                  |
  |  DELETE  -->  DeleteItem                  |
  +-------------------------------------------+
  |  Table: partiql-demo-orders               |
  |  PK: customer_id (S)                      |
  |  SK: order_date (S)                       |
  +-------------------------------------------+
```

## Hints

<details>
<summary>Hint 1: Table structure and Terraform setup</summary>

The table needs a composite primary key. PartiQL references table names in double quotes:

```hcl
resource "aws_dynamodb_table" "orders" {
  name         = "partiql-demo-orders"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "customer_id"
  range_key    = "order_date"

  attribute {
    name = "customer_id"
    type = "S"
  }

  attribute {
    name = "order_date"
    type = "S"
  }
}
```

PartiQL uses single quotes for string literals and double quotes for identifiers (table names, reserved words as column names).

</details>

<details>
<summary>Hint 2: PartiQL INSERT syntax for DynamoDB</summary>

PartiQL INSERT uses the `VALUE` keyword (not `VALUES` like standard SQL):

```sql
INSERT INTO "partiql-demo-orders" VALUE {
  'customer_id': 'cust-001',
  'order_date': '2025-01-15',
  'status': 'pending',
  'total': 49.99
}
```

Via the CLI:

```bash
aws dynamodb execute-statement --statement \
  "INSERT INTO \"partiql-demo-orders\" VALUE {'customer_id': 'cust-001', 'order_date': '2025-01-15', 'status': 'pending', 'total': 49.99}"
```

Note: PartiQL INSERT maps to `PutItem`. It overwrites an existing item with the same key unless you add a `WHERE` condition.

</details>

<details>
<summary>Hint 3: Efficient SELECT vs full scan</summary>

A SELECT with the partition key in the WHERE clause maps to a Query operation:

```sql
-- Efficient: Query on partition key
SELECT * FROM "partiql-demo-orders" WHERE customer_id = 'cust-001'

-- Efficient: Query on partition key + sort key condition
SELECT * FROM "partiql-demo-orders"
WHERE customer_id = 'cust-001' AND order_date >= '2025-01-01'
```

A SELECT without the partition key maps to a full table Scan:

```sql
-- EXPENSIVE: Full table scan -- filters AFTER reading every item
SELECT * FROM "partiql-demo-orders" WHERE status = 'shipped'

-- EXPENSIVE: Full table scan -- no WHERE clause at all
SELECT * FROM "partiql-demo-orders"
```

</details>

<details>
<summary>Hint 4: Go SDK ExecuteStatement</summary>

The Go SDK uses `ExecuteStatement` with parameterized queries:

```go
import (
    "github.com/aws/aws-sdk-go-v2/service/dynamodb"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

result, err := client.ExecuteStatement(ctx, &dynamodb.ExecuteStatementInput{
    Statement: aws.String(`SELECT * FROM "partiql-demo-orders" WHERE customer_id = ?`),
    Parameters: []types.AttributeValue{
        &types.AttributeValueMemberS{Value: "cust-001"},
    },
})
```

Use `?` placeholders and the `Parameters` field to avoid SQL injection and handle escaping automatically.

</details>

<details>
<summary>Hint 5: BatchExecuteStatement for multiple operations</summary>

Batch operations execute up to 25 statements in a single API call. Each statement is independent (no transaction guarantees):

```go
result, err := client.BatchExecuteStatement(ctx, &dynamodb.BatchExecuteStatementInput{
    Statements: []types.BatchStatementRequest{
        {
            Statement: aws.String(`INSERT INTO "partiql-demo-orders" VALUE {'customer_id': 'cust-010', 'order_date': '2025-03-01', 'status': 'pending', 'total': 25.00}`),
        },
        {
            Statement: aws.String(`INSERT INTO "partiql-demo-orders" VALUE {'customer_id': 'cust-011', 'order_date': '2025-03-02', 'status': 'pending', 'total': 35.00}`),
        },
    },
})
```

Check `result.Responses` for per-statement errors -- unlike `TransactWriteItems`, batch operations do not roll back on partial failure.

</details>

## Spot the Bug

A developer writes a PartiQL query to find all shipped orders for a dashboard. The query works correctly in the dev environment (100 items) but times out in production (5 million items) and consumes massive read capacity.

```go
func getShippedOrders(ctx context.Context, client *dynamodb.Client) ([]map[string]types.AttributeValue, error) {
    result, err := client.ExecuteStatement(ctx, &dynamodb.ExecuteStatementInput{
        Statement: aws.String(`SELECT * FROM "partiql-demo-orders" WHERE status = 'shipped'`),
    })
    if err != nil {
        return nil, err
    }
    return result.Items, nil
}
```

<details>
<summary>Explain the bug</summary>

The `WHERE status = 'shipped'` clause filters on a non-key attribute. Since `status` is neither the partition key nor the sort key, PartiQL translates this into a full table `Scan` with a filter expression. DynamoDB reads every item in the table, evaluates the filter, and returns only matching items. You are billed for the full scan (all 5 million items read), not just the matching results.

Additionally, `ExecuteStatement` returns at most 1 MB of data per call. For a 5-million-item table, you need to paginate using `NextToken`, making the operation even slower.

**Fix -- use a Global Secondary Index (GSI) and query by partition key:**

```hcl
resource "aws_dynamodb_table" "orders" {
  # ... existing config ...

  global_secondary_index {
    name            = "status-index"
    hash_key        = "status"
    range_key       = "order_date"
    projection_type = "ALL"
  }

  attribute {
    name = "status"
    type = "S"
  }
}
```

Then query the GSI using PartiQL:

```go
result, err := client.ExecuteStatement(ctx, &dynamodb.ExecuteStatementInput{
    Statement: aws.String(`SELECT * FROM "partiql-demo-orders"."status-index" WHERE status = 'shipped'`),
})
```

The GSI query uses the `status` field as a partition key, making it an efficient Query operation instead of a Scan.

On the exam, when you see "PartiQL query is slow in production," check whether the WHERE clause includes the partition key (or a GSI partition key). If it does not, the answer is almost always "add a GSI" or "restructure the query to include the partition key."

</details>

## Verify What You Learned

```bash
# Insert, query, update, and delete via PartiQL
aws dynamodb execute-statement --statement \
  "INSERT INTO \"partiql-demo-orders\" VALUE {'customer_id': 'verify-001', 'order_date': '2025-06-01', 'status': 'pending', 'total': 10.00}"

aws dynamodb execute-statement --statement \
  "SELECT * FROM \"partiql-demo-orders\" WHERE customer_id = 'verify-001'" \
  --query "Items" --output json
# Expected: one item with customer_id = verify-001

aws dynamodb execute-statement --statement \
  "UPDATE \"partiql-demo-orders\" SET status = 'shipped' WHERE customer_id = 'verify-001' AND order_date = '2025-06-01'"

aws dynamodb execute-statement --statement \
  "SELECT status FROM \"partiql-demo-orders\" WHERE customer_id = 'verify-001' AND order_date = '2025-06-01'" \
  --query "Items[0].status.S" --output text
# Expected: shipped

aws dynamodb execute-statement --statement \
  "DELETE FROM \"partiql-demo-orders\" WHERE customer_id = 'verify-001' AND order_date = '2025-06-01'"

aws dynamodb execute-statement --statement \
  "SELECT * FROM \"partiql-demo-orders\" WHERE customer_id = 'verify-001'" \
  --query "Items" --output json
# Expected: [] (empty array)

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

You executed SQL-compatible queries against DynamoDB using PartiQL and learned to distinguish efficient key-based queries from expensive full table scans. In the next exercise, you will configure **DynamoDB backup and point-in-time recovery** -- creating on-demand backups, enabling continuous backups, and restoring a table to any second within the last 35 days.

## Summary

- **PartiQL** provides SQL-compatible syntax for DynamoDB: SELECT, INSERT, UPDATE, DELETE
- INSERT uses `VALUE` (singular), not `VALUES` -- and maps directly to `PutItem`
- SELECT with a **partition key** in WHERE maps to an efficient `Query` operation
- SELECT **without a partition key** in WHERE triggers a full table `Scan` -- reading every item regardless of the filter
- Use **parameterized queries** (`?` placeholders) in the Go SDK to avoid injection and handle type marshaling
- `BatchExecuteStatement` runs up to 25 independent statements; `ExecuteTransaction` provides ACID guarantees across up to 100 statements
- PartiQL references table names in **double quotes** and string values in **single quotes**
- Query a GSI with the syntax `SELECT * FROM "table-name"."index-name" WHERE gsi_pk = 'value'`

## Reference

- [PartiQL for DynamoDB](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/ql-reference.html)
- [PartiQL SELECT Statement](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/ql-reference.select.html)
- [PartiQL INSERT Statement](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/ql-reference.insert.html)
- [AWS CLI execute-statement](https://docs.aws.amazon.com/cli/latest/reference/dynamodb/execute-statement.html)
- [Terraform aws_dynamodb_table](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/dynamodb_table)

## Additional Resources

- [PartiQL UPDATE and DELETE](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/ql-reference.update.html) -- syntax and limitations for modifying and removing items
- [BatchExecuteStatement API](https://docs.aws.amazon.com/amazondynamodb/latest/APIReference/API_BatchExecuteStatement.html) -- batch up to 25 PartiQL statements in a single call
- [ExecuteTransaction API](https://docs.aws.amazon.com/amazondynamodb/latest/APIReference/API_ExecuteTransaction.html) -- ACID transactions with PartiQL (up to 100 statements)
- [PartiQL vs Native API Performance](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/ql-reference.html) -- PartiQL adds minimal overhead but does not change the underlying access pattern
- [DynamoDB Global Secondary Indexes](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/GSI.html) -- required for efficient queries on non-key attributes

<details>
<summary>Full Solution</summary>

### `providers.tf`

```hcl
terraform {
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
  description = "Project name for resource naming"
  type        = string
  default     = "partiql-demo"
}
```

### `database.tf`

```hcl
resource "aws_dynamodb_table" "orders" {
  name         = "${var.project_name}-orders"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "customer_id"
  range_key    = "order_date"

  attribute {
    name = "customer_id"
    type = "S"
  }

  attribute {
    name = "order_date"
    type = "S"
  }
}
```

### `outputs.tf`

```hcl
output "table_name" {
  value = aws_dynamodb_table.orders.name
}
```

### `partiql-demo/main.go`

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

func main() {
	ctx := context.Background()
	cfg, _ := config.LoadDefaultConfig(ctx, config.WithRegion("us-east-1"))
	client := dynamodb.NewFromConfig(cfg)
	t := "partiql-demo-orders"

	// INSERT (literal)
	client.ExecuteStatement(ctx, &dynamodb.ExecuteStatementInput{
		Statement: aws.String(fmt.Sprintf(`INSERT INTO "%s" VALUE {'customer_id':'cust-001','order_date':'2025-01-15','status':'pending','total':49.99}`, t)),
	})

	// INSERT (parameterized)
	client.ExecuteStatement(ctx, &dynamodb.ExecuteStatementInput{
		Statement:  aws.String(fmt.Sprintf(`INSERT INTO "%s" VALUE {'customer_id':?,'order_date':?,'status':?,'total':?}`, t)),
		Parameters: []types.AttributeValue{
			&types.AttributeValueMemberS{Value: "cust-001"}, &types.AttributeValueMemberS{Value: "2025-02-20"},
			&types.AttributeValueMemberS{Value: "shipped"}, &types.AttributeValueMemberN{Value: "99.50"},
		},
	})

	// BATCH INSERT
	client.BatchExecuteStatement(ctx, &dynamodb.BatchExecuteStatementInput{
		Statements: []types.BatchStatementRequest{
			{Statement: aws.String(fmt.Sprintf(`INSERT INTO "%s" VALUE {'customer_id':'cust-002','order_date':'2025-01-10','status':'pending','total':25.00}`, t))},
			{Statement: aws.String(fmt.Sprintf(`INSERT INTO "%s" VALUE {'customer_id':'cust-003','order_date':'2025-02-14','status':'pending','total':120.00}`, t))},
		},
	})

	// SELECT by PK (efficient Query)
	res, _ := client.ExecuteStatement(ctx, &dynamodb.ExecuteStatementInput{
		Statement:  aws.String(fmt.Sprintf(`SELECT * FROM "%s" WHERE customer_id = ?`, t)),
		Parameters: []types.AttributeValue{&types.AttributeValueMemberS{Value: "cust-001"}},
	})
	fmt.Printf("Found %d items for cust-001\n", len(res.Items))

	// UPDATE
	client.ExecuteStatement(ctx, &dynamodb.ExecuteStatementInput{
		Statement:  aws.String(fmt.Sprintf(`UPDATE "%s" SET status = ? WHERE customer_id = ? AND order_date = ?`, t)),
		Parameters: []types.AttributeValue{
			&types.AttributeValueMemberS{Value: "shipped"}, &types.AttributeValueMemberS{Value: "cust-001"},
			&types.AttributeValueMemberS{Value: "2025-01-15"},
		},
	})

	// DELETE
	client.ExecuteStatement(ctx, &dynamodb.ExecuteStatementInput{
		Statement:  aws.String(fmt.Sprintf(`DELETE FROM "%s" WHERE customer_id = ? AND order_date = ?`, t)),
		Parameters: []types.AttributeValue{
			&types.AttributeValueMemberS{Value: "cust-003"}, &types.AttributeValueMemberS{Value: "2025-02-14"},
		},
	})

	log.Println("All PartiQL operations completed successfully.")
}
```

### CLI Commands

```bash
terraform init && terraform apply -auto-approve

# INSERT, SELECT, UPDATE, DELETE via CLI
aws dynamodb execute-statement --statement \
  "INSERT INTO \"partiql-demo-orders\" VALUE {'customer_id':'cli-001','order_date':'2025-04-01','status':'pending','total':30.00}"
aws dynamodb execute-statement --statement \
  "SELECT * FROM \"partiql-demo-orders\" WHERE customer_id = 'cli-001'" --query "Items" --output json
aws dynamodb execute-statement --statement \
  "UPDATE \"partiql-demo-orders\" SET status = 'shipped' WHERE customer_id = 'cli-001' AND order_date = '2025-04-01'"
aws dynamodb execute-statement --statement \
  "DELETE FROM \"partiql-demo-orders\" WHERE customer_id = 'cli-001' AND order_date = '2025-04-01'"

cd partiql-demo && go run main.go
```

</details>

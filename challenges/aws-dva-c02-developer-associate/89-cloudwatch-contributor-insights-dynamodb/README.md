# 89. CloudWatch Contributor Insights for DynamoDB

<!--
difficulty: intermediate
concepts: [contributor-insights, dynamodb-hot-keys, partition-key-distribution, throttling-analysis, access-patterns, cloudwatch-rules]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: analyze
prerequisites: [none]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a DynamoDB table with Contributor Insights enabled and a Lambda function to generate load. DynamoDB on-demand pricing is pay-per-request. Contributor Insights costs $0.02 per million events analyzed. Total cost is approximately $0.01/hr for light testing. Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.5 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| jq installed | `jq --version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Enable** CloudWatch Contributor Insights on a DynamoDB table to track the most accessed and throttled partition keys
2. **Analyze** Contributor Insights reports to identify hot partition keys causing uneven load distribution
3. **Differentiate** between the built-in Contributor Insights rules for DynamoDB: `DynamoDBContributorInsights` for most accessed keys and throttled keys
4. **Explain** why Contributor Insights data may not show throttling events when a table uses on-demand capacity mode
5. **Design** partition key strategies based on Contributor Insights data to improve access distribution

## Why CloudWatch Contributor Insights for DynamoDB

DynamoDB distributes data across partitions based on the hash of the partition key. When a small number of partition keys receive a disproportionate share of traffic -- a "hot key" pattern -- performance degrades. In provisioned mode, hot keys cause `ProvisionedThroughputExceededException` (throttling). In on-demand mode, hot keys can still cause throttling if a single partition exceeds 3,000 RCU or 1,000 WCU.

Before Contributor Insights, identifying hot keys required custom instrumentation: logging every access, aggregating by key, and analyzing the distribution. Contributor Insights automates this entirely. When enabled on a DynamoDB table, it automatically tracks the most frequently accessed partition keys and sort keys. Two built-in rules are created: one for most-accessed keys and one for most-throttled keys.

The DVA-C02 exam tests whether candidates understand hot key detection and mitigation. A common scenario: "A DynamoDB table with provisioned capacity is experiencing throttling on specific items. How do you identify which partition keys are causing the throttling?" The answer is CloudWatch Contributor Insights. The exam also tests the important caveat: if the table uses on-demand capacity mode and traffic is below the per-partition limits, the "throttled keys" report will be empty because no throttling occurs -- the traffic pattern is still uneven, but DynamoDB handles it within on-demand's adaptive capacity.

## Building Blocks

### `lambda/main.go`

This Lambda generates DynamoDB traffic with intentionally skewed key distribution:

```go
package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

var (
	client    *dynamodb.Client
	tableName string
)

func init() {
	tableName = os.Getenv("TABLE_NAME")
	cfg, _ := config.LoadDefaultConfig(context.TODO())
	client = dynamodb.NewFromConfig(cfg)
}

type Response struct {
	Written int    `json:"written"`
	Read    int    `json:"read"`
	Message string `json:"message"`
}

func handler(ctx context.Context) (Response, error) {
	written := 0
	readCount := 0

	// Hot key pattern: 80% of writes go to 5 "hot" keys,
	// 20% of writes go to 95 "cold" keys
	for i := 0; i < 100; i++ {
		var key string
		if rand.Float64() < 0.8 {
			key = fmt.Sprintf("hot-key-%d", rand.Intn(5))
		} else {
			key = fmt.Sprintf("cold-key-%d", rand.Intn(95))
		}

		_, err := client.PutItem(ctx, &dynamodb.PutItemInput{
			TableName: aws.String(tableName),
			Item: map[string]types.AttributeValue{
				"pk":        &types.AttributeValueMemberS{Value: key},
				"timestamp": &types.AttributeValueMemberN{Value: strconv.FormatInt(time.Now().UnixNano(), 10)},
				"data":      &types.AttributeValueMemberS{Value: fmt.Sprintf("payload-%d", i)},
			},
		})
		if err != nil {
			fmt.Printf("Write error for key %s: %v\n", key, err)
			continue
		}
		written++
	}

	// Hot key reads: same 80/20 distribution
	for i := 0; i < 50; i++ {
		var key string
		if rand.Float64() < 0.8 {
			key = fmt.Sprintf("hot-key-%d", rand.Intn(5))
		} else {
			key = fmt.Sprintf("cold-key-%d", rand.Intn(95))
		}

		_, err := client.GetItem(ctx, &dynamodb.GetItemInput{
			TableName: aws.String(tableName),
			Key: map[string]types.AttributeValue{
				"pk": &types.AttributeValueMemberS{Value: key},
			},
		})
		if err != nil {
			fmt.Printf("Read error for key %s: %v\n", key, err)
			continue
		}
		readCount++
	}

	return Response{
		Written: written,
		Read:    readCount,
		Message: "Load generation complete",
	}, nil
}

func main() {
	lambda.Start(handler)
}
```

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
    archive = {
      source  = "hashicorp/archive"
      version = "~> 2.0"
    }
    null = {
      source  = "hashicorp/null"
      version = "~> 3.0"
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
  default     = "contributor-insights-demo"
}
```

### `database.tf`

```hcl
resource "aws_dynamodb_table" "this" {
  name         = var.project_name
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "pk"

  attribute {
    name = "pk"
    type = "S"
  }
}

# =======================================================
# TODO 1 -- Enable Contributor Insights on the table
# =======================================================
# Requirements:
#   - Create an aws_dynamodb_contributor_insights resource
#   - Set table_name to the DynamoDB table name
#   - This automatically creates two CloudWatch Contributor
#     Insights rules for the table:
#     - Most accessed items (partition keys with highest traffic)
#     - Most throttled items (partition keys causing throttling)
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/dynamodb_contributor_insights
```

### `build.tf`

```hcl
resource "null_resource" "go_build" {
  triggers = {
    source_hash = filebase64sha256("${path.module}/main.go")
  }
  provisioner "local-exec" {
    command     = "GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go"
    working_dir = path.module
  }
}

data "archive_file" "function_zip" {
  type        = "zip"
  source_file = "${path.module}/bootstrap"
  output_path = "${path.module}/build/function.zip"
  depends_on  = [null_resource.go_build]
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

resource "aws_iam_role" "this" {
  name               = "${var.project_name}-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

resource "aws_iam_role_policy_attachment" "basic" {
  role       = aws_iam_role.this.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

data "aws_iam_policy_document" "dynamodb_access" {
  statement {
    actions   = ["dynamodb:PutItem", "dynamodb:GetItem", "dynamodb:Query"]
    resources = [aws_dynamodb_table.this.arn]
  }
}

resource "aws_iam_role_policy" "dynamodb" {
  name   = "dynamodb-access"
  role   = aws_iam_role.this.id
  policy = data.aws_iam_policy_document.dynamodb_access.json
}
```

### `lambda.tf`

```hcl
resource "aws_cloudwatch_log_group" "this" {
  name              = "/aws/lambda/${var.project_name}"
  retention_in_days = 1
}

resource "aws_lambda_function" "this" {
  function_name    = var.project_name
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.this.arn
  memory_size      = 256
  timeout          = 60

  environment {
    variables = {
      TABLE_NAME = aws_dynamodb_table.this.name
    }
  }

  depends_on = [aws_iam_role_policy_attachment.basic, aws_cloudwatch_log_group.this]
}
```

### `outputs.tf`

```hcl
output "function_name" {
  value = aws_lambda_function.this.function_name
}

output "table_name" {
  value = aws_dynamodb_table.this.name
}
```

## Spot the Bug

A developer enables Contributor Insights on their DynamoDB table and generates heavy load against a few partition keys. They check the "Most throttled items" report expecting to see the hot keys, but the report is empty. They conclude Contributor Insights is broken.

```hcl
resource "aws_dynamodb_table" "orders" {
  name         = "orders"
  billing_mode = "PAY_PER_REQUEST"   # <-- On-demand mode
  hash_key     = "order_id"

  attribute {
    name = "order_id"
    type = "S"
  }
}

resource "aws_dynamodb_contributor_insights" "orders" {
  table_name = aws_dynamodb_table.orders.name
}
```

<details>
<summary>Explain the bug</summary>

Contributor Insights is working correctly. The "Most throttled items" report is empty because the table uses **on-demand capacity mode**, which automatically scales to handle traffic without provisioned throughput limits. In on-demand mode, DynamoDB accommodates up to 40,000 RCU and 40,000 WCU per table (with adaptive capacity distributing to hot partitions). Unless a single partition key exceeds the per-partition limit of 3,000 RCU or 1,000 WCU, no throttling occurs and therefore no throttled keys appear in the report.

The developer's expectation was wrong, not the tool. The "Most accessed items" report still correctly shows which partition keys receive the most traffic -- this is the report they should examine to identify hot key patterns before they become throttling problems.

**To see throttling in the report**, the developer would need to:

1. Switch to provisioned capacity mode with intentionally low throughput:

```hcl
resource "aws_dynamodb_table" "orders" {
  name         = "orders"
  billing_mode = "PROVISIONED"
  hash_key     = "order_id"
  read_capacity  = 5    # Very low -- will throttle easily
  write_capacity = 5

  attribute {
    name = "order_id"
    type = "S"
  }
}
```

2. Or generate enough traffic to exceed the per-partition limit (1,000 WCU on a single partition key), which is impractical for testing.

The key insight: **use the "Most accessed items" report for capacity planning and hot key detection, even when using on-demand mode. The "Most throttled items" report is most useful with provisioned capacity.**

</details>

## Verify What You Learned

### Step 1 -- Apply the infrastructure

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init && terraform apply -auto-approve
```

### Step 2 -- Verify Contributor Insights is enabled

```bash
aws dynamodb describe-contributor-insights \
  --table-name $(terraform output -raw table_name) \
  --query "{Status:ContributorInsightsStatus,Rules:ContributorInsightsRuleList}" \
  --output json
```

Expected: `"Status": "ENABLED"` with rules listed.

### Step 3 -- Generate skewed load

```bash
FUNC=$(terraform output -raw function_name)

for i in $(seq 1 10); do
  aws lambda invoke --function-name "$FUNC" /dev/stdout 2>/dev/null
  echo ""
done
```

### Step 4 -- View Contributor Insights report

Wait 5 minutes for data aggregation, then:

```bash
TABLE=$(terraform output -raw table_name)

aws cloudwatch get-insight-rule-report \
  --rule-name "DynamoDBContributorInsights-PKC-${TABLE}" \
  --start-time $(date -u -v-30M +%Y-%m-%dT%H:%M:%S) \
  --end-time $(date -u +%Y-%m-%dT%H:%M:%S) \
  --period 300 \
  --max-contributor-count 10 \
  --query "Contributors[].{Key:Keys[0],Count:ApproximateAggregateValue}" \
  --output table 2>/dev/null || echo "Note: Rule name may differ. Check 'aws cloudwatch describe-insight-rules' for exact names."
```

### Step 5 -- List all Contributor Insights rules

```bash
aws cloudwatch describe-insight-rules \
  --query "InsightRules[?contains(Name, '$(terraform output -raw table_name)')].{Name:Name,State:State}" \
  --output table
```

Expected: rules containing the table name with state `ENABLED`.

### Step 6 -- Verify no drift

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Solutions

<details>
<summary>database.tf -- TODO 1 -- Enable Contributor Insights</summary>

```hcl
resource "aws_dynamodb_contributor_insights" "this" {
  table_name = aws_dynamodb_table.this.name
}
```

This single resource enables Contributor Insights on the table. DynamoDB automatically creates the CloudWatch Contributor Insights rules for most-accessed and most-throttled partition keys. No additional CloudWatch configuration is needed.

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

You used Contributor Insights to identify hot partition keys in DynamoDB. In the next exercise, you will configure **X-Ray sampling rules and groups** to control trace collection rates and organize traces by filter expressions.

## Summary

- **Contributor Insights** for DynamoDB automatically tracks the most frequently accessed and most throttled partition keys
- Enabling it creates two CloudWatch Contributor Insights rules per table (accessed keys and throttled keys)
- The **most throttled items** report is empty when using on-demand capacity mode below per-partition limits (3,000 RCU / 1,000 WCU per partition)
- The **most accessed items** report works regardless of capacity mode and is the primary tool for hot key detection
- Contributor Insights costs $0.02 per million events analyzed -- negligible for most workloads
- Hot key patterns (80/20 distribution) can be identified before they cause throttling by monitoring the access distribution
- Mitigation strategies: add a random suffix to partition keys, use write sharding, or switch access patterns to distribute load

## Reference

- [DynamoDB Contributor Insights](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/ContributorInsights.html)
- [CloudWatch Contributor Insights](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/ContributorInsights.html)
- [Terraform aws_dynamodb_contributor_insights](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/dynamodb_contributor_insights)

## Additional Resources

- [DynamoDB Partitions and Data Distribution](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/HowItWorks.Partitions.html) -- understanding how partition keys affect data placement
- [Best Practices for Partition Keys](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/bp-partition-key-design.html) -- designing keys to avoid hot partitions
- [DynamoDB Adaptive Capacity](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/bp-partition-key-design.html#bp-partition-key-partitions-adaptive) -- how DynamoDB redistributes capacity to hot partitions
- [Contributor Insights Pricing](https://aws.amazon.com/cloudwatch/pricing/) -- $0.02 per million contributor events

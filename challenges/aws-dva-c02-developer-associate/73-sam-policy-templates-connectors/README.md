# 73. SAM Policy Templates and Connectors

<!--
difficulty: intermediate
concepts: [sam-policy-templates, dynamodb-crud-policy, s3-read-policy, sqs-poller-policy, sam-connectors, aws-serverless-connector, iam-simplification, least-privilege]
tools: [sam-cli, aws-cli, terraform]
estimated_time: 40m
bloom_level: analyze, implement
prerequisites: [06-sam-local-development-packaging, 03-dynamodb-developer-sdk-operations]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates Lambda functions, a DynamoDB table, an SQS queue, and an S3 bucket. All costs are negligible for testing (~$0.01/hr or less). Remember to run `sam delete` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| SAM CLI >= 1.100 | `sam --version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| Docker installed | `docker --version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Analyze** SAM policy templates (DynamoDBCrudPolicy, S3ReadPolicy, SQSPollerPolicy) and explain how they map to underlying IAM policy statements
2. **Replace** verbose inline IAM policies with concise SAM policy templates that enforce least-privilege access
3. **Implement** SAM Connectors (AWS::Serverless::Connector) to declare permissions between resources without writing IAM policies at all
4. **Differentiate** between policy templates (explicit IAM) and Connectors (implicit IAM) and choose the appropriate approach for each scenario
5. **Debug** common errors with policy template resource references and Connector permission types

## Why This Matters

Writing IAM policies by hand is the most error-prone part of serverless development. A missing action, wrong resource ARN, or overly broad wildcard can cause runtime failures or security vulnerabilities. SAM addresses this with two mechanisms.

**Policy templates** are pre-built IAM snippets parameterized by resource. `DynamoDBCrudPolicy` generates CRUD permissions scoped to a table ARN. `SQSPollerPolicy` generates ReceiveMessage/DeleteMessage scoped to a queue. **Connectors** (`AWS::Serverless::Connector`) go further -- declare source, destination, and permission type (Read/Write), and SAM synthesizes all required policies automatically.

The DVA-C02 exam: "grant Lambda access to DynamoDB with least privilege" -- answer is `DynamoDBCrudPolicy` with the table name, not `dynamodb:*`.

## Building Blocks

### Lambda Function Code

Create a file called `order-processor/main.go`:

```go
// order-processor/main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

type Order struct {
	OrderID   string  `dynamodbav:"PK" json:"order_id"`
	Customer  string  `dynamodbav:"customer" json:"customer"`
	Amount    float64 `dynamodbav:"amount" json:"amount"`
	Status    string  `dynamodbav:"status" json:"status"`
	CreatedAt string  `dynamodbav:"created_at" json:"created_at"`
}

func handler(ctx context.Context, sqsEvent events.SQSEvent) error {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ddbClient := dynamodb.NewFromConfig(cfg)
	tableName := os.Getenv("TABLE_NAME")

	for _, record := range sqsEvent.Records {
		var order Order
		if err := json.Unmarshal([]byte(record.Body), &order); err != nil {
			fmt.Printf("ERROR: failed to parse message: %v\n", err)
			continue
		}

		order.Status = "processed"
		order.CreatedAt = time.Now().UTC().Format(time.RFC3339)

		item, err := attributevalue.MarshalMap(order)
		if err != nil {
			return fmt.Errorf("marshal order: %w", err)
		}

		_, err = ddbClient.PutItem(ctx, &dynamodb.PutItemInput{
			TableName: &tableName,
			Item:      item,
		})
		if err != nil {
			return fmt.Errorf("put item: %w", err)
		}

		fmt.Printf("Processed order %s for customer %s (amount: %.2f)\n",
			order.OrderID, order.Customer, order.Amount)
	}
	return nil
}

func main() {
	lambda.Start(handler)
}
```

### SAM Template Skeleton

Create a file called `template.yaml` with the following skeleton. Fill in each `# TODO` block.

```yaml
AWSTemplateFormatVersion: "2010-09-09"
Transform: AWS::Serverless-2016-10-31
Description: SAM Policy Templates and Connectors Demo

Globals:
  Function:
    Runtime: provided.al2023
    Architectures:
      - arm64
    Handler: bootstrap
    Timeout: 15

Resources:
  # DynamoDB Table
  OrdersTable:
    Type: AWS::DynamoDB::Table
    Properties:
      TableName: !Sub "${AWS::StackName}-orders"
      BillingMode: PAY_PER_REQUEST
      AttributeDefinitions:
        - AttributeName: PK
          AttributeType: S
      KeySchema:
        - AttributeName: PK
          KeyType: HASH

  # SQS Queue
  OrderQueue:
    Type: AWS::SQS::Queue
    Properties:
      QueueName: !Sub "${AWS::StackName}-orders"
      VisibilityTimeout: 60

  # S3 Bucket for order reports
  ReportsBucket:
    Type: AWS::S3::Bucket
    Properties:
      BucketName: !Sub "${AWS::StackName}-reports-${AWS::AccountId}"

  # =======================================================
  # TODO 1 -- Replace verbose IAM with Policy Templates
  # =======================================================
  # The OrderProcessor function currently has verbose inline
  # IAM policies. Replace them with SAM policy templates:
  #
  #   - DynamoDBCrudPolicy for the OrdersTable
  #   - SQSPollerPolicy for the OrderQueue
  #
  # Current verbose version (REPLACE THIS):
  #   Policies:
  #     - Statement:
  #         - Effect: Allow
  #           Action:
  #             - dynamodb:GetItem
  #             - dynamodb:PutItem
  #             - dynamodb:UpdateItem
  #             - dynamodb:DeleteItem
  #             - dynamodb:Query
  #             - dynamodb:Scan
  #           Resource: !GetAtt OrdersTable.Arn
  #         - Effect: Allow
  #           Action:
  #             - sqs:ReceiveMessage
  #             - sqs:DeleteMessage
  #             - sqs:GetQueueAttributes
  #           Resource: !GetAtt OrderQueue.Arn
  #
  # Replace with:
  #   Policies:
  #     - DynamoDBCrudPolicy:
  #         TableName: !Ref OrdersTable
  #     - SQSPollerPolicy:
  #         QueueName: !GetAtt OrderQueue.QueueName
  OrderProcessor:
    Type: AWS::Serverless::Function
    Metadata:
      BuildMethod: go1.x
    Properties:
      CodeUri: order-processor/
      Environment:
        Variables:
          TABLE_NAME: !Ref OrdersTable
      Events:
        SQSTrigger:
          Type: SQS
          Properties:
            Queue: !GetAtt OrderQueue.Arn
            BatchSize: 5
      # TODO: Replace these verbose policies with SAM policy templates
      Policies:
        - Statement:
            - Effect: Allow
              Action:
                - dynamodb:GetItem
                - dynamodb:PutItem
                - dynamodb:UpdateItem
                - dynamodb:DeleteItem
                - dynamodb:Query
                - dynamodb:Scan
              Resource: !GetAtt OrdersTable.Arn
            - Effect: Allow
              Action:
                - sqs:ReceiveMessage
                - sqs:DeleteMessage
                - sqs:GetQueueAttributes
              Resource: !GetAtt OrderQueue.Arn

  # =======================================================
  # TODO 2 -- Add a Connector for Lambda -> DynamoDB
  # =======================================================
  # Create an AWS::Serverless::Connector granting ReportGenerator
  # Read+Write access to OrdersTable. Source Id + Destination Id + Permissions.

  ReportGenerator:
    Type: AWS::Serverless::Function
    Metadata:
      BuildMethod: go1.x
    Properties:
      CodeUri: order-processor/
      Environment:
        Variables:
          TABLE_NAME: !Ref OrdersTable
          BUCKET_NAME: !Ref ReportsBucket
      # TODO: Use a Connector instead of inline policies

  # =======================================================
  # TODO 3 -- Add a Connector for Lambda -> S3
  # =======================================================
  # Create a Connector granting ReportGenerator Read access
  # to the ReportsBucket.
  #
  # Requirements:
  #   - Source: ReportGenerator function
  #   - Destination: ReportsBucket
  #   - Permissions: [Read]

Outputs:
  TableName:
    Value: !Ref OrdersTable
  QueueUrl:
    Value: !GetAtt OrderQueue.QueueUrl
  BucketName:
    Value: !Ref ReportsBucket
  ProcessorFunction:
    Value: !Ref OrderProcessor
```

## Spot the Bug

A developer uses SAM policy templates, but the Lambda function gets AccessDenied when calling DynamoDB PutItem. **What is wrong?**

```yaml
OrderProcessor:
  Type: AWS::Serverless::Function
  Properties:
    CodeUri: processor/
    Handler: bootstrap
    Runtime: provided.al2023
    Environment:
      Variables:
        TABLE_NAME: !Ref OrdersTable
    Policies:
      - DynamoDBCrudPolicy:
          TableName: !GetAtt OrdersTable.Arn    # <-- BUG
```

<details>
<summary>Explain the bug</summary>

The `DynamoDBCrudPolicy` template expects the **table name** (a string like `my-table`), not the **table ARN** (a string like `arn:aws:dynamodb:us-east-1:123456789012:table/my-table`). Using `!GetAtt OrdersTable.Arn` passes the full ARN as the table name parameter.

SAM policy templates construct the resource ARN internally. When you pass the ARN as the table name, SAM creates a malformed resource ARN like `arn:aws:dynamodb:*:*:table/arn:aws:dynamodb:us-east-1:123456789012:table/my-table` -- which does not match the actual table ARN.

The fix:

```yaml
Policies:
  - DynamoDBCrudPolicy:
      TableName: !Ref OrdersTable    # !Ref returns the table name, not ARN
```

Key rule: SAM policy template parameters use the **logical resource reference** (`!Ref`), not `!GetAtt`. `!Ref` for a DynamoDB table returns the table name. `!GetAtt OrdersTable.Arn` returns the full ARN. The policy template needs the name because it builds the ARN itself.

</details>

## Solutions

<details>
<summary>TODO 1 -- Policy Templates</summary>

```yaml
OrderProcessor:
  Type: AWS::Serverless::Function
  Metadata:
    BuildMethod: go1.x
  Properties:
    CodeUri: order-processor/
    Environment:
      Variables:
        TABLE_NAME: !Ref OrdersTable
    Events:
      SQSTrigger:
        Type: SQS
        Properties:
          Queue: !GetAtt OrderQueue.Arn
          BatchSize: 5
    Policies:
      - DynamoDBCrudPolicy:
          TableName: !Ref OrdersTable
      - SQSPollerPolicy:
          QueueName: !GetAtt OrderQueue.QueueName
```

`DynamoDBCrudPolicy` generates CRUD actions scoped to the table ARN and its indexes. `SQSPollerPolicy` generates ReceiveMessage, DeleteMessage, GetQueueAttributes scoped to the queue ARN.

</details>

<details>
<summary>TODO 2 -- Connector for Lambda to DynamoDB</summary>

```yaml
ReportTableConnector:
  Type: AWS::Serverless::Connector
  Properties:
    Source:
      Id: ReportGenerator
    Destination:
      Id: OrdersTable
    Permissions:
      - Read
      - Write
```

This connector generates IAM policies for reading and writing to the DynamoDB table, scoped to the table ARN and its indexes.

</details>

<details>
<summary>TODO 3 -- Connector for Lambda to S3</summary>

```yaml
ReportBucketConnector:
  Type: AWS::Serverless::Connector
  Properties:
    Source:
      Id: ReportGenerator
    Destination:
      Id: ReportsBucket
    Permissions:
      - Read
```

This connector generates S3 read policies (GetObject, ListBucket) scoped to the bucket ARN.

</details>

## Verify What You Learned

### Step 1 -- Deploy

```bash
sam build
sam deploy --guided --stack-name sam-policy-demo
```

### Step 2 -- Send a test message

```bash
QUEUE_URL=$(aws cloudformation describe-stacks --stack-name sam-policy-demo \
  --query "Stacks[0].Outputs[?OutputKey=='QueueUrl'].OutputValue" --output text)

aws sqs send-message --queue-url "$QUEUE_URL" \
  --message-body '{"order_id":"ord-001","customer":"alice","amount":99.99}'
```

### Step 3 -- Verify the order was written to DynamoDB

```bash
TABLE_NAME=$(aws cloudformation describe-stacks --stack-name sam-policy-demo \
  --query "Stacks[0].Outputs[?OutputKey=='TableName'].OutputValue" --output text)

sleep 10
aws dynamodb get-item --table-name "$TABLE_NAME" \
  --key '{"PK":{"S":"ord-001"}}' --output json | jq .
```

Expected: the order item with status "processed".

### Step 4 -- Verify IAM policies were generated

```bash
FUNCTION_NAME=$(aws cloudformation describe-stacks --stack-name sam-policy-demo \
  --query "Stacks[0].Outputs[?OutputKey=='ProcessorFunction'].OutputValue" --output text)
ROLE_NAME=$(aws lambda get-function-configuration --function-name "$FUNCTION_NAME" \
  --query "Role" --output text | awk -F/ '{print $NF}')
aws iam list-role-policies --role-name "$ROLE_NAME" --output json
```

Expected: policies generated by SAM policy templates.

## Cleanup

```bash
sam delete --stack-name sam-policy-demo --no-prompts
```

## What's Next

You simplified IAM with SAM policy templates and Connectors. In the next exercise, you will build **CloudFormation macros** -- Lambda-backed transforms that process template fragments to auto-add tags, inject resources, or enforce compliance rules.

## Summary

- **SAM policy templates** are pre-built IAM snippets: `DynamoDBCrudPolicy`, `S3ReadPolicy`, `SQSPollerPolicy`, `SNSPublishMessagePolicy`, and many more
- Policy templates use **!Ref** (resource name), not **!GetAtt** (ARN) -- SAM constructs the ARN internally
- **Connectors** (`AWS::Serverless::Connector`) declare source, destination, and permission type (Read/Write) -- SAM generates all IAM policies automatically
- Connectors support Lambda, DynamoDB, S3, SQS, SNS, Step Functions, API Gateway, and EventBridge
- Policy templates give **explicit control**; Connectors provide **implicit convenience** but less visibility
- Common exam trap: `!GetAtt Table.Arn` instead of `!Ref Table` in a policy template creates a malformed ARN

## Reference

- [SAM Policy Templates](https://docs.aws.amazon.com/serverless-application-model/latest/developerguide/serverless-policy-templates.html)
- [SAM Connectors](https://docs.aws.amazon.com/serverless-application-model/latest/developerguide/managing-permissions-connectors.html)
- [SAM Policy Template List](https://docs.aws.amazon.com/serverless-application-model/latest/developerguide/serverless-policy-template-list.html)

## Additional Resources

- [SAM Connector Supported Resource Types](https://docs.aws.amazon.com/serverless-application-model/latest/developerguide/reference-sam-connector.html) -- full matrix of supported source/destination pairs
- [Least Privilege IAM for Lambda](https://docs.aws.amazon.com/lambda/latest/dg/lambda-permissions.html) -- AWS best practices for Lambda IAM roles

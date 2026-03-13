# 29. API Gateway WebSocket APIs

<!--
difficulty: basic
concepts: [websocket-api, connect-disconnect-routes, connection-management, dynamodb-connections, apigatewaymanagementapi, wscat]
tools: [terraform, aws-cli, wscat]
estimated_time: 40m
bloom_level: understand
prerequisites: [none]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a WebSocket API Gateway, three Lambda functions, and a DynamoDB table. API Gateway WebSocket pricing is $1.00 per million connection minutes and $1.00 per million messages. For testing, this is well under $0.01/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally (for compiling Lambda binaries)
- wscat installed (`npm install -g wscat`) for testing WebSocket connections

## Learning Objectives

After completing this exercise, you will be able to:

- **Identify** the three predefined WebSocket API routes ($connect, $disconnect, $default) and when each is invoked
- **Construct** a WebSocket API with Lambda integrations for connection lifecycle management using Terraform
- **Explain** how connection IDs work: API Gateway assigns a unique connection ID on $connect, which you store in DynamoDB and use to send messages back to clients
- **Verify** real-time bidirectional communication using wscat and the API Gateway Management API
- **Describe** the API Gateway Management API endpoint (`@connections/{connectionId}`) used to push messages from the server to connected clients

## Why API Gateway WebSocket APIs

REST APIs follow a request-response pattern: the client sends a request, the server responds, and the connection closes. WebSocket APIs maintain a persistent, bidirectional connection. The server can push messages to the client at any time without the client polling.

API Gateway WebSocket APIs route messages based on a `routeKey` extracted from the message payload. Three routes are predefined:

- **$connect**: Invoked when a client opens a WebSocket connection. Use this to authenticate the client and store the connection ID.
- **$disconnect**: Invoked when a client closes the connection (or it times out). Use this to remove the connection ID from your store.
- **$default**: Invoked for any message that does not match a custom route. This is your catch-all message handler.

You can also define custom routes based on a field in the message payload (e.g., `{"action": "sendMessage"}` routes to a `sendMessage` integration).

The DVA-C02 exam tests WebSocket API concepts in the context of real-time applications: chat, notifications, live dashboards. Key exam concepts: connection management with DynamoDB, the `@connections` API for server-to-client messaging, and the difference between the `$connect` route (HTTP upgrade, one-time) and the `$default` route (per-message).

## Step 1 -- Create the Lambda Handler Code

### `lambda/main.go`

This single binary handles all three routes based on the `routeKey`:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/apigatewaymanagementapi"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

var (
	tableName  string
	ddbClient  *dynamodb.Client
)

func init() {
	tableName = os.Getenv("TABLE_NAME")
	cfg, _ := config.LoadDefaultConfig(context.TODO())
	ddbClient = dynamodb.NewFromConfig(cfg)
}

func handler(ctx context.Context, request events.APIGatewayWebsocketProxyRequest) (events.APIGatewayProxyResponse, error) {
	connectionID := request.RequestContext.ConnectionID
	routeKey := request.RequestContext.RouteKey

	fmt.Printf("Route: %s, ConnectionID: %s\n", routeKey, connectionID)

	switch routeKey {
	case "$connect":
		return handleConnect(ctx, connectionID)
	case "$disconnect":
		return handleDisconnect(ctx, connectionID)
	case "$default":
		return handleDefault(ctx, connectionID, request)
	default:
		return events.APIGatewayProxyResponse{StatusCode: 400, Body: "Unknown route"}, nil
	}
}

func handleConnect(ctx context.Context, connectionID string) (events.APIGatewayProxyResponse, error) {
	// Store connection ID in DynamoDB
	_, err := ddbClient.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(tableName),
		Item: map[string]types.AttributeValue{
			"connection_id": &types.AttributeValueMemberS{Value: connectionID},
		},
	})
	if err != nil {
		fmt.Printf("Failed to store connection: %v\n", err)
		return events.APIGatewayProxyResponse{StatusCode: 500}, err
	}

	fmt.Printf("Connected: %s\n", connectionID)
	return events.APIGatewayProxyResponse{StatusCode: 200, Body: "Connected"}, nil
}

func handleDisconnect(ctx context.Context, connectionID string) (events.APIGatewayProxyResponse, error) {
	// Remove connection ID from DynamoDB
	_, err := ddbClient.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(tableName),
		Key: map[string]types.AttributeValue{
			"connection_id": &types.AttributeValueMemberS{Value: connectionID},
		},
	})
	if err != nil {
		fmt.Printf("Failed to remove connection: %v\n", err)
		return events.APIGatewayProxyResponse{StatusCode: 500}, err
	}

	fmt.Printf("Disconnected: %s\n", connectionID)
	return events.APIGatewayProxyResponse{StatusCode: 200, Body: "Disconnected"}, nil
}

func handleDefault(ctx context.Context, connectionID string, request events.APIGatewayWebsocketProxyRequest) (events.APIGatewayProxyResponse, error) {
	// Echo the message back to the sender
	message := request.Body
	fmt.Printf("Received from %s: %s\n", connectionID, message)

	// Build the API Gateway Management API endpoint
	domainName := request.RequestContext.DomainName
	stage := request.RequestContext.Stage
	endpoint := fmt.Sprintf("https://%s/%s", domainName, stage)

	cfg, _ := config.LoadDefaultConfig(ctx)
	apiClient := apigatewaymanagementapi.NewFromConfig(cfg, func(o *apigatewaymanagementapi.Options) {
		o.BaseEndpoint = aws.String(endpoint)
	})

	// Prepare echo response
	response, _ := json.Marshal(map[string]string{
		"echo":          message,
		"connection_id": connectionID,
		"message":       "Server received your message",
	})

	// Send message back to the client
	_, err := apiClient.PostToConnection(ctx, &apigatewaymanagementapi.PostToConnectionInput{
		ConnectionId: aws.String(connectionID),
		Data:         response,
	})
	if err != nil {
		fmt.Printf("Failed to send message: %v\n", err)
		return events.APIGatewayProxyResponse{StatusCode: 500}, err
	}

	return events.APIGatewayProxyResponse{StatusCode: 200, Body: "Message sent"}, nil
}

func main() {
	lambda.Start(handler)
}
```

## Step 2 -- Create the Terraform Project Files

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
  default     = "websocket-demo"
}
```

### `build.tf`

```hcl
resource "null_resource" "go_build" {
  triggers = { source_hash = filebase64sha256("${path.module}/main.go") }
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

### `database.tf`

```hcl
# -- DynamoDB for connection management --
resource "aws_dynamodb_table" "connections" {
  name         = "${var.project_name}-connections"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "connection_id"

  attribute {
    name = "connection_id"
    type = "S"
  }

  tags = { Name = "${var.project_name}-connections" }
}
```

### `iam.tf`

```hcl
data "aws_iam_policy_document" "lambda_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals { type = "Service"; identifiers = ["lambda.amazonaws.com"] }
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

data "aws_iam_policy_document" "lambda_permissions" {
  statement {
    sid = "DynamoDB"
    actions = [
      "dynamodb:PutItem",
      "dynamodb:DeleteItem",
      "dynamodb:Scan",
    ]
    resources = [aws_dynamodb_table.connections.arn]
  }
  statement {
    sid       = "ManageConnections"
    actions   = ["execute-api:ManageConnections"]
    resources = ["arn:aws:execute-api:*:*:*/@connections/*"]
  }
}

resource "aws_iam_role_policy" "lambda" {
  name   = "${var.project_name}-lambda-permissions"
  role   = aws_iam_role.this.id
  policy = data.aws_iam_policy_document.lambda_permissions.json
}
```

### `lambda.tf`

```hcl
# -- Lambda Function (handles all three routes) --
resource "aws_lambda_function" "this" {
  function_name    = "${var.project_name}-handler"
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.this.arn
  memory_size      = 256
  timeout          = 30

  environment {
    variables = {
      TABLE_NAME = aws_dynamodb_table.connections.name
    }
  }

  depends_on = [aws_iam_role_policy_attachment.basic, aws_cloudwatch_log_group.this]
}

# -- Lambda Permission for API Gateway --
resource "aws_lambda_permission" "apigw" {
  statement_id  = "AllowAPIGatewayInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.this.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.websocket.execution_arn}/*/*"
}
```

### `monitoring.tf`

```hcl
resource "aws_cloudwatch_log_group" "this" {
  name              = "/aws/lambda/${var.project_name}-handler"
  retention_in_days = 1
}
```

### `api.tf`

```hcl
# -- WebSocket API --
resource "aws_apigatewayv2_api" "websocket" {
  name                       = var.project_name
  protocol_type              = "WEBSOCKET"
  route_selection_expression = "$request.body.action"
}

# -- Integrations (one Lambda for all routes) --
resource "aws_apigatewayv2_integration" "this" {
  api_id             = aws_apigatewayv2_api.websocket.id
  integration_type   = "AWS_PROXY"
  integration_uri    = aws_lambda_function.this.invoke_arn
  integration_method = "POST"
}

# -- Routes --
resource "aws_apigatewayv2_route" "connect" {
  api_id    = aws_apigatewayv2_api.websocket.id
  route_key = "$connect"
  target    = "integrations/${aws_apigatewayv2_integration.this.id}"
}

resource "aws_apigatewayv2_route" "disconnect" {
  api_id    = aws_apigatewayv2_api.websocket.id
  route_key = "$disconnect"
  target    = "integrations/${aws_apigatewayv2_integration.this.id}"
}

resource "aws_apigatewayv2_route" "default" {
  api_id    = aws_apigatewayv2_api.websocket.id
  route_key = "$default"
  target    = "integrations/${aws_apigatewayv2_integration.this.id}"
}

# -- Stage (auto-deploy) --
resource "aws_apigatewayv2_stage" "production" {
  api_id      = aws_apigatewayv2_api.websocket.id
  name        = "production"
  auto_deploy = true
}
```

### `outputs.tf`

```hcl
output "websocket_url" { value = aws_apigatewayv2_stage.production.invoke_url }
output "function_name" { value = aws_lambda_function.this.function_name }
output "table_name"    { value = aws_dynamodb_table.connections.name }
output "api_id"        { value = aws_apigatewayv2_api.websocket.id }
```

## Step 3 -- Build and Apply

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init
terraform apply -auto-approve
```

### Intermediate Verification

Check the WebSocket API exists:

```bash
aws apigatewayv2 get-api --api-id $(terraform output -raw api_id) \
  --query "{Name:Name,Protocol:ProtocolType,RouteSelection:RouteSelectionExpression}" --output json
```

Expected: `ProtocolType: "WEBSOCKET"`, `RouteSelectionExpression: "$request.body.action"`

## Step 4 -- Test with wscat

Connect to the WebSocket API:

```bash
WS_URL=$(terraform output -raw websocket_url)
wscat -c "$WS_URL"
```

Once connected, send a message:

```
> {"action": "sendMessage", "message": "Hello WebSocket!"}
```

Expected response:

```json
{"echo":"{\"action\":\"sendMessage\",\"message\":\"Hello WebSocket!\"}","connection_id":"abc123=","message":"Server received your message"}
```

Type `Ctrl+C` to disconnect.

## Step 5 -- Verify Connection Management in DynamoDB

While a WebSocket client is connected:

```bash
aws dynamodb scan --table-name websocket-demo-connections \
  --query "Items[*].connection_id.S" --output json
```

Expected: an array containing the active connection ID.

After disconnecting:

```bash
aws dynamodb scan --table-name websocket-demo-connections \
  --query "Count" --output text
```

Expected: `0` (connection removed on $disconnect).

## Common Mistakes

### 1. Forgetting the execute-api:ManageConnections permission

To send messages back to WebSocket clients, the Lambda function calls the API Gateway Management API (`PostToConnection`). This requires `execute-api:ManageConnections` permission.

**Wrong -- missing permission:**

```hcl
# IAM policy only has DynamoDB permissions, not execute-api
```

**What happens:** The Lambda function processes messages but cannot send responses back to the client. The `PostToConnection` call fails with `AccessDeniedException`.

**Fix -- add execute-api permission:**

```hcl
statement {
  actions   = ["execute-api:ManageConnections"]
  resources = ["arn:aws:execute-api:*:*:*/@connections/*"]
}
```

### 2. Using the wrong API endpoint for PostToConnection

The API Gateway Management API endpoint must match the deployed stage. Constructing it incorrectly causes `GoneException` or DNS resolution failures.

**Wrong -- hardcoded endpoint:**

```go
endpoint := "https://abc123.execute-api.us-east-1.amazonaws.com/production"
```

**Fix -- construct from the request context:**

```go
domainName := request.RequestContext.DomainName
stage := request.RequestContext.Stage
endpoint := fmt.Sprintf("https://%s/%s", domainName, stage)
```

### 3. Not handling stale connections

Clients can disconnect unexpectedly (network issues, browser close). The `$disconnect` route may not fire. When you try to `PostToConnection` with a stale connection ID, you get `GoneException`. Always handle this error and clean up the DynamoDB record.

## Verify What You Learned

```bash
aws apigatewayv2 get-routes --api-id $(terraform output -raw api_id) \
  --query "Items[*].RouteKey" --output json
```

Expected: `["$connect", "$default", "$disconnect"]`

```bash
aws lambda get-function-configuration --function-name websocket-demo-handler \
  --query "Environment.Variables.TABLE_NAME" --output text
```

Expected: `websocket-demo-connections`

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

Destroy all resources:

```bash
terraform destroy -auto-approve
```

Verify:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

You built a WebSocket API with connection management. In the next exercise, you will configure **API Gateway custom domains with base path mappings**, enabling you to host multiple API versions under a single domain name.

## Summary

- **WebSocket APIs** maintain persistent, bidirectional connections between client and server
- Three predefined routes: **$connect** (connection opened), **$disconnect** (connection closed), **$default** (catch-all for messages)
- **Custom routes** use `route_selection_expression` (e.g., `$request.body.action`) to route messages to specific integrations
- Store **connection IDs** in DynamoDB on `$connect`; remove on `$disconnect`
- Use the **API Gateway Management API** (`PostToConnection`) to push messages from server to client
- The Management API endpoint is constructed from `{domainName}/{stage}` available in the request context
- Lambda needs **`execute-api:ManageConnections`** permission to call `PostToConnection`
- Handle **stale connections** (`GoneException`) by removing dead connection IDs from DynamoDB
- WebSocket API pricing: **$1.00/million connection minutes** + **$1.00/million messages**

## Reference

- [API Gateway WebSocket APIs](https://docs.aws.amazon.com/apigateway/latest/developerguide/apigateway-websocket-api.html)
- [WebSocket Route Selection](https://docs.aws.amazon.com/apigateway/latest/developerguide/apigateway-websocket-api-selection-expressions.html)
- [API Gateway Management API](https://docs.aws.amazon.com/apigateway/latest/developerguide/apigateway-how-to-call-websocket-api-connections.html)
- [Terraform aws_apigatewayv2_api](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/apigatewayv2_api)

## Additional Resources

- [WebSocket API Tutorials](https://docs.aws.amazon.com/apigateway/latest/developerguide/apigateway-websocket-api-overview.html) -- step-by-step guide for building chat applications
- [Connection Management Patterns](https://docs.aws.amazon.com/apigateway/latest/developerguide/apigateway-websocket-api-mapping-template-reference.html) -- advanced connection handling and message transformation
- [WebSocket API Quotas](https://docs.aws.amazon.com/apigateway/latest/developerguide/limits.html#apigateway-account-level-limits-table) -- connection limits, message size limits, and idle timeout (10 minutes)
- [wscat Tool](https://github.com/websockets/wscat) -- command-line WebSocket client for testing

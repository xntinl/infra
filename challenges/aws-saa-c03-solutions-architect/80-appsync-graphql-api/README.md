# 80. AppSync GraphQL API

<!--
difficulty: intermediate
concepts: [appsync, graphql, schema-definition, resolvers, dynamodb-resolver, lambda-resolver, vtl-mapping-templates, subscriptions, real-time, graphql-vs-rest, caching, authorization-modes]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: apply, analyze
prerequisites: [55-dynamodb-capacity-modes-throttling, 77-lambda-event-sources-patterns]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** AppSync: 250,000 free queries/month, then $4.00 per million. Real-time subscriptions: $2.00 per million connection minutes. DynamoDB on-demand: pennies. Total ~$0.01/hr. Remember to run `terraform destroy` when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Completed exercise 55 (DynamoDB basics) | DynamoDB table creation |
| Understanding of REST API patterns | Exercise 78 |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Implement** an AppSync GraphQL API with schema, resolvers, and DynamoDB data source.
2. **Analyze** when GraphQL (AppSync) is a better fit than REST (API Gateway) based on query flexibility and data shape.
3. **Apply** resolver mapping templates to connect GraphQL operations to DynamoDB tables.
4. **Evaluate** AppSync authorization modes: API key, Cognito, IAM, OIDC, Lambda.
5. **Design** real-time data synchronization using GraphQL subscriptions.

---

## Why This Matters

AppSync appears on the SAA-C03 exam as the answer to specific patterns: "the mobile application needs to query flexible data shapes," "the application needs real-time updates when data changes," or "the frontend team wants to reduce over-fetching of data from multiple microservices." The exam does not test GraphQL syntax deeply, but it tests whether you know when AppSync is the right choice.

The key architectural comparison is REST (API Gateway) versus GraphQL (AppSync). REST APIs return fixed response shapes -- if a client needs only the user's name but the `/users/123` endpoint returns all 50 fields, the client receives all 50 fields (over-fetching). If the client needs data from both `/users/123` and `/users/123/orders`, it makes two requests (under-fetching). GraphQL solves both: the client specifies exactly which fields it wants in a single query, and the server returns exactly that.

AppSync adds real-time subscriptions, which are GraphQL operations that push data to connected clients when mutations occur. This is AppSync's killer feature for mobile and web applications: when one user creates an order, all connected clients subscribed to order updates receive the new order automatically without polling. The exam tests this pattern as an alternative to WebSocket API Gateway.

The resolver layer is where AppSync connects to data sources. DynamoDB resolvers use VTL (Velocity Template Language) mapping templates to translate GraphQL operations into DynamoDB operations. Lambda resolvers delegate to Lambda functions for complex logic. HTTP resolvers can call any HTTP endpoint. The exam tests whether you understand this data source flexibility.

---

## Building Blocks

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
  default     = "saa-ex80"
}
```

### `main.tf`

```hcl
# ============================================================
# TODO 1: DynamoDB Table
# ============================================================
# Create a DynamoDB table for the GraphQL API's data store.
#
# Requirements:
#   - name = "${var.project_name}-items"
#   - billing_mode = "PAY_PER_REQUEST"
#   - hash_key = "id" (String)
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/dynamodb_table
# ============================================================


# ============================================================
# TODO 2: AppSync GraphQL API
# ============================================================
# Create an AppSync GraphQL API with API key authentication.
#
# Requirements:
#   - Resource: aws_appsync_graphql_api
#   - name = "${var.project_name}-api"
#   - authentication_type = "API_KEY"
#   - schema (see below):
#
# GraphQL Schema:
#   type Item {
#     id: ID!
#     name: String!
#     description: String
#     price: Float!
#     category: String!
#     createdAt: String
#   }
#
#   input CreateItemInput {
#     name: String!
#     description: String
#     price: Float!
#     category: String!
#   }
#
#   type Query {
#     getItem(id: ID!): Item
#     listItems: [Item]
#   }
#
#   type Mutation {
#     createItem(input: CreateItemInput!): Item
#   }
#
#   type Subscription {
#     onCreateItem: Item
#       @aws_subscribe(mutations: ["createItem"])
#   }
#
# Create an API key:
#   - Resource: aws_appsync_api_key
#   - expires = timeadd(timestamp(), "24h")
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/appsync_graphql_api
# ============================================================


# ============================================================
# TODO 3: DynamoDB Data Source and IAM Role
# ============================================================
# Connect AppSync to DynamoDB.
#
# Requirements:
#   - IAM role for AppSync to access DynamoDB
#     - Assume role policy: appsync.amazonaws.com
#     - Policy: dynamodb:GetItem, PutItem, Scan on the table
#   - Resource: aws_appsync_datasource
#     - api_id = AppSync API id
#     - name = "DynamoDBSource"
#     - type = "AMAZON_DYNAMODB"
#     - dynamodb_config { table_name, region }
#     - service_role_arn = IAM role ARN
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/appsync_datasource
# ============================================================


# ============================================================
# TODO 4: Resolvers (Mapping Templates)
# ============================================================
# Create resolvers that map GraphQL operations to DynamoDB.
#
# Resolver 1: getItem (Query)
#   Request template:
#     { "version": "2017-02-28",
#       "operation": "GetItem",
#       "key": { "id": { "S": "$ctx.args.id" } } }
#   Response template:
#     $util.toJson($ctx.result)
#
# Resolver 2: listItems (Query)
#   Request template:
#     { "version": "2017-02-28",
#       "operation": "Scan" }
#   Response template:
#     $util.toJson($ctx.result.items)
#
# Resolver 3: createItem (Mutation)
#   Request template:
#     { "version": "2017-02-28",
#       "operation": "PutItem",
#       "key": { "id": { "S": "$util.autoId()" } },
#       "attributeValues": {
#         "name": { "S": "$ctx.args.input.name" },
#         "price": { "N": $ctx.args.input.price },
#         "category": { "S": "$ctx.args.input.category" },
#         "createdAt": { "S": "$util.time.nowISO8601()" }
#       } }
#   Response template:
#     $util.toJson($ctx.result)
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/appsync_resolver
# ============================================================
```

### `outputs.tf`

```hcl
output "graphql_url" {
  value = "Set after TODO 2 implementation"
}

output "api_key" {
  value     = "Set after TODO 2 implementation"
  sensitive = true
}
```

---

## REST vs GraphQL Decision Framework

| Criterion | REST (API Gateway) | GraphQL (AppSync) |
|---|---|---|
| **Response shape** | Fixed (server decides) | Flexible (client decides) |
| **Over-fetching** | Common (returns all fields) | None (client requests specific fields) |
| **Under-fetching** | Common (multiple endpoints needed) | None (single query spans types) |
| **Real-time** | WebSocket API (separate) | Subscriptions (built-in) |
| **Caching** | API Gateway caching, CloudFront | AppSync caching, client-side cache |
| **Offline** | Custom implementation | AWS Amplify DataStore |
| **Multiple data sources** | Lambda orchestration | Resolver per field |
| **API versioning** | URL versioning (/v1, /v2) | Schema evolution (additive) |
| **Learning curve** | Low (HTTP verbs) | Higher (schema, queries, resolvers) |
| **Tooling** | Ubiquitous | Growing (Apollo, Amplify) |

### When to Choose AppSync

| Scenario | Why AppSync |
|---|---|
| Mobile app with varying data needs | Clients query only needed fields |
| Real-time dashboard | Subscriptions push updates automatically |
| Aggregation from multiple backends | Multiple data sources behind one API |
| Offline-first mobile app | Amplify DataStore + AppSync sync |

### When to Stay with API Gateway

| Scenario | Why API Gateway |
|---|---|
| Simple CRUD with fixed schemas | REST is simpler, cheaper |
| Third-party API with usage plans | API keys + throttling on REST only |
| File upload/download | REST handles binary better |
| Existing REST ecosystem | Migration cost outweighs benefit |

---

## Spot the Bug

The following AppSync resolver mapping template for a `listItems` query returns an error. Identify the problem before expanding the answer.

```json
// Request mapping template
{
  "version": "2017-02-28",
  "operation": "Scan"
}
```

```json
// Response mapping template
$util.toJson($ctx.result)
```

The query returns an error: `Cannot return null for non-nullable type [Item]`.

<details>
<summary>Explain the bug</summary>

**The response mapping template returns the wrong format for a list query.** When DynamoDB performs a `Scan` operation, `$ctx.result` contains a wrapper object with metadata:

```json
{
  "items": [...],
  "nextToken": "...",
  "scannedCount": 10
}
```

The resolver returns the entire wrapper object, but the GraphQL schema expects `listItems` to return `[Item]` (an array of items). The wrapper object is not an array, so AppSync cannot map it to the expected type.

**Fix:** Return `$ctx.result.items` instead of `$ctx.result`:

```
$util.toJson($ctx.result.items)
```

This is one of the most common AppSync resolver mistakes. Different DynamoDB operations return results in different shapes:
- `GetItem`: `$ctx.result` is the item directly
- `PutItem`: `$ctx.result` is the item directly
- `Scan` / `Query`: `$ctx.result.items` is the array of items
- `DeleteItem`: `$ctx.result` is the deleted item

Always match the response template to the DynamoDB operation's result shape and the GraphQL return type.

</details>

---

## Verify What You Learned

1. **Deploy the infrastructure:**
   ```bash
   terraform init && terraform apply -auto-approve
   ```

2. **Get API details:**
   ```bash
   GRAPHQL_URL=$(terraform output -raw graphql_url)
   API_KEY=$(terraform output -raw api_key)
   ```

3. **Create an item:**
   ```bash
   curl -s -X POST "$GRAPHQL_URL" \
     -H "Content-Type: application/json" \
     -H "x-api-key: $API_KEY" \
     -d '{"query":"mutation { createItem(input: {name: \"Widget\", price: 29.99, category: \"electronics\"}) { id name price category createdAt }}"}' | jq .
   ```
   Expected: Created item with auto-generated ID and timestamp.

4. **List items:**
   ```bash
   curl -s -X POST "$GRAPHQL_URL" \
     -H "Content-Type: application/json" \
     -H "x-api-key: $API_KEY" \
     -d '{"query":"{ listItems { id name price } }"}' | jq .
   ```
   Expected: Array of items with only `id`, `name`, and `price` fields (no over-fetching).

5. **Get a specific item:**
   ```bash
   ITEM_ID="<id-from-step-3>"
   curl -s -X POST "$GRAPHQL_URL" \
     -H "Content-Type: application/json" \
     -H "x-api-key: $API_KEY" \
     -d "{\"query\":\"{ getItem(id: \\\"${ITEM_ID}\\\") { id name description price category createdAt }}\"}" | jq .
   ```
   Expected: Single item with all requested fields.

6. **Terraform state consistency:**
   ```bash
   terraform plan
   ```
   Expected: `No changes. Your infrastructure matches the configuration.`

---

## Solutions

<details>
<summary>main.tf -- TODO 1: DynamoDB Table</summary>

```hcl
resource "aws_dynamodb_table" "items" {
  name         = "${var.project_name}-items"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "id"

  attribute {
    name = "id"
    type = "S"
  }

  tags = { Name = "${var.project_name}-items" }
}
```

</details>

<details>
<summary>main.tf -- TODO 2: AppSync GraphQL API</summary>

```hcl
resource "aws_appsync_graphql_api" "this" {
  name                = "${var.project_name}-api"
  authentication_type = "API_KEY"

  schema = <<-SCHEMA
    type Item {
      id: ID!
      name: String!
      description: String
      price: Float!
      category: String!
      createdAt: String
    }

    input CreateItemInput {
      name: String!
      description: String
      price: Float!
      category: String!
    }

    type Query {
      getItem(id: ID!): Item
      listItems: [Item]
    }

    type Mutation {
      createItem(input: CreateItemInput!): Item
    }

    type Subscription {
      onCreateItem: Item
        @aws_subscribe(mutations: ["createItem"])
    }
  SCHEMA

  tags = { Name = "${var.project_name}-api" }
}

resource "aws_appsync_api_key" "this" {
  api_id  = aws_appsync_graphql_api.this.id
  expires = timeadd(timestamp(), "168h")

  lifecycle {
    ignore_changes = [expires]
  }
}
```

The `@aws_subscribe` directive connects the subscription to the mutation. When `createItem` is called, AppSync automatically pushes the result to all WebSocket clients subscribed to `onCreateItem`.

</details>

<details>
<summary>iam.tf -- TODO 3: DynamoDB Data Source</summary>

```hcl
resource "aws_iam_role" "appsync" {
  name = "${var.project_name}-appsync-role"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action    = "sts:AssumeRole"
      Effect    = "Allow"
      Principal = { Service = "appsync.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy" "appsync_dynamodb" {
  name = "${var.project_name}-dynamodb-access"
  role = aws_iam_role.appsync.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = [
        "dynamodb:GetItem",
        "dynamodb:PutItem",
        "dynamodb:Scan",
        "dynamodb:Query",
        "dynamodb:DeleteItem",
        "dynamodb:UpdateItem"
      ]
      Resource = aws_dynamodb_table.items.arn
    }]
  })
}

resource "aws_appsync_datasource" "dynamodb" {
  api_id           = aws_appsync_graphql_api.this.id
  name             = "DynamoDBSource"
  type             = "AMAZON_DYNAMODB"
  service_role_arn = aws_iam_role.appsync.arn

  dynamodb_config {
    table_name = aws_dynamodb_table.items.name
    region     = var.region
  }
}
```

</details>

<details>
<summary>main.tf -- TODO 4: Resolvers</summary>

```hcl
resource "aws_appsync_resolver" "get_item" {
  api_id      = aws_appsync_graphql_api.this.id
  type        = "Query"
  field       = "getItem"
  data_source = aws_appsync_datasource.dynamodb.name

  request_template = <<-VTL
    {
      "version": "2017-02-28",
      "operation": "GetItem",
      "key": {
        "id": $util.dynamodb.toDynamoDBJson($ctx.args.id)
      }
    }
  VTL

  response_template = "$util.toJson($ctx.result)"
}

resource "aws_appsync_resolver" "list_items" {
  api_id      = aws_appsync_graphql_api.this.id
  type        = "Query"
  field       = "listItems"
  data_source = aws_appsync_datasource.dynamodb.name

  request_template = <<-VTL
    {
      "version": "2017-02-28",
      "operation": "Scan"
    }
  VTL

  response_template = "$util.toJson($ctx.result.items)"
}

resource "aws_appsync_resolver" "create_item" {
  api_id      = aws_appsync_graphql_api.this.id
  type        = "Mutation"
  field       = "createItem"
  data_source = aws_appsync_datasource.dynamodb.name

  request_template = <<-VTL
    {
      "version": "2017-02-28",
      "operation": "PutItem",
      "key": {
        "id": $util.dynamodb.toDynamoDBJson($util.autoId())
      },
      "attributeValues": $util.dynamodb.toMapValuesJson({
        "name": $ctx.args.input.name,
        "description": $ctx.args.input.description,
        "price": $ctx.args.input.price,
        "category": $ctx.args.input.category,
        "createdAt": "$util.time.nowISO8601()"
      })
    }
  VTL

  response_template = "$util.toJson($ctx.result)"
}
```

Key VTL utilities:
- `$util.autoId()` -- generates a UUID
- `$util.dynamodb.toDynamoDBJson()` -- converts values to DynamoDB JSON format
- `$util.dynamodb.toMapValuesJson()` -- converts a map to DynamoDB attribute values
- `$util.time.nowISO8601()` -- current timestamp in ISO 8601 format
- `$ctx.result.items` -- the items array from a Scan/Query result

</details>

<details>
<summary>outputs.tf -- Updated Outputs</summary>

```hcl
output "graphql_url" {
  value = aws_appsync_graphql_api.this.uris["GRAPHQL"]
}

output "api_key" {
  value     = aws_appsync_api_key.this.key
  sensitive = true
}
```

</details>

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify:

```bash
aws appsync list-graphql-apis \
  --query 'graphqlApis[?name==`saa-ex80-api`]'
```

Expected: Empty list.

---

## What's Next

Exercise 81 covers **EventBridge for event-driven architecture**, where you will create custom event buses, content-based filtering rules, and input transformations. EventBridge provides the choreography counterpart to Step Functions' orchestration -- loosely coupled services communicating through events rather than centrally managed workflows.

---

## Summary

- **AppSync** is a managed GraphQL API service that solves over-fetching and under-fetching by letting clients specify exactly which data they need
- **GraphQL subscriptions** provide real-time updates via WebSocket -- when a mutation occurs, connected clients receive the data automatically
- **Resolver mapping templates** (VTL) translate GraphQL operations into data source operations (DynamoDB, Lambda, HTTP, RDS, OpenSearch)
- **DynamoDB Scan results** return `$ctx.result.items` (array), not `$ctx.result` (wrapper) -- a common resolver bug
- **Authorization modes**: API_KEY (testing), COGNITO_USER_POOLS (user auth), AWS_IAM (service auth), OPENID_CONNECT (third-party), LAMBDA (custom)
- **AppSync caching** adds server-side response caching to reduce resolver invocations (like API Gateway caching for REST)
- **Choose AppSync** when clients need flexible queries, real-time updates, or data from multiple sources in one request
- **Choose API Gateway** when you need simple REST, usage plans, WAF integration, or request validation
- **Amplify DataStore** integrates with AppSync for offline-first mobile applications with automatic conflict resolution

## Reference

- [AWS AppSync Developer Guide](https://docs.aws.amazon.com/appsync/latest/devguide/welcome.html)
- [AppSync Resolver Mapping Templates](https://docs.aws.amazon.com/appsync/latest/devguide/resolver-mapping-template-reference.html)
- [Terraform aws_appsync_graphql_api](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/appsync_graphql_api)
- [AppSync Pricing](https://aws.amazon.com/appsync/pricing/)

## Additional Resources

- [DynamoDB Resolver Reference](https://docs.aws.amazon.com/appsync/latest/devguide/resolver-mapping-template-reference-dynamodb.html) -- complete VTL reference for DynamoDB operations
- [AppSync JavaScript Resolvers](https://docs.aws.amazon.com/appsync/latest/devguide/resolver-reference-js-version.html) -- newer alternative to VTL using JavaScript
- [AppSync Real-Time Subscriptions](https://docs.aws.amazon.com/appsync/latest/devguide/real-time-data.html) -- WebSocket-based push notifications
- [AppSync Caching](https://docs.aws.amazon.com/appsync/latest/devguide/enabling-caching.html) -- server-side caching to reduce data source calls

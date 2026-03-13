# 4. Cognito User Pool with API Gateway Authorization

<!--
difficulty: intermediate
concepts: [cognito-user-pool, cognito-app-client, api-gateway-authorizer, lambda-authorizer, jwt-tokens, oauth2]
tools: [terraform, aws-cli]
estimated_time: 50m
bloom_level: design, justify, implement
prerequisites: [none]
aws_cost: ~$0.02/hr
-->

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.5 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| curl or httpie installed | `curl --version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Design** a Cognito User Pool with appropriate password policies and an App Client for machine-to-machine authentication
2. **Justify** when to use a Cognito authorizer versus a Lambda authorizer versus IAM authorization on API Gateway
3. **Implement** a REST API Gateway with a Cognito User Pool authorizer that validates JWT tokens
4. **Configure** a Lambda authorizer as an alternative authorization strategy with custom token validation logic
5. **Differentiate** between ID tokens, access tokens, and refresh tokens in the Cognito authentication flow

## Why This Matters

Every production API needs authentication and authorization. AWS Cognito User Pools provide a fully managed identity provider that handles user sign-up, sign-in, MFA, and token issuance without you writing any auth code. When you pair Cognito with API Gateway's built-in Cognito authorizer, the gateway validates JWT tokens before your Lambda function even executes -- rejecting unauthorized requests at the edge with zero custom code. This is the fastest path to securing an API on AWS.

Lambda authorizers give you a second option when you need custom logic -- validating tokens from a third-party IdP, checking a database for revoked sessions, or implementing fine-grained ABAC policies that go beyond what Cognito claims provide. The DVA-C02 exam tests your ability to choose the right authorizer type for a given scenario: Cognito authorizers for simple JWT validation against your own User Pool, Lambda authorizers for custom or third-party token validation, and IAM authorization for service-to-service calls that already have AWS credentials. Understanding all three patterns and their trade-offs is essential.

## Building Blocks

Create the following project files. Your job is to fill in each `# TODO` block.

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
  default     = "cognito-api-lab"
}
```

### `cognito.tf`

```hcl
# -------------------------------------------------------
# Cognito User Pool
# -------------------------------------------------------
resource "aws_cognito_user_pool" "this" {
  name = "${var.project_name}-pool"

  password_policy {
    minimum_length    = 8
    require_lowercase = true
    require_numbers   = true
    require_symbols   = false
    require_uppercase = true
  }

  auto_verified_attributes = ["email"]

  schema {
    attribute_data_type = "String"
    name                = "email"
    required            = true
    mutable             = true

    string_attribute_constraints {
      min_length = 5
      max_length = 128
    }
  }

  tags = {
    Name = "${var.project_name}-pool"
  }
}

# -------------------------------------------------------
# Cognito App Client
# -------------------------------------------------------
resource "aws_cognito_user_pool_client" "this" {
  name         = "${var.project_name}-client"
  user_pool_id = aws_cognito_user_pool.this.id

  explicit_auth_flows = [
    "ALLOW_USER_PASSWORD_AUTH",
    "ALLOW_REFRESH_TOKEN_AUTH",
    "ALLOW_ADMIN_USER_PASSWORD_AUTH"
  ]

  generate_secret = false
}
```

### `iam.tf`

```hcl
# -------------------------------------------------------
# IAM Role for Lambda functions
# -------------------------------------------------------
data "aws_iam_policy_document" "lambda_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "lambda" {
  name               = "${var.project_name}-lambda-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

resource "aws_iam_role_policy_attachment" "lambda_basic" {
  role       = aws_iam_role.lambda.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}
```

### `lambda.tf`

```hcl
# -------------------------------------------------------
# Lambda backend function
# -------------------------------------------------------
# NOTE: For Go Lambdas, build the binary externally and reference the zip.
#   GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
#   zip backend.zip bootstrap
data "archive_file" "backend" {
  type        = "zip"
  output_path = "${path.module}/backend.zip"

  source {
    content  = <<-GO
package main

import (
	"context"
	"encoding/json"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

func handler(ctx context.Context, event events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	claims := event.RequestContext.Authorizer

	email := "unknown"
	if v, ok := claims["claims"]; ok {
		if claimsMap, ok := v.(map[string]interface{}); ok {
			if e, ok := claimsMap["email"]; ok {
				email = e.(string)
			} else if s, ok := claimsMap["sub"]; ok {
				email = s.(string)
			}
		}
	}

	body, _ := json.Marshal(map[string]string{
		"message": "Authenticated successfully",
		"user":    email,
	})

	return events.APIGatewayProxyResponse{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(body),
	}, nil
}

func main() {
	lambda.Start(handler)
}
GO
    filename = "main.go"
  }
}

resource "aws_lambda_function" "backend" {
  function_name    = "${var.project_name}-backend"
  role             = aws_iam_role.lambda.arn
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  filename         = data.archive_file.backend.output_path
  source_code_hash = data.archive_file.backend.output_base64sha256

  tags = {
    Name = "${var.project_name}-backend"
  }
}

# -------------------------------------------------------
# Lambda authorizer function
# -------------------------------------------------------
data "archive_file" "authorizer" {
  type        = "zip"
  output_path = "${path.module}/authorizer.zip"

  source {
    content  = <<-GO
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

func handler(ctx context.Context, event events.APIGatewayCustomAuthorizerRequest) (events.APIGatewayCustomAuthorizerResponse, error) {
	token := event.AuthorizationToken
	methodArn := event.MethodArn

	if !strings.HasPrefix(token, "Bearer ") {
		return generatePolicy("user", "Deny", methodArn), nil
	}

	jwtToken := token[7:]

	parts := strings.Split(jwtToken, ".")
	if len(parts) != 3 {
		return generatePolicy("user", "Deny", methodArn), nil
	}

	// Decode payload without verification (for lab purposes only)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		fmt.Printf("Auth error: %v\n", err)
		return generatePolicy("user", "Deny", methodArn), nil
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		fmt.Printf("Auth error: %v\n", err)
		return generatePolicy("user", "Deny", methodArn), nil
	}

	sub := "unknown"
	if v, ok := claims["sub"]; ok {
		sub = fmt.Sprintf("%v", v)
	}

	return generatePolicy(sub, "Allow", methodArn), nil
}

func generatePolicy(principalID, effect, resource string) events.APIGatewayCustomAuthorizerResponse {
	return events.APIGatewayCustomAuthorizerResponse{
		PrincipalID: principalID,
		PolicyDocument: events.APIGatewayCustomAuthorizerPolicy{
			Version: "2012-10-17",
			Statement: []events.IAMPolicyStatement{
				{
					Action:   []string{"execute-api:Invoke"},
					Effect:   effect,
					Resource: []string{resource},
				},
			},
		},
	}
}

func main() {
	lambda.Start(handler)
}
GO
    filename = "main.go"
  }
}

resource "aws_lambda_function" "authorizer" {
  function_name    = "${var.project_name}-authorizer"
  role             = aws_iam_role.lambda.arn
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  filename         = data.archive_file.authorizer.output_path
  source_code_hash = data.archive_file.authorizer.output_base64sha256

  tags = {
    Name = "${var.project_name}-authorizer"
  }
}
```

### `api.tf`

```hcl
# -------------------------------------------------------
# REST API Gateway skeleton
# -------------------------------------------------------
resource "aws_api_gateway_rest_api" "this" {
  name        = "${var.project_name}-api"
  description = "Cognito-authorized API"

  endpoint_configuration {
    types = ["REGIONAL"]
  }
}

resource "aws_api_gateway_resource" "protected" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  parent_id   = aws_api_gateway_rest_api.this.root_resource_id
  path_part   = "protected"
}

# =======================================================
# TODO 1 -- Cognito User Pool Authorizer
# =======================================================
# Requirements:
#   - Create an aws_api_gateway_authorizer of type "COGNITO_USER_POOLS"
#   - Set the name to "${var.project_name}-cognito-auth"
#   - Reference the REST API
#   - Set provider_arns to the Cognito User Pool ARN (not the ID!)
#   - Set identity_source to "method.request.header.Authorization"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_authorizer


# =======================================================
# TODO 2 -- API Gateway Method with Cognito Authorization
# =======================================================
# Requirements:
#   - Create an aws_api_gateway_method on the "protected" resource
#   - HTTP method = GET
#   - authorization = "COGNITO_USER_POOLS"
#   - authorizer_id = reference to the Cognito authorizer from TODO 1
#   - Create an aws_api_gateway_integration (AWS_PROXY type)
#     pointing to the backend Lambda function
#   - Grant API Gateway permission to invoke the backend Lambda
#     (aws_lambda_permission)
#
# Docs:
#   https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_method
#   https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_integration


# =======================================================
# TODO 3 -- Lambda Authorizer (TOKEN type)
# =======================================================
# Requirements:
#   - Create a second aws_api_gateway_authorizer of type "TOKEN"
#   - Set the name to "${var.project_name}-lambda-auth"
#   - Set authorizer_uri to the invoke ARN of the authorizer Lambda
#   - Set identity_source to "method.request.header.Authorization"
#   - Set authorizer_result_ttl_in_seconds to 300
#   - Grant API Gateway permission to invoke the authorizer Lambda
#     (aws_lambda_permission with principal "apigateway.amazonaws.com")
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_authorizer
# Hint: authorizer_uri format is the Lambda invoke_arn attribute


# =======================================================
# TODO 4 -- Deploy the API
# =======================================================
# Requirements:
#   - Create an aws_api_gateway_deployment that depends on
#     the method and integration from TODO 2
#   - Create an aws_api_gateway_stage named "dev"
#   - Use a triggers block with a redeployment hash so changes
#     force a new deployment
#
# Docs:
#   https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_deployment
#   https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_stage
```

### `outputs.tf`

```hcl
output "user_pool_id" {
  value = aws_cognito_user_pool.this.id
}

output "client_id" {
  value = aws_cognito_user_pool_client.this.id
}

output "api_url" {
  value = "${aws_api_gateway_stage.dev.invoke_url}/protected"
}
```

## Spot the Bug

A colleague configured a Cognito authorizer but API Gateway returns `401 Unauthorized` for every request, even with a valid token. **What is wrong?**

```hcl
resource "aws_api_gateway_authorizer" "cognito" {
  name          = "cognito-authorizer"
  rest_api_id   = aws_api_gateway_rest_api.this.id
  type          = "COGNITO_USER_POOLS"
  provider_arns = [aws_cognito_user_pool.this.id]   # <-- BUG
}
```

<details>
<summary>Explain the bug</summary>

The `provider_arns` attribute requires the **ARN** of the Cognito User Pool, not the **ID**. The User Pool ID looks like `us-east-1_AbCdEfGhI`, while the ARN looks like `arn:aws:cognito-idp:us-east-1:123456789012:userpool/us-east-1_AbCdEfGhI`.

Terraform will accept the ID without error because `provider_arns` is just a list of strings, but API Gateway cannot locate the User Pool at runtime and rejects every token.

The fix:

```hcl
provider_arns = [aws_cognito_user_pool.this.arn]
```

</details>

## Verify What You Learned

### Step 1 -- Apply and create a test user

```
terraform init && terraform apply -auto-approve
```

Create a test user in the Cognito User Pool:

```
aws cognito-idp admin-create-user \
  --user-pool-id $(terraform output -raw user_pool_id) \
  --username testuser@example.com \
  --user-attributes Name=email,Value=testuser@example.com \
  --temporary-password "TempPass1!" \
  --message-action SUPPRESS
```

Set a permanent password:

```
aws cognito-idp admin-set-user-password \
  --user-pool-id $(terraform output -raw user_pool_id) \
  --username testuser@example.com \
  --password "MyPass123!" \
  --permanent
```

### Step 2 -- Obtain a JWT token

```
TOKEN=$(aws cognito-idp admin-initiate-auth \
  --user-pool-id $(terraform output -raw user_pool_id) \
  --client-id $(terraform output -raw client_id) \
  --auth-flow ADMIN_USER_PASSWORD_AUTH \
  --auth-parameters USERNAME=testuser@example.com,PASSWORD="MyPass123!" \
  --query "AuthenticationResult.IdToken" \
  --output text)
echo "Token obtained (first 50 chars): ${TOKEN:0:50}..."
```

### Step 3 -- Call the protected endpoint with the token

```
curl -s -H "Authorization: $TOKEN" \
  "$(terraform output -raw api_url)" | jq .
```

Expected output:

```json
{
    "message": "Authenticated successfully",
    "user": "testuser@example.com"
}
```

### Step 4 -- Verify unauthorized access is rejected

```
curl -s -H "Authorization: invalid-token" \
  "$(terraform output -raw api_url)"
```

Expected output:

```json
{
    "message": "Unauthorized"
}
```

### Step 5 -- Verify the authorizer configuration

```
aws apigateway get-authorizers \
  --rest-api-id $(aws apigateway get-rest-apis \
    --query "items[?name=='cognito-api-lab-api'].id" --output text) \
  --query "items[].{Name:name,Type:type}" \
  --output table
```

Expected output:

```
-----------------------------------------------------
|                  GetAuthorizers                    |
+-------------------------------+-------------------+
|            Name               |      Type         |
+-------------------------------+-------------------+
|  cognito-api-lab-cognito-auth |  COGNITO_USER_POOLS |
|  cognito-api-lab-lambda-auth  |  TOKEN            |
+-------------------------------+-------------------+
```

## Solutions

<details>
<summary>TODO 1 -- Cognito User Pool Authorizer (api.tf)</summary>

```hcl
resource "aws_api_gateway_authorizer" "cognito" {
  name          = "${var.project_name}-cognito-auth"
  rest_api_id   = aws_api_gateway_rest_api.this.id
  type          = "COGNITO_USER_POOLS"
  provider_arns = [aws_cognito_user_pool.this.arn]

  identity_source = "method.request.header.Authorization"
}
```

</details>

<details>
<summary>TODO 2 -- API Gateway Method with Cognito Authorization (api.tf)</summary>

```hcl
resource "aws_api_gateway_method" "protected_get" {
  rest_api_id   = aws_api_gateway_rest_api.this.id
  resource_id   = aws_api_gateway_resource.protected.id
  http_method   = "GET"
  authorization = "COGNITO_USER_POOLS"
  authorizer_id = aws_api_gateway_authorizer.cognito.id
}

resource "aws_api_gateway_integration" "protected_get" {
  rest_api_id             = aws_api_gateway_rest_api.this.id
  resource_id             = aws_api_gateway_resource.protected.id
  http_method             = aws_api_gateway_method.protected_get.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.backend.invoke_arn
}

resource "aws_lambda_permission" "api_gw_backend" {
  statement_id  = "AllowAPIGatewayInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.backend.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.this.execution_arn}/*/*"
}
```

</details>

<details>
<summary>TODO 3 -- Lambda Authorizer (api.tf)</summary>

```hcl
resource "aws_api_gateway_authorizer" "lambda" {
  name                             = "${var.project_name}-lambda-auth"
  rest_api_id                      = aws_api_gateway_rest_api.this.id
  type                             = "TOKEN"
  authorizer_uri                   = aws_lambda_function.authorizer.invoke_arn
  identity_source                  = "method.request.header.Authorization"
  authorizer_result_ttl_in_seconds = 300
}

resource "aws_lambda_permission" "api_gw_authorizer" {
  statement_id  = "AllowAPIGatewayInvokeAuthorizer"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.authorizer.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.this.execution_arn}/authorizers/*"
}
```

</details>

<details>
<summary>TODO 4 -- Deploy the API (api.tf)</summary>

```hcl
resource "aws_api_gateway_deployment" "this" {
  rest_api_id = aws_api_gateway_rest_api.this.id

  triggers = {
    redeployment = sha1(jsonencode([
      aws_api_gateway_resource.protected.id,
      aws_api_gateway_method.protected_get.id,
      aws_api_gateway_integration.protected_get.id,
      aws_api_gateway_authorizer.cognito.id,
    ]))
  }

  lifecycle {
    create_before_destroy = true
  }

  depends_on = [
    aws_api_gateway_method.protected_get,
    aws_api_gateway_integration.protected_get,
  ]
}

resource "aws_api_gateway_stage" "dev" {
  deployment_id = aws_api_gateway_deployment.this.id
  rest_api_id   = aws_api_gateway_rest_api.this.id
  stage_name    = "dev"
}
```

</details>

## Cleanup

Destroy all resources when finished:

```
terraform destroy -auto-approve
```

Delete the test user first if destroy fails on the User Pool:

```
aws cognito-idp admin-delete-user \
  --user-pool-id $(terraform output -raw user_pool_id) \
  --username testuser@example.com
```

## What's Next

In **Exercise 05 -- Lambda Error Handling, Retries, and Dead Letter Queues**, you will configure async invocation retries, dead letter queues, and Lambda Destinations to build resilient event-driven pipelines that gracefully handle failures.

## Summary

You built an API Gateway secured with two authorization strategies:

- **Cognito User Pool authorizer** -- zero-code JWT validation against your own identity provider
- **Lambda authorizer** -- custom token validation logic for third-party tokens or complex policies
- **Token lifecycle** -- created a user, authenticated, obtained an ID token, and used it to call a protected endpoint

The DVA-C02 exam expects you to choose the correct authorizer type given a scenario. Cognito authorizers are cheapest and simplest when you own the User Pool. Lambda authorizers cost more (you pay for the Lambda invocation) but handle any token format.

## Reference

| Resource | Purpose |
|----------|---------|
| `aws_cognito_user_pool` | Managed identity provider |
| `aws_cognito_user_pool_client` | App client for authentication flows |
| `aws_api_gateway_authorizer` | Validates tokens before invoking backend |
| `aws_api_gateway_method` | HTTP method configuration with auth type |
| `aws_lambda_permission` | Grants API Gateway permission to invoke Lambda |
| `aws_api_gateway_deployment` | Immutable snapshot of the API configuration |

## Additional Resources

- [Cognito User Pools Documentation](https://docs.aws.amazon.com/cognito/latest/developerguide/cognito-user-identity-pools.html)
- [API Gateway Authorizers](https://docs.aws.amazon.com/apigateway/latest/developerguide/apigateway-use-lambda-authorizer.html)
- [Cognito Token Types (ID, Access, Refresh)](https://docs.aws.amazon.com/cognito/latest/developerguide/amazon-cognito-user-pools-using-tokens-with-identity-providers.html)
- [Lambda Authorizer Caching](https://docs.aws.amazon.com/apigateway/latest/developerguide/configure-api-gateway-lambda-authorization-with-console.html)
- [DVA-C02 Exam Guide](https://aws.amazon.com/certification/certified-developer-associate/)

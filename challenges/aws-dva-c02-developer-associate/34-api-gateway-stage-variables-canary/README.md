# 34. API Gateway Stage Variables and Canary Deployments

<!--
difficulty: intermediate
concepts: [stage-variables, lambda-aliases, canary-deployment, percent-traffic, stage-variable-lambda-permission, deployment-strategies, stageVariables-context]
tools: [terraform, aws-cli, curl]
estimated_time: 45m
bloom_level: analyze, implement
prerequisites: [18-lambda-versions-aliases-traffic-shifting, 02-api-gateway-rest-vs-http-validation]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a REST API with two Lambda function versions and aliases. Costs are negligible during testing (~$0.01/hr or less). Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| curl installed | `curl --version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Analyze** how stage variables decouple stage configuration from API definition, enabling the same API definition to route to different backends per stage
2. **Implement** stage variable-based Lambda routing where a stage variable points to a Lambda alias and the integration URI uses `${stageVariables.lambdaAlias}`
3. **Configure** canary deployment settings on a REST API stage to gradually shift traffic between the current deployment and a new deployment
4. **Differentiate** between Lambda alias-based traffic shifting (weighted alias) and API Gateway canary deployment (stage-level traffic split)
5. **Debug** the common error where Lambda permission is not granted for stage variable-based invocation

## Why This Matters

Stage variables let you parameterize your API Gateway configuration per stage without duplicating the API definition. A single REST API can have `dev`, `staging`, and `prod` stages, each with a `lambdaAlias` stage variable pointing to a different Lambda alias (`dev`, `staging`, `prod`). The integration URI uses `${stageVariables.lambdaAlias}` to dynamically resolve the target Lambda at request time. This pattern is heavily tested on the DVA-C02 exam because it combines API Gateway stages, Lambda aliases, and IAM permissions.

Canary deployments add a traffic-splitting layer at the API Gateway stage level. When you deploy a new API version, the canary receives a configurable percentage of traffic (for example, 10%) while the remaining 90% continues to use the existing deployment. If the canary shows errors, you roll back without affecting the majority of users. The exam tests the difference between this approach and Lambda alias weighted routing: API Gateway canary splits at the gateway level (different API configurations), while Lambda alias weighting splits at the function level (same API, different function versions).

## Building Blocks

### Lambda Function Code

Create two versions of the function. Create `main.go`:

### `lambda/main.go`

```go
// main.go
package main

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

func handler(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	version := os.Getenv("FUNC_VERSION")
	if version == "" {
		version = "v1"
	}

	body, _ := json.Marshal(map[string]interface{}{
		"version":   version,
		"message":   "Hello from " + version,
		"stage":     req.RequestContext.Stage,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
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
```

### `lambda/go.mod`

```text
module lambda

go 1.21

require github.com/aws/aws-lambda-go v1.47.0
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
  default     = "stage-vars-canary"
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
  timeout          = 10
  publish          = true

  environment {
    variables = { FUNC_VERSION = "v1" }
  }

  depends_on = [aws_iam_role_policy_attachment.basic, aws_cloudwatch_log_group.this]
}

# -- Lambda Aliases --
resource "aws_lambda_alias" "live" {
  name             = "live"
  function_name    = aws_lambda_function.this.function_name
  function_version = aws_lambda_function.this.version
}

resource "aws_lambda_alias" "canary" {
  name             = "canary"
  function_name    = aws_lambda_function.this.function_name
  function_version = aws_lambda_function.this.version
}
```

### `api.tf`

```hcl
resource "aws_api_gateway_rest_api" "this" {
  name = "${var.project_name}-api"
}

resource "aws_api_gateway_resource" "hello" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  parent_id   = aws_api_gateway_rest_api.this.root_resource_id
  path_part   = "hello"
}

resource "aws_api_gateway_method" "get_hello" {
  rest_api_id   = aws_api_gateway_rest_api.this.id
  resource_id   = aws_api_gateway_resource.hello.id
  http_method   = "GET"
  authorization = "NONE"
}

# =======================================================
# TODO 1 -- Integration URI with Stage Variable (api.tf)
# =======================================================
# Requirements:
#   - Create an aws_api_gateway_integration for GET /hello
#   - Set type = "AWS_PROXY"
#   - Set the URI to use a stage variable for the Lambda alias:
#     uri = "arn:aws:apigateway:us-east-1:lambda:path/2015-03-31/functions/${aws_lambda_function.this.arn}:$${stageVariables.lambdaAlias}/invocations"
#   - The double $$ escapes the $ for Terraform so it passes
#     ${stageVariables.lambdaAlias} literally to API Gateway
#
# Docs: https://docs.aws.amazon.com/apigateway/latest/developerguide/aws-api-gateway-stage-variables-reference.html


# =======================================================
# TODO 2 -- Lambda Permissions for Stage Variable Invocation (lambda.tf)
# =======================================================
# Requirements:
#   - Create TWO aws_lambda_permission resources:
#     one for the "live" alias and one for the "canary" alias
#   - The qualifier must match the alias name
#   - Without these, API Gateway gets "Execution failed due to
#     configuration error: Invalid permissions on Lambda function"
#   - source_arn = "${aws_api_gateway_rest_api.this.execution_arn}/*/GET/hello"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_permission


# =======================================================
# TODO 3 -- Deployment + Stage with Stage Variables (api.tf)
# =======================================================
# Requirements:
#   - Create aws_api_gateway_deployment with triggers
#   - Create aws_api_gateway_stage "prod" with:
#     - stage_name = "prod"
#     - variables = { lambdaAlias = "live" }
#   - The stage variable "lambdaAlias" is what gets substituted
#     into the integration URI at request time
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_stage


# =======================================================
# TODO 4 -- Canary Settings on Stage (api.tf)
# =======================================================
# Requirements:
#   - Add a canary_settings block to the stage:
#     - percent_traffic = 10 (10% goes to canary deployment)
#     - stage_variable_overrides = { lambdaAlias = "canary" }
#     - use_stage_cache = false
#   - This means 90% of requests use lambdaAlias = "live"
#     and 10% use lambdaAlias = "canary"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_stage#canary_settings
```

### `outputs.tf`

```hcl
output "api_url" { value = "${aws_api_gateway_stage.prod.invoke_url}/hello" }
output "function_arn" { value = aws_lambda_function.this.arn }
output "live_alias_arn" { value = aws_lambda_alias.live.arn }
output "canary_alias_arn" { value = aws_lambda_alias.canary.arn }
```

## Spot the Bug

A developer set up stage variables to route to different Lambda aliases per stage. The API returns 500 Internal Server Error with "Execution failed due to configuration error: Invalid permissions on Lambda function." The Lambda function works when invoked directly. **What is wrong?**

```hcl
resource "aws_api_gateway_integration" "get_hello" {
  rest_api_id             = aws_api_gateway_rest_api.this.id
  resource_id             = aws_api_gateway_resource.hello.id
  http_method             = aws_api_gateway_method.get_hello.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = "arn:aws:apigateway:us-east-1:lambda:path/2015-03-31/functions/${aws_lambda_function.this.arn}:$${stageVariables.lambdaAlias}/invocations"
}

resource "aws_lambda_permission" "apigw" {
  statement_id  = "AllowAPIGateway"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.this.function_name   # <-- BUG
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.this.execution_arn}/*/GET/hello"
}

resource "aws_api_gateway_stage" "prod" {
  deployment_id = aws_api_gateway_deployment.this.id
  rest_api_id   = aws_api_gateway_rest_api.this.id
  stage_name    = "prod"
  variables     = { lambdaAlias = "live" }
}
```

<details>
<summary>Explain the bug</summary>

The Lambda permission grants API Gateway permission to invoke the **unqualified** function (`function_name` without an alias qualifier). But the integration URI resolves to `function-name:live` (with the alias). Lambda treats `function:alias` as a different resource than the unqualified function, so the permission does not apply.

You need separate Lambda permissions for each alias that might be used via stage variables:

```hcl
resource "aws_lambda_permission" "apigw_live" {
  statement_id  = "AllowAPIGateway-live"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.this.function_name
  qualifier     = "live"
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.this.execution_arn}/*/GET/hello"
}

resource "aws_lambda_permission" "apigw_canary" {
  statement_id  = "AllowAPIGateway-canary"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.this.function_name
  qualifier     = "canary"
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.this.execution_arn}/*/GET/hello"
}
```

The `qualifier` parameter tells Lambda that this permission applies to a specific alias, not the unqualified function. This is one of the most common mistakes when using stage variables with Lambda aliases and is frequently tested on the DVA-C02 exam.

</details>

## Solutions

<details>
<summary>TODO 1 -- Integration URI with Stage Variable</summary>

### `api.tf`

```hcl
resource "aws_api_gateway_integration" "get_hello" {
  rest_api_id             = aws_api_gateway_rest_api.this.id
  resource_id             = aws_api_gateway_resource.hello.id
  http_method             = aws_api_gateway_method.get_hello.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = "arn:aws:apigateway:us-east-1:lambda:path/2015-03-31/functions/${aws_lambda_function.this.arn}:$${stageVariables.lambdaAlias}/invocations"
}
```

</details>

<details>
<summary>TODO 2 -- Lambda Permissions for Stage Variable Invocation</summary>

### `lambda.tf`

```hcl
resource "aws_lambda_permission" "apigw_live" {
  statement_id  = "AllowAPIGateway-live"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.this.function_name
  qualifier     = aws_lambda_alias.live.name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.this.execution_arn}/*/GET/hello"
}

resource "aws_lambda_permission" "apigw_canary" {
  statement_id  = "AllowAPIGateway-canary"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.this.function_name
  qualifier     = aws_lambda_alias.canary.name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.this.execution_arn}/*/GET/hello"
}
```

</details>

<details>
<summary>TODO 3 + TODO 4 -- Stage with Variables and Canary Settings</summary>

### `api.tf`

```hcl
resource "aws_api_gateway_deployment" "this" {
  rest_api_id = aws_api_gateway_rest_api.this.id

  triggers = {
    redeployment = sha1(jsonencode([
      aws_api_gateway_resource.hello.id,
      aws_api_gateway_method.get_hello.id,
      aws_api_gateway_integration.get_hello.id,
    ]))
  }

  lifecycle { create_before_destroy = true }

  depends_on = [aws_api_gateway_integration.get_hello]
}

resource "aws_api_gateway_stage" "prod" {
  deployment_id = aws_api_gateway_deployment.this.id
  rest_api_id   = aws_api_gateway_rest_api.this.id
  stage_name    = "prod"

  variables = {
    lambdaAlias = "live"
  }

  canary_settings {
    percent_traffic          = 10
    stage_variable_overrides = { lambdaAlias = "canary" }
    use_stage_cache          = false
  }
}
```

</details>

## Verify What You Learned

### Step 1 -- Deploy

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init && terraform apply -auto-approve
```

### Step 2 -- Send requests and observe traffic splitting

```bash
API_URL=$(terraform output -raw api_url)

echo "Sending 20 requests to observe canary split..."
for i in $(seq 1 20); do
  VERSION=$(curl -s "$API_URL" | jq -r '.version')
  echo "Request $i: $VERSION"
done
```

Expected: Approximately 18 responses show `v1` (live alias) and 2 show `v1` (canary alias). Both aliases currently point to the same version, but the stage variable override proves the routing works.

### Step 3 -- Verify stage variables

```bash
aws apigateway get-stage \
  --rest-api-id $(terraform output -raw rest_api_id 2>/dev/null || aws apigateway get-rest-apis --query "items[?name=='stage-vars-canary-api'].id" --output text) \
  --stage-name prod \
  --query "{Variables: variables, CanaryPercent: canarySettings.percentTraffic}" \
  --output json
```

Expected: `Variables: {"lambdaAlias": "live"}`, `CanaryPercent: 10.0`

### Step 4 -- Verify no drift

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

```bash
terraform destroy -auto-approve
terraform state list
```

Expected: no output (empty state).

## What's Next

You configured stage variables for Lambda alias routing and canary deployments for gradual traffic shifting. In the next exercise, you will set up **mutual TLS authentication** on API Gateway -- generating CA certificates, client certificates, and configuring a truststore in S3.

## Summary

- **Stage variables** parameterize API Gateway configuration per stage; access them in integration URIs with `${stageVariables.varName}` and in VTL templates with `$stageVariables.varName`
- The integration URI pattern for stage variable Lambda routing: `arn:aws:apigateway:{region}:lambda:path/2015-03-31/functions/{function-arn}:${stageVariables.alias}/invocations`
- **Lambda permissions must include the `qualifier`** matching the alias name; without it, API Gateway gets "Invalid permissions on Lambda function"
- **Canary settings** split traffic at the stage level: `percent_traffic` goes to the canary deployment, `stage_variable_overrides` let the canary use different configuration
- Canary vs Lambda alias weighting: canary splits API Gateway deployments (different API configs), alias weighting splits Lambda versions (same API config, different code)
- Promote a canary by removing `canary_settings` and updating the main deployment; roll back by setting `percent_traffic = 0`

## Reference

- [API Gateway Stage Variables](https://docs.aws.amazon.com/apigateway/latest/developerguide/aws-api-gateway-stage-variables-reference.html)
- [API Gateway Canary Deployments](https://docs.aws.amazon.com/apigateway/latest/developerguide/canary-release.html)
- [Terraform aws_api_gateway_stage](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_stage)
- [Terraform aws_lambda_permission](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_permission)

## Additional Resources

- [Stage Variables for Lambda Integration](https://docs.aws.amazon.com/apigateway/latest/developerguide/stage-variables.html) -- how stage variables resolve in different integration types
- [Canary Release Deployment](https://docs.aws.amazon.com/apigateway/latest/developerguide/canary-release.html) -- canary settings, promotion, and rollback
- [Lambda Alias Traffic Shifting](https://docs.aws.amazon.com/lambda/latest/dg/configuration-aliases.html#configuring-alias-routing) -- weighted aliases for gradual code deployment
- [API Gateway Deployment Best Practices](https://docs.aws.amazon.com/apigateway/latest/developerguide/api-gateway-deployment-best-practices.html) -- strategies for safe API deployments

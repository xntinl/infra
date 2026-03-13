# 64. API Gateway Resource Policies

<!--
difficulty: intermediate
concepts: [api-gateway-resource-policy, ip-whitelist, vpc-endpoint-restriction, cross-account-access, policy-conditions, rest-api-access-control]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: apply, analyze
prerequisites: [02-api-gateway-rest-vs-http-validation]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a REST API Gateway, a Lambda function, and IAM resources. Cost is approximately $0.01/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| jq installed | `jq --version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Implement** an API Gateway resource policy that restricts access by source IP range using `aws:SourceIp` conditions
2. **Configure** VPC endpoint-based restrictions using `aws:SourceVpce` conditions in the resource policy
3. **Analyze** how API Gateway resource policies interact with IAM authentication and Lambda authorizers
4. **Apply** cross-account access rules that allow specific AWS accounts to invoke the API
5. **Differentiate** between API Gateway resource policies (who can call the API) and Lambda resource-based policies (who can invoke the function)

## Why API Gateway Resource Policies

API Gateway resource policies are JSON policy documents attached to a REST API that control which principals can invoke the API. They are evaluated before any other authorization mechanism (IAM, Cognito, Lambda authorizers). This makes them the first line of defense for API access control.

Three primary use cases:

1. **IP whitelisting**: restrict API access to specific CIDR ranges (corporate network, partner IPs). The condition key `aws:SourceIp` matches the caller's IP address.

2. **VPC endpoint restriction**: for private APIs, restrict access to specific VPC endpoints using `aws:SourceVpce`. This ensures only traffic from your VPC can reach the API.

3. **Cross-account access**: allow principals from other AWS accounts to invoke the API. The resource policy grants `execute-api:Invoke` to the external account's root or specific roles.

The DVA-C02 exam tests a critical interaction: if a REST API has a resource policy with only Deny statements and no Allow statements, **all requests are denied** because the default is implicit deny. A resource policy with an explicit Deny for certain IPs but no explicit Allow for other IPs blocks everyone. You must include an Allow statement for the principals you want to grant access to.

## Building Blocks

Create the following files in your exercise directory. Your job is to fill in each `# TODO` block.

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
  default     = "apigw-resource-policy-demo"
}

variable "allowed_cidr" {
  description = "CIDR range allowed to access the API (set to your IP for testing)"
  type        = string
  default     = "0.0.0.0/0"
}
```

### `build.tf`

```hcl
# Build: GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
# main.go: Go Lambda that returns source_ip from APIGatewayProxyRequest context.

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
data "aws_iam_policy_document" "lambda_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals { type = "Service"; identifiers = ["lambda.amazonaws.com"] }
  }
}

resource "aws_iam_role" "lambda" {
  name               = "${var.project_name}-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

resource "aws_iam_role_policy_attachment" "basic" {
  role       = aws_iam_role.lambda.name
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
  role             = aws_iam_role.lambda.arn
  timeout          = 30
  depends_on       = [aws_iam_role_policy_attachment.basic, aws_cloudwatch_log_group.this]
}

resource "aws_lambda_permission" "apigw" {
  statement_id  = "AllowAPIGateway"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.this.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.this.execution_arn}/*/*"
}
```

### `api.tf`

```hcl
# -- REST API Gateway --
resource "aws_api_gateway_rest_api" "this" {
  name = var.project_name

  endpoint_configuration {
    types = ["REGIONAL"]
  }
}

resource "aws_api_gateway_resource" "test" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  parent_id   = aws_api_gateway_rest_api.this.root_resource_id
  path_part   = "test"
}

resource "aws_api_gateway_method" "test_get" {
  rest_api_id   = aws_api_gateway_rest_api.this.id
  resource_id   = aws_api_gateway_resource.test.id
  http_method   = "GET"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "test" {
  rest_api_id             = aws_api_gateway_rest_api.this.id
  resource_id             = aws_api_gateway_resource.test.id
  http_method             = aws_api_gateway_method.test_get.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.this.invoke_arn
}

# =======================================================
# TODO 1 -- API Gateway Resource Policy: IP Whitelist
# =======================================================
# Create a resource policy that:
#   1. ALLOWs execute-api:Invoke from the specified CIDR
#   2. DENYs execute-api:Invoke from all other IPs
#
# Requirements:
#   - Statement 1 (Allow):
#     - Effect: "Allow"
#     - Principal: "*"
#     - Action: "execute-api:Invoke"
#     - Resource: "execute-api:/*"
#     - Condition: aws:SourceIp matches var.allowed_cidr
#   - Statement 2 (Deny):
#     - Effect: "Deny"
#     - Principal: "*"
#     - Action: "execute-api:Invoke"
#     - Resource: "execute-api:/*"
#     - Condition: aws:SourceIp does NOT match var.allowed_cidr
#       (use StringNotEquals or IpAddress/NotIpAddress)
#
# IMPORTANT: You need BOTH Allow and Deny statements.
# With only a Deny, all requests are implicitly denied
# because there is no matching Allow.
#
# Set this policy on the aws_api_gateway_rest_api resource
# using the "policy" attribute.
#
# Docs: https://docs.aws.amazon.com/apigateway/latest/developerguide/apigateway-resource-policies.html


# =======================================================
# TODO 2 -- Deploy the API
# =======================================================
# Create an aws_api_gateway_deployment that depends on
# the method and integration, then create an
# aws_api_gateway_stage named "dev".
#
# The deployment must be redeployed when the resource
# policy changes. Use triggers to force redeployment.
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_deployment
```

### `outputs.tf`

```hcl
output "function_name" { value = aws_lambda_function.this.function_name }
output "api_url"       { value = "${aws_api_gateway_stage.dev.invoke_url}/test" }
```

## Spot the Bug

A developer creates a resource policy with only a Deny statement to block specific IPs but no Allow statement. All requests are denied:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Deny",
      "Principal": "*",
      "Action": "execute-api:Invoke",
      "Resource": "execute-api:/*",
      "Condition": {
        "IpAddress": {
          "aws:SourceIp": ["203.0.113.0/24"]
        }
      }
    }
  ]
}
```

<details>
<summary>Explain the bug</summary>

The resource policy only has a **Deny** statement that blocks IPs in the `203.0.113.0/24` range. There is no **Allow** statement. Since IAM evaluation starts from an implicit deny, requests from IPs outside `203.0.113.0/24` are neither explicitly allowed nor explicitly denied -- they fall through to the implicit deny.

Result: requests from `203.0.113.0/24` are explicitly denied, and requests from all other IPs are implicitly denied. **Nobody can access the API.**

For REST API resource policies, the evaluation depends on the authentication type:

- **No authentication** (authorization = NONE): the resource policy alone determines access. You need an explicit Allow.
- **IAM authentication**: the resource policy and IAM policy are evaluated together. An Allow in either can grant access (unless there is an explicit Deny in the resource policy).

**Fix -- add an Allow statement for all other IPs:**

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": "*",
      "Action": "execute-api:Invoke",
      "Resource": "execute-api:/*"
    },
    {
      "Effect": "Deny",
      "Principal": "*",
      "Action": "execute-api:Invoke",
      "Resource": "execute-api:/*",
      "Condition": {
        "IpAddress": {
          "aws:SourceIp": ["203.0.113.0/24"]
        }
      }
    }
  ]
}
```

Now all IPs are explicitly allowed, except `203.0.113.0/24` which is explicitly denied (explicit deny overrides explicit allow).

</details>

## Solutions

<details>
<summary>TODO 1 -- API Gateway Resource Policy (`api.tf`)</summary>

```hcl
data "aws_iam_policy_document" "api_policy" {
  statement {
    sid       = "AllowFromCIDR"
    effect    = "Allow"
    actions   = ["execute-api:Invoke"]
    resources = ["execute-api:/*"]

    principals {
      type        = "*"
      identifiers = ["*"]
    }

    condition {
      test     = "IpAddress"
      variable = "aws:SourceIp"
      values   = [var.allowed_cidr]
    }
  }

  statement {
    sid       = "DenyOtherIPs"
    effect    = "Deny"
    actions   = ["execute-api:Invoke"]
    resources = ["execute-api:/*"]

    principals {
      type        = "*"
      identifiers = ["*"]
    }

    condition {
      test     = "NotIpAddress"
      variable = "aws:SourceIp"
      values   = [var.allowed_cidr]
    }
  }
}

resource "aws_api_gateway_rest_api_policy" "this" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  policy      = data.aws_iam_policy_document.api_policy.json
}
```

Alternatively, set the `policy` attribute directly on the `aws_api_gateway_rest_api` resource.

</details>

<details>
<summary>TODO 2 -- Deploy the API (`api.tf`)</summary>

```hcl
resource "aws_api_gateway_deployment" "this" {
  rest_api_id = aws_api_gateway_rest_api.this.id

  triggers = {
    redeployment = sha256(jsonencode([
      aws_api_gateway_resource.test.id,
      aws_api_gateway_method.test_get.id,
      aws_api_gateway_integration.test.id,
      data.aws_iam_policy_document.api_policy.json,
    ]))
  }

  lifecycle {
    create_before_destroy = true
  }

  depends_on = [
    aws_api_gateway_integration.test,
  ]
}

resource "aws_api_gateway_stage" "dev" {
  deployment_id = aws_api_gateway_deployment.this.id
  rest_api_id   = aws_api_gateway_rest_api.this.id
  stage_name    = "dev"
}
```

</details>

## Verify What You Learned

### Step 1 -- Deploy

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init && terraform apply -auto-approve
```

### Step 2 -- Test API access

```bash
# Test from your IP (should succeed if allowed_cidr covers your IP)
curl -s $(terraform output -raw api_url) | jq .
```

### Step 3 -- Verify resource policy

```bash
aws apigateway get-rest-api --rest-api-id $(aws apigateway get-rest-apis \
  --query "items[?name=='apigw-resource-policy-demo'].id" --output text) \
  --query "policy" --output text | python3 -c "import sys,urllib.parse; print(urllib.parse.unquote(sys.stdin.read()))" | jq .
```

Expected: policy with Allow and Deny statements with `aws:SourceIp` conditions.

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

Destroy all resources:

```bash
terraform destroy -auto-approve
```

Verify nothing remains:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

You configured API Gateway resource policies for IP-based access control. In the next exercise, you will explore **STS AssumeRole and temporary credentials** -- how Lambda functions assume cross-account roles to access resources in other AWS accounts.

## Summary

- **API Gateway resource policies** control who can invoke a REST API -- they are evaluated before IAM, Cognito, or Lambda authorizers
- Three use cases: **IP whitelisting** (`aws:SourceIp`), **VPC endpoint restriction** (`aws:SourceVpce`), **cross-account access**
- Resource policies require **both Allow and Deny** statements for IP-based restrictions -- a Deny-only policy blocks everyone
- Resource policies are available for **REST APIs only** (not HTTP APIs or WebSocket APIs)
- The `execute-api:Invoke` action controls the ability to call API methods
- Resource policy changes require an API **redeployment** to take effect
- For unauthenticated APIs, the resource policy alone determines access; for IAM-authenticated APIs, both the resource policy and IAM policy are evaluated together

## Reference

- [API Gateway Resource Policies](https://docs.aws.amazon.com/apigateway/latest/developerguide/apigateway-resource-policies.html)
- [Resource Policy Examples](https://docs.aws.amazon.com/apigateway/latest/developerguide/apigateway-resource-policies-examples.html)
- [Terraform aws_api_gateway_rest_api_policy](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_rest_api_policy)

## Additional Resources

- [API Gateway Policy Evaluation](https://docs.aws.amazon.com/apigateway/latest/developerguide/apigateway-authorization-flow.html) -- how resource policies interact with other authorization mechanisms
- [Private API Access Control](https://docs.aws.amazon.com/apigateway/latest/developerguide/apigateway-private-apis.html) -- VPC endpoint-based restriction for private APIs
- [Cross-Account API Access](https://docs.aws.amazon.com/apigateway/latest/developerguide/apigateway-resource-policies-examples.html#apigateway-resource-policies-cross-account-example) -- allowing other accounts to call your API
- [Condition Keys for API Gateway](https://docs.aws.amazon.com/apigateway/latest/developerguide/apigateway-resource-policies-examples.html) -- available condition keys and operators

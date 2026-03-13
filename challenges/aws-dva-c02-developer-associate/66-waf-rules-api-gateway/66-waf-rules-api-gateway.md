# 66. WAF Rules for API Gateway

<!--
difficulty: advanced
concepts: [waf-webacl, managed-rules, rate-based-rules, sql-injection, custom-rules, rule-priority, api-gateway-waf-association, override-actions]
tools: [terraform, aws-cli]
estimated_time: 55m
bloom_level: evaluate, create
prerequisites: [02-api-gateway-rest-vs-http-validation]
aws_cost: ~$0.02/hr
-->

> **AWS Cost Warning:** This exercise creates a WAF WebACL ($5.00/month base), managed rule groups ($1.00/month each), an API Gateway, and a Lambda function. Cost is approximately $0.02/hr. Remember to run `terraform destroy` when finished to avoid ongoing WAF charges.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally
- curl installed for testing

## Learning Objectives

After completing this exercise, you will be able to:

- **Evaluate** when to use managed rule groups versus custom rules based on the threat model
- **Design** a WAF WebACL with layered defenses: managed rules for common attacks, custom rules for application-specific logic
- **Implement** a rate-based rule that blocks excessive requests from a single IP
- **Analyze** rule priority ordering and how WAF evaluates rules from lowest to highest priority number
- **Configure** override actions on managed rule groups to count instead of block during initial deployment

## Why WAF Rules for API Gateway

AWS WAF (Web Application Firewall) inspects HTTP requests before they reach your application. For API Gateway, WAF sits in front of the API stage and evaluates every request against a set of rules. Matching requests can be blocked, allowed, counted, or challenged.

WAF uses three rule types:

1. **Managed rule groups**: AWS-maintained rule sets that detect common attacks. `AWSManagedRulesCommonRuleSet` covers OWASP Top 10 vulnerabilities (XSS, path traversal, file inclusion). `AWSManagedRulesSQLiRuleSet` specifically targets SQL injection patterns. These are battle-tested and updated automatically by AWS.

2. **Rate-based rules**: block IPs that exceed a request threshold within a 5-minute window. Set the limit (e.g., 100 requests per 5 minutes) and WAF automatically tracks and blocks offending IPs.

3. **Custom rules**: match on specific request attributes (headers, query strings, body, URI path) using string match, regex, size constraints, or geographic location.

The DVA-C02 exam tests rule priority (lower number = evaluated first), the default action (what happens when no rule matches), and the distinction between blocking and counting. A common deployment pattern: set managed rules to "count" mode initially to monitor false positives, then switch to "block" once you confirm legitimate traffic is not affected.

## The Challenge

Build a WAF WebACL with layered defenses and associate it with an API Gateway stage.

### Requirements

| Requirement | Description |
|---|---|
| Managed Rules | AWSManagedRulesCommonRuleSet (priority 1) and AWSManagedRulesSQLiRuleSet (priority 2) |
| Rate-Based Rule | Block IPs exceeding 100 requests per 5 minutes (priority 3) |
| Custom Rule | Block requests with specific header value for testing (priority 0) |
| Default Action | Allow (requests not matching any rule are allowed) |
| Association | Attach WebACL to API Gateway stage |
| Override | Set SQLi managed rules to count mode for monitoring |

### Architecture

```
  Client Request
       |
       v
  +-------------------+
  | AWS WAF            |
  | Priority 0: Custom |
  | Priority 1: Common |
  | Priority 2: SQLi   |
  | Priority 3: Rate   |
  | Default: Allow     |
  +-------------------+
       |
       v (if allowed)
  +-------------------+
  | API Gateway Stage  |
  +-------------------+
       |
       v
  +-------------------+
  | Lambda Function    |
  +-------------------+
```

## Hints

<details>
<summary>Hint 1: WAF WebACL structure</summary>

A WebACL has a default action and a list of rules. Each rule has a priority (lower = evaluated first), an action (block, allow, count), and a statement that defines the match condition:

```hcl
resource "aws_wafv2_web_acl" "this" {
  name        = "api-protection"
  scope       = "REGIONAL"  # REGIONAL for API Gateway, CLOUDFRONT for CloudFront
  description = "WAF for API Gateway"

  default_action {
    allow {}  # Requests not matching any rule are allowed
  }

  # Rules go here as rule {} blocks
  # Each rule has: name, priority, action/override_action, statement, visibility_config

  visibility_config {
    cloudwatch_metrics_enabled = true
    metric_name                = "api-protection"
    sampled_requests_enabled   = true
  }
}
```

</details>

<details>
<summary>Hint 2: Managed rule groups</summary>

Managed rule groups use `managed_rule_group_statement` and require `override_action` instead of `action`:

```hcl
rule {
  name     = "aws-common-rules"
  priority = 1

  override_action {
    none {}  # Use the rule group's native actions (block)
    # count {} -- Use this to count instead of block (monitoring mode)
  }

  statement {
    managed_rule_group_statement {
      vendor_name = "AWS"
      name        = "AWSManagedRulesCommonRuleSet"
    }
  }

  visibility_config {
    cloudwatch_metrics_enabled = true
    metric_name                = "common-rules"
    sampled_requests_enabled   = true
  }
}
```

For count mode (monitoring), change `override_action` to `count {}`.

</details>

<details>
<summary>Hint 3: Rate-based rule</summary>

Rate-based rules track requests per IP within a 5-minute window:

```hcl
rule {
  name     = "rate-limit"
  priority = 3

  action {
    block {}
  }

  statement {
    rate_based_statement {
      limit              = 100  # Requests per 5-minute window per IP
      aggregate_key_type = "IP"
    }
  }

  visibility_config {
    cloudwatch_metrics_enabled = true
    metric_name                = "rate-limit"
    sampled_requests_enabled   = true
  }
}
```

The minimum limit is 100 requests per 5-minute window.

</details>

<details>
<summary>Hint 4: API Gateway association</summary>

Associate the WebACL with the API Gateway stage ARN:

```hcl
resource "aws_wafv2_web_acl_association" "this" {
  resource_arn = aws_api_gateway_stage.this.arn
  web_acl_arn  = aws_wafv2_web_acl.this.arn
}
```

The stage ARN format is `arn:aws:apigateway:region::/restapis/api-id/stages/stage-name`.

</details>

## Spot the Bug

A developer creates a WebACL with rules but sets the wrong priority ordering. The rate-based rule has priority 0 and the custom block rule has priority 10. Legitimate requests are rate-limited before the custom rule can block known-bad requests:

```hcl
resource "aws_wafv2_web_acl" "this" {
  name  = "api-waf"
  scope = "REGIONAL"

  default_action { allow {} }

  rule {
    name     = "rate-limit"
    priority = 0              # <-- Evaluated first
    action { block {} }
    statement {
      rate_based_statement { limit = 100; aggregate_key_type = "IP" }
    }
    visibility_config { cloudwatch_metrics_enabled = true; metric_name = "rate"; sampled_requests_enabled = true }
  }

  rule {
    name     = "block-bad-agents"
    priority = 10             # <-- Evaluated last
    action { block {} }
    statement {
      byte_match_statement {
        field_to_match { single_header { name = "user-agent" } }
        positional_constraint = "CONTAINS"
        search_string         = "BadBot"
        text_transformation { priority = 0; type = "LOWERCASE" }
      }
    }
    visibility_config { cloudwatch_metrics_enabled = true; metric_name = "bad-agents"; sampled_requests_enabled = true }
  }

  visibility_config { cloudwatch_metrics_enabled = true; metric_name = "api-waf"; sampled_requests_enabled = true }
}
```

<details>
<summary>Explain the bug</summary>

WAF evaluates rules in **priority order** (lowest number first). With the rate-based rule at priority 0, it is evaluated before the custom rule at priority 10. This means:

1. If a bad bot sends requests under the rate limit, it will be allowed by the rate-based rule (no match) and then checked by the custom rule (blocked). This works correctly.

2. However, the real issue is **rule design priority**. Best practice is to put specific blocking rules (like bad user-agent detection) at the lowest priority numbers so they are evaluated first. Rate-based rules should have higher priority numbers because they are a broader, catch-all defense.

A more impactful version of this bug: if the custom rule used `action { allow {} }` for a known-good IP list, and the rate-based rule was at a lower priority, a known-good IP that sends too many requests would be blocked by the rate-based rule before the allow rule could exempt it.

**Fix -- reorder priorities so specific rules evaluate first:**

```hcl
rule {
  name     = "block-bad-agents"
  priority = 0    # Specific blocks first
  # ...
}

rule {
  name     = "rate-limit"
  priority = 10   # Broad rate limiting after specific rules
  # ...
}
```

General priority ordering best practice:
- Priority 0-2: Custom block/allow rules (specific)
- Priority 3-5: Managed rule groups (general protection)
- Priority 6+: Rate-based rules (broad throttling)

</details>

## Verify What You Learned

### Step 1 -- Deploy

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init && terraform apply -auto-approve
```

### Step 2 -- Test normal request

```bash
API_URL=$(terraform output -raw api_url)
curl -s "$API_URL" | jq .
```

Expected: 200 response with function output.

### Step 3 -- Test SQL injection (blocked by SQLi rule set)

```bash
curl -s -o /dev/null -w "%{http_code}" "$API_URL?id=1%20OR%201=1"
```

Expected: `403` (blocked by WAF) or `200` if SQLi rules are in count mode.

### Step 4 -- Verify WebACL configuration

```bash
aws wafv2 list-web-acls --scope REGIONAL \
  --query "WebACLs[?Name=='waf-apigw-demo'].{Name:Name,Id:Id}" --output table
```

### Step 5 -- Check sampled requests

```bash
WEB_ACL_ARN=$(aws wafv2 list-web-acls --scope REGIONAL \
  --query "WebACLs[?Name=='waf-apigw-demo'].ARN" --output text)
aws wafv2 get-sampled-requests \
  --web-acl-arn "$WEB_ACL_ARN" \
  --rule-metric-name "common-rules" \
  --scope REGIONAL \
  --time-window StartTime=$(date -u -v-1H +%Y-%m-%dT%H:%M:%SZ),EndTime=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  --max-items 5 2>/dev/null | jq '.SampledRequests[:3]'
```

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

You built a layered WAF defense for API Gateway with managed rules, rate limiting, and custom rules. In the next exercise, you will explore **Cognito Identity Pools** -- federating user identities from a Cognito User Pool to obtain temporary AWS credentials for direct service access.

## Summary

- **AWS WAF** evaluates HTTP requests against rules before they reach API Gateway
- Rules are evaluated in **priority order** (lowest number first); first matching rule's action is applied
- **Managed rule groups** (CommonRuleSet, SQLiRuleSet) provide pre-built protection for common attacks
- **Rate-based rules** automatically block IPs exceeding a request threshold per 5-minute window (minimum 100)
- Use `override_action: count` to monitor managed rules before enabling blocking (**count mode**)
- **Default action** applies when no rule matches -- typically `allow` for APIs
- WAF scope: `REGIONAL` for API Gateway/ALB, `CLOUDFRONT` for CloudFront distributions
- WAF WebACL association links the WebACL to a specific API Gateway stage ARN
- Rule priority best practice: specific blocks first, managed rules next, rate limits last

## Reference

- [AWS WAF Developer Guide](https://docs.aws.amazon.com/waf/latest/developerguide/waf-chapter.html)
- [Managed Rule Groups](https://docs.aws.amazon.com/waf/latest/developerguide/aws-managed-rule-groups-list.html)
- [Terraform aws_wafv2_web_acl](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/wafv2_web_acl)
- [Terraform aws_wafv2_web_acl_association](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/wafv2_web_acl_association)

## Additional Resources

- [WAF Rule Statements](https://docs.aws.amazon.com/waf/latest/developerguide/waf-rule-statements.html) -- all available statement types
- [Rate-Based Rules](https://docs.aws.amazon.com/waf/latest/developerguide/waf-rule-statement-type-rate-based.html) -- configuration details and scope-down statements
- [Testing and Tuning WAF](https://docs.aws.amazon.com/waf/latest/developerguide/web-acl-testing.html) -- best practices for production deployment
- [WAF Logging](https://docs.aws.amazon.com/waf/latest/developerguide/logging.html) -- enabling full request logging for analysis

<details>
<summary>Full Solution</summary>

### `lambda/main.go`

```go
package main

import (
    "context"
    "encoding/json"

    "github.com/aws/aws-lambda-go/events"
    "github.com/aws/aws-lambda-go/lambda"
)

func handler(ctx context.Context, event events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
    body, _ := json.Marshal(map[string]interface{}{
        "message":    "Hello from behind WAF",
        "source_ip":  event.RequestContext.Identity.SourceIP,
        "user_agent": event.RequestContext.Identity.UserAgent,
        "path":       event.Path,
        "query":      event.QueryStringParameters,
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

```
module waf-demo

go 1.21

require (
    github.com/aws/aws-lambda-go v1.47.0
)
```

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
  default     = "waf-apigw-demo"
}
```

### `build.tf`

```hcl
resource "null_resource" "go_build" {
  triggers = { source_hash = filebase64sha256("${path.module}/lambda/main.go") }
  provisioner "local-exec" {
    command     = "GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go"
    working_dir = "${path.module}/lambda"
  }
}

data "archive_file" "function_zip" {
  type        = "zip"
  source_file = "${path.module}/lambda/bootstrap"
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
resource "aws_cloudwatch_log_group" "lambda" {
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
  timeout          = 10

  depends_on = [aws_iam_role_policy_attachment.lambda_basic, aws_cloudwatch_log_group.lambda]
}

resource "aws_lambda_permission" "apigw" {
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.this.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.this.execution_arn}/*/*"
}
```

### `api.tf`

```hcl
resource "aws_api_gateway_rest_api" "this" {
  name = "${var.project_name}-api"
}

resource "aws_api_gateway_resource" "test" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  parent_id   = aws_api_gateway_rest_api.this.root_resource_id
  path_part   = "test"
}

resource "aws_api_gateway_method" "get" {
  rest_api_id   = aws_api_gateway_rest_api.this.id
  resource_id   = aws_api_gateway_resource.test.id
  http_method   = "GET"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "lambda" {
  rest_api_id             = aws_api_gateway_rest_api.this.id
  resource_id             = aws_api_gateway_resource.test.id
  http_method             = aws_api_gateway_method.get.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.this.invoke_arn
}

resource "aws_api_gateway_deployment" "this" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  depends_on  = [aws_api_gateway_integration.lambda]
  lifecycle { create_before_destroy = true }
}

resource "aws_api_gateway_stage" "this" {
  rest_api_id   = aws_api_gateway_rest_api.this.id
  deployment_id = aws_api_gateway_deployment.this.id
  stage_name    = "prod"
}
```

### `waf.tf`

```hcl
resource "aws_wafv2_web_acl" "this" {
  name        = var.project_name
  scope       = "REGIONAL"
  description = "WAF for API Gateway with layered defenses"

  default_action {
    allow {}
  }

  # Priority 0: Custom rule -- block requests with x-block-me header
  rule {
    name     = "block-test-header"
    priority = 0

    action {
      block {}
    }

    statement {
      byte_match_statement {
        field_to_match {
          single_header { name = "x-block-me" }
        }
        positional_constraint = "EXACTLY"
        search_string         = "true"
        text_transformation {
          priority = 0
          type     = "LOWERCASE"
        }
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "block-test-header"
      sampled_requests_enabled   = true
    }
  }

  # Priority 1: AWS Managed Common Rule Set
  rule {
    name     = "aws-common-rules"
    priority = 1

    override_action {
      none {}
    }

    statement {
      managed_rule_group_statement {
        vendor_name = "AWS"
        name        = "AWSManagedRulesCommonRuleSet"
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "common-rules"
      sampled_requests_enabled   = true
    }
  }

  # Priority 2: AWS Managed SQLi Rule Set (count mode for monitoring)
  rule {
    name     = "aws-sqli-rules"
    priority = 2

    override_action {
      count {}
    }

    statement {
      managed_rule_group_statement {
        vendor_name = "AWS"
        name        = "AWSManagedRulesSQLiRuleSet"
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "sqli-rules"
      sampled_requests_enabled   = true
    }
  }

  # Priority 3: Rate-based rule
  rule {
    name     = "rate-limit"
    priority = 3

    action {
      block {}
    }

    statement {
      rate_based_statement {
        limit              = 100
        aggregate_key_type = "IP"
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "rate-limit"
      sampled_requests_enabled   = true
    }
  }

  visibility_config {
    cloudwatch_metrics_enabled = true
    metric_name                = var.project_name
    sampled_requests_enabled   = true
  }
}

resource "aws_wafv2_web_acl_association" "this" {
  resource_arn = aws_api_gateway_stage.this.arn
  web_acl_arn  = aws_wafv2_web_acl.this.arn
}
```

### `outputs.tf`

```hcl
output "api_url" {
  value = "${aws_api_gateway_stage.this.invoke_url}/test"
}

output "web_acl_name" {
  value = aws_wafv2_web_acl.this.name
}
```

</details>

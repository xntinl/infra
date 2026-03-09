# 73. WAF Web ACL Rules

<!--
difficulty: intermediate
concepts: [waf-web-acl, managed-rule-groups, aws-managed-rules-common, sqli-rule-set, ip-reputation-list, rate-based-rules, geo-match, rule-priority, count-vs-block, alb-association]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: apply, analyze
prerequisites: [02-alb-vs-nlb-target-groups-routing]
aws_cost: ~$0.02/hr
-->

> **AWS Cost Warning:** WAF WebACL costs $5/month (~$0.007/hr) plus $1/month per rule. Managed rule groups: $1-3/month each. ALB: ~$0.023/hr. Total ~$0.02/hr. Remember to run `terraform destroy` immediately when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Default VPC available | `aws ec2 describe-vpcs --filters Name=is-default,Values=true --query 'Vpcs[0].VpcId'` |
| Completed exercise 02 (ALB basics) | Understanding of ALB resources |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Implement** a WAF WebACL with managed rule groups and custom rules using Terraform.
2. **Analyze** the difference between COUNT and BLOCK actions and when each is appropriate.
3. **Apply** rate-based rules and geo-match conditions to protect web applications.
4. **Evaluate** rule priority ordering and its impact on request evaluation.
5. **Design** a WAF rule strategy that balances security coverage with false-positive risk.

---

## Why This Matters

AWS WAF is a core security topic on the SAA-C03 exam because it sits at the boundary between network security and application security. The exam tests whether you understand what WAF can protect against (SQL injection, XSS, bot traffic, volumetric attacks) and what it cannot (it does not encrypt data, manage IAM policies, or replace NACLs/security groups). WAF operates at Layer 7 -- it inspects HTTP request content, not TCP/IP headers.

The critical architectural decision is which managed rule groups to enable and whether to use COUNT or BLOCK mode. COUNT mode logs matching requests without blocking them, which is essential during initial deployment to avoid false positives. The exam presents scenarios where "the security team deployed WAF but attacks are still succeeding" -- the answer is almost always that rules are set to COUNT instead of BLOCK. Understanding rule priority is equally important: WAF evaluates rules in priority order (lowest number first), and the first matching rule with a terminating action (BLOCK or ALLOW) stops evaluation.

Rate-based rules provide protection against DDoS at the application layer and brute-force attacks. Unlike Shield (which operates at L3/L4), WAF rate-based rules can throttle individual IPs sending more than a configured number of requests in a 5-minute window. The exam tests whether you know to combine Shield (L3/L4 DDoS) with WAF rate-based rules (L7 volumetric) for comprehensive protection.

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
  default     = "saa-ex73"
}
```

### `security.tf`

```hcl
data "aws_vpc" "default" {
  default = true
}

data "aws_subnets" "default" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.default.id]
  }
  filter {
    name   = "default-for-az"
    values = ["true"]
  }
}

resource "aws_security_group" "alb" {
  name   = "${var.project_name}-alb-sg"
  vpc_id = data.aws_vpc.default.id

  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "HTTP from anywhere"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}
```

### `alb.tf`

```hcl
resource "aws_lb" "this" {
  name               = "${var.project_name}-alb"
  internal           = false
  load_balancer_type = "application"
  security_groups    = [aws_security_group.alb.id]
  subnets            = slice(data.aws_subnets.default.ids, 0, 2)
}

resource "aws_lb_listener" "http" {
  load_balancer_arn = aws_lb.this.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type = "fixed-response"
    fixed_response {
      content_type = "text/plain"
      message_body = "OK"
      status_code  = "200"
    }
  }
}
```

### `waf.tf`

```hcl
# ============================================================
# TODO 1: WAF WebACL with Managed Rule Groups
# ============================================================
# Create a WAF WebACL with three AWS managed rule groups:
#
#   1. AWSManagedRulesCommonRuleSet (priority 1)
#      - Core protection: XSS, file inclusion, command injection
#      - Vendor: "AWS"
#
#   2. AWSManagedRulesSQLiRuleSet (priority 2)
#      - SQL injection protection
#      - Vendor: "AWS"
#
#   3. AWSManagedRulesAmazonIpReputationList (priority 3)
#      - Blocks requests from known malicious IPs
#      - Vendor: "AWS"
#
# Requirements:
#   - Resource: aws_wafv2_web_acl
#   - name = "${var.project_name}-web-acl"
#   - scope = "REGIONAL" (for ALB; use CLOUDFRONT for CF distros)
#   - default_action = allow (block only matched rules)
#   - Each managed rule group:
#     - override_action { none {} } to use the group's native actions
#     - visibility_config with sampled_requests_enabled = true
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/wafv2_web_acl
# ============================================================


# ============================================================
# TODO 2: Custom Rate-Based Rule
# ============================================================
# Add a rate-based rule to the WebACL (priority 4) that blocks
# any single IP sending more than 1000 requests in a 5-minute
# window.
#
# Requirements:
#   - Add a rule block inside the WebACL from TODO 1
#   - name = "rate-limit"
#   - priority = 4
#   - action { block {} }
#   - statement type: rate_based_statement
#     - limit = 1000
#     - aggregate_key_type = "IP"
#   - visibility_config with metric_name = "${var.project_name}-rate-limit"
#
# Note: rate-based rules evaluate over a rolling 5-minute window.
# The minimum limit is 100.
# ============================================================


# ============================================================
# TODO 3: Custom Geo-Match Rule
# ============================================================
# Add a geo-match rule (priority 5) that blocks requests from
# specific countries. For this exercise, block requests from
# country codes "RU" and "CN".
#
# Requirements:
#   - Add a rule block inside the WebACL
#   - name = "geo-block"
#   - priority = 5
#   - action { block {} }
#   - statement type: geo_match_statement
#     - country_codes = ["RU", "CN"]
#   - visibility_config with metric_name = "${var.project_name}-geo-block"
#
# Note: In production, geo-blocking should be a business decision.
# The exam tests whether you know WAF can do geo-blocking
# (not CloudFront geo-restriction, which is a separate feature).
# ============================================================


# ============================================================
# TODO 4: Associate WebACL with ALB
# ============================================================
# Associate the WebACL with the ALB so that all requests
# to the ALB are evaluated against the WAF rules.
#
# Requirements:
#   - Resource: aws_wafv2_web_acl_association
#   - web_acl_arn = WebACL ARN from TODO 1
#   - resource_arn = ALB ARN
#
# WAF can associate with: ALB, API Gateway REST API,
# AppSync GraphQL API, Cognito User Pool, App Runner,
# Verified Access Instance.
# WAF CANNOT associate with: NLB, CLB, EC2 directly.
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/wafv2_web_acl_association
# ============================================================
```

### `outputs.tf`

```hcl
output "alb_dns" {
  value = aws_lb.this.dns_name
}

output "web_acl_arn" {
  value = "Set after TODO 1 implementation"
}
```

---

## WAF Rule Evaluation Order

| Priority | Rule | Action | Effect |
|---|---|---|---|
| 1 | AWSManagedRulesCommonRuleSet | Varies per sub-rule | XSS, LFI, RFI, command injection |
| 2 | AWSManagedRulesSQLiRuleSet | Varies per sub-rule | SQL injection patterns |
| 3 | AWSManagedRulesAmazonIpReputationList | Varies per sub-rule | Known malicious IPs |
| 4 | Rate-based (1000/5min) | BLOCK | Volumetric/brute-force |
| 5 | Geo-match (RU, CN) | BLOCK | Geographic restriction |
| Default | -- | ALLOW | Everything else passes |

Rules are evaluated in priority order (lowest number first). The first rule with a terminating action (BLOCK or ALLOW) stops evaluation. COUNT is non-terminating -- the request continues to the next rule.

### WAF Cost Model

| Component | Price (us-east-1) |
|---|---|
| WebACL | $5.00/month |
| Per rule | $1.00/month |
| Per managed rule group | $1.00-$3.00/month |
| Per million requests inspected | $0.60 |

For the exam: WAF is priced per WebACL + per rule + per request. CloudFront+WAF is the most cost-effective way to protect global web applications because CloudFront handles caching (reducing requests reaching WAF) and WAF pricing is the same for CLOUDFRONT scope.

---

## Spot the Bug

The following WebACL configuration appears to protect against SQL injection but has a critical flaw. Identify the problem before expanding the answer.

```hcl
resource "aws_wafv2_web_acl" "this" {
  name  = "my-web-acl"
  scope = "REGIONAL"

  default_action {
    allow {}
  }

  rule {
    name     = "sql-injection-protection"
    priority = 1

    override_action {
      count {}
    }

    statement {
      managed_rule_group_statement {
        name        = "AWSManagedRulesSQLiRuleSet"
        vendor_name = "AWS"
      }
    }

    visibility_config {
      sampled_requests_enabled   = true
      cloudwatch_metrics_enabled = true
      metric_name                = "sql-injection"
    }
  }

  visibility_config {
    sampled_requests_enabled   = true
    cloudwatch_metrics_enabled = true
    metric_name                = "my-web-acl"
  }
}
```

<details>
<summary>Explain the bug</summary>

**The `override_action` is set to `count {}` instead of `none {}`, which overrides all BLOCK actions in the managed rule group to COUNT.** SQL injection attempts are logged in CloudWatch metrics and sampled requests, but every single one passes through to the application. The WAF is monitoring attacks without blocking them.

This is the most common WAF misconfiguration. `override_action` controls how the WebACL treats the actions defined within the managed rule group:

- `override_action { none {} }` -- use the managed rule group's native actions (BLOCK for most rules). This is what you want in production.
- `override_action { count {} }` -- override ALL actions in the group to COUNT. Use this only during initial testing to identify false positives.

The insidious part: CloudWatch metrics show the rule "matching" SQL injection patterns, which gives the false impression that protection is working. The security team sees matches in the dashboard but does not realize the requests were counted, not blocked.

**Fix:**

```hcl
override_action {
  none {}
}
```

**Best practice:** Deploy new managed rule groups with `count {}` for 24-48 hours to analyze matches. If no legitimate traffic is being matched, switch to `none {}`. Use WAF logging (to S3, CloudWatch Logs, or Kinesis Data Firehose) to review matched requests during the COUNT phase.

</details>

---

## Verify What You Learned

1. **Deploy the infrastructure:**
   ```bash
   terraform init && terraform apply -auto-approve
   ```

2. **Verify the WebACL exists with correct rules:**
   ```bash
   aws wafv2 list-web-acls --scope REGIONAL \
     --query 'WebACLs[?Name==`saa-ex73-web-acl`].{Name:Name,Id:Id}' \
     --output table
   ```
   Expected: One WebACL named `saa-ex73-web-acl`.

3. **Verify rule groups and custom rules:**
   ```bash
   WEB_ACL_ID=$(aws wafv2 list-web-acls --scope REGIONAL \
     --query 'WebACLs[?Name==`saa-ex73-web-acl`].Id' --output text)

   aws wafv2 get-web-acl --scope REGIONAL --name saa-ex73-web-acl --id "$WEB_ACL_ID" \
     --query 'WebACL.Rules[*].{Name:Name,Priority:Priority}' \
     --output table
   ```
   Expected: Five rules (3 managed groups + rate-limit + geo-block) with priorities 1-5.

4. **Verify ALB association:**
   ```bash
   ALB_ARN=$(aws elbv2 describe-load-balancers --names saa-ex73-alb \
     --query 'LoadBalancers[0].LoadBalancerArn' --output text)

   aws wafv2 get-web-acl-for-resource --resource-arn "$ALB_ARN" \
     --query 'WebACL.Name' --output text
   ```
   Expected: `saa-ex73-web-acl`

5. **Test with a simulated SQL injection (should be blocked):**
   ```bash
   ALB_DNS=$(terraform output -raw alb_dns)
   curl -s -o /dev/null -w "%{http_code}" \
     "http://${ALB_DNS}/?id=1%20OR%201=1"
   ```
   Expected: `403` (Forbidden) if SQLi rule is in BLOCK mode.

6. **Terraform state consistency:**
   ```bash
   terraform plan
   ```
   Expected: `No changes. Your infrastructure matches the configuration.`

---

## Solutions

<details>
<summary>TODO 1 -- WebACL with Managed Rule Groups (waf.tf)</summary>

```hcl
resource "aws_wafv2_web_acl" "this" {
  name  = "${var.project_name}-web-acl"
  scope = "REGIONAL"

  default_action {
    allow {}
  }

  rule {
    name     = "aws-common-rules"
    priority = 1

    override_action {
      none {}
    }

    statement {
      managed_rule_group_statement {
        name        = "AWSManagedRulesCommonRuleSet"
        vendor_name = "AWS"
      }
    }

    visibility_config {
      sampled_requests_enabled   = true
      cloudwatch_metrics_enabled = true
      metric_name                = "${var.project_name}-common-rules"
    }
  }

  rule {
    name     = "aws-sqli-rules"
    priority = 2

    override_action {
      none {}
    }

    statement {
      managed_rule_group_statement {
        name        = "AWSManagedRulesSQLiRuleSet"
        vendor_name = "AWS"
      }
    }

    visibility_config {
      sampled_requests_enabled   = true
      cloudwatch_metrics_enabled = true
      metric_name                = "${var.project_name}-sqli-rules"
    }
  }

  rule {
    name     = "aws-ip-reputation"
    priority = 3

    override_action {
      none {}
    }

    statement {
      managed_rule_group_statement {
        name        = "AWSManagedRulesAmazonIpReputationList"
        vendor_name = "AWS"
      }
    }

    visibility_config {
      sampled_requests_enabled   = true
      cloudwatch_metrics_enabled = true
      metric_name                = "${var.project_name}-ip-reputation"
    }
  }

  # Custom rules added in TODOs 2 and 3

  visibility_config {
    sampled_requests_enabled   = true
    cloudwatch_metrics_enabled = true
    metric_name                = "${var.project_name}-web-acl"
  }

  tags = { Name = "${var.project_name}-web-acl" }
}
```

Key points:
- `scope = "REGIONAL"` for ALB, API Gateway, AppSync. Use `"CLOUDFRONT"` only for CloudFront distributions (must be in us-east-1).
- `override_action { none {} }` preserves the managed rule group's native BLOCK actions. Using `count {}` would log without blocking.
- `default_action { allow {} }` means unmatched requests pass through. Only matched rules block.

</details>

<details>
<summary>TODO 2 -- Rate-Based Rule (waf.tf)</summary>

Add this rule block inside the `aws_wafv2_web_acl` resource:

```hcl
  rule {
    name     = "rate-limit"
    priority = 4

    action {
      block {}
    }

    statement {
      rate_based_statement {
        limit              = 1000
        aggregate_key_type = "IP"
      }
    }

    visibility_config {
      sampled_requests_enabled   = true
      cloudwatch_metrics_enabled = true
      metric_name                = "${var.project_name}-rate-limit"
    }
  }
```

Note: Custom rules use `action {}` (not `override_action {}` which is for managed rule groups). The rate limit evaluates over a rolling 5-minute window. Once an IP exceeds 1000 requests in 5 minutes, subsequent requests are blocked until the rate drops below the threshold.

</details>

<details>
<summary>TODO 3 -- Geo-Match Rule (waf.tf)</summary>

Add this rule block inside the `aws_wafv2_web_acl` resource:

```hcl
  rule {
    name     = "geo-block"
    priority = 5

    action {
      block {}
    }

    statement {
      geo_match_statement {
        country_codes = ["RU", "CN"]
      }
    }

    visibility_config {
      sampled_requests_enabled   = true
      cloudwatch_metrics_enabled = true
      metric_name                = "${var.project_name}-geo-block"
    }
  }
```

WAF geo-match uses the source IP's geolocation. If your application is behind CloudFront, you can also use CloudFront's geo-restriction feature, but WAF geo-match provides more flexibility (combine with other conditions, use COUNT for monitoring).

</details>

<details>
<summary>TODO 4 -- ALB Association (waf.tf)</summary>

```hcl
resource "aws_wafv2_web_acl_association" "alb" {
  web_acl_arn  = aws_wafv2_web_acl.this.arn
  resource_arn = aws_lb.this.arn
}
```

Update `outputs.tf`:

```hcl
output "web_acl_arn" {
  value = aws_wafv2_web_acl.this.arn
}
```

One WebACL can be associated with multiple resources. One resource can only be associated with one WebACL. If you need different rule sets for different ALBs, create separate WebACLs.

</details>

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify:

```bash
aws wafv2 list-web-acls --scope REGIONAL \
  --query 'WebACLs[?Name==`saa-ex73-web-acl`]'
```

Expected: Empty list.

---

## What's Next

Exercise 74 covers **Security Hub for aggregated compliance**, where you will enable Security Hub, activate compliance standards (CIS, AWS Foundational), and analyze aggregated findings from GuardDuty, Inspector, Macie, and Config. Security Hub provides the single-pane-of-glass view that ties together all the individual security services you have deployed in exercises 71-73.

---

## Summary

- **WAF WebACL** is the top-level container for WAF rules -- associate it with ALB, API Gateway, AppSync, or CloudFront
- **Managed rule groups** provide pre-built protection: CommonRuleSet (XSS, LFI), SQLiRuleSet (SQL injection), IpReputationList (known bad IPs)
- **override_action { none {} }** preserves the managed group's BLOCK actions; **count {}** logs without blocking (use for testing only)
- **Custom rules** use `action {}` (not `override_action {}`) and support rate-based, geo-match, IP set, regex, size constraint, and byte match statements
- **Rule priority** determines evaluation order (lowest number first) -- first terminating action (BLOCK/ALLOW) stops evaluation
- **Rate-based rules** block IPs exceeding a request threshold in a 5-minute rolling window (minimum limit: 100)
- **Geo-match** blocks requests by source country -- distinct from CloudFront geo-restriction
- **REGIONAL scope** for ALB, API Gateway, AppSync; **CLOUDFRONT scope** for CloudFront distributions (must deploy in us-east-1)
- **WAF + Shield Advanced** provides comprehensive L3-L7 DDoS protection with cost protection guarantees

## Reference

- [AWS WAF Developer Guide](https://docs.aws.amazon.com/waf/latest/developerguide/what-is-aws-waf.html)
- [AWS Managed Rule Groups](https://docs.aws.amazon.com/waf/latest/developerguide/aws-managed-rule-groups-list.html)
- [Terraform aws_wafv2_web_acl](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/wafv2_web_acl)
- [WAF Pricing](https://aws.amazon.com/waf/pricing/)

## Additional Resources

- [WAF Logging](https://docs.aws.amazon.com/waf/latest/developerguide/logging.html) -- send WAF logs to S3, CloudWatch Logs, or Kinesis Data Firehose for analysis
- [AWS WAF Bot Control](https://docs.aws.amazon.com/waf/latest/developerguide/waf-bot-control.html) -- advanced bot management managed rule group
- [WAF Rate-Based Rule Statement](https://docs.aws.amazon.com/waf/latest/developerguide/waf-rule-statement-type-rate-based.html) -- detailed rate-based rule configuration options
- [Testing WAF Rules](https://docs.aws.amazon.com/waf/latest/developerguide/web-acl-testing.html) -- best practices for deploying rules safely using COUNT mode

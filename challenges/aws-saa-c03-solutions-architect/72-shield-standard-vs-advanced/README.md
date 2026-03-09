# 72. Shield Standard vs Shield Advanced

<!--
difficulty: intermediate
concepts: [shield-standard, shield-advanced, ddos-protection, layer-3-4, layer-7, drt, cost-protection, protection-group, health-check, waf-integration, cloudfront-shield]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: apply, analyze
prerequisites: [65-iam-policies-identity-resource-scp]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** Shield Standard is free and automatic. Shield Advanced costs $3,000/month (1-year commitment). This exercise focuses on Shield Standard (free) and describes Shield Advanced as reference. The Terraform code creates only Shield Standard resources. Total cost ~$0.01/hr. Do NOT enable Shield Advanced unless you understand the $3,000/month commitment.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Completed exercise 65 (IAM policies) | Understanding of security concepts |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Distinguish** between Shield Standard (free, automatic L3/L4 DDoS protection) and Shield Advanced ($3,000/month, L7 protection, DRT access, cost protection).
2. **Analyze** the DDoS protection architecture: CloudFront and Route 53 always get Shield Standard, ALB/NLB/EIP get Shield Standard automatically.
3. **Evaluate** the Shield Advanced value proposition: DDoS Response Team (DRT), cost protection during attacks, L7 attack visibility, and advanced metrics.
4. **Apply** a decision framework for when Shield Advanced is justified vs when Shield Standard plus WAF is sufficient.
5. **Design** a DDoS-resilient architecture combining Shield, WAF, CloudFront, and Auto Scaling.

---

## Why This Matters

Shield appears on the SAA-C03 exam in DDoS protection scenarios. The critical distinction is: Shield Standard is free and automatic -- every AWS account gets it. It protects against common L3/L4 attacks (SYN floods, UDP reflection, DNS amplification) on all AWS resources. Shield Advanced adds L7 protection, DDoS Response Team (DRT) access, near-real-time attack visibility, and cost protection (AWS credits your bill for DDoS-related scaling costs). At $3,000/month, Shield Advanced is only justified for business-critical applications where DDoS downtime has significant financial impact.

The exam tests the decision framework: "Your e-commerce site loses $100,000/hour during downtime. How do you protect against DDoS?" -- Shield Advanced (DRT + cost protection justifies the $3,000/month). "Your internal application needs basic DDoS protection" -- Shield Standard (free, automatic, sufficient for L3/L4). The key insight is that Shield Advanced alone does not protect against L7 attacks (HTTP floods) -- you also need WAF rules to inspect and filter HTTP traffic. Shield Advanced without WAF leaves L7 attacks unmitigated.

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
  default     = "saa-ex72"
}
```

### `main.tf`

```hcl
# ============================================================
# TODO 1: Verify Shield Standard Protection
# ============================================================
# Shield Standard is automatic. Verify: aws shield get-subscription-state
# Protects CloudFront, Route 53, ALB, NLB, EIP against L3/L4.
# NOT L7 (HTTP floods require WAF).
# Docs: https://docs.aws.amazon.com/waf/latest/developerguide/ddos-standard-summary.html
# ============================================================


# ============================================================
# TODO 2: Shield Advanced (Reference — DO NOT APPLY)
# ============================================================
# $3,000/month. Adds: DRT, cost protection, L7 visibility,
# health-based detection, protection groups. Reference only.
# Docs: https://docs.aws.amazon.com/waf/latest/developerguide/ddos-advanced-summary.html
# ============================================================


# ============================================================
# TODO 3: DDoS-Resilient Architecture with WAF
# ============================================================
# WAF rate limiting + managed rules provide L7 protection
# without Shield Advanced ($5/month vs $3,000/month).
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/wafv2_web_acl
# ============================================================

resource "aws_wafv2_web_acl" "this" {
  name  = "${var.project_name}-web-acl"
  scope = "REGIONAL"

  default_action {
    allow {}
  }

  # Rate limiting: block IPs exceeding 2000 requests per 5 minutes
  rule {
    name     = "rate-limit"
    priority = 1

    action {
      block {}
    }

    statement {
      rate_based_statement {
        limit              = 2000
        aggregate_key_type = "IP"
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "${var.project_name}-rate-limit"
      sampled_requests_enabled   = true
    }
  }

  # AWS managed rule group for common attacks
  rule {
    name     = "aws-managed-common"
    priority = 2

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
      cloudwatch_metrics_enabled = true
      metric_name                = "${var.project_name}-common-rules"
      sampled_requests_enabled   = true
    }
  }

  visibility_config {
    cloudwatch_metrics_enabled = true
    metric_name                = "${var.project_name}-web-acl"
    sampled_requests_enabled   = true
  }

  tags = { Name = "${var.project_name}-web-acl" }
}
```

### `outputs.tf`

```hcl
output "web_acl_arn" {
  value = aws_wafv2_web_acl.this.arn
}

output "web_acl_id" {
  value = aws_wafv2_web_acl.this.id
}
```

---

## Shield Standard vs Shield Advanced

| Criterion | Shield Standard | Shield Advanced |
|---|---|---|
| **Cost** | Free | $3,000/month (1-year commitment) |
| **Activation** | Automatic (all AWS accounts) | Manual enrollment |
| **L3/L4 protection** | Yes (SYN floods, UDP reflection, etc.) | Yes (same + enhanced detection) |
| **L7 protection** | No | Yes (requires WAF integration) |
| **DDoS Response Team (DRT)** | No | Yes (24/7, can create WAF rules for you) |
| **Attack visibility** | Basic (CloudWatch) | Near-real-time metrics and forensics |
| **Cost protection** | No | Yes (credits for DDoS-related scaling costs) |
| **Health-based detection** | No | Yes (Route 53 health check integration) |
| **Protection groups** | No | Yes (monitor multiple resources together) |
| **SLA** | No DDoS-specific SLA | DDoS-specific availability SLA |
| **WAF included** | No | WAF charges included for protected resources |

### Decision Framework

```
Is the application business-critical?
  No  --> Shield Standard (free) + WAF rate limiting (~$5/month)
  Yes --> What is the hourly cost of downtime?
            < $3,000/month --> Shield Standard + WAF + Auto Scaling
            > $3,000/month --> Shield Advanced
                               PLUS WAF (for L7 protection)
                               PLUS Auto Scaling (absorb traffic)
                               PLUS CloudFront (edge filtering)

Does the application face targeted L7 attacks?
  No  --> Shield Standard is sufficient for L3/L4
  Yes --> Shield Advanced + WAF + DRT engagement
          DRT can create custom WAF rules during active attacks
```

---

## Spot the Bug

A company subscribed to Shield Advanced ($3,000/month) to protect their e-commerce platform from DDoS attacks. During a recent HTTP flood attack, Shield Advanced detected the attack and sent alerts, but the application still went down:

```hcl
# Shield Advanced protection on ALB
resource "aws_shield_protection" "alb" {
  name         = "ecommerce-alb"
  resource_arn = aws_lb.ecommerce.arn
}

# No WAF configured
# No CloudFront in front of ALB
# No rate limiting rules
```

The security team asks: "Why did Shield Advanced not stop the attack?"

<details>
<summary>Explain the bug</summary>

**Shield Advanced without WAF does not mitigate L7 (application-layer) attacks.** Shield Advanced provides visibility into L7 attacks and access to the DDoS Response Team, but it does not automatically block HTTP-level flood traffic. L7 attacks use legitimate-looking HTTP requests that pass through L3/L4 inspection. To actually block them, you need WAF rules that inspect HTTP headers, query strings, and request rates.

The architecture has three gaps:

1. **No WAF rules:** Shield Advanced detects the L7 attack but has no mechanism to block individual HTTP requests. WAF rate-based rules or IP-based blocking are needed.

2. **No CloudFront:** The ALB is directly exposed to the internet. CloudFront would absorb traffic at the edge (300+ edge locations), reducing the volume that reaches the ALB. CloudFront also integrates with WAF for edge-level filtering.

3. **No auto-scaling headroom:** Even with WAF blocking most attack traffic, some legitimate traffic patterns during an attack require additional compute capacity.

**Fix:** Add WAF, CloudFront, and engage the DRT proactively:

```hcl
# WAF with rate limiting on the ALB
resource "aws_wafv2_web_acl" "ecommerce" {
  name  = "ecommerce-waf"
  scope = "REGIONAL"

  default_action { allow {} }

  rule {
    name     = "rate-limit"
    priority = 1
    action { block {} }
    statement {
      rate_based_statement {
        limit              = 2000
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
    metric_name                = "ecommerce-waf"
    sampled_requests_enabled   = true
  }
}

# Associate WAF with ALB
resource "aws_wafv2_web_acl_association" "alb" {
  resource_arn = aws_lb.ecommerce.arn
  web_acl_arn  = aws_wafv2_web_acl.ecommerce.arn
}

# Enable DRT proactive engagement
resource "aws_shield_proactive_engagement" "this" {
  enabled = true
}
```

With Shield Advanced + WAF:
- Shield detects and alerts on the attack
- WAF rate-based rules automatically block high-volume IPs
- DRT can create emergency WAF rules during the attack
- Cost protection credits your account for scaling costs

</details>

---

---

## Verify What You Learned

1. **Deploy the WAF configuration:**
   ```bash
   terraform init && terraform apply -auto-approve
   ```

2. **Verify Shield Standard is active:**
   ```bash
   aws shield get-subscription-state
   ```
   Expected: `INACTIVE` (this means no Shield Advanced subscription -- Shield Standard is always active).

3. **Verify WAF Web ACL:**
   ```bash
   aws wafv2 get-web-acl \
     --name saa-ex72-web-acl \
     --scope REGIONAL \
     --id "$(terraform output -raw web_acl_id)" \
     --query 'WebACL.{Name:Name,Rules:Rules[*].Name}' \
     --output json
   ```
   Expected: Two rules (rate-limit, aws-managed-common).

4. **List WAF rules:**
   ```bash
   aws wafv2 list-web-acls \
     --scope REGIONAL \
     --query 'WebACLs[?Name==`saa-ex72-web-acl`].{Name:Name,Id:Id}' \
     --output table
   ```
   Expected: One Web ACL listed.

5. **Verify Terraform state:**
   ```bash
   terraform plan
   ```
   Expected: `No changes. Your infrastructure matches the configuration.`

---

## Solutions

<details>
<summary>TODO 1 -- Verify Shield Standard</summary>

Shield Standard is automatic. Verify with:

```bash
# Shield Standard is always active — this check confirms
# whether Shield Advanced is subscribed
aws shield get-subscription-state

# List protected resources (Shield Advanced only)
aws shield list-protections 2>&1 || echo "Shield Advanced not enabled"

# View DDoS events (Shield Standard provides limited visibility)
aws shield list-attacks \
  --start-time "$(date -u -v-24H +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d '24 hours ago' +%Y-%m-%dT%H:%M:%SZ)" \
  --end-time "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --query 'AttackSummaries[*].{ResourceArn:ResourceArn,StartTime:StartTime,Vectors:AttackVectors[*].VectorType}' \
  --output json 2>&1 || echo "No attacks detected (or Shield Advanced required for API)"
```

</details>

<details>
<summary>TODO 3 -- WAF Web ACL (already provided in main.tf, reference only)</summary>

The WAF Web ACL in the main configuration provides:

1. **Rate-based rule:** Blocks any IP that sends more than 2,000 requests in a 5-minute window. This mitigates simple HTTP flood attacks without Shield Advanced.

2. **AWS Managed Rules (Common Rule Set):** Blocks common attack patterns including SQL injection, XSS, and path traversal from the OWASP Top 10.

To associate with an ALB:
```hcl
resource "aws_wafv2_web_acl_association" "alb" {
  resource_arn = aws_lb.this.arn
  web_acl_arn  = aws_wafv2_web_acl.this.arn
}
```

To associate with CloudFront (must use `CLOUDFRONT` scope in us-east-1):
```hcl
resource "aws_wafv2_web_acl" "cloudfront" {
  provider = aws.us_east_1
  scope    = "CLOUDFRONT"
  # ... same rules
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
aws wafv2 list-web-acls --scope REGIONAL \
  --query 'WebACLs[?Name==`saa-ex72-web-acl`]' --output text
```

Expected: No output (Web ACL deleted).

---

## What's Next

You have completed the Security and IAM foundations section (exercises 65-72). The next set of exercises covers **Compute Advanced Topics** including ECS, EKS, Fargate, Lambda optimization, and Step Functions -- building on the foundational compute knowledge from the EC2 section to explore container and serverless architectures.

---

## Summary

- **Shield Standard** is free and automatic for all AWS accounts -- protects against L3/L4 DDoS attacks (SYN floods, UDP reflection, DNS amplification)
- **Shield Advanced** costs $3,000/month -- adds L7 visibility, DDoS Response Team (DRT), cost protection, health-based detection, and protection groups
- **Shield Advanced without WAF** does not block L7 attacks -- WAF rules are required to inspect and filter HTTP-level flood traffic
- **CloudFront** provides the first line of defense -- absorbs traffic at 300+ edge locations before it reaches your origin
- **WAF rate-based rules** provide cost-effective L7 protection without Shield Advanced (~$5/month for basic rate limiting)
- **DDoS Response Team (DRT)** can create WAF rules on your behalf during an active attack -- only available with Shield Advanced
- **Cost protection** (Shield Advanced) credits your AWS bill for DDoS-related scaling costs (EC2, ALB, CloudFront, Route 53 query surges)
- **Protection groups** allow monitoring multiple resources (ALB + CloudFront) as a single unit for coordinated attack detection
- **Health-based detection** (Shield Advanced + Route 53) reduces false positives by correlating traffic anomalies with actual resource health degradation
- **DDoS-resilient architecture** = CloudFront (edge) + WAF (L7 filtering) + Shield (L3/L4) + Auto Scaling (absorb residual traffic)

## Reference

- [Shield Developer Guide](https://docs.aws.amazon.com/waf/latest/developerguide/shield-chapter.html)
- [Shield Standard vs Advanced](https://docs.aws.amazon.com/waf/latest/developerguide/ddos-overview.html)
- [WAF Developer Guide](https://docs.aws.amazon.com/waf/latest/developerguide/waf-chapter.html)
- [Terraform aws_wafv2_web_acl](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/wafv2_web_acl)

## Additional Resources

- [AWS DDoS Best Practices Whitepaper](https://docs.aws.amazon.com/whitepapers/latest/aws-best-practices-ddos-resiliency/aws-best-practices-ddos-resiliency.html) -- comprehensive DDoS mitigation strategies
- [Shield Advanced Pricing](https://aws.amazon.com/shield/pricing/) -- understand the 1-year commitment and per-resource charges
- [WAF Rate-Based Rules](https://docs.aws.amazon.com/waf/latest/developerguide/waf-rule-statement-type-rate-based.html) -- configuration options for rate limiting
- [AWS Shield Response Team Engagement](https://docs.aws.amazon.com/waf/latest/developerguide/ddos-srt.html) -- how to engage the DRT during an attack

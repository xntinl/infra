# 6. CloudFront with Custom Origin, OAC, and WAF

<!--
difficulty: intermediate
concepts: [cloudfront, custom-origin, origin-access-control, waf, alb, acm]
tools: [terraform, aws-cli]
estimated_time: 60m
bloom_level: explain, implement, configure
prerequisites: [01-04]
aws_cost: ~$0.08/hr
-->

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.5 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Exercise 04 completed | Understand multi-AZ VPC with public/private subnets |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Explain** how CloudFront caches content at edge locations and the role of custom origins
2. **Implement** a CloudFront distribution with an ALB as its origin
3. **Configure** origin verification using a custom header so only CloudFront can reach the ALB
4. **Create** a WAF WebACL with rate-limiting rules and associate it with CloudFront
5. **Restrict** the ALB security group to accept traffic only from CloudFront using the AWS-managed prefix list

## Why This Matters

Users expect sub-second page loads regardless of where they are in the world. CloudFront's 400+ edge locations cache your content close to users, reducing latency from hundreds of milliseconds to single digits for cached requests. For dynamic content, CloudFront still helps by maintaining persistent connections to your origin and routing through the AWS backbone instead of the public internet.

Without origin protection, attackers can bypass CloudFront (and its WAF) by hitting your ALB directly. A custom origin header acts as a shared secret -- CloudFront injects it on every request, and your ALB's listener rule rejects requests without it. The AWS-managed prefix list for CloudFront lets you lock down the ALB security group to only CloudFront's IP ranges, which AWS keeps updated automatically. Layering WAF on top gives you rate limiting, geo-blocking, and protection against common web exploits like SQL injection -- all evaluated at the edge before traffic ever reaches your infrastructure.

## Building Blocks

Create the following files in your exercise directory. Your job is to fill in each `# TODO` block.

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
  default     = "cf-waf-lab"
}

variable "origin_secret" {
  description = "Shared secret for CloudFront origin header verification"
  type        = string
  default     = "my-super-secret-origin-header-value-12345"
  sensitive   = true
}
```

### `vpc.tf`

```hcl
data "aws_availability_zones" "available" {
  state = "available"
}

locals {
  azs = slice(data.aws_availability_zones.available.names, 0, 2)
}

resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = "${var.project_name}-vpc" }
}

resource "aws_subnet" "public" {
  for_each = toset(local.azs)

  vpc_id                  = aws_vpc.this.id
  cidr_block              = cidrsubnet("10.0.0.0/16", 8, index(local.azs, each.key))
  availability_zone       = each.key
  map_public_ip_on_launch = true

  tags = { Name = "${var.project_name}-public-${each.key}" }
}

resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id
  tags   = { Name = "${var.project_name}-igw" }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.this.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this.id
  }

  tags = { Name = "${var.project_name}-public-rt" }
}

resource "aws_route_table_association" "public" {
  for_each = aws_subnet.public

  subnet_id      = each.value.id
  route_table_id = aws_route_table.public.id
}
```

### `alb.tf`

```hcl
resource "aws_security_group" "alb" {
  name   = "${var.project_name}-alb-sg"
  vpc_id = aws_vpc.this.id

  # TODO 5 will restrict this — for now allow all HTTP inbound
  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "${var.project_name}-alb-sg" }
}

resource "aws_lb" "this" {
  name               = "${var.project_name}-alb"
  internal           = false
  load_balancer_type = "application"
  security_groups    = [aws_security_group.alb.id]
  subnets            = [for s in aws_subnet.public : s.id]

  tags = { Name = "${var.project_name}-alb" }
}

resource "aws_lb_target_group" "this" {
  name     = "${var.project_name}-tg"
  port     = 80
  protocol = "HTTP"
  vpc_id   = aws_vpc.this.id

  health_check {
    path                = "/"
    healthy_threshold   = 2
    unhealthy_threshold = 3
    interval            = 30
  }

  tags = { Name = "${var.project_name}-tg" }
}

resource "aws_lb_listener" "http" {
  load_balancer_arn = aws_lb.this.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type = "fixed-response"

    fixed_response {
      content_type = "text/plain"
      message_body = "Direct access denied. Use CloudFront."
      status_code  = 403
    }
  }
}

resource "aws_lb_listener_rule" "verify_origin" {
  listener_arn = aws_lb_listener.http.arn
  priority     = 1

  condition {
    http_header {
      http_header_name = "X-Custom-Origin-Verify"
      values           = [var.origin_secret]
    }
  }

  action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.this.arn
  }
}
```

### `dns.tf`

```hcl
# =======================================================
# TODO 1 — Create a CloudFront Distribution
# =======================================================
# Requirements:
#   - Create an aws_cloudfront_distribution resource
#   - Origin: use the ALB DNS name (aws_lb.this.dns_name)
#   - Origin settings:
#       http_port  = 80
#       https_port = 443
#       origin_protocol_policy = "http-only"
#       origin_ssl_protocols   = ["TLSv1.2"]
#   - Default cache behavior:
#       allowed_methods  = ["GET", "HEAD", "OPTIONS",
#                           "PUT", "POST", "PATCH", "DELETE"]
#       cached_methods   = ["GET", "HEAD"]
#       target_origin_id = a descriptive string (e.g., "alb-origin")
#       viewer_protocol_policy = "redirect-to-https"
#       forwarded_values: forward all headers (not recommended in
#       production, but simplifies this lab)
#   - enabled = true
#   - restrictions: no geo restrictions
#   - viewer_certificate: use cloudfront_default_certificate = true
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudfront_distribution
#
# Hint: The origin block uses domain_name (ALB DNS) and
#       custom_origin_config (not s3_origin_config).


# =======================================================
# TODO 2 — Add a custom header for origin verification
# =======================================================
# Requirements:
#   - Inside the origin block of your CloudFront distribution
#     (from TODO 1), add a custom_header block:
#       name  = "X-Custom-Origin-Verify"
#       value = var.origin_secret
#   - This header is injected by CloudFront on every request
#     to the ALB. The ALB listener rule (already defined above)
#     checks for it and returns 403 if missing.
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudfront_distribution#custom-header
#
# Note: If you already included this in TODO 1, just verify it
#       is present. This TODO is a reminder to not skip it.


# =======================================================
# TODO 3 — Create a WAF WebACL with rate limiting
# =======================================================
# Requirements:
#   - Create an aws_wafv2_web_acl resource
#   - Scope: "CLOUDFRONT" (must be in us-east-1)
#   - Default action: allow
#   - Add one rule: rate-based rule limiting to 2000 requests
#     per 5-minute period per IP
#   - Rule action: block
#   - Use visibility_config with:
#       cloudwatch_metrics_enabled = true
#       sampled_requests_enabled   = true
#       metric_name = "${var.project_name}-rate-limit"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/wafv2_web_acl
#
# Hint: The rule block needs:
#   statement { rate_based_statement { limit = 2000, aggregate_key_type = "IP" } }


# =======================================================
# TODO 4 — Associate WAF WebACL with CloudFront
# =======================================================
# Requirements:
#   - Create an aws_wafv2_web_acl_association OR set the
#     web_acl_id attribute on the CloudFront distribution
#   - For CloudFront, you set web_acl_id on the distribution
#     resource itself (not a separate association)
#   - Reference the WAF WebACL ARN from TODO 3
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudfront_distribution#web_acl_id
#
# Hint: Add web_acl_id = aws_wafv2_web_acl.this.arn to
#       your CloudFront distribution from TODO 1.
#       If you already included it, verify it is present.


# =======================================================
# TODO 5 — Restrict ALB SG to CloudFront only
# =======================================================
# Requirements:
#   - Look up the AWS-managed prefix list for CloudFront
#     using an aws_ec2_managed_prefix_list data source
#     with name = "com.amazonaws.global.cloudfront.origin-facing"
#   - Replace the ALB security group's ingress rule:
#     instead of cidr_blocks = ["0.0.0.0/0"], use
#     prefix_list_ids = [data.aws_ec2_managed_prefix_list.cloudfront.id]
#
# Docs:
#   https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/ec2_managed_prefix_list
#
# Hint: Modify the aws_security_group.alb resource in alb.tf.
#       Change cidr_blocks to prefix_list_ids in the ingress block.
```

### `outputs.tf`

```hcl
output "alb_dns_name" {
  value = aws_lb.this.dns_name
}

output "cloudfront_domain_name" {
  value = aws_cloudfront_distribution.this.domain_name
}

output "waf_web_acl_arn" {
  value = aws_wafv2_web_acl.this.arn
}
```

## Spot the Bug

A colleague configured CloudFront with HTTPS-only origin protocol, but the ALB listener is HTTP-only. Users see `502 Bad Gateway` errors. **What is wrong and how do you fix it?**

```hcl
origin {
  domain_name = aws_lb.this.dns_name
  origin_id   = "alb-origin"

  custom_origin_config {
    http_port              = 80
    https_port             = 443
    origin_protocol_policy = "https-only"    # <-- BUG
    origin_ssl_protocols   = ["TLSv1.2"]
  }
}
```

Meanwhile, the ALB listener:

```hcl
resource "aws_lb_listener" "http" {
  load_balancer_arn = aws_lb.this.arn
  port              = 80        # Only listens on HTTP
  protocol          = "HTTP"
}
```

<details>
<summary>Explain the bug</summary>

CloudFront is configured with `origin_protocol_policy = "https-only"`, which means it will connect to the ALB on port 443 using TLS. But the ALB only has an HTTP listener on port 80 -- there is no HTTPS listener and no ACM certificate attached. CloudFront's TLS handshake fails, and it returns a `502 Bad Gateway` to the user.

There are two valid fixes:

**Option A** -- Match CloudFront to the ALB (simpler for this lab):
```hcl
origin_protocol_policy = "http-only"
```

**Option B** -- Upgrade the ALB to HTTPS (production-grade):
1. Request an ACM certificate for your domain
2. Add an HTTPS listener on port 443 with the certificate
3. Keep CloudFront at `https-only`

Option B is preferred in production because it encrypts traffic between CloudFront and your origin. For this lab, Option A works because the custom header verification provides origin authentication, and the traffic stays on the AWS network.

</details>

## Verify What You Learned

### Step 1 -- Plan and apply

Run `terraform init` then `terraform plan`. You should see approximately **15-18 resources**:

```
Plan: 16 to add, 0 to change, 0 to destroy.
```

Apply when ready. Note: CloudFront distributions take 5-10 minutes to deploy.

### Step 2 -- Test CloudFront serves content

Wait for the distribution to deploy, then curl the CloudFront domain:

```
curl -s -o /dev/null -w "%{http_code}" \
  https://$(terraform output -raw cloudfront_domain_name)/
```

Expected: `403` (because the target group has no healthy targets -- the fixed response fires). The important thing is you get a response from your origin, not a CloudFront error.

### Step 3 -- Verify direct ALB access is denied

```
curl -s -o /dev/null -w "%{http_code}" \
  http://$(terraform output -raw alb_dns_name)/
```

Expected: `403` with body "Direct access denied. Use CloudFront." -- the ALB rejects requests without the custom origin header.

### Step 4 -- Verify WAF is associated

```
aws wafv2 get-web-acl \
  --name cf-waf-lab-waf \
  --scope CLOUDFRONT \
  --region us-east-1 \
  --query "WebACL.{Name:Name,Rules:Rules[].Name,Capacity:Capacity}" \
  --output table
```

Expected output shows the rate-limit rule name and capacity.

### Step 5 -- Verify ALB security group uses prefix list

```
aws ec2 describe-security-groups \
  --group-ids $(aws ec2 describe-security-groups \
    --filters "Name=group-name,Values=cf-waf-lab-alb-sg" \
    --query "SecurityGroups[0].GroupId" --output text) \
  --query "SecurityGroups[0].IpPermissions[].PrefixListIds[].PrefixListId" \
  --output text
```

Expected: a prefix list ID like `pl-3b927c52` (the CloudFront origin-facing prefix list).

## Solutions

<details>
<summary>TODO 1 + TODO 2 + TODO 4 -- CloudFront Distribution with custom header and WAF (dns.tf)</summary>

```hcl
resource "aws_cloudfront_distribution" "this" {
  enabled         = true
  is_ipv6_enabled = true
  comment         = "${var.project_name} distribution"
  web_acl_id      = aws_wafv2_web_acl.this.arn

  origin {
    domain_name = aws_lb.this.dns_name
    origin_id   = "alb-origin"

    custom_origin_config {
      http_port              = 80
      https_port             = 443
      origin_protocol_policy = "http-only"
      origin_ssl_protocols   = ["TLSv1.2"]
    }

    custom_header {
      name  = "X-Custom-Origin-Verify"
      value = var.origin_secret
    }
  }

  default_cache_behavior {
    allowed_methods  = ["GET", "HEAD", "OPTIONS", "PUT", "POST", "PATCH", "DELETE"]
    cached_methods   = ["GET", "HEAD"]
    target_origin_id = "alb-origin"

    viewer_protocol_policy = "redirect-to-https"

    forwarded_values {
      query_string = true

      headers = ["*"]

      cookies {
        forward = "all"
      }
    }
  }

  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }

  viewer_certificate {
    cloudfront_default_certificate = true
  }

  tags = {
    Name = "${var.project_name}-distribution"
  }
}
```

</details>

<details>
<summary>TODO 3 -- WAF WebACL with rate limiting (dns.tf)</summary>

```hcl
resource "aws_wafv2_web_acl" "this" {
  name  = "${var.project_name}-waf"
  scope = "CLOUDFRONT"

  default_action {
    allow {}
  }

  rule {
    name     = "${var.project_name}-rate-limit"
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
      sampled_requests_enabled   = true
      metric_name                = "${var.project_name}-rate-limit"
    }
  }

  visibility_config {
    cloudwatch_metrics_enabled = true
    sampled_requests_enabled   = true
    metric_name                = "${var.project_name}-waf"
  }

  tags = {
    Name = "${var.project_name}-waf"
  }
}
```

</details>

<details>
<summary>TODO 5 -- Restrict ALB SG to CloudFront prefix list (alb.tf)</summary>

First, add the data source:

```hcl
data "aws_ec2_managed_prefix_list" "cloudfront" {
  name = "com.amazonaws.global.cloudfront.origin-facing"
}
```

Then modify the `aws_security_group.alb` ingress block -- replace:

```hcl
cidr_blocks = ["0.0.0.0/0"]
```

with:

```hcl
prefix_list_ids = [data.aws_ec2_managed_prefix_list.cloudfront.id]
```

The full modified security group:

```hcl
resource "aws_security_group" "alb" {
  name   = "${var.project_name}-alb-sg"
  vpc_id = aws_vpc.this.id

  ingress {
    from_port       = 80
    to_port         = 80
    protocol        = "tcp"
    prefix_list_ids = [data.aws_ec2_managed_prefix_list.cloudfront.id]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "${var.project_name}-alb-sg" }
}
```

</details>

## Cleanup

Destroy all resources:

```
terraform destroy
```

Type `yes` when prompted. Note that CloudFront distributions must be disabled before deletion. Terraform handles this automatically, but it can take **15-20 minutes** for the distribution to fully disable and delete. Be patient and do not interrupt the process.

## What's Next

In **Exercise 07 -- Route 53 Routing Policies and Health Checks**, you will implement weighted, failover, and latency-based DNS routing to distribute traffic across multiple endpoints and automate failover when health checks detect issues.

## Summary

You built a CDN-protected application with:

- **CloudFront distribution** caching and accelerating content delivery from an ALB origin
- **Custom origin header** ensuring only CloudFront can reach the ALB (requests without the header get 403)
- **WAF WebACL** with rate limiting to block abusive IPs at the edge
- **Prefix-list security group** locking the ALB to CloudFront's IP ranges

This layered approach (WAF at edge, origin verification, SG restriction) is the standard pattern for protecting public-facing web applications on AWS.

## Reference

| Resource | Purpose |
|----------|---------|
| `aws_cloudfront_distribution` | CDN distribution with edge caching |
| `aws_wafv2_web_acl` | Web application firewall rules |
| `aws_lb` | Application Load Balancer (origin) |
| `aws_lb_listener_rule` | Origin header verification |
| `aws_ec2_managed_prefix_list` | AWS-maintained IP range list |

## Additional Resources

- [CloudFront Developer Guide](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/Introduction.html)
- [Restricting Access to ALBs](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/restrict-access-to-load-balancer.html)
- [WAF Developer Guide](https://docs.aws.amazon.com/waf/latest/developerguide/what-is-aws-waf.html)
- [CloudFront Managed Prefix List](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/LocationsOfEdgeServers.html)
- [Terraform CloudFront Distribution](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudfront_distribution)

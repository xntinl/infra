# 42. ALB Authentication with Cognito and OIDC

<!--
difficulty: intermediate
concepts: [alb-authentication, cognito-user-pool, oidc, authenticate-cognito-action, authenticate-oidc-action, callback-url, jwt-token]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: apply, evaluate
prerequisites: [none]
aws_cost: ~$0.03/hr
-->

> **AWS Cost Warning:** ALB costs ~$0.0225/hr plus LCU charges. Cognito User Pools: first 50,000 MAUs free. EC2 t3.micro ~$0.0104/hr. Total ~$0.03/hr. Remember to run `terraform destroy` when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Default VPC available in target region | `aws ec2 describe-vpcs --filters Name=isDefault,Values=true` |
| Understanding of ALB concepts | Familiarity with listeners, target groups, and rules |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Explain** how ALB built-in authentication works: the ALB intercepts unauthenticated requests, redirects to Cognito/OIDC, validates the token, and forwards authenticated requests to the backend.
2. **Implement** an ALB listener rule with the `authenticate-cognito` action using Terraform.
3. **Configure** a Cognito User Pool with an app client and the correct callback URL for ALB integration.
4. **Compare** ALB-based authentication (offloads auth to the load balancer) with application-level authentication (handled by backend code).
5. **Evaluate** when to use ALB + Cognito vs ALB + OIDC (third-party IdP like Okta, Azure AD, Google).

---

## Why This Matters

ALB built-in authentication is a powerful feature that offloads the authentication burden from your application to the load balancer. The SAA-C03 exam tests this in scenarios like "How do you add authentication to a web application without modifying the application code?" The answer is ALB authentication with either Cognito or an OIDC-compatible identity provider.

The architecture is straightforward: the ALB listener rule has an `authenticate-cognito` (or `authenticate-oidc`) action that runs BEFORE the `forward` action. When an unauthenticated user hits the ALB, the ALB redirects them to the Cognito hosted UI or OIDC provider login page. After successful authentication, the provider redirects back to the ALB's callback URL with an authorization code. The ALB exchanges this code for tokens, validates them, and sets the `x-amzn-oidc-*` headers on the forwarded request so the backend knows who the user is.

The critical configuration detail is the callback URL. The Cognito app client must have the ALB's callback URL (`https://<alb-dns>/oauth2/idpresponse`) in its allowed callback URLs list. Missing this is the most common failure mode and a frequent exam distractor.

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
  default     = "saa-ex42"
}
```

### `main.tf`

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

data "aws_ami" "al2023" {
  most_recent = true
  owners      = ["amazon"]

  filter {
    name   = "name"
    values = ["al2023-ami-*-x86_64"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}

data "aws_caller_identity" "current" {}
```

### `security.tf`

```hcl
resource "aws_security_group" "alb" {
  name_prefix = "${var.project_name}-alb-"
  vpc_id      = data.aws_vpc.default.id
  description = "ALB with Cognito auth"

  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_security_group" "app" {
  name_prefix = "${var.project_name}-app-"
  vpc_id      = data.aws_vpc.default.id
  description = "App instances - from ALB only"

  ingress {
    from_port       = 8080
    to_port         = 8080
    protocol        = "tcp"
    security_groups = [aws_security_group.alb.id]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}
```

### `compute.tf`

```hcl
resource "aws_instance" "app" {
  ami                    = data.aws_ami.al2023.id
  instance_type          = "t3.micro"
  subnet_id              = data.aws_subnets.default.ids[0]
  vpc_security_group_ids = [aws_security_group.app.id]

  user_data = base64encode(<<-EOF
    #!/bin/bash
    # Simple app that shows authenticated user info from ALB headers
    while true; do
      RESPONSE="HTTP/1.1 200 OK\r\nContent-Type: text/html\r\n\r\n<h1>Authenticated!</h1><p>You passed ALB authentication.</p>"
      echo -e "$RESPONSE" | nc -l -p 8080 -q 1
    done &
  EOF
  )

  tags = {
    Name = "${var.project_name}-app-instance"
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

  tags = { Name = "${var.project_name}-alb" }
}

resource "aws_lb_target_group" "app" {
  name     = "${var.project_name}-app-tg"
  port     = 8080
  protocol = "HTTP"
  vpc_id   = data.aws_vpc.default.id

  health_check {
    path                = "/"
    port                = "traffic-port"
    healthy_threshold   = 2
    unhealthy_threshold = 3
    interval            = 30
    timeout             = 5
  }
}

resource "aws_lb_target_group_attachment" "app" {
  target_group_arn = aws_lb_target_group.app.arn
  target_id        = aws_instance.app.id
  port             = 8080
}

# ============================================================
# TODO 1: Cognito User Pool and App Client
# ============================================================
# Create a Cognito User Pool for ALB authentication.
#
# Requirements:
#   - Resource: aws_cognito_user_pool
#     - name = "${var.project_name}-user-pool"
#     - password_policy:
#       - minimum_length = 8
#       - require_lowercase = true
#       - require_uppercase = true
#       - require_numbers = true
#       - require_symbols = false
#
#   - Resource: aws_cognito_user_pool_domain
#     - domain = "${var.project_name}-auth" (must be globally unique)
#     - user_pool_id
#
#   - Resource: aws_cognito_user_pool_client
#     - name = "${var.project_name}-alb-client"
#     - user_pool_id
#     - generate_secret = true (REQUIRED for ALB integration)
#     - allowed_oauth_flows = ["code"]
#     - allowed_oauth_flows_user_pool_client = true
#     - allowed_oauth_scopes = ["openid", "email", "profile"]
#     - callback_urls = ["https://${aws_lb.this.dns_name}/oauth2/idpresponse"]
#     - supported_identity_providers = ["COGNITO"]
#
# CRITICAL: The callback_url MUST match exactly:
#   https://<ALB-DNS>/oauth2/idpresponse
# This is the fixed callback path that ALB uses. If this URL
# is not in the allowed callback URLs, authentication fails
# with a redirect_mismatch error.
#
# Note: generate_secret = true is REQUIRED for ALB. Without it,
# the ALB cannot exchange the authorization code for tokens.
#
# Docs: https://docs.aws.amazon.com/cognito/latest/developerguide/cognito-user-pools-app-idp-settings.html
# ============================================================


# ============================================================
# TODO 2: ALB Listener with Cognito Authentication Action
# ============================================================
# Create an ALB listener that authenticates users via Cognito
# before forwarding requests to the backend.
#
# Requirements:
#   - Resource: aws_lb_listener
#     - load_balancer_arn = ALB ARN
#     - port = 80
#     - protocol = "HTTP"
#
#     - default_action (order 1, type = "authenticate-cognito"):
#       - authenticate_cognito:
#         - user_pool_arn = user pool ARN
#         - user_pool_client_id = app client ID
#         - user_pool_domain = domain name
#
#     - default_action (order 2, type = "forward"):
#       - target_group_arn = app target group ARN
#
# The actions execute in order:
#   1. authenticate-cognito: checks for valid session cookie
#      - If no cookie: redirects to Cognito hosted UI
#      - If valid cookie: passes through
#   2. forward: sends authenticated request to backend
#
# The ALB adds headers to the forwarded request:
#   - x-amzn-oidc-accesstoken: JWT access token
#   - x-amzn-oidc-identity: user's sub (subject) claim
#   - x-amzn-oidc-data: JWT with user claims
#
# Note: For production, use HTTPS (port 443) with ACM certificate.
# HTTP is used here for simplicity, but Cognito authentication
# works best with HTTPS to protect tokens in transit.
#
# Docs: https://docs.aws.amazon.com/elasticloadbalancing/latest/application/listener-authenticate-users.html
# ============================================================
```

### `outputs.tf`

```hcl
output "alb_dns" {
  value = aws_lb.this.dns_name
}

output "instance_id" {
  value = aws_instance.app.id
}
```

---

## Spot the Bug

A team configures ALB with Cognito authentication but users see `redirect_mismatch` errors after logging in:

```hcl
resource "aws_cognito_user_pool_client" "alb" {
  name         = "alb-client"
  user_pool_id = aws_cognito_user_pool.this.id

  generate_secret                      = false  # Bug 1
  allowed_oauth_flows                  = ["code"]
  allowed_oauth_flows_user_pool_client = true
  allowed_oauth_scopes                 = ["openid"]
  supported_identity_providers         = ["COGNITO"]

  callback_urls = [
    "https://${aws_lb.this.dns_name}/callback"  # Bug 2
  ]
}
```

<details>
<summary>Explain the bug</summary>

Two problems prevent authentication from working:

**1. `generate_secret = false`:** ALB authentication requires a client secret. The ALB uses the client secret to exchange the authorization code for tokens during the OAuth2 code flow. Without a secret, the token exchange fails and the user is stuck in a redirect loop.

**Fix:** Set `generate_secret = true`.

**2. Wrong callback URL:** The ALB uses a fixed callback path: `/oauth2/idpresponse`. The configuration uses `/callback`, which does not match. After successful login, Cognito redirects to `/callback`, but the ALB does not have a handler at that path. The ALB's authentication handler only listens on `/oauth2/idpresponse`.

**Fix:** Set the callback URL to `https://<ALB-DNS>/oauth2/idpresponse`.

**Corrected configuration:**

```hcl
resource "aws_cognito_user_pool_client" "alb" {
  name         = "alb-client"
  user_pool_id = aws_cognito_user_pool.this.id

  generate_secret                      = true   # REQUIRED for ALB
  allowed_oauth_flows                  = ["code"]
  allowed_oauth_flows_user_pool_client = true
  allowed_oauth_scopes                 = ["openid", "email", "profile"]
  supported_identity_providers         = ["COGNITO"]

  callback_urls = [
    "https://${aws_lb.this.dns_name}/oauth2/idpresponse"  # Fixed path
  ]
}
```

**SAA-C03 exam pattern:** When the exam describes ALB + Cognito authentication failing, check for:
1. Missing client secret (`generate_secret` must be `true`)
2. Wrong callback URL (must end in `/oauth2/idpresponse`)
3. Missing HTTPS (Cognito requires HTTPS for callback URLs in production)

</details>

---

## ALB Authentication Architecture

```
User Browser
    |
    | 1. GET /app (no session cookie)
    v
  [ALB]
    |
    | 2. 302 Redirect to Cognito Hosted UI
    v
  [Cognito]
    |
    | 3. User enters credentials
    | 4. 302 Redirect to ALB /oauth2/idpresponse?code=xxx
    v
  [ALB]
    |
    | 5. Exchanges code for tokens (via Cognito token endpoint)
    | 6. Validates JWT, sets session cookie
    | 7. Forwards request with x-amzn-oidc-* headers
    v
  [Backend]
    |
    | 8. Reads user info from headers, processes request
    v
  Response to user
```

### ALB Authentication Comparison

| Feature | ALB + Cognito | ALB + OIDC | Application-Level Auth |
|---------|--------------|------------|----------------------|
| **Code changes** | None (ALB handles it) | None (ALB handles it) | Application code required |
| **Supported IdPs** | Cognito User Pools | Any OIDC provider (Okta, Azure AD, Google) | Any |
| **Session management** | ALB cookie (AWSELBAuthSessionCookie) | ALB cookie | Application manages sessions |
| **Token validation** | ALB validates JWT | ALB validates JWT | Application validates |
| **Multi-app support** | One user pool for many ALBs | One IdP for many ALBs | Per-application |
| **Cost** | Cognito free tier (50K MAUs) | IdP-dependent | Development cost |
| **Best for** | AWS-native, new user directory | Existing enterprise IdP | Complex auth logic |

---

## Solutions

<details>
<summary>TODO 1 -- Cognito User Pool and App Client -- `cognito.tf`</summary>

```hcl
resource "aws_cognito_user_pool" "this" {
  name = "${var.project_name}-user-pool"

  password_policy {
    minimum_length    = 8
    require_lowercase = true
    require_uppercase = true
    require_numbers   = true
    require_symbols   = false
  }

  # Auto-verify email (required for hosted UI sign-up)
  auto_verified_attributes = ["email"]

  schema {
    attribute_data_type = "String"
    name                = "email"
    required            = true
    mutable             = true

    string_attribute_constraints {
      min_length = 0
      max_length = 256
    }
  }

  tags = {
    Name = "${var.project_name}-user-pool"
  }
}

resource "aws_cognito_user_pool_domain" "this" {
  domain       = "${var.project_name}-auth-${data.aws_caller_identity.current.account_id}"
  user_pool_id = aws_cognito_user_pool.this.id
}

resource "aws_cognito_user_pool_client" "alb" {
  name         = "${var.project_name}-alb-client"
  user_pool_id = aws_cognito_user_pool.this.id

  generate_secret                      = true  # REQUIRED for ALB
  allowed_oauth_flows                  = ["code"]
  allowed_oauth_flows_user_pool_client = true
  allowed_oauth_scopes                 = ["openid", "email", "profile"]
  supported_identity_providers         = ["COGNITO"]

  callback_urls = [
    "https://${aws_lb.this.dns_name}/oauth2/idpresponse"
  ]
}

output "cognito_user_pool_id" {
  value = aws_cognito_user_pool.this.id
}

output "cognito_domain" {
  value = aws_cognito_user_pool_domain.this.domain
}
```

Note: The domain must be globally unique across all AWS accounts. Appending the account ID ensures uniqueness.

</details>

<details>
<summary>TODO 2 -- ALB Listener with Cognito Authentication -- `alb.tf`</summary>

```hcl
resource "aws_lb_listener" "http" {
  load_balancer_arn = aws_lb.this.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type  = "authenticate-cognito"
    order = 1

    authenticate_cognito {
      user_pool_arn       = aws_cognito_user_pool.this.arn
      user_pool_client_id = aws_cognito_user_pool_client.alb.id
      user_pool_domain    = aws_cognito_user_pool_domain.this.domain
    }
  }

  default_action {
    type             = "forward"
    order            = 2
    target_group_arn = aws_lb_target_group.app.arn
  }
}
```

Action ordering is critical: `authenticate-cognito` (order 1) runs before `forward` (order 2). The ALB checks for a valid session cookie first. If the cookie is missing or expired, it redirects to the Cognito hosted UI. Only after successful authentication does the request reach the `forward` action.

For HTTPS (production), you would add an ACM certificate:

```hcl
resource "aws_lb_listener" "https" {
  load_balancer_arn = aws_lb.this.arn
  port              = 443
  protocol          = "HTTPS"
  ssl_policy        = "ELBSecurityPolicy-TLS13-1-2-2021-06"
  certificate_arn   = aws_acm_certificate.this.arn

  default_action {
    type  = "authenticate-cognito"
    order = 1
    authenticate_cognito { ... }
  }

  default_action {
    type             = "forward"
    order            = 2
    target_group_arn = aws_lb_target_group.app.arn
  }
}
```

</details>

---

## Verify What You Learned

```bash
# Verify ALB exists
aws elbv2 describe-load-balancers \
  --names saa-ex42-alb \
  --query "LoadBalancers[0].{DNS:DNSName,State:State.Code,Type:Type}" \
  --output table
```

Expected: State=active, Type=application

```bash
# Verify Cognito User Pool exists
aws cognito-idp list-user-pools --max-results 10 \
  --query "UserPools[?Name=='saa-ex42-user-pool'].{Name:Name,Id:Id}" \
  --output table
```

Expected: User pool with Name=saa-ex42-user-pool

```bash
# Verify listener has authenticate-cognito action
aws elbv2 describe-listeners \
  --load-balancer-arn $(aws elbv2 describe-load-balancers --names saa-ex42-alb --query "LoadBalancers[0].LoadBalancerArn" --output text) \
  --query "Listeners[0].DefaultActions[*].Type" --output text
```

Expected: `authenticate-cognito forward`

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify Cognito pool is deleted:

```bash
aws cognito-idp list-user-pools --max-results 10 \
  --query "UserPools[?Name=='saa-ex42-user-pool'].Id" --output text
```

Expected: no output.

---

## What's Next

You configured ALB with built-in Cognito authentication. In the next exercise, you will explore **Cross-Zone Load Balancing** -- understanding how traffic distribution changes when instances are unevenly distributed across AZs, and why this behavior differs between ALB and NLB.

---

## Summary

- ALB built-in authentication offloads auth to the load balancer with **no application code changes**
- **authenticate-cognito** action: integrates with Cognito User Pools directly
- **authenticate-oidc** action: integrates with any OIDC-compatible IdP (Okta, Azure AD, Google)
- The ALB callback URL must be exactly `https://<ALB-DNS>/oauth2/idpresponse` -- any other path causes `redirect_mismatch`
- Cognito app client must have `generate_secret = true` for ALB integration
- The ALB sets **x-amzn-oidc-*** headers on forwarded requests with user identity information
- ALB manages session state via **AWSELBAuthSessionCookie** -- no server-side session storage needed
- For production, always use **HTTPS** (port 443 with ACM certificate) to protect tokens in transit
- Cognito is free for the first **50,000 monthly active users** -- cost-effective for most applications

---

## Reference

- [ALB User Authentication](https://docs.aws.amazon.com/elasticloadbalancing/latest/application/listener-authenticate-users.html)
- [Cognito User Pools](https://docs.aws.amazon.com/cognito/latest/developerguide/cognito-user-identity-pools.html)
- [Terraform aws_cognito_user_pool](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cognito_user_pool)
- [Terraform aws_lb_listener authenticate-cognito](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb_listener#authenticate-cognito-action)

## Additional Resources

- [ALB OIDC Authentication](https://docs.aws.amazon.com/elasticloadbalancing/latest/application/listener-authenticate-users.html#oidc-requirements) -- integrating with third-party OIDC providers
- [Cognito Hosted UI](https://docs.aws.amazon.com/cognito/latest/developerguide/cognito-user-pools-app-integration.html) -- customizing the login page
- [JWT Token Verification](https://docs.aws.amazon.com/cognito/latest/developerguide/amazon-cognito-user-pools-using-tokens-verifying-a-jwt.html) -- how backends can verify the x-amzn-oidc headers
- [Cognito Pricing](https://aws.amazon.com/cognito/pricing/) -- free tier and per-MAU pricing breakdown

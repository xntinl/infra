# 70. AWS Certificate Manager and HTTPS

<!--
difficulty: basic
concepts: [acm, tls-certificate, dns-validation, email-validation, alb-https, cloudfront-certificate, certificate-renewal, san, wildcard-certificate, ssl-termination]
tools: [terraform, aws-cli]
estimated_time: 30m
bloom_level: understand
prerequisites: [none]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** ACM public certificates are free. ALB costs ~$0.02/hr minimum (if created for testing). Route 53 hosted zone costs $0.50/month. For this exercise with only ACM resources, costs are negligible (~$0.01/hr). Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- A domain name managed in Route 53 (or understanding of DNS validation process)

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** how ACM provides free, auto-renewing TLS certificates for AWS services (ALB, CloudFront, API Gateway)
- **Implement** ACM certificate request with DNS validation using Route 53
- **Construct** an ALB HTTPS listener with an ACM certificate and HTTP-to-HTTPS redirect
- **Identify** the region requirement: CloudFront certificates must be in us-east-1, ALB certificates must be in the same region as the ALB
- **Describe** ACM certificate renewal: DNS-validated certificates auto-renew; email-validated require manual action
- **Distinguish** between ACM public certificates (free, for AWS services only) and ACM Private CA certificates (paid, for internal services)

## Why ACM Matters

ACM appears on the SAA-C03 exam in virtually every architecture involving HTTPS. The exam tests three critical concepts: (1) ACM certificates are free for use with AWS services but cannot be exported or used on EC2 instances directly (use ACM Private CA or third-party certificates for that); (2) DNS validation is preferred over email validation because DNS-validated certificates auto-renew indefinitely; (3) CloudFront requires the certificate in us-east-1 regardless of where your other resources are -- this region requirement is one of the most commonly tested ACM facts.

The typical architecture pattern is: request an ACM certificate, validate via DNS (Route 53 CNAME record), attach to an ALB HTTPS listener, and configure HTTP-to-HTTPS redirect. This provides end-to-end encryption from client to ALB with zero certificate management overhead -- ACM handles renewal automatically 60 days before expiration.

## Step 1 -- Request an ACM Certificate

Create the following files in your exercise directory:

### `providers.tf`

```hcl
terraform {
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
  default     = "saa-ex70"
}

variable "domain_name" {
  type        = string
  description = "Domain name for the certificate (e.g., app.example.com)"
  default     = "app.example.com"
}

variable "hosted_zone_id" {
  type        = string
  description = "Route 53 hosted zone ID for DNS validation"
  default     = "Z1234567890ABC"
}
```

### `dns.tf`

```hcl
# ------------------------------------------------------------------
# ACM Certificate: Request a public TLS certificate.
# ACM certificates are FREE for use with ALB, CloudFront,
# API Gateway, and other integrated services.
#
# Validation methods:
#   DNS: Create a CNAME record (auto-renews, preferred)
#   Email: Send validation email to domain contacts (manual renewal)
#
# Always use DNS validation unless you cannot modify DNS records.
# ------------------------------------------------------------------
resource "aws_acm_certificate" "this" {
  domain_name       = var.domain_name
  validation_method = "DNS"

  # Subject Alternative Names: additional domains covered by
  # the same certificate. Wildcard covers all subdomains.
  subject_alternative_names = [
    "*.${var.domain_name}"
  ]

  # Required: Terraform creates the new certificate before
  # destroying the old one during replacement. Without this,
  # the ALB would briefly have no certificate.
  lifecycle {
    create_before_destroy = true
  }

  tags = { Name = "${var.project_name}-certificate" }
}

# ------------------------------------------------------------------
# DNS Validation: Create the CNAME record that proves you own
# the domain. ACM checks this record during validation and
# renewal (every 60 days before expiration).
#
# The for_each handles multiple domain names (primary + SANs).
# ------------------------------------------------------------------
resource "aws_route53_record" "validation" {
  for_each = {
    for dvo in aws_acm_certificate.this.domain_validation_options : dvo.domain_name => {
      name   = dvo.resource_record_name
      type   = dvo.resource_record_type
      record = dvo.resource_record_value
    }
  }

  allow_overwrite = true
  name            = each.value.name
  type            = each.value.type
  zone_id         = var.hosted_zone_id
  records         = [each.value.record]
  ttl             = 60
}

# ------------------------------------------------------------------
# Certificate Validation: Wait for ACM to verify the DNS records
# and issue the certificate. This can take a few minutes.
# ------------------------------------------------------------------
resource "aws_acm_certificate_validation" "this" {
  certificate_arn         = aws_acm_certificate.this.arn
  validation_record_fqdns = [for record in aws_route53_record.validation : record.fqdn]
}
```

## Step 2 -- ALB HTTPS Configuration (Reference)

```hcl
# HTTPS Listener: terminates TLS using ACM certificate
# resource "aws_lb_listener" "https" {
#   load_balancer_arn = aws_lb.this.arn
#   port = 443; protocol = "HTTPS"
#   ssl_policy      = "ELBSecurityPolicy-TLS13-1-2-2021-06"
#   certificate_arn = aws_acm_certificate_validation.this.certificate_arn
#   default_action { type = "forward"; target_group_arn = aws_lb_target_group.app.arn }
# }

# HTTP Listener: redirect to HTTPS
# resource "aws_lb_listener" "http_redirect" {
#   load_balancer_arn = aws_lb.this.arn
#   port = 80; protocol = "HTTP"
#   default_action {
#     type = "redirect"
#     redirect { port = "443"; protocol = "HTTPS"; status_code = "HTTP_301" }
#   }
# }
```

## Step 3 -- CloudFront Certificate (Reference)

```hcl
# CloudFront certificates MUST be in us-east-1
# provider "aws" { alias = "us_east_1"; region = "us-east-1" }
# resource "aws_acm_certificate" "cloudfront" {
#   provider = aws.us_east_1; domain_name = "cdn.example.com"
#   validation_method = "DNS"
#   lifecycle { create_before_destroy = true }
# }
```

## Step 4 -- Outputs

### `outputs.tf`

```hcl
output "certificate_arn" {
  value = aws_acm_certificate.this.arn
}

output "certificate_domain" {
  value = aws_acm_certificate.this.domain_name
}

output "certificate_status" {
  value = aws_acm_certificate.this.status
}

output "validation_records" {
  value = [for dvo in aws_acm_certificate.this.domain_validation_options : {
    name   = dvo.resource_record_name
    type   = dvo.resource_record_type
    record = dvo.resource_record_value
  }]
}
```

```bash
terraform init
terraform apply -auto-approve
```

## Common Mistakes

### 1. CloudFront certificate in the wrong region

**Wrong approach:** Creating the ACM certificate in the same region as your other resources (e.g., us-west-2) and trying to use it with CloudFront:

```hcl
provider "aws" {
  region = "us-west-2"
}

resource "aws_acm_certificate" "this" {
  domain_name = "cdn.example.com"
  # Created in us-west-2
}

resource "aws_cloudfront_distribution" "this" {
  viewer_certificate {
    acm_certificate_arn = aws_acm_certificate.this.arn
    # ERROR: CloudFront requires certificate in us-east-1
  }
}
```

**What happens:** CloudFront rejects the certificate with an error because it only accepts certificates from us-east-1. This is because CloudFront is a global service that uses us-east-1 as its control plane region.

**Fix:** Use a separate provider aliased to us-east-1 for CloudFront certificates:

```hcl
provider "aws" {
  alias  = "us_east_1"
  region = "us-east-1"
}

resource "aws_acm_certificate" "cloudfront" {
  provider    = aws.us_east_1
  domain_name = "cdn.example.com"
  # Created in us-east-1 -- works with CloudFront
}
```

### 2. Using email validation instead of DNS

**Wrong approach:** Choosing email validation when you have Route 53 access:

```hcl
resource "aws_acm_certificate" "this" {
  validation_method = "EMAIL"
}
```

**What happens:** Email validation requires someone to click a link in a validation email sent to domain contacts (admin@, postmaster@, etc.). The certificate does NOT auto-renew -- someone must manually respond to the renewal email every 13 months. If the email is missed, the certificate expires and HTTPS breaks.

**Fix:** Always use DNS validation when possible. DNS-validated certificates auto-renew indefinitely as long as the validation CNAME record remains in DNS.

### 3. Forgetting to wait for validation before attaching to ALB

**Wrong approach:** Referencing the certificate ARN directly without waiting for validation:

```hcl
resource "aws_lb_listener" "https" {
  certificate_arn = aws_acm_certificate.this.arn
  # Certificate may not be validated yet!
}
```

**What happens:** The certificate is in PENDING_VALIDATION state. The ALB rejects it, and the listener creation fails.

**Fix:** Reference the validation resource, which waits for the certificate to be issued:

```hcl
resource "aws_lb_listener" "https" {
  certificate_arn = aws_acm_certificate_validation.this.certificate_arn
  # Waits for certificate to be validated and issued
}
```

## Verify What You Learned

```bash
# Verify certificate was created
aws acm describe-certificate \
  --certificate-arn "$(terraform output -raw certificate_arn)" \
  --query 'Certificate.{Domain:DomainName,Status:Status,Type:Type,InUseBy:InUseBy,ValidationMethod:DomainValidationOptions[0].ValidationMethod}' \
  --output json
```

Expected: Status = `ISSUED` (if DNS validation succeeded) or `PENDING_VALIDATION`.

```bash
# List all certificates in the region
aws acm list-certificates \
  --query 'CertificateSummaryList[*].{Domain:DomainName,Status:Status}' \
  --output table
```

Expected: Your certificate listed.

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

Exercise 71 covers **Amazon GuardDuty for threat detection**. You will enable GuardDuty's ML-based analysis of VPC Flow Logs, CloudTrail events, and DNS queries, generate sample findings, and configure EventBridge rules to route high-severity findings to SNS for alerting -- the security monitoring foundation that every production architecture requires.

## Summary

- **ACM public certificates are free** for use with ALB, CloudFront, API Gateway, and other integrated AWS services
- **DNS validation is preferred** over email validation -- DNS-validated certificates auto-renew; email-validated require manual action every 13 months
- **CloudFront requires certificates in us-east-1** -- use a separate provider alias for CloudFront certificates when your infrastructure is in another region
- **ALB certificates must be in the same region** as the ALB -- each ALB region needs its own certificate
- **HTTP-to-HTTPS redirect** should always be configured on port 80 -- return HTTP 301 to the HTTPS URL
- **TLS termination at ALB** is the standard pattern -- traffic from ALB to targets is HTTP within the VPC
- **SSL policy** controls the minimum TLS version and ciphers -- use `ELBSecurityPolicy-TLS13-1-2-2021-06` or newer for security compliance
- **Wildcard certificates** (`*.example.com`) cover all first-level subdomains -- they do not cover the apex domain or deeper subdomains
- **ACM certificates cannot be exported** -- they can only be used with integrated AWS services; use ACM Private CA for EC2-installed certificates
- **create_before_destroy lifecycle** prevents HTTPS downtime during certificate replacement

## Reference

- [ACM User Guide](https://docs.aws.amazon.com/acm/latest/userguide/acm-overview.html)
- [DNS Validation](https://docs.aws.amazon.com/acm/latest/userguide/dns-validation.html)
- [ALB HTTPS Listeners](https://docs.aws.amazon.com/elasticloadbalancing/latest/application/create-https-listener.html)
- [Terraform aws_acm_certificate](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/acm_certificate)

## Additional Resources

- [ACM Certificate Renewal](https://docs.aws.amazon.com/acm/latest/userguide/managed-renewal.html) -- how automatic renewal works and troubleshooting failed renewals
- [CloudFront SSL/TLS](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/using-https.html) -- HTTPS configuration for CloudFront distributions
- [ACM Private CA](https://docs.aws.amazon.com/privateca/latest/userguide/PcaWelcome.html) -- private certificate authority for internal services ($400/month)
- [SSL Security Policies](https://docs.aws.amazon.com/elasticloadbalancing/latest/application/create-https-listener.html#describe-ssl-policies) -- comparison of ALB SSL policies and their cipher suites

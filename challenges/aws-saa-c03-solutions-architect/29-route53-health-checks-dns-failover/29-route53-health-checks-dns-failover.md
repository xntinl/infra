# 29. Route 53 Health Checks and DNS Failover

<!--
difficulty: intermediate
concepts: [route53-health-checks, http-health-check, tcp-health-check, calculated-health-check, cloudwatch-alarm-health-check, dns-failover, health-check-regions, string-matching]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: apply, evaluate
prerequisites: [28-route53-routing-policies-comparison]
aws_cost: ~$0.03/hr
-->

> **AWS Cost Warning:** Each Route 53 health check costs $0.50/month (basic HTTP) or $0.75/month (HTTPS, string matching). CloudWatch alarms cost $0.10/alarm/month. EC2 instances (if launched) cost ~$0.0104/hr. Total ~$0.03/hr. Remember to run `terraform destroy` when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| Terraform >= 1.7 installed | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Understanding of Route 53 routing policies (exercise 28) | Failover routing, health check association |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Construct** Route 53 health checks of all types: HTTP, HTTPS, TCP, calculated, and CloudWatch alarm-based.
2. **Configure** failover routing with health checks for active-passive high availability.
3. **Design** calculated health checks that combine multiple child checks with AND/OR logic.
4. **Integrate** CloudWatch alarms with Route 53 health checks for monitoring private resources.
5. **Analyze** health check timing: how request_interval, failure_threshold, and checker regions affect failover speed.
6. **Evaluate** the trade-off between fast failover (low threshold) and stability (high threshold, fewer false positives).

---

## Why This Matters

Route 53 health checks are the mechanism that enables DNS-based failover. Without health checks, routing policies like Failover and Weighted have no signal to determine whether an endpoint is healthy. The SAA-C03 exam tests several specific scenarios.

First, health check types: HTTP/HTTPS/TCP health checks monitor endpoints directly from Route 53 health checker nodes across the globe. They cannot reach private IP addresses (because health checkers are external to your VPC). For private resources, you must use CloudWatch alarm-based health checks: your application publishes a custom metric or CloudWatch monitors an internal resource, and the alarm state drives the health check status.

Second, calculated health checks combine multiple child health checks using boolean logic. If your application depends on three microservices, a calculated health check can report unhealthy when any one child fails (AND logic) or when a majority fail (threshold). This aggregation prevents false positives from single-service blips while detecting genuine outages.

Third, the health check timing math matters for the exam. With `request_interval = 10` seconds and `failure_threshold = 3`, failover takes approximately 30 seconds. The exam may ask: "The current failover takes 90 seconds. How can you reduce it?" -- lower the interval (minimum 10s) or reduce the failure threshold (minimum 1). But lower thresholds increase false positives from transient network issues.

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
  default     = "hc-demo"
}
```

### `vpc.tf`

```hcl
data "aws_availability_zones" "available" {
  state = "available"
}

resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags                 = { Name = "${var.project_name}-vpc" }
}

resource "aws_subnet" "public" {
  vpc_id                  = aws_vpc.this.id
  cidr_block              = "10.0.1.0/24"
  availability_zone       = data.aws_availability_zones.available.names[0]
  map_public_ip_on_launch = true
  tags                    = { Name = "${var.project_name}-public" }
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
  subnet_id      = aws_subnet.public.id
  route_table_id = aws_route_table.public.id
}
```

### `security.tf`

```hcl
resource "aws_security_group" "web" {
  name_prefix = "${var.project_name}-web-"
  vpc_id      = aws_vpc.this.id

  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "HTTP from Route 53 health checkers"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "${var.project_name}-web-sg" }
}
```

### `dns.tf`

```hcl
# ============================================================
# TODO 1: HTTP Health Check
# ============================================================
# Create a basic HTTP health check that monitors a web endpoint.
#
# Requirements:
#   - Resource: aws_route53_health_check
#   - type = "HTTP"
#   - fqdn = "example.com" (or use ip_address for IP-based)
#   - port = 80
#   - resource_path = "/health"
#   - failure_threshold = 3 (3 consecutive failures = unhealthy)
#   - request_interval = 10 (check every 10 seconds)
#
# Optional: Add string matching to verify response content:
#   - search_string = "OK"
#   - This confirms the endpoint returns expected content,
#     not just a 2xx status code. Catches scenarios where
#     a misconfigured load balancer returns 200 with an error page.
#
# Health checkers run from multiple regions. You can restrict
# which regions check your endpoint using:
#   - regions = ["us-east-1", "eu-west-1", "ap-southeast-1"]
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route53_health_check
# ============================================================


# ============================================================
# TODO 2: HTTPS Health Check with String Matching
# ============================================================
# Create an HTTPS health check with response body verification.
#
# Requirements:
#   - type = "HTTPS"
#   - fqdn = "example.com"
#   - port = 443
#   - resource_path = "/api/status"
#   - search_string = "\"status\":\"healthy\""
#   - failure_threshold = 2
#   - request_interval = 30
#
# String matching adds $0.25/month to the health check cost
# but catches more failure modes than status code alone.
# The search_string must appear in the first 5,120 bytes
# of the response body.
# ============================================================


# ============================================================
# TODO 3: TCP Health Check
# ============================================================
# Create a TCP health check for a non-HTTP service.
#
# Requirements:
#   - type = "TCP"
#   - ip_address = "198.51.100.1" (example IP)
#   - port = 3306 (MySQL)
#   - failure_threshold = 3
#   - request_interval = 10
#
# TCP health checks verify that a TCP connection can be
# established. They do not check the response content.
# Use for databases, Redis, custom TCP services.
# ============================================================


# ============================================================
# TODO 4: Calculated Health Check
# ============================================================
# Create a calculated health check that aggregates multiple
# child health checks.
#
# Requirements:
#   - Resource: aws_route53_health_check
#   - type = "CALCULATED"
#   - child_health_checks = [list of child health check IDs]
#   - child_healthchecks_are_unhealthy_threshold = 1
#     (report unhealthy when 1 or more children are unhealthy)
#
# Threshold logic:
#   - threshold = 1: unhealthy if ANY child fails (strictest)
#   - threshold = N: unhealthy if N children fail
#   - threshold = len(children): unhealthy if ALL fail (most lenient)
#
# Use case: application depends on 3 microservices. Set threshold
# to 2 -- unhealthy if 2 of 3 services are down. Tolerate
# single-service transient failures.
#
# Docs: https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/health-checks-creating-values.html#health-checks-creating-values-calculated
# ============================================================


# ============================================================
# TODO 5: CloudWatch Alarm-Based Health Check
# ============================================================
# For monitoring private resources (inside VPC), create a
# CloudWatch alarm and link it to a Route 53 health check.
#
# Requirements:
#   - Resource: aws_cloudwatch_metric_alarm
#     - alarm_name = "${var.project_name}-private-service"
#     - metric_name = "HealthyHostCount"
#     - namespace = "AWS/ApplicationELB"
#     - comparison_operator = "LessThanThreshold"
#     - threshold = 1
#     - evaluation_periods = 2
#     - period = 60
#
#   - Resource: aws_route53_health_check
#     - type = "CLOUDWATCH_METRIC"
#     - cloudwatch_alarm_name = alarm name
#     - cloudwatch_alarm_region = "us-east-1"
#     - insufficient_data_health_status = "Unhealthy"
#
# This is the ONLY way to health-check private resources.
# Route 53 health checkers cannot reach private IPs because
# they run outside your VPC.
#
# Docs: https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/health-checks-creating-values.html#health-checks-creating-values-cloudwatch
# ============================================================


# ============================================================
# TODO 6: Failover Record Set with Health Check
# ============================================================
# Create a failover DNS record pair (primary + secondary) that
# uses the HTTP health check from TODO 1.
#
# Requirements:
#   - Create a Route 53 zone (private or use existing)
#   - 2x aws_route53_record with failover_routing_policy
#   - PRIMARY record: health_check_id = HTTP health check ID
#   - SECONDARY record: optionally add health check
#
# When the primary's health check fails, Route 53 returns
# the secondary record. DNS TTL should be low (60s) so
# clients pick up the change quickly.
# ============================================================
```

### `outputs.tf`

```hcl
output "vpc_id" {
  value = aws_vpc.this.id
}
```

---

## Spot the Bug

A team creates Route 53 health checks but reports false positives -- the primary endpoint is healthy but the health check intermittently shows unhealthy, causing unnecessary failovers:

```hcl
resource "aws_route53_health_check" "primary" {
  ip_address        = "198.51.100.10"
  port              = 443
  type              = "HTTPS"
  resource_path     = "/api/deep-health"
  failure_threshold = 1
  request_interval  = 10

  regions = ["us-east-1"]  # <-- Bug: single region
}
```

<details>
<summary>Explain the bug</summary>

There are two problems in this configuration.

**Problem 1: `failure_threshold = 1` with `request_interval = 10`.**

A single failed check triggers unhealthy status. Network blips, transient DNS issues, or a single slow response (>4 second timeout for TCP, >2 seconds for HTTP) cause an immediate health status change. With failover routing, this triggers DNS failover for a momentary issue.

**Problem 2: `regions = ["us-east-1"]` -- health checks from a single region.**

Route 53 health checkers run from multiple regions. Using a single region means a regional network issue (not an application problem) can cause false unhealthy status. AWS recommends using at least 3 regions. If only one region is specified and that region has a network issue, the health check fails even though the application is perfectly healthy from all other regions.

**Problem 3: `resource_path = "/api/deep-health"` -- deep health checks.**

If `/api/deep-health` checks database connectivity, external API dependencies, or downstream services, a slow dependency causes the health check to fail even though the primary application is serving traffic fine. Deep health checks are useful for internal monitoring but dangerous for DNS failover health checks.

**The fix:**

```hcl
resource "aws_route53_health_check" "primary" {
  ip_address        = "198.51.100.10"
  port              = 443
  type              = "HTTPS"
  resource_path     = "/health"          # Shallow check (returns 200 immediately)
  failure_threshold = 3                   # 3 consecutive failures required
  request_interval  = 10

  # Use at least 3 regions for accurate health assessment
  # Omit 'regions' to use all available regions (recommended)
}
```

Best practices for DNS failover health checks:
- Use a **shallow health check** (`/health` that returns 200 without dependency checks) for DNS routing
- Use **deep health checks** for operational alerting (CloudWatch, PagerDuty)
- Set `failure_threshold >= 2` to absorb transient failures
- Use multiple health checker regions (omit `regions` to use all)

</details>

---

## Health Check Timing Formula

```
failover_time = request_interval * failure_threshold + DNS_TTL

Example:
  request_interval = 10s
  failure_threshold = 3
  DNS_TTL = 60s

  failover_time = 10 * 3 + 60 = 90 seconds (worst case)
```

| Interval | Threshold | Detection Time | + 60s TTL | Trade-off |
|----------|-----------|---------------|-----------|-----------|
| 30s | 3 | 90s | 150s | Stable, slow failover |
| 10s | 3 | 30s | 90s | Balanced |
| 10s | 2 | 20s | 80s | Fast, some false positives |
| 10s | 1 | 10s | 70s | Fastest, most false positives |

### Health Check Cost Comparison

| Type | Monthly Cost | Notes |
|------|-------------|-------|
| HTTP (basic) | $0.50 | Status code check only |
| HTTPS (basic) | $0.75 | TLS negotiation included |
| HTTP + string matching | $0.75 | Checks response body |
| HTTPS + string matching | $1.00 | TLS + body check |
| TCP | $0.50 | Connection check only |
| Calculated | $0.50 | Aggregates children |
| CloudWatch alarm-based | $0.50 | Uses alarm state |

---

## Solutions

<details>
<summary>TODO 1 -- HTTP Health Check -- `dns.tf`</summary>

```hcl
resource "aws_route53_health_check" "http" {
  fqdn              = "example.com"
  port              = 80
  type              = "HTTP"
  resource_path     = "/health"
  failure_threshold = 3
  request_interval  = 10

  tags = { Name = "${var.project_name}-http" }
}
```

The health check runs from Route 53 health checker nodes in multiple regions. An endpoint is healthy when 18% or more of health checkers report healthy (configurable via `health_threshold` on calculated checks).

</details>

<details>
<summary>TODO 2 -- HTTPS Health Check with String Matching -- `dns.tf`</summary>

```hcl
resource "aws_route53_health_check" "https_string" {
  fqdn              = "example.com"
  port              = 443
  type              = "HTTPS_STR_MATCH"
  resource_path     = "/api/status"
  search_string     = "\"status\":\"healthy\""
  failure_threshold = 2
  request_interval  = 30

  tags = { Name = "${var.project_name}-https-string" }
}
```

String matching verifies the response body contains the expected string. This catches scenarios where a load balancer returns 200 but serves an error page, or where the application returns 200 with `{"status":"degraded"}`.

</details>

<details>
<summary>TODO 3 -- TCP Health Check -- `dns.tf`</summary>

```hcl
resource "aws_route53_health_check" "tcp" {
  ip_address        = "198.51.100.1"
  port              = 3306
  type              = "TCP"
  failure_threshold = 3
  request_interval  = 10

  tags = { Name = "${var.project_name}-tcp" }
}
```

TCP health checks verify that a TCP connection can be established within the timeout. Use for non-HTTP services (databases, Redis, custom protocols).

</details>

<details>
<summary>TODO 4 -- Calculated Health Check -- `dns.tf`</summary>

```hcl
resource "aws_route53_health_check" "calculated" {
  type = "CALCULATED"

  child_health_checks = [
    aws_route53_health_check.http.id,
    aws_route53_health_check.https_string.id,
    aws_route53_health_check.tcp.id,
  ]

  child_healthchecks_are_unhealthy_threshold = 2

  tags = { Name = "${var.project_name}-calculated" }
}
```

With threshold = 2, the calculated check reports unhealthy when 2 of 3 children fail. This tolerates a single-service blip while detecting a broader outage. Set threshold = 1 for the strictest policy (any failure = unhealthy) or threshold = 3 for the most lenient (all must fail).

</details>

<details>
<summary>TODO 5 -- CloudWatch Alarm-Based Health Check -- `monitoring.tf`</summary>

```hcl
resource "aws_cloudwatch_metric_alarm" "private_service" {
  alarm_name          = "${var.project_name}-private-service"
  comparison_operator = "LessThanThreshold"
  evaluation_periods  = 2
  metric_name         = "HealthyHostCount"
  namespace           = "AWS/ApplicationELB"
  period              = 60
  statistic           = "Minimum"
  threshold           = 1
  alarm_description   = "No healthy targets behind ALB"

  dimensions = {
    LoadBalancer = "app/example/50dc6c495c0c9188"
    TargetGroup  = "targetgroup/example/73e2d6bc24d8a067"
  }
}

resource "aws_route53_health_check" "cloudwatch" {
  type                            = "CLOUDWATCH_METRIC"
  cloudwatch_alarm_name           = aws_cloudwatch_metric_alarm.private_service.alarm_name
  cloudwatch_alarm_region         = var.region
  insufficient_data_health_status = "Unhealthy"

  tags = { Name = "${var.project_name}-cloudwatch" }
}
```

`insufficient_data_health_status = "Unhealthy"` means if CloudWatch has no data (metric not published), the health check reports unhealthy. This is the safe default -- fail closed rather than fail open.

</details>

<details>
<summary>TODO 6 -- Failover Record Set -- `dns.tf`</summary>

```hcl
resource "aws_route53_zone" "this" {
  name = "hc-demo.internal"

  vpc {
    vpc_id = aws_vpc.this.id
  }
}

resource "aws_route53_record" "primary" {
  zone_id = aws_route53_zone.this.zone_id
  name    = "app.hc-demo.internal"
  type    = "A"
  ttl     = 60

  set_identifier = "primary"
  records        = ["10.0.1.10"]

  failover_routing_policy {
    type = "PRIMARY"
  }

  health_check_id = aws_route53_health_check.http.id
}

resource "aws_route53_record" "secondary" {
  zone_id = aws_route53_zone.this.zone_id
  name    = "app.hc-demo.internal"
  type    = "A"
  ttl     = 60

  set_identifier = "secondary"
  records        = ["10.0.2.10"]

  failover_routing_policy {
    type = "SECONDARY"
  }
}
```

Low TTL (60s) is important: after Route 53 switches from primary to secondary, clients must expire their cached DNS response before resolving to the secondary IP.

</details>

---

## Verify What You Learned

```bash
# Verify health checks exist
aws route53 list-health-checks \
  --query "HealthChecks[*].{Id:Id,Type:HealthCheckConfig.Type,FQDN:HealthCheckConfig.FullyQualifiedDomainName}" \
  --output table
```

Expected: Multiple health checks of different types (HTTP, HTTPS_STR_MATCH, TCP, CALCULATED, CLOUDWATCH_METRIC).

```bash
# Check health status of a specific health check
HC_ID=$(aws route53 list-health-checks \
  --query "HealthChecks[?HealthCheckConfig.Type=='HTTP'].Id" \
  --output text | head -1)

aws route53 get-health-check-status \
  --health-check-id "$HC_ID" \
  --query "HealthCheckObservations[*].{Region:Region,Status:StatusReport.Status}" \
  --output table
```

Expected: Status reports from multiple regions.

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify:

```bash
terraform state list
```

Expected: no output (empty state).

---

## What's Next

You have mastered Route 53 health checks and DNS failover patterns. In the next exercise, you will set up **AWS Client VPN** for secure remote access to VPC resources, including mutual TLS authentication, authorization rules, and the split-tunnel vs full-tunnel decision.

---

## Summary

- Route 53 health checks support **HTTP, HTTPS, TCP, Calculated, and CloudWatch alarm-based** types
- Health checkers run from **multiple AWS regions**; use 3+ regions to avoid false positives from regional network issues
- **Failover time** = request_interval x failure_threshold + DNS TTL; tune each parameter to meet RTO requirements
- **Calculated health checks** aggregate children with configurable threshold (AND/OR logic)
- **CloudWatch alarm-based** health checks are the only way to monitor **private resources** (health checkers cannot reach private IPs)
- Use **shallow health checks** (/health returning 200) for DNS failover; use deep checks for operational alerts
- **String matching** catches more failure modes than status code alone ($0.25/month extra)
- `insufficient_data_health_status = "Unhealthy"` is the safe default (fail closed)

## Reference

- [Route 53 Health Checks](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/dns-failover.html)
- [Health Check Types](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/health-checks-types.html)
- [Monitoring Health Check Status](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/health-checks-monitor-view-status.html)
- [Terraform aws_route53_health_check](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route53_health_check)

## Additional Resources

- [Route 53 Health Check IP Ranges](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/route-53-ip-addresses.html) -- IP ranges to whitelist in security groups for health checker access
- [Calculated Health Checks](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/dns-failover-complex-configs.html) -- complex failover configurations with multiple health check layers
- [CloudWatch Integration with Route 53](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/monitoring-health-checks.html) -- monitoring health check status via CloudWatch metrics and alarms
- [DNS Failover Patterns](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/dns-failover-configuring.html) -- active-passive, active-active, and multi-tier failover architectures

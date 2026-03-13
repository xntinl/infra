# 28. Route 53 Routing Policies Comparison

<!--
difficulty: intermediate
concepts: [route53, simple-routing, weighted-routing, latency-routing, failover-routing, geolocation-routing, geoproximity-routing, multivalue-routing, health-checks]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: apply, evaluate
prerequisites: [17-vpc-subnets-route-tables-igw]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** Route 53 hosted zone costs $0.50/month. DNS queries cost $0.40/million for standard records. Health checks cost $0.50-$0.75/month each. Total ~$0.01/hr during testing. Remember to run `terraform destroy` when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| Terraform >= 1.7 installed | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| A registered domain or willingness to use a Route 53 public hosted zone for testing | Understanding of DNS basics |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Distinguish** all seven Route 53 routing policies and identify when each is the correct choice.
2. **Implement** Simple, Weighted, Latency, Failover, Geolocation, Geoproximity, and Multivalue Answer records using Terraform.
3. **Evaluate** which routing policy satisfies a given set of requirements (availability, performance, compliance, cost).
4. **Design** multi-policy architectures that combine routing policies for complex traffic management.
5. **Explain** the relationship between routing policies and health checks.
6. **Compare** latency-based routing (best performance) with geolocation routing (compliance/localization).

---

## Why This Matters

Route 53 routing policies are one of the most frequently tested topics on the SAA-C03. The exam presents scenarios with specific requirements -- "minimize latency for global users," "comply with data sovereignty regulations," "perform blue-green deployments," "implement active-passive failover" -- and expects you to choose the correct routing policy. Each policy solves a specific problem, and choosing the wrong one either violates requirements or introduces unnecessary complexity.

The critical distinction is between latency-based routing and geolocation routing. Latency routing sends users to the AWS region with the lowest network latency -- optimal for performance. Geolocation routing sends users based on their geographic location (continent, country, or US state) -- required for data sovereignty, localization, or content licensing. A user in Germany with lower latency to US-East would still be routed to EU-West under geolocation policy if the rule maps Germany to EU. The exam tests whether you can identify which requirement (performance vs compliance) drives the policy choice.

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
  default     = "r53-policies-demo"
}
```

### `vpc.tf`

```hcl
resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags                 = { Name = "${var.project_name}-vpc" }
}
```

### `dns.tf`

```hcl
# Private hosted zone (no domain registration needed)
resource "aws_route53_zone" "this" {
  name = "demo.internal"

  vpc {
    vpc_id = aws_vpc.this.id
  }

  tags = { Name = var.project_name }
}

# ==================================================================
# 1. SIMPLE ROUTING
# ==================================================================
# Returns all values in random order. No health checks.
# Use case: single resource or multiple resources with no preference.
resource "aws_route53_record" "simple" {
  zone_id = aws_route53_zone.this.zone_id
  name    = "simple.demo.internal"
  type    = "A"
  ttl     = 300
  records = ["10.0.1.10", "10.0.1.11", "10.0.1.12"]
}

# ============================================================
# TODO 1: Weighted Routing
# ============================================================
# Create weighted records to distribute traffic between two
# endpoints at a 70/30 ratio (canary deployment pattern).
#
# Requirements:
#   - 2x aws_route53_record with routing_policy = (implicit via set_identifier)
#   - name = "weighted.demo.internal"
#   - set_identifier = "primary" and "canary"
#   - weighted_routing_policy { weight = 70 } and { weight = 30 }
#   - type = "A", ttl = 60
#   - records = ["10.0.1.20"] and ["10.0.1.21"]
#
# Weights are relative: 70/(70+30) = 70% to primary.
# Setting weight = 0 stops all traffic to that endpoint.
# Use case: canary deployments, A/B testing, gradual migrations.
#
# Docs: https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/routing-policy-weighted.html
# ============================================================


# ============================================================
# TODO 2: Latency Routing
# ============================================================
# Create latency records that route to the region with lowest
# network latency from the client.
#
# Requirements:
#   - 2x aws_route53_record
#   - name = "latency.demo.internal"
#   - set_identifier = "us-east-1" and "eu-west-1"
#   - latency_routing_policy { region = "us-east-1" } and
#     { region = "eu-west-1" }
#   - records = ["10.0.1.30"] and ["10.0.2.30"]
#
# Route 53 measures latency from the client's recursive resolver
# to each AWS region and returns the record for the lowest-latency
# region. This is the best routing policy for performance.
#
# Docs: https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/routing-policy-latency.html
# ============================================================


# ============================================================
# TODO 3: Failover Routing
# ============================================================
# Create failover records with primary and secondary endpoints.
#
# Requirements:
#   - 2x aws_route53_record
#   - name = "failover.demo.internal"
#   - set_identifier = "primary" and "secondary"
#   - failover_routing_policy { type = "PRIMARY" } and
#     { type = "SECONDARY" }
#   - records = ["10.0.1.40"] (primary) and ["10.0.2.40"] (secondary)
#   - health_check_id on the PRIMARY record (see TODO 4)
#
# Route 53 returns the primary record when its health check passes.
# When the primary health check fails, Route 53 returns the
# secondary record. The secondary does NOT require a health check
# (it is the last resort), but adding one is best practice.
#
# CRITICAL: Failover without a health check on the primary is
# useless -- Route 53 always returns the primary because it has
# no signal that the primary is down.
# ============================================================


# ============================================================
# TODO 4: Health Check for Failover
# ============================================================
# Create a Route 53 health check that monitors the primary endpoint.
#
# Requirements:
#   - Resource: aws_route53_health_check
#   - type = "HTTP"
#   - fqdn = "primary.example.com" (or use ip_address for IP-based)
#   - port = 80
#   - resource_path = "/health"
#   - failure_threshold = 3
#   - request_interval = 10
#
# Health checkers run from multiple AWS regions. An endpoint is
# considered unhealthy when failure_threshold consecutive checks
# fail. With request_interval = 10 and failure_threshold = 3,
# failover triggers in ~30 seconds.
#
# Docs: https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/dns-failover.html
# ============================================================


# ============================================================
# TODO 5: Geolocation Routing
# ============================================================
# Route users based on geographic location.
#
# Requirements:
#   - 3x aws_route53_record
#   - name = "geo.demo.internal"
#   - Geolocation rules:
#     - continent = "EU" -> 10.0.3.50 (European users)
#     - country = "US" -> 10.0.1.50 (US users)
#     - Default (*) -> 10.0.1.51 (everyone else)
#   - geolocation_routing_policy { continent = "EU" }
#   - geolocation_routing_policy { country = "US" }
#   - geolocation_routing_policy { } (default, no location specified)
#
# IMPORTANT: Always create a default record. Without it, users
# from unmapped locations get NXDOMAIN (no DNS response).
#
# Use case: data sovereignty (keep EU data in EU), content
# licensing (restrict by country), localization.
# ============================================================


# ==================================================================
# 6. GEOPROXIMITY ROUTING (requires Traffic Flow)
# ==================================================================
# Routes based on geographic location of resources AND a bias value.
# Bias shifts traffic toward or away from a resource.
# Requires Route 53 Traffic Flow ($50/month per policy record).
# Cannot be created with standard aws_route53_record -- use
# aws_route53_traffic_policy instead.
# Typically tested as a concept on the exam, not a hands-on lab.

# ==================================================================
# 7. MULTIVALUE ANSWER ROUTING
# ==================================================================
# Returns up to 8 healthy records in random order.
# Like Simple routing but WITH health checks.
resource "aws_route53_record" "multivalue_a" {
  zone_id = aws_route53_zone.this.zone_id
  name    = "multi.demo.internal"
  type    = "A"
  ttl     = 60

  set_identifier = "endpoint-a"
  records        = ["10.0.1.60"]

  multivalue_answer_routing_policy = true
}

resource "aws_route53_record" "multivalue_b" {
  zone_id = aws_route53_zone.this.zone_id
  name    = "multi.demo.internal"
  type    = "A"
  ttl     = 60

  set_identifier = "endpoint-b"
  records        = ["10.0.1.61"]

  multivalue_answer_routing_policy = true
}

resource "aws_route53_record" "multivalue_c" {
  zone_id = aws_route53_zone.this.zone_id
  name    = "multi.demo.internal"
  type    = "A"
  ttl     = 60

  set_identifier = "endpoint-c"
  records        = ["10.0.1.62"]

  multivalue_answer_routing_policy = true
}
```

### `outputs.tf`

```hcl
output "zone_id" {
  value = aws_route53_zone.this.zone_id
}

output "vpc_id" {
  value = aws_vpc.this.id
}
```

---

## Spot the Bug

A team creates failover routing but the secondary endpoint never receives traffic, even when the primary is down:

```hcl
resource "aws_route53_record" "primary" {
  zone_id = aws_route53_zone.this.zone_id
  name    = "app.demo.internal"
  type    = "A"
  ttl     = 60

  set_identifier = "primary"
  records        = ["10.0.1.40"]

  failover_routing_policy {
    type = "PRIMARY"
  }

  # No health_check_id! <-- Bug
}

resource "aws_route53_record" "secondary" {
  zone_id = aws_route53_zone.this.zone_id
  name    = "app.demo.internal"
  type    = "A"
  ttl     = 60

  set_identifier = "secondary"
  records        = ["10.0.2.40"]

  failover_routing_policy {
    type = "SECONDARY"
  }
}
```

<details>
<summary>Explain the bug</summary>

**Failover routing without a health check on the primary record is useless.** Route 53 has no way to determine that the primary is down. Without a health check, Route 53 assumes the primary is always healthy and always returns the primary record. The secondary endpoint never receives traffic.

This is one of the most common Route 53 misconfigurations and a frequent exam question. The scenario describes "failover is configured but not working" -- the answer is always "health check is missing on the primary."

**The fix:**

```hcl
resource "aws_route53_health_check" "primary" {
  fqdn              = "primary.example.com"
  port              = 80
  type              = "HTTP"
  resource_path     = "/health"
  failure_threshold = 3
  request_interval  = 10
}

resource "aws_route53_record" "primary" {
  zone_id = aws_route53_zone.this.zone_id
  name    = "app.demo.internal"
  type    = "A"
  ttl     = 60

  set_identifier = "primary"
  records        = ["10.0.1.40"]

  failover_routing_policy {
    type = "PRIMARY"
  }

  health_check_id = aws_route53_health_check.primary.id  # Required!
}
```

The health check runs from multiple AWS regions. When `failure_threshold` consecutive checks fail, Route 53 marks the primary as unhealthy and starts returning the secondary record. With `request_interval = 10` and `failure_threshold = 3`, failover happens in approximately 30 seconds.

</details>

---

## Routing Policy Decision Table

| Policy | Use When | Health Check? | Example |
|--------|----------|--------------|---------|
| **Simple** | Single resource, no special routing | No | Blog on one server |
| **Weighted** | Traffic splitting, canary deploys | Optional | 90% v1, 10% v2 |
| **Latency** | Minimize response time for global users | Optional | Multi-region API |
| **Failover** | Active/passive HA | **Required** on primary | DR site |
| **Geolocation** | Compliance, localization, licensing | Optional | EU users to EU region |
| **Geoproximity** | Like geolocation but with traffic bias | Optional | Shift traffic between regions |
| **Multivalue** | Simple + health checks (client-side LB) | Optional | Multiple app instances |

### Decision Framework

```
Need to comply with data sovereignty or localize content?
  -> Geolocation

Need lowest latency for global users?
  -> Latency-based

Need active/passive failover?
  -> Failover (with health check!)

Need canary deployments or weighted traffic split?
  -> Weighted

Need health-checked random distribution?
  -> Multivalue Answer

Need to shift traffic between regions with fine control?
  -> Geoproximity (requires Traffic Flow)

None of the above?
  -> Simple
```

---

## Solutions

<details>
<summary>TODO 1 -- Weighted Routing -- `dns.tf`</summary>

```hcl
resource "aws_route53_record" "weighted_primary" {
  zone_id = aws_route53_zone.this.zone_id
  name    = "weighted.demo.internal"
  type    = "A"
  ttl     = 60

  set_identifier = "primary"
  records        = ["10.0.1.20"]

  weighted_routing_policy {
    weight = 70
  }
}

resource "aws_route53_record" "weighted_canary" {
  zone_id = aws_route53_zone.this.zone_id
  name    = "weighted.demo.internal"
  type    = "A"
  ttl     = 60

  set_identifier = "canary"
  records        = ["10.0.1.21"]

  weighted_routing_policy {
    weight = 30
  }
}
```

Weights are relative: 70/(70+30) = 70% traffic to primary. Set weight = 0 to stop traffic to an endpoint without deleting the record. Useful for maintenance or rollback.

</details>

<details>
<summary>TODO 2 -- Latency Routing -- `dns.tf`</summary>

```hcl
resource "aws_route53_record" "latency_us" {
  zone_id = aws_route53_zone.this.zone_id
  name    = "latency.demo.internal"
  type    = "A"
  ttl     = 60

  set_identifier = "us-east-1"
  records        = ["10.0.1.30"]

  latency_routing_policy {
    region = "us-east-1"
  }
}

resource "aws_route53_record" "latency_eu" {
  zone_id = aws_route53_zone.this.zone_id
  name    = "latency.demo.internal"
  type    = "A"
  ttl     = 60

  set_identifier = "eu-west-1"
  records        = ["10.0.2.30"]

  latency_routing_policy {
    region = "eu-west-1"
  }
}
```

Route 53 uses a database of network latency measurements between DNS resolvers and AWS regions. It returns the record associated with the lowest-latency region. This does not guarantee the lowest latency to your application (network hops within the region matter too), but it is the best approximation at the DNS level.

</details>

<details>
<summary>TODO 3 & 4 -- Failover Routing with Health Check -- `dns.tf`</summary>

```hcl
resource "aws_route53_health_check" "primary" {
  ip_address        = "10.0.1.40"
  port              = 80
  type              = "HTTP"
  resource_path     = "/health"
  failure_threshold = 3
  request_interval  = 10

  tags = { Name = "${var.project_name}-primary-hc" }
}

resource "aws_route53_record" "failover_primary" {
  zone_id = aws_route53_zone.this.zone_id
  name    = "failover.demo.internal"
  type    = "A"
  ttl     = 60

  set_identifier = "primary"
  records        = ["10.0.1.40"]

  failover_routing_policy {
    type = "PRIMARY"
  }

  health_check_id = aws_route53_health_check.primary.id
}

resource "aws_route53_record" "failover_secondary" {
  zone_id = aws_route53_zone.this.zone_id
  name    = "failover.demo.internal"
  type    = "A"
  ttl     = 60

  set_identifier = "secondary"
  records        = ["10.0.2.40"]

  failover_routing_policy {
    type = "SECONDARY"
  }
}
```

Note: For private hosted zones, IP-based health checks on private IPs require CloudWatch alarm-based health checks instead. The above example uses IP-based health checks for illustration.

</details>

<details>
<summary>TODO 5 -- Geolocation Routing -- `dns.tf`</summary>

```hcl
resource "aws_route53_record" "geo_eu" {
  zone_id = aws_route53_zone.this.zone_id
  name    = "geo.demo.internal"
  type    = "A"
  ttl     = 60

  set_identifier = "eu"
  records        = ["10.0.3.50"]

  geolocation_routing_policy {
    continent = "EU"
  }
}

resource "aws_route53_record" "geo_us" {
  zone_id = aws_route53_zone.this.zone_id
  name    = "geo.demo.internal"
  type    = "A"
  ttl     = 60

  set_identifier = "us"
  records        = ["10.0.1.50"]

  geolocation_routing_policy {
    country = "US"
  }
}

resource "aws_route53_record" "geo_default" {
  zone_id = aws_route53_zone.this.zone_id
  name    = "geo.demo.internal"
  type    = "A"
  ttl     = 60

  set_identifier = "default"
  records        = ["10.0.1.51"]

  geolocation_routing_policy {}
}
```

The `geolocation_routing_policy {}` block with no attributes creates the default record. Always include a default -- without it, users from unmapped locations receive no DNS response (NXDOMAIN).

</details>

---

## Verify What You Learned

```bash
# Verify hosted zone exists
aws route53 list-hosted-zones-by-name \
  --dns-name "demo.internal" \
  --query "HostedZones[0].{Name:Name,Private:Config.PrivateZone}" \
  --output table
```

Expected: Name=demo.internal., Private=True.

```bash
# Verify records created
ZONE_ID=$(terraform output -raw zone_id)
aws route53 list-resource-record-sets \
  --hosted-zone-id "$ZONE_ID" \
  --query "ResourceRecordSets[?Type=='A'].{Name:Name,SetId:SetIdentifier,Weight:Weight,Failover:Failover,Region:Region}" \
  --output table
```

Expected: Records for simple, weighted, latency, failover, geo, and multivalue.

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

You deployed all major Route 53 routing policies and understand when to use each. In the next exercise, you will dive deeper into **Route 53 health checks and DNS failover** -- creating HTTP/HTTPS/TCP health checks, calculated health checks, and integrating with CloudWatch alarms for comprehensive monitoring.

---

## Summary

- **Simple**: returns all values randomly; no health checks; use for single resources
- **Weighted**: distributes traffic by ratio; use for canary deployments and A/B testing
- **Latency**: routes to lowest-latency region; use for global performance optimization
- **Failover**: active/passive with health check; **health check on primary is required or failover never triggers**
- **Geolocation**: routes by user location (continent/country/state); use for compliance and localization; **always include default record**
- **Geoproximity**: like geolocation but with bias to shift traffic; requires Traffic Flow ($50/month)
- **Multivalue Answer**: like Simple but with health checks; returns up to 8 healthy records
- Latency routing optimizes for **performance**; geolocation routing enforces **compliance** -- choose based on the requirement

## Reference

- [Route 53 Routing Policies](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/routing-policy.html)
- [Route 53 Health Checks](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/dns-failover.html)
- [Terraform aws_route53_record](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route53_record)
- [Route 53 Pricing](https://aws.amazon.com/route53/pricing/)

## Additional Resources

- [Route 53 Traffic Flow](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/traffic-flow.html) -- visual editor for complex routing policies including geoproximity
- [Choosing a Routing Policy](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/routing-policy.html) -- AWS decision guide for selecting the right policy
- [Weighted Routing for Blue/Green Deployments](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/routing-policy-weighted.html) -- gradual traffic migration patterns
- [Geolocation vs Latency Routing](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/routing-policy-latency.html) -- detailed comparison with examples

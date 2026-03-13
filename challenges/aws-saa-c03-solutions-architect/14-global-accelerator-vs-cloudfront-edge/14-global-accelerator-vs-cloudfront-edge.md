# 14. Global Accelerator vs CloudFront: Edge Strategy

<!--
difficulty: advanced
concepts: [global-accelerator, cloudfront, anycast-ip, edge-locations, health-check-failover, latency-routing, non-http-workloads]
tools: [terraform, aws-cli]
estimated_time: 75m
bloom_level: evaluate
prerequisites: [none]
aws_cost: ~$0.20/hr
-->

> **AWS Cost Warning:** Global Accelerator ($0.025/hr fixed) plus 2 ALBs in different regions and EC2 instances behind them. Total ~$0.20/hr. Destroy promptly -- Global Accelerator charges hourly even with zero traffic.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercises 01-13 or equivalent knowledge
- Understanding of DNS resolution and anycast routing
- Familiarity with CloudFront from exercise 12

## Learning Objectives

After completing this exercise, you will be able to:

- **Evaluate** when to use Global Accelerator versus CloudFront for edge networking based on protocol, caching, and failover requirements
- **Design** a multi-region architecture that uses both Global Accelerator and CloudFront for different traffic types
- **Implement** Global Accelerator with endpoint groups in two regions and CloudFront with origin failover
- **Analyze** the failover timing, latency characteristics, and cost differences between Global Accelerator and CloudFront

## Why This Matters

The SAA-C03 exam frequently presents scenarios involving edge networking with a twist: "The application uses UDP," "The application needs static IP addresses," or "The application needs instant regional failover." These constraints change the answer from CloudFront (the default edge solution) to Global Accelerator. Understanding why requires knowing how each service works under the hood. CloudFront is a content delivery network that caches HTTP/HTTPS responses at edge locations. Global Accelerator is an anycast network overlay that routes TCP/UDP traffic to the nearest healthy regional endpoint over the AWS backbone. They solve different problems, and the exam tests whether you can distinguish them.

## The Challenge

Deploy identical API backends in two AWS regions (us-east-1 and eu-west-1), each behind an ALB. Create both a CloudFront distribution and a Global Accelerator pointing to these backends. Compare latency, failover behavior, and protocol support.

### Requirements

1. Deploy a Go-based API Lambda behind an ALB in us-east-1 and eu-west-1
2. Create a CloudFront distribution with origin failover (primary: us-east-1, secondary: eu-west-1)
3. Create a Global Accelerator with endpoint groups in both regions
4. Test latency from both distributions
5. Simulate regional failure by making one ALB return 503s and observe failover behavior
6. Document when to use each service based on your observations

### Architecture

```
                           Client
                          /      \
                    +-----+      +-----+
                    | CF  |      | GA  |
                    | Edge|      | PoP |
                    +--+--+      +--+--+
                       |            |
              +--------+--------+   |
              |  (HTTP cache)   |   | (TCP/UDP over
              |                 |   |  AWS backbone)
              v                 v   v
    +-------------------+    +-------------------+
    |   us-east-1       |    |   eu-west-1       |
    |   +----------+    |    |   +----------+    |
    |   |   ALB    |    |    |   |   ALB    |    |
    |   +----+-----+    |    |   +----+-----+    |
    |        |          |    |        |          |
    |   +----+-----+    |    |   +----+-----+    |
    |   | Lambda   |    |    |   | Lambda   |    |
    |   | (Go,     |    |    |   | (Go,     |    |
    |   | provided |    |    |   | provided |    |
    |   | .al2023) |    |    |   | .al2023) |    |
    |   +----------+    |    |   +----------+    |
    +-------------------+    +-------------------+

    CloudFront:                Global Accelerator:
    - Caches GET responses     - No caching
    - HTTP/HTTPS only          - TCP + UDP
    - Failover on 5xx (secs)   - Health-check failover
    - 400+ edge locations      - 100+ PoPs
    - Pay per request          - Fixed hourly + DT
```

### Decision Matrix

| Feature | CloudFront | Global Accelerator |
|---|---|---|
| **Protocols** | HTTP, HTTPS, WebSocket | TCP, UDP |
| **Caching** | Yes (edge cache) | No |
| **Static IPs** | No (DNS-based) | Yes (2 anycast IPs) |
| **Failover mechanism** | Origin failover on 5xx | Health check-based |
| **Failover speed** | Seconds (on next request) | Depends on health check interval |
| **Edge locations** | 400+ | 100+ PoPs |
| **WAF integration** | Yes | No (WAF at ALB) |
| **Lambda@Edge** | Yes | No |
| **Custom error pages** | Yes | No |
| **Cost model** | Per request + data transfer | $0.025/hr + per-flow + DT |
| **Use case** | Cacheable web content, APIs | Real-time apps, gaming, VoIP, IoT |
| **IP whitelisting** | Difficult (IPs change) | Easy (2 static IPs) |

## Hints

<details>
<summary>Hint 1: Multi-Region ALB Deployment with Lambda</summary>

Deploy the same Go Lambda function and ALB in two regions using provider aliases:

```hcl
provider "aws" {
  alias  = "us"
  region = "us-east-1"
}

provider "aws" {
  alias  = "eu"
  region = "eu-west-1"
}
```

Each region needs: VPC, public subnets, ALB, Lambda function, and Lambda permission for ALB. The Lambda function is a simple Go binary compiled for `provided.al2023`:

```go
// main.go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/aws/aws-lambda-go/events"
    "github.com/aws/aws-lambda-go/lambda"
)

func handler(ctx context.Context, req events.ALBTargetGroupRequest) (events.ALBTargetGroupResponse, error) {
    region := os.Getenv("AWS_REGION")
    return events.ALBTargetGroupResponse{
        StatusCode: 200,
        Headers:    map[string]string{"Content-Type": "application/json"},
        Body:       fmt.Sprintf(`{"region":"%s","status":"healthy"}`, region),
    }, nil
}

func main() {
    lambda.Start(handler)
}
```

Build for Lambda:

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bootstrap main.go
zip function.zip bootstrap
```

The Lambda returns which region served the request, allowing you to verify routing behavior.

</details>

<details>
<summary>Hint 2: Global Accelerator Configuration</summary>

Global Accelerator provisions two static anycast IP addresses and routes traffic to the nearest healthy endpoint group:

```hcl
resource "aws_globalaccelerator_accelerator" "this" {
  name            = "edge-demo"
  ip_address_type = "IPV4"
  enabled         = true

  attributes {
    flow_logs_enabled   = false
  }
}

resource "aws_globalaccelerator_listener" "http" {
  accelerator_arn = aws_globalaccelerator_accelerator.this.id
  protocol        = "TCP"

  port_range {
    from_port = 80
    to_port   = 80
  }
}

resource "aws_globalaccelerator_endpoint_group" "us" {
  listener_arn = aws_globalaccelerator_listener.http.id
  endpoint_group_region = "us-east-1"

  endpoint_configuration {
    endpoint_id = aws_lb.us.arn
    weight      = 100
  }

  health_check_port             = 80
  health_check_protocol         = "HTTP"
  health_check_path             = "/health"
  health_check_interval_seconds = 10
  threshold_count               = 2
}

resource "aws_globalaccelerator_endpoint_group" "eu" {
  listener_arn = aws_globalaccelerator_listener.http.id
  endpoint_group_region = "eu-west-1"

  endpoint_configuration {
    endpoint_id = aws_lb.eu.arn
    weight      = 100
  }

  health_check_port             = 80
  health_check_protocol         = "HTTP"
  health_check_path             = "/health"
  health_check_interval_seconds = 10
  threshold_count               = 2
}
```

With `health_check_interval_seconds = 10` and `threshold_count = 2`, failover takes approximately 20 seconds (2 failed checks). Reduce to `threshold_count = 1` for ~10 second failover, but at the risk of more false positives.

</details>

<details>
<summary>Hint 3: CloudFront with Origin Failover</summary>

CloudFront origin failover uses an origin group with a primary and secondary origin. If the primary returns a configured error code (5xx), CloudFront automatically retries the request against the secondary origin:

```hcl
resource "aws_cloudfront_distribution" "this" {
  enabled = true

  origin {
    domain_name = aws_lb.us.dns_name
    origin_id   = "us-east-1-alb"

    custom_origin_config {
      http_port              = 80
      https_port             = 443
      origin_protocol_policy = "http-only"
      origin_ssl_protocols   = ["TLSv1.2"]
    }
  }

  origin {
    domain_name = aws_lb.eu.dns_name
    origin_id   = "eu-west-1-alb"

    custom_origin_config {
      http_port              = 80
      https_port             = 443
      origin_protocol_policy = "http-only"
      origin_ssl_protocols   = ["TLSv1.2"]
    }
  }

  origin_group {
    origin_id = "failover-group"

    failover_criteria {
      status_codes = [500, 502, 503, 504]
    }

    member {
      origin_id = "us-east-1-alb"
    }

    member {
      origin_id = "eu-west-1-alb"
    }
  }

  default_cache_behavior {
    target_origin_id       = "failover-group"
    viewer_protocol_policy = "allow-all"

    allowed_methods = ["GET", "HEAD"]
    cached_methods  = ["GET", "HEAD"]

    forwarded_values {
      query_string = false
      cookies { forward = "none" }
    }

    min_ttl     = 0
    default_ttl = 0   # No caching for this test
    max_ttl     = 0
  }

  restrictions {
    geo_restriction { restriction_type = "none" }
  }

  viewer_certificate {
    cloudfront_default_certificate = true
  }
}
```

CloudFront failover is per-request: if the primary origin returns a 5xx error, CloudFront immediately retries on the secondary origin within the same request. The client sees a slightly higher latency (2x origin fetch) but no error. This is faster than Global Accelerator health-check-based failover for individual request failures, but slower for detecting a fully failed origin (CloudFront has no background health checks on origins).

</details>

<details>
<summary>Hint 4: Testing Failover Behavior</summary>

To simulate regional failure, modify the Lambda function in one region to return 503:

```bash
# Update Lambda environment variable to trigger failure mode
aws lambda update-function-configuration \
  --function-name edge-demo-api \
  --environment "Variables={FAIL_MODE=true}" \
  --region us-east-1
```

Then test both services:

```bash
# Test Global Accelerator (replace with your anycast IPs)
GA_IP=$(aws globalaccelerator describe-accelerator \
  --accelerator-arn "$GA_ARN" \
  --query "Accelerator.IpSets[0].IpAddresses[0]" \
  --output text)

# Before failure: should return us-east-1 or eu-west-1 based on your location
curl -s "http://${GA_IP}/health" | jq .

# After failure: Global Accelerator takes ~20s to detect, then routes to eu-west-1
# Poll to observe the transition:
for i in $(seq 1 30); do
  echo "$(date +%T) $(curl -s -o /dev/null -w '%{http_code}' "http://${GA_IP}/health")"
  sleep 2
done

# Test CloudFront (replace with your distribution domain)
CF_DOMAIN=$(aws cloudfront list-distributions \
  --query "DistributionList.Items[?Comment=='edge-demo'].DomainName" \
  --output text)

# CloudFront failover is per-request -- if primary returns 503,
# it immediately tries secondary on the same request
curl -s "http://${CF_DOMAIN}/health" | jq .
```

**Expected observations:**

| Metric | CloudFront | Global Accelerator |
|---|---|---|
| First failed request | Client sees success (failover transparent) | Client sees 503 until health check fails |
| Time to full failover | Instant per-request | ~20s (2x 10s health checks) |
| Latency during failover | Higher (2x origin fetch) | Normal (once failover completes) |
| Recovery after fix | Immediate | ~20s (health checks pass again) |

</details>

<details>
<summary>Hint 5: Cost Comparison and When to Use Each</summary>

**Global Accelerator cost:**
- Fixed: $0.025/hr = ~$18/month (even with zero traffic)
- Per-flow: $0.025 per new flow (TCP connection or UDP stream)
- Data transfer: standard AWS DT rates per GB
- Example: 1M new connections/month + 100 GB DT = ~$18 + $25 + $9 = ~$52/month

**CloudFront cost:**
- No fixed cost (pay per use)
- Per request: $0.0075-$0.016 per 10K HTTPS requests (varies by region)
- Data transfer: $0.085/GB (first 10TB)
- Example: 10M requests/month + 100 GB DT = ~$10 + $8.50 = ~$18.50/month

**Decision framework:**

Use **CloudFront** when:
- Traffic is HTTP/HTTPS
- Content is cacheable (HTML, CSS, JS, images, API responses with TTL)
- You need WAF integration at the edge
- You want Lambda@Edge for request/response transformation
- You need custom error pages
- Cost optimization is important (no fixed cost)

Use **Global Accelerator** when:
- Traffic is TCP or UDP (non-HTTP protocols)
- Applications need static anycast IPs (firewall whitelisting, DNS delegation)
- You need deterministic failover (health-check-based, not per-request)
- Real-time applications (gaming, VoIP, IoT) need consistent low latency
- You want traffic to stay on the AWS backbone from the nearest edge PoP

Use **both** when:
- Web traffic goes through CloudFront (caching, WAF)
- Real-time/non-HTTP traffic goes through Global Accelerator
- Example: gaming company with a web portal (CloudFront) and game servers (Global Accelerator)

</details>

## Spot the Bug

A team configures Global Accelerator health checks but complains that failover takes over 90 seconds instead of the target 10 seconds:

```hcl
resource "aws_globalaccelerator_endpoint_group" "primary" {
  listener_arn          = aws_globalaccelerator_listener.http.id
  endpoint_group_region = "us-east-1"

  endpoint_configuration {
    endpoint_id = aws_lb.us.arn
    weight      = 100
  }

  health_check_port             = 80
  health_check_protocol         = "HTTP"
  health_check_path             = "/health"
  health_check_interval_seconds = 30    # 30 seconds between checks
  threshold_count               = 3     # 3 failures before unhealthy
}
```

<details>
<summary>Explain the bug</summary>

The failover time is determined by `health_check_interval_seconds * threshold_count`. With the current settings:

- 30 seconds between health checks
- 3 consecutive failures required to mark unhealthy
- Worst case: 30s x 3 = **90 seconds** before failover

If the target is 10-second failover, the configuration needs to be much more aggressive:

```hcl
resource "aws_globalaccelerator_endpoint_group" "primary" {
  # ...
  health_check_interval_seconds = 10    # Check every 10 seconds
  threshold_count               = 1     # 1 failure = unhealthy
}
```

With these settings, failover takes approximately 10 seconds. However, there is a trade-off:

- **threshold_count = 1:** Fastest failover, but a single transient network blip triggers unnecessary failover. The endpoint flaps between healthy/unhealthy.
- **threshold_count = 2:** ~20 second failover, tolerates one transient failure. Good balance for most workloads.
- **threshold_count = 3:** ~30-90 second failover depending on interval. Most stable but slowest to react.

The right choice depends on the application's tolerance for downtime versus unnecessary failovers. For real-time applications (gaming, trading), use `interval = 10` and `threshold = 1`. For web APIs, `interval = 10` and `threshold = 2` provides a good balance.

Also consider the health check endpoint itself. If `/health` performs a deep check (database connectivity, dependency checks), it might intermittently fail even when the service is healthy. Use a shallow health check (`/ping` that returns 200 immediately) for Global Accelerator, and deep health checks for ALB target groups.

</details>

## Verify What You Learned

```bash
# Verify Global Accelerator is deployed with 2 anycast IPs
aws globalaccelerator describe-accelerator \
  --accelerator-arn "$GA_ARN" \
  --query "Accelerator.{Name:Name,Status:Status,IPs:IpSets[0].IpAddresses}" \
  --output table
```

Expected: Status=DEPLOYED, 2 IP addresses.

```bash
# Verify endpoint groups in both regions
aws globalaccelerator list-endpoint-groups \
  --listener-arn "$LISTENER_ARN" \
  --query "EndpointGroups[*].{Region:EndpointGroupRegion,HealthCheck:HealthCheckProtocol,Interval:HealthCheckIntervalSeconds}" \
  --output table
```

Expected: 2 endpoint groups (us-east-1 and eu-west-1).

```bash
# Verify CloudFront distribution with origin failover
aws cloudfront get-distribution \
  --id "$DISTRIBUTION_ID" \
  --query "Distribution.DistributionConfig.{Origins:Origins.Items[*].Id,OriginGroups:OriginGroups.Items[*].Id}" \
  --output json
```

Expected: 2 origins and 1 origin group.

```bash
# Test Global Accelerator routing
curl -s "http://${GA_IP}/health" | jq .region
```

Expected: your nearest region (us-east-1 or eu-west-1).

```bash
# Test CloudFront origin failover (with primary healthy)
curl -s "http://${CF_DOMAIN}/health" | jq .region
```

Expected: us-east-1 (primary origin).

## Cleanup

**Important:** Global Accelerator charges $0.025/hr even with zero traffic. Destroy immediately when finished.

```bash
terraform destroy -auto-approve
```

Global Accelerator takes 5-10 minutes to fully deprovision. Verify:

```bash
aws globalaccelerator list-accelerators \
  --query "Accelerators[?Name=='edge-demo']"
```

Expected: empty or status=IN_PROGRESS (deprovisioning).

## What's Next

You have compared two edge networking strategies and understand when each is appropriate. In the next exercise, you will tackle an **insane-level challenge**: designing a full **disaster recovery strategy** across two regions with automated failover, Aurora Global Database, and DynamoDB Global Tables.

## Summary

- **CloudFront** is a CDN that caches HTTP/HTTPS content at 400+ edge locations -- use it for cacheable web content and APIs
- **Global Accelerator** is an anycast network that routes TCP/UDP traffic over the AWS backbone -- use it for non-HTTP, static IPs, or real-time applications
- CloudFront failover is **per-request**: if the primary origin returns 5xx, the secondary is tried on the same request (transparent to client)
- Global Accelerator failover is **health-check-based**: unhealthy endpoints are removed from routing after `interval x threshold` seconds
- Global Accelerator provides **2 static anycast IPs** that never change -- useful for firewall whitelisting and DNS delegation
- CloudFront supports **WAF, Lambda@Edge, and custom error pages** at the edge; Global Accelerator does not
- Global Accelerator has a **fixed hourly cost** ($0.025/hr = ~$18/month); CloudFront has no fixed cost
- For web applications, **CloudFront is usually the right choice** unless you need static IPs or non-HTTP protocol support
- Many architectures use **both**: CloudFront for web traffic, Global Accelerator for real-time/non-HTTP traffic

## Reference

- [AWS Global Accelerator](https://docs.aws.amazon.com/global-accelerator/latest/dg/what-is-global-accelerator.html)
- [CloudFront Origin Failover](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/high_availability_origin_failover.html)
- [Global Accelerator vs CloudFront](https://aws.amazon.com/global-accelerator/faqs/)
- [Terraform aws_globalaccelerator_accelerator Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/globalaccelerator_accelerator)

## Additional Resources

- [Global Accelerator Speed Comparison Tool](https://speedtest.globalaccelerator.aws/) -- compare internet path vs AWS backbone path latency from your location
- [CloudFront Functions vs Lambda@Edge](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/edge-functions.html) -- choosing the right edge compute for request manipulation
- [Anycast Routing Explained](https://docs.aws.amazon.com/global-accelerator/latest/dg/introduction-how-it-works.html) -- how Global Accelerator uses anycast to route to the nearest PoP
- [Multi-Region Application Architecture](https://aws.amazon.com/solutions/implementations/multi-region-application-architecture/) -- reference architecture for active-active and active-passive patterns

<details>
<summary>Full Solution</summary>

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

# Default provider (Global Accelerator is a global resource)
provider "aws" {
  region = var.region
}

# Two-region deployment
provider "aws" {
  alias  = "us"
  region = "us-east-1"
}

provider "aws" {
  alias  = "eu"
  region = "eu-west-1"
}
```

### `variables.tf`

```hcl
variable "region" {
  description = "AWS region for global resources"
  type        = string
  default     = "us-east-1"
}

variable "project_name" {
  description = "Project name for resource naming and tagging"
  type        = string
  default     = "edge-demo"
}
```

### `vpc.tf`

```hcl
data "aws_availability_zones" "us" {
  provider = aws.us
  state    = "available"
}

data "aws_availability_zones" "eu" {
  provider = aws.eu
  state    = "available"
}

# ==================================================================
# US-EAST-1 REGION
# ==================================================================
resource "aws_vpc" "us" {
  provider             = aws.us
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags = { Name = "${var.project_name}-us" }
}

resource "aws_subnet" "us_a" {
  provider                = aws.us
  vpc_id                  = aws_vpc.us.id
  cidr_block              = "10.0.1.0/24"
  availability_zone       = data.aws_availability_zones.us.names[0]
  map_public_ip_on_launch = true
  tags = { Name = "${var.project_name}-us-a" }
}

resource "aws_subnet" "us_b" {
  provider                = aws.us
  vpc_id                  = aws_vpc.us.id
  cidr_block              = "10.0.2.0/24"
  availability_zone       = data.aws_availability_zones.us.names[1]
  map_public_ip_on_launch = true
  tags = { Name = "${var.project_name}-us-b" }
}

resource "aws_internet_gateway" "us" {
  provider = aws.us
  vpc_id   = aws_vpc.us.id
}

resource "aws_route_table" "us" {
  provider = aws.us
  vpc_id   = aws_vpc.us.id
  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.us.id
  }
}

resource "aws_route_table_association" "us_a" {
  provider       = aws.us
  subnet_id      = aws_subnet.us_a.id
  route_table_id = aws_route_table.us.id
}

resource "aws_route_table_association" "us_b" {
  provider       = aws.us
  subnet_id      = aws_subnet.us_b.id
  route_table_id = aws_route_table.us.id
}

# ==================================================================
# EU-WEST-1 REGION (mirror of US)
# ==================================================================
resource "aws_vpc" "eu" {
  provider             = aws.eu
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags = { Name = "${var.project_name}-eu" }
}

resource "aws_subnet" "eu_a" {
  provider                = aws.eu
  vpc_id                  = aws_vpc.eu.id
  cidr_block              = "10.0.1.0/24"
  availability_zone       = data.aws_availability_zones.eu.names[0]
  map_public_ip_on_launch = true
  tags = { Name = "${var.project_name}-eu-a" }
}

resource "aws_subnet" "eu_b" {
  provider                = aws.eu
  vpc_id                  = aws_vpc.eu.id
  cidr_block              = "10.0.2.0/24"
  availability_zone       = data.aws_availability_zones.eu.names[1]
  map_public_ip_on_launch = true
  tags = { Name = "${var.project_name}-eu-b" }
}

resource "aws_internet_gateway" "eu" {
  provider = aws.eu
  vpc_id   = aws_vpc.eu.id
}

resource "aws_route_table" "eu" {
  provider = aws.eu
  vpc_id   = aws_vpc.eu.id
  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.eu.id
  }
}

resource "aws_route_table_association" "eu_a" {
  provider       = aws.eu
  subnet_id      = aws_subnet.eu_a.id
  route_table_id = aws_route_table.eu.id
}

resource "aws_route_table_association" "eu_b" {
  provider       = aws.eu
  subnet_id      = aws_subnet.eu_b.id
  route_table_id = aws_route_table.eu.id
}
```

### `security.tf`

```hcl
resource "aws_security_group" "alb_us" {
  provider    = aws.us
  name_prefix = "${var.project_name}-alb-"
  vpc_id      = aws_vpc.us.id

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
}

resource "aws_security_group" "alb_eu" {
  provider    = aws.eu
  name_prefix = "${var.project_name}-alb-"
  vpc_id      = aws_vpc.eu.id

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
}
```

### `alb.tf`

```hcl
# US ALB
resource "aws_lb" "us" {
  provider           = aws.us
  name               = "${var.project_name}-us"
  internal           = false
  load_balancer_type = "application"
  security_groups    = [aws_security_group.alb_us.id]
  subnets            = [aws_subnet.us_a.id, aws_subnet.us_b.id]
}

resource "aws_lb_target_group" "us" {
  provider    = aws.us
  name        = "${var.project_name}-us"
  target_type = "lambda"
}

resource "aws_lb_listener" "us" {
  provider          = aws.us
  load_balancer_arn = aws_lb.us.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.us.arn
  }
}

resource "aws_lb_target_group_attachment" "us" {
  provider         = aws.us
  target_group_arn = aws_lb_target_group.us.arn
  target_id        = aws_lambda_function.us.arn
  depends_on       = [aws_lambda_permission.alb_us]
}

# EU ALB
resource "aws_lb" "eu" {
  provider           = aws.eu
  name               = "${var.project_name}-eu"
  internal           = false
  load_balancer_type = "application"
  security_groups    = [aws_security_group.alb_eu.id]
  subnets            = [aws_subnet.eu_a.id, aws_subnet.eu_b.id]
}

resource "aws_lb_target_group" "eu" {
  provider    = aws.eu
  name        = "${var.project_name}-eu"
  target_type = "lambda"
}

resource "aws_lb_listener" "eu" {
  provider          = aws.eu
  load_balancer_arn = aws_lb.eu.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.eu.arn
  }
}

resource "aws_lb_target_group_attachment" "eu" {
  provider         = aws.eu
  target_group_arn = aws_lb_target_group.eu.arn
  target_id        = aws_lambda_function.eu.arn
  depends_on       = [aws_lambda_permission.alb_eu]
}
```

### `iam.tf`

```hcl
resource "aws_iam_role" "lambda_us" {
  provider = aws.us
  name     = "${var.project_name}-lambda-us"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = { Service = "lambda.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "lambda_us" {
  provider   = aws.us
  role       = aws_iam_role.lambda_us.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_iam_role" "lambda_eu" {
  provider = aws.eu
  name     = "${var.project_name}-lambda-eu"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = { Service = "lambda.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "lambda_eu" {
  provider   = aws.eu
  role       = aws_iam_role.lambda_eu.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}
```

### `lambda.tf`

```hcl
# US Lambda function (Go, provided.al2023)
resource "aws_lambda_function" "us" {
  provider      = aws.us
  function_name = "${var.project_name}-api"
  role          = aws_iam_role.lambda_us.arn
  handler       = "bootstrap"
  runtime       = "provided.al2023"
  filename      = "function.zip"

  environment {
    variables = {
      FAIL_MODE = "false"
    }
  }
}

resource "aws_lambda_permission" "alb_us" {
  provider      = aws.us
  statement_id  = "AllowALBInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.us.function_name
  principal     = "elasticloadbalancing.amazonaws.com"
  source_arn    = aws_lb_target_group.us.arn
}

# EU Lambda function (mirror of US)
resource "aws_lambda_function" "eu" {
  provider      = aws.eu
  function_name = "${var.project_name}-api"
  role          = aws_iam_role.lambda_eu.arn
  handler       = "bootstrap"
  runtime       = "provided.al2023"
  filename      = "function.zip"

  environment {
    variables = {
      FAIL_MODE = "false"
    }
  }
}

resource "aws_lambda_permission" "alb_eu" {
  provider      = aws.eu
  statement_id  = "AllowALBInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.eu.function_name
  principal     = "elasticloadbalancing.amazonaws.com"
  source_arn    = aws_lb_target_group.eu.arn
}
```

### `dns.tf`

```hcl
# ==================================================================
# GLOBAL ACCELERATOR
# ==================================================================
resource "aws_globalaccelerator_accelerator" "this" {
  name            = var.project_name
  ip_address_type = "IPV4"
  enabled         = true
}

resource "aws_globalaccelerator_listener" "http" {
  accelerator_arn = aws_globalaccelerator_accelerator.this.id
  protocol        = "TCP"

  port_range {
    from_port = 80
    to_port   = 80
  }
}

resource "aws_globalaccelerator_endpoint_group" "us" {
  listener_arn          = aws_globalaccelerator_listener.http.id
  endpoint_group_region = "us-east-1"

  endpoint_configuration {
    endpoint_id = aws_lb.us.arn
    weight      = 100
  }

  health_check_port             = 80
  health_check_protocol         = "HTTP"
  health_check_path             = "/health"
  health_check_interval_seconds = 10
  threshold_count               = 2
}

resource "aws_globalaccelerator_endpoint_group" "eu" {
  listener_arn          = aws_globalaccelerator_listener.http.id
  endpoint_group_region = "eu-west-1"

  endpoint_configuration {
    endpoint_id = aws_lb.eu.arn
    weight      = 100
  }

  health_check_port             = 80
  health_check_protocol         = "HTTP"
  health_check_path             = "/health"
  health_check_interval_seconds = 10
  threshold_count               = 2
}

# ==================================================================
# CLOUDFRONT WITH ORIGIN FAILOVER
# ==================================================================
resource "aws_cloudfront_distribution" "this" {
  enabled = true
  comment = var.project_name

  origin {
    domain_name = aws_lb.us.dns_name
    origin_id   = "us-east-1-alb"

    custom_origin_config {
      http_port              = 80
      https_port             = 443
      origin_protocol_policy = "http-only"
      origin_ssl_protocols   = ["TLSv1.2"]
    }
  }

  origin {
    domain_name = aws_lb.eu.dns_name
    origin_id   = "eu-west-1-alb"

    custom_origin_config {
      http_port              = 80
      https_port             = 443
      origin_protocol_policy = "http-only"
      origin_ssl_protocols   = ["TLSv1.2"]
    }
  }

  origin_group {
    origin_id = "failover-group"

    failover_criteria {
      status_codes = [500, 502, 503, 504]
    }

    member {
      origin_id = "us-east-1-alb"
    }

    member {
      origin_id = "eu-west-1-alb"
    }
  }

  default_cache_behavior {
    target_origin_id       = "failover-group"
    viewer_protocol_policy = "allow-all"

    allowed_methods = ["GET", "HEAD"]
    cached_methods  = ["GET", "HEAD"]

    forwarded_values {
      query_string = false
      cookies { forward = "none" }
    }

    min_ttl     = 0
    default_ttl = 0
    max_ttl     = 0
  }

  restrictions {
    geo_restriction { restriction_type = "none" }
  }

  viewer_certificate {
    cloudfront_default_certificate = true
  }
}
```

### `outputs.tf`

```hcl
output "global_accelerator_ips" {
  value = aws_globalaccelerator_accelerator.this.ip_sets[0].ip_addresses
}

output "global_accelerator_dns" {
  value = aws_globalaccelerator_accelerator.this.dns_name
}

output "cloudfront_domain" {
  value = aws_cloudfront_distribution.this.domain_name
}

output "alb_us_dns" {
  value = aws_lb.us.dns_name
}

output "alb_eu_dns" {
  value = aws_lb.eu.dns_name
}
```

### Go Lambda Source (main.go)

```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

func handler(ctx context.Context, req events.ALBTargetGroupRequest) (events.ALBTargetGroupResponse, error) {
	region := os.Getenv("AWS_REGION")
	failMode := os.Getenv("FAIL_MODE")

	if failMode == "true" {
		return events.ALBTargetGroupResponse{
			StatusCode: 503,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       fmt.Sprintf(`{"region":"%s","status":"failing"}`, region),
		}, nil
	}

	return events.ALBTargetGroupResponse{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       fmt.Sprintf(`{"region":"%s","status":"healthy"}`, region),
	}, nil
}

func main() {
	lambda.Start(handler)
}
```

Build:

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bootstrap main.go
zip function.zip bootstrap
```

</details>

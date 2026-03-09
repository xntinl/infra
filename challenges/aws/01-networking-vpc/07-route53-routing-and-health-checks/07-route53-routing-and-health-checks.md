# 7. Route 53 Routing Policies and Health Checks

<!--
difficulty: intermediate
concepts: [route53, routing-policies, health-checks, weighted-routing, failover-routing]
tools: [terraform, aws-cli]
estimated_time: 50m
bloom_level: differentiate, implement, evaluate
prerequisites: [01-04]
aws_cost: ~$0.07/hr
-->

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.5 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Exercise 04 completed | Understand multi-AZ VPC with ALB |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Differentiate** between weighted, failover, and latency-based routing policies and when to use each
2. **Implement** Route 53 health checks that monitor HTTP endpoints
3. **Configure** weighted routing to split traffic between two endpoints at a defined ratio
4. **Configure** failover routing with automatic cutover when a health check fails
5. **Evaluate** which routing policy best fits a given availability or performance requirement

## Why This Matters

DNS is the first thing that happens when a user opens your application. Route 53 routing policies let you make intelligent decisions at this layer -- before a single TCP connection is established. Weighted routing enables canary deployments (send 5% of traffic to a new version), A/B testing, or gradual migrations between regions. Failover routing gives you automatic disaster recovery: when a health check detects your primary is down, DNS resolves to the secondary within seconds, no human intervention required.

Health checks are the backbone of all advanced routing. Without them, Route 53 has no signal to act on. A health check pings your endpoint every 10 or 30 seconds from multiple AWS regions. If enough checkers report failure, Route 53 stops returning that endpoint's IP in DNS responses. This is faster and cheaper than provisioning a hot standby behind a Global Accelerator, and it works with any endpoint that responds to HTTP -- including on-premises servers.

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
  default     = "r53-routing-lab"
}

variable "domain_name" {
  description = "Private hosted zone domain"
  type        = string
  default     = "routing-lab.internal"
}
```

### `vpc.tf`

```hcl
data "aws_availability_zones" "available" {
  state = "available"
}

data "aws_ami" "amazon_linux" {
  most_recent = true
  owners      = ["amazon"]

  filter {
    name   = "name"
    values = ["al2023-ami-2023.*-x86_64"]
  }
}

locals {
  azs = slice(data.aws_availability_zones.available.names, 0, 2)

  endpoints = {
    primary = {
      az    = local.azs[0]
      cidr  = "10.0.1.0/24"
      label = "primary"
    }
    secondary = {
      az    = local.azs[1]
      cidr  = "10.0.2.0/24"
      label = "secondary"
    }
  }
}

resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = "${var.project_name}-vpc" }
}

resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id
  tags   = { Name = "${var.project_name}-igw" }
}

resource "aws_subnet" "public" {
  for_each = local.endpoints

  vpc_id                  = aws_vpc.this.id
  cidr_block              = each.value.cidr
  availability_zone       = each.value.az
  map_public_ip_on_launch = true

  tags = { Name = "${var.project_name}-${each.key}" }
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

### `security.tf`

```hcl
resource "aws_security_group" "web" {
  name   = "${var.project_name}-web-sg"
  vpc_id = aws_vpc.this.id

  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    from_port   = -1
    to_port     = -1
    protocol    = "icmp"
    cidr_blocks = ["0.0.0.0/0"]
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

### `compute.tf`

```hcl
resource "aws_instance" "web" {
  for_each = local.endpoints

  ami                    = data.aws_ami.amazon_linux.id
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.public[each.key].id
  vpc_security_group_ids = [aws_security_group.web.id]

  user_data = <<-EOF
    #!/bin/bash
    yum install -y httpd
    echo "Hello from ${each.value.label} (${each.value.az})" > /var/www/html/index.html
    echo "OK" > /var/www/html/health
    systemctl start httpd
    systemctl enable httpd
  EOF

  tags = { Name = "${var.project_name}-${each.key}" }
}
```

### `dns.tf`

```hcl
resource "aws_route53_zone" "this" {
  name = var.domain_name

  # Using a public zone so Route 53 health checks can reach
  # the public IPs of our EC2 instances. In production you
  # would use a registered domain.
  tags = { Name = "${var.project_name}-zone" }
}

# =======================================================
# TODO 1 — Create health checks for both endpoints
# =======================================================
# Requirements:
#   - Create an aws_route53_health_check for EACH endpoint
#     (use for_each over local.endpoints)
#   - Type: HTTP
#   - IP address: the PUBLIC IP of the EC2 instance
#   - Port: 80
#   - Resource path: "/health"
#   - Request interval: 30 seconds
#   - Failure threshold: 3
#   - Tag with Name = "${var.project_name}-hc-${each.key}"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route53_health_check
#
# Hint: Reference aws_instance.web[each.key].public_ip
#       for the ip_address attribute.


# =======================================================
# TODO 2 — Create weighted routing records (70/30 split)
# =======================================================
# Requirements:
#   - Create TWO aws_route53_record resources with
#     routing_policy = "weighted"
#   - Both share the same name: "weighted.${var.domain_name}"
#   - Type: A
#   - TTL: 60
#   - Primary record: weight = 70, set_identifier = "primary"
#   - Secondary record: weight = 30, set_identifier = "secondary"
#   - Each record points to its EC2 instance's public IP
#   - Associate each with its health check from TODO 1
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route53_record#weighted-routing
#
# Hint: Use the weighted_routing_policy { weight = N } block
#       and health_check_id to associate the health check.


# =======================================================
# TODO 3 — Create failover routing records
# =======================================================
# Requirements:
#   - Create TWO aws_route53_record resources with
#     routing_policy = "failover"
#   - Both share the same name: "failover.${var.domain_name}"
#   - Type: A
#   - TTL: 60
#   - Primary record: failover_routing_policy { type = "PRIMARY" }
#     set_identifier = "primary"
#   - Secondary record: failover_routing_policy { type = "SECONDARY" }
#     set_identifier = "secondary"
#   - Associate the PRIMARY record with the primary health check
#   - The secondary does NOT need a health check (it is the
#     fallback — if it is also unhealthy, Route 53 returns it anyway
#     as a last resort)
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route53_record#failover-routing


# =======================================================
# TODO 4 — Create latency-based routing records
# =======================================================
# Requirements:
#   - Create TWO aws_route53_record resources with
#     routing_policy = "latency"
#   - Both share the same name: "latency.${var.domain_name}"
#   - Type: A
#   - TTL: 60
#   - Each record specifies a latency_routing_policy block
#     with region = var.region (both are in the same region
#     for this lab; in production they would differ)
#   - set_identifier = "primary" / "secondary"
#   - Associate each with its health check
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route53_record#latency-routing
#
# Note: In this single-region lab, Route 53 will return either
#       endpoint (both are equally close). In production, you
#       would deploy in two regions with provider aliases.
```

### `outputs.tf`

```hcl
output "primary_public_ip" {
  value = aws_instance.web["primary"].public_ip
}

output "secondary_public_ip" {
  value = aws_instance.web["secondary"].public_ip
}

output "zone_id" {
  value = aws_route53_zone.this.zone_id
}

output "weighted_fqdn" {
  value = "weighted.${var.domain_name}"
}

output "failover_fqdn" {
  value = "failover.${var.domain_name}"
}

output "latency_fqdn" {
  value = "latency.${var.domain_name}"
}
```

## Spot the Bug

A colleague created health checks, but they are always reported as unhealthy even though the web servers are running fine. Failover triggers incorrectly, sending all traffic to the secondary. **What is wrong?**

```hcl
resource "aws_route53_health_check" "web" {
  for_each = local.endpoints

  ip_address        = aws_instance.web[each.key].private_ip   # <-- BUG
  port              = 80
  type              = "HTTP"
  resource_path     = "/health"
  request_interval  = 30
  failure_threshold = 3

  tags = {
    Name = "${var.project_name}-hc-${each.key}"
  }
}
```

<details>
<summary>Explain the bug</summary>

The health check targets `private_ip` instead of `public_ip`. Route 53 health checkers run from AWS's global infrastructure -- they are **not** inside your VPC. They cannot reach private IP addresses because private IPs are only routable within the VPC.

Every health check attempt times out because the checkers cannot connect to `10.0.x.x`. After 3 consecutive failures (the failure threshold), Route 53 marks the endpoint as unhealthy. Since the primary is "unhealthy," failover routing sends all traffic to the secondary -- which is also marked unhealthy, but Route 53 returns it as a last resort.

The fix:

```hcl
ip_address = aws_instance.web[each.key].public_ip
```

If your endpoints are behind a load balancer, you would use the ALB's DNS name with `fqdn` instead of `ip_address`, and set `type = "HTTP"` with the ALB's public hostname.

</details>

## Verify What You Learned

### Step 1 -- Plan and apply

Run `terraform init` then `terraform plan`. You should see approximately **20-24 resources**:

```
Plan: 22 to add, 0 to change, 0 to destroy.
```

Apply when ready. Health checks take 1-2 minutes to become healthy after the instances launch.

### Step 2 -- Verify health checks are healthy

```
aws route53 list-health-checks \
  --query "HealthChecks[?HealthCheckConfig.IPAddress!=null].{ID:Id,IP:HealthCheckConfig.IPAddress,Path:HealthCheckConfig.ResourcePath}" \
  --output table
```

Then check the status of each:

```
for hc_id in $(aws route53 list-health-checks \
  --query "HealthChecks[].Id" --output text); do
  status=$(aws route53 get-health-check-status \
    --health-check-id "$hc_id" \
    --query "HealthCheckObservations[0].StatusReport.Status" \
    --output text)
  echo "Health check $hc_id: $status"
done
```

Expected: both health checks show a status containing "Success" (e.g., "Success: HTTP Status Code 200, ...").

### Step 3 -- Verify weighted records

Query the Route 53 zone to see the weighted records:

```
aws route53 list-resource-record-sets \
  --hosted-zone-id $(terraform output -raw zone_id) \
  --query "ResourceRecordSets[?Name=='weighted.${var.domain_name}.'].{Name:Name,Type:Type,Weight:Weight,SetId:SetIdentifier}" \
  --output table
```

Expected:

```
-------------------------------------------------------------------
|                    ListResourceRecordSets                       |
+----------------------------------+------+----------+------------+
|              Name                | Type |  Weight  |   SetId    |
+----------------------------------+------+----------+------------+
|  weighted.routing-lab.internal.  |  A   |  70      |  primary   |
|  weighted.routing-lab.internal.  |  A   |  30      |  secondary |
+----------------------------------+------+----------+------------+
```

### Step 4 -- Verify failover records

```
aws route53 list-resource-record-sets \
  --hosted-zone-id $(terraform output -raw zone_id) \
  --query "ResourceRecordSets[?Name=='failover.${var.domain_name}.'].{Name:Name,Type:Type,Failover:Failover,SetId:SetIdentifier}" \
  --output table
```

Expected:

```
-----------------------------------------------------------------------
|                     ListResourceRecordSets                          |
+------------------------------------+------+-----------+-------------+
|               Name                 | Type | Failover  |   SetId     |
+------------------------------------+------+-----------+-------------+
|  failover.routing-lab.internal.    |  A   | PRIMARY   |  primary    |
|  failover.routing-lab.internal.    |  A   | SECONDARY |  secondary  |
+------------------------------------+------+-----------+-------------+
```

### Step 5 -- Test the endpoints directly

Verify both web servers respond:

```
curl -s http://$(terraform output -raw primary_public_ip)/
curl -s http://$(terraform output -raw secondary_public_ip)/
```

Expected:

```
Hello from primary (us-east-1a)
Hello from secondary (us-east-1b)
```

## Solutions

<details>
<summary>TODO 1 -- Health checks (dns.tf)</summary>

```hcl
resource "aws_route53_health_check" "web" {
  for_each = local.endpoints

  ip_address        = aws_instance.web[each.key].public_ip
  port              = 80
  type              = "HTTP"
  resource_path     = "/health"
  request_interval  = 30
  failure_threshold = 3

  tags = {
    Name = "${var.project_name}-hc-${each.key}"
  }
}
```

</details>

<details>
<summary>TODO 2 -- Weighted routing records 70/30 (dns.tf)</summary>

```hcl
resource "aws_route53_record" "weighted_primary" {
  zone_id = aws_route53_zone.this.zone_id
  name    = "weighted.${var.domain_name}"
  type    = "A"
  ttl     = 60

  set_identifier = "primary"

  weighted_routing_policy {
    weight = 70
  }

  health_check_id = aws_route53_health_check.web["primary"].id
  records         = [aws_instance.web["primary"].public_ip]
}

resource "aws_route53_record" "weighted_secondary" {
  zone_id = aws_route53_zone.this.zone_id
  name    = "weighted.${var.domain_name}"
  type    = "A"
  ttl     = 60

  set_identifier = "secondary"

  weighted_routing_policy {
    weight = 30
  }

  health_check_id = aws_route53_health_check.web["secondary"].id
  records         = [aws_instance.web["secondary"].public_ip]
}
```

</details>

<details>
<summary>TODO 3 -- Failover routing records (dns.tf)</summary>

```hcl
resource "aws_route53_record" "failover_primary" {
  zone_id = aws_route53_zone.this.zone_id
  name    = "failover.${var.domain_name}"
  type    = "A"
  ttl     = 60

  set_identifier = "primary"

  failover_routing_policy {
    type = "PRIMARY"
  }

  health_check_id = aws_route53_health_check.web["primary"].id
  records         = [aws_instance.web["primary"].public_ip]
}

resource "aws_route53_record" "failover_secondary" {
  zone_id = aws_route53_zone.this.zone_id
  name    = "failover.${var.domain_name}"
  type    = "A"
  ttl     = 60

  set_identifier = "secondary"

  failover_routing_policy {
    type = "SECONDARY"
  }

  records = [aws_instance.web["secondary"].public_ip]
}
```

</details>

<details>
<summary>TODO 4 -- Latency-based routing records (dns.tf)</summary>

```hcl
resource "aws_route53_record" "latency_primary" {
  zone_id = aws_route53_zone.this.zone_id
  name    = "latency.${var.domain_name}"
  type    = "A"
  ttl     = 60

  set_identifier = "primary"

  latency_routing_policy {
    region = var.region
  }

  health_check_id = aws_route53_health_check.web["primary"].id
  records         = [aws_instance.web["primary"].public_ip]
}

resource "aws_route53_record" "latency_secondary" {
  zone_id = aws_route53_zone.this.zone_id
  name    = "latency.${var.domain_name}"
  type    = "A"
  ttl     = 60

  set_identifier = "secondary"

  latency_routing_policy {
    region = var.region
  }

  health_check_id = aws_route53_health_check.web["secondary"].id
  records         = [aws_instance.web["secondary"].public_ip]
}
```

</details>

## Cleanup

Destroy all resources:

```
terraform destroy
```

Type `yes` when prompted. Route 53 health checks are deleted immediately. The hosted zone and records are removed together.

**Cost note:** Route 53 health checks cost $0.50/month each for AWS endpoints (HTTP) and $0.75/month for non-AWS or HTTPS endpoints. Even at the lab scale, destroy promptly to avoid charges.

## What's Next

In **Exercise 08 -- Transit Gateway Hub and Spoke**, you will connect multiple VPCs through a central hub using Transit Gateway, replacing the point-to-point peering model with a scalable hub-and-spoke topology.

## Summary

You built DNS-level traffic management with:

- **Health checks** monitoring HTTP endpoints from multiple AWS regions
- **Weighted routing** splitting traffic 70/30 between two endpoints (useful for canary deployments)
- **Failover routing** with automatic cutover when the primary health check fails
- **Latency-based routing** directing users to the nearest healthy endpoint

These routing policies can be combined (e.g., latency-based routing with failover as a secondary policy) for sophisticated multi-region architectures. The key insight is that all advanced routing depends on health checks -- without them, Route 53 has no signal to make decisions.

## Reference

| Resource | Purpose |
|----------|---------|
| `aws_route53_zone` | DNS hosted zone |
| `aws_route53_record` | DNS record with routing policy |
| `aws_route53_health_check` | Endpoint health monitoring |
| `weighted_routing_policy` | Proportional traffic distribution |
| `failover_routing_policy` | Active/passive failover |
| `latency_routing_policy` | Nearest-region routing |

## Additional Resources

- [Route 53 Routing Policies](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/routing-policy.html)
- [Route 53 Health Checks](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/health-checks-creating.html)
- [Choosing a Routing Policy](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/routing-policy-overview.html)
- [Terraform Route 53 Record](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route53_record)
- [How Health Checks Work](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/welcome-health-checks.html)

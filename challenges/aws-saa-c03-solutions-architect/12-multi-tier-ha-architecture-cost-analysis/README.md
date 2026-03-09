# 12. Multi-Tier HA Architecture with Cost Analysis

<!--
difficulty: advanced
concepts: [three-tier-architecture, high-availability, cost-estimation, multi-az, disaster-recovery, architecture-tradeoffs]
tools: [terraform, aws-cli]
estimated_time: 90m
bloom_level: evaluate
prerequisites: [none]
aws_cost: ~$0.35/hr
-->

> **AWS Cost Warning:** Full three-tier stack with ALB, ASG, RDS Multi-AZ, ElastiCache. ~$0.35/hr. Destroy promptly.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercises 01-11 or equivalent knowledge
- Understanding of VPC networking (subnets, route tables, NAT gateways)
- Familiarity with Auto Scaling concepts from exercise 04

## Learning Objectives

After completing this exercise, you will be able to:

- **Evaluate** the cost and availability trade-offs for each tier of a production three-tier architecture
- **Design** a highly available web application spanning 3 Availability Zones with appropriate redundancy at every layer
- **Implement** a complete stack: CloudFront, ALB, ASG, RDS Multi-AZ with read replica, and ElastiCache Redis
- **Analyze** monthly cost estimates and compare on-demand, reserved instance, and savings plan pricing models

## Why This Matters

The SAA-C03 exam regularly presents scenarios requiring you to design complete architectures and justify each component choice. "Design a highly available, cost-effective three-tier architecture" is one of the most common question patterns. Getting this right requires understanding not just individual services but how they compose together -- which components need Multi-AZ, where to place NAT gateways, how Auto Scaling interacts with the ALB, and when caching reduces both cost and latency. This exercise forces you to think about every tier simultaneously and calculate real costs, because in production the architecture that is "most available" is not always the right answer -- it might also be the most expensive.

## The Challenge

Design and deploy a production-grade three-tier web application that serves a Go-based API. The application handles 500 requests/second average with 3x spikes during business hours.

### Requirements

1. **Networking:** VPC spanning 3 AZs with public subnets (ALB), private subnets (compute), and isolated subnets (data)
2. **CDN:** CloudFront distribution for static assets and API caching
3. **Load Balancing:** ALB with health checks, sticky sessions disabled (stateless API)
4. **Compute:** ASG with min=2, desired=3, max=6 across 3 AZs; target tracking scaling at 60% CPU
5. **Database:** RDS MySQL Multi-AZ with one read replica in a third AZ
6. **Caching:** ElastiCache Redis cluster (2 nodes) for session and query caching
7. **Cost Analysis:** Document the monthly cost estimate for each component

### Architecture

```
                          Internet
                             |
                      +------+------+
                      | CloudFront  |
                      | (CDN + WAF) |
                      +------+------+
                             |
                      +------+------+
                      |     ALB     |
                      | (3 AZ, public subnets) |
                      +--+---+---+--+
                         |   |   |
              +----------+   |   +----------+
              |              |              |
        +-----+----+  +-----+----+  +-----+----+
        |  EC2 (a) |  |  EC2 (b) |  |  EC2 (c) |
        | ASG min=2|  | ASG      |  | ASG max=6|
        | private  |  | private  |  | private  |
        +-----+----+  +-----+----+  +-----+----+
              |              |              |
              +------+-------+-------+------+
                     |               |
              +------+------+  +-----+------+
              | RDS Primary |  | RDS Standby|
              | (AZ-a)      |  | (AZ-b)     |
              | Multi-AZ    |  | sync repl  |
              +------+------+  +------------+
                     |
              +------+------+
              | RDS Replica |
              | (AZ-c)      |
              | async repl  |
              +--------------+

              +------+------+  +-----+------+
              | Redis Node  |  | Redis Node |
              | (AZ-a)      |  | (AZ-b)     |
              | primary     |  | replica    |
              +--------------+  +------------+
```

### Cost Analysis Requirements

For each component, document:

| Component | Config | On-Demand $/mo | 1yr Reserved $/mo | Savings Plan $/mo | Justification |
|---|---|---|---|---|---|
| CloudFront | 50M requests + 500GB | ~$85 | N/A | N/A | Edge caching reduces origin load by 60% |
| ALB | 1 ALB + 500 LCU-hrs | ~$22 + LCU | N/A | N/A | Single ALB, costs scale with traffic |
| EC2 (ASG) | 3x t3.medium avg | ~$91 | ~$57 | ~$60 | Stateless API, right-sized for 500 rps |
| NAT Gateway | 3x (one per AZ) | ~$97 | N/A | N/A | Required for private subnet internet access |
| RDS Primary | db.r6g.large Multi-AZ | ~$350 | ~$228 | N/A | Multi-AZ doubles cost but provides HA |
| RDS Replica | db.r6g.large | ~$175 | ~$114 | N/A | Offloads reads, could be smaller |
| ElastiCache | 2x cache.r6g.large | ~$292 | ~$190 | N/A | Reduces DB load, sub-ms latency |
| **Total** | | **~$1,112** | **~$768** | | |

## Hints

<details>
<summary>Hint 1: VPC and Networking Tier</summary>

A three-tier VPC needs three subnet layers across each AZ:

- **Public subnets:** ALB, NAT Gateways. Have a route to the Internet Gateway.
- **Private subnets:** EC2 instances (ASG). Route to the internet via NAT Gateway for outbound (patches, API calls).
- **Isolated subnets:** RDS, ElastiCache. No route to the internet at all. Access only from private subnets.

```hcl
locals {
  azs = slice(data.aws_availability_zones.available.names, 0, 3)

  public_cidrs   = ["10.0.1.0/24", "10.0.2.0/24", "10.0.3.0/24"]
  private_cidrs  = ["10.0.11.0/24", "10.0.12.0/24", "10.0.13.0/24"]
  isolated_cidrs = ["10.0.21.0/24", "10.0.22.0/24", "10.0.23.0/24"]
}
```

**Cost decision: NAT Gateways.** Each NAT Gateway costs ~$32/month plus data processing charges. You have three options:

1. **One per AZ (recommended for production):** ~$97/month. If one AZ's NAT Gateway fails, only that AZ's instances lose outbound access. This is the HA choice.
2. **Single NAT Gateway:** ~$32/month. All AZs route through one NAT Gateway. If it fails, all instances lose outbound access. Acceptable for dev/staging.
3. **NAT Instances:** ~$7/month (t3.nano). Cheapest but you manage patching, scaling, and failover yourself.

For this exercise, deploy one NAT Gateway per AZ to demonstrate the HA pattern.

</details>

<details>
<summary>Hint 2: Compute Tier - ASG and Launch Template</summary>

The ASG spans all 3 AZs with min=2, desired=3, max=6. Use target tracking scaling at 60% average CPU.

Key decisions:

- **Instance type:** t3.medium (2 vCPU, 4 GiB). Sufficient for a Go API handling ~170 rps per instance. Go binaries are memory-efficient; you likely will not need more than 4 GiB per instance.
- **min_size=2:** Ensures the application survives one AZ failure (at least one instance remains). This is the critical HA requirement.
- **max_size=6:** Handles 3x traffic spikes (2 per AZ at peak).
- **Health check:** ALB health checks, not EC2 status checks. ALB checks verify the application responds correctly, not just that the OS is running.

```hcl
resource "aws_autoscaling_group" "this" {
  name                = "three-tier-demo"
  min_size            = 2
  desired_capacity    = 3
  max_size            = 6
  vpc_zone_identifier = aws_subnet.private[*].id
  target_group_arns   = [aws_lb_target_group.this.arn]

  health_check_type         = "ELB"  # Not "EC2"
  health_check_grace_period = 120

  launch_template {
    id      = aws_launch_template.this.id
    version = "$Latest"
  }
}

resource "aws_autoscaling_policy" "cpu" {
  name                   = "cpu-target-tracking"
  autoscaling_group_name = aws_autoscaling_group.this.name
  policy_type            = "TargetTrackingScaling"

  target_tracking_configuration {
    predefined_metric_specification {
      predefined_metric_type = "ASGAverageCPUUtilization"
    }
    target_value = 60.0
  }
}
```

**Why not Fargate?** For this exercise, EC2 with ASG is used because:
- SAA-C03 tests ASG concepts heavily
- EC2 reserved instances provide significant savings (37% discount)
- Fargate pricing is ~3x EC2 on-demand for sustained workloads

</details>

<details>
<summary>Hint 3: Database Tier - RDS Multi-AZ + Read Replica</summary>

The database tier uses RDS MySQL with Multi-AZ for high availability and a read replica for read scaling:

```hcl
resource "aws_db_instance" "primary" {
  identifier           = "three-tier-primary"
  engine               = "mysql"
  engine_version       = "8.0"
  instance_class       = "db.r6g.large"
  allocated_storage    = 100
  storage_type         = "gp3"
  storage_encrypted    = true

  multi_az             = true
  db_subnet_group_name = aws_db_subnet_group.this.name

  backup_retention_period = 7
  backup_window           = "03:00-04:00"
  maintenance_window      = "Mon:04:00-Mon:05:00"

  # Production settings
  deletion_protection = false  # true in real production
  skip_final_snapshot = true   # false in real production
}

resource "aws_db_instance" "replica" {
  identifier          = "three-tier-replica"
  replicate_source_db = aws_db_instance.primary.identifier
  instance_class      = "db.r6g.large"
  availability_zone   = local.azs[2]  # Third AZ
}
```

**Cost analysis for the database tier:**

| Option | Monthly Cost | Availability | Trade-off |
|---|---|---|---|
| Single-AZ db.r6g.large | ~$175 | 99.5% (no auto failover) | Cheapest but risky |
| Multi-AZ db.r6g.large | ~$350 | 99.95% (auto failover) | Standard production choice |
| Multi-AZ + Read Replica | ~$525 | 99.95% + read scaling | Best for read-heavy workloads |
| Aurora Multi-AZ | ~$400 | 99.99% (faster failover) | Higher baseline but better SLA |

For a 500 rps workload with 80% reads, the read replica offloads 400 rps from the primary, extending its useful life before needing a larger instance class.

</details>

<details>
<summary>Hint 4: Caching Tier - ElastiCache Redis</summary>

ElastiCache Redis reduces database load and provides sub-millisecond response times for cached data:

```hcl
resource "aws_elasticache_subnet_group" "this" {
  name       = "three-tier-demo"
  subnet_ids = aws_subnet.isolated[*].id
}

resource "aws_elasticache_replication_group" "this" {
  replication_group_id = "three-tier-cache"
  description          = "Redis cache for three-tier demo"
  node_type            = "cache.r6g.large"
  num_cache_clusters   = 2  # Primary + 1 replica

  subnet_group_name    = aws_elasticache_subnet_group.this.name
  security_group_ids   = [aws_security_group.redis.id]

  at_rest_encryption_enabled = true
  transit_encryption_enabled = true
  automatic_failover_enabled = true

  engine_version = "7.0"
  port           = 6379
}
```

**Caching strategy decision:**

| Strategy | Implementation | Cache Hit Rate | Use Case |
|---|---|---|---|
| Cache-aside (lazy loading) | App checks cache, reads DB on miss, writes to cache | 60-80% | General purpose, tolerates stale data |
| Write-through | App writes to cache and DB simultaneously | 90%+ | Read-heavy, needs consistency |
| TTL-based | Cache-aside with time-to-live expiration | 70-85% | Balance of freshness and hit rate |

For this exercise, use cache-aside with a 5-minute TTL. This provides a good cache hit rate while limiting staleness. A 60% cache hit rate reduces the effective database load from 500 rps to 200 rps.

**Cost justification:** ElastiCache at ~$292/month seems expensive, but without it you might need to upgrade the RDS instance to db.r6g.xlarge (~$350/month for the primary alone). Caching is often cheaper than scaling the database.

</details>

<details>
<summary>Hint 5: CloudFront CDN Tier</summary>

CloudFront sits in front of the ALB to cache static assets and reduce origin load:

```hcl
resource "aws_cloudfront_distribution" "this" {
  enabled         = true
  is_ipv6_enabled = true

  origin {
    domain_name = aws_lb.this.dns_name
    origin_id   = "alb"

    custom_origin_config {
      http_port              = 80
      https_port             = 443
      origin_protocol_policy = "https-only"
      origin_ssl_protocols   = ["TLSv1.2"]
    }
  }

  default_cache_behavior {
    target_origin_id       = "alb"
    viewer_protocol_policy = "redirect-to-https"

    allowed_methods = ["GET", "HEAD", "OPTIONS", "PUT", "POST", "PATCH", "DELETE"]
    cached_methods  = ["GET", "HEAD"]

    cache_policy_id          = data.aws_cloudfront_cache_policy.caching_optimized.id
    origin_request_policy_id = data.aws_cloudfront_origin_request_policy.all_viewer.id
  }

  # API paths: no caching, forward everything
  ordered_cache_behavior {
    path_pattern           = "/api/*"
    target_origin_id       = "alb"
    viewer_protocol_policy = "https-only"

    allowed_methods = ["GET", "HEAD", "OPTIONS", "PUT", "POST", "PATCH", "DELETE"]
    cached_methods  = ["GET", "HEAD"]

    cache_policy_id          = data.aws_cloudfront_cache_policy.caching_disabled.id
    origin_request_policy_id = data.aws_cloudfront_origin_request_policy.all_viewer.id
  }

  restrictions {
    geo_restriction { restriction_type = "none" }
  }

  viewer_certificate {
    cloudfront_default_certificate = true
  }
}
```

**CloudFront cost model:** You pay for requests ($0.0075 per 10K HTTPS requests) and data transfer out ($0.085/GB for the first 10TB). For 50M requests/month with 500GB transfer, expect ~$85/month. This is often less than the ALB data processing charges you would pay without CloudFront, because CloudFront serves cached responses without hitting the ALB at all.

</details>

## Spot the Bug

A team deploys a three-tier architecture with ASG min_size=1 across 3 AZs. During an AZ failure, the application experiences 2-3 minutes of complete downtime:

```hcl
resource "aws_autoscaling_group" "this" {
  name                = "production-api"
  min_size            = 1
  desired_capacity    = 3
  max_size            = 6
  vpc_zone_identifier = [
    aws_subnet.private_a.id,
    aws_subnet.private_b.id,
    aws_subnet.private_c.id,
  ]
  target_group_arns = [aws_lb_target_group.this.arn]

  health_check_type         = "ELB"
  health_check_grace_period = 300
}
```

<details>
<summary>Explain the bug</summary>

With `min_size = 1`, the ASG is allowed to run with a single instance. Under normal conditions, `desired_capacity = 3` means one instance per AZ. But consider what happens during an AZ failure:

1. AZ-a goes down, taking its instance with it
2. ASG detects the failed instance via ALB health checks
3. ASG launches a replacement instance in AZ-b or AZ-c
4. The new instance takes 1-3 minutes to pass health checks (boot, application startup, health check interval + healthy threshold)

If you were unlucky enough that ASG had already scaled down to 1 instance (during a low-traffic period, or due to a scaling policy), and that single instance was in the failing AZ, you have **zero healthy instances** until the replacement launches and passes health checks. That is 2-3 minutes of complete downtime.

The fix is `min_size = 2`:

```hcl
resource "aws_autoscaling_group" "this" {
  min_size            = 2   # Always at least 2 instances
  desired_capacity    = 3
  max_size            = 6
  # ...
}
```

With min=2, even if ASG scales down during low traffic, it maintains at least 2 instances. If one AZ fails, at least one instance in another AZ continues serving traffic while the replacement launches. The ALB immediately stops routing to the failed instance (health check failure), and the remaining instance(s) handle all traffic.

Additionally, `health_check_grace_period = 300` (5 minutes) is too long. During this period, ASG will not terminate an unhealthy instance. For a Go binary that starts in under 5 seconds, 120 seconds is sufficient:

```hcl
  health_check_grace_period = 120  # 2 minutes, not 5
```

</details>

## Verify What You Learned

```bash
# Verify ALB is serving traffic across all AZs
aws elbv2 describe-target-health \
  --target-group-arn "$TARGET_GROUP_ARN" \
  --query "TargetHealthDescriptions[*].{Target:Target.Id,AZ:Target.AvailabilityZone,Health:TargetHealth.State}" \
  --output table
```

Expected: 2-3 healthy targets across different AZs.

```bash
# Verify ASG configuration
aws autoscaling describe-auto-scaling-groups \
  --auto-scaling-group-names "three-tier-demo" \
  --query "AutoScalingGroups[0].{Min:MinSize,Desired:DesiredCapacity,Max:MaxSize,AZs:AvailabilityZones,HealthCheck:HealthCheckType}" \
  --output table
```

Expected: Min=2, Desired=3, Max=6, HealthCheck=ELB, 3 AZs.

```bash
# Verify RDS Multi-AZ
aws rds describe-db-instances \
  --db-instance-identifier three-tier-primary \
  --query "DBInstances[0].{MultiAZ:MultiAZ,AZ:AvailabilityZone,SecondaryAZ:SecondaryAvailabilityZone,Encrypted:StorageEncrypted}" \
  --output table
```

Expected: MultiAZ=True, StorageEncrypted=True.

```bash
# Verify ElastiCache cluster
aws elasticache describe-replication-groups \
  --replication-group-id three-tier-cache \
  --query "ReplicationGroups[0].{Status:Status,Nodes:MemberClusters,AutoFailover:AutomaticFailover,Encrypted:AtRestEncryptionEnabled}" \
  --output table
```

Expected: Status=available, 2 member clusters, AutomaticFailover=enabled.

```bash
# Verify CloudFront distribution
aws cloudfront list-distributions \
  --query "DistributionList.Items[?Comment=='three-tier-demo'].{Id:Id,Status:Status,Domain:DomainName}" \
  --output table
```

Expected: Status=Deployed.

## Cleanup

Destroy all resources to stop incurring charges. The three-tier stack has dependencies, so let Terraform handle the ordering:

```bash
terraform destroy -auto-approve
```

CloudFront distributions take 10-15 minutes to disable and delete. Be patient.

Verify cleanup:

```bash
aws elbv2 describe-load-balancers --query "LoadBalancers[?LoadBalancerName=='three-tier-demo']"
aws autoscaling describe-auto-scaling-groups --auto-scaling-group-names "three-tier-demo"
aws rds describe-db-instances --db-instance-identifier three-tier-primary 2>&1 || echo "RDS deleted"
```

Expected: empty results or "not found" errors.

## What's Next

You have built a complete three-tier architecture and analyzed costs at every layer. In the next exercise, you will take a **deliberately flawed architecture** and apply the Well-Architected Framework review to identify and remediate issues across all six pillars.

## Summary

- A production three-tier architecture needs **redundancy at every layer**: multi-AZ ALB, ASG min>=2, RDS Multi-AZ, ElastiCache with replicas
- **ASG min_size must be at least 2** for high availability -- min=1 means a single AZ failure can cause total downtime
- **NAT Gateway per AZ** is the HA pattern (~$97/month for 3 AZs); a single NAT Gateway is a cost-saving single point of failure
- **Cost optimization** comes from right-sizing (not over-provisioning), reserved instances (35-40% savings), and caching (reduces database tier requirements)
- **ElastiCache** is often cheaper than scaling the database: ~$292/month for caching vs upgrading RDS to a larger instance class
- **CloudFront** reduces ALB costs by serving cached responses directly from edge locations
- Use **ALB health checks** (not EC2 status checks) for ASG -- they verify application health, not just that the OS is running
- **health_check_grace_period** should match your application startup time, not be an arbitrary large number
- The total on-demand cost for this architecture is ~$1,100/month; reserved instances reduce it to ~$770/month (30% savings)

## Reference

- [AWS Well-Architected Framework - Reliability Pillar](https://docs.aws.amazon.com/wellarchitected/latest/reliability-pillar/welcome.html)
- [Auto Scaling Group Best Practices](https://docs.aws.amazon.com/autoscaling/ec2/userguide/auto-scaling-benefits.html)
- [RDS Multi-AZ Deployments](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/Concepts.MultiAZ.html)
- [ElastiCache for Redis](https://docs.aws.amazon.com/AmazonElastiCache/latest/red-ug/WhatIs.html)
- [CloudFront Pricing](https://aws.amazon.com/cloudfront/pricing/)

## Additional Resources

- [AWS Pricing Calculator](https://calculator.aws/) -- build cost estimates for any architecture
- [EC2 Reserved Instances](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-reserved-instances.html) -- pricing models and commitment terms
- [Savings Plans](https://docs.aws.amazon.com/savingsplans/latest/userguide/what-is-savings-plans.html) -- flexible pricing model alternative to reserved instances
- [Caching Best Practices](https://docs.aws.amazon.com/AmazonElastiCache/latest/red-ug/BestPractices.html) -- cache-aside vs write-through patterns, TTL strategies

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
  default     = "three-tier-demo"
}
```

### `locals.tf`

```hcl
data "aws_availability_zones" "available" { state = "available" }
data "aws_caller_identity" "current" {}

locals {
  azs            = slice(data.aws_availability_zones.available.names, 0, 3)
  public_cidrs   = ["10.0.1.0/24", "10.0.2.0/24", "10.0.3.0/24"]
  private_cidrs  = ["10.0.11.0/24", "10.0.12.0/24", "10.0.13.0/24"]
  isolated_cidrs = ["10.0.21.0/24", "10.0.22.0/24", "10.0.23.0/24"]
}
```

### `vpc.tf`

```hcl
resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags = { Name = var.project_name }
}

resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id
  tags   = { Name = var.project_name }
}

# Public Subnets (ALB + NAT Gateways)
resource "aws_subnet" "public" {
  count                   = 3
  vpc_id                  = aws_vpc.this.id
  cidr_block              = local.public_cidrs[count.index]
  availability_zone       = local.azs[count.index]
  map_public_ip_on_launch = true
  tags = { Name = "${var.project_name}-public-${local.azs[count.index]}" }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.this.id
  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this.id
  }
  tags = { Name = "${var.project_name}-public" }
}

resource "aws_route_table_association" "public" {
  count          = 3
  subnet_id      = aws_subnet.public[count.index].id
  route_table_id = aws_route_table.public.id
}

# NAT Gateways (one per AZ for HA)
resource "aws_eip" "nat" {
  count  = 3
  domain = "vpc"
  tags   = { Name = "${var.project_name}-nat-${local.azs[count.index]}" }
}

resource "aws_nat_gateway" "this" {
  count         = 3
  allocation_id = aws_eip.nat[count.index].id
  subnet_id     = aws_subnet.public[count.index].id
  tags          = { Name = "${var.project_name}-nat-${local.azs[count.index]}" }
}

# Private Subnets (Compute - ASG)
resource "aws_subnet" "private" {
  count             = 3
  vpc_id            = aws_vpc.this.id
  cidr_block        = local.private_cidrs[count.index]
  availability_zone = local.azs[count.index]
  tags = { Name = "${var.project_name}-private-${local.azs[count.index]}" }
}

resource "aws_route_table" "private" {
  count  = 3
  vpc_id = aws_vpc.this.id
  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.this[count.index].id
  }
  tags = { Name = "${var.project_name}-private-${local.azs[count.index]}" }
}

resource "aws_route_table_association" "private" {
  count          = 3
  subnet_id      = aws_subnet.private[count.index].id
  route_table_id = aws_route_table.private[count.index].id
}

# Isolated Subnets (Data - RDS, ElastiCache)
# No route table association needed -- isolated subnets have
# only the default local route (VPC-internal traffic only).
resource "aws_subnet" "isolated" {
  count             = 3
  vpc_id            = aws_vpc.this.id
  cidr_block        = local.isolated_cidrs[count.index]
  availability_zone = local.azs[count.index]
  tags = { Name = "${var.project_name}-isolated-${local.azs[count.index]}" }
}
```

### `security.tf`

```hcl
resource "aws_security_group" "alb" {
  name_prefix = "${var.project_name}-alb-"
  vpc_id      = aws_vpc.this.id
  description = "ALB - public HTTP/HTTPS"

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
  vpc_id      = aws_vpc.this.id
  description = "App tier - from ALB only"

  ingress {
    from_port       = 8080
    to_port         = 8080
    protocol        = "tcp"
    security_groups = [aws_security_group.alb.id]
    description     = "App port from ALB"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_security_group" "rds" {
  name_prefix = "${var.project_name}-rds-"
  vpc_id      = aws_vpc.this.id
  description = "RDS - from app tier only"

  ingress {
    from_port       = 3306
    to_port         = 3306
    protocol        = "tcp"
    security_groups = [aws_security_group.app.id]
    description     = "MySQL from app tier"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_security_group" "redis" {
  name_prefix = "${var.project_name}-redis-"
  vpc_id      = aws_vpc.this.id
  description = "Redis - from app tier only"

  ingress {
    from_port       = 6379
    to_port         = 6379
    protocol        = "tcp"
    security_groups = [aws_security_group.app.id]
    description     = "Redis from app tier"
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
  name               = var.project_name
  internal           = false
  load_balancer_type = "application"
  security_groups    = [aws_security_group.alb.id]
  subnets            = aws_subnet.public[*].id
  tags               = { Name = var.project_name }
}

resource "aws_lb_target_group" "this" {
  name     = var.project_name
  port     = 8080
  protocol = "HTTP"
  vpc_id   = aws_vpc.this.id

  health_check {
    path                = "/health"
    port                = "traffic-port"
    healthy_threshold   = 2
    unhealthy_threshold = 3
    interval            = 15
    timeout             = 5
  }
}

resource "aws_lb_listener" "http" {
  load_balancer_arn = aws_lb.this.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.this.arn
  }
}
```

### `compute.tf`

```hcl
data "aws_ami" "al2023" {
  most_recent = true
  owners      = ["amazon"]
  filter {
    name   = "name"
    values = ["al2023-ami-2023.*-x86_64"]
  }
}

resource "aws_launch_template" "this" {
  name_prefix   = "${var.project_name}-"
  image_id      = data.aws_ami.al2023.value
  instance_type = "t3.medium"

  network_interfaces {
    security_groups             = [aws_security_group.app.id]
    associate_public_ip_address = false
  }

  user_data = base64encode(<<-EOF
    #!/bin/bash
    # In production: install your Go binary from S3 or ECR
    # For demo: simple health check responder
    while true; do
      echo -e "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nOK" | nc -l -p 8080 -q 1
    done &
  EOF
  )

  tag_specifications {
    resource_type = "instance"
    tags          = { Name = "${var.project_name}-app" }
  }
}

resource "aws_autoscaling_group" "this" {
  name                = var.project_name
  min_size            = 2
  desired_capacity    = 3
  max_size            = 6
  vpc_zone_identifier = aws_subnet.private[*].id
  target_group_arns   = [aws_lb_target_group.this.arn]

  health_check_type         = "ELB"
  health_check_grace_period = 120

  launch_template {
    id      = aws_launch_template.this.id
    version = "$Latest"
  }

  tag {
    key                 = "Name"
    value               = "${var.project_name}-app"
    propagate_at_launch = true
  }
}

resource "aws_autoscaling_policy" "cpu" {
  name                   = "cpu-target-tracking"
  autoscaling_group_name = aws_autoscaling_group.this.name
  policy_type            = "TargetTrackingScaling"

  target_tracking_configuration {
    predefined_metric_specification {
      predefined_metric_type = "ASGAverageCPUUtilization"
    }
    target_value = 60.0
  }
}
```

### `database.tf`

```hcl
# RDS Multi-AZ + Read Replica
resource "aws_db_subnet_group" "this" {
  name       = var.project_name
  subnet_ids = aws_subnet.isolated[*].id
}

resource "aws_db_instance" "primary" {
  identifier           = "${var.project_name}-primary"
  engine               = "mysql"
  engine_version       = "8.0"
  instance_class       = "db.t3.medium"  # t3.medium for demo cost savings
  allocated_storage    = 20
  storage_type         = "gp3"
  storage_encrypted    = true
  multi_az             = true
  db_name              = "appdb"
  username             = "admin"
  password             = "DemoPass123!"
  db_subnet_group_name = aws_db_subnet_group.this.name
  vpc_security_group_ids = [aws_security_group.rds.id]
  backup_retention_period = 7
  skip_final_snapshot     = true
  apply_immediately       = true
  tags = { Name = "${var.project_name}-primary" }
}

resource "aws_db_instance" "replica" {
  identifier          = "${var.project_name}-replica"
  replicate_source_db = aws_db_instance.primary.identifier
  instance_class      = "db.t3.medium"
  availability_zone   = local.azs[2]
  skip_final_snapshot = true
  apply_immediately   = true
  tags = { Name = "${var.project_name}-replica" }
}

# ElastiCache Redis
resource "aws_elasticache_subnet_group" "this" {
  name       = var.project_name
  subnet_ids = aws_subnet.isolated[*].id
}

resource "aws_elasticache_replication_group" "this" {
  replication_group_id = "${var.project_name}-cache"
  description          = "Three-tier demo Redis cache"
  node_type            = "cache.t3.medium"  # t3.medium for demo cost savings
  num_cache_clusters   = 2

  subnet_group_name          = aws_elasticache_subnet_group.this.name
  security_group_ids         = [aws_security_group.redis.id]
  at_rest_encryption_enabled = true
  transit_encryption_enabled = true
  automatic_failover_enabled = true
  engine_version             = "7.0"
  port                       = 6379
}
```

### `outputs.tf`

```hcl
output "alb_dns" {
  value = aws_lb.this.dns_name
}

output "rds_primary_endpoint" {
  value = aws_db_instance.primary.endpoint
}

output "rds_replica_endpoint" {
  value = aws_db_instance.replica.endpoint
}

output "redis_endpoint" {
  value = aws_elasticache_replication_group.this.primary_endpoint_address
}
```

</details>

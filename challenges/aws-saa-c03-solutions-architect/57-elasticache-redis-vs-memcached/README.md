# 57. ElastiCache: Redis vs Memcached

<!--
difficulty: basic
concepts: [elasticache-redis, elasticache-memcached, in-memory-caching, persistence, replication, multi-az, pub-sub, cache-eviction, cache-node-types]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: understand
prerequisites: [none]
aws_cost: ~$0.05/hr
-->

> **AWS Cost Warning:** ElastiCache cache.t3.micro nodes cost ~$0.017/hr each. One Redis node + one Memcached node = ~$0.034/hr. Add networking overhead for ~$0.05/hr total. Remember to run `terraform destroy` when finished -- ElastiCache nodes charge continuously.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Basic understanding of caching concepts (cache hit, cache miss, TTL)
- Default VPC available in the target region

## Learning Objectives

After completing this exercise, you will be able to:

- **Identify** the key differences between Redis (feature-rich, persistent) and Memcached (simple, multi-threaded)
- **Explain** when to choose Redis over Memcached based on data structure, persistence, and replication requirements
- **Construct** ElastiCache clusters for both Redis and Memcached using Terraform
- **Describe** Redis Multi-AZ with automatic failover and its impact on availability
- **Compare** the memory management and eviction strategies between Redis and Memcached
- **Distinguish** between ElastiCache and DAX (DynamoDB-specific caching) for NoSQL workloads

## Why ElastiCache Matters

The SAA-C03 exam presents caching scenarios that test whether you can choose the right engine, not just whether you know caching exists. The fundamental decision -- Redis vs Memcached -- depends on your requirements, and the exam tests this with specific scenarios. "The application needs to cache user sessions with automatic failover" means Redis (persistence + Multi-AZ). "The application needs a simple, horizontally scalable key-value cache with multi-threaded performance" means Memcached. Getting this wrong costs either money (Redis is more expensive for simple use cases) or reliability (Memcached data is lost on node failure).

Redis and Memcached have fundamentally different architectures. Redis is single-threaded for command execution (uses I/O threads for network), supports complex data types (sorted sets, lists, hashes, streams), offers persistence (RDB snapshots, AOF logs), replication (up to 5 read replicas), Multi-AZ failover, pub/sub messaging, and Lua scripting. Memcached is multi-threaded, supports only simple key-value pairs (strings), has no persistence, no replication, no failover, but is simpler and can scale horizontally by adding nodes. The exam expects you to map requirements to the correct engine.

## Step 1 -- Create the Redis Cluster

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
  default     = "saa-ex57"
}
```

### `security.tf`

```hcl
data "aws_vpc" "default" {
  default = true
}

# ------------------------------------------------------------------
# Security group: allow access from within the VPC on the cache
# ports. Redis uses 6379, Memcached uses 11211.
# ------------------------------------------------------------------
resource "aws_security_group" "cache" {
  name   = "${var.project_name}-cache-sg"
  vpc_id = data.aws_vpc.default.id

  ingress {
    from_port   = 6379
    to_port     = 6379
    protocol    = "tcp"
    cidr_blocks = [data.aws_vpc.default.cidr_block]
    description = "Redis"
  }

  ingress {
    from_port   = 11211
    to_port     = 11211
    protocol    = "tcp"
    cidr_blocks = [data.aws_vpc.default.cidr_block]
    description = "Memcached"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "${var.project_name}-cache-sg" }
}
```

### `database.tf`

```hcl
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

# ------------------------------------------------------------------
# Subnet group: ElastiCache requires a subnet group to place
# cache nodes. Using default VPC subnets for simplicity.
# ------------------------------------------------------------------
resource "aws_elasticache_subnet_group" "this" {
  name       = "${var.project_name}-subnet-group"
  subnet_ids = data.aws_subnets.default.ids
}

# ------------------------------------------------------------------
# Redis cluster: single node for this exercise. In production,
# use a replication group with Multi-AZ for high availability.
#
# Key features demonstrated:
#   - Engine: redis (7.x)
#   - Persistence: snapshot_retention_limit > 0 enables RDB snapshots
#   - Node type: cache.t3.micro ($0.017/hr)
#
# Redis supports: strings, hashes, lists, sets, sorted sets,
#   bitmaps, HyperLogLog, streams, geospatial indexes
# ------------------------------------------------------------------
resource "aws_elasticache_cluster" "redis" {
  cluster_id           = "${var.project_name}-redis"
  engine               = "redis"
  engine_version       = "7.1"
  node_type            = "cache.t3.micro"
  num_cache_nodes      = 1
  parameter_group_name = "default.redis7"
  port                 = 6379
  subnet_group_name    = aws_elasticache_subnet_group.this.name
  security_group_ids   = [aws_security_group.cache.id]

  # Snapshots: RDB persistence. Set to 0 to disable.
  # Non-zero values enable daily automatic snapshots retained
  # for this many days. Snapshots to S3 enable point-in-time recovery.
  snapshot_retention_limit = 1

  tags = { Name = "${var.project_name}-redis" }
}

# ------------------------------------------------------------------
# Memcached cluster: single node for comparison.
#
# Key differences from Redis:
#   - No persistence (data lost on node restart)
#   - No replication (no read replicas)
#   - No Multi-AZ failover
#   - Multi-threaded (can use multiple CPU cores)
#   - Simple key-value only (no complex data structures)
#   - Supports auto-discovery (client finds all nodes)
#
# When to choose Memcached:
#   - Simple string-based caching
#   - Multi-threaded performance needed
#   - No persistence requirement
#   - Horizontal scaling by adding nodes (data sharded)
# ------------------------------------------------------------------
resource "aws_elasticache_cluster" "memcached" {
  cluster_id           = "${var.project_name}-memc"
  engine               = "memcached"
  engine_version       = "1.6.22"
  node_type            = "cache.t3.micro"
  num_cache_nodes      = 1
  parameter_group_name = "default.memcached1.6"
  port                 = 11211
  subnet_group_name    = aws_elasticache_subnet_group.this.name
  security_group_ids   = [aws_security_group.cache.id]

  tags = { Name = "${var.project_name}-memcached" }
}
```

### `outputs.tf`

```hcl
output "redis_endpoint" {
  description = "Redis primary endpoint"
  value       = aws_elasticache_cluster.redis.cache_nodes[0].address
}

output "redis_port" {
  value = aws_elasticache_cluster.redis.port
}

output "memcached_endpoint" {
  description = "Memcached endpoint"
  value       = aws_elasticache_cluster.memcached.cluster_address
}

output "memcached_port" {
  value = aws_elasticache_cluster.memcached.port
}
```

## Step 2 -- Initialize and Apply

```bash
terraform init
terraform apply -auto-approve
```

ElastiCache cluster creation takes 5-10 minutes. Wait for completion before proceeding.

## Step 3 -- Redis vs Memcached Decision Framework

| Feature | Redis | Memcached |
|---|---|---|
| **Data structures** | Strings, hashes, lists, sets, sorted sets, streams, geospatial | Strings only |
| **Persistence** | RDB snapshots + AOF logs | None |
| **Replication** | Up to 5 read replicas | None |
| **Multi-AZ** | Yes (automatic failover) | No |
| **Pub/Sub** | Yes | No |
| **Lua scripting** | Yes | No |
| **Threading model** | Single-threaded (I/O threads in 6.x+) | Multi-threaded |
| **Max item size** | 512 MB | 1 MB (default, configurable to 128 MB) |
| **Horizontal scaling** | Cluster mode (data sharded) | Auto-discovery (data sharded) |
| **Backup/Restore** | Yes (to S3) | No |
| **Encryption** | At-rest and in-transit | At-rest and in-transit |
| **Typical use cases** | Session store, leaderboards, real-time analytics, message queues | Simple page caching, database query caching |
| **Cost (cache.t3.micro)** | $0.017/hr | $0.017/hr |

### When to Choose Redis

- You need **persistence** (data survives restarts)
- You need **replication** (read replicas for read scaling)
- You need **Multi-AZ** (automatic failover for HA)
- You need **complex data types** (sorted sets for leaderboards, lists for queues)
- You need **pub/sub** (real-time messaging)
- You need **atomic operations** on data structures

### When to Choose Memcached

- You need the **simplest** possible caching layer
- You need **multi-threaded** performance for high-throughput simple caching
- You do not need **persistence** (cache can be rebuilt from database)
- You need to **horizontally scale** by adding nodes with auto-discovery
- **Cost sensitivity** on compute (multi-threaded uses fewer nodes for the same throughput)

## Step 4 -- Verify Cluster Configuration

```bash
# Redis cluster details
aws elasticache describe-cache-clusters \
  --cache-cluster-id saa-ex57-redis \
  --show-cache-node-info \
  --query 'CacheClusters[0].{Engine:Engine,Version:EngineVersion,NodeType:CacheNodeType,Nodes:CacheNodes[*].{Endpoint:Endpoint.Address,Port:Endpoint.Port,Status:CacheNodeStatus}}' \
  --output json

# Memcached cluster details
aws elasticache describe-cache-clusters \
  --cache-cluster-id saa-ex57-memc \
  --show-cache-node-info \
  --query 'CacheClusters[0].{Engine:Engine,Version:EngineVersion,NodeType:CacheNodeType,Nodes:CacheNodes[*].{Endpoint:Endpoint.Address,Port:Endpoint.Port,Status:CacheNodeStatus}}' \
  --output json
```

### ElastiCache vs DAX Comparison

| Criterion | ElastiCache (Redis/Memcached) | DAX |
|---|---|---|
| **Works with** | Any application | DynamoDB only |
| **Protocol** | Redis/Memcached protocol | DynamoDB-compatible API |
| **Code changes** | Application must use cache client | Minimal (change DDB endpoint) |
| **Caching strategy** | Application manages cache (aside, through, etc.) | Transparent (read-through, write-through) |
| **Use case** | General-purpose caching | DynamoDB read acceleration |
| **Latency** | Sub-millisecond | Microseconds (single-digit) |

## Common Mistakes

### 1. Choosing Memcached when persistence is needed

**Wrong approach:** Using Memcached for session storage:

```hcl
resource "aws_elasticache_cluster" "sessions" {
  engine     = "memcached"
  # ...
}
```

**What happens:** When a Memcached node fails or restarts, all session data is lost. Users are logged out instantly. With multiple nodes, losing one node loses 1/N of all sessions with no recovery mechanism.

**Fix:** Use Redis with Multi-AZ for session storage. Data persists through failures, and automatic failover promotes a replica:

```hcl
resource "aws_elasticache_replication_group" "sessions" {
  description                = "Session store"
  replication_group_id       = "session-store"
  node_type                  = "cache.t3.micro"
  num_cache_clusters         = 2
  automatic_failover_enabled = true
  multi_az_enabled           = true
  engine                     = "redis"
}
```

### 2. Using ElastiCache for DynamoDB when DAX is available

**Wrong approach:** Building a custom caching layer with ElastiCache in front of DynamoDB:

```go
// Application code with manual cache management
result, err := redisClient.Get(ctx, key).Result()
if err == redis.Nil {
    out, _ := dynamoClient.GetItem(ctx, &dynamodb.GetItemInput{TableName: &tableName, Key: key})
    result = marshal(out.Item)
    redisClient.Set(ctx, key, result, 300*time.Second)
}
```

**What happens:** This works but requires significant application code for cache invalidation, TTL management, and consistency. It also introduces a different client library (Redis) alongside the DynamoDB SDK.

**Fix:** If your cache is exclusively for DynamoDB reads, use DAX instead. It is a drop-in replacement for the DynamoDB client with transparent caching:

```go
// DAX: transparent caching with microsecond reads
cfg, _ := config.LoadDefaultConfig(ctx)
daxClient := dax.New(cfg, daxEndpoint)
out, _ := daxClient.GetItem(ctx, &dynamodb.GetItemInput{TableName: &tableName, Key: key})
```

### 3. Not enabling encryption for compliance

**Wrong approach:** Deploying ElastiCache without encryption:

```hcl
resource "aws_elasticache_cluster" "redis" {
  engine = "redis"
  # No encryption configured
}
```

**What happens:** Data in the cache and data in transit between the application and cache is unencrypted. This violates PCI-DSS, HIPAA, and most compliance frameworks.

**Fix:** Enable both at-rest and in-transit encryption. Note: this must be set at creation time for Redis replication groups and cannot be changed later:

```hcl
resource "aws_elasticache_replication_group" "redis" {
  at_rest_encryption_enabled = true
  transit_encryption_enabled = true
  # ...
}
```

## Verify What You Learned

```bash
# Verify Redis cluster is running
aws elasticache describe-cache-clusters \
  --cache-cluster-id saa-ex57-redis \
  --query 'CacheClusters[0].CacheClusterStatus' --output text
```

Expected: `available`

```bash
# Verify Memcached cluster is running
aws elasticache describe-cache-clusters \
  --cache-cluster-id saa-ex57-memc \
  --query 'CacheClusters[0].CacheClusterStatus' --output text
```

Expected: `available`

```bash
# Verify Redis engine version
aws elasticache describe-cache-clusters \
  --cache-cluster-id saa-ex57-redis \
  --query 'CacheClusters[0].{Engine:Engine,Version:EngineVersion}' --output table
```

Expected: `Engine = redis`, `Version = 7.1`

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
aws elasticache describe-cache-clusters \
  --cache-cluster-id saa-ex57-redis 2>&1 || echo "Redis cluster deleted"
aws elasticache describe-cache-clusters \
  --cache-cluster-id saa-ex57-memc 2>&1 || echo "Memcached cluster deleted"
```

## What's Next

Exercise 58 covers **ElastiCache Redis Cluster Mode and Replication**, where you will configure replication groups with Cluster Mode Disabled (single shard, read replicas) and Cluster Mode Enabled (multiple shards, data partitioning). You will test failover behavior and understand when sharding provides real benefit vs unnecessary complexity.

## Summary

- **Redis** is the feature-rich choice: persistence, replication, Multi-AZ failover, complex data types, pub/sub, backup/restore
- **Memcached** is the simple choice: multi-threaded, key-value only, no persistence, no replication, horizontal scaling
- **Choose Redis** when you need persistence, failover, complex data structures, or pub/sub messaging
- **Choose Memcached** when you need simple caching with multi-threaded performance and can rebuild cache from source
- **DAX** is the better choice than ElastiCache when caching DynamoDB reads specifically (transparent, microsecond latency)
- **Session storage** should always use Redis (not Memcached) -- data loss on node failure logs out all users
- **Encryption** (at-rest and in-transit) must be enabled at creation time for compliance -- cannot be added later
- **ElastiCache nodes charge continuously** -- always destroy test clusters when finished
- **Redis single-threaded** model means one node handles ~100K operations/second; use cluster mode for higher throughput
- **Memcached multi-threaded** model can saturate multiple CPU cores on a single node

## Reference

- [ElastiCache for Redis](https://docs.aws.amazon.com/AmazonElastiCache/latest/red-ug/WhatIs.html)
- [ElastiCache for Memcached](https://docs.aws.amazon.com/AmazonElastiCache/latest/mem-ug/WhatIs.html)
- [ElastiCache Pricing](https://aws.amazon.com/elasticache/pricing/)
- [Terraform aws_elasticache_cluster](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/elasticache_cluster)

## Additional Resources

- [Comparing Redis and Memcached](https://docs.aws.amazon.com/AmazonElastiCache/latest/red-ug/SelectEngine.html) -- AWS official comparison guide
- [Caching Strategies](https://docs.aws.amazon.com/AmazonElastiCache/latest/red-ug/Strategies.html) -- lazy loading, write-through, and TTL patterns
- [ElastiCache Best Practices](https://docs.aws.amazon.com/AmazonElastiCache/latest/red-ug/BestPractices.html) -- node sizing, connection management, and eviction policies
- [DynamoDB Accelerator (DAX)](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/DAX.html) -- when to use DAX instead of ElastiCache for DynamoDB

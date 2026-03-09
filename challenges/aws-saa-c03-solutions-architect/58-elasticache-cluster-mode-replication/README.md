# 58. ElastiCache Redis Cluster Mode and Replication

<!--
difficulty: intermediate
concepts: [elasticache-replication-group, cluster-mode-disabled, cluster-mode-enabled, sharding, read-replicas, automatic-failover, multi-az, data-partitioning, slot-distribution]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: apply, analyze
prerequisites: [57-elasticache-redis-vs-memcached]
aws_cost: ~$0.10/hr
-->

> **AWS Cost Warning:** Replication groups with multiple nodes. Cluster Mode Disabled: 1 primary + 1 replica = 2 x cache.t3.micro (~$0.034/hr). Cluster Mode Enabled: 2 shards x 2 nodes = 4 x cache.t3.micro (~$0.068/hr). Total ~$0.10/hr. Remember to run `terraform destroy` immediately when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Completed exercise 57 (Redis vs Memcached) | Redis basics |
| Default VPC with subnets in >= 2 AZs | `aws ec2 describe-subnets --filters Name=default-for-az,Values=true --query 'Subnets[*].AvailabilityZone'` |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Implement** ElastiCache Redis replication groups in both Cluster Mode Disabled and Cluster Mode Enabled configurations.
2. **Analyze** when Cluster Mode Enabled (multiple shards) provides real benefit vs unnecessary complexity.
3. **Apply** Multi-AZ automatic failover configuration to ensure high availability.
4. **Evaluate** the trade-offs between vertical scaling (larger node types) and horizontal scaling (more shards).
5. **Design** Redis architectures that match specific performance and availability requirements.

---

## Why This Matters

ElastiCache Redis cluster architecture is a common SAA-C03 exam topic because it tests whether you understand the difference between read scaling and write scaling. Cluster Mode Disabled provides read replicas (up to 5) that offload read traffic from the primary, but all writes go to a single primary node. This is sufficient for read-heavy workloads where the primary can handle the write volume. Cluster Mode Enabled partitions data across multiple shards, each with its own primary and replicas, distributing both reads AND writes across the cluster.

The exam presents scenarios where you must choose the right mode. "The application has 50,000 reads/second and 1,000 writes/second with a 10 GB dataset" -- Cluster Mode Disabled with read replicas handles this easily. "The application has 100,000 writes/second and a 100 GB dataset that exceeds the memory of a single node" -- only Cluster Mode Enabled with multiple shards can handle this. The key insight: if your dataset fits in a single node and your write throughput fits a single primary, adding shards adds complexity with no benefit.

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
  default     = "saa-ex58"
}
```

### `security.tf`

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

resource "aws_security_group" "cache" {
  name   = "${var.project_name}-cache-sg"
  vpc_id = data.aws_vpc.default.id

  ingress {
    from_port   = 6379
    to_port     = 6379
    protocol    = "tcp"
    cidr_blocks = [data.aws_vpc.default.cidr_block]
    description = "Redis from VPC"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}
```

### `database.tf`

```hcl
resource "aws_elasticache_subnet_group" "this" {
  name       = "${var.project_name}-subnet-group"
  subnet_ids = data.aws_subnets.default.ids
}

# ============================================================
# TODO 1: Cluster Mode Disabled (Single Shard + Replicas)
# ============================================================
# Create a Redis replication group with Cluster Mode DISABLED.
# This means: 1 shard with 1 primary + N replicas.
#
# Architecture:
#   Primary (read/write) --> Replica 1 (read-only)
#                        --> Replica 2 (read-only)
#                        --> ... up to 5 replicas
#
# Requirements:
#   - Resource: aws_elasticache_replication_group
#   - replication_group_id = "${var.project_name}-cmd"
#   - description = "Cluster Mode Disabled demo"
#   - node_type = "cache.t3.micro"
#   - num_cache_clusters = 2 (1 primary + 1 replica)
#   - automatic_failover_enabled = true
#   - multi_az_enabled = true
#   - engine_version = "7.1"
#   - port = 6379
#   - subnet_group_name = subnet group
#   - security_group_ids = [security group]
#   - snapshot_retention_limit = 1
#
# Note: With automatic_failover_enabled = true, if the
# primary fails, a replica is promoted to primary
# automatically (~30-60 seconds).
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/elasticache_replication_group
# ============================================================


# ============================================================
# TODO 2: Cluster Mode Enabled (Multiple Shards)
# ============================================================
# Create a Redis replication group with Cluster Mode ENABLED.
# This means: N shards, each with its own primary + replicas.
# Data is partitioned across shards using hash slots (16,384
# slots distributed evenly).
#
# Architecture:
#   Shard 1: Primary (slots 0-8191)    --> Replica 1A
#   Shard 2: Primary (slots 8192-16383) --> Replica 2A
#
# Requirements:
#   - Resource: aws_elasticache_replication_group
#   - replication_group_id = "${var.project_name}-cme"
#   - description = "Cluster Mode Enabled demo"
#   - node_type = "cache.t3.micro"
#   - automatic_failover_enabled = true (required for cluster mode)
#   - multi_az_enabled = true
#   - engine_version = "7.1"
#   - port = 6379
#   - num_node_groups = 2 (number of shards)
#   - replicas_per_node_group = 1 (replicas per shard)
#   - subnet_group_name = subnet group
#   - security_group_ids = [security group]
#   - snapshot_retention_limit = 1
#
# Note: num_node_groups > 1 automatically enables cluster mode.
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/elasticache_replication_group
# ============================================================


# ============================================================
# TODO 3: Test Failover (CLI)
# ============================================================
# This TODO is CLI-based. After terraform apply, test the
# automatic failover behavior:
#
#   a) List nodes in the replication group:
#      aws elasticache describe-replication-groups \
#        --replication-group-id saa-ex58-cmd \
#        --query 'ReplicationGroups[0].NodeGroups[0].NodeGroupMembers[*].{Node:CacheClusterId,Role:CurrentRole,AZ:PreferredAvailabilityZone}'
#
#   b) Trigger a failover test:
#      aws elasticache test-failover \
#        --replication-group-id saa-ex58-cmd \
#        --node-group-id 0001
#
#   c) Monitor failover progress:
#      watch -n 5 "aws elasticache describe-replication-groups \
#        --replication-group-id saa-ex58-cmd \
#        --query 'ReplicationGroups[0].{Status:Status,NodeGroups:NodeGroups[0].NodeGroupMembers[*].{Node:CacheClusterId,Role:CurrentRole}}'"
#
#   d) After failover completes (~30-60s), verify roles swapped:
#      The previous replica is now primary, and the previous
#      primary is now a replica.
#
# Docs: https://docs.aws.amazon.com/AmazonElastiCache/latest/red-ug/AutoFailover.html
# ============================================================
```

### `outputs.tf`

```hcl
output "cmd_primary_endpoint" {
  value = "Set after TODO 1 implementation"
}

output "cme_configuration_endpoint" {
  value = "Set after TODO 2 implementation"
}
```

---

## Cluster Mode Disabled vs Enabled

| Criterion | Cluster Mode Disabled | Cluster Mode Enabled |
|---|---|---|
| **Shards** | 1 | 1-500 |
| **Max replicas** | 5 per shard | 5 per shard |
| **Total nodes** | 1-6 | 2-1000 |
| **Max data** | Limited by single node memory | Distributed across shards |
| **Write scaling** | Single primary | Multiple primaries (one per shard) |
| **Read scaling** | Add replicas | Add replicas per shard |
| **Multi-key ops** | Full support | Only within same hash slot |
| **Failover** | Automatic (30-60s) | Automatic (30-60s) |
| **Scaling** | Vertical (change node type) | Horizontal (add/remove shards) |
| **Complexity** | Low | Higher (hash slot awareness) |
| **Use case** | Read-heavy, data fits one node | Write-heavy, large datasets |

### When Cluster Mode Enabled Adds No Value

Cluster Mode Enabled adds complexity without benefit when:
- Your dataset fits in a single node's memory
- Your write throughput fits a single primary node (~100K ops/sec for small values)
- Your application uses multi-key commands (MGET, pipeline) extensively
- You have fewer than 5 read replicas worth of read load

---

## Spot the Bug

The following Cluster Mode Enabled configuration has an architectural flaw. Identify the problem before expanding the answer.

```hcl
resource "aws_elasticache_replication_group" "cme" {
  replication_group_id = "my-cluster-mode"
  description          = "Cluster Mode Enabled"
  node_type            = "cache.t3.micro"
  engine_version       = "7.1"
  port                 = 6379

  num_node_groups         = 1
  replicas_per_node_group = 1

  automatic_failover_enabled = true
  multi_az_enabled           = true
  subnet_group_name          = aws_elasticache_subnet_group.this.name
  security_group_ids         = [aws_security_group.cache.id]
}
```

<details>
<summary>Explain the bug</summary>

**Cluster Mode Enabled with a single shard (`num_node_groups = 1`) provides no benefit over Cluster Mode Disabled.** The configuration creates one shard with one primary and one replica -- functionally identical to Cluster Mode Disabled with `num_cache_clusters = 2`, but with the added complexity of cluster mode (hash slot awareness, CROSSSLOT errors for multi-key commands, more complex client configuration).

Cluster Mode Enabled is only beneficial when you have multiple shards to distribute data and writes. With one shard, you get:
- No write distribution (single primary)
- No data partitioning (all data in one shard)
- Cluster mode protocol overhead
- Restricted multi-key operations (must use `{hashtag}` for keys that need to be in the same slot)

**Fix:** Either:

1. Use Cluster Mode Disabled for single-shard deployments:
```hcl
resource "aws_elasticache_replication_group" "cmd" {
  num_cache_clusters = 2  # 1 primary + 1 replica
  # No num_node_groups or replicas_per_node_group
}
```

2. Or use multiple shards if you actually need horizontal scaling:
```hcl
num_node_groups         = 3  # 3 shards for real data partitioning
replicas_per_node_group = 1
```

Do not enable cluster mode "for future scaling" -- it adds complexity today with no benefit. You can migrate from Cluster Mode Disabled to Enabled later if needed.

</details>

---

## Verify What You Learned

1. **Deploy the infrastructure:**
   ```bash
   terraform init && terraform apply -auto-approve
   ```
   Note: Replication group creation takes 10-15 minutes.

2. **Verify Cluster Mode Disabled replication group:**
   ```bash
   aws elasticache describe-replication-groups \
     --replication-group-id saa-ex58-cmd \
     --query 'ReplicationGroups[0].{Status:Status,ClusterEnabled:ClusterEnabled,NodeGroups:NodeGroups[0].NodeGroupMembers[*].{Node:CacheClusterId,Role:CurrentRole,AZ:PreferredAvailabilityZone}}' \
     --output json
   ```
   Expected: `ClusterEnabled = false`, one primary and one replica in different AZs.

3. **Verify Cluster Mode Enabled replication group:**
   ```bash
   aws elasticache describe-replication-groups \
     --replication-group-id saa-ex58-cme \
     --query 'ReplicationGroups[0].{Status:Status,ClusterEnabled:ClusterEnabled,NodeGroups:NodeGroups[*].{ShardId:NodeGroupId,Slots:Slots,Members:NodeGroupMembers[*].{Node:CacheClusterId,Role:CurrentRole}}}' \
     --output json
   ```
   Expected: `ClusterEnabled = true`, two node groups (shards) with different slot ranges.

4. **Test failover:**
   ```bash
   aws elasticache test-failover \
     --replication-group-id saa-ex58-cmd \
     --node-group-id 0001

   # Wait ~60 seconds, then check roles
   sleep 60
   aws elasticache describe-replication-groups \
     --replication-group-id saa-ex58-cmd \
     --query 'ReplicationGroups[0].NodeGroups[0].NodeGroupMembers[*].{Node:CacheClusterId,Role:CurrentRole}' \
     --output table
   ```
   Expected: Roles have swapped (previous replica is now primary).

5. **Verify Terraform state:**
   ```bash
   terraform plan
   ```
   Expected: `No changes. Your infrastructure matches the configuration.` (Note: after failover, the primary endpoint remains the same -- the DNS record is updated.)

---

## Solutions

<details>
<summary>TODO 1 -- Cluster Mode Disabled (database.tf)</summary>

```hcl
resource "aws_elasticache_replication_group" "cmd" {
  replication_group_id = "${var.project_name}-cmd"
  description          = "Cluster Mode Disabled demo"
  node_type            = "cache.t3.micro"
  num_cache_clusters   = 2
  engine_version       = "7.1"
  port                 = 6379

  automatic_failover_enabled = true
  multi_az_enabled           = true
  subnet_group_name          = aws_elasticache_subnet_group.this.name
  security_group_ids         = [aws_security_group.cache.id]
  snapshot_retention_limit   = 1

  tags = { Name = "${var.project_name}-cmd" }
}

output "cmd_primary_endpoint" {
  value = aws_elasticache_replication_group.cmd.primary_endpoint_address
}
```

With `num_cache_clusters = 2`, you get 1 primary and 1 replica. The primary handles all writes; the replica handles read traffic and serves as a failover target.

</details>

<details>
<summary>TODO 2 -- Cluster Mode Enabled (database.tf)</summary>

```hcl
resource "aws_elasticache_replication_group" "cme" {
  replication_group_id = "${var.project_name}-cme"
  description          = "Cluster Mode Enabled demo"
  node_type            = "cache.t3.micro"
  engine_version       = "7.1"
  port                 = 6379

  num_node_groups         = 2
  replicas_per_node_group = 1

  automatic_failover_enabled = true
  multi_az_enabled           = true
  subnet_group_name          = aws_elasticache_subnet_group.this.name
  security_group_ids         = [aws_security_group.cache.id]
  snapshot_retention_limit   = 1

  tags = { Name = "${var.project_name}-cme" }
}

output "cme_configuration_endpoint" {
  value = aws_elasticache_replication_group.cme.configuration_endpoint_address
}
```

With `num_node_groups = 2` and `replicas_per_node_group = 1`, you get 4 total nodes: 2 primaries (one per shard) + 2 replicas. Data is partitioned across the two shards via hash slots.

Note: Cluster Mode Enabled uses the **configuration endpoint** (not the primary endpoint). Clients connect to the configuration endpoint and are redirected to the appropriate shard based on the key's hash slot.

</details>

<details>
<summary>TODO 3 -- Failover Test (CLI)</summary>

```bash
# List current roles
aws elasticache describe-replication-groups \
  --replication-group-id saa-ex58-cmd \
  --query 'ReplicationGroups[0].NodeGroups[0].NodeGroupMembers[*].{Node:CacheClusterId,Role:CurrentRole,AZ:PreferredAvailabilityZone}' \
  --output table

# Trigger failover
aws elasticache test-failover \
  --replication-group-id saa-ex58-cmd \
  --node-group-id 0001

echo "Failover initiated. Waiting 60 seconds..."
sleep 60

# Verify roles swapped
aws elasticache describe-replication-groups \
  --replication-group-id saa-ex58-cmd \
  --query 'ReplicationGroups[0].NodeGroups[0].NodeGroupMembers[*].{Node:CacheClusterId,Role:CurrentRole,AZ:PreferredAvailabilityZone}' \
  --output table
```

During failover:
1. ElastiCache detects the primary is unavailable
2. DNS is updated to point the primary endpoint to the new primary
3. The replica is promoted to primary (~30-60 seconds)
4. The old primary restarts as a replica

</details>

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Replication group deletion takes 5-10 minutes. Verify:

```bash
aws elasticache describe-replication-groups \
  --replication-group-id saa-ex58-cmd 2>&1 || echo "CMD deleted"
aws elasticache describe-replication-groups \
  --replication-group-id saa-ex58-cme 2>&1 || echo "CME deleted"
```

---

## What's Next

You have completed the ElastiCache section. The next set of exercises (starting with exercise 59) covers **Amazon Redshift architecture and distribution styles** -- AWS's data warehousing solution for analytical workloads. The column-oriented storage and massively parallel processing (MPP) architecture provides a fundamentally different performance model from the row-oriented DynamoDB and in-memory ElastiCache you have worked with in this section.

---

## Summary

- **Cluster Mode Disabled** = 1 shard with 1 primary and up to 5 read replicas -- ideal for read-heavy workloads that fit in a single node
- **Cluster Mode Enabled** = 1-500 shards, each with a primary and up to 5 replicas -- required when data exceeds single node memory or write throughput exceeds single primary capacity
- **Automatic failover** promotes a replica to primary in ~30-60 seconds when the primary fails (requires at least 1 replica)
- **Multi-AZ** places primary and replicas in different Availability Zones for resilience
- **Hash slots** (16,384 total) are distributed evenly across shards in Cluster Mode Enabled -- keys are assigned to slots via CRC16
- **Multi-key operations** (MGET, MSET, pipelines) in Cluster Mode Enabled only work within the same hash slot -- use `{hashtag}` to co-locate related keys
- **Single shard in Cluster Mode Enabled provides no benefit** -- use Cluster Mode Disabled for simpler operation
- **Vertical scaling** (larger node type) is simpler; **horizontal scaling** (more shards) is needed only when data or write throughput exceeds single-node limits
- **Configuration endpoint** (Cluster Mode Enabled) vs **primary endpoint** (Cluster Mode Disabled) -- clients must connect to the correct endpoint type
- **test-failover** is a non-destructive way to validate your failover configuration -- always test before relying on it in production

## Reference

- [ElastiCache Replication Groups](https://docs.aws.amazon.com/AmazonElastiCache/latest/red-ug/Replication.html)
- [Cluster Mode Overview](https://docs.aws.amazon.com/AmazonElastiCache/latest/red-ug/Replication.Redis-RedisCluster.html)
- [Testing Automatic Failover](https://docs.aws.amazon.com/AmazonElastiCache/latest/red-ug/AutoFailover.html#auto-failover-test)
- [Terraform aws_elasticache_replication_group](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/elasticache_replication_group)

## Additional Resources

- [Choosing Node Types](https://docs.aws.amazon.com/AmazonElastiCache/latest/red-ug/nodes-select-size.html) -- memory, network, and CPU considerations for node selection
- [Online Resharding](https://docs.aws.amazon.com/AmazonElastiCache/latest/red-ug/scaling-redis-cluster-mode-enabled.html) -- adding or removing shards without downtime
- [Redis Cluster Specification](https://redis.io/docs/reference/cluster-spec/) -- hash slot assignment and MOVED/ASK redirections
- [ElastiCache Global Datastore](https://docs.aws.amazon.com/AmazonElastiCache/latest/red-ug/Redis-Global-Datastore.html) -- cross-region replication for ElastiCache Redis (analogous to DynamoDB Global Tables)

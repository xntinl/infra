# 56. DynamoDB Global Tables for Multi-Region Active-Active

<!--
difficulty: intermediate
concepts: [dynamodb-global-tables, multi-region, active-active, last-writer-wins, replication-latency, global-secondary-index, replica-auto-scaling, conflict-resolution]
tools: [terraform, aws-cli]
estimated_time: 50m
bloom_level: apply, analyze
prerequisites: [55-dynamodb-capacity-modes-throttling]
aws_cost: ~$0.05/hr
-->

> **AWS Cost Warning:** DynamoDB Global Tables charge for replicated write capacity units (rWCU at $0.000975/hr each, ~1.5x standard WCU cost) plus cross-region data transfer ($0.09/GB). With minimal provisioned capacity across 2 regions, cost ~$0.05/hr. Remember to run `terraform destroy` when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Completed exercise 55 (DynamoDB Capacity) | RCU/WCU concepts |
| Two AWS regions available | `aws ec2 describe-regions --query 'Regions[*].RegionName'` |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Implement** a DynamoDB Global Table with replicas in multiple regions using Terraform.
2. **Analyze** the last-writer-wins conflict resolution mechanism and its implications for concurrent writes.
3. **Apply** auto scaling configuration to replica tables to handle region-specific traffic patterns.
4. **Evaluate** the cost trade-offs of Global Tables (1.5x write cost + data transfer) vs application-level replication.
5. **Design** multi-region architectures that use Global Tables for active-active or active-passive patterns.

---

## Why This Matters

DynamoDB Global Tables is the AWS-managed solution for multi-region, active-active databases, and the SAA-C03 exam tests it in scenarios requiring low-latency global access and disaster recovery. A Global Table automatically replicates data across up to five AWS regions with sub-second replication latency. Any replica accepts writes, making it truly active-active -- unlike read replicas in RDS, where only the primary accepts writes.

The key architectural trade-off is the conflict resolution model: last-writer-wins based on a record-level timestamp. If two users update the same item in two different regions within the replication window, the write with the later timestamp wins and the earlier write is silently overwritten. There is no merge or conflict notification. This means Global Tables work well for user-specific data (each user writes in their closest region) but poorly for shared counters or collaborative editing. The exam tests whether you understand this limitation and when to recommend Global Tables vs other multi-region patterns (Aurora Global Database, ElastiCache Global Datastore).

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
  region = "us-east-1"
  alias  = "primary"
}

provider "aws" {
  region = "eu-west-1"
  alias  = "replica"
}
```

### `variables.tf`

```hcl
variable "project_name" {
  description = "Project name for resource naming and tagging"
  type        = string
  default     = "saa-ex56"
}
```

### `database.tf`

```hcl
# ============================================================
# TODO 1: Global Table with Replica
# ============================================================
# Create a DynamoDB table with a replica in eu-west-1 to form
# a Global Table. In Terraform, you define the primary table
# with a `replica` block for each additional region.
#
# Requirements:
#   - Resource: aws_dynamodb_table
#   - billing_mode = "PAY_PER_REQUEST" (simplest for global tables)
#   - hash_key = "PK", range_key = "SK" (both type "S")
#   - stream_enabled = true
#   - stream_view_type = "NEW_AND_OLD_IMAGES"
#     (required for Global Tables replication)
#   - replica block with region_name = "eu-west-1"
#
# Note: Global Tables Version 2019.11.21 (current) requires
# DynamoDB Streams enabled with NEW_AND_OLD_IMAGES.
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/dynamodb_table#replica
# ============================================================


# ============================================================
# TODO 2: Write in Primary, Read in Replica
# ============================================================
# This TODO is CLI-based. After terraform apply:
#
#   a) Write an item to the primary region (us-east-1):
#      aws dynamodb put-item \
#        --table-name TABLE_NAME \
#        --item '{"PK":{"S":"USER#100"},"SK":{"S":"PROFILE"},"name":{"S":"Alice"},"region":{"S":"us-east-1"}}' \
#        --region us-east-1
#
#   b) Wait 1-2 seconds for replication
#
#   c) Read the item from the replica region (eu-west-1):
#      aws dynamodb get-item \
#        --table-name TABLE_NAME \
#        --key '{"PK":{"S":"USER#100"},"SK":{"S":"PROFILE"}}' \
#        --region eu-west-1
#
#   d) Write directly to the replica region:
#      aws dynamodb put-item \
#        --table-name TABLE_NAME \
#        --item '{"PK":{"S":"USER#200"},"SK":{"S":"PROFILE"},"name":{"S":"Bob"},"region":{"S":"eu-west-1"}}' \
#        --region eu-west-1
#
#   e) Verify replication back to primary:
#      aws dynamodb get-item \
#        --table-name TABLE_NAME \
#        --key '{"PK":{"S":"USER#200"},"SK":{"S":"PROFILE"}}' \
#        --region us-east-1
# ============================================================


# ============================================================
# TODO 3: Demonstrate Last-Writer-Wins
# ============================================================
# This TODO is CLI-based. Demonstrate the conflict resolution:
#
#   a) Write to the same item from both regions simultaneously:
#      aws dynamodb put-item \
#        --table-name TABLE_NAME \
#        --item '{"PK":{"S":"USER#CONFLICT"},"SK":{"S":"DATA"},"value":{"S":"from-us-east-1"},"ts":{"S":"2026-03-08T10:00:00Z"}}' \
#        --region us-east-1 &
#
#      aws dynamodb put-item \
#        --table-name TABLE_NAME \
#        --item '{"PK":{"S":"USER#CONFLICT"},"SK":{"S":"DATA"},"value":{"S":"from-eu-west-1"},"ts":{"S":"2026-03-08T10:00:00Z"}}' \
#        --region eu-west-1 &
#
#      wait
#
#   b) Wait 5 seconds for replication to settle
#
#   c) Read from both regions:
#      aws dynamodb get-item --table-name TABLE_NAME \
#        --key '{"PK":{"S":"USER#CONFLICT"},"SK":{"S":"DATA"}}' \
#        --region us-east-1
#
#      aws dynamodb get-item --table-name TABLE_NAME \
#        --key '{"PK":{"S":"USER#CONFLICT"},"SK":{"S":"DATA"}}' \
#        --region eu-west-1
#
#   Both regions will show the SAME value: whichever write
#   had the later DynamoDB-assigned timestamp wins.
#   The "losing" write is silently discarded.
# ============================================================
```

### `outputs.tf`

```hcl
output "table_name" {
  value = "Set after TODO 1 is implemented"
}
```

---

## Global Tables Architecture

```
Region: us-east-1                     Region: eu-west-1
+------------------------+            +------------------------+
|  DynamoDB Table        |            |  DynamoDB Table        |
|  (Primary Replica)     |            |  (Replica)             |
|                        |  <-------> |                        |
|  Accepts reads/writes  |  DynamoDB  |  Accepts reads/writes  |
|  Full table copy       |  Streams   |  Full table copy       |
+------------------------+            +------------------------+
         ^                                      ^
         |                                      |
    Application                            Application
    (US users)                             (EU users)
```

Replication is automatic, fully managed, and typically completes within 1 second.

---

## Spot the Bug

The following Global Table configuration has an operational flaw that will cause cost issues. Identify the problem before expanding the answer.

```hcl
resource "aws_dynamodb_table" "global" {
  name         = "my-global-table"
  billing_mode = "PROVISIONED"
  hash_key     = "PK"
  range_key    = "SK"

  read_capacity  = 100
  write_capacity = 100

  stream_enabled   = true
  stream_view_type = "NEW_AND_OLD_IMAGES"

  attribute {
    name = "PK"
    type = "S"
  }

  attribute {
    name = "SK"
    type = "S"
  }

  replica {
    region_name = "eu-west-1"
  }
}
```

The table receives 100 writes/second in us-east-1 and 10 writes/second in eu-west-1.

<details>
<summary>Explain the bug</summary>

**The replica in eu-west-1 has provisioned capacity but no auto scaling, and the capacity is inherited from the primary table.** While the replica receives only 10 direct writes/second, it also receives all 100 writes/second replicated from us-east-1. That is 110 writes/second total to the replica, but the provisioned WCU is only 100 (inherited from the primary).

The replica will be throttled on replicated writes, causing replication lag to increase. Replicated writes consume the same WCU as direct writes on the replica.

**Fix:** Either use on-demand capacity (`PAY_PER_REQUEST`) to avoid capacity planning across regions, or configure auto scaling on both the primary and replica tables:

```hcl
billing_mode = "PAY_PER_REQUEST"
```

Or with provisioned capacity, ensure the replica has enough capacity for direct writes PLUS replicated writes:

```hcl
replica {
  region_name = "eu-west-1"
  # In Terraform, you can configure per-replica capacity
  # via aws_dynamodb_table_replica or auto scaling
}
```

The general recommendation for Global Tables is to use on-demand capacity unless you have very predictable, region-specific traffic patterns with proper auto scaling configured on each replica.

</details>

---

## Cost Analysis: Global Tables

| Component | Standard Table | Global Table (2 regions) |
|---|---|---|
| Write cost | $1.25/million WRU | $1.875/million rWRU (1.5x) |
| Read cost | $0.25/million RRU | $0.25/million RRU (same) |
| Data transfer | N/A | $0.09/GB cross-region |
| Storage | $0.25/GB/month | $0.25/GB/month per region |

**Key insight:** Writes cost 1.5x more because replicated Write Request Units (rWRU) are priced at $1.875/million vs $1.25/million for standard. Reads are the same price because they are served locally from the regional replica.

---

## Verify What You Learned

1. **Deploy the infrastructure:**
   ```bash
   terraform init && terraform apply -auto-approve
   ```

2. **Verify the Global Table has replicas:**
   ```bash
   TABLE_NAME="saa-ex56-global"

   aws dynamodb describe-table \
     --table-name "$TABLE_NAME" \
     --region us-east-1 \
     --query 'Table.Replicas[*].{Region:RegionName,Status:ReplicaStatus}' \
     --output table
   ```
   Expected: Two replicas (us-east-1, eu-west-1) with status `ACTIVE`.

3. **Write in us-east-1, read in eu-west-1:**
   ```bash
   aws dynamodb put-item \
     --table-name "$TABLE_NAME" \
     --item '{"PK":{"S":"USER#100"},"SK":{"S":"PROFILE"},"name":{"S":"Alice"}}' \
     --region us-east-1

   sleep 2

   aws dynamodb get-item \
     --table-name "$TABLE_NAME" \
     --key '{"PK":{"S":"USER#100"},"SK":{"S":"PROFILE"}}' \
     --region eu-west-1 \
     --query 'Item.name.S'
   ```
   Expected: `"Alice"`

4. **Verify streams are enabled:**
   ```bash
   aws dynamodb describe-table \
     --table-name "$TABLE_NAME" \
     --region us-east-1 \
     --query 'Table.{StreamEnabled:StreamSpecification.StreamEnabled,StreamView:StreamSpecification.StreamViewType}'
   ```
   Expected: `StreamEnabled = true`, `StreamView = NEW_AND_OLD_IMAGES`

5. **Verify Terraform state:**
   ```bash
   terraform plan
   ```
   Expected: `No changes. Your infrastructure matches the configuration.`

---

## Solutions

<details>
<summary>TODO 1 -- Global Table with Replica (database.tf)</summary>

```hcl
resource "aws_dynamodb_table" "global" {
  provider     = aws.primary
  name         = "${var.project_name}-global"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "PK"
  range_key    = "SK"

  stream_enabled   = true
  stream_view_type = "NEW_AND_OLD_IMAGES"

  attribute {
    name = "PK"
    type = "S"
  }

  attribute {
    name = "SK"
    type = "S"
  }

  replica {
    region_name = "eu-west-1"
  }

  tags = { Name = "${var.project_name}-global" }
}

output "table_name" {
  value = aws_dynamodb_table.global.name
}
```

</details>

<details>
<summary>TODO 2 -- Write/Read Commands (CLI)</summary>

```bash
TABLE_NAME=$(terraform output -raw table_name)

# Write in primary region
aws dynamodb put-item \
  --table-name "$TABLE_NAME" \
  --item '{"PK":{"S":"USER#100"},"SK":{"S":"PROFILE"},"name":{"S":"Alice"},"region":{"S":"us-east-1"}}' \
  --region us-east-1

# Wait for replication
sleep 2

# Read from replica
aws dynamodb get-item \
  --table-name "$TABLE_NAME" \
  --key '{"PK":{"S":"USER#100"},"SK":{"S":"PROFILE"}}' \
  --region eu-west-1

# Write directly to replica
aws dynamodb put-item \
  --table-name "$TABLE_NAME" \
  --item '{"PK":{"S":"USER#200"},"SK":{"S":"PROFILE"},"name":{"S":"Bob"},"region":{"S":"eu-west-1"}}' \
  --region eu-west-1

sleep 2

# Verify replication back to primary
aws dynamodb get-item \
  --table-name "$TABLE_NAME" \
  --key '{"PK":{"S":"USER#200"},"SK":{"S":"PROFILE"}}' \
  --region us-east-1
```

</details>

<details>
<summary>TODO 3 -- Last-Writer-Wins Demonstration (CLI)</summary>

```bash
TABLE_NAME=$(terraform output -raw table_name)

# Concurrent writes to the same item from two regions
aws dynamodb put-item \
  --table-name "$TABLE_NAME" \
  --item '{"PK":{"S":"USER#CONFLICT"},"SK":{"S":"DATA"},"value":{"S":"from-us-east-1"}}' \
  --region us-east-1 &

aws dynamodb put-item \
  --table-name "$TABLE_NAME" \
  --item '{"PK":{"S":"USER#CONFLICT"},"SK":{"S":"DATA"},"value":{"S":"from-eu-west-1"}}' \
  --region eu-west-1 &

wait
sleep 5

# Both regions show the same winner
echo "=== us-east-1 ==="
aws dynamodb get-item \
  --table-name "$TABLE_NAME" \
  --key '{"PK":{"S":"USER#CONFLICT"},"SK":{"S":"DATA"}}' \
  --region us-east-1 --query 'Item.value.S'

echo "=== eu-west-1 ==="
aws dynamodb get-item \
  --table-name "$TABLE_NAME" \
  --key '{"PK":{"S":"USER#CONFLICT"},"SK":{"S":"DATA"}}' \
  --region eu-west-1 --query 'Item.value.S'
```

Both regions will show the same value. The write with the later DynamoDB-assigned timestamp wins.

</details>

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Note: Destroying a Global Table removes all replicas. This may take a few minutes.

```bash
terraform state list
```

Expected: no output (empty state).

---

## What's Next

Exercise 57 covers **ElastiCache Redis vs Memcached**, where you will shift from DynamoDB to in-memory caching. You will compare Redis (persistence, replication, complex data types) with Memcached (multi-threaded, simple key-value), build both with Terraform, and develop a decision framework for choosing between them.

---

## Summary

- **DynamoDB Global Tables** provide fully managed, active-active multi-region replication with sub-second latency
- **Both replicas accept reads and writes** -- truly active-active, unlike RDS read replicas
- **Last-writer-wins** conflict resolution uses DynamoDB-assigned timestamps -- no merge or notification for conflicting writes
- **DynamoDB Streams** (NEW_AND_OLD_IMAGES) must be enabled for Global Tables replication
- **Replicated writes cost 1.5x** standard writes ($1.875/million rWRU vs $1.25/million WRU)
- **Reads are local** and cost the same as standard tables ($0.25/million RRU)
- **Cross-region data transfer** costs $0.09/GB for replicated data
- **On-demand is recommended** for Global Tables to avoid capacity planning across regions
- **Replica must have sufficient WCU** to handle both direct writes AND replicated writes (common sizing mistake)
- Global Tables are ideal for **user-specific data** where each user writes in their closest region; not ideal for shared state with frequent concurrent updates

## Reference

- [DynamoDB Global Tables](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/GlobalTables.html)
- [Global Tables Conflict Resolution](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/V2globaltables_HowItWorks.html)
- [Terraform DynamoDB Table Replica](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/dynamodb_table#replica)
- [DynamoDB Pricing - Global Tables](https://aws.amazon.com/dynamodb/pricing/)

## Additional Resources

- [Global Tables Best Practices](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/V2globaltables_HowItWorks.html#V2globaltables_HowItWorks.BestPractices) -- data modeling patterns that work well with last-writer-wins
- [Monitoring Global Tables](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/V2globaltables-monitoring.html) -- CloudWatch metrics for replication latency and errors
- [DynamoDB Streams](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/Streams.html) -- the underlying mechanism for Global Tables replication
- [Multi-Region Architectures](https://aws.amazon.com/blogs/database/how-to-use-amazon-dynamodb-global-tables-to-power-multiregion-architectures/) -- AWS blog on designing with Global Tables

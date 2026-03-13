# 62. Amazon Keyspaces and Cassandra Migration

<!--
difficulty: advanced
concepts: [keyspaces, cassandra, cql, wide-column, partition-key, clustering-key, on-demand-capacity, provisioned-capacity, serverless, ttl, point-in-time-recovery]
tools: [terraform, aws-cli, cqlsh]
estimated_time: 50m
bloom_level: evaluate, create
prerequisites: [55-dynamodb-capacity-modes-throttling]
aws_cost: ~$0.05/hr
-->

> **AWS Cost Warning:** Amazon Keyspaces charges per read/write operation (on-demand mode) and storage. For this exercise with small test data, costs are negligible (~$0.05/hr). No minimum instance costs. Remember to clean up tables when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| cqlsh or cqlsh-expansion installed | `cqlsh --version` or `pip install cqlsh-expansion` |
| Python 3 installed (for cqlsh) | `python3 --version` |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Create** Amazon Keyspaces keyspaces and tables using both Terraform and CQL.
2. **Evaluate** the architectural differences between Keyspaces (serverless, managed) and self-managed Apache Cassandra (cluster, nodes, replication factor).
3. **Analyze** which Apache Cassandra features are supported in Keyspaces and which are not -- critical for migration planning.
4. **Design** a Keyspaces table schema with appropriate partition keys and clustering columns for specific access patterns.
5. **Justify** the choice between Keyspaces and DynamoDB for wide-column workloads based on existing Cassandra expertise, CQL requirements, and cost model differences.

---

## Why This Matters

Amazon Keyspaces appears on the SAA-C03 exam as the managed Cassandra option. The critical architect decision is whether to migrate an existing Cassandra workload to Keyspaces (managed, serverless, CQL-compatible) or to DynamoDB (also managed but requires rewriting queries from CQL to DynamoDB API). Keyspaces preserves CQL compatibility, which reduces migration effort for teams with existing Cassandra applications. However, Keyspaces does not support all Cassandra features -- user-defined functions, materialized views, logged batches across partitions, and certain CQL data types are not available.

The architectural insight is that Keyspaces is serverless -- there are no nodes to provision, no replication factor to configure, and no cluster topology to manage. Data is automatically replicated across three AZs. This eliminates the operational burden of Cassandra cluster management (node replacement, compaction tuning, repair operations) but removes the fine-grained control that some workloads require. The exam tests whether you can identify when this trade-off is acceptable.

---

## Architecture Comparison

```
Self-Managed Apache Cassandra:
  Cluster (you manage)
    --> Node 1 (data + coordination)
    --> Node 2 (data + coordination)
    --> Node 3 (data + coordination)
    --> ... (more nodes for capacity)
  Replication: configurable (RF=3 typical)
  Consistency: tunable (ONE, QUORUM, ALL)
  Operations: YOU manage compaction, repair, node replacement

Amazon Keyspaces:
  Serverless (AWS manages)
    --> Automatic replication across 3 AZs
    --> No nodes to provision or manage
    --> Capacity: on-demand or provisioned (like DynamoDB)
  Replication: always 3 AZs (not configurable)
  Consistency: LOCAL_QUORUM only (eventual not supported for writes)
  Operations: AWS manages everything
```

---

## Hints

This is an advanced exercise. Use these hints if you get stuck.

<details>
<summary>Hint 1: Connecting cqlsh to Keyspaces</summary>

Keyspaces requires TLS and SigV4 authentication (or service-specific credentials). The easiest approach is service-specific credentials:

1. Create credentials in IAM console: IAM > Users > Security Credentials > Cassandra Credentials
2. Download the Starfield digital certificate:
   ```bash
   curl https://certs.secureserver.net/repository/sf-class2-root.crt -O
   ```
3. Create a `cassandra.conf` file:
   ```ini
   [connection]
   port = 9142
   factory = cqlshlib.ssl.ssl_transport_factory

   [ssl]
   validate = true
   certfile = /path/to/sf-class2-root.crt

   [authentication]
   username = YOUR_SERVICE_USERNAME
   password = YOUR_SERVICE_PASSWORD
   ```
4. Connect:
   ```bash
   cqlsh cassandra.us-east-1.amazonaws.com 9142 --ssl --cqlshrc=cassandra.conf
   ```

</details>

<details>
<summary>Hint 2: Capacity Modes</summary>

Keyspaces supports two capacity modes (similar to DynamoDB):

- **On-demand:** Pay per read/write request. No capacity planning. Good for unpredictable workloads.
- **Provisioned:** Specify read/write capacity units. Cheaper for predictable, steady workloads. Supports auto-scaling.

Set via Terraform:
```hcl
resource "aws_keyspaces_table" "this" {
  capacity_specification {
    throughput_mode = "PAY_PER_REQUEST"  # or "PROVISIONED"
    # read_capacity_units  = 10  # only for PROVISIONED
    # write_capacity_units = 10  # only for PROVISIONED
  }
}
```

</details>

<details>
<summary>Hint 3: CQL Differences from Apache Cassandra</summary>

Key differences to be aware of:
- **No user-defined functions (UDFs)** -- use application-level logic instead
- **No materialized views** -- use denormalized tables (same pattern as Cassandra best practice)
- **No lightweight transactions (LWT) with IF NOT EXISTS on UPDATE** -- INSERT with IF NOT EXISTS is supported
- **No BATCH across partitions** -- single-partition batches only
- **No counters** -- use application-level atomic operations
- **TTL supported** -- but set at table or row level via CQL, not column level
- **Consistency levels** -- only `LOCAL_QUORUM` and `LOCAL_ONE` for reads; writes always `LOCAL_QUORUM`

</details>

---

## Requirements

Using the hints above and the AWS documentation, complete these tasks:

1. **Create a Keyspaces keyspace** using Terraform with `SingleRegion` replication strategy.

2. **Create two tables** -- one via Terraform, one via CQL:
   - Terraform table: `sensor_data` with partition key `sensor_id (text)` and clustering column `timestamp (timestamp)`, on-demand capacity, TTL enabled (90 days default).
   - CQL table: `user_events` with partition key `user_id (text)`, clustering column `event_time (timestamp) DESC`, and static column `user_name (text)`.

3. **Insert and query data** using CQL to verify both tables work.

4. **Compare feature availability** -- attempt operations that would work in Apache Cassandra but fail in Keyspaces.

---

## Spot the Bug

A team is migrating their Apache Cassandra application to Amazon Keyspaces. Their existing Cassandra schema includes:

```sql
-- Existing Apache Cassandra schema
CREATE KEYSPACE analytics WITH replication = {
  'class': 'NetworkTopologyStrategy',
  'us-east': 3,
  'eu-west': 3
};

CREATE TABLE analytics.page_views (
  site_id text,
  page_url text,
  view_count counter,
  PRIMARY KEY (site_id, page_url)
);

CREATE MATERIALIZED VIEW analytics.views_by_page AS
  SELECT * FROM analytics.page_views
  WHERE page_url IS NOT NULL AND site_id IS NOT NULL
  PRIMARY KEY (page_url, site_id);

CREATE FUNCTION analytics.normalize_url(url text)
  RETURNS NULL ON NULL INPUT
  RETURNS text
  LANGUAGE java
  AS 'return url.toLowerCase().replaceAll("/$", "");';
```

They attempt to recreate this schema in Keyspaces and expect everything to work.

<details>
<summary>Explain the bug</summary>

**Three features in this schema are not supported by Amazon Keyspaces:**

1. **Counter columns (`counter` type):** Keyspaces does not support the Cassandra counter data type. The `page_views` table with `view_count counter` will fail to create.

2. **Materialized views (`CREATE MATERIALIZED VIEW`):** Keyspaces does not support materialized views. The `views_by_page` view will fail.

3. **User-defined functions (`CREATE FUNCTION`):** Keyspaces does not support UDFs. The `normalize_url` function will fail.

Additionally, the replication strategy `NetworkTopologyStrategy` with multi-region configuration is not applicable -- Keyspaces automatically replicates across 3 AZs within a single region (use Keyspaces multi-Region for cross-region replication).

**Fix:** Redesign the schema for Keyspaces compatibility:

```sql
-- Keyspaces-compatible schema
CREATE KEYSPACE analytics WITH replication = {
  'class': 'SingleRegion'
};

-- Replace counter with regular integer + application-level increment
CREATE TABLE analytics.page_views (
  site_id text,
  page_url text,
  view_count bigint,
  PRIMARY KEY (site_id, page_url)
);

-- Replace materialized view with a denormalized table
-- (maintain via application-level dual writes)
CREATE TABLE analytics.views_by_page (
  page_url text,
  site_id text,
  view_count bigint,
  PRIMARY KEY (page_url, site_id)
);

-- Replace UDF with application-level logic
-- normalize_url() must be implemented in application code
```

The migration requires application code changes to:
- Atomically increment `view_count` using read-modify-write (or use DynamoDB atomic counters if this pattern is critical)
- Dual-write to both `page_views` and `views_by_page` tables
- Move URL normalization to application code

</details>

---

## Verify What You Learned

1. **Deploy the infrastructure:**
   ```bash
   terraform init && terraform apply -auto-approve
   ```

2. **Verify the keyspace:**
   ```bash
   aws keyspaces list-keyspaces \
     --query 'keyspaces[?keyspaceName==`saa_ex62`].{Name:keyspaceName,ARN:resourceArn}' \
     --output table
   ```
   Expected: One keyspace named `saa_ex62`.

3. **Verify the table:**
   ```bash
   aws keyspaces list-tables \
     --keyspace-name saa_ex62 \
     --query 'tables[*].{Name:tableName,Status:status}' \
     --output table
   ```
   Expected: `sensor_data` table with Status = `ACTIVE`.

4. **Verify TTL configuration:**
   ```bash
   aws keyspaces get-table \
     --keyspace-name saa_ex62 \
     --table-name sensor_data \
     --query '{TTL:ttl,CapacityMode:capacitySpecification.throughputMode}' \
     --output json
   ```
   Expected: TTL status = `ENABLED`, throughput mode = `PAY_PER_REQUEST`.

5. **Test CQL operations (from cqlsh):**
   ```sql
   INSERT INTO saa_ex62.sensor_data (sensor_id, timestamp, temperature, humidity)
   VALUES ('sensor-001', toTimestamp(now()), 22.5, 65.0);

   SELECT * FROM saa_ex62.sensor_data WHERE sensor_id = 'sensor-001';
   ```
   Expected: One row returned.

6. **Verify Terraform state:**
   ```bash
   terraform plan
   ```
   Expected: `No changes. Your infrastructure matches the configuration.`

---

## Full Solution

<details>
<summary>Complete Terraform Configuration</summary>

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
  default     = "saa-ex62"
}
```

### `main.tf`

```hcl
# ------------------------------------------------------------------
# Keyspace (equivalent to a Cassandra keyspace)
# Replication is always SingleRegion in Keyspaces — data is
# automatically replicated across 3 AZs. You cannot configure
# replication factor or strategy like in self-managed Cassandra.
# ------------------------------------------------------------------
resource "aws_keyspaces_keyspace" "this" {
  name = "saa_ex62"

  tags = { Name = "${var.project_name}-keyspace" }
}

# ------------------------------------------------------------------
# Table with on-demand capacity and TTL
# Partition key: sensor_id (determines which partition stores the row)
# Clustering column: timestamp (determines sort order within partition)
# ------------------------------------------------------------------
resource "aws_keyspaces_table" "sensor_data" {
  keyspace_name = aws_keyspaces_keyspace.this.name
  table_name    = "sensor_data"

  schema_definition {
    column {
      name = "sensor_id"
      type = "text"
    }
    column {
      name = "timestamp"
      type = "timestamp"
    }
    column {
      name = "temperature"
      type = "double"
    }
    column {
      name = "humidity"
      type = "double"
    }

    partition_key {
      name = "sensor_id"
    }

    clustering_key {
      name     = "timestamp"
      order_by = "DESC"
    }
  }

  capacity_specification {
    throughput_mode = "PAY_PER_REQUEST"
  }

  ttl {
    status = "ENABLED"
  }

  default_time_to_live = 7776000  # 90 days in seconds

  point_in_time_recovery {
    status = "ENABLED"
  }

  tags = { Name = "${var.project_name}-sensor-data" }
}
```

### `outputs.tf`

```hcl
output "keyspace_name" {
  value = aws_keyspaces_keyspace.this.name
}

output "table_name" {
  value = aws_keyspaces_table.sensor_data.table_name
}

output "cqlsh_connect" {
  value = "cqlsh cassandra.${var.region}.amazonaws.com 9142 --ssl"
}
```

</details>

<details>
<summary>CQL Commands for user_events Table</summary>

```sql
-- Connect to Keyspaces via cqlsh first

-- Create the user_events table (not managed by Terraform)
CREATE TABLE saa_ex62.user_events (
  user_id    text,
  event_time timestamp,
  user_name  text STATIC,
  event_type text,
  event_data text,
  PRIMARY KEY (user_id, event_time)
) WITH CLUSTERING ORDER BY (event_time DESC)
  AND CUSTOM_PROPERTIES = {'capacity_mode': {'throughput_mode': 'PAY_PER_REQUEST'}};

-- Insert test data
INSERT INTO saa_ex62.user_events (user_id, event_time, user_name, event_type, event_data)
VALUES ('user-001', toTimestamp(now()), 'Alice', 'login', '{"ip": "10.0.1.5"}');

INSERT INTO saa_ex62.user_events (user_id, event_time, event_type, event_data)
VALUES ('user-001', toTimestamp(now()), 'page_view', '{"url": "/dashboard"}');

-- Static column user_name is shared across all rows in the partition
SELECT * FROM saa_ex62.user_events WHERE user_id = 'user-001';

-- Test unsupported feature: this will fail
-- CREATE MATERIALIZED VIEW saa_ex62.events_by_type AS
--   SELECT * FROM saa_ex62.user_events
--   WHERE event_type IS NOT NULL AND user_id IS NOT NULL AND event_time IS NOT NULL
--   PRIMARY KEY (event_type, event_time, user_id);
-- Error: Materialized views are not supported
```

</details>

---

## Keyspaces vs DynamoDB

Choose Keyspaces when migrating existing Cassandra applications (CQL compatibility reduces rewrite effort). Choose DynamoDB for new applications (richer feature set: GSIs, streams, DAX, multi-item transactions). Both use similar per-request pricing models.

---

## Cleanup

```bash
terraform destroy -auto-approve
```

If you created the `user_events` table via CQL, drop it first:

```sql
DROP TABLE saa_ex62.user_events;
```

Verify:

```bash
aws keyspaces list-keyspaces \
  --query 'keyspaces[?keyspaceName==`saa_ex62`]' --output text
```

Expected: No output (keyspace deleted).

---

## What's Next

Exercise 63 covers **AWS Database Migration Service (DMS) and Schema Conversion**. You will configure a DMS replication instance, source and target endpoints, and a migration task with change data capture (CDC) -- the standard approach for migrating databases to AWS with minimal downtime.

---

## Summary

- **Amazon Keyspaces** is serverless managed Apache Cassandra -- no nodes to provision, automatic 3-AZ replication, on-demand or provisioned capacity
- **CQL compatibility** reduces migration effort from self-managed Cassandra but is not 100% -- audit feature usage before committing
- **Not supported:** counters, materialized views, user-defined functions, logged batches across partitions, ALLOW FILTERING
- **Capacity modes** mirror DynamoDB: on-demand (pay per request) for unpredictable workloads, provisioned with auto-scaling for steady workloads
- **TTL** is supported for automatic data expiration -- set at table level (default_time_to_live) or row level (USING TTL)
- **Point-in-time recovery** enables restoring a table to any point within the last 35 days
- **Choose Keyspaces over DynamoDB** when migrating existing Cassandra applications (CQL compatibility reduces rewrite effort)
- **Choose DynamoDB over Keyspaces** for new applications (richer feature set: GSIs, streams, DAX, transactions)
- **Partition key design** is critical -- hot partitions cause throttling regardless of capacity mode

## Reference

- [Amazon Keyspaces Developer Guide](https://docs.aws.amazon.com/keyspaces/latest/devguide/what-is-keyspaces.html)
- [Supported Cassandra Features](https://docs.aws.amazon.com/keyspaces/latest/devguide/cassandra-apis.html)
- [Keyspaces Functional Differences](https://docs.aws.amazon.com/keyspaces/latest/devguide/cassandra-apis.html#cassandra-api-differences)
- [Terraform aws_keyspaces_table](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/keyspaces_table)

## Additional Resources

- [Migrating to Keyspaces](https://docs.aws.amazon.com/keyspaces/latest/devguide/migrating.html) -- step-by-step guide using cqlsh COPY, DMS, or custom tools
- [Keyspaces Capacity Planning](https://docs.aws.amazon.com/keyspaces/latest/devguide/ReadWriteCapacityMode.html) -- on-demand vs provisioned, auto-scaling configuration
- [CQL Reference for Keyspaces](https://docs.aws.amazon.com/keyspaces/latest/devguide/cql-reference.html) -- complete CQL statement support matrix
- [Cassandra to Keyspaces Compatibility Tool](https://docs.aws.amazon.com/keyspaces/latest/devguide/migrating-using-toolkit.html) -- analyze your Cassandra schema for compatibility issues

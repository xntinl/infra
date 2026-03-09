# 59. Redshift Architecture and Distribution Styles

<!--
difficulty: intermediate
concepts: [redshift-cluster, leader-node, compute-nodes, distribution-style, sort-key, columnar-storage, mpp, compression-encoding, spectrum, concurrency-scaling]
tools: [terraform, aws-cli, psql]
estimated_time: 50m
bloom_level: apply, analyze
prerequisites: [55-dynamodb-capacity-modes-throttling]
aws_cost: ~$0.25/hr
-->

> **AWS Cost Warning:** Redshift dc2.large single-node cluster costs ~$0.25/hr. Multi-node clusters cost more. Remember to run `terraform destroy` immediately when finished. A 50-minute exercise costs approximately $0.21.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| psql client installed | `psql --version` |
| Understanding of SQL and relational databases | Basic SQL knowledge |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Describe** the Redshift architecture: leader node (query parsing, planning, coordination) and compute nodes (parallel data storage and execution).
2. **Apply** distribution styles (AUTO, KEY, EVEN, ALL) to tables based on query patterns and data characteristics.
3. **Evaluate** which sort key strategy (compound vs interleaved) optimizes specific query patterns.
4. **Analyze** query execution plans to identify data redistribution overhead from incorrect distribution style choices.
5. **Design** a Redshift schema that minimizes data movement for common join and aggregation patterns.

---

## Why This Matters

Redshift appears on the SAA-C03 exam in scenarios involving data warehousing, analytics, and OLAP workloads. The exam tests whether you understand why Redshift uses columnar storage (scanning only the columns needed for a query), massively parallel processing (distributing work across compute nodes), and distribution styles (controlling how data is physically placed across nodes to minimize inter-node data movement during joins).

The architect decision that matters most is distribution style selection. When two large tables are joined frequently, placing them on the same compute nodes via KEY distribution on the join column eliminates network-intensive data redistribution. When a small dimension table is joined against many fact tables, ALL distribution replicates it to every node so the join is always local. Getting this wrong does not cause errors -- queries still work -- but they run orders of magnitude slower because data must be broadcast or redistributed across the network at query time. This is the kind of silent performance degradation that only shows up under production load.

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
  default     = "saa-ex59"
}
```

### `database.tf`

```hcl
# ============================================================
# TODO 1: Create a Redshift Cluster
# ============================================================
# Create a single-node Redshift cluster for testing distribution
# styles and sort keys.
#
# Architecture (single node):
#   Leader Node + Compute Node (combined in single-node cluster)
#   - Leader: parses SQL, creates execution plan, coordinates
#   - Compute: stores data in columnar format, executes queries
#
# Architecture (multi-node):
#   Leader Node (no data storage)
#     --> Compute Node 1 (slices 0, 1)
#     --> Compute Node 2 (slices 2, 3)
#     --> ... up to 128 nodes
#
# Requirements:
#   - Resource: aws_redshift_cluster
#   - cluster_identifier = "${var.project_name}-cluster"
#   - database_name = "analytics"
#   - master_username = "admin"
#   - master_password = "Admin1234!" (use variables in production)
#   - node_type = "dc2.large"
#   - cluster_type = "single-node"
#   - skip_final_snapshot = true
#   - publicly_accessible = true (for lab access only)
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/redshift_cluster
# ============================================================


# ============================================================
# TODO 2: Create Tables with Different Distribution Styles
# ============================================================
# Connect with psql and create tables using EVEN, KEY, ALL,
# and AUTO distribution styles. See Solutions for full SQL.
#
#   - EVEN: round-robin across slices
#   - KEY: hash on DISTKEY column (co-locates join partners)
#   - ALL: full copy on every node (small dimension tables)
#   - AUTO: Redshift chooses (starts ALL, switches to EVEN)
#
#   Connect: psql -h <endpoint> -p 5439 -U admin -d analytics
#
# Docs: https://docs.aws.amazon.com/redshift/latest/dg/c_choosing_dist_sort.html
# ============================================================


# ============================================================
# TODO 3: Run Queries and Compare Execution Plans
# ============================================================
# Insert sample data, then use EXPLAIN to compare join plans.
# Look for DS_DIST_NONE (optimal) vs DS_BCAST_INNER or
# DS_DIST_BOTH (redistribution overhead).
#
# Docs: https://docs.aws.amazon.com/redshift/latest/dg/c_data_redistribution.html
# ============================================================
```

### `outputs.tf`

```hcl
output "cluster_endpoint" {
  value = "Set after TODO 1 implementation"
}

output "cluster_port" {
  value = 5439
}

output "database_name" {
  value = "analytics"
}
```

---

## Distribution Style Decision Framework

| Distribution Style | How It Works | Best For | Avoid When |
|---|---|---|---|
| **AUTO** | Redshift chooses (starts ALL, switches to EVEN as table grows) | Default choice, small-to-medium tables | You know the optimal style |
| **KEY** | Rows hashed by DISTKEY column, co-located on same slice | Large fact tables joined on a specific column | Key has high skew (90% of rows have same value) |
| **EVEN** | Round-robin across all slices | Tables not joined with others, load balancing | Frequent joins (causes broadcast/redistribute) |
| **ALL** | Full copy on every compute node | Small dimension tables (<5M rows) joined frequently | Large tables (wastes storage, slows writes) |

### Sort Key Comparison

| Sort Key Type | Behavior | Best For |
|---|---|---|
| **Compound** | Data sorted by columns in order (col1, then col2, etc.) | Queries that filter on leading columns in order |
| **Interleaved** | Equal weight to each column | Queries that filter on any column combination |
| **None** | No sort order | Tables with unpredictable access patterns |

---

## Spot the Bug

A data team has a 500-million-row fact table `orders` joined frequently with a 200-million-row `customers` table. They chose these distribution styles:

```sql
CREATE TABLE orders (
  order_id     BIGINT,
  customer_id  BIGINT,
  order_date   DATE,
  total_amount DECIMAL(12,2),
  status       VARCHAR(20)
) DISTSTYLE KEY DISTKEY (customer_id)
SORTKEY (order_date);

CREATE TABLE customers (
  customer_id  BIGINT,
  name         VARCHAR(100),
  email        VARCHAR(200),
  region       VARCHAR(50),
  signup_date  DATE
) DISTSTYLE EVEN;
```

Their most common query:
```sql
SELECT c.region, SUM(o.total_amount)
FROM orders o
JOIN customers c ON o.customer_id = c.customer_id
WHERE o.order_date > '2025-01-01'
GROUP BY c.region;
```

<details>
<summary>Explain the bug</summary>

**The `customers` table uses EVEN distribution while `orders` uses KEY distribution on `customer_id`.** When the join executes, Redshift must redistribute the `customers` table across compute nodes so that matching `customer_id` values are co-located with the `orders` data. With 200 million customer rows, this redistribution is a massive network operation that happens on every query execution.

The EXPLAIN plan will show `DS_DIST_BOTH` or `DS_BCAST_INNER`, indicating full data redistribution.

**Fix:** Use KEY distribution on `customer_id` for both tables so matching rows are always on the same compute node:

```sql
CREATE TABLE customers (
  customer_id  BIGINT,
  name         VARCHAR(100),
  email        VARCHAR(200),
  region       VARCHAR(50),
  signup_date  DATE
) DISTSTYLE KEY DISTKEY (customer_id);
```

With both tables using `DISTKEY (customer_id)`, the join is node-local (no redistribution). The EXPLAIN plan will show `DS_DIST_NONE` -- the optimal distribution. This is the most important Redshift performance optimization: co-locate frequently joined tables by using the same DISTKEY on the join column.

If the `customers` table were small (under 5 million rows), `DISTSTYLE ALL` would also work -- every node has a full copy, so the join is always local. But at 200 million rows, ALL distribution wastes storage and slows writes. KEY distribution is the correct choice for large tables.

</details>

---

## Verify What You Learned

1. **Deploy the cluster:**
   ```bash
   terraform init && terraform apply -auto-approve
   ```
   Note: Cluster creation takes 5-10 minutes.

2. **Connect to the cluster:**
   ```bash
   ENDPOINT=$(terraform output -raw cluster_endpoint)
   psql -h "$ENDPOINT" -p 5439 -U admin -d analytics
   # Enter password: Admin1234!
   ```

3. **Verify table distribution styles:**
   ```sql
   SELECT tablename, diststyle
   FROM svv_table_info
   WHERE schema = 'public'
   ORDER BY tablename;
   ```
   Expected: `events_even` = EVEN, `events_key` = KEY(user_id), `dim_event_types` = ALL, `events_auto` = AUTO(ALL) or AUTO(EVEN).

4. **Check sort keys:**
   ```sql
   SELECT tablename, sortkey1
   FROM svv_table_info
   WHERE schema = 'public' AND sortkey1 IS NOT NULL;
   ```
   Expected: `event_date` as sort key on events tables.

5. **Verify Terraform state:**
   ```bash
   terraform plan
   ```
   Expected: `No changes. Your infrastructure matches the configuration.`

---

## Solutions

<details>
<summary>TODO 1 -- Redshift Cluster (database.tf)</summary>

```hcl
resource "aws_redshift_cluster" "this" {
  cluster_identifier  = "${var.project_name}-cluster"
  database_name       = "analytics"
  master_username     = "admin"
  master_password     = "Admin1234!"
  node_type           = "dc2.large"
  cluster_type        = "single-node"
  skip_final_snapshot = true
  publicly_accessible = true

  tags = { Name = "${var.project_name}-cluster" }
}

output "cluster_endpoint" {
  value = aws_redshift_cluster.this.endpoint
}
```

A single-node cluster combines the leader and compute node. In production, use multi-node clusters (e.g., `cluster_type = "multi-node"` with `number_of_nodes = 4`) to distribute data across multiple compute nodes for parallel processing.

</details>

<details>
<summary>TODO 2 -- Create Tables (SQL)</summary>

```sql
CREATE TABLE events_even (event_id BIGINT, user_id BIGINT, event_type VARCHAR(50),
  event_date DATE, payload VARCHAR(4096)) DISTSTYLE EVEN SORTKEY (event_date);

CREATE TABLE events_key (event_id BIGINT, user_id BIGINT, event_type VARCHAR(50),
  event_date DATE, payload VARCHAR(4096)) DISTSTYLE KEY DISTKEY (user_id) SORTKEY (event_date);

CREATE TABLE dim_event_types (event_type VARCHAR(50), category VARCHAR(50),
  description VARCHAR(256)) DISTSTYLE ALL;

INSERT INTO dim_event_types VALUES ('click','engagement','Clicked'),('view','engagement','Viewed'),
  ('purchase','transaction','Purchased'),('signup','acquisition','Signed up');

CREATE TABLE events_auto (event_id BIGINT, user_id BIGINT, event_type VARCHAR(50),
  event_date DATE, payload VARCHAR(4096)) DISTSTYLE AUTO SORTKEY (event_date);
```

</details>

<details>
<summary>TODO 3 -- Query Plans (SQL)</summary>

```sql
-- Generate sample data
INSERT INTO events_key SELECT ROW_NUMBER() OVER (), (RANDOM()*10000)::BIGINT,
  CASE (RANDOM()*4)::INT WHEN 0 THEN 'click' WHEN 1 THEN 'view' WHEN 2 THEN 'purchase' ELSE 'signup' END,
  DATEADD(day,-(RANDOM()*365)::INT,CURRENT_DATE), 'payload' FROM stl_scan LIMIT 10000;

INSERT INTO events_even SELECT * FROM events_key;
INSERT INTO events_auto SELECT * FROM events_key;

-- Compare join plans — look for DS_DIST_NONE (optimal) vs DS_BCAST_INNER (expensive)
EXPLAIN SELECT e.event_id, d.category FROM events_key e
  JOIN dim_event_types d ON e.event_type = d.event_type WHERE e.event_date > '2025-01-01';

EXPLAIN SELECT e.event_id, d.category FROM events_even e
  JOIN dim_event_types d ON e.event_type = d.event_type WHERE e.event_date > '2025-01-01';
```

EXPLAIN output indicators: `DS_DIST_NONE` = optimal (no redistribution), `DS_DIST_ALL_NONE` = ALL table joined locally, `DS_BCAST_INNER` = inner table broadcast (expensive), `DS_DIST_BOTH` = both tables redistributed (most expensive).

</details>

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify:

```bash
aws redshift describe-clusters \
  --cluster-identifier saa-ex59-cluster 2>&1 || echo "Cluster deleted"
```

---

## What's Next

Exercise 60 covers **Amazon DocumentDB and MongoDB compatibility**. You will deploy a DocumentDB cluster (which uses Aurora-like architecture with 6 copies across 3 AZs) and learn when managed DocumentDB is preferable to self-managed MongoDB on EC2 -- a common exam trade-off question.

---

## Summary

- **Redshift architecture** separates the leader node (query coordination) from compute nodes (parallel data storage and execution) -- columnar storage enables scanning only needed columns
- **Distribution styles** control how data is physically placed across compute nodes -- the most impactful performance decision in Redshift schema design
- **KEY distribution** co-locates rows with the same DISTKEY value on the same node -- use it for large tables frequently joined on a specific column
- **ALL distribution** replicates the entire table to every node -- use it for small dimension tables (under 5M rows) that are joined frequently
- **EVEN distribution** spreads rows round-robin for balanced storage -- use it for tables not involved in joins
- **AUTO distribution** lets Redshift choose -- starts as ALL for small tables, switches to EVEN as tables grow
- **Sort keys** determine the physical order of data on disk -- compound sort keys optimize range queries on leading columns
- **Co-locating join columns** (same DISTKEY on both tables) eliminates inter-node data redistribution, the primary source of Redshift query latency
- **DS_DIST_NONE in EXPLAIN** confirms optimal distribution -- any other DS_DIST pattern indicates redistribution overhead
- **Redshift Spectrum** extends queries to S3 data without loading it -- useful for infrequent queries on cold data

## Reference

- [Redshift Distribution Styles](https://docs.aws.amazon.com/redshift/latest/dg/c_choosing_dist_sort.html)
- [Redshift Sort Keys](https://docs.aws.amazon.com/redshift/latest/dg/t_Sorting_data.html)
- [Redshift EXPLAIN Plans](https://docs.aws.amazon.com/redshift/latest/dg/r_EXPLAIN.html)
- [Terraform aws_redshift_cluster](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/redshift_cluster)

## Additional Resources

- [Redshift Best Practices for Table Design](https://docs.aws.amazon.com/redshift/latest/dg/c_designing-tables-best-practices.html) -- comprehensive guide to distribution, sort keys, and compression
- [Redshift System Tables for Monitoring](https://docs.aws.amazon.com/redshift/latest/dg/cm_chap_system-tables.html) -- STL and SVV views for query performance analysis
- [Redshift Advisor](https://docs.aws.amazon.com/redshift/latest/dg/advisor.html) -- automated recommendations for distribution and sort key changes
- [Redshift vs Athena Decision](https://docs.aws.amazon.com/decision-guides/latest/redshift-vs-athena/redshift-vs-athena.html) -- when to use Redshift (frequent, complex analytics) vs Athena (ad-hoc S3 queries)

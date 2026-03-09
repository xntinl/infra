# 95. Athena S3 Query Analysis

<!--
difficulty: basic
concepts: [athena, serverless-sql, s3-data-lake, glue-catalog, external-table, partitioning, parquet, csv, workgroup, query-results, cost-optimization]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: understand
prerequisites: [94-health-dashboard-trusted-advisor]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** Athena charges $5 per TB of data scanned. This exercise uses small sample data files (~1 KB), so query costs are negligible (minimum charge is 10 MB = $0.00005 per query). S3 storage for sample data and query results is ~$0.01/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 94 (Health Dashboard & Trusted Advisor) or equivalent knowledge
- Understanding of SQL (SELECT, WHERE, GROUP BY, JOIN)
- Familiarity with S3 bucket and object operations

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** how Athena executes serverless SQL queries directly against data stored in S3 without loading it into a database
- **Describe** the relationship between Athena, the Glue Data Catalog, and S3 data -- how Athena uses Glue as a metastore
- **Identify** how data format (CSV, JSON, Parquet, ORC) and partitioning affect Athena query cost and performance
- **Construct** an Athena workgroup, Glue catalog database, external table, and sample queries using Terraform
- **Distinguish** between full-scan queries (expensive) and partition-pruned queries (cost-effective) on large datasets
- **Compare** Athena with Redshift Spectrum, EMR, and RDS for different analytics workloads

## Why This Matters

Athena is the SAA-C03 exam's go-to answer for "query data in S3 without provisioning servers." It appears in scenarios involving log analysis (CloudTrail logs, ALB access logs, VPC Flow Logs), ad-hoc analytics on data lake files, and cost-effective querying for infrequent analysis. The key architectural insight is that Athena charges per query based on the amount of data scanned, not on compute time or provisioned capacity. This makes it extremely cost-effective for infrequent queries on large datasets but potentially expensive for frequent queries on unoptimized data.

The cost optimization question is where the exam separates good architects from great ones. Querying a 1 TB CSV file costs $5. The same data in Parquet format (columnar, compressed) scans only the columns you need -- a query selecting 3 columns out of 50 might scan only 60 GB, costing $0.30 instead of $5. Adding partitions by date means a query for "last week's data" skips 51 out of 52 weekly partitions, scanning only 1/52 of the data. Combining Parquet format with partitioning can reduce Athena costs by 90-99% on large datasets.

The exam tests your understanding of when Athena is the right tool versus Redshift, EMR, or RDS. Athena is best for ad-hoc queries, infrequent analysis, and data lake exploration. Redshift is better for complex analytics with frequent queries and dashboards. EMR is for big data processing (Spark, Hive) on massive datasets. The decision depends on query frequency, data volume, complexity, and latency requirements.

## Step 1 -- Create Athena Infrastructure

Create the following files in your exercise directory:

### `providers.tf`

```hcl
terraform {
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 5.0" }
  }
}

provider "aws" { region = var.region }
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
  default     = "saa-ex95"
}
```

### `storage.tf`

```hcl
resource "random_id" "suffix" { byte_length = 4 }

# Athena queries data directly from S3 -- no ETL or loading required
resource "aws_s3_bucket" "data" {
  bucket = "${var.project_name}-data-${random_id.suffix.hex}", force_destroy = true
  tags   = { Name = "${var.project_name}-data" }
}

resource "aws_s3_bucket_public_access_block" "data" {
  bucket = aws_s3_bucket.data.id
  block_public_acls = true, block_public_policy = true
  ignore_public_acls = true, restrict_public_buckets = true
}

# Every Athena query writes results to S3 (required)
resource "aws_s3_bucket" "results" {
  bucket = "${var.project_name}-results-${random_id.suffix.hex}", force_destroy = true
  tags   = { Name = "${var.project_name}-query-results" }
}

resource "aws_s3_bucket_public_access_block" "results" {
  bucket = aws_s3_bucket.results.id
  block_public_acls = true, block_public_policy = true
  ignore_public_acls = true, restrict_public_buckets = true
}

resource "aws_s3_object" "orders_csv" {
  bucket  = aws_s3_bucket.data.id
  key     = "orders/orders.csv"
  content = <<-CSV
    order_id,customer_id,product,quantity,price,order_date,region
    1001,C001,Widget-A,5,29.99,2025-01-15,us-east-1
    1002,C002,Widget-B,2,49.99,2025-01-15,us-west-2
    1003,C001,Widget-C,1,99.99,2025-01-16,us-east-1
    1004,C003,Widget-A,10,29.99,2025-01-16,eu-west-1
    1005,C002,Widget-B,3,49.99,2025-01-17,us-east-1
    1006,C004,Widget-D,1,199.99,2025-01-17,ap-southeast-1
    1007,C001,Widget-A,2,29.99,2025-01-18,us-east-1
    1008,C005,Widget-C,4,99.99,2025-01-18,us-west-2
    1009,C003,Widget-B,1,49.99,2025-01-19,eu-west-1
    1010,C002,Widget-A,7,29.99,2025-01-19,us-east-1
  CSV
}
```

### `analytics.tf`

```hcl
# Workgroup: controls results location, cost limits, encryption settings
resource "aws_athena_workgroup" "this" {
  name = "${var.project_name}-workgroup"
  configuration {
    result_configuration { output_location = "s3://${aws_s3_bucket.results.id}/athena-results/" }
    bytes_scanned_cutoff_per_query  = 1073741824  # 1 GB cost control limit
    enforce_workgroup_configuration = true
  }
  tags = { Name = "${var.project_name}-workgroup" }
}

# Glue Catalog Database: the metastore for Athena tables
resource "aws_glue_catalog_database" "this" { name = "saa_ex95_analytics" }

# Glue Catalog Table: schema + S3 location for Athena queries
# SerDe formats: CSV=OpenCSVSerde, JSON=JsonSerDe, Parquet=ParquetHiveSerDe
resource "aws_glue_catalog_table" "orders" {
  database_name = aws_glue_catalog_database.this.name
  name          = "orders"

  table_type = "EXTERNAL_TABLE"

  parameters = {
    "skip.header.line.count" = "1"
    "EXTERNAL"               = "TRUE"
  }

  storage_descriptor {
    location      = "s3://${aws_s3_bucket.data.id}/orders/"
    input_format  = "org.apache.hadoop.mapred.TextInputFormat"
    output_format = "org.apache.hadoop.hive.ql.io.HiveIgnoreKeyTextOutputFormat"

    ser_de_info {
      serialization_library = "org.apache.hadoop.hive.serde2.OpenCSVSerde"
      parameters = {
        "separatorChar" = ","
        "quoteChar"     = "\""
      }
    }

    columns {
      name = "order_id"
      type = "string"
    }
    columns {
      name = "customer_id"
      type = "string"
    }
    columns {
      name = "product"
      type = "string"
    }
    columns {
      name = "quantity"
      type = "string"
    }
    columns {
      name = "price"
      type = "string"
    }
    columns {
      name = "order_date"
      type = "string"
    }
    columns {
      name = "region"
      type = "string"
    }
  }
}
```

### `outputs.tf`

```hcl
output "data_bucket" {
  value = aws_s3_bucket.data.id
}

output "results_bucket" {
  value = aws_s3_bucket.results.id
}

output "workgroup" {
  value = aws_athena_workgroup.this.name
}

output "database" {
  value = aws_glue_catalog_database.this.name
}
```

```bash
terraform init
terraform apply -auto-approve
```

## Step 2 -- Run Athena Queries

```bash
WORKGROUP=$(terraform output -raw workgroup)
DATABASE=$(terraform output -raw database)

# Simple SELECT query
QUERY_ID=$(aws athena start-query-execution \
  --query-string "SELECT * FROM orders LIMIT 5" \
  --work-group "$WORKGROUP" \
  --query-execution-context Database="$DATABASE" \
  --query 'QueryExecutionId' --output text)

# Wait, then get results
aws athena get-query-execution --query-execution-id "$QUERY_ID" \
  --query 'QueryExecution.{Status:Status.State,DataScanned:Statistics.DataScannedInBytes}' \
  --output table

aws athena get-query-results --query-execution-id "$QUERY_ID" \
  --query 'ResultSet.Rows[*].Data[*].VarCharValue' --output table
```

## Step 3 -- Cost Optimization: Format and Partitioning Impact

### Data Format Cost Comparison

| Format | Compression | Column Pruning | 1 TB Dataset Query Cost | Best For |
|---|---|---|---|---|
| **CSV** | None | No (full scan) | $5.00 | Simple, human-readable |
| **JSON** | None | No (full scan) | $5.00 | Semi-structured data |
| **Gzip CSV** | ~70% | No | $1.50 | Compressed but full scan |
| **Parquet** | ~75% + columnar | Yes (scan only queried columns) | $0.25-0.50 | Analytics, data lakes |
| **ORC** | ~75% + columnar | Yes | $0.25-0.50 | Hive ecosystems |

### Partitioning Cost Impact

```
# Unpartitioned: query scans ALL data
# 1 TB of logs, query for "last 7 days" scans 1 TB = $5.00

# Partitioned by date: query scans only matching partitions
# 1 TB of logs / 365 days = ~2.74 GB per day
# Query for "last 7 days" scans ~19.2 GB = $0.10

# Partitioned by date AND region:
# Query for "last 7 days in us-east-1" scans ~4.8 GB = $0.02

# Cost reduction: 99.6% from $5.00 to $0.02
```

## Common Mistakes

### 1. Querying unpartitioned data (full scan, high cost)

**Wrong:** Flat S3 prefix `s3://data-lake/logs/log-*.csv`. Every query scans ALL files regardless of WHERE clause. **Fix:** Use Hive-style partitioning (`year=2025/month=01/day=01/`). Athena prunes partitions based on WHERE, scanning only matching data.

### 2. Using CSV instead of Parquet for analytics

**Wrong:** 100 GB CSV with 50 columns. Query selecting 3 columns scans all 100 GB ($0.50). **Fix:** Convert to Parquet via CTAS: `CREATE TABLE t WITH (format='PARQUET') AS SELECT * FROM t_csv`. Same query scans ~6 GB ($0.03) because Parquet is columnar.

### 3. Not setting query scan limits in the workgroup

**Wrong:** No `bytes_scanned_cutoff_per_query`. A developer runs `SELECT * FROM logs` on 10 TB = $50 per query. **Fix:** Set `bytes_scanned_cutoff_per_query = 1073741824` (1 GB limit). Queries exceeding the limit are cancelled automatically.

## Verify What You Learned

```bash
WORKGROUP=$(terraform output -raw workgroup)
DATABASE=$(terraform output -raw database)

# Verify workgroup exists with cost controls
aws athena get-work-group \
  --work-group "$WORKGROUP" \
  --query 'WorkGroup.Configuration.{BytesLimit:BytesScanCutoffPerQuery,EnforceConfig:EnforceWorkGroupConfiguration}' \
  --output table
```

Expected: BytesLimit = `1073741824`, EnforceConfig = `True`.

```bash
# Verify Glue database and table exist
aws glue get-tables \
  --database-name "$DATABASE" \
  --query 'TableList[*].{Name:Name,Type:TableType,Location:StorageDescriptor.Location}' \
  --output table
```

Expected: `orders` table of type `EXTERNAL_TABLE`.

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
terraform state list
```

Expected: no output (empty state).

## What's Next

Exercise 96 covers **Kinesis Data Streams vs Firehose**, where you will deploy both services and compare real-time streaming (Kinesis Data Streams with custom consumers) against near-real-time delivery (Kinesis Data Firehose with automatic S3/Redshift delivery) -- building on the S3 data lake foundation from this exercise.

## Summary

- **Athena** is a serverless SQL query engine that reads data directly from S3 without loading or provisioning
- **Pricing**: $5 per TB of data scanned per query -- data format and partitioning directly control cost
- **Glue Data Catalog** serves as the metastore -- tables define schema and point to S3 locations
- **Parquet/ORC** (columnar formats) reduce scan volume by 80-95% compared to CSV/JSON for typical analytical queries
- **Partitioning** (Hive-style: `year=2025/month=01/`) enables partition pruning, scanning only relevant data
- **Workgroup scan limits** (`bytes_scanned_cutoff_per_query`) prevent accidental expensive queries
- **CTAS queries** (Create Table As Select) convert data from CSV to Parquet without external ETL tools
- **Athena vs Redshift**: Athena for ad-hoc/infrequent queries; Redshift for frequent/complex analytics with dashboards
- **Athena vs EMR**: Athena for SQL queries; EMR for Spark/Hive big data processing jobs
- **Minimum charge** is 10 MB per query regardless of actual data size

## Reference

- [Amazon Athena User Guide](https://docs.aws.amazon.com/athena/latest/ug/what-is.html)
- [Athena Pricing](https://aws.amazon.com/athena/pricing/)
- [Terraform aws_athena_workgroup](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/athena_workgroup)
- [Terraform aws_glue_catalog_table](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/glue_catalog_table)

## Additional Resources

- [Top 10 Performance Tuning Tips for Athena](https://aws.amazon.com/blogs/big-data/top-10-performance-tuning-tips-for-amazon-athena/) -- partition pruning, columnar formats, and compression strategies
- [Querying CloudTrail Logs with Athena](https://docs.aws.amazon.com/athena/latest/ug/cloudtrail-logs.html) -- analyzing API audit trails with SQL
- [Querying ALB Access Logs with Athena](https://docs.aws.amazon.com/athena/latest/ug/application-load-balancer-logs.html) -- analyzing load balancer traffic patterns
- [CTAS for Data Format Conversion](https://docs.aws.amazon.com/athena/latest/ug/ctas.html) -- converting CSV/JSON to Parquet using Athena itself

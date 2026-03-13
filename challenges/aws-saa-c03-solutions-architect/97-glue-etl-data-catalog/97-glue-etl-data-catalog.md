# 97. AWS Glue ETL and Data Catalog

<!--
difficulty: intermediate
concepts: [glue-data-catalog, glue-crawler, glue-etl-job, pyspark, schema-discovery, csv-to-parquet, data-lake, catalog-database, catalog-table, classifiers]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: apply, analyze
prerequisites: [96-kinesis-streams-vs-firehose]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** Glue Crawlers cost $0.44/DPU-hour (minimum 2 DPUs for ~10 seconds on small data = negligible). Glue ETL jobs cost $0.44/DPU-hour (minimum 2 DPUs). Glue Data Catalog: first 1 million objects free. For this exercise with small sample data, total cost is ~$0.01/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 96 (Kinesis Streams vs Firehose) or equivalent knowledge
- Understanding of Athena and Glue Data Catalog from exercise 95
- Basic familiarity with ETL concepts (extract, transform, load)

## Learning Objectives

After completing this exercise, you will be able to:

1. **Implement** Glue crawlers that auto-discover schema from S3 data and populate the Glue Data Catalog
2. **Analyze** how the Glue Data Catalog serves as a centralized metastore for Athena, Redshift Spectrum, and EMR
3. **Evaluate** the trade-offs between Glue crawlers (automatic schema discovery) and manual catalog table definitions
4. **Apply** Glue ETL jobs to transform data formats (CSV to Parquet) for cost-effective Athena queries
5. **Design** a data lake architecture using Glue as the metadata and ETL backbone

## Why This Matters

AWS Glue is the central data management service in the AWS analytics ecosystem, and the SAA-C03 exam tests three distinct components. The **Data Catalog** is a Hive-compatible metastore that Athena, Redshift Spectrum, and EMR all use to find table schemas and data locations. **Crawlers** automatically discover schema from S3 data -- you point a crawler at an S3 prefix, it reads the files, infers the schema, and creates or updates catalog tables. **ETL Jobs** run PySpark or Python scripts that transform data between formats, clean and deduplicate records, and move data between stores.

The exam tests architectural decisions around Glue. "How do you make S3 data queryable by Athena without manual schema definitions?" -- Glue Crawler. "How do you convert CSV data to Parquet for cost-effective Athena queries?" -- Glue ETL job. "How do you maintain a schema registry that multiple analytics services share?" -- Glue Data Catalog.

The crawler re-run behavior is an important exam topic. By default, crawlers update existing table schemas when they re-run. If you have manually curated a schema (added column descriptions, changed data types), a crawler re-run can overwrite your changes. Understanding crawler configuration options -- schema change policy, update behavior, and recrawl policy -- is essential for production data lakes.

## Step 1 -- Create the Data Lake Foundation

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
  default     = "saa-ex97"
}
```

### `storage.tf`

```hcl
resource "random_id" "suffix" { byte_length = 4 }

# Two-bucket pattern: raw (CSV) and processed (Parquet)
resource "aws_s3_bucket" "raw" {
  bucket = "${var.project_name}-raw-${random_id.suffix.hex}", force_destroy = true
  tags = { Name = "${var.project_name}-raw-data" }
}

resource "aws_s3_bucket_public_access_block" "raw" {
  bucket = aws_s3_bucket.raw.id
  block_public_acls = true, block_public_policy = true
  ignore_public_acls = true, restrict_public_buckets = true
}

resource "aws_s3_bucket" "processed" {
  bucket = "${var.project_name}-processed-${random_id.suffix.hex}", force_destroy = true
  tags = { Name = "${var.project_name}-processed-data" }
}

resource "aws_s3_bucket_public_access_block" "processed" {
  bucket = aws_s3_bucket.processed.id
  block_public_acls = true, block_public_policy = true
  ignore_public_acls = true, restrict_public_buckets = true
}

# ------------------------------------------------------------------
# Upload sample CSV data to the raw bucket.
# ------------------------------------------------------------------
resource "aws_s3_object" "sales_csv" {
  bucket  = aws_s3_bucket.raw.id
  key     = "sales/sales_data.csv"
  content = <<-CSV
    transaction_id,product_name,category,quantity,unit_price,sale_date,store_region
    T001,Laptop Pro,Electronics,2,1299.99,2025-01-15,us-east
    T002,Wireless Mouse,Electronics,5,29.99,2025-01-15,us-west
    T003,Office Chair,Furniture,1,449.99,2025-01-16,us-east
    T004,Standing Desk,Furniture,1,799.99,2025-01-16,eu-west
    T005,Monitor 27in,Electronics,3,399.99,2025-01-17,us-east
    T006,Keyboard,Electronics,10,79.99,2025-01-17,ap-east
    T007,Desk Lamp,Office,4,39.99,2025-01-18,us-west
    T008,Laptop Pro,Electronics,1,1299.99,2025-01-18,eu-west
    T009,Webcam HD,Electronics,6,69.99,2025-01-19,us-east
    T010,Filing Cabinet,Furniture,2,199.99,2025-01-19,us-west
  CSV
}
```

### `glue.tf`

```hcl
# ------------------------------------------------------------------
# Glue Data Catalog database.
# This database contains tables discovered by crawlers and
# is used by Athena, Redshift Spectrum, and EMR for queries.
# ------------------------------------------------------------------
resource "aws_glue_catalog_database" "this" {
  name = "saa_ex97_data_lake"
}
```

## Step 2 -- Create the Glue IAM Role

### `iam.tf`

```hcl
data "aws_iam_policy_document" "glue_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals { type = "Service", identifiers = ["glue.amazonaws.com"] }
  }
}

resource "aws_iam_role" "glue" {
  name               = "${var.project_name}-glue-role"
  assume_role_policy = data.aws_iam_policy_document.glue_assume.json
  tags = { Name = "${var.project_name}-glue-role" }
}

resource "aws_iam_role_policy_attachment" "glue_service" {
  role       = aws_iam_role.glue.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSGlueServiceRole"
}

data "aws_iam_policy_document" "glue_s3" {
  statement {
    actions   = ["s3:GetBucketLocation", "s3:ListBucket", "s3:GetObject", "s3:PutObject", "s3:DeleteObject"]
    resources = [aws_s3_bucket.raw.arn, "${aws_s3_bucket.raw.arn}/*", aws_s3_bucket.processed.arn, "${aws_s3_bucket.processed.arn}/*"]
  }
}

resource "aws_iam_role_policy" "glue_s3" {
  name = "s3-access", role = aws_iam_role.glue.id
  policy = data.aws_iam_policy_document.glue_s3.json
}
```

## Step 3 -- Configure the Glue Crawler

### TODO 1: Create the Glue Crawler

Add the following to `glue.tf`:

```hcl
# Glue Crawler: scans S3 data, infers schema, creates/updates catalog tables.
# schema_change_policy: UPDATE_IN_DATABASE (update), LOG (no modify), DELETE_FROM_DATABASE (remove)

# TODO: Create aws_glue_crawler "${var.project_name}-sales-crawler"
# - role: Glue IAM role ARN, database_name: catalog database
# - s3_target: path "s3://{raw_bucket}/sales/"
# - schema_change_policy: update_behavior="UPDATE_IN_DATABASE", delete_behavior="LOG"
# - recrawl_policy: recrawl_behavior="CRAWL_EVERYTHING"
# - configuration: JSON with Version=1.0, CrawlerOutput.Partitions.AddOrUpdateBehavior="InheritFromTable"
```

<details>
<summary>glue.tf -- Solution: Glue Crawler</summary>

```hcl
resource "aws_glue_crawler" "sales" {
  name          = "${var.project_name}-sales-crawler"
  role          = aws_iam_role.glue.arn
  database_name = aws_glue_catalog_database.this.name

  s3_target {
    path = "s3://${aws_s3_bucket.raw.id}/sales/"
  }

  schema_change_policy {
    update_behavior = "UPDATE_IN_DATABASE"
    delete_behavior = "LOG"
  }

  recrawl_policy {
    recrawl_behavior = "CRAWL_EVERYTHING"
  }

  configuration = jsonencode({
    Version = 1.0
    CrawlerOutput = {
      Partitions = {
        AddOrUpdateBehavior = "InheritFromTable"
      }
    }
  })

  tags = { Name = "${var.project_name}-sales-crawler" }
}
```

</details>

## Step 4 -- Configure the Glue ETL Job

### TODO 2: Create the ETL Job Script and Job

Add the following to `glue.tf`:

```hcl
# PySpark ETL script stored in S3, referenced by the Glue job
resource "aws_s3_object" "etl_script" {
  bucket = aws_s3_bucket.processed.id
  key    = "scripts/csv_to_parquet.py"
  content = <<-PYTHON
    import sys
    from awsglue.transforms import *
    from awsglue.utils import getResolvedOptions
    from pyspark.context import SparkContext
    from awsglue.context import GlueContext
    from awsglue.job import Job

    args = getResolvedOptions(sys.argv, ['JOB_NAME', 'SOURCE_DATABASE', 'SOURCE_TABLE', 'OUTPUT_PATH'])
    sc = SparkContext()
    glueContext = GlueContext(sc)
    job = Job(glueContext)
    job.init(args['JOB_NAME'], args)

    datasource = glueContext.create_dynamic_frame.from_catalog(
        database=args['SOURCE_DATABASE'], table_name=args['SOURCE_TABLE'], transformation_ctx="datasource")

    mapped = ApplyMapping.apply(frame=datasource, mappings=[
        ("transaction_id", "string", "transaction_id", "string"),
        ("product_name", "string", "product_name", "string"),
        ("category", "string", "category", "string"),
        ("quantity", "string", "quantity", "int"),
        ("unit_price", "string", "unit_price", "double"),
        ("sale_date", "string", "sale_date", "string"),
        ("store_region", "string", "store_region", "string"),
    ], transformation_ctx="mapped")

    glueContext.write_dynamic_frame.from_options(
        frame=mapped, connection_type="s3",
        connection_options={"path": args['OUTPUT_PATH'], "partitionKeys": ["category"]},
        format="parquet", transformation_ctx="output")
    job.commit()
  PYTHON
}

# TODO: Create aws_glue_job "${var.project_name}-csv-to-parquet"
# - role_arn: Glue IAM role, glue_version: "4.0", worker_type: "G.1X", number_of_workers: 2
# - command: script_location="s3://{processed_bucket}/scripts/csv_to_parquet.py", python_version="3"
# - default_arguments: --SOURCE_DATABASE, --SOURCE_TABLE="sales", --OUTPUT_PATH, --job-bookmark-option="job-bookmark-enable"
```

<details>
<summary>glue.tf -- Solution: Glue ETL Job</summary>

```hcl
resource "aws_glue_job" "csv_to_parquet" {
  name     = "${var.project_name}-csv-to-parquet"
  role_arn = aws_iam_role.glue.arn

  command {
    script_location = "s3://${aws_s3_bucket.processed.id}/scripts/csv_to_parquet.py"
    python_version  = "3"
  }

  glue_version      = "4.0"
  number_of_workers = 2
  worker_type       = "G.1X"

  default_arguments = {
    "--SOURCE_DATABASE"       = aws_glue_catalog_database.this.name
    "--SOURCE_TABLE"          = "sales"
    "--OUTPUT_PATH"           = "s3://${aws_s3_bucket.processed.id}/sales-parquet/"
    "--job-bookmark-option"   = "job-bookmark-enable"
    "--enable-metrics"        = "true"
  }

  tags = { Name = "${var.project_name}-csv-to-parquet" }
}
```

</details>

## Step 5 -- Add Outputs and Apply

### `outputs.tf`

```hcl
output "raw_bucket" {
  value = aws_s3_bucket.raw.id
}

output "processed_bucket" {
  value = aws_s3_bucket.processed.id
}

output "database" {
  value = aws_glue_catalog_database.this.name
}

output "crawler_name" {
  value = aws_glue_crawler.sales.name
}

output "etl_job_name" {
  value = aws_glue_job.csv_to_parquet.name
}
```

```bash
terraform init
terraform apply -auto-approve
```

## Step 6 -- Run the Crawler and ETL Job

```bash
CRAWLER=$(terraform output -raw crawler_name)

# Run the crawler to discover the schema
aws glue start-crawler --name "$CRAWLER"

# Check crawler status (wait for READY)
aws glue get-crawler --name "$CRAWLER" \
  --query 'Crawler.{Name:Name,State:State,LastCrawl:LastCrawl.Status}' \
  --output table
```

Wait for the crawler to complete (~30-60 seconds):

```bash
DATABASE=$(terraform output -raw database)

# Verify the crawler created a table
aws glue get-tables --database-name "$DATABASE" \
  --query 'TableList[*].{Name:Name,Columns:StorageDescriptor.Columns[*].Name}' \
  --output json
```

```bash
ETL_JOB=$(terraform output -raw etl_job_name)

# Run the ETL job (converts CSV to Parquet)
aws glue start-job-run --job-name "$ETL_JOB" \
  --query 'JobRunId' --output text

# Check job status
aws glue get-job-runs --job-name "$ETL_JOB" \
  --query 'JobRuns[0].{Id:Id,State:JobRunState,StartedOn:StartedOn}' \
  --output table
```

## Spot the Bug

A data engineering team sets up a Glue crawler on a schedule but discovers that their manually curated table schema keeps getting overwritten:

```hcl
resource "aws_glue_crawler" "production" {
  name          = "prod-data-crawler"
  role          = aws_iam_role.glue.arn
  database_name = "production_catalog"
  schedule      = "cron(0 */6 * * ? *)"  # Every 6 hours
  s3_target { path = "s3://production-data/events/" }
  schema_change_policy {
    update_behavior = "UPDATE_IN_DATABASE"
    delete_behavior = "DELETE_FROM_DATABASE"
  }
}
```

After each crawl: (1) column descriptions are removed, (2) data types revert to `string`, (3) tables for temporarily empty S3 paths are deleted.

<details>
<summary>Explain the bug</summary>

**Bug 1: `update_behavior = "UPDATE_IN_DATABASE"` overwrites manual schema changes.** The crawler re-infers schema from raw data each run, replacing any manual edits. **Fix:** Use `update_behavior = "LOG"` for curated tables -- review changes in CloudWatch Logs and apply manually.

**Bug 2: `delete_behavior = "DELETE_FROM_DATABASE"` removes tables for empty paths.** If an S3 path is temporarily empty (batch job not yet written), the crawler deletes the table, breaking downstream queries. **Fix:** Use `delete_behavior = "LOG"`.

**Bug 3: No `recrawl_policy`.** Default is `CRAWL_EVERYTHING`, re-processing all data each run. For incremental data, use `CRAWL_NEW_FOLDERS_ONLY`.

**Best practice:** Use crawlers for initial discovery only. Maintain schemas via Terraform or Glue API in production.

</details>

## Verify What You Learned

```bash
CRAWLER=$(terraform output -raw crawler_name)
DATABASE=$(terraform output -raw database)

# Verify crawler exists and is configured
aws glue get-crawler --name "$CRAWLER" \
  --query 'Crawler.{Name:Name,State:State,DatabaseName:DatabaseName,SchemaPolicy:SchemaChangePolicy}' \
  --output json
```

Expected: Crawler with `UPDATE_IN_DATABASE` behavior and target database.

```bash
# Verify catalog table was created
aws glue get-tables --database-name "$DATABASE" \
  --query 'TableList[*].Name' --output text
```

Expected: `sales` table in the database.

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

Exercise 98 covers **Lake Formation Access Control**, where you will apply fine-grained permissions (column-level, row-level, cell-level) on top of the Glue Data Catalog you learned about here -- moving from the data engineering layer to the governance and security layer of the data lake.

## Summary

- **Glue Data Catalog** is a centralized Hive-compatible metastore used by Athena, Redshift Spectrum, EMR, and Glue ETL
- **Crawlers** auto-discover schema from S3 data, creating catalog tables without manual DDL; re-runs with `UPDATE_IN_DATABASE` overwrite manual changes (use `LOG` for curated tables)
- **ETL Jobs** run PySpark or Python scripts on managed Spark infrastructure to transform data between formats
- **CSV to Parquet** conversion via Glue ETL reduces Athena query costs by 80-95% through columnar storage and compression
- **Job bookmarks** enable incremental ETL by tracking which data has already been processed
- **Glue version 4.0** uses Spark 3.3 and supports Python 3.10 with improved performance
- **Worker types**: G.1X (16 GB RAM) for standard ETL; G.2X (32 GB) for memory-intensive jobs; G.025X for lightweight jobs
- **Cost model**: Crawlers and ETL jobs charge per DPU-hour ($0.44); Data Catalog charges per object stored (first 1M free)
- **Best practice**: Use crawlers for initial discovery, then maintain schemas via Terraform or API for production

## Reference

- [AWS Glue Developer Guide](https://docs.aws.amazon.com/glue/latest/dg/what-is-glue.html)
- [Glue Crawlers](https://docs.aws.amazon.com/glue/latest/dg/add-crawler.html)
- [Terraform aws_glue_crawler](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/glue_crawler)
- [Terraform aws_glue_job](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/glue_job)

## Additional Resources

- [Glue ETL Programming Guide](https://docs.aws.amazon.com/glue/latest/dg/aws-glue-programming.html) -- PySpark and Python shell ETL development
- [Glue Schema Registry](https://docs.aws.amazon.com/glue/latest/dg/schema-registry.html) -- versioning schemas for streaming data compatibility
- [Glue Job Bookmarks](https://docs.aws.amazon.com/glue/latest/dg/monitor-continuations.html) -- incremental processing to avoid reprocessing old data
- [Glue Data Quality](https://docs.aws.amazon.com/glue/latest/dg/glue-data-quality.html) -- built-in data quality rules that validate data during ETL

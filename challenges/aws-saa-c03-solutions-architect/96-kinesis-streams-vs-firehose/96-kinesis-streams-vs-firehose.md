# 96. Kinesis Data Streams vs Kinesis Data Firehose

<!--
difficulty: intermediate
concepts: [kinesis-data-streams, kinesis-data-firehose, shards, partition-key, consumers, enhanced-fan-out, delivery-stream, buffer-interval, buffer-size, real-time-vs-near-real-time]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: apply, analyze
prerequisites: [95-athena-s3-query-analysis]
aws_cost: ~$0.02/hr
-->

> **AWS Cost Warning:** Kinesis Data Streams: $0.015/shard/hr (1 shard = ~$0.36/day). Kinesis Data Firehose: $0.029/GB ingested (first 500 GB/month). For this exercise with minimal data, combined cost is ~$0.02/hr. Kinesis Data Streams charges continue as long as the stream exists. Remember to run `terraform destroy` immediately when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 95 (Athena S3 Query Analysis) or equivalent knowledge
- Understanding of streaming data concepts (producers, consumers, partitions)
- Familiarity with S3 and IAM roles

## Learning Objectives

After completing this exercise, you will be able to:

1. **Implement** both Kinesis Data Streams and Kinesis Data Firehose and understand their distinct operational models
2. **Analyze** the architectural trade-offs between real-time processing (Data Streams) and near-real-time delivery (Firehose)
3. **Evaluate** when to use each service based on latency requirements, consumer complexity, and operational overhead
4. **Apply** shard capacity planning for Data Streams and buffer configuration for Firehose
5. **Design** streaming architectures that combine both services for different stages of a data pipeline

## Why This Matters

The SAA-C03 exam frequently tests your ability to choose between Kinesis Data Streams and Kinesis Data Firehose, and the distinction is nuanced. Data Streams is a real-time streaming platform where you manage shards, write custom consumer applications, and control exactly how data is processed. Firehose is a near-real-time delivery service that automatically batches, compresses, and delivers data to destinations (S3, Redshift, OpenSearch, Splunk) with zero consumer code.

The key decision factor is not just latency -- it is operational complexity versus flexibility. Data Streams gives you sub-second latency with custom processing logic, but you must manage shard capacity, write and maintain consumer code, and handle checkpointing. Firehose gives you automatic scaling, zero consumer management, and built-in transformation, but with a minimum delivery latency of 60 seconds (buffer interval).

The exam presents scenarios like "real-time fraud detection requiring sub-second processing" (Data Streams + Lambda/KCL consumer) versus "deliver clickstream data to S3 for analytics" (Firehose with Parquet conversion). Understanding the buffer interval configuration is critical -- a Firehose buffer of 900 seconds (15 minutes) for a "real-time dashboard" is a classic bug the exam tests.

## Step 1 -- Create Kinesis Data Stream

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
  default     = "saa-ex96"
}
```

### `main.tf`

```hcl
resource "random_id" "suffix" { byte_length = 4 }

# Kinesis Data Stream: real-time streaming with shard-based capacity.
# Each shard: 1 MB/s write (1,000 records/s), 2 MB/s read.
# Retention: 24h (default) to 365 days. Consumers: KCL, Lambda, SDK.
resource "aws_kinesis_stream" "this" {
  name = "${var.project_name}-data-stream"
  stream_mode_details { stream_mode = "PROVISIONED" }
  shard_count      = 1
  retention_period = 24
  tags = { Name = "${var.project_name}-data-stream" }
}
```

## Step 2 -- Create Kinesis Data Firehose

### `storage.tf`

```hcl
resource "aws_s3_bucket" "firehose_dest" {
  bucket = "${var.project_name}-firehose-${random_id.suffix.hex}", force_destroy = true
  tags   = { Name = "${var.project_name}-firehose-destination" }
}

resource "aws_s3_bucket_public_access_block" "firehose_dest" {
  bucket = aws_s3_bucket.firehose_dest.id
  block_public_acls = true, block_public_policy = true
  ignore_public_acls = true, restrict_public_buckets = true
}
```

### `iam.tf`

```hcl
data "aws_iam_policy_document" "firehose_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals { type = "Service", identifiers = ["firehose.amazonaws.com"] }
  }
}

resource "aws_iam_role" "firehose" {
  name               = "${var.project_name}-firehose-role"
  assume_role_policy = data.aws_iam_policy_document.firehose_assume.json
  tags = { Name = "${var.project_name}-firehose-role" }
}

data "aws_iam_policy_document" "firehose_s3" {
  statement {
    actions   = ["s3:AbortMultipartUpload", "s3:GetBucketLocation", "s3:GetObject", "s3:ListBucket", "s3:ListBucketMultipartUploads", "s3:PutObject"]
    resources = [aws_s3_bucket.firehose_dest.arn, "${aws_s3_bucket.firehose_dest.arn}/*"]
  }
}

resource "aws_iam_role_policy" "firehose_s3" {
  name = "s3-delivery", role = aws_iam_role.firehose.id
  policy = data.aws_iam_policy_document.firehose_s3.json
}
```

### TODO 1: Create the Firehose Delivery Stream

Add the following to `firehose.tf`:

```hcl
# ------------------------------------------------------------------
# Kinesis Data Firehose Delivery Stream:
# Automatically batches and delivers data to S3.
#
# Buffer configuration controls delivery latency:
#   - buffer_size: deliver when buffer reaches N MB (1-128 MB)
#   - buffer_interval: deliver every N seconds (60-900 seconds)
# Whichever threshold is reached first triggers delivery.
#
# For near-real-time: buffer_size=1, buffer_interval=60 (minimum)
# For cost-efficient: buffer_size=128, buffer_interval=900
# ------------------------------------------------------------------

# TODO: Create the Firehose delivery stream
# Resource type: aws_kinesis_firehose_delivery_stream
# - name: "${var.project_name}-delivery-stream"
# - destination: "extended_s3"
#
# extended_s3_configuration block:
#   - role_arn: reference the Firehose IAM role
#   - bucket_arn: reference the S3 destination bucket
#   - prefix: "data/year=!{timestamp:yyyy}/month=!{timestamp:MM}/day=!{timestamp:dd}/hour=!{timestamp:HH}/"
#   - error_output_prefix: "errors/!{firehose:error-output-type}/"
#   - buffering_size: 5 (MB)
#   - buffering_interval: 60 (seconds -- minimum for near-real-time)
#   - compression_format: "GZIP"
```

<details>
<summary>firehose.tf -- Solution: Firehose Delivery Stream</summary>

```hcl
resource "aws_kinesis_firehose_delivery_stream" "this" {
  name        = "${var.project_name}-delivery-stream"
  destination = "extended_s3"

  extended_s3_configuration {
    role_arn   = aws_iam_role.firehose.arn
    bucket_arn = aws_s3_bucket.firehose_dest.arn

    prefix              = "data/year=!{timestamp:yyyy}/month=!{timestamp:MM}/day=!{timestamp:dd}/hour=!{timestamp:HH}/"
    error_output_prefix = "errors/!{firehose:error-output-type}/"

    buffering_size     = 5    # MB
    buffering_interval = 60   # seconds (minimum)

    compression_format = "GZIP"
  }

  tags = { Name = "${var.project_name}-delivery-stream" }
}
```

</details>

## Step 3 -- Add Outputs and Apply

### `outputs.tf`

```hcl
output "stream_name" {
  value = aws_kinesis_stream.this.name
}

output "firehose_name" {
  value = aws_kinesis_firehose_delivery_stream.this.name
}

output "firehose_bucket" {
  value = aws_s3_bucket.firehose_dest.id
}
```

```bash
terraform init
terraform apply -auto-approve
```

## Step 4 -- Produce and Consume Data

```bash
STREAM=$(terraform output -raw stream_name)
FIREHOSE=$(terraform output -raw firehose_name)

# ------------------------------------------------------------------
# Produce records to Kinesis Data Stream
# ------------------------------------------------------------------
for i in $(seq 1 10); do
  aws kinesis put-record \
    --stream-name "$STREAM" \
    --data "$(echo -n "{\"sensor_id\": \"s-$i\", \"temperature\": $((20 + RANDOM % 15)), \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"}" | base64)" \
    --partition-key "sensor-$((i % 3))" \
    --query '{ShardId:ShardId,SequenceNumber:SequenceNumber}' \
    --output text
done

echo "Produced 10 records to Data Stream"

# ------------------------------------------------------------------
# Produce records to Firehose
# ------------------------------------------------------------------
for i in $(seq 1 10); do
  aws firehose put-record \
    --delivery-stream-name "$FIREHOSE" \
    --record "Data=$(echo -n "{\"sensor_id\": \"s-$i\", \"temperature\": $((20 + RANDOM % 15)), \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"}\n" | base64)" \
    --query 'RecordId' --output text
done

echo "Produced 10 records to Firehose"
```

```bash
# ------------------------------------------------------------------
# Consume from Kinesis Data Stream (manual consumer using CLI)
# This demonstrates the consumer model -- in production, you would
# use KCL (Kinesis Client Library), Lambda, or the SDK.
# ------------------------------------------------------------------
STREAM=$(terraform output -raw stream_name)

# Get shard iterator
SHARD_ITERATOR=$(aws kinesis get-shard-iterator \
  --stream-name "$STREAM" \
  --shard-id "shardId-000000000000" \
  --shard-iterator-type "TRIM_HORIZON" \
  --query 'ShardIterator' --output text)

# Read records from the stream
aws kinesis get-records \
  --shard-iterator "$SHARD_ITERATOR" \
  --limit 10 \
  --query 'Records[*].Data' --output text | \
  while read -r data; do
    echo "$data" | base64 -d
    echo ""
  done
```

```bash
# Check Firehose delivery (wait ~60 seconds for buffer flush)
echo "Waiting 60 seconds for Firehose buffer to flush..."
sleep 65

BUCKET=$(terraform output -raw firehose_bucket)
aws s3 ls "s3://$BUCKET/data/" --recursive
```

## Step 5 -- Comparison and Decision Framework

### TODO 2: Complete the Decision Table

```
# TODO: Fill in the recommended service for each scenario
#
# +---------------------------------------------+-----------+
# | Scenario                                    | Service   |
# +---------------------------------------------+-----------+
# | Real-time fraud detection (<100ms latency)  | ???       |
# | Clickstream → S3 for daily Athena analysis  | ???       |
# | IoT sensor data → OpenSearch dashboards     | ???       |
# | Log aggregation → S3 (Parquet) for Athena   | ???       |
# | Real-time leaderboard (custom processing)   | ???       |
# | CloudWatch Logs → S3 archival               | ???       |
# | Stock ticker processing with 5 consumers    | ???       |
# | Application logs → Splunk                   | ???       |
# +---------------------------------------------+-----------+
```

<details>
<summary>Solution: Decision Table</summary>

| Scenario | Service | Reasoning |
|---|---|---|
| Real-time fraud detection (<100ms latency) | **Data Streams** | Sub-second latency with Lambda or KCL consumer for custom logic |
| Clickstream to S3 for daily analysis | **Firehose** | No real-time processing needed; Firehose auto-delivers to S3 with partitioning |
| IoT sensor data to OpenSearch dashboards | **Firehose** | Direct delivery to OpenSearch; near-real-time sufficient for dashboards |
| Log aggregation to S3 (Parquet) for Athena | **Firehose** | Built-in Parquet conversion, auto-partitioning, GZIP compression |
| Real-time leaderboard (custom processing) | **Data Streams** | Custom consumer logic needed; real-time updates required |
| CloudWatch Logs to S3 archival | **Firehose** | CloudWatch Logs subscription filter directly to Firehose; no processing needed |
| Stock ticker processing with 5 consumers | **Data Streams** | Multiple consumers (Enhanced Fan-Out provides dedicated 2 MB/s per consumer) |
| Application logs to Splunk | **Firehose** | Direct Splunk delivery endpoint; no custom consumer code needed |

**Rule of thumb:**
- Need custom processing or sub-second latency? **Data Streams**
- Need managed delivery to a destination? **Firehose**
- Need both? **Data Streams + Firehose** (Firehose as a Data Streams consumer)

</details>

### Service Comparison

| Feature | Kinesis Data Streams | Kinesis Data Firehose |
|---|---|---|
| **Latency** | Real-time (~200ms) | Near-real-time (60-900s) |
| **Scaling** | Manual (shards) or On-Demand | Automatic |
| **Consumers** | Custom code (KCL, Lambda, SDK) | Managed (no code) |
| **Destinations** | Any (you build it) | S3, Redshift, OpenSearch, Splunk, HTTP |
| **Data retention** | 24 hours - 365 days | No retention (delivery only) |
| **Replay** | Yes (re-read from any position) | No (fire-and-forget) |
| **Transformation** | Consumer-side | Built-in Lambda transform |
| **Format conversion** | Consumer-side | Built-in Parquet/ORC |
| **Pricing** | Per shard/hour + per GB | Per GB ingested |
| **Management** | Higher (shards, consumers) | Lower (fully managed) |

## Spot the Bug

A team builds a "real-time" analytics dashboard that shows user activity within 1 minute. They use Kinesis Data Firehose:

```hcl
resource "aws_kinesis_firehose_delivery_stream" "realtime" {
  name        = "realtime-analytics"
  destination = "extended_s3"

  extended_s3_configuration {
    role_arn   = aws_iam_role.firehose.arn
    bucket_arn = aws_s3_bucket.analytics.arn

    buffering_size     = 128   # Maximum buffer size
    buffering_interval = 900   # 15 minutes!

    compression_format = "GZIP"
  }
}
```

The dashboard shows data that is 15+ minutes old. Users complain it is not "real-time."

<details>
<summary>Explain the bug</summary>

**Bug: The Firehose buffer interval is set to 900 seconds (15 minutes), making it impossible to meet the 1-minute requirement.**

Firehose delivers data when **either** threshold is reached first:
- `buffering_size = 128` MB -- deliver when buffer reaches 128 MB
- `buffering_interval = 900` seconds -- deliver every 15 minutes

For a low-volume stream that does not produce 128 MB in 15 minutes, data sits in the buffer for the full 900 seconds before delivery. This means the dashboard sees data that is 15+ minutes old.

**Fix 1 (still Firehose):** Minimize buffer settings for near-real-time:

```hcl
buffering_size     = 1    # 1 MB minimum
buffering_interval = 60   # 60 seconds minimum
```

This delivers data within ~60 seconds, which meets the "within 1 minute" requirement. However, Firehose can never deliver faster than 60 seconds.

**Fix 2 (true real-time):** If the requirement is sub-second latency, Firehose is the wrong service. Use Kinesis Data Streams with a Lambda consumer that processes each record immediately and writes to a real-time data store (DynamoDB, ElastiCache Redis).

**Key insight:** Firehose is a batch delivery service, not a real-time processing service. The minimum delivery latency is 60 seconds. For true real-time (sub-second), you need Data Streams.

</details>

## Verify What You Learned

```bash
STREAM=$(terraform output -raw stream_name)
FIREHOSE=$(terraform output -raw firehose_name)

# Verify Data Stream exists
aws kinesis describe-stream-summary \
  --stream-name "$STREAM" \
  --query 'StreamDescriptionSummary.{Name:StreamName,Shards:OpenShardCount,Retention:RetentionPeriodHours,Mode:StreamModeDetails.StreamMode}' \
  --output table
```

Expected: 1 shard, 24 hours retention, PROVISIONED mode.

```bash
# Verify Firehose delivery stream
aws firehose describe-delivery-stream \
  --delivery-stream-name "$FIREHOSE" \
  --query 'DeliveryStreamDescription.{Name:DeliveryStreamName,Status:DeliveryStreamStatus}' \
  --output table
```

Expected: Status = `ACTIVE`.

```bash
# Verify data was delivered to S3
BUCKET=$(terraform output -raw firehose_bucket)
aws s3 ls "s3://$BUCKET/" --recursive | wc -l
```

Expected: At least 1 file (data and/or error prefixes).

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

Exercise 97 covers **Glue ETL and Data Catalog**, where you will configure Glue crawlers to auto-discover schema from S3 data, build a Glue Data Catalog, and run ETL jobs that transform CSV to Parquet -- building on the Athena catalog knowledge from exercise 95 and the streaming data pipeline from this exercise.

## Summary

- **Kinesis Data Streams** provides real-time streaming (~200ms latency) with custom consumers and shard-based capacity
- **Kinesis Data Firehose** provides near-real-time delivery (60-900s) to S3, Redshift, OpenSearch, and Splunk with zero consumer code
- **Shard capacity**: 1 MB/s write + 2 MB/s read per shard; Enhanced Fan-Out provides dedicated 2 MB/s per consumer
- **Firehose buffers** control delivery latency: `buffering_interval` (60-900s) and `buffering_size` (1-128 MB) -- whichever is reached first
- **Data Streams retains data** (24h-365d) and supports replay; Firehose has no retention (delivery only)
- **On-Demand mode** for Data Streams auto-scales shards but costs ~20% more than provisioned mode
- **Firehose format conversion** can transform JSON to Parquet/ORC using the Glue Data Catalog schema
- **Combining both**: Data Streams for real-time processing + Firehose as a consumer for S3 archival
- **Cost model**: Data Streams charges per shard/hour; Firehose charges per GB ingested ($0.029/GB)
- **Decision rule**: custom processing or sub-second latency = Data Streams; managed delivery = Firehose

## Reference

- [Kinesis Data Streams Developer Guide](https://docs.aws.amazon.com/streams/latest/dev/introduction.html)
- [Kinesis Data Firehose Developer Guide](https://docs.aws.amazon.com/firehose/latest/dev/what-is-this-service.html)
- [Terraform aws_kinesis_stream](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/kinesis_stream)
- [Terraform aws_kinesis_firehose_delivery_stream](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/kinesis_firehose_delivery_stream)

## Additional Resources

- [Kinesis Data Streams Enhanced Fan-Out](https://docs.aws.amazon.com/streams/latest/dev/enhanced-consumers.html) -- dedicated throughput per consumer for multi-consumer architectures
- [Firehose Data Transformation](https://docs.aws.amazon.com/firehose/latest/dev/data-transformation.html) -- Lambda-based transformation of records before delivery
- [Firehose Dynamic Partitioning](https://docs.aws.amazon.com/firehose/latest/dev/dynamic-partitioning.html) -- partition S3 data by record content (e.g., customer_id) for efficient Athena queries
- [Kinesis Scaling Utilities](https://docs.aws.amazon.com/streams/latest/dev/kinesis-record-processor-scaling.html) -- auto-scaling shard count based on throughput metrics

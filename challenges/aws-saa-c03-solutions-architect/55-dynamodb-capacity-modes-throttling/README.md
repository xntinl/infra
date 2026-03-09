# 55. DynamoDB Capacity Modes and Throttling

<!--
difficulty: basic
concepts: [dynamodb-provisioned, dynamodb-on-demand, rcu, wcu, strongly-consistent-read, eventually-consistent-read, throttling, burst-capacity, item-size-rounding, auto-scaling]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: understand
prerequisites: [none]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** DynamoDB provisioned capacity charges per RCU/WCU per hour ($0.00065/RCU/hr, $0.00065/WCU/hr). On-demand charges per request ($1.25/million writes, $0.25/million reads). For this exercise with minimal capacity, cost is ~$0.01/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Basic understanding of NoSQL databases and key-value stores

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** the difference between provisioned capacity (predictable cost, potential throttling) and on-demand capacity (pay-per-request, no throttling)
- **Calculate** RCU and WCU requirements for given item sizes and access patterns
- **Identify** how item size rounding to 4 KB (reads) and 1 KB (writes) affects capacity calculations
- **Describe** the difference between strongly consistent and eventually consistent reads and their impact on RCU consumption
- **Construct** DynamoDB tables in both capacity modes using Terraform
- **Compare** the cost breakpoints where provisioned becomes cheaper than on-demand

## Why DynamoDB Capacity Modes Matter

DynamoDB capacity planning is one of the most heavily tested database topics on the SAA-C03 exam because it combines performance engineering with cost optimization. The exam presents scenarios with specific read/write patterns and item sizes, then asks you to calculate the required RCU/WCU or choose between provisioned and on-demand. Getting the calculation wrong means either throttled requests (under-provisioned) or wasted money (over-provisioned).

The fundamental unit is the Read Capacity Unit (RCU): 1 RCU = 1 strongly consistent read per second for items up to 4 KB, or 2 eventually consistent reads per second for items up to 4 KB. Write Capacity Unit (WCU): 1 WCU = 1 write per second for items up to 1 KB. Items larger than these thresholds consume proportionally more capacity. The critical detail that catches most people: item size is rounded UP to the nearest 4 KB for reads and 1 KB for writes. A 4.1 KB item consumes 2 RCUs per strongly consistent read, not 1.025.

The architectural decision between provisioned and on-demand depends on traffic predictability. Provisioned is 5-6x cheaper per request for steady workloads but requires capacity planning and risks throttling. On-demand scales automatically with zero capacity planning but costs more per request. The exam tests this trade-off with real numbers.

## Step 1 -- Create a Provisioned Capacity Table

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
  default     = "saa-ex55"
}
```

### `database.tf`

```hcl
# ------------------------------------------------------------------
# Provisioned capacity table. You specify exact RCU/WCU and pay
# a fixed hourly rate. If traffic exceeds provisioned capacity,
# requests are throttled (ProvisionedThroughputExceededException).
#
# RCU calculation example:
#   - 100 reads/second of 8 KB items (strongly consistent)
#   - 8 KB / 4 KB = 2 RCUs per read
#   - 100 reads * 2 RCUs = 200 RCUs needed
#
# WCU calculation example:
#   - 50 writes/second of 2.5 KB items
#   - ceil(2.5 KB / 1 KB) = 3 WCUs per write
#   - 50 writes * 3 WCUs = 150 WCUs needed
# ------------------------------------------------------------------
resource "aws_dynamodb_table" "provisioned" {
  name         = "${var.project_name}-provisioned"
  billing_mode = "PROVISIONED"
  hash_key     = "PK"
  range_key    = "SK"

  read_capacity  = 5
  write_capacity = 5

  attribute {
    name = "PK"
    type = "S"
  }

  attribute {
    name = "SK"
    type = "S"
  }

  tags = { Name = "${var.project_name}-provisioned" }
}

# ------------------------------------------------------------------
# On-demand capacity table. No capacity planning required. DynamoDB
# scales automatically to handle any traffic level. You pay per
# request: $1.25 per million write request units, $0.25 per million
# read request units.
#
# On-demand is ideal for:
#   - New tables with unknown workloads
#   - Unpredictable, spiky traffic patterns
#   - Applications where throttling is unacceptable
#
# On-demand is more expensive per request than provisioned:
#   - On-demand write: $1.25 per million = $0.00000125 each
#   - Provisioned write: $0.00065/WCU/hr = $0.000000018 each
#     (at 100% utilization over 1 hour)
#   - On-demand is ~69x more expensive per write at steady state
# ------------------------------------------------------------------
resource "aws_dynamodb_table" "on_demand" {
  name         = "${var.project_name}-ondemand"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "PK"
  range_key    = "SK"

  attribute {
    name = "PK"
    type = "S"
  }

  attribute {
    name = "SK"
    type = "S"
  }

  tags = { Name = "${var.project_name}-ondemand" }
}
```

### `outputs.tf`

```hcl
output "provisioned_table" {
  value = aws_dynamodb_table.provisioned.name
}

output "ondemand_table" {
  value = aws_dynamodb_table.on_demand.name
}
```

## Step 2 -- Write and Read Items

```bash
terraform init
terraform apply -auto-approve

PROV_TABLE=$(terraform output -raw provisioned_table)
OD_TABLE=$(terraform output -raw ondemand_table)

# Write items to both tables
for i in $(seq 1 10); do
  aws dynamodb put-item \
    --table-name "$PROV_TABLE" \
    --item "{
      \"PK\": {\"S\": \"USER#$i\"},
      \"SK\": {\"S\": \"PROFILE\"},
      \"name\": {\"S\": \"User $i\"},
      \"email\": {\"S\": \"user${i}@example.com\"},
      \"age\": {\"N\": \"$((20 + i))\"}
    }"
done

for i in $(seq 1 10); do
  aws dynamodb put-item \
    --table-name "$OD_TABLE" \
    --item "{
      \"PK\": {\"S\": \"USER#$i\"},
      \"SK\": {\"S\": \"PROFILE\"},
      \"name\": {\"S\": \"User $i\"},
      \"email\": {\"S\": \"user${i}@example.com\"},
      \"age\": {\"N\": \"$((20 + i))\"}
    }"
done

# Strongly consistent read (uses 1 RCU for items <= 4 KB)
aws dynamodb get-item \
  --table-name "$PROV_TABLE" \
  --key '{"PK": {"S": "USER#1"}, "SK": {"S": "PROFILE"}}' \
  --consistent-read

# Eventually consistent read (uses 0.5 RCU for items <= 4 KB)
aws dynamodb get-item \
  --table-name "$PROV_TABLE" \
  --key '{"PK": {"S": "USER#1"}, "SK": {"S": "PROFILE"}}' \
  --no-consistent-read
```

## Step 3 -- RCU and WCU Calculation Reference

### Read Capacity Units (RCU)

| Item Size | Strongly Consistent | Eventually Consistent | Transactional |
|---|---|---|---|
| <= 4 KB | 1 RCU | 0.5 RCU | 2 RCU |
| 4.1 - 8 KB | 2 RCU | 1 RCU | 4 RCU |
| 8.1 - 12 KB | 3 RCU | 1.5 RCU | 6 RCU |
| N KB | ceil(N/4) RCU | ceil(N/4) * 0.5 RCU | ceil(N/4) * 2 RCU |

### Write Capacity Units (WCU)

| Item Size | Standard Write | Transactional Write |
|---|---|---|
| <= 1 KB | 1 WCU | 2 WCU |
| 1.1 - 2 KB | 2 WCU | 4 WCU |
| 2.1 - 3 KB | 3 WCU | 6 WCU |
| N KB | ceil(N/1) WCU | ceil(N/1) * 2 WCU |

### Cost Comparison: Provisioned vs On-Demand

| Metric | Provisioned (us-east-1) | On-Demand (us-east-1) |
|---|---|---|
| Write cost | $0.00065 per WCU per hour | $1.25 per million WRU |
| Read cost | $0.00065 per RCU per hour | $0.25 per million RRU |
| Cost per million writes (sustained) | ~$0.47 (at 100% utilization) | $1.25 |
| Cost per million reads (sustained) | ~$0.47 (at 100% utilization) | $0.25 |
| Breakpoint | Cheaper above ~40% utilization | Cheaper below ~40% utilization |

## Step 4 -- Examine Consumed Capacity

```bash
PROV_TABLE=$(terraform output -raw provisioned_table)

# Read with consumed capacity metrics
aws dynamodb get-item \
  --table-name "$PROV_TABLE" \
  --key '{"PK": {"S": "USER#1"}, "SK": {"S": "PROFILE"}}' \
  --consistent-read \
  --return-consumed-capacity TOTAL

# Query with consumed capacity
aws dynamodb query \
  --table-name "$PROV_TABLE" \
  --key-condition-expression "PK = :pk" \
  --expression-attribute-values '{":pk": {"S": "USER#1"}}' \
  --consistent-read \
  --return-consumed-capacity TOTAL
```

## Step 5 -- Check CloudWatch Throttling Metrics

```bash
PROV_TABLE=$(terraform output -raw provisioned_table)

# Check for throttled requests
aws cloudwatch get-metric-statistics \
  --namespace AWS/DynamoDB \
  --metric-name ThrottledRequests \
  --dimensions Name=TableName,Value=$PROV_TABLE \
  --start-time "$(date -u -v-1H '+%Y-%m-%dT%H:%M:%S')" \
  --end-time "$(date -u '+%Y-%m-%dT%H:%M:%S')" \
  --period 300 \
  --statistics Sum
```

## Common Mistakes

### 1. Not accounting for item size rounding to 4 KB

**Wrong calculation:** "My items are 6 KB. I need 100 reads/second. That's 100 * 1.5 = 150 RCU."

**Correct calculation:** Item size is rounded UP to the nearest 4 KB multiple. A 6 KB item rounds to 8 KB (2 x 4 KB units). For strongly consistent reads: 100 * 2 = 200 RCU. For eventually consistent reads: 100 * 1 = 100 RCU.

The rounding is to 4 KB increments, not proportional. A 4.1 KB item costs the same RCU as an 8 KB item.

### 2. Forgetting that eventually consistent reads cost half

**Wrong approach:** Provisioning for strongly consistent reads when eventually consistent would suffice:

```hcl
read_capacity = 200  # Calculated for strongly consistent
```

**What happens:** If your application can tolerate reading data that might be a few hundred milliseconds stale (which most applications can), you can use eventually consistent reads at half the RCU cost.

**Fix:** Use eventually consistent reads by default and only opt into strongly consistent for operations that require the latest data:

```hcl
read_capacity = 100  # Half the RCUs needed with eventually consistent reads
```

### 3. Using on-demand for steady, predictable workloads

**Wrong approach:** Choosing on-demand because "it's simpler" for a table that handles a steady 1,000 writes/second:

```hcl
billing_mode = "PAY_PER_REQUEST"
# 1,000 writes/sec * 3600 sec/hr * $1.25/million = $4.50/hr
```

**What happens:** With provisioned capacity and auto scaling: 1,000 WCU * $0.00065/hr = $0.65/hr. You are paying 7x more for on-demand convenience.

**Fix:** Use provisioned capacity with auto scaling for predictable workloads:

```hcl
billing_mode   = "PROVISIONED"
write_capacity = 1000
# Add DynamoDB auto scaling target and policy for flexibility
```

## Verify What You Learned

```bash
PROV_TABLE=$(terraform output -raw provisioned_table)
OD_TABLE=$(terraform output -raw ondemand_table)

# Verify provisioned table capacity
aws dynamodb describe-table \
  --table-name "$PROV_TABLE" \
  --query 'Table.{BillingMode:BillingModeSummary.BillingMode,RCU:ProvisionedThroughput.ReadCapacityUnits,WCU:ProvisionedThroughput.WriteCapacityUnits}' \
  --output table
```

Expected: `BillingMode = PROVISIONED`, `RCU = 5`, `WCU = 5`

```bash
# Verify on-demand table
aws dynamodb describe-table \
  --table-name "$OD_TABLE" \
  --query 'Table.BillingModeSummary.BillingMode' \
  --output text
```

Expected: `PAY_PER_REQUEST`

```bash
# Read with consumed capacity to verify RCU calculation
aws dynamodb get-item \
  --table-name "$PROV_TABLE" \
  --key '{"PK": {"S": "USER#1"}, "SK": {"S": "PROFILE"}}' \
  --consistent-read \
  --return-consumed-capacity TOTAL \
  --query 'ConsumedCapacity.CapacityUnits'
```

Expected: `1.0` (item is under 4 KB, strongly consistent = 1 RCU)

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

Exercise 56 covers **DynamoDB Global Tables for multi-region active-active**, where you will replicate data across regions for low-latency global access. You will create a global table, write in one region, read in another, and understand last-writer-wins conflict resolution -- building on the capacity planning knowledge from this exercise.

## Summary

- **Provisioned capacity** requires you to specify RCU and WCU -- cheaper for steady workloads but risks throttling if under-provisioned
- **On-demand capacity** scales automatically with no capacity planning -- pay per request at ~5-6x higher cost per operation
- **1 RCU** = 1 strongly consistent read/second for items up to 4 KB, or 2 eventually consistent reads/second
- **1 WCU** = 1 write/second for items up to 1 KB
- **Item size rounds UP**: 4.1 KB item costs 2 RCU (rounded to 8 KB), 1.1 KB item costs 2 WCU (rounded to 2 KB)
- **Eventually consistent reads** cost half the RCUs of strongly consistent reads -- use by default unless you need the latest data
- **Transactional reads/writes** cost 2x the standard capacity units
- **DynamoDB auto scaling** adjusts provisioned capacity based on utilization -- bridges the gap between manual provisioning and on-demand
- **Burst capacity**: DynamoDB reserves unused capacity for up to 5 minutes of burst, but this is not guaranteed
- **Breakpoint**: provisioned is cheaper above ~40% sustained utilization; on-demand is cheaper for sporadic, unpredictable traffic

## Reference

- [DynamoDB Read/Write Capacity Mode](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/HowItWorks.ReadWriteCapacityMode.html)
- [DynamoDB Pricing](https://aws.amazon.com/dynamodb/pricing/)
- [Terraform aws_dynamodb_table](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/dynamodb_table)
- [DynamoDB Throughput Capacity](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/ProvisionedThroughput.html)

## Additional Resources

- [DynamoDB Auto Scaling](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/AutoScaling.html) -- automatic capacity management for provisioned tables
- [DynamoDB Burst Capacity](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/bp-partition-key-design.html#bp-partition-key-throughput-bursting) -- how unused capacity is banked for burst scenarios
- [Capacity Unit Calculations](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/CapacityUnitCalculations.html) -- detailed calculation examples with different item sizes
- [DynamoDB CloudWatch Metrics](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/metrics-dimensions.html) -- ConsumedReadCapacityUnits, ThrottledRequests, and other key metrics

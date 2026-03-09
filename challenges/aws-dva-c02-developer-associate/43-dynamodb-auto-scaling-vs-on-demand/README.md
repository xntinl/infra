# 43. DynamoDB Auto Scaling vs On-Demand

<!--
difficulty: intermediate
concepts: [provisioned-capacity, on-demand-capacity, auto-scaling, target-tracking, cloudwatch-alarms, read-capacity-units, write-capacity-units, throttling, burst-capacity]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: analyze, implement
prerequisites: [03-dynamodb-developer-sdk-operations]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates two DynamoDB tables (one provisioned, one on-demand) and Lambda functions. Provisioned capacity at the minimum settings costs negligible amounts. Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Analyze** the two DynamoDB capacity modes: provisioned (predictable cost, manual/auto capacity) vs on-demand (pay-per-request, automatic scaling)
2. **Implement** auto scaling on a provisioned table with target tracking policies that adjust RCU/WCU based on CloudWatch utilization metrics
3. **Configure** minimum and maximum capacity limits, target utilization percentage, and scale-in/scale-out cooldown periods
4. **Differentiate** between throttling causes: under-provisioned capacity, auto scaling target set too high, burst capacity exhaustion, and hot partition keys
5. **Evaluate** cost trade-offs between provisioned (cheaper at steady throughput) and on-demand (cheaper for unpredictable/spiky workloads)

## Why This Matters

Choosing the right DynamoDB capacity mode is a cost and reliability decision that the DVA-C02 exam tests directly. **Provisioned capacity** lets you specify exact RCU and WCU, and you pay for the provisioned amount whether you use it or not. Auto scaling adjusts the provisioned capacity based on actual utilization, but there is a lag: it takes 5-15 minutes for auto scaling to respond, and during that window, sudden traffic spikes cause throttling. **On-demand** capacity eliminates provisioning entirely -- DynamoDB handles all scaling automatically and you pay per request, which is roughly 6x more expensive per request than provisioned at steady state.

The exam tests edge cases: what happens when auto scaling target utilization is set to 90%? The table throttles during traffic bursts because auto scaling waits until utilization exceeds the target before scaling up, and by 90%, the burst capacity is already exhausted. What is the recommended target? 70% is the AWS default, leaving 30% headroom for bursts. The exam also tests when to switch modes: on-demand is ideal for new applications with unknown traffic patterns, and you can switch to provisioned once the pattern is established.

## Building Blocks

### Terraform Skeleton

Create the following files in your exercise directory. Fill in the `# TODO` blocks.

### `providers.tf`

```hcl
terraform {
  required_providers {
    aws     = { source = "hashicorp/aws", version = "~> 5.0" }
    archive = { source = "hashicorp/archive", version = "~> 2.0" }
    null    = { source = "hashicorp/null", version = "~> 3.0" }
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
  default     = "ddb-capacity"
}
```

### `database.tf`

```hcl
# ============================================================
# Table A: On-Demand (for comparison)
# ============================================================
resource "aws_dynamodb_table" "on_demand" {
  name         = "${var.project_name}-on-demand"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "id"

  attribute { name = "id"; type = "S" }
}

# ============================================================
# Table B: Provisioned with Auto Scaling
# ============================================================

# =======================================================
# TODO 1 -- Provisioned Table (database.tf)
# =======================================================
# Requirements:
#   - Create a table with billing_mode = "PROVISIONED"
#   - Set read_capacity = 5 and write_capacity = 5
#   - hash_key = "id"
#   - IMPORTANT: add lifecycle { ignore_changes = [read_capacity, write_capacity] }
#     because auto scaling will modify these values outside of Terraform


# =======================================================
# TODO 2 -- Auto Scaling Target (Read) (database.tf)
# =======================================================
# Requirements:
#   - Create an aws_appautoscaling_target for read capacity:
#     - service_namespace = "dynamodb"
#     - resource_id = "table/${table_name}"
#     - scalable_dimension = "dynamodb:table:ReadCapacityUnits"
#     - min_capacity = 5
#     - max_capacity = 100
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/appautoscaling_target


# =======================================================
# TODO 3 -- Auto Scaling Policy (Read) (database.tf)
# =======================================================
# Requirements:
#   - Create an aws_appautoscaling_policy for read capacity:
#     - policy_type = "TargetTrackingScaling"
#     - target_tracking_scaling_policy_configuration:
#       - target_value = 70 (70% utilization target)
#       - predefined_metric_specification:
#         - predefined_metric_type = "DynamoDBReadCapacityUtilization"
#       - scale_in_cooldown = 60
#       - scale_out_cooldown = 60
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/appautoscaling_policy


# =======================================================
# TODO 4 -- Auto Scaling Target + Policy (Write) (database.tf)
# =======================================================
# Same as TODO 2 + 3 but for write capacity:
#   - scalable_dimension = "dynamodb:table:WriteCapacityUnits"
#   - predefined_metric_type = "DynamoDBWriteCapacityUtilization"
#   - target_value = 70
```

### `iam.tf`

```hcl
data "aws_iam_policy_document" "assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals { type = "Service"; identifiers = ["lambda.amazonaws.com"] }
  }
}

resource "aws_iam_role" "this" {
  name               = "${var.project_name}-role"
  assume_role_policy = data.aws_iam_policy_document.assume.json
}

resource "aws_iam_role_policy_attachment" "basic" {
  role       = aws_iam_role.this.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

data "aws_iam_policy_document" "ddb" {
  statement {
    actions   = ["dynamodb:PutItem", "dynamodb:Query", "dynamodb:GetItem", "dynamodb:Scan", "dynamodb:DescribeTable"]
    resources = [aws_dynamodb_table.on_demand.arn, aws_dynamodb_table.provisioned.arn]
  }
}

resource "aws_iam_policy" "ddb" {
  name   = "${var.project_name}-ddb-access"
  policy = data.aws_iam_policy_document.ddb.json
}

resource "aws_iam_role_policy_attachment" "ddb" {
  role       = aws_iam_role.this.name
  policy_arn = aws_iam_policy.ddb.arn
}
```

### `outputs.tf`

```hcl
output "on_demand_table" { value = aws_dynamodb_table.on_demand.name }
output "provisioned_table" { value = aws_dynamodb_table.provisioned.name }
```

## Spot the Bug

A developer configured auto scaling on a DynamoDB table, but the application still experiences `ProvisionedThroughputExceededException` during daily traffic peaks. The auto scaling is enabled and the CloudWatch alarms are firing. **What is wrong?**

```hcl
resource "aws_appautoscaling_target" "read" {
  service_namespace  = "dynamodb"
  resource_id        = "table/${aws_dynamodb_table.provisioned.name}"
  scalable_dimension = "dynamodb:table:ReadCapacityUnits"
  min_capacity       = 5
  max_capacity       = 100
}

resource "aws_appautoscaling_policy" "read" {
  name               = "read-scaling"
  service_namespace  = "dynamodb"
  resource_id        = aws_appautoscaling_target.read.resource_id
  scalable_dimension = aws_appautoscaling_target.read.scalable_dimension
  policy_type        = "TargetTrackingScaling"

  target_tracking_scaling_policy_configuration {
    target_value = 90   # <-- BUG

    predefined_metric_specification {
      predefined_metric_type = "DynamoDBReadCapacityUtilization"
    }

    scale_in_cooldown  = 60
    scale_out_cooldown = 60
  }
}
```

<details>
<summary>Explain the bug</summary>

The target utilization is set to **90%**, which is too high. Auto scaling only triggers when utilization exceeds the target value. At 90% utilization, the table is already close to its provisioned capacity, and DynamoDB's built-in burst capacity (up to 5 minutes of unused capacity) may already be exhausted.

When a traffic spike arrives, the table operates at 90%+ utilization. Auto scaling detects this and begins provisioning more capacity, but the scaling action takes 5-15 minutes to complete. During this window, the table throttles because there is no headroom for the burst.

The fix -- lower the target to **70%** (AWS recommended default):

```hcl
target_tracking_scaling_policy_configuration {
  target_value = 70   # 30% headroom for bursts

  predefined_metric_specification {
    predefined_metric_type = "DynamoDBReadCapacityUtilization"
  }

  scale_in_cooldown  = 60
  scale_out_cooldown = 60
}
```

At 70% target utilization, auto scaling begins scaling up when the table reaches 70% of its provisioned capacity, leaving 30% headroom. By the time utilization reaches 100%, the additional capacity is usually already provisioned.

For even more headroom during known peak times, consider pre-scaling with `min_capacity` increases scheduled via a cron-triggered Lambda or AWS Application Auto Scaling scheduled actions.

</details>

## Solutions

<details>
<summary>TODO 1 -- Provisioned Table</summary>

### `database.tf`

```hcl
resource "aws_dynamodb_table" "provisioned" {
  name           = "${var.project_name}-provisioned"
  billing_mode   = "PROVISIONED"
  read_capacity  = 5
  write_capacity = 5
  hash_key       = "id"

  attribute { name = "id"; type = "S" }

  lifecycle {
    ignore_changes = [read_capacity, write_capacity]
  }
}
```

</details>

<details>
<summary>TODO 2 + TODO 3 -- Read Auto Scaling</summary>

### `database.tf`

```hcl
resource "aws_appautoscaling_target" "read" {
  service_namespace  = "dynamodb"
  resource_id        = "table/${aws_dynamodb_table.provisioned.name}"
  scalable_dimension = "dynamodb:table:ReadCapacityUnits"
  min_capacity       = 5
  max_capacity       = 100
}

resource "aws_appautoscaling_policy" "read" {
  name               = "${var.project_name}-read-scaling"
  service_namespace  = "dynamodb"
  resource_id        = aws_appautoscaling_target.read.resource_id
  scalable_dimension = aws_appautoscaling_target.read.scalable_dimension
  policy_type        = "TargetTrackingScaling"

  target_tracking_scaling_policy_configuration {
    target_value = 70

    predefined_metric_specification {
      predefined_metric_type = "DynamoDBReadCapacityUtilization"
    }

    scale_in_cooldown  = 60
    scale_out_cooldown = 60
  }
}
```

</details>

<details>
<summary>TODO 4 -- Write Auto Scaling</summary>

### `database.tf`

```hcl
resource "aws_appautoscaling_target" "write" {
  service_namespace  = "dynamodb"
  resource_id        = "table/${aws_dynamodb_table.provisioned.name}"
  scalable_dimension = "dynamodb:table:WriteCapacityUnits"
  min_capacity       = 5
  max_capacity       = 100
}

resource "aws_appautoscaling_policy" "write" {
  name               = "${var.project_name}-write-scaling"
  service_namespace  = "dynamodb"
  resource_id        = aws_appautoscaling_target.write.resource_id
  scalable_dimension = aws_appautoscaling_target.write.scalable_dimension
  policy_type        = "TargetTrackingScaling"

  target_tracking_scaling_policy_configuration {
    target_value = 70

    predefined_metric_specification {
      predefined_metric_type = "DynamoDBWriteCapacityUtilization"
    }

    scale_in_cooldown  = 60
    scale_out_cooldown = 60
  }
}
```

</details>

## Verify What You Learned

```bash
# Verify on-demand table
aws dynamodb describe-table --table-name ddb-capacity-on-demand \
  --query "Table.BillingModeSummary.BillingMode" --output text

# Verify provisioned table
aws dynamodb describe-table --table-name ddb-capacity-provisioned \
  --query "{Mode:BillingModeSummary.BillingMode,RCU:ProvisionedThroughput.ReadCapacityUnits,WCU:ProvisionedThroughput.WriteCapacityUnits}" \
  --output json

# Verify auto scaling targets
aws application-autoscaling describe-scalable-targets \
  --service-namespace dynamodb \
  --query "ScalableTargets[*].{Dimension:ScalableDimension,Min:MinCapacity,Max:MaxCapacity}" \
  --output table

# Verify auto scaling policies
aws application-autoscaling describe-scaling-policies \
  --service-namespace dynamodb \
  --query "ScalingPolicies[*].{Name:PolicyName,Target:TargetTrackingScalingPolicyConfiguration.TargetValue}" \
  --output table

terraform plan
```

Expected: on-demand table in `PAY_PER_REQUEST` mode, provisioned table with 5 RCU/WCU, auto scaling targets 5-100, target value 70.

## Cleanup

```bash
terraform destroy -auto-approve
terraform state list
```

Expected: no output (empty state).

## What's Next

You compared provisioned and on-demand capacity modes and configured auto scaling. In the next exercise, you will set up **DynamoDB global tables** for multi-region replication -- writing in one region and reading from another with eventual consistency.

## Summary

- **On-demand** (`PAY_PER_REQUEST`): automatic scaling, no capacity planning, ~6x cost per request vs provisioned at steady state; ideal for unpredictable workloads
- **Provisioned**: manual capacity setting, cheaper at steady throughput; use with auto scaling for dynamic workloads
- **Auto scaling** uses target tracking: set a utilization target (recommended 70%), and DynamoDB adjusts capacity to maintain that utilization
- The `lifecycle { ignore_changes }` block in Terraform prevents auto-scaled capacity values from being reverted on the next `terraform apply`
- **Burst capacity**: DynamoDB saves unused capacity for up to 5 minutes; bursts consume this reserve before throttling occurs
- Setting the target utilization too high (90%+) causes throttling during traffic spikes because auto scaling cannot react fast enough
- You can switch between provisioned and on-demand modes once every 24 hours

## Reference

- [DynamoDB Read/Write Capacity Mode](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/HowItWorks.ReadWriteCapacityMode.html)
- [DynamoDB Auto Scaling](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/AutoScaling.html)
- [Terraform aws_appautoscaling_target](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/appautoscaling_target)
- [Terraform aws_appautoscaling_policy](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/appautoscaling_policy)

## Additional Resources

- [Burst Capacity](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/bp-partition-key-design.html#bp-partition-key-throughput-bursting) -- how unused capacity is reserved and consumed during bursts
- [Provisioned vs On-Demand Cost Comparison](https://aws.amazon.com/dynamodb/pricing/) -- calculating break-even points between modes
- [Auto Scaling Best Practices](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/AutoScaling.html#AutoScaling.BestPractices) -- target utilization, cooldowns, and pre-scaling strategies
- [Hot Partition Diagnosis](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/bp-partition-key-design.html) -- throttling caused by uneven key distribution, not insufficient capacity

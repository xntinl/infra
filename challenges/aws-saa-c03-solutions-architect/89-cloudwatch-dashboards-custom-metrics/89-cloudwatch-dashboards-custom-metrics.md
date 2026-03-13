# 89. CloudWatch Dashboards and Custom Metrics

<!--
difficulty: basic
concepts: [cloudwatch-dashboard, dashboard-widgets, custom-metrics, put-metric-data, metric-dimensions, high-resolution-metrics, metric-math, anomaly-detection, namespace]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: understand
prerequisites: [88-datasync-online-transfer]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** CloudWatch dashboards cost $3/month per dashboard (first 3 free). Custom metrics cost $0.30/metric/month (first 10 free). High-resolution metrics (sub-minute) cost $0.30/metric/month vs $0.10 for standard resolution. For this exercise with free tier, cost is ~$0.01/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Understanding of basic CloudWatch concepts (metrics, namespaces, dimensions)
- Familiarity with JSON (dashboard widget definitions use JSON)

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** how CloudWatch dashboards organize operational metrics into visual widgets (line, stacked area, number, text)
- **Describe** the relationship between namespaces, metric names, and dimensions in CloudWatch
- **Identify** when custom metrics are needed versus relying on built-in AWS metrics
- **Construct** a CloudWatch dashboard with widgets for EC2, RDS, and ALB metrics using Terraform
- **Distinguish** between standard resolution (60-second) and high-resolution (1-second) custom metrics and their cost implications
- **Compare** CloudWatch dashboards with metric math and anomaly detection for advanced monitoring patterns

## Why This Matters

CloudWatch is the foundation of operational visibility on AWS, and the SAA-C03 exam expects you to understand both its capabilities and its cost model. Every AWS service automatically publishes metrics to CloudWatch -- EC2 CPU utilization, RDS connections, ALB request count -- at no charge for basic metrics. But built-in metrics have gaps: EC2 does not report memory utilization or disk usage from within the instance, and your application's business metrics (orders per minute, API latency percentiles, queue depth) are invisible to CloudWatch unless you publish them as custom metrics.

The cost model is the architectural trap. Each custom metric costs $0.30/metric/month at standard resolution. This seems small, but a dimension explosion can make it expensive quickly. If you publish a metric with dimensions for `{service, endpoint, status_code, region}`, and you have 10 services, 50 endpoints, 5 status codes, and 4 regions, that is 10,000 unique metric combinations at $3,000/month. High-resolution metrics (1-second granularity instead of 60-second) do not cost more per metric, but they generate more data points that count against CloudWatch Metrics Insights query limits. The exam tests whether you can design a monitoring strategy that provides operational insight without runaway costs.

## Step 1 -- Create the Dashboard

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
  default     = "saa-ex89"
}
```

### `monitoring.tf`

```hcl
# Dashboard: JSON document defining widgets on a 24-column grid.
# Widget types: metric (line/stacked/singleValue), text, log, alarm.
# Dashboards are global -- they can display metrics from any region.
resource "aws_cloudwatch_dashboard" "operations" {
  dashboard_name = "${var.project_name}-operations"

  dashboard_body = jsonencode({
    widgets = [
      {
        type = "text", x = 0, y = 0, width = 24, height = 1
        properties = { markdown = "# Operations Dashboard" }
      },
      {
        type = "metric", x = 0, y = 1, width = 12, height = 6
        properties = {
          metrics = [["AWS/EC2", "CPUUtilization", { stat = "Average", period = 300 }]]
          title = "EC2 CPU Utilization", view = "timeSeries", region = var.region
          yAxis = { left = { min = 0, max = 100 } }
        }
      },
      {
        type = "metric", x = 12, y = 1, width = 12, height = 6
        properties = {
          metrics = [
            ["AWS/EC2", "NetworkIn", { stat = "Sum", period = 300 }],
            ["AWS/EC2", "NetworkOut", { stat = "Sum", period = 300 }]
          ]
          title = "EC2 Network Traffic", view = "timeSeries", stacked = true, region = var.region
        }
      },
      {
        type = "metric", x = 0, y = 7, width = 8, height = 6
        properties = {
          metrics = [["AWS/RDS", "DatabaseConnections", { stat = "Average", period = 300 }]]
          title = "RDS Connections", view = "timeSeries", region = var.region
        }
      },
      {
        type = "metric", x = 8, y = 7, width = 8, height = 6
        properties = {
          metrics = [
            ["AWS/ApplicationELB", "RequestCount", { stat = "Sum", period = 60 }],
            ["AWS/ApplicationELB", "HTTPCode_Target_5XX_Count", { stat = "Sum", period = 60 }]
          ]
          title = "ALB Requests & 5XX", view = "timeSeries", region = var.region
        }
      },
      {
        type = "metric", x = 16, y = 7, width = 8, height = 6
        properties = {
          metrics = [
            ["SaaEx89/Application", "ApiLatencyMs", { stat = "p50", period = 60 }],
            ["SaaEx89/Application", "ApiLatencyMs", { stat = "p99", period = 60 }]
          ]
          title = "API Latency Percentiles", view = "timeSeries", region = var.region
        }
      }
    ]
  })
}
```

### `outputs.tf`

```hcl
output "dashboard_url" {
  value = "https://${var.region}.console.aws.amazon.com/cloudwatch/home?region=${var.region}#dashboards:name=${aws_cloudwatch_dashboard.operations.dashboard_name}"
}
```

```bash
terraform init
terraform apply -auto-approve
```

## Step 2 -- Publish Custom Metrics

```bash
# Simple metric (standard 60-second resolution)
aws cloudwatch put-metric-data \
  --namespace "SaaEx89/Application" \
  --metric-name "OrdersPerMinute" \
  --value 42 --unit "Count" \
  --dimensions "Environment=Production,Service=OrderAPI"

# High-resolution metric (1-second) + batch publish
aws cloudwatch put-metric-data \
  --namespace "SaaEx89/Application" \
  --metric-data '[
    { "MetricName": "ApiLatencyMs", "Value": 125.5, "Unit": "Milliseconds",
      "StorageResolution": 1,
      "Dimensions": [{"Name":"Environment","Value":"Production"},{"Name":"Service","Value":"OrderAPI"}] },
    { "MetricName": "ActiveUsers", "Value": 1250, "Unit": "Count",
      "Dimensions": [{"Name":"Environment","Value":"Production"}] }
  ]'

# Query metrics
aws cloudwatch list-metrics --namespace "SaaEx89/Application" --output table

aws cloudwatch get-metric-statistics \
  --namespace "SaaEx89/Application" --metric-name "OrdersPerMinute" \
  --dimensions Name=Environment,Value=Production \
  --start-time "$(date -u -v-1H +%Y-%m-%dT%H:%M:%S)" \
  --end-time "$(date -u +%Y-%m-%dT%H:%M:%S)" \
  --period 300 --statistics Sum Average Maximum --output table
```

## Step 3 -- Cost Analysis

| Resolution | Period | Cost/Metric/Month | Use Case |
|---|---|---|---|
| Standard | 60 seconds | $0.30 (first 10 free) | Application KPIs, business metrics |
| High-resolution | 1 second | $0.30 (same price) | Auto-scaling triggers, real-time alerting |
| Detailed EC2 | 60 seconds | $2.10/instance/month | Production EC2 instances |
| Basic EC2 | 300 seconds | Free | Dev/test EC2 instances |

### Dimension Explosion Warning

```
# 10 services x 50 endpoints x 5 status codes x 4 regions = 10,000 metrics
# Cost: 10,000 x $0.30 = $3,000/month
#
# FIX: Reduce dimensionality to ~120 metrics = $36/month
#   - Aggregate per-service (10), top-10 endpoints (100), success/error (2)
```

## Common Mistakes

### 1. High-resolution metrics without understanding retention

**Wrong:** Publishing all metrics at `StorageResolution: 1`. High-resolution data retains 1-second granularity for only 3 hours, then rolls up to 60-second. Use standard resolution for dashboards; reserve high-resolution for auto-scaling and real-time alerts.

### 2. Dimension explosion with unbounded values

**Wrong:** Using `UserId` or `RequestId` as dimensions. Each unique combination creates a separate metric. 100,000 users = 100,000 metrics x $0.30 = $30,000/month. **Fix:** Use bounded dimensions (`Environment`, `Service`, `Region`). For per-user analysis, use CloudWatch Logs Insights.

### 3. Publishing derived metrics instead of using metric math

**Wrong:** Publishing `ErrorRate` as a custom metric when it equals `ErrorCount / RequestCount`. **Fix:** Use metric math in dashboards: `{ "expression": "errors/requests*100", "id": "rate" }` -- no additional metric cost.

## Verify What You Learned

```bash
# Verify dashboard exists
aws cloudwatch list-dashboards \
  --dashboard-name-prefix "saa-ex89" \
  --query 'DashboardEntries[*].{Name:DashboardName,Size:Size}' \
  --output table
```

Expected: One dashboard named `saa-ex89-operations`.

```bash
# Verify custom metrics exist
aws cloudwatch list-metrics \
  --namespace "SaaEx89/Application" \
  --query 'Metrics[*].{Name:MetricName,Dimensions:Dimensions[*].Name}' \
  --output table
```

Expected: `OrdersPerMinute`, `ApiLatencyMs`, and `ActiveUsers` metrics with their dimensions.

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

```bash
# Delete custom metrics (they expire automatically after 15 months of
# no new data, but the namespace remains visible for a few hours)
# No action needed -- custom metrics are not Terraform-managed

terraform destroy -auto-approve
```

Verify:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

Exercise 90 covers **CloudTrail API Auditing**, where you will configure trails to capture management and data events, deliver logs to S3 with integrity validation, and understand the difference between management events (free, 90-day retention) and data events (charged per 100,000 events) -- building the compliance auditing layer on top of the operational visibility from this exercise.

## Summary

- **CloudWatch dashboards** provide visual operational monitoring with widgets for metrics, logs, alarms, and text
- **Widget types** include line charts, stacked area, single value (number), text (markdown), and alarm status
- **Custom metrics** use `PutMetricData` API to publish application-specific metrics that AWS does not collect automatically
- **Namespaces** group related metrics (e.g., `MyApp/Production`); dimensions identify specific sources within a namespace
- **Standard resolution** (60-second) is sufficient for most dashboards and costs $0.30/metric/month
- **High-resolution** (1-second) is for auto-scaling triggers and real-time alerting; data retains 1-second granularity for only 3 hours
- **Dimension explosion** is the biggest cost risk -- unbounded dimensions (UserId, RequestId) create thousands of metrics
- **Metric math** derives new metrics from existing ones at no additional cost (error rate = errors / total requests)
- **EC2 memory and disk** metrics require the CloudWatch agent -- they are not available from the hypervisor
- **Data retention**: 1-second data for 3 hours, 60-second for 15 days, 5-minute for 63 days, 1-hour for 15 months

## Reference

- [CloudWatch Dashboards](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/CloudWatch_Dashboards.html)
- [Publishing Custom Metrics](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/publishingMetrics.html)
- [Terraform aws_cloudwatch_dashboard](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_dashboard)
- [CloudWatch Pricing](https://aws.amazon.com/cloudwatch/pricing/)

## Additional Resources

- [Using Metric Math](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/using-metric-math.html) -- calculate derived metrics without publishing additional custom metrics
- [CloudWatch Anomaly Detection](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/CloudWatch_Anomaly_Detection.html) -- ML-based anomaly detection that establishes normal baselines automatically
- [CloudWatch Agent Configuration](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/Install-CloudWatch-Agent.html) -- collecting memory, disk, and custom log metrics from EC2 instances
- [Dashboard Body Structure Reference](https://docs.aws.amazon.com/AmazonCloudWatch/latest/APIReference/CloudWatch-Dashboard-Body-Structure.html) -- complete JSON schema for dashboard widget definitions

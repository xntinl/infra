# 46. Predictive Scaling with Machine Learning

<!--
difficulty: advanced
concepts: [predictive-scaling, ml-forecasting, forecast-only, forecast-and-scale, scaling-plan, metric-specification, capacity-forecast, scheduling-buffer]
tools: [terraform, aws-cli]
estimated_time: 50m
bloom_level: evaluate
prerequisites: [04-auto-scaling-policies-deep-dive, 45-asg-lifecycle-hooks-custom-actions]
aws_cost: ~$0.05/hr
-->

> **AWS Cost Warning:** ASG with t3.micro instances (~$0.0104/hr each), CloudWatch metrics (free tier for basic). Total ~$0.05/hr at baseline capacity. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 04 (Auto Scaling Policies Deep Dive)
- Understanding of target tracking and step scaling policies
- Familiarity with CloudWatch metrics and time-series data

## Learning Objectives

After completing this exercise, you will be able to:

- **Evaluate** when predictive scaling provides value over reactive scaling policies based on workload patterns
- **Design** a predictive scaling configuration that balances proactive capacity with cost efficiency
- **Analyze** the differences between ForecastOnly and ForecastAndScale modes and their operational implications
- **Implement** predictive scaling policies with custom metric specifications and scheduling buffer times
- **Assess** the ML model's forecast accuracy by comparing predicted vs actual capacity needs

## Why This Matters

Reactive scaling -- target tracking, step scaling -- always responds after the metric breaches a threshold. For workloads with predictable daily or weekly patterns (e-commerce traffic peaks, business-hours SaaS usage, batch processing schedules), this means instances launch only after users experience degraded performance. Predictive scaling uses machine learning to analyze up to 14 days of historical CloudWatch data and forecast future capacity needs, launching instances before the demand arrives.

The SAA-C03 exam increasingly tests predictive scaling because it represents a fundamental architectural trade-off: proactive scaling eliminates the cold-start latency gap but requires enough historical data (at least 24 hours, ideally 14 days) and predictable patterns to be effective. Random spikes from viral events or DDoS attacks will not be predicted. Understanding when to combine predictive scaling with reactive scaling -- and when predictive scaling adds no value -- separates a thoughtful architect from one who treats every scaling problem the same way.

The two modes are exam favorites. ForecastOnly generates forecasts without taking scaling actions -- use this to validate the ML model before trusting it with production capacity. ForecastAndScale actively adjusts the ASG's minimum capacity based on forecasts, ensuring instances are ready before demand arrives. The scheduling buffer time adds extra lead time for instances that take several minutes to initialize (install software, warm caches).

## The Challenge

You are architecting a SaaS platform that serves business users primarily during working hours (8 AM - 6 PM) across three time zones. Traffic ramps up gradually in the morning, peaks at midday, and drops off in the evening, with minimal overnight usage. The reactive target tracking policy currently causes 3-5 minute latency spikes during the morning ramp because instances take 2 minutes to launch and initialize. Design a predictive scaling solution that eliminates these morning latency spikes.

### Requirements

1. Deploy an ASG with a predictive scaling policy using ForecastOnly mode initially
2. Configure custom metric specifications for CPU utilization and request count
3. Set a scheduling buffer time that accounts for instance initialization time
4. Create a plan to transition from ForecastOnly to ForecastAndScale after validation
5. Combine predictive scaling with a target tracking policy as a safety net for unpredicted spikes

### Architecture

```
Historical CloudWatch Data (14 days)
            |
            v
    ML Forecasting Model
    (analyzes patterns)
            |
            v
    Capacity Forecast
    (predicted min capacity per hour)
            |
    +-------+-------+
    |               |
    v               v
ForecastOnly    ForecastAndScale
(metrics only)  (adjusts ASG min)
                    |
                    v
              ASG launches instances
              BEFORE demand arrives
                    |
                    v
              + Scheduling Buffer
              (extra lead time for
               instance warmup)
```

## Hints

<details>
<summary>Hint 1: Predictive Scaling Policy Structure</summary>

A predictive scaling policy requires metric specifications that define what the ML model uses for forecasting. There are two approaches:

**Predefined metrics (simple):**
```hcl
resource "aws_autoscaling_policy" "predictive" {
  name                   = "predictive-scaling"
  autoscaling_group_name = aws_autoscaling_group.this.name
  policy_type            = "PredictiveScaling"

  predictive_scaling_configuration {
    mode                          = "ForecastOnly"
    scheduling_buffer_time        = 120  # seconds before forecast time

    metric_specification {
      target_value = 60

      predefined_scaling_metric_specification {
        predefined_metric_type = "ASGAverageCPUUtilization"
        resource_label         = ""
      }

      predefined_load_metric_specification {
        predefined_metric_type = "ASGTotalCPUUtilization"
        resource_label         = ""
      }
    }
  }
}
```

**Custom metrics (advanced, needed for ALB request count):**

Custom metric specifications let you define the exact CloudWatch metrics the model uses for load and scaling decisions.

</details>

<details>
<summary>Hint 2: ForecastOnly vs ForecastAndScale</summary>

- **ForecastOnly**: The ML model generates forecasts and publishes them as CloudWatch metrics, but does NOT modify the ASG. Use this mode for 1-2 weeks to validate forecast accuracy before enabling active scaling.

- **ForecastAndScale**: The model actively sets the ASG's minimum capacity based on the forecast. The ASG will never scale below the forecasted minimum, but reactive policies (target tracking, step scaling) can still scale above it.

The transition path:

1. Deploy with `mode = "ForecastOnly"` for at least 2 weeks
2. Compare forecasted capacity vs actual capacity in CloudWatch
3. If the forecast is consistently accurate (within 10-20% of actual), switch to `mode = "ForecastAndScale"`
4. Keep a target tracking policy active as a safety net

Important: Predictive scaling sets the **minimum** capacity, not the desired. Reactive policies can still increase capacity beyond the forecast.

</details>

<details>
<summary>Hint 3: Scheduling Buffer Time</summary>

The `scheduling_buffer_time` parameter adds lead time to the forecast. If the model predicts that 6 instances are needed at 8:00 AM, and you set `scheduling_buffer_time = 300` (5 minutes), the ASG will start launching instances at 7:55 AM.

This accounts for:
- EC2 instance launch time (~1-2 minutes)
- User data script execution (software installation, configuration)
- Application warmup (cache loading, JVM JIT compilation)
- Health check passing (ALB health check interval)

Calculate your buffer as:
```
buffer = instance_launch_time + user_data_duration + warmup_time + health_check_interval
```

For example: 90s launch + 120s user_data + 60s warmup + 30s health_check = 300s buffer.

</details>

<details>
<summary>Hint 4: Analyzing Forecast Accuracy</summary>

Predictive scaling publishes forecast metrics to CloudWatch in the `AWS/AutoScaling` namespace:

```bash
# View the capacity forecast
aws autoscaling get-predictive-scaling-forecast \
  --auto-scaling-group-name "my-asg" \
  --policy-name "predictive-scaling" \
  --start-time "$(date -u -v-2d '+%Y-%m-%dT%H:%M:%SZ')" \
  --end-time "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
```

The response includes:
- `LoadForecast`: predicted load metric values
- `CapacityForecast`: predicted number of instances needed
- `UpdateTime`: when the forecast was last refreshed

Compare `CapacityForecast` values against actual `GroupInServiceInstances` from CloudWatch to assess accuracy.

</details>

## Spot the Bug

The following predictive scaling configuration has a design flaw that could cause cost overruns. Identify the problem before expanding the answer.

```hcl
resource "aws_autoscaling_policy" "predictive" {
  name                   = "predictive-cpu"
  autoscaling_group_name = aws_autoscaling_group.this.name
  policy_type            = "PredictiveScaling"

  predictive_scaling_configuration {
    mode                          = "ForecastAndScale"
    scheduling_buffer_time        = 0
    max_capacity_breach_behavior  = "HonorMaxCapacity"

    metric_specification {
      target_value = 60

      predefined_scaling_metric_specification {
        predefined_metric_type = "ASGAverageCPUUtilization"
        resource_label         = ""
      }

      predefined_load_metric_specification {
        predefined_metric_type = "ASGTotalCPUUtilization"
        resource_label         = ""
      }
    }
  }
}
```

This is deployed on a brand-new ASG that was created 6 hours ago.

<details>
<summary>Explain the bug</summary>

There are two problems:

**Bug 1: ForecastAndScale on a brand-new ASG with insufficient historical data.** Predictive scaling requires at least 24 hours of historical CloudWatch data to generate meaningful forecasts, and works best with 14 days of data to identify weekly patterns. With only 6 hours of data, the ML model will produce unreliable forecasts that may:

- Over-provision capacity based on initial deployment activity (installs, health checks, warmup causing CPU spikes)
- Under-provision during actual peak hours it has never observed
- Create erratic scaling patterns as the model trains on noisy, insufficient data

**Fix:** Always start with `mode = "ForecastOnly"` and validate the forecast for at least 1-2 weeks before switching to `ForecastAndScale`. The model needs to observe at least one full weekly cycle to detect day-of-week patterns.

**Bug 2: `scheduling_buffer_time = 0` defeats the purpose of predictive scaling.** With zero buffer, instances launch at exactly the forecasted time -- but instances take 1-2 minutes to launch and pass health checks. Users will still experience the latency spike during the ramp because the instances are not yet ready when traffic arrives.

**Fix:** Set `scheduling_buffer_time` to cover instance launch time plus initialization time. For a typical application: `scheduling_buffer_time = 300` (5 minutes).

</details>

## Verify What You Learned

```bash
ASG_NAME="saa-ex46-asg"

# Verify predictive scaling policy is attached
aws autoscaling describe-policies \
  --auto-scaling-group-name "$ASG_NAME" \
  --policy-types "PredictiveScaling" \
  --query 'ScalingPolicies[*].{Name:PolicyName,Mode:PredictiveScalingConfiguration.Mode,Buffer:PredictiveScalingConfiguration.SchedulingBufferTime}' \
  --output table
```

Expected: One policy with `Mode = ForecastOnly` and a non-zero `Buffer`.

```bash
# Verify target tracking policy also exists (safety net)
aws autoscaling describe-policies \
  --auto-scaling-group-name "$ASG_NAME" \
  --policy-types "TargetTrackingScaling" \
  --query 'ScalingPolicies[*].{Name:PolicyName,Target:TargetTrackingConfiguration.TargetValue}' \
  --output table
```

Expected: One target tracking policy with target value of 60.

```bash
# Attempt to retrieve a forecast (may return empty if <24h of data)
aws autoscaling get-predictive-scaling-forecast \
  --auto-scaling-group-name "$ASG_NAME" \
  --policy-name "saa-ex46-predictive" \
  --start-time "$(date -u -v-1d '+%Y-%m-%dT%H:%M:%SZ')" \
  --end-time "$(date -u -v+1d '+%Y-%m-%dT%H:%M:%SZ')" 2>&1
```

Expected: Either a forecast response or a message indicating insufficient data (both are valid for a new ASG).

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify no resources remain:

```bash
aws autoscaling describe-auto-scaling-groups \
  --auto-scaling-group-names saa-ex46-asg \
  --query 'AutoScalingGroups' 2>/dev/null || echo "ASG deleted successfully"
```

## What's Next

Exercise 47 introduces **S3 storage classes and lifecycle policies**, where you will transition from compute scaling to storage optimization. You will create lifecycle rules that automatically move objects between storage classes based on access patterns -- a cost optimization technique that the SAA-C03 exam tests with detailed scenario questions about access frequency, retrieval time requirements, and storage class eligibility.

## Summary

- **Predictive scaling** uses ML to analyze up to 14 days of historical CloudWatch data and forecast future capacity needs
- **ForecastOnly** mode generates forecasts without taking action -- use for 1-2 weeks to validate accuracy before enabling active scaling
- **ForecastAndScale** mode actively sets the ASG's minimum capacity based on forecasts, launching instances before demand arrives
- **Scheduling buffer time** adds lead time for instance initialization -- calculate as launch_time + user_data + warmup + health_check
- **At least 24 hours of data** is required; 14 days is recommended for weekly pattern detection
- **Predictive scaling sets minimum capacity**, not desired -- reactive policies (target tracking, step scaling) can still scale above the forecast
- **Combine with target tracking** as a safety net for unpredicted spikes that the ML model has not seen before
- **max_capacity_breach_behavior** controls whether the forecast can exceed the ASG's max_size (`HonorMaxCapacity` caps it, `IncreaseMaxCapacity` raises it temporarily)
- Random/unpredictable workloads (viral events, DDoS) gain no benefit from predictive scaling -- they need reactive policies or pre-provisioned capacity

## Reference

- [Predictive Scaling for Amazon EC2 Auto Scaling](https://docs.aws.amazon.com/autoscaling/ec2/userguide/ec2-auto-scaling-predictive-scaling.html)
- [GetPredictiveScalingForecast API](https://docs.aws.amazon.com/autoscaling/ec2/APIReference/API_GetPredictiveScalingForecast.html)
- [Terraform aws_autoscaling_policy (PredictiveScaling)](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/autoscaling_policy)
- [Predictive Scaling Metrics](https://docs.aws.amazon.com/autoscaling/ec2/userguide/predictive-scaling-customized-metric-specification.html)

## Additional Resources

- [Predictive Scaling Best Practices](https://docs.aws.amazon.com/autoscaling/ec2/userguide/predictive-scaling-best-practices.html) -- AWS recommendations for metric selection, buffer time, and mode transitions
- [Custom Metric Specifications](https://docs.aws.amazon.com/autoscaling/ec2/userguide/predictive-scaling-customized-metric-specification.html) -- defining custom CloudWatch metrics for load and scaling predictions
- [Predictive Scaling vs Target Tracking](https://aws.amazon.com/blogs/compute/introducing-native-support-for-predictive-scaling-with-amazon-ec2-auto-scaling/) -- AWS blog post comparing proactive and reactive scaling approaches
- [CloudWatch Metrics for Auto Scaling](https://docs.aws.amazon.com/autoscaling/ec2/userguide/ec2-auto-scaling-cloudwatch-monitoring.html) -- metrics used by the predictive scaling ML model

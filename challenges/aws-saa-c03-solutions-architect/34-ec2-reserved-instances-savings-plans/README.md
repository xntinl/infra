# 34. EC2 Reserved Instances and Savings Plans

<!--
difficulty: intermediate
concepts: [reserved-instances, standard-ri, convertible-ri, savings-plans, compute-savings-plan, ec2-savings-plan, cost-explorer, break-even-analysis]
tools: [aws-cli]
estimated_time: 40m
bloom_level: evaluate, analyze
prerequisites: [31-ec2-instance-types-right-sizing, 33-ec2-spot-instances-fleet-strategies]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise focuses on analysis and does not require purchasing Reserved Instances or Savings Plans. The only cost is running Cost Explorer queries and optional t3.micro instances for demonstration (~$0.01/hr). Do NOT purchase actual reservations in a sandbox account.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| AWS account with some EC2 usage history | `aws ce get-cost-and-usage --time-period Start=2025-01-01,End=2025-02-01 --granularity MONTHLY --metrics BlendedCost` |
| Completed exercise 31 (instance types) | Understanding of instance families and sizing |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Differentiate** between Standard RIs (locked family/region, up to 72% savings) and Convertible RIs (exchangeable, up to 66% savings).
2. **Compare** EC2 Savings Plans (instance family locked) with Compute Savings Plans (fully flexible) and explain when each is appropriate.
3. **Calculate** the break-even point for a Reserved Instance commitment versus on-demand pricing.
4. **Analyze** EC2 usage patterns with Cost Explorer to identify reservation candidates.
5. **Evaluate** the complete EC2 pricing portfolio (On-Demand, Spot, Reserved, Savings Plans) and select the optimal mix for a given workload.

---

## Why This Matters

The SAA-C03 exam frequently presents cost optimization scenarios where you must recommend the right pricing model. The question is never simply "use Reserved Instances" -- it asks you to choose between Standard RI, Convertible RI, EC2 Savings Plan, and Compute Savings Plan based on specific constraints. A company that might change instance families needs Convertible RIs or Compute Savings Plans. A company committed to a specific instance family in a specific region gets the deepest discount with Standard RIs. A company running containers across Fargate and EC2 benefits from Compute Savings Plans because they cover both.

The break-even analysis is equally important. A 1-year All Upfront RI breaks even at around 7-8 months of usage. If you are not certain the workload will run that long, on-demand or Spot may be cheaper. A 3-year commitment offers deeper discounts (up to 72%) but locks you in for 36 months -- if AWS releases a new instance generation with better price-performance, you are stuck on the old one with Standard RIs. Convertible RIs let you exchange, but at a lower discount (up to 66%). These trade-offs are exactly what the exam tests.

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
  default     = "saa-ex34"
}
```

### `main.tf`

```hcl
# ============================================================
# TODO 1: Analyze Current EC2 Usage with Cost Explorer
# ============================================================
# Before recommending a pricing model, you need to understand
# usage patterns. Use the AWS CLI to query Cost Explorer.
#
# Requirements (run these CLI commands):
#
# 1. Get EC2 cost by instance type for the last 3 months:
#    aws ce get-cost-and-usage \
#      --time-period Start=YYYY-MM-01,End=YYYY-MM-01 \
#      --granularity MONTHLY \
#      --metrics BlendedCost \
#      --group-by Type=DIMENSION,Key=INSTANCE_TYPE \
#      --filter '{"Dimensions":{"Key":"SERVICE","Values":["Amazon Elastic Compute Cloud - Compute"]}}'
#
# 2. Get RI utilization (if any RIs exist):
#    aws ce get-reservation-utilization \
#      --time-period Start=YYYY-MM-01,End=YYYY-MM-01 \
#      --granularity MONTHLY
#
# 3. Get RI purchase recommendations:
#    aws ce get-reservation-purchase-recommendation \
#      --service "Amazon Elastic Compute Cloud - Compute" \
#      --term-in-years ONE_YEAR \
#      --payment-option ALL_UPFRONT \
#      --lookback-period-in-days THIRTY_DAYS
#
# Document what you find:
# - Which instance types consume the most spend?
# - Are any instances running 24/7 (good RI candidates)?
# - Are any instances running <40% of the month (bad RI candidates)?
# ============================================================


# ============================================================
# TODO 2: Calculate Break-Even for m5.large Reservation
# ============================================================
# Given the following pricing for m5.large in us-east-1:
#
# On-Demand:      $0.096/hr = $69.12/month = $829.44/year
#
# Standard RI (1yr):
#   No Upfront:   $0.060/hr = $43.20/month = $518.40/year  (37% savings)
#   Partial:      $253 upfront + $0.030/hr = $253 + $262.80 = $515.80  (38%)
#   All Upfront:  $510/year                                              (39%)
#
# Standard RI (3yr):
#   All Upfront:  $968/3yr = $322.67/year                               (61%)
#
# Convertible RI (1yr):
#   All Upfront:  $584/year                                              (30%)
#
# Convertible RI (3yr):
#   All Upfront:  $1,164/3yr = $388/year                                (53%)
#
# Compute Savings Plan (1yr):
#   All Upfront:  ~$566/year (per $0.060/hr commitment)                 (32%)
#
# EC2 Savings Plan (1yr):
#   All Upfront:  ~$520/year (per $0.060/hr commitment)                 (37%)
#
# Calculate:
# 1. Break-even month for 1yr All Upfront Standard RI:
#    $510 / ($69.12/mo) = 7.4 months
#    After 7.4 months, the RI is cheaper than on-demand.
#
# 2. Break-even month for 3yr All Upfront Standard RI:
#    $968 / ($69.12/mo) = 14 months
#    After 14 months, the RI is cheaper. Remaining 22 months = pure savings.
#
# 3. Total savings over 3 years:
#    On-demand: $829.44 * 3 = $2,488.32
#    3yr RI:    $968.00
#    Savings:   $1,520.32 (61%)
#
# Document these calculations in your notes.
# ============================================================


# ============================================================
# TODO 3: Build a Decision Framework
# ============================================================
# Create a decision tree for choosing the right pricing model:
#
# Is the workload stateless and fault-tolerant?
# +-- Yes: Can it tolerate 2-minute interruption?
# |   +-- Yes -> Spot Instances (up to 90% savings)
# |   +-- No -> Continue below
# +-- No: Is usage predictable and consistent?
#     +-- Yes: Will the instance family change?
#     |   +-- Yes -> Convertible RI or Compute Savings Plan
#     |   +-- No: Will the region change?
#     |       +-- Yes -> Compute Savings Plan
#     |       +-- No -> Standard RI or EC2 Savings Plan
#     +-- No (variable usage):
#         +-- Some baseline -> Savings Plan for baseline + On-Demand for peaks
#         +-- Fully variable -> On-Demand
#
# Implement this as a comment block or output in your Terraform.
# ============================================================
```

### `outputs.tf`

```hcl
output "pricing_decision_framework" {
  value = <<-EOT
    EC2 Pricing Decision Framework:
    ================================
    1. Fault-tolerant + interruptible -> Spot (90% savings)
    2. Steady-state + known family + known region -> Standard RI (72%)
    3. Steady-state + may change family -> Convertible RI (66%)
    4. Steady-state + known family -> EC2 Savings Plan (72%)
    5. Multi-service (EC2 + Fargate + Lambda) -> Compute Savings Plan (66%)
    6. Variable/unpredictable -> On-Demand (0% savings)
  EOT
}
```

---

## Spot the Bug

A company purchases Standard Reserved Instances for their m5.large instances in `us-east-1a`, but their RI utilization report shows only 60% utilization:

```
Purchase:
  - 10x Standard RI, m5.large, us-east-1a, Linux/UNIX
  - 1-year, All Upfront, $5,100

Current running instances:
  - 6x m5.large in us-east-1a (covered by RI)
  - 4x m5.large in us-east-1b (NOT covered by RI)
  - 2x m5.xlarge in us-east-1a (NOT covered by RI)
```

<details>
<summary>Explain the bug</summary>

**Standard RIs are scoped by AZ or Region, not both.** The company purchased AZ-scoped RIs (pinned to `us-east-1a`). This means:

1. The 4 m5.large instances in `us-east-1b` are NOT covered -- AZ-scoped RIs only apply to their specific AZ. The company is paying on-demand for these instances.
2. The 2 m5.xlarge instances in `us-east-1a` are NOT covered -- Standard RIs are locked to the purchased instance size. (Regional-scoped RIs can apply to different sizes within the same family using instance size flexibility, but AZ-scoped cannot.)
3. Only 6 of the 10 purchased RIs are being used, resulting in 60% utilization. The remaining 4 RIs are wasted -- the company is paying for capacity they do not use.

**Two fixes:**

**Fix 1: Use Regional-scoped RIs.** Regional RIs apply across all AZs in the region and support instance size flexibility. The 10 m5.large RIs would cover:
- 6x m5.large in us-east-1a
- 4x m5.large in us-east-1b

The m5.xlarge instances would consume 2 m5.large equivalents each (size normalization), so if you had enough RIs, they would be partially covered too.

**Fix 2: Use Compute Savings Plans instead.** Savings Plans are automatically region-scoped and size-flexible. A Compute Savings Plan would apply across any instance family, size, AZ, OS, and tenancy -- providing maximum flexibility.

```
Recommendation:
- Convert to Regional-scoped Standard RIs (same discount, all-AZ coverage)
- Or switch to Compute Savings Plans (slightly lower discount but fully flexible)
```

For the SAA-C03 exam: when a question mentions RI coverage issues across AZs, the answer is almost always "use Regional-scoped RIs" or "use Savings Plans."

</details>

---

## Pricing Model Comparison

| Feature | On-Demand | Spot | Standard RI | Convertible RI | EC2 Savings Plan | Compute Savings Plan |
|---------|-----------|------|-------------|----------------|-----------------|---------------------|
| **Discount** | 0% | Up to 90% | Up to 72% | Up to 66% | Up to 72% | Up to 66% |
| **Commitment** | None | None | 1 or 3 year | 1 or 3 year | 1 or 3 year | 1 or 3 year |
| **Instance family** | Any | Any | Locked | Exchangeable | Locked | Any |
| **Region** | Any | Any | Locked (AZ or Regional) | Locked | Locked | Any |
| **Size flexibility** | N/A | N/A | Regional only | Yes | Yes | Yes |
| **Interruption risk** | None | 2-min notice | None | None | None | None |
| **Covers Fargate** | N/A | N/A | No | No | No | Yes |
| **Covers Lambda** | N/A | N/A | No | No | No | Yes |
| **Marketplace resale** | N/A | N/A | Yes | No | No | No |
| **Best for** | Unpredictable | Fault-tolerant | Stable, known family | May change family | Stable family | Multi-service |

### Break-Even Visualization (m5.large, 1-year)

```
Month:  1   2   3   4   5   6   7   8   9  10  11  12
        |---|---|---|---|---|---|---|---|---|---|---|---|
OD:     $69  138  207  276  345  414  483  552  621  690  759  829
RI:     $510 510  510  510  510  510  510  510  510  510  510  510
        ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^ RI ahead after month 7.4
```

After month 7.4, the cumulative on-demand cost exceeds the RI cost. Every subsequent month saves $69.12 over on-demand.

---

## Solutions

<details>
<summary>TODO 1 -- Cost Explorer Analysis Commands</summary>

```bash
# Get EC2 cost by instance type for last 3 months
aws ce get-cost-and-usage \
  --time-period Start=2025-12-01,End=2026-03-01 \
  --granularity MONTHLY \
  --metrics BlendedCost \
  --group-by Type=DIMENSION,Key=INSTANCE_TYPE \
  --filter '{"Dimensions":{"Key":"SERVICE","Values":["Amazon Elastic Compute Cloud - Compute"]}}' \
  --query 'ResultsByTime[*].{Month:TimePeriod.Start,Groups:Groups[*].{Type:Keys[0],Cost:Metrics.BlendedCost.Amount}}' \
  --output json

# Get RI purchase recommendations
aws ce get-reservation-purchase-recommendation \
  --service "Amazon Elastic Compute Cloud - Compute" \
  --term-in-years ONE_YEAR \
  --payment-option ALL_UPFRONT \
  --lookback-period-in-days THIRTY_DAYS \
  --query 'Recommendations[0].RecommendationDetails[*].{Type:InstanceDetails.EC2InstanceDetails.InstanceType,Region:InstanceDetails.EC2InstanceDetails.Region,MonthlySavings:EstimatedMonthlySavingsAmount,UpfrontCost:UpfrontCost}' \
  --output table

# Get Savings Plans recommendations
aws ce get-savings-plans-purchase-recommendation \
  --savings-plans-type COMPUTE_SP \
  --term-in-years ONE_YEAR \
  --payment-option ALL_UPFRONT \
  --lookback-period-in-days THIRTY_DAYS
```

Key patterns to look for:
- Instances running 24/7: strong RI/SP candidates (break-even at ~7 months)
- Instances running 12-16 hrs/day: consider RI with Partial Upfront
- Instances running <8 hrs/day: likely not worth committing; use on-demand
- Multiple instance families: Compute Savings Plan provides flexibility

</details>

<details>
<summary>TODO 2 -- Break-Even Calculations</summary>

```bash
# Break-even calculator (bash)
# Inputs
ON_DEMAND_HOURLY=0.096
RI_UPFRONT=510
RI_HOURLY=0  # All Upfront means $0/hr after upfront payment
HOURS_PER_MONTH=730

# Monthly costs
OD_MONTHLY=$(echo "$ON_DEMAND_HOURLY * $HOURS_PER_MONTH" | bc)
RI_MONTHLY=$(echo "$RI_HOURLY * $HOURS_PER_MONTH" | bc)

# Break-even month
BREAK_EVEN=$(echo "scale=1; $RI_UPFRONT / ($OD_MONTHLY - $RI_MONTHLY)" | bc)

echo "On-Demand monthly: \$$OD_MONTHLY"
echo "RI monthly (after upfront): \$$RI_MONTHLY"
echo "Break-even at month: $BREAK_EVEN"
echo ""

# Cumulative comparison
for month in 1 3 6 9 12; do
  OD_TOTAL=$(echo "$OD_MONTHLY * $month" | bc)
  RI_TOTAL=$(echo "$RI_UPFRONT + $RI_MONTHLY * $month" | bc)
  SAVED=$(echo "$OD_TOTAL - $RI_TOTAL" | bc)
  echo "Month $month: OD=\$$OD_TOTAL  RI=\$$RI_TOTAL  Saved=\$$SAVED"
done
```

Expected output:
```
On-Demand monthly: $70.08
RI monthly (after upfront): $0
Break-even at month: 7.2

Month 1:  OD=$70.08   RI=$510.00  Saved=-$439.92
Month 3:  OD=$210.24  RI=$510.00  Saved=-$299.76
Month 6:  OD=$420.48  RI=$510.00  Saved=-$89.52
Month 9:  OD=$630.72  RI=$510.00  Saved=$120.72
Month 12: OD=$840.96  RI=$510.00  Saved=$330.96
```

</details>

<details>
<summary>TODO 3 -- Decision Framework Output -- `outputs.tf`</summary>

```hcl
output "pricing_decision_framework" {
  value = <<-EOT
    EC2 Pricing Decision Framework:
    ================================
    1. Fault-tolerant + interruptible -> Spot (90% savings)
    2. Steady-state + known family + known region -> Standard RI (72%)
    3. Steady-state + may change family -> Convertible RI (66%)
    4. Steady-state + known family -> EC2 Savings Plan (72%)
    5. Multi-service (EC2 + Fargate + Lambda) -> Compute Savings Plan (66%)
    6. Variable/unpredictable -> On-Demand (0% savings)

    Recommended production mix:
    - 60-70% of baseline: Savings Plans or RIs
    - 20-30% of peak: Spot instances (ASG mixed policy)
    - 10% buffer: On-Demand (handles unexpected spikes)
  EOT
}
```

</details>

---

## Verify What You Learned

```bash
# Verify you can access Cost Explorer
aws ce get-cost-and-usage \
  --time-period Start=2026-02-01,End=2026-03-01 \
  --granularity MONTHLY \
  --metrics BlendedCost \
  --query 'ResultsByTime[0].Total.BlendedCost.Amount' \
  --output text
```

Expected: a dollar amount (your account's EC2 spend for last month).

```bash
# List any existing Reserved Instances
aws ec2 describe-reserved-instances \
  --filters "Name=state,Values=active" \
  --query 'ReservedInstances[*].{Type:InstanceType,Count:InstanceCount,Duration:Duration,State:State}' \
  --output table
```

Expected: empty table (sandbox accounts typically have no active RIs) or existing RI details.

```bash
# Check Savings Plans coverage
aws savingsplans describe-savings-plans \
  --query 'SavingsPlans[?State==`active`].{Type:SavingsPlansType,Commitment:Commitment,Term:TermDurationInSeconds}' \
  --output table
```

Expected: empty or existing plans.

---

## Cleanup

If you created demonstration instances:

```bash
terraform destroy -auto-approve
```

This exercise is primarily analysis-based, so there may be nothing to destroy.

---

## What's Next

You analyzed the full EC2 pricing portfolio and built a decision framework for cost optimization. In the next exercise, you will compare **EC2 Instance Store vs EBS** -- understanding the trade-offs between ephemeral high-IOPS local storage and persistent network-attached storage for different database and application workloads.

---

## Summary

- **Standard RIs** lock instance family, region, and OS for the deepest discount (up to 72% with 3-year All Upfront)
- **Convertible RIs** allow exchanging instance family/OS but offer lower discount (up to 66%)
- **EC2 Savings Plans** commit to a $/hr spend for a specific instance family in a region (up to 72%)
- **Compute Savings Plans** commit to a $/hr spend across any instance family, region, OS, and even Fargate/Lambda (up to 66%)
- **Break-even** for 1-year All Upfront RI is approximately 7-8 months -- if the workload runs less than that, on-demand is cheaper
- AZ-scoped RIs only cover instances in that specific AZ; **Regional-scoped RIs** cover all AZs with instance size flexibility
- Standard RIs can be **sold on the RI Marketplace**; Convertible RIs and Savings Plans cannot
- The optimal production mix is typically **60-70% committed** (RI/SP) + **20-30% Spot** + **10% on-demand** buffer
- Compute Savings Plans are the **most flexible** option and cover EC2, Fargate, and Lambda

---

## Reference

- [Amazon EC2 Reserved Instances](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-reserved-instances.html)
- [AWS Savings Plans](https://docs.aws.amazon.com/savingsplans/latest/userguide/what-is-savings-plans.html)
- [Cost Explorer RI Recommendations](https://docs.aws.amazon.com/cost-management/latest/userguide/ce-ris.html)
- [RI Marketplace](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ri-market-general.html)

## Additional Resources

- [Savings Plans vs Reserved Instances](https://docs.aws.amazon.com/savingsplans/latest/userguide/sp-applying.html) -- how Savings Plans apply to usage and interact with existing RIs
- [EC2 Instance Size Flexibility](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/apply_ri.html#apply_ri_size_flexibility) -- how Regional RIs apply across sizes within a family using normalization factors
- [AWS Pricing Calculator](https://calculator.aws/) -- model different pricing scenarios for your specific workload
- [Cost Optimization Pillar](https://docs.aws.amazon.com/wellarchitected/latest/cost-optimization-pillar/welcome.html) -- Well-Architected Framework guidance on commitment-based pricing

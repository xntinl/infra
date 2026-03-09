# 33. EC2 Spot Instances and Fleet Strategies

<!--
difficulty: intermediate
concepts: [spot-instances, spot-fleet, capacity-optimized, lowest-price, diversified, spot-interruption, mixed-instances-policy, on-demand-base]
tools: [terraform, aws-cli]
estimated_time: 50m
bloom_level: apply, evaluate
prerequisites: [31-ec2-instance-types-right-sizing]
aws_cost: ~$0.02/hr
-->

> **AWS Cost Warning:** Spot instances cost up to 90% less than on-demand. A diversified fleet of t3.micro/t3.small Spot instances costs ~$0.002-0.005/hr each. Total for this exercise ~$0.02/hr. Spot instances can be terminated by AWS with 2 minutes notice. Remember to run `terraform destroy` when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Default VPC available in target region | `aws ec2 describe-vpcs --filters Name=isDefault,Values=true` |
| Completed exercise 31 (instance types) | Understanding of EC2 instance families |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Explain** how Spot pricing works (unused capacity, 2-minute interruption notice, up to 90% discount).
2. **Compare** the three Spot Fleet allocation strategies (lowestPrice, diversified, capacityOptimized) and select the appropriate one for a given workload.
3. **Implement** a Spot Fleet request with multiple instance type overrides using Terraform.
4. **Design** an ASG mixed instances policy that combines on-demand base capacity with Spot instances for cost optimization.
5. **Evaluate** the trade-offs between Spot savings and interruption risk for stateless vs stateful workloads.

---

## Why This Matters

Spot instances represent the largest cost optimization opportunity on EC2 -- up to 90% off on-demand pricing for the same hardware. The SAA-C03 exam tests whether you understand when Spot is appropriate (stateless, fault-tolerant workloads) and when it is dangerous (databases, single-instance applications). But the exam goes deeper than "use Spot for batch jobs." It asks about fleet allocation strategies: should you pick the cheapest instance type (lowestPrice), spread across pools for resilience (diversified), or let AWS choose the pool least likely to be interrupted (capacityOptimized)?

The real-world architect challenge is building fleets that survive interruptions gracefully. A Spot Fleet with a single instance type in a single AZ has a high interruption rate because you are competing for a narrow capacity pool. A diversified fleet across 6+ instance types and 3 AZs has dramatically lower interruption rates because AWS can draw from many pools. The capacityOptimized strategy goes further -- it actively avoids pools with high reclaim probability. Understanding these strategies is the difference between Spot savings that work and Spot savings that cause outages.

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
  default     = "saa-ex33"
}
```

### `main.tf`

```hcl
data "aws_vpc" "default" {
  default = true
}

data "aws_subnets" "default" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.default.id]
  }
  filter {
    name   = "default-for-az"
    values = ["true"]
  }
}

data "aws_ami" "al2023" {
  most_recent = true
  owners      = ["amazon"]

  filter {
    name   = "name"
    values = ["al2023-ami-*-x86_64"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}
```

### `security.tf`

```hcl
resource "aws_security_group" "instance" {
  name_prefix = "${var.project_name}-"
  vpc_id      = data.aws_vpc.default.id
  description = "Spot fleet demo"

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}
```

### `compute.tf`

```hcl
# ============================================================
# TODO 1: IAM Role for Spot Fleet
# ============================================================
# Spot Fleet needs an IAM role that allows it to request,
# launch, and terminate instances on your behalf.
#
# Requirements:
#   - Create aws_iam_role with:
#     - name = "${var.project_name}-spot-fleet-role"
#     - assume_role_policy allowing spotfleet.amazonaws.com
#   - Attach the managed policy:
#     - arn:aws:iam::aws:policy/service-role/AmazonEC2SpotFleetTaggingRole
#
# This role is required for aws_spot_fleet_request. Without it,
# Terraform apply fails with "InvalidSpotFleetRequestConfig".
#
# Docs: https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/spot-fleet-requests.html#spot-fleet-prerequisites
# ============================================================


# ============================================================
# TODO 2: Spot Fleet Request with Diversified Strategy
# ============================================================
# Create a Spot Fleet that distributes across multiple instance
# types and AZs to minimize interruption probability.
#
# Requirements:
#   - Resource: aws_spot_fleet_request
#   - iam_fleet_role = role ARN from TODO 1
#   - allocation_strategy = "diversified"
#   - target_capacity = 3
#   - terminate_instances_with_expiration = true
#   - Add at least 3 launch_specification blocks:
#     - t3.micro in subnet [0]
#     - t3.small in subnet [1]
#     - t3.micro in subnet [2] (if 3 subnets available)
#   - Each launch_specification needs:
#     - ami, instance_type, subnet_id, vpc_security_group_ids
#     - tags with Name = "${var.project_name}-spot-{type}"
#
# The diversified strategy distributes instances evenly across
# all specified pools (instance type + AZ combinations). If one
# pool is reclaimed, only ~1/N of your fleet is interrupted.
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/spot_fleet_request
# ============================================================


# ============================================================
# TODO 3: ASG with Mixed Instances Policy
# ============================================================
# Create an ASG that uses a mix of on-demand and Spot instances.
# The on-demand base guarantees minimum capacity that cannot be
# interrupted.
#
# Requirements:
#   - Resource: aws_launch_template
#     - name_prefix = "${var.project_name}-mixed-lt-"
#     - image_id = AMI
#     - vpc_security_group_ids
#
#   - Resource: aws_autoscaling_group
#     - name = "${var.project_name}-mixed-asg"
#     - min_size = 2, max_size = 6, desired_capacity = 4
#     - vpc_zone_identifier = default subnet ids
#
#     - mixed_instances_policy block:
#       - instances_distribution:
#         - on_demand_base_capacity = 2
#           (2 instances are always on-demand, never interrupted)
#         - on_demand_percentage_above_base_capacity = 0
#           (all additional instances beyond base 2 are Spot)
#         - spot_allocation_strategy = "capacity-optimized"
#           (AWS picks the Spot pool least likely to be interrupted)
#
#       - launch_template:
#         - launch_template_specification:
#           - launch_template_id = template id
#           - version = "$Latest"
#         - override blocks for multiple instance types:
#           - { instance_type = "t3.micro" }
#           - { instance_type = "t3.small" }
#           - { instance_type = "t3a.micro" }
#           - { instance_type = "t3a.small" }
#
# Key concept: on_demand_base_capacity ensures your application
# always has at least N non-interruptible instances. The Spot
# instances provide cost-efficient scaling beyond that base.
#
# Docs: https://docs.aws.amazon.com/autoscaling/ec2/userguide/ec2-auto-scaling-mixed-instances-groups.html
# ============================================================
```

### `outputs.tf`

```hcl
output "security_group_id" {
  value = aws_security_group.instance.id
}
```

---

## Spot the Bug

A team configures a mixed instances policy for their production API but experiences full outages during Spot reclamation events:

```hcl
resource "aws_autoscaling_group" "api" {
  name                = "production-api"
  min_size            = 4
  max_size            = 12
  desired_capacity    = 6
  vpc_zone_identifier = data.aws_subnets.default.ids

  mixed_instances_policy {
    instances_distribution {
      on_demand_base_capacity                  = 0
      on_demand_percentage_above_base_capacity = 0
      spot_allocation_strategy                 = "lowest-price"
      spot_instance_pools                      = 1
    }

    launch_template {
      launch_template_specification {
        launch_template_id = aws_launch_template.api.id
        version            = "$Latest"
      }

      override {
        instance_type = "c5.xlarge"
      }
    }
  }
}
```

<details>
<summary>Explain the bug</summary>

Three compounding problems make this configuration fragile:

**1. `on_demand_base_capacity = 0` with no on-demand percentage.**
All 6 instances are Spot. If AWS reclaims the Spot capacity, the entire fleet goes to zero. For a production API, you need a non-interruptible baseline.

**2. `spot_allocation_strategy = "lowest-price"` with `spot_instance_pools = 1`.**
This tells AWS to put ALL Spot instances in the single cheapest pool. When that pool is reclaimed (which happens because it is the cheapest and most popular), all Spot instances are interrupted simultaneously. The `lowest-price` strategy is deprecated in favor of `price-capacity-optimized`.

**3. Only one instance type override (`c5.xlarge`).**
With a single instance type, there is only one Spot pool per AZ. If c5.xlarge Spot capacity is reclaimed in your AZ, you lose everything. Diversifying across 4-6 instance types creates multiple pools, reducing the chance of simultaneous interruption.

**The fix:**

```hcl
resource "aws_autoscaling_group" "api" {
  name                = "production-api"
  min_size            = 4
  max_size            = 12
  desired_capacity    = 6
  vpc_zone_identifier = data.aws_subnets.default.ids

  mixed_instances_policy {
    instances_distribution {
      on_demand_base_capacity                  = 2     # 2 guaranteed instances
      on_demand_percentage_above_base_capacity = 25    # 25% of remaining are on-demand
      spot_allocation_strategy                 = "price-capacity-optimized"
    }

    launch_template {
      launch_template_specification {
        launch_template_id = aws_launch_template.api.id
        version            = "$Latest"
      }

      override {
        instance_type = "c5.xlarge"
      }
      override {
        instance_type = "c5a.xlarge"
      }
      override {
        instance_type = "c5d.xlarge"
      }
      override {
        instance_type = "c6i.xlarge"
      }
    }
  }
}
```

With this configuration: 2 instances are always on-demand, 1 of the remaining 4 is on-demand (25%), and 3 are Spot across 4 different instance types. The `price-capacity-optimized` strategy picks pools that balance price and availability.

</details>

---

## Spot Fleet Allocation Strategy Comparison

| Strategy | How It Works | Interruption Risk | Cost | Best For |
|----------|-------------|-------------------|------|----------|
| **lowestPrice** | All instances from cheapest pool | Highest (popular pool = first reclaimed) | Lowest initial | Short batch jobs (<1hr) |
| **diversified** | Even distribution across all pools | Low (only 1/N affected per reclaim) | Moderate | Long-running fleets |
| **capacityOptimized** | AWS picks pools with most spare capacity | Lowest | Moderate | Stateful workloads |
| **priceCapacityOptimized** | Balances price and capacity depth | Low | Low-moderate | General purpose (recommended) |

### Interruption Handling Checklist

| Signal | How to Detect | Response Time |
|--------|--------------|---------------|
| Spot interruption notice | Instance metadata `http://169.254.169.254/latest/meta-data/spot/instance-action` | 2 minutes |
| EC2 Rebalance Recommendation | EventBridge event `EC2 Instance Rebalance Recommendation` | Variable (early warning) |
| CloudWatch Spot interruption metric | `AWS/EC2Spot` namespace | Reactive |

---

## Solutions

<details>
<summary>TODO 1 -- IAM Role for Spot Fleet -- `iam.tf`</summary>

```hcl
resource "aws_iam_role" "spot_fleet" {
  name = "${var.project_name}-spot-fleet-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action    = "sts:AssumeRole"
      Effect    = "Allow"
      Principal = { Service = "spotfleet.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "spot_fleet" {
  role       = aws_iam_role.spot_fleet.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonEC2SpotFleetTaggingRole"
}
```

The `AmazonEC2SpotFleetTaggingRole` policy grants permissions to launch, terminate, and tag instances, as well as describe instance types, subnets, and security groups. A custom policy is possible but rarely necessary.

</details>

<details>
<summary>TODO 2 -- Spot Fleet Request with Diversified Strategy -- `compute.tf`</summary>

```hcl
resource "aws_spot_fleet_request" "diversified" {
  iam_fleet_role                      = aws_iam_role.spot_fleet.arn
  allocation_strategy                 = "diversified"
  target_capacity                     = 3
  terminate_instances_with_expiration = true
  wait_for_fulfillment                = true

  launch_specification {
    ami           = data.aws_ami.al2023.id
    instance_type = "t3.micro"
    subnet_id     = data.aws_subnets.default.ids[0]

    vpc_security_group_ids = [aws_security_group.instance.id]

    tags = {
      Name = "${var.project_name}-spot-t3micro-az0"
    }
  }

  launch_specification {
    ami           = data.aws_ami.al2023.id
    instance_type = "t3.small"
    subnet_id     = length(data.aws_subnets.default.ids) > 1 ? data.aws_subnets.default.ids[1] : data.aws_subnets.default.ids[0]

    vpc_security_group_ids = [aws_security_group.instance.id]

    tags = {
      Name = "${var.project_name}-spot-t3small-az1"
    }
  }

  launch_specification {
    ami           = data.aws_ami.al2023.id
    instance_type = "t3.micro"
    subnet_id     = length(data.aws_subnets.default.ids) > 2 ? data.aws_subnets.default.ids[2] : data.aws_subnets.default.ids[0]

    vpc_security_group_ids = [aws_security_group.instance.id]

    tags = {
      Name = "${var.project_name}-spot-t3micro-az2"
    }
  }

  tags = {
    Name = "${var.project_name}-diversified-fleet"
  }
}
```

With `diversified`, the 3 instances are spread evenly across the 3 launch specifications (one per pool). If t3.micro Spot capacity in AZ-0 is reclaimed, only 1 of 3 instances is lost.

</details>

<details>
<summary>TODO 3 -- ASG with Mixed Instances Policy -- `compute.tf`</summary>

```hcl
resource "aws_launch_template" "mixed" {
  name_prefix   = "${var.project_name}-mixed-lt-"
  image_id      = data.aws_ami.al2023.id

  vpc_security_group_ids = [aws_security_group.instance.id]

  tag_specifications {
    resource_type = "instance"
    tags = {
      Name = "${var.project_name}-mixed-instance"
    }
  }
}

resource "aws_autoscaling_group" "mixed" {
  name                = "${var.project_name}-mixed-asg"
  min_size            = 2
  max_size            = 6
  desired_capacity    = 4
  vpc_zone_identifier = data.aws_subnets.default.ids

  mixed_instances_policy {
    instances_distribution {
      on_demand_base_capacity                  = 2
      on_demand_percentage_above_base_capacity = 0
      spot_allocation_strategy                 = "capacity-optimized"
    }

    launch_template {
      launch_template_specification {
        launch_template_id = aws_launch_template.mixed.id
        version            = "$Latest"
      }

      override {
        instance_type = "t3.micro"
      }

      override {
        instance_type = "t3.small"
      }

      override {
        instance_type = "t3a.micro"
      }

      override {
        instance_type = "t3a.small"
      }
    }
  }

  tag {
    key                 = "Name"
    value               = "${var.project_name}-mixed"
    propagate_at_launch = true
  }
}
```

This configuration guarantees 2 on-demand instances (never interrupted) and adds 2 Spot instances from whichever pool has the most available capacity. With 4 instance type overrides across 3 AZs, the ASG has 12 Spot pools to draw from.

</details>

---

## Verify What You Learned

1. **Deploy the infrastructure:**
   ```bash
   terraform init && terraform apply -auto-approve
   ```

2. **Check Spot Fleet fulfillment:**
   ```bash
   aws ec2 describe-spot-fleet-requests \
     --query 'SpotFleetRequestConfigs[?SpotFleetRequestState==`active`].{ID:SpotFleetRequestId,Capacity:SpotFleetRequestConfig.TargetCapacity,Strategy:SpotFleetRequestConfig.AllocationStrategy,Fulfilled:SpotFleetRequestConfig.FulfilledCapacity}' \
     --output table
   ```

3. **Check Spot instance pricing vs on-demand:**
   ```bash
   aws ec2 describe-spot-price-history \
     --instance-types t3.micro t3.small \
     --product-descriptions "Linux/UNIX" \
     --start-time $(date -u '+%Y-%m-%dT%H:%M:%S') \
     --query 'SpotPriceHistory[*].{Type:InstanceType,AZ:AvailabilityZone,Price:SpotPrice}' \
     --output table
   ```

4. **Check ASG mixed instances:**
   ```bash
   aws autoscaling describe-auto-scaling-groups \
     --auto-scaling-group-names saa-ex33-mixed-asg \
     --query 'AutoScalingGroups[0].Instances[*].{ID:InstanceId,Type:InstanceType,Lifecycle:LifecycleState,AZ:AvailabilityZone}' \
     --output table
   ```

5. **Verify on-demand base capacity:**
   ```bash
   aws autoscaling describe-auto-scaling-groups \
     --auto-scaling-group-names saa-ex33-mixed-asg \
     --query 'AutoScalingGroups[0].MixedInstancesPolicy.InstancesDistribution' \
     --output json
   ```

   Expected: `OnDemandBaseCapacity: 2`

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify Spot Fleet is cancelled:

```bash
aws ec2 describe-spot-fleet-requests \
  --query 'SpotFleetRequestConfigs[?SpotFleetRequestState!=`cancelled_running` && SpotFleetRequestState!=`cancelled_terminating`].SpotFleetRequestId' \
  --output text
```

Expected: no output or only unrelated fleet IDs.

---

## What's Next

You built Spot fleets with diversified and capacity-optimized strategies. In the next exercise, you will analyze **Reserved Instances and Savings Plans** -- the committed-use pricing models that complement Spot for predictable baseline workloads, typically saving 30-72% compared to on-demand.

---

## Summary

- **Spot instances** provide up to 90% discount on on-demand pricing by using AWS's unused EC2 capacity
- AWS can **reclaim Spot instances with 2 minutes notice** -- applications must be fault-tolerant and stateless
- **lowestPrice** strategy concentrates instances in one pool (high interruption risk); **diversified** spreads across pools; **capacityOptimized** picks the pool least likely to be interrupted
- **Mixed instances policy** in ASG combines on-demand base capacity (guaranteed) with Spot instances (cost savings)
- Set `on_demand_base_capacity >= 2` for production workloads to survive Spot reclamation without downtime
- Use **4-6 instance type overrides** to maximize the number of Spot pools and reduce simultaneous interruption risk
- **price-capacity-optimized** is the recommended default strategy (replaces lowestPrice)
- Spot is ideal for **batch processing, CI/CD, stateless web tiers, and big data** -- never for databases or single-instance workloads

---

## Reference

- [Amazon EC2 Spot Instances](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/using-spot-instances.html)
- [Spot Fleet Allocation Strategies](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/spot-fleet-allocation-strategy.html)
- [ASG Mixed Instances Policy](https://docs.aws.amazon.com/autoscaling/ec2/userguide/ec2-auto-scaling-mixed-instances-groups.html)
- [Terraform aws_spot_fleet_request](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/spot_fleet_request)

## Additional Resources

- [Spot Instance Advisor](https://aws.amazon.com/ec2/spot/instance-advisor/) -- shows interruption frequency and savings for each instance type
- [EC2 Spot Best Practices](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/spot-best-practices.html) -- instance diversification, capacity-optimized selection
- [Spot Interruption Handling](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/spot-instance-termination-notices.html) -- metadata service, EventBridge events, graceful shutdown patterns
- [Spot Pricing History](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/using-spot-instances-history.html) -- analyzing historical pricing for cost estimation

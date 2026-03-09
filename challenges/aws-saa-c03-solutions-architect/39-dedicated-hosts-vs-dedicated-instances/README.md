# 39. Dedicated Hosts vs Dedicated Instances

<!--
difficulty: intermediate
concepts: [dedicated-hosts, dedicated-instances, byol, socket-core-visibility, hardware-affinity, tenancy, compliance, licensing]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: apply, evaluate
prerequisites: [31-ec2-instance-types-right-sizing]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise is primarily analysis-based. Dedicated Hosts are expensive (~$1.75/hr for m5.large host). Only allocate a Dedicated Host briefly for verification, then release it immediately. Dedicated Instances add a per-region fee of $2/hr. Total ~$0.01/hr if you rely on CLI exploration without long-running resources. Do NOT leave Dedicated Hosts allocated.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Understanding of EC2 instance types | Completed exercise 31 |
| Understanding of EC2 pricing models | Completed exercise 34 |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Differentiate** between Dedicated Instances (hardware isolation, per-instance billing) and Dedicated Hosts (entire physical server, BYOL, socket/core visibility).
2. **Explain** why Bring Your Own License (BYOL) software requires Dedicated Hosts (not Dedicated Instances) due to license binding to physical socket/core counts.
3. **Implement** a Dedicated Host allocation and instance launch using Terraform and AWS CLI.
4. **Compare** the cost models: shared tenancy vs Dedicated Instances vs Dedicated Hosts vs on-demand vs Reserved.
5. **Select** the correct tenancy model for regulatory compliance, licensing, and performance isolation scenarios.

---

## Why This Matters

The SAA-C03 exam tests tenancy models in scenarios involving regulatory compliance, software licensing, and hardware isolation requirements. The critical distinction is between Dedicated Instances and Dedicated Hosts -- they sound similar but serve fundamentally different purposes.

Dedicated Instances guarantee that your instances run on hardware not shared with other AWS accounts. This satisfies compliance requirements that mandate physical isolation (HIPAA, PCI-DSS in certain interpretations). However, you have no visibility into the physical hardware and cannot control which specific server your instance runs on.

Dedicated Hosts give you an entire physical server. You can see the socket and core count, control instance placement on specific hosts, and use per-socket or per-core licensed software (Windows Server, SQL Server, Oracle). This is the BYOL (Bring Your Own License) use case -- if you have existing software licenses tied to physical socket counts, you need Dedicated Hosts to maintain license compliance. Dedicated Instances cannot satisfy this requirement because you do not have visibility into the underlying hardware.

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
  default     = "saa-ex39"
}
```

### `main.tf`

```hcl
data "aws_availability_zones" "available" {
  state = "available"
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

### `compute.tf`

```hcl
# ============================================================
# TODO 1: Allocate a Dedicated Host
# ============================================================
# Allocate a Dedicated Host for the m5 instance family.
# This gives you an entire physical server.
#
# Requirements:
#   - Resource: aws_ec2_host
#     - instance_family = "m5" (or instance_type = "m5.large")
#     - availability_zone = first available AZ
#     - auto_placement = "on"
#       (allows instances with host tenancy to be placed
#        automatically on this host)
#     - tags: Name = "${var.project_name}-dedicated-host"
#
# Key concepts:
#   - You pay for the entire host regardless of how many
#     instances you run on it
#   - An m5 Dedicated Host can fit: 1x m5.24xlarge, or
#     2x m5.12xlarge, or 4x m5.4xlarge, etc. (any combination
#     that fits the host's 48 vCPUs and 384 GiB)
#   - Socket/core visibility: you can see the physical
#     socket count and core count for BYOL compliance
#
# IMPORTANT: Release this host after verification to avoid
# ongoing charges (~$1.75/hr for m5).
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ec2_host
# ============================================================


# ============================================================
# TODO 2: Launch Instance on Dedicated Host
# ============================================================
# Launch an instance that runs on the Dedicated Host.
#
# Requirements:
#   - Resource: aws_instance
#     - ami = AMI for Amazon Linux 2023
#     - instance_type = "m5.large" (fits within m5 host)
#     - host_id = Dedicated Host ID from TODO 1
#     - tenancy = "host"
#     - tags: Name = "${var.project_name}-byol-instance"
#
# When tenancy = "host" and host_id is specified, the instance
# is launched on that specific physical server. You can see
# which host the instance runs on in the console.
#
# Compare this with tenancy = "dedicated" (Dedicated Instance):
#   - Dedicated Instance: AWS guarantees hardware isolation
#     from other accounts but you don't control which host
#   - Dedicated Host: you control the exact physical server
#
# Docs: https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/dedicated-hosts-overview.html
# ============================================================


# ============================================================
# TODO 3: Examine Host Capacity and Socket/Core Information
# ============================================================
# After allocating the host, query its physical details.
#
# CLI commands to run:
#
# 1. Describe the host to see socket/core info:
#    aws ec2 describe-hosts \
#      --host-ids <HOST_ID> \
#      --query 'Hosts[0].{HostId:HostId,Sockets:HostProperties.Sockets,Cores:HostProperties.Cores,TotalvCPUs:HostProperties.TotalVCpus,InstanceFamily:HostProperties.InstanceFamily,AvailableCapacity:AvailableCapacity}'
#
# 2. List instances running on the host:
#    aws ec2 describe-hosts \
#      --host-ids <HOST_ID> \
#      --query 'Hosts[0].Instances[*].{InstanceId:InstanceId,Type:InstanceType}'
#
# This socket/core visibility is what makes BYOL possible.
# Software licenses tied to "2 sockets with 24 cores each"
# need this information to verify compliance.
# ============================================================
```

### `outputs.tf`

```hcl
output "available_azs" {
  value = data.aws_availability_zones.available.names
}
```

---

## Spot the Bug

A company has Windows Server licenses tied to physical socket counts. They configure Dedicated Instances instead of Dedicated Hosts:

```hcl
resource "aws_instance" "windows_byol" {
  ami           = "ami-windows-server-2022"
  instance_type = "m5.xlarge"
  tenancy       = "dedicated"  # Dedicated Instance, NOT Dedicated Host

  tags = {
    Name    = "windows-byol-server"
    License = "BYOL-per-socket"
  }
}
```

The Microsoft license audit flags this as non-compliant.

<details>
<summary>Explain the bug</summary>

**Dedicated Instances (`tenancy = "dedicated"`) do not provide socket/core visibility.** The instance runs on hardware not shared with other AWS accounts, but you cannot see or control which physical server it runs on. You do not know the socket count, core count, or host ID. This means:

1. The Microsoft licensing team cannot verify that the Windows Server license is applied to a specific number of physical sockets.
2. The instance might move to different physical hardware during stop/start cycles, potentially running on a server with more sockets than the license covers.
3. The audit fails because BYOL (Bring Your Own License) per-socket licensing requires proof of the physical hardware configuration.

**The fix: Use a Dedicated Host (`tenancy = "host"`).**

```hcl
resource "aws_ec2_host" "windows" {
  instance_type     = "m5.xlarge"
  availability_zone = "us-east-1a"
  auto_placement    = "on"

  tags = {
    Name    = "windows-byol-host"
    License = "BYOL-per-socket"
  }
}

resource "aws_instance" "windows_byol" {
  ami           = "ami-windows-server-2022"
  instance_type = "m5.xlarge"
  tenancy       = "host"
  host_id       = aws_ec2_host.windows.id

  tags = {
    Name    = "windows-byol-server"
    License = "BYOL-per-socket"
  }
}
```

With a Dedicated Host:
- `aws ec2 describe-hosts` shows Sockets=2, Cores=24 (for m5)
- The instance is bound to a specific host ID for audit purposes
- License compliance is provable: "Windows Server BYOL license applied to host h-0abc123, 2 sockets, 24 cores"

**SAA-C03 exam rule:** If the question mentions BYOL, per-socket licensing, or per-core licensing, the answer is always **Dedicated Host** -- never Dedicated Instance.

</details>

---

## Tenancy Model Comparison

| Feature | Shared (default) | Dedicated Instance | Dedicated Host |
|---------|-------------------|-------------------|----------------|
| **Hardware sharing** | Shared with other accounts | Isolated from other accounts | Entire server is yours |
| **Socket/core visibility** | No | No | Yes |
| **BYOL support** | No | No | Yes |
| **Instance placement control** | No | No | Yes (host affinity) |
| **Per-region fee** | None | $2/hr | None |
| **Per-instance pricing** | On-demand rates | ~2% premium | Included in host price |
| **Host-level pricing** | N/A | N/A | ~$1.75/hr (m5) |
| **Capacity reservation** | No | No | Yes (host = reserved capacity) |
| **RI support** | Yes | Yes | Yes (Host RIs) |
| **Use case** | Most workloads | Compliance isolation | BYOL, compliance, capacity |

### Cost Comparison (m5.large in us-east-1)

| Model | Hourly Cost | Monthly Cost | Notes |
|-------|------------|-------------|-------|
| Shared On-Demand | $0.096 | $69.12 | Default, cheapest |
| Dedicated Instance | $0.098 + $2/hr region fee | $70.56 + $1,440 | First DI in region adds $2/hr |
| Dedicated Host (m5) | ~$1.75/hr for entire host | ~$1,260 | Fits up to 22x m5.large |
| Dedicated Host per-instance | ~$0.08/hr (if 22 instances) | ~$57.27 | Cheaper than shared if host is full |
| Host RI (1yr, all upfront) | N/A | ~$820/mo | 35% savings on host price |

**Key insight:** A Dedicated Host is expensive for a single instance but cost-effective when fully packed. An m5 Dedicated Host costs ~$1,260/month and fits 22x m5.large instances. Per-instance cost: $57.27/month -- cheaper than on-demand shared tenancy ($69.12/month). The break-even is approximately 13 instances on a single host.

---

## Solutions

<details>
<summary>TODO 1 -- Allocate a Dedicated Host -- `compute.tf`</summary>

```hcl
resource "aws_ec2_host" "this" {
  instance_family   = "m5"
  availability_zone = data.aws_availability_zones.available.names[0]
  auto_placement    = "on"

  tags = {
    Name = "${var.project_name}-dedicated-host"
  }
}

output "host_id" {
  value = aws_ec2_host.this.id
}
```

Note: `instance_family = "m5"` allows any m5 size on this host. Alternatively, `instance_type = "m5.large"` restricts the host to only m5.large instances. Using `instance_family` provides flexibility to mix sizes (e.g., some m5.large and some m5.xlarge).

</details>

<details>
<summary>TODO 2 -- Launch Instance on Dedicated Host -- `compute.tf`</summary>

```hcl
resource "aws_instance" "byol" {
  ami           = data.aws_ami.al2023.id
  instance_type = "m5.large"
  tenancy       = "host"
  host_id       = aws_ec2_host.this.id

  tags = {
    Name = "${var.project_name}-byol-instance"
  }
}

output "byol_instance_id" {
  value = aws_instance.byol.id
}
```

With `tenancy = "host"` and explicit `host_id`, the instance is pinned to that specific physical server. On stop/start, it returns to the same host (host affinity).

</details>

<details>
<summary>TODO 3 -- Examine Host Details</summary>

```bash
HOST_ID=$(terraform output -raw host_id)

# Physical server details
aws ec2 describe-hosts \
  --host-ids $HOST_ID \
  --query 'Hosts[0].{HostId:HostId,State:State,Sockets:HostProperties.Sockets,Cores:HostProperties.Cores,TotalvCPUs:HostProperties.TotalVCpus,InstanceFamily:HostProperties.InstanceFamily}' \
  --output table

# Expected for m5: Sockets=2, Cores=24, TotalvCPUs=48

# Instances running on the host
aws ec2 describe-hosts \
  --host-ids $HOST_ID \
  --query 'Hosts[0].Instances[*].{InstanceId:InstanceId,Type:InstanceType}' \
  --output table

# Available capacity remaining
aws ec2 describe-hosts \
  --host-ids $HOST_ID \
  --query 'Hosts[0].AvailableCapacity.AvailableInstanceCapacity[*].{Type:InstanceType,Available:AvailableCapacity,Total:TotalCapacity}' \
  --output table
```

The `AvailableCapacity` output shows exactly how many more instances of each size can fit on the host.

</details>

---

## Verify What You Learned

```bash
# Verify Dedicated Host is allocated
aws ec2 describe-hosts \
  --filter "Name=tag:Name,Values=saa-ex39-dedicated-host" \
  --query "Hosts[0].State" --output text
```

Expected: `available`

```bash
# Verify instance is running on the host
aws ec2 describe-instances \
  --filters "Name=tag:Name,Values=saa-ex39-byol-instance" "Name=instance-state-name,Values=running" \
  --query "Reservations[0].Instances[0].{Tenancy:Placement.Tenancy,HostId:Placement.HostId}" --output table
```

Expected: Tenancy=host, HostId matches the allocated host.

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

---

## Cleanup

Release the Dedicated Host and terminate instances immediately to avoid ongoing charges:

```bash
terraform destroy -auto-approve
```

Verify the host is released:

```bash
aws ec2 describe-hosts \
  --filter "Name=tag:Name,Values=saa-ex39-dedicated-host" \
  --query "Hosts[?State!='released'].HostId" --output text
```

Expected: no output (host released).

---

## What's Next

You explored the differences between Dedicated Instances and Dedicated Hosts. In the next exercise, you will configure **EC2 Auto Recovery and Status Checks** -- setting up CloudWatch alarms that automatically recover instances from underlying hardware failures.

---

## Summary

- **Dedicated Instances** provide hardware isolation from other accounts but no visibility into physical hardware
- **Dedicated Hosts** give you an entire physical server with **socket/core visibility** for BYOL compliance
- **BYOL licensing** (per-socket, per-core) requires Dedicated Hosts -- Dedicated Instances cannot satisfy this requirement
- Dedicated Hosts are cost-effective when fully packed (break-even at ~60% utilization for m5)
- A Dedicated Host has a fixed capacity based on instance family (e.g., m5 host = 48 vCPUs, 384 GiB)
- **Auto-placement** allows instances with `tenancy = "host"` to be placed on any available host in the family
- Dedicated Instances add a **$2/hr per-region fee** (charged once regardless of how many DIs run)
- For pure compliance isolation (no BYOL), Dedicated Instances are simpler and often cheaper than Dedicated Hosts
- **Host RIs** provide 35-40% savings on Dedicated Host pricing for committed workloads

---

## Reference

- [Amazon EC2 Dedicated Hosts](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/dedicated-hosts-overview.html)
- [Amazon EC2 Dedicated Instances](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/dedicated-instance.html)
- [Terraform aws_ec2_host](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ec2_host)
- [BYOL on AWS](https://aws.amazon.com/windows/resources/licensing/)

## Additional Resources

- [Dedicated Host Pricing](https://aws.amazon.com/ec2/dedicated-hosts/pricing/) -- per-family host pricing and RI options
- [AWS License Manager](https://docs.aws.amazon.com/license-manager/latest/userguide/license-manager.html) -- track and manage software licenses across Dedicated Hosts
- [Host Recovery](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/dedicated-hosts-recovery.html) -- automatic instance migration on host failure
- [Dedicated Host Capacity](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/dedicated-hosts-overview.html#dedicated-hosts-allocating) -- how many instances fit on each host type

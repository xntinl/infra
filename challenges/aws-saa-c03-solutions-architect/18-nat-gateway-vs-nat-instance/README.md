# 18. NAT Gateway vs NAT Instance

<!--
difficulty: basic
concepts: [nat-gateway, nat-instance, source-dest-check, elastic-ip, private-subnet-outbound, high-availability, cost-optimization]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: understand
prerequisites: [17-vpc-subnets-route-tables-igw]
aws_cost: ~$0.05/hr
-->

> **AWS Cost Warning:** NAT Gateway costs ~$0.045/hr plus $0.045/GB processed. NAT Instance (t3.micro) costs ~$0.0104/hr. Elastic IPs attached to running instances are free; unattached EIPs cost $0.005/hr. Total ~$0.05/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 17 or equivalent understanding of VPC, subnets, and route tables

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** why private subnets need a NAT device for outbound internet access and why inbound traffic is blocked
- **Compare** NAT Gateway (managed, HA per AZ, ~$0.045/hr) with NAT Instance (EC2, self-managed, cheaper, single point of failure)
- **Construct** both a NAT Gateway and a NAT Instance using Terraform and verify outbound connectivity
- **Identify** the source/destination check setting on NAT Instances and why disabling it is required
- **Evaluate** cost trade-offs: NAT Gateway is ~4x more expensive per hour but requires zero maintenance
- **Design** a multi-AZ NAT architecture that balances availability and cost

## Why NAT Architecture Matters

Private subnets protect backend resources from direct internet access, but those resources often need outbound connectivity: downloading patches, calling external APIs, pulling container images. A NAT (Network Address Translation) device sits in a public subnet and translates outbound traffic from private instances, replacing the private source IP with the NAT device's public IP. Return traffic is forwarded back to the original private instance. Inbound connections initiated from the internet are blocked -- this is the one-way door that makes NAT valuable for security.

The SAA-C03 exam tests two dimensions of this decision. First, availability: NAT Gateway is managed by AWS and is redundant within a single AZ, but it does not automatically fail over across AZs. If the AZ hosting your NAT Gateway goes down, all private subnets routing through it lose internet access. The production pattern is one NAT Gateway per AZ, each serving the private subnets in that AZ. Second, cost: NAT Gateway costs $0.045/hr (~$32/month) plus $0.045/GB of data processed. For a workload processing 1 TB/month, that is $45 in processing fees alone. A NAT Instance on a t3.micro costs $0.0104/hr (~$7.50/month) with no per-GB processing fee. The trade-off is operational overhead: you must manage patching, scaling, and failover for NAT Instances yourself.

## Step 1 -- Create the Project Files

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
  default     = "nat-demo"
}
```

### `vpc.tf`

```hcl
data "aws_availability_zones" "available" {
  state = "available"
}

# ------------------------------------------------------------------
# VPC with public and private subnets (same pattern as exercise 17).
# ------------------------------------------------------------------
resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = var.project_name }
}

resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id
  tags   = { Name = "${var.project_name}-igw" }
}

resource "aws_subnet" "public_a" {
  vpc_id                  = aws_vpc.this.id
  cidr_block              = "10.0.1.0/24"
  availability_zone       = data.aws_availability_zones.available.names[0]
  map_public_ip_on_launch = true
  tags                    = { Name = "${var.project_name}-public-a" }
}

resource "aws_subnet" "public_b" {
  vpc_id                  = aws_vpc.this.id
  cidr_block              = "10.0.2.0/24"
  availability_zone       = data.aws_availability_zones.available.names[1]
  map_public_ip_on_launch = true
  tags                    = { Name = "${var.project_name}-public-b" }
}

resource "aws_subnet" "private_a" {
  vpc_id            = aws_vpc.this.id
  cidr_block        = "10.0.10.0/24"
  availability_zone = data.aws_availability_zones.available.names[0]
  tags              = { Name = "${var.project_name}-private-a" }
}

resource "aws_subnet" "private_b" {
  vpc_id            = aws_vpc.this.id
  cidr_block        = "10.0.11.0/24"
  availability_zone = data.aws_availability_zones.available.names[1]
  tags              = { Name = "${var.project_name}-private-b" }
}

# Public route table with IGW route
resource "aws_route_table" "public" {
  vpc_id = aws_vpc.this.id
  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this.id
  }
  tags = { Name = "${var.project_name}-public-rt" }
}

resource "aws_route_table_association" "public_a" {
  subnet_id      = aws_subnet.public_a.id
  route_table_id = aws_route_table.public.id
}

resource "aws_route_table_association" "public_b" {
  subnet_id      = aws_subnet.public_b.id
  route_table_id = aws_route_table.public.id
}

# ------------------------------------------------------------------
# NAT Gateway: AWS-managed, highly available within a single AZ.
#
# Architect trade-offs:
# - $0.045/hr fixed + $0.045/GB processed (expensive at scale)
# - Redundant within the AZ (multiple NAT nodes behind the scenes)
# - Supports up to 55,000 simultaneous connections per destination
# - No patching, no failover scripts, no monitoring of the NAT itself
# - Scales automatically up to 100 Gbps
# - Must be placed in a public subnet (needs IGW route)
# - Requires an Elastic IP
# ------------------------------------------------------------------
resource "aws_eip" "nat_gw" {
  domain = "vpc"
  tags   = { Name = "${var.project_name}-nat-gw-eip" }
}

resource "aws_nat_gateway" "this" {
  allocation_id = aws_eip.nat_gw.id
  subnet_id     = aws_subnet.public_a.id

  tags = { Name = "${var.project_name}-nat-gw" }

  depends_on = [aws_internet_gateway.this]
}

# Route table for private subnet A: routes internet traffic to NAT GW
resource "aws_route_table" "private_a" {
  vpc_id = aws_vpc.this.id

  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.this.id
  }

  tags = { Name = "${var.project_name}-private-a-rt" }
}

resource "aws_route_table_association" "private_a" {
  subnet_id      = aws_subnet.private_a.id
  route_table_id = aws_route_table.private_a.id
}
```

### `compute.tf`

```hcl
# ------------------------------------------------------------------
# NAT Instance: an EC2 instance performing NAT.
#
# Architect trade-offs:
# - t3.micro = $0.0104/hr (~$7.50/month vs ~$32/month for NAT GW)
# - No per-GB processing fee (major savings for data-heavy workloads)
# - Single point of failure (must build HA yourself with ASG + scripts)
# - Bandwidth limited by instance type (t3.micro = up to 5 Gbps burst)
# - YOU must patch the OS, monitor health, handle failover
# - Must disable source/dest check (EC2 drops traffic not addressed
#   to itself by default -- NAT traffic has a different source IP)
# ------------------------------------------------------------------

# Use the latest Amazon Linux 2023 AMI for the NAT instance
data "aws_ami" "amazon_linux" {
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

resource "aws_instance" "nat" {
  ami                         = data.aws_ami.amazon_linux.id
  instance_type               = "t3.micro"
  subnet_id                   = aws_subnet.public_b.id
  vpc_security_group_ids      = [aws_security_group.nat_instance.id]
  associate_public_ip_address = true

  # CRITICAL: disable source/destination check.
  # By default, EC2 verifies that it is the source or destination
  # of any traffic it handles. A NAT instance forwards traffic
  # that is neither sourced from nor destined to itself -- so this
  # check must be disabled or the traffic is silently dropped.
  source_dest_check = false

  # Enable IP forwarding and NAT via iptables
  user_data = <<-EOF
    #!/bin/bash
    yum install -y iptables-services
    systemctl enable iptables
    systemctl start iptables

    # Enable IP forwarding
    echo 1 > /proc/sys/net/ipv4/ip_forward
    echo "net.ipv4.ip_forward = 1" >> /etc/sysctl.conf

    # Configure iptables MASQUERADE rule for NAT
    iptables -t nat -A POSTROUTING -o ens5 -j MASQUERADE
    iptables -A FORWARD -i ens5 -o ens5 -m state \
      --state RELATED,ESTABLISHED -j ACCEPT
    iptables -A FORWARD -i ens5 -o ens5 -j ACCEPT
    service iptables save
  EOF

  tags = { Name = "${var.project_name}-nat-instance" }
}

# Route table for private subnet B: routes internet traffic to NAT Instance
resource "aws_route_table" "private_b" {
  vpc_id = aws_vpc.this.id

  route {
    cidr_block           = "0.0.0.0/0"
    network_interface_id = aws_instance.nat.primary_network_interface_id
  }

  tags = { Name = "${var.project_name}-private-b-rt" }
}

resource "aws_route_table_association" "private_b" {
  subnet_id      = aws_subnet.private_b.id
  route_table_id = aws_route_table.private_b.id
}
```

### `security.tf`

```hcl
resource "aws_security_group" "nat_instance" {
  name_prefix = "${var.project_name}-nat-instance-"
  vpc_id      = aws_vpc.this.id
  description = "Allow outbound NAT traffic"

  # Allow traffic from private subnets
  ingress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["10.0.10.0/24", "10.0.11.0/24"]
    description = "All traffic from private subnets"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
    description = "All outbound"
  }

  tags = { Name = "${var.project_name}-nat-instance-sg" }
}
```

### `outputs.tf`

```hcl
output "nat_gateway_id" {
  description = "NAT Gateway ID"
  value       = aws_nat_gateway.this.id
}

output "nat_gateway_public_ip" {
  description = "NAT Gateway Elastic IP"
  value       = aws_eip.nat_gw.public_ip
}

output "nat_instance_id" {
  description = "NAT Instance ID"
  value       = aws_instance.nat.id
}

output "nat_instance_public_ip" {
  description = "NAT Instance public IP"
  value       = aws_instance.nat.public_ip
}

output "vpc_id" {
  value = aws_vpc.this.id
}
```

## Step 2 -- Deploy and Compare

```bash
terraform init
terraform apply -auto-approve
```

### Decision Table: NAT Gateway vs NAT Instance

| Feature | NAT Gateway | NAT Instance |
|---------|-------------|--------------|
| **Managed by** | AWS | You |
| **Hourly cost** | ~$0.045/hr (~$32/mo) | t3.micro ~$0.0104/hr (~$7.50/mo) |
| **Data processing** | $0.045/GB | None (standard EC2 data transfer) |
| **Availability** | Redundant within AZ | Single instance (SPOF) |
| **Bandwidth** | Up to 100 Gbps | Depends on instance type |
| **Connections** | 55,000 simultaneous/dest | Depends on instance type |
| **Source/dest check** | Not applicable | Must disable manually |
| **Security groups** | Cannot associate | Can associate (filter traffic) |
| **Bastion host** | No (NAT only) | Can double as bastion |
| **Port forwarding** | No | Yes (iptables) |
| **Patching** | AWS handles | You handle |
| **Use case** | Production workloads | Dev/test, cost-sensitive |

### Cost Comparison Example (1 TB/month outbound)

| Component | NAT Gateway | NAT Instance (t3.micro) |
|-----------|-------------|------------------------|
| Hourly cost | $32.40/mo | $7.49/mo |
| Data processing | $46.08/mo | $0.00/mo |
| Data transfer | $92.16/mo | $92.16/mo |
| **Total** | **$170.64/mo** | **$99.65/mo** |

At 1 TB/month, the NAT Instance saves ~$71/month. At 100 GB/month, the savings drop to ~$29/month. The break-even point where NAT Gateway becomes economically justified depends on your operations team's cost and the value of the time they would spend managing NAT Instances.

## Common Mistakes

### 1. NAT Instance without source/dest check disabled

**Wrong approach:** Launching an EC2 instance as a NAT but leaving `source_dest_check = true` (the default):

```hcl
resource "aws_instance" "nat" {
  ami           = data.aws_ami.amazon_linux.id
  instance_type = "t3.micro"
  # source_dest_check defaults to true -- WRONG for NAT
}
```

**What happens:** The NAT instance receives traffic from private instances but silently drops it because EC2 verifies the instance is the source or destination of every packet. NAT traffic has a private instance as the source, so the check fails. Private instances see connection timeouts with no helpful error messages.

**Fix:** Always set `source_dest_check = false` on NAT instances:

```hcl
resource "aws_instance" "nat" {
  source_dest_check = false
  # ...
}
```

### 2. Placing NAT Gateway in a private subnet

**Wrong approach:** Creating the NAT Gateway in a private subnet:

```hcl
resource "aws_nat_gateway" "this" {
  allocation_id = aws_eip.nat_gw.id
  subnet_id     = aws_subnet.private_a.id  # Wrong! Must be public
}
```

**What happens:** The NAT Gateway has no route to the internet because the private subnet's route table lacks an IGW route. Outbound traffic from private instances reaches the NAT Gateway but cannot continue to the internet. Terraform apply succeeds, but all outbound connections from private subnets time out.

**Fix:** Always place the NAT Gateway in a public subnet (one with a route to an IGW).

### 3. Single NAT Gateway for multi-AZ architecture

**Wrong approach:** Using one NAT Gateway for all private subnets across multiple AZs.

**What happens:** If the AZ hosting the NAT Gateway fails, all private subnets in other AZs lose outbound internet access. This creates a single point of failure that defeats the purpose of multi-AZ deployment.

**Fix:** Deploy one NAT Gateway per AZ, each in the public subnet of that AZ. Route each private subnet to the NAT Gateway in the same AZ. This costs more ($32/month per NAT Gateway) but ensures AZ independence.

## Verify What You Learned

```bash
# Verify NAT Gateway is available
aws ec2 describe-nat-gateways \
  --nat-gateway-ids $(terraform output -raw nat_gateway_id) \
  --query "NatGateways[0].State" \
  --output text
```

Expected: `available`

```bash
# Verify NAT Instance has source/dest check disabled
aws ec2 describe-instances \
  --instance-ids $(terraform output -raw nat_instance_id) \
  --query "Reservations[0].Instances[0].SourceDestCheck" \
  --output text
```

Expected: `False`

```bash
# Verify private-a routes through NAT Gateway
aws ec2 describe-route-tables \
  --filters "Name=tag:Name,Values=nat-demo-private-a-rt" \
  --query "RouteTables[0].Routes[?DestinationCidrBlock=='0.0.0.0/0'].NatGatewayId" \
  --output text
```

Expected: NAT Gateway ID.

```bash
# Verify private-b routes through NAT Instance
aws ec2 describe-route-tables \
  --filters "Name=tag:Name,Values=nat-demo-private-b-rt" \
  --query "RouteTables[0].Routes[?DestinationCidrBlock=='0.0.0.0/0'].NetworkInterfaceId" \
  --output text
```

Expected: NAT Instance network interface ID.

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

You deployed both NAT approaches and understand the cost-availability trade-off. In the next exercise, you will explore **Security Groups vs NACLs** -- the two layers of network access control in a VPC -- and learn when the stateful allow-only model of security groups is sufficient versus when you need the stateless allow-and-deny rules of Network ACLs.

## Summary

- **NAT Gateway** is AWS-managed, redundant within an AZ, costs ~$0.045/hr plus $0.045/GB processed
- **NAT Instance** is self-managed EC2, costs ~$0.0104/hr with no per-GB fee, but is a single point of failure
- NAT devices must be placed in a **public subnet** with a route to the IGW
- NAT Instances require **source/dest check disabled** or traffic is silently dropped
- Production pattern: **one NAT Gateway per AZ** to maintain AZ independence
- NAT Gateway supports **up to 100 Gbps** and 55,000 simultaneous connections per destination
- NAT Instances can double as **bastion hosts** and support **security groups** and **port forwarding**
- Cost analysis should include both **hourly cost** and **per-GB processing fees** to compare accurately

## Reference

- [NAT Gateways](https://docs.aws.amazon.com/vpc/latest/userguide/vpc-nat-gateway.html)
- [NAT Instances](https://docs.aws.amazon.com/vpc/latest/userguide/VPC_NAT_Instance.html)
- [Compare NAT Gateways and NAT Instances](https://docs.aws.amazon.com/vpc/latest/userguide/vpc-nat-comparison.html)
- [Terraform aws_nat_gateway Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/nat_gateway)

## Additional Resources

- [NAT Gateway Pricing](https://aws.amazon.com/vpc/pricing/) -- detailed pricing by region including data processing fees
- [NAT Gateway Troubleshooting](https://docs.aws.amazon.com/vpc/latest/userguide/nat-gateway-troubleshooting.html) -- common issues including ErrorPortAllocation and bandwidth limits
- [High Availability for NAT Instances](https://docs.aws.amazon.com/vpc/latest/userguide/VPC_NAT_Instance.html#nat-instance-ha) -- using ASG with custom health checks for NAT instance failover
- [VPC Ingress Routing](https://docs.aws.amazon.com/vpc/latest/userguide/VPC_Route_Tables.html#gateway-route-table) -- advanced routing patterns for traffic inspection

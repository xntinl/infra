# 2. Public and Private Subnets with NAT Gateway

<!--
difficulty: basic
concepts: [vpc, public-subnet, private-subnet, nat-gateway, elastic-ip, route-tables]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: understand
prerequisites: [01-your-first-vpc]
aws_cost: ~$0.05/hr
-->

> **AWS Cost Warning:** This exercise creates a NAT Gateway (~$0.045/hr) plus two t3.micro instances (~$0.021/hr total). The NAT Gateway charges accrue even when idle. Run `terraform destroy` as soon as you finish.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 01 (Your First VPC)

## Learning Objectives

After completing this exercise, you will be able to:

- **Distinguish** between public and private subnets based on their route table configuration
- **Implement** a NAT Gateway that gives private instances outbound internet access without exposing them inbound
- **Trace** the path of a packet from a private instance through the NAT Gateway to the internet and back

## Why Public and Private Subnets

In exercise 01, every instance sat in a public subnet with a direct route to the internet. That works for a bastion host or a public web server, but most of your infrastructure -- databases, application servers, background workers -- should not be directly reachable from the internet. Placing them in a public subnet means any misconfigured security group could expose them to the world.

A private subnet solves this by having no route to an Internet Gateway. Instances inside it have no public IP and cannot be reached from outside the VPC. But they still need outbound internet access: downloading packages, calling external APIs, pulling container images. A NAT (Network Address Translation) Gateway provides this one-way door. It sits in the public subnet, has its own Elastic IP, and forwards outbound traffic from private instances to the internet. Return traffic flows back through the NAT Gateway, but no one on the internet can initiate a connection to the private instances.

Think of it like a corporate office network. Employees (private instances) can browse the web through the company's gateway (NAT Gateway), but no one on the internet can connect directly to an employee's workstation. The bastion host in the public subnet is like the reception desk -- the only entry point, tightly controlled.

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
    tls = {
      source  = "hashicorp/tls"
      version = "~> 4.0"
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
  default     = "pub-priv"
}
```

### `vpc.tf`

```hcl
# ------------------------------------------------------------------
# Data sources: dynamic AMI and AZ lookup so nothing is hardcoded.
# ------------------------------------------------------------------
data "aws_ami" "amazon_linux_2023" {
  most_recent = true
  owners      = ["amazon"]

  filter {
    name   = "name"
    values = ["al2023-ami-2023.*-x86_64"]
  }

  filter {
    name   = "state"
    values = ["available"]
  }
}

data "aws_availability_zones" "available" {
  state = "available"
}

# ------------------------------------------------------------------
# VPC: the isolated network. Same /16 block as exercise 01.
# ------------------------------------------------------------------
resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = "${var.project_name}-vpc" }
}

# ------------------------------------------------------------------
# Internet Gateway: required for the public subnet's internet access
# and for the NAT Gateway (which itself sits in the public subnet).
# ------------------------------------------------------------------
resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id

  tags = { Name = "${var.project_name}-igw" }
}

# ------------------------------------------------------------------
# Public subnet: hosts the bastion and the NAT Gateway.
# map_public_ip_on_launch gives bastion a public IP automatically.
# ------------------------------------------------------------------
resource "aws_subnet" "public" {
  vpc_id                  = aws_vpc.this.id
  cidr_block              = "10.0.1.0/24"
  availability_zone       = data.aws_availability_zones.available.names[0]
  map_public_ip_on_launch = true

  tags = { Name = "${var.project_name}-public-subnet" }
}

# ------------------------------------------------------------------
# Private subnet: hosts backend instances. No public IPs.
# Outbound internet goes through the NAT Gateway.
# ------------------------------------------------------------------
resource "aws_subnet" "private" {
  vpc_id            = aws_vpc.this.id
  cidr_block        = "10.0.2.0/24"
  availability_zone = data.aws_availability_zones.available.names[0]

  tags = { Name = "${var.project_name}-private-subnet" }
}

# ------------------------------------------------------------------
# Elastic IP: a static public IP that the NAT Gateway uses.
# This is the address that private instances appear to come from
# when they make outbound requests.
# ------------------------------------------------------------------
resource "aws_eip" "nat" {
  domain = "vpc"

  tags = { Name = "${var.project_name}-nat-eip" }
}

# ------------------------------------------------------------------
# NAT Gateway: placed in the PUBLIC subnet so it can reach the IGW.
# Private subnet traffic is routed here for outbound internet access.
# ------------------------------------------------------------------
resource "aws_nat_gateway" "this" {
  allocation_id = aws_eip.nat.id
  subnet_id     = aws_subnet.public.id

  tags = { Name = "${var.project_name}-nat-gateway" }

  # The NAT GW needs the IGW to exist first, because it routes
  # through the IGW to reach the internet.
  depends_on = [aws_internet_gateway.this]
}

# ------------------------------------------------------------------
# Public route table: default route goes to the Internet Gateway.
# This is the same pattern from exercise 01.
# ------------------------------------------------------------------
resource "aws_route_table" "public" {
  vpc_id = aws_vpc.this.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this.id
  }

  tags = { Name = "${var.project_name}-public-rt" }
}

resource "aws_route_table_association" "public" {
  subnet_id      = aws_subnet.public.id
  route_table_id = aws_route_table.public.id
}

# ------------------------------------------------------------------
# Private route table: default route goes to the NAT Gateway.
# Traffic to other VPC addresses (10.0.0.0/16) stays local (implicit).
# All other traffic exits through the NAT Gateway.
# ------------------------------------------------------------------
resource "aws_route_table" "private" {
  vpc_id = aws_vpc.this.id

  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.this.id
  }

  tags = { Name = "${var.project_name}-private-rt" }
}

resource "aws_route_table_association" "private" {
  subnet_id      = aws_subnet.private.id
  route_table_id = aws_route_table.private.id
}
```

### `security.tf`

```hcl
# ------------------------------------------------------------------
# Bastion security group: allows SSH from anywhere.
# The bastion is the only entry point into the VPC.
# ------------------------------------------------------------------
resource "aws_security_group" "bastion" {
  name        = "bastion-sg"
  description = "Allow SSH from anywhere to the bastion host"
  vpc_id      = aws_vpc.this.id

  tags = { Name = "${var.project_name}-bastion-sg" }
}

resource "aws_vpc_security_group_ingress_rule" "bastion_ssh" {
  security_group_id = aws_security_group.bastion.id
  cidr_ipv4         = "0.0.0.0/0"
  from_port         = 22
  to_port           = 22
  ip_protocol       = "tcp"
}

resource "aws_vpc_security_group_egress_rule" "bastion_all_out" {
  security_group_id = aws_security_group.bastion.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}

# ------------------------------------------------------------------
# Private security group: allows SSH ONLY from the bastion's SG.
# This means only instances in the bastion SG can connect -- not
# the entire internet. This is security group referencing.
# ------------------------------------------------------------------
resource "aws_security_group" "private" {
  name        = "private-sg"
  description = "Allow SSH from bastion only, all outbound"
  vpc_id      = aws_vpc.this.id

  tags = { Name = "${var.project_name}-private-sg" }
}

resource "aws_vpc_security_group_ingress_rule" "private_ssh_from_bastion" {
  security_group_id            = aws_security_group.private.id
  referenced_security_group_id = aws_security_group.bastion.id
  from_port                    = 22
  to_port                      = 22
  ip_protocol                  = "tcp"
}

resource "aws_vpc_security_group_egress_rule" "private_all_out" {
  security_group_id = aws_security_group.private.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}
```

### `compute.tf`

```hcl
# ------------------------------------------------------------------
# SSH key pair: auto-generated for both instances.
# ------------------------------------------------------------------
resource "tls_private_key" "this" {
  algorithm = "RSA"
  rsa_bits  = 4096
}

resource "aws_key_pair" "this" {
  key_name   = "${var.project_name}-key"
  public_key = tls_private_key.this.public_key_openssh
}

resource "local_file" "private_key" {
  content         = tls_private_key.this.private_key_pem
  filename        = "${path.module}/my-key.pem"
  file_permission = "0400"
}

# ------------------------------------------------------------------
# Bastion host: sits in the public subnet. This is your jump box
# for reaching instances in the private subnet.
# ------------------------------------------------------------------
resource "aws_instance" "bastion" {
  ami                    = data.aws_ami.amazon_linux_2023.id
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.public.id
  vpc_security_group_ids = [aws_security_group.bastion.id]
  key_name               = aws_key_pair.this.key_name

  tags = { Name = "${var.project_name}-bastion" }
}

# ------------------------------------------------------------------
# Private instance: sits in the private subnet. No public IP.
# Reachable only from the bastion via its private IP.
# ------------------------------------------------------------------
resource "aws_instance" "private" {
  ami                    = data.aws_ami.amazon_linux_2023.id
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.private.id
  vpc_security_group_ids = [aws_security_group.private.id]
  key_name               = aws_key_pair.this.key_name

  tags = { Name = "${var.project_name}-private-instance" }
}
```

### `outputs.tf`

```hcl
output "bastion_public_ip" {
  description = "Public IP of the bastion host"
  value       = aws_instance.bastion.public_ip
}

output "private_instance_private_ip" {
  description = "Private IP of the instance in the private subnet"
  value       = aws_instance.private.private_ip
}

output "nat_gateway_public_ip" {
  description = "Elastic IP of the NAT Gateway (outbound IP for private instances)"
  value       = aws_eip.nat.public_ip
}

output "ssh_to_bastion" {
  description = "SSH command to reach the bastion"
  value       = "ssh -i my-key.pem ec2-user@${aws_instance.bastion.public_ip}"
}

output "ssh_to_private" {
  description = "SSH command from bastion to private instance"
  value       = "ssh -i /tmp/my-key.pem ec2-user@${aws_instance.private.private_ip}"
}
```

## Step 2 -- Initialize and Apply

```bash
terraform init
terraform apply -auto-approve
```

This takes 2-3 minutes. The NAT Gateway is the slowest resource to provision.

### Intermediate Verification

Confirm the route tables are configured correctly:

```bash
aws ec2 describe-route-tables --filters "Name=tag:Name,Values=pub-priv-public-rt" \
  --query "RouteTables[0].Routes[?DestinationCidrBlock=='0.0.0.0/0'].GatewayId" --output text
```

Expected: an IGW ID starting with `igw-`

```bash
aws ec2 describe-route-tables --filters "Name=tag:Name,Values=pub-priv-private-rt" \
  --query "RouteTables[0].Routes[?DestinationCidrBlock=='0.0.0.0/0'].NatGatewayId" --output text
```

Expected: a NAT GW ID starting with `nat-`

## Step 3 -- Verify the Traffic Flow

### SSH to the bastion

```bash
ssh -i my-key.pem ec2-user@$(terraform output -raw bastion_public_ip)
```

### Copy the key to the bastion (for hopping to the private instance)

From your **local machine** (not the bastion), copy the key:

```bash
scp -i my-key.pem my-key.pem ec2-user@$(terraform output -raw bastion_public_ip):/tmp/my-key.pem
```

### SSH from bastion to the private instance

From the **bastion**:

```bash
chmod 400 /tmp/my-key.pem
ssh -i /tmp/my-key.pem ec2-user@$(terraform output -raw private_instance_private_ip)
```

Run `terraform output -raw private_instance_private_ip` on your local machine first if you need the IP.

### Verify outbound internet from the private instance

From the **private instance**:

```bash
curl ifconfig.me
```

Expected: the NAT Gateway's Elastic IP (same as `nat_gateway_public_ip` output). This proves the private instance reaches the internet through the NAT Gateway, not directly.

```bash
ping -c 3 google.com
```

Expected: three successful replies.

### Verify the private instance is NOT directly reachable

From your **local machine**, try to SSH directly to the private instance's private IP:

```bash
ssh -i my-key.pem ec2-user@$(terraform output -raw private_instance_private_ip)
```

Expected: connection times out. The private instance has no public IP and no inbound route from the internet.

## Common Mistakes

### 1. Placing the NAT Gateway in the private subnet

The NAT Gateway forwards traffic to the internet. To do that, it needs to be in a subnet that has a route to the Internet Gateway -- the public subnet. Placing it in the private subnet creates a circular dependency: the NAT Gateway can't reach the internet because the private subnet routes to... the NAT Gateway.

**Wrong -- NAT Gateway in private subnet:**

```hcl
resource "aws_nat_gateway" "this" {
  allocation_id = aws_eip.nat.id
  subnet_id     = aws_subnet.private.id   # WRONG: private subnet has no IGW route

  tags = { Name = "nat-gateway" }
}
```

**What happens:** `terraform apply` succeeds, but the private instance cannot reach the internet:

```bash
$ curl ifconfig.me
curl: (28) Connection timed out after 30001 milliseconds
```

**Fix -- place the NAT Gateway in the public subnet:**

```hcl
resource "aws_nat_gateway" "this" {
  allocation_id = aws_eip.nat.id
  subnet_id     = aws_subnet.public.id    # CORRECT: public subnet routes to IGW

  tags = { Name = "nat-gateway" }
}
```

### 2. Both subnets sharing the same route table

If you associate both subnets with the public route table (the one pointing to the IGW), the "private" subnet is not actually private -- instances there with a public IP would be directly reachable from the internet.

**Wrong -- both subnets use the public route table:**

```hcl
resource "aws_route_table_association" "public" {
  subnet_id      = aws_subnet.public.id
  route_table_id = aws_route_table.public.id
}

resource "aws_route_table_association" "private" {
  subnet_id      = aws_subnet.private.id
  route_table_id = aws_route_table.public.id   # WRONG: should use private RT
}
```

**What happens:** no immediate error, but the private subnet becomes a second public subnet. If you assign a public IP to a private instance, it becomes reachable from the internet -- defeating the purpose of network isolation.

**Fix -- each subnet gets its own route table with the correct default route:**

```hcl
resource "aws_route_table_association" "private" {
  subnet_id      = aws_subnet.private.id
  route_table_id = aws_route_table.private.id   # CORRECT: routes to NAT GW
}
```

## Verify What You Learned

```bash
terraform output bastion_public_ip
```

Expected: a public IP, e.g. `"54.210.123.45"`

```bash
terraform output private_instance_private_ip
```

Expected: a private IP in the `10.0.2.x` range, e.g. `"10.0.2.47"`

```bash
terraform output nat_gateway_public_ip
```

Expected: the Elastic IP assigned to the NAT Gateway, e.g. `"3.95.12.78"`

```bash
aws ec2 describe-instances --filters "Name=tag:Name,Values=pub-priv-private-instance" \
  --query "Reservations[0].Instances[0].PublicIpAddress" --output text
```

Expected: `None` -- confirming the private instance has no public IP.

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

Destroy all resources immediately to stop NAT Gateway charges:

```bash
terraform destroy -auto-approve
```

Verify:

```bash
terraform state list
```

Expected: no output (empty state).

> **Reminder:** The NAT Gateway costs ~$0.045/hr (~$1.08/day). Always destroy when done.

## What's Next

You now understand the fundamental two-tier network pattern: public subnets for internet-facing resources, private subnets for everything else. In the next exercise, you will explore the **two layers of network security** -- Security Groups (stateful, instance-level) and Network ACLs (stateless, subnet-level) -- and learn how they work together for defense in depth.

## Summary

- A **public subnet** has a route to an Internet Gateway; a **private subnet** does not
- A **NAT Gateway** sits in the public subnet and gives private instances outbound-only internet access
- An **Elastic IP** provides the NAT Gateway with a static public address
- **Each subnet needs its own route table** -- public routes to IGW, private routes to NAT GW
- **Security group referencing** (allowing traffic from another SG instead of a CIDR) is more secure and adapts automatically as instances are added or removed

## Reference

- [AWS NAT Gateway Documentation](https://docs.aws.amazon.com/vpc/latest/userguide/vpc-nat-gateway.html)
- [Terraform aws_nat_gateway Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/nat_gateway)
- [Terraform aws_eip Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/eip)

## Additional Resources

- [NAT Gateway Scenarios (AWS Docs)](https://docs.aws.amazon.com/vpc/latest/userguide/nat-gateway-scenarios.html) -- detailed scenarios for NAT Gateway placement and routing
- [VPC with Public and Private Subnets (AWS Docs)](https://docs.aws.amazon.com/vpc/latest/userguide/VPC_Scenario2.html) -- AWS reference architecture for the public/private subnet pattern
- [Reduce NAT Gateway Costs (AWS Blog)](https://aws.amazon.com/blogs/networking-and-content-delivery/reduce-nat-gateway-costs-by-using-vpc-endpoints/) -- strategies to minimize NAT Gateway data processing charges using VPC endpoints
- [SSH Agent Forwarding](https://docs.github.com/en/authentication/connecting-to-github-with-ssh/using-ssh-agent-forwarding) -- a more secure alternative to copying SSH keys to the bastion host

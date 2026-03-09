# 1. Your First VPC: Internet Gateway and Public Subnet

<!--
difficulty: basic
concepts: [vpc, subnet, internet-gateway, route-table, security-group, ec2]
tools: [terraform, aws-cli]
estimated_time: 30m
bloom_level: understand
prerequisites: [none]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise launches a t3.micro EC2 instance (~$0.0104/hr). Remember to run `terraform destroy` when finished to avoid unexpected charges.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- An SSH client (OpenSSH, PuTTY, or similar)

## Learning Objectives

After completing this exercise, you will be able to:

- **Identify** the core components of a VPC (VPC, subnet, internet gateway, route table, security group)
- **Construct** a working public subnet that provides internet access to an EC2 instance
- **Verify** end-to-end connectivity by SSHing into the instance and reaching the internet

## Why VPCs

When you launch resources in AWS without specifying a network, they land in the default VPC -- a shared network that AWS creates automatically in every region. This works for experiments, but in production you need control over IP address ranges, who can reach what, and how traffic flows. A Virtual Private Cloud (VPC) gives you an isolated network that you define from scratch.

CIDR notation is how you describe the size of your network. A VPC with the block `10.0.0.0/16` owns 65,536 IP addresses (everything from `10.0.0.0` to `10.0.255.255`). The `/16` means the first 16 bits are fixed and the remaining 16 bits are yours to allocate. A subnet with `10.0.1.0/24` carves out 256 of those addresses (the first 24 bits are fixed, leaving 8 bits -- though AWS reserves 5 addresses per subnet for internal use).

A VPC by itself is an isolated box with no internet access. To let traffic flow in and out, you attach an Internet Gateway (IGW) and create a route table that sends `0.0.0.0/0` (all non-local traffic) to the IGW. A subnet associated with that route table becomes a "public subnet." Add a security group to control which ports are open, and you have a complete, working network.

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
  default     = "my-first"
}
```

### `vpc.tf`

```hcl
data "aws_availability_zones" "available" {
  state = "available"
}

# VPC: the isolated network that contains all other resources.
# 10.0.0.0/16 gives us 65,536 addresses to subdivide into subnets.
resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = "${var.project_name}-vpc" }
}

# Internet Gateway: the bridge between the VPC and the public internet.
# Without this, nothing inside the VPC can reach external addresses.
resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id

  tags = { Name = "${var.project_name}-igw" }
}

# Public subnet: a /24 slice (256 addresses) of the VPC.
# map_public_ip_on_launch = true gives instances a public IP
# automatically, which is required for direct SSH access.
resource "aws_subnet" "public" {
  vpc_id                  = aws_vpc.this.id
  cidr_block              = "10.0.1.0/24"
  availability_zone       = data.aws_availability_zones.available.names[0]
  map_public_ip_on_launch = true

  tags = { Name = "${var.project_name}-public-subnet" }
}

# Route table: tells the subnet where to send traffic.
# The local route (VPC CIDR) is implicit. We add 0.0.0.0/0 → IGW
# so all other traffic goes to the internet.
resource "aws_route_table" "public" {
  vpc_id = aws_vpc.this.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this.id
  }

  tags = { Name = "${var.project_name}-public-rt" }
}

# Route table association: links the public subnet to the public
# route table. Without this, the subnet uses the VPC's main route
# table, which has no internet route.
resource "aws_route_table_association" "public" {
  subnet_id      = aws_subnet.public.id
  route_table_id = aws_route_table.public.id
}
```

### `security.tf`

```hcl
# Security group: a virtual firewall for the EC2 instance.
# Inbound: allow SSH (port 22) from anywhere.
# Outbound: allow all traffic so the instance can reach the internet.
resource "aws_security_group" "ssh" {
  name        = "allow-ssh"
  description = "Allow SSH inbound and all outbound"
  vpc_id      = aws_vpc.this.id

  tags = { Name = "${var.project_name}-sg" }
}

resource "aws_vpc_security_group_ingress_rule" "ssh_in" {
  security_group_id = aws_security_group.ssh.id
  cidr_ipv4         = "0.0.0.0/0"
  from_port         = 22
  to_port           = 22
  ip_protocol       = "tcp"
}

resource "aws_vpc_security_group_egress_rule" "all_out" {
  security_group_id = aws_security_group.ssh.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}
```

### `compute.tf`

```hcl
# Look up the latest Amazon Linux 2023 AMI so nothing is hardcoded.
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

# SSH key pair: generated locally so you don't need to create one
# manually in the AWS console. The private key is saved to a local
# file for SSH access.
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

# EC2 instance: a t3.micro in the public subnet to verify
# that the VPC, IGW, route table, and security group all work.
resource "aws_instance" "this" {
  ami                    = data.aws_ami.amazon_linux_2023.id
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.public.id
  vpc_security_group_ids = [aws_security_group.ssh.id]
  key_name               = aws_key_pair.this.key_name

  tags = { Name = "${var.project_name}-instance" }
}
```

### `outputs.tf`

```hcl
output "instance_public_ip" {
  description = "Public IP of the EC2 instance"
  value       = aws_instance.this.public_ip
}

output "ssh_command" {
  description = "Ready-to-use SSH command"
  value       = "ssh -i my-key.pem ec2-user@${aws_instance.this.public_ip}"
}

output "ami_used" {
  description = "AMI ID that was resolved by the data source"
  value       = data.aws_ami.amazon_linux_2023.id
}
```

## Step 2 -- Initialize and Apply

```bash
terraform init
terraform apply -auto-approve
```

Terraform will create 9 resources: the VPC, internet gateway, subnet, route table, route table association, security group (plus its two rules), key pair, TLS key, local file, and EC2 instance.

### Intermediate Verification

Confirm the expected resource count:

```bash
terraform state list | wc -l
```

Expected: `10` (VPC, IGW, subnet, route table, route table association, security group, ingress rule, egress rule, TLS key, key pair, local file, EC2 instance -- some resources are grouped)

```bash
terraform state list
```

You should see entries including:

```
aws_instance.this
aws_internet_gateway.this
aws_key_pair.this
aws_route_table.public
aws_route_table_association.public
aws_security_group.ssh
aws_subnet.public
aws_vpc.this
aws_vpc_security_group_egress_rule.all_out
aws_vpc_security_group_ingress_rule.ssh_in
local_file.private_key
tls_private_key.this
```

## Step 3 -- Verify Connectivity

SSH into the instance and confirm it can reach the internet:

```bash
ssh -i my-key.pem ec2-user@$(terraform output -raw instance_public_ip)
```

Once connected, verify internet access from inside the instance:

```bash
curl ifconfig.me
```

Expected: the public IP of your instance (same as the `instance_public_ip` output).

```bash
ping -c 3 google.com
```

Expected: three successful replies confirming DNS resolution and internet connectivity.

Type `exit` to return to your local machine.

## Common Mistakes

### 1. Forgetting the route table association

If you create a route table with the `0.0.0.0/0 -> IGW` route but never associate it with the subnet, the subnet falls back to the VPC's main route table, which has no internet route.

**Wrong -- missing `aws_route_table_association`:**

```hcl
resource "aws_route_table" "public" {
  vpc_id = aws_vpc.this.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this.id
  }
}

# No aws_route_table_association -- subnet uses default main route table
```

**What happens:** `terraform apply` succeeds, but SSH to the instance times out:

```
ssh: connect to host 10.0.1.42 port 22: Operation timed out
```

**Fix -- add the association:**

```hcl
resource "aws_route_table_association" "public" {
  subnet_id      = aws_subnet.public.id
  route_table_id = aws_route_table.public.id
}
```

### 2. Security group with no egress rule

When you define a security group in Terraform and specify any rule (ingress or egress), Terraform removes the default "allow all outbound" rule that the AWS console adds automatically. If you only define an ingress rule, the instance has no outbound access.

**Wrong -- ingress only, no egress:**

```hcl
resource "aws_vpc_security_group_ingress_rule" "ssh_in" {
  security_group_id = aws_security_group.ssh.id
  cidr_ipv4         = "0.0.0.0/0"
  from_port         = 22
  to_port           = 22
  ip_protocol       = "tcp"
}

# No egress rule defined
```

**What happens:** SSH connects, but commands that need outbound traffic fail:

```bash
$ curl ifconfig.me
curl: (28) Connection timed out after 30001 milliseconds
```

**Fix -- add an egress rule allowing all outbound traffic:**

```hcl
resource "aws_vpc_security_group_egress_rule" "all_out" {
  security_group_id = aws_security_group.ssh.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}
```

## Verify What You Learned

```bash
terraform output instance_public_ip
```

Expected: a public IP address, e.g. `"54.210.123.45"`

```bash
terraform output ssh_command
```

Expected: `"ssh -i my-key.pem ec2-user@54.210.123.45"`

```bash
aws ec2 describe-vpcs --filters "Name=tag:Name,Values=my-first-vpc" --query "Vpcs[0].CidrBlock" --output text
```

Expected: `10.0.0.0/16`

```bash
aws ec2 describe-route-tables --filters "Name=tag:Name,Values=my-first-public-rt" --query "RouteTables[0].Routes[?DestinationCidrBlock=='0.0.0.0/0'].GatewayId" --output text
```

Expected: an IGW ID starting with `igw-`, confirming the default route points to the internet gateway.

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

Destroy all resources to stop incurring charges:

```bash
terraform destroy -auto-approve
```

Verify nothing remains:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

You built a VPC with a single public subnet where every instance gets a public IP and direct internet access. In the next exercise, you will add a **private subnet** with a **NAT Gateway** -- a pattern where backend instances can reach the internet (for updates and API calls) without being directly reachable from outside.

## Summary

- A **VPC** is an isolated virtual network you define with a CIDR block
- An **Internet Gateway** connects the VPC to the public internet
- A **route table** with a `0.0.0.0/0 -> IGW` route makes a subnet public
- A **route table association** explicitly links a subnet to a route table (without it, the subnet uses the main route table)
- A **security group** controls inbound and outbound traffic at the instance level
- Terraform removes the default "allow all outbound" rule when you define any security group rule -- always specify egress explicitly

## Reference

- [AWS VPC Documentation](https://docs.aws.amazon.com/vpc/latest/userguide/what-is-amazon-vpc.html)
- [Terraform aws_vpc Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc)
- [Terraform aws_instance Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/instance)

## Additional Resources

- [VPCs and Subnets (AWS Docs)](https://docs.aws.amazon.com/vpc/latest/userguide/configure-your-vpc.html) -- official guide to VPC and subnet configuration including CIDR block allocation
- [Terraform AWS VPC Tutorial](https://developer.hashicorp.com/terraform/tutorials/aws-get-started/aws-build) -- HashiCorp's step-by-step tutorial for provisioning AWS infrastructure with Terraform
- [Understanding CIDR Notation](https://aws.amazon.com/what-is/cidr/) -- AWS primer on CIDR notation, subnet masks, and IP address planning
- [AWS Security Groups vs NACLs](https://docs.aws.amazon.com/vpc/latest/userguide/vpc-security-groups.html) -- how security groups work as stateful firewalls at the instance level

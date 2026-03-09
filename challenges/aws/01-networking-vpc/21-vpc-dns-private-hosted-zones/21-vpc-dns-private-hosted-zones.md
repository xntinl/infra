# 21. Private DNS with Route 53 Hosted Zones

<!--
difficulty: intermediate
concepts: [route53, private-hosted-zones, split-horizon-dns, vpc-dns, cname, a-records]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: design
prerequisites: [01-your-first-vpc, 05-vpc-peering-and-dns]
aws_cost: ~$0.02/hr
-->

> **AWS Cost Warning:** This exercise creates a Route 53 Private Hosted Zone ($0.50/month), t3.micro EC2 instances (~$0.0104/hr each), and a NAT Gateway (~$0.045/hr). Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Exercise 01 completed | Understand VPC basics |
| Exercise 05 completed | Understand VPC peering and DNS |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Design** a private DNS architecture using Route 53 Private Hosted Zones
2. **Implement** split-horizon DNS where the same hostname resolves differently inside and outside the VPC
3. **Configure** A records, CNAME records, and weighted routing policies for internal service discovery
4. **Associate** a Private Hosted Zone with multiple VPCs for cross-VPC name resolution
5. **Verify** DNS resolution from within the VPC using `dig` and `nslookup`

## Why Private DNS Matters

In a microservices architecture, services need to find each other by name, not by IP address. Hardcoding IP addresses creates tight coupling -- every time a service is redeployed, every consumer must be updated. Route 53 Private Hosted Zones provide DNS resolution that only works within associated VPCs. When an application calls `api.internal.example.com`, Route 53 resolves it to the private IP of the API service. Outside the VPC, that same hostname either does not resolve or resolves to a different (public) IP -- this is called split-horizon DNS.

> **Best Practice:** Private Hosted Zones override public DNS within associated VPCs -- useful for split-horizon DNS. When you associate a Private Hosted Zone with a VPC, any query for that zone's domain name is answered by Route 53's private resolver instead of the public internet.

Private Hosted Zones are the glue that makes service discovery work without external dependencies. Unlike Consul or etcd, Route 53 Private Hosted Zones are fully managed, require no infrastructure to run, and integrate natively with every AWS service that does DNS lookups.

## Building Blocks

Create the following files in your exercise directory. Your job is to fill in each `# TODO` block.

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
  default     = "private-dns-lab"
}

variable "domain_name" {
  description = "Internal domain name for the Private Hosted Zone"
  type        = string
  default     = "internal.example.com"
}
```

### `vpc.tf`

```hcl
data "aws_availability_zones" "available" {
  state = "available"
}

locals {
  azs = slice(data.aws_availability_zones.available.names, 0, 2)
}

# ------------------------------------------------------------------
# Primary VPC: hosts the services and the Private Hosted Zone.
# ------------------------------------------------------------------
resource "aws_vpc" "primary" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = "${var.project_name}-primary-vpc" }
}

resource "aws_subnet" "primary_public" {
  vpc_id                  = aws_vpc.primary.id
  cidr_block              = "10.0.1.0/24"
  availability_zone       = local.azs[0]
  map_public_ip_on_launch = true

  tags = { Name = "${var.project_name}-primary-public" }
}

resource "aws_subnet" "primary_private" {
  for_each = toset(local.azs)

  vpc_id            = aws_vpc.primary.id
  cidr_block        = cidrsubnet("10.0.0.0/16", 8, index(local.azs, each.value) + 10)
  availability_zone = each.value

  tags = { Name = "${var.project_name}-primary-private-${each.value}" }
}

resource "aws_internet_gateway" "primary" {
  vpc_id = aws_vpc.primary.id

  tags = { Name = "${var.project_name}-primary-igw" }
}

resource "aws_route_table" "primary_public" {
  vpc_id = aws_vpc.primary.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.primary.id
  }

  tags = { Name = "${var.project_name}-primary-public-rt" }
}

resource "aws_route_table_association" "primary_public" {
  subnet_id      = aws_subnet.primary_public.id
  route_table_id = aws_route_table.primary_public.id
}

# ------------------------------------------------------------------
# Secondary VPC: will be associated with the same Private Hosted
# Zone to demonstrate cross-VPC DNS resolution.
# ------------------------------------------------------------------
resource "aws_vpc" "secondary" {
  cidr_block           = "10.1.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = "${var.project_name}-secondary-vpc" }
}

resource "aws_subnet" "secondary_private" {
  vpc_id            = aws_vpc.secondary.id
  cidr_block        = "10.1.1.0/24"
  availability_zone = local.azs[0]

  tags = { Name = "${var.project_name}-secondary-private" }
}
```

### `security.tf`

```hcl
resource "aws_security_group" "instance" {
  name        = "${var.project_name}-instance-sg"
  description = "Allow SSH and HTTP for testing DNS resolution"
  vpc_id      = aws_vpc.primary.id

  tags = { Name = "${var.project_name}-instance-sg" }
}

resource "aws_vpc_security_group_ingress_rule" "ssh" {
  security_group_id = aws_security_group.instance.id
  cidr_ipv4         = "0.0.0.0/0"
  from_port         = 22
  to_port           = 22
  ip_protocol       = "tcp"
}

resource "aws_vpc_security_group_ingress_rule" "http" {
  security_group_id = aws_security_group.instance.id
  cidr_ipv4         = "10.0.0.0/8"
  from_port         = 80
  to_port           = 80
  ip_protocol       = "tcp"
}

resource "aws_vpc_security_group_egress_rule" "all_out" {
  security_group_id = aws_security_group.instance.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}
```

### `compute.tf`

```hcl
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
# API service instance: will be registered as api.internal.example.com
# ------------------------------------------------------------------
resource "aws_instance" "api" {
  ami                    = data.aws_ami.amazon_linux_2023.id
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.primary_public.id
  vpc_security_group_ids = [aws_security_group.instance.id]
  key_name               = aws_key_pair.this.key_name

  user_data = <<-EOF
    #!/bin/bash
    yum install -y httpd bind-utils
    echo '{"service":"api","version":"v1"}' > /var/www/html/index.html
    systemctl enable httpd
    systemctl start httpd
  EOF

  tags = { Name = "${var.project_name}-api" }
}

# ------------------------------------------------------------------
# Web service instance: will be registered as web.internal.example.com
# ------------------------------------------------------------------
resource "aws_instance" "web" {
  ami                    = data.aws_ami.amazon_linux_2023.id
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.primary_public.id
  vpc_security_group_ids = [aws_security_group.instance.id]
  key_name               = aws_key_pair.this.key_name

  user_data = <<-EOF
    #!/bin/bash
    yum install -y httpd bind-utils
    echo '<html><body><h1>Web Service</h1></body></html>' > /var/www/html/index.html
    systemctl enable httpd
    systemctl start httpd
  EOF

  tags = { Name = "${var.project_name}-web" }
}
```

### `dns.tf`

```hcl
# =======================================================
# TODO 1 -- Private Hosted Zone
# =======================================================
# Requirements:
#   - Create an aws_route53_zone for var.domain_name
#   - Associate it with the primary VPC using a vpc block:
#       vpc { vpc_id = aws_vpc.primary.id }
#   - Tag with Name = "${var.project_name}-internal-zone"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route53_zone
# Hint: A zone with a vpc block is automatically private


# =======================================================
# TODO 2 -- Associate Secondary VPC with the Hosted Zone
# =======================================================
# Requirements:
#   - Create an aws_route53_zone_association to associate
#     the secondary VPC with the private hosted zone
#   - Use the zone_id from TODO 1
#   - Use the vpc_id from aws_vpc.secondary
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route53_zone_association
# Hint: This lets instances in the secondary VPC resolve
#       names in the private hosted zone


# =======================================================
# TODO 3 -- A Record for the API service
# =======================================================
# Requirements:
#   - Create an aws_route53_record of type "A"
#   - Name: "api.${var.domain_name}"
#   - TTL: 300
#   - Records: the private IP of the API instance
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route53_record


# =======================================================
# TODO 4 -- A Record for the Web service
# =======================================================
# Requirements:
#   - Create an aws_route53_record of type "A"
#   - Name: "web.${var.domain_name}"
#   - TTL: 300
#   - Records: the private IP of the web instance
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route53_record


# =======================================================
# TODO 5 -- CNAME for service alias
# =======================================================
# Requirements:
#   - Create an aws_route53_record of type "CNAME"
#   - Name: "backend.${var.domain_name}"
#   - TTL: 300
#   - Records: "api.${var.domain_name}" (points to the API service)
#   - This demonstrates using CNAME aliases for service discovery
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route53_record
# Hint: CNAME records cannot coexist with other record types at the zone apex
```

### `outputs.tf`

```hcl
output "primary_vpc_id" {
  description = "Primary VPC ID"
  value       = aws_vpc.primary.id
}

output "secondary_vpc_id" {
  description = "Secondary VPC ID"
  value       = aws_vpc.secondary.id
}

output "zone_id" {
  description = "Private Hosted Zone ID"
  value       = aws_route53_zone.internal.zone_id
}

output "api_private_ip" {
  description = "API instance private IP"
  value       = aws_instance.api.private_ip
}

output "web_private_ip" {
  description = "Web instance private IP"
  value       = aws_instance.web.private_ip
}

output "api_public_ip" {
  description = "API instance public IP (for SSH)"
  value       = aws_instance.api.public_ip
}

output "ssh_command" {
  description = "SSH command for the API instance"
  value       = "ssh -i my-key.pem ec2-user@${aws_instance.api.public_ip}"
}
```

## Spot the Bug

A colleague created a Private Hosted Zone but DNS queries for `api.internal.example.com` from within the VPC return `NXDOMAIN` (not found). The Route 53 console shows the zone and records exist. **What is wrong?**

```hcl
resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = false  # <-- BUG
  enable_dns_hostnames = false  # <-- BUG

  tags = { Name = "my-vpc" }
}

resource "aws_route53_zone" "internal" {
  name = "internal.example.com"

  vpc {
    vpc_id = aws_vpc.this.id
  }
}
```

<details>
<summary>Explain the bug</summary>

**The problem:** Both `enable_dns_support` and `enable_dns_hostnames` are set to `false`. DNS support is required for the VPC's built-in DNS resolver (at `10.0.0.2`) to function. Without it, instances cannot resolve any DNS names -- including those in the Private Hosted Zone. The zone and records exist in Route 53, but the VPC's DNS resolver is disabled, so queries never reach Route 53.

**The fix:** Enable both DNS settings on the VPC:

```hcl
resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = "my-vpc" }
}
```

`enable_dns_support` activates the VPC DNS resolver. `enable_dns_hostnames` assigns DNS hostnames to instances, which is required for many AWS features including Private Hosted Zones.

</details>

## Solutions

<details>
<summary>TODO 1 -- Private Hosted Zone (dns.tf)</summary>

```hcl
resource "aws_route53_zone" "internal" {
  name = var.domain_name

  vpc {
    vpc_id = aws_vpc.primary.id
  }

  tags = { Name = "${var.project_name}-internal-zone" }
}
```

</details>

<details>
<summary>TODO 2 -- Associate Secondary VPC (dns.tf)</summary>

```hcl
resource "aws_route53_zone_association" "secondary" {
  zone_id = aws_route53_zone.internal.zone_id
  vpc_id  = aws_vpc.secondary.id
}
```

</details>

<details>
<summary>TODO 3 -- A Record for API service (dns.tf)</summary>

```hcl
resource "aws_route53_record" "api" {
  zone_id = aws_route53_zone.internal.zone_id
  name    = "api.${var.domain_name}"
  type    = "A"
  ttl     = 300
  records = [aws_instance.api.private_ip]
}
```

</details>

<details>
<summary>TODO 4 -- A Record for Web service (dns.tf)</summary>

```hcl
resource "aws_route53_record" "web" {
  zone_id = aws_route53_zone.internal.zone_id
  name    = "web.${var.domain_name}"
  type    = "A"
  ttl     = 300
  records = [aws_instance.web.private_ip]
}
```

</details>

<details>
<summary>TODO 5 -- CNAME for service alias (dns.tf)</summary>

```hcl
resource "aws_route53_record" "backend" {
  zone_id = aws_route53_zone.internal.zone_id
  name    = "backend.${var.domain_name}"
  type    = "CNAME"
  ttl     = 300
  records = ["api.${var.domain_name}"]
}
```

</details>

## Verify What You Learned

### Step 1 -- Confirm the Private Hosted Zone exists

```bash
aws route53 list-hosted-zones-by-name \
  --dns-name "${var.domain_name:-internal.example.com}" \
  --query "HostedZones[?Config.PrivateZone==\`true\`].{ID:Id,Name:Name,Private:Config.PrivateZone}" \
  --output table
```

Expected:

```
---------------------------------------------------
|              ListHostedZonesByName               |
+---------------------------+-------+-------------+
|            ID             | Name  |   Private   |
+---------------------------+-------+-------------+
| /hostedzone/Z0123456789AB | internal.example.com. | True |
+---------------------------+-------+-------------+
```

### Step 2 -- Verify DNS records

```bash
aws route53 list-resource-record-sets \
  --hosted-zone-id $(terraform output -raw zone_id) \
  --query "ResourceRecordSets[?Type!='SOA' && Type!='NS'].{Name:Name,Type:Type,Value:ResourceRecords[0].Value}" \
  --output table
```

Expected:

```
-------------------------------------------------------------------
|                   ListResourceRecordSets                        |
+-----------------------------------+-------+--------------------+
|              Name                 | Type  |      Value         |
+-----------------------------------+-------+--------------------+
| api.internal.example.com.        |  A    |  10.0.1.x          |
| backend.internal.example.com.    | CNAME |  api.internal....  |
| web.internal.example.com.        |  A    |  10.0.1.x          |
+-----------------------------------+-------+--------------------+
```

### Step 3 -- Test DNS resolution from within the VPC

SSH into the API instance and test:

```bash
ssh -i my-key.pem ec2-user@$(terraform output -raw api_public_ip)
```

From inside the instance:

```bash
dig api.internal.example.com +short
```

Expected: the private IP of the API instance (e.g., `10.0.1.42`).

```bash
dig backend.internal.example.com +short
```

Expected: first shows `api.internal.example.com` (CNAME), then the private IP.

```bash
dig web.internal.example.com +short
```

Expected: the private IP of the web instance.

### Step 4 -- Verify VPC associations

```bash
aws route53 get-hosted-zone \
  --id $(terraform output -raw zone_id) \
  --query "VPCs[].{VpcId:VPCId,Region:VPCRegion}" \
  --output table
```

Expected: both the primary and secondary VPC IDs listed.

### Step 5 -- Verify no changes pending

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

In **Exercise 22 -- Network Cost Optimization Patterns**, you will analyze the cost of common networking patterns (NAT Gateway, data transfer, VPC endpoints) and implement optimizations that can save significant money on AWS networking bills.

## Summary

- **Private Hosted Zones** provide DNS resolution that only works within associated VPCs
- **Split-horizon DNS** lets the same hostname resolve to different IPs inside vs outside the VPC
- **A records** map hostnames to IP addresses; **CNAME records** create aliases pointing to other hostnames
- **VPC association** allows multiple VPCs to resolve names in the same Private Hosted Zone
- Both `enable_dns_support` and `enable_dns_hostnames` must be `true` on the VPC for private DNS to work
- Private Hosted Zones cost **$0.50/month** per zone plus $0.40 per million queries

## Reference

| Resource | Purpose |
|----------|---------|
| `aws_route53_zone` | Creates the Private Hosted Zone |
| `aws_route53_zone_association` | Associates additional VPCs with the zone |
| `aws_route53_record` | Creates A, CNAME, and other DNS records |
| `aws_vpc` | Must have DNS support enabled for private DNS |

## Additional Resources

- [Route 53 Private Hosted Zones (AWS Docs)](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/hosted-zones-private.html) -- creating and managing private hosted zones
- [Split-View DNS (AWS Blog)](https://aws.amazon.com/blogs/security/how-to-set-up-dns-resolution-between-on-premises-networks-and-aws-using-aws-directory-service-and-amazon-route-53/) -- split-horizon DNS patterns for hybrid architectures
- [Route 53 Resolver Endpoints](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/resolver.html) -- forward DNS queries between VPCs and on-premises networks
- [Terraform aws_route53_zone Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route53_zone) -- Terraform documentation for hosted zones
- [Route 53 Pricing](https://aws.amazon.com/route53/pricing/) -- hosted zone and query pricing details

## Apply Your Knowledge

- [Route 53 Health Checks with Failover](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/dns-failover.html) -- add health checks to DNS records for automatic failover between instances
- [Route 53 Weighted Routing](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/routing-policy-weighted.html) -- distribute DNS queries across multiple endpoints with weighted policies
- [Route 53 Resolver Rules](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/resolver-rules-managing.html) -- forward specific DNS queries to on-premises or custom DNS servers

---

> *"The world would be a better place if more engineers, like me, hated technology."*
> -- **Radia Perlman**

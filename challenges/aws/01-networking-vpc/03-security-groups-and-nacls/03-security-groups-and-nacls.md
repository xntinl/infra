# 3. Security Groups and NACLs: Defense in Depth

<!--
difficulty: basic
concepts: [security-groups, nacls, stateful-vs-stateless, defense-in-depth]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: understand
prerequisites: [01-your-first-vpc]
aws_cost: ~$0.02/hr
-->

> **AWS Cost Warning:** This exercise launches two t3.micro EC2 instances (~$0.021/hr total). Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 01 (Your First VPC)

## Learning Objectives

After completing this exercise, you will be able to:

- **Compare** Security Groups and Network ACLs by identifying their key differences (stateful vs. stateless, instance vs. subnet level)
- **Configure** both layers of network security to implement defense in depth
- **Predict** whether a packet will be allowed or denied based on SG rules and NACL rules evaluated together

## Why Two Layers of Firewall

AWS gives you two independent layers of network filtering, and understanding both is essential. A Security Group is a stateful firewall attached to an individual resource (like an EC2 instance). "Stateful" means that if you allow an inbound request on port 80, the response traffic is automatically allowed -- you never need a separate rule for the return path. You only write allow rules; there is no way to write a deny rule in a Security Group.

A Network Access Control List (NACL) is a stateless firewall attached to a subnet. "Stateless" means every packet is evaluated independently -- if you allow inbound HTTP on port 80, you must also explicitly allow the outbound response on ephemeral ports (1024-65535), or the response will be silently dropped. NACLs support both allow and deny rules, and rules are evaluated in order by rule number (lowest first).

Using both together is called defense in depth. Security Groups handle the common case -- "this web server accepts HTTP and HTTPS." NACLs add a second net -- "block all traffic from this known-bad IP range" or "restrict an entire subnet to specific protocols." If one layer is misconfigured, the other still provides protection.

## Security Groups vs. NACLs

| Feature | Security Group | Network ACL |
|---------|---------------|-------------|
| **Operates at** | Instance (ENI) level | Subnet level |
| **Stateful/Stateless** | Stateful (return traffic auto-allowed) | Stateless (must allow both directions) |
| **Rule types** | Allow only | Allow and Deny |
| **Rule evaluation** | All rules evaluated as a group | Rules evaluated in order by number |
| **Default behavior** | Denies all inbound, allows all outbound | Default NACL allows all; custom NACL denies all |
| **Applies to** | Only instances assigned to the SG | All instances in the subnet |

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
  default     = "sg-nacl"
}
```

### `vpc.tf`

```hcl
# ------------------------------------------------------------------
# Data sources: dynamic AMI and AZ lookup.
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
# VPC: the isolated network for this exercise.
# ------------------------------------------------------------------
resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = "${var.project_name}-vpc" }
}

# ------------------------------------------------------------------
# Internet Gateway and public route table: standard setup so both
# subnets can reach the internet for testing.
# ------------------------------------------------------------------
resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id

  tags = { Name = "${var.project_name}-igw" }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.this.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this.id
  }

  tags = { Name = "${var.project_name}-public-rt" }
}

# ------------------------------------------------------------------
# Web subnet: hosts the web-tier instance. Uses the default NACL
# (which allows all traffic) -- security is handled by the SG.
# ------------------------------------------------------------------
resource "aws_subnet" "web" {
  vpc_id                  = aws_vpc.this.id
  cidr_block              = "10.0.1.0/24"
  availability_zone       = data.aws_availability_zones.available.names[0]
  map_public_ip_on_launch = true

  tags = { Name = "${var.project_name}-web-subnet" }
}

resource "aws_route_table_association" "web" {
  subnet_id      = aws_subnet.web.id
  route_table_id = aws_route_table.public.id
}

# ------------------------------------------------------------------
# App subnet: hosts the app-tier instance. Gets a custom NACL
# that demonstrates stateless filtering.
# ------------------------------------------------------------------
resource "aws_subnet" "app" {
  vpc_id                  = aws_vpc.this.id
  cidr_block              = "10.0.2.0/24"
  availability_zone       = data.aws_availability_zones.available.names[0]
  map_public_ip_on_launch = true

  tags = { Name = "${var.project_name}-app-subnet" }
}

resource "aws_route_table_association" "app" {
  subnet_id      = aws_subnet.app.id
  route_table_id = aws_route_table.public.id
}
```

### `security.tf`

```hcl
# ------------------------------------------------------------------
# Web security group: allows HTTP (80), HTTPS (443), and SSH (22)
# from anywhere, plus all outbound. This is a typical web server SG.
# Because SGs are STATEFUL, we only need inbound rules -- response
# traffic is automatically allowed.
# ------------------------------------------------------------------
resource "aws_security_group" "web" {
  name        = "web-sg"
  description = "Web tier: HTTP, HTTPS, SSH inbound; all outbound"
  vpc_id      = aws_vpc.this.id

  tags = { Name = "${var.project_name}-web-sg" }
}

resource "aws_vpc_security_group_ingress_rule" "web_http" {
  security_group_id = aws_security_group.web.id
  cidr_ipv4         = "0.0.0.0/0"
  from_port         = 80
  to_port           = 80
  ip_protocol       = "tcp"
}

resource "aws_vpc_security_group_ingress_rule" "web_https" {
  security_group_id = aws_security_group.web.id
  cidr_ipv4         = "0.0.0.0/0"
  from_port         = 443
  to_port           = 443
  ip_protocol       = "tcp"
}

resource "aws_vpc_security_group_ingress_rule" "web_ssh" {
  security_group_id = aws_security_group.web.id
  cidr_ipv4         = "0.0.0.0/0"
  from_port         = 22
  to_port           = 22
  ip_protocol       = "tcp"
}

resource "aws_vpc_security_group_egress_rule" "web_all_out" {
  security_group_id = aws_security_group.web.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}

# ------------------------------------------------------------------
# App security group: allows port 8080 ONLY from the web SG, plus
# SSH from anywhere (for testing). Outbound allows all.
# The SG-to-SG reference means only instances in web-sg can connect
# on 8080 -- no CIDR to maintain.
# ------------------------------------------------------------------
resource "aws_security_group" "app" {
  name        = "app-sg"
  description = "App tier: 8080 from web-sg only, SSH, all outbound"
  vpc_id      = aws_vpc.this.id

  tags = { Name = "${var.project_name}-app-sg" }
}

resource "aws_vpc_security_group_ingress_rule" "app_from_web" {
  security_group_id            = aws_security_group.app.id
  referenced_security_group_id = aws_security_group.web.id
  from_port                    = 8080
  to_port                      = 8080
  ip_protocol                  = "tcp"
}

resource "aws_vpc_security_group_ingress_rule" "app_ssh" {
  security_group_id = aws_security_group.app.id
  cidr_ipv4         = "0.0.0.0/0"
  from_port         = 22
  to_port           = 22
  ip_protocol       = "tcp"
}

resource "aws_vpc_security_group_egress_rule" "app_all_out" {
  security_group_id = aws_security_group.app.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}

# ------------------------------------------------------------------
# Custom NACL for the app subnet: demonstrates stateless filtering.
#
# Rule evaluation order matters: lowest rule number is evaluated first.
# Rule 100: DENY traffic from 198.51.100.0/24 (simulated bad IP range)
# Rule 200: ALLOW all TCP inbound (SSH, app traffic from web tier)
# Rule 300: ALLOW all TCP outbound (responses, internet access)
# Rule 310: ALLOW all UDP outbound (DNS resolution)
# Rule 400: ALLOW inbound ephemeral ports (return traffic from
#           outbound connections -- required because NACLs are STATELESS)
#
# The default rule (*) denies everything not matched above.
# ------------------------------------------------------------------
resource "aws_network_acl" "app" {
  vpc_id     = aws_vpc.this.id
  subnet_ids = [aws_subnet.app.id]

  # DENY inbound from a simulated bad IP range (evaluated first)
  ingress {
    rule_no    = 100
    protocol   = "-1"
    action     = "deny"
    cidr_block = "198.51.100.0/24"
    from_port  = 0
    to_port    = 0
  }

  # ALLOW inbound TCP (SSH on 22, app traffic on 8080, etc.)
  ingress {
    rule_no    = 200
    protocol   = "tcp"
    action     = "allow"
    cidr_block = "0.0.0.0/0"
    from_port  = 1
    to_port    = 65535
  }

  # ALLOW inbound UDP (DNS responses on port 53)
  ingress {
    rule_no    = 210
    protocol   = "udp"
    action     = "allow"
    cidr_block = "0.0.0.0/0"
    from_port  = 1
    to_port    = 65535
  }

  # ALLOW outbound TCP (responses to inbound requests + internet access)
  egress {
    rule_no    = 300
    protocol   = "tcp"
    action     = "allow"
    cidr_block = "0.0.0.0/0"
    from_port  = 1
    to_port    = 65535
  }

  # ALLOW outbound UDP (DNS queries to port 53)
  egress {
    rule_no    = 310
    protocol   = "udp"
    action     = "allow"
    cidr_block = "0.0.0.0/0"
    from_port  = 1
    to_port    = 65535
  }

  tags = { Name = "${var.project_name}-app-nacl" }
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
# Web instance: runs a simple HTTP listener on port 80 to test
# security group rules. user_data starts a Python HTTP server.
# ------------------------------------------------------------------
resource "aws_instance" "web" {
  ami                    = data.aws_ami.amazon_linux_2023.id
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.web.id
  vpc_security_group_ids = [aws_security_group.web.id]
  key_name               = aws_key_pair.this.key_name

  user_data = <<-EOF
    #!/bin/bash
    echo "Hello from the web tier" > /tmp/index.html
    cd /tmp && python3 -m http.server 80 &
  EOF

  tags = { Name = "${var.project_name}-web-instance" }
}

# ------------------------------------------------------------------
# App instance: runs a simple HTTP listener on port 8080 to test
# SG-to-SG rules and NACL filtering.
# ------------------------------------------------------------------
resource "aws_instance" "app" {
  ami                    = data.aws_ami.amazon_linux_2023.id
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.app.id
  vpc_security_group_ids = [aws_security_group.app.id]
  key_name               = aws_key_pair.this.key_name

  user_data = <<-EOF
    #!/bin/bash
    echo "Hello from the app tier" > /tmp/index.html
    cd /tmp && python3 -m http.server 8080 &
  EOF

  tags = { Name = "${var.project_name}-app-instance" }
}
```

### `outputs.tf`

```hcl
output "web_public_ip" {
  description = "Public IP of the web-tier instance"
  value       = aws_instance.web.public_ip
}

output "app_public_ip" {
  description = "Public IP of the app-tier instance"
  value       = aws_instance.app.public_ip
}

output "app_private_ip" {
  description = "Private IP of the app-tier instance"
  value       = aws_instance.app.private_ip
}

output "ssh_to_web" {
  description = "SSH command for the web instance"
  value       = "ssh -i my-key.pem ec2-user@${aws_instance.web.public_ip}"
}

output "ssh_to_app" {
  description = "SSH command for the app instance"
  value       = "ssh -i my-key.pem ec2-user@${aws_instance.app.public_ip}"
}
```

## Step 2 -- Initialize and Apply

```bash
terraform init
terraform apply -auto-approve
```

Wait 1-2 minutes for the instances to boot and the user_data scripts to start the HTTP servers.

### Intermediate Verification

Confirm both security groups and the NACL exist:

```bash
aws ec2 describe-security-groups --filters "Name=vpc-id,Values=$(terraform output -raw vpc_id 2>/dev/null || aws ec2 describe-vpcs --filters 'Name=tag:Name,Values=sg-nacl-vpc' --query 'Vpcs[0].VpcId' --output text)" \
  --query "SecurityGroups[?GroupName!='default'].{Name:GroupName,ID:GroupId}" --output table
```

```bash
aws ec2 describe-network-acls --filters "Name=tag:Name,Values=sg-nacl-app-nacl" \
  --query "NetworkAcls[0].Entries[?RuleAction=='deny']" --output table
```

Expected: you should see the deny rule for `198.51.100.0/24`.

## Step 3 -- Verify Security Group Behavior (Stateful)

### Test 1: Web server is reachable on port 80

From your local machine (wait ~60 seconds after apply for user_data to finish):

```bash
curl http://$(terraform output -raw web_public_ip)
```

Expected: `Hello from the web tier`

This works because the web SG allows inbound TCP port 80. The response goes back to you automatically -- no outbound rule for port 80 is needed because **Security Groups are stateful**.

### Test 2: App server port 8080 is NOT reachable from the internet

```bash
curl --connect-timeout 5 http://$(terraform output -raw app_public_ip):8080
```

Expected: `Connection timed out` -- the app SG only allows port 8080 from the web SG, not from the internet.

### Test 3: App server IS reachable from the web instance

SSH into the web instance and curl the app:

```bash
ssh -i my-key.pem ec2-user@$(terraform output -raw web_public_ip)
```

From the web instance:

```bash
curl http://<app-private-ip>:8080
```

Replace `<app-private-ip>` with the `app_private_ip` output value.

Expected: `Hello from the app tier` -- the web instance is in `web-sg`, which is allowed by the app SG's ingress rule.

## Step 4 -- Verify NACL Behavior (Stateless)

### Understanding the NACL rules

SSH into the app instance to verify it can reach the internet:

```bash
ssh -i my-key.pem ec2-user@$(terraform output -raw app_public_ip)
```

From the app instance:

```bash
curl ifconfig.me
```

Expected: the app instance's public IP. This works because:
1. **Outbound:** NACL egress rule 300 allows TCP out (the curl request)
2. **Inbound:** NACL ingress rule 200 allows TCP in on all ports (the response comes back on an ephemeral port)

Both directions must be explicitly allowed because **NACLs are stateless**.

### Understanding NACL rule ordering

The NACL denies all traffic from `198.51.100.0/24` at rule 100. Because rule 100 is evaluated before rule 200 (which allows all TCP), traffic from that range is blocked even though a later rule would allow it. This is the power of NACLs: you can deny specific traffic that Security Groups cannot (SGs only have allow rules).

## Common Mistakes

### 1. NACL rule ordering -- DENY after ALLOW (never reached)

NACL rules are evaluated in ascending order by rule number. If you put the DENY rule at a higher number than the ALLOW rule, it is never reached.

**Wrong -- DENY at rule 300, ALLOW at rule 100:**

```hcl
resource "aws_network_acl" "app" {
  vpc_id     = aws_vpc.this.id
  subnet_ids = [aws_subnet.app.id]

  # ALLOW all TCP at rule 100 (evaluated first)
  ingress {
    rule_no    = 100
    protocol   = "tcp"
    action     = "allow"
    cidr_block = "0.0.0.0/0"
    from_port  = 1
    to_port    = 65535
  }

  # DENY bad IPs at rule 300 (NEVER REACHED -- rule 100 already matched)
  ingress {
    rule_no    = 300
    protocol   = "-1"
    action     = "deny"
    cidr_block = "198.51.100.0/24"
    from_port  = 0
    to_port    = 0
  }
}
```

**What happens:** Traffic from `198.51.100.0/24` is allowed because rule 100 matches first. The DENY at rule 300 is never evaluated.

**Fix -- always place DENY rules at lower rule numbers than ALLOW rules:**

```hcl
  # DENY bad IPs at rule 100 (evaluated first)
  ingress {
    rule_no    = 100
    protocol   = "-1"
    action     = "deny"
    cidr_block = "198.51.100.0/24"
    from_port  = 0
    to_port    = 0
  }

  # ALLOW all TCP at rule 200 (evaluated second)
  ingress {
    rule_no    = 200
    protocol   = "tcp"
    action     = "allow"
    cidr_block = "0.0.0.0/0"
    from_port  = 1
    to_port    = 65535
  }
```

### 2. Forgetting ephemeral ports in NACL -- responses blocked

Because NACLs are stateless, you must allow return traffic explicitly. When the app instance makes an outbound HTTP request, the response comes back on an ephemeral port (1024-65535). If your NACL only allows inbound on specific ports like 22 and 8080, the response is silently dropped.

**Wrong -- inbound rules only allow ports 22 and 8080:**

```hcl
resource "aws_network_acl" "app" {
  vpc_id     = aws_vpc.this.id
  subnet_ids = [aws_subnet.app.id]

  ingress {
    rule_no    = 100
    protocol   = "tcp"
    action     = "allow"
    cidr_block = "0.0.0.0/0"
    from_port  = 22
    to_port    = 22
  }

  ingress {
    rule_no    = 200
    protocol   = "tcp"
    action     = "allow"
    cidr_block = "0.0.0.0/0"
    from_port  = 8080
    to_port    = 8080
  }

  egress {
    rule_no    = 100
    protocol   = "tcp"
    action     = "allow"
    cidr_block = "0.0.0.0/0"
    from_port  = 1
    to_port    = 65535
  }
}
```

**What happens:** SSH works (port 22 is allowed inbound), but from inside the instance:

```bash
$ curl ifconfig.me
curl: (28) Connection timed out after 30001 milliseconds
```

The outbound request leaves on port 80 (allowed by egress rule 100), but the response returns on an ephemeral port like 49152 -- which is not covered by any inbound rule. The NACL drops it.

**Fix -- allow inbound ephemeral ports (or a wide TCP range as in this exercise):**

```hcl
  # Allow inbound ephemeral ports for return traffic
  ingress {
    rule_no    = 300
    protocol   = "tcp"
    action     = "allow"
    cidr_block = "0.0.0.0/0"
    from_port  = 1024
    to_port    = 65535
  }
```

## Verify What You Learned

```bash
terraform output web_public_ip
```

Expected: a public IP, e.g. `"54.210.123.45"`

```bash
curl -s http://$(terraform output -raw web_public_ip)
```

Expected: `Hello from the web tier`

```bash
curl -s --connect-timeout 5 http://$(terraform output -raw app_public_ip):8080 || echo "Connection blocked (expected)"
```

Expected: `Connection blocked (expected)` -- the app SG only allows 8080 from the web SG.

```bash
aws ec2 describe-network-acls --filters "Name=tag:Name,Values=sg-nacl-app-nacl" \
  --query "NetworkAcls[0].Entries[?Egress==\`false\` && RuleAction=='deny'].CidrBlock" --output text
```

Expected: `198.51.100.0/24` -- confirming the NACL deny rule is in place.

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

You now understand the two layers of AWS network security and when to use each. In the next exercise, you will build a **production-ready Multi-AZ VPC** with public and private subnets across multiple availability zones, applying everything from exercises 01-03 at scale.

## Summary

- **Security Groups** are stateful firewalls at the instance level -- allow-only rules, return traffic is automatic
- **Network ACLs** are stateless firewalls at the subnet level -- allow and deny rules, must handle both directions explicitly
- NACL rules are evaluated by **rule number** (lowest first); place DENY rules before ALLOW rules
- **Ephemeral ports** (1024-65535) must be allowed in NACLs for return traffic from outbound connections
- Using both layers together provides **defense in depth** -- if one is misconfigured, the other still protects
- **SG-to-SG referencing** is more maintainable than CIDR-based rules for intra-VPC traffic

## Reference

- [AWS Security Groups Documentation](https://docs.aws.amazon.com/vpc/latest/userguide/vpc-security-groups.html)
- [AWS Network ACLs Documentation](https://docs.aws.amazon.com/vpc/latest/userguide/vpc-network-acls.html)
- [Terraform aws_network_acl Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/network_acl)

## Additional Resources

- [Security Groups vs NACLs (AWS Docs)](https://docs.aws.amazon.com/vpc/latest/userguide/infrastructure-security.html) -- official comparison of the two firewall layers and when to use each
- [VPC Security Best Practices (AWS Well-Architected)](https://docs.aws.amazon.com/wellarchitected/latest/security-pillar/protecting-networks.html) -- architectural guidance on network security from the Well-Architected Framework
- [Ephemeral Ports Explained](https://docs.aws.amazon.com/vpc/latest/userguide/vpc-network-acls.html#nacl-ephemeral-ports) -- AWS explanation of ephemeral port ranges and why NACLs need them
- [Terraform Security Group Rules (Spacelift)](https://spacelift.io/blog/terraform-security-group) -- practical guide to managing AWS security groups with Terraform including best practices

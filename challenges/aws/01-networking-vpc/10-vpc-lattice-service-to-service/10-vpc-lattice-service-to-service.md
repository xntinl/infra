# 10. VPC Lattice for Service-to-Service Communication

<!--
difficulty: advanced
concepts: [vpc-lattice, service-network, service, target-group, auth-policy, listener-rules]
tools: [terraform, aws-cli]
estimated_time: 90m
bloom_level: evaluate
prerequisites: [01-08]
aws_cost: ~$0.35/hr
-->

> **AWS Cost Warning:** This exercise creates VPC Lattice resources (service network, services, target groups) and EC2 instances. Estimated total: ~$0.35/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 08 (Transit Gateway Hub-and-Spoke)
- Familiarity with IAM policy syntax

## Learning Objectives

After completing this exercise, you will be able to:

- **Compare** VPC Lattice to Transit Gateway and PrivateLink for service-to-service networking
- **Implement** a service network spanning multiple VPCs with registered services
- **Configure** VPC Lattice target groups, listeners, and routing rules
- **Integrate** IAM-based auth policies to control which services can call which
- **Evaluate** how VPC Lattice eliminates the need for route table management in application-layer networking

## Why VPC Lattice

Transit Gateway connects networks at Layer 3 (IP routing) and PrivateLink exposes individual services through endpoint ENIs. VPC Lattice operates at Layer 7 (HTTP/HTTPS) and is purpose-built for service-to-service communication. You create a service network, associate VPCs with it, register services backed by target groups, and VPC Lattice handles DNS resolution, load balancing, and auth -- all without touching a single route table. Auth policies use IAM-style JSON, letting you express rules like "only the frontend service in VPC-A may call the backend service in VPC-B." For microservice architectures spanning multiple VPCs, VPC Lattice replaces the combination of Transit Gateway + internal NLBs + service discovery that teams previously had to assemble by hand. See the [VPC Lattice documentation](https://docs.aws.amazon.com/vpc-lattice/latest/ug/what-is-vpc-lattice.html) for the full feature set.

## The Challenge

Implement service-to-service communication using Amazon VPC Lattice as a modern application-layer networking solution spanning two VPCs.

| VPC | CIDR | Role |
|-----|------|------|
| frontend-vpc | 10.50.0.0/16 | Hosts the frontend service |
| backend-vpc | 10.60.0.0/16 | Hosts the backend service |

### Requirements

1. **VPC Lattice service network** that both VPCs are associated with
2. **Two VPC Lattice services**: `frontend-svc` (in VPC-A) and `backend-svc` (in VPC-B)
3. **Target groups** (instance type) in each VPC, registered with EC2 instances running web servers
4. **Listeners** on each service (HTTP, port 80) with default rules routing to target groups
5. **Auth policy** on the backend service: only requests originating from the frontend VPC (or a specific security group) are allowed; all other traffic is denied
6. **No route table modifications** -- VPC Lattice works entirely through DNS and the data plane managed prefix list
7. **Security groups** updated to allow traffic from the VPC Lattice managed prefix list

### Architecture

```
  ┌─────────────────────────────────────────────────────┐
  │              VPC Lattice Service Network             │
  │                                                     │
  │  ┌─────────────────┐       ┌─────────────────┐     │
  │  │  frontend-svc   │       │  backend-svc    │     │
  │  │  (listener:80)  │──────►│  (listener:80)  │     │
  │  └────────┬────────┘       └────────┬────────┘     │
  └───────────┼────────────────────────┼───────────────┘
              │                        │
     ┌────────┴─────────┐    ┌────────┴─────────┐
     │  frontend-vpc     │    │  backend-vpc      │
     │  10.50.0.0/16     │    │  10.60.0.0/16     │
     │  ┌──────────┐     │    │  ┌──────────┐     │
     │  │ EC2: web │     │    │  │ EC2: api  │     │
     │  └──────────┘     │    │  └──────────┘     │
     └───────────────────┘    └───────────────────┘

     Auth policy: only frontend-vpc → backend-svc allowed
```

## Hints

Work through these one at a time. Only open the next hint if you are stuck.

<details>
<summary>Hint 1: Service network and VPC associations</summary>

Start by creating the service network, then associate both VPCs:

1. Create `aws_vpclattice_service_network` -- this is the logical grouping that services and VPCs join
2. Create `aws_vpclattice_service_network_vpc_association` for each VPC. Each association references the service network ID, the VPC ID, and optionally security group IDs

The security groups on the VPC association control what traffic from the VPC can reach the service network. You need to allow outbound HTTP (port 80) to the VPC Lattice managed prefix list.

To find the managed prefix list for VPC Lattice, use:
```
data "aws_ec2_managed_prefix_list" "vpc_lattice" {
  name = "com.amazonaws.<region>.vpc-lattice"
}
```

</details>

<details>
<summary>Hint 2: Target groups and EC2 instances</summary>

Create a target group for each service:

1. `aws_vpclattice_target_group` with `type = "INSTANCE"` and a config block specifying port, protocol (HTTP), VPC ID, and health check settings
2. `aws_vpclattice_target_group_attachment` to register each EC2 instance with its target group

The EC2 instances need a simple HTTP server. Use user-data to install `httpd` or start a Python HTTP server. Make sure security groups on the EC2 instances allow inbound HTTP from the VPC Lattice managed prefix list -- VPC Lattice sends traffic from its own IP range, not from the calling VPC's CIDR.

</details>

<details>
<summary>Hint 3: Services and listeners</summary>

Create the VPC Lattice services and wire them up:

1. `aws_vpclattice_service` for each service (frontend-svc, backend-svc). Set `auth_type = "AWS_IAM"` on the backend service to enable auth policy enforcement
2. `aws_vpclattice_listener` on each service: protocol HTTP, port 80, with a default action forwarding to the corresponding target group
3. `aws_vpclattice_service_network_service_association` to register each service with the service network

After association, VPC Lattice generates a DNS name for each service in the format `<service-name>-<random>.on.aws`. Any instance in an associated VPC can resolve this DNS name.

</details>

<details>
<summary>Hint 4: Auth policies</summary>

Use `aws_vpclattice_auth_policy` to restrict access to the backend service:

The policy uses IAM policy syntax. To allow only requests from the frontend VPC:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": "*",
      "Action": "vpc-lattice-svcs:Invoke",
      "Resource": "*",
      "Condition": {
        "StringEquals": {
          "vpc-lattice-svcs:SourceVpc": "<frontend-vpc-id>"
        }
      }
    }
  ]
}
```

Set `auth_type = "AWS_IAM"` on the backend service. Without this, VPC Lattice does not enforce auth policies even if one is attached.

To test: requests from frontend-vpc should succeed (HTTP 200), while requests from backend-vpc to its own service or any other VPC should be denied (HTTP 403).

</details>

<details>
<summary>Hint 5: Testing and debugging</summary>

Common issues and debugging steps:

1. **DNS not resolving**: Ensure the VPC association is active (`aws vpc-lattice list-service-network-vpc-associations`). DNS resolution takes a few minutes after association
2. **Connection timeout**: Check that the EC2 security group allows inbound from the VPC Lattice prefix list, not from the peer VPC CIDR
3. **HTTP 403 Forbidden**: Auth policy is working but rejecting the request. Check the `SourceVpc` condition matches the calling VPC ID exactly
4. **Health check failing**: Ensure the target group health check path returns HTTP 200 and the EC2 instance's web server is running

Test from the frontend EC2 instance:
```
curl -v http://<backend-service-dns-name>
```

Verify auth policy enforcement from the backend EC2 instance (should fail):
```
curl -v http://<backend-service-dns-name>
# Expected: HTTP 403
```

</details>

## Spot the Bug

The VPC association is created and the service is registered, but curling the backend service DNS name from the frontend EC2 instance times out. The security group looks like this:

```hcl
resource "aws_security_group" "frontend_ec2" {
  name        = "frontend-ec2-sg"
  description = "Allow outbound to backend"
  vpc_id      = aws_vpc.frontend.id
}

resource "aws_vpc_security_group_ingress_rule" "frontend_http" {
  security_group_id = aws_security_group.frontend_ec2.id
  cidr_ipv4         = "10.60.0.0/16"
  from_port         = 80
  to_port           = 80
  ip_protocol       = "tcp"
}

resource "aws_vpc_security_group_egress_rule" "frontend_all" {
  security_group_id = aws_security_group.frontend_ec2.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}

resource "aws_security_group" "backend_ec2" {
  name        = "backend-ec2-sg"
  description = "Allow inbound from frontend VPC"
  vpc_id      = aws_vpc.backend.id
}

resource "aws_vpc_security_group_ingress_rule" "backend_http" {
  security_group_id = aws_security_group.backend_ec2.id
  cidr_ipv4         = "10.50.0.0/16"
  from_port         = 80
  to_port           = 80
  ip_protocol       = "tcp"
}
```

<details>
<summary>Explain the bug</summary>

**The problem:** The backend EC2 security group allows inbound HTTP from `10.50.0.0/16` (the frontend VPC CIDR), but VPC Lattice does **not** send traffic from the calling VPC's IP range. VPC Lattice uses its own managed IP range -- traffic arrives at the backend EC2 from the **VPC Lattice managed prefix list**, not from `10.50.0.0/16`.

This is a fundamental difference from VPC peering or Transit Gateway, where traffic arrives from the peer VPC's CIDR.

**The fix:** Allow inbound traffic from the VPC Lattice managed prefix list instead of the peer VPC CIDR:

```hcl
data "aws_ec2_managed_prefix_list" "vpc_lattice" {
  name = "com.amazonaws.us-east-1.vpc-lattice"
}

resource "aws_vpc_security_group_ingress_rule" "backend_http" {
  security_group_id = aws_security_group.backend_ec2.id
  prefix_list_id    = data.aws_ec2_managed_prefix_list.vpc_lattice.id
  from_port         = 80
  to_port           = 80
  ip_protocol       = "tcp"
}
```

</details>

## Verify What You Learned

### Service network exists

```bash
aws vpc-lattice list-service-networks \
  --query "items[?name=='<your-service-network-name>'].{Name:name,Id:id,Arn:arn}" \
  --output table
```

Expected:

```
--------------------------------------------------------------
|                    ListServiceNetworks                     |
+-----+---------------------------+-------------------------+
|  Id | Name                      | Arn                     |
+-----+---------------------------+-------------------------+
| sn- | <your-service-network>    | arn:aws:vpc-lattice:... |
+-----+---------------------------+-------------------------+
```

### Both VPCs are associated

```bash
aws vpc-lattice list-service-network-vpc-associations \
  --service-network-identifier <your-service-network-id> \
  --query "items[].{VpcId:vpcId,Status:status}" \
  --output table
```

Expected:

```
-----------------------------------------
| ListServiceNetworkVpcAssociations     |
+------------------+--------------------+
|  Status          |  VpcId             |
+------------------+--------------------+
|  ACTIVE          |  vpc-xxxxx         |
|  ACTIVE          |  vpc-yyyyy         |
+------------------+--------------------+
```

### Backend service has auth policy

```bash
aws vpc-lattice get-auth-policy \
  --resource-identifier <your-backend-service-id> \
  --query "{State:state}" \
  --output table
```

Expected:

```
-----------------
| GetAuthPolicy |
+-------+-------+
| State | Active|
+-------+-------+
```

### Frontend can reach backend

```bash
# From frontend EC2 instance (via SSM or SSH):
curl -s http://<backend-service-dns-name>
```

Expected: `Hello from the backend service` (or your configured response, HTTP 200).

### Auth policy blocks unauthorized callers

```bash
# From backend EC2 instance (calling its own service, should be blocked):
curl -s -o /dev/null -w "%{http_code}" http://<backend-service-dns-name>
```

Expected: `403`

### No route table modifications

```bash
aws ec2 describe-route-tables \
  --filters "Name=vpc-id,Values=<your-frontend-vpc-id>" \
  --query "RouteTables[].Routes[].DestinationCidrBlock" \
  --output text
```

Expected: only `10.50.0.0/16` (local route) and optionally `0.0.0.0/0` for NAT -- no `10.60.0.0/16` route to the backend VPC.

### Terraform state is clean

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

Destroy all resources to stop incurring charges:

```bash
terraform destroy -auto-approve
```

VPC Lattice associations may take a few minutes to disassociate. If the destroy times out or errors, run it again.

Verify nothing remains:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

You now have experience with application-layer service networking. In the next exercise, you will deploy **AWS Network Firewall** in a centralized inspection architecture -- moving from application-layer policies (VPC Lattice auth) to network-layer deep packet inspection with Suricata-compatible rules.

## Summary

- **VPC Lattice** operates at Layer 7 (HTTP/HTTPS), unlike Transit Gateway (Layer 3) or PrivateLink (Layer 4)
- A **service network** is the shared namespace that VPCs and services join -- no route table modifications required
- **Target groups** register compute resources (EC2 instances, IPs, Lambda, ALBs) as backends for Lattice services
- **Auth policies** use IAM-style JSON with conditions like `SourceVpc` to control which callers can invoke a service
- Traffic arrives from the **VPC Lattice managed prefix list**, not the calling VPC's CIDR -- security groups must reference the prefix list
- VPC Lattice replaces the manual assembly of Transit Gateway + internal NLBs + service discovery for microservice architectures

## Reference

- [Amazon VPC Lattice Documentation](https://docs.aws.amazon.com/vpc-lattice/latest/ug/what-is-vpc-lattice.html)
- [Terraform aws_vpclattice_service_network](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpclattice_service_network)
- [Terraform aws_vpclattice_service](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpclattice_service)
- [Terraform aws_vpclattice_target_group](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpclattice_target_group)
- [Terraform aws_vpclattice_auth_policy](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpclattice_auth_policy)

## Additional Resources

- [VPC Lattice vs Transit Gateway vs PrivateLink (AWS Blog)](https://aws.amazon.com/blogs/networking-and-content-delivery/build-secure-multi-account-multi-vpc-connectivity-for-your-applications-with-amazon-vpc-lattice/) -- when to use each service
- [VPC Lattice Auth Policy Examples](https://docs.aws.amazon.com/vpc-lattice/latest/ug/auth-policies.html) -- detailed IAM policy patterns for service access control
- [VPC Lattice Quotas and Limits](https://docs.aws.amazon.com/vpc-lattice/latest/ug/quotas.html) -- service limits for networks, services, and target groups
- [Migrating from Service Mesh to VPC Lattice](https://aws.amazon.com/blogs/containers/migrating-from-aws-app-mesh-to-amazon-vpc-lattice/) -- comparison with App Mesh and migration guide

<details>
<summary>Full Solution</summary>

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
  region = "us-east-1"
}
```

### `locals.tf`

```hcl
data "aws_availability_zones" "available" {
  state = "available"
}

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

data "aws_ec2_managed_prefix_list" "vpc_lattice" {
  name = "com.amazonaws.us-east-1.vpc-lattice"
}

locals {
  az = data.aws_availability_zones.available.names[0]
}
```

### `vpc.tf`

```hcl
# ---------------------------------------------------------------
# VPCs
# ---------------------------------------------------------------
resource "aws_vpc" "frontend" {
  cidr_block           = "10.50.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags                 = { Name = "frontend-vpc" }
}

resource "aws_vpc" "backend" {
  cidr_block           = "10.60.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags                 = { Name = "backend-vpc" }
}

resource "aws_subnet" "frontend_private" {
  vpc_id            = aws_vpc.frontend.id
  cidr_block        = "10.50.1.0/24"
  availability_zone = local.az
  tags              = { Name = "frontend-private" }
}

resource "aws_subnet" "backend_private" {
  vpc_id            = aws_vpc.backend.id
  cidr_block        = "10.60.1.0/24"
  availability_zone = local.az
  tags              = { Name = "backend-private" }
}

# Public subnets + NAT for user-data package installs
resource "aws_subnet" "frontend_public" {
  vpc_id                  = aws_vpc.frontend.id
  cidr_block              = "10.50.10.0/24"
  availability_zone       = local.az
  map_public_ip_on_launch = true
  tags                    = { Name = "frontend-public" }
}

resource "aws_subnet" "backend_public" {
  vpc_id                  = aws_vpc.backend.id
  cidr_block              = "10.60.10.0/24"
  availability_zone       = local.az
  map_public_ip_on_launch = true
  tags                    = { Name = "backend-public" }
}

resource "aws_internet_gateway" "frontend" {
  vpc_id = aws_vpc.frontend.id
  tags   = { Name = "frontend-igw" }
}

resource "aws_internet_gateway" "backend" {
  vpc_id = aws_vpc.backend.id
  tags   = { Name = "backend-igw" }
}

resource "aws_eip" "frontend_nat" { domain = "vpc" }
resource "aws_eip" "backend_nat" { domain = "vpc" }

resource "aws_nat_gateway" "frontend" {
  allocation_id = aws_eip.frontend_nat.id
  subnet_id     = aws_subnet.frontend_public.id
  depends_on    = [aws_internet_gateway.frontend]
  tags          = { Name = "frontend-nat" }
}

resource "aws_nat_gateway" "backend" {
  allocation_id = aws_eip.backend_nat.id
  subnet_id     = aws_subnet.backend_public.id
  depends_on    = [aws_internet_gateway.backend]
  tags          = { Name = "backend-nat" }
}

resource "aws_route_table" "frontend_public" {
  vpc_id = aws_vpc.frontend.id
  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.frontend.id
  }
  tags = { Name = "frontend-public-rt" }
}

resource "aws_route_table" "backend_public" {
  vpc_id = aws_vpc.backend.id
  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.backend.id
  }
  tags = { Name = "backend-public-rt" }
}

resource "aws_route_table" "frontend_private" {
  vpc_id = aws_vpc.frontend.id
  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.frontend.id
  }
  tags = { Name = "frontend-private-rt" }
}

resource "aws_route_table" "backend_private" {
  vpc_id = aws_vpc.backend.id
  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.backend.id
  }
  tags = { Name = "backend-private-rt" }
}

resource "aws_route_table_association" "frontend_public" {
  subnet_id      = aws_subnet.frontend_public.id
  route_table_id = aws_route_table.frontend_public.id
}

resource "aws_route_table_association" "backend_public" {
  subnet_id      = aws_subnet.backend_public.id
  route_table_id = aws_route_table.backend_public.id
}

resource "aws_route_table_association" "frontend_private" {
  subnet_id      = aws_subnet.frontend_private.id
  route_table_id = aws_route_table.frontend_private.id
}

resource "aws_route_table_association" "backend_private" {
  subnet_id      = aws_subnet.backend_private.id
  route_table_id = aws_route_table.backend_private.id
}
```

### `security.tf`

```hcl
# ---------------------------------------------------------------
# Security groups
# ---------------------------------------------------------------
resource "aws_security_group" "frontend_ec2" {
  name        = "frontend-ec2-sg"
  description = "Allow HTTP from VPC Lattice and all egress"
  vpc_id      = aws_vpc.frontend.id
  tags        = { Name = "frontend-ec2-sg" }
}

resource "aws_vpc_security_group_ingress_rule" "frontend_lattice" {
  security_group_id = aws_security_group.frontend_ec2.id
  prefix_list_id    = data.aws_ec2_managed_prefix_list.vpc_lattice.id
  from_port         = 80
  to_port           = 80
  ip_protocol       = "tcp"
}

resource "aws_vpc_security_group_egress_rule" "frontend_all" {
  security_group_id = aws_security_group.frontend_ec2.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}

resource "aws_security_group" "backend_ec2" {
  name        = "backend-ec2-sg"
  description = "Allow HTTP from VPC Lattice and all egress"
  vpc_id      = aws_vpc.backend.id
  tags        = { Name = "backend-ec2-sg" }
}

resource "aws_vpc_security_group_ingress_rule" "backend_lattice" {
  security_group_id = aws_security_group.backend_ec2.id
  prefix_list_id    = data.aws_ec2_managed_prefix_list.vpc_lattice.id
  from_port         = 80
  to_port           = 80
  ip_protocol       = "tcp"
}

resource "aws_vpc_security_group_egress_rule" "backend_all" {
  security_group_id = aws_security_group.backend_ec2.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}
```

### `compute.tf`

```hcl
# ---------------------------------------------------------------
# EC2 instances
# ---------------------------------------------------------------
resource "aws_instance" "frontend" {
  ami                    = data.aws_ami.amazon_linux_2023.id
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.frontend_private.id
  vpc_security_group_ids = [aws_security_group.frontend_ec2.id]
  user_data = <<-EOF
    #!/bin/bash
    dnf install -y httpd
    echo "Hello from the frontend service" > /var/www/html/index.html
    systemctl enable httpd
    systemctl start httpd
  EOF
  tags = { Name = "frontend-instance" }
}

resource "aws_instance" "backend" {
  ami                    = data.aws_ami.amazon_linux_2023.id
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.backend_private.id
  vpc_security_group_ids = [aws_security_group.backend_ec2.id]
  user_data = <<-EOF
    #!/bin/bash
    dnf install -y httpd
    echo "Hello from the backend service" > /var/www/html/index.html
    systemctl enable httpd
    systemctl start httpd
  EOF
  tags = { Name = "backend-instance" }
}
```

### `lattice.tf`

```hcl
# ---------------------------------------------------------------
# VPC Lattice service network
# ---------------------------------------------------------------
resource "aws_vpclattice_service_network" "this" {
  name      = "multi-service-network"
  auth_type = "NONE"
  tags      = { Name = "multi-service-network" }
}

resource "aws_vpclattice_service_network_vpc_association" "frontend" {
  vpc_identifier             = aws_vpc.frontend.id
  service_network_identifier = aws_vpclattice_service_network.this.id
  security_group_ids         = [aws_security_group.frontend_ec2.id]
  tags                       = { Name = "frontend-vpc-assoc" }
}

resource "aws_vpclattice_service_network_vpc_association" "backend" {
  vpc_identifier             = aws_vpc.backend.id
  service_network_identifier = aws_vpclattice_service_network.this.id
  security_group_ids         = [aws_security_group.backend_ec2.id]
  tags                       = { Name = "backend-vpc-assoc" }
}

# ---------------------------------------------------------------
# VPC Lattice target groups
# ---------------------------------------------------------------
resource "aws_vpclattice_target_group" "frontend" {
  name = "frontend-tg"
  type = "INSTANCE"

  config {
    port             = 80
    protocol         = "HTTP"
    vpc_identifier   = aws_vpc.frontend.id

    health_check {
      enabled  = true
      protocol = "HTTP"
      path     = "/"
      port     = 80
    }
  }

  tags = { Name = "frontend-tg" }
}

resource "aws_vpclattice_target_group_attachment" "frontend" {
  target_group_identifier = aws_vpclattice_target_group.frontend.id

  target {
    id   = aws_instance.frontend.id
    port = 80
  }
}

resource "aws_vpclattice_target_group" "backend" {
  name = "backend-tg"
  type = "INSTANCE"

  config {
    port             = 80
    protocol         = "HTTP"
    vpc_identifier   = aws_vpc.backend.id

    health_check {
      enabled  = true
      protocol = "HTTP"
      path     = "/"
      port     = 80
    }
  }

  tags = { Name = "backend-tg" }
}

resource "aws_vpclattice_target_group_attachment" "backend" {
  target_group_identifier = aws_vpclattice_target_group.backend.id

  target {
    id   = aws_instance.backend.id
    port = 80
  }
}

# ---------------------------------------------------------------
# VPC Lattice services
# ---------------------------------------------------------------
resource "aws_vpclattice_service" "frontend" {
  name      = "frontend-svc"
  auth_type = "NONE"
  tags      = { Name = "frontend-svc" }
}

resource "aws_vpclattice_service" "backend" {
  name      = "backend-svc"
  auth_type = "AWS_IAM"
  tags      = { Name = "backend-svc" }
}

resource "aws_vpclattice_listener" "frontend" {
  name               = "frontend-listener"
  protocol           = "HTTP"
  port               = 80
  service_identifier = aws_vpclattice_service.frontend.id

  default_action {
    forward {
      target_groups {
        target_group_identifier = aws_vpclattice_target_group.frontend.id
        weight                  = 100
      }
    }
  }

  tags = { Name = "frontend-listener" }
}

resource "aws_vpclattice_listener" "backend" {
  name               = "backend-listener"
  protocol           = "HTTP"
  port               = 80
  service_identifier = aws_vpclattice_service.backend.id

  default_action {
    forward {
      target_groups {
        target_group_identifier = aws_vpclattice_target_group.backend.id
        weight                  = 100
      }
    }
  }

  tags = { Name = "backend-listener" }
}

# ---------------------------------------------------------------
# Service network associations
# ---------------------------------------------------------------
resource "aws_vpclattice_service_network_service_association" "frontend" {
  service_identifier         = aws_vpclattice_service.frontend.id
  service_network_identifier = aws_vpclattice_service_network.this.id
  tags                       = { Name = "frontend-svc-assoc" }
}

resource "aws_vpclattice_service_network_service_association" "backend" {
  service_identifier         = aws_vpclattice_service.backend.id
  service_network_identifier = aws_vpclattice_service_network.this.id
  tags                       = { Name = "backend-svc-assoc" }
}

# ---------------------------------------------------------------
# Auth policy -- only frontend VPC can call backend
# ---------------------------------------------------------------
resource "aws_vpclattice_auth_policy" "backend" {
  resource_identifier = aws_vpclattice_service.backend.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect    = "Allow"
        Principal = "*"
        Action    = "vpc-lattice-svcs:Invoke"
        Resource  = "*"
        Condition = {
          StringEquals = {
            "vpc-lattice-svcs:SourceVpc" = aws_vpc.frontend.id
          }
        }
      }
    ]
  })
}
```

### `outputs.tf`

```hcl
output "service_network_id" {
  value = aws_vpclattice_service_network.this.id
}

output "frontend_service_dns" {
  value = aws_vpclattice_service.frontend.dns_entry[0].domain_name
}

output "backend_service_dns" {
  value = aws_vpclattice_service.backend.dns_entry[0].domain_name
}

output "frontend_instance_id" {
  value = aws_instance.frontend.id
}

output "backend_instance_id" {
  value = aws_instance.backend.id
}
```

</details>

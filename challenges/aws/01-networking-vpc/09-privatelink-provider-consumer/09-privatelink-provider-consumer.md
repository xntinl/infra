# 9. AWS PrivateLink: Service Provider and Consumer

<!--
difficulty: advanced
concepts: [privatelink, vpc-endpoint-service, interface-endpoint, nlb, private-connectivity, acceptance-workflow]
tools: [terraform, aws-cli]
estimated_time: 75m
bloom_level: evaluate
prerequisites: [01-04]
aws_cost: ~$0.12/hr
-->

> **AWS Cost Warning:** This exercise creates a Network Load Balancer (~$0.0225/hr), a VPC Interface Endpoint (~$0.01/hr per AZ), and EC2 instances. Estimated total: ~$0.12/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 04 (Production Multi-AZ VPC)
- Understanding of NLB concepts (target groups, listeners)

## Learning Objectives

After completing this exercise, you will be able to:

- **Architect** a PrivateLink service pattern separating provider and consumer responsibilities
- **Implement** a VPC Endpoint Service backed by a Network Load Balancer
- **Configure** the acceptance workflow where the provider approves consumer connections
- **Evaluate** the security benefits of PrivateLink versus VPC peering or Transit Gateway for service exposure
- **Verify** end-to-end private connectivity without any route table modifications

## Why PrivateLink

When you need to expose a service from one VPC to another, VPC peering and Transit Gateway both work -- but they connect entire networks, opening more surface area than necessary. AWS PrivateLink flips the model: instead of connecting networks, it exposes a single service endpoint. The consumer VPC gets an elastic network interface (ENI) that appears local, traffic stays on the AWS backbone, and neither side needs to manage overlapping CIDRs or route tables. This is how AWS itself exposes services like S3, DynamoDB, and STS to your VPCs, and it is the recommended pattern when third-party vendors or internal platform teams need to expose APIs without granting broad network access. See the [AWS PrivateLink documentation](https://docs.aws.amazon.com/vpc/latest/privatelink/what-is-privatelink.html) for the full architecture.

## The Challenge

Create a PrivateLink service where **VPC-A (provider)** exposes a web application and **VPC-B (consumer)** accesses it through an Interface VPC Endpoint -- with no VPC peering, no Transit Gateway, and no public internet involvement.

| VPC | CIDR | Role |
|-----|------|------|
| provider | 10.10.0.0/16 | Hosts NLB + EC2 application |
| consumer | 10.20.0.0/16 | Accesses service via Interface Endpoint |

### Requirements

1. **Provider VPC** with a private subnet, an internal NLB, a target group, and an EC2 instance running a web server (user-data with `python3 -m http.server 80` or `httpd`)
2. **VPC Endpoint Service** created from the NLB with `acceptance_required = true`
3. **Consumer VPC** with a private subnet and an Interface VPC Endpoint pointing to the endpoint service
4. **Connection acceptance** -- the provider must explicitly approve the consumer's endpoint connection
5. **Private DNS** -- configure a custom private DNS name so the consumer can use a friendly hostname instead of the auto-generated endpoint DNS
6. **Security groups** on both sides: provider allows traffic from the NLB CIDR range, consumer allows outbound HTTPS/HTTP to the endpoint ENIs
7. **No route table changes** in either VPC for cross-VPC traffic -- PrivateLink handles it all through ENIs

### Architecture

```
  Provider VPC (10.10.0.0/16)          Consumer VPC (10.20.0.0/16)
  ┌──────────────────────────┐         ┌──────────────────────────┐
  │                          │         │                          │
  │  ┌──────┐   ┌────────┐  │         │  ┌──────────────────┐    │
  │  │ EC2  ├──►│  NLB   │  │         │  │ Interface VPC    │    │
  │  │ web  │   │(internal)│ │         │  │ Endpoint (ENI)   │    │
  │  └──────┘   └────┬───┘  │         │  └────────┬─────────┘    │
  │                  │       │         │           │              │
  │         ┌────────┴─────┐ │         │  ┌────────┴─────────┐   │
  │         │ VPC Endpoint │ │PrivateLink│ │ Consumer EC2     │   │
  │         │ Service      ├─┼─────────┼─►│ curl endpoint-dns│   │
  │         └──────────────┘ │         │  └──────────────────┘   │
  └──────────────────────────┘         └──────────────────────────┘
         No peering. No TGW. No public internet.
```

## Hints

Work through these one at a time. Only open the next hint if you are stuck.

<details>
<summary>Hint 1: Provider infrastructure</summary>

Build the provider side first:

1. Create **VPC-A** with a private subnet (and a public subnet + NAT if you need to install packages via user-data)
2. Create an **internal** NLB (`internal = true`) -- this is critical, PrivateLink does not work with internet-facing NLBs
3. Create a target group (type `instance`, protocol TCP, port 80) and register the EC2 instance
4. Create an NLB listener (TCP, port 80) forwarding to the target group
5. The EC2 instance needs a simple web server. User-data example approach: install and start httpd, or run `python3 -m http.server 80`

Make sure the EC2 security group allows inbound on port 80 from the **VPC CIDR** (the NLB preserves client IPs by default for instance targets, but the traffic enters from the NLB's subnet).

</details>

<details>
<summary>Hint 2: VPC Endpoint Service</summary>

Create the endpoint service that wraps the NLB:

The key resource is `aws_vpc_endpoint_service`. It needs:
- `acceptance_required = true` (forces manual/Terraform approval of consumer connections)
- `network_load_balancer_arns` referencing your NLB

After creation, Terraform outputs a `service_name` in the format `com.amazonaws.vpce.<region>.<vpce-svc-id>`. The consumer will use this service name to create their endpoint.

To allow specific AWS accounts to discover and connect to your service, create an `aws_vpc_endpoint_service_allowed_principal` with the consumer's account ARN (use `data.aws_caller_identity.current.arn` if both VPCs are in the same account).

</details>

<details>
<summary>Hint 3: Consumer endpoint and acceptance</summary>

On the consumer side:

1. Create **VPC-B** with a private subnet
2. Create an `aws_vpc_endpoint` with:
   - `vpc_endpoint_type = "Interface"`
   - `service_name` = the service name from the provider's endpoint service
   - `vpc_id` = consumer VPC
   - `subnet_ids` = consumer private subnet(s)
   - `security_group_ids` = a security group allowing outbound on port 80

The endpoint will be in `pendingAcceptance` state until the provider approves it.

To accept the connection in Terraform, use `aws_vpc_endpoint_connection_accepter` on the provider side, referencing the endpoint ID. This creates a dependency: the accepter depends on the consumer endpoint existing.

</details>

<details>
<summary>Hint 4: Private DNS and testing</summary>

For private DNS, you need to:

1. Set `private_dns_name` on the `aws_vpc_endpoint_service` (e.g., `myservice.example.com`)
2. Verify domain ownership by creating a TXT record. Use `aws_vpc_endpoint_service_private_dns_verification` after placing the verification TXT record
3. On the consumer endpoint, set `private_dns_enabled = true`

If you do not own a domain, skip private DNS and use the auto-generated endpoint DNS name instead. The auto-generated name looks like `vpce-0123456789abcdef-abc.vpce-svc-0123456789abcdef.us-east-1.vpce.amazonaws.com`.

To test, deploy an EC2 instance in the consumer VPC and curl the endpoint DNS name:
```
curl http://vpce-xxxx.vpce-svc-xxxx.us-east-1.vpce.amazonaws.com
```

If you need SSH/SSM access to instances in private subnets, add a NAT Gateway or SSM VPC endpoints to each VPC.

</details>

## Spot the Bug

A team deployed PrivateLink but the consumer gets "Connection timed out" when curling the endpoint. The provider's NLB configuration looks like this:

```hcl
resource "aws_lb" "provider" {
  name               = "provider-nlb"
  load_balancer_type = "network"
  internal           = false

  subnet_mapping {
    subnet_id = aws_subnet.provider_public.id
  }

  tags = { Name = "provider-nlb" }
}

resource "aws_vpc_endpoint_service" "this" {
  acceptance_required        = true
  network_load_balancer_arns = [aws_lb.provider.arn]

  tags = { Name = "my-privatelink-service" }
}
```

<details>
<summary>Explain the bug</summary>

**The problem:** The NLB is created with `internal = false`, making it **internet-facing**. AWS PrivateLink only works with **internal** Network Load Balancers. When you try to create a VPC Endpoint Service backed by an internet-facing NLB, the API may accept the configuration but the endpoint will never receive traffic -- or AWS may reject it outright with:

```
Error: creating VPC Endpoint Service: InvalidParameter: NLB ARN is for
an internet-facing load balancer. Only internal NLBs are supported.
```

**The fix:** Set `internal = true` and place the NLB in **private** subnets:

```hcl
resource "aws_lb" "provider" {
  name               = "provider-nlb"
  load_balancer_type = "network"
  internal           = true

  subnet_mapping {
    subnet_id = aws_subnet.provider_private.id
  }

  tags = { Name = "provider-nlb" }
}
```

PrivateLink creates ENIs in the consumer's VPC that tunnel traffic to the provider's NLB over the AWS backbone. An internet-facing NLB is designed to accept traffic from the public internet, which is the opposite of what PrivateLink provides.

</details>

## Verify What You Learned

### VPC Endpoint Service exists

```bash
aws ec2 describe-vpc-endpoint-services \
  --filters "Name=service-name,Values=<your-service-name>" \
  --query "ServiceDetails[0].{ServiceName:ServiceName,AcceptanceRequired:AcceptanceRequired,ServiceType:ServiceType[0].ServiceType}" \
  --output table
```

Expected:

```
----------------------------------------------------------------------
|                  DescribeVpcEndpointServices                       |
+---------------------+----------------------------+-----------------+
| AcceptanceRequired  | ServiceName                | ServiceType     |
+---------------------+----------------------------+-----------------+
|  True               | com.amazonaws.vpce.us-...  |  Interface      |
+---------------------+----------------------------+-----------------+
```

### Consumer endpoint is in Available state

```bash
aws ec2 describe-vpc-endpoints \
  --filters "Name=vpc-id,Values=<your-consumer-vpc-id>" \
  --query "VpcEndpoints[0].{State:State,ServiceName:ServiceName,DNSEntries:DnsEntries[0].DnsName}" \
  --output table
```

Expected:

```
--------------------------------------------------------------------------
|                       DescribeVpcEndpoints                             |
+------------------------------------------+-----------+-----------------+
| DNSEntries                               | ServiceName| State          |
+------------------------------------------+-----------+-----------------+
| vpce-xxx.vpce-svc-xxx.us-east-1.vpce... | com.ama...|  available      |
+------------------------------------------+-----------+-----------------+
```

### End-to-end connectivity from consumer

```bash
# From consumer EC2 instance (via SSM Session Manager or SSH):
curl -s http://<your-endpoint-dns-name>
```

Expected: HTML response from the provider's web server (e.g., a directory listing or "Hello from provider").

### NLB is internal

```bash
aws elbv2 describe-load-balancers \
  --names <your-nlb-name> \
  --query "LoadBalancers[0].Scheme" \
  --output text
```

Expected: `internal`

### NLB target is healthy

```bash
aws elbv2 describe-target-health \
  --target-group-arn <your-target-group-arn> \
  --query "TargetHealthDescriptions[0].TargetHealth.State" \
  --output text
```

Expected: `healthy`

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

The VPC Endpoint and Endpoint Service may take a few minutes to delete. If the destroy fails on the first attempt, run it again.

Verify nothing remains:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

You have built a point-to-point private service exposure pattern. In the next exercise, you will explore **VPC Lattice** -- AWS's newer service-to-service networking layer that provides PrivateLink-like connectivity with built-in load balancing, auth policies, and observability across multiple services and VPCs.

## Summary

- **PrivateLink** exposes a single service (not an entire network) across VPC boundaries through ENIs in the consumer's VPC
- The provider creates a **VPC Endpoint Service** backed by an **internal NLB** -- internet-facing NLBs are not supported
- The consumer creates an **Interface VPC Endpoint** that appears as a local ENI with a private IP
- **Acceptance workflow** gives the provider explicit control over who can connect
- **No route table modifications** are needed in either VPC -- traffic flows through the ENI and AWS backbone
- PrivateLink supports **cross-account** and even **cross-region** (via inter-region peering) patterns

## Reference

- [AWS PrivateLink Documentation](https://docs.aws.amazon.com/vpc/latest/privatelink/what-is-privatelink.html)
- [Terraform aws_vpc_endpoint_service](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_endpoint_service)
- [Terraform aws_vpc_endpoint](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_endpoint)
- [Terraform aws_lb (NLB)](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb)

## Additional Resources

- [PrivateLink Architecture Patterns (AWS Blog)](https://aws.amazon.com/blogs/networking-and-content-delivery/how-to-use-aws-privatelink-to-secure-and-scale-web-filtering-using-explicit-proxy/) -- real-world PrivateLink deployment patterns
- [VPC Endpoint Services Quotas](https://docs.aws.amazon.com/vpc/latest/privatelink/vpc-limits-endpoints.html) -- default limits on endpoints, services, and bandwidth
- [Accessing Services Through PrivateLink (AWS Whitepaper)](https://docs.aws.amazon.com/whitepapers/latest/aws-privatelink/what-are-vpc-endpoints.html) -- deep dive into interface vs gateway endpoints
- [NLB Target Types and Health Checks](https://docs.aws.amazon.com/elasticloadbalancing/latest/network/load-balancer-target-groups.html) -- understanding instance vs IP targets and health check configuration

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

data "aws_caller_identity" "current" {}

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

locals {
  az = data.aws_availability_zones.available.names[0]
}
```

### `vpc.tf`

```hcl
# ---------------------------------------------------------------
# Provider VPC
# ---------------------------------------------------------------
resource "aws_vpc" "provider" {
  cidr_block           = "10.10.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags                 = { Name = "provider-vpc" }
}

resource "aws_subnet" "provider_private" {
  vpc_id            = aws_vpc.provider.id
  cidr_block        = "10.10.1.0/24"
  availability_zone = local.az
  tags              = { Name = "provider-private" }
}

resource "aws_subnet" "provider_public" {
  vpc_id                  = aws_vpc.provider.id
  cidr_block              = "10.10.10.0/24"
  availability_zone       = local.az
  map_public_ip_on_launch = true
  tags                    = { Name = "provider-public" }
}

resource "aws_internet_gateway" "provider" {
  vpc_id = aws_vpc.provider.id
  tags   = { Name = "provider-igw" }
}

resource "aws_eip" "provider_nat" {
  domain = "vpc"
}

resource "aws_nat_gateway" "provider" {
  allocation_id = aws_eip.provider_nat.id
  subnet_id     = aws_subnet.provider_public.id
  depends_on    = [aws_internet_gateway.provider]
  tags          = { Name = "provider-nat" }
}

resource "aws_route_table" "provider_public" {
  vpc_id = aws_vpc.provider.id
  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.provider.id
  }
  tags = { Name = "provider-public-rt" }
}

resource "aws_route_table_association" "provider_public" {
  subnet_id      = aws_subnet.provider_public.id
  route_table_id = aws_route_table.provider_public.id
}

resource "aws_route_table" "provider_private" {
  vpc_id = aws_vpc.provider.id
  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.provider.id
  }
  tags = { Name = "provider-private-rt" }
}

resource "aws_route_table_association" "provider_private" {
  subnet_id      = aws_subnet.provider_private.id
  route_table_id = aws_route_table.provider_private.id
}

# ---------------------------------------------------------------
# Consumer VPC
# ---------------------------------------------------------------
resource "aws_vpc" "consumer" {
  cidr_block           = "10.20.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags                 = { Name = "consumer-vpc" }
}

resource "aws_subnet" "consumer_private" {
  vpc_id            = aws_vpc.consumer.id
  cidr_block        = "10.20.1.0/24"
  availability_zone = local.az
  tags              = { Name = "consumer-private" }
}
```

### `security.tf`

```hcl
# ---------------------------------------------------------------
# Provider security group
# ---------------------------------------------------------------
resource "aws_security_group" "provider_web" {
  name        = "provider-web-sg"
  description = "Allow HTTP from VPC"
  vpc_id      = aws_vpc.provider.id
  tags        = { Name = "provider-web-sg" }
}

resource "aws_vpc_security_group_ingress_rule" "provider_http" {
  security_group_id = aws_security_group.provider_web.id
  cidr_ipv4         = "10.10.0.0/16"
  from_port         = 80
  to_port           = 80
  ip_protocol       = "tcp"
}

resource "aws_vpc_security_group_egress_rule" "provider_all" {
  security_group_id = aws_security_group.provider_web.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}

# ---------------------------------------------------------------
# Consumer security groups
# ---------------------------------------------------------------
resource "aws_security_group" "consumer_endpoint" {
  name        = "consumer-endpoint-sg"
  description = "Allow HTTP to endpoint ENIs"
  vpc_id      = aws_vpc.consumer.id
  tags        = { Name = "consumer-endpoint-sg" }
}

resource "aws_vpc_security_group_ingress_rule" "consumer_http" {
  security_group_id = aws_security_group.consumer_endpoint.id
  cidr_ipv4         = "10.20.0.0/16"
  from_port         = 80
  to_port           = 80
  ip_protocol       = "tcp"
}

resource "aws_vpc_security_group_egress_rule" "consumer_all" {
  security_group_id = aws_security_group.consumer_endpoint.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}

resource "aws_security_group" "consumer_test" {
  name        = "consumer-test-sg"
  description = "Allow outbound for testing"
  vpc_id      = aws_vpc.consumer.id
  tags        = { Name = "consumer-test-sg" }
}

resource "aws_vpc_security_group_egress_rule" "consumer_test_all" {
  security_group_id = aws_security_group.consumer_test.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}
```

### `compute.tf`

```hcl
# ---------------------------------------------------------------
# Provider EC2 (web server)
# ---------------------------------------------------------------
resource "aws_instance" "provider" {
  ami                    = data.aws_ami.amazon_linux_2023.id
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.provider_private.id
  vpc_security_group_ids = [aws_security_group.provider_web.id]
  user_data = <<-EOF
    #!/bin/bash
    dnf install -y httpd
    echo "Hello from the provider via PrivateLink" > /var/www/html/index.html
    systemctl enable httpd
    systemctl start httpd
  EOF
  tags = { Name = "provider-web" }
}

# ---------------------------------------------------------------
# Consumer EC2 (for testing)
# ---------------------------------------------------------------
resource "aws_instance" "consumer" {
  ami                    = data.aws_ami.amazon_linux_2023.id
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.consumer_private.id
  vpc_security_group_ids = [aws_security_group.consumer_test.id]
  tags                   = { Name = "consumer-test" }
}
```

### `alb.tf`

```hcl
# ---------------------------------------------------------------
# Internal NLB + Target Group
# ---------------------------------------------------------------
resource "aws_lb" "provider" {
  name               = "provider-nlb"
  load_balancer_type = "network"
  internal           = true

  subnet_mapping {
    subnet_id = aws_subnet.provider_private.id
  }

  tags = { Name = "provider-nlb" }
}

resource "aws_lb_target_group" "provider" {
  name        = "provider-tg"
  port        = 80
  protocol    = "TCP"
  vpc_id      = aws_vpc.provider.id
  target_type = "instance"

  health_check {
    protocol            = "TCP"
    port                = "80"
    healthy_threshold   = 3
    unhealthy_threshold = 3
    interval            = 10
  }

  tags = { Name = "provider-tg" }
}

resource "aws_lb_target_group_attachment" "provider" {
  target_group_arn = aws_lb_target_group.provider.arn
  target_id        = aws_instance.provider.id
  port             = 80
}

resource "aws_lb_listener" "provider" {
  load_balancer_arn = aws_lb.provider.arn
  port              = 80
  protocol          = "TCP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.provider.arn
  }
}
```

### `privatelink.tf`

```hcl
# ---------------------------------------------------------------
# VPC Endpoint Service (provider side)
# ---------------------------------------------------------------
resource "aws_vpc_endpoint_service" "this" {
  acceptance_required        = true
  network_load_balancer_arns = [aws_lb.provider.arn]
  tags                       = { Name = "my-privatelink-service" }
}

resource "aws_vpc_endpoint_service_allowed_principal" "this" {
  vpc_endpoint_service_id = aws_vpc_endpoint_service.this.id
  principal_arn           = data.aws_caller_identity.current.arn
}

# ---------------------------------------------------------------
# Interface VPC Endpoint (consumer side)
# ---------------------------------------------------------------
resource "aws_vpc_endpoint" "consumer" {
  vpc_id              = aws_vpc.consumer.id
  service_name        = aws_vpc_endpoint_service.this.service_name
  vpc_endpoint_type   = "Interface"
  subnet_ids          = [aws_subnet.consumer_private.id]
  security_group_ids  = [aws_security_group.consumer_endpoint.id]
  private_dns_enabled = false

  tags = { Name = "consumer-endpoint" }
}

# ---------------------------------------------------------------
# Accept the connection (provider side)
# ---------------------------------------------------------------
resource "aws_vpc_endpoint_connection_accepter" "this" {
  vpc_endpoint_service_id = aws_vpc_endpoint_service.this.id
  vpc_endpoint_id         = aws_vpc_endpoint.consumer.id
}
```

### `outputs.tf`

```hcl
output "endpoint_service_name" {
  value = aws_vpc_endpoint_service.this.service_name
}

output "endpoint_dns_name" {
  value = aws_vpc_endpoint.consumer.dns_entry[0].dns_name
}

output "consumer_instance_id" {
  value = aws_instance.consumer.id
}

output "nlb_dns_name" {
  value = aws_lb.provider.dns_name
}
```

</details>

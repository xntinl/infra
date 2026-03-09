# 23. PrivateLink Service Exposure

<!--
difficulty: intermediate
concepts: [privatelink, vpc-endpoint-service, nlb, interface-endpoint, cross-vpc-service, endpoint-acceptance, availability-zone-mapping]
tools: [terraform, aws-cli]
estimated_time: 50m
bloom_level: apply, analyze
prerequisites: [17-vpc-subnets-route-tables-igw, 22-vpc-endpoints-gateway-vs-interface]
aws_cost: ~$0.05/hr
-->

> **AWS Cost Warning:** NLB costs ~$0.0225/hr plus LCU charges. Interface endpoint costs ~$0.01/hr per AZ. EC2 t3.micro ~$0.0104/hr. Total ~$0.05/hr. Remember to run `terraform destroy` when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| Terraform >= 1.7 installed | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Understanding of VPC endpoints (exercise 22) | Gateway vs Interface distinction |
| Understanding of NLB (exercise 02 or equivalent) | Layer 4 load balancing basics |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Design** a PrivateLink architecture that exposes a service from a provider VPC to a consumer VPC without VPC peering.
2. **Construct** a VPC endpoint service backed by a Network Load Balancer using Terraform.
3. **Implement** the consumer-side Interface endpoint that connects to the provider's endpoint service.
4. **Configure** endpoint acceptance to control which AWS accounts can connect to your service.
5. **Explain** why the NLB and endpoint service must be in the same Availability Zones and what happens when they are not.
6. **Evaluate** when to use PrivateLink versus VPC peering for cross-VPC communication.

---

## Why This Matters

PrivateLink solves a specific architectural problem: exposing a service to another VPC (or another AWS account) without opening up the entire network. With VPC peering, both VPCs can see all of each other's resources. With PrivateLink, the consumer can only reach the specific service exposed through the endpoint -- nothing else. This is the principle of least privilege applied to networking.

On the SAA-C03 exam, PrivateLink appears in scenarios like: "A SaaS provider needs to expose an API to customers in their own VPCs without requiring customers to allow their CIDR ranges" or "Two business units need to share a microservice without full network connectivity." The answer is PrivateLink when the requirement is one-directional service access (consumer calls provider), not bidirectional network connectivity.

The architecture has three components: (1) a Network Load Balancer in the provider VPC that fronts the service, (2) a VPC endpoint service that wraps the NLB and makes it available via PrivateLink, and (3) an Interface endpoint in the consumer VPC that connects to the endpoint service. The consumer sees the service as an ENI in their own VPC with a private IP -- they never need to know the provider's CIDR range, VPC ID, or network topology.

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
```

### `vpc.tf`

```hcl
data "aws_availability_zones" "available" {
  state = "available"
}

data "aws_caller_identity" "current" {}

# ==================================================================
# PROVIDER VPC: hosts the service behind an NLB
# ==================================================================
resource "aws_vpc" "provider" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags                 = { Name = "privatelink-demo-provider" }
}

resource "aws_subnet" "provider_a" {
  vpc_id            = aws_vpc.provider.id
  cidr_block        = "10.0.1.0/24"
  availability_zone = data.aws_availability_zones.available.names[0]
  tags              = { Name = "privatelink-provider-a" }
}

resource "aws_subnet" "provider_b" {
  vpc_id            = aws_vpc.provider.id
  cidr_block        = "10.0.2.0/24"
  availability_zone = data.aws_availability_zones.available.names[1]
  tags              = { Name = "privatelink-provider-b" }
}

# ==================================================================
# CONSUMER VPC: connects to the service via Interface endpoint
# ==================================================================
resource "aws_vpc" "consumer" {
  cidr_block           = "10.1.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags                 = { Name = "privatelink-demo-consumer" }
}

resource "aws_subnet" "consumer_a" {
  vpc_id            = aws_vpc.consumer.id
  cidr_block        = "10.1.1.0/24"
  availability_zone = data.aws_availability_zones.available.names[0]
  tags              = { Name = "privatelink-consumer-a" }
}

resource "aws_subnet" "consumer_b" {
  vpc_id            = aws_vpc.consumer.id
  cidr_block        = "10.1.2.0/24"
  availability_zone = data.aws_availability_zones.available.names[1]
  tags              = { Name = "privatelink-consumer-b" }
}

# ============================================================
# TODO 1: VPC Endpoint Service (Provider Side)  [vpc.tf]
# ============================================================
# Create the endpoint service that wraps the NLB.
#
# Requirements:
#   - Resource: aws_vpc_endpoint_service
#   - acceptance_required = true (manually approve connections)
#   - network_load_balancer_arns = [NLB ARN]
#   - allowed_principals = [current account ARN]
#
# acceptance_required = true means the provider must approve
# each consumer connection. This is critical for SaaS providers
# who need to control which customers can connect.
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_endpoint_service
# ============================================================


# ============================================================
# TODO 2: Consumer Interface Endpoint  [vpc.tf]
# ============================================================
# Create an Interface endpoint in the consumer VPC that
# connects to the provider's endpoint service.
#
# Requirements:
#   - Resource: aws_vpc_endpoint
#   - vpc_id = consumer VPC ID
#   - service_name = endpoint service's service_name output
#   - vpc_endpoint_type = "Interface"
#   - subnet_ids = [consumer subnet IDs]
#   - security_group_ids = [consumer endpoint SG ID]
#
# The consumer references the provider's service by its
# service name (com.amazonaws.vpce.us-east-1.vpce-svc-xxxxx).
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_endpoint
# ============================================================


# ============================================================
# TODO 3: Accept the Endpoint Connection  [vpc.tf]
# ============================================================
# Since acceptance_required = true, the provider must accept
# the consumer's connection.
#
# Requirements:
#   - Resource: aws_vpc_endpoint_connection_accepter
#   - vpc_endpoint_service_id = endpoint service ID
#   - vpc_endpoint_id = consumer endpoint ID
#
# In cross-account scenarios, the provider would approve
# connections via the console, CLI, or a Lambda function
# triggered by EventBridge.
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_endpoint_connection_accepter
# ============================================================
```

### `alb.tf`

```hcl
# NLB fronting the service (required for PrivateLink)
resource "aws_lb" "provider" {
  name               = "privatelink-demo-nlb"
  internal           = true
  load_balancer_type = "network"
  subnets            = [aws_subnet.provider_a.id, aws_subnet.provider_b.id]

  tags = { Name = "privatelink-demo-nlb" }
}

resource "aws_lb_target_group" "provider" {
  name        = "privatelink-demo-tg"
  port        = 80
  protocol    = "TCP"
  vpc_id      = aws_vpc.provider.id
  target_type = "ip"

  health_check {
    protocol            = "TCP"
    healthy_threshold   = 2
    unhealthy_threshold = 2
    interval            = 10
  }
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

### `security.tf`

```hcl
resource "aws_security_group" "consumer_endpoint" {
  name_prefix = "privatelink-consumer-ep-"
  vpc_id      = aws_vpc.consumer.id
  description = "Allow traffic to PrivateLink endpoint"

  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = [aws_vpc.consumer.cidr_block]
    description = "HTTP from consumer VPC"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "privatelink-consumer-ep-sg" }
}
```

### `outputs.tf`

```hcl
output "provider_vpc_id" {
  value = aws_vpc.provider.id
}

output "consumer_vpc_id" {
  value = aws_vpc.consumer.id
}

output "nlb_arn" {
  value = aws_lb.provider.arn
}
```

---

## Spot the Bug

A team creates a PrivateLink service but consumers report "No available AZs" when creating their endpoint:

```hcl
# Provider NLB in us-east-1a only
resource "aws_lb" "provider" {
  name               = "service-nlb"
  internal           = true
  load_balancer_type = "network"
  subnets            = [aws_subnet.provider_a.id]  # Only AZ-a
}

resource "aws_vpc_endpoint_service" "this" {
  network_load_balancer_arns = [aws_lb.provider.arn]
  acceptance_required        = false
}

# Consumer tries to create endpoint in us-east-1b
resource "aws_vpc_endpoint" "consumer" {
  vpc_id              = aws_vpc.consumer.id
  service_name        = aws_vpc_endpoint_service.this.service_name
  vpc_endpoint_type   = "Interface"
  subnet_ids          = [aws_subnet.consumer_b.id]  # Only AZ-b
  security_group_ids  = [aws_security_group.consumer_endpoint.id]
}
```

<details>
<summary>Explain the bug</summary>

**The NLB and the consumer endpoint must have overlapping Availability Zones.** The NLB is deployed only in `us-east-1a`, but the consumer endpoint is in `us-east-1b`. PrivateLink creates cross-AZ connections through the NLB's AZ presence. If the NLB does not have a node in the consumer's AZ, the endpoint creation fails with "No available AZs."

**The fix:** Deploy the NLB in all AZs where consumers might create endpoints:

```hcl
resource "aws_lb" "provider" {
  name               = "service-nlb"
  internal           = true
  load_balancer_type = "network"
  subnets            = [
    aws_subnet.provider_a.id,  # AZ-a
    aws_subnet.provider_b.id,  # AZ-b
  ]
}
```

Or, the consumer creates their endpoint in the AZ where the NLB exists:

```hcl
resource "aws_vpc_endpoint" "consumer" {
  subnet_ids = [aws_subnet.consumer_a.id]  # Same AZ as NLB
}
```

Best practice: deploy the NLB in all AZs in the region to maximize consumer flexibility. The NLB charges per AZ, but the operational simplicity is worth the cost for shared services.

</details>

---

## Solutions

<details>
<summary>vpc.tf -- TODO 1: VPC Endpoint Service</summary>

```hcl
resource "aws_vpc_endpoint_service" "this" {
  acceptance_required        = true
  network_load_balancer_arns = [aws_lb.provider.arn]
  allowed_principals         = ["arn:aws:iam::${data.aws_caller_identity.current.account_id}:root"]

  tags = { Name = "privatelink-demo-service" }
}
```

`acceptance_required = true` means every new endpoint connection must be explicitly approved. This gives the provider control over who connects. `allowed_principals` restricts which AWS accounts can even create endpoints against this service.

</details>

<details>
<summary>vpc.tf -- TODO 2: Consumer Interface Endpoint</summary>

```hcl
resource "aws_vpc_endpoint" "consumer" {
  vpc_id              = aws_vpc.consumer.id
  service_name        = aws_vpc_endpoint_service.this.service_name
  vpc_endpoint_type   = "Interface"
  subnet_ids          = [aws_subnet.consumer_a.id, aws_subnet.consumer_b.id]
  security_group_ids  = [aws_security_group.consumer_endpoint.id]

  tags = { Name = "privatelink-demo-consumer-endpoint" }
}
```

The consumer endpoint creates ENIs in the specified subnets. These ENIs get private IPs from the consumer's CIDR range. The consumer application connects to these private IPs (or the endpoint DNS name) to reach the provider's service.

</details>

<details>
<summary>vpc.tf -- TODO 3: Accept the Connection</summary>

```hcl
resource "aws_vpc_endpoint_connection_accepter" "this" {
  vpc_endpoint_service_id = aws_vpc_endpoint_service.this.id
  vpc_endpoint_id         = aws_vpc_endpoint.consumer.id
}
```

After acceptance, the endpoint state changes from `pendingAcceptance` to `available`. Without acceptance, the consumer endpoint stays in `pendingAcceptance` indefinitely and cannot route traffic.

</details>

---

## PrivateLink vs VPC Peering Decision Framework

| Criterion | PrivateLink | VPC Peering |
|-----------|-------------|-------------|
| **Direction** | One-directional (consumer to provider) | Bidirectional |
| **Network exposure** | Only the service endpoint | Entire VPC CIDR |
| **CIDR overlap** | Allowed (no CIDR restriction) | Must not overlap |
| **Cross-account** | Yes (with acceptance) | Yes (with acceptance) |
| **Scalability** | Thousands of consumers per service | One peering per VPC pair |
| **Cost** | ~$0.01/hr per endpoint AZ + $0.01/GB | Free (data transfer only) |
| **Use case** | SaaS service exposure, shared microservices | Full network connectivity between VPCs |

---

## Verify What You Learned

```bash
# Verify endpoint service exists
aws ec2 describe-vpc-endpoint-services \
  --filters "Name=tag:Name,Values=privatelink-demo-service" \
  --query "ServiceDetails[0].{Name:ServiceName,Type:ServiceType[0].ServiceType,AZs:AvailabilityZones}" \
  --output json
```

Expected: Service with type "Interface" available in 2 AZs.

```bash
# Verify consumer endpoint is accepted and available
aws ec2 describe-vpc-endpoints \
  --filters "Name=tag:Name,Values=privatelink-demo-consumer-endpoint" \
  --query "VpcEndpoints[0].{State:State,DNS:DnsEntries[0].DnsName}" \
  --output table
```

Expected: State=available with a DNS name.

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify:

```bash
terraform state list
```

Expected: no output (empty state).

---

## What's Next

PrivateLink provides service-level connectivity between VPCs. In the next exercise, you will deploy **Transit Gateway** for hub-and-spoke network connectivity, connecting multiple VPCs through a central router with route table segmentation -- the scalable alternative to VPC peering for large multi-VPC architectures.

---

## Summary

- **PrivateLink** exposes a specific service from one VPC to another without full network connectivity
- The architecture requires an **NLB** in the provider VPC, a **VPC endpoint service**, and an **Interface endpoint** in the consumer VPC
- The consumer sees the service as an **ENI with a private IP** in their own VPC -- they never learn the provider's network topology
- **acceptance_required** controls whether connections must be approved, critical for SaaS providers
- The NLB and endpoint must share **overlapping Availability Zones** or the endpoint fails to create
- PrivateLink **allows overlapping CIDRs** between provider and consumer (unlike VPC peering)
- PrivateLink is **one-directional**: the consumer can reach the provider, but not vice versa
- Use PrivateLink for **service exposure**; use VPC peering for **full network connectivity**

## Reference

- [AWS PrivateLink](https://docs.aws.amazon.com/vpc/latest/privatelink/what-is-privatelink.html)
- [VPC Endpoint Services](https://docs.aws.amazon.com/vpc/latest/privatelink/create-endpoint-service.html)
- [Terraform aws_vpc_endpoint_service](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_endpoint_service)
- [Terraform aws_vpc_endpoint_connection_accepter](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_endpoint_connection_accepter)

## Additional Resources

- [PrivateLink Cross-Account Patterns](https://docs.aws.amazon.com/vpc/latest/privatelink/configure-endpoint-service.html) -- managing allowed principals and acceptance for multi-account setups
- [PrivateLink Pricing](https://aws.amazon.com/privatelink/pricing/) -- per-AZ hourly cost and data processing fees
- [NLB with PrivateLink](https://docs.aws.amazon.com/elasticloadbalancing/latest/network/load-balancer-target-groups.html) -- supported target types and health check configuration
- [PrivateLink for SaaS](https://docs.aws.amazon.com/vpc/latest/privatelink/privatelink-share-your-services.html) -- reference architecture for SaaS providers offering PrivateLink access

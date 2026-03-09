# 26. Site-to-Site VPN with Redundant Tunnels

<!--
difficulty: advanced
concepts: [site-to-site-vpn, vpn-gateway, customer-gateway, ipsec-tunnel, bgp-routing, tunnel-redundancy, vpn-over-tgw, active-active-tunnels, vpn-monitoring]
tools: [terraform, aws-cli]
estimated_time: 55m
bloom_level: evaluate
prerequisites: [17-vpc-subnets-route-tables-igw, 24-transit-gateway-hub-spoke-design, 25-direct-connect-virtual-interfaces]
aws_cost: ~$0.05/hr
-->

> **AWS Cost Warning:** Site-to-Site VPN costs $0.05/hr per VPN connection. Data transfer out costs $0.09/GB. This exercise creates 1 VPN connection (2 tunnels). Total ~$0.05/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Understanding of Transit Gateway (exercise 24)
- Conceptual understanding of Direct Connect (exercise 25)

## Learning Objectives

After completing this exercise, you will be able to:

- **Design** a Site-to-Site VPN architecture with redundant tunnels for high availability
- **Explain** why AWS always provisions 2 tunnels per VPN connection and the active/active vs active/passive modes
- **Configure** a VPN Gateway, Customer Gateway, and VPN connection using Terraform
- **Evaluate** BGP dynamic routing versus static routing for VPN tunnel failover
- **Analyze** the single-tunnel anti-pattern and why it violates AWS redundancy requirements
- **Describe** how VPN connections attach to Transit Gateway for multi-VPC access

## Why Site-to-Site VPN Redundancy Matters

AWS Site-to-Site VPN creates an encrypted IPsec tunnel between your on-premises network and your AWS VPC. Every VPN connection includes two tunnels, each terminating on a different AWS endpoint in a different Availability Zone. This dual-tunnel design is AWS's built-in redundancy -- if one tunnel fails (maintenance, AZ issue, routing problem), traffic automatically shifts to the other tunnel.

The SAA-C03 exam tests three critical aspects. First, the two-tunnel architecture: every VPN connection has two tunnels, and your on-premises router should be configured to use both. Running a single tunnel (ignoring the second) creates a single point of failure that AWS explicitly warns against. Second, BGP versus static routing: BGP dynamically exchanges routes between your network and AWS, enabling automatic failover when a tunnel goes down. Static routing requires manual route table updates and does not support automatic failover. Third, VPN as a DX backup: the exam frequently presents scenarios where VPN serves as a backup for Direct Connect, with BGP handling failover between the two paths.

Each tunnel supports up to 1.25 Gbps throughput. For higher bandwidth, use ECMP (Equal-Cost Multi-Path) with Transit Gateway, which load-balances traffic across multiple VPN connections (each with 2 tunnels). With 4 VPN connections, you get 10 Gbps aggregate bandwidth.

## The Challenge

Deploy a Site-to-Site VPN connection with proper redundancy. Since you cannot create a real on-premises router in a lab, this exercise simulates the customer gateway endpoint and focuses on the AWS-side configuration.

### Architecture

```
On-Premises Network
  |
  |-- Customer Gateway (your router, BGP ASN 65000)
  |
  +==== Tunnel 1 (IPsec) ====> VPN Endpoint AZ-a
  |                               |
  +==== Tunnel 2 (IPsec) ====> VPN Endpoint AZ-b
                                  |
                            Transit Gateway
                                  |
                    +-------------+-------------+
                    |             |             |
                  VPC-1        VPC-2        VPC-3
```

### Requirements

1. Customer Gateway with BGP ASN 65000
2. Transit Gateway for multi-VPC access
3. VPN connection with 2 tunnels (AWS default)
4. BGP dynamic routing (not static)
5. Both tunnels configured on the customer gateway (active/active)

## Hints

<details>
<summary>Hint 1: Customer Gateway Configuration</summary>

The Customer Gateway represents your on-premises VPN device in AWS. You specify your device's public IP and BGP ASN:

```hcl
resource "aws_customer_gateway" "this" {
  bgp_asn    = 65000
  ip_address = "198.51.100.1"  # Your on-premises router's public IP
  type       = "ipsec.1"

  tags = { Name = "vpn-demo-cgw" }
}
```

Key fields:
- `bgp_asn`: Your router's BGP Autonomous System Number. Use a private ASN (64512-65534) unless you have a public ASN.
- `ip_address`: The public IP of your VPN endpoint. Must be a static IP reachable from the internet.
- `type`: Always `"ipsec.1"` (the only supported type).

</details>

<details>
<summary>Hint 2: VPN Connection with Transit Gateway</summary>

Attach the VPN to a Transit Gateway instead of a Virtual Private Gateway. This gives you access to all VPCs attached to the TGW through a single VPN connection:

```hcl
resource "aws_ec2_transit_gateway" "this" {
  description                     = "VPN demo TGW"
  default_route_table_association = "enable"
  default_route_table_propagation = "enable"
  vpn_ecmp_support                = "enable"  # Enable for multi-VPN load balancing

  tags = { Name = "vpn-demo-tgw" }
}

resource "aws_vpn_connection" "this" {
  customer_gateway_id = aws_customer_gateway.this.id
  transit_gateway_id  = aws_ec2_transit_gateway.this.id
  type                = "ipsec.1"

  # Use BGP for dynamic routing (automatic failover)
  static_routes_only = false

  # Optional: specify tunnel options
  tunnel1_inside_cidr   = "169.254.10.0/30"
  tunnel2_inside_cidr   = "169.254.10.4/30"
  tunnel1_preshared_key = "YourPreSharedKey1ForTunnel1"
  tunnel2_preshared_key = "YourPreSharedKey2ForTunnel2"

  tags = { Name = "vpn-demo-connection" }
}
```

`vpn_ecmp_support = "enable"` on the TGW allows ECMP across multiple VPN connections. With 4 VPN connections (8 tunnels), you get up to 10 Gbps aggregate bandwidth.

</details>

<details>
<summary>Hint 3: Downloading VPN Configuration</summary>

After creating the VPN connection, download the configuration for your on-premises router:

```bash
VPN_ID=$(aws ec2 describe-vpn-connections \
  --filters "Name=tag:Name,Values=vpn-demo-connection" \
  --query "VpnConnections[0].VpnConnectionId" \
  --output text)

# Download configuration for your router type
aws ec2 describe-vpn-connections \
  --vpn-connection-ids "$VPN_ID" \
  --query "VpnConnections[0].{
    Tunnel1: Options.TunnelOptions[0].{
      OutsideIP: OutsideIpAddress,
      InsideCIDR: TunnelInsideCidr,
      PreSharedKey: PreSharedKey
    },
    Tunnel2: Options.TunnelOptions[1].{
      OutsideIP: OutsideIpAddress,
      InsideCIDR: TunnelInsideCidr,
      PreSharedKey: PreSharedKey
    }
  }" \
  --output json
```

This provides:
- **Outside IP**: AWS's public IP for each tunnel endpoint (you connect to these)
- **Inside CIDR**: /30 link addresses for BGP peering inside the tunnel
- **Pre-shared key**: IPsec authentication key for each tunnel

Both tunnels must be configured on your router. Each tunnel terminates on a different AWS endpoint in a different AZ.

</details>

<details>
<summary>Hint 4: Monitoring VPN Tunnel Status</summary>

Monitor tunnel status to detect single-tunnel degradation:

```bash
# Check tunnel status
aws ec2 describe-vpn-connections \
  --vpn-connection-ids "$VPN_ID" \
  --query "VpnConnections[0].VgwTelemetry[*].{
    OutsideIP:OutsideIpAddress,
    Status:Status,
    StatusMessage:StatusMessage,
    CertificateArn:CertificateArn
  }" \
  --output table
```

Create CloudWatch alarms for tunnel state changes:

```hcl
resource "aws_cloudwatch_metric_alarm" "tunnel_down" {
  alarm_name          = "vpn-demo-tunnel-down"
  comparison_operator = "LessThanThreshold"
  evaluation_periods  = 2
  metric_name         = "TunnelState"
  namespace           = "AWS/VPN"
  period              = 60
  statistic           = "Maximum"
  threshold           = 1
  alarm_description   = "VPN tunnel is down"

  dimensions = {
    VpnId = aws_vpn_connection.this.id
  }
}
```

`TunnelState` = 1 means UP, 0 means DOWN. Alert when either tunnel goes down so you can investigate before the second tunnel fails.

</details>

<details>
<summary>Hint 5: VPN Bandwidth and ECMP</summary>

Single VPN connection bandwidth:
- Each tunnel: up to **1.25 Gbps**
- With 2 tunnels active/active: traffic is distributed but total per-flow is still 1.25 Gbps (ECMP distributes by flow, not by packet)

For higher bandwidth, create multiple VPN connections to the same TGW with ECMP:

| VPN Connections | Tunnels | Aggregate Bandwidth |
|----------------|---------|-------------------|
| 1 | 2 | 1.25 Gbps |
| 2 | 4 | 2.5 Gbps |
| 4 | 8 | 5 Gbps |
| 8 | 16 | 10 Gbps |

Each additional VPN connection adds $0.05/hr (~$36/month).

</details>

## Spot the Bug

A team configures a VPN connection but only configures one of the two tunnels on their on-premises router:

```
On-Premises Router Configuration:
  - Tunnel 1: IPsec to 52.1.2.3 (configured, UP)
  - Tunnel 2: IPsec to 52.4.5.6 (NOT configured)
```

```hcl
# AWS side looks correct
resource "aws_vpn_connection" "this" {
  customer_gateway_id = aws_customer_gateway.this.id
  transit_gateway_id  = aws_ec2_transit_gateway.this.id
  type                = "ipsec.1"
  static_routes_only  = false
}
```

<details>
<summary>Explain the bug</summary>

**The team configured only one tunnel, creating a single point of failure.** AWS provisions two tunnels in different AZs for every VPN connection. When only one is configured:

1. **No redundancy:** If Tunnel 1's AZ experiences an issue (or AWS performs maintenance on the tunnel endpoint), all VPN connectivity is lost until the AZ recovers or Tunnel 2 is configured.

2. **AWS performs periodic maintenance** on VPN tunnel endpoints. During maintenance, a tunnel goes down briefly. With both tunnels configured, traffic seamlessly shifts to the other tunnel. With only one tunnel, maintenance causes downtime.

3. **No ECMP benefit:** With both tunnels active, BGP can distribute traffic across both for better aggregate throughput.

**The fix:** Configure BOTH tunnels on the on-premises router:

```
On-Premises Router Configuration:
  - Tunnel 1: IPsec to 52.1.2.3 (configured, UP, BGP ASN 64512)
  - Tunnel 2: IPsec to 52.4.5.6 (configured, UP, BGP ASN 64512)
  - BGP: Both tunnels advertise the same routes
  - ECMP: Enable equal-cost multi-path for active/active
```

Active/active mode: both tunnels carry traffic simultaneously with BGP ECMP. If one fails, all traffic shifts to the other within the BGP convergence time (~30 seconds).

Active/passive mode: one tunnel carries all traffic, the other is standby. Configure AS-PATH prepending to make one tunnel preferred. Failover occurs when the primary tunnel goes down.

For most workloads, **active/active is recommended** -- it uses both tunnels for bandwidth and provides the fastest failover.

</details>

## Full Solution

<details>
<summary>Complete Terraform Configuration</summary>

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
  default     = "vpn-demo"
}
```

### `vpc.tf`

```hcl
data "aws_availability_zones" "available" {
  state = "available"
}

resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags                 = { Name = var.project_name }
}

resource "aws_subnet" "a" {
  vpc_id            = aws_vpc.this.id
  cidr_block        = "10.0.1.0/24"
  availability_zone = data.aws_availability_zones.available.names[0]
  tags              = { Name = "${var.project_name}-a" }
}

resource "aws_subnet" "b" {
  vpc_id            = aws_vpc.this.id
  cidr_block        = "10.0.2.0/24"
  availability_zone = data.aws_availability_zones.available.names[1]
  tags              = { Name = "${var.project_name}-b" }
}

resource "aws_route_table" "this" {
  vpc_id = aws_vpc.this.id

  route {
    cidr_block         = "192.168.0.0/16"  # On-premises CIDR
    transit_gateway_id = aws_ec2_transit_gateway.this.id
  }

  tags = { Name = "${var.project_name}-rt" }
}

resource "aws_route_table_association" "a" {
  subnet_id      = aws_subnet.a.id
  route_table_id = aws_route_table.this.id
}

resource "aws_route_table_association" "b" {
  subnet_id      = aws_subnet.b.id
  route_table_id = aws_route_table.this.id
}
```

### `vpn.tf`

```hcl
# Transit Gateway
resource "aws_ec2_transit_gateway" "this" {
  description                     = "${var.project_name} TGW"
  default_route_table_association = "enable"
  default_route_table_propagation = "enable"
  vpn_ecmp_support                = "enable"
  dns_support                     = "enable"

  tags = { Name = "${var.project_name}-tgw" }
}

resource "aws_ec2_transit_gateway_vpc_attachment" "this" {
  transit_gateway_id = aws_ec2_transit_gateway.this.id
  vpc_id             = aws_vpc.this.id
  subnet_ids         = [aws_subnet.a.id, aws_subnet.b.id]

  tags = { Name = "${var.project_name}-tgw-attachment" }
}

# Customer Gateway (represents your on-premises router)
resource "aws_customer_gateway" "this" {
  bgp_asn    = 65000
  ip_address = "198.51.100.1"  # Replace with actual public IP
  type       = "ipsec.1"

  tags = { Name = "${var.project_name}-cgw" }
}

# VPN Connection (creates 2 tunnels automatically)
resource "aws_vpn_connection" "this" {
  customer_gateway_id = aws_customer_gateway.this.id
  transit_gateway_id  = aws_ec2_transit_gateway.this.id
  type                = "ipsec.1"
  static_routes_only  = false  # Use BGP for dynamic routing

  tags = { Name = "${var.project_name}-connection" }
}
```

### `monitoring.tf`

```hcl
resource "aws_cloudwatch_metric_alarm" "tunnel_state" {
  alarm_name          = "${var.project_name}-tunnel-down"
  comparison_operator = "LessThanThreshold"
  evaluation_periods  = 2
  metric_name         = "TunnelState"
  namespace           = "AWS/VPN"
  period              = 300
  statistic           = "Minimum"
  threshold           = 1
  alarm_description   = "At least one VPN tunnel is down"

  dimensions = {
    VpnId = aws_vpn_connection.this.id
  }
}
```

### `outputs.tf`

```hcl
output "vpn_connection_id" {
  value = aws_vpn_connection.this.id
}

output "customer_gateway_id" {
  value = aws_customer_gateway.this.id
}

output "transit_gateway_id" {
  value = aws_ec2_transit_gateway.this.id
}

output "tunnel1_address" {
  value     = aws_vpn_connection.this.tunnel1_address
  sensitive = true
}

output "tunnel2_address" {
  value     = aws_vpn_connection.this.tunnel2_address
  sensitive = true
}
```

</details>

## Verify What You Learned

```bash
# Verify VPN connection exists with 2 tunnels
aws ec2 describe-vpn-connections \
  --filters "Name=tag:Name,Values=vpn-demo-connection" \
  --query "VpnConnections[0].{Id:VpnConnectionId,State:State,Tunnels:VgwTelemetry[*].{IP:OutsideIpAddress,Status:Status}}" \
  --output json
```

Expected: State=available, 2 tunnel entries (status may be DOWN since no real on-premises router is connected).

```bash
# Verify Customer Gateway
aws ec2 describe-customer-gateways \
  --filters "Name=tag:Name,Values=vpn-demo-cgw" \
  --query "CustomerGateways[0].{Id:CustomerGatewayId,BGP:BgpAsn,IP:IpAddress}" \
  --output table
```

Expected: BGP ASN 65000, IP 198.51.100.1.

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

You configured a redundant Site-to-Site VPN with proper dual-tunnel architecture. In the next exercise, you will deploy **AWS Network Firewall** for centralized traffic inspection, combining stateful and stateless rule groups with a Transit Gateway inspection architecture.

## Summary

- Every VPN connection includes **2 tunnels** in different AZs -- configure BOTH for redundancy
- **BGP dynamic routing** enables automatic failover when a tunnel goes down (~30 seconds)
- **Static routing** requires manual intervention for failover and does not support ECMP
- Single-tunnel VPN is an **anti-pattern** -- AWS performs periodic tunnel maintenance that causes downtime
- Each tunnel supports up to **1.25 Gbps**; use ECMP with multiple VPN connections for higher bandwidth
- Attach VPN to **Transit Gateway** for multi-VPC access through a single VPN connection
- **vpn_ecmp_support = "enable"** on TGW allows load balancing across multiple VPN connections
- VPN costs **$0.05/hr** (~$36/month) per connection -- cheaper than DX for low-volume traffic
- VPN is commonly used as a **backup for Direct Connect** with BGP handling failover between paths

## Reference

- [AWS Site-to-Site VPN](https://docs.aws.amazon.com/vpn/latest/s2svpn/VPC_VPN.html)
- [VPN Tunnel Options](https://docs.aws.amazon.com/vpn/latest/s2svpn/VPNTunnels.html)
- [VPN Redundancy](https://docs.aws.amazon.com/vpn/latest/s2svpn/vpn-redundant-connection.html)
- [Terraform aws_vpn_connection](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpn_connection)

## Additional Resources

- [VPN CloudWatch Metrics](https://docs.aws.amazon.com/vpn/latest/s2svpn/monitoring-cloudwatch-vpn.html) -- TunnelState, TunnelDataIn, TunnelDataOut metrics for monitoring
- [Accelerated VPN](https://docs.aws.amazon.com/vpn/latest/s2svpn/accelerated-vpn.html) -- uses Global Accelerator to route VPN traffic over the AWS backbone for lower latency
- [VPN over Direct Connect](https://docs.aws.amazon.com/vpn/latest/s2svpn/site-to-site-vpn-over-direct-connect.html) -- encrypted VPN tunnel over a DX connection for compliance
- [Customer Gateway Device Configuration](https://docs.aws.amazon.com/vpn/latest/s2svpn/your-cgw.html) -- vendor-specific configuration guides for popular router brands

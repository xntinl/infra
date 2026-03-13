# 25. AWS Direct Connect and Virtual Interfaces

<!--
difficulty: advanced
concepts: [direct-connect, dedicated-connection, hosted-connection, private-vif, public-vif, transit-vif, lag, bgp, dx-gateway, letter-of-authorization, cross-connect]
tools: [terraform, aws-cli]
estimated_time: 60m
bloom_level: evaluate
prerequisites: [17-vpc-subnets-route-tables-igw, 24-transit-gateway-hub-spoke-design]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise is primarily conceptual. You cannot provision a real Direct Connect connection in a lab environment (it requires physical infrastructure at a colocation facility). Terraform resources are limited to DX Gateways and conceptual configurations. Actual DX costs: port fees ($0.03/hr for 1 Gbps, $0.22/hr for 10 Gbps) plus data transfer out ($0.02/GB). Total for lab resources ~$0.01/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Understanding of Transit Gateway (exercise 24) and VPC routing
- Familiarity with BGP concepts (autonomous systems, route advertisements)

## Learning Objectives

After completing this exercise, you will be able to:

- **Evaluate** when to use AWS Direct Connect versus Site-to-Site VPN based on bandwidth, latency, cost, and reliability requirements
- **Distinguish** dedicated connections (1/10/100 Gbps, your own port) from hosted connections (sub-1 Gbps, shared via partner)
- **Explain** the three virtual interface types: Private VIF (VPC access), Public VIF (AWS public services), Transit VIF (Transit Gateway)
- **Design** a Direct Connect architecture with redundancy using multiple connections and LAG groups
- **Describe** the provisioning process: LOA-CFA, cross-connect, BGP peering, VIF creation
- **Analyze** cost trade-offs between DX connection sizes, data transfer rates, and VPN as backup

## Why Direct Connect Matters

Direct Connect establishes a dedicated physical network connection between your data center and AWS. Traffic does not traverse the public internet, providing consistent latency, higher throughput, and reduced data transfer costs. The SAA-C03 exam tests Direct Connect in several contexts: hybrid architecture design, migration planning, compliance requirements, and cost optimization.

The exam expects you to know that Direct Connect is not encrypted by default (it is a dedicated fiber connection, not a VPN tunnel). For encryption over Direct Connect, you run a Site-to-Site VPN connection over the DX link -- this gives you the performance benefits of DX with the encryption of IPsec. The exam also tests the provisioning timeline: setting up a new Direct Connect connection takes weeks to months (physical cross-connect installation at a colocation facility), while VPN can be established in minutes. This timeline difference is critical for disaster recovery planning.

## The Challenge

Design a Direct Connect architecture for a company migrating to AWS. The company has a data center in a colocation facility in Ashburn, VA (us-east-1 region). They need:

1. Dedicated 1 Gbps connection for production workloads
2. Access to 3 VPCs (production, staging, shared services) through a single connection
3. Access to AWS public services (S3) without traversing the internet
4. Redundancy in case the primary connection fails
5. Encryption for compliance requirements

### Architecture

```
Data Center (Ashburn Colo)
  |
  |-- Cross-Connect (1 Gbps fiber)
  |
  v
AWS Direct Connect Location (Ashburn)
  |
  |-- Dedicated Connection (1 Gbps)
  |
  +-- Private VIF -----> DX Gateway -----> Transit Gateway
  |                                          |-- VPC: Production (10.1.0.0/16)
  |                                          |-- VPC: Staging (10.2.0.0/16)
  |                                          +-- VPC: Shared Services (10.0.0.0/16)
  |
  +-- Public VIF ------> AWS Public Services (S3, DynamoDB, etc.)
  |
  +-- [Backup: VPN over internet] -----> Transit Gateway
```

### Requirements

1. **Connection type decision:** Dedicated (1/10/100 Gbps) vs Hosted (50 Mbps - 10 Gbps via partner). For 1 Gbps, dedicated is appropriate. Hosted connections are from AWS Direct Connect Partners and are suitable when you need less than 1 Gbps or do not have presence in a DX location.

2. **Virtual Interface types:**
   - **Private VIF:** Connects to a VPC (via Virtual Private Gateway) or to a DX Gateway (which connects to multiple VPCs/TGW). Uses private IP addresses. BGP peering with your router.
   - **Public VIF:** Connects to AWS public services (S3, DynamoDB, EC2 public IPs). Uses public IP addresses. Useful for accessing S3 without internet when you do not want a VPC endpoint.
   - **Transit VIF:** Connects to a Transit Gateway via DX Gateway. Allows access to multiple VPCs and VPN connections through a single VIF. Maximum 1 transit VIF per DX connection.

3. **DX Gateway:** A global resource that connects your DX connection to VPCs or Transit Gateways in any region. Without a DX Gateway, a Private VIF connects to only one VPC in the same region as the DX location.

## Hints

<details>
<summary>Hint 1: DX Gateway and Transit VIF Architecture</summary>

The DX Gateway is the key to connecting one DX connection to multiple VPCs. Without it, you would need one Private VIF per VPC (limited to 50 VIFs per connection). With a Transit VIF through a DX Gateway connected to a Transit Gateway, you get access to all VPCs attached to the TGW through a single VIF.

```hcl
# DX Gateway (global resource)
resource "aws_dx_gateway" "this" {
  name            = "dx-demo-gateway"
  amazon_side_asn = "64512"
}

# Associate DX Gateway with Transit Gateway
resource "aws_dx_gateway_association" "tgw" {
  dx_gateway_id         = aws_dx_gateway.this.id
  associated_gateway_id = aws_ec2_transit_gateway.this.id

  allowed_prefixes = [
    "10.0.0.0/8",  # All VPC CIDRs
  ]
}
```

The `amazon_side_asn` is the BGP Autonomous System Number on the AWS side. Your on-premises router uses its own ASN for the BGP peering session. Common choices: 64512-65534 (private ASN range).

</details>

<details>
<summary>Hint 2: Redundancy Patterns</summary>

AWS recommends different redundancy levels based on criticality:

**Development/Non-Critical:**
- Single DX connection + VPN backup
- Failover: DX fails, BGP routes shift to VPN (~30 seconds)
- Cost: ~$25/month (1 Gbps port) + VPN ($0.05/hr when active)

**Production:**
- Two DX connections at the same location (different devices)
- VPN as tertiary backup
- Cost: ~$50/month (2x 1 Gbps ports)

**Mission-Critical (Maximum Resiliency):**
- Two DX connections at two different DX locations
- Each location has a separate physical path
- VPN as backup
- Cost: ~$50/month + cross-connect fees at 2 locations

```
Location A (Ashburn)          Location B (Miami)
  |-- DX Connection 1           |-- DX Connection 2
  |-- Private VIF 1             |-- Private VIF 2
  |                             |
  +-----> DX Gateway <----------+
              |
              v
        Transit Gateway
              |
    +---------+---------+
    |         |         |
  VPC-1    VPC-2    VPC-3
```

LAG (Link Aggregation Group) bundles multiple connections at the same location into a single logical connection for higher bandwidth and link-level redundancy.

</details>

<details>
<summary>Hint 3: Provisioning Process</summary>

The DX provisioning process takes weeks to months:

1. **Request connection** in AWS Console or via Terraform (aws_dx_connection)
2. **Receive LOA-CFA** (Letter of Authorization - Connecting Facility Assignment)
   - This document authorizes the colocation provider to set up the cross-connect
3. **Submit LOA-CFA** to your colocation provider
   - They install the physical fiber cross-connect between your router and the AWS device
4. **Cross-connect installed** -- physical layer is up
5. **Create Virtual Interfaces** -- logical layer configuration
6. **Configure BGP peering** on your router
   - AWS provides the BGP peer IP, ASN, and authentication key
7. **BGP session established** -- routes are exchanged
8. **Traffic flows** over the DX connection

```hcl
# Step 1: Request connection (Terraform cannot complete physical setup)
resource "aws_dx_connection" "this" {
  name      = "dx-demo-connection"
  bandwidth = "1Gbps"
  location  = "EqDC2"  # Equinix DC2, Ashburn VA

  tags = { Name = "dx-demo" }
}

# The connection stays in "ordering" state until the physical
# cross-connect is installed at the colocation facility.
# This cannot be automated -- it requires physical work.
```

</details>

<details>
<summary>Hint 4: Encryption Over Direct Connect</summary>

Direct Connect is NOT encrypted by default. The fiber connection is dedicated to you, but data travels in clear text. For compliance requirements (HIPAA, PCI-DSS, SOC 2), you need encryption.

**Option 1: Site-to-Site VPN over DX (MACsec not available)**

Create a VPN connection that routes over the DX link instead of the internet. You get DX performance with IPsec encryption. The VPN is limited to ~1.25 Gbps per tunnel, so for higher bandwidth, use multiple tunnels with ECMP.

**Option 2: MACsec (Layer 2 encryption)**

Available on 10 Gbps and 100 Gbps dedicated connections. Encrypts at the Ethernet frame level with minimal latency overhead. Requires MACsec-capable router on your side.

**Decision:**
- < 10 Gbps or need immediate encryption: VPN over DX
- 10/100 Gbps with MACsec-capable router: MACsec
- No encryption requirement: Direct DX (simplest, lowest latency)

</details>

<details>
<summary>Hint 5: DX vs VPN Decision Framework</summary>

| Criterion | Direct Connect | Site-to-Site VPN |
|-----------|---------------|-----------------|
| **Bandwidth** | 1-100 Gbps dedicated | 1.25 Gbps per tunnel |
| **Latency** | Consistent (dedicated path) | Variable (internet path) |
| **Setup time** | Weeks to months | Minutes |
| **Encryption** | Optional (MACsec/VPN overlay) | Always (IPsec) |
| **Cost (fixed)** | Port fee ($0.03-$0.22/hr) | $0.05/hr per VPN connection |
| **Cost (data)** | $0.02/GB out | $0.09/GB out |
| **Redundancy** | Multiple connections/locations | Multiple tunnels |
| **Use case** | Large-scale hybrid, consistent perf | Quick setup, backup for DX |

**Choose DX when:**
- You transfer > 1 TB/month (DX data transfer is cheaper)
- You need consistent < 10ms latency
- You need > 1.25 Gbps bandwidth
- You have long-term hybrid architecture commitment

**Choose VPN when:**
- You need immediate connectivity (cannot wait weeks for DX)
- Traffic volume is low (< 100 GB/month)
- You need encryption without MACsec-capable hardware
- As backup for DX connections

**Use both when:**
- DX for primary path (performance, cost)
- VPN as backup (immediate failover when DX fails)

</details>

## Spot the Bug

A network architect designs a Direct Connect setup with a Private VIF connected directly to VPC-1, then tries to add VPC-2 and VPC-3:

```
DX Connection
  |-- Private VIF --> Virtual Private Gateway (VPC-1)
  |-- Private VIF --> Virtual Private Gateway (VPC-2)  # Wants to add
  |-- Private VIF --> Virtual Private Gateway (VPC-3)  # Wants to add
```

Each VIF consumes one of 50 VIF slots per connection. As the company adds more VPCs, they approach the limit and cannot scale.

<details>
<summary>Explain the bug</summary>

The architecture uses Private VIFs directly connected to Virtual Private Gateways -- one VIF per VPC. This works for a small number of VPCs but does not scale. The limit is 50 VIFs per DX connection, and each VIF requires BGP configuration on the customer router.

**The fix:** Use a **Transit VIF** with a **DX Gateway** connected to a **Transit Gateway**:

```
DX Connection
  |-- Transit VIF --> DX Gateway --> Transit Gateway
                                       |-- VPC-1
                                       |-- VPC-2
                                       |-- VPC-3
                                       |-- ... (up to 5,000 VPCs)
```

One Transit VIF provides access to all VPCs attached to the Transit Gateway. The DX Gateway is a global resource that can connect to Transit Gateways in any region. This architecture:
- Uses only 1 VIF instead of N
- Scales to thousands of VPCs
- Supports route table segmentation on the TGW
- Allows cross-region access via TGW peering

The trade-off: Transit VIF supports only 1 per DX connection (you can still have Private VIFs and a Public VIF alongside it). For VPCs that need direct DX access without TGW, use Private VIFs. For everything else, use Transit VIF.

</details>

## Verify What You Learned

Since real DX connections require physical infrastructure, verify your understanding with these CLI commands that inspect existing DX resources (or demonstrate the API):

```bash
# List DX locations in your region
aws directconnect describe-locations \
  --query "locations[?region=='us-east-1'].{Name:locationName,Code:locationCode}" \
  --output table
```

Expected: List of colocation facilities where DX is available.

```bash
# List DX gateways (if created)
aws directconnect describe-direct-connect-gateways \
  --query "directConnectGateways[*].{Name:directConnectGatewayName,State:directConnectGatewayState,ASN:amazonSideAsn}" \
  --output table
```

```bash
# Describe virtual interface types
echo "Private VIF: connects to VPC via VGW or DX Gateway"
echo "Public VIF: connects to AWS public services (S3, DynamoDB)"
echo "Transit VIF: connects to Transit Gateway via DX Gateway"
```

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

Direct Connect provides dedicated physical connectivity to AWS. In the next exercise, you will deploy **Site-to-Site VPN** with redundant tunnels -- the encrypted, internet-based alternative that can also serve as a backup path for Direct Connect.

## Summary

- **Direct Connect** provides dedicated physical connectivity between your data center and AWS -- consistent latency, higher bandwidth, lower data transfer costs
- **Dedicated connections** (1/10/100 Gbps) are your own port at a DX location; **hosted connections** are shared via DX Partners
- Three VIF types: **Private** (VPC access), **Public** (AWS public services), **Transit** (Transit Gateway -- best for multi-VPC)
- **DX Gateway** is a global resource that connects DX to VPCs or TGW in any region
- **Transit VIF + DX Gateway + Transit Gateway** scales to thousands of VPCs with a single VIF
- DX is **not encrypted by default** -- use VPN overlay or MACsec (10/100 Gbps) for encryption
- Provisioning takes **weeks to months** (physical cross-connect required) -- VPN provides immediate backup
- DX data transfer is **cheaper** ($0.02/GB) than internet ($0.09/GB) -- significant savings at scale
- **Maximum resiliency** requires 2 connections at 2 different DX locations

## Reference

- [AWS Direct Connect](https://docs.aws.amazon.com/directconnect/latest/UserGuide/Welcome.html)
- [Direct Connect Virtual Interfaces](https://docs.aws.amazon.com/directconnect/latest/UserGuide/WorkingWithVirtualInterfaces.html)
- [Direct Connect Gateway](https://docs.aws.amazon.com/directconnect/latest/UserGuide/direct-connect-gateways.html)
- [Direct Connect Resiliency Recommendations](https://docs.aws.amazon.com/directconnect/latest/UserGuide/resilency_failover.html)

## Additional Resources

- [Direct Connect Pricing](https://aws.amazon.com/directconnect/pricing/) -- port hours, data transfer rates by region, and partner connection pricing
- [Direct Connect + VPN](https://docs.aws.amazon.com/directconnect/latest/UserGuide/direct-connect-transit-gateways.html) -- running encrypted VPN tunnels over Direct Connect
- [MACsec on Direct Connect](https://docs.aws.amazon.com/directconnect/latest/UserGuide/direct-connect-mac-sec-getting-started.html) -- Layer 2 encryption for 10/100 Gbps connections
- [Direct Connect SLA](https://aws.amazon.com/directconnect/sla/) -- 99.99% availability for resiliently configured connections
- [LAG (Link Aggregation Groups)](https://docs.aws.amazon.com/directconnect/latest/UserGuide/lags.html) -- bundling multiple connections for bandwidth and link redundancy

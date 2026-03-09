# 11. AWS Network Firewall with Centralized Inspection

<!--
difficulty: advanced
concepts: [network-firewall, inspection-vpc, stateful-rules, tgw-appliance-mode, centralized-egress, suricata-rules, cloudwatch-logging]
tools: [terraform, aws-cli]
estimated_time: 90m
bloom_level: evaluate
prerequisites: [01-08]
aws_cost: ~$0.45/hr
-->

> **AWS Cost Warning:** This exercise creates an AWS Network Firewall (~$0.395/hr), a Transit Gateway (~$0.05/hr plus attachments), a NAT Gateway, and EC2 instances. Estimated total: ~$0.45/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 08 (Transit Gateway Hub-and-Spoke)
- Basic understanding of firewall rule concepts (allow, deny, stateful vs stateless)

## Learning Objectives

After completing this exercise, you will be able to:

- **Design** a centralized inspection architecture with a dedicated firewall VPC
- **Implement** AWS Network Firewall with separate firewall and TGW attachment subnets
- **Author** Suricata-compatible stateful rules for domain filtering and protocol enforcement
- **Configure** Transit Gateway appliance mode to prevent asymmetric routing
- **Evaluate** CloudWatch logging output to verify that rules are firing correctly

## Why Centralized Network Firewall

Security teams need to inspect traffic flowing between VPCs and out to the internet, but deploying a firewall in every VPC is expensive and operationally complex. The centralized inspection pattern places a single AWS Network Firewall in a dedicated VPC, then uses Transit Gateway routing to funnel all traffic through it. AWS Network Firewall uses the Suricata engine under the hood, giving you deep packet inspection, TLS SNI filtering, and protocol-aware rules without managing any appliance instances. The critical architectural detail is subnet separation: the firewall ENIs and the TGW attachment ENIs must be in **different subnets** to avoid routing loops. See the [AWS Network Firewall documentation](https://docs.aws.amazon.com/network-firewall/latest/developerguide/what-is-aws-network-firewall.html) and the [centralized inspection reference architecture](https://docs.aws.amazon.com/whitepapers/latest/building-scalable-secure-multi-vpc-network-infrastructure/inspection-vpc-with-aws-network-firewall.html).

## The Challenge

Deploy AWS Network Firewall in a centralized inspection architecture where all traffic between spoke VPCs and all internet-bound traffic passes through a dedicated inspection VPC.

| VPC | CIDR | Role |
|-----|------|------|
| inspection | 10.100.0.0/16 | Firewall + NAT Gateway + IGW |
| workload-a | 10.101.0.0/16 | Spoke workload VPC |
| workload-b | 10.102.0.0/16 | Spoke workload VPC |

### Requirements

1. **Inspection VPC** with **three subnet tiers** (this separation is critical):
   - Firewall subnets -- where Network Firewall ENIs are placed
   - TGW attachment subnets -- where Transit Gateway attachment ENIs are placed
   - Public subnets -- where the NAT Gateway and IGW route internet traffic
2. **Transit Gateway** with `appliance_mode_support = "enable"` on the inspection VPC attachment to prevent asymmetric routing
3. **AWS Network Firewall** deployed in the firewall subnets with:
   - A **stateless rule group** that forwards all traffic to the stateful engine (no early drops)
   - A **stateful rule group** using Suricata syntax that:
     - Allows HTTPS to `*.amazonaws.com` (AWS service endpoints)
     - Allows HTTP/HTTPS to a specific domain you choose (e.g., `ifconfig.me`)
     - Blocks all other TLS/HTTP traffic to external domains
     - Logs all dropped packets
4. **Firewall policy** connecting the rule groups with default drop action for stateful rules
5. **CloudWatch logging** for both ALERT and FLOW log types
6. **Routing** so that:
   - Spoke VPC traffic (0.0.0.0/0) routes through TGW to the inspection VPC
   - TGW attachment subnets route to the firewall ENIs
   - Firewall subnets route to the NAT Gateway (for internet) or back to TGW (for inter-VPC)
   - Public subnets route return traffic for spoke CIDRs to the firewall ENIs (not directly to TGW)
7. **EC2 test instances** in each spoke VPC to validate inspection

### Architecture

```
                     Internet
                        │
                   ┌────┴────┐
                   │   IGW   │
                   └────┬────┘
                        │
               ┌────────┴────────┐
               │  Public Subnet  │
               │  (NAT Gateway)  │
               └────────┬────────┘
                        │
               ┌────────┴────────┐
               │ Firewall Subnet │
               │  (NF Endpoints) │◄── AWS Network Firewall
               └────────┬────────┘
                        │
               ┌────────┴────────┐
               │  TGW Subnet     │
               │  (TGW ENIs)     │
               └────────┬────────┘
                        │
               ┌────────┴────────┐
               │ Transit Gateway │
               │ (appliance mode)│
               └───┬─────────┬──┘
                   │         │
          ┌────────┴──┐  ┌──┴────────┐
          │workload-a │  │workload-b │
          │10.101/16  │  │10.102/16  │
          └───────────┘  └───────────┘

     All traffic inspected by Network Firewall
```

## Hints

Work through these one at a time. Only open the next hint if you are stuck.

<details>
<summary>Hint 1: Inspection VPC subnet design</summary>

The inspection VPC needs three distinct subnet tiers. This separation prevents routing loops:

- **TGW subnets** (e.g., `10.100.1.0/24`): TGW attachment ENIs live here. Route table sends `0.0.0.0/0` to the **firewall endpoint** (not NAT, not IGW)
- **Firewall subnets** (e.g., `10.100.2.0/24`): Network Firewall ENIs live here. Route table sends `0.0.0.0/0` to the **NAT Gateway** (for internet-bound traffic) and spoke CIDRs to the **TGW** (for return traffic to spokes)
- **Public subnets** (e.g., `10.100.3.0/24`): NAT Gateway and IGW. Route table sends `0.0.0.0/0` to the **IGW** and spoke CIDRs to the **firewall endpoint** (so return traffic from internet gets inspected too)

The firewall endpoint ID is an output of `aws_networkfirewall_firewall` -- look for it in `firewall_status[0].sync_states`.

</details>

<details>
<summary>Hint 2: Network Firewall and rule groups</summary>

Create the firewall resources in this order:

1. **Stateless rule group** (`aws_networkfirewall_rule_group` with `type = "STATELESS"`): Create a single rule with priority 1 that matches all traffic and has action `aws:forward_to_sfe` (forward to stateful engine). This ensures every packet gets deep inspection.

2. **Stateful rule group** (`aws_networkfirewall_rule_group` with `type = "STATEFUL"`): Use `rules_source` with `rules_string` containing Suricata syntax. Set `rule_order = "STRICT_ORDER"` in `stateful_rule_options` and use `action_order = "STRICT_ORDER"` in the firewall policy.

3. **Firewall policy** (`aws_networkfirewall_firewall_policy`): Reference both rule groups. Set `stateful_default_actions = ["aws:drop_established", "aws:alert_established"]` for strict default-drop behavior.

4. **Firewall** (`aws_networkfirewall_firewall`): Place in the firewall subnets (not TGW subnets!). Use `subnet_mapping` blocks.

</details>

<details>
<summary>Hint 3: Suricata rule syntax</summary>

AWS Network Firewall uses Suricata-compatible rules. Key rules for this exercise:

Allow HTTPS to AWS services (matches TLS SNI):
```
pass tls $HOME_NET any -> $EXTERNAL_NET 443 (tls.sni; content:"amazonaws.com"; endswith; msg:"Allow AWS services"; sid:100; rev:1;)
```

Allow HTTP to a specific test domain:
```
pass http $HOME_NET any -> $EXTERNAL_NET 80 (http.host; content:"ifconfig.me"; endswith; msg:"Allow ifconfig.me HTTP"; sid:101; rev:1;)
```

Allow HTTPS to a specific test domain:
```
pass tls $HOME_NET any -> $EXTERNAL_NET 443 (tls.sni; content:"ifconfig.me"; endswith; msg:"Allow ifconfig.me HTTPS"; sid:102; rev:1;)
```

Drop and alert all other outbound TLS:
```
drop tls $HOME_NET any -> $EXTERNAL_NET any (msg:"Block all other TLS"; sid:200; rev:1;)
```

Drop and alert all other outbound HTTP:
```
drop http $HOME_NET any -> $EXTERNAL_NET any (msg:"Block all other HTTP"; sid:201; rev:1;)
```

Set `$HOME_NET` by defining `ip_set` in the rule group's `rule_variables` with your VPC CIDRs.

</details>

<details>
<summary>Hint 4: Transit Gateway and appliance mode</summary>

Create the Transit Gateway and configure routing:

1. TGW with `default_route_table_association = "disable"` and `default_route_table_propagation = "disable"` (same pattern as exercise 08)

2. Three VPC attachments: inspection, workload-a, workload-b. **Critical**: on the inspection VPC attachment, set `appliance_mode_support = "enable"`. Without this, TGW may send return traffic to a different AZ than the request, bypassing the firewall (asymmetric routing).

3. TGW route tables:
   - **Spoke route table** (used by workload-a and workload-b): static route `0.0.0.0/0` pointing to the inspection VPC attachment. Propagate spoke routes so inter-VPC traffic also goes through inspection.
   - **Inspection route table** (used by inspection VPC): propagations from workload-a and workload-b so the firewall knows how to route return traffic back to spokes.

4. In each spoke VPC, the private subnet route table sends `0.0.0.0/0` to the TGW.

</details>

<details>
<summary>Hint 5: CloudWatch logging and routing the firewall endpoint</summary>

**Logging:** Use `aws_networkfirewall_logging_configuration` with two log destinations:
- `log_type = "ALERT"` to a CloudWatch log group (captures rule matches and drops)
- `log_type = "FLOW"` to a separate CloudWatch log group (captures all connection metadata)

**Getting the firewall endpoint ID:** The firewall endpoint is not a simple attribute. Extract it from the firewall resource:

```hcl
locals {
  fw_endpoint_id = [
    for ss in aws_networkfirewall_firewall.this.firewall_status[0].sync_states :
    ss.attachment[0].endpoint_id
    if ss.availability_zone == data.aws_availability_zones.available.names[0]
  ][0]
}
```

Use this endpoint ID as the target in route tables. For example, in the TGW subnet route table:

```hcl
resource "aws_route" "tgw_to_firewall" {
  route_table_id         = aws_route_table.tgw_subnet.id
  destination_cidr_block = "0.0.0.0/0"
  vpc_endpoint_id        = local.fw_endpoint_id
}
```

Note: this uses `vpc_endpoint_id`, not `gateway_id` or `nat_gateway_id`. The firewall endpoint is a Gateway Load Balancer endpoint under the hood.

</details>

## Spot the Bug

A team deployed Network Firewall but traffic entering the inspection VPC from TGW enters an infinite loop and times out. Their inspection VPC uses this subnet design:

```hcl
resource "aws_subnet" "inspection_fw_and_tgw" {
  vpc_id            = aws_vpc.inspection.id
  cidr_block        = "10.100.1.0/24"
  availability_zone = "us-east-1a"
  tags              = { Name = "inspection-combined" }
}

resource "aws_networkfirewall_firewall" "this" {
  name                = "centralized-fw"
  firewall_policy_arn = aws_networkfirewall_firewall_policy.this.arn
  vpc_id              = aws_vpc.inspection.id

  subnet_mapping {
    subnet_id = aws_subnet.inspection_fw_and_tgw.id
  }
}

resource "aws_ec2_transit_gateway_vpc_attachment" "inspection" {
  transit_gateway_id = aws_ec2_transit_gateway.this.id
  vpc_id             = aws_vpc.inspection.id
  subnet_ids         = [aws_subnet.inspection_fw_and_tgw.id]
}
```

And the route table for that combined subnet:

```hcl
resource "aws_route_table" "combined" {
  vpc_id = aws_vpc.inspection.id

  route {
    cidr_block      = "0.0.0.0/0"
    vpc_endpoint_id = local.fw_endpoint_id
  }
}
```

<details>
<summary>Explain the bug</summary>

**The problem:** The firewall ENIs and TGW attachment ENIs are in the **same subnet**, sharing the **same route table**. When traffic arrives from the TGW, the route table sends it to the firewall endpoint. After the firewall inspects the traffic, it returns it to the subnet -- where the route table sends it right back to the firewall. This creates an **infinite routing loop**.

The loop happens because:
1. TGW delivers traffic to its ENI in `inspection-combined`
2. Route table says `0.0.0.0/0 -> firewall endpoint`
3. Firewall inspects and returns traffic to the subnet
4. Route table again says `0.0.0.0/0 -> firewall endpoint`
5. Repeat forever until TTL expires

**The fix:** Use **separate subnets** with **separate route tables** for the TGW attachment and the firewall:

```hcl
resource "aws_subnet" "tgw" {
  vpc_id            = aws_vpc.inspection.id
  cidr_block        = "10.100.1.0/24"
  availability_zone = "us-east-1a"
  tags              = { Name = "inspection-tgw" }
}

resource "aws_subnet" "firewall" {
  vpc_id            = aws_vpc.inspection.id
  cidr_block        = "10.100.2.0/24"
  availability_zone = "us-east-1a"
  tags              = { Name = "inspection-firewall" }
}

# TGW subnet route table: send traffic to firewall
resource "aws_route_table" "tgw_subnet" {
  vpc_id = aws_vpc.inspection.id
  route {
    cidr_block      = "0.0.0.0/0"
    vpc_endpoint_id = local.fw_endpoint_id
  }
}

# Firewall subnet route table: send traffic to NAT (internet)
# or back to TGW (spoke return traffic)
resource "aws_route_table" "firewall_subnet" {
  vpc_id = aws_vpc.inspection.id
  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.this.id
  }
}
```

This way, traffic flows TGW subnet -> firewall -> firewall subnet -> NAT/TGW, never looping back through the firewall.

</details>

## Verify What You Learned

### Network Firewall is provisioned

```bash
aws network-firewall describe-firewall \
  --firewall-name <your-firewall-name> \
  --query "Firewall.{Name:FirewallName,Status:FirewallStatus}" \
  --output table
```

Expected:

```
------------------------------------------
|           DescribeFirewall             |
+------------------+---------------------+
|  Name            |  Status             |
+------------------+---------------------+
|  centralized-fw  |                     |
+------------------+---------------------+
```

### Firewall sync state is READY

```bash
aws network-firewall describe-firewall \
  --firewall-name <your-firewall-name> \
  --query "FirewallStatus.SyncStates" \
  --output json
```

Expected: each AZ shows `"Status": "IN_SYNC"` and has an `Attachment` with an `EndpointId` starting with `vpce-`.

### Appliance mode enabled on inspection attachment

```bash
aws ec2 describe-transit-gateway-vpc-attachments \
  --filters "Name=tag:Name,Values=<your-inspection-attachment-name>" \
  --query "TransitGatewayVpcAttachments[0].Options.ApplianceModeSupport" \
  --output text
```

Expected: `enable`

### Allowed domain works from spoke

```bash
# From workload-a EC2 instance (via SSM or SSH):
curl -s http://ifconfig.me
```

Expected: the public IP of the inspection VPC's NAT Gateway (confirms traffic is egressing through the centralized path).

### Blocked domain is dropped

```bash
# From workload-a EC2 instance:
curl -s --max-time 5 https://example.com
```

Expected: connection timeout (no response, because the firewall drops the TLS handshake).

### Alert logs in CloudWatch

```bash
aws logs filter-log-events \
  --log-group-name <your-alert-log-group> \
  --filter-pattern "Block" \
  --query "events[0].message" \
  --output text
```

Expected: a JSON log entry containing `"event_type":"alert"` with the blocked domain and your Suricata rule message (e.g., `"Block all other TLS"`).

### Flow logs in CloudWatch

```bash
aws logs filter-log-events \
  --log-group-name <your-flow-log-group> \
  --query "events | length(@)"
```

Expected: a positive number (flow records are being captured).

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

Network Firewall and Transit Gateway attachments can take several minutes to delete. The destroy may need to be run twice if there are dependency ordering issues.

Verify nothing remains:

```bash
terraform state list
```

Expected: no output (empty state).

```bash
aws network-firewall list-firewalls \
  --query "Firewalls[?FirewallName=='<your-firewall-name>']" \
  --output text
```

Expected: no output.

## What's Next

You have built a centralized inspection architecture with deep packet inspection. In the next exercise, you will explore **Site-to-Site VPN with BGP** -- connecting an on-premises network to AWS with dynamic routing, completing the hybrid networking picture.

## Summary

- **AWS Network Firewall** provides managed deep packet inspection using the Suricata engine, supporting stateful rules for domain filtering, protocol enforcement, and IDS/IPS
- **Subnet separation** in the inspection VPC is non-negotiable: firewall ENIs and TGW ENIs must be in different subnets with different route tables to avoid routing loops
- **Appliance mode** on the TGW attachment ensures symmetric routing, preventing traffic from bypassing the firewall due to AZ-level path selection
- **Stateless rules** should forward all traffic to the stateful engine (`aws:forward_to_sfe`) for complete deep inspection
- **Suricata rule syntax** enables TLS SNI filtering (`tls.sni`), HTTP host matching (`http.host`), and protocol-aware inspection without terminating encryption
- **CloudWatch logging** (ALERT + FLOW) provides the observability needed to verify that rules are working and to troubleshoot connectivity issues

## Reference

- [AWS Network Firewall Documentation](https://docs.aws.amazon.com/network-firewall/latest/developerguide/what-is-aws-network-firewall.html)
- [Terraform aws_networkfirewall_firewall](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/networkfirewall_firewall)
- [Terraform aws_networkfirewall_rule_group](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/networkfirewall_rule_group)
- [Terraform aws_networkfirewall_firewall_policy](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/networkfirewall_firewall_policy)
- [Suricata Rule Format](https://suricata.readthedocs.io/en/latest/rules/intro.html)

## Additional Resources

- [Centralized Inspection Architecture (AWS Whitepaper)](https://docs.aws.amazon.com/whitepapers/latest/building-scalable-secure-multi-vpc-network-infrastructure/inspection-vpc-with-aws-network-firewall.html) -- the reference architecture this exercise implements
- [AWS Network Firewall Best Practices](https://docs.aws.amazon.com/network-firewall/latest/developerguide/firewall-best-practices.html) -- rule ordering, logging, and performance considerations
- [Suricata Rule Writing Guide](https://suricata.readthedocs.io/en/latest/rules/index.html) -- comprehensive guide to Suricata rule syntax and keywords
- [Network Firewall Logging and Monitoring](https://docs.aws.amazon.com/network-firewall/latest/developerguide/firewall-logging.html) -- understanding ALERT vs FLOW logs and S3/CloudWatch/Kinesis destinations
- [TGW Appliance Mode Explained (AWS Blog)](https://aws.amazon.com/blogs/networking-and-content-delivery/centralized-inspection-architecture-with-aws-gateway-load-balancer-and-aws-transit-gateway/) -- why appliance mode prevents asymmetric routing

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

locals {
  az = data.aws_availability_zones.available.names[0]
  fw_endpoint_id = [
    for ss in aws_networkfirewall_firewall.this.firewall_status[0].sync_states :
    ss.attachment[0].endpoint_id
    if ss.availability_zone == local.az
  ][0]
}
```

### `vpc.tf`

```hcl
# ---------------------------------------------------------------
# Inspection VPC -- three subnet tiers
# ---------------------------------------------------------------
resource "aws_vpc" "inspection" {
  cidr_block           = "10.100.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags                 = { Name = "inspection-vpc" }
}

resource "aws_subnet" "tgw" {
  vpc_id            = aws_vpc.inspection.id
  cidr_block        = "10.100.1.0/24"
  availability_zone = local.az
  tags              = { Name = "inspection-tgw" }
}

resource "aws_subnet" "firewall" {
  vpc_id            = aws_vpc.inspection.id
  cidr_block        = "10.100.2.0/24"
  availability_zone = local.az
  tags              = { Name = "inspection-firewall" }
}

resource "aws_subnet" "public" {
  vpc_id                  = aws_vpc.inspection.id
  cidr_block              = "10.100.3.0/24"
  availability_zone       = local.az
  map_public_ip_on_launch = true
  tags                    = { Name = "inspection-public" }
}

# ---------------------------------------------------------------
# IGW + NAT Gateway
# ---------------------------------------------------------------
resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.inspection.id
  tags   = { Name = "inspection-igw" }
}

resource "aws_eip" "nat" {
  domain = "vpc"
  tags   = { Name = "inspection-nat-eip" }
}

resource "aws_nat_gateway" "this" {
  allocation_id = aws_eip.nat.id
  subnet_id     = aws_subnet.public.id
  depends_on    = [aws_internet_gateway.this]
  tags          = { Name = "inspection-nat" }
}

# ---------------------------------------------------------------
# Inspection VPC route tables
# ---------------------------------------------------------------

# TGW subnet: all traffic -> firewall endpoint
resource "aws_route_table" "tgw_subnet" {
  vpc_id = aws_vpc.inspection.id
  tags   = { Name = "inspection-tgw-rt" }
}

resource "aws_route" "tgw_to_fw" {
  route_table_id         = aws_route_table.tgw_subnet.id
  destination_cidr_block = "0.0.0.0/0"
  vpc_endpoint_id        = local.fw_endpoint_id
}

resource "aws_route_table_association" "tgw_subnet" {
  subnet_id      = aws_subnet.tgw.id
  route_table_id = aws_route_table.tgw_subnet.id
}

# Firewall subnet: internet -> NAT, spokes -> TGW
resource "aws_route_table" "firewall_subnet" {
  vpc_id = aws_vpc.inspection.id
  tags   = { Name = "inspection-firewall-rt" }
}

resource "aws_route" "fw_to_nat" {
  route_table_id         = aws_route_table.firewall_subnet.id
  destination_cidr_block = "0.0.0.0/0"
  nat_gateway_id         = aws_nat_gateway.this.id
}

resource "aws_route" "fw_to_tgw_a" {
  route_table_id         = aws_route_table.firewall_subnet.id
  destination_cidr_block = "10.101.0.0/16"
  transit_gateway_id     = aws_ec2_transit_gateway.this.id
  depends_on             = [aws_ec2_transit_gateway_vpc_attachment.inspection]
}

resource "aws_route" "fw_to_tgw_b" {
  route_table_id         = aws_route_table.firewall_subnet.id
  destination_cidr_block = "10.102.0.0/16"
  transit_gateway_id     = aws_ec2_transit_gateway.this.id
  depends_on             = [aws_ec2_transit_gateway_vpc_attachment.inspection]
}

resource "aws_route_table_association" "firewall_subnet" {
  subnet_id      = aws_subnet.firewall.id
  route_table_id = aws_route_table.firewall_subnet.id
}

# Public subnet: internet -> IGW, spokes -> firewall endpoint
resource "aws_route_table" "public_subnet" {
  vpc_id = aws_vpc.inspection.id
  tags   = { Name = "inspection-public-rt" }
}

resource "aws_route" "public_to_igw" {
  route_table_id         = aws_route_table.public_subnet.id
  destination_cidr_block = "0.0.0.0/0"
  gateway_id             = aws_internet_gateway.this.id
}

resource "aws_route" "public_to_fw_a" {
  route_table_id         = aws_route_table.public_subnet.id
  destination_cidr_block = "10.101.0.0/16"
  vpc_endpoint_id        = local.fw_endpoint_id
}

resource "aws_route" "public_to_fw_b" {
  route_table_id         = aws_route_table.public_subnet.id
  destination_cidr_block = "10.102.0.0/16"
  vpc_endpoint_id        = local.fw_endpoint_id
}

resource "aws_route_table_association" "public_subnet" {
  subnet_id      = aws_subnet.public.id
  route_table_id = aws_route_table.public_subnet.id
}

# ---------------------------------------------------------------
# Spoke VPCs
# ---------------------------------------------------------------
resource "aws_vpc" "workload_a" {
  cidr_block           = "10.101.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags                 = { Name = "workload-a" }
}

resource "aws_vpc" "workload_b" {
  cidr_block           = "10.102.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags                 = { Name = "workload-b" }
}

resource "aws_subnet" "workload_a" {
  vpc_id            = aws_vpc.workload_a.id
  cidr_block        = "10.101.1.0/24"
  availability_zone = local.az
  tags              = { Name = "workload-a-private" }
}

resource "aws_subnet" "workload_b" {
  vpc_id            = aws_vpc.workload_b.id
  cidr_block        = "10.102.1.0/24"
  availability_zone = local.az
  tags              = { Name = "workload-b-private" }
}

resource "aws_route_table" "workload_a" {
  vpc_id = aws_vpc.workload_a.id
  tags   = { Name = "workload-a-rt" }
}

resource "aws_route" "workload_a_default" {
  route_table_id         = aws_route_table.workload_a.id
  destination_cidr_block = "0.0.0.0/0"
  transit_gateway_id     = aws_ec2_transit_gateway.this.id
  depends_on             = [aws_ec2_transit_gateway_vpc_attachment.workload_a]
}

resource "aws_route_table_association" "workload_a" {
  subnet_id      = aws_subnet.workload_a.id
  route_table_id = aws_route_table.workload_a.id
}

resource "aws_route_table" "workload_b" {
  vpc_id = aws_vpc.workload_b.id
  tags   = { Name = "workload-b-rt" }
}

resource "aws_route" "workload_b_default" {
  route_table_id         = aws_route_table.workload_b.id
  destination_cidr_block = "0.0.0.0/0"
  transit_gateway_id     = aws_ec2_transit_gateway.this.id
  depends_on             = [aws_ec2_transit_gateway_vpc_attachment.workload_b]
}

resource "aws_route_table_association" "workload_b" {
  subnet_id      = aws_subnet.workload_b.id
  route_table_id = aws_route_table.workload_b.id
}
```

### `tgw.tf`

```hcl
# ---------------------------------------------------------------
# Transit Gateway
# ---------------------------------------------------------------
resource "aws_ec2_transit_gateway" "this" {
  description                     = "Centralized inspection TGW"
  default_route_table_association = "disable"
  default_route_table_propagation = "disable"
  tags                            = { Name = "inspection-tgw" }
}

resource "aws_ec2_transit_gateway_vpc_attachment" "inspection" {
  transit_gateway_id        = aws_ec2_transit_gateway.this.id
  vpc_id                    = aws_vpc.inspection.id
  subnet_ids                = [aws_subnet.tgw.id]
  appliance_mode_support    = "enable"
  tags                      = { Name = "inspection-tgw-att" }
}

resource "aws_ec2_transit_gateway_vpc_attachment" "workload_a" {
  transit_gateway_id = aws_ec2_transit_gateway.this.id
  vpc_id             = aws_vpc.workload_a.id
  subnet_ids         = [aws_subnet.workload_a.id]
  tags               = { Name = "workload-a-tgw-att" }
}

resource "aws_ec2_transit_gateway_vpc_attachment" "workload_b" {
  transit_gateway_id = aws_ec2_transit_gateway.this.id
  vpc_id             = aws_vpc.workload_b.id
  subnet_ids         = [aws_subnet.workload_b.id]
  tags               = { Name = "workload-b-tgw-att" }
}

# TGW route tables
resource "aws_ec2_transit_gateway_route_table" "spokes" {
  transit_gateway_id = aws_ec2_transit_gateway.this.id
  tags               = { Name = "tgw-rt-spokes" }
}

resource "aws_ec2_transit_gateway_route_table" "inspection" {
  transit_gateway_id = aws_ec2_transit_gateway.this.id
  tags               = { Name = "tgw-rt-inspection" }
}

resource "aws_ec2_transit_gateway_route_table_association" "workload_a" {
  transit_gateway_attachment_id  = aws_ec2_transit_gateway_vpc_attachment.workload_a.id
  transit_gateway_route_table_id = aws_ec2_transit_gateway_route_table.spokes.id
}

resource "aws_ec2_transit_gateway_route_table_association" "workload_b" {
  transit_gateway_attachment_id  = aws_ec2_transit_gateway_vpc_attachment.workload_b.id
  transit_gateway_route_table_id = aws_ec2_transit_gateway_route_table.spokes.id
}

resource "aws_ec2_transit_gateway_route_table_association" "inspection" {
  transit_gateway_attachment_id  = aws_ec2_transit_gateway_vpc_attachment.inspection.id
  transit_gateway_route_table_id = aws_ec2_transit_gateway_route_table.inspection.id
}

# Spokes route table: all traffic -> inspection
resource "aws_ec2_transit_gateway_route" "spokes_default" {
  destination_cidr_block         = "0.0.0.0/0"
  transit_gateway_attachment_id  = aws_ec2_transit_gateway_vpc_attachment.inspection.id
  transit_gateway_route_table_id = aws_ec2_transit_gateway_route_table.spokes.id
}

# Inspection route table: propagations from spokes
resource "aws_ec2_transit_gateway_route_table_propagation" "from_a" {
  transit_gateway_attachment_id  = aws_ec2_transit_gateway_vpc_attachment.workload_a.id
  transit_gateway_route_table_id = aws_ec2_transit_gateway_route_table.inspection.id
}

resource "aws_ec2_transit_gateway_route_table_propagation" "from_b" {
  transit_gateway_attachment_id  = aws_ec2_transit_gateway_vpc_attachment.workload_b.id
  transit_gateway_route_table_id = aws_ec2_transit_gateway_route_table.inspection.id
}
```

### `firewall.tf`

```hcl
# ---------------------------------------------------------------
# AWS Network Firewall
# ---------------------------------------------------------------

# Stateless rule group: forward everything to stateful engine
resource "aws_networkfirewall_rule_group" "stateless_forward" {
  capacity = 10
  name     = "forward-to-stateful"
  type     = "STATELESS"

  rule_group {
    rules_source {
      stateless_rules_and_custom_actions {
        stateless_rule {
          priority = 1
          rule_definition {
            actions = ["aws:forward_to_sfe"]
            match_attributes {
              source {
                address_definition = "0.0.0.0/0"
              }
              destination {
                address_definition = "0.0.0.0/0"
              }
            }
          }
        }
      }
    }
  }

  tags = { Name = "forward-to-stateful" }
}

# Stateful rule group: Suricata rules
resource "aws_networkfirewall_rule_group" "stateful_domain" {
  capacity = 100
  name     = "domain-filtering"
  type     = "STATEFUL"

  rule_group {
    rule_variables {
      ip_sets {
        key = "HOME_NET"
        ip_set {
          definition = ["10.101.0.0/16", "10.102.0.0/16"]
        }
      }
      ip_sets {
        key = "EXTERNAL_NET"
        ip_set {
          definition = ["0.0.0.0/0"]
        }
      }
    }

    rules_source {
      rules_string = <<-RULES
        pass tls $HOME_NET any -> $EXTERNAL_NET 443 (tls.sni; content:"amazonaws.com"; endswith; msg:"Allow AWS services"; sid:100; rev:1;)
        pass http $HOME_NET any -> $EXTERNAL_NET 80 (http.host; content:"ifconfig.me"; endswith; msg:"Allow ifconfig.me HTTP"; sid:101; rev:1;)
        pass tls $HOME_NET any -> $EXTERNAL_NET 443 (tls.sni; content:"ifconfig.me"; endswith; msg:"Allow ifconfig.me HTTPS"; sid:102; rev:1;)
        drop tls $HOME_NET any -> $EXTERNAL_NET any (msg:"Block all other TLS"; sid:200; rev:1;)
        drop http $HOME_NET any -> $EXTERNAL_NET any (msg:"Block all other HTTP"; sid:201; rev:1;)
      RULES
    }

    stateful_rule_options {
      capacity = 100
    }
  }

  tags = { Name = "domain-filtering" }
}

# Firewall policy
resource "aws_networkfirewall_firewall_policy" "this" {
  name = "centralized-policy"

  firewall_policy {
    stateless_default_actions          = ["aws:forward_to_sfe"]
    stateless_fragment_default_actions = ["aws:forward_to_sfe"]

    stateless_rule_group_reference {
      priority     = 1
      resource_arn = aws_networkfirewall_rule_group.stateless_forward.arn
    }

    stateful_engine_options {
      rule_order = "STRICT_ORDER"
    }

    stateful_default_actions = ["aws:drop_established", "aws:alert_established"]

    stateful_rule_group_reference {
      priority     = 1
      resource_arn = aws_networkfirewall_rule_group.stateful_domain.arn
    }
  }

  tags = { Name = "centralized-policy" }
}

# Firewall
resource "aws_networkfirewall_firewall" "this" {
  name                = "centralized-fw"
  firewall_policy_arn = aws_networkfirewall_firewall_policy.this.arn
  vpc_id              = aws_vpc.inspection.id

  subnet_mapping {
    subnet_id = aws_subnet.firewall.id
  }

  tags = { Name = "centralized-fw" }
}
```

### `monitoring.tf`

```hcl
# ---------------------------------------------------------------
# CloudWatch logging
# ---------------------------------------------------------------
resource "aws_cloudwatch_log_group" "fw_alert" {
  name              = "/networkfirewall/alert"
  retention_in_days = 7
}

resource "aws_cloudwatch_log_group" "fw_flow" {
  name              = "/networkfirewall/flow"
  retention_in_days = 7
}

resource "aws_networkfirewall_logging_configuration" "this" {
  firewall_arn = aws_networkfirewall_firewall.this.arn

  logging_configuration {
    log_destination_config {
      log_destination = {
        logGroup = aws_cloudwatch_log_group.fw_alert.name
      }
      log_destination_type = "CloudWatchLogs"
      log_type             = "ALERT"
    }

    log_destination_config {
      log_destination = {
        logGroup = aws_cloudwatch_log_group.fw_flow.name
      }
      log_destination_type = "CloudWatchLogs"
      log_type             = "FLOW"
    }
  }
}
```

### `security.tf`

```hcl
# ---------------------------------------------------------------
# Security groups
# ---------------------------------------------------------------
resource "aws_security_group" "workload_a" {
  name        = "workload-a-sg"
  description = "Allow ICMP and all egress"
  vpc_id      = aws_vpc.workload_a.id
  tags        = { Name = "workload-a-sg" }
}

resource "aws_vpc_security_group_ingress_rule" "workload_a_icmp" {
  security_group_id = aws_security_group.workload_a.id
  cidr_ipv4         = "10.0.0.0/8"
  from_port         = -1
  to_port           = -1
  ip_protocol       = "icmp"
}

resource "aws_vpc_security_group_egress_rule" "workload_a_all" {
  security_group_id = aws_security_group.workload_a.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}

resource "aws_security_group" "workload_b" {
  name        = "workload-b-sg"
  description = "Allow ICMP and all egress"
  vpc_id      = aws_vpc.workload_b.id
  tags        = { Name = "workload-b-sg" }
}

resource "aws_vpc_security_group_ingress_rule" "workload_b_icmp" {
  security_group_id = aws_security_group.workload_b.id
  cidr_ipv4         = "10.0.0.0/8"
  from_port         = -1
  to_port           = -1
  ip_protocol       = "icmp"
}

resource "aws_vpc_security_group_egress_rule" "workload_b_all" {
  security_group_id = aws_security_group.workload_b.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}
```

### `compute.tf`

```hcl
# ---------------------------------------------------------------
# EC2 test instances
# ---------------------------------------------------------------
resource "aws_instance" "workload_a" {
  ami                    = data.aws_ami.amazon_linux_2023.id
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.workload_a.id
  vpc_security_group_ids = [aws_security_group.workload_a.id]
  tags                   = { Name = "workload-a-test" }
}

resource "aws_instance" "workload_b" {
  ami                    = data.aws_ami.amazon_linux_2023.id
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.workload_b.id
  vpc_security_group_ids = [aws_security_group.workload_b.id]
  tags                   = { Name = "workload-b-test" }
}
```

### `outputs.tf`

```hcl
output "firewall_name" {
  value = aws_networkfirewall_firewall.this.name
}

output "firewall_endpoint_id" {
  value = local.fw_endpoint_id
}

output "tgw_id" {
  value = aws_ec2_transit_gateway.this.id
}

output "nat_public_ip" {
  value = aws_eip.nat.public_ip
}

output "workload_a_private_ip" {
  value = aws_instance.workload_a.private_ip
}

output "workload_b_private_ip" {
  value = aws_instance.workload_b.private_ip
}
```

</details>

# 27. AWS Network Firewall with Centralized Inspection

<!--
difficulty: advanced
concepts: [network-firewall, stateful-rules, stateless-rules, firewall-policy, inspection-vpc, tgw-routing, suricata-rules, centralized-inspection-architecture, firewall-endpoint]
tools: [terraform, aws-cli]
estimated_time: 65m
bloom_level: evaluate, create
prerequisites: [17-vpc-subnets-route-tables-igw, 19-security-groups-vs-nacls, 24-transit-gateway-hub-spoke-design]
aws_cost: ~$0.10/hr
-->

> **AWS Cost Warning:** AWS Network Firewall costs $0.395/hr per endpoint plus $0.065/GB data processed. Transit Gateway costs $0.05/hr plus attachments. Total ~$0.10/hr minimum. Destroy IMMEDIATELY when finished to avoid significant charges.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Understanding of Transit Gateway hub-and-spoke (exercise 24)
- Understanding of security groups and NACLs (exercise 19)

## Learning Objectives

After completing this exercise, you will be able to:

- **Design** a centralized inspection architecture using AWS Network Firewall with Transit Gateway
- **Distinguish** stateless rule groups (fast, 5-tuple matching) from stateful rule groups (deep packet inspection, Suricata-compatible)
- **Implement** a firewall policy with both stateless and stateful rule groups
- **Configure** TGW routing to force all inter-VPC traffic through the inspection VPC
- **Evaluate** when to use Network Firewall versus security groups/NACLs versus third-party appliances
- **Analyze** the cost implications of centralized inspection ($0.395/hr per endpoint + data processing)

## Why Centralized Network Inspection Matters

AWS Network Firewall provides stateful deep packet inspection, intrusion detection/prevention (IDS/IPS), and domain-based filtering for traffic flowing through your VPC. The SAA-C03 exam tests Network Firewall in scenarios requiring inspection capabilities beyond what security groups and NACLs provide: blocking traffic to specific domains, detecting malware signatures, or enforcing protocol-level rules.

The centralized inspection pattern uses a dedicated "inspection VPC" with Network Firewall endpoints. Transit Gateway routes all traffic through this VPC before reaching its destination. This creates a single chokepoint where all inter-VPC, egress, and ingress traffic is inspected. The pattern is analogous to a traditional data center firewall positioned between network zones.

The key architectural challenge is routing. TGW must be configured with appliance mode enabled to ensure symmetric routing -- both the request and response packets traverse the same firewall endpoint. Without appliance mode, asymmetric routing causes stateful rules to fail because the firewall sees only half of each connection.

## The Challenge

Deploy a centralized inspection architecture with AWS Network Firewall. All traffic between spoke VPCs and egress traffic to the internet must pass through the firewall.

### Architecture

```
                     Internet
                        |
                   +----+----+
                   |   IGW   |
                   +----+----+
                        |
              +---------+---------+
              |  Inspection VPC   |
              |                   |
              |  +-------------+  |
              |  |  Network    |  |
              |  |  Firewall   |  |
              |  |  Endpoint   |  |
              |  +------+------+  |
              |         |         |
              +---------+---------+
                        |
                   +----+----+
                   |   TGW   |
                   +----+----+
                   /         \
        +---------+         +---------+
        | Spoke-1 |         | Spoke-2 |
        | VPC     |         | VPC     |
        +---------+         +---------+
```

### Requirements

1. Inspection VPC with public and firewall subnets
2. AWS Network Firewall with stateless and stateful rule groups
3. Transit Gateway connecting spoke VPCs and inspection VPC
4. TGW routing that forces all traffic through the firewall
5. Stateless rules: block known bad ports (23/Telnet, 445/SMB)
6. Stateful rules: allow HTTP/HTTPS, block specific domains
7. Firewall logging to CloudWatch Logs

## Hints

<details>
<summary>Hint 1: Inspection VPC Subnet Design</summary>

The inspection VPC needs three subnet tiers:

1. **Public subnet**: IGW route, NAT Gateway for spoke egress
2. **Firewall subnet**: Network Firewall endpoint lives here
3. **TGW subnet**: Transit Gateway attachment

```hcl
resource "aws_vpc" "inspection" {
  cidr_block           = "10.100.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags                 = { Name = "inspection-vpc" }
}

resource "aws_subnet" "firewall" {
  vpc_id            = aws_vpc.inspection.id
  cidr_block        = "10.100.1.0/24"
  availability_zone = data.aws_availability_zones.available.names[0]
  tags              = { Name = "inspection-firewall" }
}

resource "aws_subnet" "public" {
  vpc_id                  = aws_vpc.inspection.id
  cidr_block              = "10.100.2.0/24"
  availability_zone       = data.aws_availability_zones.available.names[0]
  map_public_ip_on_launch = true
  tags                    = { Name = "inspection-public" }
}

resource "aws_subnet" "tgw" {
  vpc_id            = aws_vpc.inspection.id
  cidr_block        = "10.100.3.0/24"
  availability_zone = data.aws_availability_zones.available.names[0]
  tags              = { Name = "inspection-tgw" }
}
```

Traffic flow for spoke-to-internet:
1. Spoke VPC -> TGW -> TGW subnet
2. TGW subnet -> Firewall subnet (Network Firewall inspects)
3. Firewall subnet -> Public subnet (NAT Gateway)
4. Public subnet -> IGW -> Internet

</details>

<details>
<summary>Hint 2: Network Firewall with Rule Groups</summary>

```hcl
# Stateless rule group: fast 5-tuple matching, no connection tracking
resource "aws_networkfirewall_rule_group" "stateless_block" {
  capacity = 10
  name     = "block-bad-ports"
  type     = "STATELESS"

  rule_group {
    rules_source {
      stateless_rules_and_custom_actions {

        # Block Telnet (port 23) -- insecure protocol
        stateless_rule {
          priority = 1
          rule_definition {
            actions = ["aws:drop"]
            match_attributes {
              destination {
                address_definition = "0.0.0.0/0"
              }
              destination_port {
                from_port = 23
                to_port   = 23
              }
              protocols = [6]  # TCP
              source {
                address_definition = "0.0.0.0/0"
              }
            }
          }
        }

        # Block SMB (port 445) -- common attack vector
        stateless_rule {
          priority = 2
          rule_definition {
            actions = ["aws:drop"]
            match_attributes {
              destination {
                address_definition = "0.0.0.0/0"
              }
              destination_port {
                from_port = 445
                to_port   = 445
              }
              protocols = [6]
              source {
                address_definition = "0.0.0.0/0"
              }
            }
          }
        }
      }
    }
  }
}

# Stateful rule group: deep packet inspection with Suricata syntax
resource "aws_networkfirewall_rule_group" "stateful_domain" {
  capacity = 100
  name     = "domain-filtering"
  type     = "STATEFUL"

  rule_group {
    rule_variables {
      ip_sets {
        key = "HOME_NET"
        ip_set {
          definition = ["10.0.0.0/8"]
        }
      }
    }

    rules_source {
      rules_source_list {
        generated_rules_type = "DENYLIST"
        target_types         = ["HTTP_HOST", "TLS_SNI"]
        targets              = [".malware.example.com", ".phishing.example.com"]
      }
    }
  }
}
```

Stateless vs Stateful rule groups:

| Feature | Stateless | Stateful |
|---------|-----------|---------|
| Speed | Faster (no state tracking) | Slower (maintains connection state) |
| Matching | 5-tuple (src/dst IP, port, protocol) | Deep packet inspection, domain, Suricata |
| Use case | Block known-bad ports, CIDR ranges | Domain filtering, IDS/IPS, protocol rules |
| Evaluation | By priority (like NACLs) | All rules evaluated |
| Return traffic | Must handle explicitly | Automatically tracked |

</details>

<details>
<summary>Hint 3: Firewall Policy</summary>

The firewall policy combines rule groups and defines the default action:

```hcl
resource "aws_networkfirewall_firewall_policy" "this" {
  name = "inspection-policy"

  firewall_policy {
    # Stateless: evaluate rules by priority, then forward to stateful engine
    stateless_default_actions          = ["aws:forward_to_sfe"]
    stateless_fragment_default_actions = ["aws:forward_to_sfe"]

    stateless_rule_group_reference {
      resource_arn = aws_networkfirewall_rule_group.stateless_block.arn
      priority     = 1
    }

    # Stateful: default action for traffic that matches no stateful rule
    stateful_engine_options {
      rule_order = "STRICT_ORDER"
    }

    stateful_default_actions = ["aws:drop_strict"]

    stateful_rule_group_reference {
      resource_arn = aws_networkfirewall_rule_group.stateful_domain.arn
      priority     = 1
    }
  }
}
```

The flow:
1. **Stateless rules** evaluated first by priority
2. Matched traffic: action applied (drop, pass, forward_to_sfe)
3. Unmatched traffic: `stateless_default_actions` applied
4. `aws:forward_to_sfe` sends to stateful engine
5. **Stateful rules** evaluated (all rules, not by priority unless STRICT_ORDER)
6. Default action for unmatched stateful traffic: `aws:drop_strict` or `aws:alert_strict`

</details>

<details>
<summary>Hint 4: TGW Routing for Centralized Inspection</summary>

The critical routing configuration forces all traffic through the firewall:

```hcl
resource "aws_ec2_transit_gateway" "this" {
  default_route_table_association = "disable"
  default_route_table_propagation = "disable"
  dns_support                     = "enable"

  tags = { Name = "inspection-tgw" }
}

# Inspection VPC attachment -- enable appliance mode!
resource "aws_ec2_transit_gateway_vpc_attachment" "inspection" {
  transit_gateway_id = aws_ec2_transit_gateway.this.id
  vpc_id             = aws_vpc.inspection.id
  subnet_ids         = [aws_subnet.tgw.id]

  # CRITICAL: appliance_mode_support ensures symmetric routing
  # Without this, request and response may traverse different
  # firewall endpoints, breaking stateful inspection
  appliance_mode_support = "enable"

  tags = { Name = "inspection-attachment" }
}

# Spoke route table: all traffic goes to inspection VPC
resource "aws_ec2_transit_gateway_route" "spoke_default" {
  destination_cidr_block         = "0.0.0.0/0"
  transit_gateway_route_table_id = aws_ec2_transit_gateway_route_table.spoke.id
  transit_gateway_attachment_id  = aws_ec2_transit_gateway_vpc_attachment.inspection.id
}

# Inspection route table: routes to spoke VPCs
resource "aws_ec2_transit_gateway_route_table_propagation" "spokes" {
  for_each = { for k, v in aws_ec2_transit_gateway_vpc_attachment.spoke : k => v }

  transit_gateway_attachment_id  = each.value.id
  transit_gateway_route_table_id = aws_ec2_transit_gateway_route_table.inspection.id
}
```

**appliance_mode_support = "enable"** is the most important setting. Without it, TGW may route the request through one AZ's firewall endpoint and the response through another, causing the stateful engine to drop packets because it does not have the full connection context.

</details>

<details>
<summary>Hint 5: Firewall Logging</summary>

```hcl
resource "aws_networkfirewall_logging_configuration" "this" {
  firewall_arn = aws_networkfirewall_firewall.this.arn

  logging_configuration {
    log_destination_config {
      log_destination = {
        logGroup = aws_cloudwatch_log_group.firewall_alert.name
      }
      log_destination_type = "CloudWatchLogs"
      log_type             = "ALERT"
    }

    log_destination_config {
      log_destination = {
        logGroup = aws_cloudwatch_log_group.firewall_flow.name
      }
      log_destination_type = "CloudWatchLogs"
      log_type             = "FLOW"
    }
  }
}

resource "aws_cloudwatch_log_group" "firewall_alert" {
  name              = "/aws/network-firewall/alert"
  retention_in_days = 7
}

resource "aws_cloudwatch_log_group" "firewall_flow" {
  name              = "/aws/network-firewall/flow"
  retention_in_days = 7
}
```

Two log types:
- **ALERT**: logs when a stateful rule matches (drop, alert, reject actions)
- **FLOW**: logs all traffic flow metadata (similar to VPC Flow Logs but at the firewall level)

</details>

## Spot the Bug

A team deploys Network Firewall with a TGW inspection architecture but stateful rules intermittently fail -- some connections are dropped even though they should be allowed:

```hcl
resource "aws_ec2_transit_gateway_vpc_attachment" "inspection" {
  transit_gateway_id = aws_ec2_transit_gateway.this.id
  vpc_id             = aws_vpc.inspection.id
  subnet_ids         = [aws_subnet.tgw_a.id, aws_subnet.tgw_b.id]

  # appliance_mode_support not set (defaults to "disable")
}
```

<details>
<summary>Explain the bug</summary>

**Missing `appliance_mode_support = "enable"` causes asymmetric routing.** When a TGW attachment spans multiple AZs without appliance mode, TGW may route the request packet through AZ-a's firewall endpoint and the response packet through AZ-b's firewall endpoint. The stateful engine in AZ-b has no record of the original connection, so it drops the response.

This happens because TGW uses ECMP hashing by default, and the request and response packets have different source/destination IPs (they are swapped), resulting in different hash values and potentially different AZ selection.

**The fix:**

```hcl
resource "aws_ec2_transit_gateway_vpc_attachment" "inspection" {
  transit_gateway_id     = aws_ec2_transit_gateway.this.id
  vpc_id                 = aws_vpc.inspection.id
  subnet_ids             = [aws_subnet.tgw_a.id, aws_subnet.tgw_b.id]
  appliance_mode_support = "enable"  # Ensures symmetric routing
}
```

With appliance mode enabled, TGW routes both the request and response through the same AZ, ensuring the stateful firewall sees the complete connection. This is required for any stateful inspection appliance (Network Firewall, third-party firewalls, IDS/IPS).

</details>

## Network Firewall vs Alternatives

| Feature | Security Groups | NACLs | Network Firewall |
|---------|----------------|-------|-----------------|
| **Scope** | Instance/ENI | Subnet | VPC/TGW |
| **State** | Stateful | Stateless | Both |
| **Domain filtering** | No | No | Yes |
| **IDS/IPS** | No | No | Yes (Suricata) |
| **Deny rules** | No | Yes | Yes |
| **Cost** | Free | Free | $0.395/hr + $0.065/GB |
| **Use case** | Instance access control | Subnet filtering | Deep inspection, compliance |

## Verify What You Learned

```bash
# Verify Network Firewall exists
aws network-firewall describe-firewall \
  --firewall-name "inspection-demo" \
  --query "Firewall.{Name:FirewallName,Status:FirewallStatus.Status,Endpoints:FirewallStatus.SyncStates}" \
  --output json 2>/dev/null || echo "Create the firewall first"
```

```bash
# Verify firewall policy
aws network-firewall describe-firewall-policy \
  --firewall-policy-name "inspection-policy" \
  --query "FirewallPolicy.{StatelessDefault:StatelessDefaultActions,StatefulDefault:StatefulDefaultActions}" \
  --output json 2>/dev/null || echo "Create the policy first"
```

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

**Important:** Network Firewall charges $0.395/hr. Destroy immediately when finished.

```bash
terraform destroy -auto-approve
```

Network Firewall deletion can take 5-10 minutes. Verify:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

You deployed centralized network inspection with AWS Network Firewall. In the next exercise, you will explore **Route 53 routing policies** -- comparing Simple, Weighted, Latency, Failover, Geolocation, Geoproximity, and Multivalue Answer routing to control how DNS queries are resolved for your applications.

## Summary

- **AWS Network Firewall** provides stateful deep packet inspection, domain filtering, and IDS/IPS at the VPC level
- **Stateless rules** match on 5-tuple (fast, like NACLs); **stateful rules** use Suricata syntax for deep inspection
- The **centralized inspection** pattern routes all traffic through a dedicated inspection VPC via Transit Gateway
- **appliance_mode_support = "enable"** on the TGW attachment is critical to prevent asymmetric routing that breaks stateful inspection
- Firewall policy flow: stateless rules first (by priority), then `forward_to_sfe` sends unmatched traffic to the stateful engine
- Network Firewall costs **$0.395/hr per endpoint** (~$285/month) plus **$0.065/GB** data processed -- evaluate whether the inspection requirement justifies the cost
- Use Network Firewall when you need **domain filtering, IDS/IPS, or Suricata rules** -- use SGs/NACLs for basic access control
- **ALERT** logs capture rule matches; **FLOW** logs capture all traffic metadata through the firewall

## Reference

- [AWS Network Firewall](https://docs.aws.amazon.com/network-firewall/latest/developerguide/what-is-aws-network-firewall.html)
- [Network Firewall Rule Groups](https://docs.aws.amazon.com/network-firewall/latest/developerguide/rule-groups.html)
- [Centralized Inspection Architecture](https://docs.aws.amazon.com/prescriptive-guidance/latest/inline-traffic-inspection-third-party-appliances/architecture-1.html)
- [Terraform aws_networkfirewall_firewall](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/networkfirewall_firewall)

## Additional Resources

- [Suricata Rule Format](https://docs.aws.amazon.com/network-firewall/latest/developerguide/suricata-examples.html) -- writing custom Suricata rules for Network Firewall stateful engine
- [Network Firewall Pricing](https://aws.amazon.com/network-firewall/pricing/) -- per-endpoint and per-GB costs by region
- [Appliance Mode on TGW](https://docs.aws.amazon.com/vpc/latest/tgw/transit-gateway-appliance-scenario.html) -- detailed explanation of symmetric routing for inspection appliances
- [Network Firewall vs Third-Party](https://docs.aws.amazon.com/prescriptive-guidance/latest/inline-traffic-inspection-third-party-appliances/comparison.html) -- comparing AWS-native firewall with Palo Alto, Fortinet, and Check Point
- [Network Firewall Logging](https://docs.aws.amazon.com/network-firewall/latest/developerguide/firewall-logging.html) -- configuring ALERT and FLOW logs to CloudWatch, S3, or Kinesis

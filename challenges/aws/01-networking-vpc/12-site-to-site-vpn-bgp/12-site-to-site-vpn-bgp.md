# 12. Site-to-Site VPN with BGP Dynamic Routing

<!--
difficulty: insane
concepts: [vpn-gateway, customer-gateway, bgp, dynamic-routing, ipsec]
tools: [terraform, aws-cli]
estimated_time: 120m
bloom_level: create
prerequisites: [01-08]
aws_cost: ~$0.30/hr
-->

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.5 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Exercise 08 completed | Understand Transit Gateway basics |

## The Scenario

Your organization needs hybrid connectivity between AWS and an on-premises data center during a cloud migration. Simulate the on-premises side using an EC2 instance running StrongSwan as the customer gateway device. Establish a fully redundant Site-to-Site VPN with BGP dynamic routing through a Transit Gateway so that new subnets are advertised automatically without manual route table updates.

## Constraints

1. Transit Gateway (not Virtual Private Gateway) terminates the VPN. Attach the cloud VPC via a VPC attachment.
2. Customer Gateway registered with the EIP of the StrongSwan instance. Customer ASN: `65000`, AWS ASN: `64512`.
3. StrongSwan runs on an EC2 instance in a separate "on-premises" VPC (`172.16.0.0/16`). Configure IPsec tunnels using pre-shared keys from the VPN connection XML.
4. BGP daemon (FRRouting or BIRD) on StrongSwan peers with AWS tunnel inside IPs and advertises the on-premises CIDR.
5. Both VPN tunnels active simultaneously (dual-tunnel redundancy).
6. TGW route propagation enabled -- on-premises CIDR appears via BGP, not static routes.
7. Demonstrate failover: stop one tunnel, verify traffic continues over the other.
8. Security groups allow UDP 500, UDP 4500, ESP (protocol 50) for StrongSwan; ICMP from on-premises CIDR for cloud instances.

## Success Criteria

- Both tunnels show `UP` in `aws ec2 describe-vpn-connections`.
- BGP sessions established on both tunnels (verify via `vtysh -c "show bgp summary"` on StrongSwan).
- TGW route table shows on-premises CIDR as a propagated route (`aws ec2 search-transit-gateway-routes`).
- Ping succeeds bidirectionally between cloud VPC and on-premises VPC private IPs.
- After stopping one tunnel (`ipsec down tunnel-1`), pings continue over the surviving tunnel.
- Restarting the tunnel restores dual-tunnel operation.

## Verification Commands

```bash
aws ec2 describe-vpn-connections \
  --filters "Name=tag:Name,Values=<your-vpn-name>" \
  --query "VpnConnections[].VgwTelemetry[].{Tunnel:OutsideIpAddress,Status:Status}" \
  --output table

aws ec2 search-transit-gateway-routes \
  --transit-gateway-route-table-id <your-tgw-rt-id> \
  --filters "Name=type,Values=propagated" \
  --output table

ping -c 5 <on-premises-instance-private-ip>   # from cloud VPC instance
ping -c 5 <cloud-instance-private-ip>          # from on-premises instance
```

## Cleanup

```bash
terraform destroy
```

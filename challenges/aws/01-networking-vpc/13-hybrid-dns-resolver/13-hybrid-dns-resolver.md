# 13. Hybrid DNS with Route 53 Resolver Endpoints

<!--
difficulty: insane
concepts: [route53-resolver, inbound-endpoint, outbound-endpoint, forwarding-rules, conditional-forwarding]
tools: [terraform, aws-cli]
estimated_time: 120m
bloom_level: create
prerequisites: [01-08, 01-12]
aws_cost: ~$0.50/hr
-->

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.5 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Exercise 08 completed | Transit Gateway and VPC routing |
| Exercise 12 completed | VPN or equivalent cross-VPC connectivity |

## The Scenario

With VPN connectivity established (exercise 12), DNS is the next gap. AWS workloads need to resolve on-premises hostnames (`server.corp.example.com`) and on-premises apps need to resolve AWS private hosted zone records (`api.aws.example.internal`). Implement bidirectional DNS resolution using Route 53 Resolver endpoints and an on-premises BIND9 server.

## Constraints

1. Route 53 Private Hosted Zone for `aws.example.internal` associated with the cloud VPC. At least two A records (`api.aws.example.internal`, `db.aws.example.internal`).
2. Inbound Endpoint with ENIs in two subnets (HA) in the cloud VPC -- allows on-premises BIND9 to forward `aws.example.internal` queries to Route 53.
3. Outbound Endpoint with ENIs in two subnets in the cloud VPC -- sends `corp.example.com` queries to the on-premises BIND9.
4. Forwarding Rule: queries for `corp.example.com` route through the Outbound Endpoint to BIND9's IP.
5. BIND9 on EC2 in the on-premises VPC: authoritative for `corp.example.com` (two A records), forwarding zone for `aws.example.internal` pointing to Inbound Endpoint IPs.
6. Custom DHCP options set on the on-premises VPC uses BIND9 as DNS server.
7. Security groups allow TCP/UDP 53 on all resolver ENIs and BIND9.
8. Resolver Query Logging enabled to CloudWatch for the Outbound Endpoint.

## Success Criteria

- From on-premises: `nslookup api.aws.example.internal <inbound-endpoint-ip>` resolves correctly (query path: instance -> BIND9 -> Inbound Endpoint -> Route 53).
- From cloud VPC: `nslookup server.corp.example.com` resolves correctly (query path: instance -> Resolver -> Outbound Endpoint -> BIND9).
- CloudWatch Resolver Query Log shows forwarded queries for `corp.example.com`.
- On-premises instances resolve both domains without specifying a nameserver manually.
- Both Inbound and Outbound Endpoint ENIs report healthy status.

## Verification Commands

```bash
aws route53resolver list-resolver-endpoints \
  --query "ResolverEndpoints[].{Id:Id,Direction:Direction,Status:Status}" \
  --output table

aws route53resolver list-resolver-rules \
  --query "ResolverRules[?DomainName=='corp.example.com.'].{Id:Id,Status:Status}" \
  --output table

# From on-premises instance:
nslookup api.aws.example.internal <inbound-endpoint-ip>

# From cloud VPC instance:
nslookup server.corp.example.com

aws logs filter-log-events \
  --log-group-name <your-query-log-group> \
  --filter-pattern "corp.example.com" \
  --query "events[].message" --output text
```

## Cleanup

```bash
terraform destroy
```

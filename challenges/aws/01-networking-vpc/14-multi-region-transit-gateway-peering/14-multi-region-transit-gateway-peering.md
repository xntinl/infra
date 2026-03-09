# 14. Multi-Region Active-Passive with Transit Gateway Peering

<!--
difficulty: insane
concepts: [tgw-peering, multi-region, active-passive, route53-failover, cross-region]
tools: [terraform, aws-cli]
estimated_time: 120m
bloom_level: create
prerequisites: [01-08, 01-07]
aws_cost: ~$0.60/hr
-->

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.5 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Exercise 08 completed | Transit Gateway fundamentals |
| Exercise 07 completed | ALB and health checks |

## The Scenario

Your company needs a multi-region active-passive architecture. The primary region (us-east-1) serves all traffic; the secondary (us-west-2) runs as warm standby. Transit Gateway peering connects the regions for data replication and internal communication. Route 53 failover routing handles automatic DNS switchover when the primary health check fails.

## Constraints

1. Transit Gateway in each region. Each TGW has at least one spoke VPC attachment. Non-overlapping CIDRs (`10.1.0.0/16` for us-east-1, `10.2.0.0/16` for us-west-2).
2. TGW peering attachment between regions. Route tables on both sides route remote CIDR through the peering attachment.
3. Auto Scaling Group (min 2) behind an ALB in each region. Instances return the region name in HTTP responses.
4. ALB health checks with explicit path, interval, and thresholds.
5. Route 53 failover routing: primary record (us-east-1 ALB) with Route 53 health check, secondary record (us-west-2 ALB) as failover target. Use `evaluate_target_health = true`.
6. Route 53 health check on primary ALB: `failure_threshold = 2`, `request_interval = 10`.
7. Cross-region private connectivity must work via TGW peering (ping from us-east-1 instance to us-west-2 instance via private IPs).
8. Terraform aliased providers for multi-region management in a single configuration.

## Success Criteria

- Cross-region ping via TGW peering succeeds between private IPs.
- Normal conditions: DNS resolves to us-east-1 ALB; curl returns primary region response.
- After simulating primary failure (stop instances or break health check path): Route 53 health check fails, DNS resolves to us-west-2 ALB within ~30-60s, curl returns secondary region response.
- After restoring primary: Route 53 fails back, DNS resolves to us-east-1 ALB again.
- TGW route tables in both regions show the remote CIDR via the peering attachment.

## Verification Commands

```bash
aws ec2 describe-transit-gateway-peering-attachments \
  --query "TransitGatewayPeeringAttachments[].{Id:TransitGatewayAttachmentId,State:State}" \
  --output table

aws route53 get-health-check-status \
  --health-check-id <your-health-check-id> \
  --query "HealthCheckObservations[].{Region:Region,Status:StatusReport.Status}" \
  --output table

dig +short app.example.com
curl -s http://app.example.com

# From us-east-1 instance:
ping -c 5 <us-west-2-instance-private-ip>
```

## Cleanup

```bash
terraform destroy
```

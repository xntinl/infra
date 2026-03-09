# 15. Disaster Recovery Strategy Selection

<!--
difficulty: insane
concepts: [disaster-recovery, rpo, rto, pilot-light, warm-standby, active-active, aurora-global, dynamodb-global-tables, s3-crr, route53-health-checks]
tools: [terraform, aws-cli]
estimated_time: 120m
bloom_level: create
prerequisites: [saa-01 through saa-14]
aws_cost: ~$0.50/hr
-->

> **AWS Cost Warning:** Multi-region deployment with Aurora Global Database, DynamoDB Global Tables, S3 Cross-Region Replication, ASGs in 2 regions, Route 53 health checks. ~$0.50/hr. Destroy IMMEDIATELY when finished.

## Prerequisites

| Prerequisite | Exercises | Why |
|---|---|---|
| Multi-AZ RDS and read replicas | 01 | Aurora Global Database extends cross-region replication concepts |
| ALB, target groups, health checks | 02 | Multi-region ALBs with Route 53 failover routing |
| Auto Scaling policies | 04 | Secondary region ASG scale-up during failover |
| S3 replication and lifecycle | 05 | Cross-Region Replication for static assets and backups |
| Multi-tier HA architecture | 12 | Full-stack design across availability zones and regions |
| Well-Architected reliability pillar | 13 | DR strategy aligns with reliability best practices |
| Global Accelerator and CloudFront | 14 | Edge-layer routing during regional failover |

## The Scenario

You are the lead architect for an e-commerce platform processing $2M/day in transactions. The board requires a disaster recovery strategy with RPO <1 minute and RTO <15 minutes. The budget does not support running full capacity in two regions simultaneously (active-active is too expensive). Your task: implement a warm standby DR strategy and defend your choice against the alternatives.

The primary region runs the full production stack: ALB, ASG (min=3, max=12), Aurora MySQL cluster (writer + 2 readers), DynamoDB table for cart/session data, and S3 for product images. The secondary region must be ready to take over within 15 minutes of a regional failure, with less than 1 minute of data loss.

## Constraints

1. **RPO < 1 minute:** All data stores must replicate cross-region with sub-minute lag. Aurora Global Database provides ~1 second replication lag. DynamoDB Global Tables provide eventual consistency within seconds. S3 CRR is asynchronous but must meet the RPO for new objects.
2. **RTO < 15 minutes:** From the moment a regional failure is detected to the moment the secondary region is serving production traffic. This includes DNS propagation, ASG scale-up, and Aurora failover.
3. **Cost target:** Secondary region baseline cost must not exceed 30% of primary region cost. Warm standby runs minimum-capacity ASG (min=1) and uses Aurora Global Database secondary cluster (no additional readers until failover).
4. **Automated detection:** Route 53 health checks must detect primary region failure within 60 seconds. CloudWatch alarm triggers ASG scaling in the secondary region.
5. **Automated failover:** Route 53 failover routing policy switches DNS to secondary ALB. No manual intervention required for initial failover.
6. **Aurora planned failover:** Aurora Global Database planned failover promotes the secondary cluster to read-write. RPO = 0 for planned failovers. For unplanned failovers, use detach-and-promote with RPO ~1 second.
7. **DNS TTL:** Route 53 records must use TTL <= 60 seconds to ensure clients resolve to the new region within the RTO window.
8. **Data consistency:** After failover, DynamoDB Global Tables must resolve conflicts using last-writer-wins. The application must tolerate eventual consistency during the failover window.
9. **Defend your choice:** Document why warm standby is preferred over pilot light (RTO too slow for pilot light -- compute provisioning takes 10+ minutes) and active-active (cost exceeds budget -- 2x baseline infrastructure).
10. **Failback plan:** After the primary region recovers, document the procedure to reverse replication and fail back without data loss.

## Success Criteria

- [ ] Aurora Global Database deployed with primary cluster in us-east-1 and secondary cluster in eu-west-1
- [ ] DynamoDB Global Table replicating between both regions
- [ ] S3 Cross-Region Replication configured from primary to secondary bucket
- [ ] Primary region: ALB + ASG (min=3) + full Aurora cluster
- [ ] Secondary region: ALB + ASG (min=1) + Aurora secondary cluster (read-only)
- [ ] Route 53 failover routing policy with health checks on primary ALB
- [ ] Route 53 health check evaluation period <= 10 seconds, failure threshold <= 3
- [ ] CloudWatch alarm in secondary region triggers ASG scale-up when primary health check fails
- [ ] Simulated failover completes within 15 minutes (DNS switch + ASG scale-up + Aurora promote)
- [ ] Data written to primary before failure is available in secondary after failover (RPO verification)
- [ ] Written justification: warm standby vs pilot light vs active-active with cost comparison table
- [ ] Failback procedure documented and tested

## Verification Commands

```bash
# Verify Aurora Global Database
aws rds describe-global-clusters \
  --query "GlobalClusters[?GlobalClusterIdentifier=='dr-demo-global'].{Id:GlobalClusterIdentifier,Status:Status,Members:GlobalClusterMembers[*].{Cluster:DBClusterArn,IsWriter:IsWriter}}" \
  --output json

# Verify Aurora replication lag
aws cloudwatch get-metric-statistics \
  --namespace "AWS/RDS" \
  --metric-name "AuroraGlobalDBReplicationLag" \
  --dimensions Name=DBClusterIdentifier,Value=dr-demo-secondary \
  --start-time "$(date -u -v-10M +%Y-%m-%dT%H:%M:%S)" \
  --end-time "$(date -u +%Y-%m-%dT%H:%M:%S)" \
  --period 60 --statistics Average \
  --region eu-west-1

# Verify DynamoDB Global Table
aws dynamodb describe-table \
  --table-name dr-demo-cart \
  --query "Table.Replicas[*].{Region:RegionName,Status:ReplicaStatus}" \
  --output table

# Verify S3 Cross-Region Replication
aws s3api get-bucket-replication \
  --bucket dr-demo-assets-primary \
  --query "ReplicationConfiguration.Rules[0].{Status:Status,Destination:Destination.Bucket}"

# Verify Route 53 failover records
aws route53 list-resource-record-sets \
  --hosted-zone-id "$ZONE_ID" \
  --query "ResourceRecordSets[?Name=='app.example.com.'].{Name:Name,Type:Type,Failover:Failover,HealthCheckId:HealthCheckId}" \
  --output table

# Verify Route 53 health check
aws route53 get-health-check-status \
  --health-check-id "$HC_ID" \
  --query "HealthCheckObservations[*].{Region:Region,Status:StatusReport.Status}" \
  --output table

# Verify secondary ASG (should be min=1 during normal operation)
aws autoscaling describe-auto-scaling-groups \
  --auto-scaling-group-names "dr-demo-secondary" \
  --query "AutoScalingGroups[0].{Min:MinSize,Desired:DesiredCapacity,Max:MaxSize}" \
  --output table \
  --region eu-west-1

# Simulate failover: promote Aurora secondary
aws rds failover-global-cluster \
  --global-cluster-identifier dr-demo-global \
  --target-db-cluster-identifier arn:aws:rds:eu-west-1:ACCOUNT:cluster/dr-demo-secondary

# Verify failover completed (secondary becomes writer)
aws rds describe-global-clusters \
  --query "GlobalClusters[?GlobalClusterIdentifier=='dr-demo-global'].GlobalClusterMembers[*].{Cluster:DBClusterArn,Writer:IsWriter}" \
  --output table
```

## Cleanup

Destroy in reverse dependency order. Global resources must be dismantled before regional resources:

```bash
# 1. Remove Aurora secondary from global cluster (required before destroying)
aws rds remove-from-global-cluster \
  --global-cluster-identifier dr-demo-global \
  --db-cluster-identifier arn:aws:rds:eu-west-1:ACCOUNT:cluster/dr-demo-secondary

# 2. Destroy all Terraform resources
terraform destroy -auto-approve

# 3. Verify no resources remain
aws rds describe-global-clusters --query "GlobalClusters[?GlobalClusterIdentifier=='dr-demo-global']"
aws dynamodb describe-table --table-name dr-demo-cart 2>&1 || echo "Table deleted"
aws s3 ls s3://dr-demo-assets-primary 2>&1 || echo "Bucket deleted"
```

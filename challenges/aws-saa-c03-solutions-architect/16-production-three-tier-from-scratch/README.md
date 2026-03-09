# 16. Production-Grade Three-Tier Architecture from Scratch

<!--
difficulty: insane
concepts: [production-architecture, https, encryption-at-rest, automated-backups, auto-scaling, waf, centralized-logging, cost-optimization, sla]
tools: [terraform, aws-cli]
estimated_time: 150m
bloom_level: create
prerequisites: [saa-01 through saa-15]
aws_cost: ~$0.60/hr
-->

> **AWS Cost Warning:** Full production stack: ALB, ASG, RDS Multi-AZ, ElastiCache, WAF, CloudWatch, AWS Backup, NAT Gateways. ~$0.60/hr. Destroy IMMEDIATELY when finished.

## Prerequisites

| Prerequisite | Exercises | Why |
|---|---|---|
| RDS Multi-AZ, encryption, backups | 01, 13 | Production database requires all three |
| ALB with health checks and target groups | 02 | HTTPS termination, WAF association |
| Auto Scaling policies and right-sizing | 04, 12 | Handle 3x traffic spikes within cost target |
| S3 lifecycle and encryption | 05, 13 | Centralized log archival with encryption at rest |
| VPC networking and security groups | 06, 12 | Three-tier network isolation with least-privilege |
| Caching with ElastiCache | 12 | Sub-millisecond reads, reduce database load |
| Well-Architected best practices | 13 | Every resource must pass a WA review |
| Disaster recovery fundamentals | 15 | Backup and recovery strategy for 99.95% SLA |

## The Scenario

Your company is launching a customer-facing application expected to serve 10,000 concurrent users with a 99.95% SLA commitment. The CTO has given you a single directive: "Build it right the first time. No shortcuts." You must deliver a production-grade three-tier architecture with HTTPS everywhere, encryption at rest for all data stores, automated 30-day backups, auto-scaling for 3x traffic spikes, WAF protection, and centralized logging -- all within a $2,000/month baseline cost target.

No starter code. No hints. No solution. Design and implement the entire stack from an empty Terraform directory.

## Constraints

1. **HTTPS everywhere:** ACM certificate on ALB. All client-to-ALB traffic is HTTPS (redirect HTTP to HTTPS). ALB-to-backend can be HTTP within the VPC.
2. **Encryption at rest:** KMS-managed encryption for EBS volumes (launch template), RDS storage, S3 buckets (logs and assets), and ElastiCache. Use a single KMS key with appropriate key policy.
3. **Encryption in transit:** TLS between ALB and clients (enforced). TLS between ElastiCache nodes (transit encryption enabled). RDS SSL connections encouraged but not enforced at the database level for this exercise.
4. **Automated backups:** AWS Backup plan with 30-day retention covering RDS and EBS. Daily backup at 02:00 UTC. Backup vault with KMS encryption.
5. **Auto-scaling:** ASG with min=2, desired=3, max=9. Target tracking at 65% CPU. Scale-out cooldown 120 seconds. Scale-in cooldown 300 seconds (conservative to avoid flapping). Must handle 3x traffic spike (10K to 30K concurrent users) within 5 minutes.
6. **WAF:** AWS WAF v2 on ALB with AWS managed rule groups: AWSManagedRulesCommonRuleSet and AWSManagedRulesKnownBadInputsRuleSet. Rate limiting rule: 2000 requests per 5-minute window per IP.
7. **Centralized logging:** CloudWatch Logs for application logs (7-day retention). S3 bucket for log archival (transition to Glacier after 90 days, expire after 365 days). VPC Flow Logs to CloudWatch Logs (30-day retention). ALB access logs to S3.
8. **CloudWatch alarms:** Minimum 5 alarms: ALB 5xx rate > 1%, ASG CPU > 80%, RDS CPU > 75%, RDS free storage < 5 GB, ElastiCache evictions > 100/min. All alarms notify an SNS topic.
9. **Cost target:** Monthly baseline cost must not exceed $2,000 using on-demand pricing. Provide a cost breakdown table showing each component's estimated monthly cost. Identify which components should use reserved instances or savings plans for production.
10. **Network isolation:** Three subnet tiers across 3 AZs. Public (ALB + NAT GW), private (compute), isolated (data). Security groups enforce least-privilege: ALB accepts only 80/443 from internet, compute accepts only from ALB, data tier accepts only from compute.
11. **Tagging:** Every resource must have `Environment = "production"`, `Project = "saa-capstone"`, and `ManagedBy = "terraform"` tags.
12. **No Lambda functions in the application tier.** This is a traditional three-tier architecture exercise. EC2 instances in ASG serve the application. Use Go or Rust binaries deployed via user_data or a pre-built AMI.

## Success Criteria

- [ ] `terraform plan` produces a clean plan with zero errors
- [ ] `terraform apply` completes successfully
- [ ] ALB serves HTTPS traffic with valid ACM certificate
- [ ] HTTP requests to ALB are redirected to HTTPS
- [ ] WAF is associated with ALB and managed rules are active
- [ ] WAF rate-limiting rule blocks excessive requests from a single IP
- [ ] ASG maintains minimum 2 instances across 3 AZs
- [ ] ASG scales to 9 instances under load within 5 minutes
- [ ] RDS Multi-AZ is enabled with storage encryption
- [ ] RDS automated backup retention is 30 days
- [ ] AWS Backup plan exists with daily schedule and 30-day retention
- [ ] ElastiCache has at-rest and in-transit encryption enabled
- [ ] All S3 buckets have default encryption and public access blocked
- [ ] VPC Flow Logs are active
- [ ] ALB access logs are delivered to S3
- [ ] All 5+ CloudWatch alarms exist and are in OK state
- [ ] All resources have required tags
- [ ] Estimated monthly cost is documented and under $2,000

## Verification Commands

```bash
# HTTPS and certificate
aws elbv2 describe-listeners \
  --load-balancer-arn "$ALB_ARN" \
  --query "Listeners[*].{Port:Port,Protocol:Protocol,Cert:Certificates[0].CertificateArn}" \
  --output table

# HTTP redirect rule
aws elbv2 describe-rules \
  --listener-arn "$HTTP_LISTENER_ARN" \
  --query "Rules[*].Actions[?Type=='redirect'].RedirectConfig.Protocol" \
  --output text

# WAF association
aws wafv2 get-web-acl-for-resource \
  --resource-arn "$ALB_ARN" \
  --query "WebACL.{Name:Name,Rules:Rules[*].Name}" \
  --output json

# WAF rate limit rule
aws wafv2 get-web-acl \
  --name "production-waf" \
  --scope REGIONAL \
  --id "$WAF_ID" \
  --query "WebACL.Rules[?Name=='rate-limit'].Statement.RateBasedStatement.Limit"

# Encryption at rest: RDS
aws rds describe-db-instances \
  --db-instance-identifier production-primary \
  --query "DBInstances[0].{Encrypted:StorageEncrypted,KmsKey:KmsKeyId,MultiAZ:MultiAZ,BackupRetention:BackupRetentionPeriod}" \
  --output table

# Encryption at rest: ElastiCache
aws elasticache describe-replication-groups \
  --replication-group-id production-cache \
  --query "ReplicationGroups[0].{AtRestEncryption:AtRestEncryptionEnabled,TransitEncryption:TransitEncryptionEnabled}" \
  --output table

# Encryption at rest: S3
aws s3api get-bucket-encryption \
  --bucket "$LOGS_BUCKET" \
  --query "ServerSideEncryptionConfiguration.Rules[0].ApplyServerSideEncryptionByDefault.SSEAlgorithm"

# S3 public access blocked
aws s3api get-public-access-block \
  --bucket "$LOGS_BUCKET" \
  --query "PublicAccessBlockConfiguration"

# AWS Backup plan
aws backup list-backup-plans \
  --query "BackupPlansList[?BackupPlanName=='production-daily'].{Name:BackupPlanName,Id:BackupPlanId}" \
  --output table

# AWS Backup vault encryption
aws backup describe-backup-vault \
  --backup-vault-name production-vault \
  --query "{Name:BackupVaultName,KmsKey:EncryptionKeyArn}"

# ASG configuration
aws autoscaling describe-auto-scaling-groups \
  --auto-scaling-group-names "production-app" \
  --query "AutoScalingGroups[0].{Min:MinSize,Desired:DesiredCapacity,Max:MaxSize,AZs:AvailabilityZones,HealthCheck:HealthCheckType}" \
  --output table

# Scaling policies
aws autoscaling describe-policies \
  --auto-scaling-group-name "production-app" \
  --query "ScalingPolicies[*].{Name:PolicyName,Type:PolicyType,Target:TargetTrackingConfiguration.TargetValue}" \
  --output table

# CloudWatch alarms
aws cloudwatch describe-alarms \
  --alarm-name-prefix "production-" \
  --query "MetricAlarms[*].{Name:AlarmName,Metric:MetricName,State:StateValue}" \
  --output table

# VPC Flow Logs
aws ec2 describe-flow-logs \
  --filter Name=resource-id,Values="$VPC_ID" \
  --query "FlowLogs[0].{Status:FlowLogStatus,Destination:LogDestinationType,Traffic:TrafficType}" \
  --output table

# ALB access logs
aws elbv2 describe-load-balancer-attributes \
  --load-balancer-arn "$ALB_ARN" \
  --query "Attributes[?Key=='access_logs.s3.enabled'].Value" \
  --output text

# Tag compliance (spot check)
aws rds list-tags-for-resource \
  --resource-name "$RDS_ARN" \
  --query "TagList[?Key=='Environment' || Key=='Project' || Key=='ManagedBy']" \
  --output table
```

## Cleanup

This stack costs ~$0.60/hr ($432/month if left running). Destroy immediately when verification is complete:

```bash
# Empty S3 buckets first (required before Terraform can delete them)
aws s3 rm "s3://${LOGS_BUCKET}" --recursive
aws s3 rm "s3://${ASSETS_BUCKET}" --recursive

# Destroy all resources
terraform destroy -auto-approve

# Verify complete cleanup
aws elbv2 describe-load-balancers --query "LoadBalancers[?LoadBalancerName=='production-alb']"
aws rds describe-db-instances --db-instance-identifier production-primary 2>&1 || echo "RDS deleted"
aws wafv2 list-web-acls --scope REGIONAL --query "WebACLs[?Name=='production-waf']"
aws backup list-backup-plans --query "BackupPlansList[?BackupPlanName=='production-daily']"
```

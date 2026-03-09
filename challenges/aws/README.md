# AWS Challenges & Exercises

> ~200 desafios practicos de AWS organizados en 22 categorias (12 AWS General + 10 Terraform Avanzado).
> Cada challenge incluye una guia completa con pasos, validacion y recursos.

---

## Parte 1: AWS General

### 01 - Networking & VPC

| # | Exercise | Difficulty | AWS Cost |
|---|----------|------------|----------|
| 01 | [Your First VPC: Internet Gateway and Public Subnet](01-networking-vpc/01-your-first-vpc/) | Basic | ~$0.01/hr |
| 02 | [Public and Private Subnets with NAT Gateway](01-networking-vpc/02-public-and-private-subnets/) | Basic | ~$0.05/hr |
| 03 | [Security Groups and NACLs: Defense in Depth](01-networking-vpc/03-security-groups-and-nacls/) | Basic | ~$0.02/hr |
| 04 | [Production Multi-AZ Multi-Tier VPC](01-networking-vpc/04-production-multi-az-vpc/) | Intermediate | ~$0.15/hr |
| 05 | [VPC Peering and Cross-VPC DNS Resolution](01-networking-vpc/05-vpc-peering-and-dns/) | Intermediate | ~$0.05/hr |
| 06 | [CloudFront with Custom Origin, OAC, and WAF](01-networking-vpc/06-cloudfront-custom-origin/) | Intermediate | ~$0.08/hr |
| 07 | [Route 53 Routing Policies and Health Checks](01-networking-vpc/07-route53-routing-and-health-checks/) | Intermediate | ~$0.07/hr |
| 08 | [Multi-VPC Hub-and-Spoke with Transit Gateway](01-networking-vpc/08-transit-gateway-hub-and-spoke/) | Advanced | ~$0.25/hr |
| 09 | [AWS PrivateLink: Service Provider and Consumer](01-networking-vpc/09-privatelink-provider-consumer/) | Advanced | ~$0.12/hr |
| 10 | [VPC Lattice for Service-to-Service Communication](01-networking-vpc/10-vpc-lattice-service-to-service/) | Advanced | ~$0.35/hr |
| 11 | [AWS Network Firewall with Centralized Inspection](01-networking-vpc/11-network-firewall-centralized/) | Advanced | ~$0.45/hr |
| 12 | [Site-to-Site VPN with BGP Dynamic Routing](01-networking-vpc/12-site-to-site-vpn-bgp/) | Insane | ~$0.30/hr |
| 13 | [Hybrid DNS with Route 53 Resolver Endpoints](01-networking-vpc/13-hybrid-dns-resolver/) | Insane | ~$0.50/hr |
| 14 | [Multi-Region Active-Passive with Transit Gateway Peering](01-networking-vpc/14-multi-region-transit-gateway-peering/) | Insane | ~$0.60/hr |

### 02 - Compute

| # | Challenge | Complejidad |
|---|-----------|-------------|
| 2.01 | [ECS Blue/Green with CodeDeploy](02-compute/2.01-ecs-blue-green-codedeploy/) | Avanzado |
| 2.02 | [Lambda Custom Runtime (Rust/Go)](02-compute/2.02-lambda-custom-runtime-rust-go/) | Avanzado |
| 2.03 | [Auto Scaling Mixed Instances + Spot](02-compute/2.03-auto-scaling-mixed-instances-spot/) | Avanzado |
| 2.04 | [EKS Karpenter Node Autoscaling](02-compute/2.04-eks-karpenter-node-autoscaling/) | Experto |
| 2.05 | [Lambda Provisioned Concurrency Auto Scaling](02-compute/2.05-lambda-provisioned-concurrency-auto-scaling/) | Intermedio |
| 2.06 | [ECS Service Connect Microservices](02-compute/2.06-ecs-service-connect-microservices/) | Intermedio |
| 2.07 | [WordPress Architecture Evolution](02-compute/2.07-wordpress-architecture-evolution/) | Intermedio |
| 2.08 | [Lambda Container Images Optimization](02-compute/2.08-lambda-container-images-optimization/) | Intermedio |
| 2.09 | [Graviton Migration Benchmarking](02-compute/2.09-graviton-migration-benchmarking/) | Intermedio |
| 2.10 | [EKS IRSA (IAM Roles for Service Accounts)](02-compute/2.10-eks-irsa-iam-roles-service-accounts/) | Intermedio |

### 03 - Storage & Databases

| # | Challenge | Complejidad |
|---|-----------|-------------|
| 3.01 | [S3 Cross-Region Replication + Lifecycle](03-storage-databases/3.01-s3-cross-region-replication-lifecycle/) | Intermedio |
| 3.02 | [DynamoDB Single-Table Design GSI/LSI](03-storage-databases/3.02-dynamodb-single-table-design-gsi-lsi/) | Avanzado |
| 3.03 | [DynamoDB Global Tables Conflict Resolution](03-storage-databases/3.03-dynamodb-global-tables-conflict-resolution/) | Avanzado |
| 3.04 | [DAX Cluster Cache Patterns](03-storage-databases/3.04-dax-cluster-cache-patterns/) | Intermedio |
| 3.05 | [Aurora Global Database Managed Failover](03-storage-databases/3.05-aurora-global-database-managed-failover/) | Avanzado |
| 3.06 | [ElastiCache Redis Cluster + Pub/Sub](03-storage-databases/3.06-elasticache-redis-cluster-pubsub/) | Avanzado |
| 3.07 | [S3 Event-Driven Processing Pipeline](03-storage-databases/3.07-s3-event-driven-processing-pipeline/) | Intermedio |
| 3.08 | [Database Migration DMS + SCT](03-storage-databases/3.08-database-migration-dms-sct/) | Avanzado |
| 3.09 | [S3 Intelligent Tiering + Storage Lens](03-storage-databases/3.09-s3-intelligent-tiering-storage-lens/) | Intermedio |
| 3.10 | [EFS Cross-AZ Performance Testing](03-storage-databases/3.10-efs-cross-az-performance-testing/) | Intermedio |

### 04 - Security

| # | Challenge | Complejidad |
|---|-----------|-------------|
| 4.01 | [Organizations SCPs Multi-Account Governance](04-security/4.01-organizations-scps-multi-account-governance/) | Avanzado |
| 4.02 | [IAM Advanced Policy Conditions + Permission Boundaries](04-security/4.02-iam-advanced-policy-conditions-permission-boundaries/) | Avanzado |
| 4.03 | [GuardDuty + Security Hub Automated Remediation](04-security/4.03-guardduty-security-hub-automated-remediation/) | Avanzado |
| 4.04 | [KMS Envelope Encryption Cross-Account](04-security/4.04-kms-envelope-encryption-cross-account/) | Avanzado |
| 4.05 | [WAF WebACL Custom Rules + Bot Control](04-security/4.05-waf-webacl-custom-rules-bot-control/) | Intermedio |
| 4.06 | [Secrets Manager Lambda Rotation](04-security/4.06-secrets-manager-lambda-rotation/) | Intermedio |
| 4.07 | [AWS Config Rules Auto-Remediation](04-security/4.07-aws-config-rules-auto-remediation/) | Intermedio |
| 4.08 | [Data Perimeter VPC Endpoints + Conditions](04-security/4.08-data-perimeter-vpc-endpoints-conditions/) | Experto |
| 4.09 | [CloudTrail Lake Advanced Queries](04-security/4.09-cloudtrail-lake-advanced-queries/) | Avanzado |
| 4.10 | [IAM Identity Center SSO External IdP](04-security/4.10-iam-identity-center-sso-external-idp/) | Avanzado |

### 05 - CI/CD & DevOps

| # | Challenge | Complejidad |
|---|-----------|-------------|
| 5.01 | [Full CodePipeline Blue/Green ECS](05-cicd-devops/5.01-full-codepipeline-blue-green-ecs/) | Avanzado |
| 5.02 | [Canary Deployments Lambda + CodeDeploy](05-cicd-devops/5.02-canary-deployments-lambda-codedeploy/) | Intermedio |
| 5.03 | [GitOps ArgoCD on EKS](05-cicd-devops/5.03-gitops-argocd-eks/) | Experto |
| 5.04 | [Multi-Account CI/CD Cross-Account Deployment](05-cicd-devops/5.04-multi-account-cicd-cross-account-deployment/) | Avanzado |
| 5.05 | [Infrastructure Testing Terratest](05-cicd-devops/5.05-infrastructure-testing-terratest/) | Avanzado |
| 5.06 | [Container Image Scanning Pipeline](05-cicd-devops/5.06-container-image-scanning-pipeline/) | Intermedio |
| 5.07 | [Feature Flags AppConfig](05-cicd-devops/5.07-feature-flags-appconfig/) | Intermedio |
| 5.08 | [Self-Mutating CDK Pipeline](05-cicd-devops/5.08-self-mutating-cdk-pipeline/) | Avanzado |
| 5.09 | [Database Schema Migrations CI/CD](05-cicd-devops/5.09-database-schema-migrations-cicd/) | Avanzado |
| 5.10 | [Chaos Engineering Fault Injection Simulator](05-cicd-devops/5.10-chaos-engineering-fault-injection-simulator/) | Avanzado |

### 06 - Serverless

| # | Challenge | Complejidad |
|---|-----------|-------------|
| 6.01 | [Step Functions Saga Pattern](06-serverless/6.01-step-functions-saga-pattern/) | Avanzado |
| 6.02 | [EventBridge Event-Driven Microservices](06-serverless/6.02-eventbridge-event-driven-microservices/) | Avanzado |
| 6.03 | [SQS/SNS Fan-Out + Filtering](06-serverless/6.03-sqs-sns-fan-out-filtering/) | Intermedio |
| 6.04 | [API Gateway WebSocket Real-Time Chat](06-serverless/6.04-api-gateway-websocket-real-time-chat/) | Intermedio |
| 6.05 | [AppSync GraphQL DynamoDB Resolvers](06-serverless/6.05-appsync-graphql-dynamodb-resolvers/) | Avanzado |
| 6.06 | [Step Functions Express High-Volume](06-serverless/6.06-step-functions-express-high-volume/) | Intermedio |
| 6.07 | [Serverless REST API Production-Grade](06-serverless/6.07-serverless-rest-api-production-grade/) | Avanzado |
| 6.08 | [EventBridge Scheduler Temporal Patterns](06-serverless/6.08-eventbridge-scheduler-temporal-patterns/) | Intermedio |
| 6.09 | [Serverlesspresso Coffee Shop Ordering](06-serverless/6.09-serverlesspresso-coffee-shop-ordering/) | Experto |
| 6.10 | [Lambda Powertools Implementation](06-serverless/6.10-lambda-powertools-implementation/) | Intermedio |

### 07 - Monitoring & Observability

| # | Challenge | Complejidad |
|---|-----------|-------------|
| 7.01 | [One Observability Workshop Full Stack](07-monitoring-observability/7.01-one-observability-workshop-full-stack/) | Avanzado |
| 7.02 | [CloudWatch Logs Insights Advanced Queries](07-monitoring-observability/7.02-cloudwatch-logs-insights-advanced-queries/) | Intermedio |
| 7.03 | [Composite Alarms + Suppression](07-monitoring-observability/7.03-composite-alarms-suppression/) | Intermedio |
| 7.04 | [Distributed Tracing OpenTelemetry ADOT EKS](07-monitoring-observability/7.04-distributed-tracing-opentelemetry-adot-eks/) | Avanzado |
| 7.05 | [Custom CloudWatch Metrics EMF](07-monitoring-observability/7.05-custom-cloudwatch-metrics-emf/) | Intermedio |
| 7.06 | [Container Insights ECS/EKS](07-monitoring-observability/7.06-container-insights-ecs-eks/) | Intermedio |
| 7.07 | [Managed Prometheus + Grafana](07-monitoring-observability/7.07-managed-prometheus-grafana/) | Avanzado |
| 7.08 | [Synthetic Canary Monitoring](07-monitoring-observability/7.08-synthetic-canary-monitoring/) | Intermedio |
| 7.09 | [Application Signals Service-Level Objectives](07-monitoring-observability/7.09-application-signals-service-level-objectives/) | Avanzado |
| 7.10 | [Cross-Account Centralized Observability](07-monitoring-observability/7.10-cross-account-centralized-observability/) | Experto |

### 08 - Infrastructure as Code

| # | Challenge | Complejidad |
|---|-----------|-------------|
| 8.01 | [Terraform Module Library Versioned Releases](08-infrastructure-as-code/8.01-terraform-module-library-versioned-releases/) | Avanzado |
| 8.02 | [Terraform State Management Migration + Recovery](08-infrastructure-as-code/8.02-terraform-state-management-migration-recovery/) | Avanzado |
| 8.03 | [Terraform Workspace Multi-Environment](08-infrastructure-as-code/8.03-terraform-workspace-multi-environment/) | Intermedio |
| 8.04 | [Terraform Drift Detection Remediation Pipeline](08-infrastructure-as-code/8.04-terraform-drift-detection-remediation-pipeline/) | Avanzado |
| 8.05 | [CloudFormation StackSets Organization Deployment](08-infrastructure-as-code/8.05-cloudformation-stacksets-organization-deployment/) | Avanzado |
| 8.06 | [Terraform AWS Landing Zone](08-infrastructure-as-code/8.06-terraform-aws-landing-zone/) | Experto |
| 8.07 | [Terraform Custom Provider Development](08-infrastructure-as-code/8.07-terraform-custom-provider-development/) | Experto |
| 8.08 | [Terragrunt DRY Multi-Account](08-infrastructure-as-code/8.08-terragrunt-dry-multi-account/) | Avanzado |
| 8.09 | [Policy-as-Code Sentinel/OPA](08-infrastructure-as-code/8.09-policy-as-code-sentinel-opa/) | Avanzado |
| 8.10 | [CDK Constructs Library Organization Standards](08-infrastructure-as-code/8.10-cdk-constructs-library-organization-standards/) | Avanzado |

### 09 - Data & Analytics

| # | Challenge | Complejidad |
|---|-----------|-------------|
| 9.01 | [Serverless Data Lake Glue + Athena + Lake Formation](09-data-analytics/9.01-serverless-data-lake-glue-athena-lakeformation/) | Avanzado |
| 9.02 | [Real-Time Streaming Kinesis Data Streams](09-data-analytics/9.02-real-time-streaming-kinesis-data-streams/) | Avanzado |
| 9.03 | [Kinesis Firehose Data Lake](09-data-analytics/9.03-kinesis-firehose-data-lake/) | Intermedio |
| 9.04 | [Athena Federated Query Multiple Sources](09-data-analytics/9.04-athena-federated-query-multiple-sources/) | Avanzado |
| 9.05 | [EMR Spot Instances + Spark](09-data-analytics/9.05-emr-spot-instances-spark/) | Avanzado |
| 9.06 | [Glue Interactive Sessions + Data Quality](09-data-analytics/9.06-glue-interactive-sessions-data-quality/) | Intermedio |
| 9.07 | [Lake Formation Cross-Account Data Sharing](09-data-analytics/9.07-lake-formation-cross-account-data-sharing/) | Avanzado |
| 9.08 | [EventBridge Pipes ETL Orchestration](09-data-analytics/9.08-eventbridge-pipes-etl-orchestration/) | Intermedio |
| 9.09 | [Redshift Serverless + Spectrum](09-data-analytics/9.09-redshift-serverless-spectrum/) | Avanzado |
| 9.10 | [QuickSight Embedded Analytics Dashboard](09-data-analytics/9.10-quicksight-embedded-analytics-dashboard/) | Avanzado |

### 10 - High Availability & DR

| # | Challenge | Complejidad |
|---|-----------|-------------|
| 10.01 | [Multi-Region Active-Passive Aurora Global](10-high-availability-dr/10.01-multi-region-active-passive-aurora-global/) | Experto |
| 10.02 | [Multi-Region Active-Active DynamoDB Global](10-high-availability-dr/10.02-multi-region-active-active-dynamodb-global/) | Experto |
| 10.03 | [Cross-Region S3 Replication + Backup Strategy](10-high-availability-dr/10.03-cross-region-s3-replication-backup-strategy/) | Avanzado |
| 10.04 | [Route 53 Application Recovery Controller](10-high-availability-dr/10.04-route53-application-recovery-controller/) | Experto |
| 10.05 | [Pilot Light DR Strategy](10-high-availability-dr/10.05-pilot-light-dr-strategy/) | Avanzado |
| 10.06 | [Warm Standby Auto Scaling Failover](10-high-availability-dr/10.06-warm-standby-auto-scaling-failover/) | Avanzado |
| 10.07 | [Multi-AZ Resilience Testing FIS](10-high-availability-dr/10.07-multi-az-resilience-testing-fis/) | Avanzado |
| 10.08 | [Serverless Multi-Region Event Replication](10-high-availability-dr/10.08-serverless-multi-region-event-replication/) | Experto |
| 10.09 | [Backup Compliance Automation AWS Backup](10-high-availability-dr/10.09-backup-compliance-automation-aws-backup/) | Intermedio |
| 10.10 | [Elastic Disaster Recovery DRS](10-high-availability-dr/10.10-elastic-disaster-recovery-drs/) | Avanzado |

### 11 - Cost Optimization

| # | Challenge | Complejidad |
|---|-----------|-------------|
| 11.01 | [Savings Plans Analysis + Purchase Strategy](11-cost-optimization/11.01-savings-plans-analysis-purchase-strategy/) | Intermedio |
| 11.02 | [Right-Sizing Compute Optimizer](11-cost-optimization/11.02-right-sizing-compute-optimizer/) | Intermedio |
| 11.03 | [CUR Cost Usage Report Analysis Pipeline](11-cost-optimization/11.03-cur-cost-usage-report-analysis-pipeline/) | Avanzado |
| 11.04 | [Automated Idle Resource Cleanup](11-cost-optimization/11.04-automated-idle-resource-cleanup/) | Intermedio |
| 11.05 | [Budget Alerts + Anomaly Detection](11-cost-optimization/11.05-budget-alerts-anomaly-detection/) | Intermedio |
| 11.06 | [Spot Instance Portfolio Flexibility](11-cost-optimization/11.06-spot-instance-portfolio-flexibility/) | Avanzado |
| 11.07 | [Data Transfer Cost Optimization](11-cost-optimization/11.07-data-transfer-cost-optimization/) | Intermedio |
| 11.08 | [Lambda Cost Optimization Power Tuning](11-cost-optimization/11.08-lambda-cost-optimization-power-tuning/) | Intermedio |
| 11.09 | [Container Cost Optimization ECS/EKS](11-cost-optimization/11.09-container-cost-optimization-ecs-eks/) | Avanzado |
| 11.10 | [FinOps Dashboard + Chargeback Model](11-cost-optimization/11.10-finops-dashboard-chargeback-model/) | Avanzado |

### 12 - Containers & Orchestration

| # | Challenge | Complejidad |
|---|-----------|-------------|
| 12.01 | [ECS Fargate Service Discovery + Auto Scaling](12-containers-orchestration/12.01-ecs-fargate-service-discovery-auto-scaling/) | Intermedio |
| 12.02 | [EKS App Mesh Service Mesh](12-containers-orchestration/12.02-eks-app-mesh-service-mesh/) | Experto |
| 12.03 | [ECR Image Lifecycle Cross-Account Sharing](12-containers-orchestration/12.03-ecr-image-lifecycle-cross-account-sharing/) | Intermedio |
| 12.04 | [ECS EC2 Capacity Providers + Spot](12-containers-orchestration/12.04-ecs-ec2-capacity-providers-spot/) | Avanzado |
| 12.05 | [Multi-Cluster EKS Cluster API](12-containers-orchestration/12.05-multi-cluster-eks-cluster-api/) | Experto |
| 12.06 | [ECS FireLens Centralized Logging](12-containers-orchestration/12.06-ecs-firelens-centralized-logging/) | Intermedio |
| 12.07 | [EKS Pod Security + Network Policies](12-containers-orchestration/12.07-eks-pod-security-network-policies/) | Avanzado |
| 12.08 | [Container CI/CD CodeBuild + ECR](12-containers-orchestration/12.08-container-cicd-codebuild-ecr/) | Intermedio |
| 12.09 | [Kubernetes HPA + VPA Autoscaling](12-containers-orchestration/12.09-kubernetes-hpa-vpa-autoscaling/) | Avanzado |
| 12.10 | [ECS Anywhere Hybrid Deployments](12-containers-orchestration/12.10-ecs-anywhere-hybrid-deployments/) | Avanzado |

---

## Parte 2: Terraform Avanzado

### 13 - Terraform State Management

| # | Challenge | Complejidad |
|---|-----------|-------------|
| 13.01 | [S3 Backend Native State Locking](13-terraform-state-management/13.01-s3-backend-native-state-locking/) | Intermedio |
| 13.02 | [State Migration Between Backends](13-terraform-state-management/13.02-state-migration-between-backends/) | Avanzado |
| 13.03 | [Cross-Account State Sharing Data Sources](13-terraform-state-management/13.03-cross-account-state-sharing-data-sources/) | Avanzado |
| 13.04 | [Import Existing Resources Terraform](13-terraform-state-management/13.04-import-existing-resources-terraform/) | Avanzado |
| 13.05 | [State Surgery and Recovery](13-terraform-state-management/13.05-state-surgery-and-recovery/) | Avanzado |
| 13.06 | [Workspace Strategy Multiple Environments](13-terraform-state-management/13.06-workspace-strategy-multiple-environments/) | Intermedio |
| 13.07 | [Multi-Region State Architecture](13-terraform-state-management/13.07-multi-region-state-architecture/) | Avanzado |
| 13.08 | [State Audit Compliance Pipeline](13-terraform-state-management/13.08-state-audit-compliance-pipeline/) | Avanzado |
| 13.09 | [Terraform State Disaster Recovery](13-terraform-state-management/13.09-terraform-state-disaster-recovery/) | Avanzado |
| 13.10 | [Ephemeral Workspaces Feature Branches](13-terraform-state-management/13.10-ephemeral-workspaces-feature-branches/) | Avanzado |

### 14 - Module Design Patterns

| # | Challenge | Complejidad |
|---|-----------|-------------|
| 14.01 | [Composable VPC Module Flexible Subnets](14-module-design-patterns/14.01-composable-vpc-module-flexible-subnets/) | Intermedio |
| 14.02 | [Module Versioning Pipeline Semantic Release](14-module-design-patterns/14.02-module-versioning-pipeline-semantic-release/) | Avanzado |
| 14.03 | [Testing Modules Terratest](14-module-design-patterns/14.03-testing-modules-terratest/) | Avanzado |
| 14.04 | [Native terraform test Framework](14-module-design-patterns/14.04-native-terraform-test-framework/) | Intermedio |
| 14.05 | [Resource Factory Pattern YAML Config](14-module-design-patterns/14.05-resource-factory-pattern-yaml-config/) | Avanzado |
| 14.06 | [Composition Pattern Stacking Modules](14-module-design-patterns/14.06-composition-pattern-stacking-modules/) | Avanzado |
| 14.07 | [Custom Validation Rules Module Inputs](14-module-design-patterns/14.07-custom-validation-rules-module-inputs/) | Intermedio |
| 14.08 | [Multi-Cloud Abstraction Module](14-module-design-patterns/14.08-multi-cloud-abstraction-module/) | Avanzado |
| 14.09 | [Nested Module Hierarchies](14-module-design-patterns/14.09-nested-module-hierarchies/) | Avanzado |
| 14.10 | [Module Migration Major Version Upgrades](14-module-design-patterns/14.10-module-migration-major-version-upgrades/) | Avanzado |

### 15 - Advanced HCL

| # | Challenge | Complejidad |
|---|-----------|-------------|
| 15.01 | [Dynamic Security Groups Complex Rules](15-advanced-hcl/15.01-dynamic-security-groups-complex-rules/) | Intermedio |
| 15.02 | [Complex for_each Maps of Objects](15-advanced-hcl/15.02-complex-for-each-maps-objects/) | Avanzado |
| 15.03 | [Conditional Resource Creation Patterns](15-advanced-hcl/15.03-conditional-resource-creation-patterns/) | Intermedio |
| 15.04 | [local-exec and remote-exec Provisioners](15-advanced-hcl/15.04-local-exec-remote-exec-provisioners/) | Intermedio |
| 15.05 | [Advanced Function Mastery](15-advanced-hcl/15.05-advanced-function-mastery/) | Intermedio |
| 15.06 | [Type Constraints optional() + Defaults](15-advanced-hcl/15.06-type-constraints-optional-defaults/) | Avanzado |
| 15.07 | [Multi-Region Multi-Account Provider Config](15-advanced-hcl/15.07-multi-region-multi-account-provider-config/) | Avanzado |
| 15.08 | [Generating JSON/YAML from HCL](15-advanced-hcl/15.08-generating-json-yaml-from-hcl/) | Intermedio |
| 15.09 | [Meta-Arguments Deep Dive](15-advanced-hcl/15.09-meta-arguments-deep-dive/) | Intermedio |
| 15.10 | [Data Source Patterns + External Data](15-advanced-hcl/15.10-data-source-patterns-external-data/) | Intermedio |

### 16 - Multi-Environment

| # | Challenge | Complejidad |
|---|-----------|-------------|
| 16.01 | [Workspaces vs Directory Structure Analysis](16-multi-environment/16.01-workspaces-vs-directory-structure-analysis/) | Intermedio |
| 16.02 | [Environment Promotion Pipeline](16-multi-environment/16.02-environment-promotion-pipeline/) | Avanzado |
| 16.03 | [Terragrunt Multi-Environment DRY Config](16-multi-environment/16.03-terragrunt-multi-environment-dry-config/) | Avanzado |
| 16.04 | [Feature-Branch Environments](16-multi-environment/16.04-feature-branch-environments/) | Avanzado |
| 16.05 | [Environment Parity Validation](16-multi-environment/16.05-environment-parity-validation/) | Avanzado |
| 16.06 | [Blue-Green Infrastructure Deployments](16-multi-environment/16.06-blue-green-infrastructure-deployments/) | Avanzado |
| 16.07 | [Module Version Promotion Across Environments](16-multi-environment/16.07-module-version-promotion-across-environments/) | Avanzado |
| 16.08 | [Secrets Management Across Environments](16-multi-environment/16.08-secrets-management-across-environments/) | Avanzado |
| 16.09 | [Cost-Differentiated Environments](16-multi-environment/16.09-cost-differentiated-environments/) | Intermedio |
| 16.10 | [Multi-Region Multi-Environment Matrix](16-multi-environment/16.10-multi-region-multi-environment-matrix/) | Experto |

### 17 - CI/CD para Terraform

| # | Challenge | Complejidad |
|---|-----------|-------------|
| 17.01 | [GitHub Actions Pipeline OIDC Auth](17-cicd-terraform/17.01-github-actions-pipeline-oidc-auth/) | Intermedio |
| 17.02 | [Atlantis on ECS Team Collaboration](17-cicd-terraform/17.02-atlantis-ecs-team-collaboration/) | Avanzado |
| 17.03 | [HCP Terraform Sentinel Policies](17-cicd-terraform/17.03-hcp-terraform-sentinel-policies/) | Avanzado |
| 17.04 | [Pre-Commit Hooks Terraform](17-cicd-terraform/17.04-pre-commit-hooks-terraform/) | Intermedio |
| 17.05 | [Multi-Stage Approval Pipeline](17-cicd-terraform/17.05-multi-stage-approval-pipeline/) | Avanzado |
| 17.06 | [Automated Drift Detection Pipeline](17-cicd-terraform/17.06-automated-drift-detection-pipeline/) | Avanzado |
| 17.07 | [Matrix Strategy Multi-Account Deployment](17-cicd-terraform/17.07-matrix-strategy-multi-account-deployment/) | Avanzado |
| 17.08 | [Dynamic Workspace CI/CD](17-cicd-terraform/17.08-dynamic-workspace-cicd/) | Avanzado |
| 17.09 | [Rollback Pipeline Failed Applies](17-cicd-terraform/17.09-rollback-pipeline-failed-applies/) | Experto |
| 17.10 | [Monorepo Change Detection](17-cicd-terraform/17.10-monorepo-change-detection/) | Avanzado |

### 18 - Seguridad en Terraform

| # | Challenge | Complejidad |
|---|-----------|-------------|
| 18.01 | [OIDC Provider Keyless CI/CD](18-seguridad-terraform/18.01-oidc-provider-keyless-cicd/) | Intermedio |
| 18.02 | [tfsec/Trivy Scanning + Remediation](18-seguridad-terraform/18.02-tfsec-trivy-scanning-remediation/) | Intermedio |
| 18.03 | [Checkov CIS Compliance Scanning](18-seguridad-terraform/18.03-checkov-cis-compliance-scanning/) | Avanzado |
| 18.04 | [Sentinel Policy Suite Terraform Cloud](18-seguridad-terraform/18.04-sentinel-policy-suite-terraform-cloud/) | Avanzado |
| 18.05 | [Least-Privilege IAM Terraform Execution](18-seguridad-terraform/18.05-least-privilege-iam-terraform-execution/) | Avanzado |
| 18.06 | [Secrets-Free Terraform Configurations](18-seguridad-terraform/18.06-secrets-free-terraform-configurations/) | Avanzado |
| 18.07 | [Defense-in-Depth Network Security Module](18-seguridad-terraform/18.07-defense-in-depth-network-security-module/) | Avanzado |
| 18.08 | [OPA/Rego Policy Pipeline](18-seguridad-terraform/18.08-opa-rego-policy-pipeline/) | Avanzado |
| 18.09 | [AWS Config Continuous Compliance via Terraform](18-seguridad-terraform/18.09-aws-config-continuous-compliance-terraform/) | Avanzado |
| 18.10 | [Module Supply Chain Security](18-seguridad-terraform/18.10-module-supply-chain-security/) | Avanzado |

### 19 - Arquitecturas Complejas con Terraform

| # | Challenge | Complejidad |
|---|-----------|-------------|
| 19.01 | [Production 3-Tier Web Application](19-arquitecturas-complejas-terraform/19.01-production-3-tier-web-application/) | Avanzado |
| 19.02 | [Serverless Microservices Platform](19-arquitecturas-complejas-terraform/19.02-serverless-microservices-platform/) | Avanzado |
| 19.03 | [Streaming + Batch Data Pipeline](19-arquitecturas-complejas-terraform/19.03-streaming-batch-data-pipeline/) | Avanzado |
| 19.04 | [Multi-Region Active-Active Architecture](19-arquitecturas-complejas-terraform/19.04-multi-region-active-active-architecture/) | Experto |
| 19.05 | [AWS Landing Zone Account Factory](19-arquitecturas-complejas-terraform/19.05-aws-landing-zone-account-factory/) | Experto |
| 19.06 | [EKS Platform Service Mesh](19-arquitecturas-complejas-terraform/19.06-eks-platform-service-mesh/) | Experto |
| 19.07 | [Event-Driven Architecture](19-arquitecturas-complejas-terraform/19.07-event-driven-architecture/) | Avanzado |
| 19.08 | [CI/CD Platform Infrastructure](19-arquitecturas-complejas-terraform/19.08-cicd-platform-infrastructure/) | Avanzado |
| 19.09 | [Compliance + Governance Platform](19-arquitecturas-complejas-terraform/19.09-compliance-governance-platform/) | Experto |
| 19.10 | [Disaster Recovery Architecture](19-arquitecturas-complejas-terraform/19.10-disaster-recovery-architecture/) | Experto |

### 20 - Testing de Infraestructura

| # | Challenge | Complejidad |
|---|-----------|-------------|
| 20.01 | [Unit Testing terraform test](20-testing-infraestructura/20.01-unit-testing-terraform-test/) | Intermedio |
| 20.02 | [Integration Testing Real Resources](20-testing-infraestructura/20.02-integration-testing-real-resources/) | Avanzado |
| 20.03 | [Terratest Complex Infrastructure](20-testing-infraestructura/20.03-terratest-complex-infrastructure/) | Avanzado |
| 20.04 | [Mock Provider Testing](20-testing-infraestructura/20.04-mock-provider-testing/) | Intermedio |
| 20.05 | [OPA Policy Testing](20-testing-infraestructura/20.05-opa-policy-testing/) | Avanzado |
| 20.06 | [Contract Testing Between Modules](20-testing-infraestructura/20.06-contract-testing-between-modules/) | Avanzado |
| 20.07 | [Chaos Engineering Infrastructure](20-testing-infraestructura/20.07-chaos-engineering-infrastructure/) | Avanzado |
| 20.08 | [Performance Benchmarking Terraform Plans](20-testing-infraestructura/20.08-performance-benchmarking-terraform-plans/) | Intermedio |
| 20.09 | [Post-Deploy Application Testing](20-testing-infraestructura/20.09-post-deploy-application-testing/) | Intermedio |
| 20.10 | [Regression Testing Module Upgrades](20-testing-infraestructura/20.10-regression-testing-module-upgrades/) | Avanzado |

### 21 - Drift Detection

| # | Challenge | Complejidad |
|---|-----------|-------------|
| 21.01 | [Scheduled Drift Detection Pipeline](21-drift-detection/21.01-scheduled-drift-detection-pipeline/) | Intermedio |
| 21.02 | [Manual Drift Reconciliation Strategies](21-drift-detection/21.02-manual-drift-reconciliation-strategies/) | Intermedio |
| 21.03 | [CloudTrail-Based Drift Attribution](21-drift-detection/21.03-cloudtrail-based-drift-attribution/) | Avanzado |
| 21.04 | [Automated Drift Remediation Risk Classification](21-drift-detection/21.04-automated-drift-remediation-risk-classification/) | Avanzado |
| 21.05 | [Multi-State Drift Dashboard](21-drift-detection/21.05-multi-state-drift-dashboard/) | Avanzado |
| 21.06 | [Drift Prevention AWS Config](21-drift-detection/21.06-drift-prevention-aws-config/) | Avanzado |
| 21.07 | [State vs Reality Comparison Tool](21-drift-detection/21.07-state-vs-reality-comparison-tool/) | Avanzado |
| 21.08 | [Import-Based Drift Resolution](21-drift-detection/21.08-import-based-drift-resolution/) | Intermedio |

### 22 - Cost Management con Terraform

| # | Challenge | Complejidad |
|---|-----------|-------------|
| 22.01 | [Infracost CI Integration](22-cost-management-terraform/22.01-infracost-ci-integration/) | Intermedio |
| 22.02 | [Comprehensive Tagging default_tags](22-cost-management-terraform/22.02-comprehensive-tagging-default-tags/) | Intermedio |
| 22.03 | [Right-Sizing Automation](22-cost-management-terraform/22.03-right-sizing-automation/) | Avanzado |
| 22.04 | [Scheduled Scaling Terraform](22-cost-management-terraform/22.04-scheduled-scaling-terraform/) | Intermedio |
| 22.05 | [Spot Instance Integration Terraform](22-cost-management-terraform/22.05-spot-instance-integration-terraform/) | Avanzado |
| 22.06 | [Resource Lifecycle Policies Terraform](22-cost-management-terraform/22.06-resource-lifecycle-policies-terraform/) | Intermedio |
| 22.07 | [Cost Anomaly Detection Budget Enforcement](22-cost-management-terraform/22.07-cost-anomaly-detection-budget-enforcement/) | Avanzado |
| 22.08 | [Multi-Account CUR Aggregation](22-cost-management-terraform/22.08-multi-account-cur-aggregation/) | Avanzado |

---

## Estadisticas

| Nivel | Cantidad |
|-------|----------|
| Intermedio | 72 |
| Avanzado | 118 |
| Experto | 26 |
| **Total** | **216** |

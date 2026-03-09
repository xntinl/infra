# 99. ECS vs EKS Decision Framework

<!--
difficulty: basic
concepts: [ecs, eks, kubernetes, fargate, ec2-launch-type, container-orchestration, control-plane, task-definition, pod, multi-cloud, portability, complexity]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: understand
prerequisites: [98-lake-formation-access-control]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates only IAM roles, ECR repositories, and ECS cluster definitions (no running tasks). EKS control plane would cost $0.10/hr and is NOT created in this exercise. Total cost ~$0.01/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Understanding of container concepts (Docker images, containers, registries)
- Familiarity with basic networking (VPC, subnets, security groups)
- No prior Kubernetes experience required (this exercise explains the relevant differences)

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** the fundamental architectural differences between ECS (AWS-native orchestration) and EKS (managed Kubernetes)
- **Describe** the launch type options: Fargate (serverless) versus EC2 (self-managed instances) for both ECS and EKS
- **Identify** the cost implications of each option, particularly the EKS control plane cost ($0.10/hr = $73/month)
- **Construct** an ECS cluster with task definitions and service configurations using Terraform
- **Distinguish** when ECS is the better choice (simpler workloads, AWS-native) versus when EKS is justified (multi-cloud, Kubernetes ecosystem, team expertise)
- **Compare** the operational overhead, learning curve, and ecosystem advantages of each platform

## Why This Matters

The ECS vs EKS decision is one of the most consequential container architecture choices on AWS, and the SAA-C03 exam tests it from the architect's perspective: which platform fits the customer's requirements, constraints, and team capabilities? The exam does not ask you to configure Kubernetes YAML -- it asks you to justify the choice.

ECS is AWS-native, simpler, and tightly integrated with AWS services. There is no additional control plane cost. Task definitions are straightforward JSON/HCL, and the ECS API maps directly to AWS concepts (services, tasks, task definitions). For teams that are AWS-committed and want the simplest container platform, ECS is the clear winner.

EKS is managed Kubernetes, which adds a $0.10/hr ($73/month) control plane cost on top of compute costs. The value proposition is portability and ecosystem: Kubernetes runs on every cloud and on-premises, so workloads are portable. The Kubernetes ecosystem provides tools for service mesh (Istio), GitOps (ArgoCD), observability (Prometheus/Grafana), and progressive delivery (Argo Rollouts) that have no direct ECS equivalents. For teams with Kubernetes expertise or multi-cloud requirements, EKS is justified.

The exam trap is choosing EKS "because it's Kubernetes" for a simple workload that runs entirely on AWS. The added complexity, cost, and learning curve of Kubernetes provide no value if you are running a handful of containers on a single cloud. The architect's job is to match the platform to the actual requirements, not the technology hype.

## Step 1 -- ECS Cluster and Task Definition

Create the following files in your exercise directory:

### `providers.tf`

```hcl
terraform {
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 5.0" }
  }
}

provider "aws" { region = var.region }
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
  default     = "saa-ex99"
}
```

### `iam.tf`

```hcl
# Task Execution Role: pulls images, sends logs (NOT the runtime role)
data "aws_iam_policy_document" "ecs_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals { type = "Service", identifiers = ["ecs-tasks.amazonaws.com"] }
  }
}

resource "aws_iam_role" "task_execution" {
  name               = "${var.project_name}-task-execution"
  assume_role_policy = data.aws_iam_policy_document.ecs_assume.json
}

resource "aws_iam_role_policy_attachment" "task_execution" {
  role       = aws_iam_role.task_execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

# Task Role: the IAM role the container assumes at runtime (S3, DynamoDB, etc.)
resource "aws_iam_role" "task_role" {
  name               = "${var.project_name}-task-role"
  assume_role_policy = data.aws_iam_policy_document.ecs_assume.json
}
```

### `containers.tf`

```hcl
# ECS cluster: no cost for the cluster itself -- you pay for compute only
resource "aws_ecs_cluster" "this" {
  name = "${var.project_name}-cluster"
  setting { name = "containerInsights", value = "enabled" }
  tags = { Name = "${var.project_name}-cluster" }
}

# Task Definition = what to run; Service = how to run it (replicas, ALB, scaling)
resource "aws_ecs_task_definition" "api" {
  family                   = "${var.project_name}-api"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = "256"   # 0.25 vCPU
  memory                   = "512"   # 512 MB
  execution_role_arn       = aws_iam_role.task_execution.arn
  task_role_arn            = aws_iam_role.task_role.arn

  container_definitions = jsonencode([
    {
      name      = "api"
      image     = "public.ecr.aws/nginx/nginx:alpine"
      essential = true

      portMappings = [
        {
          containerPort = 80
          protocol      = "tcp"
        }
      ]

      logConfiguration = {
        logDriver = "awslogs"
        options = {
          "awslogs-group"         = "/ecs/${var.project_name}-api"
          "awslogs-region"        = var.region
          "awslogs-stream-prefix" = "ecs"
          "awslogs-create-group"  = "true"
        }
      }

      healthCheck = {
        command     = ["CMD-SHELL", "curl -f http://localhost/ || exit 1"]
        interval    = 30
        timeout     = 5
        retries     = 3
        startPeriod = 60
      }
    }
  ])

  tags = { Name = "${var.project_name}-api" }
}
```

### `outputs.tf`

```hcl
output "cluster_name" {
  value = aws_ecs_cluster.this.name
}

output "task_definition_arn" {
  value = aws_ecs_task_definition.api.arn
}
```

```bash
terraform init
terraform apply -auto-approve
```

## Step 2 -- ECS vs EKS Comparison

### Comparison Table

| Feature | ECS | EKS |
|---|---|---|
| **Control plane cost** | Free | $0.10/hr ($73/month) per cluster |
| **Learning curve** | Low (AWS concepts) | High (Kubernetes concepts) |
| **Configuration** | Task definitions (JSON/HCL) | Kubernetes manifests (YAML) |
| **Portability** | AWS only | Any cloud, on-premises |
| **Service mesh** | App Mesh (limited) | Istio, Linkerd (rich ecosystem) |
| **GitOps** | CodeDeploy | ArgoCD, FluxCD |
| **Observability** | CloudWatch | Prometheus + Grafana |
| **Fargate / EC2** | Both supported | Both supported |
| **Helm charts** | N/A | Thousands of community charts |

### Cost Comparison (Example: 10 Tasks/Pods, Fargate)

```
# Workload: 10 containers, each 0.5 vCPU + 1 GB RAM
# Running 24/7 in us-east-1

# ECS Fargate
# Per task: 0.5 vCPU x $0.04048/hr + 1 GB x $0.004445/hr = $0.02469/hr
# 10 tasks: $0.2469/hr = $178/month
# Control plane: $0
# Total: $178/month

# EKS Fargate
# Per pod: same compute cost = $0.2469/hr = $178/month
# Control plane: $0.10/hr = $73/month
# Total: $251/month ($73 more than ECS)

# EKS with EC2 (managed node groups)
# 2x t3.large (2 vCPU, 8 GB): $0.0832/hr x 2 = $0.1664/hr = $120/month
# Control plane: $73/month
# Total: $193/month (less compute, more mgmt)
```

## Step 3 -- Decision Framework

### When to Choose ECS

| Factor | ECS Wins When |
|---|---|
| **Team expertise** | Team knows AWS but not Kubernetes |
| **Workload complexity** | Simple services (web APIs, workers, scheduled tasks) |
| **Cloud strategy** | AWS-committed, no multi-cloud requirement |
| **Operational overhead** | Want minimal operational burden |
| **Cost sensitivity** | Cannot justify $73/month control plane per cluster |
| **Integration** | Deep integration with AWS services is priority |
| **Time to production** | Need fastest path to running containers |

### When to Choose EKS

| Factor | EKS Wins When |
|---|---|
| **Team expertise** | Team has Kubernetes experience |
| **Multi-cloud** | Workloads must be portable across clouds |
| **Ecosystem** | Need Istio, ArgoCD, Prometheus, Helm charts |
| **Complexity** | Complex microservices with custom controllers |
| **Industry standard** | Organization standardized on Kubernetes |
| **Vendor independence** | Avoiding AWS lock-in is a strategic priority |
| **Existing investment** | Already have Kubernetes infrastructure/tooling |

### The Decision Flowchart

```
Does the team have Kubernetes expertise?
+-- No --> ECS (avoid unnecessary learning curve)
+-- Yes --> Is multi-cloud or portability required?
          +-- Yes --> EKS
          +-- No --> Is the Kubernetes ecosystem needed?
                    (Istio, ArgoCD, custom operators)
                    +-- Yes --> EKS
                    +-- No --> How complex is the workload?
                              +-- Simple services --> ECS
                              +-- Complex microservices --> Either works,
                                  use team preference
```

## Step 4 -- Explore the ECS Configuration

```bash
CLUSTER=$(terraform output -raw cluster_name)
TASK_DEF=$(terraform output -raw task_definition_arn)

# Describe the ECS cluster
aws ecs describe-clusters --clusters "$CLUSTER" \
  --query 'clusters[0].{Name:clusterName,Status:status,Insights:settings[0].value}' \
  --output table

# Describe the task definition
aws ecs describe-task-definition \
  --task-definition "$TASK_DEF" \
  --query 'taskDefinition.{Family:family,CPU:cpu,Memory:memory,NetworkMode:networkMode,Compatibility:requiresCompatibilities[0]}' \
  --output table
```

### Fargate CPU/Memory Valid Combinations

| CPU (vCPU) | Memory Options (GB) |
|---|---|
| 0.25 | 0.5, 1, 2 |
| 0.5 | 1, 2, 3, 4 |
| 1 | 2, 3, 4, 5, 6, 7, 8 |
| 2 | 4-16 (in 1 GB increments) |
| 4 | 8-30 (in 1 GB increments) |
| 8 | 16-60 (in 4 GB increments) |
| 16 | 32-120 (in 8 GB increments) |

## Common Mistakes

### 1. Choosing EKS "just because it's Kubernetes"

**Wrong:** Adopting EKS for a simple web API. The team spends weeks on Kubernetes concepts, ALB Ingress Controller, Prometheus setup -- for a workload ECS handles with a task definition and an ALB target group. Ongoing cost: $73/month control plane per cluster, mandatory upgrades every 12-14 months, additional tooling (kubectl, Helm). **Fix:** Start with ECS. Migrate to EKS only when multi-cloud portability, Kubernetes ecosystem, or team expertise justifies the complexity.

### 2. Confusing task execution role with task role

**Wrong:** Putting application S3 permissions on the execution role and omitting `task_role_arn`. The execution role is used by the ECS agent (image pull, logging) -- not by the running container. Without a task role, the container has zero AWS permissions. **Fix:** Execution role = ECR + CloudWatch. Task role = application permissions (S3, DynamoDB, SQS).

### 3. Not accounting for EKS upgrade cadence

**Wrong:** "Deploy EKS once and forget it." Kubernetes versions are supported ~14 months on EKS. Each upgrade requires testing workloads, updating node groups, and handling API deprecations. ECS has no equivalent upgrade requirement.

## Verify What You Learned

```bash
CLUSTER=$(terraform output -raw cluster_name)

# Verify ECS cluster exists
aws ecs describe-clusters --clusters "$CLUSTER" \
  --query 'clusters[0].status' --output text
```

Expected: `ACTIVE`

```bash
# Verify task definition is registered
aws ecs list-task-definitions \
  --family-prefix "saa-ex99" \
  --query 'taskDefinitionArns' --output json
```

Expected: One task definition ARN for `saa-ex99-api`.

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

Exercise 100 covers **ECS Fargate Task Networking**, where you will deploy actual running Fargate tasks with awsvpc networking, connect them to an ALB with IP-based target groups, and configure service auto-scaling -- applying the ECS concepts from this exercise to a working deployment.

## Summary

- **ECS** is AWS-native container orchestration with no control plane cost and lower operational complexity
- **EKS** is managed Kubernetes with a $0.10/hr ($73/month) control plane cost but provides portability and ecosystem access
- **Fargate** (serverless) works with both ECS and EKS, eliminating EC2 instance management
- **Choose ECS** for AWS-committed workloads, simple services, teams without Kubernetes expertise, and cost-sensitive environments
- **Choose EKS** for multi-cloud portability, Kubernetes ecosystem requirements, complex microservices, and teams with existing K8s skills
- **Task execution role** pulls images and sends logs; **task role** provides runtime AWS permissions to the container
- **EKS requires periodic upgrades** (~every 14 months) that ECS does not
- **Fargate CPU/memory** combinations are fixed -- invalid combinations cause task launch failures
- **Container Insights** provides monitoring for both ECS and EKS at $0.30 per task/pod/month
- **The complexity cost of Kubernetes** (learning curve, tooling, upgrades) is often underestimated in architecture decisions

## Reference

- [Amazon ECS Developer Guide](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/Welcome.html)
- [Amazon EKS User Guide](https://docs.aws.amazon.com/eks/latest/userguide/what-is-eks.html)
- [Terraform aws_ecs_cluster](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ecs_cluster)
- [Terraform aws_ecs_task_definition](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ecs_task_definition)

## Additional Resources

- [ECS vs EKS: Choosing the Right Container Service](https://aws.amazon.com/blogs/containers/amazon-ecs-vs-amazon-eks-making-sense-of-aws-container-services/) -- AWS's own comparison and decision guide
- [Fargate Pricing Calculator](https://aws.amazon.com/fargate/pricing/) -- compute pricing for Fargate on both ECS and EKS
- [EKS Kubernetes Version Support](https://docs.aws.amazon.com/eks/latest/userguide/kubernetes-versions.html) -- version lifecycle and upgrade requirements
- [ECS Task Definition Parameters](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/task_definition_parameters.html) -- complete reference for task definition configuration

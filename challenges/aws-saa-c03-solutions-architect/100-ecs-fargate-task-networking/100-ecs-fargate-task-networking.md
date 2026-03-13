# 100. ECS Fargate Task Networking

<!--
difficulty: intermediate
concepts: [ecs-fargate, awsvpc-networking, eni-per-task, task-definition, ecs-service, alb-target-group-ip, service-discovery, service-auto-scaling, container-health-check, deployment-circuit-breaker]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: apply, analyze
prerequisites: [99-ecs-vs-eks-decision-framework]
aws_cost: ~$0.05/hr
-->

> **AWS Cost Warning:** Fargate tasks: 2 x (0.25 vCPU + 0.5 GB) = ~$0.025/hr. ALB: ~$0.0225/hr. Total ~$0.05/hr. Costs accumulate as long as the service is running. Remember to run `terraform destroy` immediately when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 99 (ECS vs EKS Decision Framework) or equivalent knowledge
- Understanding of ECS task definitions and IAM roles from exercise 99
- Familiarity with ALB concepts (listeners, target groups) from exercise 02

## Learning Objectives

After completing this exercise, you will be able to:

1. **Implement** ECS Fargate tasks with awsvpc networking, where each task receives its own ENI and private IP address
2. **Analyze** the awsvpc networking model and why Fargate requires IP-based target groups (not instance-based)
3. **Evaluate** ALB integration patterns for Fargate services, including health checks and deregistration delay
4. **Apply** ECS service deployment configuration with circuit breakers for safe rollouts
5. **Design** a production-ready Fargate service architecture with proper networking, load balancing, and auto-scaling

## Why This Matters

ECS Fargate networking is a frequent SAA-C03 exam topic because it tests a fundamental architectural difference: every Fargate task gets its own ENI with a unique private IP (awsvpc network mode, the only mode Fargate supports). Unlike EC2 launch type where containers share a host's interface, Fargate tasks are network-isolated.

The critical exam implication: ALB target groups must use target type `ip`, not `instance`. With Fargate, there is no EC2 instance -- the ALB routes directly to the task's IP address. Using `instance` type causes registration failure.

Because each task has its own ENI, security groups apply per task, enabling micro-segmentation: API tasks allow port 8080 from ALB while worker tasks allow no inbound traffic. This is not possible with EC2 bridge or host network modes.

## Step 1 -- Create Network Infrastructure

Create the following files in your exercise directory:

### `providers.tf`

```hcl
terraform {
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 5.0" }
  }
}

provider "aws" { region = "us-east-1" }
```

### `main.tf`

```hcl
data "aws_vpc" "default" { default = true }

data "aws_subnets" "default" {
  filter { name = "vpc-id", values = [data.aws_vpc.default.id] }
  filter { name = "default-for-az", values = ["true"] }
}
```

### `security.tf`

```hcl
resource "aws_security_group" "alb" {
  name   = "saa-ex100-alb-sg"
  vpc_id = data.aws_vpc.default.id
  ingress { from_port = 80, to_port = 80, protocol = "tcp", cidr_blocks = ["0.0.0.0/0"] }
  egress  { from_port = 0, to_port = 0, protocol = "-1", cidr_blocks = ["0.0.0.0/0"] }
  tags = { Name = "saa-ex100-alb-sg" }
}

# Each Fargate task gets its own ENI -- security groups apply per-task
resource "aws_security_group" "tasks" {
  name   = "saa-ex100-tasks-sg"
  vpc_id = data.aws_vpc.default.id
  ingress { from_port = 80, to_port = 80, protocol = "tcp", security_groups = [aws_security_group.alb.id] }
  egress  { from_port = 0, to_port = 0, protocol = "-1", cidr_blocks = ["0.0.0.0/0"] }
  tags = { Name = "saa-ex100-tasks-sg" }
}
```

## Step 2 -- Create the ALB

### `alb.tf`

```hcl
resource "aws_lb" "this" {
  name               = "saa-ex100-alb"
  internal           = false
  load_balancer_type = "application"
  security_groups    = [aws_security_group.alb.id]
  subnets            = slice(data.aws_subnets.default.ids, 0, 2)
  tags = { Name = "saa-ex100-alb" }
}

# CRITICAL: target_type = "ip" is REQUIRED for Fargate.
# Fargate tasks have no EC2 instance ID -- ECS registers the task's
# private IP directly. Using "instance" causes registration failure.
resource "aws_lb_target_group" "this" {
  name        = "saa-ex100-targets"
  port        = 80
  protocol    = "HTTP"
  vpc_id      = data.aws_vpc.default.id
  target_type = "ip"  # REQUIRED for Fargate

  health_check {
    path = "/", healthy_threshold = 2, unhealthy_threshold = 3
    timeout = 5, interval = 30, matcher = "200"
  }

  deregistration_delay = 30
  tags = { Name = "saa-ex100-targets" }
}

resource "aws_lb_listener" "http" {
  load_balancer_arn = aws_lb.this.arn
  port = 80, protocol = "HTTP"
  default_action { type = "forward", target_group_arn = aws_lb_target_group.this.arn }
}
```

## Step 3 -- Create the ECS Cluster and Task Definition

### `iam.tf`

```hcl
data "aws_iam_policy_document" "ecs_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals { type = "Service", identifiers = ["ecs-tasks.amazonaws.com"] }
  }
}

resource "aws_iam_role" "execution" {
  name               = "saa-ex100-execution-role"
  assume_role_policy = data.aws_iam_policy_document.ecs_assume.json
}

resource "aws_iam_role_policy_attachment" "execution" {
  role       = aws_iam_role.execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

resource "aws_iam_role" "task" {
  name               = "saa-ex100-task-role"
  assume_role_policy = data.aws_iam_policy_document.ecs_assume.json
}
```

### `containers.tf`

```hcl
resource "aws_ecs_cluster" "this" {
  name = "saa-ex100-cluster"
  setting { name = "containerInsights", value = "enabled" }
  tags = { Name = "saa-ex100-cluster" }
}

resource "aws_cloudwatch_log_group" "ecs" {
  name = "/ecs/saa-ex100-api", retention_in_days = 7
}

# awsvpc mode: each task gets its own ENI + private IP (only mode Fargate supports)
# CPU/Memory must be valid combination: 256 CPU supports 512/1024/2048 MB
resource "aws_ecs_task_definition" "api" {
  family                   = "saa-ex100-api"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = "256"
  memory                   = "512"
  execution_role_arn       = aws_iam_role.execution.arn
  task_role_arn            = aws_iam_role.task.arn

  container_definitions = jsonencode([{
    name = "nginx", image = "public.ecr.aws/nginx/nginx:alpine", essential = true
    portMappings = [{ containerPort = 80, hostPort = 80, protocol = "tcp" }]
    logConfiguration = {
      logDriver = "awslogs"
      options = { "awslogs-group" = aws_cloudwatch_log_group.ecs.name, "awslogs-region" = "us-east-1", "awslogs-stream-prefix" = "nginx" }
    }
    healthCheck = { command = ["CMD-SHELL", "curl -f http://localhost/ || exit 1"], interval = 30, timeout = 5, retries = 3, startPeriod = 15 }
  }])

  tags = { Name = "saa-ex100-api" }
}
```

## Step 4 -- Create the ECS Service

### TODO 1: Create the Fargate Service

```hcl
# TODO: Create aws_ecs_service "saa-ex100-api-service"
# - cluster, task_definition, desired_count=2, launch_type="FARGATE"
# - network_configuration: subnets, security_groups=[tasks sg], assign_public_ip=true
# - load_balancer: target_group_arn, container_name="nginx", container_port=80
# - deployment_circuit_breaker: enable=true, rollback=true
# - depends_on: [aws_lb_listener.http]
```

### TODO 2: Configure Service Auto-Scaling

```hcl
# TODO: Create aws_appautoscaling_target
# - min_capacity=2, max_capacity=4, resource_id="service/{cluster}/{service}"
# - scalable_dimension="ecs:service:DesiredCount", service_namespace="ecs"

# TODO: Create aws_appautoscaling_policy "saa-ex100-cpu-scaling"
# - policy_type="TargetTrackingScaling", target_value=70.0
# - predefined_metric_type="ECSServiceAverageCPUUtilization"
# - scale_in_cooldown=300, scale_out_cooldown=60
```

<details>
<summary>containers.tf -- Solution: ECS Service and Auto-Scaling</summary>

```hcl
resource "aws_ecs_service" "api" {
  name            = "saa-ex100-api-service"
  cluster         = aws_ecs_cluster.this.id
  task_definition = aws_ecs_task_definition.api.arn
  desired_count   = 2
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = slice(data.aws_subnets.default.ids, 0, 2)
    security_groups  = [aws_security_group.tasks.id]
    assign_public_ip = true
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.this.arn
    container_name   = "nginx"
    container_port   = 80
  }

  deployment_circuit_breaker {
    enable   = true
    rollback = true
  }

  depends_on = [aws_lb_listener.http]

  tags = { Name = "saa-ex100-api-service" }
}

resource "aws_appautoscaling_target" "ecs" {
  max_capacity       = 4
  min_capacity       = 2
  resource_id        = "service/${aws_ecs_cluster.this.name}/${aws_ecs_service.api.name}"
  scalable_dimension = "ecs:service:DesiredCount"
  service_namespace  = "ecs"
}

resource "aws_appautoscaling_policy" "cpu" {
  name               = "saa-ex100-cpu-scaling"
  policy_type        = "TargetTrackingScaling"
  resource_id        = aws_appautoscaling_target.ecs.resource_id
  scalable_dimension = aws_appautoscaling_target.ecs.scalable_dimension
  service_namespace  = aws_appautoscaling_target.ecs.service_namespace

  target_tracking_scaling_policy_configuration {
    target_value = 70.0

    predefined_metric_specification {
      predefined_metric_type = "ECSServiceAverageCPUUtilization"
    }

    scale_in_cooldown  = 300
    scale_out_cooldown = 60
  }
}
```

</details>

## Step 5 -- Add Outputs and Apply

### `outputs.tf`

```hcl
output "alb_dns_name" {
  value = "http://${aws_lb.this.dns_name}"
}

output "cluster_name" {
  value = aws_ecs_cluster.this.name
}

output "service_name" {
  value = aws_ecs_service.api.name
}
```

```bash
terraform init
terraform apply -auto-approve
```

## Step 6 -- Verify the Deployment

```bash
ALB_DNS=$(terraform output -raw alb_dns_name)
CLUSTER=$(terraform output -raw cluster_name)
SERVICE=$(terraform output -raw service_name)

# Check service status (wait 1-2 min for tasks to pass health checks)
aws ecs describe-services --cluster "$CLUSTER" --services "$SERVICE" \
  --query 'services[0].{Desired:desiredCount,Running:runningCount,Status:status}' --output table

# Check target group health (tasks register by IP, not instance)
TG_ARN=$(aws elbv2 describe-target-groups --names "saa-ex100-targets" \
  --query 'TargetGroups[0].TargetGroupArn' --output text)
aws elbv2 describe-target-health --target-group-arn "$TG_ARN" \
  --query 'TargetHealthDescriptions[*].{Target:Target.Id,Health:TargetHealth.State}' --output table

# Test the ALB endpoint
curl -s "$ALB_DNS" | head -5
```

## Spot the Bug

A team deploys a Fargate service but tasks fail to register with the ALB:

```hcl
resource "aws_lb_target_group" "api" {
  name        = "api-targets"
  port        = 8080
  protocol    = "HTTP"
  vpc_id      = data.aws_vpc.default.id
  target_type = "instance"  # Bug is here

  health_check {
    path = "/health"
  }
}

resource "aws_ecs_service" "api" {
  name            = "api-service"
  cluster         = aws_ecs_cluster.this.id
  task_definition = aws_ecs_task_definition.api.arn
  desired_count   = 3
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = data.aws_subnets.private.ids
    security_groups  = [aws_security_group.tasks.id]
    assign_public_ip = false
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.api.arn
    container_name   = "api"
    container_port   = 8080
  }
}
```

The service creation fails with: `InvalidParameterException: The target group does not have an associated load balancer.` or tasks start but never become healthy in the target group.

<details>
<summary>Explain the bug</summary>

**Bug: The target group uses `target_type = "instance"` but Fargate requires `target_type = "ip"`.**

Fargate tasks do not run on EC2 instances that you own. Each task gets its own ENI with a private IP address, but there is no EC2 instance ID associated with it. When ECS tries to register the Fargate task with an `instance`-type target group, it fails because there is no instance ID to register.

**Fix:**

```hcl
resource "aws_lb_target_group" "api" {
  name        = "api-targets"
  port        = 8080
  protocol    = "HTTP"
  vpc_id      = data.aws_vpc.default.id
  target_type = "ip"  # Required for Fargate
}
```

**Why:** `instance` type registers `instance-id:host-port` (EC2 launch type). `ip` type registers `task-ip:container-port` (Fargate). No instance ID exists in Fargate.

**Secondary issue:** Private subnets with `assign_public_ip = false` need NAT Gateway or VPC endpoints (ECR API, ECR DKR, S3 gateway, CloudWatch Logs) for image pulls and logging.

</details>

## Verify What You Learned

```bash
CLUSTER=$(terraform output -raw cluster_name)
SERVICE=$(terraform output -raw service_name)

# Verify service is running with desired task count
aws ecs describe-services \
  --cluster "$CLUSTER" \
  --services "$SERVICE" \
  --query 'services[0].{Running:runningCount,Desired:desiredCount}' \
  --output table
```

Expected: `Running = 2`, `Desired = 2`.

```bash
# Verify target group uses IP type (not instance)
aws elbv2 describe-target-groups --names "saa-ex100-targets" \
  --query 'TargetGroups[0].TargetType' --output text
```

Expected: `ip`

```bash
# Verify ALB returns 200 and terraform is clean
ALB_DNS=$(terraform output -raw alb_dns_name)
curl -s -o /dev/null -w "%{http_code}" "$ALB_DNS"
terraform plan
```

Expected: `200` and `No changes. Your infrastructure matches the configuration.`

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

This is exercise 100 -- a milestone in the SAA-C03 series. You have now covered storage, monitoring, analytics, and containers. Future exercises will continue with advanced container networking, serverless containers (App Runner), and multi-account strategies.

## Summary

- **awsvpc network mode** gives each Fargate task its own ENI with a unique private IP -- the only mode Fargate supports
- **Target group type must be `ip`** for Fargate (not `instance`) because tasks have no EC2 instance ID
- **Security groups apply per task** with awsvpc, enabling micro-segmentation at the container level
- **assign_public_ip** is required in public subnets without NAT; private subnets need NAT or VPC endpoints
- **Deployment circuit breaker** stops bad deployments and auto-rolls back to the last healthy version
- **Service auto-scaling** uses Application Auto Scaling with target tracking on CPU, memory, or ALB request count
- **Task execution role** pulls images/sends logs; **task role** provides runtime AWS permissions
- **Health check alignment**: ALB and container health checks must both pass; deregistration delay should match request timeout
- **Fargate CPU/memory** combinations are fixed (e.g., 256 CPU supports 512/1024/2048 MB only)

## Reference

- [ECS Fargate Networking](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/fargate-task-networking.html)
- [ECS Service Load Balancing](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/service-load-balancing.html)
- [Terraform aws_ecs_service](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ecs_service)
- [Fargate Task Size Reference](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/task_definition_parameters.html#task_size)

## Additional Resources

- [ECS Deployment Types](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/deployment-types.html) -- rolling update, blue/green (CodeDeploy), and external controllers
- [VPC Endpoints for Fargate](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/vpc-endpoints.html) -- required endpoints for private subnet deployments
- [ECS Service Connect](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/service-connect.html) -- simplified service-to-service communication within ECS

# 45. ASG Lifecycle Hooks and Custom Actions

<!--
difficulty: advanced
concepts: [asg-lifecycle-hooks, launching-hook, terminating-hook, eventbridge-lambda, heartbeat-timeout, complete-lifecycle-action, service-discovery, connection-draining]
tools: [terraform, aws-cli]
estimated_time: 55m
bloom_level: evaluate
prerequisites: [04-auto-scaling-policies-deep-dive, 44-connection-draining-deregistration]
aws_cost: ~$0.05/hr
-->

> **AWS Cost Warning:** ASG with t3.micro instances (~$0.0104/hr each), Lambda invocations (free tier), EventBridge rules (free tier). Total ~$0.05/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercises 04 (Auto Scaling Policies) and 44 (Connection Draining)
- Understanding of Lambda function basics
- Familiarity with EventBridge event patterns

## Learning Objectives

After completing this exercise, you will be able to:

- **Evaluate** when lifecycle hooks are necessary versus relying on built-in ASG health checks and connection draining
- **Design** a lifecycle hook architecture that integrates ASG scaling events with EventBridge and Lambda for custom actions
- **Analyze** the trade-offs between CONTINUE and ABANDON default results and their impact on instance launch failures
- **Implement** launching hooks (software installation, service discovery registration) and terminating hooks (connection draining, deregistration)
- **Diagnose** heartbeat timeout issues where the timeout expires before custom actions complete

## Why This Matters

ASG lifecycle hooks are one of the most powerful but least understood features in the SAA-C03 exam scope. By default, when an ASG launches a new instance, it immediately enters the InService state and starts receiving traffic -- even if the instance has not finished installing required software, warming caches, or registering with a service discovery system. Similarly, when an instance is terminated, the ASG removes it immediately without giving your application time to drain connections, deregister from service meshes, or flush logs to durable storage.

Lifecycle hooks solve this by inserting a pause in the launch or termination process. The instance enters a Pending:Wait or Terminating:Wait state, and the ASG waits for either an explicit CompleteLifecycleAction call or a heartbeat timeout before proceeding. This is the mechanism that lets you run custom initialization scripts, register with Consul or Cloud Map, pull configuration from Parameter Store, warm application caches, or perform any action that must complete before the instance handles production traffic. The exam tests whether you understand this flow, the timeout mechanics, and common failure modes like heartbeat expiration during long-running setup scripts.

## The Challenge

You are architecting a microservices platform where each EC2 instance must complete three actions before receiving traffic: install application dependencies, register with AWS Cloud Map for service discovery, and warm a local cache from DynamoDB. On termination, each instance must deregister from Cloud Map and drain active connections. Design and implement lifecycle hooks with EventBridge-to-Lambda integration to orchestrate these custom actions.

### Requirements

1. Deploy an ASG with lifecycle hooks for both Launching and Terminating transitions
2. Configure EventBridge rules to capture lifecycle hook events and invoke Lambda functions
3. Lambda functions perform custom actions and call CompleteLifecycleAction when done
4. Set appropriate heartbeat timeouts based on expected action durations
5. Handle the CONTINUE vs ABANDON default result decision for each hook

### Architecture

```
                          ASG Lifecycle Event
                                |
                    +-----------+-----------+
                    |                       |
            Launching Hook          Terminating Hook
            (Pending:Wait)          (Terminating:Wait)
                    |                       |
                    v                       v
            EventBridge Rule        EventBridge Rule
                    |                       |
                    v                       v
            Lambda Function         Lambda Function
            - Install deps          - Deregister from
            - Register with           service discovery
              service discovery     - Drain connections
            - Warm cache            - Flush logs
                    |                       |
                    v                       v
          CompleteLifecycleAction  CompleteLifecycleAction
          (CONTINUE or ABANDON)   (CONTINUE or ABANDON)
                    |                       |
                    v                       v
              InService              Terminated
```

## Hints

<details>
<summary>Hint 1: Lifecycle Hook Configuration</summary>

A lifecycle hook intercepts the ASG at a specific transition point. The two transition points are:

- `autoscaling:EC2_INSTANCE_LAUNCHING` -- instance is launching, enters `Pending:Wait`
- `autoscaling:EC2_INSTANCE_TERMINATING` -- instance is terminating, enters `Terminating:Wait`

Key parameters:

```hcl
resource "aws_autoscaling_lifecycle_hook" "launching" {
  name                   = "launching-hook"
  autoscaling_group_name = aws_autoscaling_group.this.name
  lifecycle_transition   = "autoscaling:EC2_INSTANCE_LAUNCHING"
  heartbeat_timeout      = 300   # seconds to wait for CompleteLifecycleAction
  default_result         = "ABANDON"  # what happens if timeout expires
}
```

- `heartbeat_timeout`: the ASG waits this many seconds for a CompleteLifecycleAction call. If no call arrives, the `default_result` applies. Range: 30-7200 seconds (default 3600).
- `default_result`: `CONTINUE` proceeds as if the action succeeded. `ABANDON` for launching hooks terminates the instance. For terminating hooks, both CONTINUE and ABANDON terminate the instance, but ABANDON skips any remaining lifecycle hooks.

</details>

<details>
<summary>Hint 2: EventBridge Event Pattern</summary>

ASG lifecycle hooks emit events to EventBridge automatically. The event pattern to match:

```json
{
  "source": ["aws.autoscaling"],
  "detail-type": ["EC2 Instance-launch Lifecycle Action", "EC2 Instance-terminate Lifecycle Action"],
  "detail": {
    "AutoScalingGroupName": ["your-asg-name"]
  }
}
```

The event detail contains everything your Lambda needs:

```json
{
  "LifecycleActionToken": "token-for-completing-action",
  "AutoScalingGroupName": "my-asg",
  "LifecycleHookName": "launching-hook",
  "EC2InstanceId": "i-0abc123def456",
  "LifecycleTransition": "autoscaling:EC2_INSTANCE_LAUNCHING"
}
```

Your Lambda extracts these fields and passes them to `complete_lifecycle_action()`.

</details>

<details>
<summary>Hint 3: Lambda CompleteLifecycleAction Call</summary>

The Lambda function must call CompleteLifecycleAction to release the instance from the Wait state:

```go
package main

import (
	"context"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
)

func handler(ctx context.Context, event map[string]interface{}) error {
	detail := event["detail"].(map[string]interface{})

	cfg, _ := config.LoadDefaultConfig(ctx)
	asgClient := autoscaling.NewFromConfig(cfg)

	hookName := detail["LifecycleHookName"].(string)
	asgName := detail["AutoScalingGroupName"].(string)
	token := detail["LifecycleActionToken"].(string)
	result := "CONTINUE" // or "ABANDON" on failure

	asgClient.CompleteLifecycleAction(ctx, &autoscaling.CompleteLifecycleActionInput{
		LifecycleHookName:     &hookName,
		AutoScalingGroupName:  &asgName,
		LifecycleActionToken:  &token,
		LifecycleActionResult: &result,
	})
	return nil
}

func main() { lambda.Start(handler) }
```

If your custom action fails, you can send `ABANDON` to prevent a broken instance from entering InService.

</details>

<details>
<summary>Hint 4: Heartbeat Timeout Sizing</summary>

The heartbeat timeout must be longer than the longest expected custom action duration. If your setup script takes 4 minutes on average:

- Set `heartbeat_timeout = 600` (10 minutes) to allow for retries and slow starts
- If the timeout expires, the `default_result` fires automatically
- You can extend the timeout by calling `record_lifecycle_action_heartbeat()` from your Lambda

If you need more than the 7200-second maximum, call `record_lifecycle_action_heartbeat()` periodically to reset the timer:

```go
asgClient.RecordLifecycleActionHeartbeat(ctx, &autoscaling.RecordLifecycleActionHeartbeatInput{
    LifecycleHookName:    &hookName,
    AutoScalingGroupName: &asgName,
    LifecycleActionToken: &token,
})
```

</details>

## Spot the Bug

The following lifecycle hook configuration has a critical timing flaw. Identify the problem before expanding the answer.

```hcl
resource "aws_autoscaling_lifecycle_hook" "launching" {
  name                   = "app-init-hook"
  autoscaling_group_name = aws_autoscaling_group.this.name
  lifecycle_transition   = "autoscaling:EC2_INSTANCE_LAUNCHING"
  heartbeat_timeout      = 120
  default_result         = "CONTINUE"
}
```

The Lambda function triggered by this hook runs an SSM command on the instance to install software:

```go
func handler(ctx context.Context, event map[string]interface{}) error {
    detail := event["detail"].(map[string]interface{})
    instanceID := detail["EC2InstanceId"].(string)

    cfg, _ := config.LoadDefaultConfig(ctx)
    ssmClient := ssm.NewFromConfig(cfg)

    docName := "AWS-RunShellScript"
    resp, _ := ssmClient.SendCommand(ctx, &ssm.SendCommandInput{
        InstanceIds:  []string{instanceID},
        DocumentName: &docName,
        Parameters: map[string][]string{
            "commands": {
                "yum install -y java-17-amazon-corretto nginx",
                "aws s3 cp s3://config-bucket/app.jar /opt/app/",
                "aws s3 cp s3://config-bucket/nginx.conf /etc/nginx/",
                "systemctl enable nginx && systemctl start nginx",
                "java -jar /opt/app/app.jar --warmup",
            },
        },
    })

    // Immediately complete the lifecycle action
    asgClient := autoscaling.NewFromConfig(cfg)
    hookName := detail["LifecycleHookName"].(string)
    asgName := detail["AutoScalingGroupName"].(string)
    token := detail["LifecycleActionToken"].(string)
    result := "CONTINUE"

    asgClient.CompleteLifecycleAction(ctx, &autoscaling.CompleteLifecycleActionInput{
        LifecycleHookName:     &hookName,
        AutoScalingGroupName:  &asgName,
        LifecycleActionToken:  &token,
        LifecycleActionResult: &result,
    })
    _ = resp
    return nil
}
```

<details>
<summary>Explain the bug</summary>

There are two bugs:

**Bug 1: CompleteLifecycleAction is called immediately, without waiting for SSM command completion.** The `SendCommand` API is asynchronous -- it returns immediately after submitting the command, not after the command finishes executing. The Lambda calls `CompleteLifecycleAction(CONTINUE)` right away, releasing the instance into InService while `yum install`, the S3 downloads, and the Java warmup are still running. The instance starts receiving production traffic before it is ready.

**Fix:** Poll the SSM command status using `GetCommandInvocation` in a loop until it completes, then call `CompleteLifecycleAction`:

```go
commandID := *resp.Command.CommandId
for {
    inv, _ := ssmClient.GetCommandInvocation(ctx, &ssm.GetCommandInvocationInput{
        CommandId:  &commandID,
        InstanceId: &instanceID,
    })
    status := string(inv.Status)
    if status == "Success" || status == "Failed" || status == "TimedOut" || status == "Cancelled" {
        if status == "Success" {
            result = "CONTINUE"
        } else {
            result = "ABANDON"
        }
        break
    }
    time.Sleep(10 * time.Second)
}
```

**Bug 2: The heartbeat timeout (120 seconds) is almost certainly too short.** Installing Java 17, downloading application artifacts from S3, configuring nginx, and running a Java warmup will typically take 3-5 minutes. If the Lambda properly waited for the SSM command, the 120-second heartbeat would expire before the script completes. With `default_result = "CONTINUE"`, the instance enters InService in an uninitialized state.

**Fix:** Set `heartbeat_timeout = 600` (10 minutes) to accommodate installation time plus safety margin. Change `default_result = "ABANDON"` so that timeout expiration terminates the instance rather than putting an uninitialized instance into service.

</details>

## Verify What You Learned

After implementing the lifecycle hooks architecture, verify with these commands:

```bash
ASG_NAME="saa-ex45-asg"

# Verify lifecycle hooks are attached to the ASG
aws autoscaling describe-lifecycle-hooks \
  --auto-scaling-group-name "$ASG_NAME" \
  --query 'LifecycleHooks[*].{Name:LifecycleHookName,Transition:LifecycleTransition,Timeout:HeartbeatTimeout,Default:DefaultResult}' \
  --output table
```

Expected: Two hooks -- one for `autoscaling:EC2_INSTANCE_LAUNCHING` and one for `autoscaling:EC2_INSTANCE_TERMINATING`.

```bash
# Verify EventBridge rules exist
aws events list-rules \
  --name-prefix "saa-ex45" \
  --query 'Rules[*].{Name:Name,State:State}' \
  --output table
```

Expected: Rules in `ENABLED` state targeting the lifecycle hook events.

```bash
# Trigger a scale-out event and observe lifecycle activity
aws autoscaling set-desired-capacity \
  --auto-scaling-group-name "$ASG_NAME" \
  --desired-capacity 3

# Watch for Pending:Wait state
aws autoscaling describe-auto-scaling-instances \
  --query 'AutoScalingInstances[?AutoScalingGroupName==`saa-ex45-asg`].{Id:InstanceId,State:LifecycleState}' \
  --output table
```

Expected: One instance in `Pending:Wait` state while the Lambda executes.

```bash
# Check Lambda execution logs
aws logs tail "/aws/lambda/saa-ex45-launching-hook" --since 5m --format short
```

Expected: Log entries showing the lifecycle hook event received and CompleteLifecycleAction called.

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify no ASG remains:

```bash
aws autoscaling describe-auto-scaling-groups \
  --auto-scaling-group-names saa-ex45-asg \
  --query 'AutoScalingGroups' 2>/dev/null || echo "ASG deleted successfully"
```

## What's Next

Exercise 46 explores **predictive scaling with ML-based forecasting**, where the ASG uses historical metric data to proactively scale capacity before demand arrives. You will configure ForecastOnly vs ForecastAndScale modes and analyze how the ML model generates scaling plans -- building on the reactive scaling from exercise 04 and the lifecycle awareness from this exercise.

## Summary

- **Lifecycle hooks** pause ASG instance launches and terminations to allow custom actions before state transitions
- **Launching hooks** (`Pending:Wait`) are used for software installation, service discovery registration, cache warming, and configuration pulling
- **Terminating hooks** (`Terminating:Wait`) are used for connection draining, service deregistration, log flushing, and graceful shutdown
- **Heartbeat timeout** must exceed the longest expected action duration -- if it expires, the `default_result` fires automatically
- **ABANDON** on launching hooks terminates the instance (use when an uninitialized instance would be dangerous)
- **CONTINUE** on launching hooks puts the instance into InService regardless of action outcome (use when partial initialization is acceptable)
- **EventBridge + Lambda** is the recommended integration pattern for lifecycle hooks (replaces the older SNS/SQS notification approach)
- **record_lifecycle_action_heartbeat()** extends the timeout window for actions that may exceed the initial heartbeat period
- SSM `send_command()` is asynchronous -- you must poll for completion before calling CompleteLifecycleAction

## Reference

- [Amazon EC2 Auto Scaling Lifecycle Hooks](https://docs.aws.amazon.com/autoscaling/ec2/userguide/lifecycle-hooks.html)
- [CompleteLifecycleAction API](https://docs.aws.amazon.com/autoscaling/ec2/APIReference/API_CompleteLifecycleAction.html)
- [Terraform aws_autoscaling_lifecycle_hook](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/autoscaling_lifecycle_hook)
- [EventBridge Events for Auto Scaling](https://docs.aws.amazon.com/autoscaling/ec2/userguide/cloud-watch-events.html)

## Additional Resources

- [Tutorial: Configure a Lifecycle Hook with EventBridge](https://docs.aws.amazon.com/autoscaling/ec2/userguide/tutorial-lifecycle-hook-lambda.html) -- step-by-step walkthrough of the EventBridge + Lambda pattern
- [RecordLifecycleActionHeartbeat API](https://docs.aws.amazon.com/autoscaling/ec2/APIReference/API_RecordLifecycleActionHeartbeat.html) -- extending the heartbeat window for long-running actions
- [AWS Cloud Map Service Discovery](https://docs.aws.amazon.com/cloud-map/latest/dg/what-is-cloud-map.html) -- the AWS-native service discovery system commonly integrated with lifecycle hooks
- [ASG Instance Lifecycle States](https://docs.aws.amazon.com/autoscaling/ec2/userguide/ec2-auto-scaling-lifecycle.html) -- complete state machine diagram showing where hooks intercept

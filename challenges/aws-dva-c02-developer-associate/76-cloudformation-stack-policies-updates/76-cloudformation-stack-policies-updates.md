# 76. CloudFormation Stack Policies and Update Behaviors

<!--
difficulty: intermediate
concepts: [stack-policies, update-replace, update-no-interrupt, update-with-interrupt, update-prevention, temporary-override, resource-protection, drift-detection]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: analyze, implement
prerequisites: [11-cloudformation-intrinsic-functions-rollback]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates CloudFormation stacks with DynamoDB tables and SQS queues. All costs are negligible for testing (~$0.01/hr or less). Remember to delete your stacks when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| AWS CLI configured | `aws sts get-caller-identity` |
| jq installed | `jq --version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Analyze** CloudFormation update behaviors (Update:No Interruption, Update:Some Interruption, Update:Replace) and predict which changes trigger resource replacement
2. **Implement** a stack policy that prevents accidental updates to critical resources like RDS instances and DynamoDB tables
3. **Configure** temporary stack policy overrides to allow planned replacements while keeping the default deny policy in place
4. **Differentiate** between stack policies (prevent updates), DeletionPolicy (prevent deletion), and UpdateReplacePolicy (control replacement behavior)
5. **Debug** stack update failures caused by overly restrictive stack policies

## Why This Matters

Stack policies protect production resources from accidental changes. Without a stack policy, any `UpdateStack` call can modify or replace any resource in the stack. Replacing an RDS instance destroys the existing database and creates a new one -- losing all data. Replacing a DynamoDB table drops all items. Stack policies add a safety net by denying specific update actions on specific resources.

The DVA-C02 exam tests three concepts. First, stack policies are JSON documents that Allow or Deny specific update actions (Update:Modify, Update:Replace, Update:Delete, Update:*) on specific resources. Second, once set, a stack policy cannot be removed -- you can only replace it with a more permissive policy. Third, you can provide a **temporary override** during a specific update to bypass the stack policy for planned changes. The override applies only to that single update and reverts automatically.

A common exam trap: a stack policy that denies `Update:*` on all resources prevents ALL updates, including updates to the stack policy itself. You must use `--stack-policy-during-update-body` to temporarily override the policy before you can change anything.

## Building Blocks

### CloudFormation Template

Create a file called `template.yaml`:

```yaml
AWSTemplateFormatVersion: "2010-09-09"
Description: Stack Policies Demo - Critical Infrastructure

Parameters:
  Environment:
    Type: String
    Default: dev
    AllowedValues: [dev, staging, prod]
  TableReadCapacity:
    Type: Number
    Default: 5
  TableWriteCapacity:
    Type: Number
    Default: 5
  QueueRetentionPeriod:
    Type: Number
    Default: 345600

Resources:
  # Critical resource -- must not be replaced
  OrdersTable:
    Type: AWS::DynamoDB::Table
    DeletionPolicy: Retain
    UpdateReplacePolicy: Retain
    Properties:
      TableName: !Sub "${AWS::StackName}-orders"
      BillingMode: PROVISIONED
      ProvisionedThroughput:
        ReadCapacityUnits: !Ref TableReadCapacity
        WriteCapacityUnits: !Ref TableWriteCapacity
      AttributeDefinitions:
        - AttributeName: PK
          AttributeType: S
      KeySchema:
        - AttributeName: PK
          KeyType: HASH

  # Less critical -- can be updated
  OrderQueue:
    Type: AWS::SQS::Queue
    Properties:
      QueueName: !Sub "${AWS::StackName}-orders"
      MessageRetentionPeriod: !Ref QueueRetentionPeriod
      VisibilityTimeout: 60

  # Non-critical -- can be replaced
  ProcessingQueue:
    Type: AWS::SQS::Queue
    Properties:
      QueueName: !Sub "${AWS::StackName}-processing"
      VisibilityTimeout: 30

Outputs:
  TableName:
    Value: !Ref OrdersTable
  TableArn:
    Value: !GetAtt OrdersTable.Arn
  OrderQueueUrl:
    Value: !GetAtt OrderQueue.QueueUrl
  ProcessingQueueUrl:
    Value: !GetAtt ProcessingQueue.QueueUrl
```

### Stack Policy Skeleton

Create a file called `stack-policy.json`. Fill in the `# TODO` blocks.

```json
{
  "Statement": [
    {
      "Effect": "Allow",
      "Action": "Update:*",
      "Principal": "*",
      "Resource": "*"
    }
  ]
}
```

The above policy allows all updates (it is the implicit default). Your task is to modify it.

## TODO 1 -- Create a Restrictive Stack Policy

Create a file called `stack-policy-strict.json` that:

1. **Denies** `Update:Replace` and `Update:Delete` on the `OrdersTable` resource
2. **Denies** `Update:Replace` on the `OrderQueue` resource
3. **Allows** all other updates on all other resources

Requirements:
- Use `ResourceType` condition for the DynamoDB table: `AWS::DynamoDB::Table`
- Use `LogicalResourceId` for specific resource targeting
- Remember: Deny takes precedence over Allow

## TODO 2 -- Apply the Stack Policy

Apply the stack policy to an existing stack using the AWS CLI:

```bash
# Create the stack first
aws cloudformation create-stack \
  --stack-name stack-policy-demo \
  --template-body file://template.yaml

aws cloudformation wait stack-create-complete --stack-name stack-policy-demo

# TODO: Apply the strict stack policy
# aws cloudformation set-stack-policy \
#   --stack-name stack-policy-demo \
#   --stack-policy-body file://???
```

## TODO 3 -- Test a Blocked Update

Attempt to update the stack with a change that would replace the DynamoDB table (changing the key schema triggers replacement):

Create a file called `template-replace-key.yaml` that changes the key schema from `PK` to `OrderID`. Try to update the stack:

```bash
# This should fail because the stack policy denies Update:Replace on DynamoDB
# aws cloudformation update-stack \
#   --stack-name stack-policy-demo \
#   --template-body file://template-replace-key.yaml
```

## TODO 4 -- Temporary Override for Planned Replacement

Create a temporary override policy that allows the replacement, then apply it during the update:

```bash
# Create a temporary override policy file
# aws cloudformation update-stack \
#   --stack-name stack-policy-demo \
#   --template-body file://template-replace-key.yaml \
#   --stack-policy-during-update-body file://???
```

## Spot the Bug

A developer creates a stack policy to protect their RDS database but cannot update ANY resource in the stack afterward. The stack update fails with "Action denied by stack policy." **What is wrong?**

```json
{
  "Statement": [
    {
      "Effect": "Deny",
      "Action": "Update:*",
      "Principal": "*",
      "Resource": "LogicalResourceId/ProductionDatabase"
    }
  ]
}
```

<details>
<summary>Explain the bug</summary>

The stack policy only has a Deny statement and no Allow statement. The implicit default for stack policies is **Deny** -- if a resource is not covered by an explicit Allow statement, updates are denied.

With this policy:
- `ProductionDatabase` is explicitly denied (redundant, since the default is deny)
- **All other resources** have no explicit Allow statement, so they are implicitly denied too

The result: no resource in the stack can be updated.

The fix requires adding an explicit Allow for all resources, then denying specific actions on the protected resource. Deny takes precedence over Allow:

```json
{
  "Statement": [
    {
      "Effect": "Allow",
      "Action": "Update:*",
      "Principal": "*",
      "Resource": "*"
    },
    {
      "Effect": "Deny",
      "Action": ["Update:Replace", "Update:Delete"],
      "Principal": "*",
      "Resource": "LogicalResourceId/ProductionDatabase"
    }
  ]
}
```

This allows all updates to all resources except Replace and Delete on the database. The Allow covers everything, and the Deny overrides it for the specific resource and actions.

On the exam, the key rule is: stack policies have an **implicit deny** on all resources not explicitly mentioned, and **explicit Deny always wins** over Allow.

</details>

## Solutions

<details>
<summary>TODO 1 -- Restrictive Stack Policy</summary>

Create `stack-policy-strict.json`:

```json
{
  "Statement": [
    {
      "Effect": "Allow",
      "Action": "Update:*",
      "Principal": "*",
      "Resource": "*"
    },
    {
      "Effect": "Deny",
      "Action": ["Update:Replace", "Update:Delete"],
      "Principal": "*",
      "Resource": "LogicalResourceId/OrdersTable"
    },
    {
      "Effect": "Deny",
      "Action": "Update:Replace",
      "Principal": "*",
      "Resource": "LogicalResourceId/OrderQueue"
    }
  ]
}
```

Statement 1 allows all updates on all resources (base permission). Statement 2 denies replacement and deletion of the DynamoDB table. Statement 3 denies replacement of the order queue. Other resources (ProcessingQueue) can be freely updated, replaced, or deleted.

</details>

<details>
<summary>TODO 2 -- Apply the Stack Policy</summary>

```bash
aws cloudformation create-stack \
  --stack-name stack-policy-demo \
  --template-body file://template.yaml

aws cloudformation wait stack-create-complete --stack-name stack-policy-demo

aws cloudformation set-stack-policy \
  --stack-name stack-policy-demo \
  --stack-policy-body file://stack-policy-strict.json

# Verify the policy was applied
aws cloudformation get-stack-policy \
  --stack-name stack-policy-demo | jq -r '.StackPolicyBody' | jq .
```

</details>

<details>
<summary>TODO 3 -- Blocked Update</summary>

The update fails because changing the key schema triggers Update:Replace on the DynamoDB table, which the stack policy denies:

```bash
# This will fail with "Action denied by stack policy"
aws cloudformation update-stack \
  --stack-name stack-policy-demo \
  --template-body file://template.yaml \
  --parameters ParameterKey=TableReadCapacity,ParameterValue=5

# Safe update: changing capacity does NOT trigger replacement
aws cloudformation update-stack \
  --stack-name stack-policy-demo \
  --template-body file://template.yaml \
  --parameters ParameterKey=TableReadCapacity,ParameterValue=10

aws cloudformation wait stack-update-complete --stack-name stack-policy-demo
```

Changing `ReadCapacityUnits` is an Update:No Interruption change -- it is allowed by the stack policy. Changing the key schema would be Update:Replace -- denied.

</details>

<details>
<summary>TODO 4 -- Temporary Override</summary>

Create `override-policy.json`:

```json
{
  "Statement": [
    {
      "Effect": "Allow",
      "Action": "Update:*",
      "Principal": "*",
      "Resource": "*"
    }
  ]
}
```

Apply the override during the update:

```bash
aws cloudformation update-stack \
  --stack-name stack-policy-demo \
  --template-body file://template-replace-key.yaml \
  --stack-policy-during-update-body file://override-policy.json
```

The `--stack-policy-during-update-body` temporarily overrides the stack policy for this single update only. After the update completes (or fails), the original strict policy is restored automatically. The override does not persist.

</details>

## Verify What You Learned

```bash
# Verify the stack exists and has a policy
aws cloudformation get-stack-policy --stack-name stack-policy-demo \
  --query "StackPolicyBody" --output text | jq .
```

Expected: the strict stack policy with Allow and Deny statements.

```bash
# Verify a safe update works (capacity change, no replacement)
aws cloudformation update-stack \
  --stack-name stack-policy-demo \
  --template-body file://template.yaml \
  --parameters ParameterKey=TableReadCapacity,ParameterValue=5 \
  ParameterKey=QueueRetentionPeriod,ParameterValue=259200

aws cloudformation wait stack-update-complete --stack-name stack-policy-demo
echo "Safe update succeeded"
```

Expected: update completes successfully because capacity and retention changes do not trigger replacement.

## Cleanup

```bash
# Note: DeletionPolicy: Retain means the table survives stack deletion
aws cloudformation delete-stack --stack-name stack-policy-demo
aws cloudformation wait stack-delete-complete --stack-name stack-policy-demo

# Manually delete the retained table
aws dynamodb delete-table --table-name stack-policy-demo-orders 2>/dev/null
```

## What's Next

You protected critical resources with stack policies and temporary overrides. In the next exercise, you will explore **CodeCommit triggers and notifications** -- configuring SNS and Lambda triggers on repository events and notification rules for pull request workflows.

## Summary

- **Stack policies** are JSON documents that Allow or Deny update actions on specific resources within a CloudFormation stack
- Update actions include: `Update:Modify` (in-place change), `Update:Replace` (delete + recreate), `Update:Delete` (removal from template), and `Update:*` (all)
- Stack policies have an **implicit deny** -- resources not covered by an explicit Allow statement cannot be updated
- **Deny always wins** over Allow, just like IAM policies
- Once set, a stack policy **cannot be removed** -- only replaced with a more permissive policy
- **Temporary overrides** (`--stack-policy-during-update-body`) bypass the policy for a single update and revert automatically
- **DeletionPolicy: Retain** keeps the physical resource when the stack is deleted; **UpdateReplacePolicy: Retain** keeps the old resource when a replacement is triggered
- Common trap: a Deny-only policy (no Allow) blocks all updates on all resources, not just the targeted ones

## Reference

- [CloudFormation Stack Policies](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/protect-stack-resources.html)
- [Update Behaviors of Stack Resources](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-updating-stacks-update-behaviors.html)
- [DeletionPolicy Attribute](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-attribute-deletionpolicy.html)
- [UpdateReplacePolicy Attribute](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-attribute-updatereplacepolicy.html)

## Additional Resources

- [Preventing Updates to Stack Resources](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/protect-stack-resources.html) -- detailed examples of stack policies with conditions and resource types
- [CloudFormation Drift Detection](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-stack-drift.html) -- detecting out-of-band changes to stack resources
- [CloudFormation Change Sets](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-updating-stacks-changesets.html) -- previewing changes before applying updates
- [Resource Import](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/resource-import.html) -- importing existing resources into a stack without replacement

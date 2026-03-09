# 11. CloudFormation Intrinsic Functions, Rollback, and Drift

<!--
difficulty: advanced
concepts: [cloudformation, intrinsic-functions, fn-sub, fn-if, fn-select, fn-getatt, fn-importvalue, rollback, drift-detection, change-sets]
tools: [aws-cli, terraform]
estimated_time: 50m
bloom_level: evaluate
prerequisites: [none]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates CloudFormation stacks with a Lambda function and an S3 bucket. Cost is approximately $0.01/hr. CloudFormation itself is free -- you only pay for the resources it creates. Remember to delete the stacks when finished.

## Prerequisites

- AWS CLI configured with a sandbox account
- Basic familiarity with JSON or YAML template syntax
- No Terraform is used in this exercise -- CloudFormation is used directly because the DVA-C02 exam tests CFN-specific knowledge

## Learning Objectives

After completing this exercise, you will be able to:

- **Evaluate** when to use each CloudFormation intrinsic function (`Fn::Sub`, `Fn::If`, `Fn::Select`, `Fn::GetAtt`, `Fn::ImportValue`) based on the template requirement
- **Design** a CloudFormation template with conditional resource creation using `Conditions` and `Fn::If`
- **Implement** cross-stack references using `Outputs` with `Export` and `Fn::ImportValue`
- **Configure** a change set to preview modifications before applying them, and compare this to direct stack updates
- **Analyze** stack rollback events and drift detection results to identify why an update failed or how deployed resources diverged from the template

## Why This Matters

CloudFormation is AWS's native infrastructure-as-code service and the DVA-C02 exam dedicates significant coverage to it -- even if you use Terraform day-to-day. The exam tests intrinsic functions extensively: can you spot the difference between `Fn::Sub` (string interpolation with variable substitution) and `Fn::Join` (concatenation with a delimiter)? Do you know that `Fn::If` requires a `Conditions` block and returns one of two values based on a condition? Can you trace a `Fn::ImportValue` reference back to the exporting stack's `Outputs`?

Beyond template authoring, the exam tests operational knowledge. What happens when a stack update fails? CloudFormation rolls back to the previous known-good state, but understanding which events triggered the rollback requires reading the stack events log. Drift detection answers a different question: has someone modified a resource outside of CloudFormation (e.g., via the Console)? The exam tests whether you know what constitutes drift (property changes, deletions) and what does not (tags added by AWS services, certain computed properties).

## The Challenge

Build two CloudFormation stacks that demonstrate intrinsic functions, cross-stack references, conditional logic, rollback behavior, and drift detection. This exercise uses CloudFormation directly (not Terraform) because the exam tests CFN-specific syntax and behavior.

### Requirements

| Requirement | Description |
|---|---|
| Base Stack | Exports a Lambda function ARN and an S3 bucket name via `Outputs` with `Export` |
| App Stack | Imports the exported values using `Fn::ImportValue` |
| Conditions | `CreateAlarm` condition: create a CloudWatch alarm only when `Environment` parameter is `prod` |
| Fn::Sub | Construct resource names using `${AWS::StackName}` and `${AWS::Region}` pseudo parameters |
| Fn::If | Conditionally set Lambda reserved concurrency (10 for prod, omit for dev) |
| Fn::Select | Select a memory size from a list based on an index parameter |
| Fn::GetAtt | Reference the Lambda function ARN and S3 bucket ARN within the same template |
| Rollback | Trigger a rollback by updating the Lambda runtime to an invalid value |
| Drift | Manually modify a resource property via CLI, then detect the drift |
| Change Sets | Create a change set, review proposed changes, then execute it |

### Architecture

```
  +----------------------------------+     +----------------------------------+
  |         Base Stack               |     |          App Stack               |
  |                                  |     |                                  |
  |  S3 Bucket --------------------Export--> Fn::ImportValue(BucketName)     |
  |    Fn::Sub: ${AWS::StackName}-   |     |                                  |
  |             ${AWS::Region}-data  |     |  Lambda reads from imported      |
  |                                  |     |  bucket name via env var         |
  |  Lambda Function ---------------Export--> Fn::ImportValue(FnArn)         |
  |    Memory: Fn::Select            |     |                                  |
  |    Concurrency: Fn::If(IsProd)   |     |  CloudWatch Alarm               |
  |    Name: Fn::Sub                 |     |    Condition: CreateAlarm        |
  |    ARN: Fn::GetAtt               |     |    Fn::If: only in prod         |
  +----------------------------------+     +----------------------------------+

  Rollback test:     Update Lambda runtime to "provided.al2099" -> stack rolls back
  Drift test:        aws lambda update-function-configuration -> drift detected
  Change set test:   Create change set -> review -> execute
```

## Hints

<details>
<summary>Hint 1: Template structure with Conditions and intrinsic functions</summary>

A CloudFormation template with conditions follows this structure. The `Conditions` block defines named boolean expressions that you reference with `Fn::If` in the `Resources` block.

```yaml
AWSTemplateFormatVersion: '2010-09-09'
Description: Base stack with intrinsic functions demo

Parameters:
  Environment:
    Type: String
    AllowedValues: [dev, prod]
    Default: dev
  MemoryIndex:
    Type: Number
    Default: 1
    AllowedValues: [0, 1, 2]
    Description: "Index into memory sizes list: 0=128, 1=256, 2=512"

Conditions:
  IsProd: !Equals [!Ref Environment, prod]

Resources:
  # Use Fn::Sub for string interpolation with pseudo parameters
  DataBucket:
    Type: AWS::S3::Bucket
    Properties:
      BucketName: !Sub "${AWS::StackName}-${AWS::Region}-data"

  DemoFunction:
    Type: AWS::Lambda::Function
    Properties:
      FunctionName: !Sub "${AWS::StackName}-processor"
      Runtime: provided.al2023
      Handler: bootstrap
      # Fn::Select picks from a list by index
      MemorySize: !Select [!Ref MemoryIndex, [128, 256, 512]]
      # Fn::If returns one value if IsProd is true, another if false
      ReservedConcurrentExecutions: !If [IsProd, 10, !Ref "AWS::NoValue"]
      # ...
```

Key points:
- `!Ref "AWS::NoValue"` omits the property entirely (as if it were not in the template)
- `!Equals` is a condition function, not an intrinsic function -- it can only appear in `Conditions`
- `!Select` uses zero-based indexing

</details>

<details>
<summary>Hint 2: Cross-stack references with Outputs and ImportValue</summary>

The exporting stack defines `Outputs` with `Export.Name`. The importing stack uses `Fn::ImportValue` to reference those exports. Exported names must be unique within a region.

In the base stack:

```yaml
Outputs:
  FunctionArn:
    Description: ARN of the processor Lambda function
    Value: !GetAtt DemoFunction.Arn
    Export:
      Name: !Sub "${AWS::StackName}-FunctionArn"

  BucketName:
    Description: Name of the data bucket
    Value: !Ref DataBucket
    Export:
      Name: !Sub "${AWS::StackName}-BucketName"
```

In the app stack:

```yaml
Resources:
  AppFunction:
    Type: AWS::Lambda::Function
    Properties:
      Environment:
        Variables:
          DATA_BUCKET: !ImportValue
            Fn::Sub: "${BaseStackName}-BucketName"
          PROCESSOR_ARN: !ImportValue
            Fn::Sub: "${BaseStackName}-FunctionArn"
```

Important: you cannot delete a stack that has active exports referenced by another stack. You must delete the importing stack first.

</details>

<details>
<summary>Hint 3: Triggering and observing a rollback</summary>

To trigger a rollback, update the stack with an invalid property value that CloudFormation only validates during resource creation/update, not during template validation.

```bash
# Create a change to the base stack that will fail during update
# (invalid runtime triggers rollback, not template validation error)
aws cloudformation update-stack \
  --stack-name cfn-base-demo \
  --template-body file://base-stack-invalid.yaml \
  --capabilities CAPABILITY_NAMED_IAM

# Watch the rollback in progress
aws cloudformation describe-stack-events \
  --stack-name cfn-base-demo \
  --query "StackEvents[?ResourceStatus=='UPDATE_FAILED' || ResourceStatus=='UPDATE_ROLLBACK_COMPLETE'].[LogicalResourceId,ResourceStatus,ResourceStatusReason]" \
  --output table
```

The key insight is that CloudFormation validates template syntax immediately but validates resource properties only when it tries to create or update the resource. An invalid runtime like `provided.al2099` passes template validation but fails during the Lambda update, triggering a rollback to the previous configuration.

After rollback, the stack status is `UPDATE_ROLLBACK_COMPLETE` and the Lambda runtime is restored to its previous value.

</details>

<details>
<summary>Hint 4: Detecting drift after manual changes</summary>

Drift detection compares the actual resource configuration against the template definition. Trigger it after making a manual change:

```bash
# Step 1: Manually change the Lambda memory via CLI (outside CloudFormation)
aws lambda update-function-configuration \
  --function-name cfn-base-demo-processor \
  --memory-size 512

# Step 2: Initiate drift detection
DRIFT_ID=$(aws cloudformation detect-stack-drift \
  --stack-name cfn-base-demo \
  --query "StackDriftDetectionId" --output text)

# Step 3: Wait for detection to complete
aws cloudformation describe-stack-drift-detection-status \
  --stack-drift-detection-id "$DRIFT_ID"

# Step 4: View the drifted resources
aws cloudformation describe-stack-resource-drifts \
  --stack-name cfn-base-demo \
  --stack-resource-drift-status-filters MODIFIED \
  --query "StackResourceDrifts[*].{Resource:LogicalResourceId,Status:StackResourceDriftStatus,Differences:PropertyDifferences}" \
  --output json
```

The drift output shows the `Expected` value (from the template), the `Actual` value (from the live resource), and the `DifferenceType` (ADD, REMOVE, or NOT_EQUAL). Not all properties support drift detection -- check the [CloudFormation documentation](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-stack-drift-resource-list.html) for supported resources and properties.

</details>

<details>
<summary>Hint 5: Change sets vs direct updates</summary>

A change set lets you preview what CloudFormation will modify before executing. This is analogous to `terraform plan`.

```bash
# Create a change set (does NOT execute changes yet)
aws cloudformation create-change-set \
  --stack-name cfn-base-demo \
  --change-set-name update-memory \
  --template-body file://base-stack.yaml \
  --parameters ParameterKey=Environment,ParameterValue=dev \
               ParameterKey=MemoryIndex,ParameterValue=2 \
  --capabilities CAPABILITY_NAMED_IAM

# Review what will change
aws cloudformation describe-change-set \
  --stack-name cfn-base-demo \
  --change-set-name update-memory \
  --query "Changes[*].ResourceChange.{Action:Action,Resource:LogicalResourceId,Replacement:Replacement}" \
  --output table

# Execute the change set (applies the changes)
aws cloudformation execute-change-set \
  --stack-name cfn-base-demo \
  --change-set-name update-memory

# Or delete it if you decide not to proceed
aws cloudformation delete-change-set \
  --stack-name cfn-base-demo \
  --change-set-name update-memory
```

Key exam point: change sets show whether a resource will be **modified in place**, **replaced** (new physical ID), or **conditionally replaced**. A direct `update-stack` skips the preview and applies immediately.

</details>

## Spot the Bug

The following CloudFormation template snippet uses `Fn::Sub` to construct a bucket name, but the deployed bucket name does not include the stack name as expected:

```yaml
Resources:
  DataBucket:
    Type: AWS::S3::Bucket
    Properties:
      BucketName: !Sub "${StackName}-${AWS::Region}-data"
```

The developer expects a bucket named `my-stack-us-east-1-data` but the deployment fails with an error about an unresolved variable.

<details>
<summary>Explain the bug</summary>

The variable `${StackName}` is incorrect. CloudFormation pseudo parameters require the `AWS::` prefix. The correct pseudo parameter is `${AWS::StackName}`.

Without the `AWS::` prefix, CloudFormation interprets `${StackName}` as a reference to a resource or parameter named `StackName`. Since no such resource or parameter exists in the template, the deployment fails with:

```
Template error: variable StackName in Fn::Sub expression does not resolve to a resource, parameter, or mapping
```

The fix:

```yaml
BucketName: !Sub "${AWS::StackName}-${AWS::Region}-data"
```

CloudFormation pseudo parameters available in `Fn::Sub`:
- `${AWS::StackName}` -- the stack name
- `${AWS::Region}` -- the deployment region
- `${AWS::AccountId}` -- the AWS account ID
- `${AWS::StackId}` -- the full stack ARN
- `${AWS::URLSuffix}` -- typically `amazonaws.com`
- `${AWS::Partition}` -- typically `aws` (or `aws-cn`, `aws-us-gov`)

All pseudo parameters require the `AWS::` prefix in `Fn::Sub` expressions.

</details>

## Verify What You Learned

```bash
# Verify the base stack deployed successfully
aws cloudformation describe-stacks --stack-name cfn-base-demo \
  --query "Stacks[0].StackStatus" --output text
```

Expected: `CREATE_COMPLETE`

```bash
# Verify exports exist
aws cloudformation list-exports \
  --query "Exports[?starts_with(Name, 'cfn-base-demo')].[Name,Value]" --output table
```

Expected: Two exports -- one for FunctionArn and one for BucketName.

```bash
# Verify the condition worked (no alarm in dev)
aws cloudwatch describe-alarms \
  --alarm-name-prefix "cfn-app-demo" \
  --query "MetricAlarms | length(@)" --output text
```

Expected: `0` (alarm is conditional on `prod` environment).

```bash
# Verify the rollback restored the original runtime
aws lambda get-function-configuration \
  --function-name cfn-base-demo-processor \
  --query "Runtime" --output text
```

Expected: `provided.al2023` (restored after rollback).

```bash
# Verify drift was detected
aws cloudformation describe-stack-resource-drifts \
  --stack-name cfn-base-demo \
  --stack-resource-drift-status-filters MODIFIED \
  --query "StackResourceDrifts | length(@)" --output text
```

Expected: `1` or more (the manually modified resource).

## Cleanup

Delete stacks in reverse dependency order (importing stack first):

```bash
# Delete the app stack first (it imports from base stack)
aws cloudformation delete-stack --stack-name cfn-app-demo
aws cloudformation wait stack-delete-complete --stack-name cfn-app-demo

# Then delete the base stack
aws cloudformation delete-stack --stack-name cfn-base-demo
aws cloudformation wait stack-delete-complete --stack-name cfn-base-demo
```

Verify nothing remains:

```bash
aws cloudformation list-stacks \
  --stack-status-filter CREATE_COMPLETE UPDATE_COMPLETE \
  --query "StackSummaries[?starts_with(StackName, 'cfn-')].StackName" --output text
```

Expected: no output.

## What's Next

You explored CloudFormation's intrinsic functions, rollback mechanics, drift detection, and change sets. In the next exercise, you will build **DynamoDB Streams with Lambda trigger patterns** -- configuring stream view types, event filtering, batch error handling, and idempotent processing.

## Summary

- **Fn::Sub** interpolates variables using `${AWS::PseudoParam}` and `${ResourceOrParam}` syntax -- pseudo parameters always require the `AWS::` prefix
- **Fn::If** references a named condition from the `Conditions` block and returns one of two values; use `AWS::NoValue` to omit a property entirely
- **Fn::Select** picks a value from a list by zero-based index -- useful for parameterized sizing (memory, instance types)
- **Fn::GetAtt** retrieves a resource attribute (like ARN) from within the same template; **Fn::ImportValue** retrieves exports from another stack
- **Rollback** occurs when a resource update fails; CloudFormation reverts all changes and the stack status becomes `UPDATE_ROLLBACK_COMPLETE`
- **Drift detection** compares live resource properties against the template; not all properties or resource types support drift detection
- **Change sets** preview proposed changes (modify, replace, delete) before execution -- analogous to `terraform plan`
- Cross-stack dependencies enforce deletion order: you cannot delete an exporting stack while another stack imports its values

## Reference

- [CloudFormation Intrinsic Functions](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/intrinsic-function-reference.html)
- [CloudFormation Conditions](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/conditions-section-structure.html)
- [CloudFormation Drift Detection](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-stack-drift.html)
- [CloudFormation Change Sets](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-updating-stacks-changesets.html)
- [CloudFormation Pseudo Parameters](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/pseudo-parameter-reference.html)

## Additional Resources

- [CloudFormation Stack Rollback Options](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/stack-failure-options.html) -- configuring rollback behavior and preserving successfully created resources
- [Fn::ImportValue and Cross-Stack References](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/intrinsic-function-reference-importvalue.html) -- rules and limitations of cross-stack exports
- [Resources That Support Drift Detection](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-stack-drift-resource-list.html) -- not all resource types support drift; check this list
- [CloudFormation Best Practices](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/best-practices.html) -- organizing templates, using change sets, and managing cross-stack dependencies

<details>
<summary>Full Solution</summary>

### File Structure

```
11-cloudformation-intrinsic-functions-rollback/
├── base-stack.yaml
├── base-stack-invalid.yaml
├── app-stack.yaml
└── lambda/
    ├── main.go
    └── go.mod
```

### lambda/main.go

```go
package main

import (
	"context"
	"encoding/json"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

func handler(ctx context.Context, event events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	body, _ := json.Marshal(map[string]string{
		"bucket":      os.Getenv("DATA_BUCKET"),
		"environment": os.Getenv("ENVIRONMENT"),
		"message":     "CloudFormation intrinsic functions demo",
	})

	return events.APIGatewayProxyResponse{
		StatusCode: 200,
		Body:       string(body),
	}, nil
}

func main() {
	lambda.Start(handler)
}
```

### base-stack.yaml

```yaml
AWSTemplateFormatVersion: '2010-09-09'
Description: >
  Base stack demonstrating CloudFormation intrinsic functions:
  Fn::Sub, Fn::If, Fn::Select, Fn::GetAtt, and cross-stack Exports.

Parameters:
  Environment:
    Type: String
    AllowedValues: [dev, prod]
    Default: dev
    Description: Deployment environment (controls conditional resources)
  MemoryIndex:
    Type: Number
    Default: 1
    AllowedValues: [0, 1, 2]
    Description: "Index into memory sizes: 0=128MB, 1=256MB, 2=512MB"

Conditions:
  IsProd: !Equals [!Ref Environment, prod]

Resources:
  # ------------------------------------------------------------------
  # S3 Bucket: name built with Fn::Sub using pseudo parameters
  # ------------------------------------------------------------------
  DataBucket:
    Type: AWS::S3::Bucket
    Properties:
      BucketName: !Sub "${AWS::StackName}-${AWS::Region}-data"
      Tags:
        - Key: Environment
          Value: !Ref Environment

  # ------------------------------------------------------------------
  # IAM Role for Lambda
  # ------------------------------------------------------------------
  LambdaRole:
    Type: AWS::IAM::Role
    Properties:
      RoleName: !Sub "${AWS::StackName}-lambda-role"
      AssumeRolePolicyDocument:
        Version: '2012-10-17'
        Statement:
          - Effect: Allow
            Principal:
              Service: lambda.amazonaws.com
            Action: sts:AssumeRole
      ManagedPolicyArns:
        - arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole
      Policies:
        - PolicyName: s3-read
          PolicyDocument:
            Version: '2012-10-17'
            Statement:
              - Effect: Allow
                Action:
                  - s3:GetObject
                  - s3:ListBucket
                # Fn::GetAtt retrieves the bucket ARN from within this template
                Resource:
                  - !GetAtt DataBucket.Arn
                  - !Sub "${DataBucket.Arn}/*"

  # ------------------------------------------------------------------
  # Lambda Function: demonstrates Fn::Select, Fn::If, Fn::Sub
  # ------------------------------------------------------------------
  # NOTE: Build the Go binary and upload to S3 or use inline ZipFile
  # For the custom runtime, package the bootstrap binary in a zip:
  #   GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
  #   zip function.zip bootstrap
  ProcessorFunction:
    Type: AWS::Lambda::Function
    Properties:
      FunctionName: !Sub "${AWS::StackName}-processor"
      Runtime: provided.al2023
      Handler: bootstrap
      Role: !GetAtt LambdaRole.Arn
      # Fn::Select: pick memory from a list by index parameter
      MemorySize: !Select [!Ref MemoryIndex, [128, 256, 512]]
      Timeout: 30
      # Fn::If: set reserved concurrency only in prod; AWS::NoValue omits it
      ReservedConcurrentExecutions: !If [IsProd, 10, !Ref "AWS::NoValue"]
      Environment:
        Variables:
          ENVIRONMENT: !Ref Environment
          DATA_BUCKET: !Ref DataBucket
          STACK_NAME: !Sub "${AWS::StackName}"
      Code:
        ZipFile: |
          package main
          import (
            "context"
            "encoding/json"
            "os"
            "github.com/aws/aws-lambda-go/lambda"
          )
          func handler(ctx context.Context) (map[string]interface{}, error) {
            return map[string]interface{}{
              "statusCode": 200,
              "body": func() string {
                b, _ := json.Marshal(map[string]string{
                  "bucket": os.Getenv("DATA_BUCKET"),
                  "environment": os.Getenv("ENVIRONMENT"),
                  "stack": os.Getenv("STACK_NAME"),
                  "message": "CloudFormation intrinsic functions demo",
                })
                return string(b)
              }(),
            }, nil
          }
          func main() { lambda.Start(handler) }

  # ------------------------------------------------------------------
  # CloudWatch Log Group
  # ------------------------------------------------------------------
  LogGroup:
    Type: AWS::Logs::LogGroup
    Properties:
      LogGroupName: !Sub "/aws/lambda/${AWS::StackName}-processor"
      RetentionInDays: 1

# ------------------------------------------------------------------
# Outputs with Export for cross-stack references
# ------------------------------------------------------------------
Outputs:
  FunctionArn:
    Description: ARN of the processor Lambda function
    Value: !GetAtt ProcessorFunction.Arn
    Export:
      Name: !Sub "${AWS::StackName}-FunctionArn"

  BucketName:
    Description: Name of the data S3 bucket
    Value: !Ref DataBucket
    Export:
      Name: !Sub "${AWS::StackName}-BucketName"

  BucketArn:
    Description: ARN of the data S3 bucket
    Value: !GetAtt DataBucket.Arn
    Export:
      Name: !Sub "${AWS::StackName}-BucketArn"
```

### base-stack-invalid.yaml

This is the same as `base-stack.yaml` but with `Runtime: provided.al2099` to trigger a rollback. Copy `base-stack.yaml` and change the Runtime line:

```yaml
      # Invalid runtime -- triggers rollback during update
      Runtime: provided.al2099
```

### app-stack.yaml

```yaml
AWSTemplateFormatVersion: '2010-09-09'
Description: >
  App stack demonstrating Fn::ImportValue for cross-stack references
  and conditional CloudWatch alarm creation.

Parameters:
  BaseStackName:
    Type: String
    Default: cfn-base-demo
    Description: Name of the base stack to import values from
  Environment:
    Type: String
    AllowedValues: [dev, prod]
    Default: dev

Conditions:
  CreateAlarm: !Equals [!Ref Environment, prod]

Resources:
  # ------------------------------------------------------------------
  # Lambda that references the base stack's exports
  # ------------------------------------------------------------------
  AppRole:
    Type: AWS::IAM::Role
    Properties:
      RoleName: !Sub "${AWS::StackName}-app-role"
      AssumeRolePolicyDocument:
        Version: '2012-10-17'
        Statement:
          - Effect: Allow
            Principal:
              Service: lambda.amazonaws.com
            Action: sts:AssumeRole
      ManagedPolicyArns:
        - arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole

  AppFunction:
    Type: AWS::Lambda::Function
    Properties:
      FunctionName: !Sub "${AWS::StackName}-app"
      Runtime: provided.al2023
      Handler: bootstrap
      Role: !GetAtt AppRole.Arn
      MemorySize: 128
      Timeout: 10
      Environment:
        Variables:
          # Fn::ImportValue retrieves exports from the base stack
          DATA_BUCKET: !ImportValue
            Fn::Sub: "${BaseStackName}-BucketName"
          PROCESSOR_ARN: !ImportValue
            Fn::Sub: "${BaseStackName}-FunctionArn"
      Code:
        ZipFile: |
          package main
          import (
            "context"
            "encoding/json"
            "os"
            "github.com/aws/aws-lambda-go/lambda"
          )
          func handler(ctx context.Context) (map[string]interface{}, error) {
            return map[string]interface{}{
              "statusCode": 200,
              "body": func() string {
                b, _ := json.Marshal(map[string]string{
                  "data_bucket": os.Getenv("DATA_BUCKET"),
                  "processor_arn": os.Getenv("PROCESSOR_ARN"),
                })
                return string(b)
              }(),
            }, nil
          }
          func main() { lambda.Start(handler) }

  # ------------------------------------------------------------------
  # Conditional alarm: only created when Environment = prod
  # ------------------------------------------------------------------
  ErrorAlarm:
    Type: AWS::CloudWatch::Alarm
    Condition: CreateAlarm
    Properties:
      AlarmName: !Sub "${AWS::StackName}-errors"
      AlarmDescription: Lambda error rate alarm (prod only)
      Namespace: AWS/Lambda
      MetricName: Errors
      Dimensions:
        - Name: FunctionName
          Value: !Ref AppFunction
      Statistic: Sum
      Period: 300
      EvaluationPeriods: 1
      Threshold: 1
      ComparisonOperator: GreaterThanOrEqualToThreshold

Outputs:
  AppFunctionName:
    Value: !Ref AppFunction
  AlarmCreated:
    Value: !If [CreateAlarm, "yes", "no"]
```

### Deployment Commands

```bash
# Step 1: Deploy the base stack
aws cloudformation create-stack \
  --stack-name cfn-base-demo \
  --template-body file://base-stack.yaml \
  --parameters ParameterKey=Environment,ParameterValue=dev \
               ParameterKey=MemoryIndex,ParameterValue=1 \
  --capabilities CAPABILITY_NAMED_IAM

aws cloudformation wait stack-create-complete --stack-name cfn-base-demo

# Step 2: Deploy the app stack (imports from base stack)
aws cloudformation create-stack \
  --stack-name cfn-app-demo \
  --template-body file://app-stack.yaml \
  --parameters ParameterKey=BaseStackName,ParameterValue=cfn-base-demo \
               ParameterKey=Environment,ParameterValue=dev \
  --capabilities CAPABILITY_NAMED_IAM

aws cloudformation wait stack-create-complete --stack-name cfn-app-demo

# Step 3: Verify exports
aws cloudformation list-exports \
  --query "Exports[?starts_with(Name, 'cfn-base-demo')]" --output table

# Step 4: Trigger a rollback with invalid runtime
aws cloudformation update-stack \
  --stack-name cfn-base-demo \
  --template-body file://base-stack-invalid.yaml \
  --parameters ParameterKey=Environment,ParameterValue=dev \
               ParameterKey=MemoryIndex,ParameterValue=1 \
  --capabilities CAPABILITY_NAMED_IAM

# Wait and observe the rollback
aws cloudformation wait stack-update-complete --stack-name cfn-base-demo 2>/dev/null || true
aws cloudformation describe-stack-events --stack-name cfn-base-demo \
  --query "StackEvents[0:5].[ResourceStatus,LogicalResourceId,ResourceStatusReason]" \
  --output table

# Step 5: Cause drift by modifying Lambda memory outside CloudFormation
aws lambda update-function-configuration \
  --function-name cfn-base-demo-processor \
  --memory-size 512

# Detect drift
DRIFT_ID=$(aws cloudformation detect-stack-drift \
  --stack-name cfn-base-demo --query "StackDriftDetectionId" --output text)
sleep 10
aws cloudformation describe-stack-resource-drifts \
  --stack-name cfn-base-demo \
  --stack-resource-drift-status-filters MODIFIED --output json

# Step 6: Create and review a change set
aws cloudformation create-change-set \
  --stack-name cfn-base-demo \
  --change-set-name update-memory-size \
  --template-body file://base-stack.yaml \
  --parameters ParameterKey=Environment,ParameterValue=dev \
               ParameterKey=MemoryIndex,ParameterValue=2 \
  --capabilities CAPABILITY_NAMED_IAM

sleep 5
aws cloudformation describe-change-set \
  --stack-name cfn-base-demo \
  --change-set-name update-memory-size \
  --query "Changes[*].ResourceChange.{Action:Action,Resource:LogicalResourceId,Replacement:Replacement}" \
  --output table

# Execute the change set
aws cloudformation execute-change-set \
  --stack-name cfn-base-demo \
  --change-set-name update-memory-size
```

### Cleanup

```bash
aws cloudformation delete-stack --stack-name cfn-app-demo
aws cloudformation wait stack-delete-complete --stack-name cfn-app-demo
aws cloudformation delete-stack --stack-name cfn-base-demo
aws cloudformation wait stack-delete-complete --stack-name cfn-base-demo
```

</details>

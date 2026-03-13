# 72. CloudFormation Helper Scripts

<!--
difficulty: intermediate
concepts: [cfn-init, cfn-signal, cfn-hup, creation-policy, metadata-init, packages, files, services, configsets, wait-condition]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: apply, analyze
prerequisites: [11-cloudformation-intrinsic-functions-rollback]
aws_cost: ~$0.02/hr
-->

> **AWS Cost Warning:** This exercise creates an EC2 instance (t3.micro), a security group, and associated resources. EC2 cost is approximately $0.02/hr for t3.micro. Remember to delete the CloudFormation stack when finished to stop incurring charges.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| jq installed | `jq --version` |
| An EC2 key pair (optional) | `aws ec2 describe-key-pairs` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Implement** cfn-init metadata to install packages, create files, and manage services on an EC2 instance during stack creation
2. **Configure** cfn-signal to report success or failure back to CloudFormation, used with a CreationPolicy to gate stack completion
3. **Apply** cfn-hup to detect metadata changes and re-run cfn-init when the stack is updated
4. **Analyze** what happens when cfn-signal is not called: the stack waits until the CreationPolicy timeout and then rolls back
5. **Differentiate** between UserData scripts (run once at boot) and cfn-init (declarative, re-runnable, metadata-driven)

## Why CloudFormation Helper Scripts

CloudFormation creates AWS resources, but it does not know what happens inside an EC2 instance. When you launch an instance with UserData, CloudFormation marks the resource as `CREATE_COMPLETE` as soon as the instance enters the `running` state -- before your initialization scripts finish (or even start). If the application fails to install, CloudFormation does not know.

Helper scripts solve this by providing a feedback loop:

1. **cfn-init**: reads metadata from the CloudFormation template (`AWS::CloudFormation::Init`) and performs declarative configuration -- installing packages, writing files, starting services. Unlike bash scripts in UserData, cfn-init is idempotent and can be re-run on updates.

2. **cfn-signal**: sends a success or failure signal back to CloudFormation. Combined with a `CreationPolicy`, CloudFormation waits for the signal before marking the instance as complete. If the signal does not arrive within the timeout, CloudFormation rolls back.

3. **cfn-hup**: a daemon that polls for metadata changes. When you update the stack (changing metadata in the template), cfn-hup detects the change and re-runs cfn-init to apply the new configuration.

The DVA-C02 exam tests the relationship between these helpers and the `CreationPolicy`. The most common bug: the developer configures cfn-init and CreationPolicy but forgets to call cfn-signal. CloudFormation waits for the signal, times out (default 15 minutes), and rolls back the entire stack.

## Building Blocks

Create the following files in your exercise directory. Your job is to fill in each `# TODO` block.

### `cfn-helpers-template.yaml`

```yaml
AWSTemplateFormatVersion: '2010-09-09'
Description: EC2 instance with cfn-init, cfn-signal, and cfn-hup

Parameters:
  ProjectName:
    Type: String
    Default: cfn-helpers-demo
  InstanceType:
    Type: String
    Default: t3.micro
  LatestAmiId:
    Type: AWS::SSM::Parameter::Value<AWS::EC2::Image::Id>
    Default: /aws/service/ami-amazon-linux-latest/al2023-ami-kernel-6.1-arm64

Resources:
  # -- Security Group --
  InstanceSecurityGroup:
    Type: AWS::EC2::SecurityGroup
    Properties:
      GroupDescription: Allow HTTP and SSH
      SecurityGroupIngress:
        - IpProtocol: tcp
          FromPort: 80
          ToPort: 80
          CidrIp: 0.0.0.0/0
        - IpProtocol: tcp
          FromPort: 22
          ToPort: 22
          CidrIp: 0.0.0.0/0
      Tags:
        - Key: Name
          Value: !Sub '${ProjectName}-sg'

  # -- IAM Role for SSM and CloudFormation --
  InstanceRole:
    Type: AWS::IAM::Role
    Properties:
      RoleName: !Sub '${ProjectName}-instance-role'
      AssumeRolePolicyDocument:
        Version: '2012-10-17'
        Statement:
          - Effect: Allow
            Principal:
              Service: ec2.amazonaws.com
            Action: sts:AssumeRole
      ManagedPolicyArns:
        - arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore
        - arn:aws:iam::aws:policy/CloudWatchAgentServerPolicy

  InstanceProfile:
    Type: AWS::IAM::InstanceProfile
    Properties:
      Roles:
        - !Ref InstanceRole

  # =====================================================
  # TODO 1 -- EC2 Instance with cfn-init Metadata
  # =====================================================
  # Create an EC2 instance with AWS::CloudFormation::Init
  # metadata that:
  #
  # Metadata:
  #   AWS::CloudFormation::Init:
  #     configSets:
  #       default: [install, configure, start]
  #
  #     install:
  #       packages:
  #         yum:
  #           httpd: []        # Install Apache
  #           jq: []           # Install jq
  #
  #     configure:
  #       files:
  #         /var/www/html/index.html:
  #           content: |
  #             <html>
  #             <body>
  #             <h1>Hello from cfn-init!</h1>
  #             <p>Instance: INSTANCE_ID</p>
  #             <p>Region: REGION</p>
  #             </body>
  #             </html>
  #           mode: '000644'
  #           owner: root
  #           group: root
  #
  #         /etc/cfn/cfn-hup.conf:
  #           content: !Sub |
  #             [main]
  #             stack=${AWS::StackId}
  #             region=${AWS::Region}
  #             interval=1
  #           mode: '000400'
  #           owner: root
  #           group: root
  #
  #         /etc/cfn/hooks.d/cfn-auto-reloader.conf:
  #           content: !Sub |
  #             [cfn-auto-reloader-hook]
  #             triggers=post.update
  #             path=Resources.WebServer.Metadata.AWS::CloudFormation::Init
  #             action=/opt/aws/bin/cfn-init -v --stack ${AWS::StackName} --resource WebServer --configsets default --region ${AWS::Region}
  #             runas=root
  #           mode: '000400'
  #           owner: root
  #           group: root
  #
  #     start:
  #       services:
  #         sysvinit:
  #           httpd:
  #             enabled: true
  #             ensureRunning: true
  #           cfn-hup:
  #             enabled: true
  #             ensureRunning: true
  #             files:
  #               - /etc/cfn/cfn-hup.conf
  #               - /etc/cfn/hooks.d/cfn-auto-reloader.conf
  #
  # Properties:
  #   - ImageId: !Ref LatestAmiId
  #   - InstanceType: !Ref InstanceType
  #   - SecurityGroupIds: [!Ref InstanceSecurityGroup]
  #   - IamInstanceProfile: !Ref InstanceProfile
  #
  # CreationPolicy:
  #   ResourceSignal:
  #     Timeout: PT5M    # Wait 5 minutes for cfn-signal
  #     Count: 1         # Expect 1 signal
  #
  # UserData (calls cfn-init then cfn-signal):
  #   Fn::Base64: !Sub |
  #     #!/bin/bash -xe
  #     /opt/aws/bin/cfn-init -v \
  #       --stack ${AWS::StackName} \
  #       --resource WebServer \
  #       --configsets default \
  #       --region ${AWS::Region}
  #
  #     /opt/aws/bin/cfn-signal -e $? \
  #       --stack ${AWS::StackName} \
  #       --resource WebServer \
  #       --region ${AWS::Region}
  #
  # Docs:
  #   - https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-init.html
  #   - https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/cfn-signal.html


Outputs:
  InstanceId:
    Value: !Ref WebServer
  PublicIp:
    Value: !GetAtt WebServer.PublicIp
    Description: Public IP of the web server
  WebUrl:
    Value: !Sub 'http://${WebServer.PublicIp}'
    Description: URL of the web server
```

### `providers.tf`

```hcl
terraform {
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 5.0" }
  }
}

provider "aws" {
  region = var.region
}
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
  default     = "cfn-helpers-demo"
}
```

## Spot the Bug

A developer configures cfn-init and a CreationPolicy but forgets to call cfn-signal in the UserData script. The stack waits for 15 minutes and then rolls back:

```yaml
Resources:
  WebServer:
    Type: AWS::EC2::Instance
    CreationPolicy:
      ResourceSignal:
        Timeout: PT15M
    Metadata:
      AWS::CloudFormation::Init:
        config:
          packages:
            yum:
              httpd: []
          services:
            sysvinit:
              httpd:
                enabled: true
                ensureRunning: true
    Properties:
      ImageId: !Ref LatestAmiId
      InstanceType: t3.micro
      UserData:
        Fn::Base64: !Sub |
          #!/bin/bash -xe
          /opt/aws/bin/cfn-init -v \
            --stack ${AWS::StackName} \
            --resource WebServer \
            --region ${AWS::Region}
          # cfn-signal is MISSING  # <-- BUG
```

<details>
<summary>Explain the bug</summary>

The `CreationPolicy` with `ResourceSignal` tells CloudFormation: "Do not mark this resource as CREATE_COMPLETE until you receive 1 success signal within 15 minutes." The UserData script runs `cfn-init` to install and configure software, but it **never calls `cfn-signal`** to report back.

CloudFormation waits for the signal. After 15 minutes (PT15M), no signal arrives, so CloudFormation concludes the initialization failed and marks the resource as `CREATE_FAILED`. This triggers a rollback of the entire stack, including the instance itself.

The insidious part: the instance may be perfectly healthy with Apache running correctly. But without the signal, CloudFormation does not know that.

**Fix -- add cfn-signal after cfn-init:**

```yaml
UserData:
  Fn::Base64: !Sub |
    #!/bin/bash -xe
    /opt/aws/bin/cfn-init -v \
      --stack ${AWS::StackName} \
      --resource WebServer \
      --region ${AWS::Region}

    # Signal CloudFormation with the exit code of cfn-init
    # $? is 0 if cfn-init succeeded, non-zero if it failed
    /opt/aws/bin/cfn-signal -e $? \
      --stack ${AWS::StackName} \
      --resource WebServer \
      --region ${AWS::Region}
```

The `-e $?` flag sends the exit code of the previous command. If cfn-init fails (non-zero exit), cfn-signal sends a failure signal, and CloudFormation rolls back immediately instead of waiting for the timeout.

</details>

## Solutions

<details>
<summary>TODO 1 -- EC2 Instance with cfn-init, cfn-signal, cfn-hup (`cfn-helpers-template.yaml`)</summary>

Key structure of the `WebServer` resource:

- **CreationPolicy**: `ResourceSignal` with `Timeout: PT5M`, `Count: 1`
- **Metadata** `AWS::CloudFormation::Init`: configSets `[install, configure, start]`
  - `install`: yum packages (`httpd`, `jq`)
  - `configure`: files (`/var/www/html/index.html`, `/etc/cfn/cfn-hup.conf`, `/etc/cfn/hooks.d/cfn-auto-reloader.conf`)
  - `start`: services (httpd enabled+running, cfn-hup enabled+running watching config files)
- **UserData**: runs `cfn-init` then `cfn-signal -e $?`
- **Properties**: ImageId from SSM, SecurityGroupIds, IamInstanceProfile

The cfn-hup hook watches `Resources.WebServer.Metadata.AWS::CloudFormation::Init` for changes and re-runs cfn-init on updates.

</details>

## Verify What You Learned

### Step 1 -- Create the stack

```bash
aws cloudformation create-stack \
  --stack-name cfn-helpers-demo \
  --template-body file://cfn-helpers-template.yaml \
  --capabilities CAPABILITY_NAMED_IAM

echo "Waiting for stack creation (includes cfn-signal wait)..."
aws cloudformation wait stack-create-complete --stack-name cfn-helpers-demo
echo "Stack created successfully!"
```

### Step 2 -- Verify the web server is running

```bash
PUBLIC_IP=$(aws cloudformation describe-stacks \
  --stack-name cfn-helpers-demo \
  --query "Stacks[0].Outputs[?OutputKey=='PublicIp'].OutputValue" --output text)

curl -s "http://$PUBLIC_IP"
```

Expected: HTML page showing "Hello from cfn-init!" with stack name and region.

### Step 3 -- Verify cfn-init ran successfully

```bash
INSTANCE_ID=$(aws cloudformation describe-stacks \
  --stack-name cfn-helpers-demo \
  --query "Stacks[0].Outputs[?OutputKey=='InstanceId'].OutputValue" --output text)

# Check cfn-init log via SSM (if SSM is available)
aws ssm send-command \
  --instance-ids "$INSTANCE_ID" \
  --document-name "AWS-RunShellScript" \
  --parameters commands=["tail -20 /var/log/cfn-init.log"] \
  --query "Command.CommandId" --output text 2>/dev/null
```

### Step 4 -- Verify stack events show successful signal

```bash
aws cloudformation describe-stack-events \
  --stack-name cfn-helpers-demo \
  --query "StackEvents[?ResourceType=='AWS::EC2::Instance'].{Time:Timestamp,Status:ResourceStatus,Reason:ResourceStatusReason}" \
  --output table | head -20
```

Expected: events showing `CREATE_IN_PROGRESS`, then `Received SUCCESS signal`, then `CREATE_COMPLETE`.

### Step 5 -- Test cfn-hup by updating the stack

```bash
# Modify the HTML content in the template, then update the stack
# (change "Hello from cfn-init!" to "Updated via cfn-hup!")
# cfn-hup will detect the metadata change and re-run cfn-init

aws cloudformation update-stack \
  --stack-name cfn-helpers-demo \
  --template-body file://cfn-helpers-template.yaml \
  --capabilities CAPABILITY_NAMED_IAM \
  --parameters ParameterKey=ProjectName,ParameterValue=cfn-helpers-demo 2>/dev/null || echo "No changes to update"
```

## Cleanup

Delete the CloudFormation stack:

```bash
aws cloudformation delete-stack --stack-name cfn-helpers-demo
aws cloudformation wait stack-delete-complete --stack-name cfn-helpers-demo
```

Verify:

```bash
aws cloudformation describe-stacks --stack-name cfn-helpers-demo 2>&1 | head -1
```

Expected: stack not found error.

## What's Next

You configured CloudFormation helper scripts to install software, signal completion, and detect updates on EC2 instances. These are foundational infrastructure-as-code patterns that the DVA-C02 exam expects you to understand for EC2-based deployments.

## Summary

- **cfn-init** reads `AWS::CloudFormation::Init` metadata and performs declarative configuration (packages, files, services)
- **cfn-signal** sends SUCCESS/FAILED back to CloudFormation, gating the `CreationPolicy` completion
- **cfn-hup** is a daemon that polls for metadata changes and re-runs cfn-init on stack updates
- Without cfn-signal, CloudFormation **waits until the CreationPolicy timeout and then rolls back** -- even if the instance is healthy
- The `-e $?` flag passes the cfn-init exit code to cfn-signal: 0 = success, non-zero = failure
- **configSets** define the order of configuration execution: install -> configure -> start
- cfn-init is **idempotent** and can be re-run safely, unlike raw bash scripts in UserData
- The `services` section ensures daemons are started and enabled for reboot persistence
- cfn-hup requires two config files: `/etc/cfn/cfn-hup.conf` and a hooks file in `/etc/cfn/hooks.d/`

## Reference

- [cfn-init](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/cfn-init.html)
- [cfn-signal](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/cfn-signal.html)
- [cfn-hup](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/cfn-hup.html)
- [CreationPolicy](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-attribute-creationpolicy.html)

## Additional Resources

- [AWS::CloudFormation::Init](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-init.html) -- complete metadata reference
- [ConfigSets](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-init.html#aws-resource-init-configsets) -- ordering and grouping configurations
- [WaitCondition vs CreationPolicy](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-waitcondition.html) -- when to use each mechanism
- [Bootstrapping EC2 Best Practices](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/best-practices.html#cfn-best-practices-bootstrap) -- AWS recommendations for instance initialization

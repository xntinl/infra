# 75. AWS CDK Constructs and Synthesis with Go

<!--
difficulty: advanced
concepts: [aws-cdk, cdk-go, l1-constructs, l2-constructs, l3-constructs, cdk-synth, cdk-diff, cdk-deploy, construct-tree, cdk-aspects, cdk-app, cdk-stack]
tools: [cdk-cli, aws-cli, go]
estimated_time: 50m
bloom_level: create, evaluate
prerequisites: [11-cloudformation-intrinsic-functions-rollback, 01-lambda-environment-layers-configuration]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a Lambda function and API Gateway using CDK. All costs are negligible for testing (~$0.01/hr or less). Remember to run `cdk destroy` when finished.

## Prerequisites

- AWS CDK CLI >= 2.100 installed (`npm install -g aws-cdk`)
- AWS CLI configured with a sandbox account
- Go 1.21+ installed
- Node.js >= 18 installed (required by CDK CLI)
- CDK bootstrapped in your account (`cdk bootstrap aws://ACCOUNT_ID/us-east-1`)

## Learning Objectives

After completing this exercise, you will be able to:

- **Create** a CDK application in Go with stacks containing L1 (Cfn), L2 (curated), and L3 (patterns) constructs
- **Evaluate** the differences between construct levels: L1 maps 1:1 to CloudFormation resources, L2 adds defaults and convenience methods, L3 composes multiple resources into common patterns
- **Analyze** synthesized CloudFormation output using `cdk synth`, compare changes with `cdk diff`, and deploy with `cdk deploy`
- **Design** CDK Aspects that enforce compliance rules (e.g., all Lambda functions must have tracing enabled) across an entire construct tree
- **Explain** the CDK synthesis process: App -> Stacks -> Constructs -> CloudFormation template -> CloudFormation deployment

## Why This Matters

AWS CDK lets you define cloud infrastructure using familiar programming languages instead of YAML or JSON. The Go CDK library provides type-safe constructs that map to AWS resources, catching configuration errors at compile time rather than deploy time.

The exam tests three construct levels: **L1** (`Cfn` prefix) maps 1:1 to CloudFormation. **L2** (e.g., `awslambda.Function`) adds defaults and convenience methods like `GrantReadWriteData`. **L3** (patterns) composes multiple L2 constructs into architectures.

The CDK lifecycle: `cdk synth` generates CloudFormation without deploying, `cdk diff` compares with deployed state, `cdk deploy` creates/updates the stack. CDK is a CloudFormation generator, not a replacement.

## The Challenge

Build a CDK application in Go that creates a Lambda function behind an API Gateway HTTP API. Use different construct levels to understand their trade-offs, and implement a CDK Aspect that enforces Lambda tracing.

### Requirements

| Requirement | Description |
|---|---|
| CDK App | Go CDK application with one stack |
| Lambda (L2) | Lambda function using the L2 construct with Go runtime |
| API Gateway (L2) | HTTP API using L2 constructs |
| Aspect | CDK Aspect ensuring all Lambda functions have X-Ray tracing enabled |
| Synthesis | Verify with `cdk synth` and `cdk diff` |

### Architecture

```
  CDK App (Go)
  +------------------+
  |  MyStack         |
  |  +-- Lambda (L2) |     cdk synth      CloudFormation
  |  +-- HTTP API    |  ------------->  +------------------+
  |  +-- Integration |                  | AWS::Lambda::Fn  |
  |  +-- Aspect      |                  | AWS::ApiGWv2::Api|
  +------------------+                  | AWS::IAM::Role   |
                                        +------------------+
```

## Hints

<details>
<summary>Hint 1: CDK Go project structure</summary>

Initialize a CDK Go project:

```bash
mkdir cdk-lab && cd cdk-lab
cdk init app --language go
```

This creates:

```
cdk-lab/
├── cdk-lab.go          # App entry point
├── cdk-lab_test.go     # Tests
├── cdk.json            # CDK configuration
├── go.mod
└── go.sum
```

The main file defines the App and Stack:

```go
package main

import (
    "github.com/aws/aws-cdk-go/awscdk/v2"
    "github.com/aws/constructs-go/constructs/v10"
    "github.com/aws/jsii-runtime-go"
)

type MyStackProps struct {
    awscdk.StackProps
}

func NewMyStack(scope constructs.Construct, id string, props *MyStackProps) awscdk.Stack {
    var sprops awscdk.StackProps
    if props != nil {
        sprops = props.StackProps
    }
    stack := awscdk.NewStack(scope, &id, &sprops)

    // Define resources here

    return stack
}

func main() {
    defer jsii.Close()

    app := awscdk.NewApp(nil)
    NewMyStack(app, "CdkLabStack", &MyStackProps{
        awscdk.StackProps{
            Env: env(),
        },
    })
    app.Synth(nil)
}

func env() *awscdk.Environment {
    return &awscdk.Environment{
        Region: jsii.String("us-east-1"),
    }
}
```

</details>

<details>
<summary>Hint 2: L1 vs L2 vs L3 constructs</summary>

**L2 (curated constructs)** -- sensible defaults, convenience methods:

```go
// L2: Defaults for logging, IAM role, etc. are created automatically
fn := awslambda.NewFunction(stack, jsii.String("MyFnL2"), &awslambda.FunctionProps{
    Runtime:      awslambda.Runtime_PROVIDED_AL2023(),
    Handler:      jsii.String("bootstrap"),
    Code:         awslambda.Code_FromAsset(jsii.String("./app"), nil),
    Architecture: awslambda.Architecture_ARM_64(),
    Timeout:      awscdk.Duration_Seconds(jsii.Number(15)),
})
// L2 auto-creates: IAM role, log group, and grants
```

**L3 (patterns)** -- compose multiple resources:

```go
import "github.com/aws/aws-cdk-go/awscdk/v2/awsapigatewayv2integrations"

// L3: Creates HTTP API + Lambda integration + route in one call
integration := awsapigatewayv2integrations.NewHttpLambdaIntegration(
    jsii.String("LambdaIntegration"), fn, nil)
```

</details>

<details>
<summary>Hint 3: CDK Aspects for compliance</summary>

CDK Aspects are visitors that traverse the construct tree and can inspect or modify every construct. Use them to enforce organizational policies:

```go
import (
    "fmt"
    "github.com/aws/aws-cdk-go/awscdk/v2"
    "github.com/aws/aws-cdk-go/awscdk/v2/awslambda"
    "github.com/aws/constructs-go/constructs/v10"
    "github.com/aws/jsii-runtime-go"
)

type TracingAspect struct{}

func (t *TracingAspect) Visit(node constructs.IConstruct) {
    // Check if the construct is a Lambda function (L1 level)
    if fn, ok := node.(awslambda.CfnFunction); ok {
        // Ensure tracing is enabled
        if fn.TracingConfig() == nil {
            awscdk.Annotations_Of(node).AddWarning(
                jsii.String("Lambda function missing X-Ray tracing configuration"))
        }
    }
}

// Apply the aspect to the stack:
// awscdk.Aspects_Of(stack).Add(&TracingAspect{})
```

Aspects run during synthesis (`cdk synth`). They can add warnings, errors, or even modify resource properties.

</details>

<details>
<summary>Hint 4: Synthesis and deployment commands</summary>

```bash
# Synthesize -- generates CloudFormation template without deploying
cdk synth

# View the generated template
cdk synth > template.yaml
cat template.yaml

# Compare with deployed stack
cdk diff

# Deploy
cdk deploy --require-approval never

# Destroy
cdk destroy --force
```

The synthesized template is stored in `cdk.out/CdkLabStack.template.json`. Inspect it to understand what CDK generates from your constructs.

</details>

<details>
<summary>Hint 5: Grant methods for cross-resource permissions</summary>

L2 constructs provide grant methods that create least-privilege IAM policies:

```go
// Grant the function read/write access to the table
table.GrantReadWriteData(fn)

// Grant the function permission to publish to a topic
topic.GrantPublish(fn)

// Grant the function permission to send messages to a queue
queue.GrantSendMessages(fn)
```

These methods create scoped IAM policies on the function's execution role automatically. No manual IAM policy writing required.

</details>

## Spot the Bug

A developer creates a CDK stack with a Lambda function, but `cdk synth` generates a template where the Lambda function has `Runtime: python3.12` even though they specified Go. **What is wrong?**

```go
fn := awslambda.NewFunction(stack, jsii.String("Handler"), &awslambda.FunctionProps{
    Runtime: awslambda.Runtime_PYTHON_3_12(),    // <-- BUG: wrong runtime
    Handler: jsii.String("bootstrap"),
    Code:    awslambda.Code_FromAsset(jsii.String("./app"), nil),
})
```

<details>
<summary>Explain the bug</summary>

The developer used `awslambda.Runtime_PYTHON_3_12()` instead of `awslambda.Runtime_PROVIDED_AL2023()`. Go Lambda functions compiled as custom runtimes use the `provided.al2023` runtime, not any language-specific runtime. The `Handler` is set to `bootstrap` (correct for Go custom runtime), but the Runtime does not match.

The fix:

```go
fn := awslambda.NewFunction(stack, jsii.String("Handler"), &awslambda.FunctionProps{
    Runtime:      awslambda.Runtime_PROVIDED_AL2023(),
    Handler:      jsii.String("bootstrap"),
    Architecture: awslambda.Architecture_ARM_64(),
    Code:         awslambda.Code_FromAsset(jsii.String("./app"), nil),
})
```

This is a compile-time-safe error in a sense -- the code compiles and synthesizes, but the deployed function fails at runtime because `bootstrap` is not a valid Python handler. CDK catches type errors but cannot validate semantic correctness (you chose a valid runtime, just the wrong one for your code).

On the exam, CDK questions test whether you understand that Go Lambda functions use `provided.al2023` (custom runtime) with handler `bootstrap`, not a language-specific runtime.

</details>

<details>
<summary>Full Solution</summary>

### Project structure

```
cdk-lab/
├── app/
│   └── main.go          # Lambda function code
├── cdk-lab.go           # CDK app
├── cdk.json
├── go.mod
└── go.sum
```

### app/main.go (Lambda function)

```go
package main

import (
    "context"
    "fmt"
    "os"
    "time"

    "github.com/aws/aws-lambda-go/events"
    "github.com/aws/aws-lambda-go/lambda"
)

func handler(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
    fmt.Printf("Request: %s %s\n", req.RequestContext.HTTP.Method, req.RequestContext.HTTP.Path)

    body := fmt.Sprintf(`{"message":"Hello from CDK Go Lambda","region":"%s","time":"%s"}`,
        os.Getenv("AWS_REGION"), time.Now().UTC().Format(time.RFC3339))

    return events.APIGatewayV2HTTPResponse{
        StatusCode: 200,
        Headers:    map[string]string{"Content-Type": "application/json"},
        Body:       body,
    }, nil
}

func main() {
    lambda.Start(handler)
}
```

### cdk-lab.go (CDK app)

```go
package main

import (
    "github.com/aws/aws-cdk-go/awscdk/v2"
    "github.com/aws/aws-cdk-go/awscdk/v2/awsapigatewayv2"
    "github.com/aws/aws-cdk-go/awscdk/v2/awsapigatewayv2integrations"
    "github.com/aws/aws-cdk-go/awscdk/v2/awslambda"
    "github.com/aws/constructs-go/constructs/v10"
    "github.com/aws/jsii-runtime-go"
)

// TracingAspect enforces X-Ray tracing on all Lambda functions
type TracingAspect struct{}

func (a *TracingAspect) Visit(node constructs.IConstruct) {
    if fn, ok := node.(awslambda.CfnFunction); ok {
        fn.SetTracingConfig(&awslambda.CfnFunction_TracingConfigProperty{
            Mode: jsii.String("Active"),
        })
    }
}

func NewCdkLabStack(scope constructs.Construct, id string, props *awscdk.StackProps) awscdk.Stack {
    stack := awscdk.NewStack(scope, &id, props)

    // L2 Lambda construct -- creates IAM role, log group automatically
    fn := awslambda.NewFunction(stack, jsii.String("Handler"), &awslambda.FunctionProps{
        Runtime:      awslambda.Runtime_PROVIDED_AL2023(),
        Handler:      jsii.String("bootstrap"),
        Architecture: awslambda.Architecture_ARM_64(),
        Code:         awslambda.Code_FromAsset(jsii.String("./app"), &awslambda.AssetOptions{}),
        Timeout:      awscdk.Duration_Seconds(jsii.Number(15)),
        MemorySize:   jsii.Number(128),
    })

    // L3 pattern -- HTTP API with Lambda integration
    integration := awsapigatewayv2integrations.NewHttpLambdaIntegration(
        jsii.String("LambdaIntegration"), fn, nil)

    httpApi := awsapigatewayv2.NewHttpApi(stack, jsii.String("HttpApi"), &awsapigatewayv2.HttpApiProps{
        ApiName: jsii.String("cdk-lab-api"),
    })

    httpApi.AddRoutes(&awsapigatewayv2.AddRoutesOptions{
        Path:        jsii.String("/hello"),
        Methods:     &[]awsapigatewayv2.HttpMethod{awsapigatewayv2.HttpMethod_GET},
        Integration: integration,
    })

    // Apply Aspect: enforce tracing on all Lambda functions
    awscdk.Aspects_Of(stack).Add(&TracingAspect{})

    // Outputs
    awscdk.NewCfnOutput(stack, jsii.String("ApiUrl"), &awscdk.CfnOutputProps{
        Value:       httpApi.Url(),
        Description: jsii.String("HTTP API endpoint URL"),
    })

    awscdk.NewCfnOutput(stack, jsii.String("FunctionName"), &awscdk.CfnOutputProps{
        Value: fn.FunctionName(),
    })

    return stack
}

func main() {
    defer jsii.Close()
    app := awscdk.NewApp(nil)

    NewCdkLabStack(app, "CdkLabStack", &awscdk.StackProps{
        Env: &awscdk.Environment{
            Region: jsii.String("us-east-1"),
        },
    })

    app.Synth(nil)
}
```

### Build and deploy

```bash
# Build the Lambda function
cd app && GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go && cd ..

# Synthesize the CloudFormation template
cdk synth

# View what will be created
cdk diff

# Deploy
cdk deploy --require-approval never

# Test
API_URL=$(aws cloudformation describe-stacks --stack-name CdkLabStack \
  --query "Stacks[0].Outputs[?OutputKey=='ApiUrl'].OutputValue" --output text)
curl -s "${API_URL}hello" | jq .

# Destroy
cdk destroy --force
```

</details>

## Verify What You Learned

```bash
# Verify synthesis generates valid CloudFormation
cdk synth > /dev/null && echo "Synthesis successful"

# Count resources in synthesized template
cdk synth 2>/dev/null | grep "Type: AWS::" | wc -l
```

Expected: several AWS resources including Lambda function, IAM role, log group, HTTP API, and integration.

```bash
# Verify the Aspect added tracing
cdk synth 2>/dev/null | grep -A2 "TracingConfig"
```

Expected: `Mode: Active` under TracingConfig, proving the Aspect modified the Lambda configuration.

## Cleanup

```bash
cdk destroy --force
```

Verify:

```bash
aws cloudformation describe-stacks --stack-name CdkLabStack 2>&1 | grep -q "does not exist" && echo "Stack deleted" || echo "Stack still exists"
```

## What's Next

You built a CDK application with L2 constructs, L3 patterns, and an Aspect for compliance. In the next exercise, you will explore **CloudFormation stack policies** -- preventing accidental updates and replacements of critical resources.

## Summary

- **CDK** is an infrastructure-as-code framework that generates CloudFormation templates from programming language code
- **L1 constructs** (`Cfn` prefix) map 1:1 to CloudFormation resources -- no defaults, full control, most verbose
- **L2 constructs** (e.g., `awslambda.Function`) add sensible defaults (IAM roles, log groups), convenience methods (`GrantReadWriteData`), and validation
- **L3 constructs** (patterns) compose multiple L2 constructs into common architectures (e.g., HTTP API + Lambda integration)
- **`cdk synth`** generates the CloudFormation template; **`cdk diff`** compares with deployed state; **`cdk deploy`** creates/updates the stack
- **Aspects** are visitors that traverse the construct tree during synthesis -- use them to enforce compliance rules or modify resources
- CDK requires **bootstrapping** (`cdk bootstrap`) to create the CDKToolkit stack with an S3 bucket and IAM roles for deployment
- Go Lambda functions use `Runtime_PROVIDED_AL2023()` with handler `bootstrap`, not language-specific runtimes
- Grant methods (`table.GrantReadWriteData(fn)`) generate least-privilege IAM policies automatically

## Reference

- [AWS CDK Go Reference](https://docs.aws.amazon.com/cdk/api/v2/docs/aws-construct-library.html)
- [CDK Concepts](https://docs.aws.amazon.com/cdk/v2/guide/core_concepts.html)
- [CDK Constructs](https://docs.aws.amazon.com/cdk/v2/guide/constructs.html)
- [CDK Aspects](https://docs.aws.amazon.com/cdk/v2/guide/aspects.html)

## Additional Resources

- [CDK Workshop (Go)](https://cdkworkshop.com/) -- hands-on tutorial covering CDK fundamentals
- [CDK Best Practices](https://docs.aws.amazon.com/cdk/v2/guide/best-practices.html) -- organization, testing, and deployment patterns
- [CDK Testing](https://docs.aws.amazon.com/cdk/v2/guide/testing.html) -- snapshot tests and fine-grained assertions for CDK stacks
- [Construct Hub](https://constructs.dev/) -- registry of open-source CDK constructs from the community

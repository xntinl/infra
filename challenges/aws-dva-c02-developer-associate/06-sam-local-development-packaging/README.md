# 6. SAM Local Development, Packaging, and Deployment

<!--
difficulty: intermediate
concepts: [sam-cli, sam-template, cloudformation-transform, sam-local-invoke, sam-local-start-api, sam-build, sam-deploy]
tools: [sam-cli, aws-cli, terraform]
estimated_time: 40m
bloom_level: design, implement
prerequisites: [none]
aws_cost: ~$0.01/hr
-->

## Prerequisites

| Requirement | Verify |
|------------|--------|
| SAM CLI >= 1.100 | `sam --version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| Docker installed and running | `docker info` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Design** a SAM template that defines serverless resources using the `AWS::Serverless` transform
2. **Implement** a multi-function SAM application with shared configuration via the `Globals` section
3. **Configure** `sam local invoke` and `sam local start-api` for local development and testing
4. **Implement** a build and deployment workflow using `sam build` and `sam deploy --guided`
5. **Differentiate** between SAM templates and raw CloudFormation -- what the transform generates under the hood

## Why This Matters

SAM (Serverless Application Model) is AWS's official framework for building serverless applications. It extends CloudFormation with a shorthand syntax that reduces boilerplate: a single `AWS::Serverless::Function` resource generates the Lambda function, IAM role, API Gateway, and permissions that would otherwise require 5-8 separate CloudFormation resources. The DVA-C02 exam tests your ability to read SAM templates, understand what they generate, and use SAM CLI commands for the development lifecycle.

The local development capabilities are equally important. `sam local invoke` runs your Lambda function in a Docker container that mimics the Lambda execution environment, so you can test with realistic event payloads without deploying. `sam local start-api` spins up a local HTTP server that routes requests through your API Gateway configuration to your Lambda functions. This tight feedback loop -- edit code, test locally, deploy when ready -- is how professional serverless developers work. Understanding the SAM CLI commands (`build`, `package`, `deploy`, `local invoke`, `local start-api`) and their flags is directly tested on the exam.

## Building Blocks

Create the following directory structure:

```
sam-app/
  template.yaml
  events/
    event.json
  hello/
    main.go
    go.mod
    Makefile
  goodbye/
    main.go
    go.mod
    Makefile
```

### Step 1 -- Create the Lambda function code

Create `sam-app/hello/main.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

func handler(ctx context.Context, event events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	name := "World"

	// Handle API Gateway proxy event
	if event.QueryStringParameters != nil {
		if n, ok := event.QueryStringParameters["name"]; ok {
			name = n
		}
	}

	body, _ := json.Marshal(map[string]string{
		"message":   fmt.Sprintf("Hello, %s!", name),
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"function":  "hello",
	})

	return events.APIGatewayProxyResponse{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(body),
	}, nil
}

func main() {
	lambda.Start(handler)
}
```

Create `sam-app/hello/go.mod`:

```
module hello

go 1.21

require (
	github.com/aws/aws-lambda-go v1.47.0
)
```

Create `sam-app/hello/Makefile` (used by SAM's makefile builder):

```makefile
.PHONY: build

build-HelloFunction:
	GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o $(ARTIFACTS_DIR)/bootstrap main.go
```

Create `sam-app/goodbye/main.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

func handler(ctx context.Context, event events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	name := "World"

	if event.QueryStringParameters != nil {
		if n, ok := event.QueryStringParameters["name"]; ok {
			name = n
		}
	}

	body, _ := json.Marshal(map[string]string{
		"message":   fmt.Sprintf("Goodbye, %s!", name),
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"function":  "goodbye",
	})

	return events.APIGatewayProxyResponse{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(body),
	}, nil
}

func main() {
	lambda.Start(handler)
}
```

Create `sam-app/goodbye/go.mod`:

```
module goodbye

go 1.21

require (
	github.com/aws/aws-lambda-go v1.47.0
)
```

Create `sam-app/goodbye/Makefile` (used by SAM's makefile builder):

```makefile
.PHONY: build

build-GoodbyeFunction:
	GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o $(ARTIFACTS_DIR)/bootstrap main.go
```

### Step 2 -- Create the test event

Create `sam-app/events/event.json`:

```json
{
  "queryStringParameters": {
    "name": "Developer"
  },
  "httpMethod": "GET",
  "path": "/hello",
  "requestContext": {
    "stage": "dev"
  }
}
```

### Step 3 -- Complete the SAM template

Create `sam-app/template.yaml` with the following skeleton. Your job is to fill in each `# TODO` block.

```yaml
AWSTemplateFormatVersion: '2010-09-09'
Transform: AWS::Serverless-2016-10-31
Description: SAM local development lab -- DVA-C02

# =======================================================
# TODO 1 -- Globals section
# =======================================================
# Requirements:
#   - Add a Globals section that sets default values for
#     ALL functions in this template:
#     - Runtime: provided.al2023
#     - Architectures: [arm64]
#     - Timeout: 10
#     - MemorySize: 128
#     - Environment variable LOG_LEVEL: INFO
#   - Globals prevent repeating the same config on every
#     function. Individual functions can override these.
#
# Docs: https://docs.aws.amazon.com/serverless-application-model/latest/developerguide/sam-specification-template-anatomy-globals.html
# Format:
#   Globals:
#     Function:
#       Runtime: ...
#       Timeout: ...


# =======================================================
# TODO 2 -- HelloFunction resource
# =======================================================
# Requirements:
#   - Define an AWS::Serverless::Function named HelloFunction
#   - CodeUri: hello/
#   - Handler: bootstrap
#   - BuildMethod: makefile
#   - Do NOT set Runtime or Timeout (inherited from Globals)
#   - Add an Events section with an Api event:
#     - Type: Api
#     - Path: /hello
#     - Method: get
#   - SAM auto-generates the API Gateway, IAM role, and
#     Lambda permission from this single resource
#
# Docs: https://docs.aws.amazon.com/serverless-application-model/latest/developerguide/sam-resource-function.html

Resources:

  # TODO 2: HelloFunction here


# =======================================================
# TODO 3 -- GoodbyeFunction resource
# =======================================================
# Requirements:
#   - Define a second AWS::Serverless::Function named GoodbyeFunction
#   - CodeUri: goodbye/
#   - Handler: bootstrap
#   - BuildMethod: makefile
#   - Override the Timeout to 15 (demonstrating per-function override)
#   - Add an Api event on path /goodbye, method get
#
# Docs: https://docs.aws.amazon.com/serverless-application-model/latest/developerguide/sam-resource-function.html

  # TODO 3: GoodbyeFunction here


# =======================================================
# TODO 4 -- Outputs section
# =======================================================
# Requirements:
#   - Output the API Gateway endpoint URL
#   - Use the implicit !Sub reference to the ServerlessRestApi:
#     !Sub "https://${ServerlessRestApi}.execute-api.${AWS::Region}.amazonaws.com/Prod/"
#   - Output the HelloFunction ARN using !GetAtt HelloFunction.Arn
#   - Output the GoodbyeFunction ARN using !GetAtt GoodbyeFunction.Arn
#
# Docs: https://docs.aws.amazon.com/serverless-application-model/latest/developerguide/sam-specification-template-anatomy.html

Outputs:

  # TODO 4: Outputs here
```

### Step 4 -- Local testing commands

After completing the template, you will use these commands:

```bash
# =======================================================
# TODO 5 -- Build, local test, and deploy
# =======================================================
# Requirements:
#   a) Run sam build from the sam-app directory
#      This compiles your Go functions via the Makefile targets
#
#   b) Run sam local invoke HelloFunction with the test event:
#      sam local invoke HelloFunction -e events/event.json
#
#   c) Run sam local start-api to start a local HTTP server:
#      sam local start-api
#      Then in another terminal: curl http://127.0.0.1:3000/hello?name=SAM
#
#   d) Deploy to AWS with guided mode:
#      sam deploy --guided
#      This prompts for stack name, region, and capabilities
#
# Docs:
#   https://docs.aws.amazon.com/serverless-application-model/latest/developerguide/sam-cli-command-reference-sam-build.html
#   https://docs.aws.amazon.com/serverless-application-model/latest/developerguide/sam-cli-command-reference-sam-local-invoke.html
#   https://docs.aws.amazon.com/serverless-application-model/latest/developerguide/sam-cli-command-reference-sam-deploy.html
```

## Spot the Bug

A developer has two Lambda functions in their SAM template but forgot the `Globals` section. Both functions work, but the template is verbose and error-prone. When they later change the runtime to a newer version, they update one function but forget the other. **What pattern prevents this?**

```yaml
Resources:
  FunctionA:
    Type: AWS::Serverless::Function
    Properties:
      CodeUri: func_a/
      Handler: bootstrap
      Runtime: provided.al2023       # <-- duplicated
      Architectures: [arm64]         # <-- duplicated
      Timeout: 10                    # <-- duplicated
      MemorySize: 128                # <-- duplicated
      Environment:
        Variables:
          LOG_LEVEL: INFO            # <-- duplicated
    Metadata:
      BuildMethod: makefile

  FunctionB:
    Type: AWS::Serverless::Function
    Properties:
      CodeUri: func_b/
      Handler: bootstrap
      Runtime: provided.al2023       # <-- must remember to update both
      Architectures: [arm64]
      Timeout: 10
      MemorySize: 128
      Environment:
        Variables:
          LOG_LEVEL: INFO
    Metadata:
      BuildMethod: makefile
```

<details>
<summary>Explain the bug</summary>

The missing `Globals` section forces every function to repeat shared configuration. When the runtime, timeout, or environment variables change, developers must update every function individually -- a maintenance burden that leads to inconsistencies.

The fix is to extract shared settings into the `Globals` section:

```yaml
Globals:
  Function:
    Runtime: provided.al2023
    Architectures: [arm64]
    Timeout: 10
    MemorySize: 128
    Environment:
      Variables:
        LOG_LEVEL: INFO

Resources:
  FunctionA:
    Type: AWS::Serverless::Function
    Properties:
      CodeUri: func_a/
      Handler: bootstrap
      # Runtime, Architectures, Timeout, MemorySize, Environment inherited from Globals
    Metadata:
      BuildMethod: makefile

  FunctionB:
    Type: AWS::Serverless::Function
    Properties:
      CodeUri: func_b/
      Handler: bootstrap
      # Same -- inherited from Globals
    Metadata:
      BuildMethod: makefile
```

Now changing the runtime requires editing one line. Individual functions can still override any Global value when needed (e.g., a function that needs a longer timeout).

</details>

## Verify What You Learned

### Step 1 -- Build the application

```
cd sam-app
sam build
```

Expected output:

```
Building codeuri: hello/ runtime: provided.al2023 metadata: {'BuildMethod': 'makefile'} ...
Building codeuri: goodbye/ runtime: provided.al2023 metadata: {'BuildMethod': 'makefile'} ...
Build Succeeded

Built Artifacts  : .aws-sam/build
Built Template   : .aws-sam/build/template.yaml
```

### Step 2 -- Local invoke with test event

```
sam local invoke HelloFunction -e events/event.json
```

Expected output (inside Docker container output):

```json
{
    "statusCode": 200,
    "headers": {"Content-Type": "application/json"},
    "body": "{\"message\": \"Hello, Developer!\", ...}"
}
```

### Step 3 -- Local API server

In one terminal:

```
sam local start-api
```

Expected: `Running on http://127.0.0.1:3000`

In another terminal:

```
curl -s "http://127.0.0.1:3000/hello?name=SAM" | jq .
```

Expected:

```json
{
    "message": "Hello, SAM!",
    "timestamp": "2026-03-08T...",
    "function": "hello"
}
```

### Step 4 -- Deploy to AWS

```
sam deploy --guided
```

Follow the prompts:

```
Stack Name [sam-app]: sam-lab
AWS Region [us-east-1]: us-east-1
Confirm changes before deploy [Y/n]: y
Allow SAM CLI IAM role creation [Y/n]: y
HelloFunction may not have authorization defined, Is this okay? [y/N]: y
GoodbyeFunction may not have authorization defined, Is this okay? [y/N]: y
Save arguments to configuration file [Y/n]: y
```

### Step 5 -- Verify the deployed API

```
API_URL=$(aws cloudformation describe-stacks \
  --stack-name sam-lab \
  --query "Stacks[0].Outputs[?OutputKey=='ApiEndpoint'].OutputValue" \
  --output text)
curl -s "${API_URL}hello?name=Cloud" | jq .
```

Expected:

```json
{
    "message": "Hello, Cloud!",
    "timestamp": "...",
    "function": "hello"
}
```

### Step 6 -- Inspect what SAM generated

```
aws cloudformation describe-stack-resources \
  --stack-name sam-lab \
  --query "StackResources[].{Type:ResourceType,Logical:LogicalResourceId}" \
  --output table
```

Expected: You should see that a single `AWS::Serverless::Function` expanded into multiple CloudFormation resources including `AWS::Lambda::Function`, `AWS::IAM::Role`, `AWS::ApiGateway::RestApi`, `AWS::ApiGateway::Method`, and `AWS::Lambda::Permission`.

## Solutions

<details>
<summary>TODO 1 -- Globals section</summary>

```yaml
Globals:
  Function:
    Runtime: provided.al2023
    Architectures:
      - arm64
    Timeout: 10
    MemorySize: 128
    Environment:
      Variables:
        LOG_LEVEL: INFO
```

</details>

<details>
<summary>TODO 2 -- HelloFunction resource</summary>

```yaml
Resources:
  HelloFunction:
    Type: AWS::Serverless::Function
    Properties:
      CodeUri: hello/
      Handler: bootstrap
      Events:
        HelloApi:
          Type: Api
          Properties:
            Path: /hello
            Method: get
    Metadata:
      BuildMethod: makefile
```

</details>

<details>
<summary>TODO 3 -- GoodbyeFunction resource</summary>

```yaml
  GoodbyeFunction:
    Type: AWS::Serverless::Function
    Properties:
      CodeUri: goodbye/
      Handler: bootstrap
      Timeout: 15
      Events:
        GoodbyeApi:
          Type: Api
          Properties:
            Path: /goodbye
            Method: get
    Metadata:
      BuildMethod: makefile
```

</details>

<details>
<summary>TODO 4 -- Outputs section</summary>

```yaml
Outputs:
  ApiEndpoint:
    Description: "API Gateway endpoint URL"
    Value: !Sub "https://${ServerlessRestApi}.execute-api.${AWS::Region}.amazonaws.com/Prod/"

  HelloFunctionArn:
    Description: "Hello Function ARN"
    Value: !GetAtt HelloFunction.Arn

  GoodbyeFunctionArn:
    Description: "Goodbye Function ARN"
    Value: !GetAtt GoodbyeFunction.Arn
```

</details>

<details>
<summary>TODO 5 -- Build, local test, and deploy commands</summary>

```bash
# a) Build
cd sam-app
sam build

# b) Local invoke with event
sam local invoke HelloFunction -e events/event.json

# c) Start local API server
sam local start-api
# In another terminal:
curl http://127.0.0.1:3000/hello?name=SAM

# d) Deploy with guided prompts
sam deploy --guided
```

</details>

## Cleanup

Delete the CloudFormation stack created by SAM:

```
sam delete --stack-name sam-lab --no-prompts
```

Or via AWS CLI:

```
aws cloudformation delete-stack --stack-name sam-lab
aws cloudformation wait stack-delete-complete --stack-name sam-lab
```

Remove local build artifacts:

```
rm -rf sam-app/.aws-sam
```

## What's Next

In **Exercise 07 -- Parameter Store and AppConfig for Runtime Configuration**, you will manage application configuration using SSM Parameter Store hierarchies and AppConfig deployment strategies, learning how to change runtime behavior without redeploying code.

## Summary

You built a SAM application with:

- **Globals section** -- shared configuration across all functions (runtime, architectures, timeout, memory, environment)
- **Two Lambda functions** -- each defined with a single `AWS::Serverless::Function` resource using Go with the makefile builder
- **Implicit API Gateway** -- auto-generated from the `Events` section of each function
- **Local development** -- `sam local invoke` for unit testing, `sam local start-api` for integration testing
- **Guided deployment** -- `sam deploy --guided` for first-time setup with saved configuration

SAM is syntactic sugar over CloudFormation. Each `AWS::Serverless::Function` expands to 5-8 raw CloudFormation resources. The DVA-C02 exam expects you to understand both the SAM shorthand and the generated resources.

## Reference

| Command | Purpose |
|---------|---------|
| `sam init` | Scaffold a new SAM project |
| `sam build` | Compile functions via Makefile targets and resolve dependencies |
| `sam local invoke` | Run a function locally in Docker |
| `sam local start-api` | Start a local API Gateway emulator |
| `sam deploy --guided` | Deploy with interactive prompts |
| `sam delete` | Delete the deployed stack |

## Additional Resources

- [SAM Developer Guide](https://docs.aws.amazon.com/serverless-application-model/latest/developerguide/what-is-sam.html)
- [SAM Template Specification](https://docs.aws.amazon.com/serverless-application-model/latest/developerguide/sam-specification.html)
- [SAM CLI Reference](https://docs.aws.amazon.com/serverless-application-model/latest/developerguide/serverless-sam-cli-command-reference.html)
- [AWS::Serverless Transform](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/transform-aws-serverless.html)
- [SAM Local Testing](https://docs.aws.amazon.com/serverless-application-model/latest/developerguide/serverless-sam-cli-using-invoke.html)
- [Building Go Lambda Functions with SAM](https://docs.aws.amazon.com/serverless-application-model/latest/developerguide/building-custom-runtimes.html)
- [DVA-C02 Exam Guide](https://aws.amazon.com/certification/certified-developer-associate/)

# 16. Multi-Service Debugging Challenge

<!--
difficulty: insane
concepts: [debugging, cloudwatch-logs, xray-traces, iam-troubleshooting, lambda-errors, dynamodb-throttling, sqs-visibility-timeout, cognito-tokens]
tools: [terraform, aws-cli]
estimated_time: 90m
bloom_level: create
prerequisites: [dva-01 through dva-12]
aws_cost: ~$0.03/hr
-->

## Prerequisites

| Prerequisite | Why |
|---|---|
| Exercise 1: Lambda Environment, Layers, Configuration | Understanding Lambda timeout, memory, and environment variable behavior |
| Exercise 2: API Gateway REST vs HTTP, Validation | API Gateway proxy integration response format requirements |
| Exercise 3: DynamoDB Developer SDK Operations | IAM permissions for specific DynamoDB operations, throughput behavior |
| Exercise 4: Cognito User Pool API Authorization | Cognito authorizer configuration and token validation |
| Exercise 5: Lambda Error Handling, Retry, DLQ | Lambda error types, retry behavior, timeout implications |
| Exercise 8: SQS Lambda Concurrency Throttling | SQS visibility timeout relationship with Lambda timeout |
| Exercise 10: X-Ray SDK Instrumentation | X-Ray sampling rules, active tracing configuration |
| Exercise 12: DynamoDB Streams Lambda Trigger Patterns | Event source mapping configuration |

## The Scenario

You have just joined a team that deployed a serverless application two weeks ago. The application was built by a contractor who has since left the company. The Terraform deployment succeeds without errors, but the application does not work correctly. Users report intermittent 502 errors from the API, missing data, and some requests that seem to process twice. The previous team lead said "we tried to set up X-Ray but no traces ever appeared."

The application is a simple order lookup service: an API Gateway with a Cognito authorizer receives GET requests, a Lambda function queries DynamoDB, and a separate SQS-triggered Lambda processes background tasks. The Terraform code has 8 intentional bugs. Each bug produces a different symptom that you must diagnose using CloudWatch Logs, X-Ray traces (where available), and AWS CLI queries -- then fix in the Terraform code.

Your manager has given you one rule: diagnose before you fix. For each bug, you must first identify the symptom using observability tools, then explain the root cause, and only then modify the Terraform code. You may not read the Lambda source code until you have formed a hypothesis from the logs and metrics. This mirrors real-world incident response where the first instinct to "just read the code" wastes time when the bug is in the infrastructure configuration, not the application logic.

## Constraints

1. Deploy the provided Terraform configuration as-is before making any changes. The deployment must succeed (it will -- the bugs are runtime issues, not syntax errors).
2. For each bug, document your diagnosis process: what symptom you observed, which CLI command or log entry revealed it, and what the root cause is.
3. Fix bugs one at a time. After each fix, run `terraform apply` and verify the specific symptom is resolved before moving to the next bug.
4. You may NOT read the Lambda function source code until you have identified at least the symptom and a hypothesis for each bug from CloudWatch Logs, X-Ray, or AWS CLI output.
5. Do not use the AWS Console. All diagnosis and fixes must use the AWS CLI and Terraform.
6. Keep a log of the order in which you discovered and fixed each bug. Some bugs mask others -- the order matters.

## The 8 Bugs

The Terraform configuration contains these intentional misconfigurations. They are listed so you know how many to find, but no hints are given about where they are or how to fix them.

1. **Lambda timeout too short** -- 3s timeout, downstream calls take 5s. Function killed mid-execution.
2. **Missing IAM permission** -- has `dynamodb:GetItem` but not `dynamodb:Query`. GSI query fails with AccessDeniedException.
3. **DynamoDB throughput too low** -- 1 RCU/1 WCU. Immediate `ProvisionedThroughputExceededException`.
4. **SQS visibility timeout < Lambda timeout** -- 30s visibility, 60s Lambda timeout. Messages reprocessed.
5. **Lambda response format wrong** -- missing `statusCode` field. API GW returns 502.
6. **X-Ray sampling at zero** -- `fixed_rate = 0`, `reservoir_size = 0`. No traces captured.
7. **Wrong environment variable** -- `TABLE_NAME` points to `orders-table`, table is named `orders`.
8. **Cognito authorizer misconfigured** -- wrong `provider_arns` or wrong user pool reference.

## Success Criteria

- All 8 bugs identified and fixed in Terraform
- GET request with valid Cognito token returns 200 with order data
- X-Ray service map shows API Gateway, Lambda, and DynamoDB
- SQS messages processed exactly once; no timeouts, no AccessDenied, no throttling
- `terraform plan` shows no changes after all fixes

## Verification Commands

```bash
terraform init && terraform apply -auto-approve

# Authenticate
USER_POOL_ID=$(terraform output -raw user_pool_id)
CLIENT_ID=$(terraform output -raw client_id)
aws cognito-idp admin-create-user --user-pool-id "$USER_POOL_ID" \
  --username debuguser --temporary-password 'TempPass1!' --message-action SUPPRESS
aws cognito-idp admin-set-user-password --user-pool-id "$USER_POOL_ID" \
  --username debuguser --password 'DebugPass1!' --permanent
TOKEN=$(aws cognito-idp initiate-auth --auth-flow USER_PASSWORD_AUTH \
  --client-id "$CLIENT_ID" --auth-parameters USERNAME=debuguser,PASSWORD='DebugPass1!' \
  --query "AuthenticationResult.IdToken" --output text)

# Test API (will fail initially)
API_URL=$(terraform output -raw api_url)
curl -s -X GET "$API_URL/orders" -H "Authorization: $TOKEN" -w "\n%{http_code}\n"

# Diagnosis commands
LOG_GROUP="/aws/lambda/$(terraform output -raw api_function_name)"
aws logs filter-log-events --log-group-name "$LOG_GROUP" --filter-pattern "ERROR" \
  --query "events[*].message" --output text
aws logs filter-log-events --log-group-name "$LOG_GROUP" --filter-pattern "Task timed out" \
  --query "events[*].message" --output text
aws logs filter-log-events --log-group-name "$LOG_GROUP" --filter-pattern "AccessDeniedException" \
  --query "events[*].message" --output text
aws logs filter-log-events --log-group-name "$LOG_GROUP" --filter-pattern "ResourceNotFoundException" \
  --query "events[*].message" --output text
aws xray get-trace-summaries --start-time $(date -u -v-1H +%s) --end-time $(date -u +%s) \
  --query "TraceSummaries | length(@)"
aws sqs get-queue-attributes --queue-url $(terraform output -raw queue_url) \
  --attribute-names VisibilityTimeout --output text
aws lambda get-function-configuration --function-name $(terraform output -raw worker_function_name) \
  --query "Timeout" --output text

# After all fixes
curl -s -X GET "$API_URL/orders" -H "Authorization: $TOKEN" | jq .
aws xray get-service-graph --start-time $(date -u -v-10M +%s) --end-time $(date -u +%s) \
  --query "Services[*].Name" --output table
terraform plan
```

## Cleanup

```bash
terraform destroy -auto-approve
terraform state list
```

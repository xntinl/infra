# 15. Serverless CRUD API Full Stack

<!--
difficulty: insane
concepts: [serverless-architecture, cognito-auth, api-gateway-caching, lambda-crud, dynamodb-gsi, sam-deployment, codepipeline-cicd, xray-observability]
tools: [terraform, sam-cli, aws-cli]
estimated_time: 120m
bloom_level: create
prerequisites: [dva-01 through dva-13]
aws_cost: ~$0.05/hr
-->

## Prerequisites

| Prerequisite | Why |
|---|---|
| Exercise 1: Lambda Environment, Layers, Configuration | Lambda packaging, environment variables, layers |
| Exercise 2: API Gateway REST vs HTTP, Validation | REST API with request models and validation |
| Exercise 3: DynamoDB Developer SDK Operations | Table design, GSIs, query vs scan, conditional writes |
| Exercise 4: Cognito User Pool API Authorization | User pool setup, JWT tokens, API Gateway authorizer |
| Exercise 5: Lambda Error Handling, Retry, DLQ | Error handling patterns for CRUD operations |
| Exercise 6: SAM Local Development and Packaging | SAM template authoring and deployment |
| Exercise 7: Parameter Store and AppConfig | Runtime configuration for feature flags |
| Exercise 8: SQS Lambda Concurrency Throttling | Understanding concurrency limits for write-heavy workloads |
| Exercise 9: API Gateway Caching, Throttling, Usage Plans | API key management, caching, rate limiting |
| Exercise 10: X-Ray SDK Instrumentation | End-to-end tracing for the full API |
| Exercise 12: DynamoDB Streams Lambda Trigger Patterns | Change data capture for audit trail |
| Exercise 13: CodePipeline CodeBuild Lambda Deploy | CI/CD pipeline with traffic shifting |

## The Scenario

Your team is building a task management API for an internal productivity tool used by 500 employees. The API must support full CRUD operations on tasks, with each user only able to access their own tasks. Tasks have statuses (open, in_progress, done) and priorities (low, medium, high, critical), and users need to query tasks by status and by due date. The product owner requires an audit trail of every task change for compliance reporting.

The API must be production-ready from launch. Authentication is handled by Cognito -- each request must carry a valid JWT token, and the Lambda functions extract the user ID from the token claims to enforce data isolation. The API must support caching for GET endpoints to reduce DynamoDB read costs during peak hours when users refresh their dashboards every 30 seconds. A usage plan with API keys controls access for the internal mobile app and the web frontend independently, each with different throttle limits. The entire deployment pipeline -- from code change to production traffic -- must be automated through CodePipeline with CodeDeploy traffic shifting, so that a bad deployment can automatically roll back before affecting all users.

Observability covers the full stack: X-Ray traces from API Gateway through every Lambda to DynamoDB, structured logs with request IDs, and a CloudWatch dashboard showing p99 latency, error rates, and DynamoDB consumed capacity. The dashboard is not decorative -- the on-call engineer uses it during incidents, so it must surface the metrics that actually matter for diagnosis.

## Constraints

1. All infrastructure must be defined in Terraform, except the Lambda functions themselves which must use a SAM template (`template.yaml`) for packaging and local testing with `sam local invoke`.
2. Cognito User Pool with at least two app clients (web, mobile) -- each with different callback URLs and token validity settings.
3. API Gateway REST API with a Cognito authorizer on all CRUD endpoints. No endpoint may be publicly accessible without a valid JWT.
4. API Gateway caching enabled on GET endpoints only (TTL 60 seconds), with cache key parameters including the authenticated user ID to prevent cross-user cache pollution.
5. DynamoDB table with a composite primary key (`user_id` partition key, `task_id` sort key) and a GSI for querying tasks by status (`user_id` partition key, `status` sort key).
6. A second GSI for querying tasks by due date (`user_id` partition key, `due_date` sort key) to support "show me all tasks due this week" queries.
7. DynamoDB Streams enabled with `NEW_AND_OLD_IMAGES` for an audit trail Lambda that writes every change to a separate audit table.
8. CodePipeline with S3 source, CodeBuild for SAM packaging, and CodeDeploy with `LambdaCanary10Percent5Minutes` traffic shifting on the CRUD Lambda functions.
9. Usage plan with two API keys: `web-client` (100 req/s burst, 50 req/s rate) and `mobile-client` (50 req/s burst, 25 req/s rate).
10. CloudWatch dashboard with widgets for: Lambda p99 duration, Lambda error count, API Gateway 4xx/5xx rates, DynamoDB consumed read/write capacity, and DynamoDB throttle events.

## Success Criteria

- Cognito authentication end-to-end: create user, get JWT, call API
- `POST /tasks` creates a task; `GET /tasks` returns only the authenticated user's tasks
- `GET /tasks?status=open` uses the GSI (not a scan); `PUT` and `DELETE` verify ownership via JWT `user_id`
- Repeated `GET /tasks` within 60 seconds returns cached responses; requests without JWT get 401; throttle excess gets 429
- Every task mutation produces an audit record in the audit table via DynamoDB Streams
- CodePipeline deploys with canary traffic shifting; CloudWatch dashboard shows populated widgets
- X-Ray service map shows API Gateway through Lambda to DynamoDB; `sam local invoke` works locally
- `terraform plan` shows no changes after full deployment

## Verification Commands

```bash
# Authenticate
USER_POOL_ID=$(terraform output -raw user_pool_id)
CLIENT_ID=$(terraform output -raw web_client_id)
aws cognito-idp admin-create-user --user-pool-id "$USER_POOL_ID" \
  --username testuser --temporary-password 'TempPass1!' --message-action SUPPRESS
aws cognito-idp admin-set-user-password --user-pool-id "$USER_POOL_ID" \
  --username testuser --password 'TestPass1!' --permanent
TOKEN=$(aws cognito-idp initiate-auth --auth-flow USER_PASSWORD_AUTH \
  --client-id "$CLIENT_ID" --auth-parameters USERNAME=testuser,PASSWORD='TestPass1!' \
  --query "AuthenticationResult.IdToken" --output text)

API_URL=$(terraform output -raw api_url)
API_KEY=$(terraform output -raw web_api_key)

# CRUD operations
curl -s -X POST "$API_URL/tasks" -H "Authorization: $TOKEN" -H "x-api-key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"title":"Ship feature","status":"open","priority":"high","due_date":"2026-03-15"}' | jq .
curl -s "$API_URL/tasks" -H "Authorization: $TOKEN" -H "x-api-key: $API_KEY" | jq .
curl -s "$API_URL/tasks?status=open" -H "Authorization: $TOKEN" -H "x-api-key: $API_KEY" | jq .

# Auth and caching checks
curl -s -o /dev/null -w "%{http_code}\n" "$API_URL/tasks"
aws dynamodb scan --table-name $(terraform output -raw audit_table) --query "Count" --output text
aws xray get-service-graph --start-time $(date -u -v-10M +%s) --end-time $(date -u +%s) \
  --query "Services[*].Name" --output table
aws cloudwatch get-dashboard --dashboard-name $(terraform output -raw dashboard_name) \
  --query "DashboardName" --output text
terraform plan
```

## Cleanup

```bash
aws s3 rm s3://$(terraform output -raw source_bucket) --recursive
aws s3 rm s3://$(terraform output -raw artifacts_bucket) --recursive
aws cognito-idp admin-delete-user --user-pool-id $(terraform output -raw user_pool_id) --username testuser
terraform destroy -auto-approve
```

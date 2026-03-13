# 25. Lambda@Edge and CloudFront Functions

<!--
difficulty: advanced
concepts: [lambda-at-edge, cloudfront-functions, viewer-request, viewer-response, origin-request, origin-response, edge-computing, ab-testing]
tools: [terraform, aws-cli]
estimated_time: 55m
bloom_level: evaluate
prerequisites: [none]
aws_cost: ~$0.03/hr
-->

> **AWS Cost Warning:** This exercise creates a CloudFront distribution, an S3 origin bucket, a CloudFront Function, and a Lambda@Edge function. CloudFront costs approximately $0.03/hr for light testing traffic. Remember to run `terraform destroy` when finished. Note: CloudFront distributions can take 15-20 minutes to create and delete.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally
- Basic understanding of CloudFront distributions and caching

## Learning Objectives

After completing this exercise, you will be able to:

- **Evaluate** when to use CloudFront Functions versus Lambda@Edge based on execution limits, supported triggers, and cost
- **Design** a URL rewrite strategy using CloudFront Functions at the viewer request stage
- **Implement** an A/B testing solution using Lambda@Edge at the origin request stage to route users to different origin paths
- **Analyze** the four CloudFront event trigger points (viewer request, viewer response, origin request, origin response) and which compute option supports each
- **Differentiate** between the runtime constraints of CloudFront Functions (JavaScript only, 1ms max, no network, 2MB max) and Lambda@Edge (Node.js/Python, 5s viewer/30s origin, network access, 50MB)

## Why This Matters

CloudFront operates at 400+ edge locations worldwide. Running code at the edge -- before the request reaches your origin -- reduces latency and offloads work from your backend. The DVA-C02 exam tests the distinction between two edge compute options:

**CloudFront Functions**: Lightweight JavaScript-only functions that run at every edge location. They execute in under 1 millisecond, cannot make network calls, and have a 2 MB total package limit. Use them for simple transformations: URL rewrites, header manipulation, redirects, cache key normalization. They support only viewer request and viewer response triggers. Cost: ~$0.000001 per invocation (1/6 the cost of Lambda@Edge).

**Lambda@Edge**: Full Lambda functions (Node.js or Python only, not Go) that run at regional edge caches (not all 400+ locations). They support all four trigger points, can make network calls, and have up to 30 seconds execution time for origin triggers. Use them for complex logic: A/B testing with cookies, authentication, dynamic origin selection, image transformation. Cost: ~$0.0000006/GB-s plus per-request charges.

The exam frequently asks: "A developer needs to modify the Host header on viewer requests. Which is the most cost-effective solution?" The answer is CloudFront Functions, because header manipulation is a simple transformation that does not require network access.

Note: Lambda@Edge currently supports only Node.js and Python runtimes, not Go. For this exercise, the Lambda@Edge function uses Node.js while the test origin function uses Go with `provided.al2023`.

## The Challenge

Build a CloudFront distribution with two edge compute layers:

1. A **CloudFront Function** on viewer request that normalizes URLs (removes trailing slashes, lowercases paths)
2. A **Lambda@Edge function** on origin request that implements A/B testing by routing users to different S3 prefixes based on a cookie

### Requirements

| Requirement | Description |
|---|---|
| CloudFront Function | URL normalization on viewer request: remove trailing slashes, lowercase paths |
| Lambda@Edge | A/B testing on origin request: check cookie, route to /variant-a/ or /variant-b/ prefix |
| S3 Origin | Static files under /variant-a/ and /variant-b/ paths |
| Cookie Logic | No cookie = random assignment + Set-Cookie response; existing cookie = honor it |
| Comparison Table | Document when to use CF Functions vs Lambda@Edge |

### Architecture

```
  Client Request
       |
       v
  +-------------------+
  | CloudFront Edge   |
  | (Viewer Request)  |
  |                   |
  | CloudFront Func:  |
  | - lowercase path  |
  | - remove trailing/|
  +-------------------+
       |
       v
  +-------------------+
  | Regional Edge     |
  | (Origin Request)  |
  |                   |
  | Lambda@Edge:      |
  | - read AB cookie  |
  | - set S3 prefix   |
  | - /variant-a/ or  |
  |   /variant-b/     |
  +-------------------+
       |
       v
  +-------------------+
  | S3 Origin         |
  | /variant-a/...    |
  | /variant-b/...    |
  +-------------------+
```

### Comparison Table

| Feature | CloudFront Functions | Lambda@Edge |
|---------|---------------------|-------------|
| **Runtime** | JavaScript (ECMAScript 5.1) | Node.js, Python |
| **Execution location** | All 400+ edge locations | ~13 regional edge caches |
| **Max execution time** | 1 ms | 5s (viewer) / 30s (origin) |
| **Max memory** | 2 MB | 128 MB (viewer) / 10,240 MB (origin) |
| **Network access** | No | Yes |
| **File system access** | No | Yes (read-only /tmp, 512 MB) |
| **Max package size** | 10 KB | 1 MB (viewer) / 50 MB (origin) |
| **Triggers** | Viewer request, viewer response | All four trigger points |
| **Pricing** | ~$0.10 per million | ~$0.60 per million + duration |
| **Use cases** | URL rewrite, redirects, headers, cache key | Auth, A/B test, dynamic origin, image resize |

## Hints

<details>
<summary>Hint 1: CloudFront Function for URL normalization</summary>

CloudFront Functions use a specific event structure. The function receives the event and must return the (possibly modified) request object:

```javascript
function handler(event) {
    var request = event.request;
    var uri = request.uri;

    // Lowercase the URI path
    uri = uri.toLowerCase();

    // Remove trailing slash (except for root /)
    if (uri.length > 1 && uri.endsWith('/')) {
        uri = uri.slice(0, -1);
    }

    // Add .html extension if no extension present
    if (!uri.includes('.') && uri !== '/') {
        uri = uri + '/index.html';
    }

    request.uri = uri;
    return request;
}
```

In Terraform:

```hcl
resource "aws_cloudfront_function" "url_normalize" {
  name    = "url-normalize"
  runtime = "cloudfront-js-2.0"
  code    = file("${path.module}/cf-function.js")
  publish = true
}
```

The `cloudfront-js-2.0` runtime supports ECMAScript 5.1 features. The function must be published before it can be associated with a distribution.

</details>

<details>
<summary>Hint 2: Lambda@Edge function for A/B testing</summary>

Lambda@Edge functions must be deployed to **us-east-1** (CloudFront replicates them globally). They use Node.js or Python (not Go):

```javascript
// edge-function/index.mjs
export const handler = async (event) => {
    const request = event.Records[0].cf.request;
    const headers = request.headers;

    // Check for existing A/B test cookie
    let variant = null;
    if (headers.cookie) {
        for (const cookieHeader of headers.cookie) {
            const match = cookieHeader.value.match(/ab-test=([^;]+)/);
            if (match) {
                variant = match[1];
                break;
            }
        }
    }

    // Assign variant if no cookie
    if (!variant) {
        variant = Math.random() < 0.5 ? 'variant-a' : 'variant-b';

        // Add cookie header so the response sets the cookie
        // (we'll add a viewer response function to set Set-Cookie)
        headers['x-ab-variant'] = [{ key: 'X-AB-Variant', value: variant }];
    }

    // Modify the origin path to route to the correct variant
    request.origin.s3.path = '/' + variant;

    return request;
};
```

Key constraints:
- Must be in us-east-1
- Cannot use environment variables
- Cannot use Lambda layers
- Viewer triggers: max 128 MB, 5s timeout
- Origin triggers: max 10,240 MB, 30s timeout
- Must use `publish = true` (Lambda@Edge uses versions, not `$LATEST`)

</details>

<details>
<summary>Hint 3: CloudFront distribution with function associations</summary>

The CloudFront distribution connects both edge compute functions:

```hcl
resource "aws_cloudfront_distribution" "this" {
  origin {
    domain_name = aws_s3_bucket.origin.bucket_regional_domain_name
    origin_id   = "s3-origin"

    s3_origin_config {
      origin_access_identity = aws_cloudfront_origin_access_identity.this.cloudfront_access_identity_path
    }
  }

  enabled             = true
  default_root_object = "index.html"

  default_cache_behavior {
    allowed_methods        = ["GET", "HEAD"]
    cached_methods         = ["GET", "HEAD"]
    target_origin_id       = "s3-origin"
    viewer_protocol_policy = "redirect-to-https"

    forwarded_values {
      query_string = false
      cookies { forward = "none" }
    }

    # CloudFront Function on viewer request
    function_association {
      event_type   = "viewer-request"
      function_arn = aws_cloudfront_function.url_normalize.arn
    }

    # Lambda@Edge on origin request
    lambda_function_association {
      event_type   = "origin-request"
      lambda_arn   = aws_lambda_function.edge.qualified_arn
      include_body = false
    }
  }

  restrictions {
    geo_restriction { restriction_type = "none" }
  }

  viewer_certificate {
    cloudfront_default_certificate = true
  }
}
```

Note the different blocks: `function_association` for CloudFront Functions, `lambda_function_association` for Lambda@Edge. Lambda@Edge requires `qualified_arn` (includes version number), not the unqualified ARN.

</details>

<details>
<summary>Hint 4: S3 origin with variant content</summary>

Create test content for both variants:

```bash
# Create variant content
echo '<html><body><h1>Variant A</h1></body></html>' > variant-a.html
echo '<html><body><h1>Variant B</h1></body></html>' > variant-b.html

# Upload to S3 with correct prefixes
aws s3 cp variant-a.html s3://$(terraform output -raw origin_bucket)/variant-a/index.html --content-type text/html
aws s3 cp variant-b.html s3://$(terraform output -raw origin_bucket)/variant-b/index.html --content-type text/html
```

</details>

## Spot the Bug

A developer creates a Lambda@Edge function using the Go runtime:

```hcl
resource "aws_lambda_function" "edge" {
  provider         = aws.us_east_1
  function_name    = "edge-ab-testing"
  filename         = data.archive_file.edge.output_path
  handler          = "bootstrap"
  runtime          = "provided.al2023"    # <-- Go custom runtime
  role             = aws_iam_role.edge.arn
  publish          = true
}

resource "aws_cloudfront_distribution" "this" {
  # ...
  default_cache_behavior {
    lambda_function_association {
      event_type = "origin-request"
      lambda_arn = aws_lambda_function.edge.qualified_arn
    }
  }
}
```

<details>
<summary>Explain the bug</summary>

Lambda@Edge does **not** support custom runtimes (`provided.al2023`). It only supports Node.js and Python runtimes. The `terraform apply` will succeed (the Lambda function is valid), but associating it with the CloudFront distribution will fail with `InvalidLambdaFunctionAssociation: The function's runtime is not supported`.

Supported Lambda@Edge runtimes:
- `nodejs18.x`, `nodejs20.x`
- `python3.9`, `python3.10`, `python3.11`, `python3.12`

The fix -- use Node.js for the Lambda@Edge function:

```hcl
resource "aws_lambda_function" "edge" {
  provider         = aws.us_east_1
  function_name    = "edge-ab-testing"
  filename         = data.archive_file.edge.output_path
  handler          = "index.handler"
  runtime          = "nodejs20.x"         # Supported runtime
  role             = aws_iam_role.edge.arn
  publish          = true
}
```

If you need Go at the edge, use CloudFront Functions (JavaScript only) for simple logic, or run Go functions behind the origin (standard Lambda with API Gateway) and use Lambda@Edge only for routing decisions.

</details>

## Verify What You Learned

```bash
# Verify the CloudFront Function exists
aws cloudfront list-functions \
  --query "FunctionList.Items[?Name=='url-normalize'].{Name:Name,Status:Status,Runtime:FunctionConfig.Runtime}" \
  --output table
```

Expected: function with status `UNASSOCIATED` or `DEPLOYED` and runtime `cloudfront-js-2.0`.

```bash
# Verify the CloudFront distribution
aws cloudfront list-distributions \
  --query "DistributionList.Items[0].{Id:Id,Domain:DomainName,Status:Status}" \
  --output table
```

Expected: a distribution with status `Deployed`.

```bash
# Test URL normalization (trailing slash removal)
DOMAIN=$(aws cloudfront list-distributions --query "DistributionList.Items[0].DomainName" --output text)
curl -s -o /dev/null -w "%{redirect_url}\n" "https://$DOMAIN/Test/Path/"
```

```bash
# Test A/B testing (observe Set-Cookie header)
curl -s -I "https://$DOMAIN/" | grep -i "set-cookie\|x-ab-variant"
```

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

Destroy all resources:

```bash
# Empty S3 bucket first
aws s3 rm s3://$(terraform output -raw origin_bucket) --recursive

terraform destroy -auto-approve
```

Note: CloudFront distribution deletion can take 15-20 minutes. Lambda@Edge replicas are cleaned up asynchronously and may take several hours. If `terraform destroy` times out on the Lambda@Edge function, wait and retry.

Verify:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

You implemented edge computing with CloudFront Functions and Lambda@Edge. In the next exercise, you will explore **Lambda SnapStart** to reduce cold start latency by restoring from a pre-initialized snapshot.

## Summary

- **CloudFront Functions**: JavaScript-only, <1ms, 10KB limit, viewer triggers only, cheapest ($0.10/million), no network access
- **Lambda@Edge**: Node.js/Python only (no Go), up to 30s for origin triggers, all four trigger points, full network access
- CloudFront Functions run at **all 400+ edge locations**; Lambda@Edge runs at **~13 regional edge caches**
- Lambda@Edge must be deployed in **us-east-1** and uses `publish = true` (version-based, not `$LATEST`)
- Four trigger points: **viewer request** (before cache), **origin request** (cache miss), **origin response** (from origin), **viewer response** (before client)
- Use CloudFront Functions for **simple, high-volume** operations (URL rewrite, redirects, header manipulation)
- Use Lambda@Edge for **complex logic** requiring network access (auth, A/B testing, dynamic origin selection)
- Lambda@Edge does **not** support environment variables, Lambda layers, VPC configuration, or custom runtimes

## Reference

- [CloudFront Functions](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/cloudfront-functions.html)
- [Lambda@Edge](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/lambda-at-the-edge.html)
- [CloudFront Events](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/lambda-cloudfront-trigger-events.html)
- [Terraform aws_cloudfront_function](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudfront_function)
- [Terraform aws_cloudfront_distribution](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudfront_distribution)

## Additional Resources

- [Choosing Between CloudFront Functions and Lambda@Edge](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/edge-functions.html) -- official decision guide
- [CloudFront Functions Runtime](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/functions-javascript-runtime-features.html) -- supported JavaScript features and limitations
- [Lambda@Edge Restrictions](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/edge-functions-restrictions.html) -- complete list of limitations for Lambda@Edge
- [A/B Testing with Lambda@Edge](https://aws.amazon.com/blogs/networking-and-content-delivery/dynamically-route-viewer-requests-to-any-origin-using-lambdaedge/) -- AWS blog with detailed A/B testing implementation

<details>
<summary>Full Solution</summary>

### File Structure

```
25-lambda-at-edge-cloudfront-functions/
├── main.tf
├── cf-function.js
└── edge-function/
    └── index.mjs
```

### `cf-function.js`

```javascript
function handler(event) {
    var request = event.request;
    var uri = request.uri;

    // Lowercase the URI path
    uri = uri.toLowerCase();

    // Remove trailing slash (except for root /)
    if (uri.length > 1 && uri.endsWith('/')) {
        uri = uri.slice(0, -1);
    }

    // Add /index.html if no file extension
    if (!uri.includes('.') && uri !== '/') {
        uri = uri + '/index.html';
    }

    if (uri === '/') {
        uri = '/index.html';
    }

    request.uri = uri;
    return request;
}

```

### `edge-function/index.mjs`

```javascript
export const handler = async (event) => {
    const request = event.Records[0].cf.request;
    const headers = request.headers;

    let variant = null;

    if (headers.cookie) {
        for (const cookieHeader of headers.cookie) {
            const match = cookieHeader.value.match(/ab-test=([^;]+)/);
            if (match) {
                variant = match[1];
                break;
            }
        }
    }

    if (!variant) {
        variant = Math.random() < 0.5 ? 'variant-a' : 'variant-b';
        headers['x-ab-variant'] = [{ key: 'X-AB-Variant', value: variant }];
    }

    request.origin.s3.path = '/' + variant;
    return request;
};
```

### `providers.tf`

```hcl
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    archive = {
      source  = "hashicorp/archive"
      version = "~> 2.0"
    }
  }
}

provider "aws" {
  region = var.region
}
```

### `variables.tf`

```hcl
variable "region" {
  description = "AWS region (must be us-east-1 for Lambda@Edge)"
  type        = string
  default     = "us-east-1"
}

variable "project_name" {
  description = "Project name for resource naming"
  type        = string
  default     = "cf-edge-demo"
}
```

### `storage.tf`

```hcl
data "aws_caller_identity" "current" {}

resource "aws_s3_bucket" "origin" {
  bucket        = "${var.project_name}-${data.aws_caller_identity.current.account_id}"
  force_destroy = true
}

resource "aws_cloudfront_origin_access_identity" "this" {
  comment = "OAI for edge demo"
}

data "aws_iam_policy_document" "s3_policy" {
  statement {
    actions   = ["s3:GetObject"]
    resources = ["${aws_s3_bucket.origin.arn}/*"]
    principals {
      type        = "AWS"
      identifiers = [aws_cloudfront_origin_access_identity.this.iam_arn]
    }
  }
}

resource "aws_s3_bucket_policy" "origin" {
  bucket = aws_s3_bucket.origin.id
  policy = data.aws_iam_policy_document.s3_policy.json
}
```

### `iam.tf`

```hcl
data "aws_iam_policy_document" "edge_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com", "edgelambda.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "edge" {
  name               = "${var.project_name}-role"
  assume_role_policy = data.aws_iam_policy_document.edge_assume.json
}

resource "aws_iam_role_policy_attachment" "edge_basic" {
  role       = aws_iam_role.edge.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}
```

### `lambda.tf`

```hcl
data "archive_file" "edge_function" {
  type        = "zip"
  source_file = "${path.module}/edge-function/index.mjs"
  output_path = "${path.module}/build/edge-function.zip"
}

resource "aws_lambda_function" "edge" {
  function_name    = "${var.project_name}-ab-testing"
  filename         = data.archive_file.edge_function.output_path
  source_code_hash = data.archive_file.edge_function.output_base64sha256
  handler          = "index.handler"
  runtime          = "nodejs20.x"
  role             = aws_iam_role.edge.arn
  publish          = true
  timeout          = 5
  memory_size      = 128
}
```

### `dns.tf`

```hcl
resource "aws_cloudfront_function" "url_normalize" {
  name    = "url-normalize"
  runtime = "cloudfront-js-2.0"
  code    = file("${path.module}/cf-function.js")
  publish = true
}

resource "aws_cloudfront_distribution" "this" {
  origin {
    domain_name = aws_s3_bucket.origin.bucket_regional_domain_name
    origin_id   = "s3-origin"

    s3_origin_config {
      origin_access_identity = aws_cloudfront_origin_access_identity.this.cloudfront_access_identity_path
    }
  }

  enabled             = true
  default_root_object = "index.html"

  default_cache_behavior {
    allowed_methods        = ["GET", "HEAD"]
    cached_methods         = ["GET", "HEAD"]
    target_origin_id       = "s3-origin"
    viewer_protocol_policy = "redirect-to-https"

    forwarded_values {
      query_string = false
      cookies { forward = "none" }
    }

    function_association {
      event_type   = "viewer-request"
      function_arn = aws_cloudfront_function.url_normalize.arn
    }

    lambda_function_association {
      event_type   = "origin-request"
      lambda_arn   = aws_lambda_function.edge.qualified_arn
      include_body = false
    }
  }

  restrictions {
    geo_restriction { restriction_type = "none" }
  }

  viewer_certificate {
    cloudfront_default_certificate = true
  }
}
```

### `outputs.tf`

```hcl
output "distribution_domain" { value = aws_cloudfront_distribution.this.domain_name }
output "origin_bucket"       { value = aws_s3_bucket.origin.bucket }
```

</details>

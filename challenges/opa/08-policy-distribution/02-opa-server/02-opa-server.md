# Exercise 08-02: Running OPA as a REST Server

## Prerequisites

- OPA CLI installed (`opa version`)
- `curl` and `jq` installed
- Completed exercise 08-01 (Bundles)

## Learning Objectives

After completing this exercise, you will be able to:

- Run OPA as a persistent HTTP server and query it with `curl`
- Upload and remove policies dynamically through the REST API
- Use path-based queries to retrieve specific rules from a policy package

## Why Run OPA as a Server

In the previous exercise you packaged policies into bundles and evaluated them locally. That is fine for CI pipelines and CLI checks, but in a microservice architecture you need something different: a running daemon that your services can query over HTTP. Your application asks "can this user do this?" and OPA responds in milliseconds, without your app needing to know anything about Rego.

The command `opa run --server` turns OPA into exactly that -- a REST API for policy decisions. It is the most common OPA deployment pattern in production.

## The OPA REST API

When OPA starts as a server (by default on port 8181), it exposes these endpoints:

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/v1/data/{path}` | GET/POST | Query rules. POST lets you send `input` in the body |
| `/v1/policies` | GET | List loaded policies |
| `/v1/policies/{id}` | PUT | Upload or update a policy |
| `/v1/policies/{id}` | DELETE | Remove a policy |
| `/health` | GET | Health check |

When you `POST /v1/data/my_package/my_rule`, OPA evaluates `data.my_package.my_rule` using the `input` from the request body. The result comes back as `{"result": ...}`.

You can load policies in two ways: pass them as arguments at startup (`opa run --server policy.rego`), or upload them dynamically via `PUT /v1/policies/{id}`. The first approach is standard in production (policies baked in or pulled from bundles). The second is handy during development and testing.

## Step 1: Create the Policy and Start the Server

Create `authz.rego`:

```rego
package system.authz

import rego.v1

default allowed := false

# Admins can do everything
allowed if {
    input.user.role == "admin"
}

# Editors can read and write
allowed if {
    input.user.role == "editor"
    input.action in {"read", "write"}
}

# Viewers can only read
allowed if {
    input.user.role == "viewer"
    input.action == "read"
}

# Provide a human-readable reason for the decision
reason := "user is admin" if {
    input.user.role == "admin"
}

reason := "editor can read/write" if {
    input.user.role == "editor"
    input.action in {"read", "write"}
}

reason := "viewer can only read" if {
    input.user.role == "viewer"
    input.action == "read"
}

reason := "action not allowed for this role" if {
    not allowed
}
```

Start OPA with this policy preloaded:

```bash
opa run --server --addr :8181 authz.rego &
```

The `&` sends it to the background so you can keep using the terminal.

## Step 2: Query the Server

**Scenario 1 -- Admin deletes a resource (allowed):**

```bash
curl -s localhost:8181/v1/data/system/authz -d '{
  "input": {
    "user": {"name": "ana", "role": "admin"},
    "action": "delete",
    "resource": "document-123"
  }
}' | jq
```

Expected output:

```json
{
  "result": {
    "allowed": true,
    "reason": "user is admin"
  }
}
```

**Scenario 2 -- Viewer tries to write (denied):**

```bash
curl -s localhost:8181/v1/data/system/authz -d '{
  "input": {
    "user": {"name": "carlos", "role": "viewer"},
    "action": "write",
    "resource": "document-456"
  }
}' | jq
```

Expected output:

```json
{
  "result": {
    "allowed": false,
    "reason": "action not allowed for this role"
  }
}
```

Notice the only change between the two queries is the role and action. The policy, the server, and the endpoint are the same. This is the power of decoupling policy from application code.

## Step 3: Query a Specific Rule

You can drill deeper into the path to get just one rule instead of the entire package:

```bash
curl -s localhost:8181/v1/data/system/authz/allowed -d '{
  "input": {
    "user": {"name": "ana", "role": "admin"},
    "action": "delete"
  }
}' | jq
```

Expected output:

```json
{
  "result": true
}
```

This returns only the `allowed` value, not the full package with `reason`. Use this pattern when your application only needs a boolean decision.

## Step 4: List and Manage Policies

**List loaded policies:**

```bash
curl -s localhost:8181/v1/policies | jq '.result[].id'
```

Expected output:

```
"authz.rego"
```

**Upload a new policy without restarting OPA:**

Suppose you want to add a rate limiting policy on the fly:

```bash
curl -s -X PUT localhost:8181/v1/policies/ratelimit \
  -H "Content-Type: text/plain" \
  -d '
package system.ratelimit

import rego.v1

default within_limit := true

within_limit := false if {
    input.requests_last_minute > 100
}
'
```

Now query it:

```bash
curl -s localhost:8181/v1/data/system/ratelimit/within_limit -d '{
  "input": {"requests_last_minute": 150}
}' | jq
```

Expected output:

```json
{
  "result": false
}
```

The rate limit policy was loaded dynamically. No restart required.

## Step 5: Health Check

```bash
curl -s localhost:8181/health | jq
```

Expected output:

```json
{}
```

An empty body with HTTP status 200 means OPA is healthy. If you add `?bundles=true` to the URL, OPA also verifies that all configured bundles are loaded before reporting healthy -- useful for readiness probes in Kubernetes.

## A Common Mistake: Wrong Package Path in the URL

A frequent error is querying `/v1/data/authz/allowed` when the package is `system.authz`. The URL path must match the package declaration exactly: `system.authz` becomes `/v1/data/system/authz`. If OPA returns `{"result": {}}` or no result at all, double-check that the URL matches the package name.

Stop the server before moving on:

```bash
kill %1
```

## Verify What You Learned

**Command 1** -- Start OPA, query whether an editor can read, and confirm the result is `true`:

```bash
opa run --server --addr :8282 authz.rego &
sleep 1
curl -s localhost:8282/v1/data/system/authz/allowed -d '{
  "input": {"user": {"role": "editor"}, "action": "read"}
}' | jq -r '.result'
kill %1
```

Expected output:

```
true
```

**Command 2** -- Confirm that a viewer cannot delete:

```bash
opa run --server --addr :8283 authz.rego &
sleep 1
curl -s localhost:8283/v1/data/system/authz/allowed -d '{
  "input": {"user": {"role": "viewer"}, "action": "delete"}
}' | jq -r '.result'
kill %1
```

Expected output:

```
false
```

**Command 3** -- Verify the health check returns HTTP 200:

```bash
opa run --server --addr :8284 authz.rego &
sleep 1
curl -s -o /dev/null -w "%{http_code}" localhost:8284/health
echo
kill %1
```

Expected output:

```
200
```

You now know how to run OPA as a REST server, query policies over HTTP, upload policies dynamically, and monitor OPA health. In the next exercise, you will add observability by configuring decision logs.

## What's Next

Exercise 08-03 covers decision logs -- OPA's built-in audit trail that records every policy decision with full context, so you can debug access denials and satisfy compliance requirements.

## Reference

- [OPA REST API](https://www.openpolicyagent.org/docs/latest/rest-api/)
- [OPA deployments guide](https://www.openpolicyagent.org/docs/latest/deployments/)
- [Health API](https://www.openpolicyagent.org/docs/latest/rest-api/#health-api)

## Additional Resources

- [Styra Academy -- OPA fundamentals](https://academy.styra.com/)
- [OPA HTTP API authorization tutorial](https://www.openpolicyagent.org/docs/latest/http-api-authorization/)
- [OPA Playground](https://play.openpolicyagent.org/)

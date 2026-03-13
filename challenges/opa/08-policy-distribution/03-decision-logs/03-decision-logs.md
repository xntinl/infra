# Exercise 08-03: Configuring and Consuming Decision Logs

## Prerequisites

- OPA CLI installed (`opa version`)
- `curl` and `jq` installed
- Completed exercises 08-01 (Bundles) and 08-02 (OPA Server)

## Learning Objectives

After completing this exercise, you will be able to:

- Enable decision logging in OPA and interpret log entries
- Configure field masking to redact sensitive data from logs
- Understand how decision logs support production audit and debugging workflows

## Why Decision Logs Matter

Your OPA server is running, answering thousands of authorization queries per minute. Everything works. Then one day someone says "I was denied access and it should not have happened." How do you debug that without logs?

Decision logs are OPA's answer. OPA can record every decision it makes, including the input it received, the result it returned, and how long evaluation took. It is the equivalent of an audit log for your policy engine -- every query is recorded with its full context. This is not just useful for debugging; many compliance frameworks require a verifiable decision trail.

## How Decision Logs Work

Decision logs are a built-in OPA feature. Each log entry contains:

- **`input`** -- what the query received (the user's request)
- **`result`** -- what OPA returned (allow/deny, etc.)
- **`path`** -- which rule was evaluated (e.g., `system/authz/allowed`)
- **`timestamp`** -- when it happened
- **`metrics`** -- how long evaluation took (in nanoseconds)
- **`decision_id`** -- a unique UUID per decision

For local development, OPA has a `console` plugin that prints logs directly to stderr. In production you would use an HTTP plugin that ships logs to an external service (Elasticsearch, Datadog, a custom endpoint, etc.), but the structure is the same.

The configuration goes in the OPA YAML config file. Here is the console version:

```yaml
decision_logs:
  console: true
```

That is it. With that single setting, every query OPA receives gets logged to stderr in JSON format.

### Masking: Protecting Sensitive Data

Sometimes the `input` contains data you do not want in your logs -- passwords, tokens, personal information. OPA has a masking mechanism: you write a rule that defines which fields to hide, and OPA replaces them with `"**REDACTED**"` before logging.

The masking rule lives in a special package: `system.log`. OPA looks for it automatically.

An important subtlety: inside `system.log`, the rule's `input` is the entire log entry, not the original query input. So to access the user's password from the original query, you write `input.input.user.password` -- the first `input` is the log entry, the second `input` is what the user sent. The path you return in `mask` uses JSON Pointer format.

## Step 1: Create the Policy

Create `authz.rego`:

```rego
package system.authz

import rego.v1

default allowed := false

allowed if {
    input.user.role == "admin"
}

allowed if {
    input.user.role == "editor"
    input.action in {"read", "write"}
}
```

## Step 2: Create the Masking Rule

Create `mask.rego`:

```rego
package system.log

import rego.v1

# Mask the password field from decision logs
mask contains "/input/user/password" if {
    input.input.user.password
}

# Mask tokens as well
mask contains "/input/user/token" if {
    input.input.user.token
}
```

## Step 3: Configure and Start OPA

Create `config.yaml`:

```yaml
decision_logs:
  console: true
```

Start OPA, redirecting stderr to a file so you can inspect the logs afterward:

```bash
opa run --server --addr :8181 --config-file config.yaml authz.rego mask.rego 2>decision_logs.txt &
```

## Step 4: Generate Some Decisions

**Query 1 -- Admin with a password field (should be allowed, password should be masked in logs):**

```bash
curl -s localhost:8181/v1/data/system/authz/allowed -d '{
  "input": {
    "user": {"name": "ana", "role": "admin", "password": "secret123"},
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

**Query 2 -- Editor tries to delete (should be denied, token should be masked in logs):**

```bash
curl -s localhost:8181/v1/data/system/authz/allowed -d '{
  "input": {
    "user": {"name": "luis", "role": "editor", "token": "eyJhbG..."},
    "action": "delete"
  }
}' | jq
```

Expected output:

```json
{
  "result": false
}
```

## Step 5: Inspect the Decision Logs

Wait a moment for OPA to flush the logs, then look at the first entry:

```bash
cat decision_logs.txt | grep "decision_id" | head -1 | jq
```

You will see something like:

```json
{
  "decision_id": "a1b2c3d4-...",
  "input": {
    "user": {
      "name": "ana",
      "role": "admin",
      "password": "**REDACTED**"
    },
    "action": "delete"
  },
  "labels": {
    "id": "...",
    "version": "..."
  },
  "metrics": {
    "timer_rego_query_eval_ns": 54321,
    "timer_server_handler_ns": 123456
  },
  "msg": "Decision Log",
  "path": "system/authz/allowed",
  "result": true,
  "timestamp": "2026-03-07T..."
}
```

Notice that `password` appears as `"**REDACTED**"`. The masking worked. The token from the second query will also be masked in its corresponding log entry.

The `metrics` fields tell you how long evaluation took. `timer_rego_query_eval_ns` is the pure Rego evaluation time, and `timer_server_handler_ns` includes the HTTP overhead. In production these numbers help you spot slow policies.

Stop the server:

```bash
kill %1
```

## A Common Mistake: Masking Paths Do Not Match

If your masking rule references `/input/user/password` but the actual input nests the password differently (say, under `/input/credentials/password`), the field will not be masked and sensitive data will appear in your logs. Always test masking rules by inspecting actual log output, as we did above.

## Decision Logs in Production

In production you do not write logs to a text file -- you ship them to a service. The configuration looks like this:

```yaml
decision_logs:
  plugin: http_send
  http_send:
    url: https://my-log-service.example.com/v1/logs
    headers:
      Authorization: "Bearer ${TOKEN}"
    batch_size: 100
    max_delay_seconds: 5
```

OPA batches logs and sends them periodically. If the service is unreachable, OPA stores them in an internal buffer and retries.

## Verify What You Learned

**Command 1** -- Start OPA with decision logs, make a query, and verify that a log entry was generated:

```bash
opa run --server --addr :8381 --config-file config.yaml authz.rego mask.rego 2>test_logs.txt &
sleep 1
curl -s localhost:8381/v1/data/system/authz/allowed -d '{"input":{"user":{"role":"admin"},"action":"read"}}' > /dev/null
sleep 1
grep -c "decision_id" test_logs.txt
kill %1
```

Expected output:

```
1
```

**Command 2** -- Verify that masking redacts the password:

```bash
opa run --server --addr :8382 --config-file config.yaml authz.rego mask.rego 2>mask_logs.txt &
sleep 1
curl -s localhost:8382/v1/data/system/authz/allowed -d '{"input":{"user":{"role":"admin","password":"my-secret"},"action":"read"}}' > /dev/null
sleep 1
grep "REDACTED" mask_logs.txt | wc -l
kill %1
```

Expected output (at least one line containing REDACTED):

```
1
```

**Command 3** -- Verify that the decision result is logged correctly:

```bash
opa run --server --addr :8383 --config-file config.yaml authz.rego mask.rego 2>result_logs.txt &
sleep 1
curl -s localhost:8383/v1/data/system/authz/allowed -d '{"input":{"user":{"role":"viewer"},"action":"delete"}}' > /dev/null
sleep 1
cat result_logs.txt | grep "decision_id" | jq -r '.result'
kill %1
```

Expected output:

```
false
```

## Section 08 Summary

Across these three exercises you covered the full lifecycle of policy distribution:

- **Bundles** (08-01): packaging policies into versioned, distributable archives that OPA downloads automatically from any HTTP server
- **OPA Server** (08-02): running OPA as a persistent REST API that your services query for authorization decisions
- **Decision Logs** (08-03): recording every decision with full context for debugging and audit, including masking sensitive fields

Together, these form the operational foundation for running OPA in production. Bundles give you versioned delivery. The server gives your applications a query endpoint. Decision logs give you observability and compliance evidence. In the next section, you will build on this foundation with advanced patterns: performance optimization, policy composition, and compliance frameworks.

## What's Next

Section 09 covers advanced patterns. Exercise 09-01 starts with performance optimization -- how to write policies that evaluate fast enough for high-throughput production workloads.

## Reference

- [OPA Decision Logs](https://www.openpolicyagent.org/docs/latest/management-decision-logs/)
- [Decision Log Masking](https://www.openpolicyagent.org/docs/latest/management-decision-logs/#masking)
- [Decision Log Plugins](https://www.openpolicyagent.org/docs/latest/plugins/)

## Additional Resources

- [Styra Academy -- OPA fundamentals](https://academy.styra.com/)
- [OPA Management APIs overview](https://www.openpolicyagent.org/docs/latest/management-introduction/)
- [OPA contrib examples on GitHub](https://github.com/open-policy-agent/contrib)

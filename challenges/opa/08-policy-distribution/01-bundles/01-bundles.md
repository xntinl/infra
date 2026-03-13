# Exercise 08-01: Packaging Policies as OPA Bundles

## Prerequisites

- OPA CLI installed (`opa version`)
- Python 3 installed (for the HTTP server)
- Completed exercises in sections 01 through 07

## Learning Objectives

After completing this exercise, you will be able to:

- Package Rego policies and data into a distributable OPA bundle
- Inspect bundle contents and manage bundle revisions
- Serve bundles over HTTP and configure OPA to download them automatically

## Why Bundles Matter

So far, your policies have been loose `.rego` files sitting next to your code. That works fine when you have one service. But when ten services need the same authorization rules, copying `.rego` files everywhere does not scale. You need a way to package policies once and distribute them to every OPA instance automatically.

OPA bundles solve this. A bundle is a `.tar.gz` archive containing your policies, optional data files, and a manifest. You publish it to any HTTP server -- S3, GCS, an OCI registry, or even a local Python server for development -- and every OPA instance downloads it on a polling interval. Think of it as a release artifact for your policies: version 1.0, version 1.1, and so on.

## How Bundles Work

The `opa build` command takes a directory of policies and produces a bundle:

```bash
opa build -b <directory> -o bundle.tar.gz
```

The `-b` flag tells OPA to treat the directory as a bundle, resolving imports between files. The `-o` flag sets the output filename. Without `-o`, it defaults to `bundle.tar.gz`.

Inside the bundle you will find:

```
/.manifest          # metadata: revision, roots
/policy.rego        # your compiled policies
/data.json          # static data (if any)
```

The `.manifest` contains two important fields:

- **`revision`** -- a version string you can use to track which bundle is deployed
- **`roots`** -- which paths in the data tree this bundle "owns" (critical when multiple bundles feed into the same OPA instance, so each bundle controls its own slice of the tree)

## Step 1: Create a Policy and Data File

Create a directory for the bundle contents and add an authorization policy.

Create `mybundle/policy.rego`:

```rego
package authz

import rego.v1

default allow := false

# Admins can do anything
allow if {
    input.role == "admin"
}

# Editors can only read
allow if {
    input.role == "editor"
    input.action == "read"
}
```

Now add a data file with valid roles.

Create `mybundle/data.json`:

```json
{
    "authz": {
        "valid_roles": ["admin", "editor", "viewer"]
    }
}
```

## Step 2: Build the Bundle

Run `opa build` to produce the bundle archive:

```bash
opa build -b mybundle/ -o mybundle.tar.gz
```

Verify the bundle contains the expected files:

```bash
tar tzf mybundle.tar.gz
```

Expected output:

```
/policy.rego
/data.json
/.manifest
```

Inspect the manifest:

```bash
tar xzf mybundle.tar.gz .manifest -O
```

Expected output:

```json
{"revision":"","roots":[""]}
```

The revision is empty because you did not set one. Fix that by adding `--revision`:

```bash
opa build -b mybundle/ -o mybundle.tar.gz --revision "v1.0.0"
tar xzf mybundle.tar.gz .manifest -O
```

Now the manifest shows:

```json
{"revision":"v1.0.0","roots":[""]}
```

This revision string is what you would increment in CI/CD each time policies change.

## Step 3: Serve the Bundle Over HTTP

Any static HTTP server works. The fastest option for local development is Python's built-in server:

```bash
python3 -m http.server 9090 &
```

Now configure an OPA instance to download the bundle.

Create `config.yaml`:

```yaml
services:
  local:
    url: http://localhost:9090

bundles:
  authz:
    service: local
    resource: mybundle.tar.gz
    polling:
      min_delay_seconds: 10
      max_delay_seconds: 30
```

This tells OPA: "connect to the `local` service at `localhost:9090`, download `mybundle.tar.gz`, and re-check every 10 to 30 seconds for updates."

Start OPA as a server with this configuration:

```bash
opa run --server --config-file config.yaml --addr :8181
```

OPA downloads the bundle automatically on startup. Verify it works by querying the policy:

```bash
curl -s localhost:8181/v1/data/authz/allow \
  -d '{"input": {"role": "admin", "action": "delete"}}' | jq
```

Expected output:

```json
{
  "result": true
}
```

Whenever you rebuild and replace the bundle file on the HTTP server, OPA picks up the new version on the next polling cycle. In production you would use S3, GCS, or an OCI registry instead of Python's `http.server`, but the mechanism is identical.

## A Common Mistake: Forgetting the `-b` Flag

If you run `opa build` without `-b`, OPA treats each file independently and does not resolve cross-file imports. This is a frequent source of "undefined" errors when a policy in one file imports a package from another. Always use `-b` when building bundles from a directory with multiple files.

## Step 4: Evaluate Directly From the Bundle

You do not need a running server to test a bundle. OPA can evaluate policies directly from the archive:

```bash
opa eval -b mybundle.tar.gz -i input.json "data.authz.allow" --format pretty
```

This is useful in CI pipelines where you want to validate the bundle before publishing it.

## Verify What You Learned

**Command 1** -- Build the bundle and confirm it contains the three expected files:

```bash
opa build -b mybundle/ -o mybundle.tar.gz --revision "v1.0.0" && tar tzf mybundle.tar.gz | sort
```

Expected output:

```
/.manifest
/data.json
/policy.rego
```

**Command 2** -- Evaluate the policy from the bundle for an editor reading (should be allowed):

Create `input-editor-read.json`:

```json
{"role": "editor", "action": "read"}
```

```bash
opa eval -b mybundle.tar.gz -i input-editor-read.json "data.authz.allow" --format pretty
```

Expected output:

```
true
```

**Command 3** -- Evaluate the policy from the bundle for a viewer reading (should be denied, since viewers have no rule):

Create `input-viewer-read.json`:

```json
{"role": "viewer", "action": "read"}
```

```bash
opa eval -b mybundle.tar.gz -i input-viewer-read.json "data.authz.allow" --format pretty
```

Expected output:

```
false
```

**Command 4** -- Confirm the manifest contains the correct revision:

```bash
tar xzf mybundle.tar.gz .manifest -O | jq -r '.revision'
```

Expected output:

```
v1.0.0
```

You now know how to package policies into bundles, version them, serve them over HTTP, and have OPA download them automatically. In the next exercise, you will run OPA as a persistent REST server and interact with its full API.

## What's Next

Exercise 08-02 covers running OPA as a REST server -- the deployment model used in production microservice architectures, where your applications send authorization queries over HTTP instead of embedding OPA locally.

## Reference

- [OPA Bundles](https://www.openpolicyagent.org/docs/latest/management-bundles/)
- [opa build CLI reference](https://www.openpolicyagent.org/docs/latest/cli/#opa-build)
- [Bundle signing](https://www.openpolicyagent.org/docs/latest/management-bundles/#signing)

## Additional Resources

- [Styra Academy -- OPA fundamentals](https://academy.styra.com/)
- [OPA Playground](https://play.openpolicyagent.org/) -- test bundle policies interactively
- [OPA contrib examples on GitHub](https://github.com/open-policy-agent/contrib)

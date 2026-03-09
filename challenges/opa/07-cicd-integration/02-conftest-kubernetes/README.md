# Conftest + Kubernetes Manifests

## Prerequisites

- OPA CLI installed (`opa version`)
- Conftest installed (`conftest --version`)
- Completed exercise 07-01 (Conftest + Dockerfiles)

## Learning Objectives

After completing this exercise, you will be able to:

- Write Conftest policies that validate Kubernetes Deployment manifests for security and reliability
- Iterate over containers in a pod spec to check for common misconfigurations
- Apply the fix-and-retest pattern: break a manifest, observe failures, fix it, confirm zero failures

## Why This Matters

In the previous exercise, Conftest validated Dockerfiles. Now we apply the same approach to Kubernetes manifests. If your team deploys with `kubectl apply`, the logical step is to verify that every YAML meets your team's security standards before it reaches the cluster. Conftest with YAML is even more straightforward than with Dockerfiles -- there is no special parsing needed. The YAML converts directly to a JSON object, so `input.spec.containers[0].image` in YAML becomes exactly `input.spec.containers[0].image` in Rego. No intermediate transformation, no surprises.

For organizing policies, you can have multiple `.rego` files in the `policy/` directory. Conftest loads all of them automatically. They all use `package main` by default. For a small project, a single `policy.rego` is enough. When the project grows, you can split by resource type: `policy/deployment.rego`, `policy/service.rego`, etc.

The `deny` / `warn` distinction gives you flexibility. Something is critical and must break the pipeline? Use `deny`. It is a best practice but you do not want to block deployments yet? Use `warn`. Your CI decides what to do with each level.

## Practice

Here is a Kubernetes Deployment with several security issues.

Create `deployment.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web-app
  labels:
    app: web-app
spec:
  replicas: 1
  selector:
    matchLabels:
      app: web-app
  template:
    metadata:
      labels:
        app: web-app
    spec:
      containers:
        - name: web
          image: nginx:latest
          ports:
            - containerPort: 80
          securityContext:
            privileged: true
```

This deployment has problems: it uses `latest`, it has no resource limits, it has no liveness probe, and the container runs as privileged. Let's write policies for all of these.

Create `policy/policy.rego`:

```rego
package main

import rego.v1

# Helper: extract all containers from the pod spec
containers := input.spec.template.spec.containers

# --- DENY: do not use :latest in images ---
deny contains msg if {
    some container in containers
    endswith(container.image, ":latest")
    msg := sprintf("Container '%s' uses image '%s' -- pin a specific tag", [container.name, container.image])
}

# --- DENY: must have resource limits ---
deny contains msg if {
    some container in containers
    not container.resources.limits
    msg := sprintf("Container '%s' does not have resources.limits -- without limits it can consume the entire node", [container.name])
}

# --- WARN: should have liveness probe ---
warn contains msg if {
    some container in containers
    not container.livenessProbe
    msg := sprintf("Container '%s' does not have livenessProbe -- Kubernetes will not know if it is alive", [container.name])
}

# --- DENY: do not run as privileged ---
deny contains msg if {
    some container in containers
    container.securityContext.privileged == true
    msg := sprintf("Container '%s' runs as privileged -- this gives full access to the host", [container.name])
}

# --- WARN: should have readiness probe ---
warn contains msg if {
    some container in containers
    not container.readinessProbe
    msg := sprintf("Container '%s' does not have readinessProbe -- the Service could send traffic before it is ready", [container.name])
}
```

Run it:

```bash
conftest test deployment.yaml
```

Expected output:

```
FAIL - deployment.yaml - main - Container 'web' uses image 'nginx:latest' -- pin a specific tag
FAIL - deployment.yaml - main - Container 'web' does not have resources.limits -- without limits it can consume the entire node
FAIL - deployment.yaml - main - Container 'web' runs as privileged -- this gives full access to the host
WARN - deployment.yaml - main - Container 'web' does not have livenessProbe -- Kubernetes will not know if it is alive
WARN - deployment.yaml - main - Container 'web' does not have readinessProbe -- the Service could send traffic before it is ready

5 tests, 0 passed, 2 warnings, 3 failures
```

Three hard failures and two warnings. In a real CI pipeline, this would block the deployment.

### Fixing the Manifest

Now let's fix every issue and confirm the manifest passes cleanly.

Create `deployment-fixed.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web-app
  labels:
    app: web-app
spec:
  replicas: 2
  selector:
    matchLabels:
      app: web-app
  template:
    metadata:
      labels:
        app: web-app
    spec:
      containers:
        - name: web
          image: nginx:1.25.3
          ports:
            - containerPort: 80
          resources:
            limits:
              cpu: "500m"
              memory: "128Mi"
            requests:
              cpu: "250m"
              memory: "64Mi"
          livenessProbe:
            httpGet:
              path: /
              port: 80
            initialDelaySeconds: 5
            periodSeconds: 10
          readinessProbe:
            httpGet:
              path: /
              port: 80
            initialDelaySeconds: 3
            periodSeconds: 5
          securityContext:
            privileged: false
```

```bash
conftest test deployment-fixed.yaml
```

Expected output:

```
5 tests, 5 passed, 0 warnings, 0 failures
```

That is what a green pipeline looks like. In a real CI setup, you would run `conftest test k8s/*.yaml` as a step, and any non-compliant manifest breaks the build.

### Intermediate Verification with OPA

You can also use `opa eval` directly against the YAML (OPA can read YAML as input):

```bash
opa eval --format pretty -i deployment.yaml -d policy/ "data.main.deny"
```

This shows the set of deny messages, confirming the same results without needing Conftest.

## Verify What You Learned

**1.** Confirm that the original deployment fails with 3 errors:

```bash
conftest test deployment.yaml 2>&1 | tail -1
```

Expected output:

```
5 tests, 0 passed, 2 warnings, 3 failures
```

**2.** Confirm that the fixed deployment passes cleanly:

```bash
conftest test deployment-fixed.yaml 2>&1 | tail -1
```

Expected output:

```
5 tests, 5 passed, 0 warnings, 0 failures
```

**3.** Use `opa eval` directly to count deny violations on the original deployment:

```bash
opa eval --format pretty -i deployment.yaml -d policy/ "count(data.main.deny)"
```

Expected output:

```
3
```

## What's Next

You have validated Dockerfiles and Kubernetes manifests. The next exercise completes the CI/CD integration picture by validating Terraform plans -- catching infrastructure misconfigurations like untagged resources, open security groups, and oversized instances before `terraform apply` runs.

## Reference

- [Conftest -- YAML parsing](https://www.conftest.dev/parser/#yaml)
- [Kubernetes Pod Security Standards](https://kubernetes.io/docs/concepts/security/pod-security-standards/)
- [Conftest in CI/CD](https://www.conftest.dev/usage/#cicd)

## Additional Resources

- [Kyverno](https://kyverno.io/) -- a Kubernetes-native alternative for policy enforcement at admission time
- [Kubernetes Security Best Practices](https://kubernetes.io/docs/concepts/security/overview/) -- official security guidance for cluster operators

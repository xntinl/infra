# Image Allowlist: Controlling What Runs in Your Cluster

## Prerequisites

- OPA CLI installed (`opa version`)
- Completed exercise 05-02 (required-labels)

## Learning Objectives

After completing this exercise, you will be able to:

- Validate container images against an allowlist of trusted registries
- Detect the `:latest` tag and bare images (no tag or digest) as policy violations
- Cover both `containers` and `initContainers` to prevent sneaky bypasses

## Why This Matters

Labels validate metadata, but images are what actually runs in your cluster. An image from an untrusted registry, or with the `:latest` tag (which is mutable), is a supply-chain attack vector. In this exercise you will build a policy that controls which images can run in your cluster.

There are three classic problems:

**Supply-chain attacks**: someone compromises a public image on Docker Hub. If your pods use it without restriction, you are executing code from a stranger. Restricting to internal registries (`gcr.io/myproject`, your private ECR) limits the attack surface.

**The `:latest` tag is a trap**: `nginx:latest` today can be one version and tomorrow a completely different one. If a pod restarts and pulls `:latest`, it may bring a new, untested, incompatible version. It is not reproducible. What worked on Monday can fail on Tuesday without anyone changing anything.

**No tag is worse**: if someone writes `image: nginx` without a tag, Docker implicitly assumes `:latest`. It is the same problem but more silent.

The ideal solution in production is to use **digests**: `nginx@sha256:abc123...`. A digest is an immutable hash of the image. It guarantees that you always run exactly that image, regardless of what happens with tags.

---

## Anatomy of an Image Reference

Before writing the policy, you need to understand how an image reference is composed:

```
registry/repo:tag          -> gcr.io/myproject/api:v1.2.3
registry/repo@sha256:...   -> gcr.io/myproject/api@sha256:abcdef...
repo:tag                   -> nginx:1.25 (implicit registry: docker.io/library)
repo                       -> nginx (implicit registry and tag: docker.io/library/nginx:latest)
```

The policies you write need to inspect each part.

---

## The Classic Mistake: Forgetting initContainers

A pod can have `containers` (the main ones) and `initContainers` (ones that run first). Many people write policies that only validate `containers` and forget about init containers. An attacker could put a malicious image in an initContainer and the policy would say nothing.

Your policy will validate both.

---

## The Policy

Create `policy.rego`:

```rego
package kubernetes.images

import rego.v1

# Allowed registries
allowed_registries := ["gcr.io/myproject", "docker.io/library"]

# Collect all containers (regular + init)
all_containers contains container if {
    some container in input.review.object.spec.containers
}

all_containers contains container if {
    some container in input.review.object.spec.initContainers
}

# Violation: disallowed registry
violation contains {"msg": msg} if {
    some container in all_containers
    image := container.image
    not _image_in_allowed_registry(image)
    msg := sprintf(
        "container '%s' uses image '%s' from a disallowed registry (allowed: %v)",
        [container.name, image, allowed_registries],
    )
}

# Violation: uses tag :latest
violation contains {"msg": msg} if {
    some container in all_containers
    image := container.image
    endswith(image, ":latest")
    msg := sprintf(
        "container '%s' uses image '%s' with tag ':latest' (use a specific tag or digest)",
        [container.name, image],
    )
}

# Violation: no tag or digest (bare image like "nginx")
violation contains {"msg": msg} if {
    some container in all_containers
    image := container.image
    not contains(image, ":")
    not contains(image, "@")
    msg := sprintf(
        "container '%s' uses image '%s' without tag or digest (specify a version)",
        [container.name, image],
    )
}

# Helper: checks if the image starts with one of the allowed registries
_image_in_allowed_registry(image) if {
    some registry in allowed_registries
    startswith(image, concat("/", [registry, ""]))
}

# Special case: images without registry prefix like "nginx:1.25"
# are considered from docker.io/library, which is in the allowed list
_image_in_allowed_registry(image) if {
    not contains(image, "/")
    some registry in allowed_registries
    registry == "docker.io/library"
}
```

Here is how each piece works:

- `all_containers` combines containers and initContainers into a single set. This way the violation rules iterate over all of them without duplicating code.
- The first `violation` checks the registry. It uses a helper `_image_in_allowed_registry` (the underscore prefix is a convention to indicate it is internal, not part of the policy's public API).
- The second `violation` catches the explicit `:latest` tag.
- The third `violation` catches images with no tag and no digest -- those that Docker resolves to `:latest` by default.
- The helper has two cases: images with a full path (`gcr.io/myproject/something`) and "bare" images without a slash (`nginx`) that are considered from `docker.io/library`.

---

## Scenario 1: Correct Images

Create `input.json`:

```json
{
    "review": {
        "object": {
            "kind": "Pod",
            "metadata": {
                "name": "my-app"
            },
            "spec": {
                "initContainers": [
                    {
                        "name": "init-db",
                        "image": "gcr.io/myproject/db-migrator:v2.1.0"
                    }
                ],
                "containers": [
                    {
                        "name": "api",
                        "image": "gcr.io/myproject/api:v1.5.3"
                    },
                    {
                        "name": "sidecar",
                        "image": "gcr.io/myproject/envoy:v1.28.0"
                    }
                ]
            }
        }
    }
}
```

```bash
opa eval --format pretty -d policy.rego -i input.json "data.kubernetes.images.violation"
```

Expected output:

```json
[]
```

All images are from `gcr.io/myproject` (allowed registry), with specific tags. No violations.

---

## Scenario 2: The :latest Tag

Create `input-latest-tag.json`:

```json
{
    "review": {
        "object": {
            "kind": "Pod",
            "metadata": {"name": "pod-latest"},
            "spec": {
                "containers": [
                    {
                        "name": "web",
                        "image": "gcr.io/myproject/web:latest"
                    }
                ]
            }
        }
    }
}
```

```bash
opa eval --format pretty -d policy.rego -i input-latest-tag.json "data.kubernetes.images.violation"
```

Expected output:

```json
[
  {
    "msg": "container 'web' uses image 'gcr.io/myproject/web:latest' with tag ':latest' (use a specific tag or digest)"
  }
]
```

The registry is fine, but `:latest` is not acceptable.

---

## Scenario 3: Disallowed Registry

Create `input-bad-registry.json`:

```json
{
    "review": {
        "object": {
            "kind": "Pod",
            "metadata": {"name": "external-pod"},
            "spec": {
                "containers": [
                    {
                        "name": "app",
                        "image": "quay.io/someone/malicious:v1.0"
                    }
                ]
            }
        }
    }
}
```

```bash
opa eval --format pretty -d policy.rego -i input-bad-registry.json "data.kubernetes.images.violation"
```

Expected output:

```json
[
  {
    "msg": "container 'app' uses image 'quay.io/someone/malicious:v1.0' from a disallowed registry (allowed: [\"gcr.io/myproject\", \"docker.io/library\"])"
  }
]
```

`quay.io` is not in the allowed registries list.

---

## Scenario 4: Image With Digest (The Ideal)

Create `input-digest.json`:

```json
{
    "review": {
        "object": {
            "kind": "Pod",
            "metadata": {"name": "secure-pod"},
            "spec": {
                "containers": [
                    {
                        "name": "app",
                        "image": "gcr.io/myproject/api@sha256:a3ed95caeb02ffe68cdd9fd84406680ae93d633cb16422d00e8a7c22955b46d4"
                    }
                ]
            }
        }
    }
}
```

```bash
opa eval --format pretty -d policy.rego -i input-digest.json "data.kubernetes.images.violation"
```

Expected output:

```json
[]
```

An image with a digest from an allowed registry. This is the most secure option: immutable and from a trusted origin.

---

## Scenario 5: Bare Image Without Tag

Create `input-no-tag.json`:

```json
{
    "review": {
        "object": {
            "kind": "Pod",
            "metadata": {"name": "bare-pod"},
            "spec": {
                "containers": [
                    {
                        "name": "web",
                        "image": "nginx"
                    }
                ]
            }
        }
    }
}
```

```bash
opa eval --format pretty -d policy.rego -i input-no-tag.json "data.kubernetes.images.violation"
```

Expected output:

```json
[
  {
    "msg": "container 'web' uses image 'nginx' without tag or digest (specify a version)"
  }
]
```

`nginx` without a tag resolves to `nginx:latest` implicitly. The policy catches it.

---

## Scenario 6: Sneaky initContainer

This is the critical case. Everything looks fine in containers, but the initContainer hides a suspicious image.

Create `input-sneaky-init.json`:

```json
{
    "review": {
        "object": {
            "kind": "Pod",
            "metadata": {"name": "pod-sneaky"},
            "spec": {
                "initContainers": [
                    {
                        "name": "init-hack",
                        "image": "evil.registry.io/backdoor:v1"
                    }
                ],
                "containers": [
                    {
                        "name": "app",
                        "image": "gcr.io/myproject/api:v1.0.0"
                    }
                ]
            }
        }
    }
}
```

```bash
opa eval --format pretty -d policy.rego -i input-sneaky-init.json "data.kubernetes.images.violation"
```

Expected output:

```json
[
  {
    "msg": "container 'init-hack' uses image 'evil.registry.io/backdoor:v1' from a disallowed registry (allowed: [\"gcr.io/myproject\", \"docker.io/library\"])"
  }
]
```

The main container is fine, but the initContainer was caught. Without the `all_containers` rule that combines both, this would have gone unnoticed.

---

## Verify What You Learned

**Command 1** -- Confirm the compliant pod passes:

```bash
opa eval --format pretty -d policy.rego -i input.json "data.kubernetes.images.violation"
```

Expected output: `[]`

**Command 2** -- A pod with both :latest and a bad registry:

Create `input-latest-and-bad-registry.json`:

```json
{
    "review": {
        "object": {
            "kind": "Pod",
            "metadata": {"name": "test"},
            "spec": {
                "containers": [
                    {"name": "a", "image": "gcr.io/myproject/x:latest"},
                    {"name": "b", "image": "unknown.io/y:v1"}
                ]
            }
        }
    }
}
```

```bash
opa eval --format pretty -d policy.rego -i input-latest-and-bad-registry.json "data.kubernetes.images.violation"
```

Expected output:

```json
[
  {
    "msg": "container 'a' uses image 'gcr.io/myproject/x:latest' with tag ':latest' (use a specific tag or digest)"
  },
  {
    "msg": "container 'b' uses image 'unknown.io/y:v1' from a disallowed registry (allowed: [\"gcr.io/myproject\", \"docker.io/library\"])"
  }
]
```

**Command 3** -- Docker Hub official images are allowed:

Create `input-allowed-dockerhub.json`:

```json
{
    "review": {
        "object": {
            "kind": "Pod",
            "metadata": {"name": "test"},
            "spec": {
                "containers": [
                    {"name": "ok", "image": "docker.io/library/nginx:1.25"}
                ]
            }
        }
    }
}
```

```bash
opa eval --format pretty -d policy.rego -i input-allowed-dockerhub.json "data.kubernetes.images.violation"
```

Expected output: `[]`

**Command 4** -- Verify that all_containers includes both init and regular containers:

Create `input-all-containers.json`:

```json
{
    "review": {
        "object": {
            "kind": "Pod",
            "metadata": {"name": "test"},
            "spec": {
                "initContainers": [{"name": "init", "image": "gcr.io/myproject/init:v1"}],
                "containers": [{"name": "app", "image": "gcr.io/myproject/app:v2"}]
            }
        }
    }
}
```

```bash
opa eval --format pretty -d policy.rego -i input-all-containers.json "data.kubernetes.images.all_containers"
```

Expected output:

```json
[
  {
    "image": "gcr.io/myproject/app:v2",
    "name": "app"
  },
  {
    "image": "gcr.io/myproject/init:v1",
    "name": "init"
  }
]
```

---

## What's Next

You can now control which images run in your cluster and from which registries. In the next exercise you will tackle the deepest layer of pod security: SecurityContexts, which control how containers run at the Linux process level.

## Reference

- [Kubernetes Container Images](https://kubernetes.io/docs/concepts/containers/images/) -- how Kubernetes pulls and manages container images.
- [OPA Policy Reference: Strings](https://www.openpolicyagent.org/docs/latest/policy-reference/#strings) -- `startswith`, `endswith`, `contains`, and `concat` builtins.
- [Gatekeeper Library: Allowed Repos](https://open-policy-agent.github.io/gatekeeper-library/website/validation/allowedrepos) -- the upstream Gatekeeper library template for image allowlists.

## Additional Resources

- [SLSA Framework](https://slsa.dev/) -- supply-chain security framework for software artifacts including container images.
- [Cosign](https://docs.sigstore.dev/cosign/overview/) -- tool for signing and verifying container images.
- [Docker Image Tagging Best Practices](https://docs.docker.com/develop/dev-best-practices/) -- Docker guidance on tagging strategies.

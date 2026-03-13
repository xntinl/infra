# Tekton Triggers and Event-Driven Pipelines

<!--
difficulty: advanced
concepts: [tekton-triggers, eventlistener, triggerbinding, triggertemplate, interceptors, webhooks, event-driven-ci]
tools: [kubectl, tkn, tekton-triggers]
estimated_time: 40m
bloom_level: analyze
prerequisites: [03-tekton-tasks-and-pipelines]
-->

## Overview

Tekton Triggers extends Tekton Pipelines by enabling event-driven pipeline execution. When a webhook fires (e.g., a GitHub push event), Tekton Triggers receives the event, extracts parameters from the payload, and creates a PipelineRun. This removes the need for external CI orchestration -- the pipeline runs automatically when code changes.

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                 Tekton Triggers Architecture                  │
│                                                                │
│  GitHub/GitLab ──webhook──▶ EventListener (Service)           │
│                                  │                             │
│                           ┌──────▼──────┐                     │
│                           │ Interceptors │                     │
│                           │ (validate,   │                     │
│                           │  filter,     │                     │
│                           │  transform)  │                     │
│                           └──────┬──────┘                     │
│                                  │                             │
│                           ┌──────▼──────┐                     │
│                           │TriggerBinding│ extracts fields     │
│                           │ from payload │ (repo, branch,     │
│                           │              │  commit SHA)        │
│                           └──────┬──────┘                     │
│                                  │                             │
│                           ┌──────▼────────┐                   │
│                           │TriggerTemplate │ creates           │
│                           │               │ PipelineRun       │
│                           └───────────────┘                   │
└──────────────────────────────────────────────────────────────┘
```

## Suggested Steps

### 1. Install Tekton Triggers

```bash
# Install Tekton Pipelines (if not already installed)
kubectl apply --filename https://storage.googleapis.com/tekton-releases/pipeline/latest/release.yaml

# Install Tekton Triggers
kubectl apply --filename https://storage.googleapis.com/tekton-releases/triggers/latest/release.yaml
kubectl apply --filename https://storage.googleapis.com/tekton-releases/triggers/latest/interceptors.yaml

# Wait for readiness
kubectl wait --for=condition=Available deployment/tekton-triggers-controller \
  -n tekton-pipelines --timeout=120s
```

### 2. Create the Pipeline (reuse from Exercise 03)

```yaml
# pipeline.yaml
apiVersion: tekton.dev/v1
kind: Task
metadata:
  name: git-clone
spec:
  params:
    - name: url
      type: string
    - name: revision
      type: string
  workspaces:
    - name: output
  steps:
    - name: clone
      image: alpine/git:2.43.0
      script: |
        git clone --branch $(params.revision) $(params.url) $(workspaces.output.path)/source
        cd $(workspaces.output.path)/source
        echo "Checked out $(git rev-parse HEAD)"
---
apiVersion: tekton.dev/v1
kind: Task
metadata:
  name: build-and-push
spec:
  params:
    - name: image
      type: string
  workspaces:
    - name: source
  steps:
    - name: build
      image: busybox:1.37
      script: |
        echo "Building image $(params.image)..."
        echo "Source at $(workspaces.source.path)/source"
        echo "Build complete (simulated)"
---
apiVersion: tekton.dev/v1
kind: Pipeline
metadata:
  name: ci-pipeline
spec:
  params:
    - name: git-url
      type: string
    - name: git-revision
      type: string
      default: main
    - name: image-name
      type: string
  workspaces:
    - name: shared-workspace
  tasks:
    - name: clone
      taskRef:
        name: git-clone
      params:
        - name: url
          value: $(params.git-url)
        - name: revision
          value: $(params.git-revision)
      workspaces:
        - name: output
          workspace: shared-workspace
    - name: build
      taskRef:
        name: build-and-push
      runAfter: [clone]
      params:
        - name: image
          value: $(params.image-name)
      workspaces:
        - name: source
          workspace: shared-workspace
```

```bash
kubectl apply -f pipeline.yaml
```

### 3. Create TriggerBinding and TriggerTemplate

```yaml
# triggers.yaml
apiVersion: triggers.tekton.dev/v1beta1
kind: TriggerBinding
metadata:
  name: github-push-binding
spec:
  params:
    - name: git-url
      value: $(body.repository.clone_url)       # extract from GitHub webhook payload
    - name: git-revision
      value: $(body.head_commit.id)
    - name: image-name
      value: "ghcr.io/$(body.repository.full_name):$(body.head_commit.id)"
---
apiVersion: triggers.tekton.dev/v1beta1
kind: TriggerTemplate
metadata:
  name: ci-pipeline-template
spec:
  params:
    - name: git-url
    - name: git-revision
    - name: image-name
  resourcetemplates:
    - apiVersion: tekton.dev/v1
      kind: PipelineRun
      metadata:
        generateName: ci-triggered-run-
      spec:
        pipelineRef:
          name: ci-pipeline
        params:
          - name: git-url
            value: $(tt.params.git-url)
          - name: git-revision
            value: $(tt.params.git-revision)
          - name: image-name
            value: $(tt.params.image-name)
        workspaces:
          - name: shared-workspace
            volumeClaimTemplate:
              spec:
                accessModes: [ReadWriteOnce]
                resources:
                  requests:
                    storage: 1Gi
```

```bash
kubectl apply -f triggers.yaml
```

### 4. Create the EventListener

```yaml
# eventlistener.yaml
apiVersion: triggers.tekton.dev/v1beta1
kind: EventListener
metadata:
  name: github-listener
spec:
  serviceAccountName: tekton-triggers-sa
  triggers:
    - name: github-push
      interceptors:
        - ref:
            name: github                        # built-in GitHub interceptor
          params:
            - name: secretRef
              value:
                secretName: github-webhook-secret
                secretKey: secret
            - name: eventTypes
              value: ["push"]                   # only trigger on push events
        - ref:
            name: cel                           # CEL filter
          params:
            - name: filter
              value: "body.ref == 'refs/heads/main'"  # only main branch
      bindings:
        - ref: github-push-binding
      template:
        ref: ci-pipeline-template
```

```bash
# Create the webhook secret
kubectl create secret generic github-webhook-secret \
  --from-literal=secret=my-webhook-secret-token

# Create RBAC for the EventListener
kubectl apply -f - <<'EOF'
apiVersion: v1
kind: ServiceAccount
metadata:
  name: tekton-triggers-sa
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: tekton-triggers-binding
subjects:
  - kind: ServiceAccount
    name: tekton-triggers-sa
roleRef:
  kind: ClusterRole
  name: tekton-triggers-eventlistener-roles
  apiGroup: rbac.authorization.k8s.io
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: tekton-triggers-clusterbinding
subjects:
  - kind: ServiceAccount
    name: tekton-triggers-sa
    namespace: default
roleRef:
  kind: ClusterRole
  name: tekton-triggers-eventlistener-clusterroles
  apiGroup: rbac.authorization.k8s.io
EOF

kubectl apply -f eventlistener.yaml

# Wait for the EventListener to create its service
kubectl get eventlistener github-listener
kubectl get svc el-github-listener
```

### 5. Test with a Simulated Webhook

```bash
# Port-forward the EventListener service
kubectl port-forward svc/el-github-listener 8080:8080 &

# Send a simulated GitHub push event
curl -X POST http://localhost:8080 \
  -H "Content-Type: application/json" \
  -H "X-GitHub-Event: push" \
  -H "X-Hub-Signature-256: $(echo -n '{"ref":"refs/heads/main","head_commit":{"id":"abc123"},"repository":{"clone_url":"https://github.com/example/repo.git","full_name":"example/repo"}}' | openssl dgst -sha256 -hmac 'my-webhook-secret-token' | awk '{print "sha256="$2}')" \
  -d '{
    "ref": "refs/heads/main",
    "head_commit": {"id": "abc123"},
    "repository": {
      "clone_url": "https://github.com/example/repo.git",
      "full_name": "example/repo"
    }
  }'

# Check if a PipelineRun was created
sleep 5
tkn pipelinerun list
```

## Verify

```bash
# EventListener is running
kubectl get eventlistener github-listener
kubectl get svc el-github-listener

# Trigger components exist
kubectl get triggerbindings
kubectl get triggertemplates

# A PipelineRun was created from the webhook
tkn pipelinerun list | grep ci-triggered
```

## Cleanup

```bash
kubectl delete eventlistener github-listener
kubectl delete triggerbinding github-push-binding
kubectl delete triggertemplate ci-pipeline-template
kubectl delete pipeline ci-pipeline
kubectl delete tasks git-clone build-and-push
kubectl delete pipelineruns --all
kubectl delete secret github-webhook-secret
kubectl delete sa tekton-triggers-sa
kubectl delete rolebinding tekton-triggers-binding
kubectl delete clusterrolebinding tekton-triggers-clusterbinding
kubectl delete -f https://storage.googleapis.com/tekton-releases/triggers/latest/release.yaml
kubectl delete -f https://storage.googleapis.com/tekton-releases/triggers/latest/interceptors.yaml
kubectl delete -f https://storage.googleapis.com/tekton-releases/pipeline/latest/release.yaml
```

## Reference

- [Tekton Triggers](https://tekton.dev/docs/triggers/)
- [EventListener](https://tekton.dev/docs/triggers/eventlisteners/)
- [Interceptors](https://tekton.dev/docs/triggers/interceptors/)

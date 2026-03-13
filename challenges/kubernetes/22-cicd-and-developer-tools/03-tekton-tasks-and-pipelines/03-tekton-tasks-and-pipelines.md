# Tekton: Tasks and Pipelines

<!--
difficulty: intermediate
concepts: [tekton, tasks, pipelines, steps, workspaces, params, results, pipelineruns]
tools: [kubectl, tkn]
estimated_time: 35m
bloom_level: apply
prerequisites: [kubectl-basics, container-basics]
-->

## Overview

Tekton is a Kubernetes-native CI/CD framework. It defines CI/CD primitives (Tasks, Pipelines, Workspaces) as Kubernetes custom resources. Pipelines run as pods in your cluster, giving you the same scalability, observability, and access control as any other Kubernetes workload.

## Why This Matters

Unlike external CI services, Tekton runs inside your cluster with direct access to Kubernetes APIs, container registries, and internal services. Pipelines are version-controlled YAML that follow Kubernetes conventions, making them a natural fit for GitOps workflows.

## Step-by-Step Instructions

### Step 1 -- Install Tekton

```bash
# Install Tekton Pipelines
kubectl apply --filename https://storage.googleapis.com/tekton-releases/pipeline/latest/release.yaml

# Wait for Tekton to be ready
kubectl wait --for=condition=Available deployment/tekton-pipelines-controller \
  -n tekton-pipelines --timeout=120s

# (Optional) Install the Tekton CLI
# brew install tektoncd-cli
```

### Step 2 -- Create a Simple Task

A Task is a sequence of steps, each running in a container.

```yaml
# clone-and-test.yaml
apiVersion: tekton.dev/v1
kind: Task
metadata:
  name: clone-and-test
spec:
  params:
    - name: repo-url
      type: string
      description: "Git repository URL"
    - name: branch
      type: string
      default: main
  workspaces:
    - name: source                # shared volume between steps
  steps:
    - name: clone
      image: alpine/git:2.43.0
      script: |
        #!/bin/sh
        git clone --branch $(params.branch) $(params.repo-url) $(workspaces.source.path)/repo
        echo "Cloned $(params.repo-url) at $(params.branch)"

    - name: list-files
      image: busybox:1.37
      script: |
        #!/bin/sh
        echo "Repository contents:"
        ls -la $(workspaces.source.path)/repo

    - name: run-tests
      image: busybox:1.37
      script: |
        #!/bin/sh
        echo "Running tests..."
        # Simulate test execution
        if [ -f "$(workspaces.source.path)/repo/Makefile" ]; then
          echo "Makefile found -- tests would run here"
        else
          echo "No Makefile found -- running basic checks"
        fi
        echo "Tests passed!"
  results:
    - name: commit-sha
      description: "The git commit SHA that was tested"
```

```bash
kubectl apply -f clone-and-test.yaml
```

### Step 3 -- Run a Task

```yaml
# taskrun-clone.yaml
apiVersion: tekton.dev/v1
kind: TaskRun
metadata:
  generateName: clone-and-test-run-
spec:
  taskRef:
    name: clone-and-test
  params:
    - name: repo-url
      value: "https://github.com/tektoncd/pipeline.git"
    - name: branch
      value: main
  workspaces:
    - name: source
      emptyDir: {}                 # use an emptyDir volume for the workspace
```

```bash
kubectl create -f taskrun-clone.yaml

# Watch the TaskRun progress
tkn taskrun list
tkn taskrun logs --last -f

# Or with kubectl:
kubectl get taskruns
kubectl logs -l tekton.dev/taskRun --all-containers -f
```

### Step 4 -- Create a Build and Deploy Pipeline

```yaml
# build-deploy-pipeline.yaml
apiVersion: tekton.dev/v1
kind: Task
metadata:
  name: build-image
spec:
  params:
    - name: image-name
      type: string
  workspaces:
    - name: source
  steps:
    - name: build
      image: busybox:1.37
      script: |
        #!/bin/sh
        echo "Building image $(params.image-name)..."
        echo "Using source from $(workspaces.source.path)/repo"
        # In a real pipeline, this would use Kaniko or Buildah
        echo "Image built successfully!"
  results:
    - name: image-digest
      description: "Image digest"
---
apiVersion: tekton.dev/v1
kind: Task
metadata:
  name: deploy-to-k8s
spec:
  params:
    - name: image-name
      type: string
    - name: namespace
      type: string
      default: default
  steps:
    - name: deploy
      image: bitnami/kubectl:1.30
      script: |
        #!/bin/sh
        echo "Deploying $(params.image-name) to namespace $(params.namespace)..."
        kubectl create deployment tekton-deployed \
          --image=$(params.image-name) \
          --dry-run=client -o yaml | kubectl apply -f - -n $(params.namespace)
        echo "Deployment complete!"
---
apiVersion: tekton.dev/v1
kind: Pipeline
metadata:
  name: build-and-deploy
spec:
  params:
    - name: repo-url
      type: string
    - name: image-name
      type: string
    - name: deploy-namespace
      type: string
      default: default
  workspaces:
    - name: shared-workspace        # passed through to tasks
  tasks:
    - name: fetch-source
      taskRef:
        name: clone-and-test
      params:
        - name: repo-url
          value: $(params.repo-url)
      workspaces:
        - name: source
          workspace: shared-workspace

    - name: build
      taskRef:
        name: build-image
      runAfter:
        - fetch-source              # explicit ordering
      params:
        - name: image-name
          value: $(params.image-name)
      workspaces:
        - name: source
          workspace: shared-workspace

    - name: deploy
      taskRef:
        name: deploy-to-k8s
      runAfter:
        - build
      params:
        - name: image-name
          value: $(params.image-name)
        - name: namespace
          value: $(params.deploy-namespace)
```

```bash
kubectl apply -f build-deploy-pipeline.yaml
```

### Step 5 -- Run the Pipeline

```yaml
# pipelinerun.yaml
apiVersion: tekton.dev/v1
kind: PipelineRun
metadata:
  generateName: build-and-deploy-run-
spec:
  pipelineRef:
    name: build-and-deploy
  params:
    - name: repo-url
      value: "https://github.com/tektoncd/pipeline.git"
    - name: image-name
      value: "myregistry/myapp:latest"
    - name: deploy-namespace
      value: default
  workspaces:
    - name: shared-workspace
      volumeClaimTemplate:
        spec:
          accessModes:
            - ReadWriteOnce
          resources:
            requests:
              storage: 1Gi
```

```bash
kubectl create -f pipelinerun.yaml

# Watch pipeline progress
tkn pipelinerun list
tkn pipelinerun logs --last -f
```

### Step 6 -- Inspect Results

```bash
# List all pipeline runs with status
tkn pipelinerun list

# Get detailed pipeline run status
kubectl get pipelineruns -o wide

# See which tasks succeeded/failed
tkn pipelinerun describe --last
```

## Verify

```bash
# Tasks are registered
kubectl get tasks

# Pipeline is registered
kubectl get pipelines

# PipelineRun completed successfully
tkn pipelinerun list
# STATUS should show "Succeeded"

# Or with kubectl:
kubectl get pipelineruns -o custom-columns='NAME:.metadata.name,STATUS:.status.conditions[0].status'
```

## Cleanup

```bash
kubectl delete pipelineruns --all
kubectl delete pipelines --all
kubectl delete tasks --all
kubectl delete taskruns --all
kubectl delete deployment tekton-deployed --ignore-not-found
kubectl delete -f https://storage.googleapis.com/tekton-releases/pipeline/latest/release.yaml
```

## Reference

- [Tekton Pipelines](https://tekton.dev/docs/pipelines/)
- [Task Reference](https://tekton.dev/docs/pipelines/tasks/)
- [Pipeline Reference](https://tekton.dev/docs/pipelines/pipelines/)

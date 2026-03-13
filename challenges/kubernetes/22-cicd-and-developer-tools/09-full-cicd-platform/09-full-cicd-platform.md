# Full CI/CD Platform on Kubernetes

<!--
difficulty: insane
concepts: [ci-cd-platform, tekton, argocd, gitops, image-registry, secret-management, monitoring, end-to-end]
tools: [kubectl, helm, tekton, argocd, kaniko, sealed-secrets]
estimated_time: 120m
bloom_level: create
prerequisites: [03-tekton-tasks-and-pipelines, 06-tekton-triggers, 07-kaniko-in-cluster-builds]
-->

## Scenario

Build a complete, self-hosted CI/CD platform running entirely on Kubernetes. The platform should handle the full lifecycle: code push triggers a build, tests run, a container image is built and pushed, and the application is deployed to staging automatically and to production with approval. All components run in-cluster -- no external CI service.

The platform must include:
- **Source control integration**: Webhook-triggered pipelines from GitHub/GitLab
- **CI pipeline**: Build, test, lint, security scan, and image build (using Kaniko)
- **CD pipeline**: GitOps-based deployment using Argo CD
- **Secret management**: Sealed Secrets or External Secrets for credential handling
- **Artifact storage**: In-cluster container registry
- **Observability**: Pipeline metrics and alerting

## Constraints

1. All CI/CD infrastructure must run inside the Kubernetes cluster (no external services except the Git provider)
2. The CI pipeline must complete in under 10 minutes for a typical Go/Node.js application
3. Container images must be signed and scanned for vulnerabilities before deployment
4. Argo CD must deploy from a GitOps repository (separate from the application repository)
5. Secrets must never appear in plain text in Git repositories (use Sealed Secrets)
6. The platform must support at least 3 concurrent pipeline runs
7. Pipeline metrics must be exposed to Prometheus (pipeline duration, success/failure rate)
8. Rollback must be achievable via Git revert on the GitOps repository

## Success Criteria

1. Pushing code to a GitHub repository triggers a Tekton PipelineRun within 30 seconds
2. The pipeline clones, tests, lints, builds (Kaniko), scans (Trivy), and pushes the image
3. The pipeline updates the GitOps repository with the new image tag
4. Argo CD detects the change and deploys to staging automatically
5. Production deployment requires manual sync in Argo CD (or a PR-based approval flow)
6. Sealed Secrets are decryptable only by the cluster's controller
7. Rolling back to a previous version is a `git revert` followed by Argo CD sync
8. Prometheus scrapes pipeline metrics (tekton_pipelines_controller_pipelinerun_duration_seconds)
9. The platform can handle 3 simultaneous pipeline runs without resource contention

## Verification Commands

```bash
# Infrastructure components are healthy
kubectl get pods -n tekton-pipelines
kubectl get pods -n argocd
kubectl get pods -n registry
kubectl get pods -n sealed-secrets

# Tekton Triggers is receiving webhooks
kubectl get eventlistener -n ci
kubectl logs -l app.kubernetes.io/part-of=tekton-triggers -n tekton-pipelines --tail=10

# Pipeline runs
tkn pipelinerun list -n ci
tkn pipelinerun logs --last -n ci

# Argo CD applications
kubectl get applications -n argocd
argocd app list
argocd app get myapp-staging

# Image exists in registry
curl -s http://registry.registry.svc:5000/v2/myapp/tags/list | jq .

# Sealed Secrets controller
kubectl get pods -n sealed-secrets
kubectl get sealedsecrets -A

# Prometheus metrics
kubectl port-forward -n monitoring svc/prometheus 9090:9090 &
curl -s 'http://localhost:9090/api/v1/query?query=tekton_pipelines_controller_pipelinerun_duration_seconds_count' | jq .

# Concurrent pipeline test
for i in 1 2 3; do
  curl -X POST http://el-github-listener.ci.svc:8080 \
    -H "Content-Type: application/json" \
    -H "X-GitHub-Event: push" \
    -d "{\"ref\":\"refs/heads/main\",\"head_commit\":{\"id\":\"test$i\"},\"repository\":{\"clone_url\":\"https://github.com/example/repo.git\",\"full_name\":\"example/repo\"}}" &
done
wait
tkn pipelinerun list -n ci  # should show 3 runs

# Rollback test
cd gitops-repo
git revert HEAD --no-edit
git push origin main
# Wait for Argo CD to sync
argocd app wait myapp-staging --timeout 120
kubectl get deployment myapp -n staging -o jsonpath='{.spec.template.spec.containers[0].image}'
# Should show the previous image tag
```

## Cleanup

```bash
# Remove applications
argocd app delete myapp-staging --cascade
argocd app delete myapp-production --cascade

# Remove CI resources
kubectl delete namespace ci
kubectl delete pipelineruns --all -A
kubectl delete taskruns --all -A

# Remove infrastructure
helm uninstall argocd -n argocd
kubectl delete namespace argocd
kubectl delete -f https://storage.googleapis.com/tekton-releases/pipeline/latest/release.yaml
kubectl delete -f https://storage.googleapis.com/tekton-releases/triggers/latest/release.yaml
kubectl delete namespace registry
kubectl delete namespace sealed-secrets
kubectl delete namespace monitoring
```

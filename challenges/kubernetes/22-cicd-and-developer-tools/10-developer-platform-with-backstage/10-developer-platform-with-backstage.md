# Developer Platform with Backstage and K8s

<!--
difficulty: insane
concepts: [backstage, developer-portal, software-catalog, templates, plugins, techdocs, kubernetes-integration, idp]
tools: [backstage, kubectl, helm, node, yarn]
estimated_time: 120m
bloom_level: create
prerequisites: [kubectl-basics, 03-tekton-tasks-and-pipelines]
-->

## Scenario

Build an Internal Developer Platform (IDP) using Backstage as the developer portal, backed by Kubernetes for workload management. Developers should be able to:

- Browse a software catalog showing all services, their owners, and dependencies
- Create new microservices from templates that scaffold a Git repository, CI/CD pipeline, and Kubernetes manifests
- View Kubernetes pod status, logs, and deployment health directly in Backstage
- Read auto-generated TechDocs (documentation as code) for each service
- Trigger CI/CD pipelines from the portal

This represents the modern platform engineering approach where Kubernetes is the runtime and Backstage is the user-facing layer.

## Constraints

1. Backstage must run as a Deployment inside the Kubernetes cluster
2. The software catalog must be populated from a Git repository containing `catalog-info.yaml` files
3. At least 2 software templates must be available: one for a Go microservice and one for a Node.js service
4. Templates must scaffold: source code, Dockerfile, Kubernetes manifests (Deployment + Service), Tekton pipeline, and catalog registration
5. The Kubernetes plugin must show live pod status and recent events for each service
6. TechDocs must render Markdown documentation stored alongside the service code
7. RBAC must be configured so only team owners can trigger deployments for their services
8. The platform must support at least 10 registered services in the catalog
9. Backstage must use PostgreSQL for persistent storage (not SQLite)

## Success Criteria

1. Backstage is accessible at `https://backstage.internal` (or a port-forward equivalent)
2. The software catalog displays at least 3 registered services with correct ownership and lifecycle metadata
3. Creating a new service from a template produces a Git repository with all required files
4. The Kubernetes tab in Backstage shows pod status, CPU/memory usage, and recent events for each service
5. TechDocs renders Markdown documentation for at least one service
6. Triggering a pipeline from Backstage creates a Tekton PipelineRun visible in the CI/CD tab
7. The catalog auto-discovers new `catalog-info.yaml` files from registered Git repositories
8. PostgreSQL is running as a StatefulSet with persistent storage
9. Backstage pods survive restart without losing catalog data

## Verification Commands

```bash
# Backstage is running
kubectl get pods -n backstage
kubectl get svc -n backstage

# PostgreSQL is running
kubectl get statefulset -n backstage
kubectl get pvc -n backstage

# Access Backstage
kubectl port-forward -n backstage svc/backstage 7007:7007 &

# Verify catalog has registered entities
curl -s http://localhost:7007/api/catalog/entities | jq '.[].metadata.name' | head -10

# Verify Kubernetes plugin connectivity
curl -s http://localhost:7007/api/kubernetes/clusters | jq .

# Verify software templates
curl -s http://localhost:7007/api/catalog/entities?filter=kind=template | jq '.[].metadata.name'

# Verify TechDocs
curl -s http://localhost:7007/api/techdocs/metadata/entity/default/component/myservice | jq .

# Create a service from a template (via the UI or API)
# Navigate to http://localhost:7007/create and fill out a template

# Verify the created service appears in catalog
curl -s http://localhost:7007/api/catalog/entities?filter=metadata.name=new-service | jq .

# Check pod status through Backstage Kubernetes plugin
curl -s 'http://localhost:7007/api/kubernetes/services/myservice' | jq '.items[].status'

# Sample catalog-info.yaml that services should have
cat <<'EOF'
apiVersion: backstage.io/v1alpha1
kind: Component
metadata:
  name: myservice
  description: My microservice
  annotations:
    backstage.io/techdocs-ref: dir:.
    backstage.io/kubernetes-id: myservice
    github.com/project-slug: myorg/myservice
spec:
  type: service
  lifecycle: production
  owner: team-backend
  system: platform
  dependsOn:
    - resource:default/database
  providesApis:
    - myservice-api
EOF
```

## Cleanup

```bash
# Delete Backstage
kubectl delete namespace backstage

# Delete any scaffolded services
kubectl delete deployments,services -l app.kubernetes.io/managed-by=backstage --all-namespaces

# Remove Tekton integration
kubectl delete pipelineruns -l backstage.io/created=true --all-namespaces

# Clean up Git repositories created by templates (manual step via GitHub/GitLab)
```

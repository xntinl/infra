# Crossplane: Compositions and Cloud Resources

<!--
difficulty: insane
concepts: [crossplane, compositions, xrd, managed-resources, providers, composite-resources, claims, iac-kubernetes]
tools: [kubectl, helm, crossplane-cli]
estimated_time: 90m
bloom_level: create
prerequisites: [01-crd-basics, 05-operator-pattern]
-->

## Scenario

Your platform team needs to provide self-service cloud infrastructure to developers through Kubernetes. Using Crossplane, you will build a platform abstraction that lets developers request a "Database" via a Kubernetes custom resource claim, and the platform automatically provisions a cloud database (RDS), a Kubernetes Secret with connection credentials, and a NetworkPolicy for access control.

Developers should not need to know cloud-provider details -- they submit a simple claim, and the platform handles the rest.

## Constraints

1. Install Crossplane and the AWS provider (or use a local provider for testing)
2. Define a CompositeResourceDefinition (XRD) named `XDatabase` with fields: `engine` (postgres/mysql), `version`, `size` (small/medium/large), and `region`
3. Create a Composition that maps the XRD to:
   - An RDS instance (or local mock) with engine-specific configuration
   - A Kubernetes Secret containing host, port, username, password
   - A NetworkPolicy allowing only pods with a specific label to access the database port
4. Size mappings: small = db.t3.micro/20GB, medium = db.t3.medium/100GB, large = db.r5.large/500GB
5. Create a Claim (namespace-scoped) that developers use to request databases
6. The Composition must support both postgres and mysql engines with different default ports (5432/3306)
7. Implement a usage policy that limits each namespace to at most 3 database claims
8. All resources must be cleaned up when the claim is deleted

## Success Criteria

1. Crossplane is installed and the provider is healthy
2. The XRD is registered and produces a CRD for both composite resources and claims
3. Creating a DatabaseClaim in a namespace triggers provisioning of all composed resources
4. The connection Secret is created in the same namespace as the claim
5. The NetworkPolicy is correctly configured for the selected engine's port
6. Size mapping produces correct instance class and storage parameters
7. Deleting the claim cleans up all composed resources
8. A second Composition for a different cloud provider can be added without changing the XRD

## Verification Commands

```bash
# Crossplane is healthy
kubectl get pods -n crossplane-system
kubectl get providers

# XRD and Compositions exist
kubectl get xrd
kubectl get compositions

# Create a database claim
kubectl apply -f - <<'EOF'
apiVersion: database.platform.example.com/v1alpha1
kind: DatabaseClaim
metadata:
  name: app-db
  namespace: team-alpha
spec:
  engine: postgres
  version: "16"
  size: medium
  region: us-east-1
EOF

# Composite resource is created
kubectl get xdatabases

# Managed resources are provisioning
kubectl get managed

# Secret is created in the claim's namespace
kubectl get secret app-db-connection -n team-alpha -o yaml

# NetworkPolicy exists
kubectl get networkpolicy -n team-alpha

# Check composed resource details
kubectl describe xdatabase $(kubectl get xdatabase -o name | head -1)

# Verify size mapping
kubectl get managed -o custom-columns='NAME:.metadata.name,INSTANCE_CLASS:.spec.forProvider.instanceClass,STORAGE:.spec.forProvider.allocatedStorage'

# Delete claim and verify cleanup
kubectl delete databaseclaim app-db -n team-alpha
kubectl get xdatabases          # should be empty
kubectl get managed             # should be empty
kubectl get secret app-db-connection -n team-alpha  # should be gone

# Test usage limit (create 4 claims, 4th should be rejected or queued)
for i in 1 2 3 4; do
  kubectl apply -f - <<EOF
apiVersion: database.platform.example.com/v1alpha1
kind: DatabaseClaim
metadata:
  name: db-$i
  namespace: team-alpha
spec:
  engine: postgres
  version: "16"
  size: small
  region: us-east-1
EOF
done
kubectl get databaseclaims -n team-alpha
```

## Cleanup

```bash
# Delete all claims
kubectl delete databaseclaims --all -A

# Wait for managed resources to be deleted
kubectl get managed  # wait until empty

# Uninstall Crossplane
helm uninstall crossplane -n crossplane-system
kubectl delete namespace crossplane-system

# Remove CRDs
kubectl get crds | grep crossplane | awk '{print $1}' | xargs kubectl delete crd
kubectl get crds | grep database.platform | awk '{print $1}' | xargs kubectl delete crd
```

# Exercise 9: Enterprise Policy Framework Design

<!--
difficulty: insane
concepts: [policy-framework, policy-hierarchy, exception-management, policy-as-code, multi-cluster, compliance, reporting]
tools: [kubectl, kyverno, gatekeeper]
estimated_time: 120m
bloom_level: create
prerequisites: [13-policy-engines/02-kyverno-basics, 13-policy-engines/03-gatekeeper-constraint-templates, 13-policy-engines/04-kyverno-mutate-policies, 13-policy-engines/05-kyverno-generate-policies, 13-policy-engines/08-policy-testing-and-cicd]
-->

## Scenario

You are the platform security architect for an enterprise with 50+ engineering teams, 8 Kubernetes clusters (dev, staging, production across 3 regions, plus 2 shared services clusters), and strict compliance requirements (SOC 2, PCI-DSS). You must design and implement a comprehensive policy framework that enforces security baselines while allowing team-level exceptions through a controlled process.

## Constraints

1. **Policy hierarchy:** Policies must be organized in three tiers:
   - **Mandatory** (Enforce mode, no exceptions) -- image registry restrictions, privilege escalation blocks, resource limits
   - **Standard** (Enforce mode, exceptions allowed via PolicyException) -- label requirements, network policy requirements, PSA compliance
   - **Advisory** (Audit mode) -- best practices like readOnly filesystem, seccomp profiles, image digest pinning

2. **Policy-as-code:** All policies must be stored in Git, versioned, tested in CI, and deployed via GitOps (Flux or ArgoCD). No manual `kubectl apply` of policies in production.

3. **Exception management:** Teams must be able to request exceptions to Standard-tier policies via a PR process. Each exception must:
   - Be scoped to a specific namespace and resource
   - Have an expiration date (max 90 days)
   - Include a justification
   - Be approved by a security team member
   - Use Kyverno `PolicyException` or equivalent mechanism

4. **Multi-cluster consistency:** The same policy set must be deployed to all 8 clusters with environment-specific overrides (e.g., dev clusters allow Audit mode for policies that are Enforce in production).

5. **Compliance reporting:** Generate policy compliance reports that show:
   - Per-cluster violation counts
   - Per-team violation trends over time
   - Exception inventory with expiration dates
   - CIS benchmark alignment score

6. **Mutation standards:** Implement at least these automated mutations:
   - Add standard labels (team, environment, cost-center) to all resources
   - Inject security defaults (seccomp, capabilities drop)
   - Add pod anti-affinity for all Deployments with >1 replica
   - Set imagePullPolicy to Always for production namespaces

7. **Generation standards:** Auto-create these resources for every new namespace:
   - Default deny NetworkPolicy
   - ResourceQuota
   - LimitRange
   - Monitoring ServiceAccount with read-only RBAC

8. At least 20 total policies across all three tiers, covering validation, mutation, and generation.

## Success Criteria

1. Policy Git repository contains at least 20 policies organized by tier (mandatory/standard/advisory).
2. CI pipeline runs all policy tests and passes on every merge to main.
3. A `PolicyException` for a Standard-tier policy is correctly scoped, time-limited, and allows the excepted resource.
4. Deploying a policy change via GitOps updates all target clusters.
5. Compliance reports are generated showing per-cluster, per-team violation summaries.
6. Mandatory-tier policies cannot be overridden by any team, even with a PolicyException.
7. Creating a new namespace auto-generates NetworkPolicy, ResourceQuota, LimitRange, and monitoring RBAC.
8. All mutations are applied correctly: a bare `kubectl run` command results in a fully labeled, security-hardened pod.

## Verification Commands

```bash
# List all policies by tier
kubectl get clusterpolicy -l policy.example.com/tier=mandatory
kubectl get clusterpolicy -l policy.example.com/tier=standard
kubectl get clusterpolicy -l policy.example.com/tier=advisory

# Check policy exceptions
kubectl get policyexception -A

# Verify mandatory policy cannot be excepted
kubectl apply -f exception-for-mandatory.yaml 2>&1
# Expected: rejected or ignored (mandatory policies have no exception mechanism)

# Check namespace auto-generation
kubectl create namespace new-team
kubectl get networkpolicy,resourcequota,limitrange,rolebinding -n new-team

# Check mutation results
kubectl run test --image=nginx:1.27 -n new-team --dry-run=server -o yaml | \
  grep -E "team|environment|seccompProfile|allowPrivilegeEscalation"

# Compliance report
kubectl get policyreport -A --no-headers | wc -l
kubectl get clusterpolicyreport -o json | \
  python3 -c "import sys,json; d=json.load(sys.stdin); print(f'Pass: {sum(1 for i in d.get(\"items\",[]) for r in i.get(\"results\",[]) if r[\"result\"]==\"pass\")}, Fail: {sum(1 for i in d.get(\"items\",[]) for r in i.get(\"results\",[]) if r[\"result\"]==\"fail\")}')"

# CI test results
kyverno test tests/
gator verify tests/gator-suite.yaml
```

## Cleanup

```bash
kubectl delete namespace new-team 2>/dev/null
kubectl delete clusterpolicy -l policy.example.com/managed=true
kubectl delete policyexception -A --all
```

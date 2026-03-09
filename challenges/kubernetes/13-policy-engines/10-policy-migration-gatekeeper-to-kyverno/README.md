# Exercise 10: Policy Migration: Gatekeeper to Kyverno

<!--
difficulty: insane
concepts: [policy-migration, gatekeeper, kyverno, rego-to-yaml, constraint-to-clusterpolicy, parallel-enforcement, canary-migration]
tools: [kubectl, gatekeeper, kyverno]
estimated_time: 120m
bloom_level: create
prerequisites: [13-policy-engines/01-opa-gatekeeper-basics, 13-policy-engines/02-kyverno-basics, 13-policy-engines/03-gatekeeper-constraint-templates, 13-policy-engines/04-kyverno-mutate-policies]
-->

## Scenario

Your organization has been running OPA Gatekeeper for two years with 15 ConstraintTemplates and 25 Constraints. The platform team has decided to migrate to Kyverno for its simpler YAML-based policies and built-in mutation/generation capabilities. You must execute a zero-downtime migration that:

- Translates all Gatekeeper policies to Kyverno equivalents
- Runs both engines in parallel during the transition
- Validates that Kyverno produces identical admission decisions
- Cuts over to Kyverno-only without any policy gap

## Constraints

1. **Zero policy gaps:** At no point during migration should a resource that would have been rejected by Gatekeeper be allowed through. Both engines must run simultaneously until validation is complete.

2. **Policy parity validation:** For each Gatekeeper policy, create a test suite with at least 3 test cases (1 pass, 2 fail with different violation reasons). Both engines must produce identical results on all test cases.

3. **Phased migration** (5 phases):
   - Phase 1: Install Kyverno alongside Gatekeeper. All Kyverno policies start in `Audit` mode.
   - Phase 2: Validate Kyverno audit results match Gatekeeper enforcement for 48 hours.
   - Phase 3: Switch Kyverno to `Enforce` for non-critical policies. Both engines enforce.
   - Phase 4: Switch Gatekeeper to `dryrun` for migrated policies. Kyverno is primary.
   - Phase 5: Remove Gatekeeper.

4. **Translate these specific policy types:**
   - Required labels (ConstraintTemplate with Rego) -> Kyverno validate pattern
   - Allowed registries (Rego with string matching) -> Kyverno validate deny with conditions
   - Resource limits (Rego iterating containers) -> Kyverno validate pattern
   - Block privilege escalation (Rego security context check) -> Kyverno validate pattern
   - Block hostPath volumes (Rego iterating volumes) -> Kyverno validate deny
   - Required probes (Rego checking liveness/readiness) -> Kyverno validate pattern
   - Namespace label enforcement -> Kyverno validate on Namespace kind

5. **Gatekeeper mutations** (Assign, AssignMetadata) must be translated to Kyverno mutate policies.

6. **Rollback plan:** At each phase, document how to rollback to the previous state within 5 minutes. The rollback must be a single command or script.

7. **Monitoring:** Set up alerts for policy decision discrepancies between the two engines during the parallel-run phase.

## Success Criteria

1. All 15 ConstraintTemplates have equivalent Kyverno ClusterPolicies.
2. All 25 Constraints have equivalent Kyverno rules with matching scope (namespaces, kinds).
3. Test suites pass for all policies on both engines with identical results.
4. The parallel-run phase shows zero discrepancies in admission decisions.
5. After Phase 5, Gatekeeper is fully removed and Kyverno is the sole policy engine.
6. No workload disruption occurs during any phase of the migration.
7. Rollback from any phase to the previous phase completes in under 5 minutes.

## Verification Commands

```bash
# Phase 1: Both engines are running
kubectl get pods -n gatekeeper-system
kubectl get pods -n kyverno

# Phase 2: Compare audit results
# Gatekeeper violations
kubectl get constraints -o json | \
  python3 -c "import sys,json; d=json.load(sys.stdin); [print(f'{i[\"kind\"]}/{i[\"metadata\"][\"name\"]}: {len(i.get(\"status\",{}).get(\"violations\",[]))} violations') for i in d['items']]"

# Kyverno audit results
kubectl get policyreport -A -o json | \
  python3 -c "import sys,json; d=json.load(sys.stdin); [print(f'{i[\"metadata\"][\"namespace\"]}: {sum(1 for r in i.get(\"results\",[]) if r[\"result\"]==\"fail\")} violations') for i in d['items']]"

# Phase 3: Kyverno enforcement is active
kubectl get clusterpolicy -o custom-columns='NAME:.metadata.name,ACTION:.spec.validationFailureAction'

# Phase 4: Gatekeeper in dryrun
kubectl get constraints -o json | \
  python3 -c "import sys,json; d=json.load(sys.stdin); [print(f'{i[\"kind\"]}/{i[\"metadata\"][\"name\"]}: {i[\"spec\"].get(\"enforcementAction\",\"deny\")}') for i in d['items']]"

# Phase 5: Gatekeeper is removed
kubectl get ns gatekeeper-system 2>&1
# Expected: not found

# Test parity: same resource rejected by Kyverno as was by Gatekeeper
kubectl run test-parity --image=evil.io/bad:latest --dry-run=server 2>&1
# Expected: rejected by Kyverno

# Rollback readiness
cat rollback-scripts/phase3-to-phase2.sh
```

## Cleanup

```bash
# Full cleanup (after successful migration)
kubectl delete clusterpolicy -l migration.example.com/source=gatekeeper
kubectl delete constrainttemplate --all
kubectl delete -f https://raw.githubusercontent.com/open-policy-agent/gatekeeper/v3.18.0/deploy/gatekeeper.yaml 2>/dev/null
```

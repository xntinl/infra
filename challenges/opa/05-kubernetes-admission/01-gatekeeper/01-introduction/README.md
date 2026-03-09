# Introduction to Gatekeeper

## Learning Objectives

After completing this section, you will be able to:

- Understand how Kubernetes admission controllers intercept API requests
- Trace the full admission flow from request to etcd
- Explain how Gatekeeper registers itself and evaluates policies

## Why This Matters

An **admission controller** in Kubernetes intercepts every request to the API server before the resource is persisted in etcd. If the controller rejects the request, the resource is not created. It is a control point where you can enforce security, compliance, and convention policies.

**Gatekeeper** is an admission controller that uses OPA as its decision engine. You write the rules in Rego, and Gatekeeper executes them automatically against every request to the API server.

Think of it like a security guard at the door of a building. Every person (API request) must pass through the guard (Gatekeeper). The guard checks a rulebook (your Rego policies) and either lets the person in or turns them away. The rulebook is written once but applied to every visitor.

---

## The Full Admission Flow

When someone runs `kubectl apply`, the request travels through several stages before the resource is stored in etcd:

```
                                                  Gatekeeper
                                                  lives here
                                                      |
                                                      v
Request --> Authentication --> Authorization --> Mutating --> Schema --> Validating --> etcd
                                                Admission   Validation  Admission
```

1. **Authentication**: Is the caller who they claim to be? (certificates, tokens, OIDC)
2. **Authorization**: Is this identity allowed to perform this action? (RBAC, ABAC)
3. **Mutating Admission**: Webhooks that can modify the request. Examples: injecting sidecar containers, adding default labels, setting resource defaults.
4. **Schema Validation**: Does the object conform to the OpenAPI schema for its kind? Catches typos and structural errors.
5. **Validating Admission**: Webhooks that can accept or reject but cannot modify. Gatekeeper runs here. This is the last checkpoint before the resource is persisted.
6. **etcd**: The resource is stored. From this point it is live in the cluster.

**Analogy**: Think of an airport. Check-in (authentication) verifies your identity. The ticket gate (authorization) confirms you have a valid boarding pass. Baggage adjustment (mutating admission) wraps and tags your luggage. The X-ray machine (schema validation) checks the structure of your bags. The final security checkpoint (validating admission / Gatekeeper) decides whether you board or get turned away. Once you pass, you are at the gate (etcd) -- you are in.

Gatekeeper operates at step 5. By the time your policy sees a resource, the caller is authenticated, authorized, and the resource has been mutated and structurally validated. Your Rego only needs to answer one question: does this resource comply with organizational policy?

---

## How Gatekeeper Integrates

Gatekeeper installs itself as a **ValidatingAdmissionWebhook**. This is a Kubernetes-native mechanism: you register a webhook URL, and the API server calls it for matching requests.

The flow works like this:

1. A user submits a resource (e.g., `kubectl apply -f pod.yaml`).
2. The API server builds an **AdmissionReview** object containing the resource and metadata about the request.
3. The API server sends this AdmissionReview as an HTTPS POST to the Gatekeeper webhook endpoint.
4. Gatekeeper finds all Constraints whose `match` fields apply to this resource kind and namespace.
5. For each matching Constraint, Gatekeeper evaluates the associated ConstraintTemplate's Rego against the resource.
6. If any `violation` rule fires, Gatekeeper returns `allowed: false` with the violation messages.
7. If no violations fire, Gatekeeper returns `allowed: true`.

The AdmissionReview has this simplified structure:

```json
{
  "request": {
    "uid": "abc-123",
    "kind": {"group": "", "version": "v1", "kind": "Pod"},
    "operation": "CREATE",
    "object": { ... the full resource ... },
    "oldObject": null,
    "userInfo": {"username": "jane", "groups": ["developers"]}
  }
}
```

Inside your Rego, `input.review.object` maps to `request.object` above. This is the resource you are validating.

---

## What's Next

Continue to [02-concepts](../02-concepts/) to learn about Gatekeeper's operational features: failure policies, enforcement actions, match scoping, and more.

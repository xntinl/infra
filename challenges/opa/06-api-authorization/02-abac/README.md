# ABAC: Attribute-Based Access Control

## Prerequisites

- OPA CLI installed (`opa version`)
- Completed exercise 06-01 (HTTP RBAC)

## Learning Objectives

After completing this exercise, you will be able to:

- Write policies that combine multiple attributes (department, clearance level, time, ownership) into a single authorization decision
- Distinguish when ABAC is necessary versus when RBAC is sufficient
- Build diagnostic `deny_reasons` rules that explain exactly why a request was denied

## Why This Matters

In the previous exercise, RBAC made decisions based on a single attribute: the user's role. That works well for many cases, but it has a limitation. Two editors -- one from finance and one from marketing -- have the same role. With pure RBAC, you cannot prevent one from editing the other department's documents.

ABAC (Attribute-Based Access Control) evaluates **attributes** of the user, the resource, and the context to make decisions. The authorization logic considers combinations like: the user's department, their clearance level, whether they own the resource, and what time the request is made.

The fundamental difference in granularity:

| RBAC | ABAC |
|------|------|
| "You are an editor, you can edit" | "You are an editor in the finance department with clearance level 3, and it is business hours -- you can edit finance documents that require clearance 3 or less" |

Typical attributes fall into three categories:

- **User attributes**: department, clearance level, user ID, location
- **Resource attributes**: owning department, classification level, owner ID
- **Context attributes**: time of day, day of week, source IP

These attributes combine with boolean logic. You can say "allow if the user is in the same department as the resource AND has sufficient clearance AND it is business hours." Each condition is independent, and all must be satisfied.

A particularly powerful pattern is **resource ownership**: "only the owner can modify this." In pure RBAC this is impossible because the role does not know who created each individual resource. In ABAC, you simply compare `user.id == resource.owner_id`.

## Practice

We are building a policy for an internal document management system. The rules are:

1. You can only access documents from your own department
2. Your clearance level must be equal to or higher than what the document requires
3. If you are the document owner, you can edit it regardless of the time
4. If you are NOT the owner, you can only edit during business hours (9 to 18)
5. Anyone can read public documents regardless of department

Create `policy.rego`:

```rego
package api.abac

import rego.v1

default allow := false

# Rule 5: public documents -- anyone can read
allow if {
    input.resource.classification == "public"
    input.action == "read"
}

# Rules 1 + 2: same department, sufficient clearance, reading
allow if {
    input.resource.classification != "public"
    input.user.department == input.resource.department
    input.user.clearance >= input.resource.min_clearance
    input.action == "read"
}

# Rule 3: owner can edit at any time
allow if {
    input.action == "edit"
    input.user.id == input.resource.owner_id
    input.user.department == input.resource.department
    input.user.clearance >= input.resource.min_clearance
}

# Rule 4: non-owner can only edit during business hours
allow if {
    input.action == "edit"
    input.user.id != input.resource.owner_id
    input.user.department == input.resource.department
    input.user.clearance >= input.resource.min_clearance
    input.context.hour >= 9
    input.context.hour < 18
}

# Diagnostic: denial reasons
deny_reasons contains "different department" if {
    input.resource.classification != "public"
    input.user.department != input.resource.department
}

deny_reasons contains "insufficient clearance" if {
    input.user.clearance < input.resource.min_clearance
}

deny_reasons contains "outside business hours" if {
    input.action == "edit"
    input.user.id != input.resource.owner_id
    input.context.hour < 9
}

deny_reasons contains "outside business hours" if {
    input.action == "edit"
    input.user.id != input.resource.owner_id
    input.context.hour >= 18
}
```

Notice how each `allow` rule is a different combination of conditions. OPA evaluates all of them, and if **any** produces `true`, access is granted. The `deny_reasons` rules are independent -- they help diagnose why something was denied.

### Scenario 1: Same Department, Sufficient Clearance (Allowed)

Ana is in finance with clearance 3 and wants to read a finance document that requires clearance 2.

Create `input-same-dept-read.json`:

```json
{
  "user": {
    "id": "ana01",
    "name": "Ana Garcia",
    "department": "finance",
    "clearance": 3
  },
  "resource": {
    "id": "doc-42",
    "department": "finance",
    "min_clearance": 2,
    "classification": "internal",
    "owner_id": "carlos02"
  },
  "action": "read",
  "context": {"hour": 14}
}
```

```bash
opa eval --format pretty -d policy.rego -i input-same-dept-read.json "data.api.abac.allow"
```

```
true
```

Same department, sufficient clearance -- access granted.

### Scenario 2: Cross-Department Access (Denied)

Luis from marketing tries to read a finance document. The only change from the previous scenario: the user's department.

Create `input-cross-dept-denied.json`:

```json
{
  "user": {
    "id": "luis03",
    "name": "Luis Mendez",
    "department": "marketing",
    "clearance": 4
  },
  "resource": {
    "id": "doc-42",
    "department": "finance",
    "min_clearance": 2,
    "classification": "internal",
    "owner_id": "carlos02"
  },
  "action": "read",
  "context": {"hour": 10}
}
```

```bash
opa eval --format pretty -d policy.rego -i input-cross-dept-denied.json "data.api.abac.allow" "data.api.abac.deny_reasons"
```

```
false
[
  "different department"
]
```

Even though Luis has clearance 4 (more than enough), he is in marketing trying to access a finance document. ABAC says no. Notice that `deny_reasons` tells you exactly why.

### Scenario 3: Insufficient Clearance

Maria is in finance but has clearance 1, and the document requires clearance 2. The only change from Scenario 1: the user's clearance level drops from 3 to 1.

Create `input-low-clearance.json`:

```json
{
  "user": {
    "id": "maria04",
    "name": "Maria Lopez",
    "department": "finance",
    "clearance": 1
  },
  "resource": {
    "id": "doc-42",
    "department": "finance",
    "min_clearance": 2,
    "classification": "internal",
    "owner_id": "carlos02"
  },
  "action": "read",
  "context": {"hour": 11}
}
```

```bash
opa eval --format pretty -d policy.rego -i input-low-clearance.json "data.api.abac.allow" "data.api.abac.deny_reasons"
```

```
false
[
  "insufficient clearance"
]
```

Same department, but insufficient clearance.

### Scenario 4: Owner Edits After Hours (Allowed)

Carlos is the document owner and wants to edit it at 22:00. The key variable here is ownership.

Create `input-owner-edit-after-hours.json`:

```json
{
  "user": {
    "id": "carlos02",
    "name": "Carlos Ruiz",
    "department": "finance",
    "clearance": 3
  },
  "resource": {
    "id": "doc-42",
    "department": "finance",
    "min_clearance": 2,
    "classification": "internal",
    "owner_id": "carlos02"
  },
  "action": "edit",
  "context": {"hour": 22}
}
```

```bash
opa eval --format pretty -d policy.rego -i input-owner-edit-after-hours.json "data.api.abac.allow"
```

```
true
```

The owner can edit regardless of the time. This is the power of ABAC -- the ownership rule overrides the time restriction.

### Scenario 5: Non-Owner Edits After Hours (Denied)

Ana wants to edit Carlos's document at 22:00. The only change from the previous scenario: the user is `ana01` instead of `carlos02` (the owner).

Create `input-non-owner-edit-after-hours.json`:

```json
{
  "user": {
    "id": "ana01",
    "name": "Ana Garcia",
    "department": "finance",
    "clearance": 3
  },
  "resource": {
    "id": "doc-42",
    "department": "finance",
    "min_clearance": 2,
    "classification": "internal",
    "owner_id": "carlos02"
  },
  "action": "edit",
  "context": {"hour": 22}
}
```

```bash
opa eval --format pretty -d policy.rego -i input-non-owner-edit-after-hours.json "data.api.abac.allow" "data.api.abac.deny_reasons"
```

```
false
[
  "outside business hours"
]
```

Ana has everything -- correct department, sufficient clearance -- but she is not the owner and it is 22:00. ABAC combines multiple conditions, and it only takes one failing condition to deny access.

### Scenario 6: Public Document

Anyone can read a public document regardless of department or clearance.

Create `input-public-doc-read.json`:

```json
{
  "user": {
    "id": "external99",
    "name": "External User",
    "department": "none",
    "clearance": 0
  },
  "resource": {
    "id": "doc-public-01",
    "department": "hr",
    "min_clearance": 0,
    "classification": "public",
    "owner_id": "hr-admin"
  },
  "action": "read",
  "context": {"hour": 3}
}
```

```bash
opa eval --format pretty -d policy.rego -i input-public-doc-read.json "data.api.abac.allow"
```

```
true
```

The `"public"` classification short-circuits all other rules.

### Common Mistake: Assuming High Clearance Overrides Department

A natural assumption is that a very high clearance level should grant access to any department's documents. Let's verify that it does not.

Create `input-high-clearance-wrong-dept.json`:

```json
{
  "user": {"id": "power-user", "department": "marketing", "clearance": 5},
  "resource": {"id": "doc-42", "department": "finance", "min_clearance": 1, "classification": "internal", "owner_id": "carlos02"},
  "action": "edit",
  "context": {"hour": 12}
}
```

```bash
opa eval --format pretty -d policy.rego -i input-high-clearance-wrong-dept.json "data.api.abac.allow" "data.api.abac.deny_reasons"
```

```
false
[
  "different department"
]
```

Clearance 5 is extremely high, but the wrong department still blocks access. In ABAC, attributes are independent conditions -- high clearance does not compensate for a department mismatch.

## Verify What You Learned

**1.** Ana (finance, clearance 3) edits Carlos's document at 10:00 (business hours).

Create `input-non-owner-edit-work-hours.json`:

```json
{
  "user": {"id": "ana01", "department": "finance", "clearance": 3},
  "resource": {"id": "doc-42", "department": "finance", "min_clearance": 2, "classification": "internal", "owner_id": "carlos02"},
  "action": "edit",
  "context": {"hour": 10}
}
```

```bash
opa eval --format pretty -d policy.rego -i input-non-owner-edit-work-hours.json "data.api.abac.allow"
```

Expected output: `true`

**2.** Query the denial reasons for the high-clearance wrong-department case:

```bash
opa eval --format pretty -d policy.rego -i input-high-clearance-wrong-dept.json "data.api.abac.deny_reasons"
```

Expected output: `["different department"]`

**3.** An external user tries to edit (not read) a public document.

Create `input-public-doc-edit.json`:

```json
{
  "user": {"id": "external99", "department": "none", "clearance": 0},
  "resource": {"id": "doc-public-01", "department": "hr", "min_clearance": 0, "classification": "public", "owner_id": "hr-admin"},
  "action": "edit",
  "context": {"hour": 10}
}
```

```bash
opa eval --format pretty -d policy.rego -i input-public-doc-edit.json "data.api.abac.allow"
```

Expected output: `false` (the public document rule only permits `read`, not `edit`)

## What's Next

Both RBAC and ABAC assume the user's identity is already known. But in real APIs, identity comes from a JWT (JSON Web Token) in the `Authorization` header. The next exercise covers how to decode and validate JWTs with OPA before making authorization decisions.

## Reference

- [OPA -- Comparison operators](https://www.openpolicyagent.org/docs/latest/policy-reference/#comparison) -- `>=`, `<`, `!=` used for clearance and time checks
- [NIST ABAC Guide (SP 800-162)](https://csrc.nist.gov/publications/detail/sp/800-162/final) -- the standard that formally defines the ABAC model
- [OPA -- Sets with `contains`](https://www.openpolicyagent.org/docs/latest/policy-language/#membership-and-iteration-in-rules) -- how `deny_reasons contains` accumulates reasons

## Additional Resources

- [OWASP Access Control Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Access_Control_Cheat_Sheet.html) -- practical access control guidance
- [OPA Playground](https://play.openpolicyagent.org/) -- experiment with ABAC policies interactively

# Decision Logging: Auditing Policy Decisions

## Prerequisites

- OPA CLI installed (`opa version`)
- Completed exercises 06-01 (RBAC), 06-02 (ABAC), and 06-03 (JWT validation)

## Learning Objectives

After completing this exercise, you will be able to:

- Build a structured decision object in Rego that captures who, what, where, why, and when
- Tag decisions with compliance controls (HIPAA, SOC2, GDPR) for audit trails
- Construct human-readable denial reasons alongside machine-readable booleans
- Produce audit-ready output from a single policy evaluation

## Why This Matters

Up to this point, our policies return `true` or `false`. That works for enforcement, but when an auditor asks "why was this user denied access last Tuesday at 15:00?", a bare `false` says nothing. It is the equivalent of an HTTP 403 with no body.

What you actually need is a **rich decision object**: not just whether access was allowed, but who requested what, on which resource, why it was denied (or allowed), and which regulations apply. That is the difference between a useless log entry and an audit trail that saves you during a compliance review.

The approach is straightforward: instead of returning a boolean, return a **structured object** containing:

- **allowed**: the boolean (the machine still needs it)
- **reason**: a human-readable explanation
- **user / action / resource**: the context of the request
- **deny_reasons**: if denied, exactly why (there can be multiple reasons)
- **compliance**: which regulatory controls apply to this decision (SOC2, GDPR, HIPAA, etc.)
- **timestamp / request_id**: when the decision was made and how to correlate it

In Rego, you can construct arbitrary objects as rule results. You are not limited to booleans or strings. This lets you build responses as rich as you need without leaving the policy.

Compliance tags are particularly powerful. If your organization needs to demonstrate SOC2 CC6.1 compliance (logical access restriction) or GDPR Article 25 (data protection by design), you can tag each decision with the relevant controls. When the auditor asks "how do you demonstrate CC6.1 compliance?", you show them the decision log filtered by that tag.

## Practice

We are building an access policy for a healthcare records system -- a domain where decision logging is not optional but a legal requirement in many jurisdictions.

Create `policy.rego`:

```rego
package api.audit

import rego.v1

# =====================================================
# Reference data
# =====================================================

# Roles and their permissions
role_permissions := {
    "doctor": {"read_records", "write_records", "prescribe"},
    "nurse": {"read_records", "update_vitals"},
    "admin": {"read_records", "manage_users"},
    "patient": {"read_own_records"}
}

# Mapping of actions to compliance controls
compliance_map := {
    "read_records":     ["HIPAA-164.312(a)", "SOC2-CC6.1"],
    "write_records":    ["HIPAA-164.312(a)", "HIPAA-164.312(c)", "SOC2-CC6.1"],
    "prescribe":        ["HIPAA-164.312(a)", "HIPAA-164.312(d)"],
    "read_own_records": ["HIPAA-164.312(a)", "GDPR-Art15"],
    "update_vitals":    ["HIPAA-164.312(a)", "SOC2-CC6.1"],
    "manage_users":     ["SOC2-CC6.2", "SOC2-CC6.3"]
}

# =====================================================
# Authorization rules
# =====================================================

default request_allowed := false

# General role-based access
request_allowed if {
    perms := role_permissions[input.user.role]
    input.action in perms
    input.action != "read_own_records"
}

# Special case: patients can only view their own records
request_allowed if {
    input.user.role == "patient"
    input.action == "read_own_records"
    input.user.id == input.resource.patient_id
}

# =====================================================
# Denial reasons
# =====================================================

deny_reasons contains "unrecognized role" if {
    not role_permissions[input.user.role]
}

deny_reasons contains "action not allowed for this role" if {
    perms := role_permissions[input.user.role]
    not input.action in perms
}

deny_reasons contains "patient attempting to access another patient's records" if {
    input.user.role == "patient"
    input.action == "read_own_records"
    input.user.id != input.resource.patient_id
}

# =====================================================
# Decision object
# =====================================================

decision := {
    "allowed": request_allowed,
    "reason": final_reason,
    "user": input.user.id,
    "user_role": input.user.role,
    "action": input.action,
    "resource": input.resource.id,
    "resource_type": input.resource.type,
    "deny_reasons": deny_reasons,
    "compliance": applicable_compliance,
    "timestamp": input.timestamp,
    "request_id": input.request_id
}

# Human-readable reason
final_reason := "access allowed" if {
    request_allowed
}

final_reason := concat(": ", ["access denied", concat(", ", deny_reasons)]) if {
    not request_allowed
}

# Applicable compliance tags
applicable_compliance := compliance_map[input.action] if {
    compliance_map[input.action]
}

applicable_compliance := ["UNKNOWN-ACTION"] if {
    not compliance_map[input.action]
}
```

The policy has three clear layers: the authorization itself (`request_allowed`), the denial diagnostics (`deny_reasons`), and the decision object that packages everything together (`decision`). Each layer is independent, but they feed into each other.

### Scenario 1: Doctor Reads Records (Allowed)

Create `input-doctor-read.json`:

```json
{
  "user": {"id": "dr-martinez", "role": "doctor"},
  "action": "read_records",
  "resource": {"id": "record-2024-001", "type": "medical_record", "patient_id": "patient-555"},
  "timestamp": "2026-03-07T14:30:00Z",
  "request_id": "req-abc-123"
}
```

```bash
opa eval --format pretty -d policy.rego -i input-doctor-read.json "data.api.audit.decision"
```

```json
{
  "allowed": true,
  "action": "read_records",
  "compliance": [
    "HIPAA-164.312(a)",
    "SOC2-CC6.1"
  ],
  "deny_reasons": [],
  "reason": "access allowed",
  "request_id": "req-abc-123",
  "resource": "record-2024-001",
  "resource_type": "medical_record",
  "timestamp": "2026-03-07T14:30:00Z",
  "user": "dr-martinez",
  "user_role": "doctor"
}
```

Look at the richness of that response. It is not just `true` -- it is a complete record of the decision. If an auditor asks "who accessed that medical record?", you have the answer with timestamp, compliance tags, and everything.

### Scenario 2: Nurse Attempts to Prescribe (Denied)

The only change from the previous scenario: the user's role is `nurse` and the action is `prescribe`.

Create `input-nurse-prescribe.json`:

```json
{
  "user": {"id": "nurse-lopez", "role": "nurse"},
  "action": "prescribe",
  "resource": {"id": "rx-2024-050", "type": "prescription", "patient_id": "patient-555"},
  "timestamp": "2026-03-07T15:45:00Z",
  "request_id": "req-def-456"
}
```

```bash
opa eval --format pretty -d policy.rego -i input-nurse-prescribe.json "data.api.audit.decision"
```

```json
{
  "allowed": false,
  "action": "prescribe",
  "compliance": [
    "HIPAA-164.312(a)",
    "HIPAA-164.312(d)"
  ],
  "deny_reasons": [
    "action not allowed for this role"
  ],
  "reason": "access denied: action not allowed for this role",
  "request_id": "req-def-456",
  "resource": "rx-2024-050",
  "resource_type": "prescription",
  "timestamp": "2026-03-07T15:45:00Z",
  "user": "nurse-lopez",
  "user_role": "nurse"
}
```

The nurse cannot prescribe -- only doctors can. Notice that `deny_reasons` explains exactly why, and `compliance` shows which HIPAA controls apply to the prescribe action. Even though the request was denied, the compliance tags are still relevant for auditing purposes.

### Scenario 3: Patient Views Their Own Records (Allowed)

Create `input-patient-own-records.json`:

```json
{
  "user": {"id": "patient-555", "role": "patient"},
  "action": "read_own_records",
  "resource": {"id": "record-2024-001", "type": "medical_record", "patient_id": "patient-555"},
  "timestamp": "2026-03-07T18:00:00Z",
  "request_id": "req-ghi-789"
}
```

```bash
opa eval --format pretty -d policy.rego -i input-patient-own-records.json "data.api.audit.decision"
```

```json
{
  "allowed": true,
  "action": "read_own_records",
  "compliance": [
    "HIPAA-164.312(a)",
    "GDPR-Art15"
  ],
  "deny_reasons": [],
  "reason": "access allowed",
  "request_id": "req-ghi-789",
  "resource": "record-2024-001",
  "resource_type": "medical_record",
  "timestamp": "2026-03-07T18:00:00Z",
  "user": "patient-555",
  "user_role": "patient"
}
```

Notice something interesting: the compliance tags include `GDPR-Art15`, the data subject's right of access. When a patient accesses their own data, that is an exercise of a GDPR right. Having it tagged automatically is extremely valuable for compliance reporting.

### Scenario 4: Patient Tries to View Another Patient's Records (Denied)

The only change: the resource's `patient_id` is different from the user's `id`.

Create `input-patient-other-records.json`:

```json
{
  "user": {"id": "patient-555", "role": "patient"},
  "action": "read_own_records",
  "resource": {"id": "record-2024-002", "type": "medical_record", "patient_id": "patient-999"},
  "timestamp": "2026-03-07T18:05:00Z",
  "request_id": "req-jkl-012"
}
```

```bash
opa eval --format pretty -d policy.rego -i input-patient-other-records.json "data.api.audit.decision"
```

```json
{
  "allowed": false,
  "action": "read_own_records",
  "compliance": [
    "HIPAA-164.312(a)",
    "GDPR-Art15"
  ],
  "deny_reasons": [
    "action not allowed for this role",
    "patient attempting to access another patient's records"
  ],
  "reason": "access denied: action not allowed for this role, patient attempting to access another patient's records",
  "request_id": "req-jkl-012",
  "resource": "record-2024-002",
  "resource_type": "medical_record",
  "timestamp": "2026-03-07T18:05:00Z",
  "user": "patient-555",
  "user_role": "patient"
}
```

Two denial reasons at once. The second one is the most specific and the one that really matters: patient-555 tried to access patient-999's records. In a real environment, this type of event would likely trigger a security alert.

### Scenario 5: Unrecognized Role

Create `input-unknown-role.json`:

```json
{
  "user": {"id": "mystery-user", "role": "janitor"},
  "action": "read_records",
  "resource": {"id": "record-2024-001", "type": "medical_record", "patient_id": "patient-555"},
  "timestamp": "2026-03-07T20:00:00Z",
  "request_id": "req-mno-345"
}
```

```bash
opa eval --format pretty -d policy.rego -i input-unknown-role.json "data.api.audit.decision"
```

```json
{
  "allowed": false,
  "action": "read_records",
  "compliance": [
    "HIPAA-164.312(a)",
    "SOC2-CC6.1"
  ],
  "deny_reasons": [
    "unrecognized role"
  ],
  "reason": "access denied: unrecognized role",
  "request_id": "req-mno-345",
  "resource": "record-2024-001",
  "resource_type": "medical_record",
  "timestamp": "2026-03-07T20:00:00Z",
  "user": "mystery-user",
  "user_role": "janitor"
}
```

The role "janitor" does not exist in `role_permissions`. The diagnostic is clear: "unrecognized role." Without the decision object, you would only see `false` and have to guess why.

## Verify What You Learned

**1.** An admin managing users.

Create `input-admin-manage-users.json`:

```json
{
  "user": {"id": "admin-garcia", "role": "admin"},
  "action": "manage_users",
  "resource": {"id": "user-mgmt", "type": "admin_panel", "patient_id": "n/a"},
  "timestamp": "2026-03-07T09:00:00Z",
  "request_id": "req-verify-1"
}
```

```bash
opa eval --format pretty -d policy.rego -i input-admin-manage-users.json "data.api.audit.decision.allowed" "data.api.audit.decision.compliance"
```

Expected output: `allowed` is `true`, and `compliance` is `["SOC2-CC6.2", "SOC2-CC6.3"]`

**2.** A nurse updating vital signs (a valid action for the nurse role).

Create `input-nurse-update-vitals.json`:

```json
{
  "user": {"id": "nurse-ramirez", "role": "nurse"},
  "action": "update_vitals",
  "resource": {"id": "vitals-2024-100", "type": "vital_signs", "patient_id": "patient-333"},
  "timestamp": "2026-03-07T11:30:00Z",
  "request_id": "req-verify-2"
}
```

```bash
opa eval --format pretty -d policy.rego -i input-nurse-update-vitals.json "data.api.audit.decision.allowed" "data.api.audit.decision.reason"
```

Expected output: `allowed` is `true`, `reason` is `"access allowed"`

**3.** Query the `deny_reasons` for a doctor trying to manage users.

Create `input-doctor-manage-users.json`:

```json
{
  "user": {"id": "dr-martinez", "role": "doctor"},
  "action": "manage_users",
  "resource": {"id": "user-mgmt", "type": "admin_panel", "patient_id": "n/a"},
  "timestamp": "2026-03-07T16:00:00Z",
  "request_id": "req-verify-3"
}
```

```bash
opa eval --format pretty -d policy.rego -i input-doctor-manage-users.json "data.api.audit.decision.deny_reasons"
```

Expected output: `["action not allowed for this role"]`

## Section 06 Summary

Across these four exercises, you built a complete API authorization stack:

1. **RBAC** (exercise 01): role-to-permission mapping with `glob.match` for path matching and deny-by-default semantics
2. **ABAC** (exercise 02): multi-attribute decisions combining department, clearance, ownership, and time -- with diagnostic `deny_reasons`
3. **JWT validation** (exercise 03): claim verification (issuer, audience, expiration, scopes) with `validation_errors` for debugging
4. **Decision logging** (exercise 04): rich structured decision objects with compliance tags for audit trails

The progression followed a real-world pattern: first decide *what* a role can do, then refine with *attribute-based* conditions, then verify *who* is making the request via tokens, and finally *log everything* for compliance. Each layer builds on the previous one.

## What's Next

You now have policies that make and log authorization decisions. The next section covers how to integrate OPA policies into CI/CD pipelines using Conftest -- validating Dockerfiles, Kubernetes manifests, and Terraform plans automatically before they reach production.

## Reference

- [OPA -- Object construction](https://www.openpolicyagent.org/docs/latest/policy-language/#composite-values) -- building rich objects as rule results
- [`concat`](https://www.openpolicyagent.org/docs/latest/policy-reference/#strings) -- joining strings with a separator for human-readable messages
- [OPA Decision Logs](https://www.openpolicyagent.org/docs/latest/management-decision-logs/) -- OPA's native decision logging system (complementary to what we built in the policy)
- [HIPAA Security Rule](https://www.hhs.gov/hipaa/for-professionals/security/) -- the controls referenced in our compliance tags

## Additional Resources

- [SOC2 Compliance Overview](https://us.aicpa.org/interestareas/frc/assuranceadvisoryservices/aaborrowingfundstoinvest) -- understanding the CC6 controls referenced in this exercise
- [GDPR Article 15 -- Right of Access](https://gdpr-info.eu/art-15-gdpr/) -- the data subject right we tagged in patient access decisions
- [OPA Best Practices -- Structured Decisions](https://www.openpolicyagent.org/docs/latest/guides-identity/) -- patterns for building decision objects

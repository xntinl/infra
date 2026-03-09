# JWT Validation: Verifying Tokens

## Prerequisites

- OPA CLI installed (`opa version`)
- Completed exercises 06-01 (RBAC) and 06-02 (ABAC)
- Basic understanding of Base64 encoding

## Learning Objectives

After completing this exercise, you will be able to:

- Decode a JWT and identify its three parts (header, payload, signature)
- Write OPA rules that validate standard claims: issuer, audience, expiration, and scopes
- Build a `validation_errors` diagnostic set that reports all failures at once, not just the first one

## Why This Matters

When your API receives a request, it needs to know who sent it. The most common mechanism in modern APIs is a **JWT** (JSON Web Token) -- a compact token carried in the `Authorization` header that contains claims about the user (identity, permissions, expiration), cryptographically signed by the issuer. OPA can decode the token, verify the signature, and validate the claims before authorizing the request.

### Anatomy of a JWT

A JWT is three parts separated by dots:

```
eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1c2VyMTIzIn0.signature_here
|___________________| |________________________| |________|
      HEADER                  PAYLOAD              SIGNATURE
```

- **Header**: which algorithm was used for signing (`alg: HS256`, `RS256`, etc.)
- **Payload**: the **claims** -- the actual data in the token
- **Signature**: proof that nobody tampered with the token

Each part is JSON encoded in Base64URL. You can decode and read the claims without the secret key -- the signature only guarantees integrity, not confidentiality.

### Claims That Matter

The standard claims relevant for validation are:

- `iss` (issuer): who issued the token (e.g., `"https://auth.myapp.com"`)
- `sub` (subject): the user the token belongs to (e.g., `"user123"`)
- `aud` (audience): who the token is intended for (e.g., `"my-api"`)
- `exp` (expiration): Unix timestamp after which the token is expired
- `iat` (issued at): when the token was issued
- `scope` or `permissions`: what the bearer can do (not a standard claim, but extremely common)

### How OPA Decodes JWTs

OPA provides `io.jwt.decode`, which takes a JWT string and returns an array with `[header, payload, signature]`. For claim validation, we focus on the payload. In production you would use `io.jwt.decode_verify` with public keys for cryptographic signature verification, but for learning claim validation logic, working with the decoded payload directly is clearer.

### Trying `io.jwt.decode`

Before building the policy, let's see how `io.jwt.decode` works. We can construct an unsigned JWT manually:

```bash
# Header: {"alg":"none"}
# Payload: {"sub":"user123","iss":"https://auth.myapp.com","exp":1893456000,"scope":["read","write"]}

HEADER=$(echo -n '{"alg":"none"}' | base64 | tr -d '=' | tr '+/' '-_')
PAYLOAD=$(echo -n '{"sub":"user123","iss":"https://auth.myapp.com","exp":1893456000,"scope":["read","write"]}' | base64 | tr -d '=' | tr '+/' '-_')
echo "${HEADER}.${PAYLOAD}."
```

Now decode that token with OPA:

```bash
TOKEN=$(echo -n '{"alg":"none"}' | base64 | tr -d '=' | tr '+/' '-_').$(echo -n '{"sub":"user123","iss":"https://auth.myapp.com","exp":1893456000,"scope":["read","write"]}' | base64 | tr -d '=' | tr '+/' '-_').

opa eval "io.jwt.decode(\"${TOKEN}\")"
```

You will see the decoded header, payload, and signature. That is all `io.jwt.decode` does -- it decodes Base64, it does not verify anything.

## Practice

For our policy, we pass the claims already decoded in the input. This lets us focus purely on validation logic without worrying about Base64 encoding. In a real system, you would first decode the JWT and then pass the claims to OPA (or OPA would decode them with `io.jwt.decode_verify`).

Create `policy.rego`:

```rego
package api.jwt

import rego.v1

default allow := false

# Issuers we trust
trusted_issuers := {
    "https://auth.myapp.com",
    "https://accounts.google.com",
    "https://login.microsoftonline.com"
}

# Expected audience
expected_audience := "my-api"

# Full token validation
allow if {
    claims_valid
    has_required_scopes
}

# All required claims are present and valid
claims_valid if {
    required_claims_present
    issuer_trusted
    not token_expired
    audience_valid
}

# Required claims exist
required_claims_present if {
    input.claims.iss
    input.claims.sub
    input.claims.exp
}

# The issuer is in our trusted set
issuer_trusted if {
    input.claims.iss in trusted_issuers
}

# The token has not expired
# input.current_time is the current Unix timestamp (passed externally for testability)
token_expired if {
    input.current_time > input.claims.exp
}

# The audience matches what we expect
audience_valid if {
    input.claims.aud == expected_audience
}

# Every required scope must be present in the token's scopes
# input.required_scopes is what the API endpoint needs
has_required_scopes if {
    every scope in input.required_scopes {
        scope in input.claims.scope
    }
}

# Diagnostics: what failed?
validation_errors contains "required claims missing (iss, sub, exp)" if {
    not required_claims_present
}

validation_errors contains "untrusted issuer" if {
    not issuer_trusted
}

validation_errors contains "token expired" if {
    token_expired
}

validation_errors contains "invalid audience" if {
    not audience_valid
}

validation_errors contains "insufficient scopes" if {
    not has_required_scopes
}
```

The policy is decomposed into small, named rules. This is not just for readability -- each rule becomes a diagnostic point. If something fails, `validation_errors` tells you exactly what.

### Scenario 1: Fully Valid Token

A token with all correct claims, not expired, from a trusted issuer, with the required scopes.

Create `input-valid-token.json`:

```json
{
  "claims": {
    "iss": "https://auth.myapp.com",
    "sub": "user123",
    "aud": "my-api",
    "exp": 1893456000,
    "iat": 1700000000,
    "scope": ["read", "write", "admin"]
  },
  "current_time": 1700000500,
  "required_scopes": ["read", "write"]
}
```

```bash
opa eval --format pretty -d policy.rego -i input-valid-token.json "data.api.jwt.allow"
```

```
true
```

Everything checks out. The token has more scopes than required (it also has `admin`), but that does not matter -- we only verify that the required scopes are present.

### Scenario 2: Expired Token

The same token, but `current_time` is after `exp`. The only change: `exp` moves to a past timestamp.

Create `input-expired-token.json`:

```json
{
  "claims": {
    "iss": "https://auth.myapp.com",
    "sub": "user123",
    "aud": "my-api",
    "exp": 1700000000,
    "iat": 1699999000,
    "scope": ["read", "write"]
  },
  "current_time": 1700005000,
  "required_scopes": ["read"]
}
```

```bash
opa eval --format pretty -d policy.rego -i input-expired-token.json "data.api.jwt.allow" "data.api.jwt.validation_errors"
```

```
false
[
  "token expired"
]
```

`current_time` (1700005000) is greater than `exp` (1700000000), so the token is expired.

### Scenario 3: Untrusted Issuer

A token from an issuer not in our trusted set. The only change from Scenario 1: the `iss` value.

Create `input-untrusted-issuer.json`:

```json
{
  "claims": {
    "iss": "https://auth.hacker.com",
    "sub": "evil-user",
    "aud": "my-api",
    "exp": 1893456000,
    "scope": ["read", "write", "admin"]
  },
  "current_time": 1700000000,
  "required_scopes": ["read"]
}
```

```bash
opa eval --format pretty -d policy.rego -i input-untrusted-issuer.json "data.api.jwt.allow" "data.api.jwt.validation_errors"
```

```
false
[
  "untrusted issuer"
]
```

It does not matter that the token has every scope imaginable -- if the issuer is not in `trusted_issuers`, it does not pass.

### Scenario 4: Insufficient Scopes

The token is valid but lacks the `delete` scope that the endpoint requires. The only change: `required_scopes` now asks for `delete`.

Create `input-missing-scope.json`:

```json
{
  "claims": {
    "iss": "https://auth.myapp.com",
    "sub": "user123",
    "aud": "my-api",
    "exp": 1893456000,
    "scope": ["read", "write"]
  },
  "current_time": 1700000000,
  "required_scopes": ["read", "delete"]
}
```

```bash
opa eval --format pretty -d policy.rego -i input-missing-scope.json "data.api.jwt.allow" "data.api.jwt.validation_errors"
```

```
false
[
  "insufficient scopes"
]
```

The user has `read` and `write`, but the endpoint requires `read` and `delete`. The missing `delete` scope causes denial.

### Scenario 5: Multiple Errors at Once

A token that fails several validations simultaneously.

Create `input-multiple-errors.json`:

```json
{
  "claims": {
    "iss": "https://auth.unknown.com",
    "sub": "user456",
    "aud": "other-api",
    "exp": 1600000000,
    "scope": ["read"]
  },
  "current_time": 1700000000,
  "required_scopes": ["admin"]
}
```

```bash
opa eval --format pretty -d policy.rego -i input-multiple-errors.json "data.api.jwt.validation_errors"
```

```
[
  "insufficient scopes",
  "invalid audience",
  "token expired",
  "untrusted issuer"
]
```

Four errors at once. This is invaluable for debugging -- instead of "invalid token," you know exactly what is wrong.

### Scenario 6: Missing Claims

A token that is missing the `exp` claim entirely.

Create `input-missing-claims.json`:

```json
{
  "claims": {
    "iss": "https://auth.myapp.com",
    "sub": "user123",
    "aud": "my-api",
    "scope": ["read"]
  },
  "current_time": 1700000000,
  "required_scopes": ["read"]
}
```

```bash
opa eval --format pretty -d policy.rego -i input-missing-claims.json "data.api.jwt.allow" "data.api.jwt.validation_errors"
```

```
false
[
  "required claims missing (iss, sub, exp)"
]
```

Without `exp`, the token does not even pass the first validation. This is critical for security -- a token without an expiration would be dangerous.

## Verify What You Learned

**1.** A Google token (trusted issuer) with the correct scopes.

Create `input-google-token.json`:

```json
{
  "claims": {
    "iss": "https://accounts.google.com",
    "sub": "google-user-789",
    "aud": "my-api",
    "exp": 1893456000,
    "scope": ["read"]
  },
  "current_time": 1700000000,
  "required_scopes": ["read"]
}
```

```bash
opa eval --format pretty -d policy.rego -i input-google-token.json "data.api.jwt.allow"
```

Expected output: `true`

**2.** The same token but with an incorrect audience.

Create `input-wrong-audience.json`:

```json
{
  "claims": {
    "iss": "https://accounts.google.com",
    "sub": "google-user-789",
    "aud": "some-other-api",
    "exp": 1893456000,
    "scope": ["read"]
  },
  "current_time": 1700000000,
  "required_scopes": ["read"]
}
```

```bash
opa eval --format pretty -d policy.rego -i input-wrong-audience.json "data.api.jwt.validation_errors"
```

Expected output: `["invalid audience"]`

**3.** A token with no scopes (empty array) where the endpoint also requires no scopes.

Create `input-empty-scopes.json`:

```json
{
  "claims": {
    "iss": "https://auth.myapp.com",
    "sub": "minimal-user",
    "aud": "my-api",
    "exp": 1893456000,
    "scope": []
  },
  "current_time": 1700000000,
  "required_scopes": []
}
```

```bash
opa eval --format pretty -d policy.rego -i input-empty-scopes.json "data.api.jwt.allow"
```

Expected output: `true` (no scopes are required, and `every` over an empty set is vacuously true)

## What's Next

You can now validate tokens and make authorization decisions. But when an auditor asks "why was this user denied access last Tuesday at 15:00?", a bare `false` tells them nothing. The next exercise shows how to build rich decision objects that log who requested what, over which resource, why it was denied, and which compliance controls apply.

## Reference

- [`io.jwt.decode`](https://www.openpolicyagent.org/docs/latest/policy-reference/#tokens) -- decodes a JWT without verifying the signature
- [`io.jwt.decode_verify`](https://www.openpolicyagent.org/docs/latest/policy-reference/#tokens) -- decodes and verifies the signature (for production use)
- [`every` keyword](https://www.openpolicyagent.org/docs/latest/policy-language/#every-keyword) -- universal quantification, used to verify that all required scopes are present
- [JWT RFC 7519](https://datatracker.ietf.org/doc/html/rfc7519) -- the formal JWT specification

## Additional Resources

- [jwt.io](https://jwt.io/) -- decode and inspect JWTs in the browser
- [Auth0 JWT Handbook](https://auth0.com/resources/ebooks/jwt-handbook) -- comprehensive guide to JWT mechanics and best practices

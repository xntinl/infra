# HTTP RBAC: Protecting Your API

## Prerequisites

- OPA CLI installed (`opa version`)
- Completed section 05 exercises (Rego fundamentals)

## Learning Objectives

After completing this exercise, you will be able to:

- Define roles and permissions as external data that OPA consults at evaluation time
- Write a deny-by-default policy that matches HTTP methods and paths using `glob.match`
- Test authorization decisions across multiple roles and endpoints

## Why This Matters

Every HTTP API needs to answer four questions about each incoming request:

- **Who** is making the request? (the user and their role)
- **What** do they want to do? (the HTTP method: GET, POST, PUT, DELETE)
- **Where** do they want to do it? (the path: `/api/users`, `/api/posts/42`)
- **Are they allowed?** (the policy decision)

RBAC (Role-Based Access Control) is the most common model for answering these questions: each user has a role, and each role has specific permissions on specific endpoints. The strategy is **deny-by-default** -- everything is forbidden unless a rule explicitly permits it. Think of it like a firewall with a `default DROP` policy: only traffic that matches an explicit `ALLOW` rule gets through.

For path matching we use `glob.match`, which lets us use wildcards. For example, `glob.match("/api/users/*", ["/"], "/api/users/123")` matches any individual resource under `/api/users/`. This is far more practical than comparing paths one by one.

## Practice

We are going to protect an API with 5 endpoints and 3 roles. First, we define each role's permissions in a data file, then the policy that evaluates each request.

Create `roles.json`:

```json
{
  "roles": {
    "admin": {
      "permissions": [
        {"method": "GET",    "path": "/api/users/*"},
        {"method": "POST",   "path": "/api/users"},
        {"method": "PUT",    "path": "/api/users/*"},
        {"method": "DELETE", "path": "/api/users/*"},
        {"method": "GET",    "path": "/api/posts/*"},
        {"method": "POST",   "path": "/api/posts"},
        {"method": "PUT",    "path": "/api/posts/*"},
        {"method": "DELETE", "path": "/api/posts/*"},
        {"method": "GET",    "path": "/api/settings"}
      ]
    },
    "editor": {
      "permissions": [
        {"method": "GET",    "path": "/api/users/*"},
        {"method": "GET",    "path": "/api/posts/*"},
        {"method": "POST",   "path": "/api/posts"},
        {"method": "PUT",    "path": "/api/posts/*"}
      ]
    },
    "viewer": {
      "permissions": [
        {"method": "GET", "path": "/api/users/*"},
        {"method": "GET", "path": "/api/posts/*"}
      ]
    }
  }
}
```

The admin can do everything. The editor can read users and manage posts (but not delete them). The viewer can only read. Nobody except the admin can touch settings.

Now the policy.

Create `policy.rego`:

```rego
package api.authz

import rego.v1

default allow := false

allow if {
    some perm in data.roles[input.user.role].permissions
    perm.method == input.request.method
    glob.match(perm.path, ["/"], input.request.path)
}
```

That is the entire policy. The rule says: "allow if there exists some permission in the user's role whose method matches the request method AND whose path pattern (with wildcards) matches the request path." If no combination matches, the `default allow := false` wins.

Let's test with an admin reading a user.

Create `input-admin-get-user.json`:

```json
{
  "user": {"name": "alice", "role": "admin"},
  "request": {"method": "GET", "path": "/api/users/42"}
}
```

```bash
opa eval --format pretty -d policy.rego -d roles.json -i input-admin-get-user.json "data.api.authz.allow"
```

```
true
```

Now an editor trying to delete a post.

Create `input-editor-delete-post.json`:

```json
{
  "user": {"name": "bob", "role": "editor"},
  "request": {"method": "DELETE", "path": "/api/posts/99"}
}
```

```bash
opa eval --format pretty -d policy.rego -d roles.json -i input-editor-delete-post.json "data.api.authz.allow"
```

```
false
```

The editor role has no DELETE permission for posts, so the request is denied.

A viewer reading posts (allowed).

Create `input-viewer-read-post.json`:

```json
{
  "user": {"name": "carol", "role": "viewer"},
  "request": {"method": "GET", "path": "/api/posts/5"}
}
```

```bash
opa eval --format pretty -d policy.rego -d roles.json -i input-viewer-read-post.json "data.api.authz.allow"
```

```
true
```

A viewer trying to create a user (denied).

Create `input-viewer-create-user.json`:

```json
{
  "user": {"name": "carol", "role": "viewer"},
  "request": {"method": "POST", "path": "/api/users"}
}
```

```bash
opa eval --format pretty -d policy.rego -d roles.json -i input-viewer-create-user.json "data.api.authz.allow"
```

```
false
```

### Edge Case: a Role That Does Not Exist

Create `input-unknown-role.json`:

```json
{
  "user": {"name": "hacker", "role": "superadmin"},
  "request": {"method": "GET", "path": "/api/settings"}
}
```

```bash
opa eval --format pretty -d policy.rego -d roles.json -i input-unknown-role.json "data.api.authz.allow"
```

```
false
```

This is critical: a nonexistent role simply has no permissions. There is no error, no exception -- deny-by-default does its job silently. If someone injects a fabricated role name, they get nothing.

## Verify What You Learned

**1.** Confirm that the editor can create posts.

Create `input-editor-create-post.json`:

```json
{
  "user": {"name": "bob", "role": "editor"},
  "request": {"method": "POST", "path": "/api/posts"}
}
```

```bash
opa eval --format pretty -d policy.rego -d roles.json -i input-editor-create-post.json "data.api.authz.allow"
```

Expected output: `true`

**2.** Confirm that the viewer cannot access settings.

Create `input-viewer-get-settings.json`:

```json
{
  "user": {"name": "carol", "role": "viewer"},
  "request": {"method": "GET", "path": "/api/settings"}
}
```

```bash
opa eval --format pretty -d policy.rego -d roles.json -i input-viewer-get-settings.json "data.api.authz.allow"
```

Expected output: `false`

**3.** Confirm that the admin can access settings.

Create `input-admin-get-settings.json`:

```json
{
  "user": {"name": "alice", "role": "admin"},
  "request": {"method": "GET", "path": "/api/settings"}
}
```

```bash
opa eval --format pretty -d policy.rego -d roles.json -i input-admin-get-settings.json "data.api.authz.allow"
```

Expected output: `true`

## What's Next

RBAC assigns permissions based on a single attribute: the user's role. But what if two editors work in different departments and should only access their own department's resources? That requires evaluating multiple attributes at once -- which is exactly what ABAC (Attribute-Based Access Control) provides in the next exercise.

## Reference

- [`glob.match`](https://www.openpolicyagent.org/docs/latest/policy-reference/#glob) -- wildcard pattern matching for paths
- [Policy Reference -- `default`](https://www.openpolicyagent.org/docs/latest/policy-language/#default-keyword) -- how deny-by-default works
- [External Data](https://www.openpolicyagent.org/docs/latest/external-data/) -- loading external data like the roles file

## Additional Resources

- [OPA Playground](https://play.openpolicyagent.org/) -- experiment with policies in the browser
- [Styra Academy -- OPA Fundamentals](https://academy.styra.com/) -- free course covering RBAC patterns with OPA

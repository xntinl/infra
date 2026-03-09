# 1. Input vs Data: Two Worlds of Information

## Prerequisites

- `opa` CLI installed ([install guide](https://www.openpolicyagent.org/docs/latest/#1-download-opa))
- Completed section `01-elements`

## Learning Objectives

After completing this exercise, you will be able to:

- Distinguish between `input` (dynamic per-request data) and `data` (static configuration loaded at startup)
- Load JSON files as static data using the `-d` flag
- Build a complete RBAC system that separates configuration from policy logic

## Why Separate Input from Data

Throughout section `01-elements`, you worked exclusively with `input` -- the JSON you pass to OPA via the `-i` flag. But real-world policies need a second source of information: `data`. Understanding the boundary between these two is fundamental to writing maintainable policies.

Consider a role-based access control system. The request ("Alice wants to delete resource X") changes with every evaluation. But the role definitions ("admins can read, write, and delete") change only when an administrator updates the configuration. Mixing both into a single input document means every request must carry the entire role configuration with it. Separating them lets each source change at its own pace.

OPA works with two distinct sources:

- **`input`** is the dynamic data for each request: who the user is, what they want to do, which resource they want to access. It changes with every evaluation. Passed via the `-i` flag.
- **`data`** is the static data you load alongside the policy: which roles exist, what permissions each role has, which users belong to each role. Loaded at startup via the `-d` flag.

The policy (the `.rego` file) receives the request (`input`), consults the static configuration (`data`), and produces a decision.

## The `data` Namespace

You already know `input.*` -- you access fields from the JSON passed with `-i`. Static data lives under `data.*`. But there is something you may not have noticed: **your own rules also live under `data`**.

When you run `opa eval "data.myapp.allow"`, the `data.myapp` part is the namespace of your package. Rego rules, JSON files loaded as static data -- everything lives under the `data` tree. The `input` is the only thing that lives separately.

```
data.*          <-- all static data
  data.roles    <-- a JSON loaded as data
  data.users    <-- another JSON loaded as data
  data.rbac.*   <-- your rules (package rbac)

input.*         <-- dynamic data (each request)
```

## Loading Data with `-d`

You have been using `-d` to load `.rego` files. It turns out `-d` also loads `.json` (and `.yaml`) files. OPA decides what to do based on the file extension:

- `.rego` -- interpreted as policy (rules)
- `.json` / `.yaml` -- loaded as static data under `data`

What happens is straightforward: OPA takes the contents of the JSON and **merges** them directly into the `data` tree. If your file contains `{"roles": {...}}`, then `data.roles` exists. If it contains `{"users": {...}}`, then `data.users` exists.

## Building a Complete RBAC System

We will build a role-based access control (RBAC) system using three files: one with roles and their permissions, one with users and their roles, and the policy that makes the decision.

Create `roles.json`:

```json
{
    "roles": {
        "admin": {
            "permissions": ["read", "write", "delete"]
        },
        "editor": {
            "permissions": ["read", "write"]
        },
        "viewer": {
            "permissions": ["read"]
        }
    }
}
```

This file defines three roles. Each role has an array of permissions. `admin` can do everything, `editor` can read and write, `viewer` can only read.

Create `users.json`:

```json
{
    "users": {
        "alice": {
            "role": "admin"
        },
        "bob": {
            "role": "editor"
        },
        "charlie": {
            "role": "viewer"
        }
    }
}
```

This file maps each user to their role. Alice is admin, Bob is editor, Charlie is viewer.

Now create `policy.rego`:

```rego
package rbac

import rego.v1

# Step 1: look up the user's role in data.users
user_role := data.users[input.user].role

# Step 2: look up that role's permissions in data.roles
user_permissions := data.roles[user_role].permissions

# Step 3: allow if the requested action is in the role's permissions
allow if {
    some permission in user_permissions
    permission == input.action
}
```

Read the policy line by line:

1. `user_role` looks up the user from the input in `data.users` and extracts their role. If the input says `"user": "alice"`, then `data.users["alice"].role` is `"admin"`.
2. `user_permissions` takes that role and looks up its permissions in `data.roles`. If the role is `"admin"`, then `data.roles["admin"].permissions` is `["read", "write", "delete"]`.
3. `allow` iterates through the permissions with `some` and checks if any match the requested action from the input.

The key insight is that the policy **does not know** which roles exist or what permissions each one has. It only knows how to look them up in `data`. You can add roles or change permissions without touching the policy.

## Testing with Multiple Inputs

Now test with different users and actions.

**Alice (admin) wants to delete:**

Create `input-alice-delete.json`:

```json
{
    "user": "alice",
    "action": "delete"
}
```

```bash
opa eval --format pretty \
  -d policy.rego -d roles.json -d users.json \
  -i input-alice-delete.json \
  "data.rbac"
```

```
{
  "allow": true,
  "user_permissions": [
    "read",
    "write",
    "delete"
  ],
  "user_role": "admin"
}
```

Alice is admin, she has the `delete` permission, so `allow` is `true`. You can also see `user_role` and `user_permissions` because they are visible rules in the package.

**Bob (editor) wants to write:**

Create `input-bob-write.json`:

```json
{
    "user": "bob",
    "action": "write"
}
```

```bash
opa eval --format pretty \
  -d policy.rego -d roles.json -d users.json \
  -i input-bob-write.json \
  "data.rbac"
```

```
{
  "allow": true,
  "user_permissions": [
    "read",
    "write"
  ],
  "user_role": "editor"
}
```

Bob is editor, he has the `write` permission. Allowed.

### Intermediate Verification: What Happens When Permission Is Missing

Change only one variable from the previous scenario: instead of Bob writing, Bob tries to delete.

Create `input-bob-delete.json`:

```json
{
    "user": "bob",
    "action": "delete"
}
```

```bash
opa eval --format pretty \
  -d policy.rego -d roles.json -d users.json \
  -i input-bob-delete.json \
  "data.rbac"
```

```
{
  "user_permissions": [
    "read",
    "write"
  ],
  "user_role": "editor"
}
```

Notice: `allow` **does not appear**. It is not `false`, it is `undefined` -- because `delete` is not in the editor's permissions. The permissions and role resolve fine, but the `allow` rule does not match.

**Charlie (viewer) wants to read:**

Create `input-charlie-read.json`:

```json
{
    "user": "charlie",
    "action": "read"
}
```

```bash
opa eval --format pretty \
  -d policy.rego -d roles.json -d users.json \
  -i input-charlie-read.json \
  "data.rbac"
```

```
{
  "allow": true,
  "user_permissions": [
    "read"
  ],
  "user_role": "viewer"
}
```

Charlie is viewer, has only `read`, and that is exactly what was requested. Allowed.

## Common Mistakes

### Unknown User Produces Silent Failure

What happens if the input contains a user that does not exist in `data.users`?

Create `input-diana-read.json`:

```json
{
    "user": "diana",
    "action": "read"
}
```

```bash
opa eval --format pretty \
  -d policy.rego -d roles.json -d users.json \
  -i input-diana-read.json \
  "data.rbac"
```

```
{}
```

Empty result. Neither `allow`, nor `user_role`, nor `user_permissions` appear. Everything is `undefined`.

Here is what happened: `data.users["diana"]` does not exist, so `data.users[input.user].role` is `undefined`. When `user_role` is undefined, `data.roles[user_role].permissions` is also undefined. And without permissions, `allow` cannot resolve either.

OPA does not throw an error or crash -- it simply produces no results. This is secure by default: if it cannot determine the user's role, it does not allow anything. But it can also be confusing if you do not expect it: a typo in the username silently produces "no results" instead of a clear error.

You can verify field by field to see where the chain breaks (reusing `input-diana-read.json`):

```bash
opa eval --format pretty \
  -d policy.rego -d roles.json -d users.json \
  -i input-diana-read.json \
  "data.rbac.user_role"
```

(no output)

This confirms the problem starts at `user_role` -- the user does not exist in `data.users`.

## Why Separating Input from Data Matters

Why does OPA separate these two worlds? Because they change at different rates:

- **Data** (roles, permissions, configuration) changes when an admin updates the configuration. Perhaps once a day or once a week.
- **Input** changes with every request. Thousands of times per second.
- **Policy** changes when a developer updates it. Perhaps once a month.

By separating them, you can update roles without rewriting the policy, and the policy receives thousands of different inputs without the static data changing.

## Verify What You Learned

```bash
opa eval --format pretty \
  -d policy.rego -d roles.json -d users.json \
  -i input-alice-delete.json \
  "data.rbac.allow"
```

```
true
```

Create `input-charlie-write.json`:

```json
{
    "user": "charlie",
    "action": "write"
}
```

```bash
opa eval --format pretty \
  -d policy.rego -d roles.json -d users.json \
  -i input-charlie-write.json \
  "data.rbac.allow"
```

(no output -- undefined, because viewer does not have the `write` permission)

Create `input-bob-read.json`:

```json
{
    "user": "bob",
    "action": "read"
}
```

```bash
opa eval --format pretty \
  -d policy.rego -d roles.json -d users.json \
  -i input-bob-read.json \
  "data.rbac.user_role"
```

```
"editor"
```

Create `input-alice-read.json`:

```json
{
    "user": "alice",
    "action": "read"
}
```

```bash
opa eval --format pretty \
  -d policy.rego -d roles.json -d users.json \
  -i input-alice-read.json \
  "data.rbac.user_permissions"
```

```
[
  "read",
  "write",
  "delete"
]
```

```bash
opa eval --format pretty \
  -d policy.rego -d roles.json -d users.json \
  -i input-diana-read.json \
  "data.rbac"
```

```
{}
```

## What's Next

You now understand how to separate dynamic request data (`input`) from static configuration (`data`), and how to build policies that connect the two. In the next exercise, you will learn about partial rules and the `default` keyword -- tools for accumulating collections of results and ensuring your policies always return a predictable value.

## Reference

- [The Data Document](https://www.openpolicyagent.org/docs/latest/philosophy/#the-data-document)
- [Referring to External Data](https://www.openpolicyagent.org/docs/latest/external-data/)
- [opa eval CLI](https://www.openpolicyagent.org/docs/latest/cli/#opa-eval)

## Additional Resources

- [OPA Playground](https://play.openpolicyagent.org/) -- interactive browser-based Rego evaluator
- [Styra Academy](https://academy.styra.com/) -- free OPA/Rego courses
- [OPA Policy Cheatsheet](https://docs.styra.com/opa/rego-cheat-sheet) -- quick reference for Rego syntax
